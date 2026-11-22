# B 历史读取—派生索引 · 设计

## D1 目标与证据边界

共同合同见 [顶层 design](../../design.md)；本文件负责 B 的角色比较与实施签发，叶命令只在 [tasks](tasks.md) 维护。
根目录为 `/Users/pengweiye/Documents/codes/tagent`；以下源码路径均相对该根。当前只有读码/Grep 证据，无本轮运行结论。
保留事实身份、原子提交、耐久屏障、提交顺序、只读视图、原文授权与可读性；历史物理增长不替代完整成本目标。

| 断言 | 当前证据 | 处置与主叶 |
|---|---|---|
| 低负载保证探测不足 128 | `memory/ledger/journal.go:1488–1578` 超界报损坏；129 同桶键为算术反例，未走存储 | 06.101 通过真实提交/重开/缺失读验证合法聚集键，不先选哈希方案 |
| GetParent 已消除历史成本 | `assembly/read.go:274–377` 先从最新找目标，再找前驱；fallback 再走读链 | 06.101 固定目标与前驱，独立增加目标之后历史 |
| scoped 首读仅付目标成本 | `memory/ledger/reference.go:276` 经 ensureLoaded；`memory/ledger/kv_original_store.go:142` 读 blob/ref 全族 | 06.101 区分裸键、scope、热命中与首次读，检查全部入口 |
| anchor 正文因 ID 含 text 而不可寻址 | `assembly/compress_consumer.go:378` 的理由被 `api/event/compress_product.go:75` 推翻：哈希输入为 request、producer、key | 06.104 先查所有生产者、直接构造与恢复来源，不直接删 Content |
| meta 只有自写可删除 | `memory/ledger/segment_store.go:423` 读 meta 键判发布，`:561` 点读 meta 判发布，`:1331` 更新窗口 | 06.102 按发布资格和窗口状态分别比较，不凭值读取次数裁删 |
| 开档全 CRC 可改读时校验 | `memory/ledger/journal.go:673` 全日志校验；slot 的 absent/empty/tombstone、遗漏条目不由正文摘要涵盖 | 未有等价证明前保留；06.102/06.105 核全部读面 |
| 阶段档案有生产消费者 | `restoreStageSpan`、`IntegrityPassSpan`、`DirectoryMaintenancePass*` 的已查读取仅在测试；`passRecords` 在生产追加 | 06.101 核全引用与自身成本，06.104 比较移出默认生产路径 |

本表末两行的处置已由 06.102 落定：CRC 保留、`passRecords` 保留（增长轴更正为维护 pass），理由与读数见 [D9.3](design.md)/[D9.5](design.md)。

## D2 角色、必要性与替代比较

事实是权威；索引、缓存、发布摘要是否可重建，要按丢失后的行为和独立消费者判断，不能按副本数量判多权威。
下表是待比较角色而非保留清单；06.101 补齐生成→存储→读取→返回、失效/重建与全部读写者，06.102 逐行签发。

| 角色 / 当前承载 | 独立需求与删除反事实 | 保留 / 最小改 / 删并或替代的比较点 |
|---|---|---|
| 身份索引：slots、分区 idx、原文 blob/ref | 同键判重、冲突、存在/缺失及按键取原字节；删后可能全史查找或串身份 | 保留需消除合法聚集误判；最小改寻址/碰撞处理；合并须覆盖分区、墓碑、缺失和原文 scope |
| 有序索引：order、idx 的提交位置 | 最新 checkpoint、尾回放、跨分区顺序、前驱与全序导出 | 保留扫描需量找目标的前段；局部定位与合并均核稳定顺序/早停，不能以键排序代提交序 |
| 发布元数据：meta 键、store:seat | 哪个窗口已发布、计数、分区发现、提交位置；不等于业务事实副本 | 区分 meta 存在性与值、seat 标量与分区；合并后失败切点、重推、EventCount 全史含义一致 |
| 窗口：读视图增量、meta 窗口状态、segment cache | 写者旁读取尚未陈述的提交、窗口定位与复用；缓存命中不是新的授权 | 区分每视图快照范围、缓存失效和重建；删缓存可能只把成本移到热读，须同尺量 |
| 校验职责：帧 CRC、槽/序校验、水位、原文 digest/length | 原子帧、地址可信、序与遗漏、正文完整性各自不同；摘要不能证明不存在 | 保留全扫是安全基线；读时替代需覆盖未读坏帧与负查询，不以更少字节自动获准 |
| 压缩定位：产物事实、投影 key、anchor Content | 恢复已生成摘要，不再调用模型；覆盖原始事实且保原序/预算 | 保留正文须独立来源理由；可定位后去冗余；新增索引/字段的成本与持久迁移一起比较 |
| 原文读面：裸读、scoped 读、ScopedReader、RecallPort | 操作员转换、分段召回、接口满足及授权；直接调用数不足以裁删 | 先迁行为锚；能共用读取但不合并不同 scope/分段语义，无消费者面才删 |
| 观测：计数器、时钟、阶段档案及报告 | 验证者要能发现退化，不等于生产要永久保留每次采样 | 保留量自身开销；最小缩采样；移到测试/隔离探针仍可证伪增长，不以关掉量得零成本 |

### 方案总账（06.102 填同尺结果后选择）

| 方案 | 收益候选 | 代价 / 否决条件 |
|---|---|---|
| 保留现状并收窄未证声明 | 最小迁移面，保留已知安全路径 | 已复现缺陷仍要解决；不能只声明未知增长后关闭 |
| 最小修正 | 针对碰撞、前驱起点、scope 首读或采样寿命消重复工作 | 若特殊状态/额外副本使总维护成本上升，重比删并案 |
| 删除或合并派生层 | 减少维护、同步、修复与配置面 | 角色缺失、查询退化、原文丢失或校验缺口即否决；不以行数作判据 |
| 替代实现 / 成熟库 | 可能删除自有格式、索引或修复代码 | 不预选；先读一手文档及版本，再用同数据/故障/权限/冷热条件原型比较 |

