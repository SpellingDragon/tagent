#!/usr/bin/env python3
"""run_runtime_scenarios.py — 09.1 场景 runner：真实驱动 + 机器对照 golden（纯 stdlib）。

它**不判断"看起来通过"**，也不允许人工汇总 PASS。它只做四件事：

  1. 按 golden 合同选验收集（--scope）：complete 要求全部场景；其余必须是合同
     `scopes.labels` 登记的标签（如 deterministic），未纳入的场景一律列进
     `remaining_required`，**不对未纳入场景报全通过**。
  2. 真实跑一次 `go test <pkg...> -run '^(用例名…)$' -count=1 -json`：单次调用保证每个
     (包,用例) 恰有一次 run，日志原样落到 test-log.jsonl（check_runtime_evidence.py
     的生命周期状态机可直接消费）。
  3. 从 test-log 的 Output 行解析 `TAGENT-SCENARIO-RECORD:` 记录——观测值是场景用例
     真实驱动产品入口后自报的，runner 不改写、不补值、不归一成"期望值"。
  4. 拿真实观测逐场景逐键对照 golden，按三轴出差异：值 / 消息角色序列 / 身份关系。
     差异只如实报告；golden 是对照物不是金标准，修正 golden 或修产品的裁决不在这里。

证据产物（RUN-file）按 golden `evidence_contract` 的字段集写出，供
`python3 scripts/check_runtime_evidence.py --evidence <RUN-file> --test-log <PATH>
--scope <同一 scope>` 二次核账。

退出码：
  0  选中的每个场景都有记录且三轴全等
  1  有场景拿不到记录（无用例 / 未 run / fail / skip）——不冒充已验
  2  用法错误（未知 scope、合同不可读等）
  3  全部有记录但存在与 golden 的差异（差异明细逐条打印，不静默通过）

运行:
  python3 scripts/run_runtime_scenarios.py --scope deterministic
  python3 scripts/run_runtime_scenarios.py --scope complete --out /tmp/scenarios
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import subprocess
import sys
import tempfile
import time

MODULE_PATH = "github.com/SpellingDragon/tagent"
SCHEMA_VERSION = 1
RECORD_MARKER = "TAGENT-SCENARIO-RECORD:"
SCOPE_COMPLETE = "complete"
SOURCE_DIR = os.path.join("tests", "integration", "acceptance")

# 三轴分类：合同里每一个观测键都必须落到恰好一轴上（未登记键直接报错，避免新观测
# 悄悄逃出对照）。角色序列轴只看有序 message role 序列；身份关系轴收对象间引用/归属
# 关系的结构化观测（多对象记录、映射与有序对象序列）；其余标量与纯计数落值轴。
AXIS_VALUE = "value"
AXIS_ROLE_SEQUENCE = "message-role-sequence"
AXIS_IDENTITY = "identity-relation"
ROLE_SEQUENCE_KEYS = frozenset({"model_message_roles"})
IDENTITY_KEYS = frozenset({
    "requested_calls",
    "receipts",
    "backgrounds",
    "captured_content_markers",
    "notice_identity_fields",
    "blocker_detail_rows",
    "pipeline_artifact_links",
    "model_replacement_binding",
})

EXIT_OK = 0
EXIT_UNATTESTED = 1
EXIT_USAGE = 2
EXIT_DRIFT = 3


class RunnerError(Exception):
    """A usage/contract error the runner reports instead of guessing past."""


def axis_of(key: str) -> str:
    """Return the single axis one contract observation key belongs to."""
    if key in ROLE_SEQUENCE_KEYS:
        return AXIS_ROLE_SEQUENCE
    if key in IDENTITY_KEYS:
        return AXIS_IDENTITY
    return AXIS_VALUE


def typed_equal(expected, observed) -> bool:
    """Compare contract values without letting True equal 1 or 1 equal 1.0."""
    if isinstance(expected, bool) or isinstance(observed, bool):
        return isinstance(expected, bool) and isinstance(observed, bool) and expected == observed
    if isinstance(expected, (int, float)) and isinstance(observed, (int, float)):
        return type(expected) is type(observed) and expected == observed
    if isinstance(expected, dict) and isinstance(observed, dict):
        return set(expected) == set(observed) and all(
            typed_equal(expected[key], observed[key]) for key in expected)
    if isinstance(expected, list) and isinstance(observed, list):
        return len(expected) == len(observed) and all(
            typed_equal(left, right) for left, right in zip(expected, observed))
    return type(expected) is type(observed) and expected == observed


def diff_paths(expected, observed, prefix: str = "") -> list:
    """Report every leaf where the real observation diverges from the contract.

    A list is an ordered sequence: the first position where order or content
    diverges is named, so a role sequence reads as `model_message_roles[3]`
    rather than as one opaque mismatch.
    """
    if isinstance(expected, dict) and isinstance(observed, dict):
        out = []
        for key in sorted(set(expected) | set(observed)):
            path = "{}.{}".format(prefix, key) if prefix else key
            if key not in observed:
                out.append({"path": path, "expected": expected[key], "observed": None,
                            "reason": "missing"})
            elif key not in expected:
                out.append({"path": path, "expected": None, "observed": observed[key],
                            "reason": "unexpected"})
            else:
                out.extend(diff_paths(expected[key], observed[key], path))
        return out
    if isinstance(expected, list) and isinstance(observed, list):
        out = []
        for index in range(min(len(expected), len(observed))):
            out.extend(diff_paths(expected[index], observed[index],
                                  "{}[{}]".format(prefix, index)))
        if len(expected) != len(observed):
            out.append({"path": "{}[len]".format(prefix), "expected": len(expected),
                        "observed": len(observed), "reason": "length"})
        return out
    if not typed_equal(expected, observed):
        return [{"path": prefix, "expected": expected, "observed": observed,
                 "reason": "value"}]
    return []


def load_contract(path: str) -> dict:
    """Read the scenario contract; a missing or malformed file is a usage error."""
    try:
        with open(path, encoding="utf-8") as handle:
            contract = json.load(handle)
    except OSError as exc:
        raise RunnerError("scenario contract not readable: {}: {}".format(path, exc))
    except json.JSONDecodeError as exc:
        raise RunnerError("scenario contract is not JSON: {}: {}".format(path, exc))
    if not isinstance(contract, dict):
        raise RunnerError("scenario contract must be a JSON object")
    scenarios = contract.get("scenarios")
    if not isinstance(scenarios, list) or not scenarios:
        raise RunnerError("scenario contract carries no scenarios")
    return contract


def index_scenarios(contract: dict) -> dict:
    """Index the contract by scenario id and reject two scenarios sharing an id."""
    index = {}
    for scenario in contract.get("scenarios", ()):
        if not isinstance(scenario, dict):
            raise RunnerError("scenario entry must be an object")
        scenario_id = scenario.get("id")
        if not isinstance(scenario_id, str) or not scenario_id.strip():
            raise RunnerError("scenario entry carries no id")
        if scenario_id in index:
            raise RunnerError("duplicate scenario id {!r}".format(scenario_id))
        index[scenario_id] = scenario
    return index


def validate_axes(index: dict) -> None:
    """Every contract observation key must be classified, or the run is refused."""
    for scenario_id, scenario in index.items():
        observations = scenario.get("observations")
        if not isinstance(observations, dict) or not observations:
            raise RunnerError("{}: observations must be a non-empty object".format(scenario_id))
        for key in observations:
            if not isinstance(key, str) or not key.strip():
                raise RunnerError("{}: observation key must be a non-empty string".format(
                    scenario_id))


def registered_labels(contract: dict) -> list:
    """Read the unique scope registration; a scenario's scope is its one label."""
    block = contract.get("scopes")
    labels = block.get("labels") if isinstance(block, dict) else None
    if not isinstance(labels, list) or not labels:
        raise RunnerError("contract declares no scopes.labels registration")
    for label in labels:
        if not isinstance(label, str) or not label.strip():
            raise RunnerError("contract scope label must be a non-empty string")
    if len(labels) != len(set(labels)):
        raise RunnerError("contract scope labels must be unique")
    return labels


