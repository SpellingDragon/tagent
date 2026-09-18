package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// FULL TASK CHAIN E2E (resident-remaining-hardening 3.3, archived 7.3).
//
// The individual links each had unit coverage; this ties them into ONE chain
// driven through the REAL production wiring (the same hook shapes agent.go
// installs) so the connective tissue is proven, not assumed:
//
//	spawn(+Origin) → task_spawned record → [watch/detach] → settle
//	  → task_settled event (+lineage baggage / unknown-source hold)
//	  → persistBusEvent → fact-chain settle record + deterministic feedback
//	  → RebuildTaskRegistry fold (spawned − terminal)
//
// plus the reachable edge dimensions named by the task: inline-vs-background
// settle, unknown source (lineage_absent), service/job lifetime, post-terminal
// (late) signal fencing, and resume re-binding under a stable task id.
//
// Delivery THROUGH the live loop is covered by the 3.2 resident E2E; here the
// reclaim turn's persist step (cm.persistBusEvent) is driven directly, which is
// exactly what the loop does with a reclaimed task_settled event.

const chainPID = 7

type chainHarness struct {
	cm     *ContextManager
	store  *memory.InMemoryStore
	tm     *task.TaskManager
	settle chan *AgentEvent // background OnSettle captures (post-window settles)
}

func newChainHarness(t *testing.T) *chainHarness {
	t.Helper()
	store := memory.NewInMemoryStore()
	cm := &ContextManager{
		name:        "tagent",
		memStore:    store,
		projection:  compress.NewSessionProjection(),
		partitionID: chainPID,
	}
	sink := &taskRecordSink{cm: cm}
	settled := make(chan *AgentEvent, 16)
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			evt := newTaskSettledEvent(tk, sig, settleInlineCapChars, t.TempDir())
			cm.persistBusEvent(evt) // the reclaim turn's persist step
			settled <- evt
		},
		OnSpawn:        sink.onSpawn,
		OnInlineSettle: sink.onInlineSettle,
		OnCancel:       sink.onCancel,
	})
	return &chainHarness{cm: cm, store: store, tm: tm, settle: settled}
}

func (h *chainHarness) records(typ string) []memory.FullEvent {
	refs, err := h.store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{chainPID}, EventTypes: []string{typ}, Limit: 200,
	})
	if err != nil {
		return nil
	}
	var out []memory.FullEvent
	for _, r := range refs {
		if ev, err := h.store.GetEvent(r.EventKey); err == nil && ev != nil {
			out = append(out, *ev)
		}
	}
	return out
}

func (h *chainHarness) nextSettle(t *testing.T) *AgentEvent {
	t.Helper()
	select {
	case evt := <-h.settle:
		return evt
	case <-time.After(3 * time.Second):
		t.Fatal("no background task_settled event arrived")
		return nil
	}
}

func (h *chainHarness) noSettleWithin(d time.Duration) bool {
	select {
	case <-h.settle:
		return false
	case <-time.After(d):
		return true
	}
}

