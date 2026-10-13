# 07-evolution — 反思与自演进（design）

> 本设计只写「怎么做」，不改一级 design、主 spec、prototype/wiki 与既有源码。产品实现为**未来授权**，本文件仅确定技术取向与候选文件。

## Context

- 现场：`evolution/`、`plugin/`、冥想链路的旧实现，以及 `model/`、`state/`、`tagent/`（连同 `tests/`）**均已随 2026-10-13 用户授权清理批次删除**（见 `baseline.md` §2.1.1）；当前新基线 HEAD `a9381ce`，无可运行基线。
- 历史参照 `50acdfe` 与 `docs/wiki/evolution/evolution-architecture.md`、`docs/wiki/agent/agent-architecture.md` 可读，但**不作恢复清单**（wiki 已声明为已移除实现的历史记录）。
- 新内核差异（决定本域写法）：① 事件管线插件绑定归 **state**（随代借用），反思/观测是**插件阶段**消费者而非独立总线；② 文件（prompt/skill）**直接热读**，git 只做版本层与后验建议，两者作用范围不得混淆；③ state 持生效定义与运行现场，反思只读事实链、不写他人分区。

## Goals / Non-Goals

**Goals**

- 重建冥想触发链：授权观察面 + 双闸门（novelty / 节奏）+ 水位新鲜度 fail-closed + 确定性自状态 digest。
- 重建 git 原生 refine：显式 pathspec 的 register、改进台账 status、标记校验的 rollback；**不卷用户 index**。
- 重建后验评估：**激活时刻（激活证据）**为窗口锚、按真实消费版本归因（GitSHA 仅关联）、feedback 版本章 join、四态结论（样本不足 ≠ 健康）。
- 重建结构评估门：Eval/Benchmark/ApplyGate 消费同一声明与同代绑定；缺证据不报通过，错误因果保留。

**Non-Goals**

- 不做 state/agent/task/memory/config 的机制实现（属 01/02/03/04/05 域）。
- 不新造 bundle 版本库/发布道；不自动 git 回滚；不新建持久化账本；不恢复退役训练桥。
- 不改主 spec、不动 prototype/wiki、不改旧 change、不提交归档。

## Decisions

**D07-1 反思是产物/观测插件阶段消费者，不是第二条事实总线。**
- 反思在 state 借出的同代插件管线内按阶段接入（请求丰富/提交后观测），只读 `memory.QueryEvents`/`GetEvent` 水合事实；MUST NOT 为形式复用构造并行事件总线。
- 被否方案：为反思另建事实总线或直编回调链——与"插件绑定归 state、按代借用"冲突（历史 RV 勘误已裁）。

**D07-2 观察面 = 授权分区解析 + 水位新鲜度 fail-closed。**
- 观察面 `observed_namespaces ⊆ read_namespaces ∪ {自身分区}`，缺省 `[自身分区]`，越界 **装配期具名拒绝**（不静默剔除）。
- novelty 判据只认观察面内 `Timestamp > lastMeditation` 且**谱系非自管**（event 包 `SelfManagedLineage` 单源）的事件；查询失败/未接读缝 → 门保持关闭、具名 debug，**不回落备用判据**。
- **水位新鲜度**：让位（deferred）水位不动、pending 清零；被让位反思覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）；`lastMeditation` 为零（冷启动）直通一次后自锁。
- 被否方案：以时间阈值预判忙闲、读失败时回退到"最近会话猜测"——前者与"结果记账不等待慢模型"冲突，后者引入不可解释的隐式输入。

**D07-3 refine register 以显式 pathspec 为界，MUST NOT 卷用户 index。**
- `register(paths=[...], note)` 仅对受控路径（`evolution.protected_paths`）内、**显式列出**的文件执行 `git add <paths>` + 结构化 commit（`[self-improve]`）；MUST NOT 使用 `git add -A`/`git add .`/`git commit -a` 等会纳入工作区其他改动或**预暂存内容**的形式。
- 越界路径**拒绝登记**（不执行任何 git 操作）并返回受控清单+越界文件；非 git 仓/ git 失败返回具名错误，但**文件改动已按 mtime 热读生效**（如实呈现，不阻塞）。
- 成功后发 improvement 事件（sha/paths/note）；sha **仅作版本关联**，评估窗锚为改进版本**首次被真实请求消费**的激活证据时刻（**非 commit 时刻**）。
- 被否方案：`git add -A` 或复用用户已 staged 内容——会卷走用户工作区改动、破坏用户 index 边界（历史教训）。

**D07-4 refine rollback 只回滚带标记 commit，执行权在 agent/人。**
- `rollback` 校验目标 commit 带 `[self-improve]` 标记后 `git revert --no-edit <sha>`；非改进标记 commit 拒绝（保护用户提交）；revert 冲突返回冲突详情由 agent 处置。
- 框架 MUST NOT 自动执行 revert：评估只产 `evaluation` 建议事件，经既有消费面（冥想 digest/召回）渗透，不打断消息路由。
- 被否方案：guardrail/judge 判定劣化即自动回滚——与"框架永不动手、执行权永远在 agent/人"冲突。

