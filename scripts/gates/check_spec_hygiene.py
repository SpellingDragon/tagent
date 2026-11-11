#!/usr/bin/env python3
"""check_spec_hygiene.py - ADR 0006 R4: main-spec requirements keep invariants.

Rule: the requirement prose of a main spec (openspec/specs/<capability>/spec.md)
states what must hold, not how it is implemented. A requirement line containing
a .go file name, a function-call shape, or a numeric constant is red: those are
mechanism details that belong in the implementation document, where they can
change without a spec operation.

Only requirement prose is scanned. Scenario blocks are acceptance examples and
may name a test or a command; they are deliberately outside this rule. The
scan stops at the first #### heading of a requirement.

Existing violations are grandfathered once in golden/runtime/spec_hygiene.json,
which carries the ADR 0006 metadata (ADR, user ruling, reason and review
deadline). The baseline stores one fingerprint per violating line and its
occurrence count, so adding a new violation, or repeating a grandfathered one,
is red. A line that is fixed leaves a stale entry; that is reported as stale,
not as an error, so a fix is never blocked waiting for a baseline edit. Run
--write-baseline after a reviewed tightening to remove stale entries.

Exit 0 when no new violation is present, 1 on violations or unusable input.
"""
from __future__ import annotations

import argparse
import glob
import hashlib
import json
import os
import re
import sys

import guard_checklib as lib

DEFAULT_REGISTRY = "golden/runtime/spec_hygiene.json"
REGISTRY_LABEL = "spec hygiene baseline"
DEFAULT_SPEC_GLOB = "openspec/specs/*/spec.md"
RULE_NAMES = ("go-file", "function-call", "numeric-constant")

RULES = {
    "go-file": re.compile(r"\b[A-Za-z0-9_][A-Za-z0-9_./-]*\.go\b"),
    "function-call": re.compile(r"\b[A-Za-z_][A-Za-z0-9_.]*\([^()\n]*\)"),
    "numeric-constant": re.compile(
        r"(?<![\w.])[-+]?\d+(?:\.\d+)*(?:[A-Za-z%]+)?(?![.\w])"
    ),
}


def parse_requirements(path):
    """Yield (title, [(lineno, text), ...]) for each requirement's prose."""
    lines = lib.read_text(path).splitlines()
    title = None
    prose = []
    in_prose = False
    for lineno, line in enumerate(lines, 1):
        if line.startswith("### Requirement:"):
            if title is not None:
                yield title, prose
            title = line[len("### Requirement:"):].strip()
            prose = []
            in_prose = True
            continue
        if title is None:
            continue
        if line.startswith("#### ") or line.startswith("### ") or line.startswith("## "):
            yield title, prose
            title = None
            prose = []
            in_prose = False
            continue
        if in_prose:
            prose.append((lineno, line))
    if title is not None:
        yield title, prose


def scan_specs(root, spec_glob):
    specs = sorted(glob.glob(os.path.join(root, spec_glob)))
    violations = {}
    requirement_count = 0
    for path in specs:
        rel = lib.rel_path(root, path)
        for title, prose in parse_requirements(path):
            requirement_count += 1
            for lineno, line in prose:
                text = line.strip()
                if not text:
                    continue
                digest = hashlib.sha256(text.encode("utf-8")).hexdigest()
                for rule, pattern in RULES.items():
                    if not pattern.search(text):
                        continue
                    key = (rel, title, rule, digest)
                    entry = violations.setdefault(
                        key,
                        {
                            "count": 0,
                            "lines": [],
                            "preview": text[:160],
                            "rule": rule,
                            "requirement": title,
                        },
                    )
                    entry["count"] += 1
                    entry["lines"].append(lineno)
    return specs, requirement_count, violations


