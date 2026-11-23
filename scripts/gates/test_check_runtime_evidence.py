#!/usr/bin/env python3
"""Selftests for check_runtime_evidence.py.

The fixtures deliberately reuse the real golden contract (10 scenarios) so the
asset itself is guarded, and — ONLY inside these fixtures — copy the contract's
expected values into the fake "observed" records. That is explicit fixture
plumbing, not product proof; every test then mutates exactly one thing so the
checker has to reject it.
"""
import copy
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
import uuid
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
REPO = SCRIPTS.parents[1]
CHECKER = SCRIPTS / "check_runtime_evidence.py"
REAL_CONTRACT = REPO / "golden" / "runtime" / "scenarios.json"
MODULE = "github.com/SpellingDragon/tagent"

sys.path.insert(0, str(SCRIPTS))
import check_runtime_evidence as checker  # noqa: E402

CHECKER_ENVIRONMENT_NAMES = checker.EVENT_ENVIRONMENT_NAMES


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
        self.assertEqual(payload["counts"]["scenarios_covered"], 15)
        self.assertEqual(payload["counts"]["scenarios_required"], 15)
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
        self.assertEqual(payload["counts"]["scenario_count"], 15)

    def test_contract_only_mode_with_plan_graph(self):
        graph = REPO / "openspec" / "changes" / "archive" / "2026-10-18-runtime-rebuild" / "task-graph.json"
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
        self.assertIn("expected 15 golden scenarios", out)

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
        # The first package terminal moves ahead of its own tests (its start
        # stays active, the tests land after it); that MUST be rejected.
        rows = full_test_log(self.contract)
        packages = sorted({full_module_package(s) for s in self.contract["scenarios"]})
        first_terminal = len(rows) - len(packages)
        rows.insert(1, rows.pop(first_terminal))
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
        packages = sorted({full_module_package(s) for s in self.contract["scenarios"]})
        other = MODULE + "/tests/integration/otherpkg"
        rows = [row for pkg in packages for row in action_rows(pkg, "", ["start"])]
        rows += action_rows(other, "", ["start"])
        for scenario in self.contract["scenarios"]:
            pkg = full_module_package(scenario)
            rows += action_rows(pkg, scenario["test_name"], ["run", "pass"])
        rows += action_rows(other, "TestOtherPackageLifecycle", ["run", "pass"])
        rows += action_rows(other, "", ["pass"])
        rows += [row for pkg in packages for row in action_rows(pkg, "", ["pass"])]
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
        self.assertEqual(payload["counts"]["scenarios_required"], 14)
        self.assertEqual(payload["counts"]["scenarios_verified"], 14)
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


REAL_EVENT_CONTRACT = REPO / "golden" / "runtime" / "event_integrity_scenarios.json"


def load_event_contract():
    return json.loads(REAL_EVENT_CONTRACT.read_text(encoding="utf-8"))


def event_tests(contract, scope):
    return [test for test in contract["tests"] if test["scope"] == scope]


def event_full_package(module_path, test_package):
    """The full package path `go test -json` reports for a contract package."""
    rel = test_package[2:] if test_package.startswith("./") else test_package
    return module_path if rel == "." else module_path + "/" + rel


def event_row(package, test, action):
    return {"Time": "2026-01-01T00:00:00Z", "Action": action,
            "Package": package, "Test": test, "Output": ""}


def event_log(contract, tests):
    """A legal multi-module `go test -json` stream for the selected tests.

    One window per package, subtests nested between their parent's run and pass,
    exactly as the real tool emits them.
    """
    packages = {}
    for test in tests:
        package = event_full_package(test["module_path"], test["test_package"])
        packages.setdefault(package, []).append(test)
    rows = []
    for package in sorted(packages):
        rows.append(event_row(package, "", "start"))
        for test in packages[package]:
            name = test["test_name"]
            rows.append(event_row(package, name, "run"))
            for subtest in test["subtests"]:
                anchor = name + "/" + subtest["name"]
                rows.append(event_row(package, anchor, "run"))
                rows.append(event_row(package, anchor, "pass"))
            rows.append(event_row(package, name, "pass"))
        rows.append(event_row(package, "", "pass"))
    return rows


RUNNER_ENTRYPOINT = Path("scripts") / "scenarios" / "run_runtime_scenarios.py"
GATE_ENTRYPOINT = Path("scripts") / "gates" / "check_event_integrity_mutations.py"
GATE_TOOL_NAME = "check_event_integrity_mutations"


def load_runner():
    """The producer module the checker imports, so the fixtures hold real digests."""
    scenarios = str(SCRIPTS.parent / "scenarios")
    if scenarios not in sys.path:
        sys.path.insert(0, scenarios)
    import run_runtime_scenarios
    return run_runtime_scenarios


runner = load_runner()


def scope_seeds(contract, scope):
    """The module groups one scope selects, as the runner names them."""
    seeds = {}
    for test in event_tests(contract, scope):
        seeds.setdefault(test["dir"], test["module_path"])
    return [{"dir": directory, "module_path": seeds[directory]} for directory in sorted(seeds)]


def event_digest(path):
    return "sha256:" + hashlib.sha256(Path(path).read_bytes()).hexdigest()


EVENT_FIXTURE_BOUND = 600


def event_selectors(tests):
    """The selectors a run declares: the contract's own module/package/name rows."""
    return [{"test_id": test["id"], "module_path": test["module_path"],
             "test_package": test["test_package"], "test_name": test["test_name"]}
            for test in tests]


def event_invocations(tests, seed=None, window=None, bound=EVENT_FIXTURE_BOUND):
    """The argv the runner derives: one invocation per (module dir, tags, race) group.

    Read through the producer rather than restated here: which groups exist, which
    packages share one invocation, and which invocation may carry the matrix pair
    are the runner's own decisions, so a fixture that re-derived them could agree
    with the checker while disagreeing with the tool that writes the evidence.
    """
    index = {test["id"]: test for test in tests}
    invocations = []
    for group in runner.event_groups(index, list(index)):
        for command in runner.event_commands(group, index, seed, window, bound):
            invocations.append({"module_path": group["module_path"], "dir": group["dir"],
                                "packages": runner.command_packages(command),
                                "tags": list(group["tags"]), "race": True,
                                "command": " ".join(command), "exit_code": 0})
    return invocations



def mutation_evidence(contract_digest=None, fingerprints=None, binding=None, tree=None):
    """The 04.4 mutation report plus the baseline the bundle ties it to.

    A legal report names the tested baseline in the runner's own vocabulary —
    the contract digest, the per-module source fingerprints and the source binding
    — so the bundle can prove the mutations were driven against the tree the runs
    exercised, and can re-resolve that closure against the tree it is checked on.
    It also names the gate that decided the verdicts, fingerprinted from `tree`,
    which is the tree the checker reads the report against.
    """
    return {
        "schema_version": 2,
        "profile": "event-integrity",
        "contract_digest": contract_digest,
        "module_fingerprints": fingerprints,
        "source_binding": binding,
        "gate_tool": runner.tool_identity(str(tree or REPO), name=GATE_TOOL_NAME,
                                          entrypoint=GATE_ENTRYPOINT.as_posix()),
        "control_tree_green": True,
        "mutations": [
            {"id": "M01", "status": "killed", "verdict": "killed", "counted_killed": True,
             "expect_assertion": "TestAlpha/positive", "assertion_hit": True},
            {"id": "M02", "status": "killed", "verdict": "killed", "counted_killed": True,
             "expect_assertion": "TestBeta/negative", "assertion_hit": True},
        ],
        "not_evidence_backed": [],
    }


