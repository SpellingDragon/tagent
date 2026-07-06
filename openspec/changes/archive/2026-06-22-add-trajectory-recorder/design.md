## Context

tagent 当前有两种运行模式：
1. **普通模式**（tagent.yaml）：LLM 请求直接发往智谱AI（`open.bigmodel.cn`），不记录任何数据
2. **RL 训练模式**（tagent.rl.yaml）：通过 `SwappableModel` 将 LLM 请求转发到 AReaL proxy（SGLang），所有 RL 数据（logprobs、completion_ids、reward）在 AReaL 侧记录

经探索确认：
- AReaL 的 `ArealOpenAI.create()` 直接调用 `engine.agenerate()`，不是 HTTP 转发，无法代理智谱AI 请求
- AReaL 不支持离线 RL（从预收集轨迹训练 PPO），只有 SFT（`SFTTrainer`）支持预收集数据
- trpc-agent-go 的 `model.Choice` 没有 `Logprobs` 字段，`GenerationConfig` 没有 logprobs 参数
- AReaL 的 trajectory dump 格式包含 `prompt_text` + `completion_text` + `reward`，是单向输出

因此需要在 tagent 侧新增轨迹记录层，使日常运行时收集的轨迹可后续转为 AReaL SFT dataset 或 RL prompt dataset。

## Goals / Non-Goals

**Goals:**
- tagent 在任何模式（智谱AI / AReaL proxy）下都能记录 LLM 调用轨迹
- 轨迹以 JSONL 格式持久化到本地磁盘，按 session 组织
- 记录格式包含完整 messages、tool calls、response、usage、timing、metadata
- 提供转换脚本，将 JSONL 转为 AReaL SFT dataset（`input_ids` + `loss_mask`）或 RL prompt dataset（`messages`）
- TrajectoryRecorder 与 SwappableModel 可组合：`TrajectoryRecorder(SwappableModel(realModel))`

**Non-Goals:**
- 不在 tagent 侧捕获 logprobs（trpc-agent-go 接口限制）
- 不实现离线 RL 训练（AReaL 不支持，超出 tagent 范围）
- 不修改 AReaL 源码
- 不修改 trpc-agent-go 的 model 接口
- 不实现 reward 计算（reward 留给离线标注或 AReaL 训练时计算）

## Decisions

### 1. TrajectoryRecorder 实现 model.Model 接口（而非 plugin/hook）

**选择**：TrajectoryRecorder 实现 `model.Model` 接口，包装内部 model。

**理由**：
- model.Model 是 tagent 所有 LLM 调用的唯一入口，在此层记录可覆盖所有 agent 的所有调用
- 与 SwappableModel 同层，可组合：`TrajectoryRecorder(SwappableModel(openai.New(...)))`
- 不侵入 runner/llmagent 内部逻辑
- 替代方案（plugin hook、event channel 拦截）需要深入框架内部，且无法捕获完整的 request 对象

### 2. JSONL 格式，每 session 一个文件

**选择**：`{trajectory_dir}/{session_id}.jsonl`，每行一条 LLM 调用记录。

**理由**：
- JSONL 是 AReaL trajectory dump 的现有格式，天然兼容
- 按 session 组织便于后续按会话粒度筛选和转换
- 追加写入，性能好，不阻塞 LLM 调用
- 替代方案（SQLite、单文件全量 JSON）增加复杂度或写入锁竞争

### 3. 异步写入（buffered channel + 后台 goroutine）

**选择**：TrajectoryRecorder 内部维护一个 buffered channel，LLM 调用完成后将记录推入 channel，后台 goroutine 负责写盘。

**理由**：
- 写盘不应阻塞 LLM 调用链路
- channel 满时丢弃记录并打 warning log（不阻塞主流程）
- 替代方案（同步写盘）在磁盘慢时会拖慢 LLM 响应

### 4. 转换脚本用 Python（而非 Go）

**选择**：`areal/convert_trajectories.py`，Python 实现。

**理由**：
- AReaL 是 Python 生态，转换后直接用 HuggingFace `datasets` 库加载
- 需要 tokenizer 做 `input_ids` + `loss_mask` 编码，Python 有 `transformers` 库
- Go 侧只负责记录原始文本，Python 侧负责 tokenization 和格式转换

### 5. 配置项放在 tagent.yaml 顶层

**选择**：`trajectory_dump: true` 和 `trajectory_dir: "data/trajectories"` 放在 Config 顶层。

**理由**：
- 与 `api_endpoint`、`model` 等运行时配置同级，语义清晰
- tagent.New() 读取配置后决定是否包装 TrajectoryRecorder
- 默认关闭（`trajectory_dump: false`），用户显式开启

## Risks / Trade-offs

- **[性能开销]** 每次额外序列化 + channel 推送 → 异步写入 + channel 满丢弃，预期 <1ms 开销
- **[磁盘空间]** 长期运行积累大量 JSONL → 配置项控制开关，转换脚本支持按日期/session 筛选
- **[缺少 logprobs]** 轨迹只有文本，没有 per-token logprobs → 后续可用训练模型重新计算 logprobs；SFT 不需要 logprobs
- **[tool_calls 序列化]** ToolCall 含 `[]byte` Arguments → JSON 序列化时 base64 编码，转换脚本解码
- **[并发安全]** 多 agent 共享同一个 TrajectoryRecorder → 用 `sync.Mutex` 保护文件句柄或每 agent 独立 recorder
