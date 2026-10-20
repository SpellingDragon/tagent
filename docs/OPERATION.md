# tagent 运行手册（OPERATION）

**这份文档面向接手维护者**：读完它应当能在没有原作者在场的情况下构建、测试、过门禁、发版本，并知道哪几处会咬人。它是长期事实文档，不是过程记录——每一条命令都在本文落笔时按写下的形态实跑过，输出摘要随命令一同给出；数字一律附可复跑的命令，不写"大约"。

**当前姿态一句话**：pre-release、单人维护、行为已由测试与门禁固定，但**不承诺数据迁移**（见 §七·B）。读本文的顺序建议：§一 → §三/§四/§五（照抄命令）→ §七（风险面，必读）→ 其余按需。

---

## 一、结论层：接手第一天只需三件事

1. **仓库是两个 module**：根 module 与 `examples/wechat-bot`（独立 module，无 `go.work`）。根 `./...` 永远到不了第二个 module，构建/测试/vet 都必须各跑一遍。
2. **五个门禁入口**：`bash scripts/lint.sh`（格式+构建+vet+注释棘轮+八个文档/脚本门）、`go mod verify`、`bash scripts/check-openspec.sh`、`./scripts/race_check.sh`、`bash scripts/gen_godoc.sh --check`。CI 与本地跑的是同一批脚本，所以"本地绿"与"CI 绿"不会是两个意思。
3. **三条风险面**：配置严格解码（未知键**拒启动**）、pre-release 无迁移承诺、依赖 pin 与白盒测试欠账（§七·D）。

---

## 二、环境与工具链

| 工具 | 本文实测版本 | 用途 | 缺失时的行为 |
|---|---|---|---|
| Go | 本地 `go1.27.1`；`go.mod` 声明 `go 1.24.1`；CI 用 `1.24` | 构建、测试、门禁 | 硬需求，无回退 |
| tmux | 本机在 PATH 内（`command -v tmux` 可解析） | exec 工具面与异步任务层测试 | 相关用例 `t.Skip`（CI 显式安装以保住信号） |
| python3 | CI 用 `3.11` | 校验器自测（validators job） | 只影响该 job，不影响 `lint.sh` |
| node + openspec CLI | CI 装 `@fission-ai/openspec@1.2.0` | spec 卫生门 | `check-openspec.sh` 找不到 CLI 直接 `exit 2` |
| 模块代理网络 | — | 拉依赖（含仓库 pin 的 fork tag） | 冷启动构建失败；`go mod verify` 会先报校验和 |

版本对齐的硬点：**openspec CLI 固定在 1.2.0**。更新的 CLI 会追加本仓 spec 语料尚未采纳的卫生检查，隐式升级会让无关的既有 spec 变红——升级必须走一次专门的 spec 卫生批次。

---

<a id="cli-host-build"></a>
## 三、构建

```bash
go build ./...                                  # 根 module
go mod verify                                   # 校验和必须与 go.sum 一致（pin 的模块尤其）
(cd examples/wechat-bot && go build ./... )     # 第二个 module
```

实测（2026-10-18）：三条均 exit 0，`go mod verify` 输出 `all modules verified`。

---

<a id="cli-host-test"></a>
## 四、测试

```bash
go test ./... -short -count=1                                  # 常规单元面
(cd examples/wechat-bot && go test ./... -short -count=1)      # 第二 module
GOMAXPROCS=1 go test . -short -count=1                         # 根包调度下限（归因不被"先到的轮"满足）
./scripts/race_check.sh . ./memory/... ./agent/... ./evolution/ ./event/ ./tool/... ./plugin/ ./rl/
```

实测（2026-10-18）：根 module `-short` 全量 exit 0（14 个包 `ok`）。race 门的退出码是唯一判定——`race_check.sh` 只在上游已知竞态签名**逐个**匹配且**无**普通失败时才放行豁免，日志看着绿而命令失败不可能被报成通过。

长跑（CI 仅 `workflow_dispatch` 手动触发）：

```bash
go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 -v -timeout 45m -args -rounds=30 -events-per-round=30
```

`tests/` 下的真实模型用例在无凭据时**自跳过**，永不阻塞 CI；需要真跑时提供 `ZAI_API_KEY`（glm 家族）或 `TENCENT_HY_API_KEY`（混元家族），两者相互独立、都可选。

