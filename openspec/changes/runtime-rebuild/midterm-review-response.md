# 中期复审响应与计划修订记录（2026-10-15）

> 输入：用户指令「进展过半，做一次中期 review……目前的设计和实现是否能充分响应这一预期，是否在设计上就能防御之前我们遇上的各种问题」→ 复审结论交付 → 用户指令「重新修订计划」（即对修订取向的裁定）。
> 复审方式：单次全局 CodeReview＋主编排者逐项核实（静态调用链＋五组现有测试 `-race -count=1` 复跑全绿）。外部批准计划 `运行时_重设_实施计划_304e54c3.md` **只读不动**；本修订只改内部规划资产与任务账。
> 本文件是本轮修订的裁决单点记录（与 report-response.md 同位阶）；派发依据仍是 ORCHESTRATION.md＋task-graph.json。

## 〇、核心裁决

**域机制成立 ≠ 叶任务完成。** design D8 的叶任务完成口径是「实际产物＋**生产消费者**＋点名测试」，且原计划 §10 明写「孙任务完成需要实际产物、正式消费者及点名验证」。本轮复审证实：一批已勾叶的点名测试**由测试自身手工补接线**（Declare/Approver/Recorder/保护句柄）或只驱动领域组件，**正式公共入口（Assemble/Invoke/Publish/Close/Query、SDK/CLI）没有消费这些保证，甚至可以绕过它们**。据此：

1. **重开 31 个叶任务**：23 叶为**补差重开**（缺正式消费者，见 §二）＋8 叶为 **M 回归重验**（02.7/03.5/03.6/04.3/04.6/07.2/07.3/09.2——实现不判错，但其前置被重开，完成证据建立在待重构上游之上；check_runtime_plan 的 done-with-unfinished-dep 硬拦亦要求一致，见 §四）。全部 status done→planned，evidence 作废留档于台账批次 8–15；域内实现**保留不推倒**，补差只做正式入口接线与负向反例。
2. **验收纪律修正**：四查→**五查**，新增「生产消费者实测」——完成证据必须含正式公共入口的**负向反例**（错误路径走不通），测试手工补线不构成生产消费者。
3. **实施优先序改纵向收拢**（M1–M5 波），暂停按完成数扩展功能；先闭合正式链路，再续剩余 NN.M 与 09 验收。
4. **镜像接缝零容忍**：`agent/request_assembly.go` 的 `ProjectedEntry` 为禁 import 镜像字段，直接违反 design D1 被否方案——删除并改为注入式请求装配面（签名用 model 类型，实现挂组合根/memory 侧）。

## 一、系统性缺陷归纳（复审发现 → 防御缺口）

| # | 缺陷 | 为何防不住 | 纪律修正 |
|---|---|---|---|
| S1 | 正式入口缺治理/审批/统一准入（standGovernance 固定通过、file/skill 绕 wrapper、插件 nil） | 保证写在可选接口里，消费者可绕过 | 五查之五：正式入口负向反例 |
| S2 | 测试手工补线冒充生产消费者（durable 测试补 Declare、governance 测试补 Approver/CallPass、副本测试手工 Acquire） | 只查了「组件能工作」没查「生产链在用」 | 完成证据须指明正式入口调用路径 |
| S3 | 追认缩水替代原始目标（04.7「真实源」推后、07.4「结构见证」替「Eval/Benchmark」） | 裁决记录没有回扣原任务行的验收面 | 重开时缺口逐条写回任务行（M 重开标注） |
| S4 | 镜像接缝复发（ProjectedEntry） | 禁 import 的架构约束压过了语义边界 | D1 重申：包边用组合根/注入缝解决，不造字段镜像 |
| S5 | 勾选证据缺原始口径（"八包/十包"漂移、部分 evidence 无原始退出码） | 四查未覆盖生产消费者，数字溯源止于测试名 | evidence 必附验收命令原文＋原始退出码＋正式入口路径 |

## 二、重开清单（23 叶）与补差合同

