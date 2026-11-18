## ADDED Requirements

### Requirement: 预算契约文档同步

预算字段归位后，`docs/GLOSSARY.md` §一「请求预算」行、`request-budget-accounting` 主 spec 与 `golden/runtime/window_bounds.json` 四类分类法 SHALL 同步一致：回放窗 `replay_window`（唯一 window 类，外部模型窗口）／预算面 `context_window`+`max_tokens`（budget 类）／输出余量派生自有效输出限（无 window/4 凭空缺省）／策略面／机制常量。门类教义（`check_window_bounds.py`）SHALL 与注册表一致。

主 spec 冻结的旧门语义（「预算门开 ⟺ 显式 provider 窗口声明 ∨ AgentSpec.MaxTokens>0」「MaxTokens 既有语义 MUST NOT 改变」）SHALL 由本变更显式 MODIFIED 取代为乙（门开 ⟺ context_window>0）。历史编排条款引用的旧门语义只附勘误注，MUST NOT 改写历史 F1 断言本身。

#### Scenario: GLOSSARY 与注册表一致

- **WHEN** 归位落地后核对 GLOSSARY §一请求预算行与 window_bounds.json
- **THEN** 四类分类 SHALL 逐字一致（replay_window=window、context_window+max_tokens=budget、无 output-reserve/divisor 条目）

#### Scenario: 旧门语义零残留

- **WHEN** grep 主 spec 与 GLOSSARY 的「window/4」「门开⟺MaxTokens>0」旧断言
- **THEN** SHALL 零残留（冻结归档除外）；spec 与实现 budgetReserve/budgetWindow 一致