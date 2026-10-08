# inference-exploration Specification

## ADDED Requirements

### Requirement: 稳定段-可变段分段图交付

报告 SHALL 沿上下文装配链交付"稳定段/可变段"分段图（system / 工具声明 / 历史冻结段 / 面板 / 新近事件 / 综述），每段标注：变化频率、缓存命中预期、wall-clock 渗入点。E7 的预算与缓存问题 SHALL 以此图作答。

#### Scenario: 分段图可稽

- **WHEN** 编排者四查本报告
- **THEN** 分段 ≥6 段且每段有注入点 文件:符号 佐证

### Requirement: 估值一致性核验

H1/H2（双常数与低估）SHALL 给出消费方-常数对照表与预算判定输入集清单；结论区分"静态推演"与"有测试/数据佐证"两档。

#### Scenario: 推演不冒充实测

- **WHEN** 报告引用任何效率数字
- **THEN** 其来源（测试名/静态推演/文档自认）被显式标注
