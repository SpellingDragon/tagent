## ADDED Requirements

### Requirement: 异步命令 TTL 贯通与到期强杀

异步命令 SHALL 在 spawn 时确定一个有限 TTL：命令携带 `ttl` 参数则用之，否则取配置默认（复用原 `task_job_deadline` 配置位并更名以反映"默认 TTL"语义），配置缺省为 10 分钟。TTL MUST NOT 提供 0/负值/无限等"禁用"哨兵——回收器对全体任务恒开。TTL 到期时，回收器 SHALL 经唯一 reaper 以 owner `Cancel()` 终止底层进程/会话，写 failed 终态，reap 记录，并使其不再出现在后续看板。该覆盖 MUST 及于全部 active 态（running/stable/alive_detached/stale/suspect）与全部寿命类（job 与 resident/interactive 服务），消除 suspect 等未 detach 态此前不受任何年龄墙约束的盲区。

常驻/交互服务不因类型被豁免，而由模型在 spawn 时设置足够大的 TTL 或经重入续命维持；模型未设即接受默认 10 分钟到期强杀，此为既定语义。

该 `ttl` 自设通道 MUST 同样及于 subagent（AgentToolWrapper）：包装工具 schema 携带 `ttl`（可选整数秒，>0 生效、0/缺省回落配置默认、负值拒绝），透传至 TaskSpec.TTL 并持久化于 Declarative.Params，`SubagentSpecFromDeclarative` 回放时复原该锚点——使长时多轮委派不再只能撞 10 分钟地板强杀，与 command 类对称。

#### Scenario: 模型为长时 subagent 自设 TTL
- **WHEN** 模型 spawn 一个预计超过 10 分钟的多轮 subagent 委派并在工具调用里带上 `ttl`
- **THEN** 该 ttl 成为其到期锚点（不再受 10 分钟地板强杀），看板呈现其剩余寿命；跨重启经声明式回放保持同一锚点

#### Scenario: 未 detach 的 suspect 命令到期被回收
- **WHEN** 一个换装/服务脚本转入 suspect（静默、从未 detach）且已超过其 TTL
- **THEN** 唯一 reaper 终止其底层会话并写 failed 终态，任务从看板移除，不再逐回合重渲染

#### Scenario: 正常完成的命令 TTL 不放行
- **WHEN** 一次性命令在 TTL 内 settle 为 completed
- **THEN** 记录老化移除，到期强杀路径不触发，结果照常回写

#### Scenario: 常驻服务由模型自设寿命
- **WHEN** 模型 spawn 一个 `mode:resident` 服务并显式带较大 `ttl`
- **THEN** 该 ttl 成为其到期锚点；缺省未设时按配置默认（无配置=10min）到期强杀，服务不享受年龄豁免

### Requirement: 重入刷新 TTL

模型对既有任务会话的**写入型重入**（`resume_task` 或 `exec op=send` 注入输入）SHALL 把该任务 TTL 锚点重置为 `now + ttl`；只读 `op=peek` MUST NOT 刷新。刷新后到期强杀按新锚点计算。一次性无会话命令无重入路径，锚点恒为 spawn。

#### Scenario: resume 续命
- **WHEN** 模型对 alive-detached 会话执行 resume 并注入新输入
- **THEN** 该任务 TTL 顺延为自本次重入起的完整 ttl，看板剩余寿命相应更新

#### Scenario: peek 不续命
- **WHEN** 模型仅以 `op=peek` 读取会话增量输出
- **THEN** TTL 锚点不变，到期仍按原时刻强杀

### Requirement: 看板呈现剩余寿命并移除误导裁决邀请

后台任务看板 SHALL 为每个 active 任务呈现其 TTL/预计回收时刻（或剩余寿命），使模型一次判明是否需处置，无需反复推断。看板 MUST NOT 再以"⚠ 长时间无输出，可能假死，需确认"之类非终态邀请诱发模型逐回合空转裁决；到期或终态任务 MUST NOT 出现在看板上。看板仍为每回合重算、注入尾部、不落库、不参与压缩的只读观察快照。

