# 运行时恢复理想态（runtime-recovery-ideal）

> 当前阶段（2026-11-18，R265）：复盘后的计划校正，未完成。先前归档已回退，既有实现保留；旧整体完成判断及R264全面修复声称撤销。当前目标见design D1/D5/§六，执行叶见tasks索引及ORCHESTRATION；原始台账和顶层任务移入活动变更内HISTORY文件，不是完成归档。独立部分可在完整验证后分批commit，当前尚无已获放行候选。

## Why

### 原立案诊断

用户原话：「目前 WAL 的 journal.log 会无限膨胀」。考古与严肃反思后，问题不止物理膨胀——**之前的实现没有完全兑现设计预期**（用户裁定：「需要严肃反思」）。

教义（`59feb537`）：「one fact chain as the single truth, with **bounded derived state** and **one g(x) derivation path**」；session-checkpoint-material-unification/design.md:9：「链是真相，checkpoint 是派生态的载体而非第二真相；恢复＝读最近载体＋对尾部重跑同一个 g(x)，不另立派生实现」。

对照 spec SHALL 与代码（证据见 design §三），真实违背：

1. **任务恢复全史读**：`DurableCommit.load`（runtime/state/durable.go:305）在 checkpoint 恢复之前无条件全扫五前缀、解码全部任务正文；`restoreChains` 覆盖跳过只省 fold 不省 load。违 checkpoint-state 的「任务终态有界恢复」条款「boot 任务恢复成本 SHALL 不随历史任务总数线性增长」（该 capability 无主 spec 目录，权威文本见 12-checkpoint-state 冻结归档）。12.4b 残留栏书面自认「body 已由 durable load 全量读入，非本单可消」。
2. **journal 开档全读 + 索引全量物化**：journal.go:490 走整份文件、order 每帧每键永不回收；segment_store.go:281 Load 建全量 positions map。只有正文读有界，开档 I/O + 索引 + 常驻内存仍 O(全史)。违 checkpoint-state 的「框架统一 checkpoint 事实承载可恢复状态」与「恢复为最近 checkpoint 加偏移后回放」两条，以及 window-recovery 的「折叠锚恢复」条款（两者均无主 spec 目录，权威文本在 2026-11-11-hot-update-and-purity-closure 冻结归档）。
3. **governance 未托管**：DenialLedger/GoalRegistry 双全扫（denial.go:304/327）但生产零调用；生产只挂 approval book（ApprovalManager.rebuild 目录扫）。教义「全部可恢复状态 checkpoint 托管」未覆盖。
4. **验收锚量错面**：checkpoint_test.go:1231「boot 平坦」只断言 CommittedFacts()（正文计数），不量 journal 字节/索引规模/durable load/墙钟；夹具单会话无任务。违背 1-2 全在锚外，「全绿归档」掩盖未达成。
5. **主 spec 与代码反向**：~16 主 spec / 628 SHALL / 181 legacy 条款（task-skeleton-compression:51/:74 禁 LLM vs summary_gist.go:57 LLM gist；compression-recovery-ticket:8 仍 SHALL 已退役 ticket；等）；golden durable_truth.json:300 键形 prose 漂移。

