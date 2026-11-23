#!/usr/bin/env python3
"""Selftests for check_dod_coverage.py - one synthetic gap per coverage direction.

Each case builds a whole miniature repository (criteria source, predicate registry,
CI workflow, frozen mutation registry, anchor test, and the run product the mutation
gate writes) and asks the gate to judge it: a fully bound and proven registry is
green, and every gap this gate exists for is red by name - a proxy anchor that never
judges the named measure, an anchor that judges it only at a noise bound, a criterion
with no registry entry, a registry entry the design no longer states, a mutation that
is unregistered, a mutation whose run is recorded as surviving, a verdict stated only
by the registry's own field, an anchor test that does not exist, and a CI gate the
workflow does not run.

Two directions are stated here because they are what a run product can and cannot
carry. The assertion text a run records is the frozen registry's, never the
criterion's own name, so the green case records that text and a kill recorded under
any other text is red. A kill is attributed by text alone - the anchor's own body
states the text, and the run names the anchor test as the one that failed - so a
literal a neighbouring test of the same file prints, a run naming another test, and a
run naming no test are red, and an edit above the assertion never enters either tie.

A criterion whose measurement is a gate instead of a test names the gate and its
selftest. The gate has to exist, read the named measure, fail rather than print, and
come with a selftest that reads a gate exit beside zero: each of those absences is red
by name, so a path that is not there, a gate that only reports, and a selftest that
cannot tell a rejection from a pass never read as a bound criterion.

契约: golden/runtime/dod_predicates.json (the predicate registry this gate reads)
"""
from __future__ import annotations

import importlib.util
import io
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_dod_coverage.py")
SCRIPTS = pathlib.Path(HERE)
REPO = SCRIPTS.parents[1]
MODULE = "github.com/SpellingDragon/tagent"
TOOL_ENTRYPOINT = pathlib.Path("scripts") / "scenarios" / "run_runtime_scenarios.py"

FAILURES = []
CASES = 0


def load_runner():
    """The producer whose digests the fixtures record, imported not re-implemented."""
    scenarios = str(SCRIPTS.parent / "scenarios")
    if scenarios not in sys.path:
        sys.path.insert(0, scenarios)
    import run_runtime_scenarios
    return run_runtime_scenarios


runner = load_runner()

MUTATION_ID = "MUT-FIXTURE-cold-unbounded"
GATE_MUTATION_ID = "MUT-FIXTURE-gate-clause"
# The criterion's own name for the assertion it requires: a name, never the text a
# run records, so the two are deliberately different strings here.
ASSERTION = "wallclock_flat_across_generations"
ANCHOR_TEST = "TestFixtureAnchor"
# The text the frozen mutation registry registers, and so the text the run records.
KILL_TEXT = "the wall clock of the boot grew beside the second boot"
# The gate fixture's own report text: a gate kill is recorded under what the gate
# states, so the registered text stands in the gate's source.
GATE_KILL_TEXT = "fixture gate: clean"
EVIDENCE_PATH = "evidence/evidence.json"
GATE_PATH = "scripts/gates/fixture_gate.py"
GATE_SELFTEST_PATH = "scripts/gates/test_fixture_gate.py"

DESIGN = """# Design: fixture

## D1 第一性原理与纯粹判据

- **P1 单一真相**：链唯一真相，一身份一不可变事实。
- **P4 有界恢复**：boot 墙钟不随历史线性增长（真量，非正文计数代理量）。

## D2 子域表
"""

CI = """name: ci
jobs:
  test:
    runs-on: ubuntu-latest
  event-integrity-long:
    runs-on: ubuntu-latest
"""

MUTATIONS = """#!/usr/bin/env python3
from dataclasses import dataclass


@dataclass(frozen=True)
class Mutation:
    id: str
    expect_assertion: str


MUTATIONS = (
    Mutation(id="MUT-FIXTURE-other", expect_assertion="another assertion"),
)
"""

ANCHOR_MEASURE = """package assembly

import "testing"

func TestFixtureAnchor(t *testing.T) {
\tobserved := walk(t)
\tfor generation := 2; generation < 5; generation++ {
\t\tif held := observed[generation].journalBytes; held > observed[1].journalBytes*2 {
\t\t\tt.Fatalf("generation %d replayed %d bytes", generation, held)
\t\t}
\t\tif second, last := observed[1].wallClock, observed[4].wallClock; last > second*2 {
\t\t\tt.Fatalf("the wall clock of the boot grew beside the second boot")
\t\t}
\t}
}
"""

ANCHOR_PROXY = """package assembly

import "testing"

func TestFixtureAnchor(t *testing.T) {
\tobserved := walk(t)
\tfor generation := 2; generation < 5; generation++ {
\t\tif held := observed[generation].journalBytes; held > observed[1].journalBytes*2 {
\t\t\tt.Fatalf("generation %d replayed %d bytes", generation, held)
\t\t}
\t}
\tt.Logf("the boot paid %v", observed[1].wallClock)
}
"""

ANCHOR_NOISE = """package assembly

import "testing"

func TestFixtureAnchor(t *testing.T) {
\tconst wallClockTolerance = 8
\tobserved := walk(t)
\tif second, last := observed[1].wallClock, observed[4].wallClock; last > second*wallClockTolerance {
\t\tt.Fatalf("the wall clock of the boot grew beside the second boot")
\t}
}
"""

# The registered text stands in the same file but in another test's body: the kill it
# reports is that test's, so this anchor cannot claim it.
ANCHOR_NEIGHBOUR = """package assembly

import "testing"

func TestFixtureNeighbour(t *testing.T) {
\tobserved := walk(t)
\tif last := observed[4].wallClock; last > observed[1].wallClock*2 {
\t\tt.Fatalf("the wall clock of the boot grew beside the second boot")
\t}
}

func TestFixtureAnchor(t *testing.T) {
\tobserved := walk(t)
\tif last := observed[4].wallClock; last > observed[1].wallClock*2 {
\t\tt.Fatalf("the anchor paid %d", last)
\t}
}
"""

