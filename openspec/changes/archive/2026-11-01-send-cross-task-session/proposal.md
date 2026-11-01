# send 收敛为唯一跨 task、会话通信工具

## Why

用户裁定（立案原话）："对于 agent 应当只暴露一个 send 工具，这一 send 工具唯一作用是跨 task、会话通信。例如冥想使用的 session 需要向另一个 session 发送事件，或者 main agent 重入 task（包括 agent 和交互式工具）。"

现状与之不符（✅ 代码实证）：

1. **口径分裂**：`hostSendTool` 的 Declaration 承诺 "Deliver one body to the user address of this call"（出网送达用户），CHANGELOG v0.4.0 却称 send 仅保留为跨代理通信工具，设计文档 D1 标注"跨代理 send（未来）"——同一工具名下并存两套职责口径。
2. **通信工具面不唯一**：冥想跨会话写走 `deliver_summary`（`features/evolution/meditation/deliver.go`），task 重入走 `resume_task`（`task-reentry` 主 spec + prompt 资产大量引用），进程内跨 agent 投递走 `WithDeliverTo` 缝，模型面可见的"通信"工具有三个。
3. **重入语义缺失**：subagent task 一轮即 settle（`tool/subagent/tool.go` serve→completeDelegation），settle 后不可再驱动；`resume_task` 的重入原语仅在 spec 与 prompt 中存在，且其合法源状态含 failed（与用户裁定冲突，见 design D7）。

## What Changes

- **BREAKING（模型面）**：`send` 的唯一职责重定义为**跨 task、会话通信**，参数面为 `target`（对象二选一：`{task}` 或 `{agent, session}`）+ `content`；旧出网语义（送达用户渠道）从 send 移除，出网唯一路径是回合正文自动投递（v0.4.0 既定，不变）。
- **BREAKING（工具面收敛）**：`deliver_summary` 删除归并（冥想改用 `send{target:{agent,session}}`）；`resume_task` 作为模型工具删除归并（重入改经 `send{target:{task}}`）；任一 agent 的模型面恰有一个通信工具 `send`。任务管理工具（list/get/cancel/relaunch）不在"通信"之列，保持现状。
- **对内重入（task ID 寻址）**：main agent 面前 agent tool 与异步命令统一为 task；`send{target:{task}, content}` 向 task 注入输入使其继续。**settled 可被重新激活并修改状态**（settled→活跃，新 Round 续行，可再次 settle）；failed/cancelled 具名拒绝（`[send_denied]`）。交互式命令（tmux）重入降档为条件波次（用户明示："如果交互式命令重入不好做，先保障好内部 agent tool 重入"）。
- **对外通信（agent+session 寻址，仅同级别感知会址）**：`send{target:{agent,session}, content}` 向同级会话发事件；**送达语义=入链+驱动一轮**；复用进程内投递缝既有安全门（deliver_to 白名单 fail-closed / 盲投拒绝 / 未知目标 / 未运行具名拒绝）；risk_rules 审批可对跨会话形态配 critical→hold→批准补投。
- **回执语义不变**：成功裸 `ok`、拒绝 `[send_denied] <reason>`、hold `[send_held] <reason>`；error 仅留给参数形状非法。
- **通知消费可靠性（R1）**：启动通知（转世通报/系统告警）先入**稳定身份的持久接纳记录**，可靠交接或消费确认后才完成 `.notified`/`.consumed` 标记；重投复用同一身份，幂等不重复处理；Drain 不再先于输入提交清空。
- **运维通报目标与唤醒（R3，用户裁定：立即唤醒+固定目标）**：启动通知投递到预定目标会址（不再空地址入 pending 由到达先后决定），无人输入时即驱动目标会话跑一轮。
- **逐消息来源保留（R4）**：每条入箱消息的原始来源（`reincarnation`/`system_alert`/…）进持久事实 Metadata，与回合级 `trigger_source` 分账（"事实从哪来" vs "回合谁触发"）。
- **历史现场接线（R5）**：转世通报接入受范围限制的真实现场查询面（当前传 nil），无存储/读失败时清楚报告降级。
- **健康契约一致（R6）**：healthz 鉴权契约与 `ops-deployment-integrity` 既有条款对齐（鉴权不豁免），运维脚本探测区分五态（200/401/403/超时/拒连），鉴权失败不触发杀进程。
- **全双工消费（R7）**：消费循环收敛到 `consumer-loop-convergence` 既有条款（派发工具不等结果、多来源合批），慢工具在跑时新输入按时进下一次决策。

## Capabilities

spec 已下沉到二级子变更（`changes/<NN-domain>/specs/`），一级只留编排契约。

### Modified Capabilities（5.9 检索裁决：全部扩展既有能力，不新增并行 capability）

