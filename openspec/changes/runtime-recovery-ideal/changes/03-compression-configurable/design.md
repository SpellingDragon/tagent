# 03 压缩可配置 · 设计（对照一级 design.md D6.17 / D1 P8 / §五）

## 当前重整设计（本轮勘误，行动单仅见 tasks.md）

**状态**：03.R1/03.R2/03.R4 已落地（03.R2 含 R278 走序修复，凭据见 `tasks.md`「03.R2 收口记录」）；03.R3 已复验（凭据见 `tasks.md`「03.R3 复验记录」：四要素逐项核销、代码零偏差，两项 wiki 叙述滞后与一项无锚场景已登记待派）。下文原预算裁决与历史凭据不推导当前完成；四叶的独立只读审未做。

| 事实与缺口 | 设计取向 | 承接 |
|---|---|---|
| `assembly/host_document.go:90-117/:194-212` 的宿主字段及映射曾缺预算（03.R4 已接通）；核心 `AgentSpec.SummaryBudgetBytes` 已有 | 宿主 YAML 透传既有字段，默认仍由既有配置/`normalizeFoldWindow` 处理；不新造宿主预算或放宽严格解码 | 03.R4；03.R3 验收 |
| `compress_checkpoint.go:57-64/:104-106` 输出只含未完，却仍按全部历史遍历、预分配；`compress_consumer.go:178/:190` 的 `judgments/answered` 仍累积 | 清减恢复有效态与构建过程；历史及已答证明按身份读链，乱序未完不由单一最大序号抹掉 | 03.R2，依 G2/12.R2 |

03.R2 的成本见证固定未完量与每次新输入、增加已结历史，分开报告：①构建遍历量与分配量；②常驻状态项数/内存；③序列化输出字节。既有 `TestCompressionSliceBodyIsBoundedByTheBatchItOwes` 只证第三面，不能证明前两面；耗时作辅助，不用小输出冒充少读。

已落链的模型摘要是事实；重试读取它，不假设模型再次生成相同文字。清减保留请求身份、历史决定及待办，不删事实、不以 TTL 抹义务；恢复与运行期继续同一消费语义。需要格式变化时先冻结迁移/回退验收，数据只用合成根。文件集、正负/真实入口锚及独立审要求集中于 `tasks.md`，不在此重复。

## 一 原预算设计裁定（含被否方案）

### D3.1 预算进声明面，形态对齐既有两条压缩策略

- 声明键 `summary_budget_bytes`（int，policy 类）：`agentKeys`（api/config/decode.go:158-165）在 `keep_recent_tasks` 后追加，`decodeAgent` 同处增设 `countField` 读取（对照 :208），`AgentSpec`（api/config/config.go:82-90 段）在 `KeepRecentTasks` 后加字段，`DefaultSummaryBudgetBytes = 2048` 落 api/config/defaults.go（与 :40/:43 两条策略缺省并列）。
- **两处填缺省，与既有两条策略同形**：声明路径由 decode 填（defaults.go:24/:27 的形态）；直接构造 `AgentSpec` 的宿主/测试路径（如 tests/integration/host/compress_pipeline_test.go:360-363 只给 `ReplayWindow`）由 `normalizeFoldWindow`（assembly/request.go:596）填。`FoldWindow`（assembly/age_fold.go:25-33）加 `SummaryBudgetBytes`，`foldWindowOf`（request.go:606）从 spec 取，`normalizeFoldWindow` 零值补缺省。
- 否「挂在 `memory/replay` 包上直接读 api/config」：该包现无 api/config import，纯读层不被声明层污染（D1 P2 / 契约: docs/wiki/agent/event-flow.md#projection-lifecycle）。

### D3.2 界由 plan 承载，常量降为策略缺省（不删）

