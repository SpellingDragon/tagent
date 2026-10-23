> **历史文档（historical-verbatim）**：本篇是已移除生产实现的压缩重写，原版逐字归档于 [docs/archive/wiki/examples/wechat-bot-runtime.md](../../archive/wiki/examples/wechat-bot-runtime.md)；当期行为以 `examples/wechat-bot/` 码面为准。

# wechat-bot 运行面：入站去重、文件收发与投递目标

`examples/wechat-bot` 是 tagent 的微信网关示例，独立 go.mod。它把 IM 消息喂给运行时，再把回复投回微信。本页承载这条链路上**只有示例侧才知道**的判据：窄接口边界、去重键分层、文件识别规则、显式会址、回执血统，以及不进 CI 的真链路验收。

<a id="module-layout"></a>
## 一、文件清单

| 文件 | 职责 |
|---|---|
| `main.go` | 被动通道消费循环：送达意图、长文分片、附件投递、回执接线 |
| `host_runtime.go` | 宿主文档装配：entry agent / provider 注册表 / agents / governance / wechat 段；`internalInbox` 与 `InjectInternal` |
| `session_routing.go` | `SessionRouter`：显式 chat 身份 → 会话 `event.Address` 的单射映射与投递身份；`TakeUndelivered` |
| `dedup.go` | 消息级幂等（`SeenStore` + `DedupKey`），跨重启生效 |
| `file_intake.go` | 入站分类与落盘：`MediaDownloader` 窄接口、`ClassifyInbound`、`sanitizeName` |
| `file_delivery.go` | 出站文件投递：`ExtractFilePaths` 六规则、`selectSendFn`、具名拒绝账目 |
| `file_replica.go` | 大结果投递副本生命周期（workspace 副本区、活跃保护、原文再材料化） |
| `delivery_receipt.go` | 投递终态回执：`receiptLineage`、分级日志与 `InjectInternal` 回流 |
| `send_tool.go` | 主动送达工具：按本次调用的显式会址发送，**不猜、不回退他会话** |
| `approval_channel.go` | 审批网关：请求投递到审批人会话，按 digest 前缀解析批准/拒绝 |
| `system_alert.go` / `reincarnation_notice.go` | 一次性启动钩子：重启失败告警 / 换装后转世通报 |
| `examples/wechat-bot/scripts/verify_large_file.go` | 大文件真链路人工验收脚本（`go:build ignore`，不进 CI） |

<a id="inbound-dedup"></a>
## 二、入站去重：键分层与持久化语义

`SeenStore` 提供**跨重启**的消息级幂等。键按优先级取第一个可用者：

1. `ClientID` 非空时用 `"cid:" + ClientID`——客户端自己生成的幂等键；
2. 否则退化为 `fmt.Sprintf("uid:%s#%s", FromUserID, hex(sha256(text)[:8]))`——`uid:` 前缀 + 用户 ID + `#` + 文本 SHA-256 前 8 字节的十六进制（16 位）。

上游消息模型不携带 `msg_id`，因此第 2 层是当前主键；**如果将来上游给出 msg_id，应优先取它作为主键**，而不是继续用文本哈希。

**持久化**：`seen.json` 落在 bot 配置旁，启动时加载（这就是重启恢复），每次保存按 TTL 与容量剪枝，写入原子（临时文件 + rename）。文件损坏时降级为空集并告警——**去重自身的失败绝不阻塞消息管线**：宁可重复投递一条消息，也不能因为幂等存储坏了就把整条链路停住。

<a id="inbound-intake"></a>
## 三、入站接收：窄接口、流式与名字清洗

`MediaDownloader` 是接收层依赖的**窄接口**：SDK 边下边解密写入调用方给的 `Writer`（流式，内存峰值与文件大小无关），`MaxSize` 由 SDK 在传输过程中强制。`*wechat.Bot` 天然满足该接口，测试期用 mock 替身——与出站侧 `FileSender` 同一模式，接口满足性由**编译期断言**钉住，不靠人记。

