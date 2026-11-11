#!/usr/bin/env python3
"""Selftests for check_doc_links.py — tempfile fixtures only, no product writes."""
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_doc_links as gate  # noqa: E402


def write(root, rel, text):
    p = os.path.join(root, rel)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, "w", encoding="utf-8") as h:
        h.write(text)


class DocLinksTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        write(self.root, "docs/wiki/README.md", "# Index\n<a id=\"current\"></a>\n## 活契约页\n")
        write(self.root, "docs/wiki/sub/page.md",
              "# Page\n[up](../README.md#current) and [self](../README.md#current) and [ext](https://example.com).")

    def tearDown(self):
        self.tmp.cleanup()

    def test_clean_tree_is_green(self):
        r = gate.check(self.root)
        self.assertEqual(r["errors"], [], r)

    def test_negative_control_missing_file_turns_red(self):
        write(self.root, "docs/wiki/sub/bad.md", "[gone](missing.md)\n")
        r = gate.check(self.root)
        self.assertTrue(any("dangling link" in e for e in r["errors"]), r)

    def test_negative_control_deleted_anchor_turns_red(self):
        write(self.root, "docs/wiki/sub/bad.md", "[dead](../README.md#deleted-anchor)\n")
        r = gate.check(self.root)
        self.assertTrue(any("unresolved anchor" in e for e in r["errors"]), r)

    def test_negative_control_wrong_relative_depth_turns_red(self):
        # ../../README.md from docs/wiki/sub/ resolves to docs/README.md (absent).
        write(self.root, "docs/wiki/sub/bad.md", "[up](../../README.md#current)\n")
        r = gate.check(self.root)
        self.assertTrue(any("dangling link" in e for e in r["errors"]), r)

    def test_heading_slug_resolves_without_explicit_id(self):
        write(self.root, "docs/wiki/sub/slug.md", "## 7. 冥想：事件驱动，没有定时器\n")
        write(self.root, "docs/wiki/sub/ref.md", "[m](slug.md#7-冥想事件驱动没有定时器)\n")
        r = gate.check(self.root)
        self.assertEqual(r["errors"], [], r)

    def test_generated_docs_api_is_excluded(self):
        write(self.root, "docs/api/gen.md", "[broken](no-such-file.md)\n")
        self.assertEqual(gate.check(self.root)["errors"], [])

    def test_empty_docs_root_fails_closed(self):
        empty = tempfile.TemporaryDirectory()
        r = gate.check(empty.name)
        self.assertTrue(r["errors"])
        empty.cleanup()


if __name__ == "__main__":
    unittest.main()
