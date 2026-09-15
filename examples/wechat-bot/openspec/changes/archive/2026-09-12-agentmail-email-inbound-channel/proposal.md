## Why

tagent 微信助手目前只有微信一个入站渠道。引入 AgentMail（agentmail.to）后，用户可通过发邮件向助手提交任务——邮件即指令，无需公网回调（本机无公网可达端点），拓宽助手的可达面。

## What Changes

- 新增独立邮件拉取器（poller）：定时调用 AgentMail `GET /inboxes/{inbox_id}/messages` 列信，对未见过的 `message_id` 去重后经 `GET .../messages/{message_id}` 取正文，注入 tagent 持久事件循环
- 注入采用 tagent 既有 `POST http://127.0.0.1:8089/task` HTTPAPI（202 accepted；loop 未激活时 503），首轮不做框架改造
- 拉取器常驻化（systemd 单元或 tmux resident），模板平移自 `deploy/tagent-wechat.service` 先例
- 邮件→任务消息的格式化约定、投递失败重试、邮件去重状态文件（平移 `dedup.go` seen-store 模式）
- 端到端验证：向专用 inbox 发一封测试邮件，tagent 侧自动收到并消费（日志 + 微信侧可观测回复）

## Impact

- 新增代码：拉取器脚本（Python 或 Go，取决于环境探测结果）、systemd/tmux 单元文件、README 文档；产物入 git 提交
- 运行依赖：本机 AgentMail CLI/SDK/API key 就绪情况（并行探测中）；tagent wechat-bot 进程常驻（健康端点 `127.0.0.1:8089/healthz`）
- 交互面：邮件注入经 `InjectMessage`（source="user"），consumer 按 `trigger_source=user` 路由回复——当前 fallback 到最近活跃微信会话（main.go:380/483），微信侧会看到邮件触发的回复；外部渠道独立回执在拉取器侧归档日志（首期不改 tagent 框架）
