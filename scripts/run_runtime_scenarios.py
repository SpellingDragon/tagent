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

schema v2（`event-integrity` 合同）额外能力，v1 行为不变：

  * 按 `tests[].module`/`module_path`/`dir` 用 `go -C <module> test <pkg> -run '^(…)$'
    -count=1 [-tags …] [-race] -json`，一个 (module, tags, race) 组一次调用；
  * `--repeat N`：N 次独立进程（每次独立 UUID run_id、目录 `repeat-<i>/`、
    test-log 与 evidence）；底层一律 `-count=1`，禁止用 `-count=N` 冒充多轮；
  * `--seed`/`--window`：必须是合同 matrix 内的成对组合，追加 `-args`；
  * `--test-id <leaf>/<TestName>`（可重复）：按合同 id 精确选择子集，未登记即用法错误；
  * 零匹配即失败：进程退出 0 但日志里没有该测试的 run 终局时判 unattested，
    不把退出码单独当凭据；
  * 指纹覆盖受影响第一方源码、未跟踪测试、fixture、合同与各模块 go.mod/go.sum，
    不只记 HEAD。
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
import uuid

import runtime_checklib as lib

MODULE_PATH = "github.com/SpellingDragon/tagent"
SCHEMA_VERSION = 1
EVENT_SCHEMA_VERSION = 2
EVENT_PROFILE = "event-integrity"
EVENT_FINGERPRINT_SUFFIXES = (".go", ".mod", ".sum")
EVENT_FINGERPRINT_SKIP_DIRS = frozenset({".git", "node_modules", "vendor", "__pycache__"})
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


def read_contract_object(path: str) -> dict:
    """Read a contract file as a JSON object without assuming its schema."""
    try:
        with open(path, encoding="utf-8") as handle:
            contract = json.load(handle)
    except OSError as exc:
        raise RunnerError("scenario contract not readable: {}: {}".format(path, exc))
    except json.JSONDecodeError as exc:
        raise RunnerError("scenario contract is not JSON: {}: {}".format(path, exc))
    if not isinstance(contract, dict):
        raise RunnerError("scenario contract must be a JSON object")
    return contract


def load_contract(path: str) -> dict:
    """Read the v1 scenario contract; a missing or malformed file is a usage error."""
    contract = read_contract_object(path)
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


# ---------------------------------------------------------------------------
# Schema v2: module-aware, repeatable, matrix-bound event-integrity runs
#
# The v1 path above stays byte-for-byte compatible.  Everything below only runs
# when the contract declares `schema_version: 2` + `profile: event-integrity`.
# ---------------------------------------------------------------------------

EVENT_REQUIRED_TEST_FIELDS = ("id", "leaf", "kind", "module", "module_path", "dir",
                              "test_package", "test_name", "file", "selector", "scope")


def index_event_tests(contract: dict) -> dict:
    """Index the v2 contract by `leaf/TestName`, rejecting unbalanced entries."""
    if contract.get("schema_version") != EVENT_SCHEMA_VERSION:
        raise RunnerError("event contract schema_version must be {}".format(EVENT_SCHEMA_VERSION))
    if contract.get("profile") != EVENT_PROFILE:
        raise RunnerError("event contract profile must be {!r}".format(EVENT_PROFILE))
    tests = contract.get("tests")
    if not isinstance(tests, list) or not tests:
        raise RunnerError("event contract carries no tests")
    index = {}
    for position, test in enumerate(tests):
        where = "tests[{}]".format(position)
        if not isinstance(test, dict):
            raise RunnerError("{} must be an object".format(where))
        test_id = test.get("id")
        if not isinstance(test_id, str) or not test_id.strip():
            raise RunnerError("{} carries no id".format(where))
        if test_id in index:
            raise RunnerError("duplicate event test id {!r}".format(test_id))
        for field in EVENT_REQUIRED_TEST_FIELDS:
            value = test.get(field)
            if not isinstance(value, str) or not value.strip():
                raise RunnerError("{}: {} must be a non-empty string".format(test_id, field))
        if not isinstance(test.get("tags"), list):
            raise RunnerError("{}: tags must be a list".format(test_id))
        if test.get("race") is not True:
            raise RunnerError("{}: race must be true".format(test_id))
        subtests = test.get("subtests")
        if not isinstance(subtests, list) or not subtests:
            raise RunnerError("{}: subtests must be a non-empty list".format(test_id))
        index[test_id] = test
    return index


