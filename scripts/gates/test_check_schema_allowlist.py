#!/usr/bin/env python3
"""Selftests for check_schema_allowlist.py - tempfile fixtures, subprocess only.

Each case proves a direction: a generic-only declaration is green, a new
feature-specific section is red, and a registered grandfather exception is green
only while its ADR/ruling/review metadata is complete.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_schema_allowlist.py")
FAILURES = []
CASES = 0

ADR = "docs/adr/guard.md"
FUTURE = "2999-12-31"

CLEAN_HOST = """provider: fixture
agents:
  alpha:
    provider: fixture
    model: fixture
    plugins:
      - { id: audit }
    meditation:
      enabled: true
"""


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def exception(**overrides):
    entry = {
        "key": "meditation",
        "reason": "grandfathered host section pending the generic plugins mount",
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
    }
    entry.update(overrides)
    return entry


def registry(exceptions=None, generic=None):
    return {
        "schema_version": 1,
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
        "reason": "fixture registry",
        "generic_keys": generic or ["provider", "model", "plugins"],
        "exceptions": [exception()] if exceptions is None else exceptions,
    }


def build_fixture(root, host=None, data=None):
    write(root, "docs/adr/guard.md", "# fixture ADR\n")
    write(root, "host.yaml", CLEAN_HOST if host is None else host)
    write(root, "golden/runtime/schema_allowlist.json",
          json.dumps(registry() if data is None else data, indent=2) + "\n")


def run_checker(root):
    command = [
        sys.executable,
        CHECKER,
        "--root",
        root,
        "--registry",
        "golden/runtime/schema_allowlist.json",
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


def case(name, host=None, data=None, expected_rc=0, contains=(), excludes=()):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, host=host, data=data)
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
    case("generic sections plus the registered exception pass")

    bad = CLEAN_HOST.replace(
        "    plugins:\n      - { id: audit }\n",
        "    plugins:\n      - { id: audit }\n    fancy_feature:\n      enabled: true\n",
    )
    case(
        "a new feature-specific section is rejected",
        host=bad,
        expected_rc=1,
        contains=["fancy_feature", "R1", "no reviewed exception"],
    )

    case(
        "flow-style feature-specific section is rejected",
        host="agents: {a: {fancy_feature: {}}}\n",
        expected_rc=1,
        contains=["fancy_feature", "R1"],
    )

    case(
        "meditation without a registered exception is rejected",
        data=registry(exceptions=[]),
        expected_rc=1,
        contains=["meditation", "R1"],
    )

    case(
        "a registry that is not an object is rejected",
        data=[],
        expected_rc=1,
        contains=["must be a JSON object"],
    )

    missing_review = exception()
    del missing_review["review_by"]
    case(
        "an exception without a review deadline is rejected",
        data=registry(exceptions=[missing_review]),
        expected_rc=1,
        contains=["missing review_by"],
    )

    case(
        "an expired exception review deadline is rejected",
        data=registry(exceptions=[exception(review_by="2000-01-01")]),
        expected_rc=1,
        contains=["has passed"],
    )

    case(
        "an exception without an existing ADR is rejected",
        data=registry(exceptions=[exception(adr="docs/adr/missing.md")]),
        expected_rc=1,
        contains=["does not exist"],
    )

    case(
        "an unused exception is rejected",
        host="agents:\n  alpha:\n    model: fixture\n",
        data=registry(exceptions=[exception()]),
        expected_rc=1,
        contains=["no host document declares it"],
    )

    case(
        "a commented feature section is not a declaration",
        host="agents:\n  alpha:\n    model: fixture\n    # fancy_feature:\n    #   enabled: true\n",
        data=registry(exceptions=[]),
        contains=["check_schema_allowlist: ok"],
    )

    case(
        "a generic section must be named in the registry allowlist",
        host=CLEAN_HOST,
        data=registry(exceptions=[exception()], generic=["provider", "model"]),
        expected_rc=1,
        contains=["plugins", "no reviewed exception"],
    )

    case(
        "list-style agents reach the same verdict for a registered exception",
        host="agents:\n  - id: alpha\n    model: fixture\n    meditation:\n      enabled: true\n",
        data=registry(exceptions=[exception()], generic=["provider", "model", "plugins", "id"]),
        contains=["check_schema_allowlist: ok"],
    )

    case(
        "list-style agents name an unregistered feature section",
        host="agents:\n  - id: alpha\n    model: fixture\n    fancy_feature:\n      enabled: true\n",
        data=registry(exceptions=[exception()], generic=["provider", "model", "plugins", "id"]),
        expected_rc=1,
        contains=["fancy_feature", "R1", "under agents.alpha"],
        excludes=["'id'"],
    )

    if FAILURES:
        print("test_check_schema_allowlist: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("test_check_schema_allowlist: {} case(s) PASS".format(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
