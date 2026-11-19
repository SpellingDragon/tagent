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

## 十三、R2a/R2b 执行落地（R270 实测，2026-11-18）

### 一、落地形态：闭包写进证据，消费时对现树重算

§二「源树」组的两条独立条件各自落地，互不替代：**证据记录实际解析的闭包**（R2a），**检查器在消费时对 `--repo-root` 重算同一闭包**（R2b）。

| 侧 | 落点 | 记录/核对的字段 | 判据 |
|---|---|---|---|
| 生产者（R2a） | `run_runtime_scenarios.py`：`source_binding()`／`binding_window()`／`tool_identity()` | `source_binding.modules[]`（`dir`／`module_path`／`roles`／`revision`／`content_hash`）、`edges[]`（local `replace` 的 from／target／resolved_dir）、`workspace`（`go.work` 状态与 `use` 目录）、`before`／`after`（闭包摘要＋模块数）、`repo_root`、`tool`（入口／digest／python／go） | 运行前解析、运行后复算，两次摘要不等即拒（`the local dependency closure changed while the run was in progress`） |
| 生产者（变异，R2a） | `check_event_integrity_mutations.py`：`tested_source_binding()`／`baseline_evidence()`／`run_gate()` | 报告级 `source_binding`＋`module_fingerprints`＋`gate_tool`（脚本入口／digest／python／go） | 变异跑完后再次复算闭包，不等即拒（不写下一份描述"从未静止的树"的报告） |
| 消费者（R2b） | `check_runtime_evidence.py`：`check_event_source_binding()` | 同一组字段，比对目标是 `--repo-root` 的**现树字节** | 每模块 `content_hash` 与现树重算值相等；按记录的 tested 模块重解 replace／work 边（漏一条即具名报缺，不折进 digest 差异）；工具身份须是本树的那一份；`before`／`after` 须等于闭包合计。缺 `source_binding` 即具名拒绝 |

**`revision` 的分工不变**：字节是必须匹配的对象，`revision` 只作同一次 checkout 的两个制品之间的对齐（`check_mutation_baseline`）——一次不改编译字节的提交会移动 SHA，故不与树比对（§十二·2 的原判据保留）。

**本轮真产物实测**（`/tmp/r270-short-ci/repeat-1/evidence.json`，event-short 第 1/10 轮）：

- 闭包 **3 模块**：`.`（`roles` = `tested`+`local-replacement`，因两个 example 模块都 replace 到它）＋`examples/wechat-bot`＋`examples/cli-host`；**2 条** local replace 边（均 `../..` → `.`）；`workspace = absent`。
- 这意味着 §十二·1 指出的缺口（六长的 wechat 模块实际编译根模块、而旧证据只绑 wechat）在字段面已闭：`. ` 以 `local-replacement` 身份进入同一份证据。
- 运行前后摘要同值 `sha256:da458bf4…`（modules=3）；工具身份 `run_runtime_scenarios`／`sha256:9bca6859…`／python 3.12.8／`go1.27.1 darwin/arm64`。
- **`golden/runtime/event_integrity_scenarios.json` 未改**：闭包由树内 `go.mod`／`go.work` 解析，合同不声明闭包；实测未动合同时 `--evidence` 校验 exit 0，故 §十二「若需证据字段声明」的条件不成立（合同摘要仍 `sha256:e1dfb96b…`）。同一结论在变异侧成立（见下）。

### 二、锚漂移与重指（12.R2.2 级联，语义不变）

`12.R2.2` 改 `assembly/facts.go`（`foldMapped` 只认链最新事实、`persistFact` 的存在性读局部名 `err`→`heldErr`）后，`validate_registry` 报 **3 条** stale 锚：一条属 `MUT-EVID-04-position-boundary`、两条属 `MUT-CAND-01.8-storage-success`。这是跨面锚漂移（另一叶净改产品文件），不是本域判据缺陷；按 2026-11-08 先例「facts.go 边界改动同步重锚」处理：

| 变异 | 原锚失效原因 | 重指后的锚 | 变异语义 |
|---|---|---|---|
| `MUT-EVID-04-position-boundary` | 折叠段从「单条赋值＋两处 `endedAt`」变成「`var landed bool`＋多值赋值＋`if !landed` 早返」，锚 0 命中 | 现形整块；`position := held.projection.Len()` 记在折叠与早返**之前** | 不变：边界记成它追加的位置而非身份。补丁集仍 8 条／2 文件，自测的补丁数与断言无需改 |
| `MUT-CAND-01.8-storage-success`（1/2） | `default: return err` 段的下一个标识因新增 `chainHoldsFact` 从 `// sameFactOf` 变 `// chainHoldsFact` | 改锚到 `// chainHoldsFact` | 不变：提交被拒仍答成功。新增的 `chainHoldsFact` 读拒仍会传播，故描述里点明本变异覆盖的是 `persistFact` 自己的出口（不把它说成"所有读也吞掉"） |
| `MUT-CAND-01.8-storage-success`（2/2） | 存在性读的局部名 `err`→`heldErr` | 同名替换 | 不变。补丁集仍 4 条／1 文件 |

重指**不是**放宽：判据（`apply_patch` 锚命中数须恰为 1、补丁须进被测编译面、killed 须由具名断言命中）一条未改，只把锚对准现形代码。重指后真跑变异门复验：两款均 `killed(named_assertion)`，`control_tree_green=true`，见 §十三·三。

### 三、正式入口与三套自测实测（逐条可复跑）

| 命令 | EXIT | 实际结果 | 红绿归属 |
|---|---|---|---|
| `python3 scripts/gates/test_check_event_integrity_mutations.py -v` | 0 | Ran **55** tests OK（修前 51 例：3 failures + 2 errors） | 收口绿 |
| `python3 scripts/gates/check_event_integrity_mutations.py --mutation MUT-EVID-04-position-boundary --mutation MUT-CAND-01.8-storage-success --out /tmp/r270-mut-evidence --json` | 0 | `gate.ok=true`、`control_tree_green=true`、2/2 `killed(named_assertion)`（`assertion_hit=true`、`counted_killed=true`）；产物同带闭包 `sha256:3e42e6fd…` 与 `gate_tool` `sha256:b1270eaf…` | 重锚后仍 killed |
| `python3 scripts/scenarios/test_run_runtime_scenarios.py` | 0 | Ran **39** tests OK | R2a 自测保持绿 |
| `python3 scripts/gates/test_check_runtime_evidence.py` | 0 | Ran **116** tests OK | R2b 自测保持绿 |
| `run_runtime_scenarios.py --scope event-short --repeat-index 1 --timeout 600`（= CI 逐分片形态） | 0 | `verdict=match`、`repeats: passed=1/10`、run_id `525f7dca-…` | 真产物绿 |
| `check_runtime_evidence.py --evidence <上条产物> --repo-root . --json` | 0 | `ok=true`、29 test／135 anchor／9 包／180 事件行 | 单份现树绑定绿 |
| `check_runtime_evidence.py --bundle <同一产物> --json` | 1 | 5 条错误**全是矩阵完整度**（short 1/10、long 0/6、mutations 0/1），树绑定/判词 0 条 | 整包绿待 09.R3/R4 齐矩阵，非本叶可造 |
| `check_runtime_evidence.py --bundle <失败 runner 产物> --json` | 1 | **167** 条错误，`source_binding`／`closure`／工具身份类 **0** 条 | 消费者拒真红而不误判绑定 |

**并发归因（实测一次）**：与另一修复叶同跑时，一次 short 运行被本域守卫中止——`the local dependency closure changed while the run was in progress: sha256:87be253f… -> sha256:48b238e2…`，期间该叶在写 `runtime/state/durable.go`／`durable_test.go`／`assembly/checkpoint_test.go`。该红归并发写单，非本域缺陷；文件静止 120 s 后重跑即绿。这条同时是 R2a 运行前后指纹判据的**真树正向证据**（构造字典造不出这个红）。

### 四、未验证面与登记缺陷（如实列，不冒充闭合）