> 「保留」＝已验证的域内机制不推倒；「补差」＝重派时的唯一工作面；「负向锚」＝完成证据必须新增的正式入口反例。M 波归属见 §三。

| 叶 | 保留（域内已验证） | 补差合同（缺什么） | 负向锚（正式入口反例） | M |
|---|---|---|---|---|
| 01.4 | 候选构造/失败逆序释放/StatePlan 借用面 | 正式根治理注入（替 standGovernance）；Loop 经 Publisher.Borrow（退役推进）；Query 租约/配置读源统一到当前 owner | 无治理配置的候选发布被拒（正式 Assemble→Publish） | M1 |
| 01.5 | Publisher 发布/基线复核/同内容不换代/退役账 | 正式发布门不再固定通过：必需检查缺失→拒绝发布 | 正式 Publish 缺 eval 证据被拒、旧代不变 | M1 |
| 01.6 | 三通道分路/文件固化版本/Cause 链 | prompt/skill 正文进入实际 GenerateContent（下一请求读新版本可见于模型请求） | 文件改动后下一请求的 Request 携带新版本正文（假 provider 断言） | M1/M3 |
| 02.3 | wrapper 受理/审批等待/dedup/拒绝配对 | file/skill/FunctionTool 全部经 task.Wrap 统一受理；正式根接 Approver | 无 approver 时严格档拒绝而非 RulingRun；file/skill 调用产生真实 Receipt | M1 |
| 02.5 | 单一归约驱动/多工具配对/invocation 隔离 | Receipt 进 transcript；后台结果独立观察回流（不冒充第二次同步响应）；受理循环不被 await 阻塞；正常回流走 mailbox 接纳；跨 invocation 共享会话历史；answerText 前缀改写→原样透传 | 慢工具在途时后续工具已受理（Receipt 可见）；二次 Invoke 请求含前次历史 | M2 |
| 02.6 | Before 恰一次/最终参数执行与记录/denied 收束/After 时序 | pass.Risk/CallPass 接入正式策略裁决（Policy.Judge）；After 的 Presentation/Gaps 交正式呈现/诊断消费者 | strict 违规调用零执行（正式 Loop）；观察失败缺口可从 Query/诊断读到 | M1 |
| 02.8 | 通知身份 seq/冻结路由/关闭保留/重试不改投 | Handoff 记持久 mutation（重启后未来通知走新目标）；通知调用归属取持久材料（不依赖运输 ctx 的 CallFrame） | Handoff→重启→新通知投新实例、旧通知仍旧实例（独立进程链） | M2 |
| 02.9 | 三段关闭/义务结算/producer 确认/幂等 Close | 正式 Invoke/排队/直接工具/模型 producer 登记进 Shutdown（Admit/Watch）；cancel≠停止（WithoutCancel 的 execute 有真实停止确认） | 模型在途时 Close 返回 Drained=false＋具名未收束 | M2 |
| 03.3 | DurableCommit 幂等/三态/重放/owner 解析 | Register 首提自包含 Spec（同一受理提交落盘，不需外部 Declare）；Restore 缺 Spec 具名拒绝（不报 Recovered）；正式根提供持久档装配（配置驱动 tier） | 只经正式 Register→重启→Restore：Spec 在链、缺 Spec 链被拒 | M2 |
| 03.4 | 投影纯回放/预算组装/因果隔离 | 投影/预算进入正式 decide 请求构造；**删 ProjectedEntry 镜像**（注入式装配面） | 正式 Invoke 的 Request.Messages 含预算投影结果 | M3 |
| 03.7 | 启动/周期清理/不删用户文件/不跨链接 | 活跃保护随物化/读取生命周期（打开即持保护句柄，读闭才释放），不靠调用者手工 Acquire | 物化与清理并发下无「protected vanished」窗口（无需测试补 Acquire） | M4 |
| 04.1 | file/skill/FunctionTool 注册/加载状态回 state/文件固化 | 统一经 task.Wrap 受理（真实 Receipt）；技能正文进入下一请求模型上下文 | file/skill 调用被正式准入拒绝路径覆盖；技能 Body 出现在下一 Request | M1/M3 |
| 04.5 | warn/strict/Hold≠Denied/审批身份/ApprovalManager | Policy.Judge 接入正式 Before 裁决（不靠 rig 手工映射） | 正式路径 critical 调用恒 Hold、strict 违规零执行 | M1 |
| 04.7 | QueryEvents 读端口/薄映射/RRF 降级/embedder 缝 | canonical event→QueryEvents 真实存储源接线（Body schema 承载落定；embedder 真实端点仍资源门） | 真实存储写入事件可经 search/QueryEvents 召回（非 MemoryEventQuery 演示源） | M4 |
| 05.1 | Inventory 六维/统计派生/SourceError 具名 | 公开投影保留真实状态维度（不压成 active）；配置 agent 随 Publish 更新；租约读当前 owner 账 | Publish 新 agent 后 Query 可见其 configured 态 | M4 |
| 05.2 | 授权过滤/不可变快照/稳定分页/gap-reset | 单快照对象/字节预算（不先全量物化再分页）；超界具名截断或拒绝 | 大明细下小页请求不物化全量（预算断言） | M4 |
| 05.3 | 观察分层/Skipped 留痕/关闭 OTel 不影响 Query | telemetry/CommitObserver 挂正式装配；After 产物正式消费（缺口可查） | 正式 Invoke 后审计记录可读、观察缺口可查 | M4 |
| 06.1 | 版本化记录/Line 信封/旧格式导入 | 正式根挂 Recorder；Invoke 带 Episode 输入；记录失败进显式 loss/gap 账（不静默 `_=`） | 正式 Invoke 产生 episode/decision/attempt 三记录 | M4 |
| 06.2 | 安全快照/脱敏/恒拒重跑/logprobs 三态 | 请求边界固定安全快照接 GenerateContent 前（正式链） | 正式请求的快照含真实 Messages/Tools 且无 secrets | M3 |
| 06.3 | 有界准入/丢失账/同 protocol 封账/真 fsync | CaptureSink/writer 接正式生产链（记录经统一采集入口） | 正式链产物经 Seal 封账可复核 | M4 |
| 06.4 | 授权导出/derive-on-change/两账分开/manifest | ExportSource 接正式工件链（非 fake source） | 正式链导出样本可溯源到正式记录 | M4 |
| 07.4 | 同源声明/同代绑定/Unwrap 链/GateReport 四要素 | Eval/Benchmark 有真实判据（候选行为/对比基准），不以 Completed 探针冒充；ApplyGate 消费既有评估证据（不重发探针） | 内容错误但 Completed 的模型不能使 Eval 通过 | M4 |
| 08.1 | 唯一 Assemble 四面/CLI 薄壳/Close 两段 | /query 请求主体认证（未配认证不开诊断口）；Query 读源随 05.1 修正；在途关闭随 02.9 修正 | 无凭据请求 /query 被拒（401/具名拒绝） | M1 |

