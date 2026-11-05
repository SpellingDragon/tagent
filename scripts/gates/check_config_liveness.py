#!/usr/bin/env python3
"""check_config_liveness.py — the configuration truth chain's structural pin.

The boundary-closure 08 domain freezes one claim: the source file is the editing
surface, the chain is the ledger, and every change takes effect through one
publication entry. This checker makes the claim executable by scanning the Go
sources for the shapes that would quietly add a second path:

  * loadHostDocument ............ Boot, Reload and its own definition only
  * state.NewPublisher .......... the one Run construction only
  * Publisher.Publish ........... the one Run.Publish call only
  * func resolve<Feature> ....... no new feature-specific assembly path (frozen
                                   allowlist: resolveHostMeditation)
  * "config.declaration" ........ api/config owns the literal; assembly reads
                                   the frozen FactType instead of a second copy
  * examples/wechat-bot watcher . goes through Host.Reload only

It also requires golden/runtime/architecture.json to register the declaration and
publication fact types beside the entry, and checks the registered publication
type against the constant the composition root commits. Run it from the repo
root: python3 scripts/gates/check_config_liveness.py [--root DIR] [--json].
Exit 0 = the sole path still holds; 1 = a second path (or an unregistered fact
type) exists.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

LOADER = "loadHostDocument("
PUBLISHER = ".Publish("
CONSTRUCTOR = "NewPublisher("
CONFIG_TYPE_LITERAL = '"config.declaration"'
PUBLICATION_CONST_RE = re.compile(r'const\s+configPublicationFactType\s*=\s*"([^"]+)"')
RESOLVE_RE = re.compile(r"^func\s+(resolve[A-Z]\w*)\s*\(")
DECLARATION_LITERAL_ALLOWED = {"api/config/fact.go"}
LOADER_ALLOWED = {"assembly/host.go", "assembly/host_document.go", "assembly/host_reload.go"}
CONSTRUCTOR_ALLOWED = {"assembly/run.go", "runtime/state/publish.go"}
PUBLISH_ALLOWED = {"assembly/run.go", "runtime/state/publish.go"}
RESOLVE_ALLOWED = {"resolveHostMeditation"}
WATCHER = "examples/wechat-bot/config_watch.go"


def go_files(root):
    out = []
    for base in ("",):
        for dp, dn, fn in os.walk(os.path.join(root, base)):
            dn[:] = sorted(d for d in dn if not d.startswith(".") and d not in {
                "vendor", "node_modules", "docs", "resources", "testdata", ".git",
                "scripts", ".agents", ".tagent-workspace",
            })
            for name in sorted(fn):
                if name.endswith(".go"):
                    path = os.path.join(dp, name)
                    out.append(os.path.relpath(path, root).replace(os.sep, "/"))
    return sorted(out)


def production(rel):
    return not rel.endswith("_test.go")


def read(root, rel):
    with open(os.path.join(root, rel), encoding="utf-8") as handle:
        return handle.read()


def errors_for(root):
    errors = []
    seen = {}
    for rel in go_files(root):
        if not production(rel):
            continue
        text = read(root, rel)
        for number, line in enumerate(text.split("\n"), start=1):
            if LOADER in line and rel not in LOADER_ALLOWED:
                errors.append("{}:{}: loadHostDocument outside Boot/Reload: a second load path".format(rel, number))
            if CONSTRUCTOR in line and rel not in CONSTRUCTOR_ALLOWED:
                errors.append("{}:{}: NewPublisher outside the one Run construction: a second publication owner".format(rel, number))
            if PUBLISHER in line and rel not in PUBLISH_ALLOWED and not rel.startswith("examples/contract/"):
                errors.append("{}:{}: .Publish outside Run.Publish: a second publication path".format(rel, number))
            if rel.startswith("assembly/") and CONFIG_TYPE_LITERAL in line and rel not in DECLARATION_LITERAL_ALLOWED:
                errors.append("{}:{}: the config.declaration literal is duplicated; read the frozen FactType".format(rel, number))
            match = RESOLVE_RE.match(line.strip())
            if match and rel.startswith("assembly/") and match.group(1) not in RESOLVE_ALLOWED:
                errors.append("{}:{}: {} is a feature-specific assembly path; register it or use the shared resolver".format(rel, number, match.group(1)))
        seen[rel] = text

    watcher = WATCHER
    if watcher not in seen:
        errors.append("{}: the content watcher is missing: no automatic reload path is wired".format(watcher))
    else:
        text = seen[watcher]
        if "host.Reload(" not in text and ".Reload(" not in text:
            errors.append("{}: the watcher does not enter Host.Reload".format(watcher))
        for banned in (LOADER, CONSTRUCTOR, PUBLISHER):
            if banned in text:
                errors.append("{}: the watcher carries {} instead of the reload entry".format(watcher, banned))

    errors.extend(golden_errors(root, seen.get("assembly/commit.go", "")))
    return errors


def golden_errors(root, commit_source):
    golden_path = os.path.join(root, "golden", "runtime", "architecture.json")
    try:
        with open(golden_path, encoding="utf-8") as handle:
            golden = json.load(handle)
    except (OSError, ValueError) as exc:
        return ["golden/runtime/architecture.json: cannot read the registration: {}".format(exc)]
    facts = golden.get("config_facts")
    if not isinstance(facts, dict):
        return ["golden/runtime/architecture.json: config_facts is not registered: the fact types and the one entry are unrecorded"]
    out = []
    declaration = facts.get("declaration") or {}
    if declaration.get("type") != "config.declaration":
        out.append("golden/runtime/architecture.json: config_facts.declaration.type must be config.declaration")
    if declaration.get("type_source") != "api/config.Declaration.FactType":
        out.append("golden/runtime/architecture.json: config_facts.declaration.type_source must name the frozen api/config source")
    publication = facts.get("publication") or {}
    if publication.get("type") != "config.publication":
        out.append("golden/runtime/architecture.json: config_facts.publication.type must be config.publication")
    match = PUBLICATION_CONST_RE.search(commit_source)
    if not match:
        out.append("assembly/commit.go: the publication fact type constant is missing")
    elif match.group(1) != publication.get("type"):
        out.append("golden/runtime/architecture.json: config_facts.publication.type {} does not match the committed constant {}".format(publication.get("type"), match.group(1)))
    entry = facts.get("entry") or {}
    if entry.get("publisher") != "state.Publisher.Publish":
        out.append("golden/runtime/architecture.json: config_facts.entry.publisher must name state.Publisher.Publish")
    if entry.get("restart_reconcile") != "assembly.reconcileBootConfig":
        out.append("golden/runtime/architecture.json: config_facts.entry.restart_reconcile must name the restart reconciliation")
    return out


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args(argv)
    root = os.path.abspath(args.root)
    errors = errors_for(root)
    if args.json:
        print(json.dumps({"root": root, "errors": errors, "ok": not errors}, ensure_ascii=False, indent=2))
    else:
        for error in errors:
            print("config-liveness: {}".format(error), file=sys.stderr)
        print("config-liveness: {} error(s)".format(len(errors)))
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
