# Tasks: qqrobot-control-link-optimization

> 优先级：A（本地指令通道）> B（事件联动）> C（白名单收紧）> D（上线验证）。
> 约束：robot 正在服务（pid 3008871），重启需谨慎但可接受（秒级，守护兜底）；
> 所有改动读回验证；不碰 cookie.json。

## 1. A — 本地指令通道（agent→robot，最高优先）

- [ ] 1.1 盘点 `service/commands.go` 指令注册表（commandRegistry、GetCommand、CommandArgs 字段语义、全部 14 个指令的 Keyword/Aliases），确认 /cmd 分发所需的最小导出面（必要时给 service 包新增只读枚举函数，如 `ListCommands()`）
- [ ] 1.2 在 `ops_server.go` 实现 `POST /cmd`：解析 `{command, args[]}` JSON，经 `service.GetCommand` 分发，合成 `CommandArgs{Author: "local-admin", AuthorName: "local-admin", From: config 默认消息频道, ChannelID/GuildID: config 值}`，未知指令返回 404 JSON，panic/错误兜底返回 500
- [ ] 1.3 在 `ops_server.go` 实现 `GET /cmd`：列出注册表内全部指令（keyword + aliases + description），JSON 数组返回
- [ ] 1.4 编写 `ops_server_test.go`（httptest）：覆盖 GET /cmd 列表非空、POST /cmd 未知指令 404、POST /cmd 合法指令 200、参数缺省默认值、端口绑定仅 127.0.0.1
- [ ] 1.5 旧端点回归：/healthz /logs /safe 行为不变（现有测试或手动 curl 验证）

## 2. C — 白名单收紧

- [ ] 2.1 修 `robot.go`/`engine.go` 中 IsInWhiteList 硬编码 `return true`：改为读 config `white_list` 列表（config 结构体新增字段 + yaml 解析）；配置了→严格校验；未配置→保持兼容 allow-all 且启动时醒目 Warn（log 到启动日志）
- [ ] 2.2 编写白名单单测：两态覆盖——配置列表（命中/未命中各一例）与未配置（allow-all + Warn 触发）
- [ ] 2.3 config.yaml 增加 `white_list: []` 注释示例（不动密钥段，python 定点插行方式）

## 3. A+C 本地验证

- [ ] 3.1 go build 通过（GOROOT=/usr/local/go.bak 旧工具链铁律，build.sh 或等价命令）
- [ ] 3.2 `go test ./...` 全绿（新增 ops_server_test + whitelist_test）

## 4. B — 事件联动（robot→agent）

- [ ] 4.1 确认 tagent HTTP API（8089）`/task` 喂任务端点的路径与 payload 契约：读 tagent 源码（`tagent/` 下 server/router 相关文件），记录 handler 路径、方法、字段、返回码
- [ ] 4.2 改造 `resources/check.shell`：进程不在→拉起后 POST 告知 agent；healthz 异常/storm 失败/cookie 过龄→POST 告警；正常静默。crontab 频率改 30 分钟（原为每小时）。顺带修 `robot.log..gz` 双点文件名 bug
- [ ] 4.3 B 组干跑：不动 robot 服务，用 curl 模拟 POST 告知/告警到 agent 8089（或干跑脚本打印 payload 不实发），验证 payload 契约与脚本逻辑
- [ ] 4.4 修 cron.log 里已出现的 `robot.log..gz` 残留（如改名/清理）

## 5. D — 上线与验证（收尾，激活 A/C + 全量实弹）

- [ ] 5.1 重启 qqrobot（分离会话方式，秒级盲窗，守护兜底），读回验证：`pgrep -x qqrobot` 新 pid、启动日志无 fatal、/healthz 200
- [ ] 5.2 实弹验证 A：`curl 127.0.0.1:9601/cmd` 列指令；POST /cmd 触发一条无害指令（如查状态类），读回机器人响应（日志或频道消息）
- [ ] 5.3 实弹验证 C：white_list 未配置→allow-all + 启动 Warn；配置测试项→拒绝非白名单消息（可借 QQ 频道消息或单测级模拟）
- [ ] 5.4 实弹验证 B：触发 check.shell 一次，确认拉起/告警 POST 到达 agent（agent 侧日志或回执），正常路径不打扰
- [ ] 5.5 QQchannelRobot git 提交（含 ops_server.go、engine.go/robot.go、check.shell、config.yaml 示例、测试文件；不动 cookie.json、不 push 除非用户指示）
- [ ] 5.6 同步 skill 事实文档 `tagent/examples/wechat-bot/skills/go-service-ops/qqchannel-robot-facts.md`：新增本地指令通道（/cmd 端点、local-admin 语义）、白名单两态行为、check.shell 30 分钟周期与 /task 告警联动、双点文件名 bug 修复记录

## 验收标准

- GET/POST /cmd 实弹可用，未配置白名单时 allow-all 且启动有 Warn
- go build + go test 全绿
- check.shell 干跑与实弹均符合"没事不吵"原则
- git 提交完成、事实文档同步、无 cookie.json 变更
