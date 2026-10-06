// 本文件负责 Declarative 作为可序列化投影对 per-call 视图覆盖的冻结义务：覆盖随
// task_spawned 事实入账、随折叠还原、重入以还原后的记录为唯一输入重放出同一视图，
// 且与 Params 的标量分工互不合并。
// 契约: docs/wiki/platform/org-hot-reload.md#percall-overrides
package task

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// settledDetector returns a manually driven detector whose first signal has
// already landed, so Spawn takes its synchronous settle window and returns with
// the task in a terminal state — no watcher goroutine left running behind the test.
func settledDetector() *ManualDetector {
	d := NewManualDetector()
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "settled"})
	d.Done()
	return d
}

// TestDeclarative_OverridesArePartOfTheSerializableProjection 钉住冻结面：Declarative.Overrides 是记录的一部分。
// - 序列化后逐字段还原，视图覆盖与标量 knob 各归其位；
// - 未携带覆盖的记录不新增载荷字段，读出仍是无覆盖。
func TestDeclarative_OverridesArePartOfTheSerializableProjection(t *testing.T) {
	decl := Declarative{
		Kind:        "subagent",
		Desc:        "blank: work",
		Key:         "blank:work",
		AgentName:   "blank",
		MessageBody: "work",
		Params:      map[string]string{"ttl": "600"},
		Overrides: &Overrides{
			SystemPrompt: "OVERRIDE-PROMPT",
			ModelRef:     "provider/registered-model",
			ToolsSubset:  []string{"read_file"},
		},
	}
	data, err := json.Marshal(decl)
	require.NoError(t, err)

	var restored Declarative
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, decl.Overrides, restored.Overrides,
		"the frozen view must come back field for field")
	require.Equal(t, decl.Params, restored.Params,
		"scalar knobs stay in Params; the two faces never merge into one bag")

	plain, err := json.Marshal(Declarative{Kind: "subagent", AgentName: "blank"})
	require.NoError(t, err)
	require.NotContains(t, string(plain), "overrides",
		"a delegation that carried no override must record no override payload")
	var bare Declarative
	require.NoError(t, json.Unmarshal(plain, &bare))
	require.Nil(t, bare.Overrides)
	require.True(t, bare.Overrides.IsEmpty(),
		"and an absent payload resolves to the generation's own view")
}

// TestDeclarative_RelaunchReplaysTheFoldedOverrides 钉住重放面：一次重入的覆盖来自还原后的记录，而不是任何活的对象。
// - 折叠（序列化后反序列化）交给重入的 Declarative 必须带着原覆盖；
// - 由它重放出的新任务，视图覆盖与原调用逐字段相同；
// - task_spawned 事实的 content 就是整个 Declarative，重建折叠解码回的那条记录是重入的唯一视图来源。
func TestDeclarative_RelaunchReplaysTheFoldedOverrides(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	original := Declarative{
		Kind:        "subagent",
		Desc:        "blank: work",
		AgentName:   "blank",
		MessageBody: "work",
		Overrides:   &Overrides{SystemPrompt: "OVERRIDE-PROMPT", ToolsSubset: []string{"read_file"}},
	}

	content, err := json.Marshal(original)
	require.NoError(t, err)
	var folded Declarative
	require.NoError(t, json.Unmarshal(content, &folded))
	require.Equal(t, original.Overrides, folded.Overrides,
		"the fold hands a re-entry the frozen view")

	replayed := make([]*Overrides, 0, 1)
	res := tm.Spawn(TaskSpec{
		Kind:        folded.Kind,
		Desc:        folded.Desc,
		Declarative: &folded,
		Relaunch: func(context.Context) (SpawnResult, error) {
			replay := folded
			replayed = append(replayed, replay.Overrides)
			next := folded
			return tm.Spawn(TaskSpec{
				Kind:        next.Kind,
				Desc:        next.Desc + " (replay)",
				Declarative: &next,
			}, settledDetector()), nil
		},
	}, settledDetector())
	require.NotNil(t, res.Task)
	require.Equal(t, original.Overrides, res.Task.Spec.Declarative.Overrides,
		"the spawned task's record keeps the override")

	re, err := tm.Relaunch(context.Background(), res.Task.ID)
	require.NoError(t, err)
	require.NotNil(t, re.Task)
	require.Len(t, replayed, 1, "the relaunch closure ran once")
	require.Equal(t, original.Overrides, replayed[0],
		"it read the override off the folded record, the same source an in-process relaunch replays")
	require.Equal(t, original.Overrides, re.Task.Spec.Declarative.Overrides,
		"and the re-spawned task carries the same view, so a second relaunch stays faithful too")
}
