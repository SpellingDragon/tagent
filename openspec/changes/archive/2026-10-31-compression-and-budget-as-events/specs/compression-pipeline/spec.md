# compression-pipeline

## ADDED Requirements

### Requirement: 压缩的判定与执行各成事实

压缩 SHALL 分三段：producer 判定并提交 `compress.request`；consumer 读该 request 执行折叠并提交 `compress.product`；投影 SHALL 从 `compress.product` 恢复产物条目。

#### Scenario: 一次判定一次产物
- **WHEN** 判定产出一条 `compress.request`
- **THEN** 消费方 SHALL 至多提交一条同 request 的 `compress.product`，其键为产物负键、覆盖区间按原序

#### Scenario: 未接消费方时行为等价
- **WHEN** 宿主只接判定、未接消费
- **THEN** `compress.request` 照旧提交，产物不生成，投影呈现等价于无该机制时的现算结果

#### Scenario: 恢复不重折
- **WHEN** 重启恢复读到 `compress.product`
- **THEN** 投影 SHALL 以该事实的键与正文重建合成条目，MUST NOT 重新折叠；只有读不到 product 的 request 才视为未执行的判定
