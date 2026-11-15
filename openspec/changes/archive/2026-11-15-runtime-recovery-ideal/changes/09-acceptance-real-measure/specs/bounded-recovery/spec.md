## ADDED Requirements

### Requirement: boot 验收 SHALL 量四真量

boot 验收锚 SHALL 暴露并断言四真量：`journalBytes`（开档解码读入的 journal 字节，memory/ledger/journal.go:433/490）、`indexEntries`（常驻索引规模 `len(s.index)`，memory/ledger/segment_store.go:373；**非 `EventCount()` 全史标量**，§五裁定）、`durableLoadCount`（一次 load 五前缀读入键数之和，runtime/state/durable.go:259/270/282/294/305 → `LoadStats()`:209）、`wallClock`（真实 boot 入口一次 assemble 的墙钟）。四量名与语义为共享面冻结契约（一级 design §五），由本能力定义，01/02/03 消费转绿。

boot 验收 SHALL NOT 以正文计数（`Run.CommittedFacts()`，assembly/run.go:2012）作为有界性判据；正文计数只 SHALL 作观测量记录。`journalBytes` SHALL 是开档读入量而非 journal 磁盘厚度（厚度属 S1 退位面），SHALL NOT 复用 append 偏移 `journalLog.size`（journal.go:501/588）。`indexEntries` SHALL 与链索引分母（`chainIndexSize`，assembly/anchored_boot_parity_test.go:254）分立：分母恒随历史增长，`indexEntries` SHALL 有界于存活键集＋尾。

#### Scenario: 四量取自真实 boot 入口

- **WHEN** 一次重启在多代多任务存储上发生
- **THEN** 四量 SHALL 取自正式 boot 入口（assembly/run.go:690 `restoreCheckpointState` → run.go:2417/2562 `seedAnchoredReplay`），SHALL NOT 取自手工 rebuild 或测试自造遍历

#### Scenario: 正文计数平坦而真量增长

- **WHEN** 一次 boot 的正文计数逐代平坦，而 `journalBytes`/`indexEntries`/`durableLoadCount` 随历史线性增长
- **THEN** 验收 SHALL 红（代理量不得掩盖违背），失败文案 SHALL 点名真量与代序

#### Scenario: 开档读量与磁盘厚度分立

- **WHEN** 有界开档落地后开档只读派生索引＋尾，而 journal 磁盘厚度尚未退位
- **THEN** `journalBytes` SHALL 反映开档读入量（小于厚度），S1 的厚度断言 SHALL 独立成立，两量 SHALL NOT 互相顶替

### Requirement: 度量探针 SHALL NOT 扰动 boot

四量探针 SHALL 是只读观测（只读字段＋只读方法），沿用既有观测先例（`LoadStats` durable.go:201-215、`EventCount` segment_store.go:584）。探针 SHALL NOT 在开档路径新增 I/O、新增锁竞争或改变生产恢复行为，SHALL NOT 改 `AtomicStore` 四方法（memory/ledger/atomic_store.go:50-61）；`wallClock` SHALL 由夹具在 boot 入口两侧计时，SHALL NOT 依赖 env 门控日志（assembly/run.go:572-576）。

#### Scenario: 探针接线后生产行为不变

- **WHEN** 四量探针接线完成
- **THEN** `go build ./...` SHALL exit 0，开档路径 SHALL 零新增 I/O，既有 durable load 日志面（run.go:621）SHALL 不变

#### Scenario: 墙钟非确定性处置

- **WHEN** `wallClock` 单独红而三确定量绿
- **THEN** SHALL 以 ≥10 次重复运行与竞态检测统计复现率后归因（优先怀疑实现），SHALL NOT 放宽断言绕过；有界性硬判定 SHALL 落在三确定量

### Requirement: 验收夹具 SHALL 多代多任务

验收夹具 SHALL 在多代重启（每代重新 assemble 同一持久根）之上每代提交多个任务事实，使 durable 五前缀与索引真有正文；任务提交 SHALL 经正式入口（`run.registry.Register`/`Advance`，assembly/task_checkpoint_test.go:39/46）。夹具 SHALL 自证：`durableLoadCount > 0` 且逐代增长、任务身份跨代不重复、链索引分母末代大于第二代。

#### Scenario: 夹具空转即红

- **WHEN** 夹具未提交任务（`durableLoadCount` 恒 0）或链未增长
- **THEN** 夹具自证 SHALL 具名失败，SHALL NOT 让四量断言在空夹具上假绿

#### Scenario: 多代多任务使全史扫可见

- **WHEN** 每代提交任务并结清后重启
- **THEN** `durableLoadCount` SHALL 随历史任务总数增长（runtime/state/durable.go:305 全扫），使任务恢复面的违背进入锚内

### Requirement: S2 症状直译锚 SHALL 第一天红

S2 症状锚（`TestSymptomAnchorBootCostFlatAcrossGenerations`，assembly/symptom_anchor_recovery_test.go；W0 已立红，04 精化断言面为四真量探针）SHALL 断言四真量不随历史线性增长，SHALL 保留分母防线（历史真增长）。既有 `TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（checkpoint_test.go:1231，正文计数代理量）保留不动作历史对照（config-surface 占用该文件，本变更不改它）。01/02 落地后 SHALL 转绿；完整回滚 SHALL 必红，红绿两次证据 SHALL 入 run-notes（一级 F3）。工程化目标 SHALL 只能追加，SHALL NOT 取代症状锚。

#### Scenario: W0 红凭据

- **WHEN** W0 启动门复跑 S2 锚
- **THEN** SHALL 红：`journalBytes` 因整份文件重放（journal.go:490）线性增长、`indexEntries` 因全量物化（segment_store.go:281/373）恒等于分母、`durableLoadCount` 因全史扫（durable.go:305）线性增长

#### Scenario: 修复转绿与回滚必红

- **WHEN** 01/02 在 W1 合单内落地后复跑，随后完整回滚再复跑
- **THEN** 前者 SHALL exit 0（四量有界于存活键集＋尾），后者 SHALL 必红，两次凭据 SHALL 入 run-notes

### Requirement: 负锚 SHALL 使全史读回归红

本能力 SHALL 提供负锚（`TestSymptomAnchorBootFullHistoryReadRegression`），在同一多代多任务存储上以正式全史走入口（`seedFullReplay`，assembly/run.go:2470）量四量作对照。负锚 SHALL 自证对照面（全史四量逐代线性增长），并 SHALL 断言 boot 四量逐项显著小于对照（boot × 2 < 全史）。恢复退化为全史读 SHALL 使该断言红。

#### Scenario: 对照面自证

- **WHEN** 负锚在今天的存储上走全史
- **THEN** 全史四量 SHALL 逐代线性增长，证明对照量的是全史而非空走

#### Scenario: 全史读回归

- **WHEN** boot 恢复退化为全史读（01/02 回滚或新增全史旁路）
- **THEN** boot 四量 SHALL 趋近全史对照，负锚 SHALL 红并点名回归的量