# 静态决策与待裁问题

## Context

当前本地 HEAD 落后 PR 远端 8 个提交，工作树有既存未提交修改。编排者必须区分本地代码、远端 PR 与 origin/main，不得把某一时点的审计结论冒充当前事实。已有 concept budget 只约束接口形状，不判断概念必要性。

## Goals / Non-Goals

**Goals**

- 每个核心事实只有一个权威写入者，每个地址只有一个模型驱动者，每次关闭只有一个状态机。
- 公共 API 表达领域事实，不表达某次修复的脚手架。
- CI 快速反馈与完整证明都有明确门禁位置，失败证据不丢失。
- 每轮修复经过独立复审，直至没有新的关键问题。

**Non-Goals**

- 不以改名、搬包、增加注释或压测矩阵扩张冒充概念收敛。
- 不在 W0 修改生产代码，不保留所有实现耦合测试，不直接 squash merge 当前 PR。
- 不把产品压缩语义修改混入 CI 提速。
- 不在缺少公共 seam 确认时新增测试。

## Decisions

### D1 证据层级

事实优先级为：当前公共入口可复现行为 > 当前代码与主 spec 的直接矛盾 > 已归档设计 > 前次审计叙述。远端与本地不一致时必须分别举证。

### D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | address-consumption | consumer-loop-convergence | 地址、实例、Consumer、segment/turn 与答案归属 | 无；首条核心探针 |
| 02 | message-semantics | cross-session-delivery, unified-task-semantics, task-reentry | send 回执、通知结清、委派与重入 | 01 的唯一驱动裁定 |
| 03 | close-lifecycle | async-task-execution, attention-* | Unsettled、审批等待、writer 与关闭结果 | 01、02 的状态权威 |
| 04 | domain-contracts | architecture-guardrails, persistent-event-loop | 词汇、公共 API、失效 spec 与概念预算 | 01–03 裁定 |
| 05 | ci-feedback | evals-harness, race-gate-coverage | 工作流、runner、证据 bundle 与长测分层 | 无产品实现依赖 |
| 06 | compression-cost | task-skeleton-compression, compression-pipeline | 容量触发、折叠材料与长程成本 | 独立于 CI 配置；需性能实测 |

### D3 启动门与波次表

`runtime-simplicity-upgrade`、`event-driven-consumption`、`post-training-data-loop` 与本变更共享运行时热点文件或主规格；在本变更激活期间，它们只允许只读证据输入，不得并行派发实现或规格写入。`success-semantics` 的现有实现不得被当作未发生而重写。若需恢复其中任一独立实施，必须先由编排者明确移交热点文件与规格所有权。


| 波次 | 内容 | 解锁条件 |
|---|---|---|
| W0 | 六域只读拷问与决策树 | 六份报告到齐，编排者逐条裁定 |
| W1 | 最小可证伪路径：公开 send/Host 路径只能由地址 Consumer 驱动 | 公共 seam 确认；01 规格与三锚冻结 |
| W2 | 消息/关闭垂直切片；CI 独立快切 | 前序权威入主干；各单触碰集不相交 |
| W3 | 公共概念删除、失效 spec 清理、压缩独立裁决 | 行为锚绿且概念账本净减 |
| F | 独立终审、全量证据、归档 | 新审者显式 frontier 空 |

波次仅用于汇报；实际解锁以依赖与触碰集为准。

### D4 合并与写入规则

- W0 所有代理只读；共享 git 索引、根账目、主 specs 和现有工作树修改归编排者。
- 实施派发原子是孙任务，不是整个域；写入白名单精确到文件，触碰集相交即串行或合单。
- 禁止代理 git commit/checkout、禁跑全仓长测、禁修改现有未提交工作；过程复现只放仓库外。
- 公共概念只允许“保留并证明领域必要性 / 内化 / 删除”三种结局，不允许加兼容别名凑绿。
- CI 与产品修复分线；压缩语义与 CI 调度分线。

### D5 三级验收

1. 孙任务：冻结的正锚、负锚、生产消费者锚均非空转且 exit 0。
2. 子域：每个 spec scenario 映射到公共行为证据；独立偏差审计无 Major 遗留。
3. 变更级：用户症状锚回滚即红/修复即绿；概念账本净减；完整 CI 证明在进入 main 前通过；新审者 frontier 空。

### D6 最小核心模型白名单

只允许事实链、地址、消费循环、任务、通知、审批、回执、关闭作为一级领域词。Segment/Turn、Writer/Movement、Continuation、Ledger/Projection 必须证明是既有词的独立事实面，否则内化或删除。CI runner/evidence 属于验证域，不进入运行时尚域模型。

## Risks / Trade-offs

- [大规模重建再次失控] → 先完成一条公开症状红绿探针，再扩任务；同一机制三次修复仍出关键缺陷则停线。
- [merge queue 改变每 PR 全量的旧政策] → 显式修改规格与词汇；不得用 nightly 后置侦测冒充 merge 前证明。
- [删除实现耦合测试导致症状失去保护] → 只迁移/保留公共行为锚，删除前建立等价或更强的生产消费者证据。
- [既有工作树被覆盖] → W0 只读；实施前记录文件所有权和基线差异。

## 裁定状态

W0 与规划复审 Q1–Q6 已裁定：send ok=目标输入事实持久提交；通知以持久消费结果和连续位置结清，failed/cancelled 也可以是已处理结果；CloseCut 保留业务状态且不得隐式取消审批；segment 是私有执行态、Turn 是不可变结果；完整六组合目标为 merge_group 必过，但外部启用前保持 PR 全量，5.6 始终 blocked，不新造缩水 long canary。

唯一实施基线为远端 PR `dfdfa72e70d2cdd91ce8b1db408d5543481dc499`，实施树为 `.quiesce`；主工作区 HEAD `06c0853613f5632d97b8654f99a2942b39d9528c`、origin/main `e7480c658a4f0e5863b6884f0efd34386c3fe4be` 与主工作区 dirty patch 仅作对照，所有脏 hunk 降格为逐项候选。只放行 pilot 1.1；其公共红→绿和独立复审完成前，其余 32 叶均不 formal dispatch。
