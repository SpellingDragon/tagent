# 公开基准（bench）

本目录是 tagent 的公开基准套件：三类读数（吞吐 / 恢复时延 / 常驻内存），全部通过模块根包 `tagent` 的公开入口驱动，模型是脚本化 mock（不访问任何端点，不需要凭据），不修改产品代码。

口径先说清：

- 这里给的是**本机实测基线**，用于横向比版本、比改动，不是 SLO，也不能跨机器直接比。
- 吞吐口径分两种：`AssembleInvoke`（装配→发布→一次 turn 的完整链路）与 `InvokeTurn`（常驻 run 上的单 turn）。单 turn 基准每次用独立 session，避免会话转录累积混入吞吐读数；共享会话的历史成本单列为趋势项。
- 恢复口径 = 重启时重新装配同一个持久档根目录（打开 tier + 加载事实日志 + 发布 + 逐链 fold-back），不是只读 KV 的裸耗时。
- 内存口径 = 装配后常驻 run 在若干 turn 之后的堆占用与每 turn 分配量，包含 run 保留的账本。

## 环境（2026-10-19 实测）

| 项 | 值 |
|---|---|
| 机器 | Apple M3 Pro，12 逻辑核，36 GiB |
| 系统 | macOS 14.7.2（23H311），darwin/arm64 |
| Go | go1.27.1（go.mod 声明 `go 1.24.1`） |
| 模型 | 进程内 mock（两次决策：一次 tool call + 一次 final） |
| 持久档 | `memory.StorageLocalFile`，`b.TempDir()` |

## 跑法

```bash
# 冒烟（每个基准跑一次，用于确认套件可跑；数值是冷启动样本）
go test -bench=. -benchtime=1x ./bench/

# 吞吐基线（5 次取样，取中位数）
go test -run '^$' -bench '^(BenchmarkAssemble|BenchmarkAssembleInvoke|BenchmarkInvokeTurn|BenchmarkInvokeParallel)$' \
  -benchtime=2s -count=5 ./bench/

# 常驻内存基线
go test -run '^$' -bench '^BenchmarkResidentMemory$' -benchtime=2s -count=5 ./bench/

# 恢复时延基线（每个子档 20 次重启装配）
go test -run '^$' -bench '^BenchmarkRestoreLatency$' -benchtime=20x -count=5 ./bench/

# 共享会话历史成本趋势（不是吞吐基线：数值随迭代数增长）
go test -run '^$' -bench '^BenchmarkInvokeSharedSession$' -benchtime=1000x -count=3 ./bench/
go test -run '^$' -bench '^BenchmarkInvokeSharedSession$' -benchtime=5000x -count=3 ./bench/
```

`go test -bench` 不会运行本包的 Test 函数（本包没有），`-run '^$'` 只是把这一事实写明。

## 当前基线（5 次取样的中位数）

### 吞吐

| 基准 | ns/op | 折算吞吐 | B/op | allocs/op |
|---|---:|---:|---:|---:|
| `BenchmarkAssemble`（装配 + 首次发布） | 5,054 | ≈ 197,900 次/s | 12,029 | 130 |
| `BenchmarkAssembleInvoke`（装配→发布→一次 turn） | 30,063 | ≈ 33,300 次/s | 41,921 | 332 |
| `BenchmarkInvokeTurn`（常驻 run 单 turn，独立 session） | 28,848 | ≈ 34,700 次/s | 26,600 | 186 |
| `BenchmarkInvokeParallel`（12 并发调用方，单 run） | 11,730 | ≈ 85,300 次/s（聚合） | 26,651 | 186 |

一次 turn 的固定内容：一个 user 事件、两次模型决策、一次经任务闸准入并结算的 tool 调用。

### 恢复时延（本地持久档）

| 已提交任务链 | ns/op | 折算 |
|---|---:|---:|
| 0（空档，装配基线） | 274,156 | 0.274 ms |
| 1 | 437,760 | 0.438 ms |
| 64 | 1,346,042 | 1.346 ms |
| 256 | 4,619,667 | 4.620 ms |

按差值推算，每多一条已提交任务链约 **15 µs**（(4.620−0.438) ms / 255 ≈ 16.4 µs；(1.346−0.438) ms / 63 ≈ 14.4 µs）。`facts=0` 的 5 个样本里第一个偏冷（562 µs），之后稳定在 168–274 µs，表中取中位数。

### 常驻内存

| 指标 | 值 |
|---|---:|
| 装配后 run 的堆占用（约 9 万 turn 结束时） | 0.738 MiB |
| 相对装配时的堆增长（保留账本） | 0.038 MiB |
| 每 turn 分配量 | 0.0255 MiB（≈ 26.1 KiB） |
| allocs/op | 186 |

### 共享会话历史成本（趋势项，不是基线）

8 个 session 轮转、单 run：

| 迭代数（≈ 每 session turn 数） | ns/op | B/op |
|---|---:|---:|
| 1,000（≈125） | 492,377 | 881 KB |
| 5,000（≈625） | 2,479,927 | 4.7 MB |

读数随共享会话的转录长度近似线性增长（每 1000 次迭代约 +0.5 ms/turn）：运行期每次决策都会携带该 session 已累积的转录。这一项只用于观察历史成本，不参与吞吐基线；`BenchmarkInvokeTurn` / `BenchmarkInvokeParallel` 每次用独立 session，正是为了把这段成本隔离出去。

## 复跑与对照

- 单基准复跑：`go test -run '^$' -bench '^BenchmarkInvokeTurn$' -benchtime=2s -count=5 ./bench/`
- 编译与静态检查：`go vet ./bench/`
- 与产品隔离的证明：`git status --short -- ':!bench'`（本批次不改产品代码）；`go test ./tests/integration/... -count=1` 保持绿。
- 基准只 import 公开根包与 `config`/`event`/`memory`/`model`/`state`，不触碰内部字段，也不读环境变量。

## 已知集成待办（本批次白名单外，提交前必须补）

新增顶层 `bench/` 包会触发三条仓库门禁，它们的修改面不在 `bench/**` 白名单内：

1. `scripts/check_runtime_architecture.py --implementation` 报 `unknown first-party package 'bench'`：需在架构策略里登记 `bench` 的显式边（禁止通配）。
2. `scripts/codetools` 的 `tracked-hygiene` 只允许登记过的顶层目录（`scripts/codetools/hygiene.go` 的 `registeredTopDirs`）：需登记 `bench`，并同步根 `README.md` 的目录说明。
3. `scripts/gen_godoc.sh --check` 会因新包生成 `docs/api/github_com__SpellingDragon__tagent__bench.md` 而报 stale：需重新生成 `docs/api` 并纳入本仓文档预算。

在上述三项补齐前，`bash scripts/lint.sh` 会在最后两步失败；`go test`/`go vet` 与 `tests/integration` 不受影响。
