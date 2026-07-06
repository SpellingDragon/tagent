## 1. outputCh 阻塞写入（output-ch-blocking-write）

- [x] 1.1 修改 `ContextManager.RunFlow` 中 outputCh 写入逻辑：所有事件阻塞写入（`select` + `ctx.Done()`），移除 `default` 丢弃分支

## 2. 持续消费模式（continuous-event-consumption）

- [x] 2.1 修改 `examples/wechat-bot/main.go`：将 `consumeUntilFinal` 改为持续消费 goroutine + 按事件类型分发
- [x] 2.2 持续消费者收到 agent_output 时：有等待中的用户消息 → 回复用户；无等待 → 记录日志
- [x] 2.3 持续消费者收到非 final 事件时：日志记录 + 继续打字指示

## 3. 冥想 prompt 重写

- [x] 3.1 重写 `examples/wechat-bot/resources/prompts/meditation.md`：渐进式流程（获取→判断→分析→行动），增加信息充分性判断指导
- [x] 3.2 在 `examples/wechat-bot/resources/prompts/recall_agent.md` 中增加信息充分性判断段落
- [x] 3.3 重写 `examples/wechat-bot/resources/prompts/recall_tool_desc.md` 去除重复内容
- [x] 3.4 在 `examples/wechat-bot/resources/prompts/AGENTS.md` 中增加 Tool Call Discipline 约束

## 4. 文档更新

- [x] 4.1 更新 `README.md` 场景一（持久事件循环）：反映持续消费模式和按事件类型分发
- [x] 4.2 更新 `README.md` "各模块视角"：增加 outputCh 消费者视角

## 5. 验证与测试

- [x] 5.1 运行 `go build ./...` 确保编译通过
- [x] 5.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
- [x] 5.3 验证 outputCh 阻塞写入：通过现有测试验证（阻塞写入不丢弃事件，消费者持续消费）
