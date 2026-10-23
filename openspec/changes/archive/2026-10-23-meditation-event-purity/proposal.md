# meditation-event-purity：冥想事件纯化——归核·事件驱动·跨会话写模型决策化

## 裁定（用户明示）

冥想机制静默向用户会话投递是 bug 不是 feature。跨 session 写事件必须是模型决策；机制不得代替模型决定"把什么写进别人的会话"。

## 事件生产消费链现状与病灶

生产者（谁写链）／判定：
1. 宿主渠道 Drive→InjectEvent→ExternalInput(user)——走链 ✓
2. 工具调用事实——走链 ✓
3. 通知 commitNotice→Notice 事实（幂等key）——走链 ✓
4. 冥想反思注入 InjectReflection→InjectEvent→链——走链 ✓
5. 冥想产出→freshAssistant→DeliverFunc→sender.Send——绕链 ✗（bug：用户会话链无记录/呈现权被剥夺/内容成色无判断——"本轮无值得回流的结论"也被机制发出，2026-10-21 03:41 实发）

驱动：
- 用户回合：宿主 Drive ✓
- 冥想回合：host.go 的 hostReflection ticker（宿主级 cron）✗ 应归核＋事件驱动

消费者：装配 AssembleBudgetPrefix／Drive 头 NoticeFeed／观察面 ObservedFacts ✓

## 修法（三件事一体）

### M1 冥想归核＋事件驱动
- hostReflection 的 ticker/驱动逻辑从 host.go 移入 evolution/meditation（核心自持）
- 触发改事件驱动：冥想消费器以 PostCommitObserver 端口入流——事实提交推进计时器 state，成熟条件（水位后有新素材＋interval 期满＋min_gap 满足）即 fire
- 无 ticker：无新事件＝无新素材＝不反思（新颖性语义下 cron 轮询是多余的）
- host.go 只留"解析冥想声明＋挂载驱动"

### M2 跨会话写＝模型决策（删机制静默投递）
- 删：DeliverFunc/DeliverReflection/WithDeliverer/hostDeliverer 全链＋tick 内 freshAssistant→deliver 机制投递
- 增：冥想工具 deliver_summary(content)——模型在反思回合内显式调用（决策权归模型）：
  - 执行：对 deliver_to 声明的每个 agent 的活跃绑定会话，commit 一条 TypeExternalInput＋TriggerSource="meditation" 的 summary 事件落目标会话链
  - 授权：目标面由 deliver_to 声明圈定（模型只决定发不发＋内容，不指定任意目标）
  - 衔接压缩：summary 事件进目标会话投影，按预算降级（Full/Ticket/Summary）免费获得
- meditator 原 send 工具死路（会址未绑定）随 DeliverFunc 删除一并清理

### M3 wechat 适配
- 删 wechat.go 的 DeliverReflection 实现
- tagent.yaml meditator 工具面：send→deliver_summary；提示词更新（无结论不产出正文不调工具；有结论显式调 deliver_summary）

## 防循环（已有机制不动，验证补锚）

1. NoveltyPolicy 闭表：meditation 戳 ∉ hostFacingStamps → summary 事件不触发新反思
2. 水位窗：Advance 后只读严格晚于水位的事件
3. min_gap 物理限流兜底

## 协议（冻结——两单并发前提）

- evolution/meditation（归核后核心自持）：Driver 实现 plugin.Instance+PostCommitObserver——ObserveCommit(evt) 推进计时器→成熟即 fire
- EventSink 跨会话写落链口：CommitToSession(ctx, addr, evt) (EventID, error)
- 冥想工具 deliver_summary：args {content string}／目标=deliver_to 白名单活跃会话
- 删除面：DeliverFunc、WithDeliverer、DeliverReflection（wechat）、cfg.hostDeliverer

## 验收

- 负锚（硬门）：反思完成后无模型调用 deliver_summary 时零跨会话事件（机制不投递）
- 正锚：模型调 deliver_summary → summary 事件落目标会话链 → 目标会话下轮装配可见
- 防循环锚：summary 事件被冥想观察面扫到 → no-novelty（不触发新反思）
- 38 包全绿＋wechat 测试绿＋golden match＋lint ok
