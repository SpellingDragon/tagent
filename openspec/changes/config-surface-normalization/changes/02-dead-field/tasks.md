# 02 死字段清除 · 孙任务

> 与 01 合单原子（W1）。verify 三要素随任务行冻结。

- [ ] 2.1 删 `AgentSpec.Params` + decode:199/clone:58/fingerprint:210 伴生 + host_reload:719 note("params")；fingerprint_test:142/143/300 fixture 改用 Temperature 测负零归一 —— 验证：正锚 `go test ./api/config/... -run TestFingerprint`（负零归一仍绿，用例数>0）；负锚 声明 params 键→`DecodeStrict` 拒（ErrUnknownConfigKey）；消费者锚 `grep -rn "\.Params" --include=*.go` 仅余 ToolRef.Params/Refs[].Params（string map）与 MCP/go-types 无关引用，AgentSpec.Params 零残留。