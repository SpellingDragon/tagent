# 01 state-binding — 二级子变更提案

> 一级变更：`runtime-rebuild`（schema `spec-driven`）。本文件是二级域 `01-state-binding` 的提案。
> 二级嵌套不被 CLI 跟踪是**有意设计**：本域可整体提升为顶层 change 而结构无损；进度由一级 `tasks.md` 的域 checkbox 人工记账。
> 领域 = 责任与验收单位（计划 §6），不是代码包数量，也不是整域派发单位；派发原子下沉到就绪孙任务（计划 §8）。
> 本轮只产出 OpenSpec 规划产物；**不写产品代码/测试、不改残件、不跑产品 build/test、不安装/提交/归档**。

## Why

清理后的工作树没有可运行的 state 现场：声明、生效绑定、会话与 mailbox 归属需要重新冻结。历史失效模式的根因是「执行器自持第二份状态真源」与「声明/活对象混入同一份指纹」——导致在途请求被半途拼接、同内容重载误换代、候选失败部分生效、文件热更被误当作经过前置质量门。本域把「纯声明 → 生效绑定 → 请求借用同代」这条链一次性定清，使后续执行域（02）只借用绑定而不复制状态。

## What Changes

- `config` 只做**纯组织声明**解析（yaml/json）：严格解析、适用默认值、规范化内容指纹，不构造 provider/agent/连接/锁。
- `state` 建立并持有 agent/session 现场：可信身份、投递与读取授权、显式 `(agent, session)` 寻址，不猜默认来源；实例身份区分源/目标实例，退役时未交付义务保留（不以 `removed` 丢弃）。
- mailbox 接纳 / 认领 / 消费完成**分别记账**，同 session 执行占用受管；接纳、取消与结果记账不等待慢模型。
- 组合根准备原生模型/工具/插件**活绑定**（候选私有），state 持有并按代借出；一次请求取得同代模型、工具及有序插件管线，旧请求继续持旧代；**文件内容按来源在请求边界读取固化，MUST NOT 被无条件钉在同发布代**（结构 generation 可不变）。
- 组织发布：候选记基线、提交前复核、同内容不换代、过期候选不覆盖；**发布前置契约**（由 00.3 审定）——**必需检查缺失即拒绝发布**，**不适用/禁用检查显式记录豁免、不作证据通过**；失败按构造逆序释放**候选独有**资源，共享面与**既有在线资源**不回收。发布机制验证（01.5）可注入确定性真实政策替身做机制测试，但**非正式质量结论**（正式质量归 07.4）。
- `prompt`/`skill` 文件内容在请求边界读取并固化实际版本、直接热读；结构变更走前置检查分路；失败保留错误原因链。

## Capabilities

### New Capabilities

本域不新增产品能力。`session 身份与授权`、`mailbox 记账`、`候选活绑定准备与同代借用` 作为既有 `config-state-twinship` 的**新增 Requirement**（ADDED）登记，而非另起同义新能力。

### Modified Capabilities

- `workflow-config-compilation`：新增「配置解析为纯组织声明」；修改「发布指纹与版本代」「唯一原子发布入口」（基线复核 / 过期候选）。
- `config-state-twinship`：修改「生效执行绑定由 state 持有」；修改「config→plan→govern→state 管道」；新增「session 身份与授权」「session mailbox 记账与执行占用」「候选活绑定准备与同代借用」。
- `config-hot-reload`：修改「配置热更维度矩阵」（FILE 懒读面直接生效）；修改「首代可回滚」；新增「结构变更与文件直接生效分路及错误原因链」。
- `per-call-subagent-overrides`：修改「调用时覆盖子 agent 视图」（覆盖视图对 state 借出的同代绑定解析、invocation 作用域、跨代际钉定）。

`context-scoping` 本域**不改**：其 resume 任务链还原由 04/03 的恢复路径消费；本域在 `design.md` 映射表中以**证据映射**（继承源块）登记，不出 delta、不声称已认证。

## Impact

- 实施候选写面（本轮不写）：`config/*.go`、`state/*.go`（含 state 同包的 resources 消费点，如 `state/resources.go`）、`prepare.go`（module 根组合入口）、`tests/integration/config/**`、`tests/integration/state/**`。
- 只读消费：`event.*` 身份/元数据常量（02.1 冻结）、task 提交消费端口签名（02.1 冻结）、框架 `skill`/`tool` 复用点。
- 对外符号 `package tagent` 位于 **module 根**（`/Users/pengweiye/Documents/codes/tagent/tagent`），不 nested 到 `tagent/tagent`；本域不建立根门面。
- 组合根、主 spec、旧 change、外部计划、依赖、`scripts/` 均不在本轮写域。

## 边界与依赖

### 依赖（接口前置 vs 实现前置）

- **接口前置**（签名冻结即可开工，不等待实现）：
  - 00.3 G1 共享合同：身份/state 绑定/插件相位/任务提交/通知/关闭/Query 的字段与版本解释。接口设计消费 00.3 审定的身份与提交**合同（签名级）**；不在本域预设返回 Receipt 的提交签名。
- **实现前置**（需前序实现真实落地）：
  - 01.2/01.3/01.4 的**实施及验收**依赖 02.1 身份与**内存提交载体**的真实集成（非仅签名）；提交结果与受理 Receipt 分离。
  - 01.2 ← 02.1（身份与提交端口实现）。
  - 01.3 ← 01.2、02.1。
  - 01.4 ← 01.1、02.1。
  - 01.5 ← 01.4；01.6 ← 01.5。
- 01.1 仅依赖 00.3（声明解析无执行侧实现前置）。

### 被依赖

- 02.5 从 state 借同代绑定驱动 agent；03.3 复用 state 持久提交与幂等接纳；05.1/05.2 读 state 只读总图与快照 revision。

### 接口面（本域最小冻结面，只登记不实现）

- `Address{AgentID, SessionID}`；`Publish(ctx, target, event) (Receipt, error)`。
- `config.Load`（纯声明解析）→ 组合根 `Compile`（OrgPlan 声明）→ `Prepare`（StatePlan 绑定 + 候选租约）→ 前置检查 → state commit。
- `StatePlan{Plan OrgPlan, Agents map[AgentID]AgentBinding, Leases []LeaseRef}`；`AgentBinding.ToolSets` 的 `Set` 连接 Close 归资源租约管辖。

### 禁止事项

- 不在 `config` 构造 provider/agent/连接/锁；不建第二在线真源或第二编排表示（无 workflow/v1、无图 DSL、无灰度分派）。
- 不引入通用 DI 容器、插件 DAG；不为保留旧接口发明消费者；不按旧类名/包数恢复旧代码。
- 不引入第二份可写会话历史；不把文件热更挂到 register/评估/前置质量门上。
- 不提交/推送/归档；不跑产品 build/test；不触碰 Git index。
