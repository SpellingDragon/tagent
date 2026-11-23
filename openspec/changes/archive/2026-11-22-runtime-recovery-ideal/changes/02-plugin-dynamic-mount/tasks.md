> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 02 插件动态挂载 · 孙任务

> **当前承接（R265）**：目录与声明消费已有实现，原2.4暂未交付的压缩消费者换代/条件注册由02.R1和12.R3共同核销，不因原2.4勾选而消失。02.R1等待的是12.R3的生命周期验收；12.R3使用已有装配能力，不反向等待02.R1完成。旧证据只作历史，不宣告整域可独立归档。

## 当前重整叶

- [x] 02.R1 在12.R3完成后复核 builtin、贡献实例、禁用、替换及待恢复状态的同一声明入口；不得把丢失消费者的义务静默移除。文件白名单：`assembly/plugin_catalog.go`、`assembly/compress_mount.go`、`assembly/plugin_catalog_test.go`（共享run/host/state接线由12单写）—— 正锚：`go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature|TestPluginDeclarationCarriesAContributedInstance|TestPluginDeclarationDisablesABuiltinPlugin)$' -count=1 -v`；负锚：`go test ./assembly -run '^(TestBuiltinCatalogRefusesADuplicateName|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1 -v` 加12.R3拟新增义务交接反例；消费者锚：`go test ./tests/integration/acceptance -run '^TestScenario11ConfigDrivenSubagentAndPipeline$' -count=1 -v`，原测试之外的卸载欠账覆盖不得假称已经存在。

## 原执行任务与凭据（历史，不推导当前完成）

> verify 三要素随任务行冻结（R29）：**正锚**（点名测试＋可复跑命令，测试名与 `-run` 正则逐字符一致）、**负锚**（具名错误路径，勾选硬门）、**消费者锚**（正式入口/真实宿主路径实测；测试手工补接线不算生产消费者）。验收面不得追认缩水，缩水＝该叶重开。
> 共同门：每叶结账前 `go build ./...` exit 0、`go test ./assembly/ ./api/config/ ./runtime/state/ -count=1`、`python3 scripts/gates/check_plugin_mounting.py` exit 0（build/test 与 commit 永不串联）。
> 触碰面：`assembly/{plugin_catalog,compress_mount}.go`（新）、`assembly/{run,tools,host,host_document}.go`、`api/config/config.go`（字段注释）、新增 `assembly/plugin_mount_test.go`；不改 `scripts/gates/`、`golden/`、`api/config/{decode,clone,fingerprint}.go`、部署 yaml（缺省全挂）。

- [x] 2.1 builtin 插件目录＋默认启动注册（`assembly/plugin_catalog.go` 一行一插件、7 项、带作用域；`assembly/compress_mount.go` 承载压缩消费构造；`Run.mountBuiltinPlugins` 走 prepare→run 两趟；`hostPlugins(seam)` 走宿主遍历，回填 `driver`/`settleDriver`；装配根零特性名）—— **凭据**：正锚 `go test ./assembly/ -run 'TestBuiltinCatalogMountsEveryFeature|TestHostPluginsMountThroughTheCatalog' -count=1` → `ok`（目录 7 项齐、run 级集合含 compress/final-round、宿主面经目录回填）；负锚 `go test ./assembly/ -run TestBuiltinCatalogRefusesADuplicateName -count=1` → `ok`（同名两项具名拒绝 "registered twice"）；消费者锚 `python3 scripts/gates/check_plugin_mounting.py` → `mounts=0 ... registry_impl=2 present=2 errors=0`＋`go build ./...` exit 0。

- [x] 2.2 `plugins:` 声明接通＋定制（`hostAgent.Plugins` `yaml:"plugins"`＋`mapDocument` 携带；`Run.agentPlugins` 以 `range spec.Plugins` 解析；缺省全挂、`-name` 禁用、`name` 幂等点亮、宿主贡献实例按 `Name()` 可点名/可禁用；空名／既非内置亦非贡献名／run·host 域内置名具名拒绝）—— **凭据**：正锚 `go test ./assembly/ -run 'TestHostDocumentCarriesTheDeclaredPluginMount|TestPluginDeclarationDisablesABuiltinPlugin|TestPluginDeclarationMountsANamedPluginOnce|TestPluginDeclarationCarriesAContributedInstance' -count=1` → `ok`（真实 `loadHostDocument`→`Declaration` 逐项等于文档所写；`-age-fold` 后管线不含 age fold 且 run 级仍在；重复点名只挂一个）；负锚 `go test ./assembly/ -run 'TestPluginDeclarationRefusesAnUnregisteredName|TestPluginDeclarationRefusesARunLevelPluginName' -count=1` → `ok`（未注册名与 `-compress-consumer` 均具名拒绝并列出 agent 级内置）；消费者锚 `python3 scripts/gates/check_plugin_mounting.py` → `consumers=1`（C1-C4 全绿，声明非死字段）＋`go test ./tests/integration/acceptance/ -run TestScenario11ConfigDrivenSubagentAndPipeline -count=1` → `ok`（宿主贡献实例名声明仍被接纳，未误伤扩展面）。

