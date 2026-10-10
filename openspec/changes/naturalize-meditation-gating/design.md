# Design: naturalize-meditation-gating

基线 dev `98efb8f`（含 startedAt 过渡补丁，本 change 落地时退役）。

## D1 状态机（pending 三态）
- `fire`：检查（novelty ✓ ∧ 节奏 ✓ ∧ !pending）→ `pending=true` → 注入；
- `consumed`（纯冥想批被 event_loop 消费）：`pending=false`，水位推进到**注入时刻**（覆盖让位窗口期事实）→ 自锁；
- `deferred`（混合批让位丢弃）：`pending=false`，水位**不动** → 下个 tick 重试（tick=interval 天然限频，空试成本=事件对象级）；
- 防卡死：pending 超 `3×interval` 未决自动清零（注入丢失的防御性复位，日志 WARN）。
- 通知机制：event_loop 的 `UpdateLastTurnEnd` 回调点改造为 `NoteMeditationBatchOutcome(consumed bool)`——跨模块回调点数量不变（1 个），语义从"任意回合"收窄为"冥想批结果"。
- 持久化：只持久化 `lastMeditation`（执行语义）；pending 不持久化——重启复位的最大代价是 fire-未执行窗口的重复注入一次，水位已推进则 novelty 自锁，幂等安全。

## D2 节奏门
`now - lastMeditation ≥ min_gap`；`lastMeditation==0`（首次或锚文件缺失）→ 直通。锚文件兼容：旧格式 `LastTurnEnd` 键 Load 忽略（既有忽略未知键机制）。重启恢复锚后正常间隔判定（重启即思的语义由"水位老于新事实"自然决定，无额外规则）。

## D3 让位语义（MODIFIED 混合批条款）
`dropMeditationFromMixedBatch` 行为不变（丢弃注入的事件），但管理器侧记账变化：让位=推迟（水位不动+pending 清零）。自然节律表述：用户高频期每 tick 一次空试（30m 级，可忽略）；用户离开后下个 tick 即思，**覆盖全程积累事实**（对比现状：干等 min_gap 且期间水位可能已被烧）。

## D4 两形态统一矩阵（验收剧本）
| 场景 | 预期 |
|---|---|
| 外部形态冷启动（远端 18h 案翻转） | 首 tick 直通→存量通读一次→自锁→新事实驱动 |
| 自察+用户高频 | 让位空试（tick 频率）；用户走后下 tick 即思，覆盖全程 |
| 家务流常驻（自察） | 家务不进锚、不点亮 novelty——彻底透明 |
| 冥想 turn 进行中 tick | pending 防重入 |
| 让位后用户又来 | 连续让位，水位始终不动——窗口无损 |
| 重启（锚恢复/锚缺失） | 恢复→正常节奏；缺失→直通一次 |
| pending 卡死防御 | 3×interval 复位 + WARN |

## D5 spec 与测试
- 主 spec idle-gating 三处条款变更（REMOVED/MODIFIED×2）+ADDED 执行语义条款，标题与主 spec 逐字对齐；
- 测试：既有 `TestMeditationGate_*` 旧语义矩阵（28 例）改写为新矩阵（按 D4 七场景展开），冷启动两用例（98efb8f 引入）保留语义并入新矩阵；`UpdateLastTurnEnd` 相关用例随符号删除清账。

## D6 观测
三态 INFO：`[Meditation] fired (pending)` / `executed — watermark advanced to <t>` / `deferred (batch yield) — retry next tick`；pending 超时 WARN。自然节律全链可观测。

## D7 部署知会
min_gap 等效间隔变短（不再被日常回合拉长）；远端 600s 摘上限与 #4 修复同批换装后，本 change 单独换装（行为面大）。
