# 01 三挂点动态拓展 golden 守护 · 设计

> 判据来源：一级 design D6.15（openspec/changes/runtime-recovery-ideal/design.md:81）与 §五 共享面冻结（同文件:115）。本域只立判据与登记，**不改任何特性代码**；门禁在现树红是 W0 症状，转绿归 02-plugin-dynamic-mount。

## 1 不变量（三条，与 §五 逐字对齐）

1. **装配根无具体特性类型构造 / 无条件 append**：装配根对插件管道集合的成员增补，只允许「转发已解析集合」与「取声明/注册表解析结果」两种来源。
2. **特性经 plugins: 声明＋代际注册表挂载**：装配根按名字解析，不按具体类型构造。
3. **plugins: 声明有生产消费方**：声明名集必须到达 spec 并被逐项迭代求值，不是死字段。

判据不裁决「谁算特性」，只裁决「装配根是否按名对插件管道做了具体增补」——这条正是 §五 的表述，且避免自造第二份特性名单（ADR 0006 元规则二：结构不得自证）。

## 2 R5a 管道成员来源形态判据

### 2.1 扫描面

assembly/ 生产 Go 源（测试排除），注释与字符串先掩码——沿用 R2 的做法与同一组工具：scripts/gates/guard_checklib.py:38 `go_files`、:59 `strip_go_comments`、:107 `scan_go_strings`。讨论一个挂载的注释不是挂载。

### 2.2 管道集合由类型锚推导，不设登记名单

容器 = 类型为 `[]plugin.Instance` 的字段或局部：

- 字段形态 `^\s*(\w+)\s+\[\]plugin\.Instance\s*$` → assembly/run.go:282 `plugins`（装配根自身的管道集合，可达名 `r.plugins`；assembly/run.go:76 `NativeAgent.Plugins` 是宿主贡献**输入**，不作增补面）；
- 局部形态 `^\s*(\w+)\s*:?=\s*(?:append\(\s*\[\]plugin\.Instance\(nil\)|make\(\[\]plugin\.Instance,)` → assembly/tools.go:247 `elements`、assembly/host.go:690 `instances`。

派生即用，不登记：新增管道容器自动落入范围（登记名单会让新容器静默漏扫）。推导为空即红——类型锚消失说明判据失锚，不许静默通过。

### 2.3 四种元素实参形态

| 形态 | 判据 | 现树实例 |
|---|---|---|
| 合规 A 展开转发 | 元素实参以 `...` 结尾（转发另一已解析集合） | assembly/tools.go:247 `contributed.Plugins...`、:248 `r.plugins...` |
| 合规 B 整赋值取解析结果 | `<container> = <expr>`，`<expr>` 非 append | assembly/run.go:612 `r.plugins = instances`（`instances` 来自 :607-611 的 `cfg.plugins(r)`） |
| 红 C 单值具体成员 | 元素实参是 `r.<member>` 或局部具体组件（变量／调用结果） | assembly/run.go:615 `r.compress`、:618 `r.finalRound`；assembly/tools.go:250 `observer`、:253 `r.ageFold`；assembly/host.go:709 `driver`、:728 `settleDriver` |
| 红 D 复合字面量 | 元素实参是 `T{...}` | assembly/tools.go:256-258 `observePlugin{...}` |

**现树红集 = 7 处 / 3 文件**（run.go 615/618、tools.go 250/253/256、host.go 709/728）。原登记只列压缩消费两处；后五处是依本判据复核才浮现的同族点——这正是判据必须机械化的理由。

行号取自当前工作树；基线 HEAD f1c51e87 上是同样的 7 处（run.go:597/600、tools.go:250/253/256、host.go:694/713），仅 run.go 与 host.go 因未提交变更位移。判据按**形态**判定、不登记行号，故位移不影响判定；行号只是交接清单的读法，实施时以门禁输出为权威。

### 2.4 例外只有一条通道

红形态要变绿，只有两条路：把挂载改成合规 A/B（属 02 的动态化），或按 ADR 0006:22 突破协议登记例外（reason + adr + user_ruling + review_by）。**没有白名单通道、没有 `--write-baseline`、没有跳过开关**（沿用 scripts/gates/check_assembly_symbols.py:12-14 的立场与 scripts/gates/preflight.sh:19-21 的「不得静默关闭」哲学）。

