#!/usr/bin/env python3
"""verify_runtime_acceptance.py — D17 真实本地验收的核账器（F11 工具，纯 stdlib）。

它**不执行 go test**，也**不生成任何数据**：它只核对 W4 已经跑完的真实产物之间是否
互相自洽。之所以要有这个工具，是因为人读一屏 go test 输出会得出"看起来都过了"的
结论，而验收要求的是账目级证据：

  1. go test -json 的 Action 字段 —— 必需用例真的 pass，不是 skip、不是缺失；
  2. capture 封账 manifest —— 每个 run 都 complete+sealed+synchronized 且写出的
     记录数 > 0（零记录的"完整"数据集不是证据）；
  3. facts 导出 manifest —— complete=true，且 events_written 与快照实际行数、
     source_sha256 与快照实际字节对得上；
  4. 数据集 —— samples 数 > 0，随机抽样的 labels/loss_mask 形状一致且目标 token 数 > 0；
  5. 分组 —— train ∩ test = ∅（同一 (capture_namespace, root_session_id) 组不得跨侧）；
  6. 拒绝清单 —— rejected.jsonl 存在且逐行含 reason；
  7. 转换账本 —— conversion_manifest.json 的 accepted/rejections 与落盘行数对得上。

任何一项拿不到证据就是 FAIL（不是 SKIP）：缺失的输入不能被当作通过的检查。
退出码 0 = 全部必需检查通过；1 = 有 FAIL。--report 机器可读结果。

运行:
  python3 scripts/verify_runtime_acceptance.py --gojson run.json \\
      --capture-dir var/trajectories --facts out/facts.jsonl \\
      --manifest out/capture-manifest.json --facts-manifest out/export-manifest.json \\
      --dataset-dir out/dataset --expect-tests TestRealModel_Chat,TestRealModel_Tools \\
      --report out/acceptance.json
"""
from __future__ import annotations

import argparse
import glob
import json
import os
import random
import sys

import convert_trajectories as ct

SCHEMA = "tagent-runtime-acceptance/v1"
REALMODEL_PREFIX = "TestRealModel_"
LOSS_FIELDS = ("input_ids", "labels", "loss_mask")
NON_SPLIT_FILES = ("rejected.jsonl",)  # the ledger sits beside the samples; it is not a split


def _int(value, default=None):
    """Read a counter without turning a genuine 0 into the default."""
    if value is None:
        return default
    try:
        return int(value)
    except (TypeError, ValueError):
        return default

PASS = "pass"
FAIL = "fail"


def check(name: str, ok: bool, detail: str, evidence=None) -> dict:
    return {"name": name, "status": PASS if ok else FAIL, "detail": detail,
            "evidence": evidence if evidence is not None else {}}


# ---------------------------------------------------------------------------
# 1. go test -json
# ---------------------------------------------------------------------------

def parse_go_json(path: str) -> dict:
    """Read `go test -json` output into {test_name: outcome}.

    Only the Action field is trusted for a verdict (run/pass/fail/skip). Output lines
    are counted, never interpreted — a "--- PASS" in captured text is not a result.
    """
    tests = {}
    lines = 0
    bad_lines = 0
    build_output = []
    package_fails = set()
    with open(path, "r", encoding="utf-8") as f:
        for raw in f:
            raw = raw.strip()
            if not raw:
                continue
            lines += 1
            try:
                row = json.loads(raw)
            except json.JSONDecodeError:
                bad_lines += 1
                build_output.append(raw[:200])
                continue
            if not isinstance(row, dict):
                bad_lines += 1
                continue
            action = str(row.get("Action") or "")
            name = row.get("Test")
            if name is None:
                if action == "fail":
                    package_fails.add(str(row.get("Package") or ""))
                if action in ("build-output", "output") and row.get("Output"):
                    build_output.append(str(row["Output"])[:200])
                continue
            entry = tests.setdefault(str(name), {"actions": [], "outputs": 0})
            entry["actions"].append(action)
            if action == "output":
                entry["outputs"] += 1

    outcomes = {}
    for name, entry in tests.items():
        actions = [a for a in entry["actions"] if a in ("run", "pass", "fail", "skip")]
        final = actions[-1] if actions else ""
        outcomes[name] = {
            "final": final,
            "passed": final == "pass",
            "skipped": "skip" in actions,
            "failed": final == "fail",
            "actions": actions,
            "outputs": entry["outputs"],
        }
    return {"tests": outcomes, "lines": lines, "bad_lines": bad_lines,
            "build_output": build_output[:20], "package_fails": sorted(package_fails)}


