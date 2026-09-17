# 常驻实现收口工作清单

本文件是后续实施任务，不代表本轮已修改代码。全部复选框初始未完成；本轮已执行的基线验证见 `assessment.md`。依赖与决策以 `design.md` 为准。每任务先读取当前实现、补 fail-before 场景，再修改与回归；遇设计哲学冲突停下裁决，不忠实执行冲突方案。不得因归档记录勾选而跳过本轮验收。

## 1. WP0：证据与契约基线（F10/F11，P0）

准入：基线复核。可与 WP7 文档草案并行，后续行为改造以本包完成为前置。

- [x] 1.1 对比实施时 HEAD 与评估基线，逐条刷新 F01–F11 的仍在/已修/部分/未验证状态及反证；记录变更文件→测试映射，保留三份原报告。
- [x] 1.2 为 race wrapper 建负向测试：非法包、编译失败、普通断言失败、超时、未知 race、已知 race 混合普通失败；旧脚本应至少在非法包场景失败。
- [x] 1.3 修复 `scripts/race_check.sh` 的退出码保真和完整失败输出；已知 race 仅匹配明确登记签名，所有 1.2 场景通过。
- [x] 1.4 对照当前主 spec 核验本变更 delta：无锚 fallback、StopLoop 终结态、ring 配置回滚、有效 memory 指纹；保证替换整个冲突 Requirement，不在提案阶段提前同步主 spec。
- [x] 1.5 整理 upstream race 豁免的测试名、签名、依赖版本、解除条件；保留 `agent/invariants_i2_test.go` 的真实插件管线验证，禁止无证据扩大 skip。
- [x] 1.6 复跑双模块 build/vet/short、脚本反例和定向 race，记录命令及跳过项；WP0 准出不能以“无 race 文本”代替测试成功。

## 2. WP1：事件存储契约（F01/F02/F04，P0/P1）

准入：WP0。修改域 `memory/kv/`、`memory/segment_store.go`、生命周期、装饰链和组合根接线；与 WP3 修改组合根的任务串行。

- [x] 2.1 给事件写入屏障建独立子进程测试：单事件不足 flush 阈值，StoreEvent 成功后不 Close 直接终止，新进程按 key 水合原文与索引；证明失败发生在旧实现。
- [x] 2.2 在 KV 层增加可注入文件操作故障点，覆盖 write/Flush/file Sync/首次 WAL 目录 Sync/snapshot rename/目录 Sync；不改真实数据目录。
- [x] 2.3 明确异步 KVPut 与同步事件提交契约，实现可选 Sync/耐久能力透传；FileSegmentStore 完整写 evt/idx/meta 后屏障成功才更新缓存、计数和返回。
- [x] 2.4 传播阈值和定时 flush 失败，保留 pending、last_error 和降级能力；非“不支持”的目录同步错误不可吞；装饰链直到 diagnostics 可见。
- [x] 2.5 引入 typed missing/duplicate/I/O 错误，修复 StoreEvent 碰撞检查、必需 meta 写失败、GetEvents/QueryEvents 吞错；合法 missing 与 I/O 在测试中分别断言。
- [x] 2.6 将存储重放集中为同 key 同内容核对及补齐路径；构造 evt 已写/idx 缺失/meta 缺失/屏障失败场景，重试无不同内容覆盖、无重复投影；普通重复提交仍明确拒绝。
- [x] 2.7 统一内存与文件后端重复键、无分区查询和可变值拷贝语义；共享一致性测试涵盖 Metadata/Message map/slice、缓存命中与未命中，测试调试遍历改用显式接口。
- [x] 2.8 冷启动先恢复墓碑，再按 EventKey 去重重建 live count，最后启动生命周期/压实；枚举不支持/失败显示 unknown 并暂停容量淘汰。
- [x] 2.9 修复计数更新时点与重复递减；覆盖写失败、duplicate、TTL、容量墓碑、物理清理和压实搬迁。类型豁免从注册表派生，TTL 默认保持。
- [x] 2.10 运行 `go test ./memory/... ./plugin/... ./tool/recall/... -short -count=1` 及对应 race，补 stored-gate/recall/engine 装饰链集成测试；记录 fsync 开关两档成本，不以关闭 fsync 的测试宣称耐久通过。

## 3. WP2：可靠接收与恢复可见性（F03/F05，P0）

准入：WP1。不默认开启可靠模式。receipt、EventTypeSpec、投影过滤及宿主消费者同步修改。