// TestTaskChain_BackgroundSettleFullChain walks a command task from spawn to a
// reclaim-visible settle and asserts EVERY downstream link agrees on the same
// task id / origin: fact record, event lineage, feedback verdict, and a
// ghost-free registry fold.
func TestTaskChain_BackgroundSettleFullChain(t *testing.T) {
	h := newChainHarness(t)

	spec := task.TaskSpec{
		Kind: "command", Desc: "build svc", Key: "chain-k1",
		Origin:      map[string]string{"trigger_source": "user", "chat_id": "c-42"},
		Declarative: &task.Declarative{Kind: "command", Desc: "build svc", Key: "chain-k1", Command: "make build", TaskID: "sess-1"},
	}
	// dense=1ms, fn returns at 40ms → detaches first → settle is a BACKGROUND
	// (post-window) settle routed through OnSettle.
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "BUILD_OK_7788", nil
	}, time.Millisecond)

	res := h.tm.Spawn(spec, det)
	require.NotNil(t, res.Task)
	require.False(t, res.Settled, "slow fn must detach first (background settle)")
	require.False(t, res.Deduped)
	id := res.Task.ID

	// LINK 1 — task_spawned record carries declarative + Origin + lifetime.
	var spawned *memory.FullEvent
	for _, e := range h.records(tagentevent.TypeTaskSpawned) {
		if e.Metadata["task_id"] == id {
			rec := e
			spawned = &rec
		}
	}
	require.NotNil(t, spawned, "task_spawned fact record must exist")
	var decl task.Declarative
	require.NoError(t, json.Unmarshal([]byte(spawned.Content), &decl))
	require.Equal(t, "make build", decl.Command)
	require.Equal(t, "c-42", decl.Origin["chat_id"], "routing baggage persisted for cross-restart restore")
	require.Equal(t, task.LifetimeJob, decl.Lifetime, "command kind → job lifetime persisted")

	// LINK 2 — the task_settled event carries lineage (NOT flagged absent).
	evt := h.nextSettle(t)
	require.Equal(t, SourceTask, evt.Source)
	require.Equal(t, "completed", evt.Metadata["settle_status"])
	require.Equal(t, id, evt.Metadata["task_id"])
	require.Equal(t, "c-42", evt.Metadata["chat_id"], "origin baggage copied onto the settle event")
	require.NotContains(t, evt.Metadata, "lineage_absent")
	require.Contains(t, evt.Message.Content, "[task settled]")
	require.Contains(t, evt.Message.Content, "BUILD_OK_7788")

	// LINK 3 — persistBusEvent wrote a machine-readable settle record AND bound
	// a deterministic positive feedback for a completed verdict.
	settledRec := false
	for _, e := range h.records(tagentevent.TypeExternalInput) {
		if e.Metadata["task_id"] == id && e.Metadata["settle_status"] == "completed" {
			settledRec = true
		}
	}
	require.True(t, settledRec, "background settle must persist an external_input record with settle_status for the registry")
	require.Len(t, h.records("feedback"), 1, "completed → exactly one feedback")
	require.Contains(t, h.records("feedback")[0].Content, `"verdict":"positive"`)

	// LINK 4 — registry fold: spawned − completed-terminal settle → 0 restored
	// (no ghost board entry / no re-fire after restart).
	restored := RebuildTaskRegistry(h.store, chainPID, task.NewTaskManager(task.TaskManagerConfig{}), nil)
	require.Zero(t, restored, "a terminal (completed) task must not resurrect as suspect")
}

// TestTaskChain_UnknownSourceIsHeld proves the "unknown 来源" safety link: a task
// spawned with NO origin baggage settles into an event explicitly marked
// lineage_absent, so the host delivery gate withholds it rather than mechanically
// routing it as a plain "task" source.
func TestTaskChain_UnknownSourceIsHeld(t *testing.T) {
	h := newChainHarness(t)
	// No Origin on the spec → the settle event must be stamped lineage_absent.
	spec := task.TaskSpec{
		Kind: "command", Desc: "orphan-ish", Key: "chain-unk",
		Declarative: &task.Declarative{Kind: "command", Desc: "orphan-ish", TaskID: "s-u"},
	}
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "done", nil
	}, time.Millisecond)
	res := h.tm.Spawn(spec, det)
	require.False(t, res.Settled)

	evt := h.nextSettle(t)
	require.Equal(t, "true", evt.Metadata["lineage_absent"], "unknown-origin settle must be flagged for host hold")
}

