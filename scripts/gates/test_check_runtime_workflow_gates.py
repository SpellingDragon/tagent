#!/usr/bin/env python3
"""05/5.4 + 05/5.5 anchors: ten independent short shards and one root-gate owner.

Both contracts are structural, so they are parsed from the YAML (and the
negative controls mutate parsed copies) rather than grepped from the text:

* TestCIShortMatrixPreservesTenIndependentRounds: event-integrity-short is a
  matrix over exactly the ten repeat indexes 1..10 with fail-fast false; each
  shard runs one independent runner process through the REPEAT_INDEX env
  binding (no --repeat batch, no -count amplification, no --test-id subset),
  uploads exactly one artifact named per index, and all ten expanded artifact
  names are collected by the required job's existing download pattern.
* TestRootProductGatesHaveOneOwner: root build/vet/short run exactly once
  across ci.yml and runtime-contracts.yml, and that one owner is ci.yml's
  stable "test" job; runtime-contracts keeps the stable "format-and-contract"
  check name.

Negative controls mutate parsed COPIES (the real workflows are never touched):
a missing/duplicated index, an unbound env, a count-amplified run, a colliding
artifact name, an empty artifact path, a missing/duplicated/migrated root gate
and a renamed stable check must each turn the corresponding assertion red.
"""
import copy
import fnmatch
import re
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[2]
CI_WORKFLOW = REPO / ".github" / "workflows" / "ci.yml"
CONTRACTS_WORKFLOW = REPO / ".github" / "workflows" / "runtime-contracts.yml"
EVENT_CONTRACT = "golden/runtime/event_integrity_scenarios.json"

SHORT_JOB = "event-integrity-short"
SHORT_ROUNDS = list(range(1, 11))
SHORT_TIMEOUT_MINUTES = 15
SHORT_ARTIFACT = "event-integrity-short-${{ matrix.repeat-index }}"
SHORT_PATH = "${{ runner.temp }}/event-short"
REPEAT_INDEX_ENV = "REPEAT_INDEX"
REPEAT_INDEX_VALUE = "${{ matrix.repeat-index }}"

# The one owner of the root product gates, and the stable check names branch
# protection points at (05/5.5 / design D3). A rename silently drops a check out
# of the protected set, so both the job id and the rendered check name stay put.
ROOT_OWNER_WORKFLOW = "ci.yml"
ROOT_OWNER_JOB = "test"
CONTRACTS_JOB = "contracts"
CONTRACTS_CHECK_NAME = "format-and-contract"
ROOT_GATES = (
    ("Build (root module)", "go build ./..."),
    ("Vet (root module)", "go vet ./..."),
    ("Test (root module, short)", "go test ./... -short -count=1"),
)


def load_workflow(path):
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def jobs(parsed):
    return parsed.get("jobs") or {}


def steps(job):
    return job.get("steps") or []


def normalize(command):
    return " ".join(str(command or "").split())


def exactly_one_step(job, needle, where):
    hits = [step for step in steps(job) if needle in (step.get("run") or "")]
    if len(hits) != 1:
        raise AssertionError(
            "{} must run exactly one step containing {!r}, found {}".format(
                where, needle, len(hits)))
    return hits[0]


def required_download_pattern(parsed):
    required = jobs(parsed).get("event-integrity-required")
    if required is None:
        raise AssertionError(
            "ci.yml is missing event-integrity-required: no pattern can collect "
            "the short shards")
    downloads = [step for step in steps(required)
                 if str(step.get("uses", "")).startswith("actions/download-artifact")]
    if len(downloads) != 1:
        raise AssertionError(
            "event-integrity-required must download the evidence bundle exactly once")
    pattern = (downloads[0].get("with") or {}).get("pattern")
    if not isinstance(pattern, str) or not pattern.strip():
        raise AssertionError(
            "the required download step must name an artifact pattern, found {!r}"
            .format(pattern))
    return pattern


