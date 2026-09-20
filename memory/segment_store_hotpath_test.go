package memory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStoreEvent_NormalWriteDoesNotScanHistory locks the §2.9 hot-path
// invariant (design 决策3 / event-segment-store spec L7: "公共新写正常路径
// 不扫描全历史；较贵定位只在内部恢复、窗口首次恢复或不确定修复执行"). A
// steady-state ordinary write into an already-open window must touch the KV
// layer with idx point lookups + evt/idx puts + the durability barrier ONLY —
// never a segment/history scan. The expensive locateOrphanEvtSlot/scanLiveKeys
// scans belong to the recovery path (covered in §2.3/§2.5), not the write path.
//
// The faultKV spy records every KVScan as "<path>:read:scan:<prefix>", so the
// assertion is non-vacuous: the same harness's positive control (the barrier +
// the two puts) proves the commit really ran while we require zero scans.
func TestStoreEvent_NormalWriteDoesNotScanHistory(t *testing.T) {
	s := newFaultStore(t)

	// Seed pre-existing history in DIFFERENT windows and partitions, so that a
	// (buggy) write path that scanned everything would observably hit them.
	pidOther := PartitionIDFromName("other")
	oldHour := int64(1_600_000_000)
	for i := 0; i < 3; i++ {
		k := NewSnowflakeEventKey(pidOther, oldHour*1000)
		require.NoError(t, s.StoreEvent(k, commitEvent(k, pidOther, fmt.Sprintf("old-%d", i))))
	}

	// Open the target partition's current window with one warm-up write. That
	// first-in-window write legitimately runs the single window-scoped seq
	// recovery scan (bounded to this one window prefix, at most once per
	// window switch) — it happens before clearSpy and is NOT the subject here.
	pid := PartitionIDFromName("hot")
	base := int64(1_700_000_000) // same hour for every target write → same window
	kWarm := NewSnowflakeEventKey(pid, base*1000)
	require.NoError(t, s.StoreEvent(kWarm, commitEvent(kWarm, pid, "warm")))

	// Measure a steady-state write into the already-open window (seqCounter > 0).
	s.kv.clearSpy()
	k := NewSnowflakeEventKey(pid, base*1000)
	require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, "measured")))

	spy := s.kv.spyLines()
	// Positive control: the measured write really ran the commit path, so the
	// zero-scan assertion below cannot pass vacuously.
	require.GreaterOrEqual(t, countSpy(spy, "store:sync"), 1,
		"measured write must reach the durability barrier")
	require.GreaterOrEqual(t, countSpy(spy, "store:write"), 2,
		"measured write must KVPut the evt and idx slots")
	require.GreaterOrEqual(t, countSpy(spy, "store:read:"), 1,
		"measured write must probe the idx key (point lookup)")

	// The invariant: a normal write performs NO history/segment scan.
	require.Equal(t, 0, countSpy(spy, "store:read:scan:"),
		"ordinary write must not scan history; got spy=%v", spy)
}

// TestStoreEvent_WindowSwitchScanIsWindowBounded complements the above by
// proving the one permitted scan (first-in-window seq recovery, D12) is bounded
// to a single window prefix rather than iterating the whole store: even with
// history spread across many windows/partitions, a fresh-window write emits at
// most one scan op.
func TestStoreEvent_WindowSwitchScanIsWindowBounded(t *testing.T) {
	s := newFaultStore(t)

	// Spread existing data over several distinct hours and partitions.
	pid := PartitionIDFromName("bounded")
	for h := 0; h < 4; h++ {
		for j := 0; j < 2; j++ {
			k := NewSnowflakeEventKey(pid, int64(1_600_000_000+h*3600)*1000)
			require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, fmt.Sprintf("h%d-j%d", h, j))))
		}
	}

	// A write into a brand-new (never-opened) window recovers its seq with at
	// most ONE window-scoped scan — never a per-existing-window scan.
	s.kv.clearSpy()
	k := NewSnowflakeEventKey(pid, int64(1_600_000_000+9*3600)*1000)
	require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, "new-window")))

	scanOps := countSpy(s.kv.spyLines(), "store:read:scan:")
	require.LessOrEqual(t, scanOps, 1,
		"a fresh-window write must scan at most its own single window, got %d scan ops", scanOps)
}