def event_scope_names(contract: dict) -> list:
    scopes = contract.get("scopes")
    if not isinstance(scopes, list) or not scopes:
        raise RunnerError("event contract scopes must be a non-empty list")
    names = []
    for entry in scopes:
        if not isinstance(entry, dict) or not isinstance(entry.get("name"), str):
            raise RunnerError("event contract scope entry must declare a string name")
        names.append(entry["name"])
    if len(names) != len(set(names)):
        raise RunnerError("event contract scope names must be unique")
    return names


def event_scope_repeat(contract: dict, scope: str) -> int:
    """Read the repeat count the contract fixes for one scope."""
    event_scope_names(contract)
    for entry in contract["scopes"]:
        if entry.get("name") != scope:
            continue
        repeat = entry.get("repeat")
        if type(repeat) is not int or repeat < 1:
            raise RunnerError("event contract scope {!r} must declare a positive integer repeat".format(
                scope))
        return repeat
    raise RunnerError("unknown scope {!r}: registered event scopes are {}".format(
        scope, event_scope_names(contract)))


def event_matrix_combinations(contract: dict) -> list:
    """Enumerate the frozen (seed, window) combinations exactly once each."""
    matrix = contract.get("matrix")
    if not isinstance(matrix, dict):
        raise RunnerError("event contract matrix must be an object")
    values = {}
    for name in ("seeds", "windows"):
        items = matrix.get(name)
        if not isinstance(items, list) or not items or any(type(item) is not int for item in items):
            raise RunnerError("event contract matrix.{} must be a non-empty list of integers".format(name))
        if len(items) != len(set(items)):
            raise RunnerError("event contract matrix.{} must not repeat a value".format(name))
        values[name] = items
    combos = [(seed, window) for seed in values["seeds"] for window in values["windows"]]
    declared = matrix.get("combos")
    if declared != len(combos):
        raise RunnerError("event contract matrix.combos={!r} does not equal the {} seed/window pairs".format(
            declared, len(combos)))
    return combos


def event_matrix_pair(contract: dict, seed, window):
    """Accept only a seed/window pair frozen in the contract matrix."""
    if seed is None and window is None:
        return None, None
    if seed is None or window is None:
        raise RunnerError("--seed and --window must be supplied together (the matrix pairs them)")
    combos = event_matrix_combinations(contract)
    if (seed, window) not in combos:
        raise RunnerError("--seed {} / --window {} is not a contract matrix combination; allowed: {}".format(
            seed, window, combos))
    return seed, window


def event_selection(contract: dict, index: dict, scope: str, test_ids) -> tuple:
    """Resolve exactly one contract scope plus an optional `--test-id` subset.

    A v2 evidence file describes one registration unit, so the cross-scope
    `complete` alias of the v1 runner is not accepted here: the short/long split
    is what the bundle gate consumes, and each round must name its own scope.
    """
    names = event_scope_names(contract)
    if scope not in names:
        raise RunnerError("unknown event scope {!r}: schema v2 selects one contract scope {}".format(
            scope, names))
    if test_ids:
        selected = []
        for test_id in test_ids:
            if test_id not in index:
                raise RunnerError("--test-id {!r} is not registered in the event contract".format(test_id))
            if test_id not in selected:
                selected.append(test_id)
        outside = [test_id for test_id in selected if index[test_id].get("scope") != scope]
        if outside:
            raise RunnerError("--test-id {} is outside scope {!r}".format(outside, scope))
        remaining = [test_id for test_id in index
                     if test_id not in selected and index[test_id].get("scope") == scope]
        return selected, remaining
    selected = [test_id for test_id in index if index[test_id].get("scope") == scope]
    if not selected:
        raise RunnerError("scope {!r} selects no test in the event contract".format(scope))
    return selected, [test_id for test_id in index if test_id not in selected]


def event_package_rel(module_path: str, package: str) -> str:
    """Normalise a package path to its module-relative form (no ./ prefix)."""
    if not isinstance(package, str) or not package.strip():
        raise RunnerError("test_package must be a non-empty string")
    text = package.strip()
    if text in ("", "."):
        return "."
    if text == module_path:
        return "."
    if text.startswith(module_path + "/"):
        rel = text[len(module_path) + 1:]
    elif text.startswith("./"):
        rel = text[2:]
    elif "." in text.split("/", 1)[0]:
        raise RunnerError("test package {!r} is outside module {!r}".format(package, module_path))
    else:
        rel = text
    rel = rel.rstrip("/")
    return rel or "."