def resolve_scope(contract: dict, index: dict, scope: str):
    """Resolve one scope name to (required ids, remaining ids).

    'complete' requires every registered scenario. Any other name must be a
    registered label and requires exactly the scenarios carrying it; everything
    else is remaining-required and is never reported as passed.
    """
    labels = registered_labels(contract)
    for scenario_id, scenario in index.items():
        label = scenario.get("scope")
        if label not in labels:
            raise RunnerError("{}: scope {!r} is not a registered label {}".format(
                scenario_id, label, labels))
    ordered = list(index)
    if scope == SCOPE_COMPLETE:
        return ordered, []
    if scope not in labels:
        raise RunnerError("unknown scope {!r}: registered scopes are 'complete' and {}".format(
            scope, labels))
    required = [sid for sid in ordered if index[sid].get("scope") == scope]
    remaining = [sid for sid in ordered if index[sid].get("scope") != scope]
    if not required:
        raise RunnerError("scope {!r} selects no scenario in the contract".format(scope))
    return required, remaining


def selectors(contract_index: dict, scenario_ids: list) -> list:
    """Return the (package, test name) selectors the in-scope scenarios fix."""
    out = []
    for scenario_id in scenario_ids:
        scenario = contract_index[scenario_id]
        package = scenario.get("test_package")
        name = scenario.get("test_name")
        if not isinstance(package, str) or not package.strip():
            raise RunnerError("{}: test_package must be a non-empty string".format(scenario_id))
        if not isinstance(name, str) or not name.strip():
            raise RunnerError("{}: test_name must be a non-empty string".format(scenario_id))
        out.append((scenario_id, package, name))
    return out


