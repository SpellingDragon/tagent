# E 越界清点表：runtime/task 是否吸走执行细节（叶 11 / D14）

## 判据

`runtime/task` 是**受理与结算层**：它只声明「一次调用以什么身份被受理、走过哪些生命周期状态、欠谁什么通知、通知是否已投递」。因此下列**执行细节**都不属于它，命中即为越界：

| 细节类 | 具体词族 |
|--------|---------|
| tmux | 会话名、窗格、socket、`tmux` 命令拼装 |
| 子代理 | subagent / sub-agent 的提示、工具集、组装、路由 |
| 远程 | remote / A2A / 远端地址与传输 |
| exec 退出语义 | `os/exec`、`exec.Command`、`ExitCode`、`exit code` / `exit status` |
| 进程观察 | `stdout` / `stderr`、`pid`、`pane`、`SIGKILL` / `SIGTERM` |

裁决只有两种：**回卷适配器**（把细节推回执行侧）或**写明责任边界依据**（保留，说明为何这不是执行细节）。本叶**不修改 `runtime/task` 任何代码**；若命中真越界，只出裁决，改动归执行侧单元。

取证口径：分支 `next`、`HEAD = 4494dfb8`，`runtime/task/` 全目录（含 `approval/` 子包），共 22 个文件、全部为 `.go`（根 19 + `approval/` 3）。

行号快照于叶 14 重组前（host.go 3215 行时点）；重组后活文件见 assembly/ 迁移与 host_delivery.go/host_document.go 等拆分。

## 一、搜索面与命中计数

| # | 检索面（`runtime/task/` 全目录，`--include='*.go'`） | 命中 |
|---|------------------------------------------------|------|
| S1 | `tmux\|Tmux\|TMUX` | **0** |
| S2 | `subagent\|SubAgent\|sub-agent\|Subagent` | **1**（E1） |
| S3 | `remote\|Remote\|远程` | **0** |
| S4 | `os/exec\|exec\.Command\|exec\.Cmd\|ExitCode\|exit code\|exit status\|exec 退出` | **0** |
| S5 | `stdout\|stderr\|Stdout\|Stderr` | **0** |
| S6 | `pane\|session name\|PID\|pid` | **0** |
| S7 | `SIGKILL\|SIGTERM\|signal\.` | **0** |
| S8 | 相邻词族（不属上述四类但同族）：`delegat\|Delegat` | **2**（E2、E3） |
| S9 | `compensat`（宿主补偿命名，验收测试的强负锚） | **0** |

## 二、逐命中裁决

| # | 位置 | 命中原文 | 执行细节越界？ | 裁决 | 依据 |
|---|------|---------|--------------|------|------|
| E1 | `runtime/task/spec.go:4` | `// the declaration a tool, sub-agent or result-delivery call is admitted under,` | **否** | **不回卷（保留）** | 这是包注释里的**词汇列举**，说明本包受理「工具 / 子代理 / 结果投递」三类调用，与 `spec.go:19-47` 的 `Spec` 字段一一对应（`CallID` / `Source` / `Target` / `TriggerSource` / `Execution`）。它不描述子代理如何组装提示与工具集，也不描述其路由。子代理的实现细节归属 `runtime/agent/delegate.go`（49 处）、`runtime/agent/handoff.go`（8 处）、`runtime/agent/agent.go`（3 处）、`tool/subagent/`（agentsfile/assemble/builder/capture/handoff）与 `host.go`（`host.go:52` import、`host.go:2141-2174` 组装）。 |
| E2 | `runtime/task/spec.go:38-43` | `// TriggerSource is the trigger-source stamp the admission carries … // on the task_spawned material. A delegated call carries // event.LineageDelegation; an empty stamp records no source` | **否** | **不回卷（保留）** | 记录的是**触发源戳**（一个字符串，落在 `task_spawned` 材质上，用于把「谁触发了这次受理」写进事实链），不是被委派调用的执行参数。字段本身无行为，空戳等价于「无来源」。 |
| E3 | `runtime/task/spec.go:40` | `event.LineageDelegation` | **否** | **不回卷（保留）** | 引用 `api/event` 的身份枚举常量，属事件身份词汇（D11 白名单：event 身份不受扰），非执行细节。 |

**回卷裁决：无。** 唯一命中（E1）为词汇列举，E2/E3 为事实戳与身份常量，三处均不携带 tmux / 子代理 / 远程 / 进程执行语义。

## 三、执行细节的实际归属（对照面）

