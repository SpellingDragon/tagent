# Design

## D1 拆包方向由既有立法锁定：接口注入，不是反向依赖

`architecture-guardrails`「分层依赖方向可机械断言」固化了 `agent 及其子包 MUST NOT import 根包`。世代族移入 `agent/org` 后需要的恰是根包的 `runtimeConfig`（候选事务持有 `rc *runtimeConfig`，7 处）与 `buildAgent`（1 处）。因此：

- `agent/org` 定义自己的窄契约：`ShellRuntime`（候选事务真正读写的字段子集快照）与 `ShellBuilder`（重建执行壳的回调签名）；
- 根包在装配时构造并**注入**，方向保持 root → agent；
- 这正是「唯一编排发布权与中性契约」条文的精神在物理布局上的延续：机制在 agent 域，构造权在组合根。

被否方案：org 包 import 根包取 runtimeConfig（直接违反机械断言，测试即红）；或把 buildAgent 一并挪进 agent/org（装配权下放，违反组合根独占发布的既有裁决）。

## D2 公共 API 面不变：别名 + 薄委托

盘点：`Org*` 导出类型（OrgStatus/OrgFailure/OrgLiveDebt/OrgCloseState/OrgAgentApply）在 tests/examples/tool/scripts **零引用**；`CheckOrgReload`/`Rollback` 等是 `TagentAgent` 方法，挂根包类型上天然不动。移动后根包保留 `type OrgStatus = org.Status` 形式的**类型别名**与方法薄委托——预发布期本可不兼容，但别名成本为零且免去下游（若有隐性引用）迁移。

## D3 世代集成测试留根包，纯逻辑单测随包走

三个巨型测试文件的主体是装配层集成测试：直接使用 `New/LoadConfig/StartLoop/InjectMessageContext/residentCacheForTest` 等根包未导出面，移出根包即失去被测通路。故处置是**按域拆文件**（org-candidate / org-hotreload / cross-generation / owner-retirement / partition-collision 各自成档，≤600 行/文件），留根包；candidateTxn 的纯事务逻辑、partition 碰撞的纯分配逻辑若有独立单测价值则随 `agent/org`。同位门的分组键含目录与锚点，拆分后逐一核对锚指向与生产镜像归属。

## D4 卫生门的形状（防的是"下一次"，不是这一次）

validators job 新增检查：① `git ls-files` 全集必须非空文件；② 追踪路径不得命中运行期产物模式（`*.lock`、`*.journal`、`*.tmp`、`*.prof`、`.tagent-writer.lock`）；③ 顶层目录白名单外的**新增目录**须在 README 布局表登记（白名单初始 = 现有 tracked 顶层集合，防漂移）。带负路径探针：临时制造空文件与 `x.lock` 入 index 必须红，撤除恢复绿，留痕。

## D5 阶段顺序与每步可回滚

P1 卫生（无行为）→ P2 train/ 挪移（纯路径）→ P3 拆包（接口化，最大的一步）→ P4 测试拆分（文本重组）。每阶段独立提交、独立全绿（`go build ./...`、根包与 agent 包 plain+race、`GOMAXPROCS=1` 根包、lint、`gen_godoc --check` 重生成后匹配）。P3 若发现 runtimeConfig 的字段子集难以快照化（候选事务写回运行态），停下上报而非放宽分层断言——那是设计层冲突，按规矩裁决。

## D6 实现期证伪：train/ 不是"误导性目录"，是一条陈旧训练桥（用户裁决：最彻底路线）

原 P2 前提（"纯路径移动 + 引用同步"）被三层证据推翻：① `train_tagent.py:34` 的 `sys.path` 把点号模块名当目录段（`/ "train.rl"`），指向不存在的路径；② 该脚本与 `train_rl_config.yaml` 的 `from train.rl import PPOTrainer`、`python3 -m train.rl.infra.rpc.rpc_server`、`workflow: train.rl.tagent_adapter.*` 全部指向 AReaL **改名前**的顶层包 `train`（现行为 `areal/`，`PPOTrainer` 在 `areal/trainer/rl_trainer.py`）；③ `run.sh` areal 命令族的默认配置 `areal_config.yaml` 不存在，CI 零 Python 覆盖。

**裁决（退役而非修补）**：三者无任何 openspec 规格承诺（`trajectory-recording` 只点名离线转换器；`rl-feedback`/`example-rl-visibility` 只承诺 Go 侧 HTTP 面），修补需要 AReaL 多卡环境才能验证，属"看着对"的提交。退役边界：

