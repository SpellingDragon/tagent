package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
)

// TestM3_BackgroundHotApplyConcurrentWithCompress_NoRace is the code-review
// H-1 regression, migrated to the §6.4 pull shape (轮七十八): the hot numeric
// group now arrives by rotating the owner's SOURCE concurrently with a running
// Compress, and each pass must read one coherent generation. History: this族
// originally proved that a background writer may not do a naked write to
// SmartCompressor.KeepRecentTasks while compressSkeleton reads it under paramMu
// (the UpdateKeepRecent/ApplyParams push path) — those writers are gone, and what
// remains provable is that the read side stays race-free under concurrent apply.
func TestM3_BackgroundHotApplyConcurrentWithCompress_NoRace(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)
	for _, ref := range rbAgedRefs(t, store, rbNowMs()+60_000_000) {
		proj.Append(ref)
	}
	snapshot := proj.GetAll()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)

	// Writer: stands in for the background reloader's applyHotAll.
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			keep, max := 2, 60
			if i%2 == 0 {
				keep, max = 4, 16000
			}
			rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: max, KeepRecentTasks: keep})
		}
	}()

	// Reader: an in-flight turn compressing (reads SmartCompressor.KeepRecentTasks
	// under paramMu on the fold path).
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = cc.Compress(context.Background(), snapshot)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
