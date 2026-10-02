## Context

complete-resident-reliability-protocol（7677c07+9c785ba+未提交 §9 产物）已完成 76/76 任务并经两轮深度交叉评审（A1–A6 设计三查、B1–B5 实现核对，全部发现独立实证）。评审结论：设计骨架成立、实现符合率 43/44；确认 1 项 Critical（热重建壳重复恢复登记）与 13 项 Warning，分布在协议正确性边界、租约对称性、验证诚实性与死机制四个面。本变更收敛修复清单，不扩大臂展。

## Goals / Non-Goals

**Goals:**
- 消除 Critical：热重建壳对共享持久态的重复恢复登记（泄漏腿+阻断腿+spill 面）。
- 修复三个实证的窄缝隙：键地板墓碑漏 seed、quarantine 租约悬挂、race 分类器 FAIL 块穿透。
- 执行三项已论证裁决：FSync 假轴出指纹、ReadyCh 删除、poisoned 主腿降级注记。
- 补一个能力缺口：subagent 寿命的自设通道（ttl 入参）。
- 验证与文档诚实性收口：30 轮中途审计真恒等式、empty_input 死枚举删除、注释矛盾/死代码/护栏清单/文档漂移清扫。

**Non-Goals:**
- WAL 恢复或 rustviking 提前接线（独立大决策；触发条件已建议挂主变更 §9.7 待验清单）。
- ReadyCh 接线（healthz 消费）——裁决为删除而非接线，接线需求出现时另立。
- TTL 后台 ticker——裁决为明示懒触发边界而非加定时器（常驻 bot 静默期无输入即无回收需求方，ticker 只回收无人观察的任务）。
- 评审 🟢 项之外的重构（含 B1 报告的 reconcile-Blocked 窄退化路径——不丢不双写，登记不修）。

## Decisions

### 决策 1：壳不建 durable bus、不接 spill（C1 修复形态）

`build_agent.go` 两处加 `!mode.isExecutorShell()` 门：`agentCfg.BusSpillDir` 赋值与 `ets.SetMemSpill(...)` 接线。壳 bus 降级为 volatile（构建验证所需的最小面），恢复登记自然回归常驻 owner 独占——这是 runtime-resource-ownership spec L39 既有条款的实现符合化，非新契约。spill 腿 `error_tracking.go` 的 `Warnf` 吞错改为返回错误（构建失败 fail-closed，旧 runner 继续服务）。`buildMode` 三谓词坍缩问题随本修复自然消解过半（bus 轴出现真实分叉）；三谓词暂不合并，待第三模式真出现。

依据：B3 实证——壳 ta 构建后丢弃（`_ = newTA`）且 Close 无效（借用壳零关闭权）；releaseRetention 三处生产调用全在常驻 bus 的 Ack 路径；Arm 失败腿 BeginHold 无 EndHold 且 `releaseStoreBarriers` 不回收 bus 直连 hold。壳跳过 EventReplayer 校验无损验证价值（memory.* 变更已被 reloader 先序拒绝、store 不随热更换代）。

### 决策 2：键地板 seed 语义 = 最高已发键（含墓碑）

`scanLiveKeys` 内 maxKey 更新移到墓碑 `continue` 之前（1 行）。地板不变量是"恢复代不重发磁盘上已见过的键"，墓碑化的键仍是已发键——撞上它 ReplayEvent 返 `ErrEventForgotten`（确定性）会把合法输入降级为 quarantine。补回归测试：最高键事件墓碑化→同秒重启（地板 seed 路径）→新代发号越过墓碑键。

### 决策 3：quarantine 终态补租约释放

`EventBus.QuarantineEnvelope` 包装层（叶子无 store 句柄）在移入隔离成功后对 `MaterialOf(env)` 调 `releaseRetention`（nil-safe，未 arm 者跳过）。语义：Ack 与隔离都是信封终态，终态皆释放——恢复"Ack 的目录同步成功才释放"契约在隔离路径的对称补全（隔离时目录屏障已由 rename 原子性保证）。补 retention e2e 断言：quarantine 后该 key 租约归零、可再被 TTL 淘汰。

