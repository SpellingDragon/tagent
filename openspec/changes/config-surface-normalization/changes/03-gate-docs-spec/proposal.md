# 03 门教义/GLOSSARY/spec 归位

## Why

一级 design §四 D5、§六 D-03：预算字段归位后，门类注册表教义、GLOSSARY §一「请求预算」行、`request-budget-accounting` 主 spec 须同步改写一致。spec 冻结的「MaxTokens 既有语义 MUST NOT 改变」由本变更显式 MODIFIED 取代（乙门语义）。

## What

GLOSSARY §一请求预算行改写（回放窗 replay_window／预算面 context_window+max_tokens／余量派生）；`request-budget-accounting` 主 spec 落地 01 产品 delta（MODIFIED）；`check_window_bounds.py` docstring/KNOB 教义核对（D5 保留 replay_window=window 类则教义不变，仅核）。

## 边界与依赖

- **依赖**：01 键名落地 + 门绿（W1 后，R22 共享面已冻结）。
- **被依赖方**：F1 契约面一致断言（本域改动须恰为 design 所列）。
- **接口面**：GLOSSARY 行与 window_bounds.json 类一致；spec 与实现 budgetReserve/budgetWindow 一致。
- **禁止事项**：不改写冻结归档（archive/** 只附勘误）；不触 §五冻结拼写；历史 orchestration 条款引用的旧门语义（甲）只附勘误注，不改写历史 F1 断言；与 04/05 触碰集互不相交，可并行。