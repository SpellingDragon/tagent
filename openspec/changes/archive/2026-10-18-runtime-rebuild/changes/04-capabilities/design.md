# 04-capabilities — 设计（design.md）

> 二级子变更技术设计。**只写"怎么做"的规划**；本轮不写产品代码，签名未经探针不称冻结。上游只读计划 `运行时_重设_实施计划_304e54c3.md` §4.2/§4.3/§4.4/§5/§7。

## Context

- 现场：清理前残留的 `model/`、`state/`、`tagent/` 已随 2026-10-13 用户授权清理批次删除；`tool`/`governance`/`memory`/`mcp`/`a2a` 包不存在。`go.mod` 锁定 core fork `v1.11.2-tagent.1`、provider `v1.10.0`（`go.sum` 齐全）。**清理前历史 HEAD `56c58eb`，当前新基线 HEAD `a9381ce`**。
- 权威设计闸：G0（现场/保护面/档位/资源）、G1（共享责任签名 + 生产消费者）、G2（正常/失败/并发/重启纵向推演）、G3（逐文件白名单/前置/读写冲突/命令/预算）。**本域实施单的共同前置是 G0–G3 通过**。
- 本域不承担 `state`/`agent`/`task` 核心装配（属 01/02）；本域**消费**其冻结接口。

## Goals / Non-Goals

**Goals**

- 原生 file/skill/FunctionTool 经正式注册**真实执行**（进入 `Request.Tools`，被模型第二轮看到）。
- exec/tmux 的**外部生命周期**（启动/停止凭证/重挂/TTL/续行/资源租约）有跨重启可验证证据。
- MCP/A2A/内部 send 的显式目标、调用归属与权限；协议原样复用。
- 治理执行前**恰好一次**；Hold≠Denied；预算 epoch、拒绝账本与恢复、goal 语义核实。
- 检索先验必要性、再真实消费；召回报告保留零增益/失败。

**Non-Goals**

- 不建第二工具迭代循环（禁 `LLMAgent`）。
- 不建占位结构、反射 DI、新 registry 框架。
- 不复制 state/agent/task 装配；不改写原始事实；不接 PPO/GRPO。

## Decisions（含被否方案）

- **D04-1 原生优先**：file/skill/FunctionTool/MCP/A2A 优先复用原生 `CallableTool`/`ToolSet`/`FSRepository`；tagent 只加薄适配。被否：为阻止一个 import 建同形接口/影子结构。理由：计划 §4.2/§4.3、§5 复用矩阵。
- **D04-2 绑定随代借出**：工具活绑定由 state 持有，请求边界借出同代；旧请求继续持旧代。被否：工具实例挂在 agent 私有 `Config`。理由：计划 §4.2、01 域 D 表。
- **D04-3 文件直接生效 vs 结构前置门**：prompt/skill 按旧热加载语义**直接生效**、下一请求边界一致读取；结构变化（工具集/模型）走前置门。被否：`register` 充当文件生效开关。理由：计划 §4.2。
- **D04-4 执行前策略恰一次**：变换后的最终请求经核心复核身份/权限后**同时**用于执行与记录；拒绝执行但产生**原生配对具名结果**供自纠。被否：策略多次运行、拒绝时静默丢弃。理由：计划 §4.2。
- **D04-5 Hold≠Denied**：审批等待登记为"尚未执行"的任务并明确返回等待状态；**不伪装最终 Denied**。被否：把 Hold 记为 Denied。理由：计划 §4.2。
- **D04-6 后台结果为新观察**：后台结果进入**下一次**实际模型视图，**不冒充同 CallID 的第二次同步 tool response**。被否：把后台结果塞回原 CallID。理由：计划 §4.3/§4.4。
- **D04-7 检索必要性先行**：先验证接口必要性再接真实检索源/embedder；无真实消费者的字段不预建。被否：先造接口再找消费者。理由：计划 §4.4、§4.5。
- **D04-8 goal 语义核实**：goal 状态词以**当前保留业务语义**审定，**不偷改状态词**。被否：按新命名重置状态词。理由：计划 §4.2 末、用户硬约束。
- **D04-9 已修改机制按原能力名 MODIFIED**：`action-tool-config`（`monitor` 超参 + `IsTmuxAvailable` 真实探测）虽由 04.2 承载验收，仍以**原 source 能力名**写 MODIFIED delta（`specs/action-tool-config/spec.md`）。被否：仅用文字说"已折入 `adaptive-poll-scheduling`/`resident-session-continuity`"而不写 delta——旧 source 条款仍在，不写 delta 即留**规范冲突**。理由：覆盖表 `action-tool-config→04.2 = M`；已修改的机制须按原能力名改。

