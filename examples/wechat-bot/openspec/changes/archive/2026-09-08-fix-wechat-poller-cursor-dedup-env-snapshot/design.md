## Context

三个战场的问题相互叠加：SDK 游标丢失 → 重启后网关重放全量历史 → 应用层无幂等 → 重复消息进 agent。v2 脚本快照即焚则是独立的转世可用性隐患。本设计覆盖三个战场的实现方式与关键决策。

**核实过的现状**（读回真材料）：

- SDK：`wechat/poller.go` L17 `getUpdatesBuf string`（内存游标）、L31 初始化 `""`、L191-193 更新、L199 随请求携带；无持久化钩子。
- `TokenStore` 接口（`wechat/auth.go:12-19`）：`Load/Save/Clear`，`FileTokenStore` 为 JSON 文件 + 0600 权限实现，Load 文件不存在返回 `nil, nil`。
- Bot 装配：`wechat/bot.go` NewBot 按 cfg 逐项构造（tokenStore/contextTokens 可经 Option 注入，`wechat/options.go` 有 `WithTokenStore`/`WithContextTokenStore` 先例）；`Bot.Run` L148 创建 Poller 时未传 store。
- 应用层：`main.go:471` OnMessage 注册（先审批拦截 → `ClassifyInbound` → goroutine 注入）。`WechatAppConfig`（main.go L591-601）从 yaml `app.wechat` 段加载，`EnsureDirs` 建 `.wechat-config` 等目录。
- **Message 无 MsgId**：`wechat/internal/model/message.go` L40-49 的 Message 结构仅 FromUserID/ToUserID/ClientID/MessageType/MessageState/ContextToken/GroupID/ItemList；`docs/wiki/iLink-API-Reference.md` L209-227 的 getupdates 响应样例与字段表亦无 msg_id。**调用方所述"MsgId"在网关响应中不存在**（D3 给出分层键方案）。
- go.mod：example 仅 `replace github.com/SpellingDragon/tagent => ../..`，`wechat-robot-go v1.5.0` 无 replace → bot 当前吃的是远端版本，本地 SDK 改动不生效，需加 replace。
- v2 脚本：`SNAP=/tmp/tagent_env.snapshot`（L31）；构建段 L98-99 读 SNAP 恢复 GOROOT/GOPATH；python spawn 段 L100-115 读 SNAP 恢复完整 env；成功路径 L133 `rm -f "$SNAP"` 用后即焚；`DONEDIR=$BASE/run` 已存在（L33）。

## Goals / Non-Goals

**Goals：**

- SDK：游标可持久化、可恢复，不配置则零行为变化；`go test ./wechat/...` 通过。
- 应用：入口幂等闸 + seen 持久化 + 本地 SDK replace；编译 + 冒烟通过。
- 脚本：快照双地点留存（/tmp 热路径 + run/ 兜底），转世永不裸环境。
- 全链路读回验证后交付。

**Non-Goals：**

- 不改 SDK 的 token/context token 存储行为。
- 不为 SDK 引入 SQLite/外部数据库依赖（游标用纯文件）。
- 不做 agent 消费侧的二次幂等（入口闸是唯一防线）。
- 不改 v1 脚本（restart-tagent.sh 已废弃）与 crontab 部署方式。
- 不处理去重键的网关侧协议演进（若未来网关下发 msg_id，切换键即可，见 D3 备选）。

## Decisions

### D1：CursorStore 用单值接口，不做版本化

仿 TokenStore 风格但更简：`Get() (string, error)` / `Save(cursor string) error`。游标是单值不透明字符串，无需 Clear（清游标 = 传空串 Save 即可）与版本迁移。备选（带 schema 的 map）被否：无真实多键需求。

**注入路径**：`botConfig.cursorStore` + `WithCursorStore(store CursorStore) Option`（对齐 `WithTokenStore` 先例，options.go L59-64）→ `Bot.Run` 创建 Poller 时传入。`NewPoller` 保持旧签名不动（bot.go L148 是唯一调用点，改为带 store 的新变体或 variadic，倾向新增 `NewPollerWithCursorStore`，旧函数转调，保证 example/外部调用零破坏）。

**落盘时机**：poller.go L191-193 更新内存游标处，更新成功后同步 Save（失败仅 `logger.Warn`，不影响轮询）。构造时（NewPollerWithCursorStore 内）Get，失败 Warn 并以空游标启动。

### D2：FileCursorStore 纯文本一行存储

