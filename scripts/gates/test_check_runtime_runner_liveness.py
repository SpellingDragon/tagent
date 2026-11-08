#!/usr/bin/env python3
"""Selftests for the shard runner's liveness and durable-load-scan verdicts.

Two promises the 07 domain makes are proved here, both against the runner the CI
shards actually call (`scripts/scenarios/run_runtime_scenarios.py`):

  * a bound that fires still leaves test-log and evidence on disk, and names the
    deadline (`liveness.name = go-test-bound`, `state = expired`) instead of
    escaping as an exception that would leave the upload step with no files;
  * a shard's durable load scan is red when one of the five prefix lines is
    missing or when spec/consume/fact loaded zero keys, and stays green when only
    attempt/reject are zero — the difference between "the restart reloaded the
    chain" and "the restart ran over an empty chain".

The negative cases are the point: each rule is exercised against a fixture that
must fail, so a future edit cannot turn the rule into a comment.
"""
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "scripts" / "scenarios"))

import run_runtime_scenarios as runner  # noqa: E402

DATA_PREFIXES = ("tagent/task/v1/spec:", "tagent/task/v1/consume:", "tagent/task/v1/fact:")
ZERO_PREFIXES = ("tagent/task/v1/attempt:", "tagent/task/v1/reject:")

# The heartbeat a host generation writes every 30s while it is alive. It is
# liveness evidence for the log-stall verdict, not a durable scan read, so it
# must never enter the five-prefix counts.
HEARTBEAT_PREFIX = "tagent: calibration heartbeat "


def scan_line(prefix, keys, size=1024):
    return "tagent: durable load scan prefix={} keys={} bytes={} elapsed=12us".format(
        prefix, keys, size)


def heartbeat_line(gen=0, phase="rebind", steps=1):
    return "{}generation={} phase={} processed={} elapsed=5m0s".format(
        HEARTBEAT_PREFIX, gen, phase, steps)


def full_scan_log(data_keys=4, attempt_keys=0, reject_keys=0):
    """One generation log holding all five prefix lines."""
    lines = [scan_line(prefix, data_keys) for prefix in DATA_PREFIXES]
    lines.append(scan_line(ZERO_PREFIXES[0], attempt_keys))
    lines.append(scan_line(ZERO_PREFIXES[1], reject_keys))
    return "\n".join(lines) + "\n"


class DurableScanTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        os.makedirs(os.path.join(self.dir, "logs"))

    def tearDown(self):
        self.tmp.cleanup()

    def write_log(self, name, text):
        path = os.path.join(self.dir, "logs", name)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(text)
        return path

    def test_five_lines_with_data_attest_the_shard(self):
        self.write_log("gen00.log", full_scan_log())
        self.write_log("gen01.log", full_scan_log(data_keys=9))
        state = runner.durable_scan_attestation(self.dir)
        self.assertEqual(state["state"], "attested", state["reason"])
        self.assertEqual(sorted(state["log_files"]), ["gen00.log", "gen01.log"])
        # The deepest read any generation made wins, so an early empty generation
        # can never mask the chain a later restart actually loaded.
        self.assertEqual(state["prefixes"]["tagent/task/v1/spec:"]["keys"], 9)

    def test_zero_attempt_and_reject_keep_the_shard_green(self):
        # Negative control for the rule's own scope: a shard with no failed
        # attempt and no rejection is healthy, not empty.
        self.write_log("gen00.log", full_scan_log(attempt_keys=0, reject_keys=0))
        self.assertEqual(runner.durable_scan_attestation(self.dir)["state"], "attested")

    def test_heartbeat_lines_change_no_scan_count(self):
        # The two verdicts read different evidence: the stall verdict reads log
        # growth, the scan verdict reads the five prefix lines. Once a healthy
        # generation writes a heartbeat every 30s, the generation logs grow while
        # it is alive - so the scan counts and the per-prefix read tally must come
        # out identical to a log with no heartbeat at all.
        self.write_log("gen00.log", full_scan_log())
        before = runner.durable_scan_attestation(self.dir)
        self.write_log("gen00.log",
                       heartbeat_line(0, "boot", 0) + "\n"
                       + full_scan_log()
                       + heartbeat_line(0, "inputs", 7) + "\n")
        self.write_log("gen01.log", heartbeat_line(1, "rebind", 4) + "\n")
        after = runner.durable_scan_attestation(self.dir)
        self.assertEqual(after["state"], "attested", after["reason"])
        self.assertEqual(set(after["prefixes"]), set(runner.DURABLE_SCAN_PREFIXES))
        self.assertEqual(after["prefixes"], before["prefixes"])

    def test_missing_prefix_line_is_red_and_named(self):
        text = "\n".join(scan_line(prefix, 3) for prefix in DATA_PREFIXES
                         + ZERO_PREFIXES[:1]) + "\n"
        self.write_log("gen00.log", text)
        state = runner.durable_scan_attestation(self.dir)
        self.assertEqual(state["state"], "unattested")
        self.assertIn("tagent/task/v1/reject:", state["reason"])

    def test_zero_data_prefix_is_red_and_named(self):
        text = full_scan_log()
        text = text.replace(scan_line("tagent/task/v1/fact:", 4), scan_line("tagent/task/v1/fact:", 0))
        self.write_log("gen00.log", text)
        state = runner.durable_scan_attestation(self.dir)
        self.assertEqual(state["state"], "unattested")
        self.assertIn("tagent/task/v1/fact:", state["reason"])
        self.assertIn("zero keys", state["reason"])

    def test_missing_directory_and_empty_directory_are_red(self):
        missing = runner.durable_scan_attestation(os.path.join(self.dir, "absent"))
        self.assertEqual(missing["state"], "unattested")
        self.assertIn("no logs/ subdirectory", missing["reason"])
        empty = runner.durable_scan_attestation(self.dir)
        self.assertEqual(empty["state"], "unattested")
        self.assertIn("no .log file", empty["reason"])


