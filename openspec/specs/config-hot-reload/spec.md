# config-hot-reload Specification

## Purpose
配置热重载的统一应用模型：全部热参数与结构候选在同一次混合事务中准备就绪才提交，结构发布不跳过数值应用、数值应用不落在无人服务的对象上，杜绝半应用代。
## Requirements
### Requirement: 配置热更统一应用模型

配置热更 MUST 以逐 agent 解析默认值后的完整有效配置为基线。五个既有独立热参数（compress_threshold / max_tokens / keep_recent_tasks / task_terminal_ttl / task_default_ttl）与结构指纹字段在同一次混合候选中 MUST 全部准备成功才提交；结构发布不能跳过数值应用，数值应用不能仅修改常驻 entry 或不服务请求的 resident 对象。

五个热参数 SHALL 保留原热应用语义，不为修补对象错位而一律纳入结构指纹。每个 owner SHALL 通过稳定读取入口取得同一已提交应用记录中的完整热参，子调用初始化及在途安全预算／压缩边界按需读取；一次压缩内外层使用同一组参数。MUST NOT 靠对全部私有 CM 的后台逐实例 setter 广播维持一致，也不让执行壳自建权威快照。活跃调用登记只服务实际取消／完成责任，不成为热参订阅历史。

实际任务 spawner／TaskManager SHALL 在默认寿命、reaper 回退和终态保留消费点取得有效 TTL。已有任务显式 TTL 和寿命锚点不被重新解释；被移除但排空中的 owner 的最后有效值保留至退出，不因缺定义改默认。字段删除或零值按现有规范化规则回落，不非零合并遗留旧值。工具输出封顶仍按工具构造期派生，numeric-only 不改写已发布工具。

#### Scenario: 热新增后更新跨越私有 CM 构造窗口

- **WHEN** 先热新增 B，再 numeric-only 更新 B，更新与真实 B 调用的私有 CM 构造及下一压缩边界交错
- **THEN** 发布后开始的 B 调用初始读取有效值，先开始的调用下一压缩边界读取最新完整组，无注册空窗漏更；实际压缩预算及任务默认 TTL 正确，回滚走相同消费源

#### Scenario: 同次修改结构字段与数值字段

- **WHEN** 一次保存修改 entry 的 system_prompt 和子 agent 的 max_tokens
- **THEN** 全部候选成功后一起生效，后续真实子调用的预算读取使用新值；失败时结构及热参源均保持旧值

#### Scenario: 数值修改到达实际子调用

- **WHEN** 仅修改热新增或结构发布后子 agent 的 max_tokens 或 keep_recent_tasks，而当前委派使用已发布工具绑定
- **THEN** 不必重建结构 generation，新调用和存活调用下一次安全读取使用新参数；只读 resident getter 不构成通过证据

#### Scenario: 字段删除回落默认

- **WHEN** 删除某 agent 显式设置的 keep_recent_tasks
- **THEN** 实际消费方使用解析默认值，未修改旁支维持原配置，不沿用被删除的值

#### Scenario: 默认 TTL 不改写已有显式任务寿命

- **WHEN** 更新 task_default_ttl，同时存在携显式 TTL 的存量任务和随后使用默认 TTL 创建的新任务
- **THEN** 新任务使用有效默认值，已有显式 TTL 与寿命锚点保持；回执与真实任务行为一致

### Requirement: 压缩参数同代快照
热更后的压缩参数 MUST 作为同一有效代快照应用：外层触发线（ContextCompressor）与内层压缩目标（SmartCompressor 的 maxTokens/triggerBudget/keepRecent 及派生容量）MUST 在一次应用中同步换装，单次压缩 MUST 读取同一快照。

#### Scenario: 运行中缩小窗口后触发压缩
- **WHEN** 运行中把 max_tokens 从 512k 调小到 128k 并随后触发压缩
- **THEN** 内层压缩目标 MUST 按新预算执行（最终请求降到新预算内），而非沿用冷构造旧值导致 no-op

### Requirement: 热更回执报告 effective 状态

