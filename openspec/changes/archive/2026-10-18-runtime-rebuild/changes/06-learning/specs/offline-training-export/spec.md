# offline-training-export（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/offline-training-export/spec.md` 的 delta（主 spec **只读**）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**；未列出者视为继承主 spec 现行有效约束，**不默认「无 delta = 继承」**。

## Purpose

定义授权导出、版本化 reward 派生、逐样本来源 manifest、**训练用途资格矩阵（DPO / 采样 RL）** 与**真实 SFT 消费**。本波在既有 strict 转换语义上补充：版本化 reward、真实 tokenizer/chat template/collator 消费（**不以常量替身结项**）、训练用途资格合同、真实资源**待授权**的 `design-blocked` 归属。

## MODIFIED Requirements

### Requirement: 真实本地消费验收
交付 SHALL 包含真实模型工具往返到本地真实tokenizer和collator batch的证据；缺凭据、模板不支持或SKIP不能计完成，不声称权重训练收益。**本波新增**：消费 MUST 使用**真实 tokenizer、chat template 与 collator**；MUST NOT 以自造返回常量的消费者结项；真实制品/入口**待授权**，未授权时该任务 MUST 保持 `design-blocked`，MUST NOT 标为完成。场景中的“隔离会话”指**可观测条件**（两个各自独立 session 身份），MUST NOT 被读作对某历史类/符号（如旧 `Runner`/“隔离 session”类型名）的硬要求，亦 MUST NOT 以此复活旧代码。

#### Scenario: 最小可用数据集
- **WHEN** 两个相互隔离的会话（各自独立 session 身份）完成真实工具使用并导出
- **THEN** 必需用例PASS、目标token非空、labels/mask一致、分割无泄漏、无生产私密数据。

#### Scenario: 常量替身被判失败（失败，本波新增）
- **WHEN** 消费者以自造常量代替真实 tokenizer/chat template/collator
- **THEN** 验收 MUST 显式失败，MUST NOT 计为完成。

#### Scenario: 真实资源未授权（失败，本波新增）
- **WHEN** 真实 tokenizer/chat template/制品/入口未获授权
- **THEN** 任务状态 MUST 为 `design-blocked`，接口面在 design 中明确，MUST NOT 声称 Code 实际 PASS。

## ADDED Requirements

### Requirement: 版本化 reward 派生
导出 SHALL 从 feedback 事件**派生版本化 reward**；reward 记录 SHALL 携带**派生规则版本**与来源 feedback 引用；MUST NOT 自动把任务终态映射为奖励，**feedback / reward 分开建模**。

#### Scenario: 延迟反馈到达（正常）
- **WHEN** 某 decision 在首次导出后才收到反馈
- **THEN** 重跑导出 SHALL 产出携带新 reward 版本与来源引用的记录，历史 reward MUST NOT 被就地改写。

#### Scenario: 无证据不臆造奖励（失败）
- **WHEN** 某 decision 无关联 feedback
- **THEN** reward MUST 标 `unrated`，MUST NOT 由任务终态臆造。

#### Scenario: 并发导出同分区（并发）
- **WHEN** 两个导出任务并发扫描同一授权分区
- **THEN** 各自产出 MUST NOT 覆盖已有数据集，冲突 SHALL 显式报告。

#### Scenario: 重启后 reward 版本稳定（重启）
- **WHEN** 导出进程重启后重读已导出 reward
- **THEN** 派生规则版本 MUST 与写入时一致，不被重解释。

### Requirement: 逐样本来源 manifest
导出 SHALL 为每条样本生成**来源 manifest**（capture/export id、episode、decision/attempt、版本、reward 版本）；strict 样本 SHALL 核验 manifest 一致性；缺失、格式错误或 legacy 未知质量 MUST 进入拒绝/状态清单。

#### Scenario: manifest 可追溯（正常）
- **WHEN** 一条样本被导出
- **THEN** manifest MUST 可回溯至其 capture 与 episode/decision/attempt，且版本字段完整。

#### Scenario: manifest 缺失或不一致（失败）
- **WHEN** 样本缺 manifest 或与 capture 不一致
- **THEN** MUST 进入拒绝清单并给出原因，MUST NOT 静默导出。

#### Scenario: 因果分组防泄漏（并发）
- **WHEN** 存在同根 session 的子调用或共享历史
- **THEN** manifest 分组 MUST 保证同组不跨 train/test，MUST NOT 因并发出错而泄漏。

#### Scenario: 重启后 manifest 稳定（重启）
- **WHEN** 导出进程重启后重读 manifest
- **THEN** 分组与版本 MUST 与写入时一致。

### Requirement: 训练用途资格矩阵（DPO / 采样 RL）

除 SFT 外，导出 SHALL 为每个**训练用途**（至少 **DPO** 与**采样 RL**）给出**可判定资格合同**，每行 SHALL 含：**用途** / 所需**真实证据** / 结论（三态之**恰**一：`unknown` = 真正缺证据；`ineligible` = 已知不满足该用途必要条件；`qualified` = 已确定消费者且条件齐备）/ **缺失原因**。判定 MUST NOT 由 SFT 资格外推：**SFT 合格推不出 DPO 或采样 RL 合格**；本域 MUST NOT 接入训练引擎（PPO/GRPO/DPO 训练器），只产**资格合同**。

**DPO**：资格 SHALL 要求**同输入候选对**（同一 prompt 的多个候选响应）与**偏好依据**（选择依据、偏好标签来源）、其**版本**与**分组**（同根 session/episode 不跨组）；缺候选对/偏好依据/版本时：真正缺证据 SHALL 标 `unknown`，已知不满足该用途必要条件 SHALL 标 `ineligible`，两者均具名缺失；仅当已确定消费者且条件齐备时方 SHALL 标 `qualified`。

**采样 RL**：资格 SHALL 取决于**实际算法与行为策略**及**采样证据**（行为策略标识、采样温度/分布、**必要时**的概率或 logprobs 来源）；MUST NOT 笼统断言「所有采样 RL 算法都必须 logprobs」；无消费者与能力证据时：真正缺证据 SHALL 标 `unknown`，已知不满足该用途必要条件 SHALL 标 `ineligible`，**无实际采样算法消费者时 MUST NOT 标 `qualified`**。

#### Scenario: DPO 缺候选对标 unknown
- **WHEN** 数据仅含单一响应、无同输入候选对或偏好依据
- **THEN** DPO 资格：真正缺证据 SHALL 标 `unknown`，已知不满足该用途必要条件 SHALL 标 `ineligible`，具名缺失候选对/偏好依据；MUST NOT 因 SFT 合格而外推为 DPO 合格

#### Scenario: 采样 RL 资格取决于真实算法
- **WHEN** 评估采样 RL 资格
- **THEN** SHALL 依据实际算法与行为策略/采样证据判定；算法不需 logprobs 时 MUST NOT 因缺 logprobs 判不合格；无实际采样算法消费者时标 `unknown`（真正缺证据）或 `ineligible`（已知不满足必要条件），MUST NOT 标 `qualified`

#### Scenario: 用途矩阵四要素齐备（正常）
- **WHEN** 审查用途矩阵
- **THEN** 每个用途行 SHALL 含 用途/所需真实证据/结论（三态之恰一）/缺失原因，结论为三态之一（`qualified` 仅当已确定消费者且条件齐备），MUST NOT 以数量宣称全用途 `qualified`
