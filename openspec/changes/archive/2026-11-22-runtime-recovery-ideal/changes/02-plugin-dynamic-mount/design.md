> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 02 插件动态挂载 · 设计

> 静态决策见一级 [design.md](../../design.md)：**D1 P0**（三挂点动态拓展为 golden）、**D6.16**（全插件化＋builtin 默认注册＋使用者可自定义）、**D6.15**（判据先行）、D4 合并规则、§五共享面冻结签名。本域只给实施要点与实证触碰集，不改冻结签名、不触 `scripts/gates/`。
> 落地契约：`docs/adr/0004-feature-mounting-is-a-plugin-registry.md`（声明一行、装配根零改动）＋`docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md`（注册表＝代际作用域运行期生命周期）。

## 技术要点（承 D6.16）

1. **builtin 插件目录（装配根零改动的落点）**：新增 `assembly/plugin_catalog.go`，一行一插件：

   ```
   名称 | 作用域 | 构造
   compress-consumer | run   | assembly/compress_mount.go
   final-round       | run   | catalog
   reflection-driver | host  | catalog（宿主面 seam）
   settle-driver     | host  | catalog（宿主面 seam）
   lease-observer    | agent | catalog
   age-fold          | agent | catalog（run 级实例 boot 预备）
   telemetry-observer| agent | catalog
   ```

   作用域决定挂载路径：`run` → boot 遍历装 `r.plugins`；`host` → 宿主装配遍历装宿主 `WithPlugins` 集合（携带 seam：run／`host()` 访问器／冥想 port 与 plan／声明）；`agent` → agent 管线遍历按声明解析。目录与其条目是**插件注册表实现**，已在 `golden/runtime/architecture.json` 的 `mounting.registry_implementation` 在册（R5a 豁免面）——判据因此不判掉唯一合法构造点。

2. **默认启动流程＝目录遍历**：`Run.mountBuiltinPlugins` 先跑目录的 `prepare`（构造 run 级实例，如 age fold 的判定者），再按目录序跑 `run` 构造（compress、final-round），`r.plugins` 只以转发方式拼接（`append(r.plugins, instances...)`）。宿主侧 `hostPlugins(seam)` 按目录序构造 host 项并回填宿主保留面（`driver`/`settleDriver`）。装配根（`run.go`/`host.go`/`tools.go`）零特性名。

3. **`plugins:` 声明接通**：`hostAgent` 加 `Plugins []string` `yaml:"plugins"`；`mapDocument` 在 `config.AgentSpec` 字面量中携带 `Plugins: append([]string(nil), entry.Plugins...)`。config 侧零改动（`agentKeys`/`stringSeqField`/`clone`/`fingerprint`/`host_reload` 均已就绪）。反事实：不改 `mapDocument`，声明在宿主路径上恒空——死字段。

4. **声明语义（定制内置，不静默改默认）**：agent 管线插件集 = 宿主贡献实例 → run 级实例 → agent 级内置（目录序），其中 agent 级内置**缺省全挂**；声明项 `name` 点亮（幂等），`-name` 禁用。缺省全挂是「框架开箱即用」的落点，也让既有部署零改动即行为不变（`examples/wechat-bot/tagent.yaml` 无需补行——原稿「补 `plugins: [compress-consumer]`，否则上线停跑」的风险随缺省注册消失）。

5. **误配响亮失败**（ADR 0004「参数校验下沉」）：空名、目录未注册名、以及作用域属 run/host 的内置名被写进 agent 声明 → 装配期具名拒绝并列出可选内置。反事实：静默忽略 → 用户写了声明、插件不挂，症状以「摘要不产出/无折叠」隐性出现。

6. **七处硬编码清零**（golden R5a 实证红集）：`run.go:596-599`（compress/ageFold/finalRound 具名构造）与 `:614-619`（两次无条件 append）、`tools.go:247-258`（lease observer 单成员、ageFold 单成员、`observePlugin{}` 复合字面量）、`host.go:689-731`（`driver`、`settleDriver` 单成员）全部移除，改为转发目录解析集合。管线顺序不变：contributed → run 级（host driver/settle → compress → finalRound）→ agent 级（lease-observer → age-fold → telemetry-observer）。