def event_package_arg(module_path: str, package: str) -> str:
    """Render one package as the `go test` argument for its own module."""
    rel = event_package_rel(module_path, package)
    return "." if rel == "." else "./" + rel


def event_groups(index: dict, selected: list) -> list:
    """Group the selection by (module dir, build tags, race) for one invocation each."""
    groups = {}
    for test_id in selected:
        test = index[test_id]
        module_dir = test["dir"]
        tags = tuple(sorted(test.get("tags") or []))
        race = test.get("race") is True
        key = (module_dir, tags, race)
        group = groups.get(key)
        if group is None:
            group = {"dir": module_dir, "module_path": test["module_path"],
                     "tags": list(tags), "race": race, "packages": [], "tests": []}
            groups[key] = group
        elif group["module_path"] != test["module_path"]:
            raise RunnerError("module dir {!r} is declared with two module paths".format(module_dir))
        package = event_package_arg(test["module_path"], test["test_package"])
        if package not in group["packages"]:
            group["packages"].append(package)
        group["tests"].append(test_id)
    ordered = [groups[key] for key in sorted(groups, key=lambda item: (item[0], item[1], item[2]))]
    for group in ordered:
        group["packages"] = sorted(group["packages"])
        group["tests"] = sorted(group["tests"])
    return ordered


def event_command(group: dict, index: dict, seed, window, timeout: float = None) -> list:
    """Build one `go -C <module> test ...` invocation with the declared flags."""
    names = sorted({index[test_id]["test_name"] for test_id in group["tests"]})
    pattern = "^({})$".format("|".join(names))
    command = ["go", "-C", group["dir"], "test"] + list(group["packages"])
    command += ["-run", pattern, "-count=1"]
    if timeout:
        # Propagate the budget into the child so go test's own 10m default
        # cannot fire before the runner's wait does.
        command += ["-timeout", "{}s".format(int(timeout))]
    if group["tags"]:
        command += ["-tags", ",".join(group["tags"])]
    if group["race"]:
        command.append("-race")
    command.append("-json")
    if seed is not None:
        command += ["-args", "-seed={}".format(seed), "-window={}".format(window)]
    return command


def _fingerprint_entries(module_dir: str) -> list:
    """List (relative path, absolute path) of every fingerprinted module file."""
    entries = []
    for dirpath, dirnames, filenames in os.walk(module_dir):
        dirnames[:] = sorted(
            name for name in dirnames
            if name not in EVENT_FINGERPRINT_SKIP_DIRS
            and not os.path.isfile(os.path.join(dirpath, name, "go.mod")))
        for name in sorted(filenames):
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, module_dir)
            if not (name.endswith(EVENT_FINGERPRINT_SUFFIXES) or "testdata" in rel.split(os.sep)):
                continue
            entries.append((rel.replace(os.sep, "/"), path))
    return sorted(entries)


def module_fingerprint(repo_root: str, module_dir: str, module_path: str) -> dict:
    """Fingerprint one module's sources, deps and the tests actually driven.

    HEAD alone is not enough: uncommitted first-party code, untracked test files
    and the module's go.mod/go.sum all change what a run means, so the digest is
    computed over those bytes (nested modules are skipped, they have their own).
    """
    absolute = os.path.join(repo_root, module_dir)
    if not os.path.isdir(absolute):
        raise RunnerError("module directory not found: {}".format(absolute))
    entries = _fingerprint_entries(absolute)
    if not entries:
        raise RunnerError("no fingerprintable sources under {}".format(absolute))
    digest = hashlib.sha256()
    for rel, path in entries:
        try:
            with open(path, "rb") as handle:
                data = handle.read()
        except OSError as exc:
            raise RunnerError("cannot fingerprint {}: {}".format(path, exc))
        digest.update(rel.encode("utf-8"))
        digest.update(b"\0")
        digest.update(data)
    return {"module_path": module_path, "dir": module_dir.replace(os.sep, "/"),
            "revision": git_revision(repo_root),
            "content_hash": "sha256:" + digest.hexdigest()}


def resolve_log_package(package: str, module_paths) -> tuple:
    """Resolve one test-log Package to (module_path, module-relative package)."""
    for module_path in sorted(module_paths, key=len, reverse=True):
        if package == module_path:
            return module_path, "."
        prefix = module_path + "/"
        if package.startswith(prefix):
            return module_path, package[len(prefix):]
    return None, None


