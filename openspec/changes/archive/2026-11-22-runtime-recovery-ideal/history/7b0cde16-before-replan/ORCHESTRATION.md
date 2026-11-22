# ORCHESTRATION · runtime-recovery-ideal

## ① 结论

**当前状态（2026-11-18，R266）**：用户裁定「先不拆分直接commit，然后继续分派子代理推进」——整体候选一笔落账（进度提交，非完成宣告），随后按R265计划派发实施叶。实施与终验仍未完成；不修改主规格、不变更权限、不push（本轮未授权）。已存在的实现保留，但没有可直接继承的最终验收结论。R264 的“三项 P1 全修”“稳态零额外成本”“comment_policy 触碰包零 findings”均撤销；具体差距已交由对应领域任务承接。

**抵达完成的路径**：先闭合失败后恢复、折叠后重试和通知再次到达的完整行为，再闭合配置入口、状态成本与生命周期；收齐旧要求及清理项后，对稳定候选运行完整矩阵并独立复审。局部测试绿不释放整个领域。

**最大风险**：实现、测量和完成报告覆盖的不是同一组行为。测试只查键存在而未查顺序、只查输出大小而未查遍历工作量、只比报告摘要而未绑定实际依赖代码，均不能支持整体完成。

阅读顺序：本文的当前派发条件 → [任务索引](tasks.md)的对应域 → 该域 tasks/design/specs。目标与取舍由 [design.md](design.md) 定义。历史仅在追溯裁定或原始证据时读取 [ORCHESTRATION-HISTORY.md](ORCHESTRATION-HISTORY.md) 与 [TASKS-HISTORY.md](TASKS-HISTORY.md)；旧文档中的“当前”“在跑”“已完成”均是历史时点，不驱动派发。

## ② 编排四问

### 次序

下表是尚未通过验收的工作簇，实施时仍以域内具名叶为最小派发单位；同簇不等于整域授权。

| 工作簇 | 对应叶与承接 | 进入实施前的条件 | 释放给谁 |
|---|---|---|---|
| 完整恢复与重复处理 | 12.R2；05.R2/R3；04.R1 的重复通知识别 | 冻结一个正常处理→失败→保存进度→重启→旧身份再次到达的正负场景；区分重复生成与同输出重发 | 12.R3、03.R2、各状态域最终复验 |
| 最新恢复修法复核 | 12 域补差：两批重放的总顺序、已折叠输出重试、新增读取量 | 不以键集合一致替代顺序一致；不新增持久应用日志或全历史判重副本 | 12.R2 的独立审查，不凭现有宽窗口测试放行 |
| 配置与最终呈现 | 03 宿主预算入口；04.R0/R1/R2 | 正式宿主文件解码、最终请求预算、带票据的重复通知识别各有反例；04.R0 明确与另一变更的无环交接 | 03/04 域验收 |
| 有效状态与成本 | 03.R2、05.R2、07.R2；06.R3/R5、09.R2 | 分清输出字节、遍历量、常驻状态、实际读写与墙钟；历史决定不能因清减消失 | 09.R3 最终成本与完整矩阵 |
| 生命周期 | 02.R1、12.R3 | 12.R2 的共享失败/恢复语义通过；测试接受“拒绝卸载且原持有者保留”；未完成项由业务拥有者陈述 | 插件与恢复联合验收 |
| 存储与运维 | 11.R2/R3、08.R3/R4及外部折回叶 | 拒绝与结果未知按真实落地保证分类；旧锚删除有承接；源离线、目标失败不切换的转换场景齐备 | 数据安全与运维验收 |
| 要求、旧码和规格 | 10 域现行清理叶及旧项承接 | 列完原要求→现存实现→验证→保留/删除/承接的对应关系；测试调用不自动成为生产存在理由 | F1；影响其他域时先回到对应实现叶 |
| 真实放行入口 | 13.R2/R3及本轮补差 | 绑定被测代码与本地依赖；正式 lint 消费真实变异证据；错误、过期、缺失材料实际被拒 | 09.R3 收证与 F2/F3/F4 |

**不以未勾选反推未实施**：01/02 原接入、04 原文召回、05 任务托管、06 索引、08 删除退位、11 整笔保存都有成果。是否独立完成由原要求及当前反例决定，不能由本表或历史绿数决定。

### 授权

- 用户本次授权直接调整计划，并要求评估先归档/commit已确定完成部分。历史文档分离已在此范围内；不等于授权把未完成代码标成完成。
- **R266授权（取代R265「不动index」）**：用户明示整体commit不拆分。范围＝全部已跟踪改动（含两旧归档移回、config-surface耦合、12域修复、R265计划重整）＋本变更两份HISTORY新文件；外来未跟踪文件仍零触碰。已知未闭合项（S5、恢复总序、折叠重试、重复投递、成本归因、证据绑定）随提交如实记录，不以提交冒充完成。
- 后续独立提交可按语义闭合部分进行，不要求所有工作永远捆成一笔；候选先满足下文的分批条件。当前没有已通过这些条件的候选。
- 不触碰 `openspec/changes/runtime-architecture-convergence/`、`openspec/changes/no-prefix-criterion/`、`golden/runtime/no_prefix_criterion.json`、`scripts/gates/check_event_semantics_symptoms.py`、`scripts/gates/runtime_budgets.py`、`scripts/gates/test_check_runtime_symptoms.py`、`scripts/gates/check_no_prefix_criterion.py`、`scripts/gates/test_check_no_prefix_criterion.py`、`examples/wechat-bot/skills/interview/agent-interview-helper/`。
- 不读用户真实 `examples/wechat-bot/.wechat-config/data` 或用户 `--source` 根；不修改冻结 `openspec/changes/archive/**`。既有移回进行中的差异原样保留，本轮不重新移动它们。
- 不更改权限、分支保护、CI或质量基线来绕过失败。配置归位变更保持原授权的耦合关系；不吸收另一变更的全面身份格式迁移。

### 凭据

每个执行叶保留正面、负面和正式使用入口三类验证，附完整命令、实际命中数、程序原始退出码、源树与依赖身份、环境、原始结果文件。拟新增测试未实现前不计覆盖；只读结论不是测试通过。

| 需要撤销或限定的旧声称 | 当前有效判断 | 承接 |
|---|---|---|
| R264 三项 P1 全修 | 定向测试覆盖有限；恢复顺序与折叠后重试仍有缺口 | 12 域本轮补差 |
| R264 稳态零额外成本 | 新位置读取先于空区间判断；实际成本未量 | 12 补差、09.R2 |
| comment_policy 零 findings／外来文件导致失败 | 无目录参数导致扫描集合不符；未取得有效 findings 结论 | 13 验证入口；最终正式 lint |
| F3 三反例完成等于目标全部完成 | 三反例仅证明各自性质；CRC反例不证明性能回归能被拒 | 09.R2、F3 |
| 十短六长及 bundle 历史绿等于当前绿 | 报告互相一致不等于现树和依赖一致 | 13、09.R3 |
| 磁盘物理封顶仍是终点 | 旧 S1 封顶谓词已被删除退位的裁定取代；保留原文与增长量观测 | design D5/§六、09 |

完整实施收尾使用 `bash scripts/lint.sh` 及09.R3的原必需矩阵；纯计划调整只核规划结构、链接、要求承接与独立评审，不声称运行时全绿。测试输出不以 `grep`/`tail` 的退出码代替被测程序退出码；build/test与commit不串联。

对反例先确认控制版本通过、补丁可运行、目标路径实际执行，再检查被要求的行为是否失败。一个更早的守卫失败，不代表后面的性质已经检验。

### 释放等待表

| 资源或共享面 | 当前持有者/状态 | 释放条件 | 等待工作 |
|---|---|---|---|
| 一级目标、编排、共享规格裁定 | 编排者；计划调整中 | 本轮文档独立审无阻断 | 后续精确叶派发 |
| 03/07/12计划文件 | 行为计划单；仅文档写 | 六文件差异核查完成 | 对应行为实施叶 |
| 09/10/13计划文件 | 验收计划单；仅文档写 | 六文件差异核查完成 | 计量、清理、放行叶 |
| 生产代码与测试 | 本轮无写者 | 计划验收及对应叶前置通过 | 精确写面不相交的实现单 |
| 最终计时与全矩阵 | 未启动 | 实现稳定、计量定义及证据绑定有效 | 09.R3、F2/F3 |
| git index/归档 | 保持现场，不操作 | 独立候选完整放行或全变更完成 | 分批提交或F4 |

## ③ 明细

### 文档分离不等于完成归档

历史文件保留调整前的原始字节，不迁入 OpenSpec 完成归档目录，不撤销未完成叶，不重写历史测量。当前入口只说明有效要求、前置、工作状态与勘误；不得要求执行者通读五千行历史才能发现当前任务。

旧轮次引用按 `ORCHESTRATION-HISTORY.md` 中的 R 编号查找；旧顶层任务细节在 `TASKS-HISTORY.md`。历史快照 SHA-256：

- `ORCHESTRATION-HISTORY.md`：`46346867e9a8f7b2e24420f37f1100d60c3043add5057ccf5f302b650b323645`
- `TASKS-HISTORY.md`：`3c5095dbb68905bc02fe76c7ac043a40ddc998448b41be22ca4635c630137a59`

### 分批 commit 与 OpenSpec 完成归档

一个候选只有同时满足以下条件，才可作为“已确定完成部分”提取：

1. 具名需求、代码、测试和必要文档构成独立能力，未完成部分不被它隐式依赖；移出后剩余计划仍有完整承接。
2. 在实际拟提交的文件/依赖集合上验证，而非借当前混合工作树的通过结果；历史证据不能替代它。
3. 正负及正式入口验证齐备，独立审查明确范围与未查面，范围内无未解决阻断；不得删除红测试或缩小需求造完成。
4. 提交内容逐文件审阅，不夹带当前暂存区的其他改动；已核候选与未完成工作保持可恢复，不擅自清空index或覆盖工作树。
5. commit保存一个已验证代码单元；OpenSpec归档声明该change完整完成。嵌套子域若需单独归档，先具备独立change及规格归属，不能直接把一个子目录搬进archive冒充完成。

本轮优先解决阅读体积，不以仓库历史整理代替能力验收。没有合格候选时，保留代码和暂存现场，报告缺哪些前置，不阻止计划文档先调整。

| 已检查的拆分候选 | 当前判定 | 下一项证据 |
|---|---|---|
| 01守护检查器补强 | 最适合先构造独立候选；不是整个01域完成 | 守护脚本、自测、必要文档的准确补丁集；依赖在基线已存在；隔离候选的正负/装配入口验证与独立R35审查 |
| 08外部折回脚本 | 不能直接套到旧HEAD独立交付 | 依赖06/11新增写权、只读开档；先确定并验证基础设施最小闭包，再通过08.R5源变化与失败目标场景 |

只读拆分核查未找到任一候选的最终独立放行证据；因此本轮不commit、不执行OpenSpec完成归档。此判定不等于永远不拆，候选通过自己的验收后可提交，仍不能宣告父变更已完成。

### 并发与验收窗口

实施只派就绪叶。写面、构建读取及验证读取相交时串行或合单；纯文档单可以按文件并行。生命周期与通知重试共享失败语义，不能因目录不同就同时改不同契约。长测在源码稳定后独立运行，禁止与同实现写入并发取最终证据。

## ④ 本轮记录

### R268 E诊断定因：重复生成根因＝账户按身份读缺口（2026-11-18）

- **实测因果链**（/tmp/g71-anchor failed-attempts long-s1-w256-attempt2，pre-G83树）：gen17任务终结→notice提交→settle-ack输出事实21-687cfc84→收据consumed；gen18 boot `tasks=1 tail=4 reads=0 keys=0`→恢复期无新输入再`settle turn landed`→第二settle-ack输出事实2-bd71eed5。**两次＝两个输出事实＝两次生成**，第二次由boot恢复驱动，非同输出重发。
- **证伪**「收据未落盘」：旧树recordConsumption先Put+Sync后写内存，consumed日志在Consume返回后⇒收据已耐久。
- **根因B（读码推断HEAD可达）**：`durable.go` settled()/attemptOf()仅查内存；Seat已终结任务不入切片、尾段只读cut后键⇒gen18未按身份补读该任务收据/尝试⇒settled=false、attemptOf=false，settled判重与尝试拦截两层防御同因失效。旧树A(outbox无条件owed)/C(owedOfWork无settled过滤)已由G83在HEAD修，**B仍在**。
- **具名修法**（下一实施叶05.R4+12.R2.3，待A收口后串行派openspec-apply，共享assembly/checkpoint_test.go与12域docs）：settled()/attemptOf()内存miss时按身份点读consumeKey/attemptKey（TierFacts.Get），或owedOfWork遇未持有任务先readTaskLocked补读三族再判欠——有界O(owed)、不新增第二份已消费真相、不回退G83 A/C、不把已结收据塞回切片。尝试拦截与settled共用同一加载缺口，修账户即同修。
- **未证**：attempt2精确cut偏移与notice重提交时序交错（临时根已清，日志不足以唯一确定）。红锚TestUnloadedSettledReceiptStaysSettled(05.R4)＋跨进程TestRedeliveredNoticeAfterRestartSettlesOnce(12.R2.3)。
- **C(04.R3)已五查通过**：正锚-count=10复跑绿、改动恰白名单三文件、产品零改动、身份判定单一来源；既有vet unreachable属HEAD；长跑消费者锚归09.R3未冒认。

### R270 A/B核销＋修复叶与D接续派发（2026-11-18）

- **A核销**：12.R2.1/R2.2已勾＋执行记录（12域tasks）。九锚-race×10绿；全量assembly确定性红仅S5；boot成本锚全量并发下偶发墙钟红、隔离-count=5全PASS、计数面全平坦零⇒归未校准2×墙钟界噪声（09.R2校准），非A回归。
- **B核销**：03.R4三锚-count=10绿（含反向对照），改动面核实（host_document.go实+2行）。
- **C/E**：04.R3复验通过；E定因落账R268。
- **派发（openspec-apply，写集不相交）**：修复叶＝05.R4+12.R2.3（账户settled/attemptOf内存miss按身份有界点读，修根因B；红锚TestUnloadedSettledReceiptStaysSettled＋跨进程TestRedeliveredNoticeAfterRestartSettlesOnce）；D接续＝13.R2a/R2b剩余（重指MUT-EVID-04锚到A当前foldMapped边界＋mutations自测收口＋golden绑定字段＋13域docs＋真实短runner→checker；R2a/R2b核心已绿禁重开）。
- **并发归因约定**：D控制树自测编译根模块，修复叶编辑source期间瞬态红归D重试；修复叶assembly测试不受D（D在副本树操作）影响。
- **修复叶收口亲验**：05.R4+12.R2.3实现完整（撞150轮于记录收尾，记录已写完）。修法仅durable.go（unseatedTaskLocked＋readReceiptLocked精确键Scan，因FactLog端口仅Put/Scan/Sync；命中不回写、不造第二真相、G83 A/C未回退、attempt同路径不另预装）。编排者复跑：TestUnloadedSettledReceiptStaysSettled/TestUnloadedOwedReceiptRemainsOwed/TestConsumptionFailureKeepsTheNoticeOwed绿，state+task锚-race×10绿，跨进程TestRedeliveredNoticeAfterRestartSettlesOnce count=1 PASS（agent记录-race×10＝30PASS，编排者后台复证bb3dv4b6c）；runtime/state+task+agent全包回归无红。05.R4/12.R2.3已勾＋执行记录诚实（未验证面明列）。
- **新留未闭合面**：①loop.go:373送达事实写失败后继续投递——修复叶诚实未闭，需「写失败是否阻断投递」语义取向（下一叶或裁定）；②12.R2.4成本量化（LoadStats不含三条点读，口径另立）；③R35独立审＋整域复跑未做。
- **修复叶终核**：跨进程TestRedeliveredNoticeAfterRestartSettlesOnce编排者复证-race×10 ok（158.4s），与agent记录30PASS一致。05.R4+12.R2.3完全核销。
- **12.R3派发（openspec-apply）**：卸载安全拒绝（R1裁定：具名拒绝＋指未完成项数＋保原对象，空/已结可卸载）；先纠正S5锚旧前提（原断言对象消失与策略相反）；未完项数由拥有者可选face陈述非猜JSON；代际同权威。与D写集不相交。预算有限先核心disposer拒绝＋S5纠正，代际部分撞预算写停放账。

