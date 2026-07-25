# Design: unified-memory-curation

> 修订记录:初版 D2 采用"pinned 演进叙述"（投影特殊 ref+独立刷新）。整体评审否决:pinned 需要四条特殊规则（恒首位/免 Replace/免计数/测试排除）,违背"无例外"哲学;且叙述作为常驻认知层有漂移风险、开关与成本争议。终版统一为**卡片序列**:历史表示只有一个对象（滚动 summaryRef 内的索引卡行序列）,工程化增长为常态,LLM 整理仅在超限时触发（与 SmartCompressor"轻手段先行、重手段兜底"元模式同构）,降级路径永不塌。
>
> 实施评审修订（apply 后 CodeReview,已修）:① resume 状态门放宽——原仅 alive-detached 使 subagent resume 不可达（其 detector 只发 completed）;合法源状态={alive-detached, stable, completed, failed},running/suspect/cancelled 拒绝。② 并发 resume 占坑原子化（task.mu 内置 running 再调 ResumeFn,失败回滚）;Cancel 读 detector 加锁。③ 旧 watch 以 watchDone 退役（防 goroutine 泄漏与陈旧信号串轮）。④ 浓缩卡 LLM 输出单行化（防跨轮解析丢行）;固化物容量淘汰同豁免;归档缓存封顶。
>
> 架构收敛（二轮评审后,用户定调"完美收敛"）:tmux 换绑方案（新 detector+RebindSessionCallback+顺序纪律）暴露了 baseline 竞态与一组附带机制,根源是"detector 绑轮次"与"tmux 会话永生"的错配。终版:**detector 绑会话**,resume 仅 `Rearm`（新基线+新 detach 窗口,锁内轮次态）,回调/watch 永不换手——换绑/顺序/竞态面整组删除;任务层同 detector 则不退役 watch（一行判断）,新 detector（subagent 每轮新 Run）仍走 watchDone 退役。同时补齐两项硬限制配置化:resume_context_rounds（agent 级）、compress.archive_cache_cap。

## Layer 1: Problem & Requirements

### 问题

1. **三条平行摘要管线互不复用**:SummaryPlugin（入库,机械,多数=原文）、SmartCompressor L2/L3（压缩,全文重摘,103k tokens 实测）、冥想（回顾,产物被压掉）。同一信息被处理多次,成本随历史总量增长。
2. **压缩后历史只剩 key 清单**:模型对更早历史无感知目录,只能盲 recall;长期运行下"做过什么"不可见。
3. **任务无重入原语**:alive-detached tmux 会话无法继续输入;sub agent 续跑靠 LLM 自觉手工还原上下文（实机:主 agent 为此浪费十几轮）。
4. **段摘要固化物未挂因果链**:recall_trace 无法从摘要回溯原文。

### 需求

- R1 内容级总结单点化,素材律=下层固化物,成本 O(增量) 而非 O(历史总量)
- R2 历史表示=**卡片序列**（滚动 summaryRef 内）:工程化增长（零 LLM/零漂移）为常态,超限时 LLM 整理,无 SummaryModel 时工程沉底,行为有界且降级不塌
- R3 冥想总结以高亮卡片行沉淀（零 LLM 成本闭环）
- R4 sub agent resume 时框架自动从 memory 还原任务链上下文（还原器,非过早抽象的"统一契约"）
- R5 resume_task 统一重入,复用 spawn 生命周期,tmux/subagent 特异实现
- R6 新约束全配置化且并入既有 compress 家族;无 SummaryModel 时零回归
- R7 索引卡为 recall 标准协议:输入形态分流——接到卡片条目（含 key）走工程化精确召回,自由文本走语义召回;两路均为主 agent 直持的纯函数工具（确定性路径上无 LLM 中间层）

### 不变量（在 unified-event-projection I1-I4 之上新增）

- I5 **素材律**:第 N 层总结的素材恒为第 N-1 层固化物（原文→段摘要→卡片行→浓缩卡片）,唯一例外是第 0→1 层（段内事件原文）
- I6 **历史表示有界**:滚动 summaryRef 的渲染长度恒有界（卡片序列 ≤ card_max_chars,超限必整理或沉底）;投影无任何特殊 ref/例外规则
- I7 **固化物可追溯**:段摘要 SHALL 挂 RelationStore 因果链并保留来源 key 集合;浓缩卡片 SHALL 保留其吸收的关键固化物 key 引用

## Layer 2: Solution Design

### D1 记忆三原语与总结引擎收口

