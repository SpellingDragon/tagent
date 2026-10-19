#!/usr/bin/env python3
"""Selftests for run_runtime_scenarios.py.

Two layers, deliberately separated:

  * pure functions (axis classification, ordered diffs, scope resolution, record
    collection, attestation) are driven with synthetic rows, so the runner's
    judgement is guarded without pretending a synthetic log is product evidence;
  * one end-to-end run per scope drives the real scenario cases, and then asserts
    only the invariants that must always hold — every in-scope scenario attested,
    nothing outside the scope presented, no synthetic record for an unattested
    scenario, and an evidence file the checker can read.

The e2e assertions deliberately do NOT hard-code today's drift values: golden is
a comparison target, so a legitimate contract or product fix must be able to turn
a drift into a match without editing this file.
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
REPO = SCRIPTS.parent
RUNNER = SCRIPTS / "run_runtime_scenarios.py"
CHECKER = SCRIPTS / "check_runtime_evidence.py"
REAL_CONTRACT = REPO / "golden" / "runtime" / "scenarios.json"
MODULE = "github.com/SpellingDragon/tagent"

sys.path.insert(0, str(SCRIPTS))
import run_runtime_scenarios as runner  # noqa: E402


def load_contract():
    return json.loads(REAL_CONTRACT.read_text(encoding="utf-8"))


def run_tool(script, *args):
    proc = subprocess.run([sys.executable, str(script)] + list(args),
                          capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def action_rows(package, test, actions):
    return [{"Action": action, "Package": package, "Test": test, "Output": ""}
            for action in actions]


def record_row(scenario_id, package, test, observations=None):
    payload = {"scenario_id": scenario_id, "test_package": package, "test_name": test,
               "event_refs": [scenario_id + "-ref"],
               "observations": observations if observations is not None else {}}
    return {"Action": "output", "Package": package, "Test": test,
            "Output": runner.RECORD_MARKER + json.dumps(payload) + "\n"}


class AxisTest(unittest.TestCase):
    def test_every_contract_key_is_classified(self):
        for scenario in load_contract()["scenarios"]:
            for key in scenario["observations"]:
                self.assertIn(runner.axis_of(key), (runner.AXIS_VALUE,
                                                    runner.AXIS_ROLE_SEQUENCE,
                                                    runner.AXIS_IDENTITY))

    def test_role_sequence_axis_holds_the_ordered_role_observation(self):
        self.assertEqual(runner.axis_of("model_message_roles"), runner.AXIS_ROLE_SEQUENCE)
        self.assertEqual(runner.axis_of("receipts"), runner.AXIS_IDENTITY)
        self.assertEqual(runner.axis_of("blocker_detail_rows"), runner.AXIS_IDENTITY)
        self.assertEqual(runner.axis_of("tokenizer_capability"), runner.AXIS_VALUE)


class DiffTest(unittest.TestCase):
    def test_order_divergence_is_named_at_its_position(self):
        diffs = runner.diff_paths(["user", "assistant", "tool"],
                                  ["user", "assistant", "tool", "user"],
                                  "model_message_roles")
        self.assertEqual([entry["path"] for entry in diffs], ["model_message_roles[len]"])
        self.assertEqual(diffs[0]["expected"], 3)
        self.assertEqual(diffs[0]["observed"], 4)

    def test_identity_relation_divergence_is_named_per_field(self):
        diffs = runner.diff_paths([{"call": "call-a", "decision": "decision-a"}],
                                  [{"call": "call-a", "decision": "decision-b"}],
                                  "receipts")
        self.assertEqual([entry["path"] for entry in diffs], ["receipts[0].decision"])
        self.assertEqual(diffs[0]["reason"], "value")

    def test_a_boolean_never_equals_an_integer(self):
        self.assertFalse(runner.typed_equal(True, 1))
        self.assertFalse(runner.typed_equal(1, 1.0))
        self.assertTrue(runner.typed_equal(1, 1))

    def test_extra_observation_is_reported_not_ignored(self):
        diffs = runner.diff_paths({"a": 1}, {"a": 1, "b": 2}, "row")
        self.assertEqual(diffs[0]["path"], "row.b")
        self.assertEqual(diffs[0]["reason"], "unexpected")


class ScopeTest(unittest.TestCase):
    def test_deterministic_scope_leaves_the_resource_scenario_remaining(self):
        contract = load_contract()
        index = runner.index_scenarios(contract)
        runner.validate_axes(index)
        required, remaining = runner.resolve_scope(contract, index, "deterministic")
        self.assertEqual(remaining, ["S10"])
        self.assertEqual(len(required), len(index) - 1)

    def test_complete_scope_requires_every_scenario(self):
        contract = load_contract()
        index = runner.index_scenarios(contract)
        required, remaining = runner.resolve_scope(contract, index, "complete")
        self.assertEqual(len(required), len(index))
        self.assertEqual(remaining, [])

    def test_unregistered_scope_is_refused(self):
        contract = load_contract()
        index = runner.index_scenarios(contract)
        with self.assertRaises(runner.RunnerError):
            runner.resolve_scope(contract, index, "quick")

    def test_scenario_with_an_unregistered_scope_label_is_refused(self):
        contract = load_contract()
        contract["scenarios"][0]["scope"] = "made-up"
        index = runner.index_scenarios(contract)
        with self.assertRaises(runner.RunnerError):
            runner.resolve_scope(contract, index, "complete")

    def test_go_test_command_runs_one_case_of_each_scenario_once(self):
        command = runner.go_test_command(["tests/integration/acceptance"],
                                         ["TestB", "TestA", "TestA"])
        self.assertEqual(command[0], "go")
        self.assertIn("-count=1", command)
        self.assertEqual(command[command.index("-run") + 1], "^(TestA|TestB)$")


class RecordTest(unittest.TestCase):
    def test_records_are_collected_from_the_real_log_marker(self):
        rows = [record_row("S01", "tests/integration/acceptance", "TestOne",
                           {"value": 1})]
        records = runner.collect_records(rows)
        self.assertEqual(records["S01"]["observations"], {"value": 1})

    def test_two_records_for_one_scenario_are_refused(self):
        row = record_row("S01", "tests/integration/acceptance", "TestOne")
        with self.assertRaises(runner.RunnerError):
            runner.collect_records([row, dict(row)])

    def test_a_log_without_a_record_attests_nothing(self):
        package = "tests/integration/acceptance"
        rows = action_rows(MODULE + "/" + package, "TestOne", ["run", "pass"])
        actions = runner.index_actions(rows)
        state = runner.attest("S01", package, "TestOne", None, actions)
        self.assertEqual(state["state"], "unattested")
        self.assertIn("emitted no scenario record", state["reason"])

    def test_a_record_beside_a_real_pass_is_attested(self):
        package = "tests/integration/acceptance"
        rows = action_rows(MODULE + "/" + package, "TestOne", ["run", "pass"])
        rows.append(record_row("S01", package, "TestOne"))
        actions = runner.index_actions(rows)
        record = runner.collect_records(rows)["S01"]
        self.assertEqual(runner.attest("S01", package, "TestOne", record, actions)["state"],
                         "attested")

    def test_a_record_from_another_case_attests_nothing(self):
        package = "tests/integration/acceptance"
        rows = action_rows(MODULE + "/" + package, "TestOne", ["run", "pass"])
        rows.append(record_row("S01", package, "TestOther"))
        actions = runner.index_actions(rows)
        record = runner.collect_records(rows)["S01"]
        state = runner.attest("S01", package, "TestOne", record, actions)
        self.assertEqual(state["state"], "unattested")
        self.assertIn("does not match the contract", state["reason"])


class EndToEndTest(unittest.TestCase):
    """One real run per scope: the invariants that must hold every time."""

    def run_scope(self, scope):
        out = tempfile.mkdtemp(prefix="scenarios-" + scope + "-")
        code, stdout, stderr = run_tool(RUNNER, "--scope", scope, "--out", out, "--json")
        self.assertIn(code, (0, 1, 3), stderr)
        report = json.loads(stdout)
        evidence = json.loads((Path(out) / "evidence.json").read_text(encoding="utf-8"))
        return code, report, evidence, Path(out)

    def test_deterministic_scope_is_attested_on_every_scenario(self):
        code, report, evidence, out = self.run_scope("deterministic")
        self.assertEqual(report["remaining_required"], ["S10"])
        self.assertEqual(report["counts"]["required"], len(report["scenarios"]))
        self.assertEqual(report["counts"]["attested"], len(report["scenarios"]))
        self.assertEqual(report["counts"]["unattested"], 0)
        self.assertIn(report["verdict"], ("match", "drift"))
        self.assertEqual(code, 0 if report["verdict"] == "match" else 3)
        self.assertEqual(evidence["execution"]["exit_code"], 0)
        self.assertEqual(len(evidence["records"]), len(report["scenarios"]))
        self.assertEqual(evidence["test_log_ref"], "test-log.jsonl")
        self.assertTrue((out / "test-log.jsonl").is_file())

    def test_complete_scope_presents_no_out_of_scope_scenario(self):
        _, report, evidence, _ = self.run_scope("complete")
        self.assertEqual(report["counts"]["required"], len(report["scenarios"]))
        covered = {record["scenario_id"] for record in evidence["records"]}
        for state in report["scenarios"]:
            if state["state"] == "unattested":
                self.assertNotIn(state["scenario_id"], covered)

    def test_runner_evidence_is_readable_by_the_checker(self):
        _, report, evidence, out = self.run_scope("deterministic")
        code, stdout, stderr = run_tool(CHECKER, "--evidence", str(out / "evidence.json"),
                                        "--test-log", str(out / "test-log.jsonl"),
                                        "--scope", "deterministic")
        self.assertIn(code, (0, 1), stderr)
        for shape_error in ("empty test selector", "does not match the golden contract",
                            "no run/pass in test-log", "unknown scenario_id",
                            "duplicate evidence record", "has no run+pass"):
            self.assertNotIn(shape_error, stdout)
        self.assertEqual({record["scenario_id"] for record in evidence["records"]},
                         set(report["required"]))


if __name__ == "__main__":
    unittest.main()
