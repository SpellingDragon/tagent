## 1. EventBus.TryPull（react-bus-integration）

- [x] 1.1 在 `agent/event_bus.go` 中新增 `TryPull() []*AgentEvent` 方法：非阻塞批量读取所有 pending 事件

## 2. InjectBusInputs BeforeModel 回调（react-bus-integration）

- [x] 2.1 在 `agent/context_manager.go` 的 `NewContextManager` 中，在 InjectEventKeys 回调之前注册 `InjectBusInputs` 回调：TryPull EventBus → 过滤非用户消息 → 追加到 args.Request.Messages
- [x] 2.2 验证：注入的消息在 InjectEventKeys 回调之后获得 event_key 前缀（通过注册顺序保证）

## 3. 验证与测试

- [x] 3.1 运行 `go build ./...` 确保编译通过
- [x] 3.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
- [x] 3.3 编写测试：验证 TryPull 非阻塞读取、批量读取、空返回（3 个测试通过）
## 1. EventBus.TryPull（react-bus-integration）

- [ ] 1.1 在 `agent/event_bus.go` 中新增 `TryPull() []*AgentEvent` 方法：非阻塞批量读取所有 pending 事件

## 2. InjectBusInputs BeforeModel 回调（react-bus-integration）

- [ ] 2.1 在 `agent/context_manager.go` 的 `NewContextManager` 中，在 InjectEventKeys 回调之前注册 `InjectBusInputs` 回调：TryPull EventBus → 过滤非用户消息 → 追加到 args.Request.Messages
- [ ] 2.2 验证：注入的消息在 InjectEventKeys 回调之后获得 event_key 前缀

## 3. 验证与测试

- [ ] 3.1 运行 `go build ./...` 确保编译通过
- [ ] 3.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
- [ ] 3.3 编写测试：验证 RunFlow 期间 InjectMessage 的新消息在 BeforeModel 中被 TryPull 并注入到 LLM 请求
