## 1. Core: TrajectoryRecorder 实现

- [x] 1.1 创建 `agent/trajectory_recorder.go`：定义 `TrajectoryRecorder` 类型，实现 `model.Model` 接口（`GenerateContent` + `Info`），包装内部 model
- [x] 1.2 实现 JSONL 记录结构体：`TrajectoryRecord`（timestamp, session_id, user_id, batch_index, llm_call, metadata）
- [x] 1.3 实现异步写入层：buffered channel + 后台 goroutine，channel 满时丢弃并打 warning log
- [x] 1.4 实现 `Close()` 方法：flush 剩余记录并关闭文件句柄
- [x] 1.5 实现 `SetSessionInfo(userID, sessionID string)` 方法：供 loop 调用以设置当前 session 上下文
- [x] 1.6 实现并发安全：`sync.Mutex` 保护 session 信息和文件句柄

## 2. Core: 配置与集成

- [x] 2.1 在 `config.go` 的 `Config` 结构体中添加 `TrajectoryDump bool` 和 `TrajectoryDir string` 字段（yaml tag: `trajectory_dump`, `trajectory_dir`）
- [x] 2.2 在 `config.go` 的 `ApplyDefaults()` 中设置默认值：`TrajectoryDump: false`，`TrajectoryDir: "data/trajectories"`
- [x] 2.3 在 `tagent.go` 的 `New()` 函数中：当 `TrajectoryDump == true` 时，用 `TrajectoryRecorder` 包装 model
- [x] 2.4 在 `tagent.go` 中将 TrajectoryRecorder 注册为 Closer，确保 graceful shutdown 时 flush

## 3. Core: 单元测试

- [x] 3.1 创建 `agent/trajectory_recorder_test.go`：测试 TrajectoryRecorder 基本记录功能（mock model → 验证 JSONL 输出）
- [x] 3.2 测试异步写入：验证 channel 满时不阻塞 LLM 调用
- [x] 3.3 测试 Close() flush：验证关闭后所有记录已写入磁盘
- [x] 3.4 测试与 SwappableModel 组合：`TrajectoryRecorder(SwappableModel(mock))` → Swap 后记录新 endpoint

## 4. Example: wechat-bot 集成

- [x] 4.1 更新 `examples/wechat-bot/tagent.yaml`：添加 `trajectory_dump: true` 和 `trajectory_dir: "data/trajectories"`
- [x] 4.2 更新 `examples/wechat-bot/tagent.rl.yaml`：添加 `trajectory_dump: true` 和 `trajectory_dir: "data/trajectories"`
- [x] 4.3 更新 `examples/wechat-bot/main.go`：在 model 创建链路中集成 TrajectoryRecorder（普通模式 + RL 模式）
- [x] 4.4 更新 `examples/wechat-bot/main.go`：确保 TrajectoryRecorder 的 session info 在 StartLoop 时设置
- [x] 4.5 更新 `examples/wechat-bot/run.sh`：在 `.gitignore` 或启动脚本中处理 `data/trajectories/` 目录创建

## 5. 转换脚本

- [x] 5.1 创建 `areal/convert_trajectories.py`：CLI 参数 `--input`, `--output`, `--tokenizer`, `--mode (sft|rl)`
- [x] 5.2 实现 SFT 模式：读取 JSONL → 提取 prompt + completion → tokenizer encode → 输出 `{input_ids, loss_mask}` HuggingFace Dataset
- [x] 5.3 实现 RL 模式：读取 JSONL → 提取初始 user message → 输出 `{messages}` HuggingFace Dataset
- [x] 5.4 支持多文件批量读取：遍历 `--input` 目录下所有 `.jsonl` 文件

## 6. 文档: wiki

- [x] 6.1 更新 `docs/wiki/agent/agent-architecture.md` §7：新增 "§7.6 离线数据收集（TrajectoryRecorder）" 小节
- [x] 6.2 在 §7.6 中说明：架构图（TrajectoryRecorder 包装层）、JSONL 格式、与 AReaL SFT/RL 的转换路径
- [x] 6.3 在 §7.6 中添加数据流转图：日常运行 → JSONL → convert_trajectories.py → AReaL SFT/RL dataset

## 7. 文档: README

- [x] 7.1 更新 `README.md`：新增 "轨迹记录" 小节，说明配置项和数据格式
- [x] 7.2 更新 `README.md` 架构图：在 tagent 内部添加 TrajectoryRecorder 层
- [x] 7.3 更新 `README.md` 配置表：添加 `trajectory_dump` 和 `trajectory_dir` 行
- [x] 7.4 更新 `areal/README.md`：新增 "Offline Data Pipeline" 小节，说明 convert_trajectories.py 用法
- [x] 7.5 更新 `README_EN.md`：同步轨迹记录相关描述

## 8. 构建验证

- [x] 8.1 `go build ./agent/...` 编译通过
- [x] 8.2 `go test ./agent/ -run "TestTrajectoryRecorder" -count=1 -v` 测试通过
- [x] 8.3 `go build -o /dev/null .` (wechat-bot) 编译通过
- [x] 8.4 `bash -n examples/wechat-bot/run.sh` 语法检查通过
- [x] 8.5 验证 `python3 areal/convert_trajectories.py --help` 可正常运行
