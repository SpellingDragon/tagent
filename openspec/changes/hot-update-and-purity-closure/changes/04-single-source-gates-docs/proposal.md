# Proposal: 04-single-source-gates-docs

父变更：hot-update-and-purity-closure（合并转移自 restart-recovery-optimization / runtime-simplicity-upgrade / event-driven-consumption）

## Why

评审实证：window_bounds 登记称 BudgetCharsPerToken 唯一换算常量，但 fuse.go:68 bytesPerToken=4 另立同值、messageOverheadBytes=16 与 budgetMessageOverheadTokens=4 自称两表一致却无机械连接；check_feature_boundary 误判新合法键 output_reserve（GENERIC_AGENT_KEYS 未随 surface 更新，与 decode.go agentKeys 成第二登记处）、列表式 `- id:` agent 挂 meditation 完全漏检；preflight 长稳定性种子收窄+--skip-event-long；文档多处仍述已撤机制（wechat README 教撤掉的检索键、tool-architecture"三个具名原因"、bounded-trajectory spec 仍写 capture_budget_mb、host_capture 注释、event-flow 缺空闲回收、tmux-action 缺 generation token）。

## What Changes

常量单源机械连接；gate 两洞修复+正反样例自测；preflight 覆盖恢复或显式裁决；文档脱节清偿（本域 owning 页）。

## 边界与依赖

- 依赖：无；接口常数=window_bounds.json 登记族（沿用并扩展机械连接）。
- 被依赖方：无。
- 接口面：runtime/agent/fuse.go、assembly/request_assembly.go、golden/runtime/window_bounds.json、scripts/gates/check_window_bounds.py、scripts/gates/check_feature_boundary、scripts/preflight.sh、examples/wechat-bot/README.md、docs/wiki/tool/tool-architecture.md、docs/wiki/agent/event-flow.md、docs/wiki/tool/tmux-action.md、openspec/specs/bounded-trajectory-capture/spec.md、assembly/host_capture.go 注释。
- 禁止事项：不改 durable-delivery.md（01 owning）、org-hot-reload.md（05 owning）；不新增第二常量登记处。