四行的同尺结果已由 06.102 逐角色填毕，行选择与读数见 [D9](#d9--06102-方案比较记录)；本表不再单独记结论。

比较记录包含独立消费者、状态/字节副本、提交/失败/重启、配置/格式迁移、开档/读/写/关闭成本与运维负担。
外部方案未取得一手原文或真实原型时只列未验证，不以依赖声誉代替只读、原子性和存储序证据。
A/B 的共享提交位置、发布资格与持久格式只有一份签发事实；A 定义应用/快照需求，B 不从待读取的 checkpoint 自身派生开档所需索引。

## D3 完整性、缺失与转换

| 读面 | 等价比较的正常 / 反例 | 失败归属 |
|---|---|---|
| 正向点读与枚举 | 原字节/最新值；槽损坏、span 越界、正文损坏、漏索引与发布不足 | 存储具名返回，消费者不静默跳过 |
| absent / tombstone / 命名空间 | 未提交键合法 missing，墓碑不复活且保探测连续；损坏不能伪造不存在 | 原文 scope 不可见仍按既有不泄漏语义回答 |
| 顺序、前驱、尾与首次开档 | 跨分区/会话隔离、年轻无 checkpoint 链；派生丢失、坏水位、无序引擎 | 安全重推与拒绝的条件逐项签发，不一概全局拒绝 |
| 写者旁只读视图 | 未陈述窗口可读；撕裂尾、零填充尾、中间零洞及坏索引 | 只读不截断、不修复写盘；损坏与可忽略尾区分 |
| 原文与分段 | scope/过期/空正文/二进制/多字节、分段拼回原字节 | `RecallMissing`、`RecallFailed`、完整性错误不互换 |
| 外部转换 | 干跑源不变；新根独立重开核每键/每事实/原文/计数/提交序 | 源变化、冲突或部分目标拒报 ready，不恢复运行时冷读兼容 |

当前基线 `TestJournalOpenRefusesCorruptionBehindAStatedWatermark`（`memory/ledger/journal_test.go:2955–2977`）在水位到日志末端后损坏帧，分别要求 `openJournalLog`、`openJournalView(dir, false)` 当场返回 `ErrJournalCorrupt`；它包含安全性质和旧 open 时机两层断言。06.102 取证保留该原锚，不用替代方案改写基线。
变更校验时机时，损坏、present/absent/tombstone、枚举遗漏、前驱、原文、未访问历史损坏及写者旁只读安全都纳入等价证明。06.102 在 D6 逐条签发旧时机断言与完整成本锚的迁移：旧断言、替代性质及可观察时点、文件、全名/完整 argv/命中数、独立安全等价审。未访问坏帧不能以“尚未读取”免验；所选保证未证则保留全 CRC，不预选读时替代。
06.103/06.105/06.106 按签发分流：保留方案执行原安全/成本锚；替代方案先完成已签等价迁移，再执行替代锚及全包回归，不叠加强制旧 open 时机。旧测试中不依赖该时机的安全断言继续承接，迁移不得靠 skip、删测、宽断言或排除全包逃过旧测试；原基线记录与历史不改写。
去 CRC 的实验只属于安全反例，不能充作性能负控。性能负控增加真实读取/枚举/重复阶段，保持所选且已证的完整性保证不变；保证未证仍保留 CRC。
转换复用 `scripts/fold-cold-root/main.go` 及其正式 CLI 测试；若选型要求更改，06.102 先签发精确改动，源保持只读、新根验证后才可交付。

## D4 产物与原文清理

06.104 遍历 `CompressProductFor`、直接构造/解码、插件生产者和 `PlanByAge`/`FoldByAge`/恢复入口；区分 mapped 事实、段产物和模型摘要。
已否定的是“ID 含 text”，不是“Content 已证冗余”。逐生产者回答 request/producer/key 是否持有、产物是否已耐久、定位失败如何报告。
保留 Text 原字节、Covered 原序、摘要预算、原始来源与已完成义务；恢复时不重调模型，不以机械重算替代历史模型摘要。
若部分产物没有可读事实，先解决真实来源/发布语义；不能删 Content 后把缺失推给恢复者，也不能默认新增一套正文存储。

原文旧序号逐项保全：#1 `OriginalStore.Get` 服务外部转换；#2 `OriginalStore.ReadSegment` 保留待裁；#3 `GetScoped`、#4 `ReadSegmentScoped` 保留真实能力；#5/#6 `ScopedReader.Get/ReadSegment` 先迁锚再裁；#7 `RecallPort` 与 `Run.RecallReads` 继续入账。
`GetScoped` 的接口满足面使 `Tier.Originals` 能绑定 `ScopedOriginalStore`；删方法可能让运行时断言转 false，令 recall_get 失去 Originals，即使无直接调用亦不能判死。
正式活链为 `host_document.go` 装配 recall → `tool/recall/get.go` → `RecallPort.ReadSegmentScoped` → scoped store；同时保全 Query/Reference/GetParent 的调用和错误传播。
#2/#5/#6 若裁删，先把字节、行界/字节界、scope、过期、损坏与接口绑定断言迁到保留入口，再处理 skeleton_assert、契约示例和全部集成测试；不按同名批删。
公共/文档/baseline 连带面由 06.102 与 13.102 共同签发，在删除窗口由指定拥有者单写；13.105 后续对账，不作为删除的反向前置。范围未冻结或等价锚未就绪时保留接口；所需清理仍欠账则本叶不勾，不以转派代完成。

## D5 成本闭合而非指标加层

成本清单覆盖冷编译、冷开档、热态、索引维护/增长/重推、原文首次/重复读、查询、前驱、提交、checkpoint、rebind、关闭与整代 total。
各操作分别列历史字节/键数、目标之后历史、匹配集、分区数、链深、尾量、scope 引用数、采样次数等轴，不写所有访问 O(1)。
现状/保留方案的完整帧 CRC O(日志字节)单列字节与时长；替代方案按同轴列开档、首次/重复读、负查询、枚举、写入、checkpoint、rebind、关闭和总量的全部校验成本，不把移出 open 的成本记成消失。索引回放窗口为零不等于开档零读取，不遮蔽未知 boot/total 增长；所选完整性保证未证仍保留 CRC。
旧常驻 index/order/windows 规模、当前枚举计数、EventCount 全史标量分列；指标删除后仍测墙钟、分配/保留量和实际 I/O，不能把无出口记成零。
`passRecords`、时钟读取与原子计数的开销随调用/历史实测；比较默认生产、隔离采样与关断态，优先复用测试可观测面而非新公共 hook。关断对照可用测试替身或隔离 overlay，不预设新增生产配置开关。
阶段计数与时长对应同次调用、同区间、同单位；父子区间说明包含关系，重复记账/漏阶段用独立控制捕获，不靠相减恒等式证明覆盖。
保留原 1.3× 判决含 boot/total；校准不能事后放宽。冷编译/冷开档/热态分别报告，同机同输入深浅对照且保留失败轮。
历史末首低比值不证明常数复杂度；旧 GetParent 两枚测试钉 H+2/整条 trace 扫描数，只作当前计数完整性的证据，不冻结未来的线性实现。
若改进读取，先将这些锚迁为真实访问全成本及安全不变量，再保留可失败的历史扫描控制，不能删断言让未测新路径过绿。校验时机变更同样先迁 `TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt`：D6 逐项分开旧开档全扫形态与完整计量性质，替代后的校验不能漏在首读、负查询、枚举、写入或关闭等区间之外；原安全/成本基线留作对照，不把旧线性形态强制到新方案。
`memory`、`journal`、`localfile` 别名、`file`/真实 rustviking 及声明的 Engine 覆盖面分开；无 rustviking 资源或 skip 不是全档绿色，替身测试只证明替身覆盖的协议。

## D6 签发区（06.102 已填，待独立设计审生效）

本节由 06.102 填写，比较与读数见 [D9](#d9--06102-方案比较记录)。**生效条件**：独立设计审逐项确认（D6 第 10、13 行）后，06.103/06.104 才按下表子集派发；候选文件不是整簇写权限。**行5④（scoped 损坏作用域收窄为身份级）是行为差异，须经 A 侧（12.102）会签后方可派发 06.103 的 `kv_original_store.go`/`reference.go` scoped 子项**——已于 2026-11-21 会签通过（12 域 D8.7.1 第 6 条）。**编排者追认（2026-11-21）**：行6 偏移索引实施为**按槽键控**（`capacity×8`，字＝槽键最近提交的 order 行尾）而非本表"每提交 8 字节"——槽记录布局冻结无 seq，按 seq 二分违背"读数不随目标后历史增长"；文件随容量轴而非历史轴增长，共用槽表水位与唯一重建规则，表头/版本零改动（理由全录 D10.6），予以追认，06.106 独立审复核。任何新增字段、格式、依赖或跨拥有者文件，先回本表补签。

| 签发字段 | 值 |
|---|---|
| 候选身份与反证 | 源码候选 `7b0cde162627ad37f1d364bbf3508b3bd0518431`（派发时重核）；合同面＝`memory/ledger` 三处探测循环 `journal.go:1488/1551/2163`、原文 scope 读 `reference.go:276/290`、前驱走查 `assembly/read.go:274/327`。**已证缺陷**：H08（`TestLegalClusteredKeysSurviveCommitAndReopen` 红：127/128 提交、缺失读报损坏、root 不可重开，本叶复跑 `-count=1` 原样复现，raw exit 1）。**已证成本**：H09（前驱第一段随目标之后历史线性：16/64/256→18/66/258；`recall_trace` 每步一次 `GetParent`，128 条更新下 398 次命名；scoped 首读付 `blob:`+`ref:` 全族，24 代＝48 对）。**已驳回**：H07 的“ID 含 text 故不可寻址”（`compress_product.go:76` 哈希输入＝request/producer/key）；H10 可等价替代（等价面为零，见 D9.3）；06.101 战报对 H11 的“生产追加”口径按 D9.5 澄清限定（`openPass` 仅维护路径三处，D8.5 现文“每 pass ＋1 条”与此一致，D8 原文不改写）。**仍待证**：真实 `Tier` 装配下同一聚集、`file`/rustviking 档、并发路径、boot/total 未归因项（06.105）。 |
| 方案与替代 | 三处选择：① **H08 取“探测终止条件＝表本身”**（`probe`/`lookupOnce` 上界 `used+1`，`stageProbe` 上界 `capacity`；走满即以 `errSlotUnplaceable` 回到既有唯一修复规则“从 order 重建”，绝不以链长判损坏）；被否：扩链/溢出区（新布局＋新修复面）、开档分类后“降级为可恢复事实”（仍不修读路径，且对哈希同桶无效）、判定改判重建重试（mod 不变则重建出同一聚集）、按探针失败即扩容（对哈希同位聚集无不变量收益，却引入提交路径的状态变化）。② **H09 前驱取“逐提交 order 行偏移索引”**（新派生文件，8 字节/提交；`GetParent`/`parentFromBodies` 命中目标行后向后走，第一段消失，第二段＝链距保留）；被否：槽记录内嵌行偏移（把记录布局耦合到无界键长，`fileOnce:1457–1465` 的 stride 倍增会整表翻倍）、提交时先查身份索引再定段（只能省分区分支，走查仍在）、接受现状（H09 已证增长，不许“已声明”代替消除）。③ **H09 scoped 首读取“按身份解析单条”**（`GetScoped`/`ReadSegmentScoped` 走 `entryLocked` 的两次点读，`ensureLoaded` 只留给 `BlobCount`）；被否：保留全族装载（24 代 48 对→与族规模同增长）、惰性按代装载（引入代际索引与失效面，收益低于直接点读）。同尺读数与被否理由见 D9.1/D9.2。 |
| 精确所有权 | **06.103**：`memory/ledger/journal.go`（探测上界与终止语义、删 `journalSlotMaxProbe`、order 偏移文件的写/截断/重建与水位、派发 seek 面）、`memory/ledger/journal_kv.go`（暴露 seek 的一次点读；提交/读语义不变）、`memory/ledger/segment_store.go`（可选 seek 接口与 `eachIndexEntryNewestFirst` 的命中路径）、`memory/ledger/kv_original_store.go`（scoped 读的按身份解析；`load`/`BlobCount` 不变）、`memory/ledger/reference.go`（`GetScoped`/`ReadSegmentScoped` 改走单条）、`assembly/read.go`（`GetParent`/`parentFromBodies` 命中即定位，缺 seek 面回落原走查）。**测试镜像同叶**：`memory/ledger/journal_test.go`、`journal_kv_test.go`、`kv_original_store_test.go`、`segment_store_test.go`、`assembly/read_test.go`。**06.104**：`assembly/compress_consumer.go`（只改 `anchorEntry` 注释里被证伪的理由，字段与行为不变）；`memory/ledger/journal.go` 的 `passRecords` **不改**（见 D9.5，故不列入其写面）。`api/event/compress_product.go`、`memory/ledger/{atomic_store,rustviking_kv,original_store,skeleton_assert}.go`、`scripts/fold-cold-root/main.go`、`examples/contract/**`、`tests/integration/**` 本变更加载下**均不改**：偏移文件是派生文件，缺档/短档由既有单一修复规则重建，转换器无需适配。共享文件只有 `memory/ledger/journal.go`，单写拥有者为 06.103。 |
| 接口、字段、格式、依赖 | **增**：派生文件 `memory/ledger` 的 order 行偏移索引（每提交 8 字节，路径由 `journalAddress` 独占；无持久事实格式变化）；两个**可选**接口分支（引擎侧“按 seq 取行偏移”、存储侧“从目标条目起走”），未实现者回落现状走查。**删**：常量 `journalSlotMaxProbe` 与其“链长即损坏”判词；`GetScoped`/`ReadSegmentScoped` 对 `ensureLoaded` 的调用（`BlobCount` 保留）。**不变**：`AtomicStore`/`CommitOrder`/`ScopedOriginalStore`/`OriginalStore` 契约、槽记录布局（`journalSlotRecordHeaderBytes=20`/`stride=96`）、槽表头字节布局与 `journalSlotVersion`、`SESSION_FORMAT`、`ErrJournalCorrupt`/`errSlotUnplaceable`/`ErrSnapshotCorrupt`/`RecallOutcome` 语义、提交结局五值。**迁移**：偏移文件缺失/短/长于 `seq×8`、槽表头 `used` 与槽记录不符，均走既有“从日志重建”的一条路径；旧 root 首次开档多一次重建（只读文件字节，不改事实）。**外部依赖**：不新增库、CLI、服务、网络与权限。 |
| A/B 共享事实 | 本叶只给候选与必需事实，最终单一记录由编排者与 12.102 会签：① **提交结局分类不变**（`FactUncertain`/`ErrCommitUncertain` 仍只表示“结果未知”，但**合法聚集不再进入该分支**——A 的 D7 静态映射 `segment_store.go:861–878` 的成因集合缩小）；② **提交位置/发布资格不变**：`store:seat` 标量、`{pid}:meta:{window}`、`publishedPosition` 的判据与写入位置一字不动，偏移文件不参与发布；③ **快照覆盖语义不变**：B 不新增读序，`EachIndexNewestFirstUntil` 的顺序与早停位在命中/未命中两条路径上相同（A 拥有的切面截止不受影响）；④ **原文定位**：scoped 读改为按身份解析后，“引用无正文”仍具名 `ErrSnapshotCorrupt`，但作用域从“全族加载失败”收窄为“该身份失败”（**行为差异，需 A 侧确认不依赖全族污染**）。 |
| 消费者与验证 | 全部真实调用面与三锚见 D9.8 表与下列断言清单（既有名以 `func <name>(` 定义命中 1 核过，拟新增名命中 0）：**H08**：正锚 `memory/ledger -run '^TestAddressable'`（13 名）与 `-run '^TestFactCommitAnswersNewExistingConflictUncertain$'`；负锚 `-run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10`（实施后必须由红转绿，此刻为红）；拟新增负锚 `-run '^TestAddressableIndexRefusesATableWithNoEmptySlot$' -count=10`（满表不静默答缺失，走重建后再答）。**H09 前驱**：拟新增 `assembly -run '^TestPredecessorQueryBoundsTheWalkToTheChain$' -count=10`（目标之后历史 16/64/256 时读数不增，链距仍计）；旧计数锚按 D6 第 9 行迁移。**H09 scoped**：`memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10`（7 子案）、拟新增 `-run '^TestScopedReadResolvesTheNamedIdentity$' -count=10`（`wholeSetLoads=0`/`identityKeys=2`/损坏只落该身份）。**消费者**：`assembly -run '^(TestParentLookupKeepsSessionIsolation\|TestTierQueryHonorsScopeBeforeDecoding\|TestReadCorruptionNamedError\|TestRecallTraceThroughTierCountsOldTargetLookup)$'`、`tests/integration/capabilities -run '^(TestRecallToolSurface\|TestSearchRecallsFromRealStore)$'`、`tests/integration/history -run '^(TestStorageCapabilities\|TestOriginalReferenceIsolation)$'`、`scripts/fold-cold-root -run '^(TestOfflineConversionPreservesEveryFact\|TestSourceChangeRefusesConversion\|TestPartialTargetIsNotReportedReady)$'`。构建标签：`memory/ledger`、`assembly`、`scripts/fold-cold-root`、`tests/integration/{history,capabilities}` 均无构建标签；smoke 标签不属本单。相邻回归：`memory/... ./assembly ./tool/recall ./tests/integration/history ./tests/integration/capabilities ./scripts/fold-cold-root -count=1`。 |
| 校验时机旧断言与替代性质 | **选择保留全帧 CRC：无迁移，原断言原样承接。** 逐条承接：`TestJournalOpenRefusesCorruptionBehindAStatedWatermark`（`journal_test.go:2955–2977`）中“水位覆盖帧损坏→写 open 与只读 open 均当场报 `ErrJournalCorrupt`”两条，原时点、原文件、原名不变。替代案（校验移至读时）**未被选中**，理由为等价面未证且已找到反例：`journalAddress.value`（`journal.go:1804–1818`）只按 span 读值并 `json.Unmarshal`，**不做帧 CRC**，故“旧帧被信任、读时再验”会让一个仍可解码的坏值静默返回——这正是未访问坏帧缺口。等价面清点（供日后重开）：present/absent/tombstone、枚举遗漏、槽链越界、前驱、原文 digest/length、未访问坏帧、只读视图不截断、撕裂/零填充尾（写视图必须能定位末完整帧才能安全追加）。本叶同时记录 H08 对校验面的**加强**：走查变长后每次查询读更多槽记录，坏记录更早被逐记录 CRC 命中并回到重建。 |
| 完整成本旧断言与替代性质 | 全 CRC 保留，故 `TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt`（校验字节＝日志、回放窗口 0/0、200/2000 提交的 30400/304000 字节）**原样承接、不改写、不缩**。新增成本项逐条入账：① 合法聚集的写入代价＋读取代价（129 同桶：单轮 51,096÷3≈17,032 次槽记录读；其中簇内插入 ≈8.4k 次、簇内读 ≈8.4k 次，96 字节/次≈1.6 MB；读单键≤链长 129 次）；② order 偏移索引 8 字节/提交＋每次前驱查询 1 次点读＋1 次 64 KiB 块读（`journalOrderChunkBytes`）＋链距；③ scoped 首读由“2 次前缀扫描、2×族规模对”降为“2 次点读/身份、热读 0”。**不缩**原 1.3× 判决（含 boot/total）：偏移文件把 `capacity×stride` 之外的索引字节加入分母而非分子，开档全 CRC 的 O(日志字节) 仍是未消项，由 06.105 全成本矩阵判。 |
| 迁移执行清单 | ①`memory/ledger/journal_test.go:1325/1413/1489–1490/1509` 五处 `journalSlotMaxProbe` 引用：`slotTestRecordOffset` 与“植入孤儿记录”两处上界改由表形状推出（`header.Capacity`），`TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily` 保留其自有字面界（`longest > 40` 与分布断言不变），H08 锚的 129 由字面量 `129` 构造（不再由被删常量推导）——性质不变，仅去符号依赖；②`assembly/read_test.go:671` `TestParentLookupCountsTheSearchBeforeTheTarget` 的 H+2 读数与 `TestRecallTraceThroughTierCountsOldTargetLookup` 的 398 是**现状观测**，迁为全成本轴：`TestPredecessorQueryBoundsTheWalkToTheChain` 固定前驱间距、把目标之后历史 16/64/256 各跑一遍，断言“前驱查询的 order 行读与索引条目数**不随目标之后历史增长**”并保留一个可失败的历史扫描控制（若回到从头走查即红）；原两条测试保留安全/答案断言（同链前驱、会话隔离、空答），不再冻结线性次数；③全 CRC 无迁移（D6 第 7 行）；④新锚 `TestAddressableIndexRefusesATableWithNoEmptySlot`、`TestScopedReadResolvesTheNamedIdentity` 为拟新增（当前 0 命中），落 `memory/ledger/journal_test.go` 与 `memory/ledger/kv_original_store_test.go`；⑤禁止以改名/skip/删测/宽断言或排除全包替代上述性质；迁移后全包回归仍须真实非零命中。 |
| 独立安全等价审 | **未执行（本表为待审稿）**。送审范围＝D9.1–D9.7 七案结论、D9.8 全部原始命令/退出码/读数、D9.9 未验面。审者须复现：①探测上界 `used+1` 的可靠性（表头 `used` 与实际槽记录一致由 D6 第 4 行的同一条修复规则保证；满表→重建→重答，绝不静默答缺失）；②偏移文件与水位/截断的一致性（缺/短/长均重建；只读视图不写文件）；③scoped 读作用域收窄是否削弱任何安全性质（引用无正文仍具名失败；scope 外键仍答 missing 且不泄漏）；④被否四案的理由是否成立（尤其“扩容无不变量收益”与“记录内嵌行偏移的整表翻倍”）。任一性质不能成立即回本叶重签；所选保证未证则保留 CRC。 |
| 成本与安全 | 增长轴：槽点读＝链长（≤`used+1`；正常族的“最长链 12/32,000 键”是常量注释所记历史测量，本叶未复测分布，只由 H08 锚证合法簇的上界）；枚举＝`capacity×stride`（不变）；提交＝1 帧＋1 屏障＋每键 1 条记录（不变）；前驱＝1 点读＋1 块读＋链距（原为“目标之后历史＋链距”）；scoped 首读＝2 点读/身份（原为 2×族规模）；开档＝全日志 CRC（保留，单列字节与时长）＋窗口回放。冷/热与深/浅按 06.105 矩阵分列；`memory`/`journal`/`localfile` 走本方案，`file`（rustviking 缺资源）记未验。安全：原子帧、槽记录 CRC、水位、原文 digest/length 四条防线全部保留；删掉的只有“链长≥128 即损坏”这条被证伪的判词。原 1.3×/boot/total 判决不缩、不事后放宽。 |
| 清理交接与数据边界 | `#2 OriginalStore.ReadSegment` **保留**（行界语义无替代入口，`examples/contract/memory_ledger/OriginalStore_example.go:76`、`tests/integration/history/{storage_capabilities_test.go:64/145,original_reference_isolation_test.go:124–239}` 真实调用）；`#5/#6 ScopedReader.Get/ReadSegment` **保留**（`NewScopedReader` 是 `ErrScopeRequired` 的唯一具名拒绝点，`skeleton_assert.go:76/78` 钉接口面，三处集成测试真实使用）——保留理由为“有独立需求”，不是“未及处理”；若 06.104 仍判删，等价锚迁址草案见 D9.6（迁至 `ScopedOriginalStore.GetScoped/ReadSegmentScoped`＋`skeleton_assert` 同窗口替换），删留执行权在 06.104。13.105 连带面：`assembly/compress_consumer.go:378` 的注释、`docs/wiki/memory/memory-architecture.md:107` 的现有正确叙述（无需改）、任何以“ID 含 text”为前提的文档。数据边界：外部转换源只读、新根独立重开核每键/每事实/原文/计数/提交序（`scripts/fold-cold-root` 不改即能读新派生文件，因其由开档重建）；真实数据根与用户 `--source` 根不读不写；资源与进程按编排文档释放表。 |
| 独立审与反转 | 审者结论待填（非实施者，06.106）。回设计门的触发：①偏移文件的一致性无法并入既有单条修复规则；②真实档位下合法聚集的单次读或写入代价越过 06.105 已签界；③scoped 读作用域收窄被任一真实消费者证否（例如依赖“全族损坏即整体失败”的调用方）；④任何新增持久格式/依赖需求。反转/回退＝丢弃 06.103 副本改动、仓库内零 diff；事实、序、原文、scope 与发布资格均由既有防线保证，回退不改变已提交日志字节，只让派生索引回到重建路径。 |
| D13 续签（2026-11-22） | **配置链发现的写面与锚另见 [D13](#d13--boot-走查重开裁定)**（06.106 判决①的修复形状）。新签窗口 **06.103R**（拥有者＝06 域）：生产写面 `assembly/commit.go`（发现段，该文件首次入本单元签发面）＋`memory/ledger/tier_facts.go`＋`memory/ledger/journal_kv.go`（各退役一条单消费者面）；测试写面 `memory/ledger/journal_kv_test.go`（两处改调未导出 helper）＋`assembly/commit_test.go`（新增负锚，既有两锚一字不改）。本表第 3/4/6/9 行的既有写面与锚、第 5 行四条共享事实、第 7/8 行 CRC 与完整成本面**全部不变**；第 10 行（独立安全等价审）与第 13 行（独立审与反转）对 D13 同样适用。无持久格式/键/配置面/接口新增，只退役两条被本裁定移除唯一消费者后的面，故无需 A/B 会签（理由见 D13.6）。 |
| R4 补签（编排者代签，2026-11-22） | **本行由编排者授权补签，理由＝06.106 R4**：06.106 关闭轮判 2.4「部分兑现」，唯一原因＝具名锚 `TestBootIndexEntriesBoundedAcrossGenerations` 缺（槽键控机制与计数面原已有据）。**锚名** `TestBootIndexEntriesBoundedAcrossGenerations`。**落点** `assembly/symptom_anchor_recovery_test.go`（新锚＋`orderLineBytesOf`/`orderLineShape`/`commitsOfShape` 三 helper；共享 fixture `walkBootGenerations`/`generationBoot` 零改动，既有锚逐字节未动）。**判据** F1 boot 期枚举条目：以第 2 代（`observed[1]`）为参照，其后各代 `indexEntries ≤ 2×indexEntries[1]`，且每代 `2×indexEntries < indexed`（`indexed`＝链上索引事实数；fixture 前提由 `judgeBootFixture` 判：链与日志逐代增长、每代 settle 与 account 相符）。F2 槽键控容量轴（本表行6 追认的 `capacity×8`）：同 100 键 1 次提交与同 100 键 50 次重写提交，`order.line` 字节必须相等（且后者日志显著增长，证历史轴变了而表不变）；5000 键 1 次提交的 `order.line` 字节须大于 100 键根（容量轴增长）。**命令** `go test ./assembly -run '^TestBootIndexEntriesBoundedAcrossGenerations$' -count=10 -v`。**预期命中** RUN 10／PASS 10、raw exit 0；读数 `indexEntries=[0 0 0 0 0]`、`journalLogBytes` 逐代增长、`order.line`＝8192/8192/131072 字节（十轮一致）。**负变异**（`-overlay` 指向 `/tmp/rr-r4/mut/`，仓库字节 md5 前后一致）g1＝`Load` 恒走 `rederive` 全索引枚举→锚红（`indexEntries=[0 43 94 145 196]`，"enumerated 43 … beside the 43 facts"）；g2＝`fileCommit` 每提交 `grow(capacity+1)`→锚红（8192→8592 字节）；控制＝`TestBootFixtureGrowsTasksAcrossGenerations` 两组变异下均绿。**写面**：本行＋该测试文件＋本文件 D13.9；不新增生产字段/格式/依赖/跨拥有者文件，不改任何任务勾选（06.106 已勾；CARRYOVER §四.1 的 2.4 勘误由编排者另办）。 |
| F2 补签 · 无屏障标记追加（编排者裁决，2026-11-22） | **本行签一张新窄口，理由＝12 域 D8.9.6 的长跑回归裁定**（256 窗每 cut 在 landing 锁内多付一次 fsync）。**名称与签名**：`AppendOrdered(ctx context.Context, batch Batch) error`；消费者侧声明为 `assembly/checkpoint.go` 的未导出接口 `orderedAppend`（消费方窄口，Go 结构化满足），实现在 `memory/ledger/journal_kv.go`。**不新增导出命名类型**：故 D1 概念预算零新绑定，`golden/runtime/architecture.json` 的 `bindings`/`new_max`/`exported_interfaces_max` 三处均不动（备选「ledger 侧导出接口 `OrderedAppend`」因必须新增绑定并抬两个棘轮上限而被否，见下行）。**语义**：只把一条记录送进 journal 的追加序（`journalLog.append`），**不完成屏障**（不 `file.Sync`），也不向读面发布（resident 集与索引在后续 `Commit` 完成屏障后由 `confirm` 就位）；空批量零操作；Close 之后／无写权具名拒绝；帧界具名拒绝，其余 append 失败如实归 `ErrCommitUncertain`。**顺序与耐久的分离**：顺序＝文档所记的追加序（先于它的记录在前、后于它的在后）；耐久＝搭后续提交的既有 fsync（同一 journal，`fsync` 刷前序全部字节）；撕裂/缺席均为既有读法（`tailFactKeys` 答 `nil` 读全日志、`quietPointBoundsTheAccount` 答 false）。**唯一消费者**＝`stateQuietPoint`。**否决理由**：既有 `Commit` 面按 `memory/ledger/atomic_store.go:45` 契约「A commit takes exactly one barrier」**必带一次 fsync**，而标记只需顺序不需要即时耐久；用它＝每 cut 在 `r.landing` 内多付一次屏障，256 窗高频折叠下阻塞全部事实提交。**写面**：`memory/ledger/journal_kv.go`（新方法＋`journalRecordOf` 提公共件，`Commit` 的帧构造与行为一字不变）、`assembly/checkpoint.go`（`orderedAppend` 声明、`stateQuietPoint` 改走新面；仍持 `r.landing` 保序；`CommitOrder` 缺失仍零操作早退；**保留带屏障 `Commit` 作为无新面引擎的回退**，故既有锚与成本面逐字节不变）。**本表其余各行的写面、锚、共享事实与 CRC/成本面全部不变**；第 6 行「成本与安全」的「提交＝1 帧＋1 屏障」描述的是 `Commit` 面，不变，标记面的成本记在 12 域 D8.9.6。**验证**：12 域 D9.11（长跑 match、锚矩阵、三则负变异）。**反转**：删 `orderedAppend` 分支即回到逐 cut 一屏障的基线行为；无持久格式、键族、载荷或版本改动。 |

## D7 关闭判据

06.105 覆盖真实 Tier/recall/query/转换和全部公开档位；06.106 独立审角色台账、同机制相邻路径及原始成本，不只看测试名。
范围内正确性、安全、需求或未知增长非空就回对应叶；移交只改变调度，不改变本单元未完成状态。
新方案若增加格式/依赖或破坏原文、序、scope、只读性，停止实施并重签；保守防线在等价证明之前继续有效。

## D8 · 06.101 取证记录

> 产生方式：只读枚举（代码/Grep/既有测试原始输出）＋一枚新测试探针＋本记录。产品 `.go` 零改动。写面＝`memory/ledger/journal_test.go` 新增 `TestLegalClusteredKeysSurviveCommitAndReopen`、本节、`tasks.md` 06.101 指针行。06.101 未勾选框；未 commit/push/archive。
> 执行根 `/Users/pengweiye/Documents/codes/tagent`；Go 1.24 工具链，darwin/arm64。既有锚名先用 Grep 核定义命中 1，再运行；新增锚运行前命中 0。

### D8.0 结论先行

| 疑点 | 结论 | 一句话证据（详见 D8.6） |
|---|---|---|
| H07 双压缩接口、正文内联各有独立必要性 | **部分证伪**：前置理由被推翻，必要性未证 | `api/event/compress_product.go:76` 的哈希输入＝`request\0producer\0key`，不含 text；“ID 含 text 故不可寻址”不成立。Content 之删留仍交 06.104 |
| H08 负载低于 50% 保证探测不足 128 | **证实**（真实存储入口，红） | 1024 槽、12.5% 负载下，同桶第 129 个合法键被 `ErrJournalCorrupt` 拒绝；随后该 root 无法重开 |
| H09 GetParent/原文首读的历史工作量不可消 | **证实** | 目标之后每多 1 条历史，前驱查询多走 1 条索引条目（16/64/256→18/66/258）；scoped 首读付 `blob:`+`ref:` 全族 |
| H10 开档全扫 CRC 是不变量而非校验时机选择 | **未证**（保留 CRC） | 读面已枚举；absent/tombstone/槽链越界/枚举遗漏不在帧 CRC 覆盖内，等价证明为零。本叶不顺带删防线 |
| H11 常开采样与“增长已声明”足以闭成本 | **未证**（不能闭） | 阶段档案生产追加且无界，读者只在测试；boot/total 未归因项仍须 06.105 判 |

最大发现不是“算术反例存在”，而是 H08 经真实入口产生**三个**生产后果：提交被判 `ErrCommitUncertain`、合法缺失读被报损坏、root 重开失败。

### D8.1 新锚：构造与真实结果

- 命令：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10 -v`
- 构造：用 `slotAddressOf(key, journalSlotCapacity)`（`journal.go:2232`）在候选键里筛出**同一槽地址**的 130 个键（`0:cluster:%08d`）；其中第 1 个先提交后删除（墓碑）、中间 128 个提交持有、最后 1 个同桶但从不提交（absent）。全部经 `openJournalKV`（`journal_kv.go:47`＝`openJournalView(dir,true)`，即 `OpenAtomicStore(StorageJournal)` 在 `config.go:243` 调用的同一面）执行 `Commit`/`Read`/`Close`，再以同一入口重开读回。键由算法在**运行时**推导，不是写死字面量；断言靠真实存储入口，算术本身不构成证据。
- 实跑终局：`EXIT 1`；`=== RUN` 命中 **10**，`--- PASS` **0**，`--- FAIL` **10**。十轮输出逐字同构（仅临时目录名不同），非 flaky。
- 三段断言输出（原文，目录已缩写）：

| 断言面 | 实测原文 | 期望 |
|---|---|---|
| 129 个合法键须全部提交 | `committed 127 of the 128 held key(s)`；`1 of the 128 held key(s) … were refused, the first "0:cluster:00134887": memory: commit outcome uncertain: memory: journal frame is corrupt: …/slots holds no free slot within 128 of "0:cluster:00134887"; the table states 1024 slot(s) and the load rule grows it at 50 percent` | 全部提交成功，无损坏 |
| 合法缺失/墓碑读须答缺失 | `read "0:cluster:00135724" = 0 pair(s) (err=memory: journal frame is corrupt: … holds no free slot within 128 of "0:cluster:00135724")` | 缺失（err=nil） |
| 关闭后重开须可读回 | `reopen the root holding 127 key(s) one slot addresses: memory: journal frame is corrupt: … holds no free slot within 128 of "0:cluster:00134887"` | 重开成功、键全可读 |

- 机制定位（静态读码＋上述实测）：
  1. `probe` 只走 `journalSlotMaxProbe=128` 步（`journal.go:119`、`:1488–1508`），走满即报 `ErrJournalCorrupt`；
  2. 翻倍只看负载（`a.used*100 >= a.capacity*journalSlotLoadPercent`，`:1456–1468`，`journalSlotLoadPercent=50` `:114`），128/1024＝12.5% 远未触发；
  3. 故“链长落在界内”只是哈希分布的经验陈述，不是不变量；合法键经构造即可全落一桶；
  4. 报错在 `confirm()→state()→fileCommit→file→probe`（`:1305/:1346/:1405/:1436`），`confirm` 只 `rollback` order/seq（`:1336`，帧字节留在日志），`Commit` 出口包成 `ErrCommitUncertain`（`journal_kv.go:188–226`）；
  5. 生产映射（静态推断，未实跑 `CommitFact`）：`segment_store.go:861–878` 把 `ErrCommitUncertain` 判为 `FactUncertain` 并附 `ErrSegmentBarrier`——即一次合法聚集会表现为“提交结果未知，须重读裁决”。

### D8.2 逐文件角色清单

每行＝一个读/写面角色。来源列给出承载体；成本轴按“读什么、跟什么增长”记，不写 O(1)。

| 角色 | 来源（file:line） | 真实入口 | 返回语义 | 成本轴 | 正常 / 失败路径 |
|---|---|---|---|---|---|
| 槽地址（身份定位） | `journal.go:2232` `slotAddressOf`、`:2243` `slotHashOf`（FNV-1a 64＋混合器，mod capacity） | `openJournalView`→`Commit`/`Read` | 一个键的起始槽 | 1 次哈希＝键长；容量 1024 起（`:108`），负载 50% 翻倍（`:1466`） | 正常：槽空或同名；失败：同桶链满 128 步→`ErrJournalCorrupt`（`:1507–1508`，本叶实测） |
| 身份点读 | `journal.go:1518` `lookup`→`:1551` `lookupOnce`→`:1586` `stated`；`journal_kv.go:68` `Read`→`:260` `valueLocked`→`journal.go:2338` `read` | `AtomicStore.Read(Fetch{Key})` | 存在→原字节；缺失→空；损坏→具名错误 | 1 次槽记录读＋链步数＋值 span 字节；resident LRU（`kv_resident.go:11`，1 MiB）命中则 0 次落盘读 | 正常：命中/缺失；失败：链越界报损坏（实测把合法缺失也报成损坏）；槽记录不可解码→`errSlotUnplaceable`→按 order 重建（`:1518–1545`） |
| 墓碑 | `journal.go:1422`（`file(key, journalSlotAbsent,…)`）、`:1569`（absent→缺失） | `Commit(Batch{Deletes})`/`Read` | 删后读缺失且链仍可走 | 同点读 | 正常：不复活；失败：同链界 |
| 枚举（键/命名空间） | `journal_kv.go:102` `Keys`、`:117` `Namespaces`、`:122` `journalSlotScanBatch=512` | `AtomicStore.Keys/Namespaces` | 键序、不读值 | 一表一遍＝capacity×stride；`SlotTableBytesScanned`（`:2394`） | 正常：只答持有键；失败：表/记录不可解码→重建 |
| 提交序（最新在前） | `journal_kv.go:300/314`；`journal.go:2262/2274` `eachKeyNewestFirst[Value]` | `CommitOrder.EachCommittedNewestFirst` | 每键一次，到 visit=false 止 | 走过的 order 记录（＋值） | 正常：早停；失败：order 行校验 `errOrderUnplaceable`（`:1366`）→重建 |
| 落盘与发布顺序 | `journal_kv.go:188` `Commit`、`journal.go:2281` `append`、`:2318` `sync`、`:1305` `confirm`、`:1346` `state`、`:1336` `rollback` | 同上 | 成功＝durable；未定性＝`ErrCommitUncertain` | 1 次 append＋1 次 barrier＋每键 1 条槽记录 | 正常：整批或整拒；失败：`state` 报错→rollback（只回退 order/seq，帧字节留日志）→包成 `ErrCommitUncertain`（实测） |
| 水位（槽表头） | `journal.go:799` `journalSlotHeader`、`:2084` `decodeSlotHeader`、`:1586` `stated` | 开档与每次点读/枚举 | 本视图可见的日志末字节 | 头 1 读 | 正常：越水位记录按缺失；失败：头不可解码→重建 |
| 发布元数据（段存储 meta/seat） | `segment_store.go:509` `storeSeatKey`、`:485` `readSeat`、`:423` 窗口键枚举、`:561` 窗口 meta 点读、`:1331` `nextWindowMeta` | `FileSegmentStore.Load/publishedPosition/StoreEvent` | 哪个窗口已发布、分区发现、提交位置 | 座位 1 次点读；窗口 meta＝每分区 1 次前缀枚举＋每位置 1 次点读 | 正常：未发布不可见；失败：座位不可用→`rederive`（`:352`），具名拒绝 |
| 窗口／缓存 | `segment_store.go:129–190` `segmentCache`；`journal.go:1541` `windowValue` | `Read`/`GetEvent` | 写者尚未陈述的提交在读视图可读 | 命中 0 次落盘读，未命中 1 次值读 | 正常：缓存命中不等于新授权；失败：越窗口→按索引答 |
| 开档完整性（全帧 CRC） | `journal.go:672–673` `journalReplay(file,0,size,…)` | `openJournalView`/`openJournalLog` | 整条日志通过或 `ErrJournalCorrupt` | 日志全部字节（实测 200 提交 30400 B、2000 提交 304000 B，回放窗口 0/0） | 正常：整条校验后开；失败：中间帧坏→当场拒；**不覆盖** absent/tombstone/枚举遗漏/槽链越界 |
| 尾部判定（撕裂/零填充） | 同上开档路径；安全负锚两枚 | 开档 | 只读视图不截断，写视图裁到末完整帧 | 尾扫 | 正常：负锚 10/10 绿；失败：填充被帧续写→两视图均拒 |
| 前驱（GetParent） | `assembly/read.go:274` `GetParent`、`:327` `parentFromBodies`；`segment_store.go:1183` `EachIndexNewestFirstUntil` | `NewTierQuerySource`（`read.go:82`）→`GetParent` | 同链前驱键或空 | **两段**：目标之后全部索引条目＋目标到前驱段；`IndexWalkReads`（`segment_store.go:1070`） | 正常：visit=false 早停；失败：body 不可解码→具名错误；无 session 回落体读 |
| 原文（裸/scoped/端口） | `reference.go:161/201/238/255/268/276/290`；`kv_original_store.go:93/112/142/215/354/390/522`；`original_store.go:51/74/81` | `Tier.Originals`、`ScopedReader`、RecallPort | `RecallResult{Found/Missing/Expired/Failed}`、`SegmentResult` | 首读＝`blob:`+`ref:` 全族（1 load＋2×族条数）；热读＝0（`byKey` 命中）；身份点读＝2 键 | 正常：原字节/scope/分段拼回；失败：`ErrOriginalMissing`、`ErrOriginalExpired`、`ErrSnapshotCorrupt`/`ErrIntegrityMismatch`、`ErrSegmentRange` |
| 事实日志面 | `tier_facts.go:26/31/36/51/64/70` | `TierFactLog(tier)` | Put/Scan/Get/CommitOrder/Sync | 各操作沿用引擎轴 | 正常：透传；失败：透传引擎错误 |
| 压缩产物（定位） | `api/event/compress_product.go:74–77` | `CompressProductFor` | `ID = cmpout-`＋sha256(`request\0producer\0key`)[:8] | 1 次 sha256 | 哈希输入**不含 text** ⇒ 06.102/06.104 不得再以“ID 含 text”为由删 Content |
| 压缩产物（恢复正文） | `assembly/compress_consumer.go:374–381` `anchorEntry.Content` | `compress_checkpoint` 恢复 | 折叠产物正文随 checkpoint 走 | checkpoint 字节＝条目数×正文长度 | 行内理由称“键由 material 与 text 共同内容寻址”，与上一行冲突 ⇒ 必要性待 06.104 逐生产者枚举 |
| 外部转换 | `scripts/fold-cold-root/main.go:12–15` | `fold-cold-root` 命令 scan/restore | 新 root 的键/事实/提交序 | 源全键＋每批 `foldedBatchKeys` | 正常：源只读、目标预置即拒；失败：`ErrMigrateSourceUnchanged/ErrSourceUnstated/ErrSourceConflict/ErrSourceIncomplete/ErrRootWriteLocked/ErrBatchNotAtomic` |
| 指标（计数） | `journal.go:2381 IntegrityBytesScanned`、`:2394 SlotTableBytesScanned`、`:2411 ValueReads`、`journal_kv.go:140 JournalBytesOpen`、`kv_original_store.go:522 OriginalsReads`、`segment_store.go:1070 IndexWalkReads` | 生产者自增、读者混合 | 计数/字节量 | 自增 O(1) | 正常：可读；失败：无出口的计数无法复核（见 D8.5 未归因项） |
| 阶段档案（采样） | `journal.go:996` `passRecords`、`:1014–1020` `openPass`（每 pass 两次 `time.Now()`＋追加一条）、读者 `:2429` `IntegrityPassSpan`、`:2442` `DirectoryMaintenancePasses`、`:2458` `DirectoryMaintenancePass`；`assembly/run.go:1199` `restoreStageSpan` | **生产只写**；读者全在测试（`assembly/symptom_anchor_recovery_test.go:1291/1310/1382`、`symptom_anchor_longrun_test.go:135`） | — | 每 pass 2 次时钟读＋1 条记录，`passRecords` 无界（见 D8.5） | 见 D8.5；`restoreStageSpan` 无生产调用方 |

### D8.3 六个角色分列（供 06.102 判必要性，此处不下结论）

1. **槽**：身份唯一入口，点读/写入都靠它把键换成 span。删掉即每次读退化为全史扫描；保留的代价是本叶实测的链界缺陷（H08）。
2. **序**：`order` 行给最新在前顺序、前驱与全序导出，`EachIndexNewestFirst[Until]` 的早停出口只在序上成立。删掉则前驱、最新查询与导出都无早停位。
3. **发布元数据**：`store:seat` 标量＋`{pid}:meta:{window}` 决定“哪个窗口已发布、有哪些分区、提交位置”。其存在性与值用途不同（`:485` 读标量、`:423` 枚举分区、`:561` 点读判定），不能以“值只读一次”裁删。
4. **窗口**：读视图的 `windowValue` 让写者未陈述的提交在同进程可读；`segmentCache` 是热读缓存。缓存命中不是新授权（缓存失效只把成本移回热读）。
5. **校验**：帧 CRC（`:672`）、槽记录解码（`:2206`）、水位（`:1586`）、原文 digest/length（`original_store.go:51/74`）各证不同性质；摘要不能证明“不存在”，故 CRC 与其他面不可互替。
6. **指标自身**：`valueReads`/`slotScanBytes`/`integrityBytes`/`passes`+`passRecords` 中，前四者被生产或验证者读，最后一组只有测试读者（见 D8.5 末条）。

### D8.4 固定目标与前驱间距；目标之后的历史

探针：既有锚 `TestParentLookupCountsTheSearchBeforeTheTarget`（`assembly/read_test.go:671`），目标到前驱间距恒为 1，只改目标**之后**的历史。

| 目标之后历史条数 | 前驱查询命中索引条目 | 同轮最新条目查询 |
|---|---|---|
| 16 | 18 | 2 |
| 64 | 66 | 2 |
| 256 | 258 | 2 |

读数＝目标之后条数＋2，十轮一致（每轮都打印三行，见 D8.6 命令 1）。消费者侧同构：`TestRecallTraceThroughTierCountsOldTargetLookup` 在 128 条更新历史下打印 `old-target-trace-named=398`、`newest-trace-named=2`。结论：前驱查询的**第一段**（最新→目标）随目标之后历史线性增长，不是“已消除的旧成本”；这两个计数是现状观测，未来实现改变时须按真实访问全成本迁移，不得删断言。

### D8.5 只读量测：首读/热读、采样、分配、档案保留

| 量 | 读数（原始输出） | 命令 |
|---|---|---|
| 开档完整性扫描 | `integrity pass verified 30400 byte(s) at 200 commits and 304000 at 2000, window 0 and 0` | 见 D8.6 命令 7 |
| 热读（段窗口点读） | `631.2 ns/op  730 B/op  12 allocs/op` | 见 D8.6 命令 8 |
| scoped 首读（全族装载） | 同一 `ensureLoaded`→`load` 面（`GetScoped`/`ReadSegmentScoped`/`BlobCount` 共用）：断言 `wholeSetLoads==1`、`wholeSetPairs==2×族条数`（24 代时 2×24＝48 条），读的前缀是 `blob:` 与 `ref:` | 见 D8.6 命令 9 |
| 身份/热读（提交路径点读） | 断言：每代 `identityReads==2`、`wholeSetLoads==0`，族按代增长作分母 | 同命令 9 |
| 单键提交（含 fsync） | 探针实测 127 次提交 525–593 ms（≈4.1–4.7 ms/次） | 见 D8.6 命令 3 |
| 阶段档案保留 | `passes` 每 pass ＋1；`passRecords` 每 pass ＋1 条（含两次 `time.Now()`），无界且无裁剪；唯一读者是测试 | 静态读码 `journal.go:996/1014–1020/2458` ＋ Grep 引用面 |

未归因项（不得记成已接受）：boot/total 墙钟、开档与首次读的分配峰值、`passRecords` 在长跑下的实际内存增长。它们归 06.105，本叶只给轴与已有读数。

### D8.6 H07–H11 逐项与命令凭据

| 项 | 结论 | 证据 |
|---|---|---|
| H07 | 部分证伪 | `compress_product.go:76` 哈希输入不含 text（静态读码，本叶直接读源码）；`compress_consumer.go:374–381` 的行内理由与之冲突。双接口各自的独立必要性**未证**，交 06.102/06.104 |
| H08 | 证实 | D8.1 三段实测（EXIT 1，10/10 同构） |
| H09 | 证实 | D8.4 两组探针读数（`18/66/258`；`398`）＋静态读码 `kv_original_store.go:93/142`、`reference.go:276/290` |
| H10 | 未证 | 读面已枚举（D8.2），但帧 CRC 不覆盖 absent/tombstone/槽链越界/枚举遗漏；等价证明为零，故保留全扫 CRC |
| H11 | 未证 | 指标对照两枚锚只证“端点独立、双计/漏计被拒”，不证成本闭合；`passRecords` 生产追加无界、读者仅测试；boot/total 未归因 |

必跑命令与原始终局（全部在 `/Users/pengweiye/Documents/codes/tagent`；日志落 `/tmp/06-101-*.log`）：

| # | 命令（`go -C <root> test …`） | exit | 命中 |
|---|---|---|---|
| 1 | `./assembly -run '^TestParentLookupCountsTheSearchBeforeTheTarget$' -count=10 -v` | 0 | RUN 10 / PASS 10 |
| 2 | `./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -v` | 0 | RUN 10 / PASS 10；7 子案×10＝70 条子 PASS |
| 3 | `./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10 -v`（新锚） | **1** | RUN 10 / PASS 0 / FAIL 10 |
| 4 | `./memory/ledger -run '^(TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast\|TestJournalReadViewLeavesTheTornTailOfTheWriter)$' -count=10 -v` | 0 | RUN 10/10 / PASS 10/10 |
| 5 | `./assembly -run '^(TestBootStageSamplesHaveIndependentEndpoints\|TestBootStageSamplesRejectDoubleChargeAndMissingSpan)$' -count=10 -v` | 0 | RUN 10/10 / PASS 10/10 |
| 6 | `./assembly -run '^TestRecallTraceThroughTierCountsOldTargetLookup$' -count=10 -v` | 0 | RUN 10 / PASS 10 |
| 7 | `./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=1 -v` | 0 | RUN 1 / PASS 1（读数见 D8.5） |
| 8 | `./memory/ledger -run '^$' -bench '^BenchmarkSegmentWindowRead$' -benchmem -benchtime=1s -count=1` | 0 | 1 个基准（读数见 D8.5） |
| 9 | `./memory/ledger -run '^(TestKVOriginalStoreDefersLoadUntilFirstRead\|TestKVOriginalStoreLoadReadsItsOwnEntries\|TestCommitReadsTheOriginalOfItsOwnIdentityAtEveryDepth)$' -count=1 -v` | 0 | RUN 3 / PASS 3 |

补记：`gofmt -l memory/ledger/journal_test.go` 无输出；`go vet ./memory/ledger` 无输出。命令 3 为取证叶的**主题红**（允许缺陷红，D5 第 1 条），不是噪声：十轮逐字同构、键名与错误文本固定。

### D8.7 未验面与交付去向

未验面（不得被下游当成已证）：
1. 真实 `Tier`/`Boot` 装配下的同一聚集（本叶只经 `journalKV` 面；`CommitFact` 的结局映射是静态读码 `segment_store.go:861–878`，未实跑）。
2. `file`/真实 rustviking 档的同一构造（本机无该资源）。
3. 并发/竞争下的同一路径（本叶未加 `-race`，见 06.103/06.105 已登记的 race 单列）。
4. boot/total 成本与 `passRecords` 实际增长（06.105）。

交付去向：06.102 拿 D8.2/D8.3 逐角色判必要性，且必须正面处理 H08（链界不是负载函数）；06.103 使本锚转绿是其硬门，且 `./memory/ledger` 全包回归在本锚红时不可能通过；06.105 取得全成本轴；12.102 接原文 scope 首读全族的共享事实（不可两边各写一版）；13.102 接能力/验证缺口（含“合法聚集不误报损坏”这一条尚无实现承接）。

## D9 · 06.102 方案比较记录

> 产生方式：**只读读码 ＋ `/tmp` 隔离原型 ＋ 本节记录**。产品 `.go` 零改动；写面＝本节＋D6 签发区＋`tasks.md` 的 06.102 指针行。未 commit/push/archive，未勾选 06.102。
> 执行根 `/Users/pengweiye/Documents/codes/tagent`；`go1.27.1 darwin/arm64`。原型全部经 `go test -overlay=<json>` 把 `/tmp/p06` 下的副本替换进包，仓库内文件字节不变（`git status --short -- memory/ assembly/` 为空，见 D9.8 末行）。

### D9.0 结论先行

| 案例 | 选择 | 一句话理由（读数见各小节） |
|---|---|---|
| 1 · H08 链界（已证缺陷） | **探测终止条件＝表本身**：上界 `used+1`，走满回既有唯一修复规则；删“链长即损坏”判词 | 129 同桶键由 10/10 红转 3/3 绿（raw exit 0），邻域 20 项不红；代价只落在该链上 |
| 2a · H09 前驱（已证成本） | **逐提交 order 行偏移索引**（8 B/提交）＋命中即定位；链距保留 | 第一段随目标之后历史（18/66/258）消失；被否三案见 D9.2 |
| 2b · H09 scoped 首读全族 | **按身份解析单条**（2 次点读），`ensureLoaded` 只留给 `BlobCount` | 24 代 48 对→0 对；行为差异（损坏作用域收窄）已具名 |
| 3 · H10 CRC 时机 | **保留全帧 CRC（默认案）**；不建替代锚 | 已找到替代案的反例：`value()` 不验帧 CRC |
| 4 · H07 Content | **保留 Content 字段**，只改被证伪的理由文字 | 恢复要正文；真实理由与“ID 含 text”无关 |
| 5 · H11 passRecords | **保留**（移出默认生产无净收益且有自证风险） | 增长轴是“维护 pass”（pass 不含提交路径，见 D9.5 澄清） |
| 6 · #2/#5/#6 原文读面 | **三面均保留**；删留执行权仍在 06.104，草案见 D9.6 | 行界语义无替代入口；`ErrScopeRequired` 唯一出口 |
| 7 · 无 tier/档位 | **不新增机制**，维持 06.105 档位矩阵 | 本叶不触碰档位与声明面 |

### D9.1 案例 1：H08 链界缺陷（已证）的候选比较

事实（D8.1，本叶复跑同构）：129 个同桶合法键在 12.5% 负载下第 129 个被 `ErrJournalCorrupt` 拒绝 → `ErrCommitUncertain`；同链合法缺失读报损坏；root 不可重开。机制：`probe`/`lookupOnce` 以 `journalSlotMaxProbe=128` 为**判词**而非性能上界，而负载规则只在 50% 处扩容，故“链长有界”只是分布经验。

线性探测的唯一健全终止条件：**从基址走到第一个空槽**（插入总落在首个空槽，故走到空槽即证明不存在）。合法表内最长链 ≤ `used`，因此上界取 `used+1` 是**紧的且可证**，不需要任何分布假设。

| 候选 | 字节布局/迁移 | 读写成本 | 与 CRC/水位交互 | H08 转绿路径 | 判 |
|---|---|---|---|---|---|
| A 上界＝表（本叶所选） | 无（记录、表头、`journalSlotVersion` 全不动）；无迁移 | 点读≤`used+1` 条记录（正常族实测 ≤12）；合法 129 簇：单键读 ≤129 条，簇内插入合计 ≈8.4k 条 | 不变；逐记录 CRC 反而更早命中坏记录→回重建 | 走满即 `errSlotUnplaceable`→`rebuildFrom`→重答；**合法簇根本不走到**，直接提交/读回 | **选** |
| B 扩链/溢出区（链指针或溢写区） | 记录或新文件布局改动＋`journalSlotVersion` 升级＋旧 root 重建 | 每次溢出跳一次块读；写入多一次指针维护 | 新结构要自带校验，否则坏指针即坏答案 | 合法簇可落溢出区 | 否：新布局、新修复面、新校验缺口，换来的只是把“无界走”变成“有界跳” |
| C 开档区分合法聚集/真损坏后“降级为可恢复事实” | 开档需扫表（`capacity×stride`）＋一个新事实通道 | 开档多一遍表；读路径仍受 128 判词约束 | 与 CRC 时机无关，但开档成本上升 | **不转绿**：读/缺失读仍是 128 判词 | 否：`GetParent`/读面不修，缺陷原样在 |
| D 判定改判：重建后重试 | 无 | 重建 O(日志) | 无 | 只在“重建扩容”时偶然转绿；mod 不变则重建出**同一聚集** | 否：概率性修法，无安全不变量 |
| E 探测失败即扩容 | 无（沿用 `grow`） | 提交路径多一次整表重写 | 无 | 只对“mod 不同即散开”的簇有效；哈希同位簇（FNV+splitmix 可构造）不受影响 | 否：引入提交路径状态变化而无不变量收益；且把合法提交变成“有时成功” |

全局复杂度差额：A 是**表达式级**改动（三处循环上界＋删一个被证伪的判词），并把“损坏”判词保留在**满表**这一真正不可能态上；B/C/E 各引入新结构或新通道，却都不消除“链长被当判词”的根因。记录内嵌/溢出还牵动转换器与版本面，A 一条也不需要。

### D9.2 案例 2：H09 两段成本的候选比较

**2a 前驱（`GetParent`）**。当前两段：①最新→目标（随目标**之后**历史）；②目标→同链前驱（＝链距，内在）。读数（06.101 D8.4，既有锚）：目标之后 16/64/256 → 18/66/258 条目；`recall_trace` 在 128 条更新下 `old-target-trace-named=398`。消费链：`tool/recall/trace.go:103–133` **每步一次** `GetParent`，故整条 trace 的代价是 `Σ(history_after)`——随历史二次增长，且正是原症状里的“可消除的历史重复工作”。

| 候选 | 第一段消除？ | 新结构/字节 | 读成本 | 判 |
|---|---|---|---|---|
| 逐提交 order 行偏移索引（所选） | 是：由目标 `seq` 一次读 8 字节偏移后直接定位该行 | 派生文件，8 B/提交（5 键/提交时约为槽表字节的 1/100） | 1 点读＋1 次 64 KiB 块读＋链距；与目标之后历史**无关** | **选** |
| 槽记录内嵌行偏移 | 是 | 记录头 20→28 字节；键位容量 76→68，触发 `fileOnce:1457–1465` 的 stride 倍增即**整表翻倍** | 0 额外 I/O | 否：把记录布局耦合到无界键长；1.3× 直接受威胁 |
| 先按身份索引取 partition/session 再走 | 否：只省分区分支，走查仍在 | 无 | 仍随历史 | 否 |
| 保留现状并声明 | 否 | 无 | 18/66/258 继续增长 | 否：H09 已证，“已声明”不代替消除 |

迁移与一致性：偏移文件与 order 文件**同一水位**（表头已声明 `Seq` 与 `OrderSize`），缺档/短档/长档一律回“从日志重建”；只读视图不写；`rebuild()` 截断 order 时同步截断偏移文件。可选项化：引擎/存储两侧各一个可选接口，未实现者回落现状走查——因此消除只落在 `journal`/`localfile` 两档（生产默认档），`memory`/`file` 档仍走原路，06.105 按档逐项报告，不得以 journal 档读数代全档。

**2b scoped 首读全族装载**。`GetScoped`/`ReadSegmentScoped` 经 `ensureLoaded`→`load` 读 `blob:`+`ref:` 两条前缀，代价＝2×族规模（24 代＝48 对）。但 `scopedRecall`/`scopedSegment` 只查 `byKey[key]` 一个键——全族装载是**实现形式的副作用**，不是语义需要。

| 候选 | 读数（本叶实测） | 判 |
|---|---|---|
| 按身份解析（所选） | `wholeSetLoads=0/wholeSetPairs=0/identityKeys=2`；同族第二个键再 +2；命中后 0 | **选** |
| 惰性按代装载 | 需引入代际定位与失效面；收益不高于直接点读 | 否 |
| 保留全族 | `wholeSetLoads=1/wholeSetPairs=48/identityKeys=0` | 否：与族规模同增长 |

**具名行为差异（必须随本叶一起审）**：全族装载失败会让**每个** scoped 读回 `RecallFailed`；按身份解析后只有该身份失败。实测：损坏 `0702` 的引用记录时，基线 `0701`＝`RecallFailed(4)` 且错误文本指向 `0702`，候选 `0701`＝`RecallFound(1)`、`0702`＝`RecallFailed(4)` 且仍为具名 `ErrSnapshotCorrupt`。该差异使“作用域内可见性”从**存储级**收窄为**身份级**，与 `ScopedOriginalStore` 契约“按身份判可见性”一致，但改变了损坏时的失败面，故列为 D6 第 5 行共享事实与第 10 行送审项。

### D9.3 案例 3：CRC 时机（H10 未证）

默认案＝**保留全帧 CRC**，理由不是“未及设计”，而是**替代案存在反例**：`journalAddress.value`（`journal.go:1804–1818`）只按 span 读值并 `json.Unmarshal`，不校验帧 CRC；把校验推到读时，一个仍可解码的坏值会静默返回。要等价替代，必须逐项给出证明（present/absent/tombstone、枚举遗漏、槽链越界、前驱、原文 digest/length、未访问坏帧、只读不截断、撕裂/零填充尾），并让每处读自行验帧——这是一套新的逐读校验结构，不是“换个时机”。故本叶不建 `TestRecoveryReadIntegrityMatchesFullVerification`（保持 0 命中），原两条 open 时机断言原样承接；`TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt` 亦原样承接（未访问坏帧不得以“尚未读取”免验）。

### D9.4 案例 4：H07 Content 字段

已驳回的只是“ID 含 text 故不可寻址”：`compress_product.go:76` 的哈希输入是 `request\0producer\0key`。**本门只裁字段去留**：**保留 `anchorEntry.Content`**。真实理由：`hydrateSurvivors`（`run.go:2961–2979`）对既非 mapped 又无正文的幸存者具名拒绝 `ErrCheckpointSurvivorUnstated`，恢复所需的折叠正文只在 checkpoint 里；同一正文在产物事实的 `compress_text` 元数据中也存在（`docs/wiki/memory/memory-architecture.md:107`），故 checkpoint 里这份是**启动状态的自带副本**，不是第二权威——去掉它就要为每个幸存者付一次按身份读＋解码，且新增“正文缺失”失败面。因此 `assembly/compress_consumer.go` 只需改写 `:378` 的注释理由，字段、载荷与行为不变；生产者定位面的完整枚举仍归 06.104（它必须逐生产者回答 request/producer/key 是否持有、产物是否已耐久）。

### D9.5 案例 5：H11 `passRecords`

**澄清 D8.5 的增长轴（不改写 D8 原文）**：`openPass` 只有三处生产调用——`rebuildFrom`（`journal.go:1254`）、`grow`（`:1662`）、`resetTable`（`:1718`，由 `rebuild` 调用），**提交路径不开 pass**。D8.5 现文“每 pass ＋1 条”中的 pass 即维护 pass；06.101 战报“生产追加/无界”按此口径理解（增长随维护次数而非提交数，扩容＝键数的对数级、重建＝修复时才发生；开档阶段用独立的 `integrityBegin/End`）。若 06.101 轮曾存在“每次提交一条”表述，以其战报口径差异记录在此，不追溯改写 D8。据此比较：

| 候选 | 收益 | 代价 | 判 |
|---|---|---|---|
| 保留（所选） | 阶段锚（`IntegrityPassSpan`/`DirectoryMaintenancePass(es)`）继续有真实来源 | 每次 pass 两次 `time.Now()`＋一条记录；量级为对数 | **选** |
| 移出默认生产 | 省掉的字节与时钟读可忽略 | 需要一个生产开关（06 D5 已否决“预设新增生产配置开关”）或测试后门（自证结构），且阶段锚失去真实来源 | 否 |

因此 06.104 的“把只有 test 读取的档案移出默认生产”在这一项上**结论为保留**，并以“关断仍可证伪增长”的独立锚（`TestRecoveryCostDetectsGrowthWithInstrumentationDisabled`）承接敏感度，不以关掉量得零成本。

### D9.6 案例 6：`#2`/`#5`/`#6` 原文读面的删留与等价锚草案

| 面 | 真实消费者（本叶核） | 结论 | 若日后改判删，等价锚迁址草案 |
|---|---|---|---|
| #2 `OriginalStore.ReadSegment`（行界） | `examples/contract/memory_ledger/OriginalStore_example.go:76`；`tests/integration/history/storage_capabilities_test.go:64/145`；`original_reference_isolation_test.go:124/127/208/214/229/233/236/239` | **保留**：`SegmentRequest` 是字节界，无法复现行界与 `ErrSegmentPastEnd`/`ErrSegmentRange` | 无（无替代入口，故不设删案） |
| #5/#6 `ScopedReader.Get/ReadSegment` | `NewScopedReader` 是 `ErrScopeRequired` 唯一具名拒绝点；`skeleton_assert.go:76/78` 钉方法集；`search_real_store_test.go:187/344/355`、`storage_capabilities_test.go:153/161`、`original_reference_isolation_test.go:160/167/175` | **保留**：scope 一次绑定＋零 scope 具名拒绝无替代 | 迁至 `ScopedOriginalStore.GetScoped/ReadSegmentScoped`（每调用自带 scope）；断言清单：字节原样、行/字节界、过期、损坏具名、scope 外答 missing 不泄漏、`ErrScopeRequired` 改由 `ReadScope.Validate` 断言；同窗口替换 `skeleton_assert.go` 两行；否则不得删 |

### D9.7 案例 7：无 tier/档位

本叶不新增任何档位、声明、配置字段或外部依赖；`memory`/`journal`/`localfile`/`file` 四档矩阵仍由 06.105 逐项运行并逐项报告（含 rustviking 缺失记未验）。D6 第 4 行的“可选接口”不是配置面：它按引擎实现能力分流，不改变任何档位的公开语义。

### D9.8 原型命令与原始终局

全部在 `/Users/pengweiye/Documents/codes/tagent` 执行；原型副本在 `/tmp/p06/`，经 `-overlay` 注入，仓库文件字节不变。日志 `/tmp/p06/*.log`（scratch，只作执行留档，关键行已抄入 D9.1/D9.2）。

| # | 命令 | raw exit | 命中/终局 | 读数 |
|---|---|---|---|---|
| 1 | `go test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=1`（基线，无管道） | **1** | RUN 1 / PASS 0 / FAIL 1 | 127/128 提交；缺失读报损坏；重开报损坏 |
| 2 | `go test -overlay=/tmp/p06/overlay.json ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=3 -v` | **0** | RUN 3 / PASS 3 / FAIL 0 | 记录读合计 51,096（≈17,032/轮） |
| 3 | `go test -overlay=/tmp/p06/overlay.json ./memory/ledger -run '^TestAddressable' -count=1 -v` | **0** | RUN 20（含 7 子案）/ 顶层 PASS 13 / FAIL 0 | 分布、修复、孤儿、载入等多个邻域全绿 |
| 4 | `go test -overlay=/tmp/p06/overlay.json ./memory/ledger -run '^(TestLegalClusteredKeysSurviveCommitAndReopen\|TestJournalOpenRefusesCorruptionBehindAStatedWatermark\|TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt\|TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast\|TestJournalReadViewLeavesTheTornTailOfTheWriter)$' -count=1 -v` | **0** | RUN 8（含 3 子案）/ PASS 8 / FAIL 0 | 安全与成本锚不受选择影响 |
| 5 | `go test -overlay=/tmp/p06/overlay-base-probe.json ./memory/ledger -run '^TestZZProbe' -count=1 -v`（scoped 基线） | **0** | PASS 2 | `wholeSetLoads=1 wholeSetPairs=48 identityKeys=0`；损坏时 0701 与 0702 同为 `RecallFailed` |
| 6 | `go test -overlay=/tmp/p06/overlay-ref-probe.json ./memory/ledger -run '^TestZZProbe' -count=1 -v`（scoped 候选） | **0** | PASS 2 | `wholeSetLoads=0 wholeSetPairs=0 identityKeys=2`；0701=`RecallFound`、0702=`RecallFailed` 具名 |
| 7 | `go test -overlay=/tmp/p06/overlay-ref.json ./memory/ledger -run '^(TestOriginalReadEntrypointsPreserveBytesAndScope\|TestKVOriginalStoreDefersLoadUntilFirstRead\|TestKVOriginalStoreLazyLoadRetriesAfterRepair\|TestKVOriginalStoreLoadReadsItsOwnEntries\|TestCommitReadsTheOriginalOfItsOwnIdentityAtEveryDepth\|TestOriginalStoreCommitsBodyAndReferenceInOneBatch)$' -count=1 -v` | **0** | RUN 19（含 13 子案）/ 顶层 PASS 6 / FAIL 0 | 含“引用无正文→具名损坏快照”“过期/缺失/篡改字节”各子案 |
| 8 | `go vet -overlay=<各自 json> ./memory/ledger` | **0** | 无输出 | 原型可编译 |
| 9 | `git status --short -- memory/ assembly/ api/ tests/ scripts/` | — | 空 | 产品/测试/门零改动 |

注 1：基线命令未经管道，`go test` 自身 raw exit 为 1（`FAIL`），与 D8.1 十轮同构；本叶只复跑一轮以确认环境未漂移（本叶不重跑 06.101 的 10 轮凭据）。
注 2：命令 5/6 的探针文件与命令 3/4 的注入副本都在 `/tmp/p06`，不入库；探针名 `TestZZProbe*` 仅为原型脚手架，**不得**作为 06.103 的验收锚。
注 3：本树下 `git rev-parse` 返回 `fatal: not a git repository`（与 12.101 D7.0-2 同一判定），故写面自查改用文件面：`date` 记本叶收尾时刻后跑 `find . -name '*.go' -newermt '<该时刻>' | head` 为空，`ls -lT` 只列出 `design.md`/`tasks.md` 两个规划文件被本叶改写；产品、测试、门、`examples/`、`tests/integration/` 零字节改动。

### D9.9 未验面与移交

1. **前驱偏移索引无原型**：本叶只做读数轴推导与代码面界定（`journal.go` 的 order 写/截断/重建、`segment_store.go` 的走查入口、`assembly/read.go` 的两条查询）。其真实读数（`TestPredecessorQueryBoundsTheWalkToTheChain`）尚未建立，属 06.103 的硬门。
2. **合法聚集的真实装配面未验**：原型仍经 `journalKV` 面；`CommitFact` 对合法聚集的终局映射未实跑（沿用 D8.7 第 1 条）。
3. **档位未验**：`file`（rustviking 缺失）、并发/race 单列属 06.105。
4. **未消成本**：开档全帧 CRC 的 O(日志字节) 仍在（D9.3），06.105 必须单列并参与 1.3×/boot/total 判决。
5. **跨单元**：D6 第 5 行的四项共享事实须与 12.102 会签同一记录；13.102 需接“合法聚集不得误报损坏”这一能力缺口（尚无实现承接，现由 06.103 承接）。

交付去向：06.103 按 D6 第 3 行子集实施并以命令 2/4/7 的迁移后形态取证；06.104 只改 `compress_consumer.go:378` 的注释并按 D9.5/D9.6 记录保留结论；06.105 取全成本轴与档位矩阵；06.106 按 D6 第 10 行逐性质复现。

## D10 · 06.103 执行记录

> 产生方式：一次实施会话。执行根 `/Users/pengweiye/Documents/codes/tagent`；工具 `go1.27.1 darwin/arm64`，命令一律 `env GOFLAGS=-buildvcs=false go …`（本树 `.git` 不可读）。写面＝D6 第 3 行的 06.103 子集＋同叶所列测试镜像＋本节＋`tasks.md` 的 06.103 指针；未 git 写，未勾选 06.103。
> **结果：H08 转绿（10/10，`-race`）、H09 前驱第一段消失（16/64/256 → 2/2/2）、H09 scoped 首读降为 2 次身份点读（`wholeSetLoads=0`）。三案均有变异红＋控制绿。** 与 D6 的文字有一处结构性偏离（见 D10.6 ①），须由 06.102/06.106 复核。

### D10.1 七案落点（文件:行，实施后行号）

| 案 | 落点 | 说明 |
|---|---|---|
| 1 · H08 探测终止＝表本身 | `journal.go:1642` `probe`（上界 `used+1`，走满回 `errSlotUnplaceable`）、`journal.go:1766` `findRecord`（`lookupOnce` 与之共用的同一条链走查）、`journal.go:2502` `stageProbe`（上界 `capacity`）；常量 `journalSlotMaxProbe` 已删，`journalSlotLoadPercent` 注释改为"成本规则而非正确性规则" | `file`/`lookup`/`orderLineOf` 的 `errSlotUnplaceable` 修复分支未改，仍是"从 order 重建"这一条 |
| 2a · H09 前驱进入点 | `journal.go:97` `journalOrderLineFileName="order.line"`＋`:102` 未知字；写 `:1335` `writeLineOffset`（`fileCommit→file→fileOnce:1600`）；读 `:1320` `readLineOffset`；尺寸 `:1280` `linesState`（`capacity×8`，不符即 `rebuildFrom("open")`）；重建 `:1293` `resizeLines`（`resetTable` 调用）；随表搬移 `:1937` `stageLines` / `:1980` `replaceLines`（`grow` 调用）；seek `:1703` `orderLineOf` / `:1716` `orderLineOnce`；入口 `:2235` `eachNewestFirstAt` / `:2355` `eachNewestFirstAtValue` | 引擎侧可选能力 `journal_kv.go:305` `commitOrderSeek`（`:316` `OrderLineEnd`、`:332` `EachCommittedNewestFirstFrom`）；存储侧 `segment_store.go:1218` `EachIndexNewestFirstFrom`（`:1242` `indexEntrySeek`、`:1332` `walkIndexValues` 为两条走法共用体）；消费 `assembly/read.go:277` `GetParent`、`:332` `parentFromBodies` |
| 2b · H09 scoped 按身份 | `kv_original_store.go:143` `scopedOnlyLocked`（走既有 `entryLocked` 的两次点读）；`reference.go:281` `GetScoped`、`:297` `ReadSegmentScoped` 去掉 `ensureLoaded`，改走单条；`ensureLoaded` 只留 `BlobCount`（`kv_original_store.go:428`） | 作用域收窄为身份级，D6 第 5 行④ 已由 12 域 D8.7.1 第 6 条会签 |
| 3 · H10 CRC | 未动 | D9.3 保留案；原安全/成本双锚原样执行并绿 |
| 4 · H07 Content | 未动（属 06.104） | 本叶写面不含 |
| 5 · H11 `passRecords` | 未动 | 同上 |
| 6 · `#2`/`#5`/`#6` | 未动（保留） | 同上 |
| 7 · 无 tier/档位 | 未新增档位、配置字段或依赖 | `commitOrderSeek` 按引擎能力分流，不是配置面 |

同叶测试镜像：`journal_test.go:1106` `TestAddressableIndexRefusesATableWithNoEmptySlot`（拟新增，负锚）、`journal_test.go:1157` `TestOrderLineIndexFollowsTheTableItParallels`（拟新增，偏移文件随表搬移/重开）、`kv_original_store_test.go:794` `TestScopedReadResolvesTheNamedIdentity`（拟新增，D6 第 6 行）、`assembly/read_test.go:680` `TestPredecessorQueryBoundsTheWalkToTheChain`（由 `TestParentLookupCountsTheSearchBeforeTheTarget` 迁移改名，D6 第 9 行②）、`assembly/read_test.go:763` `TestHistoricalReadsAvoidUnrelatedHistory`（拟新增，本叶派单要求）；`TestRecallTraceThroughTierCountsOldTargetLookup` 的 `398` 迁为 `5`（同一条迁移条款）。

### D10.2 H08 红→绿

| 轮次 | 命令（`./memory/ledger`） | raw exit | 终局 |
|---|---|---|---|
| 改前基线 | `-run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=1` | **1** | `committed 127 of the 128`；`holds no free slot within 128`；重开报损坏（与 D8.1 逐字同构） |
| 改后 | 同上 | **0** | `committed 128 of the 128 held key(s) … in 546ms`；重开 2.7ms |
| 硬门 | 同签名 `-race -count=10 -v` | **0** | `=== RUN` 10 / `--- PASS` 10 / `--- FAIL` 0 |

代价（同尺，只落该链）：单键读 ≤ `used+1` 条槽记录；129 同桶簇单键读 ≤129 条（96 B/条 ≈ 12 KiB），提交侧每条键 1 条记录不变。正常族未复测分布（`TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily` 的 `longest > 40` 自有字面界保留、绿）。

### D10.3 H09 前驱与 scoped 的读数

`./assembly -run '^TestPredecessorQueryBoundsTheWalkToTheChain$' -count=1 -v`（raw exit 0）：

| 目标之后历史 | 改前（D8.4） | 改后 seek 面 | 无偏移文件的控制面 |
|---|---|---|---|
| 16 | 18 | **2** | 18 |
| 64 | 66 | **2** | 66 |
| 256 | 258 | **2** | 258 |

`TestRecallTraceThroughTierCountsOldTargetLookup`：`old-target-trace-named` 398 → **5**（三步各进自己的提交：2+2+1），`newest-trace-named` 仍 2。`TestParentLookupCostsItsOwnChainNotTheHistory`：bounded 3/3/3（原 3/3/3）、control 401/1601/6401（历史扫描负控仍在）。

`./memory/ledger -run '^TestScopedReadResolvesTheNamedIdentity$' -count=1 -v`：24 代族下首读 `wholeSetLoads=0 / wholeSetPairs=0 / identityKeys=2`，热读 0，scope 外答 `RecallMissing` 且仍只 2 键；`BlobCount` 仍驱动全族装载一次（`loads=1 / pairs=48`），故零是"被测到的零"。`TestOriginalReadEntrypointsPreserveBytesAndScope -race -count=10`：顶层 10 PASS、子案 70 PASS，终局 0——字节/行界/scope/过期/损坏判据未缩。

### D10.4 偏移文件的重建路径（实测）

`TestHistoricalReadsAvoidUnrelatedHistory`（`./assembly`，`-count=10` → raw exit 0，RUN 10 / PASS 10）末段：关闭写视图 → `rm kv/order.line` → 重开写视图 → 该文件被重新写出（`size > 0`）且同一前驱查询仍答 `chain-mid` 且读数仍为 **2**；`TestOrderLineIndexFollowsTheTableItParallels` 覆盖表扩容时的字搬移与重开后的同一 seek。只读视图不写：`TestPredecessorQueryBoundsTheWalkToTheChain` 的控制面在读完后再核"文件仍不存在"，`TestJournalReadViewWritesNothingBesideTheWriter -race -count=10` 仍 10/10（其 `journalTestRootState` 覆盖整目录，含新文件）。

### D10.5 负变异（`-overlay` 注入 `/tmp/rr-06-103/mut/`，仓库字节不变）

| 组 | 变异 | 被咬锚（红） | 控制（未变异同名锚） |
|---|---|---|---|
| 1 · H08 | `probe` 恢复 `step < 128` 且走满回 `ErrJournalCorrupt`（即"链长即损坏"判词） | `TestLegalClusteredKeysSurviveCommitAndReopen`：`1 of the 128 … were refused … holds no free slot within 128`（与改前逐字同构） | 同签名 `-race -count=10` 10/10 绿 |
| 2 · 偏移文件 | (a) `linesState` 只判"句柄非空"（丢失的索引被信任） | `TestHistoricalReadsAvoidUnrelatedHistory`：重开段 `… read the order-line index: EOF`，前驱答空 | 未变异副本 10/10 绿；(b) `stageLines` 不搬字 → `TestOrderLineIndexFollowsTheTableItParallels` 红（seek 落空） |
| 3 · scoped | `GetScoped`/`ReadSegmentScoped` 恢复 `ensureLoaded()`＋全族 `byKey` | `TestScopedReadResolvesTheNamedIdentity`：`the first scoped read loaded the whole original family 1 time(s)` | 未变异副本绿；**`TestOriginalReadEntrypointsPreserveBytesAndScope` 在该变异下仍绿**（它不含装载断言），故按派单"用 D8/D9 记录的 `wholeSetLoads` 观测面"改由新锚承接 |

### D10.6 偏离与自行决策

1. **偏移文件的键由"提交"改为"槽"（须复核）**。D6 第 4 行写"每提交 8 字节…按 seq 取行偏移"，但读路径上没有任何地方能把一个键换成交付 seq：槽记录布局冻结（无 seq 字段）、`indexPosition.commit` 是存储层自己的提交计数器而非引擎 order seq、按 seq 做二分要走 O(log 总史) 次行读（与"读数不随目标之后历史增长"不符）。因此本叶按**槽**键控：`k×8` 字节（k＝表容量），一个字＝"最后写入该槽键的那次提交的 order 行尾字节"。后果：文件为槽表的 1/12（D6 估的是 1/100），但**容量轴**（活键集）而非**历史轴**（总提交数）增长，且与槽表共用同一水位、同一重建规则、同一次 `grow` 搬移——不需要第二个水位字段，表头字节不变；槽记录、`journalSlotVersion`、四契约与五值语义一字未动。若 06.102/06.106 判定必须按提交键控，则须先给出 O(1) 的键→提交来源，否则该项回到设计门。
2. **引擎侧能力形态**：`commitOrderSeek{OrderLineEnd(key)→byte, EachCommittedNewestFirstFrom(byte)}`，即 D6"按 seq 取行偏移"＋"从目标条目起走"的同一分工，但入口参数是取到的字节（与第 1 条同因）。未实现者（`memory`/`file` 档、只读视图无该文件）全部回落原走查，`EachIndexNewestFirstFrom` 内部保证。
3. **普通提交序走查不使用该文件**（仍从 `a.orderSize` 起）。D6/D9 预期"偏移文件不重建 → 序锚红"，本实现下该变异落在前驱锚（D10.5 组 2）上，`TestEachIndexNewestFirstStreamsTheCommitOrder` 不受影响——已在本节申报，不改变该锚仍绿的事实。
4. **改名**：`TestParentLookupCountsTheSearchBeforeTheTarget` → `TestPredecessorQueryBoundsTheWalkToTheChain`（D6 第 9 行②指名）。06.101 任务行与 `task-graph.json` 里记录的旧命令因此不再命中（非零匹配变为零匹配），属历史记录不动、后继指针在此。
5. **越界自查**：本轮只改 D6 第 3 行的 06.103 子集＋同叶五个测试镜像＋本文件＋`tasks.md` 一行指针。未碰 `api/event/**`、`atomic_store.go`、`rustviking_kv.go`、`original_store.go`、`skeleton_assert.go`、`scripts/fold-cold-root/**`、`examples/**`、`tests/integration/**`、CI、主规格、权限、git 索引/refs。`gofmt -l` 九文件空；`go vet ./memory/ledger ./assembly` 无输出；`python3 scripts/gates/check_concept_budget.py` → `RESULT: ok`（exit 0）。

### D10.7 命令与终局（全部在上述根执行）

| # | 命令 | raw exit | 终局 |
|---|---|---|---|
| 1 | `./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -race -count=10 -v` | **0** | RUN 10 / PASS 10 |
| 2 | `./memory/ledger -run '^(TestFactCommitAnswersNewExistingConflictUncertain\|TestEachIndexNewestFirstStreamsTheCommitOrder)$' -race -count=10 -v` | **0** | RUN 20 / PASS 20 |
| 3 | `./assembly -run '^TestHistoricalReadsAvoidUnrelatedHistory$' -count=10 -v` | **0** | RUN 10 / PASS 10 |
| 4 | `./memory/ledger -run '^(TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast\|TestJournalReadViewWritesNothingBesideTheWriter\|TestUncertainFactCommitRequiresAuthoritativeRead)$' -race -count=10 -v` | **0** | RUN 100（含子案）/ 顶层 PASS 30 |
| 5 | `./assembly -run '^(TestParentLookupKeepsSessionIsolation\|TestTierQueryHonorsScopeBeforeDecoding\|TestReadCorruptionNamedError)$' -race -count=10 -v` | **0** | RUN 30 / PASS 30 |
| 6 | `./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -race -count=10 -v` | **0** | 顶层 10 PASS ＋ 子案 70 PASS |
| 7 | `./memory/ledger -run '^TestJournalOpenRefusesCorruptionBehindAStatedWatermark$' -race -count=10 -v` | **0** | RUN 10 / PASS 10（原时点未改） |
| 8 | `./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v` | **0** | RUN 10 / PASS 10；`integrity pass verified 30400 byte(s) at 200 commits and 304000 at 2000, window 0 and 0` |
| 9 | `./assembly -run '^TestPredecessorQueryBoundsTheWalkToTheChain$' -count=1 -v` | **0** | 2/2/2 与控制 18/66/258 |
| 10 | `./memory/ledger -run '^(TestAddressableIndexRefusesATableWithNoEmptySlot\|TestOrderLineIndexFollowsTheTableItParallels\|TestScopedReadResolvesTheNamedIdentity)$' -count=1 -v` | **0** | 3 名全 PASS |
| 11 | `./memory/ledger ./assembly ./scripts/fold-cold-root -count=1` | **1** | `memory/ledger` 与 `scripts/fold-cold-root` 绿；`assembly` 唯一红＝`TestBackflowFailureRecoversWithoutManualSettle`（12.104 范围，派单已预登记），无其它红 |
| 12 | 同上加 `./tool/recall` | **1** | 同上，`tool/recall` 绿 |
| 13 | `gofmt -l`（九文件）/ `go vet ./memory/ledger ./assembly` / `check_concept_budget.py` | 0 / 0 / **0** | 空 / 无输出 / `RESULT: ok` |

### D10.8 未验面（不得被下游当成已证）

1. `file`（rustviking 缺失）与 `memory` 档：本叶只保证回落原走查；档位矩阵仍归 06.105。
2. 真实 `Tier/Boot` 装配下的大聚集并发（本叶 `-race` 只覆盖上述锚，未做风暴测试）。
3. 偏移文件在**并发写者**下的搬移（一格一写者已由根写锁保证，未做多进程实测）。
4. 原 1.3×/boot/total 判决未重跑：偏移文件把 `capacity×8` 加进开档/索引字节分母，`TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt` 的字节读数未变（仍是日志字节），06.105 按同轴判。
5. 本叶未勾选：是否完成由编排者按本节与 D10.5 的变异证据核。

## D11 · 06.104 执行记录

> 产生方式：一次实施会话。执行根 `/Users/pengweiye/Documents/codes/tagent`；工具 `go1.27.1 darwin/arm64`，命令一律 `env GOFLAGS=-buildvcs=false go …`。写面＝`assembly/compress_consumer.go`（仅 `anchorEntry` 注释一行）、`assembly/compress_checkpoint_test.go`、`assembly/symptom_anchor_longrun_test.go`、本节、`tasks.md` 06.104 指针；`memory/ledger/journal.go`、`api/event/**`、`#2`/`#5`/`#6` 三面与 `passRecords` 零改动；未 git 写，未勾选 06.104。
> **结果：两枚新锚建立且各 10/10 绿；`anchorEntry.Content` 的保留理由改写为“恢复自带正文副本”；D9.4/D9.5/D9.6 的保留裁定照办，无代码动作。** 相邻回归的红面（history 2、capabilities 1、host 4，共 7 项）经隔离回退归因为本树两份未提交生产文件，不属本叶写面（见 D11.5）。

### D11.1 产物生产者/恢复者逐项与锚内断言

| 角色 | 落点（实施后行号） | 锚内断言 |
|---|---|---|
| 产物定位身份 | `api/event/compress_product.go:74–83` `CompressProductFor`（哈希输入 request␀producer␀key） | 20/20 产物的定位字段重算 ID 等于其事件键；正文不参与地址 |
| 产物事实承载定位 | `assembly/compress_consumer.go:636–660` `compressProductEvent` | 事实必须陈述 request/producer/key，缺一即红；text/covered 经回读往返一致 |
| 折叠期正文（生产者） | `compress_consumer.go:398–421` `anchorCheckpointOf`＋`:387–396` `carriedTextOf` | 折叠产物条目必须携正文、mapped 条目必须不携（两分支各断一次） |
| 检查点条目（生产者） | `assembly/checkpoint.go:1408–1427` `anchorEntriesOf`，经真实 `Run.Checkpoint` 落盘 | 同上，走真实框架检查点路径 |
| 执行前定位探针 | `compress_consumer.go:564–577` `holdsCompressProduct` | 已归档判断答 true；链上从未归档答 false 且无错；不携正文、不调模型 |
| 产物回读（恢复者） | `compress_consumer.go:667–685` `compressProductOf` | 逐字段往返；缺 key、缺 request、身份冲突三例各具名拒绝 |
| 锚定恢复正文 | `assembly/run.go:2925–2954` `restoreSurvivors`＋`:2967–2987` `hydrateSurvivors` | 真实 `seedAnchoredReplay`：`Anchors≥1`、携正文幸存者逐字节相等、模型 0 次 |
| 检查点恢复正文 | `checkpoint.go:1356–1383` `restoreProjectionSlice`，经真实 Boot | 引导后投影对携正文幸存者逐字节相等；6 个 mapped 幸存者回读链上正文相等 |
| 恢复不重调模型 | `compress_consumer.go:354–370` `summarizeProduct` 是唯一 gist 面 | 生产期模型 10 次；引导 0 次、锚定重建 0 次；链上产物数引导前后相等 |

### D11.2 两锚定义

- `TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel`（`assembly/compress_checkpoint_test.go`）：正例＝上表九个角色；负例＝缺 key、缺 request、身份冲突、未陈述幸存者，各具名拒绝；守护＝生产期模型调用非零、携正文幸存者非零，否则整套断言不作数。
- `TestRecoveryCostDetectsGrowthWithInstrumentationDisabled`（`assembly/symptom_anchor_longrun_test.go`）：`unmeteredJournal` 视图把引擎自报计量全置零（`IntegrityBytesScanned`/`SlotTableBytesScanned`/`ValueReads` 为 0，`IntegrityPassSpan` 为空对）＝等价关断态；独立量＝视图在引擎边界逐阶段计的真实调用（读次数、读回 pair、读回字节、枚举次数、枚举键数、序走查条目与字节、提交批与 pair）；`repeat=1` 时每次读取在引擎上真实重复一遍而答案只交一次，故重复工作是物理的，不靠指标声明。

### D11.3 命令与终局（全部在上述根执行）

| # | 命令 | raw exit | 终局 |
|---|---|---|---|
| 1 | `./assembly -run '^TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel$' -count=10 -v` | **0** | RUN 10 / PASS 10 / FAIL 0；每轮 20 产物定位、7 幸存者（1 携正文）、6 mapped 回读、模型生产 10 次、三类事实负例＋未陈述幸存者具名拒绝 |
| 2 | `./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v` | **0** | RUN 10 / PASS 10 / FAIL 0；每轮最深引导读 31 值、走查 28 条目；同一日志两次引导相等；重复读引导 64 读 对 32 读被具名拒绝 |
| 3 | `./assembly -run '^(TestCompressionSummaryReplaysTheRecordedGist\|TestCompressionSummaryCutsToTheDeclaredBudget)$' -count=10 -v` | **0** | RUN 20 / PASS 20 |
| 4 | `./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -v` | **0** | RUN 80（含子案）/ 顶层 PASS 10 / FAIL 0 |
| 5 | `./tests/integration/host -run '^TestCompressProductRestoresAfterRestart$' -count=10 -v` | **0** | RUN 10 / PASS 10 |
| 6 | `./tests/integration/capabilities -run '^(TestRecallToolSurface\|TestSearchRecallsFromRealStore)$' -count=10 -v` | **0** | RUN 180（含子案）/ 顶层 PASS 20 |
| 7 | `./api/event ./memory/replay ./memory/ledger ./assembly ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host -count=1` | **1** | 前四包 `ok`（0.7/0.5/165.8/472.5 s，含本叶两锚）；7 红全在 history/capabilities/host，归因见 D11.5 |
| 8 | `gofmt -l`（三文件）/ `go vet ./assembly` | 空 / 无输出 | 0 / 0 |

### D11.4 负变异（`-overlay` 注入 `/tmp/rr-06-104/mut/`，仓库字节不变）

| 组 | 变异 | 被咬锚（红） | 控制 |
|---|---|---|---|
| 1a | `carriedTextOf` 恒答空（检查点不再携正文） | 锚1 EXIT 1：`the checkpoint names the fold product -hist-… carrying no text, want the text a restore states it from` | 未变异副本 EXIT 0 |
| 1b | `compressProductEvent` 不再陈述 `compress_request` | 锚1 EXIT 1：`the fold filed no product, so no locating below is a claim about one` | 同上 |
| 2 | `unmeteredBoot` 不再计独立量，只剩引擎计量（关断态全零，即“关断后无感”缺陷形态） | 锚2 EXIT 1：`the judgement accepted a boot that read the log twice beside every measure switched off, want it refused by name` | 未变异副本 EXIT 0 |

三组变异副本在 `/tmp/rr-06-104/mut/`，overlay JSON 在同级；变异后 `find . -name '*.go' -newermt '<末次编辑时刻>'` 为空，仓库字节未变。

### D11.5 相邻回归 7 红的归因（不属本叶写面）

`history` 与 `capabilities` 两包未加 `-v`，7 条 `--- FAIL` 与三包 `FAIL` 行交错输出；按测试定义归属：history＝`TestEventIntegrityConsumptionRecovery`、`TestEventIntegrityRestoreUse`；capabilities＝`TestGovernanceRecovery`；host＝`TestCommittedReentryIsNotRetroactivelyDenied`、`TestRequestFidelity`、`TestReenteredTaskServesAfterRestart`、`TestReactivatedSettleFiresOnce`。逐轮回退（`-overlay` 换入 `git show HEAD:` 字节，同签名复跑）：

| 回退面 | 结果 |
|---|---|
| 本叶三文件→HEAD（另加 `assembly/facts.go`→HEAD） | 同 7 红（host 4 复现） |
| 06.103 生产面（`memory/ledger` 五文件＋`assembly/read.go`）＋`facts.go` | 同 7 红（host 4 复现） |
| 全部 19 个未提交生产文件→HEAD | **0 红** |
| 仅 `runtime/agent/loop.go`＋`features/evolution/governance/goal.go`→HEAD | **0 红**（history 2.1 s、capabilities 0.4 s、host 10.7 s 全绿） |

结论：7 红由这两份未提交生产文件的改动引起，落在 12 域与 loop 面；本叶不动他人文件、不缩验收面，交由所属叶与 06.105 复核。本叶写面对该包的编译影响为零——唯一生产改动是注释行，两个测试文件不进库。

### D11.6 偏离与自行决策

1. **注释只改一行**：`compress_consumer.go:378` 原文“键由 material 与 text 共同内容寻址”被证伪（`compress_product.go:76` 的哈希输入是 request␀producer␀key），改写为“产物事实把该正文存在 `CompressProductFor` 由 request、producer、key 铸出的身份下，读取该条目键到不了它，故恢复由载荷自带正文陈述”；并补“该正文是 age 界已持有的有界摘要，检查点携带的是启动状态的副本而非第二权威”。字段、载荷与行为零变化。
2. **两锚的辅助件都落在各自测试文件内**（`locatingChainRun`；`unmeteredJournal`/`unmeteredBoot`/`judges`），不新建文件、不改他人测试文件，避免扩大 D6 第 3 行之外的写面。
3. **不复用 `repeatWorkStore`**：它按步数做纯算术重复，独立计量看不到；且其 `IntegrityBytesScanned` 等透传真实计数，构不成关断态。改为“在引擎边界计数＋真实重复读取”。
4. **锚2 以同一日志的两次引导作判据不空转的证据**（control 对 mirror 必须相等），不跨根比较：两根本身的派生文件重建差异（D10.4）会让跨根相等不成立。
5. **相邻回归红面用 `-overlay` 回退 HEAD 归因**，不写他人文件、不改 git。
6. 落点与 D6 第 3 行一致：`#2`/`#5`/`#6` 三面保留、`passRecords` 保留，本叶对它们零改动。

### D11.7 未验面（不得被下游当成已证）

1. 本叶只建立“关断态仍可证伪增长”的敏感度锚；06.105 的冷热、深浅、档位（`file`/rustviking 缺资源）与 1.3×/boot/total 判决仍未跑。
2. 锚2 的重复读取是单进程内的实读重复，不含并发写者与多进程；race/风暴仍归 06.105。
3. 相邻回归 7 红在写面外，本叶未修、未屏蔽、未缩面；其归属与修复不属本叶。
4. 本叶未勾选：是否完成由编排者按本节与 D11.4 的变异证据核。

## D12 · 06.105 成本与等价回归记录

> 产生方式：一次验证会话（2026-11-21 23:25 – 2026-11-22 00:10）。执行根 `/Users/pengweiye/Documents/codes/tagent`；源码身份 `7b0cde162627ad37f1d364bbf3508b3bd0518431`（工作树 HEAD，未提交改动仍在）；工具 `go1.27.1 darwin/arm64`；命令一律 `env GOFLAGS=-buildvcs=false go test …`，全部串行，无本单并发命令。scratch `/tmp/rr-06-105/`（逐锚原始日志 `*.log`、汇总 `matrix.tsv`、runner `run.sh`）。写面＝本节与 `tasks.md` 06.105 行记录；未改产品、测试、门、主规格，未 git 写，未勾选 06.105。
> **结果：16 组锚共 310 次顶层执行，15 组 exit 0（309 次绿）；1 组 exit 1**——`A1-cold` 第 1 轮 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` 的**阶段墙钟**判决拒绝（commits/settling/sessions/frame/closing 五阶段同时 1.58–1.68×），同配置复跑 10/10 绿，**未复现**；该轮的**计数面**（读值、枚举、字节）全部通过，见 D12.4。**1.3× 判决含 boot/total 未闭合**（boot 墙钟在 depth 252 实测 2.0–4.7× 参照深度，全日志 CRC 保留所致）；`file`（rustviking）档为具名 skip，未验；`localfile` 别名在 `TestStorageCapabilities` 内无子案，改由 `memory/ledger` 引擎矩阵承接。

### D12.0 结论先行

| 目标面 | 终局 | 依据 |
|---|---|---|
| 真实 Tier/recall_get/trace/查询的字节、原序、scope、缺失/损坏 | **成立** | A7a/A7b/A7c/A10 全绿；A7a 中 `file` 档 skip 具名，其余五子案 10/10 |
| 转换干跑与独立重开（全键全事实）、源变/部分目标拒绝 | **成立** | A8 三锚 30/30；`scripts/fold-cold-root` 真实 CLI 入口 |
| 前驱/原文范围的历史重复工作已消 | **成立** | B1 前驱 2/2/2（目标之后 16/64/256）、控制走查仍 18/66/258、`recall_trace` 5（原 398）、root 查询 401/1601/6401 |
| 完整性保留方案（全帧 CRC）安全与成本等价 | **成立** | A6 三安全负锚 `-race` 30/30；A5 10/10，校验字节 30400@200、304000@2000，回放窗口 0/0（D6 第 8 行原样承接，未缩） |
| 档位覆盖 | **部分成立** | memory/journal 绿；`localfile` 别名经引擎矩阵绿；`file` 真 rustviking **未验** |
| 冷/热、深/浅分列 | **成立**（读数见 D12.3） | A1-warm 与 A1-cold 同命令分列；冷编译 9525 ms |
| 1.3× 判决含 boot/total | **未闭合** | boot 墙钟随日志字节增长 2.0–4.7×；total 墙钟不在断言面内；一次阶段拒绝未复现（D12.4） |
| 相邻回归 0 FAIL | **成立** | REG：12 包 ok、0 `--- FAIL`、无 `cached`、412 s |
| 既有 flake 定性 | **成立**（定性入 D12.5） | 串行单跑 2/5 红，签名与 12 域 D13.6-① 逐字同构 |

### D12.1 逐锚矩阵（命令在工作树根执行，参数原样）

| 组 | 命令（`env GOFLAGS=-buildvcs=false go test …`） | raw exit | RUN/顶层 PASS/FAIL | 耗时 | 关键读数 |
|---|---|---|---|---|---|
| A1-warm | `./assembly -run '^(TestSymptomAnchorBootCostFlatAcrossGenerations\|TestSymptomAnchorPerGenerationCostFlatOverALongRun)$' -count=10 -v` | **0** | 20 / 20 / 0 | 390 s | 10 轮 `held p10` 2.221–4.270 对 `control p90` 1.192–1.573；读数见 D12.3 |
| A1-cold | 同上，另加 `GOCACHE=/tmp/rr-06-105/gocache-cold GOTMPDIR=… TMPDIR=/tmp/rr-06-105/tmp` | **1** | 20 / 19 / **1** | 428 s | 第 1 轮阶段拒绝（D12.4）；`held p10 2.025 > control p90 1.560` |
| C1-cold-repeat | `./assembly -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=10 -v`，同 C 条件、独立新 `GOCACHE` | **0** | 10 / 10 / 0 | 329 s | `held p10 2.377 > control p90 1.559`；未复现 |
| A2 | `./assembly -run '^(TestBootStageSamplesHaveIndependentEndpoints\|TestBootStageSamplesRejectDoubleChargeAndMissingSpan\|TestBootWallClockRejectsUnattributedGrowth)$' -count=10 -v` | **0** | 30 / 30 / 0 | 121 s | 端点独立、双计/漏计具名拒绝；阶段自报 boot 16.50 ms、rebind 2.04 µs、integrity 32.71 µs、commit 18.58 ms |
| A3 | `./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v` | **0** | 10 / 10 / 0 | 4 s | 关断态最深引导读 31 值、走查 28 条目；重复轮 64 读 vs 控制 32 读被具名拒绝；漏阶段被具名拒绝 |
| A6 | `./memory/ledger -run '^(TestJournalOpenRefusesCorruptionBehindAStatedWatermark\|TestJournalReadViewLeavesTheTornTailOfTheWriter\|TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast)$' -race -count=10 -v` | **0** | 60 / 30 / 0 | 4 s | 水位后损坏两 open 均当场 `ErrJournalCorrupt`；只读不截断；零填充尾具名 torn 并拒绝越读（合并"校验保留方案"首锚，不重复执行） |
| A5 | `./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v` | **0** | 10 / 10 / 0 | 105 s | `integrity pass verified 30400 byte(s) at 200 commits and 304000 at 2000, window 0 and 0` ×10 |
| A7a | `./tests/integration/history -run '^(TestStorageCapabilities\|TestSymptomAnchorMidbandProcessRecalledByTicket)$' -count=10 -v` | **0** | 90 / 20 / 0 | 5 s | 档位矩阵见 D12.2；midband 序列 14 条目、2 段折叠产物，`recall_get("evt_00-1-plan")` 答 228 字节原文 |
| A7b | `./tests/integration/capabilities -run '^TestRecallToolSurface$' -count=10 -v` | **0** | 100 / 10 / 0 | 6 s | normal 六子案（因果走查、精确读命中/越界/分段原文、最新页 early-stop、检索工具）+ failure 两子案（空接线四工具具名拒绝、无嵌入能力时检索工具具名拒绝）×10 |
| A7c | `./assembly -run '^TestHistoricalReadsAvoidUnrelatedHistory$' -count=10 -v` | **0** | 10 / 10 / 0 | 8 s | `newer-history=64 old-target-named=2 granted=3 elsewhere=64`（06.103 锚完整命令，同轮执行） |
| A8 | `./scripts/fold-cold-root -run '^(TestOfflineConversionPreservesEveryFact\|TestSourceChangeRefusesConversion\|TestPartialTargetIsNotReportedReady)$' -count=10 -v` | **0** | 30 / 30 / 0 | 22 s | 真实 CLI：全键全事实等价、源变拒绝、部分目标不报 ready |
| A9 | `./assembly -run '^(TestParentLookupKeepsSessionIsolation\|TestAnchoredBootMatchesFullBoot)$' -race -count=10 -v` | **0** | 20 / 20 / 0 | 14 s | 会话隔离与手调 seed 算法对照；race 数据不入时长比较 |
| A10 | `./assembly -run '^TestRecoveryEntriesPreserveStateAndObligations$' -race -count=10 -v` | **0** | 70 / 10 / 0 | 11 s | 12.105 交接锚：三生产分支（无可用锚 / 有效锚含与不含 checkpoint / 允许降级）+ 不可恢复幸存者具名拒绝 + 无压缩锚 checkpoint 子分支 |
| A11v | `./memory/ledger -run '^(TestAtomicStoreContract\|TestEveryEngineCommitsABatchWholeOrNamesTheCapabilityItLacks)$' -count=10 -v` | **0** | 180 / 20 / 0 | 5 s | `retired_name`（＝`localfile` 别名）七子案各 10 次：commit 回读、删除重开、空值重开、拒提交不发布、屏障不落任何键、批能力自述 |
| B1 | `./assembly -run '^(TestPredecessorQueryBoundsTheWalkToTheChain\|TestParentLookupCostsItsOwnChainNotTheHistory\|TestRecallTraceThroughTierCountsOldTargetLookup)$' -count=10 -v` | **0** | 30 / 30 / 0 | 445 s | 前驱 2/2/2（`newer-history` 16/64/256）；控制走查仍 18/66/258；`old-target-trace-named=5`（原 398）、`newest=2`；root 查询 401/1601/6401（背景 400/1600/6400） |
| B2 | `./memory/ledger -run '^(TestOrderLineIndexFollowsTheTableItParallels\|TestLegalClusteredKeysSurviveCommitAndReopen\|TestScopedReadResolvesTheNamedIdentity\|TestAddressableIndexRefusesATableWithNoEmptySlot)$' -race -count=10 -v` | **0** | 50 / 40 / 0 | 34 s | 槽键控偏移文件随表搬移与重开、129 同桶合法聚集、按身份 scoped 读、满表拒绝静默缺失（D6 第 9 行迁移面存活） |
| COLD-COMPILE | `go build -buildvcs=false -a ./memory/ledger ./assembly` | **0** | — | **9525 ms** | 全部依赖重建一次 |
| FLAKE | `./tests/integration/execution -run '^TestNativeConversationRoundTrip$' -count=5 -v` | **1** | 20 / 3 / **2** | 22 s | 定性见 D12.5 |
| REG | `./memory/... ./assembly ./tool/recall ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host ./scripts/fold-cold-root -count=1` | **0** | 12 包 ok / 0 FAIL | 412 s | `memory 0.2s`、`memory/ledger 131.7s`、`assembly 410.1s`、`history 21.6s`、`capabilities 7.3s`、`host 241.4s`、`fold-cold-root 13.1s`、其余五包 ≤1.2 s；无 `cached` 行 |

### D12.2 档位矩阵（`TestStorageCapabilities` 子案逐档）

| 档 | 子案（`=== RUN` 命中 ×10） | 终局 |
|---|---|---|
| `memory` | `memory tier keeps the commit contract and claims no recovery` | 10/10 PASS |
| `journal`（durable） | `durable tier reads back byte for byte across an independent reopen`、`the durable tier keeps identical content independent per event key`、`the durable tier reopens an empty original body`、`durable tier open refuses a damaged record`、`a failed barrier reports no durable success and publishes no part` | 各 10/10 PASS（非零命中，含具名拒绝与重开等价） |
| `localfile` 别名 | **`TestStorageCapabilities` 内无该档子案**（缺口，见 D12.6-②）；由 A11v 的 `TestAtomicStoreContract/retired_name` 等七子案 ×10 承接，走真实 journal 引擎而非替身 | 10/10 PASS |
| `file`（真 rustviking） | `file tier runs the real rustviking CLI and names the batch it cannot carry` | **SKIP ×10**，`storage_capabilities_test.go:333`：`rustviking CLI "rustviking" is not executable here … no mock stands in for it`；顶层 PASS 不覆盖此子案 → **未验** |

### D12.3 冷热分列与成本读数（同命令、同输入、同机，深 252 代 / 参照深度 5 代）

| 轴 | 热态（A1-warm） | 冷态（A1-cold 第 1 轮，独立 `GOCACHE`+`TMPDIR`） |
|---|---|---|
| 冷编译（`-a`） | 9525 ms（单列于上表） | 同左（独立 `GOCACHE` 的编译在 A1-cold/C1 内部各发生一次，未单列） |
| 冷开档（每根首开槽表） | `slotTableBytesScanned=[589824 0 0 0 0]`：首开一次，其后 0 | 同（首开一次 589824 B） |
| 全日志 CRC（未消项） | `integrityBytesScanned=journalLogBytes=[0 102489 223333 344502 465700]` | `[0 102493 223337 344510 465712]` |
| 代际总墙钟（前 5 代） | 311.9 / 325.0 / 344.9 / 311.3 / 340.8 ms | 431.6 / 431.2 / 481.8 / 456.7 / 432.2 ms |
| boot 墙钟（前 5 代） | 12.94 / 3.87 / 5.06 / 3.70 / 3.94 ms | 17.31 / 5.37 / 5.55 / 3.90 / 6.64 ms |
| deep(252) 对参照 boot 比值（配对） | 10 轮 `held p10` 2.221–4.270、`control p90` 1.192–1.573（最大 2.221/4.270 分属不同轮） | 10 轮 `held p10` 2.025–3.773、`control p90` 1.154–1.564 |
| 代际计数面 | `indexWalkVisits=[0 4 4 4 4]`、`indexWalkValueReads=[0 4 4 4 4]`、`addressedValueReads=[1 37 37 37 37]`、`historyTasks=20`、`durableAccount=[4 4 4 4 4]` | 同（逐轮读数字节级一致，仅 `journalLogBytes` 因写序差 8–12 B） |
| 完整帧 CRC 线性项 | A5：30400 B @200 提交、304000 B @2000、回放窗口 0/0 | 同（A5 不涉冷缓存分列） |
| 停顿窗（20 代中位，10 轮内） | 1 轮 280.2 ms（`spike` 3.12）、2 轮 141.3–150.3 ms（1.55–1.64）、其余 ≤1.40 | 1 轮 283.1 ms（3.00）、2 轮 160.7–172.6 ms（1.68–1.85）、其余 ≤1.42；C1 另有 1 轮 284.2 ms（3.02） |

冷热差落在启动路径与页缓存：同一代码、同一输入下代际总墙钟冷态约为热态 1.35×，boot 墙钟冷态约为热态 1.3–1.7×；计数面冷热完全一致，故差异不在读写数量。

### D12.4 未闭合项：1.3× 判决含 boot/total

1. **阶段判决的一次拒绝（保留，不删轮）**。`A1-cold` 第 1 轮，`judgeGenerationStages` 同时拒绝五阶段：`commits 59.83 ms vs 37.85 ms`（1.58×）、`settling 20.10 vs 12.01`（1.67×）、`sessions 8.66 vs 5.29`（1.64×）、`frame 14.78 vs 8.97`（1.65×）、`closing 37.67 vs 23.11`（1.63×），界＝13 成数。同轮 `judgeCountedFaces` 已通过（该语句位于阶段判决之前），即**读数面不随链增长而墙钟独涨**。
2. **未复现**。同配置复跑（C1，独立新 `GOCACHE`）10/10 绿；本锚在本单 31 次执行中（A1-warm 10 + A1-cold 10 + C1 10 + `-count=1` 探针 1）仅此 1 次阶段拒绝。30 轮总墙钟序列（A1-cold/C1/A1-warm 各 10 轮）里 7 轮出现 ≥1.5× 的孤立停顿窗（20 代中位最高 284 ms，`spike` 至 3.02×），失败轮是"停顿恰好落在末窗"的那一轮（`ref 93.0 / held 173.8 / ratio 1.87`，同轮 180–239 代回落到 110.8，**非单调**）。history 只会单调增长，故该指纹不指向"随链携带历史"的实现缺陷；也不据此判为已接受常数——按 D5 只记未归因。
3. **boot 与 total 仍未闭合**。锚自述把 boot 排除在平界外（其声明读由计数面钉住、其墙钟由配对噪声带判），故 boot 墙钟在 depth 252 对参照深度实测 2.0–4.7× 并**不被 1.3× 断言约束**；total 墙钟同样不在断言面内。全日志 CRC 的 O(日志字节) 就是 D6 第 8 行列的未消项（`integrityBytesScanned` 与 `journalLogBytes` 逐代相等，见 D12.3）。**判定：1.3× 含 boot/total 未闭合，不得勾选。**
4. 归因口径：以上拒绝发生在**墙钟**面且不可复现，读数面全绿；无证据把缺陷归到 06.103 写面，也未修、未放宽任何界（`perGenerationCostBoundTenths` 仍 13）。

### D12.5 既有 flake 定性：`TestNativeConversationRoundTrip`

串行单跑（本单无并发命令在跑）`-count=5 -v`：raw exit 1，PASS 3 / FAIL 2。签名与 12 域 D13.6-① 逐字同构（`native_conversation_roundtrip_test.go:93: settled observation(s) of [call-1] never reached a model request in 10s`）。绿轮耗时 0.00 s、红轮 10.01/10.02 s——是 10 s 预算内的**间歇挂起**，不是计时边界。D13 期间已测 HEAD 树 2/10 红，故非本候选引入；今日读数进一步表明**不需要并发负载即可触发**，D13 的"负载诱发"应改为"与并发无关的间歇挂起"。不属 06 写面：未修、未屏蔽、未从回归排除（REG 的 12 包不含 `tests/integration/execution`，与任务行选集一致）。

### D12.6 未验面（不得被下游当成已证）

1. `file` 真 rustviking 档：CLI 不可执行，子案具名 skip ×10；未以替身或 `AtomicStore` 冒充，未验。
2. `localfile` 别名在 `TestStorageCapabilities` 内**无子案**（缺口）。补子案属新增验收面，须先按 D6 签发，本叶不建；现由 `memory/ledger` 引擎矩阵承接（真实引擎）。
3. 1.3× 判决含 boot/total：未闭合（D12.4-3）。
4. 多进程与并发写者下的冷热成本：未覆盖（D11.7-② 交办仍未做）。
5. 十短/六长正式矩阵与 CI 入口：归顶层 F2，本叶不作。
6. D12.3 的停顿窗（20 代中位 160–283 ms）未归因到具体外部负载：执行期未取该时刻的负载快照（仅有事后读数 load average 6.24–17.94），故只记"环境停顿存在"，不记为已接受的成本常数。

### D12.7 偏离与自行决策

1. **加跑 12.105 交接锚 A10**：派单"必跑"未逐字列出，任务行第 119 行把它列为 06.105 的签收面；12.105 已完成，故按任务行执行并记录（70 RUN / 顶层 10 PASS）。
2. **加跑 B1/B2 两组**：任务行要求"槽键控偏移的等价迁移按 D6 追认口径核对既有覆盖"。B1 核前驱迁移锚与两条控制面仍活，B2 核偏移文件、合法聚集、按身份 scoped、满表拒绝四枚锚在 `-race` 下仍绿；均为既有测试，未新增验收面。
3. **不扩 `TestStorageCapabilities` 的别名子案**：D6 未签发该文件写面，本叶性质为验证，故只记缺口（D12.6-②）。
4. **冷条件用独立 `GOCACHE`+`GOTMPDIR`+`TMPDIR`**，不删共享缓存；A1-cold 与 C1 各用独立缓存目录，失败轮原样保留。
5. **未改任何界、未删轮、未 skip**：`perGenerationCostBoundTenths=13` 与 A5 的 30400/304000 读数均为原样承接。
6. **越界自查**：本叶只写 `design.md`（本节）与 `tasks.md` 06.105 行记录，scratch 限 `/tmp/rr-06-105/`；未碰产品、测试、门、主规格、CI、权限、git 索引/refs；未 commit/push/archive；06.105 未勾选。

### D12.8 阻断④处置：`CommittedIndexEntry` 死生产者删除（12.106 复审单，2026-11-22）

> 产生方式：12.106 初轮阻断④的修复单；债原记于 12 域 design.md D9.7.5-②。写面＝`memory/ledger/segment_store.go`（仅④）与本文件；未改测试、门、主规格、golden；未 git 写。**选择「删除」而非「登记豁免」**：该函数无接口满足面（无 `var _`、无契约示例引用，签名只被包内自调用），是纯解码口；唯一消费者是 12.103 已净删的 `tailFactKeys` 类型/会话判词。

**处置前引用清单**（`grep -rn "CommittedIndexEntry" .`，排除二进制产物）：

| 位置 | 形态 |
|---|---|
| `memory/ledger/segment_store.go:1006` | doc 块 |
| `memory/ledger/segment_store.go:1011` | 函数声明 |
| `golden/runtime/architecture.json`（`concept_ratchet.note`） | 历史抬升理由的自由文本，不是门判据 |
| `docs/api/github_com__SpellingDragon__tagent__memory__ledger.md:588` | 生成物（处置前已 stale，见下） |
| 12 域 design／tasks 与 ORCHESTRATION 台账 | 记录与债 |

**证死**：Go 侧零调用（生产包、`*_test.go`、`examples/`、`cli`、bench 全无）；无接口满足面；`golden/runtime/dead_producers.json` 的 4 条豁免均为外部库接口方法，无一涉及本名。

**删除（1 hunk，删 17 行）**：doc 块＋函数体整段退出；`indexPositionOf`、`segmentIndexEntry`、`segmentIndexCarried`、`indexPosition` 仍有其它生产调用方（`publishedPositionLocked:550`、`partitionPositionLocked`、`eachIndexEntryNewestFirst` 等），**保留**，`go build` 证明无孤立伴生。

**级联观察（本单未动，交 06.106／设计门裁定）**：`IndexEntry` 类型由此**无生产者**。未删——它在 `golden/runtime/architecture.json` 的 `concept_bindings.bindings["IndexEntry"]` 登记为 `event-segment-store` 的概念声明（`domain_surface.bindings=57` 之一），删名须同步改共享 golden，超出本单写面；「声明面保留」与「删名并同步 binding」二选一请设计门裁定。

**证据（2026-11-22，串行执行；scratch `/tmp/rr-12-106-blk2/`）**：

| 项 | 命令 | 终局 |
|---|---|---|
| 死生产者门 | `python3 scripts/gates/check_dead_producers.py` | **exit 0**：`every one of 3513 production function(s) in 32 package(s) has a consumer`（处置前同命令 exit 1 并点名 `memory/ledger/CommittedIndexEntry has no consumer`） |
| 概念预算门 | `python3 scripts/gates/check_concept_budget.py` | **exit 0**，errors 0；`domain_surface.new=57／removed=34／exported_interfaces=79／bindings=57`，与处置前逐项相同（函数不入类型清单） |
| 构建 | `go build -buildvcs=false ./...` | **exit 0** |
| 包回归 | `go test ./memory/ledger -count=1`；`go test ./assembly -count=1` | **exit 0**（83.378s／369.772s） |
| 代表锚 | 见 12 域 design.md D12.0.2 证据行 | RUN 24／PASS 24／FAIL 0 |

**计数口径**：`judged` 由 3515 降到 3513 是**一名函数被数两次**——注册表 `packages` 是顶层目录（`memory` 等），门递归展开出子目录 `memory/ledger` 并与顶层并列，故该包每个函数在总额计两次；删除一名函数使总额减二。同口径下逐目录读数：`memory/ledger` 229、`assembly` 867。

**lint 链残余红（均既存，非本单）**：① `bash scripts/gates/gen_godoc.sh --check` 在处置**前**即报 7 个包页与源码不一致（含 `memory__ledger.md`；日志 `/tmp/godoc-check-baseline.log`），故本删除不构成「由绿转红」；`docs/api` 是生成物，随文档面收口全量重生成，不在本单手工编辑。② `go run ./scripts/comment_policy -no-baseline assembly memory` 报 58 项既存 finding，`-v` 逐项清单在本次三文件**编辑区零命中**（唯一同文件项 `memory/ledger/segment_store.go:1189` 属既有 `EachIndexNewestFirstFrom` doc），本单未新增注释违规。

### D12.9 boot/total 归因（06.106 判决①回叶）

> 产生方式：06.105 回叶，收 06.106 判决①——CRC 仅占 boot 增长 12–20%，`bootBeyondVerification`/`bootUnattributed` 自身增 2.1–5.7×（中位≈4×），"CRC 线性项"已由 D12 自有读数定量驳回，读数在锚序列内、无需新机制。本叶只读归因＋两次最小补测（既有 env-gated trace、既有锚的 cpuprofile，均不新增生产计量）。写面＝本节与 `tasks.md` 06.105 行记录；scratch `/tmp/rr-06-105-attrib/`；未改产品、测试、门、主规格，未 git 写，未勾选。

**读数来源（三批，串行独占）**

| 批 | 来源 | 读数面 |
|---|---|---|
| 既有 30 轮 | `/tmp/rr-06-105/{A1-warm,A1-cold,C1-cold-repeat}.log` 的 `the generations paid total=` 自打印（252 代/轮、window=5、第 0 代排除） | boot、integritySpan、rebindSpan、bootBeyondVerification、bootUnattributed、restored（＝seat.Count，提交数分母）、journalLogBytes 等全序列 |
| 补测① 阶段明细 | `env GOFLAGS=-buildvcs=false TAGENT_BOOT_TRACE=1 go test ./assembly -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v` → **raw exit 0**（`--- PASS … (27.11s)`），日志 `/tmp/rr-06-105-attrib/boot-trace.log` | 每次 boot 六个阶段累计读数（built／storage／restoreChains／supplyRetrieval／seedReplay／converge），354 次 boot（深根 252＋对照与浅根 pair）；用的是既有生产 trace（`assembly/run.go:584-590` 的 `booTrace`），未新增计量 |
| 补测② 调用面 | 同锚加 `-cpuprofile /tmp/rr-06-105-attrib/cpu.out` → **raw exit 0**（`ok … 34.978s`，samples 7.64 s，已存盘） | 逐函数累计样本，用于块内定位 |

环境账：`go1.27.1 darwin/arm64`；执行根 `/Users/pengweiye/Documents/codes/tagent`；源码身份＝`7b0cde16` 工作树＋未提交改动（含 12.106 于 2026-11-22 00:48 的死写删除 `sessionFiled/sessionCuts/sessionSlices`，该三字段现零引用，不改 boot 路径行为）；D12 的 30 轮取自同一工作树 11-21 23:25–00:10（早于该删除）。测前 `uptime`：**load 74.99 / 37.28 / 17.53**（01:05 复读 55.13 / 35.19 / 17.24；占 CPU 者为 Qoder／iOA／CodeBuddy／WindowServer 等 GUI 进程，非本仓测试）；测试期无并发 `go test`／`go build`（`ps` 计数 0）；测毕 4.44 / 15.12 / 13.22。两批串行，无本单并发命令。

**boot 阶段清单与既有计量归属**（boot＝`assemble` `assembly/run.go:509`；阶段 mark 见 :584-590）

| # | 阶段 | 代码位置 | 既有计量面 | 随史增长 |
|---|---|---|---|---|
| 1 | 结构构建＋插件装配（`mountBuiltinPlugins`） | run.go:531-608 | trace `built` | 否（窗中位 0.002 ms） |
| 2 | 日志开档＋全帧 CRC | `ledger.OpenTier` run.go:614 → `openJournalLogFace` `memory/ledger/journal.go:658-708`（`journalReplay` :691） | `integritySpan`（引擎自报成对钟读）＋`integrityBytesScanned`（线性面，分母 `journalLogBytes`） | **是，线性于日志字节** |
| 3 | 地址索引打开（槽表／order／order.line） | journal.go:1055 `openJournalAddress` | `slotTableBytesScanned`（只计枚举，开档读不计入） | 基本否（残项 +0.93 ms，每 KB 成本随深度下降，非字节项；未再分解） |
| 4 | 段存储 Load（seat 一次点读） | `memory/ledger/segment_store.go:330` | `restored`＝seat.Count（**分母**，非成本） | 否（O(1)） |
| 5 | **配置链载入（发现最新配置事实）** | run.go:676 `adoptRecordedPublication` → `assembly/commit.go:729` → commit.go:644 `latestPublishedRecord` → commit.go:371 `latest` → commit.go:277 `loadAddressedLocked` → `memory/ledger/journal_kv.go:353` `EachCommittedKeyNewestFirst` | **无**任何计数面 | **是，线性于提交数（历史）** |
| 6 | checkpoint 载入（framework＋每会话） | run.go:684 → `assembly/run.go:1071`／`assembly/checkpoint.go:1133`／:1038 | `indexWalkVisits`=4（段索引，平）；profile 0.15 s | 否 |
| 7 | 任务账户入表（尾段） | checkpoint.go:833 `seatDurableAccount` → :960 `tailFactKeys` | 日志 `boot seated the task account … tail=6 reads=… keys=…` | 否（静点标记界，尾段 6 键） |
| 8 | fold／rebind | run.go:1049 → run.go:1104 `restoreChains` | `rebindSpan`（成对钟读） | 否（≈1 µs；夹具账户为空） |
| 9 | `supplyRetrieval` | `assembly/read.go:388` | trace | 否（+0.002 ms） |
| 10 | `seedReplay`（cut 界内索引走查、幸存者恢复、站点建立） | run.go:2473／:2705 | `indexWalkVisits`／`indexWalkValueBytes`／`indexWalkValueReads`／`addressedValueReads`（全平）＋trace | 否（+0.026 ms） |
| 11 | `convergeGenerationRetirements` | run.go:721 | trace | 否（0.000） |

**归因表（补测①，window=5，ref=g1–5，held=g247–251，取各序列中位；ms）**

| 项 | ref | held | 增量 | 占 boot 增量（11.566） |
|---|---|---|---|---|
| boot（锚自量） | 3.312 | 14.878 | +11.566 | 100% |
| storage（#1＋#2＋#3＋#4，含 CRC） | 2.809 | 5.603 | +2.794 | 24.2% |
| 　└ `integritySpan`（CRC，已声明线性面） | 0.115 | 1.934 | +1.819 | 15.7% |
| 　└ storage−CRC（#1／#3／#4 残项） | 2.697 | 3.626 | +0.929 | 8.0% |
| `restoreCheckpointState`+`restoreInMemoryState` 块（#5–#8） | 0.284 | 9.498 | +9.214 | **79.7%** |
| 　└ 其中 fold（`rebindSpan`） | 0.001 | 0.001 | +0.000 | 0.0% |
| seedReplay（#10） | 0.150 | 0.176 | +0.026 | 0.2% |
| supply／converge／built（#1、#9、#11） | ≤0.004 | ≤0.004 | ~0 | 0.0% |

各阶段中位独立取，合计 104%（中位差不可加，非重复计数）。30 轮既有读数交叉核：CRC 逐轮占 boot 增量 **5.6%–21.7%、中位 17.3%**（29 轮正增长），与 06.106 判决①的 12–20% 同量级；fold 占比 ~0。

**块内定位（补测②，samples 7.64 s）**

- `adoptRecordedPublication` 累计 **1.87 s（24.48% 全样本）** → `latestPublishedRecord` 1.86 → `configChain.latest` 1.86 → `loadAddressedLocked` 1.86 → `EachCommittedKeyNewestFirst` 1.80 → `eachNewestFirstAt`（每命名一键一次 `pread`）；
- 同块其它调用：`restoreCheckpointState` 0.15 s、`seedReplay` 0.09 s、`restoreChains` 微秒级；对照 `OpenTier` 0.84 s（其中 `journalReplay` 0.52 s）。
- 线性性校验（每提交成本恒定，提交分母＝`restored`/seat.Count）：g20–30 4.01 µs、g60–70 5.01、g120–130 3.52、g180–190 3.21、g245–251 3.47（提交数 267→2720）。

**机制**：`loadAddressedLocked` 自最新提交向最旧逐条命名键，直到命中 `tagent/config/v1/fact:` 前缀（`commit.go:93`）；成本＝**最新配置事实之后的提交条数**，每键一次 `pread`。配置不变的长跑根上最新配置事实停在首代，故每次 boot 走完整条提交序＝O(历史)。引入提交 **`0f84b2d1`**（本候选 stage 提交）：候选前该链走 `Scan(configChainPrefix)`（地址索引前缀枚举，随配置事实数），候选把它换成提交序走查——**本候选引入的新增长**。

**结论（b）未声明的新增长，缺陷候选，不修**

- （a）不成立：被声明的线性面只有 CRC（`assembly/symptom_anchor_recovery_test.go:881` 的 integrity face 声明、`assembly/symptom_anchor_longrun_test.go:346` 的 face 说明与 :353 的 `countedFace{denominator: journalLogBytes}`）；配置链走查无计量、无声明，`judgeCountedFaces`（longrun :362）的六个 face 只覆盖段存储索引与槽表枚举，不覆盖 journal 提交序的 key 走查。
- （c）不成立：无重复累计——`judgeStageSpansAccounted`（longrun :407）已判包含关系，`integrity`／`rebind` 是 boot 内嵌套、只减一次；块增量与 profile 归属互证。另记两处**计量面覆盖洞**（判读用，不改产品）：①锚的计数面缺 journal 提交序 key 走查，本条增长因此能"计数面全绿"地穿过；②`unmeteredJournal`（longrun :942）未实现 `EachCommittedKeyNewestFirst`，A3 类"关断计量"锚在该引擎上落到 prefix-scan 回退，不覆盖本路径。
- 触发条件：配置长期不变的根（生产稳态）→ 每次 boot 走完历史；配置每轮都变的根走查很短。夹具与既有 30 轮都落在触发面。
- 不修依据：修法涉及派生格式或新增 head 键（写面与原子性的设计选择），属设计门；本叶性质为归因。
- 建议路径（供设计门选，未实施）：①配置链最新序列落 seat 键（同段存储 `storeSeat` 口径：一次点读、写侧同批更新）；②固定 head 键 `tagent/config/v1/head` 记最新事实键、append 同批写；③回退候选前的 prefix 读（随配置事实数而非历史）。不推荐"把该走查声明为已声明轴"：与"boot 的声明读由计数面钉住"的锚口径冲突，且产品上稳态配置的启动随历史线性增长。

**total 轴**：`total` 有读数面（每轮自打印 `total=`，252 代）但**无判决面**（`judgeGenerationStages` :276 显式跳过 boot；五工作阶段各过 13 成数界；`total` 只在 `historyProportionalCost` 中作为被拒的对照输入出现）。既有 30 轮：total 窗比 **中位 1.18×（0.74–1.87）**，因工作段≈88 ms 占绝对多数且平（中位 88.14→84.26 ms）；boot 是唯一增长分量（中位 4.36→19.24 ms，占 total 增量 123%）。判定 **「total 需 F2 矩阵补面」**——F2 清单项，不属本归因阻断。

**对 1.3× 判决的影响：维持"未闭合"，理由重述**

1. 五工作阶段的 13 成数（1.3×）判决本身不受影响：补测①该锚 exit 0，D12 矩阵仍绿。
2. D12.4-3 的因果句"boot 2.0–4.7× 即保留的全日志 CRC 线性项"**由本归因改写**：CRC 占 boot 增量 15.7%（补测①；30 轮 5.6–21.7%、中位 17.3%），主体 79.7% 是候选引入的配置链发现走查。
3. 故 boot/total 不能按"已声明轴成本"闭合：按任务行停止条件（"未知 boot/total 增长…不得勾选"）06.105 仍不勾；本条交 06.106 记为成本阻断候选并转设计门，修复回 owning 叶（`assembly/commit.go` 现不在任何叶写面内——12.103 D 表明确"不改 `assembly/commit.go`"）。

**越界自查**：只写本文件本节与 `tasks.md` 06.105 行记录；scratch 限 `/tmp/rr-06-105-attrib/`（两个解析脚本、`boot-trace.log`、`cpu.out`、`cpu-run.log`）；未改产品、测试、门、主规格、CI、权限、git 索引或 refs；未 commit／push／archive；06.105 未勾选。

## D13 · boot 走查重开裁定

> 编号属 D 系列（06 域设计门记录）；因 D12 是冻结的验证记录且不改写，本节物理上排在 D12.9 之后（与 12 域 D8.9 同一处置）。本裁定**不解锁任何实施动作**，只定形状、写面与锚。
>
> 产生方式：**只读读码 ＋ 复算 D12.9 的读数 ＋ 本节记录**。产品/测试/门/主规格零改动；写面＝本 `design.md`（本节＋D6 一行续签）与同单元 `tasks.md` 的 06.105 行指针；**未勾选任何任务框**，未 commit/push/archive。证据等级：D12.9 的读数是实测，`0f84b2d1` 的 diff 是实证；本节的成本推导与候选判定是静态读码，**未运行**。

### D13.0 结论先行

| 案 | 裁定 | 一句话依据 | 会签 |
|---|---|---|---|
| boot 期"最新配置事实"的发现 | **按名探针**（倍增定界＋二分定位，命中值进装载、零重读） | 成本＝2⌈log₂C⌉+1 次点读＋C 次值读，与提交数无关；零新键、零新格式、零写侧改动，并退役两条只有单一消费者的面 | 不需要 A/B（D13.6） |
| D12.9 建议③ 回退候选前的 prefix 读 | **否** | 与"slot table bytes scanned 恒为 0"的既有面互斥，且其轴是活键集（随历史增长）——只换常数不换轴 | — |
| D12.9 建议①② head/seat 位置 | **否** | 每 boot 省十余次点读，要付一条持久派生键＋同批写＋崩溃窗校验＋旧根回退＋重建路径（＝被删的走查） | — |
| 把该走查声明为已声明轴 | **否**（D12.9 已驳） | 与"装载只付本链事实"的既有面冲突，稳态配置的启动仍随历史线性 | — |

### D13.1 缺陷复述、消费者与失效面

**机制（D12.9 原读数）**：boot 经 `run.go:676 adoptRecordedPublication → assembly/commit.go:644 latestPublishedRecord → :371 latest → :361 snapshot → :215 loadLocked → :277 loadAddressedLocked`，在 `:279–285` 以 `EachCommittedKeyNewestFirst`（`memory/ledger/journal_kv.go:348–355`）自最新提交向最旧逐键命名，直到命中 `tagent/config/v1/fact:`（`:93`）前缀。成本＝**最新配置事实之后的提交条数**×每键一次 `pread`（3.2–5.0 µs/键，267→2720 提交线性；补测② profile 中 `adoptRecordedPublication` 1.87 s＝24.48% 全样本）。配置不变的长跑根上最新配置事实停在首代，故每次 boot 走完整条提交序。**由 `0f84b2d1` 引入**（`loadAddressedLocked`、`addressedChainLog`、`addressedKeyOrder` 三个符号在该提交新增，`git show 0f84b2d1 -- assembly/commit.go` 逐 hunk 可见），该提交同时引入 `symptom_anchor_longrun_test.go`（该文件为新增文件）。

**勘误 D12.9 的一处括号**：D12.9 机制段称候选前"走 `Scan(configChainPrefix)`（地址索引前缀枚举，随配置事实数）"。实际路径是 `TierFacts.Scan → AtomicStore.Read(Fetch{Prefix}) → journal_kv.go:85 eachHeld → journal.go:2128` 的**槽表整遍**（`journal.go:1846–1866 eachRecordOnce`，`:1854` 每次 `slotScanBytes += records×stride`），再加每个配置事实一次值读；其轴是 `capacity×stride`（活键集）＋C，不是 C。**仅此括号被纠正**（D12.9 的成本读数、归因结论与 79.7% 占比不变），其余以 D12.9 原文为准。

**真实消费者（全部经同一条 `loadLocked`；装载成功后内存扫描，`loaded` 只在 `attach` 时清）**：

| 调用面 | 何时读 | 读什么 | 失效条件 |
|---|---|---|---|
| `run.go:676 adoptRecordedPublication` | boot 一次（本进程首个 `latest`） | `latestPublishedRecord` | `attach`（换档/重连）清缓存；进程内 `append` 直接扩 `c.facts` |
| `host_document.go:1061/1065/1187` | 文档视图读 | `latestDeclaration`＋`latestPublishedRecord` | 同上；已装载则该次零成本 |
| `host_reload.go:237/378/425` | 热更读写 | `latestDeclaration`／`latestPublicationAttempt` | 同上 |
| `commit.go:610 recordDeclarationFact` | 追加声明事实前 | `latest(configDeclarationKind)` | 同上 |

`api/config` 只给事实身份（`Declaration.FactKey`/`FactType`、`declarationFromPayload`），**不含链读者**（`grep -rn "configChain" api/` 零命中）——链是 `assembly` 组合根的产物，故本裁定的消费者面只在 `assembly`。

**"发现一次"的正确性依据**（本单须写进注释）：`append`（`:332–358`）在 `c.mu` 内先 `loadLocked`、再定 `fact.Seq = len(facts)+1`、再 `Put`＋`Sync`，故**链的序号顺序＝提交顺序**（单写者，同一把锁）；且 `tagent/config/v1/fact:` 只有本链一个写者（常量包内私有，`configChainPrefix` 的生产引用仅本文件；root 一写者由 `ErrRootWriteLocked` 保证，`memory/ledger/config.go:276`）。

### D13.2 候选同尺比较

成本轴一律"随提交数／随配置事实数 C／随活键集"：

| 候选 | 发现成本 | 装载成本 | 随提交数 | 新结构/格式/写侧 | 判 |
|---|---|---|---|---|---|
| 现状（提交序 key 走查） | 最新配置事实之后的提交条数 × 一次 `pread` | C 次值读 | **是（缺陷）** | 无 | 否 |
| a1 回退 prefix 读 | 槽表一遍 `capacity×stride` | C 次值读 | 是（活键集，常数小） | 无 | 否：与零面互斥，见下 |
| a2 用 06.103 的 order 行偏移 seek | 须先说出最新键名才可 seek，而该名正是待求 | — | — | 无 | 否：循环 |
| a3 **按名探针（所选）** | 2⌈log₂C⌉+1 次点读（每次＝槽链走查 ≤`used+1` 条，`journal.go:1766`） | C 次值读（探针命中值直接进装载） | **否** | 无（`Get` 是既有面） | **选** |
| b head/seat 位置键 | 1–2 次点读 | C | 否 | 新派生键＋同批写＋崩溃窗校验＋旧根回退＋重建路径；`durable.FactLog` 无批口 | 否 |
| c B 新增"前缀最新"窄接口 | 无实现能在既有面下优于 a1/a3 | — | — | 新公共面，单一消费者 | 否 |
| e 声明为已声明轴 | — | — | 是 | 无 | 否（D12.9 已驳） |

**a1 的定量否决**（不是口味）：`assembly/symptom_anchor_longrun_test.go:351` 的 `{name: "slot table bytes scanned", …, zero: true}` 自第 1 代起要求恒 0，其判词即"the boot addresses the keys it names, and a full-table enumeration pays the capacity of every family the log stores"。a1 会让该面第 1 代起红。该面与 `loadAddressedLocked` 同出自 `0f84b2d1`，故按其反事实检验：**删掉该面，长跑 boot 就付活键集**——与本节要关闭的同一条目标（boot 不随历史增长）冲突，该面成立，a1 出局。同时承认：a1 的常数比走查小约两个数量级，这正是 0f84b2d1 之前 boot 读数"看起来还好"的原因；只换常数不换轴不构成缺陷闭合。

**当初换走查的理由（防复活被否原因）**：`0f84b2d1` 是整体落账提交，其 message 不陈述本处理由；理由在 10 域被逐字记下——[10 域 design](../10-contract-sync-dead-code/design.md) 的"终态三事实"③与 [10 tasks](../10-contract-sync-dead-code/tasks.md) 第 190 行：`TierFacts.Get` **因 `assembly/commit.go` 的 `addressedChainLog` 而保留**，并明写"删则 `configChain` 的按地址装链断言静默转 false、退回整前缀扫描"。即：按地址装链的存在理由就是"不退回整前缀扫描（＝槽表一遍）"，与本节的零面判据同源。a3 保留 `Get`（且它成为 `addressedChainLog` 唯一方法），故不翻转 10 域任何结论。

**a2 的循环**：`orderLineOf`（`journal.go:1692–1726`）答"最后陈述某键的那次提交的 order 行尾"，入参是键名；本缺陷待求的恰是最新键名，故不缩短发现。

**b 的同尺账**：a3 比 b 每 boot 多付 ≈2⌈log₂C⌉ 次点读（C=1 时 2 次、C=64 时 13 次），而 b 需要一条持久派生键（命名空间新增）、`append` 的同批写（现为 `Put`＋`Sync` 两次调用，`durable.FactLog` 无 batch 口，改口子跨 `api/durable` 共享契约）、崩溃窗校验（head 落后于事实时必须前探，否则静默少装最新事实）、旧根无键回退（回退路径正是被去掉的走查）、以及"head 与日志不一致"这一新失败面。**每 boot 省十余次点读取一条持久格式是负收益**；head 声称的事实可由日志导出，属第二真相，其唯一成立条件是"每次重算太贵"，而 a3 已把它降到对数级。

**c 的否决**：唯一消费者是本链发现（`grep` 核 `addressedChainLog`/`addressedKeyOrder` 仅出现在 `commit.go`）；且零面的约束下不存在比 a3 更便宜的"前缀最新"实现——枚举＝槽表（零面违反）、走查＝历史（本缺陷）、有序＝新持久结构（＝b）。a3 直接复用 `Get`（`memory/ledger/tier_facts.go:44–52`），**不新增接口**。

### D13.3 所选案的语义（实施期逐条验）

1. **定界**：`seq=1` 起步倍增探测（1,2,4,8,…）至首个"不持有"。`1` 不持有 ⇒ 链为空（`facts=nil`、装载完成；今日为空链付整条走查，本裁定降为 1 次探测）。设首个不持有为 `2^k`，则在 `(2^{k-1}, 2^k)` 上二分得 `C`＝最后一个持有。
   - 序号连续是链自身写者保证的不变量（`append` 的 `len(facts)+1`；`:313` 已以"hole is a damaged ledger"依赖它），探针把同一不变量用于定界。
2. **装载**：读 `1..C`、解码为 `[]configFact`（与现状同序同解码）。**探针命中的 `(key,value)` 必须交给装载复用**——装载不得重读；否则 `TestConfigChainFindsItsNewestFactByKeyAlone` 的 `reads==2` 会红（这是硬门，不是优化）。
3. **损坏**：`Get` 的错误一律具名透传（`ErrJournalCorrupt`/`ErrReadAfterClose`/字节解码失败）；`held=false` 只在"槽链走到空槽"（`journal.go:1766 findRecord` 的既有终止条件，H08 后与 `used+1` 同一条不变量）或"键在本视图水位之外"时答出，故**缺席与损坏不互换**。`1..C` 中任一缺失 ⇒ 仍以 `:313` 的同一句具名拒绝（`TestConfigChainRefusesAHoleInItsSequence` 原样绿）。

**具名行为差异（须随本单一起审）**：

| 差异 | 现状 | 本裁定 | 依据 |
|---|---|---|---|
| ① 前缀下"非编号形态"的键（如 `fact:xyz`） | 走查发现它，`ParseUint` 失败 ⇒ 具名拒绝（`commit.go:294–297`） | 不被发现（探针只造 `%020d` 名） | 该命名空间只有本链写者（常量包内私有、`append` 是唯一写入点、root 一写者）；**无测试钉住旧行为**（`grep "names no sequence number"` 仅命中 `commit.go:296`） |
| ② 高于"最后持有序号"的洞（C=3 而日志另有 5、6 无 4） | 走查发现 6，读 1..6 时在 4 处具名拒绝 | 装载止于 C=3，5/6 不可见 | 同一写者保证：`append` 只写 `C+1`，崩溃只会重写同名键、不会跳号；高位洞只能来自外来写者（root 写锁排除）或索引损坏（槽表/order 校验与重建承接）。**这是"损坏读成不存在"的一处新边界**，须在实施记录具名，并由 D13.5⑤ 变异组 4 证上界不错位 |
| ③ 空链证明 | 走完整条提交序 | 1 次探测 | 严格更省，同一答案 |

### D13.4 与既有面的交互

| 面 | 交互 | 结论 |
|---|---|---|
| H08 探测上界（`journal.go:1766 findRecord`，`used+1`） | 探针只走 `Get`，"不存在"的证明就是 H08 的同一条不变量（走到空槽） | 零改动，不新增判词 |
| 06.103 order 行偏移索引（`journal.go:1703 orderLineOf`，槽键控，D10.6①） | 该面答"最后陈述某键的提交的 order 行尾"，对"哪个键最新"无答案 | 本单不用、不改；`grow`/搬移/截断/重建面零交互 |
| 槽表枚举与零面（`longrun_test.go:351`） | 探针不调 `Keys`／`Read(Fetch{Prefix})` | 零面不受影响 |
| `EachCommittedKeyNewestFirst`（`journal_kv.go:348`） | 唯二生产引用是 `commit.go:266/279`；测试引用两处 | 退役导出面（D13.5③） |
| `TierFacts.CommitOrder`（`tier_facts.go:58–66`） | 唯一消费者是 `commit.go:223` 的走查闸门 | 退役（D13.5③） |
| `eachCommittedNewestFirst`（未导出 helper，`journal_kv.go:279`） | 仍被 `segment_store.go:1240/1256` 的索引走查使用 | **保留**；两处测试改调它，断言不变 |
| 档位 | `TierFacts` 恒有 `Get`，故 journal/localfile/file 三档都走探针（今日 `file` 档走 Scan 回落）；内存档 `memoryFactLog` 无 `Get`，仍走 `Scan`（该 log 只装配置事实，O(C)） | `file` 真 rustviking 的发现机制改变，该档在本变更仍未验（同 D12.6①） |
| A 单元（12 域） | 不消费上述两条被退役的面（全仓 `grep` 核） | 不需会签；D8.7.1 六条不变 |
| 10 域（契约同步） | 该域已裁定 `TierFacts.Get` **保留**，理由正是 `assembly/commit.go` 的 `addressedChainLog`（10 design"终态三事实"③、10 tasks 第 190 行）；本裁定让 `Get` 成为 `addressedChainLog` 的**唯一**方法 | 一致，不翻转 10 域结论；`TierFacts` 端口仍严格三方法（Put/Scan/Sync） |

### D13.5 签发（新实施窗口 06.103R，拥有者＝06 域）

**① 窗口与拥有者**：`assembly/commit.go` 至今不在任何叶写面（12.103 D 表"不改 `assembly/commit.go`"；D6 第 3 行不含）。本裁定新签 **06.103R**，前置＝本 D13 通过独立设计审；拥有者＝06 域（`memory/ledger/**` 同域，不跨单元）。A 单元与 13 单元零写面。

**② 精确写面**：

| 文件 | 改动 | 不变量 |
|---|---|---|
| `assembly/commit.go` | `:269–328 loadAddressedLocked` 的发现段改为按名探针（倍增＋二分＋值复用）；删 `:260–267 addressedKeyOrder`；`:251–258 addressedChainLog` 去掉 `CommitOrder()` 只留 `Get`；`:269–276` 五条注释按新机制重写（保留"按地址读、不读无关族、空链＝空账、hole 具名"） | `:186–194 configChain` 与 `loaded` 语义、`:332–358 append` 编号、`:361–384 snapshot/latest`、事实编解码与其余错误文案（仅 `:294–297` 一句随发现方式退役） |
| `memory/ledger/tier_facts.go` | 删 `:58–66 CommitOrder` 及其 doc；`:3–11` 包/类型 doc 去掉"the commit order its engine states"一项 | `TierFacts` 的 Put/Scan/Get/Sync 四口与 `durable.FactLog` 满足面一字不动 |
| `memory/ledger/journal_kv.go` | 删 `:348–355 EachCommittedKeyNewestFirst` 及其 doc | `:272–293 eachCommittedNewestFirst`（未导出）与 `CommitOrder` 接口面不动 |
| `memory/ledger/journal_kv_test.go` | `:259`、`:283` 两处改调 `kv.eachCommittedNewestFirst` | 断言（键序、零值读、早停）逐字不变 |
| `assembly/commit_test.go` | 新增负锚（见④）；`TestConfigChainFindsItsNewestFactByKeyAlone`（`:741`）与 `TestConfigChainRefusesAHoleInItsSequence`（`:797`）**不改一字** | 两条既有锚是本裁定的"不缩"证据 |
| `docs/wiki/**` | 无 | `grep "commit order\|newest fact" docs/wiki/` 零命中；`docs/api/**` 生成物留收口窗口重生成 |

**不写**：`assembly/symptom_anchor_longrun_test.go`（不新增计数面，见④说明）、`api/**`、`memory/ledger/journal.go`（本单零改动）、`memory/ledger/segment_store.go`、`scripts/**`、CI、主 spec。

**③ 接口/字段/格式/依赖变化**：**无持久格式、无新键、无新配置面、无新库、无生产计量**。接口面只有两处退役（`journalKV.EachCommittedKeyNewestFirst`、`TierFacts.CommitOrder`），均为"唯一消费者被本裁定移除"的级联检查（§四），不是新增抽象；`configChain` 对日志的依赖缩为 `Get`；`durable.FactLog`（`api/durable`）不动。

**④ 三锚（随本行冻结）**：

- **正锚（carried，必须原样绿）**：`go -C <root> test ./assembly -run '^(TestConfigChainFindsItsNewestFactByKeyAlone|TestConfigChainRefusesAHoleInItsSequence)$' -count=10 -v` → raw exit 0、RUN 20／PASS 20。前者钉"2 事实链在 6 条外来提交之后仍只读 2 个值"（即探针必须复用命中值），后者钉洞仍具名。
- **负锚（拟新增，今日 0 命中，先红后绿）**：`TestConfigChainDiscoveryCostsTheChainNotTheHistory`，落 `assembly/commit_test.go`，`-count=10 -v`。
  - 构造：两个独立 root，各只有 **1 条**配置事实（`writer.append` 一次），分别再提交 **16** 与 **512** 条无关事实（`1:evt:*`，同 `TestConfigChainFindsItsNewestFactByKeyAlone` 的写法）；两 root 各经 `WithStorage(ledger.StorageConfig{Type: StorageLocalFile, Root: r, Engine: view})`（既有注入缝 `memory/ledger/config.go:283–290`，用法同 `symptom_anchor_longrun_test.go:1160/1188`）真实 boot 一次。
  - `view` 实现 `AtomicStore` 四口＋`EachCommittedNewestFirst`＋`EachCommittedKeyNewestFirst`（转发真实 `ledger.OpenAtomicStore`；**两个走查都要露出来**，否则今日代码落 Scan 回落，所测非所改），按 key 前缀计数：`configPointReads`（`Read(Fetch{Key})` 且键带 `tagent/config/v1/fact:`）、`configPrefixReads`（同前缀的 `Read(Fetch{Prefix})`／`Keys`）、`orderKeysWalked`（两条走查 visit 的键数）。
  - 断言四条：`orderKeysWalked == 0` 且两侧相等；`configPrefixReads == 0`；`configPointReads` **两侧相等**（发现是链的函数，必须与提交数无关，不用 1.3× 界）；行为等量——两侧 boot 后 `latestPublishedRecord` 为同一事实且 `len(chain.facts)==1`（费用不是在跳读下虚报的）。
  - 今日预期红：`orderKeysWalked` 为 16 对 512（D8.4 同构读数 18/66/258），首条断言红；区分子是走查计数（`configPointReads` 两侧同为 1，不具判别力）。
  - 配置事实轴的复核（同一锚第二段，推荐）：同一提交数下 C=1 与 C=64（64 次 `recordPublicationAttempt`）两 root，`configPointReads ≤ C + 2⌈log₂C⌉ + 2`——钉住"随配置事实数（对数）"。
- **消费者锚（既有，真实入口）**：`go -C <root> test ./assembly -run '^(TestPublishRecordLandsOnChain|TestCarriedGenerationAddsNoRecord|TestRestartReconcilesFileWithChain|TestRevertedDeclarationSurvivesRestart)$' -count=10 -v` → RUN 40／PASS 40；四条都经 `publish`／`assemble` 与跨重启读回，证明改的是发现成本不是发现答案。

**⑤ 负变异（`-overlay` 注入隔离副本，仓库字节不变；每组一红一控制）**：

| 组 | 变异 | 必须咬住的锚 |
|---|---|---|
| 1 | 发现段恢复提交序走查（等价于 `0f84b2d1` 的发现段） | 新负锚：`orderKeysWalked` 16 对 512，红 |
| 2 | 发现段改走 `Scan(configChainPrefix)` | 新负锚：`configPrefixReads != 0`，红 |
| 3 | 探针命中值不交给装载（装载重读） | 正锚 `TestConfigChainFindsItsNewestFactByKeyAlone`：`reads==2` 变 3，红 |
| 4 | 定界上界错位（取首个不持有者当 C） | 正锚两枚红（多装一条不存在的事实／洞判词错位） |

**⑥ 回归与门**：`go build ./...`；`gofmt -l`（改动文件）；`go vet ./assembly ./memory/ledger`；`python3 scripts/gates/check_dead_producers.py`（exit 0，且**不再**点名 `TierFacts.CommitOrder`／`journalKV.EachCommittedKeyNewestFirst`）；`python3 scripts/gates/check_concept_budget.py`（`RESULT: ok`）；`go run ./scripts/comment_policy -no-baseline assembly memory`（本单编辑区零 finding）；相邻回归选集同 06.105 REG（`./memory/... ./assembly ./tool/recall ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host ./scripts/fold-cold-root -count=1`）→ 0 FAIL。**归因复核（不作判决面）**：`TAGENT_BOOT_TRACE=1 go test ./assembly -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v` 一次，把 D12.9 的阶段表重算——`restoreCheckpointState` 块的 depth 增量应从 9.214 ms 落到 ≤1 ms 量级、`adoptRecordedPublication` 退出 profile 前列。墙钟不作判决面（D12.4 已证其不可复现）。

**为什么不在长跑锚新增计数面**：能咬住"走查复活"的计数（提交序走过的键）在修复后恒为 0，需要一个**永久只有测试读者**的生产计数器；D9.5/D8.5 对 `passRecords` 的结论是"有真实来源才保留"，此处没有真实来源。D12.9 记的"计量面覆盖洞①"由上述具名锚闭合（配置链发现是 boot 里唯一走该路径的消费者），不以新计量层闭合。

**⑦ 回滚与停止条件**：回滚＝丢弃 06.103R 副本改动，仓库内零 diff；无持久产物、无格式升级、无迁移，日志字节不变（发现方式回到走查，缺陷回来，别的什么都不变）。**回设计门的触发**：① 探针命中值无法在"值读不增"下复用（正锚 `reads==2` 保不住）；② 任一真实档位拿不到点读或答错存在性（`file` 档允许记未验，但不得以未验机制替换 Scan 而不记）；③ 两个计数在正常路径非零；④ 任何新增持久格式、键、接口、配置面或生产计量需求；⑤ 相邻锚出现与本单无关的红（先归因，不得就地改判据）。

### D13.6 共享事实与会签

**不需要 A/B 会签**，三条逐条可核：① `TierFacts.CommitOrder` 与 `journalKV.EachCommittedKeyNewestFirst` 在 12 域零引用（全仓 `grep` 核过，唯一引用是 `assembly/commit.go` 与 `memory/ledger` 自身测试）；② 不涉及提交结局分类、提交位置/发布资格、快照覆盖、原文定位任何一项（D6 第 5 行四条与 D8.7.1 六条一字不变）；③ `assembly/commit.go` 的改动限于配置链发现段，不与 A 的任何写面同文件。若独立设计审认为退役 `TierFacts.CommitOrder` 属共享面，按 D8.7.1 先例由编排者单写一条会签记录，A 侧立场应为"无依赖"。

**跨域影响**：13 域无 spec 改动（无要求陈述"用提交序发现最新配置事实"；实施期 `grep -rn "commit order" openspec/specs` 复核）；`docs/api/**` 是生成物，随收口窗口重生成；13.105 对账加一条"`configChain` 发现机制已无陈旧说法"。

### D13.7 未验面与移交

1. **未运行**：本节全部为读码推导；新负锚今日不存在（拟新增，实施期先核 0 命中再建立），须保留"先红"原始终局。
2. **探针绝对读数未测**：2⌈log₂C⌉+1 是上界，实际二分命中数取决于 C 的二进制形态；接受条件是"两侧相等"与"≤ 上界"，不是某个具体数。
3. **`file` 真 rustviking 档**：发现机制由 Scan 改为探针，本变更仍未验（同 D12.6①）；不得以 journal 档读数代全档。
4. **A3 类锚的读数会移动**：`TestRecoveryCostDetectsGrowthWithInstrumentationDisabled` 的 `unmeteredJournal` 今日缺 `EachCommittedKeyNewestFirst` 而落 Scan 回落（D12.9 覆盖洞②）；修复后它走探针，`boundaryWork.reads` 会多出 2 次点读（C=1 时），其等价断言（control 对 mirror 相等）不变。实施期须重取读数并入记录，不得以"读数变了"判红或改判据。
5. **移交**：06.103R 按 D13.5②–⑥实施；06.106 独立审须逐条复现 D13.5 的变异组与三锚，并按 D13.3 三处具名差异给结论；06.105 不因本裁定勾选（1.3×/boot/total 的判决在本单收录后重判）。

### D13.8 · 06.103R 执行记录（2026-11-22）

> 产生方式：按 D13.5 签发实施一次；全部命令串行、原始 exit 与计数入本节。写面＝`assembly/commit.go`、`memory/ledger/tier_facts.go`、`memory/ledger/journal_kv.go`、`memory/ledger/journal_kv_test.go`、`assembly/commit_test.go` 与本文件＋`tasks.md` 06.105 行；scratch 限 `/tmp/rr-06-103r/`（变异副本、overlay 清单、21 个原始日志）。**未 commit/push/archive**；**06.105 未勾选**（勾选仍等 06.106 复审）。执行根 `/Users/pengweiye/Documents/codes/tagent`，Go 1.27.1 darwin/arm64。候选身份：HEAD `7b0cde162627ad37f1d364bbf3508b3bd0518431`；工作树含本候选未提交改动且 index 空置（`git ls-files` 仅 1 条），故实施前后对照以**内容逐字节比较**核，不以 index/diff 反推。

#### D13.8.1 落点

| 文件 | 改动 | 行数（±） |
|---|---|---|
| `assembly/commit.go` | `loadLocked` 的 addressed 分支去掉 `CommitOrder`→`addressedKeyOrder` 两级断言；`addressedChainLog` 缩为 `Get` 单口；`loadAddressedLocked` 改按名探针（倍增定界＋二分＋命中值进装载）；删 `addressedKeyOrder` 与 `strconv` 导入 | −60／+70 |
| `memory/ledger/tier_facts.go` | 删 `CommitOrder()` 及其 doc；类型 doc 去掉 "the commit order its engine states" 一项 | −14 |
| `memory/ledger/journal_kv.go` | 删 `EachCommittedKeyNewestFirst` 及其 doc | −9 |
| `memory/ledger/journal_kv_test.go` | `:259`/`:283` 改调 `kv.eachCommittedNewestFirst`（断言逐字不变） | 2 行 |
| `assembly/commit_test.go` | 新增 `chainDiscoveryView`＋`measureChainDiscovery`＋`TestConfigChainDiscoveryCostsTheChainNotTheHistory`；既有两锚经脚本对照 `identical`（逐字节未动） | +149 |

保留：`eachCommittedNewestFirst`（未导出，仍被 `segment_store.go` 索引走查使用）、`CommitOrder` 接口面（`segment_store.go:1268`、`examples/contract/memory_ledger/CommitOrder_example.go`）、`TierFacts` 的 Put/Scan/Get/Sync 四口。"按地址读、不读无关族、空链＝空账、hole 具名"四条不变量按 D13.5 ② 保留；零面（slot table bytes scanned）不动（探针不调 `Keys`／`Read(Fetch{Prefix})`）。

#### D13.8.2 探针的形状（实施后语义）

1. **定界**：点读 `seq=1`；不持有则再点读 `seq=2`——持有即以 `:313` 同一句具名拒绝（`states fact 2 and holds no fact 1`），两者皆不持有才答空链（`facts=nil`、`loaded=true`）。持有则自 `2` 倍增（2,4,8…）到首个不持有，在 `(bound/2, bound)` 上二分得 C＝最后持有序号。
2. **装载**：探针命中的 `(seq,value)` 存入 `read`，装载 `1..C` 直接复用；未命中的序号才点读。`ValueReads` 逐轮＝C（不因发现而多读一条值）。
3. **损坏**：`Get` 错误一律 `tagent: read the configuration chain: %w` 透传（`ErrJournalCorrupt`／`ErrReadAfterClose` 原样包裹）；`1..C` 中缺失仍以 `:313` 同一句拒绝；解码文案不变（`tagent: decode the configuration fact %s: %w`）。D13.3-②的高位洞边界按签发成立：装载止于 C，C 之上的洞不可见。

#### D13.8.3 与 D13 的三处偏离（申报，交 06.106 复核）

| # | D13 原文 | 实施 | 依据 |
|---|---|---|---|
| ① | D13.3-1："`1` 不持有 ⇒ 链为空（…降为 1 次探测）" | 1 不持有再探 2；2 持有则具名拒绝。空链付 **2** 次探测 | D13.5 ④ 把 `TestConfigChainRefusesAHoleInItsSequence` 冻结为"必须原样绿、**不改一字**"，其夹具正是"删 fact 1、日志持 fact 2"；D13.5 ② 的保留清单同时要求"空链＝空账"与"hole 具名"，"1 次探测"与二者不可同真，取不缩验收面。代价＝空链多 1 次点读（O(1)，不进历史轴） |
| ② | D13.5 ④："`orderKeysWalked`（两条走查 visit 的键数）" | 只计 key-only walk 的 visit；value walk 转发而不计 | 同一 boot 的索引走查经 `ledger.CommitOrder` 走 **value walk**（`segment_store.go:1268`），每代都过全键；两条都计则修复后仍恒 >0，`==0` 断言不可能绿，且该面与配置链发现无关 |
| ③ | D13.5 ⑤ 组 3："正锚 `reads==2` 变 3，红" | 组 3 **不咬**：无锚红 | `journalKV.valueLocked`（`journal_kv.go:260–270`）先读 resident 集：同一键第二次读不落 `valueReads`，"装载重读"在 `ValueReads` 面无读数；位移只出现在新锚 `configPointReads`（2→3、71→78，仍在上界内）。复用按签发实施，但其"硬门"预期不成立 |

#### D13.8.4 三锚（原始读数）

- **新负锚** `TestConfigChainDiscoveryCostsTheChainNotTheHistory`（实施前 `func` 定义 grep 0 命中已核）：
  - **先红**（实施前，`-count=1`，raw exit 1）：`commit_test.go:1070: the discovery walked 17 commit key(s) after 16 unrelated commits, want none: …`
  - **后绿**（`-count=10 -v`，raw exit 0，RUN 10／PASS 10）：`point reads 2/2`（16 vs 512 无关提交）、`prefix reads 0/0`、`walked keys 0/0`；C=1 vs C=64 `point reads 2/71`（上界 3 与 64+2·6+2=78）。10 轮读数逐轮一致。
- **正锚（carried）** `-count=10 -v`：实施前 RUN 20／PASS 20 raw exit 0；实施后 RUN 20／PASS 20 raw exit 0。两锚逐字节未动。
- **消费者锚（4 名）** `-count=10 -v`：实施前 RUN 40／PASS 40；实施后 RUN 40／PASS 40，raw exit 0。

#### D13.8.5 变异（`-overlay` 指向 `/tmp/rr-06-103r/mut/`，仓库字节不变；每组一红一控制）

| 组 | 变异 | 咬住的锚（原始判词） | 控制绿 |
|---|---|---|---|
| 1 | 三生产文件回退为实施前（走查复活） | 新锚红：`walked 17 commit key(s)` | 两正锚绿 |
| 2 | `loadLocked` 去掉 addressed 分支（走 Scan） | 新锚红：`enumerated the configuration family 1 time(s)`；**洞锚亦红**（Scan 无洞判据，答 1 fact） | `…FindsItsNewestFactByKeyAlone` 绿 |
| 3 | 探针命中值不交给装载（装载重读） | **无锚红**（见 D13.8.3-③）：读数位移 2→3、71→78 | 全部绿 |
| 4 | 定界上界错位（取首个不持有者当 C） | `…FindsItsNewestFactByKeyAlone` 红（`states fact 3 and holds no fact 3`）＋新锚红（boot 失败） | 洞锚绿 |
| 4b | 去掉头部成对判据（1 不持有即答空链） | 洞锚红（`answered 0 fact(s) … want the hole refused by name`） | `…FindsItsNewestFactByKeyAlone` 绿 |

组 4 与 4b 是 D13.5 组 4 在本实施下的两半（本实施把"定界上界"与"头部成对"分成两条规则），"正锚两枚红"由两变异合取达成。

#### D13.8.6 门与回归（全部串行，原始 exit）

| 命令 | 结果 |
|---|---|
| `go build -buildvcs=false ./...` | exit 0 |
| `gofmt -l`（5 个改动文件） | 空 |
| `go vet ./assembly ./memory/ledger` | exit 0 |
| `python3 scripts/gates/check_dead_producers.py` | exit 0；"every one of 3509 production function(s) in 32 package(s) has a consumer"；grep 两条退役面＝0（不再点名） |
| `python3 scripts/gates/check_concept_budget.py` | exit 0，`RESULT: ok`（本单未新增结构，只降不抬） |
| `go run ./scripts/comment_policy -no-baseline assembly memory` | exit 0；5 个编辑文件 0 finding ——**勘误**：见 D13.8.12（`-v` 实测编辑区 3 条） |
| `go test ./memory/ledger ./assembly -count=1` | exit 0，0 FAIL；ledger 107.361s、assembly 393.874s |
| `go test ./tests/integration/history -count=1` | exit 0，0 FAIL，10.796s |
| 相邻选集 `./memory/... ./tool/recall ./tests/integration/capabilities ./tests/integration/host ./scripts/fold-cold-root -count=1` | exit 0，10 个包全 `ok`（memory 0.237s／embedder 0.509／ledger 98.193／query 0.738／replay 1.212／search 0.970／recall 0.495／capabilities 5.439／host 221.611／fold-cold-root 9.549） |
| `go test ./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v`（D13.7-4 重取） | exit 0，RUN 10／PASS 10；等价断言（control 对 mirror 相等）不变。`unmeteredJournal` 不打印 `boundaryWork.reads`，位移按机制为每代 boot 多 2 次点读（C=1）；未新增印刷面（不在签发写面） |

#### D13.8.7 boot 成本复测与 D12.9 复核

**判决面＝新锚计数，墙钟仅记录**（D12.4／D13.5）。`go test ./assembly -run '^(TestSymptomAnchorBootCostFlatAcrossGenerations|TestSymptomAnchorPerGenerationCostFlatOverALongRun)$' -count=10 -v` → **RUN 20／PASS 19／FAIL 1**，raw exit 1。

- 红轮＝`TestSymptomAnchorPerGenerationCostFlatOverALongRun` 的**墙钟阶段判**（`symptom_anchor_longrun_test.go:909`：`closing` 阶段第 247 代 28.995917 ms 对上界 28.541229 ms（13 成数），`total=[…]` 原样保留），该次 load average **7.85**；同一轮的计数面判绿（`:902` "the counted faces hold it to the declared reads"）。
- 归因三步：① 判据是墙钟（不可复现，且本单不触摸 closing 阶段路径——链在 boot 已装载，close 的 append 不再读）；② **基线 A/B**：同锚在实施前代码上（`-overlay` 回退三生产文件）`-count=10` → RUN 10／PASS 10；③ **候选复跑**（load 3.61）：同命令 `-count=10` → RUN 10／PASS 10。
- 三次候选执行共 30 轮：计数面全绿、墙钟 1 红。结论＝负载下的墙钟边界，非本变更引入；失败轮保留在 `/tmp/rr-06-103r/15-boot-cost-count10.log`。
- **最终字节复跑同一命令（D13.8.11 改词后）：RUN 20／PASS 20，raw exit 0**。十轮 boot 判词为 `held p10` 1.177–1.385（3 轮 "separated … hold it to the declared reads"、7 轮 "not separated … still pin the reads"），计数面十轮全绿；该轮未再出现阶段墙钟红。

**`TAGENT_BOOT_TRACE=1` 同锚单跑一次**（`-count=1 -v`，raw exit 0；354 次 boot 的 6 个 trace 相位，取首 10／末 10 中位，相位差为块增量）：

| 块 | 首 10 中位 | 末 10 中位（最深代） | D12.9 归因表（held 参照深度→depth 252） |
|---|---|---|---|
| `built`→`storage`（开档） | 5429.5 µs | 5575.5 µs | storage 2.809→5.603 ms |
| `storage`→`restoreChains`（`restoreCheckpointState`+`restoreInMemoryState` 块） | **661.5 µs** | **636.5 µs** | 0.284→**9.498 ms** |
| `restoreChains`→`supplyRetrieval` | 8.0 µs | 8.0 µs | ≤0.004 ms |
| `supplyRetrieval`→`seedReplay` | 565.5 µs | 454.0 µs | 0.150→0.176 ms |
| `seedReplay`→`converge` | 9.0 µs | 8.5 µs | ≤0.004 ms |
| boot total（`built`→`converge`） | 6781.0 µs | 6658.5 µs | boot 3.312→14.878 ms |

`restoreCheckpointState` 块由"随历史线性（9.498 ms @ depth 252）"落到 **O(log C) 且与深度无关（636.5 µs）**，符合 D13.5 ⑥ 的 `≤1 ms` 量级判；boot total 中位在首末两侧持平（6781→6659 µs）。整个 trace 的最大值出现在单轮 `storage→restoreChains` 6.341 ms（354 次中一次，离群，未归因，保留）。

**最终字节同命令复跑**（`/tmp/rr-06-103r/28-boot-trace-final.log`，354 次 boot）：`storage→restoreChains` 首 10 中位 **779.0 µs**／末 10 中位（最深代）**348.0 µs**（max 6.816 ms，同为单轮离群）；`built→storage` 5937.0→7092.5 µs、`supplyRetrieval→seedReplay` 540.5→345.0 µs、其余块 ≤10 µs。两次独立取样的结论一致：该块不再随深度增长。

#### D13.8.8 rustviking（06.105 具名 skip 补记）

`env PATH=/tmp/rv-target/release:$PATH GOFLAGS=-buildvcs=false go test ./tests/integration/history -run '^TestStorageCapabilities$' -count=10 -v` → raw exit 0，**RUN 80／PASS 80、0 SKIP**；其中 `TestStorageCapabilities/file_tier_runs_the_real_rustviking_CLI_and_names_the_batch_it_cannot_carry` 子案 RUN 20／PASS 20（10 次 × 2 行）。所用二进制＝sibling 只读构建 `/tmp/rv-target/release/rustviking`（`rustviking 0.1.0`）。06.105 记录的"`file` 真 rustviking 具名 skip ×10、未验"在本窗口补记。

**仍未验**：配置链发现本身未在 `file` 档端到端跑（新锚只跑 `localfile`）。机制面静态核：`rustvikingKV.Read` 的 `fetch.Key` 走 `kv get -k`，缺失答 `nil` 条目→`TierFacts.Get` 答 `held=false`（点读与缺席语义成立）。按 D13.7-3 记未验，不以 localfile 读数代全档。

#### D13.8.9 未验面与移交

1. `docs/api/github_com__SpellingDragon__tagent__memory__ledger.md` 与 examples 页仍列两条退役面——生成物，按 D13.5 ② 留收口窗口重生成；`docs/wiki` 与 `openspec/specs` 的 `commit order|newest fact` grep 均 0 命中（与 D13.5 ②／D13.6 预判一致）。
2. 变异组 3 无锚可咬（D13.8.3-③）：若 06.106 要求"复用"有可咬证据，须新增读数面——不在本单签发写面内。
3. `file` 档配置链发现未验（D13.8.8）。
4. 06.105 仍不勾选：本单只闭合"发现成本"，1.3× 含 boot/total 的判决与 A1-cold 冷尖峰归 06.106 复审后重判；不因本记录改判。
5. 06.106 须逐条复现本节三锚、五组变异（含组 3 不咬）、三处偏离与 D13.3 的具名行为差异①②，并对 `file` 档未验给结论。

#### D13.8.10 越界自查

只写上表 5 个代码/测试文件与本文件、`tasks.md` 06.105 行指针；scratch 限 `/tmp/rr-06-103r/`；未改 `api/**`、`memory/ledger/journal.go`、`memory/ledger/segment_store.go`、`scripts/**`、CI、主 spec、`docs/**`、`openspec/specs/**`；未动 git 索引/refs，未 commit/push/archive；06.105 未勾选。

#### D13.8.11 证据的字节边界

D13.8.4–D13.8.9 的读数取自同一候选字节；其后只改 `assembly/commit.go` 一处 doc 词（方向词纠错，非语义）：

```
-//   - an engine that answers no key by name loads the chain through the prefix scan below, …
+//   - an engine that answers no key by name loads the chain through the prefix scan of the load, …
```

改词后在**最终字节**上重取全部受影响证据：

| 命令 | 结果（最终字节） |
|---|---|
| `go build -buildvcs=false ./...` | exit 0 |
| `gofmt -l`（5 文件） | 空 |
| `go vet ./assembly ./memory/ledger` | exit 0 |
| `python3 scripts/gates/check_dead_producers.py` | exit 0，3509 生产函数全有消费者 |
| `python3 scripts/gates/check_concept_budget.py` | exit 0，`RESULT: ok` |
| `go run ./scripts/comment_policy -no-baseline assembly memory` | exit 0，编辑区 0 finding ——**勘误**：见 D13.8.12（`-v` 实测编辑区 3 条） |
| 三锚 `-count=10`（新锚＋两正锚） | RUN 30／PASS 30，raw exit 0 |
| 消费者锚 4 名 `-count=10` | RUN 40／PASS 40，raw exit 0 |
| boot 成本两锚 `-count=10` | RUN 20／PASS 20，raw exit 0（`/tmp/rr-06-103r/27-boot-cost-final.log`） |
| `TAGENT_BOOT_TRACE=1` 同锚 `-count=1` | raw exit 0；`storage→restoreChains` 779.0→348.0 µs（`/tmp/rr-06-103r/28-boot-trace-final.log`） |

重证据（`./memory/ledger ./assembly` 全包 393.874 s、`TAGENT_BOOT_TRACE` 354 次 boot、rustviking 80/80、相邻 10 包、A/B 基线 10 轮）在改词前字节上取得；两者内容差异只有上述一行注释，无代码/测试/断言语义变化。

#### D13.8.12 · 接续单复测（前单撞限；2026-11-22，最终字节）

> 产生方式：**只跑命令＋补本节、两处勘误指针与 `tasks.md` 06.105 行**；未改生产/测试/门/主规格/CI/docs。全部命令串行，逐块记 `uptime`；执行根与工作树字节同 D13.8.11（5 个代码/测试文件 mtime ≤ 02:06:09，其后仅本记录文件变动）。scratch `/tmp/rr-06-103r-f/`（本轮 30–50 号日志、`headtree/`、`parse_trace.py`）。**变异未复做**：前单存档在 `/tmp/rr-06-103r/mut/`（五份 overlay JSON＋五份变体源），本轮只读复核（见下文表格）。

**复测矩阵（原始 exit／计数；`env GOFLAGS=-buildvcs=false`，命令在工作树根）**

| 项 | 命令 | 原始结果 |
|---|---|---|
| 新负锚完整 | `go test ./assembly -run '^TestConfigChainDiscoveryCostsTheChainNotTheHistory$' -count=10 -v` | raw exit 0；RUN 10／PASS 10／FAIL 0；10 轮读数逐轮一致：16 vs 512 无关提交 `point reads 2/2`、`prefix reads 0/0`、`walked keys 0/0`；C=1/64 `point reads 2/71`（上界 3／78） |
| 正锚两枚（carried） | `go test ./assembly -run '^(TestConfigChainFindsItsNewestFactByKeyAlone|TestConfigChainRefusesAHoleInItsSequence)$' -count=10 -v` | raw exit 0；RUN 20／PASS 20／FAIL 0 |
| 消费者锚 4 名 | `go test ./assembly -run '^(TestPublishRecordLandsOnChain|TestCarriedGenerationAddsNoRecord|TestRestartReconcilesFileWithChain|TestRevertedDeclarationSurvivesRestart)$' -count=10 -v` | raw exit 0；RUN 40／PASS 40／FAIL 0（每名 10） |
| boot 成本两锚 | `go test ./assembly -run '^(TestSymptomAnchorBootCostFlatAcrossGenerations|TestSymptomAnchorPerGenerationCostFlatOverALongRun)$' -count=10 -v` | raw exit 0；RUN 20／PASS 20／FAIL 0；起测 load 7.03／5.23／4.73。十轮计数面全绿（`the counted faces hold it to the declared reads` ×10）；墙钟判 `held p10` 1.132–1.455，3 轮 `separated`（1.455／1.334／1.431 对 control p90 1.387／1.280／1.220）、7 轮 `not separated`；**无阶段墙钟红**（前单 19/20 的红在 load 7.85，本轮 load 7.03 全绿，与 D13.8.7 的负载归因一致） |
| boot trace | `TAGENT_BOOT_TRACE=1 go test ./assembly -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v` | raw exit 0；PASS 33.44s；354 次 boot（起测 load 5.70／6.95／5.86） |
| rustviking | `env PATH=/tmp/rv-target/release:$PATH go test ./tests/integration/history -run '^TestStorageCapabilities$' -count=10 -v` | raw exit 0；RUN 80／PASS 80／FAIL 0／SKIP 0；`file_tier_runs_the_real_rustviking_CLI_and_names_the_batch_it_cannot_carry` RUN 20／PASS 20（10 轮；`rustviking 0.1.0`，`/tmp/rv-target/release/rustviking`） |
| 退役核（grep） | `grep -rn 'EachCommittedKeyNewestFirst' --include='*.go' memory/ runtime/ api/ tool/` | 0 行（exit 1）；全仓同 grep 除测试注入缝 `assembly/commit_test.go:929`（`chainDiscoveryView`）外亦 0；`TierFacts.CommitOrder` 方法定义已无，`CommitOrder` 仅余接口面（`memory/ledger/segment_store.go:1283` 消费）与 `examples/contract/memory_ledger/CommitOrder_example.go` |
| 退役核（门） | `python3 scripts/gates/check_dead_producers.py` | raw exit 0；`every one of 3509 production function(s) in 32 package(s) has a consumer`；输出中两条退役面命中 0 次 |
| 相邻回归 | `go build -buildvcs=false ./...`；`gofmt -l`（5 文件）；`go vet ./assembly ./memory/ledger` | 依次 raw exit 0／空输出／raw exit 0 |
| 相邻回归 | `go test ./memory/ledger ./assembly -count=1` | raw exit 0；`ok memory/ledger 124.223s`、`ok assembly 416.311s`；FAIL 0（起测 load 8.07／7.23／6.02） |
| 相邻回归 | `go test ./tests/integration/history -count=1` | raw exit 0；`ok 11.173s`；FAIL 0 |
| 门 | `python3 scripts/gates/check_concept_budget.py` | raw exit 0，`RESULT: ok`（只降不抬） |

**boot 块读数（本轮 354 次 boot；块＝相邻相位差；首 10／末 10 取中位）**

| 块 | 首 10 中位 | 末 10 中位（最深代） | D12.9 归因表（ref→held） |
|---|---|---|---|
| built→storage（开档） | 6136.0 µs | 6793.5 µs | storage 2.809→5.603 ms |
| storage→restoreChains（含 `restoreCheckpointState`＋`restoreInMemoryState`） | 714.0 µs | **539.0 µs** | 0.284→**9.498 ms** |
| restoreChains→supplyRetrieval | 8.0 µs | 6.5 µs | ≤0.004 ms |
| supplyRetrieval→seedReplay | 477.5 µs | 409.0 µs | 0.150→0.176 ms |
| seedReplay→converge | 8.5 µs | 6.0 µs | ≤0.004 ms |
| boot total（built→converge） | 7679.0 µs | 8086.5 µs | boot 3.312→14.878 ms |

变化＝**O(历史)→O(log C)**：同一块在 D12.9 的 held（depth 252）是 **9.498 ms**，本轮最深代为 **539.0 µs**，且末 10 中位低于首 10（714.0→539.0，不再随深度上升）；与 D13.8.7 两次独立取样（661.5→636.5、779.0→348.0）同结论。单轮最大值 8844.0 µs（第 307 次 boot）为离群，前两轮同形各一次（6816 µs／6.341 ms），未归因。boot total 只记持平（7679.0→8086.5，D12.4 已证墙钟不作判决面）。

**变异复核（读前单存档，未复做）**：日志 `/tmp/rr-06-103r/08-mut-g1.log`、`09-mut-g2.log`、`09-mut-g3.log`、`09-mut-g4.log`、`09-mut-g4b.log`，overlay＝`mut/overlay-g*.json`（绝对路径 `Replace`，不写仓库）。当前 `assembly/commit.go` 的 md5 `ad4201b6…` 与五份变体源全部不同，g1（`58404149…`）亦与 g2/g3/g4/g4b 互异，故存档确非仓库状态：

| 组 | 变异 | 红（原始判词） | 控制绿 |
|---|---|---|---|
| 1 | 三文件回退（走查复活） | 新锚 FAIL `the discovery walked 17 commit key(s) after 16 unrelated commits, want none` | 两正锚 PASS |
| 2 | 转 `Scan(configChainPrefix)` | 新锚 FAIL `enumerated the configuration family 1 time(s)`；洞锚亦 FAIL `answered 1 fact(s) … want the hole refused by name` | `…FindsItsNewestFactByKeyAlone` PASS |
| 3 | 命中值不交装载 | **无锚红**（D13.8.3-③：resident 集吸收重读）；读数位移 `point reads 3/3`、`3/78`，仍在上界内 | 全部 PASS |
| 4 | 上界错位（取首个不持有者为 C） | `…FindsItsNewestFactByKeyAlone` FAIL `states fact 3 and holds no fact 3`；新锚 FAIL（boot 夹具同句） | 洞锚 PASS |
| 4b | 去掉头部成对判据 | 洞锚 FAIL `answered 0 fact(s) … want the hole refused by name` | `…FindsItsNewestFactByKeyAlone` 与新锚 PASS |

**勘误（D13.8.6 与 D13.8.11 的 comment_policy 行）**。原文“`go run ./scripts/comment_policy -no-baseline assembly memory` exit 0；5 个编辑文件 0 finding”。复测：该命令**不带 `-v` 时只打印总数、不列逐条 finding**，据“输出无文件名”推“无 finding”不成立。`-v` 实测（`/tmp/rr-06-103r-f/34-comment-policy-verbose.log`）：

- `assembly/commit_test.go:1061` 两条规则（`test-doc-not-one-sentence`、`test-doc-line-too-long`），对象＝新锚 `TestConfigChainDiscoveryCostsTheChainNotTheHistory` 的 doc；
- `assembly/commit_test.go:1096` `free-standing`，对象＝新锚内 `The bound is the chain's own values plus two probes per binary digit of its length.`；
- `memory/ledger/journal_kv.go:295` 一条 `doc-not-brief`，对象 `commitOrderSeek`（06.103 既有面，非 06.103R）。

基线对照（编辑前副本逐文件扫描）：`/tmp/rr-06-103r/pre-commit_test.go`＝**0 finding**、`mut/g1-journal_kv.go`＝1（即上述既有条）、`g1-tier_facts.go`＝0、`g1-commit.go`＝0 → **06.103R 净增 3 条 finding，全部在 `assembly/commit_test.go` 的新锚内**。
项目门：`bash scripts/lint.sh --only policy` 在当前工作树 raw exit 1（77 finding／基线 `counts: {}`）；同一命令在 HEAD 树（`git archive HEAD` → `/tmp/rr-06-103r-f/headtree/`）raw exit 0、`0 finding(s)`。77 的构成＝06.103R 新增 3 ＋ 编辑区内既有 1 ＋ 编辑区之外 73（如 `assembly/checkpoint_test.go`、`memory/ledger/kv_original_store_test.go`，属其他在飞工作）。
处置：本单**禁改生产/测试**，3 条未修；交 **06.106 裁定**——或随候选收口清形状并重生成基线（连带其余 74 条），或回 owning 叶改 doc/注释形状。`docs/api/**` 生成物仍列 `TierFacts.CommitOrder`（`docs/api/github_com__SpellingDragon__tagent__memory__ledger.md:1090`）与 `ExampleCommitOrder`，同 D13.8.9 留收口窗口。

**观察（非偏离）**：`assembly/commit.go:311–313` 另有 `bound > 1<<62` 的具名拒绝，D13.3／D13.8.2 未列；只在 uint64 上界不可达，不改任何可达语义、不触断言。

**越界自查**：本轮未改生产/测试/门/主规格/CI/docs；只写本文件本节、D13.8.6 与 D13.8.11 两处勘误指针、`tasks.md` 06.105 行；scratch 限 `/tmp/rr-06-103r-f/`；未动 git 索引/refs，未 commit/push/archive；06.105 未勾选。

### D13.9 · R4 补锚（2026-11-22）

> 产生方式：按编排者授权补一枚具名锚（理由＝06.106 关闭轮 **R4**：2.4 判「部分兑现」的唯一原因＝具名锚 `TestBootIndexEntriesBoundedAcrossGenerations` 缺，槽键控机制与计数面原已有据）。写面＝`assembly/symptom_anchor_recovery_test.go`（新锚＋三 helper）与本文件 D6 一行＋本节。**未改任何任务勾选**（06.106 已勾，本项是 CARRYOVER §四.1 登记项的清偿）；未改生产/门/主规格/CI/docs；未动 git 索引/refs，未 commit/push/archive；scratch 限 `/tmp/rr-r4/`。执行根 `/Users/pengweiye/Documents/codes/tagent`，Go 1.27.1 darwin/arm64，命令全部在工作树根串行执行、原始 exit 记录如下。冻结证据的字节：测试文件 `md5 8578bbed19fba5c0b5071a8d90636df8`；生产两文件前后一致（`segment_store.go da637877cd66b7e28541d5956f216105`、`journal.go 0ee7da88a63f19865912bf8efa140c45`）。

#### D13.9.1 结论先行

2.4 的具名锚已建立并在冻结字节上十轮绿；机械面零改动，既有锚逐字节未动（对 HEAD `diff`：新增 107 行、删改 0 行），共享 fixture `walkBootGenerations`/`generationBoot` 未改。判据分两面：**F1** boot 期枚举条目随代际有界（相对第 2 代 2×，且每代 `< 链上索引事实数的一半`），**F2** 派生 `order.line` 表按槽容量轴而非提交数增长（同键 1 提交与 50 提交字节相等、5000 键根更大）。两组变异分别咬住两面，控制绿。CARRYOVER §四.1 的 2.4「部分兑现→兑现」勘误与 13.106 终审的 R4 项关闭不在本单，由编排者另办。

#### D13.9.2 落点与判据

| 面 | 判据 | 读数面 |
|---|---|---|
| F1 boot 期枚举条目 | 以第 2 代 `observed[1]` 为参照：其后各代 `indexEntries ≤ 2×indexEntries[1]`；每代 `2×indexEntries < indexed`（`indexed`＝链上索引事实数）。fixture 前提由 `judgeBootFixture` 判（链与日志逐代增长、每代 settle 与 account 相符、链上任务＝全史） | `FileSegmentStore.IndexEntriesRead()`（生产计量，随已省道装载为 0）；`indexed`＝`chainIndexSize` |
| F2 槽键控容量轴 | 同 100 键 1 次提交 vs 同 100 键 50 次重写提交：`<root>/kv/order.line` 字节必须相等，且后者 `journal.log` 字节须更大（历史轴变了、表不变）；5000 键 1 次提交的 `order.line` 字节须大于 100 键根 | `<root>/kv/order.line`（派生表；本表行6 追认的 `capacity×8` 落盘面）、`<root>/kv/journal.log`（历史字节） |

F2 之所以是「容量轴」判据：`order.line` 一字一槽（8 字节/槽），文件大小＝槽表容量×8，与提交数无关；若回落为「每提交 8 字节」（D10.6 第 1 条被否的方案），50 提交根必比 1 提交根多 400 字节，相等判据即红。该读数经 `ledger.OpenAtomicStore`＋`AtomicStore.Commit` 的生产提交入口写入，不是测试手接。

#### D13.9.3 三锚（冻结字节，原始读数）

- **正锚（新名）**：`go test ./assembly -run '^TestBootIndexEntriesBoundedAcrossGenerations$' -count=10 -v` → raw exit 0，**RUN 10／PASS 10／FAIL 0**，25.841 s。十轮读数逐轮一致：`indexEntries=[0 0 0 0 0]`、`order.line`＝**8192 / 8192 / 131072 字节**（100 键×1 提交、100 键×50 提交、5000 键×1 提交）；`journalLogBytes` 每轮内逐代增长（如 `[0 103061 224529 346378 468280]`，末值随二进制时间戳位数字在 468240–468280 间漂移，每轮均满足 fixture 的"末代>第2代"）。本锚不判墙钟（无墙钟断言），只判计数与表字节。
- **负锚（两组变异）**：见 D13.9.4，两组 raw exit 1，各自咬住一面。
- **消费者锚**：同文件既有锚 `TestSymptomAnchorBootCostFlatAcrossGenerations` 对同一计量断言 `indexEntries != 0` 即 fatal，与本锚同 run 复跑：`go test ./assembly -run '^(TestBootFixtureGrowsTasksAcrossGenerations|TestBootQuantityProbesReadTheRealBoot|TestSymptomAnchorBootCostFlatAcrossGenerations)$' -count=1 -v` → raw exit 0，**RUN 3／PASS 3**（12.097 s）。F2 的三根经 `AtomicStore.Commit`（生产提交入口）写入后由 `Close` 释放根锁，再读盘上表，不依赖测试后门。

#### D13.9.4 变异（`-overlay` 指向 `/tmp/rr-r4/mut/`，仓库字节不变；每组一红一控制）

| 组 | 变异 | 咬住的锚（原始判词） | 控制绿 |
|---|---|---|---|
| g1 | `FileSegmentStore.Load` 恒走 `rederive`（全索引枚举，即"完整回退 `Load` 回全量前缀扫"） | **F1 红**：`indexEntries=[0 43 94 145 196]`，判词 `the boot of generation 1 enumerated 43 index entr(ies) beside the 43 facts the chain indexes, want at most half` | `TestBootFixtureGrowsTasksAcrossGenerations` exit 0（2.230 s） |
| g2 | `journalAddress.fileCommit` 每提交 `grow(capacity+1)`（派生表随提交数线性累积） | **F2 红**：`order.line` 8200（1 提交）比 **8592**（50 提交），判词 `the root rewritten under 50 commit(s) states 8592 order-line byte(s) beside the 8200 of the one-commit root`；同轮 F1 面仍 `indexEntries=[0 0 0 0 0]`（两面独立可咬） | 同上 exit 0（8.624 s） |

g2 的两根都各带变异自身的一个字：`grow(capacity+1)` 在每次提交后执行，故 1 提交根本身是 1025 槽（8200 字节）；相等判据仍须相等，50 提交根多出 50 字（+400 字节）即被咬住。变异源与 overlay 清单在 `/tmp/rr-r4/mut/`（`g1-segment_store.go`、`g2-journal.go`、`overlay-g1.json`、`overlay-g2.json`），仓库两生产文件 md5 前后一致（见本节开头），变异未写入仓库。

#### D13.9.5 门与相邻

| 命令 | 结果 |
|---|---|
| `go build -buildvcs=false ./...` | raw exit 0，空输出 |
| `gofmt -l assembly/symptom_anchor_recovery_test.go` | 空 |
| `go vet ./assembly` | raw exit 0，空输出 |
| `go run ./scripts/comment_policy -no-baseline -v assembly` | raw exit 0；`0 finding(s) over [assembly]`（本文件 0；另提示 1 个 git-ignored Go 文件不在库内，属既有） |

#### D13.9.6 未验面与移交

1. `file`（rustviking）档未跑：本锚只跑 `localfile`。两档共用同一 `journalAddress` 实现，但未以 rustviking 端到端证 F2。
2. F2 三根走引擎级 `AtomicStore.Commit` 而非 `Run` 事务路径：fixture 的每代键集随代增长、capacity 可能跨过倍增，故 F2 的相等判据不用 generation 根；F1 与 F2 是正交两面，未在同一根上同时取两读数。
3. F1 的 2× 是以第 2 代为参照的相对界，不冻结 `==0`（允许实现枚举"存活键集＋尾"，只要不随代际增长、不达链的一半）；F2 的相等判据把 `capacity×8` 钉为该机制的不变量——改起始容量或 load percent 不改变判据，改按提交键控必红（正是 2.4 的回归面）。
4. CARRYOVER §四.1 的 2.4 行勘误、13.106 终审的 R4 项关闭、06.106 对 D6 补签行的复核，均由编排者/审者另办；本节只交凭据。

#### D13.9.7 越界自查

只写 `assembly/symptom_anchor_recovery_test.go`（新锚＋`orderLineBytesOf`/`orderLineShape`/`commitsOfShape`，对 HEAD `diff` 新增 107 行、删改 0 行，既有锚与 fixture 逐字节未动）与本文件 D6 一行＋本节；**未改 `tasks.md`、未动任何勾选**；未改生产/门/主规格/CI/docs；scratch 限 `/tmp/rr-r4/`（20 份运行日志＋2 份 HEAD 快照＋`mut/` 变异源与 overlay）；未动 git 索引/refs，未 commit/push/archive。
