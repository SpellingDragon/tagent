# 13 验证完成 · 设计

> 静态决策见一级 [design.md](../../design.md) D8 **G4 证据放行**、D9 第 5 条、D10「证据确实挡放行」行。本轮仅改本域 tasks.md/design.md，不实现检查器、不跑测试、不动 CI/权限。
> **当前缺口与执行承接见 §十二。** 下文旧审计/运行报告保留原文，旧“源树已覆盖”“唯一剩余 N6”不作当前结论。历史 R/G 及 ORCHESTRATION 引文统一指 [ORCHESTRATION-HISTORY.md](../../ORCHESTRATION-HISTORY.md)；本轮仅静态核对指定源码，不将历史命令输出冒充本轮实测。

## 一、既有引擎的能力与差距

| 引擎 | 已经做的 | 缺的（本域要补的判据） |
|---|---|---|
| `scripts/gates/check_runtime_evidence.py` | 无 `--evidence` 即失败（strict）；对照真实 `go test -json` 记录核 run/终态、包终局窗口、子测试窗口；重复 run、skip、包级 fail、后补 pass 覆盖先失败均判失败；`--scope` 只验子集并报 `remaining_required`；`--bundle` 要求 short 恰好十份、long 六组合、mutations 恰一份、基线源树摘要一致 | 源树指纹与合同摘要是**执行侧自报**（工具自述「不声称它是防伪、真实或来源已认证」）；`status=done` 只要求 `run_id/commands/exit_codes/artifact_paths` 四字段存在，**不核量名与单位与区间**；不核「期望集合 = 合同登记集合」以外的规格完整性 |
| `scripts/scenarios/run_runtime_scenarios.py` | 每次派生命令固定 `-count=1`、每轮独立日志与 UUID run-id、写 `contract_digest` 与 `module_fingerprints`（受测模块 `content_hash`+`revision`）、`liveness` 记运行时限 | 只覆盖 `event_integrity_scenarios.json` 声明的测试面；不判断这些测试是否就是该判据的全部承载 |
| `scripts/gates/check_event_integrity_mutations.py` | 隔离副本树、控制树先绿、仅「进程非零且输出命中登记断言」记 killed；`classify` 已判 timeout／compile_failure／zero_match／no_test_run／test_green／panic_without_assertion／assertion_not_matched；`apply_patch` 锚命中数≠1 即拒（`file:line` 见 tasks） | ①**控制树跳过**：`_ran_test` 只要求存在一条 `run` 事件，测试 `skip` 也满足，控制树照样判绿；②**目标字样+无关失败**：`_assertion_hit` 按 `Test` 名归属，同一测试内另一条断言失败、或测试自身日志打印了该字样，即可冒充命中；③**变异未进入被测编译面**：补丁锚命中即应用，不核该文件是否属于该选择器实际编译的包/构建标签/模块 |
| `scripts/gates/check_dod_coverage.py` | 判据 ↔ 测量/锚/变异绑定结构；criteria 与 design 文本双向对齐；`ci_gate` 须是工作流作业之一 | 只核**作业名存在**（今 P4 绑在 `event-integrity-long`，而锚在 `assembly` 包由 `test` 作业跑）；变异判词读登记表 `mutation_verdicts`（**手填**），不读运行产物 |
| `openspec archive` | 归档时同步主规格并校验 | **不核完成判据**（`--help` 无相关选项，`-y` 只跳过确认） |
| 分支保护（外部） | — | `required_status_checks` 为 `ABSENT`：合并侧目前没有必需检查，属外部权限 |

结论：链路上缺的不是「再多一个检查器」，而是**把已有的执行产物绑成一份可拒绝的证据，并接到三处拒绝面**。

### G4 判决（13.R1 只读审计，2026-11-16）

**判决：FAIL（不通过）。** 既有三引擎能拒一部分假绿（空证据／空 bundle／零测试／evidence 模式下的 skip），但**不能可靠拒绝假绿**：三条假 killed 判据缺失（已动态复现）、变异判词手填、合并／归档拒绝面未生效。G4 不通过 ⇒ 13.R2／13.R3 仍阻（ORCHESTRATION §② 等待表）。本审计不改任何引擎，只产出判决与差距表。

**源树绑定（ORCHESTRATION §②，强制）**：本审计所有引擎运行量的是**工作树**（HEAD `9547faca` 之上的未提交候选），非 HEAD。三处假 killed 函数（`_ran_test`／`_assertion_hit`／`apply_patch`／`classify_control`）经 `git diff HEAD` 核**与 HEAD 逐字一致**，故 hole 判决对 HEAD 与工作树同成立；`check_dod_coverage.py`＋其 `lint.sh` 接线＋`dod_predicates.json` 为**工作树独有**（untracked／未提交），HEAD 无此门。CI 原始记录须绑 HEAD，不得以本工作树量解释 CI 红。