GATE = """#!/usr/bin/env python3
import sys


def dead_producers(root):
    return []


def main(argv=None):
    if dead_producers("."):
        return 1
    print("fixture gate: clean")
    return 0


if __name__ == "__main__":
    sys.exit(main())
"""

GATE_SELFTEST = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def judge():
    return subprocess.run([sys.executable, CHECKER], capture_output=True).returncode


def main():
    code = judge()
    if code != 0:
        print("FAIL the gate rejected a clean tree")
        return 1
    print("PASS")
    return 0
"""

# A gate that reports its finding and exits zero judges nothing.
GATE_PRINT_ONLY = GATE.replace("        return 1\n", "        print(\"fixture gate: dead\")\n")

# A gate that never reads the measure the criterion names is bound to another reading.
GATE_NO_MEASURE = GATE.replace("def dead_producers(root):", "def scan(root):").replace(
    "    if dead_producers(\".\"):", "    if scan(\".\"):")

# A selftest that never reads an exit beside zero cannot tell a rejection from a pass.
GATE_SELFTEST_NO_EXIT = GATE_SELFTEST.replace("    if code != 0:", "    if code:")

# The exit code crosses a helper as one slot of a returned tuple: the comparison
# is judged by the binding's source, whatever the bound name is called.
GATE_SELFTEST_TUPLE_HELPER = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def run_checker():
    proc = subprocess.run([sys.executable, CHECKER], capture_output=True)
    return proc.returncode, proc.stdout


def main():
    rc, output = run_checker()
    if rc != 0:
        print("FAIL the gate rejected a clean tree")
        return 1
    print("PASS")
    return 0
"""

GATE_SELFTEST_DIRECT_ATTR = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def main():
    proc = subprocess.run([sys.executable, CHECKER], capture_output=True)
    if proc.returncode != 0:
        print("FAIL the gate rejected a clean tree")
        return 1
    print("PASS")
    return 0
"""

GATE_SELFTEST_EQ_ZERO = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def run_checker():
    proc = subprocess.run([sys.executable, CHECKER], capture_output=True)
    return proc.returncode, proc.stdout


def main():
    rc, output = run_checker()
    if rc == 0:
        print("PASS")
        return 0
    print("FAIL the gate rejected a clean tree")
    return 1
"""

GATE_SELFTEST_PRINT_ONLY = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def main():
    proc = subprocess.run([sys.executable, CHECKER], capture_output=True)
    print(proc.stdout)
    return 0
"""

GATE_SELFTEST_UNRELATED = """#!/usr/bin/env python3
import subprocess
import sys

CHECKER = "fixture_gate.py"


def main():
    proc = subprocess.run([sys.executable, CHECKER], capture_output=True)
    count = len(proc.stdout)
    if count != 0:
        print("FAIL the gate printed a finding")
        return 1
    print("PASS")
    return 0
"""

GATE_SELFTEST_NEQ_ONE = GATE_SELFTEST_TUPLE_HELPER.replace("    if rc != 0:", "    if rc != 1:")

GATE_SELFTEST_CONSTANT_HELPER = GATE_SELFTEST_TUPLE_HELPER.replace(
    "    return proc.returncode, proc.stdout", "    return 0, proc.stdout")

GATE_SELFTEST_BROKEN = "def main(:\n"


def load_checker():
    spec = importlib.util.spec_from_file_location("check_dod_coverage", CHECKER)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def predicate(identifier, title, mutation_id=MUTATION_ID, ci_gate="test", bound=2,
              must_assert=ASSERTION, gate=None, selftest=GATE_SELFTEST_PATH):
    if gate is not None:
        anchor = {"must_assert": must_assert, "measure_token": "dead_producers"}
        if gate is not False:
            anchor["gate"] = gate
        if selftest is not None:
            anchor["selftest"] = selftest
    else:
        anchor = {"test": ANCHOR_TEST, "must_assert": must_assert,
                  "measure_token": "wallClock"}
        if bound is not None:
            anchor["bound"] = {"factor": bound, "vs": "the second boot"}
    return {
        "id": identifier,
        "criterion": "{} {}".format(identifier, title),
        "measure": "wallClock",
        "scope": "the boot of the fixture chain",
        "anchor": anchor,
        "mutation": {"id": mutation_id, "must_be_killed": True},
        "ci_gate": ci_gate,
    }


def registry(predicates, verdicts=None, criteria_paths=None):
    return {
        "schema_version": 1,
        "criteria_source": {
            "paths": criteria_paths or ["fixture-spec/design.md"],
            "pattern": "^- \\*\\*(?P<id>P\\d+) (?P<title>[^*]+)\\*\\*：(?P<text>.+)$",
        },
        "predicates": list(predicates),
        "mutation_verdicts": [] if verdicts is None else list(verdicts),
    }


def build(tmp, registry_value, anchor=ANCHOR_MEASURE, mutations=MUTATIONS,
          design=DESIGN, ci=CI, anchor_path="assembly/anchor_test.go",
          evidence=True, extra_files=None, tool_copy=False, binding_from=None):
    root = pathlib.Path(tmp)
    write(root / "fixture-spec" / "design.md", design)
    write(root / "golden" / "runtime" / "dod_predicates.json",
          json.dumps(registry_value, ensure_ascii=False, indent=2))
    write(root / ".github" / "workflows" / "ci.yml", ci)
    write(root / "scripts" / "gates" / "check_event_integrity_mutations.py", mutations)
    if anchor is not None:
        write(root / anchor_path, anchor)
    for rel, text in (extra_files or {}).items():
        if text is not None:
            write(root / rel, text)
    source_tree(root, tool_copy=tool_copy)
    if evidence:
        # The run product is written last: its recorded closure digests the bytes
        # this fixture has now, which is what the gate reads it against.
        binding = source_binding(root if binding_from is None else binding_from)
        write(root / EVIDENCE_PATH,
              json.dumps(run_product(registry_value.get("mutation_verdicts") or [], binding),
                         ensure_ascii=False, indent=2))
    return root


def source_tree(root, main="package app\n", tool_copy=False):
    """The module tree a run product describes, laid out as the repository is."""
    write(root / "go.mod", "module {}\n".format(MODULE))
    write(root / "go.sum", "")
    write(root / "main.go", main)
    if tool_copy:
        # The consumer imports the producer from the tree it judges, so a fixture
        # whose entry point is exercised as a program carries the real module.
        (root / TOOL_ENTRYPOINT).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(REPO / TOOL_ENTRYPOINT, root / TOOL_ENTRYPOINT)
    else:
        write(root / TOOL_ENTRYPOINT, "# fixture producer stand-in\n")


def source_binding(root):
    """The closure the producer records for `root`, as the consumer recomputes it."""
    binding = runner.source_binding(str(root), [{"dir": ".", "module_path": MODULE}],
                                    revision="0" * 40)
    window = {"digest": binding["digest"], "modules": len(binding["modules"])}
    binding["before"] = window
    binding["after"] = dict(window)
    return binding


def drift(root):
    """Move a compiled byte after the run product that describes it was written."""
    write(root / "main.go", "package app\n\n// a later edit the product never saw\n")


def strip_binding(root):
    """Drop the closure from a run product, leaving the verdicts it states."""
    path = root / EVIDENCE_PATH
    payload = json.loads(path.read_text(encoding="utf-8"))
    payload.pop("source_binding", None)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


def restate_product(**fields):
    """State `fields` on a run product's top level, leaving its records alone."""
    def rewrite(root):
        path = root / EVIDENCE_PATH
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload.update(fields)
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    return rewrite


