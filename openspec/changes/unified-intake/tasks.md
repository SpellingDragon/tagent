# 任务（只编排；孙任务见 changes/<域>/tasks.md）

```mermaid
graph LR
  A1A[symptom anchor RED] --> A2[loop ready increment]
  A2 --> A3[assembly inject ReadyFeed]
  A3 --> A4[host Invoke semantics]
  A4 --> A5[spec delta + anchor migration]
  A5 --> B1[meditation anchors]
  B1 --> B2[driver activity gate]
  B2 --> B3[meditation spec delta]
  B3 --> F[F closeout]
```

头部三行：只编排 / 启动门=本域无跨 change 前置（规格现条款在 main）/ 波次仅为汇报分组，解锁=依赖入 main。

- [ ] 01 域 decision-boundary —— changes/01-decision-boundary/（叶 A1–A6）
- [ ] 02 域 meditation-activity —— changes/02-meditation-activity/（叶 B1–B3）
- [ ] F1 契约零改动断言（openspec/specs 逐条 diff=仅本变更 delta 的两处）
- [ ] F2 全量门禁（lint+三轮事件门+双模块）
- [ ] F3 DoD 核验（两症状锚红转绿+回滚即红双证据入 run notes）
- [ ] F4 archive+导览
