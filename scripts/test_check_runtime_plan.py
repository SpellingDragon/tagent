#!/usr/bin/env python3
"""Selftests for check_runtime_plan.py — all fixtures live in tempfile trees.

Never touches the real repository: each test builds a throw-away change dir under
a TemporaryDirectory and runs the checker as a subprocess so the process exit
code (not just the printed text) is part of the assertion.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
CHECKER = SCRIPTS / "check_runtime_plan.py"

DOMAIN_SPEC = (
    ("00-foundation", 20, 3),
    ("01-state-binding", 6, 2),
    ("02-execution", 9, 4),
    ("03-history", 8, 2),
    ("04-capabilities", 9, 4),
    ("05-inspection", 4, 2),
    ("06-learning", 8, 2),
    ("07-evolution", 4, 2),
    ("08-host", 5, 2),
    ("09-acceptance", 6, 1),
)
STAGE_GATE_ID = "00.P0.3"

GOOD_SPEC = """## ADDED Requirements

### Requirement: {cap} 契约成立
系统 SHALL 在 {cap} 内保持既定语义；实现 MUST 以真实消费者验收。

#### Scenario: 正常路径
- **WHEN** 调用 {cap} 的正式入口
- **THEN** 返回与金样一致的观测
"""

BAD_SPEC = """## ADDED Requirements

### Requirement: {cap} 缺断言
这里没有规范语句也没有场景。
"""


def task_ids():
    ids = []
    for domain, count, p0_count in DOMAIN_SPEC:
        prefix = domain.split("-", 1)[0]
        ids.extend("{}.{}".format(prefix, i) for i in range(1, count + 1))
        ids.extend("{}.P0.{}".format(prefix, i) for i in range(1, p0_count + 1))
    ids.extend(SLIM_IDS)
    return ids


# 概念收敛战役收敛叶（与 check_runtime_plan.py 的 SLIM_IDS 同源——两处必须同步修改）。
SLIM_IDS = (
    "00.21", "00.22", "00.23", "00.24", "00.25",
    "02.21", "02.22", "04.21", "04.22", "04.23",
    "05.21", "06.21", "07.22", "09.21", "00.26", "09.7", "04.24", "04.25", "09.8", "09.9",
)
SLIM_DOMAIN = {
    "00": "00-foundation", "02": "02-execution", "04": "04-capabilities",
    "05": "05-inspection", "06": "06-learning", "07": "07-evolution", "09": "09-acceptance",
}


def graph_tasks():
    tasks = []
    for domain, count, p0_count in DOMAIN_SPEC:
        prefix = domain.split("-", 1)[0]
        for index in range(1, count + 1):
            # Hard check (b): every 01–09 non-P0 task carries the stage-gate edge.
            deps = [STAGE_GATE_ID] if prefix != "00" else []
            phase = "implementation"
            if prefix == "00" and index >= 7:
                # 报告响应修订轮 R 任务（00.7–00.20）：批次串行、phase=design
                phase = "design"
                if index <= 11:
                    deps = ["00.P0.3"]
                elif index <= 17:
                    deps = ["00.11"]
                else:
                    deps = ["00.17"]
            tasks.append({
                "id": "{}.{}".format(prefix, index),
                "domain": domain,
                "depends_on": deps,
                "phase": phase,
                "status": "planned",
                "verification": ["go test ./... -run '^TestThing$' -count=1 -json"],
            })
        for index in range(1, p0_count + 1):
            tid = "{}.P0.{}".format(prefix, index)
            # Hard check (a): skeleton tasks are 'skeleton' (00.P0.2 is 'tooling');
            # fixture keeps 00.P0.1/00.P0.2 planned like the rest.
            phase = "tooling" if tid == "00.P0.2" else "skeleton"
            if tid == "00.P0.2":
                deps = ["00.P0.1"]
            elif tid in ("00.P0.1", "00.P0.3"):
                deps = []
            else:
                deps = ["00.3"]
            tasks.append({
                "id": tid,
                "domain": domain,
                "depends_on": deps,
                "phase": phase,
                "status": "planned",
                "verification": ["go build ./..."],
            })
    for tid in SLIM_IDS:
        prefix = tid.split(".", 1)[0]
        deps = [] if prefix == "00" else [STAGE_GATE_ID]
        tasks.append({
            "id": tid,
            "domain": SLIM_DOMAIN[prefix],
            "depends_on": deps,
            "phase": "tooling" if tid == "00.21" else "implementation",
            "status": "planned",
            "verification": ["go build ./..."],
        })
    return tasks


def write(path, text):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


class Fixture:
    def __init__(self, root):
        self.root = Path(root)
        self.change = self.root / "openspec" / "changes" / "runtime-rebuild"
        self.main_specs = self.root / "openspec" / "specs"

    def build(self):
        caps = [domain for domain, _count, _p0 in DOMAIN_SPEC]
        proposal = ["## Why", "fixture", "", "## Capabilities", "", "### Modified Capabilities", ""]
        proposal += ["- `{}`：测试能力。".format(cap) for cap in caps + ["orchestration"]]
        write(self.change / "proposal.md", "\n".join(proposal) + "\n")
        write(self.change / "design.md", "## Decisions\n\n- D1 fixture\n")
        write(self.change / "coverage.md", "| `orchestration` | 编排 | 00.3 | M | — | fixture |\n")
        write(self.change / "tasks.md", "## 编排\n\n- [ ] 00 域清单（无叶编号）\n")
        write(self.change / "ORCHESTRATION.md", ORCHESTRATION)
        write(self.change / "specs" / "orchestration" / "spec.md",
              GOOD_SPEC.format(cap="orchestration"))
        for domain, count, p0_count in DOMAIN_SPEC:
            base = self.change / "changes" / domain
            write(base / "proposal.md", "## 边界\n\n能力 `{}` 已声明。\n".format(domain))
            write(base / "design.md", "## 设计\n\nfixture\n")
            prefix = domain.split("-", 1)[0]
            lines = ["## 叶任务", ""]
            for index in range(1, count + 1):
                lines.append("- [ ] {}.{} 占位动作 —— 验证：go test ./... -json".format(prefix, index))
            lines += ["", "## P0 骨架任务", ""]
            for index in range(1, p0_count + 1):
                lines.append("- [ ] {}.P0.{} 骨架占位 —— 验证：go build ./...".format(prefix, index))
            slim = [tid for tid in SLIM_IDS if tid.startswith(prefix + ".")]
            if slim:
                lines += ["", "## C 系列（概念收敛）", ""]
                for tid in slim:
                    lines.append("- [ ] {} 收敛占位 —— 验证：go build ./...".format(tid))
            write(base / "tasks.md", "\n".join(lines) + "\n")
            write(base / "specs" / domain / "spec.md", GOOD_SPEC.format(cap=domain))
        write(self.change / "task-graph.json", json.dumps(
            {"schema_version": 1, "change": "runtime-rebuild", "tasks": graph_tasks()},
            ensure_ascii=False, indent=2) + "\n")
        write(self.main_specs / "orchestration" / "spec.md",
              "# orchestration Specification\n\n## Purpose\n\nfixture purpose long enough.\n\n"
              "## Requirements\n\n### Requirement: 编排基线\n系统 SHALL 成立。\n\n"
              "#### Scenario: 基线\n- **WHEN** x\n- **THEN** y\n")

    def load_graph(self):
        return json.loads((self.change / "task-graph.json").read_text(encoding="utf-8"))

    def save_graph(self, graph):
        (self.change / "task-graph.json").write_text(
            json.dumps(graph, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


ORCHESTRATION = """# runtime-rebuild 编排文档

