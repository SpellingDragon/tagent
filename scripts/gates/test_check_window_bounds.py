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
import sys

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


def main():
    declared_keys = ["max_tokens", "provider_context_window", "compress_threshold"]
    run("a registered four-class registry is green", None,
        registry(WINDOW, BUDGET, POLICY, CONSTANT), declared_keys, True)
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
    if FAILURES:
        for failure in FAILURES:
            print("FAIL", failure)
        print("%d/%d cases failed" % (len(FAILURES), CASES))
        return 1
    print("%d/%d cases passed" % (CASES, CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
