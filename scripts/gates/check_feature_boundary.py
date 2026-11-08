#!/usr/bin/env python3
"""check_feature_boundary.py - ADR 0006 R1/R2/R3 machine guard.

R1 schema sections: agent declarations in host YAML may use only the generic
   agent keys. The generic set is not restated here: it is read out of
   api/config/decode.go's agentKeys, the one place the decoder declares which
   keys an agent mapping may carry, so a key the decoder accepts is never read
   as a feature-specific section and this gate can never hold a second copy of
   that vocabulary. A key that still names a feature section (meditation) is
   refused here unless the reviewed exception registry of
   check_schema_allowlist.py allows it.
R2 assembly symbols: declarations shaped resolve<Feature>/validate<Feature>/
   <feature>Port under assembly/ must be listed in the shared R2 baseline,
   golden/runtime/assembly_symbols.json (the same registry read by
   check_assembly_symbols.py). load_baseline() accepts a plain string list,
   a dict with 'symbols', or a reviewed registry of objects with a 'name'
   field; all shapes are reduced to symbol names.
R3 runtime branches: executable code under runtime/ must not name the
   meditation/reflection feature. Comments are ignored.

The scan is static and indentation-based by design: no YAML dependency and no
Go type checking. It reads both shapes an `agents:` block is written in: a
mapping keyed by agent id and a sequence of `- id: ...` items. Exit 0 when every
selected rule passes, 1 on violations or unusable input.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

DEFAULT_BASELINE = "golden/runtime/assembly_symbols.json"
DEFAULT_DOCUMENTS = ("examples/wechat-bot/tagent.yaml", "tagent.yaml")

# The two declaration surfaces an agent section is decoded by: the config chain
# (decode.go's agentKeys) and the host document dialect (hostAgent's yaml tags).
# The gate reads them instead of holding a copy, because a copy is what let a
# legal key (output_reserve) read as a feature section while the surface moved.
AGENT_KEYS_FILE = os.path.join("api", "config", "decode.go")
AGENT_KEYS_BLOCK = re.compile(r"var agentKeys = \[\]string\{(.*?)\n\}", re.S)
HOST_DOCUMENT_FILE = os.path.join("assembly", "host_document.go")
HOST_AGENT_BLOCK = re.compile(r"^type hostAgent struct \{(.*?)^\}", re.S | re.M)
YAML_TAG = re.compile(r'yaml:"([a-z_]+)"')
QUOTED_KEY = re.compile(r'"([A-Za-z_][A-Za-z0-9_]*)"')

# Methods are scanned too: feature-specific assembly paths have appeared as
# declarations like validateMeditationTools, not only as package-level funcs.
FUNC_SYMBOL_RE = re.compile(
    r"^[ \t]*func[ \t]+(?:\([^)\n]*\)[ \t]*)?((?:resolve|validate)[A-Z][A-Za-z0-9_]*)\b",
    re.M,
)
# All <feature>Port shapes are matched, including the already-grandfathered
# exported RecallPort, because the baseline records that existing debt.
PORT_SYMBOL_RE = re.compile(r"^[ \t]*type[ \t]+([A-Za-z_][A-Za-z0-9_]*Port)\b", re.M)
RAW_QUOTE = chr(96)
IDENT_RE = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*\b")
FEATURE_SUBSTR_RE = re.compile(r"(?i)(meditation|reflection)")


def read_text(path):
    with open(path, "r", encoding="utf-8") as handle:
        return handle.read()


def generic_agent_keys(root):
    """The generic agent keys, read out of the two surfaces that decode them.

    Reading the declarations is what makes R1 one registration rather than two:
    a key either decoder accepts for an agent section is generic by definition,
    and no hand-written copy of that vocabulary can fall behind the source it
    copies. The config chain (api/config/decode.go agentKeys) is required; the
    host document dialect (assembly/host_document.go's hostAgent tags) joins the
    set while that file exists, which is the only window in which a host document
    may carry those keys at all. An unreadable required file or a missing
    declaration is a named error, never an empty allowlist — that would refuse
    every key and read as a thousand violations of one cause.
    """
    path = os.path.join(root, AGENT_KEYS_FILE)
    try:
        text = read_text(path)
    except (OSError, UnicodeError) as exc:
        raise ValueError("the agent key declaration {} is unreadable: {}".format(AGENT_KEYS_FILE, exc))
    blocks = AGENT_KEYS_BLOCK.findall(text)
    if not blocks:
        raise ValueError("the agent key declaration {} carries no agentKeys block".format(AGENT_KEYS_FILE))
    keys = set()
    for block in blocks:
        keys.update(QUOTED_KEY.findall(block))
    if not keys:
        raise ValueError("the agent key declaration {} names no agent key".format(AGENT_KEYS_FILE))

    host_path = os.path.join(root, HOST_DOCUMENT_FILE)
    if os.path.exists(host_path):
        try:
            host_text = read_text(host_path)
        except (OSError, UnicodeError) as exc:
            raise ValueError("the host document dialect {} is unreadable: {}".format(HOST_DOCUMENT_FILE, exc))
        host_blocks = HOST_AGENT_BLOCK.findall(host_text)
        if not host_blocks:
            raise ValueError(
                "the host document dialect {} carries no hostAgent struct".format(HOST_DOCUMENT_FILE))
        for block in host_blocks:
            keys.update(YAML_TAG.findall(block))
    return frozenset(keys)


def rel_path(root, path):
    return os.path.relpath(path, root).replace(os.sep, "/")


def display_path(root, path):
    rel = rel_path(root, path)
    return path if rel.startswith("..") else rel


def strip_go_comments(text):
    """Blank out Go comments while preserving strings, newlines and offsets."""
    out = list(text)
    i, n = 0, len(text)
    state = "code"
    while i < n:
        ch = text[i]
        if state == "code":
            if ch == "/" and i + 1 < n and text[i + 1] == "/":
                out[i] = out[i + 1] = " "
                i += 2
                while i < n and text[i] != "\n":
                    out[i] = " "
                    i += 1
                continue
            if ch == "/" and i + 1 < n and text[i + 1] == "*":
                out[i] = out[i + 1] = " "
                i += 2
                while i < n:
                    if text[i] == "*" and i + 1 < n and text[i + 1] == "/":
                        out[i] = out[i + 1] = " "
                        i += 2
                        break
                    if text[i] != "\n":
                        out[i] = " "
                    i += 1
                continue
            if ch == '"':
                state = "string"
            elif ch == "'":
                state = "rune"
            elif ch == RAW_QUOTE:
                state = "raw"
            i += 1
        elif state in ("string", "rune"):
            if ch == "\\":
                i += 2
                continue
            if (state == "string" and ch == '"') or (state == "rune" and ch == "'"):
                state = "code"
            i += 1
        else:  # raw string
            if ch == RAW_QUOTE:
                state = "code"
            i += 1
    return "".join(out)


def scan_go_strings(code):
    """Return (string literals as (line, content), code with strings/runes blanked)."""
    literals = []
    masked = list(code)
    i, n = 0, len(code)
    while i < n:
        ch = code[i]
        if ch == '"':
            start = i
            i += 1
            while i < n:
                if code[i] == "\\":
                    i += 2
                    continue
                if code[i] == '"':
                    i += 1
                    break
                i += 1
            content = code[start + 1:i - 1] if i > start + 1 and code[i - 1] == '"' else code[start + 1:i]
            literals.append((line_of(code, start), content))
            _blank(masked, start, i)
        elif ch == "'":
            start = i
            i += 1
            while i < n:
                if code[i] == "\\":
                    i += 2
                    continue
                if code[i] == "'":
                    i += 1
                    break
                i += 1
            _blank(masked, start, i)
        elif ch == RAW_QUOTE:
            start = i
            i += 1
            while i < n and code[i] != RAW_QUOTE:
                i += 1
            if i < n:
                i += 1
            content = code[start + 1:i - 1] if i > start + 1 and code[i - 1] == RAW_QUOTE else code[start + 1:i]
            literals.append((line_of(code, start), content))
            _blank(masked, start, i)
        else:
            i += 1
    return literals, "".join(masked)


def _blank(buf, start, end):
    for k in range(start, end):
        if buf[k] != "\n":
            buf[k] = " "


def go_files(root, name):
    """Return (sorted non-test Go files, directory existed)."""
    base = os.path.join(root, name)
    if not os.path.isdir(base):
        return [], False
    found = []
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = sorted(d for d in dirnames if not d.startswith(".") and d != "__pycache__")
        for filename in sorted(filenames):
            if filename.endswith(".go") and not filename.endswith("_test.go"):
                found.append(os.path.join(dirpath, filename))
    return sorted(found), True


def line_of(text, offset):
    return text.count("\n", 0, offset) + 1


def _indent(line):
    return len(line) - len(line.lstrip(" "))


def _blank_or_comment(line):
    stripped = line.strip()
    return not stripped or stripped.startswith("#")


def _unquote(text):
    text = text.strip()
    if len(text) >= 2 and text[0] == text[-1] and text[0] in "\"'":
        return text[1:-1]
    return text


def _strip_yaml_comment(text):
    quote = None
    i = 0
    while i < len(text):
        ch = text[i]
        if quote:
            if quote == '"' and ch == "\\":
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in "\"'":
            quote = ch
        elif ch == "#" and (i == 0 or text[i - 1].isspace()):
            return text[:i].rstrip()
        i += 1
    return text.rstrip()


def _split_top_level(text, separator=","):
    parts, start, depth, quote = [], 0, 0, None
    i = 0
    while i < len(text):
        ch = text[i]
        if quote:
            if quote == '"' and ch == "\\":
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in "\"'":
            quote = ch
        elif ch in "{[":
            depth += 1
        elif ch in "}]":
            depth -= 1
        elif ch == separator and depth == 0:
            parts.append(text[start:i])
            start = i + 1
        i += 1
    parts.append(text[start:])
    return parts


def _split_key_value(text):
    depth, quote = 0, None
    i = 0
    while i < len(text):
        ch = text[i]
        if quote:
            if quote == '"' and ch == "\\":
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in "\"'":
            quote = ch
        elif ch in "{[":
            depth += 1
        elif ch in "}]":
            depth -= 1
        elif ch == ":" and depth == 0:
            return text[:i], text[i + 1:]
        i += 1
    return None, None


def _parse_mapping_line(line):
    stripped = line.strip()
    if not stripped or stripped.startswith("#") or stripped.startswith("-"):
        return None
    key, value = _split_key_value(stripped)
    if key is None:
        return None
    key = _unquote(key)
    if not key:
        return None
    return key, (value or "").strip()


def _flow_entries(text):
    text = text.strip()
    if not (text.startswith("{") and text.endswith("}")):
        return []
    body = text[1:-1].strip()
    if not body:
        return []
    entries = []
    for part in _split_top_level(body):
        if not part.strip():
            continue
        key, value = _split_key_value(part)
        if key is None:
            continue
        name = _unquote(key)
        if name:
            entries.append((name, (value or "").strip()))
    return entries


def _flow_balanced(text):
    depth, quote = 0, None
    i = 0
    while i < len(text):
        ch = text[i]
        if quote:
            if quote == '"' and ch == "\\":
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in "\"'":
            quote = ch
        elif ch in "{[":
            depth += 1
        elif ch in "}]":
            depth -= 1
        i += 1
    return depth <= 0


def _gather_flow(lines, start, first):
    text = _strip_yaml_comment(first)
    j = start + 1
    while not _flow_balanced(text) and j < len(lines):
        if not _blank_or_comment(lines[j]):
            text += "\n" + _strip_yaml_comment(lines[j])
        j += 1
    return text


def _block_end(lines, start):
    for j in range(start + 1, len(lines)):
        if not _blank_or_comment(lines[j]) and _indent(lines[j]) <= 0:
            return j
    return len(lines)


def _keys_of_item(rel, lines, start, end, key_indent, agent, allowed, violations):
    """Judge every key one agent block carries at its own key indent."""
    for k in range(start, end):
        if _blank_or_comment(lines[k]) or _indent(lines[k]) != key_indent:
            continue
        parsed = _parse_mapping_line(lines[k])
        if parsed is None:
            continue
        key = parsed[0]
        if key not in allowed:
            violations.append((rel, k + 1, key, agent))


def _sequence_item_label(lines, start, end, key_indent):
    """Name the agent one `- id: ...` item declares, or the item's line."""
    for k in range(start, end):
        if _blank_or_comment(lines[k]) or _indent(lines[k]) != key_indent:
            continue
        parsed = _parse_mapping_line(lines[k])
        if parsed is not None and parsed[0] == "id":
            label = _unquote(_strip_yaml_comment(parsed[1]).strip())
            if label:
                return label
    return "line {}".format(start + 1)


def _agent_sequence_violations(rel, lines, children, block_end, item_indent, allowed, violations):
    """Judge an `agents:` block written as a sequence of `- id: ...` items.

    The sequence marker is replaced with two spaces, so one item reads as the
    mapping it carries and the same key-indent rule applies to both shapes. An
    item whose marker sits alone on its line keeps its keys on the following
    lines, which the normalized block already places at that same indent.
    """
    starts = [j for j in children if _indent(lines[j]) == item_indent and lines[j].strip().startswith("-")]
    if not starts:
        return
    normalized = list(lines)
    for index, start in enumerate(starts):
        end = starts[index + 1] if index + 1 < len(starts) else block_end
        line = lines[start]
        marker_at = item_indent
        rest = line[marker_at + 1:]
        rest = rest[1:] if rest.startswith(" ") else rest
        normalized[start] = line[:marker_at] + "  " + rest
        candidates = [
            k for k in range(start, end)
            if not _blank_or_comment(normalized[k]) and _parse_mapping_line(normalized[k]) is not None
        ]
        if not candidates:
            continue
        key_indent = min(_indent(normalized[k]) for k in candidates)
        agent = _sequence_item_label(normalized, start, end, key_indent)
        _keys_of_item(rel, normalized, start, end, key_indent, agent, allowed, violations)


def _flow_sequence_violations(rel, lines, start, text, allowed, violations):
    """Judge an `agents: [ {...}, {...} ]` flow sequence."""
    body = text.strip()
    body = body[1:-1] if body.startswith("[") and body.endswith("]") else body[1:]
    for part in _split_top_level(body):
        if not part.strip().startswith("{"):
            continue
        entries = _flow_entries(part)
        agent = ""
        for key, value in entries:
            if key == "id":
                agent = _unquote(value.strip())
        for key, _ in entries:
            if key not in allowed:
                violations.append((rel, start + 1, key, agent))


def _agent_section_violations(rel, lines, start, block_end, rest, allowed):
    violations = []
    if rest.startswith("{"):
        for agent, value in _flow_entries(_gather_flow(lines, start, rest)):
            for key, _ in _flow_entries(value):
                if key not in allowed:
                    violations.append((rel, start + 1, key, agent))
        return violations
    if rest.startswith("["):
        _flow_sequence_violations(rel, lines, start, _gather_flow(lines, start, rest), allowed, violations)
        return violations

    children = [j for j in range(start + 1, block_end) if not _blank_or_comment(lines[j])]
    if not children:
        return violations
    agent_indent = min(_indent(lines[j]) for j in children)
    if lines[children[0]].strip().startswith("-"):
        _agent_sequence_violations(rel, lines, children, block_end, agent_indent, allowed, violations)
        return violations
    j = start + 1
    while j < block_end:
        line = lines[j]
        if _blank_or_comment(line):
            j += 1
            continue
        indent = _indent(line)
        if indent < agent_indent:
            break
        if indent != agent_indent:
            j += 1
            continue
        parsed = _parse_mapping_line(line)
        if parsed is None:
            j += 1
            continue
        agent, value = parsed
        value = _strip_yaml_comment(value).strip()
        end = j + 1
        while end < block_end:
            if not _blank_or_comment(lines[end]) and _indent(lines[end]) <= agent_indent:
                break
            end += 1
        if value.startswith("{"):
            for key, _ in _flow_entries(_gather_flow(lines, j, value)):
                if key not in allowed:
                    violations.append((rel, j + 1, key, agent))
        else:
            key_lines = []
            for k in range(j + 1, end):
                if _blank_or_comment(lines[k]) or _indent(lines[k]) <= agent_indent:
                    continue
                parsed_key = _parse_mapping_line(lines[k])
                if parsed_key is not None:
                    key_lines.append((k, parsed_key[0]))
            if key_lines:
                key_indent = min(_indent(lines[k]) for k, _ in key_lines)
                _keys_of_item(rel, lines, j + 1, end, key_indent, agent, allowed, violations)
        j = end
    return violations


def check_host_document(root, path, allowed):
    rel = rel_path(root, path)
    try:
        text = read_text(path)
    except (OSError, UnicodeError) as exc:
        return ["{}: R1 host document is unreadable: {}".format(rel, exc)]
    lines = text.splitlines()
    violations = []
    for idx, line in enumerate(lines):
        if _blank_or_comment(line) or _indent(line) != 0:
            continue
        match = re.match(r"^agents\s*:\s*(.*)$", line.strip())
        if not match:
            continue
        rest = _strip_yaml_comment(match.group(1)).strip()
        block_end = _block_end(lines, idx)
        violations.extend(_agent_section_violations(rel, lines, idx, block_end, rest, allowed))
    errors = []
    for rel_name, lineno, key, agent in violations:
        errors.append(
            "{}:{}: R1 new feature-specific schema section {!r} under agents.{}".format(
                rel_name, lineno, key, agent
            )
        )
    return sorted(set(errors))


def check_assembly(root):
    files, exists = go_files(root, "assembly")
    if not exists:
        return ["assembly/: scan directory not found"], 0, []
    errors, declarations = [], []
    for path in files:
        rel = rel_path(root, path)
        try:
            text = read_text(path)
        except (OSError, UnicodeError) as exc:
            errors.append("{}: R2 Go source is unreadable: {}".format(rel, exc))
            continue
        code = strip_go_comments(text)
        _, masked = scan_go_strings(code)
        for match in FUNC_SYMBOL_RE.finditer(masked):
            declarations.append((rel, line_of(code, match.start()), match.group(1)))
        for match in PORT_SYMBOL_RE.finditer(masked):
            declarations.append((rel, line_of(code, match.start()), match.group(1)))
    return errors, len(files), sorted(set(declarations))


def check_runtime(root):
    files, exists = go_files(root, "runtime")
    if not exists:
        return ["runtime/: scan directory not found"], 0
    errors = []
    for path in files:
        rel = rel_path(root, path)
        try:
            text = read_text(path)
        except (OSError, UnicodeError) as exc:
            errors.append("{}: R3 Go source is unreadable: {}".format(rel, exc))
            continue
        code = strip_go_comments(text)
        literals, masked = scan_go_strings(code)
        seen = set()
        for lineno, content in literals:
            feature = FEATURE_SUBSTR_RE.search(content)
            if feature is None:
                continue
            token = feature.group(1).lower()
            if (lineno, token) not in seen:
                seen.add((lineno, token))
                errors.append(
                    "{}:{}: R3 runtime feature literal {!r} in executable code".format(
                        rel, lineno, token
                    )
                )
        for match in IDENT_RE.finditer(masked):
            token = match.group(0)
            lowered = token.lower()
            if "meditation" not in lowered and "reflection" not in lowered:
                continue
            lineno = line_of(code, match.start())
            if (lineno, token) not in seen:
                seen.add((lineno, token))
                errors.append(
                    "{}:{}: R3 runtime feature identifier {!r} in executable code".format(
                        rel, lineno, token
                    )
                )
    return errors, len(files)


def load_baseline(path):
    """Load R2 symbol names from any supported baseline shape.

    The shared golden registry (golden/runtime/assembly_symbols.json) is a
    dict whose 'symbols' entries are reviewed objects carrying a 'name' field;
    the legacy feature-autonomy baseline was a plain list of strings. Both are
    reduced to the same set of symbol names here, so this guard and
    check_assembly_symbols.py read one source of truth.
    """
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
    except (OSError, ValueError) as exc:
        raise ValueError("cannot read baseline {}: {}".format(path, exc))
    if isinstance(data, dict):
        symbols = data.get("symbols")
        if symbols is None:
            symbols = data.get("r2_symbols")
        if symbols is None:
            return []
    else:
        symbols = data
    if not isinstance(symbols, list):
        raise ValueError("baseline {} must contain a 'symbols' list".format(path))
    names = []
    for item in symbols:
        if isinstance(item, str):
            names.append(item)
        elif isinstance(item, dict) and isinstance(item.get("name"), str):
            names.append(item["name"])
        else:
            raise ValueError(
                "baseline {} 'symbols' entries must be strings or objects with "
                "a string 'name'".format(path)
            )
    return names


def write_baseline(path, declarations):
    symbols = sorted({name for _, _, name in declarations})
    payload = {
        "schema_version": 1,
        "note": (
            "ADR 0006 R2 baseline debt: assembly declarations shaped "
            "resolve<Feature>/validate<Feature>/<feature>Port that already "
            "existed when the boundary guard landed. New entries require a "
            "reviewed update, never a silent widening."
        ),
        "symbols": symbols,
    }
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, indent=2) + "\n")
    return symbols


