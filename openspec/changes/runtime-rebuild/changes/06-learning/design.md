# 06-learning — 设计（design.md）

> 二级子变更技术设计。只写"怎么做"的规划；本轮不写产品代码，签名未经探针不称冻结。上游只读计划 §2/§4.5/§5/§7（06 行）。

## Context

- 现场：无采集/导出/SFT 消费产物；`go.mod` 无 AReaL 直接依赖（**不默认新增**）。既有 `convert_trajectories.py`（旧 v2 转换器）**只读导入**，其存在不等于新格式成立。
- 用户已选范围：**数据闭环＋SFT 消费**；**不接入 PPO/GRPO 训练引擎**。
- 设计闸 G0–G3 为实施共同前置；字段与版本解释在 **00.3 审定**后由本域落实生产点。

## Goals / Non-Goals

**Goals**

- 版本化记录 + episode/decision/attempt 生产点；旧格式只读导入、未知谱系显式。
- 请求边界安全快照；secrets 不落盘；logprobs 三态（present/unsupported/unknown）。
- 有界、可封账、no seal 记 partial；采集与 model 共享终局。
- 授权导出 + 版本化 reward + 逐样本 manifest；episode≠session、因果依赖分组防泄漏、feedback/reward 分开。
- **真实** SFT 消费（tokenizer/chat template/collator，labels/mask/非空/拒绝账，**制品由 06.6 具名核准、不预设固定模型**）；**训练用途资格矩阵（DPO / 采样 RL）**。

**Non-Goals**

- 不接 PPO/GRPO；不默认 AReaL 新依赖；不伪造训练质量/资格；不改 TTL/墓碑；不开第二生产 writer。

## Decisions（含被否方案）

- **D06-1 版本化与生产点前移**：核心字段在**生产点**（02.5 装配/02.4 终局）确定，不到采集任务才临时决定。被否：采集侧临时推断字段。理由：计划 §7（06.1）。
- **D06-2 新格式明确版本，旧 v2 只读导入**：新格式 SHALL 带**显式版本号**与向后读取/转换路由；旧 `convert_trajectories.py` v2 结构**只读导入**（**旧 AReaL 兼容仅 v2 只读导入说明，不作本次默认新依赖/指定制品**），MUST NOT 让新格式被旧 v2 字段绑架；**旧 JSONL 路径/字段只界定兼容范围**，**新 schema 由 00.3 审定**、**最小真实生产字段不丢**；MUST NOT 静默改变 v2 含义。被否：沿用 v2 结构不升版。理由：计划 §7 尾。
- **D06-3 请求边界安全快照 + secrets 不落盘**：快照在**请求开始**固定实际模型/生成参数/prompt-skill-tool 可见集合与版本；`Headers`/URL 中的 secrets MUST NOT 落盘；原生日志/`ExtraFields` SHALL 支持**可配置安全投影**；MUST NOT 重跑 provider options 造快照。被否：调用后再 marshal。理由：计划 §4.5、用户硬约束。
- **D06-4 logprobs 三态**：Native logprobs **确实返回才 present**；provider 声明 `unsupported` SHALL 有支持声明来源；`unknown≠not_returned`（无证据标 unknown，不默认 unsupported）。被否：缺省当未返回。理由：计划 §4.5、用户硬约束。
- **D06-5 采集与 model 共享终局 + no seal partial**：capture 与 model **共享同一终局**；发送准入与关闭排空**同一 protocol**；crash 无 seal MUST 记 `partial`，MUST NOT 宣称完整。被否：capture 独立终局/独立 protocol。理由：计划 §4.5。
- **D06-6 episode≠session 与因果分组**：episode 代表业务过程（子调用/Handoff/共享历史保存数据依赖），**不等于 session**（应用 MAY 选择令某 episode 与某 session 重合，MUST NOT 强制等同、亦 MUST NOT 强制把每个 session 拆成多 episode）；split SHALL 按 episode、共享祖先、候选组及历史依赖分组**防泄漏**。被否：按 session 切分。理由：计划 §4.5、用户硬约束。
- **D06-7 feedback/reward 分开且版本化**：feedback 是反馈事件；reward 是从反馈**派生的版本化记录**；二者 MUST 分开建模，MUST NOT 自动把任务终态映射奖励。被否：feedback 即 reward。理由：计划 §4.5、用户硬约束。
- **D06-8 真实 SFT 消费、资源待授权**：SFT 消费验收 SHALL 用**真实 tokenizer/chat template、批次构造、labels 与动作 mask**；**不以自造返回常量的消费者代替**；**具体 tokenizer/制品由 06.6 具名核准**（不预设固定模型、不默认新依赖）；真实制品/入口**待授权**，未授权保持 `design-blocked`。被否：常量替身结项。理由：计划 §2。
- **D06-9 训练用途资格矩阵（DPO / 采样 RL）**：除 SFT 外，导出 SHALL 为每个训练用途（至少 DPO 与采样 RL）给**可判定资格合同**（用途/所需真实证据/结论 qualified-ineligible-unknown/缺失原因）；**SFT 资格不推出其它用途资格**。DPO 需同输入候选对 + 偏好依据/版本/分组；采样 RL 资格取决于**实际算法与行为策略/采样证据**（**不笼统要求所有算法都有 logprobs**）；无消费者与能力证据标 `unknown`/`ineligible`。**本域不接训练引擎**。理由：计划 §4.5、用户硬约束。

