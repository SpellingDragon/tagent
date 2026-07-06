# tool-security-alignment Specification

## Purpose
TBD - created by archiving change production-readiness-fix. Update Purpose after archive.
## Requirements
### Requirement: tmux_exec 模式接收并使用安全配置

NewCommandTool SHALL 将 runAsUser、runAsGroup、workspace 传递给 TmuxExecutor（通过 WithTmuxRunAsUser、WithTmuxWorkspace 选项）。TmuxExecutor.CreateSession SHALL 使用 runAsUser 包装 tmux 命令（sudo -n -u <user>），使用 opts.Env 设置环境变量。

#### Scenario: runAsUser 传递到 TmuxExecutor

- **WHEN** CommandTool 配置了 runAsUser="tagent-runner"，创建 tmux session
- **THEN** TmuxExecutor.runAsUser == "tagent-runner"
- **AND** tmux 命令以 `sudo -n -u tagent-runner tmux new-session ...` 执行

#### Scenario: runAsUser 为空时不使用 sudo

- **WHEN** CommandTool 未配置 runAsUser，创建 tmux session
- **THEN** tmux 命令以 `tmux new-session ...` 执行（无 sudo 前缀）
- **AND** 向后兼容（当前行为不变）

#### Scenario: 环境变量传递到 tmux session

- **WHEN** executeAsync 接收 args.Env={"FOO": "bar"}，创建 tmux session
- **THEN** CreateSession 后通过 `tmux set-environment -t <session> FOO bar` 设置环境变量
- **AND** tmux session 内的命令可以访问 FOO=bar

### Requirement: exec 与 tmux_exec 安全对称

exec 模式和 tmux_exec 模式在用户隔离、环境变量、工作目录三个维度上 SHALL 行为对称。当配置了 runAsUser 时，两种模式都以目标用户执行命令。当传入 Env 时，两种模式都设置环境变量。

#### Scenario: 两种模式都使用 sudo 用户隔离

- **WHEN** CommandTool 配置了 runAsUser="runner"
- **AND** exec 模式执行 `ls /`
- **AND** tmux_exec 模式执行 `ls /`
- **THEN** exec 模式通过 `sudo -n -u runner sh -c "ls /"` 执行
- **AND** tmux_exec 模式通过 `sudo -n -u runner tmux new-session ... ls /` 执行

#### Scenario: 两种模式都传递环境变量

- **WHEN** CommandTool 接收 Env={"PATH": "/custom/bin"}
- **AND** exec 模式执行命令
- **AND** tmux_exec 模式执行命令
- **THEN** exec 模式通过 cmd.Env 设置 PATH=/custom/bin
- **AND** tmux_exec 模式通过 set-environment 设置 PATH=/custom/bin

### Requirement: RestartSession 保持安全上下文

TmuxExecutor.RestartSession SHALL 使用与 CreateSession 相同的 runAsUser 和 Env 设置。handleFakeAlive 构造 TmuxCreateOptions 时 SHALL 包含原始 session 的安全上下文。

#### Scenario: RestartSession 使用 sudo

- **WHEN** TmuxExecutor.runAsUser="runner"，调用 RestartSession
- **THEN** 重启命令以 `sudo -n -u runner tmux new-session ...` 执行

#### Scenario: handleFakeAlive 传递安全上下文

- **WHEN** TmuxMonitor 检测到 fakeAlive 状态，调用 RestartSession
- **THEN** TmuxCreateOptions 包含原始 session 的 Command、WorkDir、IsInteractive
- **AND** TmuxExecutor.runAsUser 被 RestartSession 使用（不需要在 opts 中传递，TmuxExecutor 已持有）