**正／负锚原始退出码与实测计数（非空转证明）**：

| 锚 | 命令 | EXIT | 实测 |
|---|---|---|---|
| 正1 | `check_runtime_evidence.py`（无 `--evidence`） | 2 | strict 点名「no evidence supplied」 |
| 正2 | `test_check_runtime_evidence.py` | 0 | Ran 97 tests |
| 正3 | `test_check_event_integrity_mutations.py` | 0 | Ran 15 tests |
| 正4 | `test_run_runtime_scenarios.py` | 0 | Ran 32 tests |
| 负1 | `--evidence <空目录>` | 1 | 点名「--test-log must both be supplied」 |
| 负2 | `--bundle <空目录>`（CI 真实面） | 1 | 点名「holds no evidence.json」「exactly 10 … found 0」「missing event-long …」 |
| 附 | `check_dod_coverage.py` | 1 | 19 gap（判词手填，见 G4-4） |
| 附 | `test_check_dod_coverage.py` | 0 | 13/13 cases |

自测仅证明已列反例被拒，**非产品验收、不覆盖规格全集**（本域 `specs/architecture-guardrails/spec.md` 的「检查资产区分三类证据」条款）；不得以自测绿冒充 G4 通过。

**差距表（每条带源码实证与承接叶）**：

| # | 差距 | 源码实证（file:line） | 承接叶 |
|---|---|---|---|
| G4-1 | 控制树跳过仍判绿：`_ran_test` 只认 `run` 事件，`classify_control` 不要求 pass 终局、不拒 skip | check_event_integrity_mutations.py:932-934,1033-1041（动态：skip→green） | 13.R2 |
| G4-2 | 目标字样+无关失败冒充 killed：`_assertion_hit` 按 Test 名+子串归属，父测试日志／同测试另一断言即命中 | :958-972（动态：parent-log／other-assertion→killed） | 13.R2 |
| G4-3 | 变异未进入编译面仍算应用：`apply_patch` 只核锚命中数==1，`go_test_command` 无 `-tags`，无 `not_applied` 判词，无包／标签／模块成员核 | :1433-1450,1403-1405；无 NOT_APPLIED 常量（:83-86） | 13.R2 |
| G4-4 | 变异判词手填：`mutation_verdicts` 读登记表自由文本，非 `--out` 运行产物 | check_dod_coverage.py:455,393-413；dod_predicates.json:135-141 | 13.R2 |
| G4-5 | 合并拒绝面**未生效**：`required_status_checks`=ABSENT，红 CI 不阻合并（「flaky」override 机械可行） | `gh api …/main/protection`→ABSENT；ci.yml:428 自认 required 是外部设置 | 13.R3（外部权限，须授权） |
| G4-6 | 归档拒绝面未接线：`openspec archive -y` 只跳确认、`mv` 全绕过；无提交树检查拒无证据归档 | `openspec archive --help`（无证据选项）；check-openspec.sh 仅 spec hygiene；ci.yml:174-194 | 13.R3 |
| G4-7 | 本地预检只跑 `--check-contract`（合同态），不跑 `--bundle`／`--evidence` 拒绝面 | lint.sh:179,188；preflight.sh 无 evidence／bundle；真实面仅 ci.yml:408 | 13.R3 |

**真实消费者链（verdict 在哪产生、在哪被读／不被读）**：
- 产生：`run_runtime_scenarios.py --out`（preflight:58/84/112、ci:250/325）＋`check_event_integrity_mutations.py --out`（preflight:122、ci:354）→ 制品。
- 读取：`check_runtime_evidence.py --bundle`（**仅 ci.yml:408** event-integrity-required 作业）消费上述制品；`set -euo pipefail`＋`run()` 使非零退出在 lint.sh／preflight.sh 内传播。
- **链断点**：①该 bundle 作业**非必需检查**（ABSENT）→ 红 verdict 不阻合并；②`check_dod_coverage.py` 经 lint.sh:145→preflight:48／ci:109 `test` 作业被消费（exit 1 传播），但 `test` 作业同样非必需；③本地链只跑 `--check-contract`，不读运行证据；④归档／合并无证据拒绝面。名字出现在源码≠消费者成立：以上每条均经真实 CLI 运行＋退出码传播核实。

## 二、R2 证据聚合的绑定字段

一份完成证据 SHALL 至少绑定下列各组，缺一组即判无效：

