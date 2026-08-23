## 1. task_settled 输出转储（D1 修订版：初版"全文入 Content"经评审推翻，改为对齐同步路径转储模式）

- [x] 1.1 `agent/event_bus.go`：`newTaskSettledEvent` 增加转储逻辑——结果超阈值（与 OutputLimitTool 同公式 `MaxTokens/2×4` 字符）时全文写 `workspace tool-output/task-<id8>-<ts>.txt`（复用 `workspace.ToolOutputPath`），Content = 尾部 2000 字符 + 文件路径票据；≤阈值全文内联（配置链删除已随初版完成）
- [x] 1.2 转储写入失败降级：文件写入失败时退回全文内联（可用性优先于有界性）并 Warnf
- [x] 1.3 迁移测试：`agent/task_settled_test.go` 的全量断言改为分界断言（小结果全文内联无标记；大结果尾部+票据、事件 Content 有界、文件存在且为全文）
- [x] 1.4 端到端断言：大结果事件经 memory_recall 召回返回**有界版+票据**（不复发）；`read_file` 对转储文件分页可读（start_line/num_lines）

## 2. 压缩触发单维化（D2，BREAKING）

- [x] 2.1 `agent/compress/context_compressor.go`：触发判断改为 `usedTokens > threshold` 单维；删除 `completeTurns` 统计与轮数维度；更新触发日志行
- [x] 2.2 更新触发点注释：删除"轮数维度防饿死"论述，替换为容量单维原则（D2 论证：饿死已根治、token 累积保证触达）
- [x] 2.3 迁移依赖轮数触发的既有测试（`skeleton_archive_test.go` 收敛断言等）为容量触发等价形态（低 `max_tokens` 强制触发）

## 3. 渲染冻结——整理边界锚定（D3）

- [x] 3.1 `agent/compress/projection.go`：`SessionProjection` 新增整理边界 key（含 getter/setter）；`Replace` 时由调用方设置
- [x] 3.2 `context_compressor.go`：整理轮在 `buildRetainedRefs` 后设置新边界（最近 `recentFullCount` 条 retained 正 key refs 的最前一条）；`resolveRef` 的 full 判定改为 `ref.EventKey >= 边界`（负 key 合成 ref 不参与）
- [x] 3.3 初始态处理：从未整理时 boundary=0（全 refs 全文），与现状小会话行为一致
- [x] 3.4 新增稳定性测试：整理后追加新 refs 未触发整理的连续两轮，渲染消息序列公共前缀逐字节相同；旧 refs 渲染方式（全文/摘要）不变、新 refs 全文
- [x] 3.5 新增测试：整理轮重设边界后，下一整理间以新边界冻结

## 4. 工具链折叠移入整理路径（D4）

- [x] 4.1 `context_compressor.go`：`foldToolRuns` 调用从触发判断之前移到触发之后的整理路径内；未触发轮 pass-through 不折叠
- [x] 4.2 迁移 `tool_chain_test.go`：折叠测试改为整理路径内调用（或低预算强制触发后断言）；新增"未触发轮不折叠"断言
- [x] 4.3 验证折叠语义注释更新（折叠=整理动作的组成部分，非持续维护）

## 5. 框架文案票据化（D5）

- [x] 5.1 `agent/tool_agent.go`：同名去重提示收缩为事实性票据（task id + 等待 task_settled + 勿重复发起），删除 `get_task_result`/`resume_task` 教学
- [x] 5.2 `agent/compress/smart_compress.go`：`buildSegmentCompressNotice` 删除工具名与调用示例列举（`recall({...})`/`search_content`/`read_file` 参数句），保留纪律句与 evt 票据清单
- [x] 5.3 `resources/prompts/action_tool_desc.md`（及 example 副本）：删除任务工具组引用句
- [x] 5.4 同步文案锚定测试：`tool_agent_extra_params_test.go` 去重断言、`tests/contracts_llm_test.go` 归档通知锚定改为新文案形态

## 6. get_task_result 退役（D6）

