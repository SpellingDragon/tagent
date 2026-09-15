## Why

v4 中位数相对频率低频判定已在 `service/gate/stats.go` 完整实现（四条件：u ≤ max(1, 0.5×M)、M≥minMedian=3、观察满 24h、lifetime≥3，并有 gate_test.go 四用例覆盖），但全仓零调用——消息入口（robot.go L245/L429）只喂 `profile.RecordUserProfile`，频率判定仍走 `service/profile/profile.go` 的 v3 硬计数（尾随 24h ≤2 条），且 profile 与 gate.GroupStats 各自维护 FirstSeen/Recent/MsgCount 双账本：v4 是死代码，两套账本判定语义漂移。

## What Changes

- **A. 消息入口接线 v4 统计**：robot.go 两消息入口（L245 `groupMessageHandler` 用 data.GroupOpenID；L429 `groupAtMessageEventHandler` 用 data.GroupID）在既有 `gate.HistoryFor(...).Append(...)` 邻近补 `gate.StatsFor(<groupOID>).Record(<memberOpenID>, time.Now())`，两入口都喂。
- **B. 低频判定切 v4**：gating.go `OwnerTriggered` 低频路从 `profile.LowFreqSpeaker`（v3）切换为本包 `StatsFor(groupOpenID).LowFreqSpeaker(authorOpenID, now)`（v4 四条件）；触发矩阵其余三路（活跃窗口 / owner_trigger / 提及群主）不动。
- **C. 收敛双账本**：删 profile 侧 v3 `LowFreqSpeaker` 及其专用状态——`Recent` 滑窗（含 8 条修剪）、`FirstSeen` 字段（含 LoadProfiles 的 -48h 回填，其唯一消费者是 v3 冷启动检查）；保留身份/别名/notes 落盘与 `MsgCount`（仅 Summary 展示）。
- **D. 验证**：go build + go test ./service/... 双绿后重启 qqrobot 进程并验证健康。

## Capabilities

- `low-freq-v4-wiring`：群消息全量记账 + 低频激活 v4 四条件判定生效、v3 绝对计数退役
- `profile-ledger-convergence`：频率状态单一事实源、画像职责收缩到身份/别名/备注/展示计数、profiles.json 落盘格式不变

## Impact

- 代码：`QQchannelRobot/robot.go`（两入口各 +1 行）、`service/gate/gating.go`（低频路切换）、`service/profile/profile.go`（删函数与字段）。
- 数据：`data/profiles.json` 落盘格式不变（profileDump 不含统计字段，删字段不触序列化）；gate 统计纯内存，重启后观察期重新起算（既有语义）。
- 运行时：v4 上线后首个 24h 内低频激活静默（first_seen 冷启动重积累）——预期行为，非回归；窗口/owner_trigger/@bot 路不受影响。
- 不动：白名单/令牌桶/群主命令/history 注入链路（前一 change 已接线）；`profile.NickHit`/`profile.IsOwnerOpenid`（提及群主路，非频率账本）。
