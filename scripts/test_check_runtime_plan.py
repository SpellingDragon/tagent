#!/usr/bin/env python3
"""Selftests for check_runtime_plan.py — all fixtures live in tempfile trees.

Never touches the real repository: each test builds a throw-away change dir under
a TemporaryDirectory and runs the checker as a subprocess so the process exit
code (not just the printed text) is part of the assertion.
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
CHECKER = SCRIPTS / "check_runtime_plan.py"

DOMAIN_SPEC = (
    ("00-foundation", 6),
    ("01-state-binding", 6),
    ("02-execution", 9),
    ("03-history", 8),
    ("04-capabilities", 9),
    ("05-inspection", 4),
    ("06-learning", 8),
    ("07-evolution", 4),
    ("08-host", 5),
    ("09-acceptance", 6),
)

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
    for domain, count in DOMAIN_SPEC:
        prefix = domain.split("-", 1)[0]
        ids.extend("{}.{}".format(prefix, i) for i in range(1, count + 1))
    return ids


def graph_tasks():
    tasks = []
    for domain, count in DOMAIN_SPEC:
        prefix = domain.split("-", 1)[0]
        for index in range(1, count + 1):
            tasks.append({
                "id": "{}.{}".format(prefix, index),
                "domain": domain,
                "depends_on": [],
                "phase": "implementation",
                "status": "planned",
                "verification": ["go test ./... -run '^TestThing$' -count=1 -json"],
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
        caps = [domain for domain, _ in DOMAIN_SPEC]
        proposal = ["## Why", "fixture", "", "## Capabilities", "", "### Modified Capabilities", ""]
        proposal += ["- `{}`：测试能力。".format(cap) for cap in caps + ["orchestration"]]
        write(self.change / "proposal.md", "\n".join(proposal) + "\n")
        write(self.change / "design.md", "## Decisions\n\n- D1 fixture\n")
        write(self.change / "coverage.md", "| `orchestration` | 编排 | 00.3 | M | — | fixture |\n")
        write(self.change / "tasks.md", "## 编排\n\n- [ ] 00 域清单（无叶编号）\n")
        write(self.change / "ORCHESTRATION.md", ORCHESTRATION)
        write(self.change / "specs" / "orchestration" / "spec.md",
              GOOD_SPEC.format(cap="orchestration"))
        for domain, count in DOMAIN_SPEC:
            base = self.change / "changes" / domain
            write(base / "proposal.md", "## 边界\n\n能力 `{}` 已声明。\n".format(domain))
            write(base / "design.md", "## 设计\n\nfixture\n")
            prefix = domain.split("-", 1)[0]
            lines = ["## 叶任务", ""]
            for index in range(1, count + 1):
                lines.append("- [ ] {}.{} 占位动作 —— 验证：go test ./... -json".format(prefix, index))
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
        self.assertEqual(payload["counts"]["graph_tasks"], 65)
        self.assertEqual(payload["counts"]["checkboxes"], 65)
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
        for domain, _count in DOMAIN_SPEC:
            path = self.fixture.change / "changes" / domain / "tasks.md"
            text = path.read_text(encoding="utf-8")
            new = text.replace("- [ ] " + tid, "- [x] " + tid, 1)
            if new != text:
                path.write_text(new, encoding="utf-8")
                return

    def test_done_with_evidence_and_ticked_box_is_accepted(self):
        self.tick("02.3")
        self.set_status("02.3", "done", {"run_id": "run-1", "commands": ["go test ./..."],
                                           "exit_codes": [0], "artifact_paths": ["out/run-1.json"]})
        code, out, err = self.check()
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


if __name__ == "__main__":
    unittest.main()
