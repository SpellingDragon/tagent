# 交付闭合（事件化）：技术要点

## 既有能力裁决（5.9）

- `delivery-dispatch-receipts`（2026-10-04 归档）与 `delivery-closure` **不重合、互为对侧**：receipts 管入站感知面（渠道收到了吗、谁签收、回执事件），delivery-closure 管出站责任面（系统还欠什么、义务生命周期）。交叉条款一致：结果未知不得自动重发（两边同文）；发送动作身份沿用 receipts 的动作轴。跨 spec 消费者锚=`TestHeldSettlementReachesModel` 断言结算事件以回执同族形式进入模型消费。receipts 的 MODIFIED **实际形态=全文保留既有条款+「活进程内」限定词+重启补送追加句**（不是只追加）。
- `failure-notice`（兜底两支，r8 F1 定版；r9 Blocker 1 收口）：① **失败告知**：既有 failure-notice 照发（条件=failed+无正文+有原因，**无义务条件**；01 不新增第二条通道）；② **延迟告知**：**存在未清 held 交付义务 × 该义务的驱动轮结束无面向用户产出**时，01 新增恰一条最小延迟告知，话术按 held.reason 分层（`retrying`→稍后必补送；`approval`/`unknown`/`retry-exhausted`→已转人工处置），MUST NOT 与模型产出重复，MUST NOT 对未知/等审批承诺自动补送。支②**不是失败告知**：`failure-notice` 只管失败告知的措辞；支②仅以 failure-notice「成功但无正文保持静默」场景的豁免句衔接——**该场景 MODIFIED 准入**（未清 held 义务时按延迟告知恰发一条，非失败话术），不新增通道、不改 failed 触发条件；支②也 MUST NOT 以“成功终态”为前提（body 存在⇒owed 随 agent_output 在链上、send 失败落 held，该前提不可达），**且不限于 answer 轮终态**（任何驱动该义务的轮结束且无面向用户产出均触发）。MUST NOT 为**悬空无终态**发框架通知（1.4b 实测；若暴露无人知晓的悬空轮=法定停点上报）。
- `cross-session-delivery`/`quiet-receipts`：条款不改。

## 文件清单

- assembly/host_delivery.go：交付结算事实族（**族在所属包 assembly**，身份=交付身份字符串【由 fact key 确定性导出——见下】，类型=`delivery.settlement`；FACT-VOCABULARY 登记行照此写——身份用纯字符串从而不依赖 event 契约，assembly 既有 import 不受影响）+ DeliveryConsumer + 迁移
- **runtime/agent/request_assembly.go**：`delivery.settlement` 的类型→角色映射（RoleOf；未知类型会被 ErrProjectionTypeUnmapped 拒绝——不注册则 TestHeldSettlementReachesModel 必挂）；**类型字符串常量 `projectionTypeDeliverySettlement` 为 runtime/agent 本地常量（F13）**：冻结于本 design、注释指回 assembly/host_delivery.go 的族定义；不 import assembly（反向依赖禁止）、api/ 零改动
- **memory/ledger 生命周期注册**：TypeTTL 目前仅有声明、无生产挂载（全仓无生产写入者），注册负 TTL 是**前瞻豁免**——测试断言注册形态；**恒可回读为两段式：当前由 Lifecycle 未挂载保证；挂载后由负 TTL 豁免保证**；`TestSettlementTTLEntryRegistered` 注明“断言注册形态+debt”。
- runtime/agent/loop.go：commitOutput 的提交载荷携带 owed 声明（Meta 字段）
- **memory/query_events.go（F3/F11 定版）**：`QueryOptions` 新增**未导出字段 `keyPrefix`** + `Prefix()` 读取方法 + `WithKeyPrefix()` 值拷贝设置方法（不新增导出字段）；结算事实以该前缀过滤**返回集**（`delivery.` 前缀）；扫描面是 `r.facts` 事实窗口内，代价 **O(事实窗口内)**，过滤本身不改变扫描量
- **assembly/run.go（F2/F3/F13 定版）**：`r.replays` 的会话注册枚举（同包读面）——**映射序列仅用于会话枚举**；恢复枚举的 owed 声明读 **`r.facts` 事实窗口内内 agent_output 的 Meta**（`r.facts` 本就是事实窗口内事实表），代价 **O(事实窗口内)**；查询 `PartitionIDs` 由 replays 注册键经 `PartitionIDFromName` 派生（分区来自 replays 而非盲扫）。**该事实窗口内面与 03 的 1.2a 共用；跨域集成测试归 03 的 1.2a（`TestAssemblyObservedReplayFactsBounded`/`TestAssemblyColdWindowReadback`）；本域用 `TestSettlementFactsStayHotWhileUnsettled` 锁定未清交付事实驻留。**
- assembly/host_delivery.go / host.go：恢复读链、审批落事实、兜底告知
- **changes/01-delivery-closure/specs/delivery-dispatch-receipts/spec.md**：MODIFIED「发送动作身份与未确认结果」（全文保留既有条款+活进程内限定词+重启补送追加句）；**changes/01-delivery-closure/specs/failure-notice/spec.md**：MODIFIED「成功与静默边界不得被失败话术污染」（全文保留+「成功但无正文保持静默」场景的延迟告知豁免句）；**本域归档时含四个 delta：delivery-closure、delivery-dispatch-receipts、fact-vocabulary、failure-notice**
- assembly/boundary_closure_anchor_test.go 等测试：三锚转绿+负锚（锚①另断言收件方收到恰 1 条失败通知，由失败告知（支①）承担）
- docs/FACT-VOCABULARY.md、docs/wiki/mechanism-tour.md、docs/wiki/agent/agent-architecture.md、docs/GLOSSARY.md：登记与概念同步

