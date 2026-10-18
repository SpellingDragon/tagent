# 共享行为与设计准入合同

本档描述新运行时的共享行为、生产者和消费者，不复制框架原类型，也不把未编译的候选签名称为冻结实现。字段是跨域语义清单；最终Go签名在00.3/G1联合设计及后续授权的探针中核定。本轮只规划和写检查器。

## 1. 身份与授权

| 语义 | 建立方 | 消费者／约束 |
|---|---|---|
| AgentID/SessionID | 应用创建；state接纳 | 显式目标；不猜最近会话 |
| 实例身份 | state实例建立 | 同名重建不同实例；任务来源实例与通知目标实例分别保存 |
| Assemble(ctx, decl, agents, opts ...Option) | 组合根公共入口（D-M1 起） | 治理/审批/执行档/存储档经 Option 注入（D8 注入缝，非镜像）；无治理拒绝装配（ErrGovernanceMissing）；file/skill/FunctionTool 一律经 wrapper 受理；WithStorage 驱动持久档 |
| InvocationID | 可信受理/委派入口 | 一次责任范围；同session可有多个调用，默认分开决策 |
| DecisionID/AttemptID | Agent请求边界/可观察模型尝试 | 每次实际决策/尝试独立；底层不可见重试不伪造 |
| 原生CallID | provider响应 | 原样保留；与所属实例/invocation/decision共同限定受理去重 |
| EvaluationCriterionRef（Tools 中 Name=="evaluation" 的 ToolRef） | 07.4 结构评估判据载体 | 组合根/工具解析面解析 AgentSpec.Tools 时 SHALL 跳过该引用（判据载体，非可调用工具）；组合根不得将其列入模型工具面 |
| Handoff mutation 持久编码 `handoff:<slot>:<json>` | 02.8 Handoff 记账（S1 登记） | handoffTargetOf 为唯一解析点；slot=既有 handoff 事实数+1；同身份异内容冲突对 Handoff 不可达（防冲突由 slot 递增承担）；Restore 据此折叠重放路由迁移 |
| tool/exec MonitorConfig 四值钉定 | 04.2 执行资源节奏（S3 回写） | interval=30s、stable_duration=60s、interactive_stable_duration=90s、fake_dead_duration=120s（前二 spec 钉定，后二为 04.2 实现取值现具名钉定防漂移）；未知配置键静默不消费、畸形值按名报错、compress 键 MUST NOT 消费不报错 |
| ctx 缝契约表（S3，波30） | 本变更 ctx 隐式关系全集（02.21） | ①`plugin.callPassKey{}`→CallPass{Final,Risk,TriggerSource}，写=Loop.dispatch(WithCallPass)，读=CallPassFrom 唯一 accessor，缺省=拒受理；②`task.callFrameKey{}`→CallFrame{Identity,Address,Execution}，写=Loop.dispatch/deliveryContext，读=CallFrameFrom 唯一 accessor，缺省=ErrCallFrameMissing。规则：MUST 经 accessor 读写、调用点禁裸 ctx.Value；MUST NOT 新增第三缝（缝值优先入签名）；已删键=triggerSourceKey（随 CallPass.TriggerSource 显式传递） |
| 03.8 外置生产者义务（T3 裁决） | 宿主契约 | tool/task/tmux 大结果外置的生产者接线义务在宿主侧（wechat-bot host 已承接 file_delivery/file_replica）——组合根不接（NativeAgent 红线3 防宿主工具替换+workspace→task 禁边设计所致）；09.1 收口时核宿主侧接线完整性 |
| 会话正文与快照契约（T4 裁决） | 组合根/持久化 | 会话正文=执行现场读出面（run 持有）；可序列化现场=facts+配置，正文经 durable 事实链回放恢复（03.3），不停旁挂正文快照——原型"完整现场可快照"本质=可恢复性，由 durable 链+09.4 SIGKILL 实证满足 |
| 回滚语义（T7 裁决） | 代际不可变 | 回滚=新代重发布旧内容（代际单向性更纯，不复制旧代）；state.Rollback 域内面保留（域测试消费者）；不加公共 Rollback 出口（零独立消费者） || TaskID/轮次/MutationID | task | 执行轮次不等于状态revision；一次mutation独立幂等 |
| 通知身份 | task | TaskID+轮次+类别+序号；重试沿原身份 |
| Episode及父子/依赖关系 | 应用业务边界与可信运行时 | 不用session或trace替代；未知谱系明确标注 |
| session mailbox记账 | state | 接纳（单调序）／认领（pending·claimed）／消费完成三时点分别可读、互不覆盖；同session至多一个有效模型决策，其余排队 |

