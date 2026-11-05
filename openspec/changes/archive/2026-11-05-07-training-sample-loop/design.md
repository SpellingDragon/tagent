# 训练样本闭环：技术要点

## 文件清单

- features/rl/capture.go / capture_wiring.go：样本身份与三要素 annex（如需最小扩展）
- **features/rl/record.go、line.go、produce.go**：新增 Line 记录种类 `RecordKindAnnex` 与 `AnnexRecord`（导出类型，≤5 个导出字段；AttemptID 关联、append-only——与“MUST NOT edit”红线一致；不改 AttemptRecord 字段）；**`Line.annex` 字段未导出，经 `Annex()` Reader 方法读取、`NewAnnexLine` 构造（F11；不新增 Line 导出字段）**；**`features/rl/line.go` 的 `MarshalLine`/`ParseLine` 增 `RecordKindAnnex` 分支（编码/解码进 payloads；codec 分支是序列化必需，不改变未导出字段+Reader 方法形态）**
- **features/rl/sink.go**：新增导出方法 `RecordAnnex`（append-only 写入 annex 记录；计入 loss/manifest 记账，不走静默旁路）
- **features/rl/sample/sample_gate.go（新增，子包）**：资格门（三要素判定与齐备率）
- **features/rl/sample/acceptance.go（新增，子包；复用 06 的 `bench` 根包——1.1 产出的可 import 任务/验收器导出）**：规则验收器
- **features/rl/sample/*_test.go**：资格门/验收器测试（`features/rl` 根包保持 data-and-ports）
- features/rl/sample_loop_test.go：端到端样本测试（scripted 模式）
- bench/tasks 子集引用（不复制夹具，引 06 的任务定义）
- **golden/runtime/architecture.json**：`features/rl/sample` 登记 packages+edges；**新增 `features/rl/sample→bench` 边**（复用 bench 根包；不是 features/rl→…，也不是 bench/acceptance 子包）

## 既有能力裁决（5.9）

- `offline-training-export`「精确关联不等同自动奖励」：**不用任务终态/trace 存在映射奖励**——本域只做独立验收器对**任务产物**的核验结论，并把结论记入 rl annex；奖励向量派生整体延后为显式欠账，不碰该能力条款。
- `decision-capture`/`bounded-trajectory-capture`：本域消费其快照/预算能力（读回+swap），不改其条款；只新增 `RecordKindAnnex`、不改 `tagent.record.v1` 既有字段。

## 关键决策

- 样本身份：一个任务的一次完整执行=一个样本（run-id+task-id+attempt）；**join key=`AttemptID`+bench 任务 ID**，两者写入 annex；三要素按样本身份聚合。
- **annex 形态（r6 F5 定版；F11 收口）**：`RecordKindAnnex` 是 Line 的新记录种类（与 episode/decision/attempt 并列），payload 以 `AttemptID` 关联、append-only；**不改 AttemptRecord 字段**。**payload 承载：`Line.annex` 未导出（不新增 Line 导出字段），读侧只经 `Annex()` Reader 方法；`AnnexRecord` 为导出类型（≤5 导出字段），在 `golden/runtime/architecture.json` 的 `concept_bindings` 绑定 `training-sample-loop` 的 concept**。**序列化：`MarshalLine`/`ParseLine` 增 `RecordKindAnnex` 分支（编码进 payloads；未导出字段+Reader 方法不变）；`TestAnnexRoundtrip` 断言 `Marshal→Parse→Annex()` 内容一致**。
- 回读证明：读回的快照重组请求并断言与记录的请求摘要一致（哈希对账），非"文件存在即回读"。
- 独立验收与验收结论 annex（r5 定版：**不写 feedback 事件、不派生 reward 向量**）：验收器只看任务定义与终态产物、不读执行自评；**验收判定+派生规则版本 v1+产物引用**记入 rl 自有记录族的 annex（新 Line 记录种类 `RecordKindAnnex`——AttemptID 关联、append-only，不改 AttemptRecord 字段；rl 有自己的写入面，不碰 event store / RelationStore / feedback-binding 条款）；验收失败也是有效样本（负样本），判定如实记 fail。
- **奖励向量派生=显式欠账**：本域不派生 reward 向量、不写 feedback 事件；“验收结论→feedback 事件→训练导出”的桥接留给后续 feedback 桥接变更（届时对 `feedback-binding`/`offline-training-export` 做 MODIFIED）；07 触碰集维持 features/rl/**（产出端含 `features/rl/sink.go` 的 `RecordAnnex`；+bench 根包依赖与 golden 登记）。
- **判定结论事实族=L5 桥（显式欠账，F3/F5）**：本域的验收结论只落 rl annex（训练侧），不立通用判定结论事实族；agent 感知与冥想消费依赖该族，属未来 L5 桥变更。本轮不预写条款、不建消费面。
- **资格门时序（F16 定版）**：资格门在 **Seal 之前**评估并把判定写入 annex（annex 记录先于封存落账；Seal 后 sink 拒收不变）；缺要素样本标 `incomplete` 永久留档不进可发布集。**流程=5 任务→gate 评估+annex 写入→seal→齐备率**（分母=全部样本，含 incomplete）。
- scripted 优先：端到端测试用 scripted 模型（零真实配额）；真实样本仅当 06 门开时增量采集。

## 风险与回退

- 快照读回在 swap store 深冷区延迟：批量预读；延迟数字入账。
- 回退：annex 为 append-only（只增不改），旧记录不受影响；域独立可 revert。
