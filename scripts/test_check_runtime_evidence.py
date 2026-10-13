#!/usr/bin/env python3
"""Selftests for check_runtime_evidence.py.

The fixtures deliberately reuse the real golden contract (10 scenarios) so the
asset itself is guarded, and — ONLY inside these fixtures — copy the contract's
expected values into the fake "observed" records. That is explicit fixture
plumbing, not product proof; every test then mutates exactly one thing so the
checker has to reject it.
"""
import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
REPO = SCRIPTS.parent
CHECKER = SCRIPTS / "check_runtime_evidence.py"
REAL_CONTRACT = REPO / "golden" / "runtime" / "scenarios.json"
MODULE = "github.com/SpellingDragon/tagent"


def run_checker(*args):
    proc = subprocess.run([sys.executable, str(CHECKER)] + list(args),
                          capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def load_contract():
    return json.loads(REAL_CONTRACT.read_text(encoding="utf-8"))


def full_module_package(contract_scenario):
    return MODULE + "/" + contract_scenario["test_package"]


def action_rows(package, test, actions):
    return [{"Time": "2026-01-01T00:00:00Z", "Action": action,
             "Package": package, "Test": test, "Output": ""} for action in actions]


def full_test_log(contract, extra_rows=None, single_action=None, subtest=None):
    """A realistic `go test -json` stream.

    A requested subtest is emitted BETWEEN its parent test's run and pass, exactly
    as the real tool nests it, so the checker's window rules are exercised against
    a legal log rather than a convenient one.
    """
    packages = sorted({full_module_package(s) for s in contract["scenarios"]})
    rows = [row for package in packages for row in action_rows(package, "", ["start"])]
    for scenario in contract["scenarios"]:
        package = full_module_package(scenario)
        test = scenario["test_name"]
        if single_action and single_action[0] == scenario["id"]:
            rows.extend(action_rows(package, test, [single_action[1]]))
            continue
        rows.extend(action_rows(package, test, ["run"]))
        if subtest and subtest[0] == scenario["id"]:
            rows.extend(action_rows(package, test + subtest[1], subtest[2]))
        rows.extend(action_rows(package, test, ["pass"]))
    rows.extend(extra_rows or [])
    rows.extend(row for package in packages for row in action_rows(package, "", ["pass"]))
    return rows


def full_evidence(contract, hook=None):
    """Fixture only: observed values start as a copy of the contract's expected."""
    records = []
    for scenario in contract["scenarios"]:
        package, test = scenario["test_package"], scenario["test_name"]
        observations = copy.deepcopy(scenario["observations"])
        if hook:
            package, test, observations = hook(scenario, package, test, observations)
        records.append({
            "scenario_id": scenario["id"],
            "test_package": package,
            "test_name": test,
            "event_refs": ["{}::{}".format(package, test)],
            "observations": observations,
        })
    return {
        "schema_version": 1,
        "runner": {"name": "selftest-fixture", "entrypoint": "test_check_runtime_evidence"},
        "source_fingerprint": {"module_path": MODULE, "revision": "0" * 40,
                               "content_hash": "sha256:" + "0" * 64},
        "test_log_ref": "test-log.jsonl",
        "execution": {"run_id": "fixture-run", "command": "fixture-only", "platform": "selftest", "exit_code": 0},
        "records": records,
    }


class EvidenceTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.contract = load_contract()

    def tearDown(self):
        self._tmp.cleanup()

    def write(self, name, payload):
        path = self.root / name
        if isinstance(payload, str):
            path.write_text(payload, encoding="utf-8")
        else:
            path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        return path

    def write_log(self, rows):
        path = self.root / "test-log.jsonl"
        path.write_text("\n".join(json.dumps(row, ensure_ascii=False) for row in rows) + "\n",
                        encoding="utf-8")
        return path

    def verify(self, evidence, rows=None, scope=None):
        evidence_path = self.write("evidence.json", evidence)
        log_path = self.write_log(rows if rows is not None else full_test_log(self.contract))
        args = ["--contract", str(REAL_CONTRACT), "--evidence", str(evidence_path),
                "--test-log", str(log_path), "--json"]
        if scope is not None:
            args += ["--scope", scope]
        return run_checker(*args)

    def test_happy_path(self):
        code, out, err = self.verify(full_evidence(self.contract))
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["counts"]["scenarios_covered"], 10)
        self.assertEqual(payload["counts"]["scenarios_required"], 10)
        self.assertEqual(payload["extra"]["scope"], "complete")
        self.assertEqual(payload["extra"]["remaining_required"], [])
        self.assertIsNone(payload["productVerified"])

    def test_missing_package_terminal_is_rejected(self):
        rows = [r for r in full_test_log(self.contract) if r["Test"] or r["Action"] != "pass"]
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("missing", out)

    def test_pass_before_run_is_rejected(self):
        rows = full_test_log(self.contract)
        first = self.contract["scenarios"][0]["test_name"]
        for row in rows:
            if row["Test"] == first:
                row["Action"] = "pass" if row["Action"] == "run" else "run"
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("without active run", out)

    def test_unfinished_subtest_is_rejected(self):
        first = self.contract["scenarios"][0]
        rows = full_test_log(self.contract, subtest=(first["id"], "/partial", ["run"]))
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("missing terminal", out)

    def test_parallel_subtest_lifecycle_is_valid(self):
        first = self.contract["scenarios"][0]
        rows = full_test_log(self.contract,
                             subtest=(first["id"], "/parallel", ["run", "pause", "cont", "pass"]))
        code, out, err = self.verify(full_evidence(self.contract), rows)
        self.assertEqual(code, 0, out + err)

    def test_failed_process_exit_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["execution"]["exit_code"] = 1
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("execution.exit_code", out)

    def test_wrong_log_reference_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["test_log_ref"] = "other.jsonl"
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("test_log_ref does not match", out)

    def test_duplicate_records_are_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["records"].append(copy.deepcopy(evidence["records"][0]))
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate evidence record", out)

    def test_contract_only_mode(self):
        code, out, err = run_checker("--contract", str(REAL_CONTRACT), "--check-contract", "--json")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["scenario_count"], 10)

    def test_contract_only_mode_with_plan_graph(self):
        graph = REPO / "openspec" / "changes" / "runtime-rebuild" / "task-graph.json"
        code, out, err = run_checker("--contract", str(REAL_CONTRACT), "--check-contract",
                                     "--plan-graph", str(graph), "--json")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["evidence_contract_fields"], 18)

    def test_evidence_contract_omitting_enforced_field_is_rejected(self):
        contract = load_contract()
        contract["evidence_contract"]["required_fields"] = [
            f for f in contract["evidence_contract"]["required_fields"] if f != "execution"]
        path = self.write("contract_no_exec.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("omits checker-enforced field", out)

    def test_evidence_contract_missing_execution_fields_is_rejected(self):
        contract = load_contract()
        del contract["evidence_contract"]["execution_fields"]
        path = self.write("contract_no_execfields.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("execution_fields", out)

    def test_evidence_contract_extra_field_only_warns(self):
        contract = load_contract()
        contract["evidence_contract"]["required_fields"].append("future_field")
        path = self.write("contract_extra.json", contract)
        code, out, err = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertEqual(code, 0, out + err)

    def test_wrong_value_is_rejected(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S01":
                observations["published_generation_count"] = 7
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("expected 1 but observed 7", out)

    def test_bool_int_type_mismatch_is_rejected(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S02":
                observations["cancel_isolation"] = 1
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("cancel_isolation", out)
        self.assertIn("expected true but observed 1", out)

    def test_missing_case_is_rejected(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S03":
                observations.pop("receipts_per_call")
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("missing observation case 'receipts_per_call'", out)

    def test_unknown_observation_is_rejected(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S04":
                observations["invented_key"] = True
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("unknown observation 'invented_key'", out)

    def test_duplicate_run_is_rejected(self):
        rows = full_test_log(self.contract)
        first = self.contract["scenarios"][0]
        rows += action_rows(full_module_package(first), first["test_name"], ["run"])
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate run", out)

    def test_past_fail_cannot_be_covered_by_later_pass(self):
        rows = full_test_log(self.contract)
        first = self.contract["scenarios"][0]
        rows += action_rows(full_module_package(first), first["test_name"], ["fail", "pass"])
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("recorded fail", out)

    def test_skipped_subtest_is_rejected(self):
        first = self.contract["scenarios"][0]
        rows = full_test_log(self.contract, subtest=(first["id"], "/sub", ["run", "skip"]))
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("subtest", out)

    def test_package_failure_is_rejected(self):
        rows = full_test_log(self.contract)
        first = self.contract["scenarios"][0]
        rows += action_rows(full_module_package(first), "", ["fail"])
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("package-level fail", out)

    def test_missing_run_is_rejected(self):
        rows = full_test_log(self.contract, single_action=("S05", "pass"))
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("no run+pass", out)

    def test_missing_scenario_record_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["records"] = [r for r in evidence["records"] if r["scenario_id"] != "S06"]
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("has no evidence record", out)

    def test_empty_test_selector_is_rejected(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S07":
                test = ""
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("empty test selector", out)

    def test_unknown_scenario_id_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["records"][0]["scenario_id"] = "S99"
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("unknown scenario_id", out)

    def test_duplicate_json_key_in_evidence_is_fatal(self):
        text = json.dumps(full_evidence(self.contract), ensure_ascii=False, indent=2)
        text = text.replace('"schema_version": 1,', '"schema_version": 1,\n  "schema_version": 1,', 1)
        code, out, _ = self.verify(text)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate JSON key", out)

    def test_foreign_package_with_matching_suffix_does_not_match(self):
        rows = []
        for scenario in self.contract["scenarios"]:
            rows.extend(action_rows("example.com/foreign/" + scenario["test_package"],
                                    scenario["test_name"], ["run", "pass"]))
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("no run/pass in test-log", out)

    def test_relative_package_is_normalised(self):
        # evidence keeps the module-relative form; the log uses the full module path.
        code, out, err = self.verify(full_evidence(self.contract))
        self.assertEqual(code, 0, out + err)

    def test_selector_must_match_golden_contract(self):
        def hook(scenario, package, test, observations):
            if scenario["id"] == "S01":
                package = "tests/integration/other"
            return package, test, observations
        code, out, _ = self.verify(full_evidence(self.contract, hook))
        self.assertNotEqual(code, 0)
        self.assertIn("does not match the golden contract", out)

    def test_unreadable_content_hash_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["source_fingerprint"]["content_hash"] = "sha256:not-hex"
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("is not a readable sha256 digest", out)

    def test_unreadable_revision_is_rejected(self):
        evidence = full_evidence(self.contract)
        evidence["source_fingerprint"]["revision"] = "not-a-sha"
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("is not a readable git SHA", out)

    def test_modes_are_mutually_exclusive(self):
        code, _, err = run_checker("--check-contract", "--evidence", "x", "--test-log", "y")
        self.assertEqual(code, 2)
        self.assertIn("mutually exclusive", err)

    def test_no_mode_is_a_strict_failure(self):
        code, _, err = run_checker("--contract", str(REAL_CONTRACT))
        self.assertEqual(code, 2)
        self.assertIn("strict", err)

    def test_scenario_count_must_be_ten(self):
        contract = load_contract()
        contract["scenarios"] = contract["scenarios"][:9]
        path = self.write("contract9.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("expected 10 golden scenarios", out)

    def test_placeholder_observation_is_rejected(self):
        contract = load_contract()
        contract["scenarios"][0]["observations"]["inflight_request_generation"] = "TBD"
        path = self.write("placeholder.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("placeholder", out)

    def test_old_numbering_is_rejected(self):
        contract = load_contract()
        contract["scenarios"][0]["id"] = "F01"
        path = self.write("oldnum.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("must not reuse old F/RV numbering", out)

    def test_real_contract_statuses_are_planned_and_forward_compatible(self):
        # This round's golden is all 'planned'; the checker must not be permanently
        # stuck there, so every status must be drawn from the shared vocabulary.
        statuses = {scenario["status"] for scenario in load_contract()["scenarios"]}
        self.assertEqual(statuses, {"planned"})

    def test_unknown_scenario_status_is_rejected(self):
        contract = load_contract()
        contract["scenarios"][0]["status"] = "finished"
        path = self.write("badstatus.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("status", out)

    def test_done_scenario_without_evidence_is_rejected(self):
        contract = load_contract()
        contract["scenarios"][0]["status"] = "done"
        path = self.write("donenoevidence.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("requires an evidence object", out)

    def test_done_scenario_with_evidence_is_accepted(self):
        contract = load_contract()
        contract["scenarios"][0]["status"] = "done"
        contract["scenarios"][0]["evidence"] = {
            "run_id": "run-2026-01-01",
            "commands": ["go test ./tests/integration/acceptance -run TestScenario01"],
            "exit_codes": [0],
            "artifact_paths": ["artifacts/S01.json"],
        }
        path = self.write("doneok.json", contract)
        code, out, err = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertEqual(code, 0, out + err)

    def test_done_scenario_with_malformed_evidence_is_rejected(self):
        contract = load_contract()
        contract["scenarios"][0]["status"] = "done"
        contract["scenarios"][0]["evidence"] = {
            "run_id": "",
            "commands": [],
            "exit_codes": [True],
            "artifact_paths": [],
        }
        path = self.write("donebad.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("evidence", out)

    # ---- P2-1: package/test lifecycle nesting --------------------------------

    def test_test_event_before_package_start_is_rejected(self):
        first = self.contract["scenarios"][0]
        pkg = full_module_package(first)
        rows = (action_rows(pkg, first["test_name"], ["run", "pass"])
                + action_rows(pkg, "", ["start", "pass"]))
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("outside its package start/end window", out)

    def test_package_terminal_moved_before_its_tests_is_rejected(self):
        # The old success fixture did `rows.insert(1, rows.pop())`, moving the
        # package pass ahead of its own tests; that MUST now be rejected.
        rows = full_test_log(self.contract)
        rows.insert(1, rows.pop())
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("outside its package start/end window", out)

    def test_package_terminal_with_open_test_is_rejected(self):
        first = self.contract["scenarios"][0]
        pkg = full_module_package(first)
        rows = action_rows(pkg, "", ["start"])
        rows += action_rows(pkg, first["test_name"], ["run"])
        rows += action_rows(pkg, "", ["pass"])
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("still open", out)

    def test_multi_package_concurrency_is_valid(self):
        real = full_module_package(self.contract["scenarios"][0])
        other = MODULE + "/tests/integration/otherpkg"
        rows = action_rows(real, "", ["start"]) + action_rows(other, "", ["start"])
        for scenario in self.contract["scenarios"]:
            rows += action_rows(real, scenario["test_name"], ["run", "pass"])
        rows += action_rows(other, "TestOtherPackageLifecycle", ["run", "pass"])
        rows += action_rows(other, "", ["pass"]) + action_rows(real, "", ["pass"])
        code, out, err = self.verify(full_evidence(self.contract), rows)
        self.assertEqual(code, 0, out + err)

    def test_subtest_after_parent_pass_is_rejected(self):
        # The previous (invalid) parallel fixture appended the subtest AFTER the
        # parent's pass; a real go log never does that, so it must be rejected.
        first = self.contract["scenarios"][0]
        pkg = full_module_package(first)
        late = action_rows(pkg, first["test_name"] + "/late", ["run", "pass"])
        rows = full_test_log(self.contract, extra_rows=late)
        code, out, _ = self.verify(full_evidence(self.contract), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("outside its parent test", out)

    # ---- P2-3: scope (deterministic vs complete) -----------------------------

    @staticmethod
    def _drop_record(evidence, scenario_id):
        evidence["records"] = [r for r in evidence["records"] if r["scenario_id"] != scenario_id]

    def test_default_scope_missing_resource_scenario_fails(self):
        evidence = full_evidence(self.contract)
        self._drop_record(evidence, "S10")
        code, out, _ = self.verify(evidence)
        self.assertNotEqual(code, 0)
        self.assertIn("S10", out)
        self.assertIn("has no evidence record", out)

    def test_deterministic_scope_passes_subset_and_reports_debt(self):
        evidence = full_evidence(self.contract)
        self._drop_record(evidence, "S10")
        code, out, err = self.verify(evidence, scope="deterministic")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["extra"]["scope"], "deterministic")
        self.assertEqual(payload["extra"]["remaining_required"], ["S10"])
        self.assertEqual(payload["counts"]["scenarios_required"], 9)
        self.assertEqual(payload["counts"]["scenarios_verified"], 9)
        self.assertEqual(payload["counts"]["scope_is_complete"], 0)
        self.assertIsNone(payload["productVerified"])

    def test_deterministic_scope_rejects_synthetic_resource_record(self):
        # Unauthorized: a deterministic run MUST NOT carry the resource scenario
        # to fabricate completeness.
        code, out, _ = self.verify(full_evidence(self.contract), scope="deterministic")
        self.assertNotEqual(code, 0)
        self.assertIn("outside the selected scope", out)
        self.assertIn("S10", out)

    def test_complete_scope_still_requires_resource_scenario(self):
        evidence = full_evidence(self.contract)
        self._drop_record(evidence, "S10")
        code, out, _ = self.verify(evidence, scope="complete")
        self.assertNotEqual(code, 0)
        self.assertIn("S10", out)

    def test_unknown_scope_is_rejected(self):
        code, out, _ = run_checker("--contract", str(REAL_CONTRACT), "--check-contract",
                                   "--scope", "bogus", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("unknown scope", out)

    def test_scenario_scope_must_be_registered(self):
        contract = load_contract()
        contract["scenarios"][0]["scope"] = "mystery"
        path = self.write("badscopelabel.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("not a registered scope label", out)

    def test_missing_scopes_block_is_rejected(self):
        contract = load_contract()
        del contract["scopes"]
        path = self.write("noscopes.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("scopes must be an object", out)

    def test_duplicate_scope_labels_are_rejected(self):
        contract = load_contract()
        contract["scopes"]["labels"] = ["deterministic", "deterministic"]
        path = self.write("dupscope.json", contract)
        code, out, _ = run_checker("--contract", str(path), "--check-contract", "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("must be unique", out)


if __name__ == "__main__":
    unittest.main()