```
store    = 事件入库(原文,不可变,挂链)                ← 现状保留
compress = summarize(固化入库+挂链) + forget(投影移除) ← 本变更收口
recall   = 按 key/时间/keyword 回补                  ← 现状保留,检索层可独立增强
```

- 固化级联:`事件原文 →(L3,唯一全文接触点)→ 段摘要 →(工程提取)→ 卡片行 →(超限时 LLM)→ 浓缩卡片`。
- **SummaryPlugin 退位**:保留类型推断与元数据注入,`event_summary` 语义明确为"原文截断视图"（改注释与 spec,不改字段名——消费端零波及）。
- 段摘要缓存（segEventKey→summaryKey）:同段跨轮不重摘;已归档段不进评估批次。
- **固化物 TTL 分层**（推演决策）:原文按既有 lifecycle TTL 自然遗忘;固化物（段摘要/浓缩卡片）豁免 TTL——"原文可忘,固化物长存"。索引卡指向的段摘要因此恒可达。

**Alternatives**:① 每事件入库 LLM 摘要——成本×事件数、无上下文,否决;② 压缩时用 EventSummary 当摘要素材——EventSummary 多数=原文,不成立,否决。

### D2 卡片序列（历史表示的唯一对象,零特殊规则）

历史表示 = 滚动 summaryRef（现状机制）的 EventSummary,内容为:

```
[Compacted N 累计] + [卡片行序列] + [recent keys=...(现状,有界)]

卡片行(工程化提取,Compact 时刻,零 LLM):
  - 07-25 18:20 [sA] plan: 深度分析 tagent ✓completed
  - ★ 07-26 03:00 冥想回顾: ...          ← 冥想=高亮卡片行
素材:L3 段摘要首行;无 SummaryModel 时取段边界事件 EventSummary 首行+任务层元数据
     (settle 状态/task desc)——工程提取永远成立,降级不塌

生命周期(与压缩元模式同构):
  常态   = 新行机械追加(零成本零漂移)
  超限   = 序列 > card_max_chars → LLM 整理:旧卡片行(第2层固化物)→浓缩卡片(第3层),
           新序列=[浓缩卡片]+[新行];浓缩 prompt 强制保留任务骨架与关键 key 引用
  降级   = 无 SummaryModel/整理失败 → 工程沉底("更早 n 项(recall 可查)",现状滚动行为)
```

- **无 pinned、无投影特殊规则**:卡片序列就住在滚动 summaryRef 里（buildRetainedRefs 已有的合并逻辑扩展）,天然在投影头部,I1-I3 无需任何排除。
- 渲染形态不变:user 级〔历史归档〕注记（role 规则一致）。
- 构建时机 = Compact 时刻（信息离开窗口前提取,全文/段摘要都在手上）——"固化"的定义本身。

**Alternatives**:① pinned 演进叙述（初版）——四条特殊规则+常驻认知层漂移,否决;② 纯 key 清单（现状）——模型对历史无目录,只能盲 recall,否决;③ 每轮 LLM 刷新叙述——成本与漂移均高,仅超限触发已覆盖需求,否决。

### D3 resume 上下文还原器（sub agent）

- AgentToolWrapper 新增任务链还原器:resume 时沿 RelationStore 从任务关联 key 回溯固化物 + 上次 settle 结果 → 注入 `external_context`。
- 定位为 **resume 的还原器**,不升华为"ctxOf 统一契约"——主 agent 装配/recall 已各自就位,过早抽象无收益;若未来出现第三个 scope 再抽象。
- "子写、顶读、顶编排"不破:子 agent 仍是单 turn 原语,还原是框架代顶层做的工程化喂入。

### D4 resume_task 重入原语

```
状态机: spawn → running(dense) → ACK → alive-detached ──resume(input)──▶ running(dense) → ...
                                  └────────── settle → task_settled 通知(同一 task id) ────┘
```

- 工具:`resume_task(id, input)`,注册于任务工具族;生命周期完全复用 spawn 的 dense→内联/ACK→settle;非 alive 状态 resume → 明确错误引导（relaunch 或新调用）。
- **tmux executor**:detector 绑定会话而非轮次——resume 仅 `Rearm`（新输出基线+新 dense 窗口）并 `SendKeys`,回调与 watch 永不换手（无换绑、无顺序纪律、无陈旧信号面）;IsTUI 拒绝;并发 resume 由任务层占坑单胜。
- **subagent executor**:新 Run + D3 还原器注入;无进程复活。

**Alternatives**：① steering——违背单 turn 原语,否决;② resume 开新 session——丢服务会话状态,否决。

### D6 recall 协议化（索引卡=召回票据）

