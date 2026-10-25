# 02-event-consumers：业务消费者的事件消费完整性

## Why

一级变更要回答的问题是"同一条事实会不会被重复执行、会不会因防重而漏执行"。这个问题最终由八个**业务消费者**暴露：冥想节奏与反思、反思投递（`deliver_summary`）、跨会话投递、投影折叠（压缩/恢复）、模型替换、审批、决策采集、离线训练导出。主规格已分别声明这些能力的义务，但当前缺的是**以正式消费者实际请求为准的逐叶证据**：组件级测试绿不等于正式入口把消费者接上了，更不等于"消费一次、只消费一次"。

本域因此在 00.3 冻结契约之后，对每个消费者先取证、再决定是否修复：没有复现出业务缺陷时，保留现有正确机制并补可证伪的消费者回归；只有证实缺陷才做最小修复。**不把"空引用""`Seq=0`""driver 忙标志"这类静态线索直接定性为缺陷。**

## What Changes

- 七个叶（02.1—02.7），全部以 00.3 的契约冻结与缺陷分派为前置；每叶只做本消费者域内的取证、最小修复与回归。
- 每叶先声明**精确写集**（候选白名单，实施期由 00.3 按证实路径裁剪）、**测试文件**、**完整父 selector**、**具名正锚/负锚**与**实际消费者入口**；缺任一锚不得勾选，不得以部分正负通过或 `design-blocked` 勾整叶。
- 生产入口接线分三叶：根正式装配归 01.5、微信宿主装配归 01.6、CLI 宿主装配归 01.7。本域先以已有正式入口（`tagent.Boot`、`POST /reload`、正式治理+任务包装、`runApprovalCLI`、公开离线 `Export`）做域内先验；需要根接线的域产物以 `ready` 交 01.5 集成，微信审批交 01.6 回验，**交接不记完成**。01.5 不以 02 全部勾选为输入；02 的 checkbox 在集成后退验通过才勾选，复验失败重开对应叶。该顺序是“产物 ready → 集成 → 回验 → 勾选”，不是 01.5/01.6 ↔ 02.x 的互等环。
- 测试文件按真源统一：02.2/02.4 共用 `tests/integration/host/event_integrity_test.go`（同文件单写）；02.5 根审批用 `tests/integration/capabilities/event_integrity_test.go`；02.6 微信审批扩展 `examples/wechat-bot/wechat_test.go`，不新增 `wechat_approval_test.go`。
- G2 replay 身份、G4 生产恢复、G6 导出关联未验时，对应叶保持未勾选；G6 的显式离线 API 是真实消费者，不以新增 CLI 或 Boot 自动导出凑消费者。
- 七项能力写 delta（新增义务或完整 MODIFIED）；两项能力**纯引用既有主规格**、不写 delta，理由见下。
- 发送结果未知时保留动作身份与未确认状态，**不自动重发、不伪报成功**；导出是**显式离线消费**，不要求也不新增 Boot 自动导出，不冒充真实 tokenizer/训练资格。
- 本轮仅落 OpenSpec 文档：不改产品代码、产品测试、CI、依赖与主规格，不提交。

## Capabilities

### New Capabilities

无。全部为既有能力的消费者完整性扩展；不新建事件平台、第二消费状态仓或平行评估系统。

### Modified Capabilities

| 能力 | 叶 | delta 形态 | 新增/改写义务一句话 |
|---|---|---|---|
| `meditation-idle-gating` | 02.1 | 完整 MODIFIED「水位执行语义与让位补位」 | 反思回合未达 completed 终态 SHALL 记推迟而非消费；驱动忙时到达的观察 MUST NOT 推进水位或被计为已消费 |
| `meditation-delivery-tool` | 02.2 | ADDED「投递结果与目标覆盖对账」 | 成功回执只在全部目标提交成功后出现；部分失败/零存活目标 MUST NOT 报成功 |
| `event-sourced-projection` | 02.3 | ADDED「折叠的幂等与不回流」 | 同一材料折叠到同一内容寻址键；已折叠投影不再折叠；重建与折叠产物 MUST NOT 触发自身 |
| `config-hot-reload` | 02.4 | ADDED「模型替换的幂等与在途稳定」 | 同版本重复替换不重复发布/安装且结果可判定；失败替换不安装；在途流保持开始版本，新请求取新生效版本 |
| `approval-channels` | 02.5、02.6 | 完整 MODIFIED「审批消息流抽象」 | 过期请求的回应 MUST 被具名拒绝且不恢复为待执行义务；改参执行 MUST 重新审批；根审批与微信审批各自独立取证 |
| `decision-capture` | 02.7 | ADDED「记录与真实请求/尝试逐项对应」 | 未发生的请求不产生记录；已发生的尝试要么被记录、要么进入显式损失账目；失败尝试不得记为成功 |

### 纯引用能力（不写 delta，避免复制旧义务另造同义 ADDED）

