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

### Requirement: 构建失败回收登记先于可失败接线

组合根与装配路径 SHALL 在每次成功 acquire 运行时资源（store/engine/租约）之后、任何可能失败的后续接线之前，立即登记失败回收；构建成功完成交接后才撤销回收登记。任何构建失败出口 MUST NOT 遗漏已获资源的释放，也不得依赖"稍后统一 defer"在时间上晚于新的可失败步骤。

#### Scenario: spill 登记失败不漏租约

- **WHEN** store 租约已取得而后续 SetMemSpill/恢复登记失败导致构建返回错误
- **THEN** 该次构建已获取的租约被回收（共享者不受影响），后续同路径生命周期可正常收敛，无遗留引用计数

### Requirement: 可等待关闭与未确认停止保有 owner

Stop/Close SHALL 按既有 runtime 协议等待在途 turn、任务与模型流：已有完成结果但 completion 尚未持久化时，在有界等待内重试原内容，超时返回明确的未固化清单，MUST NOT 谎报干净关闭。不得为此引入 durable Graph。执行器、模型包装器与执行 owner 按依赖序关闭；无法确认停止时继续显式持有资源，沿用 RuntimeResources 的 poisoned 规则。

#### Scenario: 未固化结果的关闭诚实上报

- **WHEN** Close 时模型已完成但 completion 尚未持久化，且等待超时
- **THEN** Close 返回未固化批次清单；重启按既有 durable 材料恢复，不假装此前内存结果没有丢失风险

#### Scenario: 执行停止未确认封路

- **WHEN** 某共享 store 的旧执行 owner 无法确认停止
- **THEN** 保有其原 lease／登记并拒绝关闭中的同名 owner 重建，不释放路径给第二 backend writer；执行迟后停止可继续原最终退出，若资源层退出已失败形成 poisoned，则沿原规则保持封路，不自动清除

### Requirement: 组织版本引用覆盖实际执行

已退役组织版本 SHALL 保持其执行器绑定和必要资源引用直到实际调用、响应流及派生后台执行停止。发布新版、下游早停或顶层异步 ACK 均 MUST NOT 单独触发旧资源关闭；常驻 store/session/TaskManager 仍归原 owner，不被候选版本误关。

#### Scenario: ACK 后旧版本仍有后台执行

- **WHEN** G1 请求返回任务 ACK，发布 G2 后 G1 后台调用仍在运行
- **THEN** G1 所需绑定与资源保持，实际停止后恰好释放；task_settled 产生的新顶层请求按开始执行时选择当前版本

### Requirement: 实际生产者完成独立于处理后事件流

执行资源释放 SHALL 依据覆盖模型／工具生产、框架 flow、转发及相关清理的完成确认。处理后事件流关闭、消费者返回、取消通知、任务终态以及 race 测试未报告冲突 MUST NOT 代替该确认。公开关闭调用有界返回，未确认停止者继续显式持有并执行既有 poisoned 保护，不无限等待或强关。

#### Scenario: 取消后生产者尚未退出

- **WHEN** 请求被取消且处理后事件通道已关闭，但实际生产者仍停在可控屏障
- **THEN** 绑定和所用 store 租约不释放，Close 截止时明确报告未收敛；解除屏障并完成转发／清理后恰好释放

#### Scenario: 无关旧代独立回收

- **WHEN** G1 的一个调用尚未停止，而随后发布的 G2 已被 G3 替代且 G2 无实际引用
- **THEN** G2 的独占资源可回收，不被 G1 的全局聚合计数无关阻挡；G1 所需共享资源仍保留

### Requirement: 递归获取责任与关闭责任唯一

候选级资源日志 SHALL 覆盖递归部分成功及可失败接线前的每次获取；父子对象接管时明确转移责任，不以 map 顺序推断清理顺序。失败清理错误 SHALL 与主错误共同报告，借用资源不被关闭。

#### Scenario: 父对象接管不重复释放

- **WHEN** 子对象成功后把其资源关闭责任交给父对象，随后整个候选失败
- **THEN** 同一资源不会同时被父 Close 和获取日志重复释放；实际清理顺序、共享租约保持与错误回传均可验证

### Requirement: 版本持有全部合法本地依赖的使用权

组织 binding SHALL 在公开前取得该版本地可调用闭包中每个不同 owner 的使用权，覆盖尚未实际调用但该版允许稍后调用的子 owner。当前发布槽或实际执行引用存在期间持续保有；版本独有工具停止后才归还 owner 使用权。MUST NOT 以子 resident 自身 CM 没有 turn 或 shell 未登记为理由提前退役；MUST NOT 通过全局忙计数阻挡无关旧代回收。store 关闭权仍属于原 RuntimeResources/owner。

#### Scenario: 父已取版而子尚未被调用

- **WHEN** A 已持 G1，G1 允许调用 B，G2 删除 B 时 A 尚未委派
- **THEN** B owner 及其存储保持可用，A 后续真实调用 B 能按 G1 完成；无引用的其他版本可独立回收，G1 最后实际引用完成后 B 才具备退役条件

### Requirement: 有界关闭返回不丢失最终释放责任

公共 Close SHALL 有界返回等待结果，未收敛时保有依赖并上报；同一关闭尾部 SHALL 在真实执行、转发和清理完成后继续恰一次释放 store lease 及登记，无需下一次业务请求、配置编辑或重复 Close。初次等待已返回与资源最终退出 SHALL 可区分。关闭错误不能使仍被借用的 ActionTool/MCP/recorder 提前关闭；backend 或锁退出不确认继续执行原 poisoned 规则，不自动解除。

#### Scenario: 超时以后自动完成资源退出

- **WHEN** 第一次 Close 超时，所有新入口已拒绝，之后无新请求且生产者解除屏障并真实停止
- **THEN** 同一最终尾部完成版本工具、owner 组件、store lease 和登记退出，均恰一次；重复 Close 不重复清理，现有诊断可分辨初次超时和最终退出

#### Scenario: 后代未停不关闭组织共享组件

- **WHEN** org Close 中某子调用尚在使用共享 MCP 或轨迹组件，并导致关闭等待超时
- **THEN** 共享组件仍受保护，超时到达宿主；最后借用者实际退出后才按唯一所有权关闭，不能因为逐 owner Close 已经返回就继续强关

