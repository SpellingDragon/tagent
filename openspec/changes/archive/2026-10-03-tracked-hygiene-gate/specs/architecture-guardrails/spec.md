## MODIFIED Requirements

### Requirement: 组合根物理边界与追踪卫生

根包（组合根）SHALL 只承载装配面职责：编排装配、配置装载、注册表、资源与提示词引用、面向外部消费者的入口。世代治理的机制实现（候选事务、热更执行、属主退役、分区碰撞消解）SHALL 位于 agent 域子包，经该子包定义的壳构造契约由组合根**注入**协作，MUST NOT 以 import 根包的方式取得装配内部状态（既有分层断言继续机械生效）。

仓库追踪内容 MUST 满足：追踪文件非空；追踪路径不命中运行期产物模式（锁文件、journal、tmp、prof 等）；**追踪路径 MUST NOT 同时被 ignore 规则排除**（"追踪但隐形"的文件其新增姊妹文件永不入库、修改需 `-f`，白名单式 `.gitignore` MUST 显式命名每个要保留的追踪文件）。顶层目录集合 SHALL 以白名单固化于 CI，新增顶层目录 MUST 同步登记于 README 布局说明。测试运行 MUST NOT 在仓库工作目录落盘（临时数据走测试临时目录）。

#### Scenario: 运行残骸无法入库

- **WHEN** 一次测试或运行以相对路径在工作目录产出锁文件/journal 并被加入索引
- **THEN** 卫生门 MUST 以具名失败拒绝，指出文件与规则

#### Scenario: 追踪文件同时被 ignore 规则排除

- **WHEN** 一个已在索引中的路径命中某条 `.gitignore` 的排除规则（例如白名单式 `.gitignore` 未点名该文件）
- **THEN** 卫生门 MUST 具名拒绝，处置为"去掉该 ignore 规则或在白名单点名"，MUST NOT 靠 `git add -f` 长期共存
