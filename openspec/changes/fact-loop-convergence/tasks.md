# Tasks：化归收敛

> 三域触碰集不相交可并发；verify 冻结；域=边界/账目单位，派发原子=就绪叶。

## 01-fact-vocabulary（事实类别契约）

- [x] 01.1 event 包声明 `Fact` 类别接口（FactKey/FactType 两问）＋包 doc 词汇表指引 —— 验证：`go test ./event -run '^TestFactCategoryContract$' -count=10`（各族可赋 Fact：编译断言+两问返回稳定）
- [x] 01.2 五族实现 Fact（Event/Notice/AttemptRecord/Declaration/Ticket 各加方法，零搬迁） —— 验证：`go test ./event ./task ./rl ./config ./workspace -count=1` 全绿＋`grep -rn "func.*FactKey" event task rl config workspace | wc -l` ≥5
- [x] 01.3 `docs/FACT-VOCABULARY.md` 词汇表（族/身份/类型/归属/锁定机制一行一族＋化归判据） —— 验证：`go run ./scripts/codetools doc-refs docs/FACT-VOCABULARY.md` exit 0＋文档预算对账

## 02-consumer-topology（消费器拓扑）

- [x] 02.1 消费器协议文档节（docs/EXTENSIONS.md 追加：读事实→消费→产事实；三实例清单 Drive/Tick/host 与驱动源） —— 验证：doc-refs exit 0
- [x] 02.2 协议一致性测试 `^TestConsumersShareTheProtocol$`（Drive 与 Tick 各消费一条事实并产出新事实；断言不修改源事实/身份保真） —— 验证：`go test ./tests/integration/evolution ./tests/integration/execution -run '^TestConsumersShareTheProtocol$' -count=10` 全绿

## 03-presentation-view（呈现视图归位）

- [x] 03.1 新建 `view` 包（Presentation/Gap/View 自 plugin/present.go 迁出，逐字保留语义）＋契约索引 —— 验证：`go build ./...` 绿＋`grep -c "type Presentation" plugin/*.go` 为 0
- [x] 03.2 plugin 引用 view（FinalPresenter 返回 view.Presentation；After gap 收集用 view 类型）＋run 族/tests/examples 引用更新 —— 验证：`go build ./... && go test ./tests/integration/... -count=1` 全绿
- [x] 03.3 架构登记（golden architecture：view 包 entry/边；plugin→view；run→view） —— 验证：`python3 scripts/check_runtime_architecture.py --implementation` 与 `--check-policy` 双绿

## F 收尾（一级直管）

- [x] F1 契约零改动断言（本 change specs + 既有 golden 合同零改动：`git diff` 契约目录）
- [x] F2 全量门禁：`bash scripts/lint.sh` exit 0＋golden 12 场景全 match（行为零变化）
- [x] F3 DoD 核验入 evidence
- [ ] F4 archive（需用户明示）