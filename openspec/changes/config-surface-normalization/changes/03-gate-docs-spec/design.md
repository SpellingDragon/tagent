# 03 门教义/GLOSSARY/spec 归位 · 设计

> 静态决策见一级 [design.md](../../design.md) §四 D5、§六 D-03。产品契约 delta 见 [01 specs/request-budget-accounting](../01-budget-normalization/specs/request-budget-accounting/spec.md)。

## 文件清单

- `docs/GLOSSARY.md` §一「请求预算」行（line 26）：四类分类法改写——回放窗 `replay_window`（唯一外部窗口）／预算面 `context_window`+`max_tokens`（输出限）／余量派生自有效输出限（删 window/4 缺省表述）／策略面／机制常量（删 DefaultOutputReserveDivisor）。指向 golden/runtime/window_bounds.json。
- `openspec/specs/request-budget-accounting/spec.md`：落地 01 MODIFIED delta（门乙+余量派生+命名归位+删 output_reserve/divisor）。归档时由 openspec archive 同步。
- `scripts/gates/check_window_bounds.py`：docstring/KNOB 教义核（D5 保留 replay_window=window 类，windows==1 不变，仅核文案与新键一致）；`test_check_window_bounds.py` 门自测绿。
- `openspec/specs/config-hot-reload/spec.md`：热参名 max_tokens→replay_window（line 10/23/28/38/45）；FoldWindow.MaxTokens（line 42）保留。

## 风险与回退

- **风险**：GLOSSARY 行与注册表类不一致→F1 核对；spec 旧门语义残留→grep「MaxTokens>0」「window/4」零残留（归档除外）。
- **回退**：git revert；文档门 + openspec validate --strict 绿作验收。