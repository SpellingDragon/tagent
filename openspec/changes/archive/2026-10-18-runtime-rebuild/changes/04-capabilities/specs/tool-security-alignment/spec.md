# tool-security-alignment Specification

## Purpose

本 delta 在重建执行路径上复述 exec/tmux 的安全对称：用户隔离、环境变量、工作目录三维度一致，且停止/重挂保持安全上下文。

## ADDED Requirements

### Requirement: exec/tmux 安全对称在重建路径上保持

exec 与 tmux 模式在用户隔离、环境变量、工作目录三维度 SHALL 行为对称；当配置 `runAsUser` 时两种模式都以目标用户执行；`RestartSession` 与重挂 SHALL 保持同一安全上下文。

#### Scenario: 两种模式都使用用户隔离

- **WHEN** 配置 `runAsUser="runner"` 且 exec 与 tmux 各执行同一命令
- **THEN** exec 通过 `sudo -n -u runner sh -c ...` 执行
- **AND** tmux 通过 `sudo -n -u runner tmux new-session ...` 执行

#### Scenario: 环境变量传递一致

- **WHEN** 传入 `Env={"PATH": "/custom/bin"}`
- **THEN** exec 经 `cmd.Env` 设置，tmux 经 `set-environment` 设置

#### Scenario: 重挂/重启保持安全上下文（重启）

- **WHEN** `RestartSession` 或跨重启重挂一个会话
- **THEN** 重启命令沿用同一 `runAsUser` 与 `Env`
- **AND** 不降级为无 sudo 执行