### R271 D接续核销＋两裁定＋runner既有缺陷登记（2026-11-18）

- **D接续核销**：13.R2a/R2b已勾。编排者复跑evidence 116 OK、mutations 55 OK、validate_registry stale=[]；runner 39（D复跑两次）。3条漂移锚重指（MUT-EVID-04＋MUT-CAND-01.8两条，同类同处置）真跑killed/assertion_hit。写面守约（runner/evidence首轮+355/+230累计在工作树，golden未改）。
- **裁定①（R2b验收面）**：R2b树绑定能力已证（单份现树绿＋bundle绑定错误0条）；「完整同树证据绿」的整包完整度（short1/10 long0/6 mutations0/1）本征属09.R3/R4矩阵交付，非R2b缩水。R2a/R2b核销，bundle整包绿回验归09.R3/R4。
- **裁定②（runner既有缺陷）**：`run_runtime_scenarios.py:773`把`-args -seed/-window`追加到调用内每包，但自定义flag仅examples/wechat-bot/smoke_test.go定义⇒`--scope event-short --seed 1 --window 256`形式9/9包全灭`flag provided but not defined: -seed`。git show HEAD逐字同形＝既有缺陷非本变更引入。**登记为09.R3前置必修**（矩阵需正确seed/window形态）；归13.R3a或独立runner修复叶，本轮D未修（runner超其写面）。D以`--repeat-index 1`等价面取绿，seed/window形态未实测入未验证面。【勘误R282：「9/9包全灭」不精确——repo grep坐实`-seed/-window`仅wechat `smoke_test.go:3452-3453`定义＋首行`//go:build smoke`，必死面＝8 distinct包（根7＋cli-host）＋wechat无tag构建，wechat smoke构建可解析；承载身份＝模块×包×构建标签（原令只写「包」差构建标签轴），以R282为准。】
- **D诚实留口**：13.R2本体N6（手填killed→产物）、R3/R3a/R4（lint/preflight/CI接线，禁改下一波）、六长真跑、合同对恶意伪造无认证（原声明）。
- **08.R5核销**：三锚-count=10复跑ok（29.3s）、整包15PASS、改动恰5文件+403/−28、checkbox勾、3负控红→复原、doc_budget ok(22149/22149)。源离线改整源盖戳＋两处陈述（非逐日志自校验，避跨档混合快照）；半成品用内容驱动2×17MiB越帧上界确定性失败。**R35审项**：新增journalReadHook包级注入点生产nil/test-only（确定性源变化注入替代flaky sleep），具名披露，独立审核其是否正当或需重设计。未验证面：file档源/目标未重测、盘满/权限/被杀未逐个取证（同一错误出口）、CLI子进程内源变化未取证。

### R272 12.R3核销：S5转绿，12域核心闭合（2026-11-18）

- **12.R3核销**：disposer经可选面`unfinishedWork interface{UnfinishedWork() int}`（type assertion沿reducer/emitter先例，不造第二注册体系）有未完义务时具名拒绝＋保原对象，空/已结可卸载。S5锚纠正旧前提（原断言对象消失→拒绝保对象＋义务保留＋重启同义务）后转绿——**assembly最后确定性红消除**。代际同权威为既有行为只补锚（genregistry/plugin_catalog/run.go未改）。state三锚-race×10 ok、S5 count=1 ok、assembly全量317s ok（boot成本锚本轮未误红）。
- **白名单外机械传播（已申报接受）**：assembly/checkpoint.go RegisterStateSlice签名func()→func() error（disposer返回类型带动，2行逻辑），调用点均_丢弃无行为变化。
- **R35审项**：①卸载路径今天无生产消费者（两生产注册点丢弃disposer，host.go治理接线禁改），拒绝可达性只在RegisterStateSlice＋真Boot层证（R1已知潜在能力）；②08.R5 journalReadHook test-only注入。
- **门禁累积债（F2前必清）**：comment_policy三模块scope现25 beyond baseline——12.R3自增3条已修，余25经逐行归属属前序叶（A/B/修复叶）新测试test-doc-line-too-long＋HEAD既有；基线不在各叶写面未改。须 dedicated清偿pass或逐叶追缴，F2正式lint前清零。
- **12域状态**：R2.1/R2.2/R2.3/R3＋05.R4核销；余R2.4成本量化、loop.go:373语义取向、R35独立审。

### R273 11.R4核销＋FactUncertain裁定（2026-11-18）

- **11.R4核销**：分类由名字白名单反演为契约反演（factCommitOutcomeOfCommit：仅ErrCommitUncertain归不确定，余皆FactRefused，据all-or-nothing契约非代码名，future-proof）；file多键/只读/关闭/超帧五腿-race×10＝70PASS＋负控红→复原；不确定须独立开档权威读（进程内未发布索引不可判不存在）；重复不花号正式入口-race×10＝10PASS＋负控。facts.go零改动（②已走GetEventInPartition耐久点读；git diff facts.go+62是A的chainHoldsFact累计未提交，非11.R4）。memory/ledger整包106s ok。file档真CLI缺席resource-gated（stub驱动，明列未验）。
- **裁定（FactUncertain处置）**：保留persistFact现状如实上抛（default:return err），同身份重试路径再裁决；不就地谎称「已存在」（守「不静默推进/不谎称」）。装配层上游显式处置策略登记为可选未来叶，非阻断。
- **消费者锚载体偏离（接受）**：原「打FactAlreadyPresent分支」生产不可达（前置判重＋landing串行化），改钉性质（正式入口读checkpointWatermark断重复不推进/不花号＋原文原序号），验收面三性质未缩、测试名/正则未改。

### R274 07.R2.1核销（2026-11-18）

- **07.R2.1核销**：已决审批按身份读链。载体判据＝a.factLog==nil时无耐久面切面照旧陈述（保4冻结负锚零改动的唯一解），有耐久面裁决归链切面不载。正锚TestSettledApprovalsStayReadableWithoutResidentEvidence-race×10 ok（历史递增而切面/常驻恒1）、4冻结负锚-race×10＝40PASS（governance/checkpoint_test.go确未改）、入口锚TestGovernanceBootReadsSettledAnswerByIdentity count=3 ok（两次Boot删approvals目录仍读链裁决）。3变异复原。读代价归索取者（Lookup无常驻时按身份扫request族）。attention_log绑定扫描次数清减归R2.3停放。approval+governance+assembly全量ok。comment_policy本次4文件0findings。

### R275 03.R2阻塞裁定：乙案扩面（2026-11-18）

- **03.R2诚实停摆（零代码写入）**：删compress_consumer.go的answered字段撞白名单外assembly/anchored_boot_parity_test.go:254 answeredCount=len(run.compress.answered)（:226/:229/:237三调用点）→assembly测试包编译失败。三件事（收窄judgments/删answered/已答按身份读链）原子不可拆：留answered则pending()按它过滤、按身份读链成死逻辑→无牙锚。代理停于白名单边界上报，纪律正确。
- **裁定取乙（保验收面）**：授权03.R2接续扩白名单含assembly/anchored_boot_parity_test.go——answeredCount改run.compress.struckNotes()（compress_consumer.go实现O(1)计数，仅note实际摘除一条未完判决时自增、重复不自增，语义==len(answered)），三处断言语义原样保留不缩面。否甲（删answeredCount三断言＝缩面，需R29明示，不取）。
- **关键前提须接续验证**：产物身份可由(request,compressionProducer,fold产物key)执行期算出（api/event/compress_product.go:74-83 sum不含正文）⇒按身份读链不需扫链/不需内存索引。若前提不成立须停报。
- **原则核对**：12-design:56禁「删answered/已结收据换常数成本」禁的是删权威记录，非禁以权威链读替换内存缓存；answered真相留在链上，删内存缓存＋按身份读链合法。

### R276 07.R2.2核销（2026-11-18）

- **07.R2.2核销**：目标载荷清减。正锚40PASS-race×10（stated goals恒1而stored events 3→11、常驻len(goals)==1、过期不入切面、restore后Current/声明/List正确）；负锚Count/Query边界绿（尾号不冒充条数）；入口TestGovernanceRecovery 10PASS（含无面书kept verbatim）；4变异复原。历史按身份可读经现有List()承载（Query/Count/List生产调用点0，新Read(id)即单使用方抽象，不造）；剪枝以g.log!=nil为门（无面书全量陈述保冻结锚）；不缓存历史（代价归List索取者）。07.R2.1冻结锚保持绿。governance+integration capabilities全量ok。
- **验收面改写（接受）**：TestGovernanceSliceSnapshot子用例1旧断言「切面陈述声明+裁决2项」正是被清载荷→改「切面陈述两尾号无目标，Bind后已结目标仍由流派答出」，恢复可读一字未减。
- **assembly交叉复跑停放**：03.R2在飞删answered致assembly测试包build fail（answered undefined），07.R2.2正确归因停放，待03.R2落地后编排者统一复跑./assembly。denial.go/integration文件确未改。

### R277 10.R9a盘点核销＋下游阻塞登记（2026-11-18）

- **10.R9a核销（只读盘点零代码）**：132命中全归属（生产调用1/间接9/定义28/示例9/测试35/断言6/注释1/主spec32/golden11），3负控拒绝（漏项/测试当生产/误删KEEP）。design §十四（548行11子节6表）＋tasks勾。dead_producers exit0但测试算消费者⇒绿≠零生产死码（生产调用图独立结论）；concept_budget exit1＝03.R2在飞compressSessions加HoldsCompressProduct瞬态（HEAD3方法），非基线变更。「12文件」确12、「4冲突」不成立（锚冲突0）。compatMaterialOf更正15面/11文件、9生产Port/Material成对⇒兼容路径生产可达0。第七条间接入口RecallPort.GetScoped/ReadSegmentScoped（tools.go:839晚绑定host_document:767-780）＋Run.RecallReads次级。冻结跨文件名断言耦合fold_test.go:108-150（R9b删该测试会静默失效折叠成本证据）。
- **下游阻塞（R9b/R9c/R7/R5处置）**：B1 architecture.json:1432/1437仍绑Stateful/StateRegistry→ticket superseded待迁锚；B2 Compactor 4主spec仍SHALL要求，原「resolveMemoryStore转正」不可执行（名0命中，活owner ledger.OpenTier/OpenAtomicStore）；B3 GetScoped处置待R9c；B4 compatMaterialOf甲案需抬api/plugin:Instance S1基线（越POLYMORPHIC_METHOD_LIMIT=2）——**倾向乙KEEP**（不为单使用方抬基线），终裁待R9c/R7；B5 ErrChainKeyRequired/RebuildSkipNotEmpty断言迁移待R9b先给等价断言。

### R278 03.R2二轮停摆裁定：A修run.go走序（2026-11-18）

- **03.R2接续实施已落（3/4锚绿）**：judgments收窄未完、删answered、struckNotes() O(1)、HoldsCompressProduct按身份读链（前提成立：cmpout-sha256(request+producer+foldkey)正文不入标识，执行侧不需扫链；seat侧只有request ID算不出身份）。三面成本实测：body 1046B恒定、构建分配1352B/调用恒定（settled 0/64/256三档）、常驻2项恒定。真实入口锚-race×10绿。
- **更深真实缺陷（实测推翻R275假设）**：TestAnchoredBootMatchesFullBoot 10/10稳定红。两条恢复走遍先note产物后seat判决：full struck=20/pending=0、anchored struck=19/pending=1（run.go:2798 NoteProduct跑在replayChainPastCut:2800前，产物request在behind段后到）、fallback struck=0/pending=20（run.go:2817-2840索引新→旧重放，产物恒先于request到达）。**struckNotes≡len(answered)不成立**（fallback 0 vs 20）。生产后果：fallback后20条「产物已在链」判决永久留未完账（Execute预检只摘plan命名判决，材料已折完早退永不摘），每次checkpoint重序列化——真实correctness/cost缺陷，非仅测试。
- **裁定取A（保验收面，修根因）**：run.go属主落两处走序——①run.go:2798 NoteProduct后移到重放之后（request先seat再note）；②run.go:2817-2840 fallback改commit序（旧→新，request先于product到达）。注意fallback循环`if agent==""{agent=owner}`取首到达ref的agent，但slot= session+partition、partition由agent派生⇒同slot同agent，方向改变agent源不变（须验证）。否B（fallback面降格＝验收面缩水，不取）。
- **代理已否的替代（0残留）**：NoteProduct自清「待判答案」暂存表——anchored转绿但full每产物note两次→暂存表永久累积（parked 19/20）＝换名把answered请回常驻态，违12-design:56。字段探针已删grep=0。
- **证明走序唯一解**：正向走遍后重note链上20产物→pending=0 struck=20。

### R279 10.R9c核销（2026-11-18）

- **10.R9c核销（保留＋验证，零生产改动）**：TestOriginalReadEntrypointsPreserveBytesAndScope 10/10PASS（7子测：原字节/scope缺失/未提交缺失/过期/完整性不符/损坏快照/tier满足scoped端口），4变异复原（reference.go/kv_original_store.go零diff shasum一致）。消费者链逐跳复读host_document:775-779→get.go:153→tools.go:924/931→reference.go:258，tool/recall+capabilities+history三包ok。examples/contract/memory_ledger无测试文件＝编译锚非行为锚（诚实）。
- **七入口处置**：#1 OriginalStore.Get KEEP（fold-cold-root生产）；#3 GetScoped结案KEEP（删它→tools.go:168运行时断言静默false→召回全死，接口满足面是可删性判据非调用计数，「生产直连0⇒裁删」推论作废）；#4 ReadSegmentScoped KEEP（唯一活入口）；#7 RecallPort＋7bis RecallReads KEEP（绑定所需）。
- **待裁转派**：#2 OriginalStore.ReadSegment/#5 ScopedReader.Get/#6 ReadSegment裁删——R9a冻裁删但越写面（需skeleton_assert:76/82/84＋examples/contract＋integration 8处NewScopedReader＋product_surface_baseline:18 R7写面＋docs/api R6写面），须独立派发单扩写面＋R7/R6联动＋先给等价断言落点。本叶保留不删。

### R280 13.R3a核销（2026-11-18）

- **13.R3a核销（机制 verified，全量lint绿停放）**：DoD证据接线——TAGENT_RUNTIME_EVIDENCE_DIR→$DIR/mutations，lint.sh:150/152/159传--mutation-evidence，preflight序short→long→mutations生成→bundle现树绑定→DoD消费严格递增，check_dod_coverage.py:616/631/645先绑后计（复用check_event_source_binding单一实现）。自测51/51（+2绑定负例+10壳入口）、evidence 116、workflow 55绿。真壳入口（shim记argv后exec真解释器）＋真树旧产物拒绝（/tmp/r270-mut-evidence→EXIT1，content/closure digest mismatch，P0-P8 unproven）。两级红绿演示复原sha256一致。转发scripts/lint.sh/preflight.sh字节未动（exec透传）。
- **偏离（接受）**：bundle完整性留preflight非lint（10短+6长+1变异同树，长只在preflight生成；放lint会使「今天没跑长跑」＝「lint不可用」）；DoD端闭包复算已足「错树」面。单独lint不验17件套齐全归preflight/CI。
- **停放（F2前清偿）**：全量lint绿待comment_policy超基线＋DoD P4标题漂移＋P0-P8运行产物；真preflight全链需授权（6长6270s/片）；CI接线归13.R3（ci.yml须授权，TAGENT_RUNTIME_EVIDENCE_DIR CI未设）；docs/OPERATION.md:94-97裸lint命令未提新变量（白名单外待补）。行为变更（有意）：裸bash scripts/lint.sh现DoD段具名红指preflight＝设计的缺证据红。

