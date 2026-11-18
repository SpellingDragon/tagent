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
import sys
import tempfile
from contextlib import redirect_stderr, redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
CHECKER = os.path.join(HERE, "check_dod_coverage.py")

FAILURES = []
CASES = 0

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
          evidence=True, extra_files=None):
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
    if evidence:
        write(root / EVIDENCE_PATH,
              json.dumps(run_product(registry_value.get("mutation_verdicts") or []),
                         ensure_ascii=False, indent=2))
    return root


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
        extra_files=None):
    global CASES
    CASES += 1
    with tempfile.TemporaryDirectory() as tmp:
        root = build(tmp, registry_value, anchor, mutations, design, ci, anchor_path,
                     evidence, extra_files)
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
    if want not in output:
        FAILURES.append("%s: want an error naming %r, got %r" % (name, want, output))
        return
    print("PASS", name)


def killed(assertion=KILL_TEXT, verdict="killed", mutation_id=MUTATION_ID,
           test_name=ANCHOR_TEST, gate=None):
    return [{"id": mutation_id, "verdict": verdict, "assertion": assertion,
             "test_name": test_name, "gate": gate,
             "killing_test": "assembly/anchor_test.go#TestFixtureAnchor",
             "evidence": "synthetic: the fixture records the kill"}]


def run_product(verdicts):
    """The report `check_event_integrity_mutations.py --out` writes.

    The fixtures state their verdicts once, here: a criterion is proven by what a
    run recorded, so the registry's own `mutation_verdicts` field is a claim beside
    it, never its source. The assertion text and the test a record names are the
    frozen registry's, which is how a run product states them. A gate verdict
    names the gate it judged instead of a test.
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
        "is recorded 'not_killed'",
        registry(bound, killed(verdict="not_killed")), mutations=registered())

    run("a mutation with no recorded run is red",
        "registered and unproven", registry(bound, []), mutations=registered())

    run("a verdict stated only by the registry field is red",
        "stated only by this registry's own field",
        registry(bound, killed()), mutations=registered(), evidence=False)

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

    if FAILURES:
        for failure in FAILURES:
            print("FAIL", failure)
        print("%d/%d cases failed" % (len(FAILURES), CASES))
        return 1
    print("%d/%d cases passed" % (CASES, CASES))
    return 0


if __name__ == "__main__":
    sys.exit(main())