`ClassifyInbound` 纯函数四分类，依据全在消息本身与一个配置事实（是否配了工作区目录）：

| 分类 | 条件 |
|---|---|
| `InboundMedia` | 图片/语音/文件/视频 |
| `InboundIgnore` | 文本为空 |
| `InboundLongText` | 配置了工作区**且** rune 数超过长文本阈值（1000） |
| `InboundShortText` | 其余（含未配工作区时的长文本降级） |

落盘文件名与用户标识一律经 `sanitizeName`：只保留 `[A-Za-z0-9._-]`，去掉路径分隔符与 `..`，剥掉前导点（防隐藏文件与相对路径段）。**不可信的名字不得进入磁盘路径**。单文件上限 50MB（容器 workspace 是 256m tmpfs，且 SDK 下载为全量 `[]byte`）。

<a id="real-send-gate"></a>
<a id="outbound-delivery"></a>
## 四、出站投递：识别规则与接口分派

`ExtractFilePaths` 从回复文本解析**本地**文件路径，六条规则按序全部成立才算：

1. 绝对路径以 `/` 起头，或相对路径含 `/`、以 `./`／`../` 起头；
2. 排除以 `://` 开头的 URL；
3. 必须以文件扩展名结尾（正则保证）；
4. 必须 `os.Stat` 确认存在且是普通文件；相对路径按 `workspaceDir` 解析，**`workspaceDir` 为空则跳过相对路径**；
5. 硬性排除可执行文件：任意可执行权限位命中，或扩展名在拒绝列表内——拒绝本身**可见**（进入 `Refused` 账目并产回执），不静默丢弃；
6. 结果去重并保持首次出现顺序。

`selectSendFn` 按扩展名分派：图片族 image、音频族 voice（`duration` 传 0；**超过 5MB 的音频回退为普通文件消息**，voice CDN 拒收长音频）、视频族 video，其余 `SendFileFromPath`。

`DeliverFiles` **只负责文件投递**：文本发送（含超 2000 字分片）仍由消费者负责，因为 `SendLongText` 是包级函数、无法纳入 `FileSender` 接口——把它塞进接口会为了接线而改写既有长文本逻辑。任一文件失败只记日志并继续，不阻断其余文件。`send` 工具走同一发送实现（`hostSender`），只多一个"显式会址"的入口面。

<a id="startup-and-routing"></a>
## 五、启动钩子、会址与审批通道

`hostSender` 是宿主**唯一的出网发送通路**：被动通道（回合结束交付）与主动通道（`send` 工具）汇合于 `sendToUser`，typing 清理、长文分片、附件投递、失败回执一条不漏。

`SessionRouter` 把显式 chat 身份映射到会话 `event.Address`，映射在拼写上**单射**：一个 chat 身份恰有一个会话身份，两个 chat 身份绝不共享；重启重导出同一批会址而不是发明新历史。送达目标按**精确会址**解析：会址须完整且已绑定会话，否则**具名拒绝、不广播、不猜测、不回退他会话**（`ErrTargetRebind` 拒绝把已投递过的地址改绑另一个 chat）。

`maybeConsumeSystemAlert` 是一次性启动钩子：等事件循环静默、注入待处理的重启失败、标记已消费——**每一步都只记日志，任何一步都不允许把 bot 打崩**；告警是锦上添花，不是启动的前置条件。

