# 注释面收敛：矛盾、缺陷与待裁事项（交接归纳）

本文是 `restrict-comments-to-godoc-and-index` 全程运行台账（tasks.md §291–§367）的**决策视图**：把散落在各批里发现的矛盾、门缺陷、需您裁决的点集中一份，供下一阶段直接使用。所有条目均标注**可复核的证据位置**（台账节号或 `file:line`），数字取自本窗口的真回执，不含推断。

## 1. 现状

| 项 | 值 | 证据 |
|---|---|---|
| 棘轮基线起点 | **522** | §359 提交记录 |
| 当前基线 | **53**（`audit-marker 3`／`doc-not-name-prefixed 16`／`free-standing 12`／`missing-test-responsibility 22`） | `scripts/comment_policy/baseline.json` |
| `test-doc-not-one-sentence` | **0（该规则族首次清零）** | 同上（键已消失＝写入器只记非零键，见 §359） |
| 归零文件数 | **33** | §358–§367 |
| 已提交 | `66a9edc`（覆盖 §306–§358），分支 `dev` ahead 6，**未推送** | §359 |
| 未提交工作树 | §360–§367 的注释面改动 | 各批 `comment-check` 回执 |

**一句话判断**：能靠我自己推进的都推完了；**剩余 53 条中约 41 条卡在"需要您裁决"，不是卡在工时**。

## 1b. §368 之后的状态差（先读这一节，再引用下面的条目）

§368「门·堵洞」批次已把下面 **A 组的 9 项纯缺陷全部修完并有契约测＋CLI 实证**，请按"已闭"对待，不要再据此立项：
**A1**（许可证头误报）、**A2**（`_` 编译期断言）、**A6**（索引目标依赖 CWD）、**A7**（`firstLine` 按字节截断）、**A8**（`indexTargetRoots` 注释谎称 README 合法）、**W1**（指令＋散文同组的整组豁免逃生舱）、**W3**（写端单调＋`-force-raise`）、**W4**（缺 `openspec` 树时静默少一整个轴）、**W7**（`index-root` 诊断文案与白名单不同源）。

⇒ 基线由本文所载 **53** 降至 **42**（`audit-marker 3`／`doc-not-name-prefixed 6`／`free-standing 11`／`missing-test-responsibility 22`）；`doc-not-name-prefixed` 的 **−10 全部是 A2 的 `var _ Iface` 断言**（7 生产＋3 测试），所以第 5 节里"A2 需改门 3 条"与"前缀项待裁"两类合计实际只剩 **6 条**（全在 `config.go` 字段 doc）。

**§369–§370 之后的状态**：可自助项已全部做完，基线 **53 → 42 → 29 → 20**（现值 `audit-marker 3`／`free-standing 4`／`missing-test-responsibility 13`；**`test-doc-not-one-sentence` 与 `doc-not-name-prefixed` 两族整族清零**，归零文件累计 **47 个**，§370 新增 9 个：`modelref_test.go`、`partition_collision_test.go`、`tests/async_result_test.go`、`tests/async_task_e2e_test.go`、`tests/resident_e2e_test.go`、`tests/offline_bench/offline_bench_test.go`、`tool/action/{tmux_executor,tmux_monitor_scenario,tui_timeout}_test.go`）。⇒ **剩余 20 条无一条是工时问题**。⚠️ §370 同时纠正我 §369.8 的口径错误：我曾把 22 条 resp 笼统说成"需裁决"，实际全量锚点匹配后有 9 条本来就有落点——**判"无落点"之前必须先导出锚点目录做全量匹配**。


**§371 之后的状态（代码债＋对账批）**：门仍为 **20**（本批中途我自己写过两条体内散文注释，触发 `free-standing` 20→22，删净归位后回到 20，`0 beyond baseline`）。登记的代码债两条已做完：**18.8**（`prompt/loader.go` 的 `LoadBootstrap` 由 `errors.Is(errors.Unwrap(err), …)`＋字符串兜底改为单条 `errors.Is(err, os.ErrNotExist)`，**同文件 `225` 行 `LoadFromDirectory` 早已是正确写法 ⇒ 属同文件内不一致**；契约半边"其他读取失败整体中止"补进 doc）与 **26.6**（删 `websearch_test.go` 的 `callable :=` 冗余中转）。新测 `TestLoader_LoadBootstrap_OrderFileMissingSkipsOtherFailureAborts` 以**双向变异**证非空壳（去跳过 FAIL at `:399`、去中止 FAIL at `:412`）。阶段项对账闭合 **8.1／8.2／9.6**（判据即这三条自己指定的扫描器单点：`missing-package-doc`、`missing-symbol-doc` 均已实现于 `main.go:826/889` 且**计数 0**；`docs/api/index.md` 只列包清单并标注生成物禁手改；`check-openspec` rc=0）。分域 0 违规实测：**`agent/compress`／`agent/governance`／`agent/reliability`／`agent/task`／`memory`／`event`／`plugin` 七域均 0**。⚠️ **本窗口查出的读数口径错（影响此前每一批我给您的"明细"）**：棘轮作用域是 `baseline.json` 的 `dirs=[".","examples/wechat-bot"]`（= `POLICY_DIRS`），而我每批取明细都用裸 CLI（仅 `[.]`）⇒ **`examples/wechat-bot` 的 3 条每批都被漏看**；§369 报的 `audit-marker`/`free-standing` 成员也**归错到 `scripts/`**。权威 20 条成员见 §371.2，本文下面的 B7/C1–C4 引用的行号据此才第一次对得上。

