#!/usr/bin/env python3
"""check_dod_coverage.py - every purity criterion of the definition of done is bound to its own measurement.

The definition of done of a change states purity criteria (golden/runtime/dod_predicates.json
names the source that states them). A criterion is covered only when four things hold
together, and this gate judges all four:

  criteria  the criterion set is read off the design's criteria list, never off the
            registry: a criterion the design names and the registry omits is
            unguarded, and a registry entry the design no longer names is stale
  anchor    the named test exists exactly once, and its failure paths judge the
            named measure: a measure the anchor never judges in a failure path is a
            proxy for the criterion (a boot reading bytes is not a boot wall clock),
            and a growth criterion whose measure is judged only at a looser factor
            than the registry freezes is a noise bound, not a judgement. A criterion
            whose measurement is a gate names that gate and the selftest proving it
            rejects a violation instead of a test, so the anchor is a path rather
            than a line and drift inside the gate never stales the binding
  mutation  the counterexample is registered in the frozen mutation registry
            (scripts/gates/check_event_integrity_mutations.py), the text the run
            records for it is the one that registry registers, that text stands in
            the anchor's own body, and the run names the anchor as the test that
            failed — or, for a gate anchor, records the verdict of that very gate —
            so the kill is attributable to this anchor and not to a neighbour.
            `anchor.must_assert` names the assertion the criterion requires; the
            text a run records is the registry's, never the name
  ci gate   the CI job carrying the signal is a job of .github/workflows/ci.yml

Exit 0 only when every criterion is bound and proven; 1 names every gap. A
verdict is read off the run products named by `--mutation-evidence` — the
`evidence.json` reports `check_event_integrity_mutations.py --out` writes. A
verdict whose only source is a field of this registry is judged invalid and
named as such: a hand-filled verdict states a conclusion, not a measurement.

A run product also has to describe the tree being judged. Its recorded closure is
read against `--root` by the consumer that owns that comparison
(check_runtime_evidence.check_event_source_binding), and a report written against
another tree is named and refused instead of counted: evidence of a kill in some
other description of the code proves nothing about this one. The formal entry
(`scripts/lint.sh`) therefore passes `$TAGENT_RUNTIME_EVIDENCE_DIR/mutations`;
an invocation without that argument judges a tree no run has described.
"""
from __future__ import annotations

import argparse
import ast
import glob
import importlib.util
import json
import re
import sys
from pathlib import Path

DEFAULT_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_REGISTRY = "golden/runtime/dod_predicates.json"
DEFAULT_CI_WORKFLOW = ".github/workflows/ci.yml"
DEFAULT_MUTATION_REGISTRY = "scripts/gates/check_event_integrity_mutations.py"
MUTATION_PROFILE = "event-integrity"
EVIDENCE_NAME = "evidence.json"
TEST_FILE = "*_test.go"
# Directory names whose Go tests are not this repository's own judgement: the
# nested example modules, the vendored copies and the tool caches.
SKIPPED_PARTS = {".git", "examples", "node_modules", "vendor"}
BLOCK_HEAD = re.compile(r"^\s*(?:if|for|switch)\b")
FAILURE_CALL = re.compile(r"\bt\.(?:Fatalf|Fatal|Errorf|Error)\(")
GO_TEST_SIGNATURE = "func {}("
DOTTED_NAME = re.compile(r"\.\s*([a-z][A-Za-z0-9_]*)")
NUMERIC_FACTOR = re.compile(r"\*\s*([0-9]+)\b")
NAMED_FACTOR = re.compile(r"\*\s*([A-Za-z_][A-Za-z0-9_]*)")
CONST_VALUE = re.compile(r"\bconst\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*([0-9]+)\b")
# Names a failure block reads off a value rather than judges, kept out of the
# proxy report so the gap names quantities.
NOT_A_QUANTITY = {"Helper", "Logf", "Fatalf", "Fatal", "Errorf", "Error"}
# A gate anchor is judged by three facts of its source: an exit that reports a
# violation, and a selftest that reads a gate's exit code beside zero (so it can
# tell a rejection from a pass). The exit code is recognised by where its value
# comes from - a process result's returncode, or a name bound from one, directly
# or through a helper that returns it - never by the variable's name alone. A
# gate that only prints passes every case.
FAILURE_EXIT = re.compile(r"\breturn 1\b|\bsys\.exit\(1\)|\bexit\(1\)")
EXIT_ATTRIBUTE = "returncode"
EXIT_CALLS = {("subprocess", "call"), ("os", "system")}
EXIT_TUPLE_CALLS = {("subprocess", "getstatusoutput"): 0}
ZERO_OPS = (ast.Eq, ast.NotEq)
REQUIRED_PREDICATE_FIELDS = ("id", "criterion", "measure", "scope", "ci_gate")


