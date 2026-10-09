# meditation-agent-partition Specification

## Purpose
TBD - created by archiving change externalize-meditation-session. Update Purpose after archive.
## Requirements
### Requirement: 冥想 agent 经配置声明并以授权分区为观察面

冥想 SHALL 可配置于 `agents:` 下的任何 agent（无新 agent 类型/运行时机制）。观察面解析：`observed_namespaces` 显式声明时 SHALL ⊆ `read_namespaces` ∪ {自身分区}（自身恒合法，他人须授权，越界具名拒绝启动）；**缺省（未声明）＝[自身分区]**——入口 agent 配冥想即自察反思，独立策展 agent 显式列他人分区即跨域策展，同一机制。观察面变更走结构换代。

#### Scenario: 缺省自察

- **WHEN** agent 配 `meditation.enabled` 而未声明 `observed_namespaces`
- **THEN** 观察面=[自身分区]，反思触发条件为自身分区水位后的非自管事件

#### Scenario: 混合观察面

- **WHEN** `observed_namespaces` 同时含自身与他人授权分区
- **THEN** 任一观察分区水位后的非自管事件均可开门（自察+跨域一体）

#### Scenario: 越界授权拒绝

- **WHEN** 观察面含未授权的他人分区
- **THEN** 装配期具名拒绝启动（不静默剔除）

### Requirement: novelty 判据为跨分区非自管谱系新事件

novelty SHALL 定义为：观察面（缺省自身分区）内存在 `Timestamp > lastMeditation` 且谱系非自管的事件。自管判定 SHALL 调用 event 包 `SelfManagedLineage` 单源派生，MUST NOT 在消费方复刻清单。数据面 SHALL 使用 `memory.QueryEvents` 时间窗查询后逐条 `GetEvent` 水合过滤（`EventReference` 无 Metadata 为已知边界），命中即早停；`trigger_source` 缺失/未知 SHALL NOT 计入（保守：宁可少反思）；查询失败/未接读缝 SHALL NOT 计入且门保持关闭（fail-closed，debug 具名）。

#### Scenario: 跨分区用户事件触发

- **WHEN** 观察面内任一分区有新 user 谱系事件晚于水位
- **THEN** novelty 门打开，配合自身空闲门满足后触发反思

#### Scenario: 防永动（自管产出不计入）

- **WHEN** 仅有自管谱系产出（meditation 卡片、consolidation_hint、未知谱系）而无新非自管事件
- **THEN** 无论多少空闲窗口 novelty 门保持关闭——反思链路在结构上不可能自持

#### Scenario: 读失败不猜测

- **WHEN** QueryEvents/GetEvent 返回错误
- **THEN** 本轮不触发、门保持关闭并具名 debug；不回落任何备用判据

### Requirement: 门控与锚点沿用既有语义

门控三件 SHALL 为唯一形态定义：触发节奏 interval、自身空闲锚 `lastTurnEnd`（任意谱系回合结束即更新，失败回合同样计忙）、novelty 水位锚 `lastMeditation`（有效触发时推进并自锁，无需额外重置）。锚点持久化 SHALL 用 AnchorStore **两锚结构**（lastTurnEnd/lastMeditation）；历史三锚文件中的多余键 SHALL 被忽略（自然兼容，无需迁移）。

#### Scenario: 锚点跨重启

- **WHEN** agent 重启且旧三锚文件在位
- **THEN** 两锚正常恢复、多余 `last_user_input` 键忽略；不立即误触发

#### Scenario: 触发后自锁

- **WHEN** 有效反思触发且此后观察面无新非自管事件
- **THEN** 无论经过多少 interval 窗口均不再触发

### Requirement: 冥想 agent 的产出为普通事件

外部化冥想的产出（经验卡片/综述）SHALL 以普通事件写入**自身分区**的事实链；MUST NOT 写 compaction 事件（单压缩权不可转移）、MUST NOT 直接修改任何被观察分区的状态。对目标上下文的影响 SHALL 只经"目标自然折叠吸收共享事实链中的新事件"间接发生。

#### Scenario: 产出落自身分区

- **WHEN** 冥想 agent 完成一轮跨域巩固产出卡片
- **THEN** 卡片事件在冥想 agent 自己的分区；目标分区零写入

### Requirement: 反思事件注入本 agent 的循环 session

冥想管理器的触发动作 SHALL 恒为向本 agent 常驻循环的 session 注入冥想输入事件（`source=meditation`）——不跨 agent 注入、不依赖投递缝。session 安排约定：业务线 session 沿用宿主路由；独立策展 agent 的反思线使用保留 session 名（推荐 `meditation`（冥想语族一致；与谱系常量 `meditation` 属不同命名空间——session 是循环身份、trigger_source 是事件属性，亲和非冲突）），固定单线（思路连续、增长由自身阈值折叠管理），永不与用户路由 session 撞名。

#### Scenario: 注入目标恒为本循环

- **WHEN** 任一 agent 的反思触发
- **THEN** 冥想事件进入该 agent 自己的 mailbox/session；无跨 agent 写入路径

#### Scenario: 策展线固定 session

- **WHEN** 策展 agent 多次反思
- **THEN** 反思均发生在同一保留 session（如 `meditation`），跨重启经投影重建延续

