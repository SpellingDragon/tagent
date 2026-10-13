# 09-acceptance — 完整验收与回退（design）

> 本设计只写「怎么做」；不改一级 design、主 spec、prototype/wiki 与既有源码。产品/脚本/CI 实现为**未来授权**（检查资产由主代理 tooling 单创建）。

## Context

- 残件现场：`golden/` 与上一轮 architecture/runtime-evidence 检查资产**已不存在**；`.github/workflows/ci.yml` 仍指旧包集合与旧 race 路径；`scripts/` 下有旧 `race_check.sh`/`lint.sh`/`check-openspec.sh`/`verify_*.py` 等。
- 既有 race 门（`scripts/race_check.sh`）以退出码保真、上游 waiver 仅限登记签名——本域继承该纪律并覆盖新第一方全包。
- 产出物消费者：产品验收（09.1/09.3/09.4/09.5）与**检查器自验收**（检查器自身负控）必须分开，防"检查器绿=产品绿"的混淆。

## Goals / Non-Goals

**Goals**

- 建立三类严格区分的检查（规范/格式、结构覆盖、运行行为），并以固定名称资产承载。
- golden 断言真实输出与一对一关系（值/消息/身份关系），非键名。
- 离线迁移读旧写新、源字节不变；回退不伪 restore 副作用。
- 完整门禁覆盖两 module + 第一方全包 race + 检查器负控；不改规则换绿。

**Non-Goals**

- 不实现产品行为（属 00–08 域）；不实现检查资产本体（主代理 tooling 单）；不归档、不提交、不部署。

## Decisions

**D09-1 三类检查严格区分，互不顶替。**
- **规范/格式有效**：OpenSpec/YAML/JSON/引用/任务编号/依赖规则可解析——**不表示产品成立**。
- **结构覆盖有效**：全部第一方包及子包都有归属；允许集按职责**人工登记**，不从当前残件 imports 自动学习。
- **运行行为有效**：正式入口产生观测，与 golden 预期值和真实测试记录一致；缺 case/错值/未知断言/子测试 Skip/包级 Fail 都失败。

**D09-2 检查资产名称固定，由主代理 tooling 单创建，本域只引用。**
- 脚本：`scripts/check_runtime_plan.py`、`check_runtime_architecture.py`、`check_runtime_structure.py`、`check_runtime_evidence.py`；自测 `scripts/test_check_runtime_*.py`；golden `golden/runtime/{architecture.json,scenarios.json}`；CI `.github/workflows/runtime-contracts.yml`。
- manifest：`openspec/changes/runtime-rebuild/task-graph.json`（**65 task 已定**；json 只含 `planned`，带 `dep`/`schema`）。计划**结构**校验走 `check_runtime_plan.py --change-dir <dir> --json`（**无** `--check-contract` flag——不得自造该参数）；**合同**校验为 `check_runtime_evidence.py --check-contract`（dep/schema 一致），**严格行为**校验为 `check_runtime_evidence.py --evidence <RUN-file.json> --test-log <PATH>`（`--evidence`/`--test-log` **成对**提供，只给一半即参数错误，不静默跳过）。
- 场景 runner 的**具体名称未定（待实现）**：未来由主代理 tooling 单实现时命名并落盘；未命名前相关子项标**待实现**，MUST NOT 以占位/未命名 runner 冒充已实现并报绿。
- 本域 MUST NOT 重命名/另立同名检查资产；资产不存在时相关任务 `design-blocked`（不得以自造资产顶替）。

**D09-3 golden 断言真实输出与关系，不写成期望值复制器。**
- 场景 runner SHALL 记录真实输出（模型消息、身份关系、状态、错误、权限）；随机 ID/时间可规范化，但保留一对一关系、顺序与因果。MUST NOT 把观测器写成"回放期望值"。
- 断言落到**值**（passed 数/run-id/指标值），只比键名不算通过。

