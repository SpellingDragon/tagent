# 静态决策（D1–D14）

## D1 工具面收敛：send 是唯一通信工具

`send` 唯一职责=跨 task、会话通信。`resume_task`（重入）与 `deliver_summary`（冥想跨会话写）作为**模型工具删除**，语义归并进 send 的两种寻址；任务管理工具（list/get/cancel/relaunch）不属"通信"，保持现状。**被否**：send 与 resume_task 并存（用户明示"只暴露一个 send"）；旧出网 send 语义保留一版（无生产消费者，无兼容收益）。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | send-tool-face | wechat-host-send-tool | send 声明/参数面/回执/装配唯一化（R2 目标·身份·范围·回执统一） | 无（接口常数先行） |
| 02 | task-reentry-send | task-reentry, async-task-execution | 对内重入：task ID 寻址、settled 重激活、新 Round、占坑单胜 | 01（参数面常数） |
| 03 | cross-session-send | cross-session-delivery, meditation-delivery-tool | 对外通信：agent+session 寻址、四道安全门、入链+驱动一轮、deliver_summary 归并 | 01 |
| 04 | consumers-migration | —（验证归 01–03 场景） | prompt/wiki/smoke/CHANGELOG 消费面迁移 | 01,02,03 |
| 05 | interactive-reentry | async-task-lifetime | 交互式命令（tmux）重入归并（**条件波次**，用户明示可降档） | 02 |
| 06 | notice-inbox-contract | unified-task-semantics, event-metadata-contract | R1 持久接纳+消费确认+稳定身份重投；R4 逐消息来源分账 | 无 |
| 07 | ops-notice-delivery | ops-notice-delivery(ADDED) | R3 固定目标+立即唤醒；R5 真实现场查询接线 | 06 |
| 08 | ops-health-contract | ops-deployment-integrity | R6 healthz 鉴权对齐+五态探测，鉴权失败不杀进程 | 无 |
| 09 | full-duplex-scheduling | consumer-loop-convergence | R7 慢工具不阻塞新输入、多来源合批（**首叶先实证 R7 断言**） | 02（重入消费面） |

## D3 波次表（波次仅汇报分组，解锁=依赖入 main）

| 波次 | 域 | 说明 |
|---|---|---|
| W0 最小可证伪路径 | 01 部分 + S2 红→绿短测 | R34 规模警戒（约 40 叶>20）：先杀麻雀——S2 从用户症状直通缺陷机制 |
| W1 核心语义 | 01, 06, 08 | 互不依赖，三域并发（触碰集不相交） |
| W2 重入与对外 | 02, 03 | 依赖 01 参数面常数（R22 冻结后可并行开发、集成需 01 合并） |
| W3 消费与运维 | 07, 09 | 07 依赖 06；09 首叶实证 R7 后展开 |
| W4 迁移与收尾 | 04, 05(条件), F | 05 未实施即显式记录裁剪 |

## D4 合并规则（契约守护）

- 契约目录=主 `openspec/specs/` 与 `api/` 公共面；本变更对主 spec 只做 MODIFIED/ADDED delta，F1 断言"除声明 delta 外契约零改动"。
- 同一文件簇（`assembly/host_tools.go`+`host_document.go`、`examples/wechat-bot/wechat*.go`）跨域改动必须合单或串行（R28 触碰集）。
- 共享面（`task.Spec`/`Notice` 身份常数、`event.MetaKey*`）由编排者单写或 R22 冻结签名级契约后派发。

## D5 三级验收

1. **孙任务级**：每叶 verify 命令 exit 0（正锚+负锚+消费者锚随任务行冻结，R29）。
2. **子域级**：每域 spec scenarios 全部映射到具名测试。
3. **变更级 DoD**：①症状锚 S1–S3+N1–N6 红转绿（回滚双证据）；②`go build ./...`+`go test ./...`+项目门禁 exit 0；③模型工具面断言（恰一个通信工具）；④CHANGELOG 四要素；⑤openspec validate --strict。

## D6 target 参数面：对象二选一

`target` 为对象，内含互斥子字段：`{"task": "<task-id>"}` 或 `{"agent": "<id>", "session": "<id>"}`；两者同现或皆缺=具名拒绝（`[send_denied]`）。**被否**：双顶层字段（声明面冗余）；单字符串前缀自判（脆弱、命名即契约）。

## D7 重激活语义：仅 settled、修改状态、新 Round

`send{target:{task}}` 对 settled task 触发**重激活**：状态 settled→活跃（registry 新增 reactivate 迁移，走 `MutationID`+`ExpectedRevision` 乐观并发），以新 Round 续行，可再次 settle；**failed/cancelled 具名拒绝**；running/suspect（轮次在飞）拒绝并引导。重激活留审计事实（settled→reactivated 翻转可追溯）；新 Round 的 settle 通知是新身份（TaskID+Round+Kind+Seq 不与旧合并），恰好一次驱动发起方。

**与既有 spec 的冲突处置（显式记录）**：`task-reentry` 与 `async-task-execution` 现钉"完成态（completed/**failed**）可重入"。用户裁定（2026-11-01）："settled 可以被重新激活，修改状态"+"仅 settled 可重激活，failed/cancelled 具名拒绝"。按用户裁定翻转 failed 条款（MODIFIED delta + 场景翻转 + 回归证据）；`action_tool_desc.md` 既有 prompt（"已退出命令 resume 失败→relaunch_task"）与新语义一致。**被否**：保留 failed 可重入（违反用户裁定）；终态开新 Round 不改状态（用户明示"修改状态"）；终态可复活全部（用户裁定仅 settled）。

