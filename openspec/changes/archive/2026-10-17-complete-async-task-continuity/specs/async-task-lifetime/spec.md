## MODIFIED Requirements

### Requirement: 写入型重入续期锚点，只读观察不续期

成功写入型 send/resume SHALL 更新同一 producer 的 TTL 锚点；peek MUST NOT 续命。Resume 的候选锚点 SHALL 在绑定受理记录中持久化，避免绑定提交后、首信号前崩溃丢失续期。意图受理后准备、绑定或实际发送失败时，系统 MUST 保留失败尝试轮号并恢复旧 producer、owner 和寿命锚点，不把失败尝试当作成功续命。显式TTL仍优先于默认值。

#### Scenario: 绑定提交后立即崩溃
- **WHEN** 新轮绑定已提交而尚无任何结果信号
- **THEN** 恢复读取该绑定中的候选寿命锚点，不退回旧锚点

#### Scenario: 输入发送失败
- **WHEN** Resume 已记录意图但实际SendKeys失败
- **THEN** 记录 round_aborted，旧 producer 的身份和TTL不变，下一尝试不复用失败轮号

### Requirement: 结算通知以有界票据进入投影并可被折叠回收

结算正文 SHALL 有界且可按票据取回；消费过的观察 SHALL 仅在 input_disposition 的源EventKey和partition精确核验后折叠。未消费观察、decision输入、用户输入和仍被恢复需要的事实 MUST 保留。批量退役可对无调用归属条目汇总通知，但每条raw信号 SHALL 独立保留受理与ACK责任；有 invocation 归属的条目 MUST 走原调用逐条路由。退役谱系 SHALL 只盖在信号上，MUST NOT 改写 Spec.Origin。

#### Scenario: 有处置凭据的观察被折叠
- **WHEN** 一个观察有与其源key及partition匹配的observe处置事实
- **THEN** 投影可回收其正文，原始事实仍可按key取回

#### Scenario: 伪造处置凭据
- **WHEN** receipt 引用错误源key或不同partition
- **THEN** 原观察不被视为已消费，也不得据此折叠

## ADDED Requirements

### Requirement: 单一最近期限调度主动执行 TTL

生产 TaskManager SHALL 启用 deadline reaper，以一个可重排的最近期限 timer 处理 active TTL 及可回收终态，不使用逐任务 sleep 或固定扫空 ticker。无输入、无看板、无冥想时，到期任务仍 SHALL 由 owner 请求 Cancel 并一次退役为 failed。没有任务时 SHALL 停用 timer。RecheckDeadlines SHALL 从现有 TTL source 和真实锚点重新调度，MUST NOT 另存一份 TTL 真值。独立构建且未启用主动调度的管理器可保留显式 List/reconcile 行为。

#### Scenario: 完全静默任务到期
- **WHEN** 已纳管任务到期后没有任何外部唤醒
- **THEN** 最近期限 timer 仍触发取消和失败退役

#### Scenario: 默认 TTL 热更新
- **WHEN** 当前 source 降低默认 TTL 并调用 RecheckDeadlines
- **THEN** 既有无显式TTL任务按新值重新排期，显式TTL与任务锚点不被覆盖

## REMOVED Requirements

### Requirement: 回收由唤醒驱动，不为到期引入常驻计时器

**Reason**: 仅依赖看板/冥想会使无输入任务越过TTL仍运行。

**Migration**: 生产装配启用单一最近期限timer，保留独立管理器的lazy兼容。
