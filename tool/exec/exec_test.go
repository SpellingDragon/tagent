package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestLeaseLifecycleKeepsOneHolderFromAcquireToSweep pins the lease state machine: one holder per key, renewal inside the window, expiry decided only by a sweep.
// - acquiring the same key twice starts one execution and reports the second acquisition as reused
// - the lease deadline is the acquisition time plus the declared ttl, and the manager floor applies when no ttl is declared
// - a renewal moves the deadline to its own call time plus the declared span and is visible in Explain
// - an expired lease refuses renewal and stays held until a sweep drives its stop
// - a sweep stops every expired lease in key order and reports the confirmed release
// - a sweep leaves an unexpired resource held and out of its reports
// - releasing a freed key is refused by name, and so are observe, capture, renew and explain over it
// - a negative ttl is refused by name on acquisition and on renewal
// - a declaration naming no key is refused by name before any execution starts
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestLeaseLifecycleKeepsOneHolderFromAcquireToSweep(t *testing.T) {
	ctx := context.Background()

	t.Run("one execution per key", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		decl := testExecutionDecl("one")
		first, err := manager.Acquire(ctx, decl, WithTTL(time.Minute))
		if err != nil {
			t.Fatalf("first acquisition = %v, want a held key", err)
		}
		if first != decl.Resource {
			t.Fatalf("first acquisition = %s, want %s", first, decl.Resource)
		}
		second, err := manager.Acquire(ctx, decl, WithTTL(time.Minute))
		if err != nil {
			t.Fatalf("second acquisition = %v, want the held key", err)
		}
		if second != first {
			t.Fatalf("second acquisition = %s, want %s", second, first)
		}
		if got := backend.startCount(); got != 1 {
			t.Fatalf("backend started %d executions, want 1", got)
		}
		if got := manager.Reused(); got != 1 {
			t.Fatalf("reused = %d, want 1", got)
		}
		if got := manager.Held(); got != 1 {
			t.Fatalf("held = %d, want 1", got)
		}
		if got := backend.declarationAt(t, 0); got.Command != decl.Command || got.Dir != decl.Dir || len(got.Args) != len(decl.Args) {
			t.Fatalf("backend started under %+v, want %+v", got, decl)
		}
	})

	t.Run("deadline from acquisition and floor", func(t *testing.T) {
		manager, _, clock := newLeaseTestManager(t)
		decl := testExecutionDecl("deadline")
		if _, err := manager.Acquire(ctx, decl, WithTTL(90*time.Second)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		explanation, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if want := clock.Now().Add(90 * time.Second); !explanation.Deadline.Equal(want) {
			t.Fatalf("deadline = %s, want %s", explanation.Deadline, want)
		}

		floorManager, _, floorClock := newLeaseTestManager(t, WithDefaultTTL(2*time.Minute))
		floorDecl := testExecutionDecl("floor")
		if _, err := floorManager.Acquire(ctx, floorDecl); err != nil {
			t.Fatalf("acquisition under the manager floor = %v", err)
		}
		floor, err := floorManager.Explain(floorDecl.Resource)
		if err != nil {
			t.Fatalf("Explain under the manager floor = %v", err)
		}
		if want := floorClock.Now().Add(2 * time.Minute); !floor.Deadline.Equal(want) {
			t.Fatalf("deadline under the manager floor = %s, want %s", floor.Deadline, want)
		}

		defaultManager, _, defaultClock := newLeaseTestManager(t)
		defaultDecl := testExecutionDecl("default")
		if _, err := defaultManager.Acquire(ctx, defaultDecl); err != nil {
			t.Fatalf("acquisition under the default floor = %v", err)
		}
		observed, err := defaultManager.Explain(defaultDecl.Resource)
		if err != nil {
			t.Fatalf("Explain under the default floor = %v", err)
		}
		if want := defaultClock.Now().Add(DefaultLeaseTTL); !observed.Deadline.Equal(want) {
			t.Fatalf("deadline under the default floor = %s, want %s", observed.Deadline, want)
		}
	})

	t.Run("renewal moves the deadline", func(t *testing.T) {
		manager, _, clock := newLeaseTestManager(t)
		decl := testExecutionDecl("renew")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		clock.Advance(20 * time.Second)
		if err := manager.Renew(ctx, decl.Resource, 2*time.Minute); err != nil {
			t.Fatalf("renewal = %v", err)
		}
		explanation, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if want := clock.Now().Add(2 * time.Minute); !explanation.Deadline.Equal(want) {
			t.Fatalf("renewed deadline = %s, want %s", explanation.Deadline, want)
		}
		if explanation.Renewed != 1 {
			t.Fatalf("renewed count = %d, want 1", explanation.Renewed)
		}
	})

	t.Run("expiry holds until the sweep", func(t *testing.T) {
		manager, backend, clock := newLeaseTestManager(t)
		decl := testExecutionDecl("expired")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		before, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain before expiry = %v", err)
		}
		clock.Advance(2 * time.Minute)
		if err := manager.Renew(ctx, decl.Resource, time.Minute); !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("renewal over an expired lease = %v, want ErrLeaseExpired", err)
		}
		after, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain after the refused renewal = %v", err)
		}
		if !after.Deadline.Equal(before.Deadline) {
			t.Fatalf("refused renewal moved the deadline from %s to %s", before.Deadline, after.Deadline)
		}
		if manager.Held() != 1 {
			t.Fatalf("held after expiry = %d, want the resource still held", manager.Held())
		}
		if got := backend.stopCount(); got != 0 {
			t.Fatalf("expiry drove %d stops, want none before the sweep", got)
		}

		freshDecl := testExecutionDecl("fresh")
		if _, err := manager.Acquire(ctx, freshDecl, WithTTL(10*time.Minute)); err != nil {
			t.Fatalf("acquisition of an unexpired resource = %v", err)
		}
		clock.Advance(time.Minute)
		reports := manager.Sweep(ctx)
		if len(reports) != 1 {
			t.Fatalf("sweep reported %d resources, want only the expired one: %+v", len(reports), reports)
		}
		report := reports[0]
		if report.Key != decl.Resource {
			t.Fatalf("sweep reported %s, want %s", report.Key, decl.Resource)
		}
		if !report.Expired || !report.Evidence.Released || !report.Evidence.Stop.Confirmed {
			t.Fatalf("sweep report = %+v, want an expired confirmed release", report)
		}
		if report.Evidence.Stop.ExitCode != testExitCode {
			t.Fatalf("released exit code = %d, want %d", report.Evidence.Stop.ExitCode, testExitCode)
		}
		if manager.Held() != 1 || manager.Released() != 1 {
			t.Fatalf("held=%d released=%d, want the fresh resource held and one release", manager.Held(), manager.Released())
		}
		if _, err := manager.Explain(freshDecl.Resource); err != nil {
			t.Fatalf("the unexpired resource left the table: %v", err)
		}
	})

	t.Run("sweep stops expired leases in key order", func(t *testing.T) {
		manager, _, clock := newLeaseTestManager(t)
		for _, key := range []string{"b", "a", "c"} {
			if _, err := manager.Acquire(ctx, testExecutionDecl(key), WithTTL(time.Second)); err != nil {
				t.Fatalf("acquisition of %q = %v", key, err)
			}
		}
		clock.Advance(2 * time.Second)
		reports := manager.Sweep(ctx)
		if len(reports) != 3 {
			t.Fatalf("sweep reported %d resources, want 3", len(reports))
		}
		for i, want := range []string{"test/a", "test/b", "test/c"} {
			if got := reports[i].Key.String(); got != want {
				t.Fatalf("sweep report %d = %s, want %s", i, got, want)
			}
		}
	})

	t.Run("freed and unknown keys are named", func(t *testing.T) {
		manager, _, clock := newLeaseTestManager(t)
		decl := testExecutionDecl("freed")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		clock.Advance(2 * time.Minute)
		if reports := manager.Sweep(ctx); len(reports) != 1 || !reports[0].Evidence.Released {
			t.Fatalf("sweep = %+v, want one confirmed release", reports)
		}
		evidence, err := manager.Release(ctx, decl.Resource)
		if !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("release of a freed key = %v, want ErrResourceUnknown", err)
		}
		if evidence.Resource != decl.Resource || evidence.Released {
			t.Fatalf("release evidence = %+v, want the key beside an unreleased result", evidence)
		}
		if err := manager.Renew(ctx, decl.Resource, time.Minute); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("renewal over a freed key = %v, want ErrResourceUnknown", err)
		}
		if _, err := manager.Observe(ctx, decl.Resource); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("observe over a freed key = %v, want ErrResourceUnknown", err)
		}
		if _, err := manager.Capture(ctx, decl.Resource); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("capture over a freed key = %v, want ErrResourceUnknown", err)
		}
		if _, err := manager.Explain(decl.Resource); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("explain over a freed key = %v, want ErrResourceUnknown", err)
		}
	})

	t.Run("invalid and missing declarations", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		decl := testExecutionDecl("invalid")
		if _, err := manager.Acquire(ctx, decl, WithTTL(-time.Second)); !errors.Is(err, ErrLeaseInvalid) {
			t.Fatalf("acquisition over a negative ttl = %v, want ErrLeaseInvalid", err)
		}
		if _, err := manager.Acquire(ctx, ExecutionDecl{}); !errors.Is(err, ErrKeyMissing) {
			t.Fatalf("acquisition naming no key = %v, want ErrKeyMissing", err)
		}
		if got := backend.startCount(); got != 0 {
			t.Fatalf("backend started %d executions for refused acquisitions, want 0", got)
		}
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		if err := manager.Renew(ctx, decl.Resource, -time.Second); !errors.Is(err, ErrLeaseInvalid) {
			t.Fatalf("renewal over a negative ttl = %v, want ErrLeaseInvalid", err)
		}
	})
}

