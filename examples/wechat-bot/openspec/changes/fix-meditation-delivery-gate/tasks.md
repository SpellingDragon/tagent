## 1. 根因取证两问（只读核查；2026-09-15 重大修正：看板回流假设已证伪，原"回流环节追踪"废止）

> 修订记录：原 1.1（回流管道追踪）被证伪性新证废止——看板为 BeforeModel 请求期注入（context_manager.go:406/:1221-1227，request-only、never projected/compressed），轨迹 3977 条 board 内容 0 命中（二次重查仍 0），不产生批内事件。原 1.2 执行中产出该证伪实证（重大修正已吸收进 design.md），但其核心问题——泄漏 turn 消费端实际读到的 trigger_source 值——**仍未核答**，并入新 1.1。

- [x] 1.1 钉死 cm.triggerSource 在泄漏 turn 的实值（两问之一）：提取泄漏①（03:20 task settle）与泄漏②（04:52 纯冥想批）turn 的轨迹 span 属性（tagent.turn.trigger_source）与生产日志（[Event] Debug 行 StateDelta[trigger_source]、RunFlow 盖章值），核对消费端实际读到的值：是 "meditation"（→泄漏在旁路通道）、是 ""（→空值兜底 main.go:375 成 user 投递）、还是其他；三次泄漏逐一核答，坐标与结论写入 root-cause.md
- [x] 1.2 盘点全部投递通道/switch 覆盖面（两问之二）：以 main.go:400-481 trigger_source switch 为基线，grep 全部能把输出送达用户的路径（:488+ non-final 按 role 分发、interim/typing、SendTextToUser/SendLongText/DeliverFiles/bot.Send 全部调用点），逐一归类"是否受 trigger_source 门禁约束"，产出通道清单（含每条通道的判定依据坐标）写入 root-cause.md；特别核答：冥想输出是否可能经 non-final 通道绕过 switch
- [ ] 1.3 盘点真正入批的系统注入（适用面圈定）：grep 全部 Source="user" 但非真实用户来源的**批内事件**注入点（看板已证实不入批、剔除），产出归类清单——哪些注入真实进入事件批并以 user source 冒充（应归 internal-observation 类），为 D2① 圈定适用面；若清单为空，D2①② 降级为防御性加固并在 root-cause.md 记录
- [x] 1.4 产出 root-cause.md 实证记录（写入本 change 目录）：已证伪链（看板回流假设，附轨迹 0 命中双重验证）+ 两问核答结论（每次泄漏的具体通道） + 真实根因链坐标 + "任务清闲时门禁正常"的对照证据 + 待修复面清单

## 2. 框架层修复（按 §1 取证结论定向；原 D2"回流环节补类别"已废止）

- [ ] 2.1 事件层独立类别（适用面按 1.3 结论）：为真正入批的系统注入建立 internal-observation 类别（事件 Source 增设类别值如 "internal_observation"，或 AgentEvent 增设类别字段——按 1.3 清单选最小侵入形态），语义约束：内部观察不冒充 user；若 1.3 清单为空则仅做防御性加固并记录
- [ ] 2.2 extractTriggerSource 按类别确定性导出（event_loop.go:215 起）：内部观察事件不参与优先级 1"真实 user 必胜"判定、不覆盖冥想/任务世系；优先级 1 收窄为"真实 user（非内部观察）必胜"
- [x] 2.3 投递门禁修复（方向按 1.1/1.2 取证结论选定，二选一或叠加）：(a) 若泄漏成因=空值兜底（冥想批导出 "" → main.go:375 兜底 user）→ 导出侧保证世系已知批次 trigger_source 非空 + 消费端空值策略 fail-open 改 fail-closed（空值不投递仅记日志）；(b) 若泄漏成因=non-final 旁路（绕过 trigger_source switch）→ 该通道补 trigger_source 门禁约束；(c) 若其他成因 → 据实定向；选定方向与理由记录进 root-cause.md 后动工
- [ ] 2.4 修复必须同时覆盖两类输入路径：纯冥想批次（meditation 注入事件被 idle agent 消费）与 meditation 世系 spawn 的 task settle（task_settled 回流 turn，世系经 Origin→Metadata→extractTriggerSource）——两类路径的输出均不投递给用户
- [ ] 2.5 部署修复：执行部署/重启序列（restart-tagent.sh），核验健康检查与版本串（healthz 窗口 180s）确认新二进制生效（含一次完整重启而非仅 hotswap 热重建）

## 3. 回归测试（三用例 + 按取证结论设定前置 + 既有测试不回归）

> 修订记录：原"批内构造看板注入"前置随看板假设证伪废止——看板不入事件批，非泄漏前置；新前置按 §1 取证结论设定。

- [ ] 3.1 新增回归用例一：纯冥想批次（仅 meditation 注入事件入批）→ agent 输出事件 trigger_source="meditation"，消费端不投递（仿 tests/async_result_routing_test.go 的 StateDelta 断言风格）；前置按 1.1 结论构造（如空值成因 → 构造 trigger_source 为空/缺失的冥想批验证 fail-closed）
- [ ] 3.2 新增回归用例二：meditation 世系 spawn 的 task settle 回流 turn → 输出 trigger_source 世系="meditation"，消费端不投递（覆盖 e2195fc 读侧分支 + 8bab6a4 写侧盖章 + 门禁修复的端到端串联）；同样按取证结论补前置
- [ ] 3.3 新增回归用例三（防过修）：普通用户消息 → trigger_source="user"，输出正常投递到 meta_chat_id 对应会话；含 lastActiveChat 回退路径不回归；若 2.3 走 fail-closed 方向，加子断言：user turn 的 trigger_source 为空时不被误伤（fail-closed 仅拦内部批，真实用户批导出非空）
- [ ] 3.4 全量跑既有测试不回归：meditation_test.go（e2195fc 六用例）、spawn_lineage_write_test.go（8bab6a4）、metadata_propagation_test.go、event/metadata_test.go、task/task_board_test.go（看板注入单测：注入位置/nil 安全断言，role 形态未动应零变化）、governance 相关 trigger_source 消费用例
- [ ] 3.5 `go build ./...` + `go vet` 通过（examples/ 目录一并在编译范围，防 a792b8c 类 root-module gate 漏编译）

## 4. 生产验证（双重证据）

- [ ] 4.1 部署修复后，观察下一次泄漏复现场景的冥想 turn（场景按 1.1/1.2 结论设定）：轨迹（turn span trigger_source 属性）与投递日志（[Agent][meditation] 冥想输出 vs [Agent][user→chat]）双重确认输出未投递给用户微信
- [ ] 4.2 复核生产日志无 "meditation 输出投给用户" 的告警/投诉复现；若 24h 内再发生，回滚并重开根因（root-cause.md 更新）

## 5. 协调、解耦项与收尾

- [ ] 5.1 与并行计划 wechat-bot-reincarnation-notice 协调实施顺序（建议先本计划后通报计划，或同 PR 分 commit），避免 main.go/注入链改动冲突
- [ ] 5.2 【解耦项，非本计划范围】看板 RoleUser 呈现层异议（内部观察冒充用户位）另立计划处理：立项时引用本 design.md"board 的 role 呈现问题（解耦处理）"段与本条，明确不改门禁、仅改呈现；此处仅作登记，不阻塞本计划任何步骤
- [ ] 5.3 归档前更新本 tasks 全勾 + LEDGER.md 登记，按 B 级流程 spec validate 通过