- `AgeFoldPlan`（memory/replay/age_segments.go:413）加 `SummaryBudgetBytes`；解析与 `KeepRecent` / `VolumeThreshold` 并列（:474-478、`thresholdOf` age_plan.go:81-88），新增 `summaryBudgetOf(plan) int`，零值读 `DefaultAgeSummaryTextLimit`。
- `historySummaryOf(entries, limit)`（:603）与 `ageSummaryTextOf(lines, limit)`（:641）由包级常量改为形参；调用点 :509（`FoldByAge`，plan 在作用域）与 age_plan.go:115（`planFold`）传解析后的预算。机械侧的 `ageSummaryDropReserve` / `ageSummaryLineJoin`（:102/:105）不变——它们是同一正文的形状预留，不是预算。
- **常量保留为缺省值**，理由三条且逐条可验：① 对称——同族 `DefaultAgeKeepRecent=2`（:68）与 `DefaultAgeVolumeThreshold=4096`（:71）同为「plan 零值读包内缺省」，删一条造成平行值不对称（AGENTS.md「平行值取对称」）；② 分层——replay 不读 api/config，plan 零值必须有定义域内的意义（`foldWindowOf` 之外还有 programmatic 调用：assembly/facts.go:570 `FoldWindow{}`、测试直构 plan）；③ 缺失语义——正文界必须为正，零值若读「无界」会让摘要随历史增长，回到 S1 红。
- **被否**：「删常量，零值读 `config.DefaultSummaryBudgetBytes`」——replay 反向依赖 api/config，且同族两条缺省仍在 replay 包内，形态分裂。**被否**：「保留 2048 作硬界只加声明但不改 golden」——D6.17 明确要求 golden 从冻结机制常量改为可配置策略量。
- 常量身份随之改变：不再是「承载界的机制常量」，只是「策略缺省值」，golden reason 同 `compress-threshold-default`（golden/runtime/window_bounds.json:61-68「策略缺省值；策略可调而缺省值不可配」）的口径。

### D3.3 摘要模型面与 `suppliers` 同一取法（租约 + 声明采样）

- `bandSummaries.SummaryGist`（assembly/summary_gist.go:57-84）取模型改为：读 `r.agentSpecOf(agent)`（assembly/run.go:996-999）；`held := r.leaseOf(agent)`（run.go:3315）非 nil 时用 `withHostModelParams(rl.NewLeaseModel(held.lease), spec)`，否则回落 `withHostModelParams(native.Model, spec)`。此即 assembly/tools.go:218-227 的同一取法，禁第二套；交接说明见该处 :208-217 注释（租约句柄稳定、版本按请求起手借用）。
- 缺模型判据保持：无租约且 native 无模型 → `ErrNoSummaryModel`（summary_gist.go:27），消费方降级机械型（compress_consumer.go:280-283）。
- 采样参数由 `hostParameterizedModel` 注入（host_model.go:53-70），只填请求未命名的参数，故 `bandSummaryRequest`（summary_gist.go:88-95）不必自带采样。
- 否「摘要请求自建租约/自建模型」：租约是 Run 的既有装配面（run.go:3244-3256 leaseState），摘要面只是其消费者。

### D3.4 预算单参数贯穿提示词与裁剪（禁第二处字面量）

- `bandSummaryInstruction` 由 const 改为 `bandSummaryInstruction(budget int) string`（summary_gist.go:40-43，`fmt` 已在 :15 导入），`bandSummaryRequest(material, budget)`（:88-95）；`boundedSummaryGist(gist, budget)`（:114-124）。
- 预算来源只有一条：`Execute` 在 compress_consumer.go:218 已取 `window`，:249 调用处把 `window.SummaryBudgetBytes` 透传给 `summarizeProduct`（:275-291），后者再交给 gist 面 → `summaryGists.SummaryGist(ctx, session, material string, budget int)`（包内私有面，实现在 summary_gist.go:57、替身在 summary_gist_test.go:34）。
- plan 侧预算构造：assembly/age_fold.go:171-175、assembly/compress_consumer.go:230-233 两处 `AgeFoldPlan{...}` 加 `SummaryBudgetBytes: window.SummaryBudgetBytes`（生产判与执行两侧同窗，保证判定与执行的界一致）。

### D3.5 golden 重分类（class/kind 约束已实证）

- 校验器（scripts/gates/check_window_bounds.py:152-178）实测约束：`constant` 类必须 `kind: "const"`、必须在声明文件里真有该 const、且不许是声明键；`policy` 类必须 `kind: "config_key"` 且必须出现在 `agentKeys`。`KNOB`（:38）含 `budget_`（仅排除 `budget_seconds`），`FAMILY`（:73）含 `budget`——故 `summary_budget_bytes` **必须**登记 policy，`DefaultSummaryBudgetBytes` **必须**登记 constant，否则门禁红。
- 改动（golden/runtime/window_bounds.json）：
  - 新增 `summary-budget-bytes`（class `policy`，kind `config_key`，file api/config/decode.go，source「declared, defaults to DefaultSummaryBudgetBytes」，reason 写「策略面：老带摘要正文的字节上界，可调」）。
  - 新增 `summary-budget-default`（class `constant`，`DefaultSummaryBudgetBytes`，api/config/defaults.go，reason「策略缺省值。」）。
  - 改写 `age-summary-text-limit`（:108-114）reason：由「机制常量：一份 history summary 正文的字节上界」改为「策略缺省值：折叠侧读不到预算时的兜底界；界本身由 summary_budget_bytes 声明」。
