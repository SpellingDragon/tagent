#!/usr/bin/env python3
"""04.402 + 05/5.4 + 05/5.5 anchors: race shards and one root-gate owner.

These contracts are structural, so they are parsed from the YAML (and the
negative controls mutate parsed copies) rather than grepped from the text:

* TestCIShortMatrixPreservesTenIndependentRounds: event-integrity-short is a
  matrix over exactly the ten repeat indexes 1..10 with fail-fast false; each
  shard runs one independent runner process through the REPEAT_INDEX env
  binding (no --repeat batch, no -count amplification, no --test-id subset),
  uploads exactly one artifact named per index, and all ten expanded artifact
  names are collected by the required job's existing download pattern.
* TestRootProductGatesHaveOneOwner: root build/vet/short and the isolated
  response-call cap-fuse consumer-path rerun each run exactly once across
  ci.yml and runtime-contracts.yml, and that one owner is ci.yml's stable
  "test" job; runtime-contracts keeps the stable "format-and-contract" check
  name. The short run skips the exact case and the isolated step reruns it
  without -short.
* TestGoCacheCoversThreeModules: every setup-go enables module/build caching
  and hashes the root, wechat-bot and cli-host go.sum files.
* TestRaceShardsHaveOneFailClosedAggregate: race is exactly three canonical
  shards, each uploading its selected package manifest; race-aggregate refuses
  non-success results, compares the uploaded union with the script's full set,
  and owns the only consumer-path and wechat-bot race reruns.

Negative controls mutate parsed COPIES (the real workflows are never touched):
a missing/duplicated index, an unbound env, a count-amplified run, a colliding
artifact name, an empty artifact path, a missing/duplicated/migrated root gate
(including either half of the consumer-path isolation), a renamed stable check,
a stripped module checksum, a missing/duplicated race shard, a lost shard
manifest or union check, a non-fail-closed aggregate, and a duplicated or
migrated consumer/wechat-bot rerun must each turn the corresponding assertion
red.
"""
import copy
import fnmatch
import re
import subprocess
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
RACE_JOB = "race"
RACE_SHARDS = [1, 2, 3]
RACE_MAIN_STEP = "Race (every first-party package + all integration suites)"
RACE_CONSUMER_STEP = "Race (response-call cap fuse consumer path)"
RACE_WECHAT_STEP = "Race (wechat-bot module)"
RACE_AGGREGATE_JOB = "race-aggregate"
RACE_SHARD_ENV = "RACE_SHARD"
RACE_SHARD_VALUE = "${{ matrix.shard }}"
RACE_MANIFEST_ENV = "RACE_CHECK_PACKAGE_FILE"
RACE_MANIFEST_PATH = "${{ runner.temp }}/race-shard-${{ matrix.shard }}/packages.txt"
RACE_ARTIFACT = "race-shard-${{ matrix.shard }}"
RACE_ARTIFACT_PATH = "${{ runner.temp }}/race-shard-${{ matrix.shard }}"
RACE_BUNDLE_PATH = "${{ runner.temp }}/race-bundle"
CONSUMER_SKIP = "^TestResponseCallCapFuse$/^consumer_path_holds_the_batch_for_approval$"
RACE_CONSUMER_COMMAND = (
    "go test -race -count=1 -p 1 ./tests/integration/execution "
    "-run '^TestResponseCallCapFuse$/^consumer_path_holds_the_batch_for_approval$'")
RACE_WECHAT_COMMAND = "go -C examples/wechat-bot test -race -count=1 -p 1 ./..."
CONTRACTS_JOB = "contracts"
CONTRACTS_CHECK_NAME = "format-and-contract"
ROOT_GATES = (
    ("Build (root module)", "go build ./..."),
    ("Vet (root module)", "go vet ./..."),
    ("Test (root module, short)",
     "go test ./... -short -count=1 -p 4 -skip "
     "'^TestResponseCallCapFuse$/^consumer_path_holds_the_batch_for_approval$'"),
    ("Test (root module, response-call cap fuse consumer path)",
     "go test ./tests/integration/execution "
     "-run '^TestResponseCallCapFuse$/^consumer_path_holds_the_batch_for_approval$' "
     "-count=1"),
)

