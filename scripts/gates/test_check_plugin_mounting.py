#!/usr/bin/env python3
"""Selftests for check_plugin_mounting.py - tempfile fixtures, subprocess only.

Each case proves a direction: a forwarded or assigned collection stays green, a
concrete member or a composite literal is red, a registered exception passes and
a stale one is rejected, each of the four liveness conditions has its own red
direction, and the golden registration cannot be silently absent or incomplete.
A rule demoted to printing only fails these cases, and the lint wiring runs this
file before the gate for exactly that reason.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_plugin_mounting.py")
FAILURES = []
CASES = 0

ADR = "docs/adr/guard.md"
FUTURE = "2999-12-31"

ASSEMBLY = """package assembly

type Run struct {
	plugins []plugin.Instance
}

func pipeline(r *Run, declared []plugin.Instance) []plugin.Instance {
	elements := append([]plugin.Instance(nil), declared...)
	elements = append(elements, r.plugins...)
	return elements
}
"""

ASSEMBLY_ASSIGNED = """package assembly

type Run struct {
	plugins []plugin.Instance
}

func wire(r *Run, resolved []plugin.Instance) {
	r.plugins = resolved
}
"""

ASSEMBLY_MEMBER = ASSEMBLY + """
func hardcoded(r *Run) {
	r.plugins = append(r.plugins, r.compress)
}
"""

ASSEMBLY_LITERAL = ASSEMBLY + """
func hardcoded(r *Run) {
	elements := make([]plugin.Instance, 0)
	elements = append(elements,
		observePlugin{observer: r})
}
"""

HOST_DOCUMENT = """package assembly

type hostAgent struct {
	Provider string `yaml:"provider"`
	Plugins  []string `yaml:"plugins"`
}

func (d *hostDocument) mapDocument() (document.Document, error) {
	spec := config.AgentSpec{
		Model:   d.model,
		Plugins: append([]string(nil), d.entry.Plugins...),
	}
	return document.Document{}, nil
}
"""

HOST_DOCUMENT_NO_KEY = """package assembly

type hostAgent struct {
	Provider string `yaml:"provider"`
}

func (d *hostDocument) mapDocument() (document.Document, error) {
	spec := config.AgentSpec{
		Model:   d.model,
		Plugins: append([]string(nil), d.entry.Plugins...),
	}
	return document.Document{}, nil
}
"""

HOST_DOCUMENT_NO_MAPPING = """package assembly

type hostAgent struct {
	Provider string `yaml:"provider"`
	Plugins  []string `yaml:"plugins"`
}

func (d *hostDocument) mapDocument() (document.Document, error) {
	spec := config.AgentSpec{
		Model: d.model,
	}
	return document.Document{}, nil
}
"""

DECODE = """package config

var agentKeys = []string{
	"id", "model", "plugins",
}

func decodeAgent() {
	a.Plugins = nil
}
"""

DECODE_WITHOUT_KEY = """package config

var agentKeys = []string{
	"id", "model",
}

func decodeAgent() {
	a.Plugins = nil
}
"""

CONFIG_SPEC = """package config

type AgentSpec struct {
	ID      string
	Plugins []string
}
"""

RUNTIME_MOUNT = """package runtime

func mounts(spec config.AgentSpec) []plugin.Instance {
	out := make([]plugin.Instance, 0, len(spec.Plugins))
	for _, name := range spec.Plugins {
		out = append(out, lookup(name))
	}
	return out
}
"""

# A change note compares the declared names; it iterates nothing, so it is not a
# consumer even though a range appears on the same line.
RUNTIME_MOUNT_CHANGE_NOTE = """package runtime