class GateError(RuntimeError):
    """The gate cannot judge: an input it reads is missing or unusable."""


def resolve(root: Path, path: str) -> Path:
    """Resolve a declared path against the root unless it is already absolute."""
    candidate = Path(path)
    return candidate if candidate.is_absolute() else root / candidate


def read_json(path: Path) -> dict:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise GateError("cannot read {}: {}".format(path, exc))
    if not isinstance(payload, dict):
        raise GateError("{} must hold a JSON object".format(path))
    return payload


def mask(text: str) -> str:
    """Blank string literals and comments, keeping every offset and newline.

    A token that appears only inside a failure message never counts as a judged
    quantity: judgement is read off code, so the mask blanks the text a test
    prints and keeps the expressions it compares.
    """
    out = list(text)
    index = 0
    length = len(text)
    while index < length:
        char = text[index]
        if char == "/" and index + 1 < length and text[index + 1] == "/":
            while index < length and text[index] != "\n":
                out[index] = " "
                index += 1
            continue
        if char == "/" and index + 1 < length and text[index + 1] == "*":
            while index < length and not (text[index] == "*" and index + 1 < length and text[index + 1] == "/"):
                if text[index] != "\n":
                    out[index] = " "
                index += 1
            for offset in (0, 1):
                if index + offset < length:
                    out[index + offset] = " "
            index += 2
            continue
        if char in "\"`'":
            quote = char
            out[index] = " "
            index += 1
            while index < length and text[index] != quote:
                if text[index] == "\\" and quote != "`":
                    out[index] = " "
                    if index + 1 < length:
                        out[index + 1] = " "
                    index += 2
                    continue
                if text[index] != "\n":
                    out[index] = " "
                index += 1
            if index < length:
                out[index] = " "
                index += 1
            continue
        index += 1
    return "".join(out)


def design_criteria(root: Path, source: dict) -> dict:
    """Read the criterion set off the design the registry names as its source."""
    paths = source.get("paths")
    pattern = source.get("pattern")
    if not isinstance(paths, list) or not paths:
        raise GateError("criteria_source.paths must name at least one path")
    if not isinstance(pattern, str) or not pattern:
        raise GateError("criteria_source.pattern must be a regular expression")
    files = []
    for declared in paths:
        files.extend(sorted(glob.glob(str(resolve(root, declared)))))
    if not files:
        raise GateError(
            "the criteria source matched no file: {}".format(", ".join(paths)))
    try:
        regex = re.compile(pattern, re.MULTILINE)
    except re.error as exc:
        raise GateError("criteria_source.pattern does not compile: {}".format(exc))
    criteria = {}
    for name in files:
        text = Path(name).read_text(encoding="utf-8")
        for match in regex.finditer(text):
            criteria[match.group("id")] = {
                "title": match.group("title").strip(),
                "text": match.group("text").strip(),
                "file": name,
            }
    if not criteria:
        raise GateError(
            "the criteria source states no criterion: {} matches none of {}".format(
                pattern, ", ".join(files)))
    return criteria