清点表的意义在于指出细节「应该在哪」，因此同时取证其真实归属，证明它们留在执行侧而非被 `runtime/task` 吸走：

| 细节类 | 归属位置 | 证据 |
|--------|---------|------|
| tmux | `tool/exec/tmux_executor.go`（71 处）、`tool/exec/tmux_session.go`（16）、`tool/exec/tmux_command.go`（7）、`tool/exec/monitor.go`（1） | 宿主在 `host.go:360` 才 `tagentexec.NewTmuxExecutor()`；`runtime/task` 零命中 |
| 子代理 | `runtime/agent/delegate.go`、`runtime/agent/handoff.go`、`runtime/agent/agent.go`、`tool/subagent/*`、`host.go:52,2141-2174` | 见 E1 依据 |
| 远程 / A2A | `runtime/agent/handoff.go:107`（A2A data part 的远端传输）、`host.go`、`api/config` | `runtime/task` 零命中 |
| exec 退出语义 | `tool/exec/stop.go:47` `StopEvidence{Resource, Confirmed, ExitCode, Reason}`、`tool/exec/exec_test.go` | **关键边界样本**：执行侧的 `StopEvidence` 带 `ExitCode` 与 `Resource`；任务侧的 `runtime/task/cancel.go:24-32` `StopEvidence{TaskID, Stopped, Reason}` **只有布尔与原因，没有退出码、没有 pid、没有资源句柄**。退出码在适配器内被折叠成 `Stopped bool` 后才上交——这正是「回卷适配器」已在位的证据 |
| 进程观察（stdout/stderr/pid/pane/信号） | `tool/exec/*`（执行报告）；`runtime/task` 零命中 | S5/S6/S7 全 0 |

### `runtime/task` 的 `Execution` 不是进程 exec

`runtime/task/spec.go:44-46`（`Execution Serialization`）与 `spec.go:55-59`（`Serialization` 声明）描述的是**串行执行形状**：同一 session 同一时刻至多一个有效模型决策，其余排队；`Serialization` 是这份声明的名字（字符串），不涉及进程、pty、退出码。`runtime/task/logical.go:64-66` 的 `ExecutionTimeout` 是「一轮执行可以跑多久」的静默分级阈值，也是任务层观察，不是进程控制。故 S4 零命中不构成遗漏，而是分层正确的结果。

## 四、结论

- **越界数 = 0**。S1、S3–S9 全零命中；S2 的唯一命中（E1）为包注释词汇列举，随附的 E2/E3 为触发源戳与身份常量，逐条裁决为「不回卷，写明责任边界依据」。
- **无需回卷适配器**：执行细节（tmux 会话/窗格、子代理组装、远端传输、`ExitCode`、`stdout`/`stderr`、pid、信号）全部留在 `tool/exec`、`tool/subagent`、`runtime/agent`、`host.go`；任务层只收 `{Stopped, Reason}` 这类已折叠的证据。
- **本叶未改 `runtime/task` 一行**：`git status --short runtime/task/` 为空，`git diff -- runtime/task/` 为空（含叶 11 期间为变异验证临时注入并已完全复原的 `Status` 常量）。
- 该结论的可执行对应物：`tests/integration/execution/new_tool_adapter_test.go` 的 `TestNewToolAdapterNeedsNoHostCompensation`——一个只提供执行、取消、恢复证据三面的适配器接进 `NativeAgent.Tools` 即跑通受理→执行/进度→结算→投递，宿主零补偿，且 `Status` 枚举保持闭集。

## 五、复现命令

```bash
# S1–S7：执行细节词族在 runtime/task 全目录的命中
for p in 'tmux|Tmux|TMUX' 'subagent|SubAgent|sub-agent|Subagent' 'remote|Remote|远程' \
         'os/exec|exec\.Command|exec\.Cmd|ExitCode|exit code|exit status' \
         'stdout|stderr|Stdout|Stderr' 'pane|session name|PID|pid' 'SIGKILL|SIGTERM|signal\.'; do
  printf '%-45s => ' "$p"; grep -rnE "$p" --include='*.go' runtime/task/ | wc -l | tr -d ' '
done

# S8/S9
grep -rniE 'delegat' --include='*.go' runtime/task/
grep -rniE 'compensat' --include='*.go' . | grep -v new_tool_adapter_test.go | wc -l   # => 0

# 对照面
grep -rnE 'type StopEvidence' --include='*.go' tool/exec/ runtime/task/
grep -rniE 'tmux' --include='*.go' tool/exec/ | wc -l                                  # => 95
```
