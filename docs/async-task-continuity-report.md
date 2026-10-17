# 异步任务连续性：历史根因、修复边界与运行预期

日期：2026-10-17。基线：`dev` 的 `0232fde`。本文记录本次连续性修复的技术依据；当前实现和契约见[任务生命周期](wiki/agent/task-lifecycle.md)与[事件流](wiki/agent/event-flow.md)。验证表保留集成补位，不能把规格归档等同于发布验收。

## 问题的实际边界

“命令还在运行”“前台已返回ACK”“模型已回复等待”“结果已经写入”“接收方已经消费”是五个不同事实。历史实现分别优化了这些边界，却缺少贯穿边界的稳定任务/轮次身份与受理证据。因此既可能表现为过早完成，也可能表现为任务已完成却没有后续回复、重启后只剩suspect，或旧结算误释放另一个等待。

本次保留同一任务层、同一事件消费壳和同一事实链，补齐从意图准入、资源绑定、producer结果、receiver受理、模型输入处置到原父调用结果的连续路径。没有新建独立工作流执行图，也不声称可以恢复Go栈、远端进程或任意外部工具的exactly-once副作用。

## 历史证据与初衷

下表由指定commit的消息和实际diff核验；“影响”是从当时控制流得出的技术判断，不把某次改动当作所有后续故障的唯一原因。

| 日期与commit | 当时初衷 | 代码证据 | 连续性影响与本次修复 |
|---|---|---|---|
| 2026-07-15 `62bd1e0` | 统一事件消费者、纯异步tmux，避免“已经开始、等待结果”冗余回复 | `agent/context_manager.go` 新增 `isTmuxAsyncPlaceholder`，RunFlow遇 `waiting_async_response` 直接continue；monitor只在稳定类状态回调 | 过滤的是明确的工具占位事件，不是所有真实终局。它使ACK、模型回复和最终工作结果更容易被混作一个边界；现在保留诚实ACK，逻辑调用是否结束由真实等待与受理决定，不按占位文本判定 |
| 2026-07-24 `059d475` | 引入TaskManager与dense→sparse，让短任务内联、长任务不阻塞循环 | 新 `tool/action/settle.go` 在信号队满default分支丢额外事件；native终局后立即close/reap。`runWithTimeoutAndRetry` 对本地与远端都套整调用timeout | 终局没有接收方受理屏障，队满可丢唯一完成且资源先销毁；本地合法等待仍受整调用600s上限。现在冻结raw信号、受理后ACK清理，拒收自主重试；本地只保留active模型预算与统一Task TTL，远端仍有transport deadline |
| 2026-09-18 `832f43e` | 为durable inbox建立逐输入提交与receipt/ack闭环 | `agent/event_loop.go` 只对带inbox_path的事件预存，任一存在便给整批设置FactsPrePersisted | durable用户输入与volatile任务结果混批时，后者可能未预存却被整批跳过合并回显。现在逐条冻结与提交全部相关输入，先确认事实，再区分observe/decision及处置 |
| 2026-09-27 `eeae214` | entry与被调用方复用统一事件壳，以invocation bus和pending计数避免terminal-before-delivery竞态 | 新 `settle_routing.go` 以invocation累计整数pending，route发布后递减，inline调用撤销booking；调用有私有CM/投影但没有完整持久逻辑scope | 每次信号、等待满足和producer结束仍共用粗计数，原调用退出或进程重启可丢私有等待；A的后续信号可能影响B。本次按task+producer round+owner+WaitFor记账，保存stream seq区间及continuation私有投影和输入处置，恢复附着原invocation |
| 2026-10-02 `b672982` | 修正失败极性，避免job的静默stable被误报成服务就绪 | `agent/task/task_manager.go` 的stable/suspect分支对非service直接return；注释明确表示暂时不发job中间信号，等待后续信号矩阵收口 | 这是有意抑制误ready，不应反推为所有结果都被吞。副作用是观察也不再到后续决策链。本次让观察进入可靠事实和处置管线，普通观察不单独调用模型、不解除exit等待，终局和明确条件另行驱动 |
| 2026-10-14 `0232fde` | 修复第一段异步结算断链 | job观察重新入管线、窗口交接保后续完成、Final与取消释放末轮、volatile输入逐条入库 | 修复了直接丢事件，但尚未覆盖lazy准入、精确round/seq受理、私有逻辑scope恢复、native身份及无输入TTL；这些是本次继续补齐的范围 |

可复核命令为 `git show <commit> -- <file>`。commit标题记录的通过结论只属于其历史验证，不代表当前工作树或本次部署已通过。

## 本次改动与对应责任