## 二·1 负向锚具名测试（机器硬门，2026-10-15 增）

> 23 叶的负向锚已固化为**具名测试**并写进 task-graph `verification`；`check_runtime_plan` 新增两规则（测试锚存在性＋verification 全覆盖 evidence）使它们成为**勾选硬门**：负向测试不存在（空转/改名漂移）或未被 evidence 点名（只跑一半），检查器直接红。对齐规则（exit_codes↔commands）已有。

| 叶 | 负向锚测试（包） | 断言核心 |
|---|---|---|
| 01.4 | TestFormalAssemblyRequiresGovernance（host） | 无真实治理时正式 Assemble→Publish 被拒 |
| 01.5 | TestFormalPublishRefusesMissingEvidence（host） | 缺 eval 证据的正式 Publish 被拒、旧代不变 |
| 01.6 | TestFileChangesReachNextRequest（host） | 文件改动后下一请求 Request 携带新版本正文 |
| 02.3 | TestFormalWrapperAdmission（capabilities） | 无 approver 严格档拒绝而非放行；file/skill 产生真实 Receipt |
| 02.5 | TestReceiptAndBackgroundStaySeparate（execution） | Receipt 进 transcript、后台结果独立观察；受理不被 await 阻塞 |
| 02.6 | TestFormalLoopEnforcesPolicy（capabilities） | strict 违规在正式 Loop 零执行；After 缺口可查 |
| 02.8 | TestHandoffRoutingSurvivesRestart（execution） | Handoff→重启：新通知投新实例、旧通知不动 |
| 02.9 | TestCloseRefusesWhileModelInFlight（host） | 模型在途 Close 返回 Drained=false＋具名未收束 |
| 03.3 | TestRestoreRefusesChainWithoutSpec（history） | 正式 Register→重启→Restore：Spec 在链；缺 Spec 链被拒 |
| 03.4 | TestRequestCarriesProjectedBudget（host） | 正式 Request.Messages 含预算投影结果（镜像已删） |
| 03.7 | TestReplicaLifecycleProtectsActiveUse（history） | 物化/读取生命周期自带保护（无手工 Acquire） |
| 04.1 | TestNativeToolFamiliesAdmitted＋TestSkillBodyReachesNextRequest（host） | file/skill 统一受理；技能正文入下一请求 |
| 04.5 | TestCriticalCallsHoldThroughFormalPath（capabilities） | 正式路径 critical 恒 Hold、strict 违规零执行 |
| 04.7 | TestSearchRecallsFromRealStore（capabilities） | 真实存储写入事件可经 QueryEvents/search 召回 |
| 05.1 | TestQueryShowsPublishedAgentState（host） | Publish 新 agent 后 Query 可见真实状态维度 |
| 05.2 | TestSnapshotPagesWithinByteBudget（inspection） | 小页请求不物化全量、超预算具名 |
| 05.3 | TestObservationReachDiagnostics（inspection） | 正式 Invoke 后审计记录/观察缺口可读 |
| 06.1 | TestFormalInvokeProducesTrainingRecords（learning） | 正式 Invoke 产生 episode/decision/attempt 三记录 |
| 06.2 | TestFormalRequestSnapshotHasNoSecrets（learning） | 正式请求快照含真实 Messages/Tools 且无 secrets |
| 06.3 | TestFormalChainSealsCapture（learning） | 正式链产物经 Seal 封账可复核 |
| 06.4 | TestExportTracesFormalRecords（learning） | 导出样本可溯源到正式记录 |
| 07.4 | TestEvalRejectsWrongContentCompletion（evolution） | 内容错误但 Completed 不能使 Eval 通过 |
| 08.1 | TestDiagnosticEndpointRequiresAuthentication（host） | 无凭据请求 /query 被拒 |

