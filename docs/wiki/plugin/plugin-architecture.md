# tagent/plugin：六端口材料流与窄口扩展点

`api/plugin` 是一条**材料流**的声明。框架在三个位置注入差异化能力——执行前的一次调用变换与一次裁决、请求装配时的一次环境块贡献、提交后的一次观测、一次呈现、一次压缩触发。一个插件是一个注册单元：报告一个稳定名字与**恰好一个**端口，实现那个端口的**一个**窄接口。

<a id="overview"></a>
## 一、词汇与窄接口

端口是具名常量，常量名就是它代表的转向——**读的那一面材料挨着答的那一面产物**：

| 端口（`plugin.Port` 常量值） | 窄接口 | 时点与判据 |
|---|---|---|
| `call->call` | `TransformCall` | 副作用之前、准入之前，每 call 一次；可改工具名与参数，**不得改 call 身份**（`CallID` 与已解析信任域），改了以 `ErrCallIdentityChanged` 具名拒 |
| `call->ruling` | `RiskAssessor` + `Governor` | 同一裁定的两张脸：`AssessRisk` 出 grade，`Governor` 在 grade 上出唯一执行前裁定；grade 只读一次，绝不重算 |
| `request->environment` | `EnvironmentInject` | 请求装配的 InjectEnvironment 趟；每块是一个产物，回查发起它的请求；不写运行时、不改请求 |
| `fact->observation` | `PostCommitObserver` | 事实的规范提交**之后**，只读旁路；失败记具名 gap 或保留义务，不回滚、不重跑、不写事实 |
| `fact->view` | `FinalPresenter` | 为渠道渲染呈现文本并回答是否被丢弃；**丢弃必须报告**，不撤回事实 |
| `fact->compression` | `CompressTrigger` | 已归档压缩请求的 TriggerCompress 趟；请求身份与 subject 保持原样，本面不压缩、不退役请求 |

六个端口就是闭表：`validatePort` 是一个 `switch`，六 case 全中返回 nil，其余按名拒（"不是本流的已声明端口"）。`assemble` 也是六 case 具名派发，每个 case 只做一次类型断言，断言不中即"声明的端口没有实现它的窄接口"。**曾经的 Material×Product 九常量矩阵已删**——没有任何一趟按"材料集合"走路，端口常量就是完整声明，读材料与产产物分开声明的写法也不再存在。

红线（`Instance` 的三条）：必须报告自己服务的那一个端口并实现它的窄接口；不得报告未实现其窄接口的端口；一个注册元素只服务一个端口（`call->ruling` 可同时带两张脸）。

落在一趟里的词汇也是契约：`CallPass{Final, Risk, TriggerSource}` 是裁决者拿到的**全部输入**（最终 call、risk face 已出的最强档、派发层声明的触发来源），裁定没有隐藏输入；`RiskLevel` 四档 `low`/`medium`/`high`/`critical`，`Rank()` 是"取最强"与"比阈值"共用的唯一排序；`Disposition` 三值 `allow`/`record`/`hold`——拒不是第四种 Disposition，而是 `Decision.Refused()` 为真、`Denial` 非 nil 的终局结果，所以"挂起"永远不会被读成"拒绝"。

## 二、装配的四条拒绝（`NewFlow`）

nil 实例、端口不在闭表、端口合法但没实现该端口的窄接口、同一个名字声明两次——各自按名拒绝，不静默修剪，因为一个产物的名字必须溯回写它的那一个元素。`assemble` 末尾再判一次 `servedAny`：声明了端口却一个窄接口都没实现的元素不会静默通过。

## 三、三趟语义

`Before`：每个 transform 在自己的**私有副本**上改正，随后 risk face 在改正后的 call 上打分，pass 回答最终 call、**最强** grade 与触发来源。任一 transform 报错即拒整个 pass（后续面不跑、不裁定、不执行、不记录）。空 grade 不是一种 grade——裁定读作 `RiskMedium`，所以旁边没有 risk face 的 call 仍按普通变更调用被裁。

`After`：先跑全部 observer，再跑全部 presenter。观测失败进 `view.Presentation.Gaps`；presenter 丢弃只丢视图。**pass 不返回 error**：这里的失败只能是 gap、保留的义务或 dropped view，绝不是碰已提交事实的理由。

`TriggerCompress` 与 `InjectEnvironment` 各自成趟：前者驱动一个已归档压缩请求的映射链（失败即拒该请求，材料不压缩、请求保持待处理）；后者按声明序追加环境块（空面必须如实报空，单面失败即拒这次装配）。

