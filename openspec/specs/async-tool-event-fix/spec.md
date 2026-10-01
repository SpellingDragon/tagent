# async-tool-event-fix Specification

## Purpose

本规范定义 async-tool-event-fix 能力：异步工具事件的两个横切契约——总线注入消息在消费时的角色归一化，与非交互命令的 settle 分类。

> **状态核验（2026-09-09 立，2026-10-01 按码面修订）**：① 角色归一化——名为 `InjectBusInputs` 的函数已不存在（仅日志串与测试名沿用该称谓），但**转换本身并未消失**（消费侧与 canonical 侧各一处"复制 Message 后转 user"，见首条需求的承载坐标）；原记"消息层 Role 转换不再需要"是误判，已更正——不得据函数名消失判定能力消失。② 段边界（user/external_input 开新段）**已被 task-skeleton-compression 推翻**（段边界现为 agent_output 闭合回合，`isMessageTaskBoundary`/`isReferenceTaskBoundary` 符号非注释码面 0 命中）——相应两条需求已于 2026-10-01 移除，现行边界契约见该能力。③ 非交互 stable 即 completed **有效**，由 `tool/action/settle.go` 三档分类实现。④ "压缩产物不含引导消息"的行为由骨架模型保持（卡片行无引导语），但 `findPendingUserMessage` 符号已移除、原需求以死符号立文，已移除；若需成文，归属骨架/压缩能力而非本页。同标题重复的"Non-interactive"需求（历史吸收 append 而非 modify 所致）已收敛为一条。

## Requirements

### Requirement: 冻结批次消费时归一化外部消息角色

系统注入的外部输入 SHALL 在下一次有限拉取中被认领（非唤醒式排空＋durable 认领，不阻塞事件循环）。准备 canonical fact 与实际请求消息时 MUST 复制 Message 再把 `model.RoleSystem` 转为 `model.RoleUser`，不得原地改写原件，也不得把系统注入提升为模型系统指令。原始 source_event、Content、完整消息与来源 SHALL 保持不变；本就是 user 的消息语义不变。同步工具结果仍由框架在当前 ReAct 中处理，MUST NOT 因总线的批次边界而延后。

> 承载坐标（现行码面）：消费侧 `agent/context_manager.go` 的 BeforeModel 回调（`msg := *evt.Message` 后按角色转换，日志串仍作 `[InjectBusInputs]`）；canonical 侧同文件的 `buildBusFact`（注释明示"durable fact 的 canonical 形态只决定一次，重放不重推"）；注入源头 `agent/inject.go` 的 `EmitSystemAlert` 仍以 `RoleSystem` 经 `NewExternalInputEvent` 入总线；行为钉测 `tests/inject_bus_inputs_test.go`。**名为 `InjectBusInputs` 的函数已不存在，但本命题的行为仍在——不得据函数名消失判定能力消失。**

#### Scenario: 异步任务结果在当前轮执行中到达

- **WHEN** 当前 turn 执行期间到达一条 system-role 的异步任务结果
- **THEN** 原件留队至下一次拉取，消费时使用 user-role 副本，当前轮不认领它

#### Scenario: 原始消息不被改写

- **WHEN** 一条 system 消息经历准备、提交与重启
- **THEN** 原 source_event 的角色仍为 system，canonical 与模型侧为 user，内容与来源不被原地修改

#### Scenario: 普通用户输入与同步工具结果不受影响

- **WHEN** 处理 user 输入，或当前 ReAct 的同步工具结果
- **THEN** user 语义保持；同步工具结果照常参与当前轮，不受异步总线边界影响

### Requirement: Non-interactive commands complete on stable

In `detectSessionState`, when `!session.IsInteractive && !session.IsTUI` and the output has been stable for `stableDuration >= threshold`, the session SHALL return `SessionCompleted` immediately, without waiting for `fakeDeadDuration`. This prevents non-interactive commands (e.g., `curl`, `ls`) from being incorrectly classified as `fakeAlive` and restarted.

#### Scenario: Non-interactive command reaches stable

- **WHEN** a non-interactive, non-TUI session's output is stable for >= stable_duration
- **THEN** detectSessionState SHALL return SessionCompleted
- **AND** SHALL NOT enter fakeDeadDuration/heartbeat detection

#### Scenario: Interactive command reaches stable

- **WHEN** an interactive session's output is stable for >= stable_duration
- **THEN** the existing logic SHALL apply (stable → fakeDeadDuration → heartbeat)
- **AND** SHALL NOT be affected by this change

#### Scenario: TUI command reaches stable

- **WHEN** a TUI session's output is stable for >= stable_duration
- **THEN** the existing TUI logic SHALL apply (stable → fakeDeadDuration → TimedOut)
- **AND** SHALL NOT be affected by this change
