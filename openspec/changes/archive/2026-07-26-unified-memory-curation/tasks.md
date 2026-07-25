# Tasks: unified-memory-curation

按两波次组织:波次 A 无哲学争议、先落地先收益;波次 B 依赖 A 的挂链。每组完成后跑全量测试。

## 波次 A

### 1. 固化物基建（挂链 + 缓存 + TTL 分层）

- [x] 1.1 `archiveSegment` 产出挂 RelationStore 因果链（parent=段尾事件 key）,固化物记录来源 key 集合;单测:沿链回溯
- [x] 1.2 段摘要缓存（segEventKey→summaryKey）:同段跨轮不重摘、已归档段不进评估批次;单测:两轮压缩 LLM 调用次数断言
- [x] 1.3 固化物 TTL 豁免（段摘要/浓缩卡片）,原文按既有 lifecycle TTL;单测:TTL 清理后固化物仍在
- [x] 1.4 SummaryPlugin 退位:注释/文档明确 `event_summary`="原文截断视图",StateDelta 键名与消费端不变

### 2. 卡片序列（历史表示唯一对象,无投影特例）

- [x] 2.1 卡片行工程化提取（Compact 时刻,零 LLM）:有 SummaryModel 取段摘要首行;无则取段边界事件摘要首行+任务层元数据;并入滚动 summaryRef 合并逻辑（buildRetainedRefs 扩展）;单测:两路提取
- [x] 2.2 超限 LLM 整理:序列 > `card_max_chars` 时浓缩旧卡片行（prompt 强制保留任务骨架+关键 key 引用）,新序列=[浓缩卡片]+[新行];失败/无模型工程沉底;`card_max_chars` 配置化（yaml→AgentConfig→agent 包,0=包默认 DefaultCardMaxChars）;单测:整理/沉底/key 引用保留
- [x] 2.3 冥想闭环:冥想总结固化时写高亮卡片行（★ 前缀,零 LLM）;单测
- [x] 2.4 不变量测试:I5（素材律,引擎拒收越级素材）/I6（多轮压缩恒有界,投影无特例——I1-I3 不加排除分支）

### 3. recall 协议化（memory_recall）

- [x] 3.1 `memory_recall` 纯函数工具:items（GetEvent 批量/原序/hint 回显/未命中明确报告）+ query（QueryOptions 现状检索）分流,items 优先;输出协议统一;工具注册（plain tool 工厂）
- [x] 3.2 主 agent 挂载（example yaml）+ prompt 引导（recall_tool_desc/TOOLS.md:票据在手→memory_recall,复杂检索→recall agent）
- [x] 3.3 单测:分流四制（items 精确/query 语义/同时提供 items 优先/未命中报告）+ 卡片行票据无损性（key 可抠出构 items）

### 4. resume_task（tmux）

- [x] 4.1 Task 层 `resume_task(id, input)`:状态机 alive-detached→running(dense) 边,复用 dense→ACK→settle,同一 task id;非法状态明确错误;注册进任务工具族
- [x] 4.2 tmux 特异:SendKeys + capture 基线增量输出;IsTUI 拒绝;同会话并发 resume 拒绝后到;单测:状态机+基线
- [x] 4.3 e2e:tmux 异步链路实机通过（pty 池修复后 tool/action 全量+TestRealLLM_AsyncTask 全绿,零 session 残留）

### 5. 波次 A 收尾

- [x] 5.1 全仓测试 + race_check.sh;真实 LLM e2e（多轮 Compact 后卡片序列含早期事实且可 memory_recall 回补）

## 波次 B

### 6. resume_task（subagent）+ 任务链还原器

- [x] 6.1 AgentToolWrapper 任务链还原器:任务本地轮次链（上次 settle 结果为首）→ external_context;沿 RelationStore 回溯固化物的增强留接缝（待 task.resultRef 桥接 settle 事件 key）;单测:只含相关任务内容
- [x] 6.2 subagent resume=新 Run + 还原器注入;单测（mock）完成;e2e（真实 LLM plan resume）留待实机验证一并覆盖

### 7. 收尾

- [x] 7.1 全仓测试 + race_check.sh;真实 LLM e2e 全量
- [x] 7.2 文档:README 双语（三原语/卡片序列/recall 协议/resume）、example yaml 配置示例与 prompt 引导
- [x] 7.3 openspec validate --strict 通过;实机 tmux/异步链路验证通过后归档（wechat-bot 长跑对账转日常运营观察项）

## 实机反馈追加（会话回收闭环）

- [x] 8.1 ActionTool.Close 收编存活 session（优雅退出即回收,不再留孤儿到下次启动）
- [x] 8.2 启动孤儿清扫 CleanupOrphanSessions（上代实例崩溃/强杀残留的 tagent-* session 开机即收;多实例场景 WithOrphanCleanupDisabled 或独立前缀）
