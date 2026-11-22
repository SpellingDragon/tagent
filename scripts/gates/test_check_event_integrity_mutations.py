#!/usr/bin/env python3
"""Selftests for check_event_integrity_mutations.py.

Every negative case is a synthetic `go test -json` stream or a synthetic source
tree under a temporary directory: these tests never run go and never write to the
repository. The positive cases still read the real frozen registry and the real
00.3 contract so a stale patch anchor or a missing defect mapping fails the
selftest before any product run.

These tests only prove the listed counterexamples are rejected. They are not
product acceptance and they do not cover the whole of the spec's requirement.
"""
import dataclasses
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
if str(SCRIPTS) not in sys.path:
    sys.path.insert(0, str(SCRIPTS))
SCENARIOS = SCRIPTS.parent / "scenarios"
if str(SCENARIOS) not in sys.path:
    sys.path.insert(0, str(SCENARIOS))

import check_event_integrity_mutations as gate  # noqa: E402
import run_runtime_scenarios as runtime_runner  # noqa: E402
import test_check_runtime_evidence as runtime_evidence  # noqa: E402


def event(action, test="", output=""):
    payload = {
        "Action": action,
        "Package": "github.com/SpellingDragon/tagent/tests/integration/host",
    }
    if test:
        payload["Test"] = test
    if output:
        payload["Output"] = output
    return payload


def run_result(test_name, actions, exit_code, raw_text="", timed_out=False):
    events, raw = [], []
    for item in actions:
        if isinstance(item, dict):
            events.append(item)
        else:
            raw.append(item)
    return gate.RunResult(
        test_name=test_name,
        exit_code=exit_code,
        events=events,
        raw_text="\n".join(raw) if raw else raw_text,
        log_ref="synthetic",
        command=("go", "test", "-run", "^" + test_name + "$"),
        timed_out=timed_out,
    )


def green_result(test_name):
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("output", test_name, "=== RUN   " + test_name + "\n"),
        event("pass", test_name),
        event("pass"),
    ], 0)


def literal_site(mutation):
    """The basename and line of the anchored function's body stating the literal.

    The registry names a function rather than a line, so a fixture reads the line
    off the same resolver the gate judges with: a fixture that guessed one would
    test its own arithmetic instead of the registry. A gate-anchor site is the
    declaration line of the gate function, read off the gate's own site resolver.
    """
    if mutation.gate_path:
        sites = gate.gate_assertion_sites(mutation, gate.DEFAULT_ROOT)
        assert sites, "the registered gate site names no declared function in " + mutation.gate_path
        return next(iter(sites))
    site_file, site_func = gate.parse_assertion_site(mutation.expect_assertion_site)
    lines = (gate.DEFAULT_ROOT / site_file).read_text(encoding="utf-8").splitlines()
    holding = [number for number
               in gate.assertion_body_lines(lines, site_func, mutation.expect_assertion)
               if any(call in lines[number - 1] for call in gate.FAILING_CONSTRUCTS)]
    assert holding, "the registered assertion states no literal in " + site_func
    return os.path.basename(site_file), holding[0]


def report_line(mutation, text=None, line=None):
    """One located failure report at the mutation's registered assertion site.

    `go test` prints `<file>:<line>: <message>` for a failing assertion, so a
    fixture that claims a kill has to print the location the registry registers
    for it: the position is what ties the report to that assertion.
    """
    name, number = literal_site(mutation)
    return "    {}:{}: {}\n".format(
        name, number if line is None else line,
        mutation.expect_assertion if text is None else text)


def killed_result(mutation, assertion_line=None):
    if mutation.gate_path:
        # A gate verdict is one process exit beside its whole output, so the
        # synthetic kill carries the registered literal as the gate's report
        # rather than a located Go test record.
        return gate.RunResult(
            test_name="",
            exit_code=1,
            events=[],
            raw_text=mutation.expect_assertion + "\n",
            log_ref="synthetic",
            command=tuple(gate.gate_command(gate.DEFAULT_ROOT, mutation)),
            timed_out=False,
        )
    parent = mutation.test_name
    child = parent + "/" + "named_case"
    line = assertion_line or report_line(mutation)
    return run_result(parent, [
        event("start"),
        event("run", parent),
        event("run", child),
        event("output", child, line),
        event("fail", child),
        event("fail", parent),
        event("fail"),
    ], 1)


def skipped_control_result(test_name):
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("output", test_name, "=== RUN   " + test_name + "\n"),
        event("output", test_name, "--- SKIP: " + test_name + " (0.00s)\n"),
        event("skip", test_name),
        event("pass"),
    ], 0)


def control_with_parallel_cases(test_name):
    case_a, case_b = test_name + "/case_a", test_name + "/case_b"
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("output", test_name, "=== RUN   " + test_name + "\n"),
        event("run", case_a),
        event("output", case_a, "=== PAUSE " + case_a + "\n"),
        event("run", case_b),
        event("output", case_b, "=== PAUSE " + case_b + "\n"),
        event("output", case_b, "=== CONT  " + case_b + "\n"),
        event("pass", case_b),
        event("output", case_a, "=== CONT  " + case_a + "\n"),
        event("pass", case_a),
        event("output", test_name, "--- PASS: " + test_name + " (0.01s)\n"),
        event("pass", test_name),
        event("pass"),
    ], 0)


def waived_race_report():
    return ("==================\nWARNING: DATA RACE\nWrite at 0x00c0001 by goroutine 7:\n"
            "  trpc-agent-go/internal/steer.go:10 +0x1\n"
            "==================\n")


def control_with_a_waived_race(test_name):
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("output", test_name, waived_race_report()),
        event("output", test_name, "    testing.go:1865: race detected during execution of test\n"),
        event("output", test_name, "--- FAIL: " + test_name + " (0.50s)\n"),
        event("fail", test_name),
        event("fail"),
    ], 1)


def control_failing_an_assertion_beside_a_waived_race(test_name):
    """A real assertion failure under the same registered race report."""
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("output", test_name, waived_race_report()),
        event("output", test_name,
              "    event_integrity_test.go:52: no query face may report success\n"),
        event("output", test_name, "--- FAIL: " + test_name + " (0.50s)\n"),
        event("fail", test_name),
        event("fail"),
    ], 1)


def control_with_a_waived_race_beside_an_asserting_case(test_name):
    """One case raced, another failed for itself: the race explains only its own."""
    raced, asserted = test_name + "/raced_case", test_name + "/asserted_case"
    return run_result(test_name, [
        event("start"),
        event("run", test_name),
        event("run", raced),
        event("output", raced, waived_race_report()),
        event("output", raced, "    testing.go:1865: race detected during execution of test\n"),
        event("fail", raced),
        event("run", asserted),
        event("output", asserted, "    event_integrity_test.go:52: no query face may report success\n"),
        event("fail", asserted),
        event("fail", test_name),
        event("fail"),
    ], 1)


def enabled_mutations():
    return [mutation for mutation in gate.MUTATIONS if mutation.enabled]


def green_controls(mutations):
    return [{
        "test": {"selector": mutation.test_selector, "name": mutation.test_name},
        "verdict": "green",
        "reason": "green",
        "exit_code": 0,
    } for mutation in gate.controls_for(mutations)]


