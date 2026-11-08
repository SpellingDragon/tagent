#!/usr/bin/env python3
"""Selftests for check_assembly_symbols.py - tempfile fixtures, subprocess only.

Each case proves a direction: registered declarations stay green, a new
resolve<Feature>/validate<Feature>/<feature>Port shape is red, and a registration
only covers the exact file it names.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_assembly_symbols.py")
FAILURES = []
CASES = 0

ADR = "docs/adr/guard.md"
FUTURE = "2999-12-31"

CLEAN_ASSEMBLY = """package assembly

type RecallPort struct{}

func resolveKept() {}

func (d *hostDocument) validateKept() {}
"""

def symbol(file_value, name, **overrides):
    entry = {
        "file": file_value,
        "name": name,
        "reason": "fixture symbol",
        "review_by": FUTURE,
    }
    entry.update(overrides)
    return entry


BASE_SYMBOLS = [
    symbol("assembly/fixture.go", "RecallPort"),
    symbol("assembly/fixture.go", "resolveKept"),
    symbol("assembly/fixture.go", "validateKept"),
]


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def registry(symbols=None, **overrides):
    data = {
        "schema_version": 1,
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
        "reason": "fixture registry",
        "symbols": BASE_SYMBOLS if symbols is None else symbols,
    }
    data.update(overrides)
    return data


def build_fixture(root, assembly=None, data=None):
    write(root, "docs/adr/guard.md", "# fixture ADR\n")
    write(root, "assembly/fixture.go", CLEAN_ASSEMBLY if assembly is None else assembly)
    write(root, "golden/runtime/assembly_symbols.json",
          json.dumps(registry() if data is None else data, indent=2) + "\n")


def run_checker(root):
    command = [
        sys.executable,
        CHECKER,
        "--root",
        root,
        "--registry",
        "golden/runtime/assembly_symbols.json",
    ]
    proc = subprocess.run(
        command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120
    )
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


def case(name, assembly=None, data=None, expected_rc=0, contains=(), excludes=()):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, assembly=assembly, data=data)
        rc, output = run_checker(root)
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


def main():
    case("registered declarations pass")

    case(
        "a new resolve symbol is rejected",
        assembly=CLEAN_ASSEMBLY + "\nfunc resolveFancy() {}\n",
        expected_rc=1,
        contains=["resolveFancy", "R2", "not registered"],
    )

    case(
        "a new validate method is rejected",
        assembly=CLEAN_ASSEMBLY + "\nfunc (d *hostDocument) validateFancy() {}\n",
        expected_rc=1,
        contains=["validateFancy", "R2"],
    )

    case(
        "a new Port type is rejected",
        assembly=CLEAN_ASSEMBLY + "\ntype fancyPort struct{}\n",
        expected_rc=1,
        contains=["fancyPort", "R2"],
    )

    case(
        "comments and strings do not declare a symbol",
        assembly=CLEAN_ASSEMBLY
        + '\n// func resolveCommented() {}\nvar fixture = "func resolveString() {}"\n',
        contains=["check_assembly_symbols: ok"],
        excludes=["resolveCommented", "resolveString"],
    )

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root)
        write(root, "assembly/other.go", "package assembly\n\nfunc resolveMoved() {}\n")
        write(
            root,
            "golden/runtime/assembly_symbols.json",
            json.dumps(
                registry(
                    symbols=BASE_SYMBOLS
                    + [symbol("assembly/fixture.go", "resolveMoved")]
                ),
                indent=2,
            )
            + "\n",
        )
        rc, output = run_checker(root)
        problems = []
        if rc != 1:
            problems.append("exit {} != 1".format(rc))
        if "resolveMoved" not in output:
            problems.append("missing 'resolveMoved'")
        if "assembly/other.go" not in output or "not registered" not in output:
            problems.append("the declaration in the new file was not rejected")
        report("a registration is bound to its file", problems, output)

    case(
        "a stale registration is rejected",
        data=registry(
            symbols=BASE_SYMBOLS + [symbol("assembly/fixture.go", "resolveGone")]
        ),
        expected_rc=1,
        contains=["resolveGone", "no longer exists"],
    )

    case(
        "a registry that is not an object is rejected",
        data=[],
        expected_rc=1,
        contains=["must be a JSON object"],
    )

    missing_review = registry()
    del missing_review["symbols"][0]["review_by"]
    case(
        "a symbol without a review deadline is rejected",
        data=missing_review,
        expected_rc=1,
        contains=["missing review_by"],
    )

    missing = registry()
    del missing["adr"]
    case(
        "a registry without its ADR metadata is rejected",
        data=missing,
        expected_rc=1,
        contains=["missing adr"],
    )

    case(
        "a registration outside assembly is rejected",
        data=registry(symbols=BASE_SYMBOLS + [symbol("runtime/x.go", "resolveX")]),
        expected_rc=1,
        contains=["outside the assembly/ scope"],
    )

    if FAILURES:
        print("test_check_assembly_symbols: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("test_check_assembly_symbols: {} case(s) PASS".format(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
