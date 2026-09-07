# evals — 组件级行为评估(D4 精简骨架)

运行:`go test ./evals/`(mock 可跑,进 CI;真实 LLM 项 opt-in,同 tests/ 跳过机制)。

## Suites

| suite | 文件 | 断言 |
|---|---|---|
| ticket-recall | `suites/ticket-recall.yaml` | 压缩后卡片 key 经 FormatEventKey↔ParseEventKey↔GetEvent 全量可召回(原文一致) |
| tool-choice | `suites/tool-choice.yaml` | 注册表路由:场景→期望工具(mock model 注入) |
| bad-case | `cases/` | hex 断裂 key 显式拒绝(历史教训资产化:tests/README「静默存活多日」) |

## 扩展

新 eval:在 `suites/` 加 YAML 声明 + `evals_test.go` 加对应用例;契约类守护(handoff 四段)以「prompt 含契约段」存在性断言入 suites(漂移即红)。

## 注意

- 受控路径下的契约 prompt(resources/prompts/plan_agent.md 等)为 git-native 受控文件——本地修改须 `refine register` 登记。
