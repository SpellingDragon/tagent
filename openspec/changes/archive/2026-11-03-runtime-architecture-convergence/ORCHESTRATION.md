# runtime-architecture-convergence 编排文档

## 一、结论

抵达 DoD 的唯一路径已收敛为：**删除性归一**（并引擎、立自驱法则、删内存权威）→ **概念净减**（D6 公共导出面相对 dfdfa72e 净减 + 4.4 内化四公共面）→ **提速收官**（CI 分片去重 + F1–F4 + 新审者 frontier 空）。当前最大风险：继续为正确性叶子做加法（窗口/缓存恢复/旁路机制）而推迟删除——这正是本案最初症状“越改越多”的复发形态。

## 二、编排四问

### 2.1 次序

**S 簇终局矩阵（r41 重写；流动并发 P1–P4 不变：写集簇=派发原子、无栅栏、一叶一锚、矩阵未核不派）**

| 簇 | 内容（吸收旧叶） | 写集 | 前置 |
|---|---|---|---|
| S1 引擎归一 | Drive 并入 Consumer；相关事实自驱法则（吸收 02/2.7 与三个特例）；链为答案唯一权威（读透缓存，删恢复机制：叶B 分类/叶C 重建只保留链读）；3.4 关闭快照覆盖 | `runtime/agent/{loop.go,consumer.go,*_test.go}`、`assembly/{invoke.go,run.go}` | W2-07、叶C 完 |
| S2 消息余项 | 2.5 续行不依赖内存 continuation；2.6 两族投递同构 | `assembly/{tools.go,host_tools.go,host_delivery.go}`+新测试 | 与 S1 的 invoke.go 交接后 |
| S3 关闭收敛 | 3.3 合并双关闭循环+串行化；3.5 收缩 CloseRequest/DrainReport；**删除 retainSettleDelivery**（W2-05 债，Quiesce offerOwed 替代） | `assembly/{host.go,settle_turn.go,run.go}` | S1 后 |
| S4 概念净减 | 4.3 三规范收缩；4.4 内化/删除 Writers/Movement/Continuations/PresentedFace；4.5 D6 净账发布（放行硬门） | specs+广泛浅改（届时列精确清单） | S1–S3 稳定后独占 |
| S5 CI 提速 | 5.3 repeat-index；5.4 十分片；5.5 去重；5.7 race 全第一方 | `scripts/scenarios/`、workflows、race spec | 无（可全程并行） |
| S6 压缩余项 | 6.3 A/B 数据；6.4 测量不扩容；6.5 随 04 退并行权威 | `memory/replay/`+specs | 6.2 已完，随时 |
| 编排者单写 | 1.4 规格同步；C 类三条归口；check-openspec.sh cwd 修复 | specs/脚本 | 叶C 后 / 随时 |
| F 收尾 | F1 契约零改动；F2 全量门禁；F3 症状锚红转绿双证；F4 frontier 空+archive | 只读+归档 | 全簇完 |

**红线（恢复 pr36 原契约）**：任何新窗口/轮询/旁路机制/新公共名字 → 停线交用户；同一机制三次修复仍出新关键缺陷 → 停线。放行=双门（行为门+概念净减门）+frontier 空，不以测试绿单独放行。

（历史波次矩阵保留为凭据索引）

**全波次流动并发策略（r35 裁定，替代逐波栅栏）**

- **P1 派发原子=写集簇**：共享任一文件的叶合并为一个派发单元（如 03/3.3+D-W5-A 同簇合单），单元内按依赖串行、单元间写集两两不相交即并发。消除“同簇叶分派导致的隐式等待”。
- **P2 无栅栏流动**：任一单元完成→其写集立即释放→依赖该单元的下一单元即刻派发；不设波次边界、不等批次齐。（R3 的正确形态）
- **P3 粒度规则**：一叶=一个公共红锚→绿+一个最小实现+包级回归，可在一个代理会话内完成；粗叶（如 1.3、3.3）拆到满足本规则。
- **P4 完整矩阵**：读六域 tasks.md 全部剩余叶，产出 叶→写集→依赖→锚 矩阵，按簇重组后即成流动派发队列；矩阵未核实前不派该叶。

（以下历史波次矩阵保留为已完成单的凭据索引）

唯一实施基线为 `dfdfa72e70d2cdd91ce8b1db408d5543481dc499`，唯一实施树为 `/Users/pengweiye/Documents/codes/tagent/.quiesce`。主工作区 HEAD、origin/main 与 dirty patch 只作对照；所有脏 hunk 均为逐项候选，不得在实施树上假定存在。

**Wave 3 并发矩阵（R21：写集两两不相交即可并发；同包测试读取不冲突，`go test` 各自运行）**

**Wave 4 并发矩阵**（W3-01 完成解锁；W2-05/W2-06 在途不冲突）

| 派发单 | 孙任务 | 状态 | 精确写集 |
|---|---|---|---|
| D-W3-02 | 01/1.3剩余 WaitAnswer 从 input_events 重建 | ready | `runtime/agent/{consumer.go,consumer_test.go,loop_test.go}` |
| D-W2-07 | 02/2.4 通知恢复点阵 | ready | `assembly/{facts.go,settle_turn.go,host.go}`、`tests/integration/host/notice_restore_lattice_test.go`(新) |
| D-W4-02 | 04/4.2 persistent-event-loop 残条迁移清偿 | ready | `openspec/specs/persistent-event-loop/spec.md`、`changes/04-domain-contracts/` 四件套 |
| D-W4-03 | 04 mutation 生产者补基线字段 | ready | `scripts/gates/{check_event_integrity_mutations.py,test_check_event_integrity_mutations.py}` |

串行约束：03/3.3（合并双关闭循环）与 D-W2-07 同文件簇（settle_turn/host），排 Wave 5；01/1.4 依赖 W3-02；02/2.5、2.6、03/3.4、3.5、05/5.3–5.5、06/6.3–6.5 写集待读域四件套核实后入 Wave 5。

| 派发单 | 孙任务 | 状态 | 精确写集 |
|---|---|---|---|
| D-W3-01 | 03/3.1+3.2 关闭预算 join/假 Quiesced | running | `runtime/agent/{consumer.go,loop.go}`、`assembly/{invoke.go,host.go}`、`tests/integration/host/close_refuses_while_model_in_flight_test.go`、`tests/integration/execution/send_cross_session_gates_test.go` |
| D-W2-05 | 02/2.2 settle fact 提交参与 Accepted | ready | `assembly/tools.go`、`tests/integration/host/settle_delivery_pending_test.go`(新) |
| D-W2-06 | 02/2.3 已提交重入不反向否认 | ready | `assembly/host_tools.go`、`tool/subagent/continuation.go`、`runtime/task/reactivate.go`、`tests/integration/host/reentry_no_retroactive_denial_test.go`(新) |
| D-W4-01 | 04/4.1 domain-surface 概念门 | ready | `scripts/gates/{check_concept_budget.py,test_check_concept_budget.py}`、`golden/runtime/architecture.json` |
| D-W5-01 | 05/5.1 证据等集/同基线负锚 | ready | `scripts/gates/{check_runtime_evidence.py,test_check_runtime_evidence.py}` |
| D-W5-02 | 05/5.2 PR stale 取消策略 | ready | `.github/workflows/{ci.yml,runtime-contracts.yml}`、`scripts/gates/test_check_runtime_workflow.py` |
| D-W6-01 | 06/6.1 压缩 request 因果 | ready | `assembly/age_fold.go`、`tests/integration/host/compress_request_causality_test.go`(新) |
| D-W6-02 | 06/6.2 Volume 门+候选门+删 forced | ready | `memory/replay/{age_plan.go,age_segments.go}`、`memory/replay/fold_capacity_rules_test.go`(新) |

互斥保留 blocked：01/1.3 剩余（consumer.go 与 W3-01 冲突）；02/2.4（host.go 冲突）；03/3.3–3.5（依赖 W3-01）；01/1.4（依赖 1.3）；05/5.3+（依赖 5.1）；06/6.3+（依赖 6.2）。新增测试独立命名，禁止共文件。全量/长测、commit/push/rebase/archive 仍禁止。

### 2.2 授权

- Wave 3 各单均为“先红后绿”：先写公共红锚并复现（原始退出码入汇报），再最小实现转绿；一次一个测试与一个实现，禁止顺带重构。
- 各单写权限于上表精确写集；跨单文件=越权，发现即停。同包他单测试只读运行允许。
- 公共 seam 是正式 `Host → 模型可见 send → 目标地址 Consumer → 公开事实/模型观察`。既有 `pr36-concept-convergence` 计划已记录用户确认正式公共入口缝；该草案将保留为一级变更 evidence，不作为第二派发权威。
- 禁止修改主工作区脏文件、git index、依赖、主 specs 或其他叶文件；禁止 git commit/checkout；过程件只放 `/tmp/runtime-architecture-convergence/W1-01/`。
- 根账目、ORCHESTRATION、主 specs、CI workflows 与跨域裁定归编排者单写。
- `runtime-simplicity-upgrade`、`event-driven-consumption`、`post-training-data-loop` 的相关实现/规格写入暂停；`success-semantics` 已落地代码只作基线。
- 任何新增公共类型、扩大文件面、需要 rebase/提交或同机制第三次修复仍失败，均停线上报。

### 2.3 凭据

D-W1-01 必须按顺序提供：

1. 红：`go test ./tests/integration/host -run '^TestSendIdleTargetUsesResidentConsumer$' -count=1 -v`，原始退出码非 0，且输出证明测试实际运行而非零匹配；失败原因必须是 send-derived 直驱而非夹具错误。
2. 绿：最小修改后复跑同一命令，原始退出码 0，静止后目标输入与输出各恰一条，输出属于目标 Consumer 的 segment/range；`TestSendMountsBoundIdleConsumer` 覆盖已绑定未挂载目标，`TestIdleSendReceiptDoesNotWaitForTargetAnswer` 用 parked 模型证明回执不等待回答。
3. 负锚：`go test ./tests/integration/host -run '^TestSendDrivesTargetTurn/duplicate_delivery_idempotent$' -count=1 -v`，原始退出码 0，重复身份不新增事实或第二次有效驱动；停止回归 `go test ./runtime/agent -run '^TestStoppedResidentsRejectDelegatedWork$' -count=1 -v` 须先在当前实现红、修复后绿，公共 SubmitDelegated 返回 ErrDelegatedInstanceUnavailable，不得读取私有注册表；`TestConsumerSetDoesNotReplaceMountedAddress` 用可控 HistoryFace 交错证明地址挂载后集合不静默替换；停止当前集合后公共 SubmitDelegated 具名拒绝，不得由另一集合复活。
4. 生产消费者锚：红绿均经正式 Host/模型工具 send，不得直接调用 `hostMeditationPort.deliver`、`Loop.Drive` 或手工构造 Consumer 结果。
5. 写后核查：实际 diff 仅限五个授权文件；`go test` 与编辑不得并行；记录 go test 的 pass/run 计数，不能用管道遮蔽退出码。

该 pilot 只证明空闲 send 唯一驱动，不证明 Run.Invoke/InjectEvent、答案淘汰、通知恢复、关闭、CI 或压缩已收敛。

### 2.4 释放

| 资源 | 占用者 | 释放条件 | 等待表 |
|---|---|---|---|
| assembly/host_tools.go、runtime/agent/loop.go 与公共 send 测试写权 | D-W1-01 | 红绿、负锚、diff 与原始退出码全部核验 | 仅释放给新鲜独立复审；复审通过后才重冻下一叶 |
| 其余 32 叶 | 无 | pilot 未过不得占用 | 保持 blocked；merged-spec preview 和三锚修复可在主规划树只读准备，但不派发实现 |
| 外部 merge queue | 未启用 | 仓库规则与真实 merge_group run 到齐 | 5.6 继续 blocked |

## 三、明细

- 当前派发原子仅 1.1，不设多agent写并发；它的单叶拆分理由是从用户症状直通唯一直驱缺陷，能杀死旧实现且不需要先建设其他域。
- 预算：定向 Go 测试，禁止全仓/长测、付费服务、依赖安装；复现过程件只放上述 /tmp。
- merged-spec preview：后续每个 capability 必须逐条决定 ADDED/MODIFIED/REMOVED，保留或显式裁定每个既有 Scenario；不得提前手改主 specs。嵌套 delta 需用实际读取它们的校验/预览，不得用根 change validate 冒充。
- Python 验证必须使用 unittest 可发现的 `test_*` 全名与精确 `python3 -m unittest module.Class.test_name` 命令；禁止 TestCI/TestEvent 形式。纯文档/暂缓叶不伪造红绿。
- 一级账目、二级四件套与计划回写只改规划树；实施树只改 D-W1-01 的五个授权文件。

## 四、台账

