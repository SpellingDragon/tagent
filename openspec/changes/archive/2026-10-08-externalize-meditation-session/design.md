# Design: externalize-meditation-session

基线 `bdad9d6`（main/dev 同树）。用户三项裁决：单 change 平铺 / 全量一次立 / 目标未运行=明确拒绝。

## D1 冥想 agent 的形态：配置声明的同构 agent（非新机制）

冥想 agent = `agents:` 下的一个普通 agent 定义（先例：recall agent），配 `meditation.enabled` + `memory.read_namespaces`（被观察分区授权）。store 装配形态：`memory.type: memory` 同 path 共享实例（recall 先例同型）——观察授权经 read_namespaces，装配期校验 `observed ⊆ read`、`deliver_to 分区 ∈ observed`（越界/盲投具名拒绝启动，不留纸面约束）。**不新增 agent 类型、不新增运行时机制**：它有自己的 EventBus/loop/TaskManager（同构自治），由组合根照常装配。被否方案：专用 MeditationService 单例（违反"不新增通用 manager"；同构协作已覆盖）。被否方案：复用入口 agent 的 MeditationManager 实例（跨 agent 共享管理器=共享可变状态，属主不清）。

## D2 跨分区 novelty 判据：谱系派生 + 读后过滤

novelty := 存在事件 e 满足 `e.Timestamp > lastMeditation` ∧ `partition(e) ∈ observedNamespaces` ∧ `lineage(e) ∉ SelfManaged`。数据面：`memory.QueryEvents`（按分区+时间窗）**读后过滤** `Metadata[trigger_source]`——QueryOptions 无 metadata 过滤（已知边界），一次查询的分页量由既有 page 语义约束。判据单源：自管判定**必须**调 event 包 `SelfManagedLineage`（与投递白名单同源派生），禁止在本文件复刻清单。锚点三件（novelty/idle/last-meditation）与持久化（AnchorStore）沿用既有语义，per-(agent) 落盘。

**跨分区 task 计入 novelty 的永动核对（探针语义确认）**：本判据比 in-loop 版（仅 source=user 算新鲜）**宽**：task/system_alert（deliverable=非自管）也计入。这是**有意为之且安全**：防永动不变量的本质是"观察者的自管产出不得喂养自己"——冥想 agent 只写自身分区（D1/spec 已钉），被观察分区的 task 事件属**其它 agent 的真实后台活动**，正是跨域理解的素材而非自喂回路。若收紧为仅 user，反而丢掉"理解其他 session 执行情况"的核心目标。此判据选择写入 spec 场景与代码注释。

**W0 探针（启动门）**：核实 `trigger_source` 是否已持久化于 FullEvent.Metadata。若未持久化：补一条**只写不读既有语义**的持久化（入库时从归因面抄入，方向与 MetaKeyCallID 同法），旧事件无该键按"未知谱系"处理——未知**不计** novelty（保守：宁可少反思，不可误判新鲜）；此判据写入 spec 场景。

## D3 巩固谱系：折叠进既有 consolidation-triggers（W1 执行期推翻重写）

原设计前提错误："既有只是拉式 digest，本变更新增推式事件"。W1 四查发现既有 `consolidation-triggers` 主 spec 的容量路**早已是完整推式**（build_agent `SetOnHint → InjectMessageWithSource` 注入唤醒巩固 turn，含 threshold/snooze 防抖）。据此裁决（防并行机制熵增）：
- **撤** ADDED 能力 consolidation-request-events（其唯一真增量=谱系显式登记+"不计入 novelty"两条，后者已被 meditation-agent-partition 的自管排除判据自动覆盖）；
- **删** W1 初版引入的第二字面量 `TriggerConsolidation="consolidation"` 与第二缝 `SetOnRequest`（无既有 SetOnHint 覆盖不了的消费者，属死缝）及 request 测试；
- **立** 常量 `LineageConsolidationHint="consolidation_hint"`（对准既有字面量 + Lineage* 命名族）显式登记为自管非投递；build_agent 注入侧改引该常量（消字面量复写=真源化）；
- **保留** consolidation-triggers 的 MODIFIED delta（显式登记 + 被 novelty 消费声明）；既有硬门控/snooze/min_source_events/拉式 digest 全部原样复用。
被否方案：维持独立新能力（与既有能力重复、双字面量并存）；迁移改名旧 consolidation_hint→consolidation（触及既有 spec/wiki/装配，改动面大且无语义收益）。

## D4 投递缝：进程内寻址 + 白名单 + 具名拒绝

API（组合根面，如 `tagent.DeliverToAgent`）：`Deliver(agentName, sessionID, msg)` → ①目标名 ∈ 冥想配置 `deliver_to` 白名单（未配=空表=全部拒绝，fail-closed）；②进程内 agent 寻址（org 常驻表）；③目标 loop 状态检查——**未运行返回具名错误**（不触发 InjectMessage 的 one-shot 回退：该回退会绕过 loopActive 语义起新 Run，违反投递契约）；④运行中→ `InjectMessageWithSource("meditation", msg)`（既有单入口，谱系/遥测/novelty 语义全部现成）。目标侧零新代码。被否方案：durable inbox 落盘重投（跨开关耦合 reliability，用户已裁明确拒绝）；静默丢弃+计数（违背 fail-loud）。范围钉死：**进程内**；跨进程=既有 HTTPAPI，不在本变更。

## D5 配置面与热更归属