1. **`--seed`／`--window` 对 short 域不可用（既有缺陷，本轮不改；已在 §十五 修复）**：`event_command` 把 `-args -seed=… -window=…` 追加给一次调用的**全部**包（`run_runtime_scenarios.py:773`，`git show HEAD` 逐字相同，非本波引入），而只有 `examples/wechat-bot` 定义了这两面旗标（`smoke_test.go:3452-3453`）。故 `--scope event-short --seed 1 --window 256` 下 9/9 包以 `flag provided but not defined: -seed` 退出、`repeats: passed=0/1`、EXIT=1（实测）。CI 的 short 分片本就不传 seed（`ci.yml:250`），long 分片传 seed 而 long 只选 wechat 包，故生产链未暴露此缺陷。**结论：short 域的 seed/window 形状面本轮无真跑证据**，修它要动 runner 的派生命令构造（本叶写面外），登记给后续叶（§十五 记修法、真跑红→绿与剩余未验证面）。
2. **`--repeat 1` 与合同 `repeat: 10` 冲突**：单轮产物会被判 `repeat_total 1 does not match contract scope 'event-short' repeat 10`；CI 形态用 `--repeat-index I`（`repeat_total` 仍取合同值），本轮改用它取绿。
3. **整包绿未取**：10 短 + 6 长 + 1 变异须同一棵树，长跑归 09.R3，本叶禁跑六长。
4. **13.R2 的 N6（产品日志行）与 13.R3（外部权限／CI 接入）未动**，故 R2/R3/R4 的勾选状态不变；本轮只勾 R2a/R2b。**R3a 的接线已在 §十四 落并验证（单列一节，避免与本节的时点判断混淆）**。

## 十四、R3a 执行落地（正式 DoD 证据输入与失败传播）

### 一、落地形态：证据目录由本次运行给出，消费时对现树重判

§十二「接线选择」里三条要各自落地，缺一条就只剩"手工补参绿"：

| 侧 | 落点 | 形态 | 拒绝面 |
|---|---|---|---|
| 入口（lint） | `scripts/gates/lint.sh` 的 DoD 段 | 读 `TAGENT_RUNTIME_EVIDENCE_DIR`，把其 `mutations/` 子目录交给 `check_dod_coverage.py --mutation-evidence` | 变量未给／目录里没有 `mutations/` 产物，各**具名红并非零退出**，不搜索"最近一份绿结果"、不把登记表当后备 |
| 链序（preflight） | `scripts/gates/preflight.sh` | 证据目录一次运行一份（`TAGENT_RUNTIME_EVIDENCE_DIR` 优先，否则 `run-<UTC>-<pid>`），序为生成 mutations → `check_runtime_evidence.py --bundle` 现树绑定 → 以 `env TAGENT_RUNTIME_EVIDENCE_DIR=<该目录>` 调 lint | 前两步失败即中止，DoD 段根本不到达＝"未执行"而非"通过"；`--skip-event-long` 时整包本就不完整，绑定步具名跳过并保留"不具备完整 MR 等价证据" |
| 消费（DoD） | `check_dod_coverage.py` | 每份产物在计数判词之前先复算绑定：`check_event_source_binding()` 由 `check_runtime_evidence.py` **导入复用**（`_GapReport` 适配其 `error()` 汇入 gap 列表），绑定不过即 refuse，其判词一条不计 | 缺 `source_binding`、模块内容摘要不符、闭包摘要不符、工具身份不符、`mode=replay`，各具名红 |

**转发脚本零改动（实测结论，非推断）**：`scripts/lint.sh`／`scripts/preflight.sh` 都是 `exec bash "$here/gates/…" "$@"`，`exec` 不重建环境，`TAGENT_RUNTIME_EVIDENCE_DIR` 随进程继承；壳入口用例走 `bash scripts/lint.sh`（真转发）取到绿，即透传成立，故不在本叶改它们。

### 二、与 §十二 的偏离（一条，理由在判据面）

原文"再由 lint 显式传其 mutation 子目录给 DoD、**将 bundle 交现树检查**"：bundle 面**没有**放进 lint，留在 preflight。理由有两条且都不是省事：

1. bundle 的完整性判据是 **10 短 + 6 长 + 1 变异同树**，长跑只在 preflight 生成；在 lint 里跑 bundle 检查会把"今天没跑长跑"变成"lint 不可用"，而 CI 的 lint 作业并不产矩阵——这会把一条门债换成另一条。
2. "错树"这条拒绝面不需要 bundle 也成立：DoD 端读产物时**自己**按 `--root` 复算闭包（上表第三行），单份产物即足以拒。故 lint 的拒绝面是"缺／空／错树／手填"四类，bundle 完整度归 preflight 与 CI。

保留的代价（如实记）：`bash scripts/lint.sh` 单独跑不会验证 17 件套齐全，只验证它所消费的那份产物确实是本树产物；整包齐全由 `bash scripts/preflight.sh`（与 CI 的 `event-integrity-required` 同一命令）承担，归 09.R3／13.R3。

### 三、自测与红绿归属（逐条可复跑）

`test_check_dod_coverage.py` 39 → **51** 案：+2 条绑定负例（产物写出后树字节移动、产物不记闭包），+10 条壳入口判定（三个具名测试函数：`test_formal_lint_consumes_bound_mutation_evidence` 2 条、`test_formal_lint_refuses_missing_stale_or_handwritten_evidence` 4 条、`test_preflight_generates_evidence_before_dod_gate` 4 条）。

夹具形态：真 `scripts/lint.sh`／`scripts/preflight.sh`／`gates/lint.sh`／`gates/preflight.sh`／`check_dod_coverage.py`／`check_runtime_evidence.py`／`runtime_checklib.py`，其余同链门一律 stub；`python3` 走 shim（**记录 argv 后 exec 真解释器**，故记录即入口真跑的命令），`go` 走 shim（`comment_policy: 0 finding(s).`）；变异生产者以"声明冻结登记表＋把本轮产物写到 `--out`"的替身承担，绑定步替身**以产物已在盘上为通过条件**（顺序判据不靠 argv 相邻）。夹具自测不代完整正式运行。

| 命令 | EXIT | 实际结果 | 红绿归属 |
|---|---|---|---|
| `python3 scripts/gates/test_check_dod_coverage.py -v` | 0 | `51/51 cases passed` | 本叶新增 15 条判定全绿 |
| `python3 scripts/gates/test_check_runtime_evidence.py` | 0 | Ran **116** tests OK | R2b 自测未回退 |
| `python3 scripts/gates/test_check_runtime_workflow.py` | 0 | Ran **55** tests OK | preflight 结构断言（步骤名／旗标／长跑种子窗口）仍成立 |
| 临时删 `--mutation-evidence` 后跑自测（演示后逐字节复原，sha256 相同） | 1 | **6/51 红**：consume 2 条、preflight 消费 2 条、另 2 条随判定归位 | 正锚红＝接线就是判据 |
| 临时把 lint 步移到 mutations 之前（同上复原） | 1 | **4/51 红**：生成先于绑定、绑定先于消费、绑定步见到产物、DoD 从本轮产物取绿 | 顺序就是判据 |
| `check_dod_coverage.py --mutation-evidence /tmp/r270-mut-evidence`（真树＋真变异产物） | 1 | **12** gap 中 2 条为绑定拒绝：`module '.' content digest … does not match this tree's …`、`closure digest … does not match this tree's …`，其后 P0..P8 全为"registered and unproven" | 真树上"旧树产物不计数"实测成立 |

### 四、未验证面（停放，不冒认）

1. **`bash scripts/lint.sh` 全量绿未取**：现树还有独立门债（`comment_policy` 超基线、DoD 的 `P4` 标题漂移与 `P0..P8` 无运行产物等），这些红与本叶接线无关，也非本叶写面。本叶只证"证据消费机制按设计工作"，全量绿待债清偿后回验。
2. **`bash scripts/preflight.sh` 全链未真跑**：六长分片（每片 6270 s 上界）与 smoke 需实施期授权，本叶禁跑；壳入口顺序由夹具验证，真矩阵归 09.R3／13.R4。
3. **CI 消费未接**：`.github/workflows/ci.yml` 不由本叶改（须单独授权），故 `TAGENT_RUNTIME_EVIDENCE_DIR` 在 CI 侧仍未设置；那一面归 13.R3。