**§372 之后的状态（D8 准出项全量实测，零源码改动）**：门仍 **20**。四条准出项**第一次拿到仓级真回执且全部仍不可勾，但每条阻塞已缩到一句话**——9.4(a) 对 D0=`03a0cc3^` 复验 171 个生产文件 ⇒ **166 处 code identical ＋ 5 处例外，其中 4 处均有台账支撑，只剩 `plugin/memory_plugin.go` 一处仍是 §309.6 未答的三选一**；9.4(b) 查明其真实阻塞是 **merge-check 的不变量与 G 批职责对立**（用 D0 作 base 会把 G 批正当注释改动判成 `PRODUCTION-CHANGED`），故 9.4(b) 与 **14.5"按批设基线"是同一道锁**；9.3 全量三门跑齐（wechat-bot 0/0/ok；36 包 `-race`＝31 ok／4 无测／1 包失败，`DATA RACE` 0）⇒ 差 **C9** 的处置；9.2 的"编号残留"半边**早有机器判据且为绿**（`codetools name-check`，其提示语即"keep domain vocabulary like Int64/L1/V2"，与我逐条人工分类互证一致），差"每个测试文件都有索引"那 13 条；9.5 的断言计数对账半边＝**HEAD 6,470 → 6,532（＋62，不降）**，差"需承接条目逐条验证落点"那专批。

**§373 之后的状态（按建议顺序执行的第一批：三处真源落地＋两笔禁用测处置）**：门 **19 → 16**，基线已收紧为 `{audit-marker 2, free-standing 1, missing-test-responsibility 13}`。**已闭、请按"已闭"对待**：**C7**（删 `wiring.go` 的死分支与 "nil → default enabled (D1)" 假注释；实读三处后真相是 `config.go` 字段 doc 与 `local_file_kv.go` 本已一致，wiki §十八 `:1334-1339` 也早已承载 ⇒ 我"三处口径互斥"的说法**过重**，错的只有那一条体内注释）、**C5**（§13.1 依实现改为 **default-nothing**：`build_agent.go:276` 恒附自身分区 → 两处 `resolvePartitions` 未指定返回 `nil` → `QueryEvents` 零扫静默 0 条；原伪代码 `return allPartitions` 与"漏配将扫全库"**均为假、方向相反**；另删 wiki 内的 Go 伪代码块并补 `#read-paths` 锚点）、**B6**（`quiet_timeout` 下限并入 `tmux-action.md#quiet-vs-dead` 的"同类校验还有三条"，数值单点仍归 `TmuxMonitor.stableWindow`）、**C2**（`agent/tool_agent_test.go` 停用块判**删除**——它在 `agent` 包调 `rl` 的非导出 `record()`，本包永不编译；测试面 `411 → 411` 自证零变化）、**D-13**（`rl/trajectory_recorder_test.go` 停用块判**复活**并 `--- PASS`，恢复一条此前无任何活测覆盖的组合契约；见 `doc-drift-ledger.md` 新增的「D-13 处置」行）。⭐**C1 被我自己推翻并升级为 C1′"必须移植"**：`/healthz`（`rl/http_api.go:273`）与 `/task`（`:237`）路由仍在生产码、尸块 12 个测名在 `http_api_closeout_test.go` 的 14 个活测里**零同名接替** ⇒ 删除＝"删了没搬"；`NewHTTPAPI(agent AgentLoop)` 已是接口，本地 fake 即可复活那 10 个 HTTPAPI 用例（这是**待做工作项**，不再是待裁项）。**剩余报项 16 条的构成**：13 条 resp（等 B 族 (a)/(b)/(c) 与 B7）＋ `rl/http_api_test.go:3` 的 audit＋free-standing（随 C1′ 自然归零）＋ `test_stores_test.go:15` 的 audit（属 B1）。⚠️ **新立 9.7"无双重真源"候选**：`config.go:424-431` 的 `FSync` 字段 doc 用 8 行英文复述 wiki §十八 已承载的命题，应收成一行契约＋索引——但 §十八 目前无 `<a id>` 锚点，需先加锚点（未擅自扩大范围）。⚠️ **本批我另犯两起写盘事故**：heredoc 追加 §373 连续两次未落地（`SyntaxError`，且 `"373 written"` 未打印才是真相）、以及一次 **SearchReplace 破坏表格**（`old_string` 只取表头格 ⇒ 「D-13 处置」与 D-14 并成一行、D-14 标签消失），已修回并按规程 40 自验（`doc-drift-ledger.md` 1 表／35 行／0 问题）。⇒ **规程 41**（长中文台账走文件编辑工具、写后必回读条目列表与计数，计数只取回读值）与**规程 42**（在表格文件插行时 `old_string` 必须是完整一行，不得只取行首片段）新增。

**§374 之后的状态（C1′ 移植批：门 **16 → 13**、`free-standing` 整族归零）**：⭐**C1′ 已做完**——`rl/http_api_test.go` 的 331 行尸块改写为 146 行活测（本地替身 `messageRecordingLoop` ＋ 6 个新测），`rl/swappable_model_test.go` 另补 1 个（`Info()` 委托此前在活面上**零断言**）。**12 个尸块测名逐一销账**：1 同名复活、8 个映射为 5 个新测、1 个内核迁到 `swappable_model_test.go`、**3 个不搬并有理由**——其中 `TestHTTPAPI_PostTask_NoCallback_NoError` 的期望"未装回调带 `llm_base_url` ⇒ 202 无错"**已被现设计反转**（`http_api.go:497-501` 现在 400 `endpoint_redirect_unconfigured`，且新分支早有活测），原样复活等于把一条被推翻的契约钉回去。测面 `37 → 44`，`comm` 对账证明**零声明丢失**；**变异验证 7/7 KILLED**（并新立判据：变异必须保持可编译，否则 `[build failed]` 不算红）。承载面：要钉的 6 条契约里 **4 条 `#http-api` 早有承载**，只为剩下 2 条新增小节「状态读取与未命中路由」（B 族 (a) 路线，未放宽任何规则）。⚠️**纠正 §373 两处**：(i) 我把该文件记成"2 条报项"，**实为 3 条**（`:1` 的 resp 本就在那 13 条里；**本文 C 表原文"3 条基线项"一直是对的，错的是我的构成拆分**）⇒ 本批 −3，余 **13＝12 resp ＋ 1 audit**，**非 resp 报项只剩 B1 那一条**；(ii) §372.4 的"断言计数 6,470 → 6,532"**不可复现**，已换成确定性 python 口径实测 **HEAD 7,931 → 7,995（＋64）**（结论方向不变：不降），并查明**本环境 `grep -E`／`git grep -E` 都不认 `\b`**，此类计数只能用 python。⇒ 门 **13**＝`{audit-marker 1, missing-test-responsibility 12}`，`free-standing` 为**第三个整族归零的规则族**。

