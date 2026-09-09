## Context

- 真材料路径：工程代码在 `/home/lighthouse/QQchannelRobot/`，openspec 计划沙箱在 `tagent/examples/wechat-bot/openspec/`（两树分离，读写路径见 proposal）。
- 现状核实（已读回）：
  - `service/whitelist.go`：两态语义（空=allow-all + sync.Once 启动警告；local-admin 绕过），本 change 沿用。
  - `service/chat_gate.go`（388 行，将退役）：`userProfile`（Nickname/Role/FirstSeen/LastSeen/MsgCount/Recent[8]/Aliases/Notes）、`LowFreqSpeaker`（绝对阈值 ≤2/24h + lifetime≥3 + 冷启动 24h）、`NickHit`、`AddAlias`、`FindUserByNick`、`SetNote`、`OwnerAliasSeeds`、`IsOwnerOpenid`、`OwnerTriggered`（owner 窗口 + 昵称命中 owner + 低频）、`LLMAllowed`（冷却 + 令牌桶容量 2/4h 回填 1）、`SaveProfiles/LoadProfiles`（tmp+rename 落 `data/profiles.json`，统计不落盘）、`UserProfileSummary`、`ParseGroupAttachments`（与门控无关，注意归属）。
  - `service/profile_tools.go`：add_user_alias / set_user_note / list_known_users 三工具（依赖 FindUserByNick/AddAlias/SetNote/UserProfileSummary/SaveProfiles/ReactCtx）。
  - `robot.go` 接线点：L237 `RecordUserProfile`、L246-249 `OwnerTriggered`（@bot 前置 gate）、L586 `LLMAllowed`（default 分支）、L319/400/424/496/631 `IsInWhiteList`（命令白名单，另一概念，勿混）。
  - `entity/channel_config.go` L24-27：`OwnerTrigger []string`（owner_trigger）、`ActiveWindowSec int`（active_window_sec, def 300）、`CooldownSec int`（cooldown_sec, def 60）、`WhiteList []string`（white_list，命令白名单）。
  - 既有测试：`whitelist_ext_test.go`（外部包测试 IsInWhiteList）。
- 基线：`qqrobot-group-tools-concurrency` 15/22，实弹与归档（5.2/5.3/5.4）另案，不阻塞本 change。
- 工具链：`bash build.sh`，go1.18 冻结链（注意：`any`/generics 可用但保持既有代码风格；新增代码不引第三方依赖）。

## Goals / Non-Goals

**Goals:**

- 四块范围按 spec 落地：白名单、相对频率、包重构、群主开关。
- gate/profile 两子包可独立表驱动单测；chat_gate.go 删除后 build 全绿。
- config 参数全部可调（quiet_factor / min_median / history_cap / chat_window 等）。
- kb 归档设计决策与参数表。

**Non-Goals:**

- 不处理前一 change 的实弹欠账（5.2/5.3/5.4）与归档。
- 不改 `IsInWhiteList` 命令白名单语义（那是命令准入门，不是发言门）。
- 不迁移 `ParseGroupAttachments`（与门控无关，留在 service 层或随 robot.go）。
- 不落盘统计性字段（滑窗/中位数/令牌桶留内存）。

## Decisions

### D1 白名单数据模型与原子写

`data/group_whitelist.json` 单文件承载两块状态：`{"speech_whitelist": [group...], "bot_switch": {group: bool}}`（具体字段名实现时定，但两块 MUST 同文件）。写法沿用 profiles.json 的 tmp+rename 模式。理由：群主切换是低频动作，单文件足够；与既有 SaveProfiles 一致的原子写避免半文件。
替代：分两文件——多一次 IO 与两文件状态不一致风险，弃。

### D2 群主判定双路

`IsOwner(group, user)` = 画像 role==owner **或** WS 帧 member_role==owner。WS 帧判定必须由调用方（robot.go 解析 payload 时）传入 member_role，profile 包只存画像侧。理由：WS 帧是平台事实（权威），画像是学得事实（可离线积累），两者并集最稳。
替代：只认 WS 帧——重启冷启动时群主命令失效，弃。

### D3 相对频率算法（stats.go）

