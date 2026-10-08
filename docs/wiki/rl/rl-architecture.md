# RL 侧接口（rl 包）架构与契约

`rl` 提供训练/采样侧的 HTTP 接口与其配套件：轨迹录制、可热换的模型句柄、以及对外部 LLM 端点的**安全边界**。本篇是这些契约的权威描述。

<a id="redirect-policy"></a>
## 一、端点allowlist 与逐跳重定向防线（SSRF）

### 为什么单有初始 URL 的allowlist 不够
`HTTPAPI` 的 `llm_base_url` 校验只约束**初始**目标。若不在 HTTP 客户端装 `CheckRedirect`，一个被放行的端点可以用 30x 把 LLM 客户端引到**任意主机**——这就是通往元数据服务的 SSRF 桥。因此allowlist 判定必须**逐跳**执行。

### 语义（必须逐条成立）

| 规则 | 语义 |
|---|---|
| 匹配粒度 | **精确 host 匹配、任意端口**（与初始 URL 校验同一套语义，避免两种口径） |
| 空allowlist | **拒绝每一跳**。这对应"动态重定向关闭"的部署语义：任何一跳都不得离开初始 URL |
| 跳数上界 | **自定义 `CheckRedirect` 会整体替换 net/http 的默认策略，其中包括它自带的 10 跳环路上界**，因此该上界必须在此**重新施加**：即便全链路都在allowlist 内互相 ping-pong 30x，也必须显式失败，而不是把这一回合挂到 ctx 取消为止 |
| 主机名归一 | 小写化并去端口；带方括号的 IPv6 字面量保留方括号 |
| 失败形态 | 返回错误而非静默继续，错误里带上**第几跳**、目标 host 与初始 URL，便于运维判定是配置窄了还是端点在漂 |

### 依赖方向
allowlist 判定留在 `rl` 包内，**不引入任何 provider SDK 依赖**：宿主经 `openai.WithOpenAIOptions` 之类的传输层注入口把守卫装上去。`NewEndpointGuardedClient` 提供默认代理传输 ＋ 逐跳守卫的现成客户端。

### 配置面
`TAGENT_RL_ALLOW_LLM_REDIRECT`：为 `1` 才允许动态重定向，**默认禁用**。新增逐跳守卫是对既有行为的**收紧**——若某部署曾依赖"allowlist 内端点做 30x"，升级后必须把目标 host 显式加入allowlist，否则该链路会开始报错。

<a id="swappable-model"></a>
## 二、可热换模型句柄与退役回收

`SwappableModel` 让 HTTPAPI 在收到外部传入的 `llm_base_url` 时换掉底层模型实例，而**不重建 LLMAgent / Runner，也不改事件机制**（常驻循环、注入、输出通道全部不动）——只换最底下的 `model.Model`。

### 回收的核心不变量：租约覆盖**整条流**
换出的模型进入"退役待回收"队列，等到**没有任何在途租约引用它**、且它不是当前 inner 时，才被 `io.Closer` 关闭**恰好一次**。由此推出四条必须成立的规则：

| 规则 | 为什么 |
|---|---|
| 租约覆盖**返回流的整个生命周期**，而非只覆盖"调用返回" | 流式模型在 `GenerateContent` 返回后仍在工作；只按调用计数会在流未结束时就关掉它 |
| 出错或返回 nil 流时**立即**释放租约 | 没有流可等，扣着租约会永久挡住回收 |
| A→B→A 的反弹中，**被重新选中的实例绝不关闭**；同一实例重复退役只入队一次 | 否则会把正在使用的 inner 关掉；重复入队会导致重复 Close |
| 关闭前在锁内**二次确认**租约为零 | 无锁检查与关闭之间可能有新调用启动，那是 use-after-close |
| 模型若泄漏自己的通道（永不关闭），租约就**永不释放**，宁可不关 | 保守优先：绝不关闭可能仍活着的资源 |

**调用方弃流不得把租约卡死**：转发协程往调用方通道写入时若被放弃（上游取消而没人排空），必须转入"排空上游到关闭后退出"的路径——租约**最终一定会被释放**，否则退役模型永远关不掉。取消已经发生时仍要继续排空上游，让生产方也不被卡住。

