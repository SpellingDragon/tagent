#!/usr/bin/env python3
"""check_plugin_mounting.py - ADR 0006 R5: features mount through the plugin registry.

R5a pipeline member sourcing. A pipeline collection is any container whose type
is a slice of plugin instances ([]plugin.Instance). The set is derived from the
source, never registered: a container added later joins the judged set without a
registry edit, and an empty derivation is red because it means the type anchor
this rule reads has moved away. Every declaration shape of that type is a
derivation: a struct field, a local initialised from it, a long declaration, a
composite literal initialiser, and a parameter, where a parameter group declares
every name its comma list carries (a, b []plugin.Instance derives both). An
append into a pipeline collection may carry only forwarded arguments: a spread of
a collection resolved elsewhere, or an element a range binds over one. A single
concrete member (a field, a local component, a constructor result) or a composite
literal is red unless it is registered with the review that accepted it, and a
spread of a composite literal names a construction rather than a collection. A
whole assignment of a resolved collection into the container is not an append and
is not judged here.

Neither forward may launder a construction of this file. A name this file binds
from a []plugin.Instance literal holds concrete constructions, so spreading it or
ranging over it is not a resolution and stays red; an alias of such a name is the
same name. Resolution is interprocedural-blind: a local bound from a call result
is read as resolved, because the gate judges appends and does not follow returns.

The plugin's own mount file and the one-line registry entry under
mounting.registry_implementation in golden/runtime/architecture.json are the
legal landing point for a concrete construction, so a forwarded element is
accepted there: the element an append names may be one member a range binds over
a collection resolved elsewhere. That exemption is a shape, not a file: outside
those files only a whole spread forwards, and inside them a concrete member or a
composite literal stays red, so a hardcoded mount cannot hide behind the
registry's name. A registered entry whose file is absent is red: the exemption
names a file the gate must be able to scan.

R5b declaration liveness. The plugins declaration is live only when all four
hold: C1 the declaration surface accepts the key (api/config/decode.go
agentKeys); C2 the host document dialect declares the key
(assembly/host_document.go hostAgent); C3 the mapping writes it into the agent
spec (host_document.go mapDocument); C4 production code iterates the declared
names. C4 is type-aware: the range subject's field must be the name set, so the
subject's type resolves through the declarations beside it and the slice fields
declared in the sources. A field whose element type is the plugin instance the
pipeline carries ([]plugin.Instance) is an instance collection, not the declared
names, and a subject whose type does not resolve is not a consumer either - both
fail closed instead of passing on a field name. The limit of the resolution: it
reads parameters, receivers, fields, long and short declarations, and a path with
a scalar field in its middle stays unresolved. The config package's own
encode/decode, clone and fingerprint mapping, a compile-time assertion, and a
change note carrying reflect.DeepEqual are not consumers.

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

# Every shape a pipeline collection is declared in, all derived from the type
# anchor: a field of the given type, a local initialised from one, a long or
# short declaration of the type itself, and a parameter of it. A parameter group
# carries a comma list of names sharing the type that follows it, so the pattern
# captures the whole list and every name in it is derived; the prefix stays one
# of the three group delimiters, so the derived set only ever grows.
PIPELINE_FIELD_RE = re.compile(
    r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]+\[\]plugin\.Instance[ \t]*$", re.M
)
PIPELINE_LOCAL_RE = re.compile(
    r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]:?=[ \t]*(?:"
    r"append\([ \t]*\[\]plugin\.Instance\(nil\)|make\([ \t]*\[\]plugin\.Instance,"
    r"|\[\]plugin\.Instance[ \t]*\{|\[\]plugin\.Instance\(nil\))",
    re.M,
)
PIPELINE_VAR_RE = re.compile(
    r"^[ \t]*var[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]+\[\]plugin\.Instance[ \t]*(?:=|$)",
    re.M,
)
PIPELINE_PARAM_RE = re.compile(
    r"[(\[,][ \t]*([A-Za-z_][A-Za-z0-9_]*"
    r"(?:[ \t\r\n]*,[ \t\r\n]*[A-Za-z_][A-Za-z0-9_]*)*)[ \t]+\[\]plugin\.Instance\b"
)
PIPELINE_DECLARATIONS = (
    PIPELINE_FIELD_RE,
    PIPELINE_LOCAL_RE,
    PIPELINE_VAR_RE,
    PIPELINE_PARAM_RE,
)
# A name this file binds from a []plugin.Instance literal holds constructions of
# this file, which no forward may then launder into a pipeline collection.
PIPELINE_LITERAL_RE = re.compile(
    r"^[ \t]*(?:var[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*:?=[ \t]*"
    r"(?:append\([ \t]*)?\[\]plugin\.Instance[ \t]*\{",
    re.M,
)
PIPELINE_ALIAS_RE = re.compile(
    r"^[ \t]*(?:var[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*:?=[ \t]*"
    r"([A-Za-z_][A-Za-z0-9_]*)[ \t]*$",
    re.M,
)
# A spread may carry a slice of the collection it names; the name is still the
# operand, so a subscript does not launder what the name holds.
SPREAD_BASE_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)[ \t]*\[")
APPEND_RE = re.compile(r"\bappend\(")
LITERAL_TARGET_RE = re.compile(r"^\[\]plugin\.Instance\(")
TAIL_IDENT_RE = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)$")
COMPOSITE_RE = re.compile(r"[A-Za-z0-9_\]]\s*\{")
IDENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
IDENT_PREFIX_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
# A range clause: the names it binds and the collection it reads. A composite
# literal cannot match, so a range over a literal is never read as a resolution.
FOR_RANGE_RE = re.compile(r"\bfor[ \t]+([^;{}\n]*?)\brange[ \t]+([^;{}\n]+)")

# The two element types the judgement separates: the instance slice the pipeline
# carries, and the type of the names a declaration holds.
INSTANCE_TYPE = "plugin.Instance"
STRUCT_DECL_RE = re.compile(r"^type[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]+struct[ \t]*\{", re.M)
SLICE_FIELD_RE = re.compile(r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]+\[\]([A-Za-z_][A-Za-z0-9_.]*)", re.M)
FUNC_DECL_RE = re.compile(r"\bfunc\b")
TYPE_TOKEN_RE = re.compile(r"^[*\[\]]*[A-Za-z_][A-Za-z0-9_.]*$")
VAR_DECL_RE = re.compile(
    r"^[ \t]*(?:var[ \t]+)?([a-z][A-Za-z0-9_]*)[ \t]+(\*?[A-Za-z_][A-Za-z0-9_.]*)[ \t]*(?:`[^`]*`)?[ \t]*$",
    re.M,
)
LITERAL_ASSIGN_RE = re.compile(
    r"\b([a-z][A-Za-z0-9_]*)[ \t]*:?=[ \t]*&?([A-Za-z_][A-Za-z0-9_.]*)[ \t]*\{"
)

# The declaration surfaces read rather than restated: the decoder vocabulary, the
# host document dialect and the mapping into the agent spec.
AGENT_KEYS_BLOCK = re.compile(r"var agentKeys = \[\]string\{(.*?)\n\}", re.S)
QUOTED_KEY = re.compile(r'"([A-Za-z_][A-Za-z0-9_]*)"')
HOST_AGENT_BLOCK = re.compile(r"^type hostAgent struct \{(.*?)^\}", re.S | re.M)
YAML_TAG = re.compile(r'yaml:"([a-z_]+)"')
AGENT_SPEC_BLOCK = re.compile(r"^type AgentSpec struct \{(.*?)^\}", re.S | re.M)
MAP_DOCUMENT_RE = re.compile(r"^func[ \t]+\([^)\n]*\)[ \t]*mapDocument\(", re.M)
SPEC_LITERAL_RE = re.compile(r"config\.AgentSpec\{")
RANGE_PLUGINS_RE = re.compile(
    r"\brange[ \t]+([A-Za-z_][A-Za-z0-9_.]*)[ \t]*\.Plugins\b"
)
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


def unqualified(type_name):
    """A type name without its pointer, slice and package qualifiers."""
    return type_name.lstrip("*[]").rsplit(".", 1)[-1]


def literal_sources(code):
    """The names this file binds from a []plugin.Instance literal, aliases closed.

    A rebinding of one of those names keeps the literal as its source, so the set
    is closed to a fixed point: a forward through an alias is the same laundering.
    """
    names = {match.group(1) for match in PIPELINE_LITERAL_RE.finditer(code)}
    pending = True
    while pending:
        pending = False
        for match in PIPELINE_ALIAS_RE.finditer(code):
            target, source = match.group(1), match.group(2)
            if source in names and target not in names:
                names.add(target)
                pending = True
    return names


def forwards_resolved(code, offset, element, literal_names, exempt):
    """Whether an append element forwards a collection resolved outside this file.

    A spread forwards the operand it names, or a slice of it; an element forwards
    one member a range binds. A spread of a composite literal names a construction,
    not a collection, and either operand is refused when this file binds the name
    from a []plugin.Instance literal, because the elements are then constructions
    of this file. `exempt` admits the element form, which only a registry
    implementation may use while it filters. When several ranges bind the element,
    one of them reading a literal is enough to refuse it.

    Returns (forwarded, laundered_from): laundered_from names the local the
    element was laundered through when that binding is the only reason to refuse
    it, and is None for every other rejection.
    """
    if element.endswith("..."):
        operand = element[:-3].strip()
        base = SPREAD_BASE_RE.match(operand)
        if base is not None:
            operand = base.group(1)
        if operand in literal_names:
            return False, operand
        return not COMPOSITE_RE.search(element[:-3].strip()), None
    if not exempt or not IDENT_RE.match(element):
        return False, None
    body_start = code.rfind("\nfunc", 0, offset)
    body = code[body_start if body_start >= 0 else 0:offset]
    bound, laundered = False, None
    for match in FOR_RANGE_RE.finditer(body):
        names = [name.strip() for name in match.group(1).replace(":=", " ").split(",")]
        if element not in names:
            continue
        bound = True
        source = match.group(2).strip()
        if source in literal_names:
            laundered = source
    if bound and laundered is None:
        return True, None
    return False, laundered


def struct_field_elements(code):
    """The slice fields each struct type declares, with their element types."""
    fields = {}
    for match in STRUCT_DECL_RE.finditer(code):
        open_at = code.find("{", match.end() - 1)
        close_at = matching_close(code, open_at) if open_at >= 0 else None
        if close_at is None:
            continue
        entry = fields.setdefault(match.group(1), {})
        for field in SLICE_FIELD_RE.finditer(code[open_at + 1:close_at]):
            entry[field.group(1)] = field.group(2)
    return fields


def parameter_pairs(code):
    """The name and type pairs the parenthesised groups of a func declare.

    A receiver group, a parameter list and a result list are all read the same
    way, so a receiver's type resolves as well as a parameter's.
    """
    pairs = []
    for match in FUNC_DECL_RE.finditer(code):
        position = match.end()
        while position < len(code) and code[position] in " \t\n":
            position += 1
        if position < len(code) and code[position] == "(":
            close_at = matching_close(code, position)
            if close_at is not None:
                pairs.extend(pairs_of_group(code[position + 1:close_at]))
                position = close_at + 1
                while position < len(code) and code[position] in " \t\n":
                    position += 1
        name = IDENT_PREFIX_RE.match(code, position)
        if name is not None:
            position = name.end()
        while True:
            while position < len(code) and code[position] in " \t\n":
                position += 1
            if position >= len(code) or code[position] != "(":
                break
            close_at = matching_close(code, position)
            if close_at is None:
                break
            pairs.extend(pairs_of_group(code[position + 1:close_at]))
            position = close_at + 1
    return pairs


def pairs_of_group(group):
    """The name and type pairs one parenthesised group declares."""
    pairs = []
    for part in split_top_level(group):
        tokens = part.split()
        if len(tokens) < 2 or not TYPE_TOKEN_RE.match(tokens[-1]):
            continue
        for name in tokens[:-1]:
            if IDENT_RE.match(name):
                pairs.append((name, tokens[-1]))
    return pairs


def declared_types(code):
    """The type each identifier of one file is declared with, by shape."""
    types = {}
    for name, type_name in parameter_pairs(code):
        types.setdefault(name, set()).add(unqualified(type_name))
    for match in VAR_DECL_RE.finditer(code):
        types.setdefault(match.group(1), set()).add(unqualified(match.group(2)))
    for match in LITERAL_ASSIGN_RE.finditer(code):
        types.setdefault(match.group(1), set()).add(unqualified(match.group(2)))
    return types


def declared_name_element(parts, types, fields):
    """The element type of the slice field a range subject names, or None.

    A subject resolves when it is one identifier this file declares with a single
    type, followed by a slice field that type declares. Any other shape stays
    unresolved, and an unresolved subject is not a consumer.
    """
    if len(parts) != 2:
        return None
    candidates = types.get(parts[0])
    if not candidates or len(candidates) != 1:
        return None
    return fields.get(next(iter(candidates)), {}).get(parts[1])


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
        if not os.path.isfile(os.path.join(root, entry)):
            errors.append(
                "{}: mounting.registry_implementation[{}] {!r} does not exist".format(
                    GOLDEN_LABEL, index, entry
                )
            )
            continue
        paths.append(entry)
    return paths, errors


def pipeline_containers(root):
    """Derive the pipeline collections from the []plugin.Instance type anchor."""
    containers, files = set(), []
    for path in lib.go_files(root, [SCOPE_DIR]):
        rel = lib.rel_path(root, path)
        files.append(rel)
        code = lib.scan_go_strings(lib.strip_go_comments(lib.read_text(path)))
        for pattern in PIPELINE_DECLARATIONS:
            for match in pattern.finditer(code):
                for name in match.group(1).split(","):
                    containers.add(name.strip())
    return containers, files


def mount_violations(root, containers, files, forwarded):
    """Appends into a pipeline collection that carry a concrete member.

    In the registry implementation files a member a range forwards is accepted;
    everywhere else, and for every other element, only a spread forwards. Neither
    forward may carry a name this file binds from a []plugin.Instance literal.
    """
    found = []
    for rel in files:
        code = lib.scan_go_strings(
            lib.strip_go_comments(lib.read_text(os.path.join(root, rel)))
        )
        literal_names = literal_sources(code)
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
                forwarded_ok, laundered = forwards_resolved(
                    code, match.start(), element, literal_names, rel in forwarded
                )
                if forwarded_ok:
                    continue
                if element.endswith("..."):
                    form = "spread"
                    note = (
                        ", a name this file binds from a []plugin.Instance literal"
                        if laundered
                        else ""
                    )
                else:
                    form = (
                        "composite literal"
                        if COMPOSITE_RE.search(element)
                        else "concrete member"
                    )
                    note = (
                        ", forwarded from {!r}, which this file binds from a "
                        "[]plugin.Instance literal".format(laundered)
                        if laundered
                        else ""
                    )
                found.append(
                    (rel, lib.line_of(code, match.start()), form, normalize(element), note)
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

    consumers, rejected = [], []
    fields, sources = {}, []
    for path in lib.go_files(root, list(PRODUCT_DIRS)):
        code = lib.strip_go_comments(lib.read_text(path))
        sources.append((lib.rel_path(root, path), code))
        for type_name, mapping in struct_field_elements(code).items():
            fields.setdefault(type_name, {}).update(mapping)
    field = DECLARATION_KEY.capitalize()
    for rel, code in sources:
        if rel.startswith("api/config/"):
            continue
        types = declared_types(code)
        for number, line in enumerate(code.splitlines(), 1):
            if CHANGE_NOTE_MARKER in line:
                continue
            match = RANGE_PLUGINS_RE.search(line)
            if match is None:
                continue
            subject = "{}.{}".format(match.group(1), field)
            element = declared_name_element(subject.split("."), types, fields)
            where = "{}:{}".format(rel, number)
            if element is None:
                rejected.append((where, subject, "resolves to no type that declares it"))
            elif element == INSTANCE_TYPE:
                rejected.append(
                    (where, subject, "reads a {} field, an instance collection".format(element))
                )
            else:
                consumers.append(where)
    if not consumers:
        errors.append(
            "R5b C4: no production code iterates the declared names "
            "(range <expr>.Plugins); the declaration is a dead field"
        )
    for where, subject, why in rejected:
        errors.append(
            "R5b C4 {}: range {} {} - the declared name set is not what it reads; "
            "the declaration stays a dead field".format(where, subject, why)
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
        containers, files = pipeline_containers(root)
        if not containers:
            errors.append(
                "R5a {}/: no []plugin.Instance pipeline collection was derived; the "
                "type anchor this rule reads is gone".format(SCOPE_DIR)
            )
        seen = set()
        violations = mount_violations(root, containers, files, set(skipped))
        for rel, lineno, form, element, note in violations:
            key = (rel, element)
            if key in registered:
                seen.add(key)
                continue
            errors.append(
                "{}:{}: R5a pipeline collection takes a {} {!r}{}; forward a resolved "
                "collection or assign one, or register the breakthrough in {}".format(
                    rel, lineno, form, element, note, DEFAULT_REGISTRY
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
