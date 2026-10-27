# 编排账目（只编排）

> **只编排**：验收条款在 design D6 与 ORCHESTRATION.md；**启动门已满足**（fidelity-and-recovery 归档 `3c2cf89`）。
> **派发单元=批**（D0/D4：根包闭包相交，批内串行、批间串行；并发轴=实施×验证×后台门禁三角色）。
> **每批三问**：删了什么（diff）· 责任归谁（报告）· 验收仍成立（锚复跑+定点破坏）。
> **负锚=勾选硬门**（R29）：测试不存在或未被证据点名=不得勾选；验收面只对账不缩水。

## 跟踪图

```mermaid
graph LR
  G[启动门: fidelity 已归档 3c2cf89] --> B1[D1 批1 统一提交/编码/历史]
  B1 --> B2[D2 批2 责任: 2.1核验→2.2快照→2.3分层]
  B2 --> B3[D3 批3 词汇/呈现/锚核验]
  B3 --> F[F1–F4]
  classDef crit fill:#f88,stroke:#900,color:#fff
  class B1 crit
```

## 批 1（统一正确性分支）——派发单 D1（根包簇：facts.go/tools.go/age_fold.go/memory/event_body.go/run.go/request.go）

- [x] 1.1 (a) 提交单入口：删 facts.go:57 `commitFact`，tools.go:111、age_fold.go:219 改走 `commitFactChecked`，age_fold 提交失败补观测（返回/记录，不静默）；单入口语义入 Godoc —— 验证：`go test ./tests/integration/host -run 'TestFactFidelity' -count=1`（正锚）+ `go test ./tests/integration/host -run 'TestFoldCommitFailureSurfaces' -count=1`（负锚·新建：注入拒写 tier→fold 提交失败可观测）+ `go build ./...` 与 `go test ./tests/integration/host -run 'RequestFidelity' -count=1`（消费者锚）—— 补勾（波1-A1，2026-10-27）：全部锚 RC=0；**裁定①**：tier 注入不可构造（fold 事实无 Owner.AgentID 在 persistFact 早退）→ 负锚改公共入口两类真实拒写（无事件时间/同键异内容），具名与正则不变
- [x] 1.2 (c) EventBody 索引派生+legacy 删除：删 Summary/Content 存储字段（Record/Event 改派生自 Meta[summary]/Message.Content），删 DecodeEventBody legacy 分支与 legacyBodyKey，旧数据不迁移冷启动重建（D3.2 用户裁定）—— 验证：`go test ./memory/... -short -count=1` + `go test ./tests/integration/host -run 'TestFactFidelity' -count=1`（正锚）+ `go test ./memory -run 'TestDecodeEventBodyLegacyRefused' -count=1`（负锚·新建：legacy event_key 字节→ErrEventBody）+ `TAGENT_FIDELITY_COLD_BOOT=1 go test ./tests/integration/host -run 'RequestFidelity' -count=1`（消费者锚·冷启动正式读回）—— 补勾（波1-B，2026-10-27）：memory 37 PASS、锚全 RC=0、负锚变异-复原红绿双证；**裁定②**：根包 5 处字段引用（facts×3/read×2，read.go 原无归属=计划缺口）由编排者适配为派生读；混旧代 body 的 state 冷启动具名硬失败（D3.2 推论，登记 findings）
- [x] 1.3 (b) 历史路径统一：删 facts.go `r.history` map 与 CommitHistory 映射门（654–681）、HistoryFace 语义迁移（SessionHistory 读投影/单路径恢复）、删 AttachReplay 注册门（无窗口也注册，fold 用缺省窗口，预算只影响呈现）—— 验证：`go test ./tests/integration/host -run 'TestHistoryRestoresWithoutBudget' -count=1`（正锚·G7 锚不动）+ `go test ./runtime/agent -short -count=1`（HistoryFace 契约）+ `go test ./tests/integration/host -run 'TestSingleHistoryPath' -count=1`（负锚·新建：同会话有/无预算恢复历史逐消息相等）+ 结构断言 `grep -c 'history\[' facts.go`=0 + 冷启动复跑（消费者锚）—— 补勾（波1-A1，2026-10-27）：结构断言 2→0、全锚与全仓 short 38 包 RC=0；**裁定③**：第二面在 8919acf 已不可达（映射门先在、注册门已无）——本叶为删死代码，红转绿由结构断言承载，行为锚=不回归；SessionHistory 单面空/CommitHistory no-op，host 既有锚一字未动；ageFold 挂载门保留（G7 钉）；HistoryFace 文档补 mapped-sequence 例外

