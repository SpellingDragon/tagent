## 为什么

`tool/command/command_test.go` 中存在 3 个预存测试失败，这些问题在 `thinking-plan-event-separation` 变更实施期间被发现：

1. **`TestCommandTool_WorkDir`** — macOS 路径符号链接导致断言失败（`/var` vs `/private/var`）
2. **`TestCommandTool_Timeout`** — 超时机制未生效，`sleep 5` + `timeout 1` 未返回错误（**功能风险**）
3. **`TestTmuxMonitor_StateDetection`** — 等待时间不足，会话状态未更新到 `completed`

其中问题 2 最为关键：如果命令执行的 timeout 机制在测试中不生效，可能在**生产环境**也存在同样问题，导致 Agent 被长时间运行的命令阻塞。

## 变更范围

| 文件 | 变更 |
|------|------|
| `tool/command/command_test.go` | 修复 3 个失败用例 |
| `tool/command/command.go` 或执行器 | 可能需要修复 timeout 机制（如问题根因在代码层） |

## 能力

### fix-workdir-test
修复 `TestCommandTool_WorkDir` 的路径断言，使其兼容 macOS 的 `/private/var` 符号链接。

### fix-timeout-test
排查并修复 `TestCommandTool_Timeout` 的 timeout 机制失效问题。如果根因在代码层（exec.CommandContext 未正确取消），一并修复执行器逻辑。

### fix-tmux-state-test
修复 `TestTmuxMonitor_StateDetection` 的状态检测时机问题，确保会话完成后状态正确更新。

## 影响

- **测试稳定性**：修复后全量 `go test ./...` 应全部通过（除集成测试需 LLM 环境）
- **潜在功能修复**：timeout 机制修复直接影响命令执行的可靠性
- **影响面有限**：仅涉及 command 工具包，其他模块不受影响
