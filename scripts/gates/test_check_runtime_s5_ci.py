#!/usr/bin/env python3
"""Contract anchors for the 05 domain's CI changes (runtime-architecture-convergence).

05/5.7 race-gate coverage: the main spec requires the race job to cover every
first-party package of the root module (including the root entry `.`) and the
wechat-bot module, with the package list fully registered.  This module pins
that promise against golden/runtime/architecture.json instead of a hand-copied
subset: adding a first-party package to the architecture registry without a
covering race pattern turns this check red, and shrinking the set to the old
host/history subset is exercised as a negative control that must fail.

The spec text itself (openspec/specs/race-gate-coverage) is owned by the
orchestrator; this anchor only tests the CI surface that the spec constrains.
"""
import copy
import json
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[2]
CI_WORKFLOW = REPO / ".github" / "workflows" / "ci.yml"
RACE_SCRIPT = REPO / "scripts" / "gates" / "race_check.sh"
ARCHITECTURE = REPO / "golden" / "runtime" / "architecture.json"
WECHAT_MODULE = REPO / "examples" / "wechat-bot"


def load_workflow():
    return yaml.safe_load(CI_WORKFLOW.read_text(encoding="utf-8"))


def job_steps(job):
    steps = job.get("steps") or []
    if not isinstance(steps, list):
        raise AssertionError("job steps must be a list")
    return steps


def race_job(workflow):
    jobs = workflow.get("jobs") or {}
    job = jobs.get("race")
    if not isinstance(job, dict):
        raise AssertionError("ci.yml has no race job")
    return job


def race_command(workflow):
    runs = [step.get("run") or "" for step in job_steps(race_job(workflow))
            if "race_check.sh" in (step.get("run") or "")]
    if len(runs) != 1:
        raise AssertionError(
            "the race job must invoke race_check.sh exactly once for the root "
            "module, found {}".format(len(runs)))
    return runs[0]


def declared_patterns(command):
    """The package arguments after race_check.sh, never shell operators."""
    tokens = command.split()
    positions = [index for index, token in enumerate(tokens)
                 if token.endswith("race_check.sh")]
    if len(positions) != 1:
        raise AssertionError("the race command must name race_check.sh exactly once")
    patterns = tokens[positions[0] + 1:]
    if not patterns:
        raise AssertionError("the race command declares no package at all")
    for pattern in patterns:
        if not pattern.startswith("."):
            raise AssertionError(
                "race package argument {!r} is not a package pattern (shell "
                "operators or flags must not be mistaken for packages)".format(pattern))
    return patterns


def covers(token, package):
    """Whether one package pattern covers one first-party package name."""
    if token == ".":
        return package == "."
    relative = token[2:]
    if relative.endswith("/..."):
        base = relative[:-4]
        return package == base or package.startswith(base + "/")
    return package == relative


def first_party_packages():
    document = json.loads(ARCHITECTURE.read_text(encoding="utf-8"))
    packages = [entry.get("pattern") for entry in document.get("packages") or []
                if isinstance(entry, dict)]
    if not packages or not all(isinstance(name, str) and name for name in packages):
        raise AssertionError("golden/runtime/architecture.json registers no first-party packages")
    return packages


def assert_race_gate_covers_first_party(workflow, packages):
    patterns = declared_patterns(race_command(workflow))
    missing = [package for package in packages
               if not any(covers(token, package) for token in patterns)]
    if missing:
        raise AssertionError(
            "the race job does not cover first-party package(s) {}: the main spec "
            "requires every registered package (subpackages included), not the "
            "modified-batch subset".format(missing))
    if "." not in patterns:
        raise AssertionError(
            "the race job must name the root entry '.' explicitly: the spec "
            "requires the root package even though race_check.sh skips it with a "
            "printed note while it carries no Go files")


WECHAT_COMMAND_PREFIX = "go -C examples/wechat-bot test"


def wechat_race_step(workflow):
    steps = [step for step in job_steps(race_job(workflow))
             if WECHAT_COMMAND_PREFIX in (step.get("run") or "")]
    if len(steps) != 1:
        raise AssertionError(
            "the race job must run the wechat-bot module under race exactly "
            "once (the independent module is not reached by the root module's "
            "./...), found {}".format(len(steps)))
    return steps[0]


