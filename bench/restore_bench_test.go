// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package bench

import (
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// BenchmarkRestoreLatency measures the restart assembly over a durable fact
// log: the setup commits one task per invocation, the timed iteration
// re-assembles the same root and folds every committed chain back.
func BenchmarkRestoreLatency(b *testing.B) {
	for _, facts := range []int{0, 1, 64, 256} {
		b.Run(fmt.Sprintf("facts=%d", facts), func(b *testing.B) {
			storage := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: b.TempDir()}
			writer := assemble(b, tagent.WithStorage(storage))
			for i := 0; i < facts; i++ {
				drive(b, writer, i, event.SessionID(fmt.Sprintf("restore-%d", i)))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				restarted := assemble(b, tagent.WithStorage(storage))
				if report := restarted.Recovery(); len(report.Tasks) != facts {
					b.Fatalf("restored tasks = %d, want %d", len(report.Tasks), facts)
				}
			}
		})
	}
}