地址不是授权。Query/CLI/工具读取只检查已有授权，不自行GrantRead；统计和订阅也按范围过滤。所有能力使用最终已校验的参数、目标和身份，不靠文本猜关联。

取消或结束一次invocation MUST NOT连带取消同session的其他invocation：各invocation各自决策、各自持有解析与去重身份，互不连坐。

### 1.1 event 常量集冻结口径（骨架裁决，apply-00.3 补遗 2026-10-13）

`event.Type*`/`MetaKey*` 常量集随本裁决冻结骨架范围（解除 02 域 G0 的常量集待核；`model.Message` 承载问题仍待核）：

- **Type* 骨架集（8 个）**：`external_input`、`agent_output`、`action_command`、`thinking_plan`、`context_compress`、`task_settled`、`feedback`、`governance`。除 `task_settled` 外值沿用遗留 `event` 包字符串（已存储事件向后兼容）；`task_settled` 由遗留「external_input+settle 通知」升格为一等事件类型（persistent-event-loop delta 明文要求），值为 `"task_settled"`。
- **MetaKey* 骨架集（7 键）**：`event_key`、`partition_id`、`event_type`、`event_summary`、`trigger_source`、`settle_trigger_source`、`bundle_id`（07 归因消费）；另冻结透传前缀常量 `meta_`（前缀命中即透传，不枚举）。
- **范围纪律**：遗留其余常量（wf_*、thinking_recall/knowledge、tool_chain、resident_session、consolidation、cognitive_asset_changed、rollout_id、trace_id、span_id、subtype、inbox_*、source_* 等）**不搬入骨架**——阶段二实际需要时按「共享签名修订具备证据与影响闭合」路径（expectations §0）新增，不为举例预建。
- **扩展路径**：新增 Type*/MetaKey* 须附真实消费者与验收场景，登记于本表后实施。

## 2. state生效绑定与插件

组合根从纯声明构造候选活模型、原生工具/ToolSet、文件来源和插件实例；state持生效执行代。Agent取得一个请求的借用句柄，在途不被换成另一代；释放以实际使用/生产者结束为准。

纯声明摘要包含规范化配置和明确版本引用，不包含函数、provider.Option闭包、连接、锁、租约或候选构建时间。文件内容直接热读生效，实际请求快照记录读取到的版本；结构发布与文件通道不混同。

候选私有准备期按声明次序构造候选独占资源（模型 → 原生工具／ToolSet → 文件来源 → 插件实例）并登记候选资源；store／session／TaskManager等共享面在该准备期之前建立，不属候选回收面。候选部分构造后失败（构造失败、治理段拒绝或基线已推进）时，只按构造逆序释放候选独占资源且每件一次，共享面与既有在线生效资源不受影响。

候选SHALL记录其构造基线的生效版本，提交点复核基线在构造期间未被推进；已有新发布则丢弃过期候选、不覆盖新发布，字节相同的重载不产生新代。发布前治理前置（Eval→Benchmark→ApplyGate）中必需检查缺失MUST拒绝发布，不适用或被显式禁用的检查SHALL显式记录豁免理由与范围、不计入证据通过。

借用（执行绑定取得）发生在该请求开始执行时；排队中的输入不预先绑定，故发布后开始执行的排队输入使用新代，而在途请求继续持其开始时取得的旧代。

