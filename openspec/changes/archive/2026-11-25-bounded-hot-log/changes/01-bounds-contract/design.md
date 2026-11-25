# Design · 01

## D1 增长构成实测（01.101）

- 方法：合成提交（含大正文/工具结果）× 多代（≥3 个 cut 周期），**按键族统计 journal 帧字节**（事件键、window 元键、`blob:`、`ref:`、`checkpoint:`、`quiet:` 标记、其余）——checkpoint 切片 dump 与安静点标记就在同一帧空间内，按文件统计会漏掉构成；同时记录 cut cadence（提交数/时间窗）、每 cut 段字节分布、checkpoint 帧与 quiet 标记帧大小；标出“每事实固定开销”与“正文体”的分界；**测开档校验基线**（当前全日志逐帧 CRC 的耗时与字节，作策略优化前后对比）。
- 判据：若正文只存一份（帧内或 original 族二选一），则归档单位＝帧+其正文；若两处冗余先立删除项（单一真相），不得把冗余带进归档设计。
- 输出：`evidence/growth.md`（原始命令与数字）；可搬性结论按键族给出（哪些族随段走、哪些属恒热集合）。
- **实测触发（01.101，2026-11-23）**：正文在 `:evt:` 与 `blob:` 两处逐字节重复（768/768 事实帧 + 16/16 checkpoint 帧，双份合计 95.87% 帧字节；单份化可减半 journal）——按本判据**先立删除项 01.105**；02 的归档单位与段预算以单一化后构成为准。
- **01.105 已实施（方向 A，用户裁 2026-11-23）**：新写不产 `blob:`（`ref:` 账目照旧）；读面在 blob 缺席时经事件体取正文并以 ref 的 digest/length 复算（漂移具名 `ErrIntegrityMismatch`）；旧根 blob 只读、不清理、行为不变。**登记残留**：`message/content` 字段名契约横跨 `memory.EventBody`（编码）与 `memory/ledger`（解析）两包，无编译期守护——由 recall 锚与 digest 复算兜住；02 的段读面复用该路径时须复核该契约。

## D2 判据形式化（01.102）

- `hot_bytes(gen)` 取每代收尾（close 后、下一 boot 前）热卷目录字节和（不含归档卷）。
- 构成口径：热字节＝恒热集合（最新 cut 段＋开段＋索引侧）＋配置保留的旧段；平台成立的先决条件是 cut 正常发生——`hot_over_budget` 作为当前未收敛区间状态（0/1）每代记录，不记历史累计次数或最长超界时长；cut 不可达导致的超界如实入档，不得以调低 checkpoint 频率凑平台。
- 平台判据：对 N≥3 代序列，`max(hot_bytes[2..N]) ≤ min(hot_bytes[2..N]) × 1.3` 且 `hot_bytes[2..N] ≤ G[0] × 1.1`，**`G[0]`＝首个满窗代**（窗口未满前的代不参与参照；原 `hot_bytes[1]` 口径与「窗口未满允许增长」相抵，01.102 裁定特例化）。分母记录窗口内提交字节。
- 机理断言（随 S1 同锚）：末代关闭后，最新 cut 段与开段均不在 manifest（恒热）；全史封段数＝成功 cut 数（失败 cut 不封段；热卷常态仅余最新 cut 段＋开段）；`configured = hot_log_max_bytes + 2×当前热段最大日志区间 ≥ H` 的部署在正常负载（无故障注入）下 `hot_over_budget` 为零（状态为 1 即锚红）。
- 归档声明：`archive_bytes(gen)` 单调记录并声明线性；跨档读延迟 p50/p90 单列。
- S1 锚（第一天红）：`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`。

## D3 归档契约（01.103）

