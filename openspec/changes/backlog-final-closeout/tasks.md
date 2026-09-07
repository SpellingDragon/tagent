## 1. R1-R5 工程缺口

- [ ] 1.1 R1:meditation.md §1 回顾清单加负反馈一行+§2 归因提示(prompt 级,闭环:负反馈→冥想→改进)
- [ ] 1.2 R2:rl/http_api 增 `GET /diagnostics`(SetDiagnosticsFn 注入模式)+example 装配;回归:请求返回快照 JSON 含 wal_quarantined 键
- [ ] 1.3 R3:溢出落盘后经 persistBusEvent 登记 overflow_dump 事件(Summary 含路径+大小;**票据与事件 Content 均含取回指引「可 exec cat <path>」**——C4:agent 见票据须知道怎么取);回归:溢出后 recall 可命中票据
- [ ] 1.4 R4:platform wiki reliability 节注记 hintTracker 会话态(一行)
- [ ] 1.5 R5:**新增 TagentAgent.AddApprovalChannel passthrough**(C2:AddChannel 现仅在 New() 内部,example 无装配面);example 装配 wechatApprovalChannel(实现 Deliver→SendTextToUser)+README 补直投说明;回归:Deliver 调用消息送达(mock bot)

## 2. evals 骨架(D4 精简)

- [ ] 2.1 evals/ 目录+README+evals_test.go runner(go test ./evals/ 即跑)
- [ ] 2.2 票据可召回率 suite:构造→压缩→逐 key recall 断言原文(mock 进 CI);回归:全量取回
- [ ] 2.3 工具选择 suite+Bad Case 资产(hex 断裂/静默空结果两用例转 cases/);**C5:Bad Case 为 fail-first 探针——首跑红=揭示 recall 畸形 key 现状缺陷,修后转绿即资产化**;回归:Bad Case 断言显式拒绝
- [ ] 2.4 suites YAML 声明化(suites/*.yaml 场景/期望/断言类型;runner 解析执行)

## 3. D5 git-native 重写

- [ ] 3.1 M1:plan/knowledge 子 Agent prompt 增结构化 handoff 四段(任务/摘要/交付物/验收)+evals 契约守护用例(输出匹配 schema)
- [ ] 3.2 M2:meditation.md §4 强化 critic 自检清单(登记前:可运行?/位置?/可核查?——不过则回炉不 register)
- [ ] 3.3 M4:HTTPAPI `GET /feedback/wait` long-poll(队列+30s 上限,**内存态重启清空=接受丢失** C7)+TurnTracker(**基于现有用户输入型事件计数——无 TypeTurnStart,以 registry 实名为准** C1;顺手把 TurnCount 口径从全事件数修正为真实 turn 数,neg_fb 分母联动质变);回归:写入 feedback 后 wait 立即返回;TurnCount 非零且=用户输入数

## 4. 发布工程

- [ ] 4.1 CHANGELOG 核对(git-native 返工/五特性/评审修复条目)→ `git tag -a v0.1.0` → push --tags
- [ ] 4.2 README 开发节补 evals 命令(`go test ./evals/`)

## 5. 收尾

- [ ] 5.1 全仓 grep 复核(退役词仅存退役声明);门禁:build/vet+全量 short+关键包 race 全绿
- [ ] 5.2 LEDGER 记账(backlog 清空,首 tag);文档零漂移抽查(新端点/evals 在 README/wiki 可见)
