#!/usr/bin/env python3
"""Selftests for .github/workflows/runtime-contracts.yml — static text only.

Deliberately no PyYAML dependency: the workflow is validated with the standard
library alone, matching the "no pip install" rule of the contracts job itself.
These tests pin the security- and honesty-relevant invariants of the workflow
text that a YAML parse alone would not catch — branch coverage, no raw
`${{ ... }}` interpolation into a quoted shell argument, paired evidence inputs,
and the opt-in gate on the real-acceptance job.
"""
import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
WORKFLOW = REPO / ".github" / "workflows" / "runtime-contracts.yml"


class WorkflowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = WORKFLOW.read_text(encoding="utf-8")

    def test_workflow_file_exists(self):
        self.assertTrue(WORKFLOW.is_file(), "runtime-contracts.yml is missing")

    def test_push_covers_all_integration_branches(self):
        match = re.search(r"push:\s*\n\s*branches:\s*\[(?P<branches>[^\]]*)\]", self.text)
        self.assertIsNotNone(match, "push.branches list not found")
        branches = [branch.strip() for branch in match.group("branches").split(",")]
        for branch in ("main", "dev", "next"):
            self.assertIn(branch, branches)

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
        # The toolchain must be a floor >=1.24.1, never the old bare "1.24".
        self.assertIn('go-version: ">=1.24.1"', self.text)
        self.assertNotIn('go-version: "1.24"', self.text)

    def test_scenario_contract_cross_checks_task_graph(self):
        # The scenario contract must pass --plan-graph so task_ids are
        # cross-checked against the manifest, not silently accepted.
        step = self.text.split("Scenarios contract", 1)[1].split("- name:", 1)[0]
        self.assertIn("--check-contract", step)
        self.assertIn("--plan-graph openspec/changes/runtime-rebuild/task-graph.json", step)


if __name__ == "__main__":
    unittest.main()