def restate_first_record(**fields):
    """State `fields` on a run product's first mutation record."""
    def rewrite(root):
        path = root / EVIDENCE_PATH
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["mutations"][0].update(fields)
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    return rewrite


def write(path, text):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def judge(root, evidence=True):
    checker = load_checker()
    argv = ["--root", str(root)]
    if evidence:
        argv += ["--mutation-evidence", str(root / "evidence")]
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = checker.main(argv)
    return code, out.getvalue() + err.getvalue()


def run(name, want, registry_value, anchor=ANCHOR_MEASURE, mutations=MUTATIONS,
        design=DESIGN, ci=CI, anchor_path="assembly/anchor_test.go", evidence=True,
        extra_files=None, tool_copy=False, binding_from=None, after=None):
    global CASES
    CASES += 1
    with tempfile.TemporaryDirectory() as tmp:
        root = build(tmp, registry_value, anchor, mutations, design, ci, anchor_path,
                     evidence, extra_files, tool_copy=tool_copy, binding_from=binding_from)
        if after is not None:
            after(root)
        code, output = judge(root, evidence)
    if want is None:
        if code != 0:
            FAILURES.append("%s: want green, got exit %d and %r" % (name, code, output))
            return
        print("PASS", name)
        return
    if code == 0:
        FAILURES.append("%s: want red naming %r, got exit 0" % (name, want))
        return
    # A tuple states every message the case requires: a refusal that did not also
    # stop the verdicts from counting would satisfy a single substring alone.
    unwoven = (want,) if isinstance(want, str) else tuple(want)
    for message in unwoven:
        if message not in output:
            FAILURES.append("%s: want an error naming %r, got %r" % (name, message, output))
            return
    print("PASS", name)


def killed(assertion=KILL_TEXT, verdict="killed", mutation_id=MUTATION_ID,
           test_name=ANCHOR_TEST, gate=None):
    return [{"id": mutation_id, "verdict": verdict, "assertion": assertion,
             "test_name": test_name, "gate": gate,
             "killing_test": "assembly/anchor_test.go#TestFixtureAnchor",
             "evidence": "synthetic: the fixture records the kill"}]


def run_product(verdicts, binding):
    """The report `check_event_integrity_mutations.py --out` writes.

    The fixtures state their verdicts once, here: a criterion is proven by what a
    run recorded, so the registry's own `mutation_verdicts` field is a claim beside
    it, never its source. The assertion text and the test a record names are the
    frozen registry's, which is how a run product states them. A gate verdict
    names the gate it judged instead of a test. The report also carries the
    closure it was written against, because a product that does not describe the
    tree under judgement proves nothing about it.
    """
    records = []
    for entry in verdicts:
        verdict = entry.get("verdict")
        gate = entry.get("gate")
        records.append({
            "id": entry.get("id"),
            "status": verdict,
            "verdict": verdict,
            "reason": "named_assertion" if verdict == "killed" else "assertion_not_matched",
            "counted_killed": verdict == "killed",
            "expect_assertion": entry.get("assertion"),
            "assertion_site": "assembly/anchor_test.go:9",
            "assertion_hit": verdict == "killed",
            "test": {"module": ".", "package": "assembly",
                     "file": "assembly/anchor_test.go",
                     "name": entry.get("test_name"),
                     "selector": "^{}$".format(entry.get("test_name") or "")},
            "gate": {"path": gate, "selftest": GATE_SELFTEST_PATH} if gate else None,
        })
    return {
        "schema_version": 2,
        "profile": "event-integrity",
        "mode": "live",
        "control_tree_green": True,
        "not_evidence_backed": [],
        "source_binding": binding,
        "mutations": records,
    }


def registered(assertion=KILL_TEXT):
    return MUTATIONS.replace(
        'Mutation(id="MUT-FIXTURE-other", expect_assertion="another assertion"),',
        'Mutation(id="MUT-FIXTURE-other", expect_assertion="another assertion"),\n'
        '    Mutation(id="%s", expect_assertion="%s"),' % (MUTATION_ID, assertion))


