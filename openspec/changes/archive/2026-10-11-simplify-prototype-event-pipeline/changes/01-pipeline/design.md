## Decisions
以 ../../design.md D4/D5 的确切签名为准。只写 `.qoder/staging/prototype-reset/01-pipeline/agent.go` 与 `task_manager.go`，package prototype，不重复contracts类型。
核心不持queue/context/tasks，只引用AgentState；停止壳不取消owner任务。任务context由TaskManager持有，Close用于孤儿回收及宿主关闭。每次完成发所属owner事件，running/unknown只发notice，不等待另一任务。
AgentTask通过NewAgent与同一循环执行子agent，以函数返回作为任务结果；不新增子执行引擎。每文件简短中文头注释。
验证分层：域内gofmt检查语法；汇合后主代理编译和集成测试。无独立单测、无代码评审代理。
