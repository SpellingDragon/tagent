# Proposal: hardening-review-batch2

## Why

2026-09-16 对 main 一周变更（`2c2b137..7080753`，129 commits）的独立深度审查发现：多处变更存在「局部动作完成 ≠ 端到端能力生效」的断链。此前向远端通报的部分「已闭环/已根治」结论被代码证据推翻，必须修正，否则会在生产中继续产生错误宣称的成功：

- **内部任务世系跨重启丢失**（R2 恢复路径）：`task_spawned` 未保存 `Spec.Origin`，恢复闭包工厂又整体覆盖 spec——冥想派生任务重启后退化为合法 `task` 来源，可绕过现有 fail-closed 投递门禁（得到的是错误但合法的 stamp，而非缺 stamp）。
- **失败被写成成功事实**（`7080753` 引入 + 既有 zombie/orphan 路径）：墙把内存置 `TaskFailed` 却发 `SettleCompleted` 无 Err；`SettleFailed` Kind 不被 settle mapper 识别同样落入 completed——WAL、反馈与看板三方失真。
- **24h resident 清理可误杀正常会话**：Sweep 按最初 `SpawnedAt` 判超龄、先清理后收养、注释承诺的「重挂刷新时间」并未实现。
- **R3 重挂与跨重启 resume 的 detector 信号链未接通**：重挂创建的 detector 未交 TaskManager 消费、monitor 未启动、resume 新旧 detector 错绑——「会话存在/看板 running」不能证明后续输出与结算能回来。
- **前缀验证器错误放行确定丢失**：向前配对任意旧记录使「死亡前 100 条→恢复 30 条」判 EXACT（已复现，exit 0）；另一类截尾直接 IndexError 崩溃；`CRITICAL=0` 因此不能作为完整性证明。
- **全配置热更多处断链**：数值热更与结构热更互斥分支互相漏字段；热更只处理 entry agent；`UpdateMaxTokens` 只动外层触发线，内层 SmartCompressor 目标仍是冷构造值；R4 换代的新工具 wrapper 绑定空投影、新 prompt source 被旧常驻 getter 覆盖——「热更新已生效」通知不能证明参数已应用。
- **严格解析破坏 MCP 完整配置热同步**：registry 用仅含 `mcp_servers` 的局部结构严格解码完整配置文件，合法根字段被判 unknown，热同步静默失败保留旧 registry。
- **HTTP 认证与运维探针/邮件入口未集成**：`/healthz` 受 token 保护，但 restart/maintenance 探针与 mail poller 不发送认证头——启用 token 即误重启 + 邮件阻断。
- **时长墙（`7080753`）设计缺陷**：无进程证据即强判 failed、计时 `detachedAt` 不持久化（跨重启绕过墙）、晚到 detector 信号可复活终态、只改状态不治理进程——需重设计为「stale 观测 + owner 终止」而非看板状态替代进程事实。
- **no-anchor 恢复静默截断 500 原始事件**：历史超限时确定性丢失且未标明 partial——不能兑现「死亡时上下文端到端恢复」。

## What Changes

按「先停止错误宣称成功，再补全生效链路，最后治理运维」三批组织：

**批 1 — 防错误宣称成功（P0 实施）**
1. 任务身份/来源/路由作为持久化事实跨重启保真；闭包工厂只补执行能力不覆盖身份；内部来源不得默认升级为可投递来源。
2. 统一终态转换入口：内存状态、Settle Kind、WAL settle_status、反馈四者一致；失败不写 positive；迟到信号不得复活终态（fencing）。
3. 停止按 spawn 年龄杀长驻会话：先收养后清理、独立 orphan grace、无绝对寿命上限。
4. R3 detector 消费链接通：唯一 session→detector→task 绑定；验收到达真实 watch/完成信号。
5. stale-detached 墙重设计：超龄默认标记 stale/suspect 观测事实；终止经 owner cancel→确认退出→一次结算；job/service 生命周期显式化。
6. 修复前缀验证器：boot/agent/session 身份配对、固定死亡前最后请求、截尾/缺记录/无法配对=验收失败、看板段单列比较、CRITICAL 非零退出码。
7. 运维探针与 mail poller 携带认证；401 不触发杀进程；区分认证失败/监听失败/未就绪。

