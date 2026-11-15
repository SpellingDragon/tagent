# ORCHESTRATION · runtime-recovery-ideal

> 派发唯一依据。金字塔四层不得倒装：①结论 ②编排四问 ③明细 ④台账。每次派发前重读对齐，单完/波后回填台账。纪律载体是本文档非会话记忆（R27）。

## ① 结论

- **抵达 DoD 的唯一路径**：W0 症状锚 S1/S2 红＋共享面契约冻结 → W1 01+02+04 合单原子（task 托管＋引擎自持索引＋验收真量，boot 路径簇）→ W2 03（governance 托管）+05（契约同步/死码）并行 → W3 06 退位（乙，搬迁非删除）→ W4 F1–F4。
- **当前最大风险**：02 引擎索引自持切面是新设计（D6.8 循环依赖闸门），且 06 退位 identity-read＋排他安静点是最大新建面；二者须先冻结签名再派发。

## ② 编排四问

- **次序**：派发原子＝就绪孙任务集合（R28）。W1 三域触碰集相交（run.go boot 簇/journal.go open/segment_store.go Load）→ 合单串行，不可并行。W2 03/05 不相交→并行。W3 06 依赖 W1+W2 的 存活 权威。
- **授权**：派发前置＝资源划拨审计＋本单边界声明（九要素）；写入白名单精确到文件、只含本单所需；账目单点归属（R17）。共享面（Stateful 接缝/存活 权威/退位 identity-read/验收四量/引擎索引切片）编排者单写，冻结签名后各域消费（R22）。
- **凭据**：完成度五查（勾选真实性/产物盘点/数字溯源/遗漏检测/生产消费者实测）按派发单核＋写后校验三连；波次收尾派独立验证子代理评估偏差（R30，对照原任务行验收面非追认缩水）；返工止于波内。
- **释放**：释放即消费（R31）——W1 绿当场派 W2，W2 绿当场进 W3，波次收尾是记账不是停点。连续推进至五法定停点（资源门缺凭据/提交归档需明示/重大取向裁定/blocked/DoD）。

### 等待表（释放必携）

| 波 | 释放条件 | 等待中的下游 | 凭据 |
|---|---|---|---|
| W0 | S1/S2 红＋契约冻结 | W1 | 症状锚复跑红＋§五签名冻结 |
| W1 | 01+02+04 合单绿 | W2 03/05 | verify 三要素 exit 0＋S2 转绿 |
| W2 | 03 绿＋05 绿 | W3 06 | spec Scenario 绿＋死码 grep 零＋主spec同向 |
| W3 | 06 绿 | W4 F | S1 转绿＋identity-read 负锚绿＋长跑消费者锚 |
| W4 | F1–F3 绿 | F4（停点） | 全量门 exit 0＋S1/S2 红转绿两次证据 |

## ③ 明细

### 派发单-孙任务矩阵

