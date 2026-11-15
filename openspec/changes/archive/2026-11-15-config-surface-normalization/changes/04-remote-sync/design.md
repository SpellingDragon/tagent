# 04 远端同步 · 设计

> 静态决策见一级 [design.md](../../design.md) §四 D4（乙硬要求）、§六 D-04。

## yaml 现状（✅ 实测）

- `examples/wechat-bot/tagent.yaml:106` `max_tokens: 128000`，无 `output_reserve`/`provider_context_window`。
- `final_round` max_rounds:2400 budget_seconds:33000 timeout_seconds:120；`meditation` enabled interval:30m min_gap:2h（前序已解决 2h 回复间隔）。
- providers 注册表（zhipu/deepseek/moonshot/tencent_hy/tencent/gemini_oc）只 provider/api_endpoint/api_key_env（连接属性，无容量）。
- 子 agent knowledge/recall/action/plan；「待接线」注释列 max_tokens/compress_threshold。

## 归位改写

- 主 agent 与子 agent：`max_tokens:128000`→`replay_window:128000` + **必显式** `context_window:128000`（乙门唯一来源）。
- 输出限 `max_tokens`：按需声明（微信助手当前无显式输出限→reserve=0，整窗口作输入预算；如需限制输出再声明）。
- 注释如实改写：「待接线」清单随归位更新（replay_window/context_window 已接线）。
- `tagent.rl.yaml` 同步（若含预算键）。

## 风险与回退

- **风险**：漏写 context_window→乙下门闭失预算保护（D-04 负锚）；旧键 max_tokens 作回放窗→strict 解码拒启动。
- **回退**：git revert；`loadHostDocument` strict 解码 exit 0 + 预算门开（context_window>0）作验收。
- **部署边界**：本域只改仓库内 yaml 声明；远端部署（git pull + rebuild + restart）须用户明示授权，不在本域自动执行。