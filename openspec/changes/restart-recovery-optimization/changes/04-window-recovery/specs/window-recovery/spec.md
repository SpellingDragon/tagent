# window-recovery Specification

## ADDED Requirements

### Requirement: 窗口界与策略分开

系统的窗口类上限 SHALL 只有 `max_tokens` 一个外部输入；其余窗口类上限 MUST 由机制派生或降为不可配常量。压缩时机与保留深度是策略，由 `compress_threshold` 与 `keep_recent_tasks` 可调；压缩策略本身 MUST NOT 因本变更改变。任何新增的窗口类可配项 SHALL 判红，除非登记为策略并说明理由。

#### Scenario: 新增上限类可配项即红

- **WHEN** 代码或配置面新增一个未登记的窗口类上限
- **THEN** 界登记门判红并指出该上限名

#### Scenario: 压缩策略不变

- **WHEN** 本变更完成后对同一输入序列回放
- **THEN** 折叠的段、产物与保留深度与变更前逐条一致

### Requirement: recall 与窗口解耦

召回与查询 SHALL 只读落盘的事件数据，MUST NOT 依赖 runtime 窗口内容；观察表 SHALL 作为可丢弃的查询缓存，启动时 MUST NOT 全量装载；语义召回 MUST NOT 在内存保留 runtime 窗口的向量状态或全史向量表，查询时只对本次候选集计算向量。

#### Scenario: 启动零装载

- **WHEN** 进程以任意长度的历史启动
- **THEN** 观察表装载条数为 0，向量提交数为 0，同一查询的结果与全量装载时逐条一致

### Requirement: 窗口内恢复

重启恢复的投影重建 SHALL 只读窗口内的段并挂链上已有压缩产物，窗口起点由产物覆盖边界与最近未闭合段算得；一次恢复消费的事件数 MUST NOT 与历史事件数成正比。产物或边界不可得时 SHALL 退回全量重建并具名告警，结果 MUST 与窗口路径逐条一致。

#### Scenario: 窗口重建与全量一致

- **WHEN** 历史 1000 条事实、窗口覆盖其中 200 条，进程重启
- **THEN** 恢复只读窗口内的 200 条，重建投影与「全量折叠后裁窗」逐条一致
