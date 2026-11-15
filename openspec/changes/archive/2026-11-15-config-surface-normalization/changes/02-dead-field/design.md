# 02 死字段清除 · 设计

> 静态决策见一级 [design.md](../../design.md) §三缺陷B、§四 D6。

## 实证触碰集（✅ grep `\.Params`，区分 AgentSpec vs ToolRef）

**删（AgentSpec.Params，float map）**：
- `api/config/decode.go:199`（floatMapField）、`clone.go:58`（cloneFloatMap）、`fingerprint.go:210`（canonicalFloatMap）
- `assembly/host_reload.go:719`（note "params"）
- `api/config/fingerprint_test.go:142/143/300`（Params["temperature"] fixture → 改 Temperature 字段测负零归一）
- `api/config/config.go`（Params 字段定义）

**保留（ToolRef.Params / Refs[].Params，string map，独立字段）**：
- presets.go:149-193、tools.go:373/411/416、host_reload.go:664-671、decode.go:617（stringMapField）、clone.go:108（cloneStringMap）、fingerprint.go:385（canonicalStringMap）、criteria.go:73-81、fingerprint_test.go:295/297/308/318/324
- MCP req.Params.Arguments（mcp_lifecycle_test/unified_intake_test/smoke_mcp_test，无关）
- go/types Params()（event_ownership.go:924-931，函数签名，无关）

## 风险与回退

- **风险**：误删 ToolRef.Params（同形异义）→ grep 上下文区分 float map（AgentSpec）vs string map（ToolRef），只删 float map 链。
- **回退**：git revert 合单；负锚=声明 params 键→decode 拒（unknown field）。