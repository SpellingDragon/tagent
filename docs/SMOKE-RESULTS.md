# Smoke and throughput evidence / 冒烟与吞吐证据

Measured: 2026-11-01 (Asia/Shanghai). This is a development-snapshot measurement, not release certification.

## Version and environment

- Code: [`21ba86a371d1c932aec4d49ee49ee9dadaf832a7`](https://github.com/SpellingDragon/tagent/tree/21ba86a371d1c932aec4d49ee49ee9dadaf832a7), the inspected `next` head. Documentation may live on a different branch; these results refer only to this code SHA.
- Go 1.24.1; linux/amd64; reported CPU: AMD EPYC 9V74 80-Core Processor.
- `GOMAXPROCS=2`; compilation used `GOFLAGS=-p=2`. This does not mean the physical machine has only two cores.
- Real example-host assembly and invocation path; local scripted HTTP model; recording output channel. No real model, embedding or WeChat credentials are used by these benchmarks.
- Six benchmark cases; 30 operations per case, repeated three times. Reported median is the median of the three ns/op observations; throughput = `1e9 / median_ns`.

## Reproduce

Use a clean checkout of the measured version, with Go 1.24.1+ and the example's required command tools available:

```bash
git clone https://github.com/SpellingDragon/tagent.git tagent-measure
cd tagent-measure
git checkout --detach 21ba86a371d1c932aec4d49ee49ee9dadaf832a7
go build ./...
cd examples/cli-host
go run . -c hello
cd ../wechat-bot
GOMAXPROCS=2 GOFLAGS=-p=2 go test -tags smoke ./... \
  -run '^TestLocalSmoke$' -count=1 -timeout 5m -v
GOMAXPROCS=2 GOFLAGS=-p=2 go test -tags smoke ./... \
  -run '^$' \
  -bench '^Benchmark(SyncRound|SettleTurnaround|FoldRound|ConcurrentSessions)$' \
  -benchtime=30x -count=3 -benchmem
```

The long smoke currently fails as described below. Run the benchmark separately so a smoke failure cannot be hidden by a later successful command. Definitions: [smoke test](../examples/wechat-bot/smoke_test.go), [benchmark](../examples/wechat-bot/smoke_bench_test.go), [harness](../examples/wechat-bot/smoke_harness_test.go). These relative links show the documentation branch; the code SHA above pins the measured source.

## Outcomes

| Check | Observed result |
|---|---|
| `go build ./...` | PASS, exit 0 |
| CLI `go run . -c hello` | PASS, `mock reply: hello` |
| Six benchmark cases × three repetitions | PASS, exit 0; benchmark package time 6.284s |
| Entire `TestLocalSmoke` | **FAIL**, 124.63s; reflection summary did not arrive within 2 minutes, reflect requests=0 |
| Same-session slow-tool / cross-source consumer anchors | **Not passing**: slow-tool assertion failed; the combined 30s-bounded run timed out during the batching case |
| Real provider nightly inspected | **SKIPPED** for LLM judge and embedding; the credential classifier's success is not a provider pass |

The provider status was checked in [run 41137052603](https://github.com/SpellingDragon/tagent/actions/runs/41137052603), source `c111d7a`. That is historical main-branch evidence and is separate from the local next-snapshot measurements.

## Long-smoke observations before failure

The pump and folding assertions ran before the reflection failure:

| Observation | Recorded value |
|---|---:|
| Pump rounds | 300 |
| Recorded requests | 469 |
| Session switches / stable bindings | 29 / 3 |
| Delegations with admission and terminal follow-up | 45 |
| Delivery reviews held, approved and delivered | 15 |
| File replies | 30 |
| Long-output fallback replies | 30 |
| Sessions with history-fold assertions | 3 |
| Sampled goroutine range | 15–15 |
| Sampled heap, first → last | 3,514,368 → 24,731,648 bytes |
| Reported heap slope | 80,344 bytes/round |

This is partial progress inside a failing test. Later trajectory checks were not reached. The heap observations do not establish bounded long-term memory, and the pump counters do not prove restart recovery in this run.

Failure excerpt:

```text
smoke_test.go:189: no meditation summary landed in a user session chain within 2m0s (reflect requests=0)
--- FAIL: TestLocalSmoke (124.63s)
FAIL    wechat-bot    124.643s
```

The cause of the reflection failure needs a separate investigation; this report records the observed failure without asserting a root cause or a fix.

## Throughput summary

| Benchmark | Median ns/op | Median ms/op | Derived op/s |
|---|---:|---:|---:|
| SyncRound | 4,091,215 | 4.091 | 244.4 |
| SettleTurnaround | 27,052,752 | 27.053 | 37.0 |
| FoldRound/fold=off | 4,488,603 | 4.489 | 222.8 |
| FoldRound/fold=on | 3,217,827 | 3.218 | 310.8 |
| ConcurrentSessions/threads-3 | 2,526,754 | 2.527 | 395.8 |
| ConcurrentSessions/threads-10 | 3,856,902 | 3.857 | 259.3 |

The sync path includes answer generation at the scripted endpoint and host delivery. SettleTurnaround includes delegation through its terminal follow-up. Concurrent-session ns/op is aggregate benchmark time per completed operation, not an individual request's latency percentile.

These small, short runs characterize the smoke harness under this environment. They are not sustained production capacity, real-model tokens/s, a comparison with another framework, or evidence that folding is always faster. Window size, accumulated history, scheduling and warm-up differ across cases. The previous README's 1,142 rounds/s is not carried forward without equivalent version/environment evidence.

## Raw benchmark observations

```text
goos: linux
goarch: amd64
pkg: wechat-bot
cpu: AMD EPYC 9V74 80-Core Processor
BenchmarkSyncRound-2                                  30    4091215 ns/op    809656 B/op     3875 allocs/op
BenchmarkSyncRound-2                                  30    4190972 ns/op    780629 B/op     3866 allocs/op
BenchmarkSyncRound-2                                  30    3051641 ns/op    775073 B/op     3862 allocs/op
BenchmarkSettleTurnaround-2                           30   29082367 ns/op   7100736 B/op    27760 allocs/op
BenchmarkSettleTurnaround-2                           30   27052752 ns/op   6919417 B/op    27718 allocs/op
BenchmarkSettleTurnaround-2                           30   25533139 ns/op   6926441 B/op    27683 allocs/op
BenchmarkFoldRound/fold=off-2                         30    4890730 ns/op    0 folds/op    0 products    818577 B/op    3881 allocs/op
BenchmarkFoldRound/fold=off-2                         30    3936408 ns/op    0 folds/op    0 products    788333 B/op    3870 allocs/op
BenchmarkFoldRound/fold=off-2                         30    4488603 ns/op    0 folds/op    0 products    770438 B/op    3869 allocs/op
BenchmarkFoldRound/fold=on-2                          30    3217827 ns/op    1 folds/op    1 products    603290 B/op    2729 allocs/op
BenchmarkFoldRound/fold=on-2                          30    3400368 ns/op    1 folds/op    1 products    598194 B/op    2733 allocs/op
BenchmarkFoldRound/fold=on-2                          30    3062869 ns/op    1 folds/op    1 products    604485 B/op    2728 allocs/op
BenchmarkConcurrentSessions/threads-3-2              30    2452563 ns/op    651931 B/op     3243 allocs/op
BenchmarkConcurrentSessions/threads-3-2              30    2526754 ns/op    633640 B/op     3238 allocs/op
BenchmarkConcurrentSessions/threads-3-2              30    2804079 ns/op    647258 B/op     3234 allocs/op
BenchmarkConcurrentSessions/threads-10-2             30    3754463 ns/op   1042060 B/op     4594 allocs/op
BenchmarkConcurrentSessions/threads-10-2             30    3856902 ns/op   1058301 B/op     4595 allocs/op
BenchmarkConcurrentSessions/threads-10-2             30    3970735 ns/op   1015798 B/op     4591 allocs/op
PASS
ok      wechat-bot    6.284s
```
