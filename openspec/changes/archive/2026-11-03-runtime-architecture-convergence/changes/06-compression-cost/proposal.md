# 压缩容量语义与成本收敛

父变更：runtime-architecture-convergence。W0-A06 的 Q1–Q6 已由编排者采纳，C1 独立列为关键正确性缺陷。

## Why

工作树用 Material 作为第二容量门，既新增公共读面，又会在总容量超阈时抑制真实可折候选；每次提交还增加分段与渲染扫描。与此同时，基线允许 request 提交失败后仍由内存消费者生成 product，破坏 request→product 的事实因果。

## What Changes

- request 未持久化时立即返回错误，禁止进入消费者或生成无链上前因的 product。
- 恢复 Volume 为唯一容量信号；只有容量超阈且确有新候选产品时才执行和写压缩事实。
- 删除未发布 Material 导出字段及 threshold<=1 的隐式 forced 特例；恢复容量内提前返回。
- 不可折投影可超过折叠阈值但不产生空事实；实际模型请求仍由既有预算装配约束。
- 4096 暂不作为永久长程 shard；保留一次性 benchmark，只有独特症状、规格和校准齐备后才可加入。
- request/product 为唯一现行压缩管线；legacy 条款与 04 域共同退出。

## 边界与依赖

- **依赖**：无核心 01–03 实现依赖；C1 可独立修复。4096 调度依赖 05，legacy spec 清理依赖 04。
- **被依赖方**：01 的答案链上关联不得被折叠隐藏；05 使用本域基准作性能证据。
- **接口面**：公共 PlanByAge/FoldByAge/ApplyProducts、AgeFoldPlugin.ObserveCommit、正式 Boot/Host 模型请求。
- **禁止事项**：不以 CI 总时长证明压缩正确；不新增 Material 阈值、缓存状态或第三套压缩管线；不覆盖既有工作树，修改必须基于当前 hunk 重定基。
