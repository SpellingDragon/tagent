## ADDED Requirements

### Requirement: FileSegmentStore 原子写入

FileSegmentStore.StoreEvent SHALL 使用临时文件 + rename 策略保证写入原子性：先写入 `{eventKey}.json.tmp`，完成后 `os.Rename` 为 `{eventKey}.json`。崩溃时最多留下 `.tmp` 文件，不影响已有数据。

#### Scenario: 正常写入

- **WHEN** StoreEvent 写入 eventKey=123 的 FullEvent
- **THEN** 先写入 `123.json.tmp`
- **AND** 成功后 rename 为 `123.json`
- **AND** `123.json.tmp` 不存在

#### Scenario: 写入过程中崩溃

- **WHEN** StoreEvent 写入 `123.json.tmp` 后、rename 前进程崩溃
- **THEN** 重启后 `123.json` 不存在（数据不完整但一致）
- **AND** `123.json.tmp` 存在（残留文件）

### Requirement: FileSegmentStore 启动时清理临时文件

FileSegmentStore 初始化时 SHALL 扫描数据目录，删除所有 `*.tmp` 残留文件。清理 SHALL 在首次读写操作前完成。

#### Scenario: 启动时清理残留

- **WHEN** FileSegmentStore 初始化，发现 `123.json.tmp` 和 `456.json.tmp`
- **THEN** 删除这两个 `.tmp` 文件
- **AND** 正常的 `.json` 文件不受影响

#### Scenario: 无残留文件

- **WHEN** FileSegmentStore 初始化，无 `.tmp` 文件
- **THEN** 清理步骤为 no-op

### Requirement: RelationStore WAL 崩溃后自动恢复

InMemRelationStore 在启动时 SHALL 从 WAL（Write-Ahead Log）和 snapshot 恢复因果关系。如果 WAL 文件损坏或为空，SHALL 回退到空状态并记录警告日志，不 panic。

#### Scenario: 正常恢复

- **WHEN** InMemRelationStore 初始化，WAL 和 snapshot 文件都存在且完整
- **THEN** 从 snapshot 恢复全量关系
- **AND** 重放 WAL 中 snapshot 之后的新增关系
- **AND** 因果链恢复到崩溃前状态

#### Scenario: WAL 文件损坏

- **WHEN** InMemRelationStore 初始化，WAL 文件损坏（无法 unmarshal）
- **THEN** 记录 warning 日志
- **AND** 回退到空状态（所有因果关系丢失）
- **AND** 不 panic，服务正常启动

#### Scenario: 首次启动无 WAL

- **WHEN** InMemRelationStore 初始化，WAL 和 snapshot 都不存在
- **THEN** 以空状态启动
- **AND** 创建新的 WAL 文件
