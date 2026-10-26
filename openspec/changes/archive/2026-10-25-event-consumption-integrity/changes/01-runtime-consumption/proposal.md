# 01-runtime-consumption 子变更提案

父变更：`event-consumption-integrity`。本域负责输入、任务、消费、恢复、会话事实持久化、外部执行资源生命周期与发送责任；依赖 `00.3` 的冻结契约，`01.3` 依赖 `03.1`，`01.9` 依赖 `01.8`，`01.10a` 依赖 `00.3`、`01.10b` 依赖 `01.10a` + `00.3`；`01.5` 依赖 `02.1`—`02.5`、`02.7` 的可集成根接线产物 ready（不要求其最终 checkbox；`02` 最终验收在 `01.5` 之后；`02.6` 微信审批经 `01.6` 回验）；`01.6`/`01.7` 只依赖 `01.4`。本域现为 11 叶（01.1—01.10b）：`01.8`/`01.9`/`01.10` 为计划回写③新增，`01.10` 于计划回写④拆为 `01.10a`/`01.10b`；`01.2`/`01.3` 因补差回退为未勾选。

## Why

通知身份、事实引用、原调用责任、逻辑消费与外部发送当前没有逐身份对账。静态面已确认两条不一致路径：`host.go:fireNoticeTurn` 为一次终态通知创建新 invocation，先提交携带正文的 Feedback，再把同一正文交给 `InvokeRequest.Input`；`settle_turn.go` 的 `firing` 属于整个驱动器，忙时 `ObserveCommit` 直接返回。前者可能让同一通知在一次 decision 中被重复生成材料或重复消费，后者可能让忙时到达的通知永久漏处理。二者都必须先由 `00.2` 行为复现，再由本域按证实路径修复。

恢复面已有任务提交/恢复、mailbox `Claim`/`Consume` 与整封隔离，但缺少“通知消费位置连续、不跨空洞”的明确条款；发送面已有失败回执，但缺少“动作身份、已确认不重发、结果未知不自动重发”的完整契约。若直接以“去重”修，容易把合法多消费者读取或运输重试误伤为业务重复。

## What Changes

- `01.1` 通知身份：经 `Boot`/`Host.Invoke` → 声明工具/子代理 → `runtime/task` 正式通知生产 → `Run.deliver` → `facts.go` 现有消费者验证工具/子代理通知可达原 invocation；同身份重送幂等、异内容冲突、多 progress 按 `Seq` 有序、Handoff 前后目标保真。
- `01.2` 消费恢复：既有 task 提交/恢复 + mailbox `Claim`/`Consume`；领取不提前确认、连续消费位置不跨空洞、失败保留义务、正式独立进程恢复、损坏 slot 整封隔离；不建立第二套消费状态仓，不复活旧 inbox。补差：`Consume` 成功与 `Backflow` 交接之间退出时交接结果必须可恢复，证明可恢复后再确认消费；不得仅挪动确认调用，不得恢复正文双路输入。
- `01.3` 任务续转：忙/闲续转、两任务与同 Host 多会话、后到通知不丢；同一通知不重复生成用户输入或重复逻辑消费，正文可在历史中合法重现；不新建 invocation、不重造责任范围。补差：未配置投影/零窗口/有窗口三态下通知材料均可达，移除正文 `Input` 后无投影配置的合法路径不能断通知输入。
- `01.4` 发送回执：合法动作均送出、已确认动作重入不重发、确认丢失保留未知且不自动重发、显式 `send` 与最终答复动作身份分离；正式 sender 与声明式 `send` 工具。
- `01.5` 根正式装配：根正式入口（`host.go`/`run.go`/`prepare.go`/`tools.go`/`facts.go`/`request.go`/`invoke.go`）汇入 `02` 域根接线需求；缺必要端口时具名拒绝，不由测试补接线；不因导出符号、`Seq=0` 或零用例默认接回。
- `01.6` 微信宿主装配：微信正式入口 `newWechatApp → Boot → bindWechat → bindChannel → registerHandlers`；缺必要身份时具名拒绝。
- `01.7` CLI 宿主装配：CLI 正式入口 `run`；缺必要凭据时具名拒绝，不用 `cli.NewCLI` 或接口声明冒充生产入口。
- `01.8` 会话事实持久化与实时召回：正式回合产生的对话与工具结果当次持久化并经正式 recall 查到原文；存储失败不返回伪成功、索引失败留可恢复欠账、新事实入索引不使既有可达记录不可达。实施前先归因当前持久化真实载体（durable chain / 框架存储插件 / ledger），再最小改动。
- `01.9` 冷启动恢复与后续使用：正常关闭与进程中断后重开同一存储，恢复原文、invocation/task 归属与工具 call/result 配对；恢复后下一个实际请求必须使用恢复材料，而非仅恢复账本。
- `01.10a` 执行资源查询面生命周期：在 `tool/exec` 包内验证五态查询面——调用已返回不证明进程结束、同键复用 `Capture` 可见后续输出、进程真正退出及退出码（`Release` 的 `exit=N`）、释放确认或状态具名未知（`retained`/`ErrResourceUnknown`）；负锚证明稳定不等于成功、部分输出不等于完成。不改根装配。
- `01.10b` 执行状态事件生产与通知接线：返回后持续跟踪（续租不随返回终止）、状态变化生成通知进入既有消费链（sink/Backflow）、生产 Sweep 挂钩与 `Host.Close` 触达 exec 资源（修复 close 后泄漏）、重启接管或具名未确认；需要新 public API 时先列最小面再动，超面停报。
- 复用现有 mailbox、任务提交与恢复材料；是否需要新增持久事实，由失败窗口和正式恢复消费者证明，不预建第二消费状态仓；不默认新增 public API 或持久化格式，确需新增须经 `00.3` 裁定并另获单独实施授权，未授权保持未勾选。

