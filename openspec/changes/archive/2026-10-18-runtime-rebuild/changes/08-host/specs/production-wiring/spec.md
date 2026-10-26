# production-wiring delta（08-host）

> 本 delta 只承载**新增条款**；既有 Requirement（FileSegmentStore 生命周期子系统装配、setter 注入、Close 顺序、配置严格解析、agent 引用环检测）语义不变，**继承**（见 `../proposal.md` §继承）。

## ADDED Requirements

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