def judged_records(mutations, confirmed):
    named = [mutation.basis for mutation in mutations
             if mutation.evidence_kind == "named_assertion"]
    records = []
    for mutation in mutations:
        status = gate.evidence_status_for(
            mutation.basis, confirmed, gate.REPRODUCED_DEFECTS, named)
        records.append(gate.judge_mutation(mutation, killed_result(mutation), status))
    return records


class MutationGateTest(unittest.TestCase):
    """Registry, verdict and gate semantics without any real go execution."""

    def setUp(self):
        self.root = gate.DEFAULT_ROOT
        self.confirmed = gate.confirmed_defect_ids(self.root, None)

    def test_control_tree_green_then_every_confirmed_mutation_killed_by_named_assertion(self):
        mutations = enabled_mutations()
        self.assertGreaterEqual(len(mutations), 3)
        # The registry itself is not stale: every patch anchor occurs once in the
        # product file and every named assertion exists in the named test file.
        self.assertEqual(gate.validate_registry(self.root, gate.MUTATIONS), [])
        self.assertEqual(len(gate.CANDIDATE_FORMS), 5)

        controls = green_controls(mutations)
        records = judged_records(mutations, self.confirmed)
        self.assertTrue(all(record["verdict"] == gate.KILLED for record in records))
        self.assertTrue(all(record["assertion_hit"] is True for record in records))
        self.assertTrue(all(record["counted_killed"] is True for record in records))

        decision = gate.evaluate_gate(records, controls, self.confirmed)
        self.assertTrue(decision["ok"], decision["failures"])
        payload = gate.summarize(records, controls, decision, self.root, "sha256:test")

        # The payload is exactly what check_runtime_evidence.py --bundle consumes.
        self.assertIs(payload["control_tree_green"], True)
        self.assertEqual(payload["not_evidence_backed"], [])
        for record in payload["mutations"]:
            self.assertEqual(record["status"], "killed")
            self.assertTrue(record["expect_assertion"])
            self.assertIs(record["assertion_hit"], True)
            self.assertTrue(record["counted_killed"])
        # A patch set is one mutation: the EVID-04 defect lives in two files and
        # is still killed by one named assertion.
        evid = [record for record in payload["mutations"]
                if record["id"] == "MUT-EVID-04-position-boundary"]
        self.assertEqual(len(evid), 1, [r["id"] for r in payload["mutations"]])
        self.assertEqual(sorted(set(evid[0]["targets"])), ["assembly/facts.go", "assembly/request.go"])

    def test_every_confirmed_defect_has_mutation(self):
        self.assertGreaterEqual(len(self.confirmed), 2)
        mutations = enabled_mutations()
        for defect_id in self.confirmed:
            hits = [mutation for mutation in mutations if mutation.basis == defect_id]
            self.assertEqual(
                len(hits), 1,
                "confirmed defect {} must map to exactly one enabled mutation".format(
                    defect_id))
            self.assertEqual(hits[0].evidence_kind, "confirmed_defect")
        for reproduced_id in gate.REPRODUCED_DEFECTS:
            hits = [mutation for mutation in mutations
                    if mutation.basis == reproduced_id]
            self.assertEqual(
                len(hits), 1,
                "reproduced defect {} must map to exactly one enabled mutation".format(
                    reproduced_id))
            self.assertEqual(hits[0].evidence_kind, "reproduction")

    def test_unconfirmed_candidate_is_not_claimed_killed(self):
        candidate = dataclasses.replace(
            gate.MUTATIONS[0],
            id="MUT-UNCONFIRMED-CANDIDATE",
            basis="CAND-UNCONFIRMED",
            evidence_kind="named_assertion",
        )
        status = gate.evidence_status_for(
            candidate.basis, self.confirmed, gate.REPRODUCED_DEFECTS, [])
        record = gate.judge_mutation(candidate, killed_result(candidate), status)
        self.assertEqual(record["verdict"], gate.KILLED)
        self.assertEqual(record["evidence_status"], "not_evidence_backed")
        self.assertFalse(record["counted_killed"])

        decision = gate.evaluate_gate([record], green_controls([candidate]), [])
        self.assertFalse(decision["ok"])
        self.assertEqual(decision["killed"], [])
        self.assertEqual(decision["not_evidence_backed"], [record["id"]])
        self.assertTrue(any("not evidence-backed" in failure
                            for failure in decision["failures"]), decision["failures"])

    def test_compile_failure_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("build-output", "", "# github.com/SpellingDragon/tagent\n"),
            event("build-output", "", "host.go:1:1: syntax error: unexpected EOF\n"),
            event("build-fail"),
            event("fail"),
        ], 1)
        outcome = gate.classify(result, mutation)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "compile_failure")
        self.assertFalse(outcome["assertion_hit"])

    def test_timeout_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("run", mutation.test_name),
        ], -1, timed_out=True)
        outcome = gate.classify(result, mutation)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "timeout")

    def test_zero_test_match_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("output", "", "testing: warning: no tests to run\n"),
            event("pass"),
        ], 0)
        outcome = gate.classify(result, mutation)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "zero_match")

    def test_panic_without_expect_assertion_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("run", mutation.test_name),
            event("output", mutation.test_name, "panic: the assembly exploded\n"),
            event("fail", mutation.test_name),
            event("fail"),
        ], 2)
        outcome = gate.classify(result, mutation)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "panic_without_assertion")

    def test_unknown_or_stale_mutation_fails_gate(self):
        with self.assertRaises(gate.GateError):
            gate.select_mutations(gate.MUTATIONS, ["MUT-DOES-NOT-EXIST"])

        stale = dataclasses.replace(
            gate.MUTATIONS[0],
            id="MUT-STALE",
            replacement=gate.Replacement(
                file="assembly/host.go", old="an anchor the file no longer carries\n", new="x\n"),
        )
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            (tree / "assembly").mkdir()
            (tree / "assembly" / "host.go").write_text("package assembly\n", encoding="utf-8")
            with self.assertRaises(gate.GateError):
                gate.apply_patch(tree, stale)

        broken = dataclasses.replace(
            gate.MUTATIONS[0],
            id="MUT-STALE-REGISTRY",
            replacement=gate.Replacement(
                file="assembly/host.go", old="an anchor the file no longer carries\n", new="x\n"),
        )
        problems = gate.validate_registry(self.root, [broken])
        self.assertTrue(any("stale mutation" in problem for problem in problems), problems)

    def test_red_control_tree_fails_gate(self):
        mutation = enabled_mutations()[0]
        red_control = {
            "test": {"selector": mutation.test_selector, "name": mutation.test_name},
            "verdict": "red",
            "reason": "test_failed",
            "exit_code": 1,
        }
        record = judged_records([mutation], self.confirmed)[0]
        decision = gate.evaluate_gate([record], [red_control], [])
        self.assertFalse(decision["ok"])
        self.assertTrue(any("control" in failure for failure in decision["failures"]),
                        decision["failures"])

    def test_alive_process_counterexample_is_enabled_once_its_anchor_landed(self):
        entry = [mutation for mutation in gate.MUTATIONS
                 if mutation.id == "MUT-CAND-01.10-alive-process-complete"][0]
        self.assertTrue(entry.enabled)
        self.assertEqual(entry.test_package, "./tool/exec")
        self.assertEqual(entry.test_file, "tool/exec/event_integrity_test.go")
        self.assertEqual(entry.test_selector, "^TestEventIntegrityExecLifecycle$")
        self.assertEqual([replacement.file for replacement in entry.patch_set()],
                         ["tool/exec/tool.go"])
        self.assertIn(entry.anchor_subtest,
                      ("partial_output_is_not_completion", "stable_is_not_success"))
        self.assertTrue(entry.expect_assertion)
        self.assertIn(entry.expect_assertion,
                      (self.root / entry.test_file).read_text(encoding="utf-8"))
        self.assertTrue(
            gate.anchor_landed(self.root, entry),
            "01.10a implemented the lifecycle query faces in the package test")

        # No counterexample stays suspended once its anchor lands, and the
        # enabled entry is part of the default execution set.
        self.assertEqual([mutation.id for mutation in gate.MUTATIONS
                          if mutation.status == gate.PENDING], [])
        self.assertIn(entry.id, [mutation.id for mutation in
                                 gate.select_mutations(gate.MUTATIONS, [])])

        mutations = enabled_mutations()
        records = judged_records(mutations, self.confirmed)
        decision = gate.evaluate_gate(records, green_controls(mutations), self.confirmed)
        self.assertTrue(decision["ok"], decision["failures"])
        payload = gate.summarize(records, green_controls(mutations), decision,
                                 self.root, "sha256:test")
        self.assertEqual(payload["pending"], [])
        hit = [record for record in payload["mutations"] if record["id"] == entry.id]
        self.assertEqual(len(hit), 1, [record["id"] for record in payload["mutations"]])
        self.assertEqual(hit[0]["status"], "killed")
        self.assertIs(hit[0]["assertion_hit"], True)
        self.assertTrue(hit[0]["counted_killed"])

    def test_pending_anchor_that_lands_must_be_enabled(self):
        entry = [mutation for mutation in gate.MUTATIONS
                 if mutation.id == "MUT-CAND-01.10-alive-process-complete"][0]
        # A counterexample registered before its anchor exists stays pending;
        # the moment the anchor lands it must be enabled instead.
        pending = dataclasses.replace(
            entry,
            status=gate.PENDING,
            replacement=None,
            companion=(),
            expect_assertion="",
            pending_reason="synthetic: the anchor has not landed yet",
        )
        unlanded = dataclasses.replace(
            pending, test_file="tool/exec/not_implemented_test.go")
        self.assertFalse(gate.anchor_landed(self.root, unlanded),
                         "a pending entry whose test file is absent is not landed")
        self.assertTrue(gate.anchor_landed(self.root, pending))
        swapped = tuple(pending if mutation.id == entry.id else mutation
                        for mutation in gate.MUTATIONS)
        problems = gate.validate_registry(self.root, swapped)
        self.assertTrue(any("has landed" in problem for problem in problems), problems)

        # A pending entry that also declares a patch is a half-registered
        # mutation: the patch would never run and never be evidenced.
        smuggled = dataclasses.replace(pending, replacement=entry.replacement)
        swapped = tuple(smuggled if mutation.id == entry.id else mutation
                        for mutation in gate.MUTATIONS)
        problems = gate.validate_registry(self.root, swapped)
        self.assertTrue(any("must not declare a patch" in problem for problem in problems),
                        problems)

    def test_patch_set_is_applied_and_restored_as_one(self):
        mutation = [item for item in gate.MUTATIONS
                    if item.id == "MUT-EVID-04-position-boundary"][0]
        self.assertEqual(len(mutation.patch_set()), 9)
        files = {replacement.file for replacement in mutation.patch_set()}
        self.assertEqual(files, {"assembly/request.go", "assembly/facts.go"})

        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            for name in files:
                (tree / name).parent.mkdir(parents=True, exist_ok=True)
                source = self.root / name
                (tree / name).write_text(source.read_text(encoding="utf-8"),
                                         encoding="utf-8")
            pristine = gate.apply_patch(tree, mutation)
            mutated = {name: (tree / name).read_text(encoding="utf-8")
                       for name in files}
            self.assertEqual(sorted(pristine), sorted(files))
            self.assertIn("\tended           int\n", mutated["assembly/request.go"])
            self.assertNotIn("endedPrefix", mutated["assembly/request.go"])
            self.assertIn("held.ended = position", mutated["assembly/facts.go"])
            self.assertIn("\tboundary := held.ended\n", mutated["assembly/facts.go"])
            # Every read of the boundary is relocated: one left on the deleted
            # method is a mutant the package refuses to build, which the gate
            # reports as a compile_failure rather than a kill.
            self.assertNotIn("held.endedPrefix()", mutated["assembly/facts.go"])
            self.assertNotIn("held.endedPrefix()", mutated["assembly/request.go"])
            gate.restore_patch(tree, pristine)
            for name in files:
                self.assertEqual((tree / name).read_text(encoding="utf-8"),
                                 pristine[name])


