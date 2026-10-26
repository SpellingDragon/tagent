# git-native-refine Specification

## Purpose

git 原生自我改进通道:refine register/status/rollback 三操作、受控路径约束、[self-improve] 标记 commit、improvement/evaluation 事件台账、评估窗口锚(register commit 时刻)、建议式回滚信号——文件即真源,git 为版本层,框架永不动手。
## Requirements

### Requirement: refine register 登记自我改进
系统 SHALL 提供 `refine` 工具 `register` 操作:接收产物路径列表与 note,对受控路径内文件执行 `git add` + 结构化 `git commit`(message 带 `[self-improve]` 标记),成功后发 governance improvement 事件(含 sha/paths/note)。该事件 SHALL 为评估补充 **Git 关联**(sha→真实消费版本),MUST NOT 以 commit/register 时刻**开窗**：评估窗口锚点 SHALL 为改进版本**首次被真实请求消费**的激活证据时刻(见 `evolution-evaluation`「后验评估锚点」),register 只**补**关联,MUST NOT 重贴既有请求/feedback;无法建立关联时 SHALL 标 `unknown`/`insufficient` 并具名缺失,MUST NOT 猜测。

登记的 git 边界 SHALL 为**显式 pathspec**：`register` 只对调用方显式列出的受控路径文件执行 `git add <显式 paths>` 并提交，MUST NOT 使用 `git add -A` / `git add .` / `git commit -a` 等会纳入工作区其他改动或用户**预暂存（staged）**内容的形式；一次登记的 commit MUST 只含本单显式列出的受控文件，用户 index 与工作区其他改动 MUST NOT 被本次登记卷走或改写。

#### Scenario: 登记受控路径产物
- **WHEN** agent 在落盘 `resources/prompts/SOUL.md` 修改后调用 `refine register` (paths=["resources/prompts/SOUL.md"], note="痛点→产物→预期收益")
- **THEN** git 仓产生带 `[self-improve]` 标记的 commit(仅含该文件,不携带工作区其他改动),governance 事件落库并补 **Git 关联**(sha/paths/note),评估窗口锚点为该版本**首次被真实请求消费**的激活证据时刻(**非 commit 时刻**),工具返回 sha 与「窗口已开,劣化将有回滚建议」提示

#### Scenario: register 补关联不重贴既有请求

- **WHEN** 某改进版本已被若干请求真实消费后 register 才提交
- **THEN** register 只补 Git 关联，MUST NOT 把这些既有请求/feedback 重贴到 commit 时刻；窗口仍按**真实消费版本**归因；无法建立关联时标 `unknown`/`insufficient` 并具名，MUST NOT 猜

#### Scenario: 越界路径拒绝
- **WHEN** register 的 paths 含受控路径(`evolution.protected_paths`,默认 `resources/prompts/**` 与 `skills/**`)之外的文件
- **THEN** 拒绝登记并以 result 渗透受控路径清单与越界文件列表(不执行任何 git 操作)

#### Scenario: 非 git 仓降级
- **WHEN** 运行目录不是 git 仓(或 git 命令失败)时调用 register
- **THEN** 返回明确错误「改进登记需 git 仓」;文件改动本身仍已生效(mtime 热重载),行为如实呈现不阻塞

#### Scenario: 不卷用户已暂存内容

- **WHEN** 工作区存在用户已 `git add`（staged）的其他改动，agent 对某个受控文件调用 `register`（显式 pathspec 只含该文件）
- **THEN** 产生的 `[self-improve]` commit 只含该受控文件；用户已暂存的改动仍保持 staged，未被提交、未被取消暂存、未被改写

#### Scenario: 无歧义显式边界

- **WHEN** register 的实现被审查
- **THEN** 其对 `git add` 的调用均以显式文件路径列表为参数，不存在 `-A`/`.`/`commit -a` 等全量形态；越界或空 paths 在触碰 git 前即拒绝

### Requirement: refine status 改进台账
系统 SHALL 提供 `refine status` 操作:经 `git log` 过滤 `[self-improve]` 标记列出改进历史,并 join 各窗口评估结论; SHALL 列出「未登记产物」(受控路径中 mtime 晚于最后一次登记 commit 时间的文件)作为软提醒。

#### Scenario: 查询改进历史
- **WHEN** agent 调用 `refine status`
- **THEN** 返回改进 commit 列表(sha/note/时间)与各窗口的评估结论(健康/劣化/未到期),以及未登记产物提醒(如有)

### Requirement: refine rollback 安全回滚
系统 SHALL 提供 `refine rollback` 操作:校验目标 commit 带 `[self-improve]` 标记后执行 `git revert --no-edit <sha>`;非改进标记 commit MUST 拒绝;revert 冲突时返回冲突详情由 agent 处置,失败以 result 渗透。

#### Scenario: 回滚已登记改进
- **WHEN** agent 对带改进标记的 sha 调用 `refine rollback`
- **THEN** 执行 git revert 生成反向 commit,返回结果;若产生冲突则返回冲突详情供 agent 决定后续

#### Scenario: 拒绝回滚用户提交
- **WHEN** agent 对不带 `[self-improve]` 标记的 commit 调用 rollback
- **THEN** 拒绝执行并说明「仅可回滚改进登记的 commit」,保护用户提交不被误 revert

### Requirement: 治理门控可配置且默认宽松
受控路径写入 MUST 默认无额外审查(改文件即生效);治理闸对受控路径写入的升级审查(如 critical 审批)SHALL 为可配置项,默认关闭。

#### Scenario: 默认路径零摩擦
- **WHEN** evolution 启用、治理闸未配置受控路径规则时,agent 经 file 工具写入受控路径
- **THEN** 写入直接生效(热重载),无审批、无阻断

### Requirement: 溢出全文取回面

输出溢出落盘时 MUST 经 persistBusEvent 登记 overflow_dump 事件(Summary 含路径+大小,Content 含取回指引「可 exec cat 取回」);溢出票据 MUST 自带同一指引——agent 见票据即知道全文位置与取回方式。

#### Scenario: 溢出后 recall 可达
- **WHEN** outputCh 宽限耗尽、事件落盘并登记
- **THEN** recall「溢出」可命中该事件票据,agent 经 exec cat 取回全文

### Requirement: 审批直投通道(宿主装配)

宿主/example MUST 可经 WithApprovalChannel option 注入审批直投通道(Deliver→渠道);直投目标=白名单首个 approver(声明于文档);Deliver MUST 带超时(悬挂不阻塞审批门)。

#### Scenario: example 直投装配
- **WHEN** example 配置 approvers 并以 WithApprovalChannel 装配
- **THEN** critical 审批请求直接送达微信(不经 agent 转述);bot 未就绪时 Deliver 显式报错
