# 在线 RL 训练接入（M3：真实 token 账本与行为策略协议）

## Why

在线 RL 与离线 SFT/DPO 的差别不在算法，而在**每次采样都是训练输入**：策略梯度要求知道"这条 token 是哪个策略版本、在什么采样参数下、以多大概率产生的"。tagent 现有的 `rl/` 采集面只记录 logprobs 的**三态**（`present` / `unknown` / `unsupported`，`rl/record.go`）与响应原文——provider 返回时原文里带的是 **token 文本级** logprob（`TokenLogprob.Token` 是文本，无 token id），**没有服务端实际 token IDs、没有与 token 位置对齐的账本、没有行为策略权重版本**——拿这些记录去训练 RL，只能从文本回推 token 身份，等于制造伪在线数据。

设计文档第 16 节把这条线定在 M3：先验证 SFT 的输入、掩码与 held out 成功率，再启用 DPO，最后才做在线 RL；且必须"用可控推理服务在极小任务集做协议联调，检查 token、logprob 和策略版本一一对应"。用户已裁定：在线 RL 从 `post-training-data-loop`（M0-M2）拆出，独立立档为本变更，作为其**下游**。

本变更当前只立档（骨架级），不实施：全部叶子的细化拆分待 M0-M2 完成、SFT 验证通过后进行。

## What Changes

- **在线 RL 必备服务协议**：每个实际采样 Attempt 记录
  - 行为策略权重版本（`policy_version`）与采样参数（温度/`top_p`/`max_tokens` 等实际生效值）；
  - 服务端**实际** prompt 与 response token IDs，以及 tokenizer 与 chat template **摘要**；
  - 所选算法要求的**逐 token 行为 logprob**。
- **token 账本不可补造**：token IDs 与 logprob 只能来自服务端实际分词/生成结果，或与 pinned tokenizer + chat template 摘要一致的再分词**且逐 id 相等**证明；不得从文字、其他模型或另一 tokenizer 补造。既有 `LogprobsState` 三态语义原样沿用（`unknown` 不得被读作"没有 logprob"）。
- **Trainer Adapter 能力协商**：adapter 显式声明 `algorithm` / `policy_version` / `tokenizer_digest` / `chat_template_digest` / `required_logprobs` / `max_policy_staleness` / `loss_normalization` / `sample_merge_policy` 与可接受 schema 版本；**缺字段立即 named rejection**，不静默降级成伪在线数据。`token_exact` 不等于满足某算法，仍须过该算法的 logprob 与策略一致性检查。
- **固定其他角色的单角色闭环**：首期固定工具、子代理与裁判等其他角色，只训练一个角色；优势与损失权重按 root episode 与训练角色归一，归一形式由训练后端验证；`joint_credit`（多角色联合信用）仅用于团队评估，不进入单角色训练目标。
- **受控推理服务联调**：在极小任务集验证 token、logprob、策略版本与 attempt 一一对应（需用户授权服务与预算）。
- **小规模多 seed RL 与隐藏评测**：单角色、多 seed 报告功能成功、硬约束与成本；隐藏评测集与试采/训练切分隔离。
- **不改**：M0-M2 已立的数据契约、验收器、奖励与 SFT/DPO 导出范围；本变更只新增在线 RL 协议与 adapter，并消费 M0-M2 的产物。

## Capabilities

### New Capabilities

- `online-rl-protocol`：在线 RL 采样记录契约——行为策略版本、服务端实际 token 账本、tokenizer/chat template 摘要、采样参数、逐 token 行为 logprob、三态沿用与"不得补造"、单角色归因与 root episode 归一、策略陈旧窗口处理。
- `trainer-adapter`：训练后端接入面的能力协商与拒收语义——capability 声明字段、逐 attempt 核对、named rejection 码表、在线 RL 导出最低字段、"token 精确 ≠ 算法满足"的独立判定。

### Modified Capabilities

- `offline-training-export`：**条件性登记，本立档不提交 delta**。该 spec 的「训练用途资格矩阵（DPO / 采样 RL）」已把采样 RL 资格挂在实际算法与行为策略消费者上；本变更一旦为它提供真实消费者与 reader 面扩展，需按对账结果补 `MODIFIED` delta（或把 reader 扩展完全落在 `trainer-adapter` 契约内，则不改）。对账时点为 M0-M2 的 M1 导出面定稿后。

## Impact

- 新增（拟）：`trainer adapter` 独立包（命名与落位待 M2 对账，不建平行子系统）、在线 RL token 账本记录与拒收码表、adapter 契约测试与负例。
- 修改（拟）：`rl/`——在 `AttemptRecord`（`rl/record.go`）之外新增在线 RL 账本面（**不改**其既有字段与三态语义）；采集包装在 `rl/capture.go` 已有的请求边界快照上扩展；`rl/export_chain.go` 的 reader 面仅在需要时扩展。
- 依赖：受控推理服务的 token ID / logprob 回传能力（选型待裁）；训练后端（算法待裁）；M0-M2 的 `tagent.training.v1` 记录、验收器与奖励向量。
- 成本：受控 serving 与训练算力的真实花费；联调前须由用户授权服务与预算。
- 不产生：任何生产运行路径的行为变更；任何对真实模型服务的默认调用（联调需显式授权）。

## 启动门（硬前置，不可跳）

1. `post-training-data-loop` 的 **M0-M2 完成**：数据契约、验收器、奖励向量、SFT/DPO 导出与试采质量门均已落地并通过其域内验收。
2. **SFT 先验证**：输入快照、loss 掩码与 held out 成功率有实测证据（设计第 16 节"先验证 SFT 的输入、掩码与 held out 成功率，再启用 DPO"）。
3. **DPO 次之**：DPO 数据管线已启用并能产出合格偏好对（同上顺序）。
4. **受控推理服务获授权**：服务选型、预算与数据边界经用户明示；未授权时 W1 起的叶子保持 `design-blocked`，不得以本地替身冒充在线证据。

## 边界声明（与 `post-training-data-loop` 不重叠）

- 该变更 **M1 只留契约位**（capability 声明字段与 schema 兼容位，不做实现、不冒充 token 资格）；本变更**实现**该协议与 adapter。
- 数据契约（`tagent.training.v1`）、任务规格/验收器、奖励向量、SFT/DPO 导出与试采工厂**归 M0-M2**；本变更只**消费**，不复制、不改写其口径。
- 若在线 RL 需要新字段，走**版本化 annex**（新 schema 版本或新 kind），不改 M0-M2 已承诺的字段语义。
- 该变更的 **M3 里程碑（在线 RL）由本变更交付**；在本变更启动门满足前，M3 MUST NOT 被宣称开始或完成，两边不得各自宣称对方范围。

## 非目标

- 不做多角色联合训练（`joint_credit` 仅团队评估）。
- 不做生产流量在线学习、不做自动策略发布/回滚。
- 不做训练算法本身的实现或调参；算法选型待裁。
- 不用第三方文本 API 的文本输出补造 token 账本。
