# architecture-guardrails Specification

## Purpose

以结构而非纪律承载正确性（implementation-hardening 7A 立法）：分层依赖方向可机械断言（root → agent → plugin → memory，event 为纯叶子）；对上游 trpc-agent-go 内部行为的关键假设（投影完备性所依赖的管线同步等待）以真实管线钉测锁定，升级破坏即红；变更局部性（一个意图一个落点）与可选项单点拆除为内聚/演进的准入准绳。
## Requirements
### Requirement: 分层依赖方向可机械断言

宣称的依赖方向（root → config → {agent, tool/*, prompt, workspace}；root → agent → plugin → memory；event 为纯叶子）SHALL 以自动化测试固化：测试 SHALL 枚举内部包的传递依赖并断言——memory/plugin 及其子包 MUST NOT import agent、config 或根包；event MUST NOT import 任何其他内部包；agent 及其子包 MUST NOT import 根包，且 agent 主体 MUST NOT import config（其 org 子包按注入契约需要时例外）；config MUST NOT import 根包。现状核验全绿，断言为固化；未来违例 SHALL 使测试即刻失败。

#### Scenario: 新代码从 memory 反向引用 agent

- **WHEN** 某次变更在 memory 包引入对 agent 包（或根包、config 包）的 import
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

### Requirement: 测试文件族与职责同位

测试文件的划分 SHALL 与生产职责同位：同一职责的多个工况 MUST 收敛在同一测试文件内，以子测试（表驱动优先）区分工况，MUST NOT 以「一工况一文件」平铺。目标形态是测试文件族与所辖生产文件族一一对应；每个测试文件 SHALL 在其 package/文件级 doc 槽位以一行索引声明所辖职责（`// 契约: <docs/** 下承载该职责的路径>`；索引目标根只有 `docs/`，生产文件路径不是合法索引目标——终裁对齐门 `index-root`）。

本要求只约束测试文件族，不改动生产文件划分：既有 god file（`context_manager.go`/`tool_agent.go`）的解体另按「变更局部性准绳」既定安排处理，MUST NOT 借测试合并之名提前拆分生产文件。

「同一职责」的机检判定单位 SHALL 为 `(目录, 包名, build-tag 集, 契约锚点)` 四元组，参与者 SHALL 为含至少一个 `func Test` 声明的 `_test.go`；参与者数量不足 2 的键不构成违规。同键参与者 ≥2 时，组内**无镜像**（同目录不存在去掉 `_test` 后缀的同名 `.go` 文件；既存变体后缀仅 `_real` 按家族镜像宽容）的每个测试文件构成一条 `responsibility-fragmentation` finding。镜像文件、build-tag 异组、无 `func Test` 的 test-support 文件（桩/基座/纯基准）SHALL NOT 被判定为碎片，MUST NOT 为满足收敛而与异 tag 文件或镜像文件错并。

碎片 finding 的消除 SHALL 三出口等价合法：①工况并入同键的镜像文件；②测试文件改名对齐生产镜像（MUST 经映射表登记旧名→新名）；③文档侧收敛锚点（wiki 小节上收/细化/重挂）。门禁 SHALL 并列提示三条出口，MUST NOT 把「合并」预设为唯一解。该规则 SHALL 以棘轮接入 `comment_policy`（按文件计数、只降不升），基线归零后 SHALL 从基线移除槽位、自动升级为与 `mechanism-narrative` 同级的零容忍硬门。

#### Scenario: 同职责工况分散在多文件

- **WHEN** 同一执行代发布职责的测试散为租约、隔离、性能、接缝等多个文件
- **THEN** 这些工况 SHALL 合并为该职责的单一测试文件，工况以子测试表达；共享 fixture/helper 归一，同名异义或近重复的 helper MUST 显式裁决并记录取舍理由，不得静默择一

#### Scenario: 合并不改变测试覆盖

- **WHEN** 执行一次测试文件合并
- **THEN** 生产文件 SHALL 零变化；合并前后测试清单 SHALL 一一对应（无丢失、无静默新增）；每个测试函数的断言数量 SHALL NOT 下降；受影响包全量测试与 `-race` SHALL 全绿

#### Scenario: 端到端链不因收敛而拆散

- **WHEN** 一个贯穿场景（常驻重挂、跨发布回流、多级委派等）由多个协作面构成
- **THEN** 该链 SHALL 保持单一端到端测试落点，MUST NOT 为满足「按包分文件」把它拆成各包的局部测试后以分别通过充当整链通过

#### Scenario: 同锚点多文件各自镜像生产文件

- **WHEN** 同一 (目录, build-tag 集, 锚点) 键下多个测试文件，且每个文件去掉 `_test` 后缀都能对上同目录一个生产文件（如 `settle_test.go`/`tmux_executor_test.go` 各对 `settle.go`/`tmux_executor.go`）
- **THEN** 门禁 SHALL 判零 finding；MUST NOT 要求这些镜像文件相互合并，因为「测试文件族与生产文件族一一对应」条款与收敛条款在此共同成立

#### Scenario: build-tag 异组与 test-support 不参与判定

- **WHEN** `//go:build soak`（或 `integration`）文件与默认 tag 文件同锚点，或同锚点文件不含任何 `func Test`（桩/基座/纯基准）
- **THEN** 门禁 SHALL 将其排除在判定之外；MUST NOT 产生把 soak 用例并入默认 tag 文件这类物理上不可执行或语义错误的收敛要求

#### Scenario: 同一目录的内外部测试包不并组

- **WHEN** 一个目录同时持有内部测试包（`package memory`）与外部测试包（`package memory_test`）的文件，且它们声明同一 `契约:` 锚点
- **THEN** 门禁 SHALL 以包名分键、不作碎片判定——函数体跨编译单元平移必改限定符，与「合并不改变测试覆盖」的无损要求直接冲突

#### Scenario: 机检发现真碎片并列三出口

- **WHEN** 同键参与者 ≥2 且某文件无镜像（如 `meditation_audit_test.go` 与镜像文件 `telemetry_audit_test.go` 同键 `#telemetry-ladder`）
- **THEN** 门禁 SHALL 对该文件计一条 `responsibility-fragmentation` finding，消息中并列三条消除出口（并入镜像文件 / 改名对齐 / 文档侧锚点收敛），不得预设合并是唯一解

#### Scenario: 改名对齐镜像消除 finding

- **WHEN** 命名错位型碎片（如 `action_test.go` 对生产 `action_tool.go`）经改名对齐镜像
- **THEN** 改名 MUST 经映射表登记旧名→新名并同步外部引用，门禁 SHALL 对其停止计数；受影响包测试清单计数 SHALL 不变

#### Scenario: 文档侧锚点收敛消除 finding

- **WHEN** 碎片的成因是锚点粒度（一个 wiki 小节罩住整族，或 e2e 文件挂单元级锚）
- **THEN** SHALL 以文档侧收敛（小节上收/细化/重挂）消除 finding；锚点 MUST 继续满足既有 `index-anchor-*` 硬门（指向真实存在的小节），MUST NOT 借机铸造无实体小节的逃逸锚

#### Scenario: 棘轮只降不升与归零切硬

- **WHEN** 一批收敛使违规文件数下降
- **THEN** 基线 SHALL 经 `lint.sh --update-baseline` 下降并随批提交；任何使计数上升的改动 SHALL 被 CI 拒绝；计数归零后 SHALL 移除基线槽位，此后任何新增 finding（即回到 >0）SHALL 直接失败，无需二次立法

#### Scenario: 收敛批提交触发无损校验

- **WHEN** 一次收敛批的提交暂存了 `_test.go` 移动/改名（携带映射表）
- **THEN** pre-commit SHALL 以 HEAD 为基线调用无损校验（生产码零改、测试函数逐一对应、断言数不降）；校验不过 SHALL 阻止提交

### Requirement: 测试标识不承载迭代编号

测试文件名与测试/基准函数名 SHALL 以被测职责与工况语义命名，MUST NOT 含变更任务号、批次号、review 小节号、日期或「回归/修复第 N 轮」式代号（此类编号在其所属变更归档后不可解）。重命名 MUST 提供旧→新映射表，并同步所有外部引用（文档、脚本、CI 过滤器），旧标识在仓库内的引用残留 SHALL 为 0。

#### Scenario: 编号标识改名

- **WHEN** 测试标识形如「任务号＋主题」或「字母加数字代号」
- **THEN** 应改为职责语义名（说明测哪个契约、何种工况），并在映射表登记旧名→新名；`go test -list` 计数不变，过滤器命中真实测试名

#### Scenario: 期望移入可执行位置

- **WHEN** 原注释或编号承载的是「期望什么」
- **THEN** 期望 SHALL 由测试名、子测试名与断言消息承载；长期判据 SHALL 迁入 `docs/wiki/` 或 `openspec/specs/`，代码内只留一行索引

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

