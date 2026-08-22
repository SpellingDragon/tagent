## 1. task_settled 全量保真（D1）

- [x] 1.1 `agent/event_bus.go`：`newTaskSettledEvent` 删除截断分支与 `maxInline` 参数，结果全文直入 Content；删除 `get_task_result` 截断提示文案
- [x] 1.2 删除配置链：`agent/event_bus.go` 的 `DefaultTaskSettledMaxInline`、`agent` 配置结构体 `TaskSettledMaxInline` 字段、`tagent.go`/`agent.go` 的穿线、`config.go` 的 yaml/json 键
- [x] 1.3 迁移测试：`agent/task_settled_test.go` 的截断断言（`TestNewTaskSettledEvent_LargeResultTruncated`）改为全量断言（大结果 Content=全文、无截断标记、无工具提示）
- [x] 1.4 新增断言：全量 settle 事件经 `memory_recall(items=[key])` 召回取到全文（TTL 无关，票据=evt key）

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
