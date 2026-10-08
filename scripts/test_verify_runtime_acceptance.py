#!/usr/bin/env python3
"""test_verify_runtime_acceptance.py — D17 真实验收核账器（F11 工具）的回归。

核账器的存在意义是"人读一屏输出会误判"，所以它的测试重点不在 happy path，而在
**它必须拒绝的那些情形**：skip 冒充 pass、缺失当通过、manifest 说写了 0 条也叫完整、
快照行数与 digest 对不上、分组跨越 train/test、拒绝清单缺 reason、账本自相矛盾。

数据集一侧用真实的 scripts/convert_trajectories.run_strict 双流产物（fixture 形状与
test_convert_trajectories.py 共用，即与 rl/ 写出形状同源），其余按手工文件驱动。
不执行 go test：go test -json 的输入用逐行 Action 记录手工构造。

运行: python3 -m unittest discover -s scripts -p test_verify_runtime_acceptance.py -v
"""
from __future__ import annotations

import json
import os
import contextlib
import io
import tempfile
import random
import unittest

import convert_trajectories as ct
import verify_runtime_acceptance as v
from test_convert_trajectories import (FakeTokenizer, capture_record, fact_for,
                                        feedback_for, sealed_manifest, _rmtree)

PACKAGE = "github.com/SpellingDragon/tagent/rl"
REQUIRED = ["TestRealModel_Chat", "TestRealModel_Tools"]


def dump(row: dict) -> dict:
    return {k: v_ for k, v_ in row.items() if not k.startswith("_")}