def assert_short_matrix(parsed):
    """05/5.4: ten independent shards, each one complete short round."""
    job = jobs(parsed).get(SHORT_JOB)
    if job is None:
        raise AssertionError("ci.yml is missing the {} job".format(SHORT_JOB))

    strategy = job.get("strategy") or {}
    if strategy.get("fail-fast") is not False:
        raise AssertionError(
            "{} must keep fail-fast: false: one red shard must not cancel the "
            "other nine rounds' evidence".format(SHORT_JOB))
    matrix = strategy.get("matrix") or {}
    if matrix.get("repeat-index") != SHORT_ROUNDS:
        raise AssertionError(
            "{} matrix.repeat-index must be exactly 1..10, found {!r}".format(
                SHORT_JOB, matrix.get("repeat-index")))

    step = exactly_one_step(job, "run_runtime_scenarios.py", SHORT_JOB)
    run = normalize(step.get("run"))
    for needle in ("--contract " + EVENT_CONTRACT, "--scope event-short",
                   '--repeat-index "$REPEAT_INDEX"',
                   '--out "$RUNNER_TEMP/event-short"'):
        if needle not in run:
            raise AssertionError(
                "{} command is missing {!r}: {!r}".format(SHORT_JOB, needle, run))
    # One shard = exactly one round. A batch --repeat here would multiply it,
    # -count would amplify the go test process, and --test-id would shrink the
    # complete scope. --repeat-index stays legal (it is the single-round flag).
    if re.search(r"--repeat(?![-\w])", run):
        raise AssertionError(
            "{} must run one round per shard via --repeat-index, not a --repeat "
            "batch: {!r}".format(SHORT_JOB, run))
    if re.search(r"-count(?:[= ]|$)", run):
        raise AssertionError(
            "{} must not amplify a round with -count: {!r}".format(SHORT_JOB, run))
    if "--test-id" in run:
        raise AssertionError(
            "{} must run the complete scope, not a --test-id subset: {!r}".format(
                SHORT_JOB, run))

    env = step.get("env") or {}
    if env.get(REPEAT_INDEX_ENV) != REPEAT_INDEX_VALUE:
        raise AssertionError(
            "{} matrix value must reach the command through env {} = {!r}, "
            "found {!r}".format(SHORT_JOB, REPEAT_INDEX_ENV, REPEAT_INDEX_VALUE,
                                env.get(REPEAT_INDEX_ENV)))

    if job.get("timeout-minutes") != SHORT_TIMEOUT_MINUTES:
        raise AssertionError(
            "{} timeout-minutes must be the per-shard budget {} (one 2m58s round "
            "plus cold compile, not the ten-round 45m bound), found {!r}".format(
                SHORT_JOB, SHORT_TIMEOUT_MINUTES, job.get("timeout-minutes")))

    uploads = [step for step in steps(job)
               if str(step.get("uses", "")).startswith("actions/upload-artifact")]
    if len(uploads) != 1:
        raise AssertionError(
            "{} must upload exactly one artifact per shard, found {}".format(
                SHORT_JOB, len(uploads)))
    upload = uploads[0]
    if upload.get("if") != "always()":
        raise AssertionError(
            "{} artifact must upload with if: always(): a failed round keeps the "
            "evidence that explains it".format(SHORT_JOB))
    with_block = upload.get("with") or {}
    if with_block.get("name") != SHORT_ARTIFACT:
        raise AssertionError(
            "{} artifact name must be {!r}, found {!r}".format(
                SHORT_JOB, SHORT_ARTIFACT, with_block.get("name")))
    path = with_block.get("path")
    if not isinstance(path, str) or not path.strip():
        raise AssertionError(
            "{} artifact path must not be empty: one shard's evidence must land "
            "in its own artifact".format(SHORT_JOB))
    if path != SHORT_PATH:
        raise AssertionError(
            "{} artifact path must be {!r}, found {!r}".format(
                SHORT_JOB, SHORT_PATH, path))
    if with_block.get("if-no-files-found") != "error":
        raise AssertionError(
            "{} must fail when it produced no evidence, not pass quietly".format(
                SHORT_JOB))

    expanded = [SHORT_ARTIFACT.replace("${{ matrix.repeat-index }}", str(index))
                for index in SHORT_ROUNDS]
    if len(set(expanded)) != len(SHORT_ROUNDS):
        raise AssertionError(
            "{} must produce ten unique artifact names, found {}".format(
                SHORT_JOB, expanded))
    pattern = required_download_pattern(parsed)
    missed = [name for name in expanded if not fnmatch.fnmatchcase(name, pattern)]
    if missed:
        raise AssertionError(
            "{} artifacts {} do not match the required download pattern {!r}: "
            "the summary would silently miss those shards".format(
                SHORT_JOB, missed, pattern))


