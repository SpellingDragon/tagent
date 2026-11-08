# 01-vocabulary 孙任务

- [x] 1.1 中文词汇「转世」→「重启恢复」叙述层替换（含 `*.sh`/`*.yaml`） —— 验证：`grep -rn --binary-files=without-match --include='*.md' --include='*.go' --include='*.sh' --include='*.yaml' --include='*.yml' '转世' docs examples openspec --exclude-dir=restart-recovery-optimization --exclude-dir=knowledge_base --exclude-dir=.dev --exclude-dir=.git | grep -v 'openspec/changes/archive/' | grep -v 'docs/wiki/platform/reincarnation-notice.md' | grep -vF '[转世通报]' | wc -l` == 0。豁免范围：`openspec/changes/archive/`、本变更目录、`knowledge_base`、`docs/.dev` 历史笔记、历史快照 `docs/wiki/platform/reincarnation-notice.md`，以及四个冻结契约拼写（其中只有 `[转世通报]` 含中文，其余三个为 ASCII 不命中 `转世`）与 GLOSSARY 的撤词声明。
- [x] 1.2 代码标识符简化规范化（maybeInjectReincarnationNotice→injectRestartNotice 等，提案表见 design） —— 验证：`go build ./... && go test ./... -count=1 && go -C examples/wechat-bot test ./... -count=1` exit 0（`examples/wechat-bot` 是独立 module，根模块 `go test ./...` 不覆盖它）。
- [x] 1.3 撤销「读透视图/链权威答案」作为项目词汇，改代码实名；同步 GLOSSARY 第 3 行 —— 验证：`grep -rnE '^(#+|\|).*(读透视图|链权威答案)' docs --include='*.md' --exclude-dir=.dev | wc -l` == 0（判据是「不是术语行/标题」，撤词声明可继续点名这两个词，不按字符串总数清零）。
- [x] 1.4 契约零改动断言（相对基线 `1c4e165f` 的 worktree，不是 vacuous 的 `HEAD..HEAD`） —— 验证：`python3 openspec/changes/restart-recovery-optimization/changes/01-vocabulary/verify_contracts.py` 打印 `VERDICT: PASS (line sets identical)` 且 `MISMATCH_FILES= 0`。脚本对 10 个契约持有文件，按六个冻结字符串的固定子串抽取基线行集合与工作树行集合逐集合比对；禁用 grep -E，因为 `[转世通报]` 在正则里是字符类，会把含「转」/「世」的叙述行误计为契约改动（实测 50 行假阳性）。基线实测 10 文件 56 行。契约表唯一登记处是 docs/GLOSSARY.md「五、契约拼写」（r9 偿还入受测区），不在零改动清单内。

## verify 三锚（随任务行冻结，R29；阶段 1 只结正锚）

- 正锚（已证）：1.1 叙述层 `转世`=0；1.2 根模块 build/test 与 `examples/wechat-bot` 独立模块 test 全绿。
- 负锚（待阶段 6）：F1 负例测试（篡改契约值应被门禁阻断）属 06-guards-and-tests，本阶段未跑；1.4 是 `verify_contracts.py` 的自动逐集合比对（非人工核对），其「能否检出篡改」由阶段 6 的负例测试证明。
- 消费者锚（待阶段 6）：真实 Boot 日志含「重启恢复」、冥想血统白名单仍认 `"reincarnation"` 的端到端取证，留待阶段 6 真实运行。
