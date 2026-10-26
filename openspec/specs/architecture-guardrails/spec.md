# architecture-guardrails Specification

## Purpose

以结构而非纪律承载正确性（implementation-hardening 7A 立法）：分层依赖方向可机械断言（root → agent → plugin → memory，event 为纯叶子）；对上游 trpc-agent-go 内部行为的关键假设（投影完备性所依赖的管线同步等待）以真实管线钉测锁定，升级破坏即红；变更局部性（一个意图一个落点）与可选项单点拆除为内聚/演进的准入准绳。
## Requirements

### Requirement: 分层依赖方向可机械断言
架构检查 SHALL 以人工登记的 golden/runtime/architecture.json 为准，枚举第一方产品包及全部子包，逐一给出归属层并登记进允许依赖集（人工显式清单）。组合根 MUST 位于module根且显式登记允许边；未登记子包、未知内部边、循环依赖和缺必需包均须具名拒收（默认拒绝：新包先登记归属再合入）。政策表不得从现存残件自动学习；覆盖证据 MUST NOT 用 `tagent: *`、`tagent/...`、通配或前缀折叠掩盖未覆盖子包，每个子包 SHALL 单独可判定。纯声明不得依赖运行时，事实和任务提交不得反向依赖执行器。

#### Scenario: 未登记子包或反向依赖
- **WHEN** 新增未登记子包或memory反向import agent
- **THEN** check_runtime_architecture --implementation非零退出并标明包与边，政策模式通过不能覆盖失败

#### Scenario: 通配不得掩盖未覆盖子包
- **WHEN** 覆盖清单以 `tagent: *` 或前缀折叠声称全绿，而某子包实际未被逐一判定
- **THEN** strict 结构门判失败并具名该未登记子包，MUST NOT 以通配静默放行

#### Scenario: 包枚举命令失败
- **WHEN** 递归go list失败或依赖缺失
- **THEN** 检查保留原始退出码并拒绝结构通过，不解析残缺输出伪造成功

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
组合根 SHALL 构造候选模型、原生工具及插件；state SHALL 持有和发布生效绑定，请求只借用同代绑定。执行器、Query和训练观察 MUST NOT 复制可写运行现场或反向依赖根包。共享端口由实际消费方审定，持久记录不得含指针、闭包或连接。包与边按真实职责设计，不强制复活旧agent子包机制。

#### Scenario: 候选失败或在途换代
- **WHEN** 候选构造失败或发布时存在旧请求
- **THEN** 候选独占资源释放，state生效代不受失败候选影响，在途请求保留旧代直到实际使用结束；由01.4/01.5和金样S01取证

#### Scenario: 共享接口未被消费者验证
- **WHEN** G1只有类型草案而没有生产构造链和最小原生签名探针
- **THEN** 保持design-blocked，不为避免import建立同形影子协议

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

根包（组合根）SHALL 只承载装配面职责：编排装配、配置装载、注册表、提示词引用、面向外部消费者的入口，以及资源治理与世代治理的**装配注入点**。资源租约治理的实现 SHALL 位于 agent 域子包；世代治理的机制实现（候选事务、热更执行、属主退役、分区碰撞消解、世代簿记与状态类型）SHALL 位于 agent 域子包，经该子包定义的壳构造契约由组合根**注入**协作，MUST NOT 以 import 根包的方式取得装配内部状态（既有分层断言继续机械生效）。世代簿记与状态类型经根包类型别名再导出时，别名 MUST 与实体同名且不新增语义。发布动作（orgCoordinator 的换入/发布/告警）SHALL 留在组合根。

仓库追踪内容 MUST 满足：追踪文件非空；追踪路径不命中运行期产物模式（锁文件、journal、tmp、prof 等）；追踪路径 MUST NOT 同时被 ignore 规则排除（白名单式 `.gitignore` MUST 显式命名每个要保留的追踪文件）。顶层目录集合 SHALL 以白名单固化于 CI，新增顶层目录 MUST 同步登记于 README 布局说明。测试运行 MUST NOT 在仓库工作目录落盘（临时数据走测试临时目录）。只用导出面的测试 SHALL 归位于外部测试包（`tests/`），引用未导出符号的灰盒测试归位于被测包内——判定以编译为准，不以静态扫描为准。**被测包内的灰盒测试 SHALL 按 wiki 页组织为一文件**（同页测试不拆多文件；文件级锚取该页主锚，节锚保留在各用例 doc 中），测试函数与断言在合并中 MUST 零损失（以逐名对账证明）。

