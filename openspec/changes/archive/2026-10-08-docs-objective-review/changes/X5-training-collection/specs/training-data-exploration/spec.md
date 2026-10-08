# training-data-exploration Specification

## ADDED Requirements

### Requirement: 采集链断点图交付

报告 SHALL 交付"采集链断点图"：从模型调用→轨迹行→事件库→反馈→转换器→训练样本的完整数据流，标出每个断点（缺字段/缺外键/缺工具/缺契约）与其两端的数据形态。E9-E11 的改造优先级 SHALL 以此图排序。

#### Scenario: 断点可稽

- **WHEN** 编排者四查本报告
- **THEN** 断点图 ≥5 个节点且每个断点有两端 文件:符号 佐证

### Requirement: 最小改造面定位

报告 SHALL 为"随运行采集 reward"定位最小改造面：至少评估（a）轨迹行补 event_key、（b）转换器经事件中转 join、（c）反馈侧补 trace_id 三条路线的改动点（文件:符号 级）与代价对比，并明确推荐其一或"保留现状"——只定位不实现。

#### Scenario: 三线对比齐备

- **WHEN** 汇总引用改造建议
- **THEN** 三条路线各有改动点清单与代价行
