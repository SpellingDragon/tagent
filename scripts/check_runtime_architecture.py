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

--go-list-json PATH 用一份已捕获的 go list 输出替代真实执行，仅供自测注入；
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


def load_policy(report, golden_path):
    policy = lib.load_json_object(golden_path)
    if policy.get("schema_version") != 1:
        report.error("architecture schema_version must be 1, got {!r}".format(
            policy.get("schema_version")))
    if not isinstance(policy.get("module_path"), str) or not policy["module_path"].strip():
        report.error("architecture module_path must be a non-empty string")
    if policy.get("root_package") != ".":
        report.error("root_package must be '.' (module root, no nested tagent facade)")
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
    if "." not in registry:
        report.error("architecture must register the module root as composition package '.'")
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
    cycle = find_cycle(adjacency)
    if cycle:
        report.error("architecture package cycle: {}".format(" -> ".join(cycle)))
    root_allow = set(adjacency.get(".", []))
    for pattern in sorted(known - {"."}):
        if pattern not in root_allow:
            report.error("composition root does not list package {!r}".format(pattern))
    report.count("policy_edges", len(seen_edges))
    return policy, registry, adjacency


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


def check_implementation(report, policy, registry, adjacency, repo_root, injected_path):
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


def run(argv):
    args = parse_args(argv)
    if args.check_policy and args.implementation:
        sys.stderr.write("--check-policy and --implementation are mutually exclusive\n")
        return 2
    report = lib.Report("check_runtime_architecture")
    golden_path = os.path.abspath(args.golden)
    if not os.path.isfile(golden_path):
        report.error("architecture golden not found: {}".format(golden_path))
        return report.emit(args.json)
    try:
        policy, registry, adjacency = load_policy(report, golden_path)
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
        check_implementation(report, policy, registry, adjacency, repo_root, args.go_list_json)
    report.note("structural coverage: {} (format/policy valid never implies product green)".format(
        "OK" if report.ok else "FAILED"))
    return report.emit(args.json)


if __name__ == "__main__":
    sys.exit(run(sys.argv[1:]))
