# runtime-simplicity-upgrade 编排文档

## 一、结论（先行）

- **抵达 DoD 的唯一路径**：0.1 红锚 → 追加日志（1.1/1.2/1.4）→ 请求证据（2.1—2.4）→ 流式（3.x）→ 观测（4.x）→ 离线数据（5.x）→ 收敛（6.x）→ F。任一步跳过都会让后面的验收失去意义：没有日志就没有可证明的持久边界，没有落盘证据就没有可导出样本，没有证据就没有流的最终对照。
- **当前最大风险**：1.2 把 `kvPort` 从整分片快照换成追加日志，是唯一一处触达全部写路径的改动。若它错了，后面所有"重启后可读"的断言都会以看似通过的方式失败（读到的是旧快照或内存态）。因此 1.x 必须单独一轮做完并单独复验，不与 2.x 同批。
- **现状**：规划工件已落盘，尚未实施；0.1 红锚尚未运行。

## 二、编排四问

### 2.1 次序

派发原子是单条叶任务。域只作授权与验收单位。

| 叶 | 前置 | 状态 | 触碰集 | 派发单 |
|---|---|---|---|---|
| 0.1 | — | ready | `tests/integration/upgrade/` | D0 |
| 0.2 | 0.1 | blocked | `openspec/changes/runtime-simplicity-upgrade/design.md` | D0 |
| 1.1 | 0.1 | blocked | `memory/ledger/journal.go` + 测试 | D1 |
| 1.2 | 1.1 | blocked | `memory/ledger/journal_kv.go`、`localfile.go` | D1 |
| 1.3 | 1.1 | blocked | `memory/ledger/config.go` | D2 |
| 1.4 | 1.2 | blocked | `runtime/state/durable.go` | D2 |
| 1.5 | 1.2、1.3 | blocked | `memory/ledger/*`、`memory/ledger/segment_store.go` | D3 |
| 1.6 | 1.5 | blocked | 无写入 | D3 |
| 2.1 | — | ready（与 1.x 触碰集不相交） | `assembly/host_model.go` | D4 |
| 2.2 | 1.4、2.1 | blocked | `runtime/agent/loop.go` | D5 |
| 2.3 | 2.2 | blocked | `features/rl/line.go`、`sink.go` | D5 |
| 2.4 | 2.3 | blocked | `assembly/request.go` | D5 |
| 2.5 | 2.4 | blocked | `features/rl/capture.go`、`docs/api/` | D5 |
| 3.1 | 2.5 | blocked | `runtime/agent/loop.go` | D6 |
| 3.2 | 3.1 | blocked | `runtime/agent/stream.go` | D6 |
| 3.3 | 3.2 | blocked | `assembly/host.go`、`assembly/host_document.go` | D6 |
| 3.4 | 3.2 | blocked | `examples/cli-host/main.go` | D7 |
| 4.1 | 3.3 | blocked | `features/telemetry/*` | D7 |
| 4.2 | 4.1 | blocked | `features/telemetry/metrics.go` | D7 |
| 4.3 | 4.2 | blocked | `assembly/host_diagnostics.go` | D8 |
| 5.1 | 2.4 | blocked | `features/rl/export_chain.go` | D8 |
| 5.2 | 5.1 | blocked | `features/rl/export_chain.go` | D8 |
| 5.3 | 5.2 | blocked | `features/rl/export_chain.go` | D9 |
| 5.4 | 5.3 | blocked | `training/tests/`、`training/pyproject.toml` | D9 |
| 6.1 | 5.4 | blocked | `features/rl/*`、`assembly/run.go` | D9 |
| 6.2 | 6.1 | blocked | `scripts/`、`.github/`、`*skeleton_assert.go` | D10 |
| 6.3 | 6.2 | blocked | `docs/` | D10 |
| F1—F4 | 全部 | blocked | 只读 + 归档命令 | D11（编排者亲验） |

并发准入按触碰集判定：

