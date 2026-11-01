# 01 技术要点

## 要点

1. **参数面**：`hostSendArgs{Target hostSendTarget, Content string}`；`hostSendTarget{Task string, Agent string, Session string}`。校验序：未知字段 → 目标互斥（task 与 agent/session 同现拒绝；全空拒绝；agent 与 session 必须成对）→ content 空/超 8KiB → 派发分派（02/03 消费）。
2. **回执**：沿用三态文本（`ok` / `[send_denied] <reason>` / `[send_held] <reason>`）；error 仅参数形状非法。分派目标未注册（02/03 未装载）时回 `[send_denied] <具名原因>`，不得回出网。
3. **装配**：`toolsFor` 中 send 条件仅 `declaresTool(id, hostSendToolID)`；`host.go` Boot 的 `declaresSendOutside` 校验**删除**（DEL-6，该函数随之单使用方回卷，§四五级检查）。
4. **W0 短测**：0.1 先红——经 `assembly.Boot` 真实入口驱动一次模型调 send 重入，红因="send 不支持 task 目标/重入不可达"。

## 文件清单

- 改：`assembly/host_tools.go`（声明/参数/校验/常数）、`assembly/host_document.go`（装配）、`assembly/host.go`（校验删除）。
- 测：`assembly/*_test.go`、`tests/integration/execution/send_symptom_test.go`（新）、`tests/integration/host`（1.3 迁移）。

## 风险与回退

- 风险：1.3 移除出网语义会打红既有 delivery 审查测试（`bootDeliveryHost` 系）——迁移而非删除（场景保真，D6 测试迁移原则）。
- 回退：单域可独立回滚；出网链未动，回退与基线零 diff 于契约目录。