---

## 五、门禁

### 5.1 唯一入口 `scripts/lint.sh`

CI 与本地的命令逐字相同。分阶段：

| 阶段 | 内容 | 失败意味着 |
|---|---|---|
| `fmt` | `gofmt -l` 覆盖 `POLICY_DIRS`（根 + bot module） | 有文件未格式化 |
| `build` / `vet` | 两个 module 各一遍 | 编译或静态检查失败 |
| `policy` | 注释策略**棘轮**（只许降不许升，按规则计数） | 新增了规则外注释 |
| `name-check` | 测试标识不得承载迭代编号 | 标识里带了批次/轮次号 |
| `doc-refs` | markdown 引用的文件必须存在（历史树按快照锚定） | 文档引用了已删除/改名的文件 |
| `gen_godoc --check` | `docs/api` 必须与源码注释同步 | 生成面过期 |
| `proc-refs` | 脚本与 CI 不得把读者指向变更过程工件 | 门禁/脚本里写了过程路径 |
| `tracked-hygiene` | 入库内容非空、非运行期产物、顶层目录已登记 | 误入库 |
| `dotted-refs` | 机器会 import 的点号模块引用必须可解析或登记 | 引用解析不到 |
| `commit-scope` | 文档标题的提交不得夹带代码路径 | 提交面混装 |

用法：

```bash
bash scripts/lint.sh                 # 全量（CI 口径）
bash scripts/lint.sh --only fmt      # 只跑 fmt；build/vet/policy 跳过，八个文档/脚本门照跑
bash scripts/lint.sh --strict        # 要求零发现（收敛终态）
bash scripts/lint.sh --update-baseline   # 批量降低注释计数后重写基线
```

`--only <阶段>` 指定 fmt/build/vet/policy 中**只跑哪一个**；它管不到八个文档/脚本门——那些始终会跑。这就是"只想验文档改动"时最有用的形态。

### 5.2 其余门与常用工具

```bash
bash scripts/check-openspec.sh                        # 主 specs 严格校验（in-flight change 只报告不判红）
bash scripts/gen_godoc.sh                             # 重新生成 docs/api（唯一作者，禁止手改）
bash scripts/gen_godoc.sh --check                     # 生成面新鲜度
bash scripts/check_dependency_debt.sh                 # 依赖 pin 登记提醒（恒 exit 0，见 §七·D1）
python3 scripts/test_verify_restart_prefix.py         # 重启前缀校验器的负例自测
bash scripts/test_race_check.sh                       # race 门分类器的自测（防"掩盖真实失败"）
```

实测（2026-10-18）：`check-openspec.sh` exit 0，`Totals: 121 passed, 0 failed`；`gen_godoc --check` exit 0（`47 packages`）；`proc-refs`/`tracked-hygiene`/`dotted-refs` 均 exit 0。

批次作业时另有两个证明工具（不属常规门）：

```bash
bash scripts/check_comment_only.sh <baseline-ref> <paths...>   # 证明这批只动了注释
bash scripts/check_test_merge.sh <baseline-ref> <dir>          # 证明测试合并没有丢用例/断言
```

### 5.3 两个"本地红但代码没问题"的已知形态

接手后第一次跑 `lint.sh` 若见红，先对照这两条再动手：

**(a) `docs/wiki` 有未提交编辑时，`doc-refs` 会集体报 dangling。** 该门对历史文档按其**快照提交**锚定引用，但前提是文档相对该提交无本地改动（`git diff --quiet <sha> -- <doc>` 失败即放弃锚定，退回按今天的树判定）。所以一批未提交的 wiki 编辑会把原本全部锚定的历史引用当场变成 dangling。

