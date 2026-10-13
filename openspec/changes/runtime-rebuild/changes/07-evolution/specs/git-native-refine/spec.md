# git-native-refine delta（07-evolution）

> 本 delta 修改 `refine register` 的 git 边界与**评估窗口锚点表述**（与 `evolution-evaluation` 对齐为**激活证据时刻**、GitSHA 仅作**关联**）；其余 Requirement（status 改进台账、rollback 安全回滚、治理门控默认宽松、溢出全文取回面、审批直投通道）语义不变，**继承**（见 `../proposal.md` §继承）。
> 复用登记（非规范）：宿主机 `git` CLI 经受控执行面调用；tagent 只做「显式 pathspec → git 原语」的薄胶水，core 无对应符号。

## MODIFIED Requirements

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
