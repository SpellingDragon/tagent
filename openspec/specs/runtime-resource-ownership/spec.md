# runtime-resource-ownership Specification

## Purpose
TBD - created by archiving change resident-readiness-plan. Update Purpose after archive.
## Requirements
### Requirement: 运行时资源租约与关闭

组合根 SHALL 通过 RuntimeResources 管理持久 store/engine 与根租约；资源按 canonical path 和配置指纹登记。同物理路径同配置可共享，不兼容配置 SHALL 拒绝。子 agent、验证壳和执行代 SHALL 只借用，不关闭其他 owner 的资源。最后根租约释放 SHALL 关闭并移除登记，后续 New SHALL 获得真正打开的新实例。构建失败 SHALL 逆序释放已获取资源。

#### Scenario: 两个根共享后关闭一个
- **WHEN** 两个根租约共享同一存储，其中一个 Close
- **THEN** 另一个仍可写读且后台扫描仍正常，最后一个 Close 才关闭存储

#### Scenario: 关闭后重新打开
- **WHEN** 最后租约关闭后在同进程同路径再次 New
- **THEN** 不返回原 dead store，新实例读取持久化数据并拥有新的后台生命周期

#### Scenario: 冲突配置与构建失败
- **WHEN** 同路径 fsync/lifecycle/engine 配置冲突，或构建后续步骤失败
- **THEN** 冲突明确拒绝，失败不泄漏已取得的租约、goroutine 和文件句柄

### Requirement: 持久目录单 writer

持久后端 SHALL 对 canonical 物理目录实施进程级单 writer 所有权保护；同进程共享经 registry，跨进程重复打开 SHALL 拒绝。正常关闭或进程退出后的新实例 SHALL 能重新取得所有权，不能因遗留标记永久锁死。

#### Scenario: 独立进程并发打开
- **WHEN** 一个进程已持有目录写权，第二个进程启动
- **THEN** 第二个进程明确失败，不并发写入或清理第一个进程的临时文件

### Requirement: agent 身份隔离

每个 agent 的 store/session/projection/task 绑定 SHALL 与其身份一致，跨 agent 读取只按显式 read_namespaces 授权。共享实际 store 内不同名称映射相同 partition ID 时 SHALL 在构造/热更前拒绝并列明冲突；SHALL NOT 自动更改 EventKey 布局或迁移历史归属。

#### Scenario: 哈希冲突
- **WHEN** 同一实际 store 的两个不同 agent 名产生相同 pid
- **THEN** 构造失败并指明冲突；未授权记忆不合并、不自动重写 key

#### Scenario: 热更后隔离不漂移
- **WHEN** 子 agent 原用独立 store，entry 发生结构热更
- **THEN** 子 agent 继续使用自己的 store 与投影，不被替换为 entry store

