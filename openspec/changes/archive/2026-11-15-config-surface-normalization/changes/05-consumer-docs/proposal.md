# 05 消费者收尾（godoc/docs）

## Why

一级 design §六 D-05：预算字段归位后，生成 godoc 与消费者文档须同步——`docs/api/*.md`（godoc 重生成）、`CHANGELOG.md`、`examples/wechat-bot/README.md`、`docs/wiki/platform/org-hot-reload.md` 涉预算键处。文档随代码走，脱节即修。

## What

重生成 `docs/api/*.md` godoc（反映 AgentSpec 新字段名）；改 CHANGELOG/README/wiki 涉 `output_reserve`/`provider_context_window`/`max_tokens`（回放窗义）处为新键与派生语义。

## 边界与依赖

- **依赖**：01 落地（W1 后，godoc 源字段已改名）。
- **被依赖方**：F1 契约面一致；doc 门。
- **接口面**：godoc 由字段注释生成；CHANGELOG 四要素（内容+影响范围+触发条件+自查方法）。
- **禁止事项**：不改写冻结归档；不触 §五冻结拼写；doc-budget 棘轮只降不升（若涉体量）；与 03/04 触碰集互不相交，可并行；03 改 GLOSSARY/spec，05 改 godoc/CHANGELOG/README/wiki，不重叠。