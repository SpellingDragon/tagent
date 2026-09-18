# 3.9 独立代码审查 — 咨询包（REVIEW PACKAGE）

> **定位**：本文件**服务**任务 3.9，**不完成** 3.9。3.9 要求「**独立** reviewer，自审不算」——作者（本会话）复核不计准出。这里梳理 diff 面、风险点、不变量、预验事实，最大化第二人审查效率。**3.9 checkbox 保持未勾，直至独立 reviewer 签署。**
>
> **⚠️ 范围教训（cold-eyes 2026-09-18 修正）**：初版误用 `git status` 界定 diff 面——只捕捉了**未提交的工作树尾部**（3 个小文件），漏掉本 change 已提交的实质核心。正确面是**分支累积 diff**。已按下述权威范围重写。

## 0. 权威审查范围（必须两条都覆盖，缺一即漏审）

本 change 首个提交是 `c14cdbc`（建 change 目录，6.7①/D1）；其父 `832f43e` 是继承的 WP0-WP4 耐久 inbox 基线（**属前一 change，不在本审查面内**）。

| 覆盖 | 命令 | 得到 |
|------|------|------|
| A. 已提交 + 已改（tracked） | `git diff 832f43e` | 23 文件，含核心逻辑 |
| B. 未跟踪新文件（多为测试） | `git ls-files --others --exclude-standard` | 11 新 `*_test.go` |
| **合并（推荐）** | `git add -A && git diff --cached 832f43e`（审后 `git reset`） | **39 文件 / +3817 / −110** |

> 只看 `git status` 会漏掉 22 个已提交文件（含最大的核心改动）；只看 `git diff <base>` 会漏掉 11 个未跟踪新测试。**两者并集才是完整审查面。**

## 1. 变更面分类（据上表权威范围，非工作树）

**核心生产码（行为承载，审查主体，~700+ 行）**：

| 文件 | ± | 内容 | 优先级 |
|------|---|------|--------|
| `agent/compress/context_compressor.go` | **+384** | `foldSettleRuns`（settle_fold 折叠）+ `guardCondensedCard`（浓缩票据反伪造）+ 预算诚实 + `curateCards` O(n²) 定因修复 | **最高** |
| `rl/endpoint_redirect.go` | **+74** | `EndpointRedirectPolicy(allowedHosts)`→`CheckRedirect` 逐跳 host 校验，空 allowlist 全拒（SSRF/token 外泄防护）| **高（安全）**|
| `agent/task/task_manager.go` + `agent/agent.go` | +58 / +12 | 批量退役源头汇总（`OnBatchRetire`，D1）——reconcile/orphan 结算风暴限流为单事件 | 高（不丢事实）|
| `examples/wechat-bot/main.go` | +57 | 宿主装配：allowlist env 解析、endpoint policy 注入 LLM client | 中（默认是否 fail-closed）|
| `memory/compaction.go` | +37 | 压缩路径改动 | 中 |
| `event/types.go` + `event/registry.go` | +9 / +6 | `TypeSettleFold` 常量 + 注册表 spec（Synthetic/Skeleton/Recallable/TTL）| 中 |
| `agent/event_bus.go` | +43 | **1.2 新增 `newBatchRetiredSummaryEvent`**（batch-retire 单事件生产者，非纯清理）+ 4.4 死常量 `TypeToolUse` 删除 + 注释 | 中 |
| `memory/lifecycle.go` +9 / `memory/segment_store.go` +14 | | harness 访问器 `SweepOnce()`（转调既有 checkTTL+checkCapacity）/ `Lifecycle()`（nil-safe getter）| 低 |
| **基建** `ci.yml` +60 / `race_check.sh` / `.gitignore` / `go.mod` | | 双模块矩阵 + race 子包 + Python 验证器负例 + OpenSpec strict + 上游竞态豁免 | **高（是否掩盖失败）** |
| **离线工具/数据** `rl/trajectory_analyze.py` +52 / `tests/offline_bench/{gen_token_fixture.py 99, token_fixture.json 827, report-*.json 489}` | | 估值器偏差分析 + 基准夹具（**运行时零消费者**，全仓 grep 确认）| 低 |