### 迭代式接口必须保真
`GenerateContentIter` 的意义在于**不隐藏内层真实的 `IterModel` 能力**：装饰器若只实现 `GenerateContent`，会把具备迭代能力的底层模型**静默降级**成"通道＋协程"路径。规则是：

- **惰性**：构造返回的 Seq **不算调用**——不加租约、不碰内层、不起协程，直到调用方真正开始迭代。
- 内层是 `IterModel` 时**直接委托它的迭代入口**（真快路径，不走通道桥接）；否则才把通道桥进 Seq。
- 租约覆盖整个迭代（与 `GenerateContent` 同构）；提前停止或 ctx 取消时排空上游，保证生产方不被卡、租约最终释放。
- **迭代路径不得把错误咽成"空迭代器的成功"**：通道形态的 `GenerateContent` 会把该错误返回给调用方，因此迭代桥接至少要把它记出来，否则同一模型走两条路会有一条静默失败。

<a id="trajectory-recorder"></a>
## 三、轨迹录制（RL 训练数据的投影面）

`TrajectoryRecorder` 也是 `model.Model` 的包装器：把每次 LLM 调用的 request/response 异步落成 JSONL，供 RL 训练侧消费。它与 `SwappableModel` **可组合**（`TrajectoryRecorder(SwappableModel(model))`），子 agent 的模型经 `TrajectoryRecorderModelWrapper` 共享**同一条 record 通道与同一个写入协程**，因此不同 agent 的调用进入同一份训练数据流。

### 四条必须成立的取舍

| 取舍 | 规则 | 代价与理由 |
|---|---|---|
| **绝不阻塞模型调用** | 缓冲通道 ＋ 后台写协程；通道满时**丢弃记录并告警** | 宁可丢一条训练样本，也不能让录制拖住对话回合 |
| 文件按 session 组织 | `{trajectory_dir}/{session_id}.jsonl`，空 session 归 `default` | 消费者按会话取用 |
| 批次号语义 | 每次调用占一个递增 `batch_index`；**新会话开始时归零**（`SetSessionInfo`） | 与训练侧的批次对齐 |
| 端点可换 | `SetModelEndpoint` 与热换模型配套，元数据里记的是**当时**的端点 | 换端点后旧样本仍可溯源 |

### 关闭与刷盘
`Close` 的顺序是：等在途调用结束 → 投递 flush 哨兵 → 关通道 → 等写协程退出。哨兵投递是**非阻塞**的：通道满时会被丢弃，此时由写协程在退出前对**所有打开文件补一次最终 `Sync`** 兜底。这条兜底是修正出来的：文档曾承诺"关闭即 drain＋sync"，而实现只 `Close` 不 `Sync`——**属于文档说谎的实例**，现在实现与承诺一致，并把兜底写明为设计的一部分（Go 的 `os.File` 直写系统调用、无用户态缓冲，故风险仅在掉电）。

`Flush` 幂等，可反复调用；它与 `record` 一样持有同一把关闭锁，避免与 `Close` 竞态。

### 与遥测的锚点关系：条件成立，不是默认成立
记录携带 `trace_id`/`span_id`（`TrajectoryRecorder` 从 ctx 的 span 取，字段 `omitempty` ⇒ 旧 RL 消费者向后兼容）。**同一份事实的三种投影确实可以互链，但这条互链是有前提的**：

- `trace_id`/`span_id` 只在**装了真实 tracer provider** 时非空。默认的 OTel noop provider 下 span context 无效（`agent/trace.go:spanTraceIDs` 对 `IsValid()==false` 返回空串），事件与轨迹两侧的这两个字段**都是空的**——空字段省写，因此"有锚点"这件事本身要按部署核对，不能默认假定；
- 事件库**没有 metadata 查询面**（`memory.QueryOptions` 不含 metadata 过滤字段），因此拿一个 `trace_id` 反查事件需要遍历，trace 锚点不适合当在线检索入口；
- 反馈与父事件的连接主路**不是** trace，而是 `parent_key` 的离线 join（见「六、授权离线导出」）：因果父指针由存储给出，与 tracing 是否开启无关。

