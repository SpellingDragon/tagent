## MODIFIED Requirements

### Requirement: 运行时资源租约与关闭

组合根 SHALL 通过 RuntimeResources 管理同代持久 backend、可选 engine、后台生命周期组件、writer lock 与 owner 租约；资源按 canonical path、后端和配置指纹登记。同物理路径同配置可共享，不兼容配置 SHALL 拒绝。每次 acquire 的 release MUST 绑定具体 entry 身份且恰执行一次，重复或并发释放不得减少其他租约，更不能作用于同路径新代。

子 agent/执行壳的资源绑定 SHALL 服从既有 owner 与身份规则；借用方不得关闭其他 owner 的资源。agent Close MUST 幂等并保留首次关闭结果。最后 owner 释放 SHALL 先停止生产者和 in-flight 使用，再停止调用 engine 的扫描/压实活动、停止并等待 engine worker，随后 flush/close backend，最后释放 writer lock 并移除登记。新 New SHALL 获得真正打开的新 backend/engine，不得命中独立旧 engine 缓存。

构建失败 SHALL 从首次取得资源开始逆序清理，覆盖 engine wiring 失败；每 agent 的 capacity hook、错误追踪和读权限仍独立。embedding 初始化失败 SHALL 保持既有关键词降级和 capacity hook，不使主事实提交不可用。关闭错误 MUST 可观测；无法确认旧 worker 停止时不得开放同路径新 writer。

#### Scenario: 两个根共享后关闭一个
- **WHEN** 两个根租约共享同一存储，其中一个 Close
- **THEN** 另一个仍可写读且后台扫描和 engine 正常，最后一个 Close 才关闭共享资源

#### Scenario: 关闭后重新打开
- **WHEN** 最后租约关闭后在同进程同路径再次 New
- **THEN** 返回新 backend 与新 engine，读取持久数据并拥有新的后台生命周期，不引用旧 store/KV

#### Scenario: 冲突配置与构建失败
- **WHEN** 同路径 fsync/lifecycle/engine 配置冲突，或构建后续步骤包括 engine wiring 失败
- **THEN** 冲突明确拒绝，失败不泄漏已取得的租约、goroutine、文件句柄和写锁

#### Scenario: 重复 Close 不伤害共享者
- **WHEN** A、B 共享资源，A 被连续或并发 Close 多次
- **THEN** A 只释放一次租约，B 的事实写入、检索及 engine 继续有效

#### Scenario: 旧 release 不影响新代
- **WHEN** 旧 entry 已关闭，同路径新 entry 已打开，旧 release 再次执行
- **THEN** 新 entry 的计数、backend、engine 与 writer lock 均不受影响

#### Scenario: engine 关闭与降级
- **WHEN** 最后 owner 关闭，或 embedding 初始化失败而仅启用 capacity hook
- **THEN** 已启动 engine worker 在 backend 关闭前退出；降级路径无悬挂 worker 且 capacity hook 仍可用

### Requirement: 持久目录单 writer

持久后端 SHALL 对 canonical 物理目录实施进程级单 writer 所有权保护；同进程共享经 registry，跨进程重复打开 SHALL 拒绝。writer lock MUST 在可写 backend/后台 worker 启动前取得，直到全部旧代使用者停止且关闭处理完成后才释放。同一路径 acquire 与最后 release SHALL 经同一 per-key 协调，其他路径 SHALL 不被慢 open/close 的全局锁阻塞。正常关闭或进程退出后的新实例 SHALL 能重新取得所有权，不能因遗留标记永久锁死。

#### Scenario: 独立进程并发打开
- **WHEN** 一个进程已持有目录写权，第二个进程启动
- **THEN** 第二个进程明确失败，不并发写入或清理第一个进程的临时文件

#### Scenario: 关闭与同路径重开竞争
- **WHEN** 最后 release 尚在停止 worker/flush，同路径 New 并发到达
- **THEN** 新代等待旧代关闭完成或得到明确关闭失败，不与旧 writer 并存
- **AND** 不同路径的 New 不因 registry 全局锁等待该慢关闭
