# tests/ — 跨包集成与端到端测试

本目录只收**跨包黑盒**测试（`package tagent_test`，仅经导出 API）。**首要职责：用真实 LLM 守护"模型↔框架"文本契约**——工程侧单测锁不住模型侧的抄写行为（event_keys hex 断裂曾静默存活多日，实机 18/18 调用 event_keys=0，教训）。

## 契约守护矩阵（真实 LLM）

| 契约接缝 | 框架产出（生产模板锚点） | 模型职责 | 守护测试 |
|---|---|---|---|
| 时间线前缀 → event_keys | `[evt_HEX\|type]`（event.FormatEventPrefix） | 抄 hex key 给子 agent 工具 | `event_keys_llm_test.go` |
| 卡片/归档票据 → memory_recall | 卡片行 `[HEX]`、`摘要 key=HEX`（compress） | 抄 hex 构造 items | `TestContract_CardTicket_ToMemoryRecall` |
| settle 通知 → memory_recall | `[evt_HEX\|external_input]` 前缀 + 全文结果（event_bus） | 抄 evt key 召回原文 | `TestContract_TaskSettledTicket_ToMemoryRecall` |
| ACK → resume_task | `已在后台运行 (task xxx)`（tool_agent） | 抄 task id + 续跑指令 | `TestContract_AckTaskID_ToResumeTask` |
| 原生 tool 历史 → 无伪调用 | assistant ToolCalls + role=tool 配对 | 发起真实 ToolCall,文本零调用语法 | `TestContract_NoTextualToolCallImitation` |
| cwd 语义 → 命令路径 | fresh-shell 声明（action_tool_desc） | 不假设 cd 跨调用保持,根路径出命令 | `TestContract_ActionCwdFreshShell` |
| plan 写入边界 | save_file 沙箱(base_dir=openspec)+prompt | 产出收敛进 changes/<plan>/,不越界写他处 | `TestContract_PlanWriteBoundary` |

工程侧（解析/回补往返）由 `agent/settle_routing_test.go` 等同包契约测试锁定；两层合一才是完整守护。**契约文本样例与生产模板同步锚定**——模板改动会使这里失败，即提示同步（这是特性不是缺陷）。

## 其余测试职责

| 类型 | 文件 | 职责边界 |
|---|---|---|
| 真实 LLM 契约套件 | `contracts_llm_test.go`（`TestContract_*` 系列）/ `event_keys_llm_test.go` | 上表七条「模型↔框架」文本接缝的守护主体 |
| 真实 LLM 机制流转 | `integration_test.go` / `async_task_e2e_test.go` / `async_result_delivery_e2e_test.go` / `edge_case_integration_test.go` / `plan_agent_bug_test.go` / `plan_agent_create_behavior_test.go` / `plan_reentry_llm_test.go` / `mcp_llm_test.go` | 压缩循环/异步 settle/子 agent 完成/边界场景/plan 行为与重入/MCP 发现-调用闭环等机制端到端 |
| mock 机制回归 | `causal_chain_test.go` / `compression_test.go` / `inject_bus_inputs_test.go` / `invariants_test.go` / `multi_user_dispatch_test.go` / `async_result_routing_test.go` / `tagent_integration_test.go` | 确定性时序与不变量（I1-I4），不承担模型行为守护 |
| 白盒单元 | 各业务包内 `*_test.go` | 私有状态机中间态（勿迁入本目录，勿为迁移导出内部符号） |

运行：`go test ./tests/`（真实 LLM 经 `testutil.LoadAPIKey` 读 `ZAI_API_KEY`——GLM Coding Plan，端点 `open.bigmodel.cn/api/coding/paas/v4`；`-short` 全部跳过 LLM 用例。例外：`hy3_thinking_test` 为混元专属测试，用 `TENCENT_API_KEY`）。契约套件单跑：`go test ./tests/ -run 'TestContract_|ModelCopiesHex' -v`。

## Soak 连续性测试（-tags soak，implementation-hardening 8.3）