- manifest：双文件 generation swap，记录 `segment_id、键域摘要、字节数、sha256`；无最终名制品且无 manifest＝空归档态（零段，非错误）；有最终名制品而无 manifest、或 manifest 不可读→`ErrArchiveCorrupt`（fail-closed）；manifest 陈述而段缺→`ErrArchiveMissing`。
- 段索引：随段文件放置（键→偏移+长度），由封段器生成；热卷不为其付费。
- 定位读序：热索引→manifest→段索引→段；未陈述≠损坏：热/归档都未陈述→`ErrOriginalMissing`；manifest 陈述而段缺→`ErrArchiveMissing`。
- 配置：`archive_root`（空＝现状）、`hot_log_max_bytes`（>0 才启用切割与归档；`configured = hot_log_max_bytes + 2×当前热段最大日志区间` 低于 `H` 时以结构下限为准，见顶层 D3）、`archive_max_age` 可选；严格解码拒绝未知键与非法值。
- 契约修订清单：`session-checkpoint-material`“恒可读”→“恒可定位读取（在线直读/归档契约读）”；`bounded-recovery` 补热卷有界要求；GLOSSARY 增“归档（archive）/定位读/段界（segment boundary）/物理重放水位”四条，并写明物理重放水位（slot 头 `Seq/ValuesSize/OrderSize`）与逻辑 cut（framework checkpoint）的区分；升级说明一条。
- **承诺面（semantic commitments）**：`contracts/archive.md` 增设承诺表节：每条可宣称性质（有界/归档可读/恒热）＝`{id, claim（canonical 句）, condition, fails_when, anchors（正常绿锚＋故障可破锚成对）, canonical_docs（唯一家）}`；wiki/README/CHANGELOG/升级说明只引用 id 或逐字句，不复述改写。语义强度由条件句与锚成对承担，人只判条件充分性；强度词扫描仅作发现清单（warn-only）。
- **开档校验口径（归档后）**：boot 的完整性校验 SHALL 覆盖热段逐帧实算与 manifest 的结构/字节数；归档段与归档段索引 SHALL 在读时校验（摘要实算，`ErrArchiveCorrupt` 三态不变）。稳态 boot MUST NOT 预扫归档卷或读取归档索引内容；崩溃残留仍有已陈述热副本或待收缩 journal 前缀时，删除最后良好副本前 SHALL 对对应段／索引补验摘要，不得扫描既有归档代。开档索引重建 SHALL 有界于热窗。该口径使「热盘有界」同时落在**恢复成本**上；从未归档的根未开归档时行为与今日零 diff。
- **完整性校验分层契约**：四层各自防不同失效、不互为重复——①帧 CRC32C：真相字节完整性（半帧/静默损坏），坏了＝`ErrJournalCorrupt` 拒开；②`order.log` 行校验：提交索引行可定位性，坏了＝从日志重述（索引可弃）；③`slots` 记录校验：快速路径正确性，坏了＝重建；④manifest sha256：搬迁权威与跨卷内容确认，缺失/坏＝`ErrArchiveMissing/ErrArchiveCorrupt`。CRC 是检错非防篡改；boot 校验量下界＝热窗字节（MUST NOT 以段头自述摘要替代实算）。

## D4 签发区（01.104）

每实施叶填：候选指纹、已证/已驳项、所选/否决、精确写面、接口/配置变化、真实消费者、三锚全名与命令、崩溃切点注入方式、回滚触发、独立设计审结论。缺项则该叶 blocked。

## D5 停止条件

- 实测发现正文存在两处冗余且无法单一化 → 先立删除项，不回退判据。
- 归档需要改事实身份或账本责任 → 停，回设计门。

## D6 与 checkpoint 的接缝约束（2026-11-22 补；2026-11-23 用户裁定“cut 唯一时点”后重写）

- **时点守恒（结构保证，取代初稿“含锚段不搬”的内容检查）**：段界≡cut，切段与 checkpoint 是同一事件（顶层 D3）；搬迁水位＝段界 ≤ 倒数第二个 cut 的段，最新 cut 所在段与滚动开段恒热。故 `tailFactKeys` 所需的安静点标记与切片事实必在热卷，“找不到标记→退全史”不会因常规搬迁发生。恢复读法＝manifest→本代 cut→切片直读→重放开段。
- **界破姿态（用户裁定“等”）**：字节超界→请求提前 cut；安静点长期不可达（在飞操作不落地）→容忍超界，`hot_over_budget` 作为当前未收敛区间状态（0/1）与告警具名上报。MUST NOT 在非安静点封段，MUST NOT 以动态拒轮转/调大判据改写界破形态（初稿的“窗下限校验”删除）。
- **跨档 tail（仅损坏恢复）**：常规 boot 尾必在开段；仅当 checkpoint/安静点标记缺失或损坏（`tailFactKeys` 全日志回退）时可跨入归档，`archive-tail-read` 计入损坏恢复成本，S1 平台锚不得掩盖。
- **切片引用键**：checkpoint 切片（survivors/tasks/delivery body）按身份引用的正文可能已归档——恢复经定位读取，属契约允许（归档可读），延迟归恢复面。
- **pin 不阻搬字节**：`pinFact` 是内存驻留窗（delivery owed），搬迁只移动字节；但 owed 键的恢复读依赖归档在线，须列入 boot 前提。
- **归档卷边界**：`archive_root` 必须落在与热卷不同的卷/配额域，否则“目录有界”不等于“总盘有界”；部署文档须写明。
- **术语修正（2026-11-23 用户校准）**：checkpoint 与 tail 属**逻辑层**，journal/WAL 与段/归档属**物理层**；有界性是物理层属性，逻辑层语义与按身份可读承诺不因物理搬迁改变。文档与汇报一律写“物理层服务逻辑层”，MUST NOT 再称“正交/独立/两轴互搏”。