- 删除：`train/rl/tagent_adapter.py`、`examples/wechat-bot/train_tagent.py`、`examples/wechat-bot/train_rl_config.yaml`、`run.sh` 的 areal 命令族与 AREAL_* 变量、README 相关行；
- 保留并迁移：`convert_trajectories.py`（纯 stdlib、离线、被 `trajectory-recording` 点名）→ `scripts/convert_trajectories.py`，wiki 同步；
- 保留：`rl/` Go 包（轨迹记录、可换模型、HTTP API）、`tagent.rl.yaml` 运行时配置、bot README 的 RL 运行时环境变量表——它们是活面且各有规格；
- wiki `rl-architecture` 记录退役事实与重接条件（按现行 `areal.*` 布局重写，经 dotted-refs 门）。

## D7 dotted-refs 门的最小形状

只查**机器会真去 import 的位置**：shell 中 `python -m`/`python3 -m` 的后续 token、yaml 中 `workflow:` 的值。解析规则：点号换斜杠后在仓库内存在对应 `.py` 即通过；否则必须命中显式外部允许表（初值为空）。不做全文件点号 token 扫描——`scheduler.type=local` 这类配置覆盖语法会被误伤，门的强度来自位置精确而非范围贪大。

## D8 P3 前置盘点结论（3.1 落档）：三个依赖点，全部可切

对五文件与根包的耦合逐点核实（非猜测，逐行）：

1. **store-owner 注册表自包含**：`storeOwners/storeOwnersMu` 字段只在 `partition_collision.go` 内部出现，三个方法（register/unRegister/ownedAgentNames）的全部外呼只有 `build_agent.go` 两处（构建期注册、`SetStoreOwnerRevoker` 闭包）与 `org_candidate_txn.go` 一处——可作为独立类型整体移入 `agent/org`，根包持引用并调用（方向 root→agent 合规）。
2. **resident 缓存已是封装类型**：`rc.resident` 的使用全部经其自身 API（Snapshot/Get/Add/Unpublish），overlay 不碰内部；`residentMemFP` 的唯一写者就是 overlay 自己，属组织族自管状态，随族迁移。
3. **buildAgent 可闭包化**：`org_candidate_overlay.go:66` 把 rc 整个传给 buildAgent——改为根包构造 `ShellBuilder` 闭包（捕获 rc+loader），`agent/org` 只见回调签名。

据此 `agent/org` 的注入契约定格为三件：`ShellBuilder`（重建执行壳）、resident 缓存句柄、store-owner 注册表。`runtimeConfig` 本体不动、不留反向 import；D5 的熔断条件（写回运行态无法快照化）**未触发**——`residentMemFP` 是可随族迁移的族内状态，不是装配运行态。

## D9 熔断触发（3.2 第一步）：原 P3 计划与既有立法「组合根独占编排发布权」相撞

逐行核对五文件对根包配置模型的依赖后发现，切割可行性差异极大：

| 文件 | 对 `Config`/`AgentConfig` 的依赖 | 对 runtimeConfig/buildAgent 的依赖 | 可否今日迁出根包 |
|---|---|---|---|
| `owner_retirement.go` | **0** | **0** | **可**（纯机制：ledger + 注入回调） |
| `org_candidate_txn.go` | 0 | 3 | 可（D8 的三件注入即可） |
| `org_candidate_overlay.go` | 1 | 4 | 勉强（含 buildModeResident 语义） |
| `org_hotreload.go` | **12** | 1 | 不可（整个类型在 Config 模型上） |
| `partition_collision.go` | 混合（fingerprint/changed/reachable/remote 全以 `*Config` 为入参） | 注册表方法挂在 runtimeConfig | 一半可 |

真正的拦路不是耦合量，而是**立法冲突**：`architecture-guardrails` 的「唯一编排发布权与中性契约」明文规定——组合根 SHALL 独占编排执行绑定的构造与发布，内部包 MUST NOT 依赖根包或编排内部状态。`org_hotreload.go` 的 `orgCoordinator`（swap/publish/generation）**就是那个发布权本身**：把它移进 `agent/org` 等于把组合根的特权下放给内部包，与既有裁决直接相反；而它一旦留在根包，其类型就必须能引用根包 `Config`。

因此按 D5 停下，不自作主张改立法。三条出路（详见会话记录）：