| 轮次 | 情报/裁定 | 凭据 | 后续 |
|---|---|---|---|
| r0 | 建立一级变更与六域只读 W0；当前 PR/工作树仅作证据 | OpenSpec 初始化成功；前序独立审计显示 PR 仍有并行驱动/关闭语义 | 派发 W0-A01…A06 |
| r1 | 六个 fresh 只读审者已派发；裁定既有 runtime-simplicity-upgrade/event-driven-consumption 不得并行实施，success-semantics 已落地代码只作基线 | OpenSpec 4/4 且 strict valid；六单写入白名单均为空 | 等待六份报告，逐条亲核后生成二级四件套 |
| r2-A01 | 采纳 F1–F7：所有正式入口经 Consumer；答案可答性由事实关系保证；并发同段等待者共享结果；segment 私有/Turn 为结果 | 审者具名行号；lead 另核 Run.InjectEvent 直驱，确认并非只有 send 旁路 | 01 四件套落地，1.1 为最小核心红绿探针 |
| r2-A05 | 采纳 Q1–Q7；证据等集先于分片，实际 merge queue/required checks 先于移长测；PR fast 目标 10m | 最近 20 run 中位 90m49s；short 17m20s；root 双跑可去 212s；GH 无可查 merge_group | 05 四件套落地；5.1/5.2 可作独立候选，5.6 外部资源门未开 |
| r2-A06 | 采纳 Q1–Q6；C1 成立：request commit 失败仍 Record/返回 nil | lead 亲核 assembly/age_fold.go:225-252 与 compression-pipeline 主 spec:9-23 | 06 四件套落地；6.1 正确性先于性能，Material 公共字段裁定删除 |
| r2-A02 | 采纳 Q1–Q5：send ok=输入事实提交；通知以消费结果+位置结清；已提交重入不反向否认；恢复不依赖内存 continuation | lead 亲核 tools.go:49-121、manager.go:145-186、tool.go:327-345；C1–C3 成立 | 02 四件套落地；通知失败结果不再默认无限重放，外发义务独立 |
| r2-A03 | 采纳 Q1–Q4；补充 Cut 后可重试、writer 存活资源不释放、最终 closed 才缓存 | W0-A03 证明 Close 隐式取消、无界 join、双循环及 Movement 写点反例 | 03 四件套落地；删除隐式 cut 与公共 Movement 是目标，不在其上加补丁 |
| r2-A04 | 采纳 Q1–Q5；形状预算与 domain-surface 分门，读面/事实/声明/端口分类，无符号 KPI | W0-A04 证明 superseded+SHALL 与形状门漏检；CompressProduct/DeliveryLedger 为正例 | 04 四件套落地；GLOSSARY 移除 segment 公共权威和事实族重复表，写入 send/通知/关闭定义 |
| r3 | 六域 W0 与二级规划完成；发现 .quiesce 已有同目标 pr36-concept-convergence 草案及公共 seam 确认，待合并为单一权威；新鲜规划审者已派发 | OpenSpec 4/4 strict valid；doc-refs 无悬空；33 叶必须先跑最小路径 | 等规划 frontier；随后整合 .quiesce，先执行 1.1 公共红锚，不批量实施 |
| r4a | pilot 红证后发现 WakeAddress 只 Wake 不 Ensure；为避免已绑定未挂载目标滞留，将既有 loop.go 纳入同一叶，复用 WakeAddress 内部 Ensure/Wake，不新增 API | 红测实际输出 #sent 直驱；读 loop.go:494-504 证明直接换 Wake 的回归风险 | 授权写集增至三文件，其他 32 叶仍 blocked |
| r6-W1-01 | 新鲜复审 frontier 非空：WakeAddress 在 StopResidents 后可新建集；已绑定未挂载与恰一答案未被锚住 | 审者复跑公共 race exit 0，但静态证明 StopResidents 后复活；不接受测试绿代替活性 | 增加公共 SubmitDelegated 停止红锚，其余叶继续 blocked |
| r8-W1-01 | 第二轮独立复审发现 SetConsumers 擦除终止标记、Wake/切换交错丢唤醒、链读后旧集合挂载；归为同一生命周期非原子，不逐点补丁 | 审者指定 race 命令均 exit 0，但给出确定性公共路径；同机制第二次返修 | 以私有 lifecycle 锁串行 Wake/Set/Stop，挂载后禁止替换，instanceFor 复查挂载权；下一轮同类关键问题则停线 |
| r158-淘汰窗口缺口修复 | 0e036b81 CI：race/mutations **success**（修复生效）、test 红=TestEvictionKeepsUnrecoverableAnswers（第六个 CI 负载敏感点）。根因=settleSegment 先 deliver 后 indexAnswerLocked（同 c.mu 临界区），测试读窗口时插入/淘汰在飞——确定性复现（TEMP DIAGNOSTIC sleep 拉开）修前红修后绿。修=测试等待硬化 awaitIndexedAnswer（order 可见即淘汰完成），同族 ttl ageAnswer 一并修。产品淘汰零改动、断言 want 零变动。亲验 count=10+单核 5 绿，推 660cf71c | 本地账目待 CI 绿随 complete 一起推 |
| r157-goal完成核对 | 六条核对（CI 绿即标 complete）：(1)规矩先行=156 轮零新窗口/轮询/旁路/公共名/事件类型，红线触发即停批；(2)删字诀=removed 10/interfaces 78→77/renames 0/棘轮常驻；(3)一条法则=命名唤醒统一四特例全删；(4)关闭即时间线=单预算+失败点名+失败事实驱动；(5)委派一条事件=W1 原子+血统键+子零感知；(6)数字验收=概念硬门+R35 frontier 空+CI 五层红清偿。三宪法：org 全热迁移/config_change 无第三条路/delegatedInputOf。终局三件：文档重写（r150）/归档（archive/2026-11-03）/squash main（29d9d214）。runtime-contracts 双 success；主 CI 排队（29d9d214 run 41697746806） | CI 全绿→update_goal complete |
| r156-doc结构修复推main | b1e877a3（race修复）引入 2 条 doc-not-name-prefixed（logSwapLock 块插进 consumerDebug doc 组中间）——重组：doc 还函数、锁块移到 debug/warn 两面之间。comment 0/concept ok/godoc ok/agent tests 绿，推 b44be143。CI 三 run 复跑中（b1e877a3 红已知、e94344a5/b44be143 待全绿） | CI 全绿→goal 完成核对 |
| r155-三遗留收口推main | 深挖单交付亲验绿推 e94344a5：(1) quiesce=测试时序缺口（follow-up 未 hold 在飞，CI 提前失败结算致空账）——场景 holdFollowUp 语义修（产品不变，-race count=5 绿）；(2) MUT-EVID-04=断言面不足（降级形态未计）——锚计全呈现形态+门禁精确字面量（11/11 killed exit 0）；(3) long32768=夹具混算（SMOKE-REFLECTION 回退业务 marker 重复计数）——夹具答 reflection received（产品 send/meditation 零改动，全量 660s PASS）。三处全测试侧、断言零放宽 | 等 CI 全绿（5.6/5.8 取证）→goal 完成核对 |
| r154-race修复+三遗留派单 | f1d721be CI 复跑：runtime-contracts **success**（注释 153→0 生效）、race 红（真竞态：watchDelegationLog 写 trpclog.Default vs consumerDebug 读，detector 非误报）——已修（logSwapLock RWMutex 统一 8 产品读点+2 测试替换，本地 -race count=3 绿）；test job 唯一失败=TestQuiesceCancelledCallerAnswersCut（本地多核+单核 count=5 全绿=CI 负载敏感：worker 通知归属期望欠账 Unsettled、CI 随失败段结清 Failed:#c2）；mutations/long(32768) 失败未明。durable_test 跨包替换登记观察 | 推 race 修复；派深挖单（quiesce 归属语义+mutations+long32768） |
| r153-CI红全清+推main | CI 红三层全清推 f1d721be：(1) 注释 153→0（两单 140+lead 补 tool/subagent 漏网 7+state/task 6+S4 红线误删恢复——注释单把 HistoryFace 红线格式化成 bullets 致 S4 红，段12合并留红线段双门齐绿）；(2) 等待硬化 7 处（直驱删除=执行异步受理，探针证实 200×AdmissionReceipt）；(3) settle 重复驱动根因修复（批 whole 计数=one follow-up，GOMAXPROCS=1 requests 89-183→2、results 500/500、actions=1；非批保持 fast_result 冻结判据）。全门禁绿（comment 0/concept ok/doc ok/141/141/godoc/build）+单核 execution 绿+全树零 FAIL | 等 CI 复跑（5.6/5.8 取证） |
| r152-GOMAXPROCS=1根因 | CI 红深挖：GOMAXPROCS=1 复现（scheduling floor=单核）暴露**直驱引擎删除的测试等待缺口**（工具执行已异步受理，dispatched 断言读执行数过早）——探针证实 dispatch 循环跑满 200 次全 AdmissionReceipt，awaitToolCalls/awaitRecorded 等待硬化 7 处后 5 测试红→2。剩余深因：storm 型 97 个 follow-up requests（设计=one follow-up decision 读全部 settles）+355 result facts=**settle 路径逐 result 重复驱动**，撞不重复驱动门，疑 W6 遗留（fireNoticeTurn AwaitTaskState 移交）显现。注释合规两单完成（140 findings 清零）+runtime/state,task 6 条遗留 | 派多驱动根因单；补 6 条注释+gen_godoc 归 lead |
| r151-CI红判责 | squash main 170ef267 后 runtime-contracts 红（comment_policy 产品包 153 findings，测量态要求 0）。判责：main 合并前 e7480c65 该门 success（0 findings）→ 153 条全由本批注释形态引入（设计叙述散文进了契约注释槽），本批责任本批修。分布：free-standing 59/test-doc-one-sentence 35/doc-not-brief 26/audit-marker 16/frag 8/name-prefixed 4/line-too-long 3/missing-test-responsibility 2。按包派两单（runtime/agent | assembly+api+memory）纯注释合规化+结构类处置，docs/api 重生成归 lead | 两单回收→gen_godoc→推 main 补丁→CI 复跑 |
| r150-归档+文档收编+推送 | openspec archive 完成（archive/2026-11-03-runtime-architecture-convergence，specs 同步 +50 编排行，141/141）。doc 棘轮等量收编：comment-gate-tooling 散文压缩（140→114）+ upgrade-rollback-drill 由 inbox-v1 历史 runbook 退役为演练口径存根（86→18，链接活）→ tighten 锁入。终门禁全家桶绿（概念/doc/specs/openspec/godoc/build），amend 为 dfd05442。推送 quiesce-criterion-batch → squash merge main（objective r23 授权）| 终局证据：CI 实跑（5.6/5.8 外部项）|
| r149-merge main | git 终局推进：单一提交 bb82a2d1（feat: converge the runtime onto one fact chain and one consumption loop，全量清单）→ merge origin/main（e7480c65）amend 为 388c174d。冲突解析：#32 与本支=同一稳定前缀工作的两系演化，凡冲突我方胜（request/loop/facts/intake/boundary 全 ours；自动合并偷换 facts.go 的旧 rangeOpen 被抓回）；main 收编=README 定位/CHANGELOG 双段/go.mod tagent.3。文档棘轮：删 PROJECT.md+config-migration.md（并入/过时，README 链接改指机制导览+事件流）+致谢段合并，doc_budget 三态 ok。合并树门禁速验全绿，全树 short 后台取证 | 下：归档→push→squash merge main |
| r148-补扫Minor收口 | R35 激活面补扫放行后两 Minor lead 亲收：判别锚 TestProjectionFoldsByTheWiredConsumptionRelation 3 绿+变异证刃（删接线→具名红→还原绿）；evidence/06 旧锚名改指父锚+子测；docs/api 重生成 --check 过。夹具教训顺证回退规则（meta=JSON 数组非字符串） | **git 终局序列启动：单一提交→merge main→push** |
| r147-F2终态绿 | F2 终态全树 -short 取证完成（A 接线后最终树，零 FAIL，40 包）——行为门最终证据落定。等 R35 激活面补扫后进 git 终局 | F1 已过（r139 五）；F3 台账已备（r90/r138）；F4 待序列尾 |
| r146-A接线生效 | lead 亲接生产线（mappedEntryOf→WithInputEvents 一行）+注记更新为已启用；四层亲验绿（replay 整包/host 压缩族/history/黄金门 14/14）。A 生产生效=活输出按所消费折叠、无关系事实保位置括号。R35 定向补扫派发（生产通路对齐/delegated 事实入关系/重启面/红线/锚抽查）；F2 终态全树后台取证 | 补扫绿后 git 终局序列 |
| r145-终裁A启用 | 用户终裁（覆盖 r144）：捡起 A 且生产启用。语义两层=链保持到达序（位置优先，账本记何时）；投影读关系折叠——有 input_events 按消费关系归段，无则到达序+位置折叠回退。与在途重放单实现/spec 措辞完全一致。执行=重放落地→lead 接 mappedEntryOf 一行→全量验证（折叠行为真实变化，压缩族锚按新语义迁移）→R35 激活面补扫→F2→git 终局。r141-r145 五轮摇摆完整入账=deliberation 的诚实记录 | 在途：重放单 |
| r144-终裁0残留 | 用户终裁：A 整体放下，0 残留——回 r142 纯净态（位置式+诚实注记+负向守卫）。裁定依据：A 从一行长出一串边（载体/WAL/delegated 不入关系/spec 措辞/休眠两态）=红线要拦的形状；账本（到达序）+关系（meta）+投影（位置式）三层分工即为终态。r143 重放单落地后 lead 按已两验的回退地图再清一次。升级配方仅存台账（红锚三例+实现地图，证据触发=摘要凝聚力可观测劣化）| 终态定型后 F2 重取→git 终局序列 |
| r143-保留A休眠 | 用户改裁：保留"到达序+读关系折叠（A，已实现休眠）"——r142 回退作废，按原单（dc5d35b3 回报）精确地图重放实施：replay 层支持关系闭合（直注条目可测）、生产不接线（mappedEntryOf 不喂关系）=休眠；升级路径=一行接线+R35 补扫。spec 保持翻转态（replay 层行为真实且有测） | 重放单派发；F2 全树证据终态重取 | 终局序列随后 |
| r142-r141撤销执行 | 用户终裁"不用改"；回退单（冻结令前在途）完成亲验：A 实现四文件零残留还原（顶层 TestSegmentsCloseAtAgentOutput 复位/负向守卫复位/141/141/压缩锚绿），唯一保留=4 行诚实注记（段按到达序归组，因果真相在链上 input_events，投影有意只读位置）。树回 R35 扫过形态。**A 升级路径完整存档**：红锚三例+实现地图在本行与 dc5d35b3 回报，证据触发条件=折叠摘要凝聚力可观测劣化 | 终局序列待用户放行：单一提交→merge main→push→F4 |
| r141-段边界裁定 | 用户令"一并做"：按实际消费关系闭合段在本 PR 落地（未落地宣称→活实现）。语义：agent_output 带 input_events→精确归段（其命名输入归该段，未命名输入滚入下段）；legacy 无 input_events→回退位置式闭合（向后兼容）。收口逻辑=input_events 因果链建了真相、投影边界读真相，位置判据在全双工/合批下会归错输入。r140 的拒绝采纳转为采纳并翻转刚加的负向守卫 | 派实现单 | Minor 批单仍在途 |
| r140-spec改写归档义务 | 4.3/6.5 spec 改写完成亲验（死符号 SHALL 清零/141/141/概念门 ok）：17 文件 A=20 迁指 B=46 退役（9 live 改指现行锚（含 request-budget-accounting 链 AgeFoldPlugin.ObserveCommit→CompressionConsumer.Execute→FoldByAge/compress.product）、7 整能力 superseded 先例化）；拒绝 event-driven-consumption 越权声明有据（splitSegments=agent_output 边界+负向守卫）——lead 批准。残余登记：6 存储生命周期 spec 的 Compactor=存储压实非压缩域（映射外正确未动）、turn-tracing/memory-curation L3 措辞映射外 | 在途：Minor 批单 | 回收后 git 终局序列 |
| r139-R35放行+纠错 | **R35 终局复扫：frontier 空，建议放行**（六门禁+锚 48 顶层 PASS+残余债逐项判定 accepted-debt/resolved）。抓到 lead 账目过度声明：r137 三分法"已入 spec"实未落盘（python 静默死+未变 spec 上 141/141 假确认）——edit 工具重落并当场核内容（grep=1+1、141/141），教训=**声明完成必须当场核落盘内容而非只跑校验**。新 Minor：F1 棘轮 note 印刷顺序/自测缺、F2 wiki channels 过期行、F3 r134/r136 措辞、F4 host_delivery 读旧 entry、F5 fuse 测试硬停、b5 新增代理 meditation/exec 段静默接受永不构建=假热更需具名拒。归档义务：4.3/6.5 spec 改写（specs 仍指名已删符号）F4 前必做 | 派两收尾单：spec 改写+小修批 → 回收后 git 终局序列 |
| r138-D2对账+4.5发布 | 全域对账完成：33 叶勾选+逐叶证据注记（含 2.6 被取代裁定=回执统一/命名唤醒覆盖、3.3/3.5 实质收敛+残余记卫生债、4.3/6.5 spec 侧随归档段同批），余 2 项=5.6/5.8 外部依赖（随 push 取证），change strict valid。4.5 净账发布物落盘（evidence/04-concept-net-ledger.md：removed=10 名单/renames=0/棘轮指针/三关对照），与 r137 棘轮构成一次证明+常驻守卫双证 | 终局序下一件：R35 全 frontier 复扫 → 单一提交 → merge main → push → F4 |
| r137-D1棘轮+裁定落地 | 用户四裁定+F5 全落：D1 棘轮完成（check_ratchet+golden 冻结 removed≥10/new≤1/interfaces≤77，变异证刃红→还原绿，自测 51 OK）；F5 三分法已入 spec（141/141）；D3 单一提交、D4 merge main 待终局 git 序列；D2 全域对账进行中。教训再证：heredoc 静默死×2（改 edit 工具后一次成）| 终局序：对账→净账发布→R35→commit→merge main→push→F4 |
| r136-org热迁移验收 | org 入口/集合热迁移完成亲验（四锚 count=3 绿+资源拒守护锚绿）：租约发布期扩展（installModelLeases 锁下挂载/移除不删表）、原生端口 nativeFor 闭包当前代构造、入口 publishedEntry 当前代读（FP 在途保旧投递+ErrHostAgentUndeclared 具名拒未声明地址）、compatibleWith 只留资源性二档。守护锚迁移（409→200+事实）。**org 全维度热迁移=代理同构统一收官**。遗留：durable delivery account 读旧 h.entry（后续叶）、Boot 全局件不扩展（meditation/exec 需另叶）、docs/api+wiki+spec delta 归 lead | D1-D4 评审裁定待用户 | 终局前最后产品变更完成 |
| r135-终稿面 | GLOSSARY 终稿词条落（委派=一张交接单+背景归档+零子角色、配置变更=控制面唯一通道无第三条路）。F3 近终稿汇编（除 org 行）：三关红绿证据行全齐（r90 骨架+W1-W6 增量），org 行留空位待单回收 | 可派单清零（org 单为最后产品变更）| 终局：org 回收→spec delta+wiki+docs→R35 复扫→F3 补全→F1-F3→push→F4 |
| r134-org改热裁定 | 用户裁定：入口/代理集合（接线限制）本 PR 改热；资源性（存储目录/端点）维持拒绝。三处焊死证据：r.leases run.go:410 注释自认装配后永不写、natives（recall/meditation/reentry）装配建造、h.entry 多点按名硬读（host.go:805/844/887）。解法=发布期扩展（新代含新代理→publish 时在 run 锁下补租约+观察者+原生端口；移除=新输入具名拒）+入口改读当前代声明（FP 语义：换代下一回合生效、在途保旧视图）+compatibleWith 撤 entry/集合拒绝入维度差量。守护锚迁移（agent 新增 409→发布成功+事实）。spec delta 归 lead（矩阵行 RESTART→FP/源）| 派单（同簇串行）| 资源拒锚保留 |
| r133-限制分类修正 | 用户令分清技术限制 vs 变更规则限制。修正：入口/代理集合的拒绝=**接线限制**（Boot 一次性建造消费者：recall/meditation/reentry 端口+租约挂载）被规则化（拒绝代码+守护锚+spec RESTART 档）——非资源性不可迁移；存储目录/端点才是资源性限制。W5-2 懒重建模式即解此类限制的钥匙（事件+首次使用懒挂载）。本 PR 维持窄口径（解接线=新能力），但 frontier 记账改口：org.agents/org.entry=技术债非领域真理；spec RESTART 档措辞未来应区分资源性/接线性 | R35 复扫时按此分类登记 |
| r132-W4验收 | W4 完成亲验（org.assembly 双锚+差量单测+file fact 锚 count=3 绿；TestLongContextFuse 单跑 5/5 绿=负载 flake 登记；docs/api 重生成 --check 过）：org.assembly 真落链（agent reference 装配随代发布+下一委派跑新 target 模型）、org.agents/entry=窄口径分类+拒绝零事实（r131）、preset 经 file 事实（D 遗留闭环）、file 维度常量提升 event.ConfigChangeDimensionFile。变异 4/4。全树 40 包 exit 0（单据）。遗留：Target.purpose 裸读=工具 Declaration 端口无 ctx（范围外登记）；org.assembly 事实无投影消费者（与 prompt/binding 同族）| **W1-W6 全部闭环** | 下：R35 全 frontier 复扫 → F1-F3 → push 实证 → F4 归档请示 |
| r131-W4口径 | W4 勘察揭示 org.agents/entry 真落链不可达（compatibleWith 在差量前二档拒绝+守护锚钉死+Boot 专属端口）。裁定=按代理口径交付：org.agents/entry 进维度分类+拒绝零事实（负锚不造假事件）——主 spec 矩阵明文 RESTART 拒=拒绝即正确应答，热迁移属新能力非本 PR 范围；org.assembly 真落链（agent reference 装配随代发布+dimension=org.assembly）；preset 接 file 事实面。声明口径窄化确认：运行时调用参数声明的装配=调用行为非配置面（W5-3 meta 已覆盖）| W4 按此交付中 |
| r130-W5验收 | W5 全验收亲验（四锚绿+assembly short 33s 绿=r129 挂账闭合+全树 40 包 exit 0 单据）：high-water 从 mutation/notice 推导（真 bug 修复）、Delegator RestoreEntry 懒重建、声明 meta 旅行（仅 generic call_subagent task fact；Target 不可重启重入=现状）、重启整合双锚；变异刃 5 组全过。W5 细化块在（代理读到重建前副本）。lead 面：docs/api 已重生成 --check 过；MetaKeyDelegation 的 spec 归家随 W4 一并（04 词汇表）| **W3/W5/W6 全闭环** | 最后域叶：W4 → R35 复扫 → F1-F4 |
| r129-W5确认 | W5-1..4 生产代码+锚全绿（high-water 从 mutation/notice 推导修真 bug、Delegator RestoreEntry 懒重建、声明 meta 落 task fact+continuation adopt/Seed、重启整合双锚），最终全量 -short 复跑中。确认：写集扩张在 r127 批内；新公共面 DelegatedSiteSession/RestoreEntry/WithDeclaration=方法/函数非类型（D1 过+概念门 ok），按 r121 builder 先例批、透明登记。docs/api 待重生成 | 等最终全量后复跑 assembly 门 |
| r128-W6验收 | W6 完成（m-5 pendingPass 计数=零窗口轮询定时器红线安全；m-6 Awaited 保留名字至结清/Consumed 收到即释放/下轮 notice 不被误判=r115 缺陷证实修复）：红 4 锚→绿、变异刃×2 过、整包 race 绿。遗留：assembly 门待 W5 收敛复跑（host_tools Seed 在途）；fireNoticeTurn AwaitTaskState 语义移交 R35 留意；1.5/1.6 已勾 | 在途：W5（W5-2/3/4）|
| r127-W5重建+扩权 | W5 实施者报到揭示 §2.1.5 W5 块被并发覆盖回旧稿（grill 细化落域 tasks/design 丢了编排块）——lead 从 grill 汇报重建 W5-1..4 细化文本（锚/写集/裁定引用）落盘。批准 W5-3 写集扩权：loop.go（delegatedInputOf 写 declaration meta）+tool.go（runRound 带声明）——A 案必然落点，DelegatedInput 棘轮下用非导出字段+WithDeclaration builder（r121 先例）。W5-1/2 先行不等扩权 | 在途：W5/W6 | 治理教训：grill 改域文件不改编排块的分工要写进派发令（防再覆盖丢失） |
| r126-W3验收 | W3 完成（净删 149 行：handoff 死渲染助手整块 105 行/测试残件 16 行/CauseChain 27 行/pin）：零引用门 2→0、删前删后同绿、概念门不回归（removed=0 预期）、变异不适用纯删除。额外两处测试残件按删字诀批准。lead 面随收：02 tasks 2.7 勾选、docs/api 重生成 --check 通过 | 在途：W5/W6 |
| r125-W3W5W6派发 | 三簇并发派发（写集互斥：W3=handoff.go 死码批+CauseChain 删；W5=续行搬家四叶（high-water 真 bug/Delegator 重建/声明 meta 旅行/重启锚）；W6=Resting 窗口+awaited 清理）。计划 §2.1.5 grill 细化版为派发依据 | 在途三簇 | 余：W4（org 第二叶）→ R35 复扫 → F1-F4 |
| r124-lead收尾 | lead 四件收尾完成（soak 双证迁移=段前缀+链上调用方事实、概念门 stale 行删、golden reason、注释）：soak 完整父测试绿（真实子进程）、概念门 tail 1 ok。过程教训重演：python heredoc 的 \t 是字面字符导致 assert 静默死、输出被 V=0 掩盖——**验证输出必须非空断言**再次证实。W2b 全段+lead 清理面闭环 | 控制面收归全落地（model/prompt/binding/file）| 下：W3/W5/W6 → W4 → R35 → F1-F4 |
| r123-W2b3收归 | W2b-3 完成亲验（替换锚+配置锚绿、概念门 ok）：model 收归 config_change dimension=model（lease-diff+CommitFactFor 重试语义、旧 model_replacement 事实族退役留 meta 词汇、NoteModelChange/pendingModels 零调用面删除），两变异证刃过。**控制面收归全部落地**（model/prompt/binding/file 四维度+org.* 归 W4）。lead 收尾三件：soak 红=答案身份家族（W1 driveInvocation 语义，迁移 soak 断言）、dimension 常量提升、stale 行/golden reason/注释/docs | W2b 全段完 | 下：W3/W5/W6 → W4 → R35 → F1-F4 |
| r122-W2b2验收 | W2b-2 完成亲验（file 双锚 3 次绿）：fileFixer 边界落 config_change dimension=file（内容寻址键幂等、best-effort 不挡业务、请求读事实投影），三变异证刃（不投影/失败拒绝/dimension 错）全红全还原，r97 守护锚+全绿。遗留裁定：A dimension 常量提升=event.ConfigChangeDimensionFile（W2b-3 释 api/event 后顺手）；B/C/E=有意设计（generation 0 不换代/基线事实=账本完整/累积=账本本性）登记不阻塞；**D=preset 指令装配仍直读文件=真残余归 W4 org.assembly 维度**；F=r119 并发已知 | 在途：W2b-3（model 收归，最后一段） |
| r121-S2消解+W2b4 | W2b-4 完成（channels.go 删 271 行/5 类型 6 错误、skeleton 16 pin、7 子测迁移，grep CLEAN、变异证刃过、Host.Reload 具名+行列验证过）；概念门 removed=10。**r119 引入的 ProjectionEntry S2 红同轮消解**（我的债不留）：MessageRole→非导出 messageRole+WithMessageRole builder+StatedRole() 读方法，5 导出字段复归；渲染锚 3 次绿。概念门 ok=True errors=[]。遗留：CauseChain 零生产者并入 W3 批；docs/api 重生成（lead 队列） | 在途：W2b-2/W2b-3 |
| r120-spec delta+三单 | lead 落主 spec config-hot-reload delta（4 点：FILE 懒读面→FILE 事实面/读句改从事实投影禁裸读/Scenario 改经事实生效/分路共用 config_change 通道），141/141 过——W2b-2/4 解锁；原因链 scenario 保留（decode 天然带行列满足，channels 死实现不破坏）。W2b-3 从 lead 亲做改派发（run.go 空闲+grill 正解清晰）。三单并发：W2b-2（fileFixer 边界落事实）/W2b-3（model lease-diff+CommitFactFor 收归）/W2b-4（channels 整包删+7 子测迁移），写集互斥 | 在途三单 |
| r119-渲染一致性 | W1 披露4 亲修闭环：ProjectionEntry 加 MessageRole（原文档注明一条规则两用例）、mappedEntryOf 填原始 Message.Role、projectedSegment 改消息角色优先/类型填空（与 aggregateReady 同规则）。锚 TestProjectionRendersTheStatedMessageRole：绿→变异红（user 漂移精确复现）→恢复绿（shasum 7e07f2b5）。岗位说明投影渲染=system、任务=user，谁在说话两路一致。四包+host 委派族回归绿 | W1 全闭环不留尾单 | 下：W2b 余段（model 收归/file/channels+主 spec delta） |
| r118-W1验收 | W1 完成亲验（包绿+重启锚+概念门 ok）：1b→1c→1a 落地（岗位说明 system 事实+材料投影键+task 殿后+startsRange 驱动资格语义+撤展开器），五冻结锚+三变异证刃+全树 short 40 包+黄金门 14/14+静态门 CLEAN。披露处置：(1)写集外 2 处按 r82 追认；(2)Materials=[]event.Event 已裁（L-W1-5=A）；(3)task meta 不带说明=按 D6 独立事实，过；(4)**渲染不一致=真问题**：投影型路径岗位说明渲染 user（type→role 表无 system 映射、未看 Message.Role），transcript 路径 system——谁在说话又歪，lead 亲修=投影渲染改 Message.Role 优先同 aggregateReady 规则（一条规则两用例）；(5)quiet 双保险过；(6)TestSoakContinuity=答案身份迁移家族（-short 外）登记；(7)并发覆盖确认；(8)无 git 写 | 渲染一致性亲修中 |
| r117-W2b1a验收 | W2b-1a 完成亲验（概念门 ok/bindings=1/errors=0；config+reload 锚 3 次绿）：ConfigChange 降 5 字段+非导出 model 载荷+绑定、四缺锚（binding 差量/重启可读/不进请求 skip/boot unchanged）+变异证刃、host_reload 零改动。待办认领：baseline.commit 前移=提交后动作（F4 前置，禁回填作弊）；写集外 2 处按 r82 批（skeleton_assert 编译必需+facts.go skip 实现点）。new=1 属 untracked 现状，提交即 0 | W1 在途（其半写瞬时编译失败已自愈）|
| r116-两题终裁 | 用户令"可破坏性重构+不留一单"解两题：(甲)材料键=**投影键** delegated-<site>-material-<源键>，源键入 meta 血统（r103 字面源键被同键冲突证据推翻，域语义"材料是源事件投影"完整保留，重启材料在子链零恢复）；(乙)孤儿不留——**材料/说明事实不 startsRange，靠委托工具命名唤醒纳入驱动集**（=objective 第5条"按这些键命名唤醒"的完整实现；与被删批次护栏的本质区别：护栏是进程内抑制拉取，此为事实的驱动资格语义，用 S1a 既有命名驱动面零新机制）；task 失败后孤儿不自动开段，问题消解。W1 全分叉清零解冻 | 派发 W1+并发 W2b-1a |
| r115-grill收束 | 三 grill 回收，W2 侧七分叉按契约自裁：L1=A（Run.Publish 成功后写+差量基线统一 r.current，失败下次 reload 重判同 model 族）；L1-C 登记不扩 W2；**L3=B**（r108 用户原话"读事实不裸读文件"，主 spec FILE 条款 delta 归 lead）；**L4=A**（r110 删令，矩阵 delta 归 lead）；L5=**删**（零读者=死代码，账本唯一——config_change 重启可读锚已覆盖）；F0=A（字段差量）；B2=5 导出字段+非导出 model 载荷（S2 冻结判据不加债务行）。W1 侧：L-W1-2=A（空链 mount 跳过首读，生产路径 commit 后 Wake 已是现状）；L-W1-3=现状即答案（r114 已裁）；L-W1-4=撤净删门改 grep=0+无新公共类型+numstat。**两题呈用户**：(甲)材料键投影化（源键入 meta 为血统，落账键=delegated-<site>-material-<源键>——技术证据推翻字面源键裁定）；(乙)底层故障孤儿边缘接受（不加批次标记，红线；正常世界原子无孤儿） |
| r114-grill六裁定 | grill 双单回报（W2/W3W5W6），六分叉裁定：(1)D-W5-A=**A 案**（声明随 delegated input 事实 meta 旅行，事件带状态——B 案是第二持久源违反删字诀；04 spec delta 归 lead 单写）；(2)D-W5-B=**L 案**（re-entry 懒重建，boot 零动作，tombstone/单 current 相容）；(3)L-W1-3=**现状即答案**（键已按任务+输入派生：同 body=幂等重放旧答案、不同 body=新轮，round 不穿）；(4)D-W6-A=**(a) 消费者侧自治删除**（pullDriving 收 terminal 时清 awaited，无新公共面），m-6 先红锚证实再定；(5)终局序列修正=W1→W2→（W3∥W5/W6 文件簇串行），废 W2∥W5；(6)W3 在 W1-02 后（handoff_test.go 同簇）。硬发现照办：ConfigChange 降 5 字段+concept binding（W2b-1a）、model 收归正解=lease-diff+CommitFactFor+删 noteDeclaredModelChanges、PublicationRecord 零读者改**删除**非降缓存、channels 删除需主 spec delta（lead 任务）、W5 high-water 真 bug（overlay 已证 Reactivate 2 want 3）入 W5-1 | grill 计划已回写 §2.1.5（W2b 5 叶+W3/W5/W6 拆叶）|
| r113-独立复审结论 | R35 回归（overlay A/B+shasum 对账）：**三件放行**（m1m7/m2 代码面/r89 重建复核）；**M-2 需改**——abandon/drain 两缺陷各自承重（A/B 实证）但决策资源仍泄漏：loop.go:1553-1559 production() 普通语句、Vacate/Release 只在显式路径，Goexit 后 model writer 挂起+decision 槽永不 vacate+handle 泄漏→重启 driver 卡 occupy 1ms 轮询，死实例无重启路径生产面成立；锚只断 Resting 测不到。补笔=decideWith 幂等 defer 释放 writer/slot/handle+锚强化（writers.Stopped/真实 Close/重启后决策）。次要：defer 次序（abandon 先于 forget）、consumer.go:1940 笔误。docs/api 收官重生成为硬门 | lead 亲修 M-2 补笔 | grill 三单在途（计划文本） |
| r112-config叶验收+收归偏差 | config_change 叶亲验（锚 3 次绿）：型+prompt/binding 两维度落链+负锚+守护锚全过，写集零越权。**偏差**：按兄弟型交付（明确 model 排除、保留 model.replacement 并行）——与 r107/r108/r110 收归令相反。对齐改造=lead 亲做：model 替换落链点改 dimension=model config_change，model.replacement 退役（消费者迁移），旁路退役（PublicationRecord 降缓存/文件面落事实）与 channels 删除留 W2 余段。独立复审+三 grill 单在途 |
| r111-终局计划重写 | 用户令：重写细化+grill 自细化循环。W 簇明细落 §2.1.5（叶级三锚+写集+依赖）；派 3 grill 单（W1/W2/W3W5）自审自改各自域计划文本；循环至 lead 完全有信心后进入实施 |
| r110-通道唯一 |