def _top_level(names, wanted: str) -> list:
    """One test name plus all of its subtests (Go spells them Parent/Child)."""
    return sorted(n for n in names if n == wanted or n.startswith(wanted + "/"))


def check_go_json(go: dict, expected: list) -> list:
    checks = []
    if not go["tests"]:
        checks.append(check("go_test_json_parsed", False,
                            "no test entries found in --gojson (empty or wrong format?)",
                            {"lines": go["lines"], "bad_lines": go["bad_lines"]}))
        return checks
    checks.append(check("go_test_json_parsed", True,
                        "parsed %d test entry result(s) from %d line(s)"
                        % (len(go["tests"]), go["lines"]),
                        {"lines": go["lines"], "bad_lines": go["bad_lines"]}))

    names = list(go["tests"])
    missing = {}
    not_passed = {}
    skipped = {}
    for wanted in expected:
        matched = _top_level(names, wanted)
        if not matched:
            missing[wanted] = "absent from the go test -json run"
            continue
        tops = [m for m in matched if m == wanted]
        target = tops[0] if tops else matched[0]
        outcome = go["tests"][target]
        # A required test is not accepted until IT AND ITS SUBTESTS pass: D19 forbids a
        # SKIP inside the required surface, and a failing subtest under a passing parent
        # is exactly how "we ran it" gets mistaken for "it works".
        skipped_subs = sorted(m for m in matched if go["tests"][m]["skipped"])
        failed_subs = sorted(m for m in matched if go["tests"][m]["final"] == "fail")
        if outcome["skipped"] or skipped_subs:
            skipped[wanted] = skipped_subs or [target]
        if not outcome["passed"] or failed_subs:
            not_passed[wanted] = {"final": outcome["final"], "failed_subtests": failed_subs,
                                 "subtests": len(matched) - 1}

    checks.append(check("required_tests_present", not missing,
                        "all %d required test(s) ran" % len(expected) if not missing
                        else "missing required test(s): %s" % ", ".join(sorted(missing)),
                        {"expected": len(expected), "missing": missing}))
    checks.append(check("required_tests_pass", not not_passed,
                        "no required test failed or was unfinished" if not not_passed
                        else "required test(s) not pass: %s" % json.dumps(not_passed, sort_keys=True),
                        {"not_passed": not_passed}))
    checks.append(check("required_tests_not_skipped", not skipped,
                        "no required test or subtest was skipped" if not skipped
                        else "skip is not acceptance (D19): %s" % json.dumps(skipped, sort_keys=True),
                        {"skipped": skipped}))

    # The real-model surface is the acceptance surface: anything TestRealModel_* that
    # ran but did not pass is a failure, whether or not it was listed as required. A
    # newly added real-model test cannot enter the ledger by being left out of --expect-tests.
    surface = {n: go["tests"][n] for n in names if n.startswith(REALMODEL_PREFIX)}
    dirty = {n: o["final"] for n, o in surface.items() if not o["passed"]}
    unlisted = sorted(n for n in surface
                      if not any(n == e or n.startswith(e + "/") for e in expected))
    checks.append(check("realmodel_surface_clean", not dirty,
                        "%d TestRealModel_* entr(y/ies) all pass" % len(surface) if not dirty
                        else "TestRealModel_* did not pass: %s" % json.dumps(dirty, sort_keys=True),
                        {"surface": len(surface), "dirty": dirty,
                         "unlisted_in_expect_tests": unlisted}))
    if go["package_fails"]:
        checks.append(check("go_packages_pass", False,
                            "package-level failure(s) without a test name: %s"
                            % ", ".join(go["package_fails"]),
                            {"build_output_tail": go["build_output"][-5:]}))
    return checks


# ---------------------------------------------------------------------------
# 2. capture seal manifest (rl.MarshalCaptureManifest shape: flat run entries)
# ---------------------------------------------------------------------------

def capture_written_total(manifest: dict) -> int:
    """The record count the seal claims: rl.CaptureStats.written, summed over runs.

    A key this reader does not have reads as zero, and a zero read as "nothing was
    written, so nothing is missing" is exactly how an absent ledger looks clean.
    """
    total = 0
    for entry in manifest["runs"].values():
        value = _int(entry.get("written"))
        if value is None:
            return -1
        total += value
    return total