- 可并行：0.1 ∥ 2.1（不同包，无构建闭包交集）。
- **不可并行**：1.x 与 1.5（同包）；2.2 与 3.1（同文件 `loop.go`）；5.1/5.2/5.3（同文件 `export_chain.go` 必须合单串行）。
- `memory/ledger` 与 `runtime/state` 同属一个构建闭包，1.2 与 1.4 串行。

关键路径：0.1 → 1.1 → 1.2 → 1.4 → 2.2 → 2.3 → 2.4 → 3.1 → 3.2 → 3.3 → 4.1 → 4.2 → 4.3 → 5.x → 6.x → F。

本轮物理上做不了：真实 provider 联网验证、微信真发、OTLP 远端 collector、模型权重训练。触发条件：获得明确授权与预算。

### 2.2 授权

- 0.x、6.2、F：编排者本人。
- 1.x：单一实施者独占 `memory/ledger` 与 `runtime/state`。
- 2.x：`assembly` 属共享面，单写者；`features/rl` 与 `assembly` 不并行。
- 3.x：`runtime/agent/loop.go` 是热点文件，2.2 与 3.1 必须合单或串行。
- 4.x、5.x：可分派给不同实施者，但 `features/rl/export_chain.go` 只能一人写。
- 共享面冻结常数（跨单必须先定）：记录 kind 的字符串常量、日志帧头布局、`EffectiveGenerationConfig` 的签名、增量回调的函数类型。这些在 1.1/2.1 内先落定，后续单以之为准，不等彼此产物。

偏离分级：

- 预授权自决：测试文件内新增负向子测试、注释修正、同包内私有 helper。
- 必须上报待裁：改动 `api/model` 的既有导出类型、改动 `ReduceChannel` 签名、修改日志帧格式、删除任何有生产调用方的函数。

过程件只落 `/tmp/tagent-<单号>/`，不入仓；波后由编排者清理并核对 `git status` 只含预期改动。

### 2.3 凭据

- 每单完成须附：命令原文、原始退出码、实际执行用例数（`-json` 输出中 `Action==pass` 计数 > 0）、改动文件清单。
- 必须亲验的数字：0.1 的红项与原因行；1.2 的"不再重写整分片"证据（对比写入字节数）；1.6/2.x 的 `-count=10` 通过数；5.4 的 pytest 用例数与掩码断言；F2 的全部退出码。
- 训练类不以 stdout 文本为凭：以 pytest 退出码 + 用例数 + 掩码断言为准。
- 反空转：`-run` 与测试名逐字符一致；零匹配视为未验证。

### 2.4 释放

| 资源 | 占用者 | 释放条件 | 等待表 |
|---|---|---|---|
| `memory/ledger` 写权 | D1 | 1.2/1.3 验收通过 | 1.5 立即可派 |
| `runtime/state` 写权 | D2 | 1.4 验收通过 | 2.2 立即可派 |
| `runtime/agent/loop.go` 写权 | D5 | 2.2、3.1 结束 | 3.2 立即可派 |
| `features/rl/export_chain.go` | D8/D9 | 5.1—5.3 全部结束 | 5.4 立即可派 |
| 端口（测试） | 全部 | 用例结束 | 动态分配，禁固定端口 |

释放即触发：每单完成后对照上表补派，不等下一轮指令。

## 三、明细层

- 单均叶数：1—3 条。小于 2 条的拆分（如 1.3 单列）理由：它与 1.2 触碰不同文件，可并行释放前置，且其负向语义（旧目录不动）独立可验。
- 合单：5.1/5.2/5.3 合为一单（同文件）；2.2 与 3.1 若同一人则在一次会话内顺序完成。
- 预算上限：单轮最多三路可写实施；长跑（1.6、F2）用后台进程不占槽。
- 档位与真实路径欠账：外部 provider/微信/collector 标为未验证，不阻断本地验收，也不得在报告中写成已验证。

## 四、波次台账

| 波次/派发单 | 五查结论 | 集成验证 | 跨域裁决 | 账目勾选凭据 | 下轮情报 | 锁释放与队列消费 |
|---|---|---|---|---|---|---|
| 规划完成 | 无（尚未实施） | 未运行 | 无 | 无 | 0.1 可立即派发 | — |

无凭据摘要不得写"完成"。每轮追加一行。
