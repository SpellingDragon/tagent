# 02 死字段清除（params）

## Why

一级 design §三缺陷B：`AgentSpec.Params`（map[string]float64）无任何行为消费者——✅ grep 全 .go 引用皆是声明面自身 machinery（decode/clone/fingerprint）+ 一个 reload 日志标签（host_reload.go:719）+ 测试 fixture；远端宿主 `hostAgent` 根本不暴露 params 键。违元规则二（结构不得自证）：其「消费者」全服务于声明面自身，无独立行为需求。

## What

删 `AgentSpec.Params` + decode/clone/fingerprint 伴生 + host_reload note("params")；fingerprint_test 的 Params fixture 改用 Temperature 测负零归一。

## 边界与依赖

- **依赖**：与 01 同簇（config.go/decode.go/fingerprint.go/clone.go），**合单原子**（W1）。
- **被依赖方**：无（死字段，无行为消费者）；指纹 canonical 编码变更触发 republish 属预期。
- **接口面**：`api/config` leaf 性质不变；`ToolRef.Params`/`Refs[].Params`（string map，presets/tools/criteria/decode:617/clone:108/fingerprint:385）是**独立字段，保留**。
- **禁止事项**：不触 ToolRef.Params（另一字段）；不触 MCP req.Params.Arguments（无关）；不触 go/types Params()（函数签名，无关）；与 01 合单不并行。