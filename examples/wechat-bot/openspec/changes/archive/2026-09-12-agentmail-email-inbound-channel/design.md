## Context

- tagent 微信助手跑在 `/home/lighthouse/tagent/examples/wechat-bot`（`./wechat-bot` 二进制，HTTPAPI 健康端点 `127.0.0.1:8089/healthz`，POST `/task` 注入）。
- `tagent/rl/http_api.go`：`POST /task`（body: `messages[{role,content}]`）→ `InjectMessage` 进持久事件循环，loop 未激活时 503；已验证路径存在且语义明确（AReaL 桥就是外部调用者先例）。
- `main.go:380/483`：consumer 按 `trigger_source` 路由——外部 POST /task 注入的消息无 `chat_id` metadata，回复 fallback 到最近活跃微信会话（`lastActiveChat`）。即：**邮件触发的回复会出现在微信侧**，这是首期接受的行为。
- `main.go:576`：微信消息经 `InjectMessageWithMetadata("user", ..., {chat_id, user_name})` 注入——HTTPAPI 无 metadata 通道，两者注入面不同但殊途同归（都进持久循环）。
- 本机邮件通道已于 2026-09-11 定案：agent.qq.com（@tencent-qqmail/agently-cli，OAuth 已授权 weiyepeng@agent.qq.com），官方 `message +watch` 长轮询（NDJSON）+ shim 注入（详见 D2/D3 定案段）。

## Goals / Non-Goals

**Goals:**

- 邮件 → tagent 端到端打通：发测试邮件后 tagent 自动消费
- 拉取器常驻（systemd 优先 / tmux resident 等价），崩溃自恢复、去重状态持久化
- 代码与配置产物入 git；密钥不入库
- 决策点显式记录（D1–D5），有据可查

**Non-Goals:**

- 不改 tagent 框架本体（不加 webhook 端点、不加 HTTPAPI metadata 通道、不改 consumer 路由）——首个外部渠道用最小侵入面
- 不做邮件**回复**（tagent 输出 → 发邮件给原发件人）：回复面继续走微信
- 不做多 inbox / 多租户；只接一个专用 inbox
- 不做附件下载与富媒体解析（首期只取文本正文；若正文为 HTML 则粗提取文本）

## Decisions

### D1. 接线方式：外部拉取器 + 既有 HTTPAPI（POST /task），不进进程内

- **选**：独立进程（Python/Go 拉取器）→ `curl` 级简单 POST 到 `127.0.0.1:8089/task`
- **理由**：零侵入——不改 wechat-bot 与 tagent 框架代码；AReaL 桥已验证该端点可被外部调用；拉取器独立迭代/重启不影响 bot 主进程；进程隔离符合 systemd 常驻模板先例（`deploy/tagent-wechat.service`）
- **备选否**：①改 main.go 进程内轮询 goroutine——侵入 bot 二进制，每次改动需重新构建部署 bot，风险面大；②直接写 WAL/队列文件——绕过框架注入面，违反事件溯源完整性约定
- **风险注记**：POST /task 无 metadata 通道，邮件回复 fallback 到微信（`lastActiveChat`）。可接受：微信是主通知面；后续若要定向回执，做 `tagent-async-action-overhaul` 式框架增强（见 Open Questions）

### D2/D3.（定案 2026-09-11）监听机制：官方 `agently-cli message +watch` 长轮询，弃自写轮询与 AgentMail REST

- **定案**：环境探测发现原设想的 AgentMail（agentmail.to REST + API key）不是本机可用通道；实际通道为腾讯 agent.qq.com（@tencent-qqmail/agently-cli，OAuth 授权，weiyepeng@agent.qq.com）。D3 判定顺序三条路线全部废弃，改用官方 `message +watch` 长轮询子进程 + 轻量 shim：
  - `+watch --msg-format full` 逐行输出 NDJSON（`{"message": {...}}`），新邮件即时推送，无需自写定时轮询
  - 空轮询、瞬断、限频（exit code 1/4/7）由 shim 按 skill 错误处理表处置：网络类带退避静默重启子进程，限频按 Retry-After，授权失效（exit 3）告警降频
  - shim 只做三件事：解析 NDJSON → message_id 去重（seen-store，D5 不变）→ POST /task 注入（D1 不变）
