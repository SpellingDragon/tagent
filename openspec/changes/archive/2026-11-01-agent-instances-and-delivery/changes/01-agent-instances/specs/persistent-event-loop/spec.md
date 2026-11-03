# persistent-event-loop Specification（delta：循环面向每 agent 地址）

> 本文件为 MODIFIED delta。"Loop goroutine 持续运行不退出"条款的装配范围从入口扩为每 agent 地址；drain→merge→turn 的循环体语义不变。

## ADDED Requirements

### Requirement: 每 agent 地址的持久事件循环

每个 agent 地址 SHALL 挂持久事件循环（drain mailbox → 合并为一条模型消息 → 执行一个 turn → 回到 drain），仅在显式 StopLoop/run 关闭时退出。直驱式"一次调用一个回合"的形态 MUST NOT 与持久循环并存为第二套驱动路径。

#### Scenario: 循环跨越多次事件

- **WHEN** 三次事件先后到达同一非入口地址
- **THEN** 同一循环进程内连续处理，无第二次实例装配

#### Scenario: 关闭即退出

- **WHEN** run 关闭
- **THEN** 全部地址循环退出并回收，无泄漏 goroutine
