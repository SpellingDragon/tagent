# plan-interaction-contract — 技术设计

## Context

生产日志（wechat-bot 2026-07-30, 13:32–13:38）完整暴露了顶层 agent 与 plan 的交互失控。逐项根因（均已代码/磁盘/复现实证）：

| # | 现象 | 根因 | 证据 |
|---|------|------|------|
| A1 | validate 失败 → 修正死角 → 迭代耗尽（51>50） | `plan-agent` 规格自身矛盾：A 级可跳过 specs（L162-186）却要求一律 `validate --strict` 收尾（L188-203）；实测 strict 与非 strict 均要求 deltas | 本地复现 `openspec validate <A级change>` ERROR "must have at least one delta" |
| A2 | progress 零 LLM 旁路从未触发 | `AgentToolWrapper.Declaration()` 只声明 `request`+`event_keys`，`Call()` 只读 `request` → `action`/`name` 被静默丢弃 → `extractAction` 拿到纯文本永远返回 ""。系既有规格 "structured action parameter" 的实现回归 | 日志 `[PlanAgent] progress query` 计数 = 0 |
| A3 | 读写双向路径踩错 | 读工具 base_dir="."、写工具 base_dir="openspec"，prompt 只讲了写侧规则 | `read_file("changes/...")`×4 失败；`openspec/openspec/.../evaluation-report.md`（15KB）落盘实存 |
| A4 | 三 Run 并发抢同一 change，`init`×15 | 幂等 Key = `agentName:request`，三次 request 文本不同 → 不去重 | tool_enter 13:32:12/13:33:06/13:34:25 均早于首个 tool_exit 13:36:09 |
| B1 | plan 被当执行者派活 → 越权代工 15KB 报告、tasks 新建即全 `[x]` | `plan_tool_desc.md`（父唯一可见面）未声明产出物边界 | 父三次请求均为"产出评估报告"类派活 |
| B2 | prompt 自相矛盾 | `plan_agent.md` 归档章声称"`action`（shell）不受限"，但 plan 工具集结构上无 exec；另有重复编号与错置条目 | plan_agent.md L53 vs L185-190 |

相关既有能力：`plan-agent`（dual-mode、合规级别、模板）、`task-reentry`（resume 原语，plan 是其范例场景）、`unified-tool-registry`（ToolRef properties 通道）。

## Goals / Non-Goals

**Goals:**
- 确立并结构化"报账–审计"交互契约：身份边界、参数通道、时序闸门、并发语义四件事从 prompt 软约束升级为 schema/引擎硬约束。
- 恢复并扩展结构化参数通道（`action` + `name`），使 progress 旁路真实可达、多计划并行下按 name 精确定位。
- A 级创建流程可正常收尾（status 结构自检），B 级维持 strict 校验。
- 同名 change 的并发调用单飞。

**Non-Goals:**
- 不改 openspec CLI；不为 A 级"补造" specs deltas。
- 不引入 plan 全局单例（多计划并行是确认的合法场景）。
- 不持久化 subagentRounds（进程重启后的重入历史恢复另行处理）。
- 不统一读写 file 工具的 base_dir（读全工程是做计划的必要能力，安全不对称保留，靠双列对照表讲清）。

## Decisions

### D1：交互模型 = 报账–审计，update/progress/archive 经 resume_task 续行

| 角色 | 身份 | 干 | 不干 |
|---|---|---|---|
| 顶层 agent | 所有者/执行者/信息桥 | 决策、补信息、按计划执行、带证据报账 | 不拆计划、不直写 openspec/ |
| plan | 规划师/记账员/审计员 | 拆解工件、勾选记账、归档审计 | 不产出工作成果 |

一个 change 生命周期绑定一个 task id：create 用新 Call（记住返回的 task id + name），其后 update/progress/archive 一律 `resume_task(task_id, ...)`。任务链还原器自动注入前序轮次（含 name 与上轮结论），plan 无需在多个活跃 change 间猜测；并发 resume 由任务层既有占坑单胜兜底。

*时序闸门*：create settle 之前 update 在语义上不存在（拿到 ACK ≠ 拿到计划）——resume 对 running 任务本就返回"轮次在飞"错误，天然成为结构闸门；`plan_tool_desc.md` 同步写明。

