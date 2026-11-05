#!/usr/bin/env python3
"""Selftests for check_durable_truth_registry.py - tempfile fixtures, subprocess.

Each case proves a direction: a registered durable store and a declared non-truth
cache are green; an unregistered write surface, an unregistered *Prefix constant,
a changed prefix value and stale metadata are red.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_durable_truth_registry.py")
FAILURES = []
CASES = 0

ADR = "docs/adr/guard.md"
FUTURE = "2999-12-31"

STORE = """package ledger

import "os"

const keptPrefix = "tagent/kept/v1:"

func Keep(path string) error {
	return os.WriteFile(path, nil, 0o644)
}
"""

IDENTITY = """package task

const attentionKeyPrefix = "attention:"
"""

CACHE = """package exec

import "os"

func clear(path string) {
	_ = os.Remove(path)
}
"""

META = """package event

const MetaPrefix = "meta_"
"""


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def registry(**overrides):
    data = {
        "schema_version": 1,
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
        "reason": "fixture registry",
        "surfaces": [
            {
                "id": "memory.ledger.store",
                "kind": "kv-store",
                "owner": "memory/ledger",
                "files": ["memory/ledger/store.go"],
                "prefixes": [
                    {
                        "file": "memory/ledger/store.go",
                        "name": "keptPrefix",
                        "value": "tagent/kept/v1:",
                    }
                ],
                "reason": "fixture durable store",
                "review_by": FUTURE,
            },
            {
                "id": "runtime.task.identity",
                "kind": "fact-key",
                "owner": "runtime/task",
                "files": ["runtime/task/identity.go"],
                "prefixes": [
                    {
                        "file": "runtime/task/identity.go",
                        "name": "attentionKeyPrefix",
                        "value": "attention:",
                    }
                ],
                "reason": "fixture durable identity",
                "review_by": FUTURE,
            },
        ],
        "non_truth_files": [
            {"file": "tool/exec/cache.go", "reason": "fixture cache, not truth"}
        ],
        "non_truth_prefixes": [
            {
                "file": "api/event/meta.go",
                "name": "MetaPrefix",
                "value": "meta_",
                "reason": "fixture metadata marker",
            }
        ],
    }
    data.update(overrides)
    return data


def build_fixture(root, data=None):
    write(root, "docs/adr/guard.md", "# fixture ADR\n")
    write(root, "memory/ledger/store.go", STORE)
    write(root, "runtime/task/identity.go", IDENTITY)
    write(root, "tool/exec/cache.go", CACHE)
    write(root, "api/event/meta.go", META)
    write(
        root,
        "golden/runtime/durable_truth.json",
        json.dumps(registry() if data is None else data, indent=2) + "\n",
    )


def run_checker(root):
    command = [
        sys.executable,
        CHECKER,
        "--root",
        root,
        "--registry",
        "golden/runtime/durable_truth.json",
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


def case(name, data=None, setup=None, expected_rc=0, contains=(), excludes=()):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, data=data)
        if setup:
            setup(root)
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


def add_bypass_file(root):
    write(
        root,
        "memory/ledger/bypass.go",
        'package ledger\n\nimport "os"\n\nfunc Bypass(path string) error {\n'
        '\treturn os.WriteFile(path, nil, 0o644)\n}\n',
    )


def add_bypass_prefix(root):
    write(
        root,
        "memory/ledger/store.go",
        STORE.replace(
            'const keptPrefix = "tagent/kept/v1:"',
            'const keptPrefix = "tagent/kept/v1:"\n\nconst bypassPrefix = "tagent/bypass/v1:"',
        ),
    )


def drift_prefix(root):
    write(
        root,
        "memory/ledger/store.go",
        STORE.replace('tagent/kept/v1:', 'tagent/kept/v2:'),
    )


def remove_prefix(root):
    write(
        root,
        "memory/ledger/store.go",
        STORE.replace('const keptPrefix = "tagent/kept/v1:"\n\n', ""),
    )


def add_comments_and_strings(root):
    write(
        root,
        "memory/ledger/notes.go",
        'package ledger\n\n// os.WriteFile(path, nil, 0o644) is discussed here.\n'
        'var fixture = "os.WriteFile(path, nil, 0o644)"\n',
    )


def main():
    case("registered durable and non-truth surfaces pass")

    case(
        "an unregistered persistence file is rejected",
        setup=add_bypass_file,
        expected_rc=1,
        contains=["memory/ledger/bypass.go", "R3", "not in the durable truth registry"],
    )

    case(
        "an unregistered prefix in a registered file is rejected",
        setup=add_bypass_prefix,
        expected_rc=1,
        contains=["bypassPrefix", "tagent/bypass/v1:", "not registered"],
    )

    case(
        "a registered prefix whose value changes is rejected",
        setup=drift_prefix,
        expected_rc=1,
        contains=["keptPrefix", "value drift"],
    )

    case(
        "a registered prefix that disappears is rejected",
        setup=remove_prefix,
        expected_rc=1,
        contains=["keptPrefix", "no longer declared"],
    )

    case(
        "a comment or string mentioning a persistence API is not a write surface",
        setup=add_comments_and_strings,
        contains=["check_durable_truth_registry: ok"],
    )

    case(
        "a registry that is not an object is rejected",
        data=[],
        expected_rc=1,
        contains=["must be a JSON object"],
    )

    missing_review = registry()
    del missing_review["surfaces"][0]["review_by"]
    case(
        "a surface without a review deadline is rejected",
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

    bad_kind = registry()
    bad_kind["surfaces"][0]["kind"] = "mystery-store"
    case(
        "an unknown surface kind is rejected",
        data=bad_kind,
        expected_rc=1,
        contains=["mystery-store", "is not one of"],
    )

    duplicated = registry()
    duplicated["non_truth_files"].append(
        {"file": "memory/ledger/store.go", "reason": "duplicate"}
    )
    case(
        "a file registered twice is rejected",
        data=duplicated,
        expected_rc=1,
        contains=["already registered"],
    )

    if FAILURES:
        print("test_check_durable_truth_registry: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("test_check_durable_truth_registry: {} case(s) PASS".format(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