### R281 run.go走序修复＋03.R2核销：全量assembly绿（2026-11-18）

- **run.go走序修复核销（R278 A案）**：①NoteProduct后移到replayChainPastCut＋RestoreProducts之后（request先seat再note）；②fallback降级走遍改slot内按ref.seq升序重放（**方向不敏感**——实测refs升序seq88→19致反向遍历=新→旧，根因「遍历方向随tier变」非循环写法，故按seq排序比R278字面翻转索引更稳，翻转只修一半）。三面struck/pending修复前full20/0 anchored19/1 fallback0/20→修复后全20/0。agent来源70ref全owner=agent-a单一验证（同slot同partition方向不变）。parity-race×10绿、03.R2六锚60PASS、A五锚25PASS、**全量assembly两次0FAIL（302/304s）**、build/vet/gofmt绿、openspec strict valid。
- **03.R2核销**：压缩常驻态清减（judgments收窄/删answered/struckNotes O(1)/HoldsCompressProduct按身份读链）＋走序修复后parity绿。三面成本body1046B/构建1352B/常驻2项恒定。
- **偏离（接受）**：R278前提②字面「翻转循环」与现状不符（循环已旧→新但refs升序），代理按语义落地方向不敏感排序，更稳。EachIndexNewestFirst文档称newest-first但blankTypedTier给升序＋空typ——不在白名单未修，②方向不敏感不依赖。
- **未验证面**：fallback面无Boot级端到端锚（仅seedAnchoredReplay直调blankTypedTier，anchored面有Boot锚绿）；03.R2独立只读审未做（勾＝实现＋自验，放行待审）；tests/integration/host未跑。

### R282 13.R2c核销：runner -args缺陷修复（09.R3前置解锁）（2026-11-18）

- **13.R2c核销（编排者亲验，非采信汇报）**：正锚`test_run_runtime_scenarios.py`我亲跑**Ran 40 OK**（新案`test_seed_window_only_reach_defining_package`逐字符line547）；消费者锚stored `/tmp/r282-checker.json` `ok=true`/`errors=[]`/29test·135anchor·9包·180事件行；负锚`/tmp/r282-prefix` `verdict=unattested`/`exit1`（`./tool/exec`带`-args -seed`无smoke tag→flag未定义，6 mandatory subtests全missing）；runner真产物`before==after==sha256:6706cead`(modules3)/`contract_digest e1dfb96b`未变/`verdict=match`/repeat_total10。写面4文件+774/-41，勾选[x]line21、13.R3行已复原line28。
- **承载面自决采纳（勘误R271裁定②）**：代理把承载身份从「包」精化为「模块×包×构建标签」，repo全域grep坐实`-seed/-window`仅wechat smoke定义⇒更合单一真相，采纳并勘误我原令。`EVENT_CALIBRATION_FLAGS`静态声明＋自测读真源对齐钉（smoke_test.go改build tag自测先红）＝有机械守护的声明非第二真相；否合同字段（golden冻结）/按长跑用例传/运行期grep三替代（design§十五·三）。invocation的`packages`改由`command_packages()`从实际argv反读（记录即执行同源）。
- **并发污染甄别**：`approval_store.go` mtime19:57:46（07.R2.3在飞），runner真跑窗口19:45-19:48内无`.go`写（attention_log19:43在前、approval19:57在后）⇒stored evidence对该单权威；runner是`.py`改动不入`.go/.mod/.sum`指纹。现树checker复跑须待07.R2.3收口（否则闭包漂移红属并发非runner缺陷）。
- **未验证面**：`event-long --seed/--window`真跑归09.R3（六长需授权6270s/片）；同组混合承载/非承载拆分分支今天合同不存在只自测验；CI short不传seed、long只选wechat smoke（repeat本1）故生产链两处不受本缺陷影响。

### R283 07.R2.3核销：绑定阶段全历史恢复工作移除（2026-11-18）

- **07.R2.3核销（agent撞150轮但实施＋记录已落，非半改态）**：勾选[x]line37＋执行记录line39＋design§二十二均在；`go build ./...`＝0树可编译。编排者亲跑全部具名锚——vet/gofmt0、冻结负锚组exit0(4tests×10=40)、governance组exit0(3×5=15)、读数锚组**50PASS/0FAIL**、assembly真实入口锚`TestGovernanceBootWorkTracksOpenItemsAndTail`×10＝**10PASS/0FAIL/DATA RACE0**（正式Boot链，run.go:1173/2488生产日志在场）。
- **三面读数逐字坐实**：12轮与24轮已结历史下`scans=3 decoded=0 resident=3 allocated=3472`**完全相同**（绑定工作量与已结历史无关），对照臂`scans=2 decoded=78`证同一store真装78份正文⇒平坦非「测试看不见」。生产切片字节：已结6→12轮governance载荷逐字节同、都只陈述3项。
- **绑定判据＝stated（RestoreEvidence置真）**：有切面陈述只读在力义务各自answer键尾（3义务3读），无切面照旧全折（保三枚面锚＋重启册载体要求，反事实检验design§二十二line530）。切片侧零改动（字节由R2.1/R2.2语义定）。Lookup/Decide两扩展（开放记录答pending/`requestToRule`按身份点读）由变异M3/M4证是既有锚`TestGovernanceSliceRefusesToDropAnOpenApproval`必要条件非顺带功能；M1-M4均/tmp备份复原。
- **写面4文件**（approval_store.go/attention_log.go/attention_log_test.go/assembly/governance_checkpoint_test.go）＋design/tasks；`attention_store.go`/`approval_store_test.go`的diff属R2.1（本叶零改动，design§二十二line519明示）。
- **未覆盖（如实）**：tests/integration禁跑；停机被裁项裁定人名Lookup常驻路径读不到（改动前亦如此，断言改走Decide带裁定人）；assembly层精确读次数无计数缝（用坏正文不被读＋`DeliveryGaps()==0`间接证，精确计数留包内factStore）。**07域R2.1/R2.2/R2.3全核销**，余R35独立审。全量assembly回归确认：**ok 305.312s／0FAIL**（b5ratexmq，树静止两写单收口后权威读数），07.R2.3落地后「全量assembly绿」里程碑仍成立。

### R284 01.R1a＋12.R2.4核销：守护独立候选＋边界读成本量化（2026-11-18）

- **01.R1a核销（编排者亲跑三要素）**：self-test 50PASS exit0、checker真树errors=0（go_files32/containers5/mounts0/consumers1/exceptions0/registry_impl2/present2）、Go mount锚2PASS exit0。变异29/29（/tmp副本errors.append→print各红自有用例，主树两文件sha256未变）。**候选补丁集为空**：检查器/自测/architecture.json mounting节/plugin_mounts.json/lint.sh:127-128接线均在HEAD 0f84b2d1⇒「独立候选」＝从单体提交拆语义提交（git历史操作非代码补丁），R35可声明该补强部分完成。**锚路径勘误**：plugin_mount_test.go已不存在，两锚现址plugin_catalog_test.go:58/:84（design§10.5已注）。01.R1父/01.R1b未勾。
- **12.R2.4核销（纯测试量化，编排者亲跑）**：成本锚`TestCheckpointRefusalCostAcrossSessions`×10＝10PASS/0FAIL，三臂读数逐字稳定——无错与瞬时错后同身份成功`valueReads=92/walkVisits=4/decoded36`（零残留），长期错`valueReads=269(+192%)/walkVisits=93(+2225%)`/9cut全拒含2无关链/链更短40<44⇒代价落下次boot重放；边界`survivors=3/coldReads=9/warmReads0/absentReads0`＝每幸存者3次点读（索引segment_store:542＋窗meta:560＋正文:937），空区间照付⇒「空区间≠零读取」实测成立。负锚`TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames`-race×10 exit0、既有成本锚2枚×10 exit0(40s)。变异unappliedCutRefusal→nil红:4384、survivorPositions早返回红:4574，/tmp副本复原sha256==主树。写面仅assembly/checkpoint_test.go（parity/state checkpoint_test.go的diff属前序叶）。
- **裁定①（全局拒cut KEEP）**：全局拒＝冻结乙案不变量，负锚名`WhateverChainItNames`钉的就是跨链全局拒；细分（按会话/链）＝放宽冻结锚，禁。长期错阻无关链＋下次boot多读＝已声明量化的保守代价非缺陷。可选窄优化（只答seq读省每名1次正文读＋合锚分支两次调用）登记未来窄写面叶（run.go生产），不阻断、本叶不宣称零成本（R264撤）。
- **裁定②（符号勘误）**：12/tasks:43叶行`newestSurvivorSeq:3033-3044`陈旧——run.go零命中，实为`survivorPositions`(run.go:3083-3100，锚分支:2784/:2790，restoreSurvivors:2908内联同形)，判据不变只更正符号（已重写叶行标勘误）；R281「newestSurvivorSeq」同此勘误。§十八·3「稳态零额外成本」收窄「零额外折叠」（design§十九）。
- **未验证面**：12.R2.4 R35独立只读审＋整域矩阵复跑未做；loop.go:373未触碰（待裁语义分叉）；01.R1a候选隔离提交属git操作待编排者按R35条件评估。

### R285 01.R1b＋06.R5.a核销：生命周期联合守护＋提交关闭屏障承接（2026-11-18）

- **01.R1b核销（编排者亲跑）**：Python self-test50 exit0＋checker errors0 exit0＋Go生命周期锚-race×10＝**30PASS/0FAIL/0RACE** exit0（TestUnmountKeepsTheOwedItemsOfItsSlice checkpoint_test.go:3749／TestDisabledBuiltinHandsOverItsObligations plugin_catalog_test.go:173／TestGenerationSwitchKeepsTheSameStateAuthority hotupdate_generation_test.go:858）。断言本体逐段读：拒绝保原对象(:3790/:3802/:3821)、空/已结可注销(:3833/:3836)、换代同权威(:896)。检查器零改动sha256同R1a。写面仅01/tasks。**01.R1父未勾**（待R35独立审）；01域守护补强R1a+R1b实施完成。
- **06.R5.a核销（编排者亲跑）**：ledger屏障锚-race×10＝**20PASS** exit0、assembly新锚TestCloseReportsTheActualBarrierFailure-race×10＝**10PASS** exit0、既有两枚×10 exit0。旧锚→现行路径映射4行（一处「重述退回rebuild」随退位删**无承接**，诚实记design§二十七·一不造假设锚）。可红演示：阶段计量坍缩→红named、包装层提前拒绝→红，复原sha256同。**无需生产修改**（读引擎已持状态命名阶段，非加计数器/后门）。如实限制：assembly经StorageConfig.Engine注入触不到OS级Sync(2)，真syscall失败由ledger两os.Pipe锚负责不外推。写面journal_test.go+211/journal_kv_test.go+99/checkpoint_test.go补差+06docs。06.R3.a未勾（待09.R2）。
- **comment_policy现状（06.R5.a核出）**：baseline.json counts为空⇒该闸本变更之前即全树红；-v -no-baseline逐条核新增锚与helper**零finding**，残留全在写面外既有行(checkpoint_test.go:3048/3291/4178)，未动基线。25债清偿pass仍待（F2前）。
- 全量assembly回归后台跑（b3hip5mac）确认后更新。

### R286 loop.go:373写侧分叉登记＋12.R2.5叶＋全量assembly三绿（2026-11-18）

- **全量assembly回归**（b3hip5mac，01.R1b+06.R5.a落地后累积树）：**ok 304.631s/0FAIL**。本segment三跑全量assembly均绿（305.312/304.631＋06.R5.a自跑315.4），里程碑稳。
- **loop.go:373写侧分叉（精化倾向，不盲裁）**：12.R2.3已[x]但其负锚`TestDeliveryFactFailurePreservesTheNoticeObligation`未建（grep零命中）、loop.go:371-375写侧未闭合＝R2.3诚实留口（读侧05.R4已修）。域权威12-design:41「**不承诺外部发送exactly-once**」＋loop.go是`commitSettled`**先于**`in.deliver`（收据先于投递，次序本身可疑：收据落但投递前崩溃⇒重启settled不再投⇒模型永失通知）⇒甲（阻断）可行但非唯一解。登记新叶**12.R2.5**（写侧补差承接R2.3），**方向三选一由实施叶先枚举语义＋既有锚再判**（甲阻断/乙保义务不停沿用单元三模板/丙重排次序），真分叉停报待裁。冻结锚不变。:401 commitProgressFact审计面风险低不强改。
- **并发准入定案**：assembly是universal bottleneck（几乎所有叶verify经assembly、各写assembly依赖包）⇒**.go写单串行**，每单收口再派下一。Python守护单（01.R1a/b）可与assembly单并行（写面不相交，Go verify撞并发写则归因defer）。
- **就绪队列（串行）**：12.R2.5（runtime/agent，方向先验证，本轮派）→05.R3（assembly/state Seat尾三态复核，前置06.R4[x]met，锚已存在）→02.R1（assembly declaration入口复核，消费者锚tests/integration/acceptance须确认不禁跑）→06.R5（per-generation成本回归frame×2/closing×2.19，当前故意红symptom锚，生产fix）。06.R3.a待09.R2计量定义；09.R2a口径只读可先行；10.R9b/#2#5#6删除待等价断言＋R7/R6联动；comment_policy全树红（baseline counts空）待dedicated清偿pass（F2前）。

### R287 12.R2.5方向裁定乙＋写面扩facts.go＋机制勘误（2026-11-18）

- **12.R2.5方向先验证单停报真分叉**（未改.go，纪律正确）。编排者亲读权威核实取证：facts.go:972-1004 sink doc明写「receipt MUST follow handover，stop between two leaves owed restart hands same result over again」＋code序bookNotice→Backflow(void)→Consume无条件；loop.go:351-378 Backflow void、commitSettled写TypeTaskSettled送达标记(非收据)、失败consumerWarn+continue in.deliver；retainNoticeOwed(facts.go:1011)既有机制(closeFinished用，Release claim保acceptance)。
- **机制勘误（推翻R286叶行归因）**：commitSettled写送达标记TypeTaskSettled**非消费收据**；收据写入者实为facts.go:1003 store.Consume（settled()读consume/reject键durable.go:421，唯一写者recordConsumption:833/858）。真实缺陷＝标记commit失败但Backflow void⇒sink无条件Consume⇒**义务被清但送达标记永缺**⇒3读面(fold.go:132/request.go:1288/host_tools.go:1002)缺送达正文记录、重启不再投(收据已持)。非「链无收据再派」（R286误，以本条为准）。
- **裁定乙（保义务不停投递，复用既有机制）**：Backflow报标记拒收信号→sink走既有retainNoticeOwed而非Consume⇒义务保欠账，in.deliver照常（进程内交接非外部动作loop.go:2074-2084），重启重投补标记（链按key幂等至多一条耐久收据+标记）。依据：sink doc「restart hands same result over again」接受at-least-once交接＋域12-design:41「不承诺exactly-once」＋:233「沿用单元三模板不新造机制」。**否甲**（阻断in.deliver）：非权威所需、增liveness风险（标记持续失败则模型永不见）、Backflow void下阻断须更大重构；**丙证伪**（收据后于交接，崩溃落两者间Consume未执行⇒settled读不到⇒重启重投同身份幂等，无「模型永失」）。
- **写面扩**：12.R2.5增`assembly/facts.go`（Backflow返回拒收信号＋sink分支retainNoticeOwed）＋loop.go/loop_test.go；Backflow签名void→error须**枚举全部调用方级联更新**；retainNoticeOwed若须泛化doc（closeFinished专用→标记拒收通用）同批改不新造第二机制。
- **可反转探针**：若实施发现retainNoticeOwed用于标记拒收破closeFinished语义/致double-Release/重启重投产第二条标记(非幂等)⇒裁定乙错停报重议。冻结锚TestDeliveryFactFailurePreservesTheNoticeObligation非空守卫钉：注入确发生/义务确仍欠(未Consume)/重启后收据+标记恰一条。