## 十五、R2c／R271 校准旗标按定义面传递（2026-11-18）

### 一、缺陷机理（真机实测，非推断）

`event_command()` 曾把合同 matrix 对写成 `-args -seed=… -window=…` 追加在**一次调用的包列表之后**。`-args` 之后的 token 由 `go test` 交给这次调用命名的**每一个**包的测试二进制，而 `-seed`／`-window` 只在 `examples/wechat-bot/smoke_test.go:3452-3453` 的 `flag.Int` 里定义，且该文件首行是 `//go:build smoke`。因此旗标面的身份是「模块目录 × 包 × 构建标签」，不是「模块目录 × 包」——同一包换个构建就不认这两面旗标。

| 命令（go1.27.1 darwin/arm64，本树实测） | 结果 |
|---|---|
| `go -C . test ./tool/exec -run '^$' -count=1 -args -seed=1 -window=256` | `flag provided but not defined: -seed`，exit 1 |
| `go -C examples/wechat-bot test . -run '^$' -count=1 -args -seed=1 -window=256`（无 `-tags smoke`） | 同上，exit 1——同包，仅因该构建不编译定义文件 |
| `go -C examples/wechat-bot test . -run '^$' -count=1 -tags smoke -args -seed=1 -window=256` | `ok wechat-bot [no tests to run]`，exit 0 |

short 域的 9 个包里，8 个非承载包＋wechat-bot 的无 tag 构建全部死在旗标解析，`repeats: passed=0/…`；long 域只选 wechat 的 smoke 构建，故生产链历史上未暴露（R271 登记／`git show HEAD` 逐字同形＝既有缺陷）。

### 二、修法：声明定义面，按面分调用

| 侧 | 落点 | 判据 |
|---|---|---|
| 声明 | `EVENT_CALIBRATION_FLAGS`＝`({"dir": "examples/wechat-bot", "package": ".", "tags": ("smoke",)},)`，注释随定义文件与其构建约束 | 只有当某构建的 tags 覆盖该条 `tags` 时，那个包才算承载者（`event_calibration_bearer()`）；自测另读真源文件核 `//go:build smoke` 与两条 `flag.Int` 仍在 |
| 构造 | `event_commands()`：无 matrix 对时仍是一次调用（未校准与 v1 形态零变化）；有对时按承载面分组，**只有承载者的调用**追加 `-args`，同组其余包另起不带 `-args` 的调用 | 一次调用只管一面旗标，`flag provided but not defined` 结构性不可达 |
| 记录 | invocation 的 `packages` 改由 `command_packages(command)` 从**实际 argv** 反读 | 记录与执行同源：拆分后不会照抄整组包名 |

合同、golden 与消费者判据零改动：`check_runtime_evidence.py` 从不要求 argv 带 `-args`，其 invocation 校验按（`-C` 目录、`packages`、`-tags`、`-race`、`-run` 命名并集）成立，拆出不改判据；`test_check_runtime_evidence.py` 116 例未动即绿。

### 三、被否的方案

1. **在合同加「消费校准旗标」字段**——`golden/runtime/event_integrity_scenarios.json` 本变更冻结（§十三·一实测未改），且"哪面旗标在哪个构建里存在"是 Go 源码的构建约束事实，不是合同能声明的性质。
2. **只按「组内含 03.11 长跑用例」传对**——把定义面与消费者面混同：short 域的 01.6、03.4–03.8 同样是 smoke 构建、同样能解析这对旗标，拒给它们没有依据；而"谁消费"在合同里没有字段可依。
3. **每次运行扫源码反推 `flag.Int("seed"`**——把一条静态编译约束变成运行期文本猜测，`//go:build` 的判断会退化成 grep 文字。

### 四、自测与红绿（逐条可复跑）

| 命令 | EXIT | 实际结果 | 红绿归属 |
|---|---|---|---|
| `python3 scripts/scenarios/test_run_runtime_scenarios.py -v` | 0 | Ran **40** tests OK（39→40） | 新增 `test_seed_window_only_reach_defining_package` |
| 同上，把声明面的 `tags` 清空（＝不看构建标签） | 1 | **1 red**：无 tag 的 wechat 组也被判承载，严格替身返回 exit 1／unattested | 变异对准"构建标签"这条轴 |
| 同上，把声明面整表清空 | 1 | **1 red**：无承载者，承载调用数 0≠期望 | 变异对准"这对旗标该交给谁" |
| 复原后 | 0 | 40 OK | 两个变异演示均逐值复原，不留痕 |

新案四段判据：①**声明与真源对齐**（`examples/wechat-bot/smoke_test.go` 首行 `//go:build smoke`、`flag.Int("seed"`／`flag.Int("window"` 仍在，声明面含 `("examples/wechat-bot", ".", ("smoke",))`）；②**命令形状**（承载调用每轮 1 条、非承载 4 条，承载命令尾部恰为 `-json -args -seed=7 -window=32768`，无 tag 的 wechat 命令不带 `-args`）；③**证据记录**（每轮 3 条 invocation 的 `packages` 与 argv 逐字一致，`-args` 只出现在 tags 含 smoke 的那条）；④**负控**（把 `event_commands` 换回 HEAD 形态，用"像 go 一样解析旗标"的替身喂同一 contract，判 exit 1／unattested）。

### 五、真树红→绿（同一命令形态，只差这一处）

| 运行 | 结果 |
|---|---|
| HEAD 形态（运行期把 `event_commands` 换回"单调用＋对每包追加"；`--scope event-short --test-id 01.10a/TestEventIntegrityExecLifecycle --seed 1 --window 256 --repeat-index 1 --timeout 600 --out /tmp/r282-prefix`） | EXIT **1**、`verdict=unattested`、`repeats: passed=0/10`；test-log 内 `flag provided but not defined: -seed` 后随 `fail` |
| 修复后（`--scope event-short --seed 1 --window 256 --repeat-index 1 --timeout 600 --out /tmp/r282-runner`，29 用例真跑） | EXIT **0**、`verdict=match`、`repeats: passed=1/10`、run_id `f0185bc9-…`；4 条 invocation 依序 `exit=0`（根模块 7 包／cli-host／wechat 无 tag／wechat smoke），只有 smoke 那条带 `-args` |
| `python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --repo-root . --evidence /tmp/r282-runner/repeat-1/evidence.json --json` | EXIT **0**、`ok=true`、29 test／135 anchor／9 包／180 事件行、`errors: []` |

`--repeat` 与 `--repeat-index` 的关系（R271 登记的第二条）本叶不改也不该改：合同 `event-short` 的 `repeat` 是 10，`--repeat 1` 会让产物自报 `repeat_total=1` 并被消费者判「repeat_total 1 does not match contract scope 'event-short' repeat 10」；分片形态写 `--repeat-index I`（`repeat_total` 仍取合同 10）。CI 的 short 分片不传 seed、long 分片传 seed 而 long 只选 wechat 的 smoke 构建且 `repeat` 本就是 1，故生产链两处均不受本缺陷影响——本叶修的是「09.R3 矩阵前置面的可用性」。

### 六、未验证面

1. **`--scope event-long --seed … --window …` 真跑未在本叶重跑**（六长需授权、6270 s/片）：其命令形态由 §二 的构造与 §四 ②③④ 段覆盖，真产物归 09.R3。
2. **同一 (dir, tags, race) 组里同时含承载包与非承载包**的拆分分支今天不存在于合同，故只在自测中验（root 组 7 包非承载、wechat 组单包承载）；将来若出现，承载调用与非承载调用各跑一次该组的 `-run` 选择器。
3. **`EVENT_CALIBRATION_FLAGS` 是 runner 对"旗标定义在哪个构建里"的静态声明**：正确性由自测读真源文件＋真跑共同钉住；若 `smoke_test.go` 改构建标签或改名，`//go:build smoke` 断言先红。

## 十六、R2d 执行落地（R294 R35 证据生产者两项坐实项的修复）

### 一、两处缺陷的机理

