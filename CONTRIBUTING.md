# 贡献指南（CONTRIBUTING）

面向提交代码、测试与文档的人。维护者的运行、发布与风险面细节见 [`docs/OPERATION.md`](docs/OPERATION.md)。本文每条命令与每个路径都在落笔时按写下的形态实跑/`ls` 过；实测输出随命令给出。

## 一、仓库形态

- **三个 Go module**：根 module `github.com/SpellingDragon/tagent`，以及 `examples/wechat-bot`、`examples/cli-host`（各自独立 module，无 `go.work`）。根 `./...` 永远不覆盖这两个示例 module，三者都要构建与测试。
- **Go 下限** `go 1.24.1`（见 [`go.mod`](go.mod)）。
- **代码面**：顶层产品包为 `api`、`runtime`、`memory`、`evolution`、`telemetry`、`rl`、`tool`、`internal`（基准面 `internal/bench`），加 `cli` 宿主壳与 `assembly` 组合根（组合根住 `assembly/`，目录名即包名）；包边界登记在 [`golden/runtime/architecture.json`](golden/runtime/architecture.json)。
- **门禁工具**：bash、Go、git；spec 门另需 openspec CLI 1.2.0；校验器自测需 python3。

## 二、包结构

领域包（`memory`、`runtime/agent`、`runtime/task`…）按职责切；机制挂在领域包下的**子包**里，子包只装三类文件——第四类不是文件，是判据：

| 三类文件 | 回答什么 | 典型形态 |
|---|---|---|
| **事件词汇** | 这东西是什么：事实/事件类型与身份 | Fact 实现、事件常量、payload 键名 |
| **消费接线** | 谁驱动它：Tick、loop 接入点、协议实现 | 装配函数、装饰器、订阅端 |
| **扩展策略** | 哪一面可插拔：策略、Condenser、判定函数 | 策略接口与默认实现 |

- **业务逻辑不属机制子包**：领域规则本身（分级表、状态机、预算算法）回领域包；机制子包只回答"怎么被事实化、被驱动、被替换"。
- **宿主壳 `cli/` 在组合根之上**（对外装配面，嵌入者经 `NewCLI` 驱动）：命令面与宿主转换面（`HostBridge`/回执/审批）从根包拆出，只依赖包 `assembly` 的公共面与词汇/只读投影，运行时不反向依赖它；宿主不嵌壳时整包丢弃。
- **能力域 `features/`**：可选能力（`telemetry`/`evolution`/`rl`）住 `features/` 下——一能力一包、一个组合根装配点、`api/plugin` 端口声明；后续能力迭代统一走此形，不再平级顶层开新域。
- **新包准入三问**（逐问写下答案，答不全就不开新包，先落进既有包的文件）：
  1. **业务还是机制？** 业务 → 领域包；机制 → 领域包下的机制子包。
  2. **三类文件各是哪几个？** 逐类点名文件；某类为空要写"无"，不是不写。
  3. **扩展策略可插拔吗？** 写成接口或函数值；写成具体实现等于没回答第三类。
- **自检**：`go list -f '{{.ImportPath}}: {{join .GoFiles " "}}' ./memory/...` 列出目标包的文件，逐个贴进上表三行；贴不进任何一行的文件就是第四类，回领域包。
- **分包三原则**：① **契约居核心包、适配器进子包**——绑定接口与类型留核心包，子包装具体实现，子包可整体替换而不动契约；② **一包一语义单元**——包名即该单元的名字，一个包里出现两个能独立命名的主语就再拆；③ **拟人名词只在机制正名处**——"冥想"只指 meditation 机制，不复用为回放标记名（回放的已消化标记叫 consumed），历史旧名只留在命名沿革注释里；**rl 不拆（具名裁定）**：① record/export/sft 共用同一记录本体与 Fact 身份，拆分须复制身份或另立契约包；② capture→record→line→manifest→SFT 的字节溯源链在同一包内闭合；③ 消费者是同一训练数据闭环、无独立生命周期边界，拆包只会增加登记与依赖边。

**文件名规范三条**（门：`codetools package-layout`，接入 `scripts/lint.sh`；判据见 [`docs/comment-gate-tooling.md`](docs/comment-gate-tooling.md) §九）：

| 规则 | 含义 | 违反时 |
|---|---|---|
| **只写 `lower_snake.go`** | 无大写、连字符或空格：排序、import 差异与大小写不敏感文件系统都依赖唯一拼写。唯一例外是 `examples/contract/` 的 `<Iface>_example.go`（文件名即它实现的接口，由 `check_concept_budget.py` 钉） | `file-name-not-lower-snake` |
| **每包至多一份 `skeleton_assert.go`** | 编译期形状钉的正名就是这一个；第二种拼写是同一份契约的变体，不是第二个文件，必须并入 | `skeleton-variant` / `skeleton-collision` |
| **`*_test.go` 镜像生产文件** | 名字是 `<生产文件>_test.go`，让改名压力落在"它是哪个文件的测试"上；宽容表冻结为 `_real` 一个后缀（真实 API 用例）。`tests/integration/`、`examples/contract/` 无生产家族，不做镜像判定 | `test-mirror-missing` |