## 批 2（统一责任）——派发单 D2（2.1 核验先行 → 2.2 → 2.3）

- [x] 2.1 (d) 核验+关闭（重定基线：旧状态已由 3.2+F3b 删）：TestSettleWakeSingleDrive 三子测复跑 3 遍 + 结构证据 + findings 登记「跨会话串行 pump=已知边界，并行化另行立案」—— 验证：`go test ./tests/integration/host -run 'TestSettleWakeSingleDrive' -count=3`（正锚+消费者锚·真子进程 SIGKILL+重启）+ `grep -cE 'retained|firing|rearm' settle_turn.go`=0（负锚·结构）—— 补勾（集成-1，2026-10-27）：`-count=3` RC=0；结构 grep=0（C1 注释措辞 retained→owed 保断言）；pump 串行边界登记 findings
- [x] 2.2 (e) 快照单源：删 run.go:200 `snapshots` 与 request.go snapshotRequest 双保存，快照只采集一次入 Capture（rl.CaptureAdapter.Snapshot），RequestSnapshots/callArgumentsOf 改读 Capture（SnapshotRefs/SnapshotOf），Seal 不动（D3.1），decision-capture 主规格条款核对（D3.7，失真走 MODIFIED 登记）—— 验证：`go test ./rl -short -count=1` + `go test ./tests/integration/host -run 'RequestFidelity' -count=1`（正锚·Seal 二封契约+协议）+ `go test ./rl -run 'TestRequestSnapshotsSingleSource' -count=1`（负锚·新建：读出面与 Capture 同源）+ `grep -c 'snapshots' run.go`=0 + `go test ./runtime/agent -short -count=1`（消费者锚·loop sealCapture 生产消费）—— 补勾（波2-A2+A2b+集成-2，2026-10-27）：全部锚 RC=0；**裁定⑤（P1）**：capture 关闭时读面由「有」变「空」=接受方案 A（快照=capture 材料，删无条件双保存正是本叶本义；formal 三夹具补挂 WithCapture，断言一字未改）；**裁定⑥（P2）**：VisibleSkills 经 `agent.BoundaryView.VisibleSkills`（唯一新增导出字段，A2b 越清单一处 request_assembly.go 追认——避免 loop 侧第三份命名规则）传入 CaptureAdapter；死面 RequestSnapshotFace 族全仓零引用删除（grep=0，loop.go 净 −42）；负锚位置 rl→根包（rl 不能 import 根包，原命令空转=计划缺陷修正，命令 `go test . -run 'TestRequestSnapshotsSingleSource'`）；LeaseObserver 裁 Observer-only（无自有持久账户，边界登记 findings）；decision-capture 条款核对=仍真（快照固定点未动）
- [x] 2.3 ① Observer 责任分层：注册分类 Observer-only/Driver（marker 或注册面声明），Observer-only 失败=记缺口不阻断、Driver 失败=义务保留可重试（mailbox 账既有），分层语义入 Godoc 红线 —— 验证：`go test ./api/plugin -run 'TestObserverLayering' -count=1`（负锚·新建：Observer-only 失败→流程继续+缺口可查；Driver 失败→义务保留，只记日志=红）+ `go test ./tests/integration/host -run 'TestSettleWakeSingleDrive' -count=1` + `go test ./telemetry ./rl -short -count=1`（正锚+消费者锚·真实 Driver=SettleDriver、真实 Observer=telemetry/rl）—— 补勾（波1-C1，2026-10-27）：4 子测 PASS+双变异红绿、消费者面全 RC=0；**裁定④**：`After` 无 error 返回（钉住）→ driver 失败以 gap+sentinel `ErrObservationObligationRetained` 落读面，mailbox 义务路径未动；机制未导出（S4 无新债）；settle_turn.go 增量 10 行；LeaseObserver 归属→A2（已裁 Observer-only）

## 批 3（降抽象成本）——派发单 D3（api/plugin → memory/replay/request.go；3.2 前置=2.2）

