# 完整请求预算：技术要点

## 既有能力裁决（5.9）

- **E12 归因更正（F33）**：`modelutil`/`agent/compress` 预算实现的删除发生在 **6625dff8（2026-10-26，event consumption integrity）**，非 170ef267；同提交新增 api/event/compress_request.go（压缩请求事件族）与治理/预算删除；170ef267（11-04）新增 memory/replay/replay_budget.go（投影侧预算）。**当前 HEAD 状态=投影预算在、最终请求统一预算面缺**。任务 1.0 审计以 6625dff8 为删除基线、检查两提交各自留存面。
- delta 归属 `request-budget-accounting`（F7）：目录已改 `specs/request-budget-accounting/`，MODIFIED 头逐字匹配；归档前提升顶层并实跑 archive（判据不含 Aborted）。
- 与 `evals-harness` 不重合（组件 mock 评估 vs 真实请求预算）。

## 归档 requirements 对照表（任务 1.0 产出，审计基线 6625dff8 删除点 / 170ef267 投影预算新增点）

| # | 归档 requirement | HEAD 审计坐标（或 0 命中） | 本域处置 |
|---|---|---|---|
| ① | 声明快照 | 存续（形态迁移）：`features/rl/capture.go:207` RequestSnapshot.VisibleTools；`features/rl/capture.go:243` SnapshotRequest；`features/rl/capture_wiring.go:444` 捕获读同一 model.Request；`runtime/agent/loop.go:1741` `state.ToolFace(handle.Binding)` 冻结本决策工具声明。旧 `modelutil.RequestSnapshot` 0 命中（`grep -rn 'modelutil' --include='*.go' .` = 0） | 不重建旧包；申报快照继续由 capture 面持有，预算只读同一 `model.Request`，不新增第二份快照 |
| ② | 完整输入精度（七面+unknown+输入输出分离） | HEAD 缺：只存投影预算 `memory/replay/replay_budget.go:147` AssembleBudget 与 `assembly/request.go:1162` projectionBudget（仅投影侧）；最终请求无统一计量（旧 `modelutil`/`agent/compress` 删除后 0 命中） | **恢复核心**：`runtime/agent.AccountRequestBudget` 九面计量 + 三窗口 + 输入/输出分离；unknown 媒体标 unknown 不当零；见代码面清单 |
| ③ | 单一压缩 | 存续：`assembly/age_fold.go:86` AgeFoldPlugin、`:138` ObserveCommit；`assembly/compress_consumer.go:46` CompressionConsumer；6625dff8 起为事件族形态（compress request/product） | 不重造第二压缩路径；预算只触发投影再折叠，压缩判断仍归单一插件/消费者 |
| ④ | 摘要时限 | 0 命中：`grep -rn 'summary_timeout' --include='*.go' .` = 0；旧 LLM summary 机制已删除，现存 `history_summary` 只是 age fold 的产品（`runtime/agent/request_assembly.go` RoleOf 映射），无外部摘要调用、无时限可挂 | **显式欠账**：不重建摘要调用/时限；若摘要调用回归，其时序预算必须并入本域计量与阶梯（主 spec 条款保留） |
| ⑤ | usage 尾包 | 存续：`api/model/terminal.go:80` SignalUsageTail、`:119`、`:250`、`:380`（仅在归约里沉积 usage，不作为内容） | 不重造；usage 尾包仍归终端归约，预算不把 usage 当请求输入 |
| 附 | 任务板（九面之一） | 生产 0 命中：`grep -rn 'task_board' --include='*.go' .` 在 HEAD 只命中本域新增的计量面/测试；旧 `agent/task` 任务板材料已随 6625dff8 删除 | **保留计量桶，测试注入**；生产面显式欠账（当前无任务板材料进入请求，若回归即走同一 meter/taskBoard 参数） |

**恢复范围定稿**：恢复统一预算核心（九面计量、开门口径、provider/output/replay 三窗口、降级阶梯、拒发、debug 事实、来源标注）；不恢复旧 `modelutil`/`agent/compress` 第二预算/第二压缩实现；④摘要时限与任务板生产面记显式欠账，主 spec 条款不删。

## 文件清单

- **runtime/agent/loop.go（F13）**：最终请求拼装处（boundary.Messages+transcript+final prompt+DynamicSystem+tools 合成 model.Request 的位置，约 :1580）插入统一预算裁决
- assembly/request.go：boundary 侧计量供给
- **api/config（D6 例外）**：新增**两个可选导出字段**（provider context window 声明、输出余量；**配置字段必须导出供 YAML 解码**；AgentSpec 导出字段 24→26）；AgentSpec.MaxTokens 语义不动；**api/config/decode.go** 解码与 **api/config/fingerprint.go** 的 canonicalAgentCore 同步（新增字段进指纹；既有 AgentSpec 指纹迁移=配置变更事实一条，记 CHANGELOG；F1 例外行相应列入）；**concept budget 基线（r9 记法 `golden/concept_budget` 基线文件；仓内实落=`scripts/gates/check_concept_budget.py` 的 `BASELINE` struct-fields AgentSpec 24→26 登记 + `golden/runtime/architecture.json` 的 `concept_bindings` 域面基线同批核验）入本域触碰集**
- assembly/request_budget_test.go：锚④与阶梯/拒发/余量/fold 回归测试

