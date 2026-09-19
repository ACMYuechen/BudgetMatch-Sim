package storageacceptance

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestAcceptanceTargetRejectsUnsafeAddresses(t *testing.T) {
	for _, address := range []string{"localhost:25432", "db:25432", "8.8.8.8:25432", "0.0.0.0:25432",
		"192.168.1.1:25432", "/tmp/postgres", "127.0.0.1", "127.0.0.1:5432", "127.0.0.1:6379",
		"[::]:25432", "[::1%lo]:25432", "127.0.0.1:80", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:+25432"} {
		t.Run(address, func(t *testing.T) { require.Error(t, checkAddress(address)) })
	}
	for _, address := range []string{"127.0.0.1:25432", "[::1]:26379"} {
		require.NoError(t, checkAddress(address))
	}
}

func TestAcceptanceConfigIsExplicitPrivateAndDoesNotInherit(t *testing.T) {
	cfg := testTarget()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "acceptance.json")
	require.NoError(t, os.WriteFile(path, data, 0600))
	for _, tc := range []struct {
		name, path, grant string
		environ           []string
	}{
		{name: "no-config", grant: cfg.RunID},
		{name: "no-grant", path: path},
		{name: "wrong-grant", path: path, grant: strings.Repeat("f", 32)},
		{name: "pg-host", path: path, grant: cfg.RunID, environ: []string{"PGHOST=elsewhere"}},
		{name: "pg-service", path: path, grant: cfg.RunID, environ: []string{"PGSERVICE=business"}},
		{name: "pg-password-file", path: path, grant: cfg.RunID, environ: []string{"PGPASSFILE=secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadGrantedTarget(tc.path, tc.grant, tc.environ)
			require.Error(t, err)
			require.NotContains(t, err.Error(), cfg.PostgresPassword)
			require.NotContains(t, err.Error(), cfg.RedisPassword)
		})
	}
	got, err := loadGrantedTarget(path, cfg.RunID, []string{"RAG_TEST_PG_DSN=ignored", "AGENT_MEMORY_TEST_PG_DSN=ignored", "GPG_TTY=ignored"})
	require.NoError(t, err)
	require.Equal(t, cfg, got)
	require.NoError(t, os.Chmod(path, 0644))
	_, err = loadGrantedTarget(path, cfg.RunID, nil)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(t.TempDir(), "config-link")
	require.NoError(t, os.Symlink(path, link))
	_, err = loadGrantedTarget(link, cfg.RunID, nil)
	require.Error(t, err)
	_, err = loadGrantedTarget(filepath.Dir(path), cfg.RunID, nil)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte(" "), 16*1024+1), 0600))
	_, err = loadGrantedTarget(path, cfg.RunID, nil)
	require.Error(t, err)
}

func TestAcceptancePostgresDSNCannotOverrideTarget(t *testing.T) {
	cfg := testTarget()
	cfg.PostgresPassword = "synthetic@pass:/?#&=word"
	parsed, err := url.Parse(cfg.postgresDSN())
	require.NoError(t, err)
	require.Equal(t, cfg.PostgresAddress, parsed.Host)
	require.Equal(t, "/agent_m62_"+cfg.RunID, parsed.Path)
	require.Equal(t, "agent_m62", parsed.User.Username())
	password, exists := parsed.User.Password()
	require.True(t, exists)
	require.Equal(t, cfg.PostgresPassword, password)
	require.Equal(t, "disable", parsed.Query().Get("sslmode"))
	require.Empty(t, parsed.Query().Get("host"))
}

func TestAcceptanceConfigStrictDecodingAndRedaction(t *testing.T) {
	cfg := testTarget()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	got, err := decodeTarget(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, cfg, got)
	for _, raw := range []string{`{}`, `null`, `[]`, string(data) + `{}`, string(data) + `garbage`,
		strings.TrimSuffix(string(data), "}") + `,"dsn":"secret"}`, `{"run_id": "secret-unclosed`} {
		_, err := decodeTarget(strings.NewReader(raw))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.NotContains(t, err.Error(), cfg.PostgresPassword)
	}
	for name, change := range map[string]func(*targetConfig){
		"bad-run-id":           func(c *targetConfig) { c.RunID = "../business" },
		"uppercase":            func(c *targetConfig) { c.RunID = strings.Repeat("A", 32) },
		"short-run-id":         func(c *targetConfig) { c.RunID = "1234" },
		"no-password":          func(c *targetConfig) { c.PostgresPassword = "" },
		"short-redis-password": func(c *targetConfig) { c.RedisPassword = "short" },
		"same-endpoint":        func(c *targetConfig) { c.RedisAddress = c.PostgresAddress },
		"remote-postgres":      func(c *targetConfig) { c.PostgresAddress = "203.0.113.1:25432" },
		"remote-redis":         func(c *targetConfig) { c.RedisAddress = "203.0.113.1:26379" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := cfg
			change(&copy)
			require.Error(t, copy.validate(true))
		})
	}
}

func TestAcceptanceRedisRequiresMatchingMarkerBeforeWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := testTarget()
	cfg.RedisAddress = mr.Addr()
	mr.RequireAuth(cfg.RedisPassword)
	for _, owner := range []string{"", "another-run", cfg.RunID} {
		if owner != "" {
			require.NoError(t, mr.Set(ownerKey, owner))
		}
		before := mr.Dump()
		client, err := cfg.openRedis(t.Context())
		if owner != cfg.RunID {
			require.Error(t, err)
			require.Nil(t, client)
		} else {
			require.NoError(t, err)
			require.NoError(t, client.Close())
		}
		require.Equal(t, before, mr.Dump(), "preflight mutated Redis")
	}
}

func TestWorkerProtocolGatesRejectMissingOrWrongCommands(t *testing.T) {
	for _, command := range []string{"", `"start"`, `"continue"`} {
		var output bytes.Buffer
		p := workerProtocol{input: json.NewDecoder(strings.NewReader(command)), output: json.NewEncoder(&output), pause: "before-save"}
		err := p.barrier("before-save")
		if command == `"continue"` {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		var event workerEvent
		require.NoError(t, json.Unmarshal(output.Bytes(), &event))
		require.Equal(t, "before-save", event.Stage)
		require.Equal(t, os.Getpid(), event.PID)
	}
}
