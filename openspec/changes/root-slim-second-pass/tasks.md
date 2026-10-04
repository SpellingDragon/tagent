# Tasks

## 1. A 组：三件零依赖外迁

- [x] 1.1 完成（比计划多两层实况）：
  - `resources.go` → `agent/resources/resources.go`（导出面：`RuntimeResources/Acquire/OpenedResource{Store,Engine}/FingerprintMemory/Canonicalize/OnceRelease/各锁函数/三 Err 哨兵/DefaultResources`；`MemoryConfig`→`config.MemoryConfig`）
  - **测试切分为二**（计划预估的"编译为准"落地）：`TestOwnership_*` 等 12 用例经 `New()` 装配 ⇒ **留根包**（D3 同款裁决），词级限定 `resources.`；`TestBuildFailure_*/TestWriterLock/TestPoisoned_*` 4 用例直接构造 `resourceKey` ⇒ 深白盒**随迁** `agent/resources/entry_test.go`；桩 `seqStore/seqEngine/blockCloseStore` 双侧各持一份（根侧新 `resources_stubs_test.go`）
  - 判据全中：build/vet/根包+agent/resources short 绿、`-race` 73.7s、`GOMAXPROCS=1` 57.2s、lint ok、**测试守恒 207+4=211**、卫生门抓到一次我 `rm` 未清索引（tracked-path-missing→git rm --cached）
  - 自曝：机械替换在 `OpenedResource{store:/engine:}` 字面量上连环误伤（resourceEntry 同名字段、`NewRuntimeResources` 被劈成 `Newresources.`），改用词级正则 + `git checkout` 重做才收敛——教训入 D5
  - 对象：整文件 412 行（`RuntimeResources`/`NewRuntimeResources` 及其方法）；单测 `resources_test.go`(683 行)
  - 做法：`mkdir agent/resources && git mv`；包名 `resources`；文件头索引行随迁（锚不变，`resolveIndexTarget` 按 repoRoot 解析 ✓）；根包调用点（grep `NewRuntimeResources|RuntimeResources|\.resources` 得全集）改 `agent/resources.` 限定或加根别名；`resources_test.go` 同迁试编译，失败处即记录（该测试引用 `acquireDirLock` 等未导出符号时，测试**留在根包**或把被引用符号随迁——以编译为准）
  - 完成：`go build ./...` + `go vet ./...` + `go test ./agent/resources/ . -count=1 -short` 绿；分层断言 `TestArch_LayeredDependencyDirection` PASS（agent/resources 不得 import 根包）
  - 边界：函数体零改动；根包 API `tagent.RuntimeResources` 若被外部用则保留别名，否则直接限定名（grep 全仓定）
  - 前置：无
- [x] 1.2 完成：`git mv` 双文件入 evolution 包；`build_agent.go` 调用点限定（`evolution.NewAssetAuditor/DefaultAssetPatterns/AssetChange`）；`task_record_sink.go` 注释指向改 `evolution.AssetChange`；**坑：迁入后残留自引用 import 报 import cycle**（Go 视同包自 import 为环，移除即愈）；`DefaultAssetPatterns` 内的 `evolution.` 限定同除
  - 对象：整文件 322 行（`FileEntry`/`AssetChange`/`AssetAuditor`/`NewAssetAuditor`/`DiffAssetSnapshots`/`DefaultAssetPatterns`）；单测 `asset_drift_test.go`(197 行)
  - 做法：`git mv` 进 evolution 包；根包调用点（grep 六个导出符号名）改限定；`agent/task_record_sink.go` 的 `CognitiveAssetChange` 是独立类型**不动**，其注释"与根包 tagent.AssetChange 对应"改指 `evolution.AssetChange`
  - 完成：同 1.1 验证墙；`agent/task_record_sink.go` 注释引用不悬空（doc-refs 门）
  - 边界：函数体零改动；evolution 包不得因收留它而新增对根包的 import
  - 前置：无