### R288 12.R2.5核销：乙案落地（送达标记拒收→保义务欠账）（2026-11-18）

- **12.R2.5接续单撞150轮但实施＋锚＋勾选＋记录全落**（同07.R2.3型）。编排者亲验：go build ./...＝0、go vet runtime/agent+task＝0（Backflow void→error签名级联编译过，除sink外调用方均测试直驱、忽略error返回合法）；负锚`TestDeliveryFactFailurePreservesTheNoticeObligation`-race×10＝**10PASS/0FAIL/0RACE**(22.8s)、真实入口`TestRedeliveredNoticeAfterRestartSettlesOnce`-race×10 exit0(156.9s)、Backflow既有锚exit0、runtime/task负锚exit0。
- **乙案落地三处核实**：①loop.go:373 Backflow签名void→error，:396标记commit失败改`consumerError`(ERROR级，原consumerWarn)，:429 commitProgressFact审计面仍WARN不动；②facts.go:1005-1008 sink `if refusal:=Backflow(...);refusal!=nil{return retainNoticeOwed}`否则Consume；retainNoticeOwed doc泛化「两成因(close已返回/标记被拒)同一处置」不新造机制；③in.deliver照常(进程内非外部动作)。skeleton_assert.go签名断言级联(白名单外编译强制，已申报，同12.R3 RegisterStateSlice先例)。
- **负锚非空守卫扎实**：注入TypeTaskSettled拒收(struck==1)→turn完成2决策→链0标记→mailbox欠1(ClaimPending)→0消费收据→重启欠同通知0收据→重投后标记+收据恰1→二次投0续轮幂等链仍1标记。**可反转探针(R287)未触发**：closeFinished语义未破、无double-Release、重启重投幂等。
- **记录勘误**：agent记录误名负锚文件delivery_fact_obligation_test.go(不存在)，实在host_test.go:481(锚＋helper＋401行)，已重写叶行标勘误。host_test.go+401留R35合并前holistic夹带扫描。
- 全量assembly回归确认：**ok 316.824s/0FAIL**（b60hzjw7g，Backflow void→error级联后权威读数），无回归。**12域R2.1/2/3/4/5全核销**，余R1两门落点待裁＋R35独立审。

### R289 05.R3核销：Seat尾三态复核（零生产改动）（2026-11-18）

- **05.R3核销（复核叶，三态已成立无需调整，零.go写入）**：编排者亲跑正锚三态×10＝**30PASS/0FAIL** exit0、消费者锚×10 exit0(52.3s)；checkpoint.go mtime18:13/durable.go18:00均早于开工21:53⇒零生产改动坐实。三态：①未找到框架检查点/无提交序⇒nil具名(checkpoint.go:900-903/:881-885，durable.go:459 tail==nil整份读)；②非nil零年轻⇒非nil空集(:886/:904，durable.go:471零点读)；③精确尾键集(:888-896按Type==TypeCheckpoint∧Session==""停走不按键拼写)。旧「永不为空集」已不存在(design:606 R80勘误，生产无守卫，:847-850用tail!=nil区分未陈述/陈述为无)。
- **08.R3耦合核验**：退位已删(非测试grep TailKeys/RetireFacts/keptHotKeys/retireSettled零命中)，Seat尾取数走seatDurableAccount(:822)→tailFactKeys(:877)不依赖TailKeys⇒无静默漏座。变异M1(丢Session=="")/M2(tail==nil→len==0)/M3(!found返非nil空集)各红，-overlay /tmp注入主树零落盘复原sha256同。
- **自决（接受）**：「非nil空集」实现为「非nil且零年轻任务」(map携切面自身提交的非任务键)，锚注释checkpoint_test.go:2733明写按任务面判不按长度判⇒非削弱。runtime/task旧故意红TestConsumptionFailureIsReportedByName随全量回归转绿。
- **待派跟进**：引擎侧「无提交序即具名退化」spec具名操作表条目属memory/ledger/06域spec（05.R3白名单外未写），登记待派。**05域**：R3/R4核销；R1/R2未就绪（R1正锚缺席＋红前提疑失效、R2待G2合并）。

### R290 02.R1核销：声明入口复核（零生产改动）（2026-11-18）

- **02.R1核销（复核叶，声明入口已成立，零.go写入）**：编排者亲跑正锚3+负锚2+负锚二2＝**7PASS/0FAIL** exit0、消费者锚acceptance `TestScenario11ConfigDrivenSubagentAndPipeline` exit0(3.4s，t.TempDir+golden+替身模型非禁跑非真实数据)、check_plugin_mounting errors0 exit0；plugin_catalog.go mtime17:53/compress_mount.go00:59均早于开工⇒零生产改动坐实。五状态解析集中唯一入口`declaredPluginsOf`(plugin_catalog.go:275-314)，生产唯一调用链Run.agentPlugins(:380-403)→tools.go:245→prepare.go:124→AgentBinding.Plugins；目录唯一构造builtinPluginCatalog(:110-206)，checkPluginCatalog(:210-222)启动首步拒重名/空名。
- **义务不静默移除核实**：plugin_catalog.go不导入runtime/state不触StateRegistry，生产RegisterStateSlice调用点(governance_checkpoint.go:53/host_delivery_checkpoint.go:152)均非目录插件⇒「禁用即丢消费者」声明面不可达(与12域E12-7勘误一致)。联合锚TestDisabledBuiltinHandsOverItsObligations(plugin_catalog_test.go:173)钉run级仍在/注册表点名/cut陈述oweItem/unmount具名拒绝报1 unfinished/拒后名字仍在。
- **自决未证面（接受，诚实登记非假称）**：①未补「禁用声明+重启待恢复义务」联合锚（声明面与StateRegistry无因果，补＝自证同前提；出现「声明可禁用持切片插件」生产路径须重开）；②未修贡献实例名撞builtin不可寻址（NativeAgent.Plugins今日无生产者，不为无第二使用方建防御；出现生产者须具名拒重名）；③reload后插件集替换未单独立锚只引code事实(host_reload.go:717/prepare.go:124)。
- **02域R1核销**。共同门go build0/assembly+api/config+runtime/state -count=1全ok(320s)/checker0。

### R291 06.R5核销：成本回归经08.R3退位删除已消＋判据口径裁定（2026-11-18）

- **06.R5接续核销（零.go改动，重测＋存活路径重钉判据）**：编排者亲跑正锚`TestSymptomAnchorPerGenerationCostFlatOverALongRun`×3均exit0（agent×5均exit0，首窗中位81.85/末窗89.04ms全平），屏障锚exit0；journal.go mtime01:17/segment_store.go18:38早于开工⇒零生产改动。**08.R3退位删除后该锚不再红**（2026-11-16「故意红」失效）：屏障frame1道/closing3道（优于改前7/9、旧改后3/5），reset=grow=0逐代恒定无整表重述。assembly全量317.5s零具名红（旧记录两枚预期红已消）。
- **判据口径裁定（RATIFY核销）**：叶原要求「绝对成本回改前带132-140/243-264ms」——该带属退位世代形状（frame/closing内嵌relocate→rewrite重述），08.R3删退位后机制不存在。实测81/89ms低于该带两端＝有害方向消失＋逐阶段全平，核销正确（要求回该带＝要求成本更高，荒谬）。**非比值替代**（1.026-1.174仅并列）。叶验证面文本已就地勘误（line80）。判据随机制失效而勘误，非放宽门。
- 屏障判据按存活路径重出＝复用06.R5.a两锚（不重复造，checkpoint落盘与提交共用journalKV.Commit→sync→confirm→flush同一路径，assembly/checkpoint.go无独立屏障点，元规则二）＋overlay探针逐相位计数。负锚可红：去flush字节预算判定→exit1红文点名「log+index barrier want log's alone」，frame道数1→3耗时5→21-35ms，复原exit0。
- **comment_policy债更新**：06.R5核出 -no-baseline 8模块**63 findings**（memory17在kv_original_store_test.go/segment_store_test.go等其他叶写面），本叶零.go写基线未动；2026-11-16「0finding」时点已不成立不追认。正式三模块scope beyond-baseline债须dedicated清偿pass（F2前＋解锁08.R3/R4核销）。
- **06域**：R4/R5/R5.a/R7核销；R3.a待09.R2计量定义。

### R292 03.R3核销：预算租约默认对账复验（零代码偏差）（2026-11-18）

- **03.R3核销（复验叶，仅03docs写面，零.go改动find -newermt60min=0）**：编排者亲跑正锚api/config2 exit0、负锚assembly2 exit0、消费者锚host**3PASS/0SKIP** exit0（t.TempDir/StorageMemory+scripted provider可跑、非真实数据非长测，禁跑顾虑对这三支不成立）。原3.1-3.4逐项无代码偏差，旧3.4 host面红已消。agent另跑assembly八锚8/8、replay3/3、StrictDecode29/29、assembly全量322.57s0FAIL、host -short201.73s0FAIL、openspec strict valid、build0。
- **真实宿主入口双证**：Assemble声明面＋Boot宿主文档面（host_document.go:117 yaml键/:208 mapDocument透传宿主不填默认），不以核心声明路径替代。3.2界由plan承载常量降策略缺省（age_segments.go:423-426/age_plan.go:83-88 summaryBudgetOf与thresholdOf同形），提示词裁剪同源无第二字面界，生产字面2048仅剩两处策略缺省定义。
- **待派（白名单外doc滞后2项）**：①docs/wiki/agent/event-flow.md:29仍写「按DefaultAgeSummaryTextLimit(2048)裁」与原3.2[x]不符；②docs/wiki/platform/org-hot-reload.md:45指纹字段枚举缺summary_budget_bytes（03.R4已登记仍在）。
- **未证项（缺锚非缺陷）**：本域spec「界无硬编码来源」AND子句（极小预算退化）无具名锚，行为由ageSegments.go:650-680 room<0→0＋末行cutBytes按构造成立，建议派测试补钉。host非-short全包（真进程deployment soak长测）需授权未跑。独立只读审未做。**03域R1/R2/R3/R4全核销**（R2/R4独立审待）。

### R293 comment_policy超基线债清偿：66/66清零两门绿＋越界追认（2026-11-18）

- **清偿核销（编排者亲跑两门）**：正式门(POLICY_DIRS=`. examples/wechat-bot examples/cli-host`)修前66finding/66beyond exit1→修后**0/0 exit0**；产品零容忍门(-no-baseline 8模块)修前63→**0 exit0**；go build0。16文件注释形状修（test doc单意图行+≤160bullet+docs/索引；production doc≤2段+bullet；free-standing事实已承载者删/未承载挂回函数doc bullet；audit-marker6处措辞改写；去重checkpoint_test.go重复bullet）。
- **baseline counts空真因（诊断正确非损坏）**：c1a5d485收敛终态(155→0零点)，`git show c1a5d485:baseline.json`与HEAD逐字节同(counts:{})，readBaseline缺槽按0计，-strict要求空基线＝converged end state⇒门即零容忍。未手填基线。调和R272「25beyond」/06.R5「63 -no-baseline」/baseline空三读数（不同scope/mode）。
- **归属（git archive HEAD实证）**：HEAD树7finding、工作树66⇒59本会话写面(属①)+7HEAD既有；c1a5d485..HEAD baseline diff空+零点⇒7条HEAD既有亦本变更引入(非pre-existing)，一并清正确（产品零容忍门要求0）。属②待裁清单空。
- **越界追认（四判据）**：8finding落4产品文件(facts.go×2/run.go×3/goal.go×2/checkpoint.go×1)，agent改产品注释行(白名单原写测试.go)。追认：①必要(产品门须0)②最小(仅注释行)③无行为变更(编排者spot核goal.go stated()/g.log!=nil、facts.go chainHoldsFact/commitFactNoCadence、checkpoint.go unappliedCutRefusal、run.go survivorPositions逻辑符号全在位+build0+agent全量assembly351.7s/ledger106.4s/governance/state/task绿)④已自申报。**R35合并前holistic必核**：4产品文件本单hunk确comment-only(累积diff无法逐hunk隔离，符号在位+测试绿为间接证)。
- doc-budget slack0(22149/22149)⇒细节压代码注释未迁wiki。**F2 comment_policy阻断解除**；08.R3/R4核销解锁（两注释门0）。

### R294 R35 双路审：证据 frontier 7 项坐实，生产审配额中断由编排者接手（2026-11-18）

- **证据/门/夹带审（B）完成**：扫描门278/278、测试739/739、docs339/339 hunk（语义为风险抽样非全审），frontier 7。编排者复核坐实：①MUT-CAND-01.2 锚失配（12.R2.5 sink 改拒收分支后旧裸 Backflow+Consume 0命中，自测2确定性 stale＋1 git-index 环境 error）；②消费者不核 seed/window 实际 argv（check_event_invocations 只核 run/tags/race，run_config 自报即可）；③gate_tool 身份与 status/verdict/counted_killed 一致性未消费；④DoD run_verdicts 只绑 source_binding 不调 check_mutation_evidence，红控制/not_evidence_backed 可被接受；classify_control 的 `_race_waived` 只看 race frames 不排除普通业务失败；⑤runtime-plan 嵌套 lint 未备证据环境致1红，CI 未接线属13.R3未生效面；⑥concept_budget compressSessions 4>3 未登记＋DoD P4 标题漂移（P0-P8 缺运行产物为预期）；⑦文档4处矛盾/滞后。
- **生产正确性审（A）因模型配额429中断**（152工具后无报告），非代码失败；编排者接手生产 diff 审查，不采信未完成审为放行。B 驳回：host_test.go+401 非同属性双锚（两锚 race×10 绿＋overlay 恢复无条件 Consume 负锚红）；comment_policy 两口径0且 baseline 未抬（git-index fallback 警告只作内容测量）；journalReadHook 只调度真实 holdSource 不伪造判据。
- **修复叶登记**：13.R2d（生产者：变异锚重指＋race豁免不掩盖业务失败）、13.R2e（消费者：seed/window argv＋gate_tool/判词一致＋DoD全量结构校验）、13.R3b（runtime-plan夹具＋P4标题＋docs对账，CI仍13.R3）。concept_budget compressSessions 4方法须生产审裁接口形状，不先抬基线。串行派（共享13域docs/证据契约）。

### R295 编排者接手生产核心面初审（A 配额中断后）（2026-11-18）