## Capabilities

### New Capabilities

- `session-fact-lifecycle`：会话事实的提交即持久化、当次召回、索引失败可恢复与新事实可达性底线（01.8/01.9）。delta 见 `specs/session-fact-lifecycle/spec.md`。
- `execution-resource-lifecycle`：外部执行资源的五态查询面、返回后持续跟踪、租约/取消、生产挂钩、重启接管与完成判定（01.10a/01.10b）。delta 见 `specs/execution-resource-lifecycle/spec.md`（一份 spec 两组 Requirement，按两叶分批实施）。

### Modified Capabilities

- `persistent-event-loop`：完整修改「task_settled 作为一等事件触发回收 turn」，增加“同一通知不重复生成输入或重复逻辑消费、忙时保留待办、多任务多会话各自续转”；正文可在会话历史中合法重现，重复判定按通知/事实/动作身份与因果归属；新增「通知消费位置连续且不跨空洞」与「正式宿主缺必要端口或身份时具名拒绝」。delta 见 `specs/persistent-event-loop/spec.md`。
- `async-task-execution`：新增「通知义务存活至逻辑消费确认」，明确运输接收/发送成功/任务终态均不等于消费完成，Handoff 只改未来通知。delta 见 `specs/async-task-execution/spec.md`。
- `telemetry-channel`：完整修改「消费状态决定遥测退出时点」，把“已消费”收紧为逻辑消费确认，明确运输接收与 claim 不得当作消费确认、不得据此降级；“已外显”仍按既有 outputCh 投递宿主口径，发送动作的去重、未知与重试由 `delivery-dispatch-receipts` 规定。delta 见 `specs/telemetry-channel/spec.md`。
- `delivery-dispatch-receipts`：新增「发送动作身份与未确认结果」，区分显式 `send`、最终答复、文件与分片动作身份；已确认不重发、未知不自动重发、显式重试保留关联。delta 见 `specs/delivery-dispatch-receipts/spec.md`。

纯引用既有要求不复制为新增要求；`event-metadata-contract` 的身份分层与 `committed-event-attribution` 的消费归属由 `00` 域冻结，本域只引用，不另建同义要求。

## 边界与依赖

### 1. 依赖

