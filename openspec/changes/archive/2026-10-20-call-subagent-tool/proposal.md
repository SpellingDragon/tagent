# Proposal：call_subagent 工具与预置能力装配

## Why

外部评估与用户澄清（2026-10-19）确认 tagent 的核心可拓展能力是 **plugin、event、event loop**，且 subagent 唤起应成为「用配置就能预置」的一等工具面：

1. agent 工具应可通过配置预置：`knowledge`、`recall`、`call_subagent`；
2. `call_subagent` 的工具描述（tool desc）应支持两种装配源：
   - **agents 配置文件**（markdown + frontmatter 形态，类似 `~/.qoder/agents/openspec-apply.md`）——按文件自动装配；
   - **显式参数**——直接指定 `prompt`、`model`、可用 `skill`、`tool`、`MCP`；
3. 装配完成后即执行（委派为 task，保留 tickle/通知语义）；
4. 尽量复用 tRPC-Agent-Go 底层能力（`tool/agent.NewTool(agent.Agent)`、`tool/transfer`、`knowledge/tool` 全栈、agent 构建器），不重复造轮子。

现状缺口（已核实）：`AgentSpec` 配置面齐全（Model/ParentID/Files/Tools/Plugins/TaskPolicy/Params），`agent.Delegate` 已把子 agent 调用抽象为异步 task，但 **call_subagent 没有 tool desc 双装配源；knowledge 未预置为工具；golden 无 config 驱动 subagent/pipeline 场景（S11）**。

## What Changes

- 新增 **`tool/subagent`** 包：`call_subagent` 工具（desc 双装配源：`agents_file` 或显式参数）＋装配器（md frontmatter 解析 → 复用 trpc agent 构建器 → 委派 task 化执行）。
- 配置面：预置 `knowledge`（复用 `trpc knowledge/tool`）、`recall`（现 `memory` 检索面已有，补工具装配引用）、`call_subagent` 三类工具引用。
- **plugin / event / event-loop 扩展面保真**：明确 event 自定义消费的扩展点并补测试（不迁历史冥想/knowledge 宿主行为，作为扩展点示例文档化）。
- golden 场景新增 **S11**（config 驱动 subagent ＋ pipeline 顺序）与 **S12**（call_subagent 双装配源 → task 执行 → 通知回调）。

## Capabilities

### New
- `call-subagent-config`：call_subagent 工具的双装配源契约（agents 文件/显式参数）、装配→执行链路、三类预置工具的配置引用、通知回调语义。

### Modified
- `action-tool-config`：工具配置面新增 subagent/knowledge/recall 引用的装配语义。
- `runtime-scenarios`（若有 golden 合同能力）：S11/S12 场景登记。

## Impact

- 新增包 `tool/subagent`；`config.AgentSpec.Tools` 引用面扩展；`agent.Delegate` 复用不重写。
- tRPC 复用登记：`tool/agent`（agent 工具形态参考）、`knowledge/tool`（knowledge 检索工具）、agent 构建器（装配产物）；fork replace 现状不动（用户裁定 4）。
