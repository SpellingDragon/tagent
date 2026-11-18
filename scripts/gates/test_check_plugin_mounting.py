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

CONFIG_SPEC_WITHOUT_PLUGINS = """package config

type AgentSpec struct {
	ID string
}
"""

CONFIG_SPEC_WITHOUT_SPEC_BLOCK = """package config

const Version = 1
"""

# The two halves of the host document a rule reads: the dialect that declares the
# key, and the mapping that writes it into the spec.
HOST_DOCUMENT_NO_DIALECT = """package assembly

func (d *hostDocument) mapDocument() (document.Document, error) {
	spec := config.AgentSpec{
		Model:   d.model,
		Plugins: append([]string(nil), d.entry.Plugins...),
	}
	return document.Document{}, nil
}
"""

HOST_DOCUMENT_NO_MAP_DOCUMENT = """package assembly

type hostAgent struct {
	Provider string `yaml:"provider"`
	Plugins  []string `yaml:"plugins"`
}
"""

DECODE_WITHOUT_AGENT_KEYS = """package config

func decodeAgent() {
	a.Plugins = nil
}
"""

ASSEMBLY_WITHOUT_PIPELINE = """package assembly

type Run struct {
	seq uint64
}

func tick(r *Run) { r.seq++ }
"""

# The member a pipeline collection is appended from is declared in four further
# shapes: a long declaration, a literal initialiser, a parameter, and a field
# carrying a struct tag (whose masked string must not hide the type).
ASSEMBLY_VAR_DECLARED = ASSEMBLY + """
func hardcodedVar(r *Run) {
	var extras []plugin.Instance
	extras = append(extras, r.compress)
	_ = extras
}
"""

ASSEMBLY_LITERAL_INITIALIZED = ASSEMBLY + """
func hardcodedLiteral(r *Run) {
	extras := []plugin.Instance{r.compress}
	extras = append(extras, r.ageFold)
	_ = extras
}
"""

ASSEMBLY_PARAMETER = ASSEMBLY + """
func hardcodedParam(r *Run, extras []plugin.Instance) []plugin.Instance {
	return append(extras, r.compress)
}
"""

# A parameter group declares every name of its comma list with the type that
# follows the list, so the first name carries the same judgement as the last.
ASSEMBLY_GROUPED_PARAMETER = ASSEMBLY + """
func hardcodedGrouped(r *Run, a, b []plugin.Instance) {
	a = append(a, r.compress)
	_ = b
}
"""

ASSEMBLY_GROUPED_SPREADS = ASSEMBLY + """
func forwardedGrouped(r *Run, a, b []plugin.Instance) []plugin.Instance {
	a = append(a, r.plugins...)
	return append(b, a...)
}
"""

# A name this file binds from a []plugin.Instance literal holds constructions of
# this file, so spreading it forwards no resolution.
ASSEMBLY_LITERAL_SPREAD = ASSEMBLY + """
func launderSpread(r *Run) {
	extras := []plugin.Instance{r.compress}
	r.plugins = append(r.plugins, extras...)
}
"""

# A spread of a composite literal names a construction rather than a collection.
ASSEMBLY_INLINE_LITERAL_SPREAD = ASSEMBLY + """
func inlineSpread(r *Run) {
	r.plugins = append(r.plugins, []plugin.Instance{r.compress}...)
}
"""

# A slice of a laundered name is still that name's elements.
ASSEMBLY_LITERAL_SLICE_SPREAD = ASSEMBLY + """
func launderSliceSpread(r *Run) {
	extras := []plugin.Instance{r.compress}
	r.plugins = append(r.plugins, extras[:]...)
}
"""

ASSEMBLY_TAGGED_FIELD = """package assembly

type Run struct {
	plugins []plugin.Instance `yaml:"plugins"`
}

func hardcodedTagged(r *Run) {
	r.plugins = append(r.plugins, r.compress)
}
"""

# The registry implementation builds and filters instances, so the element a
# range binds there may be re-emitted; a hardcoded member beside it is not.
REGISTRY_MOUNT_HARDCODED = """package assembly

func (r *Run) hardcoded() {
	r.plugins = append(r.plugins, r.compress)
}
"""

# A local bound from a []plugin.Instance literal holds constructions of this file,
# so ranging over it and re-emitting its elements forwards no resolution; an
# alias of that local is the same source.
REGISTRY_MOUNT_LAUNDERED = """package assembly

func launderElement(r *Run) []plugin.Instance {
	extras := []plugin.Instance{r.compress}
	elements := make([]plugin.Instance, 0, len(extras))
	for _, instance := range extras {
		elements = append(elements, instance)
	}
	return elements
}
"""

