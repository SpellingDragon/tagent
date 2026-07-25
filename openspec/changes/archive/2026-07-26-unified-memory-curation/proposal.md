# Proposal: unified-memory-curation

## Why

tagent 的长期运行（数天、数万事件）依赖记忆系统,但当前存在三条平行摘要管线（SummaryPlugin 入库机械提取、SmartCompressor 压缩时全文重摘、冥想回顾后即被压掉）,彼此不复用、成本随历史总量增长,且压缩后模型对更早历史**只剩 key 清单、没有连续叙述**——盲 recall 是唯一回补手段。实机取证（2026-07-25,10 万 token 级会话）:单次压缩输入 103k tokens 全文重摘;EventSummary 对多数事件就是原文（无摘要价值）;冥想总结"说了就忘"。

同时,异步任务层缺少"重入"原语:alive-detached 的 tmux 服务会话无法被继续输入（SendKeys 仅内部心跳使用）;sub agent 续跑需要 LLM 自觉手工还原上下文,无框架保障。两者合流于同一命题:**长生命周期对象（历史/会话/任务）如何被后续 turn 低成本重新触达**。

## What Changes

围绕记忆三原语（store=记录、compress=总结+遗忘、recall=回忆）统一固化与重入机制,分两波次落地:

**波次 A（无哲学争议、先收益）**
- **总结引擎收口**:内容级总结收归 compress 固化时刻,素材律=下层固化物（事件原文→段摘要→卡片行→浓缩卡片）;SummaryPlugin 退位为元数据标注
- **卡片序列**（历史表示的唯一对象）:住在滚动 summaryRef 里,工程化提取为常态（零 LLM/零漂移）,超 `card_max_chars` 时 LLM 整理为浓缩卡片,无 SummaryModel/失败时工程沉底——与 SmartCompressor"轻手段先行、重手段兜底"元模式同构,降级不塌;**无 pinned/无投影特例规则**
- **段摘要挂链+缓存**:挂 RelationStore（recall_trace 可达）;同段跨轮不重摘;固化物豁免 TTL（原文可忘、固化物长存）
- **冥想闭环**:冥想总结以高亮卡片行沉淀（零 LLM 成本）
- **recall 协议化**:索引卡=召回标准票据——新增主 agent 直持的纯函数工具 `memory_recall`,输入形态分流（items 含 key→工程化精确召回;query→语义召回,检索层可独立演进）;RecallAgent 保留但定位收窄为复杂检索/多跳编排
- **resume_task（tmux）**:服务会话重入原语,SendKeys+输出基线,复用 spawn 生命周期

**波次 B（依赖 A 的挂链）**
- **resume_task（subagent）+ 任务链还原器**:本任务前序轮次链（上次 settle 结果为首）→external_context,框架代顶层还原（子写顶读顶编排不破）;RelationStore 固化物回溯为预留接缝（待 resultRef 桥）
- 所有新约束并入既有 compress 家族配置（净新增 `card_max_chars` 1 字段,开关复用 `summary_model`）

## Capabilities

### New Capabilities

- `memory-curation`: 固化级联与素材律、卡片序列（历史表示唯一对象/工程增长/超限 LLM 整理/降级沉底/TTL 豁免）、冥想高亮卡片闭环
- `recall-protocol`: 索引卡为召回标准协议（memory_recall 纯函数分流,RecallAgent 收窄）
- `context-scoping`: resume 任务链还原器、段摘要因果链挂载
- `task-reentry`: resume_task 重入原语（统一生命周期,tmux/subagent 特异出入口,输出基线）

### Modified Capabilities

- `value-driven-compression`: 摘要素材从渲染全文改为下层固化物;摘要保留关联标识约束不变
- `batched-summarization`: 批量摘要的输入变为固化物级联素材;段摘要产出增加因果链挂载与缓存(同段不重摘)
- `async-task-execution`: 任务状态机新增 alive-detached --resume--> running 边;任务工具族加入 resume_task
- `meditation-self-state-digest`: 冥想总结进入叙述刷新素材流

## Impact

- **代码**: `agent/smart_compress.go`(总结引擎收口)、`agent/context_compressor.go`(叙述 pinned ref/装配)、`agent/context_manager.go`(ctxOf 抽取)、`agent/task_manager.go`+`agent/tool_agent.go`+`tool/action/`(resume)、`plugin/summary_plugin.go`(退位)、`memory/`(固化物挂链)、`config.go`(新约束配置)
- **投影不变量**: 卡片序列住在滚动 summaryRef 内,**无 pinned/无任何投影特例规则**（I1-I3 不加排除分支）
- **兼容性**: 无 SummaryModel 时卡片工程沉底=现状滚动行为,零回归;event_summary 消费端（main.go 展示/recall 列表）语义不变
- **风险**: 浓缩卡片失真（素材已是高密度卡片行、仅超限才 LLM 介入一次、prompt 强制保留 key 引用+recall 兜底）;resume 并发 send 拒后到;tmux 输出基线
