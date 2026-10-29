# 平台子系统（组合根 · 配置面 · 治理闸 · 严格解码 · 投递账本）

`assembly` 是组合根：一份纯声明在这里变成活的运行时，进程内没有第二个装配入口。本篇的几个子系统不是并列的功能模块，而是**同一条装配链上的不同关卡**：配置面决定能声明什么，严格解码决定声明写错时怎么失败，治理闸决定一次调用放不放行，投递账本决定结果出去没有，观测/记忆/MCP 是链末的旁路与后端。共同纪律：能力缺席时按名拒绝或具名 gap，绝不给出"零读数"冒充空闲。

<a id="composition-root"></a>
## 一、组合根（`assembly/`）

模块根已经没有 Go 源：`golden/runtime/architecture.json` 的 `root_package` 是 `assembly`，23 个文件整体在 `assembly/` 下，**目录名即包名**；包边界与 `// 契约:` 索引都以它为准。

三个生命周期入口：`Boot(configPath, opts...)` 读一份宿主文档并带回 `*Host`；`Invoke(ctx, req)` 驱动一次回合；`Close(ctx)` 拆掉整个宿主。`Host` 拥有装配好的 run、结算驱动、反思调度与回环端点；出站消息面、审批面、投递面由调用方经 Option 注入，宿主不自己发明这些面。

host 簇按域分文件：

| 文件 | 域 |
|---|---|
| `host.go` | Boot / 驱动 / settle / 生命周期与关闭次序 |
| `host_document.go` | 方言解码与装配（文档 → 共享声明 → 运行部件） |
| `host_delivery.go` | 交付账本（义务登记、挂起、重发、销账） |
| `host_tools.go` | send 工具、反思计划、注入端口 |
| `host_face.go` | HTTP face：健康探针、任务端点、`/reload` |
| `host_diagnostics.go` | 诊断面：统一运行读数 + 反思 / 训练 / 观测三面 |
| `host_model.go` | 采样参数（temperature / thinking / reasoning_effort）的请求包装 |
| `host_capture.go` | 轨迹采集：一次账户覆盖整次运行 |
| `host_reload.go` | 热更新三档判据（见[组织级热重载](./org-hot-reload.md)） |

装配链本体在其余文件：`run.go`（唯一公开装配入口与发布）、`prepare.go`（候选部件构造与资源登记）、`request.go`（请求装配与回放窗）、`invoke.go`（回合与关闭）、`tools.go`（工具受理闸与裁决适配）、`facts.go` / `commit.go` / `read.go`（事实与投影）、`settle_turn.go`（事件驱动的结算回合）。

候选构造顺序固定：model → tools → prompt → plugins，外层再套工具受理 wrap。每个部件在**获取那一刻**把释放函数登记进候选账目，因此失败按获取逆序回收；共享面与在用资源不在任何账目里。

<a id="config-surface"></a>
## 二、配置面（`api/config` · `api/document`）

`Declaration` 是共享的声明词汇：`Entry` + `Agents []AgentSpec` + `Refs []ToolRef` + `Baseline` + `Servers map[string]MCPServerSpec`。`AgentSpec` 以 agent 为中心：自己声明的 `Model`、`Tools`、`Plugins`、`TaskPolicy`、`Files`、`Params`，以及四个域段 `Memory` / `Meditation` / `File` / `Exec`。

