# 配置面归位（config-surface-normalization）

## Why

远端微信助手 c75 会话拒发：`estimated 96407 input tokens over the declared input budget 96000 (provider window 128000, output reserve 32000)`。用户追问：「输出 reserve 这个机制非常奇怪，这里没有配置为什么有这样的控制？」

查证落到三条名实不符（证据见 design §三）：

1. **输出余量凭空扣**：`tagent.yaml` 只声明 `max_tokens: 128000`，未声明 `output_reserve`。运行时按 `window/4` 缺省扣 32000，输入预算被压到 96000。这个 `4`（`DefaultOutputReserveDivisor`）无领域来源——它不对应任何声明量，也从不发给 provider。
2. **余量与真实输出限脱钩**：live spec（`request-budget-accounting`）已规定「输出余量＝请求层 `GenerationConfig.MaxTokens` 或声明值」，但实现 `budgetReserve` 只读 `OutputReserve || window/4`，从不读请求输出限；而生产请求的 `GenerationConfig.MaxTokens` 恒为 nil（`decorate` 只填 temperature/thinking/reasoning，不填输出限）。余量对应一个根本不存在的量。
3. **`max_tokens` 名实不符**：它实际是「回放窗」（折叠节奏／投影窗口），不是 API 通用语义的「输出 token 上限」。添加自定义模型时，provider 注册表只声明连接属性（protocol/endpoint/key），物理容量无处声明，只能借回放窗兼任预算窗。

此外 `AgentSpec.Params`（free-form float map）无任何行为消费者，是死字段。

## What

按用户裁定**破坏性归位 + 远端同步**，对**整个 agent 配置面**做语义归位（不写旧键兼容、不留迁移兜底）：

- **预算三量归位**（核心）：`max_tokens`（回放窗）→ `replay_window`；`provider_context_window` → `context_window`（物理容量／预算门窗口）；收回 `max_tokens` 名给**真实输出上限**（经 `decorate` 发给 provider）；输入预算 = `context_window − 有效输出限`，**派生而非第三旋钮**；删 `output_reserve` 独立字段、删 `DefaultOutputReserveDivisor` 与 `OutputReserveOf`。未声明输出限 = 不预留（reserve=0），不再凭空扣 window/4。
- **预算门唯一来源（用户裁定乙）**：预算门开 ⟺ `context_window>0`；`replay_window` 只驱动折叠，不再兼任预算窗缺省（删「derived window」耦合）。**远端 yaml 必须显式声明 `context_window`**，否则长会话失去预算保护。
- **死字段清除**：删 `AgentSpec.Params` 及其 decode/clone/fingerprint/reload-note 伴生引用。
- **门与注册表归位**：`golden/runtime/window_bounds.json`四类分类法、`check_window_bounds.py`、`docs/GLOSSARY.md` §一「请求预算」、`request-budget-accounting` spec 同步改写一致（spec 冻结的「MaxTokens 既有语义 MUST NOT 改变」由本变更显式 MODIFIED）。
- **远端同步**：`examples/wechat-bot/tagent.yaml`、`tagent.rl.yaml` 按新键改写，显式声明 `context_window` 与（如需）`max_tokens` 输出限。

本期**不**新增 model 级物理容量注册表（判据5的完整形态留待未来）；物理容量在 agent 级 `context_window` 声明。

## Capabilities

- `request-budget-accounting`（MODIFIED）：窗口三分语义归位，输出余量改为派生自真实输出限，删凭空缺省除数。
- `config-hot-reload`（MODIFIED）：规范指纹字段名随归位变更（rename 触发 republish，属预期）。
- 配置声明面（`api/config` + `assembly/host_document`）：键词汇归位 + 死字段清除。

## 边界与依赖

- **依赖**：`runtime/agent` 预算计量（`WithBudget`/`inputBudget`/九面）、`assembly/host_model` 的 `EffectiveGenerationConfig`/`decorate` 接缝（已存在，本期接上输出限）、`memory/replay` 折叠（`FoldWindow.MaxTokens` 随 rename）。
- **被依赖方**：远端 wechat-bot 部署（`tagent.yaml` 是唯一部署声明）；所有构造 `config.AgentSpec{MaxTokens:...}` 的测试。
- **接口面**：`api/config` 是 leaf（仅依赖 stdlib），归位不改其 leaf 性质；`assembly/host_document` 的 `hostAgent` yaml 标签是远端 yaml 的实际解析层，须与 `api/config/decode.go` 的 `agentKeys` 双层同步。
- **禁止事项**：
  - 不触碰外来未跟踪文件（`runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py`、`runtime_budgets.py`、`test_check_runtime_symptoms.py`、`skills/interview/agent-interview-helper/`）。
  - 不改写冻结归档（`openspec/changes/archive/**` 只附勘误）。
  - 不触 §五冻结契约拼写（reincarnation 族；预算字段不在其中，已核）。
  - 尊重「回放窗与预算窗刻意解耦」的真实设计意图（smoke 入口 = 2×回放窗，r32 的 1519/1500）——归位保留两量分立，只改名与派生关系，不合并。
  - 本文档阶段**只规划不实施**；实施待用户审过 design 后另行编排。