def ci_jobs(path: Path) -> set:
    """The job names of the CI workflow: the required check set."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise GateError("cannot read {}: {}".format(path, exc))
    jobs = set()
    inside = False
    for line in lines:
        if re.match(r"^jobs:\s*$", line):
            inside = True
            continue
        if not inside:
            continue
        if line and not line.startswith(" "):
            break
        match = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", line)
        if match:
            jobs.add(match.group(1))
    if not jobs:
        raise GateError("{} declares no job under jobs:".format(path))
    return jobs


def load_mutation_module(path: Path):
    """The frozen mutation registry loaded as a module."""
    if not path.is_file():
        raise GateError("the mutation registry is missing: {}".format(path))
    spec = importlib.util.spec_from_file_location("dod_mutation_registry", str(path))
    if spec is None or spec.loader is None:
        raise GateError("the mutation registry cannot be loaded: {}".format(path))
    module = importlib.util.module_from_spec(spec)
    # The registry declares frozen dataclasses, which read their own module back
    # out of sys.modules while the class is built.
    sys.modules[spec.name] = module
    try:
        spec.loader.exec_module(module)
    except BaseException:
        sys.modules.pop(spec.name, None)
        raise
    return module


def frozen_mutations(module) -> dict:
    """The frozen mutation registry: id -> the failure text the kill must produce."""
    entries = getattr(module, "MUTATIONS", None)
    if not isinstance(entries, (list, tuple)) or not entries:
        raise GateError("the mutation registry declares no MUTATIONS")
    out = {}
    for entry in entries:
        identifier = getattr(entry, "id", None)
        if not isinstance(identifier, str) or not identifier:
            raise GateError("the mutation registry holds a mutation with no id")
        out[identifier] = getattr(entry, "expect_assertion", "") or ""
    return out


def test_files(root: Path):
    for name in sorted(root.rglob(TEST_FILE)):
        if SKIPPED_PARTS & set(name.parts):
            continue
        yield name


def anchor_body(root: Path, name: str) -> list:
    """Every file whose masked source declares the named Go test.

    Each match carries the body twice: masked, for the judgement it makes, and as
    written, for the failure text it prints. A literal is judgement's report rather
    than its expression, so the mask blanks it and only the raw body states it.
    """
    found = []
    signature = GO_TEST_SIGNATURE.format(name)
    for path in test_files(root):
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as exc:
            raise GateError("cannot read {}: {}".format(path, exc))
        masked = mask(text)
        if signature not in masked:
            continue
        start = masked.index(signature)
        opening = masked.find("{", start)
        if opening < 0:
            continue
        depth = 0
        end = opening
        for index in range(opening, len(masked)):
            if masked[index] == "{":
                depth += 1
            elif masked[index] == "}":
                depth -= 1
                if depth == 0:
                    end = index
                    break
        found.append((path, masked[opening:end + 1], text[opening:end + 1]))
    return found


def judgement_blocks(body: str) -> list:
    """The failure paths of one test body: the innermost blocks that end in a fatal call.

    Innermost matters: a judgement is one comparison beside its failure text, so a
    loop that carries several judgements is not itself one judgement. The factors a
    block applies are read off that block alone, and an outer block that wraps the
    strict bounds of others would hand every measure the loop's loosest bound.
    """
    lines = body.split("\n")
    depths = [0]
    depth = 0
    for line in lines:
        depth += line.count("{") - line.count("}")
        depths.append(depth)
    spans = []
    for index, line in enumerate(lines):
        if not BLOCK_HEAD.match(line):
            continue
        base = depths[index]
        last = index
        while last + 1 < len(depths) and depths[last + 1] > base:
            last += 1
        spans.append((index, last))
    leaves = [span for span in spans
              if not any(other[0] > span[0] and other[1] <= span[1] for other in spans)]
    blocks = ["\n".join(lines[start:end + 1]) for start, end in leaves]
    return [block for block in blocks if FAILURE_CALL.search(block)]


def judged_factor(body: str, block: str):
    """The tightest growth factor the block applies, resolved through consts."""
    consts = {name: int(value) for name, value in CONST_VALUE.findall(body)}
    factors = [int(value) for value in NUMERIC_FACTOR.findall(block)]
    for name in NAMED_FACTOR.findall(block):
        if name in consts:
            factors.append(consts[name])
    return min(factors) if factors else None


def judged_quantities(blocks: list) -> list:
    names = set()
    for block in blocks:
        names.update(DOTTED_NAME.findall(block))
    return sorted(names - NOT_A_QUANTITY)


def scope_nodes(body: list):
    """The nodes of one scope, leaving nested function and class bodies to their own."""
    stack = list(body)
    while stack:
        node = stack.pop()
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            continue
        yield node
        stack.extend(ast.iter_child_nodes(node))


def call_key(func):
    if isinstance(func, ast.Attribute) and isinstance(func.value, ast.Name):
        return (func.value.id, func.attr)
    return None


def exit_positions(node, helpers: dict) -> set:
    """Where a process exit code stands in an expression.

    None marks the value itself, an integer a tuple slot, so a helper returning
    `(returncode, output)` carries the exit at slot 0 and a destructuring
    assignment binds the right name.
    """
    if isinstance(node, ast.Attribute) and node.attr == EXIT_ATTRIBUTE:
        return {None}
    if isinstance(node, (ast.Tuple, ast.List)):
        return {index for index, element in enumerate(node.elts)
                if None in exit_positions(element, helpers)}
    if isinstance(node, ast.Call):
        if isinstance(node.func, ast.Name):
            return set(helpers.get(node.func.id, ()))
        key = call_key(node.func)
        if key in EXIT_CALLS:
            return {None}
        if key in EXIT_TUPLE_CALLS:
            return {EXIT_TUPLE_CALLS[key]}
    return set()


def exit_helpers(tree) -> dict:
    """Function name -> the exit positions its returns carry, resolved to a fixed point."""
    functions = [node for node in ast.walk(tree)
                 if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))]
    helpers = {}
    changed = True
    while changed:
        changed = False
        for func in functions:
            positions = set()
            for node in scope_nodes(func.body):
                if isinstance(node, ast.Return) and node.value is not None:
                    positions |= exit_positions(node.value, helpers)
            if positions and helpers.get(func.name) != positions:
                helpers[func.name] = positions
                changed = True
    return helpers


def exit_names(body: list, helpers: dict) -> set:
    """The names of one scope bound from a process exit code."""
    names = set()
    for node in scope_nodes(body):
        if isinstance(node, ast.Assign):
            positions = exit_positions(node.value, helpers)
            targets = node.targets
        elif isinstance(node, ast.AnnAssign) and node.value is not None:
            positions = exit_positions(node.value, helpers)
            targets = [node.target]
        else:
            continue
        for target in targets:
            if isinstance(target, ast.Name) and None in positions:
                names.add(target.id)
            elif isinstance(target, (ast.Tuple, ast.List)):
                for index, element in enumerate(target.elts):
                    if isinstance(element, ast.Name) and index in positions:
                        names.add(element.id)
    return names


def is_zero(node) -> bool:
    return (isinstance(node, ast.Constant) and node.value == 0
            and not isinstance(node.value, bool))


def reads_exit(node, helpers: dict, names: set) -> bool:
    if isinstance(node, ast.Name):
        return node.id in names
    if None in exit_positions(node, helpers):
        return True
    if (isinstance(node, ast.Subscript) and isinstance(node.slice, ast.Constant)
            and isinstance(node.slice.value, int)):
        return node.slice.value in exit_positions(node.value, helpers)
    return False


def compares_exit_to_zero(body: list, helpers: dict, names: set) -> bool:
    for node in scope_nodes(body):
        if not isinstance(node, ast.Compare):
            continue
        operands = [node.left] + node.comparators
        for index, op in enumerate(node.ops):
            if not isinstance(op, ZERO_OPS):
                continue
            left, right = operands[index], operands[index + 1]
            if ((is_zero(left) and reads_exit(right, helpers, names))
                    or (is_zero(right) and reads_exit(left, helpers, names))):
                return True
    return False


def selftest_compares_exit(text: str) -> bool:
    """Whether a selftest reads a gate's exit code beside zero.

    The comparison is judged by the binding's source, so a name bound from a
    process result counts whatever it is called, and a comparison of an
    unrelated quantity never stands in for the judgement. Raises SyntaxError
    when the selftest does not parse.
    """
    tree = ast.parse(text)
    helpers = exit_helpers(tree)
    scopes = [tree.body] + [node.body for node in ast.walk(tree)
                            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))]
    for body in scopes:
        names = exit_names(body, helpers)
        if compares_exit_to_zero(body, helpers, names):
            return True
    return False


def check_anchor_gate(entry: dict, anchor: dict, root: Path, gaps: list) -> None:
    """The named gate exists, reads the criterion's measure, and can judge a violation.

    A gate anchor is a text anchor: the gate is named by its path rather than a line,
    so an edit inside the file never makes the binding stale. What has to hold is
    that the gate stands in the tree, that it reads the measure the criterion names
    (else the binding points at another reading), that it fails rather than prints a
    violation, and that its selftest reads a gate exit beside zero so it can tell a
    rejection from a pass.
    """
    identifier = entry["id"]
    declared = anchor.get("gate") or ""
    gate = resolve(root, declared)
    if not gate.is_file():
        gaps.append(
            "{}: the anchor gate {} does not exist in the codebase".format(identifier, declared))
        return
    try:
        text = gate.read_text(encoding="utf-8")
    except OSError as exc:
        raise GateError("cannot read {}: {}".format(gate, exc))
    measure = anchor.get("measure_token") or entry.get("measure") or ""
    if not measure:
        gaps.append("{}: the criterion names no measure".format(identifier))
        return
    if measure not in text:
        gaps.append(
            "{}: the anchor gate {} never reads the measure {}, so the criterion is bound to "
            "another reading".format(identifier, declared, measure))
        return
    if not FAILURE_EXIT.search(text):
        gaps.append(
            "{}: the anchor gate {} states no failing exit, so it reports a violation without "
            "judging one".format(identifier, declared))
        return
    selftest = anchor.get("selftest") or ""
    if not selftest:
        gaps.append(
            "{}: the anchor gate {} names no selftest, so nothing proves it rejects a "
            "violation".format(identifier, declared))
        return
    path = resolve(root, selftest)
    if not path.is_file():
        gaps.append(
            "{}: the anchor gate's selftest {} does not exist in the codebase".format(
                identifier, selftest))
        return
    try:
        test_text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise GateError("cannot read {}: {}".format(path, exc))
    try:
        compares = selftest_compares_exit(test_text)
    except SyntaxError as exc:
        gaps.append(
            "{}: the selftest {} does not parse ({}), so nothing proves the gate rejects a "
            "violation".format(identifier, selftest, exc))
        return
    if not compares:
        gaps.append(
            "{}: the selftest {} never compares a gate exit against zero, so it cannot tell a "
            "rejection from a pass".format(identifier, selftest))


def check_anchor(entry: dict, root: Path, gaps: list) -> None:
    """The anchor exists once and judges the criterion's own measure."""
    identifier = entry["id"]
    anchor = entry.get("anchor")
    if not isinstance(anchor, dict):
        gaps.append("{}: the criterion binds no anchor".format(identifier))
        return
    name = anchor.get("test") or ""
    gate = anchor.get("gate") or ""
    if gate and name:
        gaps.append(
            "{}: the anchor names both the test {} and the gate {}, so it states two "
            "measurements".format(identifier, name, gate))
        return
    if gate:
        check_anchor_gate(entry, anchor, root, gaps)
        return
    if not name:
        gaps.append(
            "{}: the criterion binds neither an anchor test nor an anchor gate, so no "
            "measurement judges it".format(identifier))
        return
    found = anchor_body(root, name)
    if not found:
        gaps.append(
            "{}: the anchor test {} does not exist in the codebase".format(identifier, name))
        return
    if len(found) > 1:
        gaps.append(
            "{}: the anchor test {} is declared {} times ({}), want exactly one".format(
                identifier, name, len(found),
                ", ".join(str(path.relative_to(root)) for path, _, _ in found)))
        return
    path, body, _ = found[0]
    blocks = judgement_blocks(body)
    if not blocks:
        gaps.append(
            "{}: the anchor {} judges nothing in a failure path".format(identifier, name))
        return
    measure = anchor.get("measure_token") or entry.get("measure") or ""
    if not measure:
        gaps.append("{}: the criterion names no measure".format(identifier))
        return
    judged = [block for block in blocks if measure in block]
    if not judged:
        gaps.append(
            "{}: the anchor {} never judges the measure {} in a failure path; it judges {} instead, "
            "so the criterion is bound to a proxy ({})".format(
                identifier, name, measure,
                ", ".join(judged_quantities(blocks)) or "nothing",
                path.relative_to(root)))
        return
    bound = anchor.get("bound")
    if not isinstance(bound, dict):
        return
    factor = bound.get("factor")
    if not isinstance(factor, int) or factor < 1:
        gaps.append(
            "{}: anchor.bound.factor must be a positive integer".format(identifier))
        return
    factors = [value for value in (judged_factor(body, block) for block in judged)
               if value is not None]
    if not factors:
        gaps.append(
            "{}: the anchor {} judges {} with no growth bound, beside the {}x this criterion "
            "is frozen at ({}), so the judgement cannot fail on a regression".format(
                identifier, name, measure, factor, bound.get("vs") or "the reference"))
        return
    if min(factors) > factor:
        gaps.append(
            "{}: the anchor {} judges {} at {}x beside the {}x this criterion is frozen at ({}): "
            "a bound looser than the criterion measures noise, not the measure ({})".format(
                identifier, name, measure, min(factors), factor,
                bound.get("vs") or "the reference", path.relative_to(root)))


