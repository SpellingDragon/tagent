package execution

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestLongExecutionRenewsLease checks that a long execution renews the lease it holds through the formal entry.
// - one admitted exec_run call drives the production execution path and the execution behind it takes longer than its declared lease
// - the lease is renewed while the execution runs, so a sweep over the whole window of the run reaps nothing
// - the sweep window outlasts the declared lease, so a lease nobody renews reaches its deadline inside it
// - the resource stays held after the run settled, beside the renewals counted on it
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func TestLongExecutionRenewsLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	executor := newLeaseExecutor()
	manager := tagentexec.NewManager(executor)
	provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(convCall("call-long", "exec_run",
				`{"command":"download","args":["87mb"],"kind":"proc","resource":"long-run","ttl":"600ms","monitor":{"interval":"20ms"}}`))
		}
		return convFinalTurn("the long execution settled")
	}}
	run, err := tagent.Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tagentexec.NewRunTool(manager)}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	type drive struct {
		turn agent.Turn
		err  error
	}
	driven := make(chan drive, 1)
	go func() {
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-long-run",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-long-run"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "download the archive"}},
		})
		driven <- drive{turn: turn, err: err}
	}()

	select {
	case <-executor.entered:
	case <-ctx.Done():
		t.Fatalf("the execution path never reached the running execution: %v", ctx.Err())
	}

	reaped := make([]tagentexec.SweepReport, 0, 1)
	window := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(window) {
		reaped = append(reaped, manager.Sweep(ctx)...)
		if held := manager.Held(); held != 1 {
			t.Fatalf("held during the long execution = %d, want the one resource the running call holds", held)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if len(reaped) != 0 {
		t.Fatalf("sweep reports during the long execution = %+v, want no lease reaped while the call renews it inside a window longer than its declared lease", reaped)
	}

	close(executor.gate)
	var result drive
	select {
	case result = <-driven:
	case <-ctx.Done():
		t.Fatalf("the long execution never settled: %v", ctx.Err())
	}
	if result.err != nil {
		t.Fatalf("invoke the long execution: %v", result.err)
	}
	if result.turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed terminal of the settled run", result.turn.Terminal)
	}

	key := tagentexec.ResourceKey{Kind: "proc", Key: "long-run"}
	explanation, err := manager.Explain(key)
	if err != nil {
		t.Fatalf("explain the long execution: %v", err)
	}
	if explanation.Renewed < 1 {
		t.Fatalf("renewals of the long execution = %d, want the lease renewed while the execution ran", explanation.Renewed)
	}
	if !explanation.Deadline.After(time.Now()) {
		t.Fatalf("deadline after the run = %v, want the renewed lease still ahead of the clock", explanation.Deadline)
	}
	if manager.Held() != 1 || manager.Released() != 0 || executor.stops != 0 {
		t.Fatalf("accounting after the run = (held %d, released %d, stops %d), want the resource kept beside no stop",
			manager.Held(), manager.Released(), executor.stops)
	}
	if reports := manager.Sweep(ctx); len(reports) != 0 {
		t.Fatalf("sweep reports after the run = %+v, want the renewed lease reaping nothing", reports)
	}
}

