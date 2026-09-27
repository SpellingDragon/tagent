## MODIFIED Requirements

### Requirement: 配置热更统一应用模型

配置热更 MUST 以逐 agent 解析默认值后的完整有效配置为基线。五个既有独立热参数（compress_threshold / max_tokens / keep_recent_tasks / task_terminal_ttl / task_default_ttl）与结构指纹字段在同一次混合候选中 MUST 全部准备成功才提交；结构发布不能跳过数值应用，数值应用不能仅修改常驻 entry 或不服务请求的 resident 对象。

五个热参数 SHALL 保留原热应用语义，不为修补对象错位而一律纳入结构指纹。每个 owner SHALL 通过稳定读取入口取得同一已提交应用记录中的完整热参，子调用初始化及在途安全预算／压缩边界按需读取；一次压缩内外层使用同一组参数。MUST NOT 靠对全部私有 CM 的后台逐实例 setter 广播维持一致，也不让执行壳自建权威快照。活跃调用登记只服务实际取消／完成责任，不成为热参订阅历史。

实际任务 spawner／TaskManager SHALL 在默认寿命、reaper 回退和终态保留消费点取得有效 TTL。已有任务显式 TTL 和寿命锚点不被重新解释；被移除但排空中的 owner 的最后有效值保留至退出，不因缺定义改默认。字段删除或零值按现有规范化规则回落，不非零合并遗留旧值。工具输出封顶仍按工具构造期派生，numeric-only 不改写已发布工具。

#### Scenario: 热新增后更新跨越私有 CM 构造窗口

- **WHEN** 先热新增 B，再 numeric-only 更新 B，更新与真实 B 调用的私有 CM 构造及下一压缩边界交错
- **THEN** 发布后开始的 B 调用初始读取有效值，先开始的调用下一压缩边界读取最新完整组，无注册空窗漏更；实际压缩预算及任务默认 TTL 正确，回滚走相同消费源

#### Scenario: 同次修改结构字段与数值字段

- **WHEN** 一次保存修改 entry 的 system_prompt 和子 agent 的 max_tokens
- **THEN** 全部候选成功后一起生效，后续真实子调用的预算读取使用新值；失败时结构及热参源均保持旧值

#### Scenario: 数值修改到达实际子调用

- **WHEN** 仅修改热新增或结构发布后子 agent 的 max_tokens 或 keep_recent_tasks，而当前委派使用已发布工具绑定
- **THEN** 不必重建结构 generation，新调用和存活调用下一次安全读取使用新参数；只读 resident getter 不构成通过证据

#### Scenario: 字段删除回落默认

- **WHEN** 删除某 agent 显式设置的 keep_recent_tasks
- **THEN** 实际消费方使用解析默认值，未修改旁支维持原配置，不沿用被删除的值

#### Scenario: 默认 TTL 不改写已有显式任务寿命

- **WHEN** 更新 task_default_ttl，同时存在携显式 TTL 的存量任务和随后使用默认 TTL 创建的新任务
- **THEN** 新任务使用有效默认值，已有显式 TTL 与寿命锚点保持；回执与真实任务行为一致

### Requirement: 热更回执报告 effective 状态

每次配置应用 MUST 报告 desired/effective、结构 generation、实际应用范围和保持／拒绝原因，以及可验证的退役引用与 owner 状态。应用成功意味着消费源已安装且后续行为使用该值，不等同于调用一次 resident setter。lastAppliedAt SHALL 表示完整配置最近成功应用时间（含 numeric-only），lastPublishedAt SHALL 单独表示最近结构发布／回滚时间。失败不推进成功时间。

#### Scenario: 热更回执核对

- **WHEN** 任一次热更完成
- **THEN** 回执中的预算和 TTL 能与真实子模型／任务行为互证，失败明确未生效，排空者明确保持而非 applied

#### Scenario: 数值应用不伪造结构发布时间

- **WHEN** numeric-only 修改成功且结构绑定不变
- **THEN** lastAppliedAt 与完整 effective 配置更新，lastPublishedAt 和结构 generation 不推进

## ADDED Requirements

### Requirement: 数值与结构共享完整有效配置回滚记录

每次语义有变化且成功的应用 SHALL 更新协调器当前／上一份完整有效配置，numeric-only 不能只更新运行对象而保留过期回滚配置。Rollback SHALL 从上一份完整配置走同一候选事务，同时恢复结构和热参数并发布新 generation；失败两轴均不部分生效，不保存旧执行器作为恢复依据。

#### Scenario: 数值更新后的完整回滚

- **WHEN** G1 的完整配置先经历一次 numeric-only 成功更新，随后执行 Rollback
- **THEN** 原结构与上一份数值配置一起恢复并产生新发布序号；真实子调用及任务默认值与回滚记录一致，进行中结构绑定不被迁移
