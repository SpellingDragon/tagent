#!/usr/bin/env python3
"""Selftests for check_concept_budget.py — tempfile fixtures only, no product writes.

覆盖四条规则的负向锚（构造超标件必须被点名）、合规件零发现、baseline 豁免与棘轮
（只降不升）、stale 清零提示、--rule 单跑、--self-check 自校、JSON 报告形状。
"""
import json
import os
import re
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
    # domain-surface 判定的夹具政策：基线用本地树快照（真实树用 git commit）。
    "concept_bindings": {"baseline": {"tree": ".baseline-tree"}, "bindings": {}},
}

# 非 superseded 主 spec 夹具：拥有 SHALL 是正常形态，只有 superseded+SHALL 才红。
LIVE_SPEC = """# {capability} Specification

## Purpose

该能力拥有自己的领域事实与读面，是活的规范权威。

## Requirements

### Requirement: 事实可恢复

领域事实 SHALL 可由事件链恢复，读投影 SHALL 只从既有事实计算。

#### Scenario: 重放恢复

- **WHEN** 重放事件链
- **THEN** 恢复领域事实
"""

# 负锚 B 夹具：镜像真实 persistent-event-loop 的形状——blockquote superseded 标记
# ＋「保留为历史记录」注释＋Requirements 里仍有 SHALL。
SUPERSEDED_SPEC = """# {capability} Specification

## Purpose

> **superseded（2026-11-02 核验）**：本规范描述的常驻循环已退役。以下 Purpose 与 Requirements 保留为历史记录；现行契约见现行能力的主 spec。
## Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行 drain mailbox，并 SHALL NOT 在 Flow.Run 返回时退出。

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** Loop goroutine 退出
"""

SUPERSEDED_EMPTY_SPEC = """# {capability} Specification

## Purpose

> **superseded（2026-11-02 核验）**：有效条款已全部迁移到现行能力；本文件不再保留任何现行命令。

## Requirements

### Requirement: 历史条款已迁移

历史条款的现行版本见现行能力的规范。

#### Scenario: 查现行要求

- **WHEN** 需要现行要求
- **THEN** 读现行能力的规范
"""

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
        # domain-surface 的基线树：默认空目录（缺基线＝fail-closed），
        # 需要合观证据的夹具用 write_baseline 镜像内容。
        (self.root / ".baseline-tree").mkdir()

    def tearDown(self):
        self._tmp.cleanup()

    def write(self, rel_path, text):
        path = self.root / rel_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")

    def write_baseline(self, rel_path, text):
        self.write(os.path.join(".baseline-tree", rel_path), text)

    def write_spec(self, capability, text):
        self.write(os.path.join("openspec", "specs", capability, "spec.md"), text)

    def rewrite_bindings(self, bindings, baseline=None):
        config = dict(ARCH)
        config["concept_bindings"] = {
            "baseline": baseline if baseline is not None else {"tree": ".baseline-tree"},
            "bindings": bindings,
        }
        self.write(".concept-arch.json", json.dumps(config))

    def domain(self, *args):
        return self.cli("--rule", "domain-surface", *args)

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
        # domain-surface 合规件：基线镜像（当前面==基线面，无新增）＋活的 spec 锚。
        self.write_baseline("good/faces.go", GOOD_FACE)
        self.write_spec("delivery-facts", LIVE_SPEC.format(capability="delivery-facts"))


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


