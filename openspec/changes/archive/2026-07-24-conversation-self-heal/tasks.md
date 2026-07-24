## 0. Spike — 确认双投影来源（先于开发）

- [x] 0.1 给 `projection.Append` 加一行"命中重复 key 即 warn（含 role/type/来源线索）"的观测（最小改动），验证重复的触发条件与频率
- [x] 0.2 据观测判定：是否需在 onEvent 上游堵双投（若确为上游双调用），还是 L1 入库幂等已足够；记录结论到 design.md

## 1. L1 — 投影按 EventKey 幂等

- [x] 1.1 `SessionProjection` 维护已见 `EventKey` 集合；`Append` 对 `EventKey>0` 已存在者跳过并 warn；`EventKey==0` 不去重
- [x] 1.2 `Replace`（压缩）后重建已见集合，保证与替换后投影一致
- [x] 1.3 单测：重复 key 跳过、key==0 不合并、Replace 往返后幂等仍生效、并发 Append 安全（-race）

## 2. L2 — 发送前 tool 配对校验 + 保守修复

- [x] 2.1 新增 `agent/message_validate.go`：`validateToolPairing(msgs) []issue`（孤立 tool、重复 tool_call_id）+ `repairToolPairing(msgs) msgs`（删重复、孤立补占位/成对移除），纯函数
- [x] 2.2 作为 BeforeModel 链**最后一步**注册（SmartCompressor 之后），对 `args.Request.Messages` 校验；畸形则保守修复并 warn；**不回写 projection**
- [x] 2.3 在 `context_manager.go` 注册处显式保证顺序（压缩后）+ 注释说明依赖
- [x] 2.4 单测：合法序列不改动；重复 tool_call_id 去重；孤立 tool 补占位；修复仅作用于入参、不触碰持久投影

## 3. L3 — 错误分类与针对性重试（已撤销）

> 实现期核实推翻了前提：`RunFlow` 会 swallow 模型错误（`runner.Run` 返回事件流，4xx 作为携带 `Response.Error` 的事件流经 `outputCh`，`RunFlow` 返回 `nil`），故 event_loop 的重试对模型错误**根本不触发**——不存在"盲重试放大"。为保持架构纯粹简单，L3 移出本 change；诚实透出真实错误由消费端（wechat-bot main.go）单独处理。

## 4. 验证与收尾

- [x] 4.1 回归：`go test ./agent/ -short -count=1`（含 -race 关键项）全绿；`go build ./...` 通过
- [x] 4.2 端到端佐证：构造含重复 tool 事件的消息序列 → `repairToolPairing` 去重/去孤立后配对合法（不再 400）；投影层 `Append` 幂等去重（含并发）
- [x] 4.3 `scripts/check-openspec.sh` 通过；`openspec validate conversation-self-heal --strict` 通过；按 Conventional Commits 提交
