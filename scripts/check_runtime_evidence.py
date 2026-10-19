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
import os
import re
import sys

import runtime_checklib as lib

EXPECTED_SCENARIOS = 12
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
    parser.add_argument("--plan-graph", default=None,
                        help="可选：task-graph.json，用于核对 task_ids 归属")
    parser.add_argument("--scope", default=SCOPE_COMPLETE,
                        help="验收集：complete（默认，全部场景）或合同登记的唯一子集标签")
    parser.add_argument("--module-path", default=lib.MODULE_PATH)
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


def run(argv):
    args = parse_args(argv)
    contract_mode = args.check_contract
    evidence_mode = bool(args.evidence) or bool(args.test_log)
    if contract_mode and evidence_mode:
        sys.stderr.write("--check-contract and --evidence/--test-log are mutually exclusive\n")
        return 2
    if not contract_mode and not evidence_mode:
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
