# W4b 证据 · 合并单（03.302 装配面 ＋ 03.303 Governor 乙案 ＋ attention 点读迁移）

- 候选指纹：`52d7b664521554fed8d7a5674ee1f934c410a3e3`（分支 `dev`；开工 `git rev-parse HEAD` 重核一致，本单未产生提交）。
- 合同与裁定：[plugin-assembly.md](../../01-contract-evidence/contracts/plugin-assembly.md) §四（`Governor` 实际参与裁决，不允许「声明在流、裁决走旁路」）；[03 design D1](../design.md) 治理行与材料行；ORCHESTRATION 台账「03.303-api 收单」（**Governor 分叉编排者裁乙**：装配裁决面消费管线声明 `Governor`，零新公共接口，`WithPolicy`＝无声明缺省，今日无生产声明者故零行为变化）与「03.302-features 收单」（`attention_log.go:145/169` 遗留改归 W4b）。
- 本单写面（实际）：`assembly/run.go`（`declaredGovernor`/`declaredGovernorOf`、`admissionRuling` 首选声明者、`WithPolicy` 文档改为「无声明时的缺省」）、`assembly/run_test.go`（新锚 `TestGovernorDeclaredInThePipelineRulesTheCall` ＋ 两个夹具类型）、`runtime/task/approval/attention_log.go`（两处完整键 `Scan`→`Get`）、`runtime/task/approval/attention_log_test.go`（替身计数改为点读并同步文案）、`docs/api/github_com__SpellingDragon__tagent__assembly.md` 等 6 页（gen_godoc 全量重生成）、本文件。
- 边界：`assembly/governance_checkpoint.go`、`assembly/tools.go`、`assembly/plugin_catalog.go` 经核对**未改**（见 §一、§二）；`api/plugin/**`、`features/evolution/governance/**` 生产代码未改；`tasks.md` 未勾（不在本单白名单，按 03.301/03.303 先例归编排者）。**白名单外一处（仅测试替身）**：`features/evolution/governance/checkpoint_test.go` 的 `chainReadFailureLog` 补 `Get` 拒绝覆盖，理由与替代见 §五.1。

## 一 装配面结论（03.302 的 `seatGovernance`/`Bind` 触发）

**一句结论**：`Bind` 已由装配关系（host→seat 的拥有者关系）触发，无需改；本叶的装配面是核对而非补丁。

| 核对项 | 落点 | 结论 |
|---|---|---|
| 谁造座位 | `assembly/host.go:578-580`：仅当 `cfg.policy == nil`（宿主自己开治理门）才造 `NewStateSeat(ledger, goals, hostApprovals)` | 座位由**拥有者**造；调用方带自己的 policy 时 `governanceState` 为 nil，宿主不代座 |
| 谁触发 seat | `assembly/host.go:786` `host.seatGovernance(ctx, governanceState)`，`Boot` 内唯一调用点 | 触发者是装配关系，不是消费者或路由判断 |
| seat 做什么 | `assembly/governance_checkpoint.go:53` `RegisterStateSlice` → `:57` 仅当切点未陈述该切片才 `RestoreState(ctx, nil)` → `:65` `seat.Bind(ctx, log)`（`ledger.TierFactLog(run.tier)`） | 顺序固定：挂载→（缺片时）零切点→绑定尾读；不解析治理载荷 |
| 缺片判定 | `governance_checkpoint.go:71-78` `pendingStateSlice` 只比较切片名 | 协调者不读内容，无消费者特判 |
| 审批书同源 | `assembly/host.go:573` `hostApprovalSeat(cfg.policy == nil)` | 与座位同一条拥有者关系：宿主开门才标 seated |
| 读失败不铸新请求 | `features/evolution/governance/checkpoint.go:188-202` `Bind` 三条读各 `%w` 上返；`assembly/host.go:786-789` 失败即 `abortBoot` | 读失败=启动具名拒绝，不落空态、不建第二身份 |

结论与 03.302-features 证据 §一 的只读核对一致；本单未改这三处（停止条件「甲案需加公共接口」未触发）。

