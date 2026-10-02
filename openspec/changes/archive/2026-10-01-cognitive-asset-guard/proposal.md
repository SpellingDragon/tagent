# 认知资产防线：漂移审计（终态）· 权限域分离（终态）· 过渡脚手架

## Why

09-30 生产实证（wechat-bot）：远端 agent 用 exec（python 直写）修改了服务器上的 `resources/prompts/action_tool_desc.md`，将**归因错误的走私方法论**（「长作业必须 tmux 套壳；bare nohup 会被清扫」——伪因果，框架 reaper 从未杀过 nohup 孤儿）以「hard-won, do not relearn」铁律语气写入工具描述真源，热重载下回合即生效、每代 agent 加载。

结构性根因（「认知资产裸奔」）：写入通道结构性永开（exec 核心工具 + 同用户文件权限），保护全部选配（evolution/governance/冥想默认关，漂移审计无设计）。自治 agent 的错误经验一旦写入认知资产即自我强化且系统内无纠错力量（反思只整合不纠错；多源证据只在宿主侧）。同日还实证走私入舱不可见与配置漂移不可见（maxTokens 512K→288K 无人知晓）。

按本项目四判据（J1 职责层修复 / J2 不变量优先 / J4 消纳优先），本变更明确区分**终态**（不变量与权限模型，不可绕过）与**过渡脚手架**（正则规则与提示，可绕过、预埋拆除条件）——拒绝把脚手架当作终态交付。

## What Changes

### 终态层（不变量，本变更交付且长期存在）

- **漂移审计（J2：不变量观测）**：启动时对认知资产文件集做 SHA-256 快照（跨重启持久化），周期比对；变更必产 `cognitive_asset_changed` 事件入事实链（file、old/new hash8、size、mtime）+ Info 日志。「内容变则 hash 变」不可绕过——任何写入方（含绕过审批的变量拼接/base64/外力）必然被捕获。默认开启，不依赖 governance，不网络上报。

### 终态层（权限模型，方向确立、部署迁移另行推进）

- **权限域分离（J2 终局形态，本变更仅确立方向）**：exec 执行域与框架资产域物理隔离——资产目录对 exec 会话只读（挂载选项/部署隔离），使「写入认知资产」从「识别并拦截」问题变为「物理不可能」问题。属部署形态迁移，作为独立运维变更推进；本变更在文档中确立其为审批规则的终态。

### 过渡脚手架（通往终态，预埋拆除条件）

- **资产写审批**：governance `DefaultRules` 新增 `exec.cognitive-asset-write` → critical（命令文本匹配资产路径 + 写动词上下文），复用 ApprovalManager 异步审批。**脚手架性质**：文本匹配可被变量拼接/编码绕过；拆除条件 = 权限域分离落地（物理不可写后规则失去对象）。
- **走私入舱引导**：exec 命令含 `nohup … &`/嵌套 `tmux new-session` 时结果尾附提示行（指向 `mode:resident` + 大 `ttl`）。**脚手架性质**：走私的存在理由是失败极性缺失导致的归因真空 + 协议可发现性不足——failure-polarity-passthrough 落地后动机自然消退（J4 消纳）；拆除条件 = fp 变更上线一个观察期后走私行为未再现。
- **远端方法论纠偏（交付动作）**：撤销 desc-patch 毒药段 + 带证据链的验证任务单（restart.log 时间戳对齐、HF 代理掐断复测、maxTokens 漂移认领、insurance 尾段日志）——纠错必须带证据，无证据的否定催生下一个迷信。

不改变：文件系统权限模型（本期）、evolution/refine 语义、approval-channels 契约、冥想产物纪律。

## Capabilities

### New Capabilities

- `cognitive-asset-guard`: 认知资产的防线契约——漂移审计不变量（终态）、资产写入审批规则（脚手架，标注拆除条件）、走私形态引导（脚手架，标注拆除条件）、权限域分离方向（终态，非本期实现）。

### Modified Capabilities

（无——governance 风险分类无现存 spec；approval-channels 通道契约不变。）

## Impact

- **代码落点**：漂移审计（新模块 + wiring + event registry 登记）、`agent/governance/classifier.go`（DefaultRules 新规则）、`tool/action/action_tool.go`（走私提示，Call 入口单点）、文档（README/wiki：终态-脚手架结构与权限域分离方向）。
- **行为变更**：governance 开启部署中资产写入从静默放行变挂起待批；governance 关闭部署零变化。入舱引导默认开（纯提示零拦截）。漂移审计默认开（事件+日志，无打断）。
- **拆除账本**：审批规则（拆除条件：权限域分离落地）、走私提示（拆除条件：fp 上线+观察期无走私复发）。两则在 tasks 与 wiki 中登记，条件触发即进入拆除队列。
- **依赖**：无新外部依赖；走私提示的拆除依赖 failure-polarity-passthrough 上线。
