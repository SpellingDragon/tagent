# trajectory-trace-correlation Specification (Delta)

## ADDED Requirements

### Requirement: 轨迹记录携带 trace 关联字段
TrajectoryRecorder 录制 LLM 调用时 SHALL 从 ctx 提取当前 trace_id/span_id 写入 LLMCallRecord 的可选字段(JSON omitempty);trace 未启用(noop)时字段 SHALL 为空且轨迹文件其余内容与现状逐字节兼容(旧消费者可忽略新字段)。

#### Scenario: 启用导出时轨迹可跳转到 trace
- **WHEN** OTLP 启用且 TrajectoryDump 开启,完成一次 LLM 调用
- **THEN** 对应 JSONL 记录含非空 trace_id/span_id,可在 trace 后端定位该次调用的 span

#### Scenario: noop 时轨迹向后兼容
- **WHEN** 未设 OTLP endpoint
- **THEN** 轨迹记录不含 trace 字段(omitempty 省略),既有解析逻辑无需修改

### Requirement: trace 反向定位轨迹
启用 TrajectoryDump 时,turn root span SHALL 携带轨迹定位属性(轨迹目录与 session 文件名),使运维侧从任一 turn span 可跳转到对应 RL 轨迹文件。

#### Scenario: 从 span 跳转轨迹
- **WHEN** OTLP 与 TrajectoryDump 同时启用
- **THEN** tagent.turn span 属性含 tagent.trajectory.dir 与 session 文件标识

## MODIFIED Requirements

<!-- trajectory-recording 主规格(openspec/specs/trajectory-recording/)如有 LLMCallRecord 字段结构的既有条目,归档同步时按"全文拷贝+增量字段"规程合并;本 delta 仅新增可选字段,不改变既有字段语义 -->