## 二 Governor 乙案：管线声明者实际裁决（03.303 assembly 面）

**接线形态**（零新公共接口、零新状态）：

| 环节 | 落点 | 做什么 |
|---|---|---|
| 读声明者 | `assembly/run.go:2234` `declaredGovernor` | 按 `r.current.Plan.Declaration.Agents` 的**声明序**取已发布绑定，用 `:2258 declaredGovernorOf(instances []plugin.Instance)` 取该管线第一个实现 `plugin.Governor` 的实例 |
| 裁决优先级 | `assembly/run.go:2274` `admissionRuling` | 声明者 → 否则 `governorInEffect()`（`WithPolicy`／宿主代际治理面）→ 否则 `r.approver` → 否则 strict 拒绝 |
| 缺省语义 | `assembly/run.go:1495-1504` `WithPolicy` 文档 | `WithPolicy` 改为「无声明时的缺省」，不再是「唯一执行前裁定」 |

**为什么读已发布计划而不是 `installGovernor`**（反事实）：`installGovernor` 的写入点在 prepare（`assembly/prepare.go`→`tools.go:234` supplier）与代际治理面（`host_document.go:1507/1519`），而 prepare 先于 `publisher.Publish` 执行。候选失败／未变／回滚时 `built.Restore()` 只还原代际消费者；声明者若已写进 `Run.policy` 就没有还原点，会把**从未生效代际**的裁决面留给在役绑定。读 `r.current` 让「在效裁决面」＝「在效绑定」，不需要第二状态，也不需要改 `assembly/commit.go`（adopt 路径，白名单外）。

**为什么不用 `Pipeline.governors`**：给流加裁决 pass 即新增框架级接口（03 proposal 明列不新增；ORCHESTRATION 要求先回设计门）。`Pipeline.governors` 仍是「只收集不派发」——流的裁决一律由装配的受理面发出，这是同一份声明的唯一消费者。

**语义保持**：裁决仍逐 call 解析（reload 后新代际的声明者立即生效；无声明者时既有「reload 安装的治理表仍裁决在役绑定」不变）。`WithPlugins` 是 boot 级集合（reload 不重建），故贡献的 Governor 跨 reload 继续裁决。

**红证（乙案接线前该锚红）**：隔离副本 `/tmp/tagent-w4b-red`（rsync 现树，排除 `.git`/`logs`/`docs/.dev`），只把 `admissionRuling` 换回未接线的两行（保留未使用的 `declaredGovernor`），锚原样跑：

```
go test ./assembly -run '^TestGovernorDeclaredInThePipelineRulesTheCall$' -count=1 -v
```

`rc=1`，两腿各红在「声明者未被问」：

```
run_test.go:1158: the declared governor was asked 0 time(s), want the one ruling of the call
run_test.go:1197: the declared governor was asked 0 time(s), want the one ruling of the refused call
```

副本已 `rm -rf`，主树零变异。

**绿证（现树）**：同命令 `rc=0`，父＋2 子 PASS：

```
--- PASS: TestGovernorDeclaredInThePipelineRulesTheCall (0.00s)
    --- PASS: .../the_declared_governor_rules_the_call_and_the_assembly_policy_is_not_consulted
    --- PASS: .../the_same_governor_refuses_the_call_before_any_execution
```

断言面：① 声明者被问恰 1 次且收到的 `pass.Final.Function.Name` 是真实调用；② 放行时工具执行恰 1 次、`WithPolicy` 的毒化裁定面**零次**被问（「`WithPolicy` 不再被咨询」的直接证据）；③ 同一 Governor 拒绝时工具执行 0 次，模型读回 reason＋rule；消费者＝公开 `Assemble`＋`Run.Invoke`＋每个原生工具都过的受理 wrap。

