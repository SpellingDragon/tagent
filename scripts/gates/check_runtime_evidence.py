#!/usr/bin/env python3
"""check_runtime_evidence.py — 运行行为证据检查（格式合同 vs 真实记录）。

两种模式（互斥）：
  * --check-contract                     只验证 golden/runtime/scenarios.json 合同
                                         （10 个场景、五字段、具体可比值、plan task 归属）。
  * --evidence PATH --test-log PATH      对照真实证据：source 指纹、逐条 record、
                                         以及真实 `go test -json` 记录。

严格性：没有 --evidence 就失败（strict，不放行）；test-log 必须出现 run 与 pass，
重复 run / 子测试 skip / 包级 fail / 只追加 pass 都不算通过（fail 不被后面的 pass
last-wins 覆盖）；观测缺 case、未知键、错误值、bool-int 类型漂移、空测试选择器
一律非 0。evidence 记录的 test_package/test_name 必须与 10 金样合同逐字一致，不得
自选等价的其它用例名。JSONL 逐行拒绝重复键。包路径只做精确 module 前缀匹配
（不用 endswith）。source 指纹/event refs 只做“明示存在与格式（git SHA / sha256）
可读”校验；该元数据由 runner 自报，**本工具不声称它是防伪、真实或来源已认证的
证据**。productVerified 恒为 null。

test-log 状态机不只看每 (包,用例) 自己的 run/终局序列，还核验生命周期嵌于所属窗口：
测试事件（run/pass/pause/cont）MUST 落在其所属包的 start..终局窗口内（包启动前/结束后
的测试事件一律拒绝），包终局时其下 MUST 无未终结用例，子测试事件 MUST 落在其父测试
窗口内；同时允许合法多包并发与合法并行子测试 run/pause/cont/pass。

`--scope` 选择验收集：默认 `complete`（严格，全部 10 个场景）；合同用唯一 scope 登记
（每个场景恰一个 scope 标签）声明子集，`--scope <label>` 只验该子集并输出 `scope` 与
`remaining_required`，**不对未纳入的场景报全通过**；不提供任意 `--scenario` 挑选入口。

scenario 的 status 接受 planned/blocked/in_progress/done：本轮金样全为 planned，
但不得把检查器永久锁死在 planned（否则未来每个真实完成都会红）。status=done 必须
携带 evidence{run_id,commands,exit_codes,artifact_paths}，否则不算完成。
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

import runtime_checklib as lib

EXPECTED_SCENARIOS = 15
# Default verification set: the complete golden. A contract may register named
# subsets (e.g. "deterministic") that exclude resource-bound scenarios; a subset
# run must report the outstanding scenarios rather than a full pass.
SCOPE_COMPLETE = "complete"
SCENARIO_ID_RE = re.compile(r"^[A-Za-z][A-Za-z0-9_-]*\d{2,}$")
TEST_NAME_RE = re.compile(r"^Test[A-Za-z0-9_]+$")
OLD_NUMBERING_RE = re.compile(r"^(F\d+|.*RV\d+.*)$")
REVISION_RE = re.compile(r"^[0-9a-f]{7,40}$")
CONTENT_HASH_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
PLACEHOLDER_VALUES = {"", "tbd", "todo", "value", "expected", "placeholder", "?", "n/a"}
CONTRACT_FIELDS = ("id", "title", "task_ids", "test_package", "test_name", "status", "observations")
# scenario status 词表（与 task-graph 一致）。本轮金样全为 planned；其余状态存在，
# 是为了让未来完成能以证据如实表达，而不是把检查器永久锁死在 planned。
SCENARIO_STATUSES = ("planned", "blocked", "in_progress", "done")
DONE_EVIDENCE_FIELDS = ("run_id", "commands", "exit_codes", "artifact_paths")
# The evidence_contract block must declare (at least) the fields this checker
# enforces; a contract that silently omits an enforced field would let the
# documented asset drift away from the code that guards it.  Declaring extra
# fields is only a warning (the contract may describe more than the checker
# asserts), but omitting an enforced field is a hard error.
CONTRACT_FIELD_SETS = (
    ("required_fields", ("schema_version", "runner", "execution", "source_fingerprint",
                         "test_log_ref", "records")),
    ("record_fields", ("scenario_id", "test_package", "test_name", "event_refs", "observations")),
    ("source_fingerprint_fields", ("module_path", "revision", "content_hash")),
    ("execution_fields", ("run_id", "command", "platform", "exit_code")),
)

# ---------------------------------------------------------------------------
# Event-integrity contract (schema v2).  The v1 golden above stays frozen; the v2
# branch reads the graph's explicit test registry instead of introducing a second
# tuple manifest, so counts are derived from lists rather than hand-maintained.
# ---------------------------------------------------------------------------

EVENT_SCHEMA_VERSION = 2
EVENT_PROFILE = "event-integrity"
EVENT_FAMILIES = ("E1", "E2", "E3", "E4", "E5", "E6", "E7", "E8")
EVENT_ROLES = ("positive", "negative", "consumer", "control")
EVENT_TEST_KINDS = ("product-green", "diagnostic")
EVENT_SCOPES = ("event-short", "event-long")
EVENT_TEST_ID_RE = re.compile(r"^\d{2}\.\d+[a-z]?/Test[A-Za-z0-9_]+$")
EVENT_SUBTEST_RE = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_./-]*$")
EVENT_WHOLE_CHAIN_ID_RE = re.compile(r"^EI\d{2}$")
EVENT_TEST_FUNC_RE = re.compile(r"^func (Test[A-Za-z0-9_]+)\(", re.MULTILINE)
EVENT_RUN_RE = re.compile(r"\bt\.Run\(\s*\"([^\"]+)\"")
EVENT_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
# Fixed policy values from 04 design D1: any shrink or arithmetic drift is rejected.
EVENT_MATRIX_EXPECTED = {
    "seeds": [1, 7, 42],
    "windows": [256, 32768],
    "combos": 6,
    "sessions_per_host": 4,
    "inputs_per_session": 300,
    "restarts_per_session": 30,
    "per_combo": {"inputs": 1200, "restarts": 120},
    "total": {"inputs": 7200, "restarts": 720},
}


def check_scenario_done_evidence(report, where, evidence):
    """A scenario marked 'done' must carry machine-checkable evidence, not a flag.

    This round's golden contract stays 'planned'; a historical 'done' is never
    inherited.  The shape mirrors the task-graph 'done' evidence so the two stay
    consistent.
    """
    lib.check_completion_evidence(report, where, evidence)


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", default=os.path.join("golden", "runtime", "scenarios.json"))
    parser.add_argument("--check-contract", action="store_true")
    parser.add_argument("--evidence", default=None)
    parser.add_argument("--test-log", default=None)
    parser.add_argument("--bundle", default=None,
                        help="v2：汇总目录（收集多份 evidence.json：short 十份、long 六组合、"
                             "mutations 一份）")
    parser.add_argument("--plan-graph", default=None,
                        help="可选：task-graph.json，用于核对 task_ids 归属")
    parser.add_argument("--scope", default=SCOPE_COMPLETE,
                        help="验收集：complete（默认，全部场景）或合同登记的唯一子集标签")
    parser.add_argument("--module-path", default=lib.MODULE_PATH)
    parser.add_argument("--repo-root", default=EVENT_REPO_ROOT,
                        help="v2 合同模式：源码树根（默认仓库根）；selftest 用隔离的 fixture 树"
                             "验证 selector/发现逻辑")
    parser.add_argument("--json", action="store_true")
    return parser.parse_args(argv)


def validate_evidence_contract(report, contract):
    """Self-consistency: the declared field sets must cover what is enforced."""
    block = contract.get("evidence_contract")
    if not isinstance(block, dict):
        report.error("evidence_contract must be an object")
        return
    total = 0
    for name, expected in CONTRACT_FIELD_SETS:
        declared = block.get(name)
        if not isinstance(declared, list) or any(not isinstance(value, str) for value in declared):
            report.error("evidence_contract.{} must be a list of strings".format(name))
            continue
        total += len(declared)
        missing = [field for field in expected if field not in declared]
        if missing:
            report.error("evidence_contract.{} omits checker-enforced field(s) {}".format(
                name, missing))
        extra = [field for field in declared if field not in expected]
        if extra:
            report.warn("evidence_contract.{} declares field(s) not enforced by the checker: "
                        "{}".format(name, extra))
    report.count("evidence_contract_fields", total)


def validate_scopes(report, contract):
    """Validate the unique scope registration; return the registered labels.

    The contract must always describe all 10 golden scenarios; a scope only
    selects which subset a run is responsible for.  Each scenario carries exactly
    one scope label (its registration), and the label set is declared once.
    """
    block = contract.get("scopes")
    if not isinstance(block, dict):
        report.error("scopes must be an object declaring the unique scope registration")
        return set()
    labels = block.get("labels")
    if (not isinstance(labels, list) or not labels
            or any(not isinstance(label, str) or not label.strip() for label in labels)):
        report.error("scopes.labels must be a non-empty list of scope-name strings")
        return set()
    if len(labels) != len(set(labels)):
        report.error("scopes.labels must be unique (no duplicate scope registration)")
    return {label for label in labels if isinstance(label, str)}


def resolve_scope(report, contract, index, scope_name):
    """Resolve a scope name to (required_ids, remaining_ids).

    'complete' (the default) requires every registered scenario.  Any other name
    must be a registered scope label and requires exactly the scenarios labelled
    with it; the rest are reported as remaining-required (never a silent pass).
    A free-form --scenario cherry-pick is deliberately NOT offered.
    """
    all_ids = list(index)
    if scope_name == SCOPE_COMPLETE:
        return all_ids, []
    block = contract.get("scopes")
    labels = block.get("labels") if isinstance(block, dict) else None
    if not isinstance(labels, list) or scope_name not in labels:
        report.error("unknown scope {!r}: registered scopes are 'complete' and {}".format(
            scope_name, sorted(labels) if isinstance(labels, list) else []))
        return None, None
    required = [sid for sid in all_ids if index[sid].get("scope") == scope_name]
    remaining = [sid for sid in all_ids if index[sid].get("scope") != scope_name]
    if not required:
        report.error("scope {!r} selects no scenario in the contract".format(scope_name))
        return None, None
    return required, remaining


def validate_contract(report, contract, graph_ids):
    if contract.get("schema_version") != 1:
        report.error("scenarios schema_version must be 1, got {!r}".format(
            contract.get("schema_version")))
    validate_evidence_contract(report, contract)
    registered_scopes = validate_scopes(report, contract)
    scenarios = contract.get("scenarios")
    if not isinstance(scenarios, list):
        report.error("scenarios must be a list")
        return {}, 0
    report.count("scenario_count", len(scenarios))
    if len(scenarios) != EXPECTED_SCENARIOS:
        report.error("expected {} golden scenarios, found {}".format(
            EXPECTED_SCENARIOS, len(scenarios)))
    index = {}
    observation_kinds = set()
    observation_total = 0
    for position, scenario in enumerate(scenarios):
        where = "scenarios[{}]".format(position)
        if not isinstance(scenario, dict):
            report.error("{} must be an object".format(where))
            continue
        sid = scenario.get("id")
        if not isinstance(sid, str) or not SCENARIO_ID_RE.match(sid):
            report.error("{} has invalid id {!r}".format(where, sid))
            continue
        where = "scenario {}".format(sid)
        if OLD_NUMBERING_RE.match(sid):
            report.error("{}: must not reuse old F/RV numbering".format(where))
        scope_label = scenario.get("scope")
        if not isinstance(scope_label, str) or scope_label not in registered_scopes:
            report.error("{}: scope {!r} is not a registered scope label {}".format(
                where, scope_label, sorted(registered_scopes)))
        if sid in index:
            report.error("duplicate scenario id {}".format(sid))
            continue
        index[sid] = scenario
        for field in CONTRACT_FIELDS:
            if field not in scenario:
                report.error("{}: missing field {!r}".format(where, field))
        if scenario.get("status") not in SCENARIO_STATUSES:
            report.error("{}: status {!r} not in {}".format(
                where, scenario.get("status"), SCENARIO_STATUSES))
        elif scenario.get("status") == "done":
            # Never locked to 'planned': a future verified scenario must be able
            # to advance to 'done' — but only with real evidence.
            check_scenario_done_evidence(report, where, scenario.get("evidence"))
        task_ids = scenario.get("task_ids")
        if not isinstance(task_ids, list) or not task_ids:
            report.error("{}: task_ids must be a non-empty list".format(where))
            task_ids = []
        for tid in task_ids:
            if not isinstance(tid, str) or not lib.TASK_ID_RE.match(tid):
                report.error("{}: invalid task id {!r}".format(where, tid))
            elif graph_ids is not None and tid not in graph_ids:
                report.error("{}: task {} not present in task-graph".format(where, tid))
        package = scenario.get("test_package")
        if not isinstance(package, str) or not package.strip():
            report.error("{}: test_package must be a non-empty string".format(where))
        elif lib.normalize_package(package) is None:
            report.error("{}: test_package {!r} is not a module-relative path".format(where, package))
        name = scenario.get("test_name")
        if not isinstance(name, str) or not TEST_NAME_RE.match(name):
            report.error("{}: test_name {!r} must look like a Go test function".format(where, name))
        observations = scenario.get("observations")
        if not isinstance(observations, dict) or not observations:
            report.error("{}: observations must be a non-empty object".format(where))
            continue
        for key, value in observations.items():
            if value is None:
                report.error("{}: observation {!r} is null (key name is not a value)".format(where, key))
                continue
            if isinstance(value, str) and value.strip().lower() in PLACEHOLDER_VALUES:
                report.error("{}: observation {!r} is a placeholder {!r}".format(where, key, value))
            observation_kinds.add(lib.value_kind(value))
            observation_total += 1
    report.count("observation_values", observation_total)
    for kind in ("bool", "number", "list", "dict"):
        if kind not in observation_kinds:
            report.error("contract has no {!r}-valued observation: expectations must be "
                         "concrete comparable values, not key names".format(kind))
    return index, observation_total


def load_graph_ids(report, path):
    try:
        graph = lib.load_json_object(path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return None
    tasks = graph.get("tasks")
    if not isinstance(tasks, list):
        report.error("{}: tasks must be a list".format(path))
        return None
    return {task.get("id") for task in tasks if isinstance(task, dict)}


def index_test_log(report, rows, module_path):
    """Index a `go test -json` stream with package/test lifecycle nesting.

    Each (package, test) sequence is not judged in isolation: a test lifecycle
    event MUST fall inside its owning package's start..terminal window (an event
    before the package starts or after it ends is rejected), the owning package
    MUST NOT terminate while a test under it is still open, and a subtest's
    events MUST fall inside its parent test's window. Legal multi-package
    concurrency and legal parallel subtest run/pause/cont/pass are accepted.
    """
    packages = set()
    package_actions = {}
    test_actions = {}
    foreign = set()
    pkg_state = {}      # rel -> new | running | paused | ended
    test_state = {}     # (rel, test) -> running | paused | terminal
    open_tests = {}     # rel -> set of test names not yet terminal
    open_action = ("output", "build-output", "build-fail", "bench")
    for row in rows:
        action = row.get("Action")
        package = row.get("Package")
        if not isinstance(action, str) or not isinstance(package, str):
            report.error("malformed test-log row: Action and Package must be strings")
            continue
        rel = lib.normalize_package(package, module_path)
        if rel is None:
            foreign.add(package)
            continue
        packages.add(rel)
        test = row.get("Test", "")
        if test is None:
            test = ""
        if not isinstance(test, str):
            report.error("malformed test-log Test must be a string")
            continue
        pstate = pkg_state.get(rel, "new")
        if not test:
            # ---- package-level lifecycle -----------------------------------
            if action == "start":
                if pstate != "new":
                    report.error("{} package duplicate start".format(rel))
                pkg_state[rel] = "running"
            elif action in ("pass", "fail", "skip"):
                if pstate not in ("running", "paused"):
                    report.error("{} package terminal {} without active start".format(rel, action))
                still_open = sorted(open_tests.get(rel, ()))
                if still_open:
                    report.error("{} package {} while test(s) {} still open (unterminated case)".format(
                        rel, action, still_open))
                pkg_state[rel] = "ended"
            elif action == "pause" and pstate == "running":
                pkg_state[rel] = "paused"
            elif action == "cont" and pstate == "paused":
                pkg_state[rel] = "running"
            elif action not in open_action:
                report.error("{} package invalid action sequence {}".format(rel, action))
            if action in ("fail", "skip"):
                report.error("{} package recorded {} in test-log".format(rel, action))
            counter = package_actions.setdefault(rel, {})
            counter[action] = counter.get(action, 0) + 1
            continue
        # ---- test-level lifecycle -----------------------------------------
        key = (rel, test)
        tstate = test_state.get(key)
        if pstate not in ("running", "paused"):
            report.error("{}::{} lifecycle {} outside its package start/end window".format(
                rel, test, action))
        if "/" in test:
            parent = test.rsplit("/", 1)[0]
            if test_state.get((rel, parent)) not in ("running", "paused"):
                report.error("{}::{} subtest event {} outside its parent test".format(
                    rel, test, action))
        if action == "run":
            if tstate is not None:
                report.error("{}::{} duplicate run".format(rel, test))
            test_state[key] = "running"
            open_tests.setdefault(rel, set()).add(test)
        elif action in ("pass", "fail", "skip"):
            if tstate not in ("running", "paused"):
                report.error("{}::{} terminal {} without active run".format(rel, test, action))
            test_state[key] = "terminal"
            open_tests.get(rel, set()).discard(test)
        elif action == "pause" and tstate == "running":
            test_state[key] = "paused"
        elif action == "cont" and tstate == "paused":
            test_state[key] = "running"
        elif action not in open_action:
            report.error("{}::{} invalid action sequence {}".format(rel, test, action))
        if action in ("fail", "skip"):
            report.error("{}::{} recorded {} in test-log".format(rel, test, action))
        counter = test_actions.setdefault(key, {})
        counter[action] = counter.get(action, 0) + 1
    for (package, test), state in test_state.items():
        if state != "terminal":
            report.error("{}::{} missing terminal event".format(package, test))
    for package, state in pkg_state.items():
        if state != "ended":
            report.error("{} package missing terminal event".format(package))
    report.count("test_log_packages", len(packages))
    report.count("test_log_tests", len(test_actions))
    if foreign:
        report.warn("test-log has {} package(s) outside the module prefix".format(len(foreign)))
    return {"packages": packages, "package_actions": package_actions,
            "test_actions": test_actions, "foreign": foreign}


def validate_evidence_shape(report, evidence):
    if evidence.get("schema_version") != 1:
        report.error("evidence schema_version must be 1, got {!r}".format(
            evidence.get("schema_version")))
    execution = evidence.get("execution")
    if not isinstance(execution, dict):
        report.error("evidence.execution is required")
    else:
        for field in ("run_id", "command", "platform"):
            if not isinstance(execution.get(field), str) or not execution[field].strip():
                report.error("execution.{} must be a non-empty string".format(field))
        if type(execution.get("exit_code")) is not int or execution["exit_code"] != 0:
            report.error("execution.exit_code must be successful integer exit 0")
    runner = evidence.get("runner")
    if not isinstance(runner, dict) or not runner.get("name") or not runner.get("entrypoint"):
        report.error("evidence.runner must declare name and entrypoint")
    fingerprint = evidence.get("source_fingerprint")
    if not isinstance(fingerprint, dict):
        report.error("evidence.source_fingerprint is required")
    else:
        for field in ("module_path", "revision", "content_hash"):
            value = fingerprint.get(field)
            if not isinstance(value, str) or not value.strip():
                report.error("source_fingerprint.{} must be a non-empty string".format(field))
        revision = fingerprint.get("revision")
        if isinstance(revision, str) and revision.strip() and not REVISION_RE.match(revision.strip()):
            report.error("source_fingerprint.revision {!r} is not a readable git SHA "
                         "(7-40 lowercase hex)".format(revision))
        content_hash = fingerprint.get("content_hash")
        if isinstance(content_hash, str) and content_hash.strip() and not CONTENT_HASH_RE.match(content_hash.strip()):
            report.error("source_fingerprint.content_hash {!r} is not a readable sha256 "
                         "digest (sha256:<64 hex>)".format(content_hash))
    if not isinstance(evidence.get("test_log_ref"), str) or not evidence["test_log_ref"].strip():
        report.error("evidence.test_log_ref must be a non-empty string")
    records = evidence.get("records")
    if not isinstance(records, list) or not records:
        report.error("evidence.records must be a non-empty list")
        return []
    report.note("source fingerprint / event refs are self-reported by the runner and are "
                "validated for presence and shape only (git SHA / sha256 readable, not "
                "authenticated); this is neither a proof of authenticity nor of provenance")
    return records


def check_selector(report, scenario_id, package, name, log_index):
    if not package or not name:
        report.error("{}: empty test selector".format(scenario_id))
        return
    rel = lib.normalize_package(package)
    if rel is None:
        report.error("{}: test package {!r} cannot be normalised by exact module prefix".format(
            scenario_id, package))
        return
    counter = log_index["test_actions"].get((rel, name))
    if not counter:
        report.error("{}: no run/pass in test-log for {}::{}".format(scenario_id, rel, name))
        return
    runs = counter.get("run", 0)
    passes = counter.get("pass", 0)
    if runs == 0 or passes == 0:
        report.error("{}: {}::{} has no run+pass (run={}, pass={})".format(
            scenario_id, rel, name, runs, passes))
    if runs > 1:
        report.error("{}: {}::{} ran {} times (duplicate run)".format(scenario_id, rel, name, runs))
    if counter.get("fail", 0):
        report.error("{}: {}::{} recorded fail (a later pass cannot cover it)".format(
            scenario_id, rel, name))
    if counter.get("skip", 0):
        report.error("{}: {}::{} was skipped".format(scenario_id, rel, name))
    for (pkg, test), sub in log_index["test_actions"].items():
        if pkg != rel or not test.startswith(name + "/"):
            continue
        if sub.get("skip", 0):
            report.error("{}: subtest {}::{} was skipped".format(scenario_id, pkg, test))
        if sub.get("fail", 0):
            report.error("{}: subtest {}::{} failed".format(scenario_id, pkg, test))
    package_counter = log_index["package_actions"].get(rel, {})
    if package_counter.get("start", 0) != 1 or package_counter.get("pass", 0) != 1:
        report.error("{}: package {} missing unique start/pass terminal".format(scenario_id, rel))
    if package_counter.get("fail", 0):
        report.error("{}: package {} recorded a package-level fail".format(scenario_id, rel))
    if package_counter.get("skip", 0):
        report.error("{}: package {} was skipped".format(scenario_id, rel))


def compare_observations(report, scenario_id, expected, actual):
    if not isinstance(actual, dict):
        report.error("{}: observations must be an object".format(scenario_id))
        return
    for key in expected:
        if key not in actual:
            report.error("{}: missing observation case {!r}".format(scenario_id, key))
            continue
        if not lib.typed_equal(expected[key], actual[key]):
            report.error("{}: observation {!r} expected {} but observed {}".format(
                scenario_id, key, lib.describe_value(expected[key]), lib.describe_value(actual[key])))
    for key in actual:
        if key not in expected:
            report.error("{}: unknown observation {!r}".format(scenario_id, key))


def check_evidence(report, contract_index, evidence, log_index, required_ids=None):
    records = validate_evidence_shape(report, evidence)
    required = set(contract_index) if required_ids is None else set(required_ids)
    covered = set()
    for position, record in enumerate(records):
        where = "records[{}]".format(position)
        if not isinstance(record, dict):
            report.error("{} must be an object".format(where))
            continue
        scenario_id = record.get("scenario_id")
        if not isinstance(scenario_id, str) or scenario_id not in contract_index:
            report.error("{}: unknown scenario_id {!r}".format(where, scenario_id))
            continue
        if scenario_id not in required:
            report.error("{}: scenario {} is outside the selected scope (remaining-required); "
                         "a subset-scope run MUST NOT present it (no synthetic resource case)".format(
                             where, scenario_id))
            continue
        where = "record {}".format(scenario_id)
        if scenario_id in covered:
            report.error("{}: duplicate evidence record for scenario".format(where))
        covered.add(scenario_id)
        package = record.get("test_package")
        name = record.get("test_name")
        if not isinstance(package, str) or not isinstance(name, str) or not package.strip() or not name.strip():
            report.error("{}: empty test selector".format(where))
            continue
        scenario = contract_index[scenario_id]
        if package != scenario.get("test_package") or name != scenario.get("test_name"):
            report.error("{}: selector {}::{} does not match the golden contract {}::{} "
                         "(evidence must use the fixed scenario case)".format(
                             where, package, name, scenario.get("test_package"),
                             scenario.get("test_name")))
        refs = record.get("event_refs")
        if not isinstance(refs, list) or not refs or any(not isinstance(ref, str) or not ref.strip() for ref in refs):
            report.error("{}: event_refs must be a non-empty list of non-empty strings".format(where))
        check_selector(report, where, package, name, log_index)
        compare_observations(report, where, contract_index[scenario_id]["observations"],
                             record.get("observations"))
    for scenario_id in sorted(required - covered):
        report.error("scenario {} has no evidence record (missing case)".format(scenario_id))
    report.count("evidence_records", len(records))
    report.count("scenarios_covered", len(covered & set(contract_index)))
    report.count("scenarios_required", len(required))
    report.count("scenarios_verified", len(covered & required))


# ---------------------------------------------------------------------------
# Schema v2: explicit event-integrity contract
# ---------------------------------------------------------------------------


def _event_repo_path(path):
    if os.path.isabs(path):
        return path
    return os.path.join(EVENT_REPO_ROOT, path)


def _event_int(value):
    return type(value) is int


def load_event_graph(report, contract, plan_graph):
    """Load the graph that owns the event test registry.

    The v2 contract carries graph_ref so the plain 04.1 command needs no extra
    flag; --plan-graph (used by 04.5) overrides it for the active graph.
    """
    raw = plan_graph if plan_graph else contract.get("graph_ref")
    if not isinstance(raw, str) or not raw.strip():
        report.error("event contract requires graph_ref (or --plan-graph)")
        return None
    path = os.path.abspath(raw) if plan_graph else _event_repo_path(raw)
    try:
        return lib.load_json_object(path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return None


def index_event_graph(report, graph):
    leaves = graph.get("leaves")
    registry = graph.get("test_registry")
    if not isinstance(leaves, list) or not leaves:
        report.error("task-graph.leaves must be a non-empty list")
        return None
    if not isinstance(registry, list) or not registry:
        report.error("task-graph.test_registry must be a non-empty list")
        return None
    leaf_by_id = {}
    for position, leaf in enumerate(leaves):
        if not isinstance(leaf, dict) or not isinstance(leaf.get("id"), str):
            report.error("task-graph.leaves[{}] must declare a string id".format(position))
            continue
        if leaf["id"] in leaf_by_id:
            report.error("task-graph has duplicate leaf id {}".format(leaf["id"]))
            continue
        leaf_by_id[leaf["id"]] = leaf
    registry_by_id = {}
    for position, entry in enumerate(registry):
        if not isinstance(entry, dict):
            report.error("task-graph.test_registry[{}] must be an object".format(position))
            continue
        contract_id = entry.get("contract_id")
        if not isinstance(contract_id, str) or not contract_id.strip():
            report.error("task-graph.test_registry[{}] must declare contract_id".format(position))
            continue
        if contract_id in registry_by_id:
            report.error("task-graph has duplicate contract_id {}".format(contract_id))
            continue
        registry_by_id[contract_id] = entry
    return {"leaves": leaf_by_id, "registry": registry_by_id}


def expected_event_subtests(report, leaf, entries):
    """Derive (parent -> {name -> roles}) from one leaf's verification lists."""
    verification = leaf.get("verification")
    if not isinstance(verification, dict):
        report.error("task-graph leaf {} has no verification object".format(leaf.get("id")))
        return None
    parents = {entry.get("parent") for entry in entries if isinstance(entry.get("parent"), str)}
    expected = {parent: {} for parent in parents}
    for role in ("positive", "negative", "consumer"):
        items = verification.get(role)
        if items is None:
            items = []
        if not isinstance(items, list):
            report.error("task-graph leaf {} verification.{} must be a list".format(
                leaf.get("id"), role))
            continue
        for item in items:
            if not isinstance(item, str) or "/" not in item:
                report.error("task-graph leaf {} verification.{} item {!r} must be parent/name".format(
                    leaf.get("id"), role, item))
                continue
            parent, name = item.split("/", 1)
            if parent not in expected:
                report.error("task-graph leaf {} verification.{} references unregistered parent {}".format(
                    leaf.get("id"), role, parent))
                continue
            if not name:
                report.error("task-graph leaf {} verification.{} has an empty subtest name".format(
                    leaf.get("id"), role))
                continue
            expected[parent].setdefault(name, set()).add(role)
    for parent, names in expected.items():
        if not names:
            report.error("task-graph test {} has no named subtests in leaf {}".format(
                parent, leaf.get("id")))
    return expected


