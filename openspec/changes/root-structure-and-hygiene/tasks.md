# Tasks

## 1. P1 追踪卫生

- [x] 1.1 （实测：10 个残骸目录全为 0 字节、仅含 lock/journal、无 .md/.json 数据后删除；`.pre-isolation-backup/`、`.tagent-workspace/` 按约保留）`git rm` hottest-sub1/ hottest-sub2/（4 个零字节文件）；删孤儿 `go.work.sum`；清工作树遗物（own-*、hottest-sub3、hottest-drop-*；`.pre-isolation-backup/`、`.tagent-workspace/` 属运行/备份数据，默认保留）
- [x] 1.2 （实现为 `codetools tracked-hygiene` 子命令，纯函数 + 3 条单测；接入 `scripts/lint.sh` 与本地同命令；负路径双红留痕 `hygiene-probe.log`）新增 `scripts/check_tracked_hygiene`（空文件/运行期后缀/顶层白名单三查）并接入 validators job；负路径探针（空文件与 `x.lock` 各造一枚）双红留痕后撤除
- [x] 1.3 （`go test ./... -short` 31 包全绿零 FAIL、`GOMAXPROCS=1` 根包 59.6s ok、lint ok 含新门）全量 short + lint 绿；提交

## 2. P2 退役陈旧 AReaL 训练桥 + dotted-refs 门（依 D6/D7 与用户裁决）

- [x] 2.1 （桥三件 git rm；convert_trajectories.py git mv 至 scripts/；README 知识并入 wiki rl-architecture 新增 #offline-converter 与退役记录；白名单重生成 21 目录不含 train）删除 `train/rl/tagent_adapter.py`、`examples/wechat-bot/train_tagent.py`、`examples/wechat-bot/train_rl_config.yaml`；`git mv train/rl/convert_trajectories.py scripts/convert_trajectories.py`；`train/rl/README.md` 中仅转换器相关内容并入 wiki rl 页，其余随桥退役；顶层白名单去 `train`
- [x] 2.2 （run.sh 摘除 areal 命令族/变量/状态块共 59 处，bash -n 过；切除留下的孤儿 shift;; 与无标题横幅两处手术伤已修；`rl` 运行时模式与 AREAL_API_KEY 认证行保留；root/bot README 同步）`run.sh` 移除 areal 命令族与 AREAL_* 变量（`rl` 运行时模式保留）；root/bot README 与 wiki `rl-architecture` 同步：退役事实、保留面（`rl/` Go 包、转换器、RL 运行时配置）、重接条件
- [x] 2.3 （codetools dotted-refs：位置精确提取 + 纯函数判定 + 2 组单测；接入 lint.sh；探针红/复原绿留痕 dotted-refs-probe.log；期间 doc-refs 连锁抓到退役记录的悬空引用并已改写）新增 `codetools dotted-refs`（位置精确：`python -m` 后续 token 与 yaml `workflow:` 值；仓库内解析或显式外部允许表）+ 单测 + 接入 lint.sh；负路径探针（造一条指向已删布局的 workflow 引用必红）留痕
- [x] 2.4 （`go test ./... -short` 31 包零 FAIL；bot 模块三连绿 final rc=0——首跑曾见一次无法复现的 exit status 1，如实记档交 CI 裁决；lint ok 含两道新门）全量 short + lint（含两新门）绿；提交

## 3. P3 结构一步到位（D10/D11）

### C1 配置模型外迁 config 包

