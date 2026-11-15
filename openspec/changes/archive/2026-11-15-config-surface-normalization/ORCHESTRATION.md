# 配置面归位 编排文档

> 派发唯一依据。每次派发前重读二节四问；单完/波后回填四节台账。
> 三者边界：`specs/orchestration/spec.md`=可验收编排契约；`design.md`=静态决策（D1–D7，乙已裁定）；本档=驱动方式+当场账。
> 证据分级：✅ 实测（读码/grep）· ⚠️ 提及未确认 · ❓ 推断。

## 一、结论

**抵达 DoD 的唯一路径**：W0 症状锚红 + 键名契约冻结 → W1 主线程串行原子（rename 双层 schema + 输出限经 decorate 真发 + reserve 派生自有效输出限 + 预算门唯一来源乙 + 删 params + window_bounds.json 重分类 + 测试归位）→ W2 三域并行（GLOSSARY/spec/门教义、远端 yaml 必显式 context_window、godoc/docs）→ W3 F 收尾（契约面恰为 design 所列 + 全量门 exit 0 + S1 红转绿两次证据 + 归档），**停于 commit/push 法定停点等用户明示授权**。

**当前最大风险**：rename 消费者面宽（✅ grep：`MaxTokens` 55 文件上界，须区分 AgentSpec 回放窗／GenerationConfig 输出限／FoldWindow 折叠窗三类语义；`ProviderContextWindow` 13 文件；`OutputReserve` 族 8 文件）且 `check_window_bounds.py` 强制「恰一个 window 类」——漏改注册表或门类即红。故 W1 **必须主线程串行原子**，每子步 `go build ./...` 保持可编译，不派并发 agent 避免漏改。

## 二、编排四问（MECE）

### 2.1 次序

就绪孙任务表（派发原子=孙任务集合，R28）：

| 单号 | 域 | 孙任务 | 前置 | 触碰集（摘要） | 状态 |
|---|---|---|---|---|---|
| **P0** | W0 启动门 | grep 实证触碰集 · S1/S4 行为红锚 · S2 现状锚 · 键名契约冻结 | design 审结（乙） | `assembly/request_test.go`（新锚）+ design 契约节 | **ready** |
| **P1** | W1 01+02 合单 | 1.1 rename · 1.4 注册表 · 1.2 输出限真发 · 1.3 reserve 派生+门乙 · 2.1 params 删 · 1.5 测试归位 | P0 红+冻结 | .go 簇 + `window_bounds.json` + 测试（见三） | blocked by P0 |
| **P2a** | W2 03 | 3.1 GLOSSARY · 3.2 spec MODIFIED · 3.3 门教义核 | P1 键名落地+门绿 | `docs/GLOSSARY.md` + spec + `check_window_bounds.py` docstring | blocked by P1 |
| **P2b** | W2 04 | 4.1 远端 yaml 新键+必显式 context_window | P1 键名落地 | `examples/wechat-bot/tagent.yaml`、`tagent.rl.yaml` | blocked by P1 |
| **P2c** | W2 05 | 5.1 godoc 重生成+CHANGELOG/README/wiki | P1 落地 | `docs/api/*.md` + CHANGELOG + README + wiki | blocked by P1 |
| **P3** | W3 收尾 | F1 契约面一致 · F2 全量门 · F3 DoD+S1 红转绿 · F4 归档 | P2a/b/c 全绿 | 全仓门 + 归档 | blocked by P2 |

- **并发准入（R21 触碰集不相交）**：P1 触碰集自相交（.go 簇 + 门强耦合）→ **主线程串行原子，不派并发 agent**。P2a/P2b/P2c 触碰集互不相交（文档/spec ⊥ yaml ⊥ godoc/docs）→ P1 绿后**三 agent 并行**。
- **关键路径**：P0 → P1 → P3（红）。P2 三域并行不在关键路径（但 P3 须其全绿）。
- **本轮做不了什么**：P1 须 P0 红锚+契约冻结先落；commit/push/PR 须用户明示授权（P3 DoD 过后停，法定停点）。

### 2.2 授权

每单白名单到文件见 §三。共享面归属（单写）：

| 共享面 | 归属单 | 理由 |
|---|---|---|
| `golden/runtime/window_bounds.json` | P1 | 门强耦合，rename 同原子单 |
| `api/config/*.go`、`assembly/*.go`、`runtime/agent/*.go` | P1 | .go 簇单写 |
| `docs/GLOSSARY.md`、`openspec/specs/request-budget-accounting/spec.md` | P2a | 文档/spec 单写 |
| `examples/wechat-bot/*.yaml` | P2b | 远端声明单写 |
| `docs/api/*.md`、CHANGELOG、README、wiki | P2c | 生成物/消费者文档单写 |
| 一级 `tasks.md`、本档台账 | 编排者（主线程） | 账目单点归属（R17） |