def validate_event_families(report, contract):
    families = contract.get("families")
    if not isinstance(families, list):
        report.error("event contract families must be a list")
        return
    ids = []
    for position, family in enumerate(families):
        if not isinstance(family, dict):
            report.error("families[{}] must be an object".format(position))
            continue
        fid = family.get("id")
        name = family.get("name")
        if not isinstance(fid, str) or fid not in EVENT_FAMILIES:
            report.error("families[{}] has invalid id {!r}".format(position, fid))
            continue
        if not isinstance(name, str) or not name.strip():
            report.error("family {} must declare a non-empty name".format(fid))
        ids.append(fid)
    if ids != list(EVENT_FAMILIES):
        report.error("families must declare exactly E1—E8 in order; got {}".format(ids))
    report.count("event_families", len(ids))


def event_tier_labels(report, where, tier):
    if isinstance(tier, str):
        if not tier.strip():
            report.error("{}: tier must be a non-empty string or list".format(where))
            return None
        return [tier]
    if isinstance(tier, list) and tier and all(
            isinstance(item, str) and item.strip() for item in tier):
        return list(tier)
    report.error("{}: tier must be a non-empty string or list of non-empty strings".format(where))
    return None


def event_scope_for_tier(report, where, tier):
    labels = event_tier_labels(report, where, tier)
    if labels is None:
        return None
    if "event-short-final" in labels:
        return "event-short"
    for label in labels:
        if label in EVENT_SCOPES:
            return label
    report.error("{}: tier {} does not identify a registered event scope".format(where, labels))
    return None