- [~] 3.2 config.go → config/ 包（代码已完成，工作树未提交；背景见 design D12）
  - [x] config/ 五文件成形（config/modelref/clone/lifecycle/config_test）+ 根包 config_alias.go（20 类型别名 + 2 常量 + 2 函数包装）+ IsRemoteRef 导出化 + 4 个注册表系测试并回 registry_test.go + build/vet/config 测试/根包全量 short 57.9s/gen_godoc/hygiene 白名单 全过

  - [x] 3.2a 清 org_hotreload 最后一条 lint finding（孤儿行已删；clone.go 的 Clone doc 归一，期间自查出两处自伤：续行漏 `// ` 前缀、doc 与函数间多空行 ⇒ free-standing；最终 comment_policy 0 finding）
    - 对象：`org_hotreload.go` 约 440 行、`orgSubset` 类型 doc 上方的孤儿行，原文恰为：`// Clone returns a private deep copy of the configuration: a published generation`
    - 做法：删除该行；把"私有深拷贝、发布代各自持有快照"这层意思并入 `config/clone.go` 的 `Clone` doc（现 doc 已含 round-trip/ConfigPath/omitempty 三点，补上第一句语义即可）
    - 完成：`go run ./scripts/comment_policy -v . examples/wechat-bot` 输出中 `org_hotreload.go` 零 finding
    - 边界：不许动 orgSubset 自身 doc 与类型体；不许为清 finding 调 `--update-baseline`

  - [x] 3.2b README 布局表登记 config/ 层（插在 `agent/` 行之前，合分层顺序；doc-refs 绿）
    - 对象：README.md 的模块表（`| \`agent/\` |` 行起，约 232 行）
    - 做法：在 `agent/` 行**之前**插入一行 `| \`config/\` | 配置模型层：编排声明的类型实体（Config/AgentConfig/ToolRef 族）与 LoadConfig/严格校验/生命周期投影；根包以别名再导出保持 tagent.* API 不变 |`——位置在 agent 之前是分层顺序（root → config → agent）
    - 完成：肉眼核对表行对齐；`bash scripts/lint.sh` 的 doc-refs 段仍绿
    - 边界：只加一行，不重排现有行

  - [x] 3.2c TestArch_LayeredDependencyDirection 预跑：PASS(0.73s) 无需最小修正——新 config 包不在既有枚举集合内故不触断言；C2 的 3.5 正式扩集
    - 对象：`guardrails_test.go:90` 的既有分层断言测试
    - 做法：`go test . -run '^TestArch_LayeredDependencyDirection$' -count=1 -v` 直接跑；若因新增 `config` 包而红，**只允许**把 config 加进该测试枚举的包集合（这正是 C2/3.5 的正式扩集的前置信号，先做最小修正让 C1 可提交），并把红的原因记进 3.5 的执行注记
    - 完成：该测试 PASS
    - 边界：不许删除断言、不许放宽既有方向规则；若红因与 config 无关 ⇒ 停，按熔断上报

  - [ ] 3.2d TestLiveSession 并发红定性 —— **A/B 后判为"证据不足"，转 6.1 家族任务**
    - 数据：C1 工作树下 `go test ./... -short -count=1` ×3 → 第2遍红 `TestLiveSessionStaysWatchedAcrossToolGeneration (32.19s)`，两次绿；单包跑 PASS(12.8s)
    - A/B：worktree 检出无 C1 的 `1478cb1` 同条件 ×3 → 该测试未红，但第3遍红在另一枚 `TestBuildFailure_UnconfirmedReclaimSealsWriter (0.01s)`
    - 判读：两枚都是全量并行下的间歇；家族先于 C1 存在。1/3 vs 0/3 的样本量既不能归因 C1 也不能排除，**不下结论**
    - 处置：不在 C1 内修（无判据支撑任何改动）；登记 6.1 采集足够样本后定性
    - 对象：`cross_generation_test.go` 的 `TestLiveSessionStaysWatchedAcrossToolGeneration`（曾于全量 `./...` 并发跑红一次 32.19s；单包复跑绿 12.8s）
    - 做法：连跑三次 `go test ./... -short -count=1`，记录该测试每次结果
    - 完成（二选一，各自闭环）：①三次全绿 ⇒ 在本任务后追加注记"不可复现，三连绿放行"并勾掉；②任一次复现 ⇒ **不在本变更修**，将复现命令与输出追加到 design D12，立新档处理（同族先例：fix-nested-hop-alert-attribution）
    - 边界：禁止用调大 waitFor 预算的方式"处理"；禁止跳过该测试

- [x] 3.3 C1 提交 `201da9c`（显式 pathspec；验证墙全绿：build/vet/config+根包 short 57.3s/-race 72.2s/GOMAXPROCS=1 57.2s/lint ok/gen_godoc 39 文件；全新检出 worktree 复核 build+根包测试通过）
  - 附带更正：前序 `643f56b`（标称 docs）因 `git mv` 立即入索引 + `git commit` 未带 pathspec 而夹带三处零内容重命名，致该 tip 全新检出不可编译；`201da9c` 已使其复健。防复发见 6.2
  - 做法：`gofmt -l .` 为空 → `go build ./...` → `go test ./config/ . -count=1 -short` → `go test . -count=1 -race -short` → `GOMAXPROCS=1 go test . -short -count=1` → `bash scripts/gen_godoc.sh && bash scripts/gen_godoc.sh --check` → `bash scripts/lint.sh` → `openspec validate root-structure-and-hygiene --strict`，全绿后 `git add` 全部 C1 路径（config/、config_alias.go、wiring.go、build_agent.go、org_hotreload.go、partition_collision.go、registry_test.go、scripts/codetools/hygiene.go、docs/api/）提交
  - 完成：提交落库，`git status --short` 为空；tasks 3.2 主项改 [x]
  - 边界：提交信息写明"行为零变更：模型外迁 + 别名层"；不许把 C2 的任何改动混进本提交

