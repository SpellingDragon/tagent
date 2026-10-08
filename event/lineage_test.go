// 契约: docs/wiki/agent/event-flow.md#event-stream-overview
//
// 谱系投递白名单作为单一真源的取值形状：白名单外一律内部（fail-closed），自管判定由同白名单派生。
// meditation/consolidation 显式登记自管（前者可外显宿主、后者纯内部唤醒），投递白名单只管宿主可见性。
package event

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeliverableLineage_Whitelist(t *testing.T) {
	for _, ts := range []string{"user", "task", "reincarnation", "system_alert", "meditation"} {
		require.True(t, DeliverableLineage(ts), "whitelist member %q must be deliverable", ts)
	}
	for _, ts := range []string{"task-unstamped", "task-retired", "unknown", "", "weird", "user-x", "USER", LineageConsolidationHint} {
		require.False(t, DeliverableLineage(ts), "non-whitelist %q must be withheld (fail-closed): negative-list-era strays, retired-signal lineage, empty, the consolidation wake signal and any unknown future value", ts)
	}
}

// TestSelfManagedLineage_DerivesFromSameSource 钉住 audit 判定与投递白名单同源。
// - 对任意既非冥想亦非巩固的谱系，自管 = 不可投递（恰为反）。
// - 显式登记的两谱系恒自管：冥想保留第二层（可外显仍自管），巩固纯内部唤醒。
func TestSelfManagedLineage_DerivesFromSameSource(t *testing.T) {
	require.True(t, SelfManagedLineage("meditation"), "meditation keeps its self-initiated layer")
	require.True(t, DeliverableLineage("meditation"), "and its host-visible layer")
	for _, ts := range []string{"user", "task", "reincarnation", "system_alert",
		"task-unstamped", "task-retired", "unknown", "", "future-value"} {
		require.Equal(t, !DeliverableLineage(ts), SelfManagedLineage(ts),
			"self-managed must be the exact complement of deliverable for %q", ts)
	}
	for _, ts := range []string{LineageMeditation, LineageConsolidationHint} {
		require.True(t, SelfManagedLineage(ts), "%q is explicitly registered self-managed, independent of the deliverable whitelist", ts)
	}
}

// TestSelfManagedLineage_ConsolidationHintRegistered 钉住既有容量路谱系的显式登记。
func TestSelfManagedLineage_ConsolidationHintRegistered(t *testing.T) {
	require.Equal(t, "consolidation_hint", LineageConsolidationHint)
	require.True(t, SelfManagedLineage(LineageConsolidationHint), "consolidation_hint must be self-managed by explicit registration")
	require.False(t, DeliverableLineage(LineageConsolidationHint), "consolidation_hint is an internal wake signal, never host-deliverable")
}