// TestLeaseRetentionFollowsStopEvidence pins that retention is decided by what the executor observed, never by what the caller requested.
// - an unconfirmed stop keeps the resource held with its obligations and reports the reason
// - an acquisition after an unconfirmed stop is refused by name instead of opening a second execution
// - a confirmed stop frees the resource and reports the release beside its evidence
// - a stop error keeps the resource held and puts the error on the release evidence
// - release drives the stop under a context detached from a cancelled caller context
// - undecidable probes extend the unknown run and escalate without freeing, and a decisive probe resets it
// - adoption of a live resource binds a lease without starting one, an undecidable probe refuses it, and a held key refuses it
// - an unavailable capability refuses acquisition beside the degrade error and starts nothing
// - the monitor rhythm of a refused or an accepted call travels with it
// - concurrent acquisitions of one key start one execution and both answer the same key
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestLeaseRetentionFollowsStopEvidence(t *testing.T) {
	ctx := context.Background()

	t.Run("unconfirmed stop retains the resource", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		backend.stopFn = func(context.Context) (StopEvidence, error) {
			return StopEvidence{Confirmed: false, Reason: "the process ignored the signal"}, nil
		}
		decl := testExecutionDecl("retained")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		evidence, err := manager.Release(ctx, decl.Resource)
		if err != nil {
			t.Fatalf("release beside an unconfirmed stop = %v, want no error", err)
		}
		if evidence.Released || evidence.Reason != "the process ignored the signal" {
			t.Fatalf("release evidence = %+v, want a retained resource beside its reason", evidence)
		}
		if manager.Held() != 1 {
			t.Fatalf("held = %d, want the resource retained", manager.Held())
		}
		explanation, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if len(explanation.Obligations) == 0 {
			t.Fatalf("retained resource carries no obligations: %+v", explanation)
		}
		if explanation.LastStop == nil || explanation.LastStop.Confirmed {
			t.Fatalf("last stop = %+v, want the unconfirmed observation recorded", explanation.LastStop)
		}

		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); !errors.Is(err, ErrResourceHeld) {
			t.Fatalf("acquisition after an unconfirmed stop = %v, want ErrResourceHeld", err)
		}
		if got := backend.startCount(); got != 1 {
			t.Fatalf("backend started %d executions, want the refused acquisition to start none", got)
		}

		backend.stopFn = func(context.Context) (StopEvidence, error) {
			return StopEvidence{Confirmed: true, ExitCode: 3}, nil
		}
		evidence, err = manager.Release(ctx, decl.Resource)
		if err != nil {
			t.Fatalf("release beside a confirmed stop = %v", err)
		}
		if !evidence.Released || evidence.Stop.ExitCode != 3 {
			t.Fatalf("confirmed release evidence = %+v, want the freed resource beside exit 3", evidence)
		}
		if manager.Held() != 0 || manager.Released() != 1 {
			t.Fatalf("held=%d released=%d, want the resource freed once", manager.Held(), manager.Released())
		}
	})

	t.Run("stop error retains the resource", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		stopFailed := errors.New("the terminal multiplexer refused the kill")
		backend.stopFn = func(context.Context) (StopEvidence, error) {
			return StopEvidence{Confirmed: false}, stopFailed
		}
		decl := testExecutionDecl("stop-error")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		evidence, err := manager.Release(ctx, decl.Resource)
		if !errors.Is(err, stopFailed) {
			t.Fatalf("release beside a stop error = %v, want the executor error", err)
		}
		if evidence.Released || evidence.Reason != stopFailed.Error() {
			t.Fatalf("release evidence = %+v, want a retained resource beside the error", evidence)
		}
		if manager.Held() != 1 {
			t.Fatalf("held = %d, want the resource retained", manager.Held())
		}
	})

	t.Run("release detaches the stop from a cancelled caller", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		var observed error
		backend.stopFn = func(stopCtx context.Context) (StopEvidence, error) {
			observed = stopCtx.Err()
			return StopEvidence{Confirmed: true}, nil
		}
		decl := testExecutionDecl("detached")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		evidence, err := manager.Release(cancelled, decl.Resource)
		if err != nil {
			t.Fatalf("release under a cancelled caller = %v", err)
		}
		if observed != nil {
			t.Fatalf("the stop observed a cancelled context: %v", observed)
		}
		if !evidence.Released || manager.Held() != 0 {
			t.Fatalf("release evidence = %+v, held = %d, want a confirmed release", evidence, manager.Held())
		}
	})

	t.Run("unknown probes escalate and never free", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		backend.probeFn = func() (ProbeResult, error) {
			return ProbeResult{State: ProbeUnknown, Detail: "the probe command timed out"}, nil
		}
		decl := testExecutionDecl("unknown")
		if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		for i := 1; i <= 2; i++ {
			observed, err := manager.Observe(ctx, decl.Resource)
			if err != nil {
				t.Fatalf("observe %d = %v", i, err)
			}
			if observed.State != ProbeUnknown {
				t.Fatalf("observe %d reported %v, want ProbeUnknown", i, observed.State)
			}
			explanation, err := manager.Explain(decl.Resource)
			if err != nil {
				t.Fatalf("Explain after observe %d = %v", i, err)
			}
			if explanation.ConsecutiveUnknowns != i || explanation.Escalations != i {
				t.Fatalf("after observe %d: unknowns=%d escalations=%d", i, explanation.ConsecutiveUnknowns, explanation.Escalations)
			}
		}
		if manager.Escalated() != 2 {
			t.Fatalf("manager escalations = %d, want 2", manager.Escalated())
		}
		if manager.Held() != 1 {
			t.Fatalf("undecidable probes freed the resource: held = %d", manager.Held())
		}
		backend.probeFn = func() (ProbeResult, error) { return ProbeResult{State: ProbeAlive}, nil }
		if _, err := manager.Observe(ctx, decl.Resource); err != nil {
			t.Fatalf("decisive observe = %v", err)
		}
		explanation, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain after a decisive probe = %v", err)
		}
		if explanation.ConsecutiveUnknowns != 0 || explanation.Escalations != 2 {
			t.Fatalf("after a decisive probe: unknowns=%d escalations=%d, want 0 and 2", explanation.ConsecutiveUnknowns, explanation.Escalations)
		}
	})

	t.Run("adoption binds only an observed live resource", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		backend.probeFn = func() (ProbeResult, error) { return ProbeResult{State: ProbeAlive}, nil }
		adopted := ResourceKey{Kind: "test", Key: "adopted"}
		if err := manager.Adopt(ctx, adopted, WithTTL(time.Minute)); err != nil {
			t.Fatalf("adoption = %v", err)
		}
		if manager.Adopted() != 1 || backend.startCount() != 0 || manager.Held() != 1 {
			t.Fatalf("adopted=%d started=%d held=%d, want one adoption and no start", manager.Adopted(), backend.startCount(), manager.Held())
		}
		explanation, err := manager.Explain(adopted)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if !explanation.Adopted {
			t.Fatalf("explanation = %+v, want an adopted resource", explanation)
		}
		if err := manager.Adopt(ctx, adopted, WithTTL(time.Minute)); !errors.Is(err, ErrResourceHeld) {
			t.Fatalf("adoption over a held key = %v, want ErrResourceHeld", err)
		}
		if err := manager.Adopt(ctx, adopted, WithTTL(-time.Second)); !errors.Is(err, ErrLeaseInvalid) {
			t.Fatalf("adoption over a negative ttl = %v, want ErrLeaseInvalid", err)
		}

		unknownManager, unknownBackend, _ := newLeaseTestManager(t)
		unknownBackend.probeFn = func() (ProbeResult, error) {
			return ProbeResult{State: ProbeUnknown, Detail: "no answer"}, nil
		}
		undecided := ResourceKey{Kind: "test", Key: "undecided"}
		if err := unknownManager.Adopt(ctx, undecided, WithTTL(time.Minute)); !errors.Is(err, ErrAdoptUnconfirmed) {
			t.Fatalf("adoption beside an undecidable probe = %v, want ErrAdoptUnconfirmed", err)
		}
		if unknownManager.Held() != 0 {
			t.Fatalf("refused adoption held %d resources, want none", unknownManager.Held())
		}
	})

	t.Run("availability refusal carries the rhythm", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t, WithAvailability(func(ResourceKey) bool { return false }))
		rhythm := MonitorConfig{Interval: time.Second, StableDuration: 2 * time.Second}
		decl := testExecutionDecl("degraded")
		_, err := manager.Acquire(ctx, decl, WithMonitorConfig(rhythm))
		var degraded *DegradeError
		if !errors.As(err, &degraded) {
			t.Fatalf("acquisition beside a missing capability = %v, want *DegradeError", err)
		}
		if !errors.Is(err, ErrTmuxUnavailable) {
			t.Fatalf("degrade refusal = %v, want it to name the missing capability", err)
		}
		if degraded.Key != decl.Resource || degraded.Rhythm != rhythm {
			t.Fatalf("degrade refusal = %+v, want key %s beside the call rhythm %+v", degraded, decl.Resource, rhythm)
		}
		if backend.startCount() != 0 {
			t.Fatalf("the refused acquisition started %d executions, want none", backend.startCount())
		}
	})

	t.Run("accepted call keeps its rhythm", func(t *testing.T) {
		manager, _, _ := newLeaseTestManager(t)
		rhythm := MonitorConfig{Interval: 4 * time.Second, FakeDeadDuration: 9 * time.Second}
		decl := testExecutionDecl("rhythm")
		if _, err := manager.Acquire(ctx, decl, WithMonitorConfig(rhythm)); err != nil {
			t.Fatalf("acquisition = %v", err)
		}
		explanation, err := manager.Explain(decl.Resource)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if explanation.Rhythm != rhythm {
			t.Fatalf("rhythm = %+v, want the call rhythm %+v", explanation.Rhythm, rhythm)
		}
	})

	t.Run("concurrent acquisitions share one execution", func(t *testing.T) {
		manager, backend, _ := newLeaseTestManager(t)
		decl := testExecutionDecl("concurrent")
		const callers = 8
		var wg sync.WaitGroup
		keys := make(chan ResourceKey, callers)
		errs := make(chan error, callers)
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				key, err := manager.Acquire(ctx, decl, WithTTL(time.Minute))
				keys <- key
				errs <- err
			}()
		}
		wg.Wait()
		close(keys)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent acquisition = %v", err)
			}
		}
		for key := range keys {
			if key != decl.Resource {
				t.Fatalf("concurrent acquisition answered %s, want %s", key, decl.Resource)
			}
		}
		if got := backend.startCount(); got != 1 {
			t.Fatalf("backend started %d executions, want 1", got)
		}
		if got := manager.Reused(); got != callers-1 {
			t.Fatalf("reused = %d, want %d", got, callers-1)
		}
	})
}