def check_capture_manifest(path: str) -> list:
    try:
        manifest = ct.load_capture_manifest(path)
    except ct.StrictError as exc:
        return [check("capture_manifest_readable", False, str(exc)),
                check("capture_ledger_claim", False,
                      "no capture manifest, so no record count to hold the directory to")]
    runs = manifest["runs"]
    if not runs:
        return [check("capture_manifest_sealed", False,
                      "manifest has no run entries: nothing was sealed",
                      {"complete": manifest["complete"],
                       "synchronized": manifest["synchronized"]})]
    problems = {}
    written = {}
    for run_id, entry in runs.items():
        status, detail = ct._manifest_run_status(manifest, run_id)
        if status != "sealed":
            problems[run_id] = detail
        written[run_id] = _int(entry.get("written"), 0)
    checks = [check("capture_manifest_sealed", not problems,
                    "%d run(s) complete+sealed+synchronized, ledger quiesced" % len(runs)
                    if not problems else "not sealed: %s" % json.dumps(problems, sort_keys=True),
                    {"runs": sorted(runs), "problems": problems})]
    total = sum(written.values())
    claim = capture_written_total(manifest)
    checks.append(check("capture_ledger_claim", claim >= 0,
                        "the seal claims %d written record(s)" % claim if claim >= 0
                        else "a run entry carries no written counter: it cannot be checked "
                             "against the capture directory"))
    checks.append(check("capture_has_records", total > 0,
                        "capture wrote %d record(s)" % total if total > 0
                        else "sealed manifest with written==0: a zero-record run is not evidence",
                        {"written_per_run": written, "written_total": total}))
    checks.append(check("capture_manifest_top_level", bool(manifest["complete"]),
                        "top-level complete=%s synchronized=%s"
                        % (manifest["complete"], manifest["synchronized"]),
                        {"complete": manifest["complete"],
                         "synchronized": manifest["synchronized"]}))
    return checks


def check_capture_dir(path: str, written_total: int) -> list:
    """The seal says how many records were written; the capture dir must hold them.

    Only a one-sided assertion is legitimate here: a capture directory can hold more
    records than one manifest covers (append across runs), so "dir >= manifest" is the
    claim that catches a vanished/emptied directory without inventing an equality the
    writers never promised.
    """
    if not os.path.exists(path):
        return [check("capture_dir_holds_records", False,
                      "capture directory not found: %s" % path, {"path": path})]
    files = _facts_files(path)
    files = [f for f in files if os.path.isfile(f)]
    if not files:
        return [check("capture_dir_holds_records", False,
                      "no .jsonl capture file under --capture-dir %s" % path, {"path": path})]
    held = 0
    for f in files:
        held += len(read_lines(f))
    ok = held >= written_total and held > 0
    detail = ("capture dir holds %d line(s) across %d file(s), manifest claims %d written"
              % (held, len(files), written_total)) if ok else (
              "the seal claims %d written record(s) but the capture dir holds %d: "
              "the primary source is not where the manifest says it is"
              % (written_total, held) if held else
              "capture dir holds no records at all")
    return [check("capture_dir_holds_records", ok, detail,
                  {"held_lines": held, "files": [os.path.basename(f) for f in files],
                   "manifest_written": written_total})]


# ---------------------------------------------------------------------------
# 3. facts export manifest vs the snapshot itself
# ---------------------------------------------------------------------------

def read_lines(path: str) -> list:
    with open(path, "r", encoding="utf-8") as f:
        return [line for line in f if line.strip()]


def _facts_files(path: str) -> list:
    if os.path.isdir(path):
        return sorted(glob.glob(os.path.join(path, "*.jsonl")))
    return [path]


def _export_tie_unevidenced(detail: str) -> list:
    """The two export -> snapshot tie checks, reported as explicit failures.

    When the export manifest is missing or unreadable these checks cannot run, but they
    must NOT vanish from the report: a machine consumer that diffs check-name sets would
    read an absent entry as "not applicable" rather than "never verified". Naming them and
    failing them keeps the assertion set the same size for every input combination.
    """
    return [check("facts_manifest_matches_snapshot", False, detail, {"unevidenced": True}),
            check("facts_digest_matches_snapshot", False, detail, {"unevidenced": True})]


