# evolution-evaluation Specification

## Purpose

后验评估能力:窗口锚=improvement 事件(register commit 时刻)、judge_delay 一次性快照、结论四态(健康/劣化/样本不足/未到期)、劣化只产 evaluation 建议事件;bundle/发布道体系退役。
## Requirements

### Requirement: 后验评估锚点(improvement 事件)

LLMJudge 与 MetricGuardrail 的评估窗口 SHALL 以 improvement 事件的**激活证据时刻**为锚点——事件即窗口表，无独立时刻表组件；窗口锚 SHALL 为改进版本**首次被真实请求消费**的激活时刻（文件按 mtime 热读、无需 register 即生效，故 register 可晚于实际消费），MUST NOT 以登记 commit 时刻为锚。窗口证据（feedback join、治理拒绝率等）SHALL 从激活时刻起算。GitSHA（改进版本章）SHALL 仅作**关联**（把 feedback/因果子事件归到真实消费版本），MUST NOT 用作窗口起算时刻；窗口内前后版本的请求 SHALL 按**真实消费版本**归因（真实版本边界），MUST NOT 以当前 sha 一刀切重贴旧请求。`judge_delay`（原 canary_hold 语义）SHALL 保留。缺激活证据时输出 SHALL 为**样本不足**并具名缺失，MUST NOT 退化为按 commit 时刻开窗或猜测时间。

#### Scenario: 窗口从激活时刻起算（文件热读，register 滞后）

- **WHEN** 文件在 t0 被修改（mtime 热读即生效）、某请求在 t1 实际消费新版本、refine register 直到 t2 才提交
- **THEN** 评估窗口锚为 t1 的激活证据；t1 起的请求按真实消费版本归因，MUST NOT 从 t2 起算而漏掉 t1→t2 的消费；GitSHA 仅用于把 feedback 归到版本

#### Scenario: 缺激活证据即样本不足

- **WHEN** 某改进窗口无法给出激活时刻证据（未记录首次实际消费）
- **THEN** 评估输出**样本不足**并具名缺失激活证据；MUST NOT 猜 commit 时间或按 register ts 开窗

#### Scenario: feedback 经版本章精确 join

- **WHEN** 某激活版本（sha=S）开启后产生任务结算负反馈
- **THEN** feedback 经版本章 sha=S 精确 join 到该激活窗口的真实消费版本

### Requirement: 建议式劣化信号(框架不动手)
guardrail/judge 判定劣化时,系统 SHALL 将结论与回滚建议写为 evaluation 事件(Content 含窗口 sha、verdict、证据摘要、建议 `refine rollback <sha>` 文案与「先 diff」引导),经既有事件消费面(冥想 digest 钩子/召回)渗透给 agent;MUST NOT 自动执行任何 git 回滚操作,亦 MUST NOT 经消息注入通道打断——执行权永远在 agent/人。

#### Scenario: 硬指标劣化仅建议
- **WHEN** 某窗口负反馈率超阈(guardrail Breach)
- **THEN** evaluation 事件落库(含证据数字与 sha);agent 在下一反思点经冥想 digest 看到建议,自行决定 rollback、diff 复核或保留观察;框架不执行 revert

#### Scenario: LLM-judge 劣化仅建议
- **WHEN** judge 评估窗口证据判定劣化
- **THEN** 同上——evaluation 事件渗透,无自动回滚

### Requirement: bundle 体系退役
系统 MUST 删除 BundleStore/Bundle/InitBaseline/VersionedSource/BundleProvider 与 ReleaseManager 发布状态机(Lane/Stage/Submit/快慢道/ProtectedPrompts/预算 Gate)及 refine 的 propose/diff 操作;evolution 配置段的发布道字段 MUST 同步删除。系统 MUST NOT 保留 bundle/git 双后端开关。

#### Scenario: evolution 启用下的提示词真源
- **WHEN** evolution 启用且 agent 修改 resources/prompts/ 下系统提示词文件
- **THEN** 改动经 mtime 热重载直接生效——无任何快照遮蔽层(旧 VersionedSource 遮蔽行为消失)

#### Scenario: 旧 bundle 存档
- **WHEN** 存量部署带有历史 bundle JSON 存档
- **THEN** 升级后存档保持只读原样(无自动迁移),运行不受影响;文档提供一行迁移注记

### Requirement: 改进版本章(feedback join 保持)