#### Scenario: 运行残骸无法入库

- **WHEN** 一次测试或运行以相对路径在工作目录产出锁文件/journal 并被加入索引
- **THEN** 卫生门 MUST 以具名失败拒绝，指出文件与规则

#### Scenario: 资源治理实现回流根包

- **WHEN** 某次变更把资源租约治理（运行时资源的最后引用清理）实现写回根包而非 agent 域
- **THEN** 分层断言或评审 MUST 拒绝：根包持有的是注入点与接线，不是治理实现

#### Scenario: 灰盒测试被错误外移

- **WHEN** 一个引用根包未导出符号的测试被搬到外部测试包
- **THEN** 编译失败 MUST 被视为判定，而非通过导出该符号来"修复"——扩测试导出面是独立裁决

#### Scenario: 同页测试分散多文件

- **WHEN** 同一 wiki 页的灰盒测试分散在被测包内的多个文件
- **THEN** 组织为每页一文件；合并 MUST 以测试集合逐名对账（Test/Benchmark/Example 零丢失）与全量门禁绿共同证明

### Requirement: 机器消费的点号模块引用必须可解析

脚本与配置中会被运行时实际导入的点号模块引用（shell 的 `python -m` 参数、配置的 `workflow:` 值）SHALL 满足：点号换斜杠后能解析为仓库内的 Python 文件，或命中显式登记的外部包允许表。挂在已删除布局上的此类引用 MUST 在门禁处以具名失败暴露，MUST NOT 静默留存为"看着对"的死配置。

#### Scenario: 引用了不存在的模块布局

- **WHEN** 一条 `workflow:` 值或 `python -m` 参数指向的点号路径在仓库内无对应文件且不在允许表
- **THEN** 门禁 MUST 具名拒绝并给出该引用的位置

#### Scenario: 外部包引用需显式登记

- **WHEN** 引用目标是不随仓库分发的安装包
- **THEN** 它 MUST 出现在允许表内并附用途说明，未登记即红

### Requirement: 检查资产区分三类证据
规划检查、实际结构覆盖与运行行为证据 SHALL 分开输出。完成状态 MUST 具有命令、原始成功退出码、run-id和产物引用；这些字段存在仍不代表来源已认证。运行检查 MUST 核验真实测试run/终局、包终局及金样具体值，不能仅比较键名。

#### Scenario: 缺失或矛盾的运行记录
- **WHEN** 证据缺case、run、pass、包终局，或出现Skip/Fail、重复键、未知观测、错值
- **THEN** check_runtime_evidence非零退出；后补pass不能覆盖先前失败

#### Scenario: 自测负控
- **WHEN** 临时fixture分别注入未知子包、错值、缺终局和错误退出码
- **THEN** 检查器各自拒绝，unittest证明的是拒收逻辑，不勾选产品验收

### Requirement: 质量门迁移不得放宽判据
CI SHALL 覆盖next与既有集成分支，产品门覆盖根与微信两module、全部适用第一方子包race，Go版本不得低于1.24.1。旧规则引发碎片化时 MUST 先记录真实反例与正反测试，再修改规则，不提高baseline掩盖错误。

#### Scenario: 本轮只能生成检查资产
- **WHEN** 规划和新检查workflow已可运行而旧产品树仍不完整
- **THEN** 只报告检查资产完成，00.6完整迁移及09.3全量产品门保持未完成，不删除旧门

### Requirement: 事件完整性显式测试清单与共用执行引擎

