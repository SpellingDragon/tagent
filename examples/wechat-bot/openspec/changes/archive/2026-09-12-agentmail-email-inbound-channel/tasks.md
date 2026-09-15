## 1. 环境探测与选型定案（决策点 D3）

- [x] 1.1 探测本机邮件通道就绪情况（✅ 阶段①报账实证：@tencent-qqmail/agently-cli 已装并更新至最新，npm 全局 bin 在 ~/.local/lib/node-v22.14.0-linux-x64/bin、不在非交互 PATH——根因已定位；agent.qq.com OAuth 有效：+me ok，weiyepeng@agent.qq.com，scopes alias:read/mail:delete/mail:read/mail:send；agently-mail skill 已装入 ~/.claude/skills/agently-mail——CLI bin 与 SKILL.md 计划侧已读回核实）
- [x] 1.2 探测 wechat-bot HTTPAPI 存活（✅ 实证：POST /task 返回 202 accepted，probe 消息已进持久循环并回显到微信，贯通确认）
- [x] 1.3 依 D3 判定顺序定实现形态（✅ 定案 D3'：弃自写轮询与 AgentMail REST，采用官方 `agently-cli message +watch` 长轮询监听——逐行 NDJSON 输出、空轮询/瞬断由官方处理、--msg-format full/event 两模式、schema 已取证；结论随 1.5 落盘 notes）
- [x] 1.4 实证注入契约（✅ POST /task → 202 {"status":"accepted"}，probe 消息进 loop 并回显到微信，全链路贯通）
- [x] 1.5 补写 `docs/notes/mail-poller-notes.md`：阶段①探测结论（CLI 安装形态/PATH 根因、OAuth 与 scopes、skill 位置）、D3' 定案与 watch schema 取证、POST /task 契约实证——把口头结论落盘归档（1.1/1.3 的欠账，归档前必须完成）

## 2. 监听器实现（决策点 D2/D5/D6；按 D3' 定案重述）

- [x] 2.1 新建 `examples/wechat-bot/mail-poller/` 目录（与 bot 同级受 git 管理），实现 shim 主程序：拉起并看护 `agently-cli message +watch --msg-format full` 子进程 → 逐行读 stdout（NDJSON）→ 解析 message_id/发件人/主题/时间/正文 → 按 D6 模板格式化 → POST /task 注入（默认 127.0.0.1:8089）；CLI 用绝对路径可配置（bin 不在非交互 PATH）
- [x] 2.2 实现去重 seen-store：`data/mail-poller/seen.json`，成功注入后原子写（tmp+rename），5000 条滚动裁剪；watch 重连重放/重启场景下同一 message_id 不二次注入
- [x] 2.3 实现投递与看护重试：POST /task 连接失败/503 时该邮件保持待投递，指数退避（30s 起，上限 10min），期间不标记已处理；429 限频按退避处理并记日志；watch 子进程退出按 exit code 分级处置（1/4 网络类→带退避静默重启子进程；7 限频→按 Retry-After；3 授权失效→告警日志并降频重试，不得静默死循环）
- [x] 2.4 实现配置装载：从 `.env`（或 `mail-poller.env`）读 CLI bin 绝对路径、tagent 端点、msg-format、PATH 注入项；缺关键项启动即报错退出（fail-fast）；OAuth 凭据沿用 CLI 自身存储（~/.agently-cli），shim 不经手令牌
- [x] 2.5 冒烟自测：shim 前台手动跑一轮——向 weiyepeng@agent.qq.com 发一封测试邮件，观察日志出现"watch 收到 NDJSON→解析→格式化→POST /task 202→去重标记"完整链路；kill 并重启 shim 再验证，确认不重复注入旧邮件
- [x] 2.6 邮件解析健壮性：HTML 正文粗提取文本；非 JSON 行/解析失败记日志跳过或注入占位符 `[正文为非文本格式，message_id=X]` 不阻塞流程；超长正文按 D6 截断并附 message_id

## 3. 常驻化（决策点 D4）

- [x] 3.1 编写 `deploy/tagent-mail-poller.service`（平移 tagent-wechat.service 模式：Type=simple + Restart=always + RestartSec=5 + EnvironmentFile + journald 日志；ReadWritePaths 仅放行 shim 数据目录；PATH 或 ExecStart 需处理 node bin 目录不在系统 PATH 的问题）
- [x] 3.2 安装并启用：`systemctl enable --now tagent-mail-poller`（无 sudo 权限时降级 tmux resident：独立 tmux 会话脚本，形态记入 notes）
- [x] 3.3 崩溃自恢复验证：kill -9 shim 进程，确认 5s 内自动重启、watch 子进程被重新拉起且去重状态完好（重放无重复注入日志）
- [x] 3.4 开机自启确认：`systemctl is-enabled` 输出 enabled（或 tmux 形态记入 crontab @reboot 等价方案）

## 4. 端到端验证与文档

- [x] 4.1 端到端冒烟：向 weiyepeng@agent.qq.com 发主题含 `[e2e-test]` 标记的测试邮件 → 监听时延+消费时延内 → wechat-bot 日志出现 `[Agent][user->...]` 由该邮件触发的回复记录 → 微信侧收到对应回复；全程日志摘录记入 notes
- [x] 4.2 bot 宕机恢复演练：停 wechat-bot → 发邮件 → shim 按退避重试 → 重启 bot → 邮件补投成功（日志可证）
- [x] 4.3 编写 `mail-poller/README.md`：架构一句话（+watch 长轮询 + shim 注入）、部署步骤（env 模板、systemd/tmux 安装、CLI PATH 注意）、验证方法、**回滚方式**（disable 服务/杀 tmux 会话；bot 零改动零回滚）
- [x] 4.4 更新 `.env.example` 或新增 `mail-poller/.env.example`：列出全部配置项与说明，注明密钥/OAuth 凭据不入 git
- [x] 4.5 git 提交：代码 + 单元文件 + README + notes（一次性或分阶段均可），确认 `git status` 无密钥/凭据文件混入（.env 与 ~/.agently-cli 均不入库）

## 5. 归档与交接

- [x] 5.1 对照 specs/email-inbound-polling 逐条 Requirements 场景过一遍（新邮件注入/去重/重启去重/暂不可用重试/崩溃自恢复/凭据不入库/端到端冒烟），结果记入 notes 的验证清单小节
- [x] 5.2 openspec archive：全部任务完成后 `spec(op="archive")` 归档本变更