| 组 | 字段 | 判据 |
|---|---|---|
| 源树 | 受测模块及实际本地依赖闭包 `module_path`／解析目录／`revision`／`content_hash`；fixture/config、go.mod/go.sum/go.work、合同及运行器/检查器身份 | R2a 由运行器记录实际解析闭包并核运行前后不漂移；R2b 在消费时对 `--repo-root` 现树重算。A/B 各绑各树，control/mutant 各绑精确补丁前后树；证据互相一致与对应现树是两条独立条件，不信手填字段，也不把本地摘要当来源认证 |
| 命令 | `argv`（含 `-count=1`、`-tags`、`-race`、`--seed`、`--window`）、工作目录、影响判定的环境变量白名单 | 命令可复跑且与合同派生命令一致 |
| 期望集合 | 合同登记的 `test_ids`／`subtests[(parent,name)]`／`mandatory` 标记／`scope` 的 `remaining_required` | 期望集合来自规格与命令清单，MUST NOT 由本次执行结果反推 |
| 实际执行 | 每个用例的 `run` 与**通过终局**、包终局、无 `skip`；实际执行计数 >0 | 零匹配、只跑子集、被跳过，均判失败 |
| 量 | 量名、单位、计量区间及父子包含关系、聚合方式、样本数与工作量 | 启动和整代总量分开；分项相加只用独占区间或时间并集，嵌套/并发不能重复累计；与09.R2同口径 |
| 有效性 | 控制树真跑且绿、合同摘要一致、`bundle` 同源、产物路径可追溯、判词来源是运行产物而非登记表 | 任一不符即判无效 |

**明确不作证据**：登记表手填的 `killed`；源码里出现关键词；退出码 0 但零测试；同一执行凭据冒充不同独立轮次；把检查器自测当作产品验收。

## 三、假 killed 的四个类别（含现有覆盖）

| 类别 | 现状 | 判据（须具名拒绝） |
|---|---|---|
| 编译失败 | 已覆盖（`classify` 判 `compile_failure`） | 不计杀死 |
| 超时／零命中／无断言 panic／测试照绿 | 已覆盖 | 不计杀死 |
| **控制树跳过** | **未覆盖**：`_ran_test` 只认 `run` 事件 | 控制树须有该选择器的**通过终局**且无 `skip`；否则整轮判「控制树未真跑」，变异判词一律不计数 |
| **目标字样 + 无关失败** | **未覆盖**：按 `Test` 名归属、按输出包含判定 | 失败须来自**该测试该断言的同一条失败记录**；无关测试、无关包、或同测试内另一断言的失败 MUST NOT 计入 |
| **变异未真正应用** | 部分覆盖：锚命中数≠1 即拒 | 还须核被测文件属于该选择器的编译面（包、构建标签、模块），否则判 `not_applied` 而非 killed |

**13.R1 动态确认（工作树实测，probe 日志 `/tmp/recovery-13r1/probe_holes.log`，EXIT=0）**：上表三条「未覆盖／部分覆盖」均已用反例**动态复现**，非仅静态读码——①控制树 skip 事件流（`run`+`skip`,exit0）经 `classify_control` 判 `green`（`_ran_test=True`，无 pass 终局／skip 核）；②父测试日志含登记字样／同测试另一断言失败，经 `classify` 判 `killed`+`assertion_hit:True`（两变体均复现）；③`apply_patch` 成功写入 `//go:build ignore` 的越包文件，`go_test_command` 无 `-tags`，无 `not_applied` 判词。三条假 killed 判据缺失成立，承接 13.R2；现有缺陷允许当前判错，不伪造已通过。

## 四、R3 三处拒绝入口

| 入口 | 现状 | 接入方式 | 权限 |
|---|---|---|---|
| 本地预检 | `scripts/gates/preflight.sh` 已跑部分检查 | 追加「证据拒绝」一步，非零即中止 | 无 |
| CI 必跑作业 | `event-integrity-required` 已汇总 short/long/mutations 的 bundle | 扩为消费 §二 的绑定字段；判据判词改读运行产物 | 无（改工作流须单独授权） |
| 归档与归档目录变化 | `openspec archive` 不核 DoD | 受控归档入口验有效证据；提交检查及服务端必需检查还要从实际文件树识别新增归档目录，直接archive或mv也须有同源码证据，否则拒绝进入可发布提交 | 本地接线不等于硬拦截；服务端保护与工具执行权限须授权并验证 |
| 合并 | `required_status_checks` 为 `ABSENT` | 由外部配置必需检查 | **外部权限，须明示授权** |

纪律：未配置的入口 SHALL 如实报告「未生效」，MUST NOT 声称已拦截；权限不足 SHALL 阻塞并上报，不得用可绕过的本地包装脚本冒充硬拦截。

## 五、与 09／10 的分工

- **09**：量什么（七段口径、四量、矩阵与同条件对照）。本域消费其量名/单位/区间定义。
- **10**：判据与账是否对齐（P0–P8 双向对账、余项处置、交接）。本域消费其对账表作为「期望集合」的输入之一。
- **13**：证据是否成立、能否挡住放行。三域不重定义彼此的量与账；任一处出现第二份真源即视为冲突。