- per-user 滑窗 `[]time.Time`（cap 8，沿用）算尾随 24h 计数 u。
- per-group 中位数 M：群内所有"有发言的用户"的 24h 计数取中位数。实现：stats.go 维护 per-group per-user 计数 map，请求时 O(n) 收集+排序取中位数（群规模量级，微秒级）。
- 触发条件：u ≤ quiet_factor × M **且** M ≥ min_median **且** 观察满 24h **且** lifetime≥3。
- 死群停用：M < min_median(默认3) 时全群不触发。
- 自限：激活计入该用户发言数（机器人回复引发的用户跟言自然抬频）→ 自动退出，无需额外机制。
- 参数全部进 config：`quiet_factor`(默认 0.5)、`min_median`(默认 3)。
- 替代方案：全量落盘中位数——违反"统计留内存"约束，弃。

### D4 令牌桶（bucket.go）

沿用 chat_gate.go 既有语义：容量 2、每 4h 回填 1、per-group、惰性回填（读取时按 elapsed 补）。参数 `bucket_capacity`(2)、`bucket_refill_hours`(4) 进 config。
替代：固定日界计数——不平滑，弃（既有实现已选令牌桶）。

### D5 历史缓冲（history.go）——补欠账

per-group 环形缓冲：容量 20 条、TTL 10min（`history_cap`=20、`chat_window_min`=10 进 config）。激活时拼接注入 prompt，上限 1200 字符（`history_prompt_cap`=1200），从头保留最近消息、超长截断。
替代：全量历史——内存无界，弃。

### D6 触发矩阵（gate.go）窄接口

robot.go 侧只调一个入口，例如 `gate.Decide(group, user, kind) → (allow bool, reason string)`，kind ∈ {at, nick, lowfreq}。矩阵内部合并判定：白名单（全局空=allow-all）→ 群开关（bot_switch）→ kind 判断（at 恒过门；nick/lowfreq 受开关）→ lowfreq 再过相对频率+令牌桶。历史注入独立函数 `gate.HistoryPrompt(group)`。
替代：robot.go 直接调子包各文件函数——接口面大、调用方易漏判，弃。

### D7 工具发言的白名单约束

mute/profile 工具组的发言路径（react_agent 侧发送）在发送前过 `gate.Decide(group, _, kind=at|tool)` 或独立的 `gate.AllowSpeech(group)`。理由：LLM 工具产生的发言与机器人主动发言同一发送出口，收口在此处最小改动。
替代：逐工具内嵌判断——散点维护，弃。

### D8 profile 子包边界

store.go：注册表 + data/profiles.json 原子读写 + 画像查询（FindUserByNick/AddAlias/SetNote/Summary/IsOwnerOpenid）。tools.go：三工具迁入，ReactCtx 依赖保留。first_seen 持久化进 profiles.json（新增字段，向后兼容：旧文件无此字段则视为"已观察满 24h"——与现 LoadProfiles 的 FirstSeen=-48h 语义一致）。
替代：first_seen 单独文件——多一次 IO，弃。

### D9 审计日志

统一格式 `[gate-audit] ts=<RFC3339> group=<gid> actor=<oid> action=<toggle_whitelist_on/off | bot_switch_on/off> result=<ok|denied>`，走 log.Printf（与既有风格一致），不另建日志文件。
替代：独立审计文件——运维面增加，非目标，弃。

## Risks / Trade-offs

- [风险] 重构切换期间 robot.go 接线错误导致 @bot 失效 → 迁移顺序：先建子包+单测，再接线，最后删旧文件；每步 build.sh 验证。
- [风险] profiles.json 新增 first_seen 字段与旧文件兼容 → 旧记录缺字段时回退 -48h（等效"老面孔"），单测覆盖。
- [风险] 中位数随小群抖动（M 在 3 附近跳动）→ min_median 死群门槛 + 观察期 24h 双重缓冲；参数可调。
- [风险] 群主命令字符串误触发（普通聊天含"关闭机器人"）→ 命令需精确匹配（TrimSpace 后全等）+ 群主身份判定，非群主发送不产生副作用。
- [风险] 令牌桶与冷却并存语义混淆 → cooldown 是 @bot 主路径的节流（既有），令牌桶只管激活类；文档在 kb 参数表中写明分工。
- [权衡] 统计不落盘 → 重启后死群判定/中位数重积累，最多 24h 内触发保守（偏安全侧），可接受。

## Migration Plan

执行顺序（与调用方给定一致）：规格确认 → 子包骨架+单测 → 白名单+群主命令 → 触发矩阵接线 → 删旧 → build 全绿 → 重启实弹。回滚：git revert 整个 change；数据文件向后兼容（group_whitelist.json 删除即回 allow-all）。

## Open Questions

（无——参数默认值已在 D3-D5 定，均为 config 可调，不阻塞开工。）
