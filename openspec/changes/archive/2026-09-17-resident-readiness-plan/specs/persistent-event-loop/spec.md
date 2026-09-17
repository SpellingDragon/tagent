## REMOVED Requirements

### Requirement: 循环停止后可重启

**Reason**: 旧规格要求同实例重启，与已实现的终结态生命周期及关闭通道契约冲突，不能继续保留相反条文。
**Migration**: 调用方在 StopLoop 后创建新 TagentAgent 并恢复事实链；新终结态 Requirement 覆盖关闭与恢复语义。

## ADDED Requirements

### Requirement: 循环停止为终结态

同一个 TagentAgent 的 StopLoop SHALL 为终结操作：再次 StartLoop SHALL 显式返回错误，不返回关闭通道，不创建第二个循环。需要重新运行时 SHALL 创建新 agent 并从事实链恢复。重复 Stop/Close SHALL 幂等；Start/Stop/Close 的并发状态转换 MUST 受同一生命周期同步机制保护。每次成功启动的输出通道 SHALL 恰关闭一次，循环异常退出也 SHALL 更新真实 active/terminated 状态。

#### Scenario: Stop 后 Start 的往返
- **WHEN** 同实例 StopLoop 返回后再次 StartLoop
- **THEN** 返回明确终结态错误；新实例可正常恢复并启动

#### Scenario: 两轮完整往返后停止
- **WHEN** 分别创建两个实例执行 Start→Stop→Close
- **THEN** 两个实例各关闭自己的通道一次，重复停止不 panic

#### Scenario: 并发启动停止
- **WHEN** Start/Stop/Close 并发执行或 loop 异常退出
- **THEN** active 状态与真实消费者一致，无 WaitGroup 误用、双关闭或后台孤儿

### Requirement: 可判定的接收结果

系统 SHALL 提供带 context 和稳定请求 ID 的接收入口，返回 receipt 或明确错误，区分 volatile/durable accepted。旧 void 入口 SHALL 保留兼容并记录拒绝计数，随载 HTTP/宿主 SHALL 使用新入口。关闭、满额、超时和存储失败 SHALL NOT 被表示为 accepted。202 SHALL 只表示接收，不表示任务处理/消息送达完成。

#### Scenario: volatile 模式满队列
- **WHEN** 未配置可靠 inbox 且队列满至超时
- **THEN** 返回明确背压错误，计数可见，不承诺持久成功

### Requirement: 可靠输入全序持久化

显式可靠模式 SHALL 将所有输入先写入有界 inbox-v1，文件和目录屏障完成后才返回 durable。发布序列 SHALL 由单一串行化点分配；消费者按该序处理，channel 只作唤醒。默认未确认上限 2560，满额/不可写/初始化失败 SHALL 拒绝，不回退 volatile。一次批量提交 SHALL 以单 envelope 接收，保留各消息的来源和稳定身份。

#### Scenario: 低负载可靠输入仍可恢复
- **WHEN** 只有一个输入收到 durable receipt 后进程终止
- **THEN** 新进程可恢复该输入，不依赖先填满 channel

#### Scenario: 并发与背压不超车
- **WHEN** 多生产者在积压临界点并发提交
- **THEN** 所有成功 receipt 有全序；超额明确拒绝，新事件不绕过旧持久项

### Requirement: 输入处理确认与幂等

claim SHALL 不删除原件。事实提交使用固定 EventKey 与源 ID 幂等，投影不重复。只有 turn 结束且处理结果 receipt 已写入事实链并耐久后，系统 SHALL ack inbox。崩溃后未完成 claim MUST 可重试，已确认 receipt MUST 不重复处理。处理 receipt SHALL 注册为非投影事件；对应 inbox 尚未确认清理时 SHALL 免于 TTL/容量淘汰，ack 删除及目录同步后按 30 天保留窗口处理。重启去重索引 SHALL 只覆盖 outstanding IDs；超过 30 天的客户端重复提交不承诺幂等。工具副作用 SHALL 明确为至少一次边界，不宣称 exactly-once。

#### Scenario: 取出后入库前崩溃
- **WHEN** claim 完成但事实提交未完成即终止
- **THEN** 原输入仍存在，恢复后可重试且不丢

#### Scenario: 入库后执行前崩溃
- **WHEN** 原始事件已提交但尚无处理完成 receipt
- **THEN** 恢复后继续/重试处理，原始事件与投影不重复追加

#### Scenario: receipt 后 ack 失败
- **WHEN** 处理完成 receipt 已耐久但删除 inbox 失败
- **THEN** 恢复只重试确认清理，不再次执行已确认输入

#### Scenario: 旧格式或损坏项
- **WHEN** 存在未迁移旧 spill 或不可读取的新 inbox 项
- **THEN** 旧格式未排空时拒绝启动并给迁移提示；损坏项保留隔离及告警，不静默删除
