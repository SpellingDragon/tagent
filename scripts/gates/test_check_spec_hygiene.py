#!/usr/bin/env python3
"""Selftests for check_spec_hygiene.py - tempfile fixtures, subprocess only.

Each case proves a direction: requirement prose is judged, scenario text is not,
and the reviewed baseline grandfathers existing lines without granting a new
violation or silently reviving a fixed one.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_spec_hygiene.py")
FAILURES = []
CASES = 0

ADR = "docs/adr/guard.md"
FUTURE = "2999-12-31"

CLEAN_SPEC = """# alpha Specification

## Purpose

A fixture purpose long enough to be a real main spec.

## Requirements

### Requirement: clean requirement

The system SHALL do the right thing.

#### Scenario: a scenario may name an implementation

- **WHEN** `foo.go` calls `Call()` with 42
- **THEN** the scenario is evidence, not requirement prose
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
        "reason": "fixture baseline",
        "spec_glob": "openspec/specs/*/spec.md",
        "exceptions": [],
    }
    data.update(overrides)
    return data


def build_fixture(root, spec=CLEAN_SPEC, data=None):
    write(root, "docs/adr/guard.md", "# fixture ADR\n")
    write(root, "openspec/specs/alpha/spec.md", spec)
    write(
        root,
        "golden/runtime/spec_hygiene.json",
        json.dumps(registry() if data is None else data, indent=2) + "\n",
    )


def run_checker(root, extra=None):
    command = [
        sys.executable,
        CHECKER,
        "--root",
        root,
        "--registry",
        "golden/runtime/spec_hygiene.json",
    ]
    if extra:
        command.extend(extra)
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


def case(name, spec=CLEAN_SPEC, data=None, expected_rc=0, contains=(), excludes=()):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, spec=spec, data=data)
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


def prose(line):
    return CLEAN_SPEC.replace(
        "The system SHALL do the right thing.",
        "The system SHALL do the right thing.\n" + line,
    )


def main():
    case("clean requirement prose and a dirty scenario pass")

    case(
        "a .go file name in requirement prose is rejected",
        spec=prose("The resolver lives in host.go."),
        expected_rc=1,
        contains=["go-file", "host.go", "R4"],
    )

    case(
        "a function-call shape in requirement prose is rejected",
        spec=prose("The resolver SHALL call ResolveThing() first."),
        expected_rc=1,
        contains=["function-call", "ResolveThing()", "R4"],
    )

    case(
        "a numeric constant in requirement prose is rejected",
        spec=prose("The queue SHALL hold at most 10000 entries."),
        expected_rc=1,
        contains=["numeric-constant", "10000", "R4"],
    )

    case(
        "a numeric label, a version tag and spaced prose are not call shapes",
        spec=prose("Layers L1 and v1.2.3 are named; a function (see below) is prose."),
        contains=["check_spec_hygiene: ok"],
    )

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, spec=prose("The resolver SHALL call ResolveThing() first."))
        rc, output = run_checker(root, ["--write-baseline"])
        problems = []
        if rc != 0:
            problems.append("--write-baseline exit {} != 0".format(rc))
        check_rc, check_output = run_checker(root)
        if check_rc != 0:
            problems.append("check after baseline exit {} != 0".format(check_rc))
        report("a grandfathered line passes after --write-baseline", problems, output + check_output)

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, spec=prose("The resolver SHALL call ResolveThing() first."))
        run_checker(root, ["--write-baseline"])
        write(
            root,
            "openspec/specs/alpha/spec.md",
            prose("The resolver SHALL call ResolveThing() first.\nThe resolver SHALL call ResolveOther() too."),
        )
        rc, output = run_checker(root)
        problems = []
        if rc != 1:
            problems.append("exit {} != 1".format(rc))
        if "ResolveOther()" not in output:
            problems.append("missing the new call")
        report("a new line beside a grandfathered line is rejected", problems, output)

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, spec=prose("The resolver SHALL call ResolveThing() first."))
        run_checker(root, ["--write-baseline"])
        write(root, "openspec/specs/alpha/spec.md", CLEAN_SPEC)
        rc, output = run_checker(root)
        problems = []
        if rc != 0:
            problems.append("a fixed line must not block the gate: exit {} != 0".format(rc))
        if "stale=1" not in output:
            problems.append("missing stale=1")
        report("fixing a baseline line reports stale without failing", problems, output)

    case(
        "a registry that is not an object is rejected",
        data=[],
        expected_rc=1,
        contains=["must be a JSON object"],
    )

    missing = registry()
    del missing["adr"]
    case(
        "a baseline without its ADR metadata is rejected",
        data=missing,
        expected_rc=1,
        contains=["missing adr"],
    )

    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, spec=prose("The resolver lives in host.go."))
        os.remove(os.path.join(root, "openspec/specs/alpha/spec.md"))
        rc, output = run_checker(root)
        problems = []
        if rc != 1:
            problems.append("exit {} != 1".format(rc))
        if "no main spec matched" not in output:
            problems.append("missing 'no main spec matched'")
        report("a missing main-spec corpus is red", problems, output)

    if FAILURES:
        print("test_check_spec_hygiene: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("test_check_spec_hygiene: {} case(s) PASS".format(CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