def validate_event_test_shape(report, position, test, declared_scopes):
    where = "tests[{}]".format(position)
    if not isinstance(test, dict):
        report.error("{} must be an object".format(where))
        return None
    test_id = test.get("id")
    if not isinstance(test_id, str) or not EVENT_TEST_ID_RE.match(test_id):
        report.error("{} has invalid id {!r}".format(where, test_id))
        return None
    where = "test {}".format(test_id)
    leaf = test.get("leaf")
    if not isinstance(leaf, str) or not leaf.strip():
        report.error("{}: leaf must be a non-empty string".format(where))
    test_name = test.get("test_name")
    if not isinstance(test_name, str) or not TEST_NAME_RE.match(test_name):
        report.error("{}: test_name {!r} must look like a Go test function".format(where, test_name))
    if isinstance(leaf, str) and isinstance(test_name, str) and test_id != leaf + "/" + test_name:
        report.error("{}: id must equal leaf/TestName".format(where))
    kind = test.get("kind")
    if kind not in EVENT_TEST_KINDS:
        report.error("{}: kind {!r} not in {}".format(where, kind, EVENT_TEST_KINDS))
    for field in ("module", "module_path", "dir", "test_package", "file", "selector"):
        value = test.get(field)
        if not isinstance(value, str) or not value.strip():
            report.error("{}: {} must be a non-empty string".format(where, field))
    selector = test.get("selector")
    if isinstance(selector, str) and not selector.strip():
        report.error("{}: empty selector".format(where))
    tags = test.get("tags")
    if not isinstance(tags, list) or any(
            not isinstance(tag, str) or not tag.strip() for tag in tags):
        report.error("{}: tags must be a list of non-empty strings".format(where))
    if test.get("race") is not True:
        report.error("{}: race must be true (all event tests run under -race)".format(where))
    tier = test.get("tier")
    scope = test.get("scope")
    derived_scope = event_scope_for_tier(report, where, tier)
    if not isinstance(scope, str) or scope not in declared_scopes:
        report.error("{}: scope {!r} is not a registered event scope".format(where, scope))
    elif derived_scope is not None and scope != derived_scope:
        report.error("{}: scope {!r} disagrees with tier {}".format(where, scope, tier))
    labels = event_tier_labels(report, where, tier) or []
    derived_kind = "diagnostic" if "diagnostic-baseline" in labels else "product-green"
    if kind in EVENT_TEST_KINDS and kind != derived_kind:
        report.error("{}: kind {!r} disagrees with tier {} (expected {!r})".format(
            where, kind, tier, derived_kind))
    subtests = test.get("subtests")
    if not isinstance(subtests, list) or not subtests:
        report.error("{}: subtests must be a non-empty list".format(where))
    anchor = test.get("consumer_anchor")
    if not isinstance(anchor, str) or not anchor.strip():
        report.error("{}: consumer_anchor must be a non-empty string".format(where))
    return test


def validate_event_subtests(report, test, expected_by_name):
    test_id = test.get("id")
    parent = test.get("test_name")
    where = "test {}".format(test_id)
    subtests = test.get("subtests")
    if not isinstance(subtests, list) or not subtests:
        return {}
    by_name = {}
    for position, subtest in enumerate(subtests):
        swhere = "{} subtests[{}]".format(where, position)
        if not isinstance(subtest, dict):
            report.error("{} must be an object".format(swhere))
            continue
        name = subtest.get("name")
        if not isinstance(name, str) or not EVENT_SUBTEST_RE.match(name):
            report.error("{} has invalid name {!r}".format(swhere, name))
            continue
        swhere = "subtest ({},{})".format(parent, name)
        if name in by_name:
            report.error("{}: duplicate subtest entity registration for the same (parent,name) key".format(
                swhere))
            continue
        roles = subtest.get("roles")
        if (not isinstance(roles, list) or not roles
                or any(not isinstance(role, str) or role not in EVENT_ROLES for role in roles)):
            report.error("{}: roles must be a non-empty list drawn from {}".format(
                swhere, EVENT_ROLES))
            roles = []
        if len(set(roles)) != len(roles):
            report.error("{}: roles must be unique".format(swhere))
        families = subtest.get("families")
        if (not isinstance(families, list) or not families
                or any(not isinstance(family, str) or family not in EVENT_FAMILIES
                       for family in families)):
            report.error("{}: families must be a non-empty list drawn from E1—E8".format(swhere))
            families = []
        if len(set(families)) != len(families):
            report.error("{}: families must be unique".format(swhere))
        if subtest.get("mandatory") is not True:
            report.error("{}: mandatory must be true (all registered event anchors are required)".format(
                swhere))
        by_name[name] = subtest
    if expected_by_name is None:
        return by_name
    expected_names = set(expected_by_name)
    actual_names = set(by_name)
    for name in sorted(expected_names - actual_names):
        report.error("subtest ({},{}): listed by task-graph but missing from event contract".format(
            parent, name))
    for name in sorted(actual_names - expected_names):
        report.error("subtest ({},{}): present in event contract but not listed by task-graph".format(
            parent, name))
    for name in sorted(expected_names & actual_names):
        expected_roles = expected_by_name[name]
        actual_roles = set(by_name[name].get("roles") or [])
        for role in sorted(expected_roles - actual_roles):
            report.error("subtest ({},{}): missing role {!r} required by task-graph".format(
                parent, name, role))
        for role in sorted(actual_roles - expected_roles):
            # A dual-role consumer row is legal only as the declared anchor and only
            # when it reuses a positive row; standalone consumer rows come from graph.
            allowed_dual = (role == "consumer" and name == test.get("consumer_anchor")
                            and "positive" in actual_roles)
            if not allowed_dual:
                report.error("subtest ({},{}): unexpected role {!r} not listed by task-graph".format(
                    parent, name, role))
    positive = [name for name, sub in by_name.items()
                if "positive" in (sub.get("roles") or [])]
    negative = [name for name, sub in by_name.items()
                if "negative" in (sub.get("roles") or [])]
    if not positive:
        report.error("{}: no positive subtest".format(where))
    if not negative:
        report.error("{}: no negative subtest".format(where))
    anchor = test.get("consumer_anchor")
    if isinstance(anchor, str):
        subtest = by_name.get(anchor)
        if subtest is None:
            report.error("{}: consumer_anchor {!r} is not a registered subtest of this parent".format(
                where, anchor))
        elif "consumer" not in (subtest.get("roles") or []):
            report.error("{}: consumer_anchor {!r} does not have the 'consumer' role".format(
                where, anchor))
    return by_name


def validate_event_family_coverage(report, tests):
    covered = {}
    for test in tests:
        if not isinstance(test, dict) or test.get("kind") != "product-green":
            continue
        for subtest in test.get("subtests") or []:
            if not isinstance(subtest, dict) or subtest.get("mandatory") is not True:
                continue
            for family in subtest.get("families") or []:
                covered[family] = covered.get(family, 0) + 1
    for family in EVENT_FAMILIES:
        if not covered.get(family):
            report.error("family {} is not covered by any mandatory product-green subtest; "
                         "whole_chain entries are aggregate_only and cannot cover a family".format(
                             family))
    report.count("event_families_covered", len(covered))
    report.extra["family_coverage"] = {family: covered.get(family, 0) for family in EVENT_FAMILIES}


def validate_event_whole_chain(report, contract, test_index):
    entries = contract.get("whole_chain")
    if not isinstance(entries, list) or not entries:
        report.error("whole_chain must be a non-empty list")
        return
    ids = []
    for position, entry in enumerate(entries):
        where = "whole_chain[{}]".format(position)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        eid = entry.get("id")
        if not isinstance(eid, str) or not EVENT_WHOLE_CHAIN_ID_RE.match(eid):
            report.error("{} has invalid id {!r}".format(where, eid))
            continue
        if eid in ids:
            report.error("duplicate whole_chain id {}".format(eid))
        ids.append(eid)
        test_id = entry.get("test_id")
        if not isinstance(test_id, str) or test_id not in test_index:
            report.error("{}: test_id {!r} does not reference a registered event test".format(
                where, test_id))
        if entry.get("aggregate_only") is not True:
            report.error("{}: aggregate_only must be true (whole-chain groups never replace tests)".format(
                where))
        families = entry.get("families")
        if (not isinstance(families, list) or not families
                or any(not isinstance(family, str) or family not in EVENT_FAMILIES
                       for family in families)):
            report.error("{}: families must be a non-empty list drawn from E1—E8".format(where))
        note = entry.get("note")
        if not isinstance(note, str) or not note.strip():
            report.error("{}: note must be a non-empty string".format(where))
    expected_ids = ["EI{:02d}".format(index) for index in range(1, 11)]
    if ids != expected_ids:
        report.error("whole_chain must declare exactly EI01—EI10; got {}".format(ids))
    report.count("event_whole_chain_entries", len(entries))


def validate_event_non_product_leaves(report, contract, graph_index):
    entries = contract.get("non_product_leaves")
    if not isinstance(entries, list) or not entries:
        report.error("non_product_leaves must be a non-empty list")
        return
    seen = set()
    for position, entry in enumerate(entries):
        where = "non_product_leaves[{}]".format(position)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        leaf = entry.get("leaf")
        if not isinstance(leaf, str) or not leaf.strip():
            report.error("{}: leaf must be a non-empty string".format(where))
            continue
        if leaf in seen:
            report.error("non_product_leaves: duplicate leaf {}".format(leaf))
        seen.add(leaf)
        leaf_def = graph_index["leaves"].get(leaf)
        if leaf_def is None:
            report.error("{}: leaf {} not present in task-graph".format(where, leaf))
        elif leaf_def.get("kind") != "non_product":
            report.error("{}: leaf {} is not a non_product leaf".format(where, leaf))
        kind = entry.get("evidence_kind")
        if not isinstance(kind, str) or not kind.strip():
            report.error("{}: evidence_kind must be a non-empty string".format(where))
        checker = entry.get("checker")
        doc_ref = entry.get("doc_ref")
        if checker is not None and not isinstance(checker, str):
            report.error("{}: checker must be a string".format(where))
        if doc_ref is not None and not isinstance(doc_ref, str):
            report.error("{}: doc_ref must be a string".format(where))
        if not ((isinstance(checker, str) and checker.strip())
                or (isinstance(doc_ref, str) and doc_ref.strip())):
            report.error("{}: checker or doc_ref must be non-empty".format(where))
    expected = {leaf_id for leaf_id, leaf in graph_index["leaves"].items()
                if leaf.get("kind") == "non_product"}
    for missing in sorted(expected - seen):
        report.error("non_product_leaves: missing graph non-product leaf {}".format(missing))
    for extra in sorted(seen - expected):
        report.error("non_product_leaves: leaf {} is not a graph non-product leaf".format(extra))
    report.count("event_non_product_leaves", len(seen))