## 关键决策

- **收口与降级=两阶段**：裁决在 loop.go 最终拼装处；降级执行回 boundary（BoundaryView 携带来源标注，或 loop 裁决超限后回调 boundary 按阶梯重装投影），循环至满足或拒发——BoundaryView 扁平 Messages 不带来源是实现要解的点。**来源标注（或分段结构）是 1.3 的显式实现步骤**；阶梯测试在 boundary 重装路径上测（assembly 包），断言各来源分别让位。
- **触达集（r2 F27 更正；r9 F17 补全）**：runtime/agent/loop.go、runtime/agent/request_assembly.go、assembly/request.go、api/config（仅新增两个可选**导出**字段；decode.go/fingerprint.go 同步）、**concept budget 基线（r9 记法 `golden/concept_budget`；实落 `scripts/gates/check_concept_budget.py` 的 struct-fields AgentSpec 24→26 登记 + `golden/runtime/architecture.json` 的 `concept_bindings` 域面核验）**、测试。
- **三窗口拆分（r2 F4 定版；r9 F17 收口）**：provider context window（新可选声明；**缺省=AgentSpec.MaxTokens 本值，不加系数**——保守读法使锚④探针语义成立：声明 32 即输入预算上限基数）；输出余量=请求层 GenerationConfig.MaxTokens（已存在）或**新增可选声明字段**；**未声明输出余量时=provider 窗口的固定比例常量**（值冻结于代码常量，TestBudgetDefaultOutputReserve 锁定）；replay/fold window=AgentSpec.MaxTokens 原语义不变（回归锚锁定）。输入侧预算=provider 窗口-输出余量（声明值或缺省比例常量）；**缺省规则（F1 定版，唯一口径）**：预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）；锚④探针=显式声明 32。**两个新增字段 MUST 为导出字段（YAML 解码必需）；默认即预算生效（从只约束回放变为约束整包）是行为变化，记 CHANGELOG；不设零默认行为不变的表述**。
- **MaxTokens 双语义（r5 定版）**：`AgentSpec.MaxTokens` **仍是 replay 窗口**；请求预算仅在推导缺省 provider 窗口时**读**它，MUST NOT 改变其语义；新增 provider/output 字段进 canonicalAgentCore 指纹，既有 AgentSpec 指纹迁移=配置变更事实一条（CHANGELOG）。
- 计量同源：复用 FoldWindow 计量函数；reasoning/ContentParts/任务板/恢复提示按既有 spec 面入计量（unknown 媒体标 unknown 不当零）。
- 降级阶梯与拒发：动态→技能→投影再折叠；必需面（固定提示+工具声明）不可让位；拒发+可观测事实。**拒发归 failed 终态并按既有 failure-notice 告知（不新增通道）**；阶梯与常量属实现细节，spec 只留可观察不变量。
- 配置（唯一口径）：**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**；MaxTokens>0 的推导使预算生效=行为变化，记 CHANGELOG；零值从不推导出 provider 窗口（推导出 0 会拒绝一切请求，荒谬）。
- **输出余量来源**：输出余量=**新增 api/config 可选导出字段**（与 provider 窗口同一例外清单；进 decode.go 解码与 fingerprint.go canonicalAgentCore——例外行相应列入 F1 复核清单；导出字段是 YAML 解码前提）；**未声明时=provider 窗口的固定比例常量**（值冻结于代码常量；常量值锁定于 TestBudgetDefaultOutputReserve，缺省路径断言并入 TestBudgetReservesOutputAllowance）。

## 实现落点（任务 1.1–1.6 后回填）

