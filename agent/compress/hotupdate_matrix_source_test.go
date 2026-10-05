// hotupdate_matrix_source_test 承载热更矩阵的源面红线：数值组必须被真实压实边界消费，读数可查不算抵达。
//
// - 矩阵里 keepRecent 一行只有读数级证据，本文件补上行为级：同一份内容、两个独立压缩器、源里的热值是唯一变量；
// - 阈值与预算线的行为级断言已由既有用例承担，本文件不重复钉那些维度，只钉缺的那一格。
// 契约: docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity
package compress

import (
	"context"
	"strings"
	"testing"

	memory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// matrixHotTurnCount is how many complete turns one compaction run is given:
// enough turns that the aging window is the thing deciding the result size.
const matrixHotTurnCount = 8

// hotKeepRecentCompaction runs one real compaction over matrixHotTurnCount
// complete turns, with hotKeepRecent as the only variable inside the pulled
// numeric group, and reports how many messages the compaction actually kept.
func hotKeepRecentCompaction(t *testing.T, hotKeepRecent int) int {
	t.Helper()
	store := memory.NewInMemoryStore()
	cur := &HotNumbers{}
	cc := rotatingCC(cur,
		NewSmartCompressor(WithKeepRecentTasks(1)), store, 600, 0.8, 1)

	for i := 0; i < matrixHotTurnCount; i++ {
		storeTurn(t, store, strings.Repeat("payload-", 40)+string(rune('a'+i)))
	}
	refs := allRefs(t, store)
	require.Lenf(t, refs, matrixHotTurnCount*2, "every turn must store its two events")

	*cur = HotNumbers{ThresholdPct: 0.8, MaxTokens: 600, KeepRecent: hotKeepRecent}
	res := cc.Compress(context.Background(), refs)
	require.Truef(t, res.Compressed,
		"over-budget turns must be compacted (keepRecent=%d), got a pass-through", hotKeepRecent)
	return len(res.Messages)
}

// TestHotSourceKeepRecentReachesRealCompaction 钉住 源里轮转的 keepRecent 必须改变真实压实结果，而不只是改变读数。
// - 窄窗与宽窗之间保留下来的消息条数必须严格拉开，否则这个热值是"源里有而消费点没读"的假热更。
// 契约: docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity
func TestHotSourceKeepRecentReachesRealCompaction(t *testing.T) {
	tight := hotKeepRecentCompaction(t, 1)
	wide := hotKeepRecentCompaction(t, 5)
	require.Greaterf(t, wide, tight,
		"hot keepRecent must reach the real compaction: keepRecent=1 kept %d messages, keepRecent=5 kept %d", tight, wide)
}
