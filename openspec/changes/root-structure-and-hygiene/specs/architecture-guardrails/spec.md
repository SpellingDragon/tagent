## MODIFIED Requirements

### Requirement: 分层依赖方向可机械断言

宣称的依赖方向（root → config → {agent, tool/*, prompt, workspace}；root → agent → plugin → memory；event 为纯叶子）SHALL 以自动化测试固化：测试 SHALL 枚举内部包的传递依赖并断言——memory/plugin 及其子包 MUST NOT import agent、config 或根包；event MUST NOT import 任何其他内部包；agent 及其子包 MUST NOT import 根包，且 agent 主体 MUST NOT import config（其 org 子包按注入契约需要时例外）；config MUST NOT import 根包。现状核验全绿，断言为固化；未来违例 SHALL 使测试即刻失败。

#### Scenario: 新代码从 memory 反向引用 agent

- **WHEN** 某次变更在 memory 包引入对 agent 包（或根包、config 包）的 import
- **THEN** 分层断言测试失败并指明违规包与被引包，该变更无法通过 CI

### Requirement: 唯一编排发布权与中性契约

组合根 SHALL 独占编排执行绑定的构造与发布；agent/task/memory/reliability 等内部包 MUST NOT 依赖根包或任何编排内部状态取得版本。版本引用 SHALL 经 agent 层定义的最小执行绑定/租约契约（由组合根注入、经 context 或显式调用参数传递）传达，MUST NOT 将指针写入 inbox/task 持久格式。原 agent→plugin→memory 与 event 纯叶子边界保持并以机械断言固化。

世代治理的**机制**（退役账本、候选事务簿记、换壳 overlay）SHALL 位于 agent 域子包并经注入契约（壳构造回调、注册表接口、resident 句柄）与组合根协作；**发布动作**（orgCoordinator 的换入/发布/告警）SHALL 留在组合根——机制与特权物理分离，两者协作只经注入面。

系统 MUST NOT 复活内部 durable engine/saver/facts、workflow 灰度分派或第二套编排调度；已撤回的独立图 DSL 不得以新名称重新引入。既有绑定内的 owner 执行视图 SHALL 仅用于定位该版配置；可调用目标仍由该 owner 的原 Tools 集合决定，不新增独立维护的平行路由表。

#### Scenario: 内部包反向依赖被阻断

- **WHEN** 某变更在 agent/task/memory 内引入对根包或编排发布器的 import
- **THEN** 分层断言测试失败并指明违规，变更无法通过 CI

#### Scenario: 世代机制绕过注入面取装配态

- **WHEN** agent 域子包里的世代机制直接引用组合根的 runtimeConfig 或 buildAgent
- **THEN** 编译即失败（无 import 路径可达），机制只能经注入契约协作

#### Scenario: 持久格式不含版本指针

- **WHEN** 审查 inbox envelope 与 task 记录的持久字段
- **THEN** 不存在进程内指针或闭包序列化；版本选择只发生在执行入口

## ADDED Requirements

### Requirement: 组合根物理边界与追踪卫生

根包（组合根）SHALL 只承载装配面职责：编排装配、配置装载、注册表、资源与提示词引用、面向外部消费者的入口。世代治理的机制实现（候选事务、热更执行、属主退役、分区碰撞消解）SHALL 位于 agent 域子包，经该子包定义的壳构造契约由组合根**注入**协作，MUST NOT 以 import 根包的方式取得装配内部状态（既有分层断言继续机械生效）。

仓库追踪内容 MUST 满足：追踪文件非空；追踪路径不命中运行期产物模式（锁文件、journal、tmp、prof 等）。顶层目录集合 SHALL 以白名单固化于 CI，新增顶层目录 MUST 同步登记于 README 布局说明。测试运行 MUST NOT 在仓库工作目录落盘（临时数据走测试临时目录）。

#### Scenario: 运行残骸无法入库

- **WHEN** 一次测试或运行以相对路径在工作目录产出锁文件/journal 并被加入索引
- **THEN** 卫生门 MUST 以具名失败拒绝，指出文件与规则

- **WHEN** 向仓库新增一个空文件或一个顶层目录
- **THEN** 卫生门 MUST 分别以"空文件"与"未登记顶层目录"拒绝

#### Scenario: 世代机制移出组合根后依赖方向不变

- **WHEN** 世代治理机制位于 agent 域子包并需要重建执行壳
- **THEN** 它 MUST 经自身定义的构造契约取得能力，组合根在装配期注入；分层机械断言 MUST 保持全绿

### Requirement: 机器消费的点号模块引用必须可解析

脚本与配置中会被运行时实际导入的点号模块引用（shell 的 `python -m` 参数、配置的 `workflow:` 值）SHALL 满足：点号换斜杠后能解析为仓库内的 Python 文件，或命中显式登记的外部包允许表。挂在已删除布局上的此类引用 MUST 在门禁处以具名失败暴露，MUST NOT 静默留存为"看着对"的死配置。

#### Scenario: 引用了不存在的模块布局

- **WHEN** 一条 `workflow:` 值或 `python -m` 参数指向的点号路径在仓库内无对应文件且不在允许表
- **THEN** 门禁 MUST 具名拒绝并给出该引用的位置

#### Scenario: 外部包引用需显式登记

- **WHEN** 引用目标是不随仓库分发的安装包
- **THEN** 它 MUST 出现在允许表内并附用途说明，未登记即红