| 阶段 | 可委派插件 | 核心责任 |
|---|---|---|
| 请求装配 | prompt/skill丰富、检索与视图策略 | 授权、预算、实际工具可见集合、请求快照 |
| 工具副作用前 | 风险规则、审批建议、允许的参数变换 | 原生参数解析、最终身份/权限、一次受理 |
| 结果产生后 | 外置策略调用注入原文存储 | 原文成功后才能发布引用；失败保留原因 |
| canonical提交后 | OTel、recorder、RL观察、反思输入生产 | 事实不回滚、不重复执行；观察缺口可见 |
| 最终呈现 | 摘要、格式、渠道视图 | 呈现drop不删事实；投递实际回执 |

Before执行授权不在事实重放时再次运行。After呈现与提交后只读观察是不同职责。框架nil“不替换”不等于呈现drop；协议适配要验证真实时点，不因为名字相近直接套用。

## 3. 原生消息与任务受理

使用锁定tRPC的 `model.Model`/`IterModel`、`Message`、`Response`、`ToolCall` 和 `tool.CallableTool`/`ToolSet`；不用镜像schema或第二工具循环。

保留原生assistant消息边界、多CallID、ContentParts、reasoning签名和透传字段。wrapper成功受理只返回一次Receipt；快任务仍走此协议。拒绝是一次配对错误响应；审批Hold可登记为未执行的等待任务，不能伪作终局Denied。

真实后台结果是新观察事件，进入原invocation的下一decision；Handoff明确改变后续接收责任时，保存原任务因果关系与新目标实例。后台结果不是原CallID的第二次同步tool response。模型请求测试要验证真实Receipt和结果正文，不用硬编码final替代。

channel与iterator使用各自原生消费方式，共用终局判断语义。usage-only尾包不能覆盖正文，partial-only不是完整答案，取消不等于生产者已停止。RL观察同一归约事实源，不另写一份terminal状态机。

模型侧终局MUST唯一（`completed`／`failed`／`cancelled`／`partial-only`／`unknown`之一），`unknown`MUST告警、MUST NOT默认completed；终局后迟到的响应SHALL保留诊断，MUST NOT派发工具、MUST NOT复活终局。后台结算的失败以失败极性（✗／failed）呈现并携带`exit_code=N`；纯空白载荷降级为单行票据。

同一invocation的就绪输入可合并为一个decision；不同父请求或不同继承代的输入MUST NOT混入同一批次。一个turn内多个工具调用SHALL各自并行受理、各自持有一次Receipt与各自义务，受理互不阻塞、不随调用数串行累加；审批等待（Hold）登记为未执行的等待任务，不占用session决策槽、不伪作终局Denied。

## 4. 提交、幂等与交付

三个结果分别表达：受理结果、存储提交结果、目标接纳结果。不能把CommitFailed/Uncertain/DeliveryRejected全部塞入一个Receipt生命周期。

提交端口在task状态转移位置调用，不放到通知发送后面补写。提交记录自包含本次mutation、预期revision、状态变化和待通知意图；实现按后端屏障能力给出已提交、确定未提交或不确定。只有已提交后才能暴露相应状态/派发；不确定先对账，不重跑无幂等副作用。

同mutation重复提交不重复事实；同身份异内容拒绝。任务同一轮次可产生accepted、progress、settled等多条通知，身份类别/序号不冲突。重复提交不等于交付完成，目标拒收或发送错误要保留outbox义务。mailbox按稳定通知身份幂等接纳。

模型response结束、任务终局、调用结束、mailbox接纳和外部送达不同。成功投递只记交付不自激；失败/拒绝按业务决定呈现，不形成回执循环。关闭拒新后保留结算出口，截止返回未收束原因。Handoff不追回已开始投递通知，重试保持其旧目标及实例。

受理回执不结清义务：成功受理MUST保持本次预登记义务至明确结清，仅未新增义务的dedup／rejected可撤本次预登记，既有义务不受影响。关闭或退役时，其在途结果与未交付义务MUST保留并由可恢复路径承接，MUST NOT丢弃、MUST NOT凭空流入同名新实例或已关闭session。Handoff后未来通知投新接收session与新实例并记录转交关联；源实例／目标实例身份分别认领，同一通知MUST NOT被两端重复生成事实。