def load_registry(root, path):
    errors = []
    try:
        data = lib.load_json(path)
    except ValueError as exc:
        return None, None, [str(exc)]
    if not isinstance(data, dict):
        lib.validate_guard_metadata(data, root, REGISTRY_LABEL, errors)
        return None, None, errors
    lib.validate_guard_metadata(data, root, REGISTRY_LABEL, errors)

    spec_glob = data.get("spec_glob", DEFAULT_SPEC_GLOB)
    if not isinstance(spec_glob, str) or not spec_glob.strip():
        errors.append("{}: 'spec_glob' must be a non-empty string".format(REGISTRY_LABEL))
        spec_glob = DEFAULT_SPEC_GLOB

    exceptions = data.get("exceptions")
    if not isinstance(exceptions, list):
        errors.append("{}: 'exceptions' must be a list".format(REGISTRY_LABEL))
        exceptions = []
    baseline = {}
    for index, entry in enumerate(exceptions):
        label = "{} exception[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        file_value = lib.require_field(entry, "file", label, errors, str)
        requirement = lib.require_field(entry, "requirement", label, errors, str)
        rule = lib.require_field(entry, "rule", label, errors, str)
        digest = lib.require_field(entry, "text_sha256", label, errors, str)
        count = entry.get("count")
        if not isinstance(count, int) or isinstance(count, bool) or count < 1:
            errors.append("{}: 'count' must be a positive integer".format(label))
            count = 1
        if rule is not None and rule not in RULE_NAMES:
            errors.append(
                "{}: rule {!r} is not one of {}".format(label, rule, ", ".join(RULE_NAMES))
            )
        if digest is not None and not re.fullmatch(r"[0-9a-f]{64}", digest):
            errors.append("{}: 'text_sha256' must be a lowercase sha256".format(label))
        if file_value is None or requirement is None or rule is None or digest is None:
            continue
        key = (file_value, requirement, rule, digest)
        if key in baseline:
            errors.append(
                "{}: duplicate baseline entry for {}:{}".format(label, file_value, rule)
            )
            continue
        baseline[key] = entry
    return data, {"spec_glob": spec_glob, "exceptions": baseline}, errors


def write_baseline(path, data, spec_glob, violations):
    entries = []
    for key in sorted(violations):
        file_value, requirement, rule, digest = key
        entry = violations[key]
        entries.append(
            {
                "file": file_value,
                "requirement": requirement,
                "rule": rule,
                "text_sha256": digest,
                "preview": entry["preview"],
                "count": entry["count"],
            }
        )
    data = dict(data or {})
    data["spec_glob"] = spec_glob
    data["exceptions"] = entries
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(data, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    return len(entries)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--registry", default=DEFAULT_REGISTRY)
    parser.add_argument(
        "--write-baseline",
        action="store_true",
        help="record the current requirement violations as the reviewed baseline",
    )
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    registry_path = (
        args.registry if os.path.isabs(args.registry) else os.path.join(root, args.registry)
    )
    data, registry, errors = load_registry(root, registry_path)

    if args.write_baseline:
        if data is None or errors:
            for error in errors:
                print("ERROR: " + error)
            print(
                "check_spec_hygiene: registry metadata is unusable; fix it before "
                "--write-baseline"
            )
            return 1
        spec_glob = (registry or {}).get("spec_glob", DEFAULT_SPEC_GLOB)
        specs, _, violations = scan_specs(root, spec_glob)
        if not specs:
            for error in errors:
                print("ERROR: " + error)
            print("check_spec_hygiene: no spec matched {!r}".format(spec_glob))
            return 1
        written = write_baseline(registry_path, data, spec_glob, violations)
        print("check_spec_hygiene: wrote {} baseline exception(s)".format(written))
        return 0

    if registry is None:
        for error in errors:
            print("ERROR: " + error)
        print("check_spec_hygiene: registry unusable")
        return 1

    specs, requirement_count, violations = scan_specs(root, registry["spec_glob"])
    if not specs:
        errors.append(
            "R4 no main spec matched {!r}; the rule has no object".format(
                registry["spec_glob"]
            )
        )

    stale = 0
    for key, entry in registry["exceptions"].items():
        file_value, requirement, rule, digest = key
        current = violations.get(key)
        if current is None:
            stale += 1
            continue
        if current["count"] > entry.get("count", 1):
            errors.append(
                "{}:{}: R4 requirement {!r} repeats a grandfathered {} line "
                "{} time(s); the baseline allows {}".format(
                    file_value,
                    current["lines"][0],
                    requirement,
                    rule,
                    current["count"],
                    entry.get("count", 1),
                )
            )
        elif current["count"] < entry.get("count", 1):
            stale += 1

    for key in sorted(violations):
        if key in registry["exceptions"]:
            continue
        file_value, requirement, rule, _ = key
        entry = violations[key]
        errors.append(
            "{}:{}: R4 {} in requirement {!r}: {}".format(
                file_value, entry["lines"][0], rule, requirement, entry["preview"]
            )
        )

    for error in errors:
        print("ERROR: " + error)
    print(
        "check_spec_hygiene: specs={} requirements={} violations={} baseline={} "
        "stale={} errors={}".format(
            len(specs),
            requirement_count,
            len(violations),
            len(registry["exceptions"]),
            stale,
            len(errors),
        )
    )
    if errors:
        print("check_spec_hygiene: {} error(s)".format(len(errors)))
        return 1
    print("check_spec_hygiene: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
