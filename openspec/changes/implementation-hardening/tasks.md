# implementation-hardening — 任务（细化版）

> 实施序：WP0 → WP1 → WP2 → WP3 → WP5 → WP4 → WP6 → WP7 → WP8 → 收尾。每包一 commit、回归绿才勾选。
> 侦察定谳全表见 design.md「侦察定谳汇总（R1-R28）」——实施遇与定谳冲突时停下上报，勿臆测强行。

## 执行守则（防跑偏总纲）

**停下上报四种情形**（先停，呈证据与选项，不得自行扩权）：
1. 实施中发现与 proposal V1-V14 / design 定谳冲突的事实（例：某「死码」实有调用方）；
2. 修复引出设计级权衡（例：nil 守卫与既有语义冲突需改契约）；
3. fail-before 测试无法构造出红（说明根因判断有误）；
4. 需要改上游 trpc-agent-go 或新增第三方依赖。

**禁做清单**：不顺手重构任务外代码（含格式化无关文件）；不加新依赖；不动 Non-Goals（子 agent store 漂移 / 治理匹配强化 / token 计量 / exec 超时 F-6 F-8）；不新增子系统或配置面；除 9.2 外不动 dev 分支。

**纪律**：修 bug 必先红后绿（fail-before）；每 WP 完成即跑该包回归命令并全绿才许勾选、才许开下一包；commit message 引任务号（如 `fix(task): 1.2 …`）。

**测试构造已知路**：TaskManager 直调即可重建任务（RestoreTask 唯一生产调用方 task_record_sink.go:272，测试无须走它）；zombie 测试风格参照 agent/task/task_zombie_reconcile_test.go。

## 0. 深钻定谳（四项全部完成于侦察+探索两轮，结论见 design.md R/N 两表）

- [x] 0.1 冷分区：**定谳=证实**（Init() 空函数；TTL/容量扫描正是 `store.partitions.Range`（lifecycle.go:132/199）遍历仅含本进程写入分区的 sync.Map）→ 修复入 2.4（N5 机制已定）
- [x] 0.2 StopLoop→StartLoop：**定谳=证实（V15）**——StartLoop goroutine defer `close(ta.outputCh)`（lifecycle.go:180）+ 成员不重建：重启后消费者读已关通道、二次 Stop close 已关通道 panic 逃逸 recover；冥想无恙（:186 随 StartLoop 重启，N2）→ 修复入 1.6
- [x] 0.3 spec 线性漂移：**定谳=漂移证实（V16/N3）**——spec:24-30 线性边界 vs 代码指数 {k,2k,4k} → 修正入 7.2
- [x] 0.4 recall items：**定谳=证实**（真身 memory_recall.go:84/97，非 recall_subtools）→ 修正入 3.4（锚点已正，N11）

## 1. WP1 裂缝模式级收口（正确性）

- [x] 1.1 fail-before：`TestResume_RestoredTaskNilWatchDone`——构造已定谳（N4）：RestoreTask(status=TaskStable（合法源四态之一，:864）) + Spec.ResumeFn 返回新 detector（nil 则 :874 提前报错够不到 :900）→ Resume，先证 `close(nil)` panic【锚点 task_manager.go:900】。**红不了即停（守则 3）**
- [x] 1.2 修复：RestoreTask 补 `watchDone/firstSettle: make(...)`（detector 保持 nil，设计语义）；Resume 换代判定改 `task.detector == nil || detector != task.detector`【锚点 :899】
- [x] 1.3 nil detector 守卫：Spawn select【:449】与 Resume select【:918】的 `detector.Detached()` 分支加 `if detector != nil`（nil → 只等 firstSettle，与 :443 注释「纯同步」对齐）；watch()【:460】对 nil detector 直接 return。**坑**：nil-detach-channel 与 nil-interface 是两态，注释里写清（注释宣称的 nil 支持实为 nil Detached channel 形态）
- [x] 1.4 KeepRecentTasks：SmartCompressor.Compress 加 `CompressOptions{KeepRecentTasks int}` 参数（**自有具体类型，无接口契约，R28 已证**），删 context_compressor.go:333-335 暂存-改写-defer；全调用方仅 :337 一处（R4 已证）+ 测试迁移
- [ ] 1.5 模式级审计：全库 grep `close(` 裸调 + detector/Closer 接口方法裸调，产出清单（file:line+处置）入 LEDGER；同型者修或豁免（豁免写不可达论证）
- [x] 1.6 V15 重启修复：方案 A——StartLoop 每次重建 `ta.outputCh = make(chan *event.Event, cap)`（goroutine defer close 当次通道；旧消费者已收 close 终态）；**先 grep ta.outputCh 全部读写点**（若有成员快照/其他写入方需一并梳理）+ fail-before e2e（Start→Stop→Start→Inject 断言消费；Start→Stop→Start→Stop 断言无二次 close panic）+ 冥想重启行为锁定（N2）
- [x] 1.7 回归门：`go build ./... && go vet ./... && go test ./agent/... -short -count=1` 全绿