class ControlTreeFaceTest(unittest.TestCase):
    """G4-1: a control that never really ran must not read green.

    A green exit code alone let a control tree of started-then-skipped cases
    count, and every mutated verdict after it counted on no execution at all.
    """

    def setUp(self):
        self.root = gate.DEFAULT_ROOT
        self.mutation = enabled_mutations()[0]
        self.confirmed = gate.confirmed_defect_ids(self.root, None)

    def _control(self, result):
        return gate.control_record(self.mutation, result,
                                   gate.classify_control(result))

    def _decision(self, control):
        record = judged_records([self.mutation], self.confirmed)[0]
        return gate.evaluate_gate([record], [control], [])

    def test_control_whose_selector_skipped_is_not_really_run(self):
        control = self._control(skipped_control_result(self.mutation.test_name))
        self.assertEqual(control["verdict"], "red")
        self.assertEqual(control["reason"], "skipped")
        self.assertEqual(control["skipped"], [self.mutation.test_name])
        decision = self._decision(control)
        self.assertFalse(decision["ok"])
        self.assertTrue(any("is red (skipped)" in failure
                            for failure in decision["failures"]), decision["failures"])

    def test_control_with_only_a_start_record_is_not_really_run(self):
        started = run_result(self.mutation.test_name, [
            event("start"),
            event("run", self.mutation.test_name),
            event("pass"),
        ], 0)
        control = self._control(started)
        self.assertEqual(control["verdict"], "red")
        self.assertEqual(control["reason"], "no_pass_terminal")
        self.assertFalse(self._decision(control)["ok"])

    def test_control_with_zero_matches_is_not_really_run(self):
        nothing = run_result(self.mutation.test_name, [
            event("start"),
            event("output", "", "testing: warning: no tests to run\n"),
            event("pass"),
        ], 0)
        control = self._control(nothing)
        self.assertEqual(control["verdict"], "red")
        self.assertEqual(control["reason"], "zero_match")
        self.assertFalse(self._decision(control)["ok"])

    def test_control_with_parallel_cases_that_pass_stays_green(self):
        control = self._control(control_with_parallel_cases(self.mutation.test_name))
        self.assertEqual(control["verdict"], "green")
        self.assertEqual(control["skipped"], [])
        self.assertTrue(self._decision(control)["ok"])

    def test_control_failing_only_on_a_waived_race_stays_green(self):
        control = self._control(control_with_a_waived_race(self.mutation.test_name))
        self.assertEqual(control["verdict"], "green")
        self.assertEqual(control["reason"], "green_race_waived")

    def test_control_asserting_beside_a_waived_race_is_red(self):
        result = control_failing_an_assertion_beside_a_waived_race(self.mutation.test_name)
        verdict = gate.classify_control(result)
        self.assertEqual(verdict["verdict"], "red")
        self.assertEqual(verdict["reason"], "test_failed")
        self.assertEqual(verdict["failing"], [self.mutation.test_name])
        self.assertFalse(self._decision(self._control(result))["ok"])

    def test_control_with_an_asserting_case_beside_a_raced_case_is_red(self):
        result = control_with_a_waived_race_beside_an_asserting_case(self.mutation.test_name)
        verdict = gate.classify_control(result)
        self.assertEqual(verdict["verdict"], "red")
        self.assertEqual(verdict["reason"], "test_failed")
        self.assertIn(self.mutation.test_name + "/asserted_case", verdict["failing"])
        self.assertFalse(self._decision(self._control(result))["ok"])


