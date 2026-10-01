// 本文件钉住 谱系投递白名单作为单一真源的取值形状：白名单外一律内部（fail-closed），
// 自管判定由同一白名单派生，双向对同一输入必须一致。
package event

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeliverableLineage_Whitelist(t *testing.T) {
	for _, ts := range []string{"user", "task", "reincarnation", "system_alert", "meditation"} {
		require.True(t, DeliverableLineage(ts), "whitelist member %q must be deliverable", ts)
	}
	// 白名单外一律内部：负名单时代漏配的谱系（task-unstamped）、退役信号谱系、
	// 空值与任何未知未来值全部扣留（fail-closed，unknown-withhold 同向）。
	for _, ts := range []string{"task-unstamped", "task-retired", "unknown", "", "weird", "user-x", "USER"} {
		require.False(t, DeliverableLineage(ts), "non-whitelist %q must be withheld (fail-closed)", ts)
	}
}

// TestSelfManagedLineage_DerivesFromSameSource 钉住 audit 判定与投递白名单同源：
// 对任意非冥想谱系，自管 = 不可投递（恰为反）；冥想保留第二层语义——可外显仍自管。
func TestSelfManagedLineage_DerivesFromSameSource(t *testing.T) {
	require.True(t, SelfManagedLineage("meditation"), "meditation keeps its self-initiated layer")
	require.True(t, DeliverableLineage("meditation"), "and its host-visible layer")
	for _, ts := range []string{"user", "task", "reincarnation", "system_alert",
		"task-unstamped", "task-retired", "unknown", "", "future-value"} {
		require.Equal(t, !DeliverableLineage(ts), SelfManagedLineage(ts),
			"self-managed must be the exact complement of deliverable for %q", ts)
	}
}
