"""runtime_checklib — shared primitives for the four runtime contract checkers.

Deliberately small: only the handful of operations the checkers genuinely share
(strict JSON/JSONL loading with duplicate-key detection, typed comparison of
golden values, module-relative package normalisation, and one report
accumulator).  It is NOT a framework — no rule registry, no plugin engine, no
generic schema language.  Anything used by a single checker stays in that
checker.

Contract types are validated here, not described: three state kinds are kept
strictly apart by every caller.
  * format/spec validity   (parseable, declared)      -> never implies product
  * structural coverage    (every package classified)
  * runtime behaviour      (observations vs golden)   -> only real evidence
`productVerified` is therefore always None in every report: no checker in this
unit is allowed to claim a product PASS.
"""
from __future__ import annotations

import json
import re
import sys

# The one module path this repository's first-party packages live under.  Kept
# here so normalisation cannot drift between checkers.
MODULE_PATH = "github.com/SpellingDragon/tagent"

# Phases: 'skeleton' is the two-phase revision's stage-one phase (data/interface
# definitions + CI-compliance doc comments, zero logic), inserted after 'design'
# and before the stage-two 'implementation'.
PHASES = ("design", "skeleton", "tooling", "implementation", "acceptance")
# Leaf-task ids come in two shapes: the 65 stage-two tasks `NN.M` and the 24
# stage-one skeleton tasks `NN.P0.M` (total 89).
TASK_ID_RE = re.compile(r"^\d{2}\.(?:P0\.\d+|\d+)$")
CHECKBOX_RE = re.compile(r"^\s*-\s*\[(?P<mark>[ xX])\]\s+(?P<id>\d{2}\.(?:P0\.\d+|\d+))\b")
REQUIREMENT_RE = re.compile(r"^###\s+Requirement:\s*(?P<title>.+?)\s*$")
SCENARIO_RE = re.compile(r"^####\s+Scenario:\s*(?P<title>.+?)\s*$")
DELTA_KIND_RE = re.compile(
    r"^##\s+(?P<kind>ADDED|MODIFIED|REMOVED|RENAMED)\s+Requirements\s*$"
)
SHALL_RE = re.compile(r"\b(SHALL|MUST)\b")


class CheckFailure(Exception):
    """Fatal input problem: the checker cannot even start comparing."""


class DuplicateKeyError(CheckFailure):
    """A JSON object repeated a key — rejected, never silently last-wins."""


def _reject_duplicate_keys(pairs):
    seen = set()
    for key, _value in pairs:
        if key in seen:
            raise DuplicateKeyError("duplicate JSON key: {!r}".format(key))
        seen.add(key)
    return dict(pairs)


def load_json_object(path):
    """Load a JSON object, rejecting duplicate keys and non-object roots."""
    try:
        with open(path, "r", encoding="utf-8") as handle:
            text = handle.read()
    except OSError as exc:
        raise CheckFailure("cannot read {}: {}".format(path, exc))
    try:
        value = json.loads(text, object_pairs_hook=_reject_duplicate_keys)
    except CheckFailure:
        raise
    except ValueError as exc:
        raise CheckFailure("invalid JSON in {}: {}".format(path, exc))
    if not isinstance(value, dict):
        raise CheckFailure("{}: top-level JSON value must be an object".format(path))
    return value


def load_jsonl_objects(path):
    """Load a JSONL file (one object per non-blank line), strict on each line."""
    rows = []
    try:
        with open(path, "r", encoding="utf-8") as handle:
            raw_lines = handle.read().splitlines()
    except OSError as exc:
        raise CheckFailure("cannot read {}: {}".format(path, exc))
    for lineno, line in enumerate(raw_lines, start=1):
        if not line.strip():
            continue
        try:
            value = json.loads(line, object_pairs_hook=_reject_duplicate_keys)
        except CheckFailure as exc:
            raise CheckFailure("{}:{}: {}".format(path, lineno, exc))
        except ValueError as exc:
            raise CheckFailure("{}:{}: invalid JSON: {}".format(path, lineno, exc))
        if not isinstance(value, dict):
            raise CheckFailure(
                "{}:{}: each JSONL row must be an object".format(path, lineno)
            )
        rows.append(value)
    return rows


def check_completion_evidence(report, where, evidence):
    """校验完成凭据的结构及逐命令成功结果，不认证来源真实性。"""
    if not isinstance(evidence, dict):
        report.error("{} status 'done' requires an evidence object".format(where))
        return
    run_id = evidence.get("run_id")
    if not isinstance(run_id, str) or not run_id.strip():
        report.error("{} evidence.run_id must be a non-empty string".format(where))
    for field in ("commands", "artifact_paths"):
        value = evidence.get(field)
        if (not isinstance(value, list) or not value
                or any(not isinstance(item, str) or not item.strip() for item in value)):
            report.error("{} evidence.{} must be a non-empty string list".format(where, field))
    codes = evidence.get("exit_codes")
    if (not isinstance(codes, list) or not codes
            or any(type(code) is not int or code != 0 for code in codes)):
        report.error("{} evidence.exit_codes must contain only successful integer exits".format(where))
    commands = evidence.get("commands")
    if isinstance(commands, list) and isinstance(codes, list) and len(commands) != len(codes):
        report.error("{} evidence commands/exit_codes length mismatch".format(where))