1. **保守切**：只把 `owner_retirement.go`（零依赖）与 `org_candidate_txn.go`（三件注入）移出，`org_hotreload.go`+overlay+collision 留根包并在 wiki 标注"物理上即发布权所在"。收益：根包 −334 行；语义：机制与特权分离，合法。
2. **先抽配置模型再切**：`config.go`(+builtin/registry) 移入新包 `config`，根包保留 `type Config = config.Config` 等别名与 `LoadConfig` 包装（源码级兼容）。之后 Config 依赖不再是留守理由，`org_hotreload` 的**机制部分**（代数、指纹、可达集）可迁 `agent/org`，只留发布动作在根。收益最大，改动面与验证成本也最大。
3. **判定现布局合法**：接受"根包=装配+发布+世代机制"，只做文件分组与职责索引（已由 `契约:` 行完成），不再移动代码。收益最小但零风险。

## D10 终版切割设计（用户裁决：不起新档，一步到位）

D9 的出路 2 并入本变更，与 D8 三件注入合并为终版。层与归属一次定死：

**新层 `config`（配置模型）**：`config.go` 的类型模型 + `LoadConfig` 迁入。它**不是叶子**——盘点实据：import 了 agent/prompt/tool 八件/workspace/internal/strictyaml，因此它坐在 root 与 agent/tool **之间**：`root → config → {agent, prompt, tool/*, workspace}`。`builtin.go`/`registry.go` 是装配动作（把工具注册进模型槽位），**留根**。`partition_collision.go` 的纯查询（`agentMemoryFingerprint/changedMemoryAgents/reachableAgents/remoteDeclarationOnly`）是配置模型的查询面，**随迁 config 包**——由此 `agent/org` 对 config 的需求只剩 overlay 的一处类型引用。

**`agent/org`（世代机制）**：迁 `owner_retirement.go`（零依赖）、`org_candidate_txn.go`、`org_candidate_overlay.go`；注册表改为注入接口（`StoreOwnerRegistry`，实现留在 runtimeConfig——注册发生在构建期属装配，注销被 txn 调用属机制，接口两边都够用）；`buildAgent` 经 `ShellBuilder` 闭包注入（buildMode 语义封进闭包，org 不见装配模式）。导出必要性：跨包引用即必要性，Ledger/CandidateTxn/Overlay 为包内 API，不进根公共面（根只留编排调用）。

**根包（组合根）终形**：装配（tagent/build_agent/wiring/builtin/registry/resources/modelref/prompts）+ **发布权**（org_hotreload 的 orgCoordinator 整文件留根——D9 已论证这是立法要求的物理位置）+ Org* 公共类型原位不动（无需别名）。根包生产代码预计 18.2k → 约 15.5k 行，三职责中的两职（配置模型、世代机制）物理出根。