*为什么不全部新 Call*：新 Call = 失忆的新秘书（本次事故实证），且绕开任务层并发防护。

### D2：附加参数经 ToolRef 声明、JSON 打包透传（修复 A2）

`AgentToolWrapper` 增加通用附加参数机制，不为 plan 特判：

- **声明**：ToolRef 新增 `extra_params`（列表，每项 `{name, type, enum?, description}`），tagent.go 装配时传入 wrapper；`Declaration()` 将其并入 InputSchema。plan 声明 `action`（enum: create/update/archive/progress）与 `name`（string）。
- **透传**：`Call()` 收集 request 之外的已声明附加参数，若存在则把 `{action, name, ..., request}` 整体序列化为 JSON 作为 `inv.Message.Content`；无附加参数时维持纯文本 request（其余子 agent 零影响）。
- **消费**：`PlanAgent.extractAction` 已有 JSON 解析路径，天然衔接；新增 `extractName` 同源解析。progress 旁路按 `name` 定位 change（有 name 直取；无 name 且恰一个活跃 change 时取之；否则列出活跃清单请指定）。
- resume 路径同样打包：`subagentResume` 的新输入若为 JSON 结构则原样传递。

*修正（code review Major-1）*：resume 输入**不经 wrapper 打包**——`resume_task(id, "progress name=X")` 送达到子 agent 的是纯文本。因此 `extractName` SHALL 采用双路解析：JSON 优先（wrapper 打包路径），失败时回退到纯文本 `name=<token>` 模式（终止于空白或常见标点）。否则契约主推的 resume 路径上，progress 按 name 定位实际不可达。

*为什么打包进消息体而非 RuntimeState*：消息体是子 agent LLM 也能看到的唯一位置（ReAct 路径需要模型知道 action/name）；extractAction 的既有实现恰以消息体 JSON 为第一优先级——这是对既有设计的归位而非新发明。

### D3：创建收尾按级别分派（修复 A1，消除规格矛盾）

| 级别 | 收尾方式 | 判据 |
|---|---|---|
| A 级 | `spec(op="status", name=X, json=true)` 结构自检 | proposal + tasks 均 `done` 即合规 |
| B 级 | `spec(op="validate", name=X)`（保持 --strict） | CLI 通过 |

`plan-agent` 规格中 "tasks.md 遵循官方模板并以 validate 收尾" 的 requirement 相应改写：validate --strict 收尾仅适用 B 级；A 级 SHALL NOT 调 validate（结构上必失败）。失败修正上限 ≤2 次维持不变。

*为什么不给 spec 工具加 strict 开关*：实测非 strict 同样要求 deltas，开关救不了 A 级；status 自检是唯一与 A 级定义自洽的收尾。

### D4：同名任务单飞（修复 A4）

`AgentToolWrapper` spawn 时的幂等 Key：附加参数含非空 `name` → `Key = agentName + ":" + name`；否则维持 `agentName + ":" + request`。同名 change 的并发 spawn 被任务层 dedup 短路（返回既有任务 + Deduped 标记），后到者收到"该计划任务已在运行（task id）"而不是开新 Run。

*边界*：create 通常不带已存在的 name（名字由 plan 生成）→ 仍按 request 去重；这是可接受的残余风险，由 `plan_tool_desc.md` 的“create 未 settle 前不要重复 create”与 plan 侧“create 前先 list、同目标复用”双重软约束覆盖。

*补充（code review Minor-2 / 自查 S-1）*：
- `subagentRelaunch` SHALL 透传初次 spawnKey，使同名单飞语义覆盖 relaunch 轮次（否则 relaunch 出的任务回退到 request 键，与初次 spawn 不一致）。
- Deduped 提示语 SHALL 先引导“等 task_settled”而非直接“resume_task”：去重仅匹配**活跃**任务，此时任务必为 running，而 `Resume` 对 running 返回“轮次在飞”错误——直接引导 resume 会造成一轮无效调用。

### D5：Prompt 契约对齐（修复 B1/B2/A3/A5 的表述面）

