# 准出对照表 / Exit & Evidence-Boundary (resident-remaining-hardening 4.2, 4.7, 4.8)

> 本文件是发布候选的**证据边界**记录，不是功能文档。每个「已验」都可复现到具体命令/测试；
> 未满足前置的项一律列「未验」，绝不计入通过。诚实优先于好看。

## 1. 准出闸门（4.2 · 逐命令）

| 闸门 | 命令 | 本机结果（2026-09-18，darwin/arm64） |
|------|------|--------------------------------------|
| 根模块 build | `go build ./...` | PASS |
| 根模块 vet | `go vet ./...` | PASS |
| 根模块 short | `go test ./... -short -count=1` | PASS（real-LLM 用例自跳过） |
| bot 模块 build/vet/short | `(cd examples/wechat-bot && go build ./... && go vet ./... && go test ./... -short -count=1)` | PASS |
| 定向 race | `./scripts/race_check.sh ./memory/... ./agent/... ./evolution/ ./event/ ./tool/... ./plugin/ ./rl/` | PASS（`race_check: OK`） |
| 离线基准 | `RUN_OFFLINE_BENCH=1 go test ./tests/offline_bench/ -run TestOfflineBenchmark` | PASS（数据见 `tests/offline_bench/REPORT.md`） |
| OpenSpec strict | `openspec validate resident-remaining-hardening --strict` + `scripts/check-openspec.sh` | PASS（96 主 spec 全绿） |

**上游豁免（race）**：`race_check.sh` 登记 trpc-agent-go v1.10.0 内部竞态 U2（runner steer-queue/invocation）、U3（inmemory session service），
仅在「每条 DATA RACE 顶帧均命中豁免签名 且 无非-race 失败」时放行；豁免 wrapper 不掩盖普通断言失败（exit-code 保真，3.5）。

## 2. 能力 → 实现 → 测试 → 证据边界（4.7）

| 能力（headline） | 配置开关 | 实现落点 | 测试证据 | 证据边界（未验/降级） |
|------|------|------|------|------|
| 批量退役源头汇总（6.7①/D1） | TaskManagerConfig.OnBatchRetire（可选） | `agent/task` retire 循环 + `agent/agent.go` 注册 | `task_orphan_retire_test.go`、3.2 结算风暴用例 | 远端 24h 回收率未采（1.6 未验） |
| settled 票据化折叠（D2） | compress 折叠判定（恒开） | `agent/compress` foldSettleRuns + `event` settle_fold 类型 | `settle_fold_test.go`（≥80% 回收 + recall 取回）；3.2 折叠卡片断言 | estimator-bias 真实分布已测（1.6，`TRAJECTORY-BASELINE.md`）；D2 上线后回收率>30% 仍待远端复采 |
| 重定向逐跳 allowlist（D3/Major5） | `TAGENT_RL_ALLOW_LLM_REDIRECT` + endpoint allowlist | `rl` EndpointRedirectPolicy 装 http.Client CheckRedirect | `endpoint_redirect_test.go` | 仅覆盖 CheckRedirect 纯函数，未对真实 provider 30x 链 |
| 事件级耐久屏障 / durable inbox-v1（继承） | inbox 配置 | （继承归档 WP0-WP4，两轮 cold-eyes） | 3.2 投递门 + receipt 对账 | 长跑 72h 未验（3.7 BLOCKED） |
| task 全链（spawn→record→registry→settle→feedback→投递） | TaskManagerConfig 钩子 | `agent/task` + `task_record_sink.go` + `persistBusEvent` | **3.3 `task_chain_e2e_test.go`**（inline/bg/unknown/late/resume/lifetime/fold） | stale/deadline/orphan 计时路径在 `agent/task` 单测覆盖（tm.now 注入），非本 E2E |
| 治理/演化/冥想 fail-closed 开关组合 | Enabled/Enforcement/signalsAvailable/novelty-gate | `agent/governance`、`evolution`、`agent/meditation.go` | **3.4 `switch_combo_test.go` ×3 + `meditation_selffeed_test.go`** | 真实模型契约矩阵已验（3.8：deepseek-flash 6/6 PASS，见 §2.1） |
| 票据守卫（浓缩反伪造） | card guard（恒开） | `agent/compress` guardCondensedCard | 2.1-2.4 + **3.6 `card_fuzz_test.go`（accept⇒票据⊆输入）** | — |
| 键解析/事件协议全函数性 | — | `memory.ParseKey`、`event.ParseEventKey`、snowflake | **3.6 有界 fuzz + 原生 target（2.4M execs 无 crasher）** | — |
| lastEventKeys 淘汰因果语义 | maxLastEventKeys=4096 | `plugin/memory_plugin.go` | **3.6 `causal_eviction_test.go`**（淘汰→parent 0，绝不 cross-link） | — |

### 2.1 真实模型契约矩阵（3.8，2026-09-18 授权实跑）

`model_contract_matrix_test.go`（package `tagent`，经 `resolveAgentModel` 走真实 openai-兼容适配器，非手搓 HTTP）。授权门：`DEEPSEEK_API_KEY` 未设 → 整组 `t.Skip`（已验 CI 安全 `ok`，绝不记 PASS）。预算：全程 ≤4 次调用、每次 `max_tokens≤128`、prompt 极短。目标 `deepseek-flash @ https://api.deepseek.com/v1`。

