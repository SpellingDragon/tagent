# memory-storage-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D1 域报告 SHALL 覆盖六维（预期特性/架构设计/过度设计嫌疑/缺陷设计嫌疑/推理友好性/训练友好性），每维 SHALL 含 A/B/C/D 评分与置信度，并 SHALL 含"本域最尖锐的三个问题"小节。

#### Scenario: 报告章节齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节、"最尖锐的三个问题"、代码抽查小节全部命中

### Requirement: 关键机制断言佐证

报告 SHALL 对以下机制中至少 2 个给出源码级佐证：LSM L0-L3 分层压实、TTL 遗忘默认值（3-30 天/-1 永久）、票据召回零幻觉的测试守护、因果链 RelationStore 写入路径。佐证 SHALL 精确到 `文件:符号` 并注明文档声明与代码是否一致。

#### Scenario: 漂移明示

- **WHEN** 抽查发现文档声明与代码不符
- **THEN** 报告在缺陷设计嫌疑维明示冲突双方证据，而非择一隐匿

### Requirement: README 特性兑现判定

报告 SHALL 对 README 场景一（卡片折叠/票据召回/语义召回）与"三层数据表示""压缩永不动存储"承诺给出兑现判定（兑现/部分兑现/仅文档），判定 SHALL 引用具体证据。

#### Scenario: 判定可溯源

- **WHEN** 汇总阶段引用 D1 兑现判定
- **THEN** 判定可回溯到报告内的证据行