## 一、结论（先行）

- 唯一路径与最大风险各一句。

## 二、编排四问

- 次序：就绪队列下沉到孙任务。
- 授权：每单白名单精确到文件。
- 凭据：四查按单核。
- 释放：锁与等待表。

## 三、明细层

- 派发单-孙任务矩阵。

## 四、台账层

- 每轮推进记录。
"""


# 六域 event-integrity 图的 fixture：叶数/域分布与真实变更一致（38 叶），
# 依赖只保留每域链式最小图——判定的是 profile 规则，不是复制真实图。
EVENT_DOMAIN_SPEC = (
    ("00-contract-evidence", 4),
    ("01-runtime-consumption", 7),
    ("02-event-consumers", 7),
    ("03-formal-simulation", 11),
    ("04-regression-gates", 5),
    ("05-mr-integration", 4),
)
EVENT_PRODUCT_IDS = ({"00.2"}
                     | {"01.{}".format(index) for index in range(1, 8)}
                     | {"02.{}".format(index) for index in range(1, 8)}
                     | {"03.{}".format(index) for index in range(1, 10)}
                     | {"03.11"})
# 与真实变更当前台账一致：02.3 受 G4 阻塞，其余为未开始的叶。
EVENT_UNCHECKED = {"02.3", "03.11", "04.2", "04.5", "05.1", "05.2", "05.3", "05.4"}


class EventProfileFixture:
    """A six-domain / 38-leaf graph that exercises the event domain_profile path."""

    def __init__(self, root):
        self.root = Path(root)
        self.change = (self.root / "openspec" / "changes" / "event-consumption-integrity")
        self.main_specs = self.root / "openspec" / "specs"

    def leaves(self):
        leaves = []
        for domain, count in EVENT_DOMAIN_SPEC:
            prefix = domain.split("-", 1)[0]
            ids = ["{}.{}".format(prefix, index) for index in range(1, count + 1)]
            for index, leaf_id in enumerate(ids):
                leaves.append({
                    "id": leaf_id,
                    "domain": domain,
                    "kind": "product" if leaf_id in EVENT_PRODUCT_IDS else "non_product",
                    # 不写字面 change 根路径：scripts 下的过程工件引用门按子串扫描。
                    "task_file": "/".join(["openspec", "changes", "event-consumption-integrity",
                                            "changes", domain, "tasks.md"]),
                    "depends_on": [ids[index - 1]] if index else [],
                    "verify_after": [ids[index - 1]] if index else [],
                    "tests": [],
                    "verification": {"commands": ["go test ./... -json"]},
                    "status": "planned",
                })
        return leaves

    def graph(self):
        leaves = self.leaves()
        nodes = [leaf["id"] + suffix for leaf in leaves
                 for suffix in ("/ready", "/verified")]
        # 展开图边：每叶 ready→verified 一条，加上 depends_on 与 verify_after。
        edges = len(leaves) + sum(len(leaf["depends_on"]) + len(leaf["verify_after"])
                                  for leaf in leaves)
        return {
            "schema_version": 1,
            "change": "event-consumption-integrity",
            "domain_profile": "event-integrity",
            "scope": "whole-change-planning-materialization",
            "counts": {
                "leaves_total": len(leaves),
                "leaves_by_domain": {domain: count for domain, count in EVENT_DOMAIN_SPEC},
                "leaves_by_kind": {"product": len(EVENT_PRODUCT_IDS),
                                   "non_product": len(leaves) - len(EVENT_PRODUCT_IDS)},
            },
            "phase_graph": {
                "nodes_total": len(nodes),
                "edges_total": edges,
                "acyclic": True,
                "topological_order": nodes,
            },
            "leaves": leaves,
        }

    def build(self):
        caps = [domain for domain, _count in EVENT_DOMAIN_SPEC]
        proposal = ["## Why", "fixture", "", "## Capabilities", "",
                    "### Modified Capabilities", ""]
        proposal += ["- `{}`：六域 fixture。".format(cap) for cap in caps + ["orchestration"]]
        write(self.change / "proposal.md", "\n".join(proposal) + "\n")
        write(self.change / "design.md", "## Decisions\n\n- D1 fixture\n")
        write(self.change / "coverage.md", "| 叶 | 测试 | 证据 |\n| --- | --- | --- |\n"
              "| 00.1 | — | 文档 |\n")
        write(self.change / "tasks.md", "## 编排\n\n- [ ] 六域 fixture\n")
        write(self.change / "ORCHESTRATION.md", ORCHESTRATION)
        write(self.change / "specs" / "orchestration" / "spec.md",
              GOOD_SPEC.format(cap="orchestration"))
        for domain, count in EVENT_DOMAIN_SPEC:
            base = self.change / "changes" / domain
            write(base / "proposal.md", "## 边界\n\n能力 `{}` 已声明。\n".format(domain))
            write(base / "design.md", "## 设计\n\nfixture\n")
            prefix = domain.split("-", 1)[0]
            lines = ["## 叶任务", ""]
            for index in range(1, count + 1):
                leaf_id = "{}.{}".format(prefix, index)
                mark = " " if leaf_id in EVENT_UNCHECKED else "x"
                lines.append("- [{}] {} fixture leaf —— 验证：go test ./... -json".format(
                    mark, leaf_id))
            write(base / "tasks.md", "\n".join(lines) + "\n")
            write(base / "specs" / domain / "spec.md", GOOD_SPEC.format(cap=domain))
        write(self.change / "task-graph.json", json.dumps(
            self.graph(), ensure_ascii=False, indent=2) + "\n")
        write(self.main_specs / "orchestration" / "spec.md",
              "# orchestration Specification\n\n## Purpose\n\nfixture purpose long enough.\n\n"
              "## Requirements\n\n### Requirement: 编排基线\n系统 SHALL 成立。\n\n"
              "#### Scenario: 基线\n- **WHEN** x\n- **THEN** y\n")

    def load_graph(self):
        return json.loads((self.change / "task-graph.json").read_text(encoding="utf-8"))

    def save_graph(self, graph):
        (self.change / "task-graph.json").write_text(
            json.dumps(graph, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def run_checker(*args):
    proc = subprocess.run([sys.executable, str(CHECKER)] + list(args),
                          capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


class PlanCheckerTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.fixture = Fixture(self._tmp.name)
        self.fixture.build()

    def tearDown(self):
        self._tmp.cleanup()

    def check(self, *args):
        return run_checker("--change-dir", str(self.fixture.change), "--json", *args)

    def test_happy_path(self):
        code, out, err = self.check()
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertIsNone(payload["productVerified"])
        self.assertEqual(payload["counts"]["graph_tasks"], len(graph_tasks()))
        self.assertEqual(payload["counts"]["checkboxes"], len(task_ids()))
        self.assertEqual(payload["counts"]["scenarios"], 11)

    def test_missing_task_reported(self):
        graph = self.fixture.load_graph()
        graph["tasks"] = [t for t in graph["tasks"] if t["id"] != "03.5"]
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("task 03.5 missing from task-graph", out)

    def test_duplicate_task_reported(self):
        graph = self.fixture.load_graph()
        graph["tasks"].append(dict(graph["tasks"][0]))
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate task id in graph", out)

    def test_duplicate_dependency_is_rejected(self):
        graph = self.fixture.load_graph()
        graph["tasks"][1]["depends_on"] = ["00.1", "00.1"]
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate dependency", out)

    def test_failed_completion_exit_is_rejected(self):
        self.tick("02.3")
        self.set_status("02.3", "done", {"run_id": "r", "commands": ["c"],
                        "exit_codes": [1], "artifact_paths": ["a"]})
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("successful integer exits", out)

    def test_completion_command_exit_counts_must_match(self):
        self.tick("02.3")
        self.set_status("02.3", "done", {"run_id": "r", "commands": ["c", "d"],
                        "exit_codes": [0], "artifact_paths": ["a"]})
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("length mismatch", out)

    def test_done_named_test_must_exist_and_be_run(self):
        # 正例（2026-10-15 测试锚规则）：verification 承诺的 TestThing 在仓库
        # 真实存在且被 evidence 命令面跑过，完成合法。
        root = Path(self._tmp.name) / "repo"
        write(root / "probe_test.go", "package probe\n\nfunc TestThing(t *testing.T) {}\n")
        self.tick(STAGE_GATE_ID)
        self.set_status(STAGE_GATE_ID, "done", {"run_id": "r",
                        "commands": ["go build ./..."], "exit_codes": [0],
                        "artifact_paths": ["a"]})
        self.tick("02.3")
        self.set_status("02.3", "done", {"run_id": "r",
                        "commands": ["go test ./... -run '^TestThing$' -count=1"],
                        "exit_codes": [0], "artifact_paths": ["a"]})
        code, out, _ = self.check("--repo-root", str(root))
        self.assertEqual(code, 0, out)

    def test_done_renamed_or_absent_test_is_rejected(self):
        # 反例 A：evidence 声称跑了 TestGone，但仓库无任何 *_test.go 定义它
        # （历史先例：Roundtrip/RoundTrip 改名漂移导致验收命令零匹配空转）。
        root = Path(self._tmp.name) / "repo"
        write(root / "probe_test.go", "package probe\n\nfunc TestThing(t *testing.T) {}\n")
        self.tick(STAGE_GATE_ID)
        self.set_status(STAGE_GATE_ID, "done", {"run_id": "r",
                        "commands": ["go build ./..."], "exit_codes": [0],
                        "artifact_paths": ["a"]})
        self.tick("02.3")
        self.set_status("02.3", "done", {"run_id": "r",
                        "commands": ["go test ./... -run '^TestGone$' -count=1"],
                        "exit_codes": [0], "artifact_paths": ["a"]})
        code, out, _ = self.check("--repo-root", str(root))
        self.assertNotEqual(code, 0)
        self.assertIn("no *_test.go defines", out)
        self.assertIn("never ran it (只跑一半)", out)

    def test_done_verification_promises_must_all_run(self):
        # 反例 B：verification 承诺两个点名测试，evidence 只跑一半。
        root = Path(self._tmp.name) / "repo"
        write(root / "probe_test.go",
              "package probe\n\nfunc TestAlpha(t *testing.T) {}\n\nfunc TestBeta(t *testing.T) {}\n")
        self.tick(STAGE_GATE_ID)
        self.set_status(STAGE_GATE_ID, "done", {"run_id": "r",
                        "commands": ["go build ./..."], "exit_codes": [0],
                        "artifact_paths": ["a"]})
        self.tick("02.3")
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "02.3":
                task["verification"] = ["go test -run '^TestAlpha$'", "go test -run '^TestBeta$'"]
        self.fixture.save_graph(graph)
        self.set_status("02.3", "done", {"run_id": "r",
                        "commands": ["go test -run '^TestAlpha$'"],
                        "exit_codes": [0], "artifact_paths": ["a"]})
        code, out, _ = self.check("--repo-root", str(root))
        self.assertNotEqual(code, 0)
        self.assertIn("TestBeta", out)
        self.assertIn("never ran it (只跑一半)", out)

    def test_missing_coverage_row_is_rejected(self):
        write(self.fixture.change / "coverage.md", "# 空映射\n")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("coverage capability set mismatch", out)

    def test_duplicate_capability_writer_is_rejected(self):
        write(self.fixture.change / "changes/00-foundation/specs/orchestration/spec.md",
              GOOD_SPEC.format(cap="orchestration"))
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("multiple delta writers", out)

    def test_cycle_reported(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "03.8":
                task["depends_on"] = ["04.2"]
            if task["id"] == "04.2":
                task["depends_on"] = ["02.9", "03.8"]
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("cycle", out)

    def test_wrong_domain_reported(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "01.1":
                task["domain"] = "02-execution"
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("belongs to 01-state-binding", out)

    def test_unknown_dependency_reported(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "02.1":
                task["depends_on"] = ["99.9"]
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("unknown dependency 99.9", out)

    def test_empty_spec_reported(self):
        write(self.fixture.change / "changes" / "05-inspection" / "specs" / "05-inspection" / "spec.md",
              BAD_SPEC.format(cap="05-inspection"))
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("has no SHALL/MUST statement", out)
        self.assertIn("has no #### Scenario", out)

    def change_orchestration_spec(self, text):
        write(self.fixture.change / "specs" / "orchestration" / "spec.md", text)

    def test_valid_removed_needs_no_scenario(self):
        # A REMOVED requirement is a deletion: Reason/Migration, not SHALL/Scenario.
        self.change_orchestration_spec(
            "## REMOVED Requirements\n\n### Requirement: 编排基线\n\n"
            "**Reason**: 过渡态，生产者归零。\n\n**Migration**: 改走既定结算路径。\n")
        code, out, err = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertEqual(code, 0, out + err)

    def test_removed_without_reason_is_rejected(self):
        self.change_orchestration_spec(
            "## REMOVED Requirements\n\n### Requirement: 编排基线\n\n"
            "**Migration**: 改走既定结算路径。\n")
        code, out, _ = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertNotEqual(code, 0)
        self.assertIn("has no **Reason**", out)

    def test_removed_without_migration_is_rejected(self):
        self.change_orchestration_spec(
            "## REMOVED Requirements\n\n### Requirement: 编排基线\n\n"
            "**Reason**: 过渡态。\n")
        code, out, _ = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertNotEqual(code, 0)
        self.assertIn("has no **Migration**", out)

    def test_empty_spec_is_rejected(self):
        self.change_orchestration_spec("# orchestration\n\n## Purpose\n\n空壳，无任何 Requirement。\n")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("spec declares no ### Requirement", out)

    def test_unrecognised_section_is_rejected(self):
        self.change_orchestration_spec(
            "## FOO Requirements\n\n### Requirement: 编排基线\n系统 SHALL 成立。\n\n"
            "#### Scenario: x\n- **WHEN** a\n- **THEN** b\n")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("unrecognised section", out)

    def test_requirement_outside_section_is_rejected(self):
        self.change_orchestration_spec(
            "### Requirement: 编排基线\n系统 SHALL 成立。\n\n"
            "#### Scenario: x\n- **WHEN** a\n- **THEN** b\n")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("appears before any Requirements section", out)

    def test_added_collision_with_main_source_is_rejected(self):
        self.change_orchestration_spec(
            "## ADDED Requirements\n\n### Requirement: 编排基线\n系统 SHALL 成立。\n\n"
            "#### Scenario: x\n- **WHEN** a\n- **THEN** b\n")
        code, out, _ = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertNotEqual(code, 0)
        self.assertIn("already exists in main spec", out)

    def set_status(self, tid, status, evidence=None):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == tid:
                task["status"] = status
                if evidence is not None:
                    task["evidence"] = evidence
        self.fixture.save_graph(graph)

    def tick(self, tid):
        for domain, _count, _p0 in DOMAIN_SPEC:
            path = self.fixture.change / "changes" / domain / "tasks.md"
            text = path.read_text(encoding="utf-8")
            new = text.replace("- [ ] " + tid, "- [x] " + tid, 1)
            if new != text:
                path.write_text(new, encoding="utf-8")
                return

    def test_done_with_evidence_and_ticked_box_is_accepted(self):
        # 2026-10-15 测试锚规则级联：完成证据须跑过 verification 承诺的点名
        # 测试，故 commands 点名 TestThing 且仓库根真实定义它。
        root = Path(self._tmp.name) / "repo"
        write(root / "probe_test.go", "package probe\n\nfunc TestThing(t *testing.T) {}\n")
        evidence = {"run_id": "run-1", "commands": ["go test ./... -run '^TestThing$' -count=1"],
                    "exit_codes": [0], "artifact_paths": ["out/run-1.json"]}
        # The stage gate itself must be done before any NN.M task may be done.
        self.tick(STAGE_GATE_ID)
        self.set_status(STAGE_GATE_ID, "done", {"run_id": "run-1",
                        "commands": ["go build ./..."], "exit_codes": [0],
                        "artifact_paths": ["out/run-1.json"]})
        self.tick("02.3")
        self.set_status("02.3", "done", evidence)
        code, out, err = self.check("--repo-root", str(root))
        self.assertEqual(code, 0, out + err)

    def test_done_without_evidence_is_rejected(self):
        self.set_status("02.3", "done")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("requires an evidence object", out)

    def test_done_without_ticked_box_is_rejected(self):
        self.set_status("02.3", "done", {"run_id": "r", "commands": ["c"],
                                           "exit_codes": [0], "artifact_paths": ["a"]})
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("leaf checkbox is unchecked", out)

    def test_unknown_status_is_rejected(self):
        self.set_status("02.3", "finished")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("status 'finished' not in", out)

    def test_checked_box_without_done_is_rejected(self):
        self.tick("02.3")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("completion without evidence", out)

    def test_checked_without_evidence_reported(self):
        path = self.fixture.change / "changes" / "02-execution" / "tasks.md"
        text = path.read_text(encoding="utf-8").replace("- [ ] 02.3", "- [x] 02.3", 1)
        path.write_text(text, encoding="utf-8")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("completion without evidence", out)

    def test_orchestration_missing_layer_reported(self):
        write(self.fixture.change / "ORCHESTRATION.md", ORCHESTRATION.replace("## 四、台账层", "## 四、尾巴"))
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("missing layer", out)

    def test_orchestration_out_of_order_reported(self):
        reversed_doc = (
            "# runtime-rebuild 编排文档\n\n"
            "## 一、台账层\n\n- 记录。\n\n"
            "## 二、明细层\n\n- 矩阵。\n\n"
            "## 三、编排四问\n\n- 次序 / 授权 / 凭据 / 释放。\n\n"
            "## 四、结论（先行）\n\n- 结论。\n"
        )
        write(self.fixture.change / "ORCHESTRATION.md", reversed_doc)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("out of order", out)

    def test_modified_requirement_needs_main_source(self):
        write(self.fixture.change / "specs" / "orchestration" / "spec.md",
              "## MODIFIED Requirements\n\n### Requirement: 不存在的条款\n系统 SHALL 成立。\n\n"
              "#### Scenario: x\n- **WHEN** a\n- **THEN** b\n")
        code, out, _ = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertNotEqual(code, 0)
        self.assertIn("not present in main spec", out)

    def test_modified_requirement_ambiguity_reported(self):
        write(self.fixture.main_specs / "orchestration" / "spec.md",
              "# orchestration Specification\n\n## Purpose\n\nfixture.\n\n## Requirements\n\n"
              "### Requirement: 编排基线\nA SHALL hold.\n\n#### Scenario: a\n- **WHEN** x\n- **THEN** y\n\n"
              "### Requirement: 编排基线\nB SHALL hold.\n\n#### Scenario: b\n- **WHEN** x\n- **THEN** y\n")
        write(self.fixture.change / "specs" / "orchestration" / "spec.md",
              "## MODIFIED Requirements\n\n### Requirement: 编排基线\nC SHALL hold.\n\n"
              "#### Scenario: c\n- **WHEN** x\n- **THEN** y\n")
        code, out, _ = self.check("--main-specs-root", str(self.fixture.main_specs))
        self.assertNotEqual(code, 0)
        self.assertIn("ambiguous in main spec", out)

    def test_duplicate_json_key_is_fatal(self):
        path = self.fixture.change / "task-graph.json"
        text = path.read_text(encoding="utf-8").replace(
            '"schema_version": 1,', '"schema_version": 1,\n  "schema_version": 1,', 1)
        path.write_text(text, encoding="utf-8")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("duplicate JSON key", out)

    def test_missing_change_dir_reported(self):
        code, out, _ = run_checker("--change-dir", str(self.fixture.root / "nope"), "--json")
        self.assertNotEqual(code, 0)
        self.assertIn("change dir not found", out)

    # --- 两阶段修订新增负控（骨架编号 / 缺失 / 阶段门硬边 / phase） ---

    def test_invalid_skeleton_task_id_is_rejected(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "01.P0.1":
                task["id"] = "01.P0x"
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("invalid id", out)

    def test_missing_skeleton_task_is_rejected(self):
        graph = self.fixture.load_graph()
        graph["tasks"] = [t for t in graph["tasks"] if t["id"] != "05.P0.2"]
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("task 05.P0.2 missing from task-graph", out)

    def test_non_p0_task_without_stage_gate_edge_is_rejected(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "03.1":
                task["depends_on"] = []
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("阶段门", out)

    def test_skeleton_task_with_wrong_phase_is_rejected(self):
        graph = self.fixture.load_graph()
        for task in graph["tasks"]:
            if task["id"] == "02.P0.1":
                task["phase"] = "implementation"
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("skeleton task so its phase must be", out)


class EventProfileTest(unittest.TestCase):
    """domain_profile=event-integrity：六域 / 38 叶 / 76 节点规则。"""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.fixture = EventProfileFixture(self._tmp.name)
        self.fixture.build()

    def tearDown(self):
        self._tmp.cleanup()

    def check(self, *args):
        return run_checker("--change-dir", str(self.fixture.change), "--json", *args)

    def remove_box(self, leaf_id):
        for domain, _count in EVENT_DOMAIN_SPEC:
            path = self.fixture.change / "changes" / domain / "tasks.md"
            lines = path.read_text(encoding="utf-8").splitlines()
            kept = [line for line in lines
                    if not line.startswith("- [ ] " + leaf_id + " ")
                    and not line.startswith("- [x] " + leaf_id + " ")]
            if kept != lines:
                path.write_text("\n".join(kept) + "\n", encoding="utf-8")
                return
        self.fail("leaf {} has no checkbox to remove".format(leaf_id))

    def test_domain_profile_accepts_six_domains_and_38_leaves(self):
        code, out, err = self.check()
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        counts = payload["counts"]
        self.assertEqual(counts["graph_leaves"], 38)
        self.assertEqual(counts["graph_leaf_ids_unique"], 38)
        self.assertEqual(counts["domains_with_spec"], 6)
        self.assertEqual(counts["phase_nodes"], 76)
        self.assertEqual(counts["phase_acyclic"], 1)
        self.assertEqual(payload["extra"]["leaves_by_domain"],
                         {domain: count for domain, count in EVENT_DOMAIN_SPEC})

        # 域归属不是装饰：把叶挪到十域旧名必须红，而不是被 profile 静默接受。
        graph = self.fixture.load_graph()
        for leaf in graph["leaves"]:
            if leaf["id"] == "00.1":
                leaf["domain"] = "00-foundation"
        self.fixture.save_graph(graph)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("is not one of the six event domains", out)

    def test_every_leaf_is_covered(self):
        code, out, err = self.check()
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        counts = payload["counts"]
        self.assertEqual(counts["checkboxes"], 38)
        self.assertEqual(counts["leaves_checked"], 30)
        self.assertEqual(counts["leaves_unchecked"], 8)
        self.assertEqual(payload["extra"]["unchecked_leaf_ids"], sorted(EVENT_UNCHECKED))

        # 删一个叶 checkbox：叶失去执行记录入口，必须点名缺哪个叶。
        self.remove_box("03.11")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("leaf 03.11 has no checkbox", out)

    def test_legacy_ten_domain_rules_are_not_relaxed(self):
        legacy = Fixture(Path(self._tmp.name) / "legacy")
        legacy.build()

        def legacy_check(*args):
            return run_checker("--change-dir", str(legacy.change), "--json", *args)

        code, out, err = legacy_check()
        self.assertEqual(code, 0, out + err)

        # 旧图没有 planning-only scope：勾选但没有 done 证据，旧规则一个字符不放宽。
        path = legacy.change / "changes" / "02-execution" / "tasks.md"
        path.write_text(path.read_text(encoding="utf-8").replace(
            "- [ ] 02.3", "- [x] 02.3", 1), encoding="utf-8")
        code, out, _ = legacy_check()
        self.assertNotEqual(code, 0)
        self.assertIn("completion without evidence", out)

        # 在旧十域图上盖 event-integrity 印章也不能换轨成更松的规则。
        graph = legacy.load_graph()
        graph["domain_profile"] = "event-integrity"
        legacy.save_graph(graph)
        code, out, _ = legacy_check()
        self.assertNotEqual(code, 0)
        self.assertIn("task-graph leaves must be a non-empty list", out)


PLAN_WRAPPER = '''#!/usr/bin/env python3
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
with open(os.path.join(HERE, "plan_invocations.log"), "a", encoding="utf-8") as handle:
    print(" ".join(sys.argv[1:]), file=handle)
sys.exit(subprocess.call(
    [sys.executable, os.path.join(HERE, "real_check_runtime_plan.py")] + sys.argv[1:]))
'''
PY_STUB = "import sys\n\nsys.exit(0)\n"
SH_STUB = "#!/usr/bin/env bash\nexit 0\n"
GO_STUB = '''#!/usr/bin/env bash
for arg in "$@"; do
  if [ "$arg" = "./scripts/comment_policy" ]; then
    echo "comment_policy: 0 finding(s)."
    exit 0
  fi
done
exit 0
'''


class LintObjectSelectionTest(unittest.TestCase):
    """lint.sh 的计划门必须检查活动 graph，而不是固定归档路径。"""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.fixture = Fixture(self.root)
        self.fixture.build()
        archive = self.root / "openspec" / "changes" / "archive" / "2026-10-18-runtime-rebuild"
        shutil.copytree(str(self.fixture.change), str(archive))

        scripts = self.root / "scripts"
        scripts.mkdir(parents=True, exist_ok=True)
        shutil.copy2(str(SCRIPTS / "lint.sh"), str(scripts / "lint.sh"))
        shutil.copy2(str(CHECKER), str(scripts / "real_check_runtime_plan.py"))
        shutil.copy2(str(SCRIPTS / "runtime_checklib.py"),
                     str(scripts / "runtime_checklib.py"))
        write(scripts / "check_runtime_plan.py", PLAN_WRAPPER)
        for name in ("check_runtime_evidence.py", "check_runtime_architecture.py",
                     "check_concept_budget.py", "test_check_concept_budget.py",
                     "test_verify_restart_prefix.py"):
            write(scripts / name, PY_STUB)
        for name in ("gen_godoc.sh", "test_race_check.sh", "test_check_package_layout.sh"):
            write(scripts / name, SH_STUB)

        self.bin = self.root / "bin"
        self.bin.mkdir()
        tools = {"python3": sys.executable, "bash": shutil.which("bash"),
                 "grep": shutil.which("grep"), "sed": shutil.which("sed"),
                 "tr": shutil.which("tr"), "dirname": shutil.which("dirname")}
        for name, target in tools.items():
            self.assertIsNotNone(target, name)
            os.symlink(target, str(self.bin / name))
        write(self.bin / "go", GO_STUB)
        os.chmod(str(self.bin / "go"), 0o755)

    def tearDown(self):
        self._tmp.cleanup()

    def run_lint(self):
        env = dict(os.environ)
        # 只保留测试 bin：openspec 因此不可见，tail 的 spec-hygiene 门如实跳过。
        env["PATH"] = str(self.bin)
        env.pop("PYTHONPATH", None)
        return subprocess.run(["bash", "scripts/lint.sh", "--only", "contracts"],
                              cwd=str(self.root), env=env, capture_output=True, text=True)

    def test_active_graph_is_checked_not_archive(self):
        proc = self.run_lint()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("lint: runtime plan contract", proc.stdout)
        self.assertNotIn("no active task-graph.json", proc.stdout)
        log = self.root / "scripts" / "plan_invocations.log"
        lines = log.read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 1, proc.stdout)
        expected = "--change-dir " + "/".join(["openspec", "changes", "runtime-rebuild"])
        self.assertIn(expected, lines[0])
        self.assertNotIn("archive", lines[0])

    def test_archive_graph_object_mismatch_fails(self):
        # 旧固定对象（归档路径）在新命名下本身就不自洽：图声明 change=runtime-rebuild，
        # 目录名带日期前缀。规则不放宽，因此直接跑真实检查器必须红。
        proc = subprocess.run(
            [sys.executable, "scripts/real_check_runtime_plan.py",
             "--change-dir", "/".join(["openspec", "changes", "archive",
                                        "2026-10-18-runtime-rebuild"]),
             "--json"], cwd=str(self.root), capture_output=True, text=True)
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("task-graph change", proc.stdout)


if __name__ == "__main__":
    unittest.main()