| 缺陷 | 现树事实 | 坏在哪 |
|---|---|---|
| 变异锚失配 | `12.R2.5` 后 sink 为「`closeFinished()`→`retainNoticeOwed`；`if refusal := r.loop.Backflow(...); refusal != nil { return r.retainNoticeOwed(...) }`；否则 `return r.store.Consume(...)`」。旧锚 `r.loop.Backflow(...); return r.store.Consume(...)` 命中 **0** 次 | `validate_registry` 报一条 stale ⇒ 自测 2 条确定性红，且注册表说的变异在现树不存在（假证据而非弱证据） |
| race 豁免掩盖业务失败 | `go` 的 `testing` 包对**被测出竞争的那个测试自己**记 `race detected during execution of test`（`$GOROOT/src/testing/testing.go:1865`，同处注释明写父级不再重复上报），且 race 报告本身也归到该测试的输出 | 旧判据只核「全篇 race frames 全部匹配已登记上游签名」，不读这条逐测试归属；于是「普通业务断言失败 ＋ 同批次可豁免 race」直接 `green`，控制树绿了而它其实断言过 |

### 二、修法与被否方案

- **锚重指**：锚改为现行拒收分支整块；补丁形态改为「先 `Consume`（err 即返）→ 再 `Backflow`（拒收仍交 `retainNoticeOwed`）→ `return nil`」。变异语义不变（`confirm_before_handover`）：把完成消费记在交接之前，交接窗口内的停机就让通知不再欠着。判据一条未改（锚须恰命中 1 次、补丁须进被测编译面、killed 须由具名断言命中）。
- **race 判据**：`classify_control` 的豁免条件由 `_race_waived(result)` 收紧为 `_race_waived(result)` **且** `_failures_beside_the_race(result)` 为空。逐测试归属：失败的 case 自己的记录里带那行 marker 才算被 race 解释；聚合父级自身不记（`testing` 有意如此），由其失败子级全被解释而解释；任何没有 marker 的失败 case ⇒ `red/test_failed`。
- **被否①：一刀切禁 race 豁免**（`_race_waived` 直接不用）。那会误杀合法的上游 race 控制——登记的上游签名今天就会在控制树里冒报告，禁掉等于让门在别人修上游之前永久红。
- **被否②：只核「race 报告出现在失败测试的输出里」**。这只证明该测试的输出里出现过报告，不证明**这次失败是它造成的**：一个先断言后踩雷的 case 同样满足，仍会掩盖业务失败。`testing` 自己的 `c.Errorf("race detected during execution of test")` 才是「它就是失败原因」的字段。
- **被否③：靠字形识别断言失败行**（`file.go:NNN:` 前缀）。`t.Logf` 与 `t.Errorf` 打出来的行同形，用它判会把「顺手打日志的合法 race 控制」判红，正是要避免的误杀。

### 三、自测与红绿（逐条可复跑）

| 读数 | 结果 |
|---|---|
| 修前（原树）`test_check_event_integrity_mutations.py` | EXIT **1**、`Ran 51 tests`、`FAILED (failures=2, errors=1)`：两条红均为 `MUT-CAND-01.2… stale mutation, the patch anchor occurs 0 times in assembly/facts.go`，error 是 git-index 环境故障（见 §五） |
| 修后（同命令） | EXIT **0**、`Ran 60 tests`、`OK`（51→60＝新增 5 例 ＋ `ClosureCliTest` 原被锚漂移挡在 `setUpClass` 的 4 例恢复执行） |
| `validate_registry('.', MUTATIONS)` | `[]` |
| 红绿①：`classify_control` 去掉 `and not _failures_beside_the_race(...)` | `ControlTreeFaceTest` 7 例中 **2 红**（`test_control_asserting_beside_a_waived_race_is_red`、`test_control_with_an_asserting_case_beside_a_raced_case_is_red`），纯 race 那例仍绿 ⇒ 判据对准的正是这两类形态，不是同义重复 |
| 红绿②：登记锚回退成旧裸 `Backflow`+`Consume` | 全量 **3 红**（`test_registry_accepts_a_literal_beside_a_shared_log`、`test_control_tree_green_then_…`、新 `test_the_registered_anchor_is_the_refusal_branch_of_the_sink`）；另有 `ClosureCliTest` 因夹具「stale 锚→把产品文件回滚到 HEAD」的修复回路级联出 `MUT-CAND-01.8-storage-success` stale，属演示副产物，复原后不复现 |
| 复原 | 两次演示后 `assembly/facts.go`、两门脚本 sha256 与演示前逐字节一致（`6c0d5f01…3119`／`cb23910a…7ecb`／`1aabd49b…a50b`） |
| 相邻门 | `test_check_runtime_evidence.py` EXIT **0**、116 OK；`test_check_dod_coverage.py` EXIT **0**、`51/51 cases passed` |

### 四、真跑（消费者锚）

`python3 scripts/gates/check_event_integrity_mutations.py --mutation MUT-CAND-01.2-handover-confirm --out /tmp/r2d-mut --json`（两次同判）：EXIT **0**、`control_tree_green=true`、control `TestEventIntegrityConsumptionRecovery` `green/exit 0`；变异 `verdict=killed`、`reason=named_assertion`、`counted_killed=true`、`assertion_hit=true`；`run.jsonl` 内失败用例 `TestEventIntegrityConsumptionRecovery/handover_before_confirmation` 打印 `not recoverable after the restart: …`，即既有命名断言杀死了新锚。两次运行前后 `assembly/facts.go` sha256 不变 ⇒ 补丁应用与复原逐字节一致。

### 五、环境故障（必须上报，已隔离未修复）

本机 `.git/index`、`.git/HEAD` 是 iCloud 逐出占位：`ls -lO` 报 `dataless`、`stat` blocks=0，`bird` 与 `iCloudDriveFileProvider` 在跑，CloudDocs 上传报 `Quota exceeded`。直读 `TimeoutError [Errno 60]`；`HEAD` 一度读出 0 字节，使 `git rev-parse --git-dir` 直接报 `not a git repository`；`brctl download` 只救回 `HEAD`，`index` 持续 dataless。后果：`git ls-files` 失败（`fatal: .git/index: unable to map index file: Operation timed out`），`ClosureCliTest` 与真跑的 `source_files` 一并失效。本轮以 `GIT_INDEX_FILE=/tmp/r2d-index`（`git read-tree HEAD` 在**仓外**生成，488742 字节与原 index 同尺寸）临时恢复取文件表能力——判据零改动、零 git 写；等价性是「同尺寸＋`git status` 语义一致」的间接证据，真 index 今日不可读无法逐字节对照。仓库 `.git` 元数据目前不可读，**任何 git 操作（含 commit/archive）都会失败，须编排者处置**。

### 六、未验证面

1. 真跑只覆盖 `MUT-CAND-01.2` 单变异；全量 `--all`（17 件套＋控制树）未跑，需长跑授权，归 13.R4。
2. 新 race 判据的全部证据是合成 `go test -json` 夹具，加上「真跑里 control 全绿」这一侧；「真实 waived race ＋ 普通业务断言失败」的样本今天在生产里不存在，真样本出现前该半面仍是构造证据。
3. `GIT_INDEX_FILE` 隔离的等价性未与真 index 逐字节比对（见 §五）。


## 十七、R2e 执行落地（R294 R35 四项消费者孤儿闭合，2026-11-18）

### 一、四处孤儿：生产者写了，消费者没读

| 孤儿 | 现树事实 | 后果 |
|---|---|---|
| seed/window 未核 argv | `check_event_invocations` 只核 `-run`/`-tags`/`-race`/`-count`/`-json`/`-timeout`/`-C`，`run_config.seed`/`window` 自报即算 | 删掉 `-args -seed/-window`（测试根本没收到校准）或把对广播给非承载包（那些包在解析旗标时即退出），证据仍可绿 |
| `gate_tool` 身份无人读 | `check_event_integrity_mutations.py:2301` 写 `gate_tool`（entrypoint/digest），`check_mutation_baseline` 从不读 | 换一版门写下的判词（不同变异集/词表）与原树同权计入 |
| 判词一致性无人读 | `check_mutation_evidence` 只核 `status=="killed"`，`verdict`/`counted_killed`/`assertion_hit` 不核（`assertion_hit` 仅「非 None 时须 true」，缺省放过） | 自相矛盾记录（`status=killed`＋`verdict=not_killed`/`counted_killed=false`）被当作杀死计入 |
| DoD 只绑 source_binding | `run_verdicts`→`bind_mutation_evidence` 只调 `check_event_source_binding` | `control_tree_green=false`、`not_evidence_backed` 非空、结构非法的报告照样把判词递进 DoD |