于是准确的表述是：OTel 导出启用时，事件溯源、OTel span 树、RL 训练轨迹三者按 `trace_id` 互链；导出未启用时，事件与轨迹仍按 `event_key`/`call_id` 互链，trace 维度**缺席**。把"双向互链"当无条件成立会让离线消费者把一份空锚点表当成"没有关联"。

### 空 choices 的守卫（不做重试）
provider 返回 200 却**零 choices**（实测发生过，深度思考＋长上下文场景）：既无错误也无内容，静默落盘会让下游把"空响应"当成正常样本。因此显式在可观测层报 `EMPTY-CHOICES`。但**重试语义属于 agent loop**，不在录制层做——录制器只负责如实记录，不参与决策。

### 迭代入口
与 `SwappableModel` 同理：必须暴露迭代入口，否则偏好 `IterModel` 的流程会被这个装饰器**静默降级**。且入口是**惰性**的——调用方真正开始迭代之前不占批次号、不触发模型；提前停止时排空通道，让录制协程仍能写完它那条记录而不被卡住。

装饰器的迭代能力必须**保真**而不是藏起来：只实现 `GenerateContent` 的装饰器会把具备 `model.IterModel` 能力的底层模型静默降级成"通道 + 协程"路径。`SwappableModel` 的迭代入口因此有四条不变量：

- **惰性**：构造返回的 Seq 不算调用——在调用方真正开始迭代之前不加租约、不碰内层、不起协程；
- **真快路径**：内层是 `IterModel` 时直接委托其迭代入口，不做通道桥接，只有非迭代内层才桥接；
- **租约覆盖整个迭代**（与 `GenerateContent` 同构）：换出的模型不会在流中被关；提前停止或 ctx 取消时排空上游，生产方不被卡住、租约最终必被释放；
- **不得咽错误**：迭代路径不能把错误吞成"空迭代器的成功"——通道形态会把错误返回给调用方，桥接侧至少必须记录，否则同一模型走两条路径会有一条静默失败。

<a id="http-api"></a>
## 四、RL HTTP 接口的安全面与受理契约

`HTTPAPI` 把常驻事件循环暴露给外部训练环（任何按其契约行事的消费者）。它是**可选**组件，一旦开启就是一张能操纵 agent 的攻击面，因此以下防线都是结构性的，不是可关掉的选项。

### 四条安全防线

| 防线 | 规则 | 不这么做会怎样 |
|---|---|---|
| **单一鉴权点** | 鉴权在路由**之前**统一执行，任何端点（含只读的 `/healthz`、`/diagnostics`）都不许有旁路豁免 | 逐个端点加检查必漏一个；未鉴权就能触发副作用 |
| **无 token ⇒ 只能 loopback** | 宿主必须在 `ListenAndServe` 前调 `ValidateListenAddr`：没有 token 时非 loopback 地址直接拒绝启动，错误里列明三条出路（配 token／改监听回环／明确自担风险自行绕过） | 任何可达的调用方都能注入消息**操纵 agent**，或用 `llm_base_url` 重定向 LLM 端点，造成**完整提示词外泄** |
| **端点策略默认关闭** | 动态 `llm_base_url` 重定向默认禁用；启用必须给主机 allowlist（精确 host、任意端口）；scheme 仅 http/https，**拒绝 userinfo 与 fragment** | 见「一、SSRF 逐跳防线」 |
| **凭据不入日志** | 端点 URL 可能内嵌凭据 ⇒ 日志只记 `scheme://host` | 日志成为凭据泄漏渠道 |

token 比对是常数时间的，`Bearer` 前缀按大小写无关匹配（RFC 9110）；配置的 token 为空表示该层关闭鉴权，此时**安全闭环由上面的 loopback 守卫承担**（这是"无 token 部署形态"的唯一强制点）。

