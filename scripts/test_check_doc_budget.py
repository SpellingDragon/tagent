#!/usr/bin/env python3
"""Selftests for check_doc_budget.py — tempfile fixtures only, no product writes.

覆盖棘轮门的正负锚：
  * 正锚：实测=基线 → exit 0；实测<基线 → exit 0 且提示可下调；--tighten 下调成功；
  * 负锚：超基线（篇数/行数分别）→ exit 1；--tighten 试图上调 → exit 1 且文件不动；
    0 篇 / 基线缺失 / 基线形状坏 → exit 1（不许空转判绿）；
  * 口径锚：gitignored 不入预算、未提交但未 ignore 的计入、非 markdown 不计、
    末尾无换行的残行计一行；`openspec/changes/**`（活跃计划与归档）作为过程工件
    不入预算，而 `openspec/specs/` 作为权威规格仍然计入；
  * 消费者锚：仓库基线文件与现树实测一致（基线=实测，不是抄来的数），
    scripts/lint.sh 把门接进了 comment policy 同一位置。
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
CHECKER = SCRIPTS / "check_doc_budget.py"
REPO_ROOT = SCRIPTS.parent
BASELINE_NAME = Path("scripts") / "doc_budget.json"
sys.path.insert(0, str(SCRIPTS))

import check_doc_budget as budget  # noqa: E402  (needs scripts/ on sys.path)


class FixtureCase(unittest.TestCase):
    """One temp git repo per test: the gate must be provable without touching the product tree."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.git("init", "-q")

    def git(self, *args):
        proc = subprocess.run(["git", "-C", str(self.root), *args],
                              capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        return proc.stdout

    def write(self, rel, text, stage=True):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        if stage:
            self.git("add", "--", rel)
        return path

    def baseline(self, files, lines, schema_version=1, raw=None):
        if raw is not None:  # 逐字写盘，用来构造坏 JSON
            return self.write(str(BASELINE_NAME), raw)
        payload = {
            "schema_version": schema_version,
            "baseline": {"files": files, "lines": lines},
        }
        return self.write(str(BASELINE_NAME), json.dumps(payload))

    def cli(self, *args):
        proc = subprocess.run(
            [sys.executable, str(CHECKER), "--repo-root", str(self.root), *args],
            capture_output=True, text=True,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def report(self, *args):
        code, out, err = self.cli("--json", *args)
        self.assertTrue(out.strip(), "no JSON on stdout: " + err)
        return code, json.loads(out)


class PassAndWarnTest(FixtureCase):
    def test_at_baseline_passes(self):
        self.write("a.md", "one\ntwo\n")
        self.write("docs/b.md", "three\n")
        self.baseline(files=2, lines=3)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)
        self.assertIn("RESULT: ok", out)
        self.assertNotIn("warn:", out)

    def test_below_baseline_warns_to_tighten(self):
        self.write("a.md", "one\n")
        self.baseline(files=5, lines=50)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)
        self.assertIn("棘轮可下调", out)
        self.assertIn("--tighten", out)
        self.assertEqual(out.count("warn:"), 2, out)

    def test_equal_metric_and_lower_metric_are_independent(self):
        self.write("a.md", "one\ntwo\n")
        self.baseline(files=1, lines=99)  # files 相等、lines 更低
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count("warn:"), 1, out)
        self.assertIn("行数", out)


class OverBudgetTest(FixtureCase):
    def test_extra_file_fails(self):
        self.write("a.md", "one\n")
        self.write("b.md", "two\n")
        self.baseline(files=1, lines=10)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("篇数", out)
        self.assertIn("棘轮只降不升", out)

    def test_extra_lines_fail(self):
        self.write("a.md", "\n".join("line" for _ in range(10)) + "\n")
        self.baseline(files=1, lines=9)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("行数", out)
        self.assertIn("+1", out)

    def test_json_reports_both_sides(self):
        self.write("a.md", "one\n")
        self.baseline(files=1, lines=1)
        code, payload = self.report()
        self.assertEqual(code, 0, payload)
        self.assertEqual(payload["counts"]["baseline.files"], 1)
        self.assertEqual(payload["counts"]["measured.lines"], 1)
        self.assertEqual(payload["counts"]["slack.files"], 0)
        self.assertTrue(payload["ok"])