> 实测（2026-10-18）：同一天，干净 HEAD 克隆里 `doc-refs` 是绿的（`doc-refs: 43 citation(s) resolve at their document's snapshot commit (41 inside historical roots, 2 outside)`）；而带 27 篇未提交 wiki/操作文档编辑的工作树里，同样是那 43 条引用全部报 `DANGLING-DOC-REF`。引用本身没坏，是锚定基准暂时失效。
>
> 单文件因果验证（同一克隆）：给 `docs/wiki/tool/tool-architecture.md` 追加一个空行，该篇的 7 条引用立刻转为 dangling（锚定计数 43 → 36，门 exit 1）；`git checkout` 还原即恢复绿。改动内容无关，**"相对快照提交有本地改动"本身就足以取消锚定**。
> 处置：文档批次**先落盘再跑该门**；需要当场判真伪时按文件单独跑——`go run ./scripts/codetools doc-refs <单个文件>` 只判该文件的引用。注意 `lint.sh --only <阶段>` 跳不过这道门（八个文档/脚本门始终会跑），没有任何开关能把它绕过。不要为了绿去改引用路径——那会把历史快照改成对今天码面的断言，等于销毁分区语义（见 §八）。

**(b) bot module 的红与根 module 无关。** 根 `./...` 不含 `examples/wechat-bot`。它的 `build`/`vet`/`test` 在 `lint.sh` 内各有一遍，CI 的 test job 也单列三步。任何"根全绿"的结论都不覆盖它。

---

## 六、发布流程

**仓库内没有自动发布流水线**：`.github/workflows` 只有 `ci.yml` 与 `runtime-contracts.yml`，两者都不含 publish/release 步骤。版本由 git tag 记录（现存 `v0.1.0`、`v0.2.0`、`v0.3.0`），变更由 `CHANGELOG.md` 记录，格式为 Keep a Changelog + SemVer，`[Unreleased]` 常驻（当前内容为"无未发布变更"）。

发一个版本的实际动作序列（全部人工，无脚本代劳）：

1. **门禁全绿**：CI 的 test / race / validators / openspec 四条 job 皆绿（`ci.yml` 对 `main`、`dev` 的 push 与所有 PR 触发）。
2. **写变更记录**：把 `[Unreleased]` 的内容定版为 `## [vX.Y.Z] - YYYY-MM-DD`，Breaking Changes 段必须写明"用户会感知到什么"（现有条目即此口径）。
3. **打 tag**：在待发布提交上 `git tag vX.Y.Z` 并推送；仓库无制品产出、无签名约定，tag 即唯一发布记录。
4. **合并线**：`dev` 接受直推且由 CI 全量验证，`main` 是发布线；跨线合并前按 CHANGELOG 的口径做一次评审（历史上一次 `dev→main` 合并前评审收敛了 45 项发现）。

**没有的东西也要写清楚**，免得接手者去找：无 goreleaser/制品仓库、无 Docker 镜像、无发布审批状态机（早期的发布道机制已整体退役，文件即真源 + git 原语）。发布前唯一必须人工判断的是 Breaking Changes 的用户可见性。

---

## 七、风险面（接手必读）

### A. 配置严格解码：未知键 = 拒启动

所有配置入口共用同一套严格解码：**未知字段让加载明确失败并点名该字段**，不是静默忽略。契约见 `docs/wiki/platform/platform-subsystems.md` 的 strict-decode 节。同一套严格度覆盖：YAML 首个文档之后的尾随文档（拒绝）、JSON 首个值之后的尾随内容（拒绝）、完全空文档（成功、解出零值）、票据的 `0x` 前缀（正常解析）。

**为什么这是风险而不是特性**：升级时若删掉某个配置键（历史上 `memory.fsync` 就是这样下线的），老配置**会带着该键直接拒启动**。升级清单因此必须包含"对照 CHANGELOG 的 Breaking Changes 删键"，而不是"先起服务再看日志"。

### B. pre-release：不承诺迁移

`v0.3.0` 起 CHANGELOG 明示"pre-release 姿态，无迁移承诺"。已发生的先例：旧单文件快照 `kv.json` 不迁移——存在即忽略、报一次、冷启动重建。**结论：升级前读 Breaking Changes，不要假设数据/配置/API 兼容。**

### C. 资源门清单（哪些测试要真东西）

| 门 | 需要的资源 | 无资源时 |
|---|---|---|
| tmux 动作面 / 异步任务层 | tmux 可执行 | `t.Skip`（CI 装 tmux 以保住信号） |
| 真实模型契约层 | `ZAI_API_KEY` 或 `TENCENT_HY_API_KEY` | 自跳过，不阻塞 |
| 微信真实发送面 | `TAGENT_WECHAT_TOKEN`、`TAGENT_WECHAT_REAL_TARGET`、`TAGENT_WECHAT_SEND_AUTHORIZED` 三者同时 | 三者全空 → `t.Skip`（"未验证"）；**只给一部分 → `t.Fatalf`**（半配比无门更诚实） |
| soak 连续性 | 45 分钟窗口 + `workflow_dispatch` | 不跑（手动门） |
| spec 卫生 | node + openspec 1.2.0 | `exit 2`，不静默跳过 |

