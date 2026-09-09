## Why

今日日志体检查出三个问题，本计划一次性修复：

1. **SDK 游标纯内存**（战场 A）：`wechat/poller.go` 的 `getUpdatesBuf` 是纯内存游标（L17/L31/L191/L199），进程重启即丢——重启后从头拉取，触发网关全量重放，与应用层缺陷叠加形成重复消息风暴。
2. **应用层无幂等**（战场 B-1）：`examples/wechat-bot/main.go:471` 的 OnMessage 入口对网关重放无任何防御，同一条消息可被注入 agent 多次，造成重复回复/重复执行。
3. **v2 保险脚本隐患**（战场 B-2）：`restart-maintenance.sh` 中 env 快照 `SNAP=/tmp/tagent_env.snapshot` 在重启成功路径被 `rm -f`（L133 用后即焚）——而脚本本身依赖快照恢复 GOROOT/GOPATH 与完整 env（python spawn L100-115 读快照）。下次转世若 /tmp 快照已失，bot 可能以裸环境重启（缺 TAGENT_CONFIG/TAGENT_* 等），config 加载失败、行为不可预期。

## What Changes

### 战场 A：SDK 层游标持久化（/home/lighthouse/src/wechat-robot-go）

- 新增 `CursorStore` 接口（仿现有 `TokenStore` 风格，`auth.go:12-19`）：`Get() (string, error)` / `Save(cursor string) error`，游标为单值无需 `Clear` 语义。
- 新增 `FileCursorStore` 文件实现（参照 `FileTokenStore` 的 JSON 落盘 + 0600 权限 + 文件不存在返回零值）。
- `Poller` 集成：构造时从 store 加载游标；每次收到非空 `getUpdatesBuf` 更新内存游标后同步落盘（落盘失败仅告警不中断轮询）。
- 向后兼容：不注入 store 则行为与现在完全一致（游标仍在内存）；经 `Bot` Option（`WithCursorStore`）注入，`NewPoller` 旧签名不破坏。
- 补充单元测试（store 读写 + Poller 加载/落盘钩子）。

### 战场 B-1：应用层消息幂等（tagent/examples/wechat-bot/main.go）

- OnMessage 入口最前加去重闸：消息键 `MsgId`（幂等键来源见 design 决策 D3——网关响应体经核实**无 msg_id 字段**，`model.Message` 亦无；需按分层键方案落地）。
- seen 集合持久化到本地 JSON 文件（复用 `.wechat-config` 目录风格），带容量上限与淘汰，防无限增长。
- `go.mod` 增加 `replace github.com/SpellingDragon/wechat-robot-go => /home/lighthouse/src/wechat-robot-go`，使 bot 编译吃本地 SDK 源码（现状只有 tagent 的 replace）。

### 战场 B-2：v2 脚本 env 快照保留（restart-maintenance.sh）

- 成功路径不再 `rm -f "$SNAP"`：改为先归档 `cp -f "$SNAP" "$BASE/run/env.snapshot"` 再清 /tmp 副本。
- 快照读取顺序改为 /tmp 优先、`$BASE/run/env.snapshot` 兜底（跨重启/跨机器清理可用）。
- `bash -n` 语法校验 + 读回验证。

## Impact

- **SDK**（wechat-robot-go）：新增 `wechat/cursor_store.go`（或并入现有文件）、`wechat/poller.go` 接线、`wechat/options.go`/`wechat/bot.go` 暴露 Option；`go test ./wechat/...` 全绿。
- **应用**（wechat-bot example）：`main.go` 去重闸 + seen 持久化新文件（如 `dedup.go` + `dedup_test.go`）、`go.mod`/`go.sum` replace 调整；编译通过 + 冒烟。
- **脚本**：`restart-maintenance.sh` 快照生命周期改动。
- **行为影响**：bot 重启后不再重放历史消息（游标恢复 + seen 恢复双保险）；v2 转世不再可能裸环境启动。