def validate_event_matrix(report, contract):
    matrix = contract.get("matrix")
    if not isinstance(matrix, dict):
        report.error("matrix must be an object")
        return
    for key, expected in EVENT_MATRIX_EXPECTED.items():
        actual = matrix.get(key)
        if not lib.typed_equal(expected, actual):
            report.error("matrix.{} must stay at {} (got {})".format(
                key, lib.describe_value(expected), lib.describe_value(actual)))
    seeds = matrix.get("seeds")
    windows = matrix.get("windows")
    if not isinstance(seeds, list) or not isinstance(windows, list):
        return
    combos = matrix.get("combos")
    sessions = matrix.get("sessions_per_host")
    inputs = matrix.get("inputs_per_session")
    restarts = matrix.get("restarts_per_session")
    per_combo = matrix.get("per_combo")
    total = matrix.get("total")
    if not all(_event_int(value) for value in (combos, sessions, inputs, restarts)):
        report.error("matrix combos/sessions_per_host/inputs_per_session/restarts_per_session "
                     "must be integers")
        return
    if not isinstance(per_combo, dict) or not isinstance(total, dict):
        report.error("matrix per_combo/total must be objects")
        return
    per_inputs = per_combo.get("inputs")
    per_restarts = per_combo.get("restarts")
    total_inputs = total.get("inputs")
    total_restarts = total.get("restarts")
    if not all(_event_int(value) for value in (per_inputs, per_restarts,
                                               total_inputs, total_restarts)):
        report.error("matrix per_combo/total inputs and restarts must be integers")
        return
    if combos != len(seeds) * len(windows):
        report.error("matrix.combos {} must equal len(seeds)*len(windows) {}".format(
            combos, len(seeds) * len(windows)))
    if per_inputs != sessions * inputs:
        report.error("matrix.per_combo.inputs {} must equal sessions*inputs {}".format(
            per_inputs, sessions * inputs))
    if per_restarts != sessions * restarts:
        report.error("matrix.per_combo.restarts {} must equal sessions*restarts {}".format(
            per_restarts, sessions * restarts))
    if total_inputs != per_inputs * combos:
        report.error("matrix.total.inputs {} must equal per_combo.inputs*combos {}".format(
            total_inputs, per_inputs * combos))
    if total_restarts != per_restarts * combos:
        report.error("matrix.total.restarts {} must equal per_combo.restarts*combos {}".format(
            total_restarts, per_restarts * combos))
    report.count("event_matrix_inputs_total", total_inputs)
    report.count("event_matrix_restarts_total", total_restarts)
    report.count("event_matrix_combos", combos)


def validate_event_scopes(report, contract, tests, graph, graph_index):
    scopes = contract.get("scopes")
    if not isinstance(scopes, list) or not scopes:
        report.error("scopes must be a non-empty list")
        return set()
    names = []
    declared = {}
    for position, scope in enumerate(scopes):
        where = "scopes[{}]".format(position)
        if not isinstance(scope, dict):
            report.error("{} must be an object".format(where))
            continue
        name = scope.get("name")
        if not isinstance(name, str) or not name.strip():
            report.error("{}: name must be a non-empty string".format(where))
            continue
        if name not in EVENT_SCOPES:
            report.error("{}: unknown event scope {!r}".format(where, name))
        if name in declared:
            report.error("duplicate event scope {}".format(name))
        names.append(name)
        declared[name] = scope
    if sorted(names) != sorted(EVENT_SCOPES):
        report.error("scopes must declare exactly {}; got {}".format(
            list(EVENT_SCOPES), sorted(names)))
    counts = graph.get("counts") if isinstance(graph.get("counts"), dict) else {}
    short_policy = counts.get("short_policy") if isinstance(counts.get("short_policy"), dict) else {}
    long_matrix = counts.get("long_matrix") if isinstance(counts.get("long_matrix"), dict) else {}
    repeat_expected = {
        "event-short": short_policy.get("runs"),
        "event-long": long_matrix.get("repeat_per_combo"),
    }
    for name in EVENT_SCOPES:
        scope = declared.get(name)
        if scope is None:
            continue
        repeat = scope.get("repeat")
        expected_repeat = repeat_expected.get(name)
        if not _event_int(repeat):
            report.error("scope {}: repeat must be an integer".format(name))
        elif not _event_int(expected_repeat):
            report.error("task-graph counts do not declare the repeat policy for scope {}".format(name))
        elif repeat != expected_repeat:
            report.error("scope {}: repeat {} must equal task-graph policy {}".format(
                name, repeat, expected_repeat))
        selected = [test for test in tests
                    if isinstance(test, dict) and test.get("scope") == name]
        computed_parents = len(selected)
        computed_anchors = sum(len(test.get("subtests") or []) for test in selected)
        if scope.get("parent_tests") != computed_parents:
            report.error("scope {}: parent_tests {} must equal computed {}".format(
                name, scope.get("parent_tests"), computed_parents))
        if scope.get("named_anchors") != computed_anchors:
            report.error("scope {}: named_anchors {} must equal computed {}".format(
                name, scope.get("named_anchors"), computed_anchors))
        report.count("event_scope_{}_parents".format(name.replace("-", "_")), computed_parents)
        report.count("event_scope_{}_anchors".format(name.replace("-", "_")), computed_anchors)
    long_entries = [contract_id for contract_id, entry in graph_index["registry"].items()
                    if entry.get("tier") == "event-long"]
    long_anchors = counts.get("long_parent_anchors")
    long_scope = declared.get("event-long")
    if long_scope is not None:
        if long_scope.get("parent_tests") != len(long_entries):
            report.error("scope event-long: parent_tests must equal registry event-long tests {}".format(
                len(long_entries)))
        if _event_int(long_anchors) and long_scope.get("named_anchors") != long_anchors:
            report.error("scope event-long: named_anchors must equal task-graph long_parent_anchors {}".format(
                long_anchors))
    short_scope = declared.get("event-short")
    if short_scope is not None:
        expected_short_parents = len(graph_index["registry"]) - len(long_entries)
        if short_scope.get("parent_tests") != expected_short_parents:
            report.error("scope event-short: parent_tests must equal all non-long tests {}".format(
                expected_short_parents))
        total_anchors = counts.get("named_anchors_after_all_fixes")
        if _event_int(total_anchors) and _event_int(long_anchors):
            expected_short_anchors = total_anchors - long_anchors
            if short_scope.get("named_anchors") != expected_short_anchors:
                report.error("scope event-short: named_anchors must equal total-long {}".format(
                    expected_short_anchors))
        diagnostic_ids = [contract_id for contract_id, entry in graph_index["registry"].items()
                          if isinstance(entry.get("tier"), list)
                          and "diagnostic-baseline" in entry["tier"]]
        for contract_id in diagnostic_ids:
            test = next((item for item in tests
                         if isinstance(item, dict) and item.get("id") == contract_id), None)
            if test is not None and test.get("scope") != "event-short":
                report.error("diagnostic test {} must be included in event-short".format(contract_id))
    return set(names)


def _event_string_end(text, index):
    """Return the index just past the Go string literal that starts at `index`."""
    quote = text[index]
    index += 1
    while index < len(text):
        char = text[index]
        if quote == '"' and char == "\\":
            index += 2
            continue
        if char == quote:
            return index + 1
        index += 1
    return len(text)


def _event_top_level_runs(block):
    """Collect t.Run names written directly in one test function body.

    A t.Run nested inside another subtest closure implements that anchor; it is
    not a separate contract row and must never be counted as one.  Braces in
    strings and comments do not change the nesting depth, so a data literal
    cannot make a nested call look top-level (or vice versa).
    """
    names = set()
    depth = 0
    body_depth = None
    index = 0
    limit = len(block)
    while index < limit:
        char = block[index]
        if char == "/" and index + 1 < limit and block[index + 1] == "/":
            end = block.find("\n", index)
            index = limit if end == -1 else end + 1
            continue
        if char == "/" and index + 1 < limit and block[index + 1] == "*":
            end = block.find("*/", index + 2)
            index = limit if end == -1 else end + 2
            continue
        if char in ('"', "`"):
            index = _event_string_end(block, index)
            continue
        if char == "'":
            index += 1
            while index < limit:
                if block[index] == "\\":
                    index += 2
                    continue
                if block[index] == "'":
                    index += 1
                    break
                index += 1
            continue
        if char == "{":
            depth += 1
            if body_depth is None:
                body_depth = depth
            index += 1
            continue
        if char == "}":
            if body_depth is not None and depth == body_depth:
                break
            if depth > 0:
                depth -= 1
            index += 1
            continue
        if (body_depth is not None and depth == body_depth
                and block.startswith("t.Run(", index)
                and (index == 0 or not (block[index - 1].isalnum() or block[index - 1] == "_"))):
            cursor = index + len("t.Run(")
            while cursor < limit and block[cursor] in " \t\r\n":
                cursor += 1
            if cursor < limit and block[cursor] in ('"', "`"):
                end = _event_string_end(block, cursor)
                names.add(block[cursor + 1:end - 1])
            index = cursor
            continue
        index += 1
    return names


def _event_test_blocks(text):
    matches = list(EVENT_TEST_FUNC_RE.finditer(text))
    blocks = {}
    for index, match in enumerate(matches):
        name = match.group(1)
        if not name.startswith("TestEventIntegrity"):
            continue
        end = matches[index + 1].start() if index + 1 < len(matches) else len(text)
        blocks[name] = _event_top_level_runs(text[match.end():end])
    return blocks


def validate_event_source_only(report, graph, tests):
    """Validate graph-declared parents that are intentionally outside the contract."""
    entries = graph.get("source_only_tests")
    if entries is None:
        return {}
    if not isinstance(entries, list):
        report.error("task-graph.source_only_tests must be a list")
        return {}
    contract_names = {test.get("test_name") for test in tests if isinstance(test, dict)}
    contract_ids = {test.get("id") for test in tests if isinstance(test, dict)}
    leaf_by_id = {leaf.get("id"): leaf for leaf in graph.get("leaves") or []
                  if isinstance(leaf, dict)}
    registry_by_leaf = {}
    for entry in graph.get("test_registry") or []:
        if isinstance(entry, dict):
            registry_by_leaf.setdefault(entry.get("leaf"), []).append(entry)
    by_parent = {}
    for position, entry in enumerate(entries):
        where = "task-graph.source_only_tests[{}]".format(position)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        parent = entry.get("parent")
        if not isinstance(parent, str) or not parent.strip():
            report.error("{}: parent must be a non-empty string".format(where))
            continue
        if parent in by_parent:
            report.error("{}: duplicate source-only parent {}".format(where, parent))
            continue
        status = entry.get("status")
        if status not in ("superseded", "off_contract"):
            report.error("{}: status {!r} must be 'superseded' or 'off_contract'".format(
                where, status))
        if parent in contract_names:
            report.error("{}: source-only test {} must not also be registered in the event "
                         "contract".format(where, parent))
        if not isinstance(entry.get("file"), str) or not entry.get("file").strip():
            report.error("{}: file must be a non-empty string".format(where))
        leaf_id = entry.get("leaf")
        leaf = leaf_by_id.get(leaf_id)
        if leaf is None:
            report.error("{}: leaf {} is not present in task-graph".format(where, leaf_id))
        if status == "superseded":
            superseded_by = entry.get("superseded_by")
            if (not isinstance(superseded_by, list) or not superseded_by
                    or any(not isinstance(item, str) or item not in contract_ids
                           for item in superseded_by)):
                report.error("{}: superseded_by must list registered contract ids".format(where))
        if status == "off_contract":
            if isinstance(leaf, dict) and leaf.get("kind") != "non_product":
                report.error("{}: off_contract test {} must belong to a non_product "
                             "leaf".format(where, parent))
            if isinstance(leaf_id, str) and registry_by_leaf.get(leaf_id):
                report.error("{}: off_contract leaf {} must not have test_registry "
                             "entries".format(where, leaf_id))
        by_parent[parent] = entry
    report.count("event_source_only_tests", len(by_parent))
    report.extra["source_only"] = sorted(by_parent)
    return by_parent


def _event_gated_pending_anchor(leaf, parent, name, test):
    """Decide whether a missing anchor is an explicitly gated pending anchor.

    Two mechanisms are accepted, both of which keep the anchor inside the
    frozen acceptance surface while the source lands later:

    * ``leaf.verification.pending_anchors`` names the anchor verbatim.  It may
      carry any role; 01.2/01.3's 补差 anchors (one negative, one positive) are
      declared this way because the parent test already exists in the source
      tree and only the new subtest is outstanding.
    * The older positive-only ``hard_gate`` mechanism (02.3's G4 compaction
      writer) still applies when no pending_anchors row names the anchor.

    Negative and consumer anchors outside pending_anchors are never pending:
    the contract's protective rows cannot be waived by a gate, they either
    exist or the contract is broken.
    """
    if not isinstance(leaf, dict):
        return None
    rows = [subtest for subtest in test.get("subtests") or []
            if isinstance(subtest, dict) and subtest.get("name") == name]
    if not rows:
        return None
    verification = leaf.get("verification")
    if not isinstance(verification, dict):
        return None
    anchor = "{}/{}".format(parent, name)
    declared = verification.get("pending_anchors")
    if isinstance(declared, list) and anchor in declared:
        return {"test_id": test.get("id"), "leaf": leaf.get("id"),
                "anchor": anchor, "pending_anchors": list(declared)}
    if set(rows[0].get("roles") or []) != {"positive"}:
        return None
    gate = verification.get("hard_gate")
    if not isinstance(gate, str) or not gate.strip():
        return None
    if not re.search(r"(?<![A-Za-z0-9_]){}(?![A-Za-z0-9_])".format(re.escape(name)), gate):
        return None
    return {"test_id": test.get("id"), "leaf": leaf.get("id"),
            "anchor": anchor, "hard_gate": gate}