## D8 对外送达语义：入链+驱动一轮

`send{target:{agent,session}}`：写事件入目标会话链（`CommitToSession`）并驱动目标会话跑一轮（复用 `Host.Invoke` 驱动面）；忙碌会话在规定边界（one-slot gate）读取，重复投递按事实身份幂等。**被否**：静默入链（用户裁定驱动）；参数区分驱动/记录两态（先单态，需要时再扩）。

## D9 通知持久接纳模型（R1）

启动通知先进入**稳定身份的持久接纳记录**（身份=来源+内容摘要+接纳序号，重投同身份），可靠交接（输入提交成功）后才完成 `.notified`/`.consumed` 标记；改名失败不再"下次重报"而是保持待接纳。Drain 返回副本但**消费确认前不清空**（ack 由 Invoke 提交成功驱动）。**被否**：仅调整改名时序（窗口仍在）；借用 task 受理面（启动通知非 task，强行挂接违反语义单元）。

## D10 运维通报目标与唤醒（R3，用户裁定）

启动通知投递到**预定目标会址**（配置声明的运维会话，缺省=宿主运维会址常量），不再空地址入 pending；无人输入时即驱动目标会话跑一轮（立即唤醒）。多会话竞争不由到达先后决定。**被否**：维持"下次对话带入"（用户裁定唤醒）；唤醒可配（先单态）。

## D11 来源分账（R4）

每条入箱消息的原始来源（`reincarnation`/`system_alert`/`receipt`…）随消息进持久事实 Metadata（`event-metadata-contract` 键词表扩展），与回合级 `trigger_source`（谁触发本回合）分账；不解析正文即可查询。**被否**：整回合统一盖来源（掩盖混合输入）。

## D12 健康契约（R6）

对齐 `ops-deployment-integrity` 既有条款：healthz 鉴权不豁免（或显式修订该条款——二选一在 08 域首叶定夺，附被否记录）；运维脚本探测区分五态（200/401/403/超时/拒连），401/403→AUTH_FAIL 告警**不杀进程**。**被否**：改脚本注释迁就免鉴权现状（spec 已钉不豁免，改注释=文档撒谎）。

## D13 全双工消费（R7，断言待实证）

R7 断言本轮未实证（dispatch 等待/合批测量），09 域**首叶=断言实证**（既有测量复跑），真则按 `consumer-loop-convergence` 既有条款收敛实现（派发工具不等结果、多来源合批、慢工具不阻塞新输入），假则降为"测量缺口"登记。**被否**：未实证即排实现工（R34）。

## D14 回退与基线

出网投递链（`hostDeliverFunc`/三态审核）零改动（它服务回合正文，非过时机制）；task 持久层旧记录只读保全。**回退=回滚提交**（清除式删除不设运行时开关，D15），与基线 diff 仅含本变更声明面（F1）。

## D15 清除式归并（用户指令：不保留过时机制代码，实现干净、架构熵减）

本变更对被归并机制**删除**而非并存（覆盖"旧机制不下线"默认纪律，回退保障由 git 回滚承担）。删除清单（DEL）与保留边界：

| # | 删除对象 | 级联回卷检查（§四五级） |
|---|---|---|
| DEL-1 | 旧出网 send 路径：`hostSendTool.deliver` 字段、send 侧 `plugin.Delivery` 构造；`hostSendLineage`/`hostSendLineageOf` 无新消费者则一并删（血统审计由 06 来源分账承担） | `hostDeliverFunc` 本体保留（回合正文出网）；删除后单使用方伴生引用回卷 |
| DEL-2 | `DeliverSummaryTool` 整体（`features/evolution/meditation/deliver.go`）、`meditation.DeliverSummaryToolName`、`hostToolIDs`/`toolsFor` 挂载点、`task.DedupKey` receipts | `hostMeditationPort`（`EventSink`/`SessionQuery` 接口面）降为单使用方/无使用方即回卷或收缩 |
| DEL-3 | `resume_task` 模型工具面（声明/挂载点） | **保留**被 send 复用的底层 Resume/注入通道（新机制的实现基座，非过时机制） |
| DEL-4 | `internalInbox` 空地址 pending 路径（启动通知改固定目标后无消费者）；`InjectInternal` 不完整地址改具名拒绝 | 无消费者即删；`Drain` 空地址分支随之回卷 |
| DEL-5 | 旧"入箱即改名"时序（06 时序倒转后旧代码删除） | 标记语义唯一化，无第二套完成判定 |
| DEL-6 | `declaresSendOutside` 的 send 校验分支 | 函数单使用方回卷或收缩 |

**不删**：测试场景（迁移保真不删场景）、CHANGELOG 已发布段与历史审计（不改写）、出网投递链本体、prompt 中的 task 生命周期协议（只换运输工具名）。

**残留门（F1.5）**：收尾全量 `grep` 对 DEL 关键字（`deliver_summary`/`DeliverSummaryTool`/`resume_task`/`hostSendLineage`/`declaresSendOutside`）在 `*.go` 与 prompt 面**零残留**（spec/CHANGELOG 历史段排除表显式列出）；每删一项跑伴生引用降级检查。
