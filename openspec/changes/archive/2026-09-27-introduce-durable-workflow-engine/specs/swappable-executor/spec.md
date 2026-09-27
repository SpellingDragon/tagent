## MODIFIED Requirements

### Requirement: 触发时机与观测

组织懒检查 SHALL 在顶层业务 turn 开始、重试循环外触发单飞构建请求，不在调用线程执行长解析、恢复或资源关闭，不保持在每次 BeforeModel 内。该 turn SHALL 获取当时已发布 effective；候选成功发布之后开始的 turn 才保证使用新代。构建调度 SHALL 合并重复请求，不无限积累候选。

ops/test 的显式 CheckOrgReload 与 Rollback SHALL 保持同步管理语义，通过同一协调器等待本次请求的提交／拒绝结果，不阻塞其他业务请求取得旧 effective，不形成第二生效路径。结果 SHALL 在原日志／诊断面呈现 desired、effective generation、拒绝原因与未收敛引用。

#### Scenario: 懒检测不等待候选构建

- **GIVEN** 配置文件被编辑，候选构建停在可控屏障
- **WHEN** 一个业务 turn 的起点懒检查发现变化
- **THEN** 该 turn 及随后在发布前开始的请求立即使用旧 effective，不等待构建；解除屏障并成功发布后开始的 turn 使用新代，失败则仍旧代且错误可见

#### Scenario: 手动检查同步等待但不封住业务获取

- **WHEN** 运维同步调用 CheckOrgReload，候选尚未完成
- **THEN** 管理调用等待结果，业务请求仍能取得旧 effective；管理调用成功返回后开始的 turn 使用新代

#### Scenario: 进行中不再重复检查

- **WHEN** 一个业务 turn 的多次 LLM 迭代期间配置再次变更
- **THEN** 本 turn 不中途切换绑定；后续 turn 的起点检查可调度变更，成功发布之后开始的 turn 使用新版，不强制发现变更的 turn 等待构建

### Requirement: Rollback 手动触发面

执行器换代能力（单 owner 走 `PublishExecutor`；组织走 `StageExecutor → ActivateExecutor`；二者共用同一条线性化，只换 runner 而不换执行面的独立入口已废除）的 Rollback SHALL 具备生产可达的手动触发面（进程信号或宿主管理命令），触发后经唯一版本协调器将上一份完整有效配置重新构造并发布为新 generation（记 rollback 来源日志），行为与普通热更共用构建、校验和发布路径，仅影响之后开始的调用。

#### Scenario: 运维发信号回滚上一代

- **WHEN** 常驻进程收到约定的回滚信号
- **THEN** 按上一份有效配置重建执行绑定并发布新序号，日志记录代际与指纹，后续请求使用上一代配置

## ADDED Requirements

### Requirement: 整份编排执行绑定发布

执行器换代 SHALL 以整份编排执行绑定为单位发布：候选包含全部受影响 agent 的执行配置、工具声明与子调用目标，全部构造校验成功后一次提交；MUST NOT 以逐 agent 部分发布造成新旧绑定混用。发布后旧绑定保持可用直到其引用的实际调用、响应流与派生后台执行停止，随后按既有退役路径回收；发布与顶层 ACK 本身 MUST NOT 关闭旧资源。

#### Scenario: 发布不提前关闭旧资源

- **WHEN** G2 发布时 G1 仍有响应流未排空或后台任务未结束
- **THEN** G1 所需绑定与资源保持，实际停止后恰好释放一次

### Requirement: 单一应用记录与请求获取一致

有效配置、执行 binding、本地 owner 视图、热参、revision/generation 和成功回执 SHALL 从同一次提交记录读取。新工作取得该记录和使用引用 MUST 与发布／关闭互斥，不留先读后加引用的回收空窗。构造、I/O、恢复激活的重活与资源 Close MUST 不进入业务获取使用的短临界区；多个 setter、Add 和 runner 换入仅相邻执行不能视为原子发布。

#### Scenario: 混合候选的提交屏障

- **WHEN** 同时修改工具和子 agent 热参，候选停在真正提交之前
- **THEN** 新普通请求与消费边界仍读取旧已提交源，新 owner 不可路由；解除屏障后，新请求及诊断取得同一完整新记录，失败不提前改变热参

#### Scenario: 关闭与后到候选竞争

- **WHEN** 构建中开始组织 Close，候选随后准备完成
- **THEN** 候选被丢弃而不发布，新 Run／输入被拒绝，已有调用继续按停止合同收尾，候选独有资源恰一次退出
