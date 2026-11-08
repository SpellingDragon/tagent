// Package bench owns the live-benchmark task set, its task declaration and the
// machine-readable acceptance verifiers the three runners share.
//
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
//
// A task is data, not a test: the JSON fixtures under bench/tasks/ declare the
// prompts, the required capability set, the setup files, the verifier and one
// positive/negative example pair. The exported Task and Verifier surface is
// importable (features/rl/sample reuses it) and never depends on a runner.
package bench