## 关键决策

- **事实键结构**：`delivery.<交付身份>.<终态>`——键含状态，状态转移=新事实追加（同键同内容幂等、同键异内容冲突拒收）；按 `delivery.<身份>.` 前缀回读全状态线；结算事实 Content=一行可读状态行。
- **形态裁决**：**混合形态**——**新输出**的 owed 是 agent_output.Meta 里的**声明**（目标+血统；单一事件一次提交，构造性原子）；**交付身份**由落链后的 fact key 确定性导出（`freshObligationOf` 家族，host_delivery.go:798-872 同源规则），**不要求在提交时刻预知**；**结算四态（held/sent/refused/headless-retired）＋迁移专用 owed 态**（held 的 reason ∈ {approval, retrying, retry-exhausted, unknown}；retrying=首次重试前写入的在途 held，成功后由 sent 覆盖对账；retry-exhausted 是重试耗尽的 held，二者均非独立终态）为独立事实族（追加式、同身份幂等）。**“owed 事实”仅存在于迁移**：旧 KV 记录指向无 Meta 的历史输出，迁移为携带 owed 态的结算事实（一次性、幂等）；新输出永不另立 owed 事实。崩溃恢复=按链读 owed 声明与结算事实对账。
- **提交入口（r5 F25）**：结算事实经 run 的**既有事实提交入口**进链与投影（SessionFacts 可见）——**不经 ledger 裸 Put**；提交失败走与 agent_output 同一失败面。
- **可见性统一口径**：**结算事实**（四态：held/sent/refused/headless-retired，一行 Content；迁移专用 owed 态仅作历史迁移事实、非常规呈现态；held 的 reason ∈ {approval, retrying, retry-exhausted, unknown}）MUST 进入模型消费与投影呈现（回执事件同族待遇——感知是构造保证；类型→角色映射由 1.6b 注册）；**owed 声明随 agent_output 本体呈现，无独立可见面**；**管道记账噪音**（重试计时、中间探测、迁移记账）MUST NOT 进入投影可见面。折叠豁免=事实可恒按身份回读（限定见上述 TTL 条款）。
- **错误分类（互斥判定表）**：**明确失败**=渠道确定性拒绝（连接被拒/鉴证失败/渠道明确否定完成；特征：发送动作未开始或渠道明确报错，且非超时形状——`net.Error.Timeout()==true` 一律归未知）；**未知**=超时、中断、错误不可辨。分类器是唯一入口，正锚用“拒连非超时”形状，负锚用“纯超时”形状。重试每 attempt 携带动作身份并关联原身份；**首次重试前即写 held(reason=retrying) 结算事实**（成功后 sent 覆盖对账；重试间隙不是不可见状态）；redrive 只在**未确认 sent** 时发生，不违反 receipts“已确认不重发”。负锚=**`TestUnknownResultNeverAutoRedriven`**（纯超时形状；F19 名称与 tasks 逐字一致）。**恢复补送语义**：重启补送遵循 **at-least-once**，仅针对**无结算事实 ∪ held(reason=retrying)** 的义务——中断在退避途中的义务重启后续跑（不搁浅），发过未销账窗口内可能重发一次（`docs/wiki/reliability/durable-delivery.md`“至少一次容许一次多投”同义）；**已确认 sent 的义务绝不重发**。与 receipts 的“未知不自动重发”不冲突——后者约束活进程的未知结果处置，重启补送是文档化的恢复路径；收方按身份去重是通道契约假设，写入 ADR 0002 注记。
- **消费驱动**：DeliveryConsumer 读链驱动 owed→send→settle；**DeliveryConsumer MUST NOT 以 ObserveCommit 自监听来驱动自身——退避是状态机自持计时（不靠新事实唤醒），结算事实是只读结果面**（`TestDeliveryConsumerNotSelfTriggered`）；明确失败退避 1s/4s/16s，上限 3 次落 held；**首次重试前写 held(reason=retrying)**，耗尽落 held(reason=retry-exhausted)；held 结算事件必达模型（感知层）。**DeliveryConsumer 持有的义务恒驻事实窗口内**（未清交付事实不落事实窗口外；held/owed 活动集驻留），该驻留是 03 的 1.2a 事实窗口内包含全部未清交付事实的机制。
- **旧 KV 迁移**：**旧 KV 六态**映射——owed→**迁移专用 owed 结算事实**（仅历史输出，见形态裁决）；sent/refused→对应结算；held/retry→held 结算（retry 的 reason=retry-exhausted）；**headless→headless-retired 结算（不重发、正文留档审计）**；迁移幂等（重启两次不双建，负锚）；不可解析→具名拒启动（不猜）。迁移后旧 KV 只读留存，新恢复路径不再扫 KV 前缀（负锚锁定）。
- **恢复枚举（F2/F3/F14/F15 定版；诚实代价）**：多会话恢复分两路，**均读 `r.facts` 事实窗口内**——①**结算事实**按 `memory/query_events.go` 的 `QueryOptions` 未导出 `keyPrefix`/`Prefix()`/`WithKeyPrefix()` 做**返回集前缀过滤**（`delivery.` 前缀）；②**owed 声明**读 `r.facts` 事实窗口内内 agent_output 的 Meta；`r.replays` 的映射序列**仅用于会话枚举**。两条路径的代价均为 **O(事实窗口内)**（`r.facts` 本就是事实窗口内事实表），MUST NOT 以全链 O(N) 扫描替代；`QueryOptions.keyPrefix` 只筛返回集，不承担按匹配事实计价的扫描量主张。查询 `PartitionIDs` 由 replays 注册键经 `PartitionIDFromName` 派生（分区来自 replays、非盲扫，满足“无分区不盲扫”）。恢复只读链上 owed 声明与结算事实补送**无结算事实 ∪ held(reason=retrying)** 的义务。`TestMultiSessionRecoveryBounded` 的**计数口径=返回的匹配事实条目数**（断言注入无关 agent_output 不抬高匹配数、未清义务全在结果中）；**不另设扫描量断言**——扫描量主张已收窄为 O(事实窗口内)。
- **兜底与双断恢复（1.4b；r8 F1/F3/F11）**：兜底两支见「既有能力裁决」（①失败告知=failure-notice；②延迟告知=未清 held × 驱动轮无面向用户产出，**驱动轮=当前观察轮，即产生该通知判定的那一轮**）；悬空无终态**不发框架通知**；1.4b 先构造真实悬空轮（input 已提交、output 提交前杀驱动）再断言 host 处理后：无通知发出**且**悬空轮在链上可辨识（input 事实存在、无 agent_output）——两个正向断言+一个负向断言；若实测暴露无人知晓的悬空轮=法定停点上报，不得 skip 结项。
- **无实验开关（r22 纯化）**：事件化是唯一交付路径；旧 KV 仅保留迁移的一次只读扫描，不再有回退账本与开关排期。

## 风险与回退

- 迁移风险：负锚覆盖旧 KV 六态+幂等+拒启动。
- 链增重：每笔外发约 1-2 条小事实；联合预算与归因见 03 域 1.5b/1.6（W1 后、03 修复前加测一次 300 轮，把 01 增量单独计量，超门即上报裁决）。
- 回退与开关：无实验开关（r22 纯化，事件化是唯一路径）；回退＝整体 revert 本域。