每次配置应用 MUST 报告 desired/effective、结构 generation、实际应用范围和保持／拒绝原因，以及可验证的退役引用与 owner 状态。应用成功意味着消费源已安装且后续行为使用该值，不等同于调用一次 resident setter。lastAppliedAt SHALL 表示完整配置最近成功应用时间（含 numeric-only），lastPublishedAt SHALL 单独表示最近结构发布／回滚时间。失败不推进成功时间。

#### Scenario: 热更回执核对

- **WHEN** 任一次热更完成
- **THEN** 回执中的预算和 TTL 能与真实子模型／任务行为互证，失败明确未生效，排空者明确保持而非 applied

#### Scenario: 数值应用不伪造结构发布时间

- **WHEN** numeric-only 修改成功且结构绑定不变
- **THEN** lastAppliedAt 与完整 effective 配置更新，lastPublishedAt 和结构 generation 不推进

### Requirement: 执行代绑定完整性
结构热更换入新 executor 时，新代对象 MUST 完成对常驻状态的绑定后才视为成功：工具 wrapper 的 parentProjection MUST 指向常驻投影（非新壳空投影）；system prompt getter MUST 按执行代不可变快照读取；常驻 cm 的 execCfg MUST 更新为生效代配置。

被钉委派（继承发起调用租约的子跳）的验收 MUST 按**回合身份**取该跳的答案，MUST NOT 以"发布后第一条新答案"之类的顺序下标代表被测跳：发布抬起的通知回合会在新代面上产生同类答案，其到达顺序不是被测属性。

#### Scenario: 热更后子 agent 自动上下文注入
- **WHEN** 结构热更成功后调用支持 event_keys 的子 agent 且未显式提供 event_keys
- **THEN** 自动上下文注入 MUST 与热更前等价（读取常驻投影而非空投影）

#### Scenario: 被钉跳的验收锚定被钉跳本身
- **WHEN** 一轮子调用在发布前被挂起，发布后通知回合与它各自产生一条同类答案
- **THEN** 验收 MUST 只取被挂起那一轮的答案来断言代际，且 MUST 在断言前确认该答案已出现；通知回合的新代答案 MUST NOT 被当作被测跳的答案，也 MUST NOT 替它达标

### Requirement: 首代可回滚
热更 reloader MUST 在安装时保存启动代有效配置快照；第一次结构热更成功后 Rollback MUST 可用（不得因无上一代快照而失败）。

#### Scenario: 首次热更后立即回滚
- **WHEN** 进程启动后的第一次结构热更成功，随后调用 Rollback
- **THEN** 回滚 MUST 恢复到启动代配置并再次可用，不得报「无上一代快照」

### Requirement: 数值与结构共享完整有效配置回滚记录

每次语义有变化且成功的应用 SHALL 更新协调器当前／上一份完整有效配置，numeric-only 不能只更新运行对象而保留过期回滚配置。Rollback SHALL 从上一份完整配置走同一候选事务，同时恢复结构和热参数并发布新 generation；失败两轴均不部分生效，不保存旧执行器作为恢复依据。

回滚在途场景的验收 MUST NOT 被发布抬起的告警轮劫持或替达标：在途窗口 SHALL 以被拦截调用的直接观测（park 状态）钉住，谓词与断言 SHALL 锚定完成事件增量而非历史存在性；MUST NOT 依赖告警轮排空计数阈值（告警轮可多枚且可合批，阈值不可靠）。

#### Scenario: 数值更新后的完整回滚

- **WHEN** G1 的完整配置先经历一次 numeric-only 成功更新，随后执行 Rollback
- **THEN** 原结构与上一份数值配置一起恢复并产生新发布序号；真实子调用及任务默认值与回滚记录一致，进行中结构绑定不被迁移

#### Scenario: 回滚在途验收不被告警轮劫持

- **WHEN** 结构热加（或任何会发告警轮的发布）后立刻注入用于构造在途窗口的输入
- **THEN** 该用例 MUST 以 park 观测确认在途窗口真实存在（被拦截的调用在回滚时刻确属进行中，与轮归属无关），断言所等的完成事件 MUST 只能由在途调用或其后续输入产生

### Requirement: 重入的执行视图与目标解析同源

存储任务的重入（`relaunch_task` / `resume_task`）MUST 在**解析所选的那张执行视图**上运行：解析出目标 wrapper 后，重入 MUST arms 该 wrapper 所声明的子代绑定（与一次正常 `Call` 同源），MUST NOT 仅携带属主面的租约直连子 agent 运行。

