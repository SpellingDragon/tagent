#!/usr/bin/env python3
"""check_vocabulary_alignment.py - code vocabulary aligned with docs/GLOSSARY.md.

A pure phrase ban: any banned occurrence is a violation. There is no baseline, no
classification and no exemption. Four rules over every ``*.go`` file in the tree
plus the generated ``docs/api/*.md``:

* carrier - the legacy polysemous word is fully retired. The case-insensitive
  substring ``carrier`` catches commitCarrier, newCarrier, seedCarrier, "the
  carrier" and "carriers". The verbs carries/carried/carrying lack the substring
  and are not matched.
* compression-generation - promoting compressed material is band promotion, not
  generational / fold generation / folding generation. A case-insensitive phrase
  ban; the phrase allows zero space so a camel-case identifier (foldGeneration)
  is seen. Comment continuation lines are joined into prose first, so a phrase
  split across a wrap ("folding" ends one line, "generation" starts the next) is
  caught. gen:, instance_id and "recorder generation" match none of these.
* plugin-product - the deleted Material x Product matrix must not return. The
  case-sensitive substring ``Product`` is scanned within api/plugin/ only, so a
  compound (MaterialProduct, pluginProduct) is seen. The compressed-product fact
  family lives in api/event (not scanned) and lowercase "product" prose is not
  matched.
* retired-word - 读透视图 / 链权威答案 are not project vocabulary.

The single-token rules (carrier, Product) are scanned per line. The phrase rules
are scanned per line for identifiers and string literals, and again over joined
comment prose for cross-line wraps; the joined pass records only matches that
cross a wrap, so a single-line phrase is counted once.

Exit 0 when clean, 1 on any violation or unusable input (no Go file, no api doc).
"""
from __future__ import annotations

import argparse
import glob
import json
import os
import re
import sys

import guard_checklib as lib

DEFAULT_DOC_GLOB = "docs/api/*.md"
RULE_NAMES = ("carrier", "compression-generation", "plugin-product", "retired-word")
CARRIER_NEEDLE = "carrier"
PRODUCT_NEEDLE = "Product"
PHRASES = {
    "compression-generation": re.compile(
        r"generational|fold\s*generation|folding\s*generation", re.IGNORECASE
    ),
    "retired-word": re.compile(r"读透视图|链权威答案"),
}
PROSE_RULES = tuple(PHRASES)
PLUGIN_DIR = "api/plugin/"
SKIP_DIRS = {".git", "node_modules", "vendor", "__pycache__"}


def scan_files(root, doc_glob):
    """Every Go file in the tree plus the generated API documents."""
    go_paths = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
        for filename in sorted(filenames):
            if filename.endswith(".go"):
                go_paths.append(os.path.join(dirpath, filename))
    doc_paths = sorted(glob.glob(os.path.join(root, doc_glob)))
    return sorted(go_paths), doc_paths


def joined_pieces(pieces):
    """The paragraph text and each piece's [start, end) window in it."""
    text = " ".join(piece for _, piece in pieces)
    windows = []
    offset = 0
    for lineno, piece in pieces:
        windows.append((offset, offset + len(piece), lineno))
        offset += len(piece) + 1
    return text, windows


def piece_line(windows, offset):
    for start, end, lineno in windows:
        if start <= offset < end:
            return lineno
    return windows[-1][2]


def go_prose_paragraphs(text):
    """Comment paragraphs of a Go file, each as [(lineno, text), ...].

    A paragraph is a run of line comments with no blank comment between them, a
    block comment body, or a single trailing comment. Joining the run is what
    lets a phrase spanning a comment wrap match.
    """
    lines = text.splitlines()
    paragraphs = []
    index = 0
    total = len(lines)
    while index < total:
        stripped = lines[index].strip()
        if stripped.startswith("//"):
            pieces = []
            while index < total:
                current = lines[index].strip()
                if not current.startswith("//"):
                    break
                content = current[2:].strip()
                if not content:
                    index += 1
                    break
                pieces.append((index + 1, content))
                index += 1
            if pieces:
                paragraphs.append(pieces)
            continue
        opened = stripped.find("/*")
        if opened >= 0:
            pieces = []
            rest = stripped[opened + 2:]
            while True:
                closes = rest.find("*/")
                chunk = rest if closes < 0 else rest[:closes]
                chunk = re.sub(r"^\s*\*+", "", chunk).strip()
                if chunk:
                    pieces.append((index + 1, chunk))
                if closes >= 0 or index + 1 >= total:
                    index += 1
                    break
                index += 1
                rest = lines[index]
            if pieces:
                paragraphs.append(pieces)
            continue
        trailing = stripped.find("//")
        if trailing >= 0:
            content = stripped[trailing + 2:].strip()
            if content:
                paragraphs.append([(index + 1, content)])
        index += 1
    return paragraphs


