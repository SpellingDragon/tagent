# trajectory-recording（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/trajectory-recording/spec.md` 的 delta（主 spec **只读**）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**；未列出者视为继承主 spec 现行有效约束，**不默认「无 delta = 继承」**。

## Purpose

定义轨迹与训练记录的落盘格式、版本谱系与生产点。本波将既有 JSONL 记录升格为**显式版本化**记录，并把 episode/decision/attempt 的字段解释前移到**实际生产点**；旧 v2 结构与既有转换器**只读导入**，新格式 MUST NOT 被旧结构绑架。

## MODIFIED Requirements

### Requirement: JSONL trajectory format

The system SHALL persist trajectory records as JSONL files at `{trajectory_dir}/{session_id}.jsonl`. Each line SHALL be a JSON object containing the following fields: `timestamp`, `session_id`, `user_id`, `batch_index`, `llm_call.request.messages`, `llm_call.request.model`, `llm_call.request.generation_config`, `llm_call.response.choices`, `llm_call.response.usage`, `llm_call.response.finish_reason`, `llm_call.response.error`, `metadata.duration_ms`, `metadata.model_endpoint`. **本波新增**：每条记录 SHALL 携带**显式 schema 版本号**；episode/decision/attempt 及其字段解释 SHALL 在**实际生产点**确定（02.5 装配 / 02.4 模型终局），MUST NOT 由采集侧临时推断；未知谱系 MUST 显式标注，MUST NOT 静默归类。**兼容范围界定**：上述 `{trajectory_dir}/{session_id}.jsonl` 路径与字段清单**只界定旧格式兼容范围**，MUST NOT 被当作新格式的硬约束；**新记录 schema 由 00.3 审定**，其**最小真实生产字段**（请求/响应/usage/工具调用/时间等）MUST NOT 因「不硬套旧结构」而丢失。

#### Scenario: Single LLM call produces one JSONL line

- **WHEN** an LLM call completes (either success or error)
- **THEN** exactly one JSON line SHALL be appended to the session's JSONL file
- **AND** the line SHALL contain the full request messages and response choices

#### Scenario: Tool calls are recorded

- **WHEN** the LLM response contains tool calls
- **THEN** the `llm_call.response.choices` field SHALL include the complete tool call definitions (name, arguments, id)

#### Scenario: 每行携带显式版本（正常，本波新增）
- **WHEN** 任一记录被写入 JSONL
- **THEN** 该行 MUST 含显式 schema 版本字段，读取方 MUST 依据版本选择解析路径。

#### Scenario: 未知谱系显式（失败，本波新增）
- **WHEN** 读到版本/谱系无法识别的记录
- **THEN** MUST 显式标为未知谱系并进入拒绝/状态清单，MUST NOT 静默当作已知 v2。

### Requirement: Conversion to AReaL SFT dataset

> 标题沿用旧名（delta 寻址）：语义已改为**中性 SFT 消费合同**。

系统 SHALL 提供一条将 tagent JSONL 轨迹转换为**可被某个 SFT 消费者读取**的数据集（含 `input_ids` 与 `loss_mask`，prompt 置 `loss_mask=0`、completion 置 `loss_mask=1`）的路径。**具体 tokenizer / chat template / 制品 MUST NOT 由本 spec 指定**：MUST NOT 默认 AReaL 或固定模型（如 `Qwen/Qwen2.5-1.5B-Instruct`），MUST NOT 作本次默认新依赖；真实 SFT consumer 与制品 SHALL 在 **06.6 前具名核准授权**（维持 `design-blocked` 待核），MUST NOT 自动选择/安装模型。

**本波范围修正**：本次只做**数据闭环＋SFT 消费**，源 `--mode rl`（AReaL `PPOTrainer` prompt dataset 路径）**不在本次范围**——重构 MUST NOT 新增真实 on-policy 消费者，MUST NOT 把 PPO/GRPO/RL prompt dataset 兼容当作本次交付或验收项。既有转换器 `v2` 结构**只读导入**（见「旧格式只读导入」）——旧 AReaL 兼容**仅是 v2 只读导入说明**，不改其 v2 含义。