**§375 之后的状态（最后两处自助收益：门 13 → 11，注释面已无我可自助的一项）**：`tests/soak_test.go` 与 `tool/action/tui_integration_test.go` 的落点**经逐节读原文证实成立**，各补意图 doc ＋ 契约索引后 resp **12 → 10**。两处核实都不是"标题对上就算"：soak 我先否证了 `#compaction-integrity`（§二十六三条只管压实内部完整性），真载体是 **§16.5 硬契约表**（契约 1 压缩只作用于视图与可见性／契约 5 召回底线／契约 8 压实 crash-safe 不丢事件）＋ §16.6「层位绝不编码进键」；tui 我先读完 `tmux-action.md` 十节，确认它只承载**非 TUI** 那半边（静默⇒Stable 不击杀），`TimedOut` 在全库 wiki **零命中**，真源在 **go doc 层**（`tmux_executor.go:159` 已写明"TUI 静默越阈的终态、不做假死/假活探测、随即移出监控"）⇒ 索引指向职责面正确的 `tool-architecture.md#tmux-monitor`，命题不重复叙述。⭐**由此新登记 D-46**：wiki §9.2 的枚举代码块只列 6 个 `SessionStatus`（漏 `SessionTimedOut`）、§9.1 状态图无 TUI 超时出口 ⇒ 属"复述码面枚举、漏项即成假陈述"的 **9.7 双重真源族**，与 373.8（`FSync` 字段 doc 复述 wiki §十八）同族，**两处一并待您对 9.7 定口径**（出路都是"收成指针"而非"补全复述"）。回执：门 **11**＝`{audit-marker 1, missing-test-responsibility 10}`，`LINT rc=0`、`check-openspec rc=0`、`gofmt` 净、**`go vet -tags 'soak integration' ./...` rc=0**（这两文件在构建标签后、默认套件从不执行，故只做编译核对未实跑）；生产码零改动。⇒ **余 11 条全部待裁**：10 resp＝5 条 B7 ＋ 4 条已否证无落点 ＋ 1 条 B1；1 audit 与 B1 同源。

**§376 之后的状态（9.7 双重真源两处收敛：门不移动，收益在真源唯一性）**：按您给的默认路径做完两处。**373.8**：`config.go` 的 `FSync` 字段 doc 由 8 行英文复述收成 2 行契约＋一行索引（并清掉一行 `//:` 断行残渣与 `localfile-minimization ruling` 这个**英文决策名残留**——它属本 change 要清的迭代坐标，只因用词是英文才没被词表抓到）。ⓘ **我自己的前提错了一半**：373.8 写"§十八 无锚点需先加"，实为章标题无锚但**小节早有 `#local-file-kv`**（`memory-architecture.md:1317`，1331 行已完整承载 `WithFSync` 被忽略的命题）⇒ 直接指过去，无需先动 wiki。⭐**给"绿"补了反向探针**：把锚换成不存在的 `#nope-missing-anchor` 后门即报 `index-anchor-unknown`（`config.go:424`）⇒ 门的锚校验是**活检**，不是没实现；探针用成对 `perl -i` 正反替换**不借 git**（`config.go` 带前批未提交改动，`git checkout` 会一起吞掉）。**D-46**：按"**收成指针**"而非"补全复述"处置——§9.2 的 6 态 `const` 代码块（正是这份复述漏了 `timed_out`）删成"取值与语义以码面 `SessionStatus` 的 go doc 为唯一真源"，§9.1 补两条经代码核实的边（`Stable --> TimedOut`／`TimedOut --> [*]`，依据 `tmux_monitor.go:607-628`＋`settle.go:44,56`）；ⓘ **未审范围已划清**：§9.1 其余边未逐条核（`FakeAlive` 入态、`ModeResident` 刷新边），§9.4 故意不加 TimedOut 行（加即第三次复述），§9.5 复述 `DefaultMonitorConfig` 六项默认值今日实测**与码面一致故非漂移**、但违背 `tmux-action.md:16` 自立的"本页不另存一份常数"规矩 ⇒ 同族候选，登记未动。⇒ **新增待裁（9.7 族的三条同类项）**：§9.1 其余边的核实修正、§9.5 默认值代码块收指针、以及**是否全仓普查"复述型字段 doc"**（373.8 型残留）。棘轮侧读数：**门仍 11、基线未变**（此类收敛不减报项，我不用它冒充进度）；`check_comment_only 03a0cc3^ config.go` ⇒ **code identical**（vs D0 生产例外仍 6）；根包全量 `ok 59.028s`；`LINT rc=0`、`check-openspec rc=0`；工作树 **70**；未提交。

