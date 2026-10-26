# 在线 RL 训练接入：静态决策（骨架级）

> 本文件只定**不随实施波次变化**的决策；实施拆分与派发待 `post-training-data-loop` M2 完成后进行。
> 证据基线：设计文档第 16 节（在线 RL 必备服务协议 / 推荐上线顺序）；交接包 `interfaces.md`（Trainer Adapter 能力协商与导出最低字段）；现有 `rl/record.go`（`LogprobsState` 三态、`AttemptRecord`）、`rl/capture.go`（请求边界快照）、`rl/export_chain.go`（`SFTSample` 纯文本）。

## D1 上线顺序：SFT → DPO → RL，三步放行不可跳

**决策**：本变更的启动门 = `post-training-data-loop` M0-M2 完成 **且** SFT 的输入/掩码/held out 成功率有实测证据 **且** DPO 管线已启用；三者未同时成立不进入 W1。RL 协议联调可以在 W0 做**纯契约**工作（schema、拒收码表、测试），但不得接触真实采样证据。

**依据**：设计第 16 节"推荐上线顺序"——先验证 SFT 的输入、掩码与 held out 成功率，再启用 DPO 数据管线；再用可控推理服务在极小任务集做在线 RL 协议联调。理由是三层共用同一份快照/掩码/归因口径：SFT 掩码错、DPO 前缀不等价都会在 RL 里被放大成"策略梯度学错 token"。

**被否方案**：
- *三线并行*：协议代码可并行，但**验收**不能并行——没有 SFT/DPO 的合格证据，RL 的错误会被归因到算法而非数据。否。
- *先做 RL 再补 SFT*：与设计推荐顺序相反，且 RL 数据量小、噪声大，无法反证 SFT 掩码正确。否。

## D2 能力协商契约：字段、拒收语义与"不静默降级"

**决策**：adapter 在连接时声明一组**扁平、可序列化、可快照**的字段（命名沿用交接包）：

| 字段 | 语义 | 缺失/非法时 |
|---|---|---|
| `algorithm` | 训练算法标识（含版本） | 具名拒收 `missing_algorithm` |
| `policy_version` | 本批训练所用策略权重版本（单调可比较：权重代数或等价序号） | `missing_policy_version` / `uncomparable_policy_version` |
| `tokenizer_digest` | tokenizer 摘要（与记录比对） | `missing_tokenizer_digest` |
| `chat_template_digest` | chat template 摘要 | `missing_chat_template_digest` |
| `required_logprobs` | 算法要求的 logprob 形态（如 `response_token`；空集＝不需要） | `missing_required_logprobs` |
| `max_policy_staleness` | 允许的行为策略版本差（首期整数代数差） | `missing_staleness_window`；`0` 是合法值，**不得**与"未声明"混同 |
| `loss_normalization` | 归一形式标识（root episode / role / token 等） | `missing_loss_normalization` |
| `sample_merge_policy` | 跨调用样本合并策略（独立样本 / 合并序列） | `missing_sample_merge_policy` |
| `schema_versions` | 可接受的记录 schema 版本集合 | `unsupported_schema_version` |

**拒收语义**：每种拒收是 `(code, field, detail)` 三元组，code 为稳定机器码（测试按码断言，不按自由文本）；拒收粒度分**整批**（协商失败、schema 不支持）与**单 Attempt**（摘要/版本/logprob 不一致）。**MUST NOT** 用零值、默认窗口、"最接近 schema"继续。

**被否方案**：
- *宽松协商（缺字段取默认）*：正是"静默退化伪在线数据"的入口。否。
- *只在导出末尾做一次检查*：错误发现太晚，且无法定位到 attempt；协商 + 逐 attempt 双检保留。否。
- *把 capability 塞进 `tagent.training.v1` 记录*：capability 是**消费者声明**，不是事实记录；放记录里会诱导"用记录反推消费者"。否（记录只存**声明快照**供复核）。

## D3 token 账本边界：不得补造，三态沿用，`token_exact` ≠ 算法满足

**事实基线（实测）**：

- `rl/record.go:103-151`：`LogprobsState` 三态 `unknown`(零值)/`present`/`unsupported`，wire 名为显式字符串，未知名拒收；`unsupported` 必须带 `LogprobsSource`。
- `rl/record.go` 的 `AttemptRecord` 只有 `Response model.Response` + `Usage` + `Logprobs` 状态：**没有 token IDs、没有与 token 位置对齐的账本、没有 policy version**；provider 返回时 logprob 仅在响应原文里以 token 文本形式存在。
- 上游 `model.Response.Choices[].Logprobs.Content []TokenLogprob`（`trpc-agent-go@v1.11.2-tagent.1/model/response.go:76-89`）的 `TokenLogprob` 是 `{Token string, Logprob float64, Bytes []int, TopLogprobs []}`——**只有 token 文本与字节，没有 token id**，且不保证每个 provider 都返回。

**决策**：

1. token 账本只接受两类来源：① 受控服务**实际**回传的 prompt/response token IDs；② 与 pinned `tokenizer_digest` + `chat_template_digest` 一致的再分词，且**逐 id 相等**并记录比对结果。两者都不成立 → 账本状态 `unknown` + 该格式具名拒收。
2. **MUST NOT** 用 `TokenLogprob.Token` 文本反查词表、用第三方 tokenizer 反推、或其他模型"对齐"出 token id。
3. 三态语义**原样沿用**：`unknown` 不是"没返回"，也不是"不支持"；算法要求 logprob 而状态非 `present` → 拒收；算法不需要 → 不因此拒收。
4. `token_exact` 只是**必要**条件：仍需算法要求的 logprob 与策略一致性检查（写在 `trainer-adapter` 的独立 requirement 里）。