### 本域按行消费的 CI 失败清单（编排者补记，R23）

> 13.R1 的审计以 [09/design.md §八](../09-acceptance-real-measure/design.md) 的固定清单为输入面，但其产出未逐行标注引用。下表由编排者按已核证据补记映射，**不是 13.R1 代理的原始输出**。补记目的：满足 09.R1 消费者锚「表被证据域按行引用」，并解开 09.R1→13.R2→09.R2→09.R1 的依赖环（见 09/tasks.md R23 注）。

| CI 行 | 本域消费点 | 关系 |
|---|---|---|
| `CI-3`（256 三片，内层 exit 124，`ran without a terminal pass`） | G4-1 | **对照面**：引擎对*产品运行*缺终局判 unattested 是对的；而 `_ran_test` 对*控制树*只认 `run` 事件、不查 pass 终局与 skip。同一份「须有通过终局」的要求，产品侧已守、控制树侧未守——这正是 G4-1 要补的不对称。 |
| `CI-5`（`durable_scan: unattested`，三数据前缀零键，8/9 代无扫描行） | N6（R20）＋G4-4 | 该 attestation 的代理量的是已退役的整族扫描机制。补发库存计数行可使其转绿却证明不了重读，故须与 N2 删结构同批重写代理；重写后的拒绝面归本域 13.R2。 |
| `CI-6`（`errors[0]=evidence.go_test_exit_code must be successful integer exit 0`，六条 verdict） | G4-5 | bundle 门**确实**具名拒绝了假绿（引擎能力成立）；缺口不在判决而在**该作业不是必需检查**（`required_status_checks`＝ABSENT），故红判决不阻合并。判决能力与放行接线须分开评价，不得因门会拒就声称已拦截。 |

`CI-1`／`CI-2`（同一判定答出两产物）归 03.R1 产品面，`CI-4`（票据结算通知失配）归 04.R1 呈现消费者面，本域不认领其修复，只在 13.R2 绑定期望测试集合时按行引用。

## 六、风险与回退

- **自证风险**：新增判据本身也要能被证伪。每个新拒绝面 SHALL 配一条「注入该假绿的 fixture 必被拒」的自测；自测只证明已列反例，MUST NOT 宣称绝无遗漏。
- **误伤风险**：收紧控制树与断言归属可能打到既有合法形态（并列子测试、共享日志的多 case）。迁移时 SHALL 逐条核对既有合同条目，MUST NOT 以放宽判据换绿。
- **回退**：本域是判据与拒绝面，回退＝撤掉拒绝入口的接线；不改变产品行为，也不改变既有证据格式。
- **审核分工**：机器挡已列反例；规格完整性与自然语言判据的对应关系仍须独立审查，本域不承诺自动理解规格全集。

## 七、未来实施触碰集（本轮只登记，不动）

- 判据与自测：`scripts/gates/check_runtime_evidence.py`、`scripts/gates/check_event_integrity_mutations.py`、`scripts/gates/check_dod_coverage.py` 及其 `test_*.py`。
- 汇总与合同：`golden/runtime/scenarios.json`、`golden/runtime/event_integrity_scenarios.json`（只在其声明的字段面内改）、`golden/runtime/dod_predicates.json`（把 `mutation_verdicts` 的手填改为消费运行产物）。
- 入口：`scripts/gates/preflight.sh`、`.github/workflows/ci.yml`（改工作流须单独授权）。
- **不动**：`openspec/changes/archive/**`、`examples/wechat-bot/.wechat-config/data`、用户规则与 skill、分支保护。

## 八、假绿形态清单新增两例（来源：06 G6 反转设计门 §十七·一／§十七·十）

两例均由 06 域在 G6 实测中发现，`06/design.md:485` 与 `:704` 的「计划回写」要求它们进本域清单。**形态先落本节，再谈闭合**——只活在战报里的发现是残留（R64 教训）。

| # | 形态 | 实测来源（`06/design.md`） | 机理 | 本域可判面 |
|---|---|---|---|---|
| FG-1 | **区间包含**（interval containment） | §十七·一「首轮 `storeLoad` 把 `openJournalKV` 计在内」，已把引擎开档移出计时区间重跑 | 一个计量区间包含了另一个被单独计量的区间，同一份工作被合计两次；外层读数恒大于真实成本，且永远不会因为内层变快而变慢 | 证据面的量只有 `liveness`（量名 `go_test_bound`、单位秒、区间＝`bound_seconds`）与每次调用的内层 `-timeout`。内层未声明或内层 ≥ 外层时，被外层杀的调用与自然完成的调用在区间上不可分，读数归属错误 |
| FG-2 | **自指长度**（self-referential length） | §十七·十「order 语句内联自身字节长度导致水位漂移」 | 被计量的量由被测载体自身的字节数决定：写入多少，水位就被推高多少，判据永不失败 | 证据面：任何「由被测产物自己给出」的计数都不得充当证据——`repeat_total` 不是轮次数，`subtests` 不是终局数，`verdict` 不是判定。轮次数由合同登记的 scope `repeat` 给出，终局由 test-log 的 `pass` 事件给出（两者现已由 `check_event_shape`／`index_event_log` 强制） |

