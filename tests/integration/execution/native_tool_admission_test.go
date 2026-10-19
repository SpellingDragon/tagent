package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestNativeToolAdmission checks the one-shot admission protocol of the registration wrapper.
// - a tool completing at once is still answered with one receipt and keeps its obligation until the target answers
// - an approval hold registers an unexecuted task and answers with a waiting receipt carrying its approval identity and argument digest
// - an approval response resolves the hold it was issued for, and a mismatched identity or digest is refused by name
// - a refusal executes nothing and carries its named paired result beside one native tool error
// - a background result travels back through its own notification and never as a second response of the call
// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
func TestNativeToolAdmission(t *testing.T) {
	t.Run("a fast tool keeps its obligation until the target answers", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "fast-tool", output: "ok", finished: make(chan struct{})}
		recorder := &fakeRecorder{gate: make(chan struct{})}
		box := &fakeMailbox{answer: make(chan struct{}), arrived: make(chan task.Notice, 4)}
		wrapper := task.Wrap(rig.manager, inner, task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept))
		ctx := task.WithCallFrame(context.Background(), testFrame("call-fast"))

		returned, err := wrapper.Call(ctx, nil)
		if err != nil {
			t.Fatalf("Call = (%v, %v), want one admission receipt of call-fast", returned, err)
		}
		receipt, ok := returned.(task.AdmissionReceipt)
		if !ok || !receipt.Accepted || receipt.Kind != task.AdmissionFresh ||
			receipt.CallID != "call-fast" || receipt.TaskID == "" {
			t.Fatalf("Call = (%+v), want one accepted fresh receipt carrying CallID, TaskID and admission kind", returned)
		}
		awaitSignal(t, inner.finished, "the fast tool to finish")
		if got := inner.callCount(); got != 1 {
			t.Fatalf("inner calls = %d, want the fast tool executed once", got)
		}
		held, err := rig.manager.Obligations(ctx, receipt.TaskID)
		if err != nil || held.Count != 1 || held.Open[0].Kind != task.ObligationAwaitingTool {
			t.Fatalf("obligations = (%+v, %v), want the admission obligation held after the fast tool finished", held, err)
		}
		close(recorder.gate)
		notice := awaitNotice(t, box.arrived)
		if notice.TaskID != receipt.TaskID || notice.Kind != task.NoticeSettled {
			t.Fatalf("notice = %+v, want the settled notification of %s", notice, receipt.TaskID)
		}
		recorded := recorder.results()
		if len(recorded) != 1 || recorded[0].CallID != "call-fast" || recorded[0].Tool != "fast-tool" ||
			recorded[0].TaskID != receipt.TaskID || recorded[0].Output != "ok" || recorded[0].Err != nil {
			t.Fatalf("recorded = %+v, want one recorded result carrying the settled output of call-fast", recorded)
		}
		if notice.Event != recorder.factID(0) {
			t.Fatalf("notice event = %q, want the recorded fact %q", notice.Event, recorder.factID(0))
		}
		pending, err := rig.manager.Obligations(ctx, receipt.TaskID)
		if err != nil || pending.Count != 1 || pending.Open[0].Kind != task.ObligationAwaitingDelivery {
			t.Fatalf("obligations = (%+v, %v), want the delivery obligation held until the target answers", pending, err)
		}
		close(box.answer)
		awaitObligations(t, rig.manager, receipt.TaskID, 0)

		again, err := wrapper.Call(ctx, nil)
		repeat, ok := again.(task.AdmissionReceipt)
		if err != nil || !ok || repeat.Kind != task.AdmissionDedup || repeat.TaskID != receipt.TaskID {
			t.Fatalf("second Call = (%+v, %v), want the dedup receipt of the same task", again, err)
		}
		if got := inner.callCount(); got != 1 {
			t.Fatalf("inner calls = %d, want the repeated admission to execute nothing a second time", got)
		}
	})

	t.Run("an approval hold registers an unexecuted task", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "held-tool"}
		recorder := &fakeRecorder{}
		box := &fakeMailbox{arrived: make(chan task.Notice, 4)}
		approve := func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingHold, Reason: "waiting for approval"}, nil
		}
		wrapper := task.Wrap(rig.manager, inner,
			task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept), task.WithApprover(approve))
		ctx := task.WithCallFrame(context.Background(), testFrame("call-hold"))

		returned, err := wrapper.Call(ctx, nil)
		if err != nil {
			t.Fatalf("Call = (%v, %v), want the waiting receipt of call-hold", returned, err)
		}
		receipt, ok := returned.(task.HoldReceipt)
		if !ok || receipt.CallID != "call-hold" || receipt.Hold.TaskID == "" || receipt.Hold.Reason != "waiting for approval" {
			t.Fatalf("Call = (%+v), want a waiting receipt holding CallID and the waiting state", returned)
		}
		if got := inner.callCount(); got != 0 {
			t.Fatalf("inner calls = %d, want a held call to execute nothing", got)
		}
		if _, status, err := rig.registry.Lookup(ctx, receipt.Hold.TaskID); err != nil || status != task.StatusAwaitingApproval {
			t.Fatalf("lookup = (%v, %v), want the held task parked in the approval wait with no execution", status, err)
		}
		held, ok := wrapper.Holds().Waiting(receipt.Hold.TaskID)
		if !ok || held.TaskID != receipt.Hold.TaskID || held.Reason != "waiting for approval" {
			t.Fatalf("waiting = (%+v, %v), want the hold queryable as waiting", held, ok)
		}
		again, ok := wrapper.Holds().Waiting(receipt.Hold.TaskID)
		if !ok || again != held {
			t.Fatalf("repeated waiting = (%+v, %v), want the same waiting state beside no terminal answer", again, ok)
		}
		if recorded := recorder.results(); len(recorded) != 0 {
			t.Fatalf("recorded = %+v, want no result beside a waiting hold", recorded)
		}
	})

	t.Run("an approval response resolves the hold it was issued for", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "bound-tool"}
		recorder := &fakeRecorder{}
		box := &fakeMailbox{arrived: make(chan task.Notice, 4)}
		approve := func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingHold, Reason: "waiting for approval"}, nil
		}
		wrapper := task.Wrap(rig.manager, inner,
			task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept), task.WithApprover(approve))
		args := []byte(`{"path":"/tmp/a"}`)
		ctx := task.WithCallFrame(context.Background(), testFrame("call-bound"))

		returned, err := wrapper.Call(ctx, args)
		if err != nil {
			t.Fatalf("Call = (%v, %v), want the waiting receipt of call-bound", returned, err)
		}
		receipt, ok := returned.(task.HoldReceipt)
		if !ok || receipt.Hold.ApprovalID == "" || receipt.Hold.ExpectedDigest != task.DigestArgs(args) {
			t.Fatalf("Call = (%+v), want a waiting receipt carrying the approval identity and the digest of the held arguments", returned)
		}
		answer := task.ApprovalAnswer{
			ApprovalID:     receipt.Hold.ApprovalID,
			ExpectedDigest: receipt.Hold.ExpectedDigest,
			Granted:        true,
			Reason:         "allowed by the host",
		}
		first, err := wrapper.Holds().Resolve(receipt.Hold.TaskID, answer)
		if err != nil || !first.Granted || first.Reason != "allowed by the host" {
			t.Fatalf("resolve = (%+v, %v), want the matching response accepted beside its ruling", first, err)
		}
		repeat := answer
		repeat.Granted = false
		repeat.Reason = "another ground"
		again, err := wrapper.Holds().Resolve(receipt.Hold.TaskID, repeat)
		if err != nil || again != first {
			t.Fatalf("repeated resolve = (%+v, %v), want the first ruling reported again beside no second effect", again, err)
		}
		if got := inner.callCount(); got != 0 {
			t.Fatalf("inner calls = %d, want a held call to execute nothing", got)
		}
	})

	t.Run("a mismatched approval response is refused by name", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "mismatch-tool"}
		recorder := &fakeRecorder{}
		box := &fakeMailbox{arrived: make(chan task.Notice, 4)}
		approve := func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingHold, Reason: "waiting for approval"}, nil
		}
		wrapper := task.Wrap(rig.manager, inner,
			task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept), task.WithApprover(approve))
		args := []byte(`{"path":"/tmp/a"}`)
		ctx := task.WithCallFrame(context.Background(), testFrame("call-mismatch"))

		returned, err := wrapper.Call(ctx, args)
		if err != nil {
			t.Fatalf("Call = (%v, %v), want the waiting receipt of call-mismatch", returned, err)
		}
		receipt, ok := returned.(task.HoldReceipt)
		if !ok || receipt.Hold.ApprovalID == "" {
			t.Fatalf("Call = (%+v), want a waiting receipt carrying its approval identity", returned)
		}
		otherIdentity := task.ApprovalAnswer{
			ApprovalID:     "approval-other",
			ExpectedDigest: receipt.Hold.ExpectedDigest,
			Granted:        true,
		}
		if _, err := wrapper.Holds().Resolve(receipt.Hold.TaskID, otherIdentity); !errors.Is(err, task.ErrApprovalIdentityMismatch) {
			t.Fatalf("resolve err = %v, want the response beside another approval identity refused with %v", err, task.ErrApprovalIdentityMismatch)
		}
		otherArgs := task.ApprovalAnswer{
			ApprovalID:     receipt.Hold.ApprovalID,
			ExpectedDigest: task.DigestArgs([]byte(`{"path":"/other"}`)),
			Granted:        true,
		}
		if _, err := wrapper.Holds().Resolve(receipt.Hold.TaskID, otherArgs); !errors.Is(err, task.ErrApprovalDigestMismatch) {
			t.Fatalf("resolve err = %v, want the response over other arguments refused with %v", err, task.ErrApprovalDigestMismatch)
		}
		if got := inner.callCount(); got != 0 {
			t.Fatalf("inner calls = %d, want a refused response to execute nothing", got)
		}
	})

	t.Run("a refusal executes nothing and carries its named paired result", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "risk-tool"}
		recorder := &fakeRecorder{}
		box := &fakeMailbox{arrived: make(chan task.Notice, 4)}
		approve := func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingDeny, Reason: "risk rule"}, nil
		}
		wrapper := task.Wrap(rig.manager, inner,
			task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept), task.WithApprover(approve))
		ctx := task.WithCallFrame(context.Background(), testFrame("call-deny"))

		returned, err := wrapper.Call(ctx, nil)
		if err == nil || !errors.Is(err, task.ErrDenied) {
			t.Fatalf("Call = (%+v, %v), want one native tool error paired with call-deny", returned, err)
		}
		if returned != nil {
			t.Fatalf("Call = (%+v, %v), want no result body in the synchronous response", returned, err)
		}
		if got := inner.callCount(); got != 0 {
			t.Fatalf("inner calls = %d, want a refused call to execute nothing", got)
		}
		recorded := recorder.results()
		if len(recorded) != 1 {
			t.Fatalf("recorded = %+v, want exactly one named paired result of the refusal", recorded)
		}
		paired := recorded[0]
		if paired.Denied == nil || paired.Denied.Tool != "risk-tool" || paired.Denied.Reason != "risk rule" ||
			paired.CallID != "call-deny" || paired.Tool != "risk-tool" {
			t.Fatalf("paired result = %+v, want the named refusal of risk-tool paired with call-deny", paired)
		}
		if _, known := rig.admission.Lookup(testFrame("call-deny").Identity.DedupKey()); known {
			t.Fatal("the refused call must register no task and withdraw its pre-registration")
		}
	})

	t.Run("a background result travels back through its own notification", func(t *testing.T) {
		rig := newTestRig()
		inner := &fakeTool{name: "slow-tool", output: "done", gate: make(chan struct{})}
		recorder := &fakeRecorder{}
		box := &fakeMailbox{answer: make(chan struct{}), arrived: make(chan task.Notice, 4)}
		wrapper := task.Wrap(rig.manager, inner, task.WithResultRecorder(recorder.record), task.WithMailbox(box.accept))
		ctx := task.WithCallFrame(context.Background(), testFrame("call-slow"))

		returned, err := wrapper.Call(ctx, nil)
		if err != nil {
			t.Fatalf("Call = (%v, %v), want one admission receipt of call-slow", returned, err)
		}
		receipt, ok := returned.(task.AdmissionReceipt)
		if !ok || receipt.Kind != task.AdmissionFresh || receipt.CallID != "call-slow" || receipt.TaskID == "" {
			t.Fatalf("Call = (%+v), want one admission receipt of call-slow", returned)
		}
		if notes := box.snapshot(); len(notes) != 0 {
			t.Fatalf("mailbox = %+v, want no result beside a call that already returned", notes)
		}
		close(inner.gate)
		notice := awaitNotice(t, box.arrived)
		if notice.TaskID != receipt.TaskID || notice.Kind != task.NoticeSettled || notice.Event != recorder.factID(0) {
			t.Fatalf("notice = %+v, want the settled notification of %s referencing the recorded fact", notice, receipt.TaskID)
		}
		recorded := recorder.results()
		if len(recorded) != 1 || recorded[0].CallID != "call-slow" || recorded[0].Output != "done" {
			t.Fatalf("recorded = %+v, want one recorded result carrying the output of call-slow", recorded)
		}
		if body, isBody := returned.(string); isBody && body == "done" {
			t.Fatalf("Call = (%+v), want the result body to travel through the notification only", returned)
		}
		close(box.answer)
		awaitObligations(t, rig.manager, receipt.TaskID, 0)
	})
}

