# 远端 trajectory 恢复验证 / 估值器实测 (resident-remaining-hardening 1.6 · 支撑 3.8)

> 数据来源：**远端生产 trajectory**（wechat-bot 部署经 `rl.TrajectoryRecorder` 录制）。
> 本文只记**聚合统计**，不含任何用户消息正文/预览/身份；API 凭据绝不落此。
> 复现命令：`python3 rl/trajectory_analyze.py examples/wechat-bot/data/trajectories/wechat-session.jsonl [--json]`
> （该 `.jsonl` 为 gitignore 的本机大数据，522 MB / 30,704 次真实 LLM 调用，随仓库不提交）。

## 1. 语料画像（真实远端分布）

| 指标 | 值 |
|------|----|
| 记录数 | 30,704（batch 0..163，多会话批式） |
| 携带 `response.usage.prompt_tokens` 的比例 | 99%（30,373） |
| prompt_tokens 分布 | min 59 · p50 204 · p90 13.5k · p95 20.3k · max 176,696 |
| 结构 | **双峰**：大量短 tool-loop 调用（p50≈200 tok）+ 长上下文尾（p90>13k） |
| 真实 chars/token（content/tool_calls 序列化 ÷ prompt_tokens） | p50 ≈ **2.04**，属中英混排+代码/JSON |
| 端点 | `open.bigmodel.cn`（智谱，OpenAI 兼容）；本 change 3.8 另用 DeepSeek `deepseek-flash` 探针核对 |

## 2. 估值器实测结论（1.6 的「chars/token 实测报告」）

将**已发布的** `compress.DefaultTokenCounter`（固定 `CharsPerToken=2.0`，逐消息 `runes(content)/2 + 10 + 20·tool_calls`）套到真实调用，逐 turn 与 provider **真实 prompt_tokens** 比对（分析器 `[estimator-bias]` 段，`--json.estimator_bias`）：

| 维度 | est/real p50 | p90 | 低估率(est<real) |
|------|-------------|-----|------------------|
| 全体（n=30,373） | 1.076 | 1.225 | 34% |
| **大上下文 ≥8k tok（n=6,927）** | **0.846** | 1.127 | **83%** |

**核心发现（真实分布，非合成）**：

- 小/短 turn 估值**基本准**（全体 p50≈1.08，轻微高估，安全方向）；
- **大上下文系统性低估 ~11–15%（p50 0.846），且 83% 的大 turn 低估**。低估意味着压缩器以为余量比实际多 → **压缩晚触发 → 长上下文下 provider 溢出(400)风险上升**——恰是上下文最大、最该保安全的区段。此净结论不依赖任何归因，**可确证**。
- **偏差归因（严格据数据，不下过度结论；2026-09-18 cold-eyes 复核修订）**：
  - **排除**：① tool_call 参数——大 turn 里仅占内容字符 ~2%（中位 73 B/call）；② per-message 分帧——`corr(real−est, 消息数) = −0.39`（负相关，帧开销随消息数累积并非主因）。
  - **主因（方向有据、未由本语料完全确证）**：`Estimate` 仅统计 `messages`，对每次请求真实发送的 **tools schema 数组无建模**；本语料 `request` 只含 `{model,messages,generation_config}`（**不含 tools**），而 provider 的 `prompt_tokens` 计入 schema+非内容 token——带工具的大上下文 entry-agent 调用因此被结构性低估。**旁证（实测）**：小 turn（18,329 条 prompt<500）经核 **0% 带 system/tool_calls/tool 结果、中位 1 条消息**=一次性无工具子调用，故不含 schema；其对全体 p50 反而轻微高估（1.08），与「schema 常量只压带工具的大上下文 ratio」自洽。
  - **不可从本语料干净拆分**：`content_runes/real=1.66` 受上述非内容 token 污染，不能单独当作「2.0 系数过乐观」的证据；系数误差与 schema 误差混在一起，缺 provider 分词器无法解耦。
- **结论**：净低估（压缩晚触发）确凿、可据此立项；立项须**先补 estimator 对 tools-schema 每请求常量开销的建模**，再评估是否叠加内容分型系数。

> 口径注：`[compactions]` 回收率是分析器跨 turn prompt_tokens 骤降的启发式，**对会话边界敏感**（跨 session 的首调用低 prompt 会被误记为「回收」），故本文不以回收率作硬证据；`[estimator-bias]` 逐 turn 自洽、会话无关，方为主证。

## 3. 对 1.6「是否另案立项」的建议

**建议：立项**（另案，非本 change 内实施）。候选方向按归因优先级（先解耦主因，再谈系数）：
1. **tools-schema 每请求常量开销入模**（最可能主因，且是归因前置）：估器补上「当前请求 tools 定义序列化后的 token 底价」，然后在本语料复测——若大上下文低估随之收敛，即坐实主因；
2. **保守偏置（最低成本止血）**：大上下文预算线预留 ~15% 余量，先兜住已确证的低估与潜在溢出，不待归因收敛；
3. **内容分型/自适应系数**（CJK/拉丁/代码/JSON 分类 chars/token，或按 endpoint 反馈自校准 `CharsPerToken`）——**须在第 1 项排除 schema 混淆后再评估**，否则会把 schema 误差误记到系数头上。

**本 change 内只做测量结论，不改核心 estimator**（改动会使批次 C 长跑/E2E 证据失效，与 `MAINTAINABILITY-WP3.md` 同一纪律）。

## 4. 诚实边界（不得夸大）

- 语料时间戳 **2026-07**，**早于 D2（批次 A settled 折叠/批量退役，2026-09-18）部署**——故这是**真实远端分布代理**，非 1.6 字面要求的「D2 上线后 24h 窗口」。estimator-bias 与 D2 无直接耦合（D2 改折叠不改 estimator），故此结论对 estimator 立项**仍然有效**；但「D2 上线后回收率是否达标 >30%」仍需部署后专测。
- 单一 `wechat-session` 采集，端点为智谱；DeepSeek `deepseek-flash` 已跑满 3.8 契约矩阵（6/6，见 `EXIT.md` §2.1），但 **estimator-bias 主证仍只用智谱语料**——deepseek 侧未做等价大上下文偏差采集，故本报告偏差结论**不外推**到 deepseek。
- 故 **1.6 不据本报告勾选**：其「post-D2 24h 基线」前置仍未满足；本报告将其从「无任何数据」推进到「真实分布已测、立项判据成立」，余留 post-D2 窗口复采。