// TestRenewRefusesExpiredLease checks that a renewal of an already expired lease is refused by name.
// - a renewal inside the lease extends it and is counted once
// - a renewal after the deadline is refused beside ErrLeaseExpired and leaves the lease where it stood
// - an expired lease is settled by the explicit stop handling of a sweep, and a key nothing is held under keeps its own refusal
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func TestRenewRefusesExpiredLease(t *testing.T) {
	ctx := context.Background()
	executor := newLeaseExecutor()
	clock := time.Now()
	manager := tagentexec.NewManager(executor, tagentexec.WithClock(func() time.Time { return clock }))
	key, err := manager.Acquire(ctx, leaseDecl(), tagentexec.WithTTL(time.Minute))
	if err != nil {
		t.Fatalf("acquire the execution resource: %v", err)
	}

	clock = clock.Add(30 * time.Second)
	if err := manager.Renew(ctx, key, time.Minute); err != nil {
		t.Fatalf("renew the lease inside its window: %v", err)
	}
	renewed, err := manager.Explain(key)
	if err != nil || renewed.Renewed != 1 || !renewed.Deadline.Equal(clock.Add(time.Minute)) {
		t.Fatalf("explanation after the renewal = (%+v, %v), want the lease extended once to the fresh deadline", renewed, err)
	}

	clock = clock.Add(2 * time.Minute)
	if err := manager.Renew(ctx, key, time.Minute); !errors.Is(err, tagentexec.ErrLeaseExpired) {
		t.Fatalf("renewal after the deadline = %v, want %v", err, tagentexec.ErrLeaseExpired)
	}
	expired, err := manager.Explain(key)
	if err != nil || expired.Renewed != 1 || !expired.Deadline.Equal(renewed.Deadline) {
		t.Fatalf("explanation after the refused renewal = (%+v, %v), want the expired lease left where it stood", expired, err)
	}
	if !errors.Is(manager.Renew(ctx, key, -time.Second), tagentexec.ErrLeaseInvalid) {
		t.Fatalf("negative renewal lease = %v, want %v", manager.Renew(ctx, key, -time.Second), tagentexec.ErrLeaseInvalid)
	}
	if err := manager.Renew(ctx, tagentexec.ResourceKey{Kind: "proc", Key: "unknown"}, time.Minute); !errors.Is(err, tagentexec.ErrResourceUnknown) {
		t.Fatalf("renewal of an unheld key = %v, want %v", err, tagentexec.ErrResourceUnknown)
	}

	reports := manager.Sweep(ctx)
	if len(reports) != 1 || !reports[0].Expired || !reports[0].Evidence.Released {
		t.Fatalf("sweep reports = %+v, want the expired lease settled by the explicit stop it drives", reports)
	}
	if manager.Held() != 0 || manager.Released() != 1 || executor.stops != 1 {
		t.Fatalf("accounting after the sweep = (held %d, released %d, stops %d), want the expiry deciding nothing before the confirmed stop",
			manager.Held(), manager.Released(), executor.stops)
	}
}

// leaseDecl returns the execution declaration these checks run under.
func leaseDecl() tagentexec.ExecutionDecl {
	return tagentexec.ExecutionDecl{
		Command:  "download",
		Args:     []string{"87mb"},
		Resource: tagentexec.ResourceKey{Kind: "proc", Key: "long-run"},
	}
}

// leaseExecutor is the deterministic execution face of these checks: it reports
// exactly what it observed about the resources it owns, and it holds one capture
// open until the check opens the gate, so a run longer than its lease is driven
// through the real execution path instead of a fabricated one.
type leaseExecutor struct {
	mu      sync.Mutex
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
	live    map[tagentexec.ResourceKey]bool
	starts  int
	stops   int
}

// newLeaseExecutor returns the fake execution face with its capture gate closed.
func newLeaseExecutor() *leaseExecutor {
	return &leaseExecutor{
		entered: make(chan struct{}),
		gate:    make(chan struct{}),
		live:    map[tagentexec.ResourceKey]bool{},
	}
}

// Start reports the resource it now owns under the declared key.
func (e *leaseExecutor) Start(_ context.Context, decl tagentexec.ExecutionDecl) (tagentexec.ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	e.live[decl.Resource] = true
	return decl.Resource, nil
}

// Probe reports the resource observed alive while it is owned.
func (e *leaseExecutor) Probe(_ context.Context, key tagentexec.ResourceKey) (tagentexec.ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.live[key] {
		return tagentexec.ProbeResult{State: tagentexec.ProbeDead, Detail: "no session is owned under the key"}, nil
	}
	return tagentexec.ProbeResult{State: tagentexec.ProbeAlive}, nil
}

// Capture signals that the execution is running and answers with its frame once
// the check opens the gate, or once the caller stops waiting.
func (e *leaseExecutor) Capture(ctx context.Context, _ tagentexec.ResourceKey) ([]byte, error) {
	e.once.Do(func() { close(e.entered) })
	select {
	case <-e.gate:
		return []byte(strings.Repeat("frame;", 2)), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stop reports the stop it observed for the resource behind the key.
func (e *leaseExecutor) Stop(_ context.Context, key tagentexec.ResourceKey) (tagentexec.StopEvidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	evidence := tagentexec.StopEvidence{Resource: key}
	if !e.live[key] {
		evidence.Reason = "no session is owned under the key"
		return evidence, nil
	}
	delete(e.live, key)
	e.stops++
	evidence.Confirmed = true
	return evidence, nil
}
