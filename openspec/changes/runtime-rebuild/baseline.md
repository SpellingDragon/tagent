# runtime-rebuild · 现场基线（baseline）

> 角色：本文件是 COVER 单的只读基线记录，服务于新计划 `运行时_重设_实施计划_304e54c3.md`（只读 453 行）。
> 边界：本单只读 + 新增 `baseline.md`、`coverage.md` 两个 md；不写产品/测试/旧 change/主 spec/外部 plan，不跑产品 build/test/安装/提交/归档。
> 纪元：本文件所有"存在/缺失"均为**静态盘点**结论；除下方注明外，**本轮未运行任何产品门禁**（见 §5）。

## 1. 元信息

- 仓库：`/Users/pengweiye/Documents/codes/tagent`
- 分支：`next`（HEAD 游离于常规 main/dev 之外）
- HEAD：`56c58eb1da22b15ff4ad84cf9421e9cfe680c93a`（2026-10-12，"refactor: prototype rewrite as event-driven agent runtime; remove legacy production tree"）
- HEAD 更新（2026-10-13 用户授权清理批次后）：`a9381ce9b63d139a97086127b3f644750f53929d`（`chore(plan): land runtime-rebuild planning baseline and contract checkers; remove stale wave remnants`）。**上行为清理前历史记录，保留不改**；残件/旧 change 处置见 §2.1.1/§4.1。
- 历史生产参照：`50acdfe13ed4c2c178a7909651f0e0bf1b02fc20`（2026-10-11，"chore(sync): 反向同步 main 的 squash 合并点到 dev"）
- 其它分支：`main`、`dev`、`dev-resident-bugfixes`（+ remote `origin/dev|main|docs/wiki-pages|integration/dev-main`）
- openspec CLI：本机 `1.2.0`；与 `.github/workflows/ci.yml` 钉定版本一致
- 旧变更：`rebuild-formal-runtime`（18 文件残件），空壳 `runtime-rebuild`（`.openspec.yaml` + `proposal.md`）

## 2. 现存资产 / 缺失资产（静态盘点）

### 2.1 实际存在

| 资产 | 路径 | 备注 |
|---|---|---|
| 主规格 | `openspec/specs/` | **120 个目录**，各含 1 个 `spec.md`（无遗漏目录） |
| 归档变更 | `openspec/changes/archive/` | 130 项 |
| 台账 | `openspec/changes/LEDGER.md` | 历史证据台账（只读参考） |
| 新 change 骨架 | `openspec/changes/runtime-rebuild/` | **开工时**仅 `.openspec.yaml`（schema `spec-driven`）+ `proposal.md`；本轮内并行 writer 追加 `task-graph.json`、`changes/{01,04,07,08}/**`，本单追加 `baseline.md`/`coverage.md`（见 §2.3） |
| 旧 change 残件 | `openspec/changes/rebuild-formal-runtime/` | 18 文件（含 `ORCHESTRATION.md` 及 c0x/d0x/e0x/i0x 域 design/tasks） |
| prototype | `prototype/` | 11 文件（`agent.go`/`config.go`/`contracts.go`/`memory.go`/`model.go`/`plugins.go`/`state.go`/`task_manager.go`/`README.md` + 2 个 integration test） |
| 历史 wiki/文档 | `docs/` | 146 文件（`docs/wiki/{agent,event,memory,platform,plugin,prompt,examples,evolution,reliability,rl,tool}` + `docs/api` + `docs/.dev`） |
| 微信 module | `examples/wechat-bot/` | 独立 module，含 `skills/**`（含用户 `skills/interview/agent-interview-helper/`） |
| 提示词 | `resources/prompts/` | 供嵌入/装载的 prompt 模板 |
| 工程脚本 | `scripts/` | `check-openspec.sh`、`lint.sh`、`race_check.sh`、`gen_godoc.sh`、`verify_restart_prefix.py`、`test_verify_restart_prefix.py`、`test_race_check.sh`、`verify_runtime_acceptance.py`、`codetools/`、`comment_policy/` 等 |
| CI | `.github/workflows/ci.yml` | 单文件五 job（见 §5） |
| 依赖声明 | `go.mod` / `go.sum` | 根 module |
| golden（并行新建） | `golden/runtime/architecture.json` | 本轮内由并行 writer 新建，非本单产物 |
| 规划检查器（并行新建） | `scripts/check_runtime_plan.py`、`scripts/runtime_checklib.py` | 本轮内由并行 writer 新建，非本单产物 |
| 生产残件 | `model/`(2) `state/`(4) `tagent/`(2) `tests/`(2) | 见 §4 |
| 用户原文（未跟踪） | `.tagent-workspace/lilian_article/` | `article.txt`、`raw.html` |
| 隔离备份（未跟踪） | `.pre-isolation-backup/` | `untracked.tgz`、`tracked-changes.patch` |
| 空占位（未跟踪） | `.tmp-proto-a/` | 空目录 |

