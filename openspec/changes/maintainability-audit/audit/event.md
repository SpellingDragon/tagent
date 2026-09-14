# event 包评分卡

base: `cf006e1` | 4 文件 / 748 行（测试 362 行）| cover 49.7% | 取证: 全文读 + `go test ./event/ -cover`

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | S | 四文件单一职责（types 常量 / registry 注册表 / metadata 键契约 / timeline 前缀读写）；纯函数 + RWMutex 注册表，分支可数 |
| 耦合 | S | 叶子包，零内部依赖；对上游仅暴露常量与纯函数（派生访问器只读注册表） |
| 测试 | B | cover 49.7%（包内最低）；registry 派生访问器与 defaultSpec fallback 有测，`GenerateEventSummary` 的 `summarizeToolResult` JSON 摘要多分支（types.go:236-316）覆盖薄弱 |
| 文档一致 | A | wiki/event 与 14 类型注册表已同步；遗留两处 🟡（F-1 trpcclaw 残留、F-2 常青注释引归档路径） |
| 演进风险 | A | EventTypeSpec 单点注册路径清晰；「冻结纪律」明示但引用对象易腐（F-2） |

发现: F-1、F-2、F-3（见 summary.md）