**FG-1 的机器闭合**（`check_runtime_evidence.py`，本叶实现）：每次调用 MUST 声明自己的内层界限，且内层 MUST NOT 超过 `liveness.bound_seconds`；拒绝判词 `nested interval`。同一轮内同一 `(module, package, test)` 重复 run（`index_event_log`）与同一矩阵组合出现两次（bundle `seen[key]`）属同一形态的另一分支，已闭合。**成本量名口径（启动与整代总量、七段区间）属 09.R2，本域不重定义**，只要求带量证据声明包含关系并只合计独占区间。

**FG-2 的机器闭合**：由合同与 test-log 给出分母与终局，证据自报字段一律不作数（现由 `check_event_shape` 的 equal-set 与 `check_event_test_selector` 的 run+pass 强制）。**未闭合分支（如实登记，不假装闭合）**：变异门自身的「变异比例」分母仍是登记表（R64 已证自报比例须复算）。该比例不是 bundle 消费的字段，`check_mutation_evidence` 只要求逐条 `killed`＋`counted_killed`，故本叶不新增判据；若将来把它作为证据面，须以源码发射点（而非登记项）作分母。

## 九、绑定字段面审计（本轮实测，97 例既有覆盖与真实缺口）

> **限定勘误**：本节为原审计原文；“源树已覆盖/无缺口”仅能成立于字段存在、格式和证据间相等，不能扩成现树匹配；“13.R2 唯一剩余 N6”撤回，现有 R2a/R2b/R3a 同样未闭。命令/环境面已做部分不因这次勘误抹掉，但仍需按最终树复验。

| 组（§二） | 既有覆盖 | 覆盖者 | 缺口 |
|---|---|---|---|
| 源树 | **已覆盖** | `check_event_fingerprints`（module_path/dir/revision/content_hash＋重复模块拒）；bundle `check_bundle_module_fingerprints`（同一模块跨证据同一摘要）；`check_mutation_baseline`（变异绑 runner 基线） | 无 |
| 命令与参数 | **未覆盖** | `run_config` 只核 `seed/window/tags/race` 与合同一致 | 生产者已把 `run_config.selectors` 与 `run_config.invocations`（含 `dir`／`packages`／`tags`／`race`／`command`／`exit_code`）写进每份证据，**检查器从未读它们**：登记的 argv 是否含 `-count=1`、工作目录是否就是受测模块目录、调用的包是否覆盖所选测试、`-race`/`-tags` 是否与声明一致，全部无判据 |
| 环境 | **已覆盖（本轮闭，见 §十一）** | `check_event_environment`：①`run_config.environment` 必存在；②名字须在 `EVENT_ENVIRONMENT_NAMES`（14 项）；③值须为字符串；④`GOMAXPROCS` 须为正整数（`runtime/proc.go` 只认正整数，其余静默忽略）；⑤`GOFLAGS` 不得决定任何「已记录 argv 未决定」的 `go test` 形状旗标（方向见 §十一·三） | 无「可判」缺口；**环境值本身是生产者自报**，检查器不声称能证明宿主环境（已写入判词与 docstring） |
| 期望全测试集合 | **已覆盖** | evidence 模式：`test_ids` 须是登记的 id 且 scope 相符；bundle：equal-set 门「必须跑满合同 event-short 父集合」＋每轮集合不得漂移＋六组合各恰一次 | 无（集合由合同给出，不由结果反推） |
| 实际执行 | **已覆盖** | `index_event_log`（包 start/终局窗口、重复 run、缺终局）＋`check_event_test_selector`（须有 run＋pass，无 fail/skip，包级 same） | 「执行计数 >0」由 run+pass 强制，非独立计数；已等价 |
| 量 | **未覆盖** | — | `liveness` 已由生产者写入（量名／单位秒／区间），检查器从不读 |
| 有效性 | **部分覆盖** | 控制树真跑（`classify_control`，G4-1 已闭）；合同摘要一致；bundle 同源；产物可追溯（文件数、run_id／log 不得复用） | 无 |