GO_CACHE_DEPENDENCY_PATHS = (
    "go.sum",
    "examples/wechat-bot/go.sum",
    "examples/cli-host/go.sum",
)


def load_workflow(path):
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def jobs(parsed):
    return parsed.get("jobs") or {}


def steps(job):
    return job.get("steps") or []


def go_setup_steps(workflow):
    return [(job_name, step)
            for job_name, job in jobs(workflow).items()
            for step in steps(job)
            if str(step.get("uses", "")).startswith("actions/setup-go")]


def assert_go_cache_covers_all_modules(workflow):
    """Hash every module's checksums into setup-go's module/build cache key."""
    setups = go_setup_steps(workflow)
    if not setups:
        raise AssertionError("ci.yml has no actions/setup-go step")
    setup_jobs = {job_name for job_name, _setup in setups}
    for required_job in (RACE_JOB, RACE_AGGREGATE_JOB):
        if required_job not in setup_jobs:
            raise AssertionError(
                "{} must keep actions/setup-go module/build caching".format(
                    required_job))
    for job_name, setup in setups:
        config = setup.get("with") or {}
        if config.get("cache") is not True:
            raise AssertionError(
                "{} setup-go must enable cache: true to restore module/build "
                "caches".format(job_name))
        dependency_paths = config.get("cache-dependency-path")
        if not isinstance(dependency_paths, str) or not dependency_paths.strip():
            raise AssertionError(
                "{} setup-go cache-dependency-path must name all three modules' "
                "go.sum files".format(job_name))
        declared = {line.strip() for line in dependency_paths.splitlines()
                    if line.strip()}
        missing = [path for path in GO_CACHE_DEPENDENCY_PATHS
                   if path not in declared]
        if missing:
            raise AssertionError(
                "{} setup-go cache-dependency-path is missing {}: checksum "
                "changes in any of the three independent modules must invalidate "
                "the shared module/build cache".format(
                    job_name, ", ".join(missing)))


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
        if name == "Test (root module, response-call cap fuse consumer path)":
            owner_step = next(step for label, job_name, step, hit in hits if hit == command)
            if owner_step.get("env", {}).get("TMPDIR") != "/dev/shm":
                raise AssertionError("short consumer step must use tmpfs for its fsync-heavy fixture")


def named_steps(job, name, where):
    hits = [step for step in steps(job) if step.get("name") == name]
    if len(hits) != 1:
        raise AssertionError(
            "{} must have exactly one {!r} step, found {}".format(
                where, name, len(hits)))
    return hits[0]


def command_owners(ci_parsed, contracts_parsed, predicate):
    owners = []
    for label, parsed in (("ci.yml", ci_parsed),
                          ("runtime-contracts.yml", contracts_parsed)):
        for job_name, job in jobs(parsed).items():
            for step in steps(job):
                if predicate(normalize(step.get("run"))):
                    owners.append((label, job_name, step))
    return owners


def is_consumer_race(command):
    return all(needle in command for needle in (
        "go test", "-race", "./tests/integration/execution",
        "TestResponseCallCapFuse", "consumer_path_holds_the_batch_for_approval"))


def is_wechat_race(command):
    return "go -C examples/wechat-bot test" in command and \
        "-race" in command and "./..." in command


