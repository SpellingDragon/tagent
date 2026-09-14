# plugin 包评分卡

base: `cf006e1` | 4 文件 / 417 行（测试 393 行）| 取证: memory_plugin.go 全文 + 其余三文件结构读

| 维 | 级 | 依据 |
|----|----|------|
| 复杂度 | A- | memory_plugin.go 265 行为最大件（OnEvent 过滤三分支 + 归因盖章 + stored-gate 同点投影）；attribution/projection_sink 各 ~37 行窄接口 |
| 耦合 | A | 依赖倒置干净（ctx 载体 WithAttribution/WithProjectionSink）；归因键权威源统一在 event 包（metadata.go） |
| 测试 | A- | 393 测试行；stored-gate 三态/spill 双写有测（persist_bus_event_test 在 agent 包侧） |
| 文档一致 | A | 过滤三分支（nil/partial/退化空 final）注释带设计出处与不变量编号（D8/D1/H1） |
| 演进风险 | A- | stored-gate 失败窗的 settle feedback 丢弃为已文档化窗口（R1 归档已知项）；新过滤分支需守 invariant 注释纪律 |

发现: 无
