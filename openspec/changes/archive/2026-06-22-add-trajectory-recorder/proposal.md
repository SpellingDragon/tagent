## Why

tagent 在日常运行（智谱AI 模式）时不记录任何轨迹数据，无法积累用于 RL 训练的数据。AReaL 的 proxy 只能使用内置 SGLang 引擎、不能转发到外部 API，且 AReaL 不支持离线 RL（从预收集轨迹训练 PPO）。因此必须在 tagent 侧补齐数据记录与持久化能力，使日常运行时收集的轨迹可后续用于 AReaL SFT 或作为在线 RL 的 prompt dataset。

## What Changes

- 新增 `TrajectoryRecorder`：实现 `model.Model` 接口的包装层，记录每次 LLM 调用的 request/response（messages、tool calls、usage、timing、metadata），与 `SwappableModel` 可组合
- 新增磁盘持久化层：以 JSONL 格式按 session 写入，路径可配置（默认 `data/trajectories/`）
- 新增配置项：`tagent.yaml` / `tagent.rl.yaml` 中添加 `trajectory_dump`（bool）和 `trajectory_dir`（string）
- 新增数据转换脚本：`areal/convert_trajectories.py`，将 tagent JSONL 转为 AReaL SFT dataset（`input_ids` + `loss_mask`）或 RL prompt dataset（`messages`）
- 更新 `examples/wechat-bot/main.go`：在 model 链路中集成 `TrajectoryRecorder`
- 更新 `run.sh`：普通模式和 RL 模式均启用轨迹记录
- 更新 wiki §7、README.md、areal/README.md：新增离线数据收集章节

## Capabilities

### New Capabilities

- `trajectory-recording`: tagent 运行时的 LLM 轨迹记录与磁盘持久化，覆盖普通模式（智谱AI）和 RL 训练模式（AReaL proxy），记录格式兼容 AReaL SFT/RL dataset 转换

### Modified Capabilities

（无现有 spec 需要修改）

## Impact

- **agent/**: 新增 `trajectory_recorder.go`（TrajectoryRecorder 类型）
- **config.go**: 新增 `TrajectoryDump bool` 和 `TrajectoryDir string` 字段
- **examples/wechat-bot/main.go**: model 包装链路加入 TrajectoryRecorder
- **examples/wechat-bot/run.sh**: 启动时设置轨迹目录
- **areal/**: 新增 `convert_trajectories.py` 转换脚本
- **docs/wiki/agent/agent-architecture.md**: §7 新增离线数据收集小节
- **README.md**: 新增轨迹记录说明和数据流转图
- **areal/README.md**: 新增数据转换路径说明
- **tagent.yaml / tagent.rl.yaml**: 新增 trajectory_dump / trajectory_dir 配置项
