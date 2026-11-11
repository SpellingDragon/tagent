#!/usr/bin/env python3
"""Self-test of check_config_liveness.py: every rule must reject its own negative
fixture and the frozen shape must pass, so the gate cannot print "ok" while a
second publication path stands unrejected."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest

CHECKER = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check_config_liveness.py")

GOLDEN = {
    "schema_version": 1,
    "packages": [],
    "config_facts": {
        "declaration": {
            "type": "config.declaration",
            "type_source": "api/config.Declaration.FactType",
        },
        "publication": {"type": "config.publication"},
        "entry": {
            "publisher": "state.Publisher.Publish",
            "restart_reconcile": "assembly.reconcileBootConfig",
        },
    },
}

FILES = {
    "api/config/fact.go": "package config\nfunc (d Declaration) FactType() string { return \"config.declaration\" }\n",
    "assembly/run.go": (
        "package assembly\n"
        "func (r *Run) Publish() {\n"
        "\tr.publisher = state.NewPublisher(...)\n"
        "\toutcome, err := r.publisher.Publish(ctx, cand)\n"
        "}\n"
    ),
    "assembly/host.go": "package assembly\nfunc Boot(path string) { doc, err := loadHostDocument(path) }\n",
    "assembly/host_document.go": "package assembly\nfunc loadHostDocument(path string) (*hostDocument, error) { return nil, nil }\n",
    "assembly/host_reload.go": "package assembly\nfunc (h *Host) Reload() { doc, err := loadHostDocument(h.configPath) }\n",
    "assembly/host_tools.go": "package assembly\nfunc resolveHostMeditation(doc document.Document) (*hostMeditationPlan, error) { return nil, nil }\n",
    "assembly/commit.go": "package assembly\nconst configPublicationFactType = \"config.publication\"\n",
    "examples/wechat-bot/config_watch.go": "package main\nfunc (w *watcher) reload(ctx context.Context) error { _, err := w.host.Reload(ctx); return err }\n",
}


def make_tree(mutate=None):
    root = tempfile.mkdtemp(prefix="config-liveness-selftest-")
    files = dict(FILES)
    golden = json.loads(json.dumps(GOLDEN))
    if mutate:
        mutate(files, golden)
    for rel, body in files.items():
        if body is None:
            continue
        path = os.path.join(root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(body)
    path = os.path.join(root, "golden", "runtime", "architecture.json")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(golden, handle)
    return root


def run(root):
    return subprocess.run(
        [sys.executable, CHECKER, "--root", root, "--json"],
        capture_output=True, text=True, check=False,
    )


class ConfigLivenessGateTest(unittest.TestCase):
    def assertRejected(self, root, needle):
        result = run(root)
        self.assertNotEqual(result.returncode, 0, "checker passed a tree carrying {}".format(needle))
        self.assertIn(needle, result.stdout, "checker rejected for another reason: {}".format(result.stdout))

    def test_frozen_shape_passes(self):
        result = run(make_tree())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(json.loads(result.stdout)["errors"], [])

    def test_second_loader_path_is_rejected(self):
        def mutate(files, golden):
            files["assembly/host_extra.go"] = "package assembly\nfunc second(path string) { doc, err := loadHostDocument(path) }\n"
        self.assertRejected(make_tree(mutate), "a second load path")

    def test_second_publish_path_is_rejected(self):
        def mutate(files, golden):
            files["assembly/host_extra.go"] = "package assembly\nfunc second(ctx context.Context) { _, err := publisher.Publish(ctx, cand) }\n"
        self.assertRejected(make_tree(mutate), "a second publication path")

    def test_second_publisher_owner_is_rejected(self):
        def mutate(files, golden):
            files["assembly/host_extra.go"] = "package assembly\nfunc second() { publisher = state.NewPublisher() }\n"
        self.assertRejected(make_tree(mutate), "a second publication owner")

    def test_feature_specific_resolver_is_rejected(self):
        def mutate(files, golden):
            files["assembly/host_extra.go"] = "package assembly\nfunc resolveHostFeature(doc document.Document) error { return nil }\n"
        self.assertRejected(make_tree(mutate), "feature-specific assembly path")

    def test_duplicated_declaration_type_is_rejected(self):
        def mutate(files, golden):
            files["assembly/facts.go"] = "package assembly\nconst declarationType = \"config.declaration\"\n"
        self.assertRejected(make_tree(mutate), "literal is duplicated")

    def test_missing_watcher_is_rejected(self):
        def mutate(files, golden):
            files["examples/wechat-bot/config_watch.go"] = None
        self.assertRejected(make_tree(mutate), "content watcher is missing")

    def test_watcher_bypassing_reload_is_rejected(self):
        def mutate(files, golden):
            files["examples/wechat-bot/config_watch.go"] = "package main\nfunc (w *watcher) reload() { _, err := publisher.Publish(ctx, cand) }\n"
        self.assertRejected(make_tree(mutate), "instead of the reload entry")

    def test_unregistered_fact_types_are_rejected(self):
        def mutate(files, golden):
            del golden["config_facts"]
        self.assertRejected(make_tree(mutate), "config_facts is not registered")

    def test_golden_publication_type_must_match_the_constant(self):
        def mutate(files, golden):
            golden["config_facts"]["publication"]["type"] = "config.other"
        self.assertRejected(make_tree(mutate), "does not match the committed constant")

    def test_missing_declaration_source_is_rejected(self):
        def mutate(files, golden):
            golden["config_facts"]["declaration"]["type_source"] = "assembly.const"
        self.assertRejected(make_tree(mutate), "type_source")


if __name__ == "__main__":
    unittest.main()
