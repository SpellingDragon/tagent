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
