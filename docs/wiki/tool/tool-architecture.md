> **历史文档（historical-verbatim）**：本篇是已移除生产实现的压缩重写，原版逐字归档于 [docs/archive/wiki/tool/tool-architecture.md](../../archive/wiki/tool/tool-architecture.md)；当期契约以 `tool/{exec,mcp,remote,subagent}` 码面为准（[docs/api/](../../api/index.md)），当前架构入口见 [docs/wiki/README.md](../README.md#当前架构速览)。

# tagent/tool：工具面契约与历史机制对照

当期 `tool/` 只有四个产品包：**`exec`（外部执行的资源与租约）、`mcp`（活注册表与调用网关）、`remote`（显式寻址的远程委派）、`subagent`（子 agent 声明装配与 handoff）**。原 `tagent/tool` 的召回、知识、动作、任务与 Plan 工具面已移除，其判据压缩在第六节之后，原版在归档中。

## 一、工具面地图（当期）

| 包 | 职责 | 边 |
|---|---|---|
| `tool/exec` | 一次外部执行的**声明**（`ExecutionDecl`）、资源身份（`ResourceKey{Kind,Key}`）、执行器端口（`Executor`）与资源管理器（`Manager`：获取/续租/回收/清扫） | 只依赖 `context` 与标准库；tmux 机制在端口之后 |
| `tool/mcp` | 服务端名→活工具集的并发安全表（`ServerRegistry`）、`mcp_call` 网关（**恒定声明**，调用时读表） | 复用框架原生 MCP 工具集，自身不说 MCP 协议 |
| `tool/remote` | 调用方**显式声明**可寻址的远程 agent（`Endpoints`），一次调用携带 `CallIdentity` 与 `Handoff` | 传输走 A2A；不做默认对端解析 |
| `tool/subagent` | agents 文件（YAML frontmatter + prompt）→ `Definition`；`Assembly` 装配工具/技能/MCP；`handoff` 结构化交接；`presets` 解析 `knowledge`/`recall`/`call_subagent` 引用 | 检索能力由调用方注入，本包不自持检索 |

工具面共同的硬边界：**声明是模型侧请求前缀的一部分**，因此任何会随部署配置漂移的东西（向量能力、注册表内容、远端地址）都不得进声明，只能在调用期解析。

## 二、执行面：资源、租约与证据（`tool/exec`）

<a id="action-tool"></a>
### 2.1 声明与资源身份

`ExecutionDecl` 的每个字段都是**纯值**：`Command`（按调用方声明的样子）、`Args`（有序）、`Dir`（空串表示"调用方没有声明目录"，不等于"没有目录适用"）、`Resource`。句柄、连接、锁、租约**不属于声明**——资源租约属于持有它的执行器；因此重启前记录的声明，重启后重放出同一次执行。

`ResourceKey{Kind, Key}` 一起命名一个资源：两者都一致的声明指的是同一个资源，**已在该键下持有的资源不会被第二次打开**。`KindTmux` 是唯一依赖真实 tmux 二进制的资源族。

**tmux 是硬依赖**：`IsTmuxAvailable()` 每次调用做真实 PATH 查找，没有任何构造可以替代这次探测；不可用时获取以 `ErrTmuxUnavailable`（`exec: the tmux capability is not available on this system`）拒绝。缺依赖是启动/获取期就该看见的事，不是运行期悄悄换一种语义的事。

### 2.2 执行器端口：报告观测到的，不报告被请求的

`Executor` 四个方法：`Start`（返回资源键）、`Probe`（返回 `ProbeResult`，其 `State` 是 `ProbeState` 三值：`ProbeUnknown`（零值，判不了，只升级诊断、绝不判死）／`ProbeAlive`／`ProbeDead`）、`Capture`（运行期观测到的帧）、`Stop`（返回 `StopEvidence{Resource, Confirmed, ExitCode, Reason}`）。四条红线：必须持有自己持有的每个资源的租约；**必须报告一次探测或一次停止观测到什么，而不是请求了什么**；停止未确认的资源不得释放；未确认的退出不得报成干净的退出。

`Manager` 在端口之上提供：

- **获取与复用**：`Acquire` 校验键非空、TTL 非负；同键复用已持资源；可用性探测在启动任何资源族之前执行。
- **租约**：`Renew` 以监控节奏续租——**长执行不会因为"花了很久"被清扫**；`Sweep` 对到期租约逐个驱动停止，**到期本身不释放任何东西**：停止未确认者连同报告一起继续持有（`SweepReport.Evidence`）。
- **观测**：`Observe` 把无法判定的探测累计成连续 unknown 段，达阈才升级诊断，不把不确定读成退出；`Capture` 返回运行期观测到的帧，不用中途的部分读取覆盖它。
- **释放**：`Release` 返回 `ReleaseEvidence{Released, Stop, Reason}`；`Released` 只在**停止已确认**时为真，否则资源保持持有并说明原因。
- **解释面**：`Explain` 报告持有状态、节奏、租约截止、续租次数、是否重启接管、连续 unknown 数——排障读这一处，不猜。

### 2.3 工具面

`RunTool` 按参数声明的资源键获取（同键复用），回答执行记录到的内容；调用打开的获取同时确定这代执行的**监控节奏**，租约按该节奏续期。`ReleaseTool` 显式停止并按证据回答：停止未确认时**保留资源并报告保留**，不报成结束。两个工具的参数集合是**闭集**，闭集外的键被拒绝。

## 三、监控节奏与状态判定

<a id="tmux-monitor"></a>
### 3.1 节奏

`MonitorConfig` 是"轮询间隔 + 静默阈值"的一组值：`Interval`（基础节奏）、`StableDuration`（普通会话稳定窗）、`InteractiveStableDuration`（TUI 稳定窗）、`FakeDeadDuration`（静默越过此阈且进程仍在，按无响应处理）。**dense→sparse 的边界，就是同步等待转异步 ack 的点**——dense 窗口内结算则内联返回，越窗返回 ACK，后台结算经 `task_settled` 回收 turn。

### 3.2 三态探测

探测单源化：`alive, known := SessionAlive3(id)`（list-sessions 单源）。`has-session` 的 exit code 无法区分 dead/unknown（实测 tmux 3.6a 同为非零），故以 list-sessions 输出判定；命令不可辨时返回 `known=false`：

```go
alive, known := tm.executor.SessionAlive3(session.ID)
if !known {
    session.ProbeUnknownCount++
    if session.ProbeUnknownCount < tm.probeUnknownLimit() { // 默认 3（fail-dead 加闸）
        return SessionRunning // 连续 unknown 未达阈：保持现状，不误杀
    }
    // 达阈才按 dead 处理（tmux 抖动不再屠杀常驻会话）
}
session.ProbeUnknownCount = 0 // 可辨探测到达——重置连续计数
```

**探测失败不直接判死**：unknown 连续达阈（默认 3 次）才按 dead 处理——三态化与加闸构成双层防线。假活（进程在、pane 活、输出稳定，心跳有响应）以**原会话 ID** 重启；假死（心跳也无响应）强制 kill。逐条判据（静默≠假死、通知三门、真实尾帧、命中计数、命名单例、重挂、pipe 日志、错误可见性）见 [tmux-action.md](tmux-action.md#quiet-vs-dead)——那些判据的失效表现不是报错，而是误杀健康任务或重复投递通知。

## 四、跨重启连续

<a id="resident-continuity"></a>
### 4.1 声明式投影

`TaskSpec` 的生命周期钩子（Relaunch/ResumeFn/Alive）是闭包，不可序列化。`DeclarativeFromArgs(args, sessionID)` 把 spawn 的 `ActionArgs` 投影为可序列化的 `Declarative`：**params 为白名单键全集，严格解码——未知键拒绝**，防半重建的 spec 静默变行为；投影随 `task_spawned` 入事实链。重启后 `SpecFromDeclarative` / `SubagentSpecFromDeclarative` 按承诺表重建闭包：

| Kind | Relaunch | Alive | Resume |
|---|---|---|---|
| command | ✅ fresh startSession | ✅ TaskID 会话核查 | ✅ 新建 detector 后重挂（重挂前返回引导） |
| subagent | ✅ 经 `SubagentRedispatcher`（spawnKey 幂等去重） | ✅ | ❌ rounds 链无事件源，返回引导 |
| generic | 展示 | — | — |

### 4.2 常驻元数据与重挂

- `ResidentMeta`（command/origin/task_id/…）持久化于**可配的 `resident_meta_dir`**（离 `/tmp` 的持久卷）；旧记录零值容错。
- spawn 全参与终态结局经 `SetResidentRecordSink` 写 `resident_session` 事件（**记录-only：不发 bus、不进投影**）。
- **汇接线归各 owner 自己**：换代时把 sink 重挂到**所属 agent 自己**；spawner 的 TTL 源读所属 agent 的记录投影，不到祖先。
- `ReattachResidentSessions` 幂等重入：以 **tmux list 为 liveness 真源**对账元数据目录，逐会话新 detector 入 monitor 回调链 → 任务板 suspect 任务经 **TaskID 桥**（`IsTrackedSession`）确定性提升回 running。孤儿清扫的 orphan 语义**只指无主生成名会话**：`n-` named 会话排除，否则 cleanup 先于 reattach 屠杀常驻。

### 4.3 重入（resume）的合法源与两形态

```
running --settle(窗口内)--> stable --resume--> running
alive_detached --resume--> running
 completed/failed --resume(round 型)--> running
running/suspect/cancelled --resume--> 拒绝并引导
```

合法源状态按**轮次边界**定义（存活类＝会话重入；完成态＝round 型执行器自然续行点）；并发 resume 占坑单胜（内置 running 再调 ResumeFn，失败回滚）。两形态：tmux 用**同一 detector** `Rearm(baseline)` + `SendKeys`——detector 绑会话而非轮次，回调/watch 永不换手，零换绑零竞态，输出取基线后增量；subagent 新 Run + 任务链还原器（本任务前序轮次注入 external_context，`resume_context_rounds` 封顶，无进程复活）。任务层以 `detector != task.detector` 一行区分：同 detector 不退役 watch，新 detector 走 watchDone 退役（防泄漏与陈旧信号串轮）。

## 五、MCP 面

<a id="mcp-live-registry"></a>
### 5.1 发现读的是活注册表

`ServerRegistry` 是服务端名→**活原生工具集**的并发安全表，也是调用网关与发现**在调用时**读的同一张表。注册只移动活连接：**没有任何方法改变 agent 已声明的工具集**，因为网关工具声明固定、调用时才读表。因此运行期新注册的服务**立刻可被发现**，无需重建 agent；被移除的服务也立刻消失。

- 一次释放只 `Close` 一次；移除或关停一个仍有在途调用的服务，会**等该调用结束后**才关连接，释放错误全记在关闭账本上供 `Close` 读回。
- 一个服务工具为空（例如连接失败被框架吞掉而 yielded 无工具）**不得阻塞**其他服务的发现结果。
- 发现结果必须**如实给出调用方式**：带 `mcp_call(server=..., tool=...)` 的确切形式与输入 schema，且**不得**给出 exec 命令式的假调用路径——那会让模型照着不存在的方式去调。
- 匹配除子串包含外有 **token-AND 回退**（按空格/下划线/连字符切词，逐词命中即可）：`web search` 必须能匹配 `web_search_prime` 或描述里词序不同的表达。

<a id="mcp-gateway-injection"></a>
### 5.2 `mcp_call` 网关用的是注入的那一份活注册表

网关从配置面取得同一份注册表（`PlainToolFactoryConfig.MCPRegistry`）后交给 `NewCallTool`，因此**它看见的服务集合与装配根同一份**——运行期新注册的服务对已建好的 agent 立刻可用。没有注册表可注入时**仍然必须构建成功**：注册表缺席只改变调用结果，不改变 agent 能否建立。

声明是**常量**：`MCPCallInputSchemaJSON` 解析一次成包级 schema，绝不是"注册表里有什么"的函数——声明不随活表漂移。

调用侧的失败一律**以结果形态返回，而不是 Go error**，且带足以自纠的清单（`tool/mcp/call.go`）：

| 情形 | 返回 |
|---|---|
| 一个服务都没注册 | 显式空态，并提示两条来源（配置文件声明／运行期注册） |
| server 名不认识 | 具名报错 **＋ 当前可用服务清单** |
| tool 不在该 server 上 | 具名报错 **＋ 该 server 的工具名清单**（服务枚举为空时上报 MCP 依赖故障） |
| 内层调用失败 | 具名报错 **＋ 输入 schema**，让模型改参数重试而不是重复同一个名字 |

只有清单可列时才列：空清单不会伪装成"可用服务为空"的成功结果。

**typed nil 坑**：把一个类型化空指针赋给接口字段，接口本身**并不等于 nil**，于是"有注册表就启用"会假判成立、调用期才炸。装配侧必须在赋值之前判空——宁可不注入，也不要塞一个带类型的空指针进去；`normalizeRegistry` 是这条守卫的落点。

## 六、远程委派面

<a id="delegation-retry"></a>
<a id="十七、委派调用的取消归属与重试形状"></a>
### 6.1 显式目标、调用身份与重试形状

`Endpoints` 是调用方对"可寻址的远程 agent"的**显式声明**：一个 agent 名对应一个 `Target{AgentURL, AgentID}`，两项都全才算完整。声明里没有的 name **按名拒绝**，没有任何一次调用自行解析出目的地。

`CallIdentity` 是归属身份：两个尝试必须能被认成同一次调用，缺身份即 `ErrCallIdentityMissing`；同一个 call identity 绑定两个目标即 `ErrCallTargetConflict`——**一次调用保持它开始时的目标**。

`agent.Run` 是异步的：它立即返回事件 channel，子 agent 在后台 goroutine 里产出事件。于是取消的归属必须跟着流的末端走，而不是跟着函数返回走——**`cancel` 不得 `defer` 在发起处**，那会在调用方读完 channel 之前就把上下文取消掉；正确做法是把返回的 channel 包一层 goroutine，**在 channel 关闭之后调用 `cancel`**。

重试策略位于上一层 `runAndCollect`，规则是**一次 REMOTE 尝试至多重试一次**，且重试包住整个尝试（发送 + 排空）。理由在于传输失败实际出现的形状：A2A client 把失败的请求上报为一个携带 `Response.Error` 的**事件**，而 `Run` 自身返回 channel 与 nil error；只判断"`Run` 返回错误"的分支永远不会命中它被写出来时想覆盖的那种形状——一次 503 会直接把父调用打死（见 `a2a_delegation_test.go`）。两次尝试使用**同一个 invocation 与同一个 wrapper 实例**：租约继承意味着重试乘在发起调用的绑定上，绝不对"那时已经发布的代"重新解析。

### 6.2 交接的携带面

`HandoffCarrier` 是 handoff 在远程调用里的形态：`Validate()` 回答"这份交接是否指名了一次委派轮能承接的东西"，`DataPart()` 回答"整份交接随消息走的那一个 part"。两条红线：必须以**整份交接**的 part 回答，不是它的碎片；不得回答一个声明另一种 kind 的 part。

## 七、子 agent 装配面（`tool/subagent`）

- **agents 文件**：围栏 `---` 的 YAML frontmatter + prompt 正文；frontmatter 是**闭集**（`name`/`description`/`model`/`prompt`/`skills`/`tools`/`mcp`），未知键拒绝——半声明的 agent 不静默变行为。
- **装配**：`Assembly` 按声明装配工具/技能/MCP 引用，并报告它**实际持有**的 grants；委派轮拿到的授权以装配结果为准，不以声明文本为准。
- **handoff**：`handoffArgs` 是闭集（意图、验收约束、不得自决的决定、起始事实），未声明的键拒绝；未声明 handoff 的调用**不构造** envelope，请求保持原形。
- **presets**：`knowledge` 引用装配到 trpc 知识工具（源读入内存向量库）、`recall` 引用包住调用方注入的召回面、`call_subagent` 解析到本包工具；**未知引用留给宿主贡献的工具**，不在此拒绝。检索行为不属本包：`RecallFunc` 由调用方给。

## 八、历史契约：注册表与声明

<a id="tool-registry"></a>
### 8.1 三阶段生命周期

原 `tagent/tool` 采用"实现层指定 → 注册层注册 → 配置层组织"：各子包导出 `PlainToolFactory`；`RegisterBuiltinTools()` 用 `registerOnce` 统一注册；每个 agent 经 `Tools []ToolRef`（`kind=agent|tool`）声明使用哪些工具，构建时 `buildToolFromRef` 分派到 `buildAgentToolRef`（递归建子 agent + `AgentToolWrapper`）或 `buildPlainToolRef`（注册表取工厂 + `PlainToolFactoryConfig`）。

`PlainToolFactoryConfig` 是运行时依赖的注入面：MemStore、SkillRepo、MCPRegistry（live 优先）/MCPToolSets（legacy）、Degradation（per-agent 退化状态机）、`ReadPartitionIDs`、WorkspaceRoot、Properties。

内置 plain tool 分组（当年注册名，码面已变；当前工具面见 [docs/api/](../../api/index.md)）：

| 组 | 工具 |
|---|---|
| 执行 | `exec`（声明名 `action`） |
| 文件 | `read_file` `save_file` `list_file` `search_file` `search_content` `read_multiple_files` `replace_content` |
| 知识 | `skill_search` `skill_load` `mcp_discover` `web_search` `duckduckgo_search` `memory_query` |
| 召回 | `recall`（统一入口）+ `recall_query` `recall_get` `recall_recent` `recall_trace` |
| 任务 | `list_tasks` `cancel_task` `relaunch_task` `resume_task` |
| 其他 | `spec`、`mcp_call`、`memory_consolidate`、`memory_health` |

`refine` 与 `govx` 工具**不走注册表**：仅 entry agent 且 `evolution.enabled` 时由 buildAgent 直接追加，且先于治理包裹（rollback 改受控产物须过治理闸）。

<a id="extra-params"></a>
### 8.2 附加参数通道（`ToolRef.extra_params`）

子 agent 工具默认只有 `request`（+ `event_keys`）。需要路由级小参数（如 plan 的 `action`/`name`）时经 `extra_params` 声明：

| 阶段 | 行为 |
|------|------|
| 声明 | `Declaration()` 将声明参数并入 InputSchema（含 enum）；`request`/`event_keys` 不可被遮蔽 |
| 透传 | `Call()` 收集本次调用中出现的声明参数，打包为 `{params..., request}` JSON 作为子 agent 的消息体 |
| 未声明/未传 | 消息体仍为纯文本 `request`——其余子 agent 行为零影响 |
| 幂等键 | 携带非空 `name` 时任务 Key = `agentName:name`（同名单飞）；否则 `agentName:request` |

走消息体而非 RuntimeState 的理由：消息体是子 agent 的 LLM 与自定义 `Run`（如 `PlanAgent.extractAction`）**共同可见**的唯一位置。

<a id="govx-entry-only"></a>
### 8.3 治理面工具五件套（`tool/govx`，entry only）

`goal_declare` / `goal_list` / `goal_resolve` / `denial_query` / `approval_list`：与 refine 同槽位由 buildAgent 追加，先于治理包裹。goal 声明/关闭**双写 governance 事件**（重启经事件回放重建，goal 门不因重启静默重开）；`denial_query` 给最近拒绝记录（工具/原因/风险级/来源 agent）；`approval_list` **只列不批**，批准权始终在人。classifier 规则 `govface.readonly` 将五工具判 **low**，避免 strict 模式预算耗尽时 `goal_declare` 自我拒绝的死锁。

<a id="tool-agent-factory"></a>
### 8.4 ToolAgentFactory 只装配执行配置，不构造 agent

出生与发布各只有一条路：org 拥有唯一的**出生路径**（`wireAgent`——store-lease 槽、drain 接线、任务域恢复都在那里接上）与唯一的**发布路径**（`stageOrgGenerations` 为每个属主、每一代推进一张面）。

工厂若返回成品 `*TagentAgent`，就在这两条路之外造出了第二种 owner 出生：它永远无法经面路径推进，每次发布都必须重建整个 agent，留下无人关闭的孤儿；每个被钉住的委派会持续读到过期的构造配置。返回的 `*TagentConfig` 逐字采纳：

| 字段 | 规则 |
|---|---|
| `Name` | 尊重工厂的选择；为空时回退到注册 id |
| `MemoryStore` | 用该 name 所借用的 org store 填充 nil——自己开 store 的工厂不得同时领到 org 的 lease |
| `MemStoreRelease` | 恒为 org 的那一个，由本次调用之后的装配填充——工厂既不留存也不自造释放句柄 |

装配为它构造的每一代重新调用工厂，并把那一代的值交进去。

## 九、历史契约：召回

<a id="recall-unified-entry"></a>
### 9.1 `recall` — 统一入口，参数形态即路由

模型侧单工具；确定性形态零 LLM：

| 参数形态 | 路径 | 特性 |
|---|---|---|
| `items=[{key,hint?}]` | 批量 `GetEvent` 精确回补（原序） | 零幻觉；未命中显式 `miss`；hint 回显对账；确定性优先级最高 |
| `turn_key`(+max_steps) | 因果链回走（`walkTurnChain`，至 `external_input` 停） | 重建整轮执行过程（含被压缩丢弃的工具步骤），时间序；带 `complete`/`capped`/`reason` |
| `query`(+since/until/event_types) | 引擎就绪时 hybrid（关键词∪向量 RRF），否则纯关键词 | 入口协议与 Declaration 恒定；逐跳降级保底关键词 |
| `orchestrate: true` | LLM 多跳编排保留形态 | 未接线时返回明确指引，不静默降级；确定性形态永不进 LLM 路径 |

输出协议统一：条目 `{key(hex), type, summary, content, time}`；优先级 orchestrate > items > turn_key > query。收敛自 `memory_recall`+`memory_turn`+recall 子 agent 三张脸（注册名已退役，内部实现保留为路由目标）。

`turn_key` 的完整性三字段，且只描述这一次回走：

| 字段 | 契约 |
|---|---|
| `complete` | 只有真正回走到该回合的 `external_input`（含该条）才为 `true`；此时 `reason` 必为空 |
| `capped` | `max_steps` 先用完；缺省 20、上限 50（超出按 50 取，不报错也不夹紧提示） |
| `reason` | 六个具名读数之一：`missing_ancestor`／`relation_error`／`relation_unavailable`／`cycle`／`limit`／`no_parent_edge` |

三条硬规则：

- **不完整不等于空**：走不完时仍返回**确实读到的那一段**加具名 `reason`，绝不返回空条目冒充"这个回合没有过程"。读方拿到空历史与拿到断链，是完全不同的两个结论。
- **不推断原因**：`reason` 说的是"回走到这里读不到了"，不是"这条记录被 TTL 删了"。六个读数的语义清单与读者处置归[记忆架构](../memory/memory-architecture.md#causal-chain)，本处不复述——复制一份枚举必然随码面增删而失真。
- **字段是加法**：`reason` 恒为 `omitempty`（完整时干脆不出现）。`complete`/`capped` 在 `memory_turn` 子工具里始终输出；统一 `recall` 入口的三个字段整体 `omitempty`，`items`／`query`／`orchestrate` 形态里不出现——老读方遇到缺失按"不完整"处理，不会把缺失读成"完整"。

`memory_turn` 与 `recall` 的 `turn_key` 形态共用同一个回走函数（`walkTurnChain`），因此两处判定不会各说各话：同一个断点、同一个 `reason`。

<a id="tool-accessor"></a>
<a id="declaration-stability"></a>
### 9.2 声明区与向量能力隔离（前缀缓存稳定性）

recall 一族工具对模型呈现的 `Declaration` 里**没有任何向量或嵌入参数**：四个工具的声明在两次独立构造之间**逐字节一致**，与"这份部署有没有开向量"完全无关。

是否做引擎融合在**运行期的召回路径**判定，判据是 accessor 暴露的引擎能力位 `MemoryEngine().Capabilities().Vector`。存储侧另有一个 `SupportsVectorSearch()` 探测面，**召回路径不看它**——两者混淆会把"能力探测"错写成召回分支的前提。

声明文本还不得出现 `embedding`／向量存储／索引结构／融合算法一类实现字样：声明是模型侧请求前缀的一部分，一旦随部署配置漂移，整段前缀缓存失效，且模型在两次会话里看到的是同一个工具的两种签名。

原包根 accessor 源文件（已随包移除）里的 `MemoryStoreAccessor` 与 `SkillRepository` 是"工具自访问"的落点：工具经最小接口自访问记忆与技能，不耦合具体存储实现。

<a id="recall-agent"></a>
### 9.3 RecallAgent（orchestrate 分支的内部引擎）

原 `RecallAgent` 用内部 LLM React 循环理解查询意图、综合历史事件为连贯回答，适用于多跳追溯与跨轮收窄；作为统一 `recall` 的 `orchestrate` 分支编排引擎后，其子工具**不再对主 agent 直接暴露**。设计决策：需要"思考-行动-观察"循环 → config-driven TagentAgent + AgentToolWrapper；单一功能/执行器 → 简单 CallableTool。

<a id="recall-contract"></a>
### 9.4 检索、降级与诚实回报契约

**两种输入形态与优先级**：`items`（索引卡/时间线上的 `[evt_…]` hex 票据）优先于 `query`：手里有票据时精确回补，只有模糊线索时才走关键词/语义检索。两者皆缺时返回明确的用法错误，不做猜测试图。

票据的**写法宽容**也是契约：`[evt_HEX|type]`、剥掉方括号的 `evt_HEX`、裸 `HEX` 三种形态都必须落到同一个事件（解析以 hex 为先，十进制转写仅作老兼容）。卡片行里整段切出的字符串必须可直接当票据使用。

**分层与降级**：① 引擎混合检索（关键词∪向量，RRF 融合在引擎内）——仅当查询词非空、accessor 暴露记忆引擎且引擎声明支持向量时启用；协议与工具声明因此零变化，prefix-cache 不受影响。② 引擎不可用、报错或**零命中/全部悬挂**时降级到纯关键词，不向调用方报错。两段式保持：引擎只返回排序票据，全文再经批量水合；悬挂票据与墓碑命中**自然消失**，不返回半条内容。

| 防线 | 规则 | 为什么 |
|---|---|---|
| 入参上界 | `limit` 钳到 `maxRecallLimit`(100)；`items` 水合钳到 `maxRecallItems`(50) | 一次投数百票据不得变成无界 `GetEvent` 风暴 |
| 超取补偿 | 引擎按 `limit*2` 返候选，水合过滤后裁到 `limit` | 死键占 topK 会造成**静默少返回** |
| 分区二次防线 | 水合前按 `EventKey` 高位分区过滤 | 持久化/重建链路一旦缺 pid，只靠存储侧过滤会**跨命名空间泄漏** |

**结果必须诚实**：被上界丢弃的票据与不足上限的截断都写进 `Message`；未命中的票据逐条标 `miss`，不静默省略；**零结果不等于"没有历史"**——必须回报检索范围（自身 + `read_namespaces`）并给出确定性下一步（更短关键词、加时间范围、或用手里的票据走 `items`）。批量水合优先 `GetEvents` 一次取回；可观测 span `tagent.recall.query` **只带元数据，查询内容零入 span**。

`orchestrate` 未接线时必须显式回报"未接线"，并给出 `query` → `items` → `turn_key` 的自助路径，**不得静默降级成单一形态**。

<a id="recall-subtools"></a>
### 9.5 召回子工具的读回语义

| 子工具 | 语义 |
|---|---|
| `memory_query` | 按时间范围（Unix 毫秒）与关键词过滤；**最新优先**；关键词是摘要与内容的**大小写无关子串匹配** |
| `memory_get` | 按 key 取全文，可选 `include_parent` 带回父事件摘要（父关系存于关系存储，不占事件字段） |
| `memory_recent` | 取最近若干条，可加时间范围；条数有上限，超限即截断 |
| `memory_trace` | 从给定 key 沿父链**回溯**，步数有上界 |

三条硬语义：**断链即止**（遇到取不到的事件就停止并保留已取部分；首事件即取不到才报错，绝不跨断点猜父链）；**整轮重建以 `external_input` 为界**（回走到回合起点即停，反转为时间正序；摘要与内容都受长度裁剪，否则单条就能撑爆上下文）；**上界截断与"完整"分家**（`max_steps` 用尽而尚未到 `external_input` 时 `capped` 为真、`complete` 必为假，已取部分照常返回）。

## 十、历史契约：知识获取

<a id="knowledge-agent"></a>
### 10.1 三层渐进披露（技能）

技能内容**绝不整篇倾倒**，按需要的深度逐层给：

| 层 | 工具 | 给什么 |
|---|---|---|
| 1 | `skill_search` | 名称＋描述（取自 YAML front matter） |
| 2 | `skill_load` | 名称＋描述＋用法摘要（正文上限约 2500 字符，**按章节边界裁剪**） |
| 3 | `command` | 需要更深细节时由调用方自己读技能文件 |

裁剪只在限制区间的**后半段**里找最后一个 `## ` 章节标题下刀，否则会把正文在它第一个真实章节之前就切断。截断时必须把"原文总长＋完整文件路径＋如何继续读"一并回报；文档清单只列路径不带内容。

装配默认：迭代上限 5（知识获取只需很少迭代）、温度 0.3（要准确而非创造性）、输出上限 4096。提示词与描述"文件优先、内嵌兜底"（`PromptConfig` → `PromptDir/knowledge_agent.md` → 内置默认）。记忆存储必须与主 agent 同源，否则刚写入的知识召不回。

<a id="memory-query-hard"></a>
### 10.2 `memory_query` 的两条硬要求

1. **必须注入可读分区**：在按分区隔离的存储上，空分区列表意味着**什么都不扫**。分区范围（自身命名空间优先 ＋ `read_namespaces`）必须在构造期注入。
2. **存储故障不得塌缩成"没有历史"**：查询失败是强信号；静默返回空集会让 agent 误判"无相关知识"而去做冗余搜索，故障同时变得不可观测。因此失败时返回一条显式的 `query_error` 结果项，与"确实没有历史"可区分。

<a id="websearch-backend"></a>
### 10.3 两个 web 搜索工具互补，后端是结构化 API

`duckduckgo_search` 取即时答案类的事实/百科信息（快、结构化）；`web_search` 面向一般网页内容（时事、教程、文档），是**主用且最可靠**的那个；知识子 agent 默认同时挂上两者。

`web_search` 的后端是**结构化搜索 API**（智谱 Web Search），取代早期"抓取多个搜索引擎 HTML"的实现：API 返回标题/链接/摘要/媒体/发布日期并自带意图识别，而引擎 HTML 随时改版导致抓取无声失效。

| 情形 | 行为 |
|---|---|
| 未配置 API key | 返回带 `Message` 的空结果而**不报错**（缺配置不该中断 agent 回合）；key 取自环境变量，变量名由 `api_key_env` 配置，默认与模型 provider 共用 |
| 空查询 | 直接返回 `Message: empty query`，不发请求 |
| HTTP 非 200 或响应体带 API error 对象 | 组装成人类可读的 `Message` 返回，同样**不报错** |
| 请求条数 | 钳进 1..50（API 上界） |
| 响应条目 | 标题与链接都为空的条目丢弃；`media` 为空时来源回退为固定标签；发布日期追加进摘要 |
| 响应体积 | **不做**读取大小限制：输出限额工具会把超大返回转储成文件，条数与 30s 超时已约束体积 |

配置面识别 `endpoint`、`api_key_env`、`search_engine`、`count` 四个键，未给的一律取默认。

## 十一、历史机制对照

| 原版 | 当期状态 |
|---|---|
| 召回 `recall`/`recall_*`/`memory_turn`、知识 `skill_*`/`web_search`、任务 `*_task`、`spec`、`memoryx`、PlanAgent、govx | 包已不在 `tool/`；召回与知识能力归 `memory/{query,search}` 与 `runtime/agent`，任务与批准归 `runtime/task`，治理归 `evolution/governance`；判据保留在本篇与 [docs/api/](../../api/index.md) |
| `AgentToolWrapper` + `event_keys` + `RuntimeState["external_context"]` 上下文传递 | 由 `tool/subagent` 的 `Assembly` + handoff envelope 承接；"只序列化 EventKey/EventType/EventSummary、按需让远端自查原文"的取舍保留 |
| `EventKeys` 为 `[evt_...]` 前缀、hex 契约、`toInt64Key` 以 hex 优先 | 归 `event` 的编号与时间线前缀契约（见[事件契约](../event/event-architecture.md#timeline-prefix)）；EventKey 为 Snowflake int64（PartitionID/Timestamp/Sequence），字符串面统一 16 进制；`PartitionID = FNV-1a(AgentName) & 0x3FF`，**Memory 不感知 agent**，只按分区隔离 |
| `ActionTool` 直接持 executor/monitor，经 `TaskSpawner` spawn | 由 `tool/exec` 的 `Manager`（资源/租约/清扫）+ `runtime/task`（settle-or-detach）承接；`action: tmux not available (install: brew install tmux)` 与"同步降级直跑已移除"的历史结论保留 |
| 工具注册表 `RegisterBuiltinTools` + YAML `ToolRef` | 由组合根的显式装配与 `tool/subagent` 的声明/预设承接；三阶段分层的判据（实现/注册/配置各司其职）保留 |

## 十二、关键设计决策速记（原版结论，仍然成立）

| 决策 | 理由 |
|---|---|
| 需要"思考-行动-观察"循环 → TagentAgent + 包装；单一功能/执行器 → `CallableTool` | 决策需求决定形态，不给执行器塞 LLM |
| 注册 ID `exec`、Declaration Name `action` | 注册表 ID 标识"这是执行器"（YAML 用 `id: exec`），模型看到的工具名表达"执行行为动作" |
| monitor 用 callback 而非 channel | 让 agent 完全控制"何时触发新迭代"，无需额外 goroutine 消费 |
| 上下文用 `RuntimeState` 而非 struct 字段 | 本地直接读，远程自动映射到 A2A metadata，零额外代码 |
| wrapper 持 `agent.Agent` 接口而非 `*TagentAgent` | 本地与远程共用一条调用路径 `agent.Run(ctx, inv)`，消除分支 |
| 工厂必须交回声明，空声明直接报错终止构建 | 静默回落到 config-driven 会去服务另一个 agent，比构建失败更难发现 |
| 内置名（knowledge/recall/action 等）始终走 config-driven | 否则 YAML 声明的 `Tools` 会被注册表**静默改写** |

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| **action 成功空输出无明确文案** | exit 0 且无输出时返回内容不明确，模型可能误判失败而重发 | 返回"命令成功，无输出"显式文案 |
| **长文档任务迭代预算** | 写作型任务轮次消耗大，撞上限时无收尾机会 | 收尾轮机制／写作型任务独立预算 |
| **无 spawner 的独立使用形态** | 同步等待首个 settle，无 resume/看板；缺依赖仍是启动期硬错误，不再补降级 | 明确该形态的支持边界，或要求必须接任务层 |
| **websearch 可靠性** | duckduckgo 无鉴权接口，限流/结构变化敏感 | 多 provider 回退链 |
| **路径沙箱只覆盖文件工具族** | file 工具 `base_dir` 是真沙箱（上游 ToolSet 拒绝 `../` 与绝对路径）；`spec` 以 op 白名单 + argv 直调闭合"仅限 openspec/"；通用 shell 仍全权 | 通用 exec 的可选 allowlist 包装 |
