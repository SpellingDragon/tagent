# 契约冻结 · 存储读取（store-reads）

候选 `52d7b664`。范围：点读/扫描契约形态、消费者迁移、不存在与读失败区分、索引失效语义、可选能力与包装。依据：audit #3/#4（均已复现）、[store-probes.md](../evidence/store-probes.md)、[design.md](../design.md) D4「存储读取形态」「索引失效」。

## 一、点读与扫描的契约形态（裁定；2026-11-22 修订）

**修订裁定**：`FactLog` 保持 `Put`/`Scan`/`Sync` 三方法不变（概念棘轮 only-down：四面撞 `iface-methods` 基线 3）。点读改为**消费者侧未导出窄口**——各消费者包声明一方法 `pointReadLog { Get(ctx, key) ([]byte, bool, error) }`，经类型断言取得；日志未声明点读时**具名拒绝**该读，绝不回退族扫描。先例：`addressedChainLog`（`assembly/commit.go:249`）、`CommitOrder`（`memory/ledger/atomic_store.go:67`）均为此形。`TierFacts.Get`（`memory/ledger/tier_facts.go:51-57`）与全部替身已带同签名，生产路径断言恒成立。
- 修订理由：接口增第四方法触棘轮（基线 3）且红线行超限（8>6）；窄口形态同时满足「零包边」「点读不扫族」「未知能力具名拒」三者，并把点读契约写在唯一消费者处。
- 消费者迁移（02.201/W4b）：`readReceiptLocked`（runtime/state）、`governanceTail`（governance）、`requestRecordOnFace`/`factOnFace`（approval）各以窄口断言取代 `log.Get`；缺席＝具名错误。
- 否决「四面接口」：only-down 棘轮；否决「断言具体 `TierFacts`」：包边与装配失败（原判仍立）；否决「保留 `Scan(完整键)`」：audit #3 全表枚举（N=5000→16384 slots vs Get 1–3）。
- `Scan(prefix)` 保留族枚举语义，`eachHeld`（journal.go:2128-2154）不动；`Keys`/`Namespaces` 不入本契约面。

## 二、消费者迁移清单（02.201 承接）

| 消费者 | 现形态 | 迁移后 | 锚 |
下表为迁移记录：「现形态」列是迁移前形态，「迁移后」列是当前形态（迁移均已落地；approval 面见下方同型后继）。

|---|---|---|---|
| `readReceiptLocked`（runtime/state/durable.go:807） | `d.log.Scan(key)` 取 pairs[key] | `d.log.Get` | 负锚已存在：`TestUnreadableReceiptFailsTheRestoreByName`（durable_test.go:1067，经真实 `RestoreState`）；正锚拟新增（签发区） |
| `governanceTail` cut>0 分支（features/evolution/governance/denial.go:653-666） | 逐 seq `log.Scan(完整键)`（:655） | 逐 seq `log.Get`；首个 `!held` 停＝洞语义不变（:659-661），读失败仍具名（:657） | 语义锚已存在：`TestGovernanceTailHoleContinuesPastTheHighestCommitted`（denial_test.go:412）；消费者锚：`TestGovernanceBootWorkTracksOpenItemsAndTail`（assembly/governance_checkpoint_test.go:416） |

同型后继（W4b 已完成）：approval `requestRecordOnFace`/`factOnFace`（runtime/task/approval/attention_log.go）原 `Scan(完整键)` 已迁 `pointReadLog` 断言点读（现 `reader.Get` 于 :161/:188），读失败具名；语义由 `TestApprovalHistoryReadFailureDoesNotMintANewRequest` 与 S2 面钉。

## 三、不存在与读失败的区分

audit 无新证，沿用契约原文：`Get` 继承 `Scan` 三分——（值,true,nil）持有；（nil,false,nil）不持有；（nil,false,err）读失败。消费者不得把 err 折叠成不持有（durable.go:799-806 契约注原文：读失败折成 nothing here 使已结通知重欠、驱动已关账户；docs/wiki/reliability/durable-delivery.md:71 同向）。落锚：§二负锚（恢复具名失败、零落座）＋governanceTail 具名错误路径；`invariantFactLog` 补 `refuseGet` 注入位，负锚随迁移面平移后仍红→绿可证。

## 四、索引失效语义（audit #4 已复现）

事实：`order.line` word 无校验和；`orderLineOnce` 只拒 -1 与 >orderSize（journal.go:1716-1726），0 在合法域，`eachNewestFirstAt` 越界重置（:2235-2238）不拦；walk `for pos := from; pos > 0`（:2287）遇零进入点一次不入——`GetParent` 静默答空，全序走查答真前驱（store-probes.md 疑点 B，rc=1×2 确定性）。