def index_event_actions(rows: list, module_paths) -> dict:
    """Count run/terminal actions per (module, package, test) for one repeat."""
    actions = {}
    for row in rows:
        action = row.get("Action")
        package = row.get("Package")
        test = row.get("Test") or ""
        if not isinstance(action, str) or not isinstance(package, str) or not isinstance(test, str):
            continue
        module_path, rel = resolve_log_package(package, module_paths)
        if module_path is None:
            continue
        counter = actions.setdefault((module_path, rel, test), {})
        counter[action] = counter.get(action, 0) + 1
    return actions


def event_subtest_states(test: dict, actions: dict) -> list:
    """Record the terminal verdict of every contract-registered named anchor."""
    module_path = test["module_path"]
    package = event_package_rel(module_path, test["test_package"])
    parent = test["test_name"]
    states = []
    for subtest in test.get("subtests") or []:
        if not isinstance(subtest, dict):
            continue
        name = subtest.get("name")
        counter = actions.get((module_path, package, parent + "/" + name), {})
        if counter.get("fail"):
            status = "fail"
        elif counter.get("skip"):
            status = "skip"
        elif counter.get("pass"):
            status = "pass"
        elif counter.get("run"):
            status = "running"
        else:
            status = "missing"
        states.append({"test_id": test["id"], "name": name, "status": status,
                       "mandatory": subtest.get("mandatory") is not False})
    return states


def attest_event_test(test: dict, actions: dict) -> dict:
    """State whether a test's own JSON run terminal attests it in this repeat.

    A zero-match invocation can still exit 0, so the process exit code alone is
    never accepted: the log must carry this test's run+pass terminal.
    """
    module_path = test["module_path"]
    package = event_package_rel(module_path, test["test_package"])
    name = test["test_name"]
    counter = actions.get((module_path, package, name), {})
    state = {"test_id": test["id"], "module_path": module_path, "test_package": package,
             "test_name": name, "runs": counter.get("run", 0), "passes": counter.get("pass", 0),
             "fails": counter.get("fail", 0), "skips": counter.get("skip", 0),
             "subtests": event_subtest_states(test, actions)}
    if not counter:
        state["state"] = "unattested"
        state["reason"] = ("no case ran for {}::{}::{} (zero match; a process exit code alone "
                           "does not attest)".format(module_path, package, name))
        return state
    if counter.get("fail", 0):
        state["state"] = "unattested"
        state["reason"] = "{}::{}::{} recorded fail".format(module_path, package, name)
        return state
    if counter.get("skip", 0):
        state["state"] = "unattested"
        state["reason"] = "{}::{}::{} was skipped".format(module_path, package, name)
        return state
    if not counter.get("run", 0) or not counter.get("pass", 0):
        state["state"] = "unattested"
        state["reason"] = "{}::{}::{} ran without a terminal pass".format(module_path, package, name)
        return state
    prefix = name + "/"
    broken = sorted(
        observed for (module, package_name, observed), sub in actions.items()
        if (module, package_name) == (module_path, package) and observed.startswith(prefix)
        and (sub.get("fail", 0) or sub.get("skip", 0)))
    if broken:
        state["state"] = "unattested"
        state["reason"] = "subtest(s) {} did not pass".format(broken)
        return state
    failed_anchors = [entry["name"] for entry in state["subtests"]
                      if entry["mandatory"] and entry["status"] != "pass"]
    if failed_anchors:
        state["state"] = "unattested"
        state["reason"] = "mandatory anchor(s) {} did not pass".format(failed_anchors)
        return state
    state["state"] = "attested"
    state["reason"] = None
    return state