class RepointedHandoverAnchorTest(unittest.TestCase):
    """13.R2d: the handover confirmation anchor follows the sink 12.R2.5 reshaped.

    The sink hands the settled result over through the refusal branch of the
    backflow and records the consumption after it, so a registry still anchored on
    the bare call names a form no tree carries. That form must read stale, and the
    re-pointed registration must be the one that passes: the anchor moved with the
    code instead of the check being loosened.
    """

    ID = "MUT-CAND-01.2-handover-confirm"

    def setUp(self):
        self.root = gate.DEFAULT_ROOT
        self.mutation = [mutation for mutation in gate.MUTATIONS
                         if mutation.id == self.ID][0]
        self.old_form = dataclasses.replace(self.mutation, replacement=gate.Replacement(
            file="assembly/facts.go",
            old=("\tr.loop.Backflow(frame.Identity.InvocationID, notice)\n"
                 "\treturn r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)\n"),
            new=("\tif err := r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq); "
                 "err != nil {\n"
                 "\t\treturn err\n"
                 "\t}\n"
                 "\tr.loop.Backflow(frame.Identity.InvocationID, notice)\n"
                 "\treturn nil\n"),
        ))

    def test_the_anchor_the_sink_no_longer_carries_is_stale(self):
        patched = [self.old_form if mutation.id == self.ID else mutation
                   for mutation in gate.MUTATIONS]
        problems = gate.validate_registry(self.root, patched)
        stale = [problem for problem in problems if "stale mutation" in problem]
        self.assertEqual(len(stale), 1, problems)
        self.assertIn(self.ID, stale[0])
        self.assertIn("assembly/facts.go", stale[0])

    def test_the_registered_anchor_is_the_refusal_branch_of_the_sink(self):
        self.assertEqual(gate.validate_registry(self.root, gate.MUTATIONS), [])
        sink = (self.root / "assembly/facts.go").read_text(encoding="utf-8")
        self.assertEqual(sink.count(self.mutation.replacement.old), 1)
        self.assertIn("if refusal := r.loop.Backflow(", self.mutation.replacement.old)
        self.assertNotIn("retainNoticeOwed", self.mutation.replacement.old)
        self.assertTrue(self.mutation.replacement.old.rstrip().endswith(
            "r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)"))

    def test_the_mutated_form_records_the_consumption_before_the_handover(self):
        new = self.mutation.replacement.new
        self.assertLess(new.index("r.store.Consume"), new.index("r.loop.Backflow"))


