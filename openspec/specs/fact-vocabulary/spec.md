# fact-vocabulary Specification

## Purpose

定义事实类别契约（`event.Fact` 的身份与类型两问）、每个事实族在所属包内的自证与登记义务、消费器协议（读事实→消费→产新事实），以及呈现视图的归属边界。
## Requirements
### Requirement: 事实类别契约

框架 SHALL 在 event 包声明事实类别接口（`Fact`：身份与类型两问），每个事实族（核心事件、通知、训练记录、声明、引用票据、压缩请求、压缩产物、收尾判定、注意力；编号 6「回放结算」已退役留空）MUST 在其所属包内实现该接口；事实族的归属包 MUST 在词汇表文档中逐族登记（族/身份/类型/归属/锁定机制）。

**一身份一不可变事实 SHALL 以一个落链单元表达**：一条事实的正文载体（原文族）与身份载体（事件与索引族）SHALL 同属一个提交单元；同一身份的重复提交 SHALL 答复已存在而不落第二份字节，异内容 SHALL 具名拒绝。**「同一事实」的判据 SHALL 是领域等价**（规范化正文相等，且调用方声明的范围戳按通配规则相容，且任务身份相同），MUST NOT 以提交位置（分区/窗口/序号）相等或字面字节相等替代——同一事实在不同站点可以携带不同的字面字节而仍是一事实，同一位置也完全可以承载被拒提交的空缺。

**「未找到旧键」MUST NOT 被推导为「这是一个新事实」**：正常续轮会以稳定身份重交同一事实，键未命中不构成新事实的证据。提交方 SHALL 以身份判重，MUST NOT 以「键查无」造出第二份事实。

#### Scenario: 已登记族自证为事实

- **WHEN** 将 Event/Notice/AttemptRecord/Declaration/OriginalRef/CompressRequest/CompressProduct/FinalRoundRequest/AttentionFact 赋给 Fact
- **THEN** 编译通过，且各族的身份与类型两问返回稳定值（同一事实同一身份）

#### Scenario: 词汇表可核

- **WHEN** 读者按词汇表核查某族
- **THEN** 文档的归属包与身份/锁定机制 MUST 与该包实现一致

#### Scenario: 稳定身份重交不落第二份

- **WHEN** 同一条事实在一个执行站点被再次提交（续轮重交同一稳定身份）
- **THEN** 提交 SHALL 答复已存在并保持既有字节与位置，MUST NOT 落第二份正文或第二个索引条目

#### Scenario: 领域等价而非字节或位置相等

- **WHEN** 两次提交携带不同字面字节（范围戳一次声明、一次未声明）而规范正文相同
- **THEN** 判定 SHALL 为同一事实并答复已存在
- **AND** 两次提交规范正文不同时 SHALL 具名冲突，MUST NOT 以位置相同判为同一事实

#### Scenario: 未命中旧键的重交仍判已存在

- **WHEN** 一次提交在本视图查不到旧键，而链上已有同一身份的事实，随后以同一身份再次提交
- **THEN** 判重 SHALL 经身份判据答复已存在
- **AND** MUST NOT 因查无旧键而判为新事实并落第二份

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

事实族登记 SHALL 新增第十一族（既有九族编号不变、第六号留空不排）：**交付结算**——归属包 `assembly`、身份 `delivery.<交付身份>.<终态>`（键含状态；同键同内容幂等、同键异内容冲突拒收；按 `delivery.<身份>.` 前缀回读全状态线）、类型 `delivery.settlement`、锁定机制=追加式结算+按结算族构造性豁免 TTL 墓碑化，无前瞻注册、无未挂载债务。FACT-VOCABULARY 文档与 GLOSSARY 事实族登记表已同步登记该族与表尾计数。

#### Scenario: 第十一族登记可回读

- **WHEN** 查阅事实族登记表与 FACT-VOCABULARY 文档
- **THEN** 交付结算族以归属包/身份/类型/锁定机制四要素在列，既有九族（编号 1-5、7-10 不变，6 留空不排）条目逐字不变