- **来源标注**：`agent.BoundaryView.Sources []string` 与 `Messages` 平行（`fixed_prompt`/`skill_bodies`/`history`），动态段拆成 `dynamic []boundaryDynamicPart`（`skill_bodies`/`environment`）；`WithBudget/WithDynamicSkills/WithEnvironmentBlock` 三个方法保持导出字段数 ≤5（S2）。`RequestBoundary.ProjectionBudget` 是 loop 折投影时的覆盖预算，0=沿用窗口预算（fold 语义不变的入口）。
- **门与三窗口**：`requestBudgetPolicy{providerWindow, outputReserve}`；门开=providerWindow>0；providerWindow=显式 `ProviderContextWindow`，缺省取 `AgentSpec.MaxTokens` 本值（不加系数）；outputReserve=显式 `OutputReserve`，缺省 `OutputReserveOf(window)=ceil(window/4)`（常量 `DefaultOutputReserveDivisor=4` 冻结）；输入预算=`providerWindow-outputReserve`，负值钳 0。MaxTokens=0 且无显式窗口=门关，只记 debug 事实。
- **阶梯与拒发**：loop 最终拼装处按"环境→技能→投影再折叠"逐步重测；投影步把 `history 面 - 超出量` 作为 `RequestBoundary.ProjectionBudget` 回 boundary 重装；系统必需面仍超=返回 `ErrRequestBudgetExceeded`（走既有 failed 终态 + failure-notice）。每步 debug 记 `request budget step=...`，接受/拒发各一条。
- **计量**：`AccountRequestBudget` 九面，每消息 4 token 框架、每 tool call 5 token 框架（与 `approxPromptTokens` 的 16/20 bytes 同源），文本按 `replay.BudgetCharsPerToken=4` 上取整；unknown 媒体记类型字符串、不入总量，总量=各面和（有 unknown 时是下界）。工具声明按名称排序读 `Declaration()`（名称+描述+schema JSON）；任务板经 `taskBoard` 参数（生产传 ""，测试注入）。
- **指纹迁移**：`canonicalAgentCore` 两字段无 omitempty（始终编码），`api/config/config_test.go` 的 sectionsFingerprint 一次性迁移；新字段 decode 键 `provider_context_window`/`output_reserve`，负值在 api/config 声明解码路径由 `countField` 按字段全路径拒绝。概念基线 struct-fields AgentSpec 24→26 已登记（`scripts/gates/check_concept_budget.py`）；`golden/runtime/architecture.json` 的 concept_bindings 仅含 ConfigChange 且 commit 未动，本域不新增绑定。

## 交付边界（触碰集外事项，2026-11-05 lead 已收口）

- **CHANGELOG**：既有 AgentSpec 指纹迁移的配置变更事实由波末文档批补（lead 收口）；本文与 tasks 1.4 已记事实。
- **assembly/host_document.go**：lead 已接入两键——`hostAgent` 的 `ProviderContextWindow *int`/`OutputReserve *int`（:101-102）与 `derefInt` 映射（:191-192），缺省 0；真实 Boot YAML 方言现可声明。注意 host 方言未走 api/config 的 `countField` 非负校验：负值会被 `budgetWindow`/`budgetReserve` 当未声明处理（安全但不与 api/config 的拒绝语义一致）；如需一致，host 侧应补非负校验（触碰集外）。
- **docs/wiki request-budget 文档** 不在触碰集：契约链接指向 `docs/wiki/agent/agent-architecture.md#request-budget`，该页已有预算段落；本域只落代码与测试，不同步文档。

## 验证证据（2026-11-05）

| 任务 | 命令（均带 -v） | 结果 |
|---|---|---|
| 1.0 | 对照表（grep 计数：summary_timeout=0、modelutil=0、task_board 生产面=0） | 表已回填 |
| 1.1 | `go test ./assembly -v -run 'TestSymptomAnchorUnifiedRequestBudget' -count=1` | 1 PASS（锚④转绿；显式 32 token 窗口→拒发，failed 终态+cause 含 budget） |
| 1.2 | `go test ./assembly -v -run 'TestRequestBudgetAccountsAllFaces' -count=1` | 1 PASS |
| 1.3 | `go test ./assembly -v -run 'TestBudgetDegradeLadder\|TestBudgetRefusalWhenUndegradable' -count=1` | 2 PASS |
| 1.4 | `go test ./assembly -v -run 'TestBudgetReservesOutputAllowance\|TestBudgetDefaultOutputReserve\|TestFoldWindowSemanticsUnchanged\|TestProviderWindowGateSemantics' -count=1` | 4 PASS；`api/config -run TestRequestBudgetFieldsDecodeAndFingerprint` 1 PASS |
| 1.5 | `go test ./assembly -v -run 'TestBudgetConsumerPathEvidence' -count=1` | 1 PASS |
| 1.6 | `go test ./assembly ./runtime/agent -v -count=1` | **128 PASS，exit 0**（W1 交付面的 `TestRetryAttemptsCarryActionIdentity` 竞态已由 W1 用 `deliveryTestWaitFor` 修复） |
| 1.6 | `python3 scripts/gates/check_concept_budget.py --json` | 本域 AgentSpec 24→26 有意登记后错误清零；仅剩 `features/evolution/governance/denial.go:GovernanceEventLog`（lead 归 03 域收口）；`concept_bindings` 仍只有 ConfigChange/new_max=1；W1 改动另留 1 条 stale ctx-bare 基线 |

## 风险与回退

- 计量误差：估算器+安全系数常量锁定测试；unknown 媒体已显式标注为下界，不得当作已证明的 fit。
- 缺省推导使 MaxTokens>0 的既有 agent 从“只约束回放”变为“约束整包”（行为变化），已在门/阶梯测试与锚④锁定；回退=整体 revert 本域。
- 回退：整体 revert 本域（无实验开关；r22 纯化，W2 以无开关形态实施）。
