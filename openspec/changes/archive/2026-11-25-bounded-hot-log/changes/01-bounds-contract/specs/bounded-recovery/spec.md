# bounded-recovery 规格（delta，MODIFIED）

> 头逐字匹配主 spec `openspec/specs/bounded-recovery/spec.md` 的「### Requirement: boot 验收 SHALL 量四真量」。全文保留既有条款与场景，仅把「磁盘厚度的增长 SHALL 显式声明」改读为按部署形态分记，并补开档不预扫归档卷、校验量有界于热窗的句子。承诺 id 与条件句的唯一家在 [contracts/archive.md](../../contracts/archive.md) §六（`[C1]`、`[C4]`）。

## MODIFIED Requirements

### Requirement: boot 验收 SHALL 量四真量

boot 验收锚 SHALL 暴露并断言四真量：`journalBytes`（开档解码读入的 journal 字节）、`indexEntries`（常驻索引规模；MUST NOT 用全史事件计数标量替代）、`durableLoadCount`（一次 load 五个前缀读入键数之和）、`wallClock`（真实 boot 入口一次装配的墙钟）。四量名与语义 SHALL 是共享面冻结契约（一级 design 的裁定），由本能力定义，其余域消费转绿。

boot 验收 SHALL NOT 以正文计数作为有界性判据；正文计数只 SHALL 作观测量记录。`journalBytes` SHALL 是开档读入量而非 journal 磁盘厚度，SHALL NOT 复用 append 偏移。journal 磁盘厚度 SHALL 按部署形态分别记账：满足 `hot-log-bounds` [C1] 全部条件的部署，其热卷厚度 SHALL 有界于保留窗，归档卷厚度随历史增长并显式声明；未声明归档的部署，其厚度增长 SHALL 显式声明。任一形态下 MUST NOT 以厚度变小充当有界判据，MUST NOT 以归档增长替代热卷有界。`indexEntries` SHALL 与链索引分母分立：分母恒随历史增长，`indexEntries` SHALL 有界于存活键集＋尾。

开档 SHALL NOT 全量预扫归档卷：稳态归档段与归档段索引 SHALL 读时校验，开档只 SHALL 校验结构与字节数；崩溃残留仍有已陈述热副本或待收缩 journal 前缀时，删除最后良好副本前 SHALL 对对应段／索引补验摘要，且 MUST NOT 扫描既有归档代。完整性校验与索引重建 SHALL 有界于热窗；MUST NOT 以段头自述摘要替代实算。归档段在校验失败时 SHALL 于读时或删除前具名拒绝。

#### Scenario: 四量取自真实 boot 入口

- **WHEN** 一次重启在多代多任务存储上发生
- **THEN** 四量 SHALL 取自正式 boot 入口（assembly/run.go:690 `restoreCheckpointState` → run.go:2417/2562 `seedAnchoredReplay`），SHALL NOT 取自手工 rebuild 或测试自造遍历

#### Scenario: 正文计数平坦而真量增长

- **WHEN** 一次 boot 的正文计数逐代平坦，而 `journalBytes`/`indexEntries`/`durableLoadCount` 随历史线性增长
- **THEN** 验收 SHALL 红（代理量不得掩盖违背），失败文案 SHALL 点名真量与代序

#### Scenario: 开档读量与磁盘厚度分立

- **WHEN** 有界开档落地后开档只读派生索引＋尾，而未声明归档的部署仍让 journal 磁盘厚度随历史增长
- **THEN** `journalBytes` SHALL 反映开档读入量（小于厚度），厚度 SHALL 只作观测量记录
- **AND** 两量 SHALL NOT 互相顶替，MUST NOT 以厚度变小宣布开档有界

#### Scenario: 四量的载体坐标（实现锚）

- **WHEN** 实现或复核四量探针，需定位每个量的取值点
- **THEN** `journalBytes` SHALL 取开档解码读入量（`memory/ledger/journal.go:433/490`）；`indexEntries` SHALL 取常驻索引规模（`memory/ledger/segment_store.go:373`），同处的 `EventCount()` 是全史标量，MUST NOT 作判据
- **AND** `durableLoadCount` SHALL 取一次 load 五前缀读入键数之和（`runtime/state/durable.go:259/270/282/294/305` → `LoadStats()`，见同文件 `:209`）；`wallClock` SHALL 由夹具在 boot 入口两侧计时（`assembly/run.go:690` → `:2417`／`:2562`）
- **AND** 正文计数的来源是 `Run.CommittedFacts()`（`assembly/run.go:2012`），只作观测量；`journalBytes` MUST NOT 复用 append 偏移 `journalLog.size`（`journal.go:501/588`）；链索引分母 `chainIndexSize` 见 `assembly/anchored_boot_parity_test.go:254`

#### Scenario: 归档形态的厚度记账

- **WHEN** 满足 `hot-log-bounds` [C1] 全部条件的部署在多代写入后开档
- **THEN** 热卷厚度 SHALL 有界于保留窗（[C1] 条件），归档卷厚度 SHALL 随代增长并被显式声明为已接受成本
- **AND** 该增长 MUST NOT 被报成热卷有界，热卷厚度变小也 MUST NOT 被报成开档有界

#### Scenario: 开档不预扫归档卷

- **WHEN** 归档卷字节随代增长而开档
- **THEN** 开档完整性校验与索引重建的字节 SHALL 有界于热窗，MUST NOT 随归档卷增长
- **AND** 稳态归档段损坏 MAY 只在读取该段时才被发现；崩溃残留删除最后热副本前 SHALL 只对对应本代段／索引补验摘要，MUST NOT 为此扫描既有归档正文
