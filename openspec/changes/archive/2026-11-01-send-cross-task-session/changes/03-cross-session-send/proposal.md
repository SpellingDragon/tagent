# 03 cross-session-send：对外同级会话通信

父变更：send-cross-task-session（依赖：01 参数面常数——接口常数依赖；`meditation` 为实现依赖）。

## Why

用户裁定："冥想使用的 session 需要向另一个 session 发送事件"、"只有同级别才会感知 agent+session"、送达语义=**入链+驱动一轮**。现状：冥想跨会话写走 `deliver_summary`（第二通信工具）；`CommitToSession` 只写链不驱动（R2 实证）。

## What Changes

- `send{target:{agent,session}, content}` 经进程内投递缝四道安全门（deliver_to 白名单 fail-closed / 盲投拒绝 / 未知目标 / 未运行具名拒绝）后：写事件入目标会话链并**驱动目标会话跑一轮**（复用 `Host.Invoke` 驱动面；忙碌会话在 one-slot gate 边界读取）。
- `deliver_summary` 删除归并：冥想改用 `send{target:{agent,session}}`（deliver_to 白名单语义保留），工具面消失。
- 治理：risk_rules 可对 send 跨会话形态配 critical→hold→批准补投（复用治理审批，非出网审核）。

## 边界与依赖

- **依赖**：01（`target.agent/session` 字段常数、回执格式）；`cross-session-delivery` 四道门为既有语义（复用不新开）。
- **被依赖方**：04（smoke/文档）、S3 症状锚。
- **接口面**：`send` 对外分派入口、`deliver_to` 白名单门、`Host.Invoke` 驱动面（只读复用）。
- **禁止事项**：不动出网投递链；不改四道门语义（白名单/盲投/未知/未运行）；不实现对内重入（02 域）；冥想会话禁 send 规则保留（冥想用 send 指向**他**会话，自身不接收）。
