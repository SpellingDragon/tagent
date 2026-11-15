## ADDED Requirements

### Requirement: 开档与索引由存活键集加尾派生

存储开档 SHALL 由最近一份索引切片加其后尾增量派生索引，SHALL NOT 每次开档从偏移 0 全量重建。索引切片 SHALL 是可由链重建的派生载体，携带存活键集的键到值跨度映射、帧序、已提交计数、窗口元表与切面偏移；切面 SHALL NOT 承载任何链上没有的事实，SHALL NOT 成为第二真相。日志文件 SHALL 保持只追加不改写，切面 SHALL 落在独立的派生载体上并以临时文件加原子改名落盘。开档 SHALL 只重放切面偏移之后的尾，存储发布 SHALL 只补尾（分区 Seq 大于 Offset）的索引与窗口元条目。常驻索引、帧序与窗口元表 SHALL 有界于存活键集加尾。切面偏移 SHALL NOT 大于日志文件的完整尾；越过完整尾 SHALL 具名拒绝开档，SHALL NOT 静默全扫掩盖，SHALL NOT 静默采信。同一目录的第二视图不持排他锁，故切面 SHALL 允许落后于日志尾，尾重放 SHALL 覆盖切面偏移之后的全部完整帧。

#### Scenario: 多代存储重开档只读切面加尾

- **WHEN** 一个已累积多代历史并落过 checkpoint 的存储被重新开档
- **THEN** 开档读入的字节 SHALL 由索引切片与其后尾决定，SHALL NOT 随累积历史线性增长
- **AND** 常驻索引条目 SHALL 等于存活键集加尾的条目，SHALL NOT 等于历史全部键次

#### Scenario: 切面缺失或损坏退回全扫

- **WHEN** 索引切片不存在，或其内容无法按记录的键值状态读回
- **THEN** 开档 SHALL 具名告警并由链全扫重建索引，重建结果 SHALL 与派生路径逐条一致
- **AND** 损坏 SHALL 以 `ErrSnapshotCorrupt` 具名，SHALL NOT 静默当作空切面

#### Scenario: 切面偏移越过完整尾

- **WHEN** 索引切片记录的偏移大于日志文件实际完整帧的结束偏移
- **THEN** 开档 SHALL 具名拒绝，SHALL NOT 从该偏移重放，SHALL NOT 静默退回全扫

#### Scenario: 已提交计数语义不变

- **WHEN** 索引由切面派生加载后读取已提交事件计数
- **THEN** 计数 SHALL 仍是全部成功提交的事件数，由切面携带的标量派生
- **AND** 索引规模有界 SHALL NOT 以把计数改成存活键集计数为代价

### Requirement: 索引规模不随累积历史线性增长

boot 的开档读入字节、常驻索引条目、帧序条目与恢复墙钟 SHALL 由最近 checkpoint 的存活键集与偏移后增量决定，SHALL NOT 随累积历史线性增长。验收 SHALL 量真量（开档读入字节、常驻索引条目、durable load 计数、墙钟），SHALL NOT 以正文解码计数作代理量。已提交事件计数是全史标量，SHALL NOT 被当作常驻索引规模的度量。索引 newest-first 枚举 SHALL 保持早停语义：一次早停的走访 SHALL NOT 为未达条目付出排序或物化成本。

#### Scenario: 多代多任务存储上重启成本平坦

- **WHEN** 在累积多代历史且每代追加任务提交的存储上重启
- **THEN** 常驻索引条目与开档读入字节 SHALL 不随代数线性增长
- **AND** 判据 SHALL 取真量，正文解码计数只作观测量

#### Scenario: 索引枚举早停不物化全序

- **WHEN** 一个消费者经索引 newest-first 取首条某类型事实后停止走访
- **THEN** 该次走访 SHALL NOT 为未达条目排序或复制索引
- **AND** 帧序的常驻规模 SHALL 有界于存活键集加尾，SHALL NOT 等于历史触碰键次总数

### Requirement: 索引点名而正文缺失具名失败

恢复期读一条索引已点名的事实而其正文读不到时，SHALL 以 `ErrSegmentBodyMissing` 具名失败并携带该事实键，SHALL NOT 静默跳过该事实后继续恢复。全部恢复读路径 SHALL 对同一缺失给同一答案：锚定回放的逐槽兜底、全史走访、切面后增量走访与存储枚举 SHALL 同口径。索引未点名某键是合法裁剪结果，SHALL NOT 与「索引点名而正文缺失」混为一谈；checkpoint 点名的存活者若链上无条目且载荷未携带正文，SHALL 以 `ErrCheckpointSurvivorUnstated` 具名拒绝 boot。

