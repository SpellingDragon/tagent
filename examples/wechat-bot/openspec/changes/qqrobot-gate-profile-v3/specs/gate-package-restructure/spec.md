## Purpose

把门控与画像从单一 `service/chat_gate.go` 重构为两个内聚子包，补上历史缓冲欠账，让触发矩阵、统计、限流、历史、画像各自可测、可独立演进。

## ADDED Requirements

### Requirement: service/gate 子包结构

`service/gate/` MUST 包含四个文件：gate.go（触发矩阵）、stats.go（滑窗+中位数）、bucket.go（令牌桶）、history.go（群环形缓冲），并对外暴露窄接口供 robot.go/react 侧调用。

#### Scenario: 子包骨架可测
- **WHEN** 对 stats/bucket/history 分别运行表驱动单测
- **THEN** 全部通过（滑窗计数、中位数计算、令牌桶回填与耗尽、环形缓冲容量与过期）

### Requirement: 群历史环形缓冲

群历史 MUST 为环形缓冲，容量 20 条、TTL 10min；激活时注入 prompt 的历史上限 1200 字符，超出截断。

#### Scenario: 缓冲容量与过期
- **WHEN** 群内 10 分钟内涌入 25 条消息
- **THEN** 缓冲仅保留最近 20 条；更早消息因容量被挤出；10 分钟前的消息因 TTL 过期不注入

#### Scenario: 注入截断
- **WHEN** 激活时缓冲内历史拼接超过 1200 字符
- **THEN** 注入 prompt 的历史截断至 1200 字符以内

### Requirement: service/profile 子包结构

`service/profile/` MUST 包含 store.go（注册表 + `data/profiles.json` 原子读写）与 tools.go（三工具 add_user_alias / set_user_note / list_known_users 迁入）。

#### Scenario: 画像迁移无损
- **WHEN** chat_gate.go 的画像逻辑迁入 profile 子包
- **THEN** 别名/备注/角色等既有画像数据格式与 `data/profiles.json` 兼容，重启后可加载既有文件

### Requirement: chat_gate.go 退役

`service/chat_gate.go` MUST 被删除；其原调用方（robot.go 等）MUST 改用新子包的窄接口。

#### Scenario: 删除后全链路可编译
- **WHEN** 删除 chat_gate.go 并切换调用方到窄接口
- **THEN** `bash build.sh` 全绿，无残留对已删符号的引用

#### Scenario: 窄接口收敛
- **WHEN** robot.go/react 侧需要门控判定
- **THEN** 通过窄接口调用（触发判定、LLM 放行、历史注入），不直接触碰子包内部数据结构
