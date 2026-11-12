# code-vocabulary-alignment 编排文档

> 派发唯一依据。每次派发前重读二节四问；单完/波后回填四节台账。三者边界：`specs/orchestration/spec.md`＝可验收编排契约；`design.md`＝静态决策（D2/D4/D6）；本档＝驱动方式＋当场账。

## 一、结论

- **抵达 DoD 的唯一路径（甲）**：C 重写门为纯短语禁（现状红 390＝症状锚）＋删 2377 行基线＋缩自测 → 编排者捕获红锚 → A/B1/B2 并发退役全部 341 .go carrier（精确词映射 D6）→ D golden/wiki/GLOSSARY 漂移对齐 → E 编排者统一重生成 godoc（49 镜像清零）→ 门转绿（0）→ 真实仓根全量 lint exit 0 → 红转绿两次证据＋负例真拒＋净负 → R35 独立审者 frontier 空 → **停于 A3 发布授权（法定停点）**。
- **当前最大风险**：carrier 逐处精确词映射（D6）——341 处须按义类改**既有**规范词（commit/credential/URL/projection/envelope/fact/record），误改义类＝词汇失真；级联符号（commitCarrier/newCarrier/seedCarrier/carrier 字段/logicalRig.carrier）均包内私有已核**无跨轨级联**；compression 短语跨注释续行，门须归并散文匹配否则漏检（C 保留）。

## 二、编排四问（MECE）

### 2.1 次序（就绪轨表）

