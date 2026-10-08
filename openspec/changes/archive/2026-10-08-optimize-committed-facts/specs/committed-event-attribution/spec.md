# committed-event-attribution Specification

## ADDED Requirements

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