class EventContractTest(unittest.TestCase):
    """Selftests for the schema-v2 event-integrity scenario contract.

    The real contract is the fixture. Every negative case mutates an in-memory
    copy and writes it to a temp file; the checked-in asset is never touched.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def write(self, name, payload):
        path = self.root / name
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        return path

    def contract_check(self, contract):
        path = self.write("event-contract.json", contract)
        return run_checker("--contract", str(path), "--check-contract", "--json")

    def event_graph(self):
        path = (REPO / "openspec" / "changes" / "archive"
                / "2026-10-25-event-consumption-integrity" / "task-graph.json")
        return json.loads(path.read_text(encoding="utf-8"))

    def contract_check_with_graph(self, contract, graph, *extra):
        contract_path = self.write("event-contract.json", contract)
        graph_path = self.write("event-graph.json", graph)
        return run_checker("--contract", str(contract_path), "--check-contract",
                           "--plan-graph", str(graph_path), "--json", *extra)

    def write_projection_source(self, anchors):
        """A minimal source tree whose only event parent is the projection test."""
        root = self.root / "repo"
        path = root / "tests" / "integration" / "history" / "event_integrity_test.go"
        lines = ["package history", "",
                 "func TestEventIntegrityProjection(t *testing.T) {"]
        for name in anchors:
            lines.append('\tt.Run("{}", func(t *testing.T) {{}})'.format(name))
        lines.append("}")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        return root

    def test_every_leaf_is_covered_by_test_or_non_product_evidence(self):
        code, out, err = run_checker("--contract", str(REAL_EVENT_CONTRACT),
                                     "--check-contract", "--json")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        counts = payload["counts"]
        self.assertEqual(counts["event_leaves_total"], 42)
        self.assertEqual(counts["event_leaves_product"], 29)
        self.assertEqual(counts["event_leaves_non_product"], 13)
        self.assertEqual(counts["event_leaves_covered"], 42)
        self.assertEqual(payload["extra"]["leaf_coverage_gaps"], [])

        # Dropping one non-product mapping leaves 04.5 covered by nothing.
        contract = load_event_contract()
        contract["non_product_leaves"] = [entry for entry in contract["non_product_leaves"]
                                          if entry["leaf"] != "04.5"]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("non_product_leaves: missing graph non-product leaf 04.5", out)
        self.assertIn("task-graph leaf 04.5 is covered by neither", out)

    def test_anchor_selectors_exist(self):
        # 04.5 派单正锚名（与台账正锚同体，避免两份实现）。
        self.test_anchor_selectors_exist_in_test_files()

    def test_anchor_selectors_exist_in_test_files(self):
        anchors = ["small_window_fold_and_restore", "unconsumed_not_lost",
                   "rebuild_does_not_refold", "fold_product_no_self_trigger",
                   "actual_request_assembly", "rebuild_refolds_from_wal",
                   "notice_visible_under_fold"]
        root = self.write_projection_source(anchors)
        code, out, err = self.contract_check_with_graph(
            load_event_contract(), self.event_graph(), "--repo-root", str(root))
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["event_anchor_selectors_checked"], 1)
        self.assertEqual(payload["counts"]["event_anchor_selector_mismatches"], 0)
        self.assertEqual(payload["extra"]["selector_checked"],
                         ["02.3/TestEventIntegrityProjection"])
        # All G4/EVID-04 anchors now land in the source; none stays pending.
        self.assertEqual(payload["counts"]["event_pending_anchors"], 0)
        self.assertEqual(payload["extra"]["pending_anchors"], [])

        # A selector that selects nothing is a hole even when the parent exists.
        contract = load_event_contract()
        test = next(item for item in contract["tests"]
                    if item["id"] == "02.3/TestEventIntegrityProjection")
        test["selector"] = "^TestEventIntegrityProjectionLater$"
        code, out, _ = self.contract_check_with_graph(
            contract, self.event_graph(), "--repo-root", str(root))
        self.assertNotEqual(code, 0)
        self.assertIn("does not select the declared test", out)

    def test_renamed_leaf_fails(self):
        graph = self.event_graph()
        for leaf in graph["leaves"]:
            if leaf["id"] == "02.3":
                leaf["id"] = "02.9"
        for entry in graph["test_registry"]:
            if entry["leaf"] == "02.3":
                entry["leaf"] = "02.9"
        code, out, _ = self.contract_check_with_graph(load_event_contract(), graph)
        self.assertNotEqual(code, 0)
        self.assertIn("leaf '02.3' does not match task-graph '02.9'", out)

    def test_missing_leaf_fails(self):
        graph = self.event_graph()
        graph["leaves"] = [leaf for leaf in graph["leaves"] if leaf["id"] != "02.3"]
        code, out, _ = self.contract_check_with_graph(load_event_contract(), graph)
        self.assertNotEqual(code, 0)
        self.assertIn("test_registry 02.3/TestEventIntegrityProjection references leaf 02.3 "
                      "that is not declared", out)

    def test_missing_mandatory_subtest_fails(self):
        contract = load_event_contract()
        test = next(item for item in contract["tests"]
                    if item["id"] == "02.3/TestEventIntegrityProjection")
        test["subtests"] = [subtest for subtest in test["subtests"]
                            if subtest["name"] != "small_window_fold_and_restore"]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("listed by task-graph but missing from event contract", out)

    def test_missing_non_product_mapping_fails(self):
        contract = load_event_contract()
        contract["non_product_leaves"] = [entry for entry in contract["non_product_leaves"]
                                          if entry["leaf"] != "00.1"]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("non_product_leaves: missing graph non-product leaf 00.1", out)

    def test_missing_negative_anchor_fails(self):
        contract = load_event_contract()
        test = next(item for item in contract["tests"]
                    if item["id"] == "01.1/TestEventIntegrityNoticeIdentity")
        test["subtests"] = [subtest for subtest in test["subtests"]
                            if "negative" not in subtest["roles"]]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("no negative subtest", out)

    def test_empty_selector_fails(self):
        contract = load_event_contract()
        contract["tests"][0]["selector"] = ""
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("empty selector", out)

    def test_event_contract_matches_graph_registry_and_sources(self):
        code, out, err = run_checker("--contract", str(REAL_EVENT_CONTRACT),
                                     "--check-contract", "--json")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        counts = payload["counts"]
        self.assertEqual(counts["event_tests_total"], 30)
        self.assertEqual(counts["event_diagnostic_tests"], 1)
        self.assertEqual(counts["event_product_green_tests"], 29)
        self.assertEqual(counts["event_subtests_unique"], 138)
        self.assertEqual(counts["event_green_subtests"], 134)
        self.assertEqual(counts["event_diagnostic_subtests"], 4)
        self.assertEqual(counts["event_families_covered"], 8)
        self.assertEqual(counts["event_non_product_leaves"], 13)
        self.assertEqual(counts["event_whole_chain_entries"], 10)
        self.assertEqual(counts["event_matrix_inputs_total"], 7200)
        self.assertEqual(counts["event_matrix_restarts_total"], 720)
        self.assertEqual(counts["event_scope_event_short_parents"], 29)
        self.assertEqual(counts["event_scope_event_short_anchors"], 135)
        self.assertEqual(counts["event_scope_event_long_parents"], 1)
        self.assertEqual(counts["event_scope_event_long_anchors"], 3)
        # Source discovery grows as the 00—03 leaves land their tests; the exact
        # split is transient, while the checker cross-aligns every discovered
        # parent/subtest with the frozen contract on each run.
        self.assertGreaterEqual(counts["event_source_discovered_tests"], 4)
        self.assertGreaterEqual(counts["event_source_discovered_subtests"], 19)
        self.assertEqual(counts["event_source_discovered_tests"]
                         + counts["event_source_pending_tests"], 30)
        # The superseded control monolith was removed from the tree by the
        # 03.4—03.8 split; the graph still declares it as retained source-only,
        # so the gate lists it as absent instead of pretending the declaration
        # matches the tree.  The off-contract calibration test is present.
        self.assertEqual(counts["event_source_only_tests"], 2)
        self.assertEqual(counts["event_source_source_only_discovered_tests"], 1)
        self.assertEqual(counts["event_source_only_absent_tests"], 1)
        self.assertEqual(payload["extra"]["source_only_discovered"],
                         ["TestEventIntegrityLongRunCalibration"])
        self.assertEqual(payload["extra"]["source_only_absent"],
                         ["TestEventIntegrityControls"])
        # Mapping gate: every leaf is covered, every selector resolves, and the
        # one anchor a leaf hard gate names stays pending instead of vanishing.
        self.assertEqual(counts["event_leaves_total"], 42)
        self.assertEqual(counts["event_leaves_covered"], 42)
        self.assertEqual(payload["extra"]["leaf_coverage_gaps"], [])
        # 源码落地是持续波次：只固定“已检查 + 待发现 = 全部 29 父”这一不变量，
        # 不把某一时点的发现数写成硬编码（01.8/01.9 已落地，01.10a/01.10b 待落地）。
        self.assertGreaterEqual(counts["event_anchor_selectors_checked"], 26)
        self.assertEqual(
            counts["event_anchor_selectors_checked"]
            + counts["event_anchor_selectors_pending"], counts["event_tests_total"])
        self.assertEqual(counts["event_anchor_selector_mismatches"], 0)
        # G4/EVID-04 锚均已落地源码，无 pending；compaction_event_writer 已按用户裁定换为 rebuild_refolds_from_wal。
        self.assertEqual(counts["event_pending_anchors"], 0)
        self.assertEqual(payload["extra"]["pending_anchors"], [])
        # 01.2/01.3 的补差锚已随既有实施波次落入源码（父测试也已 discovered），
        # 不再计入 pending；其门控仍在 graph 内，源码回归缺失时 checker 会重新红。
        self.assertNotIn(
            "TestEventIntegrityConsumptionRecovery/handover_before_confirmation",
            payload["extra"]["pending_anchors"])
        self.assertNotIn(
            "TestEventIntegrityTaskContinuation/notice_input_without_projection",
            payload["extra"]["pending_anchors"])
        self.assertIn(
            "TestEventIntegrityConsumptionRecovery", payload["extra"]["source_discovered"])
        self.assertIn(
            "TestEventIntegrityTaskContinuation", payload["extra"]["source_discovered"])
        # The already-landed parents must be discovered (guards a broken scanner),
        # and their parent names must match the frozen contract.
        discovered = payload["extra"]["source_discovered"]
        for parent in ("TestEventIntegrityReproduction", "TestEventIntegrityNoticeIdentity",
                       "TestEventIntegrityMeditationSchedule", "TestEventIntegrityHarness"):
            self.assertIn(parent, discovered)
        self.assertIsNone(payload["productVerified"])
        # The contract mirrors the graph registry; the graph is the only source.
        graph = json.loads((REPO / "openspec" / "changes" / "archive"
                            / "2026-10-25-event-consumption-integrity"
                            / "task-graph.json").read_text(encoding="utf-8"))
        registry_ids = {entry["contract_id"] for entry in graph["test_registry"]}
        contract_ids = {test["id"] for test in load_event_contract()["tests"]}
        self.assertEqual(contract_ids, registry_ids)

    def test_whole_chain_entries_are_aggregate_only(self):
        contract = load_event_contract()
        self.assertEqual(len(contract["whole_chain"]), 10)
        self.assertEqual([entry["id"] for entry in contract["whole_chain"]],
                         ["EI%02d" % number for number in range(1, 11)])
        for entry in contract["whole_chain"]:
            self.assertIs(entry["aggregate_only"], True)
        contract["whole_chain"][0]["aggregate_only"] = False
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("aggregate_only must be true", out)

    def test_event_contract_dual_role_consumer_is_accepted(self):
        # 01.3 has no standalone consumer row in the graph; one positive row may
        # carry positive+consumer and own the anchor. No second entity is created.
        contract = load_event_contract()
        test = next(item for item in contract["tests"]
                    if item["id"] == "01.3/TestEventIntegrityTaskContinuation")
        previous = next(item for item in test["subtests"]
                        if item["name"] == test["consumer_anchor"])
        previous["roles"] = ["positive"]
        anchor = "late_notice_not_dropped"
        row = next(item for item in test["subtests"] if item["name"] == anchor)
        self.assertEqual(row["roles"], ["positive"])
        row["roles"] = ["positive", "consumer"]
        test["consumer_anchor"] = anchor
        code, out, err = self.contract_check(contract)
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["event_dual_role_subtests"], 12)
        self.assertEqual(payload["counts"]["event_subtests_unique"], 138)

    def test_event_contract_rejects_reviving_superseded_source_only_test(self):
        contract = load_event_contract()
        monolith = copy.deepcopy(next(item for item in contract["tests"]
                                      if item["id"] == "03.9/TestEventIntegrityRestart"))
        monolith.update({
            "id": "03.3/TestEventIntegrityControls",
            "leaf": "03.3",
            "test_name": "TestEventIntegrityControls",
            "selector": "^TestEventIntegrityControls$",
        })
        contract["tests"].append(monolith)
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("source-only test TestEventIntegrityControls must not also be registered",
                      out)

    def test_event_contract_missing_family_is_rejected(self):
        contract = load_event_contract()
        contract["families"] = [entry for entry in contract["families"]
                                 if entry["id"] != "E8"]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("families must declare exactly E1—E8", out)

    def test_event_contract_missing_negative_anchor_is_rejected(self):
        contract = load_event_contract()
        test = next(item for item in contract["tests"]
                    if item["id"] == "01.1/TestEventIntegrityNoticeIdentity")
        test["subtests"] = [item for item in test["subtests"]
                            if "negative" not in item["roles"]]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("no negative subtest", out)

    def test_event_contract_duplicate_test_or_subtest_is_rejected(self):
        base = load_event_contract()

        duplicate_test = copy.deepcopy(base)
        duplicate_test["tests"].append(copy.deepcopy(duplicate_test["tests"][0]))
        code, out, _ = self.contract_check(duplicate_test)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate test id", out)

        duplicate_subtest = copy.deepcopy(base)
        test = next(item for item in duplicate_subtest["tests"]
                    if item["id"] == "01.1/TestEventIntegrityNoticeIdentity")
        test["subtests"].append(copy.deepcopy(test["subtests"][0]))
        code, out, _ = self.contract_check(duplicate_subtest)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate subtest entity registration", out)
        self.assertIn("same (parent,name) key", out)

        # A single row carrying multiple legal roles is one entity, not a duplicate.
        self.assertEqual(self.contract_check(base)[0], 0)

    def test_event_contract_empty_selector_is_rejected(self):
        contract = load_event_contract()
        contract["tests"][0]["selector"] = ""
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("empty selector", out)

    def test_event_contract_deleting_graph_entry_is_rejected(self):
        contract = load_event_contract()
        contract["tests"] = [item for item in contract["tests"]
                             if item["id"] != "02.7/TestEventIntegrityCapture"]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("graph test 02.7/TestEventIntegrityCapture is missing from event contract",
                      out)

    def test_event_contract_matrix_shrink_is_rejected(self):
        contract = load_event_contract()
        contract["matrix"]["seeds"] = [1, 7]
        code, out, _ = self.contract_check(contract)
        self.assertNotEqual(code, 0)
        self.assertIn("matrix.seeds must stay at", out)


class EventFixture:
    """Schema-v2 evidence fixtures.  Logs are synthetic JSONL; Go is never run.

    The real golden contract is the fixture so the checked-in asset stays
    guarded, while every negative case mutates exactly one thing.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.contract = load_event_contract()
        self.digest = event_digest(REAL_EVENT_CONTRACT)
        self.tree = self.root / "tree"
        self.bindings = {}
        self.write_source_tree()

    def tearDown(self):
        self._tmp.cleanup()

    def write_source_tree(self):
        """The tree the evidence claims to describe, laid out as the repository is.

        The checker re-resolves the recorded closure against `--repo-root` and
        recomputes every module's content digest, so a fixture that only agreed
        with itself would now be rejected — correctly. The fixture tree therefore
        holds the same module layout the real repository has: the root module plus
        the two example modules whose go.mod replaces it.
        """
        self.tree.mkdir()
        (self.tree / "main.go").write_text("package app\n", encoding="utf-8")
        (self.tree / "go.mod").write_text("module {}\n".format(MODULE), encoding="utf-8")
        (self.tree / "go.sum").write_text("", encoding="utf-8")
        for relative, module_path in (("examples/wechat-bot", "wechat-bot"),
                                      ("examples/cli-host", "tagent-cli-host")):
            base = self.tree / relative
            base.mkdir(parents=True)
            (base / "go.mod").write_text(
                "module {}\n\nreplace {} => ../..\n".format(module_path, MODULE),
                encoding="utf-8")
            (base / "go.sum").write_text("", encoding="utf-8")
            (base / "smoke_test.go").write_text("package smoke\n", encoding="utf-8")
        tool = self.tree / RUNNER_ENTRYPOINT
        tool.parent.mkdir(parents=True)
        tool.write_text("# fixture runner stand-in\n", encoding="utf-8")
        gate = self.tree / GATE_ENTRYPOINT
        gate.parent.mkdir(parents=True, exist_ok=True)
        gate.write_text("# fixture mutation gate stand-in\n", encoding="utf-8")

    def binding(self, scope, revision="0" * 40, tree=None):
        """The closure one scope compiles in the fixture tree, as the runner records it.

        Cached per (scope, tree): a bundle carries seventeen evidence files and one
        closure, and re-hashing the tree seventeen times would only cost time.
        """
        key = (scope, str(tree or self.tree), revision)
        if key not in self.bindings:
            self.bindings[key] = copy.deepcopy(
                runner.source_binding(str(tree or self.tree),
                                      scope_seeds(self.contract, scope), revision=revision))
        binding = copy.deepcopy(self.bindings[key])
        window = {"digest": binding["digest"], "modules": len(binding["modules"])}
        binding["before"] = window
        binding["after"] = dict(window)
        return binding

    def evidence(self, scope, tests=None, run_id=None, repeat_index=1, repeat_total=None,
                 seed=None, window=None, tree=None):
        tests = tests if tests is not None else event_tests(self.contract, scope)
        if repeat_total is None:
            repeat_total = next(entry["repeat"] for entry in self.contract["scopes"]
                                if entry["name"] == scope)
        binding = self.binding(scope, tree=tree)
        return {
            "schema_version": 2,
            "profile": "event-integrity",
            "run_id": run_id or str(uuid.uuid4()),
            "scope": scope,
            "repeat_index": repeat_index,
            "repeat_total": repeat_total,
            "contract_digest": self.digest,
            "test_ids": [test["id"] for test in tests],
            "module_fingerprints": binding["modules"],
            "source_binding": binding,
            "run_config": {
                "seed": seed,
                "window": window,
                "tags": sorted({tag for test in tests for tag in test.get("tags") or []}),
                "race": True,
                "environment": {"GOMAXPROCS": "4"},
                "selectors": event_selectors(tests),
                "invocations": event_invocations(tests, seed, window),
            },
            "liveness": {"name": "go_test_bound", "state": "completed",
                         "bound_seconds": EVENT_FIXTURE_BOUND},
            "go_test_exit_code": 0,
            "test_log": "",
            "test_log_sha256": "",
            "subtests": [{"test_id": test["id"], "name": subtest["name"],
                          "status": "pass"}
                         for test in tests for subtest in test["subtests"]],
            "verdict": "match",
        }

    def write_run(self, directory, evidence, rows=None, log_from=None):
        directory = Path(directory)
        directory.mkdir(parents=True, exist_ok=True)
        if log_from is None:
            log_path = directory / "test-log.jsonl"
            log_path.write_text("".join(json.dumps(row) + "\n" for row in rows),
                                encoding="utf-8")
        else:
            log_path = Path(log_from)
        evidence = copy.deepcopy(evidence)
        evidence["test_log"] = os.path.relpath(log_path, directory)
        evidence["test_log_sha256"] = event_digest(log_path)
        path = directory / "evidence.json"
        path.write_text(json.dumps(evidence, ensure_ascii=False, indent=2), encoding="utf-8")
        return path

    def verify(self, evidence, rows, *extra, repo_root=None):
        path = self.write_run(self.root / "run", evidence, rows)
        return run_checker("--contract", str(REAL_EVENT_CONTRACT),
                           "--repo-root", str(repo_root or self.tree), "--evidence", str(path),
                           "--json", *extra)

    def write_bundle(self, name, short_count=10, combos=None, mutation=True,
                     short_hook=None, long_hook=None, tree=None):
        bundle = self.root / name
        bundle.mkdir(parents=True, exist_ok=True)
        short_tests = event_tests(self.contract, "event-short")
        long_tests = event_tests(self.contract, "event-long")
        short_log = event_log(self.contract, short_tests)
        long_log = event_log(self.contract, long_tests)
        if combos is None:
            combos = [(seed, window) for seed in (1, 7, 42) for window in (256, 32768)]
        for index in range(1, short_count + 1):
            evidence = self.evidence("event-short", short_tests, repeat_index=index,
                                     repeat_total=10, tree=tree)
            if short_hook:
                evidence = short_hook(index, evidence)
            self.write_run(bundle / "short-{}".format(index), evidence, short_log)
        for seed, window in combos:
            evidence = self.evidence("event-long", long_tests, repeat_index=1,
                                     repeat_total=1, seed=seed, window=window, tree=tree)
            if long_hook:
                evidence = long_hook(seed, window, evidence)
            self.write_run(bundle / "long-{}-{}".format(seed, window), evidence, long_log)
        if mutation:
            mutations = bundle / "mutations"
            mutations.mkdir()
            short_binding = self.binding("event-short", tree=tree)
            (mutations / "evidence.json").write_text(
                json.dumps(mutation_evidence(self.digest, short_binding["modules"],
                                             short_binding, tree=tree or self.tree),
                           ensure_ascii=False, indent=2),
                encoding="utf-8")
        return bundle

    def check_bundle(self, bundle, repo_root=None):
        return run_checker("--contract", str(REAL_EVENT_CONTRACT),
                           "--repo-root", str(repo_root or self.tree), "--bundle", str(bundle),
                           "--json")