def registered_gate(gate_assertion=GATE_KILL_TEXT):
    return registered().replace(
        '    Mutation(id="%s", expect_assertion="%s"),' % (MUTATION_ID, KILL_TEXT),
        '    Mutation(id="%s", expect_assertion="%s"),\n'
        '    Mutation(id="%s", expect_assertion="%s"),'
        % (MUTATION_ID, KILL_TEXT, GATE_MUTATION_ID, gate_assertion))


def anchor_gaps(entry, files=None):
    """The gaps check_anchor states for one predicate over a fixture tree.

    A gate anchor's binding is judged on its own here and in the rejections below;
    its kill attribution is judged by the end-to-end cases, which carry the run
    product's gate verdict.
    """
    with tempfile.TemporaryDirectory() as tmp:
        root = build(tmp, registry([entry]), extra_files=files)
        checker = load_checker()
        gaps = []
        checker.check_anchor(entry, root, gaps)
    return gaps


def run_anchor(name, want, entry, files=None):
    """One anchor judged alone, so its gap has to be the reason the case is red.

    An end-to-end case cannot tell a rejection from a print: the registry's other gaps
    keep the exit code red either way, so a gate anchor that only printed its finding
    would read as red here too. These cases therefore read the gap list itself.
    """
    global CASES
    CASES += 1
    gaps = anchor_gaps(entry, files)
    if want is None:
        if gaps:
            FAILURES.append("%s: want green, got %r" % (name, gaps))
            return
        print("PASS", name)
        return
    if not any(want in gap for gap in gaps):
        FAILURES.append("%s: want a gap naming %r, got %r" % (name, want, gaps))
        return
    print("PASS", name)


# The formal shell entries drive the wiring, so the wiring is judged from the shell.
#
# The acceptance of this leaf is a command (`bash scripts/lint.sh`,
# `bash scripts/preflight.sh`), not a helper call: what has to hold is the argv the
# entry produces, the order the chain runs in and the exit it propagates. These
# cases therefore build a miniature repository whose two entries and whose DoD and
# evidence checkers are the real files, stand in for every other gate, and read the
# argv the entry actually ran. The predicate registry, the criteria source, the CI
# workflow, the anchor test and the run product are the fixtures the cases above
# already use, so a green here means the same thing a green there means.

REAL = (
    "scripts/lint.sh",
    "scripts/preflight.sh",
    "scripts/gates/lint.sh",
    "scripts/gates/preflight.sh",
    "scripts/gates/check_dod_coverage.py",
    "scripts/gates/check_runtime_evidence.py",
    "scripts/gates/runtime_checklib.py",
)

REFERENCE = re.compile(r"(?:python3|bash)\s+[\"']?(scripts/[\w./-]+)")

STUB_PY = """#!/usr/bin/env python3
import sys

print("stub: {}".format(" ".join(sys.argv[1:])))
sys.exit(0)
"""

STUB_SH = """#!/usr/bin/env bash
echo "stub: $*"
exit 0
"""

# The interpreter the entry calls is shimmed, not replaced: every argv the chain
# hands to python3 is recorded and then executed for real, so the record is what the
# entry ran and the checker that ran is the checker in the tree.
PYTHON_SHIM = """#!/bin/sh
printf '%s\\n' "python3 $*" >> "$TAGENT_FIXTURE_RECORD"
exec {python} "$@"
"""

# `go run ./scripts/codetools ...` and the comment-policy measurement both go
# through this one: the measurement has to print a parsable zero, the rest just
# passes, and neither touches a toolchain this fixture does not have.
GO_SHIM = """#!/bin/sh
printf '%s\\n' "go $*" >> "$TAGENT_FIXTURE_RECORD"
if [ "$1" = "version" ]; then
  echo "go version go1.23.0 darwin/arm64"
  exit 0
fi
printf '%s\\n' "comment_policy: 0 finding(s)."
exit 0
"""

# The mutations generator, stood in for: it declares the frozen registry this gate
# parses, and when preflight calls it with --out it writes the run product of that
# run instead of driving a matrix this fixture cannot drive.
PRODUCER_MAIN = """

def main(argv=None):
    import argparse
    import os
    import shutil
    import sys

    parser = argparse.ArgumentParser()
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args(argv)
    if not args.out:
        print("producer stand-in: no --out given", file=sys.stderr)
        return 2
    os.makedirs(args.out, exist_ok=True)
    product = os.path.join(args.out, "evidence.json")
    shutil.copy(os.environ["TAGENT_FIXTURE_EVIDENCE"], product)
    print("producer stand-in: wrote {}".format(product))
    return 0


if __name__ == "__main__":
    sys.exit(main())
"""

# The binding step, stood in for, and it is not a rubber stamp: it fails unless the
# product the gate consumes is already on disk when it runs. Its exit is what places
# it after generation and before consumption. The DoD gate imports this module for
# the source binding the real checker owns, so the real one is imported beside it
# rather than the binding being written a second time.
BINDING_STUB = """#!/usr/bin/env python3
import os
import sys

from runtime_evidence_impl import *  # noqa: F401,F403


def main(argv=None):
    args = sys.argv if argv is None else argv
    bundle = args[args.index("--bundle") + 1]
    product = os.path.join(bundle, "mutations", "evidence.json")
    present = os.path.isfile(product)
    print("binding stand-in: bundle={} product_present={}".format(bundle, present))
    return 0 if present else 1


if __name__ == "__main__":
    sys.exit(main())
"""

# Python 3.12 fails the discovery step when it finds no test at all, so the fixture
# carries the one module the pattern matches.
UNIT_TEST_STUB = """import unittest


class FixtureChain(unittest.TestCase):
    def test_the_fixture_chain_is_installed(self):
        self.assertTrue(True)
"""