### 单点请求校验
所有上限集中在**一处**校验：请求体大小（先按 `ContentLength` 预拒，再用带上限的读取兜底）、`/task` 消息条数、单条消息内容大小、角色白名单（只允许 user/system）。`/feedback` **同样受这些上限约束**——否则持有 token 的客户端可以用一个请求把进程内存打满。`SetLimits` 的语义：零字段取文档默认值，**负值一律拒绝**（"拒绝一切请求"是配置错误，不是功能）。

服务器构造必须显式带超时：零值 `http.Server` 会**静默绑定 `:80` 且没有任何 deadline**（这是踩过的回归），故对外只提供带上限读/写/头/空闲超时的构造函数。

### 端点热换与批次的原子性
端点更新与消息受理**共用一把互斥锁**，因此一个批次绝不会横跨两代端点。重建失败 ⇒ **整批以 502 拒绝（fail-closed），旧端点继续服务**；不存在"半批已用新端点、半批用旧端点"的中间态。历史遗留的无返回值回调被包成"永远成功"以兼容，但新接线一律用带 error 的变体——否则失败的端点重建会 fire-and-forget，把训练流量悄悄打到错处。

### 受理单元与回执
整个 `messages` 数组是**一个受理单元**：单信封回执，返回 `202` 并携带批次身份 `request_id` 与 `durable`；`durable=false` 时状态写作 `accepted_volatile`（明确告诉调用方这是易失回退）。不支持信封的老 agent 退化为逐条注入，此时没有批次身份可言。入队被拒 ⇒ `503`；常驻循环未启动 ⇒ `503`（并说明要先 `StartLoop`）。

### 状态读取与未命中路由
`GET /healthz` 只做存活回报：恒 `200` ＋ `status:"ok"`，`loop_active` 镜像常驻循环是否在运行。它**不因循环未启动而报错**——"进程活着但循环没起"正是需要被看见的状态，拒绝受理的责任在受理端点（上一条的 `503`），读取端不重复判断。未注册的路由或未支持的方法一律 `404 not_found`；本接口不提供轨迹读取路径（轨迹只经录制器落盘，不从这里对外暴露），这类路径同样落在 `404` 里。

### 反馈通道：宁可标记不完整，也不静默丢
- `POST /feedback` 把外部结论绑到已产出的事件上（按 hex `event_key`）。**父事件不存在 ⇒ 404**，不给幻觉 key 记反馈；未接线 ⇒ 503。
- 绑定失败按 sentinel 分三类：父缺失＝确定性 404（重试无意义）；**事件已落库仅因果边失败 ⇒ 201 ＋ warning，并且照样入队通知**（若当失败处理，重试会写出重复反馈）；其余 ⇒ 500。
- 等待端点是长轮询（超时上限 30s，客户端取消即返回）；队列**内存态**，重启清空＝**接受的丢失**，因此它只是增量通知，全量必须回到事件库查。
- 队列满时**按最旧丢弃并计数**，响应里 `dropped_count` 与 `partial` 一并给出。判定是**只要有丢弃就 `partial=true`**，不管这次是否同时送到了较新条目——否则训练方会把不完整响应当全量用。
- 通知通道**必须真实创建**：nil channel 的接收会永久阻塞，长轮询唤醒通路就成了死代码（等待者只能每次等满超时）。

## 已知缺口与演进方向

- 守卫拒绝缺可枚举输出：只返回理由文本，"哪条票据丢了"要人读日志（见 [compression-and-telemetry](../agent/compression-and-telemetry.md)）；
- 投影键集有界依赖整表重算，重建成本随活跃引用数线性，尚无实测上限；
- 遥测阶梯的占比与驻留参数缺跨场景标定（高频短回合与长驻留任务的节奏差别很大）。

<a id="trajectory-capture"></a>
## 五、决策采集（capture v2）

采集层（`rl/trajectory_capture.go`）挂在 `TrajectoryRecorder` 之上，由 `NewTrajectoryRecorderWithOptions` + `WithCapture` 装配，**默认关闭**：关闭时 v1 录制路径逐字节不变，包括"队列满可丢、永不阻塞模型调用"。开启后它只承诺模型 SDK 边界**能证明**的那些事。

