# Proposal：热盘有界 + 归档可读（bounded-hot-log）

## Why

用户 2026-11-22 裁定：`journal.log` 磁盘无界不符合预期，目标取“**热盘有界＋归档可读**”（A 档）。现行构建把增长声明为已接受成本（`CHANGELOG.md` 2026-11-16、`bounded-recovery` spec），该口径撤销。

物理事实：append-only、无删除/覆盖的工作负载中，唯一事实集大小是磁盘量下界；绝对有界只能来自“移出热盘”。A 档要求在**物理层**达成有界：保留窗之外的事实封段迁出热卷；**逻辑层承诺不变**（事实身份、顺序、按身份可读、checkpoint/tail 语义均不因物理搬迁改变）。归档属可读存储的一部分，其缺失/损坏＝存储损坏（具名拒绝），不是“事实不存在”。归档介质自身的增长离线声明，不计入热盘预算。

## What Changes

- 形式化有界判据：热卷字节 ≤ `f(保留窗内工作量) + B_overhead`；稳态平台锚（多代不止升）；归档卷增长显式声明，不作有界判据（延续 `bounded-recovery` 口径）。
- 日志分段轮转与封段：热日志在 `cut`（与 checkpoint 提交同一事件）时封段，声明阈值只请求提前 cut；封段携带自描述索引（键→偏移）；封段整体迁入归档卷，manifest 原子提交后删除热段并收缩热索引。
- 定位读：按键查热索引，未中查归档定位索引，经归档契约读取；**归档缺失/损坏具名拒绝**，绝不答“不存在”。
- 配置面：`archive_root` 与保留窗阈值（字节/时间）为受校验声明字段；无归档配置＝现状行为（一个未封段），迁移零强制。
- 消费面：`recall_get`/`recall_trace`、历史查询、训练导出、全史回读按同一身份定位读路径工作。
- 验证吞吐（04 域）：Go 编译缓存接线（只加速不判定）＋race 分片（包集单点维护、并集保真、聚合 fail-closed）＋真 CI 前后读数；重复轮次/变异矩阵/双触发策略不动（证明面零收缩）。
- 契约修订：`session-checkpoint-material` 逻辑承诺原文保留（按身份可读）；新增**物理层归档契约**（归档属可读存储、丢失/损坏具名拒绝、manifest 唯一权威）；`bounded-recovery` 补物理层热卷有界要求。不再使用“修订 P1 措辞”。

不改变：事实不可变、一身份一事实、热段内按身份直读、恢复成本由 checkpoint 加偏移决定、旧机制不下线（无配置时行为与今日一致）。

## Capabilities

### New Capabilities

- `hot-log-bounds`：增长构成实测、有界判据形式化、稳态平台验收。
- `log-archival`：段轮转/封段索引/manifest 原子搬迁/热盘 GC/归档定位读与失败语义。
- `archival-read-paths`：recall、查询、导出、全史回读的跨档身份读一致性。

### Modified Capabilities

- `bounded-recovery`：补“热字节有界”要求（替换“显式声明增长”为“热卷带界、归档声明”）。

## Symptom Anchors

**S1（第一天红）**：写入 N 代（N≥3 且总量超阈）后，热卷字节在跨代间落到平台（±常数），而第 1 代老事实经 `recall_get`/按身份读**逐字节相等**仍可读。

**S2（候选，阶段 0 判定）**：归档段损坏或缺失 → 读老键具名拒绝（`ErrArchiveMissing`/`ErrArchiveCorrupt`），不答不存在、不静默丢召回；反证不成立则关闭该怀疑并保留反证。

## Impact

`memory/ledger`（轮转/封段/索引/原文族与热 GC）、`assembly`（装配配置与恢复读路径）、`features/rl` 导出、`session-query`/工具读面、`api/config`（两个声明字段）、`.github/workflows/ci.yml` 与 `scripts/gates/race_check.sh`（CI 吞吐）、`openspec/specs/{bounded-recovery,session-checkpoint-material}`、`docs/wiki/memory`、CI（有界锚 + 归档损坏负锚）。无外部服务依赖；归档卷介质与保留窗参数由部署声明（本仓库提供本地目录实现）。

## 前置与承接

- 依赖：runtime-coherence 的 `FactLog`/`pointReadLog` 窄口与恢复序（已落地）；本变更不改 runtime-coherence 的验收面。
- 与旧“退位/冷链”机制的区别：本变更**保留按身份可读**（搬而不删）、以 manifest 单一权威与具名失败为准，不复活已删除的 relocation/RetireFacts 形态。
