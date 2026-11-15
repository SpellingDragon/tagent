# 03 门教义/GLOSSARY/spec 归位 · 孙任务

> W2 并行（依赖 01 键名落地）。verify 三要素随任务行冻结。

- [x] 3.1 `docs/GLOSSARY.md` §一「请求预算」行改写（回放窗 replay_window／预算面 context_window+max_tokens／余量派生自有效输出限／删 window/4 缺省与 divisor 表述）—— 验证：正锚 doc 门绿；消费者锚 GLOSSARY 行与 window_bounds.json 四类一致（replay_window=window、context_window+max_tokens=budget）。
- [x] 3.2 `request-budget-accounting` 主 spec 落地 01 MODIFIED delta（门乙+余量派生+命名归位+删 output_reserve/divisor）；config-hot-reload 热参名 max_tokens→replay_window —— 验证：正锚 `openspec validate --strict` 绿；负锚 旧「window/4 缺省」「门开⟺MaxTokens>0」断言不再出现（grep 零残留，归档除外）；消费者锚 spec 与实现 budgetReserve/budgetWindow 一致。
- [x] 3.3 `check_window_bounds.py` docstring/KNOB 教义随归位核对（D5 保留 replay_window=window 则教义不变，仅核文案与新键一致）—— 验证：正锚 `python scripts/gates/test_check_window_bounds.py` 绿；消费者锚 门 scan_config_keys 扫到新 agentKeys（replay_window/context_window/max_tokens）。