def assert_race_sharding_and_aggregate(ci_parsed, contracts_parsed):
    """Three canonical shards feed one fail-closed aggregate and one rerun."""
    race = jobs(ci_parsed).get(RACE_JOB)
    if race is None:
        raise AssertionError("ci.yml is missing the race matrix job")
    strategy = race.get("strategy") or {}
    if strategy.get("fail-fast") is not False:
        raise AssertionError("race matrix must keep fail-fast: false")
    if strategy.get("matrix") != {"shard": RACE_SHARDS}:
        raise AssertionError(
            "race matrix must be exactly shard [1, 2, 3], found {!r}".format(
                strategy.get("matrix")))
    if race.get("if") or race.get("continue-on-error"):
        raise AssertionError("race matrix must not have a skip hatch or continue-on-error")
    for step in steps(race):
        if step.get("continue-on-error"):
            raise AssertionError("race shard steps must not continue on error")

    main = named_steps(race, RACE_MAIN_STEP, RACE_JOB)
    runners = [step for step in steps(race) if "race_check.sh" in (step.get("run") or "")]
    if runners != [main]:
        raise AssertionError("race matrix must have exactly one race_check.sh runner")
    env = main.get("env") or {}
    if env.get(RACE_SHARD_ENV) != RACE_SHARD_VALUE:
        raise AssertionError("race main step must bind RACE_SHARD to matrix.shard")
    if env.get(RACE_MANIFEST_ENV) != RACE_MANIFEST_PATH:
        raise AssertionError("race main step must write the per-shard package manifest")
    goflags = str(env.get("GOFLAGS", ""))
    if normalize(goflags) != "-skip=" + CONSUMER_SKIP:
        raise AssertionError("race main step must skip only the consumer path")
    run = normalize(main.get("run"))
    expected_run = (
        'set -euo pipefail mkdir -p "$(dirname "$RACE_CHECK_PACKAGE_FILE")" '
        './scripts/gates/race_check.sh --shard "$RACE_SHARD"')
    if run != expected_run:
        raise AssertionError(
            "race main step must only create its manifest directory and select "
            "the canonical shard by number, found {!r}".format(run))
    consumer = named_steps(race, RACE_CONSUMER_STEP, RACE_JOB)
    if consumer.get("if") != "matrix.shard == 2":
        raise AssertionError("consumer race step must run only in shard 2")
    if normalize(consumer.get("run")) != RACE_CONSUMER_COMMAND:
        raise AssertionError("shard 2 must rerun the exact consumer path once with -race")
    if consumer.get("env", {}).get("TMPDIR") != "/dev/shm":
        raise AssertionError("shard 2 consumer step must use tmpfs")
    wechat = named_steps(race, RACE_WECHAT_STEP, RACE_JOB)
    if wechat.get("if") != "matrix.shard == 3":
        raise AssertionError("wechat race step must run only in shard 3")
    if normalize(wechat.get("run")) != RACE_WECHAT_COMMAND:
        raise AssertionError("wechat-bot must preserve its independent-module race command")

    uploads = [step for step in steps(race)
               if str(step.get("uses", "")).startswith("actions/upload-artifact")]
    if len(uploads) != 1:
        raise AssertionError("race matrix must upload exactly one manifest per shard")
    upload = uploads[0]
    if upload.get("if") != "always()":
        raise AssertionError("race shard manifests must upload even when the race run fails")
    with_block = upload.get("with") or {}
    if with_block.get("name") != RACE_ARTIFACT:
        raise AssertionError("race artifact name must include matrix.shard")
    if with_block.get("path") != RACE_ARTIFACT_PATH:
        raise AssertionError("race artifact path must contain the shard manifest")
    if with_block.get("if-no-files-found") != "error":
        raise AssertionError("a shard with no package manifest must fail")
    ordered_race = steps(race)
    if not ordered_race.index(main) < ordered_race.index(consumer) < \
            ordered_race.index(wechat) < ordered_race.index(upload):
        raise AssertionError("consumer and wechat steps must follow the shard run and precede manifest upload")

    aggregate = jobs(ci_parsed).get(RACE_AGGREGATE_JOB)
    if aggregate is None:
        raise AssertionError("ci.yml is missing race-aggregate")
    if aggregate.get("needs") != [RACE_JOB]:
        raise AssertionError("race-aggregate must need the complete race matrix")
    if aggregate.get("if") != "always()":
        raise AssertionError(
            "race-aggregate must use always() so failed/skipped/cancelled shards stay red")
    if aggregate.get("continue-on-error"):
        raise AssertionError("race-aggregate must not continue on error")

    ordered = steps(aggregate)
    results = named_steps(aggregate, "Check race shard results", RACE_AGGREGATE_JOB)
    results_env = results.get("env") or {}
    if results_env.get("RACE_RESULT") != "${{ needs.race.result }}":
        raise AssertionError("aggregate must read the race matrix result through env")
    results_run = normalize(results.get("run"))
    if results_run != (
            'set -euo pipefail test -n "$RACE_RESULT" '
            'test "$RACE_RESULT" = "success"'):
        raise AssertionError("aggregate must refuse empty, failed, skipped and cancelled results")

    downloads = [step for step in ordered
                 if str(step.get("uses", "")).startswith("actions/download-artifact")]
    if len(downloads) != 1:
        raise AssertionError("race-aggregate must download the shard manifests exactly once")
    download_with = downloads[0].get("with") or {}
    if download_with.get("pattern") != "race-shard-*":
        raise AssertionError("aggregate download pattern must collect every race shard")
    if download_with.get("path") != RACE_BUNDLE_PATH:
        raise AssertionError("aggregate must keep one directory per shard artifact")
    if download_with.get("merge-multiple") is not False:
        raise AssertionError("aggregate must not merge same-named shard manifests")

    union = named_steps(aggregate, "Check race shard package union", RACE_AGGREGATE_JOB)
    if normalize(union.get("run")) != \
            './scripts/gates/race_check.sh --shard-union-check "$RUNNER_TEMP/race-bundle"':
        raise AssertionError("aggregate must compare uploaded manifests with the canonical set")
    union_runners = [step for step in ordered
                     if "race_check.sh" in (step.get("run") or "")]
    if union_runners != [union]:
        raise AssertionError("race-aggregate must have exactly one race_check.sh union runner")

    for step in ordered:
        if step.get("if"):
            raise AssertionError(
                "race-aggregate steps must not have conditional skip hatches")
        if step.get("continue-on-error"):
            raise AssertionError("race-aggregate steps must not continue on error")
    if not ordered.index(results) < ordered.index(downloads[0]) < ordered.index(union):
        raise AssertionError("aggregate order must be results, manifests, union")

    for predicate, command, step_name, owner_job in (
            (is_consumer_race, RACE_CONSUMER_COMMAND, RACE_CONSUMER_STEP, RACE_JOB),
            (is_wechat_race, RACE_WECHAT_COMMAND, RACE_WECHAT_STEP, RACE_JOB)):
        owners = command_owners(ci_parsed, contracts_parsed, predicate)
        if len(owners) != 1:
            raise AssertionError(
                "{!r} must run exactly once across both workflows, found {}".format(
                    step_name, [(label, job_name) for label, job_name, _step in owners]))
        label, job_name, owner_step = owners[0]
        if (label, job_name) != ("ci.yml", owner_job):
            raise AssertionError(
                "{!r} must be owned only by ci.yml/{}, found {}/{}".format(
                    step_name, owner_job, label, job_name))
        if normalize(owner_step.get("run")) != command:
            raise AssertionError(
                "{!r} must keep its exact race command, found {!r}".format(
                    step_name, normalize(owner_step.get("run"))))


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
    """05/5.5: root gates and the isolated consumer-path rerun execute once."""

    @classmethod
    def setUpClass(cls):
        cls.ci = load_workflow(CI_WORKFLOW)
        cls.contracts = load_workflow(CONTRACTS_WORKFLOW)

    def copy_pair(self):
        return copy.deepcopy(self.ci), copy.deepcopy(self.contracts)

    def test_root_build_vet_short_and_consumer_path_have_one_owner(self):
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