class LivenessTest(unittest.TestCase):
    def test_run_go_test_returns_partial_output_and_names_the_bound(self):
        code, stdout, _stderr, liveness = runner.run_go_test(
            ["sh", "-c", "printf 'partial-line\\n'; exec sleep 30"], os.getcwd(), 0.3)
        self.assertEqual(liveness["name"], runner.LIVENESS_GO_TEST_BOUND)
        self.assertEqual(liveness["state"], "expired")
        self.assertEqual(code, runner.TIMEOUT_EXIT_CODE)
        self.assertIn("partial-line", stdout)

    def test_expired_bound_still_writes_test_log_and_evidence(self):
        """The consumer anchor: the files CI uploads exist after a kill."""
        with tempfile.TemporaryDirectory() as out_dir:
            index = {"07.1/TestFake": {
                "id": "07.1/TestFake", "module_path": "example.test/m",
                "dir": ".", "test_package": ".", "test_name": "TestFake",
                "subtests": [{"name": "anchor", "mandatory": True}],
            }}
            groups = [{"dir": ".", "module_path": "example.test/m", "tags": [],
                       "race": False, "packages": ["."], "tests": ["07.1/TestFake"]}]
            args = runner.parse_args(["--scope", "event-long"])
            real = runner.run_go_test

            def expired(command, root, timeout):
                return real(["sh", "-c", "printf '{\"Action\":\"run\"}\\n'; exec sleep 30"],
                            root, 0.3)

            runner.run_go_test = expired
            try:
                repeat = runner.run_event_repeat(
                    args, os.getcwd(), index, groups, ["07.1/TestFake"], 1, 256,
                    1, 1, out_dir, "sha256:" + "0" * 64, [])
            finally:
                runner.run_go_test = real

            repeat_dir = os.path.join(out_dir, "repeat-1")
            self.assertTrue(os.path.isfile(os.path.join(repeat_dir, "test-log.jsonl")),
                            "a fired bound must still leave the test log")
            evidence_path = os.path.join(repeat_dir, "evidence.json")
            self.assertTrue(os.path.isfile(evidence_path),
                            "a fired bound must still leave the evidence file")
            with open(evidence_path, encoding="utf-8") as handle:
                evidence = json.load(handle)
            self.assertEqual(evidence["liveness"]["state"], "expired")
            self.assertEqual(evidence["liveness"]["name"], runner.LIVENESS_GO_TEST_BOUND)
            self.assertEqual(evidence["go_test_exit_code"], runner.TIMEOUT_EXIT_CODE)
            self.assertEqual(evidence["verdict"], "unattested")
            self.assertEqual(repeat["verdict"], "unattested")

    def test_durable_scan_verdict_lands_in_the_evidence_file(self):
        """The same file the bundle checker reads carries the shard verdict."""
        with tempfile.TemporaryDirectory() as out_dir:
            repeat_dir = os.path.join(out_dir, "repeat-1")
            os.makedirs(repeat_dir)
            evidence_path = os.path.join(repeat_dir, "evidence.json")
            with open(evidence_path, "w", encoding="utf-8") as handle:
                json.dump({"verdict": "match"}, handle)
            repeat = {"evidence": evidence_path, "verdict": "match"}
            attestation = {"state": "unattested", "reason": "synthetic", "prefixes": {},
                           "log_files": [], "directory": "/tmp/none"}
            runner.attach_durable_scan(repeat, attestation)
            with open(evidence_path, encoding="utf-8") as handle:
                evidence = json.load(handle)
            self.assertEqual(evidence["durable_scan"]["reason"], "synthetic")
            self.assertEqual(repeat["verdict"], "unattested")


if __name__ == "__main__":
    unittest.main()