class DomainSurfaceTest(FixtureCase):
    """D1 domain-surface：新增/改名公共命名类型的绑定门＋superseded 规范权威门。

    判定与 S1–S4 分门：domain 结果独立成节，不进形状 BASELINE 棘轮，也不能被其豁免。
    """

    def setUp(self):
        super().setUp()
        # 公共夹具：一个两侧都在的导出类型（无新增面）＋一个活的 spec 锚。
        stable = "package stable\n\ntype Anchor struct{ A int }\n"
        self.write("stable/types.go", stable)
        self.write_baseline("stable/types.go", stable)
        self.write_spec("delivery-facts", LIVE_SPEC.format(capability="delivery-facts"))

    # ---- 负锚 A：机制无绑定具名拒 ----

    def test_unbound_new_mechanism_types_are_named_rejected(self):
        self.write("runtime/agent/consumer.go",
                   "package agent\n\ntype Writers struct{ Next int }\n")
        self.write("runtime/task/movement.go",
                   "package task\n\ntype Movement struct{ Seq int }\n")
        self.write("tool/subagent/continuation.go",
                   "package subagent\n\ntype Continuations struct{ Key string }\n")
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        keys = [f["key"] for f in payload["extra"]["domain_surface"]]
        self.assertEqual(keys, [
            "runtime/agent/consumer.go:Writers",
            "runtime/task/movement.go:Movement",
            "tool/subagent/continuation.go:Continuations",
        ])
        for message in payload["errors"]:
            if "Writers" in message:
                self.assertIn("has no concept binding", message)
        self.assertTrue(any("no concept binding" in message for message in payload["errors"]))
        self.assertEqual(payload["counts"]["findings.domain-surface"], 3)

    def test_invalid_binding_kind_is_rejected(self):
        self.write("runtime/agent/consumer.go",
                   "package agent\n\ntype Writers struct{ Next int }\n")
        self.rewrite_bindings({
            "Writers": {"concept": "writer-ledger", "kind": "mechanism",
                        "spec": "delivery-facts"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("kind must be one of fact/read/declaration/port" in message
                            for message in payload["errors"]))

    # ---- 负锚 B：superseded spec 含 SHALL/MUST 必红 ----

    def test_superseded_spec_with_shall_is_red(self):
        self.write_spec("persistent-event-loop",
                        SUPERSEDED_SPEC.format(capability="persistent-event-loop"))
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("persistent-event-loop" in message
                            and "superseded" in message
                            for message in payload["errors"]))
        self.assertEqual(payload["counts"]["domain_surface.superseded_specs_with_requirements"], 1)

    def test_superseded_spec_without_normative_requirements_is_green(self):
        self.write_spec("persistent-event-loop",
                        SUPERSEDED_EMPTY_SPEC.format(capability="persistent-event-loop"))
        code, out, _err = self.domain()
        self.assertEqual(code, 0, out)

    def test_live_spec_with_shall_is_green(self):
        # 非 superseded 的主 spec 拥有 SHALL 是正常形态，不得到处红。
        code, out, _err = self.domain()
        self.assertEqual(code, 0, out)

    # ---- 正锚：合法绑定不误伤 ----

    def test_new_fact_binding_is_accepted(self):
        # CompressProduct：request→product→恢复所需事实，绑 fact。
        self.write("api/event/compress_product.go",
                   "package event\n\n// CompressProduct 记录压缩产物与恢复所需事实。\n"
                   "type CompressProduct struct{ Product string }\n")
        self.write_spec("compression-pipeline",
                        LIVE_SPEC.format(capability="compression-pipeline"))
        self.rewrite_bindings({
            "CompressProduct": {"concept": "compress-product", "kind": "fact",
                                "spec": "compression-pipeline"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(payload["counts"]["domain_surface.new"], 1)
        self.assertEqual(payload["counts"]["domain_surface.net_facts"], 1)
        self.assertEqual(payload["extra"]["domain_surface"], [])

    def test_new_read_binding_is_accepted_and_adds_no_fact(self):
        # DeliveryLedger：投递事实读投影，绑 read，不计新领域事实。
        self.write("assembly/host_tools.go",
                   "package assembly\n\ntype DeliveryLedger struct{ Rows int }\n")
        self.write_spec("unified-task-semantics",
                        LIVE_SPEC.format(capability="unified-task-semantics"))
        self.rewrite_bindings({
            "DeliveryLedger": {"concept": "delivery-record", "kind": "read",
                               "spec": "unified-task-semantics"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(payload["counts"]["domain_surface.new"], 1)
        self.assertEqual(payload["counts"]["domain_surface.net_facts"], 0)

    def test_same_name_relocation_is_net_zero(self):
        # Reconciler：同名搬迁（runtime/agent → runtime/task），语义权威不变。
        self.write_baseline("runtime/agent/consumer.go",
                            "package agent\n\ntype Reconciler interface{ Apply() error }\n")
        self.write("runtime/task/reconcile.go",
                   "package task\n\ntype Reconciler interface{ Apply() error }\n")
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(payload["counts"]["domain_surface.new"], 0)
        self.assertEqual(payload["counts"]["domain_surface.relocations"], 1)

    def test_pure_rename_accounts_net_zero(self):
        # 纯改名：OldLedger → DeliveryLedger，renamed_from 对账，净额 0。
        self.write_baseline("memory/ledger.go",
                            "package memory\n\ntype OldLedger struct{ Rows int }\n")
        self.write("memory/ledger.go",
                   "package memory\n\ntype DeliveryLedger struct{ Rows int }\n")
        self.write_spec("unified-task-semantics",
                        LIVE_SPEC.format(capability="unified-task-semantics"))
        self.rewrite_bindings({
            "DeliveryLedger": {"concept": "delivery-record", "kind": "read",
                               "spec": "unified-task-semantics",
                               "renamed_from": "OldLedger"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(payload["counts"]["domain_surface.renames"], 1)
        self.assertEqual(payload["counts"]["domain_surface.removed"], 1)
        self.assertEqual(payload["counts"]["domain_surface.net_facts"], 0)

    def test_rename_with_old_name_still_present_is_red(self):
        self.write_baseline("memory/ledger.go",
                            "package memory\n\ntype OldLedger struct{ Rows int }\n")
        self.write("memory/ledger.go",
                   "package memory\n\ntype OldLedger struct{ Rows int }\n"
                   "type DeliveryLedger struct{ Rows int }\n")
        self.write_spec("unified-task-semantics",
                        LIVE_SPEC.format(capability="unified-task-semantics"))
        self.rewrite_bindings({
            "DeliveryLedger": {"concept": "delivery-record", "kind": "read",
                               "spec": "unified-task-semantics",
                               "renamed_from": "OldLedger"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("still present" in message for message in payload["errors"]))

    def test_rename_from_name_absent_in_baseline_is_red(self):
        self.write("memory/ledger.go",
                   "package memory\n\ntype DeliveryLedger struct{ Rows int }\n")
        self.write_spec("unified-task-semantics",
                        LIVE_SPEC.format(capability="unified-task-semantics"))
        self.rewrite_bindings({
            "DeliveryLedger": {"concept": "delivery-record", "kind": "read",
                               "spec": "unified-task-semantics",
                               "renamed_from": "GhostLedger"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("absent from the baseline" in message
                            for message in payload["errors"]))

    # ---- fail-closed：基线/锚/表缺失不得静默通过 ----

    def test_missing_concept_bindings_section_fails_closed(self):
        config = dict(ARCH)
        del config["concept_bindings"]
        self.write(".concept-arch.json", json.dumps(config))
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("concept_bindings" in message for message in payload["errors"]))

    def test_missing_baseline_config_fails_closed(self):
        self.rewrite_bindings({}, baseline={})
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("concept_bindings.baseline" in message
                            for message in payload["errors"]))

    def test_empty_baseline_tree_fails_closed(self):
        self.rewrite_bindings({}, baseline={"tree": ".baseline-empty"})
        (self.root / ".baseline-empty").mkdir()
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("implementation not present" in message
                            for message in payload["errors"]))

    def test_unresolvable_baseline_commit_fails_closed(self):
        self.rewrite_bindings({}, baseline={"commit": "0" * 40})
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("cannot read baseline commit" in message
                            for message in payload["errors"]))

    def test_binding_to_missing_spec_anchor_fails_closed(self):
        self.write("api/event/compress_product.go",
                   "package event\n\ntype CompressProduct struct{ Product string }\n")
        self.rewrite_bindings({
            "CompressProduct": {"concept": "compress-product", "kind": "fact",
                                "spec": "no-such-capability"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("no-such-capability" in message
                            and "spec anchor" in message
                            for message in payload["errors"]))

    def test_binding_to_superseded_spec_anchor_is_red(self):
        self.write("api/event/compress_product.go",
                   "package event\n\ntype CompressProduct struct{ Product string }\n")
        self.write_spec("persistent-event-loop",
                        SUPERSEDED_EMPTY_SPEC.format(capability="persistent-event-loop"))
        self.rewrite_bindings({
            "CompressProduct": {"concept": "compress-product", "kind": "fact",
                                "spec": "persistent-event-loop"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        self.assertTrue(any("persistent-event-loop" in message
                            and "superseded" in message
                            for message in payload["errors"]))

    # ---- 表卫生与口径 ----

    def test_unexported_new_type_is_not_gated(self):
        self.write("runtime/agent/consumer.go",
                   "package agent\n\ntype writers struct{ next int }\n")
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(payload["counts"]["domain_surface.new"], 0)

    def test_stale_binding_for_unchanged_name_warns_but_passes(self):
        self.rewrite_bindings({
            "Anchor": {"concept": "anchor", "kind": "declaration",
                       "spec": "delivery-facts"},
        })
        code, out, _err = self.domain()
        payload = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertTrue(any("not required" in message for message in payload["warnings"]))

    def test_domain_findings_are_reported_separately_from_shapes(self):
        self.write("runtime/agent/consumer.go",
                   "package agent\n\ntype Writers struct{ Next int }\n")
        code, out, _err = self.cli()
        payload = json.loads(out)
        self.assertEqual(code, 1, out)
        # 形状面与领域面分门：domain 结果独立成节，形状 findings 不含 domain。
        self.assertEqual(payload["counts"]["findings"], 0)
        self.assertEqual(payload["counts"]["findings.domain-surface"], 1)
        self.assertEqual(payload["extra"]["domain_surface"][0]["rule"], "domain-surface")
        self.assertTrue(all(f["rule"] != "domain-surface"
                            for f in payload["extra"]["findings"]))


class RealTreeDomainSurfaceTest(unittest.TestCase):
    """真实树锚：persistent-event-loop 的 superseded+SHALL 债必须被具名检出。"""

    @staticmethod
    def _persistent_event_loop_debt():
        text = (REPO_ROOT / "openspec" / "specs" / "persistent-event-loop" / "spec.md") \
            .read_text(encoding="utf-8")
        lines = text.splitlines()
        superseded = any(
            line.lstrip().startswith(">") and re.search(r"superseded", line, re.I)
            for line in lines
        )
        requirements, started = [], False
        for line in lines:
            if line.startswith("## Requirements"):
                started = True
                continue
            if started and re.match(r"^##\s", line):
                break
            if started:
                requirements.append(line)
        block = "\n".join(requirements) if started else text
        normative = bool(re.search(r"\b(SHALL|MUST)\b", block))
        return superseded and normative

    def test_real_tree_superseded_debt_is_detected(self):
        code, out, _err = run_checker(
            "--repo-root", str(REPO_ROOT), "--rule", "domain-surface", "--json")
        payload = json.loads(out)
        named = any("persistent-event-loop" in message for message in payload["errors"])
        self.assertEqual(self._persistent_event_loop_debt(), named,
                         "superseded+SHALL 债与门的红必须一一对应: " + out)
        if named:
            self.assertEqual(code, 1)

    def test_real_tree_has_no_unbound_new_public_types(self):
        _code, out, _err = run_checker(
            "--repo-root", str(REPO_ROOT), "--rule", "domain-surface", "--json")
        payload = json.loads(out)
        self.assertFalse([message for message in payload["errors"]
                          if "has no concept binding" in message],
                         "本波写集不得新增未绑定公共命名类型: " + out)


class ConceptRatchetTest(unittest.TestCase):
    """concept_ratchet 三档 floor 的变异红→绿自测，连同缺键与错误的告警语义。

    三档 floor 分别冻结 removed_min、new_max、exported_interfaces_max：把任一档
    收紧到当前实测值之外必须具名变红，恢复 golden 的冻结值必须回到绿；红时不得
    留下 "held" 的安心理由。
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.path = Path(self._tmp.name) / "architecture.json"
        self.metrics = {"exported_interfaces": 70}
        self.domain_counts = {
            "domain_surface.removed": 12,
            "domain_surface.new": 0,
        }

    def write_ratchet(self, **overrides):
        ratchet = {
            "removed_min": 10,
            "new_max": 1,
            "exported_interfaces_max": 77,
        }
        ratchet.update(overrides)
        self.path.write_text(json.dumps({"concept_ratchet": ratchet}), encoding="utf-8")

    def check(self):
        report = lib.Report("selftest-ratchet")
        budget.check_ratchet(
            report, str(self.path), dict(self.metrics), dict(self.domain_counts)
        )
        return report

    def assert_red_then_green(self, error_fragment):
        mutated = self.check()
        self.assertFalse(mutated.ok)
        self.assertTrue(
            any(error_fragment in message for message in mutated.errors),
            mutated.errors,
        )
        self.assertFalse(
            any("held" in message for message in mutated.notes),
            "a failed ratchet must not print the held note: " + str(mutated.notes),
        )

        self.write_ratchet()
        restored = self.check()
        self.assertTrue(restored.ok, restored.errors)
        self.assertTrue(any("concept ratchet held" in message for message in restored.notes))

    def test_removed_floor_mutation_turns_red_then_green(self):
        # removed 实测 12，把 floor 提到 13 即低于 floor 变红。
        self.write_ratchet(removed_min=13)
        self.assert_red_then_green("below the frozen floor")

    def test_new_ceiling_mutation_turns_red_then_green(self):
        # new 实测 0，把 ceiling 压到 -1 即超过 ceiling 变红。
        self.write_ratchet(new_max=-1)
        self.assert_red_then_green("exceeds the frozen ceiling")

    def test_interface_ceiling_mutation_turns_red_then_green(self):
        # exported_interfaces 实测 70，把 ceiling 压到 69 即超过 ceiling 变红。
        self.write_ratchet(exported_interfaces_max=69)
        self.assert_red_then_green("exported_interfaces")

    def test_missing_ratchet_key_warns_instead_of_passing_silently(self):
        self.path.write_text(json.dumps({"schema_version": 1}), encoding="utf-8")
        report = self.check()
        self.assertTrue(report.ok, report.errors)
        self.assertTrue(
            any("concept_ratchet" in message for message in report.warnings),
            report.warnings,
        )
        self.assertFalse(any("held" in message for message in report.notes))

    def test_existing_error_suppresses_the_held_note(self):
        self.write_ratchet()
        report = lib.Report("selftest-ratchet")
        report.error("another rule failed")
        budget.check_ratchet(
            report, str(self.path), dict(self.metrics), dict(self.domain_counts)
        )
        self.assertFalse(report.ok)
        self.assertFalse(
            any("held" in message for message in report.notes),
            "the held note must not accompany a failed report: " + str(report.notes),
        )


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
