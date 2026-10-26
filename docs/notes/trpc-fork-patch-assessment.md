# trpc-agent-go fork patch 收口评估（R-J / D1）

> 评估对象：`go.mod` 的依赖 pin（`replace trpc.group/trpc-go/trpc-agent-go => github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`）是否应收口为本地 patch，以摆脱对 fork 存活的依赖。
> 证据分级：**实测** = 本机命令输出；**文档承诺** = 仓库文字声称、本轮未独立复验；**推断** = 由实测 + 工具链语义推得、未直接跑通。
> 评估机器：macOS 14.7.2 / arm64 / go1.27.1；fork 本地检出 `/Users/pengweiye/Documents/codes/trpc-agent-go`。

## 结论（先读这一段）

1. **不建议做本地 patch。** 4 个 commit 的受控代码在 tagent 的构建闭包与测试闭包里都不可达（实测）：`runner` 包完全不在闭包内，`agent.RunWithPlugins` 在 tagent 源码中零调用点；唯一被编译进来的 `agent/run_with_plugins.go` 改动（12 行 defer drain）没有调用方。patch 维护的是当前用不到的行为。
2. **首选动作是回正：去掉 replace，直接用官方 `v1.11.2`。** 预期行为零差异（依据同上），成本是一行 go.mod + 一次门禁复跑。本轮受白名单限制（禁改 `go.mod`）未实测该替换，只给出可复跑步骤与判定条件（见 §五）。
3. **若维护者仍要保留 pin 作为保险，现状（已发布 fork tag）优于任何 patch 形态。** 只有「fork 确定删除且上游仍未合并」时，才值得上 `vendor + patch`；本地目录 replace 与 `-overlay` 明确否决。
4. **patch 解决的是「fork 不存在」，不解决「上游行为差」。** 长期维护路径仍只有回正（上游 PR #2637 合入官方 tag）。
5. 附带实测：fork 本地 `HEAD` 已比 pin 的 tag 多 4 个后续提交（共 8 个 patch 提交、基于更新的上游基线）。若将来做 patch，**必须以 tag `v1.11.2-tagent.1` 为基线**，不能拿 `HEAD` 生成（见 §四）。

## 一、事实基线

### 1.1 pin 在仓库里的位置（实测）

| 位置 | 内容 |
|---|---|
| `go.mod:103-141` | DEPENDENCY DEBT 登记块 + `replace` 指令（第 141 行） |
| `go.sum:7-8` | `github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1` 的 `h1:` 与 `/go.mod` 校验和 |
| `docs/OPERATION.md:170-175` | 面向维护者的 D1 技术债登记与回正条件 |
| `scripts/check_dependency_debt.sh:51-60` | 从 go.mod 重新打印登记内容（恒 exit 0，只提醒不拦截） |

`go mod verify` 实测输出：`all modules verified`。`go list -m` 实测：
`trpc.group/trpc-go/trpc-agent-go v1.11.2 => github.com/SpellingDragon/trpc-agent-go v1.11.2-tagent.1`。

### 1.2 上游状态（推断，需联网复核）

- 本地 `origin/main` 最新 ref：`113795fce`（2026-09-24 15:37 +0800，`model/openai: make streaming accumulation linear (#2634)`）。
- `git branch -r --contains 3ac216807` 只命中 `fork/release/v1.11.2-tagent`；`origin/main` 不含这 4 个提交 → **截至最后一次 fetch，#2637 未合入 upstream main**（推断）。
- 复核命令：`gh pr view 2637 --repo trpc-group/trpc-agent-go --json state,mergedAt`，或 `git fetch origin && git branch -r --contains 3ac216807`。
- go.mod 文字「The tag is published, so … a fresh clone builds from the module proxy」属**文档承诺**：本机模块缓存已解析该 tag 且 `go.sum` 有校验和（实测），但未在干净环境/无缓存机器上复验（推断）。

### 1.3 关键工具链语义（规范，非实测）

`replace` 指令只在**主模块**的 `go.mod` 中生效；其他模块的 replace 被忽略。因此：

- 本 pin 只影响「以 tagent 为主模块」的构建（本仓库、以及直接构建 tagent CLI/二进制的使用者）。
- 任何把 tagent 当库 import 的下游项目，看到的仍是官方 `trpc-agent-go@v1.11.2`，pin 对它们不可见。