`fact->observation` 内部还有一层区分：**observer-only** 只读事实（观测、日志、审计不欠任何工作，失败就是 gap）；**driver** 面被事实要求去启动别人欠的工作（follow-up 回合、一次投递），失败时义务仍欠着，于是回 `ErrObservationObligationRetained`——"观察失败"与"工作还欠着"是必须分开报的两件事。duty 在装配期从面本身读出（`dutyOfObserved`），注册处只声明一次。

## 四、消费者：一次 pass 怎么走到裁决

| 环节 | 位置 | 做什么 |
|---|---|---|
| 组流 | `runtime/agent/loop.go` 的 `dispatch` | `plugin.NewFlow(decision.Binding.Plugins)`——用绑定代携带的实例表组流，loop 不另建一套派发 |
| 前趟 | 同一处，每 call 一次 | `pipeline.Before` 出 pass；被拒的 call 拿到配对结果文本，不进准入 |
| 传导 | 准入路径 | `plugin.WithCallPass(ctx, pass)` 把这一趟压在调用 context 上（键是包私有的 `callPassKey{}`，没有第二个写入口） |
| 裁决 | `assembly/tools.go` 的受理 wrap | `policyRuling` 读回 pass → `Governor.Judge` → 任务受理结论三态（拒 / 挂起 / 放行） |
| 取谱系 | `assembly/host_tools.go` | send 工具从 pass 取触发来源当投递谱系 |

`WithCallPass` / `CallPassFrom` 与端口类型同住 `api/plugin`：它们是**接口的一部分**（"裁决者拿到的那一份输入"），不是调用方的私有约定——把它挪到调用方，就会长出第二个传参约定。

## 五、装配期与派发期的分工

两件事必须分开，否则会长出第二条派发通道：

- **装配期（`assembly/prepare.go`）**只**构造与登记**：每个声明代按 model → tools → prompt → plugins 的顺序取部件，插件工厂回答该 agent 的有序实例表，放进 `state.AgentBinding.Plugins`，并把释放函数登记进候选账目。装配期**不组流**。
- **派发期（`runtime/agent/loop.go`）**只**组流与跑趟**：每次决策用该绑定代的实例表 `NewFlow`，然后逐 call 跑 `Before`。

这样"哪些实例存在"由绑定代决定，"同一 call 按什么顺序被谁处理"由 `NewFlow` 的六 case 派发决定，两处不会各说一套。插件实例本身由宿主经 `WithPlugins` 在装配期构造一次，每代绑定携带同一批实例——热更没有重建插件实例的通路，这一格在[热更新三档](../platform/org-hot-reload.md#restart-required-dimensions)里与工具声明同档。

## 六、与相邻缝的边界

- **交付前审查**（`DeliveryRuler`，`delivery->disposition` 端口）是**另一个**端口：它裁决"这条正文该不该发出去"，与六端口闭表不重叠，契约在[治理分级处置](../agent/governance-enforcement.md#disposition-and-risk)。读成第七端口会让"工具调用裁决"与"投递审查"共用一套判据，而两者的对象与重试语义都不同。
- **消费者协议**（订阅 / 投递 / 应用 / 关闭 / 观察的那几个面）不在本篇：它属事件消费器一侧，见 [docs/EXTENSIONS.md](../../EXTENSIONS.md) 第四节"消费器协议：三个实例、六个面，一条协议"。
- **记忆写入**：本仓不再有 `MemoryPlugin` / `SummaryPlugin` 这类 `OnEvent` 钩子，事件持久化与摘要视图归记忆写入路径；`plugin` 只留这条材料流。

## 七、历史对照（一行注记）

原版的两相位 `OnEvent` 插件（`MemoryPlugin` / `SummaryPlugin`）与九常量的 Material×Product 端口矩阵已移除：前者归记忆写入路径，后者由六端口闭表取代。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 顺序契约隐式 | 跨相位依赖（变换必须先于裁决）由 `Before` 的单趟顺序保证；同一端口多个实例按注册序跑，没有 requires / provides 声明 | 装配期校验显式依赖 |
| 闭表与派发是同一份知识写两处 | 新增端口要同时改 `validatePort` 与 `assemble`（都是六 case）；漏改派发会被 `servedAny` 拒掉，但给不出"你忘了派发"的提示 | 由一处生成两张表，或加一条骨架断言钉住两处同步 |
| 端口注册情况不可观测 | "某端口在本进程里没人注册"目前只能读装配代码判断，装配期不给出按端口的实例清单 | 诊断面输出按端口的实例清单（名字 + 端口） |

延伸：[存储概念图](../../demos/storage-concept-map.html)（六端口在 L2 扩展者分层的位置）· [存储时光机](../../demos/storage-time-travel.html)