7. **本波不交付的剩余（显式登记，不冒充达成）**：压缩消费的 `state.GenRegistration` **每代重建**（ADR 0007 形态）。今日代际注册表只承接 `cfg.genRegistrations`，压缩目录项是 boot/装配一次；把它改成代际重建需要 `Restore` 回放 `r.compress` 指针与 age fold executor 快照，并让 `run.go:2530/2698/2902/2938` 与 `compress_checkpoint.go:82-84/131-133` 的读出与换表同序（`r.mu`）——属独立子波，本波不做。
   - 连带：`compressStateSlice` 保持无条件注册（`run.go:571`）。条件注册会改变 checkpoint 切片聚合序，与「行为保真」冲突；目录缺省全挂时条件恒真，故无条件注册即等价形态。

## 实证触碰集

- `assembly/run.go`：:571（切片注册，保持）、:596-619（七处具名构造与 append，改）、:1505-1532（`WithPlugins`／`WithGenerationConsumer` 文档）、:1607-1625（`GenerationConsumers` 诊断）、:2530/2698/2902/2938（`NoteProduct` 读点）
- `assembly/tools.go`：:246-258（管线工厂 → `r.agentPlugins`）
- `assembly/host.go`：:600-601（driver/settle 声明）、:689-731（→ `hostPlugins(seam)`）
- `assembly/host_document.go`：:92-113（hostAgent 加字段）、:184-214（mapDocument 携带）
- 新增：`assembly/plugin_catalog.go`（目录＋解析）、`assembly/compress_mount.go`（压缩消费构造）、`assembly/plugin_mount_test.go`（锚）
- `api/config/config.go:65-67`（`Plugins` 字段注释改写为定制语义）
- **不改**：`scripts/gates/*`、`golden/runtime/architecture.json`、`api/config/{decode,clone,fingerprint}.go`、`memory/replay/*`、`runtime/task`＋`durable`
- 部署声明：`examples/wechat-bot/tagent.yaml`／`tagent.rl.yaml` **无需改动**（缺省全挂）

## 子步序（每步 `go build ./...`）

2.1 目录＋默认注册（`plugin_catalog.go`／`compress_mount.go`；boot 与宿主装配遍历）→ 2.2 `plugins:` 声明接通与定制（hostAgent/mapDocument/`agentPlugins`/错误路径）→ 2.3 七处硬编码清零（删 `run.go`/`tools.go`/`host.go` 具名构造与 append）→ 2.4 门禁三锚（golden 门 exit 0、自测 exit 0、build/test exit 0）。门绿是 W2 完成判据。

## 风险与回退

- **判据豁免面**：R5a 只跳过 `mounting.registry_implementation` 文件；本波把 7 项构造集中在册的 `plugin_catalog.go` 与 `compress_mount.go`，未新增豁免项、未改判据口径——`git diff` 对 `scripts/gates/` 与 `golden/` 为零。
- **行为保真锚**：`go test ./assembly/ ./api/config/ ./runtime/state/` 全绿；管线顺序既有断言（`tests/integration/state/`）保持绿；压缩折叠与 checkpoint 账面既有用例（`compress_consumer_test.go`／`compress_checkpoint_test.go`／`checkpoint_test.go`）保持绿。
- **声明语义差异**：今日「无声明＝全挂」与「声明＝禁用所选」，不是「声明＝白名单」。白名单会让一行无关声明把 `final-round` 等一起关掉（静默降级），故取禁用式定制。
- **回退**：恢复 `run.go:596-619` 具名构造与 append、撤 `tools.go`/`host.go` 转发（门红复现，作 F3 证据）＋删 `plugins` 字段映射（声明回死字段）。无数据迁移：不新增键、不改链字节、不改切片格式。
