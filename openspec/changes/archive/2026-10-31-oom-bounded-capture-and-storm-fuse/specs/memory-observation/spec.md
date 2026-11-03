# memory-observation

## ADDED Requirements

### Requirement: 运行时内存观测面

host face SHALL 提供 `/debug/pprof/{heap,goroutine,allocs}`（仅 localhost 绑定）；`TAGENT_GCTRACE=1` SHALL 透传 GODEBUG=gctrace=1。作为 S4（堆棘轮残差）钉死与长期内存回归的证据面。

#### Scenario: heap 端点可用
- **WHEN** host Boot 后请求 /debug/pprof/heap
- **THEN** 返回有效 pprof 采样（非 404/空）

#### Scenario: gctrace 开关
- **WHEN** 以 GODEBUG=gctrace=1 启动（或在启动器导出 TAGENT_GCTRACE=1，Boot 将其合并进 GODEBUG 供本进程与子进程继承）
- **THEN** 进程 stderr 出现 gctrace 行；对已启动进程库内 Setenv 不生效（实测边界），Boot 打一行边界提示
