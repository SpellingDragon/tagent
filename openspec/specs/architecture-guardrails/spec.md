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
