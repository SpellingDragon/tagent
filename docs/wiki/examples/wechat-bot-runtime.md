> **活契约页**：逐条对齐当前码面（`examples/wechat-bot/` 的 `main.go`／`wechat*.go` 与 `smoke*_test.go`），代码内 `// 契约:` 注释指向本页锚点。口径统一于[存储概念地图](../../demos/storage-concept-map.html)：**链**管身份与因果 · **原文仓**管字节 · **账本**管责任 · **其余一切现算**——本页只在示例侧守住"哪条消息归哪个会话、哪份正文是否真的到了用户手里"。

# wechat-bot 运行面：入站受理、单射会址与投递账

`examples/wechat-bot` 是 tagent 的微信网关示例（独立 go.mod）：它把 IM 消息喂给运行时，再把回复投回微信。**运行时全在 `assembly.Boot` 里**——模型、工具、存储、冥想、注入面都由装配给出；示例侧只剩通道语义：去重、入站分类、会址映射、唯一出网通路、投递回执与真链路验收。

<a id="module-layout"></a>
## 一、模块布局：一个装配入口，五个 go 文件

| 文件 | 职责 |
|---|---|
| `main.go` | 只做 wiring：读配置 → 审批 CLI → `newWechatApp` → `assembly.Boot` → 绑定通道 → 运行 → `host.Close` |
| `wechat.go` | 通道主体：`hostRuntime`（`Admit`／`Drive`）、`internalInbox`、`hostSender`（唯一出网通路）、`receiptLineage` 回执、`SeenStore`／`DedupKey`、`SessionRouter`、`registerHandlers` |
| `wechat_files.go` | 文件收发：`ClassifyInbound` 四分类、`sanitizeName`／`buildSavePath`、`IntakeMedia`、`classifyFilePaths` 六规则、`selectSendFn`、`fileDelivery` 报告 |
| `wechat_approval.go` | 审批网关：`Deliver` 投给审批人、`Resolve` 按 digest 前缀解析、`runApprovalCLI` |
| `wechat_notice.go` | 一次性启动钩子：`maybeInjectReincarnationNotice`（换装通报）与 `maybeConsumeSystemAlert`（重启失败告警） |
| `smoke*_test.go` | `//go:build smoke` 的本地长程冒烟与六个症状锚（见第五节） |

<a id="startup-and-routing"></a>
## 二、启动与会址：一个 chat 身份恰有一个会话

`hostRuntime{invoke, router, inbox, sender, seen, gates}` 是应用的宿主侧：一条出网通路、一张路由表、一个内部收件箱。`bindWechat` 把 `host.Invoke` 包成 `invokeFunc`（只把 `turn.Terminal == KindFailed` 折成 `failed`），再登录、绑定发送面、注册入站钩子、起两个一次性钩子；`close` 用 10s 超时 `host.Close`。

`SessionRouter` 把显式 chat 身份映射到会话 `event.Address`，拼写**单射**：一个 chat 恰一个会话，两个 chat 绝不共享（`ErrTargetRebind` 拒绝把已绑定的会址改绑另一个 chat）；`Target(addr)` 是唯一解析方向，**没有任何"最近活跃会话"回退**，解析不到就是具名拒绝（`errNoSendTarget`）。`NextInvocation(addr)` 为每次驱动取新 invocation，`lockAddress` 让同一会话的驱动串行。

**启动钩子是锦上添花**：`maybeConsumeSystemAlert` 等事件循环静默后注入重启失败、`maybeInjectReincarnationNotice` 等待 notice 出现后注入换装通报，每一步只记日志，任何一步都不许把 bot 打崩。

**审批通道是门不是墙**：`approvalGateway.Deliver` 把请求投到每个配置的审批人会话，`Resolve` 按 **digest 前缀**（最短 4 位）解析批准/拒绝——前缀命中多条即 `ErrApprovalAmbiguous`，发送者不在白名单即 `ErrApprovalNotApprover`（空白名单拒绝所有）；未配置 governance 时网关直接 `Enabled()==false`，不挂 `assembly.WithApprover`。投递失败只记可见 gap，请求仍可经 `runApprovalCLI` 或请求文件本身作答——没有审批人接话不等于拒绝。

<a id="inbound-intake"></a>
## 三、入站受理：先去重，再分类，最后才驱动

`registerHandlers` 的钩子顺序是固定的：审批裁决 → 去重 → 分类 → 附件/长文落盘 → `Drive`。