| 派发单 | 域 | 孙任务 | 触碰簇 | 波 |
|---|---|---|---|---|
| P1（合单）| 01+02+04 | 1.1-1.4 / 2.1-2.5 / 4.1-4.4 | run.go boot/journal.go open/segment_store.go Load/durable.go/checkpoint_test.go | W1 |
| P2 | 03 | 3.1-3.4 | governance/{denial,goal}.go/host.go:608/approval_store.go | W2 |
| P3 | 05 | 5.1-5.4（17 叶）| openspec/specs/*（10 活条款）/golden/fold.go:49/projection.go:342 | W2 |
| P4 | 06 | 6.1-6.5 | journal.go 新capability/lifecycle/retention/guard/config.go | W3 |

### 合单判据（R21/R28）

P1 三域同触 boot 路径簇，04 真量锚是 01/02 验证面 → 合单原子，主线程串行，每子步 go build 保持可编译，MUST NOT 派并发 agent 分摊（触碰集自相交）。P2/P3 不相交→并行。P4 后置依赖 存活 权威。

### 共享面冻结签名（编排者单写，R17/R22）

- 存活 权威：`live = checkpoint.Survivors ∪ {ref: ref.seq>Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`。
- 验收四量：journalBytes（开档读入字节）/ indexEntries（**len(s.index) 存活索引规模，非 EventCount**）/ durableLoadCount / wallClock。
- 引擎索引切片：引擎自持持久索引，**不从 checkpoint 派生**（D6.8 循环依赖闸门）。
- 退位 identity-read：GetEvent(key) 对已搬迁事实 SHALL 仍返回原字节（透明回冷层），SHALL NOT missing。
- boot 入表时序：任务账户入表先于全部切片恢复（D6.9，mailbox 读 durable 欠账）。

### 预算/规模

规模档＝可跑原型＋全量门（用户裁定）；物理有界取向＝乙（退位教义兼容扩展，用户裁定）。不考虑开发成本，全修到干净终点。远端部署实跑核验另议（超出可跑原型档）。

## ④ 台账（逐轮凭据＋计划回写）

### R0 立案（规划）

- 用户终裁：全修到干净终点＋清理废弃；物理有界乙；可跑原型＋全量门。症状锚 S1/S2 确认。
- 立档：一级 proposal/design/tasks/orchestration spec + 六域二级四件套（01-06）落盘。
- 实施期发现回写 design：D6.8（引擎索引自持，02 循环依赖闸门）/ D6.9（boot 入表时序）/ D6.10（能力名归正 session-checkpoint-material，task-skeleton:51 未反向只:74）；§五 indexEntries 改 len(s.index)（02/04 冲突裁定）。
- 编排者纠偏：orchestration spec 改 delta 格式（## ADDED Requirements）修 strict 红。
- 待核（W0 前）：05 报 16 继承 spec 中 8 已 superseded、活条款 10（含 memory-curation:24/:33/:38）；六 Material 端口仅 compatMaterialOf 生产零消费（审计后裁定）；4 测试锚被 config-surface 未提交变更占用（后置）。

### 停点（R31）

- **实施已授权**：用户明示「代码执行派发 openspec-apply 子代理完成」——「实施待审 design」停点解除，进 Phase 6 执行编排。F4 归档＋commit/push 仍须用户明示（未授权）；config-surface-normalization F4 授权不被本变更 extrapolate。
- 后续逐轮回填：W0 症状锚红凭据 → W1 合单 → W2 → W3 → W4 F1–F4＋S1/S2 红转绿两次证据。

### R1 执行启动（W0 派发）

- 用户授权实施（openspec-apply 子代理）。重读 ORCHESTRATION 二节四问对齐：次序 W0→W1 合单→W2 并行→W3→W4；共享面 §五已冻结（存活权威/四量 len(s.index)/引擎自持索引 D6.8/退位 identity-read/boot 入表 D6.9）。
- **W0 派发单 P0**：写症状直译锚 S1（TestJournalBytesBoundedByLiveSet，journal 真字节有界）+ S2（TestSymptomAnchorBootCostFlatAcrossGenerations，boot 四真量平坦；实际锚名去 Session 避 checkpoint_test.go:1231 同名冲突，见 R1-W0 核销）并复跑**红**（今天 O(全史)）。白名单＝新增独立测试文件（避开 config-surface 占用的 checkpoint_test.go 等 4 文件）；禁改生产代码、禁 git commit、禁触外来文件与 config-surface 未提交变更。凭据＝两锚 exit≠0 原始输出＋非空转证明（用例数>0）。
- W0 绿（红凭据成立）→ 当场派 W1 合单 P1（01+02+04，主线程串行原子）。

### R1-W0 核销（编排者亲验，凡报必验 R7）

- **S1 复跑红** ✅：`go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` exit 1；journal 真字节 [15714 31464 47214 62964 78714] 线性增（≈4.99×），分母自证（EventCount=45=5×9 历史真增长），红因落字节增长非空转。
- **S2 复跑红** ✅：`go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` exit 1；开档读字节 [0 132695 321759 549178 814866]、索引 [0 43 94 145 196]、durable 前缀键 [0 12 24 36 48] 三量线性，分母自证（20 任务真结清）。
- **白名单核** ✅：git status 新增恰 2 文件（assembly/symptom_anchor_recovery_test.go、tests/integration/history/symptom_anchor_journal_test.go），config-surface 占用面与外来文件未触；go build ./... exit 0。
- **锚名归正（R29 漂移回写）**：W0 为避 config-surface 占用的 checkpoint_test.go:1231 同名锚，S2 新锚名 `TestSymptomAnchorBootCostFlatAcrossGenerations`（去 Session）于 symptom_anchor_recovery_test.go。已回写 04 spec/tasks/design/proposal + 01·1.4 + 02·2.4 消费者锚指向新名；既有 Session 锚（正文计数代理量）保留不动作历史对照。04·4.3 由「改既有锚」改为「精化 W0 新锚断言面为四真量」。
- **W0 启动门达成**：S1/S2 红凭据成立＋§五契约已冻结 → 释放 W1。

### R2 充分并发派发（用户裁定：最大化并发，<3 才合单）

- **并发三轨同一回合拉起**（触碰集不相交）：
  - **P1 W1 合单**（01+02+04，boot 路径簇 run.go/checkpoint.go/durable.go/journal.go/segment_store.go）——同文件簇必须串行，三域无法拆 3 独立代理 → 合单（<3 例外，用户裁定）。
  - **P2 03 governance 托管**（features/evolution/governance/{denial,goal}.go + host.go:608 + approval_store.go + 新 governance 切片文件）——与 boot 路径簇不相交；Stateful 接缝已冻结（§五），03 注册新切片不依赖 W1 代码 → 提前并发（原 W2 门禁按用户并发裁定解除）。**禁触 run.go boot 路径簇**（若需改 run.go 上报编排者）。
  - **P3 05·5.4.1 六 Material 端口审计**（只读，零写入）——与所有写轨不相交 → 并发。审计表落 /tmp，不改代码（5.4.2 裁定待审计后）。
- 白名单（P1）：boot 路径簇生产文件＋新测试；**禁改 config-surface 占用 4 测试文件**与 checkpoint_test.go（既有 Session 锚保留）；禁触外来文件/冻结归档；禁 git commit。
- 关键闸门：D6.8 引擎索引自持（不从 checkpoint 派生）；D6.9 boot 入表时序；§五 indexEntries=len(s.index)。
- 凭据：每叶 verify 三要素 exit 0＋S2 转绿＋go build 每步可编译；完成度五查＋波后独立验证（R30）。
- 等待表：P1 绿（S2 转绿）→ 释放 05 转正/死码（5.1-5.3，待代码稳定）；P2 绿＋P1 绿 → 释放 W3 06 退位（依赖存活权威）。

### R2 回收（三轨战报＋编排者亲验）

- **P3（05·5.4.1 审计）完成** ✅：六 Material 端口审计表落 /tmp/recovery-05-audit/（241 行 123 处 file:line）；compatMaterialOf 生产运行期 0 次（7 生产类型全实现 Material()）；六端口常量活（两案 MUST NOT 退役）；**5.4.2 建议乙**（保留兼容读法＋勘误）——硬约束：session-checkpoint-material/spec.md:38 活条款「六端口 SHALL 保留到迁移完成」＋Instance 2→3 方法触 POLYMORPHIC_METHOD_LIMIT=2 棘轮＋收益仅删 20 余行。编排者待裁 5.4.2（倾向乙）。
- **P1（W1 合单）部分完成 4/13**：01·1.1（切片三族收据）/02·2.3（静默跳页改响亮）/04·4.1（四量探针）/04·4.2（多代多任务夹具）✅ 复跑绿；S2 仍红（02 核心未落地）。**设计阻塞 A（重大取向，停点）**：indexEntries 逐代线性增（43→94→145→196），有界必裁剪存活键集，裁剪即让被裁键 GetEvent 答 missing（identity-read 破），须 06 退位透明回冷层承接 → **02 与 06 固有耦合，S2 无法在 W1 单独转绿**。三选一待用户裁：甲 02+06 合波 / 乙 W1 临时热层兜底＋W3 冷层 / 丙 index 裁剪延 W3。
- **P2（03 governance）撞 150-turn 上限**：部分完成，TestGovernanceSliceWiredInProduction 挂死 9m07s（影响整包 CI）→ 编排者诊断＋窄派收尾（记忆：耦合簇撞上限改主线程诊断再窄派）。
- **编排者亲验**：go build ./... exit 0；concept_budget FAILED（StatedConsumption/StatedRefusal 无 concept binding＋domain_surface.new=53>47＋exported_interfaces=84>83）→ 编排者处理（绑定 live spec＋具名抬棘轮，M4 先例）。
- **待裁清单**：① 02/06 耦合排序（甲/乙/丙，重大取向，用户裁）；② 5.4.2 甲/乙（编排者倾向乙，可独立裁）；③ concept ratchet 抬升（编排者具名处理）；④ 04·4.4 验收面重裁（单进程内 boot×2<全史对照恒假，改 F3 级联回滚证据）；⑤ 尾键集接缝 §五 冻结（编排者单写）。

### R3 用户裁定甲：02+06 合波（重大取向已裁）

- **用户裁定甲**：02（有界索引裁剪）与 06（冷归档 identity-read）合为一个原子波——索引裁剪与冷层承接同批落地，S2 一次转绿，无「GetEvent 临时答 missing」中间态，验收面不缩水。
- **波次重构**：W1＝01+04（task 托管＋探针，P1 已部分完成）；**W2＝02+06 合波（索引裁剪＋冷层 identity-read＋排他安静点＋脚手架接线）∥ 03 governance（disjoint）**；W3＝05 契约同步/死码（待代码稳定）；W4＝F1–F4。
- **编排者独立裁**：② 5.4.2＝乙（保留兼容读法＋勘误，审计三硬约束）；④ 04·4.4 改 F3 级联回滚证据（单进程对照恒假，不新增二进制内对照锚）；③ concept ratchet 具名抬升＋新类型绑 live spec（M4 先例）；⑤ 尾键集接缝由编排者冻结签名（§五）。
- **P2 governance 撞上限**：编排者先诊断挂死测试（TestGovernanceSliceWiredInProduction）＋已完成面，再窄派收尾（记忆：耦合簇撞上限改主线程诊断再窄派）。
- **合波设计闸门（编排者冻结再派）**：索引切片格式＋裁剪输入面（存活权威 §五）＋冷层结构（<root>/cold 第二 journal 根）＋GetEvent 三态透明回冷＋排他安静点（Run 停写→drain→swap 持 journalKV.mu）＋保序逆帧序去重。合波大，按串行子步窄派（防 150-turn）。

### R3 编排者独立裁＋再派发

- **5.4.2＝乙（编排者裁，审计三硬约束）**：保留六端口兼容读法＋只落 5.2.2 勘误。依据 P3 审计：session-checkpoint-material/spec.md:38 活条款「六端口 SHALL 保留到迁移完成」（甲＝跨域契约变更）＋Instance 2→3 方法触 POLYMORPHIC_METHOD_LIMIT=2 棘轮＋甲收益仅删 20 余行而改造面 15 契约示例＋17 测试 face。六端口常量两案均不退役（活分派键）。compatMaterialOf 生产 0 调用但保留为合法可选声明形态。
- **04·4.4 重裁（编排者）**：单进程内 boot 与 seedFullReplay 共用 OpenTier，三量恒等⇒「boot×2<对照」恒假，不可判定。改为 F3 级联回滚证据（回滚 01/02/06 后同一 S2 命令必红），不新增二进制内对照锚。
- **concept ratchet（编排者处理）**：新类型 StatedConsumption/StatedRefusal/Receipts/SettleAccount/StateSeat 须绑 live spec（session-checkpoint-material）concept/kind；domain_surface.new/exported_interfaces 棘轮按具名抬升（M4 先例，记理由）。各域代理负责绑定本域新类型，编排者核棘轮抬升。
- **再派发（并发）**：P2-fix（03 governance 挂死诊断＋收尾，窄单）∥ P4（02+06 合波，按 D6.12 串行子步）。03 挂死根因待 P2-fix goroutine dump 定位（seat.Bind 流扫或 Checkpoint 切面锁）。

### R3 回收（P2-fix＋P4 战报＋编排者亲验）

- **P2-fix（03 governance）完成 4/4** ✅：**「挂死」是误判**——assembly 包真实工作量 260–334s（TestCloseStopsTheResidentsTheRunMounted 30s 等），治理测试 0.0–0.16s 是最快的几个；单跑/10 连/-race 全绿，无死锁。3.1-3.4 三锚凭据齐；StateSeat concept 绑定落 architecture.json。生产接线 host.go:575/624/799 真挂（脱离测试态）。
- **P4（02+06 合波）部分**：冷归档引擎核心落地（6.1/6.3 部分，5 正锚绿＋-race×10，搬迁后热层键 33→4，IndexEntries=1，EventCount=15 不变，保序钉绿）；**S1/S2 仍红**（生产接线未开）。能力形状偏离：以单一 RetireFacts(ctx,live) 替代 EnumerateKeys/Tombstone/ReclaimTombstoned 三方法（排他安静点内一步原子，避「已标记未搬」中间态）——编排者认可（合 D6.12 甲无中间态）。
- **编排者亲验**：go build exit 0；cold archive 5 测试 ok；governance ok（0.843s）；S1 仍红（预期）。
- **P4 设计阻塞②A（重大取向，停点待用户裁）**：冷/热两 journal 缺全局提交序——搬迁后热索引不点名已搬迁事实，EachIndexNewestFirst/NewestOfType 7 处生产消费者（checkpoint.go:829/865/985、run.go:2315/2576、read.go:114/280/317）需全局新→旧序。甲＝{pid}:idx: 值增单调提交序号（写路径格式变更，热冷按序号合序）；乙＝扩存活权威含所有 newest-first 所需事实（等于不搬迁，省不住字节）。编排者倾向甲。
- **编排者独立裁**：②B S1 夹具改走退位路径（同一 os.Stat 字节判据＋同一分母，验收面不缩，只改驱动路径——W0 夹具仅 StoreEvent 无真折叠/结清，驱动不了存活权威）；②C 装配层对耐久档默认开启退位（ledger 层默认仍关闭，spec 场景「ledger 未声明只追加」保留，S2 经装配默认开启转绿）；④3 wiki「六切片」→七（增 governance）归 05 契约同步；③3 seated-book 误接无生产触发路径，登记残余风险＋补 fail-loud 小闸；③2 architecture.json 共用写面 P2-fix/P4 各写本域绑定无冲突。
- **待用户裁**：②A 冷/热全序（甲单调序号/乙扩存活权威）；④1 03·3.3 BindStore 生产调用方口径（实际经 ledger.Bind/goals.Bind，BindStore 退化为兼容面；改任务行口径 or 退役 BindStore，涉验收面须用户裁）。

### R4 用户裁定＋追问

- **④1 BindStore＝用户裁「没用的就删」**：BindStore 兼容面生产 0 调用→退役删除（归 05 死码清理）；03·3.3 验收口径改为「ledger.Bind/goals.Bind 生产调用方≥1」（实际生产路径，验收意图不变）。
- **②A 用户追问「为什么退位后留 ADE」**：用户要先理解存活权威（liveness）再裁，不催决。编排者讲解中。

### R5 用户反问确立＋计划确认（D6.13/D6.14）

- **用户反问确立正确性/性能分层**：「真的不行吗？」——冷层透明可读时搬幸存者/blob/欠账都不破正确性，只影响读快慢。故**正确性＝identity-read＋全局提交序（甲）**；**存活权威是性能层非正确性门**；**退位判据简化为水位制**（水位前已结全搬冷、尾留热），存活权威降级可选性能优化（先不做）。落 design D6.13。
- **②A 裁定甲**（单调提交序号，唯一可行解）；**④1 BindStore 删**（D6.14，没用的就删，归 05）。
- **计划确认**：W0✅ → W1（01+04 部分）→ W2（02+06 合波：全局 seq＋水位制退位＋identity-read＋排他安静点＋脚手架接线 ∥ 03✅）→ W3（05 契约/死码）→ W4（F1–F4，归档/commit/push 须用户明示）。
- **再派发**：P4-cont（02+06 合波收尾，按 D6.13 水位制＋甲全局 seq）∥ P5-prep（05 spec 草案＋BindStore 删，disjoint）。

### R6 P4-cont 战报（02+06 收尾，按 D6.13）

- **① 全局提交序（甲）落地**：`{pid}:idx:` 值 JSON 增 `commit`（`segmentIndexCarried`）；`StoreEvent` 分配单调序号（`s.commitSeq+1`），`Load` 由 `len(published)+retired` 与已发布条目的序号取高水位（缺窗 meta 的条目不让序号回退）；`eachIndexEntryNewestFirst` 按序号归并热冷两序，`NewestOfType`/`EachIndexNewestFirst` 同径；`settled:commit` 记已搬最大序号，boot 在热层头越过该界时不打开冷层（冷层仍是懒开）。**凭据**：`go test ./memory/ledger/ -run 'TestColdArchive|TestColdTierReadSurvivesReopen|TestEachIndexNewestFirstStreamsTheCommitOrder|TestJournalStatesItsCommitOrderNewestFirst' -count=1` exit 0；新增锚 `TestColdArchiveRanksTheSettledCommitsBesideTheResidentOnes`（热层留最旧 fold、冷层持更新 fold → `NewestOfType` 答冷层那条，走查序三键全序；重启后热层头越过界 → `s.cold == nil` 证不打开冷层）。
- **② 水位制退位（D6.13）**：`RetireFacts(ctx, keepKeys)` 语义归正为「保留水位后的尾键集，其余事件族搬冷」（原 `live` 更名 `keep`）；`GetEvent`/`eventByKey`/`EachEvent`/`EachIndexNewestFirst` 冷层读与遍历已通。**原文族按 D6.13 边界未搬**（见下）。
- **③ 排他安静点**：`Run.settleGate`（提交读锁／退位写锁）＋`relocateSettledFacts`（写锁→`flight.await` 归零→读尾键集→`RetireFacts`）。**实测死锁与修正**：闸初置 `commitFactNoCadence`（派发单原口径）→ 全量 assembly 10 分钟超时 dump（`relocateSettledFacts`→`commitFence.await` 永不归零），根因＝该入口被「落链事实的结算」在闸内重入，Go RWMutex 有写者待时新读者阻塞 → 闸移到最外层 `commitFactChecked`，闸内重入不再等闸。**回写派发单口径**（R29 漂移）：闸位＝fats.go:84 原文与实现不符，实际落 facts.go `commitFactChecked`。
- **④ 装配接线**：`relocatableStorage` 对 journal/localfile 耐久档默认开启退位（②C，ledger 零值仍关）；`keptHotKeys` 以「尾（seq>offset）＋每条链最新 checkpoint＋框架 checkpoint（走到即停）」为保留集。**⑥ 未做**：lifecycle/retention/guard 接线与义务租约（原 6.4）未动。
- **⑤ 验收**：**S1 转绿（红→绿两次）**——`go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` exit 0；夹具改走退位路径（`RetireFacts: true`＋每代 `RetireFacts(kept)`），同一 os.Stat 字节判据、同一分母（末代追加字节），实测 committed=[15822 31716 31126 31266 31266]、retained=[15822 15232 15372 15372 15372]、appended≈15894 恒定，bound(liveBytes×2=31788) 通过；红证据（临时使 `RetireFacts` 空转＝能力缺席）retained=[15822 31716 47610 63504 79398] 线性、失败信息点名 79398 vs 15894；身份读红线在同夹具内断言（第 0 代已搬事实按身份读回原字节）。
- **S2 部分转绿（indexEntries 转绿，journalBytes/durableLoadCount 仍红）**：`go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$'` 退位开 → `journalBytes=[0 75465 169010 281113 411748] indexEntries=[0 2 2 2 2] durableLoadCount=[0 12 24 36 48] wallClock` 平坦；退位关（回滚）→ `journalBytes=[0 134331 326123 557274 827666] indexEntries=[0 43 94 145 196]`。**红项归属（实测取证，非猜测）**：热点 journal 字节按族分解（三代根，`blob:` 145 键 94398 字节＋`ref:` 145 键 38710 字节＝79%；`16:` 事件六键 44026；`tagent/task/v1/*` 39 键 19320）⇒ ①原文族（`ref:`/`blob:`）不在搬迁面（06·6.3 未接），②task 耐久族随账户线性增长且属 01 托管范围（01·1.2 load 改读切片未落地，durableLoadCount 12/代即其证据）。**二者均不在本派发单白名单可达范围**（01 域禁触；原文族属 06 未接面），故 S2 不能在本单转绿——待编排者裁定：派 01 收尾＋06 原文族。
- **质量门**：`go build ./...` exit 0；`go vet ./assembly/ ./memory/ledger/` exit 0；`go test ./memory/ledger/ -count=1` ok 22.1s；`go test ./tests/integration/history/ -count=1` ok 16.6s；`go test ./assembly/ -count=1` 见 R6 核销（编排者复跑）。
- **白名单核**：改 `memory/ledger/{segment_store,journal_kv,config,journal}.go`、`assembly/{run,facts,checkpoint}.go`、`memory/ledger/cold_archive_test.go`、`tests/integration/history/symptom_anchor_journal_test.go`；未触 governance 面、config-surface 占用 4 测试文件、checkpoint_test.go；未 commit。
- **残留风险**：退位开启后 `EachIndexNewestFirst` 在 checkpoint 每次落盘时走全会史（含冷层），是「显式全史读消费者」成本而非 boot 成本，但 6.4 的 Retention/Guard 未接即无节流；`ErrColdArchiveNotQuiet` 与停写超时未实现（负锚待补）。

### R7 重制计划（用户裁「重制计划，一笔做完，工作次序你定」）

- **核心重框定**：三挂点动态拓展性为框架核心、作 golden 守护（D6.15）；压缩-投影-召回拉回三挂点轨道。退位/checkpoint 有界是轨道上一个结果。工作次序＝甲（先立守护判据 W1，再修特性收敛）。
- **域重编号＋新增**：旧 01→05 task-custody、02→06 bounded-open-index、03→07 governance-custody、04→09 acceptance、05→10 contract-sync、06→08 fact-retirement（目录已 mv，内容沿用）。新增 01 three-hooks-golden、02 plugin-dynamic-mount、03 compression-configurable、04 midband-ticket-recall（四件套已落盘）。
- **波次**：W0 启动门（S1/S2/S3 红＋golden 门红）→ W1 01 守护 → W2 02 动态挂载 → W3 03+04 合单 ∥ 05+06+07 boot 簇合单 → W4 08 退位合波 → W5 09 验收 ∥ 10 契约 → W6 F1–F4。
- **代理实证纠偏**：①01 守护现树红集 7 处/3 文件（run.go:615/618、tools.go:250/253/256、host.go:709/728），按形态判非行号；新门 check_plugin_mounting.py＋plugin_mounts.json＋architecture.json mounting 节。②02 plugin-mounting 无既存主 spec→ADDED（非 MODIFIED）；01 扫描须排除插件自挂载文件（避自证）；ageFold/finalRound 相邻具名构造本域不修。③03 summary_budget_bytes 声明量（常量留作策略缺省）；摘要经 rl.NewLeaseModel＋withHostModelParams。④04 delta 增 RENAMED＋ADDED；拟导出 replay.TicketOf（须 concept_budget 绑定）；task-skeleton-compression:51/:74 既有漂移交 10。
- **待实施期核**：05-10 旧域内部编号引用需对齐新号（cosmetic）；03/04 共触 age_segments.go 合单串行；01 守护与 02 自挂载的扫描排除须冻结签名。
- **停点**：规划重制完成，实施待 W0 启动门；F4 归档＋commit/push 须用户明示（R31）。


### R8 W0/W1 核销（编排者亲验）＋P0 覆盖缺口

- **W0 启动门达成** ✅：S1 TestJournalBytesBoundedByLiveSet exit 1（journal 字节线性增，原文族 ref:/blob: 占热层 79%，夹具已并入原文族）；S2 TestSymptomAnchorBootCostFlatAcrossGenerations exit 1（boot 字节线性增）；S3 TestSymptomAnchorMidbandProcessRecalledByTicket exit 1（请求喂摘要无票据，recall_get 本身可取全文）。go build exit 0。
- **W1 01 golden 完成** ✅：check_plugin_mounting.py（R5a 形态判＋R5b plugins 存活四条件）＋plugin_mounts.json＋architecture.json mounting 节；13 例自测全过；现树 exit 1（10 错＝7 R5a mounts＋3 R5b）；lint.sh:127/128 接线。go build exit 0。
- **P0 覆盖缺口（须裁）**：golden 门现树红集 7 处具名构造＝run.go:615/618（compress＋finalRound）、tools.go:250/253/256（ageFold＋observePlugin＋telemetry）、host.go:709/728（reflection＋settle）。**域 02 仅动态化 compress**，其余 6 处仍硬编码→P0 门不能全绿。需裁：全部动态化（完整 P0）vs compress 先行＋其余跟进。
- **既有红（非本变更引入）**：check_concept_budget exported_interfaces=84>83（StateSeat 等，03/07 域绑定后核）。


### R9 用户裁 P0 范围：全插件＋builtin 默认注册＋可自定义

- **用户裁定**：「全插件，但是需要有默认启动流程注册 builtin 插件，如果框架的使用者认为不需要，允许自定义拓展」。
- **落 D6.16**：7 处具名构造全部插件化（非仅 compress）；plugin_catalog.go 列 builtin＋boot 经注册表默认注册（装配根不具名构造）；plugins: 声明可禁用/覆盖/扩展 builtin。域 02 范围扩至全部 7 特性＋builtin 默认注册机制。
- **W2 派发**：域 02 实施（全插件化＋builtin catalog＋plugins 接通），目标 golden 门 R5a 7 处全绿＋R5b plugins 存活绿。


### R10 W2 核销（编排者亲验）：P0 golden 门全绿

- **W2 02 完成** ✅：check_plugin_mounting.py exit 0（mounts=0/consumers=1/errors=0，R5a 7 处＋R5b 全绿）；selftest exit 0；check_assembly_symbols exit 0；go build exit 0；builtin 挂载＋plugins 定制测试 ok。
- **builtin 机制**：plugin_catalog.go＋compress_mount.go，boot 经注册表默认注册 7 builtin（compress/finalRound/ageFold/observePlugin/telemetry/reflection/settle）；plugins: 禁用式定制（缺省全挂，-name 卸载，贡献实例可扩）。装配根不具名构造。
- **他域在途红项（非 02）**：TestCompressProductRestoresAfterRestart（域 06 退位接线在途）；S2 症状锚（设计红，待 05/06）。
- **W3 派发**：03+04 合单（压缩可配置＋中段票据，共触 age_segments）∥ 05+06+07 合单（task/index/governance 托管，boot 簇）。


### R11 W3 部分核销（两代理撞 150-turn，主线程诊断）

- **build green** ✅（代理保持可编译）。**S2 boot 有界转绿** ✅（05/06/07 recovery 核心完成）；**S3 中段票据转绿** ✅（04 核心完成）；S1 仍红（08 退位 W4）。
- **05+06+07 全绿** ✅：runtime/state/runtime/task/memory/ledger/governance 包测试全 ok。recovery 托管完成。
- **03+04 剩余**：memory/replay 2 红＝TestFoldPlanGolden（golden 漂移，提示 -update-fold-golden 仅限意图规则变更）＋TestAgeFoldFoldsTheMiddleBandIntoOneProduct（中段票据格式变更致旧断言漂移）。tool/recall/api/config 绿。需核意图后更新 golden/测试＋域 03 压缩可配置核验＋级联。
- **窄派**：03+04 收尾单（核意图→更新 golden/测试→域 03 核验→级联）。


### R12 W3 核销完成（窄派两单收口）

- **concept_budget 全绿** ✅：6 新类型绑定（StatedConsumption/StatedAttempt/StatedRefusal/Receipts→declaration/task-registry-rebuild；SettleAccount→read；RelocateReport→declaration/event-segment-store）；SettleAccount canonical 范例＋红线；棘轮按名抬升 AgentSpec 24→25/StatedTask 13→16/StorageConfig 6/new_max 47→54/exported_interfaces 83→84（理由注记）。check_concept_budget exit 0，自测 56 OK。
- **票据漂移收口** ✅：history 3 漂移（TestEventIntegrityProjection/RestoreUse/ProjectionReplayAndBudget）转绿（presentedTicket/withoutPresentedTicket 钉票据＋剥票比对原文）；host 呈现断言已在树内改妥（0 需改）；S3 保持绿。
- **W3 完成**：P0 golden 绿＋S2/S3 绿＋recovery 绿＋memory/replay 绿＋concept_budget 绿＋漂移收口。
- **剩余红归 W4/他域**：S1（W4 退位）；TestCompressProductRestoresAfterRestart＋TestSoakContinuity（退位/恢复在途，W4）；TestGateFailsClosedWithoutAttention（审批 boot 读目录，07 收尾核）。
- **W4 派发**：08 fact-retirement（全局序＋冷层 identity-read＋排他安静点＋脚手架），目标 S1 绿＋退位相关红转绿。


### R13 W4 核心完成：S1/S2/S3 全绿（里程碑）

- **S1 journal 有界转绿** ✅（exit 0）；S2 boot 有界绿；S3 中段票据绿。**三症状锚全绿**。P0 golden 门绿。build 绿。memory/ledger 绿（19s）。
- 退位核心完成（全局序＋冷层 identity-read＋排他安静点＋脚手架，W4 代理撞 limit 前 S1 已绿，回归扫尾未尽）。
- **剩余**：W4 回归扫尾（TestSoakContinuity 崩溃恢复 ErrSpecMissing、TestGateFailsClosedWithoutAttention 审批 boot 读目录、全量回归）；W5 09 验收 DoD＋10 契约同步（task-skeleton-compression:74 MUST NOT LLM 反向、event-key-visibility 转正、死码）；W6 F1–F4（归档/commit/push 待用户明示）。
- **窄派**：W4 回归扫尾 ∥ W5-10 契约同步（disjoint）。


### R14 W4/W5 收口＋doc_budget 具名上调

- **W4 回归扫尾完成** ✅：修两真缺陷（退位窗口键枚举 retireSettled(settle) 引擎自推 keep；RestoreBoot 不可读目录书空＋DeliveryGap 记名 boot 不死）。TestSoakContinuity/TestGateFailsClosedWithoutAttention/TestCompressProductRestoresAfterRestart 全绿。全量 assembly(295s)/host(213s)/memory/runtime 绿。S1/S2/S3 全绿。
- **W5-10 契约同步** ✅：6 主 spec 同步（task-skeleton-compression/event-key-visibility/session-checkpoint-material/compression-pipeline/compaction-scheduler 退役/deterministic-compress-level）；openspec validate 151/0；spec_hygiene/doc_links/concept_budget 绿；BindStore 零残留、无死生产者。
- **doc_budget 具名上调** ✅：21998→22153（+155，6 权威 spec 同步实现的合法增长，循 M4 先例，理由注记）。check_doc_budget exit 0。
- **剩余收尾**：08 冻结锚补齐（EnumerateKeys/Tombstone/ReclaimTombstoned 与 RetireFacts 单能力 reconcile、独占闸锚、recover-kv 消费者锚、S1 回滚负锚＝F3 红转绿证据）；10 余项（RebuildProjectionFromWAL 家族删——4 文件 config-surface 占用待其后、compression-recovery-ticket superseded 需先解 Stateful 绑定）；W6 F1–F4（全量门＋DoD＋归档，归档/commit/push 待用户明示）。


### R15 F3 证据完成＋全门绿，抵 F4 停点

- **F3 红转绿两次证据** ✅（隔离副本 /tmp/recovery-f3/tree，主树 md5 零写入）：S1/S2/S3 当前树 exit 0、回滚 exit 1、复原 exit 0；红因 behavioral（S1 退位空转字节线性、S2 全史重放、S3 摘要无票据）。run-notes.md §F3/§08（62 行）。
- **08 锚对账** ✅：能力形状＝RetireFacts 单能力＋identity-read＋排他安静点（三方法枚举/分页负锚具名退出验收面）；新增 TestColdArchiveHoldsTheExclusiveGate（-race×10）＋TestRecoverKVReadsARelocatedRoot（真跑 recover-kv）。memory/ledger/history 绿。
- **全门绿** ✅：build/concept_budget/spec_hygiene/doc_links/doc_budget/plugin_mounting/window_bounds/schema_allowlist/openspec validate 全 exit 0；S1/S2/S3 绿。
- **一级 F1/F2/F3 勾** ✅；F2 远端模块待部署核验；F4 待用户明示。
- **余项（不阻 F4 核心，登记）**：08·6.4 脚手架 oracle 接线；10 RebuildProjectionFromWAL 家族删（4 文件 config-surface 占用，待其后）＋compression-recovery-ticket superseded（先解 Stateful 绑定）；远端 examples/wechat-bot 长跑核验。
- **停点（R31）**：F4 归档＋commit/push 须用户明示授权，未执行。config-surface-normalization F4 授权不被本变更 extrapolate。