def assert_wechat_module_raced(workflow):
    step = wechat_race_step(workflow)
    run = step.get("run") or ""
    tokens = run.split()
    flags = {token for token in tokens if token.startswith("-")}
    for flag in ("-race", "-p"):
        if flag not in flags:
            raise AssertionError(
                "the wechat race step must run the same race flags as the root "
                "gate ({!r} is missing): {!r}".format(flag, run))
    counts = [token for token in tokens if token.startswith("-count")]
    if counts != ["-count=1"]:
        raise AssertionError(
            "the wechat race step must run one independent process (-count=1), "
            "never a count-amplified loop: {!r}".format(run))
    tests = sorted(WECHAT_MODULE.glob("*_test.go"))
    if not tests:
        raise AssertionError(
            "examples/wechat-bot carries no *_test.go: the race run would prove "
            "nothing about the module's concurrency surface")
    if not any(token.endswith("/...") or token == "." for token in tokens):
        raise AssertionError(
            "the wechat race step must select its packages with a recursive "
            "pattern such as ./...: {!r}".format(run))


class TestRaceGateCoversEveryFirstPartyPackage(unittest.TestCase):
    """05/5.7: every first-party package and the wechat module run under race."""

    @classmethod
    def setUpClass(cls):
        cls.workflow = load_workflow()
        cls.packages = first_party_packages()

    def test_every_first_party_package_is_in_the_race_set(self):
        assert_race_gate_covers_first_party(self.workflow, self.packages)

    def test_wechat_module_runs_through_the_same_race_gate(self):
        assert_wechat_module_raced(self.workflow)

    def test_race_gate_classifier_is_installed_and_not_vacuous(self):
        script = RACE_SCRIPT.read_text(encoding="utf-8")
        for needle in ("go test -race -count=1 -p 1",
                       "no package in the declared set carries Go files",
                       "race_check: FAIL"):
            if needle not in script:
                raise AssertionError(
                    "race_check.sh lost {!r}: the gate could pass vacuously or "
                    "report success for a failed command".format(needle))
        patterns = declared_patterns(race_command(self.workflow))
        self.assertGreater(
            len([pattern for pattern in patterns if pattern != "."]), 0,
            "a set that contains only the module root would prove nothing")

    def test_negative_control_shrinking_the_set_turns_the_check_red(self):
        # The historical shrunken set (host/history only) must not satisfy the
        # contract; neither may a set missing one registered package.
        for label, mutate in (
                ("host/history only", lambda workflow: set_race_run(
                    workflow, "./scripts/gates/race_check.sh ./tests/integration/host/... "
                               "./tests/integration/history/...")),
                ("assembly dropped", lambda workflow: set_race_run(
                    workflow, race_command(workflow).replace(" ./assembly/...", ""))),
                ("no root entry", lambda workflow: set_race_run(
                    workflow, " ".join(token for token in race_command(workflow).split()
                                       if token != "."))),
        ):
            with self.subTest(mutation=label):
                mutated = copy.deepcopy(self.workflow)
                mutate(mutated)
                with self.assertRaises(AssertionError):
                    assert_race_gate_covers_first_party(mutated, self.packages)

    def test_negative_control_dropping_or_hollowing_the_wechat_run_turns_red(self):
        for label, mutate in (
                ("wechat step removed", lambda workflow: remove_wechat_step(workflow)),
                ("wechat command hollowed", lambda workflow: hollow_wechat_step(workflow)),
        ):
            with self.subTest(mutation=label):
                mutated = copy.deepcopy(self.workflow)
                mutate(mutated)
                with self.assertRaises(AssertionError):
                    assert_wechat_module_raced(mutated)


def set_race_run(workflow, run):
    step = next(step for step in job_steps(race_job(workflow))
                if "race_check.sh" in (step.get("run") or ""))
    step["run"] = run


def remove_wechat_step(workflow):
    job = race_job(workflow)
    job["steps"] = [step for step in job_steps(job) if step is not wechat_race_step(workflow)]


def hollow_wechat_step(workflow):
    wechat_race_step(workflow)["run"] = "echo skipped for speed"


if __name__ == "__main__":
    unittest.main()