persistBusEvent 盖章的版本章来源 SHALL 以**实际请求消费的改进版本**为准（把 feedback 与因果绑定的子事件归到真实消费版本），MUST NOT 以「最新 improvement 事件的 sha」作为章来源。GitSHA 映射 SHALL 仅作**因果关联**：refine `register` 之后追加的 Git 映射只**补**关联，MUST NOT 改写已存事实（MUST NOT 把已落库的旧版本请求/feedback 重贴到后来登记的版本）。无关联证据的旧数据 SHALL 标 `unknown`/`insufficient` 并具名缺失，MUST NOT 按时间窗 join 或猜想时间归因。该版本章 MUST NOT 被当作评估窗口的起算时刻（窗口锚为激活证据，见「后验评估锚点」）；feedback 与窗口 SHALL 按**真实消费版本**精确 join。BindFeedback 的章继承机制与键名（MetaKeyBundleID）MUST 保持不变。

#### Scenario: feedback 精确 join 改进版本

- **WHEN** 窗口 sha=S 开启后产生任务结算负反馈
- **THEN** 该 feedback 事件及因果绑定的子事件携带章 bundle_id=S，guardrail 窗口评估沿章与真实消费版本精确归因（章不参与窗口起算）

#### Scenario: 旧版本请求在新版本登记后结算不得重贴

- **WHEN** 某请求在版本 A 下被消费并产生 feedback/结算，其后版本 B 的 refine 才 `register`
- **THEN** 该 feedback/结算仍归版本 A（真实消费版本）；`register` 追加的 Git 映射只补 B 的关联，MUST NOT 把 A 的旧记录重贴到 B

#### Scenario: 无关联不得时间猜测

- **WHEN** 某 feedback/结算无法确定其真实消费版本（无 Git 关联证据）
- **THEN** 该记录章标 `unknown`/`insufficient` 并具名缺失；MUST NOT 按时间窗 join 或按 `register` ts 猜想归因

### Requirement: guardrail 信号可用性显式声明

MetricGuardrail 的 DenialCount/CriticalCount 判据依赖治理事件流：治理未启用时该两判据 SHALL 在评估输出（evaluation 事件与日志）中显式声明不可用（如附 `governance disabled: denial/critical signals unavailable`），MUST NOT 以静默零值冒充「无劣化信号」；此耦合 SHALL 同步写入相关 wiki/spec 文档。

#### Scenario: 治理关闭下的后验评估

- **WHEN** 治理闸未启用且一次 self-improve 注册进入后验评估窗口
- **THEN** evaluation 输出包含治理信号不可用的显式标注，判读者不会将零拒绝率误读为健康证据

### Requirement: 评估结论四态与"样本不足不得判健康"

评估（MetricGuardrail / LLMJudge）的输出 SHALL 为**显式四态**之一：健康 / 劣化 / **样本不足** / 未到期。当窗口内可判定证据量低于判据门槛、或证据源缺失/未接读缝时，输出 SHALL 为**样本不足**并中止判定，MUST NOT 折算为"健康"或"无劣化信号"，亦 MUST NOT 以"nil 恒真"或静默零值冒充通过。四态 SHALL 在 evaluation 事件与日志中可区分。

#### Scenario: 短窗证据不足

- **WHEN** 某改进窗口已到 `judge_delay` 但窗口内可判定反馈样本低于门槛
- **THEN** 评估输出为**样本不足**（非健康），evaluation 事件显式标注样本量不足，判读者不会将其误读为健康证据

#### Scenario: 证据源缺失即样本不足

- **WHEN** 评估所需证据源不可达/未接读缝
- **THEN** 输出样本不足并具名缺失源；不按"无信号=健康"放行

#### Scenario: 四态互斥可区分

- **WHEN** 审查同一窗口的评估输出
- **THEN** 其 verdict 恰为四态之一；未到期不产生劣化建议；样本不足不产生"健康"结论

### Requirement: 结构评估门消费同一声明与同代绑定，缺证据不报通过

结构候选的 Eval / Benchmark / ApplyGate SHALL 消费由组合根产出的**同一声明（OrgPlan）与同代执行绑定（StatePlan/租约）**；各门 MUST NOT 自建配置解析、影子绑定或并行声明。门判定失败时 SHALL 保留 `errors.Is/As` 可达的**具名根源**（MUST NOT 将门错误压成无 `Unwrap` 的字符串），使下游可精确归因；缺证据 SHALL 判**不通过**（或 `design-blocked`），MUST NOT 报通过。门失败 MUST NOT 连带停掉其他真正独立的工作。

#### Scenario: 同源声明/绑定

- **WHEN** 一次结构候选进入评估门
- **THEN** Eval/Benchmark/ApplyGate 消费的是同一份 OrgPlan 与同代 StatePlan/租约；不存在各门各自编译的版本

#### Scenario: 缺证据不报通过

- **WHEN** 某门所需证据缺失或不可得
- **THEN** 该门判不通过/`design-blocked`，不产出"通过"结论；其他独立工作继续

#### Scenario: 错误因果可归因

- **WHEN** 门判定失败
- **THEN** 调用方经 `errors.Is/As` 能到达具名根源（以可达性真实用例证明，而非"文本包含"代替）
