# 任务

## L1 事件契约

- [x] 新增 `api/event/compress_product.go`（字段、内容寻址 ID、Fact 实现、单测）——`29735dfc`
- [x] 新增 `api/event/final_round_request.go`（字段、内容寻址 ID、Fact 实现、单测）——`29735dfc`
- [x] `api/event` 注册两个类型；FACT-VOCABULARY 增两行——实现形态：`Fact` 接口（`FactKey`/`FactType`）在类型文件内实现，`types.go` 无需改动（`29735dfc`）

## L2 压缩三段式

- [x] `memory/replay`：把「判定」与「执行」拆开（`PlanByAge` 只报应折段与 request；`ApplyProducts` 保留执行，供 consumer 复用）——`3d668df8`；`FoldByAge` 兼容保留
- [ ] `assembly`: `AgeFoldPlanner` 只提交 request；新增 `CompressionConsumer` 读 request 执行并提交 product
- [ ] 投影：product 事实到达时产出合成条目（键/正文/覆盖区间）；`renderProjection` 不变
- [ ] 恢复：`seedReplay` 读 product 重建合成条目，删除 `restoreFoldForm` 重折路径
- [ ] 红锚：重启后投影含 product 合成条目且不重折；smoke 断言恢复严格（键相等）

## L3 预算耗尽事件化

- [ ] `assembly`: `FinalRoundPlanner` 按 policy 评估并提交 `final_round.request`
- [ ] `runtime/agent`: 决策边界读本 range 的 request → `closingRound` 以 request 为准（四红线不变）；无 request 时本地快判并补提交
- [ ] 红锚：预算耗尽后下一轮为收尾轮且链上有一条 request；未接 planner 时行为不变

## L4 迁移与验收

- [ ] smoke/event-integrity 场景随契约迁移（compress 事实、final_round 事实）
- [ ] 文档：event-flow 收尾轮段、FACT-VOCABULARY、README 一句
- [ ] 门：根全量 + lint + event-short 十轮 + event-long
