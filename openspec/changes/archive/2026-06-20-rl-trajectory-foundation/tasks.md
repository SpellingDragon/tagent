## 1. Phase 1: Trajectory 数据层

- [x] 1.1 新建 `agent/trajectory.go`：定义 `Trajectory`、`InputMessage`、`Interaction`、`ToolCallRec` 结构体（全保真 JSON 序列化）
- [x] 1.2 实现 `TrajectoryCollector`：`NewTrajectoryCollector(batchIndex, userID, sessionID)`、`RecordInput(msgs []model.Message)`、`RecordEvent(eventIndex int, evt *event.Event)`（捕获 Response.ID 作为 completion_id、tool calls、tool results、token usage、TTFT、final response、error）、`RecordError(err error)`、`Finalize(status string) *Trajectory`
- [x] 1.3 实现 `TrajectoryStore`：`NewTrajectoryStore(exportPath string)`、`Add(t *Trajectory)`、`Get(id string) (*Trajectory, bool)`、`List() []*Trajectory`、`ExportJSONL() error`；内存存储 + 最大 1000 条 FIFO 淘汰
- [x] 1.4 编写 `agent/trajectory_test.go`：测试 collector 采集逻辑（tool call 事件、tool result 事件、final response 事件、error 事件）、store CRUD + FIFO 淘汰、JSONL 导出（19 个测试，全部通过）
- [x] 1.5 验证 `go build ./...` + `go vet ./agent/...` + `go test ./agent/...` 通过

## 2. Phase 1 集成: loop() 中嵌入 TrajectoryCollector

- [x] 2.1 修改 `agent/tagent_agent.go` 的 `TagentConfig`：新增 `TrajectoryStore *TrajectoryStore` 字段（可选）
- [x] 2.2 修改 `TagentAgent` struct：新增 `trajectoryStore *TrajectoryStore` 字段
- [x] 2.3 修改 `NewTagentAgent`：从 cfg 初始化 trajectoryStore
- [x] 2.4 修改 `loop()` 函数：在 batch 开始时创建 `TrajectoryCollector`；在事件转发循环中调用 `collector.RecordEvent()`；在 batch 结束时调用 `collector.Finalize()` 并存入 store（如果 store 非 nil）
- [x] 2.5 验证 `go build ./...` + `go vet ./agent/...` + `go test ./agent/...` 通过（现有测试不受影响）

## 3. Phase 2: Reward 接口

- [x] 3.1 新建 `agent/reward.go`：定义 `RewardFunc` 接口（`Compute(trajectory *Trajectory) (float64, error)`）
- [x] 3.2 实现 `TaskCompletionReward`：有 final response = 1.0，否则 0.0
- [x] 3.3 实现 `ToolCallEfficiencyReward`：`max(0, 1 - toolCallCount/maxToolCalls)`，超过 MaxToolCalls = 0.0
- [x] 3.4 实现 `HTTPCallbackReward`：POST trajectory JSON 到 endpoint，解析响应中的 `{"reward": float64}`
- [x] 3.5 编写 `agent/reward_test.go`：测试三种 reward 函数的计算逻辑 + 边界情况（14 个测试，全部通过）
- [x] 3.6 验证 `go build ./...` + `go vet ./agent/...` + `go test ./agent/...` 通过

## 4. Phase 2 集成: loop() 中调用 RewardFunc

- [x] 4.1 修改 `agent/tagent_agent.go` 的 `TagentConfig`：新增 `RewardFunc RewardFunc` 字段（可选）
- [x] 4.2 修改 `loop()` 函数：batch 完成后，如果 cfg.RewardFunc 非 nil，调用 `rewardFunc.Compute(trajectory)`，将 reward 写入 `trajectory.Reward`，记录到 span 属性 `tagent.batch.reward` + 日志
- [x] 4.3 验证 `go build ./...` + `go vet ./agent/...` + `go test ./agent/...` 通过

## 5. Phase 3: tagent HTTP API

- [x] 5.1 新建 `agent/http_api.go`：定义 `HTTPAPI` struct + `NewHTTPAPI(agent, store)` + `ServeHTTP(w, r)`
- [x] 5.2 实现 `POST /task`：解析 body `{"messages": [...], "user_id": "...", "session_id": "..."}`，通过 `InjectMessage` 提交到 loop，返回 `{"trajectory_id": "..."}`（trajectory_id 在 batch 开始时分配）
- [x] 5.3 实现 `GET /trajectory/{id}`：从 TrajectoryStore 查询，返回完整 Trajectory JSON
- [x] 5.4 实现 `GET /trajectories`：返回 trajectory 摘要列表
- [x] 5.5 实现 `GET /healthz`：返回 `{"status": "ok", "loop_active": bool}`
- [x] 5.6 编写 `agent/http_api_test.go`：测试各端点 + 错误处理（13 个测试，全部通过，含端到端 PostTask_Success）
- [x] 5.7 验证 `go build ./...` + `go vet ./agent/...` + `go test ./agent/...` 通过

## 6. Phase 3: AReaL Python Adapter

- [x] 6.1 新建 `areal/tagent_adapter.py`：实现 `TagentARealAdapter` 类，`async def run(self, data, **extra_kwargs) -> float | dict[str, float]`
- [x] 6.2 实现任务提交：POST /task 发送 data["messages"]，获取 trajectory_id
- [x] 6.3 实现结果轮询：GET /trajectories 轮询直到 status = completed/error，间隔 100ms
- [x] 6.4 实现 reward 计算：如果 trajectory.Reward 非 nil → 直接使用；否则调用 Python 侧 reward_fn（如果有）；否则返回 0.0
- [x] 6.5 实现 reward 映射：如果 trajectory.CompletionIDs 非空，返回 `{completion_id: reward}`；否则返回 float
- [x] 6.6 新建 `areal/README.md`：集成配置示例（AReaL config + tagent 启动命令 + adapter 用法）

## 7. 文档

- [x] 7.1 更新 `docs/wiki/agent/agent-architecture.md`：新增 §7.4 "RL Trajectory 数据层"（Trajectory 结构、Collector 采集流程、Store 存储、与 OTLP span 的关系）
- [x] 7.2 更新 `docs/wiki/agent/agent-architecture.md`：新增 §7.5 "AReaL 集成"（架构图、HTTP API、Python adapter、端到端流程）
- [x] 7.3 更新 `README.md`：Architecture 部分补充 RL trajectory + AReaL bridge 描述

## 8. 端到端验证

- [x] 8.1 验证 `go build ./...` + `go vet ./...` + `go test ./...` 全项目通过
- [x] 8.2 验证 trajectory 采集：启动 tagent + loop → InjectMessage → 检查 TrajectoryStore 中的 trajectory 数据完整性（含 completion_id、tool calls、token usage、TTFT、final response）— TestHTTPAPI_PostTask_Success 覆盖
- [x] 8.3 验证 reward 计算：配置 TaskCompletionReward → 检查 trajectory.Reward 正确 — TestHTTPAPI_PostTask_Success 覆盖（reward=1.0）
- [x] 8.4 验证 HTTP API：curl 各端点 → 检查响应格式正确 — 13 个 HTTPAPI 测试覆盖