| 契约 | 模式 | 结果 | 观测 |
|------|------|------|------|
| 文本生成 | thinking | PASS | content="2" finish="stop" |
| usage 记账（驱动压缩阈值） | thinking | PASS | prompt=50 completion=48（流式末块带 usage） |
| 流式增量 | thinking | PASS | chunks=49（真分块，非单块） |
| reasoning_content 透传 | thinking | PASS | reasoning len=396，未破坏 content 解析 |
| **原生 tool_calls（ReAct 硬契约）** | 关思考 | PASS | finish="tool_calls"，`get_weather` args `{"city":"北京"}` 合法 JSON |
| **工具结果回环（多轮）** | 关思考 | PASS | 续答 "北京现在天气晴朗，气温约 21°C。"（模型确用上了 tool 结果） |

**关键诚实发现（端点约束，非实现缺陷）**：deepseek-flash 默认 thinking 模式**拒绝强制 `tool_choice`**——实测 `400 "Thinking mode does not support this tool_choice"`。故工具调用契约须在关思考（`thinking:{type:disabled}`）后确定性验证。此点由 `apiErr` 仪表化捕获（初版曾误判 FAIL，冷眼复核纠正）。**选型启示**：thinking/reasoning 模型用于 tagent ReAct 时，若依赖强制工具调用，需评估关思考或改用 auto（auto 下模型可能直接作答，不能确定性保证调用）。

## 3. 未验清单（BLOCKED — 不得计入通过）

| 项 | 阻塞类型 | 缺什么 |
|----|---------|-------|
| 3.7 72h 隔离机器长跑 | needs-authorization | 隔离机器 + 授权长时间运行 |
| 4.1 隔离部署夹具安全验证 | needs-environment | 非 root/只读根/符号链接越界的隔离部署环境 |
| 1.6 chars/token 估值器立项结论 | needs-environment | D2 部署后的远端 24h 生产基线 |

## 4. 发布候选 checklist（4.8）

> 2026-09-18 全量复验：下列「已验」项均以本会话实跑命令为准（见 §1）。

- [x] `openspec validate resident-remaining-hardening --strict` 通过
- [x] `scripts/check-openspec.sh` 主 spec 全绿（96/96）
- [x] 双模块 build/vet/short 通过（CI `.github/workflows/ci.yml` 已扩）
- [x] 定向 race 通过（含 memory 子包/plugin/rl，24 包 `race_check: OK`）
- [x] 离线基准 + Python/race_check 负例验证器全绿（§1）
- [x] 本 change 全部**可执行**任务落地并留证据：批次 A（压缩回收 + Major5）、批次 B（守卫/基准）、批次 C（3.1-3.6）、批次 D（4.2 准出表 / 4.3 文档同步 / 4.4 死代码 / 4.5 可维护性登记 / 4.6 升级回滚夹具+runbook / 4.7 证据边界表）
- [x] 升级/回滚演练夹具 `tests/upgrade_rollback_drill_test.go` `-race -count=3` PASS
- [x] BLOCKED 项显式标注「未验」，未伪造证据
- [x] 实施 diff 两轮独立 CodeReview（非实现者，用户裁定计为独立审查方）：R1 发现并促成修复 1 个修复级并发缺陷（`task_manager.go` batchCollect 解锁 append），R2 确认修复级清零。见 `REVIEW-PACKAGE.md` §7（3.9 通过）
- [ ] **归档同步主 spec（`/opsx:archive`）**——双前置未满足：(a) §3 授权/环境类任务（3.7/4.1/1.6）尚未闭合，change 未达「全部完成」；(b) 归档是**独立的用户授权命令**，specs delta 随归档合入 `openspec/specs/`，不在 `/opsx:apply` 内自动执行
- [ ] 提交 / 推送 dev —— 待用户指示（本会话不擅自提交）
- [ ] tag / 真实部署 —— **待授权**（不在本 change 自动执行）

## 5. 30 天受控部署观察清单（非完成证据，仅观察项）

- 压缩回收率是否达 >30%（对照 24h 基线 72.4% floor）；settled 风暴是否被源头限流 + 折叠兜底。
- **估值器偏差（1.6 已实测，见 `TRAJECTORY-BASELINE.md`）**：真实远端分布下 `DefaultTokenCounter(2.0)` 在大上下文（≥8k tok）**低估 ~11–15%、83% 低估率 → 压缩晚触发、长上下文溢出风险**（非早前假设的「高估→提前压缩」）。观察是否出现 provider 400/上下文超限事件；命中即启动另案立项（**先给 estimator 补 tools-schema 每请求开销建模**——主因候选，再评估内容分型/自适应系数）。
- race 豁免（U2/U3）是否仍只命中上游帧；出现 tagent-owned 帧立即上报。
- lastEventKeys 淘汰是否只降级为 root、无 cross-link 反馈；治理 critical 是否在无审批时恒拦。
- budget-unrepresentable / condensed-tickets-lost 计数是否异常增长（导航地址消亡监控）。
