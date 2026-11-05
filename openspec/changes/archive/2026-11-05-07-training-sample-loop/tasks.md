# 训练样本闭环：孙任务

- [x] 1.1 样本身份与三要素 annex：`TestSampleIdentityAnnex`（annex 同时含 `AttemptID` 与 bench 任务 ID，三要素可按同一样本身份聚合；annex 形态=Line 记录种类 `RecordKindAnnex`，append-only）—— 验证：`go test ./features/rl/... -v -run 'TestSampleIdentityAnnex' -count=1` PASS。
- [x] 1.2 请求回读端到端：`TestSnapshotReadbackReconstructsRequest` + 负锚 `TestReadbackMismatchFailsClosed` —— 验证：`go test ./features/rl/... -v -run 'TestSnapshotReadbackReconstructsRequest|TestReadbackMismatchFailsClosed' -count=1` 用例数=2 PASS。
- [x] 1.3 独立验收器 v1：**复用 `bench` 根包（1.1 产出，可 import 的任务/验收器导出；前置=06.1 入 main，F27）**；**实现落 `features/rl/sample/` 子包**（`features/rl` 根包保持 data-and-ports）；**`golden/runtime/architecture.json` 同步登记 `features/rl/sample` 包与 `features/rl/sample→bench` 边**（不是 features/rl→…；packages+edges）；`TestIndependentAcceptanceIgnoresSelfReport` —— 验证：`go test ./features/rl/... -v -run 'TestIndependentAcceptanceIgnoresSelfReport' -count=1` PASS。
- [x] 1.4 验收结论入 annex（r5 定版：不写 feedback 事件、不派生 reward 向量；奖励向量派生不在本变更范围；r9 F18 补序列化）：验收判定+派生规则版本 v1+产物引用记入 Line 的 `RecordKindAnnex` 载荷（AttemptID 关联、append-only；**不改 AttemptRecord 字段**；**Line 的 annex payload 落未导出字段 `annex`，经 `Annex()` Reader 方法读取、`NewAnnexLine` 构造，不新增 Line 导出字段**）；**`features/rl/line.go` 的 `MarshalLine`/`ParseLine` 增 `RecordKindAnnex` 分支（编码/解码进 payloads；未导出字段+Reader 方法不变）**；annex 含 `AttemptID`+bench 任务 ID；无验收判定→unrated；测试：`TestAcceptanceVerdictInAnnex`（带判定+规则版本+产物引用+双 ID；**写入经 `features/rl/sink.go` 的 `RecordAnnex`，append-only 且计入 loss/manifest 记账**）+ `TestUnratedWithoutAcceptance` + `TestSampleGateRequiresAllThreeElements`（**补持久断言：gate 在 Seal 前评估并写 annex，重读 annex 可见 incomplete 标记**）+ `TestAnnexRoundtrip`（**`MarshalLine→ParseLine→Annex()` 内容一致**）—— 验证：`go test ./features/rl/... -v -run 'TestAcceptanceVerdictInAnnex|TestUnratedWithoutAcceptance|TestSampleGateRequiresAllThreeElements|TestAnnexRoundtrip' -count=1` 输出 4 个 PASS 行。
- [x] 1.5 端到端样本（scripted 模式）：`TestSampleLoopEndToEnd`（5 任务完整执行→**gate 评估+annex 写入**→seal→三要素齐备率；**5 任务全部完整→100% 才成立，分母=全部样本（含 incomplete）=5**；报告落 run）—— 验证：`go test ./features/rl/... -v -run 'TestSampleLoopEndToEnd' -count=1` PASS + 齐备率数字入台账。
- [x] 1.6 域门禁：`go test ./features/rl/... -v -count=1` 全绿（含 `features/rl/sample` 子包）+ **concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（`AnnexRecord` 等新导出类型已在 `concept_bindings` 绑定 `training-sample-loop`；`Line.annex` 未导出）—— 验证：exit 0 + concept budget `errors: []`。

## scenario ↔ 测试映射（F27）

| spec scenario | 测试 |
|---|---|
| 缺验收结论→incomplete | TestSampleGateRequiresAllThreeElements |
| 自述与产物矛盾 | TestIndependentAcceptanceIgnoresSelfReport |
| 验收结论入 annex | TestAcceptanceVerdictInAnnex |
| 哈希漂移关闭 | TestReadbackMismatchFailsClosed |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
