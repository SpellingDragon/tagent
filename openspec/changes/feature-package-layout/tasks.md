# 任务

- [ ] 1. 主干迁移（编排者）：git mv 三域入 features/、全仓 import 重写、ci.yml race 表、概念预算 11 键、变异注册表路径、doc/api 重生成 —— 验证：`go build ./...` RC=0 + `go test ./... -short -count=1` RC=0 + `go list ./... | grep -c 'tagent/telemetry$\|tagent/evolution$\|tagent/rl$'`=0
- [ ] 2. telemetry 域单：包内 -short + 消费锚（`go test ./tests/integration/host -run 'TestRuntimeVisibility|TestLogObservation' -count=1`——按实名）+ docs 该域句 —— 验证：包内与消费锚 RC=0
- [ ] 3. rl 域单：包内 -short + 消费锚（`go test . -run 'TestRequestSnapshotsSingleSource' -count=1` + `go test ./tests/integration/learning -run 'TestFormalRequestSnapshotHasNoSecrets' -count=1`）+ docs 该域句 —— 验证：RC=0
- [ ] 4. evolution 域单：三包 -short + 变异注册表锚复跑（mutations --all）+ docs 该域句 —— 验证：RC=0
- [ ] 5. cli 定位句 + 新能力包规约（EXTENSIONS/CONTRIBUTING，等行或 doc-budget 锁步）—— 验证：`bash scripts/lint.sh` RC=0（含 doc-budget 零差额）
- [ ] 6. F：全套门禁（后台长跑）+ push + 远端 CI 全绿 + 归档锁步舞