判据的取向是一致的：**"没门"是合法跳过，"有门缺凭据"是致命失败，"半配"必须拒**——这三态在仓库里是被测试钉住的，不是约定。

### D. 技术债清单（登记在册，均有回正条件）

**D1 依赖 pin（`go.mod` 的 `replace`）。** `trpc.group/trpc-go/trpc-agent-go` 当前指向 fork tag `v1.11.2-tagent.1`（= 上游 v1.11.2 + 4 个 stream-close-contract 提交，即"生产者完成后才关闭事件流"）。tag 已发布，故仓库不依赖任何机器上的本地检出。

- **回正条件（四条，按序）**：① 上游 PR `#2637` 合并且官方 tag 携带同一组提交；② 该 tag 与 fork tag 提交集一致（无待复评的行为差）；③ 全门禁在官方 tag 上绿（short / race / lint / openspec / bot module）；④ 移除动作与 CHANGELOG 记录在同一提交。
- **移除步骤**：`go mod edit -dropreplace=…` → `go get …@<官方 tag>` → `go mod tidy && go mod verify` → 复跑 ③ 的门禁 → 删注释块并在 CHANGELOG `Unreleased` 记录。
- **检查命令**：`bash scripts/check_dependency_debt.sh`（恒 exit 0，只提醒不拦截；pin 存在时打印上述条件与步骤）。真正会咬人的门是 `go mod verify`——pin 的内容与 `go.sum` 不符即失败。
- 回正条件与移除步骤就写在 `go.mod` 的 replace 指令旁，随依赖一起被每个 clone 与每次 review 看到。

**D2 白盒测试欠账。** 判定命令（随时可复跑）：

```bash
go list -f '{{.ImportPath}} {{if or .TestGoFiles .XTestGoFiles}}HAS-WHITEBOX{{else}}NO-WHITEBOX{{end}}' ./... \
  | grep NO-WHITEBOX
```

口径与现状：生产包 = `examples/contract/*`（生成面契约样例，按设计无测试）以外的一切。计划口径登记 **14 个顶层生产包**（根包 + 13 个域包），加 4 个子包（`memory/search`、`tool/exec`、`tool/mcp`、`tool/remote`）合计 18 个生产包曾无任何白盒测试。核心三包 `state`/`task`/`agent` 先行，其余按序续补。

> 实测快照（2026-10-18）：该命令列出 **16 个**生产包仍无白盒测试（根包 1 + 域包 11 + 子包 4）。核心三包 `state`/`task`/`agent` 由专门批次先行补齐，这三个包是否还在清单里随该批次逐日变化——**以复跑输出为准**；本文登记的是判定方法与欠账结构，不是一次性的计数承诺。

**D3 双 module 覆盖缺口。** 根 `./...` 不含 `examples/wechat-bot`（独立 module、无 `go.work`），任何"根绿"都不覆盖它；`ci.yml` 已单列它的 build/vet/test。注意 `runtime-contracts.yml` 头部仍留着"bot module 目前不可编译"的旧描述，实测（干净 HEAD 克隆）该 module 的 `go build`/`go vet` 均 exit 0——**以 `ci.yml` 的实际步骤为准**，该注释待修（属文档漂移，不是行为问题）。

**D4 文档面规模。** 全仓 markdown 实测 1836 篇，其中 `openspec` 1074 篇（变更过程工件）、`examples` 586 篇（第二 module 自带）、`docs` 155 篇。**`docs` 面只有 155 篇**——外部风险评估里"1833 篇"的口径是全仓 markdown（当时 1833，本文落笔时 1836），不是 `docs`。分区对象因此只有 `docs` 这 155 篇，见 §八。

**D5 bus factor = 1。** 缓释手段就是本文档本身：任何"只在一个人的会话记忆里"的约定（门禁口径、踩坑形态、回正条件）必须回写到本文或 wiki，否则它不构成可继承的资产。§五·3 的两条已知形态即此类回写。**观测面见 §七·E**：四类识别点都直接写 trpc 框架面（`trpc-agent-go/log` 与 `telemetry/trace`），仓库不在其上叠第二套抽象。