## 三、M 波恢复实施优先序（纵向收拢）

> 图边（task-graph depends_on）仍是硬前置；M 波是**就绪后的派发优先序**。共享写簇 `run.go`/`prepare.go`/`agent/loop.go`/`task/wrapper.go` 消费面在 M1/M2 内**单写者或串行**（外部计划 §8：共享接线优先一单纵向闭环，不按域强拆）。

- **M1 安全与唯一入口**（01.4、01.5、02.3、02.6、04.5、04.1-准入、08.1-auth）：正式治理注入、统一工具准入、审批裁决接线、HTTP 认证。产出＝正式入口的错误路径走不通。
- **M2 执行与恢复闭环**（02.5、02.8、02.9、03.3）：Receipt/异步观察、Spec+Handoff 自包含提交、mailbox 同协议、真实排空与退役。
- **M3 唯一请求边界**（01.6、03.4、06.2、04.1-正文）：prompt/skill/历史/预算/安全快照进同一实际请求；删镜像接缝。
- **M4 派生读面与观察链**（05.1、05.2、05.3、06.1、06.3、06.4、03.7、04.7、07.4）：Query/RL/OTel/后验/副本消费真实事实。
- **M5 剩余功能与 09 验收**（03.8、04.2、04.4、04.8、04.9、05.4、06.5–06.8、07.1、08.2–08.5、09.1–09.6）＋既有 done 的影响回归（02.7、03.1、03.2、03.5、03.6、04.3、04.6、07.2、07.3、09.2）。

