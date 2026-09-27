## ADDED Requirements

### Requirement: 子 agent 委派使用同代目标绑定

`AgentToolWrapper` 的同步调用、传输重试、受管异步与本地/A2A 适配 SHALL 使用发起请求所持有版本中的子调用目标与声明，MUST NOT 在调用中途读取全局最新 agent 表或可变 agent 配置。本地目标 SHALL 借用唯一 resident owner；`Run` 是协作边界适配：输入经统一事件入口进入该 owner 的处理管线，请求级上下文由公共处理器创建并接该 agent 自己的服务（含其 taskController），不能按陈旧 owner.config 先造上下文后补 lease，也不得为被调方另造单轮专用执行架构或对已存在 agent 再造完整壳。当前关联的同步结果按原语义返回发起 turn，不作为新顶层输入。

调用的 message/session 标识、metadata、外部上下文和投影 SHALL 保持在本次调用作用域；不得通过共享 activeBus、lastSessionID 或 pendingExternalEvents 临时传递而污染并发调用。原直接 Ingest API 的单次交接语义须同步保留，不新增持久消息路由协议。

后台派生调用脱离父取消上下文时 SHALL 保留同代执行绑定与来源 metadata；已结束任务的 Resume/Relaunch 若创建新的 Run，有发起者则继承其版本，无发起者则获取当前版本，再从任务所属 owner 的执行面解析目标。所选版无目标明确拒绝，不复活旧执行器；仍持 G1 的发起者不因 G2 删除目标而丢失合法 G1 绑定。

#### Scenario: 多级调用使用同版本不同 owner 视图

- **WHEN** A→B→C 正在 G1 调用，期间 G2 改变 B 的工具或 C 的模型
- **THEN** 三层保持 G1，并分别使用 G1 中 A/B/C 的执行描述；B 不误用 A 的直接工具表，后续独立调用才用 G2

#### Scenario: 去壳后同 owner 并发调用隔离

- **WHEN** 同一 resident B 接收两路不同 invocation/session/external_context 的并发调用
- **THEN** 各自投影、注入 keys、metadata 和输出只属于自身请求，收尾不改写另一调用的 bus／session；两路都使用 B 的正确资源与原 RunFlow

#### Scenario: 被调方仍是完整 tagent

- **WHEN** B 作为 A 的被调方处理输入，期间 B 以自己的任务管理器启动 C 或命令并越过首答
- **THEN** C 的结算先进入 B 的事件总线并由 B 处理，随后按原请求关联产生向 A 的输出；B 的 bus、任务域与资源不因首答结束被关闭或并入 A

#### Scenario: 进行中热更不改变子调用目标

- **WHEN** 父请求在 G1 开始（A 可调用 B），执行期间发布 G2（A 改为可调用 C）
- **THEN** 该请求的工具声明与实际子调用仍为 B；下一个开始的新请求调用 C

#### Scenario: 移除目标后的新调用被拒

- **WHEN** G2 移除了 agent B 后，模型依据新声明发起了对 B 的陈旧调用或显式重投
- **THEN** 调用被明确拒绝并说明目标不存在，不静默改投其他 agent 或复用旧执行器

### Requirement: 真实委派上下文穿透工具装饰层

冷启动、候选、回滚和子调用接线 SHALL 使用现有透明工具解包能力，将委派绑定到实际父调用的 projection；MUST NOT 因 OutputLimitTool 包裹而跳过，也不得把候选壳空投影或其他调用投影作为父上下文。构造候选或调用私有 wrapper 不得原地修改已发布共享 wrapper。

#### Scenario: 省略 event_keys 自动注入真实父上下文

- **WHEN** 配置了 event_keys 参数的委派工具经生产包装链被调用，模型未传 keys，实际父投影有可注入事件
- **THEN** 子 agent 实际收到按既有规则选出的上下文，不因外层 OutputLimitTool 而缺失；显式 keys 仍优先，空投影及不支持该参数的普通工具不被强行注入

#### Scenario: 热候选与并发子调用不混用投影

- **WHEN** G1 调用持有其父上下文，同时构建并发布 G2，且不同子调用各自有私有投影
- **THEN** G1 的实际上下文不被重绑，G2 使用正确常驻父上下文，各子调用的下一层委派读取其本调用投影，不读取候选壳或其他调用投影
