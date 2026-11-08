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

# F0 (ADR 0006, 08.1): a durable key namespace must not become the truth for
# material it does not hold. When a file mints a prefix AND computes a content
# digest, the namespace carries digests of blobs stored elsewhere, so the
# registry must name the ticket fact that references them and the digest that
# verifies them. This is the shape runtime/state/recovery.go already has: the
# ticket fact carries sha256:<digest> + length and readSnapshot rechecks the blob
# against it, so the blob never stands as its own truth.
CONTENT_ADDRESS_ALGORITHM = "sha256"
DIGEST_USE_RE = re.compile(r"\bsha256\.(?:Sum256|New)\s*\(")


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
    surfaces_files = {}

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

        if surface_id is not None:
            surfaces_files[surface_id] = surface_files

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

    prefix_values = {entry["value"] for entry in registered_prefixes.values()}
    artifacts = load_content_artifacts(root, data, surfaces_files, prefix_values, errors)
    exempt = load_content_exempt(root, data, registered_files, errors)
    return {
        "surfaces": surfaces,
        "files": registered_files,
        "prefixes": registered_prefixes,
        "artifacts": artifacts,
        "exempt": exempt,
    }, errors


def load_content_artifacts(root, data, surface_files, registered_prefix_values, errors):
    """F0: parse and validate the chain-external artifacts referenced by facts.

    An artifact is material written outside the fact log that a registered ticket
    fact points at by content address. Every entry states which surface owns it,
    which registered prefix values carry the reference, and that the digest is
    sha256; the files must actually compute that digest, so the claim is checked
    against code and not only against the registry's own prose.
    """
    entries = data.get("content_addressed_artifacts", [])
    if not isinstance(entries, list):
        errors.append("{}: 'content_addressed_artifacts' must be a list".format(REGISTRY_LABEL))
        return []
    artifacts = []
    seen = set()
    for index, entry in enumerate(entries):
        label = "{} content_addressed_artifacts[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        artifact_id = lib.require_field(entry, "id", label, errors, str)
        if artifact_id is not None:
            if artifact_id in seen:
                errors.append(
                    "{}: duplicate artifact id {!r}".format(REGISTRY_LABEL, artifact_id))
            seen.add(artifact_id)
        surface_id = lib.require_field(entry, "surface", label, errors, str)
        if surface_id is not None and surface_id not in surface_files:
            errors.append(
                "{}: surface {!r} is not a registered surface".format(label, surface_id))
        files = entry.get("files")
        if not isinstance(files, list) or not files:
            errors.append("{}: 'files' must be a non-empty list".format(label))
            files = []
        owned = surface_files.get(surface_id, set())
        for file_value in files:
            clean = str(file_value).replace(os.sep, "/")
            if clean not in owned:
                errors.append(
                    "{}: file {!r} is not one of surface {!r}'s registered files".format(
                        label, file_value, surface_id))
        ticket = lib.require_field(entry, "ticket_fact", label, errors, str)
        if ticket is not None and ticket not in registered_prefix_values:
            errors.append(
                "{}: ticket fact {!r} is not a registered prefix value; an unregistered "
                "ticket is a second truth".format(label, ticket))
        algorithm = lib.require_field(entry, "content_address", label, errors, str)
        if algorithm is not None and algorithm != CONTENT_ADDRESS_ALGORITHM:
            errors.append(
                "{}: content_address {!r} must be {!r}; an artifact referenced without a "
                "verified digest is referenced by trust".format(
                    label, algorithm, CONTENT_ADDRESS_ALGORITHM))
        lib.require_field(entry, "reason", label, errors, str)
        lib.check_review_by(entry.get("review_by"), label, errors)
        for file_value in files:
            clean = str(file_value).replace(os.sep, "/")
            source_path = os.path.join(root, clean)
            if not os.path.isfile(source_path):
                continue
            code = lib.strip_go_comments(lib.read_text(source_path))
            if not DIGEST_USE_RE.search(lib.scan_go_strings(code)):
                errors.append(
                    "{}: {} computes no {} digest; the artifact is declared content-addressed "
                    "but nothing verifies it".format(
                        label, clean, CONTENT_ADDRESS_ALGORITHM))
        artifacts.append({"id": artifact_id, "surface": surface_id,
                          "files": [str(value).replace(os.sep, "/") for value in files]})
    return artifacts


def load_content_exempt(root, data, registered_files, errors):
    """Files whose digest derives a key instead of verifying stored material.

    `sha256` over an identity to mint a key is not a reference to an artifact the
    namespace does not hold: the digest IS the key, nothing is read back against
    it. Exempting one names that difference, so the trigger above stays exact
    instead of being relaxed for every file that reads a hash.
    """
    entries = data.get("content_address_exempt", [])
    if not isinstance(entries, list):
        errors.append("{}: 'content_address_exempt' must be a list".format(REGISTRY_LABEL))
        return set()
    exempt = set()
    for index, entry in enumerate(entries):
        label = "{} content_address_exempt[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        file_value = lib.require_field(entry, "file", label, errors, str)
        lib.require_field(entry, "reason", label, errors, str)
        if entry.get("review_by") is not None:
            lib.check_review_by(entry.get("review_by"), label, errors)
        if file_value is None:
            continue
        clean = file_value.replace(os.sep, "/")
        if clean in exempt:
            errors.append("{}: file {!r} is exempted twice".format(label, clean))
            continue
        if clean not in registered_files:
            errors.append(
                "{}: file {!r} is not a registered durable truth file".format(label, clean))
            continue
        exempt.add(clean)
    return exempt


def content_digest_files(root, files):
    """Report the product files that compute a content digest at all."""
    out = set()
    for path in files:
        code = lib.strip_go_comments(lib.read_text(path))
        if DIGEST_USE_RE.search(lib.scan_go_strings(code)):
            out.add(lib.rel_path(root, path))
    return out


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

    # F0 trigger: the registry cannot be the only witness. A file that mints a
    # durable key namespace and also computes a content digest carries references
    # to material it does not hold, so it must appear in the artifact list. An
    # added digest next to a prefix therefore cannot ship unregistered.
    declared_files = {rel for artifact in registry["artifacts"] for rel in artifact["files"]}
    digest_files = content_digest_files(root, go_files)
    for rel in sorted(registry["exempt"]):
        if rel not in digest_files:
            errors.append(
                "F0 exemption {}: computes no digest; remove the stale exemption".format(rel))
        if rel in declared_files:
            errors.append(
                "F0 exemption {}: is also declared as a content-addressed artifact".format(rel))
    for rel in sorted(digest_files & prefix_files):
        if rel not in declared_files and rel not in registry["exempt"]:
            errors.append(
                "F0 {}: mints a durable key namespace and computes a {} digest, but no "
                "content_addressed_artifacts entry names the ticket fact that references "
                "the material or the digest that verifies it".format(
                    rel, CONTENT_ADDRESS_ALGORITHM))

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
