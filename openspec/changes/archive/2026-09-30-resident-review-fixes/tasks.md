## 1. Critical：热重建壳恢复登记豁免（C1）

- [x] 1.1 build_agent.go 两处门控：`agentCfg.BusSpillDir` 赋值与 `ets.SetMemSpill(...)` 加 `!mode.isExecutorShell()`——壳不建 durable bus、不接 spill；error_tracking.go spill 腿 Warnf 吞错改为上抛（构建失败 fail-closed）。
- [x] 1.2 fail-before：构造壳构建场景注入恢复登记（还原旧行为）→ 泄漏断言红（壳构建后共享 lease refs 增量非零）；恢复后归零。集成测：热更+未决 envelope 场景下 refs 恒等于常驻登记、Arm 失败注入时常驻 store 遗忘屏障不被拉起。
- [x] 1.3 核对 hotreload/org_hotreload 既有测无壳 durable 断言被破坏；agent/root race + evidence + 勾选。

## 2. 协议边界缝隙三修

- [x] 2.1 键地板墓碑缝隙：scanLiveKeys 的 maxKey 更新移到墓碑 continue 前；补"最高键事件墓碑化→同秒重启→新代发号越过墓碑键"回归测；fail-before（还原顺序→测红）。
- [x] 2.2 quarantine 租约释放：EventBus.QuarantineEnvelope 包装层移入成功后 releaseRetention（nil-safe）；retention e2e 补"隔离后租约归零、可再被 TTL 淘汰"断言；fail-before（去掉释放→断言红）。
- [x] 2.3 race 分类器双修：FAIL 块扫描不受空行/panic 行复位穿透（自测补两反例）；族签名豁免加版本绑定（build info 读 trpc-agent-go 版本，≠v1.10.0 时族豁免失效），自测补版本不匹配反例；fail-before。

## 3. 死机制与假轴裁决执行

- [x] 3.1 FSync 出指纹：fingerprintMemory 删除 fsync 拼接；TestOwnership_ConflictingConfigRejected 的 fsync 腿改写为"仅 fsync 不同→共享同一实例"负向锁；evidence 记录裁决。
- [x] 3.2 ReadyCh 删除：residentReady/SetReadyCh/ReadyCh/两条关闭分支全删（含 testing.go 消费）；build 编译级验证无残留引用。
- [x] 3.3 poisoned 降级注记：closeResource poisoned 分支注释补"现引擎 InMemoryEngine.Close 恒 nil，主触发腿不可达；契约面向未来引擎"；不建解封出口（evidence 记录裁决）。
- [x] 3.4 TTL 懒触发边界明示：主变更 delta（async-task-lifetime）风险表补"回收时机=下一次唤醒（看板/冥想/冷启动），完全静默期滞后"——不引入 ticker。

## 4. subagent ttl 通道

- [x] 4.1 AgentToolWrapper InputSchema 加 ttl（可选 int 秒，>0 生效、非法拒绝），透传 TaskSpec.TTL→Declarative.Params→SubagentSpecFromDeclarative 回放；主变更 delta 的"异步命令 TTL 贯通与到期强杀"条款补 subagent 句与 Scenario。
- [x] 4.2 测：显式 ttl 生效/默认链回退/负值拒绝/跨重启回放四腿 + 看板呈现一致；fail-before + race + evidence + 勾选。

## 5. 验证诚实性与文档收口

- [x] 5.1 30 轮中途审计：inputs 与累计值真单调比较 + 信封 Raw 含当轮日程标记核对；post-ack 轮撤下身份核对宣称；fail-before（还原弱断言→红）。
- [x] 5.2 empty_input 死枚举删除：completion.go 闭合集改两值；主变更 delta（persistent-event-loop）逐槽闭合集措辞同步（空输入槽=落事实标 processed）；相关测更新。
- [x] 5.3 注释/死代码/护栏清扫：event_loop.go 恢复提示消费点矛盾注释、segment_store_barrier_test 与 resident_e2e_test 陈旧 fsync 注释、crash_input_commit/reset_drill/crash_finish 三处死代码、Attempts 字段裁决注记、静态淘汰清单补 6 符号（ConfirmDurableByRequestID/ReconcileDurableReceipts/PathForReceiptKey/ReceiptNote/ErrLegacySpillNotDrained/checkUpgradeGates）、docs/upgrade-rollback-drill.md 已删 API 引用修正、主变更 delta spec L91 容量提示措辞对齐裁决 B。
- [x] 5.4 全模块回归门：root/agent/memory/reliability/tests build+vet+short+定向 race（沿用 §8.8 门径与 race 豁免登记口径）；evidence 收口。

## 6. 交付

- [x] 6.1 evidence.md 汇总（逐任务 fail-before/验证记录+评审发现编号回链 A1-A6/B1-B5）；不提交/不推送/不 archive，交付清单与待验项沿用主变更 §9.7 纪律。