// testRig is one in-memory task stack a sub-case admits its calls against.
type testRig struct {
	admission *task.MemoryAdmission
	registry  *task.MemoryRegistry
	manager   *task.MemoryManager
}

// newTestRig returns one in-memory task stack: an admission face, a registry over
// one memory commit carrier, and the manager holding the obligation ledger.
func newTestRig() testRig {
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	return testRig{
		admission: admission,
		registry:  registry,
		manager:   task.NewMemoryManager(registry, nil),
	}
}

// testFrame returns the call frame one wrapped call of the named call is admitted under.
func testFrame(callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-a",
			InvocationID: "invocation-a",
			DecisionID:   "decision-a",
			CallID:       callID,
		},
		Address:   event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Execution: "serial",
	}
}

// fakeTool is the wrapped native tool stand-in: it counts the calls it executes
// and reports the output the sub-case names.
type fakeTool struct {
	mu       sync.Mutex
	name     string
	output   string
	err      error
	gate     chan struct{}
	finished chan struct{}
	once     sync.Once
	calls    int
}

// Declaration returns the declaration naming the fake tool.
func (f *fakeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: f.name}
}

// Call executes the fake tool once and reports its output beside its error.
func (f *fakeTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.gate != nil {
		<-f.gate
	}
	if f.finished != nil {
		f.once.Do(func() { close(f.finished) })
	}
	return f.output, f.err
}

