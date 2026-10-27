# 架构减法：删除重复权威，兑现跨层语义

## 症状（验收矩阵第一行——四条架构不变量直译 + 六项减法三问）

| 不变量（判定锚=具名测试，红即违例） | 直译测试 |
|---|---|
| 已确认提交的事实，重启前后具有相同语义 | `TestFactFidelity`（全字段/幂等/冲突/戳记边界）+ 减法后重装配全量比对 |
| 所有模型请求在压缩与恢复后仍保持协议合法 | `TestRequestFidelity` 三场景 + `ProtocolGate` 负锚（含冷启动） |
| 每项已接受的任务和交付责任都有完成证据或可恢复的未结状态 | 3.2/3.3 家族（mailbox 统一、pendingDeliveries 持久、Stop 保留） |
| 每份可变状态都有唯一所有者，他人经声明接口访问 | 读写点收敛断言（`restoreCallPairing` 等跨读改经 owner 接口） |

每批交付必答三问（机械可查部分进报告）：**删掉了哪份状态/路径（diff 可查）· 剩余责任由谁承担（批报告模板）· 原有失败与恢复验收仍成立（全量测试+定点破坏双证）**。

## Why

统一事件入口已建立，但消费者可靠性责任未分层；"事实唯一真源"未成为实现约束（EventBody/投影/模型消息多处手工转换）；三种"完成"责任衔接不全；Run/Host 职责过载。**最有价值的是删除重复权威，不是删除功能**：同一条终态通知涉及 outbox/mailbox/续跑队列——"还有没有下一轮工作"必须由一处权威判断。

## What Changes（六项减法 + 一项呈现删除）

- **(a) 提交单入口**：删 `commitFact` 吞错双入口；无返回通道调用点改签名（`age_fold.go:217`/`tools.go:111`）；失败观测归调用点，单入口只兜底。
- **(b) 历史路径统一**：删 `history`/`replays` 双路径与 `MaxTokens<=0` 一票否决（三处：facts.go:310、run.go:1240/1264、request.go:283）；恢复/读取统一，预算只影响呈现；fold 阈值与注册门拆开（注册门删除，fold 用缺省窗口）。
- **(c) 完整事件为恢复源**：EventBody 索引字段改派生（删 Summary/Content 双份）；**legacy 反造读删除**（no legacy, pure and clean——旧格式不迁移、冷启动重建，kv.json 先例）。
- **(d) 通知消费统一（重定基线 2026-10-27）**：`retained/firing/rearm` 旧状态**已由 fidelity 3.2 G6b + F3b 双驱动修复删除**（`grep -cE "retained|firing|rearm" settle_turn.go` = 0，pump→drain→driveSeat 全走 mailbox 账户）；本叶降格为**核验+关闭**：锚复跑+结构证据+边界登记（跨会话串行 pump 为已知边界，减法变更不加机制，并行化另行立案）。
- **(e) 快照单源**：删 `Run.snapshots`（消费者迁到 Capture 读回，前置=快照持久可读）；**Seal 接口不动**（rl.Capture 契约，另行评估）。
- **(f) 插件词汇精简**：删 Phase 保 Port（编译期窄接口）；observePhase 门控/日志键改 Port。
- **(g) 删呈现决策**：删 Full/Ticket/Summary 永久决策与 `recordLocked` 原地改写（Q18：Content 双角色拆解——Content=事实文本不可变，呈现 `viewOf` 现算）；"过去看到什么"由请求快照记录、"现在怎么呈现"由事实+配置+预算计算；noticeCard 预算下限漏洞同批。
- **① Observer 责任分层**：PostCommitObserver 分两类（Observer-only / Driver），关键义务（续跑/交付）与纯观察（遥测/日志）失败语义分层。

## Capabilities

### Modified Capabilities

- `persistent-event-loop`：提交单入口、戳记边界、历史读取统一。
- `memory-retrieval`：完整事件恢复源、索引派生。
- `delivery-plugin`/`execution-resource-lifecycle`：责任单一化。
- `context-assembly`：呈现无长期决策状态、协议合法。

## Impact

- 代码：`facts.go`、`run.go`、`request.go`、`memory/event_body.go`、`memory/replay/`、`settle_turn.go`、`runtime/state/mailbox.go`、`api/plugin/`、`rl/capture` 读回、`tools.go`、`age_fold.go`。
- **启动门（已满足，2026-10-27）**：fidelity-and-recovery 已 17/17 归档（`3c2cf89`，archive/2026-10-27-fidelity-and-recovery/）——G4 地址键、3.2 mailbox 形态、1.2 调用事实全部入 main。
- **派发模型（用户裁定：充分并发，2026-10-27 二次确认）**：文件簇零相交三轨并发（波1 A1∥B∥C1 → 波2 A2∥C2 → 波3 A3）+ 编排者集成验证轨；R21 编译互踩以「包内锚窗口验/host 级锚集成验/异文件编译错轮询重跑」三约缓解，见 design D0 与 ORCHESTRATION 2.3。
- 三批顺序（用户裁定）：批 1 统一提交/事件编码/历史读取 → 批 2 通知消费/快照生命周期/Observer 分层 → 批 3 词汇精简/呈现删除。

## 必答一问

"本案完成后，'重复权威'还会存在吗？"——不会：每批三问验收（删了什么/责任归谁/验收仍成立），四条不变量各有判定锚测试红转绿，F3 回滚即红。诚实边界：(e) 的 Seal 语义与 (g) 的快照前提依赖前置完成，未完成则该批停在就绪门外。
