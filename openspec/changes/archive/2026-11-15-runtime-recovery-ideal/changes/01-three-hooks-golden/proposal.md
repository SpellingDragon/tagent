# 01 三挂点动态拓展 golden 守护（W1 立判据）

## Why

**核心原则已立，机械守护未覆盖它。** ADR 0006（docs/adr/0006-three-hooks-is-the-core-iteration-principle.md:15-20）把「事件 x 生产 / 插件 f(x) 管线 / 消费 g(x) 读链」三挂点定为本项目一切特性拓展的唯一轨道，并落了 R1–R4 四条守护。四条各自守什么，逐条对呈现树核过：

| 规则 | 守什么 | 实现（实证位置） | 抓不到什么 |
|---|---|---|---|
| R1 | agent 声明的 schema 节白名单 | scripts/gates/check_schema_allowlist.py:2-20 | 挂载路径 |
| R2 | 装配根 `resolve<Feature>` / `validate<Feature>` / `<feature>Port` **符号形态** | scripts/gates/check_assembly_symbols.py:35-40（`FUNC_RE`/`PORT_RE`） | **具体类型构造**：正则只看声明名形态 |
| R3 | 持久真相登记 | scripts/gates/check_durable_truth_registry.py | 挂载路径 |
| R4 | 主 spec requirement 文本的机制细节 | scripts/gates/check_spec_hygiene.py:2-20 | 挂载路径 |

R2 判现树是**绿**的：`python3 scripts/gates/check_assembly_symbols.py` → `go_files=30 declarations=7 registered=7 errors=0` / `check_assembly_symbols: ok`（exit 0）。同一棵树里却有下面四处「装配根按特性名挂载插件管道」，全部漏过：

1. assembly/run.go:596 `r.compress = NewCompressionConsumer(ageFoldSessions{run: r}).withSummaryGist(bandSummaries{run: r})`——装配根直接构造具体压缩消费组件；
2. assembly/run.go:615 `r.plugins = append(r.plugins, r.compress)`——无条件 append 进运行级插件管道（`r.plugins []plugin.Instance`，assembly/run.go:282）；
3. assembly/run.go:598 `NewFinalRoundPlanner(...)` + :618 `append(r.plugins, r.finalRound)`——同族第二例；
4. assembly/tools.go:250 `elements = append(elements, observer)`、:253 `elements = append(elements, r.ageFold)` 与 :256-258 `elements = append(elements, observePlugin{...})`——按名把租约观测器、折叠插件与具体观测组件挂进每代绑定的插件管道；
5. assembly/host.go:709 `instances = append(instances, driver)`（世代反思驱动）与 :728 `instances = append(instances, settleDriver)`——宿主贡献面同一形态的增补。

其中 3–5 是复核 D6.15 时按判据逐处枚举才浮现的（原登记只列压缩消费两处）：同一句判据「装配根对插件管道做具体成员增补」共 **7 处命中、3 个文件**，**无一处被既有守护覆盖**。这正是判据必须机械化的理由——靠人列举，本轮就曾只数到 2。

**`plugins:` 声明是死字段。** 声明面本身是通的：api/config/decode.go:157-161 的 `agentKeys` 列了 `plugins`，:190 把它解进 `AgentSpec.Plugins`（api/config/config.go:66-67）。但消费链断在装配侧：宿主文档 dialect 的 `hostAgent`（assembly/host_document.go:91-113）没有 `plugins` 字段，`mapDocument`（:182-221）不把 `Plugins` 写进 `config.AgentSpec`，装配根也没有任何按名解析点。docs/EXTENSIONS.md:188 书面自认这一点：「声明里的 `plugins:` 名单是声明侧的次序意图，`assembly.Assemble` 不把它解析成元素」。api/config 内部的三处读取（clone.go:56、fingerprint.go:58/204、skeleton_assert.go:39）是编解码自映射与编译期断言，不是消费者；host_reload.go:717 的 `reflect.DeepEqual(before.Plugins, after.Plugins)` 是变更提示，属日志型引用，过不了自证检验。

**golden 无此断言。** golden/runtime/architecture.json 顶层只有 `concept_ratchet`/`schema_version`/`config_facts`/`change`/`module_path`/`root_package`/`description`/`scope`/`packages`/`edges`/`external`/`event_ownership`/`concept_bindings`——没有任何「特性经插件注册表动态挂载」的登记。`config_facts` 是配置真相链的登记，不是挂载面。

**后果与症状。** 动态拓展性是框架核心（ADR 0006 标题即此），当下只能靠自觉；一级 design D6.15（openspec/changes/runtime-recovery-ideal/design.md:81）据此把它立为 golden 并要求「先立判据（W1），特性据此收敛」。W0 症状即压缩消费硬编码：本条门禁在现树**必红**（一级 design §六，design.md:131），转绿归 02。