def check_facts(path: str, manifest_path, rows_min: int = 1) -> list:
    files = _facts_files(path)
    if not files or not all(os.path.isfile(f) for f in files):
        return [check("facts_snapshot_exists", False,
                      "no facts snapshot found under --facts: %s" % path, {"files": files})]
    lines = 0
    for f in files:
        lines += len(read_lines(f))
    checks = [check("facts_snapshot_exists", lines >= rows_min,
                    "%d fact line(s) in %d file(s)" % (lines, len(files)),
                    {"lines": lines, "files": [os.path.basename(f) for f in files]})]

    if not manifest_path:
        detail = ("--facts-manifest was not supplied: an unevidenced export is not "
                  "a pass (run the export with a manifest and point at it)")
        return checks + [check("facts_manifest_complete", False, detail,
                               {"events_written_in_snapshot": lines})] \
            + _export_tie_unevidenced(detail)
    if not os.path.isfile(manifest_path):
        detail = "--facts-manifest file not found: %s" % manifest_path
        return checks + [check("facts_manifest_complete", False, detail)] \
            + _export_tie_unevidenced(detail)
    with open(manifest_path, "r", encoding="utf-8") as f:
        raw = f.read()
    try:
        doc = json.loads(raw)
    except json.JSONDecodeError as exc:
        detail = "--facts-manifest is not valid JSON: %s" % exc
        return checks + [check("facts_manifest_complete", False, detail)] \
            + _export_tie_unevidenced(detail)
    if not isinstance(doc, dict):
        detail = "--facts-manifest is not a JSON object"
        return checks + [check("facts_manifest_complete", False, detail)] \
            + _export_tie_unevidenced(detail)

    complete = bool(doc.get("complete", False))
    written = _int(doc.get("events_written"), 0)
    join_columns = {k: doc.get(k) for k in ("feedback_total", "join_bound", "join_missing",
                                           "join_expired_or_missing", "join_forbidden_parent",
                                           "join_ambiguous") if k in doc}
    checks.append(check("facts_manifest_complete", complete,
                        "export manifest complete=%s (events_written=%d)" % (complete, written),
                        {"complete": complete, "events_written": written,
                         "forbidden": doc.get("forbidden"), "read_errors": doc.get("read_errors"),
                         "pages": doc.get("pages"), "join": join_columns}))
    if written != lines:
        checks.append(check("facts_manifest_matches_snapshot", False,
                            "manifest says events_written=%d but the snapshot holds %d line(s)"
                            % (written, lines),
                            {"events_written": written, "snapshot_lines": lines}))
    else:
        checks.append(check("facts_manifest_matches_snapshot", True,
                            "events_written=%d equals the snapshot line count" % written,
                            {"events_written": written, "snapshot_lines": lines}))

    digest = str(doc.get("source_sha256") or "")
    if not digest:
        checks.append(check("facts_digest_matches_snapshot", False,
                            "manifest carries no source_sha256: the snapshot is untied",
                            {"source_sha256": digest}))
    elif len(files) != 1:
        checks.append(check("facts_digest_matches_snapshot", False,
                            "source_sha256 covers a single writer stream, but --facts is a "
                            "directory of %d file(s); point --facts at the exact snapshot"
                            % len(files), {"files": len(files)}))
    else:
        import hashlib
        with open(files[0], "rb") as f:
            actual = hashlib.sha256(f.read()).hexdigest()
        checks.append(check("facts_digest_matches_snapshot", actual == digest,
                            "snapshot digest %s" % ("matches source_sha256" if actual == digest
                                                     else "does NOT match source_sha256"),
                            {"actual_sha256": actual, "manifest_sha256": digest}))
    return checks


# ---------------------------------------------------------------------------
# 4-7. the dataset itself
# ---------------------------------------------------------------------------