```
memory_recall(纯函数工具,主 agent 直持,零 LLM 中间层):
  输入形态分流(协议核心,items 优先):
    items: [{key, hint?}]  → 工程化精确召回: GetEvent 批量,原序回补,零幻觉
    query + filters?       → 语义召回: QueryOptions(keyword/time/type,现状)
                             检索层可独立演进(keyword→向量),入口协议不变
  输出协议统一: 条目 {key(hex), type, summary, content, time}

RecallAgent(sub agent) 保留,定位收窄: 复杂检索/多跳编排(trace 等),子工具不动
```

- **分流规则**:接到索引卡表示的事件列表（items 含 key）→ 自动工程化召回;否则（仅 query）→ 语义召回。卡片行的 `[hex]` key 形态即票据,无需改格式。
- **触发保持显式**:协议简化的是调用形态（一个工具两态参数）,不是调用时机——不做"模型文本提及 key 就隐式换出"（提及≠需要回补,隐式触发成本不可控且不可审计）。
- **hint 透传**:items 里的 hint（卡片行描述）原样回显,供模型对账"取到的是不是我要的"。
- **首次给主 agent 纯函数记忆工具**的哲学说明:卡片票据召回是"顶读"的直接形态,本就该是顶层肌肉而非转包;确定性路径上不应有概率性组件（实机实证:经 RecallAgent 绕行多一次 ReAct、多一个幻觉面、多一个空转失败模式）。
- **命名**:纯函数工具名 `memory_recall`（RecallAgent 保留 `recall` 名,零破坏;与 knowledge 子工具 memory_query 不同 agent 上下文,无冲突）。

**Alternatives**:① 召回继续全部经 RecallAgent——确定性路径塞概率组件,否决;② 隐式自动触发（框架扫描模型输出中的 key 自动回补）——成本不可控、不可审计,否决;③ 替换 RecallAgent——trace 等多跳编排仍需 LLM,保留收窄定位。

### D5 配置化（并入 compress 家族,净新增 1 字段）

- **开关 = `compress.summary_model`（既有,零新增）**:一切 LLM 辅助行为（L3 摘要/卡片整理）的共同开关;无则全部工程降级,行为等于现状。
- **`compress.card_max_chars`（唯一新增）**:卡片序列长度阈值,chars 量纲（与 `chunk_summary_len`/`max_*_chars` 家族一致）,0=包默认（DefaultCardMaxChars）。
- 不设刷新时机/行数等旋钮:超限即整理、失败即沉底,唯一合理行为（YAGNI）。

```yaml
agents:
  tagent:
    compress:
      summary_model: glm-4-flash   # 既有:所有 LLM 辅助的共同开关
      card_max_chars: 6000         # 新增:卡片序列阈值(chars,0=默认)
    # resume 无新配置:复用 monitor/dense 既有配置
```

## Layer 3: Constraints & Rules

- 卡片整理失败 MUST 不影响 Compact 主流程(降级沉底)
- 浓缩卡片 MUST 保留任务骨架与关键固化物 key 引用(recall 链不断)
- memory_recall MUST 为纯函数（无 LLM 调用）;items 与 query 同时提供时 items 优先;未命中的 key MUST 明确报告（不静默省略）
- resume 的 ACK/settle 事件 MUST 携带同一 task id
- SummaryPlugin 退位 MUST NOT 改变 StateDelta 键名与消费端行为
- 素材律 I5 唯一例外=第 0→1 层;固化物豁免 TTL,原文按既有 TTL
- 投影 MUST NOT 引入任何特殊 ref/例外规则(I1-I3 测试不加排除分支)

## Layer 4: Testing Strategy

- 单测:卡片行提取(有/无 SummaryModel 两路)、超限整理(素材=卡片行,产出含 key 引用)、整理失败沉底、段摘要挂链回溯、同段不重摘(LLM 调用次数断言)、固化物 TTL 豁免、memory_recall 分流(items 精确/query 语义/items 优先/未命中报告/hint 回显)、卡片行票据无损性(key 可被抠出构 items)、resume 状态机(alive→running→settle;非法拒绝)、tmux 输出基线、subagent 还原器(只含相关任务内容)
- 不变量:I5(引擎拒收越级素材)/I6(多轮压缩恒有界)/I7(可追溯)入 invariants_test
- e2e(真实 LLM):多轮 Compact 后卡片序列含早期任务事实且可 recall 回补;tmux resume 取增量输出;plan resume 引用上次结论
- 实机:wechat-bot 长跑,卡片序列 vs MemoryStore 对账(零漂移验证:浓缩前逐条可对账)