# The consumer imports the runner for its digest and closure definitions; the shell
# entries call it as a program. This tree has no golden contract to drive, so the
# program entry states that instead of failing the chain.
RUNNER_SHIM = '''#!/usr/bin/env python3
"""The real runner module, with a program entry that drives no matrix."""
import sys

from runtime_runner_impl import *  # noqa: F401,F403

if __name__ == "__main__":
    print("fixture runner: no scenario matrix was driven")
    sys.exit(0)
'''


def referenced(repo):
    """Every script the two shell entries name, as the entries name it."""
    found = set()
    for relative in ("scripts/gates/lint.sh", "scripts/gates/preflight.sh"):
        found.update(REFERENCE.findall((repo / relative).read_text(encoding="utf-8")))
    return sorted(found)


def producer_module():
    """The stand-in generator: the frozen registry plus the product it would write."""
    return registered().replace("from dataclasses import dataclass",
                                "import sys\nfrom dataclasses import dataclass") + PRODUCER_MAIN


def write_product(path, root, verdicts):
    """The run product a producer writes for `root`, bound to it."""
    write(path, json.dumps(run_product(verdicts, source_binding(root)),
                           ensure_ascii=False, indent=2))


def shell_repo(tmp, verdicts, claimed=None, stub_binding=False):
    """A repository whose shell entries are the real ones and whose gates are stubs."""
    base = pathlib.Path(tmp)
    root = base / "repo"
    root.mkdir()
    bound = [predicate("P1", "单一真相"), predicate("P4", "有界恢复")]
    build(root, registry(bound, killed() if claimed is None else claimed),
          mutations=registered(), tool_copy=True, evidence=False)
    for relative in REAL:
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(REPO / relative, target)
    for relative in ("examples/wechat-bot", "examples/cli-host"):
        write(root / relative / "go.mod", "module {}/{}\n".format(MODULE, relative))
    stubbed = {"scripts/gates/check_runtime_evidence.py"} if stub_binding else set()
    if stub_binding:
        shutil.copy(root / "scripts" / "gates" / "check_runtime_evidence.py",
                    root / "scripts" / "gates" / "runtime_evidence_impl.py")
    for relative in referenced(REPO):
        target = root / relative
        if relative in stubbed:
            write(target, BINDING_STUB)
            continue
        if relative in REAL or target.exists():
            continue
        write(target, STUB_SH if target.suffix == ".sh" else STUB_PY)
    write(root / "scripts" / "gates" / "check_event_integrity_mutations.py", producer_module())
    write(root / "scripts" / "gates" / "test_check_runtime_chain.py", UNIT_TEST_STUB)
    write(root / "scripts" / "gates" / "test_check_doc_budget.py", UNIT_TEST_STUB)
    write(root / "scripts" / "gates" / "check_doc_budget.py", STUB_PY)
    scenarios = root / "scripts" / "scenarios"
    shutil.copy(scenarios / "run_runtime_scenarios.py", scenarios / "runtime_runner_impl.py")
    write(scenarios / "run_runtime_scenarios.py", RUNNER_SHIM)
    bins = base / "bin"
    bins.mkdir()
    write(bins / "python3", PYTHON_SHIM.format(python=sys.executable))
    write(bins / "go", GO_SHIM)
    for name in ("python3", "go"):
        (bins / name).chmod(0o755)
    write_product(base / "producer-evidence.json", root, verdicts)
    return root


def shell_entry(root, argv, env_extra=None):
    """Run one formal shell entry in the fixture, returning its result and its argv."""
    base = root.parent
    record = base / "record.log"
    if record.exists():
        record.unlink()
    env = dict(os.environ)
    env.pop("TAGENT_RUNTIME_EVIDENCE_DIR", None)
    env["PATH"] = str(base / "bin") + os.pathsep + env["PATH"]
    env["TAGENT_FIXTURE_RECORD"] = str(record)
    env["TAGENT_FIXTURE_EVIDENCE"] = str(base / "producer-evidence.json")
    env.update(env_extra or {})
    proc = subprocess.run(["bash"] + list(argv), cwd=str(root), env=env,
                          capture_output=True, text=True, timeout=900)
    return proc, record.read_text(encoding="utf-8") if record.exists() else ""


def expect(name, held, detail):
    global CASES
    CASES += 1
    if held:
        print("PASS", name)
        return
    FAILURES.append("%s: %s" % (name, detail))


def told(proc, want):
    """The entry's own words, both streams, for a case that wants a named refusal."""
    return want in (proc.stdout + proc.stderr)


def steps(record, *markers):
    """Where each marker first appears in the recorded argv, verbatim."""
    return [record.find(marker) for marker in markers]


# 归档前门的正式入口是 scripts/gates/archive-preflight.sh：`--commit` 判的是"这个
# 提交带了什么"，所以负例必须真造一个把路径放进归档树的提交，而不是调 helper。
ARCHIVE_GATE = pathlib.Path("scripts") / "gates" / "archive-preflight.sh"
ARCHIVE_TREE_DIR = "/".join(["openspec", "changes", "archive", "2026-01-01-fixture"])


def git(root, *args):
    """Run one git command inside the fixture repository."""
    proc = subprocess.run(["git", "-c", "user.name=fixture",
                           "-c", "user.email=fixture@example.invalid"] + list(args),
                          cwd=str(root), capture_output=True, text=True)
    if proc.returncode != 0:
        raise AssertionError("git {} failed: {}".format(" ".join(args), proc.stderr))
    return proc.stdout.strip()


def archive_repo(tmp):
    """A git tree carrying the real archive gate plus one clean base commit."""
    root = pathlib.Path(tmp) / "repo"
    (root / "scripts" / "gates").mkdir(parents=True)
    shutil.copy(REPO / ARCHIVE_GATE, root / ARCHIVE_GATE)
    git(root, "init", "-q")
    write(root / "README.md", "fixture\n")
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "base")
    return root