### E. 观测面：四类识别的观察点（复用框架 log + telemetry）

四类各有一个观察点。日志一律写 `trpc-agent-go/log` 的 `log.Default`，链路一律写 `trpc-agent-go/telemetry/trace` 的 `trace.Tracer`；仓库内的 `telemetry/` 只把这两面接到宿主 sink，不定义新抽象。

| 类 | 观察点（源码） | 级别与载体 | 每次携带（有界） | 行首 |
|---|---|---|---|---|
| 插件相位 | `plugin.Pipeline` 的 Before/After 派发处：`api/plugin/phases.go`、`api/plugin/present.go` | DEBUG 日志 | phase、face、plugin、result、elapsed、call 或 fact 身份 | `tagent: plugin phase` |
| 事件流（事实生产提交） | 主提交之后的 After 趟：`telemetry.LogObserver`（经 `commit-observer` 元素派发） | DEBUG 逐事实 / INFO 回合边界 | 事实类型、tool、task、call、invocation | `tagent: tool call`、`tagent: turn start`、`tagent: turn end` |
| 事件消费 | `runtime/agent/loop.go`：`Loop.Drive` 的起止与每个 decision | DEBUG 日志 | agent、session、invocation、ready 数、decisions、terminal、calls、elapsed | `tagent: consumer drive start`、`tagent: consumer drive end`、`tagent: consumer decision` |
| 模型响应 | `model.ReduceChannel`、`model.ReduceSeq`（唯一终端归约入口） | INFO 日志 | model、terminal、prompt/completion/total token、responses、latency_ms | `tagent: model response` |

- **开 DEBUG**：默认 zap 级别是 Info，相位与消费面需 `log.SetLevel("debug")` 才出；模型响应是 INFO 级，开箱即见、每请求一行。
- **generation span 归框架**（框架 semconv，Langfuse 直接读）：一次模型调用恰好一条 `chat <model>` span，由**驱动模型的那一方**开——框架 `internal/flow/llmflow` 的模型步（`itrace.StartSpan`），或开了 `openai.WithChatTelemetry(true)` 的 provider 模型直调。`gen_ai.operation.name=chat`、`gen_ai.request.model`、`gen_ai.usage.input_tokens`/`output_tokens`（provider 报了缓存命中时再加 `gen_ai.usage.input_tokens.cached`）、失败请求的 `Error` status，都由框架侧渲染；本仓的模型响应面只写那一行 INFO，不在同一个 tracer 上再开第二条。
- **旁路判据**：四个观察点的 logger 调用都在 `recover` 保护内（模型响应面不再调 tracer），失败只丢一条观察，不改事实、不改终态、不改投递。
- **Langfuse 装配（进程启动时一次）**：`telemetry/langfuse.Start` 装的是 `telemetry/trace.Tracer` 与 `TracerProvider`，并把 `gen_ai.operation.name=chat` 的 span 归类为 generation，故 generation span 无需为本仓库写转换代码。**span 去重（已裁）**：框架已为每次模型调用开一条同名 `chat <model>` span，本仓 `api/model/observe.go` 曾在同一个 tracer 上再开一条（没有 input/output messages），一次调用在 Langfuse 上落两条 generation；裁决为**保留框架侧**，本仓那条已删、INFO 行与 token/latency 渲染不动。两种驱动方式下都只有一条：走框架模型步（llmflow）时框架必然开，直调 provider 模型时由宿主的 `openai.WithChatTelemetry(true)` 开——直调且未开该选项的宿主没有 generation span，需要时在模型构造处打开。复跑见 `go test ./api/model/... ./tests/integration/host -run 'ModelCallObservation|ModelRequestIsOneGeneration|FourFacesObservation' -count=1`。

```go
clean, err := langfuse.Start(ctx,
	langfuse.WithHost("cloud.langfuse.com:443"),
	langfuse.WithPublicKey(os.Getenv("LANGFUSE_PUBLIC_KEY")),
	langfuse.WithSecretKey(os.Getenv("LANGFUSE_SECRET_KEY")),
)
if err != nil {
	return err
}
defer clean(ctx)
```

