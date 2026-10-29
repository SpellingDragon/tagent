# unified-intake 编排文档

## 一、结论（先行）

- **抵达 DoD 的唯一路径**：A1 红锚 →（A2+A3 合单）loop 增量→装配注入 → A4 host 受理语义 → A5 规格 delta+锚迁移 →（B1→B2→B3 域 B）→ F。A1 必须先红：没有红锚，后面所有绿都无法证明症状被修。
- **当前最大风险**：A4 把 gate 从「消息门」改为「turn 发起权门」——它是唯一触达 public Invoke 语义的改动；若受理回执/ErrSlotHeld 语义漂移，host/tool 全部并发测试面会以看似无关的方式红。A4 必须单独一单、独自回归 `tests/integration/host` 全包。

## 二、编排四问

### 2.1 次序（派发原子=孙任务）

| 单 | 叶 | 前置 | 状态 | 触碰集 | 预估 |
|---|---|---|---|---|---|
| D1 | A1 | — | ready | `tests/integration/execution/unified_intake_test.go`（新） | 快 |
| D2 | A2+A3（合同） | A1 | blocked | `runtime/agent/{loop,invocation}.go`+`assembly/run.go`+同测试 | 中（核心） |
| D3 | A4 | D2 | blocked | `assembly/host.go`+同测试 | 中（最高风险） |
| D4 | A5 | D3 | blocked | `openspec/specs/persistent-event-loop/spec.md` 经 delta、`docs/wiki/agent/event-flow.md` | 快 |
| D5 | B1+B2（合同） | D3（席位面稳定） | blocked | `features/evolution/meditation/{driver,judgment}.go`+`tests/integration/evolution/meditation_activity_test.go`（新） | 中 |
| D6 | B3 | D5 | blocked | `openspec/specs/meditation-idle-gating` 经 delta、`docs/wiki/evolution/evolution-architecture.md` | 快 |
| D8 | E1–E4（03 域） | D3 | blocked | `assembly/request.go`（environmentBlock/段表）、`runtime/agent/{request_assembly,loop}.go`（BoundaryView 扩字段+末尾拼装）、`tests/integration/execution/unified_intake_test.go`、delta/wiki | 中 |
| D7 | F1–F4 | D4,D6,D8 | blocked | — | 收尾 |

合单判据：A2/A3 同触 loop 装配面（文件簇相交）故合单；B1/B2 同文件簇合单。拆单收益：无——域内均 <3 叶，不拆。

### 2.2 授权（每单白名单）

- D1：仅新测试文件+（如需）tests 内 fixture。禁碰生产代码。
- D2：`runtime/agent/loop.go`（每决策增量+退出判据）、`runtime/agent/skeleton_assert.go`（HistoryFace 三方法钉）、`assembly/facts.go`（界标不变量校准）、`examples/contract/runtime_agent/HistoryFace_example.go`（第 3 方法实现）、`tests/integration/execution/final_round_drain_test.go`（接口实现补方法）、同包测试。聚合复用既有 `observeReady`/`aggregateReady`/`SessionFacts`；不得改 decision inputs 落链语义。
- D3：`assembly/host.go`（Invoke/gate 段：在途记录+KindUnknown 受理，**除 meditation Driver 构造段 401-407**）、`assembly/host_face.go`（inject 段：KindUnknown 判已受理）、`assembly/run.go`（CommitToSession 内部落链段拆出；公面签名不变）、`examples/wechat-bot/wechat.go`（Drive 段：同判读+mutex 降级）+ 测试文件。ErrSlotHeld/ErrShutdownRefused/回执 "ok" 一字不变。
- D4/D6：仅规格文件+wiki 锚页（锚 slug 保全）。
- D5：`features/evolution/meditation/{driver,judgment,reflection}.go`（reflection.go 仅 Tick 内判据①段）、`assembly/host.go`（仅 meditation `NewDriver` 构造段 401-405，**按段与 D3 串行**）、4 个既有测试调用点（meditation_event_purity_test.go、evolution/event_integrity_test.go ×3）、新测试。闸零值=现行为。
- 预授权：单内 lint 自清（comment_policy 命令随令）；D4/D6 附带 `scripts/gates/doc_budget.json` 具名记账权（W6：wiki 锚页净增行按既有「先拒后具名落锁」惯例）；必须上报：任何需要动白名单外文件的连带（例如 loop 增加导出符号影响 api 面）。
- 共享面：`assembly/run.go` 全段归 D3（CommitToSession 落链段拆出）；D2 不需改 `run.go`（`*Run.SessionFacts` 既有同签名）；`assembly/host.go` 除 meditation Driver 构造段 401-405（D5）外归 D3。

### 2.3 凭据（五查落点）

- 每单必贴：目标测试 `-run` 逐字输出（正锚）、负锚测试输出、消费者锚（公共入口：Host.Invoke/Assemble/NewDriver 真构造）、`bash scripts/lint.sh` 尾行、`git diff --stat` 白名单核对。
- 必亲验数字：A1 红锚在基线上红（贴 FAIL 原文）；`go build ./...` 与 `bash scripts/lint.sh --only build` 在 D2/D3 后全绿（HistoryFace 第 3 函数的契约样例与集成测试同步）；D2 后 `go test ./tests/integration/{history,execution,learning} -count=1` 全绿（增量聚合对既有消费场景的扰动=本变更真未知数，回归面随 D2）；D3 后 `tests/integration/host` 全包 ok；`openspec validate --strict` exit 0；doc-budget 零差额。
- 每单单独提交；提交信息 `feat(intake):/feat(meditation):/test(anchor):` 前缀。

### 2.4 释放（锁与等待表）

- D1 完成即释放红锚证据 → 立即派 D2。
- D2 绿 → 派 D3（关键路径）；D3 绿 → **D4 与 D5 并行**（触碰集不相交：spec/wiki vs features）。
- D3 绿 → **D8 与 D4、D5 并行**（触碰集不相交：request/loop vs spec/wiki vs meditation）；D4/D5/D8 全绿 → D6 → D7。
- 无长跑单（全部 <10min），无后台资源锁。

## 三、明细层

- 派发粒度：孙任务级（合单后 6 单）。
- 预算：每单 ≤1 轮返工；D2/D3 任一超 2 轮 → 编排者接管诊断（R23 分类后拆叶）。
- 档位与真实路径欠账：无配置档位；真实路径=Host.Invoke/Assemble 公共入口（每单消费者锚已钉）。

## 四、台账层

| 轮 | 单 | 结果 | 凭据摘要 | 计划回写 |
|---|---|---|---|---|
| （待填） | | | | |
