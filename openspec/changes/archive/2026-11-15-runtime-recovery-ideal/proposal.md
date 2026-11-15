# 运行时恢复理想态（runtime-recovery-ideal）

## Why

用户原话：「目前 WAL 的 journal.log 会无限膨胀」。考古与严肃反思后，问题不止物理膨胀——**之前的实现没有完全兑现设计预期**（用户裁定：「需要严肃反思」）。

教义（`59feb537`）：「one fact chain as the single truth, with **bounded derived state** and **one g(x) derivation path**」；session-checkpoint-material-unification/design.md:9：「链是真相，checkpoint 是派生态的载体而非第二真相；恢复＝读最近载体＋对尾部重跑同一个 g(x)，不另立派生实现」。

对照 spec SHALL 与代码（证据见 design §三），真实违背：

1. **任务恢复全史读**：`DurableCommit.load`（runtime/state/durable.go:305）在 checkpoint 恢复之前无条件全扫五前缀、解码全部任务正文；`restoreChains` 覆盖跳过只省 fold 不省 load。违 checkpoint-state/spec.md:43「boot 任务恢复成本 SHALL 不随历史任务总数线性增长」。12.4b 残留栏书面自认「body 已由 durable load 全量读入，非本单可消」。
2. **journal 开档全读 + 索引全量物化**：journal.go:490 走整份文件、order 每帧每键永不回收；segment_store.go:281 Load 建全量 positions map。只有正文读有界，开档 I/O + 索引 + 常驻内存仍 O(全史)。违 checkpoint-state/spec.md:15/:23、window-recovery/spec.md:75。
3. **governance 未托管**：DenialLedger/GoalRegistry 双全扫（denial.go:304/327）但生产零调用；生产只挂 approval book（ApprovalManager.rebuild 目录扫）。教义「全部可恢复状态 checkpoint 托管」未覆盖。
4. **验收锚量错面**：checkpoint_test.go:1231「boot 平坦」只断言 CommittedFacts()（正文计数），不量 journal 字节/索引规模/durable load/墙钟；夹具单会话无任务。违背 1-2 全在锚外，「全绿归档」掩盖未达成。
5. **主 spec 与代码反向**：~16 主 spec / 628 SHALL / 181 legacy 条款（task-skeleton-compression:51/:74 禁 LLM vs summary_gist.go:57 LLM gist；compression-recovery-ticket:8 仍 SHALL 已退役 ticket；等）；golden durable_truth.json:300 键形 prose 漂移。

**不是违背（接受边界，须分清）**：journal 物理厚度无界本身是教义意图（链全量真相、恒重放、spec.md:44「任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实」）。cadence 三触发已全接线、B3/B4 有界投影已达成（先前误报，已澄清）。

## What

用户终裁：**把所有已知问题修干净，让机制达架构理想预期状态，同时清理废弃所有无效机制和代码**；不考虑开发成本，全修到干净终点、一笔做完。物理有界取向用户裁定**乙**（教义兼容扩展）。

- **D1 任务托管收编**：DurableCommit.load 改读 tasks checkpoint 切片（扩携 consumed/attempts/rejected），时序移到 checkpoint 恢复之后；消除全史扫旁路，兑现单一 g(x)。
- **D2 有界开档/索引**：引擎层**自持持久索引切片**＋尾增量，不每次全量重建（D6.8：不从 checkpoint 派生引擎索引——checkpoint 自身是 journal 事实，会循环依赖）；开档 I/O / 索引规模 / 常驻内存有界于存活键集＋尾。
- **D3 governance 托管**：governance 状态收编为 Stateful 切片（尾 seq + 审批态）并接线生产；或裁定 approval book 为权威并收编其 rebuild（design D3 裁定）。
- **D4 验收真量**：boot 锚改量 journal 字节 / 索引条目 / durable load 计数 / 墙钟（非正文计数代理量）；多代＋多任务夹具；负锚（全史读回归→红）。
- **D5 契约同步 + 死码清理**：~16 主 spec 转正/退役 legacy 条款；golden 同步；删死生产者 ReplayProjection.Fold（fold.go:49）/ RebuildProjectionFromWAL（projection.go:342）；六 Material 兼容读端口审计后退役。
- **D6 退位分层（乙）**：已结事实搬迁到冷层、**仍按身份可读**（spec.md:44「恢复期读全史的消费者 SHALL 仍能按身份读到其正文」），热 journal 有界。接线 lifecycle/retention/guard 脚手架服务于退位（搬迁/遮蔽，非删除，合 memory-architecture.md:50 红线「只能搬迁、遮蔽或删除可见性，不得改写正文与身份」）。

**核心不变量**：链是唯一真相、一身份一不可变事实、SHALL NOT 删/改已提交事实；退位是搬迁非删除，逻辑链恒完整可按身份重放。

## Capabilities

- `checkpoint-state`（MODIFIED）：全部可恢复状态托管（task commit port + governance 收编），有界恢复真量。
- `bounded-recovery`（ADDED/MODIFIED）：开档/索引/load/墙钟有界于存活键集＋尾，验收真量。
- `fact-retirement`（ADDED）：已结事实退位分层，按身份可读，热 journal 有界，搬迁非删除。
- `storage-lifecycle-completion`（MODIFIED）：lifecycle/retention/guard 脚手架接线服务于退位。
- 契约同步：~16 主 spec legacy 条款退役/转正，golden 同步。

## 边界与依赖

- **依赖**：现有 Stateful 接缝（runtime/state/checkpoint.go:36）、checkpoint 事实族（api/event/checkpoint.go）、seedFact 隔离重建（run.go:2946）、折叠锚（compress_consumer.go）、AtomicStore 缝（atomic_store.go）、MigrateShardedRoot（migrate.go:76，退位可借「复制+校验」半段）。
- **被依赖方**：远端 wechat-bot 部署（journal 物理有界直接影响其磁盘）；所有 boot 路径消费者。
- **接口面**：Stateful 切片接口（StateName/SnapshotState/RestoreState）是托管统一接缝；退位须新增引擎能力（journal-only，AtomicStore 缝表达不了 swap，须新 capability，参 commitOrder 先例 segment_store.go:224）。
- **禁止事项**：
  - 不触碰外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）。
  - **SHALL NOT 删除/覆盖/改写已提交事实**（spec.md:44 红线）；退位只搬迁/遮蔽，按身份恒可读。
  - 不触 config-surface-normalization 未提交变更（同 dev 分支，独立 change，互不触碰）；其 F4 归档/commit/push 仍停用户授权。
  - 本文档阶段**只规划不实施**；实施待用户审过 design 后另行编排。