def write_jsonl(path: str, rows: list) -> str:
    with open(path, "w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(dump(row), ensure_ascii=False) + "\n")
    return path


def write_json(path: str, doc) -> str:
    with open(path, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, sort_keys=True)
    return path


def read_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def go_line(action: str, test=None, output=None, pkg=PACKAGE) -> dict:
    row = {"Time": "2026-10-08T00:00:00Z", "Action": action, "Package": pkg}
    if test is not None:
        row["Test"] = test
    if output is not None:
        row["Output"] = output
    return row


def go_stream(pass_tests=(), fail_tests=(), skip_tests=(), extra=()) -> list:
    """A realistic `go test -json` sequence for a package run.

    Every test gets run → output → verdict lines, because that is what the go command
    emits; the parser may only conclude from the Action field, never from Output.
    """
    rows = [go_line("start"), go_line("run", "TestUnitThing"),
            go_line("output", "TestUnitThing", "    thing_test.go:12: ok"),
            go_line("pass", "TestUnitThing")]
    for name in pass_tests:
        rows += [go_line("run", name),
                 go_line("output", name, "=== RUN   %s" % name),
                 go_line("output", name, "--- PASS: %s (2.10s)" % name),
                 go_line("pass", name)]
    for name, sub in [(n, "%s/case" % n) for n in skip_tests]:
        rows += [go_line("run", sub),
                 go_line("output", sub, "    %s: SKIP: model not configured" % sub),
                 go_line("skip", sub),
                 go_line("run", name), go_line("skip", name)]
    for name in fail_tests:
        rows += [go_line("run", name),
                 go_line("output", name, "--- FAIL: %s (0.50s)" % name),
                 go_line("fail", name)]
    rows += list(extra)
    rows += [go_line("output", None, "PASS"), go_line("pass", None)]
    return rows


def sample_row(sample_id="runA:call-1", ns="7", root="root-A", split="train",
               mask=None, ids=None, labels=None) -> dict:
    """One dataset row in the strict converter's own field shape."""
    ids = ids if ids is not None else [11, 12, 13, 14, 15]
    mask = mask if mask is not None else [0, 0, 0, 1, 1]
    if labels is None:
        labels = [i if m == 1 else ct.LOSS_IGNORE_INDEX for i, m in zip(ids, mask)]
    return {"sample_id": sample_id, "schema_version": 2, "run_id": "runA",
            "source_call_ids": [sample_id.split(":")[-1]], "source_event_keys": ["1f4"],
            "capture_namespace": ns, "root_session_id": root, "split": split,
            "input_ids": ids, "attention_mask": [1] * len(ids), "loss_mask": mask,
            "labels": labels, "feedback": [{"verdict": "unrated"}],
            "feedback_source": "unrated",
            "quality": {flag: False for flag in ct.QUALITY_FLAGS}}


class Workspace(object):
    """A real two-stream conversion plus the artefacts a W4 acceptance run leaves behind."""

    def __init__(self, case: unittest.TestCase, roots=("root-A", "root-B", "root-C", "root-D"),
                 records_per_root=2, go_rows=None, expect=REQUIRED, with_facts_manifest=True,
                 train_ratio=0.5, split_seed=0, feedback=True):
        self.base = tempfile.mkdtemp(prefix="c5-accept-")
        case.addCleanup(self._cleanup)
        self.expect = list(expect)

        self.capture_dir = os.path.join(self.base, "capture")
        os.mkdir(self.capture_dir)
        records = []
        for ri, root in enumerate(roots):
            for ci in range(records_per_root):
                records.append(capture_record(call_id="%s-%d" % (root, ci), root=root, ns="7",
                                              run_id="runA", line=ri * records_per_root + ci + 1))
        self.records = records
        write_jsonl(os.path.join(self.capture_dir, "sess-root.jsonl"), records)

        facts = []
        for rec in records:
            facts.append(fact_for(rec))
            if feedback:
                facts.append(feedback_for(rec, verdict="good", note="useful"))
        self.facts = os.path.join(self.base, "facts.jsonl")
        write_jsonl(self.facts, facts)

        self.manifest = os.path.join(self.base, "capture-manifest.json")
        write_json(self.manifest, dump(sealed_manifest("runA", written=len(records))))

        self.export_manifest = os.path.join(self.base, "export-manifest.json")
        if with_facts_manifest:
            self.write_export_manifest()

        self.gojson = os.path.join(self.base, "go.jsonl")
        rows = go_rows if go_rows is not None else go_stream(pass_tests=self.expect)
        write_jsonl(self.gojson, rows)

        self.dataset = os.path.join(self.base, "dataset")
        self.run_kwargs = {"train_ratio": train_ratio, "split_seed": split_seed}

    def _cleanup(self):
        import shutil
        shutil.rmtree(self.base, ignore_errors=True)

    def write_export_manifest(self, **overrides):
        import hashlib
        with open(self.facts, "rb") as f:
            digest = hashlib.sha256(f.read()).hexdigest()
        doc = {"generated_at": 1762500002000, "cutoff": 1762500002000, "partitions": [7],
               "page_limit": 200, "events_written": len(self.fact_rows()), "forbidden": 0,
               "filtered": 0, "parent_key_missing": 0, "feedback_total": 0, "join_bound": 0,
               "join_missing": 0, "join_expired_or_missing": 0, "join_forbidden_parent": 0,
               "join_ambiguous": 0, "read_errors": 0, "pages": 1,
               "source_sha256": digest, "complete": True, "errs": 0}
        doc.update(overrides)
        return write_json(self.export_manifest, doc)

    def fact_rows(self):
        rows = []
        with open(self.facts, "r", encoding="utf-8") as f:
            for line in f:
                if line.strip():
                    rows.append(json.loads(line))
        return rows

    def convert(self):
        """Run the REAL strict converter over both streams into self.dataset."""
        import argparse
        if os.path.isdir(self.dataset):
            import shutil
            shutil.rmtree(self.dataset)
        args = argparse.Namespace(input=self.capture_dir, events=self.facts,
                                  manifest=self.manifest, output=self.dataset,
                                  tokenizer="fake/tokenizer", format="jsonl",
                                  content_policy="synthetic", redact_rules=None,
                                  allow_tools_free=False, **self.run_kwargs)
        return ct.run_strict(args, tokenizer=FakeTokenizer())

    def report(self, **overrides):
        if "dataset_dir" not in overrides and not os.path.isdir(self.dataset):
            self.convert()
        kwargs = {"gojson": self.gojson, "capture_dir": self.capture_dir, "facts": self.facts,
                  "manifest": self.manifest, "dataset_dir": self.dataset,
                  "expected_tests": self.expect, "facts_manifest": self.export_manifest,
                  "samples_per_split": 8, "seed": 0}
        kwargs.update(overrides)
        return v.verify(**kwargs)


def statuses(report: dict) -> dict:
    return {entry["name"]: entry["status"] for entry in report["checks"]}


def details(report: dict) -> dict:
    return {entry["name"]: entry["detail"] for entry in report["checks"]}


def evidence(report: dict, name: str) -> dict:
    for entry in report["checks"]:
        if entry["name"] == name:
            return entry["evidence"]
    raise AssertionError("no such check: %s (have %s)" % (name, sorted(statuses(report))))


def failures(report: dict) -> list:
    return sorted(report["failed_checks"])


class TestCleanRun(unittest.TestCase):
    def test_a_real_two_stream_run_passes_every_check(self):
        ws = Workspace(self)
        report = ws.report()
        self.assertTrue(report["ok"], failures(report))
        self.assertEqual(failures(report), [])
        self.assertGreaterEqual(report["counts"]["checks"], 15)
        for name, status in statuses(report).items():
            self.assertEqual(status, "pass", name)

    def test_dataset_and_ledger_numbers_are_the_converters_own(self):
        ws = Workspace(self)
        report = ws.report()
        manifest = read_json(os.path.join(ws.dataset, "conversion_manifest.json"))
        scanned = v.scan_dataset(ws.dataset, 8, 0)
        total = sum(entry["total"] for entry in scanned["splits"].values())
        self.assertEqual(total, manifest["stats"]["accepted"])
        self.assertEqual(report["counts"]["dataset_splits"],
                         {k: v["total"] for k, v in scanned["splits"].items()})
        self.assertEqual(evidence(report, "facts_snapshot_exists")["lines"], len(ws.fact_rows()))
        self.assertEqual(evidence(report, "capture_has_records")["written_total"],
                         len(ws.records))

    def test_feedback_bound_through_the_parent_reaches_the_dataset_used_by_the_checks(self):
        ws = Workspace(self)
        self.assertEqual(ws.convert(), 0)
        rows = [json.loads(line) for line in
                open(os.path.join(ws.dataset, "sft_train.jsonl"), encoding="utf-8")]
        self.assertTrue(rows)
        self.assertTrue(all(r["feedback"][0]["verdict"] == "good" for r in rows))
        self.assertTrue(all(r["feedback_source"] == "fact_index" for r in rows))

    def test_render_lists_every_check_and_the_verdict(self):
        ws = Workspace(self)
        text = v.render(ws.report())
        self.assertIn("runtime acceptance: PASS", text)
        for name in statuses(ws.report()):
            self.assertIn(name, text)


class TestGoJsonAccounting(unittest.TestCase):
    def test_skipped_required_test_is_a_failure_not_a_pass(self):
        ws = Workspace(self, go_rows=go_stream(pass_tests=["TestRealModel_Chat"],
                                              skip_tests=["TestRealModel_Tools"]))
        report = ws.report()
        self.assertFalse(report["ok"])
        self.assertIn("required_tests_not_skipped", failures(report))
        self.assertIn("TestRealModel_Tools", details(report)["required_tests_not_skipped"])
        # a SKIP inside the captured Output text is not how the skip was detected:
        # the verdict comes from Action, so the check name is the skip one only.
        self.assertIn("required_tests_pass", failures(report))

    def test_missing_required_test_is_reported_as_missing(self):
        ws = Workspace(self, go_rows=go_stream(pass_tests=["TestRealModel_Chat"]))
        report = ws.report()
        self.assertIn("required_tests_present", failures(report))
        self.assertIn("TestRealModel_Tools", evidence(report, "required_tests_present")["missing"])

    def test_failing_required_test_is_reported(self):
        ws = Workspace(self, go_rows=go_stream(pass_tests=["TestRealModel_Chat"],
                                              fail_tests=["TestRealModel_Tools"]))
        report = ws.report()
        self.assertIn("required_tests_pass", failures(report))
        self.assertIn("realmodel_surface_clean", failures(report))

    def test_output_text_is_never_read_as_a_verdict(self):
        rows = go_stream(pass_tests=["TestRealModel_Chat"])
        rows += [go_line("run", "TestRealModel_Tools"),
                 go_line("output", "TestRealModel_Tools", "--- PASS: TestRealModel_Tools (1.00s)"),
                 go_line("output", "TestRealModel_Tools", "panic: runtime error"),
                 go_line("fail", "TestRealModel_Tools")]
        ws = Workspace(self, go_rows=rows)
        report = ws.report()
        self.assertIn("required_tests_pass", failures(report))
        self.assertEqual(evidence(report, "required_tests_pass")["not_passed"]
                         ["TestRealModel_Tools"]["final"], "fail")

    def test_a_realmodel_test_that_fails_without_being_listed_is_still_a_failure(self):
        rows = go_stream(pass_tests=["TestRealModel_Chat", "TestRealModel_Tools"])
        rows += [go_line("run", "TestRealModel_Multimodal"),
                 go_line("fail", "TestRealModel_Multimodal")]
        ws = Workspace(self, go_rows=rows)
        report = ws.report()
        self.assertIn("realmodel_surface_clean", failures(report))
        self.assertIn("TestRealModel_Multimodal",
                      evidence(report, "realmodel_surface_clean")["dirty"])
        # the ledger also names the TestRealModel_* test that the required list forgot
        self.assertIn("TestRealModel_Multimodal",
                      evidence(report, "realmodel_surface_clean")["unlisted_in_expect_tests"])

    def test_subtest_skip_is_visible_under_its_parent(self):
        rows = go_stream(pass_tests=["TestRealModel_Chat"])
        rows += [go_line("run", "TestRealModel_Tools/tool_free"),
                 go_line("skip", "TestRealModel_Tools/tool_free"),
                 go_line("run", "TestRealModel_Tools"),
                 go_line("pass", "TestRealModel_Tools")]
        ws = Workspace(self, go_rows=rows)
        report = ws.report()
        self.assertIn("required_tests_not_skipped", failures(report))

    def test_package_level_failure_without_a_test_name_is_reported(self):
        rows = go_stream(pass_tests=["TestRealModel_Chat", "TestRealModel_Tools"])
        rows += [go_line("fail", None, output="build failed")]
        ws = Workspace(self, go_rows=rows)
        report = ws.report()
        self.assertIn("go_packages_pass", failures(report))

    def test_gojson_without_test_entries_fails_instead_of_looking_green(self):
        ws = Workspace(self, go_rows=[{"Action": "output", "Package": PACKAGE,
                                       "Output": "?   \tcoverage: 0.0% of statements\n"}])
        report = ws.report()
        self.assertIn("go_test_json_parsed", failures(report))
        self.assertFalse(report["ok"])

    def test_unreadable_gojson_is_a_failure_not_a_traceback(self):
        ws = Workspace(self)
        with open(ws.gojson, "w", encoding="utf-8") as f:
            f.write("not json at all\n")
        report = ws.report()
        self.assertEqual(statuses(report).get("go_test_json_parsed"), "fail")
        self.assertIn("no test entries", details(report)["go_test_json_parsed"])


# ---------------------------------------------------------------------------
# the other four surfaces: a hand-laid dataset isolates one assertion at a time
# ---------------------------------------------------------------------------

def write_manual_dataset(base: str, by_split: dict, rejections=(), stats=None,
                         with_manifest=True) -> str:
    """Lay out a dataset dir with exactly the numbers the caller asks for."""
    dirpath = os.path.join(base, "manual-dataset")
    if os.path.isdir(dirpath):
        import shutil
        shutil.rmtree(dirpath)
    os.makedirs(dirpath)
    total = 0
    counts = {}
    for split, rows in by_split.items():
        write_jsonl(os.path.join(dirpath, "sft_%s.jsonl" % split), rows)
        counts[split] = len(rows)
        total += len(rows)
    write_jsonl(os.path.join(dirpath, "rejected.jsonl"), list(rejections))
    if with_manifest:
        doc = {"conversion": "strict",
               "stats": stats if stats is not None else
               {"seen": total + len(rejections), "accepted": total, "rejected": len(rejections)},
               "split_counts": counts}
        write_json(os.path.join(dirpath, "conversion_manifest.json"), doc)
    return dirpath


class TestCaptureSealAccounting(unittest.TestCase):
    def setUp(self):
        self.ws = Workspace(self)
        self.assertEqual(self.ws.convert(), 0)

    def _remutate(self, **entry_overrides):
        doc = read_json(self.ws.manifest)
        doc["runs"]["runA"].update(entry_overrides)
        write_json(self.ws.manifest, doc)
        return self.ws.report()

    def test_intermediate_flush_is_not_a_seal(self):
        report = self._remutate(sealed=False)
        self.assertIn("capture_manifest_sealed", failures(report))
        self.assertIn("intermediate flush", details(report)["capture_manifest_sealed"])

    def test_unsynchronized_run_is_not_a_seal(self):
        report = self._remutate(synchronized=False)
        self.assertIn("capture_manifest_sealed", failures(report))
        self.assertIn("synchronized", details(report)["capture_manifest_sealed"])

    def test_dropped_records_void_completeness(self):
        report = self._remutate(dropped_disk_limit=3)
        self.assertIn("capture_manifest_sealed", failures(report))
        self.assertIn("dropped_disk_limit=3", details(report)["capture_manifest_sealed"])

    def test_zero_written_records_is_not_evidence(self):
        report = self._remutate(written=0)
        self.assertIn("capture_has_records", failures(report))
        self.assertEqual(evidence(report, "capture_has_records")["written_total"], 0)

    def test_top_level_incomplete_is_separately_visible(self):
        doc = read_json(self.ws.manifest)
        doc["complete"] = False
        write_json(self.ws.manifest, doc)
        report = self.ws.report()
        self.assertIn("capture_manifest_top_level", failures(report))

    def test_absent_manifest_fails_loudly(self):
        report = self.ws.report(manifest=os.path.join(self.ws.base, "nope.json"))
        self.assertIn("capture_manifest_readable", failures(report))
        self.assertIn("capture_ledger_claim", failures(report))

    def test_capture_directory_must_still_hold_what_the_seal_claims(self):
        report = self.ws.report(capture_dir=os.path.join(self.ws.base, "gone"))
        self.assertIn("capture_dir_holds_records", failures(report))
        self.assertIn("not found", details(report)["capture_dir_holds_records"])

    def test_a_shrunken_capture_directory_is_caught(self):
        target = os.path.join(self.ws.capture_dir, "sess-root.jsonl")
        with open(target, "w", encoding="utf-8") as f:
            f.write(json.dumps(dump(self.ws.records[0]), ensure_ascii=False) + "\n")
        report = self.ws.report()
        self.assertIn("capture_dir_holds_records", failures(report))
        self.assertEqual(evidence(report, "capture_dir_holds_records")["held_lines"], 1)
        self.assertEqual(evidence(report, "capture_dir_holds_records")["manifest_written"],
                         len(self.ws.records))

    def test_a_run_entry_without_a_written_counter_cannot_be_held_to(self):
        doc = read_json(self.ws.manifest)
        del doc["runs"]["runA"]["written"]
        write_json(self.ws.manifest, doc)
        report = self.ws.report()
        self.assertIn("capture_ledger_claim", failures(report))
        self.assertIn("capture_has_records", failures(report))

    def test_more_records_than_the_seal_claims_is_not_a_failure(self):
        # a capture dir accumulates across runs; only "dir < claim" is a contradiction
        extra = list(self.ws.records) + [capture_record(call_id="earlier-1", run_id="run0",
                                                        line=100)]
        write_jsonl(os.path.join(self.ws.capture_dir, "earlier.jsonl"), extra)
        report = self.ws.report()
        self.assertEqual(statuses(report)["capture_dir_holds_records"], "pass")

    def test_manifest_that_is_not_json_is_refused(self):
        with open(self.ws.manifest, "w", encoding="utf-8") as f:
            f.write("{oops")
        report = self.ws.report()
        self.assertIn("capture_manifest_readable", failures(report))


class TestFactsAccounting(unittest.TestCase):
    def setUp(self):
        self.ws = Workspace(self)
        self.assertEqual(self.ws.convert(), 0)

    def test_missing_export_manifest_is_a_failure_not_a_skip(self):
        report = self.ws.report(facts_manifest=None)
        self.assertIn("facts_manifest_complete", failures(report))
        self.assertIn("unevidenced", details(report)["facts_manifest_complete"])

    def test_an_unusable_export_manifest_keeps_every_tie_check_named(self):
        """The assertion set must not shrink when evidence is missing.

        The two export -> snapshot tie checks cannot be evaluated without a readable
        export manifest, but dropping them from the report would let a machine consumer
        read "absent" as "not applicable". They stay listed and fail as unevidenced, so
        the check-name set is identical whether or not the manifest was supplied.
        """
        without = self.ws.report(facts_manifest=None)
        with_ok = self.ws.report()
        self.assertEqual(sorted(entry["name"] for entry in without["checks"]),
                         sorted(entry["name"] for entry in with_ok["checks"]))
        for name in ("facts_manifest_matches_snapshot", "facts_digest_matches_snapshot"):
            self.assertEqual(statuses(without)[name], "fail", name)
            self.assertTrue(evidence(without, name)["unevidenced"], name)
        self.assertFalse(without["ok"])

    def test_an_unreadable_export_manifest_fails_the_tie_checks_too(self):
        with open(self.ws.export_manifest, "w", encoding="utf-8") as f:
            f.write("{oops")
        report = self.ws.report()
        self.assertIn("facts_manifest_complete", failures(report))
        self.assertIn("facts_digest_matches_snapshot", failures(report))

    def test_incomplete_export_is_a_failure(self):
        self.ws.write_export_manifest(complete=False, read_errors=2)
        report = self.ws.report()
        self.assertIn("facts_manifest_complete", failures(report))
        self.assertEqual(evidence(report, "facts_manifest_complete")["read_errors"], 2)

    def test_events_written_must_equal_the_snapshot_line_count(self):
        self.ws.write_export_manifest(events_written=len(self.ws.fact_rows()) + 40)
        report = self.ws.report()
        self.assertIn("facts_manifest_matches_snapshot", failures(report))
        block = evidence(report, "facts_manifest_matches_snapshot")
        self.assertEqual(block["events_written"], block["snapshot_lines"] + 40)

    def test_source_digest_ties_the_snapshot_to_the_manifest(self):
        with open(self.ws.facts, "a", encoding="utf-8") as f:
            f.write(json.dumps(dump(fact_for(capture_record(call_id="late-1")))) + "\n")
        report = self.ws.report()
        self.assertIn("facts_manifest_matches_snapshot", failures(report))
        self.assertIn("facts_digest_matches_snapshot", failures(report))

    def test_manifest_without_a_digest_cannot_be_verified(self):
        self.ws.write_export_manifest(source_sha256="")
        report = self.ws.report()
        self.assertIn("facts_digest_matches_snapshot", failures(report))
        self.assertIn("no source_sha256", details(report)["facts_digest_matches_snapshot"])

    def test_absent_snapshot_is_reported(self):
        report = self.ws.report(facts=os.path.join(self.ws.base, "no-facts.jsonl"),
                               facts_manifest=None)
        self.assertIn("facts_snapshot_exists", failures(report))

    def test_a_directory_of_snapshots_has_no_single_digest_and_says_so(self):
        snap_dir = os.path.join(self.ws.base, "snap")
        os.mkdir(snap_dir)
        rows = self.ws.fact_rows()
        write_jsonl(os.path.join(snap_dir, "part-0.jsonl"), rows[:len(rows) // 2])
        write_jsonl(os.path.join(snap_dir, "part-1.jsonl"), rows[len(rows) // 2:])
        report = self.ws.report(facts=snap_dir)
        self.assertEqual(statuses(report)["facts_snapshot_exists"], "pass")
        self.assertIn("facts_digest_matches_snapshot", failures(report))
        self.assertIn("directory", details(report)["facts_digest_matches_snapshot"])

    def test_join_columns_are_carried_into_the_evidence(self):
        self.ws.write_export_manifest(join_ambiguous=2, join_expired_or_missing=1)
        evidence_block = evidence(self.ws.report(), "facts_manifest_complete")
        self.assertEqual(evidence_block["join"]["join_ambiguous"], 2)
        self.assertEqual(evidence_block["join"]["join_expired_or_missing"], 1)


class TestDatasetAccounting(unittest.TestCase):
    def test_empty_dataset_dir_is_a_failure(self):
        ws = Workspace(self)
        empty = os.path.join(ws.base, "nothing")
        os.mkdir(empty)
        report = ws.report(dataset_dir=empty)
        self.assertIn("dataset_nonempty", failures(report))

    def test_a_sample_with_no_target_tokens_fails_the_shape_check(self):
        ws = Workspace(self)
        rows = [sample_row("runA:a", mask=[0] * 5), sample_row("runA:b", root="root-B")]
        manual = write_manual_dataset(ws.base, {"train": [rows[1]], "test": [rows[0]]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("sample_shapes_consistent", failures(report))
        self.assertIn("target token count is 0",
                      json.dumps(evidence(report, "sample_shapes_consistent")))

    def test_labels_must_be_supervised_exactly_where_the_mask_is_one(self):
        ws = Workspace(self)
        bad = sample_row("runA:bad", ids=[1, 2, 3, 4, 5], mask=[0, 0, 1, 1, 1],
                         labels=[ct.LOSS_IGNORE_INDEX, ct.LOSS_IGNORE_INDEX,
                                 ct.LOSS_IGNORE_INDEX, 4, 5])
        manual = write_manual_dataset(ws.base,
                                      {"train": [sample_row("runA:ok")], "test": [bad]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("sample_shapes_consistent", failures(report))
        self.assertIn("not supervised exactly where loss_mask==1",
                      json.dumps(evidence(report, "sample_shapes_consistent")))

    def test_mismatched_array_lengths_fail_the_shape_check(self):
        ws = Workspace(self)
        bad = sample_row("runA:bad", ids=[1, 2, 3], labels=[1, 2], mask=[0, 1])
        manual = write_manual_dataset(ws.base,
                                      {"train": [sample_row("runA:ok")], "test": [bad]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("shape mismatch", json.dumps(evidence(report, "sample_shapes_consistent")))

    def test_a_group_on_both_sides_of_the_split_is_a_leak(self):
        ws = Workspace(self)
        manual = write_manual_dataset(ws.base, {
            "train": [sample_row("runA:a", ns="7", root="root-A")],
            "test": [sample_row("runA:b", ns="7", root="root-A")]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("group_split_disjoint", failures(report))
        leaked = evidence(report, "group_split_disjoint")["leaked"]
        self.assertEqual(leaked, ["7\x1froot-A"])

    def test_rows_without_group_provenance_cannot_prove_disjointness(self):
        ws = Workspace(self)
        loose = sample_row("runA:a")
        del loose["root_session_id"]
        manual = write_manual_dataset(ws.base, {"train": [loose],
                                               "test": [sample_row("runA:b", root="root-B")]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("group_split_disjoint", failures(report))
        self.assertEqual(evidence(report, "group_split_disjoint")
                         ["rows_without_group_provenance"], 1)

    def test_a_single_split_dataset_does_not_prove_the_grouping(self):
        ws = Workspace(self)
        manual = write_manual_dataset(ws.base, {"train": [sample_row("runA:a")]})
        report = ws.report(dataset_dir=manual)
        self.assertIn("group_split_disjoint", failures(report))
        self.assertIn("both a train and a test split", details(report)["group_split_disjoint"])

    def test_disjoint_groups_pass(self):
        ws = Workspace(self)
        manual = write_manual_dataset(ws.base, {
            "train": [sample_row("runA:a", root="root-A"), sample_row("runA:b", root="root-A")],
            "test": [sample_row("runA:c", root="root-C")]})
        report = ws.report(dataset_dir=manual)
        self.assertEqual(statuses(report)["group_split_disjoint"], "pass")

    def test_arrow_only_dataset_is_counted_but_the_shapes_stay_unproven(self):
        ws = Workspace(self)
        hf = os.path.join(ws.base, "hf-dataset")
        os.makedirs(hf)
        write_json(os.path.join(hf, "dataset_info.json"),
                   {"splits": {"train": {"num_examples": 12}, "test": {"num_examples": 4}}})
        open(os.path.join(hf, "train-00000-of-00001.arrow"), "w").close()
        report = ws.report(dataset_dir=hf)
        self.assertEqual(statuses(report)["dataset_nonempty"], "pass")
        self.assertIn("sample_shapes_consistent", failures(report))
        self.assertIn("arrow", details(report)["sample_shapes_consistent"])
        self.assertEqual(evidence(report, "dataset_nonempty")["splits"],
                         {"train": 12, "test": 4})

    def test_sampling_is_deterministic_for_a_seed(self):
        ws = Workspace(self)
        rows = [sample_row("runA:%03d" % i, root="root-%d" % (i % 4)) for i in range(40)]
        manual = write_manual_dataset(ws.base, {"train": rows[:20], "test": rows[20:]})
        first = v.scan_dataset(manual, 5, 7)
        second = v.scan_dataset(manual, 5, 7)
        self.assertEqual([r["sample_id"] for r in first["splits"]["train"]["samples"]],
                         [r["sample_id"] for r in second["splits"]["train"]["samples"]])
        self.assertEqual(first["splits"]["train"]["total"], 20)
        self.assertEqual(len(first["splits"]["train"]["samples"]), 5)


class TestLedgerAccounting(unittest.TestCase):
    def setUp(self):
        self.ws = Workspace(self)
        self.assertEqual(self.ws.convert(), 0)

    def test_rejected_ledger_is_always_written_beside_the_samples(self):
        report = self.ws.report()
        self.assertEqual(statuses(report)["rejected_ledger_exists"], "pass")
        self.assertEqual(statuses(report)["rejected_ledger_lines_have_reason"], "pass")

    def test_missing_rejected_ledger_is_a_failure(self):
        os.unlink(os.path.join(self.ws.dataset, "rejected.jsonl"))
        report = self.ws.report()
        self.assertIn("rejected_ledger_exists", failures(report))

    def test_every_rejection_line_must_carry_a_reason(self):
        path = os.path.join(self.ws.dataset, "rejected.jsonl")
        with open(path, "w", encoding="utf-8") as f:
            f.write(json.dumps({"source": "sess-root.jsonl:1", "call_id": "x"}) + "\n")
            f.write(json.dumps({"reason": "not_v2_schema"}) + "\n")
        report = self.ws.report()
        self.assertIn("rejected_ledger_lines_have_reason", failures(report))
        self.assertEqual(len(evidence(report, "rejected_ledger_lines_have_reason")["malformed"]), 2)

    def test_reason_counts_are_reported_per_reason(self):
        rows = [{"source": "sess-root.jsonl:%d" % i, "call_id": "c%d" % i,
                 "reason": ct.REASON_NOT_V2 if i % 2 else ct.REASON_LEGACY_RECORD}
                for i in range(1, 5)]
        manual = write_manual_dataset(self.ws.base,
                                      {"train": [sample_row("runA:a")],
                                       "test": [sample_row("runA:b", root="root-B")]},
                                      rejections=rows)
        report = self.ws.report(dataset_dir=manual)
        self.assertEqual(evidence(report, "rejected_ledger_lines_have_reason")["by_reason"],
                         {"not_v2_schema": 2, "legacy_record": 2})

    def test_manifest_counts_must_match_the_written_dataset(self):
        path = os.path.join(self.ws.dataset, "conversion_manifest.json")
        doc = read_json(path)
        doc["stats"]["accepted"] += 3
        write_json(path, doc)
        report = self.ws.report()
        self.assertIn("conversion_ledger_matches_artifacts", failures(report))
        self.assertIn("accepted_vs_dataset",
                      evidence(report, "conversion_ledger_matches_artifacts")["problems"])

    def test_a_rejection_claimed_by_the_manifest_must_be_in_the_ledger(self):
        path = os.path.join(self.ws.dataset, "conversion_manifest.json")
        doc = read_json(path)
        doc["stats"]["rejected"] = 7
        doc["stats"]["seen"] += 7
        write_json(path, doc)
        report = self.ws.report()
        self.assertIn("conversion_ledger_matches_artifacts", failures(report))
        self.assertIn("rejected_vs_ledger",
                      evidence(report, "conversion_ledger_matches_artifacts")["problems"])

    def test_missing_conversion_manifest_cannot_be_reconciled(self):
        os.unlink(os.path.join(self.ws.dataset, "conversion_manifest.json"))
        report = self.ws.report()
        self.assertIn("conversion_ledger_matches_artifacts", failures(report))


class TestDatasetDictLayout(unittest.TestCase):

    def _dictdir(self):
        base = tempfile.mkdtemp(prefix="c5-dd-")
        self.addCleanup(_rmtree, base)
        for split, total in (("train", 3), ("test", 2)):
            sub = os.path.join(base, split)
            os.mkdir(sub)
            with open(os.path.join(sub, "dataset_info.json"), "w", encoding="utf-8") as f:
                json.dump({"splits": {"train": {"num_examples": total}}}, f)
        with open(os.path.join(base, "dataset_dict.json"), "w", encoding="utf-8") as f:
            f.write("{}\n")
        return base

    def test_dataset_dict_layout_is_recognised_and_counted(self):
        out = v.scan_dataset(self._dictdir(), samples_per_split=2, seed=0)
        self.assertEqual(out["format"], "hf")
        self.assertTrue(out["arrow_only"])
        self.assertEqual(out["splits"]["train"]["total"], 3)
        self.assertEqual(out["splits"]["test"]["total"], 2)
        self.assertIn("dataset_dict.json", out["row_count_source"])


class TestCLIContract(unittest.TestCase):
    def _argv(self, ws, *more):
        # main() checks its inputs exist, so the CLI path needs the real converter output.
        if not os.path.isdir(ws.dataset):
            self.assertEqual(ws.convert(), 0)
        return ["--gojson", ws.gojson, "--capture-dir", ws.capture_dir, "--facts", ws.facts,
                "--manifest", ws.manifest, "--facts-manifest", ws.export_manifest,
                "--dataset-dir", ws.dataset, "--expect-tests", ",".join(ws.expect)] + list(more)

    def test_clean_run_exits_zero_and_writes_the_machine_report(self):
        ws = Workspace(self)
        report_path = os.path.join(ws.base, "acceptance.json")
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = v.main(self._argv(ws, "--report", report_path))
        self.assertEqual(code, 0, out.getvalue())
        doc = read_json(report_path)
        self.assertTrue(doc["ok"])
        self.assertEqual(doc["schema"], v.SCHEMA)
        self.assertEqual(doc["failed_checks"], [])
        self.assertEqual(doc["counts"]["checks"], len(doc["checks"]))
        self.assertEqual(doc["inputs"]["expect_tests"], ws.expect)

    def test_broken_surface_exits_one_and_names_the_failed_checks(self):
        ws = Workspace(self, go_rows=go_stream(pass_tests=["TestRealModel_Chat"]))
        report_path = os.path.join(ws.base, "acceptance.json")
        with contextlib.redirect_stdout(io.StringIO()):
            code = v.main(self._argv(ws, "--report", report_path))
        self.assertEqual(code, 1)
        doc = read_json(report_path)
        self.assertFalse(doc["ok"])
        self.assertIn("required_tests_present", doc["failed_checks"])

    def test_report_is_written_under_a_missing_parent_directory(self):
        ws = Workspace(self)
        report_path = os.path.join(ws.base, "nested", "deeper", "acceptance.json")
        with contextlib.redirect_stdout(io.StringIO()):
            v.main(self._argv(ws, "--report", report_path))
        self.assertTrue(os.path.isfile(report_path))

    def test_absent_input_paths_are_reported_not_traced(self):
        ws = Workspace(self)
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            code = v.main(self._argv(ws, "--gojson", os.path.join(ws.base, "nope.jsonl")))
        self.assertEqual(code, 1)
        self.assertIn("--gojson path not found", err.getvalue())

    def test_empty_expect_list_is_a_cli_error(self):
        ws = Workspace(self)
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                v.main(self._argv(ws, "--expect-tests", " , "))
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--expect-tests", err.getvalue())

    def test_the_core_inputs_are_required(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as ctx:
                v.main(["--gojson", "go.jsonl", "--capture-dir", "cap", "--facts", "f.jsonl",
                        "--manifest", "m.json", "--expect-tests", "TestRealModel_Chat"])
        self.assertEqual(ctx.exception.code, 2)
        self.assertIn("--dataset-dir", err.getvalue())

    def test_the_verifier_never_executes_go_test(self):
        # W4 runs the tests; this tool only reads the ledger afterwards. Shelling out
        # here would let a verification tool change what it verifies.
        with open(v.__file__, "r", encoding="utf-8") as f:
            text = f.read()
        for forbidden in ("subprocess", "os.system", "Popen", "os.exec", "commands.getoutput"):
            self.assertNotIn(forbidden, text, forbidden)

    def test_only_the_standard_library_and_the_converter_are_imported(self):
        allowed = {"__future__", "argparse", "glob", "hashlib", "json", "os", "random", "sys",
                   "convert_trajectories"}
        names = set()
        with open(v.__file__, "r", encoding="utf-8") as f:
            for line in f:
                stripped = line.strip()
                if stripped.startswith("import "):
                    for piece in stripped[len("import "):].split(","):
                        names.add(piece.split()[0].split(".")[0])
                elif stripped.startswith("from ") and " import" in stripped:
                    names.add(stripped[len("from "):].split()[0].split(".")[0])
        self.assertTrue(names <= allowed, "unexpected import(s): %s" % sorted(names - allowed))
        self.assertIn("convert_trajectories", names)


if __name__ == "__main__":
    unittest.main(verbosity=2)
