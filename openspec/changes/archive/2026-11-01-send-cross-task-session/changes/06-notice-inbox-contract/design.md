# 06 技术要点

## 要点

1. **稳定身份**：接纳身份=来源名+内容摘要（SHA-256 前缀）+接纳序号；重投同身份幂等（在册即不重复接纳）。
2. **时序倒转**：现状"入箱→改名"倒转为"入箱（持久）→消费确认（Invoke 提交成功）→改名"；改名失败=保持待接纳（下次启动重投同身份，幂等）。
3. **Drain 语义**：Drain 返回副本；`ack` 由 Invoke 提交成功驱动（失败保留在册）。与 `unified-task-semantics` 三层语义同构（接纳≠交付≠确认）。
4. **来源分账**：新 `MetaKeyNoticeSource`（键词表入 `event-metadata-contract`）；消息→事实时逐条携带；回合 `trigger_source` 盖章语义零改动。
5. **故障注入测试**：子进程+kill 四窗口（借鉴 `settle_wake_single_drive_test` 的父子进程夹具形态）。

## 文件清单

- 改：`examples/wechat-bot/wechat.go`（internalInbox）、`wechat_notice.go`（改名时序）、`api/event`（MetaKey）、`examples/wechat-bot/wechat_test.go`。
- 测：四窗口故障注入（新文件）。

## 风险与回退

- 风险：`.notified` 改名移时序会让"下次启动重报"的行为消失——旧语义（re-notice on next boot）在新语义下由幂等重投承接，测试钉住。
- 回退：接纳记录 additive；旧标记文件格式只读兼容。
