// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
//
// 结算事件谱系取值优先级：信号级 Lineage > spawn 时 Origin；Origin 不因退役被改写。
package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestResumeSettleKeepsOriginalLineage 钉住谱系取值优先级与 Origin 不可变性。
// - 退役结算只在信号上盖谱系；Spec.Origin 保持 spawn 原值，resume 后的结算继承原值。
// - 事件取值：信号优先，无信号回落 Origin。
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
