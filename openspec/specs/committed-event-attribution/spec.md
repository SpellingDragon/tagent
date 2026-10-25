# committed-event-attribution Specification

## Purpose
TBD - created by archiving change optimize-committed-facts. Update Purpose after archive.
## Requirements

### Requirement: 提交成功才发布可召回票据
MemoryPlugin SHALL 在 StoreEvent 成功后才发布其 event_key/partition_id、投影引用与因果游标；失败 MUST NOT 向下游提供可用持久票据，且原正文、失败信息和凭据降级机制继续可达。

#### Scenario: 写失败后下一条成功
- **WHEN** 同因果域第一次写失败、下一条成功
- **THEN** 失败键不成为下一条父键、不进入投影，成功票据可GetEvent取回。

#### Scenario: 未接存储与关系失败
- **WHEN** store为nil，或内容已存但SetParent失败
- **THEN** 前者无持久票据；后者仍有可用事实但不冒称因果完整。

### Requirement: 因果域内线性化
同 `(partition,session)` 的读父到推进 SHALL 形成一条提交顺序；不同域 MUST 可并行；活跃锁记录不得被游标上界回收。

#### Scenario: 同域并发与异域阻塞
- **WHEN** 两个同域事件并发且另一域存储被阻塞
- **THEN** 同域形成无覆盖的已提交链，另一域不阻塞其前进。

### Requirement: 不完整回溯可辨
回溯 SHALL 保留Complete/Capped并补可选reason；缺祖先、关系错误、无关系能力、环和上限不得统一伪装空历史。不能从缺失推断TTL或权限原因。

#### Scenario: 回溯途中断链
- **WHEN** 起始事实存在而祖先不可取得
- **THEN** 返回已读链、Complete=false和具名reason；首键缺失仍返回错误。

### Requirement: Query 只报告已提交事实与水位

Query SHALL 只呈现**已成功提交**的事实（可召回票据）；未提交/提交不确定的条目 MUST NOT 出现在读面为"可用"。Query SHALL 报告源水位与滞后，MUST NOT 把多维状态相加、MUST NOT 声称跨所有后端原子。

#### Scenario: 未提交不出现在读面（失败）

- **WHEN** 同因果域一次写失败
- **THEN** 失败键不进入读面投影
- **AND** 下一条成功后其票据可被 Query 取回

#### Scenario: 报告滞后（并发）

- **WHEN** 某源滞后于快照 revision
- **THEN** Query 携带该源水位与滞后，不声称全局原子

### Requirement: 消费归属锚定已提交事实

每条逻辑消费 SHALL 归属到消费方身份与一个已成功提交的事实键；提交结果不确定的事实 MUST NOT 被报告为已消费或已完成。消费确认 SHALL 引用其所消费的已提交事实键；缺少该键时 MUST NOT 声明消费完成。多个消费者对同一已提交事实的读取 SHALL 各记独立归属，MUST NOT 合并为一条消费或复制事实。读面是否呈现某事实沿用本能力「Query 只报告已提交事实与水位」，本要求不重复定义。

#### Scenario: 提交不确定不记消费

- **WHEN** 某事实的提交结果不确定
- **THEN** 消费方 MUST NOT 以该事实键记录消费完成
- **AND** 该消费 MUST NOT 进入完成计数

#### Scenario: 两个消费者各自归属

- **WHEN** 遥测投影与任务续转各自消费同一已提交通知事实
- **THEN** 两条消费归属 SHALL 分别记录消费方身份与同一事实键
- **AND** MUST NOT 生成第二份事实

#### Scenario: 完成确认引用事实键

- **WHEN** 消费方声明一条通知已处理完成
- **THEN** 该声明 SHALL 携带其所消费的已提交事实键
- **AND** 缺少事实键的完成声明 MUST NOT 被接受
