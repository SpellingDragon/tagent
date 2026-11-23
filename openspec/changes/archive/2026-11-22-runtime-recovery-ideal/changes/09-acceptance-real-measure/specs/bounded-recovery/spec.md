> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

## ADDED Requirements

### Requirement: boot 验收 SHALL 量四真量

boot 验收锚 SHALL 暴露并断言四真量：`journalBytes`（开档解码读入的 journal 字节，memory/ledger/journal.go:433/490）、`indexEntries`（常驻索引规模 `len(s.index)`，memory/ledger/segment_store.go:373；**非 `EventCount()` 全史标量**，§五裁定）、`durableLoadCount`（一次 load 五前缀读入键数之和，runtime/state/durable.go:259/270/282/294/305 → `LoadStats()`:209）、`wallClock`（真实 boot 入口一次 assemble 的墙钟）。四量名与语义为共享面冻结契约（一级 design §五），由本能力定义，01/02/03 消费转绿。

boot 验收 SHALL NOT 以正文计数（`Run.CommittedFacts()`，assembly/run.go:2012）作为有界性判据；正文计数只 SHALL 作观测量记录。`journalBytes` SHALL 是开档读入量而非 journal 磁盘厚度，SHALL NOT 复用 append 偏移 `journalLog.size`（journal.go:501/588）；磁盘厚度的增长 SHALL 显式声明，MUST NOT 充当有界判据。`indexEntries` SHALL 与链索引分母（`chainIndexSize`，assembly/anchored_boot_parity_test.go:254）分立：分母恒随历史增长，`indexEntries` SHALL 有界于存活键集＋尾。

#### Scenario: 四量取自真实 boot 入口

- **WHEN** 一次重启在多代多任务存储上发生
- **THEN** 四量 SHALL 取自正式 boot 入口（assembly/run.go:690 `restoreCheckpointState` → run.go:2417/2562 `seedAnchoredReplay`），SHALL NOT 取自手工 rebuild 或测试自造遍历

#### Scenario: 正文计数平坦而真量增长

- **WHEN** 一次 boot 的正文计数逐代平坦，而 `journalBytes`/`indexEntries`/`durableLoadCount` 随历史线性增长
- **THEN** 验收 SHALL 红（代理量不得掩盖违背），失败文案 SHALL 点名真量与代序

#### Scenario: 开档读量与磁盘厚度分立

- **WHEN** 有界开档落地后开档只读派生索引＋尾，而 journal 磁盘厚度仍随历史增长
- **THEN** `journalBytes` SHALL 反映开档读入量（小于厚度），厚度 SHALL 只作观测量记录
- **AND** 两量 SHALL NOT 互相顶替，MUST NOT 以厚度变小宣布开档有界

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

### Requirement: 分阶段恢复成本 SHALL 分区间可复算

成本验收 SHALL 分别记录启动总耗时、整代总耗时以及开档、提交、按身份点读、目录维护、重绑定的区间。每项 SHALL 声明单位、起止及父子包含关系；嵌套与并发支路 SHALL NOT 重复相加，阶段合计 SHALL 使用独占区间或时间并集，与所属总区间核对，不能把整代工作合计与一次启动总耗时比较。

时间面的既定口径 SHALL 保持：启动成本是**一次装配**的成本，SHALL 以宽松上界判读；整代耗时 SHALL 作为新增的长期运行验收独立校准和判定，SHALL NOT 替代启动判定，也 MUST NOT 通过重新定义「启动」把整代读成启动。

计量 SHALL 可复算：同一读量 SHALL NOT 因内外嵌套被计两次。「启动成本有界」的阈值 SHALL 由同条件校准与回归反例确定，MUST NOT 由当场取值充当冻结裁决，也 MUST NOT 为转绿而放宽。

#### Scenario: 内外计时重复

- **WHEN** 外层读取窗口包住内层已计的开档段，同一开销被两处累加
- **THEN** 该计数 SHALL 被判为重复计量；复算 SHALL 落在一次读取的真实成本上，MUST NOT 以该计数作为阶段成本

#### Scenario: 启动被重定义为整代

- **WHEN** 启动判定面被改写成一整代（含提交、结清、检查点与关闭）的耗时
- **THEN** 该判定 SHALL 被拒；启动口径 SHALL 保持一次装配，整代耗时 SHALL 作为独立长期成本验收单列

#### Scenario: 阈值无来源

- **WHEN** 启动或无界判定使用一个既非原设计口径、也非同条件校准得到的倍数
- **THEN** 该阈值 SHALL 被判为无来源；判定 SHALL 依同条件校准与回归反例确定，MUST NOT 以记录值冒充冻结裁决

### Requirement: 验收 SHALL 以原始失败凭据固定回归面

本能力 SHALL 把被验收提交的原始失败面固定成可复跑清单：提交指纹、作业与选择器、实际命令与参数、退出码、具名失败测试与首条失败原文。清单 SHALL 取自只读取证（运行记录与随附证据产物）与本机复跑，MUST NOT 触发流水线本身，也 MUST NOT 以「零计数」「退出码非零」替代首个失败原因。

同一清单 SHALL 区分失败类别：断言失败、超时、未取得终局各自单列，MUST NOT 合并为一类，也 MUST NOT 在未定根因时以不稳定为名归因。清单的每一行 SHALL 能被复跑命令重新取出原文；取不出原文的行 SHALL 判无效。

#### Scenario: 首因被计数掩盖

- **WHEN** 一份清单只写某分片退出码非零与若干计数为零，而随附证据里可读出具体失败的用例与断言原文
- **THEN** 该行 SHALL 判无效；清单 SHALL 补入具名失败与首条原文

#### Scenario: 超时被当成断言红

- **WHEN** 小窗口分片因超出运行时限退出，而大窗口分片是断言失败
- **THEN** 两者 SHALL 分列；以同一归因叙述覆盖两者 SHALL 被拒

#### Scenario: 通知已被识别为到达

- **WHEN** 断言报「未收到结算答复」，而请求证据显示该通知正文已随请求到达模型
- **THEN** SHALL 记录通知已到达模型这一事实，再核识别、处理与应答路径；只有匹配代码和请求共同证明漏识别时才归为识别缺陷，不得直接写成运行时未投递

### Requirement: 最终候选 SHALL 在完整矩阵上同条件放行

最终候选 SHALL 以完整必需矩阵放行，逐项绑定源树指纹、命令与参数、退出码、合同摘要与产物路径；任一必跑项未取得通过终局 SHALL NOT 放行。

对照 SHALL 同条件：同一镜像与工具链、同一输入集与代序、冷热缓存分记，启动测量包含启动本身。baseline与candidate SHALL 分别绑定各自源码身份，不要求两树指纹相同；未声明环境或工作量差异的样本 SHALL NOT 用来量化回归或改善。候选通过证据只能来自候选本身，不能冒用另一树的通过结果。

#### Scenario: 前后对照各自绑定源码

- **WHEN** 比较声明的baseline与candidate两版实现
- **THEN** 每份结果 SHALL 分别匹配自己的受测源码和同条件输入；两版源码指纹不同不构成拒绝理由，冒用另一版通过证据则拒绝

#### Scenario: 必跑项缺终局

- **WHEN** 某个分片或汇总作业只给出未取得终局的结果
- **THEN** 矩阵 SHALL 判未闭合，MUST NOT 以其余作业通过放行

#### Scenario: 仅凭历史样本宣称百分比回归

- **WHEN** 候选与更早的异机样本比较并给出百分比结论
- **THEN** 该结论 SHALL NOT 作为判定；判定 SHALL 依同条件对照