- **环境**：worktree `.git/index` mmap 持续超时（无锁、index 存在），`git diff/status/ls-files` 受影响；对象库路径 `git show/diff <commit> <commit>` 可用，生产审查改读现行文件＋tree-to-tree diff。13.R2d 自测 ClosureCliTest 的 git ls-files 同因 error，须单独归因不得改判据绕过。
- **核心不变量初审成立（无新 P1）**：①durable settled/attemptOf 仅未座任务按身份点读 consume/reject/attempt 键，读失败保欠账、不写回内存（无第二真相）；②Reduce prior 捕获＋同身份全量重应用才清 unapplied，unappliedCutRefusal 全局按身份序具名；③run.go anchored 先 replayChainPastCut/RestoreProducts 再 NoteProduct，fallback 按 ref.seq 升序；④foldMapped 只认 newestOfChainLocked==evt.Key（链最新事实）；⑤commitFactNoCadence carried 只 reduce 不占新位，chainHoldsFact 走 GetEventInPartition 身份读；⑥sink 乙案拒收→retainNoticeOwed 否则 Consume。
- **披露/待裁（非新 P1）**：①fold-cold-root `journalReadHook` 包级 var 生产 nil、仅测试设以确定性注入源移动，holdSource 前后双 stamp 真实校验——接受为有界测试缝（B 已驳回 P1），R35 记录；②RegisterStateSlice 两生产调用点（host_delivery_checkpoint:152/governance_checkpoint:53）丢弃 disposer，拒绝卸载能力生产未接线（仅测试调用）——登记为可选能力可达性披露，非死代码判删；③compressSessions 4 方法（SessionOf/ProjectionOf/FileCompressProduct/HoldsCompressProduct）超 consumer-side 3 方法界，HoldsCompressProduct 为清减 answered 所需身份读——**须裁接口形状（有界例外 vs 拆分/重构），不先抬基线**。
- **覆盖边界（如实）**：完整候选生产面 104 文件 +8518/−2830（HEAD vs base）＋未提交；本轮初审覆盖恢复核心（durable/checkpoint/run/facts/compress_consumer/sink/fold-cold-root/RegisterStateSlice），未逐文件全审 approval/goal/ledger journal 等已核销叶面；不冒充 R35 全量放行。

### R296 13.R2d核销＋git index iCloud逐出修复（2026-11-18）

- **环境修复**：`.git/index` 为 iCloud dataless 占位（配额超限 brctl download 失败），HEAD 已可读；暂存面经 alt index 核为空⇒`git read-tree HEAD` 重建 index（非 commit，无暂存丢失），dataless 消除、status/diff 恢复。此前 13.R2d 用仓外 GIT_INDEX_FILE=/tmp/r2d-index（read-tree HEAD 生成）取文件表，判据零改动；现正常 index 复验。
- **13.R2d核销（编排者正常 index 亲跑）**：mutation 自测 **Ran 60 OK**（修前51/2fail+1error）、evidence 116 OK、DoD 51 OK。MUT-CAND-01.2 重指现行 sink（先 Consume→Backflow 拒收分支→nil），真跑两次 killed/named_assertion/counted_killed/assertion_hit、control green。race 豁免新判据：`_race_waived`（frames 全登记）为必要＋逐测试归属 `_failures_beside_the_race`（依 GOROOT testing.go race detected 行），普通断言失败＋waived race 必 red，纯 race 仍 green_race_waived；旧裸锚恰1 stale。负锚三例＋正锚纯race例齐。
- **未验证面**：全量 --all 未跑（归13.R4）；真实 waived race＋普通失败无生产样本，该半面仅构造证据。13.R2e（消费者闭合）续派。

### R297 compressSessions 接口形状裁定：隔离两窄口，不抬基线（2026-11-18）

- **裁定**：concept_budget baseline 是「只降不升」债务清单，加例外＝抬基线（禁）。compressSessions 4 方法（SessionOf/ProjectionOf/FileCompressProduct/HoldsCompressProduct）超 consumer-side ≤3。SessionOf 不可去（ObserveCommit 是 broader observer 契约只收 id，调用方 host_tools/facts/inspection 均传 id）。裁**接口隔离**：拆 compressSessionSource{SessionOf,ProjectionOf}＋compressProductStore{FileCompressProduct,HoldsCompressProduct}，各 2 方法，ageFoldSessions 实现两者，CompressionConsumer 持两窄口。理由：session 回放源与 product 账本是两 cohesive 角色，隔离后每口窄且语义零变化；非为单使用方造抽象（两能力均被消费者真实调用）。登记 03.R5 派 openspec-apply（与 13.R2e 写面不相交）。

### R298 03.R5核销：compressSessions 接口隔离清偿 concept_budget（2026-11-18）

- **03.R5核销（编排者亲验）**：`check_concept_budget.py --json` exit0/errors[]/iface-methods 15→14/baseline_stale0；`go build ./...`0；compressSessionSource(:80)/compressProductStore(:91) 各2方法、NewCompressionConsumer 双参(:127)；golden/runtime/architecture.json **零改动**（未抬基线/未加例外）。agent 跑03.R2六锚-race×10 exit0(66s)、支撑锚-race×2 exit0(138s)、assembly全量314.478s exit0、vet/gofmt绿。
- **级联（白名单外签名强制，已申报）**：生产构造点实在 compress_mount.go:23（叶行误记 run.go，run.go 零改动）；skeleton_assert.go 接口/构造断言；compress_checkpoint_test.go 替身 embed compressProductStore＋新构造。nil 守卫成对（全部调用点成对传 nil）。语义零变化（身份读/产物/投影窗不动）。
- **披露待派**：docs/api/…assembly.md:676 仍旧签名；gen_godoc.sh --check 改前即 exit1（多包 stale），docs/api 不在本叶白名单未重生成——归 godoc/文档叶。13.R2e 仍在跑。

### R299 12.R1核销：设计门交付齐备＋既有锚全绿（2026-11-18）

- **12.R1核销（编排者亲跑＋design 核）**：S4 `TestConsumerBehindTheChainSurvivesARestart` exit0、负锚 `TestConsumptionFailureIsReportedByName` exit0、消费者三锚＋跨会话两锚 exit0。design 交付齐：J1/J2（§25-28 含反事实）、E12-1..8 复跑（§九）、生命周期四态定义表（§十）、S4/S5 原始红证与当前验收（§十三）、G2-C1..C5（§八）、吞点语义单元枚举（§十二）。S5 已由12.R3实施拒卸载保原对象，旧「尚待实现」时点描述失效。两门落点已在 checkpoint_test.go/unsettled_test.go（不另建文件）。12域 R1/R2.1-5/R3 核销，余 R35 独立审。

### R300 13.R2e核销：证据消费者四缺陷闭合（撞限但实施已落）（2026-11-18）

- **13.R2e核销（编排者亲验）**：evidence 自测 OK、DoD **54/54**（+3）、mutation 60 OK、runner 40 OK。代码闭合：①seed/window 逐 argv 核承载（复用 runner.event_calibration_bearer 单一来源，非承载不得带 -args，承载必带 seed/window 对）；②check_mutation_gate_tool 现树校 entrypoint/digest（check_mutation_baseline:2879 调）；③status/verdict=killed/counted_killed=true/assertion_hit 一致；④DoD bind_mutation_evidence 调 check_mutation_evidence 全量结构＋source_binding（红控制/not_evidence_backed 拒计数）。
- **消费者锚（新鲜真跑）**：旧 r282 证据被现树绑定正确拒（树已变）；重跑 `--scope event-short --seed 1 --window 256 --repeat-index 1` verdict=match，checker ok=true/errors[]/29test·135anchor·9包·180事件。
- **待补**：agent 撞150轮于 design §十七 记录前（勾选已[x]），§十七 归 13.R3b 补。13.R3b 续派（runtime-plan 夹具＋P4 标题＋docs 对账）。

### R301 13.R3b核销：夹具/P4/文档对账＋comment_policy 复核仍0（2026-11-18）

- **13.R3b核销（编排者亲验）**：runtime-plan **44 OK**（+1，夹具铺 evidence/stand-in/mutations＋DOD_STUB 只判产物存在不复制判据，缺证据两态真跑红）；DoD 无证据 **9 gaps**（P4 标题 gap 1→0，P0-P8 registered/unproven 保留）；comment_policy 正式门＋产品零容忍门**均0**（R3b 战报称 lint 红于 comment_policy 系 stale，实测0）；doc-budget slack0/doc-links0/openspec strict valid。§十七 R2e 记录上一轮已落盘（+258），R3b 复验四自测数（126/54/60/40）＋run 产物 checker ok=true 后补复验行。
- **残留**：12 design.md:490 仍误名 delivery_fact_obligation_test.go（R3b 写面只 tasks 未含 design，待补）；proc-refs 红引外来未跟踪 no-prefix-criterion design.md（非本变更，禁触）；全量 lint 现仅红于 DoD P0-P8 无运行产物（需 mutation --all 真跑，归13.R4/09.R3）＋CI 未接（13.R3）。

### R302 12 design 勘误＋剩余叶清单＋08.R3 派（2026-11-18）

- 12 design.md:490 误名 delivery_fact_obligation_test.go 已勘误为 host_test.go:481。
- **剩余真实叶**（历史 6.x/2.x/1.4/4.5/4.3/4.4 多为被 R 叶取代的旧实施行，待 superseded 口径）：04.R0/R2；05.R1/R2；06.R3.a（待09.R2）；08.R3/R4/R1/R2；09.R2a/R2b/R3；10 清理（R9b/#2#5#6 等）；11.R1；13.R2(N6)/R3(CI)/R4(终验)；01.R1/07.R1/R2/12.R2/03 伞形父待子齐。
- **08.R3 已派**（退位删除核销，机制据报已删，验零残留＋可读性＋全树；历史6.x登记superseded）。全树验证可能长；并发禁 assembly/ledger 写单。

### R303 08.R3 删除核销＋fuse 全树红根因待修（2026-11-18）

- **08.R3 删除面已核（未勾）**：23 退位符号非测试全树0命中（coldDirName 仅 fold-cold-root 脚本不同义项保留；Retention.Retire/migrate.go/ErrJournalLegacyLayout 保留）；settleGate/ErrColdArchiveNotQuiet Go 0；测试文件整删＋两 flock 锚改名保留已随 0f84b2d1 落账（本单零.go写）；build/ledger85s/assembly332s/runtime/features/tool17包/注释门/fold-cold-root可读性三锚全绿。历史6.1-6.5 design§十三 superseded 登记。
- **全树消费者锚红（阻断勾选）**：tests/integration/execution 两 fuse——cap results=184≠200、storm results=403；`responseStormChain` 数 ReplayEntries 的 ThinkingPlan/ActionCommand。HEAD archive 同命令绿⇒候选引入。TestNativeConversationRoundTrip 独占绿＝并行抖动（11域R58既有）。**不归属外来、不除外**，登记 12.R2.6 根因修复叶（/tmp HEAD 叠文件二分，禁削弱断言）。
- **待裁小项**：assembly/symptom_anchor_longrun_test.go:492 陈旧「relocates」散文（06面小修）；08 specs/storage-lifecycle-completion/spec.md:5「无损搬迁」措辞改「压实复制」。

### R304 12.R2.6核销：fuse 回归根因＝foldMapped 顺序守卫误丢并发提交（2026-11-18）

- **根因（二分定位）**：assembly/facts.go foldMapped 的 `newestOfChainLocked!=evt.Key` 守卫把「非本链最新」当「重试既存」，但 200 结果事实并发提交，登记后折叠前同链可落地另一事实⇒合法提交被整条投影丢弃＋跳过 endedAt/rangeOpen⇒cap 漏折(184/191)、storm 边界停旧重驱(403/460)。HEAD 绿/现树红，候选引入（A 的 foldMapped 边界改动）。
- **修法（编排者亲验形状）**：删守卫与 newestOfChainLocked（O(历史)扫描）；增 typed 可选面 `repeatReducer{ReduceRepeat}`（同 reducer/emitter 先例，缝不读消费者名字）；StateRegistry.Reduce/ReduceRepeat 共用 fold(ctx,evt,repeat)，reduceInto 按 repeat 分派；提交侧 carried/retry 走 reduceCommittedRepeat→ReduceRepeat。projectionStateSlice.ReduceRepeat=nil（重复不占位，投影按身份幂等/已折叠材料不重述），其余 holder 正常折。无第二真相/无名字表。伪断言「重试即重复」注释改真话。
- **亲验**：两 fuse -count=1 exit0(9.3s)；重试锚 TestRetryOfFoldedOutputDoesNotReopenTheRound/TestRestartRetryOfFoldedOutputKeepsThePresentedRange＋12.R2.5 负锚 exit0；build0。agent 另跑 execution 全包45s/-race×5 58s、assembly 全量369s×2、runtime/state、acceptance、12.R2.5/05 负锚-race×10、comment_policy 两口径0、mutation60/evidence+DoD126 绿。变异两向复红（顺序守卫复原→cap191/storm460；ReduceRepeat 退化→重试锚红）。
- **级联申报**：runtime/state/checkpoint.go 扩 repeat 参数（任务行未列，缝唯一合法通道，已具名）；白名单外无其他。
- **新 lint 唯一红（待裁）**：test_check_runtime_symptoms.test_registry_matches_the_module——check_event_semantics_symptoms.SYMPTOM_PARENTS 少列 TestLocalSmokeSymptomBootDeployed；该脚本/自测属外来未跟踪禁触面，须裁归属（我方新锚依赖外来注册 vs 外来单补注册）。08.R3 全树消费者锚待 fix 后重跑核销。

### R305 08.R3核销：退位删除全树绿（2026-11-18）

- **08.R3核销（编排者亲验）**：tests/integration/execution -count=1 exit0(40s，两 fuse 具名 PASS)；comment_policy 正式门0；勾选[x]。agent 全树 tests/... 13/13 exit0（host205s）＋runtime/features/tool17包 exit0＋assembly319s×2＋ledger85s＋build0＋openspec strict valid。23 退位符号非测试0（coldDirName 仅 fold-cold-root 脚本不同义项）；relocat* 全树 Go0（散文已改）。
- **小修**：08 spec storage-lifecycle-completion:5+:18「无损搬迁」→「压实复制」（同 Requirement 两处的自洽，申报）；symptom_anchor_longrun_test.go:492 散文改现行语义（仅注释，numstat 1/1）；comment_policy 自造红（176runes）改153复绿。主 spec openspec/specs/...:62/71/event-segment-store:329 仍「无损搬迁」超白名单，归档 delta MODIFIED 覆盖。
- **08.R4 切面符号零残留**（placeIndexCut/cutOf/appendJournalIndexCut/writeJournalIndexCut/readJournalIndexCut/journalIndexCutVersion/journalIndexCutFileName assembly/memory/runtime 0命中）；fold-cold-root 脚本 08.R5 已落。08.R4 续派核销＋08.R1/R2 状态评估。

### R306 08.R4核销＋08.R2 真余项（lifecycle 三器生产0调用）（2026-11-18）

- **08.R4核销（编排者亲验）**：fold-cold-root/recover-kv 包级 exit0；切面7符号全树0（词边界排除 cutOffset 子串陷阱）；脚本三锚＋flock 负锚 PASS（干跑零写入/源变拒绝/半成品不报 ready/原字节原序）；升级说明 docs/OPERATION §F:246-250 随08.R5已落（doc-budget slack0 未净增）。agent 全树 tests/... 13/13 exit0＋ledger89s＋注释门0＋strict valid。勾选[x]。
- **08.R1 取代判定**：①②③④已取代（搬迁单位/孤儿原文/回退开关/冷层三态随退位归零，孤儿现行入口 recover-kv）；⑤部分取代（三方法/闸条款不在 delta，无损搬迁→压实复制已改）。design§十四登记。
- **08.R2 仍开（真余项）**：delta 硬要求 NewLifecycle/NewRetention/NewMemoryGuard「有生产调用方或被移除，MUST NOT 只被测试＋skeleton_assert 引用」；编排者 grep 生产0调用（仅定义＋skeleton_assert:125/:149＋测试）。**方向未定：接线到现行判据 vs 移除（含 skeleton/spec 硬要求/测试）**，派方向先验证单。Retention.Retire（generation retirement 不同义项）保留与否须按类型消费者枚举判，不预设。
- **门孤儿（待裁）**：check_runtime_plan.py 默认 --change-dir 指不存在的 runtime-rebuild，本变更缺 task-graph.json⇒该门未接本变更（白名单外）。归 13.R3/10 接线或退役裁定。