def scan_dataset(dataset_dir: str, samples_per_split: int, seed: int) -> dict:
    """Stream every split file, counting rows and reservoir-sampling a few.

    Streaming matters: an acceptance tool that loads a whole dataset into memory will
    be unusable on a real capture, and "unusable" turns into "skipped".
    """
    out = {"format": "jsonl", "splits": {}, "files": [], "arrow_only": False,
           "unreadable": [], "row_count_source": "sft_*.jsonl line counts"}
    jsonl_files = sorted(glob.glob(os.path.join(dataset_dir, "sft_*.jsonl")))
    source = "sft_<split>.jsonl"
    if not jsonl_files:
        # Not the strict spelling: take any other JSONL sample file, but never the
        # rejection ledger — counting it as a split would hide a missing dataset.
        jsonl_files = [f for f in sorted(glob.glob(os.path.join(dataset_dir, "*.jsonl")))
                       if os.path.basename(f) not in NON_SPLIT_FILES]
        source = "*.jsonl (no sft_ prefix found)"
    out["split_file_source"] = source
    if jsonl_files:
        rng = random.Random(seed)
        for path in jsonl_files:
            base = os.path.basename(path)
            split = base[len("sft_"):].split(".")[0] if base.startswith("sft_") \
                else os.path.splitext(base)[0]
            total, kept = _scan_jsonl(path, samples_per_split, rng)
            entry = out["splits"].setdefault(split, {"total": 0, "samples": [], "files": []})
            entry["total"] += total
            entry["samples"].extend(kept)
            entry["files"].append(base)
            out["files"].append(base)
        return out

    # No JSONL: accept the HF layouts for the COUNT only, and say so loudly where
    # the shape assertions would need decoded arrow bodies (this tool is stdlib-only).
    # Two on-disk shapes are recognised: a FLAT single Dataset (dataset_info.json at
    # the root) and a DatasetDict (dataset_dict.json at the root, one Dataset per
    # split subdirectory) — write_hf emits the flat shape for one-sided splits.
    dict_path = os.path.join(dataset_dir, "dataset_dict.json")
    if os.path.isfile(dict_path):
        out["format"] = "hf"
        out["arrow_only"] = True
        out["files"] = sorted(os.listdir(dataset_dir))
        counts = {}
        try:
            splits = sorted(
                name for name in os.listdir(dataset_dir)
                if os.path.isfile(os.path.join(dataset_dir, name, "dataset_info.json"))
            )
            for split in splits:
                with open(os.path.join(dataset_dir, split, "dataset_info.json"),
                          "r", encoding="utf-8") as f:
                    info = json.load(f)
                for name, meta in (info.get("splits") or {}).items():
                    counts[split] = counts.get(split, 0) + int(meta.get("num_examples", 0) or 0)
            out["row_count_source"] = "dataset_dict.json + split dataset_info.json splits.num_examples"
        except (json.JSONDecodeError, OSError) as exc:
            out["unreadable"].append("dataset_dict layout: %s" % exc)
        for split, total in counts.items():
            out["splits"][split] = {"total": total, "samples": [], "files": []}
        return out
    info_path = os.path.join(dataset_dir, "dataset_info.json")
    state_path = os.path.join(dataset_dir, "state.json")
    if os.path.isfile(info_path) or os.path.isfile(state_path):
        out["format"] = "hf"
        out["arrow_only"] = True
        out["files"] = sorted(os.listdir(dataset_dir))
        counts = {}
        source = ""
        if os.path.isfile(info_path):
            try:
                with open(info_path, "r", encoding="utf-8") as f:
                    info = json.load(f)
                for split, meta in (info.get("splits") or {}).items():
                    counts[split] = int(meta.get("num_examples", 0) or 0)
                source = "dataset_info.json splits.num_examples"
            except (json.JSONDecodeError, OSError) as exc:
                out["unreadable"].append("dataset_info.json: %s" % exc)
        if not counts and os.path.isfile(state_path):
            try:
                with open(state_path, "r", encoding="utf-8") as f:
                    state = json.load(f)
                for row in state.get("_data_files") or []:
                    split = str(row.get("split") or "train")
                    counts[split] = counts.get(split, 0) + 1
                # state.json lists files, not rows: that is not a sample count, so it is
                # named as what it is instead of being passed off as evidence.
                source = "state.json _data_files (file count, NOT a row count)"
            except (json.JSONDecodeError, OSError) as exc:
                out["unreadable"].append("state.json: %s" % exc)
        out["row_count_source"] = source
        for split, total in counts.items():
            out["splits"][split] = {"total": total, "samples": [], "files": []}
        return out
    out["format"] = "empty"
    return out


def _scan_jsonl(path: str, k: int, rng: random.Random) -> tuple:
    total = 0
    kept = []
    with open(path, "r", encoding="utf-8") as f:
        for line_num, line in enumerate(f, 1):
            if not line.strip():
                continue
            total += 1
            try:
                row = json.loads(line)
            except json.JSONDecodeError as exc:
                kept.append({"_parse_error": "%s:%d %s" % (os.path.basename(path), line_num, exc)})
                continue
            if len(kept) < k:
                kept.append(row)
            else:
                replace = rng.randrange(total)
                if replace < k:
                    kept[replace] = row
    return total, kept


