# cognitive-asset-guard 能力规格（新增）

> 结构约定：本能力区分**终态需求**（不变量/权限模型，长期存在）与**脚手架需求**（过渡形态，标注拆除条件——条件触发即进入拆除队列）。权限域分离为终态方向，其实现属独立运维变更，本 spec 仅以方向性需求锚定。

## ADDED Requirements

### Requirement: 认知资产漂移审计（终态）

系统 SHALL 在启动时对认知资产文件集做 hash 快照（持久化至 resident meta 目录，跨重启保留），并周期（默认 10 分钟）比对；检测到内容变更时 SHALL 产生 `cognitive_asset_changed` 事件入事实链（携带文件路径、旧/新 hash 摘要、大小、修改时间）并记 Info 日志。启动时 SHALL 与上一代快照比对，捕获停机窗口内的修改。审计 SHALL 默认开启、不依赖 governance 开关、不产生网络上报、不注入消息路由（可见但不打断）。本需求为不变量观测——任何写入方（含绕过其他防线的变量拼接、编码、外力直改）MUST NOT 逃避捕获。

#### Scenario: 运行中被直改

- **WHEN** 运行期任何一方（含 agent、外力）修改了 prompts 下文件内容
- **THEN** 下个比对周期产生 `cognitive_asset_changed` 事件，含旧/新 hash 摘要与路径

#### Scenario: 停机窗口修改被启动比对捕获

- **WHEN** 进程停止期间文件被修改，随后进程重启
- **THEN** 启动比对产生漂移事件（新快照仍以当前态为基线继续运行）

#### Scenario: 绕过审批的写入仍被捕获

- **WHEN** 命令以变量拼接路径等规避文本匹配的方式写入了资产文件
- **THEN** 漂移审计仍 MUST 在下个周期产生事件（不变量不被规则绕过技术绕过）

#### Scenario: 无变更零噪声

- **WHEN** 周期内文件集无变化
- **THEN** 不产生事件，无额外日志

### Requirement: 认知资产写入需人工审批（脚手架——拆除条件：权限域分离落地）

governance 启用时，exec 命令文本匹配认知资产路径写入意图（默认清单与 evolution `protected_paths` 同源：`resources/prompts/**`、`skills/**`，另含主配置 yaml）SHALL 被分类为 **critical**，走既有 ApprovalManager 异步审批：未批挂起（strict 模式拒绝并给出可读理由），人工裁决（文件/微信通道）后放行；审批请求摘要 SHALL 含目标路径与命令片段。refine 流程内的登记修改 MUST NOT 豁免审批。governance 关闭的部署 SHALL 零行为变化。**本需求为过渡脚手架**：文本匹配非密封边界，其终态为权限域分离（物理不可写）；拆除条件触发时本需求随变更删除。

#### Scenario: 直改工具描述被拦待批

- **WHEN** governance 开启（warn 模式），agent 提交 `python3` 脚本改写 `resources/prompts/action_tool_desc.md`
- **THEN** 该命令被分类 critical，产生审批请求（含路径与命令摘要），挂起等待人工裁决

#### Scenario: 人工批准后放行

- **WHEN** 上述审批请求经 Decide 批准
- **THEN** 同命令重试被放行并记录审计（已批准放行）

#### Scenario: 登记不豁免审批

- **WHEN** agent 经 refine register 登记后直改 protected 路径文件
- **THEN** 写入命令仍须过 critical 审批（judge 评估是事后层，审批权在人）

#### Scenario: governance 关闭零变化

- **WHEN** 部署未启用 governance
- **THEN** 同类命令按既有语义执行，无挂起、无审批请求

### Requirement: 走私形态入舱引导（脚手架——拆除条件：failure-polarity-passthrough 上线且观察期走私未复发）

exec 工具 SHALL 对命令文本做静态检测：命中后台化模式（`nohup … &`、`& disown` 等）或嵌套 tmux 会话创建（`tmux new-session`、`tmux new -s`）时，工具结果末尾 SHALL 附加引导行，说明该形态不受框架托管（无结算通知、无 TTL 保护）并指向合法形态（`mode:resident` + 大 `ttl`、退出码由框架捕获）。引导 SHALL 为纯提示——MUST NOT 拦截、延迟或拒绝命令执行。**本需求为过渡脚手架**：走私动机根源为失败极性缺失导致的归因真空，failure-polarity-passthrough 接通极性后动机自然消退；观察期走私形态未再现即触发拆除。

#### Scenario: nohup 后台化被引导

- **WHEN** agent 提交含 `nohup longjob & disown` 的命令
- **THEN** 命令照常执行，结果末尾含一行托管缺失提示与 resident/ttl 引导

#### Scenario: 嵌套 tmux 会话被引导

- **WHEN** agent 提交含 `tmux new-session -d -s name "cmd > log"` 的命令
- **THEN** 命令照常执行，结果末尾含内嵌会话不受托管的引导行

#### Scenario: 正常命令零干扰

- **WHEN** 命令不含任何走私模式
- **THEN** 结果与现状逐字节一致，无附加行

### Requirement: 权限域分离方向（终态锚定，实现属独立运维变更）

exec 执行域与框架资产域的物理隔离（资产目录对 exec 会话只读）SHALL 作为资产写防线的终态方向被文档确立：使「写入认知资产」从「识别并拦截」问题转变为「物理不可能」问题。本需求不要求本变更实现；其落地变更 SHALL 引用本能力并在验证后触发资产写审批脚手架的拆除。

#### Scenario: 方向在文档中可追溯

- **WHEN** 读者查阅平台子系统文档
- **THEN** 权限域分离作为 cognitive-asset-guard 的终态方向与审批规则的拆除条件可见

### Requirement: 防线默认态与拆除账本

入舱引导与漂移审计 SHALL 默认开启（纯附加行为：提示行与事件，无拦截）；资产写审批规则 SHALL 随 DefaultRules 默认存在但仅在 governance enabled 时被评估。三者 MUST NOT 引入新的必填配置项（路径清单与 evolution protected_paths 同源声明）。**拆除账本 SHALL 随变更交付登记**（脚手架项、拆除条件、触发动作），条件触发后脚手架 MUST NOT 滞留。

#### Scenario: 零配置可用

- **WHEN** 部署不做任何新增配置
- **THEN** 引导与审计生效；审批规则就位但因 governance 关闭不被评估

#### Scenario: 拆除条件触发后脚手架进入拆除队列

- **WHEN** 权限域分离落地验证通过（或 fp 上线且观察期走私未复发）
- **THEN** 对应脚手架项进入拆除队列，不允许条件已满足而脚手架滞留