**仍待您裁、本文未改写的部分**：A3/A4/A5（词表宽严与裸坐标是否纳入）、B 族的 (a)/(b)/(c)、B7 的 (a)/(b)、**C8**（真实 API 测是否再加构建标签——`testing.Short()` 已使其在默认套件 SKIP）、**C9**（36 包并发 `-race` 下那处负载敏感失败，9.3 的"零豁免"是否接受）、**§309.6**（`plugin/memory_plugin.go` 运行期日志串，与全仓余下 17 处 `§` 字符串一并定）、**C3 余量**（`owner_retirement.go`／`partition_collision.go` 的占位空引用）、D1/D2 的收口口径。ⓘ 已闭项不再需您处理：**C4**（`wiring.go:328`）随 **C7** 一并清除；**C1/C2/C5/C7/B6/D-13** 见 §373 状态差；**C1′** 已在 §374 兑现（不再占您裁决，也不再是待做项）。ⓘ 其中 **A3 需先修正事实**：`used to` **不在词表是有意设计**，`main_test.go` 有 `TestUsedToIsNotAResidueWord` 钉住（理由：与中文"用于"无法词法区分），故 §365.4 记的"门侧第 29 项＝缺词"半条结论不成立——是否补词是**取舍**，不是缺陷。

## 2. 待您裁决的四组

### A. 门自身语义缺陷（遵从即出错，或漏检）

| # | 事项 | 证据 | 选项 | 我的建议 |
|---|---|---|---|---|
| A1 | **许可证头被判 `free-standing`**：`testing.go` 是"版权头＋空行＋package doc＋package"，Go 只把后一组当包 doc ⇒ 版权头进了违规集；`config.go` 无版权头所以不被报 | §367 现场；`testing.go:1` | ①门豁免包头注释组 ②删版权头（荒谬） ③全仓统一加版权头再豁免 | **①** |
| A2 | **`_` 编译期断言的 doc 不可能合规**：GenDecl 分组未从 `groupNames` 剔除 `_` ⇒ 要求 doc 以 `_` 开头 | `tool/action/settle_test.go:12`、`examples/wechat-bot/file_delivery_test.go:82`、`tests/offline_bench/offline_bench_test.go:157`（**测试侧 3 条前缀项全是它**）；代码点 `main.go:947–952` | ①门豁免 `_` ②把断言改成有名变量（属代码改动） ③留在基线 | **①** |
| A3 | `auditMarker` 词表**缺英文 `used to`／`formerly`**（含该词的 doc 零命中） | §365.4（门侧第 29 项）；Go 侧单验 `used to\b` 本身可匹配 | ①补词表 ⇒ 基线涌入 ②不补，靠人工 | 与 A4 一并裁 |
| A4 | 同词表**误报合法事实陈述**：`no longer`／`legacy` 命中对**当前契约**的正常描述 | §352.2（门侧第 15 项）、§364.2 实物 | ①加句式/语境界别 ②继续逐条改措辞 | **①**（我这几批全在付这个成本） |
| A5 | **裸坐标未纳入**：`\bD\b?-?\d+`／`F\d+`／`round-NN` 形态不在 `externalCoord`；实测注释内 **150 处／53 文件**，现规则只抓 4 处（**漏 97%**） | §360.5 量化；§365.5/§366.4 每批都手工清了若干（`56bf24c3`、`（4.2 核心）`、`C 方案`、`A1 回归`、`T-D`、`Major 回归`、`审查 M1/M2/M3`、`W4`、`D1-B`、`cold-eyes P1-3`、`D8`、`B-fix/s67`、`12.1`） | ①扩规则⇒基线约 +146 再压 ②定为"不做机器化，靠审阅" | 需您定：这决定"注释面"是否真算收敛 |
| A6 | 索引目标解析**依赖 CWD**：同一子树内跑 16 条、从根跑 10 条（2 条 `index-target-missing` 是幻影） | §361.6（门侧第 28 项） | 修成相对仓库根 | 修 |
| A7 | `finding.Text` **按字节截断**，中文被切出无效 UTF-8 | 门侧登记项 | 改按 rune 截断 | 修 |
| A8 | `indexTargetRoots` 的注释谎称"plus each module's README"，`git log -S` 证明**生于 ee6d48f 即从未实现** | §361.5（门侧第 27 项）；`main.go:84–86` | ①删注释 ②实现 README 分支 | **①**（别把谎留着） |

### B. "无落点"族：这些命题在 wiki 里没有承载，删了就是毁价值

| # | 命题 | 证据 | 说明 |
|---|---|---|---|
| B1 | 测试用 store 必须挪出工作树的**完整根据**（`resources.acquire` 在分派前无条件 `MkdirAll`＋写锁、`hottest-sub1/`/`own-sub1/` 的 lock 与 journal 曾被提交跟踪、`t.TempDir()` 失败不得回落相对路径、`sync.Map` 以 TB 指针为键） | `test_stores_test.go:15` 的 **21 行 doc**，门报 `audit-marker` | §367 按"承载先于删除"**没动**。它是最典型的一例：**唯一合规出路是写一篇 wiki** |
| B2 | `offline_bench` 的隔离语义 | `tests/offline_bench/offline_bench_test.go:12`（缺索引） | §360 起登记 |
| B3 | `ZhipuCall` 的线路契约（Bearer／`search_result` 映射） | §363.6 | 无 wiki 落点，未硬凑锚点 |
| B4 | MCP 注册表**热同步**／严格解码**只作用于 `mcp_servers` 子树** | `tool/mcp/registry_test.go`；`#strict-decode` 经逐行读原文证实**不承载**热同步（§366.2–366.3） | 现索引只在"文件级职责"上成立 |
| B5 | `spec` 工具 **argv 不受模型影响、无 shell 插值**的安全契约 | `tool/spec/spec_test.go`；wiki 全篇 `grep argv/白名单/shell` 无落点（§366.3） | 同上 |
| B6 | `action` 工具的 **`quiet_timeout` 下限＝稳定窗**（非 TUI 60s／TUI 90s，更小值直接拒绝，理由"会在会话稳定前误杀"） | 代码 `tool/action/action_tool.go:387-391`；`tmux_monitor.go:493-495` 明写该下限须与 monitor 实配同源；`docs/wiki/tool/tmux-action.md#quiet-vs-dead` 只讲 opt-in 与回落默认，**不含下限** | §369.5 查出：输入校验契约的归宿是 wiki（`wiki-code-sync` 缺口），我暂存在测 doc bullet 里，非正当落点 |
| B7 | **`scripts/` 与 `examples/wechat-bot` 的测试索引没有合法目标**：索引根白名单只有 `docs/`，而 wiki 里没有注释门／`codetools` 的页面；bot 模块的契约文档只有 `README.md`／`AGENTS.md`（裸 README 被夹具 `outside.go` 明确判拒） | §370.4；`main.go:84-91`＋`main_test.go` 夹具 | 5 条 resp 卡在此处（`codetools/check_test.go`、`comment_policy/main_test.go`、bot 的 3 个测试文件）。出路：(a) 在 `docs/` 写工具契约页；(b) 裁"工具自测豁免"并写进 wiki；(c) 放开 `openspec/specs` 作索引根（不建议——与 A8/W7 刚清掉的"谎称合法"冲突） |

