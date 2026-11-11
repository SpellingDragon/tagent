#!/usr/bin/env python3
"""check_window_bounds.py - every window, budget, policy and constant is registered.

Rule: the declaration surface and the product sources may name a window, a
request budget, a compression policy or a mechanism constant, but each one must
be registered in golden/runtime/window_bounds.json with its class:

  window     the one replay window (max_tokens); exactly one, and it is external
  budget     request budget face (provider window, output reserve); declared,
             defaults derived from the replay window
  policy     adjustable compression/retention/approval policy
  constant   mechanism constant (conversion, page bound, algorithm, memory cap);
             never a declaration key

Classes are not interchangeable. A declaration key that names a window, budget,
policy or bound must be registered; a constant must never be a declaration key;
a registered constant must still be declared in its file; and a file that
already holds a registered family constant must not grow an unregistered one.
The last scan is file-scoped: a family constant introduced in a file the registry
does not name is outside this gate and relies on review. The registry has no
auto-write mode, so a new knob cannot appear silently.

Exit 0 when every side is registered and consistent, 1 on violations or
unusable input.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

REGISTRY = os.path.join("golden", "runtime", "window_bounds.json")
CLASSES = ("window", "budget", "policy", "constant")
DECLARED_CLASSES = ("window", "budget", "policy")

# A declaration key matching this asks to tune how much context the system
# carries; it must be registered under one of the declared classes.
KNOB = re.compile(r"(max_tokens|window|tokens|compress_threshold|keep_recent|reserve|top_k|rrf_k|budget_(?!seconds))", re.I)

DECODE = os.path.join("api", "config", "decode.go")
HOST_DOCUMENT = os.path.join("assembly", "host_document.go")

AGENT_KEYS = re.compile(r"var agentKeys = \[\]string\{(.*?)\n\}", re.S)
SECTION_KEYS = re.compile(r"sectionFields\([^)]*\)", re.S)
YAML_TAG = re.compile(r'yaml:"([a-z_]+)"')
QUOTED = re.compile(r'"([A-Za-z_][A-Za-z0-9_]*)"')
CONST_ONE = re.compile(r'^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)\s+(?:[A-Za-z_][A-Za-z0-9_.\[\]]*\s*)?=\s*[0-9]', re.M)
CONST_BLOCK = re.compile(r'^\s*const\s*\((.*?)^\s*\)', re.S | re.M)
CONST_MEMBER = re.compile(r'^\s*([A-Za-z_][A-Za-z0-9_]*)\s+(?:[A-Za-z_][A-Za-z0-9_.\[\]]*\s*)?=\s*[0-9]', re.M)
# The roots a conversion, page or framing constant is named under. Both numbers
# and both spellings of the counted unit belong here: a second copy of the
# packing ratio is typically called <unit>PerToken or tokensPer<Unit>, so a root
# list holding only the plural would pass the singular restatement it exists to
# catch (runtime/agent/fuse.go carried exactly that copy).
FAMILY = re.compile(r"(window|budget|reserve|tokens|token|threshold|limit|topk|rrf)", re.I)


def load_registry(path):
    """Read the registry and refuse a shape this checker cannot judge."""
    with open(path, encoding="utf-8") as handle:
        registry = json.load(handle)
    if not isinstance(registry.get("bounds"), list):
        raise ValueError("registry carries no bounds list")
    return registry


def scan_config_keys(root):
    """Answer every declaration key the agent surface names."""
    keys = set()
    decode = os.path.join(root, DECODE)
    if os.path.exists(decode):
        text = open(decode, encoding="utf-8").read()
        for block in AGENT_KEYS.findall(text):
            keys.update(QUOTED.findall(block))
        for call in SECTION_KEYS.findall(text):
            keys.update(QUOTED.findall(call))
    document = os.path.join(root, HOST_DOCUMENT)
    if os.path.exists(document):
        keys.update(YAML_TAG.findall(open(document, encoding="utf-8").read()))
    return keys


def file_declares(root, relative, name):
    """Answer whether the named file still spells the constant."""
    path = os.path.join(root, relative)
    if not os.path.exists(path):
        return False
    return re.search(r"\b%s\b" % re.escape(name), open(path, encoding="utf-8").read()) is not None


def scan_registered_files(root, registry):
    """Answer every family constant declared in the files the registry owns.

    The reverse direction of the registration rule: a file that already holds a
    registered window/budget/policy constant must not grow a new one silently.
    Constants elsewhere in the tree are outside this scan; the docstring says so.
    """
    found = {}
    for bound in registry.get("bounds") or []:
        relative = bound.get("file") or ""
        if not relative or relative in found:
            continue
        path = os.path.join(root, relative)
        if not os.path.exists(path):
            found[relative] = set()
            continue
        text = open(path, encoding="utf-8").read()
        names = set(CONST_ONE.findall(text))
        for block in CONST_BLOCK.findall(text):
            names.update(CONST_MEMBER.findall(block))
        found[relative] = {name for name in names if FAMILY.search(name)}
    return found


def check_registered_files(registry, declarations):
    """Judge the reverse direction: every family constant beside a registered one is registered."""
    errors = []
    registered = {bound.get("name") for bound in registry.get("bounds") or []}
    for relative, names in sorted(declarations.items()):
        for name in sorted(names):
            if name not in registered:
                errors.append(
                    "%s: the %s constant lives beside a registered one but is not registered"
                    % (name, relative))
    return errors


def check(registry, keys, declares):
    """Judge one registry against one declaration surface."""
    errors = []
    bounds = registry.get("bounds") or []
    seen = set()
    windows = 0
    registered = set()
    for bound in bounds:
        name = bound.get("name") or ""
        if not name:
            errors.append("an entry names nothing")
            continue
        if name in seen:
            errors.append("%s: registered twice" % name)
        seen.add(name)
        class_ = bound.get("class")
        if class_ not in CLASSES:
            errors.append("%s: class %r is not one of %s" % (name, class_, ", ".join(CLASSES)))
            continue
        if not (bound.get("reason") or "").strip():
            errors.append("%s: entry carries no reason" % name)
        kind = bound.get("kind")
        if class_ == "constant":
            if kind != "const":
                errors.append("%s: a mechanism constant must be a const entry" % name)
            if name in keys:
                errors.append("%s: a mechanism constant must not be a declaration key" % name)
            if not declares(bound):
                errors.append("%s: the declared constant is gone from %s" % (name, bound.get("file")))
            continue
        registered.add(name)
        if class_ == "window":
            windows += 1
            if bound.get("source") != "external: the model window":
                errors.append("%s: the replay window must name the external source" % name)
        if kind != "config_key":
            errors.append("%s: a %s entry must be a declaration key" % (name, class_))
        if name not in keys:
            errors.append("%s: registered as %s but absent from the declaration surface" % (name, class_))
    if windows != 1:
        errors.append("the registry names %d replay windows, want exactly one" % windows)
    for key in sorted(keys):
        if KNOB.search(key) and key not in registered:
            errors.append(
                "%s: a window/budget/policy key is not registered; add it to %s with its class"
                % (key, REGISTRY))
    return errors


def main(argv):
    parser = argparse.ArgumentParser(description="check the window/budget/policy registry")
    parser.add_argument("--root", default=".", help="tree to scan")
    parser.add_argument("--registry", default=None, help="registry path override")
    args = parser.parse_args(argv)
    root = os.path.abspath(args.root)
    registry_path = args.registry or os.path.join(root, REGISTRY)
    try:
        registry = load_registry(registry_path)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print("check_window_bounds: unusable registry %s: %s" % (registry_path, error))
        return 1
    keys = scan_config_keys(root)
    errors = check(registry, keys,
                   lambda bound: file_declares(root, bound.get("file") or "", bound.get("name") or ""))
    errors += check_registered_files(registry, scan_registered_files(root, registry))
    for error in errors:
        print("check_window_bounds: %s" % error)
    if errors:
        return 1
    declared = sum(1 for bound in registry["bounds"] if bound.get("class") in DECLARED_CLASSES)
    constants = len(registry["bounds"]) - declared
    print("check_window_bounds: %d classes, %d declared, %d constants, %d declaration keys scanned"
          % (len(CLASSES), declared, constants, len(keys)))
    print("check_window_bounds: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
