# Proposal: doc-truth-residuals（文档真源残项收口）

## Why

`restrict-comments-to-godoc-and-index` 已归档（2026-09-30）。归档前两轮整体评审（该 change tasks.md §379）证实：注释面主张成立（702 文件差异中 150＋12 纯注释、测试合并无覆盖丢失），但留下**两类残项**：一类是评审确认"仍待用户裁决"的清单（该 change `pending-decisions.md` 的 A/B/C/Q 族等），一类是评审**新发现**的设计缺陷。它们不属于原 change 的范围，也不能随档沉睡——本 change 是它们的唯一 active 载体。

## What Changes

纯收口：不改生产行为（除两处已定位的小缺陷修复需单独裁决）。按优先级：

### P0 归档评审新发现（缺陷类，有证据坐标）

1. **merge-check 仪器缺陷 ×2**（`scripts/codetools/check.go`）：① `foldLayout`（:253）折叠含字符串字面量在内的全部空白 ⇒ 对反引号原始字符串的内容变化失明；② 类型声明行尾注释不剥离 ⇒ `// milliseconds` 类删除被误报 body-changed。另有 `--map`/`--explain` 均为单值 flag 的易错面（本次评审实际踩中）。
2. **explain 台账 23 条 Test 类缺口**：explain 只豁免非 Test 声明（`check.go:222-226`），Test 类 body-changed 的处置路径只有补 map 行／换 D0 基线口径／另案定性——需逐条裁决。
3. **`agent/testsupport.go` 形态**：142 行 mock 进生产编译面（D-27 为防测试导入环的有载决策，但 `_test.go` 化或 export_test 拆分才是正形；内含双名残留 `recordableMockModel mockModel` 等）。
4. **运行期"未指定分区⇒QueryEvents 静默 0 条"**（C5 暴露：wiki §13.1 假伪代码已纠正，但静默语义本身未处置——至少加日志或文档前置条件）。
5. **`settle_routing.go:218-224` countingSpawner 泄漏屏障缺陷**（evidence.md §D1 有载：Deduped/Blocked 两形态永无 settle 路由，至今无裁决）。
6. **上游 CI 缺口**：origin/dev 的 0a31e46（attention-budget）带着 `agent/compress` 105 处 `redeclared` 不可编译测试面入库——本仓缺"测试可编译"门。
7. **`tool/action` 真实 tmux 测试抖动面**：与 HEAD 对照 3+3 采样证实为既有抖动（轮换受害者、`server exited unexpectedly`），需隔离或标 flaky。

### P1 原 change 转录的待裁项（见其 pending-decisions §7 Q-1..Q-6 与 A/B/C 表）

8. **Q-4 #16**：`event-architecture.md` §4.1 的 64 行常量复述块——(a) 表格化（推荐）/(b) 码面补 15 行薄契约/(c) 维持。
9. **Q-6**：419 处"无索引生产码长 doc"是否属 9.7 双重真源治理（或给筛选口径缩小）。
10. **Q-2 剩余**：`agent/helpers.go:151` 换锚；`tagent.go:243` 十条索引堆挂的逐条核实。
11. **A5**：英文裸坐标（`D5` 类）是否纳入机器词表。
12. **棘轮 11 报项**（`{audit 1, resp 10}`）与准出四项（§309.6／C9／14.5／9.4(a)）。

## Impact

- 受影响面：`scripts/codetools`（P0-1）、`agent/`（P0-3/5）、`memory`（P0-4）、`docs/wiki/event`（P1-8）、注释门基线（P1-12）。
- 全部为独立小批次可做项；本 change 不设 spec delta（不新增需求，只收残）——若 P0-4/P0-5 裁出行为修复，再在其下立子批。