**结论**：97 例覆盖了「源树／期望集合／实际执行／有效性」四组，**未覆盖「命令与参数／环境／量」三组**。本轮补前两组的可判部分（命令面全量、环境的 argv 覆盖面、`liveness` 量面）；**环境记录面在续单中已闭（§十一）**：生产者 `scripts/scenarios/run_runtime_scenarios.py` 增 `run_config.environment`（该文件是门脚本不属产品面，R74 裁定归本域写集合）。**13.R2 仍不勾的唯一剩余项是 N6**（产品日志行，归 06.R4）。

## 十、N7 白名单忽略门：`files` 的真实来源与本门边界（本轮实测）

- **`checkTrackedHygiene` 的 `files` 来自 `git ls-files -z`（索引）**，实测 `scripts/codetools/hygiene.go:113-121`；第二条列表 `ignored` 来自 `git ls-files -ci --exclude-standard`（已跟踪且被忽略），由 `checkTrackedAndIgnored` 判。故既有门只覆盖「已跟踪⇒不得被忽略」，**未跟踪且被忽略的源文件对两条列表都不可见**。
- **真实缺口形态**：`examples/wechat-bot/.gitignore:5` 为 `*`（白名单模式）。在该模块新建 `*.go` 而忘记加白名单行时，文件不入索引、CI 永不编译，本地 `go test` 照跑照绿。
- **补的门**：以 `git ls-files -o -i --exclude-standard -z`（未跟踪且被忽略）枚举，只对**以 `.go` 结尾的文件**报 `whitelist-unnamed-source`，并点名其所属 `.gitignore`。实测当前全仓该集合为 **0**（`git ls-files -o -i --exclude-standard | grep '\.go$' | wc -l` → 0），故新增门零误伤；模块内 47 个未跟踪 `.py/.js/.sh` 本地脚本不属 `go test` 编译面，不报。
- **`knowledge_base` 的定性（不是缺口实例）**：`examples/wechat-bot/knowledge_base/` 是一个**独立 git 仓库**（自带 `.git`，`git rev-parse --show-toplevel` 指向自身），内含 **0 个 `.go` 文件**，且 `examples/wechat-bot/README.md:127` 明写它是运行时生成、刻意不入库。R24 记的「工具报它是被忽略的 Go 文件」是 `scripts/comment_policy/main.go:171-178` 的**消息措辞缺陷**：它的查询是 `git ls-files -i -o --exclude-standard -- '*.go'`，对嵌套被忽略仓库返回的是**目录条目** `knowledge_base/`，而消息把它称作「Go file」。该文件不在本叶写白名单，**登记不改**。

## 十一、环境记录面：集合推导、方向纠正与前后对照（续单实测，2026-11-16）

### 一、记录哪些变量（由消费方实证推导，不凭猜测）

生产者 `recorded_environment()` 只抄 `EVENT_ENVIRONMENT_NAMES` 中**本进程实际持有**的名字；未设置的变量不写（不写默认值），值原样透传不重写。集合按「谁真的读它」逐条取证：

| 变量 | 消费点（证据） | 为什么它会改变判定或度量 |
|---|---|---|
| `GOFLAGS` | `go help environment` 原文：「Flags listed on the command line are applied after this list and therefore override it」 | 为未在命令行出现的旗标供默认值（`-short`/`-shuffle`/`-skip`/`-tags`/`-race`…），可直接改变跑哪些分支、并发与构建面 |
| `GOMAXPROCS` | `runtime/proc.go:935` `strconv.ParseInt(gogetenv("GOMAXPROCS"), 10, 32)`；`examples/wechat-bot/smoke_harness_test.go:1044` 把它当测量字段上报；`.github/workflows/ci.yml:98-101` 对确定性用例钉 `GOMAXPROCS=1` | 调度宽度，决定 `-race` 与成本读数的抖动 |
| `GOWORK` | `go help environment` GOWORK 条目（`auto`/`off`/指定 `go.work`） | 决定 `go -C <module>` 的主模块集合，可改变编译面 |
| `GODEBUG` | `assembly/host.go:425` 读它合并 `gctrace`；`tests/integration/execution/debug_pprof_test.go:82,91` 断言其值 | runtime 设施开关，改变日志与被断言的字节 |
| `TAGENT_GCTRACE` | `assembly/host.go:340,422` | 开关会把 `gctrace` 合进 `GODEBUG` |
| `TAGENT_BOOT_TRACE` | `assembly/host.go:467`、`assembly/run.go:596` | 开关会多打 boot 轨迹行，被日志断言消费 |
| `TAGENT_HTTP_PORT` | `assembly/host_face.go:32` | boot 面监听的端口，未 `t.Setenv` 的用例会继承宿主值 |
| `TAGENT_SESSION_ID` | `features/evolution/meditation/mount.go:268` | 给挂载的观测打标签 |
| `SMOKE_ROUNDS` | `examples/wechat-bot/smoke_test.go:285` | 改 smoke 轮数 |
| `TAGENT_CALIBRATION_MODE` / `_PLAN` / `_RUN_DIR` / `_BASELINE` / `_COLD_BUILD_MS` | `examples/wechat-bot/smoke_harness_test.go:2251-2257`，消费点 `:566,599,2040,3270`、`smoke_test.go:3449,3454,3605` | 选择校准分支、基线与读数，可让整段用例走另一条路 |

