# 设计决策（D1–D9）

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | decision-boundary | `event-boundary`（ADDED） | 在途 turn 的每个 decision 边界重新聚合链上就绪事件（含用户/外部输入）；host Invoke 受理语义与 gate 降级 | `persistent-event-loop` 现条款（:94/:105/:123 保留不动） |
| 02 | meditation-activity | `meditation-activity`（MODIFIED `meditation-idle-gating`） | 节奏门加用户事件新鲜度闸与决策席在途闸；自管回合不重置 | `event-metadata-contract` 的 trigger_source 读径（相同，不新建） |

## D1 决策边界就绪增量（域 A 核心）

- `Loop` 新增窄读面 `ReadyFeed`（消费方定义：`func(ctx, after EventID) []event.Event`——按会话读 `after` 之后的新事实），装配时注入；nil=现行为（兼容）。
- `Drive` 循环每轮 `decide` **之前**：`observeReady` 从「仅在开头跑一次」改为「每 decision 边界对增量再跑一次」——增量=链上自本 invocation 上次消费位以来的新事件（含用户消息、通知、注入）；新事件按下标成事实并入 transcript。
- **决策因果可重放**：每个 decision 的输入=其时刻链上事实集（decision 事实落链自带 inputs 引用），重放边界不变——不破坏既有可重放性。
- 首个 decision 语义不变（ready=Invocation.Ready 聚合同样适用）。

## D2 host Invoke 受理语义（域 A）

- 用户/外部输入到达：**先落链成事实**（evt 事实，`landing` 序号），返回受理回执（`ok` 最小回执语义沿用）。
- 该 session 无在途 turn → 起 turn（gate 获取成功才起）；有在途 turn → **只落事实即返回**，由在途 turn 下一 decision 边界消费。
- 无在途 but gate 被占的微窗口：落事实后挂到 gate 等待起 turn（与现状排队等价，但消息已先入链，语义=「已受理」）。

## D3 gate 降级为 turn 发起权门（域 A）

`addressGate` 语义从「消息门」改为「**turn 发起权门**」：同 session 仍至多一个在途 turn（决策席串行不变）；gate 不再阻塞消息入链，只仲裁「谁起 turn」。`ErrSlotHeld`（决策席）与 `ErrShutdownRefused` 语义不变。

## D4 外部输入规格条款（域 A delta）

`persistent-event-loop` ADDED Requirement「在途 turn 的外部输入在 decision 边界消费」：外部输入（`source==user`/渠道）SHALL 先成事实；在途 turn SHALL 在下一 decision 边界聚合并消费，SHALL NOT 排队至 turn 结束另起 turn，SHALL NOT 被执行中的 decision 打断消费。Scenario 两条（在途消费/无在途起 turn）。**不改** :94/:105/:123 的 task_settled 条款（两者同构，条款并列）。

## D5 冥想活跃闸（域 B）

- 判据数据源与 novelty 门同源（事实链 `trigger_source`）：driver 记录 per-session `lastUserEventAt`（`source==user` 的事实时间）；fire 前判：`now - lastUserEventAt < min_gap` → **不 fire**（冷却自最后用户事件重置）；自管谱系（task_settled/回执/摘要注入）不更新 `lastUserEventAt`（家务回合不阻塞条款保留）。
- 决策席在途：driver fire 前读该 session 决策席占用（装配注入窄读面 `SlotQuery`），占用→不 fire（**推迟非放弃**——水位不动、pending 机制沿用；下个 tick 重评）。
- 与 `meditation-idle-gating` 现条款关系：MODIFIED 节奏门 Requirement——保留「时间门槛禁令」原文（反时间阈值），新增「用户事件新鲜度」与「决策席在途」两侧判据（结构性判据，非时间门槛）；其余条款（novelty/让位/水位执行语义）一字不动。

## D6 被否方案

| 方案 | 否决理由 |
|---|---|
| 立即打断（interrupt 语义：取消在途工具/丢弃未归约流） | 破坏决策因果重放（每个 decision 输入须=链上事实集）；用户已裁定取决策边界消费 |
| 双轨开关（配置选忙时保留/即时消费） | 双轨维护成本高；规格已是 decision 边界语义，实现对齐即可，无需模式 |
| 规格推翻忙时保留条款 | 考古证明规格本就是「下一轮 decision 消费」；推翻无对象，用户推翻的是实现现状 |
| 冥想用纯时间门槛判忙闲 | 规格明文禁止（回合时长无上界）；改用结构性判据（用户事件新鲜度+席位占用） |

## D3'（波次表）与 D5'（三级验收）

- 波次：W1=01 域 A（A1 红锚→A2 loop 增量→A3 装配注入→A4 host 语义→A5 规格 delta+锚迁移）；W2=02 域 B（B1 红锚→B2 driver 闸→B3 规格 delta）；W3=F 收尾。域 A 是核心循环改动，单独一波做完再动 B（B2 读决策席面依赖 A4 的席位语义稳定，标接口常数依赖）。
- 三级验收：孙任务 exit 0（正锚+负锚+消费者锚）→ 子域 Scenario 逐条映射测试 → 变更级 DoD（两症状锚绿+回滚即红+全量门禁）。

## D8 合并规则

- 01 域内 A2/A3 同触 loop 装配面→合单；A1 红锚先行独立单（必须第一天红）。
- 02 域单波单代理。
- 共享面（`assembly/host.go` 的 Invoke 路径）归 A4 单写；B2 不碰 host。

## D9 回退

每域改动独立可回退：A2/A3 以 `ReadyFeed` nil=现行为为回退开关（无配置面，代码级并存）；B2 以活跃闸参数零值=现行为。回退后与基线零 diff（F1 契约目录断言+回退复跑症状锚必红）。