def run_event_repeat(args, root: str, index: dict, groups: list, selected: list,
                     seed, window, repeat_index: int, repeat_total: int,
                     out_dir: str, contract_digest: str, fingerprints: list) -> dict:
    """Run every group once, in its own directory, under one fresh run_id."""
    run_id = str(uuid.uuid4())
    repeat_dir = os.path.join(out_dir, "repeat-{}".format(repeat_index))
    os.makedirs(repeat_dir, exist_ok=True)
    chunks = []
    stderr_chunks = []
    invocations = []
    exit_code = 0
    for group in groups:
        command = event_command(group, index, seed, window, args.timeout)
        group_exit, stdout, stderr = run_go_test(command, root, args.timeout)
        chunks.append(stdout)
        if stderr.strip():
            stderr_chunks.append("# {}\n{}".format(" ".join(command), stderr))
        invocations.append({"module_path": group["module_path"], "dir": group["dir"],
                            "packages": list(group["packages"]), "tags": list(group["tags"]),
                            "race": group["race"], "command": " ".join(command),
                            "exit_code": group_exit})
        if group_exit != 0 and exit_code == 0:
            exit_code = group_exit
    log_path = os.path.join(repeat_dir, "test-log.jsonl")
    log_text = "".join(chunks)
    with open(log_path, "w", encoding="utf-8") as handle:
        handle.write(log_text)
    if stderr_chunks:
        with open(os.path.join(repeat_dir, "go-test-stderr.log"), "w", encoding="utf-8") as handle:
            handle.write("\n".join(stderr_chunks))
    rows = parse_rows(log_text)
    actions = index_event_actions(rows, [group["module_path"] for group in groups])
    states = [attest_event_test(index[test_id], actions) for test_id in selected]
    subtests = [entry for state in states for entry in state["subtests"]]
    attested = sum(1 for state in states if state["state"] == "attested")
    verdict = "match" if exit_code == 0 and attested == len(states) else "unattested"
    try:
        log_digest = lib.sha256_file(log_path)
    except lib.CheckFailure as exc:
        raise RunnerError(str(exc))
    evidence = {
        "schema_version": EVENT_SCHEMA_VERSION,
        "profile": EVENT_PROFILE,
        "run_id": run_id,
        "scope": args.scope,
        "repeat_index": repeat_index,
        "repeat_total": repeat_total,
        "contract_digest": contract_digest,
        "test_ids": list(selected),
        "module_fingerprints": fingerprints,
        "run_config": {
            "seed": seed,
            "window": window,
            "tags": sorted({tag for group in groups for tag in group["tags"]}),
            "race": all(group["race"] for group in groups),
            "selectors": [
                {"test_id": test_id, "module_path": index[test_id]["module_path"],
                 "test_package": event_package_arg(index[test_id]["module_path"],
                                                   index[test_id]["test_package"]),
                 "test_name": index[test_id]["test_name"]}
                for test_id in selected],
            "invocations": invocations,
        },
        "go_test_exit_code": exit_code,
        "test_log": os.path.abspath(log_path),
        "test_log_sha256": log_digest,
        "subtests": subtests,
        "verdict": verdict,
    }
    evidence_path = os.path.join(repeat_dir, "evidence.json")
    with open(evidence_path, "w", encoding="utf-8") as handle:
        json.dump(evidence, handle, ensure_ascii=False, indent=1, sort_keys=True)
        handle.write("\n")
    return {"repeat_index": repeat_index, "run_id": run_id, "directory": repeat_dir,
            "test_log": os.path.abspath(log_path), "evidence": evidence_path,
            "go_test_exit_code": exit_code, "verdict": verdict, "tests": states,
            "counts": {"tests_required": len(states), "tests_attested": attested,
                       "subtests_required": len(subtests),
                       "subtests_passed": sum(1 for entry in subtests if entry["status"] == "pass")}}


def execute_event(args, root: str, contract: dict, contract_path: str) -> dict:
    """Select, run and attest the v2 scope, one evidence file per repeat."""
    index = index_event_tests(contract)
    selected, remaining = event_selection(contract, index, args.scope, args.test_id)
    seed, window = event_matrix_pair(contract, args.seed, args.window)
    if args.repeat is not None:
        if type(args.repeat) is not int or args.repeat < 1:
            raise RunnerError("--repeat must be a positive integer")
        repeat_total = args.repeat
    else:
        repeat_total = event_scope_repeat(contract, args.scope)
    groups = event_groups(index, selected)
    collected = {}
    for group in groups:
        if group["dir"] not in collected:
            collected[group["dir"]] = module_fingerprint(root, group["dir"], group["module_path"])
    fingerprints = [collected[key] for key in sorted(collected)]
    try:
        contract_digest = lib.sha256_file(contract_path)
    except lib.CheckFailure as exc:
        raise RunnerError(str(exc))
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    out_dir = os.path.abspath(args.out) if args.out else os.path.join(
        tempfile.gettempdir(), "tagent-runtime-scenarios", "{}-{}".format(stamp, args.scope))
    os.makedirs(out_dir, exist_ok=True)
    repeats = []
    for repeat_index in range(1, repeat_total + 1):
        repeats.append(run_event_repeat(args, root, index, groups, selected, seed, window,
                                        repeat_index, repeat_total, out_dir,
                                        contract_digest, fingerprints))
    exit_codes = [repeat["go_test_exit_code"] for repeat in repeats]
    verdict = "match" if all(repeat["verdict"] == "match" for repeat in repeats) else "unattested"
    return {
        "mode": "event-integrity-v2",
        "scope": args.scope,
        "test_ids": selected,
        "remaining_required": remaining,
        "repeat_total": repeat_total,
        "seed": seed,
        "window": window,
        "contract_digest": contract_digest,
        "out": out_dir,
        "verdict": verdict,
        "go_test_exit_code": next((code for code in exit_codes if code != 0), 0),
        "repeats": repeats,
        "counts": {"repeat_total": repeat_total,
                   "repeats_passed": sum(1 for repeat in repeats
                                         if repeat["verdict"] == "match"),
                   "tests_required": len(selected),
                   "subtests_required": sum(len(index[test_id]["subtests"])
                                            for test_id in selected)},
    }


