# 领域词汇、规范权威与公共面收敛

父变更：runtime-architecture-convergence。W0-A04 的 Q1–Q5 已由编排者采纳，并吸收 01/02/03/06 已裁定的概念归属。

## Why

现有 concept budget 只数接口方法、字段和文档形状，无法识别 Writers/Movement 这类零字段机制公共化，也无法识别 UnsettledSource 方法数不变但领域语义已改变。superseded 主 spec 继续包含 SHALL，使新旧架构同时具有规范权威。

## What Changes

- 保留 S1–S4 形状棘轮，但新增独立 domain-surface 结果；两者不可互相覆盖，产品验证仍另算。
- 仅对新增/改名的公共命名类型、接口、FactType/状态词要求绑定 canonical concept、类别与非 superseded 主 spec；不设符号总数 KPI。
- fact 必须有身份、消费者和恢复/事实目录证据；read 必须指向既有事实；declaration/port 附着既有概念；实现机制和测试支架不能借绑定成为领域词。
- superseded 主 spec 不得继续含现行 SHALL/MUST；有效残条迁移后删除旧 Requirements，历史留归档。
- GLOSSARY 只留领域词与关系；FACT 管身份/类型/注册；机制细节留在现有 wiki，不新增庞大文档。
- Attention 是审批/通知的在场读投影：primitive 拥有 question/answer/clearance 事实，approval-attention 拥有审批绑定，attention-clearance 只拥有读投影。
- CompressProduct 是 request→product→恢复所需事实；FinalRoundRequest 是消费循环预算判定事实。二者可保留事实身份，但不因此新增一级领域词。

## 边界与依赖

- **依赖**：01 的 segment/Turn 与唯一驱动，02 的回执/通知，03 的关闭，06 的压缩事实链。
- **被依赖方**：所有实施单的公共概念合并门与最终归档。
- **接口面**：现有 check_concept_budget.py CLI、architecture.json、GLOSSARY/FACT、主 specs。
- **禁止事项**：不把改名/搬包/删符号自动记作领域净减；不按导出数量误杀合法读面；不让检查器替代独立评审。
