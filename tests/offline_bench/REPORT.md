# 离线性能基准报告（resident-remaining-hardening 2.5 / 2.6）

> 本机基线（macOS 笔记本，APFS，Go darwin/arm64），运行于 2026-09-18，**含 2.6 的 curateCards O(n²) 定因修复后复测**。
> 复现：`RUN_OFFLINE_BENCH=1 BENCH_REPORT=<绝对路径>/report.json go test ./tests/offline_bench/ -run TestOfflineBenchmark -timeout 60m -v`
> 原始数据：[`report-2026-09-18.json`](report-2026-09-18.json)；profile 复现：`RUN_OFFLINE_BENCH=1 go test ./tests/offline_bench/ -run TestOfflineBenchmark -timeout 60m -cpuprofile <绝对路径>/bench.cpu.prof`，`go tool pprof -top -cum bench.cpu.prof`（产物不入库，可再生）。
> 后续改动按 D7 对照：相同机器/数据集/配置，固定场景 **p95 与 allocs 退化 >20% 须解释并重新批准**；耐久（fsync=true）开销单列，不与 fsync=false 混用。

## 支持规模（FileSegmentStore + LocalFileKV 实测记录，不虚构通用吞吐目标）

| 维度 | 1k 事件 | 10k 事件 | 100k 事件 |
|------|---------|----------|-----------|
| 写 p50 / p95（fsync=on）ms | 0.005 / 0.030 | 0.014 / 0.050 | 0.013 / 0.052（20k 采样外推） |
| 写吞吐（fsync=on）ev/s | 3520 | 3430 | 3496 |
| 写 p50 / p95（fsync=off）ms | 0.002 / 0.015 | 0.003 / 0.012 | 0.003 / 0.012 |
| 写吞吐（fsync=off）ev/s | 22043 | 24778 | 12038 |
| 探测 p50（conc=1：Get+1min 时间窗查询均值）ms | 5.5 | 54.3 | 106.4（fsync on）/ 533.1（off 全量 100k） |
| 探测 p95（conc=100）ms | 131–138 | ~1000–1060 | 1959（on）/ 7173（off） |
| 单轮全量压缩 ms / allocs B·ref⁻¹ | 2 / 2.9k | 21 / 3.1k | 213 / 3.2k |
| RSS HeapInuse MiB | 9–12 | 40–59 | 113–502 |

**耐久开销单列**：fsync=on 写吞吐 ~3.5k/s，为 off（12k–25k/s）的 ~1/4–1/7；per-op p50 0.005–0.014 ms（fsync 成本经批量摊付，尾部 p95 ≤0.052 ms）。100k off 档 alloc 64KB/w、RSS 502MiB 偏高（无 fsync 时 WAL 驻留增长），fsync=on 档 18KB/w、113MiB。

## 悬崖定因（D7：超合理量级先 profile 定位，不调阈值掩盖）

1. **curateCards 下沉循环 O(n²)——已修复（本轮 2.6 内，同语义最小修）**。基线首跑 profile：`buildRetainedRefs → curateCards` 23.17s CPU（sink 循环每弹出一行重算一次 `strings.Join`）。修复＝长度增量维护（`joint -= len(line)+1`），下沉选择与计数逐字节不变（现有 `TestCurateCards_SinkWithoutModel` 断言守住语义）。前后对照（同机同数据）：100k refs 单轮压缩 **62.8s → 0.213s（~295×）**，allocs **5.34MB/ref → 3.2KB/ref（~1658×）**，1k/10k 亦 11→2 / 646→21 ms。
2. **时间窗查询线性扫段 + 全段 `json.Unmarshal`——未修，另立 change**。修复后 profile 热点依旧是 `QueryEvents → scanPartition → json.Unmarshal`（62% CPU）。每探测固定 1 次 KVScan：延迟随事件量近线性（5ms@1k → ~53ms@10k → 106–533ms@100k），conc=100 时 100k 档 p95 达 2–7.2s。候选方向（段级时间戳剪枝、段解码惰性化）属结构优化，遵守 6.6 约束不自动引入。
3. **fork/s**：本基准为 memory/compress 负载，无 fork 行为；tmux 探测 fork 速率属 action 域既有观测面。

## token 估算误差（fixture：tiktoken cl100k_base 0.14.0，版本入档）

`DefaultTokenCounter`（2.0 chars/token + 10/message）对参考 tokenizer：

| 语料 | \|误差\| p50 | \|误差\| p90 | est/ref p50 |
|------|-----------|-----------|-------------|
| 中文 | **31.1%** | 32.3% | **0.70（低估）** |
| 英文 | 140.7% | 142.9% | 2.41（高估） |
| 代码 | 68.3% | 68.3% | 1.68（高估） |
| JSON | 50.8% | 50.8% | 1.51（高估） |

**方向性风险**：中文被低估 ~30%（cl100k 参照；deepseek tokenizer 中文效率更高，真实幅度以远端 24h 基线测量为准 → 任务 1.6）。低估方向＝触发过晚、有溢出风险；英文/代码/JSON 为保守高估（提前压缩，安全）。分型系数候选属另立优化。

## 准出结论（2.6）

- 本报告即基线：此前无同机同数据集对照，D7 退化门（p95/allocs >20% 须解释）自本报告生效；复现命令与 fixture 版本均已固定。
- 已实施：curateCards O(n²) profile 定因 + 同语义线性化修复（收益实测 295×/1658×，非主动结构优化）。
- 登记不动：查询段扫描、压缩 alloc 常数、中文 chars/token 分型——全部**另立 change**，本期不实施（6.6 约束：不自动引入 ANN/BM25/数据库替换）。
