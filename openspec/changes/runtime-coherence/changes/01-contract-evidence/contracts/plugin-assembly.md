# 契约 · 插件装配与材料分派（01.104 冻结）

- 候选指纹：`52d7b664521554fed8d7a5674ee1f934c410a3e3`（开工重核一致，分支 dev）。
- 依据：audit.md #1/#10/#11、evidence/anchors.md（S1＋未写项节）、evidence/state-probes.md C 段、`assembly/run.go:504-727,1330-1390,1533-1541`、`assembly/host.go:465-803`、`assembly/host_reload.go:200-392,791-821`、`assembly/plugin_catalog.go`、`api/plugin/material.go:143-257`、主规格 plugin-mounting／session-checkpoint-material、ADR-0004／0007。本文件只冻结契约，实现由 02.203／02.204／03.303 承接。

## 一、装配来源与统一接线

贡献来源三类、同一执行面：调用者经 `WithPlugins` 贡献的工厂；Boot 自家 host 级工厂（`hostPlugins(seam)`，按 builtinPluginCatalog 目录序）；run／agent 级 builtin（`mountBuiltinPlugins`／`agentPlugins`，目录序）。`Boot` 与 `Assemble` SHALL 经同一 `assemble` 路径装配；`Boot` MUST NOT 重建一份 options 丢弃调用者贡献（audit #1 已复现：`cfg.plugins` 在 host.go 零引用、builder 运行 0 次；`Assemble` 侧转发面本就完好）。

**组合语义裁定**：`assemblyConfig.plugins` 由单工厂改为**工厂序列**；`WithPlugins` 语义由赋值改为**追加**；声明序即执行序，各工厂**恰好执行一次**，产出实例按执行序并入 `r.plugins`。执行序固定：

1. 调用者贡献工厂（opts 声明序）；
2. Boot 自家 `hostPlugins` 工厂（仅 Boot 路径；保持现状位置＝Boot 自建 options 中的唯一工厂位）；
3. `mountBuiltinPlugins`（prepare 先行、run 级目录序，既有不变）。

`agentPlugins` 组装序（native 贡献 → `r.plugins` → agent 级 builtin）不变。否决：**Boot 内复制工厂逻辑**——第二真相，装配语义演进须同步两份，且复制正是 audit #1 的缺陷形态；**追加第二个 option 实例（保持赋值语义）**——后写覆盖前写，调用者贡献仍被吞，覆盖形态即 audit #1 复现路径。

## 二、reload 语义（audit #10 承接）

现状静态证实：`Reload` 只走 `loadHostDocument → publishDeclared`，全程无 plugins 触碰；Boot 装好的 run 级实例跨代存活；`WithPlugins` 契约原文 "the builder runs once … and the instances it answers are never replaced"（run.go:1536-1538）。两候选：

- **甲**：reload 经同一 assemble 路径重建插件集（含贡献）。
- **乙（所选）**：显式声明换代范围＝ADR-0007 代际作用域——reload 经代际注册表重建 generation-scoped 消费者；run 级插件集（贡献＋builtin）是进程作用域，跨代存活、MUST NOT 被 reload 重装，更换须重启生效。

选乙理由：甲与 `WithPlugins` 既有公共保证（runs once／never replaced）直接冲突，改公共保证须按 ORCHESTRATION 授权节停回设计门；甲另造第二套换代通道，违背 ADR-0007 已定的「换代＝代际注册表」单一机制；且无任何已复现场景需要运行中换 run 级插件（无独立于方案自身的第二使用方）。乙与 ADR-0007、reload 现行为、既有契约注释三方一致，把「未定义」升格为显式声明，零行为变化。对 S1 的影响：S1 断言面只涉 Boot 装配，乙不触碰该路径，S1 红转绿完全由 02.203 接线达成；乙下 reload 锚定为「贡献实例跨 reload 存活并继续观察」（见签发区三锚）。

## 三、Stateful 贡献入口（登记归属，不定义）

与 01.102 状态派生契约第 6 条同源，共同证据＝audit #11 与 anchors.md 未写项节（`assemblyConfig` 无状态切片贡献字段；`RegisterStateSlice` 仅 boot 后可用）。**归属＝02.204**，写面 `runtime/task/lifecycle.go`、`runtime/task/restore.go`、`runtime/task/checkpoint.go`、`runtime/state/durable.go`；本契约不重复定义其入口形态。

## 四、材料声明→分派一致

