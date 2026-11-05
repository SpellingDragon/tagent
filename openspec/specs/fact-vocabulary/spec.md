# fact-vocabulary Specification

## Purpose

定义事实类别契约（`event.Fact` 的身份与类型两问）、每个事实族在所属包内的自证与登记义务、消费器协议（读事实→消费→产新事实），以及呈现视图的归属边界。
## Requirements
### Requirement: 事实类别契约

框架 SHALL 在 event 包声明事实类别接口（`Fact`：身份与类型两问），每个事实族（核心事件、通知、训练记录、声明、引用票据、压缩请求、压缩产物、收尾判定、注意力；编号 6「回放结算」已退役留空）MUST 在其所属包内实现该接口；事实族的归属包 MUST 在词汇表文档中逐族登记（族/身份/类型/归属/锁定机制）。

#### Scenario: 已登记族自证为事实
- **WHEN** 将 Event/Notice/AttemptRecord/Declaration/OriginalRef/CompressRequest/CompressProduct/FinalRoundRequest/AttentionFact 赋给 Fact
- **THEN** 编译通过，且各族的身份与类型两问返回稳定值（同一事实同一身份）

#### Scenario: 词汇表可核
- **WHEN** 读者按词汇表核查某族
- **THEN** 文档的归属包与身份/锁定机制 MUST 与该包实现一致

### Requirement: 消费器协议

消费器 SHALL 遵守同一协议：读事实（身份引用）→消费→产出新事实；消费器 MUST NOT 修改源事实或绕过持久层。框架的消费器实例（回合驱动、时间驱动、外部驱动）MUST 在拓扑文档中登记其驱动源。

#### Scenario: 两实例协议一致
- **WHEN** 回合驱动与时间驱动各消费一条事实
- **THEN** 两者都产出新事实且源事实身份与内容不变

### Requirement: 呈现视图归属

呈现视图词汇（Presentation/Gap/View）SHALL 归属 view 包（时态视图），plugin 包 MUST NOT 声明视图类型，只引用之；view 对 event 的依赖 MUST 仅为事实身份引用。

#### Scenario: 视图住在 view 包
- **WHEN** 检查 plugin 包的类型声明
- **THEN** 不存在 Presentation/Gap/View 声明，FinalPresenter 的呈现面引用 view 包类型

### Requirement: 交付结算族（第十一族）

事实族登记 SHALL 新增第十一族（既有九族编号 1-5、7-10 不变，6 留空不排）：**交付结算**——归属包 `assembly`、身份 `delivery.<交付身份>.<终态>`（键含状态；同键同内容幂等、同键异内容冲突拒收；按 `delivery.<身份>.` 前缀回读全状态线）、类型 `delivery.settlement`、锁定机制=追加式结算+TTL 前瞻豁免注册。FACT-VOCABULARY 文档与 GLOSSARY 事实族登记表同步加行（1.7 任务）；FACT-VOCABULARY 表尾计数行同步由 9 族更新为 10 族，GLOSSARY「已登记九族」两处同步更新。

#### Scenario: 第十一族登记可回读

- **WHEN** 查阅事实族登记表与 FACT-VOCABULARY 文档
- **THEN** 交付结算族以归属包/身份/类型/锁定机制四要素在列，既有九族条目逐字不变

