# Tasks: 01 热更矩阵审计与契约化

- [ ] 1.1 全维度取证矩阵落档：按四问法逐维度取证（用户清单七维度 + config schema 反查补遗），矩阵表入本域 `matrix.md`，每行带代码行号/测试名证据 —— 验证：`grep -c "^|" openspec/changes/subagent-config-hotreload/changes/01-hotupdate-matrix-audit/matrix.md` ≥ 10
- [ ] 1.2 fp 面维度契约测：对 prompt/tools/模型 ID/provider/端点各钉一条"配置变更⇒fingerprint 变化"断言（缺者补） —— 验证：`go test ./agent/org/ -run Fingerprint -count=1` exit 0 且覆盖上述五维度（-v 清单核对）
- [ ] 1.3 源面维度契约测：对 threshold/maxTokens/keepRecent/双 TTL 逐维度断言消费点读到解析值（既有 `OrgBudgetLine/OrgKeepRecent` 式；缺的维度补齐） —— 验证：`go test ./agent/... -run 'HotParam|BudgetLine|KeepRecent|TTL' -count=1` exit 0
- [ ] 1.4 摘要 knob 归属实证（域 02 唯一前置）：按 design 三条路排查，结论（配置位置/通道归属/消费点/字段名建议）写入矩阵行与执行注记；若两不沾则落一条现状行为测试作域 02 改前红 —— 验证：矩阵含摘要行 + 结论行；`go test ./agent/compress/ -count=1` exit 0
- [ ] 1.5 生成参数与路由"不做"注脚：各一条 fp 覆盖证据（生成参数在 fp 子集内的实证行；SwappableModel 独立域边界一行）入矩阵 —— 验证：grep 矩阵两行存在
- [ ] 1.6 spec delta（矩阵需求）+ 全量回归：`config-hot-reload` ADDED「配置热更维度矩阵」需求（含假热更红线 scenario）；全包测试净 —— 验证：`go test ./agent/... ./agent/org/... -count=1` exit 0；`openspec validate` 通过由一级收口统一跑