- [x] 2.3 七处硬编码清零（删 `run.go:596-599/614-619`、`tools.go:247-258`、`host.go:689-731` 的具名构造与单成员/复合字面量 append，改为转发目录解析集合；管线顺序 contributed → run 级 → agent 级不变；`resolvePluginDeclaration` 更名 `declaredPluginsOf` 以免撞 01 的 `resolve<Feature>` 判据）—— **凭据**：正锚 `python3 scripts/gates/check_plugin_mounting.py` exit 0 且摘要 `mounts=0 consumers=1 errors=0`（R5a 七处与 R5b 三条同时转绿）；负锚 `python3 scripts/gates/test_check_plugin_mounting.py` exit 0（13 条自测全绿，判据口径未削弱）＋`python3 scripts/gates/check_assembly_symbols.py` → `ok`（01 的 R2 亦绿）；消费者锚 `go test ./assembly/ ./api/config/ ./runtime/state/ -count=1` → api/config、runtime/state `ok`，assembly 仅 `TestSymptomAnchorBootCostFlatAcrossGenerations` 红（W0 症状直译锚，ORCHESTRATION.md:145 记为其设计上的红，归 01/06 未落地）。

- [x] 2.4 行为保真与剩余登记（`go build ./...` exit 0；三包测试如 2.3 所述；压缩消费的代际重建与 `compressStateSlice` 条件注册**本波不交付**，在 design 要点 7 显式登记；部署 yaml 无需补声明，因缺省全挂）—— **凭据**：正锚 `go build ./...` exit 0＋`go test ./assembly/ ./api/config/ ./runtime/state/ -count=1` 见 2.3；负锚 `go test ./assembly/ -run TestPluginDeclarationRefusesARunLevelPluginName -count=1` → `ok`（agent 声明不能静默关掉 run 级内置）；消费者锚 `go test ./tests/integration/host/ -run 'Compress|AgeFold|FinalRound' -count=1` → 除 `TestCompressProductRestoresAfterRestart` 外全绿；该项红经**归因实测**：把在树的 `relocatableStorage`（域 06 未提交的退位接线）临时置为不设 `RetireFacts` 后该用例转绿、原文件随即恢复 ⇒ 红因退位在途，非本域。

## 02.R1 执行记录（2026-11-18 · openspec-apply 实施，复核叶，零 .go 写入）

**结论：声明入口已成立，无需调整。** 五态（builtin／贡献实例／禁用／替换／待恢复）的解析全部落在唯一入口 `declaredPluginsOf`（`assembly/plugin_catalog.go:275-314`），其唯一生产调用者是 `Run.agentPlugins`（`:380-403`）→ `assembly/tools.go:245-247`（管线工厂）→ `assembly/prepare.go:124` → `AgentBinding.Plugins`（`:132`）→ 模型 Before/After 两遍（`assembly/facts.go:969` `plugin.NewFlow(binding.Plugins)`）。目录侧唯一构造点 `builtinPluginCatalog`（`:110-206`），boot／宿主／agent 三条路径都只遍历它（`mountBuiltinPlugins` `:333-356`、`hostPlugins` `:361-374`、`agentPlugins` `:392-401`）；`checkPluginCatalog`（`:210-222`）在 boot 首步拒重名/空名。

