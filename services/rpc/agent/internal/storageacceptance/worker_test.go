package storageacceptance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"github.com/stretchr/testify/require"
)

const workerEnv = "AGENT_STORAGE_ACCEPTANCE_WORKER"

type workerRequest struct {
	Target    targetConfig
	Mode      string
	Input     agentcore.Input
	Pause     string
	Delete    bool
	Label     string
	TimeoutMS int
}

type workerEvent struct {
	Stage       string
	PID         int
	Calls       int
	Prior       *agentcore.Intent
	Result      *agentcore.Result
	Deleted     bool
	Error       string
	PoolInUse   int
	PoolWaits   int64
	PoolWaitNS  int64
	LockWaitNS  int64
	LockHoldNS  int64
	ElapsedNS   int64
	PoolHealthy bool
}

// The worker is a separate copy of this test binary, not another goroutine or
// an in-process Service. Its only inputs are a bounded JSON request on stdin and
// explicit start/continue commands. FD 3 is separate from application logging.
func TestMain(m *testing.M) {
	if os.Getenv(workerEnv) == "1" {
		os.Exit(workerMain())
	}
	os.Exit(m.Run())
}

type workerProtocol struct {
	input  *json.Decoder
	output *json.Encoder
	pause  string
}

func (p *workerProtocol) event(e workerEvent) error {
	e.PID = os.Getpid()
	return p.output.Encode(e)
}

func (p *workerProtocol) command(want string) error {
	var got string
	if err := p.input.Decode(&got); err != nil || got != want {
		return errors.New("worker command missing or out of order")
	}
	return nil
}

func (p *workerProtocol) barrier(stage string) error {
	if err := p.event(workerEvent{Stage: stage}); err != nil {
		return err
	}
	if p.pause == stage {
		return p.command("continue")
	}
	return nil
}

type observedStore struct {
	memory.ConversationStore
	protocol *workerProtocol
	waitNS   int64
	holdNS   int64
}

func (s *observedStore) WithConversationLock(ctx context.Context, user, conversation string, fn func(context.Context) error) error {
	if err := s.protocol.barrier("acquiring"); err != nil {
		return err
	}
	started := time.Now()
	var acquired time.Time
	err := s.ConversationStore.WithConversationLock(ctx, user, conversation, func(locked context.Context) error {
		acquired = time.Now()
		if err := s.protocol.barrier("locked"); err != nil {
			return err
		}
		return fn(locked)
	})
	if acquired.IsZero() {
		s.waitNS = time.Since(started).Nanoseconds()
	} else {
		s.waitNS, s.holdNS = acquired.Sub(started).Nanoseconds(), time.Since(acquired).Nanoseconds()
	}
	return err
}

func (s *observedStore) SaveTurn(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	if err := s.protocol.barrier("before-save"); err != nil {
		return memory.Conversation{}, memory.Turn{}, err
	}
	c, turn, err := s.ConversationStore.SaveTurn(ctx, req)
	if err == nil {
		err = s.protocol.barrier("after-save")
	}
	return c, turn, err
}

type fixedAgent struct {
	protocol *workerProtocol
	label    string
	calls    int
}

func (*fixedAgent) Name() string { return "storage-acceptance-fixed-agent" }

func (a *fixedAgent) Run(_ context.Context, in agentcore.Input) (*agentcore.Result, error) {
	a.calls++
	if err := a.protocol.event(workerEvent{Stage: "model", Prior: in.PriorIntent}); err != nil {
		return nil, err
	}
	intent := agentcore.Intent{BudgetCents: in.BudgetCents, MaxItems: in.MaxItems, Keywords: []string{"键盘"}}
	if in.PriorIntent != nil {
		if intent.BudgetCents == 0 {
			intent.BudgetCents = in.PriorIntent.BudgetCents
		}
		if intent.MaxItems == 0 {
			intent.MaxItems = in.PriorIntent.MaxItems
		}
	}
	return &agentcore.Result{Intent: intent, Summary: "synthetic-" + a.label}, nil
}

