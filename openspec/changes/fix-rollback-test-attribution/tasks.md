# Tasks

## 1. fail-before 留痕

- [ ] 1.1 `GOMAXPROCS=1 go test . -run '^TestRollbackOfHotAddNumericWithInFlightTurn$' -count=8` 记录当前失败率（诊断实测 5/8）与「空转通过」结论（gate 从未拦截）入 change 目录

## 2. 编排与断言修复（仅 org_candidate_test.go）

- [ ] 2.1 armGate 前排空告警轮：`waitFor countServed(MAIN) >= 2`
- [ ] 2.2 `base := countServed(SUB-B)`；`"parked"` 谓词改 `> base`；断言①改锚第二条含 `served:SUB-B` 的 MAIN 完成事件；断言②改 `> base+1`
- [ ] 2.3 注释一行索引不变；不引入对预算的任何改动

## 3. 验证

- [ ] 3.1 `GOMAXPROCS=1 -count=8` 8/8 PASS；`GOMAXPROCS=2` PASS；`go test . -short`、`-race`（根包）绿
- [ ] 3.2 lint/openspec strict 绿；push 并确认 CI 四 job 绿
