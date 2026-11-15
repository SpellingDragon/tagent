## ADDED Requirements

### Requirement: 已结事实搬迁非删除

退位 SHALL 以**搬迁**表达物理有界：已结事实（最新 checkpoint 不再点名 ∧ 无未结义务 ∧ 非尾，即存活键集权威 `live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)` 的补集）SHALL 被复制到冷层并从热层移除，冷层与热层同引擎同帧格式（journal.go:42/56）。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实（session-checkpoint-material/spec.md:44），SHALL NOT 改写正文与身份（memory-architecture.md:50）。

搬迁 SHALL 按**复制 → 校验 → 热层重写替换**三步顺序执行：复制按批（256 键/批，migrate.go:52 先例），一个事实的段三键与其 `ref:`/`blob:` 同批；校验 SHALL 新开冷层视图逐键读回并逐字节比对，且 `ref:` SHALL 有独立 `blob:`（migrate.go:121-175 先例）；热层移除 SHALL 只在校验通过之后发生。校验不通过 SHALL 具名拒绝（`ErrMigrateMismatch` / `ErrSnapshotCorrupt`）并保持热层原样不动。冷层只追加，SHALL NOT 重写已搬迁字节。

冷层字节随已结事实累积是本能力的**显式接受边界**：有界判据量的是热层 `journal.log` 字节，冷层厚度不在本能力治理范围。

#### Scenario: 已结事实搬到冷层

- **WHEN** 一条事实既不被最新 checkpoint 点名、又无未结义务、且不在尾（seq ≤ Offset）
- **THEN** 该事实的正文与索引 SHALL 被复制到冷层并逐字节校验通过，随后从热层移除
- **AND** 冷层持有的字节 SHALL 与搬迁前热层字节完全相同，身份（key/partition/window/seq）不变

#### Scenario: 未结义务不搬迁

- **WHEN** 一条事实仍被未结投递义务租约持有
- **THEN** 该事实 SHALL 留在热层存活键集，退位 MUST NOT 搬迁它

#### Scenario: 尾与 checkpoint 点名不搬迁

- **WHEN** 一条事实的 seq 大于最新 checkpoint 的 Offset，或被最新 checkpoint 的存活键集点名
- **THEN** 该事实 SHALL 留在热层，MUST NOT 因龄期或容量被搬迁

#### Scenario: 校验不过热层原样

- **WHEN** 冷层读回的字节与热层原字节不等，或某 `ref:` 在冷层没有独立 `blob:`
- **THEN** 搬迁 SHALL 以具名错误拒绝（`ErrMigrateMismatch` / `ErrSnapshotCorrupt`），热层 SHALL 保持搬迁前原样
- **AND** MUST NOT 发生任何热层移除

#### Scenario: headless-retired 审计正文保留

- **WHEN** 一条投递义务以 headless-retired 结清，其结清事实按既有语义保留审计正文（host_delivery.go:860-873）
- **THEN** 该审计正文 SHALL 按独立审计租约留在存活键集，MUST NOT 因「已结清」被搬迁

### Requirement: 已搬迁事实按身份恒可读

`GetEvent(key)` 对已搬迁事实 SHALL 仍返回**原字节**（透明回冷层取），SHALL NOT 因搬迁返回 missing（一级 §五 identity-read 不变量）。读答复 SHALL 三分：热层命中→原路径；热层未命中而冷层持有→冷层原字节＋原索引位置；两层皆不持有→不存在。冷层持有而正文读不出或校验失败 SHALL 具名拒绝（`ErrSegmentBodyMissing`，segment_store.go:205/546），MUST NOT 降级为「不存在」。

按身份读与读全史的消费者 SHALL 全部保持可读：查询面（assembly/read.go:118/174/202/236/328）、`CommittedEvent`（run.go:2281）、`eachCommittedEvent`（run.go:2304）、全史投递扫 `eachDeliveryFact`（host_tools.go:926）、重复提交检查（facts.go:370/401）、正式修复入口（scripts/recover-kv/main.go:140）；`EachEvent`/`EachIndexNewestFirst`/`Events` SHALL 仍遍历到冷层事实并保持提交序。原文 store 的 ref/blob 身份读同责；释放租约 SHALL NOT 使原文不可读（guard.go:17-21）。

冷层视图 SHALL 懒开：boot 路径 MUST NOT 开冷层，冷读发生后才开。冷读路径的位置索引按冷层键数计费是显式全史读消费者自身的成本，SHALL NOT 被计入 boot 有界判据。

#### Scenario: 搬迁后按身份读到原字节

- **WHEN** 一条事实已搬迁到冷层，消费者以同一 key 调 `GetEvent`
- **THEN** 答复 SHALL 是搬迁前的原字节与原索引位置，`found` 为真
- **AND** MUST NOT 返回 missing 或空正文