`plan_tool_desc.md` 重写要点（父 agent 唯一可见面）：
1. **产出物边界置顶**：plan 产出计划/规格文档，不产出工作成果；派活请求会被 plan 拒绝并返回计划。
2. **action 契约表**：每个 action 的触发时机 / 必带参数（update/progress/archive 必带 name）/ 期望返回 / 传输方式（create=新 Call，其后=resume_task）。
3. **时序**：create settle 前无 update；update 必须携带产物证据（路径/结论摘要）。

`plan_agent.md` 修订要点：
1. 删除归档章 "action（shell）不受限" 整段（结构上不存在该工具），修复重复编号 "3." 与错置的"避免重复创建"条目（移至创建流程）。
2. 路径基准双列对照表：读（cwd 基准，`openspec/changes/...`）vs 写（openspec/ 基准，`changes/...`）。
3. `init` 仅在 create 流程第一步调用一次，update/progress/archive 不调。
4. A 级收尾改 status 自检（对齐 D3）；新建 tasks 全 `- [ ]` 重申，并注明"勾选权属于执行后的 update 报账"。
5. 代工拒绝：收到"产出报告/执行分析"类请求时，产出的是**执行该工作的计划**并显式说明边界。

## Risks / Trade-offs

- **[JSON 消息体改变子 agent输入形态]** plan 的 LLM 将看到 JSON 而非纯文本 → `plan_agent.md` 注明输入格式；extractAction 失败时回退纯文本路径（安全降级为 ReAct）。
- **[extra_params 机制被滥用为通用 RPC]** 仅声明式白名单透传、不做嵌套结构 → 文档限定"路由级小参数"。
- **[按 name 去重误伤]** 不同意图但同 name 的调用被 dedup → 返回体带 task id 与"用 resume_task 续行"指引，父可自行 resume；语义上同名 change 本就该单飞。
- **[status 自检弱于 validate]** A 级少了 CLI 级格式校验 → tasks.md 复选框格式由 plan_agent.md 模板约束 + progress 解析器（parseTasksMd）在消费侧兜底；B 级不受影响。
- **[TerminalTTL 截断 resume 窗口]** settle 2 分钟后任务被 prune，resume_task 失效 → 已有 `task_terminal_ttl` 配置项（前序变更），示例 yaml 为 plan 场景调大（如 "30m"）并在 tool_desc 注明"超窗后改为新 Call + name"。

## Migration Plan

1. 纯增量：`extra_params` 未声明的子 agent 行为完全不变（消息体仍是纯文本 request）。
2. plan 的 yaml 增加 `extra_params` 声明 + `task_terminal_ttl` 调大——单文件配置变更，回滚即删配置。
3. prompt 文件热重载（description/system prompt 均有 Source 机制），无需重启即可灰度表述面。
4. 规格同步：`plan-agent`/`task-reentry` deltas 随本 change 归档合入主 specs。

## Open Questions

- create 返回体是否结构化（JSON：name/task_id/任务清单）以便父 agent 稳定提取 name？倾向先在 tool_desc 约定自然语言模板（首行 `计划已创建: <name> (task <id>)`），结构化返回留作后续。
- 幽灵目录 `openspec/openspec/` 与积压活跃 change 的清理：实施时顺手清（不入规格）。
- progress 旁路在多活跃 change 且无 name 时返回清单——是否应同时返回各计划完成度摘要？倾向返回（一次 I/O，信息量大），实施时视 parseTasksMd 成本定。

### 待后续（code review 识别，本 change 不展开）

- **external_input 消息体变为 JSON 的可观测折损**：plan 调用产生的 `external_input` 事件 Content/EventSummary 现为 `{"action":...,"request":...}`，进入压缩卡片行（`extractCardLine` 取首行截 80 字）后可读性下降。非正确性问题；可选方向：卡片提取对 JSON 消息体优先取 `request` 字段。
- **extra_params 类型白名单**：限 string/number/boolean、拒嵌套结构，防止机制被滥用为通用 RPC 通道。
- **任务看板 plan 元数据**：从 Key 字符串倒推 plan 名不是长久之计，宜为 Task 增加结构化元数据字段。
- **prompt 契约测试推广**到 knowledge/recall 等其他子 agent（本 change 的 `prompt_contract_test.go` 已验证该模式可行）。