class EventEvidenceTest(EventFixture, unittest.TestCase):
    """One schema-v2 evidence file against its contract, log and anchors.

    The v1 negatives below keep their v1 fixtures; the same names in this class
    assert the v2 envelope (UUID run ids, log digests, anchor verdicts).
    """

    def test_v2_short_round_evidence_is_accepted(self):
        tests = event_tests(self.contract, "event-short")
        code, out, err = self.verify(self.evidence("event-short", tests),
                                     event_log(self.contract, tests))
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["event_tests_checked"], 29)
        self.assertEqual(payload["counts"]["event_anchors_checked"], 135)
        self.assertEqual(payload["extra"]["repeat_index"], 1)
        self.assertEqual(payload["extra"]["repeat_total"], 10)
        self.assertIsNone(payload["productVerified"])

    def test_v2_long_round_evidence_is_accepted(self):
        tests = event_tests(self.contract, "event-long")
        code, out, err = self.verify(self.evidence("event-long", tests, seed=7, window=256),
                                     event_log(self.contract, tests))
        self.assertEqual(code, 0, out + err)

    def test_a_command_without_count_one_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["invocations"][0]["command"] = (
            evidence["run_config"]["invocations"][0]["command"].replace(" -count=1", ""))
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("no '-count=1'", out)

    def test_a_command_run_in_another_directory_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        invocation = evidence["run_config"]["invocations"][0]
        invocation["command"] = invocation["command"].replace(
            "go -C {} test".format(invocation["dir"]), "go -C /elsewhere test")
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("the recorded argv runs in", out)

    def test_a_command_that_omits_a_selected_package_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        invocation = evidence["run_config"]["invocations"][0]
        dropped = invocation["packages"].pop()
        invocation["command"] = invocation["command"].replace(" " + dropped, "", 1)
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("no invocation runs selected package", out)

    def test_a_command_that_names_another_test_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        invocation = evidence["run_config"]["invocations"][0]
        dropped = sorted({test["test_name"] for test in tests
                          if test["dir"] == invocation["dir"]})[0]
        tokens = invocation["command"].split()
        tokens[tokens.index("-run") + 1] = tokens[tokens.index("-run") + 1].replace(
            dropped, "TestUnrelated")
        invocation["command"] = " ".join(tokens)
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("no '-run' selector names selected test", out)

    def test_a_selector_that_names_another_test_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["selectors"][0]["test_name"] = "TestSomethingElse"
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("drives test", out)

    def test_a_run_whose_own_bound_fired_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["liveness"]["state"] = "expired"
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("the run's own bound fired", out)

    def test_an_inner_timeout_outliving_the_runner_bound_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["invocations"][0]["command"] = (
            evidence["run_config"]["invocations"][0]["command"].replace(
                "-timeout {}s".format(EVENT_FIXTURE_BOUND),
                "-timeout {}s".format(EVENT_FIXTURE_BOUND * 2)))
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("nested interval", out)

    def test_a_command_without_its_own_timeout_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["invocations"][0]["command"] = (
            evidence["run_config"]["invocations"][0]["command"].replace(
                " -timeout {}s".format(EVENT_FIXTURE_BOUND), ""))
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("nested interval", out)

    def test_a_failed_invocation_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["invocations"][0]["exit_code"] = 2
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("cannot be evidence for a green round", out)

    def test_a_goflags_flag_no_recorded_argv_decides_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["environment"] = {"GOFLAGS": "-short -count=1"}
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("would not be the command that ran", out)
        self.assertIn("-short", out)
        self.assertNotIn("-count=1', which no recorded argv", out)

    def test_a_complete_consistent_environment_record_is_accepted(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["environment"] = {
            "GOFLAGS": "-count=1 -json",
            "GOMAXPROCS": "4",
            "GODEBUG": "",
            "TAGENT_CALIBRATION_MODE": "host",
        }
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertEqual(code, 0, out + err)

    def test_a_missing_environment_record_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        del evidence["run_config"]["environment"]
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("run_config.environment is absent", out)

    def test_a_gomaxprocs_the_runtime_ignores_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["environment"] = {"GOMAXPROCS": "auto"}
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("the runtime ignores", out)
        self.assertIn("GOMAXPROCS", out)

    def test_a_credential_bearing_variable_is_rejected_and_its_value_never_echoed(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["run_config"]["environment"] = {"TAGENT_RL_AUTH_TOKEN": "s3cret-token"}
        code, out, err = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("outside the recorded determinants", out)
        self.assertIn("TAGENT_RL_AUTH_TOKEN", out)
        self.assertNotIn("s3cret-token", out + err)

    def test_the_recorded_determinants_match_the_runner_declaration(self):
        """The checker's declaration and the producer's must not drift apart."""
        sys.path.insert(0, str(REPO / "scripts" / "scenarios"))
        try:
            import run_runtime_scenarios as runner
        finally:
            sys.path.pop(0)
        self.assertEqual(tuple(sorted(runner.EVENT_ENVIRONMENT_NAMES)),
                         tuple(sorted(CHECKER_ENVIRONMENT_NAMES)))

    def test_skipped_subtest_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        rows = event_log(self.contract, tests)
        anchor = tests[0]["test_name"] + "/" + tests[0]["subtests"][0]["name"]
        for row in rows:
            if row["Test"] == anchor and row["Action"] == "pass":
                row["Action"] = "skip"
                break
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("was skipped", out)

    def test_missing_package_terminal_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        rows = event_log(self.contract, tests)
        self.assertEqual(rows[-1]["Test"], "")
        code, out, _ = self.verify(self.evidence("event-short", tests), rows[:-1])
        self.assertNotEqual(code, 0)
        self.assertIn("missing terminal", out)

    def test_failed_process_exit_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["go_test_exit_code"] = 1
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("go_test_exit_code", out)

    def test_missing_run_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        name = test["test_name"]
        package = event_full_package(test["module_path"], test["test_package"])
        rows = [row for row in event_log(self.contract, tests)
                if not (row["Package"] == package
                        and (row["Test"] == name or row["Test"].startswith(name + "/")))]
        terminal = next(index for index, row in enumerate(rows)
                        if row["Package"] == package and row["Test"] == ""
                        and row["Action"] == "pass")
        rows.insert(terminal, event_row(package, name, "pass"))
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("no run+pass", out)

    def test_fabricated_record_is_rejected(self):
        # The evidence claims a passing round; the log shows that case never ran.
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        name = test["test_name"]
        package = event_full_package(test["module_path"], test["test_package"])
        rows = [row for row in event_log(self.contract, tests)
                if not (row["Package"] == package
                        and (row["Test"] == name or row["Test"].startswith(name + "/")))]
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("no run/pass in test-log", out)
        self.assertIn("zero match", out)

    def test_duplicate_run_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        name = test["test_name"]
        package = event_full_package(test["module_path"], test["test_package"])
        rows = event_log(self.contract, tests)
        passed = next(index for index, row in enumerate(rows)
                      if row["Package"] == package and row["Test"] == name
                      and row["Action"] == "pass")
        rows.insert(passed + 1, event_row(package, name, "run"))
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate run", out)

    def test_post_hoc_pass_cannot_cover_a_failing_run(self):
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        name = test["test_name"]
        package = event_full_package(test["module_path"], test["test_package"])
        rows = event_log(self.contract, tests)
        for row in rows:
            if (row["Package"] == package and row["Test"] == name
                    and row["Action"] == "pass"):
                row["Action"] = "fail"
                break
        terminal = next(index for index, row in enumerate(rows)
                        if row["Package"] == package and row["Test"] == ""
                        and row["Action"] == "pass")
        rows[terminal:terminal] = [event_row(package, name, "run"),
                                   event_row(package, name, "pass")]
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0)
        self.assertIn("recorded fail", out)

    def test_missing_mandatory_anchor_verdict_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        evidence["subtests"].pop()
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("missing verdict for mandatory anchor", out)

    def anchor_rows(self, tests, anchor):
        """One anchor's rows in a legal log, as (package, [rows]) for rewriting."""
        package = event_full_package(tests[0]["module_path"], tests[0]["test_package"])
        return package, [row for row in event_log(self.contract, tests)
                         if row["Package"] == package and row["Test"] == anchor]

    def test_raw_child_events_determine_subtest_verdict(self):
        """合法原始 run/pass 被接受，且被判的那个判词就是日志子行派生的判词。

        证据里的 `subtests[].status` 是生产者自己的话；run 的原始子测试行才是判据。
        两件事同时成立才算这条锚成立：合法日志被接受，且 checker 的判词与生产者的
        派生（import 而非复述）逐条相同——否则同一份日志会有两个解释。
        """
        tests = event_tests(self.contract, "event-short")
        rows = event_log(self.contract, tests)
        evidence = self.evidence("event-short", tests)
        actions = runner.index_event_actions(
            rows, sorted({test["module_path"] for test in tests}))
        derived = {}
        for test in tests:
            for state in runner.event_subtest_states(test, actions):
                derived[(test["id"], state["name"])] = state["status"]
        self.assertEqual(set(derived.values()), {"pass"})
        self.assertEqual({(entry["test_id"], entry["name"]): entry["status"]
                          for entry in evidence["subtests"]}, derived)
        code, out, err = self.verify(evidence, rows)
        self.assertEqual(code, 0, out + err)
        self.assertEqual(json.loads(out)["counts"]["event_anchors_checked"], len(derived))

    def test_subtest_without_run_is_refused(self):
        """父测试通过而子锚从未 run：自报 pass 不再成立，按缺 run 点名该 (parent,name)。"""
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        anchor = test["test_name"] + "/" + test["subtests"][0]["name"]
        package, _ = self.anchor_rows(tests, anchor)
        rows = [row for row in event_log(self.contract, tests)
                if not (row["Package"] == package and row["Test"] == anchor)]
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0, out)
        self.assertIn("({},{}) states 'pass' but the test-log child events show 'missing'".format(
            test["id"], test["subtests"][0]["name"]), out)

    def test_parent_pass_cannot_invent_a_subtest_pass(self):
        """父的 run+pass 有效，不能因此替一个没出现的子锚作保。

        这里子测试行仍在日志里，但记在另一个名字下：父 pass 与某一子行的 pass 一起
        存在，只有被证据点名的那枚锚没有任何行。父通过不是子通过的凭据。
        """
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        claimed = test["subtests"][0]["name"]
        anchor = test["test_name"] + "/" + claimed
        package, anchor_rows = self.anchor_rows(tests, anchor)
        self.assertEqual(len(anchor_rows), 2)
        rows = event_log(self.contract, tests)
        for row in rows:
            if row["Package"] == package and row["Test"] == anchor:
                row["Test"] = anchor + "-other"
        parent_terminals = [row for row in rows
                            if row["Package"] == package and row["Test"] == test["test_name"]
                            and row["Action"] == "pass"]
        self.assertEqual(len(parent_terminals), 1)
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0, out)
        self.assertIn("({},{}) states 'pass' but the test-log child events show 'missing'".format(
            test["id"], claimed), out)

    def test_same_log_has_one_verdict_across_consumers(self):
        """同一份日志在 runner 与 checker 里是同一个判词，不是两个。

        runner 用 `attest_event_test` 说该测试未被证明并点名那枚锚；checker 必须
        对同一份日志给出同一个派生（该锚 missing）并拒绝，否则一条日志就有了两套解释。
        """
        tests = event_tests(self.contract, "event-short")
        test = tests[0]
        claimed = test["subtests"][0]["name"]
        anchor = test["test_name"] + "/" + claimed
        package, _ = self.anchor_rows(tests, anchor)
        rows = [row for row in event_log(self.contract, tests)
                if not (row["Package"] == package and row["Test"] == anchor)]
        actions = runner.index_event_actions(
            rows, sorted({entry["module_path"] for entry in tests}))
        attested = runner.attest_event_test(test, actions)
        self.assertEqual(attested["state"], "unattested")
        self.assertIn(claimed, attested["reason"])
        derived = {state["name"]: state["status"]
                   for state in runner.event_subtest_states(test, actions)}
        self.assertEqual(derived[claimed], "missing")
        code, out, _ = self.verify(self.evidence("event-short", tests), rows)
        self.assertNotEqual(code, 0, out)
        self.assertIn("({},{}) states 'pass' but the test-log child events show {!r}".format(
            test["id"], claimed, derived[claimed]), out)

    def test_off_matrix_seed_window_is_rejected(self):
        tests = event_tests(self.contract, "event-long")
        evidence = self.evidence("event-long", tests, seed=1, window=512)
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0)
        self.assertIn("not a contract matrix combination", out)

    def test_a_bearer_command_that_drops_the_matrix_pair_is_rejected(self):
        # 负锚（long 删参）：长跑唯一的调用正是解析 -seed/-window 的那一次构建，
        # run_config 仍自报 1/256，但 argv 里没有 -args，测试什么也没收到。
        tests = event_tests(self.contract, "event-long")
        evidence = self.evidence("event-long", tests, seed=1, window=256)
        invocation = evidence["run_config"]["invocations"][0]
        invocation["command"] = invocation["command"].replace(
            " -args -seed=1 -window=256", "")
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0, out)
        self.assertIn("carries no '-args'", out)
        self.assertIn("never reached the tests", out)

    def test_a_matrix_pair_broadcast_to_packages_that_cannot_parse_it_is_rejected(self):
        # 负锚（short 错广播每包）：-args 会到达一次调用命名的每个包，根模块与
        # cli-host 都没有编译声明 -seed/-window 的文件，广播即在测试前退出。
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests, seed=1, window=256)
        invocation = next(entry for entry in evidence["run_config"]["invocations"]
                          if entry["module_path"] == MODULE)
        invocation["command"] += " -args -seed=1 -window=256"
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0, out)
        self.assertIn("do not all compile the file declaring those flags", out)
        self.assertIn("flag provided but not defined", out)

    def test_a_matrix_pair_the_argv_does_not_state_is_rejected(self):
        # 负锚（值一致）：承载调用带了 -args，但给测试的是 7/256，run_config 说 1/256。
        tests = event_tests(self.contract, "event-long")
        evidence = self.evidence("event-long", tests, seed=1, window=256)
        invocation = evidence["run_config"]["invocations"][0]
        invocation["command"] = invocation["command"].replace("-seed=1", "-seed=7")
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0, out)
        self.assertIn("hands '-seed=7'", out)

    def test_a_run_declaring_no_pair_cannot_carry_matrix_args(self):
        # 负锚（无对不成对）：短跑不声明 seed/window，argv 却把一次校准塞给测试。
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        invocation = next(entry for entry in evidence["run_config"]["invocations"]
                          if entry["module_path"] == "wechat-bot"
                          and entry["tags"] == ["smoke"])
        invocation["command"] += " -args -seed=1 -window=256"
        code, out, _ = self.verify(evidence, event_log(self.contract, tests))
        self.assertNotEqual(code, 0, out)
        self.assertIn("declares no matrix pair", out)

    def test_replaced_test_log_is_rejected(self):
        tests = event_tests(self.contract, "event-short")
        evidence = self.evidence("event-short", tests)
        path = self.write_run(self.root / "run", evidence, event_log(self.contract, tests))
        log = path.parent / "test-log.jsonl"
        log.write_text(log.read_text(encoding="utf-8") + "\n", encoding="utf-8")
        code, out, _ = run_checker("--contract", str(REAL_EVENT_CONTRACT),
                                   "--evidence", str(path), "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("test_log_sha256 does not match", out)


class EventBundleTest(EventFixture, unittest.TestCase):
    """`--bundle` summary mode: ten short rounds, six long combos, one mutation report."""

    def test_bundle_accepts_ten_short_six_long_one_mutation(self):
        bundle = self.write_bundle("complete")
        code, out, err = self.check_bundle(bundle)
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["bundle_short_evidence"], 10)
        self.assertEqual(payload["counts"]["bundle_long_evidence"], 6)
        self.assertEqual(payload["counts"]["bundle_mutation_evidence"], 1)
        summary = payload["extra"]["bundle"]
        self.assertEqual(len(summary["short_run_ids"]), 10)
        self.assertEqual(summary["long_combos"],
                         [[1, 256], [1, 32768], [7, 256], [7, 32768], [42, 256], [42, 32768]])
        self.assertEqual(len(summary["test_ids"]), 29)
        self.assertIsNone(payload["productVerified"])

    def test_bundle_is_readable_after_it_is_copied_off_the_producer(self):
        # CI uploads each artifact on the runner that produced it and checks the
        # downloaded copy on another runner, where no producer path exists. A
        # bundle that names its logs by their producing path reads green here
        # and red there; the copy must be checked with the original gone.
        bundle = self.write_bundle("portable")
        moved = self.root / "elsewhere" / "bundle"
        shutil.copytree(bundle, moved)
        shutil.rmtree(bundle)
        code, out, err = self.check_bundle(moved)
        self.assertEqual(code, 0, out + err)
        self.assertEqual(json.loads(out)["counts"]["bundle_evidence_files"], 17)

    def test_scope_specific_module_sets_are_not_baseline_drift(self):
        # Every scope records the closure it compiles, not just the modules it
        # selected: event-short drives the root, the CLI host and the smoke module,
        # event-long drives the smoke module alone and still compiles the root
        # through `replace ... => ../..`. The gate must accept that difference
        # instead of demanding identical closure lists.
        bundle = self.write_bundle("scope-specific-modules")
        short = json.loads((bundle / "short-1" / "evidence.json").read_text(encoding="utf-8"))
        long_evidence = json.loads((bundle / "long-1-256" / "evidence.json").read_text(
            encoding="utf-8"))
        self.assertEqual([entry["module_path"] for entry in short["module_fingerprints"]],
                         [MODULE, "tagent-cli-host", "wechat-bot"])
        self.assertEqual([entry["module_path"] for entry in long_evidence["module_fingerprints"]],
                         [MODULE, "wechat-bot"])
        self.assertEqual({entry["dir"] for entry in long_evidence["module_fingerprints"]},
                         {".", "examples/wechat-bot"})
        code, out, err = self.check_bundle(bundle)
        self.assertEqual(code, 0, out + err)

        # The shared module is the one place both scopes can contradict each
        # other: a different digest on the smoke module is drift, not scope.
        shared = next(entry for entry in long_evidence["module_fingerprints"]
                      if entry["module_path"] == "wechat-bot")
        shared["content_hash"] = "sha256:" + "9" * 64
        (bundle / "long-1-256" / "evidence.json").write_text(
            json.dumps(long_evidence, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("different baseline content digest", out)
        self.assertIn("wechat-bot", out)

        # A different revision for the shared module tells the same story.
        bundle = self.write_bundle("scope-specific-revision")
        path = bundle / "long-1-256" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        drifted = next(entry for entry in payload["module_fingerprints"]
                       if entry["module_path"] == "wechat-bot")
        drifted["revision"] = "f" * 40
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("different baseline revision", out)
        self.assertIn("wechat-bot", out)

    def test_nine_of_ten_runs_fail(self):
        bundle = self.write_bundle("nine-of-ten", short_count=9)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("exactly 10 event-short evidence file(s), found 9", out)
        self.assertIn("repeat_index coverage", out)

    def test_five_of_six_combos_fail(self):
        combos = [(1, 256), (1, 32768), (7, 256), (7, 32768), (42, 256)]
        bundle = self.write_bundle("five-of-six", combos=combos)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("exactly 6 event-long evidence file(s) (one per matrix combination), found 5",
                      out)
        self.assertIn("missing event-long matrix combination(s)", out)
        self.assertIn("(42, 32768)", out)

    def test_empty_bundle_directory_fails(self):
        empty = self.root / "empty"
        empty.mkdir()
        code, out, _ = self.check_bundle(empty)
        self.assertNotEqual(code, 0)
        self.assertIn("holds no evidence.json", out)

    def test_reused_run_id_fails(self):
        # One execution credential cannot stand for two independent rounds.
        shared = str(uuid.uuid4())

        def reuse(index, evidence):
            if index in (1, 2):
                evidence["run_id"] = shared
            return evidence

        bundle = self.write_bundle("reused-run-id", short_hook=reuse)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("bundle reuses run_id", out)

    def test_execution_credential_impersonation_fails(self):
        # Positive control: one round legally carries all 29 cases of the scope.
        baseline = self.write_bundle("baseline")
        code, out, err = self.check_bundle(baseline)
        self.assertEqual(code, 0, out + err)
        self.assertEqual(len(json.loads(out)["extra"]["bundle"]["test_ids"]), 29)

        # One run_id cannot stand for two independent rounds.
        shared = str(uuid.uuid4())

        def reuse_run_id(index, evidence):
            if index in (1, 2):
                evidence["run_id"] = shared
            return evidence

        bundle = self.write_bundle("impersonate-run", short_hook=reuse_run_id)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("reuses run_id", out)

        # Reusing the same test log is the same forged credential.
        bundle = self.write_bundle("impersonate-log")
        first = json.loads((bundle / "short-1" / "evidence.json").read_text(encoding="utf-8"))
        second_path = bundle / "short-2" / "evidence.json"
        second = json.loads(second_path.read_text(encoding="utf-8"))
        second["test_log"] = os.path.relpath(bundle / "short-1" / "test-log.jsonl",
                                             bundle / "short-2")
        second["test_log_sha256"] = first["test_log_sha256"]
        second_path.write_text(json.dumps(second, ensure_ascii=False, indent=2),
                               encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("reuses the test log", out)

        # A short round cannot borrow a long matrix combination's credential.
        bundle = self.write_bundle("impersonate-cross-scope")
        short_path = bundle / "short-1" / "evidence.json"
        long_path = bundle / "long-1-256" / "evidence.json"
        short = json.loads(short_path.read_text(encoding="utf-8"))
        long_evidence = json.loads(long_path.read_text(encoding="utf-8"))
        short["run_id"] = long_evidence["run_id"]
        short_path.write_text(json.dumps(short, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("reuses run_id", out)

    def test_mixed_baseline_digest_or_fingerprint_fails(self):
        bundle = self.write_bundle("mixed-digest")
        path = bundle / "short-3" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["contract_digest"] = "sha256:" + "9" * 64
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("mixes contract digests", out)

        bundle = self.write_bundle("mixed-fingerprint")
        path = bundle / "short-3" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["module_fingerprints"][0]["content_hash"] = "sha256:" + "9" * 64
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("different baseline content digest", out)

    def test_mutation_evidence_structure_is_enforced(self):
        bundle = self.write_bundle("mutations-survived", mutation=False)
        payload = mutation_evidence(self.digest, tree=self.tree)
        payload["mutations"][0]["status"] = "survived"
        payload["not_evidence_backed"] = ["candidate-01"]
        directory = bundle / "mutations"
        directory.mkdir()
        (directory / "evidence.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0)
        self.assertIn("is not 'killed'", out)
        self.assertIn("not_evidence_backed must be empty", out)

    def test_ten_rounds_all_shrunk_to_the_same_parent_are_rejected(self):
        # 负锚（等集）：十份 short 证据逐份合法、repeat_index 恰为 1..10、
        # run_id 与日志互不相同，但每轮都只跑同一个 parent。旧门只查
        # "各轮一致"，十轮一致缩成同一子集必须被具名拒收。
        bundle = self.write_bundle("shrunk-to-one-parent")
        parent = event_tests(self.contract, "event-short")[0]
        rows = event_log(self.contract, [parent])
        for index in range(1, 11):
            evidence = self.evidence("event-short", [parent], repeat_index=index,
                                     repeat_total=10)
            self.write_run(bundle / "short-{}".format(index), evidence, rows)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("full contract event-short parent set", out)
        self.assertIn("1 of 29 parent(s) covered", out)
        self.assertIn(os.path.join("short-1", "evidence.json"), out)
        # The missing parents are named, so the shrink is auditable.
        missing_parent = event_tests(self.contract, "event-short")[1]["id"]
        self.assertIn(missing_parent, out)

    def test_mutation_evidence_from_another_baseline_is_rejected(self):
        # 负锚（同基线）：mutation 报告结构合法、全 killed，但基线来自旧树，
        # 不得混入本基线 bundle 计数。
        bundle = self.write_bundle("mutation-old-contract")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["contract_digest"] = "sha256:" + "9" * 64
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("does not match the runner baseline", out)
        self.assertIn("another contract baseline", out)

        # 旧源码基线同样具名拒收：共享模块的 content_hash 漂移。
        bundle = self.write_bundle("mutation-old-source")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["module_fingerprints"][0]["content_hash"] = "sha256:" + "9" * 64
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("another source baseline", out)
        self.assertIn(MODULE, out)

        # 不申报任何基线字段的报告无法 tying，不得按"未知即同基线"放行。
        bundle = self.write_bundle("mutation-untied-baseline")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        del payload["contract_digest"]
        del payload["module_fingerprints"]
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("must declare contract_digest", out)
        self.assertIn("module_fingerprints must be a non-empty list", out)

    def test_a_mutation_report_naming_no_gate_is_rejected(self):
        # 负锚（门身份）：报告结构全绿、闭包是本树的，但没有申报写下判词的工具。
        bundle = self.write_bundle("mutation-no-gate")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload.pop("gate_tool")
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("must record gate_tool", out)

    def test_a_mutation_report_naming_a_gate_outside_the_tree_is_rejected(self):
        # 负锚（假路径）：entrypoint 在本树不存在，判词无法在此复算。
        bundle = self.write_bundle("mutation-gate-absent")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["gate_tool"]["entrypoint"] = "scripts/gates/absent_gate.py"
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("is not in the tree under", out)
        self.assertIn("scripts/gates/absent_gate.py", out)

    def test_a_mutation_report_from_another_gate_is_rejected(self):
        # 负锚（零 digest）：路径存在但字节不匹配，等于另一版门写下的判词。
        bundle = self.write_bundle("mutation-gate-drifted")
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["gate_tool"]["digest"] = "sha256:" + "0" * 64
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("were decided by a different", out)
        self.assertIn(GATE_ENTRYPOINT.as_posix(), out)

    def _bundle_with_mutation(self, name, **fields):
        """A legal bundle whose mutation report states `fields` on its first record."""
        bundle = self.write_bundle(name)
        path = bundle / "mutations" / "evidence.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["mutations"][0].update(fields)
        path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        return bundle

    def test_a_kill_whose_verdict_contradicts_its_status_is_rejected(self):
        # 负锚（判词一致）：status=killed 但 verdict=not_killed 的记录自相矛盾。
        bundle = self._bundle_with_mutation("mutation-verdict", verdict="not_killed")
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("disagrees with itself", out)

    def test_a_kill_the_producer_did_not_count_is_rejected(self):
        # 负锚（判词一致）：status=killed 但 counted_killed=false。
        bundle = self._bundle_with_mutation("mutation-uncounted", counted_killed=False)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("is not counted as killed", out)

    def test_a_kill_that_never_reached_its_assertion_is_rejected(self):
        # 负锚（判词一致）：status=killed 但 assertion_hit≠true。
        bundle = self._bundle_with_mutation("mutation-assertion-missed", assertion_hit=False)
        code, out, _ = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out)
        self.assertIn("did not hit its named assertion", out)


class EventTreeBindingTest(EventFixture, unittest.TestCase):
    """Evidence is read against a tree, never against its own agreement.

    A long-scope run selects `examples/wechat-bot`, which compiles the root module
    through `replace ... => ../..`; a bundle that agrees with itself can still
    describe a tree that has since moved. These are the cases that separate
    "the artifacts agree" from "the evidence is about this tree".
    """

    def test_current_tree_bundle_is_accepted(self):
        bundle = self.write_bundle("current-tree")
        code, out, err = self.check_bundle(bundle)
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["counts"]["bundle_evidence_files"], 17)
        # Acceptance is not vacuous: the evidence names the replaced root, which
        # is the module a long-scope run compiles without selecting.
        long_evidence = json.loads((bundle / "long-1-256" / "evidence.json").read_text(
            encoding="utf-8"))
        by_dir = {entry["dir"]: entry for entry in long_evidence["source_binding"]["modules"]}
        self.assertEqual(by_dir["."]["roles"], ["local-replacement"])
        self.assertEqual(long_evidence["source_binding"]["workspace"], {"state": "absent"})

    def test_consistent_stale_bundle_is_rejected(self):
        # The whole bundle is internally consistent: ten short rounds, six combo
        # rounds and the mutation report all name one closure. The tree then moves.
        # Agreement between artifacts cannot rescue them.
        bundle = self.write_bundle("stale-bundle")
        (self.tree / "main.go").write_text("package app // moved\n", encoding="utf-8")
        code, out, err = self.check_bundle(bundle)
        self.assertNotEqual(code, 0, out + err)
        self.assertIn("does not match this tree's", out)
        self.assertIn("describes another source tree", out)
        self.assertNotIn("different baseline content digest", out)

    def test_long_replacement_root_drift_is_rejected(self):
        # event-long selects only the smoke module. Before R2a/R2b the root could
        # change without any digest moving, because the root was never recorded.
        tests = event_tests(self.contract, "event-long")
        rows = event_log(self.contract, tests)
        evidence = self.evidence("event-long", tests, seed=1, window=256)
        code, out, err = self.verify(evidence, rows)
        self.assertEqual(code, 0, out + err)
        (self.tree / "main.go").write_text("package app // edited by another branch\n",
                                          encoding="utf-8")
        code, out, err = self.verify(evidence, rows)
        self.assertNotEqual(code, 0, out + err)
        self.assertIn("module '.'", out)
        self.assertIn("does not match this tree's", out)

    def test_ab_evidence_is_checked_against_its_own_tree(self):
        # Two trees, each with its own sources. Each set of evidence must pass
        # against the tree it was produced in and fail against the other, so a
        # legal A/B comparison is neither collapsed nor falsely rejected.
        tree_b = self.root / "tree-b"
        shutil.copytree(self.tree, tree_b)
        (tree_b / "main.go").write_text("package app // variant b\n", encoding="utf-8")
        (tree_b / "examples" / "wechat-bot" / "smoke_test.go").write_text(
            "package smoke // variant b\n", encoding="utf-8")
        tests = event_tests(self.contract, "event-long")
        rows = event_log(self.contract, tests)
        evidence_a = self.evidence("event-long", tests, seed=1, window=256)
        evidence_b = self.evidence("event-long", tests, seed=1, window=256, tree=tree_b)
        self.assertNotEqual(evidence_a["source_binding"]["digest"],
                            evidence_b["source_binding"]["digest"])
        for evidence, label in ((evidence_a, "a"), (evidence_b, "b")):
            path = self.write_run(self.root / "run-{}".format(label), evidence, rows)
            code, out, err = run_checker(
                "--contract", str(REAL_EVENT_CONTRACT),
                "--repo-root", str(self.tree if label == "a" else tree_b),
                "--evidence", str(path), "--json")
            self.assertEqual(code, 0, out + err)
        # Swapping the trees is not a matter of taste: A's evidence is not B's.
        path = self.write_run(self.root / "run-cross", evidence_a, rows)
        code, out, err = run_checker("--contract", str(REAL_EVENT_CONTRACT),
                                     "--repo-root", str(tree_b), "--evidence", str(path),
                                     "--json")
        self.assertNotEqual(code, 0, out + err)
        self.assertIn("describes another source tree", out)


if __name__ == "__main__":
    unittest.main()
