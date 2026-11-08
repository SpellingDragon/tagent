# restart-recovery-vocabulary Specification

## Purpose
本规范定义 restart-recovery-vocabulary 能力：进程自换后重建现场的机制统一以「重启恢复」指称，叙述层与代码标识符同口径，冻结契约拼写（事件元数据、磁盘标记、日志关键字、通报正文、测试名）不随术语更名变更。
## Requirements
### Requirement: restart-recovery-vocabulary 契约

项目 SHALL 以「重启恢复」指称进程自换后重建现场的机制，MUST NOT 再把「转世」「读透视图」「链权威答案」用作项目词汇；代码标识符 SHALL 与术语一致简化；契约值 MUST NOT 随改名变更。

#### Scenario: 主路径

- **WHEN** 阶段 1 完成
- **THEN** `docs/`、`examples/`、`openspec/`（含 `*.sh` 与 `*.yaml`）中叙述层「转世」为 0；冻结契约拼写（`"reincarnation"`、`[reincarnation]`、`REINCARNATION_NOTICE`、`[转世通报]`）、两个测试名契约、历史快照、归档变更、本变更目录、`docs/.dev` 历史笔记与 `knowledge_base` 不计入该计数
- **AND** 根模块与 `examples/wechat-bot` 独立模块的 go test 全绿
- **AND** `verify_contracts.py` 对 10 个契约持有文件、六个冻结字符串的固定子串逐集合比对，输出 `VERDICT: PASS (line sets identical)` 且 `MISMATCH_FILES= 0`（基线 10 文件 56 行）

