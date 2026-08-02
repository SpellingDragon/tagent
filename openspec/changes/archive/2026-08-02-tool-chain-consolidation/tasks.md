# tool-chain-consolidation — 实现任务

## 1. D1 工具调用摘要（根治空摘要）

- [x] 1.1 `GenerateEventSummary`：thinking_plan 且 Content=="" 且 len(ToolCalls)>0 时，用 ToolCalls 生成 `调用 <工具名>`（多个顿号连接）；新增 `formatToolNames` helper
- [x] 1.2 测试：纯工具调用 thinking_plan → EventSummary="调用 read_file、grep"；Content 非空不受影响

## 2. D2 工具链折叠（核心）

- [x] 2.1 新增 `TypeToolChain` 事件类型（"tool_chain"）
- [x] 2.2 `foldToolRuns`：对投影 refs，识别连续 ≥2 条老化（full=false）工具事件（thinking_plan/action_command，不被 external_input/agent_output 打断），折叠为一个负 key 的 tool_chain 合成引用（EventSummary=`- 工具链: names（N步）[evt_first→evt_last]`，工具名取自 ref.EventSummary）；接入 ContextCompressor.Compress（resolveRef 之前对投影 refs 执行）
- [x] 2.3 `resolveRef`：TypeToolChain 渲染为 user 侧一行 `- 工具链: …`
- [x] 2.4 `buildRetainedRefs`：TypeToolChain ref 一律保留（不进 priorCount/compressedKeys/折叠）
- [x] 2.5 测试：折叠（3 对→1 行含工具名+票据）、不跨边界、渲染、投影保留

## 3. D3 活跃前沿保护

- [x] 3.1 full=true 近期消息、未完成 tool_call（无 result）、边界事件不折叠（foldToolRuns 跳过 fullFrom 之后与非完整对）
- [x] 3.2 测试：近期工具对保持原生（含 tool_calls）；配对合法性不破

## 4. 不变量与收尾

- [x] 4.1 测试：长进行中段上下文有界（I1）、无 `(历史事件摘要为空…)` 占位符（I2）、tool_chain ref 可经 memory_turn 取回完整链（I4）
- [x] 4.2 全量验证：`go build ./...`、`go vet ./...`、`gofmt` 干净；`go test -short ./...` 相关包绿；关键测试 fail-before/pass-after
- [x] 4.3 `openspec validate tool-chain-consolidation --strict` 通过

## 5. Code Review 修复（零基审查后补充）

> 来源：实现完成后的零基审查（0 Blocker / 2 Major / 1 Minor / 2 Nit）。负 key 碰撞与票据误解析经实证为安全。

- [x] 5.1 **M1 散文泄漏**：thinking_plan 带散文摘要（reasoning 模型 think-then-call）被当工具名拼进链行 → `extractToolNameFromSummary` 只认"调用 "前缀，散文返回空；`TestFoldToolRuns_NoProseLeak`
- [x] 5.2 **M2a 链行线性累积**：每轮新老化对生成新链（65 步→~60 链）→ 相邻连续链合并为一（`mergeToolChainRef` 解析扩展名/步数/票据）；`TestFoldToolRuns_MergesContiguousChain`
- [x] 5.3 **M2b 链行永不退役**：段被 L3 归档后链行仍滞留投影 → `retainedChainKeys` 仅保留消息存活的链，归档链退役（完整链仍可经 memory_turn 取回，I4）；`TestBuildRetainedRefs_RetiresArchivedChain`
- [x] 5.4 **m1 toolSteps 死代码**："含 N 步"提示逻辑已被 tool_chain 取代且残余触发计数失真 → 删除 toolSteps 计数与提示拼接
- [x] 5.5 **n1 票据格式**：统一为 `evt_` 前缀（与设计文档一致）
- [x] 5.6 全量验证：build/vet/gofmt 干净；全 20 包测试绿
