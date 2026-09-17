## MODIFIED Requirements

### Requirement: 冷启动回放逐字节重建投影

系统 SHALL 提供 RebuildProjectionFromWAL，启动期对空投影执行且先于 inbox/mem_spill 重放。非空投影 SHALL 不被 Replace，并记录 skipped 原因。存在有效 compaction 时 SHALL 按 retained-ref 交错顺序复原：负 key 合成 ref 使用载荷全文，正 key 从唯一事实链水合；尾部 SHALL 经 MinEventKey 分页读取并按 EventKey 写入序追加，不使用 Timestamp/StartTime 近似。fullBoundary SHALL 使用载荷值，snapshot 与尾部冥想键均须回种。compaction 自身 SHALL NOT 独立进入投影。

相同事实链、配置与 TTL 存活前提下，正常 snapshot 路径的 render SHALL 字节一致。缺键或读取失败 SHALL 形成结构化 partial/failed 结果，不假称逐字节。无 compaction 时 SHALL 进入有界 fallback，不再 no-op；空库必须经成功扫描确认。

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

### Requirement: 无锚恢复不静默截断

无锚回放 MUST 分页扫描并先过滤非投影事件，再保留最新 500 个有效事件，按 EventKey 渲染；中间内存 SHALL 有界。超护栏 MUST 返回 status=partial 和准确 truncated_events，日志、diagnostics、首次模型请求均可辨。被过滤的 task/receipt/快照记录 SHALL NOT 占用 500 的投影名额。

#### Scenario: 超护栏长链冷启动
- **WHEN** 无 anchor 且有 600 条有效事件及 600 条非投影记录
- **THEN** 保留最新 500 条有效事件，truncated_events=100，结果 partial，内部记录不挤掉有效上下文

### Requirement: 恢复观测覆盖全误差面

重建 MUST 返回并保存 RecoveryResult，包含 mode/status、扫描/投影/截断数、missing_keys、pages_failed、batch_errors、payload_errors、耗时。水合 MUST 按请求 key 与返回 key 对账；底层返回空集合与 error 不得被空库分支吞掉。status=full 仅在所有相关扫描、载荷解析、水合均完整且未截断时成立。partial/failed SHALL 进入 diagnostics，并在首次模型请求尾部注入一次简短运行态提示，不成为历史第二真源。

#### Scenario: tail 分页部分失败
- **WHEN** tail 某页查询失败
- **THEN** pages_failed≥1，status=partial/failed，diagnostics 与模型可见提示一致

#### Scenario: 批量读静默漏键
- **WHEN** GetEvents 未返回 error 但返回键集合少于请求集合
- **THEN** missing_keys 列出差集，不能报告 full

#### Scenario: 空结果伴随错误
- **WHEN** 首次扫描即错误且结果为空
- **THEN** status=failed，不记录“空事实链正常恢复”

#### Scenario: payload 无法解析
- **WHEN** compaction 存在而 payload 无法解析
- **THEN** payload_errors≥1，结果 failed，保留原始数据且对宿主可见