恢复时未结算义务MUST保留，外部状态`unknown`MUST NOT被记为已完成或判死（次数阈值不作退出依据）。恢复流程（含未确认投递重放与任务清点）MUST幂等：中途崩溃后的再次恢复MUST NOT生成第二份事实。

## 5. 原文、投影与回收

原文blob可内容去重；引用metadata按owner/format/保留策略独立，后一次Put不使前一次有效引用失效。Recall逐字节返回原文；模型视图Replay复现已经记录的呈现决策，预览不冒充全文。

超大结果先原文持久提交，再生成有界预览和分段入口。副本物化成功才声明路径存在，多副本和安全复用都可实现，不强制全局唯一副本锁。副本清理不改变原文TTL，原文仍有效可授权重建。

持久恢复档在恢复清点与保护协调未就绪时禁止破坏性回收并具名报状态；不能以超时或未注入保护能力当作安全。非耐久档不宣称恢复能力。释放保护只意味着可参与保留策略，不等于立即不可读。

## 6. Query与训练观察

Query保存有界不可变快照，列表游标/详情使用同revision与授权范围，增量含同状态字段更新和删除；旧窗口与溢出报告gap/reset。源水位不同要展示，不夸全局跨store事务。OTel关闭不影响运行总图。

训练记录按实际产生点建立身份与策略快照。SDK数据与canonical业务事实分层；敏感头、Cookie、API key与URL凭据不落盘。支持声明、此次返回、采集是否保存、本地派生分别记录，无证据不推unsupported或采样token存在。

capture队列有界、发送与关闭准入一致，缺口账和seal真实。SFT导出保留原文字节摘要、消息工具配对、动作mask及数据依赖split；延迟反馈/reward派生不改旧封账。真实SFT消费者执行tokenization/collation和labels验证，未授权制品保持未完成。

## 7. 仍需G1审定的接口点
- **plugin 相位接口（已闭合于风险修复轮 2026-10-13）**：①参数变换由 Enricher.EnrichRequest 承载；②胖接口已重构为六窄接口+Plugin 注册单元（Name+Phase）。剩余待核：与框架原生 plugin.Manager/callbacks 的装配对齐（01.4/02.6 实施时验证，可能微调窄接口签名）。
- **Borrow 释放面（已闭合于风险修复轮 2026-10-13）**：Borrow 返回 *BindingHandle（Release(ctx)），释放以实际使用结束为准。

| 合同点 | 主写/联合消费者 | 最小设计证明 |
|---|---|---|
| 执行绑定取得/释放 | 01 + 02/05/06 | 发布前后模型/工具/插件同代、取消后生产者未结束不释放 |
| task提交端口 | 02 + 03 | mutation/revision与outbox一起可见，重复提交不伪交付 |
| 原文/引用/投影端口 | 03 + 02/04/06/08 | owner/format不冲突，预览恢复/精确Recall分别通过 |
| 插件相位适配 | 01/02 + 04/05/06/07 | 原生callbacks真实触发一次、最终参数一致、commit后观察 |
| Query令牌 | 05 + 01/02/08 | 授权范围+revision+过期处理明确，不自授权 |
| 训练schema/旧格式路由 | 06 + 02/03/07 | 不遗漏实际生产字段，不用trace/session猜归属 |
| 共享store并发锁边界 | 03 + 02/05 | 跨session共享store的读快照隔离级别、单事务范围与分区锁粒度未定；不以全局锁串行化、不夸大跨store事务 |
| 恢复清点完成判据 | 03 + 01/02 | 谁宣布恢复清点完成、宽限解除条件与恢复owner协调顺序；未完成时不得放行破坏性回收 |

以上是设计待核清单而非空类型占位。闭合后同步二级四件套、任务图和观察合同，再按独立授权实施。