## 四、M 回归重验（8 叶）与保留不动

**M 回归重验**（原拟保留 done，因 check_runtime_plan「done 任务的前置必须全部 done」硬拦＋完成证据依赖待重构上游，改判重验；实现不判错，工作面=上游收口后重跑回归锚＋必要影响适配）：

| 叶 | 上游重开 | 回归锚（重跑绿后重新勾选） |
|---|---|---|
| 02.7 | 02.6 | TestDelegationCompletion 不破 |
| 03.5 | 03.3 | TestRecoveryBeforeReclamation 不破 |
| 03.6 | 03.4 | TestReplicaMaterialization 不破（03.7 保护句柄接入 replica.go 一并） |
| 04.3 | 04.1 | TestMCPProtocolLifecycle 不破（MCP set 进正式装配一并） |
| 04.6 | 04.5/03.3 | TestGovernanceRecovery 不破（Policy 接线一并） |
| 07.2 | 03.3/01.6 | TestGitNativeRefine 不破 |
| 07.3 | 05.3 | TestPostActivationEvidence 不破 |
| 09.2 | 03.3 | TestOfflineMigration 不破（Spec 自包含后迁移写面复核） |

**保留不动**（无未完成前置、实现与验收均未被判错）：全部 00.x/P0/R 批次与 **01.1、01.2、01.3、02.1、02.2、02.4、03.1、03.2**。

## 五、待核项（复审未证实，不写成结论）

1. Policy 按工具名＋参数 digest 查询批准记录是否跨调用复用——随 M1 审批接线一并核。
2. Query lease 授权归属与披露规则——随 05.1 补差核。
3. 副本目录父级链接/TOCTOU/启动遗留暂存件——03.7 补差时全列负向例。
4. Handoff Source/Target/owner 解析完整语义（04.4 地址化投递前）。
5. 07.2 Git 安全边界（预暂存/部分暂存/无关修改）与 07.3 激活生产点纵向消费。
6. 历史执行日志是否存在管道吞错——09.3 收尾时以原始退出码逐条复核。
7. `agent→memory` 架构边的最终形态（镜像删除后走组合根装配还是 policy 增补边）——M3 落地时按「先证据后改 golden」裁决并留档。

## 六、本次修订执行记录

1. task-graph.json：31 叶（23 补差＋8 M 回归）status→planned、evidence 移除（run_id 留档于 ORCHESTRATION 台账批次 8–15）、notes 追加重开缘由＋补差/回归短语。
2. 各域 tasks.md：对应 31 行取消勾选＋行尾「【M 重开：缺…】」或「【M 回归重验：…】」标注。
3. ORCHESTRATION.md：结论层改写（最大风险＝正式消费者断链）、次序层增 M 波、凭据层四查→五查、台账批次 16。
4. design.md：D8 增补完成口径强化（正式入口负向反例＋镜像零容忍重申）。
5. 校验：check_runtime_plan ok（103/103 计数不变；done 52、就绪面 {01.4, 02.3, 03.3}）、openspec validate --strict 通过、勾选↔status 一致性由检查器核。
6. **偏离拦截机制（同日增，响应「及时拦截、返工止于单内」）**：check_runtime_plan 新增测试锚两规则（存在性＝拦空转/改名漂移；全覆盖＝拦只跑一半）＋正反样例自测（142 OK）；23 叶负向锚具名化入 verification（§二·1，勾选硬门）；CI 常驻 `check_runtime_architecture --implementation`；数据对齐：00.16 evidence 命令面补点名 TestSessionAuthority（当时实跑，数字溯源修正）。
