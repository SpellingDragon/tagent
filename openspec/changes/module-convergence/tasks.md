# Tasks：模块化归收编

## 01 压缩事件化（行为重排）

- [x] 01.1 `compress_request` 事实族＋判定产出点（工具结果超界/策展阈值触发时产出请求事实，Fact 契约实现） —— 验证：`go test ./memory ./workspace -run '^TestCompressionRequestIsAFact$' -count=10`
- [x] 01.2 策展消费器化（curation→Tick 同构：读 compress_request→凝缩/驱逐产物事实入 state；含既有 curation 不变量迁移） —— 验证：`go test ./memory -run '^TestCurationConsumesCompressionRequests$' -count=10`＋既有 curation 测试全绿
- [x] 01.3 预折叠（Fold 移出组装路径：策展阈值触发预折叠→fold 事实；组装只读） —— 验证：`go test ./memory -run '^TestFoldIsPrecomputedNotInline$' -count=10`＋组装路径 grep 无 Fold 调用
- [x] 01.4 settle 事实化（replay-settle 事件；跳过不留痕语义不变） —— 验证：`go test ./memory -run '^TestSettleLandsAsReplaySettle$' -count=10`
- [x] 01.5 行为等价对照（golden deterministic 11 场景全 match＋同输入压缩产物内容不变 `^TestCompressionProductsAreEquivalent$`；S10 资源场景留 09.5 启用门） —— 验证：`python3 scripts/run_runtime_scenarios.py --scope deterministic` 全 match＋`go test ./tests/integration/... -count=1` 全绿

## 02 消费者声明第二辑

- [x] 02.1 六面消费器协议断言（Subscribe/Deliverer/Apply/Shutdown/Observer/Exporter 各"读→产/不改源事实"） —— 验证：`go test ./tests/integration/... -run '^TestSecondWaveConsumersShareTheProtocol$' -count=10`
- [x] 02.2 EXTENSIONS 消费者协议第二辑表＋FACT-VOCABULARY 附表（六面归属/驱动源/产出事实族） —— 验证：doc-refs exit 0＋lint 全绿

## 03 判据收口

- [x] 03.1 EXTENSIONS 化归判据对照表（16 面三问归位表）＋doc budget 对账 —— 验证：doc-refs exit 0＋`bash scripts/lint.sh`

## F 收尾

- [x] F1 契约零改动断言（本 change specs）＋golden 合同零改动
- [x] F2 全量门禁：lint exit 0＋golden 12 场景全 match
- [x] F3 DoD 核验入 evidence
- [ ] F4 archive（需明示）