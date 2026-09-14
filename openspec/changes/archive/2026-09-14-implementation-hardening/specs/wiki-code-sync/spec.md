## ADDED Requirements

### Requirement: 头条承诺与默认行为对齐

README 头条与心智模型中关于存储期限的表述 SHALL 与默认行为一致：以「事件不可变入库 + 默认按类型 TTL 遗忘曲线（3-30 天）+ 可配置永久」的实述替代「永久入库/永久存储」的绝对表述；配置永久之法 SHALL 可从文档直达。代码内失真注释（如 compaction.go 宣称的 gzip/L3 summarization）SHALL 改为与实现一致的真述。

#### Scenario: 读者据 README 预判默认行为

- **WHEN** 读者阅读 README 存储相关表述后以默认配置部署
- **THEN** 实际遗忘行为（类型 TTL 曲线）与文档预期一致，无「第 8 天票据落空」式的意外

#### Scenario: 化石注释复核

- **WHEN** 审阅者按 compaction.go 头注释寻找 gzip 压缩实现
- **THEN** 注释描述与代码事实一致（L3 为低价值类型清空 Content，无 gzip），不存在指向不存在机制的宣称