> 主规格统计（**仅计数，不作语义认证**）：120 目录 / **601** Requirement / **1217** Scenario。
> strict 校验通过 ≠ 内容仍适用；具体能力到新域的处置见 `coverage.md`，一律不因 source 存在即视为可继承。

### 2.1.1 处置更新（2026-10-13 用户授权清理）

> 本节由 **2026-10-13 用户授权清理批次** 追加；上方 §2.1/§2.2 原盘点行**保持原样、不删改**（历史记录），仅在此追加处置结论。
> 本表「旧 change 残件」行（`openspec/changes/rebuild-formal-runtime/`）、「生产残件」行（`model/`/`state/`/`tagent/`/`tests/`）与「空占位」行（`.tmp-proto-a/`）**整目录删除**。

**删除范围（仅限下列六项，未扩大）；精确文件清单（已删除，作为规划参考价值的记录保留在本档）：**

- `model/`（整目录，2 文件）：`model/collect.go`、`model/snapshot.go`
- `state/`（整目录，4 文件）：`state/compile.go`、`state/state.go`、`state/store.go`、`state/reload/publish.go`
- `tagent/`（整目录，2 文件）：`tagent/assembly.go`、`tagent/runtime.go`
- `tests/`（整目录，2 文件）：`tests/integration/model/snapshot_test.go`、`tests/integration/state/candidate_test.go`
- `openspec/changes/rebuild-formal-runtime/`（整目录，**18** 文件）：`ORCHESTRATION.md`、`changes/c02-task-runtime/design.md`、`changes/c03-model-provider/design.md`、`changes/c03-model-provider/tasks.md`、`changes/c04-memory-plugins/design.md`、`changes/c05-state-config/design.md`、`changes/c05-state-config/tasks.md`、`changes/c06-agent-assembly/design.md`、`changes/c07-session-routing/design.md`、`changes/d01-storage-recovery/design.md`、`changes/d02-live-reload/design.md`、`changes/d02-live-reload/tasks.md`、`changes/d02-live-reload/specs/swappable-executor/spec.md`、`changes/e01-tools-mcp-remote/design.md`、`changes/e02-governance-hitl/design.md`、`changes/i01-semantic-memory/design.md`、`changes/i01-semantic-memory/specs/semantic-search/spec.md`、`changes/i03-training-data/design.md`
- `.tmp-proto-a/`（空占位目录，0 文件）

> 对账说明：实际删除文件数 = 2+4+2+2+18 = **28** 个文件（+1 个空目录）。与派单预期「30」差 2，系 `rebuild-formal-runtime/` 现场实为 **18** 文件（与本档 §1/§2.1「18 文件残件」记录一致）而非派单的 20 所致；删除范围为「六项整目录」，未因此扩大或缩小。
>
> **明确不动**：用户 interview skill（`examples/wechat-bot/skills/interview/agent-interview-helper/**`）、原数据与隔离备份（`.tagent-workspace/**`、`.pre-isolation-backup/**`）、`prototype/**`、主规格 `openspec/specs/**`、`docs/**`、`openspec/changes/archive/**`、`LEDGER.md`。

### 2.2 实际缺失