- 甲（word 哨兵化）：0 视为未陈述——读侧回退全序走查、写视图走既有 rebuild。依据：order line 最短为 8 字节校验和＋空格＋体＋换行（journal.go:1519-1522），合法 lineEnd 恒 >0，0 可安全哨兵化；改动＝orderLineOnce 谓词＋:98-102 编码注重写。局限：只堵零值形态，非零错位（audit 未查面）仍静默改答案。
- 乙（读侧身份校验，**推荐**）：seek 落位后，走查结束前必须命名进入键；未命名即弃 seek、自最新端重走。改动点＝`EachIndexNewestFirstFrom`（segment_store.go:1182-1200），覆盖其仅有的两个消费入口（assembly/read.go:286 `GetParent`、:350 `parentFromBodies`）。校验谓词不是「首候选＝key」——order line 一行可含多键，正确谓词是「走查是否命名进入键」。
- 推荐理由：word 损坏改变答案当且仅当进入点早于该键自身 commit 终点（晚于真值只多付走查、答案不变），早于必致走查漏命名进入键——乙对全类封闭，含零值形态；不改编码语义（:98-102「unknown 非 0」立约不动）；落位引擎无关（commitOrderSeek 面之上）。甲可作后续写侧收紧，非答案安全所必需。
- 与 missing/torn rebuild 承诺合拢（journal.go:86-97）：该注断言损坏 word「costs the walk its seek and never its answer」，今日仅对 rebuild 可探测形态成立；乙把该句在走查层落为强制不变量，02.202 在 :86-97 补一句点名身份校验。rebuild 机器（orderLineOf :1703-1712）不加新触发。
- 只读视图不得修盘：乙回退纯内存（自最新端重走，不写索引文件）；既有断言 read_test.go:725-727（读视图不落 order.line 字节）继续把守，新负锚复证。

## 五、可选能力与同能力包装

- `CommitOrder` 保持可选（atomic_store.go:67-85）：键序引擎不答提交序的立约不变；点读不并入 CommitOrder，也不因 `Get` 转正为必备。
- 同能力包装不丢契约成本（design.md D7）：包装必须把 `Get` 直通真实引擎；以 `Scan(prefix=key)` 实现 `Get` 的包装＝重建全表枚举，视为违约，镜像测试点名禁止。

## 签发区

- 候选指纹：`52d7b664521554fed8d7a5674ee1f934c410a3e3`（01.103 开工重核一致）。
- 已证引用：audit #3、#4；已驳引用：无（存储两疑点均复现，无驳回项）。
- 所选/否决（**修订后**）：选「`FactLog` 三方法不变＋消费者侧 `pointReadLog` 窄口断言」＋乙读侧身份校验；否决四面接口（only-down 棘轮）、消费者断言具体 `TierFacts`、保留 `Scan(完整键)`、甲单独落地（非答案安全必要条件）。
- 02.201 精确写面：`api/durable/fact_log.go`；`memory/ledger/tier_facts.go`（注释补 Get 面）；`runtime/state/durable.go`；`features/evolution/governance/denial.go`；`assembly/commit.go`（memoryFactLog 补 Get，:396-398 注释已声称 get）；`examples/contract/api_durable/FactLog_example.go`（stagedLog 断言口）；`tests/integration/state/delegation_lineage_record_test.go`（lineageFactLog）；`docs/wiki/reliability/durable-delivery.md`（:57 面清单补 Get）；＋各包镜像测试。
- 02.202 精确写面：`memory/ledger/segment_store.go`（身份校验与回退）；`memory/ledger/journal.go`（:86-97 承诺注补句，无行为改动）；`assembly/read.go`（回退与消费者谓词衔接）；＋镜像测试。
- 接口变化（**修订后，以 §一 为准**）：`FactLog` 公开面零变化（仍 Put/Scan/Sync）；点读契约为各消费者包未导出 `pointReadLog`；实现方仍各自带 `Get`（生产 TierFacts、memoryFactLog、stagedLog、lineageFactLog 与全部替身/包装），缺席即具名拒。
- 三锚落点：02.201 正锚拟新增 `TestReceiptReadIsTheOneKeyItNames`（runtime/state：收据读不进前缀计量、只付一键）；负锚已存在 `TestUnreadableReceiptFailsTheRestoreByName`；消费者锚 `TestGovernanceBootWorkTracksOpenItemsAndTail`（真实 boot 面）。02.202 正锚已存在 `TestPredecessorQueryBoundsTheWalkToTheChain`（assembly/read_test.go:682，含 order.line 缺失→全序回退与不落盘子证）；负锚拟新增 `TestGetParentSurvivesAZeroedOrderLineWord`（assembly，镜像 W1-B 主探针）；消费者锚＝正锚同测经 `NewTierQuerySource(tier.Segments).GetParent` 真实读面。
- 相邻回归：`TestLoadScanCountsRefusedScanRecordsNothing`（durable_test.go:1349，点读不入扫描计量）；`TestOrderLineIndexFollowsTheTableItParallels`（journal_test.go:1152，word 语义不动）；`TestAnchoredRebuildEqualsFullWalk`（run_test.go:141）；`OrderLineEnd`/`EachCommittedNewestFirstFrom` 其余走查入口行为不变（journal_kv.go:350-384）。
- 回滚触发：02.201 三锚任一红或收据/治理尾读仍入前缀计量；02.202 负锚红（损坏静默改答案）或 read_test.go:725 只读落盘断言红——即停，回候选基线零 diff；旧 `Scan(完整键)` 形态保留至 03.304 凭替代证据删除（audit 保留/删除清单）。
- 独立审：待。