## 字段 / 端口所有权

| 面 | 所有者 | 本域角色 | 备注 |
|---|---|---|---|
| 生效工具绑定（同代） | state（01 域） | 借用 | 不复制、不私编 |
| 原生工具 wrapper / 一次 Receipt | 02 域（agent/task） | 消费 | 本域只注册原生工具 |
| MCP 注册表 / 连接 | 本域（`mcp-server-registry`） | 持有 | 关闭对齐 state 生命周期 |
| A2A / 内部 send 目标 | 本域（`remote-agent-communication`/`cross-session-delivery`） | 持有 | 显式目标，禁盲投 |
| 审批请求落盘 | ApprovalManager（本域 `approval-channels`） | 持有 | digest 绑定不变 |
| 预算 epoch / 拒绝账本 / goal registry | 本域治理 | 持有 | 恢复经事件回放 |
| 检索面（授权/融合/embedding） | 本域 memory 检索 | 持有 | 消费者为模型/训练/SDK |
| 大结果外置/副本 | 03 域（workspace） | 消费 | 不另造外置路径 |
| `monitor` 超参 / tmux 真实探测 | 本域（`action-tool-config`） | 持有 | 按代消费；探测独立于构造，不恒真 |

## 并发 / 取消 / 生命周期 / 错误 / 配置（本域切片）

- **并发**：注册表/MCP 注册表需并发安全（`Get/List/Names/Add/Remove`）；发现按实时注册表读；工具执行前策略恰一次（per-request 一次性，不跨并发共享可变状态）。
- **取消**：后台任务取消不连坐（02 域语义）；MCP 调用取消需响应；exec/tmux 取消须有**停止凭证**，`unknown≠dead`；**连续 `unknown` 只升级诊断或显式停止处理，MUST NOT 作为判死/退出依据，未止义务与资源租约保留且可解释**。
- **生命周期**：exec/tmux 会话 TTL/续行/重挂；MCP 关停统一释放（幂等）；进程关闭时按依赖序。
- **错误**：MCP 失败以**结果（nil error）**返回自纠材料；治理拒绝产生具名原生结果；审批通道投递失败**不阻塞审批门（闸不是墙）**。
- **配置**：`mcp_servers`/`api_key_env`/`embedding`/`trajectory` 等按计划 §4.4/§4.5；embedding 未配置时**零行为变化**。

## 风险 / Trade-offs

- [原生接口在锁定版本可能缺能力] → 先做**编译探针**（G1）；缺能力即 `design-blocked`，不造影子结构。
- [检索零增益被掩盖] → 04.9 保留零增益/失败结论行。
- [Hold 被误记 Denied] → D04-5 + 场景断言分离。
- [文件热读与结构门混淆] → D04-3 + 通道矩阵。

## 待核（Open Questions，不设假默认）

1. `goal` 保留状态词集合与语义（04.6）——需现场保留业务语义核实。
2. 检索接口必要性（04.7）——先验证再接线。
3. 真实 MCP/A2A/embedding 端点、tmux socket、授权接收者是否齐备（资源欠账）。
4. `mcp-server-registry` 现有要求中 `api_key_env`/热同步语义在重建后是否仍成立（需比对，不默认继承）。
5. `action-tool-config`（`monitor`/`IsTmuxAvailable`）已按原能力名写 MODIFIED delta；其 `compress` 段退役已显式化。若设计闸（00.3/G1）判定 `monitor` 需拆独立 delta，再行调整——属**设计待核**，不预设继承。

## 文件清单（未来候选写域，**未存在亦未获批**）

- 产品（建议，按 architecture policy 允许包收敛；部分归 01/02 域，本域只列相关面）：`tool/exec/`（exec/tmux 生命周期与租约）、`tool/mcp/`（registry/call/discovery）、`tool/remote/`（A2A/send 寻址投递）、`plugin/`（策略/审批/预算/拒绝账本/goal，治理与插件同在 `plugin` 包）、`memory/`（检索/融合/策展）、module 根装配 `prepare.go`（原生 file/skill/FunctionTool 登记，复用框架 `FSRepository`/`CallableTool`，**不预建 `skill` 包**）。
- 测试：`tests/integration/capabilities/{native_tools,exec_lifecycle,mcp_lifecycle,addressed_delivery,governance,governance_recovery,recall_consumer,memory_curation,recall_evaluation}_test.go`。
- **本轮全部只写本目录 `openspec/changes/runtime-rebuild/changes/04-capabilities/**` 的 md**。