- `golden/`：**本单开工时 0 命中**；本轮内由并行 writer 新建 `golden/runtime/architecture.json`（见 §2.3）。
- 旧命名设想的检查资产 `scripts/check_architecture.py`、`check_structure.py`、`check_runtime_evidence.py`、`test_check_*.py`、`.github/workflows/architecture-gate.yml` **仍不存在**；本轮新检查器以 `scripts/check_runtime_plan.py` + `scripts/runtime_checklib.py` 形态出现（并行 writer 产物）。
- 第一方领域包**整目录缺失**：`config/`、`event/`、`memory/`（含 `memory/search`）、`plugin/`（含 `plugin/governance`）、`agent/`、`workspace/`、`tool/`（含 `tool/exec`）、`telemetry/`、`rl/`、`evolution/`。
- `state/resources/` 子包缺失（被 `state/reload/publish.go` 引用）。
- 旧 change 完整契约 `contracts.md` 及 `refine-rebuild-plan` 完整契约/原检查资产不在现场（计划 §1）。
- **不存在可运行的新生产基线**：残件引用的上述包全缺，`go build ./...` 静态上不可能通过（本轮未运行以取证，见 §5）。

### 2.3 本轮内并行 writer 新增产物（**非本单产物**，仅登记不相干性）

本单与并行 writer 共享该 change 目录；下列内容在本单开工时不存在、于本轮内由其它代理写入。本单**只读不改、不抢占**：

- `openspec/changes/runtime-rebuild/task-graph.json`（**65** 个叶任务 id 依赖索引；本单 coverage 的 task 编号取自它）
- `openspec/changes/runtime-rebuild/changes/{01-state-binding,04-capabilities,07-evolution,08-host}/`（四件套，尚在写入）
- `golden/runtime/architecture.json`
- `scripts/check_runtime_plan.py`、`scripts/runtime_checklib.py`

## 3. 保护清单（只读；不自动恢复、不自动删除）

| 类别 | 路径 | 处置 |
|---|---|---|
| prototype | `prototype/**` | 只读参照，行为对比用；不作恢复清单 |
| 历史 wiki/文档 | `docs/**`（含 `docs/wiki/**`、`docs/api`、`docs/.dev`） | 只读；机制叙事参照 |
| 旧主规格 | `openspec/specs/**`（120） | 只读；逐能力复核见 coverage |
| 用户 interview skill | `examples/wechat-bot/skills/interview/agent-interview-helper/**` | 只读；用户产物，不得改写 |
| 原数据 | `.tagent-workspace/lilian_article/**`、`.pre-isolation-backup/**` | 只读；用户数据/隔离备份 |
| 外部新 plan | `…/cache/plans/运行时_重设_实施计划_304e54c3.md` | 只读（453 行，本单唯一 exec 依据） |
| 外部旧 plan | `…/cache/plans/正式版_分层_重建_规划_304e54c3.md`、`原型扩展讨论稿_304e54c3.md` 及同目录其它 | 只读历史 |
| 旧 change | `openspec/changes/rebuild-formal-runtime/**`、`archive/**`、`LEDGER.md` | 只读历史；不续派任务 |

**逐文件处置门（首轮后续实现适用）**：每个残件/参考对象按其自身用途决定"保留参考 / 原位重写 / 隔离保留 / 另需授权清理"之一；**禁止整树自动删除、恢复或复制旧生产代码**。

## 4. 残余 8 prod + 2 tests 逐文件登记

> 全部**不自动恢复、不自动删除**；本轮仅登记用途/参考价值/不可当 baseline 理由。
> 共同：这些文件的 import 目标包（`config/event/memory/plugin/agent/workspace/tool/telemetry/rl/evolution`、`state/resources`）均缺失 → 不可编译、不可运行，**不能作为新实现基线**。