声明归一（`materialOf`：面自声明优先，否则六端口兼容读法）SHALL 驱动**同一**分派：`validateMaterial` 拒绝后经 `NewFlow` 唯一组装。六端口（`admitted` 表）与各自安全时机（pre-side-effect／post-commit／read）及失败语义 SHALL 保留；兼容读法作为公共契约的可选声明形态 MUST NOT 删除；按端口分派的行为 MUST NOT 因声明方式而改变（session-checkpoint-material 主规格原文）。**Governor 实际参与裁决**：声明了 PortCallRuling 材料的面，其裁决 SHALL 在真实调用路径被询问，不允许「声明在流、裁决走旁路」的分叉；**反向同样成立**：实现 `Judge` 但声明在其他端口的面 MUST NOT 参与裁决（`declaredGovernorOf` 只认 `Port()==PortCallRuling`，与 `phases.go` 收集条件一致）；无管线声明 Governor 时裁决 SHALL 回落 `WithPolicy`（再按既有语义回落审批/strict）。一致性核验与修齐由 **03.303** 承接，写面 `api/plugin/material.go`、`api/plugin/phases.go`、`assembly/tools.go`（`policyRuling`）、`assembly/plugin_catalog.go`（Governor 接线）＋测试。

## 五、失败清理与换代归属

- 工厂序列第 k 个失败：错误 SHALL 具名携带失败位置；已完成工厂产出的实例 MUST NOT 进入任何绑定或 flow（未挂接即无观察义务），其独占资源由产出工厂释放；清理对齐代际注册表既有 fail-closed 形态（`genregistry.go:141-157`：`errors.Join` 归并、不吞原始错误）。
- 换代归属：run 级集＝Boot 装配一次、进程生命周期（§二乙）；代际面＝ADR-0007 注册表。卸载／退役一个面时，若其尚有未完义务（保留的观察义务、未 drain 的借用），SHALL 具名拒绝而非静默卸下；整代切换、借还 drain 与退役释放归属 publication entry 及其台账（`genregistry.go:6-8`、`publish.go` refuse 形态）。

## 签发区

- **候选指纹**：`52d7b664521554fed8d7a5674ee1f934c410a3e3`。
- **已证/已驳引用**：audit #1（已复现，S1）、#10（静态证实 reload 不重建）、#11（静态证实 Stateful 无公开入口门）；state-probes.md C 段入口矩阵（Assemble 生效／Boot 不生效／reload 不重建）。
- **所选/否决**：组合语义＝工厂序列＋追加＋声明序执行＋各恰一次（否：Boot 复制工厂逻辑、第二 option 实例）；reload＝乙（否：甲）；材料＝声明归一驱动同一分派（否：删六端口、任意时机组合）。
- **02.203 精确写面**：`assembly/run.go`（`assemblyConfig.plugins` 序列化、`WithPlugins` 追加语义、assemble 执行序与失败清理）、`assembly/host.go`（`Boot` 转交调用者工厂、自家工厂并入序列）＋镜像测试。
- **03.303 精确写面**：§四所列四文件＋测试；Stateful 入口＝02.204（§三）。
- **接口变化**：`WithPlugins` 公共语义由「赋值（后写覆盖）」改为「追加（声明序各恰一次）」。现有调用方全列——生产：`assembly/host.go:690`（Boot 自家工厂，唯一）；测试：`assembly/symptom_anchor_coherence_test.go`（S1）、`tests/integration/host/meditation_event_purity_test.go`、`tests/integration/evolution/event_integrity_test.go`、`tests/integration/history/event_integrity_test.go`（Assemble 路径）。单工厂调用方行为不变。
- **三锚落点**：① 正锚＋消费者锚＝S1 `TestSymptomAnchorHostBootMountsContributedPlugin`（公开 `Boot`＋`WithPlugins`＋`Run.Invoke`，02.203 交付时红转绿）；② 负锚＝`TestFailedPluginFactoryReleasesCompletedFactories`（02.203 交付：序列失败释放已完成部分＋具名错误）；③ reload 乙语义锚＝`TestReloadKeepsContributedPluginsObservingCommits`（02.203 交付：贡献跨 reload 存活并继续观察，经真实 `Boot`＋`Reload` 入口）。
- **相邻回归**：`TestHostPluginsMountThroughTheCatalog`、`TestDeclaredPluginChoiceSurvivesRestart`、`TestPluginDeclarationRefusesAnUnregisteredName`（plugin_catalog_test.go）；Assemble＋`WithPlugins` 既有集成用例（event_integrity 等）；`scripts/gates/check_plugin_mounting.py` 门禁。
- **回滚触发**：S1 回红；任一相邻回归红；组合语义变化改变既有单工厂用例行为；reload 锚显示贡献被卸载或重建（乙违约）。
- **独立审**：待。
