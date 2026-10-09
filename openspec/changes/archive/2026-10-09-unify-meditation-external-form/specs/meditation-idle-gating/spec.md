# meditation-idle-gating Specification（delta）

## REMOVED Requirements

### Requirement: 新颖性锚点锚定输入侧

**Reason**: 双判据退役，novelty 唯一来源为观察面谱系水位判据（见 meditation-agent-partition）；`lastUserInput` 锚与注入侧挂臂机制整体删除。
**Migration**: 无数据迁移。原"用户输入武装冥想"语义由缺省观察面（自身分区）承担——用户消息入库即 user 谱系非自管事件，触发时机语义等价且覆盖面更准（system_alert 等非自管输入同样计入）。

## MODIFIED Requirements

### Requirement: 冥想触发采用双闸门判定

触发一次反思 SHALL 同时满足：① interval 自查节奏到期；② 自身空闲门：距本 agent 最近一次回合结束（**任何谱系**，含投递触发的回合与失败回合）≥ min_gap；③ novelty 门：观察面内存在水位之后的**非自管谱系**事件。观察面缺省为**[自身分区]**（不配置 `observed_namespaces` 时，任何配了 `meditation.enabled` 的 agent 反思自己的分区——与旧 in-loop 行为等效且零迁移）；显式声明可含自身与他人（他人须 ⊆ `read_namespaces` 授权）。任一门不过 SHALL 以 debug 级具名记录。反思动作恒为向**本 agent 循环的 session** 注入冥想输入事件——形态差异只是"哪个 agent 的 session、观察谁"。

#### Scenario: 三门齐备触发

- **WHEN** interval 到期、本 agent 空闲 ≥ min_gap、观察面水位后有非自管事件
- **THEN** 注入 `source="meditation"` 反思输入到本 agent 的 session 并推进水位

#### Scenario: 缺省观察面等效 in-loop

- **WHEN** 入口 agent 仅配 `meditation.enabled`（无 observed_namespaces），其业务 session 收到用户消息并入库
- **THEN** 下一个空闲窗口 novelty 门开、反思注入业务 session（共享会话上下文，自体维护）

#### Scenario: 投递风暴期只推迟

- **WHEN** 大量投递使本 agent 持续繁忙且观察面无新非自管事件
- **THEN** 空闲门与 novelty 门均不过，至多推迟反思

### Requirement: 门控不依赖输出侧血统追踪

冥想门控 SHALL NOT 依赖输出事件、任务层 `Origin` 行李或 task_settled 的血统标记；事件回调（`makeOnEventCallback`）SHALL NOT 更新冥想锚点。novelty 判据读取的 `trigger_source` 是**提交时盖章在事实链上的持久归因**（入库路径的既有部分），属"输入侧事实"而非输出侧追踪；除此之外门控不引入任何输出侧读取。

#### Scenario: 事件回调与锚点解耦

- **WHEN** 任意 trigger_source 的 final response 经过事件回调
- **THEN** 冥想锚点不因该回调而变化

#### Scenario: 唯一 novelty 读径是事实链

- **WHEN** 审查 novelty 判定路径
- **THEN** 其数据来源仅 QueryEvents/GetEvent 的持久归因，无输出事件或回调读取
