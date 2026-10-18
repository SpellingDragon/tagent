# 04.6 / 04.7 design-blocked 解阻记录（2026-10-15，主编排者裁决）

> 只读探针核实（git 历史取证+spec 对照）→ 主编排者裁决。实施单以此为合同依据；状态词"不偷改"硬约束照此执行。

## 一、04.6 goal 状态机（探针核实结论＋裁决）

### 核实事实（可复跑：`git show 214f5d9:<path>`）
- **真源修正**：goal/预算 epoch/拒绝账本旧实现位于 git 历史 `214f5d9` 的 `agent/governance/`+`tool/govx/`（56c58eb 整树移除）；**prototype/ 零 goal 命中**（其状态机是任务状态 running/unknown/failed/completed，非 goal）。保留语义权威文字源=`docs/wiki/agent/governance-enforcement.md`。
- **goal 状态词四态**（wiki:48 + ledger.go:183-194 @214f5d9）：`active`（在途占预算）/`achieved`（达成不计在途）/`abandoned`（主动放弃）/`expired`（超期，与放弃区分用于统计归因）。
- **显式转移只有 `active→achieved|abandoned`**（govx.go:52,147 两值枚举）；**`expired` 是派生态**（ExpiresMs 过期即不计 active，Status 不翻转，ledger.go:288,302）。
- **预算 epoch**（budget.go:63,206 + wiki:38）：滑动窗口桶（Window 1h/Bucket 6/MaxHigh 20/MaxMed 200，只计 high/medium）；epoch 随快照持久化，**快照缺失/解析失败/epoch==0 一律不覆盖当前 epoch**（防重启刷限——epoch 存在的全部意义）。
- **拒绝双件**（DenialLedger 记录+governance 事件）；`DenialRecord` 字段含 GoalID（旧 gate.go 六处恒传 ""）；恢复=governance 事件回放（declared/resolved+seq 对齐防 ID 冲突）。
- 执行档 warn/strict、处置 allow/record/hold、Hold≠Denied、预算耗尽恒 denied——与 04.5 已实现面一致。

### 裁决（五冲突点）
1. **事件 subtype 词形**：采纳 B——状态词沿用旧词不改；事件 subtype 用 spec 新词 `goal_declared`（事件词汇属新运行时，spec 已写；"不偷改"约束针对状态词）。旧 `event/metadata.go` 五常量整体去留随 03/05 域事件词汇一次裁决，不在 04.6 局部偷换。
2. **expired 可写性**：沿用旧面——resolve 两值 + expired 派生，不新增可写状态。
3. **warn 档缺 goal 行为**：沿用旧面——warn 记账不 denied（denied 仅 strict）。
4. **registry 终态守卫**：沿用旧面——不加守卫，工具层两值枚举兜住。
5. **"prototype 旧实现"表述**：凡计划/文档以此为据处应改为 `agent/governance@git 214f5d9` + `docs/wiki/agent/governance-enforcement.md`（本记录即修正依据）。

### 新增项（spec 新要求，非沿用）
- denial 渗透携带 goal 上下文（GoalID 填充行为是新增，字段名沿用）。
- GoalRegistry 启动从 governance 事件回放重建接新运行时（机制沿用，接线点新）。
- 治理三件（budget/denial/goal）包位置迁 `plugin/`（纯搬家词不改）。

## 二、04.7 search 检索接口必要性（探针验证结论＋裁决）

### 消费者矩阵结论（元规则二检验）
- **时间/类型轴：必要但薄**——recall 工具协议明文 time/type filters（recall-protocol:12）；既有 `QueryOptions{StartTime,EndTime,EventTypes}` 已有完整语义 → **薄映射**，禁第二套语义。
- **关系轴：自证**——全仓无"按关系过滤检索"独立需求；因果链消费全走 RelationStore 显式遍历；"内容与关系分离"是既有设计原则（wiki:381-387）。**不实施**（Query.Relations 字段保留但显式登记"无消费者，不实施"；不动冻结契约）。
- **融合：必要限两值**——hybrid+keyword_only（自动降级）承载于 recall-hybrid-fusion spec；`StrategyVectorOnly` 自证不写消费者。
- **embedder**：绑定缝/异步/降级离线可做；真实端点+召回质量=09.5 资源门（TestRecallEvaluation，缺门合法 Skip 不结项）。

### 裁决
1. 04.7 最小实施面=时间/类型薄映射+融合两值+embedder 离线缝；关系轴不实施。
2. **QueryEvents 承载断层**（探针附带发现：现码无 QueryEvents 实现而 coverage 标 03.2 已落地）：**归 04.7 实施面落地**（时间/类型/keyword 腿的承载）；coverage.md"03.2 已落地"语义=存储屏障已落地，QueryEvents 读面随 04.7（实施完成时同步修 coverage 注记）。
3. 候选文件撞名 `memory/recall.go`：04.7 独占（03.1 实际落 reference.go/guard.go，名字空闲）。
4. `changes/07-evolution/proposal.md:22` 依赖措辞失真（"04.7 授权…关系检索"）→ 实施 07.1 时不以关系轴为依赖；该句修正为"授权分区读缝（QueryEvents PartitionIDs 注入）"（随下次规划维护修正，不阻塞）。
