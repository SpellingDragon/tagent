## ADDED Requirements

### Requirement: TrajectoryRecorder 支持 Flush

TrajectoryRecorder SHALL 提供 `Flush()` 方法，将缓冲区中未写入的 trajectory 数据强制落盘。`Close()` SHALL 在关闭文件前调用 Flush。Flush SHALL 是幂等的（多次调用不产生副作用）。

#### Scenario: Flush 强制落盘

- **WHEN** TrajectoryRecorder 记录了 5 条 LLM 调用，但 buffer 尚未满（未触发自动写入）
- **AND** 调用 Flush()
- **THEN** 所有缓冲数据写入 JSONL 文件
- **AND** JSONL 文件包含 5 条记录

#### Scenario: Close 前自动 Flush

- **WHEN** TrajectoryRecorder.Close() 被调用
- **THEN** 先执行 Flush() 确保数据落盘
- **AND** 然后关闭文件句柄

#### Scenario: 空 buffer Flush

- **WHEN** buffer 为空时调用 Flush()
- **THEN** 不产生任何写入操作
- **AND** 不报错

### Requirement: TrajectoryRecorder 文件写入原子性

TrajectoryRecorder 写入 JSONL 文件时 SHALL 使用 append 模式。每条记录 SHALL 是一行完整的 JSON（以 `\n` 结尾）。进程崩溃时最多丢失最后一条未 flush 的记录。

#### Scenario: 追加写入

- **WHEN** TrajectoryRecorder 写入第 3 条记录
- **THEN** 在 JSONL 文件末尾追加一行
- **AND** 不影响前 2 条记录

#### Scenario: 崩溃后文件完整

- **WHEN** 写入第 5 条记录后、flush 前进程崩溃
- **THEN** JSONL 文件包含前 4 条完整记录
- **AND** 第 5 条记录可能不完整（被截断）
- **AND** 重启后可正常追加新记录（截断行被覆盖或跳过）
