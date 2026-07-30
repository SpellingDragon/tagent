# plan-interaction-contract

## Why

生产日志（wechat-bot 2026-07-30）实证了顶层 agent 与 plan agent 的交互失控：三次 plan 调用并发抢写同一 change（`init`×15）、`action`/`name` 参数被 `AgentToolWrapper` 静默丢弃致 progress 零 LLM 旁路**从未触发**（日志计数 0，且这是对既有 `plan-agent` 规格 "structured action parameter" 的实现回归）、A 级计划按规格只产 proposal+tasks 却被规格自身要求 `validate --strict` 收尾——实测 strict 与非 strict 均硬性要求 specs deltas，**A 级校验必然失败**，模型陷入修正死角直至迭代耗尽（51>50）；读写 file 工具路径基准不对称（读=cwd、写=openspec/）使模型双向踩错，磁盘留下 `openspec/openspec/` 幽灵目录；父 agent 看到的 `plan_tool_desc.md` 未声明产出物边界，导致 plan 被当执行者派活、越权代工写出 15KB 评估报告、新建 tasks 即全 `[x]`。

根因归纳：**交互契约（身份边界、参数通道、时序闸门、并发语义）没有结构化**，全靠两份互相看不见的 prompt 各自表述。

## What Changes

- **确立"报账–审计"交互模型**：顶层 agent = 任务所有者/执行者/信息桥；plan = 规划师/记账员/审计员，绝不产出工作成果。一个 change 的生命周期绑定一个 task id，create 之后的 update/progress/archive 一律经 `resume_task` 续行（任务链还原器自然携带 change name 与前序上下文）。
- **参数通道结构化（修复规格回归）**：`AgentToolWrapper` 支持经 ToolRef 声明附加参数（plan 声明 `action` enum + `name`），Call 时将附加参数与 request 打包为 JSON 消息体透传；`PlanAgent.extractAction` 既有 JSON 解析路径天然衔接，progress 旁路（含按 `name` 定位）恢复生效。
- **多计划并行成为一等语义**：`name` 贯穿 create 之后的所有 action；plan 侧按 name 定位 change，不再在多个活跃 change 间猜测；create 前先 `list` 盘点、同目标复用。
- **A 级校验收尾改道**：创建流程收尾从 `validate --strict` 改为按级别分派——A 级用 `status --json` 结构自检（proposal+tasks 齐全即合规），B 级维持 `validate --strict`。消除规格自身的 A 级/validate 矛盾（同步修订 `plan-agent` spec）。
- **并发防护**：plan 子 agent 任务的幂等 Key 从 `agentName:request` 改为携带 name 时 `agentName:name`（同名 change 的并发 spawn 去重单飞）；resume 路径复用任务层既有占坑单胜。
- **Prompt 契约对齐**：重写 `plan_tool_desc.md`（产出物边界、action 契约表、resume_task 协议、create-settle 闸门、name 一等公民）；修订 `plan_agent.md`（清理归档章 action/shell 自相矛盾与重复编号、路径基准双列对照表、init 仅在 create 流程调用、A 级收尾改 status 自检）。

## Capabilities

### New Capabilities

（无——本变更全部落在既有能力上）

### Modified Capabilities

- `plan-agent`：交互契约重写——附加参数结构化透传（修复 action 进 InputSchema 的实现回归并扩展 name）；A 级收尾由 `validate --strict` 改为 `status` 结构自检（消除规格内部矛盾）；产出物边界（不代工）与 tasks 新建全 `[ ]` 上升为可校验要求；多计划并行下 name 定位语义；同名任务单飞。
- `task-reentry`：plan 场景细化——update/progress/archive 经 `resume_task` 续行成为 SHALL 级协议（原 spec 仅覆盖澄清回路场景）。

## Impact

- **代码**：
  - `agent/tool_agent.go`：`AgentToolWrapper` 附加参数声明与 JSON 打包透传；任务幂等 Key 按 name 派生。
  - `tool/plan/plan_agent.go`：`extractAction` 接收结构化 JSON；progress 按 `name` 定位（多活跃 change 不再仅报"请指定"）。
  - `tagent.go` / `config.go`：ToolRef 附加参数声明接线（plan 的 action/name）。
- **Prompt**：`resources/prompts/plan_tool_desc.md`（重写）、`resources/prompts/plan_agent.md`（矛盾清理 + 路径对照 + 收尾改道）。
- **规格**：`plan-agent`（MODIFIED，含移除 "以 validate --strict 收尾" 的 A 级适用性）、`task-reentry`（MODIFIED，plan resume 协议）。
- **行为**：progress 查询恢复零 LLM 旁路；A 级创建可正常收尾不再迭代耗尽；同名并发调用收敛为单任务；plan 拒绝代工请求并返回计划。
- **不做**：不改 openspec CLI 本身；不清理存量幽灵目录/积压 change（运维动作，随实施顺手处理但不入规格）；不引入 plan 全局单例（多计划并行是确认的合法场景）。
