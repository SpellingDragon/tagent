#!/usr/bin/env python3
"""check_doc_links.py — validate markdown links and #anchors under docs/.

doc-refs (scripts/codetools/check.go) only scans BACKTICKED file tokens, so a
markdown link like [wiki](../../README.md#current) that points at a missing file
or a deleted anchor passes every existing gate. This checker closes that hole:
it walks docs/**/*.md (excluding the generated docs/api/ tree) and requires
every relative link target to exist and every #fragment to resolve either to an
explicit <a id="frag"> or to a heading whose GitHub-style slug matches.

Fail-closed: unreadable docs, malformed links and a missing docs/ root are errors.
Exit 0 = all links resolve; 1 = at least one dangling link/anchor (or input error).
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

DOC_SUFFIXES = (".md", ".markdown")
GENERATED = ("docs/api/",)
LINK_RE = re.compile(r"(?<!!)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
EXTERNAL = ("http://", "https://", "mailto:", "//")
A_ID_RE = re.compile(r"<a\s+id=[\"']([^\"']+)[\"']", re.I)


def read_text(path):
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def docs(root):
    out = []
    for dp, dn, fn in os.walk(os.path.join(root, "docs")):
        dn[:] = [d for d in dn if not d.startswith(".")]
        for f in fn:
            if f.lower().endswith(DOC_SUFFIXES):
                rel = os.path.relpath(os.path.join(dp, f), root).replace(os.sep, "/")
                if not rel.startswith(GENERATED):
                    out.append(rel)
    return sorted(out)


def slugs(text):
    """Approximate GitHub heading slugs: lowercase, keep CJK/word/space/hyphen,
    collapse spaces to '-', drop a leading numeric 'N.' prefix."""
    out = set()
    for line in text.split("\n"):
        if not line.startswith("#"):
            continue
        title = re.sub(r"<[^>]+>", "", line.lstrip("#").strip())
        slug = re.sub(r"[^\w\u4e00-\u9fff \-]", "", title.lower())
        slug = re.sub(r"^\d+\.\s*", "", slug).strip().replace(" ", "-")
        if slug:
            out.add(slug)
    return out


def check(root):
    errors = []
    files = docs(root)
    if not files:
        return {"errors": ["no markdown documents found under docs/ (fail-closed)"],
                "counts": {"documents": 0, "links": 0, "anchors": 0}}
    n_links = n_anchors = 0
    for rel in files:
        path = os.path.join(root, rel)
        try:
            text = read_text(path)
        except (OSError, UnicodeError) as exc:
            errors.append("{}: unreadable: {}".format(rel, exc))
            continue
        for lineno, line in enumerate(text.split("\n"), 1):
            for m in LINK_RE.finditer(line):
                target = m.group(1)
                if target.startswith(EXTERNAL):
                    continue
                n_links += 1
                raw_path, sep, frag = target.partition("#")
                if raw_path:
                    base = os.path.normpath(os.path.join(os.path.dirname(path), raw_path))
                else:
                    base = path
                if not os.path.isfile(base):
                    errors.append("{}:{}: dangling link -> {}".format(rel, lineno, target))
                    continue
                if not sep:
                    continue
                n_anchors += 1
                try:
                    body = read_text(base)
                except (OSError, UnicodeError) as exc:
                    errors.append("{}:{}: unreadable target {}: {}".format(rel, lineno, base, exc))
                    continue
                ids = set(A_ID_RE.findall(body))
                if frag in ids:
                    continue
                hs = slugs(body)
                if any(s == frag or s.startswith(frag) for s in hs):
                    continue
                errors.append("{}:{}: unresolved anchor #{} in {}".format(rel, lineno, frag, raw_path or os.path.basename(base)))
    return {"errors": errors, "counts": {"documents": len(files), "links": n_links, "anchors": n_anchors}}


def main(argv=None):
    p = argparse.ArgumentParser()
    p.add_argument("--repo-root", default=".")
    p.add_argument("--json", action="store_true")
    a = p.parse_args(argv)
    root = os.path.abspath(a.repo_root)
    result = check(root)
    if a.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        for e in result["errors"]:
            print("ERROR: " + e)
        print("check_doc_links: {} document(s), {} link(s), {} anchor(s), {} error(s)"
              .format(result["counts"]["documents"], result["counts"]["links"],
                      result["counts"]["anchors"], len(result["errors"])))
    return 1 if result["errors"] else 0


if __name__ == "__main__":
    sys.exit(main())
