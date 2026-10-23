# event-typing：事件类型机械判定收口＋冒烟第二波（九机制补覆盖）

## 裁定（用户）
1. 长程冒烟未覆盖所有机制——重派补齐
2. 事件信息前缀的判断和处理应改为**事件类型判定**——类型是机械的（Type 常量），
   插件/消费逻辑明确指定处理面；文本只是呈现层

## 前缀协议违例（生产级，三处）
| 位置 | 违例 | 正解 |
|---|---|---|
| age_segments.go:338 | 折叠产物 Content 带 "history summary: " 前缀；二次折叠剥前缀还原结构 | 投影条目已有 Type（ProjectionTypeHistorySummary）——再折叠按 **Type** 取材料；Content 不编码类型前缀 |
| host.go:531 | fireNoticeTurn 把结构化 Notice（Kind/TaskID 俱在）重格式化为文本提交 | 提交的事件带**机械 Meta**（kind/task_id 键）；noticeText 仅作模型 transcript 呈现 |
| loop.go:374 | MetaKeyEventSummary＝"tagent: "+kind 文本拼接 | Meta 存机械字段（kind/task_id），呈现渲染放呈现层 |

原则：**生产消费链零文本解析**——Type/Meta 判定；模型读的 prose 由呈现层从类型化事件渲染。

## 冒烟第二波（九机制）
recall 四工具（scripted embedding）／skill_search·load／mcp_call（本地假 MCP 或降级）／
exec＋final_round 收口（超轮→closing round）／模型 5xx→回合失败→用户降级文案／
reload 坏模型→replacement refused→旧版继续／审批 TTL 过期→未批准视域／
ChainExporter 导出对账（冒烟数据→样本行）／call_subagent 显式源运行时组装

## 验收
- 生产链 grep 零前缀解析（age_segments/host/loop 的三处消除；测试对呈现文本断言不属违例）
- 冒烟 12+9 项全绿＋既有测试回归＋golden match＋lint ok
