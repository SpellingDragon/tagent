// 本文件钉住 折叠的 run 级豁免与单条票据的有效键：未消费成员使整 run 不可折叠，
// Timestamp==0 的降级通知票据必须携带有效合成键。
package compress

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestFoldSettleRuns_ActiveMembersExemptWholeRun 钉住 首轮压缩里 ≥2 条相邻且均未
// 被消费（Active）的通知：整 run 原样保留——不折叠、不截断。豁免是 run 级的：
// 逐条豁免会把未消费的通知关进票据卡，违背 Active 不可丢的通道层契约。
func TestFoldSettleRuns_ActiveMembersExemptWholeRun(t *testing.T) {
	cc := newFoldCC(2)
	refs := settleRefs(101, 3, 200)
	d := map[int64]int8{101: TelemActive, 102: TelemActive, 103: TelemActive}
	folded := cc.foldSettleRuns(refs, d)
	require.Equal(t, refs, folded, "an all-Active run must pass through intact")
	require.False(t, hasSettleFold(folded))
}

// TestFoldSettleRuns_ActiveMidMemberExemptsRun 钉住 混合 run（已消费+未消费相邻）
// 同样整 run 保留：折叠动作对 run 全体生效，任一 Active 成员即否决。
func TestFoldSettleRuns_ActiveMidMemberExemptsRun(t *testing.T) {
	cc := newFoldCC(2)
	refs := settleRefs(111, 3, 200)
	d := map[int64]int8{111: TelemDemote, 112: TelemActive, 113: TelemInternal}
	folded := cc.foldSettleRuns(refs, d)
	require.Equal(t, refs, folded, "one Active member exempts the whole run")
}

// TestFoldSettleRuns_AllDemoteRunFolds 钉住 无 Active 成员的 run 正常折叠为单卡。
func TestFoldSettleRuns_AllDemoteRunFolds(t *testing.T) {
	cc := newFoldCC(2)
	refs := settleRefs(121, 3, 200)
	d := map[int64]int8{121: TelemDemote, 122: TelemDemote, 123: TelemDemote}
	folded := cc.foldSettleRuns(refs, d)
	require.Len(t, folded, 1)
	require.Equal(t, tagentevent.TypeSettleFold, folded[0].EventType)
}

// TestFoldSettleRuns_SingleDemoteZeroTimestampKeepsValidTicket 钉住 单条降级且
// Timestamp==0（历史/合成 ref）时票据仍携带有效负键——EventKey=0 会被
// buildRetainedRefs 当无效键静默丢弃，观测就此消失。
func TestFoldSettleRuns_SingleDemoteZeroTimestampKeepsValidTicket(t *testing.T) {
	cc := newFoldCC(2)
	ref := settleRef(131, 0)
	d := map[int64]int8{131: TelemDemote}
	folded := cc.foldSettleRuns([]memory.EventReference{ref}, d)
	require.Len(t, folded, 1)
	require.Equal(t, tagentevent.TypeSettleFold, folded[0].EventType)
	require.Negative(t, folded[0].EventKey, "the synthetic ticket key must be valid (non-zero)")
	require.Positive(t, folded[0].Timestamp)
}
