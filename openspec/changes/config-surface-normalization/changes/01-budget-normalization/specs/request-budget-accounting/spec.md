## MODIFIED Requirements

### Requirement: 完整预算输入与诚实精度

预算 SHALL 包括 system、messages、工具 schema/参数、reasoning、ContentParts、任务板和恢复提示；无法可靠估计的媒体 MUST 标 unknown，不能当零或宣称精确。输入预算与输出 limit 语义保持分离。预算裁决 MUST 位于最终请求拼装处（模型请求离开系统前的最后一步），对全部已知输入面统一计量：固定提示、技能正文、动态环境材料、投影历史、工具声明、reasoning、ContentParts、任务板与恢复提示同入总量。

窗口语义 MUST 三分：

- **context_window**（物理容量／预算门窗口，可选声明；原 `provider_context_window` 归位改名）；
- **输出余量**＝有效输出限（请求层 `GenerationConfig.MaxTokens`，由声明 `max_tokens` 经 `decorate` 注入 provider；**未声明输出限 = 不预留，reserve=0**）；
- **replay_window**（折叠／投影窗口；原 `AgentSpec.MaxTokens` 回放窗语义归位改名，**只驱动折叠**，折叠行为与改名前一致）。

输入侧预算 = `context_window − 有效输出限`（**派生量，无第三独立旋钮**）。**预算门开 ⟺ `context_window>0`（唯一来源）；`replay_window` MUST NOT 兼任预算窗缺省；`context_window` 未声明 = 预算门不启用（记 debug 事实，不裁剪不拒发）**。有效输出限 SHALL 单一来源算一次，同供 `decorate`（发 provider）与预算扣减，MUST NOT 两处各算。

超限处置 MUST 按可观察优先级降级（可让位材料先让位），系统必需面不可让位；降级后仍超限 MUST 显式拒发并落可观测事实，MUST NOT 静默全量进入请求。**拒发 MUST 以 failed 终态落链，并按既有 failure-notice 告知（不新增通道、不改 failed 触发条件）。**

MUST NOT 凭空按窗口比例预留输出余量：`DefaultOutputReserveDivisor`、`OutputReserveOf`、`output_reserve` 独立声明字段 SHALL 删除（无领域来源的 `window/4` 缺省是本Requirement 显式取代的旧机制）。

#### Scenario: 大 schema 与长参数

- **WHEN** 正文很短而 schema 或参数很长
- **THEN** 触发判定包含这些已知开销，纯固定开销超限时明确拒发

#### Scenario: 未知媒体

- **WHEN** 请求包含缺乏可用计量元数据的媒体
- **THEN** 保留现有请求能力并报告 unknown，不宣称已保证 provider 总窗口安全

#### Scenario: 固定提示超出窗口

- **WHEN** `context_window` 被声明为 32 token 而固定提示约 10KB
- **THEN** 最终请求被统一预算约束：按声明优先级降级或显式拒发

#### Scenario: 不可降级时拒发

- **WHEN** 系统必需面自身已超限
- **THEN** 请求被显式拒绝，拒绝事实可被后续读取
- **AND** 该轮以 failed 终态落链，并按既有 failure-notice 告知（不新增通道）

#### Scenario: fold 窗口语义不变

- **WHEN** 预算恢复启用后既有 `replay_window`/压缩阈值配置运行
- **THEN** replay/压缩行为与归位前一致（`TestFoldWindowSemanticsUnchanged` 回归锚锁定）

#### Scenario: 无输出限不预留（c75 症状直译）

- **WHEN** agent 声明 `replay_window=128000, context_window=128000`、未声明输出限，输入约 96407 token
- **THEN** reserve=0（无声明=无预留）、输入预算=128000，96407<128000 **放行**；MUST NOT 凭空扣 `window/4=32000` 致预算 96000 拒发

#### Scenario: 输出限真发 provider

- **WHEN** agent 声明 `max_tokens=8000`（输出限）
- **THEN** `GenerationConfig.MaxTokens=8000` 经 `decorate` 真发 provider，且 reserve=8000、输入预算=`context_window−8000`

#### Scenario: 只声明 replay_window 门闭（乙）

- **WHEN** agent 只声明 `replay_window=64`、未声明 `context_window`，输入超 64
- **THEN** 预算门闭、整包发送（门开 ⟺ `context_window>0`）；MUST NOT 因 `replay_window>0` 派生门开拒发