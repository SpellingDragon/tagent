# Design: backlog-final-closeout

## 0. 裁量基线

维护者裁决:「重编所有项为一份完整变更,这一次要落地干净」。哲学五主张(答案文档 §一)仍是裁判标准;每项按**最小机制**裁量(evals 骨架而非全家桶/prompt 契约而非运行时校验器)。

## 1. R1-R5 实现要点

| # | 落点 | 设计 |
|---|---|---|
| R1 | resources/prompts/meditation.md §1 | 回顾清单加一行:「含 feedback 事件(任务失败 negative/用户不满)——失败教训是最高价值反思素材」;§2 分析提示对 negative 归因痛点 |
| R2 | rl/http_api.go | `GET /diagnostics`:handler 持 DiagnosticsSnapshot 构造器注入(SetDiagnosticsFn(func() any) 模式,同 SetFeedbackStore);装配层注入 memory/engine.NewMemoryDiagnostics(nil, entryStore).Snapshot() |
| R3 | agent/output_overflow.go | dumpOverflowEvent 成功后经 cm 的既有事件写入路径(persistBusEvent)登记 `overflow_dump` 事件(EventSummary 含相对路径+字节大小,Content 为路径+首行);agent 可 recall "溢出" 命中票据→exec 读文件取全文。事件类型:复用 external/system 通用型,Metadata subtype=overflow_dump(不注册新类型——最小机制;recall 关键词命中 EventSummary) |
| R4 | docs/wiki/platform/platform-subsystems.md reliability 节 | 一行注记(会话态/重积累/接受丢失) |
| R5 | examples/wechat-bot/main.go | wechatApprovalChannel 实现 governance.ApprovalChannel(Deliver→bot.SendTextToUser(digest+批准方式));装配处 approvalManager.AddChannel(ch);README 治理审批节补「直投已装配」 |

## 2. evals 骨架

```
evals/
  README.md            # 运行方式与扩展指南
  suites/              # YAML 声明(场景/期望/断言类型)
    ticket-recall.yaml # 票据可召回率
    tool-choice.yaml   # 工具选择
  evals_test.go        # runner:go test ./evals/ 即跑;无 key 跳过真实 LLM 项
  cases/               # Bad Case 资产(tests/README 教训转用例)
```

- **票据可召回率 eval**:构造事件→压缩→收集卡片 key→逐 key recall→断言取回原文(纯工程,mock 可跑,进 CI);
- **工具选择 eval**:场景 prompt+mock model 注入期望 tool_calls→断言路由(组件级);
- **Bad Case**:hex 断裂事件(key 格式)→断言 recall 拒绝静默空结果(资产化 tests/README 教训)。

## 3. D5 重写要点(git-native 语义)

- **M1 契约 ratchet**:plan/knowledge 等子 Agent 的 handoff 格式写入各自 prompt(结构化四段:任务/上下文摘要/交付物/验收);**守护方式=evals 用例断言输出匹配契约 schema**(漂移即红)——无运行时校验器;
- **M2 critic 自检**:meditation.md §4 验证计划节强化——产物登记前自检清单(可运行?/位置对?/下轮可核查?),不通过则回炉不 register;
- **M4 long-poll**:`GET /feedback/wait?timeout=30s`:handler 持 pending 队列(feedback 写入后入队),阻塞至超时或新事件返回最近 N 条待评分摘要(event_key+summary)——AReaL 侧拉取用;TurnTracker:evidence 采集时窗口内 TypeTurnStart 计数入 Evidence(字段 TurnCount 已有,补采集)。

## 4. 发布

tag v0.1.0:CHANGELOG.md 核对返工条目→`git tag -a v0.1.0 -m "..."`→push --tags。

## 5. 风险

| 风险 | 对策 |
|---|---|
| R3 事件写入在溢出路径(outputCh 满)——再溢出? | 走 persistBusEvent(存储路径,不经 outputCh),失败仅日志(尽力而为) |
| long-poll 占用 handler | 超时上限 30s+单飞(队列消费即返) |
| evals 引入 LLM 依赖 | mock 可跑进 CI;真实 LLM 项 opt-in(tests 短跳机制同款) |

## 6. 决策记录

| 决策 | 裁定 |
|---|---|
| 范围=全部遗留重编一份 | 维护者 2026-09-07「落地干净」 |
| evals 先骨架 | 最小机制;深度评估集留扩展点 |
| M1 不做运行时校验器 | prompt 契约+evals 守护足够 |
| R3 不注册新事件类型 | 复用通用型+subtype,最小机制 |