这一条把 pin 的影响面缩到「tagent 自己的构建」，而 tagent 自己又不执行被 patch 的代码路径（§二）。

## 二、4 个 commit 清单与可达性

### 2.1 清单（实测，可复跑）

```bash
git -C /Users/pengweiye/Documents/codes/trpc-agent-go log --oneline --no-decorate v1.11.2..v1.11.2-tagent.1
git -C /Users/pengweiye/Documents/codes/trpc-agent-go rev-list --count v1.11.2..v1.11.2-tagent.1   # → 4
```

| # | commit | 标题 | 代码文件 | 测试文件 |
|---|---|---|---|---|
| 1 | `02d156755` | runner: close the processed stream only after the producer is done | `runner/runner.go` | `runner/producer_done_test.go` |
| 2 | `70a30a4d0` | agent: drain the wrapped stream before closing the callback wrapper | `agent/run_with_plugins.go`、`runner/runner.go` | `agent/run_with_plugins_test.go`、`runner/producer_done_test.go` |
| 3 | `009aafbd6` | runner: skip the producer-done drain when the agent channel is nil | `runner/runner.go` | `runner/producer_done_test.go` |
| 4 | `3ac216807` | agent: exercise the wrapper drain with a post-cancel hand-off | —（仅测试） | `agent/run_with_plugins_test.go` |

- 基线：`v1.11.2 = 5a0030b6`；tag：`v1.11.2-tagent.1 = 3ac216807`（实测 `git rev-parse`）。
- 代码面：`2 files changed, 30 insertions(+), 2 deletions(-)`；补丁 diff 共 57 行（实测 `git diff --shortstat` / `git diff | wc -l`）。
- 含测试的整组：`4 files changed, 318 insertions(+), 2 deletions(-)`。
- 语义（实测 diff 阅读）：`runner.runEventLoop` 的 defer 改为「先 cancel → 排空 `agentEventCh` 直到生产者关闭 → 再 close `processedEventCh`」，并给 nil 通道加守卫；`wrapAfterAgentCallbacks` 在 `close(out)` 之后注册一个 `for range src {}` 的 defer（LIFO 先执行），让包装流关闭也意味着内层生产者已退出。

### 2.2 tagent 的可达性（实测）

| 探针 | 输出 |
|---|---|
| `go list -deps ./... \| grep -c 'trpc-agent-go/runner'` | `0` |
| `go list -deps -test ./... \| grep -c 'trpc-agent-go/runner'` | `0` |
| `grep -rn 'RunWithPlugins\|trpc-agent-go/runner"' --include='*.go' .` | 0 行 |
| `go list -deps ./... \| grep -E '/(graph\|tool/agent\|tool/dynamicworkflow\|internal/flow/processor\|agent/(chainagent\|parallelagent\|cycleagent))$'` | 空 |
| `go list -deps ./... \| grep 'trpc-agent-go/agent$'` | 命中（`agent` 包被传递依赖编译） |

解读：

- `runner` 包不在 tagent 的任何构建或测试闭包中 → commit 1、3 的受控代码不被编译，commit 2 的 runner 部分同样不生效。
- `agent` 包在闭包中（经 `tool/skill`、`tool/function`、若干 internal 包间接引入），所以 `agent/run_with_plugins.go` 会被编译；但该文件的 patch 只改 `wrapAfterAgentCallbacks` 内部，而 tagent 不调用 `RunWithPlugins`（源码零命中），闭包内也没有任何 `RunWithPlugins` 调用方（上表第 4 行） → 改动不可达，链接器可整段裁剪。
- 结论：**当前 tagent 构建用官方 v1.11.2 与用 fork tag，运行行为一致**（实测闭包 + 静态调用图；未做二进制级对拍）。

### 2.3 这一结论的边界

- 它只覆盖「tagent 主模块自己的构建与测试」。如果将来 tagent 内部改用 trpc `runner`，或在仓库内新增 `RunWithPlugins` 调用，pin 会重新获得功能意义——回正前需重跑 §2.2 的探针。
- 下游宿主自己 import trpc `runner` 时，使用的是宿主主模块的 replace（pin 不可见，§1.3），与本仓库无关。

## 三、patch 形态评估