## 2. WP2 耐久收口

- [ ] 2.1 fsync：appendWALLocked【local_file_kv.go:210，Flush 后】加 `f.Sync()`；snapshot 写后目录 Sync（best-effort）；`NewLocalFileKV` 加 `WithFSync(bool)` 选项；MemoryConfig 加 `FSync *bool`（**nil 归一 true——禁用裸 bool+omitempty，零值歧义坑**）管道 config.go:393 → wiring.go:287；fsync=false 时启动一次性 WARN
- [ ] 2.2 durability 测试：补「KVPut→Sync→不 Close 直接弃置（模拟崩溃）→ 新实例读回」；fsync=false 回归；**坑**：页缓存语义使单测无法真证掉电不丢——测试断言「Sync 路径调用了 f.Sync」（可注入 file 以 spy），掉电语义以代码结构保证并在测试注释声明
- [ ] 2.3 量测：基准写 N=1000 事件（fsync on/off 各一轮），耗时差记 LEDGER
- [ ] 2.4 冷分区（N5 机制）：LocalFileKV 加 `ListPartitionIDs() []int`（扫内存 data map `^(\d+):meta:` 前缀，实例方法**不扩 KVStore 六方法接口**）；FileSegmentStore.Init()（现空函数，segment_store.go:168-176）以类型断言 `if lp, ok := s.kv.(interface{ ListPartitionIDs() []int }); ok` 消费——启动时把持久化分区注册进 partitions sync.Map（含 PartitionState 从 KV 恢复 seqCounter，D12 路径既有）；rustviking 后端无枚举能力则记已知限制入 LEDGER；测试：「写入→新进程重开（Init）→TTL 到期→旧分区事件被遗忘」；eventCount 恢复或文档化放弃
- [ ] 2.5 回归门：`go test ./memory/... -short -count=1` 全绿

## 3. WP3 安全收口（按 D2 修正后 API 层方案）

- [ ] 3.1 token 中枢：HTTPAPI 加 `SetAuthToken` + ServeHTTP 顶部【rl/http_api.go:80 起】单一强制点验 Bearer（401，无副作用）；`rl.AuthTokenFromEnv()` 助手；**坑**：诊断/健康类端点若有豁免需求——本变更不豁免任何端点（fail-closed 一致性）
- [ ] 3.2 监听守卫：`rl.ValidateListenAddr(addr, token) error`（非 loopback 且无 token → 错误列三出路）；测试：0.0.0.0 拒 / 127.0.0.1 过 / 有 token 任意地址过
- [ ] 3.3 wechat-bot 接线【main.go:208/235】：读 `TAGENT_RL_AUTH_TOKEN` → SetAuthToken；无 token 时监听改 `127.0.0.1:port` 并 WARN 指引（**现状 `:port` 全接口，fail-closed 直接打破——此任务即迁移本体**）；部署 README 同步
- [ ] 3.4 recall items 上限【memory_recall.go:84→97 recallByItems（N11 已正）】：钳制（对齐 engine 路径量级，如 50）+ 截断说明（丢弃计数+建议）；测试覆盖超量输入
- [ ] 3.5 回归门：`go test ./rl/ ./tool/recall/ -short -count=1` + wechat-bot 模块 `go build ./...` 全绿

## 4. WP5 死代码二清（先于 WP4——不给死码做回收）

