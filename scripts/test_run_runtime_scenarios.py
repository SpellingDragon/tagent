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
import io
import json
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest import mock

SCRIPTS = Path(__file__).resolve().parent
REPO = SCRIPTS.parent
RUNNER = SCRIPTS / "run_runtime_scenarios.py"
CHECKER = SCRIPTS / "check_runtime_evidence.py"
REAL_CONTRACT = REPO / "golden" / "runtime" / "scenarios.json"
REAL_EVENT_CONTRACT = REPO / "golden" / "runtime" / "event_integrity_scenarios.json"
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


# ---------------------------------------------------------------------------
# v2 event-integrity runner (04.2).  `FakeGo` synthesises `go test -json` rows in
# the nesting a real run produces; it NEVER invokes Go.  These tests cover
# selection, command construction, repetition and attestation only — product
# behaviour stays the business of a real run.
# ---------------------------------------------------------------------------

EVENT_UUID_RE = re.compile(
    r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")


def event_test(test_id, package, name, scope, tags=(), module=None):
    leaf = test_id.split("/", 1)[0]
    test = {
        "id": test_id, "leaf": leaf, "kind": "product-green",
        "module": "app", "module_path": "example.com/app", "dir": ".",
        "test_package": package, "test_name": name,
        "file": "tests/integration/alpha/alpha_test.go",
        "selector": "^{}$".format(name), "tags": list(tags), "race": True,
        "scope": scope,
        "subtests": [{"name": "positive", "mandatory": True},
                     {"name": "negative", "mandatory": True}],
    }
    test.update(module or {})
    return test


def fixture_event_contract():
    """Five tests over two modules, mixed build tags, two scopes, 2x2 matrix."""
    wechat = {"module": "wechat-bot", "module_path": "wechat-bot",
              "dir": "examples/wechat-bot"}
    tests = [
        event_test("01.1/TestAlpha", "./tests/integration/alpha", "TestAlpha", "event-short"),
        event_test("01.2/TestBeta", ".", "TestBeta", "event-short"),
        event_test("02.1/TestGamma", ".", "TestGamma", "event-short", tags=("smoke",),
                   module=wechat),
        event_test("02.2/TestDelta", ".", "TestDelta", "event-short", module=wechat),
        event_test("03.1/TestEpsilon", ".", "TestEpsilon", "event-long", tags=("smoke",),
                   module=wechat),
    ]
    return {
        "schema_version": 2,
        "profile": "event-integrity",
        "tests": tests,
        "scopes": [{"name": "event-short", "repeat": 2},
                   {"name": "event-long", "repeat": 1}],
        "matrix": {"seeds": [1, 7], "windows": [256, 32768], "combos": 4},
    }


def event_row(package, test, action):
    return {"Time": "2026-01-01T00:00:00Z", "Action": action,
            "Package": package, "Test": test, "Output": ""}


class FakeGo:
    """Synthetic `go test -json` producer; no Go toolchain is ever invoked."""

    def __init__(self, contract, mutate=None):
        self.tests = {test["test_name"]: test for test in contract["tests"]}
        self.mutate = mutate
        self.commands = []

    def __call__(self, command, root, timeout):
        position = len(self.commands)
        self.commands.append(list(command))
        module_dir = command[command.index("-C") + 1]
        module_path = next(test["module_path"] for test in self.tests.values()
                           if test["dir"] == module_dir)
        packages = []
        for argument in command[command.index("test") + 1:]:
            if argument.startswith("-"):
                break
            packages.append(argument)
        names = command[command.index("-run") + 1].strip("^()$").split("|")
        rows = []
        for package in packages:
            if package == ".":
                full = module_path
            elif package.startswith("./"):
                full = module_path + "/" + package[2:]
            else:
                full = module_path + "/" + package
            rows.append(event_row(full, "", "start"))
            for name in names:
                rows.append(event_row(full, name, "run"))
                for subtest in self.tests[name]["subtests"]:
                    rows.append(event_row(full, name + "/" + subtest["name"], "run"))
                    rows.append(event_row(full, name + "/" + subtest["name"], "pass"))
                rows.append(event_row(full, name, "pass"))
            rows.append(event_row(full, "", "pass"))
        exit_code = 0
        if self.mutate:
            rows, exit_code = self.mutate(position, rows)
        return exit_code, "".join(json.dumps(row) + "\n" for row in rows), ""


class EventRunnerTest(unittest.TestCase):
    """Fixture contract + fake go, in a temp repository root."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.contract = fixture_event_contract()
        self.contract_path = self.root / "event-contract.json"
        self.contract_path.write_text(json.dumps(self.contract), encoding="utf-8")
        self.write_module("", "example.com/app")
        self.write_module("examples/wechat-bot", "wechat-bot")
        (self.root / "main.go").write_text("package app\n", encoding="utf-8")
        alpha = self.root / "tests" / "integration" / "alpha"
        alpha.mkdir(parents=True)
        (alpha / "alpha_test.go").write_text("package alpha\n", encoding="utf-8")

    def tearDown(self):
        self._tmp.cleanup()

    def write_module(self, relative, module_path):
        base = self.root / relative if relative else self.root
        base.mkdir(parents=True, exist_ok=True)
        (base / "go.mod").write_text("module {}\n".format(module_path), encoding="utf-8")
        (base / "go.sum").write_text("", encoding="utf-8")
        if relative:
            (base / "smoke_test.go").write_text("package smoke\n", encoding="utf-8")

    def argv(self, *extra):
        return ["--root", str(self.root), "--contract", str(self.contract_path),
                "--out", str(self.root / "out")] + list(extra)

    def execute(self, fake, *extra):
        args = runner.parse_args(self.argv(*extra))
        with mock.patch.object(runner, "git_revision", lambda _root: "0" * 40):
            with mock.patch.object(runner, "run_go_test", fake):
                return runner.execute(args)

    def main(self, fake, *extra):
        argv = self.argv(*extra)
        output = io.StringIO()
        with mock.patch.object(runner, "git_revision", lambda _root: "0" * 40):
            with mock.patch.object(runner, "run_go_test", fake):
                with redirect_stdout(output), redirect_stderr(output):
                    code = runner.main(argv)
        return code, output.getvalue()

    @staticmethod
    def evidence_of(report, index=0):
        directory = Path(report["repeats"][index]["directory"])
        return json.loads((directory / "evidence.json").read_text(encoding="utf-8"))

    def test_v2_contract_runs_each_module_with_tags_and_race(self):
        fake = FakeGo(self.contract)
        report = self.execute(fake, "--scope", "event-short", "--repeat", "1")
        self.assertEqual(report["verdict"], "match")
        self.assertEqual(report["counts"]["tests_required"], 4)
        self.assertEqual(report["counts"]["repeats_passed"], 1)
        self.assertEqual({state["state"] for state in report["repeats"][0]["tests"]},
                         {"attested"})
        by_dir = {}
        for command in fake.commands:
            by_dir.setdefault(command[command.index("-C") + 1], []).append(command)
        self.assertEqual(sorted(by_dir), [".", "examples/wechat-bot"])
        self.assertEqual(by_dir["."], [[
            "go", "-C", ".", "test", ".", "./tests/integration/alpha",
            "-run", "^(TestAlpha|TestBeta)$", "-count=1", "-timeout", "900s", "-race", "-json"]])
        self.assertEqual(len(by_dir["examples/wechat-bot"]), 2)
        smoke = next(command for command in by_dir["examples/wechat-bot"]
                     if "-tags" in command)
        plain = next(command for command in by_dir["examples/wechat-bot"]
                     if "-tags" not in command)
        self.assertEqual(smoke, [
            "go", "-C", "examples/wechat-bot", "test", ".",
            "-run", "^(TestGamma)$", "-count=1", "-timeout", "900s", "-tags", "smoke", "-race", "-json"])
        self.assertEqual(plain, [
            "go", "-C", "examples/wechat-bot", "test", ".",
            "-run", "^(TestDelta)$", "-count=1", "-timeout", "900s", "-race", "-json"])
        for command in fake.commands:
            self.assertNotIn("-args", command)
            self.assertNotIn("-count=2", command)
        evidence = self.evidence_of(report)
        self.assertRegex(evidence["run_id"], EVENT_UUID_RE)
        self.assertEqual([entry["dir"] for entry in evidence["module_fingerprints"]],
                         [".", "examples/wechat-bot"])
        self.assertEqual(evidence["run_config"]["race"], True)
        self.assertEqual(evidence["run_config"]["tags"], ["smoke"])
        self.assertIsNone(evidence["run_config"]["seed"])
        self.assertEqual(len(evidence["run_config"]["invocations"]), 3)
        self.assertEqual(evidence["go_test_exit_code"], 0)
        self.assertEqual(evidence["verdict"], "match")
        self.assertEqual(len(evidence["subtests"]), 8)
        self.assertEqual({entry["status"] for entry in evidence["subtests"]}, {"pass"})

    def test_repeat_runs_are_independent_and_all_required(self):
        from_contract = self.execute(FakeGo(self.contract), "--scope", "event-short")
        self.assertEqual(from_contract["repeat_total"], 2)
        self.assertEqual(from_contract["counts"]["repeats_passed"], 2)

        clean = FakeGo(self.contract)
        report = self.execute(clean, "--scope", "event-short", "--repeat", "3")
        self.assertEqual(report["repeat_total"], 3)
        self.assertEqual(report["counts"]["repeats_passed"], 3)
        run_ids = [repeat["run_id"] for repeat in report["repeats"]]
        self.assertEqual(len(set(run_ids)), 3)
        for run_id in run_ids:
            self.assertRegex(run_id, EVENT_UUID_RE)
        logs = [repeat["test_log"] for repeat in report["repeats"]]
        self.assertEqual(len(set(logs)), 3)
        self.assertEqual([Path(repeat["directory"]).name for repeat in report["repeats"]],
                         ["repeat-1", "repeat-2", "repeat-3"])
        self.assertEqual(len(clean.commands), 9)
        for command in clean.commands:
            self.assertIn("-count=1", command)
        for index, repeat in enumerate(report["repeats"], start=1):
            evidence = self.evidence_of(report, index - 1)
            self.assertEqual(evidence["run_id"], repeat["run_id"])
            self.assertEqual(evidence["repeat_index"], index)
            self.assertEqual(evidence["repeat_total"], 3)
            # The evidence names its log beside itself, so the artifact still
            # resolves after CI copies it off the runner that produced it.
            self.assertFalse(Path(evidence["test_log"]).is_absolute())
            self.assertEqual(str(Path(repeat["directory"]) / evidence["test_log"]),
                             repeat["test_log"])
        # One broken repeat must not invalidate the other independent rounds.
        def drop_alpha_terminal(position, rows):
            if position != 6:
                return rows, 0
            kept = [row for row in rows
                    if not (row.get("Test") == "TestAlpha" and row.get("Action") == "pass")]
            return kept, 0
        broken = self.execute(FakeGo(self.contract, drop_alpha_terminal),
                              "--scope", "event-short", "--repeat", "3")
        self.assertEqual(broken["verdict"], "unattested")
        self.assertEqual(broken["go_test_exit_code"], 0)
        self.assertEqual([repeat["verdict"] for repeat in broken["repeats"]],
                         ["match", "match", "unattested"])
        self.assertEqual(broken["counts"]["repeats_passed"], 2)
        self.assertEqual((self.main(FakeGo(self.contract, drop_alpha_terminal),
                                    "--scope", "event-short", "--repeat", "3"))[0],
                         runner.EXIT_UNATTESTED)

    def test_six_combo_matrix_is_enumerated_once(self):
        contract = json.loads(REAL_EVENT_CONTRACT.read_text(encoding="utf-8"))
        combos = runner.event_matrix_combinations(contract)
        self.assertEqual(len(combos), 6)
        self.assertEqual(len(set(combos)), 6)
        self.assertEqual(combos, [(seed, window) for seed in (1, 7, 42)
                                  for window in (256, 32768)])
        for pair in combos:
            self.assertEqual(runner.event_matrix_pair(contract, *pair), pair)
        with self.assertRaises(runner.RunnerError):
            runner.event_matrix_pair(contract, 1, 512)
        with self.assertRaises(runner.RunnerError):
            runner.event_matrix_pair(contract, 1, None)

    def test_matrix_flags_are_appended_as_args(self):
        fake = FakeGo(self.contract)
        report = self.execute(fake, "--scope", "event-long",
                              "--seed", "7", "--window", "32768")
        self.assertEqual(report["repeat_total"], 1)
        self.assertEqual(len(fake.commands), 1)
        self.assertEqual(fake.commands[0][-4:],
                         ["-json", "-args", "-seed=7", "-window=32768"])
        self.assertEqual(fake.commands[0].count("-json"), 1)
        evidence = self.evidence_of(report)
        self.assertEqual(evidence["run_config"]["seed"], 7)
        self.assertEqual(evidence["run_config"]["window"], 32768)
        with self.assertRaises(runner.RunnerError):
            self.execute(FakeGo(self.contract), "--scope", "event-long", "--seed", "7")

    def test_fingerprint_covers_uncommitted_and_untracked_sources(self):
        def fingerprint(module_dir, module_path):
            with mock.patch.object(runner, "git_revision", lambda _root: "a" * 40):
                return runner.module_fingerprint(str(self.root), module_dir, module_path)
        first = fingerprint(".", "example.com/app")
        self.assertEqual(first["dir"], ".")
        self.assertRegex(first["revision"], r"^[0-9a-f]{40}$")
        self.assertRegex(first["content_hash"], r"^sha256:[0-9a-f]{64}$")
        (self.root / "tests" / "integration" / "alpha" / "late_test.go").write_text(
            "package alpha\n", encoding="utf-8")
        second = fingerprint(".", "example.com/app")
        self.assertNotEqual(first["content_hash"], second["content_hash"])
        self.assertEqual(first["revision"], second["revision"])
        (self.root / "go.sum").write_text("example.com/dep v1.0.0 h1:abc\n", encoding="utf-8")
        third = fingerprint(".", "example.com/app")
        self.assertNotEqual(second["content_hash"], third["content_hash"])
        # A nested module is fingerprinted on its own, never twice from the root.
        (self.root / "examples" / "wechat-bot" / "extra_test.go").write_text(
            "package smoke\n", encoding="utf-8")
        self.assertEqual(fingerprint(".", "example.com/app")["content_hash"],
                         third["content_hash"])
        self.assertNotEqual(fingerprint("examples/wechat-bot", "wechat-bot")["content_hash"],
                            third["content_hash"])

    def test_zero_match_with_exit_zero_is_rejected(self):
        def zero_match(position, rows):
            return [row for row in rows if not row.get("Test")], 0
        fake = FakeGo(self.contract, zero_match)
        report = self.execute(fake, "--scope", "event-short")
        self.assertEqual(report["go_test_exit_code"], 0)
        self.assertEqual(report["verdict"], "unattested")
        self.assertEqual(report["counts"]["repeats_passed"], 0)
        states = [state for repeat in report["repeats"] for state in repeat["tests"]]
        self.assertEqual({state["state"] for state in states}, {"unattested"})
        for state in states:
            self.assertIn("zero match", state["reason"])
        code, _ = self.main(FakeGo(self.contract, zero_match), "--scope", "event-short")
        self.assertEqual(code, runner.EXIT_UNATTESTED)

    def test_nonzero_process_exit_is_unattested(self):
        def exit_three(position, rows):
            return rows, 3
        report = self.execute(FakeGo(self.contract, exit_three), "--scope", "event-short")
        self.assertEqual(report["go_test_exit_code"], 3)
        self.assertEqual(report["verdict"], "unattested")
        self.assertEqual({state["state"] for state in report["repeats"][0]["tests"]},
                         {"attested"})
        code, _ = self.main(FakeGo(self.contract, exit_three), "--scope", "event-short")
        self.assertEqual(code, runner.EXIT_UNATTESTED)

    def test_unknown_test_id_is_rejected(self):
        fake = FakeGo(self.contract)
        with self.assertRaises(runner.RunnerError) as caught:
            self.execute(fake, "--scope", "event-short", "--test-id", "99.9/TestNope")
        self.assertIn("not registered", str(caught.exception))
        self.assertEqual(fake.commands, [])
        with self.assertRaises(runner.RunnerError) as caught:
            self.execute(FakeGo(self.contract), "--scope", "event-short",
                         "--test-id", "03.1/TestEpsilon")
        self.assertIn("outside scope", str(caught.exception))
        code, output = self.main(FakeGo(self.contract), "--scope", "event-short",
                                 "--test-id", "99.9/TestNope")
        self.assertEqual(code, runner.EXIT_USAGE)
        self.assertIn("not registered", output)

    def test_v2_rejects_the_cross_scope_complete_alias(self):
        with self.assertRaises(runner.RunnerError) as caught:
            self.execute(FakeGo(self.contract), "--scope", "complete", "--repeat", "1")
        self.assertIn("schema v2 selects one contract scope", str(caught.exception))

    def test_runner_evidence_is_accepted_by_the_v2_checker(self):
        report = self.execute(FakeGo(self.contract), "--scope", "event-short")
        repeat = report["repeats"][0]
        code, stdout, stderr = run_tool(
            CHECKER, "--contract", str(self.contract_path), "--evidence",
            str(Path(repeat["directory"]) / "evidence.json"), "--test-log",
            str(repeat["test_log"]), "--json")
        self.assertEqual(code, 0, stdout + stderr)
        payload = json.loads(stdout)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["counts"]["event_tests_checked"], 4)
        self.assertEqual(payload["counts"]["event_anchors_checked"], 8)
        self.assertIsNone(payload["productVerified"])

    def test_v1_command_shape_is_unchanged(self):
        self.assertEqual(
            runner.go_test_command(["tests/integration/acceptance"],
                                   ["TestB", "TestA", "TestA"]),
            ["go", "test", "./tests/integration/acceptance",
             "-run", "^(TestA|TestB)$", "-count=1", "-json"])
        args = runner.parse_args(["--scope", "deterministic"])
        self.assertIsNone(args.repeat)
        self.assertIsNone(args.seed)
        self.assertIsNone(args.window)
        self.assertEqual(args.test_id, [])
        fake = mock.Mock(return_value=(0, "", ""))
        v1_args = runner.parse_args(["--root", str(REPO), "--contract", str(REAL_CONTRACT),
                                     "--out", str(self.root / "v1"), "--scope", "deterministic"])
        with mock.patch.object(runner, "run_go_test", fake):
            with mock.patch.object(runner, "source_fingerprint",
                                   lambda _root: {"module_path": MODULE,
                                                  "revision": "0" * 40,
                                                  "content_hash": "sha256:" + "0" * 64}):
                report = runner.execute(v1_args)
        self.assertEqual(fake.call_count, 1)
        command = fake.call_args[0][0]
        self.assertEqual(command[:2], ["go", "test"])
        self.assertNotIn("-C", command)
        self.assertNotIn("-tags", command)
        self.assertNotIn("-args", command)
        self.assertEqual(command[-2:], ["-count=1", "-json"])
        self.assertTrue(command[command.index("-run") + 1].startswith("^("))
        self.assertEqual(report["verdict"], "unattested")
        with self.assertRaises(runner.RunnerError):
            runner.execute(runner.parse_args(["--root", str(REPO),
                                              "--contract", str(REAL_CONTRACT),
                                              "--repeat", "2"]))


if __name__ == "__main__":
    unittest.main()