def _shape_problems(row: dict) -> list:
    problems = []
    if row.get("_parse_error"):
        return ["unparseable dataset line: %s" % row["_parse_error"]]
    lengths = {}
    for field in LOSS_FIELDS:
        value = row.get(field)
        if not isinstance(value, list):
            problems.append("%s is missing or not a list" % field)
        else:
            lengths[field] = len(value)
    if len(set(lengths.values())) > 1:
        problems.append("shape mismatch: %s" % lengths)
    if lengths and 0 in lengths.values():
        problems.append("empty sample: %s" % lengths)
    mask = row.get("loss_mask")
    labels = row.get("labels")
    if isinstance(mask, list) and isinstance(labels, list) and len(mask) == len(labels):
        if any(v not in (0, 1) for v in mask):
            problems.append("loss_mask holds values other than 0/1")
        target = sum(1 for v in mask if v == 1)
        if target <= 0:
            problems.append("target token count is 0 (nothing this sample can teach)")
        supervised = sum(1 for i, v in enumerate(mask) if v == 1 and labels[i] != ct.LOSS_IGNORE_INDEX)
        ignored = sum(1 for i, v in enumerate(mask) if v == 0 and labels[i] == ct.LOSS_IGNORE_INDEX)
        if supervised != target:
            problems.append("labels are not supervised exactly where loss_mask==1 (%d/%d)"
                            % (supervised, target))
        if ignored + supervised != len(mask):
            problems.append("labels hold a mix that does not follow loss_mask")
    attention = row.get("attention_mask")
    if isinstance(attention, list) and lengths:
        if len(attention) != next(iter(lengths.values())):
            problems.append("attention_mask length differs from input_ids")
    return problems


def group_of(row: dict):
    ns = row.get("capture_namespace")
    root = row.get("root_session_id")
    if ns is None or root is None:
        return None
    return "%s\x1f%s" % (ns, root)


def check_dataset(dataset_dir: str, scanned: dict, samples_per_split: int) -> list:
    checks = []
    if scanned["format"] == "empty":
        return [check("dataset_nonempty", False,
                      "no dataset files under --dataset-dir %s" % dataset_dir,
                      {"exists": os.path.isdir(dataset_dir)})]
    total = sum(entry["total"] for entry in scanned["splits"].values())
    counts = {split: entry["total"] for split, entry in scanned["splits"].items()}
    checks.append(check("dataset_nonempty", total > 0,
                        "%d sample(s) across splits %s" % (total, counts),
                        {"total": total, "splits": counts, "files": scanned["files"]}))

    if scanned["arrow_only"]:
        checks.append(check("sample_shapes_consistent", False,
                            "the dataset is HuggingFace arrow-only: this stdlib verifier can "
                            "count it but cannot read labels/loss_mask, so the shape assertion "
                            "is UNPROVEN — re-run the converter with --format jsonl for the "
                            "acceptance artefact",
                            {"files": scanned["files"], "unreadable": scanned["unreadable"],
                         "row_count_source": scanned.get("row_count_source")}))
    else:
        problems = {}
        inspected = 0
        for split, entry in sorted(scanned["splits"].items()):
            for row in entry["samples"]:
                inspected += 1
                found = _shape_problems(row)
                if found:
                    key = "%s:%s" % (split, row.get("sample_id", "<unknown>"))
                    problems[key] = found
        checks.append(check("sample_shapes_consistent", not problems,
                            "%d sampled row(s) checked: labels/loss_mask aligned, target tokens > 0"
                            % inspected if not problems
                            else "shape/label violations: %s" % json.dumps(problems, sort_keys=True),
                            {"inspected": inspected,
                             "sampling_budget_per_split": samples_per_split,
                             "problems": problems}))

    train = scanned["splits"].get("train")
    test = scanned["splits"].get("test")
    if train is None or test is None:
        checks.append(check("group_split_disjoint", False,
                            "both a train and a test split are required for the grouping "
                            "assertion (splits=%s)" % sorted(scanned["splits"]),
                            {"splits": sorted(scanned["splits"])}))
    else:
        # Full-file group sets, not just the sample: a leak is a property of the split.
        train_groups, test_groups = set(), set()
        missing_provenance = 0
        for split, bucket in (("train", train_groups), ("test", test_groups)):
            for base in sorted(scanned["splits"][split]["files"]):
                path = os.path.join(dataset_dir, base)
                for line in read_lines(path):
                    try:
                        row = json.loads(line)
                    except json.JSONDecodeError:
                        missing_provenance += 1
                        continue
                    key = group_of(row)
                    if key is None:
                        missing_provenance += 1
                    else:
                        bucket.add(key)
        leaked = sorted(train_groups & test_groups)
        ok = bool(train_groups and test_groups) and not leaked and not missing_provenance
        detail = ("train groups=%d test groups=%d intersection=empty"
                  % (len(train_groups), len(test_groups)) if ok else
                  "grouping is not proven: leaked=%s rows_without_group_provenance=%d "
                  "train_groups=%d test_groups=%d"
                  % (leaked[:5], missing_provenance, len(train_groups), len(test_groups)))
        checks.append(check("group_split_disjoint", ok, detail,
                            {"train_groups": len(train_groups), "test_groups": len(test_groups),
                             "leaked": leaked[:20],
                             "rows_without_group_provenance": missing_provenance}))
    return checks


