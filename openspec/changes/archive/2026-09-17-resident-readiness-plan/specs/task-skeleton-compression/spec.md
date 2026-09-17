## ADDED Requirements

### Requirement: 浓缩卡片票据机器校验

curateCards 接纳 LLM 浓缩文本前 MUST 校验：输出票据是输入旧半区票据集合的子集，且包含首尾与所有高亮条目的必需票据；缺失、伪造、格式不可解析时 SHALL 拒绝该文本并复用原卡片的确定性下沉。校验 SHALL 不新增 LLM 调用，不改变容量单维触发与整理间冻结。

#### Scenario: 模型省略必需票据
- **WHEN** 浓缩文本短且非空但丢失首尾或高亮 key
- **THEN** 不采用该文本，确定性回退，原始 FullEvent 不变

#### Scenario: 模型生成未知票据
- **WHEN** 输出包含输入没有的 key
- **THEN** 判失败，不把伪造票据写进 compaction 载荷

#### Scenario: 合法浓缩
- **WHEN** 输出只含输入票据并完整包含必需集合且满足预算
- **THEN** 接纳并在序列化/恢复后保留相同可解析票据

### Requirement: 卡片预算与失去导航显式化

下沉 SHALL 只减少导航视图，不修改原始事实；earlier 计数 SHALL 表示下沉项。单条卡片超预算仍 SHALL 有明确处理，保留票据与截断标记；必需票据无法在预算内表达时 SHALL 返回 budget-unrepresentable 状态并可观测，不无界扩张或静默删票据。under-budget 轮 SHALL 不触发新整理、不改变既有前缀。

#### Scenario: 无模型或调用失败
- **WHEN** 卡片超限且模型缺失/失败
- **THEN** 原卡片走确定性下沉，有计数，原文仍受原有 TTL 而非被本次压缩删除

#### Scenario: 单条超长卡片
- **WHEN** 只余一条卡片仍超上限
- **THEN** 截短文字并保留票据与标记；若连必需票据也放不下，明确报告预算无法表达
