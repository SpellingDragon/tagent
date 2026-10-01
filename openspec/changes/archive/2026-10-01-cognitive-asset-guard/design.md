# 设计：认知资产防线

> **评审尺**（四判据）：J1 职责层修复 / J2 不变量优先 / J3 单层判定 / J4 消纳优先。补丁唯一合法形态 = 通往纯化终态的脚手架 + 预埋拆除条件。本设计的结构本身就是该尺的执行：终态层 / 脚手架层显式分层，拆除账本显式记账。

## Context

desc-patch 考古：远端 agent 遵守 self-improvement-meditation 的「直改 prompt + refine register」引导做了前半步，后半步（登记）因 evolution 默认关闭而不可达——**改的能力是结构性的（exec + 同用户文件权限），保护的能力全部是选配的**。伪因果方法论经热重载写入真源后，系统内无纠错力量：反思（冥想）只整合不纠错，会话内多源证据不存在，唯一持有多源证据（代码真文+全量日志+交叉验证）的是宿主。

相邻实证：走私入舱不可见（nohup/嵌套 tmux——三次长作业失败零通知的起点，其动机根源是失败极性缺失造成的归因真空）、配置漂移不可见（maxTokens 512K→288K 无审计面）。

既有基建（复用，不重造）：ApprovalManager（文件审批+微信 Decide，approval-channels 规格）、DefaultRules 数据驱动规则表、evolution protected_paths 默认清单（`evolution/evolve.go:64`，未导出）、事实链（MemoryStore 不可变事件）、`event/registry.go` EventTypeSpec 注册表（新事件类型的硬契约——未注册走 defaultSpec，行为不可控）。

## Goals / Non-Goals

**Goals:**
- 漂移审计不变量（终态）：任何认知资产/配置变更对宿主与 agent 可见，不可绕过。
- 权限域分离方向确立（终态，文档层）：写入从「识别拦截」问题变「物理不可能」问题。
- 过渡脚手架（审批规则 + 走私提示）：止血 desc-patch 类事件，**显式标注拆除条件**。
- 哲学核验：信号建议式（框架永不动手）、执行权在人、脚手架不伪装成终态。

**Non-Goals:**
- 本期不做文件系统层写保护（权限域分离是独立运维变更，本设计只确立方向与终态地位）。
- 不改 evolution/refine 语义与冥想产物纪律。
- 不做网络上报/远程告警；不做冥想外部裁决位注入（待漂移审计实证）。
- 不治理人类侧通知分级（另行立项）。

## Decisions

**D1 漂移审计——终态，不变量观测（J2）。**
快照器扫描资产文件集（清单与 protected_paths 同源 + 主配置 yaml），SHA-256，持久化 resident meta 目录（跨重启，原子写 tmp+rename 对齐 budget.go 模式）；周期（10min 常量）比对，变更产 `cognitive_asset_changed` 事实链事件（file、old/new hash8、size、mtime）+ Info 日志；启动时与上一代快照比对（捕获停机窗口）。
*为什么是终态*：「内容变则 hash 变」是物理不变量——无绕过路径（含变量拼接、base64、外力直改），且成本恒定（文件数十级，hash 忽略）。事件经既有消费面（投影/召回）自然可达，不注入消息路由（可见但不打断）。
*新事件类型硬契约*：`TypeCognitiveAssetChanged` 必须在 `event/registry.go` 登记 EventTypeSpec——建议属性：Role=user、Special、ToolLineSummary（单行摘要）、TTLDays=30（审计性质）、Recallable、NonProjection=false（**必须进投影**——被看见是审计的最低目标）；实现时对照最相似既有类型微调。

**D2 资产写审批——脚手架，拆除条件记账（J1 的过渡形态）。**
DefaultRules 新增 `exec.cognitive-asset-write` → critical：命令文本含清单路径 **且** 含写动词上下文（`'w'`、`>`、`tee`、`sed -i`、`mv/cp` 目标）——两条件与（只读不误伤）。复用 gate.go 既有 Request 链（预期零 gate 改动）；refine 登记不豁免（审批权在人）。
*为什么只是脚手架*：文本匹配可被变量拼路径/编码绕过——规则在追赶症状。**拆除条件：权限域分离落地**（资产目录对 exec 物理只读后，规则失去对象，删除）。拆除账本在 tasks 与 wiki 登记。