class AssertionAttributionTest(unittest.TestCase):
    """G4-2: the kill must be that test's failing assertion, not a mention.

    go test -json attributes each line to the test that printed it, so the
    attribution has to read that attribution: an aggregating parent that failed
    for its child, a passing sibling, or a test whose child failed prints lines
    of its own that say nothing about this test's assertion.
    """

    def setUp(self):
        self.mutation = [mutation for mutation in enabled_mutations()
                         if mutation.id == "MUT-FOLD-01-segment-age-window"][0]
        self.parent = self.mutation.test_name
        self.literal = self.mutation.expect_assertion

    def _killed(self, result):
        outcome = gate.classify(result, self.mutation)
        return outcome

    def test_literal_logged_by_the_aggregating_parent_is_not_a_hit(self):
        case = self.parent + "/case_b"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", case),
            event("output", case, "    age_plan_test.go:9: the ladder read L1\n"),
            event("fail", case),
            event("output", self.parent, "    age_plan_test.go:500: " + self.literal + "\n"),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "assertion_not_matched")
        self.assertFalse(outcome["assertion_hit"])

    def test_literal_printed_by_a_test_whose_child_failed_is_not_a_hit(self):
        group, leaf = self.parent + "/group", self.parent + "/group/case"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", group),
            event("run", leaf),
            event("output", group, "    age_plan_test.go:501: " + self.literal + "\n"),
            event("output", leaf, "    age_plan_test.go:77: another rule failed\n"),
            event("fail", leaf),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertFalse(outcome["assertion_hit"])

    def test_literal_printed_by_a_passing_sibling_is_not_a_hit(self):
        failing, passing = self.parent + "/failing", self.parent + "/passing"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", failing),
            event("run", passing),
            event("output", passing, "    age_plan_test.go:600: " + self.literal + "\n"),
            event("pass", passing),
            event("output", failing, "    age_plan_test.go:8: another rule failed\n"),
            event("fail", failing),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertFalse(outcome["assertion_hit"])

    def test_literal_on_a_nested_failing_case_is_a_hit(self):
        group, leaf = self.parent + "/group", self.parent + "/group/case"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", group),
            event("run", leaf),
            event("output", leaf, "    age_plan_test.go:643: " + self.literal + "\n"),
            event("fail", leaf),
            event("fail", group),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.KILLED)
        self.assertEqual(outcome["reason"], "named_assertion")
        self.assertTrue(outcome["assertion_hit"])

    def test_registry_rejects_a_literal_a_logging_call_can_emit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "tool").mkdir()
            (root / "tool" / "log_test.go").write_text(
                "package tool\n\n"
                "func TestX(t *testing.T) {\n"
                "\tt.Logf(\"" + self.literal + " only a log prints this\")\n"
                "}\n", encoding="utf-8")
            mutation = dataclasses.replace(
                self.mutation, id="MUT-LOG-EMITTABLE",
                test_package="./tool", test_file="tool/log_test.go",
                test_name="TestX", test_selector="^TestX$")
            problems = gate.validate_registry(root, [mutation])
            self.assertTrue(
                any("emittable by a logging call" in problem for problem in problems),
                problems)

    def test_registry_accepts_a_literal_beside_a_shared_log(self):
        # A case may log freely around the assertion: only the literal itself is
        # refused on a logging call, so the real registry stays valid.
        self.assertEqual(gate.validate_registry(gate.DEFAULT_ROOT, gate.MUTATIONS), [])

    def test_another_assertion_of_the_same_failing_case_is_not_a_hit(self):
        # The residual hole 13.R2 closes: one failing leaf cases asserts twice,
        # and the second assertion's message happens to state the registered
        # literal. Its report prints its own line, so the position says it is not
        # this assertion even though the case failed and the text matches.
        leaf = self.parent + "/named_case"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", leaf),
            event("output", leaf, report_line(self.mutation, line=629)),
            event("fail", leaf),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "assertion_not_matched")
        self.assertFalse(outcome["assertion_hit"])

    def test_a_helper_bubbled_report_at_its_call_site_is_a_hit(self):
        # An assertion written inside a `t.Helper()` reports the caller's line, so
        # the site a kill may print is the literal's own line beside every call site
        # that reaches it; a rule that demanded the literal's own line would drop
        # every real kill of such an assertion.
        mutation = [mutation for mutation in enabled_mutations()
                    if mutation.id == "MUT-CAND-01.2-early-confirm"][0]
        name, own_line = literal_site(mutation)
        call_site = sorted(line for file, line in gate.assertion_sites_of(mutation)
                           if line != own_line)
        self.assertTrue(call_site, "the registered helper assertion states no call site")
        leaf = mutation.test_name + "/named_case"
        result = run_result(mutation.test_name, [
            event("start"),
            event("run", mutation.test_name),
            event("run", leaf),
            event("output", leaf, "    {}:{}: {}\n".format(
                name, call_site[0], mutation.expect_assertion)),
            event("fail", leaf),
            event("fail", mutation.test_name),
            event("fail"),
        ], 1)
        outcome = gate.classify(result, mutation)
        self.assertEqual(outcome["verdict"], gate.KILLED, outcome)
        self.assertTrue(outcome["assertion_hit"])

    def test_a_multi_line_message_carries_its_literal_on_a_continuation_line(self):
        leaf = self.parent + "/named_case"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", leaf),
            event("output", leaf, "    age_plan_test.go:643: the golden differed\n"),
            event("output", leaf, "    " + self.literal + "\n"),
            event("fail", leaf),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.KILLED, outcome)

    def test_two_assertions_on_one_source_line_stay_indistinguishable(self):
        # DECLARED RESIDUAL, not a closed hole: two assertions written on the same
        # source line print the same location, so the second one's message still
        # reads as a hit. Separating them would need the column, which neither the
        # registry nor `go test -json` states. The hole this test states is one
        # line wide; the one the location closes was a whole failing case wide.
        leaf = self.parent + "/named_case"
        result = run_result(self.parent, [
            event("start"),
            event("run", self.parent),
            event("run", leaf),
            event("output", leaf, report_line(
                self.mutation,
                text="another assertion on this line: " + self.literal)),
            event("fail", leaf),
            event("fail", self.parent),
            event("fail"),
        ], 1)
        outcome = self._killed(result)
        self.assertEqual(outcome["verdict"], gate.KILLED)
        self.assertTrue(outcome["assertion_hit"])

    def _registry_problems(self, text, func="TestFoldPlanGolden"):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "memory" / "replay").mkdir(parents=True)
            (root / "memory" / "replay" / "age_plan_test.go").write_text(
                text, encoding="utf-8")
            mutation = dataclasses.replace(
                self.mutation,
                test_file="memory/replay/age_plan_test.go",
                expect_assertion_site="memory/replay/age_plan_test.go#" + func)
            return gate.validate_registry(root, [mutation])

    def test_registry_rejects_a_site_that_states_no_literal(self):
        # A neighbour function stating the literal is no evidence for the anchored
        # one: the anchor names a function, and the literal has to stand in its body.
        problems = self._registry_problems(
            "package replay\n\nfunc TestFoldPlanGolden(t *testing.T) {\n"
            "\tt.Fatalf(\"the golden differed\")\n}\n\n"
            "func TestNeighbour(t *testing.T) {\n"
            "\tt.Fatalf(\"" + self.literal + "\")\n}\n")
        self.assertTrue(
            any("states no" in problem and "the literal is stated at line(s) 8" in problem
                for problem in problems), problems)

    def test_registry_rejects_a_site_naming_a_function_the_file_does_not_declare(self):
        problems = self._registry_problems(
            "package replay\n\nfunc TestFoldPlanGolden(t *testing.T) {\n"
            "\tt.Fatalf(\"" + self.literal + "\")\n}\n", "TestFoldPlanGilden")
        self.assertTrue(
            any("names no function the file declares" in problem for problem in problems),
            problems)

    def test_registry_rejects_a_site_that_cannot_fail_the_test(self):
        problems = self._registry_problems(
            "package replay\n\nfunc TestFoldPlanGolden(t *testing.T) {\n"
            "\t_ = \"" + self.literal + "\"\n}\n")
        self.assertTrue(
            any("no failing construct" in problem for problem in problems), problems)

    def test_every_registered_assertion_site_states_its_literal_in_a_failing_construct(self):
        # The registry-level positive half: every enabled mutation names a function
        # whose body really states its literal inside a construct that fails the
        # test, and that body resolves to at least one location a run can print.
        for mutation in enabled_mutations():
            self.assertEqual(gate.assertion_site_problem(mutation, gate.DEFAULT_ROOT), "",
                             mutation.id)
            sites = gate.assertion_sites_of(mutation)
            self.assertTrue(sites, mutation.id)
            self.assertIn(literal_site(mutation), sites, mutation.id)

    def test_registry_rejects_an_enabled_mutation_without_a_site(self):
        with tempfile.TemporaryDirectory() as tmp:
            mutation = dataclasses.replace(self.mutation, expect_assertion_site="")
            problems = gate.validate_registry(Path(tmp), [mutation])
            self.assertTrue(
                any("must register the source site of its assertion" in problem
                    for problem in problems), problems)

    def test_registry_rejects_a_site_outside_the_test_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            mutation = dataclasses.replace(
                self.mutation,
                expect_assertion_site="memory/replay/other_test.go#TestFoldPlanGolden")
            problems = gate.validate_registry(Path(tmp), [mutation])
            self.assertTrue(
                any("is outside the test file" in problem for problem in problems), problems)


