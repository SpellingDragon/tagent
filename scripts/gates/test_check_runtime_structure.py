#!/usr/bin/env python3
"""Selftests for check_runtime_structure.py — tempfile repos only, no product writes."""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
CHECKER = SCRIPTS / "check_runtime_structure.py"


def run_checker(*args):
    proc = subprocess.run([sys.executable, str(CHECKER)] + list(args),
                          capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def write_lines(path, count, package="package fixture"):
    path.parent.mkdir(parents=True, exist_ok=True)
    body = [package]
    body.extend("// filler {}".format(i) for i in range(count - 1))
    path.write_text("\n".join(body) + "\n", encoding="utf-8")


class StructureTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.repo = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def check(self, *args):
        return run_checker("--repo-root", str(self.repo), "--json", *args)

    def test_small_tree_passes(self):
        write_lines(self.repo / "tagent" / "runtime.go", 120)
        write_lines(self.repo / "state" / "state_test.go", 300)
        code, out, err = self.check()
        self.assertEqual(code, 0, out + err)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["product_files"], 1)
        self.assertEqual(payload["counts"]["test_files"], 1)
        self.assertEqual(payload["counts"]["violations"], 0)
        self.assertIsNone(payload["productVerified"])
        self.assertEqual(len(payload["extra"]["files"]), 2)

    def test_product_over_limit_reported(self):
        write_lines(self.repo / "agent" / "big.go", 501)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("agent/big.go: 501 lines > 500", out)

    def test_test_file_uses_test_limit(self):
        write_lines(self.repo / "task" / "x_test.go", 501)
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        write_lines(self.repo / "task" / "y_test.go", 1001)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("task/y_test.go: 1001 lines > 1000 (limit 1000 for test)", out)

    def test_nested_test_any_position(self):
        write_lines(self.repo / "tests" / "integration" / "deep" / "z_test.go", 1001)
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("tests/integration/deep/z_test.go", out)

    def test_historical_dirs_are_excluded(self):
        write_lines(self.repo / "prototype" / "legacy.go", 9000)
        write_lines(self.repo / "scripts" / "codetools" / "tool.go", 9000)
        write_lines(self.repo / "tagent" / "runtime.go", 100)
        code, out, _ = self.check()
        self.assertEqual(code, 0, out)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["go_files"], 1)

    def test_new_directory_is_still_scanned(self):
        write_lines(self.repo / "evolution" / "rl" / "adapt" / "deep.go", 40)
        code, out, _ = self.check("--product-max-lines", "10")
        self.assertNotEqual(code, 0)
        self.assertIn("evolution/rl/adapt/deep.go", out)

    def test_no_go_files_is_not_green(self):
        (self.repo / "README.md").write_text("empty\n", encoding="utf-8")
        code, out, _ = self.check()
        self.assertNotEqual(code, 0)
        self.assertIn("implementation not present", out)

    def test_missing_repo_root_is_parameter_error(self):
        code, out, err = run_checker("--repo-root", str(self.repo / "gone"), "--json")
        self.assertEqual(code, 2)
        self.assertIn("repo root not found", err)

    def test_zero_limit_is_parameter_error(self):
        write_lines(self.repo / "tagent" / "runtime.go", 5)
        code, _, err = self.check("--product-max-lines", "0")
        self.assertEqual(code, 2)
        self.assertIn("positive", err)

    def test_extra_exclude_is_honoured(self):
        write_lines(self.repo / "vendorish" / "big.go", 900)
        write_lines(self.repo / "tagent" / "runtime.go", 100)
        code, out, _ = self.check("--exclude", "vendorish")
        self.assertEqual(code, 0, out)
        payload = json.loads(out)
        self.assertEqual(payload["counts"]["go_files"], 1)


if __name__ == "__main__":
    unittest.main()