### 二、修法与单一来源

| 缺陷 | 落点 | 判据 |
|---|---|---|
| seed/window 实际 argv | 新增 `check_event_matrix_args()`，由 `check_event_invocations` 每次调用调起 | 承载身份问生产者：`runner.event_calibration_bearer(dir, tags, package)`；argv 命名的包用生产者自己的 `runner.command_packages(tokens)` 反读（不信 invocation 自报的 `packages`）；「可带对」＝**命名包全为承载者**。`run_config.seed` 非空时承载调用必须带 `-args` 且 `-seed`/`-window` 值等于声明；非承载调用（含部分承载的混合调用）与「运行未声明对」一律不得带 |
| `gate_tool` 身份 | 新增 `check_mutation_gate_tool()`，由 `check_mutation_baseline` 在 `check_event_source_binding` 之后调起 | 结构须为对象且 `entrypoint` 非空、`digest` 可读 sha256；`--repo-root` 现树内该路径须存在且 `lib.sha256_file()` 与 digest 相等（沿 `source_binding.tool` 的先例：同一比较目标是一棵树，不是另一份产物的自述） |
| 判词一致 | `check_mutation_evidence()` 逐记录增判 | `verdict=="killed"`、`counted_killed is True`、`assertion_hit is True`（原「缺省放过」收紧为必真）；`not_evidence_backed` 非空仍拒（保留） |
| DoD 可消费 | `check_dod_coverage.bind_mutation_evidence()` | 经 `_GapReport` 适配，先 `evidence_consumer().check_mutation_evidence()` 全量结构校验、再 `check_event_source_binding()`；任一 gap 即整份 refuse，`run_verdicts` 不为该产物产出任何判词，缺口经 `gaps.extend(refused)` 具名 |

**为何不复制第二公式**：承载面与包列表分别由 `event_calibration_bearer()`、`command_packages()` 提供，二者是 R2c 已冻结的生产者定义面（`EVENT_CALIBRATION_FLAGS`），消费者只问不重写；「`-seed=<值>` 的写法」属旗标自身的 CLI 语法，不是对"哪次调用承载"的第二次推导。`run_config.seed` 的取值合法性仍由既有 `check_event_run_config` 绑合同 matrix，本叶不重算。

### 三、被否的方案

1. **在消费者里按 `module_path`/`tags` 重写一份承载表**——第二真相：`smoke_test.go` 改构建标签时，两张表可以各自绿着说不同的话；R2c 的自测（读真源 `//go:build smoke`）只钉住生产者那张。
2. **整体 argv 等值比对**（重推 `event_commands` 后与记录串逐字相等）——会把既有 6 条既有判据（`-timeout` 宽松上界、包覆盖并集等）压成一条全等断言，红因难归因；本叶只补孤儿面，不改既有判据的粒度。
3. **把 `gate_tool` 校验放进 `check_mutation_evidence`**——那里没有 `--repo-root`；与树比对的判据必须与 `source_binding` 同处（`check_mutation_baseline`），否则会退化成"证据间一致"的自证。
4. **DoD 侧再写一份结构校验**——正是 R294 记的「只绑 source_binding」的镜像错误；复用消费者并只做 `_GapReport` 适配。

### 四、自测与红绿（逐条可复跑，EXIT 为原始退出码）

| 命令 | EXIT | 实际结果 | 红绿归属 |
|---|---|---|---|
| `python3 scripts/gates/test_check_runtime_evidence.py` | 0 | Ran **126** tests OK（116→126） | 新增 10 例：承载删参、非承载广播、值不符、无对运行带参、`gate_tool` 缺省/假路径/零 digest、判词三矛盾 |
| `python3 scripts/gates/test_check_dod_coverage.py` | 0 | **54/54 cases passed**（51→54） | 新增红控制树/非空 `not_evidence_backed`/记录自相矛盾三例，每例同时要求「registered and unproven」＝判词确实没被计数 |
| `python3 scripts/gates/test_check_event_integrity_mutations.py` | 0 | Ran **60** tests OK | 相邻面（R2d 生产者）不回退 |
| `python3 scripts/scenarios/test_run_runtime_scenarios.py` | 0 | Ran **40** tests OK | 相邻面（R2c runner）不回退 |

**夹具订正（非判据放宽）**：`test_check_runtime_evidence.event_invocations` 原来自写一份命令推导，且把 `-args` 对**广播给每次调用**——与生产者 R2c 后的实际形态不符（`flag provided but not defined` 会先炸）。改为当场调 `runner.event_groups()`／`runner.event_commands()`／`runner.command_packages()` 推导；随之无人引用的 `event_package_arg` 一并删除。DoD 夹具 `run_product` 补 `assertion_hit`、`not_evidence_backed` 两个生产者必写字段。原"存活判词"用例期望串由 `is recorded 'not_killed'` 改为 `status 'not_killed' is not 'killed'`：整份报告被结构拒收后不再产出逐条判词文案，红因仍是那条存活判词。

### 五、真产物红→绿（消费者锚）

| 运行 | 结果 |
|---|---|
| `python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --seed 1 --window 256 --repeat-index 1 --out /tmp/r2e-full` | EXIT **0**、`verdict=match`、`repeats: passed=1/10`、run_id `bcef5d71-0040-43d1-bda7-98aa1ed36753`；4 条 invocation 依序 `exit=0`（根模块 7 包／cli-host／wechat 无 tag／wechat smoke），**只有 smoke 那条带 `-args -seed=1 -window=256`** |
| `python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --repo-root . --evidence /tmp/r2e-full/repeat-1/evidence.json --json` | EXIT **0**、`ok=true`、`errors: []`、29 test／135 anchor／9 包／180 事件行——与 R271 同一读法，新判据不误杀真产物 |
| 同上，把 smoke 那条的 ` -args -seed=1 -window=256` 删掉（真产物形态负锚） | EXIT **1**、`run_config.invocations[3]: … yet the argv carries no '-args'; the pair the run declares never reached the tests` |
| 同上，把同一对追加到 cli-host 那条（真产物形态负锚） | EXIT **1**、`run_config.invocations[1]: … which do not all compile the file declaring those flags … exits on 'flag provided but not defined' before any test runs` |

> 注：R294 记的既有产物 `/tmp/r282-runner/repeat-1/evidence.json` 今日已 stale（R2d 等叶改过源树，现树重算 digest 不符，属正确拒收），故按叶行允许的替代路径用 runner 真跑产物作消费者锚。

### 六、未验证面

1. **long 侧真跑未在本叶执行**（六长需授权、约 6270 s/片）：其 argv 形态由自测与生产者构造覆盖，真产物归 09.R3。
2. **同一组内混合承载/非承载包**今天在合同里不存在，故该分支（`parses_pair` 为假且带了 `-args`）的真样本仍只有构造证据；判据按"全为承载者"取真，将来出现时按混合拒绝。
3. **全量 `--all`（17 件套＋控制树）未跑**，归 13.R4。

## 十八、R2f 执行落地（`MUT-DOD-P5-legacy-clause` 锚随规格退位重指，R316 待裁② 裁A，2026-11-19）

### 一、缺陷机理：锚与被期望的标题同随主 spec 退位而消失

P5 的门锚变异用一处精确文本替换向主 spec 要求正文注入 legacy 条款（一个 `.go` 文件名），期望 `check_spec_hygiene.py` 的 R4（`go-file`）具名拒绝。原锚打在 `openspec/specs/event-lifecycle/spec.md` 的 `for age calculation.`（原「Time-based TTL expiration」要求），而 10.R5b 已把该 spec 整体标为 superseded、五条要求合并为一条「历史条款已退役」：`grep -c "for age calculation" openspec/specs/event-lifecycle/spec.md` → **0**。锚 0 命中 ⇒ runner 判 `not_applied`（既非 killed 也非存活），门本身变成空转。这是「规格退位的级联」而非变异选错形态——变异要证明的性质（legacy 条款注入被拒）仍在位，缺的只是承载它的那条要求。

