# Design（一级：静态决策）

## D1 现状定谳（规划期五轴走查实证——执行者不得重新发明）

| 实证 | 出处 |
|---|---|
| push→pull 归一已在 main：`ApplyOrgHotParams/ApplyHotParams/hotOverlayConfig/UpdateKeepRecent` 现码零匹配；`SetTTLSource` 注释明言 "pull source"（task_manager.go:605）；`NewTagentAgent` 恒装 `staticHotSource(initialHotParams(cfg))`（agent.go:569）——无无源状态 | 代码亲验 |
| 提交点 `applyHotAll` 只做换源+记账，对热参零写入（tagent.go:456-483） | 同上 |
| fp 白名单子集 = `{Entry, Model, Provider, PromptDir, Providers{name,APIEndpoint}, Agents 全规范子集}`（agent/org/fingerprint.go:41-81）⇒ prompt/tools/模型 ID/provider/端点已热（代际粒度） | 同上 |
| 源拉取面 = `OrgHotParams{ThresholdPct, MaxTokens, KeepRecentTasks, TaskTerminalTTL, TaskDefaultTTL}`（context_manager.go:189）⇒ 上下文预算/keep/双 TTL 已热（消费即生效） | 同上 |
| 消费点先例：`OrgBudgetLine()/OrgKeepRecentValue()` 契约断言、compressor 压缩时读、effTTL spawn/sweep 时读 | 同上 |

**由此**：本计划不是重构计划。缺口语义 = 「往既有骨架补一个字段」×1（摘要）+「一个新调用作用域覆盖层」×1（per-call）。

## D2 子域表（接口登记表）

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | hotupdate-matrix-audit | config-hot-reload（矩阵需求） | 维度×通道×消费点×证据矩阵 + 契约红线 + 摘要 knob 归属实证 | 无（纯读+测试） |
| 02 | summary-knobs-hotparams | config-hot-reload（摘要热参需求） | OrgHotParams 扩摘要 knob、SmartCompressor 契约扩、消费点契约测 | **01 的摘要归属结论**（接口常数：字段名与解析点） |
| 03 | percall-subagent-overrides | per-call-subagent-overrides（新） | 调用作用域覆盖栈（三层解析）、最大工具域、Declarative 冻结、防泄漏 | 无（正交于 01/02；接口常数：覆盖参数键名，与 02 无交集） |

## D3 波次表

| 波 | 域 | 说明 |
|---|---|---|
| W0 | 01 | 事实先行；02 的实现启动门 = 01 归属结论入 main |
| W1 | 02 + 03 | 互不依赖可并行；**波次仅为汇报分组，解锁=依赖入 main**，03 无需等 W0 |

## D4 合并规则

- 二级按 01 → (02 ∥ 03) 顺序各自走 dev 提交+CI 四 job 绿后并入 main；同波两域不得共笔提交。
- **三禁区（F1 断言对象）**：① fp 白名单子集字段集（agent/org/fingerprint.go 的 orgSubset）；② 恒装源（NewTagentAgent 的 staticHotSource 装配）；③ 提交点零写入（applyHotAll 不得重引入任何 push 写入）。02 扩 OrgHotParams 不属禁区（加字段非改结构）；03 的覆盖栈在 invocation 作用域，不得触碰 owner 源。
- spec delta 归属：矩阵+摘要归 config-hot-reload（01/02 各自 delta，02 archive 时注意 01 已并入的矩阵需求勿重复）；per-call 为新 capability。

## D5 三级验收

- 孙任务：`—— 验证：<命令>` exit 0 为唯一判定（不断言时长）。
- 子域：spec scenarios 全绿 + 本域全包测试净。
- 变更级 DoD：F1-F4 全勾（见 tasks.md）。

## D6 否决区（用户已裁，执行期不得复活）

| 否决项 | 理由 |
|---|---|
| 归一重构（push→pull 反转） | 已在 main，重做=负工程 |
| 生成参数挪源拉取面（per-request） | 用户裁决保 fp 面粒度；无频繁调优场景时不值一条消费点接线 |
| SwappableModel 统一入源 | rl 训练域语义与 org 配置路由不同，强统一=造第四种耦合 |
| 任何新增 push/订阅/广播通道 | 违背归一机制；补维度只允许"源加字段+消费点读" |
| 第四套参数面（per-call 独立于解析链） | per-call 必须实现为三层解析的第三层，不得另起炉灶 |