def resolve_documents(root, specified):
    if specified:
        out, errors = [], []
        for item in specified:
            path = item if os.path.isabs(item) else os.path.join(root, item)
            if os.path.isfile(path):
                out.append(path)
            else:
                errors.append("{}: R1 host document not found".format(display_path(root, path)))
        return out, errors
    out = []
    for item in DEFAULT_DOCUMENTS:
        path = os.path.join(root, item)
        if os.path.isfile(path):
            out.append(path)
    return out, []


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("documents", nargs="*", help="host YAML documents (default: discovery list)")
    parser.add_argument("--root", default=".", help="repository root for scans")
    parser.add_argument("--baseline", default=DEFAULT_BASELINE, help="R2 baseline JSON (default: golden/runtime/assembly_symbols.json; string list, dict with 'symbols', or objects with 'name')")
    parser.add_argument("--write-baseline", action="store_true", help="rewrite a plain-string R2 baseline (refuses the golden object registry)")
    parser.add_argument("--only", action="append", choices=("r1", "r2", "r3"),
                        help="run only the named rule (repeatable)")
    args = parser.parse_args(argv)
    root = os.path.abspath(args.root)
    if not os.path.isdir(root):
        print("ERROR: {}: root is not a directory".format(args.root))
        return 1
    baseline_path = args.baseline if os.path.isabs(args.baseline) else os.path.join(root, args.baseline)
    selected = set(args.only) if args.only else {"r1", "r2", "r3"}

    if args.write_baseline:
        # The golden registry is a reviewed object allowlist owned by
        # check_assembly_symbols.py: each symbol carries file/reason/review_by
        # and the registry carries ADR metadata. write_baseline() emits a plain
        # string list, so overwriting that registry would silently destroy the
        # reviewed metadata and break check_assembly_symbols.py. Refuse writing
        # back to the golden path (default or explicit) and to any existing
        # object-shaped registry, with a named error.
        golden_path = os.path.abspath(os.path.join(root, DEFAULT_BASELINE))
        target_is_golden = os.path.abspath(baseline_path) == golden_path
        existing_is_object_registry = False
        if os.path.isfile(baseline_path):
            try:
                with open(baseline_path, "r", encoding="utf-8") as handle:
                    existing = json.load(handle)
                existing_symbols = (
                    existing.get("symbols") if isinstance(existing, dict) else existing
                )
                existing_is_object_registry = (
                    isinstance(existing_symbols, list)
                    and bool(existing_symbols)
                    and isinstance(existing_symbols[0], dict)
                )
            except (OSError, ValueError):
                existing_is_object_registry = False
        if target_is_golden or existing_is_object_registry:
            print(
                "ERROR: --write-baseline refuses to overwrite the reviewed object "
                "registry {}: it would clobber the ADR metadata and per-symbol "
                "file/reason/review_by that check_assembly_symbols.py requires. "
                "Pass --baseline <a plain-string JSON> to write a legacy baseline, "
                "or edit the registry by hand with a reviewed exception.".format(
                    display_path(root, baseline_path)
                )
            )
            print("check_feature_boundary: --write-baseline blocked for the golden registry")
            return 1
        errors, _, declarations = check_assembly(root)
        if errors:
            for error in errors:
                print("ERROR: " + error)
            print("check_feature_boundary: cannot write baseline")
            return 1
        symbols = write_baseline(baseline_path, declarations)
        print("check_feature_boundary: wrote {} R2 symbol(s) to {}".format(
            len(symbols), display_path(root, baseline_path)))
        return 0

    errors = []
    summaries = []
    r1_docs = 0
    r2_files = 0
    r2_symbols = 0
    r3_files = 0

    if "r1" in selected:
        documents, doc_errors = resolve_documents(root, args.documents)
        r1_docs = len(documents)
        r1_errors = list(doc_errors)
        try:
            allowed = generic_agent_keys(root)
        except ValueError as exc:
            print("ERROR: " + str(exc))
            print("check_feature_boundary: cannot read the generic agent keys")
            return 1
        for path in documents:
            r1_errors.extend(check_host_document(root, path, allowed))
        errors.extend(r1_errors)
        summaries.append("check_feature_boundary: r1 host_documents={} generic_keys={} errors={}".format(
            r1_docs, len(allowed), len(r1_errors)))

    if "r2" in selected:
        try:
            baseline_symbols = set(load_baseline(baseline_path))
        except ValueError as exc:
            print("ERROR: " + str(exc))
            print("check_feature_boundary: cannot load the R2 baseline")
            return 1
        r2_errors, r2_files, declarations = check_assembly(root)
        r2_symbols = len({name for _, _, name in declarations})
        for rel, lineno, name in declarations:
            if name not in baseline_symbols:
                r2_errors.append(
                    "{}:{}: R2 new feature-resolution symbol {!r} is not in the baseline".format(
                        rel, lineno, name
                    )
                )
        errors.extend(sorted(r2_errors))
        summaries.append("check_feature_boundary: r2 go_files={} symbols={} errors={}".format(
            r2_files, r2_symbols, len(r2_errors)))

    if "r3" in selected:
        r3_errors, r3_files = check_runtime(root)
        errors.extend(sorted(r3_errors))
        summaries.append("check_feature_boundary: r3 go_files={} errors={}".format(
            r3_files, len(r3_errors)))

    for error in errors:
        print("ERROR: " + error)
    for summary in summaries:
        print(summary)
    if errors:
        print("check_feature_boundary: {} error(s)".format(len(errors)))
        return 1
    print("check_feature_boundary: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
