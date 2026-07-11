## 1. plan 子 agent 配置与 Prompt

- [x] 1.1 编写 `examples/wechat-bot/resources/prompts/plan_agent.md`：plan agent 的 system prompt，定义角色（工作计划管理）、工具使用说明（exec 调用 openspec CLI、read_file/save_file 读写 change 文件）、输出格式
- [x] 1.2 编写 `examples/wechat-bot/resources/prompts/plan_tool_desc.md`：tagent 调用 plan agent 的工具描述文件
- [x] 1.3 在 `examples/wechat-bot/tagent.yaml` 的 `agents:` 中添加 `plan` agent 定义（model、system_prompt、tools: exec + read_file + save_file）
- [x] 1.4 在 `examples/wechat-bot/tagent.yaml` 的 tagent agent `tools:` 中注册 `- agent: plan`

## 2. PlanProgressTracker 组件

- [x] 2.1 新建 `agent/plan_progress_tracker.go`，定义 `PlanProgressTracker` 结构体（持有 `openSpecDir` 路径）
- [x] 2.2 实现 `scanActiveChanges()` 方法：扫描 `openspec/changes/` 目录（排除 `archive/`），返回活跃 change 名称列表
- [x] 2.3 实现 `parseTasksMd(path string)` 方法：读取 tasks.md，解析 `- [ ]` / `- [x]` checkbox，返回任务列表和完成统计
- [x] 2.4 实现 `buildProgressSummary(changeName string, tasks []TaskItem) string` 方法：生成进度摘要字符串
- [x] 2.5 实现 `InjectProgress` BeforeModel 回调方法：扫描活跃 change，若恰好 1 个则读取 tasks.md 并注入摘要 system message 到 messages 末尾

## 3. 配置化

- [x] 3.1 在 `config.go` 的 `AgentConfig` 中新增 `OpenSpecDir string` 字段（`json:"openspec_dir,omitempty" yaml:"openspec_dir,omitempty"`）
- [x] 3.2 在 `config.go` 的 `applyDefaults` 中设置默认值：空时默认 `"."`
- [x] 3.3 在 `agent/tagent_agent.go` 的 `TagentConfig` 中新增 `OpenSpecDir string` 字段
- [x] 3.4 在 `tagent.go` 的 `buildAgent` 中传递 `acfg.OpenSpecDir` 到 `agentCfg.OpenSpecDir`
- [x] 3.5 在 `examples/wechat-bot/tagent.yaml` 的 tagent agent 中添加 `openspec_dir: "."` 配置项

## 4. 回调注册

- [x] 4.1 在 `agent/tagent_agent.go` 的 `newContextManagerFromConfig` 中创建 `PlanProgressTracker`（使用 `cfg.OpenSpecDir`）
- [x] 4.2 在 `agent/context_manager.go` 的回调链中注册 PlanProgressTracker（在 SmartCompressor 和 Compactor 之后，诊断日志之前）
- [x] 4.3 添加 Debug 日志：`[PlanProgressTracker] active change: <name>, progress: <done>/<total>`

## 5. FrameworkPrompt 更新

- [x] 5.1 在 `agent/tagent_agent.go` 的 `FrameworkPrompt` 常量中增加 plan agent 使用说明段落
- [x] 5.2 内容包括：复杂多步骤任务调用 plan 工具创建计划；plan agent 通过 openspec 管理 proposal + tasks；框架自动注入进度摘要；完成后调用 plan 归档

## 6. 测试与验证

- [x] 6.1 新增 `agent/plan_progress_tracker_test.go`：测试 scanActiveChanges（0/1/多个活跃 change）、parseTasksMd（正常/空文件/缺失文件）、buildProgressSummary 格式
- [x] 6.2 测试 BeforeModel 注入：恰好 1 个活跃 change 时注入摘要、0 个时不注入、多个时不注入
- [x] 6.3 `go build ./...` 通过
- [x] 6.4 `go test ./agent/...` 全部通过
