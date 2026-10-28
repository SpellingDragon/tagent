# 热更新三档：字段级映射表（叶 10 / D13 裁定）

判据（D13）：**一次 reload 只有三种合法读数**——一档「新代发布」（消费点在候选构造或请求边界现读，换代后新值生效）、二档「具名拒绝，须重启」（消费点只在 Boot 构造一次，reload 复用 Boot 的 natives，没有重建通路）、三档「一次发布」（文档内容与在用指纹相同，`PublishUnchanged`，不开新代）。第四种读数「静默 applied」——回执成功、实际没生效——是这张表要消灭的形态。

行取证口径：位置一律指本表所在提交的 `文件:行`。档位由**消费点的位置**定，不由字段的敏感程度定。

行号快照于叶 14 重组前（host.go 3215 行时点）；重组后活文件见 assembly/ 迁移与 host_delivery.go/host_document.go 等拆分。

## 一、为什么必须拒（两类静默 applied）

| 类 | 机制 | 命中字段 | 若不拒的表现 |
|---|---|---|---|
| 甲 | 不进声明指纹：`mapDocument` 只映射 entry/agents/servers（`host.go:2077-2112`），`config.Fingerprint` 只读这份声明（`runtime/state/publish.go:300-305` 同指纹直接 `PublishUnchanged`） | `storage.*`、`request_timeout_seconds`、`governance`、`telemetry` | 回执 `reloaded:true`、`published:false`，新值永不被任何代码再读 |
| 乙 | 进指纹但实例是 Boot 的：`Reload` 把 `h.natives` 原样交给 `Publish`（`host_reload.go:181-189`），而 `hostDocument.toolsFor` / `knowledgeTools` / `planFileTools` / `execManager` / `MemoryPlanOf` / `resolveHostMeditation` 都只在 Boot 跑一次 | `agents.*.tools`、`skill_dirs`、`read_root`、`file`、`exec`、`memory`、`meditation` | 换代发生、绑定重建，但工具/存储/执行实例仍是 Boot 那一批：面看起来换了，能力还是旧的 |

拒绝落点：`compatibleWith`（`host_reload.go:247-271`）在 **natives 复用（`:181-189`）之前**、在 **文档换入（`:199-203`）之前**运行，因此被拒的 reload 不改 `h.doc`、不改 `h.models`、不残留任何半发布的代。哨兵 `ErrHostReloadRestartRequired`（`host_reload.go:39-59`）文案含 `requires a restart`，`Unwrap` 到 `ErrHostReloadStructural`，所以 `/reload` 的 409 映射（`host.go:3090-3091`）与既有结构拒绝同一出口。

## 二、一档：新代发布（`hostDocument` 顶层）

| # | 字段 | 位置 | 消费点（文件:行） | 生效路径 |
|---|---|---|---|---|
| D1 | `provider` | `host.go:1954` | `modelRef`（`host.go:2560-2566`）→ `modelVersion`（`host_model.go:145-147`） | 版本身份移动 → lease 重建（`host_reload.go:134-146`） |
| D2 | `model` | `host.go:1955` | `modelName`（`host.go:2216-2221`） | 同上 |
| D3 | `prompt_dir` | `host.go:1956` | `promptFiles`（`host.go:2203-2212`）解析成绝对路径进 `files` | 清单进指纹 → 换代重建 prompt source |
| D4 | `providers` | `host.go:1957` | `buildModel`（`host.go:2578-2616`）每次构造现读 registry | 换代时 lease 重建读新 registry；**仅改端点不动版本身份时不重读，见 §七缺口** |
| D5 | `mcp_servers` | `host.go:1961` | `Publish` → `declaredServersOf` + `bindMCPServers`（`run.go:412`、`:435`） | 每次发布重绑，热面 |

## 三、一档：新代发布（`hostAgent` 与子结构）