#### Scenario: 重启后仍按身份可读

- **WHEN** 进程在搬迁完成后重启，新进程以已搬迁事实的 key 读取
- **THEN** 冷层 SHALL 被懒开并答复原字节，身份读结果与搬迁前一致

#### Scenario: 读全史的消费者仍读到已搬迁事实

- **WHEN** 全史投递扫（`eachDeliveryFact`）或正式修复入口干跑遍历搬迁后的存储根
- **THEN** 已搬迁事实 SHALL 仍被遍历到并按身份答复正文，MUST NOT 出现静默缺口或孤儿误报

#### Scenario: 已搬迁事实再提交仍判重复

- **WHEN** 同一身份的事实再次提交，而其字节已搬迁到冷层
- **THEN** 重复提交检查 SHALL 读到冷层原字节并判为重复，MUST NOT 因热层未命中而二次落链

#### Scenario: 冷层读不出具名拒绝

- **WHEN** 冷层持有某键的索引而其正文读不出或帧校验失败
- **THEN** 读 SHALL 以 `ErrSegmentBodyMissing` 具名拒绝，MUST NOT 答复「不存在」

#### Scenario: 关闭退位后已搬迁事实仍可读

- **WHEN** 退位开关被关闭（回退态），存储根已含先前搬迁的事实
- **THEN** 热层回到只追加，已搬迁事实 SHALL 仍按身份从冷层读到原字节

### Requirement: 热 journal 字节有界于存活键集

持续写入 N 条事实并经折叠与投递结清后，热层 `journal.log` 的**真字节** SHALL 有界于存活键集＋尾，SHALL NOT 随历史事实总数线性增长（一级 §六 S1）。判据 SHALL 量文件字节（`os.Stat`），MUST NOT 以正文计数等代理量替代（D6.6）。多代累积与多任务夹具下同一判据 SHALL 成立。

#### Scenario: 持续写入结清后字节有界（S1）

- **WHEN** 夹具持续写入 N 条事实、完成折叠、投递结清，并跨多代累积
- **THEN** 热层 `journal.log` 字节 SHALL 有界于存活键集＋尾，不随 N 线性增长

#### Scenario: 关闭退位回滚必红

- **WHEN** 退位被关闭或本能力代码被撤回，同一夹具重跑
- **THEN** S1 判据 SHALL 红（热层字节随历史增长），作为回滚证据

#### Scenario: 真字节而非代理量

- **WHEN** 验收锚报告有界性
- **THEN** 报告 SHALL 以热层文件字节为量，MUST NOT 只断言已提交正文条数

### Requirement: 退位搬迁只在排他安静点内

安静点是 drain 不是排他 fence（assembly/checkpoint.go:23/:102-107），退位搬迁 SHALL 另持排他安静点：Run 停写闸（提交入口 facts.go:84）→ drain 归零 → 热层重写替换持 `journalKV.mu`（journal_kv.go:105）。`¬live` 判定 SHALL 只在闸内做，闸外只读不搬（在途歧义：originals-before-segments，孤儿与在途不可区分）。

停写闸 SHALL 挡写不挡读；闸关时新提交 SHALL 等待并在闸开后按原序落链，MUST NOT 以拒绝落链表达停写（拒绝＝丢事实）。等待 SHALL 有上限，超时 SHALL 具名上报。闸内 MUST NOT 持 Run 锁读切片，持有者自身提交 MUST NOT 等待向它要状态的闸（checkpoint.go:18-26 的排序纪律）。本能力 SHALL NOT 引入跨进程锁：同根第二实例由生产 open 路径拒绝（config.go:174）。

#### Scenario: 搬迁期间提交等待后落链

- **WHEN** 搬迁持有排他安静点期间有新事实提交
- **THEN** 该提交 SHALL 等待闸开，随后按原序落链，事实不丢
- **AND** 搬迁期间的读 SHALL 继续答复热层原样内容

#### Scenario: 闸未静拒绝搬迁

- **WHEN** 流水线仍有在途提交（drain 未归零）而搬迁被请求
- **THEN** 搬迁 SHALL 以具名错误（`ErrColdArchiveNotQuiet`）拒绝，热层与冷层 SHALL 保持原样

#### Scenario: 停写等待超时具名上报

- **WHEN** 停写等待超过上限
- **THEN** 系统 SHALL 具名上报超时，MUST NOT 拒绝落链或静默丢弃该事实

#### Scenario: 闸外不判定存活键集补集

- **WHEN** 排他安静点未持有
- **THEN** 退位 SHALL 只读不搬，MUST NOT 用在途视图决定任何键的搬迁

### Requirement: 热层重写保持提交序