def archive_commit(root, relative):
    """Add one path in its own commit and return that commit's id."""
    write(root / relative, "fixture\n")
    git(root, "add", "-A")
    git(root, "commit", "-q", "-m", "add {}".format(relative))
    return git(root, "rev-parse", "HEAD")


def run_archive_gate(root, *args, evidence=None):
    """The real gate, run from the fixture root with no inherited evidence pointer."""
    env = {key: value for key, value in os.environ.items()
           if key != "TAGENT_RUNTIME_EVIDENCE_DIR"}
    if evidence is not None:
        env["TAGENT_RUNTIME_EVIDENCE_DIR"] = str(evidence)
    return subprocess.run(["bash", str(ARCHIVE_GATE)] + list(args), cwd=str(root),
                          capture_output=True, text=True, env=env)


class ArchivePreflightTest(unittest.TestCase):
    """归档树的新增路径必须绑定同一提交的运行产物，否则归档前门拒绝。"""

    def test_archive_tree_change_without_evidence_is_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = archive_repo(tmp)
            # 控制：同一棵树、同样的调用，提交只加一条非归档路径——门明说"没有对象"。
            control = archive_commit(root, "docs/notes.md")
            proc = run_archive_gate(root, "--commit", control, "--base", control + "^")
            self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
            self.assertIn("adds no path under the archive tree", proc.stdout)

            # 负例：提交把路径放进归档树，而没有任何证据目录——拒绝并点名该路径。
            archived = "{}/proposal.md".format(ARCHIVE_TREE_DIR)
            candidate = archive_commit(root, archived)
            proc = run_archive_gate(root, "--commit", candidate, "--base", candidate + "^")
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("adds 1 archived path(s)", proc.stdout)
            self.assertIn(archived, proc.stdout)
            self.assertIn("REFUSED", proc.stderr)
            self.assertIn("no evidence directory", proc.stderr)

            # 证据目录存在但没有运行产物：仍拒绝，并点名缺的那个产物。
            empty = pathlib.Path(tmp) / "empty-run"
            empty.mkdir()
            proc = run_archive_gate(root, "--commit", candidate, "--base", candidate + "^",
                                    evidence=empty)
            self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
            self.assertIn("holds no mutations/evidence.json", proc.stderr)


def test_formal_lint_consumes_bound_mutation_evidence():
    """The shell entry hands the DoD gate this run's product, and the gate reads it.

    Green here is not a stub saying yes: the checker is the real one, and without
    the directory it would have no verdicts at all - the registry's own field is
    refused - so exit 0 is the product being consumed.
    """
    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed())
        evidence_dir = pathlib.Path(tmp) / "run"
        write_product(evidence_dir / "mutations" / "evidence.json", root, killed())
        proc, record = shell_entry(root, ("scripts/lint.sh", "--only", "contracts"),
                                   {"TAGENT_RUNTIME_EVIDENCE_DIR": str(evidence_dir)})
        argv = "scripts/gates/check_dod_coverage.py --mutation-evidence {}".format(
            evidence_dir / "mutations")
        expect("the formal lint entry reaches the DoD gate with this run's product",
               proc.returncode == 0 and argv in record,
               "exit {}; argv {!r}; output {!r}".format(
                   proc.returncode, record, (proc.stdout + proc.stderr)[-3000:]))
        expect("the DoD gate proved both criteria from that product",
               "dod_coverage: 2/2 criteria bound and proven" in proc.stdout,
               "output {!r}".format((proc.stdout + proc.stderr)[-3000:]))


def test_formal_lint_refuses_missing_stale_or_handwritten_evidence():
    """Four ways to have no product for this tree, each red by name, none exit 0."""
    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed())
        proc, record = shell_entry(root, ("scripts/lint.sh", "--only", "contracts"))
        expect("the formal lint entry names an unset evidence directory and fails",
               proc.returncode != 0 and told(proc, "TAGENT_RUNTIME_EVIDENCE_DIR is unset")
               and "scripts/gates/check_dod_coverage.py " not in record,
               "exit {}; argv {!r}; output {!r}".format(
                   proc.returncode, record, (proc.stdout + proc.stderr)[-3000:]))

    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed())
        empty = pathlib.Path(tmp) / "empty-run"
        empty.mkdir()
        proc, record = shell_entry(root, ("scripts/lint.sh", "--only", "contracts"),
                                   {"TAGENT_RUNTIME_EVIDENCE_DIR": str(empty)})
        expect("the formal lint entry names a directory with no product and fails",
               proc.returncode != 0 and told(proc, "holds no mutations/ product")
               and "scripts/gates/check_dod_coverage.py " not in record,
               "exit {}; output {!r}".format(
                   proc.returncode, (proc.stdout + proc.stderr)[-3000:]))

    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed())
        evidence_dir = pathlib.Path(tmp) / "run"
        product = evidence_dir / "mutations" / "evidence.json"
        write_product(product, root, killed())
        drift(root)
        proc, record = shell_entry(root, ("scripts/lint.sh", "--only", "contracts"),
                                   {"TAGENT_RUNTIME_EVIDENCE_DIR": str(evidence_dir)})
        expect("the formal lint entry refuses a product from another tree, by name",
               proc.returncode != 0 and told(proc, "the evidence describes another source tree")
               and "dod_coverage: 2/2" not in proc.stdout,
               "exit {}; output {!r}".format(
                   proc.returncode, (proc.stdout + proc.stderr)[-3000:]))

    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed())
        evidence_dir = pathlib.Path(tmp) / "run"
        write_product(evidence_dir / "mutations" / "evidence.json", root, [])
        proc, record = shell_entry(root, ("scripts/lint.sh", "--only", "contracts"),
                                   {"TAGENT_RUNTIME_EVIDENCE_DIR": str(evidence_dir)})
        expect("the formal lint entry refuses a registry that hands its own verdicts in",
               proc.returncode != 0
               and told(proc, "stated only by this registry's own field")
               and "dod_coverage: 2/2" not in proc.stdout,
               "exit {}; output {!r}".format(
                   proc.returncode, (proc.stdout + proc.stderr)[-3000:]))


