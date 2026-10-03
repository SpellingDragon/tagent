# Tasks

## 1. fail-before 留痕

- [x] 1.1 `GOMAXPROCS=1 go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=8` 记录当前失败率（诊断实测 5/8）与「空转通过」结论（gate 从未拦截）入 change 目录
      —— 实测 3/24（batch0 1/8、batch1 0/8、batch2 2/8），见 `fail-before.log`；该任务顺带证伪了本任务的排空前提：告警轮实为 2 枚且可合批，绝对计数阈值不成立（结论 3），待裁决后改写 2.1

## 2. 编排与断言修复（org_candidate_test.go + delegation_test.go 基建）

- [x] 2.1 `"parked"` 谓词改 park 直接观测：delegModel 增 per-label parked 计数（parkEnter/parkExit/parkedNow），谓词 `parkedNow("SUB-B") >= 1`（弃排空与基线计数——D1/D1' 双否决）
- [x] 2.2 断言①改锚 MAIN 完成事件增量（`countMainCompletedByB`，Rollback 后 disarm 前取基线）；断言②维持计数增量（①后取 servedNow，注入 `"after"` 后 `> servedNow`）
- [x] 2.3 注释一行索引不变；不引入对预算的任何改动（doc 仅增一条编排策略 bullet，`契约:` 索引行与 waitFor 20s 预算均未动）

## 3. 验证

- [x] 3.1 `GOMAXPROCS=1 -count=8` 8/8 PASS；`GOMAXPROCS=2` PASS；`go test . -short`、`-race`（根包）绿
      —— P=1 三批 ×8 = 24/24 PASS（每批 ≈1.0–1.1s，无预算停滞）；P=2 ×4 PASS；`-short` ok 57.4s；`-race` ok 74.0s
- [ ] 3.2 lint/openspec strict 绿；push 并确认 CI 四 job 绿
