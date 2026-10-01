// 本文件钉住 结算事件谱系取值优先级：信号级 Lineage > spawn 时 Origin；Origin 不因
// 退役被污染，resume 后的结算谱系保持原值。
package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestResumeSettleKeepsOriginalLineage 钉住 退役结算只在信号上盖谱系：修前
// finalizeRetired 把 "task-retired" 写进 Spec.Origin，resume 后同一任务的再次
// 结算事件继承被污染的谱系且永不恢复。修后 Origin 保持 spawn 原值，事件取值
// 信号优先、无信号回落 Origin。
func TestResumeSettleKeepsOriginalLineage(t *testing.T) {
	tk := &task.Task{Spec: task.TaskSpec{
		Kind: "command", Desc: "job",
		Origin: map[string]string{tagentevent.MetaKeyTriggerSource: "host-user"},
	}}

	retired := newTaskSettledEvent(tk, task.SettleSignal{
		Kind: task.SettleFailed, Output: "ttl-expired", Lineage: task.LineageRetired,
	}, 0, "")
	require.Equal(t, task.LineageRetired, retired.Metadata[tagentevent.MetaKeyTriggerSource],
		"the retirement settle attributes lineage from the signal")
	require.Equal(t, "host-user", tk.Spec.Origin[tagentevent.MetaKeyTriggerSource],
		"a retired settle leaves Origin untouched for future resumes")

	resumed := newTaskSettledEvent(tk, task.SettleSignal{
		Kind: task.SettleCompleted, Output: "done after resume",
	}, 0, "")
	require.Equal(t, "host-user", resumed.Metadata[tagentevent.MetaKeyTriggerSource],
		"a post-resume settle without signal lineage keeps the original provenance")
}