**刻意不记**：`TAGENT_RL_AUTH_TOKEN`、`SMOKE_API_KEY`、`OPENAI_API_KEY`、`TAGENT_REAL_*_API_KEY`、`TAGENT_TEST_API_KEY` 等凭证或指向凭证的变量（证据文件落盘且入仓，记录即泄漏）；`TZ`/`LANG`/`LC_ALL`——**在范围内的用例里没有找到 locale/时区依赖的断言**，故不凭空列入。**不整份 dump 环境**。

### 二、凭证排除是机制不是惯例

`recorded_environment()` 先扫声明表：任一名字含 `EVENT_CREDENTIAL_MARKERS`（`AUTH`/`CREDENTIAL`/`KEY`/`PASSWORD`/`SECRET`/`TOKEN`）即抛 `RunnerError`，**不给「先加上再删」的窗口**。两条证据：①正测 `test_a_credential_bearing_variable_is_never_recorded`（环境里放 4 个凭证变量，记录为空且 `evidence.json` 全文不含其值）；②反测 `test_a_declared_credential_name_is_refused`（把凭证名塞进声明表即拒）。检查器侧另有 `neg4`：凭证变量名不在记录集合内→具名拒绝，且**判词只点名变量名、不回显值**（实测 `value-echoed=False`）。

### 三、方向纠正：`GOFLAGS` 与 argv 的优先关系（R74 旧判据判反了）

R74 的 `check_event_environment` 拒「`GOFLAGS` 设了 argv 已决定的旗标」，理由写作「记录的命令就不是跑的命令」。**该前提与 go 的语义相反**：命令行旗标**后应用并覆盖** `GOFLAGS`（`go help environment` 原文，本次实测复读）。所以：
- `GOFLAGS=-count=1` 并列 argv `-count=1` → 跑的就是 `-count=1`，argv 仍如实描述，**旧判据误拒真话**（实测 R74 对 `pos-goflags-decided-by-argv` exit=1，报「already decides」）；
- `GOFLAGS=-short` 而 argv 全无 `-short` → 跑的是**短模式**，argv 漏记了决定项，**旧判据反而放行**（实测 R74 对 `neg1` exit=0）。

纠正后的方向：`EVENT_RUN_SHAPING_FLAGS` 中的旗标，只要**任一**已记录 argv 未决定，就具名拒绝（`would not be the command that ran`）；argv 已决定则接受。这是**判据纠向**不是放宽：新增了真实的拒绝面（`-short`/`-shuffle`/`-skip`/未声明的 `-tags` 等），同时不再误拒合法形态。既有用例 `test_a_declared_environment_that_redecides_the_argv_is_rejected` 因此**改名为** `test_a_goflags_flag_no_recorded_argv_decides_is_rejected` 并换夹具（`-short`）。

### 四、六条前后对照（真实 CLI，逐条可复跑）

基线说明：`HEAD` 提交版**尚无四个 event 检查**（本轮四检查未提交），故环境面的真实「前」是 R74 工作树态——以 `git show`＋反向还原今日两处 hunk 复现（脚本与输出留 `/tmp/recovery-13env/`）。

| fixture | HEAD | R74（前） | 本树（后） | 判词 |
|---|---|---|---|---|
| `neg1` `GOFLAGS=-short` 而 argv 无 `-short` | 0 | **0 误放** | **1** | `GOFLAGS sets '-short', which no recorded argv decides` |
| `neg2` 缺 `run_config.environment` | 0 | **0 误放** | **1** | `run_config.environment is absent` |
| `neg3` `GOMAXPROCS=auto` | 0 | **0 误放** | **1** | `not a positive integer, which the runtime ignores` |
| `neg4` 记 `TAGENT_RL_AUTH_TOKEN` | 0 | 1（旧白名单） | **1** | `outside the recorded determinants` |
| `pos` 完整一致记录 | 0 | **1 误拒** | **0** | — |
| `pos` `GOFLAGS=-count=1` 并列 argv `-count=1` | 0 | **1 误拒** | **0** | — |

### 五、两处声明与失败方向（如实登记）

检查器 `EVENT_ENVIRONMENT_NAMES` 与生产者同名常量是**两处声明**（检查器以 `python3 路径` 独立执行，不 import 生产者）。失败方向已定：生产者多记一个名字而检查器未登记→**具名拒绝**（不是静默信任）；检查器多登记→记录里不会出现，无害。二者漂移由 `test_the_recorded_determinants_match_the_runner_declaration`（读生产者表逐项比对）**在自测里就红**，不必等到 CI。

