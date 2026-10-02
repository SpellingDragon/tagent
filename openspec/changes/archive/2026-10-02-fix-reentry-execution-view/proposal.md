# Proposal: 重入的执行视图与目标解析同源

## Why

`execution-generations.md` §十 承诺「新一代改了该目标（仍被路由）：无发起者的重入经**新发布面**到达新目标」。实现只做到了"选哪个 wrapper"，没做到"在哪张执行视图上跑"：重入闭包把**属主面**的租约放进上下文后直连 `runAndCollect`，绕过 `Call` 里那段 arms 子代声明的动作，于是 `session.go` 的 `belongsToOwnerOf(child.cm)` 判定为 false，静默回落到**出生代配置**。

后果不是偶发失败而是**静默错误执行**：重入返回成功文案（「已重跑任务…」），实际跑的是已退役代的 prompt/model/tools；运维以为当前编排在生效。CI 上 `TestChangedTargetResolvesOnTheNewGeneration` 的 20s 超时正是这一缺陷的显形——被断言的"新面被服务"事件在三次失败日志中出现 0 次，用例本地能过是靠一个与重入无关的热更告警轮替它满足了条件。

## What Changes

- **产品侧**：把"arms 目标自己的声明代"从 `Call` 内部抽成单一入口，`Call`、`subagentRelaunchClosure`、`subagentResumeClosure` 三处共用。目标解析决定"哪个目标"，arms 决定"在哪张视图上跑"，两者同源于同一次解析结果。
- **测试侧**：修正断言归因（基线在重入**之前**采样、发布后先静默告警轮、setup 任务等到终态再发布），使 `SUB-C-G2` 的任何一次出现只能由重入产生；新增回归用例先证缺陷再证修复。

## Impact

- Affected specs: `config-hot-reload`（ADDED：重入的执行视图与目标解析同源）
- Affected code: `agent/tool_agent.go`（一处 helper + 两处闭包 + `Call` 改走 helper）、`cross_generation_test.go`、`delegation_test.go`/相关 fixture
- 不改租约生命周期模型（`exec_lease.go` 无改动）；属主面租约照旧由调用方持有一份引用
