## 1. isFinalResponse role 修复

- [x] 1.1 修改 `agent/context_manager.go` 的 `isFinalResponse`,增加 `Role=assistant` 判断(工具结果 `Role=tool` 不再被误判为最终响应)
- [x] 1.2 运行 `go build ./agent/` 确认编译通过

## 2. Run() 重写为直调 RunFlow

- [x] 2.1 删除 `agent/session.go` Run() 中初始消息的 `invBus.Publish`,改为将 `message` 直接传给 `RunFlow`
- [x] 2.2 用单个 goroutine 直调 `invCM.RunFlow(ctx, message)` 替换 runEventLoop goroutine;goroutine 内 `defer close(invOutputCh)` + `defer invCM.Close()` + `defer ta.restorePersistentBus()`,并在调用前 `invCM.SetTriggerSource("user")`
- [x] 2.3 删除 wrapper 探测 goroutine(`if ToolCalls==0` 停止块、500ms drain 定时器、`runCancel` 强制取消),`Run()` 直接返回 `invOutputCh`
- [x] 2.4 删除 session.go 中已失效的 `asyncTaskCheckers` 相关注释(第 116-117 行 EXCEPTION 注释)
- [x] 2.5 保留 invBus 创建与 `setActiveBus(invBus)`(供 BeforeModel TryPull 使用、隔离 activeBus);确认 `time` import 若不再使用则移除
- [x] 2.6 运行 `go build ./agent/ && go vet ./agent/` 确认编译与 vet 通过

## 3. 确定性测试验证

- [x] 3.1 运行 `TestSubAgentRun_ToolResultStopsPrematurely`(瞬时 mock)应通过
- [x] 3.2 运行 `TestSubAgentRun_SlowLLM_ToolResultStops`(1.5s 延迟 mock)应通过,证明修复后慢 LLM 也能执行到第 2 轮
- [x] 3.3 更新/精简 `agent/session_subagent_toolstop_test.go` 中因 Run() 重写而失效的事件探测相关断言(若有),保留核心的 "tool calls 完整执行 + 最终响应" 断言(核查:两测试断言均仍有效,无需改动)

## 4. 回归与端到端验证

- [x] 4.1 运行 `go test ./agent/ -count=1`(全量回归,重点确认 persistent-event-loop 相关测试未受 isFinalResponse 修复影响)
- [x] 4.2 运行 `go test ./tests/ -count=1 -short`(集成测试非 LLM 部分)
- [x] 4.3 真实 LLM 端到端:`TRPC_CLAW_MODEL_NAME=glm-5.2 go test -v -run TestPlanAgentCreateBehavior_RealPrompt ./tests/ -timeout 180s`,确认 plan create 执行到 `openspec new change` 且写入 tasks.md
- [x] 4.4 真实 LLM 回归:`TRPC_CLAW_MODEL_NAME=glm-5.2 go test -v -run TestPlanAgentBug_AgentToolWrapper_SubAgentRun ./tests/ -timeout 180s` 仍通过

## 5. 收尾

- [x] 5.1 `openspec validate subagent-runflow-unification --strict` 通过
- [x] 5.2 按 commit 规范提交(scope: agent),说明根因与统一方案