def evidence_files(source: Path) -> list:
    """Every run report a `--mutation-evidence` input names."""
    if source.is_file():
        return [source]
    if source.is_dir():
        return sorted(source.rglob(EVIDENCE_NAME))
    raise GateError("the mutation evidence is missing: {}".format(source))


class _GapReport:
    """The evidence consumer's error sink, mapped onto this gate's gap list.

    `check_event_source_binding` states its refusals through a report object and
    this gate collects gaps. One adapter keeps the binding judged by the consumer
    that owns the comparison instead of a second reading of it written here.
    """

    def __init__(self, gaps: list):
        self.gaps = gaps

    def error(self, message: str) -> None:
        self.gaps.append(message)


def evidence_consumer():
    """The evidence checker owning the source binding, imported not copied."""
    gate_dir = str(Path(__file__).resolve().parent)
    if gate_dir not in sys.path:
        sys.path.insert(0, gate_dir)
    try:
        import check_runtime_evidence
    except ImportError as exc:
        raise GateError(
            "cannot load the evidence checker to read the mutation run products against this "
            "tree: {}".format(exc))
    return check_runtime_evidence


def bind_mutation_evidence(path: Path, payload: dict, root: Path, refused: list) -> bool:
    """True when the run product is legal on its own terms and describes `root`.

    Two questions, both owned by the evidence consumer: whether the product is a
    usable mutation report at all — a green control tree, a non-empty mutation list
    whose own fields agree with the kill each one claims, an empty
    `not_evidence_backed` — and whether the closure it names is this tree's. A
    report that fails either proves nothing about this tree, so its verdicts are
    refused rather than counted; a control tree that was not green makes every kill
    in the report unattributable, and a summary field could not see that.
    """
    before = len(refused)
    consumer = evidence_consumer()
    report = _GapReport(refused)
    consumer.check_mutation_evidence(report, str(path), payload)
    consumer.check_event_source_binding(report, str(path), payload, str(root))
    return len(refused) == before