- [x] 3.1 (f) 删 Phase 保 Port：删 Phase 类型与 RouteOf（api/plugin），observePhase 门控/日志键改 Port（present.go/phases.go），examples/contract api_plugin 面同步 —— 验证：`go test ./api/plugin -short -count=1` + `go build ./...`（正锚）+ 结构断言：`grep -rn 'plugin.Phase\|RouteOf' --include='*.go'` 非测试引用=0（负锚）+ `go test ./evolution/... -short -count=1`（消费者锚·meditation 经 Port）—— 补勾（波2-C2+集成-2，2026-10-27）：正锚 66 PASS、结构断言全仓（含测试与 examples）=0、消费者锚 evolution 三包 RC=0；闭表校验迁未导出 `validatePort`（三条具名拒绝文案逐字保留，M3b 变异证 NewFlow 公共入口承载闭表不缩水）；日志键 phase=→port=（信息等量实测样例）；导出面 −2（Phase/RouteOf 删）；C2 上报的三处跨白名单（evolution/governance/policy_test 删 6 行、host log_observation 断言迁移、wiki/EXTENSIONS 去相位改写+docs/api 重生成）由编排者落地；相位测试等价迁移逐条登记（TestRouteOfClosedTable→TestPortClosedTable、TestPhaseObservation→TestFaceObservation）
- [x] 3.2 (g) 删呈现决策+Content 双角色拆解：删 decisions 持久复用与 recordLocked 原地改写，呈现 viewOf 现算（事实+配置+预算），"过去看到什么"由请求快照（2.2 读回）承担，noticeCard 预算下限修复（room<0）—— 验证：`go test ./memory/replay -run 'TestRequestBudgetEnforced' -count=1`（正锚·G9 锚不动）+ `go test ./memory/replay -run 'TestPresentationRecomputedOnBudgetShrink|TestNoRecordedPresentationReuse' -count=1`（负锚·新建：预算缩→重裁；两次装配预算不同→呈现不同）+ `go test ./tests/integration/host -run 'RequestFidelity' -count=1`（消费者锚·三场景协议合法）—— 补勾（波3-A3+集成-3，2026-10-27）：G9 断言零字节改动（numstat 184/0）PASS；三负锚（扩 noticeCard 为 TestNoticeCardKeepsItsBound）基线树红→新树绿 8 PASS；settle 家族整体删除（反事实甄别：0 生产消费者、无事件族注册、折叠恢复 fold.go 天然不读决策——G7 三子测复跑绿）；导出面 −10、净 −241 行；**裁定⑦（跨白名单迁移）**：history/acceptance 两测试去 Decision 查表（二次装配语义改「同预算现算同一视图」+观测键 presentation_full_text_iff_decision_full→…iff_full_room，golden scenarios.json 同步重命名）；主规格 event-sourced-projection 两处直写「视图现算」+MODIFIED delta 入 change（归档零 diff）；FACT-VOCABULARY 族 6 删行+投影账本段重写、GLOSSARY 结算词条改任务结算语义（编号留空位不重排）
- [ ] 3.3 四不变量锚全量核验：D5 四锚全量复跑 + 本批定点破坏红证（四不变量各取一修复点：还原→红→还原三重→绿）—— 验证：D5 命令四条全绿 + 破坏红证入 evidence/

## F（一级直管）

- [ ] F1 契约面断言：`git diff 3c2cf89..HEAD -- openspec/specs/` 仅含本变更声明的增量（orchestration 收尾增量+可能登记的 MODIFIED）；`git diff 3c2cf89..HEAD -- golden/runtime/event_integrity_scenarios.json` 为空或逐行登记等价迁移；decision-capture 条款复核
- [ ] F2 全量门禁（逐批已跑，收尾总复跑）：lint（`bash scripts/lint.sh`）/ selftests（`python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'`）/ check-openspec（`bash scripts/gates/check-openspec.sh`）/ 三模块 `go test ./... -short -count=1` / short 十轮（`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10 --out <dir>`）/ long 六片（`--scope event-long --seed .. --window .. --repeat 1 --timeout 1200` 六组合）/ mutations（`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out <dir>`）/ 症状 job（`go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -timeout 600s`）——逐条原始退出码入 evidence/F2-gates.txt
- [ ] F3 DoD + 回滚即红：四不变量定点破坏双证（3.3 已备）+ 每批三问记录归档核对 —— 证据 evidence/F3-redgreen.txt
- [ ] F4 归档 + 导览（域→证据链接，提交号逐字符核对）

## 粒度统计

3 批 9 叶 + F4；批=派发单元（D0 闭包实测：根包簇合单，批内串行）；负锚新建 7 个具名测试（1.1/1.2/1.3/2.2/2.3/3.2×2）+ 2.1/3.1/2.2 结构断言 3 处。
