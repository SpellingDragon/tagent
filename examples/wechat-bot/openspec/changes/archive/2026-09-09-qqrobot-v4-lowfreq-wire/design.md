## Context

前一 change `qqrobot-gate-profile-v3` 已落成 gate/profile 双子包与白名单/令牌桶/群主命令/history 注入链路；其遗留的 3.3/3.4/4.4 已在计划外接线完成（robot.go HandleOwnerCommand、HistoryFor().Append 均在位）。本 change 只处理其中未收尾的频率判定线：`service/gate/stats.go` 的 v4 四条件（用户 2026-09-09 确认）实现完整、有单测，但零调用——robot.go L245/L429 仅喂 profile，gating.go L78 低频路走 profile v3 硬计数，profile 与 GroupStats 双账本并存。约束：不破坏 profiles.json 落盘格式；改动最小化。

## Goals / Non-Goals

**Goals:**

- v4 成为低频激活的唯一频率判定，且统计输入覆盖两个消息入口
- 频率状态单一事实源（gate.GroupStats），profile 侧 v3 判定及其专用状态删除
- build + test 双绿，进程重启后健康

**Non-Goals:**

- 不动触发矩阵其余三路（活跃窗口 / owner_trigger 配置 / 提及群主 NickHit+IsOwnerOpenid——后者是身份判定，非频率账本）
- 不动白名单、令牌桶、群主命令、history 注入链路
- 不改 profiles.json 落盘结构，不做数据迁移
- 不实现 history/开关等前一 change 其他遗留（另案）

## Decisions

### D1. 接线位置：紧跟既有 HistoryFor().Append，两入口对称补一行

robot.go 两入口已有 `gate.HistoryFor(<groupOID>).Append(...)`（L249-250 / L430），在其后紧邻补 `gate.StatsFor(<groupOID>).Record(<memberOpenID>, time.Now())`。理由：与 history 记账同位、改动最小、两入口对称易审。替代方案（在 OwnerTriggered 内部懒记账）被否——会把"每条消息必记账"偷换成"判定时才记账"，中位数会被判定路径污染。

- 入口 1（L245，groupMessageHandler）：`gate.StatsFor(data.GroupOpenID).Record(gm.D.Author.MemberOpenID, time.Now())`
- 入口 2（L429，groupAtMessageEventHandler）：`gate.StatsFor(data.GroupID).Record(gm.D.Author.MemberOpenID, time.Now())`（该函数内 guildID/channelID 均源自 data.GroupID，直用即可）

### D2. gating.go 低频路切换：本包 StatsFor，不动函数签名

`OwnerTriggered` 内 `profile.LowFreqSpeaker(authorOpenID)` → `StatsFor(groupOpenID).LowFreqSpeaker(authorOpenID, now)`。now 复用函数内既有 `now := time.Now()`。理由：groupOpenID 与 authorOpenID 均已在签名参数中，调用点零签名变更。注意：gateMu 已持有（L56），StatsFor→GroupStats 各方法自带独立锁，锁序为 gateMu → statsMu/g.mu，单向无环，安全。

### D3. profile 收缩范围：删函数 + 删字段，LastSeen 一并退役

- 删 `LowFreqSpeaker` 函数（v3 判定，唯一调用点在 gating.go，随 D2 消失）
- 删 `userProfile.Recent` 字段及其 8 条修剪逻辑（RecordUserProfile 内）
- 删 `userProfile.FirstSeen` 字段及 LoadProfiles 的 `FirstSeen: time.Now().Add(-48h)` 回填（其唯一消费者是 v3 冷启动检查）
- 删 `userProfile.LastSeen` 字段（唯一写点在 RecordUserProfile，删除后无读者）——与 FirstSeen/Recent 同属"仅内存统计"注释所述的时间状态；保留则成死字段
- **保留**：`MsgCount`（Summary 展示"发言N"仍用）、Nickname/Role/Aliases/Notes（身份/别名/notes 落盘）、NickHit/IsOwnerOpenid/AddAlias/FindUserByNick/SetNote/Summary/Save/Load 全部不动
- profileDump 序列化不含任何被删字段 → 落盘格式天然不变，无需迁移

### D4. 验证面：go build + go test ./service/... + 运行时健康

go1.18 冻结链（bash build.sh 同源）。profile 包无既有测试（核对确认 *_test.go 零文件涉及 profile），删代码不破坏测试面；gate 包 gate_test.go 的 v4 用例（TestLowFreqFourConditions / TestLowFreqSelfLimit）随本次接线转正为真实覆盖。重启验证沿用既有 healthz/WS READY 断言。

## Risks / Trade-offs

- [v4 上线首 24h 低频静默] gate 统计纯内存，重启即重积累：first_seen 重置 → 观察期未满 → 低频路不触发。窗口/owner_trigger/@bot 不受影响。→ 可接受（用户既有语义：丢了自然重积累）；如需平滑可后续把 first_seen 落盘（本 change 不做）。
- [FIFO 淘汰边界] maxTrackUsers=512 淘汰最久不活跃者，超大群可能误删老用户统计。→ 既有行为，非本次引入，不处理。
- [删 LastSeen 属最小范围外多删一字段] 若调用方要求字面最小改动，可只删 Recent/FirstSeen 留 LastSeen 为死字段。→ 默认删除（死字段更易误导后续维护）；异议时回退成本一行。
- [双锁嵌套] gateMu 持有下调 GroupStats 方法（自带 g.mu）。→ 锁序单向，无死锁路径；测试 + 压测观察。

## Migration Plan

1. 接线（A/B）→ build + test 双绿
2. 清理 profile（C）→ build + test 复绿（删代码后重跑）
3. 重启 qqrobot（kill 旧 pid / nohup bin/qqrobot 或既有守护方式）→ healthz ok + WS READY + robot.log 无 panic
4. 回滚：git revert 两文件（robot.go / gating.go / profile.go 三处提交），数据无迁移无需回滚动作

## Open Questions

- （无需阻塞）重启命令形态：既有守护方式（nohup/systemd/守护脚本）由执行方按现场惯例执行——若与 healthz 断言冲突再补。
