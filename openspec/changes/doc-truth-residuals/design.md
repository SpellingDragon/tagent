# Design: doc-truth-residuals

## 决策

1. **残项不混批**：P0（缺陷）与 P1（转录待裁）各自独立小批次；每项动手前须单独裁决，不因"都在本 change 里"而默认放行。
2. **仪器先行（P0-1/P0-2）**：merge-check 的两处判据缺陷与 23 条 Test 类 map 缺口先修——它们是其余一切对账读数的可信度基础（本 change 立项本身就是两轮评审用该仪器验出来的）。
3. **行为类修复单独立法**：P0-4（静默 0 条）与 P0-5（countingSpawner）若裁出行为改动，须在该项下补 spec delta 与先红后绿测，不借本 change 的收口名义直接改。
4. **无 spec delta 的项**（P0-3/P0-6/P0-7/P1 全部）以 tasks 台账承载，验收即该项自述的判据；唯一例外是 P0-1 的仪器判据，落一条 ADDED Requirement 于 `code-documentation`。
5. **上游债标注**：P0-6（origin/dev 0a31e46 的 compress 105 重名）属上游 CI 缺口，本仓侧只加"测试可编译"门，不替上游修因。

## 风险

- 23 条 map 缺口的"补行 vs 换 D0 基线"若选错，会伪造历史口径——先在 tasks 里逐条列名再选路径。
- testsupport 形态改动（`_test.go` 化）会再次触发 merge-check 的 missing-helper 噪声，须与 P0-1 同批或在其后。
