#!/usr/bin/env python3
"""Selftests for the CI workflows: runtime-contracts.yml and ci.yml.

The workflows are validated by parsing them (PyYAML — the only non-stdlib import
in the checker selftest suite, needed to parse YAML at all) and by pinning every
gate step against its expected command keywords, so a renamed or hollowed-out
gate turns this suite red before CI drifts. The honesty- and
security-relevant invariants a YAML parse alone would not catch stay pinned as
text: branch coverage, no raw `${{ ... }}` interpolation into a quoted shell
argument, paired evidence inputs, and the opt-in gate on the real-acceptance
job.

ci.yml carries the event-integrity producer jobs (short ten independent rounds,
the six-shard long matrix, the fault mutations), the required summary, the
symptom-anchor job, the race package set and the manual soak command. The same
rule applies there: the producer jobs must run on every pull request with no
skip hatch, the summary must download every producer artifact and hand it to the
one bundle checker, the symptom anchors -- which cannot enter the v2 contract
without breaking its cross-check against the archived task graph -- must run
under their own unconditional job, and the preflight script must call the same
entry points by default.

Negative controls mutate a parsed COPY of the workflow, or a textual copy of the
preflight script (never the files themselves): removing a gate step or an event
job, adding a path filter, adding continue-on-error, replacing ten independent
short rounds with one count-amplified run, making the long shards an opt-in
preflight flag, shrinking the race set to host/history, dropping a need,
removing always(), losing a producer artifact, a matrix combination or a short
round, deleting the symptom job, narrowing its selector, or giving it a skip
hatch must each turn the check red. The cancelled/skipped/empty upstream
results and the mixed-baseline bundle are executed for real, not asserted as
text: the results step runs under bash with each upstream value, and the
checker itself runs over a bundle whose evidence files disagree on the baseline.
"""
import copy
import fnmatch
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
import uuid
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[2]
WORKFLOW = REPO / ".github" / "workflows" / "runtime-contracts.yml"
GOLDEN_ARCHITECTURE = REPO / "golden" / "runtime" / "architecture.json"

INTEGRATION_BRANCHES = ("main", "dev", "next")