### 2.1.5 终局 W 簇明细（r111 起为派发唯一依据；流动并发 P1-P4 有效）

**W1 委派终态补尾**（r100/r103/r105；阻塞在途复审单回收后）
- 1a 撤子侧展开器：consumer.go 两物化点还原直接 aggregateReady；删 expandInputSources/inputRefsOf/instruction 合成 —— 正锚：既有委派族全绿（TestSubagentHandoffEnvelope/TestTargetDelegation*）；消费者锚：真实 rig 请求含岗位说明 system+材料+任务
- 1b 岗位说明产物化：delegatedInputOf 先投 system 消息事实（键 delegated-<site>-instruction）—— 负锚：拒收→零事实零段（扩既有 TestDelegatedInputRefusalLandsNothing 覆盖首事实）
- 1c Materials 替 Refs：runRound 用 builder.Facts 解析→DelegatedInput.Materials（源键）；材料以源键投子库（同库幂等命中即已在）；任务事件殿后；命名唤醒（材料键+任务键+instruction 键）—— 正锚：材料内容经真实请求在场；负锚：幻觉引用静默不物化
- 1d 测试随迁+净删行数为正 —— 验证：go test ./... -short+黄金门+概念门 new=0/removed≥4
- 写集：runtime/agent/{loop,consumer,delegate}.go+*_test、tool/subagent/{tool,handoff,builder,continuation}.go+*_test、tests/{execution,host,capabilities} 委派族