### C2 纯查询随迁 + 分层立法修订

- [x] 3.4 四查询迁 `config/queries.go` 并导出（`AgentMemoryFingerprint`/`ChangedMemoryAgents`/`ReachableAgents`/`RemoteDeclarationOnly`，doc 内 `ToolRef.isRemoteRef` 措辞同步为 `IsRemoteRef`）；`partition_collision.go` 收缩为注册表文件（连带去掉失去理由的 `var _ = agent.TagentAgent{}` 与 agent import、四个失效 import）；根侧 6+2 处调用点与 2 个测试文件限定化；**判据达成**：`grep` 旧名零命中、`go test ./config/ .` 绿
  - 对象：`partition_collision.go` 中四个以 `*Config`/`*AgentConfig` 为参的纯函数：`agentMemoryFingerprint`、`changedMemoryAgents`、`reachableAgents`、`remoteDeclarationOnly`
  - 做法：`git mv` 不可用（同文件拆分）——剪切四函数（连同各自 doc）入新文件 `config/queries.go`（文件头加 `契约: docs/wiki/platform/platform-subsystems.md#config-surface`），导出为首字母大写（`AgentMemoryFingerprint` 等）；根包调用点（`org_hotreload.go`、`org_candidate_overlay.go`、`build_agent.go` 中 grep 四个旧名可得全部位置）改为 `config.` 前缀；`partition_collision.go` 只剩注册表三方法与 `var _ = agent.TagentAgent{}`——若该哑变量失去存在理由一并清理
  - 完成：`go build ./...` + `go vet ./...` + `go test ./config/ . -count=1 -short` 全绿；`grep -rn "agentMemoryFingerprint\|changedMemoryAgents\|reachableAgents\|remoteDeclarationOnly" *.go` 零命中（根包无残留旧名）
  - 边界：函数体逻辑零改动（纯移动+改名）；`runtimeConfig` 的三个注册表方法不动（它们属 C3 的接口化，不属本任务）

- [x] 3.5 `TestArch_LayeredDependencyDirection` 加两条（config 不回指 root；agent 主体不依赖 config）+ wiki §三 新增「顶层模块层与归属」表（根=config 的消费者+发布权所在；agent/org 行为**预告**，C3 落地后生效）
  - 负路径自证的**任务假设被证伪并改法**：方向违规在 Go 里必成 import 循环 ⇒ 编译失败而非测试红，故无法用「临时加 import」构造违例。改证断言装置本身有效：把禁项临时改为 config 真实依赖的 `agent` ⇒ **FAIL(0.40s)**，恢复 ⇒ **PASS(1.02s)**，净差 7 行撤净
  - 附带学到并记入 D13：`free-standing` 是**函数体内一律不留注释**（第 8 次同类，一行说明也被拦），解释文字的家是声明位 doc/断言消息/wiki
  - 对象：`guardrails_test.go` 的 `TestArch_LayeredDependencyDirection`；`openspec/changes/root-structure-and-hygiene/specs/architecture-guardrails/spec.md`（MODIFIED 两条已写好，无需再改）；`docs/wiki/agent/agent-architecture.md` 的 `#package-layout` 表
  - 做法：断言测试按其既有模式加两条——①config 及其子包 MUST NOT import 根包；②`agent/`（不含子包）MUST NOT import config，`agent/org`（C3 产物，尚不存在时先留 TODO 注释）例外。wiki package-layout 表补 `config/` 行与根包"发布权所在"注记
  - 完成：`go test . -run TestArch_LayeredDependencyDirection -count=1 -v` PASS 且新断言真实生效（临时在 config/config.go 加 `"github.com/SpellingDragon/tagent"` import 应使测试红——验完撤掉，输出留注记）
  - 边界：方向规则只增不减；负路径验证的临时 import 必须撤净（`git diff config/` 为空）