**测试（11 未跟踪 + 已提交 card_guard 364 / settle_fold 273 / offline_bench 336 / endpoint_redirect 136 / soak 233）**：见 §3。
**文档/spec（8）**：`docs/wiki/*`(4)、`docs/upgrade-rollback-drill.md`、`openspec/…/{tasks,EXIT,TRAJECTORY-BASELINE,MAINTAINABILITY-WP3}.md` + 1 spec delta。

## 2. 高风险核心逐点（reviewer 必读——这里才是 3.9 的实质）

**`context_compressor.go`（+384，本 change 最重改动）**——四条不变量：
1. `foldSettleRuns` 折叠 `[task settled]` 连续 run→单汇总卡片：须**字符回收 ≥80%** 且 **卡片 `Recallable`→原文仍可取回**，**事实链/registry 不受影响**（折叠只作用于呈现层，绝不删耐久事实）。
2. `guardCondensedCard`：浓缩票据须满足**票据 ⊆ 输入**（反伪造）——fuzz `card_fuzz_test.go` 已证 accept 性质；reviewer 核对守卫是否有绕过路径（畸形/嵌套卡片）。
3. **预算诚实**：压缩器报告的 usedTokens **不得低报**——直接关联 `TRAJECTORY-BASELINE.md` 实测（大上下文 est/real p50=0.846、83% 低估 → 溢出风险）。**关键**：estimator 对 tools-schema 每请求开销无建模，须核对预算线是否预留该缺口，否则折叠再准也晚触发。
4. `curateCards` O(n²)→修复：核对复杂度与等价语义（勿改判定结果）。

**`rl/endpoint_redirect.go`（+74，安全）**——`CheckRedirect` 逐跳：任一跳出 `allowedHosts` 立即拒；**空 allowlist 必须全拒（fail-closed）**。核对：是否仅覆盖首跳、scheme 降级（https→http）、大小写/端口绕过、是否被 `main.go` 无条件装配（若 `TAGENT_RL_ALLOW_LLM_REDIRECT` 未设是否安全默认）。

**`task_manager.go`/`agent.go`（batch retire）**——多事件合并为单 settle：核对**不吞非相邻 run**、unknown/late 结算仍各自落账、与 1.6 折叠路径正交不冲突。

**`memory/lifecycle.go` `SweepOnce()`**——= `scannerLoop` 每轮**同一函数体**，无第二套清理语义（单一真源）；核对幂等（已 tombstone 事件再扫安全）。

## 3. 测试面（断言真不变量、非自证——逐文件抽查）

| 测试 | 承载契约 | reviewer 挑刺点 |
|------|---------|----------------|
| `agent/compress/card_guard_test.go`(364)/`card_fuzz_test.go` | 浓缩票据 ⊆ 输入、反伪造 | accept 性质非 trivially-true？畸形卡片有无漏网 |
| `agent/compress/settle_fold_test.go`(273)+`context_simulation_test.go` | ≥80% 回收 + recall 取回 + 卡片留 retained + 负合成键 | 是否验证事实链**未被折叠删除**（非只看回收率）|
| `rl/endpoint_redirect_test.go`(136) | 逐跳 allowlist 拒越界、空全拒 | 有无覆盖 https→http 降级、多跳链、裸串旁路 |
| `tests/offline_bench/offline_bench_test.go`(336) | 预算诚实基准 | 阈值/夹具与真实分布偏差方向一致？ |
| `tests/soak_test.go`(233) | 长跑 compaction 强制触发 + 至少一次 | 子进程化后断言是否仍作用真实状态机（非 mock）|
| `tests/upgrade_rollback_drill_test.go` | spill 排空/回滚拒/drain=0/冲突诊断**只读** | drain 判据真实作用于 `Inbox` 状态机；「只读」真断言键未变 |
| `tests/resident_e2e_test.go` | R1/R2/R3 重建序 + receipt 对账 | 验证**顺序**而非各自成功；进程 vs 整机重启边界 |
| `agent/task_chain_e2e_test.go` | 全链 inline/bg/unknown/late/resume/lifetime/fold | late/resume 触及「至少一次非 exactly-once」真实重投 |
| `*/switch_combo_test.go`+`meditation_selffeed_test.go` | 治理/演化/冥想 fail-closed 组合 | 关态默认**拦**非放；critical 无审批恒拦有无反例；self-feed 负例真实 |
| `event/key_fuzz_test.go`/`memory/key_fuzz_test.go` | 键解析/事件协议全函数性 | 种子/失败夹具/重放命令留存 |
| `plugin/causal_eviction_test.go` | lastEventKeys 淘汰→parent 归 0，绝不 cross-link | 因果链语义断言精确 |
| `model_contract_matrix_test.go` | 真实模型契约矩阵 | CI 安全：无 key 必 SKIP 不 PASS（已验）；`apiErr`→SKIP 不误判 FAIL |

