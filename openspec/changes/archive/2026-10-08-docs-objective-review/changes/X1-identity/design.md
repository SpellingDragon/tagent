# Design: X1 身份与事实归属探索

## 主读路径

`event/metadata.go`（MetaKey 全集）、`agent/event_loop.go`（turn/invocation 生命周期与 span）、`agent/settle_routing.go`（invID 绑定表）、`agent/event_bus.go`（durable seq）、`agent/context_manager.go`（turn_key/trace 绑定）、`rl/trajectory_recorder.go`（trace_id/span_id 落账）、`plugin/memory_plugin.go`（因果链游标）、`memory/feedback.go`（event_key 绑定）。

## 核验假设详单（≥5 行核验表素材）

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | 事件 Metadata 携 trace_id/span_id，三投影互链（D2） | 读 event/metadata.go MetaKey 常量 + 事件写入点（MemoryPlugin/persistBusEvent）是否注入 |
| H2 | TrajectoryRecord 无 event_key，仅 trace_id/span_id（D6） | 全字段核对 trajectory_recorder.go；grep event_key 于 rl/ |
| H3 | BuildInvocation 按到达序合并不按 Timestamp 排序（D2） | 读 context_manager.go:1012-1035 合并逻辑 + event_bus.go Pull 批序 |
| H4 | 幽灵前驱：lastEventKeys 不受 stored 约束（D2） | 读 plugin/memory_plugin.go:179-184 及其测试覆盖面 |
| H5 | reward 三跳 join（轨迹.trace_id→事件→event_key←feedback）理论可行但无契约无工具（汇总对撞裁决） | 逐键存在性实证：事件查询能否按 trace_id 过滤（QueryEvents 能力面） |
| H6 | invocation/turn/task_id/generation 四标识的拥有者与只读面清晰（E1 正面问题） | 普查各标识的定义点、写入点、读取点，画归属表 |

## 现有验证命令候选

`go test ./event/ -count=1`、`go test ./plugin/ -count=1`（白盒契约测试作为标识语义的证据）；必要时 `go test ./rl/ -run TestTrajectory -count=1`。

## 风险与回退

三跳 join 的中间查询能力（按 trace_id 查事件）若无索引支撑，"理论可行"要降级为"需新建索引才可行"——这正是探索要回答的，如实记录。