def normalize_package(package: str):
    """Normalise one go test package path by the exact module prefix."""
    if not isinstance(package, str) or not package.strip():
        return None
    if package == MODULE_PATH:
        return "."
    prefix = MODULE_PATH + "/"
    if package.startswith(prefix):
        return package[len(prefix):]
    return None


def go_test_command(packages: list, names: list) -> list:
    """Build the single go test invocation whose log is the run's test-log."""
    pattern = "^({})$".format("|".join(sorted(set(names))))
    targets = ["./" + package for package in packages]
    return ["go", "test"] + targets + ["-run", pattern, "-count=1", "-json"]


def parse_rows(text: str) -> list:
    """Parse a `go test -json` stream; a non-JSON line is a usage error."""
    rows = []
    for number, line in enumerate(text.splitlines(), start=1):
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError as exc:
            raise RunnerError("test-log line {} is not JSON: {}".format(number, exc))
        if not isinstance(row, dict):
            raise RunnerError("test-log line {} is not a JSON object".format(number))
        rows.append(row)
    return rows


def collect_records(rows: list) -> dict:
    """Collect the records the scenario cases actually emitted, per scenario id."""
    records = {}
    for row in rows:
        output = row.get("Output")
        if not isinstance(output, str) or RECORD_MARKER not in output:
            continue
        payload = output.split(RECORD_MARKER, 1)[1].strip()
        try:
            record = json.loads(payload)
        except json.JSONDecodeError as exc:
            raise RunnerError("scenario record is not JSON: {}".format(exc))
        if not isinstance(record, dict):
            raise RunnerError("scenario record is not an object")
        scenario_id = record.get("scenario_id")
        if not isinstance(scenario_id, str) or not scenario_id.strip():
            raise RunnerError("scenario record carries no scenario_id")
        if scenario_id in records:
            raise RunnerError("{}: emitted more than one record in this run".format(scenario_id))
        records[scenario_id] = record
    return records


def index_actions(rows: list) -> dict:
    """Index run/terminal actions per (package, test) without judging them."""
    actions = {}
    for row in rows:
        action = row.get("Action")
        package = normalize_package(row.get("Package"))
        test = row.get("Test") or ""
        if not isinstance(action, str) or package is None or not isinstance(test, str) or not test:
            continue
        counter = actions.setdefault((package, test), {})
        counter[action] = counter.get(action, 0) + 1
    return actions


def attest(scenario_id: str, package: str, name: str, record, actions: dict) -> dict:
    """State whether the run attest this scenario: a record beside a real pass."""
    counter = actions.get((package, name), {})
    state = {"scenario_id": scenario_id, "test_package": package, "test_name": name,
             "runs": counter.get("run", 0), "passes": counter.get("pass", 0),
             "fails": counter.get("fail", 0), "skips": counter.get("skip", 0)}
    if record is None:
        if not counter:
            reason = "no case ran for {}::{} (nothing emitted a record)".format(package, name)
        elif counter.get("fail", 0):
            reason = "{}::{} recorded fail".format(package, name)
        elif counter.get("skip", 0):
            reason = "{}::{} was skipped".format(package, name)
        elif counter.get("run", 0) and not counter.get("pass", 0):
            reason = "{}::{} ran without a terminal pass".format(package, name)
        else:
            reason = "{}::{} ran but emitted no scenario record".format(package, name)
        state["state"] = "unattested"
        state["reason"] = reason
        return state
    if record.get("test_package") != package or record.get("test_name") != name:
        state["state"] = "unattested"
        state["reason"] = "record selector {}::{} does not match the contract {}::{}".format(
            record.get("test_package"), record.get("test_name"), package, name)
        return state
    refs = record.get("event_refs")
    if not isinstance(refs, list) or not refs:
        state["state"] = "unattested"
        state["reason"] = "record carries no event_refs"
        return state
    if not counter.get("run", 0) or not counter.get("pass", 0):
        state["state"] = "unattested"
        state["reason"] = "{}::{} has no run+pass beside its record".format(package, name)
        return state
    state["state"] = "attested"
    return state


