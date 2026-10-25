# online-rl-protocol 规格（delta）

## ADDED Requirements

### Requirement: 每个采样 Attempt 的行为策略与参数记录

每个进入在线 RL 训练候选集的实际采样 Attempt SHALL 记录：行为策略权重版本、实际生效的采样参数、以及承载该 Attempt 的模型调用标识。行为策略版本 MUST 是该 Attempt **实际产生时**服务的权重版本，MUST NOT 以训练侧当前策略、导出时刻策略或未版本化的"默认策略"冒充。缺行为策略版本的 Attempt MUST 判为无在线 RL 资格并给出具名原因，MUST NOT 进入训练样本。

#### Scenario: 策略版本随采样落账（正常）

- **WHEN** 受控服务在权重版本 `policy_version=P` 下采样出一个 Attempt
- **THEN** 该 Attempt 的记录 MUST 携带 `P` 与温度/top_p 等实际生效参数
- **AND** 记录 MUST NOT 因导出时训练侧已刷新到 `P'` 而改写为 `P'`

#### Scenario: 缺行为策略版本（失败）

- **WHEN** 某 Attempt 只有响应内容与 token，无行为策略版本
- **THEN** 该 Attempt MUST 被判为无在线 RL 资格，原因 MUST 具名为"缺行为策略版本"
- **AND** MUST NOT 以训练侧当前策略补齐后计入训练集

### Requirement: 真实 token 账本与摘要

每个在线 RL Attempt SHALL 记录服务端**实际**分词得到的 prompt token IDs 与 response token IDs，以及 tokenizer 摘要与 chat template 摘要。token IDs MUST 来自服务端实际分词结果；唯一例外是与 pinned tokenizer 摘要加 chat template 摘要一致的再分词，且 MUST 逐 id 相等才能视为同一账本。MUST NOT 从响应文本、其他模型或另一 tokenizer/模板补造 token IDs；无法取得时 MUST 记为 `unknown` 并拒收该在线 RL 格式，不得用文本近似替代。

#### Scenario: 服务端回传 token IDs（正常）

- **WHEN** 受控服务回传 prompt 与 response 的实际 token IDs 及 tokenizer/模板摘要
- **THEN** 记录 MUST 保存两组 token IDs 与两个摘要
- **AND** 记录 MUST 保存 token 数量与实际 `usage` 的一致性核对结果

#### Scenario: 仅文本回推 token（失败）

- **WHEN** 记录只有响应文本（或只有 token 文本与 logprob），无 token IDs 也无逐 id 相等的再分词证明
- **THEN** token 账本状态 MUST 为 `unknown`，在线 RL 导出 MUST 具名拒收
- **AND** MUST NOT 用文本重新分词的结果冒充服务端实际 token

#### Scenario: 模板或 tokenizer 摘要漂移（失败）

- **WHEN** 记录的 tokenizer 或 chat template 摘要与 adapter 声明不一致
- **THEN** 该 Attempt MUST 被拒收并定位到具体摘要字段
- **AND** MUST NOT 只更换模型名称后继续使用

### Requirement: logprobs 三态沿用与按算法判定

在线 RL 记录 SHALL 沿用既有 `LogprobsState` 三态语义：仅响应**确实返回**逐 token logprob 时标 `present`；provider 能力**声明**不支持时标 `unsupported` 并保留声明来源；**无证据**时 MUST 标 `unknown`，MUST NOT 默认当作"未返回"或"不支持"。当所选算法声明需要 logprobs 而该 Attempt 状态非 `present` 时 MUST 具名拒收；算法明确不需要 logprobs 时 MUST NOT 仅因缺 logprobs 拒收。

#### Scenario: present 需真实返回（正常）

- **WHEN** 服务端返回每个 response token 的行为 logprob 并被记录
- **THEN** 状态 MUST 为 `present`，逐 token logprob 与 token 位置一一对应
- **AND** token 个数与 logprob 个数 MUST 相等，否则具名拒收

#### Scenario: unknown 不被读作不支持（失败）

- **WHEN** 某 Attempt 既无返回的 logprob 也无 provider 声明
- **THEN** 状态 MUST 为 `unknown`
- **AND** MUST NOT 记为 `unsupported`，也 MUST NOT 被算法消费者当作"零 logprob"填充

#### Scenario: 算法不需要 logprobs（正常）

- **WHEN** adapter 声明的算法不要求 logprobs，某 Attempt 状态为 `unknown`
- **THEN** 该 Attempt MUST NOT 仅因 logprobs 缺失被拒收
- **AND** 拒收原因 MUST NOT 记为该算法不满足

### Requirement: 单角色归因与跨调用归一

首期在线 RL SHALL 固定工具、子代理、裁判等其他角色，只训练一个角色；每个训练单元的归因 MUST 落在该角色自己的决策与动作上。长任务允许跨多个模型调用，优势与损失权重 SHALL 按 root episode 与训练角色归一，并记录所用归一形式。`joint_credit`（多角色联合信用）SHALL 仅用于团队评估，MUST NOT 进入单角色训练目标。

#### Scenario: 多调用同属一个 root episode（正常）

- **WHEN** 一个任务跨三次模型调用完成，三次调用同属一个 root episode
- **THEN** 三个局部 transition MUST 各自保留，并共同归属该 root episode
- **AND** 权重归一 MUST 以 root episode 为单位记录，MUST NOT 让调用次数多的任务自动获得更大总权重

#### Scenario: 其他角色被计入训练（失败）

- **WHEN** 某训练样本把工具执行者或子代理的收益计入被训练角色的优势
- **THEN** 该样本 MUST 被拒收并具名"跨角色归因"
- **AND** 该收益 MAY 仅出现在团队评估报告中

### Requirement: 策略陈旧窗口

在线 RL rollout SHALL 携带其行为策略版本，版本标识 SHALL 单调可比较（权重代数或等价序号）；不可比较的版本标识 MUST 具名拒收。adapter 按声明的陈旧窗口判定可用性；超出窗口的 rollout MUST 按算法约定处理（首期：拒收并记账），MUST NOT 静默改标为当前策略版本继续训练。

#### Scenario: 超窗 rollout 被拒收（失败）

- **WHEN** 一次 rollout 的行为策略版本与训练批所用版本之差超过声明的 `max_policy_staleness`
- **THEN** 该 rollout MUST 被拒收并记录策略版本差与窗口值
- **AND** MUST NOT 通过改写策略版本字段使其变为可用

#### Scenario: 窗口内 rollout 可用（正常）

- **WHEN** 版本差在窗口内且其余字段齐备
- **THEN** 该 rollout SHOULD 进入训练批，并保留其真实行为策略版本
- **AND** 训练侧 MUST 能按该版本复算重要性权重
