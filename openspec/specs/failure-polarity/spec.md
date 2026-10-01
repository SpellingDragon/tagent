# failure-polarity Specification

## Purpose
TBD - created by archiving change failure-polarity-passthrough. Update Purpose after archive.
## Requirements
### Requirement: 进程死亡按退出码分流极性

monitor 对被托管 tmux 会话的进程死亡/pane 死亡判定，SHALL 读取 tmux 原生 `pane_dead_status` 并按退出码分流结算极性：退出码非零（含负数信号死）→ `SessionError`（失败结算，Err 携带退出码）；退出码为零 → `SessionCompleted`（成功结算，现状语义）。退出码不可得（pane 未死、会话整体消失）SHALL 维持既有判定语义，MUST NOT 因读取失败而误判失败。

#### Scenario: 非零退出报失败

- **WHEN** 托管命令以退出码 42 结束（pane 死，remain-on-exit 保留会话）
- **THEN** monitor SHALL 判定 `SessionError`，结算信号 Err 含退出码 42，任务终态为 failed（✗）

#### Scenario: 零退出维持成功

- **WHEN** 托管命令以退出码 0 结束
- **THEN** monitor SHALL 判定 `SessionCompleted`，任务终态为 completed（✓），行为与现状一致

#### Scenario: 信号死亡报失败并标注信号

- **WHEN** 托管命令被信号杀死（如 SIGTERM，pane_dead_status=-15）
- **THEN** 结算 SHALL 为失败极性，通知文本 SHALL 以 `exit_code=-15 (signal)` 形式标注信号死

#### Scenario: 退出码不可得时不误判

- **WHEN** 会话整体消失（tmux server 外力重启）导致 pane_dead_status 不可读
- **THEN** 判定 SHALL 退化为既有语义（会话不在列表 → 按现状路径处理），MUST NOT 因读取失败而记为 failed

### Requirement: 探测不可辨超限报失败极性

probe 连续不可辨（list-sessions 持续 err）越过加闸上限时，SHALL 判定 `SessionError`（Err 注明 probe unresolvable）而非 `SessionCompleted`；MUST NOT 把「框架失明」报为任务成功。

#### Scenario: tmux server 抖动超限

- **WHEN** 某会话连续 N 次（ProbeUnknownLimit）探测不可辨
- **THEN** 结算 SHALL 为失败极性，Err 文本 SHALL 注明探测不可辨，输出按既有捕获逻辑尽力携带

### Requirement: kill 失败强拆报失败极性并提示逃逸

假死处理中 kill-session 连续三次失败后的 force remove，SHALL 以 `SessionError` 结算（Err 注明 kill failed after retries），且通知 SHALL 提示底层会话可能仍在运行（进程逃逸）；MUST NOT 报为 completed。

#### Scenario: 三连败强拆

- **WHEN** fake-dead 会话 kill 三次均失败，monitor 强制移除跟踪
- **THEN** 结算 SHALL 为失败极性，通知 SHALL 含「kill 失败、会话可能逃逸」提示

### Requirement: 结算信号与通知携带 exit_code

`SettleSignal` SHALL 携带显式 `ExitCode` 字段（零退出=0，未知=-1 哨兵，信号死=负数）；action 工具结果结构体 SHALL 暴露 `exit_code` JSON 字段；`task_settled` 通知文本对失败结算 SHALL 含 `exit_code=N`。下游消费 MUST NOT 依赖解析自然语言文本来获取退出码。

#### Scenario: 可编程消费退出码

- **WHEN** 命令以退出码 1 结束并结算
- **THEN** 工具结果 JSON 含 `"exit_code":1`，通知文本含 `exit_code=1`，且信号结构体的 ExitCode 字段值为 1

### Requirement: 纯空白载荷通知降级

结算通知构造时，载荷（输出文本）去除首尾空白后为空且无 Err 的，SHALL 降级为单行票据（任务 id、简述、极性），MUST NOT 向时间线投递空白正文。

#### Scenario: 空输出任务结算

- **WHEN** 一个 job 型任务结束时输出仅含空白字符（如换行堆）
- **THEN** 通知 SHALL 为单行票据形态（`… completed →（无输出）`），不投递空白载荷

### Requirement: 工具描述引导退出码透传

action 工具的模型面 schema 描述 SHALL 引导：退出码由框架捕获，命令应直接失败（保持非零退出），MUST NOT 以 `; echo "EXIT=$?"` 等模式吞掉退出码。

#### Scenario: schema 引导可见

- **WHEN** 模型查看 action 工具的参数/行为描述
- **THEN** 描述 SHALL 含退出码由框架捕获、不要吞码的引导语句