自检（一条命令同时验三条）：

```bash
go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host
# 期望：package-layout: 505 file(s); names, skeleton pins and test mirrors hold（exit 0）
```

门自身有负例见证 `scripts/gates/test_check_package_layout.sh`（干净／变体名／撞名／大写／孤儿测试五个 fixture 跑真命令，看退出码）。它也在 `scripts/lint.sh` 里跑：只打印不拒绝的检查器过不了干净 fixture，恒红的检查器过不了负例。

## 三、本地开发循环

```bash
go build ./...
go vet ./...
go test ./... -short -count=1
(cd examples/wechat-bot && go build ./... && go vet ./... && go test ./... -short -count=1)
(cd examples/cli-host && go build ./... && go vet ./... && go test ./... -short -count=1)
go mod verify
bash scripts/lint.sh
```

- `-short` 是常规测试面：soak 与需要真实进程/凭据的用例在 `-short` 下跳过。
- [`scripts/lint.sh`](scripts/lint.sh) 是唯一门禁入口：gofmt、根与 wechat-bot 的 build/vet、注释棘轮、doc budget、`name-check`、`doc-refs`、`docs/api` 新鲜度、`proc-refs`、`tracked-hygiene`、`package-layout`（含负例自测）、`dotted-refs`、`commit-scope`。
- 只改文档时可用 `bash scripts/lint.sh --only fmt`：只跑 gofmt，九个文档/脚本门仍然照跑；改了源码注释后先 `bash scripts/gates/gen_godoc.sh` 重新生成 [`docs/api/`](docs/api/index.md)，再 `bash scripts/gates/gen_godoc.sh --check`。**不要手改生成面。**
- race 门（与 CI 同一条命令，较慢）：

```bash
./scripts/gates/race_check.sh . ./memory/... ./runtime/agent/... ./evolution/ ./api/event/ ./tool/... ./api/plugin/ ./rl/
```

- spec 卫生门（需 openspec 1.2.0）：

```bash
bash scripts/gates/check-openspec.sh
```

实测（2026-10-21，go1.27.1 darwin/arm64）：

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | exit 0 |
| `go test ./... -short -count=1` | exit 0，0 个 `FAIL`（实测时 38 个包 `ok`；包数随新增测试增长，以复跑为准） |
| 两个示例 module 的 build / vet / test | 六步 exit 0 |
| `go mod verify` | `all modules verified` |
| `bash scripts/lint.sh` | `lint: ok` |
| `./scripts/gates/race_check.sh …`（包集同上） | `race_check: OK (go test passed)` |
| `bash scripts/gates/check-openspec.sh` | `Totals: 124 passed, 4 failed (128 items)`（4 个 failed 均为活跃 change，主规格全绿） |
| `bash scripts/gates/gen_godoc.sh --check` | `gen_godoc: docs/api matches the source (68 packages)` |

## 四、PR 规范

- **分支**：`dev` 接受直推并由 CI 全量验证，`main` 是发布线；跨线合并前按 CHANGELOG 口径评审。CI 对 `main`/`dev` 的 push 与所有 PR 触发（[`ci.yml`](.github/workflows/ci.yml)）。
- **合入前四条 job 全绿**：`test`（含两个示例 module 与调度下限）、`race`、`validators`、`openspec`。soak job 仅 `workflow_dispatch` 手动触发，不参与每 PR 判定。
- **提交主题**用 `type(scope): 摘要`（`feat` / `fix` / `docs` / `chore` / `refactor`）。
- **`docs(` / `docs:` 开头的提交不得携带代码路径**：`.go` / `.sh` / `.yml` / `.yaml` / `.py`（`docs/`、`openspec/` 与 `*.md` 除外）会被 `commit-scope` 具名拒绝。因此文档与 CI/代码不要放进同一个 docs 提交，分两次提交。（契约见 [`docs/comment-gate-tooling.md`](docs/comment-gate-tooling.md) §八。）
- **一个意图一个落点**：同一意图的改动聚在单一包/文件族；不顺手重构、不整文件重排。
- **用户可见变更记入 [`CHANGELOG.md`](CHANGELOG.md) 的 `[Unreleased]`**（Keep a Changelog + SemVer）；Breaking Changes 写"用户会感知到什么"。
- **不把运行期产物入库**：`*.test`、`*.out`、`*.log`、`*.prof`、`data/` 等由 `tracked-hygiene` 拒绝；新增顶层目录必须同时登记 `scripts/codetools/hygiene.go` 的白名单与 README 布局说明。
- **公共 API 变更**须保持 [`examples/contract/`](examples/contract/) 的编译期契约样例可编译（74 个 `Example` 函数）。

