# compression-pipeline Delta

## MODIFIED Requirements

### Requirement: 压缩的判定与执行各成事实

压缩 SHALL 分三段：producer 判定并提交 `compress.request`；consumer 读该 request 执行折叠并提交 `compress.product`；投影 SHALL 从 `compress.product` 恢复产物条目。request 的事实提交失败时，producer MUST 返回错误且 MUST NOT 把该判定交给内存消费者；任何 product MUST 有已提交的对应 request。

#### Scenario: 一次判定一次产物
- **WHEN** 判定产出一条 `compress.request`
- **THEN** 消费方 SHALL 至多提交一条同 request 的 `compress.product`，其键为产物负键、覆盖区间按原序

#### Scenario: request 提交失败
- **WHEN** producer 的 request 事实提交失败
- **THEN** 观察入口 SHALL 返回该错误，消费者 MUST NOT 接受内存中的 request，也 MUST NOT 生成对应 product

#### Scenario: 未接消费方时行为等价
- **WHEN** 宿主只接判定、未接消费
- **THEN** `compress.request` 照旧提交，产物不生成，投影呈现等价于无该机制时的现算结果

#### Scenario: 恢复不重折
- **WHEN** 重启恢复读到 `compress.product`
- **THEN** 投影 SHALL 以该事实的键与正文重建合成条目，MUST NOT 重新折叠；只有读不到 product 的 request 才视为未执行的判定