**批 2 — 热更断链补全（P1 实施）**
8. 统一应用模型：每次配置加载形成逐 agent 完整有效配置，数值与结构变化非互斥地统一应用；desired/effective 回执报告实际应用字段与生效代次。
9. 压缩参数快照化：trigger/target/maxTokens/keepRecent 及派生容量作为同一有效代应用，内外层预算一致。
10. R4 执行代绑定：换入前新工具 wrapper 绑定常驻投影；prompt getter 按执行代不可变；`execCfg` 成为生效配置快照。
11. MCP registry 改为对 `mcp_servers` 子树严格解码（或由完整 schema 层传入），完整配置文件热同步不误判 unknown。
12. 首次结构热更前保存启动代快照，保证第一次换代即可回滚。

**批 3 — 恢复语义与尾项（P1/P2 实施）**
13. no-anchor 恢复不静默截断：完整恢复或显式 `truncated/partial` 状态；lostKeys 观测扩展到 tail 分页/批量读取/payload 错误。
14. doc_snapshot.py 排除 `.damaged` 备份参与 restore 候选。
15.（观测增强随批内各任务落地：压缩流水线分段记录、恢复 diagnostics、热更回执——复用现有设施，不另建系统。）

## Capabilities

### New Capabilities
- `config-hot-reload`: 配置热更统一应用模型——数值与结构非互斥、逐 agent 应用、desired/effective 回执、执行代绑定（wrapper/prompt/execCfg）、首代可回滚。
- `trajectory-verification`: 重启前缀恢复验收规则——身份配对、死亡前最后请求锚定、截尾即失败、动态系统段单列、验收失败必须非零退出。
- `ops-deployment-integrity`: 部署与运维状态链——探针认证集成、liveness/readiness 分离、成功/失败/超时 marker 区分、告警不依赖被监护进程。

### Modified Capabilities
- `task-registry-rebuild`: 恢复保真——Origin/身份/路由跨重启不丢失，闭包工厂不得覆盖持久化身份；恢复任务缺来源时不得默认升级为可投递来源。
- `async-task-execution`: 终态事实一致性——唯一终态转换入口、失败结算语义（Kind+Err）贯穿事件与 WAL、迟到信号 fencing、stale-detached 重设计为观测+owner 终止。
- `resident-session-continuity`: 收养先于清理、detector 消费链接通、长驻会话无隐含绝对寿命。
- `mcp-server-registry`: 完整配置文件热同步与严格解析兼容（子树解码）。
- `event-sourced-projection`: no-anchor 恢复不静默截断，partial/truncated 状态显式化，恢复观测覆盖 tail 分页与 payload 错误。

## Impact

- **代码**：`agent/task/task_manager.go`（终态入口/墙重设计/detachedAt）、`agent/task_record_sink.go` + `agent/context_manager.go`（Origin 持久化/恢复闭包边界/热更绑定）、`agent/event_bus.go`（settle mapper）、`agent/compress/{context_compressor,smart_compress}.go`（参数快照）、`build_agent.go` + `tagent.go`（热更统一应用/首代快照）、`tool/action/{resident_recovery,declarative}.go`（清理顺序/detector 接线）、`tool/mcp/registry.go`（子树解析）、`agent/projection_rebuild.go`（no-anchor 语义）、`rl/http_api.go` 消费侧 `examples/wechat-bot/{restart-tagent.sh,restart-maintenance.sh,mail-poller/mail_poller.py,main.go}`（认证/Server owner）、`scripts/verify_restart_prefix.py`（重写判定）、`examples/wechat-bot/scripts/doc_snapshot.py`。
- **行为兼容**：`task_max_detached_age` 语义变更（强 failed → stale 观测 + 显式终止策略）；验证器结论口径收紧后，远端历史 EXACT 数字不可比。
- **测试**：新增跨恢复世系→投递门集成测试、终态一致性四端断言、验证器反例回归（两个已复现场景）、热更混合字段生效断言、MCP 完整配置热同步、认证探针集成。
- **不改变**：wechat-bot openspec change 不入库的既定裁决；S6 全量回放性能（用户豁免，不在本案范围）。
