package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// §4.2 (R03): an EXISTING task's Resume/Relaunch re-entry must resolve its
// delegation target against the generation the re-entry SELECTED (D5 row 4:
// initiator's binding when it rides a call, effective otherwise) — never against
// a wrapper the spawn-time closure kept alive. These tests drive the real
// production closures (installed by AgentToolWrapper.Call as TaskSpec.Relaunch /
// TaskSpec.ResumeFn) and assert only host-visible outcomes: which instance served
// the run, whether a run happened at all, and the refusal text.

// reentryChild is a named sub-agent that records how often it ran and answers
// with an instance-unique marker, so "WHICH generation's wrapper served this
// re-entry" is observable at the result, not by pointer comparison.
type reentryChild struct {
	mu      sync.Mutex
	runs    int
	name    string
	output  string
	gate    chan struct{}       // nil = answer immediately
	lastInv *upagent.Invocation // what the most recent run actually received
}

// armGate parks every subsequent run of this instance until the returned channel
// closes — the handle on 「这个重入还在途」 that the lease assertions need.
func (c *reentryChild) armGate() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
	return c.gate
}

func (c *reentryChild) last() *upagent.Invocation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastInv
}

func newReentryChild(name, output string) *reentryChild {
	return &reentryChild{name: name, output: output}
}

func (c *reentryChild) Run(ctx context.Context, inv *upagent.Invocation) (<-chan *event.Event, error) {
	c.mu.Lock()
	c.runs++
	c.lastInv = inv
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan *event.Event, 1)
	ch <- &event.Event{Response: &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: c.output}}},
	}}
	close(ch)
	return ch, nil
}

func (c *reentryChild) Tools() []trpctool.Tool { return nil }
func (c *reentryChild) Info() upagent.Info {
	return upagent.Info{Name: c.name, Description: "reentry child"}
}
func (c *reentryChild) SubAgents() []upagent.Agent        { return nil }
func (c *reentryChild) FindSubAgent(string) upagent.Agent { return nil }
func (c *reentryChild) runCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs
}

// reentryWrapper mirrors what buildAgentDFS wires for every real delegation: the
// resident owner's cm (`SetParentCM`, §4.2's handle for a re-entry with no
// initiator) plus the async window, long enough that the run settles INLINE.
func (h *reentryHarness) wrapper(cm *ContextManager, child *reentryChild) *AgentToolWrapper {
	w := NewAgentToolWrapper(child, "delegate to b", []string{"event_keys"}, nil)
	w.SetParentCM(cm)
	w.SetAsyncDenseDuration(h.dense)
	return w
}

// reentryDense is the sync-wait window of the harness wrappers. The default lets a
// run settle INLINE (so the spawned task reaches a legal re-entry state without any
// pumping); R2 shortens it to observe a re-entry still IN FLIGHT.
const reentryDense = 5 * time.Second

// reentryHarness publishes generations whose FACES carry distinct wrapper
// instances of the same declared target, then spawns a subagent task through the
// FIRST generation's wrapper — that task is the "存量任务" a later re-entry hits.
type reentryHarness struct {
	cm        *ContextManager
	spawner   *task.TaskManager
	turnG1    *ExecLease
	ctxG1     context.Context
	taskID    string
	spawnRuns int // how many times g1's own target instance ran during the spawn
	gen1      *leaseCountingRunner
	g1        *reentryChild
	g2        *reentryChild
	dense     time.Duration
}

func newReentryHarness(t *testing.T) *reentryHarness { return newReentryHarnessDense(t, reentryDense) }

