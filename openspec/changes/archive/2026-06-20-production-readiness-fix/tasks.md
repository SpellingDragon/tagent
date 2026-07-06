## 1. 事件类型统一（无依赖，可先行）

- [x] 1.1 在 `memory/types.go` 中删除 `EventTypeExternalInput`、`EventTypeAgentOutput`、`EventTypeActionCommand`、`EventTypeThinkingPlan`、`EventTypeContextCompress` 常量定义
- [x] 1.2 在 `memory/lifecycle.go` 中将 `EventTypeContextCompress`、`EventTypeThinkingPlan`、`EventTypeExternalInput`、`EventTypeAgentOutput`、`EventTypeActionCommand` 引用改为 `event.Type*`，新增 `import "github.com/SpellingDragon/tagent/event"`
- [x] 1.3 在 `memory/compaction.go` 中将 `EventTypeThinkingPlan`、`EventTypeContextCompress` 引用改为 `event.Type*`
- [x] 1.4 在 `plugin/memory_plugin.go` 中将 `tagentevent.TypeExternalInput` 确认已使用（无需改动，仅验证）
- [x] 1.5 全局搜索 `memory.EventType` 确认零结果：`grep -r "memory\.EventType" --include="*.go"`
- [x] 1.6 编译验证：`go build ./...` 一次通过，无 import cycle

## 2. 生产接线补全（P0）

- [x] 2.1 在 `memory/segment_store.go` 新增 `SetTombstoneSet(ts *TombstoneSet)` 方法，设置 `s.tombstones = ts`
- [x] 2.2 在 `memory/segment_store.go` 新增 `Close() error` 方法：停止 Compactor → 停止 LifecycleManager → flush tombstones → 关闭 RelationStore；使用 `sync.Once` 保证幂等
- [x] 2.3 在 `memory/segment_store.go` 新增 `SetLifecycleManager(lm *LifecycleManager)` 和 `SetCompactor(c *Compactor)` 方法（供 Close 使用）
- [x] 2.4 在 `tagent.go` 的 `resolveMemoryStore` file 分支中，创建 FileSegmentStore 后：
  - 创建 `TombstoneSet`（需要 RelationStore 和 KVStore）
  - 调用 `store.SetTombstoneSet(tombstone)`
  - 创建 `LifecycleManager(store, tombstone, DefaultLifecycleConfig())` 并调用 `lm.Start()`
  - 创建 `Compactor(store, kv, rel, tombstone, DefaultCompactionConfig())` 并调用 `compactor.Start()`
  - 调用 `store.SetLifecycleManager(lm)` 和 `store.SetCompactor(compactor)`
- [x] 2.5 单元测试：验证 file 类型存储创建后 LifecycleManager 和 Compactor 已启动
- [x] 2.6 单元测试：验证 store.Close() 停止所有后台组件且幂等

## 3. 工具执行安全对齐（P0）

- [x] 3.1 在 `tool/command/command_tool.go` 的 `NewCommandTool` 中，将 `ct.runAsUser`、`ct.workspace` 传递给 `NewTmuxExecutor`：
  ```go
  ct.tmuxExecutor = NewTmuxExecutor(
      WithTmuxWorkspace(ct.workspace),
      WithTmuxRunAsUser(ct.runAsUser),
  )
  ```
- [x] 3.2 在 `tool/command/tmux_executor.go` 的 `CreateSession` 中，当 `te.runAsUser != ""` 时用 sudo 包装 tmux 命令：
  ```go
  baseCmd := "tmux"
  baseArgs := args
  if te.runAsUser != "" {
      sudoArgs := []string{"-n", "-u", te.runAsUser}
      if te.runAsGroup != "" { sudoArgs = append(sudoArgs, "-g", te.runAsGroup) }
      baseCmd = "sudo"
      baseArgs = append([]string{"tmux"}, baseArgs...)
      // 实际：sudo -n -u user tmux new-session ...
  }
  ```