- [x] 3.1 新增 context + request ID 的 Publish/Inject 返回结果入口与 receipt；保留旧 void 包装，覆盖 loop 未启动/已终结、超时、nil 输入、volatile accepted 和失败计数。
- [x] 3.2 将可靠模式改为 `inbox-v1` 全量先持久化，channel 仅唤醒；串行接收分配序号、上限 2560、初始化/满额/不可写明确拒绝；并发测试验证无超车和无静默降级。
- [x] 3.3 实现单 envelope 批次与固定 source ID/EventKey，完整提交后才 accepted；合并模型 invocation 不丢来源/边界，批次重试不生成伪新事件。
- [x] 3.4 实现 claim/commit/processed receipt/ack 状态机；处理完成 receipt 注册为非投影事件，ack 前不得清除原件；崩溃在 StoreEvent 后但执行前仍可恢复处理。
- [x] 3.5 明确 receipt 保留与去重窗口：outstanding 对应 receipt 免 TTL/容量，ack 持久清理后按 30 天保留；只为 outstanding 构建启动索引，验证超窗客户端重试提示及无全历史内存膨胀。
- [x] 3.6 覆盖接收/claim/事实提交/模型结束/receipt/ack 各故障切点及 ack 删除失败；确认原文与投影不重复、未处理项不丢、外部副作用只作至少一次声明。
- [x] 3.7 实现旧 spill 非空拒绝启动与迁移指引、损坏项隔离保留、启动重放和关闭顺序；并发 Start/Stop/Close 与 loop panic 更新真实状态、通道只关一次。
- [x] 3.8 引入 RecoveryResult 并贯通所有返回路径：snapshot miss/payload 损坏/空链/分页失败/批量错误/键集合缺失；failed 不伪报空库，partial 不伪报 FULL。
- [x] 3.9 修复 fallback 为分页→过滤→保留最新 500 有效事件；用 600 有效+600 元记录、跨页乱 Timestamp 验证保留集合、写入序、truncated 计数与有界内存。
- [x] 3.10 将恢复状态传到 diagnostics 和首次模型请求尾部的一次性运行态提示；不写第二历史源，不改变旧历史前缀；TTL miss 与存储故障分别说明。
- [x] 3.11 运行 bus/spill/persist/projection/lifecycle 相关单元与完整工作流测试；补真实 FileSegmentStore 故障链，不只用直接返回 error 的 mock；运行 `go test ./agent/... ./memory/... ./event/... -race -short -count=1`。

## 4. WP3：资源所有权与多 agent 热更（F06/F07，P1）

准入：WP0；与 WP1 的组合根变更顺序合入。WP4/WP6 依赖本包。

- [x] 4.1 补共享资源 fail-before：两个根同 path 关闭一个、最后 Close 后再次 New、同路径冲突配置、构建中途失败；断言真正 reopen 而非复用旧对象。
- [x] 4.2 实现 RuntimeResources 租约 registry、canonical path 与指纹，替代裸 named maps；默认 registry 保同进程共享，子 agent/执行壳只借用，最后租约关闭与移除，失败逆序释放。
- [x] 4.3 对持久目录实施单 writer 锁；独立子进程验证重复打开拒绝、进程终止后可重新取得锁；不给同路径滚动双写隐式许可。
- [x] 4.4 为同实际 store 的 agent 名建立 pid 冲突验证；构造 10 bit 哈希碰撞对，启动/热更均 fail-closed，不修改已有 EventKey 或历史数据。
- [x] 4.5 将热更验证壳绑定改为按 agent 身份访问常驻资源表；新子树与存活对象正确挂接，新增/删除 agent 资源分别创建/在飞结束退役，禁止全部复用 entryMemStore。
- [x] 4.6 建三 agent 集成测试，真实捕获换代前后各自请求、store、projection 与 task manager；同时改工具/模型/预算，验证新代对象有效而非旧 cache 数值变化。
- [x] 4.7 收口 desired/effective 与零值/未设置语义；memory 拒绝不推进 effective，字段删除恢复默认；回执从真实对象回读，验证首次换代、重复拒绝和数值回滚。
- [x] 4.8 修复 SwappableModel 租约直到流关闭/取消，覆盖流仍输出时 Swap、error/nil channel、取消、不消费但超时、Info 并发、A→B→A；Close 恰一次且不持全局锁阻塞。
- [x] 4.9 核对 runner/model/tool/session/store 的 owner 表和退出顺序；rollback 仅保留配置快照，验证壳失败清理不误关常驻会话；不复制一套发布管理器。
- [x] 4.10 运行根装配/hotreload、agent recycle、rl swap、任务恢复及两个根 Close 集成测试与 race；承接旧 hardening-review-batch2 5.6 的未完成多 agent 验收。

