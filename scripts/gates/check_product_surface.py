#!/usr/bin/env python3
"""check_product_surface.py - product-surface hygiene ratchet (not product verification).

Two rules, both from real incidents this week:

  A. Diagnostic sleeps: 1c8bc721 pushed a time.Sleep(300ms) with a TEMP-DIAG
     marker into assembly/prepare.go; 3ef658c9 reverted it two commits later.
     No build/test/race gate caught it. This rule requires product Go sleeps to
     live only in a frozen allowed list, and rejects any diagnostic marker.

  B. Test-only exported hooks: 0ffce718 exported SwapFrameworkLogger for tests,
     breaking the ORCHESTRATION redline "any new public name -> stop and ask" and
     evidence/inherited-pr36-plan/design.md:61 "CI fixes must not add public API".
     The concept gate D1 registers exported named TYPES only, so functions are
     invisible. This rule freezes the set of exported functions with no product
     caller but with test references; a new one turns the gate red.

Static only: no type checking, no semantic proof. It guarantees no silent growth.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

BASELINE_DEFAULT = "scripts/gates/product_surface_baseline.json"
DIAG_MARKERS = ("TEMP-DIAG", "TEMP DIAGNOSTIC", "DIAGNOSTIC-SLEEP")
SLEEP_RE = re.compile(r"\btime\.Sleep\s*\(")
FUNC_RE = re.compile(r"^func\s+([A-Z]\w*)\s*\(", re.M)
SKIP_DIRS = {".git", ".quiesce", "node_modules", "__pycache__"}


def product_go_files(root):
    out = []
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d not in SKIP_DIRS and not d.startswith(".tmp")]
        rel_dir = os.path.relpath(dp, root).replace(os.sep, "/")
        if rel_dir.startswith(".tmp") or rel_dir.startswith(".git"):
            continue
        for f in fn:
            if f.endswith(".go") and not f.endswith("_test.go"):
                out.append(os.path.join(dp, f))
    return sorted(out)


def all_go_files(root):
    out = []
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d not in SKIP_DIRS and not d.startswith(".tmp")]
        for f in fn:
            if f.endswith(".go"):
                out.append(os.path.join(dp, f))
    return sorted(out)


def read(path):
    try:
        with open(path, "r", encoding="utf-8") as h:
            return h.read()
    except (OSError, UnicodeError):
        return ""


def sleep_violations(root, allowed):
    errors = []
    for path in product_go_files(root):
        rel = os.path.relpath(path, root).replace(os.sep, "/")
        text = read(path)
        for marker in DIAG_MARKERS:
            if marker in text:
                errors.append("diagnostic marker {!r} in product file {}".format(marker, rel))
        if SLEEP_RE.search(text) and rel not in allowed:
            errors.append("time.Sleep in product file {} is not in allowed_sleep_files "
                          "(a CI fix must not widen the product surface)".format(rel))
    return sorted(set(errors))


def exported_func_inventory(root):
    """name -> (definition files, product callers, test callers)."""
    prod = {}
    for path in product_go_files(root):
        for m in FUNC_RE.finditer(read(path)):
            prod.setdefault(m.group(1), set()).add(path)
    callers_prod = {n: set() for n in prod}
    callers_test = {n: set() for n in prod}
    for path in all_go_files(root):
        text = read(path)
        is_test = path.endswith("_test.go")
        for n in prod:
            if re.search(r"\b" + re.escape(n) + r"\b", text):
                if is_test:
                    callers_test[n].add(path)
                elif path not in prod[n]:
                    callers_prod[n].add(path)
    return prod, callers_prod, callers_test


def hook_violations(root, baseline_names):
    prod, cprod, ctest = exported_func_inventory(root)
    observed = sorted(n for n in prod if ctest[n] and not cprod[n])
    base = set(baseline_names)
    errors = ["new test-only exported function {!r} in {}: a CI fix must not add public "
              "API (ORCHESTRATION redline); internalize it or record a named waiver"
              .format(n, sorted(os.path.relpath(p, root).replace(os.sep, "/") for p in prod[n])[0])
              for n in observed if n not in base]
    warnings = ["baseline test-only exported function {!r} is gone (stale entry; remove it)"
                .format(n) for n in sorted(base - set(observed))]
    return sorted(errors), sorted(warnings), observed


def check_tree(root, baseline):
    errors, warnings = [], []
    errors.extend(sleep_violations(root, set(baseline.get("allowed_sleep_files") or [])))
    e, w, observed = hook_violations(root, baseline.get("test_only_exported_funcs") or [])
    errors.extend(e)
    warnings.extend(w)
    return {"errors": errors, "warnings": warnings,
            "counts": {"sleep_violations": len(sleep_violations(root, set(baseline.get("allowed_sleep_files") or []))),
                       "test_only_exported_funcs": len(observed)}}


def main(argv=None):
    p = argparse.ArgumentParser()
    p.add_argument("--repo-root", default=".")
    p.add_argument("--baseline", default=BASELINE_DEFAULT)
    p.add_argument("--json", action="store_true")
    a = p.parse_args(argv)
    root = os.path.abspath(a.repo_root)
    bpath = a.baseline if os.path.isabs(a.baseline) else os.path.join(root, a.baseline)
    try:
        with open(bpath, "r", encoding="utf-8") as h:
            baseline = json.load(h)
    except (OSError, ValueError) as exc:
        print("check_product_surface: cannot read baseline {}: {}".format(bpath, exc))
        return 1
    result = check_tree(root, baseline)
    if a.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        for e in result["errors"]:
            print("ERROR: " + e)
        for w in result["warnings"]:
            print("WARN: " + w)
        print("check_product_surface: {} error(s), {} warning(s), {} frozen test-only hook(s)"
              .format(len(result["errors"]), len(result["warnings"]),
                      result["counts"]["test_only_exported_funcs"]))
    return 1 if result["errors"] else 0


if __name__ == "__main__":
    sys.exit(main())