**W2 配置事件化一叶**（r97/r106/r108/r109/r110；依赖 W1 完成释 run.go）
- 2a TypeConfigChange 型+五维度落链：Host.Reload 各维度换代落 config_change（dimension=model/prompt/binding/file）；model_replacement 落链点收归为 dimension=model（旧型不再新增落链）—— 负锚：reload 拒绝不落事实
- 2b 旁路退役：PublicationRecord 降为缓存（事实为准）；文件改写落 dimension=file 事实、请求边界读事实（封第三条路）—— 正锚：r97 三守护锚不回归+新锚"文件改写后链上有事实且下一请求读新文"
- 2c channels 整包删除（state/channels/**，零生产消费者）—— 验证：go build ./...+go test ./runtime/state+grep 零引用
- 2d model_replacement 消费者迁移（event_integrity 替换锚等）—— 正锚：替换场景锚全绿
- 写集：api/event/**、assembly/{host_reload,commit,run}.go、features/rl/replacement.go、runtime/state/channels/**(删)、tests 对应

**W3 死代码小删批**（独立，随时）
- handoff.go 死渲染助手（refEntry/refSummaryOf/indentContent/withRefDetails/defaultRefBudget）删除；missingRefLine 测试引用随迁 —— 验证：go test ./tool/subagent
- 写集：tool/subagent/handoff.go+handoff_test.go（与 W1 时序错开）

**W4 org 第二叶**（r109；依赖 W1+W2）
- org.agents/entry/binding/assembly 四维度落链；岗位说明（Assembly.Instruction）事件化读路径 —— 正锚：改装配声明→config_change dimension=org.assembly→下一委派用新岗位说明；负锚：在飞委派保持旧说明
- 写集：assembly 装配读路径+state/binding+tests

**W5 续行搬家+m-4**（r95/m-4+r114/r115 裁定；grill 细化四叶重建 r127）
- **W5-1 轮次 high-water（真 bug）**：foldLocked 只从 outbox 推 round、unowedLocked 剔除已消费 notice→恢复后 Reactivate 返回 2 want 3 撞历史 settle:<task>:2 键。红锚=TestProofRestoredRoundHighWater（durable 两轮 settle 后消费 notice→Restore→Reactivate=3）；修=high-water 从 settle:/reactivate-rollback: mutation 身份推导，不新增持久字段。写集：runtime/task/**、state/durable.go
- **W5-2 Delegator 懒重建（L 案）**：重启首拒是 CanReopenRound 的 'no delegation this ledger holds'（非 Missing）。红锚=重启后 open round 的 re-entry 不以该错拒；修=re-entry 懒重建 Delegator entry（boot 零动作）。写集：runtime/agent/delegate.go、assembly/host_tools.go
- **W5-3 续行表+声明持久化（A 案）**：声明随 delegated input 事实 meta 旅行（declaration JSON 键）；续行表原地 seed（hostSendReentryPort.Bind 复制接口值的快照风险）；round 不穿=同 body 幂等重放现状。DelegatedInput 5 字段棘轮下用非导出字段+导出 WithDeclaration builder（r121 先例）。**扩权已批（r127）**：runtime/agent/loop.go（delegatedInputOf 写 declaration meta）+tool/subagent/tool.go（runRound 带声明）。写集：tool/subagent/{continuation.go,tool.go}、runtime/agent/loop.go、assembly/{tools.go,run.go}、api/event meta 键
- **W5-4 重启整合锚**：真 assembly 两进程=重启后 open round 服务+声明恢复+轮次正确；负锚=无任务不建槽。写集：tests 新锚
- 约束：boot 只重建 metadata、禁 Ensure/mount/不动 Loop.current；addr 可由 Spec.Source.SessionID+#sub:+CallID 推导；不持久 running/owed锚：无任务不建槽
- 写集：tool/subagent/continuation.go、runtime/task/**、assembly/host_tools.go、tests

**W6 minors**（依赖 W1 释 consumer.go）：m-5 Resting 窗口（pendingPass 计数）、m-6 awaited 清理

**终局序列（r114 修正）**：W1（全叶）→ W2b（5 叶，含 model 收归正解+PublicationRecord 删除+channels 删除）→（W3 ∥ W5/W6 按文件簇串行）→ W4 → R35 全 frontier 复扫 → F1-F3 → push 实证（已批）→ F4 归档请示。lead 单写随行：04 spec delta（D-W5-A 声明键+channels 矩阵撤销）、主 spec config-hot-reload、docs/api 重生成、GLOSSARY。 用户修正 r108(5)：不需要独立配置变更通道——事实链就是通道（config_change 事件落链→OrgPlan 投影→边界重读），channels Router/Change 不接线不复活，**列入删除清单**（零生产消费者已证 r101，纯死设计，为"事件链尚未唯一权威"的旧世界观而设）。修正后全景：配置变更唯一通道=事实链；四旁路退役=PublicationRecord 内存/裸文件读/model_replacement 独立型/channels 整包 | channels 删除叶立项（与 handoff 死助手同批小删叶） |
| r109-org收归 | 用户补令：org=声明树全部结构（入口/代理集合/接线/委派装配含岗位说明），是控制面主体——现状 Declaration→OrgPlan→StatePlan→换代只有编译链无事件面，org 结构变更不落事实=最大的内存旁路。收归维度：org.agents/org.entry/org.binding（与绑定合流）/org.assembly（岗位说明事件化，与委派重设计相接）/file。终态=声明树唯一变更通道是 config_change，OrgPlan 是事件后的投影机器，channels.Dimension 真正接线。排程裁定：第一叶=型+model/prompt/binding/file 五维度（已批）；org.* 为第二叶立项（触碰委派装配读路径，与 r105 补尾同域需排序） | config_change 单回收后重排 |
| r108-收归全清单 | 用户纠偏（注意力收拢）：config_change 的收归范围=一切绕开事件到达行为的配置路径——(1) model_replacement→dimension=model；(2) prompt/声明变更（PublicationRecord 仅内存）→dimension=prompt；(3) 绑定换代（binding.Hold 无事实）→dimension=binding；(4) **文件面改写=第三条路**（无事件直达下一请求）→落 dimension=file 事实，请求边界读事实不裸读文件；(5) channels Router/Change 合流不再是独立叶——config_change 就是它的事件面；(6) 行为面（换代重读/在飞固定）不变但投影来源统一为 config_change 事实。终态：三旁路（内存记录/裸文件读/model_replacement 独立型）退役或降为缓存 | config_change 单回报后按全清单对齐改造（必要时扩为多叶） |
| r107-收官验收+收归令 | 委派收官单验收（凭据见其报）：负锚+变异证刃过、信封线程删除、DelegatedInput 5 字段 A 案、FactKeyReader 内化、全树 short 0+黄金门 14/14+概念门 new=0/removed=4。**r105 扩展未做（按 r104 边界）→lead 亲做补尾**（撤子侧展开器/岗位说明产物化/Materials 替 Refs/命名唤醒）。用户新令：config_change 须**收归同职责概念**——model_replacement 不是兄弟是同职责，config_change 为唯一配置变更事实（维度区分，model 降为维度），行为面同步收归 | config_change 单在途待对齐 | 挂账：docs/api 重生成、handoff 死助手叶、独立复审在途 |
| r106-用户双批 | 用户批：(1) config_change 事件类型（红线放行，r97 原则最后一公里）；(2) push 授权——终局前执行（全量复绿+独立复审过后），取三条外部证据（十分片真实跑通/bundle 等集验证/PR 快反馈≤10m）。归档仍到终点请示。config_change 单即派（写集避在途委派单：api/event+host_reload+新锚，禁 run.go/tools.go/runtime/agent/tool/subagent） | 在途：委派收官+独立复审 |
| r105-不留尾单 | 用户裁定：r100/r103 归属迁移并入收官单，不留下一单。扩展范围五件（子侧展开器撤除还原 aggregateReady/岗位说明产物化为 system 事实/工具侧 builder.Facts 解析→Materials/delegatedInputOf 终态=岗位说明事实+材料源键投递+任务事件殿后+命名唤醒/SetFactReader 保留仅喂命名拉取按键读取）。测试随迁按 r103 形态。验收加概念门 new=0+净删 | msg 传达=unaddressable，以台账为准 |
| r104-收官单裁定 | (1) FactKeyReader 机械内化（删命名类型）不整删——同库接线下子的命名拉取仍需按键读取（同库=按键全库可见/链是视图），r100/r103 的展开归属迁移是下一单；(2) DelegatedInput=A 案修正版：删 CallID 与 Refs、增 Materials（工具侧解析后源键），五字段回 S2 内，幂等键从 Site 派生。范围边界：子侧展开器撤除不在本单 | 收官单继续：负锚已绿+变异证刃、信封线程已删、全量 short 已 exit 0（capabilities/recall 迁移完）、概念门 new=1 待内化后归零 |
| r103-三裁定收敛 | 用户三裁定：(1) 委派提交=父工具逻辑的原子契约，失败=委派整体失败，无部分落地概念；(2) 材料键=源键（事件带身份旅行，两边共用事件词汇，源键幂等）；(3) input_refs 不要——下游按自己的消费逻辑消费与重投影。收敛形态：委托工具原子四步=解析→材料源键投子库→落任务事件（无引用清单）→命名唤醒（材料键+任务键，S1a 既有命名驱动，零新机制）。子的循环零特例；同库接线材料天然已在+命名纳入，分库接线投递先行——一套工具逻辑两种接线通吃。疑问一~四全部消解 | 委派收官单回报后按 r100+r103 重排 | config_change 批仍待用户 |
| r102-r97叶收官 | r97 第一叶收官亲验（三锚 -count=3 绿、gofmt 净）：现行正式入口已实现裁定语义（换代/文件重读/在飞固定视图），登记守护锚 403 行；无产品改动。(A) config_change 事件类型仍待用户批；(B) channels 合流独立叶；(C) 口径=单决策已确认。r102 材料形态推导待用户确认（材料=子的输入、开子的段、先到先消费；伪问题=半批恐惧是 C-1 遗产思维） | 委派收官单在途 |
| r101-r97叶裁定 | 元信息叶中期回收：三锚全绿落守护锚（prompt 结构/文件两维+模型换代+同请求固定视图），行为无缺口，口径=请求=单决策模型请求（与 execution-generations 冻结规格一致）。裁定：(C) 确认口径；(B) channels 合流登记独立后续叶（判据合流非最小实现）；(A) prompt 变更无链上事实=r97 原话的缺口，需新事件类型 config_change（与 model_replacement 同族）→ 触红线呈用户批 | 委派收官单仍在写 |
| r100-库边界裁定 | 用户三连裁定固化：(1) 事实库边界=每个代理（域边界），物理同库与否由控制面装配决定（当前恰巧同库）；(2) 展开职责=父委托后"委托工具的消费逻辑"（框架），非子的消费循环——子的循环只读自己库，两种接线均成立；(3) 材料形态按 lead 的 A 案执行（材料=委托工具产物事实逐条落子库、任务事实殿后作驱动键、落齐才唤醒；源事实不搬家，子库记引用血统）。**Stage 1 落点作废**：撤 Loop 读取面+子侧展开器，迁至 delegatedInputOf（工具侧经 builder.Facts 解析→DelegatedInput.Materials→提交材料事实+任务事实） | 在途收官单按旧形态，回报时对齐 |
| r99-全貌快照防忘 | **五节全貌更新（用户令固化）**：预期=不变（流水账+统一消费+净减+三关）；已纠正=①窗口/缓存恢复/保留投递/批次护栏/父侧烘焙/双事实提交/直驱引擎/四公共面 **已全删**（removed=4/interfaces 78→77 实证）②关闭时间线已落地（A+语义：失败=结清+点名+失败观察落链可驱动）③引擎已并一（Drive 内化）④自驱法则已立（三特例替换）⑤CI 本地面全绿+黄金门 14/14+全树 short 曾整绿；在途=委派切换收官（两 host 测试迁移后验证中）+负锚+r98 信封线程删除；已裁定待做=r97 元信息事件化（配置=控制面声明/事件=机制）+续行状态搬家（下一变更）；挂账=独立复审四件（通道不稳待重派）。**判据不变：概念净减维持、三关维持、全量绿后独立审+frontier 空才收官** |
| r98-事件统一 | 用户裁定：输入类事件职责相同（正文+来源+血统），进一步统一。落地终态：内部委派=普通输入事实，HandoffEnvelope 线程化删除（admit 的 Validate 保留为调用参数质量门）；外部 A2A 信封只在边界适配层由平面参数渲染。与 r95 统一性原则、r97 配置事件化同构——输入、元信息、配置都是"事件+投影" | 先收尾两失败测试（requestContent 只取末消息的旧烘焙假设），随后删信封线程 |
| r97-元信息事件化 | 用户裁定（控制反转，两句合成完整原则）：(1) agent 的 system prompt 等 meta 由上游事件控制（config-change 事件通知变更）；(2) 配置文件指定的都默认是希望 agent 可被外部控制的——**配置=控制面声明清单，事件=控制机制，无第三条路**。含义：prompt/模型/工具/技能/绑定是配置事实的投影，请求边界重读，同请求内固定视图。既有种子：state/channels Change（按维度）、binding OrgPlan/StatePlan、model_replacement"配置变更是事实来源之一"、技能文件下一边界可见模式。新叶红锚三件：改 prompt 事件→下一请求新 prompt；改模型事件→下一决策新模型；同请求内固定。与委派切换正交 | 委派切换调试中（envelope.Refs 流经待查） |

| r96-委派切换落地中 | S2+S3 主体已落（lead 亲做）：DelegatedInput 增 Refs/CallID；delegatedInputOf 单事件提交（正文=任务 user、meta=instruction+input_refs、call 派生键幂等）；submitDelegated 删批次；批次机制（字段/方法/pull 守卫）与 input_batch_test 删；turn.request/externalContext/refDetails 烘焙删；assembly run.go 接 SetFactReader(CommittedEvent)；外部 tRPC 面由框架边界机械拼装（ExternalContext 块+refs 内容+任务）。剩余：两个运行时路径测试（TestDelegatedTurnReadsTheRefsContent/TestTargetDelegationReadsTheRefsContent）钉旧烘焙格式需迁移为内容在场断言（无 reader 裸身份渲染按 Q2 已死）+S11/golden 迁移+负锚 | 全仓 build 过；runtime/agent 短测绿 | 下轮完成迁移面 |
| r95-统一性原则 | 用户立原则：每个代理不分父子抽象上独立同构，父子关系是代理维护的状态而非结构。落地：机制通用化——delegated_refs→input_refs、expandDelegated→expandInputSources、delegatedRefsOf→inputRefsOf（任何输入事实可带来源引用，委派只是用例，与 input_events 同族=Q5 既定）；注释改述待 S2 时统一补。续行状态搬家立为下一变更。GLOSSARY"代理"词条待补 | 包绿 | 下一步：读 tool.go 供给链→S2+S3 原子切换 |
| r94-委派S1 | Stage 1 完成（休眠能力，lead 亲做）：Loop 新增 FactKeyReader 读取面（SetFactReader，沿用 face 家族模式，登记为读取面非新概念）+ expandDelegated 展开器（delegated_refs meta→指令在前源事件随后，幻觉引用静默不物化=Q2）接进两处物化点。无生产者写该 meta→零行为变更；runtime/agent 短测+子代理两锚绿 | 切换前必读 tool.go 供给链（Instruction/Input/Handoff 来源与子常驻 system 渲染） | 下一步：S2+S3 原子切换（单事件+删烘焙+删护栏）+S4 迁移 |
| r93-设计冻结 | 用户裁定 Q7：父不做拼装只说明任务，拼装=框架机械工作，子无感知。委派终态：一条委派事件（正文=任务说明，meta=源事件引用，call 身份幂等键）→ 子消费循环物化时框架解析引用展开为输入材料（指令在前、源事件随后）→ 删除父侧烘焙+批次护栏+双事实。Q2 幻觉引用静默失败。实施分期：S1 子侧展开能力（休眠）→ S2 切换单事件+删护栏 → S3 删烘焙 → S4 迁移锚与 golden | lead 亲做 | 勘测：物化点=aggregateReady/pull 材料；跨链读取面待查 |
| r92-Q1修正落地 | Q1 修正（渲染规则）lead 亲做完成：wire 面不变量=每请求内每 callID 至多一条 tool 消息（段内多轮重放回执合法，跨请求重复合法；providers 拒的是单载荷内重复）。加固进 TestReceiptAndBackgroundStaySeparate（非缩水，纯增）；10/10 绿；变异证刃：观察角色翻 RoleTool→锚红，python 定点恢复 shasum 对账 YES，3/3 绿。首版跨请求恰一次断言钉错维度已纠 | 子代理通道九死弃用 | 待：Q7 档位 |
| r91-委派设计裁定 | 用户裁定（grill 轮）：一次委派=一条事件，正文=主 agent 自由决定的 system prompt（职责），载荷=N 个源事件引用；子代理消费循环重投影（Q1 还原=框架职责；Q2 引用框架铸造必解析、幻觉引用直接失败不暴露、不建失败观察机制；Q3 不结构化信封——intent 等由主 agent 写进 prompt 自由文本；Q4 幂等按 call 身份；Q5 引用与 input_events 同一机制/键族）；Q6 测试迁移=钉新装配形状保信息完整（已解释）；Q7 父侧烘焙删多少待用户选档（全删/折中/双份，lead 荐全删）。删除面：批次护栏+双事实提交确定删；烘焙视 Q7 | 子代理通道八连死，委派实施由 lead 亲做 | 独立复审仍挂账必过 |
| r90-F3双证表 | 症状锚红绿双证汇编（证据轮次见台账）：**不丢消息**——迟到收割(r24 红 requests=2 缺对→绿)、孤儿自驱/空闲挂载/不空转(r55 三锚)、缓存淘汰豁免(r40)、链重建全元数据(r40)、WaitAnswer 竞态(r61 五测 5/5 超时→r63 3/3 绿)、压缩因果(r47)；**不假收束**——关闭死锁(r26→绿；r46 再红→r59 绿)、Quiesce 三测(r46→r59)、A+ 失败点名(r59)、abandon 异常退出(r84)；**不双驱**——单 current/tombstone(W1-01 簇)、输入批次(r69)、批次合一(C-1 待用户)；**概念门**——残条 556→16(r39)、removed=4(r71)。待补：C-1 落地后其锚入表；全量事故后复验在跑(job 2c9822fc) | 独立复审重派在途(c86a492e) | F3 骨架成 |
| r89-事故与恢复 | **事故（lead 责任，如实入账）**：M-2 A/B 自证的恢复分支误用 git checkout -- 把 consumer.go 还原至 HEAD，抹掉会话内全部修改。恢复：/tmp/s4-ab4 快照（含 S1a/竞态修/批次/双渲染修）+ 重放 S4 writers 内化 + 重放 abandonIfFlying；差异对账仅两处重放；四层验证全绿（包 short/四锚 count=3/assembly/关闭锚 count=3）。时间线核对：s4-ab4 之后 consumer.go 仅 S4 与 M-2 两处变更，无遗漏源。教训入册：A/B 恢复禁用 git checkout，一律 python 定点还原+shasum | C-1 形态仍待用户 | 放行前独立复审（含本次重建复核）必过 |
| r88-动荡期收束 | 子代理全灭（M2复审单亦亡）。M-2 锚非空转性以机械 A/B 自证：禁用 abandonIfFlying→锚红、恢复（shasum 对账）→3 次绿。**独立复审登记为放行前必过门**（待环境稳定重派）：覆盖=lead 的 M-2 修复+m1m7/m2 两单无汇报变更。动荡期账：五单亡、两单工作已落树经验收、C-1 三派未动工 | 待用户 C-1 形态裁定 | 放行序列不变：C-1 落地→全量复绿→独立复审→frontier 复扫→F3/F4 |
| r87-m2收账 | m-2 也亡于汇报前但工作落树（lead 验收补位）：NewContinuations 导出删除、ToolOptions.Continuations 字段删净、包内 newContinuations() 内部构造、tool/subagent+assembly 短测绿、概念门 removed=4/interfaces=77 不变（公共面再缩但不改类型计数）。R35 复议 2 的"保留公共构造+字段"残留就此清偿——现在四面真全内化 | 同 m1m7：无汇报变更待独立复审覆盖 | 待：用户 C-1 形态裁定/lead复审单 |
| r86-m1m7收账 | m-1+m-7 单二亡但工作已落树（lead 验收补位）：双结算幂等化后 'left its settle account open' 警告 0/10（原 2/10），settledFailureInvocationsOf 计数锚在树，关闭五锚+lattice -count=10 全绿（50s），assembly 短测绿。因无代理汇报，改动细节以 diff 为准待独立复审覆盖 | C-1 形态 A/C 待用户 | 在途：m2/lead复审 |
| r85-慎思令 | 用户令"慎思"。自我审视：M-2 系 lead 亲做且零独立目光（首版锚空转假象已证我会犯错）→ 派独立复审我的修复（含 wake 排空语义/defer 次序/嵌套 Goexit/锚非空转性 A/B）。终局风险清单登记：完成偏压、疲劳验收、lead 自作未审、断言预算累积缩水（各迁移单各自有理，总量需终审一次对账基线） | 在途四单：C1/m1m7/m2/lead复审 | 终局不变：frontier 复扫空才放行 |
| r84-M2亲做 | M-2 由 lead 亲做完成（两代代理死于 harness 不稳）：红锚 TestAbandonedFlyingSettlesOnAbnormalExit 先真红（首版绿是空转静止假象，补 Wake+驱动证明后红 2.11s），修=run() 注册序 defer abandonIfFlying（结清 flying）+排空 wake 令牌（第二缺陷：驱动者死后令牌无人排空，Resting 永假）。锚 5 次绿+整包绿+关闭锚 3 次绿。m-1/m-7 仍未做（从 M2 簇拆出） | 子代理仅 deepseek-account 一 provider（无 kimi），重派用短提示 | 在途：重派 C1/m1+m7/m2 |
| r83-M1收口 | M-1 完成亲验（反射负锚+包绿）：Drive 降非导出 drive、三文档同步、编译期断言行 r82 已批。lead 清扫 r82 承诺残留（consumer.go:200 注释/README:90/wiki 架构页/FACT-VOCAB:61 的 Loop.Drive 旧指称→常驻消费循环）+spec:27 旧名改述（禁令语义不变：MUST NOT 保有第二驱动入口）+docs/api 重生成 --check 通过 | 在途四单：M2+m1+m7/m2/C1/Q1修正 | R35 复扫待全部回收 |
| r82-M1写集裁定 | M-1 预检报备获批：扩展=runtime/agent/skeleton_assert.go:133（(*Loop).Drive 编译期断言，同包测试面）+docs/EXTENSIONS.md:66（同文件旧指称）。其余残留（README.md:90、docs/wiki/agent/agent-architecture.md:24,145、docs/FACT-VOCABULARY.md:61、consumer.go:200 注释、docs/api 生成物）归 lead 于单落地后亲自清扫+重生成 | M-1 先写负锚红中 | 在途五单 |
| r81-用户四裁定 | grill Round2 用户裁定：(Q1) 选 A 但点中协议要害——统一 RoleTool 会撞"每 call 恰一条 tool 消息"（受理回执已占 tool 槽）。修正后 A=统一规则非统一字面：tool 槽每调用恰一条（live=受理回执、replay=结算结果本身），live 迟到观察=user 材料；补 provider 面归一化锚（user 消息不得携带 tool_call_id 发出）+按上下文收紧 golden。(Q2) 删——委派输入合一事实，同时删批次护栏；立架构原则"tagent 特性尽量通过扩展点挂载（plugin/event/event-loop/call_subagent 四处窄口），不进核心"。(Q3) 由扩展点原则解决：续行表属 call_subagent 扩展点内部簿记，留原地不搬；m-4 重启缺口登账下轮。(Q4) 归档暂缓确认 | C-1 修复单+Q1 修正单派发 | 在途共五单 |
| r80-R35frontier | R35 独立复审回报：frontier 非空（C-1 委派批次仅进程内原子/半批事实自驱半回合=Critical；M-1 Loop.Drive 零生产调用方仍在公共面+wiki 矛盾；M-2=F-a abandon 缺失）。两复议均接受：r76 "四面全内化"表述更正为"3 面全内化+1 面保留公共构造与字段"；r66/r75 对双渲染的显式接受重开（provider 侧 role≠tool 配对可能被丢，行为门"结果必达"无断言守护）。分拣：M-1/M-2/m-1/m-2/m-7=机械（契约可推出）即刻派三单；C-1=真语义分叉呈用户（grill Round 2）；m-3~m-6 登账下轮 | 双门与 14 锚复审全过 | 放行条件：C-1 用户裁定+三单回收+frontier 复扫 |
| r79-裁定纪律 | 用户令：矛盾之处好好想再做裁定。固化为裁定协议（对 R35 复审结果及后续一切矛盾适用）：(1) 证据先行——两侧各取证到 file:line+可复现，禁凭记忆断言消费图/行为（r64/r68 之训）；(2) 对照冻结契约——两测试各钉相反答案时先查 spec 白名单与已裁定语义，能从契约推出的不算矛盾只是执行；(3) 契约本身分叉的真语义抉择（如 A+、fast-first vs 同请求）→ 呈两案+推荐+代价，交用户裁定，禁独断；(4) 裁定后 24h 内若被证据推翻，公开纠错并回滚（r66 先例） | 在途：R35 | 适用即行 |
| r78-F2全树整绿 | 全树 go test ./... -short -count=1 首次零失败（FAIL 行为零）——收敛工作开始以来第一次全量合流干净。F2 证据落地 | 在途：R35 独立复审 | 余下：F3 症状锚红绿双证汇编 → F4 archive（需用户明示授权） |
| r77-F1对账 | F1 契约差异=已批准 delta 逐条映射（无未批准项）：specs 五文件全对上裁定——architecture-guardrails+8=r53 端口拒绝、async-task-execution+8=r53 移交路由、consumer-loop-convergence±4=r54 1.4 直驱例外/第二调用撤回、delivery-dispatch-receipts+12=r53 审核四态、persistent-event-loop−556=r39 残条清偿；golden architecture.json+6=W4-01 绑定表、scenarios.json±24=r66/r67 S03+S11 迁移；workflows=5.2/5.4/5.5/5.7 | R35 独立复审派发 | 全量合流后台 |
| r76-D6终值+GLOSSARY | D6 净账终值（诚实拆账）：概念面达标（removed=4/new=0/exported_interfaces 78→77=冻结判据达成）；LOC 生产净 +912（含删除性归一砍掉的保留投递/直驱引擎/forced 特例/四公共面，也含 A+ 失败观察/输入批次/自驱法则新语义实现）、测试净 +2329（迁移+新锚）。GLOSSARY 补三术语：输入批次/链权威答案/失败观察（4.3 部分完成，域收缩另核） | 全量合流后台跑 | 余下：F1-F4+独立审 |
| r75-三红清零 | 双红迁移亲验（preset -count=20 -race 绿、roundtrip 同）：#1 根因=测试在 provider goroutine Fatalf 杀消费循环（Go 语义）+旧"同请求"直驱假设与 fast-first spec 冲突；#2 同族竞态。全量三红清零。登记两枚产品 frontier：(F-a) consumer.run 无 defer abandonSegment——goroutine 异常退出（provider panic/Goexit）时 flying 不结清、Resting 永假、close 耗预算，WaitAnswer 有 done 兜底；(F-b) r66 双渲染（harvest=user+ToolID vs chain-RoleTool）已显式接受两形，统一后可收紧 golden。全量合流后台跑（job bash-31145bf770624c3a）作 F2 前置 | 余下：D6 终值+4.3 GLOSSARY（含输入批次/段/链权威答案）+F1-F4+独立审 |
| r74-迁移裁定 | #1/#2 处置=迁移测试非改产品。#1 根因=测试在 provider goroutine 调 t.Fatalf，FailNow 杀消费循环 goroutine（Go 语义）致 WaitAnswer 撞 done 返回 ErrSegmentCancelled，非产品活性缺陷（诊断：去掉 callback 断言 Invoke 正常 completed/2 decisions）；更深=旧断言"两 obs 同在第 2 请求"是直驱时代假设，S1a 常驻消费+快结果先到先消费（spec 明文+fast_result_drives_first_test.go 锚）要求快先驱动、慢后续段到达，改产品等齐全打破锚+spec 禁止。#2 同族（fast-first 竞态+chain-restore 渲染=r66 已登记）。迁移不弱化：两 body 必达/调用恰一次/args 逐字节保留，仅去"同请求"时序假设 | 子代理按迁移继续 | 余下：D6 终值+4.3 GLOSSARY+F1-F4 |
| r73-docs重生成 | docs/api 重生成完成（gen_godoc.sh，69 文件；--check 确认 matches the source 68 packages，非 stale）。小残留登记交独立审：ToolOptions.Continuations 字段名仍导出（类型已是包内 continuationTable，类型名已删；字段非类型不计 removed=4），若要彻底净减可再改字段名 | 三红余一（活性+flaky triage 在途） | 余下：D6 净账终值+4.3 GLOSSARY+F1-F4 |
| r72-身份迁移 | TestObservationReachDiagnostics 迁移完成亲验（inspection 整包 ok）：按 r42/r44 双证契约（段前缀+链上 input_events 证消费了调用方输入），per-fact 身份断言、覆盖/边界/正文不弱化。三红余二：活性失败+flaky 在途 | docs/api 重生成待做 | |
| r71-S4净减达成 | **净减里程碑**：四面全内化，概念门亲验 removed=4、exported_interfaces 78→77、errors=[]，go doc 四名不再导出。你最初"概念膨胀"担忧的翻正硬证据。S4 偏差已按 r70 裁定收口（Continuations 保留 NewContinuations 构造，A 方案的查找机制不引入）。全量 37 ok/3 红均证非 S4：#1 TestPresetToolsAssemble=真活性失败（段未答即结束）最严重、#2 TestNativeConversationRoundTrip=flaky 竞态、#3 TestObservationReachDiagnostics=答案身份迁移家族。r53 遗留的 3 处 openspec EOF 空行已由 lead 修净 | 双 triage 在途：活性+flaky（写集 runtime/agent/tool/assembly）、身份迁移（仅测试） | docs/api 生成物 stale 待重生成 |
| r70-S4裁定 | Continuations 走 **B 非 A**。理由：(1) r68 消费图对 Continuations 是错的——生产消费者含 tool.go/tools.go/host_tools.go（宿主重入），我认错；(2) 续行状态是 taskID 键的 run 级状态，**单表才是单一权威**，A 的"每 Tool 自建"把一张表拆成 N 张（每 agent 一张）反违反单一事实来源，且需新增 taskID→Tool 查找（新机制）并动刚修好的 host 重入路径，净复杂度上升。B：类型改非导出 continuations、ToolOptions.Continuations 改匿名接口(Claim/Abandon/Serve/Served/Release/Reenter)、host_tools.go 的 hostReentryContinuations 接口本就有可保留、run.go 字段改匿名接口；导出类型名 Continuations 删除→removed=4。NewContinuations 构造函数名可留（门计类型非函数），零成本删则删，不为删它引入 A 的查找。写集授权=tool/subagent/{tool.go,continuation.go}+assembly/{tools.go,host_tools.go,run.go}（仅类型标注/接口改动，禁 A 所有权重构）。frontier：续行状态是否应归 runtime/task 与任务注册表同住，交独立审。同时批准 1 Writers(Loop 自建+Begin/Stopped 提升+run.go 非导出接口)/2 PresentedFace(删接口+HistoryFace 可选能力断言匿名接口 SessionPresented，不按链位置猜)/3 Movement(删 Movement/NewMovement/WithMovement/SetMovement，registry/store 各私有 atomic 序号+runMovement 复合围栏) 按子代理方案执行 | 四面全后硬验收：concept counts removed≥4+errors 无新 domain-surface+exported_interfaces 下降 | 在途：S4 |
| r69-黄金全绿+S11 | 里程碑：完整黄金证据门首次全绿（required=14 attested=14 drift=0 unattested=0，runner exit 0）。S11 triage 完成亲验：两漂移归因（时序计数=r66 同款、回执解析 accepted vs 最小 ok=r60 同源），并挖出真产品缺陷——委派输入拆两事实、子代理可能只拿一半就跑（9/50）→加 batching 原子性护栏修复，红锚 input_batch_test -count=3 -race 绿。红线判定：batching 非窗口/轮询/旁路、未导出，不触发红线；但登记领域概念"输入批次"入 GLOSSARY（S4 的 4.3 一并）+ frontier：能否合并为单一事实交独立审 | S4 派发（主战役：四公共面净减，验收 removed≥4） | 在途：S4 |
| r68-S4消费图 | 四面生产消费者枚举完成：全部仅 assembly/run.go 一个接线点（NewWriters:391/SetWriters:398、task.WithMovement+SetMovement:388、NewContinuations:393、SetPresentedFace:405），其余消费者全为测试（consumer_test/loop_test/skeleton_assert/final_round_drain/close_drains/scenario_s05/continuation_test/movement_test/durable_test）。S4 改法定向=内化：Loop 自建 writer 台账（删 SetWriters/NewWriters 公共面）、registry/store 自建 movement、subagent 自建 continuations、presented 由链派生不注入。净减证据=概念门 counts 删四名后 shown removed>0 | S11 在途（写集含 runtime/agent/task，与 S4 冲突）→S4 待其回收 | D6 净账复测随 S4 后 |
| r67-S1b完成 | S1b 双叶完成亲验（叶1 锚 count=3 绿+assembly 短测绿）：叶1 删保留投递净 −56 行（Quiesce 一处 offerOwed，无窗口轮询，锚 10 次绿）；叶2 S03 golden 按 r66 迁移，200/200 含 189 次晚到时序全绿。S11 根因 intel 已转在途单（回执解析 accepted vs 最小 ok=r60 同源 + 时序计数=r66 同款）。残留 settle account open/mailbox consumed 警告登记独立审核对项 | S1b 范围外的 S11 由 fd48545b 处理 | 在途：S11 triage |
| r66-纠正r64 | S1b 叶2 证据推翻 r64(a) 前提：两合法时序消息形态确实不同（早到=[user,assistant,tool,tool,user]；晚到=闭合视图 5 条+晚到 body 以 tool-role 入下一段），无可规范化统一 6 形态——R34：我把候选当事实。新裁定：接受分裂断言迁移（model_message_roles=稳定回执轮4 + 两 body 必达 + boundary 跨请求），原"6"是时序敏感脆断言（违反禁 R5），迁移是纠正非弱化。frontier 立项：同一逻辑观察 harvest 路径渲染 RoleUser+ToolID、chain-restore 路径渲染 RoleTool——两路径渲染不一致（S1a 未除残留），修好可再收紧 golden，交 S4/独立审 | S11 确认出 S1b 范围，另派 triage | leaf1 残留（settle account open/mailbox consumed 警告）登记独立审核对项 |
| r65-S4盘点 | 四公共面定位完成（S4 删除靶，r41 S4 簇）：Writers(runtime/agent/consumer.go:1902，writer 台账供 close 用，消费面=assembly/run.go+3 测试)、Movement(runtime/task/movement.go:28，RegistryOption+MemoryStore.SetMovement)、Continuations(tool/subagent/continuation.go:40)、PresentedFace(runtime/agent/loop.go:2142，presented seam，消费面=examples/contract)。S4 改法=内化为非导出（保行为缩公共面）或删除（无生产消费者时），删除后跑概念门证明 removed>0 净减 | S1b 叶1 在途 | S4 待 S1b 后派发 |
| r64-golden裁定 | S1b 叶2 裁定：批准 (a)(b)(c)——record 推导改从"闭合视图+迟到结算请求"的完整历史规范化出真实 6 消息形态（两合法时序同产一形态，观测去调度依赖）；multi_tool_call_boundary 跨所有工具后请求求值（每对回执+背景配对必须在某处出现，不弱化）；golden/runtime/scenarios.json 保持 6 roles/boundary true 严格不变（契约不弱化）；写集扩展授权 tests/integration/acceptance/scenario_s03_test.go。"删 roles 键"方案否决（6 消息形态是真实契约，非可删噪声）| 叶1 剥保留投递进行中，叶2 待其回报后按本裁定执行 | 无在途他单 |
| r63-竞态修复落地 | WaitAnswer 原子化（缓存复查+取通道同临界区，链读其后）lead 亲落并验收：runtime/agent 短测绿、evolution 五测 -count=3 -race 3/3 绿（原 5/5 超时）、fidelity count=5 绿。attention/fidelity 收尾单同时关闭（attention 按 r58 迁移+排空断言；fidelity 判定产品合规、断言强化为恰 1 条）| 全树已知红清零 | S1b 派发：删保留投递+roles golden 迁移+净账复测 |
| r62-修法验证 | 竞态根因精化：WaitAnswer 的 check-then-read 窗口（缓存查后放锁做链读、再取 notify，段恰在其间闭合即读到新通道永等）。修法定稿：cachedAnswerLocked 与 c.notify 读取同一 c.mu 临界区，链读在后，select 该 notify。已在 /tmp/evolution-fix-tree 验证 3/3 绿（原 5/5 超时）。排程：attention+fidelity 回收后 lead 亲落（写集互斥纪律），验收=evolution 五测 -count=3 -race 组合复现命令 | evolution 单以迁移+证据交付 | 在途：attention+fidelity |
| r61-竞态裁定 | evolution 清扫暴露真竞态：WaitAnswer 丢失唤醒（settleSegment 锁内 swap notify、锁外 close 旧通道；等待者换后取新通道即永等，答案已在缓存/链）。5/5 复现，链证 output-25 inputs 含等待键而 Resting=true 仍阻塞。裁定(a)修产品，禁测试规避。修复设计：WaitAnswer 在同一 c.mu 临界区内"复查答案+取通道"（原子），或 settleSegment 锁内 close——前者为正解（答案先于 swap 入索引，原子复查必命中）。排 attention+fidelity 回收后 lead 亲修 | evolution 三文件迁移绿 | 在途：attention+fidelity |
| r60-两红关闭 | 两红 triage 完成亲验（count=10 稳定绿；单据 -count=100）：产品修复=delivery fact 在 result fact 已落链时不再重复携带观察体（回 HEAD 形状）+自驱段按 request-prefix 判定呈现，结果完整性不缩水；S03 links 漂移根因=旧 helper 解析回执文本而产品已改最小 ok，改事件派生。裁定：roles 6→5 漂移按全双工语义改 golden 接受（禁产品加等待特例，与自驱法则相悖）——golden 迁移列入 S1b 伴生 | 在途：evolution清扫/attention+fidelity |
| r59-A+完成 | 关闭 A+ 落地完成亲验（七锚 count=3 绿+assembly/runtime 短测绿）：三终态结清+DrainReport.Failed 点名+失败观察落链经命名唤醒进真实请求（无重驱环）；quiesce 三测迁移保全失败可见性断言。全 host 余两红：attention（r58 已批迁移）与 request_fidelity（基线绿/合并红，S1a 嫌疑）→收尾单派发 | A+ 语义闭环 | 在途：两红triage/evolution清扫/attention+fidelity收尾 |
| r58-边界裁定 | 关闭 A+ 单两边界：(1) 批准 runtime/agent/delegate.go 的 DrainReport.Failed []event.InvocationID 最小字段扩展（派单漏列文件，编排者责任；非新类型，A+ 点名失败落点）；(2) 批准 attention_lifecycle bootUnconsumedNoticeScene 迁移——旧前提"失败制造未消费"已被 A+ 废弃，迁为"通知从未被驱动"场景，保全"未消费产生问题/工作结束清除"契约（意图保全非缩水）。单完成后按此执行 | 在途：关闭A+(host short 后台)/两红triage/evolution清扫 |
| r57-清扫回收 | Drive 调用方清扫完成（8 文件迁移绿，target -count=3/-race 绿）。暴露两枚红：receipt_background_separation（r46 时绿→S1a 嫌疑）与 S03（S1a 前已红的记录漂移，仍无归因）→ triage 单派发；evolution 漏网 3 处直呼→清扫二单派发 | 与关闭 A+ 单并发，写集互斥 | 在途：关闭A+/两红triage/evolution清扫 |
| r56-A+裁定 | 用户裁定 A+：失败=结清（杜绝重驱循环），关闭报告区分干净/带失败收尾并逐条点名，且失败信息落为链上事件、按 S1a 自驱法则可驱动消费（失败可见可驱动，不吞）。S1a 完成亲验（净删 151 行，Drive 薄壳化，三自驱锚 race 绿；直驱旧语义断言删除获批=删死引擎断言非缩水）| 待办双单：关闭 A+ 落地 / 7 文件 Drive 调用方清扫 | 派发并发 |
| r55-S1a派发 | 依赖重审：S1 前两叶写集仅 runtime/agent，不等关闭裁定。叶A=Drive 并入 Consumer（重构型，绿套件为守卫，验收=行为零变+净删行数+无第二决策路径）；叶B=相关事实自驱法则（红锚三件：孤儿结算无新输入自开段且观察达真实请求/空闲挂载首事件自消费/无孤儿不得空转）。同簇一单内部串行 | S1b（删缓存恢复+retainSettleDelivery）仍候关闭 A/B | 关闭裁定仍待用户 |
| r54-1.4完成 | 1.4 规格同步完成：主 spec consumer-loop-convergence 两处旧表述改写（Run 直驱例外→全入口收敛；第二调用只回收据→同段同不可变结果），strict 141/141+D1 绿，01 域四叶全部闭合 | 编排者单写队列余：GLOSSARY 对齐 | 关闭 A/B 裁定仍待用户 |
| r53-C类归口 | C 类三条有效无归口条款完成归口：移交路由/稳定序号→async-task-execution；正式宿主端口缺失具名拒绝→architecture-guardrails；投递前审核四态→delivery-dispatch-receipts（各带 Scenario，strict 141→144 全过）。W4-02 遗留清偿 | 关闭 A/B 裁定仍待用户 | 编排者单写队列余：1.4 规格同步/GLOSSARY 对齐 |
| r52-门禁修复 | check-openspec.sh cwd bug 修复（脚本在 scripts/gates/，.. 只回 scripts/ 致"无可验证项"假绿 exit 0；改 ../.. 后从外部目录实测真实校验 141 specs）。编排者单写队列推进中 | 关闭语义 A/B 裁定仍等用户 | 其余面绿；点阵 4 子测红=已知暂停态 |
| r51-关闭屏障裁定 | 回归 triage 完成（A/B 定位 W2-07 两处引入，还原后四锚绿+assembly 绿+count=3 稳定），但 W2-07 点阵 4 子测红=两冻结契约在"关闭进行中"窗口真矛盾。裁定 c)：Cut=时间屏障——close 返回前孤儿结果仍消费（W3-01 锚），返回后迟到结果保持 owed（W2-07 点阵）；Run 暴露内部 close 完成态（禁新公共 API），sink 据此门控 | 实施单派发：八锚同绿为验收 | 方案 a=断言缩水否决；b=掩盖竞态否决 |
| r50-host二期 | host 迁移二期完成亲验（五锚 ok）：日志边界改段身份双证+segment open/close 双行诊断覆盖；query 按段定位对账；request_boundary 下限+结果完整性（loaded 结果任何情况必须到达）。包级剩余红全部收敛于 close/quiesce 四测=回归 triage 单唯一在途面 | 复核点2（receipts 移 closing+下限）接受，属同族迁移 | 关闭回归单成为最后红灯簇 |
| r49-S域CI完成 | S5 续单完成亲验（workflow 50 OK+新锚 10 OK+gates 全集 257 OK，S6 的 6.4 最终门同树过关）：5.4 short 十分片矩阵化（timeout 45→15，artifact 独立化全被 required pattern 收齐）；5.5 root 门唯一 owner（contracts 删三步）。护栏实证：旧钉先红后改、七条过时断言逐条留痕（意图保全） | 遗留：5.4/5.5 真实 CI 消费者锚需 push 后核；新锚文件 untracked 待编排者纳管 | 在途：关闭回归/host二期；随后 S1 拆叶 |
| r48-S6完成 | S6 三叶完成亲验（replay 整包含两新锚 ok；evidence 清单 189 行落盘）：6.3 容量内 O(1) 早退（allocs −4.8%/B −5.7% A/B 实证）；6.4 暂缓叶变异证红+存在性锚；6.5 唯一权威对照清单（16 spec/628 条逐条 disposition）。裁定：候选 B（Fold/Rebuild 无产品调用方锚）随 S4/4.4 统一删除；计划缺陷登记（6.2 冻结锚 TestFoldProductsReachActualModelRequest 不存在，实际证据为 host 三锚——R29 漂移实例） | 6.5 主 spec 改写归档阶段执行 | 在途：S5续/关闭回归/host二期 |
| r47-forced锚裁定 | S6 发现 6.2 打破 golden 合同锚 02.3/fold_product_no_self_trigger（A/B 证 HEAD 绿+6.2 红，锚钉旧 forced 语义）。裁定(a)：场景意图保全（不自触发/不二次折叠/不吞产物），Triggered 期望翻转 false，lead 亲修+复跑 | guarded 分支若同钉 forced 同步翻转；golden JSON 若另钉 forced 需另报 | 在途：S6(6.4/6.5)/S5续/关闭回归/host二期 |
| r46-回归警报 | 全量合流：28+ 包 ok，6 FAIL 行。S5 的 5.3/5.7 绿（repeat-index+race 全 33 包+wechat）；host 迁移单两锚绿且升级为链上双证。警报：TestCloseRefusesWhileModelInFlight 再红（W3-01 修复被 W2-07 关闭路径改动疑似破坏）+Quiesce 三测红 → 派回归 triage 单。S6 裁定落账：evidence 路径批准、不删 TestSegmentsCloseAtAgentOutput、spec 收敛归 S4。S5 续单（裁定 A 嵌入）+host 迁移二期已派 | 在途：S6/S5续/关闭回归/host二期 | S03 e2e 漂移待归口 |
| r45-裁定 | S5 写集扩展裁定 A（子代理运行中不可寻址，先行落账）：test_check_runtime_workflow.py 属已完成单（5.2）无并发写者，旧钉形态即 5.4/5.5 明令取代对象。护栏=最小 diff+意图保全（被改旧测试仍验原意图，过时断言逐条报编排者复核）+整文件复跑+逐条说明。S5 回报后据此授权或派 5.4/5.5 后续单 | S5 推进 5.3/5.7 中 | 在途：S5/S6/host 迁移/全量合流后台 |
| r44-D6实测 | 净账量化：vs dfdfa72e 共 82 文件 +4058/-1412（净增 2646 行），公共命名类型净增≈0——"行为更对但未更简"，用户的担心当前成立；翻转依赖 S1 删除性归一+S4 净减。host 两预存红判定=答案身份迁移遗漏（turn invocation=session-X#cN vs 旧期望 inv-*），与 execution 包同族同修 | 全量合流后台 job bash-158f4e920bae46fb；S5/S6 在途 | host 迁移单派发 |
| r43-S1就绪 | 叶C 完成亲验（锚 PASS，-count=3 稳定）：链重建 Turn 五字段+Trigger 与段答案逐字段一致，缺失键零值不伪造。1.3 全域闭环，S1 前置全满 | 下一轮：D6 净账+全树合流+S1 拆三叶冻结锚+host 两预存红 triage+S5/S6 派发 |
| r42-S1前置 | D-W2-07 完成亲验（12 子锚 PASS）：已记录终态（含 failed/cancelled）不再重驱；stopped-current 不 book/backflow/consume，durable owed 保留重启；关闭取消不落 attempt。两条 host 包预存红（TestPublicRuntimeEntry/TestFinalRoundRequestLandsOnTheDrivenRange，A/B 证与本单无关）= invocation 身份面迁移遗留，编排者认领 triage | S1 最后前置仅剩叶C | 在途：叶C；待办：D6 净账/全量合流/S1 拆叶 |
| r41-重写 | 按技能重写编排层：结论改删除性归一路径；S1–S6 写集簇矩阵吸收全部剩余叶（2.7/债项并入）；恢复原契约红线与双门放行 | 用户两次纠偏后对齐最初冻结结论 | 在途：W2-07/叶C；S5/S6 可即刻并行 |
| r40-W4 | 叶A/叶B 完成亲验（runtime/agent 107 测绿）：output 事实补五键稳定元数据；缓存淘汰按"链上有含 input_events 的 output"分类，不可恢复答案豁免。交界遗留：answerOfChain 尚不读五键，重建 Turn 仍缺元数据面 → 派叶C 收口 | 在途：W2-07；结构三轮案待用户确认 |
| r39-W4 | D-W4-02 完成亲验（--rule D1 exit 0）：persistent-event-loop 558→16 行零现行 SHALL/MUST，26 条逐条迁移账；与 01 裁定冲突的旧不合批条款按现行删除。遗留：C 类 3 条待编排者归口主 spec（handoff 路由/正式宿主端口拒/投递前审核）+check-openspec.sh cwd bug+D1 与 schema 词边界张力（复数 SHALLs 披露） | 真实树概念门全绿 | 在途：W2-07/叶A/叶B；02/2.7 孤儿自驱待入簇 |
| r38-W4 | D-W2-06 完成亲验（锚 PASS，三包 short 绿）：删除 roundFailed 反向否认链，提交后失败改 roundEndedBeside 记真实终态；提交前拒绝路径保留为负锚。红锚 fixture 经裁定性修正（shard 路径与 chmod 不可达问题），核心断言 booked=1/Running 未缩水 | Wave3 全部 8 单完结 | 在途：W2-07/W4-02/叶A/叶B |
| r37-W4 | D-W3-02 完成亲验（两锚 PASS）：缓存 miss 时从链上 agent_output 的 input_events 重建答案，邻键不误答，legacy 无关系不伪造。遗留两项裁定为后续叶：A=output 事实补 Terminal/Failure/FinalRound/DecisionCount 元数据（loop.go，写集空闲）→ B=仅完整可恢复闭合才可淘汰缓存（consumer.go），A/B 不相交可并发派 | 在途：W2-06/W2-07/W4-02 |
| r36-W4 | D-W4-03 完成亲验（15 测 OK）：mutation 生产者补 contract_digest/module_fingerprints，与 runner 同源同函数；正锚过公共 --bundle，负锚旧格式必拒。遗留：真实 CI 三 job 同 commit bundle 实测属外部证据 | 解除 W5-01 遗留冻结 | 在途：W2-06/W3-02/W2-07/W4-02 |
| r35-策略 | 用户裁定：全波次流动并发，叶彼此无关、粒度明确。固化 P1-P4：写集簇=派发原子、无栅栏、一叶一锚一会话、六域矩阵后重组队列 | 下一步：读六域 tasks.md 产出 叶→写集→依赖 矩阵并按簇合单 | 在途 5 单继续不受影响 |
| r34-W4 | D-W2-05 完成亲验（锚 PASS、assembly short 绿）：deliver 失败→Pending+义务保留，恢复补写驱动后续 turn。结构裁定：tools.go 的 sync.Map 保留投递（30s 窗/150ms beat）是隐藏后台机制，属临时；更净修法=Host.Quiesce rest 判定处 h.run.offerOwed(judge) 后删除保留投递。host.go 现属在途 D-W2-07 写集 → 简化叶 D-W5-A 排 D-W2-07 之后（同文件簇串行） | 在途：W2-06/W3-02/W2-07/W4-02/W4-03 |
| r33-W4-拆分 | 按用户要求最大化并发：Wave 4 = 4 单并发（解锁自 W3-01/6.2 完成），同文件簇（3.3 vs 2.4）显式串行，写集未核实叶不入波 | 未知写集叶先读域四件套再派 | Wave 4 在途 4 + W2-05/W2-06 重派中 |
| r32-W3 | harness 重启；lead 亲验 W3-01 三面全绿（死锁锚 ok/denies_not_running ok/runtime/agent+assembly short ok，含 r31 告警项），汇报丢失但成果完整，6/8 完成。W2-05 红于恢复路径超时、W2-06 红于实现未动，均已基于在树红锚重派 | 重派单明确先读半成品再续作 | 剩余：W2-05/W2-06 |
| r31-W3 | D-W6-01 完成：根因=commit 被拒仍 Record 执行产生无链 request 的 product；修 age_fold.go:239 快路径具名拒绝+:253 commit 失败返回错误。红锚 exit1→0，压缩/fold 五锚与 replay 全绿。告警：./assembly short 现红 TestCloseStopsTheResidentsTheRunMounted（A/B 已证非本单，consumer/loop/invoke 为 W3-01 在途改动），W3-01 回收时必须复验 | 写集 2 文件零越权 | 在途：W3-01/W2-05/W2-06 |
| r30-W3 | D-W6-02 完成：根因=Triggered 在 Volume 门后立即置 true 未等候选产品；修 age_plan.go:108 与 age_segments.go:275-279 为空即 return，forced 特例本树不存在故无可删。五例先红后绿（pre exit 1→post exit 0），replay -short/-race 全绿 | 写集 3 文件零越权 | 在途：W3-01/W2-05/W2-06/W6-01 |
| r29-W3 | D-W4-01/W5-01/W5-02 完成并经 lead 亲验（概念门 51 测、证据门 97 测、workflow 50 测全 exit 0）。W4 遗留：真实树 CLI 因 persistent-event-loop 残条按设计红，属 04/4.2；W5-01 遗留：真实 mutation 生产者补基线字段另派 04 叶；W5-02 遗留：真实 PR/merge_group 消费者锚属外部证据 | 三单写集未越权；W6-02 两次启动失败零残留，第三次派发中 | 在途：W3-01/W2-05/W2-06/W6-01/W6-02 |
| r28-W3-并发重排 | 用户裁定并发不足；按 R21 触碰集重排为 8 单并发（1 running+7 ready），互斥项显式 blocked | 写集两两不相交已核；新增测试独立命名 | 批量派发 7 子代理；凭据回收后 R30 偏差评估再进 fresh 审 |
| r27-W3-计划重整 | 按技能重排：域账目核销 1/2，Wave3=03 关闭单协调器；D-W3-01 三锚=两红锚先证死锁/假 Quiesced，修复后同锚转绿，负锚=Cut 后重 Close 幂等 | 依据 03 design D1/D2：预算覆盖一切 join、静止含在飞模型、Cut 不释放活 writer | 单写者子代理实施，完成后 fresh 审；其余叶仍 blocked |
| r26-W3 | Capture 封档缺失根因=seal 只挂 Loop.Drive defer；改为段结算先 seal 后唤醒等待者。execution 三测试按全双工契约迁移（失败隔离改顺序、迟到观察跨请求/冲刷到达）。learning/capabilities/runtime/agent/assembly 整包 exit 0，task-1 核销 | 03 域两红锚保留：TestCloseRefusesWhileModelInFlight 死锁（StopResidents→Consumers.Stop 无界 join，consumer.go:1215）与 denies_not_running 假 Quiesced；不弱化 | 下一波=03 单协调器/预算覆盖 join/静止含在飞模型；WaitAnswer 恢复实现仍待做 |
| r25-W2-02 | capabilities 五文件迁移完成，lead 亲验整包 exit 0；execution 测试写与 learning 只读调查并行，互不交文件 | 原生/文件/执行/正式封装/技能结果均保留公共正负锚；全双工结果完整性已实测 | task-1 未完成；等待 execution 包绿与 Capture 根因，再做新鲜全量复审 |
| r24-W2-02 | 原生结果丢失根因是最终回复前未 harvest 飞行期间到达的结果；补上 late harvest 后原生场景通过。剩余 capabilities 同假设测试交 grill-message 单写 | 临时事实链证明三结果均落链；driveNotice 交错证明最终请求在飞；错误旁路已撤回，仅保留闭合前 harvest | 五文件测试写面，禁止产品/OpenSpec/git；结果完整性不缩，capabilities 包验证后 fresh review |
| r23-W2-native-behavior | 干净 dfdfa72e 的 NativeTools 三场景 exit 0，当前缺失 observations 是本轮回归；裁定全双工下每个回执/结果最终进入真实请求，但不强求同一请求 | 基线 job bash-0e1706d445224ef4 exit 0；当前差异为第二请求 10→8/8→7 | 保留全部身份/正文/正负断言，改跨请求完整集合；任何结果永不到达仍红，禁止改数量凑绿 |
| r22-W2-01 | 将 Turn 删除活 Decisions、改 DecisionCount 的机械迁移派为单写者任务 task-1；54 个现有引用文件已登记共享 write_scopes | 答案设计审证明无兼容恢复捷径；实施禁止答案恢复/主规格/其他行为/git 写 | 单写者完成后由 lead 复跑并派新鲜审者；其他产品写仍 blocked |
| r21-W1-answer-design | 答案设计裁定：Turn 删除活 Decisions、改 DecisionCount；output 记录完整稳定闭合字段；cache miss 只按 input_events 恢复且零副作用 | 设计审证明 Binding/Request 不可由事实恢复；failed/cancelled/final-round/abandon 分开；legacy/malformed 不猜 | 先迁移 Turn 公共面，再接恢复读取；1.2/1.3 未核销，新增工作须独立复审 |
| r20-W1-02+03 | 修复空 Run.Invoke、取消错误、driving 非 material 漏记、公共 Drive adoption/boundary 键、无 key 正文去重与精确公共输入集合 | 当前 api/event、agent、assembly short 与公共 race/handoff 全 exit 0；1.1 无回归 | WaitAnswer 完整 Turn 恢复仍 blocked-design；Binding/Request/终态不由 input_events 自动恢复，等待独立答案模型裁定 |
| r19-W1-02+03 | 输出持久记录 input_events；handoff 同构读取；Run.Invoke/InjectEvent 经公共 Consumer；公共锚同时校验身份与输入键 | 全部目标场景、handoff、api/event+agent+assembly short、定向 race 均 exit 0 | 新鲜独立审者检查元数据兼容、合批归因与恢复；通过前不勾 1.2/1.3 |
| r18-W1-02+03 | 合单 Run 入口与输出因果元数据；公共 residentOutputAfter 同时校验输入键，handoff 为生产消费者 | 前序红：Run 两入口输出 inv-run；Run 试验曾使 handoff owed=0；撤回后恢复 | 写集限 api/event/meta.go、assembly/{host.go,host_tools.go,host_delivery.go,invoke.go,run.go}、runtime/agent/{loop.go,loop_test.go,consumer.go}、tests/integration/host/{send_cross_session_delivery_test.go,resident_entry_test.go}；禁其他文件 |
| r17-W1-02 | Run 红锚成立但实现引入 handoff 回归；输出缺实际输入键关系，1.2 产品改动撤回、红测试保留 | TestHandoffRedrivesStalledOwedRecords 曾 owed=0；从 Git 对象恢复后 exit 0；1.1 面不变 | 1.2 blocked-dependency；1.3 先建立因果关联，禁止按位置猜答案 |
| r16-W1-01 | 新鲜末审 frontier 空，1.1 核销；同身份双 send 经 AdmissionDedup，五授权文件无新增公共类型 | 审者授权 race 均 exit 0；基线/工作区时点明确，未提交不冒充提交树 | 释放 1.2 的规划与红锚；其余叶继续 blocked，不归档 |
| r15-W1-01 | 末审负锚缩水成立：CommitToSession 不等于第二次 send；改为同一模型决策内同 CallID 双 send，覆盖正式工具链；读取面补 runtime/agent | 负锚现在断言同身份两次正式调用仍一条事实/一次有效驱动；无产品代码改变 | 复跑负锚与全部公共锚，新鲜一致性复核后决定放行 |
| r14-W1-01 | 两个 Minor 已修：接管文案改为不可替换；stopped-existing 公共锚绿，定向删除前置 stopped 检查后红 2.00s，恢复后绿 | 末轮 runtime+send race 均 exit 0；runtime/agent 2.464s、assembly 33.380s；diff 五授权文件 | 全新末轮审者检查；frontier 空前不勾 1.1，其他叶仍 blocked |
| r13-W1-01 | 单 current 复审无 Critical/Major；修正任务陈旧接管文案，补 stopped=true 且 existing 未 join 的公共 Submit 锚 | 审者两次指定 race 均 exit 0；无新关键死锁/丢失 | 新增 TestStoppedSetRejectsSubmitBeforeExistingInstanceJoin，零新事实；新鲜末轮复审后决定放行 |
| r12-W1-01 | 删除 residentLoops/lifecycle，单 current 首次装配且 Host 用实际返回；停止 tombstone、existing hit stopped 检查、锁外 join 全部落地 | 工具 Wake×Stop 红 2.00s→绿；停止清空/替换、集合挂载、send/race、相关包均 exit 0；五授权文件 | 新鲜单 current 审者复核中；1.1 仍未勾选，Backflow/重入 Close 明确归 02/03 |
| r11-W1-01 | 设计审 Q1–Q3 全部采纳：单 current、一次性装配且调用方可知、stopped tombstone、锁外 join；Submit existing hit 也查 stopped | 设计审给出工具 Wake×Stop、attached Stop→clear/replace、Backflow、Host 错缓存、sender 自 join 八类公共交错 | 新增 TestStopResidentsAllowsWakeFromConsumerTool；Backflow 归 02，sender Close 归 03；完成前不放行 |
| r10-W1-01 | 第三审命中同类 Critical/Major：StopResidents 持 lifecycle 等 Consumer.done，Consumer 内 WakeAddress 等同锁，ctx 无法解除 | 指定 race 仍 exit 0，但静态交错明确；达到同机制第三次返修停线条件 | 停止产品写入；改为无锁 join/陈旧集合重试的设计审查，pilot 与其他 32 叶全 blocked |
| r9-W1-01 | 新增停止后清空/替换、挂载后替换三类红锚均先失败；私有 lifecycle 边界与挂载权复查后全绿，无新公共类型 | 相关包 runtime/agent 2.471s、assembly 33.498s；两轮定向 race exit 0；diff 仍四授权文件 | 第三名 fresh 审者复审；若同机制再出关键问题，pilot 停线升级 |
| r7-W1-01 | 停止红锚先返回 nil/模型完成，保留 stopped set 后具名拒绝；补未挂载/恰一输出/parked 回执锚；定向变异只 Wake 时未挂载测试红，恢复后绿 | runtime+公共 race 均 exit 0；相关包 runtime/agent 2.475s、assembly 33.196s；无新增公共类型 | 第二名新鲜审者复审中；仍不勾 1.1，其他 32 叶 blocked |
| r5-W1-01 | 公共红→绿完成但尚未放行：旧树输出 #sent 且 exit 1；最小修复后输出 #c2 且 exit 0；幂等/忙闲、相关包和 race 均通过 | 红 0.10s；绿 0.12s；runtime/agent 2.523s、assembly 33.360s、公共 race 1.903s；diff 仅三授权文件，无新增公共类型 | 新鲜只读审者复核 WakeAddress/关闭/集合所有权；通过前 1.1 不勾选，其他叶仍 blocked |
| r4 | 独立规划复审不放行；采纳 Q1–Q6，仅放行 pilot 1.1，其余 32 叶 blocked | 24 个计划测试符号不存在；merged-spec/delta、Python 发现、触碰集与活动变更启动门均有具名缺陷 | 以 dfdfa72e 为唯一实施基线；先取真实公共红证，不批量实施 |
