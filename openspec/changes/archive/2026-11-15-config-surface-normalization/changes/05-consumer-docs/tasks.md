# 05 消费者收尾 · 孙任务

> W2 并行（依赖 01 落地）。verify 三要素随任务行冻结。

- [x] 5.1 重生成 `docs/api/*.md` godoc；改 `CHANGELOG.md`/`examples/wechat-bot/README.md`/`docs/wiki/platform/org-hot-reload.md` 涉预算键处 —— 验证：正锚 doc 门绿（`check_doc_budget.py`/`gen_godoc.sh --check`/`check_spec_hygiene.py`/`check_doc_links.py` 各 exit 0；doc_budget 经编排者具名上调 +6 行：CHANGELOG 四要素 +4、orchestration 乙勘误 +2）；消费者锚 `git grep -nI "output_reserve\|provider_context_window" -- docs/ examples/` 零残留（exit 1，归档除外）；godoc 反映 AgentSpec 新字段名（ReplayWindow/ContextWindow/输出限，无 OutputReserve/Params）。**勘误（实施期 R29 锚精化）**：原消费者锚 `grep -rn` 会命中 gitignore 的编译二进制（wechat-bot/bench-bridge 等）并在 docs/.dev 的 .docx/.zip 上超时 exit 2，非源文档残留；改 `git grep`（仅跟踪源文件）精确复跑，实测 exit 1 零残留。