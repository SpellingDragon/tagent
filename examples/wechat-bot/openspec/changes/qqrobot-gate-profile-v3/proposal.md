## Why

机器人当前在群内的三类非 @ 主动发言路径（@回复、昵称/别名触发、低频激活）缺乏统一的发言门控：任何群的任何消息都可能触发 LLM 调用，既无法按群收口，也缺历史上下文注入（前期宣称未实装）。同时 `service/chat_gate.go` 一个文件承载了触发矩阵、用户画像、令牌桶三块正交逻辑（388 行），画像三工具与之耦合在同一包内，职责边界模糊。本 change 建立"群级发言白名单 + 相对频率触发 + 群主可控开关"三重门控，并把门控与画像重构为 `service/gate/` 与 `service/profile/` 两个内聚子包，补上历史缓冲欠账。

基线：前一 change `qqrobot-group-tools-concurrency`（15/22 done）遗留实弹验证与归档（5.2/5.3/5.4），**不并入本 change**，仍由调用方另行补证归档，不阻塞本 change 开工。

## What Changes

- **主动发言白名单**（群级）：机器人一切群内主动发言（@回复 / 昵称触发 / 低频激活）SHALL 要求本群在 `group_speech_whitelist` 中；空 = allow-all + 启动一次性警告（沿用 `service/whitelist.go` 既有两态语义）。群主命令"开启白名单/关闭白名单"切换本群，持久化 `data/group_whitelist.json`（tmp+rename 原子写）；群主身份判定 = 画像 role==owner **或** WS 帧 member_role==owner；切换动作记审计日志。LLM 工具（mute/profile 工具组）产生的发言同样受此约束。
- **相对频率触发**：用户尾随 24h 发言数 u ≤ 0.5×群中位数 M，且 M≥3（死群停用）；观察满 24h（first_seen 持久化）；lifetime≥3（新号防御）；激活自增发言数→自然退出；群级令牌桶（容量 2 / 每 4h 回填 1）兜底；统计留内存不落盘。
- **包重构**：新建 `service/gate/`（gate.go 触发矩阵、stats.go 滑窗+中位数、bucket.go 令牌桶、history.go 群环形缓冲 20 条/10min——补上历史缓冲欠账，激活时注入 prompt 上限 1200 字符）与 `service/profile/`（store.go 注册表 + `data/profiles.json` 原子读写、tools.go 三工具迁入）；`service/chat_gate.go` 退役删除；robot.go/react 侧换窄接口接线。
- **群主可控开关**：群主命令"关闭机器人/开启机器人"切换本群低频激活与昵称触发（@bot 保持响应），状态入 `data/group_whitelist.json` 同文件。

## Capabilities

- `group-speech-whitelist`：群级主动发言白名单 + 群主切换命令 + 持久化
- `relative-frequency-trigger`：相对频率（中位数相对）低频激活触发
- `gate-package-restructure`：service/gate + service/profile 子包化与 chat_gate.go 退役
- `owner-bot-switch`：群主"关闭/开启机器人"本群开关

## Impact

- 代码：`QQchannelRobot/service/gate/`（新）、`QQchannelRobot/service/profile/`（新）、`service/chat_gate.go`（删）、`robot.go`（接线换窄接口）、`react_agent.go`/`profile_tools.go`/`mute_tool.go`（工具迁入与白名单约束）、`entity/channel_config.go`（新增可调参数）。
- 数据：`data/group_whitelist.json`（新）、`data/profiles.json`（沿用，迁移到 profile 子包管理）。
- 工具链：`bash build.sh`（go1.18 冻结链）全绿；stats/bucket/history 各配表驱动单测。
- 知识库：kb 归档设计决策与参数表。
- 不影响：前一 change 的实弹欠账（5.2/5.3/5.4）另案处理。
