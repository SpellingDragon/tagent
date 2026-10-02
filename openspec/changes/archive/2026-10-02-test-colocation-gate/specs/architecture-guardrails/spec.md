## MODIFIED Requirements

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
