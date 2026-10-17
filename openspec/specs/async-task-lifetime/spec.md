# async-task-lifetime Specification

## Purpose

本规范定义 async-task-lifetime 能力：派生任务的寿命如何被确定、续期、呈现、回收，以及回收产生的通知以何形态进入投影。年龄治理只有一个来源——统一 TTL 回收器；不存在并存的观测墙，也不存在按任务类型的年龄豁免。机制与理由见 `docs/wiki/agent/task-lifecycle.md#ttl-reclaim`，本页只立契约。
## Requirements
### Requirement: 统一 TTL 是所有任务的绝对寿命上限

任务 SHALL 在派生时确定一个有限 TTL：命令携带的 `ttl` 入参（整数秒，>0 生效）优先，否则取 `task_default_ttl`，该配置缺省时取管理器内建默认（10 分钟）。TTL MUST NOT 提供 0／负值／无限等"禁用"哨兵——回收对全体任务恒开。到期时唯一回收器 SHALL 经任务 owner 的 `Cancel()` 终止底层进程或会话、把任务一次结算为 failed、并使其退出看板。回收判据 MUST 只依赖"是否终态"，因此覆盖全部 active 态（running／stable／alive_detached／静默可疑态）与全部寿命类：job 与常驻交互服务同样受年龄墙约束，常驻性 MUST NOT 构成豁免，长寿命只能由模型显式设置较大 `ttl` 或经重入续命取得。

该 `ttl` 自设通道 MUST 同样及于 subagent 委派：包装工具 schema 携带可选整数秒 `ttl`（>0 生效、0 或缺省回落配置默认、负值拒绝），透传至任务声明并持久化于声明式参数，使跨重启的回放复原同一到期锚点——长时多轮委派不得只能撞默认地板强杀。

#### Scenario: 从未脱离的静默任务到期被回收

- **WHEN** 一个构建脚本转入静默可疑态且从未 detach，其年龄越过生效 TTL
- **THEN** 回收器经 owner `Cancel()` 终止其底层会话，任务一次结算为 failed 并移出看板，不再逐回合重渲染

#### Scenario: 常驻服务未自设寿命不享受豁免

- **WHEN** 模型派生一个常驻服务而未显式给出 `ttl`
- **THEN** 它按配置默认寿命到期强杀；类型本身不构成年龄豁免

#### Scenario: 跨重启回放保持同一寿命锚点

- **WHEN** 一个显式设了 `ttl` 的 subagent 委派在重启后经声明式回放重建
- **THEN** 其到期锚点与重启前一致，看板呈现的剩余寿命延续同一锚点

### Requirement: 写入型重入续期锚点，只读观察不续期

成功写入型 send/resume SHALL 更新同一 producer 的 TTL 锚点；peek MUST NOT 续命。Resume 的候选锚点 SHALL 在绑定受理记录中持久化，避免绑定提交后、首信号前崩溃丢失续期。意图受理后准备、绑定或实际发送失败时，系统 MUST 保留失败尝试轮号并恢复旧 producer、owner 和寿命锚点，不把失败尝试当作成功续命。显式TTL仍优先于默认值。

#### Scenario: 绑定提交后立即崩溃
- **WHEN** 新轮绑定已提交而尚无任何结果信号
- **THEN** 恢复读取该绑定中的候选寿命锚点，不退回旧锚点

#### Scenario: 输入发送失败
- **WHEN** Resume 已记录意图但实际SendKeys失败
- **THEN** 记录 round_aborted，旧 producer 的身份和TTL不变，下一尝试不复用失败轮号

### Requirement: 看板只呈现事实与剩余寿命，不诱发重复裁决

在途看板 SHALL 为每个 active 任务呈现其预计回收时刻或剩余寿命，使模型在同一回合内判明"继续等、重入续命、还是主动处置"；看板 MUST NOT 输出"长时间无输出，可能假死，需确认"这类非终态裁决邀请，也 MUST NOT 替模型自动取消或自动续期。到期任务与终态任务 MUST NOT 出现在看板上。看板仍是每回合重算、注入上下文尾部、不落库、不参与压缩的只读观察快照；展示规模有上限，超出即截断——完整账本在事实链里。

