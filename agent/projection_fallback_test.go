package agent

import (
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"

	"github.com/stretchr/testify/require"
)

// fbChainStore lays n agent-output events on the fact chain.
//
// Keys MUST be real Snowflake keys with partition bits = rbPid: the store's
// GetEvents resolves the partition FROM THE KEY BITS, so raw-ms keys make
// events land under partition rbPid while key-lookup decodes partition 0 —
// the fallback tail fetch would silently return empty (0 refs).
// Returns issued keys in write order (monotonic, NOT +1-consecutive).
func fbChainStore(t *testing.T, store memory.MemoryStore, n int, startMs int64) []int64 {
	t.Helper()
	keys := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		k := memory.NewSnowflakeEventKey(rbPid, startMs+int64(i))
		rbStore(t, store, k, tagentevent.TypeAgentOutput,
			fmt.Sprintf("fallback summary %d", i),
			fmt.Sprintf("fallback full content %d", i),
			startMs+int64(i), nil)
		keys = append(keys, k)
	}
	return keys
}

func TestRebuildFallback_NoAnchor_RecoversTail(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)
	const n = 8
	keys := fbChainStore(t, store, n, rbNowMs())

	cm.rebuildProjectionFromWAL() // snapKey==0 → fallback branch

	require.Equal(t, n, proj.Len(), "fallback must recover ALL chain events into the projection")
	got := proj.GetAll()
	require.Equal(t, keys[0], got[0].EventKey)
	require.Equal(t, keys[n-1], got[n-1].EventKey)
	require.Equal(t, tagentevent.TypeAgentOutput, got[n-1].EventType)
}

func TestRebuildFallback_NoCap_RecoversFullChain(t *testing.T) {
	// 2026-09-16 host directive: replay cap removed — a chain that fit before a
	// restart must fit after it. 505 events (> legacy cap 500) must ALL recover.
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)
	const n = 505 // > legacy fallbackCap=500, would have truncated before
	keys := fbChainStore(t, store, n, rbNowMs())

	cm.rebuildProjectionFromWAL()

	require.Equal(t, n, proj.Len(), "fallback must recover the FULL chain, no cap")
	got := proj.GetAll()
	require.Equal(t, keys[0], got[0].EventKey, "oldest event must survive")
	require.Equal(t, keys[n-1], got[n-1].EventKey)
}

func TestRebuildFallback_EmptyChain_StaysEmpty(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)

	cm.rebuildProjectionFromWAL()
	require.Equal(t, 0, proj.Len())
}

// TestRebuildFallback_FilterBeforeCap（resident-readiness-plan 3.9，经 b871d30
// 宿主指令修订）：无锚回放**全链存活**（fallback cap 已移除——重启后完整历史
// 必须可复原），但过滤次序不变：task_spawned 等非投影记录永不占用投影槽位。
func TestRebuildFallback_FilterBeforeCap(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, _ := rbFoldCM(store, proj, 4)

	// 600 条有效 + 600 条非投影（task_spawned）交错写入。
	base := rbNowMs()
	for i := 0; i < 600; i++ {
		ms := base + int64(2*i)
		k := memory.NewSnowflakeEventKey(rbPid, ms)
		rbStore(t, store, k, tagentevent.TypeAgentOutput,
			fmt.Sprintf("valid %d", i), fmt.Sprintf("content %d", i), ms, nil)
		k2 := memory.NewSnowflakeEventKey(rbPid, ms+1)
		rbStore(t, store, k2, tagentevent.TypeTaskSpawned,
			fmt.Sprintf("spawn %d", i), fmt.Sprintf("spawn-body %d", i), ms+1, nil)
	}

	cm.rebuildProjectionFromWAL()

	// b871d30：cap 移除——600 有效全部保留（先过滤后保留的次序仍由断言固化）。
	require.Equal(t, 600, proj.Len(), "cap removed: full chain survives restart")
	got := proj.GetAll()
	for _, ref := range got {
		require.NotEqual(t, tagentevent.TypeTaskSpawned, ref.EventType,
			"internal records must not occupy projection slots")
	}
	// 无 cap：无截断，VERDICT=FULL。
	rec := cm.RecoveryResult()
	require.NotNil(t, rec)
	require.Equal(t, "fallback", rec.Mode)
	require.Equal(t, 0, rec.Truncated, "cap removed: nothing truncated")
	require.Equal(t, "full", rec.Status)
	require.Empty(t, cm.TakeRecoveryNotice(), "full recovery has no model-facing notice")
}