- **去重**：`Admit(DedupKey(msg))` 失败即 WARN 后静默丢弃——重复消息不进会话，也不回话。`DedupKey` 取 `ClientID` 优先，否则 `uid:<FromUserID>#<text sha256 前 8 字节 hex>`；`SeenStore` 落 `seen.json`，跨重启生效，文件损坏降级为空集——**去重自身的失败绝不阻塞消息管线**。
- **分类**：`ClassifyInbound(msg, workspaceConfigured)` 是纯函数四分类——`InboundMedia`（图/语音/文件/视频）／`InboundIgnore`（文本为空）／`InboundLongText`（配了 workspace 且 rune 数 > 1000）／`InboundShortText`（其余，含未配 workspace 时的长文降级）。
- **落盘**：`IntakeMedia` 用 `MediaDownloader` 窄接口流式写入调用方给的 `Writer`，`DownloadOptions{MaxSize: 50 MiB}` 由 SDK 在传输中强制；单文件超限与可执行扩展名都在**下载前**拒收并回一条具名理由。落盘路径 `uploads/<chatID>/<ts>_<name>` 经 `sanitizeName`（只留 `[A-Za-z0-9._-]`、消 `..`、剥前导点）与 `buildSavePath` 的目录内二次校验，**不可信名字不进入磁盘路径**。
- **驱动**：`Drive` 先 `AddressFor` 再 `Bind`（会址在建，映射同时在），取 `inbox.Drain(addr)` 的内部通知与用户文本拼成输入，空输入直接返回不空转，其余交 `invoke(InvokeRequest{InvocationID, Address, TriggerSource: "user", Input})`。失败不在这里另写话术——**失败告知由宿主自己的失败通知发出**，示例侧不持第二份措辞。

<a id="outbound-delivery"></a>
## 四、出站投递：一条通路，正文与附件一次算清

`hostSender` 是宿主**唯一的出网发送通路**：回合交付（`Send`）与主动通道（`send` 工具）都汇合到 `sendToUser`，实现 `assembly.MessageSender`。

`sendToUser` 的顺序不可调换：按**精确会址**解析目标（解析不到即 `no-target` 回执 + `errNoSendTarget`，扣留而非猜测）→ 清 typing（60s 内残留才停）→ 正文 > 2000 字节时先试 context-token 长文接口，拿不到 token 或长文失败就 `truncateBytes` 到 2000 并标注 `[Message truncated]` → 文本失败回 `send-failed` 回执并把原因原样交回调用方 → **附件投递照常发生**（`deliverFiles` 用 `originalContent`，不受文本截断影响）。

文件识别（`classifyFilePaths`）六条同时成立才投：本地路径形态（绝对以 `/` 起、相对须含 `/` 且配了 workspace）／排除 `://` URL／必须带扩展名／`os.Stat` 确认是普通文件／拒绝可执行（任一可执行权限位或扩展名在拒绝表，`executableRefusal` **硬规则不可配置**）／去重保首次顺序。`selectSendFn` 按扩展名分派 image／voice／video／file，语音 `duration` 传 0，**超过 5 MiB 的语音回退为普通文件消息**。拒绝进 `Refused` 账、失败进 `Failed` 账，各回一条回执，一个文件失败不阻断其余文件。

`cli.HostBridge` 是这条通路对运行时的四个红线：只做转换（不持运行时、状态与事件循环）／每条目按给定的 `event.Address` 寻址／裁决须匹配审批身份与参数 digest／**绝不用最近会话替代会址、绝不把裁决套到另一次调用**；`DeliveryID` 是投递的稳定身份，回执按它对账。

<a id="delivery-receipts"></a>
## 五、投递回执与 owed 账：静默要回流，欠的不许忘

**已送达是 agent 可自证的，不需要回执；预期外静默必须回流。** 四个具名原因、两级日志：

| 终态 | 级别 | 触发点 |
|---|---|---|
| `no-target`（会址未绑会话，无可回退目标） | WARN | `sendToUser` 解析不到目标 |
| `send-failed`（用户在场却没收到文本） | ERROR | 文本发送失败 |
| `file-denied`（宿主按具名规则拒绝某文件） | WARN | `fileDelivery` 发送前拒绝 |
| `file-failed`（通道拒绝投递） | ERROR | `fileDelivery` 发送失败 |

回执经 `InjectInternal(addr, receiptLineage, …)` 进该会址的内部收件箱，`Drive` 下一轮 `Drain` 时随用户文本一起进模型；**回执不递归**（`triggerSource == receiptLineage` 直接丢弃），正文随产生它的会址的回执同走。

