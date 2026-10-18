# event-sourced-projection delta（03-history）

> 复用登记（非规范，不改本条语义）：投影恒等于事实链（KV LSM 事件日志）的 fold/回放；单一真相源，无游离 checkpoint；冷启动逐字节重建。
> 本 delta 只修改「投影是事实链的纯回放（一等不变量）」「冷启动回放逐字节重建投影」；本能力其余 Requirement（折叠向事实链追加 compaction 事件、compaction 事件滚动 supersede、折叠原文冷存不删、compaction 事件可召回且承载综述正文、移除游离快照补丁与 Replace-over-live、无锚恢复不静默截断、恢复观测覆盖全误差面、投影的通道分区与统一装配）由主 spec `openspec/specs/event-sourced-projection/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 投影是事实链的纯回放（一等不变量）

投影（SessionProjection）SHALL 恒等于事实链（KV LSM 事件日志）的 fold/回放映射（**正常运行路径精确成立；退化恢复路径最终一致**），兑现「投影是写入的旁路产物、事实链只在 KV 里」。运行期每次 `StoreEvent` SHALL 伴随 `projection.Add`（增量回放；`persistBusEvent` SHALL 对齐 stored-gate：StoreEvent 失败 SHALL NOT Append，交由 spill 恢复双写补回）；压缩折叠 SHALL 作为事实链的一条 compaction 事件（非游离于事实链外的投影态快照）。系统 SHALL NOT 把投影态持久化到事实链之外的独立 checkpoint（避免双真相源）。

投影消费 SHALL 使用后端返回的 canonical fact 构建引用，不以当前事件重新派生摘要/角色/时间；已存在引用不重复，普通已完成历史不因重放重新展开。内部处理回执、任务注册、常驻会话记录、**历史试验内部记录（wf.*，仅被动排除，不新增写入）**及既有快照/内联排除项 SHALL 不作为普通消息投影；正常写、在线 spill 恢复、冷启动使用同一类型/元数据判定，不能只在重启过滤。批次进入执行前幂等补齐选中事实的引用，包括恢复时未被 snapshot/tail 包含的旧 key；该补齐只覆盖原始 outstanding 选择集合。

投影回放与投影消费 SHALL 进入**实际 Agent 请求**装配：原文、模型视图与呈现决策分层（只有决策为全文时两者才相同），MUST NOT 以呈现决策反推原文。投影态只在事实链内，MUST NOT 引入第二可写真源。

#### Scenario: 运行期投影随事实链增量回放

- **WHEN** 一条可投影事实经成功提交或必要修复
- **THEN** 投影 SHALL 同步 `Add` 对应 canonical 引用（同点投影），未提交失败不能向模型暴露新引用

#### Scenario: 无游离 checkpoint

- **WHEN** 审查投影态的持久化位置
- **THEN** 投影重建所需状态 SHALL 全部来自事实链（原始事件 + compaction 事件），SHALL NOT 存在事实链之外的 KV-meta/独立快照作为第二真相源

#### Scenario: 三路径均排除内部事实

- **WHEN** 同一内部记录（处理回执或已存在的 wf.* 试验记录）分别经正常提交、在线 spill 回补、冷启动扫描
- **THEN** 均不进入普通投影、模型历史、召回或 embedding；组织编排不增加内部阶段事实写入，也不自动删除这些历史记录

#### Scenario: 已有事实不等于当前请求已包含

- **WHEN** 当前 outstanding 输入事实已存在，但未被冷启动 snapshot/tail 复原
- **THEN** 执行前核对 canonical 并补齐当前输入引用，实际请求可见，不因 already 分类跳过输入

#### Scenario: 压缩后同批重试

- **WHEN** 当前输入已投影并经历压缩，随后同业务 turn 进行传输重试
- **THEN** 不重插同一输入引用，不复制事实，使用本批已有派生视图

#### Scenario: 投影进入实际请求且分层

- **WHEN** 一次实际 Agent 请求由投影装配
- **THEN** 原文、模型视图、呈现决策 SHALL 分层；只有呈现决策为全文时视图才与原文相同，MUST NOT 以决策反推原文

### Requirement: 冷启动回放逐字节重建投影

系统 SHALL 提供 RebuildProjectionFromWAL，启动期对空投影执行且先于 inbox/mem_spill 重放。非空投影 SHALL 不被 Replace，并记录 skipped 原因。存在有效 compaction 时 SHALL 按 retained-ref 交错顺序复原：负 key 合成 ref 使用载荷全文，正 key 从唯一事实链水合；尾部 SHALL 经 MinEventKey 分页读取并按 EventKey 写入序追加，不使用 Timestamp/StartTime 近似。fullBoundary SHALL 使用载荷值，snapshot 与尾部冥想键均须回种。compaction 自身 SHALL NOT 独立进入投影。

相同事实链、配置与 TTL 存活前提下，正常 snapshot 路径的 render SHALL 字节一致。缺键或读取失败 SHALL 形成结构化 partial/failed 结果，不假称逐字节。无 compaction 时 SHALL 进入有界 fallback，不再 no-op；空库必须经成功扫描确认。

重建结果 SHALL 进入实际 Agent 请求（与「投影是事实链的纯回放」装配一致）；因果链按 `(PartitionID, SessionID)` 隔离（见 `causal-chain-isolation`），回放 MUST NOT 跨会话连链。

#### Scenario: 逐字节重建（snapshot + 尾部）

- **WHEN** 使用 Content 不等于 EventSummary 的数据，折叠后继续写尾部并独立进程重启
- **THEN** retained refs、合成 tool_chain、边界和尾部恢复后渲染字节一致

#### Scenario: 综述前置首位 + 交错顺序

- **WHEN** 恢复交错 retained refs
- **THEN** 综述在首位，合成与正 key refs 保持载荷次序

#### Scenario: 尾部不丢

- **WHEN** snapshot 后存在跨多页尾部
- **THEN** 所有可见尾部均按 EventKey 追加，不静默截最新端

#### Scenario: bus 滞留事件顺序保真

- **WHEN** Timestamp 与写入序相反
- **THEN** 恢复遵循 EventKey 写入序而非 Timestamp

#### Scenario: fullBoundary 与 meditationKeys 正确恢复

- **WHEN** snapshot 与尾部都含冥想产出
- **THEN** boundary 使用载荷值，双方冥想 keys 均回种

#### Scenario: 无 compaction 事件时恢复

- **WHEN** 事实链有原始事件但从未折叠
- **THEN** 执行 fallback，非空链不被默认为空会话

#### Scenario: 重建不跨会话连链

- **WHEN** 冷启动回放遇多会话事实
- **THEN** 因果链与投影 SHALL 按 `(PartitionID, SessionID)` 隔离，MUST NOT 把另一会话事件连入
