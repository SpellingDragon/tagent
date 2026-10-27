# 任务

- [x] 1. 主干迁移（编排者）：git mv 三域入 features/、全仓 import 重写、ci.yml race 表、概念预算 11 键、变异注册表路径、doc/api 重生成 —— 验证：`go build ./...` RC=0 + `go test ./... -short -count=1` RC=0 + `go list ./... | grep -c 'tagent/telemetry$\|tagent/evolution$\|tagent/rl$'`=0 —— 补勾（ab28851）：全部 RC=0
- [x] 2. telemetry 域单：包内 -short + 消费锚（`go test ./tests/integration/host -run 'TestRuntimeVisibility|TestLogObservation' -count=1`——按实名）+ docs 该域句 —— 补勾（W1-T）：包内 12 PASS+消费锚 12 PASS+自清 0 finding+域内零残留（R100 取证）+共享面缺口上报（golden/docs 误伤，编排者已收口）
- [x] 3. rl 域单：包内 -short + 消费锚（`go test . -run 'TestRequestSnapshotsSingleSource' -count=1` + `go test ./tests/integration/learning -run 'TestFormalRequestSnapshotHasNoSecrets' -count=1`）+ docs 该域句 —— 补勾（W1-R）：包内 7/16 PASS+三消费锚 RC=0+自清 0；docs 病句 2 存量+1 新增上报（已修）
- [x] 4. evolution 域单：三包 -short + 变异注册表锚复跑（mutations --all）+ docs 该域句 —— 补勾（W1-R）：包内 7/16 PASS+三消费锚 RC=0+自清 0；docs 病句 2 存量+1 新增上报（已修）
- [x] 5. cli 定位句 + 新能力包规约（EXTENSIONS/CONTRIBUTING，等行或 doc-budget 锁步）—— 补勾：CONTRIBUTING cli「对外装配面」短语+features 规约句；OPERATION/README/agent-architecture 误伤与措辞修正；ARCH_IMPL ok、GEN=0（lint 全套于 F 收口复跑）
- [x] 6. F：全套门禁（后台长跑）+ push + 远端 CI 全绿 + 归档锁步舞 —— 补勾（2026-10-28）：全套 21 门 19 直绿+2 红具名归因（short10 FoldControl 环境时序 flake 重跑×2 绿；race=继承 U5 基线同红 WATCH 升级）；六长片 5 直绿+1 重跑 match；远端 runtime-contracts@44761b9 SUCCESS；evidence/F2-gates.txt；归档锁步舞随本提交（增量先拒后落锁）
