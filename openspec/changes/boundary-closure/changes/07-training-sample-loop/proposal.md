# 训练样本闭环（training-sample-loop）

父变更：boundary-closure（W3；硬前置=06.1a 共享包入 main；01 送达收据与 03 长跑环境仅真实模式增强）。

## Why

评审结论：训练采集已有骨架（请求快照、决策与尝试记录、封存、采集预算），但"独立验收器、奖励向量、样本资格门、可靠发布"仍是后续建设。本域做评审建议的第一步：**先把少量任务做成"实际请求可回读、结果独立验收、验收结论入 rl annex"的完整样本**，再谈扩大采集规模。

## What Changes

1. 样本夹具：复用 06 任务集子集（5 个任务）作为样本源。
2. 请求回读端到端：决策记录的快照引用经 swap store 读回（既有能力），验证端到端可回读并完整重演请求形态（与记录摘要哈希对账，不以"文件存在"代替）。
3. 独立验收器 v1：与执行路径解耦的结果验收（阅卷员式判分：规则型文件/内容/输出匹配，**复用 06 的 `bench` 根包（1.1 产出，可 import 的任务/验收器导出）**），验收结论不采信执行路径自述。
4. 验收结论入 rl annex：验收判定+派生规则版本 v1+产物引用记入 **新 Line 记录种类 `RecordKindAnnex`**（AttemptID 关联、append-only——与“MUST NOT edit”红线一致，不改 AttemptRecord 字段；rl 自有记录族，不碰 event store / RelationStore / feedback-binding）；**写入经 `features/rl/sink.go` 新增导出方法 `RecordAnnex`（append-only，计入 loss/manifest 记账）**；**Line 的 annex payload 落未导出字段 `annex`（F11），经 Reader 方法 `Annex()` 读取，不新增导出字段**；样本身份 join key=`AttemptID`+bench 任务 ID，两者都写入 annex。**奖励向量派生整体延后为显式欠账**：未来 feedback 桥接变更届时对 `feedback-binding`/`offline-training-export` 做 MODIFIED；本域不写 feedback 事件、不派生 reward 向量。
5. 样本三要素（请求回读、独立验收、验收结论入 annex）齐备才可标记 complete；三要素缺一的样本 MUST NOT 进入可发布集，齐备率指标落 run（**分母=全部样本，含 incomplete**）。
6. 样本资格门：**在 Seal 之前评估并把判定写入 annex**（annex 记录先于封存落账；Seal 后 sink 拒收不变）；缺要素样本标 `incomplete` 永久留档，不进可发布集。

## 显式欠账

- **判定结论事实族=L5 桥**：本域验收结论只入 rl annex；agent 感知与冥想消费依赖的通用判定结论事实族属未来 L5 桥变更，本轮不立族、不预写条款。
- **奖励向量派生**：由后续 feedback 桥接变更承接；本域不写 feedback 事件、不派生 reward 向量。

## 边界与依赖

- **依赖**（r2 F28 修正）：**硬前置仅 06.1a**（任务夹具与验收器共享包——scripted 样本不需要 01 收据/03 长跑环境）；01（送达收据）与 03（稳定环境）为**真实模式增强项**（06 门开时的增量采集用），非 scripted 路径前置。
- **被依赖方**：后续 DPO/SFT 管线（post-training-data-loop / online-rl-training）以本域样本为资格输入；奖励向量桥接由后续变更承接。
- **接口面**：不改 `tagent.record.v1` 既有字段语义（只新增 Line 记录种类 `RecordKindAnnex`）；新增样本资格门只在新 annex 上判定；验收器与 06 共享包复用，不改 06 任务集定义。
- **禁止事项**：不动 assembly/、memory/ledger；不实现奖励模型、不写 feedback 事件、不派生 reward 向量（显式欠账）；不扩大采集规模（本域≤10 任务）。

## 既有能力裁决（5.9）

- `offline-training-export`「精确关联不等同自动奖励」：**不用任务终态/trace 存在映射奖励**——本域只做独立验收器对**任务产物**的核验结论，并把结论记入 rl annex；奖励向量派生作为显式欠账留待后续 feedback 桥接变更。