### 二、重指落点与语义保持

| 项 | 原值 | 现值 |
|---|---|---|
| 目标文件 | `openspec/specs/event-lifecycle/spec.md`（superseded） | `openspec/specs/storage-lifecycle-completion/spec.md`「保留保护与回收协调」（在位，`spec.md:13`） |
| 补丁锚 | `for age calculation.` | `只拒销毁，不得以「先标墓碑、压实再删」的形态复活。`（文件内恰 1 次命中） |
| 注入句 | `The TTL scanner lives in ttl_scanner.go.` | `TTL 扫描由 ttl_scanner.go 内的周期性全量扫描实现。` |
| 期望断言 | `R4 go-file in requirement 'Time-based TTL expiration'` | `R4 go-file in requirement '保留保护与回收协调'` |

**为何是这条要求**：10.R5b 把退役生命周期扫描的唯一仍在位承诺（任何路径不得物理销毁未确认恢复所需的原文等）交由 `storage-lifecycle-completion` 的这条要求唯一承载，它同时是那条退役机制的**禁入条款**（「TTL 扫描、容量淘汰、墓碑标记与物理回收 MUST NOT 作为运行期销毁路径存在」）。把 `ttl_scanner.go` 的实现细节重新写进这条要求的正文，就是字面意义的 legacy 条款复活——比另选一条与机制无关的要求更能保住门锚语义（门判的仍是「退役条款以实现细节形态回流」）。注入句只含一个 `.go` 名，故变异仍只触发 `go-file` 一条规则（真跑 `violations` 250→251、`errors=1`）。

### 三、被否的方案

1. **降级为 `pending` 或把 `must_be_killed` 改假**——用户已裁 A「不降级变异」：P5 要证明的是门拒绝 legacy 条款的能力，降级等于承认该性质无人证明。
2. **删 `golden/runtime/dod_predicates.json` 的 P5 谓词或抬 `spec_hygiene` baseline**——前者抹掉判据，后者把违规当已审基线；两者都把「门能拒」换成「门不必拒」。
3. **回改 `event-lifecycle` 主 spec 复活旧要求**——与 10.R5b 的退役裁定冲突，且让已退役机制重新成为权威正文。
4. **改成英文注入句 + 英文断言**——锚语义可保，但目标要求正文是中文，注入处只能是中文句；断言字符串随 `expect_assertion` 的模板片段（`" in requirement "`）与 gate 源码绑定，语言不影响 `gate_states_literal` 的判定，故无理由再动目标。

### 四、自测与红绿（逐条可复跑，EXIT 为原始退出码）

| 命令 | EXIT | 实际结果 |
|---|---|---|
| `GIT_INDEX_FILE=/tmp/r2d-index python3 scripts/gates/test_check_event_integrity_mutations.py` | 0 | Ran **60** tests OK；`validate_registry(DEFAULT_ROOT, MUTATIONS)` → problems **`[]`**（无 stale、无 gate site 问题） |
| `python3 scripts/gates/check_event_integrity_mutations.py --mutation MUT-DOD-P5-legacy-clause --out /tmp/r2f-mut2 --json` | 0 | 控制树 `check_spec_hygiene` EXIT 0（`violations=250 errors=0`）；变异 `status=killed`／`verdict=killed`／`reason=named_assertion`／`assertion_hit=true`／`counted_killed=true`／`control_tree_green=true`／`not_evidence_backed=[]`；变异树门输出 `…/spec.md:13: R4 go-file in requirement '保留保护与回收协调': …`、`violations=251 errors=1`、门退出码 1 |
| `python3 scripts/gates/check_dod_coverage.py --mutation-evidence /tmp/r2f-mut2` | 1 | **8 gap(s)**（P0-P4/P6-P8）；同命令不给产物时 9 条——P5 因该产物而不再「registered and unproven」，证明 DoD 的 P5 绑定真读这份判词 |
| `python3 scripts/gates/test_check_dod_coverage.py` | 0 | **54/54 cases passed**（不回退） |
| `python3 scripts/gates/check_doc_budget.py` | 0 | `RESULT: ok`（本叶改动只在 `scripts/` 与预算外的 `openspec/changes/**`） |

### 五、负锚（两类，均在真实判据上取）

1. **退役锚必须被判 stale**：以 `dataclasses.replace` 把条目换回原 `file`/`old`/`expect_assertion` 后交同一 `validate_registry` → `MUT-DOD-P5-legacy-clause: stale mutation, the patch anchor occurs 0 times in openspec/specs/event-lifecycle/spec.md`。故本条不是「任意新锚都能过」，退役形态被具名拒绝。
2. **期望标题必须与新要求绑定**：拿**真实门日志**（`/tmp/r2f-mut2/mutations/MUT-DOD-P5-legacy-clause/run.log`，退出码 1）交 `classify_gate`——用现行 `expect_assertion` 判 `killed`／`named_assertion`；换成退役的 `'Time-based TTL expiration'` 判 `not_killed`／`assertion_not_matched`（`assertion_hit=false`）。故标题改名不是文案调整，旧标题在新落点上不再计杀死。

### 六、未验证面

1. **全量 `--all`（17 件套＋控制树）未跑**（长跑需授权），归 13.R4；本叶只跑单变异加控制树。
2. **`golden/runtime/dod_predicates.json` 零改动是本叶的判断，不是遗漏**：P5 块只登记 `measure`/`scope`/`anchor.gate`/`mutation.id`，从不指名目标文件或要求标题，故重指不牵动绑定；`mutation_verdicts` 仍 `[]`（DoD 运行产物由 13.R4 的正式入口一次性产出）。
3. **`comment_policy` 两口径未复跑**：本叶只改 `description` 字符串（数据，非注释）与一处 patch 字面量，产品 Go 与脚本注释零改动。
4. **环境故障未修（必须上报）**：首次 selftest 的 `ClosureCliTest.setUpClass` 因 `git ls-files … fatal: not a git repository` 报错（Ran 56 ＋1 error，4 例因此未跑），同一命令以 `GIT_INDEX_FILE=/tmp/r2d-index` 重跑即 Ran 60 OK。与 13.R2d 登记的 iCloud 逐出 `.git` 元数据故障同源；判据零改动、零 git 写，故障本体交编排者处理。

**13.R3b 复验（2026-11-18，逐条重跑，EXIT 为原始退出码）**：`python3 scripts/gates/test_check_runtime_evidence.py` → EXIT 0、Ran **126** tests OK；`python3 scripts/gates/test_check_dod_coverage.py` → EXIT 0、**54/54 cases passed**；`python3 scripts/gates/test_check_event_integrity_mutations.py` → EXIT 0、Ran **60** tests OK（74.418 s）；`python3 scripts/scenarios/test_run_runtime_scenarios.py` → EXIT 0、Ran **40** tests OK；§五 的 run 产物 `check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --repo-root . --evidence /tmp/r2e-full/repeat-1/evidence.json --json` → EXIT 0、`ok=true`／`errors: []`／29 test・135 anchor・9 包・180 事件行（本次改动落在 docs、`golden/runtime/dod_predicates.json` 与脚本夹具，不在该产物的闭包内，故旧产物仍绑现树）。四缺陷的代码落点在本次复验中逐条在位：`check_event_matrix_args`（:2327，由 `check_event_invocations` :2460 调起）、`check_mutation_gate_tool`（:2825，:2879 调起）、`check_mutation_evidence` 的 `counted_killed`/`assertion_hit` 必真（:2806/:2814）、`check_dod_coverage.bind_mutation_evidence` 调 `check_mutation_evidence`（:659）。

## 十九、R4a 执行落地（mutation 全量真跑与 DoD 运行证据，2026-11-19）

### 一、跑法与产物

