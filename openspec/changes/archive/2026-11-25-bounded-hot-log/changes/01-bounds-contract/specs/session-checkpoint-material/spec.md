# session-checkpoint-material 规格（delta，MODIFIED）

> 头逐字匹配主 spec `openspec/specs/session-checkpoint-material/spec.md` 的「### Requirement: 派生态有界且不删除已提交事实」。全文保留既有条款，仅做三处改读：把「按身份恒可读」改读为「按身份恒可定位读取（在线直读或经归档契约读）」，把热层增长的声明句按部署形态分记，并补已归档根的回退声明条件。承诺 id 与条件句的唯一家在 [contracts/archive.md](../../contracts/archive.md) §六（`[C1]`、`[C2]`）。

## MODIFIED Requirements

### Requirement: 派生态有界且不删除已提交事实

checkpoint 承载的派生态 SHALL 以开放项加水位表达，已结部分 SHALL 留在链上而不嵌入 checkpoint，checkpoint 大小 SHALL 有界于开放项数量。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实：在本构建写出的根上，按身份读一条已提交事实 SHALL 恒可定位读取（在线直读，或经归档契约的定位读），SHALL 返回其原字节与原索引位置，SHALL NOT 答 missing；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。根下的 `cold/kv/journal.log` 由旧构建写出，其冷档事实不在本构建读面内：该形态 SHALL 先经 `scripts/fold-cold-root` 折回新根再供本构建服务，折回前按身份读冷档事实答不存在，该退化是已声明的运维前置，不构成本要求的违反。

热层字节 SHALL 按部署形态分别记账：满足 `hot-log-bounds` [C1] 全部条件的部署，热卷字节 SHALL 有界于保留窗，归档卷增长 SHALL 显式声明且归档事实按身份可定位读取；未声明归档的部署，热层字节的增长 SHALL 仍被显式声明。任一形态下 MUST NOT 把增长声明成有界，MUST NOT 把归档增长读成热卷达标。

已归档的根 SHALL 由保留归档声明的构建继续服务；移除声明后归档事实按身份读不可得，该形态 MUST NOT 被接受为回退，回退 SHALL 走离线折回或前向修复，与 `cold/kv/journal.log` 折回同属已声明的运维前置。

#### Scenario: 已结集合不进入 checkpoint

- **WHEN** 一个会话积累了大量已结交付或已答压缩判决
- **THEN** checkpoint SHALL 以水位加开放项表达这些集合，checkpoint 大小 SHALL 不随已结数量线性增长

#### Scenario: 已提交事实按身份恒可定位读取

- **WHEN** 在本构建写出的根上按身份读一条已提交的事实，无论它落在哪一代、是否被某次切面覆盖、是否已迁入归档
- **THEN** 在线直读或经归档契约的定位读 SHALL 返回该事实的原字节与原索引位置，SHALL NOT 答 missing
- **AND** 索引点名而正文读不出或校验失败 SHALL 具名拒绝，MUST NOT 降级为「不存在」

#### Scenario: 旧冷档根未折回时按身份读答不存在

- **WHEN** 根下存在旧构建写出的 `cold/kv/journal.log` 而该根尚未折回
- **THEN** 本构建按身份读冷档里的事实 SHALL 答不存在，该形态 SHALL 被读作已声明的运维前置，MUST NOT 被读作本要求的违反
- **AND** 该形态 SHALL 先以 `go run ./scripts/fold-cold-root -source <旧根> -root <新根>` 折回新根（默认干跑只报待折键数，加 `-restore` 才写入），折回后按身份读 SHALL 返回原字节与源提交序

#### Scenario: 有界化不打断全史读者

- **WHEN** 一个有界化实现试图删除、覆盖或改写已被切面覆盖的事实
- **THEN** 该实现 SHALL 被拒绝，且按身份读全史的消费者 SHALL 仍能取回正文

#### Scenario: 已归档根移除声明不属受支持回退

- **WHEN** 一个已归档的根以不含归档声明的配置或回退构建启动
- **THEN** 归档事实按身份读 SHALL 不可得，该形态 SHALL 判为不支持的回退
- **AND** 受支持的回退 SHALL 是「保留归档声明、只回退代码」或「离线折回／前向修复」，MUST NOT 记作零迁移