**今日生产零行为变化的可复跑事实**：生产侧实现 `PortCallRuling` 的只有 `features/evolution/governance/policy.go:98`（经 `installGovernor`/`WithPolicy` 装入，从不作为管线实例），`grep -rn "PortCallRuling" --include=*.go . | grep -v _test.go` 无第二处；`WithPlugins` 生产调用点只有 `assembly/host.go:636/702`（回放调用者工厂＋宿主自己的 `hostPlugins`），其实例均非 Governor。故 `declaredGovernor` 今日恒 nil，裁决路径与改前逐字相同——`TestHotReloadGovernanceNoRestart`、`TestGovernanceRejectionReceiptIsHonest` 复跑仍绿（§四）。

## 三 attention_log 完整键点读迁移（遗留项）

`runtime/task/approval/attention_log.go` 两处 `Scan(完整键)` 改 `Get(键)`：

| 位置 | 改前 | 改后 | 语义保持 |
|---|---|---|---|
| `:145` `requestRecordOnFace` | `stored, err := log.Scan(ctx, key)` ＋ `stored[key]` | `raw, kept, err := log.Get(ctx, key)` | 三态不变：有值 `(raw,true,nil)`／无值 `(_,false,nil)`／读不到 `(_,false,err)`（`durable.FactLog` 契约原文）；错误文案、键名、`errApprovalFactConflict` 判据（`record.Request.ID`/`record.Asked`）逐字保留 |
| `:168` `factOnFace` | 同上 | 同上 | 同上；`factKey(fact.Key) != key` 冲突判据保留 |

其余 `Scan` 均为**族扫描**（`storedRequestRecords` 的 `approvalRequestPrefix`、`foldAttentionFacts` 的 `attentionFactPrefix`），未动——与 03.302 证据 §四 的定性一致。

**代价面随迁移同步（替身，白名单内）**：`attention_log_test.go` 的 `factStore` 把 `scans` 计数改为 `reads`，由 `Get` 计数（命中时 `pairs++` 计解码体）；`TestAttentionBindReadsTheObligationsNotTheHistory` 与 `statedBootCost` 的断言/日志改为「点读」。实测：

```
stated bind beside 12 settled rounds: reads=3 decoded=0 resident=3 allocated=3328 bytes
stated bind beside 24 settled rounds: reads=3 decoded=0 resident=3 allocated=3328 bytes
bind beside no stated cut: point reads=0 decoded=78
```

即：陈述切点的 bind 只为 3 个未决义务付 3 次点读、与历史长度无关；无切点的 bind 走族扫描解码整条流（78），反证「平读是切点自己的，不是替身看不见的」。负锚 `TestApprovalHistoryReadFailureDoesNotMintANewRequest` 经 `Get` 拒绝答键：bind 具名失败、未决 hold 不落空、store 仍只有 1 条 request record（不铸第二身份），PASS。

## 四 验证（逐条独立语句，原始退出码）