class ScopeTest(FixtureCase):
    """口径：谁被数、谁不被数，必须由本门的扫描器而不是调用点决定。"""

    def test_gitignored_docs_are_not_counted(self):
        """本地物料（工件/语料/录要）不进仓库文档面：CI 里它们不存在。"""
        self.write(".gitignore", "artifacts/\nnode_modules/\n*.draft.md\n", stage=False)
        self.write("a.md", "one\n")
        self.write("artifacts/report.md", "local\n" * 5, stage=False)
        self.write("node_modules/pkg/README.md", "vendor\n" * 5, stage=False)
        self.write("notes.draft.md", "draft\n" * 5, stage=False)
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_uncommitted_but_unignored_doc_counts(self):
        """棘轮不能靠「还没 commit」绕过：新文档一落盘就进预算。"""
        self.write("a.md", "one\n")
        self.write("fresh.md", "new\n", stage=False)
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("篇数", out)

    def test_generated_api_docs_are_not_counted(self):
        """docs/api 由 gen_godoc.sh 生成，体量归生成器；进预算等于用包数压作者。"""
        self.write("a.md", "one\n")
        self.write("docs/api/github_com__x__pkg.md", "generated\n" * 50)
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_change_planning_docs_are_not_counted(self):
        """变更计划与归档件是过程记录，由 check-openspec.sh 把关，不进长期文档预算。"""
        self.write("a.md", "one\n")
        self.write("openspec/changes/live/proposal.md", "plan\n" * 50)
        self.write("openspec/changes/live/tasks.md", "tasks\n" * 50)
        self.write("openspec/changes/archive/2026-01-01-old/tasks.md", "archive\n" * 50)
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_main_specs_stay_in_the_budget(self):
        """过程工件退出预算不等于主规格退出：openspec/specs 是权威规格，仍要计入。"""
        self.write("openspec/specs/x/spec.md", "spec\n")
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)
        self.write("openspec/specs/x/extra.md", "more\n")
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("篇数", out)

    def test_non_markdown_files_are_not_counted(self):
        self.write("a.md", "one\n")
        self.write("b.txt", "not a doc\n" * 5)
        self.write("c.go", "package c\n")
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_markdown_suffix_is_case_insensitive(self):
        self.write("A.MD", "one\n")
        self.write("b.markdown", "two\n")
        self.baseline(files=2, lines=2)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_unterminated_last_line_counts(self):
        self.write("a.md", "first\nsecond")  # wc -l 会漏掉 second
        self.baseline(files=1, lines=2)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)

    def test_empty_file_counts_zero_lines(self):
        self.write("a.md", "")
        self.baseline(files=1, lines=0)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)


