## Why

main.go 消费循环的 task 输出兜底路由依赖纯内存 `sync.Map`（`lastActiveChat`，main.go:312）。热换装/重启后进程内存清零，转世后第一条 task 输出（如本次销假报告）因无 `meta_chat_id` 且锚点为空被静默丢弃（log 35127 行 WARN 实证）。需将锚点持久化，保证跨进程存活。

## What Changes

- main.go：新增 `persistLastActiveChat` / `seedLastActiveChat` helper，写入 `run/last_active_chat`（原子写：临时文件+rename），启动时回种 `lastActiveChat`
- 持久化/回种失败仅 WARN 不致命（与 restart.done/env.snapshot 同级容错）
- `go vet` + 预构建到 /tmp 验证编译，git commit，随后以 restart-tagent.sh（old_pid=243828）自杀换装部署
- 转世后新进程验证：seed 日志行存在 + 销假报告成功送达用户微信

## Impact

- 代码：`tagent/examples/wechat-bot/main.go`（约 340-390 行区域，涉及 lastActiveChat Store/Load 两处调用点）
- 运行态：新增 `tagent/examples/wechat-bot/run/last_active_chat` 文件（既有运行态目录）
- 部署：restart-tagent.sh 自杀换装（机制已两次实战验证），部署前必须编译预验证
- 行为：转世后首条无 meta_chat_id 的 task 输出不再被静默丢弃
