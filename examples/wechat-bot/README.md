# tagent WeChat Bot — 完整示例

tagent 组合根的**真实宿主示例**：常驻微信机器人，把 `tagent.Assemble` 产出的 `*tagent.Run` 接上消息链路——
入站去重、媒体与长文本落盘、按显式会址投递、审批通道与投递回执全部跑在真消息上。

> 部署（裸机 systemd / 容器）见 [deploy/README.md](deploy/README.md)；历史设计记录见 [../../docs/wiki/](../../docs/wiki/)（该目录 25 篇为 historical-verbatim，描述的是已移除的实现；当期契约以 [../../openspec/specs/](../../openspec/specs/) 与 `scripts/gen_godoc.sh` 生成的 `docs/api/` 为准）。

---

## 快速开始

```bash
./wizard.sh    # 六步向导：依赖检查 → 密钥(不回显) → .env → 运行目录 → 连通性验证 → 指引
./run.sh       # 前台启动（./run.sh --help 看全部命令）
```

需要 Go ≥ 1.24、以及一把 `ZAI_API_KEY`（GLM Coding Plan）与一把 `DEEPSEEK_API_KEY`（knowledge 子 agent 的 provider）。密钥写入 `.env`（chmod 600，被本目录白名单式 `.gitignore` 天然忽略）；`run.sh` 启动时自动加载 `.env`，已导出的环境变量优先，并在启动前逐个校验配置里被 agent 引用的密钥（判据是「被引用」而非「会被驱动」：`knowledge` 声明的 deepseek 密钥照样必填，尽管本宿主只驱动 entry）。

`resources/prompts/USER.md`（个人上下文）不随仓库分发：检出库不带它也能启动，部署侧需要时从 `resources/prompts/USER.md.example` 复制填写。

## 装配面（当前实现）

| 环节 | 码面 |
|---|---|
| 读配置 | `loadHostDocument(TAGENT_CONFIG)` → `api/config.DecodeStrict`：schema 之外的键**拒绝启动**，错误点名 `文件:行:字段路径`（`config.ErrUnknownConfigKey`）；schema 即 `host_runtime.go` 的 `hostDocument` 结构标签 |
| 装配 | `tagent.Assemble(ctx, decl, natives, opts...)` → `*tagent.Run`（恒挂 `WithGovernance`，配了审批网关再加 `WithPolicy`）；`decl` 由 `hostDocument.Declaration` 派生（entry agent、各 agent 的 `system_prompt.files`、声明了的 `send` 工具），模型按 `providers` 的 `api_endpoint` + `api_key_env` 现场构造 |
| 驱动 | `host.Drive` → 按会址加锁 → `run.Invoke(ctx, tagent.InvokeRequest{InvocationID, Address, Input})`；收尾走 `run.Close(ctx, agent.CloseRequest{})` |
| HTTP 面 | `newTaskFace(ctx, host, TAGENT_RL_AUTH_TOKEN)` → `Listen()` 绑 `127.0.0.1:<TAGENT_HTTP_PORT>`，`GET /healthz` 答存活、`POST /task` 收一条注入（202 先行、回合在后台跑），驱动面是 `faceDrive{Drive, mostRecentChat}` |

- **已接线的键**：`entry`/`provider`/`model`/`prompt_dir`/`providers`/`agents{provider,model,system_prompt.files,tools,final_round{max_rounds,budget_seconds,timeout_seconds,prompt}}`/`request_timeout_seconds`/`governance{enabled,enforcement,dir,approval_ttl_minutes}`/`app.wechat{config_dir,token_file,context_token_dir,workspace_dir,approvers}`；工具面只接 `hostToolNames` 这五项（`send` + 四个 `recall_*`）：`declaredTools` 按固定清单过滤，写别的 `id` 不进声明，`agent:` 键在 schema 里但零消费（写了不报错也不生效）；看门狗预算只读 entry agent 的 `final_round`（`run.go` 按声明为整条 run 接一次 `SetFinalRound`），不声明即无界。
- **环境变量**（码面实际读取）：`TAGENT_CONFIG`、`TAGENT_API_ENDPOINT`（覆盖 entry endpoint）、`TAGENT_SESSION_ID`（会址前缀标签，默认 `wechat-session`）、`TAGENT_WECHAT_WORKSPACE_DIR`、`TAGENT_HTTP_PORT`（HTTP 面端口，默认 8089）、`TAGENT_RL_AUTH_TOKEN`（HTTP 面凭据；空则只信回环对端）、`providers.*.api_key_env` 点名的每个变量（被 agent 引用的必需，未被引用的可选）。
- **HTTP 面**：宿主自己装配的最小面（**不经** `cli.NewCLI`，故无 `/query`、`/diagnostics`、`/feedback`）：`GET /healthz` 无凭据答 `200 ok`；`POST /task` 收 `{"messages":[{"role":"user","content":"..."}],"trigger_source":"user"}`，有 token 则要求 `Authorization: Bearer <token>`，否则仅回环对端；注入没有会话身份，投到**最近一次有投递的会话**上（无会话答 503）。mail-poller 走的就是这条通道。