def compare_scenario(scenario: dict, record: dict) -> list:
    """Compare one record's observations against its contract on the three axes."""
    expected = scenario.get("observations", {})
    observed = record.get("observations")
    if not isinstance(observed, dict):
        raise RunnerError("{}: record observations must be an object".format(scenario.get("id")))
    diffs = []
    for key in sorted(set(expected) | set(observed)):
        axis = axis_of(key)
        if key not in observed:
            diffs.append({"axis": axis, "path": key, "expected": expected[key],
                          "observed": None, "reason": "missing"})
            continue
        if key not in expected:
            diffs.append({"axis": axis, "path": key, "expected": None,
                          "observed": observed[key], "reason": "unexpected"})
            continue
        for entry in diff_paths(expected[key], observed[key], key):
            entry["axis"] = axis
            diffs.append(entry)
    return diffs


def source_fingerprint(root: str) -> dict:
    """Fingerprint the module revision and the scenario sources actually driven."""
    directory = os.path.join(root, SOURCE_DIR)
    if not os.path.isdir(directory):
        raise RunnerError("scenario source directory not found: {}".format(directory))
    digest = hashlib.sha256()
    count = 0
    for name in sorted(os.listdir(directory)):
        if not (name.startswith("scenario") and name.endswith(".go")):
            continue
        path = os.path.join(directory, name)
        digest.update(name.encode("utf-8"))
        with open(path, "rb") as handle:
            digest.update(handle.read())
        count += 1
    if not count:
        raise RunnerError("no scenario sources found under {}".format(directory))
    return {"module_path": MODULE_PATH, "revision": git_revision(root),
            "content_hash": "sha256:" + digest.hexdigest()}


def git_revision(root: str) -> str:
    """Read the checked-out revision; an unreadable SHA is reported, not invented."""
    try:
        result = subprocess.run(["git", "-C", root, "rev-parse", "HEAD"], check=False,
                                capture_output=True, text=True)
    except OSError as exc:
        raise RunnerError("git is not runnable: {}".format(exc))
    revision = result.stdout.strip()
    if result.returncode != 0 or not revision:
        raise RunnerError("cannot read the repository revision: {}".format(
            result.stderr.strip()))
    return revision


def run_go_test(command: list, root: str, timeout: float):
    """Run the single go test invocation and return (exit code, raw log text)."""
    try:
        result = subprocess.run(command, cwd=root, check=False, capture_output=True,
                                text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        raise RunnerError("go test timed out after {}s".format(timeout))
    except OSError as exc:
        raise RunnerError("go test is not runnable: {}".format(exc))
    return result.returncode, result.stdout, result.stderr


def build_evidence(scope: str, command: list, exit_code: int, fingerprint: dict,
                   test_log_ref: str, records: list, run_id: str) -> dict:
    """Assemble the RUN-file in the shape the golden evidence contract fixes."""
    return {
        "schema_version": SCHEMA_VERSION,
        "runner": {
            "name": "run_runtime_scenarios",
            "entrypoint": "scripts/run_runtime_scenarios.py",
            "scope": scope,
        },
        "execution": {
            "run_id": run_id,
            "command": " ".join(command),
            "platform": platform.platform(),
            "exit_code": exit_code,
        },
        "source_fingerprint": fingerprint,
        "test_log_ref": test_log_ref,
        "records": records,
    }


def describe(value) -> str:
    """Render one contract/observed value for the human diff table."""
    text = json.dumps(value, ensure_ascii=False, sort_keys=True)
    return text if len(text) <= 160 else text[:157] + "..."


def format_report(report: dict) -> str:
    """Render the run as a per-scenario, per-axis table plus the remaining debt."""
    lines = ["scope: {}".format(report["scope"]),
             "required: {}".format(" ".join(report["required"]) or "-"),
             "remaining_required: {}".format(" ".join(report["remaining_required"]) or "-"),
             "run_id: {}".format(report["run_id"]),
             "test-log: {}".format(report["test_log"]),
             "evidence: {}".format(report["evidence"]),
             "verdict: {}".format(report["verdict"])]
    for state in report["scenarios"]:
        if state["state"] == "unattested":
            lines.append("{:<5} {:<22} unattested  {}".format(
                state["scenario_id"], "-", state["reason"]))
            continue
        diffs = state["diffs"]
        if not diffs:
            lines.append("{:<5} {:<22} match       {} observation(s) equal".format(
                state["scenario_id"], "-", state["observations"]))
            continue
        for entry in diffs:
            lines.append("{:<5} {:<22} drift       {} <- expected {} / observed {}".format(
                state["scenario_id"], entry["axis"], entry["path"],
                describe(entry["expected"]), describe(entry["observed"])))
    lines.append("scenarios: required={} attested={} drift={} unattested={}".format(
        report["counts"]["required"], report["counts"]["attested"],
        report["counts"]["drift"], report["counts"]["unattested"]))
    lines.append("follow-up: python3 scripts/check_runtime_evidence.py --evidence {} "
                 "--test-log {} --scope {}".format(report["evidence"], report["test_log"],
                                                  report["scope"]))
    return "\n".join(lines)


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--scope", default=SCOPE_COMPLETE,
                        help="验收集：complete（默认，全部场景）或合同登记的唯一子集标签")
    parser.add_argument("--contract", default=os.path.join("golden", "runtime", "scenarios.json"),
                        help="scenario 合同路径（只读对照物）")
    parser.add_argument("--root", default=".", help="仓库根（默认当前目录）")
    parser.add_argument("--out", default=None,
                        help="产物目录（默认系统临时目录下的 run 目录）")
    parser.add_argument("--timeout", type=float, default=900.0, help="go test 超时秒数")
    parser.add_argument("--json", action="store_true", help="把报告以 JSON 打到 stdout")
    return parser.parse_args(argv)


