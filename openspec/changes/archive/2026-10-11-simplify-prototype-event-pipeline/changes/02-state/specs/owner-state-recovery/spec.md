## ADDED Requirements
### Requirement: 架构治理与归属现场
state SHALL 以agent id关联队列、context、memory、任务和父子定义。治理仅在新增/替换/删除定义时调用，普通工具执行不走此门。
#### Scenario: 文件热更
- **WHEN** 相同id加载新配置并换执行壳
- **THEN** 原现场保留且旧任务晚到结果进入原所属队列；治理拒绝时定义保持旧值
### Requirement: 跨进程恢复与孤儿回收
快照 SHALL 保存可序列化现场；新进程加载后根据配置中的agent id恢复所有者，找不到所有者的任务与状态回收。运行中任务恢复unknown而非伪造完成或自动重跑。
#### Scenario: 恢复现场
- **WHEN** 子进程保存快照后退出，另一个进程加载快照和新配置
- **THEN** 存在owner的待处理事件/上下文/任务/记忆可继续消费，无owner的记录不再可路由
