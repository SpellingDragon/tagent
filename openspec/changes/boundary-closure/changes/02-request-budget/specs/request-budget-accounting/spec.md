# request-budget-accounting 恢复与强化（delta，MODIFIED 于既有能力）

> 5.9 裁决（grill r1 F7/F8 修正）：归属既有 `request-budget-accounting`（目录与 requirement 头逐字匹配主 spec）；MODIFIED 保留既有全部条款语义（不收缩、不删面），新增条款以 strengthening 追加；归档前提升为顶层 change 并实跑 archive 验证（不含 Aborted）。

## MODIFIED Requirements

### Requirement: 完整预算输入与诚实精度

预算 SHALL 包括 system、messages、工具 schema/参数、reasoning、ContentParts、任务板和恢复提示；无法可靠估计的媒体 MUST 标 unknown，不能当零或宣称精确。输入预算与输出 limit 语义保持分离。预算裁决 MUST 位于最终请求拼装处（模型请求离开系统前的最后一步），对全部已知输入面统一计量：固定提示、技能正文、动态环境材料、投影历史、工具声明、reasoning、ContentParts、任务板与恢复提示同入总量。窗口语义 MUST 三分：provider context window（新增可选声明）、输出余量（请求层 GenerationConfig.MaxTokens 或声明值；未声明时 MUST 取有界缺省余量，具体常量在 design）、replay/fold window（AgentSpec.MaxTokens 既有语义 MUST NOT 改变）。输入侧预算=provider 窗口−输出余量。窗口缺省规则 MUST 可判定：**预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0（缺省推导=MaxTokens 本值；AgentSpec.MaxTokens 仍只作 replay 窗口，预算仅读它推导缺省）；MaxTokens=0 且未显式声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**。超限处置 MUST 按可观察优先级降级（可让位材料先让位），系统必需面不可让位；降级后仍超限 MUST 显式拒发并落可观测事实，MUST NOT 静默全量进入请求。**拒发 MUST 以 failed 终态落链，并按既有 failure-notice 告知（不新增通道、不改 failed 触发条件）。**

#### Scenario: 大 schema 与长参数

- **WHEN** 正文很短而 schema 或参数很长
- **THEN** 触发判定包含这些已知开销，纯固定开销超限时明确拒发

#### Scenario: 未知媒体

- **WHEN** 请求包含缺乏可用计量元数据的媒体
- **THEN** 保留现有请求能力并报告 unknown，不宣称已保证 provider 总窗口安全

#### Scenario: 固定提示超出窗口（新增）

- **WHEN** 历史窗口被声明为 32 token 而固定提示约 10KB
- **THEN** 最终请求被统一预算约束：按声明优先级降级或显式拒发

#### Scenario: 不可降级时拒发（新增）

- **WHEN** 系统必需面自身已超限
- **THEN** 请求被显式拒绝，拒绝事实可被后续读取
- **AND** 该轮以 failed 终态落链，并按既有 failure-notice 告知（不新增通道）

#### Scenario: fold 窗口语义不变（新增）

- **WHEN** 预算恢复启用后既有 FoldWindow/压缩阈值配置运行
- **THEN** replay/压缩行为与恢复前一致（回归锚锁定）
