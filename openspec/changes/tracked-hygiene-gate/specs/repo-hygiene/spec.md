## ADDED Requirements

### Requirement: 追踪卫生门

版本库的追踪集合 SHALL 保持卫生：任一追踪文件 MUST 非空（显式白名单除外，白名单起步为空且必须以修改门脚本的方式增删）；任一追踪路径 MUST NOT 被任何 `.gitignore` 规则命中，判定以 `git check-ignore --no-index` 为准（默认模式会跳过已追踪文件，等于对目标失明）。

门 MUST 运行在 lint 聚合面（`scripts/lint.sh`）内，使本地与 CI 对"lint 通过"的含义一致；门红 MUST 阻断，放行方式只有清理或改白名单，MUST NOT 以删除门或改 `.gitignore` 换绿。

#### Scenario: 运行期残骸不可入库

- **WHEN** 一个 0 字节的运行期文件（如写者锁）或位于 ignore 规则覆盖目录下的路径被提交后运行门
- **THEN** 门 MUST 红并点名具体路径

#### Scenario: 清理后保持绿

- **WHEN** 追踪集合无空文件且无 ignore 命中
- **THEN** 门绿，且该判定在本地脚本与 CI 为同一实现

#### Scenario: 豁免必须显式

- **WHEN** 未来出现合法的空追踪文件（如 `.gitkeep`）
- **THEN** 只能通过修改门脚本的白名单收录，白名单本身留在版本库内可审计