| 文件 | 行 | 自述用途 | 参考价值 | 不可当 baseline 理由 |
|---|---|---|---|---|
| `model/collect.go` | 194 | 模型流终局归约：`StreamError`/`StreamErrorKind`、`Call`/`Collect`、usage 尾包合并、`ProducerDone` | 终局协议与错误分类的设计参照 | 依赖本包已删的 `Model`/`Request`/`Response` 类型；仅凭标准库 import 判"可编译"是错的（计划 §1 明确） |
| `model/snapshot.go` | 232 | 请求快照四层：`ConfigSummary`/`ToolDeclaration`/`RequestSnapshot`/`SnapshotRequest`、`AttemptIDs` | 采集快照字段与四层分层的设计参照 | 依赖 trpc provider 与已删的 `model.Request`；采集合同尚未设计冻结（00.3/06.x） |
| `state/store.go` | 176 | owner→session 现场权威、投递授权、默认会话（`Deps`/`Store`） | state 现场/授权模型参照 | import `event`/`memory`/`plugin`（全缺）；接口面未按新 goal 冻结（01.2/01.5） |
| `state/reload/publish.go` | 294 | 候选私有构建 + 基线复核 + 原子发布/回滚（`Builder`/`Publisher`/`Candidate`） | 发布/回滚机制参照 | import `state/resources`（缺）；发布计划属 01.5 设计闸未定 |
| `state/compile.go` | 162 | 声明→`OrgPlan`/`StatePlan` 编译（`CompileOrgPlan`/`PrepareStatePlan`/`DigestFile`） | 声明编译参照 | import `config`/`event`（缺）；config 严格解析语义待 01.1 |
| `state/state.go` | 358 | `ErrorKind`/`TaskPolicy`/`AgentPlan`/`OrgPlan`/`Fingerprint` 类型面 | 类型面参照 | 引用 `config.Declaration`（缺）；类型与新任务未对齐，需 01.x 重定 |
| `tagent/runtime.go` | 232 | Runtime Query 只读面（`ObjectKind`/`RuntimeSnapshot`/`TaskControl`） | 查询面维度参照 | import `agent`/`event`（`agent` 缺）；消费方与新 Query 合同（05.x）未定 |
| `tagent/assembly.go` | 222 | 组合根装配收口（exec ledger / search ports / governance store 等适配） | 组合根接线参照 | import `tool/exec`/`memory/search`/`plugin/governance`/`workspace`（全缺）；属 01.4/08.1 |
| `tests/integration/state/candidate_test.go` | — | C05.4 候选提交/基线复核/失败逆序释放测试 | 场景意图参照 | 依赖 state 全量实现（缺）；对应旧 C05 编号任务不派发 |
| `tests/integration/model/snapshot_test.go` | — | 请求快照/透传字段/可观察尝试身份测试 | 断言意图参照 | import provider 与已删的 `model.AssembleRequest`；采集合同未冻结（06.2） |

### 4.1 处置更新（2026-10-13 用户授权清理）

> 同 §2.1.1；本表 8 prod + 2 tests、旧 change 残件与空占位于 **2026-10-13 用户授权清理批次** 整目录删除。本表原登记行**保持原样**（历史记录），仅追加处置。
> “不可编译残件”相关表述不再逐行改写，以本节处置结论为准。

**删除范围与精确文件清单（已删除，规划参考价值以本档文字记录保留）：**

- `model/`（2）：`model/collect.go`、`model/snapshot.go`
- `state/`（4）：`state/compile.go`、`state/state.go`、`state/store.go`、`state/reload/publish.go`
- `tagent/`（2）：`tagent/assembly.go`、`tagent/runtime.go`
- `tests/`（2）：`tests/integration/model/snapshot_test.go`、`tests/integration/state/candidate_test.go`
- `openspec/changes/rebuild-formal-runtime/`（**18**）：`ORCHESTRATION.md` 及 `changes/{c02-task-runtime,c03-model-provider,c04-memory-plugins,c05-state-config,c06-agent-assembly,c07-session-routing,d01-storage-recovery,d02-live-reload,e01-tools-mcp-remote,e02-governance-hitl,i01-semantic-memory,i03-training-data}/**`（含 `d02-live-reload/specs/swappable-executor/spec.md`、`i01-semantic-memory/specs/semantic-search/spec.md`）——逐文件全名见 §2.1.1
- `.tmp-proto-a/`（0，空目录）

**明确不动**：用户 interview skill、原数据/隔离备份、`prototype/**`、主规格 `openspec/specs/**`（同上 §2.1.1）。

## 5. module、Go 版本与 CI 门（原始入口 + 未运行说明）

### 5.1 module 与版本

| module | 具体路径 | module 名 | Go |
|---|---|---|---|
| 根 | `/Users/pengweiye/Documents/codes/tagent` | `github.com/SpellingDragon/tagent` | `go 1.24.1` |
| 微信 | `/Users/pengweiye/Documents/codes/tagent/examples/wechat-bot` | `wechat-bot`（`replace github.com/SpellingDragon/tagent => ../..`） | `go 1.24.1` |

- 锁定依赖（`go.mod`）：`trpc.group/trpc-go/trpc-agent-go v1.11.2`、`…/model/provider v1.10.0`、`trpc-a2a-go v0.2.6-0.20260721084546-…`。计划 §5 记述的 fork `v1.11.2-tagent.1` 以**当前 go.mod 为准**，工作区新版不代表锁定版可用。
- 计划 00.6 目标：Go **1.24.1+**，覆盖根 + 两个 module。