def value_kind(value):
    """Type family used by golden comparison; bool is never a number."""
    if isinstance(value, bool):
        return "bool"
    if isinstance(value, (int, float)):
        return "number"
    if isinstance(value, str):
        return "str"
    if isinstance(value, list):
        return "list"
    if isinstance(value, dict):
        return "dict"
    if value is None:
        return "null"
    return type(value).__name__


def typed_equal(expected, actual):
    """Compare a golden expectation with an observation, keeping types apart.

    `True != 1` and `1 != 1.0`-style drift is caught by comparing the type
    family first, then the value, then recursing into containers.
    """
    if value_kind(expected) != value_kind(actual):
        return False
    kind = value_kind(expected)
    if kind in ("bool", "number", "str", "null"):
        return expected == actual
    if kind == "list":
        if len(expected) != len(actual):
            return False
        return all(typed_equal(e, a) for e, a in zip(expected, actual))
    if kind == "dict":
        if set(expected) != set(actual):
            return False
        return all(typed_equal(expected[k], actual[k]) for k in expected)
    return expected == actual


def describe_value(value):
    """Short human-readable rendering used in mismatch reports."""
    return json.dumps(value, ensure_ascii=False, sort_keys=True)


_SAFE_RELATIVE_RE = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_./-]*$")


def normalize_package(raw, module_path=MODULE_PATH):
    """Map a package path to its module-relative form, or None if unknown.

    Accepts the full module path (`<module>/a/b`), the module root itself, or an
    already-relative path (`a/b`, `./a/b`).  Matching is an EXACT prefix test:
    `endswith` is deliberately not used, so a foreign package whose tail
    happens to equal a first-party path can never be accepted.
    """
    if not isinstance(raw, str):
        return None
    text = raw.strip()
    if not text:
        return None
    if text == module_path:
        return "."
    prefix = module_path + "/"
    if text.startswith(prefix):
        rel = text[len(prefix):]
        return rel.rstrip("/") or "."
    if text.startswith("./"):
        rel = text[2:].rstrip("/")
        return rel or "."
    # A bare relative path: no dot in the first segment and a shell-safe shape.
    head = text.split("/", 1)[0]
    if "." not in head and _SAFE_RELATIVE_RE.match(text):
        return text.rstrip("/") or "."
    return None


class Report:
    """Accumulates errors/warnings/counts and renders the mandated JSON shape."""

    def __init__(self, tool, change=None):
        self.tool = tool
        self.change = change
        self.errors = []
        self.warnings = []
        self.counts = {}
        self.notes = []
        # Free-form machine-readable detail (per-file rows, ledgers, ...); kept
        # separate from counts so numeric summaries stay scalar.
        self.extra = {}
        # Never a boolean: no checker in this unit may assert a product PASS.
        self.product_verified = None

    def error(self, message):
        self.errors.append(message)

    def warn(self, message):
        self.warnings.append(message)

    def note(self, message):
        self.notes.append(message)

    def count(self, name, value):
        self.counts[name] = value

    @property
    def ok(self):
        return not self.errors

    def to_dict(self):
        return {
            "tool": self.tool,
            "change": self.change,
            "ok": self.ok,
            "counts": self.counts,
            "extra": self.extra,
            "errors": list(self.errors),
            "warnings": list(self.warnings),
            "notes": list(self.notes),
            "productVerified": self.product_verified,
        }

    def emit(self, as_json):
        payload = self.to_dict()
        if as_json:
            print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
        else:
            print("== {} ({}) ==".format(self.tool, self.change or "-"))
            for key in sorted(payload["counts"]):
                print("counts.{}: {}".format(key, payload["counts"][key]))
            for message in payload["notes"]:
                print("note: {}".format(message))
            for message in payload["warnings"]:
                print("warn: {}".format(message))
            for message in payload["errors"]:
                print("ERROR: {}".format(message))
            print("productVerified: null")
            print("RESULT: {}".format("ok" if self.ok else "FAILED"))
        return 0 if self.ok else 1


def fail_fast(tool, message, as_json=False):
    """Emit a single fatal error report (used for parameter/input failures)."""
    report = Report(tool)
    report.error(message)
    return report.emit(as_json)


def die(tool, message, as_json=False):
    sys.exit(fail_fast(tool, message, as_json))
