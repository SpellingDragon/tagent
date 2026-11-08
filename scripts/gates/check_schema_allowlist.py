#!/usr/bin/env python3
"""check_schema_allowlist.py - ADR 0006 R1: agent schema sections are allowlisted.

Rule: an agent declaration may use only the generic agent sections every host
shares (model references, tools, memory, file, exec, the generic plugins mount,
and the model parameters). A feature-specific section such as 'meditation:' is
not a generic mount: it is red unless a reviewed exception is registered in
golden/runtime/schema_allowlist.json.

The existing 'meditation' section is registered once as grandfather debt for the
landing of this guard; it is not a precedent. Every exception entry must carry
its reason, the ADR that argued it, the user ruling that approved it, and a
review deadline. A missing one of those is red (the ADR 0006 breakthrough
protocol: ADR + user ruling + registry entry, all three or red).

Scope: every YAML/YML document under the repository with a top-level 'agents:'
block, excluding generated/openspec/vendor trees. The indentation parser is the
one the earlier feature-boundary gate carries; this checker only owns the
allowlist verdict.

Exit 0 when every selected document is clean, 1 on violations or unusable
input.
"""
from __future__ import annotations

import argparse
import os
import re
import sys

import check_feature_boundary as boundary
import guard_checklib as lib

DEFAULT_REGISTRY = "golden/runtime/schema_allowlist.json"
REGISTRY_LABEL = "schema allowlist"
SKIP_DIRS = {
    ".git",
    ".tmp",
    ".tmp-migrate",
    ".pre-isolation-backup",
    ".evidence-tmp-check",
    ".tagent",
    ".tagent-workspace",
    ".qoder",
    "__pycache__",
    "node_modules",
    "vendor",
    "openspec",
    "knowledge_base",
    "testdata",
    "skills",
}
AGENTS_LINE_RE = re.compile(r"^agents\s*:")


def discover_documents(root):
    """Every YAML document that declares a top-level agents map."""
    found = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(
            d for d in dirnames if d not in SKIP_DIRS and not d.startswith(".tmp")
        )
        for filename in sorted(filenames):
            if not filename.endswith((".yaml", ".yml")):
                continue
            path = os.path.join(dirpath, filename)
            try:
                text = lib.read_text(path)
            except (OSError, UnicodeError):
                continue
            if any(AGENTS_LINE_RE.match(line) for line in text.splitlines()):
                found.append(path)
    return sorted(found)


def agent_keys(root, path):
    """All agent-level keys, before the allowlist filter is applied."""
    rel = lib.rel_path(root, path)
    try:
        text = lib.read_text(path)
    except (OSError, UnicodeError) as exc:
        return [], ["{}: R1 host document is unreadable: {}".format(rel, exc)]
    lines = text.splitlines()
    found = []
    for idx, line in enumerate(lines):
        if boundary._blank_or_comment(line) or boundary._indent(line) != 0:
            continue
        match = AGENTS_LINE_RE.match(line)
        if not match:
            continue
        rest = boundary._strip_yaml_comment(line.split(":", 1)[1]).strip()
        block_end = boundary._block_end(lines, idx)
        found.extend(
            boundary._agent_section_violations(rel, lines, idx, block_end, rest, frozenset())
        )
    return found, []


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

    generic = data.get("generic_keys")
    if not isinstance(generic, list) or not generic:
        errors.append("{}: 'generic_keys' must be a non-empty list".format(REGISTRY_LABEL))
        generic = []
    else:
        seen = set()
        for key in generic:
            if not isinstance(key, str) or not key.strip():
                errors.append(
                    "{}: every generic key must be a non-empty string".format(
                        REGISTRY_LABEL
                    )
                )
                continue
            if key in seen:
                errors.append(
                    "{}: duplicate generic key {!r}".format(REGISTRY_LABEL, key)
                )
            seen.add(key)

    exceptions = data.get("exceptions")
    if not isinstance(exceptions, list):
        errors.append("{}: 'exceptions' must be a list".format(REGISTRY_LABEL))
        exceptions = []
    exception_keys = {}
    for index, entry in enumerate(exceptions):
        label = "{} exception[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        key = lib.require_field(entry, "key", label, errors, str)
        if key is None:
            continue
        if key in exception_keys:
            errors.append("{}: duplicate exception {!r}".format(REGISTRY_LABEL, key))
            continue
        if key in generic:
            errors.append(
                "{}: {!r} is both a generic key and an exception".format(
                    REGISTRY_LABEL, key
                )
            )
        exception_keys[key] = entry
        lib.validate_exception_metadata(entry, root, label, errors)

    return {
        "generic": set(generic),
        "exceptions": exception_keys,
    }, errors


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--registry", default=DEFAULT_REGISTRY)
    parser.add_argument(
        "--document",
        action="append",
        help="explicit host YAML path (repeatable); default discovers documents",
    )
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    registry_path = (
        args.registry if os.path.isabs(args.registry) else os.path.join(root, args.registry)
    )
    registry, errors = load_registry(root, registry_path)
    if registry is None:
        for error in errors:
            print("ERROR: " + error)
        print("check_schema_allowlist: registry unusable")
        return 1

    if args.document:
        documents = []
        for item in args.document:
            path = item if os.path.isabs(item) else os.path.join(root, item)
            if not os.path.isfile(path):
                errors.append("R1 document does not exist: {}".format(item))
            else:
                documents.append(path)
    else:
        documents = discover_documents(root)
    if not documents and not errors:
        errors.append(
            "R1 no host document with a top-level 'agents:' block was found; "
            "the rule has no object"
        )

    seen_exceptions = set()
    for path in documents:
        violations, read_errors = agent_keys(root, path)
        errors.extend(read_errors)
        for rel, lineno, key, agent in violations:
            if key in registry["generic"]:
                continue
            if key in registry["exceptions"]:
                seen_exceptions.add(key)
                continue
            errors.append(
                "{}:{}: R1 feature-specific schema section {!r} under agents.{} is "
                "not a generic section and has no reviewed exception".format(
                    rel, lineno, key, agent
                )
            )

    for key in sorted(registry["exceptions"]):
        if key not in seen_exceptions:
            errors.append(
                "R1 exception {!r} is registered but no host document declares it; "
                "remove the stale exception".format(key)
            )

    for error in errors:
        print("ERROR: " + error)
    print(
        "check_schema_allowlist: host_documents={} generic_keys={} exceptions={} errors={}".format(
            len(documents), len(registry["generic"]), len(registry["exceptions"]), len(errors)
        )
    )
    if errors:
        print("check_schema_allowlist: {} error(s)".format(len(errors)))
        return 1
    print("check_schema_allowlist: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