## What

1. **golden 不变量登记**：golden/runtime/architecture.json 增 `mounting` 节，声明「装配根无具体特性硬编码 / 特性经插件注册表动态挂载 / 声明有生产消费方」三条不变量与其判据面；登记缺失即红（仿 scripts/gates/check_config_liveness.py:108-135 读登记的做法）。
2. **新守护规则 R5**（新文件 scripts/gates/check_plugin_mounting.py）：
   - **R5a 管道成员来源受限**：对插件管道集合的 `append`，合规形态只有「展开转发已解析集合」与「整赋值给声明/注册表解析结果」；其余形态（单值具体成员、复合字面量）即红，除非按 ADR 0006:22 的突破协议登记例外。
   - **R5b 声明活体**：`plugins:` 声明须同时满足「被 agentKeys 接受」「在宿主文档 dialect 有声明的键」「映射进 `config.AgentSpec`」「存在按名迭代求值的生产消费点」四条；仅编解码自映射与变更提示不计。
3. **正反样例自测与接线**：scripts/gates/test_check_plugin_mounting.py 逐规则正反例（规则退化为只打印即自测红），接入 scripts/gates/lint.sh:112-124 三挂点守护段，经 scripts/gates/preflight.sh:48 与 .github/workflows/ci.yml:109 生效。

**本域不做的事**：不改任何特性代码（assembly/、api/ 只读判据），不实施动态化与声明接通（02 做），不实施摘要预算可配置（03 做）。本域只立判据与登记，其红集是交接给 02 的工作清单。

## Capabilities

- `three-hooks-extensibility`（**ADDED**）：装配根无具体特性硬编码、特性经插件注册表动态挂载、`plugins:` 声明有生产消费方、守护门硬编码即红（含正反样例自测）。

## 边界与依赖

- **依赖**（只读，不修改）：
  - ADR 0006 四规则及其注册表形状：golden/runtime/assembly_symbols.json（-reviewed allowlist、`schema_version`/`adr`/`user_ruling`/`review_by`/`reason` 元数据）与 scripts/gates/guard_checklib.py:203-226 的 `validate_guard_metadata`，本域新注册表沿用同一形状；
  - 声明面：api/config/decode.go:157-161 `agentKeys` 与 :190 的 `plugins` 解码——**读而不复制**（scripts/gates/check_feature_boundary.py:36-48 的既成做法：门禁读被守护面，不持第二份词表）；
  - 宿主文档 dialect：assembly/host_document.go:91-113 `hostAgent` 与其 yaml tag；
  - 管道集合的类型锚：assembly/run.go:282 `plugins []plugin.Instance`；
  - golden 登记面：golden/runtime/architecture.json。
- **被依赖方**：02-plugin-dynamic-mount（本条判据是其转绿判据）、03-compression-configurable（摘要可配置后仍须经声明挂载）、10-contract-sync-dead-code（主 spec 转正时同步本能力）。
- **接口面**：
  - 新注册表 `golden/runtime/plugin_mounts.json`（reviewed registry）：`schema_version`/`adr`/`user_ruling`/`review_by`/`reason` + `exceptions`（突破协议例外，含 `reason`/`adr`/`user_ruling`/`review_by`）；`exceptions` 现树为空数组；
  - architecture.json `mounting` 节：`invariant`（三条 SHALL 文本）+ `gate`（判据入口）+ `declaration.key`（与代码常量、agentKeys 交叉锚）；
  - 门禁 CLI：`check_plugin_mounting.py [--root DIR] [--registry PATH]`，沿用既有门禁的 `--root` 约定；**不新增 Go 公共符号**（concept_ratchet 不动）；
  - **不登记**管道集合名单与合规形态清单：前者由 `[]plugin.Instance` 类型锚推导（新容器自动入范围），后者是不变量本身（登记化即给判据留旋钮）。
- **禁止事项**：
  - **不改 assembly/、api/、runtime/ 任何生产或测试源码**——本域只立判据；判据在现树红是设计状态，不得为让树变绿而预先登记全量例外、不得降判据；
  - **不提供跳过开关**（不加 `--skip`/`--write-baseline` 类静默通道，沿用 scripts/gates/check_assembly_symbols.py:12-14 的立场与 scripts/gates/preflight.sh:19-21 的「不得静默关闭」哲学）；
  - 不动 openspec/specs/ 主 spec（转正归 10 域）；不改写冻结归档 openspec/changes/archive/**（零 diff）；
  - 不触外来未跟踪文件：runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、scripts/gates/runtime_budgets.py、scripts/gates/test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/；
  - 不触 config-surface-normalization 的未提交变更（同 dev 分支，互不触碰）；
  - 不造第二份特性名单：判据按**形态**与**被守护面读取**定义（ADR 0006 元规则二：结构不自证）。
