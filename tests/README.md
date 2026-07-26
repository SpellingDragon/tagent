# tests/ — 跨包集成与端到端测试

本目录只收**跨包黑盒**测试（`package tagent_test`，仅经导出 API）。**首要职责：用真实 LLM 守护"模型↔框架"文本契约**——工程侧单测锁不住模型侧的抄写行为（event_keys hex 断裂曾静默存活多日，实机 18/18 调用 event_keys=0，教训）。

## 契约守护矩阵（真实 LLM）

| 契约接缝 | 框架产出（生产模板锚点） | 模型职责 | 守护测试 |
|---|---|---|---|
| 时间线前缀 → event_keys | `[evt_HEX\|type]`（event.FormatEventPrefix） | 抄 hex key 给子 agent 工具 | `event_keys_llm_test.go` |
| 卡片/归档票据 → memory_recall | 卡片行 `[HEX]`、`摘要 key=HEX`（compress） | 抄 hex 构造 items | `TestContract_CardTicket_ToMemoryRecall` |
| settle 通知 → get_task_result | `(task id: xxx)`（event_bus） | 抄 task id 查全量 | `TestContract_TaskSettledID_ToGetTaskResult` |
| ACK → resume_task | `已在后台运行 (task xxx)`（tool_agent） | 抄 task id + 续跑指令 | `TestContract_AckTaskID_ToResumeTask` |
| 原生 tool 历史 → 无伪调用 | assistant ToolCalls + role=tool 配对 | 发起真实 ToolCall,文本零调用语法 | `TestContract_NoTextualToolCallImitation` |
| cwd 语义 → 命令路径 | fresh-shell 声明（action_tool_desc） | 不假设 cd 跨调用保持,根路径出命令 | `TestContract_ActionCwdFreshShell` |
| plan 写入边界 | save_file 沙箱(base_dir=openspec)+prompt | 产出收敛进 changes/<plan>/,不越界写他处 | `TestContract_PlanWriteBoundary` |

工程侧（解析/回补往返）由 `agent/event_keys_contract_test.go` 等同包契约测试锁定；两层合一才是完整守护。**契约文本样例与生产模板同步锚定**——模板改动会使这里失败，即提示同步（这是特性不是缺陷）。

## 其余测试职责

| 类型 | 文件 | 职责边界 |
|---|---|---|
| 真实 LLM 机制流转 | integration/async_task_e2e/async_result_delivery/edge_case/plan_agent_* | 压缩循环/异步 settle/子 agent 完成等机制端到端 |
| mock 机制回归 | causal_chain/compression/inject_bus/invariants/multi_user/async_result_routing/tagent_integration | 确定性时序与不变量（I1-I4）,不承担模型行为守护 |
| 白盒单元 | 各业务包内 `*_test.go` | 私有状态机中间态（勿迁入本目录,勿为迁移导出内部符号） |

运行：`go test ./tests/`（真实 LLM 需 `TENCENT_API_KEY`；`-short` 全部跳过 LLM 用例）。契约套件单跑：`go test ./tests/ -run 'TestContract_|ModelCopiesHex' -v`。
