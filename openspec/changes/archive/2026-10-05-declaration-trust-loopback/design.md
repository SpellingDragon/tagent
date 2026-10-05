# Design

## D1 判据为何是 TCP 源地址（威胁模型）

受理条件加一条：请求源地址为回环即视同已鉴权。安全性不依赖我们的判断，依赖协议栈事实：

| 论证 | 内容 |
|---|---|
| 不可伪造 | 冒充 127.0.0.1 的包在路由层就到不了目标端口（回环流量不出网卡，OS 保证）。外部主机无论怎样构造，其连接的 `RemoteAddr` 都是它自己的地址 |
| 能力边界不扩大 | 能以回环源连入的只有本机进程。能本机执行进程的实体，本就能读配置、杀进程、直接伪装 poller 流量——授予"可声明"不给它任何新能力 |
| **与既有不变量同轴（实施期发现）** | `ValidateListenAddr`（http_api.go:207）本就拒绝「无 token 却绑非回环」——即无 token 部署**不可能**有非回环监听面。本豁免不是新划信任边界，而是确认 `RemoteAddr=loopback` 恰是该既有边界在内侧的唯一形态（`authorized` Godoc 已记此关联） |
| 判据自足无需监听面检查 | 即使部署监听 0.0.0.0 且无 token，外部连入的源仍是外部地址 ⇒ 照样拒；只有真本机连接获豁免。无需引入"是否只绑回环"这种会配错的状态 |

部署形态逐一推演：

| 形态 | 连接源地址 | 结果 |
|---|---|---|
| 默认：只绑 127.0.0.1，无 token，poller 同机 | 127.0.0.1 | **受理**（本变更目标场景，零配置即通） |
| 监听 0.0.0.0，无 token，外部直连 | 外部 IP | 400 拒（不变；该形态本就该配 token） |
| 容器内服务，宿主端口映射 | 172.x 网桥 | 400 拒（跨边界场景要求 token，保守正确） |
| SSH 隧道转发后本机连接 | 127.0.0.1 | 受理——能开隧道=已有本机 shell，在本机信任边界内 |

可归因保险：豁免生效时 `log.Infof("[HTTPAPI] intent declaration admitted via loopback source (no auth configured)")`——滥用可见、可查、可事后收紧。

## D2 实现三处（一处判据 + 一处日志 + 一处对端规则）

- `rl/http_api.go`：新增 `requestFromLoopback(r)`（`net.SplitHostPort` + `net.ParseIP(...).IsLoopback()`，解析失败一律 false 保守拒）；`validateTaskRequest` 的声明校验条件由 `h.authToken == ""` 改为 `h.authToken == "" && !requestFromLoopback(r)`；豁免路径打 D1 所述日志（仅在确实无 token 时打，避免噪音）。
- `examples/wechat-bot/mail-poller/mail_poller.py`：`inject()` 声明条件 `tok` → `tok or url host 为回环（127.0.0.1/localhost/::1）`。规则与服务端同形：跨机地址无凭时**不声明**（否则 400 与注入重试循环互锁，永不收敛）。
- spec delta：`inbound-intent-declaration` 修订受理需求 + 2 scenario。

## D3 fail-before 与验证

- 红：无 token + 回环源带声明 → 现码 400（`declaration_requires_auth`）；测例期望 202 + 事件带血统 ⇒ 红。
- 绿：修后同测通过；且三条不变量各一针——无 token + 非回环源仍 400、有 token 任意源仍受理、非法值域仍 400（值域不因源放宽）。
- 全量：`go test ./rl/ ./agent/ -count=1`、根包 `-short`、`-race` 根+rl、lint、`openspec validate --strict`、CI 四 job；poller `python3 -m py_compile`。

## D4 否决区（防熵，执行期不得复活）

| 否决项 | 理由 |
|---|---|
| 回执聚合/去重，或"内部播报类不回执"（远端建议） | 压报警器不治病灶。根因消除后 `undigested-lineage` 回执频率自然归零；剩余回执全是真集成缺陷信号。若未来重现高频回执，那是新扣留病灶，该修而非该滤 |
| 退役任务（`task-retired`）结果机械补发 | 与"机械补发 pending"同理：退役时结果可能部分/过时，机器无安全域可判。现状已闭环——被扣全文在事实链与投影内（agent 后续轮可引用），送达状态由回执告知，说与不说归 agent |
| 新增"是否信任回环"配置开关 | TCP 源地址判据本身自足；开关只多一个能配错的选项 |
| 检查监听地址决定是否豁免 | 需要新增 HTTPAPI→Server 的状态耦合，且 RemoteAddr 判据已覆盖该意图，更严且无状态 |
