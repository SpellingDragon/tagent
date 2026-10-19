// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package bench

import (
	"sync/atomic"
	"testing"
)

// BenchmarkAssemble measures one formal assembly and its first publication.
func BenchmarkAssemble(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		assemble(b)
	}
}

// BenchmarkAssembleInvoke measures the whole admitted chain of one turn: the
// formal assembly, the publication and the invocation that follows it.
func BenchmarkAssembleInvoke(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		run := assemble(b)
		drive(b, run, i, rotatingSession(i))
	}
}

// BenchmarkInvokeTurn measures one turn on a resident run, each on a session of
// its own so no transcript crosses turns.
func BenchmarkInvokeTurn(b *testing.B) {
	b.ReportAllocs()
	run := assemble(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drive(b, run, i, uniqueSession(i))
	}
}

// BenchmarkInvokeSharedSession measures turns that share a bounded session set,
// so each request carries the transcript those sessions accumulated. The reading
// grows with the retained history; it reports a trend under one -benchtime, not a
// throughput baseline.
func BenchmarkInvokeSharedSession(b *testing.B) {
	b.ReportAllocs()
	run := assemble(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drive(b, run, i, rotatingSession(i))
	}
}

// BenchmarkInvokeParallel measures turns driven by concurrent callers on one
// resident run, each on its own session.
func BenchmarkInvokeParallel(b *testing.B) {
	b.ReportAllocs()
	run := assemble(b)
	var sequence int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := int(atomic.AddInt64(&sequence, 1))
			drive(b, run, n, uniqueSession(n))
		}
	})
}