热层重写存活键集 SHALL 按**逆帧序去重**反写（每键只留最新记录），因为 order 按帧记、一键在每条重写它的记录处各点名一次（journal.go:115-121）。重写后最新 checkpoint SHALL 仍被认定为最新（`checkpoint:<partition>:<seq>`，api/event/checkpoint.go:75-83），分区各自编号 MUST NOT 跨分区重排。单帧 SHALL 不超过 32MiB（journal.go:56），超限 SHALL 具名拒绝而非静默截断。

#### Scenario: 重写后最新 checkpoint 仍是最新

- **WHEN** 热层重写完成并重开视图
- **THEN** 提交序走查 SHALL 先遇到最新 checkpoint，MUST NOT 认错旧 checkpoint 为最新

#### Scenario: 同键多记录只留最新

- **WHEN** 一个键在热层历史中被多次重写
- **THEN** 重写后的热层 SHALL 只保留该键的最新记录，其字节与重写前最新记录一致

#### Scenario: 既有保序锚保持绿

- **WHEN** 退位能力落地后重跑既有保序测试
- **THEN** `TestJournalStatesItsCommitOrderNewestFirst`（journal_kv_test.go:72）与 `TestEachIndexNewestFirstStreamsTheCommitOrder`（segment_store_test.go:508） SHALL 保持绿

### Requirement: 冷层损坏具名拒绝

冷层损坏 SHALL 具名拒绝，MUST NOT 静默丢事实：帧校验失败或正文缺失 SHALL 以 `ErrSegmentBodyMissing` 拒绝；`ref:` 无独立 `blob:` SHALL 以 `ErrSnapshotCorrupt` 拒绝；复制读回不等 SHALL 以 `ErrMigrateMismatch` 拒绝。冷层跨趟累积，**冷层根非空本身不是拒绝理由**；但冷层已持有某一身份的字节时，搬迁 SHALL NOT 覆盖它（一身份一不可变事实）：字节相同 SHALL 幂等跳过复制，字节不同 SHALL 具名拒绝。任何损坏处置 SHALL NOT 删除或改写已提交事实。

#### Scenario: 冷层帧损坏

- **WHEN** 冷层某帧完整存在但校验失败（损坏注入手法参 storage_capabilities_test.go:381）
- **THEN** 该身份的读 SHALL 具名拒绝，MUST NOT 答复不存在或跳过

#### Scenario: 冷层孤儿引用

- **WHEN** 冷层某 `ref:` 没有对应的独立 `blob:`
- **THEN** 校验 SHALL 以 `ErrSnapshotCorrupt` 点名该键拒绝，热层 SHALL 保持原样

#### Scenario: 冷层已持同一身份不盖写

- **WHEN** 待搬迁键在冷层已持有字节（先前趟搬迁过，或本趟重跑）
- **THEN** 字节相同 SHALL 幂等跳过复制，字节不同 SHALL 具名拒绝，MUST NOT 盖写既有冷层字节
- **AND** 冷层根非空 SHALL NOT 单独成为拒绝搬迁的理由（冷层跨趟累积）

### Requirement: 退位能力 journal-only 且分档诚实

退位 SHALL 只在 journal 档实现：`AtomicStore` 缝（atomic_store.go:50-61）表达不了热层替换，故 SHALL 新增 journal-only 引擎能力（参 `commitOrder` 先例 segment_store.go:224 / journal_kv.go:150），由 `journalKV`/`FileSegmentStore`/`kvOriginalStore` 实现 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned`（lifecycle.go:69-79）与搬迁。rustviking file 档无跨键原子 SHALL NOT 实现退位，其能力缺失 SHALL 显式声明，MUST NOT 伪造保护已生效；memory 档不持久 SHALL NOT 搬迁。

退位开关与冷层根 SHALL 是存储配置面的可声明字段（`StorageConfig`，config.go:46；`hostStorage`，host_document.go:76，经 host.go:635 接线），缺省关闭＝热层只追加（回退态与基线零 diff），MUST NOT 以硬编码常量表达部署可变选择。

#### Scenario: journal 档实现搬迁能力

- **WHEN** 以 journal 档打开存储并启用退位
- **THEN** 引擎 SHALL 答复枚举、待搬迁标记与搬迁能力，搬迁按复制＋校验＋热层重写执行

#### Scenario: file 档显式能力缺失

- **WHEN** 以 rustviking file 档打开存储
- **THEN** 退位能力缺失 SHALL 被显式声明，MUST NOT 假装搬迁已生效，MUST NOT 执行跨键非原子的热层替换

#### Scenario: 缺省关闭即只追加

- **WHEN** 部署未声明退位开关
- **THEN** 热层 SHALL 保持只追加，行为与退位能力落地前一致（基线零 diff）