**偏离分级**：
- **预授权**（W0 裁定，实施直接执行）：
  - `FoldWindow.MaxTokens` 内部字段**保留**（age_fold.go:25-33，VolumeThresholdBytes 读 w.MaxTokens 不读 spec）；只改 `foldWindowOf`（request.go:611）读取源 `spec.MaxTokens→spec.ReplayWindow` + 注释文字归位。理由：用户裁定范围=配置面声明键，FoldWindow 是 assembly 内部运行时结构非配置面；门不扫内部字段；改它扩触碰集到 20+ 测试构造属变更面扩张（§五）。
  - `GenerationConfig.MaxTokens` **保留**（provider 输出限本义；新输出限字段经 decorate 填它）。
  - `ToolRef.Params`/`Refs[].Params`（string map）**保留**（独立字段，✅ grep 实证 presets/tools/criteria/decode:617/clone:108/fingerprint:385，与 AgentSpec.Params 无关）。
  - 测试 fixture 机械 rename；godoc 重生成。
- **须上报**（法定停点或重大取向）：任何超出 design §四裁定的语义改动；发现 design 清单外消费者→记台账+按实证修（已发生：FoldWindow 在 assembly 非 memory/replay、defaults.go 是折叠缺省非预算推导、age_fold.go 字段保留）；**commit/push/PR**（须用户明示）。
- **禁止**：触碰外来未跟踪五件（`runtime-architecture-convergence/`、`check_event_semantics_symptoms.py`、`runtime_budgets.py`、`test_check_runtime_symptoms.py`、`skills/interview/agent-interview-helper/`）；改写冻结归档（`archive/**` 只附勘误）；触 §五冻结拼写（reincarnation 族，预算字段不在其中✅已核）；agent 禁 git commit/checkout。

### 2.3 凭据

五查落点按单（复跑命令为唯一判定，非汇报）：

| 单 | 查1 勾选真实性 | 查2 产物盘点 | 查3 数字溯源 | 查4 遗漏 | 查5 生产消费者实测 |
|---|---|---|---|---|---|
| P0 | S1/S4 红锚 `go test -run` 具名=FAIL（行为红非编译错） | 新锚落盘 grep | 96407/96000/128000/32000 | S1/S4 写就 | S1 调真实 `assemble` 入口非 helper |
| P1 | 每子步 `go build ./...` exit 0 | grep 残留分类核验 | 门 `windows==1` exit 0 | 1.1–1.5/2.1 逐勾 | S1/S2/S4 绿 + `foldWindowOf`/`withEffectiveConfig`/`loadHostDocument` 生产入口 |
| P2a | doc 门 + `openspec validate --strict` | grep 旧键零残留（归档除外） | — | 3.1–3.3 | GLOSSARY 行与 window_bounds.json 类一致 |
| P2b | `loadHostDocument` strict 解码 exit 0 | yaml 新键落盘 | context_window=128000 | 4.1 | 远端 smoke 入口真装配+门开 |
| P2c | doc 门绿 | godoc 重生成 | — | 5.1 | grep `output_reserve\|provider_context_window` docs/examples 零残留 |
| P3 | F1–F4 逐项复跑 | git diff 契约面 | S1 回滚红/修复绿 | F 全勾 | 全量门 exit 0 |

**必亲验数字清单**（编排者 grep/复跑，R7）：
- S1：reserve `32000→0`、budget `96000→128000`、输入 96407 `拒发→放行`。
- S2：声明输出限 8000 → `GenerationConfig.MaxTokens=8000` 真发 + reserve=8000。
- S4：只 replay_window=64 无 context_window → 门闭整包发（乙）。
- 门：`check_window_bounds.py` exit 0，`windows==1`（replay_window=window 类）。
- grep：`AgentSpec.MaxTokens`/`ProviderContextWindow`/`OutputReserve`/`AgentSpec.Params` 零残留；`GenerationConfig.MaxTokens`/`FoldWindow.MaxTokens`/`ToolRef.Params` 保留。

**复跑命令**（build/test 与 commit 永不串联）：
```
go build ./...
go test ./api/config/... ./assembly/... ./runtime/agent/... ./memory/replay/...
go test ./tests/integration/... ./examples/wechat-bot/...
python scripts/gates/check_window_bounds.py
bash scripts/gates/check-openspec.sh
```

### 2.4 释放

