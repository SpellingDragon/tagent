# 02 delivery-contract 孙任务

验证约定：三锚冻结；I2/I3 锚**红锚先立**（先写失败测试、红因命中"元数据不同构/无账目"，再实现转绿）；触碰 `api/event` 导出面时跑 `bash scripts/gates/gen_godoc.sh --check`。

- [x] 2.1 删 `NewSendFace`（DEL）：`runtime/state/send.go` 第三套门删除，`grep` 零生产引用确认，拒绝账本可取概念小步并入第一套；伴生引用降级回卷 —— 验证：`go test ./runtime/state -count=1` 绿；`grep -c "NewSendFace\|SendFace" --include='*.go' -r .` 零残留或注释说明
- [x] 2.2 投递元数据契约同构：两路投递事实的目标/投递身份/回执键/来源分账键同构（键走 `MetaKey*` 单点），去重与回执规则一致；事实类型保留区分 —— 验证：`go test ./tests/integration/execution -run '^TestInstancesSymptomTwoEntriesOneAccount$' -count=1`（I2 锚）；负锚 `fact_type_distinct`（两类事实类型不混淆）、`dedup_rule_identical`
- [x] 2.3 插件路径补范围门：SettleDriver→fireNoticeTurn 补 deliver_to 类声明式白名单（fail-closed、具名拒绝），与 send 四门白名单档同构 —— 验证：`go test ./assembly -run '^TestPluginPathScopeGate$' -count=1`；负锚 `denies_out_of_scope`（白名单外拒绝）、`fail_closed_beside_empty_list`；红线白名单组零回退（编排者复验：口径=文档声明名册 fail-closed【W2-A2 自决，编排者裁定成立】；host.go 3 行接线编排者落地后 5 处红全绿，-count=2 稳定）
- [x] 2.4 投递账目（G3）：一条源事件→多次单目标投递→逐目标投递身份与回执可查可对账（账目查询面+持久） —— 验证：`go test ./tests/integration/execution -run '^TestInstancesSymptomMultiTargetLedger$' -count=1`（I3 锚）；负锚 `each_target_exactly_once`、`ledger_queryable_without_parsing_body`
- [x] 2.5 竞态与回归：`go test ./runtime/state ./assembly ./tests/integration/execution -race -count=5 -run 'Delivery|Ledger|ScopeGate'` exit 0；红线三组复跑绿（编排者复验：race 双批 exit 0、I2/I3 复绿、非 race 全绿） —— 验证：同命令