def run_verdicts(sources: list, root: Path) -> tuple:
    """The mutation verdicts the run products state, and every source refused.

    A verdict is usable only as the run product's own record of a counted kill:
    a replay report judges recorded streams rather than a live mutation, so it is
    not a run product and its verdicts never count, and a report whose structure
    or closure is not this tree's verdicts for some other description of the code.
    """
    verdicts = {}
    refused = []
    for source in sources:
        for path in evidence_files(source):
            payload = read_json(path)
            if payload.get("mode") == "replay":
                refused.append(
                    "{}: a replayed report is not a run product, so its verdicts do not "
                    "count".format(path))
                continue
            if payload.get("profile") != MUTATION_PROFILE:
                refused.append(
                    "{}: the report states profile {!r} beside {!r}, so it is not a run of "
                    "the mutation gate".format(path, payload.get("profile"),
                                               MUTATION_PROFILE))
                continue
            if not bind_mutation_evidence(path, payload, root, refused):
                continue
            for record in payload.get("mutations") or []:
                if not isinstance(record, dict):
                    continue
                mutation_id = record.get("id")
                if not isinstance(mutation_id, str) or not mutation_id.strip():
                    continue
                verdicts[mutation_id] = {
                    "id": mutation_id,
                    "verdict": record.get("verdict"),
                    "counted_killed": record.get("counted_killed") is True,
                    "assertion": record.get("expect_assertion"),
                    "test": record.get("test"),
                    "gate": record.get("gate"),
                    "evidence": "{} ({})".format(path, record.get("reason") or "no reason"),
                }
    return verdicts, refused


