package memory

// testBaseMs 是记忆包测试共用的固定基准毫秒时间戳（确定性 EventKey 生成）。
// memory/engine 子包测试持有一份同名副本（Go 测试不跨包共享 helper）。
const testBaseMs = int64(1750000000000)

// TypeExternalInputProbe 是测试专用的探针事件类型（不进生产类型注册表）。
const TypeExternalInputProbe = "external_input"