def is_root_scoped(step):
    working_directory = step.get("working-directory")
    return working_directory in (None, "", ".")


def root_gate_hits(parsed, workflow_label):
    commands = {command for _name, command in ROOT_GATES}
    hits = []
    for job_name, job in jobs(parsed).items():
        for step in steps(job):
            if not is_root_scoped(step):
                continue  # example-module gate, not the root product gate
            command = normalize(step.get("run"))
            if command in commands:
                hits.append((workflow_label, job_name, step, command))
    return hits


def effective_check_name(job_id, job):
    name = job.get("name")
    return str(name) if name else str(job_id)


def assert_stable_check_names(ci_parsed, contracts_parsed):
    if ROOT_OWNER_JOB not in jobs(ci_parsed):
        raise AssertionError(
            "ci.yml is missing the stable {!r} job: branch protection points at "
            "that check".format(ROOT_OWNER_JOB))
    ci_name = effective_check_name(ROOT_OWNER_JOB, jobs(ci_parsed)[ROOT_OWNER_JOB])
    if ci_name != ROOT_OWNER_JOB:
        raise AssertionError(
            "ci.yml's root product job must keep the stable check name {!r}, "
            "found {!r}".format(ROOT_OWNER_JOB, ci_name))
    contracts = jobs(contracts_parsed).get(CONTRACTS_JOB)
    if contracts is None:
        raise AssertionError(
            "runtime-contracts.yml is missing the {!r} job".format(CONTRACTS_JOB))
    contracts_name = effective_check_name(CONTRACTS_JOB, contracts)
    if contracts_name != CONTRACTS_CHECK_NAME:
        raise AssertionError(
            "runtime-contracts.yml's {!r} job must keep the stable check name "
            "{!r}, found {!r}".format(CONTRACTS_JOB, CONTRACTS_CHECK_NAME,
                                      contracts_name))
    for label, parsed in (("ci.yml", ci_parsed),
                          ("runtime-contracts.yml", contracts_parsed)):
        for job_id, job in jobs(parsed).items():
            name = effective_check_name(job_id, job)
            if name == ROOT_OWNER_JOB and (label, job_id) != (ROOT_OWNER_WORKFLOW,
                                                             ROOT_OWNER_JOB):
                raise AssertionError(
                    "the check name {!r} is duplicated at {}/{}: branch "
                    "protection cannot tell the owners apart".format(
                        name, label, job_id))
            if name == CONTRACTS_CHECK_NAME and (label, job_id) != (
                    "runtime-contracts.yml", CONTRACTS_JOB):
                raise AssertionError(
                    "the check name {!r} is duplicated at {}/{}: branch "
                    "protection cannot tell the owners apart".format(
                        name, label, job_id))


