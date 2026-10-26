// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package bench

import (
	"runtime"
	"testing"
)

// BenchmarkResidentMemory measures the heap a resident run keeps after
// assembly and the allocation volume of one turn driven on it, each turn on a
// session of its own.
func BenchmarkResidentMemory(b *testing.B) {
	b.ReportAllocs()
	run := assemble(b)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drive(b, run, i, uniqueSession(i))
	}
	b.StopTimer()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	const mib = 1 << 20
	b.ReportMetric(float64(after.HeapAlloc)/mib, "heap-MiB")
	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/mib, "retained-MiB")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(b.N)/mib, "alloc-MiB/op")
}
