## ADDED Requirements

### Requirement: 任务统一语义

工具、子 agent 与结果投递 SHALL 同为 owner 任务表管理的任务：投递经 Spawn 执行，成败以 ToolResult 通知事件回所属管线，消费循环不因投递失败终止。任务与管线的映射 SHALL 单点由 state 持有，任务通知一律经所属 AgentState 投递。

#### Scenario: 投递失败不杀循环
- **WHEN** Deliver 闭包返回错误
- **THEN** 循环继续消费，failed 通知进入上下文并驱动模型，模型可据通知收敛作答

#### Scenario: 管线关系单点
- **WHEN** 任务完成发出通知
- **THEN** 通知经所属 AgentState.Publish 投递，TaskManager 不持有第二份管线映射

### Requirement: 中间输出为内部输入事件

带工具调用时的模型输出与推理 SHALL 作为 EventModelDelta 回送本 agent 事件管线消费（进上下文投影，不驱动模型、不外投）；Reply SHALL 仅在无工具调用时发布。

#### Scenario: 中间叙述可见但不收口
- **WHEN** 模型同时返回 ToolCalls 与 Output
- **THEN** Output 以 EventModelDelta 进上下文供后续轮次可见，不进 Deliver；子 agent 以无工具调用的 Reply 作最终回报

### Requirement: 可选聚合窗不控制粒度

任务完成通知 SHALL 支持按条数聚合（窗口默认 0 即发，攒满批量投递，关闭时 flush 残余）；模型触发粒度 SHALL 保持 pull 自然聚合，窗口不等待输入、不试图控制整体节奏。

#### Scenario: 窗口攒批
- **WHEN** TaskWindow=2 且两个任务先后完成
- **THEN** 两条通知合并投递，一次消费批次同时可见；窗口为 0 时逐条即发

### Requirement: 演进命名与治理插件分工

架构变更管道 SHALL 命名为 Evolution（Eval→Benchmark→ApplyGate，语义不变）；运行时危险调用 SHALL 由治理插件在事件管线拦截——把命中调用变换为 denied ToolResult 事件，工具零执行、模型读到拒绝事实。

#### Scenario: 拦截即变换
- **WHEN** 模型调用命中 DenylistPlugin 名单的工具
- **THEN** 该工具函数零执行，模型上下文出现 denied 结果并据以继续
