# event-plugin-review Specification

## ADDED Requirements

### Requirement: 六维评审完整性

D2 域报告 SHALL 覆盖六维，每维含评分与置信度，含"本域最尖锐的三个问题"小节（契约同 D1 spec，此处不重复展开）。

#### Scenario: 章节锚点齐备

- **WHEN** 编排者 grep 报告锚点
- **THEN** 六维章节与尖锐三问全部命中

### Requirement: 上下文可复现性专判

报告 SHALL 专项判定"同状态同输入→相似上下文"是否成立：时间线渲染单点性、prompt 装配确定性、事件入队顺序稳定性三者各给结论与证据。此项为判准 T（训练友好）在本域的核心依据。

#### Scenario: 可复现性结论可引用

- **WHEN** W2 汇总训练友好性结论
- **THEN** D2 报告对上述三项各有明确判定（成立/不成立/证据不足）

### Requirement: 关键契约佐证

报告 SHALL 对以下至少 2 项给出 `文件:符号` 佐证：事件元数据契约、时间线前缀读写单点、EventTypeSpec 注册表、prompt 热重载 Source。

#### Scenario: 漂移明示

- **WHEN** 抽查发现漂移
- **THEN** 报告明示冲突双方证据