| 承诺面 | 规则 | 佐证 |
|---|---|---|
| 观测边界 | `capture_scope = sdk_request`，**从不**自称 wire：本层看得见的是 `model.Model` 的一次调用，看不见 provider 的字节 | `CaptureScopeSDKRequest` |
| 记录形态 | 一次调用一行 JSONL，`schema_version` 恒为 2，离线转换器对任何别的值直接拒收 | `CaptureSchemaVersion`、`CaptureRecord` |
| 归属来源 | owner 属性由组合根注入的 `OwnerResolver` 从 ctx 读出，采集层不自行推导、不猜"最近的一次调用"；解不出就落进 `missing_reasons` 具名枚举 | `OwnerResolver`、`buildOwner`、`Missing*` |
| 关联方式 | 每次尝试一个 `CaptureScope`（有界 4096 条），键是 `resp:<response_id>` / `toolcall:<tool_call_id>` 的**精确身份**；`Link` 绝不覆盖活映射——第二次声明报 `LinkConflict`，消费端看到 `ambiguous` 而不是一个猜测 | `CaptureScope.Link`、`ResponseKey`、`ToolCallKey` |
| 终局诚实 | 六种具名终局 `done`／`response_error`／`cancelled`／`closed_without_terminal`／`call_error`／`nil_channel`；流只走到 partial 就关闭时**不把 delta 拼成答案**，只置 `response_incomplete` | `TerminalDone` 等常数 |
| 不拖累对话 | 记录非阻塞且字节有界：单记录／在途总量／单次运行落盘三个构造期上界之一命中就停止接收新数据，但**绝不删除或轮转已有历史**；模型调用永不等待训练磁盘 | `CaptureConfig`、`DefaultCapture*` |
| 凭据 | 端点经清洗只留 scheme+host，凭据与带凭据的 URL 不入记录 | `sanitizeCaptureEndpoint` |
| trace 关系 | `trace_id`／`span_id` 只是可选观察标签，缺失**不影响** `call_id` 与绑定 | `CaptureLLMCall.TraceID` |

**这条 ctx 桥刻意搬运哪些键**：组合根的 `captureOwnerFromAttribution` 只把归因载体里已有的 `rollout_id`／`trace_id`／`span_id`／`bundle_id` 交出去，键名与 agent 侧的 owner attrs 对齐（`rollout_id` 既是本回合 session 身份也是根 session）。`capture_namespace` 不在桥上——它是装配期分区，组合根若在此伪造就等于给同一事实造第二个真源；otel 的 trace/span 由 rl 自己从 ctx 读，也不归这条桥管。`rl` 因此**不反向依赖** `plugin`。

**丢失账本不从数据队列取**：`CaptureStats` 全部读自原子计数器，因此队列堵死时账本仍然可读——而那正是消费者最需要它的时刻。

**封账（seal）**：`FlushAndWait` 把 flush 条目排进同一条队列，等写协程确认落盘后返回 `CaptureManifest`。`Complete` 要求四件事同时成立——账本静默（`Quiesced`：在途与积压归零、丢弃／超限／序列化／写／sync 计数全零）＋ 已同步 ＋ 未被运行预算拦停 ＋ 来自真实写者确认。运行关闭后再取返回**已封存的那一份**（`sealedManifest`），不二次封账；拿不到证据就是 `unknown`，绝不把"没证据"写成"完整"。

manifest 的 JSON 是**平铺计数**（`MarshalCaptureManifest` → `{"runs": {run_id: {...}}}`）：离线读者按顶层键直接取值，嵌套的 `counts` 对象会被读成 0，而 0 会被当成"什么都没丢"——一次未封账的运行就此伪装成完整数据集。capture 封账与下节的事实导出封账遵循同一条规则。

