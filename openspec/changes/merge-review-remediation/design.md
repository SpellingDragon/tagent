## Context

dev 领先 origin/main 43 提交（fast-forward 关系）。合入前的分主题并行评审（7 个主题并行审查 + 10 项高危发现人工复核，全部属实、零误报；证据存 `.git/review-notes/01-findings.md`）发现 2 P0（主规格与实现相反）+ 14 P1 + 29 P2。本变更承载 P0 全部、P1 全部与两处一行级 P2 的修订；其余 P2 登记不修（见 Non-Goals）。本地 `go build`/`go vet` 在 dev HEAD 已通过，修订在此基础上进行。

## Goals / Non-Goals

**Goals:**
- 消除全部 P0：两处主 spec 与实现相反的条款按已裁决行为对齐（b871d30 裁决、归档 delta REMOVED）。
- 消除全部 P1：数据竞争（Spec.Origin 并发写）、静默成功（execution gate 吞错）、资源/租约泄漏（liveCM、spill 释放时序）、契约违反（批量折叠 TelemActive）、门禁绕过（comment-only wrapper）、文档失真（README/wiki/归档路径）。
- 修订后 dev 可作为 MR 源分支推送并创建指向 origin/main 的 PR。

**Non-Goals:**
- 29 项 P2 中除两处一行级（`agent/recovery.go` 死残留、`lint.sh` 成功打印位置）外全部不修：涉及性能重构（KV 快照写放大）、运维脚本时序调整（restart-tagent.sh 归档窗口）、注释面清理等，避免 MR 膨胀；清单已在 review-notes 留档。
- 不改变任何对外 API/序列化/协议；不做与评审发现无关的重构。
- 不替 CI 新增门禁类型（comment_policy 棘轮等既有门禁回归通过即可）。

## Decisions

**D1 退役谱系改信号级传递（修 C-P1-1/C-P1-2）**
`SettleSignal` 增加谱系承载（如 `Lineage` 字段），`finalizeRetired` 设置信号级 `task-retired` 戳并删除对 `t.Spec.Origin` 的原地改写；`newTaskSettledEvent` 与回合谱系提取优先消费信号级值。一次性消除并发 map 写（读方 settle_routing/event_bus 无锁）与 Resume 恢复轮谱系污染（Origin 不再被改写，Resume 后真实结果按原谱系外投，符合 spec 既有"退役信号不继承原谱系"条款）。备选"退役前留 Origin 副本、Resume 时恢复"被否决：治标、留竞争窗口、多一份状态要维护。

**D2 OnBatchRetire 补 per-invocation 路由（修 C-P1-3）**
批量退役回调中，对每条 `BatchRetired` 先查 `taskInvocationID` 在 settle sink 注册表的绑定：有绑定则构建 per-task 事件走既有 `deliverTaskSettled` 路径（递减记账、唤醒父循环），仅无绑定条目折叠进单条汇总事件。备选"全部 voidSpawn"被否决：父循环失去结果可见性；备选"放弃折叠退回逐条通知"被否决：重新制造结算风暴（c14cdbc 要解决的问题）。

**D3 execution gate 失败显式呈现（修 A-P1-2）**
`GenerateContentIter` 内三个失败点（verify 拒绝、迭代器创建 error、通道创建 error/nil）一律 `yield` 携带 `Response.Error` 的失败响应后返回，与同库 `modelCallBudgetIterModel` 的处理同构、符合上游框架"流内错误编码进 Response.Error"契约；回合因此正确归约 turnFailed 并形成 failed completion。备选"向上抛 error"不可行（迭代器闭包签名既定）；备选"保持现状+日志"被否决：日志不改变归约结果，静默成功仍发生。

**D4 批量折叠豁免 TelemActive（修 B-P1-1）**
`foldSettleRuns` 在折叠 run 前按 dispositions 从 run 尾部剔除连续 Active 成员（尾部 Active 保持原样 external_input ref）；剔除后前缀不足 2 条则整段不折。选择"尾部剔除"而非"整 run 不折"：批结算常见形态是"已消费在前、未消费在后"，尾部剔除最大化折叠收益且严格守住"未消费不可丢"；若 run 全 Active 则整体不折。补钉住测试：批量 Active 不折叠、不截断。

**D5 spill 租约释放绑定落盘移除（修 E-P1-1）**
`ReplayWithNotify` 重放循环只记录本轮成功 key 集合，`rewrite` 成功后才逐个 `ReleaseKey`；rewrite 失败不释放任何 key（下轮 AlreadyCommitted 重放会重新走到释放路径）。`RetentionLease.Release` 的 Godoc 修正为按持有者释放语义（移除"幂等"错误宣称）；不改变 Release 实现（改持有者句柄会波及全部调用方，超出本次范围）。

**D6 spec 对齐方向与措辞（修 G-P0-1/G-P0-2）**
两处 P0 均为"spec 落后于已裁决实现"，修 spec 而非回退实现：`event-sourced-projection` 无锚条款按 b871d30 裁决重写为全量复原；`event-segment-store` 的 REMOVED 条款 Reason/Migration 措辞沿用归档 delta（2026-09-30-complete-resident-reliability-protocol）原文，避免两处表述漂移，并 ADDED 一条"Sync=原子快照屏障"真契约承接仍存活的提交屏障语义。

**D7 comment-only wrapper 放行删除侧（修 F-P1-1）**
wrapper 停止用双侧存在性预过滤：基线存在而工作区缺失的文件直接传入 codetools 触发 MISSING-HEAD 硬拒；仅当批次内全部为纯新增（无基线）时才 exit 0 放行。硬拒逻辑留在工具层单点，wrapper 不重复实现；顺带修未引号循环。

**D8 MR 形态**
修订以独立提交序列落在 dev（每主题一个提交，可单独 revert），push origin dev 后创建 dev→main 的 PR；PR 描述附评审摘要与本 change 链接。不建额外修复分支——dev 本就是待合并集，分支再分叉徒增合并摩擦。

## Risks / Trade-offs

- [OnBatchRetire 行为变化：有绑定条目不再进汇总事件] → 现有汇总条数断言测试同步更新；新增路由递减与父循环唤醒的钉住测试。
- [D1 触及 newTaskSettledEvent/event_bus 落链多处，信号级字段遗漏消费点会致谱系判定回退] → 全链 grep `MetaKeyTriggerSource` 消费点逐一核对；telemetry dispositions 与投递门既有测试回归。
- [spec MODIFIED 块整块复制，复制不全会丢条款] → 已按主 spec 原文整块复制后修改；openspec validate + 归档演练把关。
- [修订提交需过全部既有门禁（comment_policy 棘轮、merge-check、openspec 门、race 族）] → 每主题提交后本地跑 `scripts/lint.sh` 与相关包测试；race 族改动点（D1/D2）补 race 构建验证。
- [评审结论均为静态审查+人工复核，未经运行动态验证] → 每项修复配回归测试（fail-before/pass-after 可行处），不可行处（如 CI wrapper）以构造性输入用例证明。

## Migration Plan

无数据/配置迁移。回滚粒度为单个提交（每主题一提交）；spec 修订可独立 revert 不影响代码回滚。PR 合并即完成 main 收口，dev 与 main 重新对齐。