// TestTaskChain_InlineSettleIsRecordOnly proves the inline path (settle inside
// the sync-wait window) is registry-only: an inline settle record lands (so the
// restart replay is ghost-free) but NO task_settled bus event is published and
// NO feedback is written (the LLM already saw the result in-turn).
func TestTaskChain_InlineSettleIsRecordOnly(t *testing.T) {
	h := newChainHarness(t)
	spec := task.TaskSpec{
		Kind: "command", Desc: "quick", Key: "chain-inline",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "quick", TaskID: "s-i"},
	}
	// dense=1h, fn returns in ~0ms → settles INSIDE the window (inline).
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "inline result", nil
	}, time.Hour)
	res := h.tm.Spawn(spec, det)
	require.True(t, res.Settled, "fast fn under a long dense window settles inline")
	require.Equal(t, "inline result", res.Signal.Output)
	id := res.Task.ID

	// No background settle event fires for an inline settle.
	require.True(t, h.noSettleWithin(80*time.Millisecond), "inline settle must NOT publish a task_settled event")

	// Registry-only inline record is present and flagged.
	inlineRec := false
	for _, e := range h.records(tagentevent.TypeExternalInput) {
		if e.Metadata["task_id"] == id && e.Metadata["task_inline_record"] == "true" && e.Metadata["settle_status"] == "completed" {
			inlineRec = true
		}
	}
	require.True(t, inlineRec, "inline settle must write a registry inline record (no replay ghost)")
	require.Empty(t, h.records("feedback"), "inline settle binds no feedback (not reclaimed through the loop)")

	// Fold still excludes it (inline record is a terminal settle).
	require.Zero(t, RebuildTaskRegistry(h.store, chainPID, task.NewTaskManager(task.TaskManagerConfig{}), nil))
}

// TestTaskChain_PostTerminalLateSignalIsFenced proves "迟到信号": a second settle
// arriving after the task reached a terminal state is dropped at the watch
// signal entry — exactly one notification, never a double settle/spam.
func TestTaskChain_PostTerminalLateSignalIsFenced(t *testing.T) {
	h := newChainHarness(t)
	spec := task.TaskSpec{
		Kind: "command", Desc: "dup", Key: "chain-late",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "dup", TaskID: "s-l"},
	}
	det := task.NewManualDetectorDetach(time.Millisecond)
	res := h.tm.Spawn(spec, det)
	require.False(t, res.Settled)

	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "first"})
	_ = h.nextSettle(t) // first background settle

	// Late duplicate after terminal — must be fenced (no second event).
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "second"})
	require.True(t, h.noSettleWithin(80*time.Millisecond), "post-terminal late signal must be fenced (dropped)")

	// Exactly one feedback (the fenced late signal produced none).
	require.Len(t, h.records("feedback"), 1)
}

// TestTaskChain_ResumeRebindsUnderSameID proves "resume 换绑": resuming an
// alive-detached task installs a fresh detector and re-runs the settle lifecycle
// under the SAME task id, producing a second reclaimed settle for that id.
func TestTaskChain_ResumeRebindsUnderSameID(t *testing.T) {
	h := newChainHarness(t)

	// Resume re-opens the sync-wait window, so the new round's detector must
	// detach for Resume() to return (background round), exactly like Spawn.
	round2 := task.NewManualDetectorDetach(time.Millisecond)
	spec := task.TaskSpec{
		Kind: "command", Desc: "long svc", Key: "chain-resume",
		Origin:      map[string]string{"trigger_source": "user"},
		Declarative: &task.Declarative{Kind: "command", Desc: "long svc", TaskID: "s-r"},
		ResumeFn: func(string) (task.SettleDetector, error) {
			return round2, nil // re-bind to a fresh round's detector
		},
	}
	det1 := task.NewManualDetectorDetach(time.Millisecond)
	res := h.tm.Spawn(spec, det1)
	require.False(t, res.Settled)
	id := res.Task.ID

	// Stable settle → alive-detached (a legal resume source state).
	det1.Emit(task.SettleSignal{Kind: task.SettleStable, Output: "ready"})
	evt1 := h.nextSettle(t)
	require.Equal(t, "alive-detached", evt1.Metadata["settle_status"])
	require.Equal(t, id, evt1.Metadata["task_id"])
	svcTask, ok := h.tm.Get(id)
	require.True(t, ok)
	require.Equal(t, task.TaskAliveDetached, svcTask.Status())

	// Resume under the same id with the fresh detector.
	rr, err := h.tm.Resume(id, "continue please")
	require.NoError(t, err)
	require.NotNil(t, rr.Task)
	require.Equal(t, id, rr.Task.ID, "resume must rebind the round under the SAME task id")

	// The rebound detector settles → a second reclaim event for the same id.
	round2.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "final"})
	evt2 := h.nextSettle(t)
	require.Equal(t, id, evt2.Metadata["task_id"], "resumed round still attributes to the original id")
	require.Equal(t, "completed", evt2.Metadata["settle_status"])
	require.Contains(t, evt2.Message.Content, "final")
}
