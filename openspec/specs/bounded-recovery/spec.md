# bounded-recovery Specification

## Purpose

历史读取有界化与验收真量的契约：已提交事实保持身份、字节与提交顺序，派生索引失效不改变真实读取答案，完整性、只读与召回授权不因加速降低；读取成本按完整操作归因，验收以症状直译锚、负锚与完整矩阵给出可复算证据。

## Requirements

### Requirement: 历史事实保持身份、字节与提交顺序

存储 SHALL 保持已提交事实不可变、同身份等价、整批原子性与耐久结果分类；派生索引改变 SHALL NOT 改变历史原字节、提交顺序、全部已提交事件计数或同链前驱关系。

#### Scenario: 重复提交与失败后重开
- **WHEN** 同身份同内容重复提交、冲突提交或批次在耐久结果未明时中断，随后独立重开存储
- **THEN** 重复不产生第二事实，冲突不覆盖原文，批次不发布部分成功，未知结果不冒充成功
- **AND** 成功提交的字节、计数和跨分区顺序与提交前后事实一致，同链前驱不串会话

### Requirement: 派生索引失效不改变真实读取答案

派生索引和缓存 SHALL 可与其事实来源核对；缺失或损坏时 SHALL 安全重建或具名拒绝，SHALL NOT 伪造缺失、遗漏已发布事实或把合法聚集键当作损坏。无 checkpoint 的会话 SHALL 保持同一恢复语义。

#### Scenario: 合法聚集键与墓碑
- **WHEN** 合法键聚集、存在同键更新和删除，且执行点读、缺失读、枚举与重开
- **THEN** 所有仍持有的键返回正确最新值，删除键不复活，未提交键保持缺失
- **AND** 键分布本身不被判作损坏，枚举早停不额外物化未请求的全部历史

#### Scenario: 派生丢失与年轻会话
- **WHEN** 派生索引不可用，或会话尚无 checkpoint，恢复需要从已有事实重建
- **THEN** 恢复结果、顺序与有效状态和正常读取一致；无法安全重建时具名失败，降级成本如实呈现

### Requirement: 完整性和只读保证不因加速降低

读取 SHALL 区分真实缺失与损坏；已发布事实缺正文 SHALL 具名失败而非静默跳过。校验时机改变 SHALL 对正向读取、负查询、墓碑、枚举遗漏、未访问历史损坏及只读视图维持等价保证；未证等价 SHALL 保留既有安全防线。

#### Scenario: 校验覆盖存在与不存在两种答案
- **WHEN** 已发布正文、地址、发布信息或顺序信息损坏，或损坏会使某键被错误回答为不存在
- **THEN** 读取具名拒绝或安全恢复，不把损坏转成合法空结果；正常的未提交键仍可回答缺失
- **AND** 故障经恢复、查询及召回入口传播，不由消费者吞错继续发布不完整视图

#### Scenario: 写者旁只读视图遇异常尾
- **WHEN** 只读视图在写者旁遇撕裂尾、零填充尾、含后续非零数据的损坏区或不可用派生索引
- **THEN** 只读视图不截断、不修复写盘，区分可忽略未完成尾与真实损坏，不隐去已提交事实

### Requirement: 原文召回维持授权与分段保真

原文读取 SHALL 保持已注册 scope、有效期和完整性判定；不可见引用 SHALL 按既有不泄漏语义回答。整段和分段读取 SHALL 保留原字节，接口整理 SHALL NOT 使正式召回绑定失效。

#### Scenario: 首读、热读与跨 scope 读取
- **WHEN** 真实查询与召回读取已有、空、二进制、多字节、过期、缺失或损坏原文，并使用已授权与未授权 scope
- **THEN** 首读与热读答案一致，分段拼回原字节，过期和读取失败各具名，未授权 scope 不获得正文或新增存在性信息
- **AND** 真实工具仍能绑定原文读能力；删除辅助读面不撤掉任何仍需的安全与字节判据