**D07-5 评估四态，样本不足不得判健康。**
- **窗口锚 = 激活证据时刻**（改进版本被真实请求首次消费）；improvement 事件的 GitSHA **仅作版本关联**，不作窗口起算；`judge_delay` 保留；feedback 经改进版本章（sha=S）AND **真实消费版本**精确 join。
- **register 可能滞后**：文件按 mtime 热读、无需 register 即生效，故修改时刻 t0 < 首次消费 t1 < register t2 为常态；窗口 MUST NOT 从 t2 起算而漏掉 t1→t2 的真实消费，窗口内前后版本请求按**真实版本**归因（不以当前 sha 一刀切重贴旧请求）。
- **缺激活证据**（未记录首次实际消费）→ 输出**样本不足**并具名缺失，MUST NOT 猜 commit 时刻或以 register ts 顶替。
- 结论枚举：健康 / 劣化 / **样本不足** / 未到期。样本量低于判据门槛时输出**样本不足**并中止判定，MUST NOT 折算为"健康"或"无信号"。
- 治理未启用时，DenialCount/CriticalCount 判据 SHALL 显式标注不可用（`governance disabled: …`），MUST NOT 以静默零值冒充无劣化。
- 被否方案：以"nil 恒真"或零值视为通过——与"缺证据不报通过"直接冲突。

**D07-6 结构评估门消费同一声明/绑定，错误因果保留。**
- Eval/Benchmark/ApplyGate 消费由组合根产出的**同一声明（OrgPlan）与同代执行绑定（StatePlan/租约）**，不得各自编译自己的版本；门失败 SHALL 保留 `errors.Is/As` 可达的具名根源（不把门错误压成无 `Unwrap` 的字符串）。
- 门失败**不退出**其他独立工作：无证据即 `design-blocked`/不通过，不以"赶进度"代替判断。
- 被否方案：各门自建配置解析/影子结构——与"唯一编排发布权"冲突。

## 文件白名单（未来实施候选，本轮不写）

- 实施期（未来）：`evolution/**`（冥想调度、refine、评估）、`plugin/**`（反思/提交后观测接点）、`resources/prompts/**`（冥想/反思 prompt）、`tests/integration/evolution/**`。
- 本轮仅 `openspec/changes/runtime-rebuild/changes/07-evolution/**md`。

## 上游复用矩阵（core `trpc.group/trpc-go/trpc-agent-go` @ `v1.11.2-tagent.1`）

| 本域产出 | 复用包/符号 | 不采用 | 注意点 |
|---|---|---|---|
| 反思接入点 | `plugin` 回调（model/tool callbacks）、`plugin.Manager` 可用接点 | 自建事件总线/反射调度 | 插件绑定**归 state**、按代借用、hook 恰一次；热更同代 |
| 模型决策观测 | `model` 原生 Response/事件流终局 | 二次迭代循环 | 复用 `02.4` 单一终局协议，不另造终局规则 |
| 计时/锚点 | 标准库 `time` | 引入调度框架 | 锚点为 tagent 自有簿记（AnchorStore 两锚），core 无对应符号 |

## Risks / Trade-offs

- [git 环境依赖] refine 依赖运行目录是 git 仓 → 非 git 环境降级为具名错误、文件仍热读生效；测试用临时仓（`TestGitNativeRefine`）隔离，不动用户仓。
- [水位新鲜度与让位交互] 让位/补位边界易错 → 以"水位不动 + pending 清零 + 下 tick 重评同一事实面"断言，覆盖完整。
- [评估样本不足误判] 短窗易被当成健康 → 四态显式枚举 + 门槛判据，缺证据即样本不足。
- [窗口锚张冠李戴] 以 commit/register 时刻开窗会漏掉 t1→t2 的真实消费、并误贴旧请求 → 锚**激活证据时刻**、按**真实消费版本**归因，GitSHA 只关联；缺激活证据即样本不足，以 `TestPostActivationEvidence` 正反例断言（t0 改、t1 消费、t2 register）。
- [结构门错误因果丢失] 门错误被压成字符串后下游 `errors.Is` 不可达 → 保留 `Unwrap` 链，以可达性真实用例证明（非文本包含）。

## Open Questions（待裁，不先设假默认）

- 真实模型评估窗（07.4 的真实模型门、07.3 的 LLM-judge 真实制品）的**资源是否到位/是否授权**——未核验前相关任务 `design-blocked`，不得标完成。
- 改进版本**激活时刻证据**的采集机制（由哪个组件记录首次实际消费、持久化在哪里）——属新内核簿记，需与 01/02 域接口对齐后在本域定，不在本域擅定。
- `evolution.protected_paths` 默认清单是否随新内核调整（属结构声明，需一级 design 复核，不在本域擅定）。
