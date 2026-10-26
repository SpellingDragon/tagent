# trainer-adapter 规格（delta）

## ADDED Requirements

### Requirement: 显式能力协商与 named rejection

Trainer Adapter SHALL 在建立连接时显式声明：`algorithm`、`policy_version`、`tokenizer_digest`、`chat_template_digest`、`required_logprobs`、`max_policy_staleness`、`loss_normalization`、`sample_merge_policy`，以及可接受的记录 schema 版本集合。任一必需字段缺失、取值未知或与记录冲突时，adapter MUST 以**具名拒收**（稳定的机器可读码 + 字段定位）拒绝整批或该 Attempt，MUST NOT 静默降级、猜测默认值或把不完整数据标成在线 RL 数据。

#### Scenario: 协商字段齐备（正常）

- **WHEN** adapter 声明全部必需字段且 schema 版本在支持集合内
- **THEN** 协商 MUST 成功并返回可训练资格判定所需的声明快照
- **AND** 该快照 MUST 随导出制品一起记录，供事后复核

#### Scenario: 缺必需字段（失败）

- **WHEN** 声明的 capability 缺 `max_policy_staleness` 或 `required_logprobs`
- **THEN** adapter MUST 返回具名拒收并定位到缺失字段
- **AND** MUST NOT 以零值或默认窗口继续协商

#### Scenario: 未知 schema 版本（失败）

- **WHEN** 记录 schema 版本不在 adapter 声明的支持集合内
- **THEN** adapter MUST 具名拒收整批并列出双方版本
- **AND** MUST NOT 按"最接近版本"解析

### Requirement: 逐 Attempt 核对声明与实际

adapter SHALL 对每个 Attempt 核对：服务端权重版本与声明 `policy_version` 的关系、tokenizer/chat template 摘要与声明摘要、token 账本来源与 token 数、以及算法要求的 logprob 状态。任一项不一致 MUST 具名拒收该 Attempt 并保留字段级差异，MUST NOT 只报布尔失败或自由文本。

#### Scenario: 逐 Attempt 一致（正常）

- **WHEN** 一批 Attempt 的版本、摘要、token 数与 logprob 均满足声明
- **THEN** 全部 Attempt MUST 通过核对，拒收计数为 0
- **AND** 核对结果 MUST 可逐 Attempt 追溯

#### Scenario: 摘要不一致（失败）

- **WHEN** 某 Attempt 的 `chat_template_digest` 与声明不同
- **THEN** 该 Attempt MUST 被具名拒收，并记录两侧摘要
- **AND** 同批其余 Attempt MUST NOT 因该条被连带丢弃（除非同源污染判定）

### Requirement: token 精确不等于算法满足

`token_exact`（token 级精确对齐）SHALL NOT 被视为已满足任一 RL 算法；adapter MUST 另过该算法要求的 logprob 与策略一致性检查。算法要求的检查能力缺失时 MUST 拒收该算法，MUST NOT 以 token 精确为由默认通过。

#### Scenario: token 精确但缺策略一致性（失败）

- **WHEN** 账本 token 与服务端逐 id 相等，但缺少算法要求的策略一致性证据
- **THEN** adapter MUST 具名拒收该算法，原因定位到缺失的一致性证据
- **AND** MUST NOT 以 `token_exact=true` 作为合格结论

#### Scenario: 检查齐备（正常）

- **WHEN** token 逐 id 相等、logprob 齐备且策略版本一致
- **THEN** adapter MUST 判定该 Attempt 满足所声明算法的输入条件
- **AND** 判定 MUST 记录所用算法版本与判据版本

### Requirement: 在线 RL 导出最低字段与拒收条件

在线 RL 导出 SHALL 至少包含：root episode 与局部 transition 标识、实际 token 账本、行为策略、算法要求的 logprobs、终局奖励及其作用范围、mask、weight、adapter 版本。缺真实 token 或必需 logprob、行为策略不明、超陈旧窗口、因果或奖励为未知时 MUST 拒收，MUST NOT 以零值填充后导出。

#### Scenario: 字段齐备导出（正常）

- **WHEN** 一个 root episode 的 Attempt 与奖励范围齐备且通过上述核对
- **THEN** 导出 MUST 产出可追溯到原 episode/attempt 的记录
- **AND** 每条记录 MUST 携带 adapter 版本与归一形式

#### Scenario: 奖励未知（失败）

- **WHEN** 某 root episode 的终局奖励为 `unknown`
- **THEN** 该 episode MUST 被拒收并具名"奖励未知"
- **AND** MUST NOT 以 0 奖励导出

#### Scenario: 缺真实 token（失败）

- **WHEN** 某记录无实际 token IDs（含无逐 id 相等的再分词证明）
- **THEN** 导出 MUST 具名拒收并保留原因计数
- **AND** MUST NOT 出现在导出制品中
