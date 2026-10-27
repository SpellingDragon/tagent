# architectural-subtraction · findings

本文件记录本变更实施过程中**跑过、量过、判过**的事实：真实命令、真实退出码、真实
归属（哪条红属于本变更、哪条不属于）。一条结论若没有可复跑的命令与退出码，就不写进来。

---

## 结构收拢·scripts（2026-10-27，基线 HEAD `c868230`）

### 裁定与边界

编排者裁定：`scripts/` 已成垃圾堆（32 个文件平铺，检查器与运行器、自测与资产混在一起），
按**职责**收拢为两个目录，`codetools/`、`comment_policy/`、`hooks/` 原地不动（模块路径与
钩子路径冻结：前者是 `go run ./scripts/codetools`、`go run ./scripts/comment_policy` 的
模块入口，后者由 git 钩子按路径安装）。

**不动根路径 `.go`**：根包拆文件由第三方评审判为「单纯拆文件不解决」，等归因结论另行处理。
本单只搬 `scripts/**` 与引用面。

### 文件去向

| 去向 | 内容 | 数量 |
|---|---|---|
| `scripts/gates/` | 检查器：`check-openspec.sh` `check_comment_only.sh` `check_concept_budget.py` `check_coverage_floor.sh` `check_dependency_debt.sh` `check_doc_budget.py` `check_event_integrity_mutations.py` `check_runtime_{architecture,evidence,plan,structure}.py` `check_test_merge.sh` `race_check.sh` `gen_godoc.sh` `runtime_checklib.py` `doc_budget.json` | 15 |
| `scripts/gates/` | 自测：`test_check_{concept_budget,doc_budget,event_integrity_mutations,runtime_architecture,runtime_evidence,runtime_plan,runtime_structure,runtime_workflow}.py` `test_check_package_layout.sh` `test_race_check.sh` | 10 |
| `scripts/gates/` | 门禁实体：`lint.sh` `preflight.sh`（文件名不变，供根 wrapper 转发） | 2 |
| `scripts/scenarios/` | 运行器：`run_runtime_scenarios.py` `verify_restart_prefix.py` `verify_runtime_acceptance.py` `verify_doc.py` `convert_trajectories.py` `find_task_output.sh` `logprobe.sh` `proc_fingerprint.sh` | 8 |
| `scripts/scenarios/` | 自测：`test_run_runtime_scenarios.py` `test_verify_restart_prefix.py` `test_verify_runtime_acceptance.py` `test_convert_trajectories.py` | 4 |
| `scripts/`（根） | thin wrapper：`lint.sh` `preflight.sh` → `exec bash "$here/gates/<同名>"` | 2 |
| 原地 | `codetools/`（13）`comment_policy/`（3+testdata 2）`hooks/pre-commit`（1） | 19 |
| 删除 | `comment_sweep.py`（零引用，仅归档 change 的 tasks 文本提及）`__pycache__/`（6 个 .pyc，未跟踪） | — |

`runtime_checklib.py` 是**一份**共享库（fingerprint/diff/contract 助手），留 `gates/`；
`scenarios/` 的运行器用显式 `sys.path.insert` 指向 `../gates` 导入，而不是复制第二份。

### wrapper 决策：留根，转发实体

两条候选：(A) 全仓引用改到 `scripts/gates/lint.sh`；(B) 根留 11 行 wrapper 转发。
取 (B)：`bash scripts/lint.sh` 与 `bash scripts/preflight.sh` 出现在 CI、`CONTRIBUTING.md`、
`docs/OPERATION.md`、`ISSUE_TEMPLATE`、`hooks/pre-commit` 与使用者的肌肉记忆里，(A) 要为一次
纯搬家改动支付全仓外部引用面的重写，(B) 只支付 22 行。且规则仍只有一份（在 `gates/`），
wrapper 不复制任何判据，不存在第二处漂移点。

实体脚本自身 `cd "$(dirname "$0")/../.."` 声明仓库根——原 `lint.sh` 信任调用方 cwd，
搬深一层后必须自解析，否则从任意目录调用会读错树。

### 必须同步的内部语义（搬动即失效的硬编码）

1. `__file__` 推根：`check_event_integrity_mutations.py`（`DEFAULT_ROOT`）、
   `check_runtime_evidence.py`（`EVENT_REPO_ROOT`）、`verify_doc.py`（`DEFAULT`）、
   5 个 gates 测试的 `REPO = SCRIPTS.parents[1]`、`test_check_runtime_workflow.py` 的
   `parents[2]`——全部从「上一级即仓库根」改为按新深度推根。
2. 基线与资产自指：`check_doc_budget.py` 的 `DEFAULT_BASELINE` → `scripts/gates/doc_budget.json`；
   `doc_budget.json` 的 `measured_by` 与告警文案同步。
3. `runtime_checklib` 裸导入：`gates/` 内 7 处同目录导入不变；`scenarios/` 侧改
   `sys.path.insert(0, <repo>/scripts/gates)`。
4. `proc-refs` 白名单：`scripts/codetools/check.go` 的 `procRefSkip` 键是**仓库相对路径**，
   12 个键随文件改名，否则门假红（漏改即「白名单指向不存在的路径」）。
5. 钩子：`scripts/hooks/pre-commit` 调 `check_test_merge.sh` 的路径 → `scripts/gates/`。
6. 生成面：`gen_godoc.sh` 内嵌自身路径进 `docs/api/index.md`，改后重生成（1 行变更）。
7. CI：`.github/workflows/ci.yml`、`runtime-contracts.yml` 的 13 处命令路径；
   `test_check_runtime_workflow.py` 的 `GATE_STEPS` 关键字与
   `python -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` 精确串
   （workflow 门把 CI 里的命令当契约读，改一边不改另一边会红——这正是它该红的地方）。
