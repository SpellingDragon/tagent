> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 02 插件动态挂载（W2：全特性插件化＋builtin 默认注册＋`plugins:` 声明定制）

## Why

一级 design **D1 P0**（三挂点动态拓展为 golden）与 **D6.16** 在插件管线挂点上未兑现。golden 门 `scripts/gates/check_plugin_mounting.py` 今日红集实测 10 条：

- **R5a 七处具名构造**（`python3 scripts/gates/check_plugin_mounting.py` 实测输出）：`assembly/run.go:615` `r.compress`、`:618` `r.finalRound`、`assembly/tools.go:250` `observer`、`:253` `r.ageFold`、`:256` `observePlugin{…}`（复合字面量）、`assembly/host.go:709` `driver`、`:728` `settleDriver`。装配根因此认识 7 个特性名与特性类型：增删/替换任一特性必须改 Go 代码重编。
- **R5b 三条 liveness 全红**：`hostAgent`（`assembly/host_document.go:92-113`）无 `plugins` 字段（C2）；`mapDocument`（`:184-214`）构造 `config.AgentSpec` 时不携带（C3）；生产代码无 `range <expr>.Plugins`（C4）。而 config 侧早已就绪：`agentKeys` 含 `plugins`（`api/config/decode.go:158-165`）、`stringSeqField` 已解析（`:190-192`）、`clone.go:56`、`fingerprint.go:204`、`host_reload.go:717` 均已携带——声明写得再对也不挂任何东西（死字段）。
- **ADR 0004 目标未成立**：该 ADR 定「声明加一行、装配根零改动」，今日 7 特性全部在装配根具名构造；ADR 0007 把注册表扩为代际作用域运行期生命周期，压缩消费仍不在其中（`run.go:580-585` 只注册 `cfg.genRegistrations`）。
- **用户终裁（D6.16，权威）**：「全插件，但是需要有默认启动流程注册 builtin 插件，如果框架的使用者认为不需要，允许自定义拓展」——7 个硬编码特性**全部**经注册表挂载；**默认启动流程**注册 builtin（框架开箱即用）；使用者经 `plugins:` 声明禁用/覆盖/扩展。否「装配根硬编码」，否「无默认注册（开箱不可用）」，否「仅 compress 动态化（P0 门不全绿）」。

## What

同一原子簇三步（一级 D4「02 依赖 01」）：

1. **builtin 插件目录＋默认注册**（`assembly/plugin_catalog.go`）：目录一行一插件，7 项（`compress-consumer` / `final-round` / `reflection-driver` / `settle-driver` / `lease-observer` / `age-fold` / `telemetry-observer`），每项声明**作用域**与构造：`run`（run 级实例，boot 装 `r.plugins`）、`host`（宿主装配装 `WithPlugins` 集合，携带宿主面）、`agent`（按 agent 声明逐管线解析）。**默认启动流程**＝boot 遍历目录注册 run 级、宿主装配遍历目录注册 host 级、agent 管线遍历目录解析 agent 级——装配根只转发目录解析结果，零特性名、零具体类型构造。压缩消费的构造落它自己的挂载文件 `assembly/compress_mount.go`（ADR 0004「插件自身的挂载文件与目录一行是唯一实施处」）。
2. **`plugins:` 声明接通与定制**：宿主方言 `hostAgent` 加 `plugins` 字段、`mapDocument` 携带进 `config.AgentSpec.Plugins`；装配侧 `r.agentPlugins` 逐项迭代声明（`range spec.Plugins`）。语义：**缺省（无该键）＝目录内置全挂**（开箱即用、行为不变）；声明项 `name` 点亮该内置（幂等），`-name` 禁用该内置（定制）；未注册名、空名、以及本由 run/host 级携带的内置名，装配期**具名拒绝**（AGENTS.md「误配响亮失败」）。
3. **七处硬编码清零**：`run.go:596-599/614-619`、`tools.go:247-258`、`host.go:689-731` 的具名构造与无条件 append 全部移除，改由目录/`agentPlugins`/`hostPlugins` 转发已解析集合。

**本波不交付（剩余，须记）**：压缩消费的 `state.GenRegistration` **每代重建**（ADR 0007 形态）——目录注册是 boot/装配一次；`compressStateSlice` 保持无条件注册（注册序与今日逐项相同，checkpoint 账面零漂移）。该剩余在 tasks 2.5 显式登记，不冒充达成。

## Capabilities

- `plugin-mounting`（**ADDED**）：四条 Requirement——「特性经 builtin 目录挂载＋默认启动注册」「`plugins:` 声明定制与误配响亮失败」「装配根无特性硬编码」「`plugins:` 声明有生产消费方」。spec 已按本波实装重写（原稿的「压缩每代重建」「声明点名才挂压缩」两条与 D6.16 缺省注册语义冲突，已改写并留文）。
  - ⚠️ 实测偏差（已上报）：一级 D2 表记为 MODIFIED，但 `openspec/specs/` 与 `openspec/changes/archive/` 下**均无 `plugin-mounting` 主规格**（`grep -rn plugin-mounting openspec/` 仅命中一级 design.md），无既有 Requirement 可 MODIFIED，故取 **ADDED**。10 域做「主 spec 转正/退役」时接管归档同步。

## 边界与依赖

- **依赖**：01 的守护判据（本域是被守护面；判据已提供 `mounting.registry_implementation` 豁免面，`assembly/compress_mount.go` 与 `assembly/plugin_catalog.go` 已在册）；既有设施 `state.GenRegistry`（`runtime/state/genregistry.go:46-52/121-162`）、`plugin.Instance` 契约（`api/plugin/material.go:76-86`）、`config.AgentSpec.Plugins` 解析/克隆/指纹（`api/config`）、`golden/runtime/schema_allowlist.json:19` 已登记 `plugins` 通用键。
- **被依赖方**：03（压缩预算经该消费者取）、04（中段票据与压缩产物同一消费者）、05/06/07（boot 路径同触 `assembly/run.go`，W2 先落）、10（主 spec 转正与 golden 同步）。
- **接口面**：`hostAgent.Plugins []string`（yaml `plugins`，与 `config.AgentSpec.Plugins` 同名同义）；`plugins:` 声明项写法 `name` / `-name`（无带参插件，本域不引入 `with:`；反事实：删掉 `with:` 无人坏）；内部函数 `r.mountBuiltinPlugins`、`r.agentPlugins`、`hostPlugins`、`builtinPluginCatalog`（包内，不新增导出端口）；`state.GenRegistration` / `Stateful` 签名不变。
- **禁止事项**：
  - 不改摘要预算（03）、不改中段票据与 `ticketText`（04）、不改压缩质量与 `summary_gist` 语义（03）、不动 `recall`（04）。
  - 不改 `scripts/gates/`（判据口径属 01/10 单写）；不动共享面 golden 除 `mounting.registry_implementation` 豁免（本波未新增文件，无需改）。
  - 不触外来未跟踪文件（`runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py` 等）与 config-surface-normalization 的未提交变更。
  - 不改写冻结归档（`openspec/changes/archive/**` 零 diff）。
  - SHALL NOT 删除/覆盖/改写已提交事实；不为单使用方抽象导出；不另立第二派生实现（目录是构造点清单，不是第二真相）。
