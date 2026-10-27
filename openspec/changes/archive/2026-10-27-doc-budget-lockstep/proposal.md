# 文档预算锁步：差额恒为零，提交面强制

## 症状（验收矩阵第一行）

| 症状原话 | 直译测试 |
|---|---|
| 「基线 22764 这个也很奇怪哦，应该是0」（用户，2026-10-27） | 任意树态跑 `python3 scripts/gates/check_doc_budget.py`：实测 ≠ 基线（两向）→ exit 1；实测 = 基线 → exit 0；暂存任何 *.md 的提交面 → 同一断言在 pre-commit 跑 |

## Why

归档/发布提交把内容并入计入口径（openspec/specs/**、CHANGELOG、README）时不碰 doc_budget.json——+27 漂移穿过提交面，事后才被 lint 自测抓住，补救=手工 json 手术+散文记账。三处结构缺陷：①checker 主体对「实测<基线」仅 warn（零差额在检查面无强制力）；②仓内 pre-commit 钩子调用已搬走的 `scripts/check-openspec.sh`（b0a340 后死路径）；③钩子不含预算守卫且本环境 core.hooksPath 指向全局目录（钩子未生效）。

## What Changes

- checker 严格零差额：实测 ≠ 基线两向皆 error（低于基线的错误文案指向 `--tighten` 同提交锁入）；`--tighten` 不变=显式锁步工具；退出码契约同步。
- 钩子修复+守卫：`scripts/check-openspec.sh`→`scripts/gates/check-openspec.sh`；暂存含 *.md/*.markdown → 跑预算零差额检查（树级断言，~2s）。
- 自测扩展：低于基线不 tighten → 红；两向等值 → 绿。

## Impact

代码：scripts/gates/check_doc_budget.py、scripts/gates/test_check_doc_budget.py、scripts/hooks/pre-commit。不含 ~/.git-hooks（用户全局面，只修仓内可版本化面）；CI/lint 面自动获得严格语义。

## 必答一问

「本案完成后，基线与现树还会出现非零差额穿过提交面吗？」——不会：任何差额（两向）在检查面即红，锁步（--tighten/具名重锁）必须与文档变更同提交。
