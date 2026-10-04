# Tasks

## 1. K3 结算血统落盘（框架，最小先行）

- [x] 1.1 fail-before 双红：① `buildBusFact` 对 SourceTask+自带血统的产出断言一级 `settle_trigger_source`（现码无该键，红）；② 空 `cm.triggerSource` 时断言一级 `trigger_source` absent（现码落空串，红）；快照保留断言防回归（探针实测已入 fail-before.log）
      —— 双红实测在案（PromotesSettleLineage: Not equal；OmitsEmptyTurnLineage: Should be false）
- [x] 1.2 `MetaKeySettleTriggerSource` 常量入 `event/metadata.go` 单点定义；`buildBusFact`：SourceTask 分支补拷一级键（快照不动）；回合级 `trigger_source` 空则不写（同 `buildEventAttributes` 保护形态）
- [x] 1.3 测例转绿；`go test ./agent/... ./event/... ./memory/... -count=1` 全绿

## 2. P1 注入声明（框架 + poller）

- [x] 2.1 fail-before：`POST /task` 带 `trigger_source:"user"` 的注入事件断言 `Metadata[trigger_source]=="user"` 且 `Source=="http"`、`extractTriggerSource` 判 `user`——现码必红
      —— 盖章行 FAILBEFORE 置空即红（accepted_and_stamped: Not equal），恢复即绿；声明链路为 rl→InjectEnvelope attrs→agent 盖章，`extractTriggerSource` 的 `firstLineage=="user"` 既有路径复用，既有血统仲裁测例不重复钉
- [x] 2.2 `taskRequest` 增可选 `trigger_source`；`validateTaskRequest` 三重校验（值域仅 `"user"`；无 auth 拒 `declaration_requires_auth`；缺省不变）；`handlePostTask` 把声明传入 `InjectEnvelope` 落事件 Metadata（缺信封能力时 501 拒收，不静默丢弃）
- [x] 2.3 mail-poller POST 体加 `"trigger_source":"user"`（仅携凭时声明，与服务端同规则）；无声明/非法值/无 auth/缺能力四条负例测例全绿
- [x] 2.4 `go test ./rl/... ./agent/... -count=1` 全绿（rl 0.36s / agent 40.7s）；根模块 build/vet 净

## 3. K2 统一回执（宿主分发层）

- [x] 3.1 fail-before：构造冥想血统+`[task settled]` 特征的最终事件过分发层，断言出现 `delivery_receipt` 注入——现码必红
      —— 分级特征判定 FAILBEFORE（`if false &&` 短路）即红：meditation_settle_speaks + FeedsBack 双双 FAIL，恢复即绿；bot 分发层无集成基座，main.go 接线以同形调用+模块编译+vet 核证，真链路终态由 4.3 dogfood 实测
- [x] 3.2 分发层七终态矩阵落地（见 design D3 分级表）：send 失败 ERROR 回执；L404 未知血统/冥想交付特征/error/无目标 WARN 回执；sent-ok 与冥想纯叙事不回执；回执正文含原因/血统/截断/目标（`delivery_receipt.go` 纯函数分级 + `receiptInjector` 窄接口）
- [x] 3.3 防自激豁免分支（`delivery_receipt` 血统输出消化不再回执）；回执走 `InjectMessageWithSource` 持久总线（新文件已入白名单 `.gitignore`，wiki 开锚 `#delivery-receipts` 升第六节）
- [x] 3.4 七终态 × 有/无/级别断言全绿；`[user, receipt]` 混批一票否决由 `extractTriggerSource` 既有 user 优先路径与既有仲裁测例承载，不重复钉；回执轮二次输出零回执测例绿；bot 模块 build/vet/test 全绿

## 4. 收口

- [x] 4.1 全量：`go test ./... -short -count=1`；`-race`（根+agent+rl 包）；`bash scripts/lint.sh`；`openspec validate --strict`
      —— SHORT=0 / RACE=0 / LINT=0（gofmt 一处 + `MetaKeySettleTriggerSource` 导出后 gen_godoc 重同步）/ strict valid；poller py_compile 通过
- [x] 4.2 push 并确认 CI 四 job 绿；回信彼方（裁决案 2：P1 形态 + "http" 硬编码定谳 + B 的 WARN 部分并入 K2 + 换装邀请）
      —— `ce6f3d2` push CI test/openspec/race/validators 全绿；裁决回信已投（含 K3 取证陷阱勘误），存 `.git/review-notes/12-mail-ruling-p1k2.md`
- [ ] 4.3 彼方换装后以邮件轮真跑声明路径（dogfood S2），回执观察入档
