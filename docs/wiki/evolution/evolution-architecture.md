> **活契约页**：逐条对齐当前码面（`features/evolution/` 的 `activation`／`posterior`／`gate`／`policy`／`judge`／`criteria`／`structural`，以及 `meditation/` 的 `watermark`／`judgment`），代码内 `// 契约:` 注释指向本页锚点。口径统一于[存储概念地图](../../demos/storage-concept-map.html)：**链**管身份与因果 · **原文仓**管字节 · **账本**管责任 · **其余一切现算**——后验评估读的是链上的事实，窗口与结论都是现算读数，不另立存储。

# 自我改进（evolution）：记录真实消费，评估，只给建议

`evolution` 不拦改动：提示词/技能/脚本落盘即在下一次请求边界被热读，**改即生效**。本包只做三件事——把"某版本真的被消费过"记成证据、窗口到点后做后验评估、把劣化写成建议。**回滚不由框架执行**：`refine rollback <sha>` 只是一句写进评估事件的文本，仓内没有 refine 工具，`git` 动作由人做。

## 一、四条前提（判据保留，承载点已换）

| 前提 | 当前承载 | 违反后果 |
|---|---|---|
| 文件即真源 | 文件热读，登记不是生效前提 | 把登记做成门槛，agent 会在未登记时静默不生效 |
| 复用 git | 只存 `version → registration SHA` 的**关联**（`ActivationLedger.Correlate`），零自建版本库 | 自建台账必然与 git 分叉 |
| 建议式而非动手 | 劣化只产出 `Evaluation` 事件，进反思 digest | 自动回滚会在证据不足时错杀低风险改动 |
| 分层红线 | 本包不 import `governance`；发布闸以 `state.Governance` 的一问一答接入 | 反向依赖让评估器无法独立测试 |

<a id="evidence-window"></a>
## 二、证据窗口：起点是第一次真实消费，不是登记时刻

文件落盘即生效，所以**真实消费总在 git 登记之前**：写盘 t0 < 消费 t1 < 登记 t2。窗口锚在 **t1**，`ActivationLedger` 的第一条 `Activation` 开窗、后续消费只追加不动锚，**t2 的登记既不开窗也不挪窗**（`activation.go`）。`Activation{Version, At, Evidence}` 三字段缺一即 `ErrActivationUnusable` 拒绝——证明不了一次真实消费的记录，不得当窗口证据。

- **台账是内存态**：从已记录事件重建，从不持久化。重启后对重启前激活的版本失去锚点，评估如实落到 `insufficient`（点名缺激活证据），**绝不退化成健康**。
- **消费记录不被重贴标签**：`Correlate` 只新增 `version → sha`，与已记录映射冲突即 `ErrCorrelationConflict`，旧映射为准。
- **反馈按两条件精确 join**：`Consumed == version` 且 `Stamp ∈ {version, registration}`（`joinFeedback`），其余进入具名排除清单。不做时间窗猜测，也不把整库反馈算进这一版。
- **窗口的消费侧就是反思水位门**：`Watermark` 只被**真正被消费**的反思轮推进到注入时刻；仅注入不推进、被延后的轮次不得推进、读不到即关闸（`ErrAnchorStoreUnusable`／`ErrAnchorUnreadable`），**绝不回落到新默认值开闸**。`hostFacingStamps` 是闭表——`user`／`task`／`reincarnation`／`system_alert` 才算"有人在等"，其余血统（含 agent 自己的反思轮）不开新鲜度门，防"自省引发的自省"成环。

<a id="verdict-states"></a>
## 三、结论四态与发布闸四态：两套词汇不互用

**后验结论只有四态**（`PosteriorVerdict`）：`healthy`／`degraded`／`insufficient`／`not-due`。

