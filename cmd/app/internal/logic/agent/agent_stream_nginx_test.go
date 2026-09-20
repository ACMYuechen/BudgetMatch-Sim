package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"budgetmatch-sim/cmd/app/internal/types"

	"github.com/stretchr/testify/require"
)

// Opt-in real Nginx, production repository proxy directives, real TCP HTTP/gRPC,
// synthetic identity and RPC output. This is not a deployed/Auth/DB/model test.
func TestNginxRepositoryTemplateStream(t *testing.T) {
	bin := os.Getenv("BUDGETMATCH_TEST_NGINX_BIN")
	if bin == "" {
		t.Skip("set BUDGETMATCH_TEST_NGINX_BIN to a trusted local Nginx executable")
	}
	require.True(t, filepath.IsAbs(bin), "Nginx executable must be an explicit absolute path")
	stat, err := os.Stat(bin)
	require.NoError(t, err)
	require.True(t, stat.Mode().IsRegular() && stat.Mode().Perm()&0111 != 0, "Nginx executable must be executable")
	for _, mode := range []string{"complete", "late error", "missing done", "after done", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newProxyCloseoutGatewayHarness(t, mode)
			origin := startOwnedNginx(t, bin, h.url)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/agent/recommend/stream",
				strings.NewReader(`{"query":"synthetic nginx probe","conversation_id":"c","turn_id":"c","stream_version":1}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			response, err := h.client.Do(req)
			require.NoError(t, err)
			t.Cleanup(func() { _ = response.Body.Close() })
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Contains(t, response.Header.Get("Content-Type"), "version=1")
			require.Equal(t, "no-cache, no-transform", response.Header.Get("Cache-Control"))
			// Nginx consumes X-Accel-Buffering; progress below verifies forwarding
			// without requiring that internal control header to reach clients.
			results := readProxyCloseout(t, response)
			var events []*types.AgentStreamEvent
			for range 4 {
				result := nextProxyCloseout(t, results)
				require.False(t, result.eof)
				require.NotNil(t, result.event)
				events = append(events, result.event)
			}
			require.NotNil(t, events[3].AnswerDelta, "Nginx must forward progress before RPC completion")
			if mode == "cancel" {
				cancel()
				result := nextProxyCloseout(t, results)
				require.True(t, result.eof)
				require.Error(t, result.err, "client cancellation is not a successful EOF")
			} else {
				if mode == "complete" || mode == "late error" {
					awaitProxyCloseout(t, h.rpc.prepared, "scripted terminal sends")
					select {
					case <-results:
						t.Fatal("terminal frame escaped before normal RPC EOF")
					case <-time.After(100 * time.Millisecond):
					}
					close(h.rpc.release)
				}
				for {
					result := nextProxyCloseout(t, results)
					if result.eof {
						require.NoError(t, result.err)
						break
					}
					require.NotNil(t, result.event)
					events = append(events, result.event)
				}
				require.Len(t, events, 6)
				require.NotNil(t, events[5].Done)
				require.Equal(t, mode == "complete", events[5].Done.Ok)
				if mode == "complete" {
					require.NotNil(t, events[4].Final)
				} else {
					require.NotNil(t, events[4].Error)
				}
			}
			for i, event := range events {
				require.EqualValues(t, 1, event.SchemaVersion)
				require.EqualValues(t, i+1, event.Sequence)
				require.Equal(t, "execution", event.ExecutionId)
				require.Equal(t, "c", event.ConversationId)
				require.Equal(t, "c", event.TurnId)
				if mode != "complete" {
					require.Nil(t, event.Final, "failed/canceled stream must not expose success")
				}
			}
			awaitProxyCloseout(t, h.rpc.finished, "Nginx upstream RPC stop")
			awaitProxyCloseout(t, h.gatewayDone, "Nginx upstream HTTP handler stop")
			require.Equal(t, int32(1), h.rpc.streams.Load())
			require.Zero(t, h.rpc.unary.Load(), "Nginx must not cause a unary retry")
			if mode == "cancel" {
				require.ErrorIs(t, <-h.rpc.stopped, context.Canceled)
			}
		})
	}
}

func startOwnedNginx(t *testing.T, bin, upstream string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../../web-ui/nginx/default.conf"))
	require.NoError(t, err)
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	template := string(source)
	for _, replacement := range []struct {
		old, new string
		count    int
	}{
		{"listen 80;", "listen " + address + ";", 1},
		{"http://app:10002", upstream, 2},
		{"http://admin:10001", upstream, 1},
		{"/usr/share/nginx/html", fmt.Sprintf("%q", dir), 1},
	} {
		require.Equal(t, replacement.count, strings.Count(template, replacement.old), "repository template changed; review isolation substitutions")
		template = strings.ReplaceAll(template, replacement.old, replacement.new)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.conf"), []byte(template), 0600))
	config := fmt.Sprintf(`daemon off;
worker_processes 1;
worker_shutdown_timeout 1s;
pid %q;
error_log %q warn;
events { worker_connections 64; }
http {
    access_log off;
    client_body_temp_path %q;
    proxy_temp_path %q;
    fastcgi_temp_path %q;
    uwsgi_temp_path %q;
    scgi_temp_path %q;
    include %q;
}
`, filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "error.log"),
		filepath.Join(dir, "client"), filepath.Join(dir, "proxy"), filepath.Join(dir, "fastcgi"),
		filepath.Join(dir, "uwsgi"), filepath.Join(dir, "scgi"), filepath.Join(dir, "default.conf"))
	configPath := filepath.Join(dir, "nginx.conf")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0600))
	args := []string{"-p", dir + string(filepath.Separator), "-c", configPath, "-e", filepath.Join(dir, "error.log")}
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	checkCtx, checkCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer checkCancel()
	check := exec.CommandContext(checkCtx, bin, append([]string{"-t"}, args...)...)
	check.Env = env
	output, err := check.CombinedOutput()
	require.NoError(t, err, "isolated nginx -t: %s", output)
	log, err := os.OpenFile(filepath.Join(dir, "process.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = log.Close() })
	cmd := exec.Command(bin, args...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, log, log
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Signal(syscall.SIGQUIT)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill() // Only this test's own child handle.
				<-done
				t.Error("owned Nginx did not stop gracefully")
			}
		}
		require.NoError(t, waitErr, "owned Nginx exit")
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("owned Nginx exited during startup: %v", waitErr)
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return "http://" + address
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("owned Nginx readiness timed out")
	return ""
}
