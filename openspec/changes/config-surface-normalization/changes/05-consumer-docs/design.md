# 05 消费者收尾 · 设计

> 静态决策见一级 [design.md](../../design.md) §六 D-05、§七消费者全清单。

## 文件清单

- `docs/api/*.md`：godoc 重生成（AgentSpec.ReplayWindow/ContextWindow/输出限字段注释；删 OutputReserve/Params）。
- `CHANGELOG.md`：Unreleased 追加归位记录（四要素：内容=预算三量归位+params 删；影响范围=配置面/远端 yaml/指纹；触发条件=乙门唯一来源 context_window；自查方法=S1 红转绿+门 windows==1+grep 零残留）。
- `examples/wechat-bot/README.md`：涉预算键处改新键。
- `docs/wiki/platform/org-hot-reload.md`：热参名 max_tokens→replay_window。

## 风险与回退

- **风险**：godoc 重生成漏字段→grep 核对；旧键残留 docs/examples→grep `output_reserve\|provider_context_window` 零残留（归档除外）。
- **回退**：git revert；doc 门绿作验收。