锁/占用者/释放条件/等待表（释放=触发，当场补派，R18）：

| 锁 | 占用者 | 释放条件 | 等待表（释放后谁消费什么） |
|---|---|---|---|
| **键名契约**（replay_window/context_window/max_tokens 输出限/reserve 派生/删 output_reserve+divisor+params） | P0 冻结 | P1 键名落地 + 门绿 | P2a（GLOSSARY/spec 用新键）、P2b（yaml 用新键）、P2c（godoc 用新键）→ **P1 绿当场派 P2a/b/c 三并行** |
| **W1 .go 簇可编译态** | P1 主线程 | P1 全子步 build+test 绿 | P3 F2 全量门 → **P1 绿当场进 P3 前置** |

- 无设备/配额锁（纯本地 Go，无付费资源）。
- **释放即消费**：P1 绿 → 当场派 P2a/b/c（不「下一波再说」）；P2 全绿 → 当场进 P3。波次收尾是记账不是停点（R31）。

## 三、明细

### 派发单-孙任务矩阵（白名单到文件，✅ grep 实证触碰集）

**P1 白名单**（.go 簇 + 门 + 测试；实施时逐文件区分 MaxTokens 三类语义）：
- `api/config/`：config.go（字段定义+注释）、decode.go（agentKeys:158-165 + countField:205 + params:199 删）、fingerprint.go（canonical tag:68 + 映射:212 + Params:210 删 + 注释）、clone.go（Params:58 删）、defaults.go（a.MaxTokens:22→ReplayWindow + 注释:9-11）、skeleton_assert.go:44、config_test.go、fingerprint_test.go（Params fixture:142/143/300 改 Temperature）
- `assembly/`：host_document.go（hostAgent yaml:108-110 + mapDocument:198-200）、request.go（budgetWindow:853 / budgetReserve:865 / foldWindowOf:611 / 注释）、host_model.go（decorate:121 填输出限）、host_reload.go（note params:719 删；replay_window 标签:721 已存在）、facts.go（核 MaxTokens 语义）、skeleton_assert.go（FoldWindow:128 保留）、request_test.go（S1/S2/S4 + TestProviderWindowGateSemantics:760 + TestBudgetDefaultOutputReserve:801 删改 + TestFoldWindowSemanticsUnchanged:903 回归锚）、facts_test.go（FoldWindow 构造保留）
- `runtime/agent/`：request_assembly.go（OutputReserveOf/DefaultOutputReserveDivisor 删 + WithBudget:103 + inputBudget:154）、loop.go（withEffectiveConfig:929）
- `golden/runtime/window_bounds.json`（重分类 D5）
- `scripts/gates/check_concept_budget.py`（**实施期新枚举消费者，R11 计划缺口**：AgentSpec struct-fields 棘轮 26→24 + 注释归位；字段数耦合 .go 删，归 P1 原子单，非 P2a）
- `tests/integration/`：history/event_integrity_test.go、journey/{journey_harness,journey_fold}_test.go、host/{projection_assembly,compress_pipeline,age_fold}_test.go（ProviderContextWindow 构造）+ 其余命中 AgentSpec 预算字段的 *_test.go
- `examples/wechat-bot/smoke_test.go`（AgentSpec 预算字段改；FoldWindow:2192/2393/3256 构造保留）
- **不改**：age_fold.go（FoldWindow.MaxTokens 字段+VolumeThresholdBytes 保留，仅注释文字归位）、memory/replay（✅ 零 MaxTokens 命中）

**P2a 白名单**：docs/GLOSSARY.md（§一请求预算行）、openspec/specs/request-budget-accounting/spec.md（MODIFIED）、openspec/specs/config-hot-reload/spec.md（若涉指纹键名）、scripts/gates/check_window_bounds.py（docstring/KNOB 教义核）、scripts/gates/test_check_window_bounds.py（门自测）。

**P2b 白名单**：examples/wechat-bot/tagent.yaml、examples/wechat-bot/tagent.rl.yaml。

