#!/usr/bin/env python3
"""Selftests for check_concept_budget.py — tempfile fixtures only, no product writes.

覆盖四条规则的负向锚（构造超标件必须被点名）、合规件零发现、baseline 豁免与棘轮
（只降不升）、stale 清零提示、--rule 单跑、--self-check 自校、JSON 报告形状。
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
CHECKER = SCRIPTS / "check_concept_budget.py"
REPO_ROOT = SCRIPTS.parents[1]
sys.path.insert(0, str(SCRIPTS))

import check_concept_budget as budget  # noqa: E402  (needs scripts/gates/ on sys.path)
import runtime_checklib as lib  # noqa: E402

ARCH = {
    "schema_version": 1,
    "scope": {"exclude_dir_names": ["examples"]},
    "packages": [{"pattern": "^.*$"}],
}

GOOD_FACE = """package good

import "context"

// Face 红线清单（2 条）：
// MUST 单次调用内保持 id 稳定
// MUST NOT 持有调用方指针
type Face interface {
	Read(ctx context.Context, id string) (string, error)
	Close() error
}

type oneImpl struct{}

func (oneImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (oneImpl) Close() error                                        { return nil }

type twoImpl struct{}

func (*twoImpl) Read(ctx context.Context, id string) (string, error) { return "", nil }
func (*twoImpl) Close() error                                        { return nil }

type SmallConfig struct {
	A int
	B int
	C int
	D int
	E int
}
"""

GOOD_EXAMPLE = """package good

// ExampleFace 是 canonical 范例。
func ExampleFace() {}
"""


def run_checker(*args):
    proc = subprocess.run(
        [sys.executable, str(CHECKER)] + list(args), capture_output=True, text=True
    )
    return proc.returncode, proc.stdout, proc.stderr


class FixtureCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.write(".concept-arch.json", json.dumps(ARCH))

    def tearDown(self):
        self._tmp.cleanup()

    def write(self, rel_path, text):
        path = self.root / rel_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")

    def findings(self, rules=budget.RULES):
        collected, _metrics = budget.collect_findings(
            str(self.root), str(self.root / ".concept-arch.json"), rules
        )
        return collected

    def cli(self, *args):
        return run_checker(
            "--repo-root", str(self.root),
            "--architecture", str(self.root / ".concept-arch.json"),
            "--json", *args
        )

    def write_good_tree(self):
        self.write("good/faces.go", GOOD_FACE)
        self.write("examples/contract/good/Face_example.go", GOOD_EXAMPLE)


class CompliantTreeTest(FixtureCase):
    def test_compliant_tree_has_no_findings(self):
        self.write_good_tree()
        self.assertEqual(self.findings(), [])

    def test_compliant_tree_exits_zero(self):
        self.write_good_tree()
        code, out, err = self.cli()
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["counts"]["findings"], 0)
        self.assertEqual(payload["counts"]["interfaces"], 1)
        self.assertEqual(payload["counts"]["exported_interfaces"], 1)

    def test_report_shape_is_machine_readable(self):
        self.write_good_tree()
        code, out, _err = self.cli()
        self.assertEqual(code, 0)
        payload = json.loads(out)
        self.assertIsNone(payload["productVerified"])
        self.assertEqual(payload["tool"], "check_concept_budget")
        self.assertIsInstance(payload["extra"]["findings"], list)
        for key in ("findings", "baseline_entries", "baseline_exempted", "baseline_stale"):
            self.assertIn(key, payload["counts"])


class InterfaceRuleTest(FixtureCase):
    def test_polymorphic_interface_over_two_methods_is_flagged(self):
        self.write("bad/faces.go", """package bad

type Wide interface {
	A()
	B()
	C()
}

type one struct{}

func (one) A() {}
func (one) B() {}
func (one) C() {}

type two struct{}

func (*two) A() {}
func (*two) B() {}
func (*two) C() {}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_IFACE]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:Wide"])
        self.assertEqual(findings[0]["value"], 3)
        self.assertIn("3 methods > 2", findings[0]["detail"])
        self.assertIn("polymorphic port", findings[0]["detail"])
        self.assertIn("2 implementer(s)", findings[0]["detail"])

    def test_consumer_side_interface_gets_three_method_budget(self):
        self.write("bad/faces.go", """package bad

type Three interface {
	A()
	B()
	C()
}

type Four interface {
	A()
	B()
	C()
	D()
}

type impl struct{}

func (impl) A() {}
func (impl) B() {}
func (impl) C() {}
func (impl) D() {}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_IFACE]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:Four"])
        self.assertIn("4 methods > 3", findings[0]["detail"])
        self.assertIn("consumer-side narrow port", findings[0]["detail"])

    def test_unexported_interface_is_still_measured(self):
        self.write("bad/faces.go", """package bad

type widePort interface {
	A()
	B()
	C()
	D()
}

type impl struct{}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_IFACE]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:widePort"])

    def test_multiline_signature_counts_as_method_not_many(self):
        self.write("bad/faces.go", """package bad

type Multi interface {
	Read(
		id string,
		limit int,
	) error
}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_IFACE]
        self.assertEqual(findings, [])


class StructRuleTest(FixtureCase):
    def test_six_exported_fields_is_flagged(self):
        self.write("bad/types.go", """package bad

type Big struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_STRUCT]
        self.assertEqual([f["key"] for f in findings], ["bad/types.go:Big"])
        self.assertEqual(findings[0]["value"], 6)
        self.assertIn("6 exported fields > 5", findings[0]["detail"])

    def test_embedded_and_unexported_fields_are_not_counted(self):
        self.write("bad/types.go", """package bad

type Base struct {
	Hidden int
}

type Five struct {
	Base
	hidden int
	A, B int
	C, D int
	E string `json:"e"`
}

type Six struct {
	Base
	hidden int
	A, B int
	C, D int
	E, F string
}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_STRUCT]
        # Five 的 5 个导出字段合规；Six 的 6 个导出字段超标——嵌入/未导出不计入。
        self.assertEqual([f["key"] for f in findings], ["bad/types.go:Six"])
        self.assertEqual(findings[0]["value"], 6)

    def test_unexported_struct_is_not_measured(self):
        self.write("bad/types.go", """package bad

type hidden struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_STRUCT]
        self.assertEqual(findings, [])