`--all` 在隔离副本树上驱动 23 条启用变异（17 条测试变异＋2 条门锚变异＋4 条折叠变异）与 19 项控制（17 个 go 控制测试＋`check_spec_hygiene.py`／`check_dead_producers.py` 两个门控制）。产物 `/tmp/r4a-mut2/evidence.json`：`mode=live`、`profile=event-integrity`、`git_head=0f84b2d1…`、`contract_digest=sha256:e1dfb96b…`、`source_binding.digest=sha256:93cc349d…`（`before`＝`after`，单模块 `.`／`roles=[tested]`／无 local replace 边）、`gate_tool.digest=sha256:363fe485…`、python 3.12.8／go1.27.1。gate 原始退出码 **0**、`gate.ok=true`、`failures=[]`、`not_evidence_backed=[]`。

### 二、逐变异判词

23/23 均为 `status=killed`／`verdict=killed`／`assertion_hit=true`／`counted_killed=true`：`MUT-EVID-02-dual-material-input`、`MUT-EVID-03-busy-notice-drop`、`MUT-G1-meditation-busy-drop`、`MUT-CAND-01.2-early-confirm`、`MUT-CAND-01.1-conflicting-body`、`MUT-CAND-02.2-wrong-target`、`MUT-EVID-04-position-boundary`、`MUT-CAND-01.8-storage-success`、`MUT-CAND-01.2-handover-confirm`、`MUT-CAND-01.10-alive-process-complete`、`MUT-FOLD-01..04`、`MUT-DOD-P0..P8`。无 `not_applied`、无 stale：`run_gate` 以 `validate_registry` 为前置，锚 0 命中或门锚不成立时直接 `GateError`，本次未触发。

### 三、DoD 消费与两类负锚

| 命令 | EXIT | 结果 |
|---|---|---|
| `check_dod_coverage.py --mutation-evidence /tmp/r4a-mut2` | **0** | `9/9 criteria bound and proven`（P0-P8 全闭，R3a 前的 9 gap 归零） |
| `check_dod_coverage.py --mutation-evidence /tmp/r4a-mut`（首跑红控制产物） | **1** | **11 gap(s)**：`the control tree must be green before a mutation counts`、`mutations must be a non-empty list` ＋ P0-P8 九条 unproven |
| `check_dod_coverage.py`（无产物） | **1** | 9 gap(s)，九条「registered and unproven」 |

首跑的 `gate.failures` 里另有 `confirmed defect EVID-02/EVID-03/EVID-04 … must map to exactly one mutation, found 0` 三条 —— 那是 `evaluate_gate` 按 `records`（而非 `selected`）核确认缺陷映射，0 变异执行时必然为 0 命中，故它是红控制的下游症状，不是映射缺陷：注册表侧 `MUT-EVID-02`→`EVID-02-dual-material-input`、`MUT-EVID-03`→`EVID-03-busy-notice-loss`、`MUT-EVID-04`→`EVID-04-notice-visibility-loss` 单射成立，重跑全绿后这三条同时消失。

### 四、bundle 现树绑定（本叶证明面）

bundle＝`/tmp/r4a-bundle`：`event-short/repeat-1/{evidence.json,test-log.jsonl}`（新鲜短跑 `--scope event-short --seed 1 --window 256 --repeat-index 1`，`run_id=77d2965c-bb9e-4f00-b858-9ed2e3fa4fc2`、`verdict=match`、`repeats: passed=1/10`）＋`mutations/evidence.json`（`/tmp/r4a-mut2`）。

`check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --repo-root . --bundle /tmp/r4a-bundle --json` → EXIT **1**，4 条错误**全为矩阵完整度**（short 1/10、`repeat_index` 覆盖 `[1]`≠1..10、long 0/6、六组合缺），`counts={bundle_evidence_files:2, bundle_short_evidence:1, bundle_long_evidence:0, bundle_mutation_evidence:1, event_log_tests:180, event_log_packages:9}`；闭包/现树指纹/gate_tool/判词一致类错误 **0 条**。同短跑单份 `--evidence` → EXIT **0**／`ok=true`／`errors: []`／29 test・135 anchor・9 包・180 行。**整包（十短/六长）归 09.R3**：本叶只证 mutations 与 short 的绑定面成立，六长未跑，故整包绿不得在本叶宣称。

### 五、自测不回退

`test_check_event_integrity_mutations.py` EXIT 0／Ran **60** OK（79.103 s）；`test_check_dod_coverage.py` EXIT 0／**54/54 cases passed**；`test_check_runtime_evidence.py` EXIT 0／Ran **126** OK。

### 六、环境故障与未验证面

1. **首跑 `--all` 的 P4 控制红（加载态墙钟抖动，非树状态）**：`TestSymptomAnchorBootCostFlatAcrossGenerations` 报 `the wall clock of the assemble grows with the history: the boot of generation 4 took 40.74075ms beside the 18.870875ms of generation 1, over the bound 37.74175ms of 2 times the second boot`（`wallClock=[22.39 18.87 26.20 28.70 40.74]ms`），判 `red/test_failed` ⇒ 0 条变异执行、gate 以 `no mutation was run` 收尾（EXIT 1，产物留在 `/tmp/r4a-mut`）。同一用例在本树单跑 ×3 全绿（8.156 s／8.529 s／8.447 s），机器空闲后重跑 `--all` 全绿 ⇒ 归因机器负载，非树回归；两次跑的判据、产品代码、注册表零改动。**该墙钟判据（P4 的 `bound.factor=2` 打在 18-40 ms 量级）在高负载机上可复现红，属判据面设计问题**：改造需要动产品测试（`assembly/symptom_anchor_recovery_test.go`）与 P4 谓词，超出本叶写面，登记待裁。附带事实：一次红控制即让整份报告不可用（26 条判词全部归零），故 13.R4 终验若在 CI 并发负载下重跑，仍有同样风险。
2. **六长真跑未跑**（六组合×6270 s 上界），`--bundle` 全绿与整包完整度归 09.R3；`bash scripts/lint.sh`／`bash scripts/preflight.sh` 全链未真跑，本叶只证 DoD 与绑定消费面。
3. **`.git` 元数据仍是 iCloud 逐出占位**：全程用 `GIT_DIR=/tmp/ri-gitdir GIT_INDEX_FILE=/tmp/r2d-index`，零 git 写；故障本体交编排者。

## 二十、R3 执行落地（公共拒绝入口与归档/合并接入，2026-11-19）

### 一、落地形态：三判合一，接在「证据从这里来」的作业上

归档这件事的判据有三条，缺一条就不该放行：**证据绑不绑这棵树**、**DoD 判词来自运行产物还是登记表**、**主规格归档后是否仍 strict**。§四 的三行入口因此各归其位：

| 入口 | 落点 | 消费什么 |
|---|---|---|
| 本地链 | `scripts/gates/preflight.sh`（R3a 已排好生成→绑定→消费；本叶零改动） | 本次运行目录 `$event_out`，末尾 lint 的 DoD 段经 `TAGENT_RUNTIME_EVIDENCE_DIR` 读 `$event_out/mutations` |
| CI 必跑作业 | `ci.yml` 的 `test` 作业：`needs: [event-integrity-mutations]` ＋ `if: always()` ＋ 下载 artifact 到 `${{ runner.temp }}/dod/mutations` ＋ `Lint` 步 `env: TAGENT_RUNTIME_EVIDENCE_DIR: ${{ runner.temp }}/dod` | 本次运行上传的 mutation 产物本体（`--out` 目录即 `mutations/` 的内容） |
| 归档 | 新增 `scripts/gates/archive-preflight.sh`：证据门 → DoD 门 → `check-openspec.sh`，各自留原始退出码；`--commit REV [--base]` 从候选提交实算新增归档路径 | 同一份证据目录（`--evidence` 或 `TAGENT_RUNTIME_EVIDENCE_DIR`）＋ 候选提交的新增路径 |
| 合并 | 外部 `required_status_checks` | **今日 `ABSENT`，本叶零改动**（须明示授权） |

