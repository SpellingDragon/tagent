## MODIFIED Requirements

### Requirement: 投影是事实链的纯回放（一等不变量）

投影（SessionProjection）SHALL 恒等于事实链（KV LSM 事件日志）的 fold/回放映射（**正常运行路径精确成立；退化恢复路径最终一致**），兑现「投影是写入的旁路产物、事实链只在 KV 里」。运行期每次 `StoreEvent` SHALL 伴随 `projection.Add`（增量回放；`persistBusEvent` SHALL 对齐 stored-gate：StoreEvent 失败 SHALL NOT Append，交由 spill 恢复双写补回）；压缩折叠 SHALL 作为事实链的一条 compaction 事件（非游离于事实链外的投影态快照）。系统 SHALL NOT 把投影态持久化到事实链之外的独立 checkpoint（避免双真相源）。**文档化边界**：跨退化恢复的重启可能缺 spill 补写事件（spill 沿用旧 key，晚于 compaction 补写时被 tail 边界切掉）——最终一致非逐字节。

投影消费 SHALL 使用后端返回的 canonical fact 构建引用，不以当前事件重新派生摘要/角色/时间；已存在引用不重复，普通已完成历史不因重放重新展开。内部处理回执、任务注册、常驻会话记录、**历史试验内部记录（wf.*，仅被动排除，不新增写入）**及既有快照/内联排除项 SHALL 不作为普通消息投影；正常写、在线 spill 恢复、冷启动使用同一类型/元数据判定，不能只在重启过滤。批次进入执行前幂等补齐选中事实的引用，包括恢复时未被 snapshot/tail 包含的旧 key；该补齐只覆盖原始 outstanding 选择集合。本变更不修改普通已完成历史的压缩/回放算法，既有普通 spill 晚于 compaction 补写的最终一致边界继续明确保留。

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

#### Scenario: 被动排除不额外缩短历史 TTL

- **GIVEN** 全局 TTL 为 90 天、没有显式 wf 类型 TTL，存在一条 31 天前的历史 wf 记录
- **WHEN** 加载被动排除注册并运行既有生命周期扫描
- **THEN** 本变更不新增 30 天类型 TTL，该记录不因此提前淘汰；它仍不进入普通投影、召回或 embedding，既有显式 TTL 和 retention 保护语义不变