**D09-4 结构覆盖 strict 全 coverage，MUST NOT 用通配掩盖。**
- 结构检查 SHALL 枚举全部第一方包及子包并逐一归属；组合根 MUST NOT 用 `tagent: *` 之类通配掩盖未覆盖子包；允许集人工登记（按职责），不从残件 imports 学习。
- 本条的 **delta 归 `00-foundation` 唯一承载**（`changes/00-foundation/specs/architecture-guardrails/spec.md`）；09 域只**引用**与验收，**不另持重复 delta**，也不声称旧 architecture 条款「语义不变继承」（旧稿中「现状核验全绿」「资源/世代治理实现须位于 agent 子包」等与新 state 责任矛盾的表述不继承）。

**D09-5 历史 wiki 固定 SHA 校验；新文档映射新代码。**
- `docs/wiki/**` 为**历史设计记录**（其 README 已声明逐断言校对的是**已移除**实现）：校验以**固定 SHA 快照**为基准，MUST NOT 要求历史 wiki 与当前新代码逐字一致；引用上游文件须明示属上游。
- **新**架构/场景正式文档（未来由主代理决定创建）SHALL 映射**新代码**事实；文档中的文件/符号/字段/默认值须与新代码一致（无行号/行数）。

**D09-6 迁移读旧写新、源字节不变；回退不伪 restore 副作用。**
- 离线迁移 SHALL 只读旧数据、导入新路径；源摘要（字节）MUST 不变；MUST NOT 让旧程序读新格式（新格式不回写旧路径）；缺失外置文件/不可恢复任务 SHALL 具名。
- 回退 SHALL 只读旧数据、走新路径；**MUST NOT 伪造 restore 副作用**（不假装写回了本不存在的记录、不自动改写旧档）。

**D09-7 门控三态与资源子场景矩阵（09.5）。**
- 门控：**无门 = 合法 Skip**；**有门缺凭据 = Fatal**；**必验项 Skip 不 complete**。逐项记录启用门、资源是否存在、是否授权、预算、等待任务；只报告凭据**存在性**，不打印秘密值。
- 09.5 按资源**具体子场景**列（见下表），互不等待无关服务；用户明确：**本次只加 SFT 数据消费（真实 tokenizer/chat template/批次/label/动作 mask），不加 on-policy 训练硬件需求**；不承诺 PPO/GRPO。
- **不把合成 SFT consumer/mock 当真实**：真实消费必须读真实制品并做真实 tokenize/collate。

| 资源子场景 | 启用门 | 资源 | 授权 | 验收锚点 |
|---|---|---|---|---|
| 存储档 memory / localfile / file(rustviking) | 配置档位 | 各档后端 | 本地无需 | 逐档实际屏障（不假设批量原子） |
| 真实模型 | 端点/密钥存在 | 模型制品 | 付费授权 | 真实请求/响应序列 |
| MCP | 连接配置 | 本地/远端服务 | 授权 | 发现/刷新/关闭生命周期 |
| A2A | 目标地址 | 对端服务 | 授权 | 显式目标/调用归属/重试 |
| embedding/检索 | 索引配置 | embedder | 授权 | 真实召回质量与开销（保留零增益/失败） |
| collector/OTel | 导出端 | **真实 collector** | 本地/授权 | 真实 collector **收到非空 span/metric**（点名用例 `TestRealCollectorDelivery`，实现属 `05.3`）；关闭 OTel 仍 Query 可用属确定性 `05.3` 用例（`TestObservationDoesNotOwnState`）；**不以 fixture 封账冒充 collector 收到** |
| SFT 消费 | 制品路径 | 真实 tokenizer/chat template + 批次 | 授权 | 真实 tokenize/collate、labels/mask/非空样本、拒绝账 |
| 微信 | 发送授权 | 真实接收者 | **明确发送授权** | 文本/工具/文件/跨 session 往返，逐次核对送达 |

**D09-8 race 门覆盖第一方全包，不收窄不放水；不改规则换绿。**
- race job MUST 覆盖主模块全部第一方包（含根包）与微信 module 适用并发测试；本地等价命令 MUST 同参。
- **产品 race 入口（明确命令，逐条保存原始退出码）**：
  - 根 module 全第一方子包递归：`go test -race ./... -short -count=1 -json`
  - 微信 module 全第一方子包递归：`go -C examples/wechat-bot test -race ./... -short -count=1 -json`
  - 第一方新包（`config`/`state`/`agent`/`task`/`memory`/`workspace`/`plugin`/`model`/`telemetry`/`rl`/`evolution`/`tool/*`/`query` 及其子包）**经 `./...` 递归覆盖、MUST NOT 遗漏**；**MUST NOT 只以文字声称已覆盖**。
  - 真实外部资源启用门 **MUST NOT 被隐式打开**：race 门用 `-short` 且不设置资源启用变量，资源门用例在无门时默认 Skip（Skip 不结项）。