func workerMain() int {
	out := os.NewFile(3, "acceptance-events")
	if out == nil {
		return 2
	}
	defer out.Close()
	p := &workerProtocol{input: json.NewDecoder(io.LimitReader(os.Stdin, 32*1024)), output: json.NewEncoder(out)}
	fail := func() int {
		_ = p.event(workerEvent{Stage: "setup-error", Error: "worker setup failed (details suppressed)"})
		return 2
	}
	var req workerRequest
	if p.input.Decode(&req) != nil || req.TimeoutMS < 100 || req.TimeoutMS > 15000 {
		return fail()
	}
	p.pause = req.Pause
	setup, cancelSetup := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := openStore(setup, req.Target, req.Mode)
	cancelSetup()
	if err != nil {
		return fail()
	}
	defer s.close()
	if p.barrier("ready") != nil || p.command("start") != nil {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	probe := &fixedAgent{protocol: p, label: req.Label}
	observed := &observedStore{ConversationStore: s.store, protocol: p}
	service := recommend.NewService(probe, nil, observed)
	started := time.Now()
	done := workerEvent{Stage: "done"}
	if req.Delete {
		done.Deleted, err = service.DeleteConversation(ctx, req.Input.UserId, req.Input.ConversationId)
	} else {
		done.Result, err = service.Recommend(ctx, req.Input)
	}
	done.ElapsedNS, done.Calls = time.Since(started).Nanoseconds(), probe.calls
	done.LockWaitNS, done.LockHoldNS = observed.waitNS, observed.holdNS
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), ctx.Err() != nil:
			done.Error = "canceled"
		case errors.Is(err, memory.ErrConversationLeaseLost):
			done.Error = "lease-lost"
		default:
			done.Error = "operation-failed"
		}
	}
	// Inspect after Service has returned (including its lock cleanup), and use
	// an independent deadline to prove a canceled request did not strand the pool.
	health, cancelHealth := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelHealth()
	if s.pool != nil {
		stats := s.pool.Stats()
		done.PoolInUse, done.PoolWaits, done.PoolWaitNS = stats.InUse, stats.WaitCount, stats.WaitDuration.Nanoseconds()
		done.PoolHealthy = s.pool.PingContext(health) == nil
	} else {
		stats := s.rdb.PoolStats()
		done.PoolInUse = int(stats.TotalConns - stats.IdleConns)
		done.PoolWaits, done.PoolWaitNS = -1, -1 // SQL-only measurements, not zero Redis waiting.
		done.PoolHealthy = s.rdb.Ping(health).Err() == nil
	}
	if p.event(done) != nil {
		return 2
	}
	return 0
}

type childProcess struct {
	cmd     *exec.Cmd
	input   *json.Encoder
	events  <-chan workerEvent
	exited  <-chan error
	history []workerEvent
	waited  bool
}

func startChild(t *testing.T, req workerRequest) *childProcess {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("acceptance worker uses an inherited Unix file descriptor")
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 12000
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	cmd := exec.CommandContext(ctx, binary)
	// Do not inherit deployment secrets, test DSNs, PG* defaults or model config.
	cmd.Env = []string{workerEnv + "=1", "GOMAXPROCS=2", "GORACE=atexit_sleep_ms=0"}
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	cmd.ExtraFiles = []*os.File{writer}
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	p := &childProcess{cmd: cmd, input: json.NewEncoder(stdin)}
	events := make(chan workerEvent, 32)
	exited := make(chan error, 1)
	p.events, p.exited = events, exited
	t.Cleanup(func() {
		cancel()
		_ = stdin.Close()
		_ = writer.Close()
		_ = reader.Close()
		if cmd.Process != nil && !p.waited {
			_ = cmd.Process.Kill()
			<-exited
			p.waited = true
		}
	})
	require.NoError(t, cmd.Start())
	_ = writer.Close()
	go func() { exited <- cmd.Wait() }()
	go func() {
		defer close(events)
		decoder := json.NewDecoder(reader)
		for {
			var event workerEvent
			if decoder.Decode(&event) != nil {
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	require.NoError(t, p.input.Encode(req))
	p.until(t, "ready")
	p.send(t, "start")
	return p
}

func (p *childProcess) send(t *testing.T, command string) {
	t.Helper()
	require.NoError(t, p.input.Encode(command))
}

func (p *childProcess) until(t *testing.T, stage string) workerEvent {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-p.events:
			require.True(t, ok, "worker exited before %s; stages=%v", stage, p.stages())
			require.Equal(t, p.cmd.Process.Pid, event.PID)
			require.NotEqual(t, os.Getpid(), event.PID)
			p.history = append(p.history, event)
			if event.Stage == stage {
				return event
			}
			require.NotContains(t, []string{"done", "setup-error"}, event.Stage, "premature worker termination; stages=%v", p.stages())
		case <-timer.C:
			t.Fatalf("worker timed out before %s; stages=%v", stage, p.stages())
		}
	}
}

func (p *childProcess) stages() []string {
	stages := make([]string, 0, len(p.history))
	for _, e := range p.history {
		stages = append(stages, e.Stage)
	}
	return stages
}

func (p *childProcess) done(t *testing.T) workerEvent {
	t.Helper()
	done := p.until(t, "done")
	err := <-p.exited
	p.waited = true
	require.NoError(t, err)
	require.True(t, done.PoolHealthy, "pool must remain usable after request/lock cleanup")
	require.Zero(t, done.PoolInUse, "dedicated connection was not returned")
	t.Logf("worker pid=%d calls=%d elapsed_ns=%d lock_wait_ns=%d lock_hold_ns=%d sql_pool_waits=%d sql_pool_wait_ns=%d error=%q",
		done.PID, done.Calls, done.ElapsedNS, done.LockWaitNS, done.LockHoldNS, done.PoolWaits, done.PoolWaitNS, done.Error)
	return done
}

func (p *childProcess) kill(t *testing.T) {
	t.Helper()
	require.NoError(t, p.cmd.Process.Kill())
	err := <-p.exited
	p.waited = true
	require.Error(t, err, "killed worker unexpectedly exited normally")
}
