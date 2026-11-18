#!/usr/bin/env python3
"""check_dead_producers.py — no production function in the product packages stands without a consumer.

A definition-of-done criterion states that a dead producer is deleted, that an
unwired scaffold is wired or deleted. Build and vet cannot judge it: dead Go code
compiles, so a face whose only callers are its own members survives every compiler
gate. This checker judges it, over the product packages the composition root
serves (the same reviewed set the no-prefix checker and the lint product face
name):

  consumer   a reference from any code outside the package's own production
             declarations: another package, a test file (its calls are real, so a
             test oracle is a consumer), an example, the CLI, the benchmark suite
  dead       a producer no consumer reaches, judged by reachability rather than by
             counting mentions: a family whose members only call each other has
             mentions and still stands unreached
  root       a producer reached without a name-spelled call: init and main, a name
             an interface declares (so the call arrives through the interface), and
             a file-level reference in its own package (a registration table, a
             compile-time assertion that is not a skeleton file)

A skeleton_assert file is not a consumer. It exists to pin a shape, it asserts that
a symbol still compiles, and a package whose only reference is that assertion is
exactly the unwired scaffold this criterion judges.

The registry golden/runtime/dead_producers.json holds the scanned packages and the
reviewed exemptions: a producer whose consumer sits outside this repository
implements an interface declared elsewhere (net/http, encoding/json, a dependency
module), so no in-tree reference can spell the call. An exemption states that
consumer and its reason; an exemption matching no dead producer is rejected as
stale, so the register cannot keep permission for code that is gone or was fixed.

Exit 0 when every producer is reached or exempt; 1 naming every dead producer and
every unusable input (a package that does not exist, a scan that finds no
production function, a malformed or stale exemption).

What it cannot see, stated so a green reading is not read wider than it is: the
name is the only edge this checker follows, so two methods of one package that
share a name share one verdict (a dead one beside a live one is not reported), and
a call spelled only by a string the source holds (reflection, a registry keyed by
name) is invisible. Both directions lose a finding, neither invents one, and the
exemption register is where a producer consumed through an interface declared
outside this repository is recorded.
"""
from __future__ import annotations

import argparse
import collections
import json
import os
import re
import sys

import guard_checklib as lib