**逐态复核（引 file:line）**：
- **builtin**：7 项常量名各一（`:33-41`），`TestBuiltinCatalogMountsEveryFeature`（`plugin_catalog_test.go:58`）钉目录 7 项＋run 级集合＋`ageFold.executor==compressExecutor(run.compress)`（`compress_mount.go:24` 是 age fold 取执行者的唯一接缝）。
- **贡献实例**：`declaredPluginsOf` 的 `carried` 集合（`:276-279`）与 `keeps`（`:266-268`）是贡献实例的唯一读面；`TestPluginDeclarationCarriesAContributedInstance`（`:244`）钉点名与 `-` 禁用两臂。**生产今日无贡献实例生产者**（`NativeAgent.Plugins` 无非测试写入点，grep 仅 `host_document.go:213` 的声明字符串与 `skeleton_assert.go`），故该面是既有扩展面而非本叶新增。
- **禁用**：agent 级 `-name` → `disabled`／`unmounted` 两表（`:295-311`）；run／host 级名一律具名拒绝并报作用域（`:302-306`＋`carriedBy` `:318-326`），`TestPluginDeclarationRefusesARunLevelPluginName`（`:157`）钉 `-compress-consumer` 不可静默关掉。
- **替换**：无第二条路径——世代重建每次走 `prepare.go:124` 重新解析，声明变更经 `api/config/fingerprint.go:208` 进指纹、`assembly/host_reload.go:717` 记 `plugins` 差异；目录内不存在「同槽位换实例」的隐式替换，故不存在绕过声明入口的替换面。
- **待恢复状态**：`plugin_catalog.go` 不 import `runtime/state`、不触碰 `StateRegistry`；义务交接在 `Run.RegisterStateSlice`＋12.R3 的 `unfinishedWork` 面（`assembly/checkpoint.go:286`）。生产 `RegisterStateSlice` 三处调用点（`governance_checkpoint.go:53`、`host_delivery_checkpoint.go:152`，另一为定义）**均非目录内插件**，故「禁用/卸载某个 builtin 即丢掉切片消费者」的路径在声明面上不可达——12 域 E12-7 的勘误在本叶侧复核成立。

**义务不静默移除的联合锚**：`TestDisabledBuiltinHandsOverItsObligations`（`plugin_catalog_test.go:173`）三查同绿——禁用后 run 级集合仍在（`:199`）、注册表仍点名该切片（`:202`）、cut 仍陈述 `owedItem`（`:210`）、`unmount` 具名拒绝并报 `1 unfinished obligation(s)` 且拒绝后名字仍在（`:213-219`）。

**凭据（原始退出码，未用管道退出码冒充）**：
- 正锚 `go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature|TestPluginDeclarationCarriesAContributedInstance|TestPluginDeclarationDisablesABuiltinPlugin)$' -count=1 -v` → 3 PASS / 0 FAIL，RAW_EXIT=0。
- 负锚 `go test ./assembly -run '^(TestBuiltinCatalogRefusesADuplicateName|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1 -v` → 2 PASS，RAW_EXIT=0。
- 负锚二（12.R3 义务交接反例）`go test ./assembly -run '^(TestDisabledBuiltinHandsOverItsObligations|TestPluginDeclarationRefusesARunLevelPluginName)$' -count=1 -v` → 2 PASS，RAW_EXIT=0。
- 消费者锚 `go test ./tests/integration/acceptance -run '^TestScenario11ConfigDrivenSubagentAndPipeline$' -count=1 -v` → 1 PASS（3.02s），RAW_EXIT=0。该场景用 `t.TempDir()`＋golden 场景契约＋替身模型，未读真实数据根、未要凭证，属可跑面（非禁跑面）。
- 共同门 `go build ./...` RAW_EXIT=0；`go test ./assembly/ ./api/config/ ./runtime/state/ -count=1` RAW_EXIT=0（assembly 320.025s / api/config 1.110s / runtime/state 0.565s 全 ok）；`python3 scripts/gates/check_plugin_mounting.py` RAW_EXIT=0（`mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`）。

**未证面（不假称已存在）**：①「禁用声明 + 重启后待恢复义务」的联合锚今日不存在（`TestDisabledBuiltinHandsOverItsObligations` 只断言同一进程内 cut/拒绝；重启侧仅 12.R3 的 `TestUnmountKeepsTheOwedItemsOfItsSlice` 且不带声明）——因声明面与 `StateRegistry` 无因果关系（前者不注册切片、后者注册点均非目录插件），未补该锚；若后续出现「声明可禁用持切片插件」的生产路径，此处须重开。②贡献实例名与 builtin 名相同时声明无法分别寻址（`unmounted` 仅在 `builtinPluginNamed` 未命中时写入，`:288-301`）——今日无生产者，按「不为无第二使用方的结构建防御」登记不修，出现生产者时须具名拒绝该重名的宿主贡献。③reload 后插件集替换的行为未单独立锚，只引 `host_reload.go:717` 与 `prepare.go:124` 的代码事实。
