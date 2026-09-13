# mail-poller 实施笔记（openspec: agentmail-email-inbound-channel）

> 2026-09-12 实施完成。本笔记为任务 1.5 落盘（阶段①结论）+ 5.1 验证清单。

## 阶段① 环境探测与选型定案

- **CLI**：`@tencent-qqmail/agently-cli`（npm 全局，bin 于 `~/.local/lib/node-v22.14.0-linux-x64/bin`，不在非交互 PATH——早前"未安装"探测扑空的根因）
- **授权**：OAuth 有效，`+me` ok，邮箱 `weiyepeng@agent.qq.com`（primary），scopes: `alias:read/mail:delete/mail:read/mail:send`
- **选型定案（D2/D3'）**：官方 `message +watch --msg-format full` 长轮询（NDJSON 逐行、空轮询/瞬断静默重试）替代自写轮询；message 对象自带 body/attachments，无需二次 +read
- **实现语言定案**：Python 3.12 标准库（NDJSON 解析/HTML 剥标签/原子落盘/urllib 注入皆标准库强项；shell 无法胜任 2.6 解析健壮性）
- **注入契约实证**：`POST /task` body `{"messages":[{"role":"user","content":...}]}` → 202 accepted

## 事故与修复记录

1. **systemd 下 +watch exit 127**：CLI shebang `/usr/bin/env node`，systemd PATH 无 node → unit 加 `Environment=PATH=...node bin...` 后恢复。教训：tmux 手测通过 ≠ service 环境通过。
2. **systemd 行内注释**：`ProtectSystem=full  # 注释` 整值解析失败被静默忽略（verify 有告警），全部注释独立成行修复。
3. **发件两步确认**：`+send` 返回 confirmation_token，需**原样重放**（内容含动态时间戳会导致 "Request content modified"）。

## 5.1 验证清单（对照 specs/email-inbound-polling）

| # | 场景 | 结果 | 证据 |
|---|------|------|------|
| 1 | 新邮件注入 | ✅ | `poller.log 03:38:24 已注入 msg_p6zC...` + 微信侧回显（03:38 由 tagent 消费发出） |
| 2 | message_id 去重 | ✅ | seen.json 落盘；重启后无重复注入日志 |
| 3 | 重启去重 | ✅ | stop → 拉起，日志无 `msg_p6zC` 重复行，seen.json 持久 |
| 4 | bot 暂不可用重试 | ✅ 等效演练 | TAGENT_TASK_URL 指死端口：1s→2s→4s→8s 指数退避（notes 03:46 日志），恢复后补投语义成立 |
| 5 | 崩溃自恢复 | ✅ | SIGKILL MainPID 3882612 → 7s 内 3882834 active，seen 完好 |
| 6 | key 不入库 | ✅ | token 在 ~/.agently-cli/（仓外）；.env 无密钥；git status 待 4.5 核对 |
| 7 | 端到端冒烟 | ✅ ×2 | 第一封 03:38（tmux 实例）+ 第二封 03:45:42 `msg_RxJH...`（systemd 实例），两封微信侧均收到回显 |

## 已知限制

- `POST /task` 无 metadata 通道 → 邮件回复 fallback 最近活跃微信会话（design Open Question，定向回执待后续）
- 回复通道 fallback 行为在两次冒烟中均实际发生（回显进当前会话），符合预期但非定向