def doc_paragraphs(text):
    """Wrapped markdown paragraphs, each as [(lineno, text), ...]."""
    paragraphs = []
    pieces = []
    for lineno, line in enumerate(text.splitlines(), 1):
        content = line.strip()
        if not content:
            if pieces:
                paragraphs.append(pieces)
                pieces = []
            continue
        pieces.append((lineno, content))
    if pieces:
        paragraphs.append(pieces)
    return paragraphs


def record(hits, rel, rule, text, lineno):
    hits.append({"file": rel, "rule": rule, "line": lineno, "text": text})


def scan_line(hits, rel, lineno, text, rules):
    if "carrier" in rules and CARRIER_NEEDLE in text.lower():
        record(hits, rel, "carrier", text, lineno)
    if "plugin-product" in rules and PRODUCT_NEEDLE in text:
        record(hits, rel, "plugin-product", text, lineno)
    for rule in PROSE_RULES:
        if rule in rules and PHRASES[rule].search(text):
            record(hits, rel, rule, text, lineno)


def scan_prose(hits, rel, paragraphs, rules):
    """Record only phrase matches that cross a comment wrap; per-line caught the rest."""
    for pieces in paragraphs:
        text, windows = joined_pieces(pieces)
        for rule in PROSE_RULES:
            if rule not in rules:
                continue
            for match in PHRASES[rule].finditer(text):
                start_line = piece_line(windows, match.start())
                if start_line != piece_line(windows, match.end() - 1):
                    record(hits, rel, rule, text, start_line)


def scan_tree(root, doc_glob):
    go_paths, doc_paths = scan_files(root, doc_glob)
    hits = []
    for path in go_paths:
        rel = lib.rel_path(root, path)
        rules = ["carrier", *PROSE_RULES]
        if rel.startswith(PLUGIN_DIR):
            rules.append("plugin-product")
        text = lib.read_text(path)
        for lineno, line in enumerate(text.splitlines(), 1):
            scan_line(hits, rel, lineno, line.strip(), rules)
        scan_prose(hits, rel, go_prose_paragraphs(text), PROSE_RULES)
    for path in doc_paths:
        rel = lib.rel_path(root, path)
        text = lib.read_text(path)
        for lineno, line in enumerate(text.splitlines(), 1):
            scan_line(hits, rel, lineno, line.strip(), ["carrier", *PROSE_RULES])
        scan_prose(hits, rel, doc_paragraphs(text), PROSE_RULES)
    return go_paths, doc_paths, hits


def summary_line(go_paths, doc_paths, hits):
    by_rule = {rule: 0 for rule in RULE_NAMES}
    for hit in hits:
        by_rule[hit["rule"]] += 1
    return "check_vocabulary_alignment: go_files={} api_docs={} violations={} {}".format(
        len(go_paths),
        len(doc_paths),
        len(hits),
        " ".join("{}={}".format(rule, by_rule[rule]) for rule in RULE_NAMES),
    )


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--json", action="store_true", help="print the result as JSON")
    args = parser.parse_args(argv)

    root = os.path.abspath(args.root)
    go_paths, doc_paths, hits = scan_tree(root, DEFAULT_DOC_GLOB)
    errors = []
    if not go_paths:
        errors.append("no Go file found; the vocabulary rule has no object")
    if not doc_paths:
        errors.append(
            "no api document matched {!r}; docs/api is part of the scan face".format(DEFAULT_DOC_GLOB)
        )
    if errors:
        for error in errors:
            print("ERROR: " + error)
        print("check_vocabulary_alignment: unusable input")
        return 1

    hits = sorted(hits, key=lambda h: (h["file"], h["line"], h["rule"]))
    if args.json:
        print(json.dumps(
            {
                "go_files": len(go_paths),
                "api_docs": len(doc_paths),
                "violation_count": len(hits),
                "violations": hits,
            },
            ensure_ascii=False,
            indent=2,
        ))
        return 1 if hits else 0

    for hit in hits:
        print("ERROR: {}:{}: {}: {}".format(hit["file"], hit["line"], hit["rule"], hit["text"][:160]))
    print(summary_line(go_paths, doc_paths, hits))
    if hits:
        print("check_vocabulary_alignment: {} violation(s)".format(len(hits)))
        return 1
    print("check_vocabulary_alignment: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
