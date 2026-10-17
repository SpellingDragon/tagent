# 任务层：受理、等待、可靠结算与恢复

本页承载 `agent/task`、任务 outbox 与本地逻辑调用的连续性契约。一个任务可返回 ACK 后继续运行；一次等待可先满足而 producer 继续存活；一次模型回复也可先结束而逻辑目标仍在等子任务。这三个边界分别记账。

<a id="status-machine"></a>
## 一、状态、等待与身份

任务状态仍为 `running`、`stable`、`alive_detached`、`suspect`、`completed`、`failed`、`dead`、`cancelled`。信号包括完成、失败、明确 ready/input，以及 stable/suspect/watch/probe 观察。

| 轴 | 含义 | 不能代替的证据 |
|---|---|---|
| `Mode` | oneshot、resident、interactive 等执行形态 | 不能证明进程退出 |
| `WaitFor` | 本次前台等待 `exit`、`ready`、`input`、`signal` | 等待满足不表示 producer 已停止 |
| intent round / owner | 被受理的尝试及其接收调用 | 不能覆盖实际 producer 的轮次 |
| producer round / owner / seq | 实际资源的归属和观察顺序 | 不能用最新任务状态推断旧信号身份 |
| `InputBatchID` | 发起本轮的 opaque 输入批次 | 不能从出生时 Origin 推断 Resume 的输入归属 |
| `Origin` | 派生时来源与谱系 | 不随 Resume 或回收改写 |
| `Stopped()` | 实际 producer 及其清理已确认停止 | Cancel、终态、流关闭或 Close 超时都不能替代 |

信号产生时冻结 `RoundID`、`ProducerRound`、`RoundOwner`、`Seq`、`ObservedAt`、`WaitFor`、`InputBatchID`、producer 绑定声明和正文；`SourceID` 由 task id、round、seq 构成，重试不重新取时间或序号。`SpecSnapshot` 深拷贝声明内的 map、slice 和 overrides；`RoutingSnapshot` 在一把任务锁下读取意图、producer、状态与 pending，避免拼接出不同轮次的身份。旧信号使用自己的 child invocation 和输入批次，不能在回调时反查已经 Resume 的最新绑定。

`completed`/`failed` 自动结束等待；`Final` 也结束观察器等待，但不把 suspect 伪装成业务成功。普通 stable/watch/probe 观察不满足无关等待。命令默认等 exit，resident 默认等 ready，interactive 默认等 input；旧版无 Mode/WaitFor 的直接 service 用法保留 stable 兼容，生产命令声明明确条件。

## 二、受理与 dense 窗口

生产启动顺序是：去重及门控 → `OnSpawnAccepted` 提交意图 → `TaskSpec.Start` 准备资源 → `OnStartAccepted` 提交绑定 → `ActivatableDetector.Start` 激活业务。意图未受理时不 prepare，绑定未受理时不执行命令或子 agent。准备失败或拒绑只清理本次新资源；同一 resource 的旧 producer 不因 wrapper 更换而被杀。

`Spawn`/`Resume` 等待声明条件或 detector 的 detach，独立 `sync_wait` 不再存在。条件先到则内联返回真实结果；dense→sparse 先到则返回含 task id 的 ACK，后续交付由任务层承担。ACK 不声称完成，也不引导 sleep 或轮询。

窗口内外只能有一个接收路径。旧调用晚交接时只能关闭自己捕获的窗口，不能关闭新 Resume 的窗口。内联首观察与紧随的完成分别保留，不能挤掉终局。

<a id="detach-suppression"></a>
## 三、观察、条件满足与 producer 存活

native pane 存活时，输出稳定只是观察。oneshot 的稳定输出不等于 completed；非零退出及信号死亡按原生退出码表达失败。resident ready 需要明确 pattern 或成功 probe，interactive input 需要本轮增量输出的 input pattern。watch 命中、probe 失败/恢复边沿有各自观察类型，不因探针失败直接宣称进程死亡。

服务 ready 满足等待后仍可保持 `alive_detached`；显式resident的重复非终局稳定/可疑观察可被抑制，终态和 Final 仍进入结算。未声明Mode/WaitFor的直接旧调用只抑制同producer round、同kind、同payload的重复观察；新稳定载荷继续入管线，避免新prompt或结果被当成安静噪声。job 的非终局稳定观察不转服务 ready，也不解除 exit 等待。

