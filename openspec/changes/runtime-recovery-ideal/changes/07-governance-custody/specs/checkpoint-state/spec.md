> **取代关系（本轮重整）**：`openspec/specs/` 中无 `checkpoint-state` 能力，本块**保留为历史时点证据**（W2 时的 delta 文本与其 Scenario）。当前生效条款为主规格 `session-checkpoint-material` 的「governance 恢复读切片而非全流扫」（已由 10 域同步转正）；本轮新增 delta 见同域 `specs/session-checkpoint-material/spec.md`，在其上追加「切片限于有效态」「审批有效期以链上事实读取时判定」「历史查询边界具名」三条。本块下列 Scenario 的断言面**不撤回**，逐条由新文件与主规格承接（生产接线非测试态 / 尾 seq 读切片 / 序号连续性 / 审批态恢复一致），另含主规格中「落链 checkpoint 携带偏移与切片」「最新 checkpoint 读取不随历史增长」两条不在本域改变范围。

## MODIFIED Requirements

### Requirement: 框架统一 checkpoint 事实承载可恢复状态

框架 SHALL 以统一的 `checkpoint` 事实族承载全部可恢复状态切片（会话投影幸存者、宿主全局送达/结算账含开账、邮箱欠账、未应答输入、任务终态、governance 尾 seq 与审批态），经框架状态接缝由消费者注册、按名聚合，每条 checkpoint 事实 SHALL 携带一致性切面的分区 Seq 水位（Offset）。checkpoint 事实 SHALL 经事实提交入口落链、键按偏移单调（`checkpoint:<partitionSeq>`），一身份一不可变事实。投影恢复 SHALL 以继承的折叠锚为主路径（折叠锚即投影的 checkpoint，按各自锚边界回放，不被全局 Offset 裁剪）；压缩事实 SHALL 回归纯投影职责，仅作为 checkpoint 触发源之一。

governance 状态 SHALL 作为框架 checkpoint 的注册切片承载（尾 seq + 审批态 + 派生索引），经同一状态接缝（`StateName/SnapshotState/RestoreState`）注册、按名聚合，并在生产宿主接线（`governance.Open` 挂 ledger/goals + 注册切片），SHALL NOT 停在测试态。governance 恢复 SHALL 读切片派生加 Offset 后尾回放，SHALL NOT 全流扫事件账本（`seedEventSeq`/`replayGovernanceEvents`）或全目录扫审批簿（`rebuild`）。governance 事件键 SHALL 保持 19 位零填充（字典序＝操作序），尾 seq SHALL 取自切片续号、SHALL NOT 静默重置致复用已写键；切片缺失或损坏 SHALL 具名拒绝而非按零值重置。审批态恢复 SHALL 与运行期一致（不复活已结或过期请求）。governance 事件链 SHALL 仍是语义真相与回放源，checkpoint governance 切片只是加速恢复的派生快照；任何有界化手段 SHALL NOT 删除、覆盖或改写已提交的 governance 事件。

#### Scenario: 落链 checkpoint 携带偏移与切片
- **WHEN** 框架协调者在安静点到达 checkpoint 节奏（提交计数/折叠触发/close 收敛）
- **THEN** SHALL 落一条 type=checkpoint 事实，其 Offset 等于该切面的分区 Seq 水位，载荷含投影锚、送达账开账清单与已结水位、邮箱欠账、未应答输入、任务终态、governance 尾 seq 与审批态

#### Scenario: 最新 checkpoint 读取不随历史增长
- **WHEN** boot 读取最新 checkpoint
- **THEN** SHALL 经索引 newest-first 过滤 type=checkpoint 取首条，读取成本 SHALL 不随累积历史线性增长

#### Scenario: governance 状态托管为切片
- **WHEN** governance 持有可恢复态（denial/goal 两流尾 seq、审批态、派生索引）
- **THEN** 该态 SHALL 作为一个注册切片进入框架 checkpoint，经状态接缝按名聚合，SHALL NOT 自持一条独立于切片的恢复读路径

#### Scenario: 尾 seq 读切片非全流扫
- **WHEN** 在累积大量 governance 事件的存储上重启
- **THEN** `seedEventSeq` 与 `replayGovernanceEvents` SHALL 读恢复后切片的尾 seq 与派生索引加 Offset 后尾回放，SHALL NOT 经 `log.Scan` 全流扫；恢复成本 SHALL 不随 governance 事件总数线性增长

#### Scenario: seq 连续性不重置
- **WHEN** 重启的 ledger/goals 续号写下一条 governance 事件
- **THEN** 尾 seq SHALL 取自切片且等于已存储 max，事件键 SHALL 保持 19 位零填充（字典序＝操作序），SHALL NOT 复用已写键；切片缺失或损坏 SHALL 具名拒绝 boot，SHALL NOT 静默按零值重置致 re-key 覆盖已提交事实

#### Scenario: 生产接线非测试态
- **WHEN** 真实宿主 boot 装配 governance
- **THEN** `governance.Open` SHALL 挂 ledger/goals 并经 `Run.RegisterStateSlice` 把 governance 切片注册到生产状态接缝，ledger/goals 的生产调用方 SHALL ≥1，SHALL NOT 仅由测试或编译期断言持有

#### Scenario: 审批态恢复一致
- **WHEN** 宿主重启恢复审批簿
- **THEN** 审批态 SHALL 从 governance 切片派生 restore 加尾回放，SHALL NOT 经 `os.ReadDir` 全目录扫；恢复结果 SHALL 与运行期一致（已结请求不复活、过期请求不复活、attention 链对账不重不漏）