### R307 08.R2方向裁甲（移除 lifecycle 三器）＋级联授权（2026-11-18）

- **裁定甲（移除）**：08.R2 取证生产消费者全0（Guard/MemoryGuard/LifecycleStore/Capabilities 等0；Lifecycle 4命中是宿主 DiagnosticLifecycle 不同义项＋doc散文）；现行保护已有独立活载体——原文过期 RetentionWindows→newKVOriginalStore→ErrOriginalExpired→tool/recall/get.go:225；未结义务 pinnedFacts＋keepPinnedLocked(run.go:2292)→host_delivery 写入/落盘/结清。存储层 TTL/墓碑/压实销毁线上不存在且 delta spec:7 禁销毁已提交事实⇒接 Lifecycle 造明禁路径，接 Guard/Retention 与两载体重复＝第二真相。设计闸本就「端口依真实 consumer 收敛」，2026-10-18 至今未长消费者。dead_producers 门抓不到第三态（测试算消费者/skeleton 不算）。
- **条件（不追认验收面缩水）**：跨域两锚须迁/勘误不得静默删——①`protection holds outlive one holder`（original_reference_isolation_test.go，03.1/06 4.c0 引）若测义务保护→迁 pinnedFacts 活载体；②`TestRecoveryBeforeReclamation`（recovery_before_reclamation_test.go，03.5/runtime-rebuild O2-5 引）若测回收前恢复次序且回收已删→迁「已提交事实不销毁/恢复读原」活判据，无活等价则标 superseded 勘误到属域 design，保持历史原文。
- **授权5处级联落点**：examples/contract/memory_ledger/{Guard,LifecycleStore}_example.go 删（concept_budget 强制文件名＝接口）；product_surface_baseline.json:15/16 陈旧清（门仅 WARN）；docs/wiki/memory/memory-architecture.md:48/94/149/254 伪断言改真；他域测试迁/勘误；08 delta spec 硬要求重写（移除三器条款，不留死条款）。保留 RecallOutcome/RecallResult（reference.go 生产面）、generation retirement、migrate.go。
- 派 08.R2 接续实施单（甲案，禁重开方向）。

### R308 08.R2甲案核销＋08.R1 superseded：08域闭合（2026-11-18）

- **08.R2核销（编排者亲验）**：build0/ledger85s/concept0/comment0/31符号生产零残留（唯 Lifecycle＝宿主 DiagnosticLifecycle 不同义项）。整删 lifecycle/retention/guard_memory/lifecycle_config/guard.go（RecallOutcome/RecallResult 迁 reference.go）＋skeleton 四断言＋doc散文＋级联测试＋两 contract 例。Retention.Retire 随类型删（NewRetention 生产0且无他嵌入，构造链证断）。product_surface_baseline 37→35（收紧）；wiki 四行改真（slack0）；docs/api 重生成 gen_godoc --check0（闭 godoc stale）；08 delta REMOVED(TTL)+MODIFIED 四判据无死条款。agent 全树 tests/... 13/13＋assembly＋dead_producers0＋validate strict。
- **跨域锚判定**：锚一 protection holds outlive one holder 13断言全打 ProtectionLease 零触义务⇒superseded（单一属主 pin 取代），承接 TestPinnedDeliveryFacts/TestCrossSessionDelivery/TestPinnedFactsReseeded；锚二 TestRecoveryBeforeReclamation 6子测 G2.2/G2.6 迁活锚、G2.3 由 TestOfflineConversionPreservesEveryFact 承接、G2.1/G2.4/G2.5 无活等价具名登记（不追认缩水）。引用方冻结 archive 03.1/03.5/O2-5 历史未改写，勘误 design§十五。
- **08.R1 核销（superseded）**：①②③④随退位删除失去对象；⑤spec同向由08.R2 delta重写完成。勾选[x]。**08域 R1-R5 全闭合**（6.x历史 superseded 已登记）。
- **界外红（非候选）**：lint --only policy 唯一 test_check_runtime_symptoms registry（外来未跟踪禁触，提交树不含）。check_runtime_plan 默认 change-dir 孤儿仍待13.R3/10裁。

### R309 09.R2a量义冻结＋授权 run.go 只读阶段出口（2026-11-18）

- **09.R2a 停线（未勾，零.go写）**：量义/父子区间/独立开闭点已冻 design§九（boot/整代/提交/点读/目录维护/重绑定＋安全校验单列；未归因＝整代−Σ独占子区间，禁命名 rebind）。阻塞＝rebind 独立起止仅在 assembly/run.go（:1111/:1174 日志只有 elapsed 时长无起止，二次调 restoreCheckpointState 违「read once per boot」契约）；白名单内 journal 计时先写会成无消费者死代码（元规则二），须同批。
- **编排者授权（写面扩）**：run.go 加**只读**阶段出口（rebind 起止对＋getter，零恢复行为变更）＋journal.go directory/integrity 只读计时（照 LoadScan.Elapsed 先例）＋两锚同批。导出面最小化：先查 exported_interfaces/product_surface 棘轮余量，满则用包内访问或既有诊断面，不新造 fat API。目录维护＝地址表 grow/rebuild/resetTable（活载体）；重绑定＝装配期 restoreChains（非宿主 rebind）。
- **假绿识破**：新锚 -count=10 输出 no tests to run＝零匹配假绿，非通过（锚未建）。派接续单实施。

### R310 09.R2a核销：独立阶段采样落地（撞限但实施已落）（2026-11-18）

- **09.R2a核销（编排者亲验）**：两锚真建真跑（非零匹配）`TestBootStageSamplesHaveIndependentEndpoints`/`RejectDoubleChargeAndMissingSpan` -count=10 exit0(6s)；boot 成本锚 exit0；comment_policy0；build0。run.go restoreStage stageSpan 起止对(:1180)＋包内 getter restoreStageSpan(:1199)（未增导出面，棘轮不动）；journal 只读计时照 LoadScan.Elapsed 先例。六段独立开闭点＋安全校验单列＋unattributed 禁命名 rebind。勾选[x]。agent 撞150于 design 记录阶段，实施/锚已落。
- 09.R2b（墙钟校准＋性能反例）续派，前置 R2a 已满足。

### R311 09.R2b锚已落＋git iCloud 再逐出（环境 workaround）（2026-11-18）

- **09.R2b 部分（撞限）**：TestBootWallClockRejectsUnattributedGrowth 已建（longrun_test.go:642）-count=1 exit0；TestSymptomAnchorPerGenerationCostFlatOverALongRun -count=1 exit0(29s)。**未勾**：comment_policy 2 新 finding（两 Test 头注超160 runes）＋校准≥10轮表/门复跑待补。
- **环境 blocker**：.git/index/HEAD 被 iCloud 再逐出（dataless，配额超限 brctl 失败）；HEAD 已重建 ref:refs/heads/dev（rev-parse 0f84b2d1 绿），refs/heads/dev 可读；read-tree SIGBUS（objects pack 疑逐出），index.lock stale 已清。**验证 workaround**：GIT_INDEX_FILE=/tmp/r2d-index（仓外 HEAD 索引）供 git ls-files/comment_policy；GOFLAGS=-buildvcs=false 供 go build/test VCS stamping。**禁 git 写/commit**（对象面未修复前不可提交）。最终提交前须物化 .git/objects/pack＋重建 index。
- 派 09.R2b 接续：修两 Test 头注形状（≤160，仅注释）＋补校准表＋workaround 跑门，齐绿勾。

### R312 09.R2b核销：校准表＋性能反例＋未归因残项 frontier（2026-11-18）

- **09.R2b核销（编排者 workaround 亲验）**：comment_policy 两口径0；build0（-buildvcs=false）；longrun -count=1 exit0(29s)；wallclock -count=1 exit0。agent ≥10轮表落 design§九：工作阶段10/10界内（settling 峰值1.260 余量3% 标注亚毫秒噪声不放宽）；boot 配对控制 median0.939–1.096 界内；深度分离 held p10 2.196–3.874 > control p90 1.163–1.428⇒噪声/增长可分；integrity 线性7.811–18.757×单列允许。性能反例两类可红（计数不变阶段耗时增长10/10拒；全史枚举 overlay exit1 具名 slot table bytes，复原绿）。assembly 全量341s0；evidence126/mutation60/DoD54 绿。两 Test 头注10行拆短≤160（细节迁 design§九）。
- **公开 frontier（不隐去）**：`boot−integrity−rebind` 未归因残项 **1.624–3.366×（中位2.719）**，total **2/10 越界（1.337/1.357）**，成因残项非工作阶段；残项无名无主，**交 06/09.R3 归因，不记入已接受成本、不自动归噪声**。真实工作阶段回归反例本树不存在（阶段轴可失败性仅合成控制案证）；非空链折叠耗时承 R2a 未覆盖；race_check.sh 归 09.R3。
- **环境**：git objects pack 疑逐出，验证用 GIT_INDEX_FILE=/tmp/r2d-index＋GOFLAGS=-buildvcs=false；最终提交前须物化 pack/重建 index。09.R3 完整矩阵待产品冻结（10 清理/04/05/06 余）。

### R313 10.R5部分＋git 仓外索引稳定＋06.R3.a 并行（2026-11-18）

- **10.R5 部分（撞限）**：6 主 spec 已改（event-segment-store/memory-curation/production-wiring-fix/production-wiring/session-checkpoint-material/storage-lifecycle-completion）＋10 design(+239)/tasks；`openspec validate --strict` 绿。余 7 处冷引用辨义项：production-wiring-fix:5/compaction-scheduler:5/7/13＝superseded历史＋勘误（保留）；memory-curation:12 SummaryPlugin退位＝降级不同义项；task-skeleton-compression:25/31 回退位置＝fallback 子串。**不得过删**。接续单收口 production-wiring 自洽/五值回写/同步负例核定/ticket blocked。
- **环境稳定**：git 对象库可读（cat-file/log/tree 绿），SIGBUS 仅在写 .git/index（iCloud）；仓外索引 GIT_INDEX_FILE=/tmp/r2d-index（或 /tmp/r312-index read-tree exit0）下 status/diff 正常＝114 文件 +12399/−2964＋9 外来未跟踪。**最终提交可用 GIT_INDEX_FILE 完成**（add -u/commit 走仓外索引），无需强写 .git/index。go 验证恒加 GOFLAGS=-buildvcs=false。
- **06.R3.a 已派**（旧键前驱定位两段成本轴，assembly/read 面，与 10.R5 spec 不相交；前置 09.R2 计量已冻）。

### R314 10.R5三裁定：doc_budget只降/死租约改单pin/sync负例退役（2026-11-18）

- **裁定①doc_budget**：净−165（specs +132/−297）触发双向锁步红；授权 `python3 scripts/gates/check_doc_budget.py --tighten`（22149→21984，**只降锁步非放宽**，与 spec 改动同提交）。spec_hygiene stale 非错不阻断。
- **裁定②死租约要求**：event-segment-store:334-354「未确认恢复材料的有限保留租约」对象已死（memory/lifecycle.go absent、armGrace/Protected 全树0），且与「MUST NOT 并行保留租约账本」冲突。裁 **MODIFIED→现行单 pin**：恢复 owner 按身份 pin 未确认材料（pinnedFacts/host_delivery_checkpoint.go:253，单属主无引用计数），保 MUST NOT 销毁/fail-closed 能力缺失声明；删 armGrace/引用计数/共享内存守护集/墓碑清理等待/无损搬迁措辞；:378 spill 租约措辞同改。**不立新租约设计**（活载体已实证）。
- **裁定③sync负例退役**：test_check_openspec_sync 两拟新增负例无在仓被测实现（真实同步＝外部 openspec CLI 1.2.0 archive），对象不存在⇒勘误退役（非验收面缩水）。
- **裁定④ticket子项转R7**：architecture.json:1432/1437 迁锚后退役 compression-recovery-ticket，10.R5 其余可核销。REMOVED delta 约定：子域 specs 非 CLI 跟踪，主 spec 编辑＋design留痕即同步；archive 若需 delta 则补（核 --help）。
- 派 10.R5 第二接续实施四裁定。06.R3.a 并行在跑。

### R315 10.R5核销：四裁定落地＋event-lifecycle 留白待裁（2026-11-18）

- **10.R5核销（编排者亲验）**：validate strict0/check-openspec0（158passed/2 非门控 in-flight）/doc_budget0（tighten 22149→21979，只降具名 note）/spec_hygiene0（violations252/stale34非错）/concept0/doc_links0；event-segment-store 死措辞（armGrace/引用计数/守护集/无损搬迁/保留租约）0，单属主 pin/MUST NOT销毁/fail-closed 在位；勾选[x]。死租约:334 改单 pin（活载体 host.go:214 deliveryPins 值类型事实键无计数/host_delivery.go:682-697/run.go:2265/:2279/:2317 keepPinnedLocked/host_delivery_checkpoint.go:208-352），:378 spill 同改，场景6→5。
- **delta 约定核定**：openspec archive 1.2.0 无 delta 选项，子域 changes/10-* 非 CLI 跟踪，一级 specs/ 只 orchestration；直接编辑主 spec＋design§十六留痕即同步，不补无人消费假 delta（第二真相）。sync 负例退役（无在仓被测实现）；ticket 转 R7。
- **留白待裁→派10.R5b**：event-lifecycle 主 spec :59/:61/:66/:80/:85 仍围绕已退役 LifecycleManager/DefaultLifecycleConfig/checkTTL/墓碑/容量淘汰/保留租约交叉引用（R5 写面 design§二未点名）。须评估整能力 superseded vs 部分活（YAML 注入？），迁活承诺到 storage-lifecycle-completion/单pin，删死 SHALL。doc_budget 若净删再 --tighten（只降授权）。

### R316 10.R5b＋06.R3.a核销：event-lifecycle退役/前驱两段成本（2026-11-18）

- **10.R5b核销（亲验）**：event-lifecycle 整能力退役 superseded（LifecycleManager/NewLifecycle/DefaultLifecycleConfig/checkTTL/evictOldest/IsTombstone/MarkTombstone 等生产0，api/config 无 lifecycle 字段，yaml 无 global_ttl_days）；116→26行，Requirements 节死 SHALL/MUST 0（concept domain_surface.superseded_specs_with_requirements=0）；交叉引用改单 pin；doc_budget --tighten 21979→21889（只降具名）。validate/check-openspec/hygiene/concept/doc_links 全0。勾选[x]。
- **06.R3.a核销（亲验）**：两段成本锚（目标→前驱＋最新→目标）TestParentLookupCountsTheSearchBeforeTheTarget/KeepsSessionIsolation/RecallTraceThroughTierCountsOldTargetLookup -count=1 exit0；IndexWalkReads() 计数覆盖两段，GetParent 文档改两段真话（行为零改）；真实 recall_trace 消费者 oldNamed=398=3×(128+2)+8 与工具层 D=3 分立；assembly 全量332s0；正式 comment_policy0（agent 所称11 findings 系非正式口径，正式 POLICY_DIRS 0）。勾选[x]。回退体二次走查可消除未做（无第二使用方，overlay 探针未入树，design§二十九如实标）。
- **待裁小项→派两单**：①event-segment-store:338「TTL、容量与墓碑各自判据不因 pin 改变」TTL/容量已删⇒10.R5c 改写（墓碑按 journal 键删除记录义项核活否）；②MUT-DOD-P5-legacy-clause 锚 old="for age calculation." 打在已退役 event-lifecycle⇒not_applied，13.R2f 重指在位主 spec 要求（裁A保留门锚语义，不降级）。

