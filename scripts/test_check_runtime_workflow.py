#!/usr/bin/env python3
"""Selftests for .github/workflows/runtime-contracts.yml.

The workflow is validated by parsing it (PyYAML — the only non-stdlib import in
the checker selftest suite, needed to parse YAML at all) and by pinning every
gate step against its expected command keywords, so a renamed or hollowed-out
gate turns this suite red before CI drifts. The honesty- and
security-relevant invariants a YAML parse alone would not catch stay pinned as
text: branch coverage, no raw `${{ ... }}` interpolation into a quoted shell
argument, paired evidence inputs, and the opt-in gate on the real-acceptance
job.

Negative controls mutate a parsed COPY of the workflow (never the file itself):
removing any one gate step in turn must turn the gate-presence check red.
"""
import copy
import json
import re
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[1]
WORKFLOW = REPO / ".github" / "workflows" / "runtime-contracts.yml"
GOLDEN_ARCHITECTURE = REPO / "golden" / "runtime" / "architecture.json"

INTEGRATION_BRANCHES = ("main", "dev", "next")

# Every gate step the contracts job must carry, with the command keywords that
# make the gate real. A step that disappears or loses its command fails here.
GATE_STEPS = [
    ("Checker selftests",
     ["unittest discover -s scripts", "test_check_runtime_*.py"]),
    ("Plan contract",
     ["check_runtime_plan.py", "--json"]),
    ("Architecture policy",
     ["check_runtime_architecture.py --check-policy", "--json"]),
    ("Scenarios contract",
     ["check_runtime_evidence.py --check-contract",
      "--plan-graph openspec/changes/runtime-rebuild/task-graph.json"]),
    ("Build (root module)",
     ["go build ./..."]),
    ("Vet (root module)",
     ["go vet ./..."]),
    ("Test (root module, short)",
     ["go test ./... -short", "-count=1"]),
    ("gofmt (product packages)",
     ["gofmt -l $PRODUCT_PACKAGES", "exit 1"]),
    ("Comment policy (product packages, measurement mode)",
     ["comment_policy -no-baseline $PRODUCT_PACKAGES", '"$count" != "0"']),
    ("Tracked hygiene",
     ["codetools tracked-hygiene"]),
    ("OpenSpec strict (runtime-rebuild)",
     ["openspec validate runtime-rebuild --strict", "--no-interactive"]),
]


def on_block(workflow):
    # PyYAML 1.1 reads the bare key `on` as boolean True; accept both spellings.
    return workflow.get("on", workflow.get(True))


def step_body(step):
    return "{}\n{}".format(step.get("name", ""), step.get("run", ""))


def assert_gate_steps_present(workflow):
    bodies = [step_body(step)
              for job in (workflow.get("jobs") or {}).values()
              for step in (job.get("steps") or [])]
    missing = []
    for name, keywords in GATE_STEPS:
        body = next((b for b in bodies if name in b), "")
        if not body:
            missing.append(name)
            continue
        for keyword in keywords:
            if keyword not in body:
                missing.append("{} (missing {!r})".format(name, keyword))
    if missing:
        raise AssertionError(
            "workflow is missing gate steps/commands: {}".format(missing))


def assert_push_covers_integration_branches(workflow):
    branches = ((on_block(workflow) or {}).get("push") or {}).get("branches") or []
    branches = [str(branch).strip() for branch in branches]
    missing = [branch for branch in INTEGRATION_BRANCHES if branch not in branches]
    if missing:
        raise AssertionError("push.branches is missing {}".format(missing))


class WorkflowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = WORKFLOW.read_text(encoding="utf-8")
        cls.workflow = yaml.safe_load(cls.text)

    def test_workflow_file_exists(self):
        self.assertTrue(WORKFLOW.is_file(), "runtime-contracts.yml is missing")

    def test_yaml_parses(self):
        self.assertIsInstance(self.workflow, dict)
        self.assertIn("jobs", self.workflow)
        contracts = self.workflow["jobs"].get("contracts")
        self.assertIsInstance(contracts, dict)
        steps = contracts.get("steps")
        self.assertIsInstance(steps, list)
        for step in steps:
            self.assertIsInstance(step, dict)
            self.assertTrue(step.get("run") or step.get("uses"),
                            "every step needs a run or a uses: {!r}".format(step))

    def test_gate_steps_present_with_expected_commands(self):
        assert_gate_steps_present(self.workflow)

    def test_negative_control_removing_any_gate_step_turns_the_check_red(self):
        # The negative control proves the gate check bites: a YAML copy with one
        # gate step deleted must fail it (the real file is never touched).
        for name, _ in GATE_STEPS:
            mutated = copy.deepcopy(self.workflow)
            steps = mutated["jobs"]["contracts"]["steps"]
            kept = [step for step in steps if step.get("name") != name]
            self.assertEqual(len(kept), len(steps) - 1,
                             "gate step {!r} must exist exactly once".format(name))
            mutated["jobs"]["contracts"]["steps"] = kept
            with self.assertRaises(AssertionError,
                                   msg="removing {!r} must fail the gate check".format(name)):
                assert_gate_steps_present(mutated)

    def test_push_covers_all_integration_branches(self):
        assert_push_covers_integration_branches(self.workflow)

    def test_negative_control_dropping_next_branch_turns_the_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        push = on_block(mutated)["push"]
        push["branches"] = [branch for branch in push["branches"] if branch != "next"]
        with self.assertRaises(AssertionError):
            assert_push_covers_integration_branches(mutated)

    def test_product_scope_matches_golden_architecture(self):
        # PRODUCT_PACKAGES must cover exactly the first-party product packages
        # registered in golden/runtime/architecture.json (its scope excludes
        # scripts/tests/prototype/examples/docs/resources). The root package is
        # `*.go` because `gofmt -l .` would recurse the whole root module. A new
        # golden top-level package must land here and in the workflow together.
        scope = self.workflow["jobs"]["contracts"]["env"]["PRODUCT_PACKAGES"].split()
        self.assertIn("*.go", scope, "the root package must be covered by *.go")
        golden = json.loads(GOLDEN_ARCHITECTURE.read_text(encoding="utf-8"))
        expected = set()
        for package in golden["packages"]:
            pattern = package["pattern"]
            expected.add("*.go" if pattern == "." else pattern.split("/")[0])
        self.assertEqual(set(scope), expected,
                         "PRODUCT_PACKAGES must equal the golden product packages")

    def test_no_expression_interpolated_into_quoted_shell_arg(self):
        # '${{ ... }}' inside single quotes lets a crafted input break out of the
        # shell quoting; inputs must reach the command via env instead.
        self.assertNotRegex(self.text, r"'\$\{\{[^}]*\}\}'")

    def test_evidence_inputs_are_passed_through_env(self):
        self.assertIn("EVIDENCE_JSON: ${{ inputs.evidence_json }}", self.text)
        self.assertIn("TEST_LOG: ${{ inputs.test_log }}", self.text)
        self.assertIn('--evidence "$EVIDENCE_JSON"', self.text)
        self.assertIn('--test-log "$TEST_LOG"', self.text)

    def test_partial_evidence_input_is_rejected(self):
        self.assertRegex(
            self.text,
            r"\(inputs\.evidence_json != ''\) != \(inputs\.test_log != ''\)")
        match = re.search(r"- name: Reject partial[^\n]*\n(?:.*\n)*?(?=- name:|\Z)", self.text)
        self.assertIsNotNone(match, "partial-input rejection step not found")
        self.assertIn("exit 1", match.group(0))

    def test_real_acceptance_is_dispatch_only_and_opt_in(self):
        header = self.text.split("real-acceptance:", 1)[1].split("steps:", 1)[0]
        self.assertIn("workflow_dispatch", header)
        self.assertIn("inputs.run_implementation == 'true'", header)
        self.assertIn("inputs.evidence_json != ''", header)

    def test_no_continue_on_error(self):
        # The phrase appears in prose comments; only an actual YAML key matters.
        for line in self.text.splitlines():
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            self.assertFalse(stripped.startswith("continue-on-error:"),
                             "continue-on-error key must never be set")

    def test_contract_job_runs_all_four_gates(self):
        for needle in (
            "python -m unittest discover -s scripts -p 'test_check_runtime_*.py'",
            "check_runtime_plan.py",
            "check_runtime_architecture.py --check-policy",
            "check_runtime_evidence.py --check-contract",
        ):
            self.assertIn(needle, self.text)

    def test_go_version_is_floored_at_least_1_24_1(self):
        # The toolchain must be a floor >=1.24.1, never the old bare "1.24",
        # and every setup-go block must carry the same floor.
        self.assertIn('go-version: ">=1.24.1"', self.text)
        self.assertNotIn('go-version: "1.24"', self.text)
        versions = re.findall(r'go-version:\s*"([^"]+)"', self.text)
        self.assertTrue(versions, "no go-version pin found")
        for version in versions:
            self.assertEqual(version, ">=1.24.1",
                             "every setup-go must use the >=1.24.1 floor")

    def test_scenario_contract_cross_checks_task_graph(self):
        # The scenario contract must pass --plan-graph so task_ids are
        # cross-checked against the manifest, not silently accepted.
        step = self.text.split("Scenarios contract", 1)[1].split("- name:", 1)[0]
        self.assertIn("--check-contract", step)
        self.assertIn("--plan-graph openspec/changes/runtime-rebuild/task-graph.json", step)


if __name__ == "__main__":
    unittest.main()