def check_rejections(dataset_dir: str) -> list:
    path = os.path.join(dataset_dir, "rejected.jsonl")
    if not os.path.isfile(path):
        return [check("rejected_ledger_exists", False,
                      "rejected.jsonl is missing from --dataset-dir: a strict run always "
                      "writes its ledger", {"path": path})]
    rows = []
    bad = []
    reasons = {}
    for line_num, line in enumerate(read_lines(path), 1):
        try:
            row = json.loads(line)
        except json.JSONDecodeError as exc:
            bad.append("%d: %s" % (line_num, exc))
            continue
        rows.append(row)
        reason = row.get("reason")
        if not isinstance(reason, str) or not reason.strip():
            bad.append("%d: no reason field" % line_num)
            continue
        if not row.get("source"):
            bad.append("%d: no source field" % line_num)
        reasons[reason] = reasons.get(reason, 0) + 1
    evidence = {"lines": len(rows), "malformed": bad[:20], "by_reason": reasons}
    checks = [check("rejected_ledger_exists", True,
                   "rejected.jsonl is present with %d line(s)" % len(rows),
                   {"path": path, "lines": len(rows)})]
    # An EMPTY ledger is a legitimate outcome of a clean run, so it is not a failure
    # here; the ledger-vs-manifest reconciliation is what proves a claimed rejection
    # was actually written down.
    checks.append(check("rejected_ledger_lines_have_reason", not bad,
                        "every rejection line names a source and a reason" if not bad
                        else "%d malformed rejection line(s): %s" % (len(bad), bad[:5]),
                        evidence))
    return checks


def check_conversion_ledger(dataset_dir: str, scanned: dict, rejected_lines: int) -> list:
    path = os.path.join(dataset_dir, "conversion_manifest.json")
    if not os.path.isfile(path):
        return [check("conversion_ledger_matches_artifacts", False,
                      "conversion_manifest.json is missing from --dataset-dir", {"path": path})]
    with open(path, "r", encoding="utf-8") as f:
        doc = json.load(f)
    total = sum(entry["total"] for entry in scanned["splits"].values())
    stats = doc.get("stats") or {}
    problems = {}
    accepted = _int(stats.get("accepted"))
    rejected = _int(stats.get("rejected"))
    seen = _int(stats.get("seen"))
    if accepted != total:
        problems["accepted_vs_dataset"] = {"manifest_accepted": accepted, "dataset_rows": total}
    if rejected != rejected_lines:
        problems["rejected_vs_ledger"] = {"manifest_rejected": rejected,
                                         "ledger_lines": rejected_lines}
    if seen is None or seen < (accepted or 0) + (rejected or 0):
        problems["seen_accounting"] = {"manifest_seen": seen,
                                      "explained": (accepted or 0) + (rejected or 0)}
    if (accepted or 0) + (rejected or 0) > max(seen or 0, 0):
        problems["accepted_plus_rejected_exceeds_seen"] = {"seen": seen,
                                                          "accepted": stats.get("accepted"),
                                                          "rejected": stats.get("rejected")}
    return [check("conversion_ledger_matches_artifacts", not problems,
                  "manifest stats reconcile with the written artefacts" if not problems
                  else "ledger does not reconcile: %s" % json.dumps(problems, sort_keys=True),
                  {"stats": stats, "dataset_rows": total, "rejected_lines": rejected_lines,
                   "problems": problems})]


# ---------------------------------------------------------------------------
# orchestration
# ---------------------------------------------------------------------------

