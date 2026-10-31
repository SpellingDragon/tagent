# 任务

## D1 消费循环骨架（红锚先行）

- [x] 红锚：慢工具期间用户补充被回答（TestSlowToolDoesNotBlockNewInput 转绿，2026-10-30）
- [x] 红锚：同会话不同来源的输入进入同一次模型请求（TestCrossSourceBatching 转绿，断言对齐 spec「允许进入」措辞：同请求含两输入）
- [x] 新建 `runtime/agent/consumer.go`：每会话一个消费 goroutine，pull → dispatch → model → 回到 pull
- [x] 消费循环拉取面：从事实链读「上次 agent_output 之后」的事件，按事件类型分派（cursor+lastOutputOf 恢复位置）
- [x] 停止面：context 取消即停止；重启后从链读出恢复位置（lastOutputOf 定位最后 agent_output）

## D2 入口收敛

- [x] `Host.Invoke`：user 地址改为提交事实 → 唤醒消费循环 → 等待闭合段（invokeConsumed）；旧 gate 路径保留为无 consumer 宿主兑底
- [x] `InjectEvent`：经 fireNoticeTurn 消费分支改为提交事实 → 唤醒消费循环（不启动 Drive）
- [x] `fireNoticeTurn`：有消费循环时 CommitToSession → 唤醒 + 等段闭合 + settle 记账（W1）
- [x] 红锚：入口提交后消费循环接到事件并驱动模型（TestCrossSourceBatching/W1 六红收敛全绿）

## D3 工具等待解除

- [x] `dispatch`：派发全部调用后立即返回，删除逐个 `await`（awaitSettled 下沉为直驱路径专用）
- [x] 工具结果以事件回到队列（Backflow → intake settled → 消费循环 harvest）
- [x] 消费循环在下一次拉取时读到工具结果事件 → 组装上下文 → 调用模型
- [x] 红锚：快结果先到先消费（不等慢结果）——TestFastResultDrivesBeforeSlowSettles（W5b：慢调用 park 期间模型服务到读快结果的请求恰一次，链上有快事实无慢事实；-count=3 绿）
- [x] 红锚：慢任务期间新输入被回答（TestSlowToolDoesNotBlockNewInput）

## D4 A/B 闭合与段边界

- [x] 模型最终回复（无工具调用）→ 立即 commit `agent_output` → 闭合段（closeSegment）
- [x] 投影段边界：由消费关系决定（recordMapped 改 rangeOpen：agent_output 关段才推进 ended 边界）
- [x] 后续到达的输入（B）属于下一段；不被已闭合段误标（W1 改写后断言覆盖）
- [x] 红锚：A 期间 B 落链 → A 回答闭合 → B 在下一段被消费（W1）

## D5 gate / Occupancy 收敛

- [x] `addressGate` 收敛为 headless token 槽（inflight/hold/release/running 已删；user 路径的 acceptInflight 兑底与 gate hold/release 一并删除；token 保留为 headless 直驱互斥，不属收敛面）
- [x] `Host.gates` 保留为 headless 按地址 token 槽（随上一条）
- [x] 冥想判据②：改读消费循环运行状态（该会话是否有模型请求在执行）——W5b：驱动读面改 `modelInFlight(addr, instance)`，host 挂 `run.ModelInFlight`（Loop.InFlightModel，覆盖消费循环与直驱两路）
- [x] 查询面（`Sites`/`Inventory`）：读消费循环状态——W5b：`runReading.Sites` 改读 `Run.ModelInFlight`，Occupancy 零生产读点（写面保留单飞互斥）
- [x] 红锚：并发两请求同 session → 消费循环合并处理（TestCrossSourceBatching：同请求含两输入；受理收据快路径经裁定不设——等待闭合段优于收据，spec「或返回受理收据」为许可非要求）

## D6 压缩三段式接线（已完成 L1+L2a）

- [x] `AgeFoldPlugin` 改为只调 `PlanByAge` 并提交 `compress.request`
- [x] 新增 `CompressionConsumer`：收到 request → `ApplyProducts` → 提交 `compress.product`
- [x] `seedReplay` 读 product 重建合成条目，删除 `restoreFoldForm` 重折
- [x] 红锚：重启后投影含 product 合成条目且不重折

## D7 预算事件化

- [x] `FinalRoundPlanner`（PostCommit 观察者）按 policy 提交 `final_round.request`（W4：FinalRoundDue 同源判定，幂等，无界零事实，变异实测因果）
- [x] 消费循环在模型调用前读本会话的 request → 读到则为收尾轮（四红线不变）——归 W5b（`closingRoundOf`：按段 range 读事实，读不到回落本地判定）
- [x] 红锚：预算耗尽后下一次模型调用为收尾轮且链上有一条 request（W4：TestFinalRoundRequestLandsOnTheDrivenRange 等 4 测试 8 用例）

## D8 迁移与验收

- [x] smoke/event-integrity 场景迁移（消费循环行为、compress 事实、final_round 事实）——主仓四包 event-integrity 全绿 + W9：examples/wechat-bot -tags smoke 域段语义迁移（10 项归因处置、-count=3 全绿、红锚 0 真回归）
- [x] 文档：README「消费循环与事件驱动」节、event-flow 消费段、GLOSSARY——W7：三处落盘，13 处具名符号逐个 grep 实证，doc budget 具名落锁（22032→22061）
- [ ] 门：根全量 ✅ + lint ✅（comment_policy 0，余 tracked-hygiene 待 git 收口）+ race ✅ + event-short 十轮 ✅（passed=10/10 verdict=match，run_id 92d534e4 尾轮）+ event-long 六分片后台执行中

## 已完成（前序批次）

- [x] L1 事件契约：`compress.product` + `final_round.request`（提交 29735dfc）
- [x] L2a 判定/执行拆分：`PlanByAge` / `ApplyProducts`（提交 3d668df8）
