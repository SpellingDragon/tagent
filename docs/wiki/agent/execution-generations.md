> **本篇是当期契约**：代码注释 `// 契约: docs/wiki/agent/execution-generations.md#<锚>` 指向本页各节。实现面在 `runtime/state/`（`publish.go`/`binding.go`/`lease.go`/`retire.go`/`occupancy.go`）、`runtime/agent/`（`loop.go`/`shutdown.go`/`delegate.go`）、`assembly/`（`host_reload.go`/`prepare.go`），模型租约在 `features/rl/`（`lease_model.go`/`replacement.go`）。更细的换代面见 [platform/org-hot-reload.md](../platform/org-hot-reload.md#generations)。

# 执行代际、租约与生命周期收敛

本页承载最易在重构中丢掉的一类判据：**一个回合在哪个执行面上跑、那张面何时可回收、关闭时必须向调用方诚实到什么程度**。三者的失效模式都不报错，而是「配置静默不生效」「旧绑定复活」「数据被并发写入打断」。

<a id="generation-not-fingerprint"></a>
## 一、发布身份是代际序号，不是内容指纹
`state.Generation` 是**唯一发布入口**在每次真实发布时分配的单调递增号。同一套行为重新发布成一份新东西时必须算新一代——按内容指纹去重会让「重发同一形状」被判无变化，参数因此静默不生效；反过来**同一内容的重复发布不得算新一代**：`Publish` 对字节相同的内容返回 `PublishUnchanged`，宿主据此在报告里写 `Publication.Published=false`，而不是留一张空变更表让读者去猜。代际号由持有者自己数（`Bindings.Hold` 里 `generation++`），引用里的号来自一次真实发布，调用方永远选不了号。可判定性质两条：候选构造**不触碰在线执行面**（`Leases` 只登记候选自己取得的资源），重发布同一张面时行为保持。

<a id="lease-holds-reference"></a>
## 二、租约是引用的唯一载体
`Borrow` 四条红线就是这一节的全文：请求**开始执行**那一刻取当时生效的代，并整段持有；委派请求取**发起者那一代**；不得预先绑定排队输入，也不得在请求在途时换掉它的代；不得服务本持有者从未发过的引用。四种情形各有唯一期望：已收敛关闭的代**拒绝**新引用（`ErrBindingGenerationReleased`），且**绝不拿当前代顶替**；已退出但仍有借用的代由引用计数撑着，不因「标了退役」就断；引用里的实例号不认识即 `ErrBindingRefUnknown`（零引用、别人的持有者发的、上一次运行留下的，一律拒）；借来的壳不得当有效执行面用；**跨回合的长期闭包（任务、重入）不得借用发起方的租约**，必须按自己这次调用另取一份。回收判定的唯一输入是「借用是否全部落账」——`Inflight()` 一次答出，不让人去数日志；退役资源的释放由 `retirement` 账本在「已被取代且再无请求」时各释放一次，释放失败不阻断其余释放。

<a id="lifecycle-convergence"></a>
## 三、关闭：必须收敛，且诚实优先于干净
`Shutdown.Close` 先拒绝新输入（`ErrShutdownRefused`），再把已接受的义务结清：`Reconciler` 三条红线——只有该调用的义务清空才落它的终结、仍持有的预约必须报出（绝不一边说排空一边压着未落的终结）、不得早于也不得晚于它等的义务。排空轮询真实结算（`shutdownPollInterval` 2ms），不假设。**未收敛就报错**：`ErrDrainUnsettled` 报「没到真实终结」并附上还没停的东西的理由（如 `reasonProducerNotStopped`），绝不在写入者未确认停止期间退出存储。**「报错＋仍然持有」是诚实结果，「干净返回」是假象。**

<a id="turn-local-execution-face"></a>
## 四、回合本地状态与执行面的边界
冻结时机是**开始执行**那一刻，不是入队也不是发起：在途调用跨过一次发布仍跑它开始时的代（`BindingHandle` 持有整段生命周期），新代不得把它偷走；发布之后才开始执行的排队输入取它执行时那一代；被任务层收养的后台运行持有它开始的那一代直到落地。回合本地状态（触发源、调用元数据、席位）必须**每回合自己盖章**，不从上一回合继承，也不挂在已发布的共享对象上。一处实现细节值得记住：`AgentBinding.Plugins` 的序就是发布序，请求中途绝不重装；而 `PromptSource` 的 `Files` 属发布代、`Version` 内容在**请求边界**定版——文件改了不必换代，结构改了必须换代。席位是这条边界的第三件事：每轮决策前 `Occupy` 会话决策席，同址在途即 `ErrSlotHeld`，终结即 `Vacate`。

<a id="turn-outcome"></a>
## 五、回合结果必须归约为五值，且取消不得吞掉已认领的输入
结局归约入口唯一：`model.ReduceChannel` / `model.ReduceSeq` 覆盖两种流形状，产出恰好一个 `Outcome`；消费者只读 `Kind`，**不得在旁边再判一次**（不重分类六个 Signal、不另做请求折叠、不从流机制——channel 关闭、序列耗尽、取消——推终结）。五值互斥：`KindUnknown`（零值，读不出即告警，**不得降级成完成**）、`KindCompleted`、`KindFailed`、`KindCancelled`、`KindPartialOnly`。归约是**请求作用域**的：usage、正文、trace、迟到响应只属这一次请求，跨请求不累加。`Loop` 把这一个终结记进 `Turn{Decisions,Terminal,Failure,FinalRound}`；取消有配套硬要求：**已认领的输入不得 ack**，否则「取消了」会变成「这批输入被悄悄消化掉」，下一轮既不重放也无人知晓丢了东西。

<a id="published-wrapper-immutable"></a>
## 六、已发布对象不得被就地改写
执行面只来自**被执行的那个绑定本身**（`AgentBinding` 的模型、工具表、插件序、提示源），绝不回落到任何别处的快照——否则被新配置清掉的东西会「看起来还在」。状态面（链、投影、会话服务）始终取自当前的持有者：换代不搬运共享状态，也不重写已发布对象。三条禁令：不往已发布绑定里写调用域数据（调用域属于一次调用，经 ctx 流动）；不在请求中途改插件序或工具表；不把「换一个内容相同的新实例」当成换代（那是必须拦下的形态）。

<a id="single-linearization-body"></a>
## 七、发布入口只允许一个线性化主体
`Publish` 是唯一发布入口：它先验必需的治理检查都已裁决（`ErrRequiredCheckMissing`/`ErrPublicationContractEmpty`、被拒即 `ErrCheckRefused`、豁免必须带理由与范围否则 `ErrExemptionUnrecorded`），再 `Hold` 候选绑定成一个新的代，并在锁外释放待退役的那一代。任何「改变生效面」的动作都只有一个实现点，入口之间的差别体现在参数（回滚是「重新发一份上一版的完整配置」，`ErrRollbackUnavailable`/`ErrRollbackSourceMismatch` 按名拒），新增入口合规与否只看它是否复用这同一个主体。`ErrGenerationDiverged` 守的正是这条：有人在这个入口之外碰了持有者的编号。

<a id="hot-source-pull-authority"></a>
## 八、热参的权威是参数源，不是推送
热参的权威是参数源本身，而不是某处原子的上一次写入。消费边界每一次都解析完整的数值组（任务策略在派生那一刻、装配预算在每次装配时现算），仅轮转参数源、不做任何推送，也必须在下一读取生效。**零读数是「记录没有意见」**，此时由构造期的值应答——一次零读数不可能把在用的周期归零。派生出来的边界值不进热面：把所有派生数字折进指纹，正是判据排除的做法。

<a id="generation-wiring-window"></a>
## 九、一次发布内的接线窗口：先判能不能装，再一次性换
宿主换配置的窗口被钉死成三段，顺序不可换。**先判**：`hostDocument.compatibleWith` 在任何复用与交换之前跑——换入口 agent、增删 agent 集合即 `ErrHostReloadStructural`；动只有 `Boot` 建过的消费者即 `ErrHostReloadRestartRequired`（`restartRequiredChanges` 列出的正是那些 YAML 路径：`storage.type`/`storage.root`/`request_timeout_seconds`/`governance`/`telemetry`，以及每个 agent 的 `tools`/`skill_dirs`/`read_root`/`file`/`exec`/`memory`/`meditation`），被拒绝的 reload **两侧原样不动**。**再一次换**：通过的文档经**一次** `Publish` 进一个代（同内容则 `PublishUnchanged`，不进代），随后同一把锁内换掉在效文档与原生工具面。**最后逐 agent 记模型迁移**：从租约自身读在效版本、与新文档比对（`modelChanges`），再各自 `NoteModelChange` 产一条 replacement 事实——读的是租约而不是上次的变更表，因此一次失败的安装会被下一次 reload 重新判出来，而不是被当成已应用。

<a id="reentry-resolution"></a>
## 十、存量任务重投递：解析用哪张面
重投递的解析只有两个答案，且同源：**有发起调用绑定就用那次调用的代**（`BorrowDerived(ref)`，与一次正常 `Call` 走同一条借用路径），**没有就用当前生效的代**（`Borrow`）。四种情形互斥：发起者仍在途即取它声明的代，即使新一代已不再路由该目标，旧代绑定仍可服务；属主环路静默时取该属主的持有者（不经入口）；新一代已移除该目标则**按名拒绝**并附版本理由，既不重跑被移除目标也不静默改投；新一代改了该目标则无发起者的重入经新发布面到达新目标。三条禁令：解析只回答「哪个目标」，跑在哪张执行面上必须同源；不得把此刻的包装器表冻成快照传下去（换代后照快照解析会让已被移除的目标被旧代静默复活）；引用取不到就拒绝，不回落。

<a id="model-reference-pinning"></a>
## 十一、模型引用解析在「被选中的那一代」，不在「此刻的注册表」
`model_override` 让一次委派换掉服务的模型，但它填的是**引用名**，不是实例。`LeaseModel` 是这一条的落点：它是**跨替换稳定**的句柄，每次调用借用**该调用开始时**生效的那一版，于是替换只影响此后开始的请求——在途请求整条流都留在它开始的那一版上，替换永不在活请求底下换模型，也不必为换模型重新发布一代。`ModelIdentity{Model,Version}` 把「同一模型名的两代」记成两个身份（`Name()` 用 `@` 连接，无版本即无分隔符），因此替换前写下的记录永不被读成替换后那一版的记录。配置侧的迁移经 `NoteModelChange` 落成 replacement 事实（`ModelReplacementSourceConfig`），报告里逐 agent 给出 `from`/`to` 与事实键。

## 已知缺口与演进方向
| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 诊断面只有计数与名字 | `Inflight()` 给总数，缺「哪个持有者长期不落账」的直接定位；借用壳的误用只能靠测试钉住 | 按代与持有者分组的在途账，带上取得时刻 |
| 关闭未收敛的报告可读不可机读 | `ErrDrainUnsettled` 附理由，但理由是人类字符串 | 一个可解析的 `DrainReport` 结构，供运维二次判断 |
| 换代窗口的经验只在本仓 | 判据靠 `compatibleWith` 的三档拒绝，缺跨进程实测 | 一次「换代 + 重启」演练，正负两腿各一 |