| 能力 | 叶 | 映射的既有条款/场景 | 不写 delta 的理由 |
|---|---|---|---|
| `cross-session-delivery` | 02.2 | 「白名单外目标被拒」「未配置白名单」「目标未启动」「不走 one-shot 回退」「目标正常消费投递」「投递不喂肥目标新鲜度」「与用户输入同批让位」「目标名不存在」「显式目标投递与具名拒绝账本」 | 本域不新增跨会话投递义务，只验证现行实现是否满足既有义务。主规格以 `InjectMessageWithSource` 命名入口、现行实现经 `Run.CommitToSession` 落目标链——这是**规格/实现命名分歧（待证）**，登记为 00.3 裁定门（D8），不以重写条款的方式默认追认实现；纯引用不省略任一既有场景验收面 |
| `offline-training-export` | 02.7 | 「逐样本来源 manifest」「精确关联不等同自动奖励」；「真实本地消费验收」为既有独立待验项，本域不宣称覆盖 | 本域只验**来源关联**（Boot 实际 capture → 公开 `Export` 读取授权会话 → manifest/attempt/sample 逐项关联），并验关联不自动生成 reward；显式离线 API 即真实消费者，不新增 CLI、不自动接 Boot 导出；真实 tokenizer、chat template、collator 与训练资格是既有**独立待验项**，保留为待验，不由本域缩水替代，也不外推训练资格 |

## 边界与依赖

- **依赖（接口常数优先，实现依赖次之）**：00.3 冻结的职责身份、事实引用、运输重试、逻辑消费、外部发送五层对账口径；00.3 对每个证实缺陷的叶级分派。本域不预选 `InjectEvent` 或某个事件类型作为补丁。
- **被依赖方**：01.5（根正式装配汇入本域消费者需求）、01.6（微信宿主装配承载 02.6 回验）、01.7（CLI 宿主装配）；03.2—03.9（身份/决策流矩阵、五控制与重启的绿证）；04.x（合同/归属/变异门）；05.x（每 MR 接线与独立验收）。
- **接口面（本域消费者读取/写入的既有面）**：`meditation.WatermarkGate`/`ReflectionInput`/`EventSink`/`SessionQuery`；`replay.ReplayProjection.FoldByAge`/`RebuildProjectionFromWAL`；`rl.ModelLease`/`LeaseObserver`；`approval.ApprovalChannel`/`ApprovalManager`；`rl.CaptureSink`/`Recorder`/`ChainExporter`；`Run.CommitToSession`/`Run.NoteModelChange`（只读调用，不改签名）。
- **禁止事项**：不编辑根热点 `host.go`、`invoke.go`、`run.go`、`prepare.go`、`tools.go`、`facts.go`、`request.go`、`settle_turn.go` 与 `runtime/task/{wrapper,manager,registry,lifecycle,handoff,cancel,fact,restore,commit}.go`（归 01/01.5）；不新增公共产品 API、持久格式、依赖或测试专用接缝；不建立第二消费状态仓；不因某旧类型/旧符号仍在就接回旧机制（旧符号不是恢复指令）；不以测试手工补接线冒充生产消费者；不要求 Boot 自动导出；不为 G6 新增 CLI；不宣称真实 tokenizer/训练资格或渠道端恰好一次。所有“已有/未见/仅测试调用”的静态线索均称待证，不声称全仓绝对结论。

## Impact

- 实施期触及 `evolution/meditation`、`age_fold.go` + `memory/replay`、`host_reload.go` + `host_model.go` + `rl`、`evolution/governance` + `runtime/task/approval` + 微信审批、`host_capture.go` + `rl` 采集/导出、`tool/subagent/capture.go` + `tool/subagent/builder.go`。其中 `host_reload.go`/`host_capture.go` 与 01 的根热点同属 `package tagent` 编译单元，并发派发须串行或隔离工作树（D4 锁表）。
- 测试新增于 `tests/integration/{evolution,host,history,capabilities,learning}`；02.2/02.4 共用 `tests/integration/host/event_integrity_test.go`（同文件单写），02.5 用 `tests/integration/capabilities/event_integrity_test.go`，02.6 扩展现有 `examples/wechat-bot/wechat_test.go`，不新增 `wechat_approval_test.go`。`tests/integration/` 受 test-mirror 门豁免；微信模块不因新测试扩大 `mirrorExemptFiles`。
- 证据日志落仓库外 `/tmp/event-consumption-integrity/<叶>/run-<i>.json`，每轮独立 `-count=1 -race`；不复用同一份日志充当多次运行，不在单轮内叠 `-count=10`。每轮原始退出码单列 `exits.txt`，不以 `echo`/`tee` 包裹 `go test` 后取管道退出码；父测试与全部子测试的 `run`/`pass` 由既有 checker 扩展逐轮核验，不用 `grep -c` 计数。
- 本轮授权仅为 OpenSpec 文档物化；产品代码、测试、CI、主规格、历史归档与提交分别需要后续授权。

## 规划入口

- 静态决策、白名单、依赖与未决门：[design.md](design.md)
- 七叶任务账目与证据协议：[tasks.md](tasks.md)
- 本域能力 delta：[specs/](specs/)