#### Scenario: 逐槽兜底遇缺页

- **WHEN** 锚定回放的逐槽兜底读一条索引点名而正文缺失的事实
- **THEN** 恢复 SHALL 返回 `ErrSegmentBodyMissing` 并携带该键，boot SHALL 具名失败
- **AND** SHALL NOT 返回 nil 错误后让投影少一条事实

#### Scenario: 两条读路径同口径

- **WHEN** 同一条正文缺失的事实分别经逐槽兜底与切面后增量走访被读到
- **THEN** 两条路径 SHALL 返回同一个具名错误，SHALL NOT 一条静默跳过一条具名失败

#### Scenario: 折叠产物凭载荷正文合法重建

- **WHEN** checkpoint 点名一个折叠产物作为存活者，链上无其条目而载荷携带其正文
- **THEN** 恢复 SHALL 由载荷正文重建该条目，SHALL NOT 判为缺页
- **AND** 载荷未携带正文且链上无条目时 SHALL 以 `ErrCheckpointSurvivorUnstated` 具名拒绝

### Requirement: 全扫兜底与派生路径一致

无索引切片的存储、无 checkpoint 的年轻会话、以及从未折叠的链 SHALL 保留全扫兜底，且兜底结果 SHALL 与派生路径逐条一致。兜底 SHALL 是同一 g(x) 的退化输入，SHALL NOT 是第二套派生实现。兜底的范围 SHALL 是本链或本存储，SHALL NOT 因派生加载而扩大为全库全史读。从未折叠且历史很长的链，其存活键集等于全部落链事实，索引随该链历史增长是自然成本；该成本 SHALL 由折叠节奏与退位承接，SHALL NOT 被声称为本要求已达成的有界。

#### Scenario: 年轻链无 checkpoint

- **WHEN** 一个从未落 checkpoint 的会话链在重启后恢复
- **THEN** 该链 SHALL 全量回放并经同一 g(x) 重建，结果 SHALL 与有 checkpoint 时的派生路径对同一事实序列一致

#### Scenario: 从未折叠的链走兜底

- **WHEN** 一条链没有任何折叠产物可作锚
- **THEN** 该链 SHALL 走全扫兜底，且 SHALL NOT 触发送达、结算或冥想等 observation 副作用
- **AND** 兜底遍历的索引规模 SHALL 已有界于存活键集加尾

#### Scenario: 兜底与派生结果相等

- **WHEN** 同一存储分别经派生加载与全扫兜底完成 boot
- **THEN** 两者呈现的投影条目、已提交计数与帧序 SHALL 逐条相等，差异 SHALL 视为缺陷

### Requirement: 帧序保序不变量

帧序 SHALL 按帧记录：一条记录触碰的每个键 SHALL 在该记录处被点名，最新在后；一键在每条触碰它的记录里 SHALL 都被点名。任何对存活键集的裁剪或重写 SHALL 以逆帧序去重后反写，SHALL NOT 顺帧序去重，SHALL NOT 以键排序替代帧序。逆序走访 SHALL 先遇一键的最新记录。派生加载 SHALL NOT 绕过「帧序条目数少于发布索引条目数即具名失败」的既有检查。无帧序能力的引擎 SHALL 保留其键排序分支，本要求 SHALL NOT 改该分支。

#### Scenario: 跨分区提交序不变

- **WHEN** 三个事实按跨分区的提交先后落链，存储重开档后逆序走访索引
- **THEN** 走访 SHALL 按提交序从最新到最旧点名，SHALL NOT 按分区或窗口重排

#### Scenario: 裁剪存活键集后仍认最新 checkpoint

- **WHEN** 存活键集裁剪把同一键在多条记录处的点名收拢为一次
- **THEN** 收拢 SHALL 逆帧序去重，保留最新记录处的位置
- **AND** 重开档后读取最新 checkpoint SHALL 仍取到同一条，SHALL NOT 认错较旧的一条

#### Scenario: 删除键与早停

- **WHEN** 一条记录删除了某键，随后逆序走访帧序
- **THEN** 该键 SHALL 被跳过，SHALL NOT 被点名
- **AND** 走访在 visit 答 false 处 SHALL 结束，SHALL NOT 继续走更旧的记录

#### Scenario: 帧序缺条目具名失败

- **WHEN** 派生加载得到的帧序条目数少于发布的索引条目数
- **THEN** 加载 SHALL 具名失败并说出两个数目，SHALL NOT 发布不完整的索引