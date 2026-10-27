# 设计决策

## D1 严格零差额在 checker 主体（非仅自测）

现状：>基线 error、<基线 warn（退出码 0）。改：≠基线两向 error。理由：自测（test_baseline_matches_live_tree）已要求等值——主体放行下漂是结构漏洞（本案 +27 即经此存活至 lint）。被否：维持 warn+依赖 lint——提交面无 lint 时漂移照穿。

## D2 守卫挂提交面的形态

钩子（版本化、opt-in）按「暂存含 *.md」触发树级检查——非按路径白名单（语料定义归 checker 单点，钩子不复制排除规则=无第二处漂移点）。

## D3 不碰 ~/.git-hooks 与 core.hooksPath

用户全局配置不入案；严格 checker 使 CI/任何本地面（lint/手动/钩子）同语义。

## D4 验收锚

- 正锚：等值树 `check_doc_budget.py` exit 0；`python3 -m unittest discover -s scripts/gates -p test_check_doc_budget.py` 全绿。
- 负锚（新建 TestStrictZeroDelta）：<基线不 tighten → exit 1（错误文案含 --tighten 指引）；>基线 → exit 1（既有）。
- 消费者锚：`bash scripts/lint.sh` exit 0（lint 内含预算检查+自测）；`bash -n scripts/hooks/pre-commit` + 钩子夹具演练（暂存一个 .md → 守卫触发路径可达）。