**受理回执只有一个字**：`task.AdmissionReceiptText = "ok"`，并经 `AdmissionReceipt` 的 `Text()`／`String()`／`MarshalJSON()` 三个渲染口输出同一份最小正文——**task id、状态词、结算承诺永不进模型**。task 身份是 `task-<32 hex>`（`mintTaskID` 随机铸造、不可枚举），上游看到的 `ok` 与它无关。因此"受理静默"是可断言的：模型世界里收到的 tool result 恰为 `ok`。

**owed 账**（`assembly/host_delivery.go`）保证**崩溃不漏送**：正文在任何出海之前先落成一条 durable 交付义务（key 前缀 `tagent/task/v1/delivery:`，状态 `owed`）；发送被审核插件挂起即 `held`、被通道拒绝即 `retry`、被接受即 `sent`、被人工拒绝即 `refused`，非入口会址的欠账以 `headless` 退役（保留正文供查）。重启时 `restorePendingDeliveries` 扫回全部记录：`owed` 的入口会址**重新投递**，`held`／`retry` 重新挂回待裁决——同一身份只结算一次。

**两种"投递"不是一个东西**：本节说的是示例侧把最终响应送回 IM；框架另有**进程内跨 agent 投递**（`deliver_to` 白名单 + `runtime/state/send.go` 的 `WithDeliverTo`，越界目标以 `RefusalNotAuthorized` 拒绝），两者共用谱系白名单却方向不同，判据见[持久投递·谱系可见性](../reliability/durable-delivery.md#lineage-visibility)。本示例的冥想采用默认形态：回看业务链、把卡片回流入口 agent，循环跑在保留会话上，内部叙述不进微信对话。

| 症状锚 | 断言 | 测试 |
|---|---|---|
| G1 受理静默 | 受理后结算前本会话 outbound=0；正常提问的回答仍投递 | `TestLocalSmokeSymptomAcceptedSilent` |
| G2 完成恰一条 | 结算正文至多投递一次、无确认类话术 | `TestLocalSmokeSymptomSettledOnce` |
| G3 失败告知 | 500 注入后恰一条告知且带原始错误，成功回合零失败话术 | `TestLocalSmokeSymptomFailureNotice` |
| G4 exec 静默 | 三态各不产生模型回合，但都落无载荷审计事实 | `TestLocalSmokeSymptomExecStateSilent` |
| G-A 无 send | 入口工具面不含 `send`、部署文档不声明它，正文恰一条 | `TestLocalSmokeSymptomNoSendTool` |
| G-B 投递 hold | hold 期间 outbound=0，批准后恰一条原正文；拒绝后仍 0 条并留审计 | `TestLocalSmokeSymptomDeliveryHold` |

台架锚：`TestLocalSmoke`／`TestLocalSmokeGuards`／`TestLocalSmokeInjectedSession`（装配、守卫与注入会话形状），全部 `//go:build smoke`，不进 CI。

<a id="large-file-acceptance"></a>
## 六、大文件真链路验收（不进 CI）

`wechat-robot-go` 的 CDN 收发在数十 MB 量级上无法用 mock 证明——mock 只覆盖我们自己写的分支，覆盖不了 SDK 与 CDN 之间的字节完整性。`examples/wechat-bot/scripts/verify_large_file.go` 用 `//go:build ignore` 排除在常规构建之外，复用登录态做真链路验收：① 在 `examples/wechat-bot` 下 `go run` 该脚本；② 用微信向 bot 发一个 ~40 MiB 文件；③ 先流式（`DownloadFileFromItemTo`）落盘、再用全量 `[]byte` 接口下载，**流式内存峰值应显著更低**；④ 校验字节数与 md5 对照 `FileItem` 元数据；⑤ 反向 `SendFileFromPath` 回传同一文件验证出站链路。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 文案类失败话术只有一条通路 | 失败告知由宿主失败通知单点发出，示例侧不另写 | 若要多语言，须先定"失败告知单源"的归属 |
| 去重靠文本哈希兜底 | 上游无 `msg_id`，`uid:`+文本哈希为主键 | 上游给出 `msg_id` 后优先取它 |
| 大文件验收仍是人工 | 脚本（`examples/wechat-bot/scripts/verify_large_file.go`）不进 CI，靠上线前跑 | 用可控 CDN 桩把字节完整性纳入回归 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（链／原文仓／账本／现算的分工）· [存储时光机](../../demos/storage-time-travel.html)