def assert_root_gates_have_one_owner(ci_parsed, contracts_parsed):
    """05/5.5: the three root product gates run once, and ci.yml/test owns them."""
    assert_stable_check_names(ci_parsed, contracts_parsed)
    hits = (root_gate_hits(ci_parsed, "ci.yml")
            + root_gate_hits(contracts_parsed, "runtime-contracts.yml"))
    for name, command in ROOT_GATES:
        owners = [(label, job_name) for label, job_name, _step, hit
                  in hits if hit == command]
        if len(owners) != 1:
            raise AssertionError(
                "root gate {!r} must have exactly one owner across ci.yml and "
                "runtime-contracts.yml, found {}".format(name, owners or "none"))
        owner = owners[0]
        if owner != (ROOT_OWNER_WORKFLOW, ROOT_OWNER_JOB):
            raise AssertionError(
                "root gate {!r} must be owned by ci.yml's {!r} job, found {}/{}: "
                "a second executor doubles every failure and every toolchain "
                "upgrade".format(name, ROOT_OWNER_JOB, owner[0], owner[1]))


class TestCIShortMatrixPreservesTenIndependentRounds(unittest.TestCase):
    """05/5.4: the short scope is ten shards, each one independent round."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = load_workflow(CI_WORKFLOW)

    def mutated(self):
        return copy.deepcopy(self.workflow)

    def test_short_matrix_preserves_ten_independent_rounds(self):
        assert_short_matrix(self.workflow)

    def test_negative_control_missing_or_duplicated_index_turns_check_red(self):
        for label, values in (
                ("missing last index", SHORT_ROUNDS[:-1]),
                ("duplicated index", [1] + SHORT_ROUNDS[1:-1] + [1]),
                ("out-of-range index", SHORT_ROUNDS[:-1] + [11]),
                ("one shared index", [1] * 10)):
            mutated = self.mutated()
            strategy = mutated["jobs"][SHORT_JOB].setdefault("strategy", {})
            strategy.setdefault("matrix", {})["repeat-index"] = values
            with self.subTest(mutation=label):
                with self.assertRaises(AssertionError):
                    assert_short_matrix(mutated)

    def test_negative_control_unbound_env_turns_check_red(self):
        dropped = self.mutated()
        step = exactly_one_step(dropped["jobs"][SHORT_JOB],
                                "run_runtime_scenarios.py", SHORT_JOB)
        step.setdefault("env", {}).pop(REPEAT_INDEX_ENV, None)
        with self.subTest(mutation="env removed"):
            with self.assertRaises(AssertionError):
                assert_short_matrix(dropped)
        # The matrix value must stay a binding, never a constant.
        constant = self.mutated()
        constant_step = exactly_one_step(constant["jobs"][SHORT_JOB],
                                         "run_runtime_scenarios.py", SHORT_JOB)
        constant_step.setdefault("env", {})[REPEAT_INDEX_ENV] = "1"
        with self.subTest(mutation="constant index"):
            with self.assertRaises(AssertionError):
                assert_short_matrix(constant)

    def test_negative_control_count_amplification_turns_check_red(self):
        for label, suffix in (("-count amplification", " -count=10"),
                              ("--repeat batch", " --repeat 10")):
            mutated = self.mutated()
            step = exactly_one_step(mutated["jobs"][SHORT_JOB],
                                    "run_runtime_scenarios.py", SHORT_JOB)
            step["run"] = step["run"] + suffix
            with self.subTest(mutation=label):
                with self.assertRaises(AssertionError):
                    assert_short_matrix(mutated)

    def test_negative_control_artifact_collision_or_empty_path_turns_check_red(self):
        for label, mutation in (("colliding name", "name"),
                                ("empty path", "path")):
            mutated = self.mutated()
            upload = [step for step in steps(mutated["jobs"][SHORT_JOB])
                      if str(step.get("uses", "")).startswith("actions/upload-artifact")][0]
            upload["with"][mutation] = "" if mutation == "path" else "event-integrity-short"
            with self.subTest(mutation=label):
                with self.assertRaises(AssertionError):
                    assert_short_matrix(mutated)
        # Unique names are not enough: the required job must actually collect
        # them. Narrowing its pattern has to turn the coverage check red.
        outside_pattern = self.mutated()
        required = outside_pattern["jobs"]["event-integrity-required"]
        download = [step for step in steps(required)
                    if str(step.get("uses", "")).startswith("actions/download-artifact")][0]
        download["with"]["pattern"] = "event-integrity-long-*"
        with self.subTest(mutation="outside required pattern"):
            with self.assertRaises(AssertionError):
                assert_short_matrix(outside_pattern)


class TestRootProductGatesHaveOneOwner(unittest.TestCase):
    """05/5.5: root build/vet/short run once, in ci.yml's stable test job."""

    @classmethod
    def setUpClass(cls):
        cls.ci = load_workflow(CI_WORKFLOW)
        cls.contracts = load_workflow(CONTRACTS_WORKFLOW)

    def copy_pair(self):
        return copy.deepcopy(self.ci), copy.deepcopy(self.contracts)

    def test_root_build_vet_short_have_one_owner(self):
        assert_root_gates_have_one_owner(self.ci, self.contracts)

    def test_negative_control_missing_root_gate_turns_check_red(self):
        for name, command in ROOT_GATES:
            ci, contracts = self.copy_pair()
            job = ci["jobs"][ROOT_OWNER_JOB]
            job["steps"] = [step for step in steps(job)
                            if normalize(step.get("run")) != command]
            with self.subTest(gate=name):
                with self.assertRaises(AssertionError):
                    assert_root_gates_have_one_owner(ci, contracts)

    def test_negative_control_duplicated_root_gate_turns_check_red(self):
        for name, command in ROOT_GATES:
            ci, contracts = self.copy_pair()
            contracts["jobs"][CONTRACTS_JOB]["steps"].append({
                "name": name,
                "run": command,
            })
            with self.subTest(gate=name):
                with self.assertRaises(AssertionError):
                    assert_root_gates_have_one_owner(ci, contracts)

    def test_negative_control_migrated_root_gate_turns_check_red(self):
        ci, contracts = self.copy_pair()
        root_commands = {command for _name, command in ROOT_GATES}
        moved = [step for step in steps(ci["jobs"][ROOT_OWNER_JOB])
                 if normalize(step.get("run")) in root_commands]
        ci["jobs"][ROOT_OWNER_JOB]["steps"] = [
            step for step in steps(ci["jobs"][ROOT_OWNER_JOB])
            if step not in moved]
        contracts["jobs"][CONTRACTS_JOB]["steps"].extend(moved)
        with self.assertRaises(AssertionError):
            assert_root_gates_have_one_owner(ci, contracts)

    def test_negative_control_renamed_stable_check_turns_check_red(self):
        renamed_owner_ci, renamed_owner_contracts = self.copy_pair()
        renamed_owner_ci["jobs"]["root-test"] = renamed_owner_ci["jobs"].pop(
            ROOT_OWNER_JOB)
        with self.subTest(mutation="ci/test job renamed"):
            with self.assertRaises(AssertionError):
                assert_root_gates_have_one_owner(renamed_owner_ci,
                                                 renamed_owner_contracts)

        renamed_ci, renamed_contracts = self.copy_pair()
        renamed_contracts["jobs"][CONTRACTS_JOB]["name"] = "contracts"
        with self.subTest(mutation="format-and-contract name dropped"):
            with self.assertRaises(AssertionError):
                assert_root_gates_have_one_owner(renamed_ci, renamed_contracts)

        duplicate_ci, duplicate_contracts = self.copy_pair()
        duplicate_contracts["jobs"]["second-contracts"] = {
            "name": ROOT_OWNER_JOB,
            "runs-on": "ubuntu-latest",
            "steps": [{"run": "true"}],
        }
        with self.subTest(mutation="test check name duplicated"):
            with self.assertRaises(AssertionError):
                assert_root_gates_have_one_owner(duplicate_ci, duplicate_contracts)


if __name__ == "__main__":
    unittest.main()
