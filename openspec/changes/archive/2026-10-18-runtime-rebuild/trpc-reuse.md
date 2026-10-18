# tRPC 复用导航（trpc-reuse.md）

> 角色：登记运行时所**复用**的框架能力与其**实读证据**，避免各域重复描述或另造框架。**不是**新依赖清单、**不是**选型提案、**不是**编译通过声明、**不**引入 web 选型。
> 证据口径（三层分明，不互推）：
> - **本仓 `go.mod` 已核**（只读当前锁定）——依赖为 direct/indirect 以 `go.mod` 现状为准。
> - **锁定缓存已核**——只指在 `go env GOMODCACHE` 下的**锁定版本**读到符号/签名者；行号取自实读。
> - **工作区候选**——工作区源码仓库（与锁定缓存非同一份）里存在，只标候选，**不称锁定缓存已核**。
> 「读源码已核」≠「编译已过」≠「已被生产调用」；未经 G1 探针**不称冻结**。

## 0. 锁定来源（本仓 `go.mod` 实读）

- core：`trpc.group/trpc-go/trpc-agent-go v1.11.2`（`go.mod:10`）→ `replace … => github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`（`go.mod:109`）。锁定缓存 `…/github.com/!spelling!dragon/trpc-agent-go@v1.11.2-tagent.1`。
- provider：`trpc.group/trpc-go/trpc-agent-go/model/provider v1.10.0`（`go.mod:11`，**独立 module**；缓存 `…/trpc-agent-go/model/provider@v1.10.0`）。
- 旁路依赖（`go.mod` 实读，**按现状声明，不承诺将来维持**）：
  - `trpc.group/trpc-go/trpc-a2a-go v0.2.6-0.20260721084546-18c8244d0acb`（`go.mod:9`，**direct**）。
  - `trpc.group/trpc-go/trpc-mcp-go v0.0.10`（`go.mod:29`，**indirect**）。
- OTel：`go.opentelemetry.io/otel v1.38.0`（`go.mod:7`）、`/metric v1.38.0`（`go.mod:81`）、`/trace v1.38.0`（`go.mod:83`）——**direct**；`/sdk` 与 exporters 为 indirect。
- Go `1.24.1`。
- **独立 `evaluation` module（工作区候选）**：工作区仓库 `/Users/pengweiye/Documents/codes/trpc-agent-go` 存在 `evaluation/go.mod`，module 名 `trpc.group/trpc-go/trpc-agent-go/evaluation`。锁定缓存 fork 内**无** `evaluation/` 目录 → 只标 **工作区候选**，**不是本仓已依赖**，**不称锁定缓存已核**，**不自动引入**。

> 复跑复核：`grep -n '<symbol>' <缓存路径>/<file>`，缓存路径以 `go env GOMODCACHE` 为准。下表位置为在**锁定缓存版本**上实读所得；「生产接线任务」引用 `changes/*/tasks.md` 叶编号；「验证」为**未来**点名用例，**本轮不跑**。

## 1. 复用总览（能力 → 生产接线 → 状态）

| # | 能力 | 锁定 module | 生产接线任务 | 状态 |
|---|---|---|---|---|
| 1 | Model / IterModel / provider | core fork + provider | 02.4/02.5/06.2/06.8 | 锁定缓存已核 |
| 2 | Tool / CallableTool / ToolSet / FunctionTool | core | 02.3/04.1/04.3/04.4 | 锁定缓存已核 |
| 3 | callbacks / plugin.Manager | core | 01.4/02.6/04.5/05.3 | 锁定缓存已核 |
| 4 | file / skill | core | 04.1/01.6 | 锁定缓存已核 |
| 5 | MCP / A2A | core + 外部 module | 04.3/04.4 | 锁定缓存已核（真实服务欠账） |
| 6 | TokenCounter（**预算计数**）/ Embedder | core | **03.4**（预算/usage）/ **04.7**（授权检索） | 锁定缓存已核（真实 embedder 欠账） |
| 7 | OTel（telemetry） | core + otel direct | 05.3/07.1/07.3 | 锁定缓存已核（collector 欠账） |
| 8 | 评估面（core `evolution/`；独立 `evaluation` module 为工作区候选） | core；`evaluation` 工作区候选 | 07.3/07.4 | core 面锁定缓存已核；module 仅工作区候选 |

## 2. 实读确证签名（锁定缓存 `…/trpc-agent-go@v1.11.2-tagent.1`；行号实读）

- **Model / IterModel**（`model/model.go`）：
  - `:45` `type Model interface`
  - `:53` `GenerateContent(ctx context.Context, request *Request) (<-chan *Response, error)`
  - `:60` `type Seq[T any] func(yield func(T) bool)`
  - `:66` `type IterModel interface`；`:68` `GenerateContentIter(ctx context.Context, request *Request) (Seq[*Response], error)`
  - fork 相对上游 v1.11.2 含 **producer-done 流关闭修复**（`go.mod` R1 注释：4 个 stream-close-contract commit，钉在 fork tag；上游 PR #2637）。tagent 不引 LLMAgent 第二工具循环，**单一** channel/iterator 终局归约。
