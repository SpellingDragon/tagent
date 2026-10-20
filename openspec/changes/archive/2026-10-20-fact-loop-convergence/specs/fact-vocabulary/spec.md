# fact-vocabulary

## ADDED Requirements

### Requirement: 事实类别契约

框架 SHALL 在 event 包声明事实类别接口（`Fact`：身份与类型两问），每个事实族（核心事件、通知、训练记录、声明、引用票据）MUST 在其所属包内实现该接口；事实族的归属包 MUST 在词汇表文档中逐族登记（族/身份/类型/归属/锁定机制）。

#### Scenario: 五族自证为事实
- **WHEN** 将 Event/Notice/AttemptRecord/Declaration/Ticket 赋给 Fact
- **THEN** 编译通过，且各族的身份与类型两问返回稳定值（同一事实同一身份）

#### Scenario: 词汇表可核
- **WHEN** 读者按词汇表核查某族
- **THEN** 文档的归属包与身份/锁定机制 MUST 与该包实现一致

## ADDED Requirements

### Requirement: 消费器协议

消费器 SHALL 遵守同一协议：读事实（身份引用）→消费→产出新事实；消费器 MUST NOT 修改源事实或绕过持久层。框架的消费器实例（回合驱动、时间驱动、外部驱动）MUST 在拓扑文档中登记其驱动源。

#### Scenario: 两实例协议一致
- **WHEN** 回合驱动与时间驱动各消费一条事实
- **THEN** 两者都产出新事实且源事实身份与内容不变

## ADDED Requirements

### Requirement: 呈现视图归属

呈现视图词汇（Presentation/Gap/View）SHALL 归属 view 包（时态视图），plugin 包 MUST NOT 声明视图类型，只引用之；view 对 event 的依赖 MUST 仅为事实身份引用。

#### Scenario: 视图住在 view 包
- **WHEN** 检查 plugin 包的类型声明
- **THEN** 不存在 Presentation/Gap/View 声明，FinalPresenter 的呈现面引用 view 包类型