### Requirement: 压缩恢复读取原产物而不重生成

压缩恢复 SHALL 能定位每个实际生产者已提交的产物，恢复原摘要字节、覆盖顺序、原始来源与预算内呈现；SHALL NOT 通过再次请求模型或重新生成不同摘要补齐历史产物。只有等价来源已证明可读时才可删去冗余正文。

#### Scenario: 多轮压缩后的重启与原文召回
- **WHEN** 段产物或模型摘要经历多轮压缩后重启，随后按票据召回覆盖的原始事实
- **THEN** 恢复不重调模型，摘要与已提交产物逐字一致，覆盖顺序和原始来源可核，最终呈现含票据开销仍守预算
- **AND** 产物定位缺失或冲突具名失败，不静默跳过，不用同名 key 推断不存在的正文

### Requirement: 真实读取成本按完整操作归因

历史读取的验收 SHALL 覆盖实际操作的全部成本；读取实现 SHALL 消除已证的无关历史重复工作。前驱成本 SHALL 包含定位目标及寻找前驱两段，原文成本 SHALL 区分首读和热读。必要的全量查询、完整性校验与安全重建 SHALL 单独归因，不承诺所有操作常数成本，也不要求默认生产常开采样。

#### Scenario: 固定答案而扩大无关历史
- **WHEN** 固定前驱、scope、查询答案和本轮工作量，分别扩大目标之后历史、scope 外原文与分区数，并经真实 Tier 和召回工具读取
- **THEN** 返回结果不变，字节、访问计数、常驻量和耗时反映完整操作；新增增长有明确来源而非只量后半查找
- **AND** 索引窗口零读数或缓存命中不代表整个开档零读取，未归因增长阻止性能验收通过

### Requirement: 外部转换完整保留事实并拒绝不完整目标

外部转换 SHALL 保持源只读，在新根保留所有事实的身份、原文、计数与原提交顺序，经过独立重开验证后才报告可用；SHALL NOT 要求恢复已裁删除的运行时冷层或 lifecycle 兼容机制。

#### Scenario: 干跑、恢复与失败目标
- **WHEN** 正式转换入口执行干跑或恢复，或者源发生变化、源记录冲突、目标只完成部分写入
- **THEN** 干跑不改变源且不产生目标；成功目标经独立开档逐键、逐事实和原文原序核对
- **AND** 源变化、冲突或部分目标具名拒报可用，不以 helper 结果代替正式入口结果

### Requirement: 完整成本验证不依赖常开计量

成本验收 SHALL 分列冷编译、冷开档、热态及深浅条件，覆盖启动、提交、索引维护、查询、重绑定、checkpoint、关闭、计量自身及 total；时长与计数 SHALL 对应同次操作。原倍率判决 SHALL 包含 boot/total，完整性线性成本 SHALL 单列而不豁免未知增长。

#### Scenario: 阶段重复、漏记与指标关断
- **WHEN** 注入真实重复工作、全史读取或漏阶段，并比较指标启用与关断态
- **THEN** 独立验证仍能捕获增长、重复与漏记；关闭指标不被报告成零成本，去掉完整性校验不算性能负控
- **AND** 失败轮与原判决保留，历史低末首比值不作为常数复杂度证明，相减恒等式不作为阶段完整性证明

#### Scenario: 所有公开引擎档位参与验收
- **WHEN** 对公开存储档位执行安全与成本回归，包括声明由外部引擎提供的档位
- **THEN** 各档兑现其声明的耐久/原子能力，真实外部引擎缺资源、skip 或零命中均不算该档通过
- **AND** 未归因增长与未证安全等价不转交下游后标记本单元完成

#### Scenario: 原倍率判决的阈值口径

- **WHEN** 复核立项时的启动倍率判决
- **THEN** 该判决的阈值 SHALL 仍是原口径的 1.3×，且 boot 与 total 同列判定
- **AND** 该阈值 MUST NOT 由当场取值充当冻结裁决

