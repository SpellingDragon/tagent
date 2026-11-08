# training-sample-loop Specification

## Purpose
定义训练样本闭环：样本须同时具备可回读的实际请求、独立验收结论与入 rl annex 的验收记录，三要素缺一即标记 incomplete 且不得进入可发布集；样本资格门在 Seal 前评估，回读失败关闭。

## Requirements
### Requirement: 样本三要素齐备

一个训练样本 MUST 同时具备可回读的实际请求（哈希对账通过）、独立验收结论（阅卷员判分，不采信执行路径自述）与**验收结论入 rl annex**（新 Line 记录种类 `RecordKindAnnex`：AttemptID 关联、append-only；判定+规则版本+产物引用；不改 AttemptRecord 字段）；annex MUST 以 `AttemptID`+bench 任务 ID 为 join key；**annex payload MUST 经既有 Line 编解码往返保真（实现形态见 design）**。三要素缺一的样本 MUST 标记 incomplete 且 MUST NOT 进入可发布集。样本资格门 MUST 在 Seal 之前评估并把判定写入 annex（annex 记录先于封存落账；Seal 后 sink 拒收不变）；齐备率 MUST 以**全部样本（含 incomplete）**为分母。

奖励向量派生不在本变更范围；本域 MUST NOT 写 feedback 事件，也 MUST NOT 从任务终态或 trace 存在派生 reward 向量。

#### Scenario: 缺验收结论

- **WHEN** 样本有回读请求但无独立验收结论（或 annex 中无验收判定）
- **THEN** Seal 前资格门评估并写入 annex，样本标记 incomplete，不进可发布集，齐备率如实下降（分母=全部样本，含该 incomplete 样本）

#### Scenario: 自述与产物矛盾

- **WHEN** 执行路径自述成功但任务产物缺失
- **THEN** 独立验收判 fail，样本如实记录为负样本

#### Scenario: 验收结论入 annex

- **WHEN** 独立验收器完成对任务产物的核验
- **THEN** 判定+规则版本+产物引用写入 Line 的 `RecordKindAnnex` 载荷（append-only，AttemptID 关联），join key 含 `AttemptID` 与 bench 任务 ID，样本可按身份聚合
- **AND** annex payload 以既有 Line 记录形态承载、经既有读取面读取（MUST NOT 新增 Line 导出字段；实现见 design）
- **AND** 编解码往返后内容一致（序列化分支保真）

### Requirement: 回读失败关闭

快照读回哈希不一致 MUST 判回读失败并关闭采信，MUST NOT 以文件存在代替对账。

#### Scenario: 哈希漂移

- **WHEN** 读回内容与记录摘要不一致
- **THEN** 该要素缺失，样本不得标记 complete