const testExitCode = 7

type scriptedExecutor struct {
	mu        sync.Mutex
	starts    int
	decls     []ExecutionDecl
	probeFn   func() (ProbeResult, error)
	stopFn    func(context.Context) (StopEvidence, error)
	captureFn func() ([]byte, error)
	stops     int
}

func (e *scriptedExecutor) Start(_ context.Context, decl ExecutionDecl) (ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	e.decls = append(e.decls, decl)
	return decl.Resource, nil
}

func (e *scriptedExecutor) Probe(_ context.Context, _ ResourceKey) (ProbeResult, error) {
	if e.probeFn != nil {
		return e.probeFn()
	}
	return ProbeResult{State: ProbeAlive}, nil
}

func (e *scriptedExecutor) Capture(_ context.Context, _ ResourceKey) ([]byte, error) {
	if e.captureFn != nil {
		return e.captureFn()
	}
	return []byte("recorded output"), nil
}

func (e *scriptedExecutor) Stop(ctx context.Context, key ResourceKey) (StopEvidence, error) {
	e.mu.Lock()
	e.stops++
	e.mu.Unlock()
	if e.stopFn != nil {
		return e.stopFn(ctx)
	}
	return StopEvidence{Resource: key, Confirmed: true, ExitCode: testExitCode}, nil
}