- 门禁接线既有（scripts/gates/lint.sh:133-134），无新门。

## 二 触碰集与文件清单

| 文件 | 改动 |
|---|---|
| api/config/config.go | `AgentSpec` 加 `SummaryBudgetBytes int` 及其契约注释（:82-90 段） |
| api/config/decode.go | `agentKeys` 加 `summary_budget_bytes`（:158-165）；`decodeAgent` 加 `countField`（对照 :208） |
| api/config/defaults.go | `DefaultSummaryBudgetBytes` 常量 + decode 填缺省（对照 :24/:27、:40/:43） |
| assembly/age_fold.go | `FoldWindow.SummaryBudgetBytes`（:25-33）；plan 构造（:175） |
| assembly/request.go | `foldWindowOf`（:606）取 spec 字段；`normalizeFoldWindow`（:596）补缺省 |
| assembly/summary_gist.go | 指令按预算拼（:40-43）、`bandSummaryRequest(material, budget)`（:88-95）、`SummaryGist(..., budget)`（:57-84）、`boundedSummaryGist(gist, budget)`（:114-124）、租约 + 声明采样取模型 |
| assembly/compress_consumer.go | `summarizeProduct` 加预算形参（:275-291）；两处 plan 构造（:230-233）；调用处透传（:249） |
| memory/replay/age_segments.go | `AgeFoldPlan.SummaryBudgetBytes`（:413）+ `summaryBudgetOf`（并列 :474-478）；`historySummaryOf` / `ageSummaryTextOf` 改形参（:603/:641）；调用点 :509；常量注释改写（:94-100） |
| memory/replay/age_plan.go | `planFold` 透传预算（:115） |
| golden/runtime/window_bounds.json | 新增 policy + constant 两条，改写 `age-summary-text-limit` reason（:108-114） |
| docs/wiki/agent/event-flow.md:29 | 2048 与 `DefaultAgeSummaryTextLimit` 的叙述改为「声明预算、缺省 2048」 |

生成物 `docs/api/github_com__SpellingDragon__tagent__memory__replay.md:95-101` 由文档生成器重跑产出，不手改。

**与 04 合单的同文件界**：本域在 memory/replay/age_segments.go 只动 `AgeFoldPlan`（:413）、`historySummaryOf`（:603-619）、`ageSummaryTextOf`（:636-674）与调用点 :509；`foldProcessDetail`、`historySummaryLine`(:629)、`ticketText` 与中段呈现全归 04。

## 三 风险与回退

- **极小预算退化**：预算小于 `ageSummaryDropReserve`（:102，64 字节）+ 行连接符时，机械侧只剩「+N earlier entries covered by key」标记加一行截断；结论仍成立（正文 ≤ 预算、折叠不因预算小而失败），但信息退化明显。声明 `> 0` 直接采用（不静默抬到缺省），`<= 0` 读缺省；行为以 spec 场景钉住。
- **过大预算**：正文随声明膨胀，但折叠触发仍由 `VolumeThresholdBytes`（age_fold.go:48）与 `replay` 触发阈值决定，膨胀只会让下一轮折叠更早发生，不产生「无限摘要」。
- **判定/执行界不一致**：两侧 plan（age_fold.go:175、compress_consumer.go:230）必须取同一 `window.SummaryBudgetBytes`；漏一侧则同一材料重复折叠产出不同正文，属缺陷（3.2 的判/执一致用例钉住）。
- **租约改动误伤降级**：无租约回 native，`ErrNoSummaryModel` 三条降级（summary_gist.go:57-84、compress_consumer.go:280-283）口径不变，既有 `TestBandSummaryGistReportsAMissingModel`（summary_gist_test.go:91）保持绿。
- **端口签名变更**：`summaryGists` 是 assembly 包内私有面，唯一实现在 summary_gist.go、唯一替身在 summary_gist_test.go:34；无跨包消费者。
- **golden 误伤**：新条目按 D3.5 的 class/kind 约束登记，`python3 scripts/gates/check_window_bounds.py` 当场复跑（不得新增 auto-write）。
- **回退**：删 `summary_budget_bytes` 声明与字段、`summaryOptOf` 回常量、提示词回字面量、golden 两新条目撤销、`age-summary-text-limit` reason 复原；摘要模型面回裸 native。回退后「界不可配置」症状复红（3.1/3.2 用例退出）。