**需裁**：B 族的处置只有三条路——(a) **允许新增 wiki 承载**（则我逐条写，注释面随后瘦身）；(b) **允许这些文件保持报项**（基线停在个位数，本 change 以"带残余"收口）；(c) **允许删除**（我不建议：B1 那 21 行是踩出来的教训，删一次会再犯一次）。

### C. 越出注释轴、必须回到代码面决策的

| # | 事项 | 证据 | 影响 |
|---|---|---|---|
| C1 | `rl/http_api_test.go`：**整个测试内容被注释留在源文件里**（`/*` ＋ "Original test content moved to comment block below"），且 TODO 依赖**全仓不存在**的 `mockAgentLoop` | §367 现场（L3–L8） | 3 条基线项（audit＋free-standing＋responsibility）都源自这个死文件 |
| C2 | `agent/tool_agent_test.go:618`：**整段 `TestClose_TrajectoryRecorder` 被块注释禁用** | §367 现场 | 2 条基线项 |
| C3 | `owner_retirement.go:249`／`partition_collision.go:115`：`var _ = fmt.Sprintf // keep … for future` 一类的**占位保留** | §367 枚举 | 2 条生产侧 free-standing |
| C4 | `wiring.go:328`：函数体内 `kv.WithFSync(false)` 的决策性注释 | §367 枚举 | 1 条，属生产侧，需"搬进 doc／删"的裁定 |
| C5 | `memory-architecture.md §13.1` **与实现对立**；且 §13.1 无锚点可指 | 待裁第 1、2 项（§347 前后登记） | 文档真源之一存疑，优先级最高 |
| C6 | ~~`cross_generation_test.go`（3）／`org_diagnostics_test.go`（2）／`rl/trajectory_recorder_test.go:118`~~ 的体内说明注释 | §367 枚举 ＋ §369.3 逐行读原文 | 前两者的浮空块已确认由 `execution-generations.md` §九、`org-hot-reload.md:98/107` 承载，据此删除或压缩；⚠️ **:118 是我 §367 的分类错误**——它是 `/*` 包裹的整段禁用测，与 C1/C2 同族、**不可自助**，并入代码面裁决 |
| C7 | **`FSync` 三处口径互斥**：`config.go:421-428` 字段 doc 说 localfile "ACCEPTED AND IGNORED，后端无 WAL/fsync 机制"；`wiring.go:324-329` 却读它并 `kv.WithFSync(false)`；同处体内注释又写 "nil → default enabled" | §369.5 查出（字段 doc 与 `wiring.go` 原文对照） | 优先级与 C5 同级：**不定这一条，`wiring.go:328` 的报项就无法处置**（搬任何一种口径都是替您裁决）；牵连 `local_file_kv` 的耐久契约陈述 |
| C8 | 17.5 建议的**"直连真实 API 的测收进显式构建标签"是否纳入本变更范围** | §371.7 实测：`TestPlanAgentCreateBehavior_RealPrompt`（`tests/plan_agent_test.go:219`）与 `TestRealLLM_PlanReentry_ClarificationLoop` 在 `-short` 下均 **SKIP**（源码已有 `testing.Short()` 闸），`go test -short ./tests/` **ok** | 默认套件的确定性**已由 `testing.Short()` 保障**，所以这不是缺陷、是"要不要再加一层构建标签"的取舍；我 §371 一度据 `head -3` 截断的输出误判前者"已不存在"，已更正 |
| C9 | **9.3 的"零豁免"是否接受一处负载敏感失败**：36 包并发 `-race` 下 `TestLiveSessionStaysWatchedAcrossToolGeneration` 红，**单独 `-race` 复跑 `ok 16.840s`**；失败点 `cross_generation_test.go:1579`（期望 `task.TaskRunning`、实得 `"suspect"`），其等待用的是 **30 秒绝对 deadline**（`:1575`） | §372.3；`/tmp/race372c.log`（`DATA RACE` 计数 **0**，非竞态） | 三选一：**(a)** 按 `-race`/负载放宽那条 30s deadline（属改测试、代码面）／**(b)** 该包在 CI 里单跑以隔离并发／**(c)** 9.3 以"带一处负载敏感失败"收口并在 `evidence.md` 记为已知例外。我倾向 **(b)＋(c)**，未擅自放宽 §6.6 那条"零竞豁免"护栏；⚠️ 牵连 **B6**——`suspect` 与 `quiet_timeout` 下限（非 TUI 60s／TUI 90s）同属一个存活状态机，30s 与 60/90s 并存本身是否自洽需一并定 |

### D. 收口口径

