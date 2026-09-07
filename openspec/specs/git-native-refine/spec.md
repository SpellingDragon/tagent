# git-native-refine Specification

## Purpose

git 原生自我改进通道:refine register/status/rollback 三操作、受控路径约束、[self-improve] 标记 commit、improvement/evaluation 事件台账、评估窗口锚(register commit 时刻)、建议式回滚信号——文件即真源,git 为版本层,框架永不动手。
## Requirements

### Requirement: refine register 登记自我改进
系统 SHALL 提供 `refine` 工具 `register` 操作:接收产物路径列表与 note,对受控路径内文件执行 `git add` + 结构化 `git commit`(message 带 `[self-improve]` 标记),成功后发 governance improvement 事件(含 sha/paths/note)并开启以 commit 时刻为锚点的后验评估窗口。

#### Scenario: 登记受控路径产物
- **WHEN** agent 在落盘 `resources/prompts/SOUL.md` 修改后调用 `refine register` (paths=["resources/prompts/SOUL.md"], note="痛点→产物→预期收益")
- **THEN** git 仓产生带 `[self-improve]` 标记的 commit(仅含该文件,不携带工作区其他改动),governance 事件落库,评估窗口以 commit 时刻开启,工具返回 sha 与「窗口已开,劣化将有回滚建议」提示

#### Scenario: 越界路径拒绝
- **WHEN** register 的 paths 含受控路径(`evolution.protected_paths`,默认 `resources/prompts/**` 与 `skills/**`)之外的文件
- **THEN** 拒绝登记并以 result 渗透受控路径清单与越界文件列表(不执行任何 git 操作)

#### Scenario: 非 git 仓降级
- **WHEN** 运行目录不是 git 仓(或 git 命令失败)时调用 register
- **THEN** 返回明确错误「改进登记需 git 仓」;文件改动本身仍已生效(mtime 热重载),行为如实呈现不阻塞

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
