# meditation-agent-partition Specification（delta，MODIFIED 于既有能力）

> 头逐字匹配主 spec。本 delta 做四件事：把主语统一为 r14b 唯一版（配置冥想插件的 agent，缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）、废除"本 agent 循环 session"旧约、把五不变量接到 `meditation-autonomy`、把旧门控形态改为状态机持有（否则与 idle-gating 退役矛盾）。其余既有条款（观察面授权、产出为普通事件、新颖性规则）原文保留。

## RENAMED Requirements

- FROM: `### Requirement: 反思事件注入本 agent 的循环 session`
- TO: `### Requirement: 反思事件注入本 agent 的专属反思会话`
- FROM: `### Requirement: 门控与锚点沿用既有语义`
- TO: `### Requirement: 门控与锚点由状态机持有`

## MODIFIED Requirements

### Requirement: 冥想 agent 经配置声明并以授权分区为观察面

**主语（r14b 唯一版）**：配置冥想插件的 agent（缺省宿主自身，反思会话如 `tagent-session:tagent:reflection`）。冥想 SHALL 可配置于 `agents:` 下的任何 agent（无新 agent 类型/运行时机制）。观察面解析：`observed_namespaces` 显式声明时 SHALL ⊆ `read_namespaces` ∪ {自身分区}（自身恒合法，他人须授权，越界具名拒绝启动）；**缺省（未声明）＝[自身分区]**——入口 agent 配冥想即自察反思，独立策展 agent 显式列他人分区即跨域策展，同一机制。观察面变更走结构换代。五不变量（用户让路／锚点重启安全／触发即真实事件可审计／自管产出不计入状态积累／影响他人只经 send）见 `meditation-autonomy`，本条款不重复定义。

#### Scenario: 缺省自察

- **WHEN** agent 配冥想插件的既有 `meditation` 节而未声明 `observed_namespaces`
- **THEN** 观察面=[自身分区]，反思触发条件为自身分区水位后的非自管事件

#### Scenario: 混合观察面

- **WHEN** `observed_namespaces` 同时含自身与他人授权分区
- **THEN** 任一观察分区水位后的非自管事件均可开门（自察+跨域一体）

#### Scenario: 越界授权拒绝

- **WHEN** 观察面含未授权的他人分区
- **THEN** 装配期具名拒绝启动（不静默剔除）

### Requirement: 反思事件注入本 agent 的专属反思会话

冥想管理器的触发动作 SHALL 恒为向**本 agent 的专属反思会话**注入冥想输入事件（`source=meditation`）——同一 agent，不跨 agent 注入、不依赖投递缝。反思会话 MUST 是宿主专属的保留会话（如 `tagent-session:tagent:reflection`），与业务会话分离、固定单线（思路连续、增长由自身阈值折叠管理），永不与用户路由会话撞名。**废除"本 agent 循环 session"旧约**：反思不再注入该 agent 的通用循环 session，而是注入专属反思会话；业务线 session 沿用宿主路由。独立策展 agent 的反思线使用其保留会话名（如 `meditation`）。

#### Scenario: 注入目标恒为专属反思会话

- **WHEN** 任一 agent 的反思触发
- **THEN** 冥想事件进入该 agent 自己的专属反思会话；无跨 agent 写入路径，也不进入业务会话

#### Scenario: 策展线固定 session

- **WHEN** 策展 agent 多次反思
- **THEN** 反思均发生在同一保留 session（如 `meditation`），跨重启经投影重建延续

### Requirement: 反思作为显式输入生产者与水位新鲜度

反思产出的触发动作 SHALL 以**显式输入事件**（`source=meditation`）注入本 agent 的**专属反思会话**，作为该 agent 的显式输入生产者，MUST NOT 以输出侧派生或隐式后台调用代替；消费者（digest/评估/宿主诊断）SHALL 能就同一事实面观察到该输入事实。

水位新鲜度 SHALL 满足：注入时刻 MUST NOT 推进水位，水位 SHALL 在反思 turn 被**消费**时推进（值取注入时刻）；让位（deferred）SHALL 记水位不动、pending 清零，被让位反思覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）；水位缺失/不可解析 SHALL fail-closed——门保持关闭并具名 debug，MUST NOT 回落任何备用判据或以"新鲜"默认放行。

#### Scenario: 反思输入可被消费者观察

- **WHEN** 一次反思有效触发
- **THEN** 观察面消费者（digest/评估/宿主诊断）可就该 agent 观察到一条 `source=meditation` 的显式输入事实；该事实不是由输出事件或任务层行李反推而来

#### Scenario: 让位不烧窗口（水位新鲜度）

- **WHEN** 注入的反思事件与真实事件同批被让位丢弃，随后观察面安静
- **THEN** 水位未推进、pending 清零；下一次状态机检查对**同一事实面**（含让位窗口期全部事实）重新评估并可触发，被让位反思覆盖的事实不低于水位

#### Scenario: 水位不可解析 fail-closed

- **WHEN** 锚点水位不可建立（来源未接/记录不可读/不可解析）
- **THEN** 本轮门保持关闭并输出具名 debug；不按"新鲜"默认触发、不回落备用判据
- **场景：首次无锚点记录的引导态**
  - WHEN 启动时锚点记录文件不存在（无历史执行） THEN 零水位引导放行一次，消费推进后按水位锁定，重复结算不得再次触发

### Requirement: 门控与锚点由状态机持有

冥想触发门控与状态锚点 SHALL 由 `meditation-autonomy` 的状态机持有：激活规则可多条（定时检查、新颖性、显式请求等），每条激活 MUST 对应真实事件并携带因果元数据；MUST NOT 再把 interval/双闸门/两锚定义为唯一形态。锚点持久化 SHALL 以 `AnchorStore` 扩展承载状态与水位；历史锚点文件的多余键 SHALL 被忽略（自然兼容，无需迁移）；重启 MUST 从锚点与已提交事实恢复，MUST NOT 以文件时间或猜测触发。

#### Scenario: 状态跨重启恢复

- **WHEN** agent 重启且旧三锚文件在位
- **THEN** 状态从 `AnchorStore` 恢复、旧多余键忽略，不立即误触发

#### Scenario: 多源激活

- **WHEN** 定时检查、新颖性或显式请求任一激活规则满足
- **THEN** 激活以真实事件落链并携带规则/积累窗/来源，状态机按规则复位