## 4. 据以判 diff 的项目不变量

1. **单一真源**：新增不得造第二套状态/清理/计数（`SweepOnce`/`Lifecycle` 低风险正因转调/透传）。
2. **折叠/限流不删耐久事实**：`foldSettleRuns`/batch-retire 仅作用于呈现/汇总层，事实链与 registry 恒存，recall 可取回。
3. **至少一次 ≠ exactly-once**：claim 不删原件、receipt turn 末耐久后 ack、重投可能重复。
4. **安全 fail-closed**：redirect 空 allowlist 全拒；治理关态恒拦。
5. **基建不掩盖失败**：CI/race wrapper exit-code 保真；豁免仅「每条 DATA RACE 顶帧命中签名 **且** 无非-race 失败」。
6. **凭据/PII 卫生**：无 key、无用户正文入仓（本会话 `sk-…` 全仓扫描 CLEAN）。

## 5. 作者已预验事实（reviewer 可抽验）

- **审查范围教训**：初版 `git status` 界定面**严重漏审**（漏 22 已提交文件含核心）；权威范围 = `832f43e` 累积 diff ∪ 未跟踪，共 **39 文件/+3817**。
- 双模块 `build/vet/short` 绿；`race_check.sh` 24 包 `OK`；openspec `--strict` valid + `check-openspec.sh` 96/96。
- `TypeToolUse` 非测试 `.go` **0 引用**；`settle_fold` 生产者(`foldSettleRuns`)+注册表(`registry.go:214`)+消费者俱在（非死码）。
- `model_contract_matrix_test.go`：无 key→`t.Skip`(`ok`)，有 key→6/6 PASS；`gofmt`/`vet` 净。key 全仓 **CLEAN**。

## 6. reviewer 签署栏（3.9 闭合门）

- [ ] 用 §0 权威范围（**勿仅用 git status**），确认覆盖 39 文件。
- [ ] 核心 §2 逐点过：compressor(+384) 四不变量、redirect(+74) fail-closed、batch-retire 不丢事实。
- [ ] 基建 §1 确认不掩盖失败。
- [ ] 抽查 ≥5 测试确认断言真不变量、非自证（§3）。
- [ ] 修复级问题清零（或记「非阻断」并说明）。
- [ ] 签署人/日期/结论。**两轮失败 → 升级 BLOCKED 上报，不得强推。**

> 结论栏：（待独立 reviewer 填写）______

## 7. 审查记录

### Round 1（2026-09-18，独立 CodeReview 子代理，非实现者）
按 §0 权威范围（base `832f43e` 累积 diff ∪ 未跟踪，39 文件/+3817）审查。裁决：**🔴 Blocker 0 / 🟠 修复级 1 / 🟡 Nit 2**，**不予签署 3.9 通过**。

