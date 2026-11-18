# 01 预算三量归位 · 设计

> 静态决策见一级 [design.md](../../design.md) §三缺陷A、§四 D1–D5、§五 S1–S4、§六 D-01。本域只列实施要点与实证触碰集。

## 技术要点（承一级 D1–D5，乙已裁定）

1. **rename 双层 schema**（D1）：`AgentSpec.MaxTokens→ReplayWindow`（声明键 `max_tokens→replay_window`）、`AgentSpec.ProviderContextWindow→ContextWindow`（键 `provider_context_window→context_window`）。核心层 `decode.go` agentKeys + `fingerprint.go` canonical JSON tag + `clone.go` + `defaults.go` + `skeleton_assert.go`；宿主层 `host_document.go` hostAgent yaml 标签 + `mapDocument`。
2. **输出限真发**（D2）：新增输出限字段（收回 `max_tokens` 名，`AgentSpec.MaxOutputTokens` 或等价），经 `hostParameterizedModel.params`→`decorate`（host_model.go:121）填 `GenerationConfig.MaxTokens`。**单一来源**：有效输出限算一次，同供 decorate（发 provider）与 budgetReserve（扣预算）。
3. **reserve 派生 + 门乙**（D2/D3/D4）：`budgetReserve = 有效输出限 ?? 0`；`budgetWindow` 去 `|| MaxTokens`，直接返回 `ContextWindow`，门开 ⟺ `ContextWindow>0`。删 `output_reserve` 字段、`DefaultOutputReserveDivisor`、`OutputReserveOf`（request_assembly.go）。
4. **注册表重分类**（D5）：`window_bounds.json` → `replay_window`=window 类（保留唯一 window，source 不变）、`context_window`+`max_tokens`=budget 类、删 output-reserve/output-reserve-divisor 条目。门 `check_window_bounds.py` 强制 windows==1。
5. **测试归位**：`TestProviderWindowGateSemantics` 的 derived 例翻为门闭整包（S4）；`TestBudgetDefaultOutputReserve`（钉 divisor=4）整测删改；`TestFoldWindowSemanticsUnchanged`（request_test.go:903）作 S3 回归锚保持绿；全 `AgentSpec{MaxTokens/ProviderContextWindow/OutputReserve}` 构造改名。

## 实证触碰集（✅ grep，不照搬一级清单）

- `api/config/`：config.go:79-97（字段+注释）、decode.go:158-165/205/214/217、fingerprint.go:63-73/212、clone.go、defaults.go:22（折叠缺省，rename 不改逻辑）、skeleton_assert.go:44、config_test.go、fingerprint_test.go
- `assembly/`：host_document.go:108-110/198-200、request.go:853/865/611、host_model.go:121、host_reload.go:719/721、facts.go（核 MaxTokens 语义）、request_test.go:760/801/903
- `runtime/agent/`：request_assembly.go（OutputReserveOf/DefaultOutputReserveDivisor/WithBudget:103/inputBudget:154）、loop.go:929
- `golden/runtime/window_bounds.json`
- `tests/integration/`：history/event_integrity、journey/{journey_harness,journey_fold}、host/{projection_assembly,compress_pipeline,age_fold}（ProviderContextWindow 构造）
- `examples/wechat-bot/smoke_test.go`（AgentSpec 预算字段；FoldWindow:2192/2393/3256 保留）
- **不改**：age_fold.go（FoldWindow.MaxTokens 字段+VolumeThresholdBytes 保留，仅注释文字归位）、memory/replay（✅ 零 MaxTokens）

## 子步序（每步 go build 保持可编译）

1.1 rename → 1.2 输出限 → 1.3 reserve 派生+门乙 → 2.1 params 删 → 1.4 注册表（门绿）→ 1.5 测试归位。（R1 更正：门 `check_window_bounds.py` 扫活键集，注册表 D5 终态须 `max_tokens` 输出限生（1.2）、`output_reserve` 删（1.3）后方可 windows==1 全注册，故 1.4 移至键集落定后；门绿是 W1 完成判据非每子步，build 绿才是每子步。详见一级 ORCHESTRATION §三）

## 风险与回退

- **风险**：MaxTokens 三类语义混淆（AgentSpec 回放窗改／GenerationConfig 输出限保留／FoldWindow 折叠窗保留）→ 逐文件 grep 上下文区分，机械 rename 后 `go build` 验证。
- **回退**：本域是破坏性归位（用户裁定），无旧键兼容。回退=git revert 整原子单；S1 回滚必红作 F3 证据。