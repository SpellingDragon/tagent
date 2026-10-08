# Design: X5 训练采集探索

## 主读路径

`rl/trajectory_recorder.go`（TrajectoryRecord/LLMCallRecord 全字段与写协程）、`rl/swappable_model.go`（端点切换录制）、`rl/agent_loop.go`（解耦缝）、`memory/feedback.go`（verdict/rating/event_key 绑定）、`agent/` 侧 trace_id 注入点（与 X1 交叉）、`scripts/convert_trajectories.py`（SFT/RL 两模式全读）、`examples/wechat-bot/tagent.rl.yaml`（RL 部署形态）。

## 核验假设详单

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | TrajectoryRecord 无 reward/verdict 字段，无配置快照（D6/D5） | 全字段核对 + grep |
| H2 | feedback 绑 event_key 且无 trace_id 反查路径（D6） | 读 BindFeedback 与查询 API 面 |
| H3 | 转换器 SFT 无 chat template、RL 无 reward 列、同 episode 重复 prompt（D6） | 全读 convert_trajectories.py 两模式 |
| H4 | 通道满丢记录仅日志告警、无丢失率暴露（D6） | 读 recorder 写协程的满时分支 |
| H5 | state≡模型实际所见（截断后忠实）成立（D6 正面结论复核） | 核对录制点在装饰链的位置（是否包住最终请求） |
| H6 | "随运行采集"的最小改造面 = 轨迹行补 event_key 列 + 转换器 join（上轮探索设想） | 定位 TrajectoryRecord 可注入 event_key 的最小改动点（只定位不实现）；核对事件→trace_id 查询能力（引 X3 查询面清单交叉） |
| H7 | 延迟反馈/取消/部分成功的归因语义在 feedback 模型中有无承载（E10 补白） | 读 feedback 事件结构与因果边类型全集 |

## 现有验证命令候选

`go test ./rl/ -count=1`；`python3 -c "import ast;ast.parse(open('scripts/convert_trajectories.py').read())"`（语法级验证，只读）。

## 风险与回退

AReaL 对接形态证据不足的结论维持（桥已删），探索不试图臆断对接方式；H6 改造面只定位文件:符号，不写代码。