- [ ] 4.1 删 TypeToolUse + NewToolUseEvent + :27/:36 注释段【event_bus.go:58-91；registry 无条目 R12 已证】；头注释改写实：「turn 间事件邮箱 + turn 内框架原生 ReAct」；**坑**：测试文件引用须同步清理（grep _test 全量）；删后 grep 归零验证
- [ ] 4.2 删 modelref.go BuildDirectRequest/CallDirectModel/errDirectCall（**删前再 grep 一遍调用面归零——守则 1 防线**）；FoldModelRefAliases 保留（config.go:718 在用）
- [ ] 4.3 IsTmuxAvailable 改 `exec.LookPath("tmux")` 真探测【action_tool.go:751】（**独立于 NewTmuxExecutor 构造——勿再用 `!= nil` 判定**）；:176 降级分支语义测试（PATH 置空场景 t.Setenv）
- [ ] 4.4 回归门：staticcheck 全量零输出 + `go vet ./...` + 全包 -short 绿

## 5. WP4 资源与触发面收口

- [ ] 5.1 旧 runner 延迟 Close（N7 细节 + D5 定时兜底）：ContextManager 加 retired 列表 + 全局 in-flight 计数（RunFlow 入口 inc/defer dec，归零时扫描 retired 逐个 `io.Closer` 断言 Close，幂等 once）；**另配年龄阈值定时兜底清扫**（如换代后 10 分钟仍因持续负载未归零则强制 Close——补持续负载软点）；tagent.go 换代处把跌出 ring-2 的 old 调 `cm.RetireRunner(old)`（ring-2 在 reload 闭包 prevKeep/prevSnapshot，tagent.go:323-328，N10）；测试：三代热更断言第一代 Close 恰一次、ring 内不关；另测「持续 in-flight 下兜底清扫仍回收」
- [ ] 5.2 SwappableModel 同型：GenerateContent 计数包裹，Swap 换下归零后断言式 Close【swappable_model.go:35-47】；**坑**：in-flight 期间 Swap 多次——只追记「待关列表」，勿假设一代
- [ ] 5.3 lastEventKeys 封顶 4096：超限按 value（int64 单调）淘汰最旧【memory_plugin.go:36/208】；测试：超限修剪 + 因果链 parentKey 正确性保持
- [ ] 5.4 Rollback 手动触发面（N10：核心零改动）：wechat-bot main.go 宿主侧接 SIGUSR2【:241 现有 signal.NotifyContext 处扩展】→ 调已有 `ta.Rollback()`（task_record_sink.go:125）；e2e：触发→断言换回上一代（日志指纹）
- [ ] 5.5 回归门：`go test ./agent/... ./rl/ ./plugin/ -short -count=1` 全绿

## 6. WP6 配置健壮性

- [ ] 6.1 strict（两处一点）：抽共享 `strictDecode(data, out)` 助手（**一实现两调用点**，L3 单点），config.go:882 与 tool/mcp/registry.go:274（R2 已证）同步改接；助手内部 `yaml.NewDecoder + KnownFields(true)`，错误列全部未知字段；测试：拼错键报错并列名；**另附弃用流程注**（strict 抬高了 schema 演化税：字段改名/删除自此为显式破坏性变更，弃用流程写入 config.go 头注——两版本重叠期后再删）
- [ ] 6.2 环检测：buildAgent 递归加 visited（A↔B/自引用两形态报错指名）；测试覆盖两形态
- [ ] 6.3 迁移验证：仓库内全部随载 yaml（resources/examples/tests + wechat-bot 独立模块）逐一过 strict；**坑**：wechat-bot 生产 yaml 可能含已 Deprecated 但仍合法的别名字段——它们在 struct 内不会报错；报错的是真未知字段，逐个修正或（若属拼写）上报
- [ ] 6.4 回归门：根包 + `cd examples/wechat-bot && go build ./... && go test ./... -short` 全绿

## 7. WP7 文档对齐