def validate_event_sources(report, contract, tests, source_only=None, graph=None,
                           repo_root=None):
    source_only = source_only or {}
    by_parent = {}
    for test in tests:
        if not isinstance(test, dict):
            continue
        parent = test.get("test_name")
        if isinstance(parent, str):
            if parent in by_parent:
                report.error("duplicate event test_name {} across contract tests".format(parent))
            by_parent[parent] = test
    leaf_by_id = {}
    if isinstance(graph, dict):
        for leaf in graph.get("leaves") or []:
            if isinstance(leaf, dict) and isinstance(leaf.get("id"), str):
                leaf_by_id[leaf["id"]] = leaf
    root = repo_root or EVENT_REPO_ROOT
    discovered = {}
    for source_root in (os.path.join(root, "tests"), os.path.join(root, "examples"),
                        os.path.join(root, "tool")):
        if not os.path.isdir(source_root):
            continue
        for dirpath, dirnames, filenames in os.walk(source_root):
            dirnames[:] = sorted(name for name in dirnames if not name.startswith("."))
            for filename in sorted(filenames):
                if not filename.endswith("_test.go"):
                    continue
                path = os.path.join(dirpath, filename)
                rel = os.path.relpath(path, root).replace(os.sep, "/")
                try:
                    with open(path, "r", encoding="utf-8") as handle:
                        text = handle.read()
                except OSError as exc:
                    report.error("cannot read source test file {}: {}".format(rel, exc))
                    continue
                for parent, names in _event_test_blocks(text).items():
                    if parent in discovered:
                        report.error("source test {} is declared in more than one file".format(parent))
                        continue
                    discovered[parent] = {"file": rel, "subtests": sorted(names)}
    registered = {}
    skipped = {}
    for parent, info in sorted(discovered.items()):
        entry = source_only.get(parent)
        if entry is not None:
            skipped[parent] = info
            if info["file"] != entry.get("file"):
                report.error("source-only test {} is in {} but task-graph declares {}".format(
                    parent, info["file"], entry.get("file")))
            continue
        registered[parent] = info
    pending_anchors = []
    unregistered = []
    for parent, info in sorted(registered.items()):
        test = by_parent.get(parent)
        if test is None:
            report.error("source test {} ({}) is not registered in the event contract".format(
                parent, info["file"]))
            continue
        if test.get("file") != info["file"]:
            report.error("source test {} is in {} but the contract declares {}".format(
                parent, info["file"], test.get("file")))
        expected = {subtest.get("name") for subtest in test.get("subtests") or []
                    if isinstance(subtest, dict)}
        actual = set(info["subtests"])
        leaf = leaf_by_id.get(test.get("leaf"))
        for name in sorted(expected - actual):
            gated = _event_gated_pending_anchor(leaf, parent, name, test)
            if gated is not None:
                pending_anchors.append(gated)
                continue
            report.error("source test {}: missing t.Run(\"{}\") required by the contract".format(
                parent, name))
        for name in sorted(actual - expected):
            # The contract is the frozen acceptance surface: a product leaf may
            # add an anchor ahead of it, but that anchor is not silently folded
            # into the contract's counts.  It is listed as unregistered so the
            # next contract revision has to own it; a *renamed* row still fails
            # through the missing branch above.
            unregistered.append({"test_id": test.get("id"),
                                 "anchor": "{}/{}".format(parent, name)})
            report.warn("source test {}: t.Run(\"{}\") is not registered in the event "
                        "contract; the contract stays the acceptance surface and this anchor "
                        "is listed, not counted".format(parent, name))
    absent_source_only = sorted(parent for parent in source_only if parent not in discovered)
    for parent in absent_source_only:
        report.warn("source-only test {} is declared by task-graph.source_only_tests but no such "
                    "TestEventIntegrity function is present in the source tree; it is outside "
                    "the event contract, so the mapping gate lists it as absent rather than "
                    "failing the run".format(parent))
    pending = [test.get("id") for test in tests
               if isinstance(test, dict) and test.get("test_name") not in discovered]
    report.count("event_source_discovered_tests", len(registered))
    report.count("event_source_discovered_subtests",
                 sum(len(info["subtests"]) for info in registered.values()))
    report.count("event_source_source_only_discovered_tests", len(skipped))
    report.count("event_source_only_absent_tests", len(absent_source_only))
    report.count("event_source_pending_tests", len(pending))
    report.count("event_pending_anchors", len(pending_anchors))
    report.count("event_unregistered_source_anchors", len(unregistered))
    report.extra["source_discovered"] = {parent: info for parent, info in sorted(registered.items())}
    report.extra["source_only_discovered"] = sorted(skipped)
    report.extra["source_only_absent"] = absent_source_only
    report.extra["source_pending_test_ids"] = sorted(pending)
    report.extra["pending_anchors"] = sorted(pending_anchors, key=lambda item: str(item["anchor"]))
    report.extra["unregistered_source_anchors"] = sorted(
        unregistered, key=lambda item: str(item["anchor"]))
    if pending:
        report.note("{} contract test(s) are not present in the source tree yet (graph state "
                    "to_be_added); their selectors remain graph-frozen and are enforced once the "
                    "tests are added. Execution logs (04.2) and the 04.5 mapping gate close the "
                    "remaining gap.".format(len(pending)))
    if pending_anchors:
        named = ", ".join(item["anchor"] for item in pending_anchors)
        report.note("{} anchor(s) are declared pending by leaf verification.pending_anchors "
                    "or a hard gate: {}".format(len(pending_anchors), named))
    if unregistered:
        named = ", ".join(item["anchor"] for item in unregistered)
        report.note("{} source anchor(s) are not yet registered in the frozen contract: {}"
                    .format(len(unregistered), named))
    return {"discovered": discovered, "registered": registered, "pending": pending,
            "pending_anchors": pending_anchors}


def validate_event_selectors(report, tests, discovered):
    """Each selector must be a real, unique regex that selects its own parent.

    A selector that does not compile and fully match the declared test name
    would run nothing (or a sibling) in `go test -run`; both are silent holes.
    Parents not yet present in the source tree stay pending and are listed, not
    silently treated as verified.
    """
    checked = []
    pending = []
    mismatches = 0
    seen = {}
    for test in tests:
        if not isinstance(test, dict):
            continue
        test_id = test.get("id")
        selector = test.get("selector")
        name = test.get("test_name")
        if not isinstance(selector, str) or not selector.strip():
            # The shape check already reports the empty selector; count it here
            # so the aggregate cannot look clean.
            mismatches += 1
            continue
        try:
            pattern = re.compile(selector)
        except re.error as exc:
            mismatches += 1
            report.error("test {}: selector {!r} is not a valid regular expression: {}".format(
                test_id, selector, exc))
            continue
        if selector in seen:
            mismatches += 1
            report.error("test {}: selector {!r} is already used by test {}".format(
                test_id, selector, seen[selector]))
            continue
        seen[selector] = test_id
        if not isinstance(name, str) or pattern.fullmatch(name) is None:
            mismatches += 1
            report.error("test {}: selector {!r} does not select the declared test {}".format(
                test_id, selector, name))
            continue
        if name not in discovered:
            pending.append(test_id)
            continue
        checked.append(test_id)
    report.count("event_anchor_selectors_checked", len(checked))
    report.count("event_anchor_selectors_pending", len(pending))
    report.count("event_anchor_selector_mismatches", mismatches)
    report.extra["selector_checked"] = sorted(checked)
    report.extra["selector_pending"] = sorted(pending)


def validate_event_leaf_coverage(report, contract, graph_index, tests):
    """Every graph leaf is covered by a registered test or a non-product entry.

    Also checks the reverse direction that previously had no owner: a registry
    entry may not point at a leaf that no longer exists, and each leaf's own
    `tests` list must equal the registry ids assigned to it.  That is what makes
    a renamed or deleted leaf fail instead of silently dropping coverage.
    """
    leaves = graph_index["leaves"]
    registry = graph_index["registry"]
    registry_by_leaf = {}
    seen_registry_ids = {}
    for contract_id, entry in registry.items():
        leaf_id = entry.get("leaf")
        if leaf_id not in leaves:
            report.error("task-graph test_registry {} references leaf {} that is not declared "
                         "in task-graph.leaves".format(contract_id, leaf_id))
            continue
        registry_id = entry.get("id")
        if not isinstance(registry_id, str) or not registry_id.strip():
            report.error("task-graph test_registry {} must declare id".format(contract_id))
            continue
        if registry_id in seen_registry_ids:
            report.error("task-graph duplicates test_registry id {} ({} and {})".format(
                registry_id, seen_registry_ids[registry_id], contract_id))
            continue
        seen_registry_ids[registry_id] = contract_id
        registry_by_leaf.setdefault(leaf_id, []).append(registry_id)
    non_product_ids = set()
    for entry in contract.get("non_product_leaves") or []:
        if isinstance(entry, dict) and isinstance(entry.get("leaf"), str):
            non_product_ids.add(entry["leaf"])
    covered = set()
    product = 0
    non_product = 0
    for leaf_id, leaf in sorted(leaves.items()):
        kind = leaf.get("kind")
        declared = leaf.get("tests")
        if not isinstance(declared, list) or any(
                not isinstance(item, str) or not item.strip() for item in declared):
            report.error("task-graph leaf {} tests must be a list of test_registry ids".format(
                leaf_id))
            declared = []
        expected = sorted(registry_by_leaf.get(leaf_id, []))
        if sorted(declared) != expected:
            report.error("task-graph leaf {} tests {} do not match its test_registry ids {}".format(
                leaf_id, sorted(declared), expected))
        if kind == "product":
            product += 1
            if expected:
                covered.add(leaf_id)
        elif kind == "non_product":
            non_product += 1
            if leaf_id in non_product_ids:
                covered.add(leaf_id)
        else:
            report.error("task-graph leaf {} kind {!r} is neither product nor non_product".format(
                leaf_id, kind))
    gaps = sorted(leaf_id for leaf_id in leaves if leaf_id not in covered)
    for leaf_id in gaps:
        report.error("task-graph leaf {} is covered by neither a registered test nor a "
                     "non_product evidence entry".format(leaf_id))
    report.count("event_leaves_total", len(leaves))
    report.count("event_leaves_product", product)
    report.count("event_leaves_non_product", non_product)
    report.count("event_leaves_covered", len(covered))
    report.extra["leaf_coverage_gaps"] = gaps
    report.extra["leaf_coverage"] = {leaf_id: (leaf_id in covered) for leaf_id in sorted(leaves)}


def validate_event_graph_counts(report, graph, tests):
    counts = graph.get("counts")
    if not isinstance(counts, dict):
        report.error("task-graph.counts must be an object")
        return
    green_tests = [test for test in tests
                   if isinstance(test, dict) and test.get("kind") == "product-green"]
    diagnostic_tests = [test for test in tests
                        if isinstance(test, dict) and test.get("kind") == "diagnostic"]
    green_anchors = sum(len(test.get("subtests") or []) for test in green_tests)
    diagnostic_anchors = sum(len(test.get("subtests") or []) for test in diagnostic_tests)
    total_anchors = green_anchors + diagnostic_anchors
    derived = {
        "parent_tests_total": len(tests),
        "product_parent_tests_green": len(green_tests),
        "diagnostic_parent_tests": len(diagnostic_tests),
        "named_green_subtests_unique": green_anchors,
        "diagnostic_named_anchors": diagnostic_anchors,
        "named_anchors_after_all_fixes": total_anchors,
    }
    for key, value in derived.items():
        if key not in counts:
            report.error("task-graph.counts.{} is required".format(key))
        elif counts.get(key) != value:
            report.error("task-graph.counts.{}={} does not match the derived list length {}".format(
                key, counts.get(key), value))
    report.count("event_tests_total", len(tests))
    report.count("event_product_green_tests", len(green_tests))
    report.count("event_diagnostic_tests", len(diagnostic_tests))
    report.count("event_subtests_unique", total_anchors)
    report.count("event_green_subtests", green_anchors)
    report.count("event_diagnostic_subtests", diagnostic_anchors)
    report.count("event_total_anchors", total_anchors)
    role_counts = {}
    dual_role = 0
    for test in tests:
        for subtest in test.get("subtests") or []:
            if not isinstance(subtest, dict):
                continue
            roles = subtest.get("roles") or []
            if len(roles) > 1:
                dual_role += 1
            for role in roles:
                role_counts[role] = role_counts.get(role, 0) + 1
    report.count("event_dual_role_subtests", dual_role)
    report.extra["role_counts"] = role_counts