class CtxRuleTest(FixtureCase):
    def test_bare_ctx_value_is_counted_per_file(self):
        self.write("bad/seam.go", """package bad

import "context"

// 禁止这样跨缝取关系：ctx.Value 不是类型化 accessor。
func one(ctx context.Context) interface{} { return ctx.Value("a") }

func two(ctx context.Context) interface{} { return ctx.Value("b") }
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_CTX]
        self.assertEqual([f["key"] for f in findings], ["bad/seam.go:*"])
        self.assertEqual(findings[0]["value"], 2)

    def test_comment_mention_is_not_a_call(self):
        self.write("bad/seam.go", """package bad

// 本包从不调用 ctx.Value，关系全部走 accessor。
func safe() {}
""")
        self.assertEqual([f for f in self.findings() if f["rule"] == budget.RULE_CTX], [])


class ContractRuleTest(FixtureCase):
    def test_missing_example_is_flagged(self):
        self.write("bad/faces.go", """package bad

// 红线（1 条）：
// MUST 保持幂等
type NoExample interface {
	Do() error
}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_CONTRACT]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:NoExample"])
        self.assertIn("missing canonical example examples/contract/bad/NoExample_example.go",
                      findings[0]["detail"])

    def test_missing_redlines_is_flagged(self):
        self.write("bad/faces.go", """package bad

// NoRedline 只有范例，Godoc 里没有任何约束清单。
type NoRedline interface {
	Do() error
}
""")
        self.write("examples/contract/bad/NoRedline_example.go", """package bad

func ExampleNoRedline() {}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_CONTRACT]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:NoRedline"])
        self.assertIn("no 红线 marker", findings[0]["detail"])

    def test_too_many_redlines_is_flagged(self):
        lines = "\n".join("// MUST 规则{}".format(index) for index in range(6))
        lines += "\n// MUST NOT 越界"
        self.write("bad/faces.go", """package bad

// 红线清单：
{}
type TooMany interface {{
	Do() error
}}
""".format(lines))
        self.write("examples/contract/bad/TooMany_example.go", """package bad

func ExampleTooMany() {}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_CONTRACT]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:TooMany"])
        self.assertIn("redline lines > 6", findings[0]["detail"])

    def test_example_without_matching_function_is_flagged(self):
        self.write("bad/faces.go", """package bad

// 红线（2 条）：
// MUST 保持幂等
// MUST NOT 吞错误
type NoFunc interface {
	Do() error
}
""")
        self.write("examples/contract/bad/NoFunc_example.go", """package bad

func ExampleSomethingElse() {}
""")
        findings = [f for f in self.findings() if f["rule"] == budget.RULE_CONTRACT]
        self.assertEqual([f["key"] for f in findings], ["bad/faces.go:NoFunc"])
        self.assertIn("lacks func ExampleNoFunc()", findings[0]["detail"])