| # | 字段 | 位置 | 消费点（文件:行） | 生效路径 |
|---|---|---|---|---|
| A1 | `provider` | `host.go:1988` | `modelRef`（`host.go:2560-2566`） | 版本身份 → lease 重建 |
| A2 | `model` | `host.go:1989` | `modelName`（`host.go:2216`） | 同上 |
| A3 | `system_prompt.files`（`hostPrompt`，`host.go:2011-2013`） | `host.go:1990` | `mapDocument` 汇入 `spec.Files`（`host.go:2100-2104`）；prompt source 每次发布重建（`tools.go:215-222`）；**正文**在请求边界按文件重读（`request.go:492-503`） | 清单改名/增删 → 新代；只改正文 → FILE 懒读，本就不换代 |
| A4 | `final_round.*`（`host.go:2015-2021`） | `host.go:1994` | `finalRound()`（`host.go:2189-2200`）→ `loop.SetFinalRound`（`run.go:430-431`） | 每发布下发，热面 |
| A5 | `temperature` | `host.go:2000` | `withHostModelParams`（`host_model.go:51`） | 每次绑定构造包一层，热面 |
| A6 | `thinking_enabled` | `host.go:2001` | 同上 | 同上 |
| A7 | `reasoning_effort` | `host.go:2002` | 同上 | 同上 |
| A8 | `max_tool_iterations` | `host.go:2003` | `mapDocument` 折进 `FinalRound.MaxRounds`（`host.go:2080-2083`） | 每发布下发 |
| A9 | `max_tokens` | `host.go:2004` | `FoldWindow`（`request.go:371`）+ age fold 挂载门（`tools.go:229`） | 每绑定构造现读 |
| A10 | `compress_threshold` | `host.go:2005` | `FoldWindow`（`request.go:372`；消费 `age_fold.go:43-47`） | 同上 |
| A11 | `keep_recent_tasks` | `host.go:2006` | `FoldWindow`（`request.go:373`；消费 `age_fold.go:138`） | 同上 |

`api/config/fingerprint.go:192-214` 把这些字段全部编进指纹，所以**真实改动必然换代**，不会落进 `PublishUnchanged`；反之只有整份文档的指纹字段都没变才走三档。

## 四、二档：具名拒绝，须重启

| # | 字段 | 位置 | 唯一消费点（文件:行） | 拒绝名（回执里的 YAML 路径） |
|---|---|---|---|---|
| R1 | `storage.type`（`hostStorage`，`host.go:1971-1975`） | `host.go:1973` | `MemoryPlanOf` Boot（`host.go:271`） | `storage.type` |
| R2 | `storage.root` | `host.go:1974` | `MemoryPlanOf` Boot（`host.go:271`）+ `storageRootOf` 给冥想计划（`host.go:275`） | `storage.root` |
| R3 | `request_timeout_seconds` | `host.go:1960` | `http.Client{Timeout: doc.requestTimeout()}` Boot（`host.go:303`） | `request_timeout_seconds` |
| R4 | `governance` | `host.go:1962` | `riskRules()` + `WithPolicy(governance.Open(...))` Boot（`host.go:372-385`、`:2271-2320`） | `governance` |
| R5 | `telemetry` | `host.go:1964` | `hostTelemetryObserver` Boot（`host.go:349-357`、`:521`） | `telemetry` |
| R6 | `agents.*.tools`（含 `hostToolRef`，`host.go:2023-2031`） | `host.go:1991` | `toolsFor` Boot（`host.go:2360-2413`）：send/recall/knowledge/spec/file 实例就在此构造 | `agents.<id>.tools` |
| R7 | `agents.*.skill_dirs` | `host.go:1992` | `knowledgeTools`←`skillRoots` Boot（`host.go:2514-2527`、`:2530-2538`） | `agents.<id>.skill_dirs` |
| R8 | `agents.*.read_root` | `host.go:1993` | `knowledgeTools`←`readRoot` Boot（`host.go:2514-2527`、`:2542-2547`） | `agents.<id>.read_root` |
| R9 | `agents.*.file` | `host.go:1997` | `planFileTools` Boot（`host.go:2424-2460`，`BaseDir` 在 `:2441`/`:2447`） | `agents.<id>.file` |
| R10 | `agents.*.exec` | `host.go:1998` | `tagentexec.NewManager` Boot（`host.go:365`）；每代只重造 preset 外壳（`tools.go:270`、`:332-340`） | `agents.<id>.exec` |
| R11 | `agents.*.memory` | `host.go:1995` | `MemoryPlanOf` Boot（`host.go:271`）+ recall 分区（`host.go:2372`、`:2816`） | `agents.<id>.memory` |
| R12 | `agents.*.meditation` | `host.go:1996` | `resolveHostMeditation` Boot（`host.go:275`、`:2789`）+ reflection 插件经 `WithPlugins` 闭包读 Boot 计划（`host.go:405-444`） | `agents.<id>.meditation` |