def test_preflight_generates_evidence_before_dod_gate():
    """Generation, then binding, then consumption - in that order, in one run.

    The binding stand-in fails unless the product is already on disk, and the DoD
    gate is the real one reading the real product the generator wrote, so the record
    below is a chain and not three independent calls that happen to appear nearby.
    """
    with tempfile.TemporaryDirectory() as tmp:
        root = shell_repo(tmp, killed(), stub_binding=True)
        evidence_dir = pathlib.Path(tmp) / "evidence"
        proc, record = shell_entry(root, ("scripts/preflight.sh",),
                                   {"TAGENT_RUNTIME_EVIDENCE_DIR": str(evidence_dir)})
        produced = steps(record, "check_event_integrity_mutations.py --all --json --out "
                                 "{}".format(evidence_dir / "mutations"))[0]
        bound = steps(record, "check_runtime_evidence.py")[0]
        consumed = steps(record, "check_dod_coverage.py --mutation-evidence {}".format(
            evidence_dir / "mutations"))[0]
        expect("preflight generated the product before it bound the evidence",
               produced >= 0 and bound > produced,
               "generated at {}; bound at {}; argv {!r}".format(produced, bound, record))
        expect("preflight bound the evidence before the DoD gate consumed it",
               bound >= 0 and consumed > bound,
               "bound at {}; consumed at {}; argv {!r}".format(bound, consumed, record))
        expect("the binding step saw the product the generator had written",
               proc.returncode == 0
               and "binding stand-in: bundle={} product_present=True".format(evidence_dir)
               in proc.stdout,
               "exit {}; output {!r}".format(
                   proc.returncode, (proc.stdout + proc.stderr)[-3000:]))
        expect("the DoD gate proved both criteria from the product preflight produced",
               "dod_coverage: 2/2 criteria bound and proven" in proc.stdout,
               "output {!r}".format((proc.stdout + proc.stderr)[-3000:]))