## 字段 / 端口所有权

| 面 | 所有者 | 本域角色 |
|---|---|---|
| 模型请求/响应终局 | model 包装层（02） | 采集包装（共享终局） |
| episode/decision/attempt 生产点 | agent/task（02） | 消费并落版本化记录 |
| 提交/水位 | history（03） | 只读消费 |
| capture writer / 封账 | 本域 `decision-capture` | 持有 |
| 轨迹记录（trajectory） | 本域 `trajectory-recording` | 持有 |
| 导出/转换/SFT 消费 | 本域 `offline-training-export` | 持有 |
| feedback 事件 / reward 派生 | 本域 `feedback-binding` | 持有（分开） |
| RL HTTP 面 | 本域 `rl-feedback` | 持有 |
| 模型替换租约 | 本域（01.5 资源） | 持有/消费 |

## 并发 / 取消 / 生命周期 / 错误 / 配置（本域切片）

- **并发**：capture 有界非阻塞；writer 单点；队列满/超限/关闭/写错**分别计数**；打开文件 ≤16、单 capture 落盘上限默认 512MiB。
- **取消**：RL long-poll 响应取消/shutdown 不泄漏等待者；队列拒绝返回可区分错误。
- **生命周期**：发送准入与关闭排空统一；封账返回 writer 确认后的 manifest；no seal 记 partial。
- **错误**：缺证据标 `unknown`/`unbound`；`unsupported` 需支持声明；转换不支持（模板/多模态）**拒绝给原因**，不静默降级。
- **配置**：`trajectory_dump`/`trajectory_dir` 等；RL token 认证/监听守卫；embedding 无关。

## 风险 / Trade-offs

- [自造常量消费者掩盖真实接线] → D06-8 真实资源门 + 拒绝账断言。
- [secrets 落盘] → D06-3 secrets 不落盘 + 断言扫描。
- [logprobs 三态混淆] → D06-4 三态具名 + 断言。
- [split 泄漏] → D06-6 因果分组 + 断言。
- [旧 v2 绑架新格式] → D06-2 显式版本 + 只读导入。

## 待核（Open Questions）

1. 新格式版本号与迁移/转换路由（00.3 审定）。
2. Native logprobs `unsupported` 支持声明的探测来源。
3. **SFT 真实资源（tokenizer/chat template/collator/制品/入口）是否授权**（06.6 `design-blocked`）。
4. RL 真实端点/allowlist 资源是否存在并获授权（06.7 真实单次门控）。

## 文件清单（未来候选写域，**未存在亦未获批**）

- 产品：`rl/capture.go`、`rl/record.go`、`rl/episode.go`、`rl/sink.go`、`rl/export.go`、`rl/sft.go`、`rl/sft_consumer.go`、`rl/http.go`、`rl/replacement.go`。
- 只读导入：`scripts/train/convert_trajectories.py`（**既有 v2 转换器，只读导入，不改其 v2 含义**）。
- 测试：`tests/integration/learning/{causal_records,capture_fidelity,capture_admission,authorized_export,sft_cleaning,sft_consumer,feedback_http,model_replacement}_test.go`。
- **本轮只写本目录 `changes/06-learning/**` 的 md**。