- [x] 1.3 完成：`git mv` 双文件入 memory 包；**实况修正**：`build_agent.go` 的 `newConsolidationHintTracker(acfg)` 是 wiring.go 里的根包私有包装（与 memory.NewConsolidationHintTracker(threshold, snooze) 签名不同），调用点保持走根包装、仅 wiring 内部限定 `memory.`——比计划少改一处、导出面零扩大
  - 对象：整文件 132 行（`ConsolidationHintTracker` 及方法）；单测 `consolidation_hint_test.go`(110 行)
  - 做法：`git mv` 进 memory 包；`build_agent.go` 4 处调用点（:111/:140/:232/:504）改 `memory.` 限定（该文件已 import memory ✓）
  - 完成：同 1.1 验证墙 + `go test ./memory/ -count=1 -short` 绿
  - 边界：函数体零改动；memory 包新增 import 仅限 event（已是其合法叶子）
  - 前置：无
- [x] 1.4 完成：分两笔独立提交（`580ecbc` resources、`d7904b3` drift+hint），各自 CI 四 job 绿；验证墙全过（-race 72.4s / GOMAXPROCS=1 56.8s / lint ok / godoc 再生成）
  - 做法：显式 pathspec（三对新路径 + 三对旧路径 + 调用点文件 + docs/api 再生成）
  - 完成：验证墙全套（build/vet/根包+受影响包 short/`-race` 根包/`GOMAXPROCS=1` 根包/lint 含六门/openspec strict）；CI 四 job 绿后勾
  - 边界：不混入 B 组

## 2. B 组：世代簿记归域（前置：A 组已提交，避免同批互踩）

- [x] 2.1 完成：簿记段 139 行剪出至 `agent/org/fingerprint.go`（`ComputeOrgFingerprint`/`ExtractOrgSubset`/`CanonicalAgentSubset` 导出——tagent.go 3 处 + 根测试 17 处跨包必需；`orgSubset`/`providerSubset` 留包内）；Config 系列全改 `config.` 限定（含 MeditationConfig）
  - 对象：`org_hotreload.go` 内 `orgSubset`/`providerSubset`/`computeOrgFingerprint`/`extractOrgSubset`/`hotSignature`（约 130 行，行号以 grep 为准）
  - 做法：剪切入新文件 `agent/org/fingerprint.go`（package org，索引行指 `org-hot-reload.md#fingerprint`）；导出 `ComputeOrgFingerprint`/`ExtractOrgSubset`（跨包必需），`orgSubset`/`providerSubset` 留未导出；根包 `org_hotreload.go` 调用点改 `org.` 限定；`hotSignature` 若仅 coordinator 用则随 coordinator 留根（以 grep 调用面定）
  - 完成：`grep -n "computeOrgFingerprint" *.go` 零残留；`go build ./...`；`go test . ./agent/org/ -count=1 -short` 绿
  - 边界：函数体零改动；`agent/org` 不得 import 根包（编译+断言双保险）
- [x] 2.2 完成：五类型 71 行剪出至 `agent/org/status.go`；根包新 `org_alias.go` 五行同名别名（doc 注明实体所在）；根包 8 处调用点零改动
  - 对象：`OrgFailure`/`OrgStatus`/`OrgLiveDebt`/`OrgCloseState`/`OrgAgentApply`（org_hotreload.go 约 65 行）
  - 做法：迁入 `agent/org/status.go`；根包新增 `org_alias.go`：`type OrgStatus = org.OrgStatus` 等五行（doc 注明"别名：实体在 agent/org"，手法同 `config_alias.go`）；根包 8 处调用点**零改动**（别名兜住）
  - 完成：`go build ./...`；`grep -rn "tagent.OrgStatus\|tagent.OrgFailure" tests/ examples/` 若有引用，别名保证其编译通过；全绿
  - 边界：别名不加语义；`orgCoordinator` 与其方法**留根**（发布权，D2 裁决修订入档）
- [x] 2.3 判定：**全部留根**——org_hotreload_test(11 处)/support(2)/candidate 系对簿记符号的引用已改 `org.` 限定；这些用例同锚在 coordinator 发布路径（发布权留根），整文件迁走会失去对未导出 coordinator 行为的触达，收益为负
  - 做法：对 `org_hotreload_support_test.go` 等引用簿记符号的测试，试迁 `agent/org/`（package org 内部测试可触未导出 `orgSubset`）：`git mv` + 包名改 + `go vet`，通过者留、失败者退回并记录
  - 完成：判定结果（迁/留+原因）记入任务注记
  - 边界：断言零改动