def validate_event_contract(report, contract, contract_path, plan_graph, repo_root=None):
    if contract.get("schema_version") != EVENT_SCHEMA_VERSION:
        report.error("event contract schema_version must be 2, got {!r}".format(
            contract.get("schema_version")))
        return
    if contract.get("profile") != EVENT_PROFILE:
        report.error("event contract profile must be {!r}, got {!r}".format(
            EVENT_PROFILE, contract.get("profile")))
    graph = load_event_graph(report, contract, plan_graph)
    if graph is None:
        return
    if isinstance(contract.get("change"), str) and isinstance(graph.get("change"), str):
        if contract["change"] != graph["change"]:
            report.error("event contract change {!r} does not match task-graph change {!r}".format(
                contract["change"], graph["change"]))
    graph_index = index_event_graph(report, graph)
    if graph_index is None:
        return
    validate_event_families(report, contract)
    tests = contract.get("tests")
    if not isinstance(tests, list) or not tests:
        report.error("event contract tests must be a non-empty list")
        return
    declared_scopes = {scope.get("name") for scope in contract.get("scopes") or []
                       if isinstance(scope, dict) and isinstance(scope.get("name"), str)}
    test_index = {}
    for position, test in enumerate(tests):
        shaped = validate_event_test_shape(report, position, test, declared_scopes)
        if shaped is None:
            continue
        test_id = shaped["id"]
        if test_id in test_index:
            report.error("duplicate test id {}".format(test_id))
            continue
        test_index[test_id] = shaped
    expected_by_leaf = {}
    for leaf_id, leaf in graph_index["leaves"].items():
        entries = [entry for entry in graph_index["registry"].values()
                   if entry.get("leaf") == leaf_id]
        if leaf.get("kind") == "product":
            if not entries:
                report.error("task-graph product leaf {} has no test_registry entry".format(leaf_id))
                continue
            expected_by_leaf[leaf_id] = expected_event_subtests(report, leaf, entries)
        elif entries:
            report.error("task-graph non_product leaf {} must not have test_registry entries".format(
                leaf_id))
    graph_ids = set(graph_index["registry"])
    contract_ids = set(test_index)
    for missing in sorted(graph_ids - contract_ids):
        report.error("graph test {} is missing from event contract".format(missing))
    for extra in sorted(contract_ids - graph_ids):
        report.error("event contract test {} is not present in task-graph test_registry".format(extra))
    for test_id in sorted(graph_ids & contract_ids):
        test = test_index[test_id]
        entry = graph_index["registry"][test_id]
        leaf = graph_index["leaves"].get(entry.get("leaf"))
        where = "test {}".format(test_id)
        for field, expected in (("leaf", entry.get("leaf")),
                                ("module", entry.get("module")),
                                ("module_path", entry.get("module_path")),
                                ("dir", entry.get("dir")),
                                ("test_package", entry.get("package")),
                                ("test_name", entry.get("parent")),
                                ("file", entry.get("file")),
                                ("selector", entry.get("selector"))):
            if test.get(field) != expected:
                report.error("{}: {} {!r} does not match task-graph {!r}".format(
                    where, field, test.get(field), expected))
        if not lib.typed_equal(test.get("tags"), entry.get("tags")):
            report.error("{}: tags {} do not match task-graph {}".format(
                where, test.get("tags"), entry.get("tags")))
        if not lib.typed_equal(test.get("tier"), entry.get("tier")):
            report.error("{}: tier {} does not match task-graph {}".format(
                where, test.get("tier"), entry.get("tier")))
        if isinstance(leaf, dict):
            commands = (leaf.get("verification") or {}).get("commands") or []
            expected_race = any(isinstance(command, str) and "-race" in command
                                for command in commands)
            if test.get("race") is not expected_race:
                report.error("{}: race {!r} does not match task-graph commands (expected {!r})".format(
                    where, test.get("race"), expected_race))
        expected_for_leaf = expected_by_leaf.get(entry.get("leaf")) or {}
        validate_event_subtests(report, test, expected_for_leaf.get(entry.get("parent")))
    validate_event_family_coverage(report, tests)
    validate_event_scopes(report, contract, tests, graph, graph_index)
    validate_event_whole_chain(report, contract, test_index)
    validate_event_non_product_leaves(report, contract, graph_index)
    validate_event_leaf_coverage(report, contract, graph_index, tests)
    validate_event_matrix(report, contract)
    validate_event_graph_counts(report, graph, tests)
    source_only = validate_event_source_only(report, graph, tests)
    sources = validate_event_sources(report, contract, tests, source_only, graph=graph,
                                     repo_root=repo_root)
    validate_event_selectors(report, tests, sources["discovered"])


# ---------------------------------------------------------------------------
# Schema v2 execution evidence: one evidence file per independent repeat
# ---------------------------------------------------------------------------

