# architecture-guardrails Specification

## Purpose

以结构而非纪律承载正确性（implementation-hardening 7A 立法）：分层依赖方向可机械断言（root → agent → plugin → memory，event 为纯叶子）；对上游 trpc-agent-go 内部行为的关键假设（投影完备性所依赖的管线同步等待）以真实管线钉测锁定，升级破坏即红；变更局部性（一个意图一个落点）与可选项单点拆除为内聚/演进的准入准绳。
## Requirements
### Requirement: 分层依赖方向可机械断言

宣称的依赖方向（root → agent → plugin → memory；event 为纯叶子）SHALL 以自动化测试固化：测试 SHALL 枚举内部包的传递依赖并断言——memory/plugin 及其子包 MUST NOT import agent 或根包；event MUST NOT import 任何其他内部包；agent 及其子包 MUST NOT import 根包。现状（2026-09-14 核验）全绿，断言为纯新增固化；未来违例 SHALL 使测试即刻失败。

#### Scenario: 新代码从 memory 反向引用 agent

- **WHEN** 某次变更在 memory 包引入对 agent 包（或根包）的 import
- **THEN** 分层断言测试失败并指明违规包与被引包，该变更无法通过 CI

### Requirement: 上游内部行为假设钉

对投影完备性所依赖的上游内部行为——「框架插件管线在 tool-result 事件上同步等待完成，使 BeforeModel 时投影必完整」（不变量 I2）——SHALL 存在走**真实上游管线**（非 mock 插件序列）的假设钉测试：事件经真实管线落库后，BeforeModel 渲染 SHALL 包含全部先前已存储事件。上游升级若改变该内部行为，此测试 SHALL 失败。其余上游内部假设（如有新识别）SHALL 逐项登记于 LEDGER 红色耦合台账并标注钉测或豁免状态。

#### Scenario: 上游将插件处理改为异步

- **WHEN** 上游 trpc-agent-go 升级后插件管线不再在 tool-result 事件上同步等待
- **THEN** 假设钉测试失败（BeforeModel 渲染缺最新已存事件），破坏在合入前被发现而非在生产行为漂移后

### Requirement: 变更局部性准绳（立法）

新增或重构 SHALL 以「一个变更意图一个落点」为内聚准绳：同一意图的改动 SHOULD 聚于单一包/文件族，跨层散射（霰弹式修改）SHALL 视为内聚缺陷在评审中显式处理。新可选子系统 SHALL 满足「单点拆除」（门控双保险形态：构造期不建 + 运行期 nil 短路，拆除不动核心）。既有 god file（context_manager/tool_agent）的解体以本准绳为验收标准，列为 v0.1.0 冻结后首变。

#### Scenario: 评审一个新增可选子系统提案

- **WHEN** 提案引入新的可选能力（默认关闭）
- **THEN** 评审检查其拆除成本：关闭态零行为变化之外，整体移除 SHOULD 只需删除其自身文件与单点接线，否则退回重设计

### Requirement: 唯一编排发布权与中性契约

组合根 SHALL 独占编排执行绑定的构造与发布；agent/task/memory/reliability 等内部包 MUST NOT 依赖根包或任何编排内部状态取得版本。版本引用 SHALL 经 agent 层定义的最小执行绑定/租约契约（由组合根注入、经 context 或显式调用参数传递）传达，MUST NOT 将指针写入 inbox/task 持久格式。原 agent→plugin→memory 与 event 纯叶子边界保持并以机械断言固化。

系统 MUST NOT 复活内部 durable engine/saver/facts、workflow 灰度分派或第二套编排调度；已撤回的独立图 DSL 不得以新名称重新引入。既有绑定内的 owner 执行视图 SHALL 仅用于定位该版配置；可调用目标仍由该 owner 的原 Tools 集合决定，不新增独立维护的平行路由表。

#### Scenario: 内部包反向依赖被阻断

- **WHEN** 某变更在 agent/task/memory 内引入对根包或编排发布器的 import
- **THEN** 分层断言测试失败并指明违规，变更无法通过 CI

#### Scenario: 持久格式不含版本指针

- **WHEN** 审查 inbox envelope 与 task 记录的持久字段
- **THEN** 不存在进程内指针或闭包序列化；版本选择只发生在执行入口

### Requirement: 编排验收与 runtime 不变量分开

编排热更验收 SHALL 以生产入口行为为准：真实模型请求中的工具声明变化、实际子调用目标、在途一致性、常驻状态身份与安全退役；MUST NOT 以新增原语、编译测试、hash/指针断言或日志编号代替。既有 store/session/projection/TaskManager 身份与运行协议 SHALL 在每次编排验收中同步核对。

#### Scenario: 原语测试不算编排完成

- **WHEN** 版本协调器单元测试通过，但生产入口未见委派目标随配置变化
- **THEN** 编排热更保持未完成，不以原语绿测为准出证据

#### Scenario: 热更不破坏常驻身份

- **WHEN** 任一次编排发布/回滚后运行既有 resident 契约测
- **THEN** store/session/projection/TaskManager 身份断言原样通过，仅被明确替代的增删拒绝与触发时机断言按新要求迁移

### Requirement: 常驻状态与执行装配分离而非复制完整 shell

每个实际 agent（包括子 agent）SHALL 是完整 tagent，具备自己的事件总线、上下文与任务管理能力。config-driven 热更候选 SHALL 通过执行配置／工具装配函数更新已存在 agent，不得仅为提取执行配置再次构造同名完整 TagentAgent 及第二份 TaskManager、bus/projection、cleaner 或恢复组件；真正热新增 agent 仍须完整构造。MUST NOT 将“删除重复 shell”解释为删除实际子 agent 的 bus 或任务域。

同构 agent、执行版本和请求上下文各有生命周期；新增内部结构必须消除重复责任，不形成通用 actor/runtime/broker 框架或集中任务服务。接口形状可以按同构设计迁移，但不能静默丢弃自定义能力、改变数据归属或保留永久主／子双实现。

#### Scenario: 多次结构热更不重复构造同名状态

- **WHEN** 同一拓扑连续修改模型或工具并回滚
- **THEN** 既有 owner、TaskManager 和维护组件不被复制；仅执行配置／工具与必要 runner 换代，各候选资源有恰一次退出责任，子调用仍经原 RunFlow

#### Scenario: 实现优化必须消除重复机制

- **WHEN** 审查本次候选、热参和关闭改造的交付
- **THEN** 明确展示完整 shell、双 reload/rollback 构建和全实例热参广播已消除，新增对象的所有权与退出点可验证；只添加 owner 指针／setter／manager 而保留重复生产路径不满足准出