REGISTRY_MOUNT_LAUNDERED_ALIAS = """package assembly

func launderAlias(r *Run) []plugin.Instance {
	seed := []plugin.Instance{r.compress}
	extras := seed
	elements := make([]plugin.Instance, 0, len(extras))
	for _, instance := range extras {
		elements = append(elements, instance)
	}
	return elements
}
"""

# The registered registry implementation, present so the entry resolves without
# mounting anything.
REGISTRY_MOUNT_PRESENT = """package assembly

// mountsNothing is the fixture tree's registry implementation: the registered
// entry has to name a file that exists, so the tree carries one.
func mountsNothing() {}
"""

REGISTRY_MOUNT_FILTER = """package assembly

func filter(r *Run, contributed []plugin.Instance) []plugin.Instance {
	elements := make([]plugin.Instance, 0, len(contributed))
	for _, instance := range contributed {
		if instance.Name() != "" {
			elements = append(elements, instance)
		}
	}
	elements = append(elements, r.plugins...)
	return elements
}
"""

# The same filter shape outside the registry implementation: only a spread
# forwards there, so the element-wise re-emit is red.
ASSEMBLY_FILTER_OUTSIDE_REGISTRY = ASSEMBLY + """
func hardcodedFilter(r *Run, contributed []plugin.Instance) {
	elements := make([]plugin.Instance, 0, len(contributed))
	for _, instance := range contributed {
		elements = append(elements, instance)
	}
	_ = elements
}
"""

