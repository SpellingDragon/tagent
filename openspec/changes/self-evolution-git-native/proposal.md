## Why

tagent 的自我进化存在**载体与哲学的结构性冲突**(2026-09-07 探索会话裁定):

1. **默认路径已经符合哲学**:prompt.Source 是 mtime 热重载——改文件即生效;治理闸默认关且 enforcement 可配置;agent 有 exec → `git revert` 天然可达。维护者的设计原则是:**默认 agent 自迭代、变更默认生效、需要时才回滚、版本管理复用 git 而非自建**。
2. **bundle 体系是唯一偏离**:evolution 启用时 VersionedSource 用不可变快照遮蔽文件——冥想直接改 prompt 文件对 entry 系统提示词**无效**(改动被 bundle 快照吞掉)、对 meditation prompt 又**绕过治理**直接生效,两头都错(BundleStore+ReleaseManager 发布状态机=自建版本管理,与「复用 git」原则冗余)。
3. **冥想×refine 概念割裂**:meditation.md 自称 self-improvement engine,但其 §3.3 引导直接改文件(与 refine 提案通道互不感知);文档把冥想描述为「★卡片沉淀」(旧定位),两者的「引擎×通道」关系零叙述。

维护者裁决(2026-09-07,两个开放问题已答复):
- **大刀阔斧改彻底,不留不符合哲学的冗余设计**(bundle 体系退役,不做双后端开关);
- **git 留痕由 LLM 显式登记**(冥想产物落盘后调 refine 登记工具,框架代 commit——note 质量高、显式可控);
- **回滚全部建议式**(guardrail/judge 劣化判定只渗透信号,执行权永远在 agent/人——「闸不是墙」的极致一致)。

## What Changes

### 一、退役(直接删除,不留开关)

- `evolution/bundle.go`:Bundle/BundleStore/InitBaseline/Active 切换(不可变快照体系);
- `evolution/source.go`:VersionedSource/BundleProvider(bundle 遮蔽层——system prompt 回归文件直读+热重载);
- `evolution/release.go` 的发布状态机:Lane/ReleaseStage/Submit/快慢道/ProtectedPrompts 强制慢道/每日提案预算 Gate;
- `refine` 工具旧操作 propose/diff(提案流整体退役——无提案即无审批道);
- 配置:`evolution.protected_prompts`、bundle 存储路径等发布道字段。

### 二、保留并锚点迁移(评估能力与载体解耦)

- **LLMJudgeEvaluator + MetricGuardrail + StoreEvidenceSource** 完整保留——评估窗口锚点从「bundle 激活时刻」迁移为「登记 commit 时刻」(W4 语义不变:窗口从变更生效点起算);
- **回退语义重构**:劣化判定(硬指标/LLM-judge)**不再触发状态机回滚**,统一渗透为回滚建议消息——执行由 agent 调 rollback 工具或人工完成;
- **feedback 事件的 bundle_id 章**迁移为「改进版本章」(登记 commit sha)——guardrail 沿因果边精确 join 的 8.4 语义保留,锚点换 sha。

### 三、新增(git 原生改进通道)

- **refine 工具重定义**(三操作,git 化):
  - `register`:登记一次自我改进(产物路径+note)→ 框架对受控路径执行 `git add+commit`(结构化 message:来源/痛点/预期收益)→ 发 governance improvement 事件 → 开后验评估窗口;
  - `status`:改进历史与各窗口评估结果(`git log` 过滤改进标记);
  - `rollback`:安全封装 `git revert`(仅限改进标记 commit,防误 revert 用户提交);
- **受控路径**(`evolution.protected_paths`,默认 `resources/prompts/**`+`skills/**`):register 只接受受控路径内产物;治理闸可选对受控路径写入升级审查(默认 off,维护者可配置门控);
- **冥想 prompt 改写**:§3.3 直改文件确认为正确路径;新增产物纪律——「产物落盘后必调 refine register(登记痛点/预期收益),未登记的改进无评估保护」;adoption 核查改用 refine status。

### 四、文档统一叙事

自我改进循环 = **冥想(引擎:反思时机+产物生成)× refine(git 登记通道:留痕/评估/回滚)× consolidation(记忆通道)× 治理闸(可配置门控,默认 off)**。修正 README/wiki 中冥想的「★卡片沉淀」旧定位;冥想×refine 关系在 platform/tool 篇互相引用。

### 五、roadmap 联动

- `tagent-evolution-roadmap` P2 的 D4 replay/shadow 门设计改挂 git 载体(worktree/双版本对照);
- §5A 中与 bundle 发布道相关的裁定项同步修订。

## Capabilities

### New Capabilities

- `git-native-refine`:git 原生自我改进通道——register/status/rollback 三操作、受控路径约束、结构化 commit、改进事件登记、后验评估窗口锚点(commit 时刻)、建议式回滚信号。

### Modified Capabilities

- `self-improvement-meditation`:冥想 prompt 与产物纪律——直改文件为正确路径+显式登记纪律+adoption 核查(refine status);文档定位从「记忆沉淀」修正为「自我改进引擎」。
- `evolution-evaluation`:后验评估锚点迁移(bundle 激活→登记 commit)+建议式信号输出;bundle/发布道能力退役。

## Impact

- **代码**:`evolution/` 包大幅重构(bundle/source/release 状态机删除,保留 judge/guardrail/证据链);`tagent.go` 装配(VersionedSource 移除、git 通道接线);`config.go`(evolution 段重构);`tool/refine` 参数与实现重写;meditation.md 改写。
- **测试**:bundle/release/refine 既有测试大面积删除重写;judge/guardrail 锚点测试改造;新增 git 工具测试(tempdir git repo)。
- **兼容性**:evolution 默认关,生产影响面小;启用者需迁移(bundle 存档只读保留,不做自动迁移——历史 bundle 快照可导出为文件)。
- **关联变更**:design-report-closeout 已交付的 W4/judge/guardrail/feedback join 语义全部保留;D5 发布道条目按本变更重写。