**被否方案**：
- *用文本重新分词当作账本*：多数 tokenizer 对同一文本可有多种合法切分，且 chat template 版本漂移会改变前缀；这正是设计禁止的"从文字补造"。否。
- *把 `TokenLogprob` 提升为 token id（用 `Bytes` 反查）*：字节可多对一，无法还原 id。否。
- *缺 logprob 时填 0*：等于宣称"该 token 概率为 1"，方向性错误。否。

## D4 归因边界：首期单角色，`joint_credit` 只进团队评估

**决策**：

- 首期固定其他角色：工具执行、子代理、裁判/验收器**不训练**；只训练一个角色（默认主 agent 的决策动作）。
- 归一边界 = **root episode × 训练角色**：长任务跨多次模型调用时，局部 transition 全保留，权重按 root episode 归一再按角色归一；归一形式写入 `loss_normalization` 并由训练后端验证。
- `joint_credit` 只允许出现在团队评估报告，**不得**进入单角色训练目标或优势估计。

**被否方案**：
- *首期多角色联合训练*：信用分配无法与"单角色闭环"解耦，实验不可归因；设计也建议"固定其他角色，跑小规模 RL，稳定后扩角色"。否。
- *按调用次数或 token 数自然加权*：长轨迹自动获得更大权重，正是 SFT 规范要避免的偏差。否。

## D5 风险与回退

| 风险 | 触发条件 | 影响 | 缓解 | 回退 |
|---|---|---|---|---|
| 策略陈旧窗口 | 采样与训练版本差超 `max_policy_staleness` | 重要性权重失真、训练发散 | 首期只接受当前训练批对应的固定策略版本；超窗拒收并记账 | 丢弃该批 rollout，重采；不改写版本字段 |
| 长任务跨多调用归一 | 一个 root episode 跨多请求，合并策略不明 | 样本重复计权或截断 | `sample_merge_policy` 显式声明；不合并时按独立 request-response 导出并记录重分词版本 | 退回独立样本模式（不合并），保留诊断 |
| 受控 serving 成本 | 联调/多 seed 实验的真实 GPU 与调用费用 | 预算超支、实验中断 | W1 前定极小任务集与调用上限；按 seed 记账；先探针后放量 | 缩减 seed 数与任务数，保留已采证据 |
| token 账本不可得 | 服务不返回 token IDs / logprob | 整个 W1 无法产出在线证据 | W1 首个叶子即**能力探针**（tasks 2.1，只验证服务能否回传），失败即升级选型 | 换服务或降级为"只做协议 + 离线校验"，并显式标 `design-blocked` |
| 模板/分词漂移 | tokenizer 或 chat template 摘要变化 | 账本与训练侧不一致 | 摘要随记录落盘；adapter 逐 attempt 比对 | 拒收该批，重新导出 |

## D6 落位与最小接口面（骨架，最终以 M2 对账为准）

- **在线 RL 记录面**：`rl/` 包内新增独立文件（拟 `rl/online_rl.go`），**不修改** `AttemptRecord` 既有字段语义与 `LogprobsState`；账本作为 Attempt 的**附属记录**，以 attempt ID 关联。
- **adapter 包**：拟 `rl/trainer/`（子包），只放 capability 类型、拒收码表、逐 attempt 核对与导出最低字段校验；**不**依赖训练框架 SDK。
- **最小接口面（签名级，供 W0 冻结）**：

```go
// rl/trainer
type Capability struct {
    Algorithm          string
    PolicyVersion      string
    TokenizerDigest    string
    ChatTemplateDigest string
    RequiredLogprobs   []string
    MaxPolicyStaleness int
    LossNormalization  string
    SampleMergePolicy  string
    SchemaVersions     []string
}

type Rejection struct {
    Code   string // 稳定机器码
    Field  string
    Detail string
}

// Validate 返回空切片表示协商通过；任何缺失/非法字段各产出一条 Rejection。
func (c Capability) Validate() []Rejection
```

- **不建平行子系统**：不新建第二套事实源、不复制 `tagent.training.v1` 记录、不在 `rl/` 外另立采集器；扩展点挂在既有请求边界快照与导出 reader 上。
- **最终落位、包名与是否扩 `offline-training-export` reader 面**：M2 定稿后对账，届时允许调整并在本文件登记偏离。

## 验收锚（随 tasks 落地，规划期先定死）

- **正锚**：每条任务点名测试 + 可复跑命令（测试名与 `-run` 正则逐字符一致）。
- **负锚**：每个"不得补造/不得降级/不得超窗"都有具名负向测试（如 `TestTokenLedgerRejectsTextDerivedIDs`、`TestCapabilityRejectsMissingStalenessWindow`），作为勾选硬门。
- **消费者锚**：协议正确不等于消费者成立——W2/W3 必须证明**正式导出入口**（`trainer-adapter` 导出路径）真实调用账本与核对逻辑，不能用测试内手工补线冒充。
- **真实路径**：W1 的 token/logprob/策略版本一一对应证据只能来自**受控推理服务实跑**；本地替身仅限 W0 契约测试，并显式标注。
