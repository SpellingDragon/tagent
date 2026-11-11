# Design: 01-residual-cleanup

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：本文旧版「分类裁定」据伪前提「user_ruling 泛义保留」（执行期 agent 决定被误标为用户裁定）主张「泛义 carrier 不动（02 基线已豁免）」、仅检查点义退役，并以 02.3 基线分类为清理范围权威。用户明确「我没有裁定过泛义保留」。甲＝carrier 全退役（每处按义类改既有精确实名，见上级 design D6 映射）＋纯短语禁门（无基线/分类/豁免/棘轮）＋删 2377 行基线。旧检查点义改名经复核正确保留；task_checkpoint_test.go:174 旧「回退泛义」被甲取代→「durable commit」。权威映射见上级 design.md D6。

决策权威见一级 `design.md` D2/D4/D6；验收锚冻结于一级 `tasks.md`。本域只落清理细节。

## 清理范围（carrier 全退役，按义类改既有精确实名）

- **carrier 全退役（甲）**：carrier 系多义旧词，每处改其**精确既有规范词**，不造新概念（上级 design D6 映射为派发权威）：
  - **检查点义→checkpoint/cut**（旧已改，保留）：journal/restore/memory:418/invoke/checkpoint/segment_store 的「框架检查点事实/派生态载体」carrier。
  - **泛义→既有精确实名**（甲增量，全退役）：提交承载→commit／DurableCommit（含 commitCarrier→commitLedger、newCarrier→newDurableCommit、lifecycle.go carrier 字段→commit）；凭据→credential；URL→URL；查询载荷→projection／payload；信封→envelope／representation；事实/记录→fact／record／ledger；账户→account；首请求→request；任务层→task layer；判据→criterion。task_checkpoint_test.go:174「durable carrier」→「durable commit」（**旧 Issue3 回退被甲取代**）。
  - 动词 carries/carried/carrying 不含 carrier 子串，门自然放行，不在退役面。
- **folding generation→band promotion**（旧已改，保留）：仅 age_segments.go:7-8 跨行短语「by its folding generation」→「by its band promotion」（其余 band 散文已正确）。gen: 锚/instance_id/recorder generation 合法世代不触禁式，不动。
- **测试名引用更正**（旧已改，保留）：journal_test.go:522 引用已不存在的 TestTheCheckpointCarrierStaysBoundedByItsEntries→真名 TestTheCheckpointStaysBoundedByItsEntries（assembly/checkpoint_test.go:860 实证）。
- **检查点义测试标识符/夹具改名**（旧已改，保留）：carrierFrameValueBytes→checkpointFrameValueBytes；局部 carrier→checkpoint；TestJournalCarriesTheFrameworkCarrierFrame→TestJournalCarriesTheFrameworkCheckpointFrame；segment_store_test.go carrier-old/carrier-new/carrier 夹具→checkpoint-*（均 .WithChain(event.TypeCheckpoint,…)）。改名前后 go test -list 计数不变、断言数不降。

## 冻结归档勘误格式（只追加，不改写原文）

两处：①13-bounded-compression/tasks.md:16（引用 TestJournalCarriesTheFrameworkCarrierFrame）②session-checkpoint-material-unification/tasks.md:34（引用已改名 TestTheCheckpointCarrierStaysBoundedByItsEntries）。在原行**下方追加**勘误行：`> 勘误（code-vocabulary-alignment）：原引用测试名已改为 <新真名>（<commit>）；原文冻结不改写。` git diff 须仅新增行，原文逐字不变。

## 风险与回退

- 误改义类：carrier 全退役，每处按上级 design D6 映射改既有精确实名；映射未覆盖的歧义义类上报编排者裁，不自行豁免任何命中换绿（甲门纯短语禁，无基线/分类/豁免）。
- 改名漏引用：旧名残留 grep 全仓 0（冻结归档勘误除外）作负锚。
- 回退：纯注释/标识符改名，git revert 即恢复，无逻辑副作用。