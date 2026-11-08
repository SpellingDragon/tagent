// Package runners owns the three live-benchmark runners over one Task interface.
//
// Baseline (1.3), tagent's bench-bridge subprocess (1.4) and DeepSeek Harness
// (1.4) land here. The package deliberately exports no named type yet: the runner
// contract is bench.Task and this package may import bench, never the reverse.
//
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners
