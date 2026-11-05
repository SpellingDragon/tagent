# 折叠可追溯：孙任务

- [x] 1.1 锚⑦转绿：`TestSymptomAnchorFoldSummaryProvenance`（W0 已红）—— 验证：`go test ./memory/replay -v -run 'TestSymptomAnchorFoldSummaryProvenance' -count=1` PASS。
- [x] 1.2 世代透传：assembly/facts.go 的 mappedEntryOf 透传 InstanceID→ProjectionEntry（**文件清单已补**）；**gen=完整 InstanceID**（不用前 8 位截断——recorder-* 前缀会撞车）；**`ProjectionEntry.instanceID` 未导出，经 `WithInstanceID` 设置、`Generation()` 读取（F11/F12，不新增导出字段）**；正例 `TestMappedEntryCarriesInstanceID`（gen=真值且两条不同世代可分辨）+负锚 `TestFoldLineWithoutProvenanceDegrades` + **segment 折叠负锚 `TestSegmentFoldCarriesProvenance`**（:419 公式同改）—— 验证：`TestMappedEntryCarriesInstanceID` 落 **assembly**（经 Host/公共装配路径驱动 mappedEntryOf 透传，非直调未导出函数）；命令=`go test ./assembly ./memory/replay -v -run 'TestMappedEntryCarriesInstanceID|TestFoldLineWithoutProvenanceDegrades|TestSegmentFoldCarriesProvenance' -count=1` 输出 3 个 PASS 行；**锚⑦断言 match[2] 必须等于该行事实的 `Envelope.InstanceID`（逐字比对；占位仅 `gen:?`）**。
- [x] 1.2a segment-fold 产物与二次折叠（F15 第三处公式；r9 F22 定版）：`foldProcessDetail` 产物 Content **行首即锚点**（`[MM-DD HH:MM|gen:…]` 在首字符；原 `folded segment process:` 前缀移到锚点之后或去掉）；`historySummaryLine` 对 segment-fold 产物改用其 Content；`TestSecondFoldKeepsProvenance` 断言**行首锚点**（`[MM-DD HH:MM|gen:…]` 从首字符起匹配，二次折叠后源行时序/世代仍可辨）—— 验证：`go test ./memory/replay -v -run 'TestSecondFoldKeepsProvenance' -count=1` PASS。
- [x] 1.3 消费者锚：`TestFoldProvenanceConsumerPath` —— 验证：`go test ./memory/replay -v -run 'TestFoldProvenanceConsumerPath' -count=1` PASS。
- [x] 1.4 注记负断言（HEAD d6bfc363 已修正两处注记，本域不再改文件，只留防回归断言）：**先在临时副本注入“未接线”一词，`grep -c` 期望 ≥1（证明 grep 真能红，防“本来就没有”空转）**；再对原文件 `grep -c "未接线" examples/wechat-bot/resources/prompts/AGENTS.md` 期望=0 —— 验证：`cp examples/wechat-bot/resources/prompts/AGENTS.md /tmp/AGENTS.md.injected && printf '\n未接线\n' >> /tmp/AGENTS.md.injected && grep -c '未接线' /tmp/AGENTS.md.injected` 输出≥1（证明 grep 能红）；`grep -c '未接线' examples/wechat-bot/resources/prompts/AGENTS.md` 输出=0；两次输出入台账（非 0 则修注记后再验）。
- [x] 1.5 域门禁：`go test ./memory/replay... -count=1` 全绿 + **concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（`ProjectionEntry.instanceID` 未导出+`Generation()` 方法，不新增导出字段）—— 验证：go test exit 0 + concept budget `errors: []`。

## scenario ↔ 测试映射（F27）

| spec scenario | 测试 |
|---|---|
| 跨重启折叠带时序世代 | 锚⑦ + TestMappedEntryCarriesInstanceID |
| 缺世代占位 | TestFoldLineWithoutProvenanceDegrades |
| segment 折叠同改 | TestSegmentFoldCarriesProvenance |
| 二次折叠仍可分辨（行首锚点） | TestSecondFoldKeepsProvenance |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
