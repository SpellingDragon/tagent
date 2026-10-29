> **活契约页**：逐条对齐当前码面（`tool/{exec,file,knowledge,mcp,recall,spec,subagent}` 与 `api/plugin` 的端口声明），代码内 `// 契约:` 注释指向本页锚点。口径统一于[存储概念地图](../../demos/storage-concept-map.html)：**链**管身份与因果 · **原文仓**管字节 · **账本**管责任 · **其余一切现算**——工具面只声明"模型能调什么"，调用的身份、结算与证据都落在链与账本上。

# 工具面：声明即装配，证据即回答

`tool/` 的产品包是 `exec`、`file`、`knowledge`、`mcp`、`recall`、`spec`、`subagent`（逐包 API 见 [docs/api/](../../api/index.md)）。本页讲三件共通的硬事：**工具面由声明决定**（没有工厂注册表）、**执行只报告观测到的**（不是报告被请求的）、**声明是模型侧前缀的一部分**（所以它不许随部署漂移）。

<a id="tool-registry"></a>
## 一、工具面地图：声明名即装配入口

一个 agent 的工具面由它的 `spec.Tools []config.ToolRef` 逐条点名，`assembly` 按名分派，**没有第二张工厂注册表**：

| 声明名 | 解析为 | 面 |
|---|---|---|
| `exec` | `exec.NewPreset`（`exec_run` + `exec_release`） | 执行面，见第二节 |
| `mcp_call` / `mcp_discover` | 网关工具的两个**常量**声明 | MCP 面，见第四节 |
| `call_subagent` | `subagent.ToolName`，需委派面已接线 | 委派面，见第七节 |
| `knowledge` / `recall` | `subagent.ResolvePresets` 的预设引用 | 知识／召回面，见第五节 |
| 某个 agent 名 | 目标 agent 的工具 | 递归装配，链上成环即具名拒绝 |

**`tool/file` 是名字冻结面**：七个名字 `save_file`／`read_file`／`read_multiple_files`／`list_file`／`search_file`／`search_content`／`replace_content` 一并导出并由 `skeleton_assert.go` 编译期钉住——名字是模型侧的调用词汇，改一个名就是一次提示词与声明的同步变更，不能当成内部重命名。

**`tool/spec` 是规约面而非执行面**：七个操作闭集（`init`／`new`／`status`／`validate`／`archive`／`instructions`／`list`），闭集外的 op 以 `ErrUnknownOp` **连允许集一起**拒绝；每个参数落成 argv 的**一个离散条目**（含 `p; rm -rf /` 这种值），从不拼 shell 字符串。

**六端口是插件面的词汇，不是工具面的**：`api/plugin` 只声明 `call->call`／`call->ruling`／`request->environment`／`fact->observation`／`fact->view`／`fact->compression` 六个端口常量，`validatePort` 是闭表，一个 instance 按它声明的端口分派；**`Material` 这个词已从码面消失**（全仓无 `type Material`），材料集合不再被当作可遍历的集合。工具面沿用同一条纪律：能漂移的东西（端口名、注册表内容）不进声明。

**新工具的验收锚**：`tests/integration/execution/new_tool_adapter_test.go` 的 `TestNewToolAdapterNeedsNoHostCompensation`——一个 `CallableTool` 放进 `NativeAgent.Tools` 就走完受理→执行→结算→交付（tool result 恰为 `ok`、结算正文以用户角色回填、一个 notice 且已消费）；重启按记录结果的那条事实重放而不问适配器；适配器失败走同一套面、不产生第二条通知；进度与停止证据在端口与任务面之间传递；**晚到的结算不补偿任何东西**；状态词表仍是核声明的闭集。

<a id="action-tool"></a>
## 二、执行面：资源、租约与停止证据（`tool/exec`）

