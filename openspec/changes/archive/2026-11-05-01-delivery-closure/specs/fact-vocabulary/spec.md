# fact-vocabulary 登记交付结算族（delta：ADDED 于既有能力）

## ADDED Requirements

### Requirement: 交付结算族（第十一族）

事实族登记 SHALL 新增第十一族（既有九族编号 1-5、7-10 不变，6 留空不排）：**交付结算**——归属包 `assembly`、身份 `delivery.<交付身份>.<终态>`（键含状态；同键同内容幂等、同键异内容冲突拒收；按 `delivery.<身份>.` 前缀回读全状态线）、类型 `delivery.settlement`、锁定机制=追加式结算+TTL 前瞻豁免注册。FACT-VOCABULARY 文档与 GLOSSARY 事实族登记表同步加行（1.7 任务）；FACT-VOCABULARY 表尾计数行同步由 9 族更新为 10 族，GLOSSARY「已登记九族」两处同步更新。

#### Scenario: 第十一族登记可回读

- **WHEN** 查阅事实族登记表与 FACT-VOCABULARY 文档
- **THEN** 交付结算族以归属包/身份/类型/锁定机制四要素在列，既有九族条目逐字不变
