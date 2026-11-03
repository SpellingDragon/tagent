# 领域契约技术决策

## D1 形状与语义分门

S1–S4 继续约束接口/结构形状；domain-surface 单独回答新增公共面是否经过裁定。任一失败都阻止合并，productVerified 永不因静态门而设为真。

## D2 紧凑绑定而非新台账

在现有 architecture.json 增加 concept_bindings：symbol → canonical concept → kind(fact/read/declaration/port) → 主 spec 锚。只检查相对基线新增/改名的公共命名面，方法和普通函数不逐个登记；base/目标缺失或解析失败 fail-closed。

## D3 概念净额按权威而非符号

同名搬迁不算新增；rename 用 renamed_from 对账且净额为 0；新读面绑定既有事实不增加领域概念；只有最后一个事实/规范权威删除才算领域删除。Writers/Movement/Continuations 等机制应内化或删除，不提供机制绑定捷径。

## D4 规范单主

consumer-loop-convergence 拥有地址消费；async-task-execution 拥有关闭编排；unified-task-semantics 拥有通知状态定义；compression-pipeline 拥有 request/product。persistent-event-loop 与其他旧条款先迁移再退出，禁止 superseded tombstone 继续带 SHALL。逐条迁移账本见 tasks 4.2：旧循环、mailbox、durable inbox、turnEcho 等机制条款随实现退役撤销；Handoff 路由、正式宿主缺端口、投递前审核三条仍有现行公共行为锚但当前主 spec 无文本归口，列为遗留交编排者按主 spec 单写收口，不得回填历史文件。

## D5 词汇职责

GLOSSARY 只解释领域关系，不写 goroutine、函数签名、身份 hash 或缓存窗口。DeliveryLedger 是投递事实读投影；segment 是 Consumer 的内部执行态，不作为第二回合词；Turn 是不可变结果；Attention 是审批/通知的在场读面。

## D6 保留合法事实

CompressProduct 与 FinalRoundRequest 有稳定身份、具名消费者和恢复语义，可作为事实保留；其执行器、Planner、报告和 DTO 不自动升级为领域概念。白名单是概念归属约束，不是禁止所有新事实。

## 风险与回退

- 绑定门变成官僚主义 → 只检查新增/改名公共命名面，复用现有数据/检查器；合法 read/port 有明确正例。
- persistent 残条误删 → 逐条映射到现行 spec 与公共行为锚后才删除，不能整文件盲删。
- 既有事实目录债不全由本 PR 引入 → 本变更先修受影响面，继承债单列，不伪称全仓清零。