def registry_verdicts(registry: dict, verdicts: dict, gaps: list) -> None:
    """A verdict only this registry states is invalid, and is named as such."""
    for record in registry.get("mutation_verdicts") or []:
        if not isinstance(record, dict) or not isinstance(record.get("id"), str):
            continue
        mutation_id = record["id"]
        proven = verdicts.get(mutation_id)
        if proven is None:
            gaps.append(
                "{}: the verdict {!r} is stated only by this registry's own field; no run "
                "product records it, so the verdict is invalid and cannot count".format(
                    mutation_id, record.get("verdict")))
            continue
        if proven.get("verdict") != record.get("verdict"):
            gaps.append(
                "{}: this registry states {!r} beside the run product's {!r}; the registry "
                "is stale".format(mutation_id, record.get("verdict"),
                                  proven.get("verdict")))


def check_mutation(entry: dict, mutations: dict, verdicts: dict, root: Path,
                   gaps: list, mutation_module=None) -> None:
    """The counterexample is registered, killed, and killed by this anchor.

    A kill is attributable only through text: the assertion text the frozen registry
    registers for the counterexample has to stand in the anchor's own body, and the
    run that killed it has to name the anchor as the test that failed and repeat
    that same text. A gate anchor is named by its gate instead: the run has to
    record the verdict of that very gate, and the gate's own source has to state
    the registered text. The criterion's `must_assert` names the assertion the
    criterion requires; the text a run records is the registry's, so the registry
    is the one source compared, and a run carrying an older text is a run the
    registry moved past. No line takes part, so an edit above the assertion leaves
    the tie standing.
    """
    identifier = entry["id"]
    mutation = entry.get("mutation")
    if not isinstance(mutation, dict):
        gaps.append("{}: the criterion binds no mutation".format(identifier))
        return
    mutation_id = mutation.get("id") or ""
    if not mutation_id:
        gaps.append("{}: the criterion names no mutation id".format(identifier))
        return
    if mutation.get("must_be_killed") is not True:
        gaps.append(
            "{}: mutation {} does not have to be killed, so nothing proves the criterion "
            "catches a violation".format(identifier, mutation_id))
    if mutation_id not in mutations:
        gaps.append(
            "{}: mutation {} is not registered in the frozen mutation registry".format(
                identifier, mutation_id))
    anchor = entry.get("anchor") or {}
    name = anchor.get("test") or ""
    expectation = mutations.get(mutation_id) or ""
    found = anchor_body(root, name) if name else []
    if expectation and found and expectation not in found[0][2]:
        gaps.append(
            "{}: the registered mutation {} is killed by {!r}, which the anchor {} does not "
            "state, so a kill cannot be attributed to this anchor".format(
                identifier, mutation_id, expectation, name))
    if expectation and anchor.get("gate") and not name:
        gate_path = resolve(root, anchor["gate"])
        if gate_path.is_file():
            try:
                gate_text = gate_path.read_text(encoding="utf-8")
            except OSError as exc:
                raise GateError("cannot read {}: {}".format(gate_path, exc))
            # The registry owns what a gate "states": a report rendered through a
            # format template is stated by its static fragments, not verbatim.
            states = getattr(mutation_module, "gate_text_states", None)
            stated = (states(gate_text, expectation) if states is not None
                      else expectation in gate_text)
            if not stated:
                gaps.append(
                    "{}: the registered mutation {} is killed by {!r}, which the anchor gate {} "
                    "does not state, so a kill cannot be attributed to this anchor".format(
                        identifier, mutation_id, expectation, anchor["gate"]))
    record = verdicts.get(mutation_id)
    if record is None:
        gaps.append(
            "{}: no run records mutation {} as killed; the counterexample is registered and "
            "unproven".format(identifier, mutation_id))
        return
    verdict = record.get("verdict")
    if verdict != "killed" or not record.get("counted_killed"):
        gaps.append(
            "{}: mutation {} is recorded {!r}, so the counterexample survives the criterion "
            "({})".format(identifier, mutation_id, verdict,
                          record.get("evidence") or "no evidence recorded"))
        return
    if not (anchor.get("must_assert") or ""):
        gaps.append("{}: the criterion names no assertion to attribute the kill".format(identifier))
        return
    if not expectation:
        gaps.append(
            "{}: the frozen registry states no assertion text for mutation {}, so a kill of it "
            "names no assertion".format(identifier, mutation_id))
        return
    if anchor.get("gate") and not name:
        gate_verdict = record.get("gate")
        if not isinstance(gate_verdict, dict) or gate_verdict.get("path") != anchor.get("gate"):
            gaps.append(
                "{}: the run records the kill of {} beside the gate anchor {}, which no run "
                "product names as that gate's own verdict, so the kill is unattributable".format(
                    identifier, mutation_id, anchor.get("gate")))
            return
    else:
        recorded_test = record.get("test")
        recorded_name = recorded_test.get("name") if isinstance(recorded_test, dict) else ""
        if not isinstance(recorded_name, str) or not recorded_name.strip():
            gaps.append(
                "{}: the run records the kill of {} under no test, so the kill names no anchor "
                "site".format(identifier, mutation_id))
            return
        if recorded_name != name:
            gaps.append(
                "{}: the run records the kill of {} under the test {!r} beside the anchor {!r}, so "
                "the kill came from another test".format(
                    identifier, mutation_id, recorded_name, name))
            return
    if record.get("assertion") != expectation:
        gaps.append(
            "{}: the kill of {} is recorded under the assertion {!r} beside the registered {!r}, "
            "so the kill belongs to another assertion".format(
                identifier, mutation_id, record.get("assertion"), expectation))