- `bash scripts/test_race_check.sh` 仅为 **race 检查器的自测（负控/正控）**，**不等价于产品 race**；二者都要保留、分别记录退出码。
- 根 `./...` 若连带覆盖 `prototype/`（历史骨架、同属根 module）属**额外只读编译**，**本轮不执行**（本波无产品构建授权），作为**未来全量 command 允许**。
- MUST NOT 收窄包集合或放水分类器换绿；`no DATA RACE` 文本本身不是成功；上游 waiver 仅限登记签名/版本/测试，且无普通失败时才可豁免。
- 命令记录**原始退出码**；不用 `grep/head/tail` 退出码代替原命令；棘轮只降不升、**不提高空注释 baseline**。

**D09-9 验收集分层：确定性 09.1 vs 完整 09.5，防逼造假。**
- 合同 `golden/runtime/scenarios.json` **始终含全 10 场景**，唯一 scope 登记（各场景归一标签，S01–S09=`deterministic`、S10=`resource`）；**不允许任意 `--scenario` 挑选绕过**。
- 09.1 确定性 runner 用 `--scope deterministic` 只验合同登记的确定性集（S01–S09）并输出 `scope` 与 `remaining_required`（S10 列为**显式欠账**），**不对缺 S10 报全通过**；**确定性集不要求真实 SFT 批次，也不得用合成/mock SFT 凑 S10**——未授权不得合成。
- 完整 golden（含 S10 真实 `06.6` 及其他真实组件）由 09.5 在启用门+授权+资源齐备时以默认 `--scope complete` 验收；缺 S10 即失败。

## 文件白名单（未来实施候选，本轮不写）

- 未来（主代理 tooling 单）：`scripts/check_runtime_*.py`、`scripts/test_check_runtime_*.py`、`golden/runtime/*.json`、`.github/workflows/runtime-contracts.yml`、`tests/integration/acceptance/**`、`openspec/changes/runtime-rebuild/task-graph.json`。
- 本域（本轮）：仅 `openspec/changes/runtime-rebuild/changes/09-acceptance/**md`。

## Risks / Trade-offs

- [检查器自验收混淆] 检查器负控与产品验收混谈 → 二者分列；检查器负控必须有意失败（红）才算有效。
- [golden 腐化] 键名对齐掩盖值漂移 → 值/关系断言 + 规范化只限随机 ID/时间。
- [资源不可得] 真实模型/SFT/微信不可得 → 标 `BLOCKED`/Skip 如实呈现；正式必验项 Skip 不 complete。
- [历史 wiki 误红] 用现行代码校验历史 wiki → 固定 SHA 基准，新文档才映射新代码。

## Open Questions（待裁，不先设假默认）

- 09.4 部署环境（macOS/Linux 实跑）：正式新路径验收用例为 **`go test -race ./tests/integration/host -run '^TestSoakContinuity$' -count=1 -json`**（新计划用例名，与旧 CI 的 `TestSoak_Continuity` 不同；如需 soak 构建标签隔离则统一加 `-tags soak`，`task-graph.json`/`design.md`/`tasks.md` 三处一致）。**旧 CI soak job**（`.github/workflows/ci.yml` `soak`：`-tags soak -run TestSoak_Continuity … -timeout 45m -args -rounds=30 -events-per-round=30`，ubuntu-latest / go 1.24）的 **30 轮 × 30 事件与 45m 仅作待核参数参考**，**不作新验收锚**。所需窗口/资源未核验前 09.4 `design-blocked`。**本波不设固定 72h 窗口、不以硬件断电为验收前提、不声称掉电实证**；macOS/Linux 的**实际进程崩溃恢复、热更回滚、权限与资源泄漏仍必验**；**缺资源保持未完成，不强加硬件**。
- 09.5 各真实资源的授权与预算——逐项具名后单独解除，不先设假默认。
