#!/usr/bin/env python3
"""Selftests for check_event_integrity_mutations.py.

Every negative case is a synthetic `go test -json` stream: these tests never run
go and never patch the repository. The positive cases still read the real frozen
registry and the real 00.3 contract so a stale patch anchor or a missing defect
mapping fails the selftest before any product run.
"""
import dataclasses
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
if str(SCRIPTS) not in sys.path:
    sys.path.insert(0, str(SCRIPTS))

import check_event_integrity_mutations as gate  # noqa: E402


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


def killed_result(mutation, assertion_line=None):
    parent = mutation.test_name
    child = parent + "/" + "named_case"
    line = assertion_line or (
        "    event_integrity_test.go:1: " + mutation.expect_assertion + "\n")
    return run_result(parent, [
        event("start"),
        event("run", parent),
        event("run", child),
        event("output", child, line),
        event("fail", child),
        event("fail", parent),
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
        self.assertEqual(sorted(set(evid[0]["targets"])), ["facts.go", "request.go"])

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
        outcome = gate.classify(result, mutation.expect_assertion)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "compile_failure")
        self.assertFalse(outcome["assertion_hit"])

    def test_timeout_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("run", mutation.test_name),
        ], -1, timed_out=True)
        outcome = gate.classify(result, mutation.expect_assertion)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "timeout")

    def test_zero_test_match_is_not_a_kill(self):
        mutation = enabled_mutations()[0]
        result = run_result(mutation.test_name, [
            event("start"),
            event("output", "", "testing: warning: no tests to run\n"),
            event("pass"),
        ], 0)
        outcome = gate.classify(result, mutation.expect_assertion)
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
        outcome = gate.classify(result, mutation.expect_assertion)
        self.assertEqual(outcome["verdict"], gate.NOT_KILLED)
        self.assertEqual(outcome["reason"], "panic_without_assertion")

    def test_unknown_or_stale_mutation_fails_gate(self):
        with self.assertRaises(gate.GateError):
            gate.select_mutations(gate.MUTATIONS, ["MUT-DOES-NOT-EXIST"])

        stale = dataclasses.replace(
            gate.MUTATIONS[0],
            id="MUT-STALE",
            replacement=gate.Replacement(
                file="host.go", old="an anchor the file no longer carries\n", new="x\n"),
        )
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            (tree / "host.go").write_text("package tagent\n", encoding="utf-8")
            with self.assertRaises(gate.GateError):
                gate.apply_patch(tree, stale)

        broken = dataclasses.replace(
            gate.MUTATIONS[0],
            id="MUT-STALE-REGISTRY",
            replacement=gate.Replacement(
                file="host.go", old="an anchor the file no longer carries\n", new="x\n"),
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
        self.assertEqual(len(mutation.patch_set()), 7)
        files = {replacement.file for replacement in mutation.patch_set()}
        self.assertEqual(files, {"request.go", "facts.go"})

        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp)
            for name in files:
                source = self.root / name
                (tree / name).write_text(source.read_text(encoding="utf-8"),
                                         encoding="utf-8")
            pristine = gate.apply_patch(tree, mutation)
            mutated = {name: (tree / name).read_text(encoding="utf-8")
                       for name in files}
            self.assertEqual(sorted(pristine), sorted(files))
            self.assertIn("\tended           int\n", mutated["request.go"])
            self.assertNotIn("endedPrefix", mutated["request.go"])
            self.assertIn("held.ended = position", mutated["facts.go"])
            self.assertIn("\tboundary := held.ended\n", mutated["facts.go"])
            gate.restore_patch(tree, pristine)
            for name in files:
                self.assertEqual((tree / name).read_text(encoding="utf-8"),
                                 pristine[name])


if __name__ == "__main__":
    unittest.main()