排序与判据：`restartRequiredChanges`（`host_reload.go:285-308`）固定先比 `storage.type`/`storage.root`/`request_timeout_seconds`/`governance`/`telemetry`，再按 agent id 字典序比七个字段面，路径用 YAML 拼写，一次「混合修改」整批拒绝、`revision` 不推进。

`R12` 是「插件声明」在本方言里的落点：本机文档方言没有 `plugins` 键，插件实例来自两份 Boot 输入——文档声明的冥想计划（reflection 插件）与 `WithPlugins` 的宿主附加——两者都随 Boot 固定，故与工具同档。

## 五、不入两档的字段

| 字段 | 位置 | 理由 |
|---|---|---|
| `entry` | `host.go:1953` | 结构拒绝（既有）：`compatibleWith`（`host_reload.go:251-253`）→ `ErrHostReloadStructural`，回执点名前后值 |
| `agents`（集合本身） | `host.go:1958` | 结构拒绝（既有）：增/删 agent（`host_reload.go:254-266`），模型 lease、插件面、工具面都按 Boot 的集合钉住 |
| `app` | `host.go:1963` | 核心零消费者（`host.go` 内无 `doc.App` 读取）：原样携带，不声称应用，也不拒绝——它属于部署方自己的读面 |

## 六、三档：同指纹不开新代（档三锚）

| 锚 | 位置 | 判据 |
|---|---|---|
| 发布入口 | `runtime/state/publish.go:300-305` | `cand.Plan.Plan.Fingerprint == p.effective.Fingerprint` → `PublishOutcome{Publication: p.effective, Result: PublishUnchanged}`，不建记录、不换绑定 |
| 回执字段 | `host_reload.go:61-85`、`:195-198` | 回执带 `publication.generation` 与 `publication.published`（`Published = outcome.Result != state.PublishUnchanged`），所以「没换代」是可读的，不必从空 `model_changes` 反推 |
| 钉住它的测试 | `tests/integration/host/reload_tiers_test.go` | `TestReloadCarriesNoGenerationForAnUnchangedDocument`：两次未改 → `published:false` 且代次不变、lease 无发布；改 model → `published:true` 且代次恰 +1 |

## 七、已知缺口

- `providers.*.api_endpoint`、`api_key_env`、`provider`：只在版本身份（`provider/model`）移动触发 lease 重建时被重读（`host_reload.go:134-146` → `buildModel`）。**只改端点不动模型名**时既不换代、也不被拒：现值静默失效。它没进拒表，因为「新增 provider 键 + 换模型」是当前可用的热路径，按字段拒会误伤；缺口留给「端点热切换」独立提案。
- `app` 无核心消费者，同上表；部署方若缓存了该段，需自行重读源文件。
- `system_prompt` 的**正文**是请求边界懒读（`request.go:492-503`），不在任何档位内：改正文不需要 reload，也不会触发换代。

## 八、验证

```bash
go test ./tests/integration/host -run 'TestReloadPublishesTheHotFieldsAsOneGeneration|TestReloadRefusesTheFieldsOnlyBootReads|TestReloadCarriesNoGenerationForAnUnchangedDocument' -count=1
go test . ./tests/integration/host -count=1
bash scripts/lint.sh
openspec validate success-semantics --strict
```