class GuardsTest(FixtureCase):
    def test_empty_tree_is_not_green(self):
        self.baseline(files=0, lines=0)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("empty scan is not a green budget", out)

    def test_missing_baseline_is_fatal(self):
        self.write("a.md", "one\n")
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("baseline file missing", out)

    def test_garbled_baseline_is_fatal(self):
        self.write("a.md", "one\n")
        self.baseline(0, 0, raw="{not json")
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("not valid JSON", out)

    def test_non_integer_baseline_is_fatal(self):
        self.write("a.md", "one\n")
        self.baseline(files="2", lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("non-negative integer", out)

    def test_wrong_schema_version_is_fatal(self):
        self.write("a.md", "one\n")
        self.baseline(files=1, lines=1, schema_version=99)
        code, out, _err = self.cli()
        self.assertEqual(code, 1, out)
        self.assertIn("schema_version", out)

    def test_missing_on_disk_doc_is_warned_not_silently_dropped(self):
        gone = self.write("gone.md", "one\n")
        gone.unlink()  # 索引还在，盘上没了
        self.write("kept.md", "one\n")
        self.baseline(files=1, lines=1)
        code, out, _err = self.cli()
        self.assertEqual(code, 0, out)
        self.assertIn("missing on disk", out)
        self.assertIn("gone.md", out)


class NonRepositoryTest(unittest.TestCase):
    def test_directory_without_git_is_fatal(self):
        """没有 git 就没有「仓库文档面」：宁可报错，不可退化成全盘扫描。"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.md").write_text("one\n", encoding="utf-8")
            baseline = root / BASELINE_NAME
            baseline.parent.mkdir(parents=True)
            baseline.write_text(json.dumps({"schema_version": 1,
                                            "baseline": {"files": 1, "lines": 1}}),
                                encoding="utf-8")
            proc = subprocess.run([sys.executable, str(CHECKER), "--repo-root", str(root)],
                                  capture_output=True, text=True)
        self.assertEqual(proc.returncode, 1, proc.stdout)
        self.assertIn("git ls-files failed", proc.stdout)


class TightenTest(FixtureCase):
    def test_tighten_lowers_and_is_idempotent(self):
        self.write("a.md", "one\n")
        self.baseline(files=3, lines=30)
        code, out, _err = self.cli("--tighten")
        self.assertEqual(code, 0, out)
        self.assertIn("baseline.files lowered: 3 -> 1", out)
        self.assertIn("baseline.lines lowered: 30 -> 1", out)
        stored = json.loads((self.root / BASELINE_NAME).read_text(encoding="utf-8"))
        self.assertEqual(stored["baseline"], {"files": 1, "lines": 1})
        self.assertIn("measured_by", stored)

        code, out, _err = self.cli("--tighten")
        self.assertEqual(code, 0, out)
        self.assertIn("nothing written", out)

    def test_tighten_records_the_commit_it_measured(self):
        self.write("a.md", "one\n")
        self.baseline(files=2, lines=2)
        self.git("-c", "user.email=t@example.com", "-c", "user.name=t",
                 "commit", "-q", "-m", "fixture")
        code, out, _err = self.cli("--tighten")
        self.assertEqual(code, 0, out)
        stored = json.loads((self.root / BASELINE_NAME).read_text(encoding="utf-8"))
        self.assertRegex(stored["measured_on"], r"^[0-9a-f]{7,}(\+dirty)?$")

    def test_tighten_refuses_to_raise_and_leaves_the_file_alone(self):
        self.write("a.md", "one\n")
        self.write("b.md", "two\n")
        path = self.baseline(files=1, lines=1)
        before = path.read_bytes()
        code, out, _err = self.cli("--tighten")
        self.assertEqual(code, 1, out)
        self.assertIn("拒绝上调基线", out)
        self.assertEqual(path.read_bytes(), before)

    def test_tighten_refuses_when_one_metric_rises(self):
        """篇数降了但行数涨了：仍属上调（内容变多），必须拒绝写盘。"""
        self.write("a.md", "\n".join("line" for _ in range(5)) + "\n")
        path = self.baseline(files=4, lines=3)
        before = path.read_bytes()
        code, out, _err = self.cli("--tighten")
        self.assertEqual(code, 1, out)
        self.assertIn("行数", out)
        self.assertEqual(path.read_bytes(), before)


class RepoBaselineTest(unittest.TestCase):
    """基线=实测：仓库里的 doc_budget.json 必须是现树量出来的，不是抄来的。"""

    def test_baseline_matches_live_tree(self):
        payload, stored = budget.load_baseline(SCRIPTS / "doc_budget.json")
        _rows, totals, _unreadable = budget.scan(str(REPO_ROOT))
        self.assertEqual(
            stored, {metric: totals[metric] for metric in budget.METRICS},
            "baseline 与现树不符：现树 {}。若刚压缩过文档，跑 "
            "`python3 scripts/check_doc_budget.py --tighten` 锁住减少量；"
            "若刚新增文档，先压掉等量体量（基线只降不升）".format(totals))

    def test_baseline_records_its_measurement(self):
        payload, _stored = budget.load_baseline(SCRIPTS / "doc_budget.json")
        self.assertEqual(payload["schema_version"], 1)
        self.assertIn("scope", payload)
        self.assertIn("--json", payload["measured_by"])
        self.assertRegex(payload["measured_at"], r"^\d{4}-\d{2}-\d{2}$")


class LintRegistrationTest(unittest.TestCase):
    """门不许被静默摘掉：接入位置与 comment policy 同段，且不提供放宽开关。"""

    def setUp(self):
        self.text = (REPO_ROOT / "scripts" / "lint.sh").read_text(encoding="utf-8")

    def test_gate_is_wired_next_to_comment_policy(self):
        policy_at = self.text.index("lint: comment policy")
        budget_at = self.text.index("check_doc_budget.py")
        self.assertGreater(budget_at, policy_at)

    def test_gate_runs_under_the_policy_only_selector(self):
        lines = self.text.splitlines()
        gate_line = next(i for i, line in enumerate(lines) if "check_doc_budget.py" in line)
        guard = next(line for line in reversed(lines[:gate_line])
                     if 'if [ -z "$only" ]' in line)
        self.assertIn('= "policy"', guard)

    def test_no_relaxation_flag_is_passed(self):
        for forbidden in ("--allow-over", "--ignore", "--no-budget", "--raise-baseline"):
            self.assertNotIn(forbidden, self.text)


if __name__ == "__main__":
    unittest.main(verbosity=2)
