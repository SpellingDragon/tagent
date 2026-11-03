# task-skeleton-compression Delta

## MODIFIED Requirements

### Requirement: 压缩触发器单维化（token 容量阈值）

压缩整理 SHALL 只由渲染容量超过声明阈值触发；候选是否实际存在 SHALL 只作为执行门，不得成为第二容量阈值。容量未超阈时 MUST 提前返回且不触碰投影；容量超阈但无新候选时 SHALL no-op，不写空 request/product。存在可折候选时，不得因其材料量单独未超阈而抑制整理。

#### Scenario: 未超阈值时完全直通
- **GIVEN** 投影渲染容量低于阈值
- **WHEN** 执行压缩判定
- **THEN** SHALL 直接返回，不折叠、不定级、不重建 refs

#### Scenario: 超阈值但无可折候选
- **GIVEN** 投影容量超过阈值且没有本轮新候选
- **WHEN** 执行压缩判定
- **THEN** SHALL 不写压缩事实、不重写投影；实际模型请求仍由既有预算装配约束

#### Scenario: 超阈值且候选材料自身较小
- **GIVEN** 总容量超过阈值，可折过程材料自身低于阈值
- **WHEN** 执行压缩判定
- **THEN** SHALL 仍产生该真实候选，不得用第二材料阈值拒绝它

#### Scenario: 计划与直接折叠等价
- **WHEN** 同一投影分别经 PlanByAge+ApplyProducts 与 FoldByAge 处理
- **THEN** 两者的产物、覆盖键与最终投影 SHALL 等价，重复执行不得新增同义事实