class CompiledFaceTest(unittest.TestCase):
    """G4-3: a patch outside the selector's build never mutated the run."""

    def setUp(self):
        self.mutation = [mutation for mutation in enabled_mutations()
                         if mutation.id == "MUT-FOLD-01-segment-age-window"][0]

    def _tree(self, root: Path, *, tag: str = "", nested_module: bool = False):
        (root / "go.mod").write_text("module example.com/m\n\ngo 1.24.1\n",
                                     encoding="utf-8")
        for name, body in {
            "host/host.go": "package host\n\nimport _ \"example.com/m/product\"\n",
            "host/host_test.go": "package host\n",
            "product/impl.go": tag + "package product\n",
            "other/impl.go": "package other\n",
        }.items():
            (root / name).parent.mkdir(parents=True, exist_ok=True)
            (root / name).write_text(body, encoding="utf-8")
        if nested_module:
            (root / "other" / "go.mod").write_text("module example.com/other\n",
                                                   encoding="utf-8")
        return dataclasses.replace(
            self.mutation, test_package="./host", test_file="host/host_test.go",
            test_name="TestHost", test_selector="^TestHost$",
            replacement=gate.Replacement(file="product/impl.go", old="package product",
                                         new="package product // mutated"))

    def test_the_import_closure_names_the_packages_the_selector_compiles(self):
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            mutation = self._tree(tree)
            compiled = gate.compiled_packages(tree, "./host")
            self.assertIn((tree / "host").resolve(), compiled)
            self.assertIn((tree / "product").resolve(), compiled)
            self.assertNotIn((tree / "other").resolve(), compiled)
            self.assertEqual(gate.compiled_face_problem(tree, mutation), "")

    def test_a_patch_outside_the_import_closure_is_not_applied(self):
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            mutation = self._tree(tree)
            mutation = dataclasses.replace(
                mutation,
                replacement=gate.Replacement(file="other/impl.go", old="package other",
                                             new="package other // mutated"))
            # `other` is not imported by the selector's package.
            problem = gate.compiled_face_problem(tree, mutation)
            self.assertIn("outside the import closure", problem)

    def test_a_patch_under_an_unset_build_tag_is_not_applied(self):
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            mutation = self._tree(tree, tag="//go:build ignore\n\n")
            problem = gate.compiled_face_problem(tree, mutation)
            self.assertIn("//go:build ignore", problem)
            self.assertIn("is false for the tags the gate sets", problem)

    def test_a_patch_naming_another_platform_is_not_applied(self):
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            self._tree(tree)
            foreign = "windows" if "darwin" in gate.platform_tags() else "darwin"
            (tree / "product" / ("impl_" + foreign + ".go")).write_text(
                "package product\n", encoding="utf-8")
            problem = gate.build_constraint_excludes(
                tree / "product" / ("impl_" + foreign + ".go"),
                gate.platform_tags() | gate.CONSTRAINT_TAGS)
            self.assertIn("names the " + foreign + " build", problem)

    def test_a_patch_into_a_nested_module_is_not_applied(self):
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            mutation = self._tree(tree, nested_module=True)
            mutation = dataclasses.replace(
                mutation,
                replacement=gate.Replacement(file="other/impl.go", old="package other",
                                             new="package other // mutated"))
            problem = gate.compiled_face_problem(tree, mutation)
            self.assertIn("the patch spans modules", problem)

    def test_an_unapplied_patch_never_counts_as_a_kill(self):
        record = gate.not_applied_record(
            self.mutation, "product/impl.go: //go:build ignore is false", "named_assertion")
        self.assertEqual(record["status"], gate.NOT_APPLIED)
        self.assertFalse(record["counted_killed"])
        decision = gate.evaluate_gate([record], green_controls([self.mutation]), [])
        self.assertFalse(decision["ok"])
        self.assertEqual(decision["killed"], [])
        self.assertTrue(any("not_in_compiled_face" in failure
                            and "//go:build ignore" in failure
                            for failure in decision["failures"]), decision["failures"])

    def test_every_registered_patch_enters_its_selectors_compiled_face(self):
        for mutation in enabled_mutations():
            self.assertEqual(gate.compiled_face_problem(gate.DEFAULT_ROOT, mutation), "",
                             mutation.id)


