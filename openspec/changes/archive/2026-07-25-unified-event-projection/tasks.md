# Tasks: unified-event-projection

## 1. 不变量测试先行（红）

- [x] 1.1 新增 I1 写入统一测试：事件序列过插件管线后，store 与投影一一对应、每 EventKey 恰好一次（当前应失败：投影写在消费侧）
- [x] 1.2 新增 I2 时序测试：模拟 ReAct 迭代，断言第 K+1 次 BeforeModel 渲染已含第 K 次 tool result（当前靠碰巧）
- [x] 1.3 新增 I3 渲染合法性断言辅助函数（无 role=tool、无空 agent_output、EventKey 不重复）+ I4 边界单向测试（往框架消息尾部注入垃圾，BeforeModel 输出不变；当前应失败）
- [x] 1.4 新增元数据完整性测试（投递事件含 trigger_source/存储标识）与解析 API 占位测试

## 2. D1 投影写入统一进插件管线

- [x] 2.1 RunFlow 入口将本 ContextManager 的 projection 绑定到 ctx（context value);sub-agent 路径同样绑定
- [x] 2.2 MemoryPlugin.OnEvent 在 store 成功后原子 Append 到 ctx 中的 projection（无 ctx 则跳过）;partial(IsPartial）事件跳过（D8)
- [x] 2.3 移除消费侧 onEvent 的投影 Append 职责（保留元数据传播/冥想计时/投递）;Session AppendEventHook 的投影职责随之移除（保留其 outputCh 转发）
- [x] 2.4 验证 I1/I2 转绿；全量回归

## 3. D2 装配单行化（删除读回）

- [x] 3.1 BeforeModel 装配改为 `[system] + render(投影) + 任务看板`；删除 `extractCurrentTurnMessages`、`filterUser` 及其调用
- [x] 3.2 删除历史启发式残留（session 回显过滤注释/逻辑）;persistBusEvent 的 bus 注入路径保留（TryPull 中转入投影）
- [x] 3.3 验证 I4 转绿；全量回归 + e2e

## 4. D3 配对自由渲染

- [x] 4.1 重写 resolveRef 渲染规则：action_command → input 事件文本（工具名+短标识）;thinking_plan → assistant 文本（tool_calls 转文本描述）;external_input 通知类 → 带类别前缀文本；不再产生 role=tool
- [x] 4.2 移除 H2 的 ToolID 降级特判（配对消失后无意义）;FullEvent.ToolID 字段保留但不再用于渲染配对
- [x] 4.3 定稿通知/应答文本模板（类别前缀、task id 格式）并写进渲染测试
- [x] 4.4 验证 I3 恒过；全量回归 + e2e（真实 LLM 确认 ReAct 连续性）

## 5. D4 元数据契约

- [x] 5.1 agent 包定义元数据 key 常量（event_key/partition_id/event_type/event_summary/trigger_source/meta_ 前缀）;插件/RunFlow/onEvent/event_bus/task_manager 全部改引用
- [x] 5.2 提供 `ParseEventMeta` 解析 API 与路由助手；examples/wechat-bot 改用 API（删除裸字符串键）
- [x] 5.3 验证元数据完整性测试转绿；全量回归

## 6. D5/D6 删除退化机制

- [x] 6.1 最后一轮全仓 agent_output bus 消费审计（确认均为过滤）后，删除 `echoFinalResponse` 及空 final 抑制（H1 存储卫生保留）
- [x] 6.2 删除 `message_validate.go`(L2 repair）及其 BeforeModel 挂载；保留 L1 投影幂等
- [x] 6.3 全量回归 + e2e（断言循环无空转唤醒、渲染合法性恒过）

## 7. D7 压缩关联标识保留

- [x] 7.1 摘要生成（规则/LLM prompt）增加"保留 task id/工具名等关联标识"约束与测试
- [x] 7.2 全量回归 + 压缩相关 e2e

## 8. 收尾

- [x] 8.1 更新受影响单测（依赖 role=tool 渲染/echo/L2 的用例）
- [x] 8.2 真实 LLM e2e 全量复跑（async 投递、渲染断言、无空响应、无孤儿）
- [x] 8.3 openspec validate --strict；评估归档 async-result-delivery 与本变更的关系后归档

## 9. 实机验证驱动的设计修订（D3 v2，见 design.md 修订记录）

- [x] 9.1 D3 v2：回合内原生渲染（assistant+ToolCalls / role=tool 配对），孤儿渲染期降级 demoteToInputNote；I3 翻转为合法原生序列断言
- [x] 9.2 看板改为 user 级独立虚事件（系统观察快照声明，防模仿）
- [x] 9.3 退化空 turn 一次重试 + 空 final 取证日志（reasoning/finish_reason/error）
- [x] 9.4 sub-agent 同步/异步配置项（ToolRef.Async，async:false 强制同步）
- [x] 9.5 P0：雪花键符号位 bug（partition≥1024 产负 key 致全链路失明）——partitionIDMask 11→10 位 + AlwaysPositive 回归测试
- [x] 9.6 EventKey canonical 形态改 16 进制字符串（FormatEventKey/ParseEventKey 单点，前缀/压缩产物/StateDelta/recall 全链路）