EVENT_RUN_ID_RE = re.compile(
    r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
EVENT_ANCHOR_STATUSES = ("pass", "fail", "skip", "running", "missing")


def load_event_contract_index(report, contract):
    """Index the v2 contract for evidence checking (no source re-discovery).

    Evidence mode judges one run against the frozen contract; the full
    contract/source cross-alignment stays in --check-contract so a synthetic
    fixture cannot pass by accident and a real run never re-parses the tree.
    """
    if contract.get("schema_version") != EVENT_SCHEMA_VERSION:
        report.error("event contract schema_version must be {}, got {!r}".format(
            EVENT_SCHEMA_VERSION, contract.get("schema_version")))
        return None
    if contract.get("profile") != EVENT_PROFILE:
        report.error("event contract profile must be {!r}, got {!r}".format(
            EVENT_PROFILE, contract.get("profile")))
    tests = contract.get("tests")
    if not isinstance(tests, list) or not tests:
        report.error("event contract tests must be a non-empty list")
        return None
    index = {}
    for position, test in enumerate(tests):
        where = "tests[{}]".format(position)
        if not isinstance(test, dict):
            report.error("{} must be an object".format(where))
            continue
        test_id = test.get("id")
        if not isinstance(test_id, str) or not EVENT_TEST_ID_RE.match(test_id):
            report.error("{} has invalid id {!r}".format(where, test_id))
            continue
        if test_id in index:
            report.error("duplicate event test id {}".format(test_id))
            continue
        for field in ("module_path", "test_package", "test_name"):
            value = test.get(field)
            if not isinstance(value, str) or not value.strip():
                report.error("test {}: {} must be a non-empty string".format(test_id, field))
        if not isinstance(test.get("subtests"), list) or not test.get("subtests"):
            report.error("test {}: subtests must be a non-empty list".format(test_id))
        index[test_id] = test
    scopes = {}
    for entry in contract.get("scopes") or []:
        if isinstance(entry, dict) and isinstance(entry.get("name"), str):
            scopes[entry["name"]] = entry
    if not scopes:
        report.error("event contract scopes must register at least one scope")
    matrix = contract.get("matrix") if isinstance(contract.get("matrix"), dict) else {}
    return {"tests": index, "scopes": scopes, "matrix": matrix}


def event_matrix_pairs(report, matrix):
    """Enumerate every (seed, window) pair the contract freezes."""
    if not isinstance(matrix, dict):
        report.error("event contract matrix must be an object")
        return None
    values = {}
    for name in ("seeds", "windows"):
        items = matrix.get(name)
        if (not isinstance(items, list) or not items
                or any(type(item) is not int for item in items)):
            report.error("matrix.{} must be a non-empty list of integers".format(name))
            return None
        values[name] = items
    combos = [(seed, window) for seed in values["seeds"] for window in values["windows"]]
    declared = matrix.get("combos")
    if declared != len(combos):
        report.error("matrix.combos={!r} does not equal the {} seed/window pairs".format(
            declared, len(combos)))
    return combos


def _event_package_rel(report, where, module_path, package):
    """Normalise one contract package to module-relative form (no ./ prefix)."""
    if not isinstance(module_path, str) or not module_path.strip():
        report.error("{}: module_path must be a non-empty string".format(where))
        return None
    if not isinstance(package, str) or not package.strip():
        report.error("{}: test_package must be a non-empty string".format(where))
        return None
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
        report.error("{}: test package {!r} is outside module {!r}".format(
            where, package, module_path))
        return None
    else:
        rel = text
    rel = rel.rstrip("/")
    return rel or "."


def index_event_log(report, rows, module_paths, where):
    """Index a v2 test-log that may hold several `go test` invocations.

    One repeat can run the same package twice (different build tags), so a
    package may start again after it ended. Each start opens a new window; test
    events MUST fall inside the open window, a package MUST NOT end while a test
    under it is open, and a test MUST NOT run twice in one window nor twice in
    one round. A later pass therefore can never cover an earlier fail.
    """
    resolved = sorted(module_paths, key=len, reverse=True)

    def resolve(package):
        for module_path in resolved:
            if package == module_path:
                return module_path, "."
            prefix = module_path + "/"
            if package.startswith(prefix):
                return module_path, package[len(prefix):]
        return None, None

    quiet = ("output", "build-output", "build-fail", "bench")
    windows = {}
    packages = {}
    actions = {}
    foreign = set()
    for row in rows:
        action = row.get("Action")
        package = row.get("Package")
        if not isinstance(action, str) or not isinstance(package, str):
            report.error("{}: malformed test-log row (Action/Package must be strings)".format(where))
            continue
        module_path, rel = resolve(package)
        if module_path is None:
            foreign.add(package)
            continue
        test = row.get("Test", "")
        if test is None:
            test = ""
        if not isinstance(test, str):
            report.error("{}: malformed test-log Test must be a string".format(where))
            continue
        stack = windows.setdefault((module_path, rel), [])
        if not test:
            if action == "start":
                if stack and stack[-1]["state"] != "ended":
                    report.error("{}: package {} started again before its previous terminal".format(
                        where, package))
                stack.append({"state": "running", "open": set()})
            elif action in ("pass", "fail", "skip"):
                if not stack or stack[-1]["state"] not in ("running", "paused"):
                    report.error("{}: package {} terminal {} without an active start".format(
                        where, package, action))
                else:
                    if stack[-1]["open"]:
                        report.error("{}: package {} {} while test(s) {} still open".format(
                            where, package, action, sorted(stack[-1]["open"])))
                    stack[-1]["state"] = "ended"
                if action in ("fail", "skip"):
                    report.error("{}: package {} recorded {} in test-log".format(
                        where, package, action))
            elif action == "pause" and stack and stack[-1]["state"] == "running":
                stack[-1]["state"] = "paused"
            elif action == "cont" and stack and stack[-1]["state"] == "paused":
                stack[-1]["state"] = "running"
            elif action not in quiet:
                report.error("{}: package {} invalid action sequence {}".format(
                    where, package, action))
            counter = packages.setdefault((module_path, rel), {})
            counter[action] = counter.get(action, 0) + 1
            continue
        window = stack[-1] if stack else None
        if window is None or window["state"] not in ("running", "paused"):
            report.error("{}: {}::{} lifecycle {} outside its package start/end window".format(
                where, package, test, action))
        if "/" in test:
            parent = test.rsplit("/", 1)[0]
            if window is None or parent not in window["open"]:
                report.error("{}: subtest {}::{} event {} outside its parent test".format(
                    where, package, test, action))
        if action == "run":
            if window is not None:
                if test in window["open"]:
                    report.error("{}: {}::{} duplicate run".format(where, package, test))
                else:
                    window["open"].add(test)
        elif action in ("pass", "fail", "skip"):
            if window is None or test not in window["open"]:
                report.error("{}: {}::{} terminal {} without an active run".format(
                    where, package, test, action))
            else:
                window["open"].discard(test)
            if action in ("fail", "skip"):
                report.error("{}: {}::{} recorded {} in test-log".format(
                    where, package, test, action))
        elif action == "pause" and window is not None and test in window["open"]:
            pass
        elif action == "cont" and window is not None and test in window["open"]:
            pass
        elif action not in quiet:
            report.error("{}: {}::{} invalid action sequence {}".format(
                where, package, test, action))
        counter = actions.setdefault((module_path, rel, test), {})
        counter[action] = counter.get(action, 0) + 1
    for (module_path, rel), stack in sorted(windows.items()):
        if not stack:
            report.error("{}: package {}::{} has no start".format(where, module_path, rel))
        elif stack[-1]["state"] != "ended":
            report.error("{}: package {}::{} missing terminal event".format(
                where, module_path, rel))
        elif stack[-1]["open"]:
            report.error("{}: package {}::{} ended with open test(s) {}".format(
                where, module_path, rel, sorted(stack[-1]["open"])))
    for (module_path, rel, test), counter in sorted(actions.items()):
        if counter.get("run", 0) > 1:
            report.error("{}: {}::{}::{} ran {} times in one round (duplicate run)".format(
                where, module_path, rel, test, counter["run"]))
    report.count("event_log_packages", len(packages))
    report.count("event_log_tests", len(actions))
    if foreign:
        report.warn("{}: test-log has {} package(s) outside the selected modules".format(
            where, len(foreign)))
    return {"packages": packages, "actions": actions, "foreign": foreign}


def check_event_test_selector(report, where, test, log_index):
    """One selected test must have exactly one run and one pass terminal."""
    module_path = test.get("module_path")
    package = _event_package_rel(report, where, module_path, test.get("test_package"))
    name = test.get("test_name")
    if package is None or not isinstance(name, str) or not name.strip():
        return
    counter = log_index["actions"].get((module_path, package, name), {})
    if not counter:
        report.error("{}: no run/pass in test-log for {}::{}::{} (zero match is a failure, "
                     "even when the process exited 0)".format(where, module_path, package, name))
        return
    runs = counter.get("run", 0)
    passes = counter.get("pass", 0)
    if runs == 0 or passes == 0:
        report.error("{}: {}::{}::{} has no run+pass (run={}, pass={})".format(
            where, module_path, package, name, runs, passes))
    if counter.get("fail", 0):
        report.error("{}: {}::{}::{} recorded fail (a later pass cannot cover it)".format(
            where, module_path, package, name))
    if counter.get("skip", 0):
        report.error("{}: {}::{}::{} was skipped".format(where, module_path, package, name))
    package_counter = log_index["packages"].get((module_path, package), {})
    if package_counter.get("start", 0) < 1:
        report.error("{}: package {}::{} missing start".format(where, module_path, package))
    if package_counter.get("pass", 0) < 1:
        report.error("{}: package {}::{} missing pass terminal".format(
            where, module_path, package))
    if package_counter.get("fail", 0):
        report.error("{}: package {}::{} recorded a package-level fail".format(
            where, module_path, package))
    if package_counter.get("skip", 0):
        report.error("{}: package {}::{} was skipped".format(where, module_path, package))
    prefix = name + "/"
    for (module, rel, observed), sub in sorted(log_index["actions"].items()):
        if (module, rel) != (module_path, package) or not observed.startswith(prefix):
            continue
        if sub.get("skip", 0):
            report.error("{}: subtest {}::{} was skipped".format(where, package, observed))
        if sub.get("fail", 0):
            report.error("{}: subtest {}::{} failed".format(where, package, observed))


def check_event_fingerprints(report, fingerprints):
    if not isinstance(fingerprints, list) or not fingerprints:
        report.error("evidence.module_fingerprints must be a non-empty list")
        return
    seen = set()
    for position, entry in enumerate(fingerprints):
        where = "module_fingerprints[{}]".format(position)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        for field in ("module_path", "dir", "revision", "content_hash"):
            value = entry.get(field)
            if not isinstance(value, str) or not value.strip():
                report.error("{}.{} must be a non-empty string".format(where, field))
        revision = entry.get("revision")
        if isinstance(revision, str) and revision.strip() and not REVISION_RE.match(revision.strip()):
            report.error("{}.revision {!r} is not a readable git SHA".format(where, revision))
        content_hash = entry.get("content_hash")
        if (isinstance(content_hash, str) and content_hash.strip()
                and not CONTENT_HASH_RE.match(content_hash.strip())):
            report.error("{}.content_hash {!r} is not a readable sha256 digest".format(
                where, content_hash))
        key = entry.get("dir")
        if isinstance(key, str):
            if key in seen:
                report.error("{}: duplicate fingerprint for module dir {!r}".format(where, key))
            seen.add(key)


def check_event_run_config(report, run_config, index, selected):
    if not isinstance(run_config, dict):
        report.error("evidence.run_config must be an object")
        return
    seed = run_config.get("seed")
    window = run_config.get("window")
    if seed is not None and type(seed) is not int:
        report.error("run_config.seed must be null or an integer")
    if window is not None and type(window) is not int:
        report.error("run_config.window must be null or an integer")
    if (seed is None) != (window is None):
        report.error("run_config seed/window must be a matrix pair (both or neither)")
    elif seed is not None:
        combos = event_matrix_pairs(report, index["matrix"])
        if combos is not None and (seed, window) not in combos:
            report.error("run_config seed/window {}/{} is not a contract matrix combination"
                         .format(seed, window))
    tags = run_config.get("tags")
    if (not isinstance(tags, list)
            or any(not isinstance(tag, str) or not tag.strip() for tag in tags)):
        report.error("run_config.tags must be a list of non-empty strings")
    else:
        expected = sorted({tag for test_id in selected
                           for tag in (index["tests"][test_id].get("tags") or [])})
        if sorted(set(tags)) != expected:
            report.error("run_config.tags {} do not match the selected tests' declared tags {}"
                         .format(tags, expected))
    if run_config.get("race") is not True:
        report.error("run_config.race must be true (all event tests run under -race)")


def check_event_evidence_shape(report, evidence, index, contract_digest):
    """Validate the v2 evidence envelope; returns the parsed facts or None."""
    if evidence.get("schema_version") != EVENT_SCHEMA_VERSION:
        report.error("evidence schema_version must be {}, got {!r}".format(
            EVENT_SCHEMA_VERSION, evidence.get("schema_version")))
        return None
    if evidence.get("profile") != EVENT_PROFILE:
        report.error("evidence profile must be {!r}, got {!r}".format(
            EVENT_PROFILE, evidence.get("profile")))
    run_id = evidence.get("run_id")
    if not isinstance(run_id, str) or not EVENT_RUN_ID_RE.match(run_id):
        report.error("evidence.run_id {!r} must be a fresh UUID per independent repeat".format(run_id))
    scope = evidence.get("scope")
    if not isinstance(scope, str) or scope not in index["scopes"]:
        report.error("evidence.scope {!r} is not a registered event scope".format(scope))
    test_ids = evidence.get("test_ids")
    if (not isinstance(test_ids, list) or not test_ids
            or any(not isinstance(test_id, str) or not test_id.strip() for test_id in test_ids)):
        report.error("evidence.test_ids must be a non-empty list of contract ids")
        return None
    if len(test_ids) != len(set(test_ids)):
        report.error("evidence.test_ids must not repeat an id")
    selected = []
    for test_id in test_ids:
        if test_id not in index["tests"]:
            report.error("evidence.test_ids contains unregistered id {!r}".format(test_id))
            continue
        if scope in index["scopes"] and index["tests"][test_id].get("scope") != scope:
            report.error("test {} is outside evidence scope {!r}".format(test_id, scope))
        selected.append(test_id)
    repeat_index = evidence.get("repeat_index")
    repeat_total = evidence.get("repeat_total")
    if type(repeat_index) is not int or repeat_index < 1:
        report.error("evidence.repeat_index must be a positive integer")
    if type(repeat_total) is not int or repeat_total < 1:
        report.error("evidence.repeat_total must be a positive integer")
    if type(repeat_index) is int and type(repeat_total) is int and repeat_index > repeat_total:
        report.error("evidence.repeat_index {} exceeds repeat_total {}".format(
            repeat_index, repeat_total))
    expected_repeat = index["scopes"].get(scope, {}).get("repeat") if isinstance(scope, str) else None
    if type(expected_repeat) is int and type(repeat_total) is int and repeat_total != expected_repeat:
        report.error("evidence.repeat_total {} does not match contract scope {!r} repeat {}".format(
            repeat_total, scope, expected_repeat))
    digest = evidence.get("contract_digest")
    if digest != contract_digest:
        report.error("evidence.contract_digest {!r} does not match the supplied contract {}"
                     .format(digest, contract_digest))
    check_event_fingerprints(report, evidence.get("module_fingerprints"))
    run_config = evidence.get("run_config")
    check_event_run_config(report, run_config, index, selected)
    exit_code = evidence.get("go_test_exit_code")
    if type(exit_code) is not int or exit_code != 0:
        report.error("evidence.go_test_exit_code must be successful integer exit 0")
    log_ref = evidence.get("test_log")
    if not isinstance(log_ref, str) or not log_ref.strip():
        report.error("evidence.test_log must be a non-empty path")
        log_ref = None
    log_digest = evidence.get("test_log_sha256")
    if not isinstance(log_digest, str) or not CONTENT_HASH_RE.match(log_digest):
        report.error("evidence.test_log_sha256 must be a readable sha256 digest")
    if evidence.get("verdict") != "match":
        report.error("evidence.verdict must be 'match', got {!r}".format(evidence.get("verdict")))
    return {"test_ids": selected, "scope": scope, "run_id": run_id,
            "repeat_index": repeat_index, "repeat_total": repeat_total,
            "run_config": run_config, "log_ref": log_ref, "log_digest": log_digest,
            "exit_code": exit_code}


def check_event_evidence_subtests(report, evidence, index, selected):
    """Every mandatory (parent,name) anchor must carry a pass verdict."""
    entries = evidence.get("subtests")
    if not isinstance(entries, list) or not entries:
        report.error("evidence.subtests must be a non-empty list of named anchor verdicts")
        return
    seen = set()
    for position, entry in enumerate(entries):
        where = "subtests[{}]".format(position)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        test_id = entry.get("test_id")
        name = entry.get("name")
        status = entry.get("status")
        if test_id not in selected:
            report.error("{}: test_id {!r} is not one of the selected tests".format(where, test_id))
            continue
        registered = {sub.get("name"): sub for sub in index["tests"][test_id].get("subtests") or []
                      if isinstance(sub, dict)}
        if not isinstance(name, str) or name not in registered:
            report.error("{}: {!r} is not a registered (parent,name) anchor of {}".format(
                where, name, test_id))
            continue
        key = (test_id, name)
        if key in seen:
            report.error("{}: duplicate anchor verdict for ({},{})".format(where, test_id, name))
        seen.add(key)
        if status not in EVENT_ANCHOR_STATUSES:
            report.error("{}: status {!r} not in {}".format(where, status, EVENT_ANCHOR_STATUSES))
            continue
        if status != "pass":
            report.error("{}: mandatory anchor ({},{}) verdict {!r}; fail/skip/missing/running "
                         "are all failures".format(where, test_id, name, status))
    for test_id in selected:
        for sub in index["tests"][test_id].get("subtests") or []:
            if not isinstance(sub, dict) or sub.get("mandatory") is False:
                continue
            if (test_id, sub.get("name")) not in seen:
                report.error("missing verdict for mandatory anchor ({},{})".format(
                    test_id, sub.get("name")))


def check_event_evidence(report, index, contract_digest, evidence, evidence_path, test_log_arg=None):
    """Check one v2 evidence file against its contract and its own test-log."""
    info = check_event_evidence_shape(report, evidence, index, contract_digest)
    if info is None:
        return None
    log_path = info["log_ref"]
    if isinstance(log_path, str) and log_path.strip():
        if not os.path.isabs(log_path):
            log_path = os.path.join(os.path.dirname(os.path.abspath(evidence_path)), log_path)
        log_path = os.path.abspath(log_path)
        if not os.path.isfile(log_path):
            report.error("evidence.test_log not found: {}".format(log_path))
            return info
        try:
            actual_digest = lib.sha256_file(log_path)
        except lib.CheckFailure as exc:
            report.error(str(exc))
            return info
        if actual_digest != info["log_digest"]:
            report.error("evidence.test_log_sha256 does not match {} (the log was replaced "
                         "after the run)".format(log_path))
        if test_log_arg and os.path.realpath(test_log_arg) != os.path.realpath(log_path):
            report.error("evidence.test_log does not match the supplied --test-log")
        try:
            rows = lib.load_jsonl_objects(log_path)
        except lib.CheckFailure as exc:
            report.error(str(exc))
            return info
        module_paths = sorted({index["tests"][test_id]["module_path"]
                               for test_id in info["test_ids"]})
        log_index = index_event_log(report, rows, module_paths,
                                    "evidence {}".format(os.path.basename(evidence_path)))
        for test_id in info["test_ids"]:
            check_event_test_selector(report, "record {}".format(test_id),
                                      index["tests"][test_id], log_index)
    check_event_evidence_subtests(report, evidence, index, info["test_ids"])
    return info


def collect_bundle_evidence(report, bundle_dir):
    """Collect every `evidence.json` under the bundle root."""
    entries = []
    for dirpath, dirnames, filenames in os.walk(bundle_dir):
        dirnames[:] = sorted(name for name in dirnames if not name.startswith("."))
        for name in sorted(filenames):
            if name != "evidence.json":
                continue
            path = os.path.join(dirpath, name)
            try:
                payload = lib.load_json_object(path)
            except lib.CheckFailure as exc:
                report.error(str(exc))
                continue
            entries.append((path, payload))
    if not entries:
        report.error("bundle directory {} holds no evidence.json".format(bundle_dir))
    return entries


def bundle_kind(payload):
    """Classify one bundle artifact by its content, never by its path."""
    if isinstance(payload.get("mutations"), list):
        return "mutations"
    if isinstance(payload.get("scope"), str) and "run_id" in payload:
        return "run"
    return None


def check_mutation_evidence(report, where, payload):
    """Structural gate for the 04.4 mutation report the bundle consumes."""
    if payload.get("control_tree_green") is not True:
        report.error("{}: the control tree must be green before a mutation counts".format(where))
    mutations = payload.get("mutations")
    if not isinstance(mutations, list) or not mutations:
        report.error("{}: mutations must be a non-empty list".format(where))
        return
    for position, mutation in enumerate(mutations):
        mwhere = "{} mutations[{}]".format(where, position)
        if not isinstance(mutation, dict):
            report.error("{} must be an object".format(mwhere))
            continue
        mutation_id = mutation.get("id")
        if not isinstance(mutation_id, str) or not mutation_id.strip():
            report.error("{} must declare an id".format(mwhere))
        if mutation.get("status") != "killed":
            report.error("{}: mutation {} status {!r} is not 'killed'".format(
                mwhere, mutation_id, mutation.get("status")))
        assertion = mutation.get("expect_assertion")
        if not isinstance(assertion, str) or not assertion.strip():
            report.error("{}: mutation {} must name the expect_assertion it hit".format(
                mwhere, mutation_id))
        if mutation.get("assertion_hit") is not None and mutation.get("assertion_hit") is not True:
            report.error("{}: mutation {} did not hit its named assertion".format(
                mwhere, mutation_id))
    not_backed = payload.get("not_evidence_backed")
    if not isinstance(not_backed, list):
        report.error("{}: not_evidence_backed must be a list".format(where))
    elif not_backed:
        report.error("{}: not_evidence_backed must be empty; unproven candidates cannot count"
                     .format(where))


def check_bundle_module_fingerprints(report, runner_entries):
    """Every evidence must describe one source tree, module by module.

    Each scope drives its own groups, so the runner fingerprints only the
    modules that scope selected: event-short covers the root, the CLI host and
    the smoke module, event-long covers the smoke module alone. Comparing the
    fingerprint lists byte-for-byte therefore rejects a legal bundle. The
    checkable meaning of "one source tree" is narrower: wherever two evidence
    files speak about the same module they must agree on the content digest and
    the revision, while a module only one scope covers is a scope difference,
    not drift.
    """
    observations = {}
    for path, payload, _info in runner_entries:
        fingerprints = payload.get("module_fingerprints")
        if not isinstance(fingerprints, list):
            continue  # the envelope check already reported the malformed list
        for entry in fingerprints:
            if not isinstance(entry, dict):
                continue
            module_path = entry.get("module_path")
            if not isinstance(module_path, str) or not module_path.strip():
                continue  # the envelope check already reported the missing key
            seen = observations.setdefault(module_path, {"content_hash": {}, "revision": {}})
            for field in ("content_hash", "revision"):
                value = entry.get(field)
                if isinstance(value, str) and value.strip():
                    seen[field].setdefault(value, []).append(path)
    for module_path in sorted(observations):
        for field, label in (("content_hash", "content digest"), ("revision", "revision")):
            values = observations[module_path][field]
            if len(values) <= 1:
                continue
            detail = "; ".join(
                "{} in {}".format(value if len(value) <= 20 else value[:17] + "...",
                                  os.path.basename(os.path.dirname(paths[0])))
                for value, paths in sorted(values.items()))
            report.error(
                "bundle fingerprints module {!r} with {} different baseline {} values ({}); "
                "the evidence must describe one source tree".format(
                    module_path, len(values), label, detail))


def run_event_bundle_mode(report, contract, contract_path, bundle_dir, as_json):
    """Summarise the short/long/mutation evidence set the MR gate consumes."""
    index = load_event_contract_index(report, contract)
    if index is None:
        return report.emit(as_json)
    if not os.path.isdir(bundle_dir):
        report.error("bundle directory not found: {}".format(bundle_dir))
        return report.emit(as_json)
    try:
        contract_digest = lib.sha256_file(contract_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(as_json)
    entries = collect_bundle_evidence(report, bundle_dir)
    short = []
    long_runs = []
    mutations = []
    for path, payload in entries:
        kind = bundle_kind(payload)
        if kind == "mutations":
            mutations.append((path, payload))
            continue
        if kind is None:
            report.error("bundle file {} is not a recognised evidence object".format(path))
            continue
        info = check_event_evidence(report, index, contract_digest, payload, path)
        if info is None:
            continue
        if payload.get("scope") == "event-short":
            short.append((path, payload, info))
        elif payload.get("scope") == "event-long":
            long_runs.append((path, payload, info))
        else:
            report.error("bundle evidence {} has scope {!r} outside the event bundle scopes"
                         .format(path, payload.get("scope")))
    expected_short = index["scopes"].get("event-short", {}).get("repeat")
    if type(expected_short) is not int:
        report.error("contract scope event-short must declare an integer repeat")
    elif len(short) != expected_short:
        report.error("bundle must hold exactly {} event-short evidence file(s), found {}".format(
            expected_short, len(short)))
    run_ids = {}
    logs = {}
    repeats = {}
    test_sets = set()
    for path, payload, info in short:
        repeat_index = info["repeat_index"]
        if repeat_index in repeats:
            report.error("bundle holds two evidence files for repeat_index {} ({} and {})".format(
                repeat_index, repeats[repeat_index], path))
        else:
            repeats[repeat_index] = path
        test_sets.add(tuple(sorted(info["test_ids"])))
    if type(expected_short) is int and set(repeats) != set(range(1, expected_short + 1)):
        report.error("bundle event-short repeat_index coverage {} does not equal 1..{}".format(
            sorted(repeats), expected_short))
    if len(test_sets) > 1:
        report.error("event-short evidence files cover different test sets; a round must not "
                     "shrink the scope")
    combos = event_matrix_pairs(report, index["matrix"])
    if combos is not None:
        if len(long_runs) != len(combos):
            report.error("bundle must hold exactly {} event-long evidence file(s) (one per matrix "
                         "combination), found {}".format(len(combos), len(long_runs)))
        seen = {}
        for path, payload, info in long_runs:
            config = info["run_config"] if isinstance(info["run_config"], dict) else {}
            key = (config.get("seed"), config.get("window"))
            if key not in combos:
                report.error("bundle event-long evidence {} uses seed/window {} outside the "
                             "contract matrix".format(path, key))
                continue
            if key in seen:
                report.error("bundle holds two event-long evidence files for matrix combo {} "
                             "({} and {})".format(key, seen[key], path))
            else:
                seen[key] = path
        missing = [combo for combo in combos if combo not in seen]
        if missing:
            report.error("bundle is missing event-long matrix combination(s) {}".format(missing))
    runner_entries = short + long_runs
    # One execution credential stands for exactly one round: a run_id or a test
    # log may never be shared across rounds or across a round and a matrix combo.
    for path, payload, info in runner_entries:
        run_id = payload.get("run_id")
        if run_id in run_ids:
            report.error("bundle reuses run_id {!r} ({} and {}): one execution credential cannot "
                         "stand for two independent rounds".format(
                             run_id, run_ids[run_id], path))
        else:
            run_ids[run_id] = path
        log_key = info["log_ref"]
        if isinstance(log_key, str) and log_key.strip():
            log_key = os.path.realpath(log_key if os.path.isabs(log_key)
                                       else os.path.join(os.path.dirname(path), log_key))
            if log_key in logs:
                report.error("bundle reuses the test log {} for two rounds ({} and {})".format(
                    log_key, logs[log_key], path))
            else:
                logs[log_key] = path
    digests = {payload.get("contract_digest") for _path, payload, _info in runner_entries}
    if len(digests) > 1:
        report.error("bundle mixes contract digests {}; every evidence must share one baseline"
                     .format(sorted(str(digest) for digest in digests)))
    check_bundle_module_fingerprints(report, runner_entries)
    if len(mutations) != 1:
        report.error("bundle must hold exactly one mutation evidence file, found {}".format(
            len(mutations)))
    for path, payload in mutations:
        check_mutation_evidence(report, "mutations evidence {}".format(path), payload)
    report.count("mode", "bundle")
    report.count("bundle_evidence_files", len(entries))
    report.count("bundle_short_evidence", len(short))
    report.count("bundle_long_evidence", len(long_runs))
    report.count("bundle_mutation_evidence", len(mutations))
    report.extra["bundle"] = {
        "short_run_ids": sorted(str(payload.get("run_id")) for _p, payload, _i in short),
        "long_combos": sorted([info["run_config"].get("seed"), info["run_config"].get("window")]
                              for _path, _payload, info in long_runs
                              if isinstance(info["run_config"], dict)),
        "test_ids": sorted({test_id for _p, _pl, info in short for test_id in info["test_ids"]}),
    }
    report.note("bundle mode only checks evidence consistency; productVerified stays null")
    return report.emit(as_json)


def run_event_evidence_mode(report, args, contract, contract_path):
    """Verify one v2 evidence file and the log it names."""
    index = load_event_contract_index(report, contract)
    if index is None:
        return report.emit(args.json)
    if not args.evidence:
        report.error("--evidence is required for schema v2 evidence verification")
        return report.emit(args.json)
    evidence_path = os.path.abspath(args.evidence)
    if not os.path.isfile(evidence_path):
        report.error("input not found: {}".format(evidence_path))
        return report.emit(args.json)
    if args.test_log and not os.path.isfile(os.path.abspath(args.test_log)):
        report.error("input not found: {}".format(os.path.abspath(args.test_log)))
        return report.emit(args.json)
    try:
        evidence = lib.load_json_object(evidence_path)
        contract_digest = lib.sha256_file(contract_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(args.json)
    report.count("mode", "evidence")
    info = check_event_evidence(report, index, contract_digest, evidence, evidence_path,
                                args.test_log)
    if info is not None:
        report.extra["scope"] = info["scope"]
        report.extra["repeat_index"] = info["repeat_index"]
        report.extra["repeat_total"] = info["repeat_total"]
        report.extra["test_ids"] = info["test_ids"]
        report.count("event_tests_checked", len(info["test_ids"]))
        report.count("event_anchors_checked", len(evidence.get("subtests") or []))
    report.note("v2 evidence proves command+selector+artifact consistency only; productVerified "
                "stays null")
    return report.emit(args.json)


def resolve_event_scope(report, contract, tests, scope_name):
    if scope_name == SCOPE_COMPLETE:
        return list(tests), []
    names = [scope.get("name") for scope in contract.get("scopes") or []
             if isinstance(scope, dict)]
    if scope_name not in names:
        report.error("unknown scope {!r}: registered event scopes are 'complete' and {}".format(
            scope_name, names))
        return None, None
    required = [test for test in tests
                if isinstance(test, dict) and test.get("scope") == scope_name]
    remaining = [test for test in tests
                 if isinstance(test, dict) and test.get("scope") != scope_name]
    if not required:
        report.error("scope {!r} selects no event test in the contract".format(scope_name))
        return None, None
    return required, remaining


def run_event_contract_mode(report, args, contract, contract_path):
    validate_event_contract(report, contract, contract_path, args.plan_graph,
                            repo_root=args.repo_root)
    tests = contract.get("tests") if isinstance(contract.get("tests"), list) else []
    required, remaining = resolve_event_scope(report, contract, tests, args.scope)
    report.count("mode", "contract")
    report.extra["scope"] = args.scope
    if required is None:
        return report.emit(args.json)
    report.extra["remaining_required"] = [test.get("id") for test in remaining]
    report.count("scope_is_complete", 1 if not remaining else 0)
    report.count("event_tests_required", len(required))
    report.count("event_anchors_required",
                 sum(len(test.get("subtests") or []) for test in required))
    if remaining:
        report.note("scope {!r} is a strict subset; remaining required event test(s): {}".format(
            args.scope, sorted(test.get("id") for test in remaining)))
    report.note("contract-only: no runtime evidence was evaluated")
    return report.emit(args.json)


def run(argv):
    args = parse_args(argv)
    contract_mode = args.check_contract
    bundle_mode = bool(args.bundle)
    evidence_mode = bool(args.evidence) or bool(args.test_log)
    chosen = [name for name, enabled in (("--check-contract", contract_mode),
                                         ("--bundle", bundle_mode),
                                         ("--evidence/--test-log", evidence_mode)) if enabled]
    # Without --bundle the v1 diagnostics stay byte-for-byte as they were; only a
    # bundle combination is new, so only that branch needs new wording.
    if len(chosen) > 1:
        if bundle_mode:
            sys.stderr.write("modes are mutually exclusive: {}\n".format(", ".join(chosen)))
        else:
            sys.stderr.write("--check-contract and --evidence/--test-log are mutually exclusive\n")
        return 2
    if not chosen:
        sys.stderr.write("strict: no evidence supplied; use --check-contract for a contract-only "
                         "check, or --evidence plus --test-log to verify a real run\n")
        return 2
    report = lib.Report("check_runtime_evidence")
    contract_path = os.path.abspath(args.contract)
    if not os.path.isfile(contract_path):
        report.error("scenarios contract not found: {}".format(contract_path))
        return report.emit(args.json)
    try:
        contract = lib.load_json_object(contract_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(args.json)
    if contract.get("schema_version") == EVENT_SCHEMA_VERSION:
        if bundle_mode:
            return run_event_bundle_mode(report, contract, contract_path,
                                         os.path.abspath(args.bundle), args.json)
        if evidence_mode:
            return run_event_evidence_mode(report, args, contract, contract_path)
        return run_event_contract_mode(report, args, contract, contract_path)
    if bundle_mode:
        report.error("--bundle is only defined for the schema v2 event contract")
        return report.emit(args.json)
    graph_ids = load_graph_ids(report, args.plan_graph) if args.plan_graph else None
    contract_index, _ = validate_contract(report, contract, graph_ids)
    required_ids, remaining_ids = resolve_scope(report, contract, contract_index, args.scope)
    report.count("mode", "contract" if contract_mode else "evidence")
    if required_ids is None:
        return report.emit(args.json)
    report.extra["scope"] = args.scope
    report.extra["remaining_required"] = sorted(remaining_ids)
    report.count("scope_is_complete", 1 if not remaining_ids else 0)
    report.count("scenarios_required", len(required_ids))
    if remaining_ids:
        report.note("scope {!r} is a strict subset; remaining required scenario(s) not in this "
                    "run: {}".format(args.scope, sorted(remaining_ids)))
    if contract_mode:
        report.note("contract-only: no runtime evidence was evaluated")
        return report.emit(args.json)
    if not args.evidence or not args.test_log:
        report.error("--evidence and --test-log must both be supplied for evidence mode")
        return report.emit(args.json)
    evidence_path = os.path.abspath(args.evidence)
    test_log_path = os.path.abspath(args.test_log)
    for path in (evidence_path, test_log_path):
        if not os.path.isfile(path):
            report.error("input not found: {}".format(path))
    if report.errors:
        return report.emit(args.json)
    try:
        evidence = lib.load_json_object(evidence_path)
        rows = lib.load_jsonl_objects(test_log_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(args.json)
    ref = evidence.get("test_log_ref")
    if isinstance(ref, str) and os.path.realpath(os.path.join(os.path.dirname(evidence_path), ref)) != os.path.realpath(test_log_path):
        report.error("evidence.test_log_ref does not match supplied test log")
    fingerprint = evidence.get("source_fingerprint")
    if isinstance(fingerprint, dict) and fingerprint.get("module_path") != args.module_path:
        report.error("source_fingerprint.module_path does not match selected module")
    report.count("test_log_rows", len(rows))
    log_index = index_test_log(report, rows, args.module_path)
    check_evidence(report, contract_index, evidence, log_index, required_ids)
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