| 态 | 判据 | 不许被读成 |
|---|---|---|
| `healthy` | 窗口到点，可判证据过阈且未见劣化 | —— |
| `degraded` | 同一阈值下证据显示劣化，产出回滚建议 | 执行动作（本包不回滚） |
| `insufficient` | 试图判定但可判证据不足，或某来源缺失/不可达；**必须点名缺哪份证据** | `healthy`（样本不足不得冒充健康） |
| `not-due` | 窗口还没到判定点，不欠判断 | `healthy`（未判 ≠ 健康） |

四态互斥：`insufficient` 不掺 `degraded`（中止的判断不断言劣化），`not-due` 不读成 `healthy`。**还没产出结论是空 `PosteriorReport`，不是第五态**，空值同样绝不读成健康。

**发布闸是另一套词**（`GateVerdict`）：`pass`／`reject`／`not-applicable`／`insufficient-evidence`。`StructuralGate` 三段有序——`Eval` → `Benchmark` → `ApplyGate`——每段拿到**同一份声明计划与同一代执行绑定**，四条红线：同一份输入、失败保留 `errors.Is`／`errors.As` 可达的具名根因、缺证据绝不报 `pass`、段内不自建第二份声明或影子绑定。`Policy.Check` 把三段折成 `state.Governance` 的三个回答（`insufficient-evidence` → 未答，`not-applicable` → 豁免），**缺证据记为未答**而不是通过；`structural.go` 的 `GateError` 把根因原样带出。

## 四、双闸互补：指标闸一定性、评审器一质性

| 闸 | 特性 | 抓什么 |
|---|---|---|
| `Guardrail`（指标闸） | 确定性、廉价，先于评审 | 拒绝率 / critical 操作率 / 负反馈率超阈 |
| `JudgePolicy`（评审器） | 慢、质性 | 阈值覆盖不到的整体劣化 |

默认阈值 `DefaultMinSamples=5`、`DefaultNegativeRateMax=0.3`、`DefaultDenialRateMax=0.3`、`DefaultCriticalRateMax=0.2`；**零值折默认，负值＝显式禁用**（"不可达值"不是禁用形态）。样本不足或证据收集失败一律不判劣化。评审器同样保守：只有模型明确判出低于阈值才建议回滚，未配置、证据失败、样本不足、调用失败、解析失败一律保守通过；`judgeAnswer.Score` 用**指针**接收，字段缺失（含显式 `null`）不等于 0 分；响应先取首个 `{ … }`、越界夹到 `[0,1]`。

**建议文本**（`rollbackAdvice`）只有两形态：有登记 SHA 时 `refine rollback <sha>; review with git diff`；无登记时 `refine rollback unavailable: …`（点名无可关联提交）。SHA 是版本的 git 关联，不是窗口锚点。

**证据形状**（`criteria.go`）：`EvaluationCriterionRef` 的引用族固定为 `evaluation`，每条判据带 `id`／`task`／`expect`／`baseline` 四字段——判据是声明出来的形状，不是从某次运行反推的形容词。

## 五、测试接线必须钉住的三件事

1. 种进存储的事件必须用真实 Snowflake key——分区从 key 自身推导，裸整数 key 会让读回静默丢事件。
2. `git status --porcelain` 对未跟踪内容取目录级展示，断言按目录前缀写。
3. `encoding/json` 字段匹配大小写不敏感，`{"Score":0.9}` 不算"字段缺失"，保守分支只覆盖真缺失与显式 `null`。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 回滚无自动化 | 建议文本靠人执行，仓内无 refine 工具 | 若要自动化，须先落"回滚作为终结动作"的授权面 |
| 激活证据重启即失 | 内存台账 + `insufficient` 如实降级 | 从链上事件重建锚点的持久读法 |
| 阈值缺实测标定 | 5 / 0.3 / 0.3 / 0.2 为默认 | 一次跨场景标定，写清低样本场景的口径 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（链／原文仓／账本／现算的分工）· [存储时光机](../../demos/storage-time-travel.html)