**声明是纯值**（`ExecutionDecl{Command, Args, Dir, Resource}`）：`Dir` 为空是"调用方没声明目录"，不是"不适用目录"；句柄、连接、锁、租约都不属于声明，所以重启前记下的声明能在重启后重放出同一次执行。`ResourceKey{Kind, Key}` 一起命名资源，同键不二次打开；`KindTmux` 是唯一资源族，**tmux 是硬依赖**——`IsTmuxAvailable()` 每次真实查 PATH，缺了以 `ErrTmuxUnavailable` 拒绝，不悄悄换语义。

`Executor` 四方法四红线：`Start`（返回资源键）／`Probe`（`ProbeState` 三值，**零值是 `ProbeUnknown`**：判不了只升级诊断、绝不判死）／`Capture`（运行期观测到的帧）／`Stop`（`StopEvidence{Resource, Confirmed, ExitCode, Reason}`）。红线：必须持有自己持有的每个资源的租约；**必须报告探测或停止观测到什么，而不是请求了什么**；停止未确认的资源不得释放；未确认的退出不得报成干净退出。

`Manager` 在端口之上管租约与证据：`Acquire` 先探可用性再开资源；`Renew` 按本次的监控节奏续租——**长执行不会因为花得久被清扫**；`Sweep` 到期只驱动停止、**到期本身不释放任何东西**（`SweepReport.Evidence` 带未确认者继续持有）；`Observe` 把判不了的探测累计成连续 unknown 段，达阈才升级诊断；`Release` 返回 `ReleaseEvidence{Resource, Released, Stop, Reason}`，`Released` 只在停止已确认时为真。排障读 `Explain` 一处，不靠猜。

工具面：`PresetName = "exec"`，`exec_run` 按参数声明的键获取（同键复用）、回答执行记录到的内容，`exec_release` 显式停止并按证据回答（未确认就**报告保留**）。两个工具的 schema 是显式键集，**传入集合外的键被接受但不消费**（含已退役的工具级 compress 段）：它的存在不改变行为，也不构成拒绝理由。`kind` 只接受空值与 `KindTmux`，`resource` 是 tmux 会话名的字符集与长度模式（示例 `wx-fetch`），不是路径、也不是 `<kind>:<key>` 键。

这是新工具"零补偿"的由来：**执行、取消、恢复三种证据各自都有落点**（进度事实、`StopEvidence`、重取时的存活帧），所以后注册的工具不需要宿主为它补一层适配。

<a id="tmux-monitor"></a>
## 三、监控节奏与静默判定

`MonitorConfig` 是"轮询间隔 + 静默阈值"四值：`Interval`／`StableDuration`／`InteractiveStableDuration`（TUI 窗）／`FakeDeadDuration`。默认 30s／60s／90s／120s，零值按字段折默认；`MonitorConfigFrom` 读配置面时**未知键忽略**（含退役的 compress 段），已知键写坏则**具名报错并保留默认**，不静默替换。

`MonitorStatus` 三值：`watch`（帧还没静够稳定窗）／`stable`（帧静够稳定窗；被停止的执行其最终帧也是 stable）／`suspect`（**活着但静够假死窗**）。`Poll` 的机械规则三条：首轮把当前帧**锚定**为静默起点；帧与上轮不等即**重置**静默；`classify` **只在 `ProbeAlive` 时**才可能给出 `suspect`——停了永远不会被读成假死。

