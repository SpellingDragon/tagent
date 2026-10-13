# 06-learning — RL 基础数据与 SFT 消费（二级子变更 proposal）

> 边界：本目录 md 为本域**规划产物**（`planned` / `design-blocked`）。本轮**无产品实现授权**：不写产品代码/测试、不跑 build/test、不安装依赖、不接训练引擎、不提交、不归档。
> 语义来源：只读计划 `运行时_重设_实施计划_304e54c3.md` §2（RL 范围）、§4.5（Query 与 RL）、§5、§7（06 行）。CLI 模板取自 `openspec instructions {proposal,design,specs,tasks} --change runtime-rebuild --json`。

## Why

用户已选 **数据闭环＋SFT 消费**：运行时须自然采集、因果关联、授权导出、清洗，并让 **SFT 真实消费**（真实 tokenizer/chat template、批次构造、labels/动作 mask），以及反馈与模型替换闭环。当前无任何可运行采集/消费基线，须先立清晰边界与版本策略。

## What Changes

- 接通**版本化记录**与 episode/decision/attempt 的**实际生产点**；旧格式**只读导入**，未知谱系**显式**；新格式**明确版本**，不被旧 v2 结构绑架；**旧 JSONL 路径/字段只界定兼容范围**，**新记录 schema 由 00.3 审定**、**最小真实生产字段**不丢。
- 请求边界**安全快照**、原生日志/logprobs 保留、统一终局观察；不重跑 provider options 造快照；Headers/URL secrets **不落盘**，Native logprobs **确实返回才 present**、`unsupported` 需支持声明、`unknown≠not_returned`。
- 有界队列/字节预算/**丢失账**/发送与关闭准入/**writer 封账**；**采集与 model 共享终局**、发送与关闭同一 protocol、**no seal 记 partial**。
- 授权导出、延迟反馈、**版本化 reward**、逐样本来源 manifest；严格转换、动作/观察 mask、**episode≠session**、基于**因果依赖**的分组防泄漏、**feedback/reward 分开**。
- **真实 SFT 消费者**读取并 tokenize/collate，检查 labels/mask/非空样本与拒绝账（**不指定固定 tokenizer/制品**，真实 consumer/制品 06.6 前具名核准）；**训练用途资格矩阵（DPO / 采样 RL）**（每行：用途/所需证据/结论/缺失原因，不由 SFT 资格外推）；RL HTTP 鉴权/allowlist/逐跳重定向/大小限制/反馈归属。
- 模型替换**租约**贯穿实际请求与流结束，重建失败不发布错误绑定。

## Capabilities

### New Capabilities

（无。不新增"训练引擎"类能力；不默认 AReaL 新依赖。）

### Modified Capabilities

- `decision-capture`：安全快照在请求前、Headers/URL 不落盘、logprobs 三态、no seal partial。
- `trajectory-recording`：版本化记录 + episode/decision/attempt 生产点；旧 v2 只读导入（**旧 AReaL 兼容仅 v2 只读导入说明，不作本次默认新依赖**），新格式明确版本；**中性 SFT 消费合同**（不指定固定 tokenizer/制品、不默认 AReaL 或 Qwen）；**源 AReaL `PPOTrainer` RL prompt dataset（`--mode rl`）路径不在本次范围**（SFT-only，不新增真实 on-policy 消费者）。
- `offline-training-export`：授权导出 + 版本化 reward + **训练用途资格矩阵（DPO / 采样 RL）** + 真实 SFT 消费（**真实 tokenizer/collator**）。
- `trajectory-verification`：真实消费验收的可见失败（非空/labels/mask/拒绝账）。
- `rl-feedback`：RL HTTP 鉴权/allowlist/逐跳重定向/大小限制/反馈归属。
- `feedback-binding`：**feedback/reward 分开**、版本化与模型替换闭环。

## Impact

- 未来候选写域（未存在亦未获批）：`rl/` 采集/导出/消费、`scripts/`（**既有转换器 v2 只读导入**）与 `tests/integration/learning/**`。
- 上游依赖：01-state-binding（01.5 发布/资源）、02-execution（02.4 模型终局、02.5 装配）、03-history（03.3 持久提交）。
- 非目标：**不接 PPO/GRPO 训练引擎**；**不默认 AReaL 新依赖**（现有依赖以 `go.mod` 为准）；不伪造训练资格；不删除失败 run。

## 边界与依赖

- **上游签名/实际实现**：模型/请求/响应类型复用锁定 tRPC（`v1.11.2-tagent.1`/provider `v1.10.0`）；采集在 model 包装层，**与 model 共享终局**（同一 protocol 的发送准入/关闭排空）。签名**未经探针不称冻结**。
- **下游消费者**：SFT 消费者（本域，真实 tokenizer/collate）；反馈闭环（版本化 reward）；Query/诊断（05 域）。
- **禁止事项**：不把 capture/export 当恢复真源；不改 TTL/墓碑、不开第二生产 writer；不把 trace 存在与否当业务身份；不把任务终态自动映射为奖励；不静默降级为纯文本；**不新增真实 on-policy 消费者**（PPO/GRPO 与 AReaL `PPOTrainer` RL prompt dataset 兼容不在本次范围），不沿 source 旧类名（如旧 `Runner`/“隔离 session”类型）硬化为条款。

## 待核（不设假默认）

- **SFT 真实资源**（tokenizer/chat template/collator、制品、模型入口）**待授权**；接口面在 `design.md` 明确，未授权不得把真实消费任务标为完成（06.6 `design-blocked`）。
- 新格式版本号与向后读取/转换路由在**设计闸（00.3）审定**后由 06.1 落实生产点。
- Native logprobs 的**支持声明**来源（provider 能力探测）待核。
- 资源欠账见 `design.md`「资源与待核」。