## 十二、当前复盘与可执行承接（仅规划，未执行）

### 证据身份与正式入口

1. **R2a：依赖源码未绑定。** `run_runtime_scenarios.py:1184-1189` 只指纹 `event_groups` 选中模块，六长只选 wechat；`examples/wechat-bot/go.mod:5` 将 tagent replace 到 `../..`，故根模块实际参与编译却未被该份证据绑定。后续按实际解析的 local replace/go.work 闭包取源码与配置摘要，运行前后核一致；不把无关仓库或真实数据塞进闭包。共享算法优先复用既有指纹实现，不另造旁路摘要口径。
2. **R2b：内部同源不等于当前树。** `check_runtime_evidence.py:2594-2634` 只收集各证据的 `content_hash`/`revision` 并检查同模块是否单值，没有对现树重算。全套过期证据仍可内部一致；后续单份和 bundle 消费入口均比 `--repo-root` 的实际闭包、合同及工具身份。仅改根源码而 wechat 字节不变、全套旧证据一致、缺依赖记录各应拒；合法两树 A/B 不要求互相相等。源码/工具最终定稿后再生成证据，不能重写旧 evidence 的 digest 冒充复跑。
3. **R3a：正式 DoD 未获运行产物。** 检查器已从 `--mutation-evidence` 读产物（`check_dod_coverage.py:817/:845`），但 `scripts/gates/lint.sh:145` 默认无此参数；手工带参成功只证明该次 CLI 判定，不证明 lint/preflight 已绿。现 `preflight.sh:48` 先 lint，`:122` 才生成 mutations，后续调整生成→现树核验→DoD 顺序，不能让缺证据的前置 lint 阻死生成，也不能把证据缺失降为成功。

**接线选择（拟实施）**：沿既有 preflight 的 `event_out` 产物归属，每次运行独立目录、不覆盖历史轮次；以 `TAGENT_RUNTIME_EVIDENCE_DIR`（拟新增、未实现）将该次目录传给真实 lint，再由 lint 显式传其 mutation 子目录给 DoD、将 bundle 交现树检查。独立调用 lint 未给有效目录就具名失败，不搜索“最近一份绿结果”、不把登记表当后备。正向正式命令拟为 `TAGENT_RUNTIME_EVIDENCE_DIR=<本次目录> bash scripts/lint.sh`；`bash scripts/preflight.sh` 自己生成并传递，不要求用户手工拼检查器参数。自测走真实 shell 入口，核 argv、产物被读取和退出码传播；夹具自测不代完整正式运行。CI 消费及服务端必需检查仍是 R3 的外部未完成面，本轮不改工作流或权限。

**旧项不丢**：R1 的 G4 审计完成不代表 G4 放行；原 R2 的命令/环境/期望集合/计数/量区间、假 killed 三类和 N6 仍逐项复验；新增 a/b 只补身份缺口。R3a 不替代原 R3 的缺片、跳过、错提交、直接 archive/mv 绕入口及外部保护验证。量义与独立阶段归因归09.R2a/R2b，旧清理及主规格一致归10；三域各自闭合后才汇入R4。

### F3 与注释门的限定勘误

历史 [R259](../../ORCHESTRATION-HISTORY.md) 三反例保留：boot 的“只给键→带值走查”确实测点读增长；票据缺失确实测自主召回句柄；长跑的“全 log 校验→只校水位后”只让 `integrityBytesScanned` 的 1:1 安全断言红，不能证明性能回归被捕获。因此 F3 **部分有效、尚未完成**；09.R2b 补保持 CRC 不变的真实阶段退化与独立墙钟负例，R4 核失败站点、量、同源码复原及绿证据，不能再用安全红代性能红。

上一轮裸 `comment_policy` 的 `[.]` 与三模块 baseline 不符（`main.go:160-162/:271-274`），可能在打印总数前退出；未打印 finding 不等于零，也不能据此归因外来文件。撤回这两结论，不改历史原始报告、不改 baseline；正确两 scope 命令与完整输出核法由10.R7承接。其他独立证据是否确属外来工作需另核，本勘误不反向替其定性。

### 阶段提交不等于完成归档

独立、确定完成的部分可作为后续分批提交候选，但先核精确文件/构建依赖闭包、代码/spec/golden/测试锚的原子一致和该树证据；未绑定依赖的六长或内部一致的旧 bundle 不足以支持“已确定”。本轮不 git 写，也不把候选提交视为整个变更完成。OpenSpec 完成归档继续等09成本与矩阵、10旧项、R2/R3/R4及其余产品缺口和独立审查；历史全文存档仅保全报告，不改变归档状态。