调用屏障按 task、producer round、owner 和冻结 WaitFor 配对；等待满足与 producer stream 结束分别记录。A 已 ready 后的终局不能扣掉仍在等结果的 B。旧轮或重复 seq 不能释放新轮；被 receiver 拒绝的信号不能清掉其 booking。去重以已确认 seq 区间收敛，不积累无界 SourceID 集合。

<a id="recoverable-invocations"></a>
## 四、可靠结果、受理回执与 ACK

任务 journal 把同一事实链上的阶段和结果分开保存：

| 事实 | 职责 |
|---|---|
| `task_spawned` 阶段记录 | 原意图、资源绑定、声明与寿命锚点 |
| `task_signal` | 完整冻结结果、错误、父子地址、输出正文或已保存的文件票据 |
| `task_signal_receipt` | 该结果已被指定接收管线受理 |
| `input_disposition` | 精确源 EventKey 和 partition 的消费处置 |
| `task_round` | 显式子 agent 委派的 begin/finish 历史 |
| `continuation_state` | 逻辑调用的私有投影、等待、输入处置和结果缓存 |

accepted 回调返回 true 后，任务层才删除 pending、确认通知并 `AcknowledgeSignal` 原 detector。ACK 确认管线受理，不冒充模型已处理；后者由输入处置与 continuation checkpoint 证明。没有接收者是拒收。批量退役和终局补发同样要确认每条 raw 信号，并 ACK 原 producer；只成功发出汇总总线事件不够。

拒收信号由独立 worker 以 400ms→4s 退避自动重试，不依赖用户输入、看板或冥想。重试沿用原 SourceID、seq、时间、WaitFor、正文和目标。大结果保存失败会拒收，不能用不存在的正文路径确认成功。终局输出、pane/pipe 和结果缓存保留到相应 ACK，pending 不因 terminalTTL 被提前剔除。

取消也进入 raw 结果路径：包括已有 completed/failed 任务转 cancelled 的新转换。取消信号带终局语义和退役谱系；生产不靠旧 void OnCancel 的最佳努力记录证明保存成功。

<a id="ttl-reclaim"></a>
## 五、统一 TTL 与有界关闭

任务的绝对寿命为显式 TTL，否则读取同一默认 source；非正值不是无限哨兵。全部 active 状态和寿命类都受相同 TTL 约束，resident/interactive 无年龄豁免。quiet timeout 只决定观察，不能代替绝对寿命。

生产装配启用一个最近期限 timer：存在任务时调度最近 active 到期点或可回收终态期限；空表停用。没有输入、看板或冥想，到期仍请求 owner Cancel，并一次退役为 failed。没有逐任务 sleep，也没有固定扫空 ticker。`RecheckDeadlines` 只重读现有 TTL source 和锚点排期，不另存 TTL 真值；独立管理器保留显式 List/reconcile 的兼容入口。

成功 send/resume 更新寿命锚点，peek 不续命。Resume 候选锚点在绑定提交时就保存，避免绑定后首信号前崩溃丢掉续期；准备、绑定或实际发送失败则记录失败意图并恢复旧 producer/owner/TTL。detachedAt 只作观察，不是第二条到期墙。

`Close(ctx)` 有界停止准入、观察和重试，向所有已纳管 producer 请求取消。`CloseDone` 等操作、watch、callback、worker 和实际 Stopped 全部退出。kill 失败不能关闭 Stopped；Cancel 已先发生的 lazy producer 不能再次 Start。第一次 Close 超时后，同一关闭尾部继续承担最终退出，不能借超时提前释放仍被使用的资源。

<a id="board-rendering"></a>
## 六、面板是只读快照

面板每次 BeforeModel 在消息末尾重新生成，展示 active 事实和同一回收器计算的剩余寿命，不落库、不参与压缩、不替模型续命或裁决。展示上限不等于完整账本；终态不继续渲染，完整结果和历史由事实链、正文票据及恢复记录承接。

<a id="spawn-dedup-origin"></a>
## 七、去重与来源

active Key 去重先于 lazy prepare；空 Key 关闭去重。被拒绝或 dedup 的预先运行 detector 仍须由调用方取消并等待真实 Stopped，不能只丢掉执行引用。新 lazy 路径在准入前不启动业务。

OriginSpawner 仅在 spec 未自带时逐键复制 Origin；RoundOwner 另行记录本次调用归属。资源绑定 factory 可补执行能力，不能覆盖持久身份和不可变来源。回收谱系只盖在信号上。

