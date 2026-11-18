# 03 压缩可配置与压缩消费恢复

> 本域原3.1–3.3的历史实现保留，当前完成待03.R3复验；下文旧问题描述为立案时点，不代表现在仍硬编码。重整新增03.R1核销同一请求两个产物的CI失败，03.R2按有效状态收敛压缩检查点，03.R3覆盖原预算/租约/降级及消费者。新实现依G2/12.R2，不按旧W3整域派发。

## 本轮边界与依赖

- 依赖：09.R1真实CI失败记录；03.R1先定位再修；03.R2依G2与12.R2状态推进语义。
- 被依赖：09.R3完整长测、12消费者恢复一致和最终验收。
- 接口面：现有CompressionConsumer/检查点消费状态；新水位与未完集合结构尚待设计门，不预先冻结一张新历史表。
- 禁止：不以模型重新生成同样文字证明确定性、不删未完请求或使旧请求重产、不改变已声明摘要预算、不实现独立no-prefix迁移。


## Why

一级 [design.md](../../design.md) D1 **P8 压缩可配置**与 **D6.17** 未兑现，两处实证（✅ 读码，行号为当前 dev 工作树）：

- **摘要界硬编码 2048**：`DefaultAgeSummaryTextLimit = 2048`（memory/replay/age_segments.go:100）是机制常量，`ageSummaryTextOf`（:641）在 :643/:646/:673 三处直接读它；`bandSummaryInstruction`（assembly/summary_gist.go:40-43）把「within 2048 bytes」写进提示词；`boundedSummaryGist`（:114-124）在 :116/:119 同样读该常量。常量登记为**机制常量**（golden/runtime/window_bounds.json:108-114「机制常量：一份 history summary 正文的字节上界」）。
- **摘要请求绕过租约与声明采样**：`bandSummaries.SummaryGist`（summary_gist.go:57-84）在 :66 从 `run.natives[agent]` 取裸 native model，:71 直接 `GenerateContent`——既不读 `r.leaseOf(agent)`（assembly/run.go:3315）的租约，也不经 `withHostModelParams`（assembly/host_model.go:53）。同一 agent 的请求面在 `suppliers`（assembly/tools.go:218-227）里是「有租约走 `rl.NewLeaseModel` 且一律 `withHostModelParams`」，两处取法不一致：摘要请求既不随模型热替换换版，也不带声明的 temperature / thinking / effort / max_tokens。

用户原话口径（一级 design.md:83 转录）：「不划特殊界限、可配置」。摘要预算是策略量而非机制不变量——删掉硬界无人坏（同一节 design.md:25 反事实检验已裁）。

**本域保留的现状**：摘要正文是模型产出（LLM-first + 机械兜底：`summarizeProduct`（assembly/compress_consumer.go:275-291）→ `SummaryGist` → 模型 `GenerateContent`），本域不改这条取向，只把「经哪条模型面」和「界是多少」归正。

## What

承 D6.17，三件：

1. **摘要预算进声明面**：`AgentSpec` 加 `summary_budget_bytes`（api/config/config.go AgentSpec 段、decode.go:158-165 `agentKeys`、defaults.go 加 `DefaultSummaryBudgetBytes`），`FoldWindow`（assembly/age_fold.go:25-33）加 `SummaryBudgetBytes`，`AgeFoldPlan`（memory/replay/age_segments.go:413）加 `SummaryBudgetBytes`。零值读声明缺省，与 `compress_threshold` / `keep_recent_tasks` 同一形态。
2. **去 2048 硬界**：界由 plan / window 承载并贯穿机械折（`ageSummaryTextOf`）与模型裁（`boundedSummaryGist`）两侧；提示词按声明预算拼（`within %d bytes`）；常量降为**策略缺省值**（不再是承载界的机制常量），golden 从「冻结机制常量」重分类为「策略量 + 策略缺省」。
3. **摘要请求经租约 + 声明采样参数**：`bandSummaries` 取模型改与 `suppliers`（assembly/tools.go:218-227）同一条取法——有租约走 `rl.NewLeaseModel(held.lease)`，一律包 `withHostModelParams`；无租约回落 native（既有测试与无租约宿主不变）。

## Capabilities

- `compression-pipeline`（**MODIFIED**）：老带摘要正文的字节预算可配置、界无硬编码来源、摘要请求经租约与声明采样参数产出、会话无模型时仍保留机械有界型。

## 边界与依赖

- **依赖**：01 的三挂点守护判据（本域收敛后压缩消费由 02 动态挂载，摘要模型面属消费方内部实现，不受挂载方式影响）；02 的 `plugins:` 挂载（`withSummaryGist` 接线点 assembly/run.go:596 随 02 迁到动态挂载后仍须保留本域取法）；声明面严格解码（api/config/decode.go:158-180，`mapFields` 拒绝未声明键）。
- **被依赖方**：04（中段票据）与本域**共触 memory/replay/age_segments.go**（04 改中段呈现、03 改老带摘要界）→ 同文件簇合单串行，不可并行派发（一级 design.md:58、tasks.md 合单判据）；09 验收只引用 `compress.product` 的正文，不读本域新字段。
- **接口面**：`summary_budget_bytes`（agent 声明键，policy 类）；`FoldWindow.SummaryBudgetBytes` / `AgeFoldPlan.SummaryBudgetBytes`（int，零值读缺省）；`summaryGists.SummaryGist` 增加 `budget int` 形参（唯一实现 `bandSummaries` + 测试替身，见 summary_gist_test.go:34）。golden 新增 policy 条目，`DefaultAgeSummaryTextLimit` 条目 reason 改写。
- **禁止事项**：
  - **不改中段票据**（`[evt_KEY|type]`、`foldProcessDetail` 铸的机械摘要、`ticketText` 兼容）——归 04 域；`age_segments.go` 内本域只动 `historySummaryOf` / `ageSummaryTextOf` / `AgeFoldPlan` 三处及其调用方。
  - 不触冻结归档（`openspec/changes/archive/**` 零 diff，只附勘误）；不触 `config-surface-normalization` 未提交变更（同 dev 分支，互不触碰）；不触 `examples/wechat-bot/` 与外来未跟踪文件（`runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py` 等）。
  - 不动 `memory/replay` 的纯读性：不在 replay 包内读 api/config（现无该 import，分层保持）；模型访问只在 assembly 一侧。
  - 不新增第二处绝对值：界只能有一个来源（声明 → window/plan），禁在 assembly 或 replay 另写一份 2048 字面量。
  - 主 spec 的既有反向条款（`openspec/specs/task-skeleton-compression/spec.md` 的「老带段整段折叠为一条历史综述」原写「综述正文……MUST NOT 改写为 LLM 生成文本」）与代码相反，**本域不改它**，转 10 域同步；本域 delta 以代码现状（模型产出、可配置界）为契约基准。
  - 本文档阶段只规划不实施。