## 5. WP4：控制面与运维边界（F09，P1）

准入：WP2/WP3。复用现有认证，不重复建设认证系统。所有网络测试限 httptest/本地 mock。

- [x] 5.1 增加 HTTP limits 配置与单点完整请求验证：1 MiB body、32 messages、256 KiB content、合法 role；超限/非法请求在 endpoint/Inject 副作用前拒绝，覆盖零值/负值。
- [x] 5.2 `/task` 接入单 envelope receipt：返回 request_id/durability/原 status；closed/背压/I/O 明确非 202，批次不得部分接收后返回整批成功。
- [x] 5.3 增加可返回 error 的 endpoint 更新接口和显式 enable/host allowlist，保留受信旧 callback；验证 URL/重定向、脱敏日志、更新失败不注入，并发请求串行化。
- [x] 5.4 feedback 通知队列限制 1024，溢出淘汰最旧通知但保留事实；wait/diagnostics 暴露 dropped_count/partial/补查指引，覆盖无消费者和重启。
- [x] 5.5 long-poll 接入请求/服务器取消，宿主使用统一认证监听助手及有限 server timeouts；取消与通知同时发生不泄漏，shutdown 可完成。
- [x] 5.6 wechat-bot、训练适配器、mail poller 与重启探针同批适配；mock 401/403/413/429/503/200，验证认证失败不杀进程、不写成功 marker、不泄露 token。
- [ ] 5.7 在隔离部署夹具验证非 root、只读根、可写路径、符号链接越界和凭据权限；确认 working_dir/分类器不是安全墙，tmux 创建首条命令即获得预期环境；不修改维护者主机权限。
- [x] 5.8 双模块 build/vet/short 与 `go test ./rl/... ./tool/action/... -race -short -count=1`，运行脚本测试及 `bash -n`；记录实际执行范围，未获隔离环境时明确 BLOCKED。

## 6. WP5：票据守卫与性能边界（F08，P1/P2）

准入：WP1；不新增运行时 tokenizer 或检索后端。

