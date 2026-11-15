# 01 预算三量归位（核心原子域）

## Why

一级 design §三缺陷A：预算三量名实不符——① 凭空 `window/4` 扣 reserve（c75：未声明 output_reserve 却扣 32000）；② 余量与真实输出限脱钩，实现背离自身 spec（`budgetReserve` 从不读 `GenerationConfig.MaxTokens`，生产恒 nil）；③ `max_tokens` 名实不符（实为回放窗，非 API 通用输出限）。用户裁定**破坏性归位 + 远端同步**、**整个 agent 配置面**、D4=**乙**（预算门唯一来源 context_window）。

## What

rename 双层 schema（`MaxTokens→ReplayWindow`、`ProviderContextWindow→ContextWindow`，收回 `max_tokens` 作输出限）+ 输出限经 `decorate` 真发 provider + `budgetReserve` 派生自有效输出限 `?? 0` + `budgetWindow` 去 `|| MaxTokens`（门开 ⟺ `ContextWindow>0`，乙）+ 删 `output_reserve` 字段/`DefaultOutputReserveDivisor`/`OutputReserveOf` + `window_bounds.json` 重分类 + 测试归位。

## 边界与依赖

- **依赖**：`runtime/agent` 预算计量（`WithBudget`/`inputBudget`/九面，已存在）；`assembly/host_model` 的 `EffectiveGenerationConfig`/`decorate` 接缝（已存在，本期接上输出限）；`FoldWindow`（`assembly/age_fold.go:25`，内部字段 `MaxTokens` **保留**，主 config-hot-reload spec:42 已钉）。
- **被依赖方**：02（params 删，同簇合单）；03（spec/GLOSSARY 用新键）；04（yaml 用新键）；05（godoc 用新键）；远端 wechat-bot 部署；所有构造 `AgentSpec{MaxTokens/ProviderContextWindow/OutputReserve}` 的测试。
- **接口面**：`api/config` leaf 性质不变（仅依赖 stdlib）；双层 schema（`decode.go` agentKeys + `host_document.go` hostAgent yaml 标签）须同步；`foldWindowOf`（request.go:611）是唯一映射点，改读 `spec.ReplayWindow` 填 `FoldWindow.MaxTokens`（内部字段名不变）。
- **禁止事项**：
  - 不触外来未跟踪五件、冻结归档、§五冻结拼写（reincarnation 族）。
  - **不合并回放窗与预算窗**——尊重 `window_bounds.json` 明载的「刻意解耦」（smoke 入口=2×回放窗，r32 的 1519/1500）；归位保留两量分立，只改名与派生关系。
  - `FoldWindow.MaxTokens`（内部折叠字段）、`GenerationConfig.MaxTokens`（provider 输出限本义）、`ToolRef.Params`（独立 string map）**保留**。
  - 不新增 model 级容量注册表（D7，C5 完整形态留未来）。
  - 与 02 合单原子（触碰集相交 + 门强耦合），不并行派发。