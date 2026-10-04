# Tasks

## 1. 合并执行（分四笔独立提交，每组一笔）

- [x] 1.1 org 页 12→1（VET-OK、196 守恒、lint ok，零 free-standing）（`org_hotreload_test.go`）：保守合并 + 剥离成员 2~12 头块（保 Test doc）；判据：vet 0 退（PIPESTATUS）、-list 对账 196/196、lint ok
- [x] 1.2 cross_generation←2；同判据
- [x] 1.3 agent_architecture←4（新名，base 头取 delegation_test 责任块）（新文件名，untracked 注意 reset 不覆盖）；同判据
- [x] 1.4 resources←3（无顶层重名，方法同名不冲突）；同判据；每笔显式 pathspec（**严禁 git add -A**，9718855 教训）
- [x] 1.5 short 59.9s / -race 75.1s / GOMAXPROCS=1 56.8s / lint ok / strict valid 全绿；逐名对账 196=196 零丢失零多出：short + `-race` + `GOMAXPROCS=1` + lint + openspec strict；push CI 四 job 绿

## 2. 收口

- [x] 2.1 README modelref→agent_architecture、upgrade-rollback-drill 的 owner_retirement/partition_collision→org_hotreload、TestWriterLock→agent/resources/entry（修上档遗留失准） 中"测试位于某文件"表述同步（grep `owner_retirement_test\|org_candidate_test\|partition_collision_test`）
- [ ] 2.2 归档变更

## 边界

- 测试函数与断言零改动（本档只动文件组织）；丢一个测试即回滚重做
- P4 拆分的节锚在各用例 doc 中保留（合并产物文件级锚=首成员锚，wiki 页不变）