func noteChange(before, after config.AgentSpec, changed []string) {
	note("plugins", !reflect.DeepEqual(before.Plugins, after.Plugins)); for _, name := range before.Plugins { changed = append(changed, name) }
}
"""

ARCHITECTURE = {
    "schema_version": 1,
    "mounting": {
        "invariant": [
            "装配根 SHALL NOT 直接构造具体特性组件再挂进插件管道。",
            "特性 SHALL 经声明与代际注册表按名挂载。",
            "声明 SHALL 有生产消费方。",
        ],
        "gate": "scripts/gates/check_plugin_mounting.py",
        "declaration": {"key": "plugins"},
        "registry_implementation": ["assembly/compress_mount.go"],
    },
}


def exception(file_value, element, **overrides):
    entry = {
        "file": file_value,
        "element": element,
        "reason": "fixture exception",
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
    }
    entry.update(overrides)
    return entry


def registry(exceptions=None, **overrides):
    data = {
        "schema_version": 1,
        "adr": ADR,
        "user_ruling": "fixture ruling",
        "review_by": FUTURE,
        "reason": "fixture registry",
        "exceptions": [] if exceptions is None else exceptions,
    }
    data.update(overrides)
    return data


def write(root, rel, text):
    path = os.path.join(root, rel)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


def build_fixture(root, **overrides):
    write(root, ADR, "# fixture ADR\n")
    write(root, "scripts/gates/check_plugin_mounting.py", "# fixture gate\n")
    sources = {
        "assembly/fixture.go": ASSEMBLY,
        "assembly/host_document.go": HOST_DOCUMENT,
        "api/config/decode.go": DECODE,
        "api/config/config.go": CONFIG_SPEC,
        "runtime/mount.go": RUNTIME_MOUNT,
    }
    sources.update(overrides.get("sources", {}))
    for rel, text in sources.items():
        if text is None:
            continue
        write(root, rel, text)
    write(
        root,
        "golden/runtime/architecture.json",
        json.dumps(overrides.get("architecture", ARCHITECTURE), indent=2) + "\n",
    )
    write(
        root,
        "golden/runtime/plugin_mounts.json",
        json.dumps(overrides.get("registry", registry()), indent=2) + "\n",
    )


def run_checker(root):
    command = [
        sys.executable,
        CHECKER,
        "--root",
        root,
        "--registry",
        "golden/runtime/plugin_mounts.json",
    ]
    proc = subprocess.run(
        command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120
    )
    return proc.returncode, proc.stdout


def report(name, problems, output):
    global CASES
    CASES += 1
    if problems:
        FAILURES.append(name)
        print("FAIL " + name)
        for problem in problems:
            print("  " + problem)
        print("  --- checker output ---")
        for line in output.splitlines():
            print("  " + line)
    else:
        print("PASS " + name)


def verdict(name, expected_rc, contains, excludes, rc, output):
    problems = []
    if rc != expected_rc:
        problems.append("exit {} != {}".format(rc, expected_rc))
    for needle in contains:
        if needle not in output:
            problems.append("missing {!r}".format(needle))
    for needle in excludes:
        if needle in output:
            problems.append("unexpected {!r}".format(needle))
    report(name, problems, output)


def case(name, expected_rc=0, contains=(), excludes=(), **overrides):
    with tempfile.TemporaryDirectory() as root:
        build_fixture(root, **overrides)
        rc, output = run_checker(root)
        verdict(name, expected_rc, contains, excludes, rc, output)


def main():
    ok = ["check_plugin_mounting: ok"]

    case("spread forwarding passes", contains=ok, excludes=["R5a", "R5b"])

    case(
        "declared instances assignment passes",
        sources={"assembly/fixture.go": ASSEMBLY_ASSIGNED},
        contains=ok,
        excludes=["R5a"],
    )

    case(
        "a single member append is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_MEMBER},
        expected_rc=1,
        contains=["R5a", "concrete member", "r.compress"],
    )

    case(
        "a composite literal append is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_LITERAL},
        expected_rc=1,
        contains=["R5a", "composite literal", "observePlugin"],
    )

    case(
        "a registered exception passes",
        sources={"assembly/fixture.go": ASSEMBLY_MEMBER},
        registry=registry(exceptions=[exception("assembly/fixture.go", "r.compress")]),
        contains=ok + ["exceptions=1"],
        excludes=["R5a"],
    )

    case(
        "a stale exception is rejected",
        registry=registry(exceptions=[exception("assembly/fixture.go", "r.gone")]),
        expected_rc=1,
        contains=["R5a", "r.gone", "no longer matches a mount"],
    )

    case(
        "a declaration outside agentKeys is rejected",
        sources={"api/config/decode.go": DECODE_WITHOUT_KEY},
        expected_rc=1,
        contains=["R5b C1", "plugins"],
    )

    case(
        "a host dialect without the plugins key is rejected",
        sources={"assembly/host_document.go": HOST_DOCUMENT_NO_KEY},
        expected_rc=1,
        contains=["R5b C2", "hostAgent"],
    )

    case(
        "a spec mapping without Plugins is rejected",
        sources={"assembly/host_document.go": HOST_DOCUMENT_NO_MAPPING},
        expected_rc=1,
        contains=["R5b C3", "config.AgentSpec"],
    )

    case(
        "a change note is not a consumer",
        sources={"runtime/mount.go": RUNTIME_MOUNT_CHANGE_NOTE},
        expected_rc=1,
        contains=["R5b C4", "dead field"],
    )

    case(
        "a range consumer passes",
        contains=ok + ["consumers=1"],
        excludes=["R5b"],
    )

    case(
        "an unregistered mounting invariant is rejected",
        architecture={"schema_version": 1},
        expected_rc=1,
        contains=["mounting is not registered"],
    )

    with tempfile.TemporaryDirectory() as root:
        problems, output = [], ""
        for index, field in enumerate(("adr", "user_ruling", "review_by")):
            data = registry()
            del data[field]
            inner = os.path.join(root, "case{}".format(index))
            build_fixture(inner, registry=data)
            rc, output = run_checker(inner)
            if rc != 1:
                problems.append("{} removed: exit {} != 1".format(field, rc))
            if ("missing " + field) not in output and ("missing '" + field) not in output:
                problems.append("{} removed: the checker did not name it".format(field))
        report("mounting registry metadata is required", problems, output)

    if FAILURES:
        print("test_check_plugin_mounting: {} case(s) FAILED".format(len(FAILURES)))
        return 1
    print("check_plugin_mounting selftest: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