- [x] 3.6 C2 提交（显式 pathspec；验证墙：31 包 short 全 ok、`-race` 72.5s、`GOMAXPROCS=1` 59.4s、gen_godoc --check 38 包匹配、lint ok、openspec strict valid）
  - 6.1 累计数据：完整 `./...` 并发下当前树 **2/4 红**（同一枚 32.17/32.19s），基线 `1478cb1` **0/3**；定向争用（`. ./tests ./agent` 三包并发）两侧 3+3 **全绿** ⇒ 复现需完整并发集，仍不下归因结论
  - 做法：与 3.3 同一面验证墙（含 `GOMAXPROCS=1` 与 `-race`），提交范围：`config/queries.go`、`partition_collision.go`、根包调用点、`guardrails_test.go`、wiki、docs/api
  - 完成：提交落库、工作树净
  - 边界：不混入 C3 改动

### C3 世代机制外迁 agent/org

- [x] 3.7 三件迁移 + 注入契约落地（**拆两步提交**，降单批风险）
  - [x] 步骤 1 · `owner_retirement.go` → `agent/org/retirement.go`：`Ledger`/`NewLedger`/`RetireDecision` + 11 方法导出；根侧 9 个调用点限定化，旧名零残留。前提被实测坐实：`owner_retirement_test.go`(1219 行) 对 ledger **零直接构造**（全黑盒经装配 API），故测试一行未动；导出化连带要求 doc 首词与标识符一致（11 处改名 + NewLedger 补 doc）
  - [x] 步骤 2 · 两件已入 `agent/org/candidate_txn.go` + `agent/org/overlay.go`：注入面按事实收窄——`rc.resident` 用同层真实类型 `*agent.ResidentTopology`（原设接口是多余的）；`SetFP/DropFP/UnregisterOwner` 三函数注入；`buildAgent` 经 `ShellBuilder` 闭包（buildMode 与 loader 封在根侧闭包，org 不见 prompt）。根侧唯一接缝 `runtimeConfig.orgDeps(loader)`；discard 探针导出为 `org.LastDiscardOrder()`（根测试唯一读取点）**因函数体内注释被门禁**，探针文档写明"无生产路径读取"
  - 判据达成：`grep -rn "runtimeConfig\|buildAgent(" agent/org/` 零命中（spec 新 scenario 的机器判据）；旧根文件已删、旧名零残留；`go build`/`go vet` 全清；config/agent/根包 short 全绿（2007 行候选拒绝套件经新包跑通）；分层断言（非 short）PASS；`-race` 72.7s；`GOMAXPROCS=1` 57.4s；lint ok
  - 对象与逐件处置：
    1. `owner_retirement.go` 整文件 → `agent/org/retirement.go`：`retirementLedger`→`Ledger`、`newRetirementLedger`→`NewLedger`、`retireDecision`→`RetireDecision`（其余方法名不变）；零根包依赖，预期只需改包名与文件头索引（`契约:` 指向 wiki 世代页）
    2. `org_candidate_txn.go` 整文件 → `agent/org/candidate_txn.go`：`candidateTxn`→`Txn`；其 `rc *runtimeConfig` 字段改为注入结构 `deps{ UnregisterStoreOwner func(name string) }`；`recordDiscardOrder`/`lastDiscardOrder` 随迁，根侧若有测试读它则经新导出名
    3. `org_candidate_overlay.go` 整文件 → `agent/org/overlay.go`：`buildCandidateOwners`→`BuildOwners`；`rc` 的三个用法逐一替换——`rc.resident`→注入的 resident 句柄（其类型即根包现有封装类型，经参数传入）、`rc.residentMemFP`→随族自管字段、`buildAgent(...)` 调用→注入的 `ShellBuilder func(name string, acfg config.AgentConfig, cfg config.Config, cache map[string]*agent.TagentAgent) (*agent.TagentAgent, error)`（根包闭包内固定 buildModeResident）
  - 做法：`mkdir agent/org` 后逐件 `git mv` + 改造；根包（`org_hotreload.go`、`tagent.go`、`build_agent.go`）调用点改为 `org.Xxx`；`org_hotreload.go` **整文件留根**（发布权物理位置，D9/D10 裁决）
  - 完成：`go build ./...`、`go vet ./...`、`go test ./agent/... . -count=1 -short` 全绿；`grep -rn "runtimeConfig" agent/org/` 零命中（机制不见装配态——这正是 spec 新 scenario 的机器判据）
  - 边界：`owner_retirement_test.go` 等**测试文件不动**（D3：装配级测试留根）；若改造中发现第四个根包依赖点 ⇒ 停，先回 design 补 D8' 再继续；`agent/org` 不得 import 根包（编译器与 3.5 断言双保险）

