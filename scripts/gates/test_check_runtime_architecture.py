#!/usr/bin/env python3
"""Selftests for check_runtime_architecture.py — no real `go list` is executed.

Implementation-mode tests inject a captured `go list -deps -json` stream via
--go-list-json AND a captured event-ownership report via --event-ownership-json
inside a tempfile repo root, so the suite neither builds nor lists the real
product tree and never runs the Go gate against a tree it did not describe.
"""
import copy
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
REPO = SCRIPTS.parents[1]
CHECKER = SCRIPTS / "check_runtime_architecture.py"
REAL_GOLDEN = REPO / "golden" / "runtime" / "architecture.json"
MODULE = "github.com/SpellingDragon/tagent"


def run_checker(*args):
    proc = subprocess.run([sys.executable, str(CHECKER)] + list(args),
                          capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def real_policy():
    return json.loads(REAL_GOLDEN.read_text(encoding="utf-8"))


def patterns(policy):
    return [entry["pattern"] for entry in policy["packages"]]


def import_path(pattern):
    return MODULE if pattern == "." else "{}/{}".format(MODULE, pattern)


def ownership_report(policy, **overrides):
    """A structurally complete event-ownership report; overrides drive negatives."""
    table = policy["event_ownership"]
    payload = {
        "ok": True,
        "module": MODULE,
        "golden": "golden/runtime/architecture.json",
        "counts": {
            "owners": len(table["owners"]),
            "symbols": len(table["symbols"]),
            "packages_scanned": 31,
            "candidates": 47,
            "direct_calls": 20,
            "allowed": 14,
            "forbidden": 0,
            "unregistered": 0,
            "unowned": 0,
            "ambiguous": 0,
            "uncovered": 1,
            "same_name_non_target": 29,
        },
        "violations": [],
        "uncovered": [{
            "file": "run.go",
            "line": 307,
            "caller": "Assemble",
            "name": "deliver",
            "symbol": "host.deliver",
            "reason": "method value of a frozen symbol is passed on as a callback; "
                      "the call happens inside the port",
        }],
        "non_target": [],
        "notes": [],
    }
    payload.update(overrides)
    return payload


def go_list_stream(policy, omit=(), extra_imports=None):
    """Build a fake `go list -deps -json` stream for every registered package."""
    records = []
    registered = patterns(policy)
    for pattern in registered:
        if pattern in omit:
            continue
        imports = []
        if pattern == ".":
            imports = [import_path(other) for other in registered
                       if other != "." and other not in omit]
        records.append({
            "ImportPath": import_path(pattern),
            "Name": pattern.split("/")[-1].replace(".", "tagent"),
            "Dir": "/fake/" + pattern,
            "Module": {"Path": MODULE},
            "Imports": imports,
            "Deps": imports,
        })
    for record in records:
        key = record["ImportPath"]
        if extra_imports and key in extra_imports:
            record["Imports"] = sorted(record["Imports"] + extra_imports[key])
    return "\n".join(json.dumps(record) for record in records)


class PolicyTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.policy = real_policy()

    def tearDown(self):
        self._tmp.cleanup()

    def write_policy(self, policy):
        path = self.root / "architecture.json"
        path.write_text(json.dumps(policy, ensure_ascii=False, indent=2), encoding="utf-8")
        return path

    def check_policy(self, policy):
        path = self.write_policy(policy)
        return run_checker("--golden", str(path), "--check-policy", "--json")

    def test_real_golden_policy_is_valid(self):
        code, out, err = run_checker("--golden", str(REAL_GOLDEN), "--check-policy", "--json")
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertIsNone(payload["productVerified"])

    def test_wildcard_allow_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["edges"][0]["allow"] = ["*"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("wildcard", out)

    def test_unknown_edge_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["edges"][3]["allow"] = ["does-not-exist"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("unknown edge", out)

    def test_cycle_is_rejected(self):
        # Negative anchor: a real cycle between two non-root packages stays red.
        # The composition root's allow list is a registry, so only the root is
        # exempt from closing a loop; mutual dependencies elsewhere still fail.
        policy = copy.deepcopy(self.policy)
        for edge in policy["edges"]:
            if edge["from"] == "api/config":
                edge["allow"] = ["runtime/state"]
            if edge["from"] == "runtime/state":
                edge["allow"] = ["api/config"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("cycle", out)

    def test_back_edge_to_composition_root_is_not_a_cycle(self):
        # Positive anchor: the host shell sits above the composition root
        # (cli -> "."), and the root lists every package as a registry. Traversing
        # the root's outbound entries plus that back edge must not read as a cycle.
        policy = copy.deepcopy(self.policy)
        self.assertIn("cli", patterns(policy), "the host shell is registered")
        shell = next(edge for edge in policy["edges"] if edge["from"] == "cli")
        self.assertIn(".", shell["allow"], "the host shell drives the composition root")
        code, out, err = self.check_policy(policy)
        self.assertEqual(code, 0, out + err)
        self.assertNotIn("cycle", out)

    def test_duplicate_pattern_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["packages"].append(dict(policy["packages"][1]))
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate package pattern", out)

    def test_nested_tagent_facade_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["packages"].append({"pattern": "tagent", "role": "facade",
                                   "required": True, "reason": "nested"})
        policy["edges"].append({"from": "tagent", "allow": [], "forbidden": [], "reason": "x"})
        policy["edges"][0]["allow"].append("tagent")
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("nested 'tagent'", out)

    def test_root_must_list_every_package(self):
        policy = copy.deepcopy(self.policy)
        policy["edges"][0]["allow"] = [p for p in policy["edges"][0]["allow"] if p != "features/evolution"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("does not list package 'features/evolution'", out)

    def test_self_allowed_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        for edge in policy["edges"]:
            if edge["from"] == "api/event":
                edge["allow"] = ["api/event"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("allows itself", out)

    def test_modes_are_mutually_exclusive(self):
        code, _, err = run_checker("--check-policy", "--implementation")
        self.assertEqual(code, 2)
        self.assertIn("mutually exclusive", err)

    def test_forbidden_substrings_heuristic_is_rejected(self):
        # Name-guessing (localtool/trpcmirror/shadowtype substrings) is not a
        # structural fact and must never be a policy input.
        policy = copy.deepcopy(self.policy)
        policy["external"]["forbidden_substrings"] = ["localtool", "trpcmirror", "shadowtype"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("forbidden_substrings", out)

    def test_real_golden_registers_tool_subtree_not_top_level(self):
        pats = patterns(self.policy)
        self.assertIn("tool/mcp", pats)
        self.assertIn("tool/spec", pats)
        self.assertNotIn("mcp", pats)
        self.assertNotIn("spec", pats)

    def test_real_golden_replica_package_is_removed(self):
        # 副本层随 golden-integrity D3 硬删：注册表与依赖边都不得再出现它。
        self.assertNotIn("memory/replica", patterns(self.policy))
        edges = {edge["from"]: edge for edge in self.policy["edges"]}
        self.assertNotIn("memory/replica", edges)

    def test_real_golden_storage_tiers_are_optional(self):
        by_pattern = {entry["pattern"]: entry for entry in self.policy["packages"]}
        for tier in ("memory/localfile", "memory/rustviking"):
            self.assertIn(tier, by_pattern)
            self.assertFalse(by_pattern[tier]["required"])

    def test_real_golden_query_projection_is_optional_registered_edge(self):
        by_pattern = {entry["pattern"]: entry for entry in self.policy["packages"]}
        self.assertIn("memory/query", by_pattern)
        self.assertFalse(by_pattern["memory/query"]["required"])
        edges = {edge["from"]: edge for edge in self.policy["edges"]}
        self.assertIn("memory/query", edges)
        self.assertNotIn("runtime/agent", edges["memory/query"]["allow"])

    def test_real_golden_root_lists_every_package_without_wildcard(self):
        by_pattern = {entry["pattern"] for entry in self.policy["packages"]}
        root = next(edge for edge in self.policy["edges"] if edge["from"] == ".")
        self.assertNotIn("*", root["allow"])
        self.assertEqual(set(root["allow"]), by_pattern - {"."})

    def test_real_golden_external_has_no_name_heuristic(self):
        external = self.policy["external"]
        self.assertNotIn("forbidden_substrings", external)
        self.assertTrue(external["known_prefixes"])

    def test_real_golden_event_ownership_is_frozen(self):
        table = self.policy["event_ownership"]
        owners = {owner["id"]: owner for owner in table["owners"]}
        for owner in ("runtime/task", "runtime/state", "runtime/agent", "facts", "host"):
            self.assertIn(owner, owners)
        symbols = {symbol["id"]: symbol for symbol in table["symbols"]}
        for symbol in ("notice.issue", "mailbox.accept-notice", "mailbox.claim",
                       "mailbox.consume", "host.deliver"):
            self.assertIn(symbol, symbols)
        allowed_somewhere = set()
        for owner in owners.values():
            allowed_somewhere.update(owner["allowed"])
        self.assertEqual(set(symbols), allowed_somewhere,
                         "every frozen symbol needs at least one legal caller")

    def test_event_ownership_missing_table_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        del policy["event_ownership"]
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("event_ownership table is missing", out)

    def test_event_ownership_unknown_symbol_reference_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        host = next(owner for owner in policy["event_ownership"]["owners"]
                    if owner["id"] == "host")
        host["allowed"].append("notice.ghost")
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("references undeclared symbol", out)

    def test_event_ownership_allow_forbid_overlap_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        host = next(owner for owner in policy["event_ownership"]["owners"]
                    if owner["id"] == "host")
        host["allowed"].append("mailbox.claim")
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("both allows and forbids", out)

    def test_event_ownership_package_overlap_needs_exact_files(self):
        policy = copy.deepcopy(self.policy)
        policy["event_ownership"]["owners"].append({
            "id": "shadow",
            "packages": ["."],
            "allowed": ["host.deliver"],
            "forbidden": [],
            "reason": "a package-wide claim beside file-scoped owners",
        })
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("without an exact file list", out)

    def test_event_ownership_file_scope_rejects_wildcard(self):
        policy = copy.deepcopy(self.policy)
        host = next(owner for owner in policy["event_ownership"]["owners"]
                    if owner["id"] == "host")
        host["files"][0] = "host*.go"
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("exact *.go base name", out)

    def test_event_ownership_unknown_role_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["event_ownership"]["symbols"][0]["role"] = "second-state-store"
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("unknown role", out)

    def test_event_ownership_unregistered_symbol_package_is_rejected(self):
        policy = copy.deepcopy(self.policy)
        policy["event_ownership"]["symbols"][0]["pkg"] = "runtime/ghost"
        code, out, _ = self.check_policy(policy)
        self.assertNotEqual(code, 0)
        self.assertIn("not a registered package", out)


class ImplementationTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.policy_path = self.root / "architecture.json"
        self.policy = real_policy()
        self.policy_path.write_text(json.dumps(self.policy, ensure_ascii=False, indent=2),
                                    encoding="utf-8")

    def tearDown(self):
        self._tmp.cleanup()

    def inject(self, text, name="go-list.json"):
        path = self.root / name
        path.write_text(text, encoding="utf-8")
        return path

    def inject_ownership(self, payload):
        text = payload if isinstance(payload, str) else json.dumps(
            payload, ensure_ascii=False, indent=2)
        return self.inject(text, name="event-ownership.json")

    def run_impl(self, text, ownership=None):
        injected = self.inject(text)
        ownership_path = self.inject_ownership(
            ownership if ownership is not None else ownership_report(self.policy))
        return run_checker("--golden", str(self.policy_path), "--implementation",
                           "--repo-root", str(self.repo), "--go-list-json", str(injected),
                           "--event-ownership-json", str(ownership_path), "--json")

    def test_full_tree_passes(self):
        code, out, err = self.run_impl(go_list_stream(self.policy))
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["in_scope_packages"],
                         len(patterns(self.policy)))
        self.assertEqual(payload["counts"]["missing_required_packages"], 0)
        self.assertEqual(payload["counts"]["event_ownership_allowed"], 14)
        self.assertEqual(payload["counts"]["event_ownership_uncovered"], 1)
        self.assertIsNone(payload["productVerified"])
        self.assertTrue(any("injected" in note for note in payload["notes"]))
        self.assertTrue(any("never counted as covered" in note
                            for note in payload["notes"]))

    def test_event_ownership_failure_is_rejected(self):
        ownership = ownership_report(self.policy, ok=False)
        ownership["counts"]["forbidden"] = 1
        ownership["violations"] = [{
            "file": "run.go", "line": 9, "caller": "Assemble",
            "symbol": "mailbox.claim", "owner": "host", "rule": "forbidden",
            "detail": "host may not claim a notification",
        }]
        code, out, _ = self.run_impl(go_list_stream(self.policy), ownership)
        self.assertNotEqual(code, 0)
        self.assertIn("event-ownership forbidden", out)
        self.assertIn("mailbox.claim", out)

    def test_event_ownership_vacuous_report_is_rejected(self):
        ownership = ownership_report(self.policy)
        ownership["counts"]["allowed"] = 0
        code, out, _ = self.run_impl(go_list_stream(self.policy), ownership)
        self.assertNotEqual(code, 0)
        self.assertIn("vacuous", out)

    def test_event_ownership_uncovered_count_must_match_list(self):
        ownership = ownership_report(self.policy)
        ownership["counts"]["uncovered"] = 3
        code, out, _ = self.run_impl(go_list_stream(self.policy), ownership)
        self.assertNotEqual(code, 0)
        self.assertIn("disagrees with 1 listed uncovered site", out)

    def test_event_ownership_ok_with_violations_is_rejected(self):
        ownership = ownership_report(self.policy)
        ownership["violations"] = [{
            "file": "run.go", "line": 9, "symbol": "mailbox.claim",
            "owner": "host", "rule": "forbidden", "detail": "x",
        }]
        code, out, _ = self.run_impl(go_list_stream(self.policy), ownership)
        self.assertNotEqual(code, 0)
        self.assertIn("claims ok while recording violations", out)

    def test_unparseable_event_ownership_report_is_rejected(self):
        code, out, _ = self.run_impl(go_list_stream(self.policy), "{not json at all")
        self.assertNotEqual(code, 0)
        self.assertIn("cannot parse injected event-ownership report", out)

    def test_go_list_injection_requires_ownership_injection(self):
        injected = self.inject(go_list_stream(self.policy))
        code, out, _ = run_checker("--golden", str(self.policy_path), "--implementation",
                                   "--repo-root", str(self.repo),
                                   "--go-list-json", str(injected), "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("needs --event-ownership-json", out)

    def test_unknown_package_is_rejected(self):
        text = go_list_stream(self.policy, extra_imports=None)
        extra = json.dumps({
            "ImportPath": MODULE + "/state/reload",
            "Name": "reload",
            "Dir": "/fake/state/reload",
            "Module": {"Path": MODULE},
            "Imports": [],
        })
        code, out, _ = self.run_impl(text + "\n" + extra)
        self.assertNotEqual(code, 0)
        self.assertIn("unknown first-party package 'state/reload'", out)

    def test_missing_required_package_is_rejected(self):
        code, out, _ = self.run_impl(go_list_stream(self.policy, omit=("features/evolution",)))
        self.assertNotEqual(code, 0)
        self.assertIn("required planned package 'features/evolution' is not implemented", out)

    def test_edge_violation_is_rejected(self):
        text = go_list_stream(self.policy, extra_imports={
            import_path("runtime/state"): [import_path("runtime/agent")]})
        code, out, _ = self.run_impl(text)
        self.assertNotEqual(code, 0)
        self.assertIn("edge violation", out)

    def test_host_shell_back_edge_to_root_is_allowed(self):
        # Consumer anchor: the real cli package imports the composition root. That
        # edge is registered on the cli entry, and pointing it at "." must not read
        # as a violation while the root also lists cli in its registry.
        text = go_list_stream(self.policy, extra_imports={
            import_path("cli"): [import_path(".")]})
        code, out, err = self.run_impl(text)
        self.assertEqual(code, 0, out + err)

    def test_benign_external_substring_is_reported_not_blocked(self):
        # A third-party import whose path merely *contains* the old guess-words
        # (localtool / trpcmirror / shadowtype) is a legal dependency: naming a
        # package is never proof of an unauthorised go.mod write, so it passes.
        text = go_list_stream(self.policy, extra_imports={
            import_path("api/model"): [
                "example.com/upstream/localtool/model",
                "example.com/upstream/trpcmirror/adapter",
                "example.com/vendor/shadowtype",
            ]})
        code, out, err = self.run_impl(text)
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["external_imports"], 3)
        self.assertTrue(any("localtool" in w for w in payload["warnings"]))

    def test_external_import_is_reported_but_never_blocks(self):
        text = go_list_stream(self.policy, extra_imports={
            import_path("api/model"): ["example.com/brand/new/dep"]})
        code, out, err = self.run_impl(text)
        self.assertEqual(code, 0, out + err)
        self.assertNotIn("ERROR", out)

    def test_unparseable_go_list_is_rejected(self):
        code, out, _ = self.run_impl("{not json at all")
        self.assertNotEqual(code, 0)
        self.assertIn("cannot parse go list output", out)

    def test_empty_go_list_is_rejected(self):
        code, out, _ = self.run_impl("")
        self.assertNotEqual(code, 0)
        self.assertIn("implementation not present", out)

    def test_missing_repo_root_is_rejected(self):
        injected = self.inject(go_list_stream(self.policy))
        code, out, _ = run_checker("--golden", str(self.policy_path), "--implementation",
                                   "--repo-root", str(self.root / "gone"),
                                   "--go-list-json", str(injected), "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("repo root not found", out)


if __name__ == "__main__":
    unittest.main()