| 命令 | rc | 关键输出 |
|---|---|---|
| `go test ./assembly -run '^TestGovernorDeclaredInThePipelineRulesTheCall$' -count=1 -v` | 0 | 父＋2 子 PASS（§二） |
| `go test ./assembly -run '^TestGovernanceSliceWiredInProduction$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestGovernanceApprovalBootReadsStateSlice$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestGovernanceBootRefusesUnreadableStateCut$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestGovernanceBootReadsSettledAnswerByIdentity$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestGovernanceBootWorkTracksOpenItemsAndTail$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestGovernanceRejectionReceiptIsHonest$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestHotReloadGovernanceNoRestart$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestPluginDeclarationRefusesAnUnregisteredName$' -count=1 -v` | 0 | `--- PASS` |
| `go test ./assembly -run '^TestSymptomAnchorHostBootMountsContributedPlugin$' -count=1 -v`（S1） | 0 | `--- PASS` |
| `go test ./runtime/task/approval -run '^TestSymptomAnchorFailedRulingKeepsTheHold$' -count=1 -v`（S2） | 0 | `--- PASS` |
| retry 五锚：`TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry`、`TestConcurrentSameFactKeepsOneApplication`、`TestRetryLeavesPresentedRangeAtTheLatestRound`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestSymptomAnchorRetryAutoRedrive`（各 `-run '^名$' -count=1 -v`） | 0 | 各 `--- PASS` 1 |
| `go test ./assembly -count=1` | 0 | `ok github.com/SpellingDragon/tagent/assembly 388.169s`（全包，最终树；上线一次接线前 380.300s，两次均绿） |
| `go test ./features/evolution/governance ./runtime/task/approval -count=1` | 0 | `ok ...governance 0.248s` / `ok ...approval 0.914s` |
| `go test ./runtime/task/approval -race -count=5` | 0 | `ok ...approval 2.950s` |
| `go build ./...` | 0 | 空输出 |
| `go vet ./...` | 0 | 空输出 |
| `gofmt -l assembly runtime/task/approval features/evolution/governance` | 0 | 空列表 |
| `python3 scripts/gates/check_plugin_mounting.py` | 0 | `errors=0`；`check_plugin_mounting: ok` |
| `bash scripts/gates/gen_godoc.sh` | 0 | `wrote 83 files to docs/api` |
| `bash scripts/gates/gen_godoc.sh --check` | 0 | `docs/api matches the source (82 packages)` |
| `go run ./scripts/codetools package-layout assembly` | 0 | `68 file(s); names, skeleton pins and test mirrors hold` |
| `go run ./scripts/codetools package-layout runtime/task/approval` | 0 | `8 file(s); ... hold` |
| `go run ./scripts/comment_policy --no-baseline -v assembly` | 0 | 本单 0 项；残余 1 项为继承债（见 §五.3） |

## 五 偏离与自决

1. **白名单外一处（仅测试替身，须编排者裁定是否接受）**：C 迁移后 `features/evolution/governance` 的既有锚 `TestGovernanceHistoryReadFailureIsNamedNotAbsent` 转红，原因在替身不在产品：`chainReadFailureLog`（`checkpoint_test.go:1373-1395`）只覆盖 `Scan` 的拒绝，其继承的 `sliceLog.Get` 对同一键答「不持有」——按 `durable.FactLog` 契约，读不到必须以读失败作答，故该 `Get` 本身就是违约替身。修法＝给替身补 13 行 `Get`（`checkpoint_test.go:1385`，拒绝同一键、记同一次 `refusals`、其余委派），锚随即 PASS 且两腿仍各只付一次具名拒绝。替代方案（回退 C）会让本单交付缺项；维持红树不合派单验收。**该文件不在本单白名单，本单已改并要求裁定为「迁移的必需伴随件」。**
2. **自决（乙案实现形态）**：派单写「消费 `Pipeline.governors`」，但该集合无生产读者且不在 `api/plugin` 白名单（加访问器＝公共接口变化）。改读已发布绑定的实例表（`binding.Plugins`），与 03.303-api 证据 §三 的乙案描述逐字一致；`plugin.NewFlow` 仍由 loop 与事实面自行组流，未新增派发通道。
3. **未勾 `tasks.md` 的 03.302/03.303 两行**：不在白名单；两叶的 assembly 面由本单补足，勾选留编排者。
4. **多重声明边界（登记，不扩面）**：一个 run 只有一条受理裁决面；若同一管线（或不同 agent）声明多个 `Governor`，按声明序**第一个**裁决，第二张声明脸仍「声明而不被问」。合并多裁决是语义设计决策（拒绝/合议/优先级），越过本单授权与 03 proposal「不新增框架级接口」。今日无生产声明者，故零行为变化。
5. **文档漂移（白名单外，未改）**：`docs/wiki/plugin/plugin-architecture.md` §四「裁决」行仍写「`policyRuling` 读回 pass → `Governor.Judge`」，未点明「问哪一个 Governor」。本单改变了该句的完整语义，但 wiki 不在白名单；建议编排者收口时补一句「问已发布管线声明的 Governor，无声明时问装配 policy」。
6. **继承债（非本单）**：`go run ./scripts/comment_policy --no-baseline -v assembly` 余 1 项 `assembly/task_checkpoint_test.go:528 test-doc-not-one-sentence`，属 02.205 落地文件的既有项（本单未触该文件）。
7. **`check_plugin_mounting` R5b 触发的形态调整**：首版把 `for _, instance := range binding.Plugins` 写在 `declaredGovernor` 内，门禁报 `R5b C4 ... 声明名集合不是它读的`（该门要求 `range <x>.Plugins` 只出现在「读声明名」的场景）。改为 `declaredGovernorOf(binding.Plugins)`（函数参数接收实例表，函数内 range 参数）后 `errors=0`；语义不变，且更贴合「读管线实例表」的单一职责。

## 六 产物

- 修改：`assembly/run.go`（`declaredGovernor` :2234、`declaredGovernorOf` :2258、`admissionRuling` :2274、`WithPolicy` 文档 :1495-1504）、`assembly/run_test.go`（锚 :1133 起＋夹具 `pipelineRuler`/`pipelineRuledTool`）、`runtime/task/approval/attention_log.go`（:145/:168）、`runtime/task/approval/attention_log_test.go`（`factStore.Get` 计数、`counters` 与代价断言文案）、`features/evolution/governance/checkpoint_test.go`（`chainReadFailureLog.Get` :1385，白名单外一处，§五.1）、`docs/api/**`（gen_godoc 重生成 6 页）。
- 新增：本文件。
- 未 commit。

## 七 W4b-c 收尾（C 终态 ＋ 全量复跑）

**现场澄清（与派单文字的差异，如实登记）**：派单写「`attention_log.go` 仍有 2 处 `log.Scan`（原 :145/:169）」。开工复核：HEAD 的 :145/:169（`requestRecordOnFace`/`factOnFace`）在 §三 已迁 `Get`；树内现存 `Scan` 在 :121/:301，两处均为**族扫描**。故 C 的剩余不是迁移，而是**逐处定性并注明**——迁移未重做、未回退。

### 7.1 C 终态（四处读形逐处定性，现树行号）

| 位置 | 读者 | 读形 | 定性 |
|---|---|---|---|
| `:149` `requestRecordOnFace` | `Lookup`/`Decide` 按身份点读 | `log.Get` | 完整键点读；三态按 `durable.FactLog` 契约（持有／从未持有／读失败） |
| `:172` `factOnFace` | 问答事实按身份点读 | `log.Get` | 同上 |
| `:121` `storedRequestRecords` | `foldRequestRecords`（boot 座次 03.302）＋ `lookupOnChain`（按 tool/args 找记录） | `log.Scan(approvalRequestPrefix)` | **族读，合法保留**：键名是调用者身份，两类读者都要「面上全部记录」，无法自造键；`:115-119` 注明读形与「扫描读不到按失败上报，不当作面上无记录」 |
| `:301` `foldAttentionFacts` | 无切点时 `bind` 的平读 fold | `log.Scan(attentionFactPrefix)` | **族读，合法保留**：流内事实由 log 命名而非 fold 方，fold 必须全达；`:296-299` 注明同上 |

两处族读的注明是**注释面新增**（读/写逻辑零改动）；两者均为未导出函数，不进 godoc 面（`grep -c storedRequestRecords docs/api/...approval.md` = 0）。

### 7.2 全量复跑（现树；本单最后一次 `.go` 写之后取数）

**绑定**：现树 = HEAD `52d7b664` ＋ 未提交工作树；本单增量仅 `attention_log.go` 注释两段（+7 行）、`docs/api/...approval.md`（见 7.3）、本文件。下列各 rc 均在此终态树取得，跑期间 `git diff --stat` 与 HEAD 逐项复核未变（工作树稳定）。

| 命令 | rc | 原文 |
|---|---|---|
| `go test ./runtime/task/approval -race -count=5` | 0 | `ok ... runtime/task/approval 3.052s` |
| `go test ./assembly -run '^TestGovernorDeclaredInThePipelineRulesTheCall$' -count=1 -v` | 0 | 父＋2 子 `--- PASS`（0.00s；子：`the_declared_governor_rules_the_call_and_the_assembly_policy_is_not_consulted`、`the_same_governor_refuses_the_call_before_any_execution`） |
| 同上 `-race -count=3` | 0 | `ok ... assembly 1.435s` |
| 治理七锚（逐锚 `-run '^名$' -count=1 -v`） | 各 0 | `TestGovernanceSliceWiredInProduction`(0.19s)、`TestGovernanceApprovalBootReadsStateSlice`(0.18s)、`TestGovernanceBootRefusesUnreadableStateCut`(0.00s)、`TestGovernanceBootReadsSettledAnswerByIdentity`(0.17s)、`TestGovernanceBootWorkTracksOpenItemsAndTail`(0.50s)、`TestGovernanceRejectionReceiptIsHonest`(0.08s)、`TestHotReloadGovernanceNoRestart`(0.10s)——**7 PASS / 0 SKIP** |
| 症状与回归九锚（逐锚 `-count=1 -v`） | 各 0 | S1 `TestSymptomAnchorHostBootMountsContributedPlugin`(0.11s)［assembly］、S2 `TestSymptomAnchorFailedRulingKeepsTheHold`(0.00s)［approval］、retry 五 `TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry`(0.06s)/`TestConcurrentSameFactKeepsOneApplication`(0.08s)/`TestRetryLeavesPresentedRangeAtTheLatestRound`(0.00s)/`TestRetryOfFoldedOutputDoesNotReopenTheRound`(0.00s)/`TestSymptomAnchorRetryAutoRedrive`(1.01s)、`TestTaskRecoveryNormalizesCheckpointAndTail`(10.19s)、`TestBootMountsAContributedStateSliceAndRestoresIt`(0.22s)——**9 PASS / 0 SKIP** |
| `go test ./assembly -count=1` | 0 | `ok ... assembly 371.497s` |
| `go test ./features/evolution/governance ./runtime/task/approval ./api/plugin -count=1` | 0 | `ok ...governance 0.533s` / `ok ...approval 0.526s` / `ok ...api/plugin 0.676s` |
| `go build ./...` | 0 | 空输出 |
| `go vet ./assembly ./features/evolution/governance ./runtime/task/approval ./api/plugin` | 0 | 空输出 |
| `gofmt -l assembly runtime/task/approval features/evolution/governance` | 0 | 空列表（0 文件） |
| `python3 scripts/gates/check_plugin_mounting.py` | 0 | `go_files=32 containers=5 mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`；`check_plugin_mounting: ok` |
| `bash scripts/gates/gen_godoc.sh` | 0 | `wrote 83 files to docs/api` |
| `bash scripts/gates/gen_godoc.sh --check` | 0 | `docs/api matches the source (82 packages)` |
| `go run ./scripts/comment_policy --no-baseline -v runtime/task/approval` | 0 | `0 finding(s) over [runtime/task/approval]`（另有 1 条仓外 git-ignored 目录提示，非 finding） |

### 7.3 未竟与自决

1. **`tasks.md` 03.302/03.303 仍未勾**：不在本单白名单，按 03.301/03.303 先例归编排者（与 §五.3 一致）。
2. **docs 同步的既有漂移**：本单 `gen_godoc` 重生成吸收了 `approval_store.go` 既有 `Decide` 契约注释（`docs/api/...runtime__task__approval.md` 相对 HEAD +7 行）。该源码注释属先前波次的在飞改动（`approval_store.go` +63 行，本单未触），其文档页此前落后于源；重生成后 `--check` rc=0。**本单产品代码改动仍只有 `attention_log.go` 两段注释**；未导出函数不进 godoc 面。
3. **`assembly/run.go`/`run_test.go` 本单零改动**：B 锚语义无残缺（父＋2 子 PASS、负腿在锚内），最小修补条件未触发。
4. **C 的停止条件未触发**：两处 `Scan` 定性为族读，未改变族/前缀枚举语义，无产品逻辑字节变化。