# An instance collection iterated by name: the field name matches, the element
# type is the plugin instance the pipeline carries, so it is not the declaration.
RUNTIME_INSTANCE_CONSUMER = """package runtime

type NativeAgent struct {
	Plugins []plugin.Instance
}

func deliver(native *NativeAgent) {
	for _, instance := range native.Plugins {
		instance.Name()
	}
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

# An exception that names no review date: the breakthrough protocol asks for one.
EXCEPTION_WITHOUT_REVIEW = {
    "file": "assembly/fixture.go",
    "element": "r.compress",
    "reason": "fixture exception",
    "adr": ADR,
    "user_ruling": "fixture ruling",
}


def architecture(**overrides):
    mounting = dict(ARCHITECTURE["mounting"])
    mounting.update(overrides)
    return {"schema_version": ARCHITECTURE["schema_version"], "mounting": mounting}


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
        "assembly/compress_mount.go": REGISTRY_MOUNT_PRESENT,
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
        "an empty pipeline derivation is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_WITHOUT_PIPELINE},
        expected_rc=1,
        contains=["R5a", "no []plugin.Instance pipeline collection", "type anchor"],
    )

    # A container added in any of the four further declaration shapes joins the
    # judged set without a registry edit, so its hardcoded append is red.
    case(
        "a var-declared pipeline collection is derived",
        sources={"assembly/fixture.go": ASSEMBLY_VAR_DECLARED},
        expected_rc=1,
        contains=["R5a", "concrete member", "r.compress"],
    )

    case(
        "a literal-initialized pipeline collection is derived",
        sources={"assembly/fixture.go": ASSEMBLY_LITERAL_INITIALIZED},
        expected_rc=1,
        contains=["R5a", "concrete member", "r.ageFold"],
    )

    case(
        "a parameter pipeline collection is derived",
        sources={"assembly/fixture.go": ASSEMBLY_PARAMETER},
        expected_rc=1,
        contains=["R5a", "concrete member", "r.compress"],
    )

    # A parameter group carries a comma list of names sharing the type that
    # follows it: the first name is derived as well as the last, and legitimate
    # spreads through either name stay green.
    case(
        "a grouped parameter collection is derived",
        sources={"assembly/fixture.go": ASSEMBLY_GROUPED_PARAMETER},
        expected_rc=1,
        contains=["R5a", "concrete member", "'r.compress'", "errors=1"],
    )

    case(
        "a grouped parameter list keeps every legitimate spread",
        sources={"assembly/fixture.go": ASSEMBLY_GROUPED_SPREADS},
        contains=ok,
        excludes=["R5a"],
    )

    # A forward may not launder a construction of this file: the name a spread
    # carries, or the source a range reads, must not be bound from a
    # []plugin.Instance literal.
    case(
        "a spread of a slice literal bound in this file is not a forwarded element",
        sources={"assembly/fixture.go": ASSEMBLY_LITERAL_SPREAD},
        expected_rc=1,
        contains=[
            "R5a",
            "spread",
            "extras...",
            "binds from a []plugin.Instance literal",
            "errors=1",
        ],
    )

    case(
        "a spread of an inline slice literal is not a forwarded element",
        sources={"assembly/fixture.go": ASSEMBLY_INLINE_LITERAL_SPREAD},
        expected_rc=1,
        contains=[
            "R5a",
            "spread",
            "[]plugin.Instance{r.compress}",
            "errors=1",
        ],
    )

    case(
        "a spread of a slice of a laundered literal is not a forwarded element",
        sources={"assembly/fixture.go": ASSEMBLY_LITERAL_SLICE_SPREAD},
        expected_rc=1,
        contains=[
            "R5a",
            "spread",
            "extras[:]...",
            "binds from a []plugin.Instance literal",
            "errors=1",
        ],
    )

    case(
        "a laundered slice literal is not a forwarded element",
        sources={"assembly/compress_mount.go": REGISTRY_MOUNT_LAUNDERED},
        expected_rc=1,
        contains=[
            "R5a",
            "concrete member",
            "'instance'",
            "forwarded from 'extras'",
            "binds from a []plugin.Instance literal",
            "errors=1",
        ],
    )

    case(
        "an alias of a laundered slice literal is not a forwarded element",
        sources={"assembly/compress_mount.go": REGISTRY_MOUNT_LAUNDERED_ALIAS},
        expected_rc=1,
        contains=[
            "R5a",
            "forwarded from 'extras'",
            "binds from a []plugin.Instance literal",
            "errors=1",
        ],
    )

    case(
        "a tagged pipeline field is still derived",
        sources={"assembly/fixture.go": ASSEMBLY_TAGGED_FIELD},
        expected_rc=1,
        contains=["R5a", "concrete member", "r.compress"],
    )

    # The registry implementation is exempt from the whole-file skip but not from
    # the rule: a forwarded element passes, a hardcoded one is named.
    case(
        "a hardcoded append in a registry implementation file is rejected",
        sources={"assembly/compress_mount.go": REGISTRY_MOUNT_HARDCODED},
        expected_rc=1,
        contains=["assembly/compress_mount.go", "R5a", "concrete member", "r.compress"],
    )

    case(
        "a resolved collection filter in a registry implementation file passes",
        sources={"assembly/compress_mount.go": REGISTRY_MOUNT_FILTER},
        contains=ok + ["mounts=0"],
        excludes=["R5a"],
    )

    case(
        "a forwarded element outside the registry implementation is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_FILTER_OUTSIDE_REGISTRY},
        expected_rc=1,
        contains=["assembly/fixture.go", "R5a", "concrete member", "instance"],
    )

    # The field name alone does not make a consumer: the element type of the
    # field decides, so an instance collection iterated as native.Plugins is
    # named and the declaration stays a dead field. Both C4 emissions must speak:
    # the error count is what shows neither is printed while the other supplies
    # the exit code.
    case(
        "an instance collection is not a declared name consumer",
        sources={"runtime/mount.go": RUNTIME_INSTANCE_CONSUMER},
        expected_rc=1,
        contains=["R5b C4", "native.Plugins", "instance collection", "dead field", "errors=2"],
    )

    # Two rules read the same decoder vocabulary, so both must report: a rule
    # degraded to printing its message would still leave its text in the output,
    # and only the error count shows the gate itself refused.
    case(
        "a declaration outside agentKeys is rejected",
        sources={"api/config/decode.go": DECODE_WITHOUT_KEY},
        expected_rc=1,
        contains=["R5b C1", "mounting.declaration.key", "errors=2"],
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

    case(
        "a mounting gate that names no file is rejected",
        architecture=architecture(gate="scripts/gates/absent.py"),
        expected_rc=1,
        contains=["mounting.gate", "does not exist"],
    )

    case(
        "an invariant without its SHALL clause is rejected",
        architecture=architecture(
            invariant=[
                "装配根 SHALL NOT 直接构造具体特性组件再挂进插件管道。",
                "特性经声明与代际注册表按名挂载。",
                "声明 SHALL 有生产消费方。",
            ]
        ),
        expected_rc=1,
        contains=["mounting.invariant[1]", "SHALL"],
    )

    case(
        "an invariant list shorter than three is rejected",
        architecture=architecture(
            invariant=[
                "装配根 SHALL NOT 直接构造具体特性组件再挂进插件管道。",
                "特性 SHALL 经声明与代际注册表按名挂载。",
            ]
        ),
        expected_rc=1,
        contains=["mounting.invariant must carry the 3 invariants"],
    )

    case(
        "a declaration key no spec field carries is rejected",
        sources={"api/config/config.go": CONFIG_SPEC_WITHOUT_PLUGINS},
        expected_rc=1,
        contains=["names no Plugins field", "errors=3"],
    )

    # Every remaining emission of the gate's own surfaces: the count is asserted
    # so each case fails the moment its rule is degraded to printing, instead of
    # passing on a message another rule also produces.
    case(
        "a spec file without an AgentSpec block is rejected",
        sources={"api/config/config.go": CONFIG_SPEC_WITHOUT_SPEC_BLOCK},
        expected_rc=1,
        contains=["carries no AgentSpec block", "errors=3"],
    )

    case(
        "a registry exception that is not an object is rejected",
        registry=registry(exceptions=["assembly/fixture.go"]),
        expected_rc=1,
        contains=["exception[0]: must be an object", "errors=1"],
    )

    case(
        "a mounting gate that names nothing is rejected",
        architecture=architecture(gate="  "),
        expected_rc=1,
        contains=["mounting.gate must name the checking entry", "errors=1"],
    )

    case(
        "a registry implementation entry outside the assembly scope is rejected",
        architecture=architecture(registry_implementation=["runtime/mount.go"]),
        expected_rc=1,
        contains=["must name a assembly file", "errors=1"],
    )

    case(
        "a registry implementation entry that is not a Go source is rejected",
        architecture=architecture(registry_implementation=["assembly/x.txt"]),
        expected_rc=1,
        contains=["is not a Go source", "errors=1"],
    )

    # A registered file the gate cannot scan is a registration that lies: the
    # exemption resolves to nothing, so the entry has no consumer left.
    case(
        "a registry implementation entry naming a missing file is rejected",
        architecture=architecture(registry_implementation=["assembly/absent_mount.go"]),
        expected_rc=1,
        contains=[
            "mounting.registry_implementation[0]",
            "'assembly/absent_mount.go' does not exist",
            "errors=1",
        ],
    )

    case(
        "a missing declaration surface is rejected",
        sources={"api/config/decode.go": None},
        expected_rc=1,
        contains=["R5b C1", "does not exist", "errors=1"],
    )

    case(
        "a declaration surface without the agentKeys block is rejected",
        sources={"api/config/decode.go": DECODE_WITHOUT_AGENT_KEYS},
        expected_rc=1,
        contains=["R5b C1", "agentKeys block is missing", "errors=1"],
    )

    case(
        "a missing host document is rejected",
        sources={"assembly/host_document.go": None},
        expected_rc=1,
        contains=["R5b C2", "does not exist", "errors=1"],
    )

    case(
        "a host document without the hostAgent dialect is rejected",
        sources={"assembly/host_document.go": HOST_DOCUMENT_NO_DIALECT},
        expected_rc=1,
        contains=["R5b C2", "hostAgent dialect is missing", "errors=1"],
    )

    case(
        "a host document without mapDocument is rejected",
        sources={"assembly/host_document.go": HOST_DOCUMENT_NO_MAP_DOCUMENT},
        expected_rc=1,
        contains=["R5b C3", "mapDocument is missing", "errors=1"],
    )

    case(
        "a declaration key other than the one the rule protects is rejected",
        architecture=architecture(declaration={"key": "plugin"}),
        expected_rc=1,
        contains=["mounting.declaration.key must be 'plugins'"],
    )

    case(
        "a registry whose exceptions are not a list is rejected",
        registry=registry(exceptions={"file": "assembly/fixture.go"}),
        expected_rc=1,
        contains=["'exceptions' must be a list"],
    )

    # Both of these entries are also unmatched, so the stale rule keeps the gate
    # red on its own: the error count is what shows the entry rule itself spoke.
    case(
        "an exception for a file that does not exist is rejected",
        registry=registry(exceptions=[exception("assembly/absent.go", "r.compress")]),
        expected_rc=1,
        contains=["file 'assembly/absent.go' does not exist", "errors=2"],
    )

    case(
        "an exception outside the assembly scope is rejected",
        registry=registry(exceptions=[exception("runtime/mount.go", "r.compress")]),
        expected_rc=1,
        contains=["is outside the assembly/ scope", "errors=2"],
    )

    case(
        "a duplicate registration is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_MEMBER},
        registry=registry(
            exceptions=[
                exception("assembly/fixture.go", "r.compress"),
                exception("assembly/fixture.go", "r.compress"),
            ]
        ),
        expected_rc=1,
        contains=["duplicate registration of 'r.compress'"],
    )

    case(
        "an exception without its review metadata is rejected",
        sources={"assembly/fixture.go": ASSEMBLY_MEMBER},
        registry=registry(exceptions=[EXCEPTION_WITHOUT_REVIEW]),
        expected_rc=1,
        contains=["missing review_by"],
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