## 五、pre-release 姿态

- 仓库处于 **v0.3.0 pre-release、单人维护**：**不承诺迁移**——数据、配置与 API 都可能 breaking。
- 配置装载是**严格 YAML/JSON**：未知键拒绝启动而非静默忽略。升级前对照 CHANGELOG 的 Breaking Changes 删键。
- 先例：旧单文件快照 `kv.json` 不迁移，存在即忽略、报告一次、冷启动重建。
- 依赖 `trpc-agent-go` 当前指向 fork tag `v1.11.2-tagent.1`；回正条件与移除步骤写在 `go.mod` 的 `replace` 旁与 [`docs/OPERATION.md`](docs/OPERATION.md) §七·D1。

## 六、测试要求

两面都要，缺一面视为未覆盖：

- **黑盒契约**：[`tests/integration/`](tests/integration/) 只经公共入口驱动真实装配，按能力域分组；`examples/contract/` 是编译期契约样例。
- **白盒不变量**：包内测试文件镜像生产文件命名（`durable_test.go` 守 `durable.go`），断言内部不变量，不是把公共 API 再测一遍。参考 [`runtime/state/durable_test.go`](runtime/state/durable_test.go)、[`runtime/task/lifecycle_test.go`](runtime/task/lifecycle_test.go)、[`runtime/agent/loop_test.go`](runtime/agent/loop_test.go)。
  - 测试 doc 用要点行列出被钉住的事实；错误路径要有**具名拒绝**断言，只测 happy path 不算覆盖。
  - 单条复跑：`go test ./runtime/state -run TestDurableFactChainInvariants -count=1` → `ok …/runtime/state 0.174s`。
- **资源门三态**：无门是合法 `Skip`；有门缺凭据是致命失败；半配必须拒绝。真实模型用例在无 `ZAI_API_KEY` / `TENCENT_HY_API_KEY` 时自跳过，不阻塞 CI。
- **测试标识不承载批次/轮次编号**（`name-check` 门）。

## 七、文档要求

- **[`docs/api/`](docs/api/index.md) 是生成面**：真源是源码注释，作者是 `scripts/gates/gen_godoc.sh`，手改必被 `gen_godoc --check` 判红。
- **[`docs/wiki/`](docs/wiki/README.md) 分两类**：被代码 `// 契约:` 引用的 16 篇是**活契约页**（随代码重写式修订，锚 slug 不可改）；其余各篇是历史快照（historical-verbatim），真源是各自的快照提交，不许"对齐今天"式改写，引用由 `doc-refs` 按快照锚定保护。
- **长期事实文档**（`docs/*.md`、根级 `README.md` / `CONTRIBUTING.md`）：真源是今天的树，引用的路径必须存在。`doc-refs` 的扫描面是 `docs/wiki` 与 `docs/*.md`；根级 markdown 不在扫描面内，但同一纪律适用——写下的路径先 `ls` 到。
- **未提交的 `docs/wiki` 编辑会取消该篇的历史锚定**，使其引用集体报 dangling。文档批次先落盘再跑门禁；需要当场判单篇时用 `go run ./scripts/codetools doc-refs docs/wiki/README.md`。机制说明见 [`docs/OPERATION.md`](docs/OPERATION.md) §五·3(a)。
- **`openspec/specs/` 是已接受需求的真源**；过程记录放 `openspec/changes/`（归档后只读）。脚本与 CI 不得把读者指向过程工件（`proc-refs` 门）。
- **代码注释只写两种形态**：go doc 契约注释，与指向长期文档的单行索引 `// 契约: <docs 路径>`；机制与理由写 `docs/wiki/` 或 specs（契约见 [`openspec/specs/code-documentation/spec.md`](openspec/specs/code-documentation/spec.md)）。
- 文档与标识不记录批次号/轮次号。
- **新文档不新造机制概念**：机制名只取 [`docs/GLOSSARY.md`](docs/GLOSSARY.md) 已登记的词；一句话说不清（且不含表外新词）就说明它只是既有能力的字段，写进现有章节而不是起新名。

## 八、Issue

缺陷与功能请求走 [`.github/ISSUE_TEMPLATE/`](.github/ISSUE_TEMPLATE/) 的表单：缺陷需给出环境、复现步骤、预期与实际、模块归属；功能请求需给出场景、期望能力与影响面。
