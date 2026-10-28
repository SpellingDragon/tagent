## ADDED Requirements

### Requirement: 追加日志提交与完整前缀
持久层 SHALL 以追加记录承载事实与任务变化。一条记录 MUST 完整写入并通过校验后才算提交；读取 MUST 返回自日志开头起的全部完整记录，MUST NOT 跳过中间损坏记录继续返回后续内容。

#### Scenario: 正常提交后顺序可读
- **WHEN** 依次提交两条事实并全部返回成功
- **THEN** 重新打开日志后 MUST 按提交顺序读回这两条记录
- **AND** 提交序号 MUST 严格递增

#### Scenario: 中间记录损坏即拒绝
- **WHEN** 日志中间的一条完整帧校验失败
- **THEN** 打开 MUST 以具名错误失败
- **AND** MUST NOT 返回损坏点之前的部分记录作为可用结果

#### Scenario: 未完成尾部被截除
- **WHEN** 进程在写最后一帧的中途退出，文件末尾残留不完整字节
- **THEN** 打开时 MUST 截除该尾部并保留此前全部完整记录
- **AND** 截除动作 MUST NOT 影响已确认提交的记录

### Requirement: 刷盘后确认
关键恢复事实 MUST 在其记录刷盘成功后才对外确认。派生投影 MAY 滞后于事实落盘。

#### Scenario: 刷盘失败不确认
- **WHEN** 记录已写入但刷盘返回错误
- **THEN** 调用 MUST 返回具名错误
- **AND** 该事实 MUST NOT 进入可读的已提交集合

#### Scenario: 投影失败不影响事实
- **WHEN** 事实已提交成功而查询投影更新失败
- **THEN** 事实 MUST 保持可读
- **AND** 失败 MUST 作为读面滞后被报告，MUST NOT 回滚事实

### Requirement: 旧格式目录拒绝
打开不属于当前格式版本的数据目录时，系统 MUST 拒绝启动并点名原因，MUST NOT 自动迁移、清空或覆盖。

#### Scenario: 打开旧目录
- **WHEN** 数据目录带有旧版分片快照文件
- **THEN** 启动 MUST 失败并说明需要新目录
- **AND** 旧文件 MUST 保持原样

### Requirement: 单 writer 所有权
同一数据目录 SHALL 只允许一个运行时实例写入；第二个实例打开同目录 MUST 具名拒绝。

#### Scenario: 第二个实例打开同目录
- **WHEN** 一个运行时已持有目录写权，第二个运行时以同目录启动
- **THEN** 第二个实例 MUST 失败并说明该目录已被占用
- **AND** MUST NOT 覆盖或清理第一个实例的文件