「连续运行数天不失忆」的长程证据生成器：N 轮「写事件→Close（持久屏障）→新实例重建→断言 round-0 仍可召回」，参数化轮数与每轮事件量：

```bash
go test ./tests/ -tags soak -run TestSoak_Continuity -count=1 -v -args -rounds=30 -events-per-round=30
```

CI 经 workflow_dispatch 手动触发（ci.yml soak job）。默认套件不含（build tag 隔离）。

## 框架侧契约守护（真源锚点 + 运行门）

上表守的是「模型↔框架」的文本接缝；下面几条守的是**框架自身**的接缝——不需要模型参与也能判对错，因此放在同一目录但单列一表。下表的守护测试名以 `tests/` 内的真实函数名为准（并发落名波已到位，逐行对过）。

| 契约 | 真源锚点 | 守护测试 | 运行门 |
|---|---|---|---|
| 提交闸端到端（票据／投影／因果游标只在 `StoreEvent` 成功后发布，写失败一并撤回） | [MemoryPlugin 提交闸](../docs/wiki/plugin/plugin-architecture.md#commit-gate) | `TestCommittedFacts_ProjectionAndRecall` | `go test ./tests/` |
| 决策采集 v2（SDK 请求保真、字节上界、丢失账本、封账四条件） | [决策采集](../docs/wiki/rl/rl-architecture.md#trajectory-capture) | `TestDecisionCapture_EndToEnd`（归因跨 runner 另见 `TestDecisionCapture_FrameworkAttribution`） | `go test ./tests/` |
| 训练事实授权导出（授权先于读取、逐条二次核验、manifest/SHA 可对账） | [授权导出](../docs/wiki/rl/rl-architecture.md#training-export) | `TestTrainingCapture_OfflineDataset` | `go test ./tests/` |
| 真实模型四场景（运行时热参／完整请求预算／决策采集／离线 SFT） | [完整请求预算](../docs/wiki/agent/agent-architecture.md)、[维度分类](../docs/wiki/platform/org-hot-reload.md#restart-required-dimensions) | `TestRealModel_RuntimeOverrides` / `_RequestBudget` / `_DecisionCapture` / `_OfflineSFT` | **必须** `TAGENT_REQUIRE_REAL_MODEL=1`：未设门时该族整批 SKIP 且**不计入通过**（缺样本、零调用、SKIP 都不算数） |

真实模型门与验收器（把上面四场景的 `-json` 输出与采集目录、导出事实、数据集产物一次核账）：

```bash
TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -count=1 -json   -run '^TestRealModel_(RuntimeOverrides|RequestBudget|DecisionCapture|OfflineSFT)$'   > "$TAGENT_ACCEPTANCE_DIR/go-test.json"

python3 scripts/verify_runtime_acceptance.py   --gojson "$TAGENT_ACCEPTANCE_DIR/go-test.json"   --capture-dir "$TAGENT_ACCEPTANCE_DIR/capture"   --facts "$TAGENT_ACCEPTANCE_DIR/facts.jsonl"   --manifest "$TAGENT_ACCEPTANCE_DIR/facts_manifest.json"   --dataset-dir "$TAGENT_ACCEPTANCE_DIR/dataset"   --expect-tests 'TestRealModel_(RuntimeOverrides|RequestBudget|DecisionCapture|OfflineSFT)'   --report "$TAGENT_ACCEPTANCE_DIR/acceptance.json"
```

可选参数：`--facts-manifest`（导出快照的独立 manifest）、`--samples`（默认 8）、`--seed`（默认 0）。核账器输出**具名检查**清单（条数随输入形态在 20 上下浮动，确切数由报告里的 `counts.checks` 给出，不在文档里硬编码：gojson 解析／必需测试存在-未跳-通过／真实模型面洁净／采集目录与 manifest 封账／账本认领／事实快照与 manifest 摘要对账／转换账本与产物一致／数据集非空／样本形状一致／分组划分互斥／拒绝账本存在且逐行有理由）。**它只核"已经发生过的运行"，不代跑任何场景**。
