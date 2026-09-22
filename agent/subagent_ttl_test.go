package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// resident-review-fixes 4.2: the sub-agent lifetime self-service channel. Four
// legs — explicit ttl takes effect, omitted defers to the configured default
// (three-level chain), negative is rejected before spawn, and the ttl is
// persisted on the Declarative projection so the board and the cross-restart
// replay agree with the reaper anchor.
func ttlArgs(t *testing.T, extra map[string]any) []byte {
	t.Helper()
	m := map[string]any{"request": "do the thing"}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// spawnOneSlowSubagent runs a sub-agent that exceeds the sync-wait window (so it
// is spawned as a background task, not settled inline) and returns the tracked task.
func spawnOneSlowSubagent(t *testing.T, w *AgentToolWrapper, args []byte) (*task.TaskManager, error) {
	t.Helper()
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)
	_, err := w.Call(ctx, args)
	return tm, err
}

func TestSubagentTTL_ExplicitTakesEffect(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 300 * time.Millisecond, output: "LATE"}, "t", nil, nil)
	w.SetAsyncDenseDuration(40 * time.Millisecond) // force ack/background so the task is tracked
	tm, err := spawnOneSlowSubagent(t, w, ttlArgs(t, map[string]any{"ttl": 90}))
	require.NoError(t, err)

	tasks := tm.List()
	require.Len(t, tasks, 1, "one subagent task tracked")
	require.Equal(t, 90*time.Second, tasks[0].Spec.TTL, "explicit ttl must set TaskSpec.TTL")
	require.Equal(t, "90", tasks[0].Spec.Declarative.Params["ttl"], "ttl must persist on the Declarative projection for replay")
	// The board reads Spec.TTL for every kind, so the rendered remaining lifetime
	// reflects the self-set anchor (kanban consistency follows from the shared render).
	require.NotEmpty(t, task.RenderBoard(tasks, 10*time.Minute))

	// fail-before: without wiring ttlSeconds into the spawn, spec.TTL is 0 and the
	// Declarative carries no ttl key → this task is stuck on the reaper floor.
}

func TestSubagentTTL_OmittedDefersToDefault(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 300 * time.Millisecond, output: "LATE"}, "t", nil, nil)
	w.SetAsyncDenseDuration(40 * time.Millisecond)
	tm, err := spawnOneSlowSubagent(t, w, ttlArgs(t, nil))
	require.NoError(t, err)

	tasks := tm.List()
	require.Len(t, tasks, 1)
	require.Zero(t, tasks[0].Spec.TTL, "omitted ttl leaves spec.TTL unset → manager configured default / 10m floor governs")
	require.NotContains(t, tasks[0].Spec.Declarative.Params, "ttl", "no ttl key persisted when omitted")
}

func TestSubagentTTL_ZeroMeansDefault(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 300 * time.Millisecond, output: "LATE"}, "t", nil, nil)
	w.SetAsyncDenseDuration(40 * time.Millisecond)
	tm, err := spawnOneSlowSubagent(t, w, ttlArgs(t, map[string]any{"ttl": 0}))
	require.NoError(t, err, "ttl=0 is valid (means omit → default), never an error")
	require.Zero(t, tm.List()[0].Spec.TTL)
}

func TestSubagentTTL_NegativeRejectedBeforeSpawn(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 20 * time.Millisecond, output: "X"}, "t", nil, nil)
	w.SetAsyncDenseDuration(40 * time.Millisecond)
	tm, err := spawnOneSlowSubagent(t, w, ttlArgs(t, map[string]any{"ttl": -5}))
	require.Error(t, err, "a negative ttl must be rejected")
	require.Contains(t, err.Error(), "ttl")
	require.Empty(t, tm.List(), "no task is spawned when the ttl is rejected")
}

func TestSubagentTTL_NonIntegerRejected(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 20 * time.Millisecond, output: "X"}, "t", nil, nil)
	tm, err := spawnOneSlowSubagent(t, w, []byte(`{"request":"do the thing","ttl":"soon"}`))
	require.Error(t, err, "a non-integer ttl must be rejected")
	require.Empty(t, tm.List())
}

func TestSubagentTTL_DeclarationExposesTTL(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan"}, "t", nil, nil)
	props := w.Declaration().InputSchema.Properties
	require.Contains(t, props, "ttl", "the sub-agent tool schema must expose the ttl parameter")
	require.Equal(t, "integer", props["ttl"].Type)
}
