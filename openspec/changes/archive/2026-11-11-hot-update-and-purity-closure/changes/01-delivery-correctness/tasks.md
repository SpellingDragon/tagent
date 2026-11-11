# Tasks: 01-delivery-correctness

- [x] 1.1 复核锚 B 已由 restart-recovery E3(e5cb2663) 兑现（基线已绿，不再新建红锚） —— 验证：`go test ./assembly/ -run TestSymptomAnchorDeliveryOutsideWindowSurvivesRestart -count=1` 绿（非空转用例数>0）
- [x] 1.2 复核 owed 驻留已由 E3 链读兑现（owedDeclarations 不限热窗口）；本域不重复增钉 —— 验证：1.1 绿 + `go test ./assembly/ -run TestDeliveryOwedTrimNegative -count=1`
- [x] 1.3 retry attempt 落链跨重启续计有界 —— 验证：正 `go test ./assembly/ -run TestDeliveryRetryBoundedAcrossRestart -count=1` + 负 `go test ./assembly/ -run TestDeliveryRetryExhaustRefuses -count=1`
- [x] 1.4 settlementFactOf 读错具名不重发 —— 验证：负硬门 `go test ./assembly/ -run TestDeliverySettlementReadErrorNotResend -count=1`
- [x] 1.5 eachDeliveryFact 耐久读错 surfaced 不静默降级 —— 验证：负硬门 `go test ./assembly/ -run TestDeliveryDurableReadErrorSurfaced -count=1`
- [x] 1.6 删 deliverySettlement.legacy 只写字段 —— 验证：`grep -c legacy assembly/host_delivery.go` =0 + `go build ./...`
- [x] 1.7 消费者锚：boot/Host.Reload 真实路径补投+负例 —— 验证：`go test ./tests/integration/ -run TestDeliveryRestartRedelivery -count=1`（公共入口 boot，非 helper）
- [x] 1.8 owed 驻留增钉冷读不劣化基准（预热后热态，同树对照） —— 验证：基准 run-id+吞吐/冷读耗时入 run notes（无基准主张不成立，5.4）

## 执行期补记（本单，白名单内 finding）

- 1.3 的 attempt 事实骑结算族，但 spec `delivery-closure`「管道记账噪音（重试计时、中间探测、迁移记账）MUST NOT 进入模型投影可见面」要求在投影映射处排除：`assembly/facts.go mappedEntryOf` 增 `deliveryRetryAttemptOf` 排除 + 负锚 `TestDeliveryRetryAccountingNotProjected`（含正控制：held(retrying) 结算行仍在场）。