func newReentryHarnessDense(t *testing.T, dense time.Duration) *reentryHarness {
	t.Helper()
	cm := &ContextManager{name: "d42", runner: &leaseCountingRunner{id: "boot"}}
	h := &reentryHarness{cm: cm, spawner: task.NewTaskManager(task.TaskManagerConfig{}), dense: dense}

	h.g1, h.g2 = newReentryChild("b", "SERVED-BY-G1"), newReentryChild("b", "SERVED-BY-G2")
	h.gen1 = &leaseCountingRunner{id: "g1"}
	cm.PublishExecutor(h.gen1, ContextManagerConfig{
		Name:  "d42-g1",
		Tools: []trpctool.Tool{h.wrapper(cm, h.g1)},
	})
	h.turnG1 = cm.AcquireLease(LeaseTurn) // pins g1
	t.Cleanup(h.turnG1.Release)
	h.ctxG1 = h.turnG1.WithContext(task.WithTaskSpawner(context.Background(), h.spawner))

	// The spawn itself goes through the generation-1 wrapper, exactly as a real
	// delegation does; the task it leaves behind is what later re-entries reuse.
	w1 := cm.SubagentWrapper("b")
	require.NotNil(t, w1, "precondition: g1 routes b")
	out, err := w1.Call(h.ctxG1, subagentCallArgs(t))
	require.NoError(t, err)
	require.Equal(t, "SERVED-BY-G1", out, "the first delegation is served by g1's own target")

	h.spawnRuns = h.g1.runCount()
	require.Equal(t, 1, h.spawnRuns, "the first delegation ran g1's instance exactly once")
	tk := h.findSubagentTask(t)
	h.taskID = tk.ID
	require.NotNil(t, tk.Spec.Relaunch, "precondition: a subagent spawn installs a relaunch closure")
	require.NotNil(t, tk.Spec.ResumeFn, "…and a resume closure")
	return h
}

func (h *reentryHarness) findSubagentTask(t *testing.T) *task.Task {
	t.Helper()
	for _, tk := range h.spawner.List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	t.Fatal("the delegation left no subagent task in the registry")
	return nil
}

// publishG2 replaces the effective face. tools=nil removes the target entirely.
func (h *reentryHarness) publishG2(child *reentryChild) {
	var tools []trpctool.Tool
	if child != nil {
		tools = append(tools, h.wrapper(h.cm, child))
	}
	h.cm.PublishExecutor(&leaseCountingRunner{id: "g2"}, ContextManagerConfig{Name: "d42-g2", Tools: tools})
}

// g1Refs is the diagnostic row of the generation the harness turn pinned — read
// from the lease itself, so no test hardcodes a binding sequence number. It is nil
// while that generation is still in force (nothing unconverged to report).
func (h *reentryHarness) g1Refs() *UnconvergedRef {
	id := h.turnG1.Generation()
	for i, u := range h.cm.UnconvergedRefs() {
		if u.Generation == id {
			return &h.cm.UnconvergedRefs()[i]
		}
	}
	return nil
}

// TestReentry_RelaunchRefusesTargetRemovedByNewGeneration is the R03 core: a task
// spawned while the orchestration routed b must NOT revive that target once the
// effective generation removed it. The pre-fix closure kept the spawn-time
// wrapper, so the re-entry ran b anyway — a removed target executing on a
// generation that no longer routes it.
func TestReentry_RelaunchRefusesTargetRemovedByNewGeneration(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(nil)
	require.Nil(t, h.cm.SubagentWrapper("b"), "precondition: the effective face really stopped routing b")

	before := h.g1.runCount()
	res, err := h.spawner.Relaunch(context.Background(), h.taskID)
	require.Error(t, err, "a re-entry whose selected generation lacks the target must be refused, got %+v", res)
	require.Contains(t, err.Error(), "b", "the refusal must name the missing target")
	require.Zero(t, h.g1.runCount()-before, "a refused re-entry must not run the retired target at all")
}

// TestReentry_RelaunchWithoutInitiatorTakesCurrentFace pins the other half of D5
// row 4: with no initiating call holding a binding, the re-entry resolves against
// the CURRENT effective face — so it runs THIS generation's target instance, not
// the one the spawn captured. Both generations route b; only the served instance
// distinguishes "current" from "captured".
func TestReentry_RelaunchWithoutInitiatorTakesCurrentFace(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(h.g2)

	res, err := h.spawner.Relaunch(context.Background(), h.taskID)
	require.NoError(t, err)
	require.True(t, res.Settled, "the re-spawn settles inside the dense window")
	require.Equal(t, "SERVED-BY-G2", res.Signal.Output,
		"a re-entry with no initiator must run the CURRENT generation's target, not the captured one")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "the spawn-time target instance must never be touched again")
}

// TestReentry_ResumeWithoutInitiatorTakesCurrentFace is the same rule on the
// resume (送输入) entry: the resumed round's target comes from the current face.
func TestReentry_ResumeWithoutInitiatorTakesCurrentFace(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(h.g2)

	res, err := h.spawner.Resume(context.Background(), h.taskID, "more work")
	require.NoError(t, err)
	require.True(t, res.Settled, "the resumed round settles inside the dense window")
	require.Equal(t, "SERVED-BY-G2", res.Signal.Output,
		"resume must re-enter through the CURRENT generation's target")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "and never through the captured one")
}

