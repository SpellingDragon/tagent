# 01-vocabulary 设计

## 技术要点

文件清单：docs/GLOSSARY.md、CHANGELOG.md、docs/wiki/memory/memory-architecture.md、docs/wiki/examples/wechat-bot-runtime.md、docs/api/wechat-bot.md、examples/wechat-bot/{README.md,tagent.yaml,wechat_notice.go,wechat_test.go,wechat_notice_test.go,wechat.go,restart-maintenance.sh}、openspec/specs/{ops-notice-delivery,task-registry-rebuild,delivery-dispatch-receipts}/spec.md、golden/runtime/spec_hygiene.json。历史快照 docs/wiki/platform/reincarnation-notice.md 按裁决保持原文，不在改动清单内；tests/integration/acceptance/scenario_s05_test.go 的测试名恢复后与基线一致，无需改动；features/evolution/meditation/judgment.go 只持有冻结契约值，本变更不改。风险：契约值误改会破坏冥想血统白名单；回退：F1 断言 + 全模块 go test。GLOSSARY 第 3 行「术语不改名」与本次裁定冲突，须同步改写。

## 契约拼写（不随术语更名）

六个冻结字符串（四个契约拼写 + 两个测试名）的唯一登记处是 docs/GLOSSARY.md「五、契约拼写（不随术语更名）」（用户裁定 r9 偿还入受测区）；本节不保留副本，避免双份漂移。verify_contracts.py 逐行集合核对的命令冻结在 tasks 1.4。

## 风险与回退

见上；每单以复跑命令为准，失败即回退本单并回写计划。
