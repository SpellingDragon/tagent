# plan-interaction-contract — 实现任务

## 1. 附加参数通道（AgentToolWrapper，修复 A2）

- [x] 1.1 `config.go` ToolRef 新增 `extra_params` 声明（`[]ExtraParam{name, type, enum, description}`），`tagent.go` 装配时传入 `AgentToolWrapper`
- [x] 1.2 `AgentToolWrapper.Declaration()` 将 extra_params 并入 InputSchema；`Call()` 收集已声明附加参数，存在时将 `{附加参数..., request}` 序列化为 JSON 消息体（无附加参数维持纯文本，其余子 agent 零影响）
- [x] 1.3 resume 路径对齐：`subagentResume` 的新输入为 JSON 时原样透传
- [x] 1.4 单测：schema 含 action/name、JSON 打包透传、未声明子 agent 行为不变、精度安全（json.Number）

## 2. PlanAgent 消费侧（修复 A2/多计划定位）

- [x] 2.1 `extractAction` 确认 JSON 路径衔接；新增 `extractName` 同源解析
- [x] 2.2 progress 旁路按 `name` 定位：有 name 直取；无 name 且恰一个活跃 change 取之；否则返回活跃清单（含各计划完成度概要）
- [x] 2.3 单测：带 name 旁路直达、无 name 单计划兜底、多计划返回清单不猜测、旁路全程零 LLM

## 3. 同名任务单飞（修复 A4）

- [x] 3.1 `AgentToolWrapper` spawn 幂等 Key：附加参数含非空 `name` → `agentName:name`，否则维持 `agentName:request`
- [x] 3.2 Deduped 返回体带既有 task id 与"用 resume_task 续行"指引
- [x] 3.3 单测：同名并发 spawn 恰一个新任务、后到者收到指引、无 name 调用去重行为不变

## 4. 创建收尾按级别分派（修复 A1）

- [x] 4.1 `plan_agent.md` 创建流程改写：A 级收尾 `spec(op="status", json=true)` 自检（proposal+tasks done 即合规，SHALL NOT validate）；B 级维持 `validate`（--strict）；修正上限 ≤2 保持
- [x] 4.2 复核 `tool/spec` hintFor：validate 失败输出含 "at least one delta" 时补充提示"A 级计划不应调用 validate，改用 status 自检"（引导误入者自救）

## 5. Prompt 契约对齐（修复 B1/B2/A3/A5）

- [x] 5.1 重写 `plan_tool_desc.md`：产出物边界置顶（plan 不代工）、action 契约表（触发时机/必带参数/期望返回/传输方式）、create-settle 闸门、update 带证据、resume_task 协议与超窗（task_terminal_ttl）后按 name 续行
- [x] 5.2 修订 `plan_agent.md`：删除归档章 "action（shell）不受限" 整段与重复编号，"避免重复创建"移至创建流程；路径基准双列对照表（读 cwd vs 写 openspec/）；init 仅在 create 流程调用一次；输入可能为 JSON（action/name/request）的说明；代工拒绝行为
- [x] 5.3 `examples/wechat-bot/tagent.yaml`：plan ToolRef 声明 `extra_params`（action enum + name）；tagent agent 调大 `task_terminal_ttl`（如 "30m"）并注释 plan 续行语义

## 6. 验证与回归

- [x] 6.1 场景回放测试：以 wrapper 级测试覆盖（`TestSameNameSingleFlight` 同名并发单飞、`TestExtraParams_CallPacksJSONBody` action/name 透传、`TestBuildProgressSummary_NamedTarget` 按 name 定位）+ prompt 契约测试（`TestPlanPromptContract` 守边界/路径/收尾表述）；派活转化与 A 级收尾的真实 LLM 断言在 `tests/plan_agent_create_behavior_test.go`（非 -short 运行）
- [x] 6.2 A 级创建端到端：`tests/plan_agent_create_behavior_test.go` 断言改为「以 status 自检收尾 + 禁止 validate」（真实 LLM 集成测试，`-short` 下跳过）
- [x] 6.3 全量测试 + 构建干净（`go build ./...`、`go test -short ./...` 全绿）
- [x] 6.4 顺手清理（不入规格）：删除 `examples/wechat-bot/openspec/openspec/` 幽灵目录；同步压缩/plan 相关 wiki 交互协议章节

## 7. Code Review 修复（零基审查后补充）

> 来源：实现完成后的 fresh-eyes review（0 Blocker / 1 Major / 1 Minor / 1 Nit + 2 项自查）。

- [x] 7.1 **Major：resume 路径 name 解析缺口**——`extractName` 仅解析 JSON，而契约主推的 `resume_task(id, "progress name=X")` 送达的是纯文本（resume 输入不经 wrapper 打包），导致多计划并行下带了 name 仍被回“请指定”；新增 `name=<token>` 文本 fallback（JSON 优先，终止于空白/常见标点）+ 端到端回归测试
- [x] 7.2 **Minor：relaunch Key 未对齐**——`subagentRelaunch` 仍用 `agentName:request`，使 relaunch 出的任务脱离同名单飞语义；改为透传初次 spawnKey + `TestRelaunchKeepsNameKey` 锁定
- [x] 7.3 **自查：Deduped 提示语与任务层语义矛盾**——去重发生时任务必为 running，而 Resume 对 running 拒绝（“轮次在飞”），原提示“请用 resume_task 续行”会引导一轮无效调用；改为“先等 task_settled，结算后再 resume”
- [x] 7.4 **Nit：多计划提示文案**——不再暗示“调用方未传 name”，改为列出活跃计划后给出指定示例
- [x] 7.5 **修复 flaky 断言**：`TestSameNameSingleFlight` 原断言子 agent `Run` 调用数==1，但 `FuncSettleDetector` 在创建时即启动 fn、Spawn 的 dedup 在其后才 Cancel——重复调用的 Run 可能瞬时启动（既有框架行为，与键无关）；断言收敛为任务层契约（唯一跟踪任务 + 调用方被正确重定向），20 轮连跑 0 失败
- [x] 7.6 验证：`go build` / `go vet` / `gofmt` 干净；`go test -short ./...` 三次全量零失败