- [ ] 7.1 README【:3/:167/:253 三处】：「永久入库/永久存储」→「不可变入库 + 默认按类型 TTL（3-30 天）+ 可配永久」；配置永久之法（TTLDays -1 豁免）在配置参考可达
- [ ] 7.2 compaction.go:19-24 化石注释改真（L3=低价值类型清空 Content，无 gzip）；**deterministic-compress-level spec:24-30 定改**（V16 已证：线性边界→指数 {k,2k,4k}，与 smart_compress.go:148-153 对齐）
- [ ] 7.3 guardrail 耦合声明：evolve.go:218 评估构建处，治理关闭时附 `governance disabled: denial/critical signals unavailable`（判定来源：治理 Gate 是否接线——经 BindRuntime 注入态判，勿靠事件计数推断）；wiki 平台篇同步
- [ ] 7.4 wiki 复核：README/wiki 与三处行为变化（fsync 默认/strict/RL 认证）一致；三项评审反证（invBus/namedStores/behavior-matrix）与 V15 翻案过程入 LEDGER 留档（含 R15 误判教训：反证须穷尽读写两侧）
- [ ] 7.5 docs 门：仓库文档检查脚本（若有）通过

## 7A. WP9 架构防呆立法（v0.1.0 冻结前置；D11 路由表之入案四件）

- [ ] 7A.1 分层断言测试（L2）：新增 `arch_layers_test.go`（根包或 tests/）——`go list -deps` 枚举传递依赖，断言：memory/plugin 及子包不引 agent/根包；event 不引任何其他内部包；agent 及子包不引根包；现状已核验全绿（L1 核验 2026-09-14），测试为纯新增固化，未来违例即红；测试注释标 L2 梯级与 spec 条目引用
- [ ] 7A.2 上游假设钉（L1）：invariants_test.go 补 `TestI2_BeforeModelCompleteness_RealPipeline`——**走真实上游管线**（非 mock 插件序列）：事件经真实 plugin pipeline 落库后，BeforeModel 渲染包含全部先前已存储事件；钉的假设（插件管线在 tool-result 事件上同步等待完成）写入测试头注；若上游行为已变（测试红），停下上报（守则 1）而非改测试迁就
- [ ] 7A.3 红色耦合台账：LEDGER 新节——上游内部假设清单（插件管线同步等待/BeforeModel 时序/session service 行为等，逐项标 7A.2 钉测或豁免+论证）+ 隐式耦合清单（governance→evolution 信号、分区哈希碰撞面）；与 8.1 的 race 豁免清单交叉引用
- [ ] 7A.4 回归门：`go test . ./agent/ -short -count=1 -run 'TestArch|TestI2'` 全绿（含新钉测）

## 8. WP8 战略收尾（前置：WP1-7 与 7A 全绿）

- [ ] 8.1 V17 分类处置（探索轮已取全栈，/tmp/race 探针方法可重现）：A 类（上游 inmemory session service/steer 关闭）→豁免清单+向 trpc-agent-go 报 issue 附栈证据；B 类（loopMockTool.getCallCount 测试 mock）→测试侧加锁修复；审计 F-4「Session.Clone」描述在 LEDGER 更正为真身；**不为过门禁改生产码**
- [ ] 8.2 race 门禁扩面：ci.yml:52 命令加 `./agent/`（:48-50 注释同步改写为现状）；本地 `go test ./agent/ -race -count=1` 通过（或仅剩已豁免项）
- [ ] 8.3 soak 骨架：tests/soak_test.go（build tag `soak`，默认跳过）：N 轮「写事件→压缩→票据召回→关进程重开→投影重建断言逐字节」，N 与数据量参数化；CI workflow_dispatch 手动 job
- [ ] 8.4 全量回归：`go build ./... && go test ./... -short -count=1 && staticcheck ./...` + ci.yml 六组 race 等价本地跑
- [ ] 8.5 LEDGER 回写：本变更台账（0 批定谳结论、2.3 开销数据、8.1 栈定位与处置、三项反证与 V15 翻案、**7A.3 红色耦合台账**、D11 立法留痕与缓行项 backlog 坐标）；另注「v0.2.0 前重跑 maintainability-audit」（再审节奏首锚点）
- [ ] 8.6 v0.1.0：CHANGELOG 定稿（含三处行为变化的迁移说明 + 立法三件与准绳）→ 打 tag 推送

## 9. 收尾

- [ ] 9.1 `openspec validate implementation-hardening --strict` 通过
- [ ] 9.2 双分支：全部提交 main 并推送；dev cherry-pick 可行则同步、冲突则留待合并继承（同 848b412 先例）并注明
- [ ] 9.3 邮件通知远端：变更清单 + v0.1.0 + 部署注意（strict yaml 迁移、RL token/loopback 二选一、fsync 默认开）