- [x] 3.8 C3 提交（显式 pathspec；验证墙另加 `go test ./agent/org/ -race`——包内暂无测试，随 4.x 补）
  - 做法：验证墙同 3.3，另加 `go test ./agent/org/ -count=1 -race`；`bash scripts/gen_godoc.sh` 重生成（新包进 docs/api）；提交
  - 完成：提交落库、工作树净、`docs/api` 含 agent/org 页
  - 边界：不混入 P4

## 4. P4 巨型测试按域拆分

- [x] 4.1 按域锚切出可独立成域的块（三文件全完成，实测账见下）（recipe 依 D14 修正：同锚多文件必须各有精确生产镜像，故一域一文件、域锚全局唯一）
  - 做法（org_hotreload_test.go 优先，其余两文件按 D14 表执行）：
    1. 新文件 `org_hotreload_fingerprint_test.go`：承载 8 个指纹域 test（`TestOrgFingerprint_*` 4 枚 + `TestMemoryFingerprint_*` + `TestModelRefAliasesFoldToStableFingerprint` + `TestFingerprintFold*` 2 枚）与专属 helper（`cfgFor`、`ownerYAMLWithModel`、`writeBumped`、`aliasYAML`、`writeCfg`），文件头**首行**即 `// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint`
    2. 新文件 `org_hotreload_timing_test.go`：`#trigger-timing` 域 4 枚 test + `buildPark`/`newBuildPark`/`waitEntered`/`letGo`/`disarm`/`acquireWithin`/`genOf` helper，头锚 `#trigger-timing`
    3. 无 `Test*` 的共享 fixture（`e2eYAML`/`hotYAML*`/`prodShapeYAML`/`dropAgentYAML`/`scHot*`/`snapshotRotationYAML`/`seSpawnerTTLYAML`/`LoadConfigForTest`/`residentCacheForTest`/`keepRecentOf`/`seActionToolOf`/`_` 与 3 个 Benchmark）移入 `org_hotreload_fixtures_test.go`（零 Test 函数 ⇒ 不参与同锚计数）
    4. 其余测试按原锚留在 `org_hotreload_test.go`（同锚的 apply-record/candidate-refusal 拆不开，见 D14）
  - 完成：`go test . -list ".*" -count=1 | wc -l` 前后**相等**（测试与 Benchmark 一个不少）；`go test . -count=1 -short` 绿；`bash scripts/lint.sh` 零 finding（含同位门）；主文件行数下降至 ~1100–1200
  - 边界：整函数搬运，**禁止**改动任何断言/预算/谓词/helper 实现（改动即越界，需 fail-before 另案）；新文件首个声明之前只允许 包注释+锚+`package`，避免更早的索引行被门的"首条锚"规则误读；不引入镜像改命名制（D14 裁决项）
  - 其余两文件：`org_candidate_test.go`、`cross_generation_test.go` 依 D14 表只做不冲突锚的切分（`#published-wrapper-immutable`、`#lease-holds-reference`、`#hot-source-pull-authority`、`#close-drain`、`#identical-apply`、`#config-clone`），同锚块留原位
  - **org_hotreload 族已完成**（实测）：1716 → 主 883 + timing 180 + lockfree 112 + closed 62 + support 538；`#fingerprint` 域**放弃外迁**（该锚被无镜像的 org_candidate_test 占用，拆即撞门——被门挡住的拆分不是好拆分）；判据三条全中：`-list` 211→211 守恒、根包 short 57.3s 绿、lint ok 含同位门零 finding；`check_test_merge.sh HEAD .` ⇒ **1 package(s) intact**

- [ ] 4.2 无损证明 + 同位门核对（前置：4.1）
  - 做法：对三个源文件分别跑 `bash scripts/check_test_merge.sh <拆分前基线ref> <源文件路径>`（基线 ref 用 C3 的提交号）；`bash scripts/lint.sh` 确认 responsibility-fragmentation 零 finding
  - 完成：三份无损证明输出留痕到 change 目录 `p4-merge-proof.log`
  - 边界：证明失败 ⇒ 回 4.1 修，不许调门

- [ ] 4.3 P4 提交（前置：4.2）
  - 做法：验证墙同 3.3 另加 `GOMAXPROCS=1 go test . -short -count=1`；提交
  - 完成：提交落库、工作树净

## 5. 收口

