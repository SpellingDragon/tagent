## 1. 清理死代码

- [x] 1.1 删除 `builtin.go` 中的 `decodeProperties[T]` 函数（行 49-58）
- [x] 1.2 删除 `builtin.go` 中的 `encoding/json` import（如果不再需要）
- [x] 1.3 更新 README.md 和 wiki/tool-architecture.md 中对 `decodeProperties` 的引用

## 2. 保护内置 agent name

- [x] 2.1 在 `tagent.go` 中定义 `builtinAgentNames` map，包含 7 个内置 agent name
- [x] 2.2 修改 `buildAgent` 函数，在检查 `GetToolAgentFactory` 前先判断 `builtinAgentNames[name]`，如果为 true 则跳过 factory 路径
- [x] 2.3 添加注释说明保护机制的设计意图

## 3. 删除 read/write/speak/draw 的 NewTool 便利函数

- [x] 3.1 删除 `tool/read/read_agent.go` 中的 `NewTool` 函数和 `resolveDescription` 辅助函数
- [x] 3.2 删除 `tool/write/write_agent.go` 中的 `NewTool` 函数和 `resolveDescription` 辅助函数
- [x] 3.3 删除 `tool/speak/speak_agent.go` 中的 `NewTool` 函数和 `resolveDescription` 辅助函数
- [x] 3.4 删除 `tool/draw/draw_agent.go` 中的 `NewTool` 函数和 `resolveDescription` 辅助函数
- [x] 3.5 确保这些包只保留 `NewAgent` 函数（config-driven 路径使用）

## 4. 文档补充 actionFactory properties 字段名

- [x] 4.1 在 README.md 的"工具引用选项"章节中补充 `properties` 字段说明，列出 `work_dir`、`run_as_user`、`run_as_group` 三个字段及其含义
- [x] 4.2 在 wiki/tool-architecture.md 的 §8 ActionTool 章节中补充 properties 字段文档

## 5. 定位测试超时原因

- [x] 5.1 逐个测试包运行 `go test -v -timeout 30s`，定位哪个包挂起或超时
- [x] 5.2 如果特定测试包（如 tmux 相关）超时，分析原因：是测试逻辑问题还是确实需要长时间
- [x] 5.3 如果是测试逻辑问题（如 goroutine 泄漏、死锁），修复测试代码
- [x] 5.4 如果测试确实需要长时间，考虑添加 `//go:build integration` 标签或延长该包超时

## 6. 补充 actionFactory 测试

- [x] 6.1 创建 `action_factory_test.go`（在根包或 `tool/action` 包中）
- [x] 6.2 测试 `actionFactory` 正确处理 `work_dir` 字段（设置为 ActionTool 的 workspace）
- [x] 6.3 测试 `actionFactory` 正确处理 `run_as_user` 字段
- [x] 6.4 测试 `actionFactory` 正确处理 `run_as_group` 字段
- [x] 6.5 测试 `actionFactory` 在 properties 为空时使用默认值

## 7. 补充 MeditationManager 测试

- [x] 7.1 创建 `agent/meditation_test.go`
- [x] 7.2 测试 `NewMeditationManager` 正确初始化
- [x] 7.3 测试 `UpdateLastEventTime` 正确更新原子时间戳
- [x] 7.4 测试 `checkAndMeditate` 在 `lastEventTime == 0` 时跳过冥想
- [x] 7.5 测试 `checkAndMeditate` 在 gap < MinGap 时跳过冥想
- [x] 7.6 测试 `checkAndMeditate` 在 gap >= MinGap 时注入消息并更新 lastMeditation
- [x] 7.7 测试 `Start/Stop` 正确启动和停止 goroutine
- [x] 7.8 测试 `buildMeditationMessage` 生成正确的消息格式

## 8. 补充 buildPlainToolRef 依赖注入测试

- [x] 8.1 创建 `integration_test.go`（或在现有测试文件中添加）
- [x] 8.2 测试 `buildPlainToolRef` 正确将 `memStore` 注入到 `PlainToolFactoryConfig.MemStore`
- [x] 8.3 测试 `buildPlainToolRef` 正确将 `rc.skillRepo` 注入到 `PlainToolFactoryConfig.SkillRepo`
- [x] 8.4 测试 `buildPlainToolRef` 正确将 `readPartitionIDs` 注入到 `PlainToolFactoryConfig.ReadPartitionIDs`
- [x] 8.5 测试 `buildPlainToolRef` 在 tool id 未注册时返回 error

## 9. 补充新 agent 构建测试

- [x] 9.1 测试 `read.NewAgent` 正确创建 read agent（需要传入 model 和 tools）
- [x] 9.2 测试 `write.NewAgent` 正确创建 write agent
- [x] 9.3 测试 `speak.NewAgent` 正确创建 speak agent（stub）
- [x] 9.4 测试 `draw.NewAgent` 正确创建 draw agent（stub）
- [x] 9.5 测试这些 agent 在缺少必要配置时返回 error

## 10. 补充 tagent.New 集成测试

- [x] 10.1 测试 `tagent.New(DefaultConfig())` 成功创建 entry agent（需要 mock model）
- [x] 10.2 测试 entry agent 的 tools 列表包含 knowledge、recall、action 三个 agent tool
- [x] 10.3 测试 knowledge agent 内部有 6 个 plain tool
- [x] 10.4 测试 recall agent 内部有 4 个 plain tool
- [x] 10.5 测试 `tagent.New` 在 config 引用未注册 tool 时返回 error（ValidateToolAccess 失败）

## 11. 内置 agent name 保护测试

- [x] 11.1 测试 `buildAgent` 对 "knowledge" 不走 ToolAgentFactory 路径（即使注册了 factory）
- [x] 11.2 测试 `buildAgent` 对 "recall" 不走 ToolAgentFactory 路径
- [x] 11.3 测试 `buildAgent` 对 "action" 不走 ToolAgentFactory 路径
- [x] 11.4 测试 `buildAgent` 对自定义 agent name（如 "custom_agent"）可以走 ToolAgentFactory 路径

## 12. 全量测试回归

- [x] 12.1 运行 `go test -timeout 10m ./...`，确保所有测试通过
- [x] 12.2 如果仍有超时，分析原因并调整超时或修复测试
- [x] 12.3 确保 `go build ./...` 无错误
- [x] 12.4 确保 `go vet ./...` 无警告
