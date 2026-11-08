#!/usr/bin/env python3
"""check_durable_truth_registry.py - ADR 0006 R3: persistent truth is registered.

Rule: every persistent truth surface in product code must be registered in
golden/runtime/durable_truth.json. A surface is either a file that writes
through a persistence API (os.WriteFile/Create/OpenFile/MkdirAll/Rename/Remove/
RemoveAll, kvWrite or kvRead) or a package-level *Prefix constant that mints a
durable key namespace. An unregistered write surface or prefix is a bypass
ledger/second truth and is red.

The registry has three parts:

  surfaces           durable truth owners; each names its files, the prefix
                     constants it mints (name + value + file), and why it is a
                     truth surface;
  non_truth_files    files that persist only ephemeral/cache/workspace data,
                     with the reason they are not truth;
  non_truth_prefixes prefix constants that name identities or presentation
                     markers rather than a store.

Each entry carries a reason. The registry as a whole carries the ADR, the user
ruling and a review deadline; a new surface cannot appear silently because the
checker has no auto-write mode. Registered prefixes are compared against their
source value, so renaming a value cannot reuse an old registration.

Scope: production Go sources under the product packages (assembly, api, cli,
features, internal, memory, runtime, tool, bench); tests and the example
modules are excluded. The scan is static: an adapter that reaches storage only
through an injected, unnamed interface is outside its reach and is covered by
review, not by this gate.

Exit 0 when every persistence file and prefix is registered, 1 on violations or
unusable input.
"""
from __future__ import annotations

import argparse
import os
import re
import sys

import guard_checklib as lib

DEFAULT_REGISTRY = "golden/runtime/durable_truth.json"
REGISTRY_LABEL = "durable truth registry"
PRODUCT_DIRS = ("assembly", "api", "cli", "features", "internal", "memory", "runtime", "tool", "bench")
ALLOWED_KINDS = {"filesystem-store", "kv-store", "external-kv", "fact-key", "ledger"}

PERSISTENCE_RE = re.compile(
    r"\bos\.(?:WriteFile|Create|OpenFile|MkdirAll|Rename|Remove|RemoveAll)\s*\("
    r"|\bkvWrite\s*\("
    r"|\bkvRead\s*\("
)
PREFIX_DECL_RE = re.compile(
    r'^[ \t]*(?:const[ \t]+|var[ \t]+)?'
    r'([A-Za-z_][A-Za-z0-9_]*[Pp]refix)[ \t]*(?:string[ \t]*)?(?:=|:=)[ \t]*"([^"]*)"',
    re.M,
)


def is_product_go_file(root, value):
    if not isinstance(value, str) or not value.strip():
        return False
    clean = value.replace(os.sep, "/")
    if os.path.isabs(clean) or clean.startswith("../") or "/../" in clean:
        return False
    if not clean.endswith(".go") or clean.endswith("_test.go"):
        return False
    if not any(clean.startswith(name + "/") for name in PRODUCT_DIRS):
        return False
    return os.path.isfile(os.path.join(root, clean))