### Requirement: boot 验收 SHALL 量四真量

boot 验收锚 SHALL 暴露并断言四真量：`journalBytes`（开档解码读入的 journal 字节）、`indexEntries`（常驻索引规模；MUST NOT 用全史事件计数标量替代）、`durableLoadCount`（一次 load 五个前缀读入键数之和）、`wallClock`（真实 boot 入口一次装配的墙钟）。四量名与语义 SHALL 是共享面冻结契约（一级 design 的裁定），由本能力定义，其余域消费转绿。

boot 验收 SHALL NOT 以正文计数作为有界性判据；正文计数只 SHALL 作观测量记录。`journalBytes` SHALL 是开档读入量而非 journal 磁盘厚度，SHALL NOT 复用 append 偏移；磁盘厚度的增长 SHALL 显式声明，MUST NOT 充当有界判据。`indexEntries` SHALL 与链索引分母分立：分母恒随历史增长，`indexEntries` SHALL 有界于存活键集＋尾。

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

#### Scenario: 四量的载体坐标（实现锚）

- **WHEN** 实现或复核四量探针，需定位每个量的取值点
- **THEN** `journalBytes` SHALL 取开档解码读入量（`memory/ledger/journal.go:433/490`）；`indexEntries` SHALL 取常驻索引规模（`memory/ledger/segment_store.go:373`），同处的 `EventCount()` 是全史标量，MUST NOT 作判据
- **AND** `durableLoadCount` SHALL 取一次 load 五前缀读入键数之和（`runtime/state/durable.go:259/270/282/294/305` → `LoadStats()`，见同文件 `:209`）；`wallClock` SHALL 由夹具在 boot 入口两侧计时（`assembly/run.go:690` → `:2417`／`:2562`）
- **AND** 正文计数的来源是 `Run.CommittedFacts()`（`assembly/run.go:2012`），只作观测量；`journalBytes` MUST NOT 复用 append 偏移 `journalLog.size`（`journal.go:501/588`）；链索引分母 `chainIndexSize` 见 `assembly/anchored_boot_parity_test.go:254`

### Requirement: 度量探针 SHALL NOT 扰动 boot

四量探针 SHALL 是只读观测（只读字段＋只读方法），沿用既有观测先例。探针 SHALL NOT 在开档路径新增 I/O、新增锁竞争或改变生产恢复行为，SHALL NOT 改 `AtomicStore` 四方法；`wallClock` SHALL 由夹具在 boot 入口两侧计时，SHALL NOT 依赖 env 门控日志。

#### Scenario: 探针接线后生产行为不变

- **WHEN** 四量探针接线完成
- **THEN** `go build ./...` SHALL exit 0，开档路径 SHALL 零新增 I/O，既有 durable load 日志面（run.go:621）SHALL 不变

#### Scenario: 墙钟非确定性处置

- **WHEN** `wallClock` 单独红而三确定量绿
- **THEN** SHALL 以 ≥10 次重复运行与竞态检测统计复现率后归因（优先怀疑实现），SHALL NOT 放宽断言绕过；有界性硬判定 SHALL 落在三确定量

#### Scenario: 观测先例与不可改面（实现锚）

- **WHEN** 实现四量探针
- **THEN** 观测先例 SHALL 沿用 `LoadStats`（`runtime/state/durable.go:201-215`）与 `EventCount`（`memory/ledger/segment_store.go:584`）的只读形态
- **AND** `AtomicStore` 四方法（`memory/ledger/atomic_store.go:50-61`）MUST NOT 被改；`wallClock` MUST NOT 依赖 env 门控日志（`assembly/run.go:572-576`）；既有 durable load 日志面（`assembly/run.go:621`）SHALL 不变

### Requirement: 验收夹具 SHALL 多代多任务

