## ADDED Requirements

### Requirement: 启动门先于实施

变更 SHALL 在 W0 写就症状直译锚 S1（journal 物理有界）与 S2（boot 真量有界）且二者**红**，并冻结共享面契约（存活 权威定义、Stateful 接缝、退位 identity-read 不变量、验收真量四量、引擎自持索引切片）后，方可进入 W1 实施。

#### Scenario: 症状锚第一天红

- **WHEN** W0 完成
- **THEN** S1/S2 SHALL 已存在且复跑为红（今天无界），作为验收矩阵第一行；工程化目标 P1–P6 SHALL 只能追加，不得取代症状锚

#### Scenario: 共享面契约冻结

- **WHEN** 进入 W1
- **THEN** 存活 权威（checkpoint 点名 ∪ 尾 ∪ 义务租约 ∪ 链可达 blob）、Stateful 接缝签名、退位 identity-read 不变量、验收四量（indexEntries＝存活索引规模 len(s.index)，非 EventCount 全史标量）SHALL 已冻结，各域不得擅改

### Requirement: 波次解锁以复跑凭据为准

波次 SHALL 仅为汇报分组；子变更解锁 SHALL 以依赖孙任务的**复跑凭据绿**为准，不以汇报绿为准。触碰集相交的子变更 SHALL 合单原子落地。

#### Scenario: W1 合单原子

- **WHEN** 派发 01-task-custody / 02-bounded-open-index / 04-acceptance-real-measure
- **THEN** 三者 SHALL 合单串行落地（同触 boot 路径簇），不可并行派发；04 真量锚 SHALL 作为 01/02 的验证面

#### Scenario: 解锁凭据

- **WHEN** 某子变更宣称完成
- **THEN** 编排者 SHALL 复跑其 verify 三要素（正锚/负锚/消费者锚）确认 exit 0，方解锁下游

### Requirement: 链红线守护

变更 SHALL NOT 删除、覆盖或改写已提交事实（session-checkpoint-material/spec.md:44）。退位 SHALL 是搬迁/遮蔽，已搬迁事实 SHALL 仍按身份可读。

#### Scenario: 退位搬迁非删除

- **WHEN** 06 退位搬迁已结事实到冷层
- **THEN** GetEvent(key) 对已搬迁事实 SHALL 仍返回原字节（透明回冷层），SHALL NOT 因搬迁返回 missing；逻辑链 SHALL 恒完整可按身份重放

#### Scenario: 冻结归档零改动

- **WHEN** F1 契约面断言
- **THEN** openspec/changes/archive/** SHALL 零 diff（只附勘误不改写）；外来未跟踪文件 SHALL 不被触碰

### Requirement: 引擎索引自持不循环依赖

引擎层索引 SHALL 自持持久索引切片，SHALL NOT 从 checkpoint 派生引擎索引（checkpoint 自身是 journal 事实，anchorEntry 无 partition/seq，从 checkpoint 派生索引构成循环依赖）。checkpoint 托管的是消费者读态（投影幸存者/任务/governance），引擎索引是另一层。

#### Scenario: 索引切片自持

- **WHEN** 02 实现有界开档/索引
- **THEN** 引擎 SHALL 加载自持的索引切片＋尾增量，SHALL NOT 依赖读取 checkpoint 事实来重建引擎索引；索引切片与 checkpoint 切片分层不混

### Requirement: 三级验收与法定停点

变更 SHALL 经孙任务级（verify 三要素 exit 0）、子域级（spec Scenario 全绿）、变更级（DoD 可勾选）三级验收。commit/push 与归档 SHALL 待用户明示授权（R31 法定停点）。

#### Scenario: 症状锚红转绿两次证据

- **WHEN** F3 DoD 核验
- **THEN** S1/S2 SHALL 各有「完整回滚必红」与「修复必绿」两次复跑证据入 run-notes；P1–P6 判据 SHALL 逐项核验

#### Scenario: 停点待授权

- **WHEN** F4 归档与 commit/push
- **THEN** SHALL 停于法定停点等用户明示授权，未授权不执行；config-surface-normalization 的 F4 授权 SHALL 不被本变更 extrapolate