## 1. v4 接线（A+B）

- [x] 1.1 robot.go 入口 1（L245 附近 groupMessageHandler）：在既有 `gate.HistoryFor(data.GroupOpenID).Append(...)` 邻近补 `gate.StatsFor(data.GroupOpenID).Record(gm.D.Author.MemberOpenID, time.Now())`
- [x] 1.2 robot.go 入口 2（L429 附近 groupAtMessageEventHandler）：同法补 `gate.StatsFor(data.GroupID).Record(gm.D.Author.MemberOpenID, time.Now())`
- [x] 1.3 gating.go OwnerTriggered 低频路切换：`profile.LowFreqSpeaker(authorOpenID)`（L78 附近）→ `StatsFor(groupOpenID).LowFreqSpeaker(authorOpenID, now)`，now 复用函数内既有变量；确认 gateMu 持有下调 stats 锁（单向锁序）无死锁
- [x] 1.4 go build ./... 与 go test ./service/... 双绿（接线后首轮验证）

## 2. 双账本收敛（C）

- [x] 2.1 service/profile/profile.go：删除 `LowFreqSpeaker` 函数（v3 判定）及 `userProfile.Recent` 字段与 RecordUserProfile 内的 append/8 条修剪逻辑
- [x] 2.2 service/profile/profile.go：删除 `userProfile.FirstSeen` 字段及 LoadProfiles 内 `FirstSeen: time.Now().Add(-48 * time.Hour)` 回填；同步删除 `userProfile.LastSeen`（唯一写点随前删，删后无读者）；同步更新文件头注释"统计性字段"描述（MsgCount 保留注明仅 Summary 展示）
- [x] 2.3 全仓 grep 核对：`LowFreqSpeaker` 仅剩 gate 包定义+调用、`Recent`/`FirstSeen`/`LastSeen` 在 profile 包零残留；`profileDump` 五字段（openid/nickname/role/aliases/notes）未动 → profiles.json 落盘格式不变
- [x] 2.4 go build ./... 与 go test ./service/... 复绿（清理后重跑）；gate_test.go 既有 v4 用例（TestLowFreqFourConditions/TestLowFreqSelfLimit）全过

## 3. 重启与健康验证（D）

- [x] 3.1 重启 qqrobot 进程（按现场既有守护方式），确认 healthz 返回 ok、WS READY 会话建立、robot.log 无 panic/频错
- [x] 3.2 重启后冒烟：群内一条普通消息不误触发主动发言（owner_trigger 未命中且窗口未开）；@bot 正常响应（被动路不受影响）；次日观察 v4 观察期满后低频行为符合预期（可延后验证）

## 4. 收尾

- [x] 4.1 openspec validate --strict 通过；整理报账（改动文件清单 + 双绿输出 + 健康证据）向调用方请求归档