- [x] 3.3 在 `tool/command/tmux_executor.go` 的 `CreateSession` 中，CreateSession 成功后遍历 `opts.Env`，执行 `tmux set-environment -t <session> <key> <value>`
- [x] 3.4 在 `tool/command/tmux_executor.go` 的 `RestartSession` 中，同样使用 sudo 包装（复用 CreateSession 的逻辑）
- [x] 3.5 在 `tool/command/tmux_executor.go` 中提取 `buildTmuxCommand(args []string) (string, []string)` 辅助函数，CreateSession 和 RestartSession 共用
- [x] 3.6 单元测试：验证 runAsUser 非空时 tmux 命令带 sudo 前缀
- [x] 3.7 单元测试：验证 runAsUser 为空时 tmux 命令不带 sudo（向后兼容）
- [x] 3.8 单元测试：验证 opts.Env 被设置到 tmux session

## 4. 资源生命周期管理（P1）

- [x] 4.1 在 `tool/command/command_tool.go` 新增 `Close() error` 方法：如果 tmuxMonitor 正在运行则 Stop()；使用 `sync.Once` 保证幂等
- [x] 4.2 在 `agent/tagent_agent.go` 的 `TagentAgent` 结构体中新增 `commandTools []*command.CommandTool` 字段
- [x] 4.3 在 `agent/tagent_agent.go` 的 `buildAgent` 中，当 cmdTool != nil 时将其追加到 TagentAgent 的 commandTools 列表（需要 TagentAgent 新增 `RegisterCommandTool(ct *command.CommandTool)` 方法）
- [x] 4.4 在 `agent/tagent_agent.go` 的 `Close()` 方法中，先遍历关闭所有 commandTools，再调用 `ta.runner.Close()`
- [x] 4.5 在 `tool/command/tmux_monitor.go` 的 `handleFakeDead` 中，KillSession 失败时不删除 session：增加 `killRetryCount` 字段到 TmuxSession，失败时递增并 return（不设 shouldRemove）；达到 3 次时强制删除
- [x] 4.6 在 `tool/command/tmux_monitor.go` 的 `handleFakeAlive` 中，确保 TmuxCreateOptions 包含 session 的 Command、WorkDir、IsInteractive（当前已包含，验证即可）
- [x] 4.7 单元测试：验证 CommandTool.Close() 停止 TmuxMonitor
- [x] 4.8 单元测试：验证 TagentAgent.Close() 先关 CommandTool 再关 Runner
- [x] 4.9 单元测试：验证 KillSession 失败后 session 保留、3 次后强制删除

## 5. TUI 会话回收（P2）

- [x] 5.1 在 `tool/command/tmux_monitor.go` 新增 `SessionTimedOut SessionStatus = "timed_out"` 常量
- [x] 5.2 在 `detectSessionState` 中，TUI 会话的 `stableDuration > fakeDeadDuration` 时返回 `SessionTimedOut`（替代当前的 `SessionRunning`）
- [x] 5.3 在 `checkSession` 的 switch 中，新增 `case SessionTimedOut: shouldRemove = true`
- [x] 5.4 单元测试：验证 TUI 会话在 fakeDead 阈值后返回 SessionTimedOut 并被移除
- [x] 5.5 单元测试：验证非 TUI 会话不受影响（走 fakeDead 路径）

## 6. 压缩 User Message 策略

- [x] 6.1 在 `agent/smart_compress.go` 的 `Compress` 方法中，压缩后检查 recent segments 中是否存在 user message
- [x] 6.2 新增 `findPendingUserMessage(segments []*TaskSegment) *model.Message` 函数：从最后一个 segment 开始向前查找，找到最后一个 `IsComplete=true` 的 segment 之后的第一个 user message
- [x] 6.3 如果找到 pending user，将其追加到压缩后消息列表末尾；如果未找到，追加引导消息 `"（以上是对话历史摘要。如果有新任务，请告诉我。）"`（User role）
- [x] 6.4 在 `agent/context_intervention.go` 的 `ensureUserPrompt` 中，将硬编码 `"继续"` 替换为引导消息 `"（以上是对话历史摘要。如果有新任务，请告诉我。）"`
- [x] 6.5 确认 SmartCompressor 的 compressEvent 使用 `model.NewSystemMessage()`（当前已是 System role，验证即可）
- [x] 6.6 单元测试：验证有 pending user 时保留原始 user message
- [x] 6.7 单元测试：验证无 pending user 时添加引导消息
- [x] 6.8 单元测试：验证 ensureUserPrompt 不再添加 "继续"