| # | 需裁 |
|---|---|
| D1 | **本 change 的完成定义**：是"基线 0"，还是"注释面只剩已裁定的豁免项"？后者的话，A1/A2 修门 ＋ B 族择路 ＋ C1–C4 一次代码清理，就能收口 |
| D2 | 裸坐标 A5 若不纳入，是否在 wiki 里明写"规划坐标只作人工审阅项"，避免下一个人误以为机器已覆盖 |

## 3. 顺手发现的产品级缺陷（不在注释轴，但很重要）

**`TestResidentDurableE2E_FiveSurfaceReconciliation` 是间歇性失败测，且与本次改动无关。**

| 观测 | 数据 |
|---|---|
| 失败点 | `tests/resident_e2e_test.go:370` `reliability.NewInbox(filepath.Join(spillDir,"tagent"),0)` |
| 错误 | `reliability: requeue claimed 00000000000000000002.json: create inbox tmp: open /var/folders/…` |
| 归因 | **HEAD 纯净副本（`git archive` 导出）3 次跑 2 败**；本工作树 3 次跑 1 败；6 次失败全为同一测 ⇒ 既有 flake |
| 机制嫌疑 | 用例拆除与在途 requeue 竞争：inbox 目录已被回收，requeue 仍在建 tmp ⇒ 指向**耐久收件树的关停/重排时序**，属核心可靠性面 |

建议单独立项（耐久收件树关停顺序与 `requeue` 的目录生命周期），不要混进注释面收尾。

## 4. 我在这些批次里犯过的错（自报，供审阅）

| # | 失误 | 后果与教训 | 固化规程 |
|---|---|---|---|
| E1 | 哨兵名单用台账散文导出（先空名单＝假安心，放宽后＝7 个假红灯） | 哨兵改**纯批前/批后回执比对** | 25 |
| E2 | 记录提交的动作改变提交（1494→1505→1507 自指读数） | 台账改记"排除台账"的稳定口径 | 26 |
| E3 | 多段脚本中途抛 `ValueError`，我把混合输出只看尾部当成功 | 逐段打 rc、读数同口径 | 27 |
| E4 | test doc 用物理折行续写 | 形状判据从门源码实测导出 | 28 |
| E5 | 改了 git 未跟踪的文件（§361 的 `examples/wechat-bot/main_test.go`） | 批前先核跟踪状态；并按您裁定把忽略文件移出统计 | 29 |
| E6 | 新写的工具代码被自己的门判违规 | 新代码先按四张 pattern＋形状自查 | 30 |
| E7 | 预检不复刻门的标识符遮罩 ⇒ 会把合规词改坏 | 预检复刻 `ReplaceAll(prose, declName)` | 31 |
| E8 | 🔴🔴 **多文件批在循环里边写边验** ⇒ 一次**部分写入**（只落 1 个文件，还在半应用树上跑了一轮验证） | **两阶段：全部预演通过才统一写盘** | 32 |
| E9 | 🔴 **探针三次说谎**（语言不匹配／节窗口退化为一行／窗口越过节界）——第三次差点让我把索引改指到**不承载**该命题的锚点 | 承载核验以**逐行读原文**为终裁 | 33 |
| E10 | 凭记忆复述门的词表（与实码不符） | 词表每次从源码 `grab()`，推理也不引记忆 | 34 |
| E11 | 一次 `go test` 结果即作归因依据 | 失败必重跑并与 HEAD 对照才可归因；一次通过也不代表该包稳定 | 35 |
| E12 | 🔴 **`go test` 把 `-short` 放在包列表中间 ⇒ 排在旗标之后的包被静默跳过且 rc=0** ⇒ §366 那 9 包实际只跑 8 包，漏掉的正是我改过的 `tool/action`（已于 §367 补验 rc=0） | 旗标必须前置，且**核对 ok 行数 == 请求包数** | 36 |

## 5. 剩余 53 条的分解

| 类别 | 条数 | 能否我自己清 |
|---|---|---|
| A2 `_` 缺口导致的不可满足项 | 3 | ❌ 需改门 |
| B 无落点（含 B1 的 21 行 doc） | 约 6 | ❌ 需选路 (a)/(b)/(c) |
| C1–C4 死代码与生产侧体内注释 | 约 12 | ❌ 需回到代码面 |
| C5 文档对立相关 | 2 | ❌ 需先裁真源 |
| C6 测试体内说明（可搬进所属 doc） | 8 | ✅ 但需逐条判"搬入是否重复" |
| A5 裸坐标（若纳入则**新增**约 146） | 0→146 | ❌ 需您先定 |
| 其余零散 audit/前缀 | 约 14 | ✅ 混合，部分需改写措辞 |

## 6. 建议的下一阶段顺序

1. **先裁 C5**（`memory-architecture.md §13.1` 与实现对立）——文档真源存疑时，索引面的任何"正确性"都是沙上堡。
2. **裁 A1/A2/A6/A7/A8**（五个纯缺陷，改门即减噪，无争议）。
3. **裁 A4＋A3＋A5 一组**（词表的宽严与是否纳入裸坐标）——这决定"收敛"的口径。
4. **裁 B 族走 (a)/(b)/(c)**；若选 (a)，我按 B1→B5 逐篇补 wiki 承载，然后一次性瘦身对应 doc。
5. **C1–C4 作为一次代码清理**（死测试删除或复活、占位保留去掉、生产侧注释入 doc），与注释轴分开提交。
6. 最后才是我这边剩下的 C6/零散项自助批清。

## 7. §377 普查清单：9.7「双重真源」全量对读结果（待裁，我未改任何一处）

