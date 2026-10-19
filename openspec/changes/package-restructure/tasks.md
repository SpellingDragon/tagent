# Tasks：包结构重排

> 触碰集：01/02 均动 memory/（同簇合单派发）；03 独立（docs）；04=D3 兼容面清理（别名面删除＋词汇归 event）。verify 冻结；行为零变化=golden 判据；文档体量棘轮零余量，故各节标题与条目紧排。

## 01 冥想归位＋正名
- [x] 01.1 curation/压缩判定面迁 `evolution/meditation/`（包名/类型名同步正名 meditation；三类文件挂法落位） —— 验证：`go build ./...` 绿＋`grep -rln "package memory" evolution/meditation` 为 0＋`go test ./evolution/... -count=1` 全绿
- [x] 01.2 标记集改名 consumed（replay_budget 内＋compaction payload 键名；golden S08 同步） —— 验证：`grep -rn "MeditationKeys\|meditation map" memory/` 零残留＋`python3 scripts/run_runtime_scenarios.py --scope deterministic` 全 match
- [x] 01.3 事实词汇表更新（meditation 正名/consumed 归位＋Summary 语义注释） —— 验证：doc-refs exit 0＋`go test ./memory ./evolution -count=1` 全绿

## 02 memory 拆包
- [x] 02.1 `memory/ledger/`（reference/lifecycle/retention/guard/localfile/rustviking 迁入）＋引用更新 —— 验证：`go build ./...` 绿＋`go test ./memory/... -count=1` 全绿
- [x] 02.2 `memory/replay/`（replay_budget/replay_settle 迁入）＋引用更新 —— 验证：同上
- [x] 02.3 architecture/hygiene/gen_godoc 登记同步（新子包 entry/边/registeredTopDirs） —— 验证：`python3 scripts/check_runtime_architecture.py --implementation`＋`--check-policy` 双绿＋`bash scripts/lint.sh`

## 03 判据固化＋边界登记
- [x] 03.1 CONTRIBUTING 包结构节（机制子包三类文件挂法＋新包准入三问） —— 验证：`bash scripts/lint.sh` 全绿
- [x] 03.2 wiki 边界句（mem_spill/error_tracking 设计已放弃）＋doc budget 对账 —— 验证：doc-refs exit 0＋doc budget ok

## 04 兼容面清理（D3：别名面＝债，未发布不留）
- [x] 04.1 删兼容别名面（`memory/aliases.go`＋`memory/replay_aliases.go` 整体删除；`memory/fact.go` 随词汇迁出删除；122 别名符号＝ledger 83／replay 39，消费者 71 文件改直引＝memory/ 外 61＋根 6＋search 4） —— 验证：`go build ./...` exit 0＋声明级残留 `grep -rnE "^(var|const|type) +[A-Za-z0-9_]+ *(=|[^=]*=) *(ledger|replay)\." memory/*.go` 为 0＋122 名全量扫描 0 命中＋`grep -rln "memory\.ChainFact\|memory\.EventChain\|memory\.ProjectionEntry" --include="*.go" .`（除 memory/replay、memory/doc）为 0
- [x] 04.2 压缩请求词汇归 `event`（CompressRequest／CompressRequestType／Fact 实现迁 `event/compress_request.go`；workspace 判定产出点与 meditation 改引 event 词汇；producer/reason 常量留 `memory/compress_request.go`） —— 验证：`go test ./... -count=1` 34 ok／0 FAIL＋`go test ./tests/integration/... -count=1` 10 ok／0 FAIL＋`python3 scripts/run_runtime_scenarios.py --scope deterministic` 11/11 match（drift 0）＋`check_runtime_architecture.py --implementation`／`--check-policy` 双 `RESULT: ok`
- [ ] 04.3 lint 全绿 —— 现状 exit 1 仅停 doc-refs：4 处悬空引用在 `docs/wiki/platform/platform-subsystems.md`（`agent/event_bus.go`／`agent/reliability/inbox.go`／`event/inbox_receipt.go`／`event/wf_facts.go`），HEAD 同文同样携带且被引文件在 HEAD 即不存在 ⇒ 既有红、属 03.2 面、本单不越界；其余子项单独跑均绿（gofmt／build／vet／comment_policy 0 finding／doc budget 99240=99240／name-check／gen_godoc --check 53 packages／proc-refs／tracked-hygiene／dotted-refs／commit-scope）
## F 收尾
- [x] F1 契约零改动＋golden 合同零改动
- [x] F2 全量门禁：lint exit 0＋golden 12 场景全 match
- [x] F3 DoD 核验入 evidence
- [ ] F4 archive（需明示）