func (e *scriptedExecutor) startCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts
}

func (e *scriptedExecutor) stopCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stops
}

func (e *scriptedExecutor) declarationAt(t *testing.T, i int) ExecutionDecl {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if i >= len(e.decls) {
		t.Fatalf("backend holds %d declarations, want at least %d", len(e.decls), i+1)
	}
	return e.decls[i]
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newLeaseTestManager(t *testing.T, options ...ManagerOption) (*Manager, *scriptedExecutor, *testClock) {
	t.Helper()
	backend := &scriptedExecutor{}
	clock := newTestClock()
	manager := NewManager(backend, append([]ManagerOption{WithClock(clock.Now)}, options...)...)
	return manager, backend, clock
}

func testExecutionDecl(key string) ExecutionDecl {
	return ExecutionDecl{
		Command:  "run-" + key,
		Args:     []string{"--key", key},
		Dir:      "/tmp",
		Resource: ResourceKey{Kind: "test", Key: key},
	}
}

var _ Executor = (*scriptedExecutor)(nil)

// TestExecRunDeclarationPinsTheResourceShape pins the run declaration's kind and resource to their one legal shape:
// - kind names the only family this host serves, admits the empty kind that selects it, and says so
// - every kind enum value is a kind the executor accepts
// - resource names the session name format with its bound, character set and a legal example
// - the resource pattern and validSessionName agree on every sample name
// - no abstract family or key phrase stands where the legal values belong
// - no description offers a prefixed key such as tmux:local
// - the run declaration the production preset hands the model carries the same entries
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecRunDeclarationPinsTheResourceShape(t *testing.T) {
	declaration := NewRunTool(nil).Declaration()

	t.Run("names_the_only_family", func(t *testing.T) {
		assertExecKindNamesTheOnlyFamily(t, declaration)
	})
	t.Run("names_the_session_name_format", func(t *testing.T) {
		assertExecResourceNamesTheSessionNameFormat(t, declaration)
	})
	t.Run("pattern_agrees_with_the_validator", func(t *testing.T) {
		assertExecResourcePatternAgreesWithValidator(t, declaration)
	})
	t.Run("abstract_phrase_gone", func(t *testing.T) {
		assertExecDescriptionsCarryNoAbstractPhrase(t, declaration)
	})
	t.Run("prefixed_key_hint_absent", func(t *testing.T) {
		assertExecDescriptionsOfferNoPrefixedKey(t, declaration)
	})
	t.Run("production_preset_carries_the_same_text", func(t *testing.T) {
		manager, _, _ := newLeaseTestManager(t)
		assertExecResourceShapePinned(t, execPresetDeclaration(t, manager, 0, "exec_run"))
	})
}

// TestExecReleaseDeclarationPinsTheResourceShape pins the release declaration to the shape the run declaration carries:
// - kind names the only family this host serves, admits the empty kind that selects it, and says so
// - every kind enum value is a kind the executor accepts
// - resource names the session name format with its bound, character set and a legal example
// - the resource pattern and validSessionName agree on every sample name
// - no abstract family or key phrase stands where the legal values belong
// - no description offers a prefixed key such as tmux:local
// - run and release carry one pattern, one enum and one description vocabulary
// - the release declaration the production preset hands the model carries the same entries
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecReleaseDeclarationPinsTheResourceShape(t *testing.T) {
	declaration := NewReleaseTool(nil).Declaration()

	t.Run("names_the_only_family", func(t *testing.T) {
		assertExecKindNamesTheOnlyFamily(t, declaration)
	})
	t.Run("names_the_session_name_format", func(t *testing.T) {
		assertExecResourceNamesTheSessionNameFormat(t, declaration)
	})
	t.Run("pattern_agrees_with_the_validator", func(t *testing.T) {
		assertExecResourcePatternAgreesWithValidator(t, declaration)
	})
	t.Run("abstract_phrase_gone", func(t *testing.T) {
		assertExecDescriptionsCarryNoAbstractPhrase(t, declaration)
	})
	t.Run("prefixed_key_hint_absent", func(t *testing.T) {
		assertExecDescriptionsOfferNoPrefixedKey(t, declaration)
	})
	t.Run("both_declarations_share_one_vocabulary", func(t *testing.T) {
		assertExecDeclarationsShareOneVocabulary(t, NewRunTool(nil).Declaration(), declaration)
	})
	t.Run("production_preset_carries_the_same_text", func(t *testing.T) {
		manager, _, _ := newLeaseTestManager(t)
		assertExecResourceShapePinned(t, execPresetDeclaration(t, manager, 1, "exec_release"))
	})
}

// execPresetDeclaration returns the declaration of one tool the production preset presents.
func execPresetDeclaration(t *testing.T, manager *Manager, index int, name string) *trpctool.Declaration {
	t.Helper()
	tools := NewPreset(manager, MonitorConfig{})
	if len(tools) != 2 {
		t.Fatalf("preset holds %d tools, want the run and the release tool", len(tools))
	}
	declaration := tools[index].Declaration()
	if declaration.Name != name {
		t.Fatalf("preset tool %d is %q, want %q", index, declaration.Name, name)
	}
	return declaration
}

// execResourceEntries returns the kind and resource entries one declaration judges.
func execResourceEntries(t *testing.T, declaration *trpctool.Declaration) (kind, resource *trpctool.Schema) {
	t.Helper()
	if declaration.InputSchema == nil {
		t.Fatalf("%s declares no input schema", declaration.Name)
	}
	kind = declaration.InputSchema.Properties["kind"]
	resource = declaration.InputSchema.Properties["resource"]
	if kind == nil || resource == nil {
		t.Fatalf("%s declares kind=%v resource=%v, want both entries", declaration.Name, kind, resource)
	}
	return kind, resource
}

func assertExecResourceShapePinned(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	assertExecKindNamesTheOnlyFamily(t, declaration)
	assertExecResourceNamesTheSessionNameFormat(t, declaration)
	assertExecResourcePatternAgreesWithValidator(t, declaration)
	assertExecDescriptionsCarryNoAbstractPhrase(t, declaration)
	assertExecDescriptionsOfferNoPrefixedKey(t, declaration)
}

func assertExecKindNamesTheOnlyFamily(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	kind, _ := execResourceEntries(t, declaration)
	if len(kind.Enum) != 2 || kind.Enum[0] != "" || kind.Enum[1] != KindTmux {
		t.Fatalf("%s kind enum = %v, want the empty kind beside [%s]", declaration.Name, kind.Enum, KindTmux)
	}
	for _, value := range kind.Enum {
		if _, err := sessionNameOf(ResourceKey{Kind: fmt.Sprint(value), Key: sessionNameExample}); err != nil {
			t.Fatalf("%s kind enum value %v is refused by the executor: %v", declaration.Name, value, err)
		}
	}
	if !strings.Contains(kind.Description, fmt.Sprint(KindTmux)) {
		t.Fatalf("%s kind description = %q, want it to name %s", declaration.Name, kind.Description, KindTmux)
	}
	if !strings.Contains(kind.Description, "leave it empty") {
		t.Fatalf("%s kind description = %q, want it to say an empty kind selects the default", declaration.Name, kind.Description)
	}
}

func assertExecResourceNamesTheSessionNameFormat(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	_, resource := execResourceEntries(t, declaration)
	for _, want := range []string{fmt.Sprint(tmuxSessionMaxName), sessionNameCharset, sessionNameExample} {
		if !strings.Contains(resource.Description, want) {
			t.Fatalf("%s resource description = %q, want it to carry %q", declaration.Name, resource.Description, want)
		}
	}
}

func assertExecResourcePatternAgreesWithValidator(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	_, resource := execResourceEntries(t, declaration)
	if resource.Pattern == "" {
		t.Fatalf("%s resource entry carries no pattern", declaration.Name)
	}
	pattern, err := regexp.Compile(resource.Pattern)
	if err != nil {
		t.Fatalf("%s resource pattern %q does not compile: %v", declaration.Name, resource.Pattern, err)
	}
	samples := []string{
		sessionNameExample,
		"a",
		strings.Repeat("a", tmuxSessionMaxName),
		strings.Repeat("a", tmuxSessionMaxName+1),
		"",
		"tmux:local",
		"a b",
		"a.b",
		"a/b",
		"a:b",
	}
	legal := 0
	for _, name := range samples {
		matched, valid := pattern.MatchString(name), validSessionName(name)
		if matched != valid {
			t.Fatalf("pattern.MatchString(%q) = %t, validSessionName(%q) = %t, want the same verdict", name, matched, name, valid)
		}
		if valid {
			legal++
		}
	}
	if legal == 0 || legal == len(samples) {
		t.Fatalf("samples decide nothing: %d of %d are legal", legal, len(samples))
	}
}

func assertExecDescriptionsCarryNoAbstractPhrase(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	kind, resource := execResourceEntries(t, declaration)
	for _, phrase := range []string{"the resource family", "canonical key of the execution resource"} {
		for _, description := range []string{kind.Description, resource.Description} {
			if strings.Contains(description, phrase) {
				t.Fatalf("%s description = %q, want no abstract phrase %q in place of the legal values", declaration.Name, description, phrase)
			}
		}
	}
}

func assertExecDescriptionsOfferNoPrefixedKey(t *testing.T, declaration *trpctool.Declaration) {
	t.Helper()
	hint := regexp.MustCompile(`(?:tmux|kind|resource)\s*:\s*[A-Za-z0-9_-]`)
	if !hint.MatchString("tmux:local") {
		t.Fatal("the prefixed-key detector misses tmux:local, so its silence proves nothing")
	}
	kind, resource := execResourceEntries(t, declaration)
	for _, description := range []string{kind.Description, resource.Description} {
		if hint.MatchString(description) {
			t.Fatalf("%s description = %q offers a prefixed key", declaration.Name, description)
		}
	}
}

func assertExecDeclarationsShareOneVocabulary(t *testing.T, run, release *trpctool.Declaration) {
	t.Helper()
	runKind, runResource := execResourceEntries(t, run)
	releaseKind, releaseResource := execResourceEntries(t, release)
	if runResource.Pattern != releaseResource.Pattern {
		t.Fatalf("run pattern = %q, release pattern = %q, want one pattern", runResource.Pattern, releaseResource.Pattern)
	}
	if fmt.Sprint(runKind.Enum) != fmt.Sprint(releaseKind.Enum) {
		t.Fatalf("run enum = %v, release enum = %v, want one enum", runKind.Enum, releaseKind.Enum)
	}
	if runKind.Description != releaseKind.Description || runResource.Description != releaseResource.Description {
		t.Fatalf("run descriptions = %q / %q, release = %q / %q, want one vocabulary",
			runKind.Description, runResource.Description, releaseKind.Description, releaseResource.Description)
	}
}

// exitFrameExecutor is the executor double the final-output scenes run on: the
// frame it records grows from the partial frame into the final frame once the
// execution behind the key ended, and every capture is logged beside the state
// it was read in, so a check reads what the release path actually captured
// rather than what the caller once saw.
type exitFrameExecutor struct {
	mu        sync.Mutex
	starts    int
	dead      bool
	unknown   bool
	hangFrame bool
	captures  []string
}

func (e *exitFrameExecutor) Start(_ context.Context, decl ExecutionDecl) (ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	return decl.Resource, nil
}

func (e *exitFrameExecutor) Probe(_ context.Context, _ ResourceKey) (ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case e.unknown:
		return ProbeResult{State: ProbeUnknown, Detail: "the probe behind this key could not decide"}, nil
	case e.dead:
		return ProbeResult{State: ProbeDead, Detail: "the execution behind this key is confirmed absent"}, nil
	default:
		return ProbeResult{State: ProbeAlive}, nil
	}
}