**P2c 白名单**：docs/api/*.md（godoc 重生成）、CHANGELOG.md、examples/wechat-bot/README.md、docs/wiki/platform/org-hot-reload.md。

### 合单判据（R21/R28）
- **P1=01+02 合单原子**：触碰集相交（config.go/decode.go/fingerprint.go/clone.go）+ rename 与 window_bounds.json 门强耦合（漏改即红）→ 合单串行，不可并行派发。内部子步序（R1 更正）：**1.1 rename → 1.2 输出限 → 1.3 reserve 派生+门乙 → 2.1 params 删 → 1.4 注册表（门绿）→ 1.5 测试归位**；每子步 `go build` 保持可编译。更正因：门 `check_window_bounds.py` 扫**活键集**（decode agentKeys + host yaml 标签），`max_tokens` 输出限键 1.2 才生、`output_reserve` 1.3 才删，故 D5 注册表终态（replay_window=window、context_window+max_tokens=budget、删 output-reserve/divisor）须待键集落定方可 windows==1 全注册；原置 1.4 于第二步「门绿」不可达（✅ 实证：1.1 后门红四误＝max_tokens/provider_context_window 陈旧 + context_window/replay_window 未注册）。**门绿是 W1 完成判据，build 绿才是每子步判据**；注册表一次性原子改于 1.4，不每子步追改。
- **P2a/b/c 并行**：触碰集互不相交且都在 P1 键名落地后（R22 共享面已冻结）。

### 预算
- P1 主线程串行（rename 宽面 ~55 文件上界，派 agent 易漏改，主线程可控）；P2 三 agent 并行；P3 主线程。无付费/设备资源。

### 档位与真实路径欠账
- 远端 wechat-bot 是唯一部署档；smoke 入口=2×回放窗解耦意图保留（D5/D4）。
- ⚠️ c75 各输入面占比需 c75 轨迹（宿主日志只到 #c51）；S1 隔离复现不依赖它。
- ❓ 折叠目标数学（transcript_Content>4 时折叠徒劳）读码推导，未对 c75 运行时确认——本期不改折叠逻辑，只 rename。
- model 级容量注册表（C5 完整形态）留未来（D7），本期 agent 级 context_window。

## 四、台账

| 轮 | 单 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|---|
| R0 | 规划 | design 审结乙 | — | D4=乙；D7「甲」残留勘误→乙 | proposal/design/tasks 落盘 grep✅ | FoldWindow 在 assembly/age_fold.go 非 memory/replay；defaults.go:22 是折叠缺省非预算推导 | — |
| R0 | W0 触碰集裁定 | FoldWindow.MaxTokens 保留 | grep 实证✅ | 配置面范围裁内部字段保留；GenerationConfig.MaxTokens/ToolRef.Params 保留 | ProviderContextWindow 13 文件/OutputReserve 8/MaxTokens 55 上界/Params 精确（AgentSpec vs ToolRef） | foldWindowOf:611 唯一映射点改读源；age_fold.go 仅注释归位 | 契约待 P0 冻结 |
| R0 | 编排产物 | 本档落盘 | — | S2 红锚时点随 1.2 新字段诞生（W0 先钉 served.MaxTokens==nil 现状） | ORCHESTRATION+二级子变更+orchestration spec | W0 须先写 S1/S4 行为红锚（现有字段可表达） | — |
| R0 | W0 启动门 | S1/S4 行为红锚立起，红因命中语义非编译 | `go test ./assembly -run 'TestSymptomAnchor(PhantomReserve\|ReplayWindowAlone\|OutputLimitUnserved)' -count=1 -v`：S1 FAIL/S4 FAIL/S2 PASS✅ | 键名契约冻结于 design/ORCHESTRATION（ReplayWindow/ContextWindow/输出限） | S1 红=「96413 input tokens over budget 96000 (window 128000, reserve 32000) no degradable material」直译 c75；S4 红=「window 64, reserve 16」derived 门开；S2 绿=served.MaxTokens==nil 现状 | P1 1.3 改 reserve 派生后 S1 转绿、1.1+1.3 门乙后 S4 转绿、1.2 输出限诞生后 S2 翻红绿 | 锁1 键名契约 P0 冻结→P1 消费 |
| R1 | W1 1.1 rename | 双层 schema+全 .go 消费者改名落盘，甲行为保全 | `go build ./...`=0、`go build -tags smoke ./...`=0、`go vet ./...`=0、`go vet -tags smoke`=0；`go test ./api/config/...` ok；`go test ./assembly/...` 仅 S1/S4 FAIL（W0 锚），S2/S3/TestProviderWindowGateSemantics/TestBudgetDefaultOutputReserve 绿✅ | ①子步序更正：1.4 移至 2.1 后（门扫活键集，D5 终态须键集落定，详§三）②输出限字段名**收回 `AgentSpec.MaxTokens`**（yaml `max_tokens`），spec→hostModelParams→decorate→GenerationConfig.MaxTokens 一词贯串（design 1.2「或收回名」授权内，最纯统一相似词） | 60 .go 改动；`grep ProviderContextWindow --include=*.go` 零残留（22 处皆 docs/planning/frozen-archive）；MaxTokens 仅余合法保留（FoldWindow/GenerationConfig/model.Request/relayMaxTokens/checkpoint 持久化）；sectionsFingerprint 迁 `244c345d…`（context_window/output_reserve 无 omitempty 恒编码，键改即迁，非缩水）；门现红四误实证 1.4 须随键集 | 1.2 加 MaxTokens 经 hostModelParamsOf/decorate（host_model.go:34/121）填 GenerationConfig.MaxTokens（*int，Request 内嵌）；生产接线 tools.go:224 贡献模型/542 委派；S2 正锚 host_model_test.go TestEffectiveGenerationConfig（真 decorate）+ request_test.go S2 全链；负锚 undeclared→nil；clone.go `out:=in` 自动覆盖 int 无需显式 | 键名契约 replay_window/context_window 键已活→1.2 消费 |
| R1 | W1 1.2 输出限真发 | AgentSpec.MaxTokens（yaml `max_tokens`）收回为输出限，spec→hostModelParamsOf→decorate→GenerationConfig.MaxTokens 一词贯串；S2 锚红转绿 | 聚焦簇 `TestSymptomAnchorOutputLimitServed` PASS（served.MaxTokens 非 nil）+ `TestEffectiveGenerationConfig`（真 decorate）；BUILD/VET=0✅ | 输出限字段名收回 `MaxTokens`（design 1.2「或收回名」授权，最纯）；GenerationConfig.MaxTokens(*int)/FoldWindow.MaxTokens/relayMaxTokens/checkpoint 持久化 保留 | host_document.go MaxTokens int yaml:107+mapDocument:198；host_model.go hostModelParamsOf 读 spec.MaxTokens | 1.3 reserve 派生消费同一 MaxTokens（单一来源：budget 门在 decorate 前，budgetReserve()==spec.MaxTokens==decorate 所发） | — |
| R1 | W1 1.3 reserve 派生+门乙 | budgetWindow()→spec.ContextWindow（门唯一来源）；budgetReserve()→spec.MaxTokens（与 decorate 同源，无 window 参）；删 AgentSpec.OutputReserve 三层+DefaultOutputReserveDivisor+OutputReserveOf；伪注释（derived from ReplayWindow）换真话；门闭日志去 max_tokens=0 | 全套件 b8nj0pn30 `go test ./assembly/... ./runtime/... ./tests/integration/...`：17 包 ok，唯一红 `TestBudgetRefusalWhenUndegradable`（乙门闭 collateral，request_test.go:1168 provider requests=1 want 0）→加 `spec.ContextWindow=boundaryAnchorWindow` 修复→聚焦簇 8 测试全 PASS（BUDGET_CLUSTER_EXIT=0）；BUILD/VET/CONFIG=0✅ | 内部策略层词汇（requestBudgetPolicy.providerWindow/outputReserve、WithBudget 参、loop 诊断 window=/reserve=/provider window/output reserve）保留为独立语义层（仍准确描述预算留房），仅伪断言改写；门乙=ContextWindow>0 唯一来源 | sectionsFingerprint 迁 `244c345d…`→`f834ae0b…`（删恒编码 output_reserve:0，预期迁移非缩水）；TestBudgetDefaultOutputReserve 删（钉已删 window/4）；TestProviderWindowGateSemantics derived→replayAlone 门闭；grep OutputReserve/output_reserve/OutputReserveOf/DefaultOutputReserveDivisor 全 .go 零残留 | 2.1 删 Params 级联：floatMapField/cloneFloatMap/canonicalFloatMap 各唯一调用方→回卷；**探针实证** canonicalFloat 余消费方皆 omitempty（-0 已被 omitempty 丢弃）→canonicalFloat 冗余死结构，随 Params 删（详 2.1 行） | 门乙键集落定→1.4 注册表消费 |

| R1 | W1 2.1 params 删 | AgentSpec.Params 三层删（config/decode/fingerprint/clone/skeleton）+ 级联 floatMapField/cloneFloatMap/canonicalFloatMap + canonicalFloat 及 3 omitempty 包装 + 负零测试 + host_reload note；skeleton 补 ContextWindow（1.1 漏） | config exit 0（sectionsFingerprint f834ae0b→3a1232e6）；assembly+capabilities b6xmipn61 **0 FAIL 2 ok**（251.9s，读 log 实证非信通知）；runtime 0 FAIL 4 ok；smoke -short 0 FAIL；BUILD/VET/SMOKE=0 | **独立纯度裁定（非用户裁定）**：design D6/2.1 伪前提「改用 Temperature 测负零」纠正——Temperature 裸 *float64 不归一；探针 /tmp/negzero_probe.go 证 Params 删后 canonicalFloat 余消费方皆 omitempty（-0 已丢弃）→删 canonicalFloat 行为保持；已落 design 勘误 | 负锚 `retired_agent_model_params` PASS（agent params 键→unknown field）；消费者锚 grep `.Params` 仅 ToolRef.Params（string map）+ 无关 MCP/go-types；floatMapField/cloneFloatMap/canonicalFloatMap/canonicalFloat 全 .go 零残留 | 1.4 注册表消费已落定键集；**新枚举消费者 check_concept_budget.py AgentSpec 字段数棘轮 26→24**（R11 计划缺口） | — |
| R1 | W1 1.4 注册表重分类 | window_bounds.json D5 重分类（replay_window=window 唯一、context_window+max_tokens=budget、删 output-reserve/divisor 条目、classes 教义乙改写）；check_concept_budget.py:162 AgentSpec 棘轮 26→24 + 注释归位（删 params/output_reserve/capture_budget_mb/provider_context_window 陈旧名） | check_window_bounds **exit 0**（4 类 6 声明 19 常量 68 键，windows==1）；负锚 临时 registry 漏 max_tokens→**exit 1**「not registered」；check_concept_budget exit 0（baselined at 24）+ self-test 56 OK；feature_boundary/schema_allowlist/config_liveness/vocabulary/doc_budget 皆 exit 0 | 保留内部 id provider-window/replay-window（policy 层 providerWindow 词汇保留，id 非声明键）；check_concept_budget.py 归 P1 原子单（字段数耦合 .go 删）；feature_boundary:40 历史例句 + synthetic fixtures（test_check_feature_boundary/test_check_window_bounds）非真消费者、门绿，留 3.3/不动 | 消费者锚 门 scan_config_keys 真扫 68 键含新 agentKeys；window_bounds 零 output_reserve/provider_context_window/DefaultOutputReserveDivisor 残留 | 3.3（P2a）须改 check_window_bounds.py docstring（max_tokens→replay_window、budget 派生→乙）+ test_check_window_bounds.py synthetic fixtures 词汇；远端 yaml max_tokens:128000 语义错（输出限）待 4.1 改 replay_window+context_window | **门绿=W1 完成判据达成→W2 解锁** |
| R1 | W1 1.5 测试归位 | 具名锚 S1/S2/S3/S4 + TestProviderWindowGateSemantics + TestFoldWindowSemanticsUnchanged + TestBudgetRefusalWhenUndegradable 全绿；smoke_harness_test.go:190 乙注释归位（删「不声明按 replay_window」「3/4 输入预算」伪断言，保留 r32 解耦证据） | assembly named anchors exit 0（26.2s）；runtime exit 0；wechat-bot build/vet=0 + smoke -short exit 0（3.7s） | smoke_test.go:4267 symptomExecDocument 模板已 declare replay_window+context_window（无陈旧注释）；其余 derived 引用（fold key/projection/identity）非预算语义，不动 | 正锚 S1/S2/S3/S4 具名绿；负锚 S4 只声明 replay_window→门闭整包（replayAlone）；消费者锚 assembly+runtime/agent 全绿 | W1 全子步完成、门绿；W2 三域并行就绪（键名契约已冻结落地） | W1 完成→W2 P2a/b/c 解锁 |

### W2 三域并行 + 编排者共享面归位（R2）

| 轮 | 单 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|---|
| R2 | P2a 03 门教义/GLOSSARY/spec | GLOSSARY §一请求预算行四分类改写（回放窗 replay_window／预算面 context_window 门唯一来源＋max_tokens 输出限／余量派生自有效输出限／删 window÷4＋divisor）；config-hot-reload 热参名 max_tokens→replay_window（:10/23/28/38/45，FoldWindow.MaxTokens :42 保留）＋:5 复核注记附乙勘误（Purpose 区，非 R4 扫描）；check_window_bounds.py docstring 乙改写＋test fixtures provider_context_window→context_window | spec_hygiene exit 0（violations 284 baseline 286 stale 2 errors 0）；window_bounds exit 0；门自测 test_check_window_bounds 10/10 PASS；vocabulary/doc_links exit 0 | **ratify P2a :10 指针勘误**：原「见 tagent.yaml:123」码坐标 yaml 重写后事实失效＋rename 触发 R4 stale→改语义指针「见 tagent.yaml『待接线』注释」使 :10 R4-clean（去 digit），属 R4 教义对齐（需求散文不该含码坐标）非缩水；stale 2 非失败，留未来 --write-baseline reviewed tightening | 03 tasks 3.1/3.2/3.3 [x]；GLOSSARY grep provider_context_window/output_reserve/window÷4/DefaultOutputReserveDivisor 零残留 | request-budget-accounting 01 delta 待 F4 archive 同步转正（W2 不手改 live spec） | — |
| R2 | P2b 04 远端 yaml | tagent.yaml max_tokens:128000→replay_window:128000（:109）＋必显式 context_window:128000（:110），无输出限声明；:104-108 注释乙改写；4 待接线注释 max_tokens→replay_window；rl.yaml :87 待接线注释归位（无新声明，RL 基线非部署助手）；**:143 已接线列表补 max_tokens（输出限,经 decorate 发 provider）**（编排者 ratify P2b 留白：输出限 W1 已接线，注释如实，4.1「注释如实改写」mandate 内） | schema_allowlist exit 0（generic_keys 归位后）；feature_boundary exit 0；assembly 门锚 TestProviderWindowGateSemantics/TestSymptomAnchorReplayWindowAloneOpensNoGate/TestBudgetReservesOutputAllowance/TestSymptomAnchorOutputLimitServed/TestSymptomAnchorPhantomReserveRefusesFittingRequest 全 PASS（ANCHOR_EXIT=0） | **4.1 verify 锚精化（R29 计划缺口，非缩水）**：原正锚 TestHostDocument 不存在（-run 零匹配空转 exit 0）→改真 assembly 门锚＋smoke boot TestLocalSmokeSymptomBootDeployed；原负锚「max_tokens 作回放窗→unknown field 拒」为伪（max_tokens 已合法输出限键，声明它挤占预算非 decode 拒）→改真预算挤占拒发；已回写 04 tasks＋design §六 D-04 | 04 tasks 4.1 [x]；BootDeployed 环境 FAIL 处置：gitignore .wechat-config/data 退役分片布局 ErrJournalLegacyLayout→移 /tmp/p2b-residue 复跑绿→还原 RESIDUE_IDENTICAL（find diff 空），本地 gitignore 数据非仓内容，已披露 | 远端部署须 git pull＋rebuild＋restart 方读新 yaml（用户侧操作，停点后） | — |
| R2 | P2c 05 godoc/docs | docs/api/*.md 重生成（api__config/assembly/runtime__agent，decorate 段补输出限＝W1 源投影）；CHANGELOG [Unreleased] 新增 config-surface-normalization 四要素条目（+4 行，历史「统一最终请求预算」条目原文保留）；examples/wechat-bot/README :42 replay_window＋context_window＋max_tokens 输出限（行号核实 decode.go:158-165/host_document.go:104-112）；docs/wiki/platform/org-hot-reload :45 指纹三值归位＋预算两值（context_window 无 omitempty 零也编码） | gen_godoc --check exit 0（80 包 docs/api 匹配源）；doc_links exit 0（130 链接 0 错）；git grep -nI output_reserve\|provider_context_window -- docs/ examples/ exit 1 零残留（跟踪源） | **5.1 消费者锚精化（R29）**：原 grep -rn 命中 gitignore 编译二进制（wechat-bot/bench-bridge 等）＋docs/.dev .docx/.zip 超时 exit 2，非源残留→改 git grep（仅跟踪源）精确复跑 exit 1；已回写 05 tasks＋design §六 D-05 | 05 tasks 5.1 [x]（doc 门绿达成后勾，未勾=未缩水）；godoc 无 OutputReserve/Params/canonicalFloat | doc_budget +4（CHANGELOG）须编排者具名上调 | — |
| R2 | 编排者共享面（R17 单写） | 三共享面归位：①golden/runtime/schema_allowlist.json generic_keys 删 provider_context_window/output_reserve 增 replay_window/context_window（max_tokens 保留输出限）＋note 注明乙；②scripts/gates/doc_budget.json 具名上调 baseline 21992→21998（+6：CHANGELOG 四要素+4、orchestration 乙勘误+2，逐项列清循 M4/2026-11-11/2026-11-12 先例「权威文档不得靠删散文对冲」，--tighten 机械拒升故直编 manifest）；③openspec/specs/orchestration/spec.md:483 契约守护需求附乙勘误（boundary-closure 旧「门开⟺provider 窗口∨MaxTokens 为正」被取代，历史 body 原文保留、新勘误行 R4-clean 无 .go/digit/ASCII-paren） | schema_allowlist exit 0（host_documents 2 generic_keys 20 exceptions 1 errors 0）；doc_budget exit 0（21998=21998）；spec_hygiene exit 0（:481 body byte-identical baseline 仍有效，新勘误行零违规）；全 doc+结构门电池绿（godoc/doc_links/vocabulary/feature_boundary/window_bounds/concept_budget/schema_allowlist/spec_hygiene/doc_budget 各 exit 0） | **两 R11 计划缺口**：schema_allowlist.json（宿主文档 agent 键注册表）＋orchestration/spec.md（live 权威 spec 旧预算断言）原 design §七消费者清单漏列→按实证修＋回写 design §七 :190/191（F1「契约改动恰为 design 所列」成立）；orchestration:481 系 boundary-closure 历史 guard 转正 live spec，乙取代其预算语义→附勘误不改写历史 body（裁决取代后文档改写一致） | design §七 :190/191 补两消费者 grep✅；doc_budget note 2026-11-13 条目落盘；orchestration:483 勘误 grep✅ | F1 git diff 契约面须含 schema_allowlist＋orchestration（已 design-listed）；F2 全量门＋race；F3 S1 回滚红/修复绿两次 | **W2 三域全绿＋共享面归位→W3 P3 解锁** |

### W3 收尾 F1–F3（R3）

| 轮 | 单 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|---|
| R3 | P3 F1 契约面一致 | 契约 diff 恰 5 文件（GLOSSARY 2/schema_allowlist 6/window_bounds 28/config-hot-reload 12/orchestration 2，22+/28−），逐处对照 design §四/§六/§七全 listed；request-budget-accounting MODIFIED 在 01 delta（design 3.2），F4 archive 同步转正故 F1 时点 live 未变（非漂移） | `git diff HEAD --stat -- openspec/specs golden docs/GLOSSARY.md`=5 文件；archive/** stat=空（0 字节，唯一 archive 提及是 GLOSSARY 未改动上下文）；§五冻结拼写 +/- 行 grep reincarnation 族→**exit 1 零匹配** | F1 非零 diff 断言成立：无计划外契约漂移；冻结归档＋§五拼写零 diff | run-notes.md F1 表（5 文件→design 落点）；tasks F1 [x] | F4 archive 须同步 01 delta request-budget-accounting 至 live | — |
| R3 | P3 F2 全量门 | build/smoke build/核心测试/集成/远端 build·vet·short 各 exit 0；window_bounds/openspec/godoc/doc_links/vocabulary/feature_boundary/concept_budget/schema_allowlist/spec_hygiene/doc_budget 各 exit 0 | CORE_TEST_EXIT=0（assembly 256.9s）；INTEGRATION_EXIT=0（execution 44.9s 等全 ok）；WECHAT_BUILD/VET/SHORT=0；godoc 80 包；doc_budget 21998=21998；spec_hygiene stale=2 非 error | openspec 2 failed 是外来在途 WIP（event-driven-consumption/post-training-data-loop，not gated）非本变更；doc_budget +6 具名上调循 M4 先例 | run-notes.md F2 表（16 门原始 exit）；tasks F2 [x] | F3 症状锚红转绿 | — |
| R3 | P3 F3 DoD＋S1 红转绿 | **回滚红**：干净 worktree f1c51e87＋基线 API 移植 S1→exit 1，红因「96413 input over budget 96000 (window 128000, reserve 32000)」凭空 window/4；**修复绿**：当前树 S1 PASS，budgetWindow=ContextWindow=128000/budgetReserve=MaxTokens=0→budget 128000→96413 放行；S2/S3/S4＋门语义＋输出余量全 PASS | F3_ROLLBACK_RED_EXIT=1（行为红非编译）；F3_GREEN_EXIT=0（7 具名锚全 PASS）；远端 yaml context_window:128000>0 门开＋干净数据根 Boot exit 0（strict 解码＋全装配＋face split） | **smoke boot 本地环境阻塞归因（非回归）**：ErrJournalLegacyLayout（memory/ledger/config.go:101）撞 gitignore 本地陈旧数据根；三证＝check-ignore exit 0＋diff 不触守卫（grep exit 1）＋红在 strict 解码之后；F3 用 temp yaml 换干净 journal 根隔离（不触用户数据，较 W2 move-aside 更纯），一次性证据测试跑毕即删 | run-notes.md F3 ①②③（两次证据原文＋机理读码 request.go:852/862）；tasks F3 [x] | F4 归档＋commit/push 待用户明示 | **F1–F3 全绿→停于 R31 法定停点「提交归档需明示」** |

**停点（R31）**：F1–F3 全绿，run-notes.md 落盘。F4 `openspec archive`（01 delta 同步）＋commit/push 须用户明示授权，未执行。基线 worktree /tmp/tagent-baseline 为 F3 回滚红一次性证据产物，授权后清理。