- [ ] 6.1 补卡片反例模型：非空但无 key、未知 key、丢首尾、丢高亮、不可解析、多行；旧路径错误接纳的用例先红。
- [ ] 6.2 在 curateCards 接纳前校验输入/输出 key 集合，失败复用原卡片确定性下沉；验证合法浓缩、无模型/超时、计数正确且原文不变。
- [ ] 6.3 处理单卡超预算与 budget-unrepresentable，保留可解析票据/截断标记并把无法表达状态传给诊断；序列化/重启恢复后守卫仍成立。
- [ ] 6.4 运行压缩预算、under-budget 零整理、前缀冻结、tool 配对、recall items=50、engine 失败→关键词等组合测试，确保声明集与调用路由不变。
- [ ] 6.5 建离线 benchmark：1k/10k/100k 事件，1/10/100 探测任务，fsync 两档；记录 p50/p95、allocs、RSS、fork、扫描量及中英/代码/JSON token 估算误差，fixture 附 tokenizer 版本。
- [ ] 6.6 输出支持规模和性能回归对照，超 design D7 阈值先 profile 定因；仅有实测收益的优化另立 change，不自动引入 ANN/BM25/数据库替换。
- [ ] 6.7 压缩回收提升（远端 2026-09-17 trajectory 实测驱动；分析器 rl/trajectory_analyze.py 已落地）：task_settled 结算风暴 136 条占上下文 67.8%，external_input 不在工具对折叠范围——① reconcile/orphan 批量退役汇总为单条 external_input（事件数 N→1）；② settled 类 external_input 纳入压缩票据化折叠（卡片行+[evt_key] 票据，原文冷存可 recall）；③ 估值器另案（chars/token 实测 2.5-2.6 vs 假设 2）。准入：WP1 屏障（结算事实可信）。
- [ ] 6.8 cold-eyes review 修复（2026-09-18 两轮审查，详见 notes §cold-eyes 与 §cold-eyes R2）。**R2 修复（本批，全量+race 回归绿）**：M-1 结构修（claim 事实统一 turn-start persistBusEvent 逐消息预落库——GetEvent-guard 重放去重+单数 dedup_key+回写一次到位；管线经 DurableInbound.FactsPrePersisted 跳过合并输入重复入库；多 envelope 跨 crash 批次回归 TestDurableReceipt_MultiEnvelopeBatchReplay 证明 A/B 均落库均有自有证据 receipt+ack 收敛）/W-1（MemoryPlugin 移除 ErrDuplicateEventKey 吞噬分支——碰撞语义恢复 D15，replay dedup 职责统一归 persistBusEvent，SetDurableKeySink 撤销）/M-2（resources opening map 锁内读插 + 并发同路径 acquire race 测试）/W-2（flock 先于 open + MkdirAll，关跨进程第二写者窗口）/S-1（controlMetaKeys +4 个 inbox_* 键）/S-2（RecordEventKeys 回写去重）。已修（全量+race 回归绿）：Major 3（claimDurable 按消息序号分配单数 inbox_dedup_key——joined 形式致多消息 envelope 第 2..n 条事实静默丢失+EventKeys 无界增长）、Major 4（/feedback body 上限 413）、Warning 2（空批 continue 前 finishDurableBatch 防 envelope 僵尸化）、Major 2（TypeInboxReceipt 启动消费：RecoveryResult.ReceiptedRequestIDs 收集 + inbox.pathsByRequestID/ConfirmDurableByRequestID + build_agent ReconcileDurableReceipts——receipt 落库 ack 未落的崩溃窗口不再重执行）、Warning 1（resources acquire/release 锁外 open/close + per-key opening mutex）、Minor 1（TryPull NonWake）、Minor 4（endpoint 日志 host-only）。**第二批已修：Major 1（主输入事实路径 dedup-aware 闭环）**——plugin.WithDurableInbound/DurableInboundFrom（RunFlow ctx 注入点，与 Attribution/ProjectionSink 同款通道）；cm.turnDurableInbound（runEventLoop 批次开始设置、迭代末/continue 清除）；MemoryPlugin.onEvent 重放同键落库（ErrDuplicateEventKey→幂等成功，projection 按 key 幂等）+ SetDurableKeySink 首次执行回写新键（agent.go 接线 bus.AppendDurableEventKeys）——重放去重证据链在主路径完整闭合；claimDurable EventKeys 越界防御；测试 plugin/memory_dedup_test.go 三场景（真实 FileSegmentStore 语义）全绿；待修：②Major 5（可降级：LLM client CheckRedirect 按跳校验 allowlist，或文档化限制+迁移说明）；③Warning 3（SwappableModel 转发循环 select ctx.Done）；④Minor 批已清（第三批）：recoveryStatusOf 只填空状态（Status 初始空，failed/skipped-nonempty 不被覆盖）/PublishContext durable+claim 保留消息 Role（system 事件重放不失角色）/writeEnvelopeFile 目录 sync 失败显式报错/flock 用 syscall 具名常量+本地盘前提注记/feedback partial 语义任一丢失即全量重查提示。**Minor 2（orphan 同内容重复计数）经核实不修**：completeOrphanCommit 无法区分「完整提交后误用重复」与「barrier-fail 孤儿补齐」——heal 计数正确性优先（TestBarrierFailure 锁定该语义），重复提交的计数虚增仅在 API 误用下发生且查询幂等不受影响，登记为语义澄清。

## 7. WP6：综合验证与长期证据（F10，发布前置）

准入：WP1–WP5；不把本节待执行任务计入本轮通过项。