| 轨 | 内容 | 前置 | 触碰集 | 状态 |
|---|---|---|---|---|
| C | 门重写纯短语禁＋删基线＋缩自测＋lint 注释 | 立案即跑 | check_vocabulary_alignment.py／test_…py／vocabulary_alignment.json(删)／lint.sh:126-127 | 派发✓(background) |
| A | 生产 .go carrier 退役（97 命中/24 文件） | C 红锚捕获后 | D4 A 簇（全非测试） | 待派 |
| B1 | 测试 .go（state/assembly/agent，113 命中） | 同 A | durable_test.go／task_checkpoint_test.go／host_tools_test.go／handoff_test.go | 待派 |
| B2 | 测试 .go（task/integration，131 命中） | 同 A | runtime/task/*_test.go／tests/integration/** | 待派 |
| D | golden/wiki/GLOSSARY 漂移 | A/B 后 | architecture.json:1206/1211／durable_truth.json:204／wiki task-lifecycle.md:9／GLOSSARY | 待派 |
| E | godoc 重生成（49 镜像清零） | A+B 全部 .go 定稿 | docs/api/*（编排者单写） | 待派 |
| 验证 | build/vet/test/门绿/lint/红转绿/净负/R35 | E 后 | 编排者直管 | 待跑 |

- **并发准入**：A 全非测试、B1/B2 全测试，文件级不相交；级联符号包内私有（D4 实证）→ A/B1/B2 三轨并发。C 与 A/B 触碰集不相交，但 C 先完成以捕获干净红锚（390）。
- **关键路径**：C → 红锚 → A/B1/B2 → E → 门绿 → lint → R35 → A3 停点。
- **本轮做不了什么**：A/B 待 C 红锚捕获；E 待 A+B 全部 .go 注释定稿；提交/推送/归档待 A3 用户明示（法定停点）。

### 2.2 授权（白名单到文件＋共享面归属）

- **C 白名单**：`scripts/gates/check_vocabulary_alignment.py`、`scripts/gates/test_check_vocabulary_alignment.py`、`golden/runtime/vocabulary_alignment.json`(删)、`scripts/gates/lint.sh`(仅 126-127 注释)。禁动他文件、禁 git commit/checkout。
- **A 白名单**：D4 A 簇 24 非测试 .go。禁动测试/门/golden/docs。
- **B1 白名单**：`runtime/state/durable_test.go`、`assembly/task_checkpoint_test.go`、`assembly/host_tools_test.go`、`runtime/agent/handoff_test.go`。
- **B2 白名单**：`runtime/task/{lifecycle,reactivate,logical,restore,checkpoint}_test.go`、`tests/integration/**`(carrier 命中文件)。
- **D 白名单**：`golden/runtime/architecture.json`、`golden/runtime/durable_truth.json`、`docs/wiki/agent/task-lifecycle.md`、`docs/GLOSSARY.md`。
- **共享面归属**：`docs/api/*` godoc＝**编排者 E 单写**（gen_godoc.sh）；`lint.sh`＝C 单写；一级账目（tasks.md/ORCHESTRATION.md）＝编排者核销；计划文档（design/proposal/tasks/02-spec/子域/run-notes）＝编排者甲 重写。
- **偏离分级**：白名单内 carrier→精确词改名＝预授权（按 D6 映射）；映射未覆盖的歧义 carrier 义类＝上报编排者裁；任何 .go 逻辑改动＝上报（本变更只动词汇不动逻辑）；导出 API 改名＝禁止（级联已核全私有）。

### 2.3 凭据（五查落点＋复跑命令＋必亲验数字）

- **查1 勾选真实性**：每轨复跑验证命令 exit 0 才勾；非空转（go test 用例数>0、自测断言数>0、门红锚违规数>0）。
- **查2 产物盘点**：C 门~150/自测~120/基线删/lint 注释；A/B 改名处 git diff 对白名单；D 4 文件；E docs/api 重生成。git status 对白名单，外来文件零触碰。
- **查3 数字溯源（必亲验）**：①门红锚＝390（341 .go＋49 docs/api）清理前 exit 1；②清理后 exit 0/0 命中；③负例自测每类植入被拒＋每类合法不误拒；④go test -list 计数改名前后不变；⑤carrier 残留 grep .go＋docs/api 全仓 0（冻结归档 attribution-carrier 除外，不在门面）；⑥lint.sh 原始退出码 0（非管道吞码）；⑦净负：门 550→~150、自测 480→~120、基线 2377→0。
- **查4 遗漏检测**：A/B 须清全部 341 .go carrier（非只审计部分）；映射逐处按义类，漏改＝门红；级联符号（commitCarrier/newCarrier/seedCarrier/carrier 字段/logicalRig.carrier/reactivationFact 参）全改无残留。
- **查5 生产消费者实测**：门接 lint.sh policy 块＋ci.yml Lint step 实跑（grep 实证）；A/B 改名后受影响 go test 真跑绿（非仅 build）。
- **复跑命令**：`python3 scripts/gates/check_vocabulary_alignment.py`；`python3 scripts/gates/test_check_vocabulary_alignment.py`；`bash scripts/gates/lint.sh --only policy`；`go build ./... && go vet ./... && go test ./...`；`bash scripts/gates/gen_godoc.sh --check`；真实仓根 `bash scripts/gates/lint.sh`。

### 2.4 释放（锁/占用者/释放条件/等待表）

| 资源 | 占用者 | 释放条件 | 等待表（释放即触发） |
|---|---|---|---|
| 词汇门红锚 | C | C 完成＋编排者捕获 390 红 | 触发 A/B1/B2 派发 |
| .go 注释定稿 | A/B1/B2 | 全部 carrier 退役＋go test 绿 | 触发 E godoc 重生成 |
| docs/api godoc | 编排者 E | A+B 定稿 | 触发门绿复验＋lint |
| 全量 lint | 编排者 | E＋门绿 | 触发 R35 独立审 |
| 提交/推送/归档 | 用户 A3 | 用户明示 | 法定停点，不自动 |

- 无设备/配额长跑锁（纯静态门＋改名，分钟级）。释放＝触发：确认就绪当场补派 ready 单，不以「下一波再说」入档（R18）。

## 三、明细

### 派发单-轨矩阵

| 单 | 内容 | 合单判据 | 预算 |
|---|---|---|---|
| C | 门重写＋删基线＋缩自测＋lint 注释 | 同簇单 agent（门+自测+基线+接线） | 门 550→~150、自测 480→~120、基线 2377→0 |
| A | 生产 .go 退役 | 同簇单 agent（24 文件，级联符号私有） | 97 命中/24 文件 |
| B1 | state/assembly/agent 测试 | 同簇单 agent | 113 命中/4 文件（durable_test 109） |
| B2 | task/integration 测试 | 同簇单 agent | 131 命中/17 文件 |
| D | golden/wiki/GLOSSARY | 编排者直管（4 文件小改） | 3 golden 行＋1 wiki 行＋GLOSSARY 注 |
| E | godoc 重生成 | 编排者单写（避免并发互踩） | docs/api 6 镜像文件 |

- **合单判据**：同文件簇合单（R28 派发原子＝就绪轨集合，同簇交一 agent 避免互踩）；A/B1/B2 触碰集不相交故分单并发；C 先完成捕获红锚。
- **档位与真实路径欠账**：无训练/评测档位；真实路径＝门接 CI Lint step（非仅本地脚本）＋改名后 go test 真跑。欠账＝A/B 须清全部 341 carrier（漏改＝门红）。

## 四、台账

> **甲 勘误（取代声明）**：以下 W0–W3 台账系旧「指纹棘轮＋泛义保留」方案的历史执行记录。该方案的「泛义保留」前提系执行期 agent 决定被误标为 user_ruling（W8-VOCAB），用户裁定「我没有裁定过泛义保留」，**甲 修订取代旧方案**（carrier 全退役＋纯短语禁门＋删 2377 行基线，见 design.md 顶部勘误）。W0–W3 行**保留作审计轨迹不改写**；其中「基线豁免泛义 carrier」「user_ruling 明保」「Issue3 回退 task_checkpoint_test:174」等裁定已被甲 推翻——甲 下 task_checkpoint_test:174「durable carrier」→「durable commit」（B1 轨），基线整体删除，stripFoldGenerations→stripRecorderGenerations 保留。甲 执行轨见上节；甲 轮次台账见本节末「甲 轮次」。

| 轮 | 波 | 派发单 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|---|---|
| R0 | 规划 | — | 阶段5 自审待跑 | — | spec 最小化裁定：不新建 code-vocabulary，归 architecture-guardrails（见 design D2）；门宿主裁定：新建检查器成立（D6） | design/proposal/tasks/orchestration spec/ORCHESTRATION 落盘 | W0 派 D-02+D-03 | — |
| W0 | 02+03 | D-02,D-03 | 查1✓复跑：门 exit1/38 违规·自测 exit0/20 例 56 断言·编排者补桩后 test_check_runtime_plan exit0/43 测试·go build ./... exit0；查2✓产物对白名单（D-02 四件+lint.sh；D-03 三件，无越界）；查3✓coverage carrier=432/432·baseline 300 条(263 泛义+36 carrier 违规+1 generation 违规)·go test 249 PASS；查4✓基线全仓分类无漏；查5✓干净克隆 policy lint 0 FAIL 且词汇门被跑到红 38 | 干净克隆(HEAD a0ff631b，无外国文件，.git 为目录) `lint.sh --only policy`→selftest 0 FAIL·门 exit1 红 38·coverage 432/432→消费者锚 CI 等价成立；真树 go build exit0；真树 policy 唯一中止＝外国红 test_check_runtime_symptoms.py(ZERO-TOUCH，CI 无) | ①03.1 裁定 **KEEP**（distinct role 实证：runtime/state→runtime/task 单向依赖，MemoryRegistry 实现接缝即成导入环；契约不对等 RestoreCheckpoint 另登记去重键/观测序号；载荷 RegistryCheckpoint 非接缝信封；名词异层）②编排者补桩 test_check_runtime_plan.py 两处桩元组（共享面，D-02 白名单外，仿 check_window_bounds 先例）③外国 migrate-kv 工作(scripts/migrate-kv/main.go+docs/api 投影+examples/wechat-bot/main.go hint)判定为外来完整特性(ledger.MigrateShardedRoot/ErrJournalLegacyLayout 既有 API)，零触碰，build 绿，commit 时须分离 | 02.1-02.5✓ 03.1-03.3✓（top tasks.md 8 叶核销，02.4 阻塞经编排者补桩+干净克隆复核解除） | D-01 解锁：02 门验证红＋03 共享面 KEEP 零改动，working-tree 产品已验证；commit 待法定停点明示 | runtime/task/checkpoint.go 锁释放(03 KEEP 零改动)→触发 D-01 注释改名；词汇门红锚存在→触发 D-01 清存量转绿 |
| W1 | 01+F0-F3 | D-01（编排者 F0-F3 直管） | 查1✓复跑：门 exit0/violations0/stale0·自测 exit0/20 例 56 断言·go build/vet exit0·go test 6 包绿(ledger18.3s/replay cached/task0.6s/assembly256.3s/state0.7s/integration48.4s)；查2✓产物对白名单(01 十一.go+两勘误+3 投影；F0-F3 编排者面)；查3✓旧名残留 grep 全仓.go 0·生产 7 文件 diff 仅注释行·基线 300→262(38 stale violation 清)ADR 逐字保留·coverage389/389；查4✓6 余红全 docs/api 经 F0 复位·因果 round-trip(仅回退 01 复红 38/恢复复绿 0)；查5✓干净克隆全量 lint exit0 门被跑到绿·真树 policy 唯一中止外国红 | 干净克隆 /tmp/f2_clean(a0ff631b+仅本变更,无外国,.git 目录)全量 `lint.sh` exit0·门 violations0 被跑到·check-openspec `✓ change/code-vocabulary-alignment`·doc_budget ok231/21957·spec_hygiene ok；真树 policy exit1 唯一外国 test_check_runtime_symptoms.py(281 例 1 败,CI 无) | ①F1 verify_contracts.py 实为归档一次性脚本(BASE1c4e165f,6needle×10 文件)→等价断言以 10 冻结拼写文件直接基线 diff 替代(空)②F0 时机耦合:01 改 Go 注释→旧投影 mirror 失效 6 红→F0 重生成复位转绿(符合 2.4 释放表)③doc_budget pre-archive 绿(slack0),F4 归档合并 delta 入 specs 需具名上调(M4)④外国 migrate-kv 零触碰,index.md/migrate-kv.md 重生成逐字复现,提交分离 | 01.1-01.5✓ F0-F3✓（top tasks.md 17 叶核销,唯 F4 待法定停点）；run-notes.md 六项 DoD 逐条带命令+原始退出码+产物引用 | F4 归档+commit 待用户明示(法定停点)；独立审者 R35 frontier 空待跑；外国工作 commit 时须分离 | docs/api godoc 锁释放(F0 完成)→F1-F3 已跑；全量 lint 锁释放(F2 绿)→F3 因果已证；唯 F4 归档锁待用户明示 |
| W2 | 评审修复(R35-2) | 编排者直管(Issue2/3＋连带更名)＋独立只读审者 | 查1✓复跑：门 exit0/hits390 mirror49 exempt341 violations0 baseline263 stale0·自测 exit0/28 例 80 断言(＋8 负锚)·go build/vet exit0·TestTaskRestoreBoundedByCheckpoint ok5.591s·TestEventIntegrityProjection ok0.760s(实跑非空转)·gofmt 空；查2✓改文件对白名单(门/自测/基线/spec delta/design/run-notes＋task_checkpoint_test 回退＋event_integrity_test 更名)；查3✓基线 262→263(＋1 generic-carrier :174)·carrier389→390·收紧零意外新命中(compression-generation/plugin-product/retired-word 0/0)·stripFoldGenerations .go 残留 0；查4✓独立审者逐指纹探针(单行1/跨行1/一全＋一跨2 不同指纹非重复/复合物2)·recorder generation/gen: 零命中·小写 product 不拒；查5✓干净克隆全量 lint exit0 门被跑到绿(787go/79docs baseline263)·ci.yml:109→scripts/lint.sh:10→gates/lint.sh:128-129 policy 块接线确认 | 干净克隆 /tmp/f2_clean(同步改文件)全量 `lint.sh` exit0·门 ok·自测 28/80·check-openspec ✓；真树 policy exit1 唯一外国 test_check_runtime_symptoms.py(CI 无) | ①Issue3 过度更名回退:task_checkpoint_test.go:174「durable carrier」指代 DurableCommit 泛义提交承载面(durable.go:159/commit.go:355),user_ruling 明保→回退＋基线 generic-carrier＋spec delta L13 改例;净 diff 空不入暂存 ②Issue2 门 claim 超 behavior→收紧检测非缩 claim:Product 子串(驼峰复合物)＋短语\s*(零空格命中 foldGeneration)＋逐行扫标识符/字符串＋跨行只记跨 wrap(spans_pieces 去重);docstring 删伪引 GLOSSARY line31 改引 user_ruling ③连带更名 stripFoldGenerations→stripRecorderGenerations(甲纯粹终点:GLOSSARY 行27 规范词 recorder generation,消除禁用短语碰撞,短语规则归零空槽;非乙保留＋豁免);非冻结实证 ④Issue7 范围外:session-checkpoint-material/spec.md 中文载体,双重门外(doc_glob 排除 openspec＋needle 英文),上报用户裁不自行扩门 | run-notes.md 评审修复轮逐条带命令＋原始退出码＋独立审者 FRONTIER EMPTY 裁定(扫描范围＋逐项证据);提交分离计划更新(task_checkpoint_test 出/event_integrity_test 入,跟踪 18＋3 投影=21) | F4 归档＋commit 待用户明示(法定停点);外国 migrate-kv 提交分离;Issue7 中文载体待用户裁是否纳入后续文档对齐 | 独立审者 R35 末轮 FRONTIER EMPTY(无 release-blocking/non-blocking)→放行至法定停点;唯 F4 归档锁待用户明示 |
| W3 | Issue7 修复(用户授权) | 编排者直管(永久 spec 文档词汇对齐) | 查1✓复跑：spec_hygiene exit0(285/286/stale1 无漂移)·doc_budget exit0(231/21957 slack0 行不变)·check-openspec exit0(✓ change)·词汇门 exit0(263/0 face 不变)；查2✓产物＝openspec/specs/session-checkpoint-material/spec.md 4 行词替换(git diff 4+/4-)；查3✓载体仅余 :38 泛义(端口名语义载体)·5 处检查点义→checkpoint·结构/语义零改；查4✓:38 泛义保留正确(非检查点义)·无遗漏检查点义载体；查5✓门扫描面(.go＋docs/api 英文)不变,永久 spec 直接编辑非 delta | 用户 Q2 裁定「一并改 spec 中文载体」→纯文档对齐达干净终点;Q1「先不动我要复核」→不提交/不推送/不归档,工作树留用户复核 | ①Issue7 从范围外残留转用户授权修复:检查点义中文载体(:10/:47×2/:49/:52)→checkpoint,:38 泛义保留 ②直接编辑主 spec(非 delta):词汇规范化语义不变,openspec/AGENTS.md 允许结构有效主 spec 编辑,禁的是 delta 语法入主 spec ③归档记录(session-checkpoint-material-unification)冻结不动,只改 living spec ④doc-budget 行不变故 Issue7 不触棘轮上调(F4 architecture-guardrails delta 合并仍需具名上调) | top tasks.md 01.6✓＋01 域 tasks.md 01.6✓;run-notes Issue7 段更新为已修＋提交分离计划增 spec 文件(跟踪 21→22) | F4 归档＋commit 仍待用户明示(Q1 复核中);外国 migrate-kv 提交分离 | 无新锁;Issue7 修复完成,工作树待用户复核后决定提交/归档 |

> 台账逐轮回填：单完记五查结论＋复跑退出码；波后记独立验证子代理偏差报告；跨域裁决记裁定＋理由＋替代方案；放行记独立审者末轮 frontier 空证据。

### 甲 轮次（取代旧方案后）

| 轮 | 轨 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|---|
| 甲0 | 规划+派发 C | 全仓 carrier 实证 341 .go（45 文件）＋49 docs/api（6 镜像）＝390 红锚；非 Go：architecture.json 2／durable_truth.json 1／wiki 1／GLOSSARY 待注／基线 2377 删。级联符号 grep 实证全私有（commitCarrier 仅 assembly/commit.go；newCarrier 仅 durable.go；seedCarrier 仅 durable_test.go；MemoryRegistry.carrier 仅 lifecycle.go:245/380；logicalRig.carrier 仅 logical_test.go）。golden 耦合裁定：architecture/durable_truth 门不逐字比对代码注释→D 独立无门耦合。 | design.md 甲 重写落盘（D6 映射＝派发权威）；ORCHESTRATION 一/二/三 甲 重写＋四台账勘误 banner | ①旧 checkpoint-sense 改名（journal/restore/memory:418/invoke/checkpoint/segment_store→checkpoint/cut）＋age_segments folding generation→band promotion＋stripFoldGenerations→stripRecorderGenerations 经复核映射正确**保留**；甲 增量＝泛义退役＋门重写＋删基线 ②commitCarrier→commitLedger 据 assembly/commit.go:356 既有散文「the commit ledger behind it」 grounding，非造新词 ③task_checkpoint_test:174 旧 Issue3 回退被甲 推翻→durable commit | C 派发（background）；A/B1/B2 待 C 红锚捕获 | C 完成→捕获 390 红→派 A/B1/B2 | 词汇门红锚锁：C 占用，完成即触发 A/B |