四类的断言各有一条可复跑测试：`go test ./api/model/ ./api/plugin/ ./runtime/agent/ ./tests/integration/host -run 'Observation' -count=1`。其中 `TestFourFacesObservationThroughTheFormalEntry` 走正式装配入口，四类一次性读同一个 logger 与同一个 tracer。

**六机制观测（每事件一条，旁路不阻断）**：考古缺口清单上的五个机制各有一个有界观察点，环境块生成另有一条计数面；级别按"会不会刷屏"与"是否静默改变语义"定。

| 机制 | 观察点（源码） | 级别与载体 | 每次携带（有界） | 行首 |
|---|---|---|---|---|
| 压缩请求 | `memory/compress_filing.go` 的 `Produce`/`Consume` | DEBUG 日志 | producer、reason、id、pending、result | `tagent: compress request` |
| 委派 | `runtime/agent/delegate.go` 的 `Delegate`/`Complete`/`land` | DEBUG 受理与拒答、INFO 落定 | task、agent、call、status、elapsed | `tagent: delegation` |
| 审批 | `evolution/governance/policy.go` 的 `riskCritical` | INFO 裁定、DEBUG 等待 | tool、digest、risk、rule、ruling、by 或 expires_ms | `tagent: approval` |
| 持久化 | `runtime/state/durable.go` 的 `Commit`/`Declare`/`load` | ERROR 日志 | task、mutation、result（uncommitted/uncertain/undeclared） | `tagent: durable` |
| 恢复与存储 tier | `run.go` 的 `Assemble` 与 `restoreChains` | INFO 折叠、WARN 非持久 tier、ERROR 中断 | tier、durable、chains、recovered、dropped、elapsed | `tagent: storage tier`、`tagent: restore` |
| 环境块生成 | `run_request.go` 的 `environmentBlock` | DEBUG 日志 | reason、agent、tasks、omitted；进程计数 `tagent.EnvironmentBlockGenerations()` | `tagent: environment block generated` |

- **级别纪律与旁路**：写盘失败、屏障失败、装载失败一律 ERROR（静默的语义改变必须显式）；非持久 tier 的"进程退出即失忆"是 WARN；审批的每次裁定 INFO，同一等待的重复判定留 DEBUG；其余逐事件 DEBUG。六个观察点的 logger 调用都在 `recover` 保护内，且先判 `log.Default` 非 nil——日志失败只丢一条行，不改判定、不改落定、不改恢复结果、不改请求装配。
- **可复跑**：`go test ./memory ./runtime/agent ./evolution/governance ./runtime/state -run 'Observation' -count=1` 验五个机制各一条输出断言与一条失败不阻断断言；消费者锚 `go test ./tests/integration/history ./tests/integration/execution -run 'Observation' -count=1` 走真实路径（压缩溢出、委派受理与落定、重启折叠）读观测行；环境块计数锚 `go test ./tests/integration/host -run 'EnvironmentBlockGenerationIsObserved' -count=1`。

---

## 八、文档分区方案（只做方案，不动本体）

本节只登记方案与边界，**不迁移任何文件**。触发原因：`docs` 面 155 篇里混着生成面、历史快照、长期事实、工作笔记四类，读者无法从路径判断"这篇何时过期、能不能改"。

### 8.1 现状统计（命令 + 实测值，2026-10-18）

```bash
find docs -name "*.md" | wc -l            # 155
find docs -maxdepth 1 -name "*.md" | wc -l # 5（docs 根，含本文）
git ls-files docs | wc -l                 # 77（入库部分）
find . -name "*.md" -not -path "./.git/*" | sed 's|^\./||' | awk -F/ '{print $1}' | sort | uniq -c | sort -rn
```

| 分区 | 篇数 | 入库 | 真源（谁能改） | 现状定性 |
|---|---|---|---|---|
| `docs/wiki` | 25 | 是 | 各篇的快照提交（历史快照） | 已分区：historical-verbatim，引用按快照锚定 |
| `docs/api` | 48 | 是 | 源码注释（`scripts/gen_godoc.sh` 生成） | 已分区：生成面（live），禁止手改 |
| `docs` 根 | 5 | 4 篇入库，本文未入库 | 今天的树 | 长期事实/操作面（含本文） |
| `docs/notes` | 1 | 否（工作树未入库） | 当时的会话 | 工作笔记 |
| `docs/.dev` | 76 | 否（`.gitignore` 的 `.*/` 规则排除） | 当时的会话 | 本地历史纪要 |

