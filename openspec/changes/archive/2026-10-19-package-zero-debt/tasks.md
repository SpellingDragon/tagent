# Tasks：package-zero-debt（全项目 0 债务重构）

> 用户指令（2026-10-19）：整个项目重来、大刀阔斧、0 债务；子代理多用 LSP；充分分块并发。判据：机制包三类文件（事件词汇/消费接线/扩展策略）、领域包业务归位、命名唯一正名、零兼容别名、零死桩、行为零变化（golden 12 场景）。

## 包簇五单（并发，触碰集不相交）

- [x] Z1 evolution 域全量（三通道对称：meditation 补 reflection/digest/observation/watermark；refine/ 子包；consolidation/ 自 memory 迁入；根留评估业务） —— 验证：`go build ./...`＋`go test ./evolution/... ./memory/... -count=1`＋golden match＋architecture 双绿
- [x] Z2 memory+workspace（0 债务扫描：别名/桩/机制名残留 curation 5 处；票据语义归位；三类挂法） —— 验证：同上＋`go test ./memory/... ./workspace/... -count=1`
- [x] Z3 agent+state+task（三类挂法核对；死码/桩/遗留名清零） —— 验证：同上
- [x] Z4 query+rl+telemetry（同上；rl 拆 export/sft 子包按三类挂法裁决） —— 验证：同上
- [x] Z5 model+plugin+view+event+config+tool/*+bench+根包（同上；model aliases=框架边界保留但 doc 自证） —— 验证：同上

## 收尾（编排者直管）

- [x] Z6 docs/登记同步（FACT-VOCABULARY/EXTENSIONS/CONTRIBUTING/architecture/hygiene/gen_godoc/doc_budget 全对齐）＋F1 契约零改动＋F2 全量门禁＋F3 DoD
- [x] F4 archive（需明示）

## 终审反转（用户裁定：plugin 只留三拓展点抽象，实现分派到各领域）

- [x] R1 plugin/ 纯净化（根只留 phases/observe/present/governance 接口面） —— 验证：`go build ./...`＋`grep -rnE '\btype (SDKObserver|ApprovalManager|Policy)\b' plugin/*.go` 零命中
- [x] R2 telemetry/ 回归独立域（otel/recorder/commit_observer/skeleton/invariant 迁回） —— 验证：`go test ./telemetry/ -count=1`
- [x] R3 governance/ 独立域（policy/budget/goal/denial 迁入，实现 plugin.Governor） —— 验证：`go test ./governance/ -count=1`
- [x] R4 task/approval/ 归 task 域（approval/approval_store/skeleton 迁入） —— 验证：`go build ./task/...`
- [x] R5 meditation/refine/consolidation 三件挂法 doc 核对（词汇/接线/策略 各自成文） —— 验证：`grep -n 'package meditation\|契约' evolution/*/*.go`
- [x] R6 引用更新与登记同步（19 个消费者、examples/contract 三例迁目录、architecture/hygiene/gen_godoc/S4/EXTENSIONS/FACT-VOCABULARY/CONTRIBUTING）
- [x] R7 四锚验证（build / test / golden+architecture+lint+S4 / comment_policy）

## 终审第三弹（用户裁定：全包再过一遍、文件名保持规范、CI/Lint 辅助约束）

- [x] R8.1 骨架变体并入：`evolution/meditation/reflection_skeleton_assert.go` 合入 `skeleton_assert.go`（两行契约索引 + 两套断言合一） —— 验证：`ls evolution/meditation/` 无 reflection_skeleton 变体；`find . -name "*_skeleton*.go" | grep -v skeleton_assert.go` 零命中
- [x] R8.2 测试文件精确镜像改名（镜像规则复用 comment_policy：精确 `<stem>.go`，宽容表冻结 `_real`）：13 个 `*_invariant_test.go` 改名，judge→wiring、compress_request→externalize 合并，qualification→export+capture 拆分，wechat-bot `real_wechat_gate_test.go`→`main_real_test.go` —— 验证：`go test ./... -count=1` exit 0
- [x] R8.3 包布局门：`codetools package-layout` 四条具名拒绝（`file-name-not-lower-snake`/`skeleton-variant`/`skeleton-collision`/`test-mirror-missing`）＋单测＋负例自测 `scripts/test_check_package_layout.sh` 接入 `scripts/lint.sh` —— 验证：`go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host` → 500 file(s) exit 0；`bash scripts/test_check_package_layout.sh` → 5 fixture 负例 exit 1、absent-root exit 2
- [x] R8.4 全包文件名词义过审（16 个领域包 + 子包 + examples + scripts，逐个核对文件名与包内角色；仅改名不合语义处，无内容改动） —— 验证：战报核查表＋`go build ./...`
- [x] R8.5 登记同步：`CONTRIBUTING.md` 包结构节补「文件名规范三条」＋自检命令、`docs/comment-gate-tooling.md` §九包布局门、`docs/api` 重生成、`doc_budget.json` 基线上调（rebaseline_reason ⑨） —— 验证：`bash scripts/gen_godoc.sh --check`＋`python3 scripts/check_doc_budget.py`
- [x] R8.6 四锚验证 —— 验证：`bash scripts/lint.sh` → `lint: ok`；`go test ./... -count=1` exit 0；`python3 scripts/run_runtime_scenarios.py --scope deterministic` → `required=11 attested=11 drift=0`；`comment_policy` 0 finding

## 终审第四弹（用户裁定 A：六域收归 memory/evolution 轴；派发单 Z-A/Z-B/Z-C/Z-D）

- [x] Z-C memory/evolution 域扩展收归：`query→memory/query`、`governance→evolution/governance`（两包 git mv，包名不变；本簇消费面只改 import 行；`examples/contract/{query,governance}→{memory_query,evolution_governance}`） —— 验证：`go build ./memory/... ./evolution/...` 绿＋`ls query governance` 零＋`go test ./memory/... ./evolution/... ./tests/integration/inspection ./tests/integration/evolution ./tests/integration/host -count=1` 全绿＋`python3 scripts/run_runtime_scenarios.py --scope deterministic` drift=0＋本簇 `comment_policy` 0 finding（**2026-10-19 第五弹复跑：此前锚2 唯一缺口是 `tests/integration/inspection/observation_test.go` 的 S5 子测试仍读已迁走的 `../../../query`（`read query dir: no such file or directory`），改指 `../../../memory/query` 后五个锚全过：`ls query governance` 两者皆无、build exit 0、上列测试包全 `ok`、deterministic `required=11 attested=11 drift=0`、comment_policy `0 finding(s)`。**）

## 终审第五弹（用户裁定 B1–B4：根包 14→11、宿主壳 `cli/`、架构门登记表语义）
- [x] B1–B4 根包整合：`.` 的 `allow` 定为登记表、`find_cycle` 跳过指向根的回边（正/负例各一钉住）；`cli.go`＋`host_bridge.go`→`cli/`（`host_face.go`）、`opts.go`→`run.go`，根包 11 文件、`diagnostics.go` 独立；消费方与登记同步（architecture/hygiene/gen_godoc/S4/CONTRIBUTING/README）＋gofmt 11 文件＋`gen_godoc` 重生成＋doc_budget 记账 —— 验证：`go build ./...` 绿、`ls *.go | wc -l`=11、`go test ./... -count=1`＋integration 全绿、architecture 双绿、deterministic golden `required=11 attested=11 drift=0`、`bash scripts/lint.sh` 绿、comment_policy 0 finding、scripts 单测全绿