- [x] 6.1 `tool/task/register.go`：删除 `get_task_result` 注册；`tool/task/task_tools.go` 删除 `GetTaskResultTool` 实现与单测
- [x] 6.2 全库 grep `get_task_result` 清理残留引用（文档注释、README、yaml 注释——`tagent.yaml` L144-145 注释更新为 recall 票据说明）
- [x] 6.3 `list_tasks`/`cancel_task`/`relaunch_task`/`resume_task` 保留注册不动（确认无文案引用）

## 7. 验证与文档

- [x] 7.1 全量 `go test ./...` 通过（重点：agent、agent/compress、tool/task、tests 契约包）
- [x] 7.2 README.md / README_EN.md 配置表：删 `task_settled_max_inline` 行；`keep_recent_tasks`/`recent_full_count` 描述纯化为"整理后状态参数"；触发描述改单维
- [x] 7.3 `docs/wiki/memory/memory-architecture.md`：§16 新增小节（容量触发整理 + 渲染冻结 + settle 全量保真 + 文案票据化原则），修正既有"触发器多维化"表述
- [x] 7.4 验证收尾：`openspec` 校验 delta 合法；`gofmt`/`golangci-lint` 通过

## 8. 评审修复（fresh-eyes review 2026-08-23）

- [x] 8.1 【Major】字节级前缀冻结测试：锚定后连续两轮 under-budget `Compress`，断言两轮 `Messages` 前缀逐条相等（D3 可执行定义，目前零覆盖）
- [x] 8.2 【Major】修 `TestContextCompressor_RecentFullCount`：round 2/3 改用 healthy 预算 cc 直接继承 `fullBoundary`（同包赋值），真正走"锚定后+未超阈"直通分支；修正注释与路径不符
- [x] 8.3 【Minor】清理 `get_task_result` 残留 5 处：`agent/task/task_manager.go` L255/L264 注释（改 resume 窗口语义）、`registry.go` L80 注释、wiki `agent-architecture.md`/`tool-architecture.md` 工具清单、`task_manager_test.go` L270 失败消息
- [x] 8.4 【Minor】`anchorFullBoundary` 返回 0 退化行为加注释标注（正 key < 窗口时全渲染为正确小会话语义，理论 churn 自愈，不改代码）
- [x] 8.5 【Minor】`memory/types.go` Snowflake 生成器加时钟回拨防护（`ts <= last` 时钉住）——渲染冻结对 key 单调性是硬依赖
- [x] 8.6 【Nit】整理轮日志补折叠后实际输入估算（Debugf）
- [x] 8.7 验证：`go test ./... -count=1 -p 1` 全绿（含 real-LLM；并行全量跨包干扰已确认为 tmux/长跑资源冲突，串行为基准）

## 9. recall 统一单入口（D7）

- [x] 9.1 `tool/recall`：新建统一 `recall` 工具——InputSchema 超集（items/query+filters/turn_key/orchestrate），路由复用既有实现（memory_recall 的 items/query 分流、memory_turn 的因果链回走、orchestrate 分支升级 RecallAgent）
- [x] 9.2 注册面收敛：`recall_subtools.go` 新增 `recall` 注册，退役 `memory_recall`/`memory_turn` 独立注册名；recall 子 agent 五子工具不再注册为可装配项（保留为编排引擎内部实现）
- [x] 9.3 装配更新：`examples/wechat-bot/tagent.yaml` 主 agent 工具清单收敛为单条 `- kind: tool, id: recall`（移除 memory_recall/memory_turn 挂载与 `- agent: recall` 挂载）
- [x] 9.4 文案同步：卡片行/归档通知/占位符等框架文案中的召回指引统一为 `recall`（含空摘要占位符"可用 recall 检索"复核）；README 工具清单更新
- [x] 9.5 测试：路由分流断言（items 纯函数/turn_key 因果链/orchestrate 升级），既有 memory_recall/memory_turn 测试迁移至统一入口形态；契约测试 C2/C3 改用 `recall` 工具声明
