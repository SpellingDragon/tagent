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
