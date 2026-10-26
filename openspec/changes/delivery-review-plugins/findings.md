# 1.0 findings — 投递 hold → 批准补投 → 恰一条

## 结论

麻雀闭环已红转绿：审核插件以 `Delivery{Content, Lineage, Target}` 裁决投递；发送插件在 `deliverFresh` 的每一条 Send 前查询裁决；`hold` 只挂起不投，批准后恰一条补投，拒绝零投递并留无载荷审计事实。默认无 ruler = allow，既有用户路径行为不变。

## 红证（先于实现）

- 命令：`go test ./tests/integration/host/ -run '^TestDeliveryReviewHold$' -count=1 -v`
- 证据：`evidence/1.0-red.txt`
- 现场：仅声明 `Delivery`/`DeliveryRuler`/`WithDeliveryRuler`，`deliverFresh` 未查询裁决。测试注入 hold 后仍收到正文：
  - `delivery_review_test.go:54: delivered bodies = ["the body the delivery review holds"], want none while the review holds the body`
  - `delivery_review_test.go:112: held deliveries = [], want the one body the review held`

## 实现要点

- `api/plugin/delivery.go`：`Delivery`（Content/Lineage/Target）与 `DeliveryRuler.Rule(ctx, Delivery) (Disposition, error)`；三态沿用 `DispositionAllow/Record/Hold`，hold 是等待。
- `host.go`：
  - `WithDeliveryRuler(ruler)` 注入；nil ruler 直接投递（D5 默认 allow）。
  - `deliverFresh` 的正文与失败通知都经 `deliverOne`：allow/record 投；hold 调 `holdDelivery` 挂起。
  - 血统取 `InvokeRequest.TriggerSource`（复用 trigger_source）；空串按用户回合记为 `user`。
  - 挂起表 `pendingDeliveries` 为内存表（持久化留 TODO），身份为 host 内唯一的 `delivery-%06d`；`Host.mu` 保护。
  - `HeldDeliveries()` 返回挂起条目副本（内容+血统+目标+ID+挂起时刻），按身份排序。
  - `ApproveDelivery(ctx,id)`：消费挂起后补投一次；重复批准返回 `ErrDeliveryNotFound` 且不再投；通道拒绝发送时把挂起放回，供再次批准。
  - `RefuseDelivery(ctx,id)`：消费挂起、零投递，并提交一条 `TypeFeedback` 审计事实：`trigger_source=host`、`event_summary=delivery refused`、`meta_delivery_id/meta_lineage/meta_target_agent/meta_target_session/meta_content_bytes`，`Message` 为空（无模型载荷）。
  - 裁决返回错误或未知 disposition：挂起（fail closed），不投未经裁决的正文。
  - 批准补投不再查询 ruler：hold 已是该投递的终局裁决，重复裁决会让“批准后补投”可能再次挂起。

## 批准 / 拒绝语义

- 批准 = 消费一条挂起并把原正文原样补投到原目标，恰好一次；不是重新生成，也不是重新裁决。
- 拒绝 = 消费一条挂起并丢弃正文；零投递，审计事实保留投递语义（身份/血统/目标/字节数）但不保留正文。
- 两种决定都只消费一次：重复决定按身份拒绝（`ErrDeliveryNotFound`）。

## 绿证

- `go test ./tests/integration/host/ -run '^TestDeliveryReviewHold$' -count=3` → `ok github.com/SpellingDragon/tagent/tests/integration/host 0.812s`
- 测试覆盖：
  - hold 后用户渠道 0 条；ruler 收到恰一条语义（正文+`user` 血统+显式目标）；挂起条目存在且内容/血统/目标一致。
  - 批准后恰一条补投（同正文），挂起清空；重复批准返回 `ErrDeliveryNotFound` 且仍只有一条。
  - 拒绝后 0 条、挂起清空、审计事实存在且无 `Message` 载荷，元数据可追溯；重复拒绝按身份拒绝。

## 回归

- `go test ./tests/integration/host/ ./runtime/agent/ -count=1` → 两包 ok。
- `go -C examples/wechat-bot test . -run '^TestLocalSmokeGuards$|^TestLocalSmokeSymptom' -count=1 -tags smoke` → ok。
- `go build ./...`、`go vet ./` → 通过。
- `bash scripts/lint.sh` → `lint: ok`。

## 关于既有审批通道的复用判断

既有 governance 审批通道（`runtime/task/approval.ApprovalManager` + `runtime/task.HoldBook`）的对象是工具调用：身份由工具名+参数摘要绑定，批准后的执行者是任务执行路径。投递语义没有工具名与参数摘要，且批准后的动作是“把正文补投到目标”，无法由既有通道直接完成；把正文塞进 args digest/preview 会伪造工具调用语义。故 1.0 采用 host 内的最小挂起表，1.1/1.2 再把投递裁决接入 governance 的规则面。

## 偏离与后续

- `run.go` 增加一个 `assemblyConfig.deliveryRuler` 载字段（`HostOption` 的唯一落点），无行为默认。
- 概念预算门要求新增导出接口配 canonical example：新增 `examples/contract/api_plugin/DeliveryRuler_example.go`；`scripts/gen_godoc.sh` 重生成 `docs/api` 三页。
- 挂起表不持久化（TODO）；进程重启会丢未决投递。
- `DispositionRecord` 当前只投递、不写投递审计账；投递账本属 1.1 的 delivery-plugin 能力。
