# Proposal: doc-truth-residuals（文档真源残项收口）

## Why

`restrict-comments-to-godoc-and-index` 已归档（2026-09-30）。归档前两轮整体评审（该 change tasks.md §379）证实：注释面主张成立（702 文件差异中 150＋12 纯注释、测试合并无覆盖丢失），但留下**两类残项**：一类是评审确认"仍待用户裁决"的清单（该 change `pending-decisions.md` 的 A/B/C/Q 族等），一类是评审**新发现**的设计缺陷。它们不属于原 change 的范围，也不能随档沉睡——本 change 是它们的唯一 active 载体。

**立项当日复核（2026-09-30，对 HEAD=db4cf38）**：P0 七条经逐条核验后 **3 条改判关闭、1 条改名**——归档评审的结论是时点快照，转录为任务前必须核存在性，此教训已固化为本 change 的立项规程（见 tasks.md 顶部）。

## What Changes

纯收口。复核后的真实工作面：

### 经复核存实的项（按性价比排序）

1. **CI dev 分支覆盖**（原"测试可编译门"改名）：CI 已有 `go test` 编译步（测试包必编译），洞在触发分支只监听 main/PR——`0a31e46`（agent/compress 105 处重名、不可编译的测试面）正是从 dev 直推入库的实证。修复＝`.github/workflows/ci.yml` 触发分支加 `dev`，一行。
2. **merge-check 仪器判据**（`scripts/codetools/check.go`／`main.go`；**执行时精确化为"四处归一器吞字面量内容＋三处行尾注释槽未清"**，立项时只记了两处盲区）：① 凡把两侧文本抹平的归一器都不得伸进字面量——`foldLayout`（空白折叠）、`dropBlankLines`（空行删除与行尾裁剪）、`qualifierBlind`（别名致盲）三处同源缺陷，其中致盲一处已在本仓造成真实后果（任务 15）；② 注释剥离按槽位穷举：`TypeSpec.Comment`（定义型＋别名）与 `ImportSpec.Comment` 未清会把纯注释修改误报为正文变化；③ `--map`/`--explain` 单值 flag 的易错面与"逐包配对"正确形态写进用法自述（归档评审实际踩中）。等价对账是本项目真源机器，盲区该补。
3. **map/explain 换基线对账**：以 `db4cf38` 为基重新生成各包 map＋explain，使 merge-check 向前干净；不回填 vs origin/dev 的 23 条历史缺口（历史口径，另案定性无收益）。
4. **Q-4 #16 表格化**：`event-architecture.md` §4.1 的 64 行常量复述块——真实漂移面（`timed_out` 已实证"复述漏项即假陈述"因果链），改为承载同等命题的表格。
5. **tmux 抖动面（低优先）**：`tool/action` 真实 tmux 测的既有抖动（3+3 采样、轮换受害者）；CI `-short` 不触发，处置＝本地串行跑法写入文档。

### 复核后关闭的项（改判理由入档，见 tasks 9–12）

- **testsupport.go 形态**：编译面≠链接面（未导出＋无非测试引用＋链接期 DCE），取舍已有载于 wiki §八（D-27）。
- **未指定分区⇒静默 0 条**：安全默认态而非缺陷，wiki §13.1 已完整文档化；生产路径恒带 ownPartition。
- **countingSpawner 泄漏屏障**：已在 HEAD 修复并有钉测（evidence.md §D1 为评审时点快照）。
- **Q-6 419 处无索引长 doc 普查**：生产 go doc 即层一真源、无须索引；"带索引却复述"的一半已由 §377 全量对读清完。

### P1 转录待裁项（维持）

