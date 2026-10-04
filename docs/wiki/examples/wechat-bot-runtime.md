# wechat-bot 运行面：入站去重、文件收发与投递目标

`examples/wechat-bot` 是 tagent 的微信网关示例，独立 go.mod。它把 IM 消息喂给 `TagentAgent`，再把回复投回微信。本页承载这条链路上**只有示例侧才知道**的判据：窄接口边界、去重键分层、文件识别规则、投递目标回退，以及不进 CI 的真链路验收。

<a id="module-layout"></a>
## 一、文件清单

| 文件 | 职责 |
|---|---|
| `main.go` | 装配与消费循环：端点策略、触发源解析、投递目标解析、last-active 会话持久化 |
| `dedup.go` | 消息级幂等（`SeenStore` + `DedupKey`），跨重启生效 |
| `file_intake.go` | 入站分类与落盘：`MediaDownloader` 窄接口、`ClassifyInbound`、文件名清洗 |
| `file_delivery.go` | 出站文件投递：`FileSender` 窄接口、路径识别、按扩展名选发送接口 |
| `system_alert.go` | 一次性启动钩子：消费重启失败告警 |
| `examples/wechat-bot/scripts/verify_large_file.go` | 大文件真链路人工验收脚本（`go:build ignore`，不进 CI） |

<a id="inbound-dedup"></a>
## 二、入站去重：键分层与持久化语义

`SeenStore` 提供**跨重启**的消息级幂等。

**键的分层（按优先级取第一个可用者）**：

1. `ClientID` 非空时用之——那是客户端自己生成的幂等键；
2. 否则退化为 `"uid:" + FromUserID + "#" + sha256(text) 前 16 位十六进制`。

上游网关的消息模型不携带 `msg_id`，因此第 2 层是当前主键；**如果将来上游给出 msg_id，应优先取它作为主键**，而不是继续用文本哈希。

**持久化**：`seen.json` 落在 bot 配置旁边，启动时加载（这就是重启恢复），每次保存都按 TTL 与容量剪枝，写入是原子的（临时文件 + rename）。文件损坏时降级为空集并告警——**去重自身的失败绝不阻塞消息管线**：宁可重复投递一条消息，也不能因为幂等存储坏了就把整条链路停住。

<a id="inbound-intake"></a>
## 三、入站接收：窄接口、流式与名字清洗

`MediaDownloader` 是接收层依赖的**窄接口**：SDK 边下边解密写入调用方给的 `Writer`（流式，内存峰值与文件大小无关），`MaxSize` 由 SDK 在传输过程中强制。`*wechat.Bot` 天然满足该接口，测试期用 mock 替身——与出站侧 `FileSender` 同一模式，接口满足性由**编译期断言**钉住，不靠人记。

`ClassifyInbound` 只做四分类，且分类依据全在消息本身与一个配置事实（是否配了工作区目录）：

| 分类 | 条件 |
|---|---|
| `InboundMedia` | 图片/语音/文件/视频 |
| `InboundIgnore` | 文本为空 |
| `InboundLongText` | 配置了工作区**且** rune 数超过长文本阈值 |
| `ShortText` | 其余 |

落盘文件名与用户标识一律经 `sanitizeName`：只保留 `[A-Za-z0-9._-]`，去掉路径分隔符与 `..`，剥掉前导点（防隐藏文件与相对路径段）。不可信的名字不得进入磁盘路径。

<a id="outbound-delivery"></a>
## 四、出站投递：识别规则与接口分派

`ExtractFilePaths` 从 agent 回复文本里解析**本地**文件路径，六条规则按序全部成立才算：

1. 绝对路径以 `/` 起头，或相对路径含 `/`、以 `./`／`../` 起头；
2. 排除以 `://` 开头的 URL（http/https/ftp 等一律不是本地文件）；
3. 必须以文件扩展名结尾（由正则保证）；
4. 必须 `os.Stat` 确认存在且是普通文件；相对路径先按 `workspaceDir` 解析为绝对路径，**`workspaceDir` 为空则跳过相对路径**；
5. 硬性排除可执行文件：任意可执行权限位命中，或扩展名在拒绝列表内；
6. 结果去重并保持首次出现顺序。

`selectSendFn` 按扩展名选择微信侧的发送接口：图片族走 image、音频族走 voice（`duration` 传 0，交给 SDK/微信侧处理）、视频族走 video，其余统一走 `SendFileFromPath`。

`DeliverFiles` **只负责文件投递**：文本发送（含超 2000 字的长文本分片）仍由消费者负责，因为 `SendLongText` 是包级函数、无法纳入 `FileSender` 接口——把它塞进接口会为了接线而改写既有长文本逻辑。任一文件发送失败只记日志并继续，不阻断其余文件。

<a id="startup-and-routing"></a>
## 五、启动钩子与投递目标解析

`maybeConsumeSystemAlert` 是一次性的启动钩子：等事件循环静默、注入待处理的重启失败、标记已消费。**这里的每一步都只记日志，任何一步都不允许把 bot 打崩**——告警是锦上添花，不是启动的前置条件。

投递目标由 `resolveDeliveryTarget(metaChatID, lastActive)` 决定：优先用本次调用元数据里的会话，取不到则回退到持久化的 last-active 会话（`persistLastActiveChat` 落盘），两者皆无即不投递。触发源由 `resolveTriggerSource` 归一并给出**是否可对外投递**的判定，与 `event` 包的谱系白名单同源。

`endpointPolicyFromEnv` 读取动态端点重定向策略：一个开关加一份**精确主机名**允许列表（不限制端口）。同一份解析同时供 HTTPAPI 的端点策略与 LLM 客户端的逐跳 `CheckRedirect` 守卫使用，因此这两处**永远不会漂移**——策略分叉会让一处放行另一处拒绝。

<a id="large-file-acceptance"></a>
## 六、大文件真链路验收（不进 CI）

`wechat-robot-go` 的 CDN 下载/上传路径在数十 MB 量级上未经真实验证，而这一风险无法用 mock 覆盖——mock 只能证明我们自己写的分支，证明不了 SDK 与 CDN 之间的字节完整性。上表末行的验收脚本复用 `.weixin-token.json` 登录态做真链路验收，用 `//go:build ignore` 排除在常规构建与 CI 之外，仅用于上线前验收与 SDK 升级回归。

验收步骤：

1. 切到 `examples/wechat-bot` 目录，用 `go run` 执行上表末行那个路径；
2. 用微信向 bot 发送一个大文件（建议 ~40MB）；
3. 脚本先用流式 API（`DownloadFileFromItemTo`）落盘，再用旧的 `[]byte` API 下载，对比两者耗时与内存增量——**流式路径的内存峰值应显著低于全量路径**，这正是引入流式接口的理由；
4. 校验字节数与 md5，和 `FileItem` 元数据比对；
5. 反向 `SendFileFromPath` 回传同一文件，验证出站链路。