**不是违背（接受边界，须分清）**：journal 物理厚度无界本身是教义意图（链全量真相、恒重放，`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」：任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实）。cadence 三触发已全接线、B3/B4 有界投影已达成（先前误报，已澄清）。

## What Changes

用户终裁：**把所有已知问题修干净，让机制达架构理想预期状态，同时清理废弃所有无效机制和代码**；不考虑开发成本，全修到干净终点、一笔做完。物理有界取向用户裁定**乙**（教义兼容扩展）。

- **D1 任务托管收编**：DurableCommit.load 改读 tasks checkpoint 切片（扩携 consumed/attempts/rejected），时序移到 checkpoint 恢复之后；消除全史扫旁路，兑现单一 g(x)。
- **D2 索引与真实开档成本**：引擎自持可寻址索引与增量位置，不从checkpoint派生读取checkpoint自身所需的索引。避免每次全量物化历史；磁盘索引、常驻规模、索引回放窗口和完整帧校验分别计量。完整性扫描按O(日志字节)声明，不能再由窗口小推导整个开档I/O有界。
- **D3 governance 托管**：governance 状态收编为 Stateful 切片（尾 seq + 审批态）并接线生产；或裁定 approval book 为权威并收编其 rebuild（design D3 裁定）。
- **D4 验收真量**：boot 锚改量 journal 字节 / 索引条目 / durable load 计数 / 墙钟（非正文计数代理量）；多代＋多任务夹具；负锚（全史读回归→红）。
- **D5 契约同步 + 死码清理**：~16 主 spec 转正/退役 legacy 条款；golden 同步；删死生产者 ReplayProjection.Fold（fold.go:49）/ RebuildProjectionFromWAL（projection.go:342）；六 Material 兼容读端口审计后退役。
- **D6 退位分层（乙）**【已废，2026-11-16 用户裁定反转】：原判「已结事实搬迁到冷层、仍按身份可读，热 journal 有界，接线 lifecycle/retention/guard 脚手架」。**退位整套机制已删除**，热层只追加、不搬迁，其字节增长是显式声明、单独拥有的关切（MUST NOT 被声称有界）；既有冷档数据由外部脚本 `scripts/fold-cold-root` 折入新根。取舍依据与用户裁定原话见一级 [design.md](design.md) D6.13 勘误与 ORCHESTRATION R50/R51。

**核心不变量**：链是唯一真相、一身份一不可变事实、SHALL NOT 删/改已提交事实；逻辑链恒完整、恒可按身份重放。

### 本轮重整：从现有证据恢复完整验收

原 01–10 子域编号与有效成果保留；新增 11–13 承载此前未完整规划的跨域工作，不把新建议伪称为用户已经批准的实现签名。

- **事实完整提交（11）**：当前原文/ref 与事件/位置索引分两笔保存；统一事实身份等价、编号和整笔原子保存。正常委派续轮会重交稳定岗位说明身份，不能由「未找到旧键重交」推导只查热层；generic 引擎负责批原子，事实层负责领域等价。
- **有效状态与消费一致（05/07/12）**：检查点保存处理进度、有效状态、未完义务及必要材料；历史查询不靠启动全量恢复。正常消费与恢复共用状态应用，失败后不能把陈旧状态作为已处理状态落检查点；插件卸载/替换不能无声丢弃义务。
- **分阶段成本（06/08/09）**：分别测启动、提交、按身份读取、索引维护、重绑定与总耗时。索引读侧及去全量物化已有实现，但窗口读取少不代表全启动无历史成本；旧键前驱定位、压缩和审批状态的历史遍历仍须承接。原S2启动测量面全部保留，已声明安全扫描与可消除的重复工作分别判断；删掉恒真成本测试不等于完整归因已建立，不以改宽阈值换取通过。
- **CI 真实失败（03/04/09）**：HEAD `9547faca2327c31579e91fbe40a27405d3ffdfb3` 的大窗口分片在第 8 次重启后因结算通知匹配漏票据而失败；同一压缩请求出现两个产物、背景结果匹配超时仍待核销。小窗口相同 37 输入在 gen0/gen40/gen80 合计耗时 3.42/125.58/445.80 秒，启动 0.018/3.83/17.09 秒，重绑定 0.043/16.35/59.88 秒；证实增长，不证明所有耗时均由冷读造成，也不据历史异机样本宣称慢了 50%。
- **证据与放行（13）**：复用现有运行器、结构化测试输出和证据检查，绑定被测源码、命令/参数、实际测试集合、指标单位/区间、完整分片及反例；登记表手填 killed、源码关键词、退出码 0 且零测试都不能作放行证据。有限自测只证明已列反例，不宣称绝无遗漏。
- **旧账清偿（10）**：规格冲突、未接线能力、废弃读路径逐条对账；原 F1–F3 与子域旧勾选只代表历史时点结果，不推导当前完成。

原症状「journal.log 无限膨胀」的物理封顶目标已随D6.13裁定撤销；日志增长量与原文可读仍检查，不声称原物理问题被有界化解决。S2保留真实启动各测量面，S3保留模型自主召回。新增承接包括宿主配置入口、恢复总顺序、折叠后重试、同通知跨进程收据、生命周期安全拒绝、审批与压缩成本，以及最终代码和依赖绑定。现有失败记录、静态缺口和未建立测试分开记录，未执行者不计通过。

## Capabilities

一级只保留编排 delta；各领域优先修改既有能力，不为每条修复新建同义规格。已检索的同域能力包括：

- `session-checkpoint-material`、`task-registry-rebuild`、`consumer-loop-convergence`：状态托管、消费推进和恢复；旧 `checkpoint-state` 名称在子域重整时对齐，不另立一套恢复契约。
- `event-segment-store`、`fact-vocabulary`、`partition-local-storage-access`：整事实提交、身份不变与引擎读写保证。
- `compression-pipeline`、`event-key-visibility`、`request-budget-accounting`：压缩、呈现消费者和预算口径。
- `runtime-resource-ownership`、`config-hot-reload`：插件状态/义务交接和配置语义。
- `trajectory-verification`、`architecture-guardrails`、`orchestration`：真实执行证据与状态放行。
- 原 `bounded-recovery`、`fact-retirement`、`storage-lifecycle-completion` delta 按领域保留、对账；同域主规格已存在时使用 MODIFIED，缺失时才 ADDED，不复制另一套权威。

## 边界与依赖

- **依赖**：现有 Stateful 接缝（runtime/state/checkpoint.go:36）、checkpoint 事实族（api/event/checkpoint.go）、seedFact 隔离重建（run.go:2946）、折叠锚（compress_consumer.go）、AtomicStore 缝（atomic_store.go）、MigrateShardedRoot（migrate.go:76，外部折回脚本借用其「复制＋校验」半段）。
- **被依赖方**：远端 wechat-bot 部署（磁盘增长是显式声明的关切）；所有 boot 路径消费者。
- **接口面**：Stateful 切片接口（StateName/SnapshotState/RestoreState）是托管统一接缝；存储面另有可寻址索引的槽表与 order 文件（引擎自持，06 域），持久面端口收口见 10 域。
- **禁止事项**：
  - 不触碰外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）。
  - **SHALL NOT 删除/覆盖/改写已提交事实**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；按身份读恒返回原字节与原索引位置。
  - `config-surface-normalization` 的键名归位是当前实现前置；两笔合提曾获明确授权，保留配置成果，不制造不可构建的拆分。最终验收/归档重新核证，既有发布授权不允许带失败放行。
  - `no-prefix-criterion` 保持独立。本轮不修改其文件、不吸收全面身份格式迁移；已交接的呈现单一拥有者/双票据问题不能隐藏，若当前验收与后置启动条件构成循环，设计门先解决归属。
  - 用户真实 `.wechat-config/data` 与 `--source` 禁止访问；不把该限制扩大为禁止在独立临时根运行 wechat-bot 合成验收。
  - 本轮只规划与校正完成账，不改生产代码、实际 CI/权限、用户规则或 skill，不提交、推送、归档；新增实现选择按设计门验证，不把候选修法当成已冻结裁决。
