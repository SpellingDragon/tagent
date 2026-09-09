## 1. 规格确认

- [x] 1.1 通读 specs 四份 + design.md，核对既有代码（whitelist.go 两态语义、chat_gate.go 全量、robot.go 四接线点、entity/channel_config.go 字段），确认无理解偏差后在 design.md 补充核对结论
- [x] 1.2 在 entity/channel_config.go 新增可调参数：quiet_factor(0.5) / min_median(3) / history_cap(20) / chat_window_min(10) / history_prompt_cap(1200) / bucket_capacity(2) / bucket_refill_hours(4)，yaml 标签命名与既有风格一致，默认值经 cfgInt 兜底
  - 偏差（已接受）：仅 owner_trigger/active_window_sec/cooldown_sec 进 yaml + cfgInt 兜底；quiet_factor/min_median/history_cap/chat_window_min/history_prompt_cap 为 gate 包内常量（stats.go/bucket.go/history.go），未进 config——偏离 D3/D5"参数全部进 config"。如需调整须改代码重编译。

## 2. 子包骨架 + 表驱动单测

- [x] 2.1 新建 service/gate/ 目录与四个文件骨架：gate.go（触发矩阵窄接口 Decide/HistoryPrompt）、stats.go（滑窗+中位数）、bucket.go（令牌桶）、history.go（环形缓冲）；建 service/profile/ 目录：store.go（注册表+profiles.json 原子读写）、tools.go（三工具迁入占位）
  - 偏差（已接受）：gate 包实为 gating.go（触发矩阵，OwnerTriggered/LLMAllowed 双入口）+ whitelist.go（DB 双轨白名单）+ stats/bucket/history；profile 包单文件 profile.go（注册表+落盘合一），无 store.go/tools.go 拆分，三工具未迁入（见 4.3 遗留）。
- [x] 2.2 stats.go 表驱动单测：尾随 24h 计数、群中位数（奇偶/空群/死群 M<3）、相对频率判定四条件组合（u≤0.5M / M≥3 / 观察 24h / lifetime≥3）、激活自增后自然退出
  - 实证：gate_test.go TestRecordAndCount24h / TestMedian24h / TestLowFreqFourConditions / TestLowFreqSelfLimit（4 用例）；报账附 `go test ok ... 21.9s`。**注意：M<3 死群拒绝仅经 TestLowFreqFourConditions 隐式覆盖（观察期先拒），未见显式死群用例**——已并入 2.2 勾选但记此覆盖缺口。
- [x] 2.3 bucket.go 表驱动单测：初始 2 令牌、连发耗尽拒绝、4h 回填 1、per-group 隔离
  - 偏差（记档）：TestTokenBucket 覆盖容量 2/耗尽/4h 回填 1/回填后单令牌；**per-group 隔离用例缺失**。
- [x] 2.4 history.go 表驱动单测：容量 20 拼挤、TTL 10min 过期、拼接截断 1200 字符
  - 偏差（记档）：TestHistoryCapAndSnapshot 用 cap=3 模拟挤出（覆盖挤出语义）+ TestHistoryCharLimit 截断；**TTL 10min 过期用例缺失**（HistoryWindow 参数无用例驱动）。
- [ ] 2.5 profile/store.go 单测：profiles.json 读写往返、first_seen 旧文件缺字段回退 -48h、别名去重上限 10
  - 自报遗留（与实证一致：profile 包无任何测试文件）。

## 3. 品牌清单 + 群主命令

- [x] 3.1 实现群级发言白名单：data/group_whitelist.json 数据模型（speech_whitelist + bot_switch 同文件）、tmp+rename 厨子写、空=allow-all+启动警告（沿用两态语义）、加载恢复
  - 偏差（用户指示，已接受）：白名单改走 **DB 双轨**（MySQL chat_white_list_guild 动态轨 + config SuperGuild/SuperGroup 种子轨），whitelist.go 实现 LoadWhitelist/ChatWhitelisted/EnableGuild/DisableGuild，main() L93 接线失败降级种子。未实现 group_whitelist.json/bot_switch 同文件模型（bot_switch 部分→遗留）。
