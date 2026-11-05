#!/usr/bin/env python3
"""Selftests for check_feature_boundary.py - tempfile fixtures, subprocess only.

Every case proves a direction: the clean fixture stays green, and a newly added
feature-specific schema section / assembly symbol / runtime literal turns red.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_feature_boundary.py")
FAILURES = []
CASES = 0

CLEAN_HOST = """provider: fixture
agents:
  alpha:
    provider: fixture
    model: fixture
    system_prompt:
      files: [alpha.md]
    tools:
      - { kind: tool, id: send }
    memory:
      type: localfile
    meditation:
      enabled: true
"""

CLEAN_ASSEMBLY = """package assembly

type RecallPort struct{}

func resolveKept() {}

func (d *hostDocument) validateKept() {}
"""

CLEAN_RUNTIME = """package runtime

// meditation and reflection are only comments here.
/* reflection is also a comment */
func Work() {}
"""

BASE_SYMBOLS = ["RecallPort", "resolveKept", "validateKept"]


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def write_baseline(root, symbols):
    path = os.path.join(root, "baseline.json")
    write(root, "baseline.json", json.dumps(
        {"schema_version": 1, "symbols": sorted(symbols)}, indent=2) + "\n")
    return path


def build_fixture(root, host=None, assembly=None, runtime=None, symbols=None):
    write(root, "examples/wechat-bot/tagent.yaml", CLEAN_HOST if host is None else host)
    write(root, "assembly/fixture.go", CLEAN_ASSEMBLY if assembly is None else assembly)
    write(root, "runtime/fixture.go", CLEAN_RUNTIME if runtime is None else runtime)
    return write_baseline(root, BASE_SYMBOLS if symbols is None else symbols)


def run_checker(root, baseline, extra=None):
    command = [sys.executable, CHECKER, "--root", root, "--baseline", baseline]
    if extra:
        command.extend(extra)
    proc = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                          text=True, timeout=120)
    return proc.returncode, proc.stdout


def report(name, problems, output):
    global CASES
    CASES += 1
    if problems:
        FAILURES.append(name)
        print("FAIL " + name)
        for problem in problems:
            print("  " + problem)
        print("  --- checker output ---")
        for line in output.splitlines():
            print("  " + line)
    else:
        print("PASS " + name)


def case(name, host=None, assembly=None, runtime=None, symbols=None,
         expected_rc=0, contains=(), excludes=(), extra=None):
    with tempfile.TemporaryDirectory() as root:
        baseline = build_fixture(root, host=host, assembly=assembly,
                                 runtime=runtime, symbols=symbols)
        rc, output = run_checker(root, baseline, extra)
        problems = []
        if rc != expected_rc:
            problems.append("exit {} != {}".format(rc, expected_rc))
        for needle in contains:
            if needle not in output:
                problems.append("missing {!r}".format(needle))
        for needle in excludes:
            if needle in output:
                problems.append("unexpected {!r}".format(needle))
        report(name, problems, output)


def case_write_baseline():
    name = "--write-baseline records the current R2 symbols"
    with tempfile.TemporaryDirectory() as root:
        assembly = CLEAN_ASSEMBLY + "\nfunc resolveAdded() {}\n"
        baseline = build_fixture(root, assembly=assembly)
        rc, output = run_checker(root, baseline, ["--write-baseline"])
        problems = []
        if rc != 0:
            problems.append("write exit {} != 0".format(rc))
        try:
            with open(baseline, "r", encoding="utf-8") as handle:
                symbols = json.load(handle).get("symbols") or []
        except (OSError, ValueError) as exc:
            symbols = []
            problems.append("cannot read written baseline: {}".format(exc))
        if "resolveAdded" not in symbols:
            problems.append("resolveAdded missing from the written baseline")
        check_rc, check_output = run_checker(root, baseline)
        if check_rc != 0:
            problems.append("check after write exit {} != 0".format(check_rc))
        report(name, problems, output + check_output)


def case_object_baseline():
    """The shared golden registry is a dict whose 'symbols' are objects with a
    'name' field; a top-level list of such objects is accepted too. Both must
    reduce to the same symbol names so the fixture stays green."""
    golden_dict = {
        "schema_version": 1,
        "symbols": [
            {"file": "assembly/fixture.go", "name": n, "reason": "fixture",
             "review_by": "2999-12-31"}
            for n in BASE_SYMBOLS
        ],
    }
    golden_list = [{"name": n} for n in BASE_SYMBOLS]
    for label, payload in (
        ("dict with object 'symbols'", golden_dict),
        ("top-level list of objects", golden_list),
    ):
        name = "object-shaped baseline is accepted ({})".format(label)
        with tempfile.TemporaryDirectory() as root:
            build_fixture(root)
            baseline = os.path.join(root, "baseline.json")
            write(root, "baseline.json", json.dumps(payload, indent=2) + "\n")
            rc, output = run_checker(root, baseline)
            problems = []
            if rc != 0:
                problems.append("exit {} != 0".format(rc))
            if "check_feature_boundary: ok" not in output:
                problems.append("missing ok marker")
            for sym in BASE_SYMBOLS:
                if sym in output:
                    problems.append("unexpected {!r} in output".format(sym))
            report(name, problems, output)


def case_write_baseline_refuses_golden():
    """--write-baseline must not silently clobber the reviewed object registry."""
    golden_rel = "golden/runtime/assembly_symbols.json"
    registry = {
        "schema_version": 1,
        "adr": "docs/adr/0006-three-hooks.md",
        "user_ruling": "fixture ruling",
        "review_by": "2027-05-05",
        "reason": "fixture registry",
        "symbols": [
            {"file": "assembly/fixture.go", "name": n, "reason": "fixture",
             "review_by": "2027-05-05"}
            for n in BASE_SYMBOLS
        ],
    }
    for label, extra in (
        ("default golden path", []),
        ("explicit --baseline golden path", ["--baseline", golden_rel]),
    ):
        name = "--write-baseline refuses the golden object registry ({})".format(label)
        with tempfile.TemporaryDirectory() as root:
            build_fixture(root)
            write(root, golden_rel, json.dumps(registry, indent=2) + "\n")
            golden_abs = os.path.join(root, golden_rel)
            with open(golden_abs, "r", encoding="utf-8") as handle:
                before = handle.read()
            command = [sys.executable, CHECKER, "--root", root, "--write-baseline"]
            if extra:
                command.extend(extra)
            proc = subprocess.run(command, stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, text=True, timeout=120)
            rc, output = proc.returncode, proc.stdout
            with open(golden_abs, "r", encoding="utf-8") as handle:
                after = handle.read()
            problems = []
            if rc != 1:
                problems.append("exit {} != 1".format(rc))
            if "refuses to overwrite" not in output:
                problems.append("missing named refusal error")
            if "check_feature_boundary: ok" in output:
                problems.append("unexpected ok marker")
            if after != before:
                problems.append("the golden registry was modified")
            try:
                syms = json.loads(after).get("symbols") or []
                if not syms or not isinstance(syms[0], dict):
                    problems.append("registry lost its object shape")
            except ValueError as exc:
                problems.append("registry no longer parses: {}".format(exc))
            report(name, problems, output)


def main():
    case("clean fixture passes (generic keys, baseline symbols, comments only)",
         contains=["check_feature_boundary: ok", "r1 host_documents=1",
                   "r2 go_files=1 symbols=3"])

    bad_host = CLEAN_HOST.replace("    meditation:", "    fancy_feature: {}\n    meditation:")
    case("new feature-specific schema section is rejected",
         host=bad_host, expected_rc=1,
         contains=["fancy_feature", "R1", "new feature-specific schema section"])

    flow_host = "agents: {a: {fancy_feature: {}}}\n"
    case("flow-style feature-specific schema section is rejected",
         host=flow_host, expected_rc=1, contains=["fancy_feature", "R1"])

    case("new resolve symbol outside the baseline is rejected",
         assembly=CLEAN_ASSEMBLY + "\nfunc resolveFancy() {}\n",
         expected_rc=1, contains=["resolveFancy", "R2", "not in the baseline"])

    case("new validate method outside the baseline is rejected",
         assembly=CLEAN_ASSEMBLY + "\nfunc (d *hostDocument) validateFancy() {}\n",
         expected_rc=1, contains=["validateFancy", "R2"])

    case("new Port type outside the baseline is rejected",
         assembly=CLEAN_ASSEMBLY + "\ntype fancyPort struct{}\n",
         expected_rc=1, contains=["fancyPort", "R2"])

    case("allowlisted baseline symbols stay green while a new symbol is red",
         assembly=CLEAN_ASSEMBLY + "\nfunc resolveNew() {}\n",
         expected_rc=1, contains=["resolveNew"],
         excludes=["resolveKept", "validateKept", "RecallPort"])

    runtime_literal = """package runtime

func Branch(mode string) bool {
	return mode == "meditation"
}
"""
    case("runtime meditation literal in executable code is rejected",
         runtime=runtime_literal, expected_rc=1,
         contains=["runtime/fixture.go:4", "meditation", "R3"])

    case("runtime Reflection identifier is rejected",
         runtime="package runtime\n\ntype ReflectionPlan struct{}\n",
         expected_rc=1, contains=["ReflectionPlan", "R3"])

    case("comment-only meditation/reflection mentions are accepted",
         runtime="""package runtime

// meditation and MeditationPlan are discussed here only.
/* reflection and ReflectionPlan are discussed here only. */
func Work() {}
""", expected_rc=0, contains=["check_feature_boundary: ok"])

    case("--only r3 ignores a schema violation",
         host=bad_host, expected_rc=0, extra=["--only", "r3"],
         contains=["check_feature_boundary: ok"])

    case_write_baseline()
    case_object_baseline()
    case_write_baseline_refuses_golden()

    if FAILURES:
        print("test_check_feature_boundary: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("test_check_feature_boundary: {} case(s) PASS".format(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
