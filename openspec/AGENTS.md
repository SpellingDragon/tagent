# OpenSpec 规格与归档纪律（tagent）

本项目用 OpenSpec 做 spec-driven 开发。主规格目录 `openspec/specs/` 是**能力的事实来源**，`openspec/changes/` 是进行中/已归档的变更。以下纪律用于**防止历史上出现过的失效模式**：归档时把 delta 文本（`## ADDED/MODIFIED Requirements`）直接写进主 spec，导致 43 个主 spec 无 `## Purpose`、无法通过 `--strict`。

## 主 spec 结构（openspec/specs/<capability>/spec.md）

- 必须以 `# <capability> Specification` 开头，含 `## Purpose`（≥50 字符）与 `## Requirements`。
- **禁止**在主 spec 出现 `## ADDED / MODIFIED / REMOVED / RENAMED Requirements`——那是 change delta 的语法，只能出现在 `openspec/changes/*/specs/`。
- 每个 `### Requirement:` 必须含 `SHALL`/`MUST` 语句，且至少一个 `#### Scenario:`。

## 归档纪律

1. 归档前先跑门禁：`scripts/check-openspec.sh`（等价 `openspec validate --all --strict`，仅在主 spec 失败时阻断）。
2. 用 `openspec archive <change>` 归档；**不要**用 `--skip-specs` / `--no-validate` 绕过 spec 同步与校验——绕过正是历史 delta-直写问题的根因。
3. 若 `archive` 因某个主 spec 的**既有**结构问题而中止：先修好那个主 spec（补 Purpose / 转正结构）再归档，而非跳过。
4. 归档后再次跑 `scripts/check-openspec.sh` 确认 `openspec/specs/` 仍全绿。

## 门禁接线（可选，推荐）

`scripts/check-openspec.sh` 可手动运行，或接入 CI / 本地 pre-commit：

```sh
# 本地启用（opt-in，不改动仓库外配置）
git config core.hooksPath scripts/hooks   # 若提供了 hooks 目录
```

> 说明：`openspec/changes/` 下**进行中**的 change 未通过 `--strict` 不阻断（属 WIP）；门禁只强制 `openspec/specs/` 主 spec。完成的 change 请及时 `openspec archive`。