两处设计取舍值得写下：**为什么 DoD 消费放在 `test` 作业**——`golden/runtime/dod_predicates.json` 九条谓词的 `ci_gate` 全是 `test`，把消费挪进 `event-integrity-required` 或新作业会让这九条绑定变成假话，而改 golden 不在本叶写面；代价是 `test` 串在变异作业之后，关键路径约加该作业时长。**为什么 `always()`**——被跳过的作业在必需检查里显示为绿，而"上游失败所以没有产物"必须在本作业红（下载失败即红），不能以跳过代替。

### 二、与计划差异与被否方案

| 项 | 计划 | 实际 | 理由 |
|---|---|---|---|
| `scripts/gates/preflight.sh` | §四「追加证据拒绝一步」 | **未改** | 该链已按 `set -euo pipefail` 在 `event evidence binding` 步对非零证据中止，且末尾 lint 内含 DoD 与 openspec；再插一个包装步会在同一次命令里把三条判据各跑第二遍，不增判据只增时长。归档时点的问题是独立一问，归 `archive-preflight.sh` |
| 归档包装 | 「包装脚本或 preflight 步骤」 | 只做**门**（不代跑 `openspec archive`） | 代跑会把"归档"这一动作藏进脚本；门只回答"能不能归档"，`openspec archive <change>` 仍由人显式执行，判据与动作分离 |
| 绕行检测 | 「提交检查及服务端必需检查」 | `--commit` 模式 + 如实登记未安装 | 本地脚本拦不住不调用它的命令；把判据挂在候选提交的实际新增路径上是唯一不靠自觉的面，调用方（hook／必需检查）须授权后配置 |
| 六反例的构造 | 拟新增 fixture | 用 `/tmp/r3neg/*` 硬链副本**只改一处** | 冻结证据仍合法，反例的红只能来自被改的那一处；不新造"像证据"的合成字典 |

### 三、逐条可复跑（原始退出码，独立语句）

| 命令 | EXIT | 结果 |
|---|---|---|
| `bash scripts/gates/archive-preflight.sh --evidence /tmp/r333-bundle` | **0** | `1/3` 绑定齐、`2/3` `9/9 criteria bound and proven`、`3/3` `OK: all main specs pass validate --strict` |
| `… --evidence /tmp/r333-bundle --expect-rev HEAD` | **0** | `evidence git_head 0f84b2d…, candidate 0f84b2d…` |
| `… --commit HEAD --evidence /tmp/r333-bundle` | **0** | `adds no path under the archive tree; nothing to judge` |
| `… --commit ec03877… --evidence /tmp/r333-bundle` | **1** | `adds 74 archived path(s)` ＋ `the run products describe revision 0f84b2d…, not ec03877…` |
| DoD 段（`lint.sh:148-159` 段体逐字单跑）＋合法产物 | **0** | `9/9 criteria bound and proven` ＋ 自测 `54/54 cases passed` |
| 同上 ＋ 变量未设 | **1** | `TAGENT_RUNTIME_EVIDENCE_DIR is unset; the DoD gate counts mutation run products and there is no evidence directory to read` |
| 同上 ＋ 目录无 `mutations/` | **1** | `the evidence directory … holds no mutations/ product` |
| 同上 ＋ 手填 killed | **1** | `not counted as killed … cannot count here`；DoD 由 9/9 转 `11 gap(s)` |
| `python3 scripts/gates/check_dod_coverage.py --mutation-evidence /tmp/r333-bundle/mutations`（改 `ci.yml` 后复跑） | **0** | `9/9`（证明作业名解析未被接线破坏） |
| `python3 scripts/gates/test_check_dod_coverage.py` | **0** | `54/54 cases passed` |
| `python3 scripts/gates/check_runtime_evidence.py --bundle /tmp/r333-bundle --repo-root .` | **0** | `ok=true`、`errors: []` |
| `bash scripts/gates/check-openspec.sh` | **0** | `OK: all main specs pass validate --strict.` |
| `openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |
| `GOFLAGS=-buildvcs=false go run ./scripts/codetools proc-refs scripts .github/workflows` | **1** | 两处新改文件 **0 条** `PROCESS-ARTIFACT-CITE`；现存 1 条 `scripts/gates/test_check_no_prefix_criterion.py:24` 为外来 |
| `gh api repos/SpellingDragon/tagent/branches/main/protection --jq '.required_status_checks // "ABSENT"'` | **0** | **`ABSENT`** |
| `TAGENT_RUNTIME_EVIDENCE_DIR=/tmp/r333-bundle bash scripts/lint.sh`（全量） | **1** | 红在 DoD 段之前，归因见本节之五 |

### 四、六条归档/合并反例（每条只改一处，全部具名拒）

| # | 反例 | 构造 | EXIT | 具名拒绝（原文关键句） |
|---|---|---|---|---|
| 1 | 零测试 | 短跑 test-log 清空并重算 `test_log_sha256`（期望集合仍非空） | **1** | `no run/pass in test-log for …::TestEventIntegrityReproduction (zero match is a failure, even when the process exited 0)` |
| 2 | 跳过 | 一条顶层 `pass` 动作改 `skip` | **1** | `…::TestEventIntegrityApproval recorded skip in test-log` |
| 3 | 过期证据（源树指纹不一致） | `source_binding.modules[0].content_hash` 改全 0 | **1** | `module '.' content digest sha256:000… does not match this tree's sha256:cfc18188…; the evidence describes another source tree` |
| 4 | 错提交 | `--expect-rev HEAD^`，证据仍绑 `0f84b2d…` | **1** | `the run products describe revision 0f84b2d…, not 9547fac…: evidence from another commit cannot archive this one` |
| 5 | 手填 killed | 追加一条 `status=killed`／`counted_killed=False`／`assertion_hit=False` 的变异记录 | **1** | `is not counted as killed (counted_killed False); a kill the producer did not count cannot count here` ＋ `did not hit its named assertion` |
| 6 | 缺分片 | 删 `long-s42-w32768` | **1** | `bundle is missing event-long matrix combination(s) [(42, 32768)]` |
| 7 | 绕过包装（直接 `openspec archive -y`／`mv`） | `--commit <真实归档提交>` | **1** | 先列 `74 archived path(s)`，再以 `commit-identity` 拒绝；同一命令对不新增归档路径的 `HEAD` → EXIT **0** |

第 7 条同时说明判据的形状：`--no-renames` 让 `git mv` 必须以**新增**出现，故"移动目录"与"直接 archive"走同一条判据。**它可判，但目前不自动执行**——`openspec archive` 自己不核 DoD，唯一能自动拦住的是提交检查与必需检查，二者今日均未配置。

### 五、未验证面与外部权限（不冒认）

1. **CI 未真跑**：接线只经 YAML 解析（作业名 10 个不变、`needs`／`if`／`env` 读出）与本地同一 `env` 语义验证；`gh run view`／触发一轮须授权。且今日 `test` 作业的 DoD 段仍被两处**外来**红挡在前面：`comment_policy` 棘轮（`new violations relative to the baseline are not allowed`，R293/R294 已登记）与 `test_check_runtime_symptoms.SymptomCheckerTest.test_registry_matches_the_module`（已提交的 `TestLocalSmokeSymptomBootDeployed` 未登记进 `check_event_semantics_symptoms.SYMPTOM_PARENTS`，`Ran 316` 中 `failures=1`）。两处都不在本叶写面，未放宽任何判据去凑绿。
2. **`bash scripts/gates/preflight.sh` 全链未跑**（十短＋六长＋变异，需授权与约 30–60 min）；本叶未改该文件，"证据非零即中止"由既有 `set -euo pipefail` 与同三条判据继承。
3. **分支保护／必需检查／`core.hooksPath` 未配置**：`required_status_checks` 实测 `ABSENT`，pre-commit 未启用，故 `--commit` 模式目前无日常调用方；L4 的"合并侧拒绝面"须用户明示授权后再配置并读取实际保护范围与绕过权限。
4. **`docs/OPERATION.md` §5.1 过期**：其「CI 的 `test` 作业今天仍裸跑 `bash scripts/lint.sh`（`.github/workflows/ci.yml:109`）」已随本叶接线与行号变化失效；该文件不在本叶写面，登记待改。
