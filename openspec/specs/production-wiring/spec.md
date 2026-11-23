# production-wiring Specification

> **勘误（2026-11-16 契约同步核验；2026-11-19 复核补记）**：本规范原「生产入口创建并启动生命周期组件」「子系统引用经 setter 注入」「关闭按序停机」三条条款要求的墓碑集、遗忘管理器、压实器与生产入口名在 Go 树零定义，其装配面已整体退役；本规范现按现行开档入口重写。

## Purpose

本规范定义 production-wiring 能力：生产入口如何按声明的档位打开存储、由该开档派生哪些仓与接口，以及关闭时收尾的次序；配置严格解析、agent 引用环检测与运行时唯一公共门面的要求也在此。

## Requirements

### Requirement: 存储装配经单一开档入口派生

生产入口 SHALL 经一个开档入口按声明的档位打开引擎，并由该引擎派生原文仓与事件段仓，三者同属一个存储层视图；MUST NOT 另立第二套开档路径、第二个开档权威，MUST NOT 为某一档位单独装配一套生命周期组件。可写视图 SHALL 在打开时取得该根的独占写权，同根第二个可写视图 MUST 被具名拒绝；能力声明 SHALL 由该视图自报，不承诺跨进程恢复的档位 SHALL 在装配面被告警，MUST NOT 被当作具备恢复面。

#### Scenario: 一次开档派生三面

- **WHEN** 生产入口按声明的档位打开一个存储根
- **THEN** 该次打开 SHALL 返回引擎、原文仓与事件段仓三面，且后续提交与读取只经这三面进行
- **AND** 存储层视图 SHALL 自报其能力，非持久档位 SHALL 在装配日志中具名告警

#### Scenario: 同根第二个可写视图被拒

- **WHEN** 同一存储根上已有持写权的视图，再以写权打开一次
- **THEN** 第二次打开 SHALL 具名失败，MUST NOT 并存两个写者
- **AND** 只读视图 MAY 与写者并存，且 MUST NOT 因并存而改写该根

### Requirement: 关闭是一次提交屏障后结束视图

关闭 SHALL 以一次空批提交作为屏障收尾，然后结束该视图持有的引擎，并释放该视图在存储根上取得的写权，使下一个写者被接纳；写权的释放 MUST NOT 以屏障成功为条件。关闭 MUST NOT 依赖任何后台扫描、压实或墓碑子系统停机，亦 MUST NOT 为关闭另立第二条落盘路径。屏障失败与结束失败的任一者 SHALL 如实上报，MUST NOT 以另一者的成功掩盖。

#### Scenario: 关闭先过屏障再结束引擎

- **WHEN** 关闭一个存储层视图
- **THEN** 其实现 SHALL 先以一次提交屏障收尾，再关闭该视图持有的引擎，并交出该根的写权
- **AND** 屏障失败时该根的写权 SHALL 仍被交出，MUST NOT 留下已关闭视图仍占着根

#### Scenario: 关闭重复调用不重复结束

- **WHEN** 对同一视图再次关闭
- **THEN** 第二次关闭 MUST NOT 重复结束该引擎，且 MUST NOT 报出首次关闭未报的错误

### Requirement: 配置严格解析

tagent.yaml 解析 SHALL 采用 strict 模式（KnownFields）：出现未知字段 SHALL 使加载失败，错误信息列出全部未知字段名；字段拼写错误 MUST NOT 被静默忽略。仓库内全部随载 yaml（示例/测试/资源）SHALL 通过 strict 解析。

#### Scenario: 配置键拼写错误

- **WHEN** yaml 含拼错的键（如 `wroking_dir`）
- **THEN** 启动报配置错误并列出该未知字段，进程不进入「配置被忽略、行为悄然偏离预期」状态

### Requirement: agent 引用环构建期检测

buildAgent 递归装配 SHALL 携带 visited 集：agent 引用构成环（A↔B 或自引用）时 SHALL 返回明确配置错误（指明环上的 agent 名），MUST NOT 以栈溢出崩溃。

#### Scenario: 配置声明了引用环

- **WHEN** yaml 中 agent a 的 tools 引用 agent b、b 又引用 a
- **THEN** 构建返回形如 `agent "a" <-> "b" reference cycle detected` 的配置错误

### Requirement: 公开运行时入口在根包装配入口上完善，不重复建立运行时

公共运行时入口 SHALL 位于 module 根 `package tagent`（import 路径 `github.com/SpellingDragon/tagent`），是**唯一**公共门面；系统 MUST NOT 同时提供 `tagent/` 同名第二层门面或另建第二套组合根。公开 SDK/CLI 的构造/启动/关闭/诊断查询 SHALL 建立在**根包最小公共装配入口**（首次由执行域 `02.5` 使用）之上，宿主 MUST NOT 绕过该入口直连内部机制另起运行时。

集成测试 SHALL 经根包公开面驱动运行时，MUST NOT import `prototype` 包。

#### Scenario: 唯一公共门面

- **WHEN** 审查运行时公共入口
- **THEN** 公共 `package tagent` 位于 module 根；不存在并行的嵌套同名门面；宿主经该入口构造运行时

#### Scenario: 集成测试不依赖历史骨架

- **WHEN** 编译/运行 `tests/integration/host` 下的宿主集成测试
- **THEN** 其 import 图中不含 `prototype`；测试经根包公开面驱动真实运行时

#### Scenario: 不重复建立运行时

- **WHEN** 宿主在根包装配入口之外试图自建 org/state/task 组合
- **THEN** 评审/结构断言拒绝：宿主只消费公共入口，不复制运行时