## 7. 分批摘要

- [x] 7.1 在 `agent/smart_compress.go` 新增 `batchSegmentsByTokenBudget(segments []*TaskSegment, maxTokens int) [][]*TaskSegment` 函数：遍历 segments，按 token 估算累加，超过 maxInputTokens 时分批
- [x] 7.2 在 `agent/smart_compress.go` 新增 `summarizeBatches(ctx context.Context, batches [][]*TaskSegment) ([]model.Message, bool)` 函数：每批调用 LLM 生成摘要，返回多条 System role 消息；单批失败跳过（log warning）；全部失败时返回空 slice + hadError=true
- [x] 7.3 修改 `Compress` 方法的 Stage 2 逻辑：用 `batchSegmentsByTokenBudget` 替代直接 `generateSummary`，用 `summarizeBatches` 生成多条摘要
- [x] 7.4 修改 `buildCompressEvent`：当有多条摘要时，compressEvent 内容列出批次信息；当全部失败时，内容标注 "摘要生成失败"
- [x] 7.5 修改 `Compress` 的消息重建逻辑：将单条 compressEvent 替换为 [compressEvent + 多条摘要消息] 或 [compressEvent]（无摘要时）
- [x] 7.6 SmartCompressor 新增 `maxTokens int` 字段（从 TagentConfig 传入），用于计算 maxInputTokens
- [x] 7.7 在 `agent/tagent_agent.go` 的 `NewTagentAgent` 中，将 `cfg.MaxTokens` 传递给 SmartCompressor（新增 `WithMaxTokens` option）
- [x] 7.8 单元测试：验证多事件分批（50 事件 → 3 批 → 3 条摘要）
- [x] 7.9 单元测试：验证单批不超预算
- [x] 7.10 单元测试：验证单批失败容错（mock LLM 第 2 批失败，第 1、3 批成功）
- [x] 7.11 单元测试：验证无 summaryModel 时跳过分批（不调用 LLM）
- [x] 7.12 单元测试：验证摘要消息使用 System role 且包含批次编号

## 8. 集成验证

- [x] 8.1 全量编译：`go build ./...`
- [x] 8.2 全量 vet：`go vet ./...`
- [x] 8.3 全量测试：`go test ./... -count=1`
- [x] 8.4 竞态检测：`go test -race ./... -count=1`（修改包全部通过；tests 包有预存 race 在 tool_agent.go，非本次修改引入）
- [x] 8.5 端到端验证：创建 file 类型存储 → 写入事件 → 验证 LifecycleManager TTL 扫描运行 → 验证 Compactor 压实调度运行 → Close 干净退出（memory/wiring_test.go 覆盖）
- [x] 8.6 端到端验证：配置 run_as_user → exec 模式和 tmux_exec 模式都通过 sudo 执行 → 环境变量都传递（tmux_executor_security_test.go 覆盖）
- [x] 8.7 端到端验证：压缩触发 → 分批摘要生成多条 System 消息 → pending user 保留或引导消息添加（batch_summary_test.go + compress_usermsg_test.go 覆盖）
- [x] 8.8 grep 验证：`grep -r "memory\.EventType" --include="*.go"` 返回零结果
- [x] 8.9 grep 验证：`grep -r "继续" agent/context_intervention.go` 返回零结果

## 9. TUI 集成测试（基于 qodercli）

- [x] 9.1 新建 `tool/command/tui_integration_test.go`，添加 `qodercliAvailable()` 和 `tmuxIntegrationAvailable()` 辅助函数，不可用时 skip
- [x] 9.2 集成测试：启动 qodercli TUI 会话 → 等待 Stable → 发送输入触发 Running → 再次 Stable → fakeDead 超时 → SessionTimedOut（完整生命周期）
- [x] 9.3 集成测试：验证 qodercli 会话超时后 tmux session 被正确清理（SessionExists 返回 false）
- [x] 9.4 集成测试：验证非 TUI 命令（sleep）在 tmux_exec 模式下走 fakeDead 路径而非 TimedOut
- [x] 9.5 集成测试：验证多个 qodercli TUI 会话可并发运行且独立超时