- 🟠 **#1（已修）** `agent/task/task_manager.go` `finalize`：向共享 `batchCollect` collector 的 `*collector = append(...)` 在 `tm.mu` **之外**执行（读 collector 用锁、写不用）；并发 reconcile（turn `renderBoard` + 后台冥想 `renderSelfStateDigest` + 工具 `List()`）同时抵达 → collector 切片**数据竞争 + 结算可丢失**（违本 change「settle 不丢」不变量）。原 nested-safe 注释只证同栈重入、未覆盖并发。
  - **修法**：append 纳入 `tm.mu`（check-and-append 同锁内），host 回调 `onSettle` 仍在锁外。
  - **回归锁**：新增 `agent/task/task_batch_concurrency_test.go`（N=64 并发 finalize，单 batch 窗口）。**fail-before/pass-after 实证**：还原旧解锁写法 → `-race` `WARNING: DATA RACE ×4 + FAIL`；修复后 → `ok`。
- 🟡 **#2（已修）** `agent/event_bus.go`：`newTaskSettledEvent` 文档注释与函数体被 `newBatchRetiredSummaryEvent` 插入而错位归并——Go doc 误挂到新函数、真函数失注。已将文档块移回其函数上方（纯注释重排，零行为变更）。
- 🟡 **#3（记录，不改）** `rl/endpoint_redirect.go` 逐跳策略不约束 scheme（允许 https→http 降级到同 allowlist host）。属**设计取舍**：allowlist host 已持 key、为可信，非新 SSRF/泄露升级；收严（加 `Scheme=="https"`）会误伤 localhost/http 合法端点。留 reviewer/维护者裁量，本会话不擅改安全语义。

Round 1 PASS（子代理逐点核可）：`foldSettleRuns` 仅呈现层/事实链未删/recall 可用；`guardCondensedCard` 票据⊆输入反伪造非自证；`curateCards` O(n²)→线性等价；`EndpointRedirectPolicy` 逐跳/空表全拒/hop 上限/装配 fail-closed；`SweepOnce/CompactOnce` 单一真源；基建 exit-code 保真；契约矩阵无 key→SKIP；`resident_e2e` 作用真实状态机。

**遗留（待 Round 2 独立确认 #1 已闭合）**：修复后须第二方复核 finalize 加锁正确性（含 `finish` 锁内摘 collector 的残余窗口）+ 新测有效性。3.9 于 Round 2 签署前保持未勾。

### Round 2（2026-09-18，独立 CodeReview 子代理，聚焦确认 #1）
**裁决：🟠#1 已闭合，修复级问题 = 0，可提交 3.9 签署。**
- 独立复核 5 点全通过：① `tm.batchCollect` 及其 pointee 的全部读写（`task_manager.go:996/1005/1009/1054/1062`）均在 `tm.mu` 下，判空→解引用→append 同一不间断持锁内完成，use-after-nil 路径不存在；② `finish` 锁内置 nil 后锁外读/交付 `batch` 安全（happens-before 链完整，无并发写者可及）；③ 无死锁/锁序倒置（`t.mu` 释放后才取 `tm.mu`；`onSettle`/`onBatchRetire` 均锁外调用）；④ 单协程语义等价（legacy nil 回调早退、折叠、TOCTOU first-terminal 未触及）；⑤ 回归测 fail-before 实证——**reviewer 自行还原 HEAD `8ef6b01` 复现 `WARNING: DATA RACE` + `delivered batch = 34, want 64`**（同时坐实竞态与丢结算），SHA 校验零污染。
- **未引入新问题**。一处**非本轮、非修复级**既存健壮性观察（`RetireOrphans`/`reconcileZombies` 的 `finishBatch()` 未 `defer`，panic 可致 collector 残留）——属批量特性设计层，R1 已 PASS、R2 明确不重开，登记 `MAINTAINABILITY-WP3.md` 作跟进候选。

> **3.9 签署**：经两轮独立 CodeReview（非实现者）——R1 发现并促成修复 1 个修复级并发缺陷，R2 确认修复级清零。用户裁定该子代理计为独立审查方。**3.9 通过。**
