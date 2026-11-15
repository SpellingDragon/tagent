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
