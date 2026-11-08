#!/usr/bin/env python3
"""Shared primitives for the ADR 0006 three-hook guard checkers.

The guards are deliberately dependency-free (Python standard library only): a
YAML library present on one developer machine is not guaranteed on CI, so the
schema checker reuses the indentation parser the earlier feature-boundary gate
already carries instead of importing PyYAML.

Nothing here decides a rule; each checker owns its registry and its verdicts.
This module only holds the byte-level helpers they share: reading files,
walking the product tree, masking Go comments/strings, locating a byte offset's
line, and validating the metadata every reviewed exception must carry.
"""
from __future__ import annotations

import datetime
import json
import os
import re

SCHEMA_VERSION = 1


def read_text(path):
    with open(path, "r", encoding="utf-8") as handle:
        return handle.read()


def rel_path(root, path):
    return os.path.relpath(path, root).replace(os.sep, "/")


def display_path(root, path):
    rel = rel_path(root, path)
    return path if rel.startswith("..") else rel


def go_files(root, dirs, include_tests=False):
    """Every production Go file under the named product directories."""
    found = []
    skip = {"node_modules", "vendor", "testdata", "__pycache__"}
    for name in dirs:
        base = os.path.join(root, name)
        if not os.path.isdir(base):
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = sorted(
                d for d in dirnames if d not in skip and not d.startswith(".")
            )
            for filename in sorted(filenames):
                if not filename.endswith(".go"):
                    continue
                if not include_tests and filename.endswith("_test.go"):
                    continue
                found.append(os.path.join(dirpath, filename))
    return sorted(found)


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
            elif ch == chr(96):
                state = "raw"
            i += 1
        elif state in ("string", "rune"):
            if ch == "\\":
                i += 2
            else:
                if (state == "string" and ch == '"') or (state == "rune" and ch == "'"):
                    state = "code"
                i += 1
        else:  # raw string
            if ch == chr(96):
                state = "code"
            i += 1
    return "".join(out)


def scan_go_strings(text):
    """Blank Go strings and runes while preserving code, newlines and offsets.

    The returned text is the same length as the input, so an offset in it is an
    offset in the original source and line numbers stay exact.
    """
    out = list(text)
    i, n = 0, len(text)
    state = "code"
    while i < n:
        ch = text[i]
        if state == "code":
            if ch == '"':
                out[i] = " "
                state = "string"
            elif ch == "'":
                out[i] = " "
                state = "rune"
            elif ch == chr(96):
                out[i] = " "
                state = "raw"
            i += 1
        elif state in ("string", "rune"):
            out[i] = " "
            if ch == "\\":
                if i + 1 < n:
                    out[i + 1] = " "
                i += 2
            else:
                if (state == "string" and ch == '"') or (state == "rune" and ch == "'"):
                    state = "code"
                i += 1
        else:
            out[i] = " "
            if ch == chr(96):
                state = "code"
            i += 1
    return "".join(out)


def line_of(text, offset):
    return text.count("\n", 0, offset) + 1


def load_json(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except FileNotFoundError:
        raise ValueError("registry not found: {}".format(path))
    except OSError as exc:
        raise ValueError("cannot read registry {}: {}".format(path, exc))
    except json.JSONDecodeError as exc:
        raise ValueError("registry {} is not valid JSON: {}".format(path, exc))
    except UnicodeError as exc:
        raise ValueError("registry {} is not valid UTF-8: {}".format(path, exc))


def require_field(data, key, label, errors, kind=None):
    value = data.get(key)
    if value is None or (isinstance(value, str) and not value.strip()):
        errors.append("{}: missing '{}'".format(label, key))
        return None
    if kind is not None and not isinstance(value, kind):
        errors.append("{}: '{}' has the wrong type".format(label, key))
        return None
    return value


def check_review_by(value, label, errors, today=None):
    if not isinstance(value, str) or not value.strip():
        errors.append("{}: missing review_by".format(label))
        return
    try:
        deadline = datetime.date.fromisoformat(value)
    except ValueError:
        errors.append("{}: review_by {!r} is not an ISO date".format(label, value))
        return
    today = today or datetime.date.today()
    if deadline < today:
        errors.append(
            "{}: review_by {} has passed; re-review the exception or remove it".format(
                label, value
            )
        )


def check_adr(root, value, label, errors):
    if not isinstance(value, str) or not value.strip():
        errors.append("{}: missing adr".format(label))
        return
    path = value if os.path.isabs(value) else os.path.join(root, value)
    if not os.path.isfile(path):
        errors.append("{}: adr {!r} does not exist".format(label, value))


def validate_guard_metadata(data, root, label, errors, require_reason=True):
    """Validate the fields every reviewed guard registry must carry."""
    if not isinstance(data, dict):
        errors.append("{}: registry must be a JSON object".format(label))
        return
    version = data.get("schema_version")
    if version != SCHEMA_VERSION:
        errors.append(
            "{}: schema_version must be {}, got {!r}".format(
                label, SCHEMA_VERSION, version
            )
        )
    check_adr(root, data.get("adr"), label, errors)
    require_field(data, "user_ruling", label, errors, str)
    check_review_by(data.get("review_by"), label, errors)
    if require_reason:
        require_field(data, "reason", label, errors, str)


def validate_exception_metadata(entry, root, label, errors):
    """Validate one exception entry: reason, ADR, user ruling and review date."""
    require_field(entry, "reason", label, errors, str)
    require_field(entry, "user_ruling", label, errors, str)
    check_adr(root, entry.get("adr"), label, errors)
    check_review_by(entry.get("review_by"), label, errors)