#### Scenario: 到期即从看板消失

- **WHEN** 一个静默任务越过 TTL 被回收
- **THEN** 下一次看板渲染不再含它，模型不再对其作出"维持原判"式重复裁决

#### Scenario: 剩余寿命一次可见

- **WHEN** 看板渲染一个进行中的服务任务
- **THEN** 该行含其剩余寿命，模型无需反复推断即可判断是否处置

### Requirement: 年龄机制只有一个来源，已删除的旧墙不留别名

系统 SHALL 只保留统一 TTL 一条到期强杀路径。原"仅观测不终止"的 stale 年龄墙与第二条 job deadline 墙 MUST 并入或删除，MUST NOT 并存多重重叠年龄墙；为其服务的旧字段名与仅向后兼容而存在的重映射 MUST NOT 留下任何读取路径。该删除 SHALL 由消除清单的静态检查固化——被删机制在非注释码面出现即测试失败——使旧别名无法静默复活。到期强杀 MUST 复用既有退役通路（owner `Cancel` → failed 终态 → 退役记账），MUST NOT 新增第二套清理所有者。

#### Scenario: 单一计时入口

- **WHEN** 检查年龄相关的配置解析与任务状态机
- **THEN** 仅 TTL 一条到期强杀路径，无并存的 stale／job deadline 读取，旧别名不被读取

### Requirement: 结算通知以有界票据进入投影并可被折叠回收

结算正文 SHALL 有界且可按票据取回；消费过的观察 SHALL 仅在 input_disposition 的源EventKey和partition精确核验后折叠。未消费观察、decision输入、用户输入和仍被恢复需要的事实 MUST 保留。批量退役可对无调用归属条目汇总通知，但每条raw信号 SHALL 独立保留受理与ACK责任；有 invocation 归属的条目 MUST 走原调用逐条路由。退役谱系 SHALL 只盖在信号上，MUST NOT 改写 Spec.Origin。

#### Scenario: 有处置凭据的观察被折叠
- **WHEN** 一个观察有与其源key及partition匹配的observe处置事实
- **THEN** 投影可回收其正文，原始事实仍可按key取回

#### Scenario: 伪造处置凭据
- **WHEN** receipt 引用错误源key或不同partition
- **THEN** 原观察不被视为已消费，也不得据此折叠

### Requirement: 静默探测与 TTL 正交

静默超时探测（判疑似挂起）SHALL 保留为与 TTL 正交的独立探测，用于区分"长时间合法静默"（构建、下载等可设较大静默阈值）与"绝对最长寿命"。二者 MUST NOT 合并：持续产出却失控的任务仍由 TTL 兜底，安静但存活的任务由静默阈值判定。会话级静默阈值 MUST 能覆盖全局默认值。

#### Scenario: 长构建不误杀、失控仍有上限

- **WHEN** 一个构建任务长时间静默但被设了较大静默阈值，同时其 TTL 也较大
- **THEN** 静默探测不提前判死，TTL 提供绝对上限，两条轴独立生效

### Requirement: 单一最近期限调度主动执行 TTL

生产 TaskManager SHALL 启用 deadline reaper，以一个可重排的最近期限 timer 处理 active TTL 及可回收终态，不使用逐任务 sleep 或固定扫空 ticker。无输入、无看板、无冥想时，到期任务仍 SHALL 由 owner 请求 Cancel 并一次退役为 failed。没有任务时 SHALL 停用 timer。RecheckDeadlines SHALL 从现有 TTL source 和真实锚点重新调度，MUST NOT 另存一份 TTL 真值。独立构建且未启用主动调度的管理器可保留显式 List/reconcile 行为。

#### Scenario: 完全静默任务到期
- **WHEN** 已纳管任务到期后没有任何外部唤醒
- **THEN** 最近期限 timer 仍触发取消和失败退役

#### Scenario: 默认 TTL 热更新
- **WHEN** 当前 source 降低默认 TTL 并调用 RecheckDeadlines
- **THEN** 既有无显式TTL任务按新值重新排期，显式TTL与任务锚点不被覆盖