## 待接线清单（写进 yaml 前须先接 schema）

严格解码的判据是「键只许出现在它被读取的地方」：下列声明先要在 `host_runtime.go` 的 `hostDocument` 接线并消费，再写入 yaml；`tagent.yaml` 以行首 `待接线` 注释登记同一清单。

| 键路径 | 宿主现状 |
|---|---|
| `log_level` / `trajectory_*` / `working_dir` / `mcp_servers` | 无读取点；环境变量侧同样没读点，故 `wizard.sh`/`run.sh`/`.env.example` 已不再采集（配置即业务：不读就不采）；MCP 工具面也未接 |
| `evolution` / `reliability` / `governance.budget_*`、`max_*_risk`、`goal_required_for` | 平台面只装配治理闸：预算与 goal 未接，只消费 `enabled`/`enforcement`/`dir`/`approval_ttl_minutes` |
| `agents.*.memory` / `compress` / `meditation` / `max_tokens` / `temperature` / `thinking_enabled` / `reasoning_effort` / `compress_threshold` / `task_*_ttl` | 装配面不读，schema 未声明 |
| `agents.*.tools[]` 除 `{kind: tool, id: send}` 与 `recall_*` | 工具面接了 `send`＋`recall_query/get/recent/trace`（tool/recall），但宿主只注入检索面 `Searcher`，四个工具各缺 `Embedder`/`Table`/`Originals`/`Trace` ⇒ **调用即具名拒绝**（`recall_query: recall: the deployment wires no read face for this tool`）；`{kind: agent, ...}`（旧 subagent 形态）、`description_file`、`event_params`、file、`exec`、`spec`、`mcp_*`、`skill_*`、子 agent 委派（core 的 `tool/subagent` 预设）均无宿主实现 |

## Agent 声明（六个，`tagent.yaml`）

| Agent | 角色 | 可声明的工具面 |
|---|---|---|
| `tagent` | **entry**：接收消息、编排全局、直接回复 | `send` |
| `knowledge` | 知识获取与翻译 | — |
| `recall` | 记忆召回执行体 | `recall_query` / `recall_get` / `recall_recent` / `recall_trace`（已声明；读面未注入，调用即具名拒绝） |
| `action` | 命令与文件执行 | — |
| `plan` | 工作计划管理（拆解 / 记账 / 归档审计，**不代工**） | — |
| `meditator` | 旁路冥想线（后台反思，自触发） | `send` |

## 治理审批（critical 操作人工把关）

治理闸启用且 critical 操作被拦截时，bot 会向对话推送审批请求（含 digest）。

- **消息批准**：回复 `approve <digest>` / `reject <digest>`（或「批准/拒绝 <digest>」）——仅 `app.wechat.approvers` 白名单内的用户生效（安全默认：未配置白名单时消息批准关闭）；
- **CLI 批准**：`./wechat-bot approve <digest>`（`reject <digest>` 拒绝；零服务器，直接写审批文件）；
- **直投通道**：`ApprovalChannel` 不经 agent 转述，直接送达微信（配 `app.wechat.approvers` 后启用；**直投目标=白名单首个 approver**，其余审批人仍可经回复/CLI 批准）；
- **查看待批**：`approval_list` 工具**不在本宿主工具面内**（本宿主的工具面只有 `send` 与四个 `recall_*`），待批项经审批文件观察。

## 消息链路

```mermaid
graph LR
    WX["微信消息"] --> GATE["approvalGateway.Resolve + host.Admit(DedupKey)<br/>审批先行拦截 → 跨重启消息级去重"]
    GATE --> CLS["ClassifyInbound<br/>入站分类(纯函数)"]
    CLS -->|含媒体/超长文本| INT["IntakeMedia / SaveLongText<br/>流式下载+落盘 workspace"]
    INT --> DRIVE
    CLS -->|普通文本| DRIVE["host.Drive → run.Invoke<br/>会址加锁 → 单次 Invoke"]
    DRIVE --> OUT["deliverTurn<br/>取本会话未投递的 assistant 正文"]
    OUT -->|长文本| LONG["SendLongText 分段"]
    OUT -->|路径候选| DEL["file_delivery<br/>识别并发送文件(拒可执行文件)"]
```

入站与出站两层都依赖**窄接口**（`MediaDownloader` / `FileSender`，`*wechat.Bot` 天然满足），故可脱离真实微信登录、CDN 与 LLM 做单元测试（见 `file_intake_test.go` / `file_delivery_test.go`）。

## 目录导览

**入库的源码与配置**（clone 后即可见）：