判据：一次重入实际使用的提示词/模型/工具配置 MUST 等于解析那一代所声明的子代配置。由 `ResolveReentryDelegation` 的两条分支各自保证——有发起者时按发起代解析并 arms 发起代声明的子绑定（重入留在自己那一代）；无发起者时按生效面解析并 arms 生效面声明的子绑定（重入到达新发布面）。

声明代不可用（该代已收敛关闭）时，重入 MUST 在产生任何模型调用之前具名拒绝，并释放已取的属主租约，不留悬挂引用。

#### Scenario: 无发起者的重入落到新发布面

- **WHEN** 子 agent 任务被派生后，新一代修改了该目标（仍被路由），随后在无在途发起调用的上下文里 relaunch
- **THEN** 该次重入 MUST 由新面的子 agent 服务（产物携带新代标识），MUST NOT 回落到出生代配置
- **AND** 断言的满足 MUST 只能由重入本身产生：不存在与重入无关的告警轮替它达标

#### Scenario: 有发起者的重入留在发起代

- **WHEN** 一次 relaunch 骑在仍持租约的在途发起调用上，而新一代已停止路由该目标
- **THEN** 重入 MUST 按发起代解析并 arms 发起代声明的子绑定，MUST NOT 改投当前生效代

#### Scenario: 目标声明代已关闭时具名拒绝

- **WHEN** 重入解析到的 wrapper 其声明代已收敛关闭
- **THEN** 重入 MUST 在启动任何模型调用之前返回具名错误，且属主租约 MUST 被释放（义务计数可归零，退役排空不挂死）

### Requirement: 配置热更维度矩阵

每个子 agent 配置维度 SHALL 归属且仅归属一条热通道：**FP 代际面**（视图变更，下回合生效、在途钉定）、**SRC 源拉取面**（参数变更，下一次消费即生效）或 **FILE 懒读面**（文件即真源、消费边界按 mtime 懒读，如 prompt 正文与 mcp_servers）；或明示的 **RESTART 拒**（目录/端点类不可迁移资源，拒绝即正确行为）。维度归属与消费点 SHALL 以矩阵形式登记，每行 SHALL 携带代码行号或测试名证据；**携带而消费点未读＝假热更**，SHALL 视为缺陷而非"部分支持"。（三通道划分系域 01 执行期情报 C1 回写：初稿"FP/源"二分与既有 FILE 懒读通道冲突，不得为凑二分法拆毁既有机制）

#### Scenario: 已登记维度的消费点有契约测

- **WHEN** 矩阵中任一维度标记为 SRC 或 FILE 拉取/懒读通道
- **THEN** 该维度 SHALL 存在断言"消费点读到解析值/重读值"的契约测试，测试名登记于矩阵行

#### Scenario: 新维度接入有唯一路径

- **WHEN** 后续变更新增任一配置维度
- **THEN** 该维度 SHALL 经"FP 子集入列"、"源加字段+消费点读"或"文件懒读"三者之一接入，SHALL NOT 新增第四种机制或 push/订阅/广播通道，且矩阵 SHALL 增行登记

### Requirement: 提交点记录即消费值

org 热更在提交点把某 agent 的热参记为 `applied` 时，该 agent 的消费侧（压缩预算/keepRecent 等热参读路径）SHALL 解析到同一值；SHALL NOT 存在第二份可被独立写入而未随提交点刷新的热参权威。构造期值 SHALL 仅作为「源缺席」时的兜底，一旦源提供值，读路径 SHALL 以源为准。同一进程内重复执行（含多轮换代与多次 numeric-only）SHALL 保持该不变量。

#### Scenario: applied 即可被消费读到

- **WHEN** 一次 numeric-only 发布把某 agent 的 keepRecent 从 2 改为 7 并记为 applied
- **THEN** 该 agent 的 `OrgKeepRecent()` 与压缩动作解析到的值均为 7，无一侧仍为 2

#### Scenario: 重复执行下不变量仍成立

- **WHEN** 同一进程内该场景连续执行 12 轮（每轮含一次结构发布与一次 numeric-only）
- **THEN** 每一轮的 applied 值与消费读值都一致，无轮次退化到构造值

