# 任务

## D1 消费循环骨架（红锚先行）

- [ ] 红锚：慢工具期间用户补充被回答（当前 dispatch 阻塞，预期红）
- [ ] 红锚：同会话不同来源的输入进入同一次模型请求（当前按 invocation 分隔，预期红）
- [ ] 新建 `runtime/agent/consumer.go`：每会话一个消费 goroutine，pull → dispatch → model → 回到 pull
- [ ] 消费循环拉取面：从事实链读「上次 agent_output 之后」的事件，按事件类型分派
- [ ] 停止面：context 取消即停止；重启后从链读出恢复位置

## D2 入口收敛

- [ ] `Host.Invoke`：删 `acceptInflight`（受理判定）与 `driveLocked` 的 gate hold/release；改为提交事实 → 唤醒消费循环 → 等待 agent_output
- [ ] `InjectEvent`：改为提交事实 → 唤醒消费循环（不启动 Drive）
- [ ] `fireNoticeTurn`：改为 CommitToSession → 唤醒消费循环
- [ ] 红锚：入口提交后消费循环接到事件并驱动模型

## D3 工具等待解除

- [ ] `dispatch`：派发全部调用后立即返回，删除逐个 `await`
- [ ] 工具结果以事件回到队列（已有的 Backflow 路径保持）
- [ ] 消费循环在下一次拉取时读到工具结果事件 → 组装上下文 → 调用模型
- [ ] 红锚：快结果先到先消费（不等慢结果）
- [ ] 红锚：慢任务期间新输入被回答（对齐 prototype TestAgentPipelineAsyncDispatch）

## D4 A/B 闭合与段边界

- [ ] 模型最终回复（无工具调用）→ 立即 commit `agent_output` → 闭合段
- [ ] 投影段边界：由消费关系（本次回答实际消费了哪些输入）决定，而非链上位置
- [ ] 后续到达的输入（B）属于下一段；不被已闭合段误标
- [ ] 红锚：A 期间 B 落链 → A 回答闭合 → B 在下一段被消费

## D5 删除 gate / Occupancy 收敛

- [ ] 删除 `addressGate` 结构（token + inflight + hold/release/running）
- [ ] 删除 `Host.gates` map
- [ ] 冥想判据②：改读消费循环运行状态（该会话是否有模型请求在执行）
- [ ] 查询面（`Sites`/`Inventory`）：读消费循环状态
- [ ] 红锚：并发两请求同 session → 第二个返回受理收据，消费循环合并处理

## D6 压缩三段式接线（已完成 L1+L2a）

- [ ] `AgeFoldPlugin` 改为只调 `PlanByAge` 并提交 `compress.request`
- [ ] 新增 `CompressionConsumer`：收到 request → `ApplyProducts` → 提交 `compress.product`
- [ ] `seedReplay` 读 product 重建合成条目，删除 `restoreFoldForm` 重折
- [ ] 红锚：重启后投影含 product 合成条目且不重折

## D7 预算事件化

- [ ] `FinalRoundPlanner`（PostCommit 观察者）按 policy 提交 `final_round.request`
- [ ] 消费循环在模型调用前读本会话的 request → 读到则为收尾轮（四红线不变）
- [ ] 红锚：预算耗尽后下一次模型调用为收尾轮且链上有一条 request

## D8 迁移与验收

- [ ] smoke/event-integrity 场景迁移（消费循环行为、compress 事实、final_round 事实）
- [ ] 文档：README「消费循环与事件驱动」节、event-flow 消费段、GLOSSARY
- [ ] 门：根全量 + lint + race + event-short 十轮 + event-long

## 已完成（前序批次）

- [x] L1 事件契约：`compress.product` + `final_round.request`（提交 29735dfc）
- [x] L2a 判定/执行拆分：`PlanByAge` / `ApplyProducts`（提交 3d668df8）