`agents.<name>.meditation` 扩展：`observed_namespaces`（缺省=自身 read_namespaces）、`deliver_to`（缺省空）。归属判定按消费点：新字段在 MeditationManager 构造期读取，不入五个数值热参、不静默生效。热更归属为**结构换代**——`meditation` 整体嵌进组织指纹的 agent 子集（`agent/org/fingerprint.go` 的 `agentSubset.Meditation`），`observed_namespaces`/`deliver_to` 带 json tag，随结构体全字段序列化自动参与指纹：字段一变即移动组织指纹，换代重建 MeditationManager；扩字段的换代参与由锁测试 `TestOrgFingerprint_MeditationExtFieldsMoveFingerprint` 钉住。（W3 实证修正：原稿误记为“进 `restartRequired` 拒表”；`restartRequiredChanges` 只点名 governance/reliability/trajectory_* 等顶层不可热块，`agents.*.meditation` 不在其列，故扩字段变更走换代而非具名拒绝。）

## D6 验收档（对齐 D17 先例）

- 单测：门控三分（novelty 判据/未运行拒绝/白名单拒绝）、防永动回归（自管事件跨分区不计入）、锚点恢复、事件谱系。
- 集成：双 agent 进程内 e2e——冥想 agent 读目标分区产卡片 → 投递 → 目标 turn 消费 → 目标遥测审计计自管。
- 真实模型（三态门沿用 `TAGENT_REQUIRE_REAL_MODEL`）：冥想 agent 真跑一轮跨域巩固（读共享记忆→产卡片→投递→目标可见），预算沿用集中常量。
- 性能主张：无新吞吐主张（novelty 查询是低频路径）；仅记录单次判据查询的耗时与扫过事件数作为观测样本，不设门槛。

## D7 哲学核验（执行前过一遍）

| 核验项 | 结论 |
|---|---|
| 真源 | 事实链唯一真源不变；novelty 判据读事实链派生，不建第二索引 |
| 执行权 | 压缩权/巩固执行权留在目标；投递与建议均只是信号 |
| 默认态 | 全部 opt-in；关闭态零行为变化（含 lineage 新增 `consolidation` 值对未知值 fail-closed 无影响） |
| 复用 | lineage 单源/QueryEvents/InjectMessageWithSource/锚点骨架全部复用，零重造 |
| 第二总线 | 无新通道：投递收口既有注入入口；跨进程明确不做 |

## D8 双形态语义与双判据的显式化（熵增防护核心）

冥想存在两形态：**in-loop**（默认，无观察面配置——入口 agent 自体维护者）与**外部观察**（观察面非空——跨域策展人）。两者**共存非替代**（外部做不了目标上下文清理，单压缩权不可移）。novelty 判据因此合法地有两套，**数据面不同各有真源**：in-loop 锚定注入点（inject.go `source=="user"`，输入侧最便宜最准）；外部锚定事实链持久归因（跨分区唯一可用面）。两套判据的并存理由 SHALL 写进代码注释与 wiki——否则后人必当冗余合并或当死代码删除其中一套（本仓既往教训：被撤回的候选总有人再试一遍）。形态判据=观察面配置是否非空，单一开关，无中间态。

## D9 探索实证与遗留清理清单（2026-10-08 细化深读）

深读结论（作为任务收窄依据）：
1. **trigger_source 已持久化**：`context_manager.go:1488` 入库时盖章 `fullEvent.Metadata[MetaKeyTriggerSource]`，另有 L1496 读面与 L1726 冥想 reseed 条件消费——W0 探针 0.1 从"核实是否持久化"收窄为"验证覆盖面"（哪些入库路径盖章；投递链的 meditation 注入是否也盖章；采一条落盘样本）。
2. **文档改写面**：wiki 含 meditation 叙述的文件 8 处（memory-architecture/agent-behavior-matrix/org-hot-reload/platform-subsystems/compression-and-telemetry/agent-architecture/docs README/wechat-bot-runtime）——5.1 为**改写而非追加**，逐文件核对旧叙述与双形态的新事实。
3. **示例纠偏**：`examples/wechat-bot/tagent.yaml:152` 注释"定时心跳(仅 entry)"——外部化后任何 agent 皆可开启，注释须纠正；两份 yaml 同步。
4. **config 双层同步**：config.MeditationConfig（string durations，config.go:553）↔ agent.MeditationConfig——新字段两层同步+覆盖检查测试纳入。
5. **批次剔除交互**：`event_loop.go:306 dropMeditationFromMixedBatch`——投递消息与用户输入同批时被移除不补偿（用户优先，好事）；投递成功语义钉为"已进入 mailbox"，已补 spec 场景。
6. **符号盘点义务（新增 0.3）**：MeditationManager 导出面（Start/Stop/SetTaskController/SetAuditLine/SetAnchorStore/UpdateLastUserInput/UpdateLastTurnEnd）×两形态使用矩阵→保留理由表写入本 design；盘点中发现的真死代码当场清理而非留待后人。

## D10 digest 双形态覆盖面

in-loop=自身任务板健康（既有）；外部观察=被观察分区分谱系事件计数+最近活动摘要，自身任务段可空（优雅降级沿用）。实现面：buildMeditationMessage 的 digest 构造按形态分叉，分区概况渲染为纯函数（零 LLM、有界）。已立 meditation-self-state-digest MODIFIED delta。

## D11 锚点结构与回切连续性

外部形态复用 lastMeditation 锚作判据水位（不新增锚字段，AnchorStore 三锚结构不变）；lastUserInput 在外部形态下按注入规则继续更新但不参与判定——保留理由（回切连续性）固化于注释。回切验收：移除观察面重启后判据回输入侧锚，无需迁移。
