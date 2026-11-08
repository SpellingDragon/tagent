#!/usr/bin/env python3
"""Selftests for check_window_bounds.py - synthetic registries and key sets.

Each case proves a direction: a registered four-class registry is green; an
unregistered knob, a second window, a constant that tries to be a declaration
key, a constant gone stale and a registered key missing from the surface are
each red by name.
"""
from __future__ import annotations

import importlib.util
import os
import pathlib
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_window_bounds.py")

FAILURES = []
CASES = 0


def load_checker():
    spec = importlib.util.spec_from_file_location("check_window_bounds", CHECKER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def bound(class_, name, kind=None, source=None):
    entry = {"id": name, "class": class_, "name": name, "kind": kind, "file": "x.go", "reason": "case"}
    if source is not None:
        entry["source"] = source
    return entry


WINDOW = bound("window", "max_tokens", "config_key", "external: the model window")
BUDGET = bound("budget", "provider_context_window", "config_key")
POLICY = bound("policy", "compress_threshold", "config_key")
CONSTANT = bound("constant", "factWindow", "const")


def registry(*bounds):
    return {"bounds": list(bounds)}


def run(name, want_error, registry_value, keys, declares):
    global CASES
    CASES += 1
    checker = load_checker()
    errors = checker.check(registry_value, set(keys), lambda entry: declares)
    if want_error:
        if not any(want_error in error for error in errors):
            FAILURES.append("%s: want an error naming %r, got %r" % (name, want_error, errors))
            return
    elif errors:
        FAILURES.append("%s: want no error, got %r" % (name, errors))
        return
    print("PASS", name)


def scan_case():
    """A camelCase declaration key is scanned and then refused by the constant rule."""
    global CASES
    CASES += 1
    checker = load_checker()
    with tempfile.TemporaryDirectory() as tmp:
        decode = pathlib.Path(tmp) / "api" / "config" / "decode.go"
        decode.parent.mkdir(parents=True)
        decode.write_text(
            'package config' + chr(10) + chr(10) +
            'var agentKeys = []string{' + chr(10) + chr(9) + '"factWindow",' + chr(10) + '}' + chr(10),
            encoding="utf-8")
        keys = checker.scan_config_keys(tmp)
    if "factWindow" not in keys:
        FAILURES.append("scan_case: the camelCase declaration key never reached the key set")
        return
    errors = checker.check(registry(WINDOW, BUDGET, POLICY, CONSTANT), keys, lambda entry: True)
    if not any("must not be a declaration key" in error for error in errors):
        FAILURES.append("scan_case: want the constant rule to refuse factWindow, got %r" % (errors,))
        return
    print("PASS a camelCase declaration key is scanned and refused")


def reverse_scan_case():
    """A second copy of a registered constant's value is red beside it.

    Two directions: the file that already holds a registered constant and a
    singular-root sibling is refused by name, and the same file with the sibling
    gone is green. The singular root is the point: a packing ratio restated as
    <unit>PerToken is exactly the copy the reverse scan exists to catch.
    """
    global CASES
    CASES += 1
    checker = load_checker()
    cases = (
        ("const factWindow = 4096" + chr(10) + "const bytesPerToken = 4" + chr(10), "bytesPerToken"),
        ("const factWindow = 4096" + chr(10), None),
    )
    for body, want in cases:
        with tempfile.TemporaryDirectory() as tmp:
            source = pathlib.Path(tmp) / "x.go"
            source.write_text("package x" + chr(10) + chr(10) + body, encoding="utf-8")
            errors = checker.check_registered_files(
                registry(WINDOW, BUDGET, POLICY, CONSTANT),
                checker.scan_registered_files(tmp, registry(WINDOW, BUDGET, POLICY, CONSTANT)),
            )
        if want is None:
            if errors:
                FAILURES.append("reverse_scan_case: want no error without the second copy, got %r" % (errors,))
                return
            continue
        if not any(want in error for error in errors):
            FAILURES.append("reverse_scan_case: want an error naming %r, got %r" % (want, errors))
            return
    print("PASS a restated singular-root constant beside a registered one is red")


def main():
    declared_keys = ["max_tokens", "provider_context_window", "compress_threshold"]
    run("a registered four-class registry is green", None,
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys, True)
    run("a window-shaped knob by another spelling is red", "replay_window",
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys + ["replay_window"], True)
    run("an unregistered knob is red", "new_context_window",
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys + ["new_context_window"], True)
    run("a second window is red", "want exactly one",
        registry(WINDOW, dict(WINDOW, id="window2", name="second_window"),
                 BUDGET, POLICY, CONSTANT), declared_keys, True)
    run("a constant cannot be a declaration key", "must not be a declaration key",
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys + ["factWindow"], True)
    run("a stale constant is red", "gone from x.go",
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys, False)
    run("a registered key missing from the surface is red", "absent from the declaration surface",
        registry(WINDOW, BUDGET, POLICY, CONSTANT), ["max_tokens", "provider_context_window"], True)
    run("an unknown class is red", "is not one of",
        registry(WINDOW, {**POLICY, "class": "knob"}, CONSTANT), declared_keys, True)
    scan_case()
    reverse_scan_case()
    if FAILURES:
        for failure in FAILURES:
            print("FAIL", failure)
        print("%d/%d cases failed" % (len(FAILURES), CASES))
        return 1
    print("%d/%d cases passed" % (CASES, CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