def check(root: Path, registry: dict, ci_workflow: Path, mutation_registry: Path,
          mutation_evidence: list = ()) -> list:
    """Every gap between the definition of done and what the tree proves."""
    gaps = []
    predicates = registry.get("predicates")
    if not isinstance(predicates, list) or not predicates:
        raise GateError("{}: predicates must be a non-empty list".format(DEFAULT_REGISTRY))
    by_id = {}
    for entry in predicates:
        if not isinstance(entry, dict):
            raise GateError("every predicate must be an object")
        for field in REQUIRED_PREDICATE_FIELDS:
            if not isinstance(entry.get(field), str) or not entry[field].strip():
                raise GateError(
                    "predicate {}: {} must be a non-empty string".format(
                        entry.get("id") or "?", field))
        if entry["id"] in by_id:
            raise GateError("predicate {} is registered twice".format(entry["id"]))
        by_id[entry["id"]] = entry

    criteria = design_criteria(root, registry.get("criteria_source") or {})
    for identifier in sorted(criteria):
        if identifier not in by_id:
            gaps.append(
                "{}: the design states the criterion {!r} and the registry binds no measurement "
                "to it".format(identifier, criteria[identifier]["title"]))
    for identifier in sorted(by_id):
        if identifier not in criteria:
            gaps.append(
                "{}: the registry binds a criterion the design no longer states, so it is stale"
                .format(identifier))
            continue
        if criteria[identifier]["title"] not in by_id[identifier]["criterion"]:
            gaps.append(
                "{}: the registry criterion does not state the title the design gives it ({!r}), "
                "so the binding is stale".format(identifier, criteria[identifier]["title"]))

    jobs = ci_jobs(ci_workflow)
    mutation_module = load_mutation_module(mutation_registry)
    mutations = frozen_mutations(mutation_module)
    verdicts, refused = run_verdicts(mutation_evidence, root)
    gaps.extend(refused)
    registry_verdicts(registry, verdicts, gaps)

    for identifier in sorted(by_id):
        entry = by_id[identifier]
        if entry["ci_gate"] not in jobs:
            gaps.append(
                "{}: the CI gate {} is not a job of {}".format(
                    identifier, entry["ci_gate"], ci_workflow))
        check_anchor(entry, root, gaps)
        check_mutation(entry, mutations, verdicts, root, gaps, mutation_module)
    return gaps


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(
        description="Judge that every definition-of-done criterion is bound to its measurement.")
    parser.add_argument("--root", default=str(DEFAULT_ROOT),
                        help="repository root (default: two levels above scripts/gates/)")
    parser.add_argument("--registry", default=DEFAULT_REGISTRY,
                        help="predicate registry (default: {})".format(DEFAULT_REGISTRY))
    parser.add_argument("--ci-workflow", default=DEFAULT_CI_WORKFLOW,
                        help="CI workflow holding the required jobs (default: {})".format(
                            DEFAULT_CI_WORKFLOW))
    parser.add_argument("--mutation-registry", default=DEFAULT_MUTATION_REGISTRY,
                        help="frozen mutation registry (default: {})".format(
                            DEFAULT_MUTATION_REGISTRY))
    parser.add_argument("--mutation-evidence", action="append", default=[],
                        metavar="PATH",
                        help="a run product of check_event_integrity_mutations.py --out (a "
                             "report file or a directory holding evidence.json reports); the "
                             "verdicts are read off these, never off the registry, and each "
                             "report must describe --root. The formal entry scripts/lint.sh "
                             "passes $TAGENT_RUNTIME_EVIDENCE_DIR/mutations")
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()
    try:
        registry = read_json(resolve(root, args.registry))
        gaps = check(root, registry, resolve(root, args.ci_workflow),
                     resolve(root, args.mutation_registry),
                     [resolve(root, path) for path in args.mutation_evidence])
    except GateError as exc:
        print("dod_coverage: {}".format(exc), file=sys.stderr)
        return 1
    if gaps:
        for gap in gaps:
            print("dod_coverage: {}".format(gap))
        print("dod_coverage: {} gap(s) beside the definition of done".format(len(gaps)),
              file=sys.stderr)
        return 1
    total = len(registry.get("predicates") or [])
    print("dod_coverage: {}/{} criteria bound and proven".format(total, total))
    return 0


if __name__ == "__main__":
    sys.exit(main())
