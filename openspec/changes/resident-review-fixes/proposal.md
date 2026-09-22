## Why

complete-resident-reliability-protocol 交付后进行了两轮并行深度交叉评审（6 个设计评审员 + 5 个实现核对员，全部发现经独立实证）：设计骨架成立（实现符合率 43/44、四组疑似重复机制确认职责正交），但实证确认 1 项 Critical 与 13 项 Warning。其中 Critical（热重建壳重复恢复登记）会造成不可逆的租约泄漏与灾难性的遗忘屏障永久悬挂，必须在合入主线前修复。

## What Changes

- **修复 C1（🔴）**：热重建执行壳（buildModeExecutorShell）不再构建 durable bus、不再接线 mem_spill——两处加 `!mode.isExecutorShell()` 门控，恢复登记（ArmRetentionFromInbox/ProtectAllPending/BeginHold）回归常驻 owner 独占，满足 runtime-resource-ownership「执行壳复用已注册状态，不重复恢复」既有条款；spill 腿吞错同步上抛为构建失败（消除"热更成功+悬空屏障"组合）。
- **修复键地板墓碑缝隙（🟡P1）**：`scanLiveKeys` 的 maxKey 更新移到墓碑 continue 之前——地板语义是"新代不重发已发键"，与存活无关；补"墓碑化最高键+同秒重启"回归测试。
- **修复 quarantine 租约悬挂（🟡P2）**：`QuarantineEnvelope` 路径对信封材料补租约释放（nil-safe），恢复"Ack/隔离皆终态、终态皆释放"的对称性。
- **修复 race 分类器缝隙（🟡P3）**：`triRaceOnlyFramework` 的 FAIL 块扫描不再被空行/panic 行提前复位（真实断言失败绝不后藏）；族签名豁免补版本绑定（从 build info 读 trpc-agent-go 版本，≠登记版本时族豁免失效），使 spec 的签名/版本/测试三元组全部机械化。
- **修复 FSync 假轴指纹（🟡）**：`fingerprintMemory` 移除 FSync（零行为差异轴不得制造共享假冲突），同步修正锁定该错误行为的既有测试。
- **补 subagent TTL 通道（🟡）**：AgentToolWrapper 工具 schema 增加 ttl 入参并透传 TaskSpec（含 Declarative.Params 持久化与 SpecFromDeclarative 回放）——长时 subagent 不再只能撞 10min 地板强杀，与 command 类对称。
- **死机制裁决执行（🟡）**：删除 ReadyCh（零消费者信令）；poisoned entry 主触发腿在现引擎下不可达——保留机制、注释降级为"面向未来引擎的前置契约"；TTL 回收懒触发边界在 spec 风险表明示（不引入 ticker，静默期回收时机=下一次唤醒）。
- **验证诚实性收口（🟡）**：30 轮中途审计改为真恒等式（累计单调+信封内容对轮次日程核对，删除 vacuous 伪核对）；`empty_input` 死枚举裁决（删除——空输入槽实际落事实标 processed，规格留痕与实现语义分叉以实现为准收敛）。
- **注释与护栏清扫（🟢 汇总）**：event_loop.go 恢复提示消费点矛盾注释、两处陈旧 fsync 注释、三处死代码、`Attempts` 字段裁决注记（持久审计字段、diagnostics 消费者推迟）；静态淘汰清单补旧确认 API/迁移门/弱回退符号；docs/upgrade-rollback-drill.md 已删 API 引用修正；spec L91 与裁决 B 的文本对齐（容量提示=建议性 delta、淘汰真源=绝对计数）。

**非目标**：WAL 恢复或 rustviking 提前接线（独立大决策，另行立项——触发条件已建议挂入主变更 §9.7 待验清单）；臂展不扩大到评审 🟢 项之外的任何重构。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `runtime-resource-ownership`: "运行时资源租约与关闭"条款补指纹裁决句（零行为差异轴 MUST NOT 参与共享冲突指纹）+ 假冲突 Scenario；其余修复均为实现符合化（壳登记豁免、键地板、租约释放、race 版本机械化——条款文本已覆盖，不改 spec）。

**主变更 delta 勘误（不走本变更 delta，以任务承载）**：complete-resident-reliability-protocol 尚未 archive、其 delta 仍是活真源——`empty_input` 死枚举删除（persistent-event-loop delta 的逐槽闭合集）与 subagent ttl 通道（async-task-lifetime delta 的 TTL 贯通条款+风险表）直接勘误于该 delta 文件并记录于本变更 evidence；两变更 archive 次序须为主变更在先。
