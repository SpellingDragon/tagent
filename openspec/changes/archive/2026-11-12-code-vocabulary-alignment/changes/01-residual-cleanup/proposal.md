# Proposal: 01-residual-cleanup

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：本文旧版据伪前提「user_ruling 泛义保留」（执行期 agent 决定被误标为用户裁定）主张仅检查点义 carrier 退役、泛义经 02 基线豁免保留（「泛义 carrier 不动」）。用户明确「我没有裁定过泛义保留」。甲＝carrier 全退役（每处改既有精确实名 commit/DurableCommit/credential/URL/projection/envelope/fact/record，不造新概念）＋纯短语禁门（无基线/分类/棘轮/豁免）＋删 2377 行基线，净负代码。旧检查点义改名（journal/restore/memory:418/invoke/checkpoint/segment_store）经复核正确保留。权威映射见上级 design.md D6。

## Why

纯化已统一架构脊柱与文档词汇，但代码侧残留**多义旧词 carrier（全义）**——事实载体／提交承载／凭据／URL／查询载荷／检查点诸义混用同一词，与 GLOSSARY 行 3「描述机制一律用代码实名」脱节；另有跨行 folding generation、错误测试名引用、测试夹具旧词。本域清存量使 02 词汇门转绿（症状锚红转绿的「绿」侧）：carrier 全退役后无合法子串命中，门归 0。

## What Changes

carrier **全退役**（A 轨生产 .go＋B1/B2 轨测试 .go）：检查点义 carrier→checkpoint（旧改名保留）；泛义→既有精确实名（commit／DurableCommit／credential／URL／projection／envelope／fact／record，见上级 design D6 映射）；包内私有标识符改名（lifecycle.go carrier 字段→commit、durable.go newCarrier→newDurableCommit、commit.go commitCarrier→commitLedger、tool.go 局部 carrier→result、seedCarrier→seedCommit）；task_checkpoint_test.go:174「durable carrier」→「durable commit」（旧 Issue3 回退被甲取代）。age_segments「folding generation」→band promotion（保留）；journal_test 错误测试名引用更正（保留）；检查点义测试 helper/夹具/局部变量/测试名改名（保留）；golden/wiki/GLOSSARY 漂移对齐（D 轨）；两处冻结归档附勘误（保留）；Issue7 永久 spec 检查点义中文载体→checkpoint（保留，:38 泛义保留）。godoc 重生成由编排者 E/F0 统一跑。**导出 API 零改动、.go 逻辑零改。**

## 边界与依赖

- **依赖**：02 门入 main（**实现依赖**——门红锚存在才能验转绿；甲门为纯短语禁，红锚＝当前树 390 命中（341 .go＋49 docs/api），01 据上级 design D6 映射清全部 341 .go carrier，非只审计部分）；03 入 main（**实现依赖**——共享 runtime/task/checkpoint.go，03 先定方法名去留〔裁定 KEEP〕，01 后改注释）。GLOSSARY 规范词为**接口常数**。
- **被依赖方**：F0 godoc 重生成（待 01 全部 .go 改定）；F3 红转绿证据。
- **接口面**：无新接口；只改注释/标识符/夹具字面量，**.go 逻辑零改动**。
- **禁止事项**：MUST NOT 动 02/03 白名单文件；MUST NOT 改 .go 逻辑/断言数（改名前后 go test -list 计数不变）；MUST NOT 改写冻结归档原文（只追加勘误行）；MUST NOT 更名冻结契约拼写（GLOSSARY §五）；MUST NOT 自行跑 godoc（编排者 F0 单写）；MUST NOT 扩大注释面（code-documentation：只换词，不新增机制叙述）。

## Impact

- 验收：词汇门 carrier＋generation 规则全绿（carrier 全退役后归 0）；受影响 Go 测试绿；carrier 残留 grep 全仓 .go＋docs/api 0（冻结归档 attribution-carrier／GLOSSARY 退役注为合法门外提及）；godoc --check 绿（E/F0 后）。
- 共享面：runtime/task/checkpoint.go 注释改名待 03 方法名裁定入 main。