#!/usr/bin/env python3
"""Selftests for check_vocabulary_alignment.py - tempfile fixtures, subprocess only.

A pure phrase ban: every planted banned occurrence must be rejected (exit 1) and
every legitimate form must pass (exit 0). Each case checks the real exit code, so
a gate that prints a rule but exits 0 proves nothing. No baseline is involved.
"""
from __future__ import annotations

import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_vocabulary_alignment.py")
FAILURES = []
CASES = 0
ASSERTS = 0

CLEAN_GO = """package alpha

// Facts returns the records the log holds.
func Facts() []string { return nil }

// Recorder generation keeps the process instance that wrote a fact.
func Recorder() string { return "" }
"""
CLEAN_DOC = "# alpha\n\nThe package declares no retired word.\n"


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def build_fixture(root, files=()):
    write(root, "docs/api/alpha.md", CLEAN_DOC)
    write(root, "alpha.go", CLEAN_GO)
    for rel, text in files:
        write(root, rel, text)


def run_checker(root, extra=None):
    command = [sys.executable, CHECKER, "--root", root]
    if extra:
        command.extend(extra)
    proc = subprocess.run(
        command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=300
    )
    return proc.returncode, proc.stdout


def check(problems, condition, message):
    global ASSERTS
    ASSERTS += 1
    if not condition:
        problems.append(message)


def report(name, problems, output):
    global CASES
    CASES += 1
    if problems:
        FAILURES.append(name)
        print("FAIL " + name)
        for problem in problems:
            print("  " + problem)
        for line in output.splitlines():
            print("  " + line)
    else:
        print("PASS " + name)


def case(name, files=(), expected_rc=0, contains=(), excludes=()):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, files=files)
        rc, output = run_checker(root)
        problems = []
        check(problems, rc == expected_rc, "exit {} != {}".format(rc, expected_rc))
        for needle in contains:
            check(problems, needle in output, "missing {!r}".format(needle))
        for needle in excludes:
            check(problems, needle not in output, "unexpected {!r}".format(needle))
        report(name, problems, output)


def with_comment(line):
    return [("alpha.go", CLEAN_GO + "\n" + line + "\n")]


def main():
    case("a clean tree passes", contains=["violations=0"], excludes=["ERROR"])

    case(
        "a carrier identifier is rejected",
        files=[("alpha.go", CLEAN_GO + "\ntype commitCarrier interface{ Commit() }\n")],
        expected_rc=1,
        contains=["ERROR", "carrier=1", "commitCarrier"],
    )
    case(
        "a carrier prose comment is rejected",
        files=with_comment("// the carrier stores facts"),
        expected_rc=1,
        contains=["ERROR", "carrier=1"],
    )
    case(
        "a cross-line folding generation is rejected",
        files=[("bands.go", "package alpha\n\n// The segments promote by folding\n// generation into three bands.\nfunc Bands() int { return 3 }\n")],
        expected_rc=1,
        contains=["bands.go:3", "compression-generation=1"],
    )
    case(
        "a single-line folding generation is counted once",
        files=[("bands.go", "package alpha\n\n// The segments promote by folding generation into three bands.\nfunc Bands() int { return 3 }\n")],
        expected_rc=1,
        contains=["compression-generation=1", "violations=1"],
        excludes=["compression-generation=2"],
    )
    case(
        "a foldGeneration identifier is rejected",
        files=[("bands.go", "package alpha\n\nfunc foldGeneration() int { return 3 }\n")],
        expected_rc=1,
        contains=["ERROR", "compression-generation"],
    )
    case(
        "a generationalBand identifier is rejected",
        files=[("bands.go", "package alpha\n\nfunc generationalBand() int { return 3 }\n")],
        expected_rc=1,
        contains=["ERROR", "compression-generation"],
    )
    case(
        "a folding generation string literal is rejected",
        files=[("message.go", 'package alpha\n\nfunc Message() string { return "folding generation" }\n')],
        expected_rc=1,
        contains=["ERROR", "compression-generation"],
    )
    case(
        "a Product in api/plugin is rejected",
        files=[("api/plugin/material.go", "package plugin\n\ntype Product struct{}\n")],
        expected_rc=1,
        contains=["api/plugin/material.go", "plugin-product"],
    )
    case(
        "compounds MaterialProduct / pluginProduct in api/plugin are rejected",
        files=[("api/plugin/material.go", "package plugin\n\ntype MaterialProduct struct{}\n\ntype pluginProduct struct{}\n")],
        expected_rc=1,
        contains=["plugin-product", "MaterialProduct"],
    )
    case(
        "the retired word 读透视图 is rejected",
        files=with_comment("// The reading used to be called 读透视图 before the rename."),
        expected_rc=1,
        contains=["retired-word", "读透视图"],
    )
    case(
        "the retired word 链权威答案 is rejected",
        files=with_comment("// The answer used to be called 链权威答案 before the rename."),
        expected_rc=1,
        contains=["retired-word", "链权威答案"],
    )

    case(
        "gen: anchor, instance_id and recorder generation are legitimate",
        files=[("generation.go", "package alpha\n\n// The line carries [gen:<generation>], the recorder generation of the\n// fact, and the instance_id behind it.\nfunc stripRecorderGenerations(s string) string { return s }\n")],
        contains=["violations=0"],
        excludes=["ERROR"],
    )
    case(
        "a compress.product fact family in api/event is outside the plugin watch",
        files=[("api/event/compress_product.go", "package event\n\n// CompressProduct is the compressed-product fact of one request.\ntype CompressProduct struct {\n\tProduct []byte\n}\n")],
        contains=["violations=0"],
        excludes=["ERROR"],
    )
    case(
        "lowercase product prose in api/plugin is not the deleted matrix",
        files=[("api/plugin/material.go", "package plugin\n\n// One face answers each block as its own product, and the products stay\n// traceable to the fact each was rendered from.\nfunc Blocks() []string { return nil }\n")],
        contains=["violations=0"],
        excludes=["ERROR"],
    )
    case(
        "the verbs carries / carried / carrying are not carrier",
        files=with_comment("// The port carries facts, carried them earlier, and is carrying them now."),
        contains=["violations=0"],
        excludes=["ERROR"],
    )

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root)
        os.remove(os.path.join(root, "docs/api/alpha.md"))
        rc, output = run_checker(root)
        problems = []
        check(problems, rc == 1, "a missing api document corpus must be red: exit {} != 1".format(rc))
        check(problems, "docs/api" in output, "missing the scan-face refusal")
        report("a missing api document corpus is red", problems, output)

    if FAILURES:
        print(
            "test_check_vocabulary_alignment: {} case(s) FAILED of {} ({} assertions)".format(
                len(FAILURES), CASES, ASSERTS
            )
        )
        return 1
    print(
        "test_check_vocabulary_alignment: {} case(s) PASS ({} assertions)".format(CASES, ASSERTS)
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