| 方案 | 形态 | 自包含 | 可复现 | 主要成本 | 判定 |
|---|---|---|---|---|---|
| A 现状：fork tag | `replace` → 已发布 tag | 是（proxy + go.sum） | 是 | 依赖 fork 存活且 tag 不被改写；1 行 go.mod | 保留（若必须 pin） |
| B vendor + patch | `go mod vendor` 后用 `git apply` 打 2 个代码文件 | 是 | 是（需脚本） | vendor ≈378 个包目录 / 1979 个非测试源文件 / 25.3 MB（实测估算，见 §3.1 末条）；每次依赖变更要重放 patch；`go mod vendor` 会覆盖 vendor，必须脚本化 | 仅当 fork 必须删除时 |
| C 本地目录 replace | `replace` → `./third_party/trpc-agent-go` | 是（整树入库） | 是 | 整模块复制或 submodule（= 又依赖 fork）；目录 replace 不走 go.sum | 否决：go.mod 历史注释已记录本地检出曾被退役（`go.mod:139-140`） |
| D `-overlay` | 构建时用 JSON 映射替换模块缓存文件 | 否 | 否（每条命令、每个 CI job、IDE 都要带；路径随 GOMODCACHE 变化） | 脚本 + 机器相关路径 | 否决 |
| E tagent 层绕过 | 在 tagent 内规避 | — | — | — | 不适用：缺陷在 runner 内部，且 tagent 不用 runner |

### 3.1 方案 B 的具体机制（推断，未落地）

```bash
# 1) 以 pin 的 tag 为基线生成代码补丁（不要用 HEAD，见 §四）
git -C ~/codes/trpc-agent-go diff v1.11.2..v1.11.2-tagent.1 \
  -- agent/run_with_plugins.go runner/runner.go > patches/trpc-agent-go-stream-close.patch

# 2) 回官方版本后 vendor，再打补丁
go mod edit -dropreplace=trpc.group/trpc-go/trpc-agent-go
go get trpc.group/trpc-go/trpc-agent-go@v1.11.2
go mod tidy && go mod vendor
git apply --directory=vendor/trpc.group/trpc-go/trpc-agent-go patches/trpc-agent-go-stream-close.patch

# 3) 校验（vendor 内容不受 go.sum 保护，只能靠脚本与 review）
git diff --exit-code -- vendor   # 在「已提交 vendor」的基线上应为空
```

约束与代价：

- `vendor/` 一旦提交，`go` 自动 `-mod=vendor`，依赖图改走 vendor 副本；`go.sum` 对 vendored 包不再起校验作用，内容正确性只能靠「patch 后 git diff」与 review。
- 每次 `go mod tidy` / 依赖升级都会重写 vendor，必须固定「vendor → apply patch → diff 校验」三步脚本，并纳入 CI。
- 只应打入 2 个代码文件；vendor 中的依赖测试不会被执行，把 288 行测试一起拷入是无效体积（若改用本地目录 replace 才需要测试）。
- vendor 体积实测估算：`go list -deps -test` 覆盖 378 个非标准库包目录、1979 个非测试 Go/汇编/C/头/proto 文件、25.3 MB 源码（另加 LICENSE 等）。

### 3.2 方案 A 的风险量化

- tag 被 force-move：`go.sum` 校验和不匹配会失败（实测：`go mod verify` 当前通过）；proxy 已缓存的版本通常不受影响（推断，取决于 GOPROXY 缓存策略）。
- fork 仓库删除：已发布 tag 仍可从 module proxy 拉取（推断）；`GOPROXY=direct` 或私有代理未缓存时失败。
- 以上两者都只在本仓库作为主模块构建时才有影响，且都不改变运行行为（§2.2）。

## 四、tag 与 fork HEAD 的漂移（实测）

```bash
git -C ~/codes/trpc-agent-go rev-list --count 610a25336..HEAD   # → 8
git -C ~/codes/trpc-agent-go log --oneline 610a25336..HEAD
```

输出 8 个 patch 提交：4 个原始提交 + 4 个后续修复（nil wrapped stream、unregister 时序、测试断言、注释清理），且基于更新的上游基线 `610a25336`。

含义：

