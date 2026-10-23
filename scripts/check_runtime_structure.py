#!/usr/bin/env python3
"""check_runtime_structure.py — 超大文件结构与文件级一览（非产品行为）。

递归遍历仓库的一方产品 Go 与 tests（不靠“目录白名单”，新目录不会被漏），
按文件角色套用行数上限：产品 Go 500 行、`_test.go`（任意目录层级）1000 行。
不做启发式“实现滞留”判定（不按构造器名等猜测），不设新 baseline 豁免，
也不复制 architecture policy 的解析（依赖方向是另一个检查器的职责）。

prototype/ 与 scripts/ 是历史参照与仓库工程脚本，默认排除以免被新产品门误伤；
可经 --exclude 追加。没有一方 Go 文件或仓库根缺失时，必须报“未具备实现/参数
错误”，绝不整体 green。
"""
from __future__ import annotations

import argparse
import os
import sys

import runtime_checklib as lib

DEFAULT_EXCLUDES = (
    ".git",
    ".github",
    ".agents",
    ".tagent-workspace",
    ".pre-isolation-backup",
    "prototype",
    "scripts",
    "vendor",
    "node_modules",
)
PRODUCT_MAX_LINES = 500
TEST_MAX_LINES = 1000


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=".", help="仓库根目录")
    parser.add_argument("--exclude", action="append", default=[],
                        help="追加排除的目录名（可重复；在原默认集合之外）")
    parser.add_argument("--product-max-lines", type=int, default=PRODUCT_MAX_LINES)
    parser.add_argument("--test-max-lines", type=int, default=TEST_MAX_LINES)
    parser.add_argument("--json", action="store_true")
    return parser.parse_args(argv)


def walk_go_files(repo_root, excludes):
    rows = []
    for current, dirs, files in os.walk(repo_root):
        dirs[:] = sorted(
            name for name in dirs
            if name not in excludes and not name.startswith(".")
        )
        for name in sorted(files):
            if not name.endswith(".go"):
                continue
            path = os.path.join(current, name)
            rel = os.path.relpath(path, repo_root)
            try:
                with open(path, "r", encoding="utf-8", errors="replace") as handle:
                    line_count = len(handle.read().splitlines())
            except OSError as exc:
                rows.append({"path": rel.replace(os.sep, "/"), "lines": -1, "kind": "unreadable",
                             "error": str(exc)})
                continue
            kind = "test" if name.endswith("_test.go") else "product"
            rows.append({"path": rel.replace(os.sep, "/"), "lines": line_count, "kind": kind})
    return rows


def run(argv):
    args = parse_args(argv)
    report = lib.Report("check_runtime_structure")
    repo_root = os.path.abspath(args.repo_root)
    report.note("repo root: {}".format(repo_root))
    if not os.path.isdir(repo_root):
        sys.stderr.write("repo root not found: {}\n".format(repo_root))
        return 2
    if args.product_max_lines <= 0 or args.test_max_lines <= 0:
        sys.stderr.write("line limits must be positive\n")
        return 2
    excludes = set(DEFAULT_EXCLUDES) | set(args.exclude)
    report.extra["excludes"] = sorted(excludes)
    rows = walk_go_files(repo_root, excludes)
    if not rows:
        report.error("no first-party Go sources under {}: implementation not present".format(repo_root))
        return report.emit(args.json)
    product = [row for row in rows if row["kind"] == "product"]
    tests = [row for row in rows if row["kind"] == "test"]
    unreadable = [row for row in rows if row["kind"] == "unreadable"]
    for row in unreadable:
        report.error("unreadable file {}: {}".format(row["path"], row.get("error")))
    violations = []
    for row in rows:
        if row["kind"] == "unreadable":
            continue
        limit = args.test_max_lines if row["kind"] == "test" else args.product_max_lines
        if row["lines"] > limit:
            violations.append(row)
            report.error("{}: {} lines > {} (limit {} for {})".format(
                row["path"], row["lines"], limit, limit, row["kind"]))
    report.extra["files"] = sorted(rows, key=lambda item: item["path"])
    report.count("go_files", len(rows))
    report.count("product_files", len(product))
    report.count("test_files", len(tests))
    report.count("violations", len(violations))
    report.count("product_max_lines", args.product_max_lines)
    report.count("test_max_lines", args.test_max_lines)
    if product:
        biggest = max(product, key=lambda item: item["lines"])
        report.count("largest_product_lines", biggest["lines"])
        report.note("largest product file: {} ({} lines)".format(biggest["path"], biggest["lines"]))
    if tests:
        biggest = max(tests, key=lambda item: item["lines"])
        report.count("largest_test_lines", biggest["lines"])
        report.note("largest test file: {} ({} lines)".format(biggest["path"], biggest["lines"]))
    report.note("short files are legitimate: no merge is forced on this basis")
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