**方法**：两条对偶轴各配一台枚举仪器，都带已知正样例自检（第一版 A 轴漏剥 `//` 报出"0 候选"的**假零**，靠 `config.go:424` 这个必中样例当场揭穿 ⇒ 规程 40 的"校验器自验"推广到**一切枚举型仪器**）。A 轴＝带 `契约:` 索引且仍有 ≥5 行正文的 doc（456 个带索引 doc 组中 28 个），B 轴＝`docs/wiki` 内 fenced 代码块中复述枚举/默认值者（30 个 go/yaml 块中 2 个）。**28 个候选已 100% 逐处与索引目标节对读**（不是抽样）。

**关键分布结论**：≥5 行候选里 **13 个是测试文件的意图 doc**（`// - ` 要点是规程 28 规定的形状，描述"这个测钉什么"，不属复述）⇒ 真正需要裁的只有 **15 处生产码 doc**。其中：

### Q-1 确证复述（同一命题在 doc 与 wiki 两处并存，共 10 处）

| # | doc 位置 | 复述的另一处 | 对应紧密度 |
|---|---|---|---|
| 1 | `resources.go:320`（17 行） | `resource-ownership.md` §七 `#close-order` ＋ §八 `#poisoned-seal` | **逐小句中译英**：三序拆除（先停遗忘生产者→再等引擎工作协程→最后 flush 关后端）／错误不得被吞成"安全关闭"／无解封出口的 per-path fail-closed／引擎侧两条触发腿是真实的 |
| 2 | `agent/context_manager.go:701`（11 行） | `execution-generations.md#published-wrapper-immutable` §六 | 两条（执行面只来自被执行器、不回落到其他快照／状态面始终取自当前 cm）＋"构造期不得往已发布包装器写调用域数据，投影经流程上下文" |
| 3 | `rl/endpoint_redirect.go:15`（7 行） | `rl-architecture.md#redirect-policy` §一 | 4/4 命题逐条，含"空 allowlist＝动态重定向关闭的部署语义"字面级 |
| 4 | `tool/action/tmux_monitor.go:681`（6 行） | `tmux-action.md#fake-alive-restart` §四 | 五小句全对应（原 ID 重启保链条／成功重置稳定度元数据／下一轮自然回 Running／失败不动状态／可能自然完成或滑向假死） |
| 5 | `memory/types.go:26`（5 行） | `memory-architecture.md` §16.5 契约 6 ＋ §二十二 `#event-shape` | 时间两轴（Timestamp 唯一语义轴／EventKey 内编码仅供段落定位与同毫秒决胜／无决策同时依赖两者故分叉无害）——**同一命题现存三处** |
| 6 | `event/wf_facts.go:5`（5 行） | `event-architecture.md#internal-retention` §12.4 | 事实链是 workflow 状态唯一真源／引擎已撤回后唯一作用是让历史 `wf.*` 继续被排除／因此 TTLDays 必须为 0 |
| 7 | `agent/compress/context_compressor.go:155`（5 行） | `compression-and-telemetry.md#hot-bundle-atomicity` §六 | "一次读取整体取出⇒外层触发线与内层目标必同代"逐句英译（doc 独有的"零值／非法值回落构造值"属 API 契约，应留） |
| 8 | `agent/projection_rebuild.go:181`（6 行） | `compression-and-telemetry.md#projection-fold` §二第 4 条 | "先过滤非投影记录、后保留最新 N 条"顺序两处并存；doc 已写"理由见 wiki"，但把约束本身也抄了一份 |
| 9 | `org_hotreload.go:153`（5 行） | `org-hot-reload.md#diagnostics` §八表"逐 agent 回执"行 | 尾句"只保留最近一轮、按拓扑限界、不累积历史"逐句对应（前 3 句 applied／draining 两态语义是类型契约，该留） |
| 10 | `org_hotreload.go:41`（6 行） | `org-hot-reload.md#generations` §三要点 | "发布身份是序号不是内容指纹、回滚同一内容仍前进序号"复述 1 句（字段持有清单属层一，该留） |

### Q-2 索引错指（锚点解析成功、所指节却不承载该命题——**门不可见**，1 处确证＋1 处判定撤回）

| # | doc 位置 | 现索引 | 实情 |
|---|---|---|---|
| 11 | `tagent.go:243`（12 行装配清单） | 共 **10 条索引**（`org-hot-reload` 六节、`tool-architecture` 两节、`platform-subsystems` 两节） | ⚠️**原判"错指"已撤回**：上批只核了第 1 条 `#overview` 就据以判"零重叠⇒错指"，其余 9 条未读，**不合格**。现改记为**索引堆挂**（一条 doc 挂 10 处＝未对"单点承载"作出选择），承载关系待逐条核实 |
| 12 | `agent/helpers.go:151`（8 行 ctx 传递纪律） | `reincarnation-notice.md#delivery`（单条） | §五讲"通报必须走专用注入源"，与 invocation id 的 ctx-threading 无关；相关的是 `#subagent-loop` 第 4 条 —— **此条判定成立**（该 doc 只有一条索引，已逐个读完） |

ⓘ 这一类是 **§370.2 我人工做的事暴露出的系统性门盲区**：门只校验锚点能否解析（`index-anchor-unknown`），**不校验语义承载** ⇒ 任何"就近挂靠"都长期绿。是否给门加一条可机检的承载判据，请一并裁（我的判断：无法可靠机器化，宜靠审阅＋本清单这类普查）。

### Q-3 判为合法（读过之后确认非复述，3 处，供反向参考防误伤）

| # | doc 位置 | 为什么合法 |
|---|---|---|
| 13 | `agent/context_manager.go:1816`（8 行） | 现索引 `#board-rendering` 谈"面板只呈现事实与剩余寿命、不仲裁、有上限"；doc 谈"为何放尾部（字节每回合变⇒保前缀缓存）／nil-check 必须在调用时／面板不入投影不入压缩"——**互补而非重复** |
| 14 | `agent/event_bus.go:23`（8 行） | §一 `#event-stream-overview` 是"谁 publish 到总线"的流程图；doc 的"总线上只有一种触发态＝`TypeExternalInput`；`agent_output` 不入总线、直发 outputCh；回合内仍是上游同步 ReAct"是更锐的断言，仅部分相邻 |
| 15 | `agent/output_limit_tool.go:69`（7 行） | doc 讲"为什么必须能剥壳（每个工具都被 `OutputLimitTool` 包住⇒已发布面持有的是包装后的 wrapper）"；与 §七"剥壳不改变名字解析目标"只一句相邻 |

