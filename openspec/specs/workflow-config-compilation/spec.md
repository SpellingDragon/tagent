# workflow-config-compilation Specification

## Purpose
TBD - created by archiving change introduce-durable-workflow-engine. Update Purpose after archive.
## Requirements
### Requirement: 既有配置构造为真实执行绑定

组合根 SHALL 将当前 YAML（entry、agents、tools 引用）解析为一次可执行的编排绑定：各 agent 的执行配置、工具声明与参数、子 agent 调用目标及 entry 执行器装配。候选 MUST 在解析默认值后的完整配置上构造；删除最后一个工具或清空字段 SHALL 真实反映为新声明集，不得因非零字段合并而保留旧绑定。配置内的 map/slice/参数 SHALL 深拷贝为候选私有，访问器 MUST NOT 暴露可变内部容器。

候选校验 SHALL 沿用既有 agent/工具引用规则：未知或缺失的引用在装配期拒绝并指名；内置 agent、工厂注册及远程 A2A 引用按原解析处理，不要求所有目标都在 Config.Agents 声明。本地 owner 可达集 MUST 排除 remote-only 引用，远端声明／端点仍属于执行配置和结构指纹。PlainToolFactory 与 ToolAgentFactory SHALL 分别验证各自契约；`ToolAgentFactory` 的合同是**返回该 agent 的完整配置声明**，构造与发布归组织唯一的装配／发布路径（工厂自行构造完整 agent 会隐式产生第二 owner，已废除）；并行双注册面不被采用。若某工厂形态无法在该合同下安全准备，必须显式提出兼容性裁决，不能绕过工厂或暗中缩减支持。

#### Scenario: 远端引用随配置热更

- **WHEN** remote-only 引用无同名本地定义，冷启动成功后修改模型或远端声明并热更新
- **THEN** 候选不会为远端名创建本地 owner 或误报缺定义；成功发布后的实际请求使用新端点／声明，在途重试仍使用旧绑定

#### Scenario: 工厂类别不被测试或普通路径替代

- **WHEN** 非内置名字注册 ToolAgentFactory，工厂自带工具且候选准备失败
- **THEN** 保留该工厂的声明与构造合同，失败产物资源退出且旧服务保持；普通 PlainToolFactory 测试不构成该路径的验收证据

#### Scenario: 引用未知 agent 拒绝

- **WHEN** 候选配置的 tools 引用未声明且非内置/工厂/A2A 的 agent 名
- **THEN** 装配失败并指名引用位置，不发布候选，也不将其降级为普通工具

#### Scenario: 删除最后一个委派真实生效

- **WHEN** 热更将某 agent 的 agent 类工具从仅剩一个改为零个
- **THEN** 新开始请求的模型工具声明不含该委派，后续不再发生该子调用

### Requirement: 发布指纹与版本代

完整组织执行配置（含影响工具声明、子调用目标、模型选择与 prompt 来源的字段及完整 ToolRef）SHALL 构成内容指纹。指纹用于内容比较；每次实际发布 SHALL 赋予单调递增的 generation 序号。字节相同的重新加载 MUST NOT 产生新代或新资源；回滚 SHALL 将上一份有效配置重新构造为新的 generation 序号，不得以内容指纹充当发布序号。

结构指纹仅在确有编译缓存用途时保留，MUST NOT 形成第二个有效版本入口；多个指纹各司其职不构成多真源，唯一真源是有效执行绑定的发布入口。

#### Scenario: 参数变更开新代

- **WHEN** 仅修改某子 agent 的模型或工具参数且候选构造成功
- **THEN** 发布新 generation，之后开始的请求使用新绑定，进行中请求不受影响

#### Scenario: 同内容重载不换代

- **WHEN** 配置文件被重写为语义等价内容并触发检查
- **THEN** 不产生新 generation，不重建资源，日志说明无变化

### Requirement: 唯一原子发布入口

启动首代、配置懒检查、手动检查与回滚 SHALL 经同一个版本协调器发布。候选构造与发布 MUST 分离：构造期间不得修改有效 runner、在线工具表、共享投影、TaskManager detector 或任何常驻状态；全部构造、校验成功后才在一次串行化提交中切换有效绑定、generation 序号、回滚配置与诊断状态。任何失败 SHALL 保持 effective 不变并按构造逆序回收候选独有资源；共享 store/session/TaskManager 不在候选回收权限内。