对照 FileTokenStore 的 JSON 方案，游标是单值且可能含特殊字符，用纯文本单行 + TrimSpace 存储最稳。文件不存在 → `("", nil)`。写盘用 `os.WriteFile(path, []byte(cursor+"\n"), 0600)`，加 `sync.Mutex` 保护并发写（poller 单 goroutine 更新，但防御测试场景并发）。备选（JSON `{"cursor": "..."}`）被否：引号转义徒增复杂度；单行纯文本即 token 文件惯例。

### D3：去重键 = 分层回退键（假设标注，需执行方确认）

**矛盾点**：调用方说"MsgId 去重"，但读回证实网关响应与 `model.Message` 均无 msg_id 字段（见 Context）。不能臆测一个不存在的字段。采用分层回退：

1. `FromUserID + "#" + sha256(text)[:16]`（同用户同文本同窗口视为同一条）——主力键；
2. 若未来 SDK Message 增补 msg_id 字段，切为主键（留 TODO 标注）。

风险：用户短窗口内连发两条相同文本会被误杀（视为重放）。缓解：键加时间分桶（如分钟级 epoch 分桶）或 seen TTL。**执行方落地前应再次确认网关真实响应（可加 debug 日志 dump 一条 getupdates 原始响应核对）**；若实际存在 msg_id，改用之。此为计划中的显式假设 A1。

### D4：seen 持久化用自管 JSON，不用 tagent KV

自管 JSON（`$ConfigDir/seen.json`）结构简单、无跨模块耦合、测试独立；tagent KV 引入框架依赖（KV 能力现状未核实），换取的是已验证非必需的复用。格式：`{"seen": {"<key>": <unix_ms>}}`，加载时按容量上限 + TTL 双策略裁剪。容量上限 10000、TTL 24h（防文件膨胀）。写策略：新键加入即整文件原子写（临时文件 + rename），QPS 低（人发消息），性能可忽略。

### D5：v2 脚本改"归档不即焚"

成功路径 L133 `rm -f "$SNAP"` → 先 `cp -f "$SNAP" "$BASE/run/env.snapshot"`（chmod 600）再 `rm -f "$SNAP"`（/tmp 清理保留，防下次误用过期 /tmp 快照）。构建段与 python spawn 段读快照改为两地点回退：`SNAP` 存在用之，否则用 `$BASE/run/env.snapshot`。两处（构建段 GOROOT/GOPATH 导出、python 读快照路径）都改，python 段直接把兜底路径作为第二参数传入。防漂移：`bash -n` 校验 + 部署后读回。

## Risks / Trade-offs

- [文件游标损坏] → Get 时 JSON/文本解析失败：返回空游标 + Warn（spec 场景已定降级路径），最坏回到现状（重放一次，被 B-1 幂等闸兜住）。
- [游标 Save 失败] → 轮询不中断，游标仍在内存，行为同现状；日志告警可发现。
- [去重键误杀]（D3 假设）→ 分桶/TTL 缓解；若网关实际有 msg_id 则切换主键，误杀消失。
- [seen.json 损坏] → 重载为空集 + Warn（spec 降级场景），最坏重放窗口内重复处理一次。
- [v2 兜底快照过期]（跨版本 TAGENT_* 变更后未重投递）→ 兜底快照仍有 env 骨架（HOME/PATH/GOROOT），优于裸环境；.env 真源在 BASE/.env，人工重投成本可控。
- [SDK replace 引入] → bot 编译绑死本地路径，跨机器部署需同步 SDK 源码——本机即同一部署单元，可接受；CI 若有远端拉取会失败，需同步 go.sum。

## Migration Plan

1. SDK 先行：接口 + 实现 + Poller 集成 + 测试，`go test ./wechat/...` 全绿（不破坏既有 API）。
2. bot 侧：go.mod 加 replace → 去重闸 + seen 持久化 → `go build` → 冒烟（发送两条同键消息观察单次处理；重启 bot 观察游标恢复与 seen 恢复）。
3. 脚本：改 restart-maintenance.sh → `bash -n` → 等待下次 cron 触发或手动 `bash` 干跑读回 restart.log。
4. 回滚：SDK 改动不注入 store 即回旧行为；bot 去重闸可用配置开关或回退二进制（wechat-bot.prev）；脚本回滚 = 恢复 `rm -f "$SNAP"` 单行改动。

## Open Questions

（无——D3 去重键已按假设 A1 在计划内标注备选，执行时按"读回真实响应"确认即可，不阻塞任务拆解。）