### R317 10.R5c核销＋event-segment-store 余死条款→10.R5d（2026-11-18）

- **10.R5c核销（亲验）**：:338 等长改写（TTL/容量/墓碑三分句删→热窗窗口判据，pin 只摘命中项不改判据）；墓碑判死删（IsTombstone/MarkTombstone/TombstoneSet/tomb:/ReplayEvent/ErrEventForgotten 存储侧0；journal tombstone 是日志键删除记录活义项，不填空缺避第二真相）；活承诺8处在位；validate/check-openspec/hygiene(250不变)/concept/doc_links/doc_budget(21889等长未触发tighten)全0。勾选[x]。git stale index.lock 已清（HEAD 实 valid）。
- **余死条款→派10.R5d**：event-segment-store :77 live count 重建去重/排除墓碑/压实搬迁/容量淘汰、:80/:84-85 压实墓碑搬迁容量场景、:185 TTL年龄（Timestamp语义轴活）、:324 ReplayEvent窗口重入要求（ReplayEvent 零定义？）、:375 容量提示。逐条取证活否→改写/superseded，保活（Timestamp语义轴/sealed降级若活/容量观察若活）。

### R318 13.R2f核销：MUT-DOD-P5 重指在位禁入条款（2026-11-18）

- **13.R2f核销（亲验）**：P5 old 由退役 event-lifecycle「for age calculation.」重指 storage-lifecycle-completion:13「保留保护与回收协调」首句（在位恰1命中），注入 ttl_scanner.go legacy 条款→check_spec_hygiene R4 go-file 具名拒（violations250→251/errors1）；真跑 status/verdict=killed、assertion_hit=true、control_tree_green=true、not_evidence_backed=[]；mutation 自测60 OK/stale=[]；DoD 带 P5 产物 gaps9→8（无证据9为预期）。dod_predicates P5 块零改动（只登 measure/scope/anchor.gate/mutation.id，不指文件标题）。勾选[x]。
- 10.R5d（event-segment-store 余死条款）在跑。

### R319 10.R5d核销＋vocabulary carrier红＋sealed/剪枝留白（2026-11-18）

- **10.R5d核销（亲验）**：六项留删（eventCount 正文/场景改活删死、TTL年龄删保Timestamp轴、ReplayEvent整条删＋退役注、容量提示删保活句）；死措辞取证三族0（墓碑/段压实/淘汰/scanner），同名异义排除（memory compaction＝投影折叠、consolidation_hint stamp无生产者，不填空缺）；validate/check-openspec/hygiene(requirements764/violations250)/concept/doc_links/doc_budget(21889→21878 tighten只降)全0；活承诺8在位。勾选[x]。
- **新红→R7**：check_vocabulary_alignment 4 carrier 违例（goal.go:110/181/277 注释＋docs/api:265）——07.R2.2 注释用 carrier 触词汇门，改措辞＋godoc 重生。
- **留白待裁→另叶**：event-segment-store :18-26 sealed/Layer 场景死（window 现行毫秒非小时桶，粒度设计待裁）；:182 剪枝封口段边界族（storedWindowMeta.MinTime/MaxTime 只写不读、FileSegmentStore 无 QueryEvents）活载体待重新取证。DoD P0-P8 无 run 记录属 13 账（非本叶）。

### R320 10.R7核销：门golden/carrier/ticket/N14/N15/N10（2026-11-18）

- **10.R7核销（亲验＋接续）**：carrier 违例0（goal.go:110/181/277 注释改＋godoc 重生）；ticket 迁锚 architecture.json compression-recovery-ticket 0命中（→session-checkpoint-material）；N14 NewDurableCommit 删＋测试锚迁现行 Seat（runtime/state -count=1 绿）；N15 整族枚举残留0；N10 认两形（runtime_checklib CHECKBOX_RE `\*{0,2}`＋test_a_bold_leaf_checkbox_is_counted_too，负锚漏数即红；现树已裸78叶/粗体0，题面分布失效）；cold_archive_test.go 对象已随0f84b2d1整删勘误；13两假绿指针13.R2d/e。plan 自测45 OK；window_bounds/schema_allowlist/concept/vocabulary/comment两口径/doc_budget/mutation60/evidence126/DoD54 全0。勾选[x]。
- **lint 全绿边界（非本叶）**：lint.sh exit1 红在外来未跟踪 test_check_runtime_symptoms registry（禁触）＋DoD P0-P8 无 run 记录（13账）；本叶面齐绿不宣称 lint 全绿。comment_policy/main.go:171 目录称「Go file」警告（knowledge_base git-ignored）超写面未改，登记。N10 编号族未扩（plan 门无 task-graph skip，R306 孤儿）；日后建图须先裁编号族同批扩。

### R321 10.R8核销：三方法终态＋alt GIT_DIR 稳定（2026-11-18）

- **10.R8核销（亲验）**：durable.FactLog 生产43处/9文件（state/governance/approval/assembly），memory/ledger 结构满足不 import；旧名 GovernanceEventLog/AttentionFactLog/state.FactLog 全仓0，无别名，无第三同族端口；数量重计 exported_interfaces79/new57/removed32/bindings57（棘轮5/57/85未抬未降，design§十二·9 取代过期83/54）；build0/runtime/state+approval+governance0/消费者锚×10/assembly329s0/concept/architecture/vocabulary/comment/godoc --check 全0；变异第四方法→S1拒、旧端口声明→S4+S1+D1拒（仅旧绑定条 warn 如实记）。TierFacts.Get 保留（活消费者 commit.go:231 addressedChainLog，删则退整前缀扫描），memoryFactLog.Get 已删。勾选[x]。
- **环境**：.git/HEAD 再逐出（read 恒0）；alt `GIT_DIR=/tmp/ri-gitdir`（软链 objects/refs＋HEAD 副本）＋GIT_INDEX_FILE=/tmp/r2d-index 稳定 rev-parse 0f84b2d1。最终提交用此 alt。check_runtime_plan 因无 task-graph.json exit1（门孤儿 R306）。
- 派 10.R9b（replay 旧生产者裁删，承 5.1.2/5.3.1-4）。

### R322 10.R9b核销：replay裁删面齐绿，smoke转09.R3复验（2026-11-18）

- **10.R9b核销（裁删面，编排者亲验＋接续）**：RebuildProjectionFromWAL/RebuildOptions/RebuildReport 全仓0；memory/replay0、host 两锚×10 0、history TestEventIntegrityProjection-race×10 0、examples/contract0（编译锚）、build/vet0、comment两口径0、concept0、dead_producers0（3453，删前3505）。负测 M1 ageKey掺nonce→TestRebuildRefoldsIdempotent/TestPlanAndApplyAnswerTheFold红复原绿；M2 丢session→TestParentLookupKeepsSessionIsolation红复原绿；**M2b 仅删分区守卫全绿⇒分区轴无法独立证伪（PartitionIDFromName 从会话名派生，如实登记不计已验）**。KEEP六项在位，TestRebuildRefoldsIdempotent判据未删。fold_test.go:1 契约锚 #families→#reduction-criteria（纯注释修 fragmentation）。
- **裁定 smoke 转交**：wechat smoke exit1 8红＝no-prefix叶/宿主文档叶/ledger guard-lifecycle已删叶/用户.wechat-config/data禁触/LongRun缺-race/fold综述确定性红（归 run.go+248 重写或模块既有，待09.R3归因）；本叶 diff 只删0生产调用者符号＋注释、age_plan/segments未改⇒非本叶引入。**smoke 属 examples 共享门，转 09.R3 矩阵统一复验（补 -race＋独立数据目录＋他叶闭合），非本叶验收面缩水**。全测唯一红 TestExecFaceWiredFromDocument 并发 tmux 竞态（单跑/×3绿）非本叶。
- **非代码残留转他叶**：event-sourced-projection spec:49/156/166（R5）、docs/api memory__replay 7处（R6）、check_concept_budget.py:183 RebuildReport stale（R7）。勾选[x]。

### R323 10.R6核销＋fold_test.go iCloud逐出恢复（2026-11-18）

- **10.R6核销（接续）**：升级说明五项俱在零补写（OPERATION §B/§F.1-5 对锚 atomic_store/recover-kv/fold-cold-root/磁盘增长）；openspec/wiki 叙述核真（TierFacts.Scan/RecallPort.ReadSegmentScoped/RetentionWindows/summary_budget_bytes，AgeTextLimit 0）；docs/api memory__replay 重生614行 Rebuild0；doc_links/hygiene(250/stale0)/concept/vocabulary/validate 全0。doc_budget tracked面=231篇/21876行=基线（不tighten不上调），裸跑+12篇/+10561行全属外来未跟踪 interview-helper（.gitignore:88 白名单优先，不可删不可排除）。gen_godoc --check 红因 fold_test.go 逐出（镜像 /tmp/r6view 43/43包0diff）。勾选[x]。
- **环境 blocker**：memory/replay/fold_test.go dataless（2063B，iCloud 配额余1857B不足以下载），replay 测试 FAIL；skeleton_assert.go gofmt 已修（gofmt -w）。**全仓仅此1个 dataless .go**。派恢复单：从 HEAD 对象（6318B）＋R9b design§二十一裁删/迁锚清单重建工作树版（锚 #reduction-criteria），验 replay 绿。
- **上报**：doc_budget 外来12篇不可删（白名单）；archive .openspec.yaml 假报M（git diff 实空，iCloud anomaly）。

### R324 fold_test.go字节级等价复原＋10域余R1-R4（2026-11-18）

- **恢复核销（亲验）**：fold_test.go 由 HEAD blob(6318B/146行)减 R9b 冻结删除项重建＝2063B/45行，与 dataless stat 及 R9b 记录行号逐项吻合（TestFoldDepthMeasurementExists :25）；退役 TestFoldIsIdempotentOverTheSameMaterial＋refKeys（Fold 整删），活判据继承者 age_segments_test:522/replay_budget_test:329/:389 实测 PASS，不重造锚。replay -count=1/×3 绿、gen_godoc --check 82包0diff、build0、comment两口径0、负测 TestFoldDepthMeasurementExists 可红复原 shasum 一致。10.R6 环境 blocker 解除。
- **10域余 R1/R2/R4/R3**：R1 P0-P8双向对账、R2 旧项处置表、R4 实施清理＋spec合并（R5-R9 已做大部分）、R3 交接归档（需13.R2/R3＋04.R0）。派 R1/R2/R4 对账核销单。

### R325 10.R1/R2/R4核销：对账/处置/合并表＋终验门待闭（2026-11-18）

- **10.R1/R2/R4核销（亲验勾选＋门绿）**：R1 P0-P8 双向对账表 design§23（修4处旧断言：9变异全enabled old-count1/P1锚改名/P4 ci_gate=test measure退役/P5P6 gate+selftest 由 lint 执行）；冲突照录不退役（9变异 mutation_verdicts[] 全未证归13、P2无单一承载叶、P5判据面4篇spec留死SHALL）。R2 处置表§24 29行全覆盖（接线行0＝骨架已删 lifecycle−468/retention−184/guard−96 生产0，KEEP同族>0，config-surface4 blocked不计完成，未达4项留账）。R4 合并§25：13域21 delta 标题0重名并集无覆盖；3处 delta 目标缺失（checkpoint-state×2 实标题在 session-checkpoint-material:36/:77、08 REMOVED 主specs零命中）拒合并登记归属05/07/08——子域已有取代 banner 留历史证据（子域非CLI跟踪，archive只一级 specs/orchestration）。hygiene/concept/durable_truth/validate 全0。
- **终验门待闭**：10.R3 需 check_dod_coverage exit0（现9 gaps＝P0-P8 无 mutation run 记录，归13.R4 全量跑）＋13.R3 CI＋04.R0。09.R3 完整矩阵（十短/六长/mutations）待产品冻结。P5 死 SHALL 4篇 spec（tombstone-partition-isolation/architecture-review-standards/error-handling-consistency/rustviking-config，event-lifecycle 已 superseded）→派10.R5e。doc_budget 本地复跑需 git 索引重建（iCloud 环境，alt GIT_DIR 下 git ls-tree 仅387条虚高）。

### R326 10.R5e核销：P5四篇死spec清理＋终验相启动（2026-11-18）

- **10.R5e核销（亲验）**：tombstone-partition-isolation 整能力退役（墓碑族六符号＋tomb:前缀全0，35→26）；architecture-review-standards 部分活（25点名符号0，活义务⑦并发安全＋race_check 承载，208→23，不得 superseded 否则活SHALL红）；error-handling-consistency 部分活（trpc log34活/GetParent35活，第三条整退役，51→37）；rustviking-config 载体正名（MemoryConfig/resolveMemoryStore0→config.go:53-55/:254-263 ErrRustVikingMissing，47→52）。concept superseded_specs_with_requirements0/domain-surface0；hygiene requirements752/violations242/stale8/errors0；doc_budget --tighten 21876→21673；validate/doc_links/vocabulary0。勾选[x]。
- **余 prose 漂移（非门红，低优先）**：tool-security-alignment/concurrency-hardening 死符号篇、causal-chain-isolation 以 MemoryPlugin/RelationStore 表述现行 SHALL——concept/vocabulary 未红，登记后续清理。10域余 R3（需 DoD exit0＋13.R3＋04.R0）。
- **终验相启动**：派 13.R4a mutation 全量（--all 生成 P0-P8 run 记录→DoD exit0）；09.R3 完整矩阵（十短/六长）另派。

### R327 13.R4a核销：DoD 9/9 proven＋P4 load-flaky待校准（2026-11-18）

- **13.R4a核销（亲验）**：mutation --all EXIT0/control_tree_green（19控制）/23条全 killed（DOD-P0..P8 九条＋EVID/G1/CAND/FOLD）/not_evidence_backed[]；`check_dod_coverage --mutation-evidence /tmp/r4a-mut2` **EXIT0 9/9 criteria bound and proven**（R3a 前9gap全闭）；负锚红控制产物→11gap、无产物→9gap。bundle（新鲜短跑＋mutations）绑定类0错误（4错误全矩阵完整度 short1/10 long0/6 归09.R3）；单份短跑 ok=true 29test/135anchor/9包/180行。自测 mutation60/DoD54/evidence126 绿。勾选[x]，13.R4 终验（六长/F3/全链）不勾。
- **P4 load-flaky 待裁**：TestSymptomAnchorBootCostFlatAcrossGenerations bound.factor=2 打在18-40ms量级，高负载机可复现红（首跑第5代40.74ms越37.74ms界⇒0变异执行26判词全废），空闲单跑×3全绿⇒加载态抖动非树回归。改造需动产品测试＋P4谓词（越13.R4a写面）→归09.R2b/09.R3校准（界由同条件校准确定，不沿用未校准2×）。
- 派 09.R3 完整矩阵（十短×10/六长×6/mutations，bundle 整包绿）。

### R328 09.R3部分：十短10/10六长4/6＋(42,256)产品崩溃一致性frontier（2026-11-18）

