## 1. TagentAgent — 持久 Loop 实现

- [x] 1.1 在 `agent/tagent_agent.go` 的 TagentAgent struct 新增字段：`mailbox chan model.Message`、`outputCh chan *event.Event`、`loopCtx context.Context`、`loopCancel context.CancelFunc`、`loopActive atomic.Bool`、`loopWg sync.WaitGroup`
- [x] 1.2 实现 `StartLoop(userID, sessionID string) (<-chan *event.Event, error)`：如果 loopActive 已为 true 则返回现有 outputCh；否则创建 mailbox（cap=256）、outputCh（cap=100）、loopCtx，设置 loopActive=true，启动 loop goroutine，返回 outputCh
- [x] 1.3 实现 loop goroutine：`for { batch := drainMailbox(); msg := mergeBatch(batch); eventCh, err := runner.Run(loopCtx, userID, sessionID, msg); for evt := range eventCh { outputCh <- evt }; if loopCtx.Err() != nil { close(outputCh); return } }`
- [x] 1.4 实现 drainMailbox：阻塞等第一个 + non-blocking drain 剩余
- [x] 1.5 实现 mergeBatch：单消息直接返回；多消息提取 Content 用 `"\n\n---\n\n"` 连接，Role 设为 RoleUser
- [x] 1.6 实现 `StopLoop()`：如果 loopActive 为 false 则直接返回；否则调用 loopCancel()，等待 loop goroutine 退出（sync.WaitGroup），设置 loopActive=false

## 2. InjectMessage 双模式 + Close 修改

- [x] 2.1 修改 `InjectMessage(msg model.Message)`：开头添加 `if ta.loopActive.Load() { ta.mailbox <- msg; return }`；现有逻辑（runner.Run + drain goroutine）保留在 else 路径中
- [x] 2.2 修改 `Close()`：在关闭 closers 之前添加 `if ta.loopActive.Load() { ta.StopLoop() }`
- [x] 2.3 添加 `sync/atomic` + `strings` import

## 3. 验证

- [x] 3.1 验证 `go build ./...` 编译通过
- [x] 3.2 验证 `go vet ./...` 无警告
- [x] 3.3 编写 `agent/tagent_agent_loop_test.go`：测试 StartLoop → InjectMessage → outputCh 收到事件 → InjectMessage（第二批）→ outputCh 收到第二批事件 → StopLoop → outputCh 关闭
- [x] 3.4 测试 InjectMessage one-shot 模式（loopActive=false）行为不变

## 4. 文档

- [x] 4.1 更新 `docs/wiki/agent/agent-architecture.md`：新增 Persistent Event Loop 章节（StartLoop/StopLoop、mailbox drain、Loop of Runner.Run、InjectMessage 双模式）
- [x] 4.2 更新 `README.md` Architecture 部分：补充持久 Loop 描述