**配置面**（`trajectory_capture` 块 → `CaptureConfig`）：`enabled` 以 `trajectory_dump=true` 为前提，否则 `ErrCaptureRequiresDump`；任何负数上界一律 `ErrCaptureNegativeLimit` 拒绝，**不解释成"不限量"**，且即使该层关闭也照样校验——配置文件里的笔误不该被静默忽略。`max_open_files` 超过硬上限 16 被夹住而非服从。全部数值都在构造期读取一次，本页不承诺其可热更（热更维度判据见 [org-hot-reload](../platform/org-hot-reload.md)）。

组合根搬运给运行面的是**实际安装值**而不是配置意图：`enabled` 被前置条件拒掉时搬运的就是 `false`，端到端保持关闭，不存在「配置说要开、运行时默默没开」的分裂读数。

<a id="training-export"></a>
## 六、授权离线导出（训练事实）

`ExportTrainingFacts`（`rl/training_export.go`）是训练事实的**只读消费入口**：把已授权分区里的事件与绑定其上的反馈逐行导成快照，并附一份说明"这份快照含什么、读得有多完整"的 manifest。

| 契约 | 规则 | 佐证 |
|---|---|---|
| 授权先于读取 | `PartitionIDs` 必须非空：空表是**一次拒绝**，绝不退化成全库扫描；每个授权分区单独分页，一个分区不会把读取面放宽到邻居 | `ExportOptions`、`ErrExportAuthorization` |
| 逐条二次核验 | 每次写出行之前按授权表复查事件体：在授权分页轮里冒出来、但自身分区与事件键不一致或属外来的 body，计入 `Forbidden` 且**一行都不写** | `ExportManifest.Forbidden` |
| 行形态 | 每行是 `memory.FullEvent` 的**完整内嵌拷贝**加 `parent_key`；`FullEvent` 本身不带父指针，无据可查时为 `""` 并计入 `ParentKeyMissing`，**从不发明**。因果边存储是 `MemoryStore` 的**可选面**：接不到时仍只读事件自带的父指针，取不到就如实报「无父」而不是失败 | `ExportedFact`、`memory.RelationStoreProvider` |
| 反馈连接 | `feedback` 的 `parent_key` → 父事件 `Metadata["call_id"]`（键名单源自 `event.MetaKeyCallID`）；`missing`／`expired_or_missing`／`forbidden_parent`／`ambiguous` 是**分开的列**，不折进数值奖励、不猜父事件为何不在、不静默跳过 | `ExportManifest.Join*`、`joinOutcome` |
| 结构上不可写 | 导出器只握 `factReader`（`QueryEvents` + `GetEvent`）这一个读缝，写侧方法不可达：不改 TTL、不打 tombstone、不申请保留租约、不重编码 | `factReader` |
| 分页轴 | 用 `Offset` 而非事件键游标：库按语义 `Timestamp` 排序，`MinEventKey` 约束的是写序轴，按键轴切会**悄悄丢掉键小于游标的异步回写事件**。宁可自报不完整，也不静默丢事实 | `walkPartition` |
| 页面大小 | 0 取默认 200；落在 1..1000 之外**拒绝**而非夹取 | `exportDefaultPageLimit`、`ErrExportPageLimitExceeded` |
| 分页必须前进 | 一整页里**没有任何新增键**＝读取没在推进：具名报 `repeated page` 并停在那个分区，既不死循环，也不假装分区余下的内容不存在 | `ExportTrainingFacts` 分页循环 |
| call_id 过滤 | `CallIDs` 非空时按 metadata `call_id` 保留；**没有 metadata 索引**，因此这是授权分区内的"读后过滤"，被滤掉的计入 `Filtered` 而非报为"不存在" | `ExportOptions.CallIDs` |
| 时间界 | `Since`／`Until` 用 Unix **毫秒**，与 `FullEvent.Timestamp`、`QueryOptions.StartTime/EndTime` 同单位；0 表示开放 | `ExportOptions.Since` |
| 可核对 | `SourceSHA256` 覆盖**实际写出的字节**，`Cutoff` 记录实际生效的时间上界，`Complete` 只在没有任何分页读／事件读／写失败时为真；部分成功**同时**返回 manifest 与非 nil error | `ExportManifest` |

