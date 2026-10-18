#!/usr/bin/env python3
"""check_doc_budget.py — markdown 文档体量棘轮门（只降不升），不是产品验证。

风险来源：R-H「文档/规格熵」。本仓库的 markdown 体量约为 Go 代码的数倍，历史上
没有上限，任何一次「顺手写一篇说明」都能让总量单调增长且无人对账。本门把总量冻结
成 `scripts/doc_budget.json` 里的两个数，此后只允许下降。

口径（改这里等于改验收面）：
  * 统计对象：**git 索引里的文档面**——`git ls-files --cached --others
    --exclude-standard` 列出、后缀为 `.md` / `.markdown` 的路径（已跟踪 + 尚未提交但
    未被 ignore 的新文件；后者也计入，棘轮不能靠「还没 commit」绕过）；
  * 为什么不用全盘 `find`：全盘会把本地物料也算成仓库文档——本仓现有 607 篇
    gitignored 文件（`examples/wechat-bot` 的变更工件/知识库语料、`docs/.dev` 纪要、
    `.agents`）、21 篇 `node_modules` 第三方包文档、19 篇 `.git/review-notes` 草稿、
    12 篇 submodule 内容。它们在 CI 的干净 checkout 里不存在，若入预算，基线会凭空
    多出约 9.4 万行余量——棘轮等于不设，且本地判红/CI 判绿会不一致；
  * 不计生成物：`docs/api/` 是 `scripts/gen_godoc.sh` 从源码注释生成的，体量由生成器
    和包数决定（现有 48 篇/17805 行）；它的新鲜度已由 `gen_godoc.sh --check` 强制，
    拿它进预算只会让「新增一个 Go 包」逼作者删等量散文——那不是本门要拦的熵；
  * 行数：换行符个数，末尾未终止的半行也算一行（`wc -l` 会漏掉它）；
  * 不跟随符号链接；索引里有、盘上缺失的路径不计入并 warn（tracked-hygiene 门负责那件事）。

棘轮语义（与 check_concept_budget.py 的 BASELINE 同思路，只是基线落在外置 JSON）：
  * 实测 ≤ 基线 → exit 0；实测 < 基线 → warn「可下调」（减少量必须锁进来，否则
    基线虚高，下一批又能涨回去）；
  * 实测 > 基线 → error，exit 1（棘轮只降不升）；
  * `--tighten` 只写「不高于现值」的基线：任一指标会上升即拒绝写盘并 exit 1，
    从机制上堵住「上调基线掩盖增长」这条最省事的作弊路径；
  * 扫描到 0 篇不判绿（扫描坏了/树是空的都不能冒充通过）。

2026-10-19 实测：1150 篇 / 97916 行（`python3 scripts/check_doc_budget.py --json`，
HEAD 7610629 之上的索引（含未提交的批内改动）；基线文件里的 measured_at /
measured_on 是同一快照）。
与 R-H 登记值 1855 篇 / 212905 行 的对账（登记值用 `find . -name '*.md'` 量，
把不进仓库的东西和生成物也算进去了）：1855 = 1150（本门口径）+ 607（gitignored
本地物料）+ 48（`docs/api` 生成物）+ 21（node_modules 第三方包）+ 19（`.git/review-notes`
草稿）+ 12（submodule 内容）− 2（本门落地时同批新增的归档 README 与 CONTRIBUTING.md）。
行数同理：登记值与实测的 11.5 万行差额几乎全来自这 705 篇本地/生成/外部文件。
基线以本门口径的实测为准。

用法：
  python3 scripts/check_doc_budget.py [--repo-root DIR] [--baseline PATH] [--json]
  python3 scripts/check_doc_budget.py --tighten      # 只下调基线，拒绝上调
退出码：0=通过（含可下调 warn）；1=超基线或输入不可用。
"""
from __future__ import annotations

import argparse
import datetime
import json
import os
import subprocess
import sys

import runtime_checklib as lib

# ---- 标尺常数（口径）；改这些常数等于改验收面 ----
DOC_SUFFIXES = (".md", ".markdown")
# 生成物不进预算：体量与包数耦合，新鲜度由生成器自己的 --check 负责。
GENERATED_DOC_PREFIXES = ("docs/api/",)
DEFAULT_BASELINE = os.path.join("scripts", "doc_budget.json")
SCHEMA_VERSION = 1
METRICS = ("files", "lines")
METRIC_LABELS = {"files": "篇数", "lines": "行数"}
TOOL = "check_doc_budget"