// callCount reports how many calls the fake tool executed.
func (f *fakeTool) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeRecorder is the result recorder stand-in: it stores each recorded result
// beside the fact identity it reports back, and may pin the recording on a gate.
type fakeRecorder struct {
	mu       sync.Mutex
	gate     chan struct{}
	recorded []task.Result
	facts    []event.EventID
}

// record stores one result and reports the identity of the fact it recorded.
func (r *fakeRecorder) record(ctx context.Context, result task.Result) (event.EventID, error) {
	if r.gate != nil {
		<-r.gate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, result)
	id := event.EventID(fmt.Sprintf("fact-%d", len(r.facts)+1))
	r.facts = append(r.facts, id)
	return id, nil
}

// results reports the results recorded so far.
func (r *fakeRecorder) results() []task.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]task.Result(nil), r.recorded...)
}

// factID reports the fact identity of the recorded result at the named position.
func (r *fakeRecorder) factID(at int) event.EventID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at < 0 || at >= len(r.facts) {
		return ""
	}
	return r.facts[at]
}

// fakeMailbox is the mailbox stand-in: it stores each notification it accepts and
// may pin its acceptance answer on a gate.
type fakeMailbox struct {
	mu      sync.Mutex
	answer  chan struct{}
	arrived chan task.Notice
	notes   []task.Notice
}