- [x] 5.1 文档面同步：wiki `#package-layout` 的 agent/org 行由预告改为既成（列出 Ledger/Txn/Overlay/BuildOwners/ShellBuilder/Deps 与唯一接缝 orgDeps）；工具页新增 §七「测试文件的域锚切分」（三条事实 + “被门挡住的拆分不是好拆分” + 无损判据）与 §八「提交范围一致性门」；`grep -rn "train/" README.md docs/wiki` 零活引用（P2 时已清）
  - 对象：README 模块表（若 C2/C3 后有出入）、`docs/wiki/agent/agent-architecture.md#package-layout` 五组职责表（补 config 与 agent/org 两行、根包行改为"装配+发布权"）、`docs/wiki/README.md` 索引
  - 完成：`bash scripts/lint.sh`（doc-refs/gen_godoc --check）绿；`grep -rn "train/" README.md docs/wiki` 零活引用
  - 边界：只补布局事实，不重写文档

- [ ] 5.2 终验与归档（前置：5.1）
  - 做法：`git push` 后盯 CI 至四 job 绿（test/race/validators/openspec）；`openspec archive root-structure-and-hygiene -y`；`openspec validate --specs --strict`
  - 完成：归档目录出现 `2026-10-XX-root-structure-and-hygiene`，spec 合并计数 +2 ADDED +2 MODIFIED
  - 边界：CI 任一 job 红 ⇒ 停在归档前，先诊断（禁止带着红归档）

## 6. 执行期新增（本变更内发现，未在原计划）

- [x] 6.1 负载敏感测试族定性并结案（根因见 D15）
  - 定性：**测试编排缺陷**（跨包共享可写状态），非环境敏感、非产品缺陷。判据是失败时长稳定 32.1x s = 30s 轮询常量 + boot，饥饿会抖动
  - 修复：`tool/action` 10 处测试私有目录化 + 删除整目录 `RemoveAll`；静态不变量 `TestResidentMetaDirHygiene`；wiki `#restart-takeover` 补共享状态判据
  - 前后测量：完整 `go test ./... -short -count=1` 修复前 **3/5 红**（同一枚）、修复后 **5/5 绿**；`tool/action` 与根包单跑持续绿
  - 对象：`TestLiveSessionStaysWatchedAcrossToolGeneration`（全量并行 32.19s 超时 / 单包 12.8s 通过）与 `TestBuildFailure_UnconfirmedReclaimSealsWriter`（0.01s 断言红，见于无 C1 基线）
  - 做法：各以 `go test ./... -short -count=1` 累计 ≥10 轮采集出现率与首次失败包；对超时那枚定位其等待的谓词与并发争用点（同法：worktree 基线对照 + 变异/锚定检验）
  - 完成：每枚给出"产品缺陷 / 测试归因 / 环境敏感"三选一定性与证据，按结论决定修或另立案
  - 边界：禁止用调大预算或加跳过让 CI 变绿；CI 亦跑 `./... -short`（2 核），故此族是真实 CI 风险，不是本地噪声

- [x] 6.2 提交范围一致性门 `codetools commit-scope`（已接入 lint.sh）
  - 判据：标题 `docs(`/`docs:` 的提交遇代码路径（`*.go/.sh/.yml/.yaml/.py`，`docs/`、`openspec/`、`*.md` 除外）即具名拒绝
  - **真实历史双验**：`commit-scope 643f56b`（我那个标称 docs 实带半套迁移的提交）⇒ 抓出 3 条代码路径 exit 1；`commit-scope d52a771`（诚实的 docs 提交）⇒ exit 0
  - 门自身立刻反噬我一次：新写的单测夹具里用了 `openspec/changes/x/tasks.md`，被 `proc-refs` 当场判 PROCESS-ARTIFACT-CITE ⇒ 换夹具路径。工具的规矩对写工具的人同样生效
  - 对象：本变更暴露的失误形态——标称 `docs(openspec)` 的提交夹带代码重命名（`git mv` 即时入索引 + `git commit` 无 pathspec）
  - 做法：加一条机械检查（`codetools` 子命令或 git 钩子二选一，倾向前者以复用 CI 同命令）：当提交信息前缀为 `docs(` 且 diff 含 `*.go`/`*.sh`/`ci.yml` 变更时非零退出，负路径探针留痕
  - 完成：探针双红 + 正常 docs 提交不误红；接入 `scripts/lint.sh` 或 validators job
  - 边界：不许把该门做成"要求所有提交带 pathspec"这类无法机械判定的形式