class TestRaceShardsHaveOneFailClosedAggregate(unittest.TestCase):
    """Package shards preserve coverage; consumer and bot reruns execute once."""

    @classmethod
    def setUpClass(cls):
        cls.ci = load_workflow(CI_WORKFLOW)
        cls.contracts = load_workflow(CONTRACTS_WORKFLOW)

    def copy_pair(self):
        return copy.deepcopy(self.ci), copy.deepcopy(self.contracts)

    def test_race_matrix_aggregate_and_isolated_reruns_have_one_owner(self):
        assert_race_sharding_and_aggregate(self.ci, self.contracts)

    def test_negative_control_missing_or_duplicated_shard_turns_check_red(self):
        for label, values in (("missing shard", [1, 2]),
                              ("duplicated shard", [1, 2, 2]),
                              ("out-of-range shard", [1, 2, 4])):
            ci, contracts = self.copy_pair()
            ci["jobs"][RACE_JOB]["strategy"]["matrix"]["shard"] = values
            with self.subTest(mutation=label):
                with self.assertRaises(AssertionError):
                    assert_race_sharding_and_aggregate(ci, contracts)
        extra_dimension = self.copy_pair()
        extra_dimension[0]["jobs"][RACE_JOB]["strategy"]["matrix"]["os"] = ["ubuntu-latest"]
        with self.subTest(mutation="extra matrix dimension"):
            with self.assertRaises(AssertionError):
                assert_race_sharding_and_aggregate(*extra_dimension)

    def test_negative_control_missing_artifact_or_union_turns_check_red(self):
        for mutation in ("upload", "artifact-name", "artifact-path", "no-files",
                         "download", "union"):
            ci, contracts = self.copy_pair()
            race_steps = ci["jobs"][RACE_JOB]["steps"]
            aggregate_steps = ci["jobs"][RACE_AGGREGATE_JOB]["steps"]
            if mutation == "upload":
                ci["jobs"][RACE_JOB]["steps"] = [
                    step for step in race_steps
                    if not str(step.get("uses", "")).startswith("actions/upload-artifact")]
            elif mutation in ("artifact-name", "artifact-path", "no-files"):
                upload = [step for step in race_steps
                          if str(step.get("uses", "")).startswith("actions/upload-artifact")][0]
                if mutation == "artifact-name":
                    upload["with"]["name"] = "race-shard"
                elif mutation == "artifact-path":
                    upload["with"]["path"] = ""
                else:
                    upload["with"]["if-no-files-found"] = "warn"
            elif mutation == "download":
                ci["jobs"][RACE_AGGREGATE_JOB]["steps"] = [
                    step for step in aggregate_steps
                    if not str(step.get("uses", "")).startswith("actions/download-artifact")]
            else:
                ci["jobs"][RACE_AGGREGATE_JOB]["steps"] = [
                    step for step in aggregate_steps
                    if step.get("name") != "Check race shard package union"]
            with self.subTest(mutation=mutation):
                with self.assertRaises(AssertionError):
                    assert_race_sharding_and_aggregate(ci, contracts)

    def test_aggregate_result_command_rejects_every_non_success_value(self):
        aggregate = self.ci["jobs"][RACE_AGGREGATE_JOB]
        command = named_steps(aggregate, "Check race shard results",
                              RACE_AGGREGATE_JOB)["run"]
        for result in ("success", "failure", "cancelled", "skipped", ""):
            proc = subprocess.run(
                ["bash", "-c", command], env={"RACE_RESULT": result},
                capture_output=True, text=True)
            with self.subTest(result=result):
                if result == "success":
                    self.assertEqual(proc.returncode, 0, proc.stderr)
                else:
                    self.assertNotEqual(proc.returncode, 0)

    def test_negative_control_aggregate_fail_closed_turns_check_red(self):
        for mutation in ("always", "needs", "result", "continue-on-error",
                         "step-if", "step-continue-on-error",
                         "shard-step-continue-on-error", "extra-runner"):
            ci, contracts = self.copy_pair()
            aggregate = ci["jobs"][RACE_AGGREGATE_JOB]
            if mutation == "always":
                aggregate.pop("if")
            elif mutation == "needs":
                aggregate["needs"] = []
            elif mutation == "result":
                named_steps(aggregate, "Check race shard results", RACE_AGGREGATE_JOB)["run"] = "true"
            elif mutation == "continue-on-error":
                aggregate["continue-on-error"] = True
            elif mutation == "step-if":
                named_steps(aggregate, "Check race shard package union",
                            RACE_AGGREGATE_JOB)["if"] = "always()"
            elif mutation == "step-continue-on-error":
                named_steps(aggregate, "Check race shard package union",
                            RACE_AGGREGATE_JOB)["continue-on-error"] = True
            elif mutation == "shard-step-continue-on-error":
                named_steps(ci["jobs"][RACE_JOB], RACE_MAIN_STEP, RACE_JOB)[
                    "continue-on-error"] = True
            else:
                aggregate["steps"].append({
                    "name": "Second race runner",
                    "run": './scripts/gates/race_check.sh --shard 1',
                })
            with self.subTest(mutation=mutation):
                with self.assertRaises(AssertionError):
                    assert_race_sharding_and_aggregate(ci, contracts)

    def test_negative_control_duplicate_or_migrated_consumer_turns_check_red(self):
        for mutation in ("duplicate", "reordered-duplicate", "migrated", "missing-skip"):
            ci, contracts = self.copy_pair()
            if mutation in ("duplicate", "reordered-duplicate"):
                command = RACE_CONSUMER_COMMAND
                if mutation == "reordered-duplicate":
                    command = command.replace("-race -count=1", "-count=1 -race")
                ci["jobs"][RACE_JOB]["steps"].append({
                    "name": "Duplicate consumer race",
                    "run": command,
                })
            elif mutation == "migrated":
                race_steps = ci["jobs"][RACE_JOB]["steps"]
                consumer = named_steps(ci["jobs"][RACE_JOB],
                                       RACE_CONSUMER_STEP, RACE_JOB)
                race_steps.remove(consumer)
                ci["jobs"][RACE_AGGREGATE_JOB]["steps"].append(consumer)
            else:
                named_steps(ci["jobs"][RACE_JOB], RACE_MAIN_STEP, RACE_JOB)["env"].pop("GOFLAGS")
            with self.subTest(mutation=mutation):
                with self.assertRaises(AssertionError):
                    assert_race_sharding_and_aggregate(ci, contracts)

    def test_negative_control_duplicate_wechat_race_turns_check_red(self):
        ci, contracts = self.copy_pair()
        ci["jobs"][RACE_JOB]["steps"].append({
            "name": RACE_WECHAT_STEP,
            "run": RACE_WECHAT_COMMAND,
        })
        with self.assertRaises(AssertionError):
            assert_race_sharding_and_aggregate(ci, contracts)


class TestGoCacheCoversThreeModules(unittest.TestCase):
    """Every Go job shares a cache keyed by all three independent modules."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = load_workflow(CI_WORKFLOW)

    def test_all_go_jobs_cache_with_three_module_checksums(self):
        assert_go_cache_covers_all_modules(self.workflow)

    def test_negative_control_stripped_module_checksum_turns_check_red(self):
        for missing in GO_CACHE_DEPENDENCY_PATHS:
            mutated = copy.deepcopy(self.workflow)
            for _job_name, setup in go_setup_steps(mutated):
                dependency_paths = setup["with"]["cache-dependency-path"]
                setup["with"]["cache-dependency-path"] = "\n".join(
                    line for line in dependency_paths.splitlines()
                    if line.strip() != missing)
            with self.subTest(missing=missing):
                with self.assertRaisesRegex(AssertionError, re.escape(missing)):
                    assert_go_cache_covers_all_modules(mutated)


if __name__ == "__main__":
    unittest.main()