三态探针与 quiet-vs-dead 的逐条判据见 [tmux-action.md](tmux-action.md#quiet-vs-dead)（与本节的 `#probe-three-states`）：那些判据的失效表现不是报错，而是误杀健康任务或把静默读成结束。

<a id="mcp-live-registry"></a>
## 四、MCP 面（上）：发现读的是活注册表

四个接口，红线各成清单：`Registry`（并发安全／名单升序／**增删连接不改任何 agent 的已声明工具集**／注册结束的集合不再交出，4 条）、`Connector`（从连接声明建一个原生工具集／交出去之前会话必须起来／**绝不交出会话未起的集合**，3 条）、`Discovery`（**每个工具都带输入 schema**／**重新列举而不是复用旧快照**／只列所给注册表可达的工具，3 条）、`DeclarationReloader`（按交给请求的顺序返回／**绝不为网关不服务的工具作答**，2 条）。

`Binder` 是声明面到活表的桥：一代声明 `DeclaredServer{Name, Transport, URL, APIKeyEnv}` 绑到活注册表，reload 只搬**声明变了的那些**服务；同名声明两次、缺 url、缺 key 变量都具名拒绝（`ErrServerDeclaredTwice`／`ErrServerURLMissing`／`ErrServerKeyMissing`），**声明了服务却没有活注册表也拒绝**（`ErrRegistryMissing`），不开口匿名连接。

发现结果必须**如实给出调用方式**（`mcp_call(server=…, tool=…)` 加输入 schema），不得给 exec 命令式的假调用路径；匹配除子串包含外有 **token-AND 回退**（按空格/下划线/连字符切词、逐词命中），`web search` 能匹配 `web_search_prime`。一个服务工具集为空**不阻塞**其他服务的发现结果。

<a id="mcp-gateway-injection"></a>
## 四、MCP 面（下）：网关声明恒定，调用期读表

`mcp_call` 与 `mcp_discover` 的名字与 `MCPCallInputSchemaJSON` 都是**常量**：声明是"注册表里有什么"的函数时，模型侧前缀会随部署抖动，同一工具在两次会话里变成两种签名。于是注册只移动连接，**没有任何方法改变 agent 已声明的工具集**——运行期新注册的服务立刻可用，移除的立刻消失。

调用侧的失败一律**以结果形态返回，不是 Go error**，并带足以自纠的清单：一个服务都没注册 → 显式空态 + 两条来源（配置文件声明／运行期注册）；server 名不认识 → 具名报错 **+ 可用服务清单**；tool 不在该 server 上 → 具名报错 **+ 该 server 的工具名清单**；内层调用失败 → 具名报错 **+ 输入 schema**，让模型改参数而不是重试同一个名字。只有清单可列时才列，空清单不伪装成"可用服务为空"的成功。

**typed nil 坑**：类型化空指针赋给接口字段后接口并不等于 nil，"有注册表就启用"会假判成立、调用期才炸。装配侧在赋值之前判空（`normalizeRegistry` 是这条守卫的落点）——宁可不注入，也不塞一个带类型的空指针。

<a id="recall-agent"></a>
## 五、召回面（上）：模型词汇与声明是同一份

召回是一个**召回 agent** 的四把工具：`recall_query`／`recall_get`／`recall_recent`／`recall_trace`。这组名字是提示词与声明共用的词汇，代码里视为**冻结**——改一个名等于同时改提示词与声明。

`TestRecallPromptNamesMatchTheToolSurface` 钉住这件事的两侧：四个名字必须同时出现在 `examples/wechat-bot/resources/prompts/recall_agent.md` 与部署文档的 `id:` 行里，顺序与 `recall.New` 的声明顺序一致；`memory_query`／`memory_get`／`memory_recent`／`memory_trace` 这些旧名一旦重新出现在提示词里即失败。**模型照着一个名字调，必须落到回答它的那把工具上。**

<a id="recall-contract"></a>
## 五、召回面（中）：读的授权在构造期，不在参数里

`Reads{Searcher, Embedder, Table, Originals, Trace}` 是四把工具消费的**读面**，本包不持存储、不留事件副本。任一字段为 nil 就是"这次部署没接这面"，需要它的工具**具名拒绝**（`ErrRecallUnwired`），绝不拿空结果冒充。

授权是**构造期固定的分区集合**：`New(reads, partitions)` 收下调用方授予的分区名，`admits(partitionID)` 逐条比对，**空集不放行任何事件**——任何一次调用的参数都无法把可读范围撑大。响应里读不到正文只有三个具名原因：`missing`（读面没有这个事件）／`expired`（原文已出保留窗）／`out_of_scope`（事件不在授予的分区内）。**"没有匹配"与"读不了"必须可区分**——这是本节唯一的交易。

<a id="recall-subtools"></a>
## 五、召回面（下）：四把工具各自的有界读

| 工具 | 读什么 | 界 |
|---|---|---|
| `recall_query` | 在授予分区内做一次检索（有 embedder 才走语义，没有就具名拒绝） | 页上限 `maxPage=100`，缺省取 `search.DefaultLimit` |
| `recall_get` | 按 key 精确读一条事件的**原文分段** | 单次 `MaxBytes` 缺省 8 KiB、上限 32 KiB；`Offset`→`Next`→`EOF` 逐段推进 |
| `recall_recent` | 最近一页事件引用 | 同 `maxPage` |
| `recall_trace` | 沿父链做一次因果回走 | 步数缺省 20、上限 50；首 key 不在读面即 `ErrRecallMissing` |

**原文来自原文仓，且按字节切**：`recall_get` 走 `Originals.ReadSegmentScoped`（经 `ledger.ScopedReader` 的 `ReadSegment`／`Get` 形态），请求带字节位置与字节上界，分段在**字节边界**切开、拼回去等于原始字节；读之前先取该事件的读范围（`Reference.ReadScope()`），**持有 key 不等于获得读授权**——范围外的引用不参与可见性判定，key 在范围外就读成 missing，一点字节都不漏。

<a id="declaration-stability"></a>
## 六、声明稳定性：召回声明与向量能力隔离

四把工具对模型呈现的 `Declaration` 里**没有任何向量或嵌入参数**：两次独立构造的声明**逐字节一致**，与"这份部署有没有开向量"无关；声明文本也不得出现 `embedding`／向量存储／索引结构／融合算法一类实现字样。

**是否做语义检索在运行期判定**：判据是 accessor 暴露的引擎能力位 `MemoryEngine().Capabilities().Vector`，存储侧另有一个 `SupportsVectorSearch()` 探测面而**召回路径不看它**——把"能力探测"错写成召回分支的前提，声明就会随部署漂移，前缀缓存整段失效。

`tool/recall/skeleton_assert.go` 把这层约定做成**编译期断言**：四把工具都是 `CallableTool`（所以每次调用都先过任务包装）、`New` 的签名与三个具名错误不变、`Table` 与 `ledger.ScopedOriginalStore` 由生产组件（`memory.StoreQuerySource`、`ledger.MemoryOriginalStore`）满足而不是测试替身。

## 七、其余工具面速记

- **`tool/knowledge`**：三名 `skill_search`（短名单：名字＋描述）／`skill_load`（载入正文，并把正文发布到下一次请求的注入面）／`read_file`——后者由 `tool/file` 的工具集**只开读**构建（保存、替换、列举与两种搜索全关）；拿不到读工具即 `ErrFileToolMissing` 拒绝，**知识 agent 只读不写**。
- **`tool/subagent`**：`call_subagent` 把一次子 agent 调用**作为任务受理**并回该受理的回执。`Delegation` 两红线：必须回受理回执、并把终结作为**同一任务**的 notice 报告；绝不为委派轮造私有可写历史，也绝不用 notice 身份替换它指向的事实。`Complete` 交回 `task.Notice`，`ResultDelegation`（可选扩展）把结算正文带进通知——**这正是 settle 通知回传给发起会话的落点**。
- **受理回执单源**：模型可见的整份回执就是 `task.AdmissionReceiptText = "ok"`，经 `AdmissionReceipt` 的 `Text()`／`String()`／`MarshalJSON()` 三个渲染口输出同一份最小正文；task id（`task-<32 hex>` 随机铸造）、状态词、结算承诺**永不进模型**。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 召回语义检索依赖部署接线 | `Embedder` 为 nil 即无检索，具名拒绝而非空答 | 把"能力位 → 检索形态"的映射写进一处诊断面 |
| 执行族只有 tmux 一族 | `KindTmux` 是唯一 `Kind`，其余具名拒绝 | 新族须先证明停止证据可观测，再进闭集 |
| MCP 声明与活表是两代 | `Binder` 按声明差异搬连接，声明本身恒定 | 声明 reload 的可观测 diff 面 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（链／原文仓／账本／现算的分工）· [存储时光机](../../demos/storage-time-travel.html)