### 5.2 CI 原始入口（`.github/workflows/ci.yml`，触发：push `[main,dev]` / pull_request / workflow_dispatch）

- job `test`（ubuntu, go 1.24, 装 tmux）：
  `go mod verify` → `go build ./...` → `go vet ./...` → `go test ./... -short -count=1` →
  bot module（working-directory `examples/wechat-bot`）：`go build ./...` / `go vet ./...` / `go test ./... -short -count=1` →
  `GOMAXPROCS=1 go test . -short -count=1` → `bash scripts/lint.sh`
- job `race`：`./scripts/race_check.sh . ./memory/... ./agent/... ./evolution/ ./event/ ./tool/... ./plugin/ ./rl/`
- job `validators`（python 3.11）：`python3 scripts/test_verify_restart_prefix.py` / `bash scripts/test_race_check.sh`
- job `openspec`（node 20）：`npm install -g @fission-ai/openspec@1.2.0` → `./scripts/check-openspec.sh`
- job `soak`（仅 workflow_dispatch）：`go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 -v -timeout 45m -args -rounds=30 -events-per-round=30`

> 静态观察（未运行）：`race` job 命令引用的 `./memory/... ./agent/... ./evolution/ ./event/ ./tool/... ./plugin/ ./rl/` 与 `test`/`validators` 依赖的根包在**当前 HEAD 均不存在**，故该 CI 在现现场**结构性不可绿**。此为读命令文本所得，非运行结论。

### 5.3 未运行说明

- 本轮**未运行**任何产品门禁：`go build` / `go vet` / `go test` / `go -C examples/wechat-bot …` / `scripts/lint.sh` / `scripts/race_check.sh` / `openspec validate`。
- 原因：本单边界为"只读参考"；运行产品 build/test 会越过授权面。
- 已做的仅为**只读元数据查询**（`openspec --version`、`openspec list --json`、`openspec status --change runtime-rebuild --json`）与**静态文件/import 文本检索**，均不产出或修改产品状态。

## 6. 凭据与环境（不打印值）

- CI 注释声明：确定性套件**无需必需密钥**；另有 **2 个可选真实-LLM 密钥名**（其名见 ci.yml 注释，本档不复述**值**）。
- 本轮**未读取、未打印任何环境变量或凭据值**；真实服务一律以"存在性/授权"口径登记，绝不落值。

## 7. 本轮职责与本轮边界

- 本单定性：**COVER**。本轮仅由本单生成 **`baseline.md` + `coverage.md` 两个规划检查文档**；**不生成产品**。
- 不创建/不修改 `proposal.md` 以外的产物：不写 `design.md`/`tasks.md`/`specs/`；不写产品代码、测试、CI、golden；不恢复清理掉的残件。
- 只读并可参考并行中的其它新域产物，但**不抢占修改**。

## 8. 疑点与"勿循旧 policy"

1. `rebuild-formal-runtime/ORCHESTRATION.md` 的编排字母（W01/W02 Wave、C/E/I/D 域码、RV 编号、DEC 表、完成包数）**不得作为新调度依据**（计划 §8：W01 式整域、旧 Wave 字母、完成包数不再作为调度依据；旧 Wave 记录不导入）。
2. `rebuild-formal-runtime/` 残件内的 checkbox/进度**不继承**；`openspec list` 显示两 change 均 `totalTasks:0`。
3. `runtime-rebuild` 当前 `proposal` 为 done，`design`/`specs` 为 ready，`tasks` **blocked**（缺 design、specs）；本单**不越权补 artifact**。
4. CI 结构性不可绿（§5.2）与残件不可编译（§4）是**两个独立事实**，不可互相"证明"任务已完成。

## 9. 本轮未做事项

- 未运行任何产品门禁、未安装依赖、未提交、未归档、未清理/恢复任何文件。
- 未生成 `design.md`/`tasks.md`/`specs/`，未改主规格、旧 change、prototype、wiki、用户文件、外部 plan。
- 未对环境/凭据取值。
- 未对 601 Requirement 逐条做语义判定（coverage 为**能力级**复核，见其显式声明）。