#### Scenario: Convert JSONL to SFT dataset

- **WHEN** 转换以某个**已具名核准**的 tokenizer/模板运行（具体制品不在本 spec 指定，由 06.6 授权）
- **THEN** a dataset SHALL be created at the output path
- **AND** each sample SHALL have `input_ids` (tokenized prompt + completion) and `loss_mask` (0 for prompt, 1 for completion)

#### Scenario: 不指定固定制品且不默认新依赖（失败，本波新增）

- **WHEN** 审查本能力是否把某 tokenizer/模型（如 Qwen）或 AReaL 设为本次默认依赖/指定制品
- **THEN** MUST NOT 指定：真实 consumer/制品由 06.6 具名核准，未核准时维持 `design-blocked`，MUST NOT 自动安装/选型

#### Scenario: on-policy/PPO 路径不在本次范围（失败，本波新增）

- **WHEN** 试图以 `--mode rl` 产出 AReaL `PPOTrainer` prompt dataset 作为本次交付/验收项
- **THEN** MUST NOT 计为本次完成项；本次只验收 SFT 数据消费，MUST NOT 引入真实 on-policy 消费或 PPO/GRPO 训练引擎

## ADDED Requirements

### Requirement: 版本化记录与生产点
训练记录 SHALL 显式版本化；episode/decision/attempt 的**生产点** MUST 前移到装配与模型终局处（02.5/02.4），字段解释在 **00.3 审定**后由本域落实。**episode ≠ session（非强制等同，也非强制拆分）**：episode SHALL 表示业务过程（含子调用、Handoff、共享历史的保存数据依赖）；应用 MAY 选择令某 episode 与某 session 重合，但 MUST NOT 强制 episode 等同 session，MUST NOT 强制把每个 session 拆成多 episode。

#### Scenario: 装配点决定字段（正常）
- **WHEN** 一次请求经装配层并发起
- **THEN** decision/attempt 标识 MUST 在装配/终局点生成并向后传递，采集侧只消费不重写。

#### Scenario: episode 边界由业务依赖决定（非强制）
- **WHEN** 同一 session 内发生子调用、Handoff 或共享历史
- **THEN** 记录 SHALL 按业务依赖划分 episode 边界；应用亦 MAY 选择令 episode 与 session 重合——MUST NOT 强制等同，MUST NOT 强制把每个 session 拆成多 episode。

#### Scenario: 并发子调用各自 attempt（并发）
- **WHEN** 两个子调用并发发起
- **THEN** 各自 attempt 标识独立且可追溯到共同 episode，MUST NOT 相互覆盖。

#### Scenario: 重启后版本稳定（重启）
- **WHEN** 写入进程重启后读取既有记录
- **THEN** 版本字段与谱系解释 MUST 与写入时一致。

### Requirement: 旧格式只读导入
既有转换器 **v2**（`scripts/train/convert_trajectories.py`）SHALL 作为**只读导入**源；新格式 MUST 带**明确版本号**，MUST NOT 让新格式被旧 v2 字段结构绑架，MUST NOT 静默改变或复用 v2 字段含义。

#### Scenario: 导入旧 v2 记录（正常）
- **WHEN** 既有 v2 JSONL 被只读导入
- **THEN** 记录 MUST 标为 v2 且保留其原始字段含义，新格式解析路径不被沿用。

#### Scenario: 旧结构绑架新格式被拒绝（失败）
- **WHEN** 尝试以 v2 字段结构直接承载新格式约束
- **THEN** MUST 拒绝并给出迁移/转换路由，MUST NOT 静默降级。

#### Scenario: 新旧混读不串味（并发）
- **WHEN** 同一批次同时处理 v2 与新版本记录
- **THEN** 两条路径的字段解释 MUST 各自独立，MUST NOT 交叉污染。