<a id="finalize-lineage"></a>
## 八、终结与批量退役

失败的内存状态、信号、WAL 状态和反馈极性保持一致，未知 Kind 不默认当成功。TTL、僵尸和孤儿回收沿同一退役通路，信号带 `task-retired`，不改 Spec.Origin。有调用归属的退役逐条回原调用；无归属条目可汇总通知，但每条 raw receipt 的受理、拒收与 ACK 责任不被汇总吞掉。

正常后台与内联结果均有记录和受理证据。补发仅处理未确认终态，使用原 producer round、终局 seq 和观察时间；拒收后进入原 raw pending，不能每次补发制造新身份。

<a id="restore-rebuild"></a>
## 九、恢复先登记，receiver 就绪后再运行

恢复按 task id 幂等登记，不覆盖已有运行态，窗口初始关闭。身份、原 producer、seq、真实 settledAt、detachedAt 和 TTL 锚点以事实为准；新格式 ProducerRound=0 表示从未有实际 producer，不能从 intent round 造出一个。旧格式无 round 身份时才应用兼容推断。

生产恢复先装配任务、pending 原信号、child 私有投影和输入处置，再附着原资源及所有 receiver。期间 deadline、liveness、prune 和新观察交付均受恢复屏障保护。恢复绑定只登记 driver；ActivatableDetector 不立即 Start，即使是尚未资源化意图的 prepare 也须等待全图绑定完成。最后显式 `ResumePendingDeliveries` 解除恢复 hold、激活本管理器全部资源，再放行重试与 TTL；Cancel 先赢得激活门时不再 Start。`RestorePendingDeliveryWithMode` 保留原 inline/background 记录目的地，不能把内联结果恢复成新的模型输入。

若正式输入处置已证明消费，可补交原受理证明与 child ACK，再用 `ResolveRestoredDelivery` 清理唯一对应的冻结 pending 并 ACK 原 detector；它不改轮次、状态或序号，也不清理其他待交付结果。

`BindDetector` 绑定原 producer round，不从 round 1 重采样；终态只允许 ACK-capable detector 清理原证据，不重开业务。已受理且没有任何资源的意图，可由 `ResumeRestoredStart` 沿原 intent prepare→绑定→激活；已有资源者不走重执行入口。恢复工厂返回带 resource identity 的 spec 时，也要受理绑定后才启用观察。

进程重启只能重接仍存活的 tmux/native 资源，不恢复消失的进程。整机重启后缺执行现场应明确失败或需对账，事实链不会因此失去结果身份。没有 detector 的回收仍须 nil 安全；不能把显示 tracked 当成已连通信号链。

<a id="resume-states"></a>
## 十、显式 Resume 与逻辑调用

Resume 保持 task id。`alive_detached/stable/completed/failed` 可认领新意图，`running/suspect` 拒绝并行在飞，`cancelled` 要 relaunch。认领单胜；被受理的失败尝试仍消耗轮号。例如 round 2 输入失败而实际 producer 仍是 round 1，下一次使用 round 3，旧 owner/seq/TTL 不变。`round_aborted` 是内部尝试事实，不是旧 producer 的终局。

命令准备输入和精确字节基线，绑定提交后才 SendKeys；旧末行 prompt 不能满足新轮 input。subagent 显式 Resume 新建 Run，从持久 task_round 历史还原前序输入、输出、错误与正文票据，并按发起者绑定或当前有效面解析目标。

本地 child 的逻辑调用使用原 invocation、私有投影、等待与 stream 游标、输入处置和父 task 原 producer 地址。一次 RunFlow 回复后仍有子任务等待时，逻辑调用继续；本地等待不受覆盖整调用的固定600s截止，模型 active turn 预算与 Task TTL 仍各自有限。远端 A2A 保留有界 transport deadline。

重启重建保存的 goal 和输入处置，不重复已消费初始 goal。工具结果未提交的中断明确要求 reconciliation，不假装恢复了程序计数器。结果已保存但父未 ACK 时只重发缓存结果；goal 完成后仍活的 ready 服务明确交接到父 scope 或持久 owner，之后终局仍有接收者。owner Close 挂起 scope，父 task 的可靠取消/TTL 退役才取消对应逻辑调用。

历史根因、边界与验证进度见[异步任务连续性技术报告](../../async-task-continuity-report.md)。
