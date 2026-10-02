## MODIFIED Requirements

### Requirement: 运行时资源租约与关闭

组合根 SHALL 通过 RuntimeResources 管理持久 store/engine 与根租约；资源按 canonical path 和配置指纹登记。同物理路径同配置可共享，不兼容配置 SHALL 拒绝。**指纹 SHALL 只包含存在真实行为差异的配置轴；已被后端裁决为 accepted-and-ignored 的轴（如 localfile 的 fsync）MUST NOT 参与指纹——两份仅在该类轴上不同的配置视为相同配置共享同一实例。**子 agent、验证壳和执行代 SHALL 只借用，不关闭其他 owner 的资源。最后根租约释放 SHALL 关闭并移除登记，后续 New SHALL 获得真正打开的新实例。构建失败 SHALL 逆序释放已获取资源。

#### Scenario: 两个根共享后关闭一个
- **WHEN** 两个根租约共享同一存储，其中一个 Close
- **THEN** 另一个仍可写读且后台扫描仍正常，最后一个 Close 才关闭存储

#### Scenario: 关闭后重新打开
- **WHEN** 最后租约关闭后在同进程同路径再次 New
- **THEN** 不返回原 dead store，新实例读取持久化数据并拥有新的后台生命周期

#### Scenario: 冲突配置与构建失败
- **WHEN** 同路径 lifecycle/engine 等真实行为轴配置冲突，或构建后续步骤失败
- **THEN** 冲突明确拒绝，失败不泄漏已取得的租约、goroutine 和文件句柄

#### Scenario: 零行为差异轴不制造假冲突
- **WHEN** 两份配置仅在已被后端 accepted-and-ignored 的轴（fsync）上不同，先后在同一进程打开同一存储路径
- **THEN** 共享同一实例，不拒绝、不触发重建；既有锁定"fsync 冲突必拒"的测试被改写为锁定该共享语义