8. 文档/规格/golden 的 18 个文件按「长串优先」做行内替换（不增删行，doc budget 行数中性）。

### 验收凭据

| 门 | 命令 | 结果 |
|---|---|---|
| 检查器自测（新路径） | `python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` | `Ran 233 tests … OK` |
| gates 全部自测 | `python3 -m unittest discover -s scripts/gates -p 'test_*.py'` | `Ran 305 tests … OK` |
| scenarios 自测 | `python3 -m unittest discover -s scripts/scenarios -p 'test_*.py'` | `Ran 171 tests … OK` |
| 包布局门负例 | `bash scripts/gates/test_check_package_layout.sh` | `OK (5 fixtures)`，exit 0 |
| doc budget（行数中性） | `python3 scripts/gates/check_doc_budget.py` | `202 files / 22737 lines`，`slack 0/0`，`RESULT: ok`，exit 0 |
| wrapper 转发（参数） | `bash scripts/preflight.sh --bogus` | exit 2，报错来自实体：`preflight.sh: unknown argument --bogus` |
| wrapper 自解析根 | 从 `examples/` 执行 `bash ../scripts/lint.sh --only fmt` | gofmt 与全部自测照跑（gates 305/233/test_race 15 例、scenarios 均 OK），证明实体不信任调用方 cwd |
| CI workflow 自测 | `python3 scripts/gates/test_check_runtime_workflow.py` | `Ran 44 tests … OK`，exit 0 |
| 三模块 `-short`（工作树） | `go test ./... -short -count=1`（根 + `examples/wechat-bot` + `examples/cli-host`） | `39 ok` / `1 ok` / `1 ok`，三者 exit 0 |
| **clone 级全门** | `git clone <repo> /tmp/tv-scripts && bash scripts/lint.sh` | **exit 0，`lint: ok`**——gofmt/modules/build/vet/注释棘轮/doc budget/selftests/架构政策/概念预算/实现覆盖/事件归属/产品包零发现/openspec strict/包布局负例/点号引用/提交范围 全部过（19 个 stage 无跳红） |
| clone 级三模块 | 同上 clone：`go test ./... -short -count=1` ×3 模块 | 根 `38 ok` exit 0（比工作树少 1 个包＝外来 `zzdualattr` 不存在）、`examples/*` 各 `ok`；scenarios 自测 `Ran 171 … OK` |

`discover -s scripts` 的原命令在搬家后**必然**落到 0 个测试（unittest 只递归带
`__init__.py` 的子目录，本仓脚本目录无包声明），因此 `-s scripts/gates` 是唯一诚实写法，
CI 与 `lint.sh` 两侧同步改并各有一条断言钉住。

### 邻接物（不属于本单，如实登记）

1. **外来未跟踪目录 `zzdualattr/`**（含 `attribution_test.go`）在库内存在。它使四条门在当前
   工作树上必红，与本单无关（同一条输入、四处红）：
   - `comment_policy` 棘轮：10 findings 全在该文件（`REGRESSION free-standing` 6、
     `missing-test-responsibility` 1、`test-doc-not-one-sentence` 3）；
   - `check_runtime_architecture.py --implementation`：
     `unknown first-party package 'zzdualattr': not registered in architecture policy`；
   - `codetools name-check`：`ITERATION-NUMBER-NAME zzdualattr/attribution_test.go:
     TestE1ClaimAtomicity / TestE2DualPath / TestE3ConsumptionWindow`；
   - `gen_godoc --check`：`Only in <tmp>/gen: github_com__SpellingDragon__tagent__zzdualattr.md`。
   本单不删、不改、不 stage 该目录（属并发归因工作在途产物）；干净树（clone）上同四条全绿，
   见上表 clone 级全门。
2. `openspec/changes/fidelity-and-recovery/findings.md` 与 `evidence/` 里的旧路径**不追改**：
   那是过去式运行记录（「当时的实测命令」），改路径等于改写历史证据；且该文件当前有并发会话
   的未提交改动，动它会把他人部署混进本提交。登记于此，不静默遗漏。
3. `docs/.dev/` 下带日期的探索纪要与 `openspec/changes/archive/` 同理，属历史冻结记录，不追改。
4. **clone 级偶发一次红（已归因，非本单）**：首次在 clone 跑根 module `go test ./... -short` 时
   `tests/integration/host/TestExecFaceWiredFromDocument` 报了「closing request carries no
   confirmed release of tmux/…」——该轮与另一个测试批（scenarios 自测）在同机并发。复核：
   同树单测试 `-run TestExecFaceWiredFromDocument` 绿、同树单包 `-short` 绿（25.2s）、
   同树全量重跑 `38 ok` exit 0；基线 clone（`c868230`）全量 `38 ok` exit 0。本单 diff 内
   产品 Go 仅 `scripts/codetools/{check,check_test,package_layout}.go` 三个门禁工具文件，与
   `tests/integration/host` 无调用路径。结论：并发/共享 tmux 命名导致的偶发，与本单无关。

### 回滚

纯搬迁 + 引用同步，无行为变更：`git revert <本提交>` 即回到平铺布局。唯一的语义增量是
wrapper 与实体自解析根，revert 后两者一并消失，无残留状态。
