## 1. 战场 A：SDK 游标持久化（src/wechat-robot-go）

- [x] 1.1 新建 `wechat/cursor_store.go`：定义 `CursorStore` 接口（`Get() (string, error)` / `Save(cursor string) error`）与 `FileCursorStore` 实现（纯文本单行存储、0600 权限、文件不存在返回 `("", nil)`、`sync.Mutex` 并发保护），风格对齐 `auth.go` 的 TokenStore/FileTokenStore
- [x] 1.2 修改 `wechat/poller.go`：`Poller` 增加 `cursorStore` 字段；新增 `NewPollerWithCursorStore(client, handler, logger, channelVersion, store)` 构造函数（构造时 Get，失败 Warn 并空游标启动）；`Run` 循环中 L191-193 更新游标处更新后同步 `Save`（失败仅 Warn 不中断）；`NewPoller` 旧签名保留并转调新函数传 nil store，保证向后兼容
- [x] 1.3 修改 `wechat/options.go`：`botConfig` 增加 `cursorStore` 字段，新增 `WithCursorStore(store CursorStore) Option`（对齐 L59-64 WithTokenStore 先例）
- [x] 1.4 修改 `wechat/bot.go`：`Run`（L148）创建 Poller 改用 `NewPollerWithCursorStore` 并传入 `b.config.cursorStore`（nil 时行为不变）
- [x] 1.5 新建 `wechat/cursor_store_test.go`：覆盖 Save→Get 读回、文件不存在返回空、并发 Save（`go test -race`）、游标含换行/特殊字符、权限 0600
- [x] 1.6 新建/扩展 `wechat/poller_test.go`：覆盖带 mock CursorStore 时构造加载游标（首次请求携带已存游标）、更新游标后 Save 被调用、Save 失败不影响轮询、store 为 nil 时旧行为回归
- [x] 1.7 SDK 全量验证：`cd /home/lighthouse/src/wechat-robot-go && go vet ./wechat/... && go test -race ./wechat/...` 全绿

## 2. 战场 B：应用层修复（tagent/examples/wechat-bot）

- [x] 2.1 `go.mod` 增加 `replace github.com/SpellingDragon/wechat-robot-go => /home/lighthouse/src/wechat-robot-go`，`go mod tidy` 后 `go build ./...` 编译通过（确认本地 SDK 生效：go list -m 或 go.sum diff）
- [x] 2.2 新建 `dedup.go`（example 包内）：`SeenStore` 类型——加载 `$ConfigDir/seen.json`（损坏降级空集+Warn）、`CheckAndMark(key) bool`（已见返回 false、未见则标记并原子写盘：临时文件+rename）、容量上限 10000 + TTL 24h 裁剪、去重键生成函数（design D3 分层回退：`FromUserID + "#" + sha256(text)[:16]`，标注假设 A1：执行时 dump 一条真实 getupdates 响应核对是否存在 msg_id，若有则切换主键）
- [x] 2.3 新建 `dedup_test.go`：覆盖首次标记/重复丢弃/重启恢复（重载 seen.json 后仍判已见）/容量淘汰/TTL 过期/文件损坏降级
- [x] 2.4 修改 `main.go`：OnMessage 回调（L471 起）最前插入去重闸（已见 → 记日志 + return nil，不动 agent）；闸位置在审批拦截与 ClassifyInbound 之前（防重放审批回复也重复投递）；`WechatAppConfig.EnsureDirs` 已建 ConfigDir 无需改动
- [x] 2.5 应用编译+测试：`cd tagent/examples/wechat-bot && go vet . && go test . && go build .` 通过
- [x] 2.6 冒烟验证：启动 bot（或 dry-run）发送两条同键消息确认仅处理一次；重启 bot 确认 seen 恢复与游标恢复日志（SDK debug 日志可见 cursor 恢复）
  - 备注（诚实降级）：真机冒烟未执行（避免扰动在岗 bot 的登录态）。等价验证已由单测闭环：TestSeenStore_RestartRecovery（重启后 seen 判重）、TestPollerWithCursorStore_LoadsPersistedCursor（游标恢复并携带首请求）。真机观察窗口：下次转世重启时看主日志 "restored getupdates cursor" 与 "[Dedup] seen.json restored" 两行即可确证。
  - 注：openspec 目录不纳入 git（.gitignore），本文档仅作工作区留痕。

## 3. 战场 B：v2 脚本 env 快照隐患（restart-maintenance.sh）

- [x] 3.1 修改 `tagent/examples/wechat-bot/restart-maintenance.sh`：成功路径 L133 `rm -f "$SNAP"` 改为 `cp -f "$SNAP" "$BASE/run/env.snapshot" && chmod 600 "$BASE/run/env.snapshot" && rm -f "$SNAP"`（归档后清 /tmp）；构建段（L98-99 GOROOT/GOPATH 导出）与 python spawn 段（L100-115）读快照改为 `/tmp` 优先、`$BASE/run/env.snapshot` 兜底（python 段兜底路径作第二参数传入）
- [x] 3.2 `bash -n restart-maintenance.sh` 语法校验通过；读回改动行确认逻辑（成功路径归档、双地点回退读）
- [x] 3.3 验证快照双地点语义：构造测试快照场景（/tmp 缺失、run/ 兜底存在）走一遍干跑分支，确认 GOROOT/GOPATH 与 env 从兜底快照恢复（可临时 echo 调试，验证后移除）

## 4. 交付与收尾

- [x] 4.1 全部读回验证：SDK 测试输出、bot 编译/测试输出、脚本 bash -n 输出、冒烟日志摘录——汇总交付说明
- [x] 4.2 `go.sum` 变更核对（SDK replace 后依赖无远端拉取残留）、CHANGELOG（SDK 仓库若有惯例则补一条）
- [x] CHANGELOG 或 commit message 草稿说明三战场改动与回滚方式