### 决策 4：race 分类器双修

(a) FAIL 块扫描：非缩进行不再提前复位 `inFail`（仅新的顶层 `--- FAIL`/`PASS`/`ok` 重置块边界），并对列 0 的 `panic:` 行显式拒绝豁免——真实断言失败/panic 绝不藏在族 race 后。(b) 版本绑定：族签名常量旁锚定登记版本（`debug.ReadBuildInfo` 读 trpc-agent-go 模块版本），≠登记版本时族豁免整体失效并要求重新登记——spec 三元组（签名/版本/测试）全部机械化。自测扩展：空行穿透反例、panic 逃逸反例、版本不匹配反例。

### 决策 5：三项死机制/假轴裁决

- **FSync 出指纹**：`fingerprintMemory` 删除 fsync 拼接；`TestOwnership_ConflictingConfigRejected` 的 fsync 腿改为断言"仅 fsync 不同的配置共享同一实例"（假冲突的负向锁）。config 键保留（兼容装载，注释已诚实）。
- **ReadyCh 删除**：`residentReady` 字段、两条关闭分支、`SetReadyCh/ReadyCh` 访问器全删；`testing.go` 若有消费同步清理。
- **poisoned 降级注记**：机制保留（spec 条款+真实覆盖），注释补"当前引擎 InMemoryEngine.Close 恒 nil，主触发腿不可达；契约面向未来引擎"。不建解封出口（触发时重启恢复可接受）。

### 决策 6：subagent ttl 入参通道

AgentToolWrapper 工具 InputSchema 增加 `ttl`（int 秒，可选，语义与 ActionArgs.TTL 同构：>0 生效、非法拒绝）；透传链：wrapper 参数→TaskSpec.TTL→Declarative.Params 持久化→`SubagentSpecFromDeclarative` 回放。resolveTTL 三级链（显式→配置默认→10min 地板）天然复用。spec 风险表同步：subagent 长任务逃生门补齐。

### 决策 7：验证与文档收口细节

- 30 轮中途审计：`inputs` 与累计值比较（真单调）+ 信封 Raw 内容含本轮日程标记（`r30-%02d-`）核对；post-ack 轮从台账撤下"身份核对"宣称（该轮信封已清空，核对天然 vacuous）。
- `empty_input` 枚举删除：闭合集改两值；空输入槽实际语义（提交门先行为其落事实、标 processed）在 spec 措辞中如实化。
- 注释矛盾单点修复（event_loop.go 恢复提示消费点）+ 两处陈旧 fsync 注释 + 三处死代码 + upgrade-rollback-drill.md 已删 API 引用 + spec L91 对齐裁决 B。
- 静态淘汰清单补入：`ConfirmDurableByRequestID`/`ReconcileDurableReceipts`/`PathForReceiptKey`/`ReceiptNote`/`ErrLegacySpillNotDrained`/`checkUpgradeGates`。
- `Attempts` 字段：保留（重试审计语义有诊断价值），注释明示"持久审计字段，行为消费者（max-attempts 门）未接线，diagnostics 聚合面推迟"。

## Risks / Trade-offs

- **壳降级 volatile 后热更验证面变窄**：壳不再验证 durable 路径构建。缓解：壳的验证价值本就在 runner/executor 接线（durable 面随 store 不换代恒真）；热更+未决输入的组合验收由既有 r30/e2e 承担。
- **FSync 出指纹的兼容面**：现网若有依赖"改 fsync 触发重建"的部署会被静默共享。判定：该依赖建立在假轴上（两配置行为恒同），静默共享即正确行为。
- **版本绑定失效的噪音**：框架升级后族豁免立即失效、tri/drill 会红。这是设计意图（升级后 race 面需重新登记），但需要在升级变更的任务清单里预置"重新登记 race 族"条目提醒。
- **subagent ttl 的模型滥用面**：模型可设超长 ttl。缓解：与 command ttl 同一威胁面（既有裁决已接受），看板呈现剩余寿命保持可观察。