- `00.3 → 01.1`：身份分层、五轴对账与“原 invocation/版本时点”裁定是 `01.1` 的前置。这是**接口常数依赖**：通知四元、事实键、消费归属定义必须冻结；未冻结前不得自选补丁类型。
- `01.1 → 01.2`：消费恢复复用 `01.1` 的通知身份与幂等语义。
- `00.3 → 01.8 → 01.9`：会话事实持久化以 `00.3` 冻结的事实引用与归属为前置；冷启动恢复复用 `01.8` 的持久载体与召回语义。
- `00.3 → 01.10a`：执行资源查询面生命周期以 `00.3` 的事实/动作身份与逻辑消费边界为前置；查询面直接映射既有 `Manager` 能力，不改根装配。
- `01.10a + 00.3 → 01.10b`：状态事件生产与接线在查询面判据冻结后开始；续租、释放与后续通知消费复用既有链，不新建平行状态仓；新增 public API 按 `00.3` 流程先列最小面再动。
- `01.2 + 03.1 → 01.3`：续转测试复用 `01.2` 的恢复语义与 `03.1` 的脚本 provider/记录 sender/屏障基座。`03.1` 是**实现依赖**，未就绪时 `01.3` blocked，不自建第二套基座。
- `01.3 → 01.4`：发送动作身份以续转后的责任 invocation 为锚，未确认状态必须能回到同一责任范围。
- `01.4 + 02.1—02.5、02.7 域产物 ready → 01.5`：根正式装配汇入其他域的根接线需求（微信装配归 `01.6`、CLI 装配归 `01.7`）；按根 ORCHESTRATION 的 ready/verified 两阶段语义，`01.5` 的前置是 `02` 可集成根接线产物 ready，不等待 `02` 最终 checkbox；`01.5` 先完成根正式装配与已就绪端口验收，`02` 的最终验收在其后执行。未 ready 端口具名 blocked，不得用测试补线冒充完整接线。
- `01.4 → 01.6`：微信宿主装配复用 `01.4` 的发送动作与已确认语义；缺身份具名拒绝。
- `01.4 → 01.7`：CLI 宿主装配复用 `01.4` 的发送动作与已确认语义；缺凭据具名拒绝。
- `02.6` 微信审批以 `01.6` 为最终回验前置（`verify_after=01.6`）；`01.6` 不依赖 `02.6`，方向单向、不构成环。
- 外部接口常数：`task.Notice` 四元、`event.EventID`、`MessageSender`、`Boot` 选项、`hostSendTool`/`send` 工具、既有 task 提交/恢复与 mailbox `Claim`/`Consume` 语义。上述为**接口常数**；具体实现文件在 `01.1`—`01.7` 的候选白名单内。

### 2. 被依赖方

- `02.1`—`02.5`、`02.7`：读取 `01.1` 的通知身份与 `01.3` 的续转语义，避免各消费者各自解释“已消费”；其根接线产物 ready 供 `01.5` 装配，最终验收在 `01.5` 之后。`02.6` 微信审批以 `01.6` 为最终回验前置。
- `03.2`/`03.3`：读取 `01.5` 的根正式装配；`03.4` 读取 `01.6` 的微信宿主装配；`03.9` 重启测试依赖 `01.2` 的恢复语义。
- `04.4`：变异测试使用 `01.5`、`01.6`、`01.7`、`03.2`、`03.3`、`03.9` 的正式测试作为杀死判据。
- `05.1`：CI 接线读取 `01.5`/`01.6`/`01.7` 的正式入口与 `01` 域的十轮证据格式。

### 3. 接口面