**D3 权限域分离——终态方向（本变更仅文档确立）。**
exec 执行域与框架资产域物理隔离：systemd 单元 `ReadOnlyPaths`/`ProtectSystem` + `ReadWritePaths` 白名单（examples/wechat-bot/deploy 已有此模式的先例）、或 exec 会话的 mount namespace 隔离。属部署形态迁移，作为独立运维变更推进；落地后 D2 拆除、D1 保留（审计对「合法变更」仍要可见）。
*J2 论证*：物理不可写 > 识别写意图——不变量守恒，规则追赶。

**D4 走私入舱引导——脚手架，依赖消纳（J4）。**
Call 入口单点检测命令文本（后台化 `nohup … &`/`& disown`；嵌套 `tmux new-session`/`tmux new -s`），命中时结果尾附提示行（托管缺失说明 + resident/ttl 指引 + 可忽略声明），零拦截零延迟。
*为什么只是脚手架*：走私的存在理由 = 失败极性缺失造成的归因真空 + 协议可发现性不足。failure-polarity-passthrough 落地（退出码可见、job 无假就绪）后，走私动机自然消退——本提示是教育期的拐杖。**拆除条件：fp 上线 + 一个观察期走私未复发**。若 fp 落地前走私提示先行且命中频繁，说明消纳假设有误，回本设计重评。

**D5 远端纠偏（交付动作，P7 原则：纠错必须带证据）。**
撤销 desc-patch 毒药段（删「Long-running jobs MUST run inside a tmux session」与「nohup gets reaped」；保留「framework owns async state management」「alive_detached is an OBSERVATION」）+ 注入**带证据链**的正确方法论（10MiB 断点 + 速率换算 + audit 零痕迹 → 死亡是代理掐流非清扫）+ 验证任务单（restart.log 对齐、HF 代理复测、maxTokens 认领、insurance 尾段日志）+ 部署顺序 + HF token 轮换。

## 拆除账本（脚手架生命周期）

| 脚手架 | 拆除条件 | 触发动作 |
|---|---|---|
| D2 资产写审批规则 | 权限域分离（D3）落地验证 | 删除规则 + gate 相关测试降级为「规则不存在」断言 + wiki 更新 |
| D4 走私提示 | failure-polarity-passthrough 上线 + 观察期（建议两周）走私形态未再现 | 删除检测器与提示 + wiki 更新 |

账本随 tasks 交付登记；条件触发时进入拆除队列（新变更或并入相邻变更），**不允许条件已满足而脚手架滞留**。

## Risks / Trade-offs

- [审批规则误伤合法自改进（governance 开启部署）] → 审批异步挂起不阻塞其他工作；strict/warn 部署方自选——这是「执行权在人」的代价与目的。
- [漂移审计的周期窗口（10min 内改了又改回不可见）] → 接受：审计目标是**驻留**的有害固化（desc-patch 类），非瞬时实验。
- [走私提示误报（正当后台化被提示）] → 提示行自述可忽略；教育期价值 > 噪声成本；fp 落地后按账本拆除。
- [权限域分离迁移阻力（部署形态变更）] → 方向已在文档确立，节奏由运维变更独立决策；过渡期 D2+D1 兜底。
- [远端 governance 仍默认关] → D1 漂移审计默认开保证最低可见性底线；交付邮件含部署顺序建议。

## Migration Plan

1. 落地顺序：D1 漂移审计（独立、零行为变化）→ D4 走私提示（独立）→ D2 审批规则（依赖 governance 装配验证）→ D5 远端纠偏交付。可分 PR：审计+提示 / 审批规则。
2. 回滚：D1/D4 附加行为 revert 无残留；D2 revert 恢复 medium 放行。
3. 远端部署顺序：先审计+提示（零行为变化）→ 观察一周 `cognitive_asset_changed` 事件流 → 再议 governance 开启。
4. 终态推进：D3 权限域分离作为独立运维变更立项（引用本设计）；落地后按拆除账本清理 D2/D4。

## Open Questions

- 资产路径清单同源消费的形态：evolution 导出 `DefaultProtectedPaths`（先确认 governance↔evolution 依赖方向无环）> 提取到共同基础包 > 两处声明+互指注释（登记技术债）。
- `cognitive_asset_changed` 是否升级为冥想 digest attention 项——先经事实链自然消费，实证不可见再升级。