#### Scenario: 到期即从看板消失
- **WHEN** 一个 suspect 任务越过 TTL 被回收
- **THEN** 下一回合看板不再含它，模型不再对其作出"维持原判"式重复裁决

#### Scenario: 剩余寿命可见
- **WHEN** 看板渲染一个进行中的换装/服务任务
- **THEN** 该行含其预计回收时刻/剩余寿命，模型据此判断是否需要重入续命或主动处置

### Requirement: 复用取代既有年龄机制为单一 reaper

系统 SHALL 只保留一个年龄回收器（TTL）。原 `task_job_deadline` 的年龄终止语义与 `task_stale_after` 的"仅观测不终止"态 MUST 并入或删除，不并存多个重叠年龄墙；`stale` 观测态与 `task_stale_after` 配置项随之取消。已弃用的 `TaskMaxDetachedAge` 重映射等仅为旧版兼容而存在的分支按决策10删除，不留别名读取。到期强杀复用 `detector.Cancel`+failed 终态+reap 的既有退役通路，不新增第二套清理所有者。

#### Scenario: 单一计时入口
- **WHEN** 检查任务年龄相关配置与状态机
- **THEN** 仅存 TTL 一条到期强杀路径，无并存的 job_deadline/stale_after，旧别名不被读取

### Requirement: 结算通知票据化折叠与批量汇总

批量退役 SHALL 折叠为单条汇总事件（N→1），不为每个退役任务各发一条独立通知。`[task settled]` 结算类 external_input MUST 以有界票据形式进入投影：正文携带可召回的事件键票据与受限预览，其全量体在事实链按票据可取；投影压缩 SHALL 能像对待其它内容一样折叠这些结算票据，消除"工具对折叠不覆盖 external_input 致其结构性不可回收"的死重。此改动只界定新通知的产生形态与其可折叠性，不改写通用压缩/回放算法本身。

#### Scenario: 批量退役单事件
- **WHEN** 同时有多个任务退役
- **THEN** 仅产生一条合并汇总 external_input，而非逐任务多条

#### Scenario: 结算通知可折叠回收
- **WHEN** 历史中积累了多条 `[task settled]` 结算通知且上下文逼近预算
- **THEN** 压缩可将其折叠为票据引用并回收正文，不再因 external_input 豁免而不可回收

### Requirement: TTL 回收时机为下一次唤醒（懒触发，无后台计时器）

回收器 SHALL 由活动唤醒驱动（看板渲染、冥想周期、冷启动清点），MUST NOT 为 TTL 到期引入一个独立常驻的后台 ticker。常驻 bot 在无输入的完全静默期没有任何回收需求方，ticker 只会回收无人观察的任务并空耗唤醒。因此到期任务的"从看板消失时刻"= 下一次唤醒；完全静默期内物理回收可滞后于 TTL 名义到期，一旦有输入/看板/冥想触发即收敛。此为既定语义（resident-review-fixes 3.4 明示），非缺陷，评审据此不引入 ticker。

#### Scenario: 完全静默期到期任务滞后回收
- **WHEN** 一个任务越过 TTL 但此后进程完全静默（无输入、无看板渲染、无冥想触发）
- **THEN** 其物理回收滞后到下一次唤醒；下一次看板渲染时该任务已不在 active 集合中

### Requirement: 静默探测与 TTL 正交

`quiet_timeout`/fake-dead（静默判疑似挂起）SHALL 保留为与 TTL 正交的独立探测，用于区分"长时间合法静默"（如构建/下载，可设较大 quiet_timeout）与"绝对最长寿命"。二者 MUST NOT 合并：持续产出的失控任务仍由 TTL 兜底，安静但存活的任务由 quiet_timeout 判定。

#### Scenario: 长构建不误杀、失控仍有上限
- **WHEN** 一个构建任务长时间静默但被设了较大 quiet_timeout，同时其 TTL 也较大
- **THEN** quiet_timeout 不提前判死，TTL 提供绝对上限；二者独立生效，语义不混淆
