# 任务（单域小案，无波次）

- [x] 1. checker 严格零差额：`check_doc_budget.py` 主体两向 ≠ 皆 error（<基线文案指 --tighten 同提交锁入），退出码契约注释同步 —— 验证：`python3 scripts/gates/check_doc_budget.py`（现树等值 exit 0）+ 负锚 `python3 -m unittest discover -s scripts/gates -p test_check_doc_budget.py -k TestStrictZeroDelta`（新建：下漂不锁→红/等值→绿）—— 补勾（2026-10-27）：等值树 RC=0；TestStrictZeroDelta 3/3 ok（-k 命中）；全套件 OK（含收紧模式接线：--tighten 下下漂降为「锁步进行中」注记、上调仍拒——TightenTest 两测迁移后绿）
- [x] 2. 钩子修复+预算守卫：pre-commit 死路径改 gates/、暂存 *.md 触发树级零差额检查、头注同步 —— 验证：`bash -n scripts/hooks/pre-commit` exit 0 + 演练：临时暂存一 .md 后钩子守卫段可达（输出含 doc-budget 行）再撤暂存 —— 补勾（2026-10-27）：bash -n ok；实地演练：暂存临时 .md → 守卫触发且零差额拒绝（203>202 exit 1，错误文案具名）→ reset 清场；死路径 check-openspec.sh→gates/ 已修
- [x] 3. F：`bash scripts/lint.sh` exit 0 + `bash scripts/gates/check-openspec.sh` exit 0 + `openspec validate doc-budget-lockstep --strict` → 归档 —— 补勾（2026-10-27）：lint RC=0（lint: ok）、check-openspec RC=0、validate --strict 过
