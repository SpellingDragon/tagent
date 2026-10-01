## 1. P0 规格对齐（spec 侧，无代码依赖）

- [ ] 1.1 `event-sourced-projection` 主 spec 应用 delta：无锚恢复条款重写为全量复原语义（按本 change delta MODIFIED 块整块替换主 spec 对应 Requirement）
- [ ] 1.2 `event-segment-store` 主 spec 应用 delta：移除"WAL 中间坏行容错"与"LocalFileKV 写路径 fsync 耐久"两条，ADDED"LocalFileKV Sync 为原子快照屏障"条款
- [ ] 1.3 同步修正 `docs/wiki/agent/compression-and-telemetry.md:26` 的"保留最新 N 条"截断表述为全量复原（与 1.1 同源）
- [ ] 1.4 修正 `docs/storage-durability-positioning.md:11-19`："fsync 默认开启"改为"FSync 旋钮存在但无效果；屏障为 KV 快照原子 rename"，"行为仍在"限定为快照屏障行为

## 2. agent 核心运行时修复

- [ ] 2.1 `agent/agent.go:590-592` SetAuditLine 接线移入 `cfg.Meditation.Enabled` 块内（meditationMgr 构造后）；补测试：Meditation 启用时冥想消息含 self-audit 审计行（当前恒缺失）
- [ ] 2.2 `agent/execution_gate_model.go` 三个失败点（verify 拒绝 71-74、迭代器创建 err 77-80、通道创建 err/nil 84-86）改为 yield 携带 `Response.Error` 的失败响应；补测试：三路径均归约 failed turn、持久输入不以 completed 口径 ack（对照 persistent-event-loop ADDED 条款三场景）
- [ ] 2.3 `agent/session.go` 拒绝分支（119-121）补 `unregisterLiveCM(invCM)` 与 `invCM.Close()`（或把 104 行注册移到 Err() 检查后）；补测试：owner 关闭后到达的委托调用被拒绝时 LiveCMCount 归零、Obligations 不残留
- [ ] 2.4 删除 `agent/recovery.go:60` `var _ = sync.Mutex{}` 死残留

## 3. 常驻可靠性修复（谱系信号化 + 批量路由）

- [ ] 3.1 `agent/task/task_manager.go` `SettleSignal` 增加谱系承载字段；`finalizeRetired`（1162-1170）删除 `t.Spec.Origin` 原地改写，改设信号级 `task-retired` 戳
- [ ] 3.2 谱系消费点切换：`newTaskSettledEvent`（agent/event_bus.go:249-253 一带）与回合谱系提取（event_loop.go extractTriggerSource）优先读信号级值；全链 grep `MetaKeyTriggerSource` 核对无遗漏消费点
- [ ] 3.3 补测试：退役不写任务 Origin（并发读写竞争消除，`-race` 下 retire×watch 并发用例通过）；Resume 恢复轮结算按原谱系外投（对照 async-task-lifetime delta 新增两场景）
- [ ] 3.4 `agent/agent.go:493-500` OnBatchRetire 补 per-invocation 路由：有 `taskInvocationID` 绑定的条目走 `deliverTaskSettled`（记账递减+唤醒父循环），无绑定条目才进汇总事件；`newBatchRetiredSummaryEvent` 相应只收无绑定条目
- [ ] 3.5 补测试：父循环 await 的任务被批量退役后 invocation 循环正常静止退出（不依赖硬超时）；汇总事件条数断言同步更新；既有批量退役测试全量回归

## 4. compress 修复（批量折叠豁免 Active）

- [ ] 4.1 `agent/compress/context_compressor.go` foldSettleRuns：折叠前按 dispositions 从 run 尾部剔除连续 TelemActive 成员（原样保留 ref），剔除后前缀 <2 则整段不折
- [ ] 4.2 补钉住测试：批量相邻 Active 通知不被折叠/截断（正文完整进入模型视图）；混合 run（前 Internal 后 Active）仅折叠前缀；run 全 Active 整段不折

## 5. memory 修复（spill 释放时序）

- [ ] 5.1 `memory/mem_spill.go` ReplayWithNotify：循环内仅记录本轮成功 key，`rewrite` 成功后统一 `ReleaseKey`；rewrite 失败不释放任何 key
- [ ] 5.2 `memory/retention_lease.go:121` Release Godoc 修正（按持有者释放、非幂等；归零才解除保护）
- [ ] 5.3 补测试：rewrite 失败后重放不重复释放（同 key 两轮重放后租约计数不穿透）；下轮 AlreadyCommitted 补释放路径可达

## 6. 工程化修复（门禁）

- [ ] 6.1 `scripts/check_comment_only.sh`：删除侧文件（基线有、工作区无）直接传入 codetools 触发 MISSING-HEAD 硬拒；仅纯新增批次 exit 0；循环与传参加引号
- [ ] 6.2 构造性用例验证：删除 .go 文件的批次被硬拒（exit 非零）；新增任意代码文件且无其他变更时行为不变（纯新增放行）；混合批次中删除文件被拒
- [ ] 6.3 `scripts/lint.sh` "lint: ok" 打印移至全部四道门之后

## 7. 文档修复

- [ ] 7.1 `README.md:339` 认知资产防线表述纠正：D1 漂移审计需显式 working_dir，未设时跳过（或恢复 wechat-bot 示例 working_dir 配置，二选一并在 PR 说明）
- [ ] 7.2 `docs/wiki/rl/rl-architecture.md:118-120` "已知缺口"段改为真实缺口陈述
- [ ] 7.3 `openspec/changes/archive/**/evidence.md` 个人绝对路径（file:///Users/...）批量替换为仓库相对路径（8+ 处，重点 2026-09-30-complete-resident-reliability-protocol）

## 8. 回归验证与收口

- [ ] 8.1 全量回归：`go build ./...`、`go vet ./...`、`go test ./agent/... ./memory/... ./tool/...`、`cd test && go test ./...`（E2E 模块）
- [ ] 8.2 门禁回归：`scripts/lint.sh`（comment_policy 棘轮+name-check+doc-refs+gen_godoc+proc-refs）、`scripts/check_test_merge.sh`、openspec validate 本 change；改动点（3.x/4.x）跑 `-race` 构建
- [ ] 8.3 提交序列：按主题分组提交（spec 对齐 / agent 运行时 / 常驻可靠性 / compress / memory / 工程化 / 文档），每提交可独立 revert
- [ ] 8.4 push origin dev，创建 dev→main 的 PR（描述附评审摘要、修复清单与本 change 链接）；CI 绿后合并
- [ ] 8.5 归档本 change（openspec archive，deltas 并入主 specs），作为收口提交进入同一 PR