- **原生工具调用**（`tool/tool.go`）：`:23` `type CallableTool interface`；`:26` `Call(ctx context.Context, jsonArgs []byte) (any, error)`。CallID **非全局唯一**，去重作用域须含所属实例/调用/决策（02.1）。
- **FunctionTool**（`tool/function/function_tool.go`）：`:166` `func NewFunctionTool[I, O any](fn func(context.Context, I) (O, error), opts ...Option) *FunctionTool[I, O]`。
- **file**（`tool/file/file.go`）：`:211` `func NewToolSet(opts ...Option) (tool.ToolSet, error)`（含 `readfile`/`savefile`/`searchcontent`/`listfile`/`workspace_cache.go`）。
- **skill**（`skill/repository.go`）：`:112` `func NewFSRepository(roots ...string) (*FSRepository, error)`；`:134` `func (r *FSRepository) Refresh() error`。
- **MCP ToolSet**（`tool/mcp/toolset.go`）：`:68` `func NewMCPToolSet(config ConnectionConfig, opts ...ToolSetOption) *ToolSet`；`:102` `func (ts *ToolSet) Init(ctx context.Context) error`；`:110` `func (ts *ToolSet) Tools(ctx context.Context) []tool.Tool`；`:130` `func (ts *ToolSet) Close() error`。
- **预算计数**（`model/token_tailor.go`）：`:71` `type TokenCounter interface`（`CountTokens` / `CountTokensRange`）；`:108` `func NewSimpleTokenCounter(opts ...SimpleTokenCounterOption) *SimpleTokenCounter`。实读注释 `:101` 明示 “a **very rough** token estimation based on **rune length**”——**这是 rune 长度经验近似，不是真实 tokenizer**；旧字节经验公式**≠** token 计数，二者不可互称。
- **Embedder**（`knowledge/embedder/embedder.go`）：`:44` `type Embedder interface`。
- **评估面**（`evolution/reviewer.go`）：`:24` `type Reviewer interface`；`:99` `type LLMReviewer struct`。
- **callbacks**（`model/callbacks.go`）：`:101` `type Callbacks struct`；`:132` `func NewCallbacks(opts ...CallbacksOption) *Callbacks`。
- **plugin**（`plugin/manager.go`）：`:33` `type Plugin interface`；`:215` `type Manager struct`；`:241` `func NewManager(plugins ...Plugin) (*Manager, error)`。
- **telemetry 面**（核心目录实读存在）：`telemetry/{appid,errs,langfuse,metric,semconv,trace,tracetransform}/`。

## 3. 分域接线与验证（未来点名用例，本轮不跑）

- **01.4** `AgentBinding.Plugins` 有序链 / **02.6** 执行前恰一次、提交后呈现：消费 `plugin.Manager` 与 `model/callbacks`；相位枚举/元素类型待 01.4/02.6 对齐后冻结（G1）。
- **02.3** 原生 wrapper、一次受理 Receipt、后台结果独立回流：消费 `CallableTool`；Receipt 非框架原生导出，属 tagent 面，其契约见 02 cap delta 与 00.3。
- **02.4/02.5** 模型单一终局 + 装配驱动 / **06.2** 请求边界快照包装 / **06.8** 模型替换租约：消费 `Model`/`IterModel`/provider。
- **03.4** 模型预算/usage：`TokenCounter`（`CountTokens(Range)`）为**预算计数**接点；**不在此决定**外置阈值算法。
- **03.8** 外置路径：归 `workspace`（不预建根 `tool` 包）；大结果副本走 workspace 唯一副本生命周期。
- **04.1** 原生 file/skill/FunctionTool 登记入 `Request.Tools`（不预建 `skill` 包，复用 `FSRepository`/`CallableTool`）。
- **04.3** MCP 连接/发现/刷新失败/关闭（`NewMCPToolSet`/`Init`/`Tools`/`Close`）/ **04.4** A2A 与内部 send 显式寻址。
- **04.7** 授权检索/融合 + **`Embedder`**；embedding 未配置时零行为变化。
- **05.3** 提交后插件接 OTel/审计（观测**旁路、不拥有状态**；关闭 OTel 仍 Query 可用）/ **07.1/07.3** 反思与评估的观测读缝。

## 4. 本轮不引入（负向清单，精简）

- **不新增/不升级依赖**：不改 `go.mod`/`go.sum`；core fork 与 provider 以**当前 `go.mod` 为准**，工作区新版**不代表锁定版可用**。
- **不做 web 选型、不下载/安装**：本档是**复用**依据，非选型提案；不因本档新增/删除包、不给 `architecture.json` 增边。
- **独立 `evaluation` module**：仅**工作区候选**，未在本仓依赖 → **不自动引入**；其可用性/版本/接口待核。
- **旁路依赖**：`trpc-a2a-go`（direct）/`trpc-mcp-go`（indirect）**按现状登记**，协议**原样复用、不重写**；**不承诺**将来继续保持 indirect（升级/转直接须另行取证并登记）。

## 待核签名（诚实列，不以数量宣称全对齐）

1. `Model`/`IterModel` 的 iterator 形态与 tg 侧「单一终局归约」的字段映射 —— 02.4 前由 G1 探针确证。
2. `CallableTool` 与 tagent 原生 wrapper 的**一次受理 Receipt**契约衔接（Receipt 属 tagent 面）—— 02.3/00.3。
3. `plugin.Manager` 与本域相位标签的**元素类型/顺序**在 `AgentBinding.Plugins` 中的承载 —— 01.4/02.6 对齐后冻结。
4. MCP `ConnectionConfig`/`ToolSetOption` 与 state 生命周期（关闭/刷新）的归属 —— 04.3。
5. 独立 `evaluation` module 是否引入、其版本与接口 —— **设计待核，不预设继承**（07.3/07.4）。
6. `TokenCounter` 的「rune 近似」是否足以承担 03.4 预算判定，或需真实 tokenizer —— 03.4 设计待核（**不由本档决定阈值算法**）。
