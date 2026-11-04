#!/usr/bin/env python3
"""Selftests for check_product_surface.py — tempfile fixtures only, no product writes."""
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_product_surface as cps  # noqa: E402

BASE = {"schema_version": 1, "allowed_sleep_files": ["allowed/sleep.go"],
        "test_only_exported_funcs": []}


def write(root, rel, text):
    p = os.path.join(root, rel)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, "w", encoding="utf-8") as h:
        h.write(text)


class ProductSurfaceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        write(self.root, "allowed/sleep.go", "package allowed\nimport \"time\"\nfunc Poll() { time.Sleep(1) }\n")
        write(self.root, "product/clean.go", "package product\nfunc Real() {}\nfunc Used() {}\n")
        write(self.root, "product/use.go", "package product\nfunc x() { Used() }\n")

    def tearDown(self):
        self.tmp.cleanup()

    def test_clean_tree_is_green(self):
        r = cps.check_tree(self.root, BASE)
        self.assertEqual(r["errors"], [], r)

    def test_negative_control_diag_marker_in_product_turns_red(self):
        write(self.root, "product/diag.go",
              "package product\nimport \"time\"\nfunc Probe() { time.Sleep(300) // TEMP-DIAG widen the race\n}\n")
        r = cps.check_tree(self.root, BASE)
        self.assertTrue(any("TEMP-DIAG" in e for e in r["errors"]), r)

    def test_negative_control_new_product_sleep_turns_red(self):
        write(self.root, "product/slow.go",
              "package product\nimport \"time\"\nfunc Wait() { time.Sleep(5) }\n")
        r = cps.check_tree(self.root, BASE)
        self.assertTrue(any("not in allowed_sleep_files" in e for e in r["errors"]), r)

    def test_allowed_sleep_file_is_not_flagged(self):
        r = cps.check_tree(self.root, BASE)
        self.assertFalse(any("allowed/sleep.go" in e for e in r["errors"]), r)

    def test_negative_control_new_test_only_hook_turns_red_then_green_with_baseline(self):
        write(self.root, "product/hook.go", "package product\nfunc SwapForTests() {}\n")
        write(self.root, "product/hook_test.go", "package product\nfunc t() { SwapForTests() }\n")
        r = cps.check_tree(self.root, BASE)
        self.assertTrue(any("SwapForTests" in e and "public API" in e for e in r["errors"]), r)
        based = dict(BASE, test_only_exported_funcs=["SwapForTests"])
        self.assertEqual(cps.check_tree(self.root, based)["errors"], [])

    def test_stale_baseline_entry_warns(self):
        r = cps.check_tree(self.root, dict(BASE, test_only_exported_funcs=["GoneHook"]))
        self.assertTrue(any("stale entry" in w for w in r["warnings"]), r)

    def test_exported_func_with_product_caller_is_not_a_hook(self):
        write(self.root, "product/used.go", "package product\nfunc PublicOption() {}\n")
        write(self.root, "product/caller.go", "package product\nfunc y() { PublicOption() }\n")
        r = cps.check_tree(self.root, BASE)
        self.assertFalse(any("PublicOption" in e for e in r["errors"]), r)


if __name__ == "__main__":
    unittest.main()