| 边界 | 实现 | 解决的问题 |
|---|---|---|
| 准入→副作用 | gate/dedup→意图accepted→prepare→绑定accepted→Start | 被拒绝、重复派生或记录失败时不先执行业务；Cancel先于Start时不再激活 |
| 等待→存活 | Mode/WaitFor、Ready/WaitSatisfied/Observation/Final | stable不是native退出；ready/input可解除等待而服务继续存活；Final结束观察但不把suspect当成功 |
| 原轮→新轮 | 独立intent与actual producer、单调round、owner、seq、字节基线与resource identity | 失败尝试不复用编号、不续旧TTL；旧样本、旧末行prompt和旧window交接不能串到新轮 |
| 结果→接收 | task_signal、冻结结果正文、预留receipt、accepted hooks、原detector ACK | 队满或receiver拒收不毁唯一结果；批量、补发、取消也保留逐条raw责任 |
| 接收→消费 | 每输入事实、observe/decision、精确input_disposition | 混批不漏事实；观察可查询和折叠而不触发模型风暴；拒收不释放等待 |
| 子调用→父调用 | continuation_state、父task原round地址、私有投影/等待/游标、结果缓存 | “回复等待”不等于委派完成；重启后结果回原调用，父未ACK只重发结果，不重跑goal |
| 显式续行 | task_round begin/finish及结果正文 | Resume从持久历史继续；新Run按当前合法面解析，仍存活执行则沿原resource续输入 |
| 寿命与退出 | 单最近期限timer、同一TTL source、真实Stopped、CloseDone | 无输入也回收；热改TTL只重排期；Close超时不伪造producer停止或提前释放资源 |

## 可见变化与副作用

- oneshot安静很久也不会仅因stable成为成功；它保持运行，直到原生退出、明确观察终结、取消或统一TTL。等待时间可能比旧版更长，语义更准确。
- resident/interactive需要可验证的ready/input证据。无pattern或成功probe的安静服务不能靠沉默自动冒充就绪；模型应在声明里给出正确证据与有限TTL。
- 被receiver拒绝的结果会继续占用pending、正文和原生证据，直到可受理；这提高结果保留量，也意味着持续存储故障会占用更多资源，不能靠丢结果“恢复干净”。
- ordinary observe不单独运行模型，但仍写事实和处置；decision与Final仍可能驱动模型。这减少不必要回合，不删除原始观察。
- 本地逻辑调用可等待超过600秒；活跃模型turn仍有预算和迭代上限，整个Task仍有TTL。远端A2A未获得本地checkpoint语义。
- owner关闭把逻辑scope挂起，父task的可靠取消/TTL才终止它；ready服务在goal完成后明确交接。资源清理因此依赖真实归属和ACK，不能再以“某个输出channel关了”代替完成。
- 进程在工具副作用之后、结果提交之前中断时，恢复明确给出需对账的中断状态，不盲重跑初始goal；外部副作用依然需要其本身的幂等性或人工/模型对账。

## 未来运行预期

| 运行情形 | 预期行为 |
|---|---|
| 长命令无输出 | ACK后继续跟踪；stable/probe是观察，TTL仍能在无人输入时触发 |
| 多个任务、其中A先ready再退出 | A等待与A stream分开收敛，A退出不能清掉B等待 |
| receiver队满或短暂保存失败 | 原source/seq/time/WaitFor/正文自动退避重试，不需要用户再问一次 |
| Resume发送失败 | 保留失败intent，旧producer/owner/TTL不变；下一尝试用新round |
| 等待期间进程重启 | 先恢复事实、私有投影和receiver，再附着原native资源与原child invocation，最后放行pending和TTL |
| 意图提交后尚未准备资源便崩溃 | 沿原intent补完prepare与绑定；已有resource不走重执行入口 |
| 子结果完成但父尚未受理 | 恢复缓存结果发往原父task round，不重新执行goal |
| 整机重启，tmux资源消失 | 明确失败/需对账，不能声称进程现场可恢复 |
| Close时kill失败或callback卡住 | Close有界返回未收敛，CloseDone不假关闭；真实停止后原尾部继续退出 |

## 验证记录与集成补位

| 范围 | 截至本文编写的状态 | 集成负责人补充 |
|---|---|---|
| Task核心 `go test -race ./agent/task` | 已通过；包括退出等待、拒收重试、恢复屏障、原轮ACK、TTL、Close、旧window与共享producer拒绑；批量/补发/取消、inline恢复、裸service载荷去重、输入批次/绑定冻结、全driver绑定后激活与取消抢先新增case通过race | 最终工作树复跑时间与结果：待补 |
| `agent/task` 注释门禁 | 已执行过0 findings；新增小改随最终lint重验 | 最终结果：待补 |
| OpenSpec归档前strict | 116个主规格通过、0失败 | 已用正常archive命令同步12个能力；归档后117个主规格通过、0失败 |
| agent、ActionTool、continuation及真实tmux矩阵 | 并行集成中，本文不替各执行负责人宣称完整通过 | 命令、版本、通过/失败项：待补 |
| 全仓short/race、build/vet、独立wechat-bot模块 | 未由本报告编写任务统一执行 | 最终结果：待补 |
| lint、生成API文档、文档引用/锚点 | 集成负责人统一执行 | 最终结果：待补 |
| 部署与长时间运行 | 尚无本次部署长跑证据 | 版本、负载、重启/失败注入记录：待补 |

本次继续沿用项目的flush-only存储耐久定位；不会因为增加receipt而升级为抗掉电承诺。发布应以最终工作树门禁和实际部署证据为准。
