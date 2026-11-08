# 逐模块核对与文档补齐 · 完成报告（2026-11-04，round 2 终）

基线：`dev == origin/dev == 3b727199`；工作树 12 个原有文档修正未提交，全部改动均未提交（不代用户提交）。

## 一句话结论

四份逐模块只读审计（api/runtime/assembly、memory/tool/features、examples/tests/scripts/workflows、specs/wiki）发现的 51 个 G 项全部 DONE 或明确 BLOCKED；CHANGELOG、README(.en)、17 篇 wiki 活契约页、7 篇历史页、manifest、ADR、两份新门禁与 CI 活跃图修正全部落地；13 道门复跑全绿。剩余 7 项是**需用户裁定的决策**（非事实问题），已具名登记，无新的事实问题。

## 已交付

- **文档**：CHANGELOG 补 `[v0.5.0]` 节 + 运行时 BREAKING 四要素四条；README 归并已删 `docs/PROJECT.md` 的「三机制/测试证据/边界」；wiki 活页/历史页/导览页/索引分类口径统一为 16+7+1+1=25；wechat/cli-host/bench 示例 README 的 phantom 符号、鉴权口径、schema 声明全面正名；投递四键→五键、`DecisionCount`、`CloseOutcome` 四值、`Assemble` 在 `run.go:368`、热更结构拒绝范围、model replacement 退役符号等硬错误修正。
- **主规格**：5 份整体过时规格按 `persistent-event-loop` 先例退役为 superseded 记录；6 份活规格 SHALL 就地更正；20 份加「复核注记」区分「仍有现行 SHALL 建立在退役符号上」（B-02 待裁定）与「仅禁止项/历史注记」（9 份）。\`openspec validate --strict\` 141/141 通过。
- **证据链**：`evidence/07` 本地门禁原始退出码、`08` 台账 5 类悬空 hash→可达提交映射、`09` 远端 run 不可核的诚实登记、`10` 缺口登记表、`11` ratchet 设计张力；ORCHESTRATION 补 r159/r163、行序降序；tasks.md 加复核附录（F1–F4 保持未勾）。
- **流程**：`docs/adr/0001-dev-reverse-sync.md` + CONTRIBUTING/OPERATION 的 dev 反向同步规则；wiki-code-sync 补历史/导览页豁免条款。
- **门禁**：新增 `check_product_surface.py`（诊断睡眠门 + 测试专用导出钩子冻结基线，各 7 项负锚 selftest）；新增 `check_doc_links.py`（markdown 链接/#锚 门，7 项负锚 selftest）；G-13 workflow 活跃图对象错位修正 + selftest 负锚；两者挂 `lint.sh`（CI 覆盖）。
- **文档预算**：本轮 ㉖-㉚ 五次**先被零差额门拒、再具名落锁**，无静默上调；终值 215 篇 / 21311 行，零差额。

## 门禁复跑（全绿）

doc budget 0 · doc-refs 0 · check-openspec 0（141/141）· concept budget 0（`errors: []`）· product surface gate + selftest 0/0 · doc links gate + selftest 0/0 · gen_godoc --check 0 · workflow selftest 51 OK · 全量 checker selftests 258 OK · lint --only fmt 0 · go build ./... 0。

已知环境限制：`go test ./... -short -count=1` 的 3 个包 FAIL 全部源于 tmux `fork failed: Operation not permitted`（沙箱），非回归；见 `evidence/07`。

## 需用户裁定的决策（不再有新的事实问题）

| ID | 决策 | 证据 |
|---|---|---|
| B-02 | 11 份主规格的现行 SHALL 是否走 change delta 正式迁移/退役 | `evidence/10-doc-gap-register.md` §6.4 |
| B-03 | `concept_ratchet.new_max` 是否收紧到 0 | `evidence/11-ratchet-tension.md` |
| B-04 | `task_default_ttl` / `task_terminal_ttl` 是否接线 | 3 份规格已标「未接线」 |
| B-05 | `resources/prompts/plan_agent.md` action 枚举（运行时资产，改之变产品行为） | B-05 |
| B-06 | 代码侧：`seg.whole` 无界停放、logger 红线半闭合、`SwapFrameworkLogger` 导出 | `.git/review-notes/grill-conflict-report-2026-11-04.md` §2（按「如果是代码过时，记录并统一报告」执行）。

完整逐条状态与门禁证据见 `evidence/10-doc-gap-register.md` §6。