- **09.R3 部分（未勾）**：十短10/10 verdict=match（I=1冷编译tmux抓帧竞态，隔离×3绿，暖缓存重跑绿，红产物单列不入包）；六长4/6绿（(1,256)268s/(1,32768)241s/(7,32768)278s/(42,32768)306s，inputs1200/restarts120/121代/durable_scan attested）；(7,256)并发红单跑绿（非并发因）；**(42,256)2红0绿单跑仍红**。mutations 复用 r4a-mut2 绑定0错；bundle 16份仅2错＝完整度（long5/6缺(42,256)），绑定/摘要/run-id唯一性0错；DoD 同份证据9/9绿；负锚三份（旧树43错/根模块漂移18错/工作量缩水8错）具名拒。
- **产品 frontier（待裁修复）**：(42,256) gen26/80 `memory: journal frame is corrupt: …/storage/kv/journal.log: record carries no object`（journal.go:271 经 journalReplay:567-586）＝崩溃一致性问题非墙钟非噪声（2/2）；另 gen80 `did not appear within 10m0s`＋close `1 unsettled item(s)`＝活锁/预算耗尽（不同因）。推断（未证）：零字节尾被 journalReplay 当零长合法帧（空 payload CRC32=0 与零 CRC 相等）⇒torn 判据漏项；取证需保留子进程 journal.log（t.TempDir 清理）。
- lint 全链红在外来 symptoms registry（TestLocalSmokeSymptomBootDeployed 名单差），DoD 段未达；preflight 未跑（event-long 必撞同红）。CI gh 另需授权。派诊断修复单（保留 journal 取证→根因→最小修→(42,256)长绿）。

### R329 (42,256)根因实测：torn判据漏长度0帧头（CRC32C=0歧义）＋修法落地（2026-11-18）

- **根因（实测非推断，09 design§十三）**：journalReplay torn 判据漏「长度0帧头」——空 payload CRC32C=0 与零填充区 CRC 字段相等，全零帧头被当零长合法帧：完整性趟（visit 空操作）静默吃整段零填充、complete==size 故 openJournalLogFace 不判 torn 不截断；跨距重放（replayWindow/derive/rebuildFrom）拿空 payload 在 journalSpansAt:271 报「record carries no object」。**同一日志两读者对同段字节判定不一致**（完整性说合法/重放说损坏）。(42,256)/(7,256) 同因。
- **修法（最小，已落）**：journalReplay length==0&&crc==0 不再当帧——零填充后读到日志尾全0⇒torn 尾交回 offset（写权视图按既有逻辑截断）；零填充后仍有非零字节⇒具名 ErrJournalCorrupt（frame at offset %d carries no payload），不静默吃不越权截断。tornTail/torn 字段区分「全零尾」与「首字节即 torn」。被否：①journalSpansAt 拒空 payload（把可自愈 torn 变永久 boot 失败且不解决两趟分歧）②任何长度0当 torn 截断（删「零填充后仍有帧」真损坏拒绝面＝放宽凑绿）。
- **编排者亲验**：build0、memory/ledger -count=1 绿（90.8s）。journal.go +185/segment_store.go +22（含前序累计）。派接续：(42,256)长×3/(7,256)/负锚可红/bundle 整包6长→09.R3 复勾。

### R330 接续子代理失败＋(42,256)红因改判：非零填充尾（2026-11-19）

- **接续子代理失败**（输出文件消失、无完成通知），但死前已落诚实停线记录（09 tasks 09.R3a:51-55）：复跑保留根 `/tmp/ri-keep-42-256` 的 journal.log 逐帧核＝12690帧 CRC全对、零长帧0、≥8B零游程0、尾0字节⇒**本次红无零填充尾**，「kill后延迟分配零填充」仍只是机理推断（缺带零尾真实崩溃取证）。torn 判据修复独立成立（构造负锚可红：8B与4096B两形态旧文exit1原文=产品corrupt；修后exit0；ledger 83.8s/build0绿），**但不得以它冒充(42,256)转绿**——该修复是真实崩溃一致性缺陷修复，(42,256)红另有其因。
- **(42,256)真症状（新frontier）**：`input identity does not reconcile: missing=[SMOKE-R420560..R420596]`（37/1200恰gen56整代输入；gen56.json记drive与应答皆成功而gen56.log仅144行无输入标记）＋`unsettled: reason=the task stands at accepted`＋收尾`context deadline exceeded`。链路＝smoke_harness_test.go:744-760对账口径与agent侧任务链义务结清，超09白名单；recovered_tasks=38/restore chains=39无journal错。**活锁与损坏帧因果已否定分离，新定因方向**：①测试基建tmux抓帧竞态（与十短I=1冷编译红同族：日志丢标记而json记成功）vs ②产品义务链卡死（task停accepted致close超时）。派09.R3b方向先判定因（保留根+gen56.json/log+checkpoint在读）。
- 09.R3a保持未勾（torn修复已验但(42,256)×3绿未取）；09.R3仍4/6。

### R331 (42,256)红因定性：乙为主（重启代输入未驱动/收尾活锁）＋夹具entry-b盲点次级（2026-11-19）

- **编排者亲核（子代理两次启动失败后接手）**：对两组运行 dump 的 37 条缺失标记（SMOKE-R420560..596）按模型分布计数——**fail 运行**（801 请求）：37 条在 smoke-entry 与 smoke-entry-b **均零出现**＝真实未驱动（对账报缺失正确）；**retry 运行**（2433 请求）：37 条**全部 only-under `smoke-entry-b`**（reload 模型）＝输入已送达但夹具 `smokeCalibrationReconcile` 只计 `smokeEntryModel="smoke-entry"`（smoke_harness_test.go:54/:737-742）⇒次级盲点；retry 死于 gen80 收尾（1 unsettled `task stands at accepted`＋`context deadline exceeded`），未走到末尾对账（reconcile 在 :1094 末端，故盲点未显）。
- **方向裁定**：主＝乙（产品侧）：restart_point 代（gen56）输入驱动丢失（fail）或驱动路径漂移到 reload 模型（retry）＋gen80 收尾活锁；次＝甲（夹具）：reconcile 模型过滤漏 smokeReloadModel，须同批修（仍恰一次语义：跨 entry/entry-b 合并计数防 duplicate 假红）。09.R3a 停线记录「gen56.log 144 行无标记」与 retry 现物不符（225KB 有标记）——该记录疑为 fail 运行时点，勘误以本条为准。
- **待做（09.R3b 重派，带本证据）**：①产品侧根因——gen56 restart 后整代输入为何未驱动/漂到 reload 模型：查重启恢复→seat→drive 链（12/05 域面）；gen80 活锁＝task accepted 无进展致 close 超时；②夹具 reconcile 跨 entry/entry-b 合并恰一次；③(42,256)×3 绿后 bundle 补齐。子代理基础设施两次启动即失败（无输出文件零落盘），若再失败编排者继续亲接。

### R332 09.R3b修复落地：脏run dir陈旧报告＋reconcile模型盲点（2026-11-19）

- **根因链（agent撞限但已落，编排者复核完整）**：①`smokeCalibrationSpawn` 在 `cmd.Start()` 前**未删**复用 run dir 里上一轮同名代的 `reports/genNN.json`＋`.ready`——父侧就绪等待（`smokeCalibrationAwaitLiveness`/`AwaitFile` 只判 `os.Stat` 成功不判新鲜度）立即命中陈旧产物、读完陈旧报告就 **Kill 刚起的一代**⇒整代输入真实未驱动（fail 运行 37/1200 机理坐实，判据「陈旧产物不得被父侧当成这一代自报」）；②`smokeCalibrationReconcile:740` 只计 `smokeEntryModel` 排除 `smokeReloadModel`⇒reload 窗口输入计 0（R331 次级盲点）。
- **修（两处＋两枚新锚）**：spawn 启动前清 `reportPath`＋`reportPath+".ready"`（注释写明机理，:3427-3434）；reconcile 计数改 `req.Model != smokeEntryModel && req.Model != smokeReloadModel { continue }`（恰一次语义不放宽：跨两模型合计判定，`smokeLongReconcile`/smoke_test.go:3643 恰一次断言零改）。新锚 `TestSmokeCalibrationReconcileAcrossReloadModels`(:808)＋`TestSmokeCalibrationSpawnClearsStaleReport`(:3464)。
- **编排者亲验**：build0/gofmt 空/vet0；两锚 `-tags smoke -count=10` exit0（真匹配无 no-tests 警告）；agent 撞限于红证复原后（树完整非半改）。**消费者锚由编排者直接后台跑**（子代理两次启动失败后不再依赖）：(42,256)×3＋(7,256)＋十短 I=1（b72bmrkic），跑完更新。09.R3b 待长绿勾选；产品域零改（纯夹具面）。
- **勘误**：09.R3a 停线记录「gen56.log 144 行无标记」为 fail 运行时点读数，与 retry 现物（225KB 有标记）不符——R331 已勘误。

### R333 09.R3整包绿＋09.R3b核销：冻结树十短×10＋六长×6＋mutations 全绿（2026-11-19）

- **完整矩阵（冻结树，编排者直接跑）**：十短 I=1..10 全 exit0 verdict=match；六长 (1,256)/(1,32768)/(7,256)/(7,32768)/(42,256)/(42,32768) 全 exit0；mutations --all exit0（23 killed）。bundle＝/tmp/r333-bundle（17份：short10/long6/mutations1），`check_runtime_evidence.py --bundle` **EXIT0 ok=true errors=[]**（绑定/完整度/闭包全绿）；DoD 同份 mutations **EXIT0 9/9 proven**。
- **09.R3b 核销**：spawn 清陈旧 genNN.json/ready（脏 run dir 陈旧报告骗过 os.Stat 就绪等待⇒父 Kill 刚起一代）＋reconcile 含 smokeReloadModel（恰一次跨模型合并不放宽）。(42,256) 修复后热态×3 连续绿（首红 86s「consumption loop ended before it answered the segment」＝冷启动竞态族，非 gen56/80 症状）；两新锚 -tags smoke ×10 exit0。产品域零改。
- **09.R3 核销**：09.R2a 阶段采样＋R2b 校准＋R3 整包三叶全闭合。CI 记录 gh run view 另需授权未执行；soak 档未启用如实列。
- **zsh 分词陷阱**：`set -- $var` 致六长首跑 exit2 一个没跑（输出名带空格）——显式逐条 argv 重跑后全绿。
- 09 域 R1/R2a/R2b/R3/R3a/R3b/R4 全闭合。

### R334 13.R3核销：CI接线＋归档拒绝入口＋六反例（2026-11-19）

- **13.R3核销（亲验）**：archive-preflight.sh 对 /tmp/r333-bundle EXIT0 三段全绿（evidence binding/DoD 9/9/spec hygiene）；CI `ci.yml:132 TAGENT_RUNTIME_EVIDENCE_DIR` 接线（needs event-integrity-mutations/if always()/下载步→mutations/）；六反例＋绕行全 exit1 具名拒（零测试/跳过/过期 digest/错提交 9547fac/手填 counted_killed=false/缺 (42,32768) 分片/绕行 ec03877 74 归档路径）；lint.sh DoD 段四态（合法0/未设1/无目录1/手填1）逐字单跑。勾选[x]。
- **如实边界**：分支保护 required checks **ABSENT**（外部须授权，不伪装已拦截）；CI 未真跑（YAML 解析＋本地同语义验证）；lint 全链红在外来 symptoms registry＋comment_policy 前置段（DoD 单独证明绿）。docs/OPERATION.md §5.1 ci.yml:109 描述过期登记待改。
- **13域状态**：R1/R2/R2a-f/R3/R3a/R3b/R4a 全闭合，余 R4 终验（F3/正式全链/CI 记录）。

### R269 D撞预算中断＋A级联锚漂移（2026-11-18）

- **D(13.R2a+R2b)撞150轮上限**（非实现失败）。亲验进度：runner自测39全绿(R2a闭包指纹完成)、evidence自测116全绿(R2b现树比对完成)；mutations自测51中3失败+2错误。golden绑定字段与13域docs尚未改。
- **mutations红根因＝A级联**：`MUT-EVID-04-position-boundary`补丁锚在assembly/facts.go，A改facts.go(persistFact签名)致锚0命中→ClosureCliTest.setUpClass控制树构建失败→级联MutationGateTest等红。属跨面锚漂移，非纯D缺陷。另AssertionAttributionTest/MutationBaselineTest或含D未尽自测逻辑，待A收口后重跑分辨。
- **A级联待处**：A改persistFact为(bool,error)但facts_test.go不在A白名单、尚未同步→assembly测试包编译不一致（B复跑因此暂挂）。A收口后由编排者核facts_test.go同步与MUT-EVID-04锚重指（或判该变异过时），再派D接续。
- **排队**：D接续单（openspec-apply）待A收口后派——只装剩余（mutations自测收口＋锚重指＋golden绑定字段＋13域docs＋真实短runner→checker验），禁重开R2a/R2b设计。B锚复跑同待A收口包可编译。
- **A收口亲验（更正上条「半落」猜测）**：A实为收尾记录阶段撞预算，实现已完整落地——facts.go加`chainHoldsFact`＋commitFactNoCadence对已持事实只reduce不占新位、foldMapped边界改「只认链最新事实」（R2.2，不动replay Append）；run.go修R2.1总序。persistFact的(bool,error)实验已被A自行回退为单error，调用方一致，build=0/vet绿（B所见编译破坏是中途实验态已自愈）。A+B九锚`-race -count=10`全绿。A未勾12.R2.1/R2.2域checkbox、未落执行记录（死于记录前）。全量assembly回归后台跑（b7v4s027r）确认后由编排者核销。D mutations仍5红＝MUT-EVID-04锚被A净改动真实漂移（非瞬态），待重指。修复叶05.R4+12.R2.3与D接续待全量回归收口后派（避免写单污染验证run）。

### R266 用户裁定整体落账并续派（2026-11-18）

- **用户原话**：「先不拆分直接commit然后继续分派子代理推进」。据此整体候选一笔commit（含全部已跟踪改动与本变更HISTORY文件），不push；外来未跟踪文件零触碰。
- **提交前核查**：无二进制夹带（staged/unstaged numstat无`-`行）；钩子为透传无本地pre-commit实体；url-fetcher/TESTING.md与trpc-fork评估两份文档差异属本变更清理。代码验证基线＝最后一次全量assembly仅S5红、state -race×10绿、replay绿、build/vet绿；此后仅.md改动，未重跑（用户要求直接提交）。
- **提交后派发（写集不相交）**：A＝12.R2.1+R2.2（恢复总序＋折叠重试，关键路径）；B＝03.R4（宿主预算入口）；C＝04.R3（重复通知计数）；D＝13.R2a+R2b（证据闭包指纹＋现树比对）；E＝12.R2.3/05.R4只读诊断（重复投递根因）。A/B同assembly包不同文件，瞬态编译红按归属甄别不得互改。
- **R267 派发方式裁定**：用户「暂停，编码任务派发openspec apply子代理完成」。已停A/B/C/D四个general-purpose编码单（停时均在读取阶段，工作区0跟踪改动，无半成品），改派openspec-apply专用子代理执行同批叶；E只读诊断非编码，保留。openspec-apply单只勾域内叶checkbox＋追加域执行记录，禁编辑一级编排文件/禁archive/禁勾F。
- 本轮commit是进度落账：S5、恢复总序、折叠重试、重复投递、成本归因、证据绑定仍未闭合，R35放行与F4归档不因此解除。

### R265 复盘后的计划校正（2026-11-18）

- 保留十三域及既有成果，不新建另一套实施变更。行为、长期成本、真实入口及完成证据的缺口分别归回原领域。
- 原5178行编排和117行顶层任务逐字节保全至活动变更内历史文件；原始哈希见上。当前入口与历史证据分离，不宣称变更已归档。
- 纠正R264过度完成声明、无效注释门归因、原S1与新裁定冲突；最终完整回归、实际发布保护与独立放行均未通过。
- 本轮仅调整计划。OpenSpec结构校验通过；历史哈希与当前入口链接校验通过。两路独立计划审查发现六项阻断（旧测试落点、尾集三态、五类提交结果、正式入口未具名、01前置过宽、10四方法旧要求），修正后定点独立复审六项范围frontier空；不代表生产代码或全规格已验。分批评估未发现已获完整放行的候选，保留全部代码与index，未commit、未完成归档。