class BaselineRatchetTest(FixtureCase):
    def setUp(self):
        super().setUp()
        self.write("bad/types.go", """package bad

type Big struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}
""")
        self.findings_list = [f for f in self.findings() if f["rule"] == budget.RULE_STRUCT]

    def test_recorded_baseline_exempts_loudly(self):
        report = lib.Report("selftest")
        recorded = [(f["rule"], f["key"], f["value"]) for f in self.findings_list]
        budget.apply_baseline(report, self.findings_list, recorded)
        self.assertTrue(report.ok, report.errors)
        self.assertEqual(report.counts["baseline_exempted"], 1)
        self.assertTrue(any("baselined" in message for message in report.warnings))

    def test_growth_beyond_recorded_value_is_an_error(self):
        report = lib.Report("selftest")
        recorded = [("struct-fields", "bad/types.go:Big", 5)]
        budget.apply_baseline(report, self.findings_list, recorded)
        self.assertFalse(report.ok)
        self.assertTrue(any("ratchet" in message for message in report.errors))

    def test_missing_baseline_is_an_error(self):
        report = lib.Report("selftest")
        budget.apply_baseline(report, self.findings_list, [])
        self.assertFalse(report.ok)
        self.assertIn("not baselined", report.errors[0])

    def test_stale_baseline_warns(self):
        report = lib.Report("selftest")
        budget.apply_baseline(report, [], [("iface-methods", "nowhere.go:Ghost", 4)])
        self.assertTrue(report.ok)
        self.assertEqual(report.counts["baseline_stale"], 1)
        self.assertTrue(any("stale" in message for message in report.warnings))


class CliTest(FixtureCase):
    def test_rule_filter_runs_only_that_rule(self):
        self.write("bad/types.go", """package bad

import "context"

type Big struct {
	A1 int
	B1 int
	C1 int
	D1 int
	E1 int
	F1 int
}

func leak(ctx context.Context) interface{} { return ctx.Value("k") }
""")
        code, out, _err = self.cli("--rule", "S3")
        self.assertEqual(code, 1)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["rules_run"], 1)
        self.assertEqual(payload["counts"]["findings.ctx-bare"], 1)
        self.assertEqual(payload["counts"]["findings.struct-fields"], 0)
        self.assertTrue(any("ctx.Value" in message for message in payload["errors"]))

    def test_rule_name_alias_works(self):
        self.write("bad/types.go", "package bad\n\ntype Small struct{ A int }\n")
        code, out, _err = self.cli("--rule", "struct-fields")
        self.assertEqual(code, 0, out)

    def test_unknown_rule_is_rejected(self):
        self.write("bad/types.go", "package bad\n\ntype Small struct{ A int }\n")
        code, out, _err = self.cli("--rule", "S9")
        self.assertEqual(code, 1)
        self.assertIn("unknown rule", out)

    def test_empty_repo_is_not_green(self):
        self.write("README.md", "empty\n")
        code, out, _err = self.cli()
        self.assertEqual(code, 1)
        self.assertIn("implementation not present", out)


class WorkflowRegistrationTest(unittest.TestCase):
    """CI 注册不得被静默移除：概念门与架构门同链、自测在同一个 job 内跑。"""

    def test_workflow_registers_concept_gate_and_selftest(self):
        text = (REPO_ROOT / ".github" / "workflows" / "runtime-contracts.yml").read_text(
            encoding="utf-8")
        self.assertIn("- name: Concept budget (S1\u2013S4)", text)
        self.assertIn("python scripts/gates/check_concept_budget.py --json", text)
        self.assertIn("python scripts/gates/test_check_concept_budget.py -v", text)


class SelfCheckTest(unittest.TestCase):
    def test_self_check_passes_on_repo(self):
        code, out, err = run_checker("--self-check", "--repo-root", str(REPO_ROOT))
        self.assertEqual(code, 0, out + err)
        self.assertIn("selfcheck_rules: 4", out)
        self.assertIn("counts.selfcheck_fixture_cases: 8", out)

    def test_baseline_table_matches_repo(self):
        report = lib.Report("selftest")
        budget.check_baseline_table(report, str(REPO_ROOT))
        self.assertTrue(report.ok, report.errors)
        self.assertEqual(report.counts["baseline_entries"], len(budget.BASELINE))

    def test_self_check_warns_on_stale_baseline_file(self):
        original = budget.BASELINE
        budget.BASELINE = original + (("struct-fields", "no/such/file.go:Ghost", 6),)
        try:
            report = lib.Report("selftest")
            budget.check_baseline_table(report, str(REPO_ROOT))
        finally:
            budget.BASELINE = original
        self.assertTrue(report.ok, report.errors)
        self.assertTrue(any("missing file" in message for message in report.warnings))

    def test_self_check_catches_compliant_symbol_in_baseline(self):
        original = budget.BASELINE
        budget.BASELINE = original + (("struct-fields", "runtime/agent/delegate.go:Delegation", 5),)
        try:
            report = lib.Report("selftest")
            budget.check_baseline_table(report, str(REPO_ROOT))
        finally:
            budget.BASELINE = original
        self.assertFalse(report.ok)
        self.assertTrue(any("must be an int above the limit" in message
                            for message in report.errors))


if __name__ == "__main__":
    unittest.main(verbosity=2)