def verify(gojson: str, capture_dir: str, facts: str, manifest: str, dataset_dir: str,
           expected_tests: list, facts_manifest=None, samples_per_split: int = 8,
           seed: int = 0) -> dict:
    """Run every required assertion over the artefacts on disk. No side effects."""
    checks = []
    try:
        go = parse_go_json(gojson)
        checks += check_go_json(go, expected_tests)
    except (OSError, json.JSONDecodeError) as exc:
        checks.append(check("go_test_json_parsed", False, "cannot read --gojson: %s" % exc))

    checks += check_capture_manifest(manifest)
    try:
        claim = capture_written_total(ct.load_capture_manifest(manifest))
    except ct.StrictError:
        claim = -1  # already reported by capture_manifest_readable
    checks += check_capture_dir(capture_dir, claim)
    checks += check_facts(facts, facts_manifest)

    scanned = scan_dataset(dataset_dir, samples_per_split, seed)
    checks += check_dataset(dataset_dir, scanned, samples_per_split)
    checks += check_rejections(dataset_dir)
    rejected_lines = 0
    rejected_path = os.path.join(dataset_dir, "rejected.jsonl")
    if os.path.isfile(rejected_path):
        rejected_lines = len(read_lines(rejected_path))
    checks += check_conversion_ledger(dataset_dir, scanned, rejected_lines)

    failed = [c["name"] for c in checks if c["status"] == FAIL]
    return {
        "schema": SCHEMA,
        "ok": not failed,
        "inputs": {"gojson": gojson, "capture_dir": capture_dir, "facts": facts,
                   "manifest": manifest, "facts_manifest": facts_manifest,
                   "dataset_dir": dataset_dir, "expect_tests": expected_tests,
                   "samples_per_split": samples_per_split, "seed": seed},
        "counts": {"checks": len(checks), "passed": len(checks) - len(failed),
                   "failed": len(failed),
                   "split_file_source": scanned.get("split_file_source"),
                   "dataset_splits": {k: v["total"] for k, v in scanned["splits"].items()}},
        "checks": checks,
        "failed_checks": failed,
    }


def render(report: dict) -> str:
    lines = ["runtime acceptance: %s (%d check(s))"
             % ("PASS" if report["ok"] else "FAIL", report["counts"]["checks"])]
    for entry in report["checks"]:
        lines.append("  [%s] %s — %s" % (entry["status"].upper(), entry["name"], entry["detail"]))
    lines.append("  inputs: dataset=%s manifest=%s facts=%s gojson=%s"
                 % (report["inputs"]["dataset_dir"], report["inputs"]["manifest"],
                    report["inputs"]["facts"], report["inputs"]["gojson"]))
    if report["failed_checks"]:
        lines.append("  failed: %s" % ", ".join(report["failed_checks"]))
    return "\n".join(lines)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify D17 real local acceptance artefacts (no go test is executed here)")
    parser.add_argument("--gojson", required=True,
                        help="go test -json output file (captured by the acceptance run)")
    parser.add_argument("--capture-dir", dest="capture_dir", required=True,
                        help="the capture stream directory/file; it must still hold at least "
                             "as many records as the seal claims were written")
    parser.add_argument("--facts", required=True,
                        help="the authorised fact export snapshot (.jsonl file or dir)")
    parser.add_argument("--manifest", required=True,
                        help="capture seal manifest (rl.MarshalCaptureManifest shape)")
    parser.add_argument("--facts-manifest", dest="facts_manifest", default=None,
                        help="export manifest written with --facts (rl.ExportManifest); without "
                             "it the export completeness assertion FAILS, never skips")
    parser.add_argument("--dataset-dir", dest="dataset_dir", required=True,
                        help="converter output directory (sft_*.jsonl + rejected.jsonl + "
                             "conversion_manifest.json)")
    parser.add_argument("--expect-tests", dest="expect_tests", required=True,
                        help="comma-separated required test names, e.g. "
                             "TestRealModel_Chat,TestRealModel_Tools")
    parser.add_argument("--samples", type=int, default=8,
                        help="rows sampled per split for the shape assertions (default 8)")
    parser.add_argument("--seed", type=int, default=0, help="sampling seed (default 0)")
    parser.add_argument("--report", default=None, help="also write the machine-readable JSON here")
    args = parser.parse_args(argv)

    expected = [name.strip() for name in args.expect_tests.split(",") if name.strip()]
    if not expected:
        parser.error("--expect-tests must list at least one required test name")
    for flag, path in (("--gojson", args.gojson), ("--dataset-dir", args.dataset_dir),
                       ("--manifest", args.manifest)):
        if not os.path.exists(path):
            print("Error: %s path not found: %s" % (flag, path), file=sys.stderr)
            return 1
    if not os.path.exists(args.facts):
        print("Error: --facts path not found: %s" % args.facts, file=sys.stderr)
        return 1

    report = verify(args.gojson, args.capture_dir, args.facts, args.manifest, args.dataset_dir,
                    expected, facts_manifest=args.facts_manifest,
                    samples_per_split=max(1, args.samples), seed=args.seed)
    print(render(report))
    if args.report:
        parent = os.path.dirname(os.path.abspath(args.report))
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(args.report, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, sort_keys=True, indent=2)
            f.write("\n")
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