def count_lines(path):
    """换行符个数；末尾未终止的半行也计一行（`wc -l` 会漏掉）。"""
    with open(path, "rb") as handle:
        data = handle.read()
    if not data:
        return 0
    return data.count(b"\n") + (0 if data.endswith(b"\n") else 1)


def git_doc_paths(root):
    """Index-scoped document paths: tracked + uncommitted-but-not-ignored.

    The index, not the filesystem walk, defines the corpus. A filesystem walk
    would count local material that never reaches a checkout (ignored artefacts,
    vendored package docs, VCS scratch), which silently buys the ratchet tens of
    thousands of lines of slack and makes local runs disagree with CI.
    """
    if not os.path.isdir(root):
        raise lib.CheckFailure("repo root is not a directory: {}".format(root))
    try:
        proc = subprocess.run(
            ["git", "-C", root, "ls-files", "--cached", "--others",
             "--exclude-standard", "-z"],
            capture_output=True,
        )
    except OSError as exc:
        raise lib.CheckFailure("git is required to bound the corpus to the repository's "
                               "document surface: {}".format(exc))
    if proc.returncode != 0:
        raise lib.CheckFailure("git ls-files failed under {}: {}".format(
            root, proc.stderr.decode("utf-8", "replace").strip()))
    names = proc.stdout.decode("utf-8", "surrogateescape").split("\0")
    return sorted({name for name in names
                   if name and name.lower().endswith(DOC_SUFFIXES)
                   and not name.startswith(GENERATED_DOC_PREFIXES)})


def scan(root):
    """Count the indexed docs; return (per-file rows, totals, unreadable paths).

    The row list keeps the gate auditable (`--json` shows exactly what was
    counted) while totals stay the only judgement input, so a changed file cannot
    move the number without moving a row.
    """
    rows = []
    unreadable = []
    for rel in git_doc_paths(root):
        path = os.path.join(root, rel)
        if os.path.islink(path) or not os.path.isfile(path):
            unreadable.append(rel)
            continue
        rows.append({"path": rel, "lines": count_lines(path)})
    totals = {
        "files": len(rows),
        "lines": sum(row["lines"] for row in rows),
    }
    return rows, totals, unreadable


def describe_tree(root):
    """`<short-sha>[+dirty]` for provenance, or None when the tree has no commit.

    The baseline must say what it measured; a bare date cannot be re-derived later,
    and the corpus includes uncommitted-but-unignored files, so dirtiness matters.
    """
    try:
        proc = subprocess.run(["git", "-C", root, "rev-parse", "--short", "HEAD"],
                              capture_output=True)
        if proc.returncode != 0:
            return None
        commit = proc.stdout.decode("utf-8", "replace").strip()
        status = subprocess.run(["git", "-C", root, "status", "--porcelain"],
                                capture_output=True)
    except OSError:
        return None
    if not commit:
        return None
    return commit + ("+dirty" if status.stdout.strip() else "")