系统 MUST NOT 存在第二条编排生效路径或灰度分派（如 workflow/输入/任务开关）；候选失败后的旧版本仍是唯一有效编排，不构成双路径。

#### Scenario: 候选后半段失败不部分生效

- **WHEN** 多 agent 候选中最后一个 agent 构造失败
- **THEN** effective 指纹与 generation 不变，此前构造的候选独有资源被逆序回收，在线请求不受影响

#### Scenario: 请求获取不被长构建阻塞

- **WHEN** 一个大候选正在构造期间新请求开始执行
- **THEN** 请求以短锁获取当前有效版本并立即开始，不等待候选也不读到半成品

### Requirement: 单一编排表示

系统 SHALL NOT 提供独立于现有 YAML 的 workflow/v1 配置源、图 DSL 或第二套运行期调度分派；既有配置是组织定义的唯一表示。有效配置校验（引用存在、环检测、目标可达）SHALL 并入现有构建入口执行，不保留仅被原型消费的通用框架。

#### Scenario: 无第二配置面

- **WHEN** 审查配置装载路径
- **THEN** 组织编排仅由现有 YAML 字段表达，不存在需并行维护的图定义文件或等价 DSL

### Requirement: 执行配置访问器隔离可变数据

输入配置与公开返回的配置／声明快照 SHALL 隔离 slice backing array、map、嵌套 schema 和可变值指针。运行资源对象 SHALL 通过内部受限句柄复用，不盲目深拷贝带锁对象，不向公开可修改配置泄漏在线工具的变更权限。仅在写入时复制、读取仍返回内部容器 MUST NOT 视为不可变保证。

#### Scenario: 修改读取快照不影响已发布绑定

- **WHEN** 调用者修改 ExecutorConfig 返回的 Tools 元素、声明 schema 或 thinking/reasoning 值指针，并独立准备下一候选
- **THEN** 未发布前已有请求的声明、实际目标和当前目标解析保持不变；下一候选仅在显式提交后生效

### Requirement: 候选所有权与在线可见性分离

新增 owner、递归子实例和候选独有组件 SHALL 登记于候选私有事务；复用原恢复协议完成准备后，只有全部候选成功才交接为在线可路由状态。成功子构建不能在父候选成功之前脱离失败清理责任；未发布候选 MUST NOT 以共享 resident 表暴露新入口。owner 准备与执行配置装配 SHALL 使用同一候选解析域（在线 owner 加候选私有新增者），借用缺失 MUST 明确失败，不回落 entry store 或新建兜底 store。reload 与 rollback SHALL 复用同一 prepare/commit/discard，不复制资源重建及清理逻辑。

#### Scenario: 私有热新增的真实存储归属

- **WHEN** B 配置独立 store，候选已准备 B 但尚未公开，随后装配其执行配置并提交
- **THEN** B 的实际工具读写和新子调用使用该 B owner 的 store，不使用 entry store；提交前 B 不可被新普通入口发现，后段失败只回收该候选资源

#### Scenario: 回滚重建复用在线共享子依赖

- **GIVEN** P 已退役，P 的子依赖 Q 仍由其他在线路由使用
- **WHEN** 回滚需要重建 P，并在最后一个执行组件准备处发生失败
- **THEN** 准备全程复用在线 Q，未创建第二 Q owner；新 P 未提前并入在线清册，失败撤销其全部责任，Q 与当前执行及热参不变

#### Scenario: 子依赖成功而父工具失败

- **WHEN** 新增 A 先成功创建依赖 Z，随后另一个工具构造失败
- **THEN** A/Z 已获取的独占资源按实际获取顺序逆序回收且每件一次，owner 登记撤销，旧 effective 与在线路由不变，下一次合法重试能重新取得资源

#### Scenario: 触发懒检查的请求也不执行长构建

- **WHEN** 一个 turn 首次发现配置编辑，候选随后停在网络或恢复屏障
- **THEN** 该 turn 仅提交构建请求并获取当前已发布绑定，不等待屏障；候选成功发布后才开始的 turn 使用新代

