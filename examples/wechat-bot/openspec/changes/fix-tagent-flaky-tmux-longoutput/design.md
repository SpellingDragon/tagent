# Design: 修复 tagent tmux 长输出截尾 flaky

## Context

已读真材料（行号以当前工作区为准）：

- 测试：tagent/tool/action/tmux_complex_test.go L60-97 — 100 行 echo 刷屏 + END_MARKER，断言 `resp.Output` 尾部 2000 字符含 END_MARKER；长输出须落盘 OutputFile。
- 截尾逻辑：tagent/tool/action/action_tool.go `buildResultFromSignal` L438-498 — `output = "..." + output[len(output)-2000:]`，直接信任 `sig.Output`。
- 信号产出：tagent/tool/action/settle.go `OnStateChange` L156-181 — 从监视器回调收 `output`，trim 到 baseline 后推 chan。
- 状态判定：tagent/tool/action/tmux_monitor.go `detectSessionState` L504-558 — 判定 Completed（`!processExists || isPaneDead`）时**同一次** GetSessionOutput 作为 LastOutput 传回调；若 pane 已死但 tmux 终态行尚未渲染，尾部缺失。且 settle.go L173-178 Completed 一路 `d.reap()`（kill session + 停止跟踪），**没有第二次 capture 的机会**。
- capture 实现：tagent/tool/action/tmux_executor.go `GetSessionOutput` L246-264 — `capture-pane -p -S -1000`（含全量 scrollback，本身完整；offload 文件 8453 chars 佐证）。

结论：capture 窗口完整，问题是"结算时刻的 capture 早于终态行渲染"，且 Completed 即 reap，无补救时机。这解释了"尾部停在 line 88 而 END_MARKER 丢失"与"落盘文件完整"并存的现象。

## Goals / Non-Goals

**Goals:**

- TestActionTool_TmuxLongOutput 稳定绿（连跑 10 次全过）
- 修复落在"结算时刻保证尾部含最终行"，不靠放宽断言或 sleep 式 hack
- 回填 quiet_timeout 已归档计划 5.2 挂账

**Non-Goals:**

- 不重构 tmux 监视器整体架构（poll 调度、stable/fakeDead 阈值不动）
- 不重启 tagent 运行中的进程；不动 quiet_timeout 语义
- 不处理 "Pane is dead" 附加行清理之外的其他 cleanTmuxOutput 行为

## Decisions

### D1: 修复落点——Completed 结算前重 capture + 短收敛重试（方向 A'为主，A 为兜底）

**选择**：在 Completed/Error 结算路径上（settle 链路或 monitor 回调之前）重新执行 GetSessionOutput，若与当前值 MD5 一致则采用；不一致则以新 capture 为准（新 capture 永远不少于旧 capture 的信息量，scrollback 只增不减）。

**理由**：根因是"判定时刻早于渲染时刻"，重 capture 直接消除时序窗口；MD5 一致即返回，不引入固定延迟。落点候选：

- 候选 1（推荐）：detectSessionState 判定 Completed 前/时，重读一次并取较新值——集中一处，stable/TUI 分支零改动
- 候选 2：buildResultFromSignal 侧对 completed 类 signal 重新 capture——但此时 settle.go 已 reap，session 可能已 kill，capture 会失败 → **若选此点必须把 reap 挪到重 capture 之后**，改动面更大

**备选（已否决）**：
- 固定 sleep 重试：引入确定延迟，治标不治本，且污染所有 completed 路径
- 测试改 mark 式断言（放宽断言）：不修实现只掩盖 flaky，后患（生产 LLM 同样拿不到尾部）

### D2: 测试不放宽、增加压力轮次

断言维持"尾部含 END_MARKER + 长输出落盘"不变；修复后连跑 10 次（go test -count=10）作为锁定手段，而不是靠重试刷绿。

### D3: 回填方式

quiet_timeout 归档计划的 tasks.md 5.2（或等价挂账文件）追加结论：根因（capture 时机早于终态渲染 + Completed 即 reap 无补救）、修复 commit、10 连跑证据。

## Risks / Trade-offs

- [重 capture 在 pane 已死场景 capture 失败] → 沿用现行"保留旧 LastOutput"降级路径，不因修复引入新失败面
- [多一次 capture 增加每次 completed 结算延迟] → 仅 Completed/Error 路径多一次 exec（~10ms 级），且 MD5 一致即止；对比收益（尾部正确性）可接受
- [reap 时序调整引发 session 泄漏或双重 kill] → 保持 reapOnce 语义；若采用候选 2 必须验证 kill 前置后 mock 测试全绿
- [scrollback 不足 1000 行被截] → 本测试 ~101 行远小于上限，不构成风险；但 plan 中保留核验项（capture -S -1000 与输出行数关系）

## Migration Plan

1. 复现统计基线失败率 → 2. 落地 D1 修复 → 3. 单测 10 连跑锁定 → 4. 包级回归 → 5. 回填 5.2 → 6. archive。
回滚：revert 单个 commit 即回到旧时序行为（无数据/接口迁移）。
