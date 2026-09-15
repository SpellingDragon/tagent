# Root Cause: 冥想输出投递泄漏（fix-meditation-delivery-gate §1.4）

## 泄漏时间线（用户可感知）
- 03:20 前后 task settle 唤醒的冥想续回合输出送达用户
- 04:52 场次（batch 9 纯冥想批）后续回合总结送达用户
- 共同特征：**冥想回合内有后台任务在跑**（挂死探针/后台 knowledge），任务清闲场次无泄漏报告

## 已证机制链（全部带坐标）
1. 投递门唯一依据 = `StateDelta[MetaKeyTriggerSource]`（main.go:374 → ParseEventMeta，event/metadata.go:131）
2. 该键**全仓唯一写点** = cm.RunFlow 转发块 `evt.StateDelta[MetaKeyTriggerSource] = cm.triggerSource`，**且仅当非空**（context_manager.go:1197）
3. main.go 分发 `case "meditation"` 扣留、`case "user","task"` 投递、**default 兜底 user 投递**（main.go:407-417，含 lastActiveChat fallback——7293090 引入）
4. 第二条转发通道并存：SessionHook `original.IsUserMessage()` → cloneEventForDelivery → outputCh（agent.go:407-434）——该通道**无 trigger_source 盖章步骤**
5. 读侧（extractTriggerSource 世系分支，e2195fc）与写侧（spawn 装载世系，8bab6a4）修复后仍泄漏 → **泄漏事件不经过盖章点，或续回合 cm.triggerSource 为空**，二者必居其一

## 断点判定（二候选，均可被同一修复覆盖）
- C1：续回合（task settle 唤醒）的 cm.triggerSource 为空 → cm:1197 不盖章 → 无章事件走 default 兜底
- C2：部分输出经 SessionHook 通道（agent.go:424）旁路，天然无章
- 待补实值：泄漏 turn 的投递分支日志行（默认日志级 Debug 被回显污染未取到，不阻塞）

## D2 修法定向：fail-closed 兜底（结构性，双候选通杀）
main.go default 分支：无章事件**不再兜底投递**，改为「仅当存在世系 chat_id（task 世系）才投递 + 否则 WARN 落日志」。保障：
- 真实用户回合恒有 user 章（extractTriggerSource 对 user Source 必判 user）→ 不受影响
- 转世首条输出（7293090 诉求）走 task 章 + 世系 chat_id → fallback 保留
- 冥想/未知内部事件 → 静默扣留，泄漏路径结构性关闭

## 审计核数（plan agent 独立复核，2026-09-15）
§1 勾选前独立读回代码与本文档逐条核对，结论：

1. **机制链实核通过**：main.go:374（ParseEventMeta 取 trigger_source）、main.go:377-379（`if triggerSource == ""` 强改 "user"——**fail-open 真正位置**）、main.go:407-481（switch 分发：meditation 扣留 / user,task 投递含 lastActiveChat 回退 / default 仅 WARN 不投递）均与本文档一致。
2. **坐标偏差修正**：原文"该键全仓唯一写点 = cm.RunFlow 转发块（context_manager.go:1197）"有误。实况：MetaKeyTriggerSource 全仓共 **4 个写点**——cm:957（bus 回流系统注入 fullEvent.Metadata）、cm:1080（Attr 构造，非 StateDelta）、cm:1140（task spawner Origin，8bab6a4 写侧）、cm:1166（RunFlow 转发块 StateDelta 盖章，`:1165-1167` 仅当非空才盖）。:1197 实为 cloneEventForDelivery。**机制实质不变**：RunFlow 转发块确属"仅当非空才盖"，C1 候选成立；但"唯一写点"表述应更正，§2 修复须覆盖 cm:957（bus 回流系统注入盖的是 Metadata 而非 StateDelta，读侧未同步盖）——否则修复后若系统注入经 bus 回流走 SessionHook，仍无章。
3. **用词校准**：原第 3 点"default 兜底 user 投递"表述不准：default 分支实际**不投递**（仅 WARN）；兜底发生在 main.go:377-379 的空值→"user" 强改。D2 fail-closed 的直接靶点是 :377-379，而非 default 分支。
4. **C2 存疑点（§2 前宜核）**：agent.go:423-434 SessionHook 旁路触发条件为 `original.IsUserMessage()`，仅转发**用户消息事件**；LLM/tool 事件走 RunFlow eventCh。冥想输出（LLM final）能否以 IsUserMessage 形态出现未经实证——若不能，C2 通道实际不可达，修复重心应全压 C1（空值强改 fail-open）。建议 §2 动工前用一次生产 trace 或单测实证 IsUserMessage 与冥想 final 的交集是否为空。

> 勾选依据：1.1/1.2/1.4 报账证据（root-cause.md 落盘 + commit 1ad2945）与上述实核一致；坐标偏差不改变机制结论，修正意见已并入本文档。待补实值（泄漏 turn 投递分支日志行，Debug 级被回显污染）不阻塞 §2，与报账口径一致。
