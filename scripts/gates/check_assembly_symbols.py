#!/usr/bin/env python3
"""check_assembly_symbols.py - ADR 0006 R2: no silent feature path in assembly.

Rule: declarations in the composition root (assembly/) whose names have the
resolve<Feature>, validate<Feature> or <feature>Port shape are a feature-specific
resolution path: the assembly root is then parsing a feature's name instead of
mounting a generic plugin. Every such declaration is red unless it is registered
in golden/runtime/assembly_symbols.json with the landing review that accepted it.

The registry is a reviewed allowlist, not a learned baseline: --write-baseline
is deliberately absent. Adding a symbol means editing the registry (and carrying
the ADR 0006 metadata), which is exactly the visible act the rule asks for. A
registered symbol that disappears is stale and rejected too; the exception set
must describe the current tree.

Scope: production Go sources under assembly/ (tests excluded). Comments and
strings are masked before the scan, so a discussion of a symbol is not a
declaration.

Exit 0 when every declaration is registered, 1 on violations or unusable input.
"""
from __future__ import annotations

import argparse
import os
import re
import sys

import guard_checklib as lib

DEFAULT_REGISTRY = "golden/runtime/assembly_symbols.json"
REGISTRY_LABEL = "assembly symbol registry"
SCOPE_DIR = "assembly"

FUNC_RE = re.compile(
    r"^[ \t]*func[ \t]+(?:\([^)\n]*\)[ \t]*)?"
    r"((?:resolve|validate)[A-Z][A-Za-z0-9_]*)\b",
    re.M,
)
PORT_RE = re.compile(r"^[ \t]*type[ \t]+([A-Za-z_][A-Za-z0-9_]*Port)\b", re.M)


def load_registry(root, path):
    errors = []
    try:
        data = lib.load_json(path)
    except ValueError as exc:
        return None, [str(exc)]
    if not isinstance(data, dict):
        lib.validate_guard_metadata(data, root, REGISTRY_LABEL, errors)
        return None, errors
    lib.validate_guard_metadata(data, root, REGISTRY_LABEL, errors)

    symbols = data.get("symbols")
    if not isinstance(symbols, list):
        errors.append("{}: 'symbols' must be a list".format(REGISTRY_LABEL))
        symbols = []
    registered = {}
    for index, entry in enumerate(symbols):
        label = "{} symbol[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        name = lib.require_field(entry, "name", label, errors, str)
        path_value = lib.require_field(entry, "file", label, errors, str)
        lib.check_review_by(entry.get("review_by"), label, errors)
        if name is None or path_value is None:
            continue
        if not path_value.startswith(SCOPE_DIR + "/"):
            errors.append(
                "{}: {!r} is outside the {}/ scope".format(label, path_value, SCOPE_DIR)
            )
        if not os.path.isfile(os.path.join(root, path_value)):
            errors.append("{}: file {!r} does not exist".format(label, path_value))
        key = (path_value, name)
        if key in registered:
            errors.append(
                "{}: duplicate registration {!r} in {}".format(REGISTRY_LABEL, name, path_value)
            )
            continue
        registered[key] = entry
    return registered, errors


def declarations(root):
    found = []
    for path in lib.go_files(root, [SCOPE_DIR]):
        rel = lib.rel_path(root, path)
        text = lib.read_text(path)
        code = lib.strip_go_comments(text)
        masked = lib.scan_go_strings(code)
        for match in FUNC_RE.finditer(masked):
            found.append((rel, lib.line_of(masked, match.start(1)), match.group(1)))
        for match in PORT_RE.finditer(masked):
            found.append((rel, lib.line_of(masked, match.start(1)), match.group(1)))
    return sorted(found, key=lambda item: (item[0], item[1], item[2]))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--registry", default=DEFAULT_REGISTRY)
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    registry_path = (
        args.registry if os.path.isabs(args.registry) else os.path.join(root, args.registry)
    )
    registered, errors = load_registry(root, registry_path)
    if registered is None:
        for error in errors:
            print("ERROR: " + error)
        print("check_assembly_symbols: registry unusable")
        return 1

    found = declarations(root)
    seen = set()
    for rel, lineno, name in found:
        key = (rel, name)
        if key in registered:
            seen.add(key)
            continue
        errors.append(
            "{}:{}: R2 feature-specific assembly symbol {!r} is not registered; "
            "add a reviewed exception or remove the dedicated path".format(
                rel, lineno, name
            )
        )

    for key in sorted(registered):
        if key not in seen:
            errors.append(
                "R2 registered symbol {!r} in {} no longer exists; remove the stale "
                "exception".format(key[1], key[0])
            )

    for error in errors:
        print("ERROR: " + error)
    print(
        "check_assembly_symbols: go_files={} declarations={} registered={} errors={}".format(
            len(lib.go_files(root, [SCOPE_DIR])), len(found), len(registered), len(errors)
        )
    )
    if errors:
        print("check_assembly_symbols: {} error(s)".format(len(errors)))
        return 1
    print("check_assembly_symbols: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
