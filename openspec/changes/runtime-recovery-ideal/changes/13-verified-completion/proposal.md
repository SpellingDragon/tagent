# 13 验证完成（verified-completion）

> 一级依据：[design.md](../../design.md) D2 子域表 13、D8 设计门 **G4 证据放行**、D10「证据确实挡放行」行、D9 第 5 条「13 的放行测试先过，独立审查再验完整候选」。
> 本域只做「完成怎么算数」：不实现任何检查器；不触碰产品面、主 spec、golden、CI 配置、分支保护与用户规则。

## Why

完成判定今天可以被三样东西冒充：**登记表手填判词**、**源码关键词命中**、**退出码零但零测试**。已实测的事实：

```sh
python3 scripts/gates/check_dod_coverage.py      # exit 1，19 gap
gh api repos/SpellingDragon/tagent/branches/main/protection \
  --jq '.required_status_checks // "ABSENT"'     # ABSENT（main 无必需检查，只读查询）
openspec archive --help                          # 无 DoD 相关选项：-y 只跳过确认
```

1. **判词可手填**：`golden/runtime/dod_predicates.json` 的 `mutation_verdicts` 是登记表字段；`scripts/gates/check_dod_coverage.py:452` 只核 `ci_gate` 名是否属于工作流作业集合，不核该作业是否执行该锚。
2. **未达成仍可写全绿**：P0–P3、P5–P8 的变异「未登记」或「无运行记录其被判杀」，P4 记录为 `not_killed`（由 `journal-bytes` 断言先响，而登记断言是 `wallClock`），P5/P6 根本没有锚；`check_dod_coverage.py` 因此 exit 1，而仓库内仍可写出「全门绿」账（PR #39 HEAD `9547faca` 的 CI 恰好全红）。
3. **放行链没有拒绝面**：合并侧无必需检查（`required_status_checks` 为 `ABSENT`），归档侧 `openspec archive -y` 不检查完成判据，本地预检只跑它自己列的步骤。
4. **检查器自测不是完备性证明**：`test_check_runtime_evidence.py`、`test_check_event_integrity_mutations.py` 等 13 个自测证明的是「已列反例被拒」，不证明没有别类假绿。当前变异分类器已覆盖编译失败、超时、零测试命中、无该断言的 panic，但**未覆盖**：控制树里目标测试被跳过、目标字样出现在无关失败里、变异补丁未真正进入被测编译面。

## What

- **R1 证据链设计门 G4（只读）**：先确认既有三个执行/对账引擎的真实结果消费者与拒绝面——`scripts/gates/check_runtime_evidence.py`、`scripts/scenarios/run_runtime_scenarios.py`、`scripts/gates/check_event_integrity_mutations.py`——产出 G4 判决与差距表。复用它们，MUST NOT 新建平行评估系统或第二份合同解析器。
- **R2 真实证据聚合绑定**：完成证据 SHALL 绑定源树、命令与参数、环境、**期望的完整测试集合**、**实际执行计数（>0）**、量名与单位与计量区间、以及有效性（控制树真跑、合同摘要一致、证据同源、产物可追溯）。手填 killed、源码关键词命中、退出码零但零测试、只跑子集冒充全集，一律不作证据。
- **R3 公共拒绝入口与归档/合并接入**：同一份证据接成三处拒绝——本地预检、CI 必跑作业、归档与合并。归档侧因归档命令不核完成判据，拒绝入口须**前置独立运行**；合并侧的必需检查属外部权限，未配置时 SHALL 如实报告「未生效」。
- **假 killed 三类补齐**：控制树目标测试被跳过、目标字样出现在无关失败里、变异未真正应用/未进入被测编译面，各自具名判「验证失败」，MUST NOT 计为杀死。

## Capabilities

- `architecture-guardrails`（MODIFIED，三条既有要求）：「检查资产区分三类证据」、「故障变异必须由具名业务断言杀死」、「质量门迁移不得放宽判据」（三处一律以 Requirement 标题为锚，不再引行号）。标题已逐条核对在位，故用 MODIFIED 扩写既有要求，**不新建同义能力、不另立元数据真源**。
- `trajectory-verification` 已查标题（:6 重启点身份配对／:13 截尾即失败／:20 动态系统段单列／:27 验收失败必须可见／:34 真实消费验收失败可见），其管的是重启前缀验证与 SFT 消费验收的分类可见性，与本域的「执行集合与证据绑定」不重叠 → 不改，避免同一事实两处立法。

## 边界与依赖

- **依赖**：09.R2 的计量定义（量名、单位、区间、冷热分记）与 09.R1 固定的原始失败清单（证据格式的输入面）；10.R1 的判据—锚—作业对账表（本域消费它，不重定义）。
- **被依赖方**：一级 F4 归档、10.R3 归档前验证、09.R3 矩阵放行。
- **接口面**：复用既有证据格式（`check_runtime_evidence.py --bundle` 的 `control_tree_green`／`mutations[]`／`module_fingerprints`／`contract_digest`），不新增第二套 schema。
- **禁止事项**：
  - 不实现检查器、不改 `scripts/gates/**`、`scripts/scenarios/**`、`golden/runtime/**`、`.github/workflows/**`（本轮只规划）。
  - 不触发 CI、不改分支保护、不改工具权限（外部权限，须明示授权；未配置时只报告）。
  - 不以检查器自测冒充产品验收；不删 `event_integrity_scenarios.json` 的必跑项以求通过；不把「独立审查」写成可由机器替代。
  - 独立审查仍必要：证据能挡「已列反例」，不能自动理解自然语言规格的完整性；本域 MUST NOT 承诺自动覆盖规格全集。