| capability | 裁决 | 理由 |
|---|---|---|
| `wechat-host-send-tool` | MODIFIED（职责重写） | send 工具契约的既有归属；内容重写为跨 task/会话通信，capability 名沿用以避免第二条 send 契约并行运转 |
| `task-reentry` | MODIFIED（工具面归并 + 合法源状态改仅 settled） | `resume_task` 语义归并进 send；failed 可重入条款按用户裁定翻转（D7） |
| `async-task-execution` | MODIFIED（resume 边合法源状态同步） | 其"resume 边"条款与 task-reentry 同步改 |
| `cross-session-delivery` | MODIFIED（消费面扩为 send 对外形态） | 白名单四道门语义不变，挂载点从 deliver_to 缝扩为 send 对外寻址 |
| `meditation-delivery-tool` | MODIFIED（deliver 归并进 send） | 冥想跨会话写改经 send，deliver_summary 工具面消失 |
| `async-task-lifetime` | MODIFIED（条件波次，exec op=send 注记） | 交互式命令重入归并见 05 域；未实施则不动 |
| `unified-task-semantics` | MODIFIED（接纳/确认三层延伸到启动通知） | R1 候选修正与其"受理≠交付、接纳才推进"同构，扩展不另立并行契约 |
| `event-metadata-contract` | MODIFIED（逐消息来源入 Metadata） | R4 落在"注入点职责归一"既有条款上 |
| `ops-deployment-integrity` | MODIFIED（补 403/超时五态场景） | R6 实为实现欠账：该 spec 已钉"healthz 鉴权不豁免"，代码未做；补场景不改条款方向 |
| `consumer-loop-convergence` | MODIFIED（补慢工具/合批场景） | R7 实为实现欠账：该 spec 已钉"不等派发工具完成"；补测量与场景 |

### New Capabilities

| capability | 裁决 | 理由 |
|---|---|---|
| `ops-notice-delivery` | ADDED | 运维通报（转世/系统告警）的固定目标、立即唤醒与现场内容契约；5.9 检索无同域能力（启动通知/运维通报无既有 capability） |

## 症状直译锚（验收矩阵第一行，第一天红着）

| # | 用户视角直译场景 | 锚测试 |
|---|---|---|
| S1 | 任一 agent 的模型工具面恰一个通信工具 `send`，无 `deliver_summary`、无 `resume_task`，Declaration 只承诺跨 task、会话通信 | `TestSendSymptomOnlyOneCommunicationTool` |
| S2 | main agent 调 `send{target:{task}, content}` 向自己发起的 agent tool task 注入输入，该 task 以新输入继续（无需知道 task 内部 agent+session） | `TestSendSymptomReentersAgentTask` |
| S3 | 冥想 session 调 `send{target:{agent,session}, content}` 向另一会话发事件，目标会话入链并驱动一轮 | `TestSendSymptomCrossSessionDrivesTurn` |
| N1 | 进程在通知入箱/改名/Drain/提交任一点被杀，重启后通知仍被消费，重复恢复不重复处理（R1） | `TestNoticeSymptomCrashNoLoss` |
| N2 | 无人输入时运维通知送达预定目标会话并驱动一轮，不由到达先后决定（R3） | `TestNoticeSymptomOpsWakeupFixedTarget` |
| N3 | 运维通知与用户消息同批进入，各自来源不解析正文即可查（R4） | `TestNoticeSymptomSourceQueryable` |
| N4 | 通报携带上次工作真实现场；无存储清楚降级（R5） | `TestNoticeSymptomSceneRealOrDegraded` |
| N5 | 200/401/403/超时/拒连五态下脚本不误杀健康进程（R6） | `TestHealthSymptomFiveStatesNoKill` |
| N6 | 慢工具在跑时新输入按时进下一次决策；多来源合批回正确责任范围（R7） | `TestScheduleSymptomSlowToolNewInput` |

回滚本变更三锚必红，修复后必绿（F3 双证据）。

## Impact

### 代码

- `assembly/host_tools.go`：`hostSendTool` 重写（参数面/寻址/回执），`hostSendLineageOf` 保留。
- `assembly/host_document.go`：`hostToolIDs` 中 `meditation.DeliverSummaryToolName` 删除；send 装配规则更新。
- `assembly/host.go`：`ErrHostSenderMissing` 的 send 分支删除（用户会址回合分支保留）。
- `runtime/task/*`、`runtime/agent/delegate.go`：reactivate 迁移（settled→活跃+新 Round）、占坑单胜、审计事实。
- `tool/subagent/tool.go`：serve 一轮即 settle 的生命周期延长（可被重入续行）。
- `features/evolution/meditation/deliver.go`：`DeliverSummaryTool` 删除，冥想改经 send。
- `resources/prompts/*.md`：`resume_task` 引用改写为 send 重入。
- `tests/integration/*`、`examples/wechat-bot/*`：症状锚三则 + 既有 send 出网场景迁移（smokeApprovalDocument 的 critical 练刀对象改 send 新形态）。

### 兼容

- 模型面为 BREAKING：依赖"模型调 send 说话到渠道"或 `deliver_summary`/`resume_task` 工具名的部署提示与自动化需改写（CHANGELOG 四要素登记）。
- **清除式归并（用户指令）**：被归并的过时机制代码**删除不并存**（design D15 删除清单 DEL-1–6：旧出网 send 路径、DeliverSummaryTool、resume_task 工具面、空地址 pending、旧改名时序、send 装配校验分支）；级联回卷单使用方伴生抽象；收尾残留 grep 门零残留。
- 持久层：task 状态机新增 reactivate 迁移与审计事实，旧记录只读保全；settle 通知身份（TaskID+Round+Kind+Seq）不变，重激活以新 Round 表达。
- 回退与基线：回退=回滚提交（不设运行时并存开关）；出网投递链（服务回合正文）与既有治理门零改动。

## 非目标

- 不改出网投递链（`hostDeliverFunc`/`deliverObligation`/投递审核三态）——v0.4.0 语义保持。
- 不做远程 agent 通信（`remote-agent-communication` 不动）。
- 不改任务管理工具（list/get/cancel/relaunch）。
- 交互式命令重入归并仅条件波次承载（05 域），不做即显式记录裁剪。