// accept stores one notification and answers the delivery when its gate opens.
func (m *fakeMailbox) accept(ctx context.Context, notice task.Notice) error {
	m.mu.Lock()
	m.notes = append(m.notes, notice)
	m.mu.Unlock()
	if m.arrived != nil {
		m.arrived <- notice
	}
	if m.answer != nil {
		<-m.answer
	}
	return nil
}

// snapshot reports the notifications accepted so far.
func (m *fakeMailbox) snapshot() []task.Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]task.Notice(nil), m.notes...)
}

// awaitSignal waits for one signal to arrive and fails the test when it does not.
func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// awaitNotice waits for one notification to arrive and fails the test when none does.
func awaitNotice(t *testing.T, ch <-chan task.Notice) task.Notice {
	t.Helper()
	select {
	case notice := <-ch:
		return notice
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a notification")
		return task.Notice{}
	}
}

// awaitObligations waits until the manager reports the wanted count of open obligations.
func awaitObligations(t *testing.T, manager *task.MemoryManager, taskID event.TaskID, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		report, err := manager.Obligations(context.Background(), taskID)
		if err == nil && report.Count == want {
			return
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("obligations query failed: %v", err)
			}
			t.Fatalf("obligations = %d, want %d", report.Count, want)
		}
		time.Sleep(time.Millisecond)
	}
}
