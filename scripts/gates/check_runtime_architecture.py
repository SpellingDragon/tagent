#!/usr/bin/env python3
"""check_runtime_architecture.py — 结构覆盖检查（人工政策，非产品行为）。

两种模式：
  * --check-policy       只验证 golden/runtime/architecture.json 政策表本身
                         （字段合法、包/边登记完整、无自环、无环、无通配豁免、
                         未知边、root 组合根显式清单）。
  * --implementation     默认模式：运行 `go list -deps -json ./...` 读取全部
                         一方目录（含子包，非只 target root），把未登记的包、
                         缺失的 required 计划包、越界 import 判为失败。

禁止项只看**显式登记的一方差包路径**（packages/edges）；绝不按第三方包名的
子串猜“镜像/影子类型”（localtool/trpcmirror/shadowtype 之类）并据此阻断。第三方
依赖只做**报告**（不在 known_prefixes 时告警）：“出现一个依赖”不等于“未授权的
go.mod 写入”，二者不是同一事实。

--implementation 另跑 `codetools event-ownership --check`：按 golden 的
event_ownership 冻结表核对 owner 的可解析直接调用面。该门只认 go/types 解析出的
对象身份（声明包 + 接收者类型 + 方法名），不做名字模糊匹配；方法值/反射/解析不到
的调用点记为 uncovered 并如实列出，绝不折算成已覆盖。它证明的是“调用面符合冻结
政策”，不是“第二份消费状态不存在”。

--go-list-json / --event-ownership-json 用已捕获的输出替代真实执行，仅供自测注入；
默认在真实 repo 上执行，绝不用注入结果冒充实跑。任何模式下 productVerified
恒为 null：结构覆盖通过不等于产品成立；当前缺包/缺依赖必须如实报红。
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys

import runtime_checklib as lib

EXACT_PATTERN_RE = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_./-]*$")
ANCHORED_RE = re.compile(r"^\^.*\$$")

# The closed role vocabulary of the frozen event-ownership table. A new role is a
# policy change and must be written down here first, never invented in golden.
OWNERSHIP_ROLES = frozenset({
    "notice-identity-production",
    "delivery-route",
    "mailbox-time-point",
    "logical-consumption-read",
    "logical-consumption-booking",
    "delivery",
})
EXACT_GO_FILE_RE = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_.-]*\.go$")
OWNERSHIP_COUNT_KEYS = (
    "owners", "symbols", "packages_scanned", "candidates", "direct_calls",
    "allowed", "forbidden", "unregistered", "unowned", "ambiguous",
    "uncovered", "same_name_non_target",
)


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--golden", default=os.path.join("golden", "runtime", "architecture.json"))
    parser.add_argument("--repo-root", default=".")
    parser.add_argument("--check-policy", action="store_true",
                        help="只验证政策表（不运行 go list）")
    parser.add_argument("--implementation", action="store_true",
                        help="运行 go list 做真实结构覆盖（默认模式）")
    parser.add_argument("--go-list-json", default=None,
                        help="已捕获的 `go list -deps -json ./...` 输出；仅自测注入用")
    parser.add_argument("--event-ownership-json", default=None,
                        help="已捕获的 codetools event-ownership --json 报告；仅自测注入用")
    parser.add_argument("--json", action="store_true")
    return parser.parse_args(argv)


def pattern_is_legal(pattern):
    if not isinstance(pattern, str) or not pattern.strip():
        return False, "pattern must be a non-empty string"
    if pattern == ".":
        return True, ""
    if "*" in pattern:
        return False, "wildcard '*' is forbidden (no universal exemption)"
    if ANCHORED_RE.match(pattern):
        try:
            re.compile(pattern)
        except re.error as exc:
            return False, "invalid regex: {}".format(exc)
        return True, ""
    if EXACT_PATTERN_RE.match(pattern):
        return True, ""
    return False, "pattern must be an exact package path or an anchored ^...$ regex"


def pattern_matches(pattern, pkg):
    if pattern == pkg:
        return True
    if ANCHORED_RE.match(pattern):
        return re.match(pattern, pkg) is not None
    return False


def find_cycle(adjacency, root="."):
    """Report a dependency cycle between registered packages.

    The composition root's allow list is a registry, not a dependency edge: it must
    name every registered package so an unregistered one is caught, and no package
    may be left unlisted. A back edge pointing at the root is therefore not a cycle
    — the host shell (cli) sits above the composition root, which is the real
    topology, not a mutual dependency. Cycles between the other packages still fail,
    and the root's own outbound entries are still traversed.
    """
    color = {}
    path_stack = []

    def visit(node):
        color[node] = 1
        path_stack.append(node)
        for nxt in adjacency.get(node, ()):
            if nxt == root:
                continue
            state = color.get(nxt, 0)
            if state == 1:
                return path_stack[path_stack.index(nxt):] + [nxt]
            if state == 0:
                found = visit(nxt)
                if found:
                    return found
        path_stack.pop()
        color[node] = 2
        return None

    for node in adjacency:
        if color.get(node, 0) == 0:
            found = visit(node)
            if found:
                return found
    return None


def expand_selectors(report, where, source, selectors, known, forbid=False):
    """Resolve edge selectors to concrete registered patterns.

    A selector is either an exact package path (must be registered) or an
    anchored regex (must match at least one registered package).  A bare `*` or
    any other wildcard is rejected outright: no universal exemption.
    """
    resolved = []
    for selector in selectors:
        legal, why = pattern_is_legal(selector)
        if not legal:
            report.error("{}: {!r} {} entry {!r}: {}".format(
                where, source, "forbidden" if forbid else "allow", selector, why))
            continue
        if ANCHORED_RE.match(selector):
            hits = [pattern for pattern in known if pattern_matches(selector, pattern)]
            if not hits:
                report.error("{}: pattern {!r} matches no registered package".format(where, selector))
            resolved.extend(hits)
        elif selector in known:
            resolved.append(selector)
        else:
            report.error("{}: {!r} references unregistered package {!r} (unknown edge)".format(
                where, source, selector))
    return resolved


def non_empty_string(value):
    return isinstance(value, str) and bool(value.strip())


def validate_event_ownership(report, policy, registry):
    """Validate the frozen direct-call ownership table.

    The table is the only place that says which layer may call which frozen
    symbol. Its absence is an error, not a note: a missing table would make the
    direct-call gate disappear while the package policy still passed. Structural
    validity is checked here; the allowed/forbidden/uncovered verdict comes from
    the Go gate that reads the same table.
    """
    table = policy.get("event_ownership")
    if not isinstance(table, dict):
        report.error("architecture event_ownership table is missing: the direct-call "
                     "ownership gate would silently disappear")
        return
    if table.get("schema_version") != 1:
        report.error("event_ownership schema_version must be 1, got {!r}".format(
            table.get("schema_version")))
    for field in ("frozen_by", "statement"):
        if not non_empty_string(table.get(field)):
            report.error("event_ownership.{} must be a non-empty string".format(field))
    symbols = table.get("symbols")
    owners = table.get("owners")
    if not isinstance(symbols, list) or not symbols:
        report.error("event_ownership.symbols must be a non-empty list")
        symbols = []
    if not isinstance(owners, list) or not owners:
        report.error("event_ownership.owners must be a non-empty list")
        owners = []
    symbol_ids = {}
    for index, symbol in enumerate(symbols):
        where = "event_ownership.symbols[{}]".format(index)
        if not isinstance(symbol, dict):
            report.error("{} must be an object".format(where))
            continue
        symbol_id = symbol.get("id")
        if not non_empty_string(symbol_id):
            report.error("{} id must be a non-empty string".format(where))
            continue
        where = "event_ownership.symbol {!r}".format(symbol_id)
        if symbol_id in symbol_ids:
            report.error("{}: duplicate symbol id".format(where))
            continue
        symbol_ids[symbol_id] = symbol
        if symbol.get("pkg") not in registry:
            report.error("{}: pkg {!r} is not a registered package".format(
                where, symbol.get("pkg")))
        func_name = symbol.get("func")
        method_name = symbol.get("method")
        if bool(func_name) == bool(method_name):
            report.error("{}: exactly one of func/method must be named".format(where))
        if func_name and "receivers" in symbol:
            report.error("{}: a package-level func cannot declare receivers".format(where))
        if method_name:
            receivers = symbol.get("receivers")
            if (not isinstance(receivers, list) or not receivers
                    or any(not non_empty_string(receiver) for receiver in receivers)):
                report.error("{}: a method needs a non-empty receivers list".format(where))
        if symbol.get("role") not in OWNERSHIP_ROLES:
            report.error("{}: unknown role {!r} (known: {})".format(
                where, symbol.get("role"), ", ".join(sorted(OWNERSHIP_ROLES))))
        if not non_empty_string(symbol.get("reason")):
            report.error("{}: every frozen symbol needs a written reason".format(where))
    owner_ids = {}
    owner_files = {}
    for index, owner in enumerate(owners):
        where = "event_ownership.owners[{}]".format(index)
        if not isinstance(owner, dict):
            report.error("{} must be an object".format(where))
            continue
        owner_id = owner.get("id")
        if not non_empty_string(owner_id):
            report.error("{} id must be a non-empty string".format(where))
            continue
        where = "event_ownership.owner {!r}".format(owner_id)
        if owner_id in owner_ids:
            report.error("{}: duplicate owner id".format(where))
            continue
        owner_ids[owner_id] = owner
        packages = owner.get("packages")
        if (not isinstance(packages, list) or not packages
                or any(pkg not in registry for pkg in packages)):
            report.error("{}: packages must be a non-empty list of registered packages".format(where))
            packages = []
        files = owner.get("files")
        if files is None:
            owner_files[owner_id] = None
        elif not isinstance(files, list) or not files:
            report.error("{}: files must be a non-empty exact file list when present".format(where))
            owner_files[owner_id] = set()
        else:
            if any(not EXACT_GO_FILE_RE.match(name) for name in files):
                report.error("{}: every file must be an exact *.go base name (no wildcards): {}".format(
                    where, files))
            if len(set(files)) != len(files):
                report.error("{}: duplicate file in the exact file list".format(where))
            owner_files[owner_id] = set(files)
        allowed = owner.get("allowed")
        forbidden = owner.get("forbidden", [])
        if not isinstance(allowed, list) or not isinstance(forbidden, list):
            report.error("{}: allowed/forbidden must be lists".format(where))
            continue
        for field, ids in (("allowed", allowed), ("forbidden", forbidden)):
            unknown = sorted({symbol for symbol in ids if symbol not in symbol_ids})
            if unknown:
                report.error("{}: {} references undeclared symbol(s) {}".format(
                    where, field, unknown))
        overlap = sorted(set(allowed) & set(forbidden))
        if overlap:
            report.error("{}: both allows and forbids {}".format(where, overlap))
        if len(set(allowed)) != len(allowed) or len(set(forbidden)) != len(forbidden):
            report.error("{}: duplicate symbol entry in allowed/forbidden".format(where))
        if not non_empty_string(owner.get("reason")):
            report.error("{}: every owner needs a written reason".format(where))
    # One package may be split between owners only when the split is spelled out
    # as disjoint exact files: a package-wide claim beside a file-restricted one
    # would make the verdict depend on scan order.
    for pkg in sorted(registry):
        claimants = [owner_id for owner_id, owner in owner_ids.items()
                     if pkg in (owner.get("packages") or [])]
        if len(claimants) < 2:
            continue
        for owner_id in claimants:
            if owner_files.get(owner_id) is None:
                report.error("event_ownership: owner {!r} claims package {!r} without an "
                             "exact file list while {} other owner(s) also claim it; "
                             "overlapping owners need disjoint exact files".format(
                                 owner_id, pkg, len(claimants) - 1))
        for left in range(len(claimants)):
            for right in range(left + 1, len(claimants)):
                left_files = owner_files.get(claimants[left])
                right_files = owner_files.get(claimants[right])
                if not left_files or not right_files:
                    continue
                shared = sorted(left_files & right_files)
                if shared:
                    report.error("event_ownership: owners {!r} and {!r} both claim files {}".format(
                        claimants[left], claimants[right], shared))
    allowed_somewhere = set()
    for owner in owner_ids.values():
        allowed_somewhere.update(owner.get("allowed") or [])
    orphaned = sorted(set(symbol_ids) - allowed_somewhere)
    if orphaned:
        report.error("event_ownership: symbol(s) {} are allowed by no owner; a frozen "
                     "symbol with no legal caller is a contradiction".format(orphaned))
    report.count("event_ownership_owners", len(owner_ids))
    report.count("event_ownership_symbols", len(symbol_ids))


def load_policy(report, golden_path):
    policy = lib.load_json_object(golden_path)
    if policy.get("schema_version") != 1:
        report.error("architecture schema_version must be 1, got {!r}".format(
            policy.get("schema_version")))
    if not isinstance(policy.get("module_path"), str) or not policy["module_path"].strip():
        report.error("architecture module_path must be a non-empty string")
    # The composition root is data, not a fixed spelling: the policy names the package
    # that assembles and publishes (it sat at the module root until the assembly/ move),
    # and the gate reads that name instead of assuming one. Everything below that talks
    # about "the root" resolves it from here.
    root = policy.get("root_package")
    if not isinstance(root, str) or not root.strip():
        report.error("architecture root_package must name the composition root package")
        root = ""
    packages = policy.get("packages")
    edges = policy.get("edges")
    if not isinstance(packages, list) or not packages:
        report.error("architecture packages must be a non-empty list")
        packages = []
    if not isinstance(edges, list):
        report.error("architecture edges must be a list")
        edges = []
    registry = {}
    for index, entry in enumerate(packages):
        where = "packages[{}]".format(index)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        pattern = entry.get("pattern")
        ok, why = pattern_is_legal(pattern)
        if not ok:
            report.error("{} pattern {!r}: {}".format(where, pattern, why))
            continue
        if pattern == "tagent":
            report.error("{}: nested 'tagent' second-level facade is not allowed".format(where))
        if pattern in registry:
            report.error("{}: duplicate package pattern {!r}".format(where, pattern))
            continue
        if not isinstance(entry.get("reason"), str) or not entry["reason"].strip():
            report.error("{}: every package needs a written reason".format(where))
        registry[pattern] = entry
    report.count("policy_packages", len(registry))
    external = policy.get("external")
    if not isinstance(external, dict):
        report.error("architecture external policy must be an object")
    else:
        if "forbidden_substrings" in external:
            report.error("external.forbidden_substrings is a name-guessing heuristic and is "
                         "not allowed; register explicit forbidden package paths on edges")
        prefixes = external.get("known_prefixes")
        if not isinstance(prefixes, list) or any(
                not isinstance(prefix, str) or not prefix.strip() for prefix in prefixes):
            report.error("external.known_prefixes must be a list of non-empty strings")
        if not isinstance(external.get("reason"), str) or not external["reason"].strip():
            report.error("external policy needs a written reason")
    known = set(registry)
    if root:
        legal, why = pattern_is_legal(root)
        if not legal or "*" in root or ANCHORED_RE.match(root):
            report.error("architecture root_package {!r} must be one exact package path".format(root))
            root = ""
        elif root not in registry:
            report.error("architecture must register the composition root package {!r}".format(root))
            root = ""
        elif registry[root].get("role") != "composition":
            report.error("architecture root_package {!r} must carry role 'composition'".format(root))
            root = ""
    if not root:
        report.error("architecture has no usable composition root package")
    adjacency = {}
    seen_edges = set()
    for index, entry in enumerate(edges):
        where = "edges[{}]".format(index)
        if not isinstance(entry, dict):
            report.error("{} must be an object".format(where))
            continue
        source = entry.get("from")
        if source not in known:
            report.error("{}: unknown edge source {!r} (unknown edge)".format(where, source))
            continue
        if source in seen_edges:
            report.error("{}: duplicate edge entry for {!r}".format(where, source))
        seen_edges.add(source)
        allow = entry.get("allow")
        forbidden = entry.get("forbidden", [])
        if not isinstance(allow, list) or not isinstance(forbidden, list):
            report.error("{}: allow/forbidden must be lists".format(where))
            continue
        if not isinstance(entry.get("reason"), str) or not entry["reason"].strip():
            report.error("{}: every edge needs a written reason".format(where))
        resolved_allow = expand_selectors(report, where, source, allow, known)
        resolved_forbidden = expand_selectors(report, where, source, forbidden, known, forbid=True)
        if source in allow:
            report.error("{}: {!r} allows itself".format(where, source))
        overlap = sorted(set(resolved_allow) & set(resolved_forbidden))
        if overlap:
            report.error("{}: {!r} both allows and forbids {}".format(where, source, overlap))
        adjacency[source] = sorted(set(resolved_allow))
    for pattern in sorted(known):
        if pattern not in seen_edges:
            report.error("policy has no edge entry for package {!r}".format(pattern))
    cycle = find_cycle(adjacency, root or ".")
    if cycle:
        report.error("architecture package cycle: {}".format(" -> ".join(cycle)))
    if root:
        root_allow = set(adjacency.get(root, []))
        for pattern in sorted(known - {root}):
            if pattern not in root_allow:
                report.error("composition root does not list package {!r}".format(pattern))
    report.count("policy_edges", len(seen_edges))
    errors_before = len(report.errors)
    validate_event_ownership(report, policy, registry)
    return policy, registry, adjacency, len(report.errors) == errors_before


def parse_go_list_stream(text):
    decoder = json.JSONDecoder()
    records = []
    index = 0
    length = len(text)
    while index < length:
        while index < length and text[index] in " \t\r\n":
            index += 1
        if index >= length:
            break
        obj, end = decoder.raw_decode(text, index)
        records.append(obj)
        index = end
    return records


def obtain_go_list(report, repo_root, injected_path):
    if injected_path:
        try:
            with open(injected_path, "r", encoding="utf-8") as handle:
                text = handle.read()
        except OSError as exc:
            return None, "cannot read injected go list output {}: {}".format(injected_path, exc)
        report.note("go list source: injected file {} (readonly derivation, NOT a repository run)".format(
            injected_path))
        return text, None
    command = ["go", "list", "-deps", "-json", "./..."]
    try:
        proc = subprocess.run(command, cwd=repo_root, capture_output=True, text=True)
    except OSError as exc:
        return None, "cannot execute go: {}".format(exc)
    if proc.returncode != 0:
        tail = (proc.stderr or "").strip().splitlines()
        tail = " | ".join(tail[-6:]) if tail else "(no stderr)"
        report.note("go list source: real repository execution (exit {})".format(proc.returncode))
        return proc.stdout, "go list failed with exit {}: {}".format(proc.returncode, tail)
    report.note("go list source: real repository execution (exit 0)")
    return proc.stdout, None


def in_scope(rel, exclude_names):
    for segment in rel.split("/"):
        if segment in exclude_names:
            return False
    return True


def obtain_event_ownership(report, repo_root, golden_path, injected_path):
    """Obtain the event-ownership report from the real Go gate or an injected file.

    Injection exists only for selftests, exactly like --go-list-json: a captured
    result never masquerades as a repository run, and the note says which one it is.
    """
    if injected_path:
        try:
            with open(injected_path, "r", encoding="utf-8") as handle:
                text = handle.read()
        except OSError as exc:
            return None, "cannot read injected event-ownership report {}: {}".format(
                injected_path, exc)
        report.note("event-ownership source: injected file {} (readonly derivation, "
                    "NOT a repository run)".format(injected_path))
        try:
            return json.loads(text), None
        except ValueError as exc:
            return None, "cannot parse injected event-ownership report: {}".format(exc)
    command = ["go", "run", "./scripts/codetools", "event-ownership", "--check",
               "--repo-root", repo_root, "--golden", golden_path, "--json"]
    try:
        proc = subprocess.run(command, cwd=repo_root, capture_output=True, text=True,
                              timeout=900)
    except OSError as exc:
        return None, "cannot execute the event-ownership gate: {}".format(exc)
    except subprocess.TimeoutExpired:
        return None, "event-ownership gate timed out"
    if proc.returncode != 0:
        report.note("event-ownership source: real repository execution (exit {})".format(
            proc.returncode))
        # The gate prints its report even when it fails; parse it so violations are
        # named instead of only the exit code. An unparseable body is the fallback.
        try:
            return json.loads(proc.stdout), None
        except ValueError:
            tail = (proc.stderr or "").strip().splitlines()
            tail = " | ".join(tail[-6:]) if tail else "(no stderr)"
            return None, "event-ownership gate failed with exit {}: {}".format(
                proc.returncode, tail)
    report.note("event-ownership source: real repository execution (exit 0)")
    try:
        return json.loads(proc.stdout), None
    except ValueError as exc:
        return None, "cannot parse event-ownership output: {}".format(exc)


def check_event_ownership(report, payload):
    """Fold the gate report into this checker without upgrading its verdict.

    A structural pass is counted as coverage of the frozen call surface only. An
    uncovered call site is reported with file/line/caller/reason and is never
    added to any covered count: this gate cannot prove that a second consumption
    state does not exist through method values, reflection or an untyped edge.
    """
    if not isinstance(payload, dict):
        report.error("event-ownership report must be a JSON object")
        return
    counts = payload.get("counts")
    if not isinstance(counts, dict) or not counts:
        report.error("event-ownership report carries no counts object")
        counts = {}
    for key in OWNERSHIP_COUNT_KEYS if counts else ():
        value = counts.get(key)
        if type(value) is not int or value < 0:
            report.error("event-ownership counts.{} must be a non-negative integer, "
                         "got {!r}".format(key, value))
            continue
        report.count("event_ownership_" + key, value)
    violations = payload.get("violations")
    if not isinstance(violations, list):
        report.error("event-ownership report violations must be a list")
        violations = []
    for finding in violations:
        if not isinstance(finding, dict):
            report.error("event-ownership violation entry must be an object: {!r}".format(finding))
            continue
        report.error("event-ownership {} {}:{} {} owner={}: {}".format(
            finding.get("rule"), finding.get("file"), finding.get("line"),
            finding.get("symbol"), finding.get("owner"), finding.get("detail")))
    uncovered = payload.get("uncovered")
    if not isinstance(uncovered, list):
        report.error("event-ownership report uncovered must be a list")
        uncovered = []
    for entry in uncovered:
        if (not isinstance(entry, dict) or not non_empty_string(entry.get("file"))
                or type(entry.get("line")) is not int
                or not non_empty_string(entry.get("reason"))):
            report.error("event-ownership uncovered entry needs file/line/reason: {!r}".format(entry))
            continue
        report.note("event-ownership uncovered {}:{} {} {} — {} "
                    "(reported, never counted as covered)".format(
                        entry["file"], entry["line"], entry.get("caller", "-"),
                        entry.get("name", "-"), entry["reason"]))
    if type(counts.get("uncovered")) is int and counts["uncovered"] != len(uncovered):
        report.error("event-ownership counts.uncovered={} disagrees with {} listed "
                     "uncovered site(s)".format(counts["uncovered"], len(uncovered)))
    if not isinstance(payload.get("ok"), bool):
        report.error("event-ownership report ok must be a boolean")
    elif payload["ok"] is True:
        if violations:
            report.error("event-ownership report claims ok while recording violations")
    elif not violations:
        notes = payload.get("notes")
        if isinstance(notes, list) and notes:
            for note in notes:
                report.error("event-ownership: {}".format(note))
        else:
            report.error("event-ownership gate did not pass and gave no reason")
    if counts and counts.get("allowed") == 0:
        report.error("event-ownership counted no allowed direct call: the gate is vacuous")


def check_implementation(report, policy, registry, adjacency, repo_root, injected_path,
                         golden_path, ownership_injected, ownership_ok):
    module_path = policy.get("module_path") or lib.MODULE_PATH
    scope = policy.get("scope") or {}
    exclude_names = set(scope.get("exclude_dir_names") or [])
    external = policy.get("external") or {}
    known_prefixes = tuple(external.get("known_prefixes") or ())
    text, failure = obtain_go_list(report, repo_root, injected_path)
    if failure:
        report.error(failure)
    records = []
    if text:
        try:
            records = parse_go_list_stream(text)
        except ValueError as exc:
            report.error("cannot parse go list output: {}".format(exc))
    if not records:
        report.error("no packages returned by go list: implementation not present / not buildable")
        return
    by_import = {}
    for record in records:
        if isinstance(record, dict) and isinstance(record.get("ImportPath"), str):
            by_import[record["ImportPath"]] = record
    report.count("go_list_packages", len(by_import))
    first_party = []
    for import_path, record in by_import.items():
        module = record.get("Module") or {}
        if module.get("Path") != module_path:
            continue
        rel = lib.normalize_package(import_path, module_path)
        if rel is None:
            report.error("cannot normalise first-party import path {!r}".format(import_path))
            continue
        first_party.append((rel, record))
    report.count("first_party_packages", len(first_party))
    scoped = [(rel, record) for rel, record in first_party if in_scope(rel, exclude_names)]
    report.count("in_scope_packages", len(scoped))
    matched_patterns = set()
    unknown = []
    resolvers = {}
    for rel, _record in scoped:
        hits = [pattern for pattern in registry if pattern_matches(pattern, rel)]
        if len(hits) == 1:
            matched_patterns.add(hits[0])
            resolvers[rel] = hits[0]
        elif not hits:
            unknown.append(rel)
        else:
            report.error("package {!r} matches multiple patterns {}".format(rel, hits))
    for rel in sorted(unknown):
        report.error("unknown first-party package {!r}: not registered in architecture policy "
                     "(add an explicit edge, never a wildcard)".format(rel))
    report.count("unknown_packages", len(unknown))
    missing = []
    for pattern, entry in registry.items():
        if entry.get("required") and pattern not in matched_patterns:
            missing.append(pattern)
    for pattern in sorted(missing):
        report.error("required planned package {!r} is not implemented in the tree".format(pattern))
    report.count("missing_required_packages", len(missing))
    edge_violations = 0
    external_imports = 0
    for rel, record in scoped:
        source_pattern = resolvers.get(rel)
        allowed = set(adjacency.get(source_pattern, [])) if source_pattern else set()
        for imported in record.get("Imports", []) or []:
            if not isinstance(imported, str):
                continue
            target_record = by_import.get(imported)
            target_is_first_party = bool(target_record and (target_record.get("Module") or {}).get("Path") == module_path)
            if target_is_first_party:
                target_rel = lib.normalize_package(imported, module_path)
                if target_rel is None or not in_scope(target_rel, exclude_names):
                    continue
                target_pattern = resolvers.get(target_rel)
                if target_pattern is None or source_pattern is None:
                    continue
                if target_pattern != source_pattern and target_pattern not in allowed:
                    edge_violations += 1
                    report.error("edge violation: {} ({}) imports {} ({}) not in allow list".format(
                        rel, source_pattern, target_rel, target_pattern))
            else:
                external_imports += 1
                # Third-party imports are only *reported*: naming a dependency is
                # never proof of an unauthorised go.mod write, and guessing a
                # "mirror" from a substring is not a structural fact.  Only the
                # explicit per-edge forbidden package paths block.
                if known_prefixes and not imported.startswith(known_prefixes):
                    report.warn("external import {} from {} is not in known_prefixes "
                                "(reported, not blocked)".format(imported, rel))
    report.count("edge_violations", edge_violations)
    report.count("external_imports", external_imports)
    if ownership_ok:
        payload, failure = obtain_event_ownership(report, repo_root, golden_path,
                                                  ownership_injected)
        if failure:
            report.error(failure)
        else:
            check_event_ownership(report, payload)


def run(argv):
    args = parse_args(argv)
    if args.check_policy and args.implementation:
        sys.stderr.write("--check-policy and --implementation are mutually exclusive\n")
        return 2
    if args.check_policy and args.event_ownership_json:
        sys.stderr.write("--event-ownership-json is only meaningful with --implementation\n")
        return 2
    report = lib.Report("check_runtime_architecture")
    golden_path = os.path.abspath(args.golden)
    if not os.path.isfile(golden_path):
        report.error("architecture golden not found: {}".format(golden_path))
        return report.emit(args.json)
    try:
        policy, registry, adjacency, ownership_ok = load_policy(report, golden_path)
    except lib.CheckFailure as exc:
        report.error(str(exc))
        return report.emit(args.json)
    mode = "policy" if args.check_policy else "implementation"
    report.count("mode", mode)
    if mode == "implementation":
        repo_root = os.path.abspath(args.repo_root)
        if not os.path.isdir(repo_root):
            report.error("repo root not found: {}".format(repo_root))
            return report.emit(args.json)
        if args.go_list_json and not args.event_ownership_json:
            report.error("--go-list-json injection also needs --event-ownership-json: both "
                         "structural gates read the same tree, and running one against a "
                         "different tree than the other would be a false pass")
            return report.emit(args.json)
        check_implementation(report, policy, registry, adjacency, repo_root, args.go_list_json,
                             golden_path, args.event_ownership_json, ownership_ok)
    report.note("structural coverage: {} (format/policy valid never implies product green)".format(
        "OK" if report.ok else "FAILED"))
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