# Every gate step the contracts job must carry, with the command keywords that
# make the gate real. A step that disappears or loses its command fails here.
GATE_STEPS = [
    ("Checker selftests",
     ["unittest discover -s scripts/gates", "test_check_runtime_*.py"]),
    ("Plan contract",
     ["check_runtime_plan.py", "--json"]),
    ("Architecture policy",
     ["check_runtime_architecture.py --check-policy", "--json"]),
    ("Scenarios contract",
     ["check_runtime_evidence.py --check-contract",
      "--plan-graph openspec/changes/archive/2026-10-18-runtime-rebuild/task-graph.json"]),
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
    ("OpenSpec strict (main specs)",
     ["openspec validate --specs --strict", "--no-interactive"]),
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
        # scripts/tests/prototype/examples/docs/resources). Every registered package
        # is spelled by its own directory, the composition root included; a bare `.`
        # would recurse the whole root module, so the root names its directory too.
        # A new golden top-level package must land here and in the workflow together.
        scope = self.workflow["jobs"]["contracts"]["env"]["PRODUCT_PACKAGES"].split()
        golden = json.loads(GOLDEN_ARCHITECTURE.read_text(encoding="utf-8"))
        root = golden["root_package"]
        self.assertIn(root, scope, "the composition root must be covered by its directory")
        expected = set()
        for package in golden["packages"]:
            pattern = package["pattern"]
            expected.add(pattern if pattern == root else pattern.split("/")[0])
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
            "python -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'",
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
        self.assertIn("--plan-graph openspec/changes/archive/2026-10-18-runtime-rebuild/task-graph.json", step)


CI_WORKFLOW = REPO / ".github" / "workflows" / "ci.yml"
PREFLIGHT = REPO / "scripts" / "gates" / "preflight.sh"

EVENT_CONTRACT = "golden/runtime/event_integrity_scenarios.json"
EVENT_PRODUCER_JOBS = ("event-integrity-short",
                       "event-integrity-long",
                       "event-integrity-mutations")
EVENT_JOBS = EVENT_PRODUCER_JOBS + ("event-integrity-required",)
LEGACY_JOBS = ("test", "race", "validators", "openspec", "soak")

# The symptom anchors (event-semantics-boundary 04.3) sit in the wechat-bot
# module behind the smoke build tag and stay outside the v2 event contract: they
# are absent from the archived task graph's test registry, which the contract
# cross-checks bidirectionally, so registering them would fail the contract
# itself. Their gate is this dedicated job instead.
SYMPTOM_JOB = "event-semantics-symptoms"
SYMPTOM_ARTIFACT = "event-semantics-symptoms"
SYMPTOM_EVIDENCE_DIR = "${{ runner.temp }}/symptom"
SYMPTOM_PREFIX = "TestLocalSmokeSymptom"
SYMPTOM_MODULE = "go -C examples/wechat-bot test"
SMOKE_TEST_DIR = REPO / "examples" / "wechat-bot"
# Pinned by name: a rename that drops one of these would leave the job green
# while a user-voice promise lost its witness.
SYMPTOM_PARENTS = ("TestLocalSmokeSymptomAcceptedSilent",
                   "TestLocalSmokeSymptomSettledOnce",
                   "TestLocalSmokeSymptomFailureNotice",
                   "TestLocalSmokeSymptomExecStateSilent",
                   "TestLocalSmokeSymptomNoSendTool",
                   "TestLocalSmokeSymptomDeliveryHold")

# The race set is mapped from every modified product package and every formal
# integration suite. host/history alone is the shrunken set the negative control
# puts back to prove this check bites.
RACE_PACKAGES = (
    ".",
    "./memory/...",
    "./runtime/agent/...",
    "./runtime/task/...",
    "./runtime/state/...",
    "./features/...",
    "./api/event/...",
    "./tool/...",
    "./api/plugin/...",
    "./tests/integration/execution/...",
    "./tests/integration/history/...",
    "./tests/integration/host/...",
    "./tests/integration/capabilities/...",
    "./tests/integration/evolution/...",
    "./tests/integration/learning/...",
)

# Each preflight event step and the keywords that make it the same entry point
# CI runs. A step that disappears or loses its command fails here.
PREFLIGHT_EVENT_STEPS = (
    ("event integrity short",
     ["--contract " + EVENT_CONTRACT, "--scope event-short", "--repeat 10"]),
    ("event integrity long",
     ["--scope event-long", '--seed "$seed"', '--window "$window"',
      "--repeat 1", "--timeout 1200"]),
    ("event integrity mutations",
     ["check_event_integrity_mutations.py", "--all", "--json"]),
)


def ci_jobs(workflow):
    jobs = workflow.get("jobs") or {}
    if not isinstance(jobs, dict):
        raise AssertionError("ci.yml has no jobs mapping")
    return jobs


def job_steps(job):
    steps = job.get("steps") or []
    if not isinstance(steps, list):
        raise AssertionError("job steps must be a list")
    return steps


def step_with(job, needle):
    hits = [step for step in job_steps(job) if needle in (step.get("run") or "")]
    if len(hits) != 1:
        raise AssertionError(
            "expected exactly one step running {!r}, found {}".format(needle, len(hits)))
    return hits[0]


def run_with(job, needle):
    return step_with(job, needle).get("run") or ""


def assert_no_count_amplification(job_name, run):
    if re.search(r"-count[= ]", run):
        raise AssertionError(
            "{} must not amplify a single run with -count; ten rounds are ten "
            "independent runner processes".format(job_name))


def assert_single_artifact_upload(job_name, job, artifact, path):
    uploads = [step for step in job_steps(job)
               if str(step.get("uses", "")).startswith("actions/upload-artifact")]
    if len(uploads) != 1:
        raise AssertionError("{} must upload exactly one artifact, found {}".format(
            job_name, len(uploads)))
    upload = uploads[0]
    if upload.get("if") != "always()":
        raise AssertionError("{} artifact must upload with if: always()".format(job_name))
    with_block = upload.get("with") or {}
    if with_block.get("name") != artifact:
        raise AssertionError("{} artifact name must be {!r}, found {!r}".format(
            job_name, artifact, with_block.get("name")))
    if with_block.get("path") != path:
        raise AssertionError("{} artifact path must be {!r}, found {!r}".format(
            job_name, path, with_block.get("path")))


def assert_event_producer_jobs(workflow):
    jobs = ci_jobs(workflow)
    missing = [name for name in EVENT_JOBS if name not in jobs]
    if missing:
        raise AssertionError("ci.yml is missing event jobs: {}".format(missing))
    removed = [name for name in LEGACY_JOBS if name not in jobs]
    if removed:
        raise AssertionError(
            "ci.yml dropped pre-existing jobs; the event summary is not a "
            "replacement for them: {}".format(removed))

    short_job = jobs["event-integrity-short"]
    short_run = run_with(short_job, "run_runtime_scenarios.py")
    for needle in ("--contract " + EVENT_CONTRACT, "--scope event-short",
                   "--repeat 10", '--out "$RUNNER_TEMP/event-short"'):
        if needle not in short_run:
            raise AssertionError(
                "event-integrity-short command is missing {!r}: {!r}".format(needle, short_run))
    if "--test-id" in short_run:
        raise AssertionError(
            "event-integrity-short must run the complete scope, not a --test-id subset")
    assert_no_count_amplification("event-integrity-short", short_run)
    if short_job.get("timeout-minutes") != 45:
        raise AssertionError(
            "event-integrity-short timeout-minutes must be the measured budget "
            "(one round 2m58s x 10 x 1.5 headroom = 45; the 03.10 20m candidate "
            "would kill the ten-round job)")
    assert_single_artifact_upload("event-integrity-short", short_job,
                                  "event-integrity-short",
                                  "${{ runner.temp }}/event-short")

    long_job = jobs["event-integrity-long"]
    long_step = step_with(long_job, "--scope event-long")
    long_run = long_step.get("run") or ""
    # --timeout bounds ONE go test process, cold compile included: the
    # 256-window shard measured 3086s end to end in a local -race run (seed 1),
    # because the 256-token window makes every request assemble its history
    # under the smallest budget the matrix names. 5400s bounds that shard at
    # 1.75x and must fit inside the 120m job cap; the 32768-window shards still
    # measure 787-1111s (run 41216521132).
    for needle in ('--seed "$SEED"', '--window "$WINDOW"', "--repeat 1",
                   "--timeout 5400",
                   '--out "$RUNNER_TEMP/event-long/seed-$SEED-window-$WINDOW"'):
        if needle not in long_run:
            raise AssertionError(
                "event-integrity-long command is missing {!r}: {!r}".format(needle, long_run))
    if "--test-id" in long_run:
        raise AssertionError(
            "event-integrity-long must run the complete scope, not a --test-id subset")
    assert_no_count_amplification("event-integrity-long", long_run)
    long_env = long_step.get("env") or {}
    if long_env.get("SEED") != "${{ matrix.seed }}" or long_env.get("WINDOW") != "${{ matrix.window }}":
        raise AssertionError("matrix values must reach the long command via env")
    if long_job.get("timeout-minutes") != 120:
        raise AssertionError(
            "event-integrity-long timeout-minutes must be the measured budget "
            "(256-window shard 3086s local, 5400s --timeout, 120m job cap)")
    assert_single_artifact_upload(
        "event-integrity-long", long_job,
        "event-integrity-long-seed-${{ matrix.seed }}-window-${{ matrix.window }}",
        "${{ runner.temp }}/event-long/seed-${{ matrix.seed }}-window-${{ matrix.window }}")

    mutation_job = jobs["event-integrity-mutations"]
    mutation_run = run_with(mutation_job, "check_event_integrity_mutations.py")
    for needle in ("--all", "--json", '--out "$RUNNER_TEMP/event-integrity-mutations"'):
        if needle not in mutation_run:
            raise AssertionError(
                "event-integrity-mutations command is missing {!r}: {!r}".format(
                    needle, mutation_run))
    if mutation_job.get("timeout-minutes") != 30:
        raise AssertionError(
            "event-integrity-mutations timeout-minutes must stay at the 03.10 "
            "candidate (30, 待校准 until the mutation scope is measured)")
    assert_single_artifact_upload("event-integrity-mutations", mutation_job,
                                  "event-integrity-mutations",
                                  "${{ runner.temp }}/event-integrity-mutations")


def assert_long_matrix(workflow):
    long_job = ci_jobs(workflow)["event-integrity-long"]
    strategy = long_job.get("strategy") or {}
    if strategy.get("fail-fast") is not False:
        raise AssertionError("event-integrity-long must keep fail-fast: false")
    matrix = strategy.get("matrix") or {}
    if matrix.get("seed") != [1, 7, 42]:
        raise AssertionError(
            "long matrix seeds must be exactly [1, 7, 42], found {!r}".format(matrix.get("seed")))
    if matrix.get("window") != [256, 32768]:
        raise AssertionError(
            "long matrix windows must be exactly [256, 32768], found {!r}".format(matrix.get("window")))
    combos = {(seed, window) for seed in matrix["seed"] for window in matrix["window"]}
    if len(combos) != 6:
        raise AssertionError("the long matrix must enumerate six shards, found {}".format(len(combos)))


def assert_event_jobs_have_no_skip_hatch(workflow, text):
    jobs = ci_jobs(workflow)
    on = on_block(workflow) or {}
    pull_request = on.get("pull_request")
    if isinstance(pull_request, dict):
        for key in ("paths", "paths-ignore"):
            if key in pull_request:
                raise AssertionError(
                    "pull_request must not be narrowed by {}: an event job that can "
                    "be skipped is a silent hole".format(key))
    push = on.get("push") or {}
    if isinstance(push, dict):
        for key in ("paths", "paths-ignore"):
            if key in push:
                raise AssertionError("push must not be narrowed by {}".format(key))
        branches = [str(branch) for branch in push.get("branches") or []]
        for branch in ("main", "dev"):
            if branch not in branches:
                raise AssertionError("push.branches is missing {!r}".format(branch))
    for name in EVENT_PRODUCER_JOBS:
        job = jobs[name]
        if job.get("if"):
            raise AssertionError("{} must not carry a job-level if: {!r}".format(
                name, job.get("if")))
        if job.get("continue-on-error") not in (None, False):
            raise AssertionError("{} must not set continue-on-error".format(name))
        for step in job_steps(job):
            if step.get("continue-on-error") not in (None, False):
                raise AssertionError("{} step {!r} must not set continue-on-error".format(
                    name, step.get("name")))
        if "secrets." in json.dumps(job, ensure_ascii=False):
            raise AssertionError(
                "{} must not gate on credentials: no secrets in the event jobs".format(name))
    for line in text.splitlines():
        code = line.split("#", 1)[0].strip()
        if code.startswith("continue-on-error:"):
            raise AssertionError("ci.yml must never set continue-on-error")
    for needle in ("paths:", "paths-ignore"):
        if needle in text:
            raise AssertionError(
                "ci.yml must not carry a path filter ({!r}): the event jobs run on "
                "every pull request".format(needle))


def event_upload_steps(job):
    return [step for step in job_steps(job)
            if str(step.get("uses", "")).startswith("actions/upload-artifact")]


def assert_required_always_needs_all_three(workflow):
    required = ci_jobs(workflow).get("event-integrity-required")
    if required is None:
        raise AssertionError("ci.yml is missing the event-integrity-required job")
    if required.get("if") != "always()":
        raise AssertionError(
            "event-integrity-required must run with if: always(): with success() a "
            "failed upstream would skip the summary, and GitHub renders a skipped "
            "required check as green")
    needs = required.get("needs") or []
    if isinstance(needs, str):
        needs = [needs]
    if sorted(needs) != sorted(EVENT_PRODUCER_JOBS):
        raise AssertionError(
            "event-integrity-required needs must be exactly the three producers, "
            "found {!r}".format(needs))


def assert_required_download_covers_producer_artifacts(workflow):
    """Every producer artifact must land inside the summary's download glob."""
    required = ci_jobs(workflow)["event-integrity-required"]
    downloads = [step for step in job_steps(required)
                 if str(step.get("uses", "")).startswith("actions/download-artifact")]
    if len(downloads) != 1:
        raise AssertionError("event-integrity-required must download the event artifacts")
    pattern = (downloads[0].get("with") or {}).get("pattern")
    if pattern != "event-integrity-*":
        raise AssertionError("required must download by the fixed artifact pattern")
    names = []
    for job_name in EVENT_PRODUCER_JOBS:
        for step in event_upload_steps(ci_jobs(workflow)[job_name]):
            artifact = str((step.get("with") or {}).get("name", ""))
            if not fnmatch.fnmatchcase(artifact, pattern):
                raise AssertionError(
                    "{} artifact {!r} does not match the required download pattern "
                    "{!r}: the summary would silently miss it".format(
                        job_name, artifact, pattern))
            if artifact in names:
                raise AssertionError(
                    "two producers upload the same artifact {!r}".format(artifact))
            names.append(artifact)
    if not names:
        raise AssertionError("the event producers upload no evidence artifact at all")
    return downloads[0]


def assert_required_skeleton(workflow):
    """The summary job: always(), three producers, one bundle engine, one report."""
    required = ci_jobs(workflow)["event-integrity-required"]
    assert_required_always_needs_all_three(workflow)
    download = assert_required_download_covers_producer_artifacts(workflow)
    if download.get("if") != "always()":
        raise AssertionError(
            "the evidence download must run with if: always(): the summary has to "
            "report which artifact is missing when an upstream job failed")
    download_with = download.get("with") or {}
    if download_with.get("path") != "${{ runner.temp }}/bundle":
        raise AssertionError("required must download into runner.temp/bundle")
    if download_with.get("merge-multiple") is not False:
        raise AssertionError(
            "required must keep one directory per artifact (merge-multiple: false): "
            "a merge overwrites the same-named evidence.json across shards")

    checkers = [step for step in job_steps(required)
                if "check_runtime_evidence.py" in (step.get("run") or "")]
    if len(checkers) != 1:
        raise AssertionError(
            "event-integrity-required must run exactly one evidence checker, found "
            "{}".format(len(checkers)))
    checker = checkers[0]
    if checker.get("if") != "always()":
        raise AssertionError("the bundle check must run with if: always()")
    bundle_run = checker.get("run") or ""
    for needle in ('--bundle "$RUNNER_TEMP/bundle"', "--contract " + EVENT_CONTRACT, "--json"):
        if needle not in bundle_run:
            raise AssertionError(
                "required bundle check is missing {!r}: {!r}".format(needle, bundle_run))
    # One engine: the summary consumes the bundle, it never re-derives the matrix
    # or the fingerprints in shell (that would be a second parser).
    for step in job_steps(required):
        body = step.get("run") or ""
        for forbidden in ("jq ", "run_runtime_scenarios.py",
                          "check_event_integrity_mutations.py", "--evidence"):
            if forbidden in body:
                raise AssertionError(
                    "the required job must consume the bundle through the one "
                    "checker, not {!r}: a second rule set is a second parser".format(
                        forbidden))

    results = step_with(required, "SHORT_RESULT")
    results_env = results.get("env") or {}
    expected_env = {
        "SHORT_RESULT": "${{ needs.event-integrity-short.result }}",
        "LONG_RESULT": "${{ needs.event-integrity-long.result }}",
        "MUTATIONS_RESULT": "${{ needs.event-integrity-mutations.result }}",
    }
    for key, value in expected_env.items():
        if results_env.get(key) != value:
            raise AssertionError(
                "required must pass the upstream result through env ({}) instead of "
                "interpolating it into a quoted shell argument".format(key))
    results_run = results.get("run") or ""
    for key in expected_env:
        if 'test "${}" = "success"'.format(key) not in results_run:
            raise AssertionError(
                "required must fail unless {} is success (cancelled/skipped are not)".format(key))
    assert_single_artifact_upload("event-integrity-required", required,
                                  "event-gate-required-report", "${{ runner.temp }}/bundle")
    # The required report must live outside the `event-integrity-*` glob it
    # downloads: a job re-run would otherwise pull its own previous bundle back
    # in and duplicate every evidence file.
    report_name = [step for step in job_steps(required)
                   if str(step.get("uses", "")).startswith("actions/upload-artifact")][0]
    if str((report_name.get("with") or {}).get("name", "")).startswith("event-integrity-"):
        raise AssertionError(
            "the required report artifact must not match the event-integrity-* "
            "download pattern: it would feed its own bundle back on a re-run")


def assert_race_covers_affected_packages(workflow):
    race = ci_jobs(workflow)["race"]
    command = run_with(race, "race_check.sh")
    tokens = command.split()
    missing = [package for package in RACE_PACKAGES if package not in tokens]
    if missing:
        raise AssertionError(
            "race package set is missing {}: the set is mapped from every modified "
            "product package and formal test package, not just host/history".format(missing))


def assert_soak_command(workflow):
    soak = ci_jobs(workflow)["soak"]
    if soak.get("if") != "github.event_name == 'workflow_dispatch'":
        raise AssertionError("soak must stay a manual workflow_dispatch job")
    command = run_with(soak, "TestSoakContinuity")
    for needle in ("go -C . test ./tests/integration/host",
                   "-run '^TestSoakContinuity$'", "-count=1",
                   "-rounds=30", "-events-per-round=30"):
        if needle not in command:
            raise AssertionError("soak command is missing {!r}: {!r}".format(needle, command))
    if "TestSoak_Continuity" in command or re.search(r"\./tests/(\s|$)", command):
        raise AssertionError(
            "soak must not point at the non-existent ./tests package or "
            "TestSoak_Continuity: {!r}".format(command))
    names = set()
    for path in sorted((REPO / "tests" / "integration" / "host").glob("*_test.go")):
        names.update(re.findall(r"^func (Test\w+)\(", path.read_text(encoding="utf-8"),
                                re.MULTILINE))
    match = re.search(r"-run '\^(\w+)\$'", command)
    if not match:
        raise AssertionError("soak command carries no anchored -run selector: {!r}".format(command))
    if match.group(1) not in names:
        raise AssertionError(
            "soak selector {!r} matches no test in tests/integration/host".format(match.group(1)))


def go_version(job, job_name):
    setups = [step for step in job_steps(job)
              if str(step.get("uses", "")).startswith("actions/setup-go")]
    if len(setups) != 1:
        raise AssertionError(
            "{} must set up Go exactly once, found {}".format(job_name, len(setups)))
    version = (setups[0].get("with") or {}).get("go-version")
    if not version:
        raise AssertionError("{} pins no Go version".format(job_name))
    return version


def smoke_test_names():
    names = set()
    for path in sorted(SMOKE_TEST_DIR.glob("*_test.go")):
        names.update(re.findall(r"^func (Test\w+)\(",
                                path.read_text(encoding="utf-8"), re.MULTILINE))
    return names


def symptom_selector(run):
    match = re.search(r"-run '(\^[^']*)'", run)
    if not match:
        raise AssertionError(
            "the symptom command carries no anchored prefix selector: {!r}".format(run))
    return match.group(1)


def assert_symptom_job(workflow):
    """The symptom gate: unconditional, prefix-selected, evidence on failure.

    The three anchors cannot join the v2 contract (their absence from the
    archived task graph's registry is exactly what the contract cross-checks),
    so this job is their only automatic witness. Deleting it, narrowing the
    selector so one anchor silently drops out, or giving it any skip hatch must
    all fail here.
    """
    jobs = ci_jobs(workflow)
    job = jobs.get(SYMPTOM_JOB)
    if job is None:
        raise AssertionError(
            "ci.yml is missing the {} job: the symptom anchors would then "
            "have no unconditional gate".format(SYMPTOM_JOB))
    if job.get("runs-on") != "ubuntu-latest":
        raise AssertionError("{} must run on ubuntu-latest".format(SYMPTOM_JOB))
    if job.get("if"):
        raise AssertionError(
            "{} must not carry a job-level if: {!r}: a skipped symptom job is a "
            "silent hole in the user-voice proof".format(SYMPTOM_JOB, job.get("if")))
    if job.get("continue-on-error") not in (None, False):
        raise AssertionError("{} must not set continue-on-error".format(SYMPTOM_JOB))
    budget = job.get("timeout-minutes")
    if not isinstance(budget, int) or isinstance(budget, bool) or not 1 <= budget <= 30:
        raise AssertionError(
            "{} timeout-minutes must be a measured budget between 1 and 30, "
            "found {!r}".format(SYMPTOM_JOB, budget))
    if go_version(job, SYMPTOM_JOB) != go_version(jobs["test"], "test"):
        raise AssertionError(
            "{} must pin the same Go version as the pre-existing jobs: a smoke "
            "anchor proven on one toolchain is not proven on another".format(SYMPTOM_JOB))
    if "secrets." in json.dumps(job, ensure_ascii=False):
        raise AssertionError(
            "{} must not gate on credentials: the smoke suite is mocked".format(SYMPTOM_JOB))
    for step in job_steps(job):
        if step.get("continue-on-error") not in (None, False):
            raise AssertionError("{} step {!r} must not set continue-on-error".format(
                SYMPTOM_JOB, step.get("name")))
    # exec/async cases skip without tmux; the job would then be green while the
    # anchors never ran.
    if not any("install -y tmux" in (step.get("run") or "")
               for step in job_steps(job)):
        raise AssertionError(
            "{} must install tmux: without it the exec/async cases skip and the "
            "symptom anchors prove nothing".format(SYMPTOM_JOB))
    required = jobs.get("event-integrity-required") or {}
    needs = required.get("needs") or []
    if isinstance(needs, str):
        needs = [needs]
    if SYMPTOM_JOB in needs:
        raise AssertionError(
            "{} must stay a gate of its own: the event summary consumes the three "
            "producers' evidence through the bundle checker, and a smoke job's "
            "log is not part of that evidence schema".format(SYMPTOM_JOB))

    run = run_with(job, SYMPTOM_MODULE)
    for needle in ("-tags smoke", "-count=1", "-timeout 600s",
                   "-run '^" + SYMPTOM_PREFIX + "'"):
        if needle not in run:
            raise AssertionError(
                "{} command is missing {!r}: {!r}".format(SYMPTOM_JOB, needle, run))
    seconds = int(re.search(r"-timeout (\d+)s", run).group(1))
    if seconds <= 0 or seconds >= budget * 60:
        raise AssertionError(
            "{} must bound the go test process inside the job's timeout-minutes "
            "({}s vs {}m)".format(SYMPTOM_JOB, seconds, budget))
    if 'E="$RUNNER_TEMP/symptom"' not in run:
        raise AssertionError(
            "{} must write its evidence outside the repository, under "
            "$RUNNER_TEMP/symptom: {!r}".format(SYMPTOM_JOB, run))

    # The selector is a prefix, not an enumeration: prove it hits every pinned
    # anchor and nothing else, so neither a shrunken -run nor a widened one can
    # drift the job's meaning.
    selector = symptom_selector(run)
    pattern = re.compile(selector)
    names = smoke_test_names()
    parents = sorted(name for name in names if name.startswith(SYMPTOM_PREFIX))
    if parents != sorted(SYMPTOM_PARENTS):
        raise AssertionError(
            "the wechat-bot module must hold exactly the pinned symptom anchors, "
            "found {}".format(parents))
    missed = [name for name in parents if not pattern.search(name)]
    if missed:
        raise AssertionError(
            "the symptom selector {!r} misses {}".format(selector, missed))
    overtaken = sorted(name for name in names
                       if name not in parents and pattern.search(name))
    if overtaken:
        raise AssertionError(
            "the symptom selector {!r} also selects non-symptom tests {}: the job "
            "would run unrelated smoke work under a symptom name".format(
                selector, overtaken))

    assert_single_artifact_upload(SYMPTOM_JOB, job, SYMPTOM_ARTIFACT, SYMPTOM_EVIDENCE_DIR)
    upload = [step for step in job_steps(job)
              if str(step.get("uses", "")).startswith("actions/upload-artifact")][0]
    if (upload.get("with") or {}).get("if-no-files-found") != "error":
        raise AssertionError(
            "{} must fail when it produced no evidence, not pass quietly".format(
                SYMPTOM_JOB))
    if fnmatch.fnmatchcase(SYMPTOM_ARTIFACT, "event-integrity-*"):
        raise AssertionError(
            "the symptom artifact must stay outside the event-integrity-* download "
            "glob: the summary has no schema for it and would mix bundles")


def preflight_step_sections(text):
    parts = re.split(r'^step "([^"]+)"$', text, flags=re.MULTILINE)
    if len(parts) < 3:
        raise AssertionError("preflight.sh declares no step sections")
    return {name: body for name, body in zip(parts[1::2], parts[2::2])}


def assert_preflight_default_event_entry(text):
    sections = preflight_step_sections(text)
    for name, keywords in PREFLIGHT_EVENT_STEPS:
        body = sections.get(name)
        if body is None:
            raise AssertionError("preflight.sh has no {!r} step".format(name))
        for keyword in keywords:
            if keyword not in body:
                raise AssertionError(
                    "preflight step {!r} is missing {!r}: {!r}".format(name, keyword, body))
    long_body = sections["event integrity long"]
    for needle in ("for seed in 1 7 42", "for window in 256 32768"):
        if needle not in long_body:
            raise AssertionError(
                "preflight must run the six-shard matrix through {!r}".format(needle))

    # The event entry must be unconditional. --skip-smoke may skip the wechat
    # smoke matrix only; no opt-in switch may close the long shards by default.
    index = text.find('step "event integrity short"')
    if index < 0:
        raise AssertionError("preflight.sh has no event integrity short entry")
    guarded = text[:index].splitlines()[-3:] + text[index:].splitlines()
    for line in guarded:
        code = line.split("#", 1)[0].strip()
        if re.match(r"^(if|elif|else|fi)(\W|$)", code):
            raise AssertionError(
                "the event entry must run unconditionally, found {!r}".format(line.strip()))

    flags = set(re.findall(r"^\s*(--[a-z0-9-]+)\)", text, re.MULTILINE))
    if flags != {"--skip-smoke"}:
        raise AssertionError(
            "preflight.sh must accept only --skip-smoke, found {}: an opt-in long "
            "switch leaves the default run incomplete".format(sorted(flags)))
    if "不具备完整 MR 等价证据" not in text:
        raise AssertionError(
            "--skip-smoke must print that the run is not full MR-equivalent evidence")


class CiWorkflowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = CI_WORKFLOW.read_text(encoding="utf-8")
        cls.workflow = yaml.safe_load(cls.text)
        cls.preflight = PREFLIGHT.read_text(encoding="utf-8")

    def test_ci_workflow_parses(self):
        self.assertTrue(CI_WORKFLOW.is_file(), "ci.yml is missing")
        self.assertIsInstance(self.workflow, dict)
        self.assertIn("jobs", self.workflow)

    def test_event_producer_jobs_present_with_contract_commands(self):
        assert_event_producer_jobs(self.workflow)

    def test_long_matrix_is_three_seeds_by_two_windows(self):
        assert_long_matrix(self.workflow)

    def test_event_jobs_have_no_skip_hatch(self):
        assert_event_jobs_have_no_skip_hatch(self.workflow, self.text)

    def test_required_skeleton_needs_three_producers_and_bundle(self):
        assert_required_skeleton(self.workflow)

    def test_required_always_needs_all_three(self):
        assert_required_always_needs_all_three(self.workflow)
        required = ci_jobs(self.workflow)["event-integrity-required"]
        self.assertEqual(required.get("if"), "always()")
        self.assertEqual(sorted(required.get("needs") or []),
                         sorted(EVENT_PRODUCER_JOBS))

    def test_required_downloads_and_uses_same_checker(self):
        download = assert_required_download_covers_producer_artifacts(self.workflow)
        download_with = download.get("with") or {}
        self.assertEqual(download_with.get("pattern"), "event-integrity-*")
        self.assertEqual(download_with.get("path"), "${{ runner.temp }}/bundle")
        self.assertIs(download_with.get("merge-multiple"), False,
                      "shard evidence must keep its own directory")
        required = ci_jobs(self.workflow)["event-integrity-required"]
        checker = run_with(required, "check_runtime_evidence.py")
        self.assertIn('--bundle "$RUNNER_TEMP/bundle"', checker)
        self.assertIn("--contract " + EVENT_CONTRACT, checker)
        self.assertIn("--json", checker)
        # The producers write evidence against the same frozen contract the
        # summary checks it against; two contracts would be two truths.
        for job_name in EVENT_PRODUCER_JOBS:
            for step in job_steps(ci_jobs(self.workflow)[job_name]):
                body = step.get("run") or ""
                if "--contract" in body:
                    self.assertIn("--contract " + EVENT_CONTRACT, body,
                                  "{} uses a different contract".format(job_name))
        # And the summary never grows a second engine of its own.
        for step in job_steps(required):
            body = step.get("run") or ""
            for forbidden in ("jq ", "run_runtime_scenarios.py", "--evidence"):
                self.assertNotIn(forbidden, body,
                                 "the summary must delegate to --bundle, not {!r}".format(
                                     forbidden))

    def test_all_artifacts_uploaded_on_failure(self):
        for job_name in EVENT_JOBS:
            uploads = event_upload_steps(ci_jobs(self.workflow)[job_name])
            self.assertTrue(uploads, "{} uploads no artifact at all".format(job_name))
            for step in uploads:
                self.assertEqual(
                    step.get("if"), "always()",
                    "{} artifact {!r} must upload with if: always(): a failed run "
                    "keeps the evidence that explains the failure".format(
                        job_name, (step.get("with") or {}).get("name")))
                path = str((step.get("with") or {}).get("path", ""))
                self.assertTrue(
                    path.startswith("${{ runner.temp }}"),
                    "{} must upload from runner.temp, never the repository: {!r}".format(
                        job_name, path))
        for job_name in EVENT_PRODUCER_JOBS:
            for step in event_upload_steps(ci_jobs(self.workflow)[job_name]):
                self.assertEqual(
                    (step.get("with") or {}).get("if-no-files-found"), "error",
                    "{} produced no evidence: that must fail, not pass quietly".format(
                        job_name))

    def test_required_is_event_summary_not_replacement_for_old_jobs(self):
        jobs = ci_jobs(self.workflow)
        for name in LEGACY_JOBS:
            self.assertIn(name, jobs,
                          "the event summary must not replace the pre-existing check {!r}"
                          .format(name))
        required = jobs["event-integrity-required"]
        needs = required.get("needs") or []
        if isinstance(needs, str):
            needs = [needs]
        self.assertEqual(sorted(needs), sorted(EVENT_PRODUCER_JOBS),
                         "required aggregates the three event producers only")
        for name in LEGACY_JOBS:
            legacy_needs = jobs[name].get("needs") or []
            if isinstance(legacy_needs, str):
                legacy_needs = [legacy_needs]
            self.assertNotIn("event-integrity-required", legacy_needs,
                             "no pre-existing job may depend on the new summary")
        # The stable names are what branch protection points at: a renamed job
        # silently drops out of the protected set.
        for name in EVENT_JOBS:
            self.assertIn(name, jobs, "the stable event job name {!r} is missing".format(name))

    def test_no_expression_interpolated_into_quoted_shell_arg(self):
        # '${{ ... }}' inside single quotes lets a crafted input break out of the
        # shell quoting; inputs must reach the command via env instead.
        self.assertNotRegex(self.text, r"'\$\{\{[^}]*\}\}'")

    def test_race_covers_all_modified_test_packages(self):
        assert_race_covers_affected_packages(self.workflow)

    def test_soak_command_matches_real_test(self):
        assert_soak_command(self.workflow)

    def test_symptom_job_runs_the_three_anchors_unconditionally(self):
        assert_symptom_job(self.workflow)
        jobs = ci_jobs(self.workflow)
        self.assertNotIn(SYMPTOM_JOB, jobs["event-integrity-required"].get("needs") or [],
                         "the symptom job stays a separate gate")

    def test_negative_control_removing_symptom_job_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        del mutated["jobs"][SYMPTOM_JOB]
        with self.assertRaises(AssertionError):
            assert_symptom_job(mutated)

    def test_negative_control_symptom_skip_hatches_turn_check_red(self):
        job_if = copy.deepcopy(self.workflow)
        job_if["jobs"][SYMPTOM_JOB]["if"] = "success()"
        step_hatch = copy.deepcopy(self.workflow)
        step_with(step_hatch["jobs"][SYMPTOM_JOB], SYMPTOM_MODULE)["continue-on-error"] = True
        job_hatch = copy.deepcopy(self.workflow)
        job_hatch["jobs"][SYMPTOM_JOB]["continue-on-error"] = True
        for label, mutated in (("job-level if", job_if),
                               ("step continue-on-error", step_hatch),
                               ("job continue-on-error", job_hatch)):
            with self.subTest(mutation=label):
                with self.assertRaises(AssertionError):
                    assert_symptom_job(mutated)

    def test_negative_control_narrowing_symptom_selector_turns_check_red(self):
        for replacement in ("-run '^TestLocalSmokeSymptomAcceptedSilent$'",
                            "-run '^TestLocalSmoke'"):
            mutated = copy.deepcopy(self.workflow)
            step = step_with(mutated["jobs"][SYMPTOM_JOB], SYMPTOM_MODULE)
            step["run"] = re.sub(r"-run '\^[^']*'", replacement, step["run"])
            with self.subTest(replacement=replacement):
                with self.assertRaises(AssertionError):
                    assert_symptom_job(mutated)

    def test_negative_control_symptom_budget_toolchain_and_evidence_drift_turns_check_red(self):
        dropped = copy.deepcopy(self.workflow)
        step = step_with(dropped["jobs"][SYMPTOM_JOB], SYMPTOM_MODULE)
        step["run"] = step["run"].replace(" -timeout 600s", "")
        with self.assertRaises(AssertionError):
            assert_symptom_job(dropped)
        bloated = copy.deepcopy(self.workflow)
        bloated["jobs"][SYMPTOM_JOB]["timeout-minutes"] = 45
        with self.assertRaises(AssertionError):
            assert_symptom_job(bloated)
        drifted = copy.deepcopy(self.workflow)
        setup = [step for step in job_steps(drifted["jobs"][SYMPTOM_JOB])
                 if str(step.get("uses", "")).startswith("actions/setup-go")][0]
        setup["with"]["go-version"] = "1.25"
        with self.assertRaises(AssertionError):
            assert_symptom_job(drifted)
        skipped = copy.deepcopy(self.workflow)
        skipped["jobs"][SYMPTOM_JOB]["steps"] = [
            step for step in job_steps(skipped["jobs"][SYMPTOM_JOB])
            if "tmux" not in (step.get("run") or "")]
        with self.assertRaises(AssertionError):
            assert_symptom_job(skipped)
        lost = copy.deepcopy(self.workflow)
        for step in job_steps(lost["jobs"][SYMPTOM_JOB]):
            if str(step.get("uses", "")).startswith("actions/upload-artifact"):
                step["if"] = "success()"
        with self.assertRaises(AssertionError):
            assert_symptom_job(lost)

    def test_preflight_runs_event_short_long_mutations_by_default(self):
        assert_preflight_default_event_entry(self.preflight)

    def test_negative_control_removing_event_job_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        del mutated["jobs"]["event-integrity-mutations"]
        with self.assertRaises(AssertionError):
            assert_event_producer_jobs(mutated)

    def test_negative_control_adding_path_filter_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        on_block(mutated)["pull_request"] = {"paths": ["docs/**"]}
        with self.assertRaises(AssertionError):
            assert_event_jobs_have_no_skip_hatch(mutated, self.text)

    def test_negative_control_continue_on_error_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        mutated["jobs"]["event-integrity-short"]["continue-on-error"] = True
        with self.assertRaises(AssertionError):
            assert_event_jobs_have_no_skip_hatch(mutated, self.text)

    def test_negative_control_single_count_amplification_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        step = step_with(mutated["jobs"]["event-integrity-short"], "run_runtime_scenarios.py")
        self.assertIn("--repeat 10", step["run"])
        # Keep the ten-round flag and add a count amplification beside it: the
        # no-count rule alone must catch the mutation.
        step["run"] = step["run"] + " -count=10"
        with self.assertRaises(AssertionError):
            assert_event_producer_jobs(mutated)

    def test_negative_control_race_set_shrunk_to_host_history_turns_check_red(self):
        mutated = copy.deepcopy(self.workflow)
        step = step_with(mutated["jobs"]["race"], "race_check.sh")
        step["run"] = ("".join(["./scripts/race_check.sh . ",
                                "./tests/integration/host/... ",
                                "./tests/integration/history/..."]))
        with self.assertRaises(AssertionError):
            assert_race_covers_affected_packages(mutated)

    def test_negative_control_preflight_opt_in_long_turns_check_red(self):
        guarded = self.preflight.replace(
            'step "event integrity long"',
            'if [ -n "$EVENT_LONG_OPTIN" ]; then\nstep "event integrity long"', 1)
        guarded = guarded.replace(
            'step "event integrity mutations"',
            'fi\nstep "event integrity mutations"', 1)
        self.assertNotEqual(guarded, self.preflight)
        with self.assertRaises(AssertionError):
            assert_preflight_default_event_entry(guarded)
        flagged = self.preflight.replace(
            "    --skip-smoke) skip_smoke=1; shift ;;",
            "    --skip-smoke) skip_smoke=1; shift ;;\n"
            "    --skip-long) skip_long=1; shift ;;", 1)
        self.assertNotEqual(flagged, self.preflight)
        with self.assertRaises(AssertionError):
            assert_preflight_default_event_entry(flagged)

    def test_negative_control_dropping_a_need_turns_red(self):
        mutated = copy.deepcopy(self.workflow)
        mutated["jobs"]["event-integrity-required"]["needs"] = [
            "event-integrity-short", "event-integrity-long"]
        with self.assertRaises(AssertionError):
            assert_required_skeleton(mutated)

    def test_negative_control_removing_always_turns_red(self):
        for broken in ("success()", None):
            mutated = copy.deepcopy(self.workflow)
            if broken is None:
                del mutated["jobs"]["event-integrity-required"]["if"]
            else:
                mutated["jobs"]["event-integrity-required"]["if"] = broken
            with self.subTest(broken=broken):
                with self.assertRaises(AssertionError):
                    assert_required_skeleton(mutated)

    @staticmethod
    def run_results_script(script, values):
        env = {key: value for key, value in os.environ.items()
               if key not in ("SHORT_RESULT", "LONG_RESULT", "MUTATIONS_RESULT")}
        env.update(values)
        return subprocess.run(["bash", "-c", script], env=env,
                              capture_output=True, text=True).returncode

    def test_cancelled_or_skipped_upstream_fails(self):
        # Execute the real results step instead of asserting its text: skipped,
        # cancelled, failure and an empty/missing result must all be non-zero.
        script = run_with(ci_jobs(self.workflow)["event-integrity-required"],
                          "SHORT_RESULT")
        green = {"SHORT_RESULT": "success", "LONG_RESULT": "success",
                 "MUTATIONS_RESULT": "success"}
        self.assertEqual(self.run_results_script(script, green), 0,
                         "three successful producers must pass the summary")
        for key in sorted(green):
            for value in ("skipped", "cancelled", "failure", ""):
                values = dict(green)
                values[key] = value
                with self.subTest(key=key, value=value):
                    self.assertNotEqual(self.run_results_script(script, values), 0,
                                        "{}={!r} must fail the summary".format(key, value))
            values = dict(green)
            del values[key]
            with self.subTest(key=key, value="<missing>"):
                self.assertNotEqual(self.run_results_script(script, values), 0,
                                    "a missing {} must fail the summary".format(key))

    def test_missing_artifact_or_combo_or_short_run_fails(self):
        # Artifact: a producer artifact renamed outside the download glob would
        # silently vanish from the summary.
        mutated = copy.deepcopy(self.workflow)
        upload = event_upload_steps(mutated["jobs"]["event-integrity-long"])[0]
        upload["with"]["name"] = "event-long-shard"
        with self.assertRaises(AssertionError):
            assert_required_download_covers_producer_artifacts(mutated)
        # Combination: a dropped seed shrinks the frozen six-shard matrix.
        mutated = copy.deepcopy(self.workflow)
        mutated["jobs"]["event-integrity-long"]["strategy"]["matrix"]["seed"] = [1, 7]
        with self.assertRaises(AssertionError):
            assert_long_matrix(mutated)
        # Short run: one round instead of ten independent rounds.
        mutated = copy.deepcopy(self.workflow)
        short = step_with(mutated["jobs"]["event-integrity-short"],
                          "run_runtime_scenarios.py")
        short["run"] = short["run"].replace("--repeat 10", "--repeat 1")
        with self.assertRaises(AssertionError):
            assert_event_producer_jobs(mutated)

    def test_mixed_baseline_digest_fails(self):
        # The summary carries no digest logic of its own: it hands the bundle to
        # the same checker the producers' evidence is written for. Prove that
        # engine rejects a bundle whose evidence files disagree on the baseline,
        # using the real frozen contract and a minimal evidence skeleton.
        required = ci_jobs(self.workflow)["event-integrity-required"]
        for step in job_steps(required):
            body = step.get("run") or ""
            for forbidden in ("sha256sum", "shasum", "sha256(", "jq "):
                self.assertNotIn(forbidden, body,
                                 "the summary must not recompute the baseline itself")

        contract_path = REPO / EVENT_CONTRACT
        contract = json.loads(contract_path.read_text(encoding="utf-8"))
        case = next(test for test in contract["tests"]
                    if test.get("scope") == "event-short")
        baseline = "sha256:" + hashlib.sha256(contract_path.read_bytes()).hexdigest()
        tags = sorted({tag for tag in case.get("tags") or []})
        anchors = [{"test_id": case["id"], "name": sub["name"], "status": "pass"}
                   for sub in case.get("subtests") or []
                   if isinstance(sub, dict) and sub.get("mandatory") is not False]

        def evidence(repeat_index, contract_digest):
            return {
                "schema_version": 2,
                "profile": "event-integrity",
                "run_id": str(uuid.uuid4()),
                "scope": "event-short",
                "test_ids": [case["id"]],
                "repeat_index": repeat_index,
                "repeat_total": 10,
                "contract_digest": contract_digest,
                "module_fingerprints": [{
                    "module_path": "github.com/SpellingDragon/tagent",
                    "dir": ".",
                    "revision": "0123456789abcdef0123456789abcdef01234567",
                    "content_hash": "sha256:" + "a" * 64,
                }],
                "run_config": {"seed": None, "window": None, "race": True,
                                "tags": tags},
                "go_test_exit_code": 0,
                "test_log": "test-log.jsonl",
                "test_log_sha256": "sha256:" + "b" * 64,
                "verdict": "match",
                "subtests": anchors,
            }

        with tempfile.TemporaryDirectory() as tmp:
            bundle = Path(tmp) / "bundle"
            for index, digest in enumerate((baseline, "sha256:" + "9" * 64), start=1):
                directory = bundle / "short-{}".format(index)
                directory.mkdir(parents=True)
                (directory / "evidence.json").write_text(
                    json.dumps(evidence(index, digest), ensure_ascii=False, indent=2),
                    encoding="utf-8")
            proc = subprocess.run(
                [sys.executable, str(REPO / "scripts" / "gates" / "check_runtime_evidence.py"),
                 "--bundle", str(bundle), "--contract", str(contract_path), "--json"],
                capture_output=True, text=True)
        self.assertNotEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("mixes contract digests", proc.stdout,
                      "a bundle that mixes baselines must be named, not just red")


if __name__ == "__main__":
    unittest.main()