def load_baseline(path):
    """Read and validate the frozen budget; a missing/garbled file is fatal."""
    if not os.path.isfile(path):
        raise lib.CheckFailure(
            "baseline file missing: {} (the ratchet has nothing to compare against)"
            .format(path))
    try:
        with open(path, "r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except ValueError as exc:
        raise lib.CheckFailure("baseline file is not valid JSON: {}: {}".format(path, exc))
    if not isinstance(payload, dict):
        raise lib.CheckFailure("baseline file must hold a JSON object: {}".format(path))
    if payload.get("schema_version") != SCHEMA_VERSION:
        raise lib.CheckFailure("baseline schema_version must be {}: {}"
                               .format(SCHEMA_VERSION, path))
    baseline = payload.get("baseline")
    if not isinstance(baseline, dict):
        raise lib.CheckFailure("baseline file needs a `baseline` object: {}".format(path))
    out = {}
    for metric in METRICS:
        value = baseline.get(metric)
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise lib.CheckFailure("baseline.{} must be a non-negative integer: {}"
                                   .format(metric, path))
        out[metric] = value
    return payload, out


def compare(report, baseline, totals):
    """Apply the one-way ratchet: above fails, below earns a tighten hint."""
    for metric in METRICS:
        limit = baseline[metric]
        observed = totals[metric]
        label = METRIC_LABELS[metric]
        report.count("baseline." + metric, limit)
        report.count("measured." + metric, observed)
        report.count("slack." + metric, limit - observed)
        if observed > limit:
            report.error(
                "文档体量超基线（{}）：实测 {} > 基线 {}（+{}）。棘轮只降不升："
                "同批压掉等量文档，或把减少处锁进基线后重测；禁止上调基线掩盖增长。"
                .format(label, observed, limit, observed - limit))
        elif observed < limit:
            report.warn(
                "文档体量低于基线（{}）：实测 {} < 基线 {}（-{}）。棘轮可下调："
                "跑 `python3 scripts/check_doc_budget.py --tighten` 把减少量锁住，"
                "否则基线虚高、下一批又能涨回去。"
                .format(label, observed, limit, limit - observed))


def tighten(report, path, payload, baseline, totals, root):
    """Rewrite the baseline only in the lowering direction; a rise is refused."""
    rising = [metric for metric in METRICS if totals[metric] > baseline[metric]]
    if rising:
        for metric in rising:
            report.error(
                "拒绝上调基线（{}）：实测 {} > 基线 {}。先压缩文档，再 --tighten。"
                .format(METRIC_LABELS[metric], totals[metric], baseline[metric]))
        return

    if all(totals[metric] == baseline[metric] for metric in METRICS):
        report.note("baseline already equals the measured floor; nothing written")
        report.extra["written"] = False
        return

    lowered = {}
    for metric in METRICS:
        if totals[metric] < baseline[metric]:
            lowered[metric] = [baseline[metric], totals[metric]]

    payload = dict(payload)
    payload["measured_at"] = datetime.date.today().isoformat()
    payload["measured_by"] = "python3 scripts/check_doc_budget.py --json"
    payload["measured_on"] = describe_tree(root) or payload.get("measured_on", "")
    payload["baseline"] = {metric: totals[metric] for metric in METRICS}
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2, sort_keys=True)
        handle.write("\n")

    report.extra["written"] = True
    report.extra["lowered"] = lowered
    for metric in METRICS:
        if metric in lowered:
            before, after = lowered[metric]
            report.note("baseline.{} lowered: {} -> {}".format(metric, before, after))


def parse_args(argv):
    parser = argparse.ArgumentParser(
        description="Markdown document budget ratchet (lower-only).")
    parser.add_argument("--repo-root", default=".",
                        help="repository root to scan (default: cwd)")
    parser.add_argument("--baseline", default=DEFAULT_BASELINE,
                        help="baseline JSON path, relative to --repo-root")
    parser.add_argument("--tighten", action="store_true",
                        help="lower the baseline to the measured values (never raises)")
    parser.add_argument("--json", action="store_true", help="emit the JSON report")
    return parser.parse_args(argv)


def run(argv):
    args = parse_args(argv)
    report = lib.Report(TOOL)
    root = os.path.abspath(args.repo_root)
    baseline_path = args.baseline
    if not os.path.isabs(baseline_path):
        baseline_path = os.path.join(root, baseline_path)
    try:
        rows, totals, unreadable = scan(root)
        payload, baseline = load_baseline(baseline_path)
        report.count("documents", totals["files"])
        report.count("lines", totals["lines"])
        report.extra["files"] = rows
        report.extra["baseline_path"] = os.path.relpath(baseline_path, root).replace(os.sep, "/")
        report.extra["scope"] = ("git index: tracked + untracked-but-not-ignored "
                                 "*.md/*.markdown, excluding generated "
                                 + ", ".join(GENERATED_DOC_PREFIXES))
        if unreadable:
            report.warn("{} indexed document(s) are missing on disk and were not counted "
                        "(tracked-hygiene owns that defect): {}{}".format(
                            len(unreadable), ", ".join(unreadable[:5]),
                            " …" if len(unreadable) > 5 else ""))
        if totals["files"] == 0:
            report.error("no markdown documents found under {}: an empty scan is not a "
                         "green budget".format(root))
        else:
            compare(report, baseline, totals)
            if args.tighten:
                tighten(report, baseline_path, payload, baseline, totals, root)
        report.note("document volume only: whether the prose is right stays with review; "
                    "productVerified stays null")
    except lib.CheckFailure as exc:
        report.error(str(exc))
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