规划/回归资产 SHALL 新增 `golden/runtime/event_integrity_scenarios.json`，以 `schema_version: 2` 声明八类机制（E1—E8）、**全部产品测试及其全部必跑子场景**、模块/包/build tags/race/档位与六组合矩阵。计划回写④后 graph 显式清单 SHALL 为 28 个绿测产品测试 + 1 个诊断测试（00.2）+ 137 个必跑绿具名子场景 + 4 个诊断锚；最终验收共 29 个父测试 / 141 个具名锚（`event-short` 28 父/138 锚，`event-long` 1 父/3 锚）。`subtests[]` SHALL 以 `(parent,name)` 为唯一键，每个键仅登记一条；`roles[]` MAY 同时含多个角色；`tests[].consumer_anchor` MUST 引用同父测试下 roles 含 `consumer` 的已登记行（可兼具 positive 角色，也可为独立消费者场景），MUST NOT 复制 consumer 行。137/141 等计数 SHALL 按唯一键计，MUST NOT 按 positive/negative/consumer 角色引用求和。重复实体注册 MUST 被拒绝；合法多角色引用（如 `positive`+`consumer`）MUST NOT 被当作重复拒绝。00.2 修复前红证 SHALL 单列，修复后四锚 SHALL 纳入最终 short。数量 SHALL 由 graph 清单长度导出，MUST NOT 写死“十个 EI 场景”。`EI01—EI10` SHALL 仅作为 03 的 `whole_chain` 整链组（`aggregate_only: true`），MUST NOT 替代 00/01/02/CLI 的独立测试与负锚，MUST NOT 计入测试/子场景数量。非产品叶（00.1/00.3/00.4、03.10、04.1—04.5、05.1—05.4）SHALL 通过 `non_product_leaves[]` 映射 checker 或文档证据，MUST NOT 被要求提供虚构产品测试。旧 `golden/runtime/scenarios.json` 的 S01—S15 及其 deterministic 归属 MUST 逐字保留，MUST NOT 因新合同改写、删除或重算。

新旧合同 MUST 共用现有 `run_runtime_scenarios.py` 与 `check_runtime_evidence.py` 执行/对账引擎，MUST NOT 新建平行评估系统或第二份合同解析器。新合同 SHALL 支持独立模块、build tags、`-race`、`--repeat N` 独立重复与 `--seed`/`--window` 矩阵；每个派生命令 MUST 固定 `-count=1`，每次独立日志、UUID run-id 与证据，单轮内出现重复 run 仍判失败。runner SHALL 提供 `--test-id <leaf>/<TestName>` 精确子集用于开发/生产验收：合同 `tests[].id` 固定 `leaf/TestName`（父测试级）并与 graph `test_registry[].contract_id` 同形，未登记或传子场景名即用法错误。CI 的 MR 面 MUST 使用完整 scope，MUST NOT 以 `--test-id` 缩小必跑面。证据 SHALL 携带源码树与运行配置指纹（受影响第一方源码、必要未跟踪测试、合成配置/fixture、合同、各模块 `go.mod/go.sum`），MUST NOT 只记 HEAD。skip、缺终局、包级失败、缺场景、截断、零匹配、伪造期望记录，以及把同一执行凭据冒充不同独立轮次/不同 matrix 组合的“证据复用” MUST 判失败。

#### Scenario: 八类机制由必跑子场景覆盖

- **WHEN** 合同检查器读取新合同
- **THEN** E1—E8 每族至少被一个 `mandatory: true` 子场景覆盖；每个产品测试至少 1 个 `positive` 与 1 个 `negative` 角色，且 `consumer_anchor` 指向同父测试下 roles 含 `consumer` 的已登记行（可兼具 positive 角色，也可为独立消费者场景）（不另增重复行）；缺族、某族只被 `whole_chain` 覆盖、缺负锚、空 selector、产品测试缺 consumer anchor 或 tier/scope 不一致均非零退出

#### Scenario: 显式清单不因固定十场景漏项

- **WHEN** 合同与 graph 冻结清单（`test_registry` + `leaves[].verification`）、源码 `TestEventIntegrity*` 父测试/`t.Run` 发现清单及执行日志三方交叉对齐
- **THEN** 28 个绿测、1 个诊断测试、137 个必跑绿子场景与 4 个诊断锚按唯一 `(parent,name)` 键逐项对齐且 141 锚/29 父可导出（不按角色引用求和）；缺一条、改名、改 roles、把 mandatory 降为 false、重复实体注册、增加未登记条目或把 EI 当独立测试计数均失败；合法多角色引用不被拒绝；不另造第二份 `EXPECTED_EVENT_MANIFEST` 元组真源，计数由 graph/发现清单长度导出而非手填常数

#### Scenario: 审查删除登记项被拒

- **WHEN** 有人只从合同删除一个测试/子场景以换取通过
- **THEN** 合同与 graph/源码发现不再对齐，检查器非零；同时删除合同与 graph 条目的 diff 无法由机器判定为任意两处篡改，MUST 由独立 diff 审查拒收，不得以“数量减少后仍自洽”放行，也不得声称机器可防双删