工具私有参数走 `ToolRef{Name, Params map[string]string, Endpoint}`：共享结构不被工具私有字段污染，远程引用用 `Endpoint` 表达，本地可解析的引用留空。提示词与 skill 是 `FileRef{Path, Kind}`——**正文不在声明里**，只声明"哪个文件是提示词/skill"，正文在请求边界按文件读（所以改正文不需要重新发布，见[热更新三档](./org-hot-reload.md#restart-required-dimensions)）。

`api/document.Document` 是运行时视图那一层：`Entry` + `Agents` + `Servers`，`Order()` 给稳定顺序（entry 优先），`Declaration()` 派生共享声明。宿主方言（`assembly/host_document.go`）是它在宿主侧的读法：核心共享段逐字节按 `api/document` 读，`app` 段保持不透明、原样携带。

声明面在三处被读，**读的时刻决定热更档位**：Boot（构造消费点，改了须重启）、发布（换代重建绑定）、请求边界（提示词与 skill 正文、回放窗——下一回合就生效）。

<a id="governance-gate"></a>
## 三、治理闸（`features/evolution/governance`）

治理不是"包在工具外面的装饰器"，而是插件材料流 `call->ruling` 端口上的一个实例：`Policy` 报 `Port() = plugin.PortCallRuling`，实现 `Governor.Judge(pass)`。

一次调用过闸的完整链：

| 步 | 发生处 | 产物 |
|---|---|---|
| 变换 | `Pipeline.Before` 的 transform 在私有副本上改正 call | 最终 call |
| 打分 | 同一趟的 risk face 出 grade | 最强 grade（只读一次，不重算） |
| 传导 | 派发层把这一趟压进 context（`plugin.WithCallPass`） | 一次调用一次 pass |
| 裁决 | 受理 wrap 的适配读回 pass → `Governor.Judge` | `Decision` |
| 分类 | 适配器把裁决翻成任务受理结论 | `RulingDeny` / `RulingHold` / `RulingRun` |

三态语义：

- **拒**是终局：调用不执行，模型读到与调用配对的**工具结果**（带规则与理由），而不是 Go error——被拒的理由必须出现在模型读得到的地方，否则它既看不见为什么失败，也没有自纠入口。
- **挂起**（`hold`）是等待不是拒绝：调用交给审批面，宿主回 `HoldReceipt`，模型读到"parked awaiting approval"；批准或拒绝之后走同一条受理路径。
- **放行**（`allow` / `record`）：`record` 额外要求这次执行进审计台账。

两条 fail-closed：context 上没有 pass 时拒绝裁决（不猜），`strict` 级别且没有审批面时拒绝一切调用（不默认放行）。分级判据取**工具声明名**、不取注册别名：风险规则按工具声明名做最长前缀匹配，未命中落默认档——注册时的装配别名根本不进分级面。

<a id="strict-decode"></a>
## 四、严格解码（`api/config`）

一个策略、两个入口：`decodeDeclaration` 守护**声明词汇**自己；`DecodeStrict(source, data, v)` 把同一套拒绝词汇给**调用方自有 schema**（宿主文档这种带连接注册表、治理段、应用段的方言）用——它按 `v` 的 yaml 标签递归走完整个结构，宿主不再自己写第二套严格度。

| 输入 | 行为 | 为什么 |
|---|---|---|
| schema 外的键 | 失败，报**源 / 行 / 字段路径**（`ErrUnknownConfigKey`） | 拼错的键若被忽略，用户会以为改动了行为 |
| 同一个键写两次 | 失败 | 后写的赢等于静默丢掉前者 |
| YAML 首个文档之后的尾随文档 | 失败 | 静默忽略第二份文档会让用户以为其中配置已生效 |
| JSON 首个值之后的尾随内容 | 失败 | 拼接两份配置不可能是用户意图 |
| 完全空的文档 | 成功，解出零值 | "没写"与"写错"必须区分 |
| `.yaml` / `.yml` / `.json` 之外的扩展名 | 按名拒绝 | 猜格式等于让一份 YAML 被静默解成零值 |

同族的封闭性纪律：工具操作名是闭集（`tool/spec` 的 `Op`），集合外的操作在任何后端被触达之前按名拒绝——已退役的旧操作名重新出现时，宁可报错也不静默接管。

## 五、投递与交付账本

一次模型产出的正文，在离开宿主之前先成为**持久的交付义务**（`assembly/host_delivery.go`）：

| 记账态 | 含义 |
|---|---|
| `owed` | 已生成、尚未交出；先登记后发送，崩溃窗口才不漏送 |
| `held` | 审查挂起：渠道什么都没有，批准后只交一次 |
| `retry` | 发送被渠道拒：义务仍在，可重发 |
| `sent` | 真交出 |
| `refused` | 审查拒绝并销账（`[send_denied]` 结果文本） |
| `headless` | 没有入口地址（调用方驱动）：具名销账，不假装投递成功 |

进程内投递走另一条缝：`state.SendFace` 只对白名单内的目标受理，目标不在运行就以 `ErrDeliveryTargetNotRunning` 具名拒，回执照**受理序**登记进该站点的 mailbox。两类记录的分工是"谁把话送出去"与"这条话有没有被交出去"，术语口径见[存储概念图](../../demos/storage-concept-map.html)的交付账本与席位两卡。

## 六、观测、反思、记忆、MCP（旁路与后端）

- **观测**（`features/telemetry`）：commit observer / log observer / OTEL 三面，全部在事实提交之后读，是只读旁路。
- **反思与演进**（`features/evolution`）：激活判据、评估门（gate / judge / posterior）、冥想调度、治理策略面；只在开关打开时装配，默认关＝零行为变化。
- **记忆**（`memory`）：分区、账本、重放、查询与检索、嵌入器；查询面是只读投影，事实链是唯一来源（链管身份与因果、原文仓管字节、账本管责任）。
- **MCP**（`tool/mcp`）：`mcp_servers` 声明 → 每代发布重绑（停答即停、新增即答、没变保持连接）→ `mcp_call` 网关；声明变化进指纹，因此必换代、必重绑。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 治理只覆盖原生工具受理面 | 闸在受理 wrap 上，凡经该 wrap 的调用都过闸；绕过 wrap 直接调用的路径不受保护（当前没有这样的生产路径，但也没有结构约束阻止新增） | 把"注册即过闸"变成装配期不变量（骨架断言） |
| 交付账本与进程内投递是两条缝 | 账本管"结果的持久义务"，`SendFace` 管"进程内站点受理"，两者各自的边界清晰但没有一张统一的读数 | 诊断面把两侧按同一地址合读 |
| 诊断面的三面开关各自独立 | 关闭的面以理由作答而不是零读数；但没有"某面关闭导致另一面读数失真"的联动声明 | 在统一运行读数里带上各面开关状态 |
| 双严格入口靠纪律对齐 | `decodeDeclaration` 与 `DecodeStrict` 共享拒绝词汇（同包、同错误族），但两者的字段遍历是两段代码 | 由一份 schema 走查同时服务两入口 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（12 个存储概念与 L1/L2/L3 受众分层）· [存储时光机](../../demos/storage-time-travel.html)
