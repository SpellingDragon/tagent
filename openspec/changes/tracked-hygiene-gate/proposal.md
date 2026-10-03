# Proposal: 追踪卫生门（tracked ⇒ 非空且不被 ignore 规则命中）

## Why

版本库里已经躺着一次"运行期产物入库"的事故，而且没有任何机器手段阻止它复发：

- `hottest-sub1/`、`hottest-sub2/` 被 git 追踪，内容是**四个 0 字节**的 `.tagent-writer.lock` 与 `relations.journal`——9 月的测试以相对路径把 memory store 落在仓库 cwd，随后被顺手 `git add`。`testStore` 事后已挪进 `t.TempDir()`（`test_stores_test.go:20`，`.gitignore` 注释同证），10/2–10/3 的十几轮根包测试未再生成任何此类目录 ⇒ 这是**历史遗物，不是活缺陷**。
- 同族工作树遗物：`own-sub1/2`、`own-a2okagent`、`own-zzbadagent`、`hottest-sub3`、`hottest-drop-main/sub1/sub2`（mtime 全停在 9/18–9/23）。
- 孤儿 `go.work.sum`：`go.work` 本身不入库且不存在于工作树，这个 sum 文件是残迹。

根因不是某次手滑，而是**"运行期产物不得入库"没有门**：发生过一次，就会发生下一次。

## What Changes

- **一次性清理**：`git rm` 四个空残骸；删工作树遗物目录与孤儿 `go.work.sum`。
- **立门（真正的交付物）**：`scripts/check_tracked_hygiene.sh`——追踪文件 MUST 非空，追踪路径 MUST NOT 被任何 `.gitignore` 规则命中（`git check-ignore --no-index` 判定）；接入 `scripts/lint.sh` 聚合面，使本地与 CI 同义。白名单机制显式存在、起步为空（未来合法空文件如 `.gitkeep` 走白名单，不得删门）。

## Impact

- Affected specs: `repo-hygiene`（新 capability：追踪卫生门）
- Affected code: 新增 `scripts/check_tracked_hygiene.sh`、`scripts/lint.sh` 增一段；删除 4 个追踪文件与若干未追踪遗物
- 无 Go 代码改动 ⇒ 不触发 `docs/api` 再生成与注释门禁
- 范围外（明确不动）：`.pre-isolation-backup/`、`.tagent-workspace/`、`.agents/`、`.qoder-handover.log`（运行/交接资产，不属于追踪卫生的判断面）