func (e *exitFrameExecutor) Capture(ctx context.Context, _ ResourceKey) ([]byte, error) {
	e.mu.Lock()
	if e.hangFrame {
		e.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer e.mu.Unlock()
	frame := "alpha"
	if e.dead {
		frame = "alpha\nomega"
	}
	e.captures = append(e.captures, frame)
	return []byte(frame), nil
}

func (e *exitFrameExecutor) Stop(_ context.Context, key ResourceKey) (StopEvidence, error) {
	return StopEvidence{Resource: key, Confirmed: true, ExitCode: 7}, nil
}

// markExited ends the execution behind the key: probes report its confirmed
// absence and captures carry the final frame the exit left behind.
func (e *exitFrameExecutor) markExited() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dead = true
}

// markUndetermined turns the probe behind the key into one that cannot decide.
func (e *exitFrameExecutor) markUndetermined() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unknown = true
}

// hangCaptures makes every capture wait for its caller context to give up.
func (e *exitFrameExecutor) hangCaptures() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hangFrame = true
}

// finalFrames counts the captures that carried the final frame (omega): zero
// beside a release path that never saved the final output before it freed the
// resource.
func (e *exitFrameExecutor) finalFrames() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, frame := range e.captures {
		if strings.Contains(frame, "omega") {
			n++
		}
	}
	return n
}

