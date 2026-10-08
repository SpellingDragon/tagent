# O1 叶任务（均未执行）

- [x] 1.1 添加 store失败、nil store、关系失败的提交可见性红测并记录失败原因 —— 验证：`go test ./plugin -run '^TestMemoryPlugin_CommitVisibility$' -count=1`；红测阶段允许预期非零，修复后必须0。
- [x] 1.2 收口 stored 闸：票据、投影、游标只在内容提交成功后发布 —— 验证：`go test ./plugin -run '^TestMemoryPlugin_CommitVisibility$' -count=1`。
- [x] 1.3 实现同因果键线性化与活跃锁回收，证明不同session不互相串行 —— 验证：`go test -race ./plugin -run '^TestMemoryPlugin_CausalOrdering$' -count=1`。
- [x] 1.4 覆盖流式跳过、凭据降级、失败恢复和游标淘汰，保留无可信父的断点语义 —— 验证：`go test ./plugin -run '^TestMemoryPlugin_RecoveryBoundary$' -count=1`。
- [x] 1.5 为统一 recall/memory_turn 路径补 partial reason，验证缺祖先/环/上限/首键错误 —— 验证：`go test ./tool/recall -run '^TestRecallTurn_PartialReason$' -count=1`。
- [x] 1.6 与 O4.5 集成关系失败及真实框架管线：无未提交票据入模型；共享文件由编排者写 —— 前置：O4.5；验证：`go test ./tests -short -run '^TestCommittedFacts_ProjectionAndRecall$' -count=1`。
- [x] 1.7 执行本域及消费方全包回归和race，日志保留非零历史，不把复跑成功当不存在失败 —— 验证：`go test -short -race ./plugin ./event ./memory ./tool/recall ./tests -count=1`。
- [x] 1.8 交编排者同步插件/记忆/召回wiki与API，记录契约变化、基线和回归证据 —— 验证：`bash scripts/lint.sh && bash scripts/check-openspec.sh`。

> 编排者核销（W2）：红→绿日志 /tmp/tagent-w2/o1/*.log；1.4 为回归钉测无红可证（代理如实申报，采证方式在案）。复验 /tmp/tagent-w2v/t1.log（plugin/tool-recall ok）、t2（tests -short 5.252s，因果链/不变量零回归）、t3（agent 41.6s ok）。1.6–1.8 归 W3。

> 编排者核销（W3/W4）：1.6=tests/committed_facts_test.go（TestCommittedFacts_ProjectionAndRecall 8 子用例，真实管线红→绿 C3 战报②）；1.7=tests -short 全绿（/tmp/tagent-w4 复跑并入 F10）+C3 记录 plugin/event 定向 race 绿；1.8=D 战报③行1/2/3（commit-gate/键表/partial reason/turn 字段+tmux 撤回）。