- `01.1`：`task.Notice` 的 `(TaskID, Round, Kind, Seq)`、`Notice.FactKey()`、`Boot`/`Host.Invoke`、`Run.deliver`、`facts.go` 的 `sink`/`bookNotice`/`noticeProjection`；冲突必须具名。
- `01.2`：既有 task 提交/恢复、mailbox `Claim`/`Consume`、消费位置、恢复材料；不新增第二套消费状态仓。
- `01.3`：`Boot` 正式入口、`Host.Invoke`、`SettleDriver`、`fireNoticeTurn`；同一通知不重复生成用户输入或重复逻辑消费，正文可合法重现。
- `01.4`：`MessageSender`、`deliverFresh`、声明式 `send` 工具；动作身份与确认/未知状态。
- `01.5`：根 `Boot`；缺必要端口时具名拒绝。
- `01.6`：微信 `main → newWechatApp → Boot → bindWechat → bindChannel → registerHandlers`；缺必要身份时具名拒绝。
- `01.7`：CLI `run`；缺必要凭据时具名拒绝。

### 4. 禁止事项

- 禁止接回历史实现或零引用结构：`state.SendFace` 当前仅测试构造，不得为回执恢复；`rl.Recorder` 仅在配置启用时使用，不默认恢复；不恢复旧投影恢复路径。
- 禁止建立第二套消费状态仓、第二套 provider/sender/barrier、第二套任务通知身份。
- 禁止把无生产调用的 `MemoryRegistry.IssueNotice` 或 `IssueNotice`→`NoticeSequencer` 链接回为通知生产；`NoticeSequencer` 仅保留恢复路径的 `Observe`。
- 禁止用文本相似度、正文前缀、正文出现次数或“模型说重复”作为去重/消费判定；正文可在会话历史中合法再次出现。
- 禁止默认新增 public API 或持久化格式；确需新增须由 `00.3` 列明消费者、失败窗口、恢复路径与回退并裁定，另获单独实施授权；未裁定/未授权不实施。`01.10b` 的最小 API 面先在该叶实施单列明，超最小面停报。
- 禁止用新建 invocation 绕过“原 invocation 下一步 decision”与“当前代绑定”的裁定；`00.3` 未裁决前 `01.1`/`01.3` 保持 blocked。
- 禁止在测试里手工补接线使正式入口通过；正式入口缺端口必须具名失败。
- 禁止把运输接收、领取（`Claim`）、发送成功或 `Host.Close` 返回 nil 当作逻辑消费完成。

## Impact

- 未来实施涉及根运行时、任务/状态、宿主接线与微信/CLI 两个独立模块；逐叶精确文件候选白名单见 `design.md` D4。
- `01.1` 新增 `tests/integration/execution/event_integrity_test.go`；`01.2` 新增 `tests/integration/history/event_integrity_test.go`；`01.3`/`01.4`/`01.5` 扩展 `tests/integration/host/event_integrity_test.go`（与 `00.2` 同文件，单写串行）；`01.6` 扩展微信 `smoke_harness_test.go`（`smoke` 标签）；`01.7` 扩展 CLI `examples/cli-host/main_test.go`；`01.8`/`01.9` 扩展 history 文件（与 01.2 单写串行），`01.10a` 新增 `tool/exec/event_integrity_test.go` 包内测试，`01.10b` 扩展 execution 文件（与 01.1 单写串行）。
- `01.5` 只要求 `02.1`—`02.5`、`02.7` 的可集成根接线产物 ready，不等待其最终 checkbox；`02` 的最终验收在 `01.5` 之后执行，不构成反向门；`02.6` 微信审批只经 `01.6` 回验，不并入 `01.5` 前置。
- 每叶父 selector 同时跑全部子场景；稳定性用 10 次独立 `-count=1` 执行、逐轮日志分文件并保留原始退出码，不用 `-count=10` 混入单轮证据；原始日志写 `${TMPDIR:-/tmp}/event-consumption-integrity/<leaf>/`（CI 为 `${{ runner.temp }}/event-consumption-integrity/<leaf>/`），change 内 `evidence/*.json|md` 只留摘要与 `log_ref`；每轮全部具名 `run`/`pass` 终局由 `04.2` 正式证据检查器核对。
- 本轮只物化规划，不运行产品测试、不改 CI、不提交/推送/归档。

## 本阶段不实施

本文件只物化规划。所有产品叶任务保持未勾选；不运行产品测试、不新增产品测试、不改 CI、不安装依赖、不提交、推送、归档或访问真实模型/微信。独立 OpenSpec 校验由主代理统一执行。