// startCount reports how many executions the double started.
func (e *exitFrameExecutor) startCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts
}

// finalOutputRun drives one formal exec_run call and returns its answer.
func finalOutputRun(t *testing.T, tool *RunTool, kind, resource, script string) string {
	t.Helper()
	args, err := json.Marshal(map[string]any{
		"command":  "sh",
		"args":     []string{"-c", script},
		"kind":     kind,
		"resource": resource,
		"ttl":      "1m",
	})
	if err != nil {
		t.Fatalf("run arguments = %v", err)
	}
	answer, err := tool.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("exec_run over %s/%s = %v", kind, resource, err)
	}
	return fmt.Sprint(answer)
}

// finalOutputRelease drives one formal exec_release call and returns its answer
// beside the error, with an empty answer standing beside a named refusal.
func finalOutputRelease(t *testing.T, tool *ReleaseTool, kind, resource string) (string, error) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"kind": kind, "resource": resource})
	if err != nil {
		t.Fatalf("release arguments = %v", err)
	}
	answer, err := tool.Call(context.Background(), args)
	if answer == nil {
		return "", err
	}
	return fmt.Sprint(answer), err
}

// TestExecFinalOutputSurvivesTheExit pins the G6 symptom scene end to end.
// - the first call reads only the partial frame the execution had produced then
// - the exit that frees the resource saves the final frame before it releases
// - the tracker anchors two alive observations before it reports the ending
// - the final output stays readable beside the query faces after the exit
// - a read of the freed resource stays the named unknown, never a silent empty
// - every check runs through the formal tool entry or the manager query faces
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecFinalOutputSurvivesTheExit(t *testing.T) {
	t.Run("the_exit_saves_the_final_output", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		exited := make(chan StateEvent, 8)
		manager := NewManager(backend, WithStateObserver(func(evt StateEvent) {
			if evt.Phase == StateExited {
				exited <- evt
			}
		}))
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "final-exit"}

		answer := finalOutputRun(t, run, key.Kind, key.Key, "echo alpha; sleep 1; echo omega; exit 7")
		if !strings.Contains(answer, "alpha") {
			t.Fatalf("exec_run answer = %q, want the partial frame carrying alpha", answer)
		}
		if strings.Contains(answer, "omega") {
			t.Fatalf("exec_run answer = %q carries the final marker before the exit produced it", answer)
		}
		if got := backend.finalFrames(); got != 0 {
			t.Fatalf("final-output captures before the exit = %d, want none beside the call window", got)
		}

		time.Sleep(80 * time.Millisecond)
		if _, err := manager.FinalOutput(key); !errors.Is(err, ErrFinalOutputUnsettled) {
			t.Fatalf("final output beside the held resource = %v, want the named unsettled refusal", err)
		}
		backend.markExited()

		select {
		case evt := <-exited:
			if evt.ExitCode != 7 {
				t.Fatalf("the exit event carried exit=%d, want the observed 7", evt.ExitCode)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the tracker never reported the exit of the execution")
		}

		if got := backend.finalFrames(); got < 1 {
			t.Fatalf("最终输出捕获次数 = %d, want >= 1: the exit released the resource without saving its final output", got)
		}
		settled, err := manager.FinalOutput(key)
		if err != nil {
			t.Fatalf("final output of the exited resource = %v, want the saved frame readable beside the exit", err)
		}
		if !strings.Contains(string(settled.Output), "alpha") || !strings.Contains(string(settled.Output), "omega") {
			t.Fatalf("final output = %q, want the full frame carrying alpha and omega", settled.Output)
		}
		if settled.ExitCode != 7 {
			t.Fatalf("final output exit = %d, want the observed 7", settled.ExitCode)
		}
		if _, err := manager.Capture(context.Background(), key); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("capture of the freed resource = %v, want the named unknown", err)
		}
	})

	t.Run("the_release_answer_carries_the_final_output", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		release := NewReleaseTool(manager)
		key := ResourceKey{Kind: "probe", Key: "final-release"}

		answer := finalOutputRun(t, run, key.Kind, key.Key, "echo alpha; sleep 1; echo omega; exit 7")
		if strings.Contains(answer, "omega") {
			t.Fatalf("exec_run answer = %q carries the final marker before the exit produced it", answer)
		}
		backend.markExited()

		released, err := finalOutputRelease(t, release, key.Kind, key.Key)
		if err != nil {
			t.Fatalf("exec_release after the exit = %v", err)
		}
		if !strings.Contains(released, "released "+key.String()) || !strings.Contains(released, "exit=7") {
			t.Fatalf("exec_release answer = %q, want the confirmed release beside exit=7", released)
		}
		if !strings.Contains(released, "omega") {
			t.Fatalf("exec_release answer = %q, want the final output carrying omega beside the release", released)
		}
		if got := backend.finalFrames(); got < 1 {
			t.Fatalf("最终输出捕获次数 = %d, want >= 1: the release freed the resource without saving its final output", got)
		}
		settled, err := manager.FinalOutput(key)
		if err != nil {
			t.Fatalf("final output beside the release = %v, want the saved frame readable", err)
		}
		if !strings.Contains(string(settled.Output), "omega") || settled.ExitCode != 7 {
			t.Fatalf("final output = %q exit=%d, want the full frame beside the observed 7", settled.Output, settled.ExitCode)
		}
	})

	t.Run("a_hung_capture_never_holds_the_release", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		backend.hangCaptures()
		manager := NewManager(backend, WithFinalOutputCaptureWindow(50*time.Millisecond))
		key := ResourceKey{Kind: "probe", Key: "final-hung"}
		decl := ExecutionDecl{Command: "sh", Args: []string{"-c", "echo alpha"}, Resource: key}
		if _, err := manager.Acquire(context.Background(), decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquire the execution = %v", err)
		}

		started := time.Now()
		evidence, err := manager.Release(context.Background(), key)
		if err != nil || !evidence.Released {
			t.Fatalf("release beside a hung capture = %+v err=%v, want the release freed anyway", evidence, err)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("the release waited %v beside a hung capture, want the bounded save to give up", elapsed)
		}
		if _, err := manager.FinalOutput(key); !errors.Is(err, ErrFinalOutputUnrecorded) {
			t.Fatalf("final output beside the hung capture = %v, want the named unrecorded refusal, never a silent empty", err)
		}
	})
}