#### Scenario: 旧 15 场景不减少

- **WHEN** 新旧合同同时存在并分别检查
- **THEN** 旧合同仍为 15 项、旧命令形态与 deterministic 行为不变；新合同不得以减少旧项换取通过

#### Scenario: 独立重复与单轮唯一

- **WHEN** `event-short` 以 `--repeat 10` 执行，或 `event-long` 以六组合执行
- **THEN** 产生 10 份/6 份独立日志、UUID run-id 与 `repeat_index`，全部成功才通过；同一日志内重复 run/pass、五组合冒充六组合、9 次冒充 10 次或任何 `-count>1` 均失败

### Requirement: 直接调用归属检查不冒充完整调用图

架构门 SHALL 保留 `golden/runtime/architecture.json` 与 `scripts/check_runtime_architecture.py --implementation` 的包依赖方向判据；事件写入/消费归属 SHALL 以人工冻结的 `event_ownership` 表为输入，由 `scripts/codetools` 使用 Go 标准库（`go list`、`go/parser`、`go/types` 与类型信息）核对冻结所有者的**可解析直接调用点**。允许调用者放行，禁止调用者命中 MUST 非零；owner 无法解析、类型信息缺失、接口/反射/间接调用 MUST 计入 `uncovered` 并如实输出（`file:line` + 调用者 + 原因），MUST NOT 计为已覆盖，MUST NOT 用名字相似或字符串相邻补洞。`uncovered` SHALL 由独立复核记录逐条判定可达性，MUST NOT 静默放行。政策表 MUST NOT 从现存代码自动学习。该检查 MUST NOT 声称构建了完整调用图，也 MUST NOT 声称可静态识别任意“第二状态仓”语义；语义级重复状态判断留给 00.3 与独立评审。该检查 MUST NOT 替代包依赖门；`scripts/lint.sh` SHALL 调用该检查作为正式消费者。

#### Scenario: 合法直接调用通过

- **WHEN** 冻结 owner 的直接调用者均在 `allowed_callers` 内
- **THEN** 归属检查与包边检查均通过，退出码为 0；名字相似但非目标的调用不被误判

#### Scenario: 可解析的禁止直接调用被拒

- **WHEN** 出现对冻结 owner 的、类型信息可解析的 `forbidden_callers` 直接调用
- **THEN** 归属检查非零并具名该调用者与 owner，包边通过不能覆盖失败；检查器不把该调用描述为“第二状态仓”语义结论

#### Scenario: 无法解析的调用如实暴露

- **WHEN** 某调用点的类型信息不可得，或经接口/反射间接调用
- **THEN** 该调用进入 `uncovered` 列表并附 `file:line`、调用者与原因；检查器不得把它计为已验证；冻结 owner 本身无法解析时直接失败；`uncovered` 逐条独立复核，不以“工具没报”冒充覆盖

### Requirement: 故障变异必须由具名业务断言杀死

故障变异门 SHALL 在隔离树或 Go overlay 中恢复**已被 00.3 或后续复现证实**的缺陷形态；五类候选（双路正文、忙时忽略、身份破坏、提前确认、错误路由）MUST NOT 在未证实前启用。变异清单 SHALL 随 `scripts/check_event_integrity_mutations.py` 保存，MUST NOT 依赖进行中 change 目录存活。每个 00.3 `confirmed` 缺陷 MUST 恰有一个 mutation `basis` 指向；00.3 新证实的实际缺陷 MUST 登记，MUST NOT 漏项；未证实候选 SHALL 标为 `not_evidence_backed`，MUST NOT 计入 killed。检查 SHALL 先证明控制树在正式 selector 下全绿，再逐个应用变异并运行同一正式测试。只有进程非零且输出命中预先登记的具名业务断言 `expect_assertion` 时，才可记为 killed；编译失败、超时、零测试命中、panic 但无该断言、测试被改绿 MUST 判该变异验证失败，MUST NOT 计为成功杀死。未知/失效变异、清单与源码锚点失配、控制树不绿 MUST 使总门失败。原始证据 SHALL 记录基线源树摘要、实际补丁/overlay 摘要、实际命令与原始退出码，并写入 `--out` 指定的仓库外临时目录；证据摘要与 `log_ref` MAY 记入根 `ORCHESTRATION.md` 台账。

#### Scenario: 控制树先绿