GOLDEN_FILE = "golden/runtime/dead_producers.json"
SCHEMA_VERSION = 1
TOKEN = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
FUNC = re.compile(r"^func (?:\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)\s*\(", re.M)
INTERFACE = re.compile(r"\binterface\s*\{([^{}]*)\}", re.S)
METHOD_IN_INTERFACE = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(", re.M)
SKELETON_PREFIX = "skeleton_assert"
ALWAYS_ROOT = frozenset(("init", "main"))
REASON_MIN_CHARS = 20


class CheckError(RuntimeError):
    """The checker cannot judge: an input it reads is missing or unusable."""


def skeleton(path):
    """Whether the file is a compile-time skeleton assertion rather than a consumer."""
    return os.path.basename(path).startswith(SKELETON_PREFIX)


def files_under(root, names, include_tests=False):
    """The Go files under the named roots, with one canonical spelling per path.

    lib.go_files joins the root as given, so `"."` and a package name yield two
    spellings of the same file; the consumer face compares paths, so they are
    normalized once here.
    """
    out = []
    for path in lib.go_files(root, names, include_tests=include_tests):
        canonical = os.path.normpath(path)
        if canonical not in out:
            out.append(canonical)
    return sorted(out)


def masked(text):
    """Blank Go comments and string literals, keeping every offset and newline.

    A name a comment or a printed message mentions is not a consumer, and a brace
    inside a string is not a body.
    """
    return lib.scan_go_strings(lib.strip_go_comments(text))


def body_span(text, after):
    """The span of the function body that follows `after`, paren and bracket aware."""
    depth = 0
    index = after
    length = len(text)
    while index < length:
        char = text[index]
        if char in "([":
            depth += 1
        elif char in ")]":
            depth -= 1
        elif char == "{" and depth == 0:
            break
        index += 1
    if index >= length:
        return None
    opening = index
    depth = 0
    while index < length:
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                return opening, index + 1
        index += 1
    return None


def declarations(text):
    """Every `func` declaration of one file: name, span, and body tokens."""
    found = []
    for match in FUNC.finditer(text):
        span = body_span(text, match.end() - 1)
        if span is None:
            continue
        found.append((match.group(1), match.start(), span[1], text[span[0]:span[1]]))
    return found


def file_level(text):
    """The file's own code with every function declaration blanked out."""
    blanks = list(text)
    for _, start, end, _ in declarations(text):
        for offset in range(start, end):
            if blanks[offset] != "\n":
                blanks[offset] = " "
    return "".join(blanks)


def interface_methods(paths):
    """Every name an interface block declares anywhere in the repository."""
    names = set()
    for path in paths:
        text = masked(lib.read_text(path))
        for block in INTERFACE.finditer(text):
            for name in METHOD_IN_INTERFACE.findall(block.group(1)):
                names.add(name)
    return names


def package_dirs(root, roots):
    """Every directory under the scanned roots that holds a production Go file.

    The judged unit is a Go package, which is a directory: an unexported producer is
    visible to its own package alone, so a consumer inside another directory is a
    real consumer and one inside the same directory is not.
    """
    skipped = []
    found = []
    for name in roots:
        if not os.path.isdir(os.path.join(root, name)):
            raise CheckError("the scanned package {!r} does not exist".format(name))
        files = files_under(root, [name])
        if not files:
            skipped.append(name)
        for path in files:
            directory = lib.rel_path(root, os.path.dirname(os.path.abspath(path)))
            if directory not in found:
                found.append(directory)
    return sorted(found), sorted(skipped)


def scan(root, roots):
    """Declarations per package directory, and the files behind them."""
    directories, skipped = package_dirs(root, roots)
    declared = collections.OrderedDict()
    files_of = {}
    for directory in directories:
        files = [path for path in files_under(root, [directory]) if not skeleton(path)]
        files_of[directory] = files
        table = collections.OrderedDict()
        for path in files:
            for name, _, _, body in declarations(masked(lib.read_text(path))):
                table.setdefault(name, []).append((path, body))
        declared[directory] = table
    return declared, files_of, skipped


def consumer_tokens(directory, production, texts):
    """The tokens that count as consumers of one package's producers."""
    tokens = collections.Counter()
    base = os.path.join(os.sep, directory)
    for path, text in texts.items():
        if skeleton(path):
            continue
        if os.path.dirname(path).endswith(base):
            if path in production:
                tokens.update(TOKEN.findall(file_level(text)))
            else:
                tokens.update(TOKEN.findall(text))
            continue
        tokens.update(TOKEN.findall(text))
    return tokens


def reachable(table, roots):
    """The producers a root reaches through the calls its bodies spell."""
    names = set(table)
    reached = set(name for name in roots if name in names)
    work = list(reached)
    while work:
        name = work.pop()
        for _, body in table[name]:
            for token in TOKEN.findall(body):
                if token in names and token not in reached:
                    reached.add(token)
                    work.append(token)
    return reached


def dead_producers(root, roots):
    """Every producer of the scanned packages that no consumer reaches."""
    declared, files_of, skipped = scan(root, roots)
    every_go = [path for path in files_under(root, ["."], include_tests=True)
                if not skeleton(path)]
    texts = {path: masked(lib.read_text(path)) for path in every_go}
    interfaces = interface_methods(every_go)
    dead = []
    judged = 0
    for directory in declared:
        table = declared[directory]
        judged += len(table)
        if not table:
            continue
        consumers = consumer_tokens(directory, set(files_of[directory]), texts)
        roots = set(ALWAYS_ROOT) & set(table)
        roots.update(name for name in table if name in interfaces)
        roots.update(name for name in table if consumers[name])
        for name in sorted(set(table) - reachable(table, roots)):
            dead.append({
                "package": directory,
                "producer": name,
                "file": lib.rel_path(root, table[name][0][0]),
            })
    if judged == 0:
        raise CheckError("the scan found no production function; the rule has no object")
    return dead, judged, skipped, list(declared)


def exemptions(registry, directories, dead, errors):
    """The reviewed exemptions, and every entry that is malformed or stale."""
    rows = registry.get("exemptions")
    if rows is None:
        rows = []
    if not isinstance(rows, list):
        errors.append("exemptions must be a list")
        return set()
    allowed = {(row["package"], row["producer"]) for row in dead}
    granted = set()
    for index, row in enumerate(rows):
        label = "exemptions[{}]".format(index)
        if not isinstance(row, dict):
            errors.append("{}: every exemption must be an object".format(label))
            continue
        package = row.get("package")
        producer = row.get("producer")
        reason = row.get("reason")
        if not isinstance(package, str) or package not in directories:
            errors.append(
                "{}: package {!r} is not one of the scanned packages".format(label, package))
            continue
        if not isinstance(producer, str) or not producer.strip():
            errors.append("{}: producer must be a non-empty string".format(label))
            continue
        if not isinstance(reason, str) or len(reason.strip()) < REASON_MIN_CHARS:
            errors.append(
                "{}: reason must state the consumer outside this repository (at least {} "
                "characters)".format(label, REASON_MIN_CHARS))
            continue
        key = (package, producer)
        if key in granted:
            errors.append("{}: {}/{} is exempt twice".format(label, package, producer))
            continue
        if key not in allowed:
            errors.append(
                "{}: {}/{} is exempt but stands no dead producer; remove the stale "
                "exemption".format(label, package, producer))
            continue
        granted.add(key)
    return granted


def load_registry(root, path):
    """The reviewed registry, or a CheckError naming what it lacks."""
    target = path if os.path.isabs(path) else os.path.join(root, path)
    try:
        registry = lib.load_json(target)
    except ValueError as exc:
        raise CheckError(str(exc))
    if not isinstance(registry, dict):
        raise CheckError("{} must hold a JSON object".format(path))
    errors = []
    if registry.get("schema_version") != SCHEMA_VERSION:
        errors.append("schema_version must be {}".format(SCHEMA_VERSION))
    packages = registry.get("packages")
    if not isinstance(packages, list) or not packages:
        errors.append("packages must name at least one product package")
    elif any(not isinstance(name, str) or not name.strip() for name in packages):
        errors.append("packages must hold non-empty strings")
    if errors:
        raise CheckError("{}: {}".format(path, "; ".join(errors)))
    return registry


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Judge that every production function in the product packages has a consumer.")
    parser.add_argument("--root", default=os.path.dirname(os.path.dirname(
        os.path.dirname(os.path.abspath(__file__)))),
        help="repository root (default: two levels above scripts/gates/)")
    parser.add_argument("--registry", default=GOLDEN_FILE,
                        help="reviewed registry (default: {})".format(GOLDEN_FILE))
    parser.add_argument("--json", action="store_true", help="report as JSON")
    args = parser.parse_args(argv)
    root = os.path.abspath(args.root)
    try:
        registry = load_registry(root, args.registry)
        roots = list(registry["packages"])
        dead, judged, skipped, directories = dead_producers(root, roots)
    except CheckError as exc:
        print("dead_producers: {}".format(exc), file=sys.stderr)
        return 1
    errors = []
    granted = exemptions(registry, directories, dead, errors)
    remaining = [row for row in dead if (row["package"], row["producer"]) not in granted]
    if args.json:
        print(json.dumps({
            "scanned": roots,
            "packages": directories,
            "test_only_roots": skipped,
            "judged": judged,
            "dead": remaining,
            "exempt": sorted("{}/{}".format(*key) for key in granted),
            "errors": errors,
        }, ensure_ascii=False, indent=2))
    else:
        for root_name in skipped:
            print("dead_producers: {} holds no production Go file, so it contributes no "
                  "producer".format(root_name))
        for row in remaining:
            print("dead_producers: {}/{} has no consumer (declared in {})".format(
                row["package"], row["producer"], row["file"]))
        for error in errors:
            print("dead_producers: {}".format(error))
    if errors or remaining:
        print("dead_producers: {} dead producer(s), {} exemption error(s) over {} production "
              "function(s) in {} package(s)".format(
                  len(remaining), len(errors), judged, len(directories)), file=sys.stderr)
        return 1
    print("dead_producers: every one of {} production function(s) in {} package(s) has a "
          "consumer".format(judged, len(directories)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
