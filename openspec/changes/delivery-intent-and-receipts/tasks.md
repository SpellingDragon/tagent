# Tasks

## 1. K3 结算血统落盘（框架，最小先行）

- [x] 1.1 fail-before 双红：① `buildBusFact` 对 SourceTask+自带血统的产出断言一级 `settle_trigger_source`（现码无该键，红）；② 空 `cm.triggerSource` 时断言一级 `trigger_source` absent（现码落空串，红）；快照保留断言防回归（探针实测已入 fail-before.log）
      —— 双红实测在案（PromotesSettleLineage: Not equal；OmitsEmptyTurnLineage: Should be false）
- [x] 1.2 `MetaKeySettleTriggerSource` 常量入 `event/metadata.go` 单点定义；`buildBusFact`：SourceTask 分支补拷一级键（快照不动）；回合级 `trigger_source` 空则不写（同 `buildEventAttributes` 保护形态）
- [x] 1.3 测例转绿；`go test ./agent/... ./event/... ./memory/... -count=1` 全绿

## 2. P1 注入声明（框架 + poller）

- [ ] 2.1 fail-before：`POST /task` 带 `trigger_source:"user"` 的注入事件断言 `Metadata[trigger_source]=="user"` 且 `Source=="http"`、`extractTriggerSource` 判 `user`——现码必红
- [ ] 2.2 `taskRequest` 增可选 `trigger_source`；`validateTaskRequest` 三重校验（值域仅 `"user"`；无 auth 拒 `declaration_requires_auth`；缺省不变）；`handlePostTask` 把声明传入 `InjectEnvelope` 落事件 Metadata
- [ ] 2.3 mail-poller POST 体加 `"trigger_source":"user"`；无声明/非法值/无 auth 三条负例测例
- [ ] 2.4 `go test ./rl/... ./agent/... -count=1` 全绿

## 3. K2 统一回执（宿主分发层）

- [ ] 3.1 fail-before：构造冥想血统+`[task settled]` 特征的最终事件过分发层，断言出现 `delivery_receipt` 注入——现码必红
- [ ] 3.2 分发层七终态矩阵落地（见 design D3 分级表）：send 失败 ERROR 回执；L404 未知血统/冥想交付特征/error/无目标 WARN 回执；sent-ok 与冥想纯叙事不回执；回执正文含原因/血统/截断/目标
- [ ] 3.3 防自激豁免分支（`delivery_receipt` 血统输出消化不再回执）；回执走 `InjectMessageWithSource` 持久总线
- [ ] 3.4 七终态 × 有/无/级别断言全绿；`[user, receipt]` 混批一票否决测例；回执轮二次输出零回执测例

## 4. 收口

- [ ] 4.1 全量：`go test ./... -short -count=1`；`-race`（根+agent+rl 包）；`bash scripts/lint.sh`；`openspec validate --strict`
- [ ] 4.2 push 并确认 CI 四 job 绿；回信彼方（裁决案 2：P1 形态 + "http" 硬编码定谳 + B 的 WARN 部分并入 K2 + 换装邀请）
- [ ] 4.3 彼方换装后以邮件轮真跑声明路径（dogfood S2），回执观察入档