`approvalGateway` 是宿主侧审批面：`Deliver` 把请求投到每个配置的审批人会话，`Resolve` 按 **digest 前缀**（最短 4 位）解析批准/拒绝——前缀命中多条即拒绝（`ErrApprovalAmbiguous`），发送者不在白名单即拒绝（`ErrApprovalNotApprover`，空白名单拒绝所有）。审批通道是**门不是墙**：投递失败记可见 gap，请求仍可经 CLI、其他入口或请求文件本身作答（契约见 [governance-enforcement.md](../agent/governance-enforcement.md#approval-channel)）。

<a id="delivery-receipts"></a>
## 六、投递终态回执：预期外静默必回流 agent

已送达是 agent 自证可观察的，不需回执；其余**预期外静默**必须以内部事件回流 agent 语境——两起事故（冥想血统扣留交付结算、未声明 http 血统消化）的共同根因正是投递结局对 agent 不可见。

| 终态 | 回执 |
|---|---|
| 已送达（文本 + 文件均成功） | 不回执 |
| `no-target`（会址未绑定会话，无可回退目标） | WARN |
| `send-failed`（用户在场却没收到文本） | ERROR |
| `file-denied`（宿主按具名规则拒绝某文件） | WARN |
| `file-failed`（通道投递失败） | ERROR |

回执经 `InjectInternal(addr, "delivery_receipt", …)` 进内部收件箱：该血统**不在任何可投递血统内**，回执轮自身发送不再产回执，被扣留的正文随产生它的会址的回执一起走。**回执不递归**：`delivery_receipt` 血统自身的消化不再产回执（防自激）。交付特征检测只参与回执分级，永不参与投递裁决。

核心交易：**声明过的，完成即达；未声明的，等用户在场**。系统保证意图可声明、结局可见、账本可查，不接管"此刻该不该说"。

**别把两种"投递"混成一个**：本节说的是示例侧把最终响应送回 IM 宿主；框架另有一条**进程内跨 agent 投递缝**（`DeliverToAgent`，把冥想 agent 的产出投给同进程另一个 agent 的 mailbox），两者共用同一份谱系白名单却方向不同，判据见[持久投递·谱系可见性](../reliability/durable-delivery.md#lineage-visibility)。本示例随附配置采用默认形态：反思统一交给 `meditator` agent——`observed_namespaces: [tagent]` 回看业务线事实链、`deliver_to: [tagent]` 回流卡片，循环跑在保留 session `meditation` 上，不占业务上下文、内部叙述不进微信对话；入口 agent 的 meditation 块**保留键但关闭**。同一机制，差别只在配给谁、观察谁。

<a id="large-file-acceptance"></a>
## 七、大文件真链路验收（不进 CI）

`wechat-robot-go` 的 CDN 下载/上传路径在数十 MB 量级上未经真实验证，而这一风险无法用 mock 覆盖——mock 只能证明我们自己写的分支，证明不了 SDK 与 CDN 之间的字节完整性。`examples/wechat-bot/scripts/verify_large_file.go` 复用 `.weixin-token.json` 登录态做真链路验收，用 `//go:build ignore` 排除在常规构建与 CI 之外，仅用于上线前验收与 SDK 升级回归。

1. 切到 `examples/wechat-bot`，`go run` 执行该脚本；
2. 用微信向 bot 发一个大文件（建议 ~40MB）；
3. 脚本先用流式 API（`DownloadFileFromItemTo`）落盘，再用旧的 `[]byte` API 下载，对比耗时与内存增量——**流式路径的内存峰值应显著低于全量路径**，这正是引入流式接口的理由；
4. 校验字节数与 md5，与 `FileItem` 元数据比对；
5. 反向 `SendFileFromPath` 回传同一文件，验证出站链路。

## 八、历史对照（原版已不再成立的判据）

| 原版 | 当期 |
|---|---|
| 分发层七个终态（含冥想扣留/血统消化分级），`InjectMessageWithSource("delivery_receipt", …)` 注入持久总线 | 四个具名原因（`no-target`/`send-failed`/`file-denied`/`file-failed`）+ 两级日志，`InjectInternal` 进内部收件箱；**回执不递归**不变 |
| `resolveDeliveryTarget(metaChatID, lastActive)`：优先本次元数据，取不到回退持久化 last-active | `SessionRouter` 显式会址，**无 last-active 回退**：目标未绑定即具名拒绝 |
| `endpointPolicyFromEnv`：开关 + 精确主机名允许列表，供 HTTPAPI 与 LLM 逐跳 `CheckRedirect` 共用 | 宿主文档按 provider 条目配 `api_endpoint`；逐跳重定向守卫不在示例侧 |
| 投递目标只按 chatID 字符串 | `event.Address` 单射映射 + `TakeUndelivered`（按会址取未达消息） |