验收夹具 SHALL 在多代重启（每代重新 assemble 同一持久根）之上每代提交多个任务事实，使 durable 五前缀与索引真有正文；任务提交 SHALL 经正式入口，MUST NOT 由夹具自造写入。夹具 SHALL 自证：`durableLoadCount` 大于零且逐代增长、任务身份跨代不重复、链索引分母末代大于第二代。

#### Scenario: 夹具空转即红

- **WHEN** 夹具未提交任务（`durableLoadCount` 恒 0）或链未增长
- **THEN** 夹具自证 SHALL 具名失败，SHALL NOT 让四量断言在空夹具上假绿

#### Scenario: 多代多任务使全史扫可见

- **WHEN** 每代提交任务并结清后重启
- **THEN** `durableLoadCount` SHALL 随历史任务总数增长（`runtime/state/durable.go:305` 全扫），使任务恢复面的违背进入锚内
- **AND** 任务提交 SHALL 经正式入口 `run.registry.Register`／`Advance`（`assembly/task_checkpoint_test.go:39/46`）

### Requirement: S2 症状直译锚 SHALL 第一天红

S2 症状锚 SHALL 断言四真量不随历史线性增长，SHALL 保留分母防线（历史真增长）。既有的正文计数代理量锚 SHALL 保留作历史对照，本变更 SHALL NOT 改动它。症状锚 SHALL 在启动门即已立红；后续实现在本单落地后 SHALL 转绿；完整回滚 SHALL 必红，红绿两次证据 SHALL 入 run-notes（一级 F3）。工程化目标 SHALL 只能追加，SHALL NOT 取代症状锚。

#### Scenario: W0 红凭据

- **WHEN** W0 启动门复跑 S2 锚
- **THEN** SHALL 红：`journalBytes` 因整份文件重放（journal.go:490）线性增长、`indexEntries` 因全量物化（segment_store.go:281/373）恒等于分母、`durableLoadCount` 因全史扫（durable.go:305）线性增长

#### Scenario: 修复转绿与回滚必红

- **WHEN** 01/02 在 W1 合单内落地后复跑，随后完整回滚再复跑
- **THEN** 前者 SHALL exit 0（四量有界于存活键集＋尾），后者 SHALL 必红，两次凭据 SHALL 入 run-notes

#### Scenario: 症状锚与历史对照的载体（实现锚）

- **WHEN** 复跑 S2 症状锚与其历史对照
- **THEN** S2 锚 SHALL 是 `TestSymptomAnchorBootCostFlatAcrossGenerations`（`assembly/symptom_anchor_recovery_test.go`），断言面为四真量探针
- **AND** 历史对照锚 SHALL 是 `TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（`assembly/checkpoint_test.go:1231`，正文计数代理量），该文件由 config-surface 占用，本变更不改它

### Requirement: 负锚 SHALL 使全史读回归红

本能力 SHALL 提供负锚，在同一多代多任务存储上以正式全史走入口量四量作对照。负锚 SHALL 自证对照面（全史四量逐代线性增长），并 SHALL 断言 boot 四量逐项显著小于对照。恢复退化为全史读 SHALL 使该断言红。

#### Scenario: 对照面自证

- **WHEN** 负锚在今天的存储上走全史
- **THEN** 全史四量 SHALL 逐代线性增长，证明对照量的是全史而非空走

#### Scenario: 全史读回归

- **WHEN** boot 恢复退化为全史读（01/02 回滚或新增全史旁路）
- **THEN** boot 四量 SHALL 趋近全史对照，负锚 SHALL 红并点名回归的量

#### Scenario: 负锚入口与判定式（实现锚）

- **WHEN** 实现或复核负锚
- **THEN** 锚 SHALL 是 `TestSymptomAnchorBootFullHistoryReadRegression`，全史对照 SHALL 走正式入口 `seedFullReplay`（`assembly/run.go:2470`）
- **AND** 判定式 SHALL 是 boot 四量每项满足 `boot × 2 < 全史`；恢复退化为全史读时该式 SHALL 红

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