// TestReentry_InitiatorOnOlderGenerationKeepsItsOwnTarget is D5's inheritance row
// plus spec resident-continuity「仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1
// 绑定」at the re-entry edge: a relaunch initiated by a call that still holds G1 runs
// G1's target even though the effective face removed it — and it takes its OWN
// reference on G1 while running, so the retired generation cannot be reclaimed
// underneath it. R03's other half (a stored task must not decide routing) needs both
// halves: refusing when NO initiator vouches for the target, and honouring the
// initiator when it does.
func TestReentry_InitiatorOnOlderGenerationKeepsItsOwnTarget(t *testing.T) {
	h := newReentryHarnessDense(t, 30*time.Millisecond) // ack, so the re-entry stays in flight
	gate := h.g1.armGate()
	h.publishG2(nil)
	require.Nil(t, h.cm.SubagentWrapper("b"), "precondition: the effective face removed b")

	res, err := h.spawner.Relaunch(h.ctxG1, h.taskID)
	require.NoError(t, err, "an initiator that holds G1 must not lose its own binding because G2 removed the target")
	require.False(t, res.Settled, "the re-entry is parked mid-run, which is what makes the pinning observable")
	require.Equal(t, "SERVED-BY-G1", h.g1.output, "the target that ran belongs to the initiator's generation")
	require.Equal(t, 2, h.g1.runCount(), "spawn plus this re-entry, and no other path re-ran it")

	un := h.g1Refs()
	require.NotNil(t, un, "G1 is retired and still referenced, so it must be on the unconverged list")
	require.Equal(t, 1, un.Refs[LeaseSubCall.String()],
		"the re-entry holds its OWN reference kind on the generation it resolved against")
	require.Zero(t, h.gen1.closed.Load(), "a running re-entry keeps its generation open")

	close(gate)
	require.Eventually(t, func() bool {
		u := h.g1Refs()
		return u == nil || u.Refs[LeaseSubCall.String()] == 0
	}, 5*time.Second, 10*time.Millisecond, "the reference drops exactly when the re-entered producer stops")
	require.Zero(t, h.gen1.closed.Load(), "the initiator's own turn reference still holds it")

	h.turnG1.Release()
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"and once the LAST holder is gone the generation is reclaimed, exactly once")
	require.Equal(t, int64(1), h.gen1.closed.Load())
}

// TestReentry_RefusedResumeLeavesTheTaskChainUntouched pins the second clause of
// 「所选代无目标则拒绝且不改任务链」: a refused re-entry records no round, consumes
// nothing, and the same task still resumes with its ORIGINAL chain once some
// generation routes the target again (the task's own state also survived the
// refusal — a second resume is legally offered and legally runs).
func TestReentry_RefusedResumeLeavesTheTaskChainUntouched(t *testing.T) {
	h := newReentryHarness(t)
	h.publishG2(nil)

	_, err := h.spawner.Resume(context.Background(), h.taskID, "phantom round")
	require.Error(t, err, "a resume whose selected generation lacks the target is refused")
	require.Zero(t, h.g2.runCount(), "and nothing runs on the way to that refusal")
	require.Equal(t, h.spawnRuns, h.g1.runCount(), "nor does it reach the spawn-time target")

	// Bring the target back into the effective face: the chain must still carry
	// exactly the ONE round the original spawn settled — no phantom round from the
	// refused attempt.
	third := newReentryChild("b", "SERVED-BY-G3")
	h.publishG2(third)
	res, err := h.spawner.Resume(context.Background(), h.taskID, "continue")
	require.NoError(t, err, "the task itself stayed resumable across the refusal")
	require.True(t, res.Settled, "the resumed round ran inline")

	require.Equal(t, 1, third.runCount(), "the resumed round ran on the generation in force now")
	raw, ok := third.last().RunOptions.RuntimeState[ExternalContextKey].(json.RawMessage)
	require.True(t, ok, "the resumed run must receive the restored task-chain context")
	require.Equal(t, 1, strings.Count(string(raw), "〔本任务上一轮〕"),
		"a refused re-entry must not have appended a round (chain contents: %s)", raw)
	require.Contains(t, string(raw), "SERVED-BY-G1", "and the surviving round is the original settle result")
}