| 路径 | 说明 |
|---|---|
| `main.go` | 宿主启动：读配置 → `assembleHost` → 微信登录 → `OnMessage` 回调 → `host.Drive`；发送通路 `hostSender` |
| `host_runtime.go` | 宿主文档 schema 与装配：`loadHostDocument`/`Declaration`/`assembleHost`/`internalInbox` |
| `inject_face.go` | 宿主 HTTP 面：`GET /healthz` 存活 + `POST /task` 注入（`newTaskFace`/`Listen`，只绑回环） |
| `file_intake.go` / `file_delivery.go` | 入站接收层 / 出站投递层（各配 `_test.go`） |
| `tagent.yaml` | 主配置（六 agent + providers + governance + `app.wechat`；未接线键以注释登记） |
| `tagent.rl.yaml` | RL 参数基线（模型/端点/超时；同严格 schema，可装载） |
| `wizard.sh` / `run.sh` / `.env.example` | 部署向导 / 运行入口 / 环境变量模板 |
| `resources/prompts/` | 系统提示词：entry 的 `AGENTS.md`/`SOUL.md`/`TOOLS.md`，五个子 agent 各一份（`knowledge_agent.md`+`knowledge_backend.md`/`recall_agent.md`/`action_agent.md`/`plan_agent.md`/`meditator_agent.md`）；`USER.md` 是部署侧个人上下文不随仓库分发，仓库只带模板 `USER.md.example` |
| `deploy/`、`Dockerfile` / `docker-compose.yml` / `entrypoint.sh` | systemd 单元模板与指南、容器形态（podman/docker 兼容） |
| `skills/` | 技能体系说明与已入库技能；入库边界以 `git ls-files examples/wechat-bot/skills/` 为准（多个技能入库，非只有 `url-fetcher`） |

**运行时生成、不入库**（白名单式 `.gitignore` 刻意忽略，clone 后看不到属正常）：`.env`（密钥）、`.wechat-config/`（登录态 cursor 与去重 seen.json）、`.wechat-context-tokens/`、`workspace/`、`data/`（治理审批状态）、`logs/`、`incoming/`、`knowledge_base/`、`skills/` 下未入库的技能、`.tagent-workspace/`（附件/长文本落盘）、`wechat-bot`（构建产物）。

## 三种运行形态

| 形态 | 命令 | 适用 |
|---|---|---|
| 前台 / 后台 | `./run.sh` / `./run.sh start`（`status`/`log`/`stop`/`restart`） | 本地开发调试 |
| **裸机 systemd 常驻** | `./run.sh build` → 装 `deploy/tagent-wechat.service` → `systemctl enable --now` | 远端服务器（推荐，见 [deploy/README.md](deploy/README.md)） |
| 容器 | `docker-compose up -d`（或 podman） | 已有容器编排基建 |

> `deploy/` 指南与 compose healthcheck 都探 `:8089/healthz`：该端点由宿主 HTTP 面提供（见「装配面」末条），`main.go` 装配失败时日志报 `HTTP 面未装配`，探针随之不可用。

## RL 参数基线（入口已退役）

`rl/` 包有 30 个文件，但组合根只消费 capture 侧（`rl.Recorder`/`rl.NewRecorderAdapter`/`rl.RequestSnapshot`/`rl.SnapshotPolicy`/`rl.VersionedName`，见 `run.go`/`run_request.go`）与 `rl.FeedbackAttribution`（`host_face.go`）；`sft`/`import`/`verifier`/`reward_ledger` 与 `export` 导出器在仓内无生产消费者（只有 contract/集成测试）。模型热换（`SwappableModel`）不在任何装配里，宿主 `main.go` 亦无 RL 分支（HTTP `/task` 面由宿主 `inject_face.go` 自带，不属 `rl/`），故 `./run.sh rl` 已退役（调用即具名拒绝，不静默退回生产配置）。
`tagent.rl.yaml` 保留为**可装载的参数基线**（`TAGENT_CONFIG=tagent.rl.yaml ./run.sh` 可读），重接线时以它为起点。

## 深入阅读

| 主题 | 位置 |
|---|---|
| 部署、数据目录备份、故障排查 | [deploy/README.md](deploy/README.md) |
| 框架机制（记忆 / 事件 / 工具 / 插件 / prompt）历史设计记录 | [../../docs/wiki/](../../docs/wiki/)（historical-verbatim：描述已移除实现，勿当现状读） |
| 平台子系统历史记录（子系统当前未接线，行为矩阵是历史口径） | [platform-subsystems.md](../../docs/wiki/platform/platform-subsystems.md) · [agent-behavior-matrix.md](../../docs/wiki/platform/agent-behavior-matrix.md) |
| 行为契约（SHALL 级规格） | [../../openspec/specs/](../../openspec/specs/) |
| 真实 LLM 契约守护矩阵 | [../../tests/README.md](../../tests/README.md) |
