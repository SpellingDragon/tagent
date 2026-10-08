# Proposal: docs-objective-review

## Why

tagent 项目文档（`docs/wiki/` 26 篇 + `docs/` 根级 2 篇 + `README.md`，共约 8,400 行）自称"每篇经过逐断言代码校对"，但从未有过**逐块、跨域、带代码佐证的独立客观评审**。用户需要回答四个问题：

1. 文档承诺的**预期特性**全貌是什么？
2. **架构设计**是否自洽、是否有真正缺陷？
3. 是否存在**过度设计**（复杂度超出场景需求）？
4. 从第一性原理出发，当前设计对 **LLM 推理**与 **RL 训练**是否友好？

单会话串行读 8,400 行再交叉分析，上下文易溢出且视角单一；需按域拆分、并发评审、统一框架汇总。

## What Changes

第一阶段为**只读评审**（当时不修改源码与既有文档）；其完成记录不代表第三阶段已实施：

- 按 wiki 域结构拆为 **6 个二级子变更**（D1 记忆存储 / D2 事件插件 prompt / D3 工具任务 / D4 agent 引擎 / D5 平台子系统 / D6 外围运行），每域由一个并发子代理执行；
- 每域评审执行 **2-3 个关键断言的代码抽查**（只读定位佐证，验证"文档↔实现漂移"）；
- 每域产出一份结构统一的域评审报告，落盘 `docs/.dev/20261007-wiki-review-D<N>-<slug>.md`；
- 编排者对域报告做**协议化核查**（勾选真实性/产物盘点/数字溯源/遗漏检测）后，交叉汇总为总报告 `docs/.dev/20261007-wiki-review-summary.md`，并在对话中呈现完整结论。

## What Changes（第二阶段：代码探索，2026-10-07 追加）

第一阶段评审的代码抽查仅每域 2-3 条断言，其结论（尤其是下游探索清单 E1-E12 所依赖的假设：reward 断链、重放非确定、写放大、token 估值偏差、三跳 join 可行性等）深度不足以支撑改造决策。第二阶段将 E1-E12 十二项议题固化为 **6 个代码探索域（X1-X6）**：

- 深读源码主路径，产出机制地图（文件:符号 级）；
- 对第一阶段结论逐条**假设核验**（成立/不成立/证据不足）；
- 允许运行**现有只读验证命令**（指定包 go test / go build / go vet，不改代码不写 spike）；
- 产出探索报告落盘 `docs/.dev/20261007-code-exploration-X<N>-<slug>.md` 与汇总报告；
- 每项探索交付：现状证据、假设核验、改造面草图、候选方案与代价（含“保留现状”）、最小验证方式。

## What Changes（第三阶段：协调优化实施，已规划、未执行）

用户确认六域协同优化及必要的职责调整：先保证采集保真，再交付离线 SFT 样本闭环；真实模型测试须能在本地通过。本轮只修订计划，实施需后续 apply。本阶段记为 I0–I3，避免与第二阶段 W3 探索混淆。

- O1：修正未提交票据/因果游标可见性，串行化同因果域提交，统一回溯的不完整原因。
- O2：将已有 model_override 解析接到实际执行代，完善不支持热更字段的明确拒绝和回执；不增加 DAG、集中任务服务或 paused 状态机。
- O3：统一请求声明快照和完整预算输入，保持投影唯一压缩权；为同步摘要设置局部时限和工程降级。
- O4：按分区定向扫描，减少重复解码及快照编码成本；保持磁盘格式、逐事件提交屏障、TTL、排序与隔离语义。
- O5：可选采集真实 SDK 请求、工具声明、响应和调用归属，提供有界非阻塞录制、丢失计数与可确认封账；不强制 OTel。
- O6：显式授权导出事实/反馈，按稳定调用关联生成工具感知 SFT 样本；本机 tokenizer 验证模板、loss mask、会话级数据分割及缺失清单。
- 各域同步其长期文档及既有测试；共享装配文件由编排者单写集成。允许有证据的局部职责抽取，不按行数重写整个框架。

前轮报告只作假设来源。“无索引即不可关联”“默认 noop 是缺陷”“随机环境不可训练”“复制 trace 即闭环”等旧结论不作为实施依据；修订判据见 design.md D12。

## Capabilities

### 一级编排（仅过程契约）
- `orchestration`：三阶段边界、启动门、依赖、单写归属、证据与完成标准；见 `specs/orchestration/spec.md`。

### 第三阶段产品增量（下沉二级）
| 子变更 | 新增能力 | 既有契约处理 |
|---|---|---|
| O1-fact-consistency | committed-event-attribution | 保持事件不可变/先存后投影，补失败与 partial 细化 |
| O2-runtime-coherence | generation-bound-model-references | 保持 FP/SRC/FILE/RESTART 及调用覆盖，补引用解析与拒绝反馈 |
| O3-request-efficiency | request-budget-accounting | 增加完整请求计量与摘要时限，不授权 provider 二次裁剪 |
| O4-storage-efficiency | partition-local-storage-access | 实现优化，不改 event-segment-store 的成功/排序/遗忘契约 |
| O5-capture-fidelity | decision-capture | 新增 opt-in v2 采集；trajectory_dump 旧模式继续支持 |
| O6-training-export | offline-training-export | 新增 strict 导出；既有 SFT/RL CLI 作为显式 legacy 模式保留并警示，不冒充 strict |

D/X 的历史规格不进入产品主规格。O1–O6 在验收后经提升为独立 change、正常 archive 同步产品增量；不得用删除 specs 或 skip-specs 绕过同步。

## Impact（第三阶段）

变更局限于 tagent：event/plugin、agent 及其子域、modelutil、memory/kv、rl、转换脚本、测试和对应文档。Go 不引入新外部依赖；Python 复用既有 transformers/datasets 生态，仅在离线转换环境加载。新增字段默认关闭或可选，不迁移已有事件库，不提高 TTL，不改外部执行权限。在线 RL、真实微信收发、生产会话操作、换存储引擎及模型训练收益声明均不在本阶段。
