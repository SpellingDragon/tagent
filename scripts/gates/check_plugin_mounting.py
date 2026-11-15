#!/usr/bin/env python3
"""check_plugin_mounting.py - ADR 0006 R5: features mount through the plugin registry.

R5a pipeline member sourcing. A pipeline collection is any container whose type
is a slice of plugin instances ([]plugin.Instance). The set is derived from the
source, never registered: a container added later joins the judged set without a
registry edit, and an empty derivation is red because it means the type anchor
this rule reads has moved away. An append into a pipeline collection may carry
only forwarded (spread) arguments; a single concrete member (a field, a local
component, a constructor result) or a composite literal is red unless it is
registered with the review that accepted it. A whole assignment of a resolved
collection into the container is not an append and is not judged here.

The plugin's own mount file and the one-line registry entry are the legal
landing point for a concrete construction, so they are out of the R5a scan: the
files registered under mounting.registry_implementation in
golden/runtime/architecture.json are skipped. Without that skip the rule would
judge away the only place a plugin may be built, an unsatisfiable criterion that
justifies itself.

R5b declaration liveness. The plugins declaration is live only when all four
hold: C1 the declaration surface accepts the key (api/config/decode.go
agentKeys); C2 the host document dialect declares the key
(assembly/host_document.go hostAgent); C3 the mapping writes it into the agent
spec (host_document.go mapDocument); C4 production code iterates the declared
names (range <expr>.Plugins). The config package's own encode/decode, clone and
fingerprint mapping, a compile-time assertion, and a change note carrying
reflect.DeepEqual are not consumers.

Registration. golden/runtime/architecture.json carries the mounting invariant:
the three SHALL clauses, the gate entry, and the declaration key anchored both
to the decoder vocabulary and to the config field. A missing registration, or a
registered declaration key the decoder does not accept, is red: the golden file
is the cross-domain contract, the script alone is not.

The registry golden/runtime/plugin_mounts.json is reviewed, not a learned
baseline: --write-baseline is deliberately absent. Allowing a mount means
editing the registry (with the ADR 0006 metadata), which is the visible act the
breakthrough protocol asks for; a registered exception whose mount is gone is
stale and rejected. Exceptions are matched by file and element expression, not
by line, so an unrelated edit above the site does not invalidate them.

Scope: production Go sources of the product packages, tests excluded. Comments
and strings are masked before the R5a scan, so a discussion of a mount is not a
mount; the host dialect is read with strings kept because its keys are yaml tags.

Exit 0 when the registration holds, no unregistered mount exists, and the
declaration is live; 1 on violations or unusable input.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

import guard_checklib as lib

DEFAULT_REGISTRY = "golden/runtime/plugin_mounts.json"
REGISTRY_LABEL = "plugin mounts registry"
GOLDEN_FILE = "golden/runtime/architecture.json"
GOLDEN_LABEL = "golden/runtime/architecture.json"
SCOPE_DIR = "assembly"
DECLARATION_FILE = "api/config/decode.go"
SPEC_FILE = "api/config/config.go"
HOST_DOCUMENT_FILE = "assembly/host_document.go"
DECLARATION_KEY = "plugins"
INVARIANT_COUNT = 3
PRODUCT_DIRS = (
    "api",
    "assembly",
    "runtime",
    "memory",
    "features",
    "tool",
    "cli",
    "internal",
    "bench",
)

# The two shapes a pipeline collection is declared in: a field of the given type
# and a local initialized from one. Both are derived from the type anchor.
PIPELINE_FIELD_RE = re.compile(
    r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]+\[\]plugin\.Instance[ \t]*$", re.M
)
PIPELINE_LOCAL_RE = re.compile(
    r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]:?=[ \t]*(?:"
    r"append\([ \t]*\[\]plugin\.Instance\(nil\)|make\([ \t]*\[\]plugin\.Instance,)",
    re.M,
)
APPEND_RE = re.compile(r"\bappend\(")
LITERAL_TARGET_RE = re.compile(r"^\[\]plugin\.Instance\(")
TAIL_IDENT_RE = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)$")
COMPOSITE_RE = re.compile(r"[A-Za-z0-9_\]]\s*\{")

# The declaration surfaces read rather than restated: the decoder vocabulary, the
# host document dialect and the mapping into the agent spec.
AGENT_KEYS_BLOCK = re.compile(r"var agentKeys = \[\]string\{(.*?)\n\}", re.S)
QUOTED_KEY = re.compile(r'"([A-Za-z_][A-Za-z0-9_]*)"')
HOST_AGENT_BLOCK = re.compile(r"^type hostAgent struct \{(.*?)^\}", re.S | re.M)
YAML_TAG = re.compile(r'yaml:"([a-z_]+)"')
AGENT_SPEC_BLOCK = re.compile(r"^type AgentSpec struct \{(.*?)^\}", re.S | re.M)
MAP_DOCUMENT_RE = re.compile(r"^func[ \t]+\([^)\n]*\)[ \t]*mapDocument\(", re.M)
SPEC_LITERAL_RE = re.compile(r"config\.AgentSpec\{")
RANGE_PLUGINS_RE = re.compile(r"\brange\s+[A-Za-z_][A-Za-z0-9_.\[\]]*\.Plugins\b")
CHANGE_NOTE_MARKER = "reflect.DeepEqual"


def normalize(text):
    return " ".join(text.split())


def matching_close(text, start):
    """Offset of the bracket closing the one at start, or None."""
    depth = 0
    for index in range(start, len(text)):
        char = text[index]
        if char in "([{":
            depth += 1
        elif char in ")]}":
            depth -= 1
            if depth == 0:
                return index
    return None


def split_top_level(text):
    """Split a call argument list at its top-level commas."""
    parts, current, depth = [], [], 0
    for char in text:
        if char in "([{":
            depth += 1
        elif char in ")]}":
            depth -= 1
        if char == "," and depth == 0:
            parts.append("".join(current))
            current = []
            continue
        current.append(char)
    parts.append("".join(current))
    return [part.strip() for part in parts if part.strip()]


def load_registry(root, path):
    errors = []
    try:
        data = lib.load_json(path)
    except ValueError as exc:
        return None, [str(exc)]
    lib.validate_guard_metadata(data, root, REGISTRY_LABEL, errors)
    exceptions = data.get("exceptions") if isinstance(data, dict) else None
    if not isinstance(exceptions, list):
        errors.append("{}: 'exceptions' must be a list".format(REGISTRY_LABEL))
        exceptions = []
    registered = {}
    for index, entry in enumerate(exceptions):
        label = "{} exception[{}]".format(REGISTRY_LABEL, index)
        if not isinstance(entry, dict):
            errors.append("{}: must be an object".format(label))
            continue
        file_value = lib.require_field(entry, "file", label, errors, str)
        element = lib.require_field(entry, "element", label, errors, str)
        lib.validate_exception_metadata(entry, root, label, errors)
        if file_value is None or element is None:
            continue
        if not file_value.startswith(SCOPE_DIR + "/"):
            errors.append(
                "{}: {!r} is outside the {}/ scope".format(label, file_value, SCOPE_DIR)
            )
        if not os.path.isfile(os.path.join(root, file_value)):
            errors.append("{}: file {!r} does not exist".format(label, file_value))
        key = (file_value, normalize(element))
        if key in registered:
            errors.append(
                "{}: duplicate registration of {!r} in {}".format(
                    REGISTRY_LABEL, element, file_value
                )
            )
            continue
        registered[key] = entry
    return registered, errors


def mounting_entry(root):
    try:
        golden = lib.load_json(os.path.join(root, GOLDEN_FILE))
    except ValueError as exc:
        return None, [str(exc)]
    if not isinstance(golden, dict):
        return None, ["{}: must be a JSON object".format(GOLDEN_LABEL)]
    mounting = golden.get("mounting")
    if not isinstance(mounting, dict):
        return None, [
            "{}: mounting is not registered: no invariant records that features "
            "mount through the plugin registry".format(GOLDEN_LABEL)
        ]
    return mounting, []


def registration_errors(root, mounting, agent_keys):
    """The mounting registration: invariants, gate entry, declaration anchors."""
    errors = []
    invariants = mounting.get("invariant")
    if not isinstance(invariants, list) or len(invariants) != INVARIANT_COUNT:
        errors.append(
            "{}: mounting.invariant must carry the {} invariants, got {!r}".format(
                GOLDEN_LABEL, INVARIANT_COUNT, invariants
            )
        )
        invariants = []
    for index, statement in enumerate(invariants):
        if not isinstance(statement, str) or "SHALL" not in statement:
            errors.append(
                "{}: mounting.invariant[{}] must state its SHALL clause".format(
                    GOLDEN_LABEL, index
                )
            )
    gate = mounting.get("gate")
    if not isinstance(gate, str) or not gate.strip():
        errors.append("{}: mounting.gate must name the checking entry".format(GOLDEN_LABEL))
    elif not os.path.isfile(os.path.join(root, gate)):
        errors.append(
            "{}: mounting.gate {!r} does not exist".format(GOLDEN_LABEL, gate)
        )
    declaration = mounting.get("declaration")
    key = declaration.get("key") if isinstance(declaration, dict) else None
    if key != DECLARATION_KEY:
        errors.append(
            "{}: mounting.declaration.key must be {!r}, got {!r}".format(
                GOLDEN_LABEL, DECLARATION_KEY, key
            )
        )
    else:
        if agent_keys and key not in agent_keys:
            errors.append(
                "{}: mounting.declaration.key {!r} is not accepted by {}".format(
                    GOLDEN_LABEL, key, DECLARATION_FILE
                )
            )
        field = key.capitalize()
        spec_source = os.path.join(root, SPEC_FILE)
        block = None
        if os.path.isfile(spec_source):
            block = AGENT_SPEC_BLOCK.search(
                lib.strip_go_comments(lib.read_text(spec_source))
            )
        if block is None:
            errors.append(
                "{}: {} carries no AgentSpec block to anchor {!r}".format(
                    GOLDEN_LABEL, SPEC_FILE, key
                )
            )
        elif not re.search(r"\b" + field + r"\b", block.group(1)):
            errors.append(
                "{}: mounting.declaration.key {!r} names no {} field of {}".format(
                    GOLDEN_LABEL, key, field, SPEC_FILE
                )
            )
    return errors


def registry_implementation(root, mounting):
    """Files that are the plugin registry itself: out of the R5a scan."""
    entries = mounting.get("registry_implementation")
    if not isinstance(entries, list):
        return [], [
            "{}: mounting.registry_implementation must be a list of the plugin "
            "registry implementation files".format(GOLDEN_LABEL)
        ]
    errors, paths = [], []
    for index, entry in enumerate(entries):
        if not isinstance(entry, str) or not entry.startswith(SCOPE_DIR + "/"):
            errors.append(
                "{}: mounting.registry_implementation[{}] must name a {} file".format(
                    GOLDEN_LABEL, index, SCOPE_DIR
                )
            )
            continue
        if not entry.endswith(".go"):
            errors.append(
                "{}: mounting.registry_implementation[{}] {!r} is not a Go source".format(
                    GOLDEN_LABEL, index, entry
                )
            )
            continue
        paths.append(entry)
    return paths, errors


def pipeline_containers(root, skipped):
    """Derive the pipeline collections from the []plugin.Instance type anchor."""
    containers, files = set(), []
    for path in lib.go_files(root, [SCOPE_DIR]):
        rel = lib.rel_path(root, path)
        if rel in skipped:
            continue
        files.append(rel)
        code = lib.scan_go_strings(lib.strip_go_comments(lib.read_text(path)))
        for match in PIPELINE_FIELD_RE.finditer(code):
            containers.add(match.group(1))
        for match in PIPELINE_LOCAL_RE.finditer(code):
            containers.add(match.group(1))
    return containers, files


def mount_violations(root, containers, files):
    """Appends into a pipeline collection that carry a concrete member."""
    found = []
    for rel in files:
        code = lib.scan_go_strings(
            lib.strip_go_comments(lib.read_text(os.path.join(root, rel)))
        )
        for match in APPEND_RE.finditer(code):
            open_at = match.end() - 1
            close_at = matching_close(code, open_at)
            if close_at is None:
                continue
            args = split_top_level(code[open_at + 1:close_at])
            if len(args) < 2:
                continue
            target = args[0]
            if not LITERAL_TARGET_RE.match(target):
                tail = TAIL_IDENT_RE.search(target)
                if tail is None or tail.group(1) not in containers:
                    continue
            for element in args[1:]:
                if element.endswith("..."):
                    continue
                form = (
                    "composite literal" if COMPOSITE_RE.search(element) else "concrete member"
                )
                found.append(
                    (rel, lib.line_of(code, match.start()), form, normalize(element))
                )
    return found


def declaration_liveness(root):
    """R5b C1-C4: the plugins declaration is accepted, declared, mapped, consumed."""
    errors, agent_keys = [], []

    decode_path = os.path.join(root, DECLARATION_FILE)
    if not os.path.isfile(decode_path):
        errors.append("R5b C1 {}: does not exist".format(DECLARATION_FILE))
    else:
        block = AGENT_KEYS_BLOCK.search(
            lib.strip_go_comments(lib.read_text(decode_path))
        )
        if block is None:
            errors.append(
                "R5b C1 {}: the agentKeys block is missing".format(DECLARATION_FILE)
            )
        else:
            agent_keys = QUOTED_KEY.findall(block.group(1))
            if DECLARATION_KEY not in agent_keys:
                errors.append(
                    "R5b C1 {}: agentKeys does not accept {!r}, so the declaration "
                    "has no key".format(DECLARATION_FILE, DECLARATION_KEY)
                )

    host_source = ""
    if not os.path.isfile(os.path.join(root, HOST_DOCUMENT_FILE)):
        errors.append("R5b C2 {}: does not exist".format(HOST_DOCUMENT_FILE))
    else:
        host_source = lib.strip_go_comments(
            lib.read_text(os.path.join(root, HOST_DOCUMENT_FILE))
        )
        dialect = HOST_AGENT_BLOCK.search(host_source)
        if dialect is None:
            errors.append(
                "R5b C2 {}: the hostAgent dialect is missing".format(HOST_DOCUMENT_FILE)
            )
        elif DECLARATION_KEY not in YAML_TAG.findall(dialect.group(1)):
            errors.append(
                'R5b C2 {}: hostAgent has no yaml:"{}" field, so a declared key has '
                "no host-dialect home".format(HOST_DOCUMENT_FILE, DECLARATION_KEY)
            )

    if host_source:
        mapping = MAP_DOCUMENT_RE.search(host_source)
        mapped = False
        if mapping is None:
            errors.append(
                "R5b C3 {}: mapDocument is missing".format(HOST_DOCUMENT_FILE)
            )
        else:
            open_at = host_source.find("{", mapping.end())
            close_at = matching_close(host_source, open_at) if open_at >= 0 else None
            body = host_source[open_at:close_at] if close_at is not None else ""
            field = DECLARATION_KEY.capitalize()
            for literal in SPEC_LITERAL_RE.finditer(body):
                literal_open = literal.end() - 1
                literal_close = matching_close(body, literal_open)
                if literal_close is None:
                    continue
                if re.search(r"\b" + field + r"\s*:", body[literal_open:literal_close]):
                    mapped = True
                    break
            if not mapped:
                errors.append(
                    "R5b C3 {}: mapDocument writes no {} into config.AgentSpec, so the "
                    "declaration never reaches the spec".format(HOST_DOCUMENT_FILE, field)
                )

    consumers = []
    for path in lib.go_files(root, list(PRODUCT_DIRS)):
        rel = lib.rel_path(root, path)
        if rel.startswith("api/config/"):
            continue
        code = lib.strip_go_comments(lib.read_text(path))
        for number, line in enumerate(code.splitlines(), 1):
            if CHANGE_NOTE_MARKER in line:
                continue
            if RANGE_PLUGINS_RE.search(line):
                consumers.append("{}:{}".format(rel, number))
    if not consumers:
        errors.append(
            "R5b C4: no production code iterates the declared names "
            "(range <expr>.Plugins); the declaration is a dead field"
        )
    return errors, agent_keys, consumers


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
        print("check_plugin_mounting: registry unusable")
        return 1

    consumers, containers, files, skipped, violations = [], set(), [], [], []
    mounting, mounting_errors = mounting_entry(root)
    errors.extend(mounting_errors)
    if not mounting_errors:
        mounting = mounting or {}
        liveness_errors, agent_keys, consumers = declaration_liveness(root)
        errors.extend(liveness_errors)
        errors.extend(registration_errors(root, mounting, agent_keys))
        skipped, impl_errors = registry_implementation(root, mounting)
        errors.extend(impl_errors)
        containers, files = pipeline_containers(root, set(skipped))
        if not containers:
            errors.append(
                "R5a {}/: no []plugin.Instance pipeline collection was derived; the "
                "type anchor this rule reads is gone".format(SCOPE_DIR)
            )
        seen = set()
        violations = mount_violations(root, containers, files)
        for rel, lineno, form, element in violations:
            key = (rel, element)
            if key in registered:
                seen.add(key)
                continue
            errors.append(
                "{}:{}: R5a pipeline collection takes a {} {!r}; forward a resolved "
                "collection or assign one, or register the breakthrough in {}".format(
                    rel, lineno, form, element, DEFAULT_REGISTRY
                )
            )
        for key in sorted(registered):
            if key not in seen:
                errors.append(
                    "R5a registered exception for {!r} in {} no longer matches a mount; "
                    "remove the stale exception".format(key[1], key[0])
                )

    for error in errors:
        print("ERROR: " + error)
    print(
        "check_plugin_mounting: go_files={} containers={} mounts={} consumers={} "
        "exceptions={} registry_impl={} present={} errors={}".format(
            len(files),
            len(containers),
            len(violations),
            len(consumers),
            len(registered),
            len(skipped),
            sum(1 for entry in skipped if os.path.isfile(os.path.join(root, entry))),
            len(errors),
        )
    )
    if errors:
        print("check_plugin_mounting: {} error(s)".format(len(errors)))
        return 1
    print("check_plugin_mounting: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