// TestExecTakeoverOrNamedReport pins the D6-4 restart tree of the run tool: unknown -> query -> take over or report by name.
// - a resource observed alive is reattached instead of opened a second time
// - a probe that could not decide is reported by name and claims no ending
// - a settled key reports the saved final output instead of rerunning over it
// - a dead key with nothing recorded is a new execution
// - every check runs through the formal tool entry or the manager query faces
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecTakeoverOrNamedReport(t *testing.T) {
	t.Run("takes_over_the_running_session", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "takeover-alive"}

		answer := finalOutputRun(t, run, key.Kind, key.Key, "sleep 30")
		if !strings.HasPrefix(answer, "reattached "+key.String()+": ") {
			t.Fatalf("exec_run answer = %q, want the reattachment under %s", answer, key)
		}
		if !strings.Contains(answer, "alpha") {
			t.Fatalf("exec_run answer = %q, want the frame the takeover captured", answer)
		}
		if got := backend.startCount(); got != 0 {
			t.Fatalf("backend started %d executions, want none: a takeover must not open a second execution", got)
		}
		if got := manager.Adopted(); got != 1 {
			t.Fatalf("adopted = %d, want the one takeover", got)
		}
		if got := manager.Held(); got != 1 {
			t.Fatalf("held = %d, want the taken-over resource held", got)
		}
		explanation, err := manager.Explain(key)
		if err != nil || !explanation.Adopted {
			t.Fatalf("explain of the takeover = %+v err=%v, want the reattachment readable through the query face", explanation, err)
		}
	})

	t.Run("names_the_unconfirmed_state_without_taking_over", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		backend.markUndetermined()
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "takeover-unknown"}

		args, err := json.Marshal(map[string]any{"command": "sh", "args": []string{"-c", "sleep 30"}, "kind": key.Kind, "resource": key.Key, "ttl": "1m"})
		if err != nil {
			t.Fatalf("run arguments = %v", err)
		}
		answer, err := run.Call(context.Background(), args)
		if !errors.Is(err, ErrAdoptUnconfirmed) {
			t.Fatalf("exec_run behind an undeterminable probe = %v, want the named %v", err, ErrAdoptUnconfirmed)
		}
		if answer != nil {
			t.Fatalf("exec_run behind an undeterminable probe answered %v, want the named refusal only", answer)
		}
		if !strings.Contains(err.Error(), key.String()) {
			t.Fatalf("the refusal = %q, want it to name %s", err, key)
		}
		if got := backend.startCount(); got != 0 {
			t.Fatalf("backend started %d executions, want none behind an undeterminable probe", got)
		}
		if got := manager.Adopted(); got != 0 {
			t.Fatalf("adopted = %d, want no takeover on undeterminable evidence", got)
		}
		if got := manager.Held(); got != 0 {
			t.Fatalf("held = %d, want nothing held beside the named refusal", got)
		}
		lower := strings.ToLower(err.Error())
		for _, claim := range []string{"released", "stopped", "exit="} {
			if strings.Contains(lower, claim) {
				t.Fatalf("the refusal claims %q: %q", claim, err)
			}
		}
	})

	t.Run("reports_the_settled_final_output_without_rerunning", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "takeover-settled"}
		decl := ExecutionDecl{Command: "sh", Args: []string{"-c", "echo alpha; sleep 1; echo omega; exit 7"}, Resource: key}
		if _, err := manager.Acquire(context.Background(), decl, WithTTL(time.Minute)); err != nil {
			t.Fatalf("acquire the execution = %v", err)
		}
		backend.markExited()
		evidence, err := manager.Release(context.Background(), key)
		if err != nil || !evidence.Released {
			t.Fatalf("release after the exit = %+v err=%v, want the confirmed settlement", evidence, err)
		}

		answer := finalOutputRun(t, run, key.Kind, key.Key, "echo rerun")
		if !strings.HasPrefix(answer, "settled "+key.String()+": ") {
			t.Fatalf("exec_run answer = %q, want the settled report under %s", answer, key)
		}
		if !strings.Contains(answer, "exit=7") || !strings.Contains(answer, "omega") {
			t.Fatalf("exec_run answer = %q, want the observed exit beside the saved final output", answer)
		}
		if got := backend.startCount(); got != 1 {
			t.Fatalf("backend started %d executions, want the one settlement only: a settled key must not rerun", got)
		}
		if got := manager.Held(); got != 0 {
			t.Fatalf("held = %d, want the settled key freed", got)
		}
		settled, err := manager.FinalOutput(key)
		if err != nil || !strings.Contains(string(settled.Output), "omega") {
			t.Fatalf("final output after the rerun attempt = %q err=%v, want the record kept intact", settled.Output, err)
		}
	})

	t.Run("starts_a_new_execution_for_a_dead_key_without_a_record", func(t *testing.T) {
		backend := &exitFrameExecutor{}
		backend.markExited()
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "takeover-dead"}

		if _, err := manager.FinalOutput(key); !errors.Is(err, ErrFinalOutputUnrecorded) {
			t.Fatalf("final output of a key never settled = %v, want the named unrecorded refusal", err)
		}
		answer := finalOutputRun(t, run, key.Kind, key.Key, "echo alpha")
		if !strings.HasPrefix(answer, "executed "+key.String()+": ") {
			t.Fatalf("exec_run answer = %q, want the new execution under %s", answer, key)
		}
		if got := backend.startCount(); got != 1 {
			t.Fatalf("backend started %d executions, want the one new execution", got)
		}
	})
}

var _ Executor = (*exitFrameExecutor)(nil)