**分层条文修订口径**：方向集由「root → agent → plugin → memory；event 叶子」扩为「root → config → {agent, tool/*, prompt, workspace}；root → agent → plugin → memory；event 叶子」；新增断言：config MUST NOT import 根包；agent 主体不 import config（仅其 org 子包按需）。机械断言测试 `TestArch_LayeredDependencyDirection` 同步扩集。「唯一编排发布权」条文补一句澄清：世代**机制**在 agent 域子包，**发布动作**与 orgCoordinator 留组合根，经注入契约协作——立法与实践互证。

## D11 执行序与检查点（每步独立绿）

1. **C1 配置模型外迁**：`git mv config.go config/`；包内 import 修正；根包建 `config_alias.go` 放全部别名/包装（类型用 `type X = config.X`，函数用薄包装，变量用 `var X = config.X`）；根内引用经别名同名继续工作；`config` 包补文件职责索引行。
2. **C2 纯查询随迁**：partition_collision 的四个查询函数入 config 包（导出，含单测随迁如有）；`guardrails_test.go` 分层断言扩 config；spec delta MODIFIED 落地。
3. **C3 org 机制外迁**：三文件 `git mv` 入 `agent/org`；`StoreOwnerRegistry` 接口 + `ShellBuilder` 闭包接线；根包调用点改造；`docs/api` 重生成。
4. **C4 验证墙**：`go build ./...`、根包+agent+config plain/race、`GOMAXPROCS=1` 根包、bot 模块、lint 全家（两道新门+doc-refs+gen_godoc --check）、openspec strict；每步一提交。

## D12 C1 执行实录（进行中，2026-10-04；本轮暂停于"只差机械收尾"）

**已完成（工作树持有，未提交）**：

- `config/` 包成形：`config.go`（模型+LoadConfig）、`modelref.go`（整文件随迁）、`clone.go`（`Clone` 从 org_hotreload 摘出）、`lifecycle.go`（`ResolveLifecycleConfig` 从 wiring 摘出并导出）、`config_test.go`（随包迁移）。
- 根包 `config_alias.go`：20 个类型别名 + 2 常量 + 2 函数包装，公共 API 源码级不变；`IsRemoteRef` 由未导出方法导出化（根侧 2 处调用点同步）。
- `config_test.go` 里 4 个注册表系测试（DefaultConfigBuildable/ToolRegistry×2/RegisterBuiltinTools_Idempotent）并回根侧 `registry_test.go`——它们测装配动作而非模型，colocation 门的判定与设计一致。
- 验证已过：`go build ./...`、`go vet`、`go test ./config/`、**根包全量 short 57.9s 绿**、`gen_godoc` 重生成（39 文件）、hygiene 门（config 已登记白名单）。

**计划外的发现（D10 未预见，逐条入档）**：

1. `Config` 的方法散布三处而非一处：`modelref.go` 整文件、`org_hotreload.go` 的 `Clone`、`wiring.go` 的 `resolveLifecycleConfig`——Go 不允许跨包给别名定义方法，三处都必须随模型迁移。教训：**外迁一个类型前先 grep 它的全部方法定义点**，文件名不等于类型边界。
2. 别名层不是"零文档"层：comment_policy 对每个再导出符号都要求 doc（24 条 missing-symbol-doc 教训），别名也要逐符号写明"别名：实体在 config 包"。
3. 测试随包迁移有连锁账单：fixture 相对路径（`examples/…` → `../examples/…`）、文件头职责索引重立、原文件的孤儿 doc 注释要跟着测试走（我摘函数体时把 doc 留在原地，制造了 free-standing）。

**剩余收尾清单（恢复执行时从这继续）**：

1. 清最后 1 条 finding：`org_hotreload.go:444` orgSubset 上方还挂着 Clone doc 的残行（第二次犯同类错——摘方法时 doc 没跟走），把残行并入 `config/clone.go` 的 Clone doc。
2. `TestLiveSessionStaysWatchedAcrossToolGeneration` 在全量 `./...` 并发跑时红过一次（32.19s），单包复跑绿（12.8s）——**未定性**。C1 提交前必须全量复跑定性：若可复现则按既有家族（调度敏感）立案，不可复现则三连绿后放行并留痕。
3. `guardrails_test.go` 的 `TestArch_LayeredDependencyDirection` 扩集（config 层 + agent 主体不 import config）——属 C2（task 3.5），但 C1 提交时该测试现状仍是旧方向集，需确认其不会因新包出现而误红（预跑一次）。
4. README 布局表补 `config/` 一行（与白名单登记同批的纪律，白名单已登记、README 未写）。
5. 全量 short + lint + `GOMAXPROCS=1` 根包 + openspec strict 全绿后，C1 提交（task 3.2/3.3 落勾）。

## D13 两次自伤的机制性根因（不是"下次注意"，是流程缺陷）

1. **`git mv` 即时入索引 + `git commit` 无 pathspec** ⇒ 一个标称 docs 的提交夹带了三处零内容重命名，tip 全新检出不可编译，且我未查 CI 就推送。根因不是疏忽，是**"文档提交与代码提交共用同一索引"**这一流程形状：`git mv` 用于保历史，但它绕过了"只加本次范围"的约束。对策即 6.2 的门；另一条纪律同时生效：**凡改变代码树形状的提交，push 后必须查 CI 到绿**（此前我对 P1/P2 也漏了这步）。
2. **doc 注释手术三连错**（续行漏 `// ` 前缀 → em dash 被当代码解析；doc 与函数间多空行 → free-standing）。这是本会话第 6、7 次同类，且**每一次都被 comment_policy 硬零门当场拦下**——门的价值恰在于此：我没有一次靠运气通过。

判据澄清（第 8 次同类后被门教清）：`free-standing` 不是"注释别写长"，而是**函数体内一律不留注释**——连一行说明也不行。理由的家只有三处：声明位 doc、断言消息、docs/wiki。据此后续批次的写法固定为：断言行自解释（`assertNoDeps(pkg, deps, "config")` 已足够达意），解释文字进 wiki。

## D14 P4 的硬约束（原目标文件名不成立，按此重定 recipe）

读门实现得三条事实，它们共同否决了 4.1 原先拟的名字（core/rollback/gates、txn/overlay/store、publish/reentry/delegation）：

1. **锚的取法**：`responsibilityAnchor` 取文件里**第一条** `契约:`/`规格:` 索引行 ⇒ 文件级头注释在前，则整个文件的域锚就是它，与内部各测试的锚无关。
2. **同锚即须有镜像**：同 (dir,pkg,tag,锚) 出现 ≥2 个测试文件时，凡**没有**同名 `<stem>.go` 生产文件者判碎片（镜像比对是精确拼写，仅 `_real` 后缀被豁免）。`org_hotreload_rollback_test.go` 之类没有镜像，**拆分本身制造违规**。
3. **纯 helper 文件可豁免**：无任何顶层 `Test*` 的文件不参与计数 ⇒ 共享 fixture 可另立无测试文件（其域锚与主文件同锚也安全）。

于是只有两条合法形状：**(i) 一个域一个文件**（域锚全局唯一）；**(ii) 子文件按 `<生产文件 stem>_test.go` 精确镜像命名**——而根包的测试从来不是镜像命名制（`partition_collision_test.go` 有镜像是巧合，`org_hotreload_test.go` 覆盖 org_hotreload+owner_retirement+tagent 三文件），强行转制会把 P4 变成改名工程，故弃。

**按 (i) 的真实账**（逐声明盘点 + 现有域锚占用：`#owner-retirement`→org_hotreload_test、`#turn-local-execution-face`→cross_generation_test、`#reentry-resolution`→org_candidate_test）：

| 源文件 | 可分出的新域 | 受镜像规则约束的部分 |
|---|---|---|
| `org_hotreload_test.go`(1716) | `#fingerprint`(8 test/164行)、`#trigger-timing`(4/171)、`#lockfree-read`(2/94)、`#closed-owner-refusal`(1/61)、`#memory-preflight`(1/57)、`#generations`(1/38) ⇒ 主文件余 ~1131 行（e2e + `#apply-record` + `#candidate-refusal` 三块） | 同锚的 apply-record×5、candidate-refusal×2 拆不开（org_hotreload.go 镜像已被主文件占用） |
| `org_candidate_test.go`(2011) | 现占 `#reentry-resolution`；`#rollback`(4/191) 可迁往 owner-retirement 缺位者或**改占新锚** | 主文件已占三锚，其余同锚测试留主文件 |
| `cross_generation_test.go`(1970) | 现占 `#turn-local-execution-face`；`#published-wrapper-immutable`(2/59)、`#lease-holds-reference`(1/57)、`#hot-source-pull-authority`(1/32) 可成独立文件 | `#reentry-resolution` 的 10 个测试与 org_candidate 同锚冲突 ⇒ 只能留在一处（**收敛锚**是更正确的动作，属 D14 例外） |

**净判断**：P4 按合法形状只能做到 ~4 次安全拆分（org_hotreload 的 2 个域文件），其余受同锚测试与镜像规则牵制；把三个巨型文件降到 ≤600 行的目标**不可达**。因此 4.1 从"拆到 ≤600 行"改为"按域锚切出可独立成域的块，目标 1700 → 1100~1200 行"，并把"根包测试是否转镜像命名制"作为独立裁决项交你定（那是更大的一致性工程，不该塞进大扫除尾巴）。

## D15 6.1 结案：跨包共享可写状态造成的间歇红（不是环境敏感，也不是产品缺陷）

判据由**失败时长**给出：稳定 32.15–32.20s = 一个 30s 常量轮询预算耗尽 + 子进程 boot 1–2s。CPU 饥饿会抖，只有"被等待的对象根本没出现"才给出这么稳的数。

链路：`tool/action` 的 6 枚单元测试用裸零值 `ActionTool` ⇒ `metaDir()` 回落到**进程默认** `$TMPDIR/tagent-resident-meta`；其中 5 枚还 `defer os.RemoveAll(ct.metaDir())` **整目录清扫**。而根包那枚换代接管测试的三个子进程通过 `os.Environ()` 继承同一个 `TMPDIR`，spawn 子进程写入的 meta 文件被邻包扫掉后，restart 侧 `ReattachResidentSessions` 对读不到的 meta **静默 continue**（契约允许的 best-effort）⇒ 会话存活但失监视 ⇒ TaskID 桥不提升 ⇒ 任务停在 suspect 直到预算耗尽。

分类：测试编排缺陷。产品侧两条契约（默认 /tmp 可配、重挂 best-effort）都被正确执行；同文件另有 3 枚测试早已用 `WithResidentMetaDir(t.TempDir())`，不一致本身即证据。CI 常年绿只是 `-p 2` 下两个包的窗口几乎不重叠。

处置：10 处测试全部私有目录化并删除整目录 RemoveAll；新增静态不变量 `TestResidentMetaDirHygiene`（禁裸零值赋值、禁 RemoveAll meta 目录）；wiki `#restart-takeover` 补"该目录是跨进程共享可写状态"的判据。前后测量：完整 `./... -short` 由 **3/5 红 → 5/5 绿**。

写守卫时自己踩了两次：守卫文本命中自身文件的文档注释与判断语句（改成只匹配赋值形式 + 跳过自己），以及测试 doc 必须"一行意图 + 要点列表"（长理由归 wiki）。

