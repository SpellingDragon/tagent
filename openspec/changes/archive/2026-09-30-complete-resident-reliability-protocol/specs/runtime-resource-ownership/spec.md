## MODIFIED Requirements

### Requirement: 运行时资源租约与关闭

组合根 SHALL 通过同代资源 owner 管理 backend、可选 engine、后台生产者、恢复材料保留与写锁。资源按 canonical path 和配置指纹登记；同路径同配置共享，不兼容拒绝。子 agent/执行壳只借用，per-agent 容量观察、错误追踪和读权限继续独立。最后根租约释放才关闭共享资源，旧 release 不得影响新代，重复 release 共享首次结果。

完整实例的 Start/Stop/Close SHALL 由同一生命周期协调保护；首次关闭执行，其余调用等待同一完成结果，不因 active 已变 false 跳过等待。关闭先拒绝接收/新调用，停消息生产者，取消并等待所有在途调用，关闭 runner，再释放根租约；最后 owner 停扫描/压实、等待 engine、flush/close backend，最后释写锁。轨迹记录在 runner 停止后 flush。借用执行壳不得关闭共享状态，资源不能既列普通 closers 又由 owner 重复关闭。

同路径 acquire 与最终关闭 SHALL 共用 per-key 协调，不持 registry 全局锁做 I/O。无法确认旧代 worker/backend 停止时 MUST 保留 poisoned entry 及资源/锁文件强引用，同路径新建拒绝，不依赖不可达对象延迟回收维持锁。关闭错误返回所有关闭调用者；构造每取得资源即登记失败回收，无法安全回收时同样不开放新 writer。

#### Scenario: 两个根共享后关闭一个
- **WHEN** 两根共享，其中一个重复或并发 Close
- **THEN** 另一个保持存储与引擎可用，只有最后根关闭共享资源，未确认保护仍有效

#### Scenario: 关闭后重新打开
- **WHEN** 最后根已确认安全关闭，同进程同路径再次 New
- **THEN** 新 backend/engine 为新代，读取既有事实，不返回旧实例；陈旧 release 不影响它

#### Scenario: 冲突配置与构建失败
- **WHEN** 同路径配置冲突，或取得租约后后续构造失败
- **THEN** 冲突拒绝，失败按依赖逆序释放；无法确认停止时保留 poisoned 所有权，不静默泄露或开放写权

#### Scenario: 完整 Close 与执行交错
- **WHEN** 一个 Close 等待在途 turn，另一个 Close 同时进入
- **THEN** 两者都等待同一终态，不提前释放存储，返回相同关闭错误集合

#### Scenario: 同路径慢关闭
- **WHEN** 最后关闭仍在等待 engine/flush，同时同路径与其他路径创建实例
- **THEN** 同路径等待或收到 poisoned 拒绝，其他路径不被全局锁阻塞

#### Scenario: 关闭失败后的垃圾回收
- **WHEN** worker 停止未确认且发生垃圾回收
- **THEN** poisoned entry 仍持锁文件与资源强引用，同路径不能新开，不依赖失去引用的句柄

## ADDED Requirements

### Requirement: 恢复登记先于遗忘生产者

组合根 SHALL 按实际共享 store 汇总其各可靠输入/普通 spill 目录，在 backend 恢复后、遗忘生产者启动前精确清点并登记全部 outstanding 恢复租约，直接完成结果核对后才开放正常运行。目录读取错误不得当为空。执行壳复用已注册状态，不重复恢复或创建独立保留表。

向运行中共享 store 添加新 owner SHALL 取得恢复登记屏障，在新目录核对/登记完成前暂停该 store 的遗忘发布；发现已有合法删除不得复活。agent 关闭后共享 store 仍活跃时，未清理材料的保护继续归资源 owner；只有安全清理或整个 store 退出才结束内存租约，下一次打开从文件恢复。

#### Scenario: 过期 receipt 的冷启动
- **WHEN** 已超过 TTL 的 receipt 对应未确认信箱项
- **THEN** 先登记保护再启动扫描器，直接核对该 receipt，不先删后查

#### Scenario: 共享 store 晚加入
- **WHEN** 运行中的 store 接入一个含待处理材料的新 owner
- **THEN** 登记屏障覆盖清点/核对，失败保持阻塞并报告，不在遗忘线程竞争中丢材料

#### Scenario: 部分目录读取失败
- **WHEN** 一个共享 store 的任一恢复目录不可读
- **THEN** 不将其解释为空并开放淘汰/新接收，已有原件保持不动，诊断指明阻塞来源
