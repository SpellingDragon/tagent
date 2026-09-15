## Context

tagent 配置加载链现状（已实证）：`tagent/config.go`（902 行）定义 `Config`，`registry.go` 经 `sync.Once` 注册默认工具；`tagent.New` + `WithConfigPath` 装配时 reloader 闭包 = mtime 检查 → 重解析 → 指纹比对 → 未变则热应用阈值 / 变则报 RESTART required（`tagent.go:315,359`，增量 A 交付）。wechat-bot 的 `main.go` 是 dogfood 消费方。

**Scope 终版裁定（用户最终裁定，取代此前所有修订轮次）**：本计划最终 scope = **A 段配置热更新 pure**：
- configWatcher（**mtime 轮询触发器，不引 fsnotify——轮询间隔即天然 debounce**）
- atomic.Pointer 快照（解析→校验→指纹比对→成功才 Store，失败保旧 + ERROR + 计数器）
- 消费点改读快照（先做 1.1/1.2 盘点）
- 单测三分支 + wechat-bot main.go 接线 + dogfood 三景

两项明确出局：
- **"重启还原"** = 通过 WAL 事件回放恢复上下文现场，归属老计划 `tagent-compress-event-sourcing` 的收口（5.1 全量回归/-race、5.2 dogfood 含 boot 注入与跨进程 store 续读断言、5.3 push dev、加转世通报 NOTICE WAL 尾块修复），**与本计划零关联**
- **配置版本化/快照还原**（config-version-restore）出自由被纠正的错误指令，不入 scope，其 spec 已作废待删（连同早前作废的 restart-lkg-rollback）

**与 `tagent-agent-hot-reload` 计划的关系（续作合并）**：该计划的指纹算法（1.1/1.2）与 reloader 骨架已交付并 e2e 全绿；其 tasks 2.x（惰性重建接线）/3.x（在途事件）/4.2/5.1 未动。本设计**不重做指纹**，只补"文件变更→自动触发"与"快照原子切换"两环，把该计划的检测层从被动（按需 lazy mtime check）变主动（周期轮询）。本计划收口后，该计划剩余任务统一合并处置。

## Goals / Non-Goals

**Goals:**
- 配置文件保存后秒级（轮询周期量级）自动完成重解析→校验→快照切换或保旧+审计，全程无锁竞争、无重启、无新增依赖
- 交付门禁：dev commit+push，Go 全包回归绿（今日基线五包全绿）
- 验收：dogfood 三景真机实证（热载生效 / 坏配置保旧 / 修复自愈），日志即证据

**Non-Goals:**
- 不引 fsnotify / 不做文件系统事件监听（go.mod 零变更）
- 不做配置版本化、快照、还原/回滚（config-version-restore 已作废出 scope）
- 不做二进制 LKG/自动回滚（早已删除）
- 不做 WAL 回放/上下文恢复（归 `tagent-compress-event-sourcing`）
- 不做第 1/2/4 层（全局默认/MCP/节点引用）的监听改造
- 不改 restart-tagent.sh 任何既有段（env.snapshot 归档与 done-sentinel 握手是重启链既有职责）
- 不做跨机分发、不做配置 diff UI

## Decisions

### D1 检测机制：mtime+size 周期轮询触发器，不引 fsnotify，轮询间隔即天然 debounce

**选择**：新增 `tagent/config_watch.go`，构造 `configWatcher`：每周期（默认秒级、可配置）检查配置文件 mtime+size，与上次记录不一致即触发一次重载。**不引 fsnotify**——编辑器保存常触发 write+create+chmod 多连发，且 vim/sed 写临时文件再 rename 会丢 inode，事件语义本身不可靠；周期轮询天然规避：窗口内无论写多少次，窗口结束重读一次文件即得终态，**轮询间隔即 debounce**，无需另设归并器。

**理由**：① 零新增依赖（go.mod 不动），与项目"最小机制"气质一致；② 免去事件库的平台差异（inotify 限额/容器挂载丢事件）与降级链复杂度；③ 轮询间隔=debounce 窗口，机制合一，无独立状态机；④ 秒级间隔下解析开销可忽略（配置文件 KB 级）。

**备选**：fsnotify + 300ms debounce + 轮询降级——被否决（用户终版裁定：不引 fsnotify）；SIGHUP——被否决：要求外部显式发信号，与"改文件即生效"不符。

### D2 快照语义：`atomic.Pointer[Config]`，重载链全过才 Store，失败保旧+审计

