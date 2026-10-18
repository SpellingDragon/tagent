# 04-capabilities — 工具、治理与检索（二级子变更 proposal）

> 边界：本文件与同目录 `design.md`/`tasks.md`/`specs/**` 是本域**规划产物**（`planned` / `design-blocked`）。
> 本轮**无产品实现授权**：不写产品代码、不写产品测试、不跑 build/test、不安装依赖、不提交、不归档。
> 产品语义与行为约束来自只读外部计划 `运行时_重设_实施计划_304e54c3.md`（下称"计划"）§4.2/§4.3/§4.4/§7（04 行）。CLI 模板取自 `openspec instructions {proposal,design,specs,tasks} --change runtime-rebuild --json`。

## Why

清理后的工作树没有可运行的工具/治理/检索基线：旧生产树被移除，`tool`/`governance`/`memory` 等包不存在，`model/collect.go` 等残件引用的类型亦缺失。重建必须锁定 tRPC 原生工具接口、state 持有生效绑定、治理在执行前恰好一次，并让检索有真实消费者，否则会重演"状态归属不清 + 测试替身掩盖真实接线"的旧问题。

## What Changes

- **原生工具正式化**：file/skill/FunctionTool 经正式注册进入 `Request.Tools` 并实际执行；加载状态回 state，文件变化在下一请求边界一致读取生效（不等 Git 登记或质量门）。**不加入 LLMAgent 的第二工具迭代循环**。
- **外部执行生命周期**：exec/tmux 的启动、停止凭证、重挂、TTL/续行与资源租约统一建模；`unknown≠dead`，停止未确认不得伪称完成。
- **MCP/A2A/内部 send**：MCP 连接/发现/固定网关与关闭；A2A 与内部 `send` 的显式目标、调用归属、权限、重试；协议原样复用，不重写。
- **治理**：warn/strict、风险规则、异步审批（**Hold≠Denied**）、预算 epoch、拒绝账本与恢复、goal 旧语义**核实**（不偷改状态词）。
- **检索**：授权时间/类型/关系与混合融合；先验证接口必要性再接真实检索源与 embedder；召回质量与开销报告保留零增益/失败。

## Capabilities

### New Capabilities

（无。本域不新增产品能力——避免"同义聚合 cap"。所有变更落在既有能力名的 delta 上。）

### Modified Capabilities

- `unified-tool-registry`：注册语义改为**原生工具经 state 借用绑定**后进入 `Request.Tools`；注册表不再承载第二迭代循环。
- `tool-lifecycle-management`：三阶段生命周期在 state 绑定代际下的重述（借出/回收对齐 01 域）。
- `resident-session-continuity`：exec/tmux 会话生命周期与资源租约、停止凭证、重挂的**跨重启证据**强化。
- `adaptive-poll-scheduling`：轮询节奏与资源租约/停止凭证对齐（TTL 语义见 `async-task-lifetime`）。
- `async-task-lifetime`：TTL 锚点与外部进程/mux 会话停止证据对齐。
- `mcp-server-registry`：连接/关闭与 state 生命周期对齐，固定网关不随注册表内容漂移声明。
- `mcp-discovery`：实时注册表发现语义沿用，新增 state 绑定代际一致读取。
- `mcp-call-tool`：恒定声明网关与拒绝/错误自纠材料对齐原生配对。
- `remote-agent-communication`：A2A 显式目标与调用归属、重试绑定原生意图。
- `cross-session-delivery`：进程内常驻表寻址与显式目标、拒绝账本对齐。
- `tool-security-alignment`：exec/tmux 安全对称在重建执行路径上的复述。
- `approval-channels`：审批**等待（Hold）**与最终 **Denied** 语义分离。
- `goal-tools`：goal 旧状态语义**先核实再沿用**。
- `semantic-search`：向量链路与 state 绑定、预算 epoch 对齐。
- `recall-hybrid-fusion`：融合仅在授权检索面上生效。
- `memory-curation`：固化/整理触发因果关联不改写原始事实。
- `action-tool-config`：`monitor` 超参（TmuxMonitor 节奏）与 `IsTmuxAvailable` 真实探测在重建 exec/tmux 执行路径上按代消费；`compress` 子配置退役**显式化**（MUST NOT 恢复 tool 级压缩超参）。

### cap 声明与取舍（不造同义聚合，但已修改机制按原能力名 MODIFIED）

- 用户 cap 清单中 04 域列为 17 项；本波**使用全部 17 项**（见上 Modified Capabilities 列表）。
- `action-tool-config`：**保留原 source 能力名**写 delta（`specs/action-tool-config/spec.md`），不把"已折入别 cap"作为不写 delta 的理由——其主 spec 条款仍在，不写 delta 即留**规范冲突**。
  - `monitor` 配置（TmuxMonitor 节奏）与 `IsTmuxAvailable` 真实探测虽由 **04.2** 承载验收（`resident-session-continuity`/`adaptive-poll-scheduling` 的停止凭证/重挂/租约/节奏），但在 `action-tool-config` 名下以 **MODIFIED** 重述（已修改的机制须按原能力名改，不能仅用文字说折入）。
  - 其 `compress` 子配置**已退役**（源 spec 状态核验明示「compress 相关 SHALL 条款不约束当前实现」，无 tool 级消费方）：delta 把它**显式化**（MUST NOT 恢复 tool 级压缩超参），不新增消费者；未修改的领域机制（如安全规则）不强行新增条款。
  - 若设计闸（00.3/G1）判定 `monitor` 配置需拆分到独立 delta，再行调整；不预设继承。

## Impact

- 未来候选写域（**未存在亦未获批**）：工具/治理/检索相关产品包与 `tests/integration/capabilities/**`。
- 上游**接口**依赖由 01/02 域冻结（生效绑定、原生 wrapper、任务提交端口）；本域只消费，不复制。
- 非目标：不写 `state`/`agent`/`task` 核心装配；不建占位结构、反射 DI 或新 registry 框架；不接 PPO/GRPO 训练引擎。

## 边界与依赖

- **上游签名/实际实现**：
  - 复用（不复制）：`trpc.group/trpc-go/trpc-agent-go @ v1.11.2-tagent.1` 的 `CallableTool`/`ToolSet`/`FunctionTool`/`model`/plugin callbacks；`.../model/provider @ v1.10.0`；`trpc.group/trpc-go/trpc-mcp-go`；`trpc-a2a-go`。**签名未经探针冻结**，实施前的编译探针在设计闸（00.3/G1）审定。
  - 域内消费：01-state-binding 的生效绑定（模型/工具/有序插件管线）、02-execution 的原生 wrapper 与一次 Receipt/后台结果、03-history 的大结果外置。
- **下游消费者**：模型**第二轮**实际视图（04.1/04.3）；SDK/CLI/诊断/看板（05 域）；训练采集的因果关联（06 域）；宿主（08 域）。
- **禁止事项**：不新增 `LLMAgent` 第二循环；不发明消费者救抽象；不为阻止 import 造同形影子结构；不改写原始事实；不把未核实旧机制标为自动继承。

## 待核（不设假默认）

- `goal` 旧状态词/语义需按现场保留业务语义核实后再固化（04.6，`design-blocked`）。
- 检索接口**必要性**先验证，再决定接真实 embedding/检索源（04.7）。
- 04.9 召回质量报告依赖**授权资源**（真实检索路径），未授权不启动。
- 资源欠账（真实 MCP/A2A/embedding 端点、tmux socket、授权接收者）见 `design.md`「资源与待核」。