### Q-4 wiki 侧复述码面（B 轴，2 处；§9.5 已被仪器独立复现，另 1 处是新发现）

| # | wiki 位置 | 复述对象 | 风险 |
|---|---|---|---|
| 16 | `event-architecture.md:63`（**64 行 fenced go 块**） | `event/types.go` 的 14 个 `Type*` 常量，**连注释一并复制** | 与 §9.2 同族且**大一个量级**：新增事件类型必然漏项（`timed_out` 已实证这条因果链） |
| 17 | `tool-architecture.md:776`＝§9.5 | `DefaultMonitorConfig` 六项默认值 | 今日实测**与码面一致（非漂移）**，但与 `tmux-action.md:16` 自立的"本页不另存一份常数"相悖；数值单点应归 `DefaultMonitorConfig`／`TmuxMonitor.stableWindow` |

### Q-5 顺带查实的 A5 类残留（门不抓，需与 A5 一并裁）

`resources.go:321` 与 `:327` 两处 **`D5`** 裸坐标（"in the D5 close order"／"(D5: never two writers)"）就在一条**其余全合规**的 doc 里 ⇒ A5 的"是否纳入机器化"直接决定这类英文坐标是否还得人工清；普查中我只做了定点核对，**未做全仓 A5 重扫**（那是您裁 A5 的前置，若要我先出数，说一声）。

### Q-6 普查**未覆盖**的一轴（须您先定性，否则清单是不完整的）

A 轴的入选条件是 doc **已带索引**。但本族的原型案例——`config.go` 的 `FSync`（373.8）——当时**没有任何索引**：复述者不知道自己有一份 wiki 对应，恰恰是最需要被发现的那类。⇒ **上表 Q-1 只覆盖"明知故犯"的一半**。该池实测规模：生产码 doc ≥5 行且无索引 = **419 处**（5–6 行 215／7–9 行 117／≥10 行 87；最长 `config.go:16` `Config` 41 行、`agent/event_loop.go:117` `processTurn` 23 行、`agent/tool_agent.go:956` `ToolAgentFactory` 20 行）。逐处对读需 419 次，**不可自助**。⇒ 需要您的定性是：**"无索引的长 doc"是否也按 9.7 处理**；若只处理其中"疑似复述"，请给筛选口径（例如"含机制叙述词／数值默认值／枚举清单形态者"），我可以据此把 419 缩到一个可裁的子集，而不必全读。

ⓘ **Q-4 #17（§9.5）已在 §378 处置完毕**：4 项字段语义先搬进 `MonitorConfig` 的字段 doc（那里本来就是它们的家），再删 fenced 块收成指针；唯一只在 §9.5 承载的命题"**dense→sparse 边界即同步等待转异步 ack 的点**"以散文保留在指针段里，没跟着块一起消失。

ⓘ **Q-4 #16（event §4.1）处置停在手之前，未改一行**：照搬 §9.2 的"收指针"会**毁掉命题**——那 64 行里除常量名与取值外，还带约 40 行**机制与理由**，且**码面没有、wiki 别处也没有**：`TypeToolChain` 为何必须与 `context_compress` 区分（否则被 `buildRetainedRefs` 吸收进滚动摘要计数）、`TypeSettleFold` 的票据行形状与"底层 settle 原文仍留事实链"、`TypeGovernance` 为何用单类型＋subtype 而非五个类型、`TypeConsolidation` 的服务端 SHA1 防 LLM 伪造、`TypeFeedback` 经 RelationStore 因果边零新索引、`TypeTaskSpawned`/`TypeResidentSession` 的"不进投影＋TTL 对齐"。三条出路：

| 出路 | 形态 | 代价 | 可逆性 |
|---|---|---|---|
| (a) 块改**表格** | 保留全部命题，只去掉"复制码面声明形态"这一漂移源（新增常量时表格不再伪装成"完整列表"） | 一次文档面改写，零测试成本 | 与 (b) **不冲突**，可后续叠加 |
| (b) 码面补 15 行薄契约 | 每常量一行（是什么＋正/负 key＋是否落库），机制理由仍留 wiki，然后 §4.1 收指针 | 15 行新码面 doc ＋ gen_godoc 重生成 | 单向 |
| (c) 维持现状 | 只登记 | 无 | — |

⇒ 我建议 **(a)**（纯文档面、零命题风险、直接消除漂移源，且不与 (b) 冲突）。它已写入 §378 的默认路径；若您要 (b) 或 (c)，说一声即可改道。

**成本与出路（§378 之后的状态）**：Q-1 的 10 处**已按口径 (a) 清完**（每处删复述句、保留该由 go doc 说的部分，门与 lint 全绿）；Q-4 #17 **已清**；Q-4 #16 **停在 (a)/(b)/(c) 的形态分叉**（见上）；Q-2 剩 **1 处待换锚**（`agent/helpers.go:151`，需您确认语义归属），`tagent.go:243` 改记为索引堆挂、待逐条核实；Q-3 不动（已确证合法，本批未碰）。**关键：这批 11 处收敛不移动棘轮**（报项仍 11，收益在真源唯一性），不拿它冒充进度。

> 规程 25–36 与"承载先于删除／不为清零曲解规则／宁缺不硬凑锚点／只删引用不删命题"是下一阶段的默认工序约束；`docs/superpowers/plans/` 下的独立计划文件保持不动。