- **实现语言**：shim 用脚本（Python/Shell 择一，随阶段②实现定）；CLI bin 用绝对路径调用（npm 全局 bin 在 ~/.local/lib/node-v22.14.0-linux-x64/bin，不在非交互 PATH——已定位）
- **理由**：官方长轮询免去自写轮询的间隔权衡/分页/重连逻辑，NDJSON schema 已取证（+watch 输出每行一个 message 对象，message_id 格式 msg_xxx）；shim 面最小、可测试
- **备选否**：①自写 60s 轮询 `message +list`——空转多、新邮件时延高、需自行处理分页与重放；②AgentMail REST——本机无该通道，原 spec 假设作废；③webhook——本机无公网可达回调端点（原 D2 已否）

### D4. 常驻形态：systemd 用户级单元（或 tmux resident 等价）

- **选**：`tagent-mail-poller.service`（平移 `deploy/tagent-wechat.service` 的 Restart=always + EnvironmentFile + 日志 journald 模式；放宽 ProtectSystem 白名单至拉取器数据目录）
- **理由**：工程已有 systemd 裸机常驻先例，运维心智一致；Restart=always 满足崩溃自恢复需求
- **备选否**：cron 定时任务——无进程内去重状态共享（每 tick 冷启动），且 60s 间隔下 cron 分钟粒度不够；tmux resident 为无 sudo 权限时的降级选项

### D5. 去重状态：磁盘 JSON 线程安全写（seen-store 平移）

- **选**：状态文件（如 `data/mail-poller/seen.json`），每次成功注入后原子写（tmp+rename）；上限 N 条（如 5000）滚动裁剪，避免无限增长
- **理由**：平移 `dedup.go` seen-store 模式；磁盘持久化使重启不重复注入；原子写防半截文件
- **备选否**：内存去重——重启即重放全部历史邮件，违反去重要求；SQLite——一个 message_id 集合犯不上

### D6. 邮件→消息格式化约定

注入文本模板（首期）：

```
[邮件入站]
发件人: {from_name} <{from_addr}>
主题: {subject}
时间: {date}
---
{body_text}
```

超长正文（>2000 字符）截断并附 AgentMail 原文 message_id 供 agent 需要时再取。

## Risks / Trade-offs

- [邮件回复落到微信侧（fallback lastActiveChat）] → 首期接受并在 README 明示"邮件任务的回复经微信送达"；后续框架增强列为 backlog（Open Questions #1）
- [轮询间隔 vs API 配额] → 首期 60s；若 AgentMail 侧限频则退避加倍（见注入重试逻辑同源退避）；日志记录 429 响应
- [拉取器与 bot 端口冲突/抢占] → 拉取器只作客户端连 127.0.0.1:8089，不监听端口，无冲突面
- [API key 泄露面] → key 只进 `.env`（gitignore 已覆盖该模式）；systemd 单元用 `EnvironmentFile` 引用，不在 unit 明文
- [bot 宕机期间邮件堆积] → 待投递队列 + 指数退避重试；bot 恢复后按序补投（spec「tagent 暂不可用」场景）
- [HTML 正文解析质量] → 粗提取文本（去标签）；解析失败则注入 `[正文为非文本格式，message_id=X]` 占位不阻塞

## Migration Plan

1. 部署顺序：先跑拉取器（手动前台验证一轮）→ 再装 systemd 单元 → 最后端到端冒烟
2. **回滚**：`systemctl disable --now tagent-mail-poller`（或 kill tmux 会话）即完全下线；删除 `deploy/tagent-mail-poller.service` 与拉取器脚本目录；wechat-bot 主进程**全程未改动**，零回滚成本；去重状态文件与日志可留档或随目录删除
3. 若 D3 选了 Go 且新增了依赖 → `git revert` 提交即回滚源码；go.mod/go.sum 若被污染按 `.bak` 先例恢复

## Open Questions

1. 邮件任务的定向回执（回复发给发件人而非微信）需要框架级 metadata 通道——是否立项做（依赖 tagent-async-action-overhaul 方向）？**不阻塞本期**，首期回复走微信 fallback。
2. AgentMail 免费层 API 限频具体数值未知——首轮部署后观测 429 日志再定最终轮询间隔。**不阻塞**，默认 60s 起步。
3. inbox 专用地址的获取方式（CLI 创建 vs 网页领取）取决于探测结果——tasks 1.x 落实，不影响架构。