## 3 R5b 声明活体四条件

| 条件 | 判据 | 现树 |
|---|---|---|
| C1 声明被接受 | `plugins` ∈ api/config/decode.go:157-161 `agentKeys` 块（读被守护面，不持副本；先例 scripts/gates/check_feature_boundary.py:36-48） | 绿 |
| C2 宿主 dialect 声明键 | assembly/host_document.go:91-113 `hostAgent` 块含 `yaml:"plugins"` | **红**（无此字段） |
| C3 声明达 spec | assembly/host_document.go:182-221 `mapDocument` 的 spec 字面量含 `Plugins:` | **红**（不映射） |
| C4 按名迭代消费 | 生产源码存在 `range <expr>.Plugins`，排除 api/config/** 自映射、`*_test.go`、以及含 `reflect.DeepEqual` 的变更提示行 | **红** |

C4 取「被 range 迭代」这一形态，是因为它排除了三类伪消费者：api/config 自身编解码（clone.go:56、fingerprint.go:58/204）、编译期断言（skeleton_assert.go:39）、变更提示（assembly/host_reload.go:717 `reflect.DeepEqual(before.Plugins, after.Plugins)`——日志型引用，过不了自证检验）。同名字段 `AgentBinding.Plugins`（runtime/state/binding.go:143-145，类型 `[]plugin.Instance`）与 `NativeAgent.Plugins`（assembly/run.go:73-76）是实例集合而非声明名集，不落入 C4 的 `<expr>.Plugins` 名集消费语义——判据按「名集被逐项求值」定义，不按字段名。

**残留下界（显式声明）**：C4 只判「声明名集被逐项求值」，名字到注册实例的解析正确性由 02 的能力契约与测试承担，不在本判据范围。

## 4 登记面与突破通道

- 新建 `golden/runtime/plugin_mounts.json`：reviewed registry，形状与 golden/runtime/assembly_symbols.json 一致（`schema_version`/`adr`/`user_ruling`/`review_by`/`reason` + `exceptions`），元数据经 scripts/gates/guard_checklib.py:203-226 校验，例外条目经同文件:222 校验；`exceptions` 现树为**空数组**——「现树无豁免」是显式陈述，不是死字段（见 §6）。
- golden/runtime/architecture.json 增 `mounting` 节：`invariant`（三条 SHALL 文本）、`gate`（判据入口路径）、`declaration.key`；门禁断言 `declaration.key` 与代码常量一致且 ∈ agentKeys——登记与代码两侧互为锚，防登记悬空（先例 scripts/gates/check_config_liveness.py:120-133 把登记的类型名与代码常量对照）。登记缺失、元数据缺失、例外陈旧均红。

## 5 为何新规则而非扩 R2

- **被否 甲：扩 scripts/gates/check_assembly_symbols.py。** R2 扫的是**声明名形态**（同文件:35-40 的 `FUNC_RE`/`PORT_RE`），挂载判据扫的是**表达式形态**；同文件混两类会让 R2 的正反自测语义漂移，且 R2 按 symbol 名的注册表表达不了 append 形态的例外。
- **被否 乙：并入 scripts/gates/check_feature_boundary.py。** 该文件已载 R1/R2/R3，其 R3 是「runtime 执行码不得命名 meditation/reflection」——一条**特性名单**判据；并入会诱导把挂载判据也做成特性名单（自造第二份名单，元规则二）。
- **取：新文件 check_plugin_mounting.py = R5**，独立注册表、独立自测，与 R1–R4 一一对位。

## 6 死字段反事实检验（登记面每个字段谁消费）

| 字段 | 消费方 | 删掉会坏什么 |
|---|---|---|
| `schema_version`/`adr`/`user_ruling`/`review_by`/`reason` | guard_checklib.validate_guard_metadata:203 | 注册表退化为无裁决来源的清单，例外与复审无从审计 |
| `exceptions[]`（含各条 reason/review_by） | 形态 C/D 的放行面 + 陈旧检测 | 突破协议失去登记处，突破只能改代码，违反 ADR 0006:22「三缺一即红」 |
| `mounting.invariant` | 门禁存在性断言 | golden 无此断言（现状），判据只剩脚本、无跨域契约 |
| `mounting.declaration.key` | 与代码常量及 agentKeys 交叉锚 | 登记可指向失效键而不被发现 |

无第三消费方的字段不建：`pipeline_containers`、`allowed_forms` 一律不登记——前者由类型锚推导（§2.2），后者是不变量本身（形态 C/D 即判据文本），登记化等于给判据留下调的旋钮。

## 7 正反样例自测（scripts/gates/test_check_plugin_mounting.py）

每规则至少一正一负；规则退化为只打印时自测红。用例逐条打印 `PASS <用例名>`，集尾打印 `check_plugin_mounting selftest: ok`，退出码 0；断言形式沿用 scripts/gates/test_check_assembly_symbols.py:98-125 的 `case(...)` 式（fixture 树 + subprocess + exit code + 点名文案）。用例清单（13 例，名即可 grep 的逐字锚）：

- R5a：`spread forwarding passes`、`declared instances assignment passes`、`a single member append is rejected`、`a composite literal append is rejected`、`a registered exception passes`、`a stale exception is rejected`
- R5b：`a declaration outside agentKeys is rejected`、`a host dialect without the plugins key is rejected`、`a spec mapping without Plugins is rejected`、`a change note is not a consumer`、`a range consumer passes`
- 登记：`an unregistered mounting invariant is rejected`、`mounting registry metadata is required`

01.R1 复核（§10）另补 11 例并强化 1 例，为 24 例：空推导、gate 文件、不变量数/SHALL、声明键值、spec 字段、`exceptions` 类型、重复登记、例外复审元数据、例外文件不存在、例外越界，以及 `a declaration outside agentKeys is rejected` 追加 `errors=2`。

01.R1 续单（§10.1／§10.2）再补 18 例，为 42 例（01.R1 再续单又补 8 例，**本域现为 50 例，见 §10.5**）：四个新增推导形态各 1 例（`a var-declared pipeline collection is derived`、`a literal-initialized pipeline collection is derived`、`a parameter pipeline collection is derived`、`a tagged pipeline field is still derived`）、注册实现面 3 例（`a hardcoded append in a registry implementation file is rejected`、`a resolved collection filter in a registry implementation file passes`、`a forwarded element outside the registry implementation is rejected`）、C4 类型面 1 例（`an instance collection is not a declared name consumer`），以及把基线 10 个「无处可红」的发射点各配一例（`a spec file without an AgentSpec block is rejected`、`a registry exception that is not an object is rejected`、`a mounting gate that names nothing is rejected`、`a registry implementation entry outside the assembly scope is rejected`、`a registry implementation entry that is not a Go source is rejected`、`a missing declaration surface is rejected`、`a declaration surface without the agentKeys block is rejected`、`a missing host document is rejected`、`a host document without the hostAgent dialect is rejected`、`a host document without mapDocument is rejected`）；`a declaration key no spec field carries is rejected` 追加 `errors=3`。

## 8 状态机与验收边界

| 阶段 | 门禁状态 | 判据 |
|---|---|---|
| W1（本域） | 红：R5a 7 处 + R5b 3 缺项 | 13 例自测绿；两处登记落地；红集恰等于 §2.3/§3 枚举 |
| W2（02） | 转绿 | plugins 接通（C2/C3/C4）+ 挂载动态化（形态 C/D 归零或登记例外） |

本域验收 = 判据存在、自测绿、红集可逐行点名；**全绿是 02 的验收**（一级 D5「P0 golden 守护门」）。红门禁阻断 lint/preflight/CI 是设计状态，与症状锚红同构：assembly/symptom_anchor_recovery_test.go:382 的 `t.Fatalf` 现树即红，ORCHESTRATION.md:145 记录了对该红态的判读。

## 9 风险与回退

- **误伤非特性基础设施**：assembly/host.go:728 的 settle 驱动若判定为基础设施而非特性组件，处置是走突破协议登记（一次可见裁决），不是降判据、不是加白名单。
- **推导失锚**：管道集合推导为空（类型改动）→ 门禁红并具名说明失锚，不许静默通过。
- **02 改动形态漂移**：把挂载改成「先赋具体构造再 append」仍红，属预期——动态化应走声明/注册表解析，由 02 调实现，不由本域调判据。
- **回退**：删门禁脚本、自测、两处登记与 lint.sh 一行，回退后与基线零 diff（一级 D7 回退纪律）。

## 10 01.R1 复核：判据的真实覆盖（2026-11-16）

**牙口实测**：对每条规则做「退化为只打印」变异（`errors.append` → `print`，隔离副本上跑真自测）。`a declaration outside agentKeys is rejected` 原不能分辨：C1 的键集检查与 `registration_errors` 的交叉锚是同一谓词的两处发射，只删 C1 一侧时门禁仍红、且文本照样打印，故该用例改断言 `errors=2`（两条规则都必须真的拒绝，而不是打印）。

**比例勘误（§10.2）**：原文写「20/20 变异令自测转红」**与实测不符**。以「每个 `errors.append` 发射点各降级一次」为算子，24 例基线实测 **27 个发射点、10 个仍绿（17/27）**——缺例的是登记表条目形状（非对象）、`mounting.gate` 空值、无 AgentSpec 块、`registry_implementation` 两个形状分支、C1 文件缺失／块缺失、C2 文件缺失／方言缺失、C3 mapDocument 缺失。原文的 20/20 不是同一算子下的数，不得再作为基线引用。

**已能证伪的覆盖面**：R5a 具体成员/复合字面量/陈旧例外/空推导；R5b C1–C4（含「变更提示不是消费方」「`_test.go` 不是消费方」）；登记面七条（挂载节、不变量数、SHALL、gate 文件、声明键值、spec 字段、解码器交叉锚）；登记表五条（类型、重复、越界、文件存在、例外复审元数据）。带 tag 的结构体字段仍被推导，因为字符串先掩码、tag 不影响字段式匹配——现由 `a tagged pipeline field is still derived` 钉住。

### 10.1 五个未覆盖面已闭合（01.R1 续单）

红线：五例均在**忠实子集树**（真 `assembly/`＋`api/`＋`golden/`＋`docs/adr`，门禁输出与原树逐字同为 `go_files=30 containers=2 mounts=0 consumers=1 errors=0`）上注入，先以修前门禁证明被接受，再以修后门禁证明被点名。

| # | 修前（被接受） | 闭合做法 | 修后（点名） |
|---|---|---|---|
| 1 整文件豁免 | `plugin_catalog.go` 注入 `r.plugins = append(r.plugins, r.compress)` → `mounts=0` exit 0 | 豁免改为**形状**：只在 `registry_implementation` 文件内放行「range 绑定变量且 range 源非复合字面量」的元素转发；文件本身不再整体跳扫 | `assembly/plugin_catalog.go:406: R5a pipeline collection takes a concrete member 'r.compress'` |
| 2 C4 量字段名 | 唯一消费点改成 `range native.Plugins`（`NativeAgent.Plugins` 是 `[]plugin.Instance`）→ `consumers=1` exit 0 | **门内类型感知**（不把证明下移 Go 侧，理由见下） | `R5b C4 assembly/plugin_catalog.go:281: range native.Plugins reads a plugin.Instance field, an instance collection …` |
| 3 `var x []plugin.Instance` | 该容器内的 `append(extras, r.compress)` → exit 0 | `PIPELINE_VAR_RE` 收入为该推导形态 | `assembly/plugin_catalog.go:407: … concrete member 'r.compress'` |
| 4 `x := []plugin.Instance{…}` | 同上 → exit 0 | `PIPELINE_LOCAL_RE` 增复合字面量初始化分支 | `assembly/plugin_catalog.go:407: … concrete member 'r.ageFold'` |
| 5 函数参数容器 | `func f(r *Run, extras []plugin.Instance)` 内 append → exit 0 | `PIPELINE_PARAM_RE`（参数形与局部形不同锚，单列一条而不塞进 LOCAL） | `assembly/plugin_catalog.go:406: … concrete member 'r.compress'` |

**C4 取门内类型感知而非下移 Go 侧的理由**：Go 侧那半（断言 `declaredPluginsOf` 的计算结果来自 `spec.Plugins`）本窗口白名单外且 Go 树在改、不可运行，落一个不可验证的锚等于换一个假绿通道；而文本门的判别力可以自证——把真树 `api/config/config.go` 的 `Plugins []string` 改成 `[]plugin.Instance` 后，同一消费点即被点名（`consumers=0`、exit 1），说明判据量的是元素类型而不是字段名。类型解析的限度写在脚本 docstring：只吃「一个标识符＋一个 slice 字段」两段式，路径中间夹标量字段则不解析（fail closed，不静默放行）。Go 侧锚仍未落、仍未跑（§10.3）。

**合法形态仍被接受（具名）**：`spread forwarding passes`（展开转发）、`declared instances assignment passes`（已解析集合整体赋值）、`a range consumer passes`（名集迭代消费）、`a registered exception passes`（已登记例外）、`a resolved collection filter in a registry implementation file passes`（`:388` 那一类过滤）、`a tagged pipeline field is still derived`（带 tag 字段仍推导）。真树同批证据：`plugin_catalog.go`／`compress_mount.go` 进入扫描面后门禁仍 `mounts=0 consumers=1 errors=0`（7 个 builtin 经 catalog 装载者未被误判）；`a forwarded element outside the registry implementation is rejected` 钉住「转发豁免不得外溢」。空豁免对照实测：把 `registry_implementation` 清空后，修前修后门禁都只多出 `plugin_catalog.go:388` 一处 filter——即整文件豁免当年藏起的恰好就是这一点，收窄后由形状精确区分。

### 10.2 门自身验证的补全

**自测用例 24 → 42**（新增 18 例见 §7）。**变异比 17/27 → 28/28**（算子＝每个 `errors.append` 发射点单独降级为 `print`，隔离副本跑真自测，期望转红）。

续单另发现并修掉两条**「用例被邻规则满足」新通道**——正是 R62 对 C1 那类缺陷的同形复现（本案由新 C4 引入）：`a declaration key no spec field carries is rejected` 与 `an instance collection is not a declared name consumer` 原先只靠 `rc=1`＋文案命中，而降级后另一条 C4 发射仍供 exit 1、文案照打，用例照绿。现分别断言 `errors=3`／`errors=2`，两条 C4 发射点各自可红。

### 10.3 残余（显式，不隐）

> **勘误（2026-11-16）**：下列 1–3 项**已由 §10.4 立判据、§10.5 闭合**，原文保留为 01.R1 上一轮的现场记录，不再是当前状态；第 4 项仍开放。

1. **元素级转发的洗白路径**：`registry_implementation` 文件内先 `extras := []plugin.Instance{…}` 再 `for _, e := range extras` 转发，元素来自字面量而非解析结果，仍被接受（range 源是标识符、不含 `{`）。要闭合需把「容器初始化形态」也纳入转发判据，本单不做。
2. **同类型多参数名**（`func f(a, b []plugin.Instance)`）只推导出末名；树内无此形，未为它加正则（要全收需按参数组 findall，收益不抵复杂度）。
3. **`registry_implementation` 条目指向不存在的文件**只使 `present=` 下降、不单独判红（实测 accepted）。方向安全（豁免失效即扫描变严），仅登记不修。
4. **Go 侧消费者锚仍未跑**：`assembly/plugin_mount_test.go:71/:97` 于本窗口禁跑 `go test`；01.R1 的框不勾。

**消费者面静态链**：`plugin_catalog.go:380 agentPlugins → :381 declaredPluginsOf → :281 for _, entry := range spec.Plugins`；7 个 builtin 名在 `:34-40`（compress-consumer/final-round/reflection-driver/settle-driver/lease-observer/age-fold/telemetry-observer）。**运行期证据本次未取得**：`TestBuiltinCatalogMountsEveryFeature`／`TestHostPluginsMountThroughTheCatalog`（`assembly/plugin_mount_test.go:71/:97`）因 Go 树在改、本窗口禁跑 `go test`，须在树静定后补跑。

### 10.4 01.R1 再续单：三项残留下沉进判据（2026-11-16）

§10.3 的 1–3 项此前只活在上一单战报里（编排者 grep 本文件零命中，已由 R64 登记）。**残留只活在会话里就等于没有载体**，故先把可构造违规写死在本文件，再动门。

| # | 残留 | 可构造违规（修前被接受） | 闭合判据 | 修后点名 |
|---|---|---|---|---|
| 1 洗白 | 元素级转发的 range 源不受约束：本文件用复合字面量初始化过的局部，其元素被逐元素转发即洗成「已解析集合的成员」 | `registry_implementation` 文件内 `extras := []plugin.Instance{r.compress}` 后 `for _, instance := range extras { elements = append(elements, instance) }` | 转发判据加一条前提：range 源若为本文件从 `[]plugin.Instance{…}` 初始化的容器（含同文件别名链的定点闭包），该 range 不是解析，元素不算转发 | §10.5 |
| 2 分组参数 | `PIPELINE_PARAM_RE` 只认「类型紧邻的那一个名字」，`func f(r *Run, a, b []plugin.Instance)` 只推导出 `b`，`a` 的 append 不判 | `func grouped(r *Run, a, b []plugin.Instance) { a = append(a, r.compress) }`：`a` 未入推导集，append 不被评审，exit 0 | 参数形态改为按 Go 的分组语义收名字列表，组内每个名字都推导；前缀仍限 `(`、`[`、`,`，故推导面只增不减 | §10.5 |
| 3 悬空注册 | `mounting.registry_implementation` 条目只降 `present=`，无任何规则判红，登记可以指向不存在的文件而门禁仍绿 | `architecture.registry_implementation = ["assembly/absent_mount.go"]`：exit 0、`present=0`，`errors=0` | 所登记路径必须存在，与同门已有的两条同类判据（`mounting.gate` 不存在即红、例外 `file` 不存在即红）对齐：登记面的每一处路径引用都须解引用成功 | §10.5 |

**第 1 项闭合时发现的同族第二条通道（同批闭合，原报告未列）**：`append` 的展开形态原先一律放行，故 `extras := []plugin.Instance{r.compress}` 后 `append(r.plugins, extras...)` 一行即洗白，不需要 range；内联写法 `append(r.plugins, []plugin.Instance{r.compress}...)` 同样放行。spec 说的「展开转发已解析集合」指展开**已解析**集合，展开本文件构造的字面量不是解析。故转发判据对两种实参形态同一条前提：展开的操作数不得是本文件从字面量绑定的名字（含别名），也不得是复合字面量；元素形态另需 range 源不是这种名字。**窄化方向**：只减放行面，不减扫描面（容器集与文件集实测逐名不变）。


**第 3 项的方向论证**（原记「方向安全故不修」不成立）：窄化豁免本身不会放宽判据，但**登记说谎**是独立缺陷。本门的登记面已用同一谓词判过两处路径引用（`mounting.gate`、例外 `file`），第三处路径引用不能例外；§6 的死字段检验同理——条目指向不存在文件时，该条目对豁免面零消费方，只剩一个掉下去的计数。`present=` 保留为观测读数，但绿态下它必等于 `registry_impl=`，差值不再是通道。

**第 1 项的同族残留（本单闭合范围外，显式登记）**：跨过程的洗白仍不可判——`func seed() []plugin.Instance { return []plugin.Instance{r.compress} }` 后 `extras := r.seed()` 再逐元素转发。本门是单文件文本判据，不读调用返回值；这类「返回值即解析结果」的形状与它已声明的「range 源须在别处解析」前提同构而方向相反，不作假装闭合。**后果**：装配根仍可用一个本地 helper 函数把具体构造搬进管道；处置归 02（挂载动态化后此类形态自然消失）与本域的下一轮判据，不在此假装安全。

**合法形态逐条仍须绿**（改动不得误伤，各配正例）：展开转发、已解析集合整体赋值、range 消费者（`for _, entry := range spec.Plugins`）、已登记例外、`registry_implementation` 文件内的已解析集合过滤（真树 `plugin_catalog.go:388` 的 `for _, instance := range contributed`，`contributed` 是参数而非本文件字面量）、带 tag 管道字段仍被推导、7 个 builtin 经 catalog 装载。

### 10.5 三项残留闭合实测（2026-11-16）

每个可构造违规都在**同一份忠实夹具**上前后各跑一次：修前用本单改动前的门（`/tmp/recovery-01res/before/` 的重建件，691 行，在真树上复现 R64 基线 `32/5/0/1/0/2/2/0` 后才用于取证），修后用工作树门。夹具沿用自测的 `build_fixture`，故与被守护面同形。

| # | 修前 | 修后点名（原文） | errors |
|---|---|---|---|
| 1a 元素级洗白 | exit 0，`mounts=0` | `assembly/compress_mount.go:7: R5a pipeline collection takes a concrete member 'instance', forwarded from 'extras', which this file binds from a []plugin.Instance literal` | 1 |
| 1b 别名洗白 | exit 0 | 同上，`:8`，`forwarded from 'extras'`（别名 `seed` 经定点闭包归到 `extras`） | 1 |
| 1c 字面量展开 | exit 0 | `assembly/fixture.go:15: R5a pipeline collection takes a spread 'extras...', a name this file binds from a []plugin.Instance literal` | 1 |
| 1d 内联字面量展开 | exit 0 | `assembly/fixture.go:14: R5a pipeline collection takes a spread '[]plugin.Instance{r.compress}...'` | 1 |
| 1e 下标展开 | exit 0 | `assembly/fixture.go:15: R5a pipeline collection takes a spread 'extras[:]...', a name this file binds from a []plugin.Instance literal` | 1 |
| 2 分组参数 | exit 0，`containers=4`（只认 `b`） | `assembly/fixture.go:14: R5a pipeline collection takes a concrete member 'r.compress'`；同 fixture `containers=5`（`a`、`b` 都入集） | 1 |
| 2 对照（末名） | exit 1（本来就红） | 同上，末名红态前后一致 ⇒ 诊断为「首名漏推」而非「整组不判」 | 1 |
| 3 悬空注册 | exit 0，`present=0` | `golden/runtime/architecture.json: mounting.registry_implementation[0] 'assembly/absent_mount.go' does not exist` | 1 |

**自测 42 → 50 例**（新增 8 例：`a grouped parameter collection is derived`、`a grouped parameter list keeps every legitimate spread`、`a spread of a slice literal bound in this file is not a forwarded element`、`a spread of an inline slice literal is not a forwarded element`、`a spread of a slice of a laundered literal is not a forwarded element`、`a laundered slice literal is not a forwarded element`、`an alias of a laundered slice literal is not a forwarded element`、`a registry implementation entry naming a missing file is rejected`）。八个新例**各自断言自身 `errors` 计数**（不是只命中文案），沿用 §10.2 对 C1 类缺陷的处置。

**变异比实测 29/29**（算子＝`errors.append` 调用点逐个降级为 `print`，AST 定位，隔离副本跑真自测）。独立复算确认上一单的 28/28：以同一算子跑重建的修前对（28 个发射点）→ **28/28**，与 R64 记录一致；本单新增 1 个发射点（`registry_implementation` 存在性），故为 **29/29**。`guard_checklib` 内的元数据校验发射不在本算子面内（它不属于本门的规则）。

**本单是否开出新通道：否，且逐条有据。** 发射点算子证明不了「用例由自己的规则满足」（邻规则供 exit 1 时用例照绿），故另做**谓词变异**：每次只回退一条闭合、保留全部发射，看哪些用例转红——①回退分组参数正则→**仅** `a grouped parameter collection is derived` 红；②`literal_sources` 返回空集→**仅**四个洗白例（1b/1c/1e 与 1a）红；③展开分支恢复无条件放行→**仅**内联字面量例红；④去掉下标取基名→**仅**下标展开例红；⑤删存在性检查→**仅**悬空注册例红。五条各自只命中自己的用例，无一例被邻居顶替。

**扫描面未缩、容器集未漂**：修前修后在真树上 `pipeline_containers` 的**名字集逐名相同**（`Plugins, contributed, elements, instances, plugins`）、文件集相同（32），`mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`。另核：真树 `assembly/*.go` 里 `literal_sources` 逐个文件为空集，`plugin_catalog.go:388` 的 range 源 `contributed` 不在该集 ⇒ 闭合没有触及任何现存合法形态。

**仍不闭的一项（原样保留在 §10.3 第 4 项）**：Go 侧消费者锚 `assembly/plugin_mount_test.go:71/:97` 本窗口禁跑 `go test`，两枚测试**未执行**，运行期证据未取得；01.R1 的框不勾。