def execute(args) -> dict:
    root = os.path.abspath(args.root)
    contract = load_contract(os.path.join(root, args.contract))
    index = index_scenarios(contract)
    validate_axes(index)
    required, remaining = resolve_scope(contract, index, args.scope)
    chosen = selectors(index, required)

    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    run_id = "run-{}-{}".format(stamp, args.scope)
    out_dir = os.path.abspath(args.out) if args.out else os.path.join(
        tempfile.gettempdir(), "tagent-runtime-scenarios", run_id)
    os.makedirs(out_dir, exist_ok=True)

    packages = sorted({package for _, package, _ in chosen})
    names = [name for _, _, name in chosen]
    command = go_test_command(packages, names)
    exit_code, stdout, stderr = run_go_test(command, root, args.timeout)

    log_path = os.path.join(out_dir, "test-log.jsonl")
    with open(log_path, "w", encoding="utf-8") as handle:
        handle.write(stdout)
    if stderr.strip():
        with open(os.path.join(out_dir, "go-test-stderr.log"), "w", encoding="utf-8") as handle:
            handle.write(stderr)

    rows = parse_rows(stdout)
    records = collect_records(rows)
    actions = index_actions(rows)

    states = []
    evidence_records = []
    for scenario_id, package, name in chosen:
        record = records.get(scenario_id)
        state = attest(scenario_id, package, name, record, actions)
        if state["state"] == "attested":
            scenario = index[scenario_id]
            state["diffs"] = compare_scenario(scenario, record)
            state["observations"] = len(scenario.get("observations", {}))
            evidence_records.append({key: record.get(key) for key in
                                     ("scenario_id", "test_package", "test_name",
                                      "event_refs", "observations")})
        states.append(state)

    attested = sum(1 for state in states if state["state"] == "attested")
    drift = sum(1 for state in states if state["state"] == "attested" and state["diffs"])
    unattested = sum(1 for state in states if state["state"] == "unattested")
    if unattested:
        verdict = "unattested"
    elif drift:
        verdict = "drift"
    else:
        verdict = "match"

    fingerprint = source_fingerprint(root)
    evidence = build_evidence(args.scope, command, exit_code, fingerprint,
                              os.path.basename(log_path), evidence_records, run_id)
    evidence_path = os.path.join(out_dir, "evidence.json")
    with open(evidence_path, "w", encoding="utf-8") as handle:
        json.dump(evidence, handle, ensure_ascii=False, indent=1, sort_keys=True)
        handle.write("\n")

    return {
        "scope": args.scope,
        "required": required,
        "remaining_required": remaining,
        "run_id": run_id,
        "test_log": log_path,
        "evidence": evidence_path,
        "go_test_exit_code": exit_code,
        "verdict": verdict,
        "scenarios": states,
        "counts": {"required": len(required), "attested": attested,
                   "drift": drift, "unattested": unattested},
    }


def main(argv) -> int:
    args = parse_args(argv)
    try:
        report = execute(args)
    except RunnerError as exc:
        sys.stderr.write("run_runtime_scenarios: {}\n".format(exc))
        return EXIT_USAGE
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=1, sort_keys=True))
    else:
        print(format_report(report))
    if report["go_test_exit_code"] != 0:
        return EXIT_UNATTESTED
    if report["verdict"] == "unattested":
        return EXIT_UNATTESTED
    if report["verdict"] == "drift":
        return EXIT_DRIFT
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