def main():
    global CASES
    bound = [predicate("P1", "单一真相"), predicate("P4", "有界恢复")]
    gate_bound = [predicate("P1", "单一真相"),
                  predicate("P4", "有界恢复", gate=GATE_PATH)]
    gate_files = {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST}

    run("a fully bound and proven registry is green", None,
        registry(bound, killed()), mutations=registered())

    # The assertion text a run records is the frozen registry's; recording it is what
    # makes a kill attributable, so this is the direction a slug comparison breaks.
    run("a kill recorded under the frozen assertion text is green", None,
        registry(bound, killed(assertion=KILL_TEXT)), mutations=registered())

    run_anchor("a gate anchor whose gate reads the measure, fails, and is selftested is bound",
               None, predicate("P4", "有界恢复", gate=GATE_PATH), gate_files)

    run_anchor("an anchor gate that does not exist is red",
               "the anchor gate scripts/gates/fixture_gate.py does not exist",
               predicate("P4", "有界恢复", gate=GATE_PATH), {GATE_SELFTEST_PATH: GATE_SELFTEST})

    run_anchor("a gate that never reads the named measure is red",
               "never reads the measure dead_producers",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE_NO_MEASURE, GATE_SELFTEST_PATH: GATE_SELFTEST})

    run_anchor("a gate that reports without failing is red",
               "states no failing exit",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE_PRINT_ONLY, GATE_SELFTEST_PATH: GATE_SELFTEST})

    run_anchor("a gate anchor that names no selftest is red",
               "names no selftest",
               predicate("P4", "有界恢复", gate=GATE_PATH, selftest=None), gate_files)

    run_anchor("a gate anchor whose selftest does not exist is red",
               "the anchor gate's selftest scripts/gates/test_fixture_gate.py does not exist",
               predicate("P4", "有界恢复", gate=GATE_PATH), {GATE_PATH: GATE})

    run_anchor("a selftest that never compares a gate exit against zero is red",
               "never compares a gate exit against zero",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_NO_EXIT})

    run_anchor("a selftest binding a name from a helper's returned exit slot is bound",
               None, predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_TUPLE_HELPER})

    run_anchor("a selftest comparing the process result's returncode directly is bound",
               None, predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_DIRECT_ATTR})

    run_anchor("a selftest reading the exit beside zero with == is bound",
               None, predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_EQ_ZERO})

    run_anchor("a selftest that only prints the gate output is red",
               "never compares a gate exit against zero",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_PRINT_ONLY})

    run_anchor("a selftest comparing an unrelated quantity against zero is red",
               "never compares a gate exit against zero",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_UNRELATED})

    run_anchor("a selftest comparing the exit only against one is red",
               "never compares a gate exit against zero",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_NEQ_ONE})

    run_anchor("a selftest whose helper returns a constant instead of the exit is red",
               "never compares a gate exit against zero",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_CONSTANT_HELPER})

    run_anchor("a selftest that does not parse is red",
               "does not parse",
               predicate("P4", "有界恢复", gate=GATE_PATH),
               {GATE_PATH: GATE, GATE_SELFTEST_PATH: GATE_SELFTEST_BROKEN})

    both = predicate("P4", "有界恢复", gate=GATE_PATH)
    both["anchor"]["test"] = ANCHOR_TEST
    run_anchor("a criterion naming both a test and a gate is red",
               "states two measurements", both, gate_files)

    run("a kill whose text another test of the same file states is red",
        "which the anchor TestFixtureAnchor does not state",
        registry(bound, killed()), anchor=ANCHOR_NEIGHBOUR, mutations=registered())

    run("a kill the run records under no test is red",
        "the kill names no anchor site",
        registry(bound, killed(test_name=None)), mutations=registered())

    run("a kill the run records under another test is red",
        "the kill came from another test",
        registry(bound, killed(test_name="TestFixtureNeighbour")), mutations=registered())

    run("a criterion that names no assertion is red",
        "names no assertion to attribute the kill",
        registry([bound[0], predicate("P4", "有界恢复", must_assert="")], killed()),
        mutations=registered())

    run("a run recording a kill beside a gate anchor is red",
        "the kill is unattributable",
        registry(gate_bound, killed()), mutations=registered(), extra_files=gate_files)

    gate_predicate = predicate("P4", "有界恢复", gate=GATE_PATH,
                               mutation_id=GATE_MUTATION_ID)
    gate_verdicts = killed() + killed(mutation_id=GATE_MUTATION_ID, test_name=None,
                                      assertion=GATE_KILL_TEXT, gate=GATE_PATH)

    run("a gate kill recorded as the anchor gate's own verdict is green", None,
        registry([predicate("P1", "单一真相"), gate_predicate], gate_verdicts),
        mutations=registered_gate(), extra_files=gate_files)

    run("a gate kill recorded under another gate's verdict is red",
        "the kill is unattributable",
        registry([predicate("P1", "单一真相"), gate_predicate],
                 killed() + killed(mutation_id=GATE_MUTATION_ID, test_name=None,
                                   assertion=GATE_KILL_TEXT,
                                   gate="scripts/gates/other_gate.py")),
        mutations=registered_gate(), extra_files=gate_files)

    run("a gate kill whose registered text the gate does not state is red",
        "which the anchor gate scripts/gates/fixture_gate.py does not state",
        registry([predicate("P1", "单一真相"), gate_predicate],
                 killed() + killed(mutation_id=GATE_MUTATION_ID, test_name=None,
                                   assertion="text the gate never states", gate=GATE_PATH)),
        mutations=registered_gate(gate_assertion="text the gate never states"),
        extra_files=gate_files)

    run("an anchor that judges the measure only by a noise bound is red",
        "judges wallClock at 8x beside the 2x", registry(bound, killed()),
        anchor=ANCHOR_NOISE, mutations=registered())

    run("a proxy anchor that never judges the measure is red",
        "never judges the measure wallClock", registry(bound, killed()),
        anchor=ANCHOR_PROXY, mutations=registered())

    run("an anchor that does not exist is red",
        "the anchor test TestFixtureAnchor does not exist",
        registry(bound, killed()), anchor=None, mutations=registered())

    run("a criterion with no registry entry is red",
        "binds no measurement to it",
        registry([predicate("P4", "有界恢复")], killed()), mutations=registered())

    run("a registry entry the design no longer states is red",
        "the design no longer states",
        registry(bound + [predicate("P9", "退役判据")], killed()),
        mutations=registered())

    run("a registry entry whose title drifted from the design is red",
        "does not state the title the design gives it",
        registry([predicate("P1", "另一条判据"), bound[1]], killed()),
        mutations=registered())

    run("an unregistered mutation is red",
        "is not registered in the frozen mutation registry",
        registry(bound, killed()))

    run("a mutation whose run is recorded as surviving is red",
        "status 'not_killed' is not 'killed'",
        registry(bound, killed(verdict="not_killed")), mutations=registered())

    run("a mutation with no recorded run is red",
        "registered and unproven", registry(bound, []), mutations=registered())

    run("a verdict stated only by the registry field is red",
        "stated only by this registry's own field",
        registry(bound, killed()), mutations=registered(), evidence=False)

    # The run product proves a criterion only for the tree it compiled: the verdicts
    # are read beside the closure, and the closure is recomputed against this tree.
    run("a run product whose tree moved after the run is red",
        "the evidence describes another source tree",
        registry(bound, killed()), mutations=registered(), after=drift)

    run("a run product that records no closure is red",
        "must record source_binding",
        registry(bound, killed()), mutations=registered(), after=strip_binding)

    # 报告结构不过即整份 refuse：判词既不计数，缺口也必须具名。
    run("a run product whose control tree was not green is refused",
        ("the control tree must be green", "registered and unproven"),
        registry(bound, killed()), mutations=registered(),
        after=restate_product(control_tree_green=False))

    run("a run product carrying an unproven candidate is refused",
        ("not_evidence_backed must be empty", "registered and unproven"),
        registry(bound, killed()), mutations=registered(),
        after=restate_product(not_evidence_backed=["candidate-01"]))

    run("a run product whose kill contradicts its own record is refused",
        ("disagrees with itself", "registered and unproven"),
        registry(bound, killed()), mutations=registered(),
        after=restate_first_record(verdict="not_killed"))

    run("a kill recorded under another assertion is red",
        "the kill belongs to another assertion",
        registry(bound, killed(assertion="another_assertion")), mutations=registered())

    run("a CI gate the workflow does not run is red",
        "is not a job of",
        registry([predicate("P1", "单一真相"), predicate("P4", "有界恢复", ci_gate="soak-long")],
                 killed()), mutations=registered())

    run("criteria source that matches no file is red",
        "matched no file",
        registry(bound, killed(), criteria_paths=["fixture-spec/absent/design.md"]),
        mutations=registered())

    test_formal_lint_consumes_bound_mutation_evidence()
    test_formal_lint_refuses_missing_stale_or_handwritten_evidence()
    test_preflight_generates_evidence_before_dod_gate()

    if FAILURES:
        for failure in FAILURES:
            print("FAIL", failure)
        print("%d/%d cases failed" % (len(FAILURES), CASES))
        return 1
    print("%d/%d cases passed" % (CASES, CASES))
    return 0


if __name__ == "__main__":
    # 主干是自建用例串，没有"按名选一条"的能力；带 `-k` 时交给 unittest 选择，
    # 让一条锚能被单独复跑（不带 `-k` 的全量跑仍是自建串，口径不变）。
    if any(argument == "-k" or argument.startswith("-k")
           for argument in sys.argv[1:]):
        unittest.main()
    sys.exit(main())
