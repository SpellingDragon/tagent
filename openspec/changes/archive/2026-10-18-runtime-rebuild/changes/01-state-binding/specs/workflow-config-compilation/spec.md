# workflow-config-compilation delta（01-state-binding）

> 复用登记（非规范，不改本条语义）：原生模型/工具类型直用框架 `trpc.group/trpc-go/trpc-agent-go`（`model.Model`、`tool.Tool`/`ToolSet`），不建同形影子接口；文件来源复用框架 `skill`（`RefreshableRepository`/`NewFSRepository`）薄胶水；对外资源句柄释放统一走 `tool.ToolSet.Close()`。
> 本 delta 只覆盖「纯声明解析」「发布指纹与版本代」「唯一原子发布入口」三处；本能力其余 Requirement 由主 spec `openspec/specs/workflow-config-compilation/spec.md` 继承（**证据映射，不改**）。

## ADDED Requirements

### Requirement: 配置解析为纯组织声明

`config` SHALL 只解析**纯组织声明**（yaml/json 文本）：严格解析、适用默认值、产出可规范化的声明数据（entry、agent 图、引用、变更基线）。声明 MUST NOT 内嵌活模型/工具/插件实例、连接或锁；这些活对象 SHALL 由组合根在各候选私有准备期构造，并归 state 持有。解析后 MUST 以「默认值已应用」的内容计算规范化指纹输入（键序稳定、无语义差异的文本重写等价）。

输入配置与公开返回的声明快照 SHALL 隔离 slice backing array、map 与嵌套可变值指针；SHALL NOT 向调用方泄漏可修改的内部容器。

#### Scenario: 严格解析拒绝未知字段

- **WHEN** 声明文本含未在契约中定义的字段
- **THEN** 解析 SHALL 以具名错误拒绝并指明字段位置，不静默忽略

#### Scenario: 默认值在解析后应用

- **WHEN** 声明省略某可选字段并触发解析
- **THEN** 产出的声明数据 SHALL 携带该字段的默认值，且指纹基于应用默认值后的规范化内容

#### Scenario: 纯声明不含活对象

- **WHEN** 审查解析产出的声明数据
- **THEN** 其中 SHALL NOT 存在模型/工具/插件实例、连接或锁；仅存在声明面字段与引用

#### Scenario: 等价文本重写不改变指纹输入

- **WHEN** 同一声明被重写为语义等价文本（键序/空白差异）后解析
- **THEN** 规范化指纹输入 SHALL 相同

## MODIFIED Requirements

### Requirement: 发布指纹与版本代

完整组织执行配置（含影响工具声明、子调用目标、模型选择与 prompt 来源的字段及完整 ToolRef）SHALL 构成内容指纹。指纹用于内容比较；每次实际发布 SHALL 赋予单调递增的 generation 序号。字节相同的重新加载 MUST NOT 产生新代或新资源；回滚 SHALL 将上一份有效配置重新构造为新的 generation 序号，不得以内容指纹充当发布序号。

结构指纹仅在确有编译缓存用途时保留，MUST NOT 形成第二个有效版本入口；多个指纹各司其职不构成多真源，唯一真源是有效执行绑定的发布入口。

指纹 SHALL 由纯声明面字段构成（见「配置解析为纯组织声明」）；活对象身份与其生命周期状态 MUST NOT 参与指纹。generation SHALL 由唯一发布入口在**实际发布**时分配。

#### Scenario: 参数变更开新代

- **WHEN** 仅修改某子 agent 的模型或工具参数且候选构造成功
- **THEN** 发布新 generation，之后开始的请求使用新绑定，进行中请求不受影响

#### Scenario: 同内容重载不换代

- **WHEN** 配置文件被重写为语义等价内容并触发检查
- **THEN** 不产生新 generation，不重建资源，日志说明无变化

#### Scenario: 仅文件正文变化不换结构代

- **WHEN** 只修改 prompt/skill 文件正文（FILE 懒读面）而不改声明字段
- **THEN** 结构指纹与 generation SHALL NOT 变化（正文在请求边界固化，见 `config-hot-reload`）

### Requirement: 唯一原子发布入口

启动首代、配置懒检查、手动检查与回滚 SHALL 经同一个版本协调器发布。候选构造与发布 MUST 分离：构造期间不得修改有效 runner、在线工具表、共享投影、TaskManager detector 或任何常驻状态；全部构造、校验成功后才在一次串行化提交中切换有效绑定、generation 序号、回滚配置与诊断状态。任何失败 SHALL 保持 effective 不变并按构造逆序回收候选独有资源；共享 store/session/TaskManager 不在候选回收权限内。

系统 MUST NOT 存在第二条编排生效路径或灰度分派（如 workflow/输入/任务开关）；候选失败后的旧版本仍是唯一有效编排，不构成双路径。

候选 SHALL 记录其构造所基于的基线版本（生效 generation/指纹）；提交点 SHALL 复核该基线在候选构造期间未被推进，若已有新发布则丢弃过期候选，过期候选 MUST NOT 覆盖新发布。发布记录（指纹 + generation + 结果 + 回滚来源）SHALL 作为**独立提交**落账，MUST NOT 与任务终局转移/待通知共享一条提交记录。

#### Scenario: 候选后半段失败不部分生效

- **WHEN** 多 agent 候选中最后一个 agent 构造失败
- **THEN** effective 指纹与 generation 不变，此前构造的候选独有资源被逆序回收，在线请求不受影响

#### Scenario: 请求获取不被长构建阻塞

- **WHEN** 一个大候选正在构造期间新请求开始执行
- **THEN** 请求以短锁获取当前有效版本并立即开始，不等待候选也不读到半成品

#### Scenario: 过期候选不覆盖新发布

- **WHEN** 候选构建期间已发生一次新发布，候选随后到达提交点
- **THEN** 提交点复核发现基线已推进，过期候选被丢弃并明确呈现，MUST NOT 覆盖新发布

#### Scenario: 发布记录不与任务提交共记录

- **WHEN** 一次结构发布与一次任务终局转移同时发生
- **THEN** 发布记录 SHALL 以指纹+generation 独立对账，MUST NOT 与任务终局/待通知写入同一条记录