**选择**：进程持单一 `atomic.Pointer` 配置快照。轮询检测到变化 → 完整重跑既有 reloader 链（重解析 → 校验 → 指纹比对）→ 全部通过才 `Store` 新快照；任一步失败：不 swap，记 ERROR 日志（含失败原因、文件路径）+ 递增 `config_reload_failures_total` 计数器，旧快照继续服务。消费点改为经 getter 读 `Load()`，禁止缓存裸 `*Config` 引用（除启动装配期一次性读）。

**理由**：① atomic.Pointer 的 Load/Store 天然满足"读无锁、写原子"，与 `tagent-agent-hot-reload` D2 的快照不可变主张一致；② 先验证后 swap 使失败路径零中间态；③ 审计（ERROR+计数器）满足"坏配置长驻可发现"的既有风险缓解模式。

**备选**：RWMutex 全局 config——被否决：读路径加锁污染热路径；copy-on-write 分片 map——被否决：过度设计，整快照替换足够。

### D3 与既有 org reloader 的接线：watcher 是"触发器"，不是第二个重载引擎

**选择**：`configWatcher` 只负责"文件变了"这一事实的检测，重载执行完全复用 `org_hotreload.go` 的既有链路（指纹比对 → 热应用/RESTART required 判定）。watcher 不解析、不校验、不碰指纹——它调 reloader，reloader 决定结果。

**理由**：增量 A 已把 reloader 调通并 e2e 全绿，重造第二个引擎必然漂移；watcher 与 reloader 职责正交（检测 vs 决策），这条边界让"改 mtime 但内容没变"（touch）场景自然落在 reloader 的指纹比对里被吸收。

**备选**：watcher 内联重解析+比对——被否决：与 reloader 逻辑重复，指纹白名单/黑名单边界会在两处维护。

### D4 验收口径：dogfood 三景真机实证（日志即证据），单测降为行为边界回归

**选择**：生产语义的验证**只认 dogfood 真机实证**——直接对 wechat-bot 运行实例操作真实 `config.yaml`：① 成功热载（改白名单字段 → 运行日志出现"检测到变更→重载开始→快照已切换"，消费行为随之变化，进程不重启）；② 坏配置保旧（写入坏 YAML → 日志出现 ERROR 重载失败，进程不崩、行为保持旧值）；③ 自愈（修复坏配置 → 再次热载成功）。运行日志即验收证据。单测（t.TempDir 写真实文件）保留但降格为**回归保障**：三分支 + touch 无变更 + 轮询窗口多写合并，**不作为生产语义的证明**。

**理由**：热加载要验证的正是"运行中真实进程"的行为链（watcher 真轮询 → reloader 真决策 → 快照真切换 → 消费点真读到新值），每一环都依赖真实进程上下文；沙箱测试只证明代码路径可走通，证明不了生产语义。

**备选**：沙盒双路径实跑——被否决：桩环境与生产语义不同构，验证强度低于真机演练而成本相近，属过度设计。

## Risks / Trade-offs

- [轮询延迟] 变更感知延迟 = 一个轮询周期（秒级） → **缓解**：可配置间隔，且"保存后秒级生效"满足运维诉求；不接受 ms 级时另议（需引事件库，违背 Non-Goals）
- [轮询空转开销] 每周期一次 stat 调用 → **缓解**：开销可忽略（单文件 stat），间隔可调；不引入事件库反而免了 inotify 限额与容器丢事件风险
- [mtime 精度] 文件系统 mtime 精度不足可能漏检同秒写回 → **缓解**：mtime+size 双信号；即便漏检，下一次真实变更仍会触发（自愈语义）
- [热载与在途事件] 快照切换瞬间在途事件持旧引用 → **缓解**：沿用 `tagent-agent-hot-reload` D2 drain-free 语义，快照不可变，持引用即安全
- [计数器无消费方] 本期只落日志+计数器，无告警通道 → **缓解**：计数器命名进 ops 文档，接告警是后续能力
- [续作合并断层] 本计划收口后 `tagent-agent-hot-reload` 剩余任务需合并处置 → **缓解**：tasks 4.2 强制在收口报账中明确合并方案，不留悬空

## Migration Plan

1. 1.1/1.2 盘点（消费点清单）→ 写 `config_watch.go`（轮询 watcher + 快照管理）→ 消费点改快照读 → 单测三分支 → wechat-bot main.go 接线 → dogfood 三景
2. 全量门禁：`go build ./...` + `go vet ./...` + `go test ./...`（五包基线全绿）→ dev commit+push
3. 回滚策略：watcher 经 `WithConfigWatch(false)` 等价开关可整体关闭（回退 lazy mtime 行为）
4. 收口报账时明确 `tagent-agent-hot-reload` 剩余任务（2.x/3.x/4.2/5.1）的合并处置方案

## Open Questions

（无——scope 已由用户终版裁定收窄，fsnotify 已出局，争议点清零）