def scan_tree(root):
    files = lib.go_files(root, PRODUCT_DIRS)
    persistence_files = set()
    prefixes = []
    for path in files:
        rel = lib.rel_path(root, path)
        text = lib.read_text(path)
        code = lib.strip_go_comments(text)
        masked = lib.scan_go_strings(code)
        if PERSISTENCE_RE.search(masked):
            persistence_files.add(rel)
        for match in PREFIX_DECL_RE.finditer(code):
            prefixes.append(
                (
                    rel,
                    lib.line_of(code, match.start(1)),
                    match.group(1),
                    match.group(2),
                )
            )
    return files, persistence_files, prefixes


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

    surfaces = data.get("surfaces")
    if not isinstance(surfaces, list):
        errors.append("{}: 'surfaces' must be a list".format(REGISTRY_LABEL))
        surfaces = []
    non_truth_files = data.get("non_truth_files")
    if not isinstance(non_truth_files, list):
        errors.append("{}: 'non_truth_files' must be a list".format(REGISTRY_LABEL))
        non_truth_files = []
    non_truth_prefixes = data.get("non_truth_prefixes")
    if not isinstance(non_truth_prefixes, list):
        errors.append("{}: 'non_truth_prefixes' must be a list".format(REGISTRY_LABEL))
        non_truth_prefixes = []

    registered_files = {}
    registered_prefixes = {}
    surface_ids = set()

    def add_prefix(label, file_value, name, value, owner):
        if not isinstance(file_value, str) or not file_value.strip():
            errors.append("{}: missing file".format(label))
            return
        clean = file_value.replace(os.sep, "/")
        if not is_product_go_file(root, clean):
            errors.append(
                "{}: file {!r} is not a production Go file under the product packages".format(
                    label, file_value
                )
            )
            return
        if not isinstance(name, str) or not name.strip():
            errors.append("{}: missing prefix name".format(label))
            return
        if not isinstance(value, str):
            errors.append("{}: prefix value must be a string".format(label))
            return
        key = (clean, name)
        if key in registered_prefixes:
            errors.append(
                "{}: prefix {!r} in {} is registered twice".format(
                    REGISTRY_LABEL, name, clean
                )
            )
            return
        registered_prefixes[key] = {
            "value": value,
            "owner": owner,
            "label": label,
        }

    for index, entry in enumerate(surfaces):
        label = "{} surface[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        surface_id = lib.require_field(entry, "id", label, errors, str)
        if surface_id is not None:
            if surface_id in surface_ids:
                errors.append(
                    "{}: duplicate surface id {!r}".format(REGISTRY_LABEL, surface_id)
                )
            surface_ids.add(surface_id)
        kind = lib.require_field(entry, "kind", label, errors, str)
        if kind is not None and kind not in ALLOWED_KINDS:
            errors.append(
                "{}: kind {!r} is not one of {}".format(
                    label, kind, ", ".join(sorted(ALLOWED_KINDS))
                )
            )
        lib.require_field(entry, "owner", label, errors, str)
        lib.require_field(entry, "reason", label, errors, str)
        lib.check_review_by(entry.get("review_by"), label, errors)

        files = entry.get("files")
        if not isinstance(files, list) or not files:
            errors.append("{}: 'files' must be a non-empty list".format(label))
            files = []
        surface_files = set()
        for file_value in files:
            if not isinstance(file_value, str) or not file_value.strip():
                errors.append("{}: every file must be a non-empty string".format(label))
                continue
            clean = file_value.replace(os.sep, "/")
            if not is_product_go_file(root, clean):
                errors.append(
                    "{}: file {!r} is not a production Go file under the product packages".format(
                        label, file_value
                    )
                )
                continue
            if clean in registered_files:
                errors.append(
                    "{}: file {!r} is already registered as {}".format(
                        label, clean, registered_files[clean]["owner"]
                    )
                )
                continue
            surface_files.add(clean)
            registered_files[clean] = {
                "owner": surface_id or label,
                "kind": "surface",
                "reason": entry.get("reason"),
            }

        prefixes = entry.get("prefixes", [])
        if not isinstance(prefixes, list):
            errors.append("{}: 'prefixes' must be a list".format(label))
            prefixes = []
        for pindex, prefix_entry in enumerate(prefixes):
            plabel = "{} prefix[{}]".format(label, pindex)
            if not isinstance(prefix_entry, dict):
                errors.append("{}: must be an object".format(plabel))
                continue
            file_value = lib.require_field(prefix_entry, "file", plabel, errors, str)
            name = lib.require_field(prefix_entry, "name", plabel, errors, str)
            value = lib.require_field(prefix_entry, "value", plabel, errors, str)
            if file_value is not None:
                clean = file_value.replace(os.sep, "/")
                if clean not in surface_files:
                    errors.append(
                        "{}: prefix file {!r} is not one of the surface's files".format(
                            plabel, file_value
                        )
                    )
            if file_value is not None and name is not None and value is not None:
                add_prefix(plabel, file_value, name, value, surface_id or label)

    for index, entry in enumerate(non_truth_files):
        label = "{} non_truth_files[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        file_value = lib.require_field(entry, "file", label, errors, str)
        lib.require_field(entry, "reason", label, errors, str)
        if "review_by" in entry:
            lib.check_review_by(entry.get("review_by"), label, errors)
        if file_value is None:
            continue
        clean = file_value.replace(os.sep, "/")
        if not is_product_go_file(root, clean):
            errors.append(
                "{}: file {!r} is not a production Go file under the product packages".format(
                    label, file_value
                )
            )
            continue
        if clean in registered_files:
            errors.append(
                "{}: file {!r} is already registered as {}".format(
                    label, clean, registered_files[clean]["owner"]
                )
            )
            continue
        registered_files[clean] = {
            "owner": "non-truth",
            "kind": "non-truth",
            "reason": entry.get("reason"),
        }

    for index, entry in enumerate(non_truth_prefixes):
        label = "{} non_truth_prefixes[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        file_value = lib.require_field(entry, "file", label, errors, str)
        name = lib.require_field(entry, "name", label, errors, str)
        value = lib.require_field(entry, "value", label, errors, str)
        lib.require_field(entry, "reason", label, errors, str)
        if "review_by" in entry:
            lib.check_review_by(entry.get("review_by"), label, errors)
        if file_value is not None and name is not None and value is not None:
            add_prefix(label, file_value, name, value, "non-truth")

    return {
        "surfaces": surfaces,
        "files": registered_files,
        "prefixes": registered_prefixes,
    }, errors


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--registry", default=DEFAULT_REGISTRY)
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    registry_path = (
        args.registry if os.path.isabs(args.registry) else os.path.join(root, args.registry)
    )
    registry, errors = load_registry(root, registry_path)
    if registry is None:
        for error in errors:
            print("ERROR: " + error)
        print("check_durable_truth_registry: registry unusable")
        return 1

    go_files, persistence_files, prefixes = scan_tree(root)

    for rel in sorted(persistence_files):
        if rel not in registry["files"]:
            errors.append(
                "{}: R3 persistence write surface is not in the durable truth registry".format(
                    rel
                )
            )

    seen_prefixes = set()
    for rel, lineno, name, value in prefixes:
        key = (rel, name)
        entry = registry["prefixes"].get(key)
        if entry is None:
            errors.append(
                "{}:{}: R3 persistent prefix {!r} = {!r} is not registered".format(
                    rel, lineno, name, value
                )
            )
            continue
        if entry["value"] != value:
            errors.append(
                "{}:{}: R3 registered prefix {!r} has value {!r} in the registry and "
                "{!r} in the source; value drift reuses an old exception".format(
                    rel, lineno, name, entry["value"], value
                )
            )
            continue
        seen_prefixes.add(key)

    for key in sorted(registry["prefixes"]):
        if key not in seen_prefixes:
            errors.append(
                "R3 registered prefix {!r} in {} is no longer declared; remove the "
                "stale exception".format(key[1], key[0])
            )

    prefix_files = {key[0] for key in registry["prefixes"]}
    for rel, entry in sorted(registry["files"].items()):
        if rel in persistence_files or rel in prefix_files:
            continue
        errors.append(
            "R3 registered {} file {!r} carries neither a persistence call nor a "
            "registered prefix".format(entry["kind"], rel)
        )

    for error in errors:
        print("ERROR: " + error)
    print(
        "check_durable_truth_registry: go_files={} persistence_files={} prefixes={} "
        "surfaces={} registered_files={} errors={}".format(
            len(go_files),
            len(persistence_files),
            len(prefixes),
            len(registry["surfaces"]),
            len(registry["files"]),
            len(errors),
        )
    )
    if errors:
        print("check_durable_truth_registry: {} error(s)".format(len(errors)))
        return 1
    print("check_durable_truth_registry: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