- pin 的 tag 只含 4 个原始提交；fork 本地 `HEAD` 已多出 4 个后续修复。**两者不是同一内容**。
- 若做 patch，必须从 tag 生成（4 个）；从 HEAD 生成会把「未在 pin 中验证过的 4 个提交」带进主仓，扩大评审面。
- 反过来，若维护者认为后续 4 个修复也必要，应发布新 tag（`v1.11.2-tagent.2`）并更新登记，而不是在 patch 里夹带。

## 五、回正（drop replace）的落地判定

前置条件（全部满足才执行）：

1. `#2637` 合并且官方 tag 含同一组提交（`git log` 对比提交集，不比字符串）。
2. 官方 tag 与 pin 的提交集一致（无待复评行为差）。
3. 全门禁在官方 tag 上绿：short/race/lint/openspec/bot module。
4. 移除动作与 CHANGELOG `Unreleased` 记录在同一提交。

**但本评估给出一个更早的动作**：基于 §2.2 的闭包实测，「pin 的功能负载为零」，因此可以先做一次**行为无差异验证**再决定是否等 #2637：

```bash
# 该实验会改 go.mod，本轮白名单禁止；由有权限的批次执行
go mod edit -dropreplace=trpc.group/trpc-go/trpc-agent-go
go get trpc.group/trpc-go/trpc-agent-go@v1.11.2
go mod tidy && go mod verify
go test ./tests/integration/... -count=1
go vet ./... && bash scripts/lint.sh
go test -run '^$' -bench '^(BenchmarkAssemble|BenchmarkAssembleInvoke|BenchmarkInvokeTurn)$' -benchtime=2s -count=5 ./internal/bench/
```

判定：集成套件与 lint 全绿、bench 读数落在 `bench/README.md` 基线的噪声范围内 → 回正成立，删除 D1 登记。若出现失败，失败点即 pin 的真实功能负载，届时再评估 patch。

回正后仍需保留的说明：

- `go.mod` 的 DEPENDENCY DEBT 块按既有步骤 e 删除，并在 `CHANGELOG.md` 记一条。
- `docs/OPERATION.md` D1 与 `scripts/check_dependency_debt.sh` 的登记同步删除/更新。
- 若 #2637 之后才合入，回正时以官方 tag 的提交集与 tag `v1.11.2-tagent.1` 逐条对比（4 条），确认无行为差。

## 六、最终取舍

| 问题 | 结论 |
|---|---|
| 现在做本地 patch 吗？ | 不做。功能负载为零，成本为正（vendor 25 MB / 三步脚本 / 校验面扩大）。 |
| 现在回正吗？ | 建议尽快做一次「官方 v1.11.2 + 全门禁」验证；通过即回正。这是唯一能同时消除 fork 依赖与 patch 维护的路径。 |
| 回正前 pin 要动吗？ | 不动。现状 tag 可校验、可复现，风险低于引入第二套构建路径。 |
| 什么条件下改做 vendor+patch？ | fork 确定删除、且回正验证因真实行为差失败、且短期无法等上游。三者同时成立才做，且以 tag 为基线、只打 2 个代码文件。 |
| 什么条件下都不做？ | 本地目录 replace、`-overlay`、把 8 个 HEAD 提交整体搬入——均否决。 |

## 附：证据命令索引

```bash
# 4 commit 可溯
git -C /Users/pengweiye/Documents/codes/trpc-agent-go log --oneline --no-decorate v1.11.2..v1.11.2-tagent.1
git -C /Users/pengweiye/Documents/codes/trpc-agent-go rev-list --count v1.11.2..v1.11.2-tagent.1
git -C /Users/pengweiye/Documents/codes/trpc-agent-go diff --shortstat v1.11.2..v1.11.2-tagent.1

# 依赖闭包可达性
go list -deps ./... | grep -c 'trpc-agent-go/runner'
go list -deps -test ./... | grep -c 'trpc-agent-go/runner'
grep -rn 'RunWithPlugins' --include='*.go' .
go list -deps ./... | grep 'trpc-agent-go/agent$'

# pin 内容校验
go mod verify
go list -m trpc.group/trpc-go/trpc-agent-go

# 上游是否合入
git -C /Users/pengweiye/Documents/codes/trpc-agent-go branch -r --contains 3ac216807
gh pr view 2637 --repo trpc-group/trpc-agent-go --json state,mergedAt

# vendor 体积估算
go list -deps -test -f '{{if not .Standard}}{{.Dir}}{{end}}' ./... | sort -u | wc -l
```