- [x] 2.4 完成：本提交（含 org_alias.go、godoc 再生成、lint ok、-race 72.5s、GOMAXPROCS=1 56.9s、四包测试守恒）
  - 做法/完成/边界：同 1.4，另验分层断言 + `gen_godoc`（agent/org 新增导出符号）

## 3. C 组：根包测试按编译判定归位（前置：A/B 已提交）

- [x] 3.1 完成（`tests-relocation-verdict.md/.json` 入档）：**两枚判定陷阱被自己踩中并修正**——首轮探针名 `_probe_test.go` 以下划线开头被 Go 工具链忽略，24/24 全 PASS 是假阳性；真判定（`zz_probe_test.go`）：零改写 **0/24** 可搬
  - 做法：脚本逐个处理根包 `*_test.go`（A/B 已迁走者除外）：复制到 `tests/` 临时文件（`package tagent_test` + import tagent + 顶层符号加 `tagent.` 限定属机械改写，先只试**零改写**直接 `go vet`——失败即判灰盒，不做符号级改写以免引入噪音），`go vet ./tests/` 判定；产出入档 `tests-relocation-verdict.md`：文件/行数/判定/首个未解析符号
  - 完成：判定表覆盖全部剩余测试文件；零改写即编译通过者列出（预计极少，因根包测试多为灰盒）
  - 边界：判定脚本不修改仓库文件（在 /tmp 工作）
- [x] 3.2 完成（零动作，判定支撑）：另测"机械加 tagent. 前缀（20 个别名）"档仍 **0/24**——真墙是包内测试 helper（`cfgFor`/`stubModel`/`testStore`/`delegServed`/`writeGateConfig`/`populatedAgentConfig`/`runtimeConfig`/`defaultPromptsFS` 系），它们正是"经装配根灰盒验证世代/退役/重入"的载体。无可搬者
  - 做法：判定表中零改写通过者 `git mv` 到 `tests/`（同位门核对：tests/ 的锚与根包不同目录不冲突）；每搬一个 `go test ./tests/` 绿
  - 完成：搬后根包 `-list` 计数守恒（搬走的测试在 tests/ 复现）
  - 边界：断言零改动；不做符号级改写搬迁
- [x] 3.3 完成（判定表"首个未解析符号"列即清单）：每个墙符号的出路只有两条——helper 提升 tests/ 共享（符号级改写，本档边界外，留作后续变更候选）或导出生产符号（违背不扩公共 API，否决）。**默认不扩面**
  - 做法：从判定表汇总未导出符号→被哪些测试引用；对每个符号回答"该进 testing.go 导出面吗"（判据：AGENTS.md 导出必要性——外部测试包确需且非实现细节）；**默认不扩**，清单入档供裁决
  - 完成：清单 + 每符号建议（扩/不扩+理由）入档；本变更不执行扩面
  - 边界：不扩公共 API
- [x] 3.4 完成：判定表随本提交入库（纯新增文档+工件，零代码改动）

## 4. 收口（前置：1–3 全部提交）

- [x] 4.1 完成：README 模块表（组合根终形 11 文件清单 + agent/org + agent/resources 两新行，并修正上档遗漏的过时组合根行）；wiki agent-architecture.md 三处（终形、agent/resources 行、已迁出件清单含簿记与状态类型）
- [x] 4.2 完成——前后对照：
  - 根包生产：16,497 行/13 文件 → **14,973 行/11 文件**（-1,524 行；config/org 别名层 +org_alias.go、桩文件不计生产）
  - 根包测试：25 文件 → **24 文件**（resources 深白盒 4 用例随包走，装配级留根）
  - 出根件：resources(412)、asset_drift(322)、consolidation_hint(132)、世代簿记(139)、Org* 类型(71) 合计 **1,076 行归域**，各得其所（agent/resources、evolution、memory、agent/org）
  - 测试归位判定：0/24 可零改写搬、0/24 可机械前缀搬——根包测试群是真实灰盒面（判定表入档）
  - 行为零变更：全程 build/vet/lint ok，-race 与 GOMAXPROCS=1 每组提交后全绿，CI 三笔提交（580ecbc/d7904b3/00f1270）+ 本笔四 job 绿