def format_event_report(report: dict) -> str:
    """Render the v2 run as per-repeat rows plus the remaining selection debt."""
    lines = ["mode: event-integrity (schema v2)",
             "scope: {}".format(report["scope"]),
             "test_ids: {}".format(" ".join(report["test_ids"]) or "-"),
             "remaining_required: {}".format(" ".join(report["remaining_required"]) or "-"),
             "repeat_total: {}".format(report["repeat_total"]),
             "seed/window: {}/{}".format(report["seed"], report["window"]),
             "contract_digest: {}".format(report["contract_digest"]),
             "verdict: {}".format(report["verdict"])]
    for repeat in report["repeats"]:
        lines.append("repeat-{}/{} run_id={} verdict={} exit={} log={} evidence={}".format(
            repeat["repeat_index"], report["repeat_total"], repeat["run_id"],
            repeat["verdict"], repeat["go_test_exit_code"], repeat["test_log"],
            repeat["evidence"]))
        for state in repeat["tests"]:
            if state["state"] == "unattested":
                lines.append("  {:<42} unattested  {}".format(state["test_id"], state["reason"]))
    lines.append("repeats: passed={}/{}".format(report["counts"]["repeats_passed"],
                                                report["counts"]["repeat_total"]))
    return "\n".join(lines)


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
                        help="验收集：complete（v1 默认，全部场景）或合同登记的唯一子集标签"
                             "（v2 合同只接受登记的 scope，如 event-short/event-long）")
    parser.add_argument("--contract", default=os.path.join("golden", "runtime", "scenarios.json"),
                        help="scenario 合同路径（只读对照物）")
    parser.add_argument("--root", default=".", help="仓库根（默认当前目录）")
    parser.add_argument("--out", default=None,
                        help="产物目录（默认系统临时目录下的 run 目录）")
    parser.add_argument("--timeout", type=float, default=900.0, help="go test 超时秒数")
    parser.add_argument("--repeat", type=int, default=None,
                        help="v2：独立进程重复次数（默认取合同 scope 声明的 repeat；禁止 -count=N 冒充）")
    parser.add_argument("--seed", type=int, default=None,
                        help="v2：矩阵 seed（必须与 --window 成对且落在合同 matrix 内）")
    parser.add_argument("--window", type=int, default=None,
                        help="v2：矩阵 window（必须与 --seed 成对且落在合同 matrix 内）")
    parser.add_argument("--test-id", action="append", default=[], dest="test_id",
                        metavar="LEAF/TestName",
                        help="v2：按合同 id 精确选择（可重复）；未登记或子场景名即用法错误")
    parser.add_argument("--json", action="store_true", help="把报告以 JSON 打到 stdout")
    return parser.parse_args(argv)


def execute(args) -> dict:
    root = os.path.abspath(args.root)
    contract_path = os.path.join(root, args.contract)
    contract = read_contract_object(contract_path)
    if contract.get("schema_version") == EVENT_SCHEMA_VERSION:
        return execute_event(args, root, contract, contract_path)
    if args.repeat is not None or args.seed is not None or args.window is not None or args.test_id:
        raise RunnerError("--repeat/--seed/--window/--test-id require a schema v2 event contract; "
                          "the v1 contract keeps its single fixed command")
    contract = load_contract(contract_path)
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
    elif report.get("mode") == "event-integrity-v2":
        print(format_event_report(report))
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