`155 = 25 + 48 + 5 + 1 + 76`；`77 = 25 + 48 + 4` 为入库部分。

### 8.2 分区原则（一条判据）

**每篇文档必须能回答"它的真源是谁、它何时过期、谁在改它"。** 四类各自成立：

1. **生成面（live）**：真源是代码注释，作者是生成器。任何手改都会在 `gen_godoc --check` 变红。
2. **历史快照（historical-verbatim）**：真源是它自己的快照提交。它的断言对**已移除的实现**成立，因此不许"对齐今天"式改写；引用由 `doc-refs` 按快照锚定来保护（§五·3(a) 是其工作机制）。
3. **长期事实/操作面**：真源是今天的树。每条引用必须能 `ls` 到，否则 `doc-refs` 变红。
4. **工作笔记/过程面**：真源是当时的会话，默认不入库；一旦入库就要按"长期事实"标准补齐引用。

`openspec` 与 `examples` 的 markdown 不在分区对象内：前者是按变更归档的过程工件（`check-openspec.sh` 管主 specs 的严格性），后者属于独立 module 的自带文档。

### 8.3 建议目录（提案，未执行）

```
docs/
├── OPERATION.md     运行手册（本文：长期事实）
├── api/             生成面 —— 作者 = scripts/gen_godoc.sh
├── wiki/            历史快照 —— 锚定根 = docRefs 的 historicalDocRoots
├── notes/           工作笔记（默认不入库；入库即按长期事实标准）
├── architecture/    [提议] 当期长期事实（docs 根既有的 4 篇归此；本文自身留根）
└── archive/         [提议] 本地历史纪要入库时的落点（现 docs/.dev）
```

新增目录是**提案**：`docs` 顶层目录已被卫生门登记，新增分区无需改门；但一旦要迁移 `docs/wiki` 或 `docs/api`，就触碰了门与生成器的硬编码边界（见 8.4），必须同批改动。

### 8.4 迁移原则（保全 + 重链接）

1. **保全**：一律 `git mv`（历史可追），内容逐字不改；迁移只换位置，不改断言。
2. **重链接**：迁完必须 ① 跑 `doc-refs`；② 全仓 grep 旧路径并修引用（`openspec` 归档里的引用属历史，不改，只在迁移记录中登记）；③ 若迁 `docs/wiki`，同步改 `scripts/codetools/check.go` 的 `historicalDocRoots`；④ 若迁 `docs/api`，同步改 `scripts/gen_godoc.sh` 的输出路径——两处都是硬编码边界，漏改等于把锚定/生成语义留在旧路径。
3. **分批**：一批只迁一个分区，每批独立 exit 0；迁完跑 `bash scripts/lint.sh --only fmt`（八个文档/脚本门全跑）。**不要在同一个未提交批次里既改 `docs/wiki` 内容又迁它的路径**（§五·3(a)：那会让 43 条锚定引用同时失效，无法判断是迁移错还是锚定基准失效）。
4. **不迁**：`docs/.dev` 与 `docs/notes` 的过程记录除非确定入库，否则保持本地，避免把一次性纪要变成需要长期维护的引用面。

---

## 九、接手第一小时清单

```bash
go build ./... && go mod verify && (cd examples/wechat-bot && go build ./...)
go test ./... -short -count=1
bash scripts/lint.sh
bash scripts/check-openspec.sh
bash scripts/check_dependency_debt.sh
```

任一步红，先读 §五·3（两条已知形态）与 §七（风险面），再判断是不是真缺陷。**"看起来过了"不是判定**：本文列的每条结论都对应一条能重跑的命令。

---

## 十、本文的维护约定

- 陈述体：写"是什么/怎么做"，不写"我们打算""建议考虑"；提案必须显式标 `[提议]`（如 §8.3）。
- 每条命令写进本文前必须按该形态实跑过；数字附命令，不附记忆。
- 本文件位于 `docs/*.md`，在 `doc-refs` 扫描面内：引用的路径必须存在，这是它能长期有效的机制，不是负担。
- 不记录迭代编号与批次号（`name-check`/`proc-refs` 的取向同理）：说明契约，不指向过程。
