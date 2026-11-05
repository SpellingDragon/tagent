# 完整请求预算：孙任务

- [x] 1.0 缺口审计（F33 重定基线）：以 6625dff8 为删除基线、170ef267 为投影预算新增点，逐条核对归档 requirements ①-⑤ 在 HEAD 的现状（file:line 或 0 命中记录），对照表回填 design；恢复范围定稿（核心面必恢复，其余小并大欠）—— 验证：design 对照表存在且每行有坐标或 0 命中（`grep -c` 输出入台账）。
- [x] 1.1 锚④转绿：`TestSymptomAnchorUnifiedRequestBudget`（W0 已红）——统一预算在最终拼装处约束（降级或拒发可观测）—— 验证：`go test ./assembly -v -run 'TestSymptomAnchorUnifiedRequestBudget' -count=1` PASS。
- [x] 1.2 统一计量器（九项输入面）：`TestRequestBudgetAccountsAllFaces`（固定提示+技能正文+动态环境+投影历史+工具声明+reasoning+ContentParts+任务板+恢复提示各注入增量，断言总量=各面和；unknown 媒体标 unknown 不当零）—— 验证：`go test ./assembly -v -run 'TestRequestBudgetAccountsAllFaces' -count=1` PASS。
- [x] 1.3 降级阶梯与拒发：`TestBudgetDegradeLadder` + 负锚 `TestBudgetRefusalWhenUndegradable`（**拒发归 failed 终态并按既有 failure-notice 告知，不新增通道**）；**BoundaryView 来源标注（或分段结构）是本任务的显式实现步骤**，阶梯测试在 boundary 重装路径上断言各来源分别让位 —— 验证：`go test ./assembly -v -run 'TestBudgetDegradeLadder|TestBudgetRefusalWhenUndegradable' -count=1` 用例数=2 PASS。
- [x] 1.4 三窗口拆分与 fold 回归：`TestBudgetReservesOutputAllowance`（输出余量=新增可选字段，进 canonicalAgentCore 指纹与 decode；**并断言缺省路径=未声明时取 provider 窗口固定比例常量**）+ `TestBudgetDefaultOutputReserve`（缺省比例常量值冻结锁定）+ `TestFoldWindowSemanticsUnchanged` + `TestProviderWindowGateSemantics`（三分支：MaxTokens=0 且未显式声明→门关；MaxTokens=0 且显式声明→门开；MaxTokens>0→缺省推导门开；**AgentSpec.MaxTokens 仍是 replay 窗口，预算只在推导缺省时读它**）—— 验证：`go test ./assembly -v -run 'TestBudgetReservesOutputAllowance|TestBudgetDefaultOutputReserve|TestFoldWindowSemanticsUnchanged|TestProviderWindowGateSemantics' -count=1` 输出 4 个 PASS 行；新增字段进指纹后既有 AgentSpec 指纹迁移=配置变更事实一条（CHANGELOG）入台账；**同任务登记 concept budget 基线：`scripts/gates/check_concept_budget.py` 的 struct-fields AgentSpec 24→26（字段导出是 YAML 解码前提），并核验 `golden/runtime/architecture.json` 的 `concept_bindings` 域面基线**。
- [x] 1.5 消费者锚：`TestBudgetConsumerPathEvidence`——经正式请求装配入口（真实 AgentSpec→loop→model.Request）证明预算生效且预算决策在请求快照可回读 —— 验证：`go test ./assembly -v -run 'TestBudgetConsumerPathEvidence' -count=1` PASS。
- [x] 1.6 域门禁（lead 收口：W1 竞态两处已包 deliveryTestWaitFor（3 连跑绿）；host_document 两键已接（*int 可选→缺省 0）；concept gate 的 GovernanceEventLog 归 03 域收口；CHANGELOG 条目入波末文档批）：`go test ./assembly ./runtime/agent -v -count=1` 全绿 + **concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（api/config 两个可选**导出**字段属 D6 例外；若 S2 字段预算报警，按 baseline 棘轮对 struct-fields AgentSpec **24→26** 具名登记——配置字段必须导出供 YAML 解码，MUST NOT 退回未导出访问；不得静默放过）—— 验证：go test exit 0 + concept budget `errors: []`（F14：不存在 internal/config；配置面=api/config 可选字段）。

## scenario ↔ 测试映射（F27）

| spec scenario | 测试 |
|---|---|
| 大 schema 与长参数 | TestRequestBudgetAccountsAllFaces |
| 未知媒体 | 同上（unknown 分支断言） |
| 固定提示超窗 | 锚④ |
| 不可降级拒发（failed→failure-notice） | TestBudgetRefusalWhenUndegradable |
| fold 窗口不变 | TestFoldWindowSemanticsUnchanged |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。

> **执行状态（2026-11-05，W2）**：1.0–1.6 全部完成。lead 收口项：W1 交付竞态两处已包 `deliveryTestWaitFor`（3 连跑绿）；`host_document` 两键已接（`*int` 可选→缺省 0）；`GovernanceEventLog` 归 03 域；CHANGELOG 条目入波末文档批。
> - 本域实测：锚④ + 8 个新测试全绿；`go test ./assembly ./runtime/agent -v -count=1` = **128 PASS，exit 0**。
> - `check_concept_budget.py --json`：AgentSpec 24→26 具名登记后本域错误清零；仅剩 03 域 `GovernanceEventLog`；`concept_bindings` 仍只有 ConfigChange/new_max=1，本域未新增绑定；W1 改动另留 1 条 stale ctx-bare 基线（归 W1）。
> - 归因证据：干净 HEAD clone 同一 gate 为 `errors: []`；工作树 `GovernanceEventLog` 由 W1/03 面引入（接口文件本身未改，多了一个实现者使阈值为 2），未静默 baseline。