**快照的用途边界**：这是离线工件，**不是**生产重放源，不得用来重建在线状态。

<a id="dual-stream-cli"></a>
### 双流消费与运行时核账器

`scripts/convert_trajectories.py --strict` 读**两条流**：`--input` 是 capture 流（主事实源，一次模型调用一行 `CaptureRecord`），`--events` 是授权导出的 `ExportedFact` 流且**只作关联索引**（提供事件键、因果父键与绑定的反馈），`--manifest` 是证明主流完整的 capture 封账。主流不完整时，索引再全也补不出事实。

`scripts/verify_runtime_acceptance.py` 是这套闭环的**核账器**，不是执行器：它不跑 `go test`、不生成任何数据，只核对已跑完的真实产物之间是否互相自洽（22 个具名检查，覆盖 go test 结果、capture 封账、事实导出、数据集形状、分组不相交、拒绝清单、转换账本七类证据）。任何一项拿不到证据就是 FAIL 而非 SKIP——缺失的输入不能被当作通过的检查。

| 证据输入 | 参数 | 检查面 |
|---|---|---|
| `go test -json` 输出 | `--gojson` | 必需用例真的 pass（不是 skip、不是缺失）、真实模型面干净、各包通过 |
| capture 落盘目录 | `--capture-dir` | 封账可读且已 seal、账本声明与落盘记录数对得上、目录里确有记录 |
| 事实快照两份 | `--facts` `--facts-manifest` | 快照存在、manifest 完整、计数与快照行数对得上、`source_sha256` 与快照字节对得上 |
| 数据集目录 | `--dataset-dir` | 样本非空、抽样 `labels`/`loss_mask` 形状一致、train ∩ test = ∅（按 `capture_namespace` + `root_session_id` 分组不跨侧） |
| 拒绝清单 | 数据集目录内 `rejected.jsonl` | 存在且逐行含 `reason` |
| 点名用例 | `--expect-tests` | 必须通过的用例清单（真实模型场景等） |

`--report` 写机器可读结果；退出码 0 表示全部必需检查通过。

<a id="offline-converter"></a>
## 七、离线轨迹转换器

`scripts/convert_trajectories.py` 是规格点名的离线工具（`openspec/specs/trajectory-recording`）：读取 `TrajectoryRecorder` 落盘的 JSONL，产出 HuggingFace 数据集——SFT 模式为 `{input_ids, loss_mask}`（prompt 位 0、completion 位 1），RL 模式为 prompt-only 的 `{messages}`。它只用标准库，不依赖任何训练框架的存在，转换在训练环境之外即可完成与校验。`--strict` 形态读的是「五、决策采集」与「六、授权离线导出」两节定义的双流输入（capture 主流 ＋ facts 关联索引 ＋ 封账证明），见 [双流消费](#dual-stream-cli)。

```bash
# SFT
python3 scripts/convert_trajectories.py --input data/trajectories/ --output data/sft/   --tokenizer Qwen/Qwen2.5-1.5B-Instruct --mode sft
# RL prompt-only
python3 scripts/convert_trajectories.py --input data/trajectories/ --output data/rl/ --mode rl
```

### 退役记录：AReaL 在线训练桥（2026-10）

桥侧三件（`tagent_adapter.py`、`train_tagent.py`、`train_rl_config.yaml`，原址分别在 `train/rl/` 与示例目录）已退役删除：它们面向 **AReaL 改名前**的 `train.*` 包布局（现行 AReaL 顶层包为 `areal/`），`train_tagent.py` 的 `sys.path` 存在把点号模块名当目录段的字面 bug，且 `run.sh` 引用的 `areal_config.yaml` 从不存在、CI 无 Python 覆盖——无规格承诺、无法验证的陈旧面。保留的是活面：本 `rl/` Go 包（轨迹录制、可换模型、HTTPAPI）、`tagent.rl.yaml` 运行时配置、上述离线转换器。重新接训练环的条件：按现行 `areal.*` 布局重写适配器，且其点号模块引用须经 `codetools dotted-refs` 门（仓库内可解析或显式登记外部包）。