- [x] 3.2 实现群主身份双路判定（画像 role==owner 或 WS 帧 member_role==owner）并单测两路命中与双未命中
  - 偏差（记档）：实现侧双路就绪（profile.IsOwnerOpenid 画像路 + robot.go 每帧 RecordUserProfile(…, member_role) 持续写入画像）；**单测缺失**（两路命中/双未命中用例未写）。
- [ ] 3.3 实现群主命令"开启白名单/关闭白名单"：精确匹配+身份判定+审计日志（[gate-audit] 统一格式）+持久化
  - **报账不实**：全工程递归 grep `开启白名单|关闭白名单|关闭机器人|gate-audit` 零命中；EnableGuild/DisableGuild 在 whitelist.go 有定义但**无任何调用点**。命令层未实现。
- [ ] 3.4 实现群主命令"关闭机器人/开启机器人"：bot_switch 切换、低频/昵称停用而 @bot 保持、审计+持久化，并单测开关语义
  - **报账不实**：bot_switch 零实现，命令层零实现。

## 4. 触发矩阵接线

- [x] 4.1 gate.go 触发矩阵实现：白名单→群开关→kind(at/nick/lowfreq) 分派→lowfreq 过相对频率+令牌桶；Decide 返回 (bool, reason)
  - 偏差（已接受）：实现为 gating.go OwnerTriggered（四路触发）+ LLMAllowed（冷却+令牌桶），robot.go 分别调用；未实现 D6 窄接口 Decide(kind)；群开关层因 bot_switch 缺失而缺席。触发矩阵的四路触发+白名单+令牌桶链路已接线（robot.go L244-262）。
- [x] 4.2 robot.go 接线切换：L237 RecordUserProfile → profile 包；L246 OwnerTriggered → gate.Decide(kind=nick/lowfreq 合并判定)；L586 LLMAllowed → gate.Decide(kind=lowfreq) 或独立激活放行；移除对旧包函数依赖
  - 实证：L243 profile.RecordUserProfile（已切 profile 包）✓；L250/L278 gate.OwnerTriggered ✓；L617 gate.LLMAllowed ✓；旧包依赖已移除（chat_gate.go 退役）。
- [ ] 4.3 react_agent/mute/profile 工具发言收口：发送出口统一过 gate.AllowSpeech(group)（或 Decide kind=tool），非白名单群拒绝
  - **报账不实**：AllowSpeech/Decide(kind=tool) 不存在，工具发送出口无白名单收口。
- [ ] 4.4 history.go 接入：群消息进环形缓冲，激活时 HistoryPrompt(group) 注入 prompt（上限 1200 字符）
  - **报账不实**：history.go 环形缓冲+Snapshot 实现完整且有单测，但 robot.go/react 链路**零调用**——实现了库，未接线。

## 5. 删旧 + build + 实弹

- [x] 5.1 删除 service/chat_gate.go，全工程 grep 无 OwnerTriggered/LLMAllowed/**旧包**符号残留引用
  - 实证：chat_gate*.go 零文件 ✓；ParseGroupAttachments 独立至 service/group_media.go ✓；robot.go 中 gate.OwnerTriggered/LLMAllowed 均为 gate 新包函数（非残留）。
- [x] 5.2 bash build.sh（go1.18 冻结链）全绿：编译+既有测试（含 whitelist_ext_test.go）+新增单测全过
  - 实证：bin/qqrobot 存在（45437144 字节，与报账一致）；报账附 build/vet/test 输出。
- [ ] 5.3 重启机器人，实弹验证：非白名单群拒发、白名单切换命令生效、相对频率触发/自限退出、开关命令语义、审计日志格式、白名单持久化跨重启
  - 部分：pid 3820297 / healthz {"status":"ok"} / WS READY session 7fd03c48 为运行时断言，读工具不可核；实弹矩阵待用户群内验收（三步实弹清单已发）。
- [ ] 5.4 kb 归档：设计决策（D1-D9 摘要）与参数表（全部 config 参数+默认值+语义）写入知识库
  - 部分：kb/ops/pitfalls/2026-发现9-09-sdk-upgrade-pitfalls.md 存在（SDK 战场另案）；D1-D9 汇总与参数表缺失。

## 6. 收尾

- [ ] 6.1 openspec validate（strict）通过；向调用方报账，请求归档
  - 前置：3.3/3.4/4.3/4.4/2.5/5.3/5.4 完成后方可进入收尾。
