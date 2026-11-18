#!/usr/bin/env python3
"""Selftests for check_dead_producers.py — one synthetic tree per direction.

Every case builds a whole miniature repository (Go packages, the reviewed registry)
and asks the gate to judge it by its real exit code: a producer a consumer reaches
is green, and every way a producer can stand without one is red by name — the
family whose members only call each other, a reference a skeleton assertion alone
spells, a reference a comment alone spells, an exemption that does not name an
outside consumer, an exemption that matches no dead producer any more, a package
that does not exist, and a scan that finds no production function at all.

A gate that only printed its findings would pass the first case and fail every red
one, so the rejections are the reason these cases run.

契约: golden/runtime/dead_producers.json (the reviewed registry this gate reads)
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_dead_producers.py")
FAILURES = []
CASES = 0

REGISTRY = "golden/runtime/dead_producers.json"
OUTSIDE_CONSUMER = (
    "the interface this producer implements is declared by the standard library, "
    "which calls the method by interface rather than by name.")


def registry(packages=None, exemptions=None):
    return {
        "schema_version": 1,
        "reason": "fixture registry",
        "packages": ["alpha"] if packages is None else packages,
        "exemptions": [] if exemptions is None else exemptions,
    }


def exemption(package, producer, reason=OUTSIDE_CONSUMER):
    return {"package": package, "producer": producer, "reason": reason}


FILES = {
    "alpha/a.go": (
        "package alpha\n"
        "\n"
        "func init() { live() }\n"
        "\n"
        "// live is the producer a boot reaches.\n"
        "func live() {}\n"
    ),
}


def build(files=None, golden=None):
    root = tempfile.mkdtemp(prefix="dead-producers-selftest-")
    for rel, body in (files or FILES).items():
        write(root, rel, body)
    if golden is not False:
        write(root, REGISTRY, json.dumps(golden if golden is not None else registry(),
                                         ensure_ascii=False, indent=2))
    return root


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def judge(root):
    result = subprocess.run([sys.executable, CHECKER, "--root", root],
                            capture_output=True, text=True, check=False)
    return result.returncode, result.stdout + result.stderr


def run(name, want, files=None, golden=None):
    global CASES
    CASES += 1
    code, output = judge(build(files, golden))
    if want is None:
        if code != 0:
            FAILURES.append("%s: want green, got exit %d and %r" % (name, code, output))
            return
        print("PASS", name)
        return
    if code == 0:
        FAILURES.append("%s: want red naming %r, got exit 0 and %r" % (name, want, output))
        return
    if want not in output:
        FAILURES.append("%s: want an error naming %r, got %r" % (name, want, output))
        return
    print("PASS", name)


def with_files(extra):
    files = dict(FILES)
    files.update(extra)
    return files


CLUSTER = {
    "alpha/cluster.go": (
        "package alpha\n"
        "\n"
        "func left() { right() }\n"
        "\n"
        "func right() { left() }\n"
    ),
}

CONSUMERS = {
    "alpha/helper_test.go": (
        "package alpha\n"
        "\n"
        "import \"testing\"\n"
        "\n"
        "func TestHelper(t *testing.T) { helper() }\n"
    ),
}

HELPER = {"alpha/helper.go": "package alpha\n\nfunc helper() {}\n"}

SKELETON = {"alpha/skeleton_assert.go": "package alpha\n\nvar _ = helper\n"}

COMMENT = {"alpha/helper.go": "package alpha\n\n// helper is reached by the boot path.\nfunc helper() {}\n"}

INTERFACE = {
    "alpha/port.go": (
        "package alpha\n"
        "\n"
        "type port interface {\n"
        "\tserve()\n"
        "}\n"
        "\n"
        "type adapter struct{}\n"
        "\n"
        "func (a adapter) serve() {}\n"
    ),
}

CROSS_PACKAGE = {
    "beta/b.go": "package beta\n\nimport \"alpha\"\n\nfunc init() { alpha.Exported() }\n",
}

EXPORTED = {"alpha/exported.go": "package alpha\n\nfunc Exported() {}\n"}

NO_GO = {"alpha/notes.md": "the package holds no Go file\n"}


def main():
    run("a producer a consumer reaches is green", None)

    run("a family whose members only call each other is red",
        "alpha/left has no consumer", with_files(CLUSTER))

    run("a family member is named by its own verdict, not only its entry point",
        "alpha/right has no consumer", with_files(CLUSTER))

    run("a test call is a consumer", None, with_files(dict(HELPER, **CONSUMERS)))

    run("a skeleton assertion is not a consumer",
        "alpha/helper has no consumer", with_files(dict(HELPER, **SKELETON)))

    run("a mention in a comment is not a consumer",
        "alpha/helper has no consumer", with_files(COMMENT))

    run("a name an interface declares is a consumer", None, with_files(INTERFACE))

    run("a call from another package is a consumer",
        None, with_files(dict(EXPORTED, **CROSS_PACKAGE)))

    run("a reviewed exemption for an outside consumer is green", None,
        with_files(HELPER),
        golden=registry(exemptions=[exemption("alpha", "helper")]))

    run("an exemption that matches no dead producer is red",
        "remove the stale exemption",
        golden=registry(exemptions=[exemption("alpha", "live")]))

    run("an exemption whose reason does not name the consumer is red",
        "reason must state the consumer outside this repository",
        with_files(HELPER),
        golden=registry(exemptions=[exemption("alpha", "helper", reason="known")]))

    run("an exemption naming an unscanned package is red",
        "is not one of the scanned packages",
        with_files(HELPER),
        golden=registry(exemptions=[exemption("gamma", "helper")]))

    run("a scanned package that does not exist is red",
        "the scanned package 'gamma' does not exist",
        golden=registry(packages=["gamma"]))

    run("a scan that finds no production function is red",
        "the scan found no production function", NO_GO)

    run("a tree with no registry is red",
        "registry not found", golden=False)

    if FAILURES:
        for failure in FAILURES:
            print("FAIL", failure)
        print("%d/%d cases failed" % (len(FAILURES), CASES))
        return 1
    print("%d/%d cases passed" % (CASES, CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