- [ ] 7.1 重构 soak 骨架为独立子进程 write/terminate/reopen，禁用进程共享 registry 捷径；真实 localfile、默认 fsync，强制至少一次 compaction，检查进程 ID 与恢复实例不同。
- [ ] 7.2 建 30 轮快速 E2E：逐个 accepted ID 对账、store→projection→模型实际请求→recall→宿主投递决定；测试 Content 不等于 Summary、无锚/有锚/TTL 失效/partial。
- [ ] 7.3 任务完整链：spawn Origin→事实记录→新 registry→R3 detector 绑定→watch/settle→反馈→宿主投递门；覆盖 nil 恢复、unknown 来源、service/job、stale/deadline、迟到信号与 resume 换绑。
- [ ] 7.4 governance/evolution/meditation 开关组合回归：证据不可用时明确 unavailable/insufficient，evaluation 到 digest/status 可见；无用户新颖性不自馈电，无审批不执行 critical，无框架自动 revert。
- [ ] 7.5 扩展 CI 为根与 bot 双模块、memory 子包/plugin/rl 的 race、Python 验证器负例、OpenSpec strict；直接底层测试状态为准，不让 wrapper 掩盖失败。
- [ ] 7.6 增加事件协议/键解析/压缩投影的有界 fuzz 与错误注入；保存种子、失败夹具和重放命令；检查 lastEventKeys 淘汰后的因果语义，不将 map 封顶等同完整性已证。
- [ ] 7.7 在获准隔离机器执行 72h 长跑，覆盖并发发布、磁盘拒写、模型/MCP mock 故障、慢消费者、热更和周期重启；按 design D7 输出资源曲线、事件对账与零静默失败结论。
- [ ] 7.8 如获真实模型授权，运行既有契约矩阵与限定预算的真实模型样本；记录模型/配置/失败定性，未授权明确 SKIP，不能把 SKIP 记为 PASS。
- [ ] 7.9 对实际实施 diff 完成独立代码审查，覆盖设计不变量与实现层；必须修复级问题清零，同一门两轮仍失败则 BLOCKED 升级；代理不可用明确记录。
- [ ] 7.10 汇总双模块 build/vet/short、定向 race、E2E、长跑、基准结果与上游豁免，形成按能力逐项准出表；不因部分通过声称回归全部完成。

## 8. WP7：文档、迁移与发布候选（F11）

准入：草案在 WP0 后；准出依赖 WP6。所有外部发布动作另行授权。

- [ ] 8.1 同步 README 中英文和相关 wiki：turn 间事件驱动/turn 内 ReAct、类型 TTL、按票据诚实 miss、时间窗压实、进程重启与整机重启差异、volatile/durable/processed/delivered 边界。
- [ ] 8.2 基于真实引用清理 TypeToolUse 残留与过时注释；核验 invBus/modelref 等候选后仅删除确认不可达项，公开兼容标识不得无依据删除。
- [ ] 8.3 对 WP3 已完成的 owner/执行代职责提炼做可维护性复评，登记剩余大文件的具体耦合/重复及独立重构候选；本包不再改核心代码，以免使 WP6 长跑证据失效，不按行数目标新增抽象。
- [ ] 8.4 编写升级/回滚演练步骤：旧 spill 排空、新 inbox-v1 回滚条件、目录锁、配置冲突、分区冲突只读诊断、HTTP 限额与端点 allowlist；本地夹具演练，不操作真实部署。
- [ ] 8.5 完成“headline→配置→实现→测试→证据边界”对照表，记录未验/豁免/上游阻塞及可用部署范围；准备 30 天受控部署观察清单，不将其当作已完成证据。
- [ ] 8.6 执行 `openspec validate resident-readiness-plan --strict` 与 `scripts/check-openspec.sh`；确认任务状态真实，按正常 archive 同步主 spec，不 skip-specs/no-validate；生成发布候选 checklist，提交/推送/tag/部署仍等待授权。

## 9. 执行记录与新发现

- 实施进度（2026-09-18）：WP0 6/6、WP1 10/10、WP2 11/11、WP3 9/10（4.7 部分完成）——共 36/67。
- **N-S1（事故）**：修 4.7 introspection 时误执行 `git checkout agent/context_manager.go` 抹掉未提交修改——已按会话记录重放（recovery 字段 + persistBusEvent dedup/回写）并全量回归。教训：多轮未提交工作严禁对已改文件 git checkout，回滚前 stash/diff 备份。
- **N-S2 已收口（2026-09-18）**：4.7 introspection「盲区」实为测试断言对象错误（keep 值写在 sub1，断言读了 entry）——cc.keepRecent 由 NewContextCompressor 第 6 参正确初始化，通路本就贯通。`TestHotReload_FieldDeletionFallsBackToDefault` 已转 PASS（断言对象修正为 sub1）。4.7 勾选。
- 当前：提案阶段，未执行上述实施任务；已完成的只读核验见 assessment。
- 每个新发现记录：触发场景、源码/运行证据、根因、受影响消费者、是否触及真源/执行权/默认态、对应设计修订与新增编号任务。
- BLOCKED/DEGRADED 项保持未勾选并写原因、依赖、解除条件；不能用归档抹去未完成项。
- 性能优化、真实部署和发布动作若扩出当前授权，先请求方向；停止/继续条件以 design D7 与发布证据 spec 为准。