- **WHEN** 变异检查启动
- **THEN** 未变异的控制树先以正式 selector 跑通；控制树失败即总门失败，不继续用变异结果充数

#### Scenario: 具名断言杀死变异

- **WHEN** 应用一个已被 00.3 证实的变异并运行对应正式测试
- **THEN** 测试非零且输出包含该变异登记的 `expect_assertion`，才记为 killed

#### Scenario: 编译失败或超时不算杀死

- **WHEN** 变异导致编译失败、超时、零测试命中、仅 panic 或测试被改绿
- **THEN** 该变异判“验证失败”，总门非零，不把它报告为成功拦截

#### Scenario: 未证实形态不得凑数

- **WHEN** 00.3 未证实某候选形态，或新证实缺陷没有对应 mutation
- **THEN** 未证实候选不计 killed；新证实缺陷缺映射使自测/总门失败，不为凑五类而虚构变异

### Requirement: 叶—测试—子场景—证据映射与 lint 对象一致

回归门 SHALL 将每个叶任务、spec scenario、Golden 条目、真实 test selector、具名子场景、正式消费者与必跑档位逐项对齐：00—05 共 42 叶 MUST 各自被 `tests[].leaf` 或 `non_product_leaves[].leaf` 恰一次引用；产品叶 MUST 用测试映射，非产品叶 MUST 用 checker 或文档证据映射，不得互相冒充。每个 `tests[].leaf` MUST 存在于活动 `task-graph.json`；每个 `mandatory: true` 子场景 MUST 在所选测试文件中以真实 `t.Run("<name>"` 存在；每个产品测试 MUST 有指向正锚行的 consumer anchor 与非空 selector；`subtests[]` MUST 以 `(parent,name)` 为唯一键，`roles[]` 可多角色且不重复计数；`whole_chain[].test_id` MUST 存在且 `aggregate_only: true`。合同 MUST 与 graph 冻结清单、源码发现清单及执行日志三方对齐，改名、删除、空 selector、缺必跑子场景、缺非产品映射、证据复用均失败。同一 `(package,test)` 可服务多个整链组，但每个 run MUST 有独立 record；同一执行凭据（run_id/日志真实路径/artifact）MUST NOT 冒充不同独立轮次或不同 matrix 组合，同一轮内多个 case MAY 共享同一条日志，但每个 case MUST 按具名 selector 独立核验。`scripts/lint.sh` SHALL 在存在活动 `task-graph.json` 时检查该活动 graph，MUST NOT 固定检查归档 graph；无活动 graph 时 SHALL 明确 skip，MUST NOT 伪造通过。新检查 MUST 保留旧十域/旧 15 场景规则，MUST NOT 全局放宽编号、数量或场景约束。

#### Scenario: 改名、缺叶或缺必跑子场景失败

- **WHEN** 叶被改名/删除，或某测试的 negative 角色、consumer anchor 指向未登记的子场景、非产品叶映射缺失、selector 为空或在测试文件中不存在
- **THEN** 映射门非零并具名缺项，不允许以父测试通过或键名对齐替代

#### Scenario: 证据复用失败

- **WHEN** 同一执行凭据（run-id/日志真实路径/artifact）被用来冒充不同独立轮次或不同 matrix 组合
- **THEN** 证据检查判失败，后补 pass 不能覆盖先前失败；同轮多个 case 合法共享同一日志，但每 case 仍须按具名 selector 独立核验

#### Scenario: bundle 汇总同源

- **WHEN** `check_runtime_evidence.py --bundle` 读取 short/long/mutations 证据
- **THEN** short 恰好 10 份、long 六组合齐全、mutations 恰一份、基线源树摘要一致、同一执行凭据不得冒充不同独立轮次/不同 matrix 组合；缺件、空目录、截断、skip、重复 run 均失败

#### Scenario: lint 检查活动对象

- **WHEN** 仓库存在活动 `openspec/changes/*/task-graph.json`
- **THEN** `lint.sh` 按该活动的字符串 `domain_profile`（当前 `event-integrity`，指向固定六域规则；叶数/范围/阶段门一律读 graph，不在 profile 复制对象状态）检查计划/合同/证据；活动 graph 存在却检查归档 graph 即失败

#### Scenario: 旧规则不被放宽

- **WHEN** 新六域 profile 生效
- **THEN** 旧变更仍按冻结的十域、旧叶编号与阶段门校验；不得为新变更全局放宽或跳过旧负控