class MutationBaselineTest(unittest.TestCase):
    """The report must name the runner's baseline so --bundle can tie it.

    A mutation report can be perfectly green on its own and still come from
    another tree; the bundle only counts it when it names the same contract
    digest and the same per-module fingerprint as the runs that stayed green.
    These anchors drive the producer through summarize() and the public consumer
    through check_runtime_evidence.py --bundle, never a private copy of the rule.
    """

    def setUp(self):
        self.root = gate.DEFAULT_ROOT
        mutations = enabled_mutations()
        confirmed = gate.confirmed_defect_ids(self.root, None)
        controls = green_controls(mutations)
        records = judged_records(mutations, confirmed)
        decision = gate.evaluate_gate(records, controls, confirmed)
        self.assertTrue(decision["ok"], decision["failures"])
        self.payload = gate.summarize(records, controls, decision, self.root,
                                      "sha256:test")

    def test_report_names_contract_digest_and_tested_module_fingerprints(self):
        self.assertEqual(
            self.payload["contract_digest"],
            runtime_evidence.event_digest(runtime_evidence.REAL_EVENT_CONTRACT))
        fingerprints = self.payload["module_fingerprints"]
        self.assertEqual(len(fingerprints), 1)
        entry = fingerprints[0]
        self.assertEqual(entry["dir"], ".")
        self.assertEqual(entry["module_path"], runtime_runner.MODULE_PATH)
        self.assertRegex(entry["revision"], r"^[0-9a-f]{7,40}$")
        self.assertRegex(entry["content_hash"], r"^sha256:[0-9a-f]{64}$")
        # One definition: the report names exactly what the runner fingerprints,
        # plus the role that says why the module is in the closure.
        self.assertEqual(entry["roles"], ["tested"])
        shared = runtime_runner.module_fingerprint(str(self.root), ".",
                                                  runtime_runner.MODULE_PATH)
        self.assertEqual({field: entry[field] for field in shared}, shared)

    def test_report_names_the_closure_and_the_gate_that_decided_it(self):
        binding = self.payload["source_binding"]
        self.assertEqual([entry["dir"] for entry in binding["modules"]], ["."])
        self.assertEqual(binding["workspace"]["state"], "absent")
        self.assertEqual(binding["edges"], [])
        self.assertEqual(binding["before"], {"digest": binding["digest"], "modules": 1})
        self.assertEqual(binding["after"], binding["before"])
        # The gate whose verdict vocabulary decides "killed" names its own bytes.
        self.assertEqual(self.payload["gate_tool"]["entrypoint"],
                         "scripts/gates/check_event_integrity_mutations.py")
        self.assertRegex(self.payload["gate_tool"]["digest"], r"^sha256:[0-9a-f]{64}$")

    def _bundle(self, name="producer-baseline"):
        """A legal runner bundle describing the same tree as the report.

        The checker re-resolves every closure against `--repo-root`, so the fixture
        bundle has to describe the tested tree rather than a synthetic one: the
        evidence is generated from the real repository and checked against it.
        That includes the revision: the fixture resolves a tree with no checkout,
        so its binding carries a placeholder there, while this bundle describes the
        repository the report was driven in and the report names the revision it
        read. Naming another revision is what the baseline gate refuses, so the
        runs name the same one the report does.
        """
        fixture = runtime_evidence.EventFixture()
        fixture.setUp()
        self.addCleanup(fixture.tearDown)
        revision = self.payload["module_fingerprints"][0]["revision"]

        def same_baseline(evidence):
            for module in evidence["source_binding"]["modules"]:
                module["revision"] = revision
            for module in evidence["module_fingerprints"]:
                module["revision"] = revision
            return evidence

        bundle = fixture.write_bundle(
            name, mutation=False, tree=self.root,
            short_hook=lambda index, evidence: same_baseline(evidence),
            long_hook=lambda seed, window, evidence: same_baseline(evidence))
        mutations = bundle / "mutations"
        mutations.mkdir()
        return bundle, mutations

    def _check_bundle(self, bundle):
        return runtime_evidence.run_checker(
            "--contract", str(runtime_evidence.REAL_EVENT_CONTRACT),
            "--repo-root", str(self.root), "--bundle", str(bundle), "--json")

    def test_produced_report_passes_the_bundle_same_baseline_gate(self):
        bundle, mutations = self._bundle()
        (mutations / "evidence.json").write_text(
            json.dumps(self.payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, err = self._check_bundle(bundle)
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["bundle_mutation_evidence"], 1)
        self.assertIsNone(payload["productVerified"])

    def test_report_with_an_unapplied_patch_is_rejected_by_the_bundle(self):
        bundle, mutations = self._bundle("unapplied-patch")
        report = json.loads(json.dumps(self.payload))
        report["mutations"][0]["status"] = gate.NOT_APPLIED
        report["mutations"][0]["verdict"] = gate.NOT_APPLIED
        report["mutations"][0]["assertion_hit"] = False
        report["mutations"][0]["counted_killed"] = False
        (mutations / "evidence.json").write_text(
            json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, err = self._check_bundle(bundle)
        self.assertNotEqual(code, 0, out + err)
        self.assertIn("is not 'killed'", out)

    def test_old_report_without_baseline_fields_is_rejected(self):
        bundle, mutations = self._bundle("legacy-report")
        legacy = json.loads(json.dumps(self.payload))
        del legacy["contract_digest"]
        del legacy["module_fingerprints"]
        (mutations / "evidence.json").write_text(
            json.dumps(legacy, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, err = self._check_bundle(bundle)
        self.assertNotEqual(code, 0, out + err)
        self.assertIn("must declare contract_digest", out)
        self.assertIn("module_fingerprints must be a non-empty list", out)


class GateAnchorBranchTest(unittest.TestCase):
    """Gate-anchor entries are judged by the gate's verdict face, not a Go stream."""

    def setUp(self):
        self.root = gate.DEFAULT_ROOT
        self.entries = [mutation for mutation in enabled_mutations()
                        if mutation.gate_path]
        self.assertTrue(self.entries)

    def test_a_gate_kill_is_attributed_to_its_gate_path(self):
        for entry in self.entries:
            record = gate.judge_mutation(entry, killed_result(entry), "named_assertion")
            self.assertEqual(record["verdict"], gate.KILLED)
            self.assertTrue(record["assertion_hit"])
            self.assertTrue(record["counted_killed"])
            self.assertEqual(record["gate"],
                             {"path": entry.gate_path, "selftest": entry.gate_selftest})
            name, line = literal_site(entry)
            self.assertEqual(record["assertion_locations"], ["{}:{}".format(name, line)])

    def test_a_gate_entry_declaring_a_go_test_states_two_measurements(self):
        for entry in self.entries:
            dual = dataclasses.replace(entry, test_name="TestDualMeasurement")
            problems = gate.validate_registry(self.root, [dual])
            self.assertTrue(any("two measurements" in problem for problem in problems),
                            problems)

    def test_a_gate_entry_whose_gate_path_is_missing_is_rejected(self):
        for entry in self.entries:
            missing = dataclasses.replace(entry, gate_path="scripts/gates/no_such_gate.py")
            problems = gate.validate_registry(self.root, [missing])
            self.assertTrue(any("the anchor gate is missing" in problem
                                for problem in problems), problems)

    def test_a_gate_that_stays_green_is_gate_green_not_a_kill(self):
        for entry in self.entries:
            green = dataclasses.replace(killed_result(entry), exit_code=0)
            record = gate.judge_mutation(entry, green, "named_assertion")
            self.assertEqual(record["verdict"], gate.NOT_KILLED)
            self.assertEqual(record["reason"], "gate_green")
            self.assertFalse(record["counted_killed"])
            decision = gate.evaluate_gate([record], green_controls([entry]), [])
            self.assertFalse(decision["ok"])
            self.assertTrue(any("gate_green" in failure
                                for failure in decision["failures"]), decision["failures"])

    def test_a_gate_failure_without_the_registered_literal_is_not_matched(self):
        for entry in self.entries:
            other = dataclasses.replace(killed_result(entry),
                                        raw_text="an unrelated rejection\n")
            record = gate.judge_mutation(entry, other, "named_assertion")
            self.assertEqual(record["verdict"], gate.NOT_KILLED)
            self.assertEqual(record["reason"], "assertion_not_matched")
            self.assertFalse(record["assertion_hit"])
            self.assertFalse(record["counted_killed"])

    def test_a_gate_site_naming_an_undeclared_function_is_rejected(self):
        for entry in self.entries:
            broken = dataclasses.replace(
                entry, expect_assertion_site=entry.gate_path + "#no_such_function")
            problem = gate.assertion_site_problem(broken, self.root)
            self.assertIn("names no function the gate declares", problem)
            with self.assertRaises(AssertionError):
                literal_site(broken)


class ClosureCliTest(unittest.TestCase):
    """G4-3 and G4-2 at the interface the gate is really called by.

    A synthetic tree unit test proves the predicate's function; it does not prove
    that the CLI reaches it. These cases drive `check_event_integrity_mutations.py`
    as a program over one fixture tree that is a copy of this repository, so the
    frozen registry validates against it and the compiled-face question is asked
    of a genuine import graph. The selector the fixture moves out of the patched
    package's closure is a stand-in `*_test.go` file that compiles and imports
    nothing, so "the selector never compiles the patched package" is true of the
    tree rather than arranged by the measurement.
    """

    # Product files a concurrent edit may have moved off its registered anchors
    # are restored to HEAD in the fixture: the question under test is the closure,
    # not somebody else's in-flight product change.
    MAX_FIXTURE_REPAIRS = 3
    STALE_ANCHOR = re.compile(r"stale mutation, the patch anchor occurs \d+ times in (\S+)$")

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.mkdtemp(prefix="mutation-closure-")
        cls.tree = Path(cls.tmp) / "tree"
        cls.replay = Path(cls.tmp) / "replay"
        cls.real_selector = Path(cls.tmp) / "selector-real"
        cls.mutation = [mutation for mutation in enabled_mutations()
                        if mutation.id == "MUT-G1-meditation-busy-drop"][0]
        cls.restored = cls._build_tree()
        cls._build_replays()
        cls.real_selector.mkdir()
        for source in sorted((cls.tree / Path(cls.mutation.test_package[2:])).glob("*.go")):
            shutil.copy2(source, cls.real_selector / source.name)
            source.unlink()
        cls._write_stand_in_selector(cls.tree)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp, ignore_errors=True)

    @classmethod
    def _build_tree(cls):
        """Copy the working tree, then keep the frozen anchors satisfiable.

        The copy is the gate's own source set — tracked files plus the untracked
        ones git does not ignore — because the frozen registry may name a file a
        mid-change tree has not added to the index yet: an untracked file is still
        the file its anchors are counted in, and copying the index alone would
        report that file missing.
        """
        gate.copy_working_tree(gate.DEFAULT_ROOT, cls.tree,
                               gate.source_files(gate.DEFAULT_ROOT))
        (cls.tree / ".git" / "refs" / "heads").mkdir(parents=True)
        (cls.tree / ".git" / "objects").mkdir()
        (cls.tree / ".git" / "HEAD").write_text("ref: refs/heads/dev\n", encoding="utf-8")
        (cls.tree / ".git" / "config").write_text(
            "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n",
            encoding="utf-8")
        (cls.tree / ".git" / "refs" / "heads" / "dev").write_text(
            gate.git_head(gate.DEFAULT_ROOT) + "\n", encoding="utf-8")
        restored = []
        for _ in range(cls.MAX_FIXTURE_REPAIRS):
            stale = [match.group(1) for problem in gate.validate_registry(cls.tree, gate.MUTATIONS)
                     for match in [cls.STALE_ANCHOR.search(problem)] if match]
            if not stale:
                break
            for name in sorted(set(stale)):
                (cls.tree / name).write_text(
                    subprocess.run(["git", "-C", str(gate.DEFAULT_ROOT), "show",
                                    "HEAD:" + name], capture_output=True, text=True,
                                   check=True).stdout, encoding="utf-8")
                restored.append(name)
        problems = gate.validate_registry(cls.tree, gate.MUTATIONS)
        assert not problems, "the fixture cannot satisfy the frozen registry: {}".format(problems)
        return restored

    @classmethod
    def _write_stand_in_selector(cls, tree):
        """The selector package as one stand-in file: it compiles, it imports nothing."""
        site_file, site_func = gate.parse_assertion_site(cls.mutation.expect_assertion_site)
        _, literal_line = literal_site(cls.mutation)
        lines = ["package evolution", "", "import \"testing\"", "",
                 "// {} is the fixture selector of this tree.".format(site_func),
                 "//",
                 "// The stand-in states the named assertion of the registered mutation inside",
                 "// the body of the function the registry names, and imports nothing beside",
                 "// testing: the fixture asks what the gate does with a selector whose import",
                 "// closure never reaches the package a mutation patches.",
                 "func {}(t *testing.T) {{".format(site_func),
                 "\tt.Run(\"busy_observation_holds_watermark\", func(t *testing.T) {"]
        while len(lines) < literal_line - 1:
            lines.append("\t\t// the observation this fixture never reaches: the gate answers it")
        lines.append("\t\tt.Fatal(\"" + cls.mutation.expect_assertion +
                     ": no next tick re-evaluated the material it named\")")
        lines += ["\t})", "}"]
        target = tree / site_file
        target.write_text("\n".join(lines) + "\n", encoding="utf-8")
        cls.stand_in = Path(cls.tmp) / "selector-stand-in.go.txt"
        cls.stand_in.write_text("\n".join(lines) + "\n", encoding="utf-8")

    @classmethod
    def _build_replays(cls):
        """Recorded streams: the control green, the mutant failing on its assertion."""
        package = None
        name = cls.mutation.test_name

        def event(action, test=None, output=None):
            payload = {"Action": action,
                       "Package": "github.com/SpellingDragon/tagent/" +
                                  cls.mutation.test_package[2:]}
            if test:
                payload["Test"] = test
            if output:
                payload["Output"] = output
            return payload

        control = [event("start"), event("run", name),
                   event("output", name, "=== RUN   " + name + "\n"),
                   event("pass", name), event("pass")]
        basename, literal_line = literal_site(cls.mutation)
        for label, line in (("hit", literal_line), ("look-alike", literal_line + 1)):
            directory = cls.replay / label
            (directory / "control").mkdir(parents=True)
            (directory / "mutations" / cls.mutation.id).mkdir(parents=True)
            case = name + "/busy_observation_holds_watermark"
            mutated = [event("start"), event("run", name), event("run", case),
                       event("output", case, "    {}:{}: {}\n".format(
                           basename, line, cls.mutation.expect_assertion)),
                       event("fail", case), event("fail", name), event("fail")]
            for path, stream in (
                    (directory / "control" / (name + ".jsonl"), control),
                    (directory / "mutations" / cls.mutation.id / "run.jsonl", mutated)):
                path.write_text("".join(json.dumps(item) + "\n" for item in stream),
                                encoding="utf-8")
        del package

    def _cli(self, replay, label, nested_module=False):
        """Run the gate as a program over the fixture tree; return (code, payload)."""
        nested = self.tree / "features" / "evolution" / "go.mod"
        if nested_module:
            nested.write_text("module example.com/evolution\n\ngo 1.24.1\n", encoding="utf-8")
        try:
            out = Path(self.tmp) / ("out-" + label)
            proc = subprocess.run(
                [sys.executable, str(SCRIPTS / "check_event_integrity_mutations.py"),
                 "--root", str(self.tree), "--replay", str(self.replay / replay),
                 "--out", str(out), "--mutation", self.mutation.id, "--json"],
                capture_output=True, text=True)
        finally:
            if nested.exists():
                nested.unlink()
        payload = json.loads(proc.stdout) if proc.stdout.strip() else {}
        return proc.returncode, payload, proc.stderr

    def _selector_dir(self):
        return self.tree / self.mutation.test_package[2:]

    def _install_real_selector(self):
        for path in self._selector_dir().glob("*.go"):
            path.unlink()
        for source in sorted(self.real_selector.glob("*.go")):
            shutil.copy2(source, self._selector_dir() / source.name)

    def _install_stand_in_selector(self):
        for path in self._selector_dir().glob("*.go"):
            path.unlink()
        (self._selector_dir() / "event_integrity_test.go").write_text(
            self.stand_in.read_text(encoding="utf-8"), encoding="utf-8")

    def test_a_selector_that_never_compiles_the_patched_package_is_rejected(self):
        # G4-3 at the CLI: the stand-in selector imports nothing, so the patched
        # package is outside its closure and the run proves nothing about it.
        self._install_stand_in_selector()
        code, payload, err = self._cli("hit", "closure-red")
        self.assertNotEqual(code, 0, err)
        self.assertFalse(payload["gate"]["ok"])
        record = payload["mutations"][0]
        self.assertEqual(record["status"], gate.NOT_APPLIED)
        self.assertEqual(record["reason"], "not_in_compiled_face")
        self.assertIn("outside the import closure", record["patch_face"])
        self.assertFalse(record["counted_killed"])
        self.assertTrue(any("not_in_compiled_face" in failure
                            for failure in payload["gate"]["failures"]),
                        payload["gate"]["failures"])

    def test_the_selector_that_compiles_the_target_is_accepted(self):
        # The pair's other half: the same recorded stream, the same tree, and a
        # selector whose closure reaches the patched package reads as a kill.
        self._install_real_selector()
        code, payload, err = self._cli("hit", "closure-green")
        self.assertEqual(code, 0, err + json.dumps(payload.get("gate", {})))
        record = payload["mutations"][0]
        self.assertEqual(record["status"], gate.KILLED)
        self.assertTrue(record["counted_killed"])
        self.assertEqual(record["assertion_site"], self.mutation.expect_assertion_site)

    def test_a_literal_at_another_line_of_the_same_failing_case_is_rejected(self):
        # G4-2 at the CLI: the case failed and its output states the literal, but
        # the record sits on another assertion's line, so it is no kill.
        self._install_real_selector()
        code, payload, err = self._cli("look-alike", "closure-lookalike")
        self.assertNotEqual(code, 0, err)
        record = payload["mutations"][0]
        self.assertEqual(record["status"], gate.NOT_KILLED)
        self.assertEqual(record["reason"], "assertion_not_matched")
        self.assertFalse(record["assertion_hit"])

    def test_a_patch_inside_a_nested_module_is_rejected(self):
        # The cross-module form at the CLI: a go.mod under the patched package
        # makes it a module of its own, so this selector cannot build it.
        self._install_real_selector()
        code, payload, err = self._cli("hit", "closure-nested-module", nested_module=True)
        self.assertNotEqual(code, 0, err)
        record = payload["mutations"][0]
        self.assertEqual(record["status"], gate.NOT_APPLIED)
        self.assertIn("spans modules", record["patch_face"])


if __name__ == "__main__":
    unittest.main()