6. **Q-2 剩余**：`agent/helpers.go:151` 换锚；`tagent.go:243` 十条索引堆挂逐条核实。**执行结果改写了这条的定性**：换锚成立（现锚 `#delivery` 与命题零重叠；且归档候选锚漏标篇名——`reincarnation-notice.md` 并无 `#subagent-loop`，真锚在 `agent-architecture.md:448`，照抄即造出新错指）；而"**索引堆挂**"这一改判**也不成立**——十条指针逐条对实据全是 `New` 真实接线的承载节，真缺陷是那份 7 条 bullet 清单没写出这些动作，使指针无从追认（已按纯注释面补全 6 条动作，机制理由仍留 wiki）。⇒ 这是 design 决策 6/7 的又一次兑现：**连"上一轮已改判过"的结论（堆挂）也须重新核**，改判本身不是终态。
7. **A5**：英文裸坐标（`D5` 类）是否纳入机器词表。
8. **棘轮 11 报项**（`{audit 1, resp 10}`）与准出四项（§309.6／C9／14.5／9.4(a)）。
9. **主 specs 吸收核对**（2026-09-30 归档三件 resident 系 change 时新发现，见 tasks 13）：`async-task-lifetime` 能力在主 specs 整体缺位（且 `async-task-execution` 仍留被取代的"任务超龄治理"旧语义）；「未确认恢复材料的有限保留租约」未入 `event-segment-store`。补立以现行码面＋wiki 为准，不照搬旧 delta 原文。**任务 13 执行时把这条的规模核清了**：原句"delta 大部分已被 09-27 主 spec 吸收/取代"是未核的乐观归因——三件 resident 归档至今未跟踪、相关主 spec 最后改于 09-10／09-27，故**从未吸收**；仅其中一份 delta 的完整债务面为"1 能力整体缺位 ＋ 5 能力 6 条 ADDED 缺位 ＋ 3 条 REMOVED 未撤 ＋ 14 条 MODIFIED 正文漂移"。本批已落本项所记两块（`async-task-lifetime` 全部 7 条 ＋ 租约需求），余者见项 14。

### 执行任务 2–5 期间新暴露（2026-09-30／10-01，见 tasks 14–17）

10. **测试固件被改名波及的真实缺陷**：`tests/integration_test.go` 4 处串写成 `"... entry tagentagent."`（`03a0cc3` 把标识符改名施加进字面量内部）。旧仪器因别名致盲而长期失明，收口该盲区后立刻暴露——属"该缺陷在本仓有实证后果"，不是理论风险。无断言引用，故语义惰性；修不修、在哪一批修，待裁。
11. **门的固有盲区两条**：`--map`/`--explain` 重复传入静默、`examples/wechat-bot` 的 gitignore 测试文件在任何基线都恒报 `extra-*`（该包门读数永久不可用作准出凭据）。
12. **"完整列表"式假计数不止一处**：任务 4 执行中实测同一事实有三个互相矛盾的数（16／14／11），而码面真值是 23——文档面的计数断言与枚举断言同属漂移源，本批已把这三处改为归属指针。
13. **文档面的变更名引用无门**：`external-coord-ref` 只扫码面注释；全 wiki 实测 3 行引用变更名，其中 1 行指向**仍 active 的变更**（归档即悬空）。加扫 `docs/wiki` 与否待裁。

### 执行任务 13 期间新暴露（2026-10-01，见 tasks 18–19）

14. **主 specs 的吸收债远大于本项所记两块**：仅 `complete-resident-reliability-protocol` 一份 delta 就还有 6 条 ADDED 缺位（分布 5 个能力）、3 条 REMOVED 未撤（主 spec 仍在承诺已移除的行为）、14 条 MODIFIED 正文与 delta 不一致（须逐条分辨"后续 change 更新"与"从未吸收"），另两份未跟踪归档尚未审计。撤除与覆盖都属语义变更，必须逐条对码面核实后才动，故登记为 tasks 18 而非顺手批量改写。
15. **码面注释仍在描述被消除清单守为"不得复活"的旧年龄墙**：`agent/task/task_manager.go:73/83/170/1104` 四处注释述及 stale 观测态与 job deadline 墙。消除清单 `TestEliminationList_ZeroLegacySymbols` 明文只扫**非注释行**，因此这类假陈述落在门的射程之外——是"门的覆盖面对象选错"（判据现成：被删机制的任何表述都该受管），不是漏写一处。同族幻影坐标另见 `guardrails_test.go` why 串与 `memory/lifecycle.go:107` 的**运行日志**串 `§2.8`。待裁，登记为 tasks 19。

## Impact

- 受影响面：`.github/workflows/ci.yml`（项 1）、`scripts/codetools`＋`scripts/check_test_merge.sh`（项 2-3）、`docs/wiki/event`（项 4）、贡献者文档（项 5）、注释门基线（项 8）、主 specs（项 9：新增 `openspec/specs/async-task-lifetime/spec.md`，改写 `async-task-execution` 超龄条款，`event-segment-store` 补租约需求）。
- 全部为独立小批次可做项；项 1-3 建议一批做完（仪器与防线先立，后续对账才可信）。若未来裁出行为修复（如收尾轮），另立 change 单独立法＋先红后绿。
- **仪器严格化的追溯性已实测**：任务 2 的新旧 A/B 复跑覆盖归档五个批次，**新增报项 0** ⇒ 严格化不推翻任何已声明的"纯注释"读数；唯一变化是一处历史误诊被纠正（tasks 2）。
