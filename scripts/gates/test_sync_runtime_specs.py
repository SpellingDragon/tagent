#!/usr/bin/env python3
"""Selftests for the spec-sync step that F4 will run over this change's deltas.

Every case drives the real `openspec` CLI in a throw-away tree: no fixture helper
stands in for the sync, and the repository's own specs are never written.

What the three cases cover:

* 合法并集 —— two compatible ADDED deltas land in one capability (the shape of
  06 and 09 both adding to `bounded-recovery`), and an ADDED delta lands beside an
  existing requirement; every requirement that was already present keeps its own
  block text. The union is additive, never last-write-wins.
* 冲突文本 —— one requirement offered with two different texts (MODIFIED and
  ADDED) is refused by name, and nothing is written or archived.
* 缺 rename 源 —— RENAMED whose FROM header is absent from the main spec is
  refused by name, and nothing is written or archived.

What it does not cover (kept honest, not hidden): the CLI replaces a MODIFIED
block wholesale (last write wins) and has no notion of semantically divergent
text, so a stale MODIFIED delta silently rewrites the main clause. Divergent
delta blocks are therefore excluded from the merge input by the frozen list in
`changes/13-verified-completion/design.md` §C11, not by this gate.

The CLI exits 0 on every refusal path (it prints the named error and returns), so
each assertion reads the printed refusal plus the resulting files; the process
exit code alone is never treated as the verdict.
"""
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
REPO_ROOT = SCRIPTS.parents[1]
CHANGE_DIR = REPO_ROOT / "openspec" / "changes" / "runtime-recovery-ideal"

CLI = "openspec"
PINNED_VERSION = "1.2.0"

PURPOSE = (
    "Probe capability that holds the requirements this sync selftest keeps "
    "untouched beside the ones the deltas add."
)

BASELINE_SPEC = """# {cap} Specification

## Purpose

{purpose}

## Requirements

### Requirement: Baseline Clause

The system SHALL keep this requirement exactly as written by the baseline.

#### Scenario: baseline path
- **WHEN** the baseline path runs
- **THEN** it keeps the baseline answer
"""


def run_cli(args, cwd):
    """Run the CLI and return (exit code, combined output)."""
    proc = subprocess.run(
        [CLI] + args,
        cwd=str(cwd),
        capture_output=True,
        text=True,
    )
    return proc.returncode, (proc.stdout or "") + (proc.stderr or "")


def requirement_blocks(text):
    """Map requirement header -> body text, dropping trailing blank lines."""
    blocks = {}
    current = None
    buf = []
    for line in text.splitlines():
        if line.startswith("### Requirement: "):
            if current is not None:
                blocks[current] = "\n".join(buf).strip("\n")
            current = line[len("### Requirement: "):].strip()
            buf = []
        elif current is not None:
            buf.append(line.rstrip())
    if current is not None:
        blocks[current] = "\n".join(buf).strip("\n")
    return blocks


class Probe(object):
    """One throw-away openspec tree the CLI can archive into."""

    def __init__(self, root):
        self.root = Path(root)
        self.specs = self.root / "openspec" / "specs"
        self.changes = self.root / "openspec" / "changes"
        self.specs.mkdir(parents=True)
        self.changes.mkdir(parents=True)

    def write_main(self, cap, requirements):
        target = self.specs / cap / "spec.md"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(
            BASELINE_SPEC.format(cap=cap, purpose=PURPOSE) + requirements,
            encoding="utf-8",
        )
        return target

    def write_change(self, name, cap, delta):
        change = self.changes / name
        (change / "specs" / cap).mkdir(parents=True, exist_ok=True)
        (change / "proposal.md").write_text(
            "## Why\n\nA synthetic change for the spec-sync selftest.\n\n"
            "## What Changes\n\n" + delta.splitlines()[0] + "\n",
            encoding="utf-8",
        )
        (change / "tasks.md").write_text("- [x] synthetic\n", encoding="utf-8")
        (change / "specs" / cap / "spec.md").write_text(delta, encoding="utf-8")
        return change

    def archive(self, name):
        return run_cli(["archive", name, "-y"], self.root)

    def archived(self, name):
        folder = self.changes / "archive"
        if not folder.is_dir():
            return None
        for entry in sorted(folder.iterdir()):
            if entry.name.endswith("-" + name):
                return entry
        return None


class CompatibleUnionTest(unittest.TestCase):
    def test_compatible_requirement_union_is_synced(self):
        with tempfile.TemporaryDirectory() as root:
            probe = Probe(root)
            created = """## ADDED Requirements

### Requirement: Bounded Read Answers By Identity

The system SHALL answer a committed fact by its identity from the bounded read.

#### Scenario: read by identity
- **WHEN** a reader names a committed fact
- **THEN** the bounded read answers its original bytes
"""
            extended = """## ADDED Requirements

### Requirement: Full Cost Is Measured

The system SHALL measure a boot's full cost in the four named quantities.

#### Scenario: four quantities
- **WHEN** a boot is measured
- **THEN** all four quantities carry a unit and a range

### Requirement: Matrix Gates The Candidate

The system SHALL run the complete matrix on one frozen candidate.

#### Scenario: frozen candidate
- **WHEN** the matrix runs
- **THEN** it reads one source identity
"""
            beside = """## ADDED Requirements

### Requirement: Added Beside The Baseline

The system SHALL add this requirement without touching the baseline block.

#### Scenario: additive only
- **WHEN** the delta is synced
- **THEN** the baseline requirement keeps its text
"""
            probe.write_change("probe-create", "probe-capability", created)
            probe.write_change("probe-extend", "probe-capability", extended)
            baseline = probe.write_main("probe-baseline", "")
            probe.write_change("probe-beside", "probe-baseline", beside)

            code, out = probe.archive("probe-create")
            self.assertEqual(0, code, out)
            self.assertIn("Specs updated successfully.", out, out)
            self.assertIsNotNone(probe.archived("probe-create"), out)

            code, out = probe.archive("probe-extend")
            self.assertEqual(0, code, out)
            self.assertIn("Specs updated successfully.", out, out)
            self.assertIsNotNone(probe.archived("probe-extend"), out)

            baseline_before = requirement_blocks(baseline.read_text(encoding="utf-8"))
            code, out = probe.archive("probe-beside")
            self.assertEqual(0, code, out)
            self.assertIn("Specs updated successfully.", out, out)
            self.assertIsNotNone(probe.archived("probe-beside"), out)

            union = requirement_blocks(
                (probe.specs / "probe-capability" / "spec.md").read_text(encoding="utf-8")
            )
            self.assertEqual(
                [
                    "Bounded Read Answers By Identity",
                    "Full Cost Is Measured",
                    "Matrix Gates The Candidate",
                ],
                list(union),
                "the second ADDED delta extends the capability instead of replacing it",
            )
            self.assertIn("answers its original bytes", union["Bounded Read Answers By Identity"])
            self.assertIn("four quantities", union["Full Cost Is Measured"])
            self.assertIn("frozen candidate", union["Matrix Gates The Candidate"])

            beside_blocks = requirement_blocks(baseline.read_text(encoding="utf-8"))
            self.assertEqual(
                baseline_before["Baseline Clause"],
                beside_blocks["Baseline Clause"],
                "an ADDED delta must not rewrite the requirement it lands beside",
            )
            self.assertEqual(
                ["Baseline Clause", "Added Beside The Baseline"],
                list(beside_blocks),
            )

            # The real repository's ADDED deltas that share a capability must
            # offer pairwise-disjoint headers, else the union is not legal.
            shared = {}
            for spec_file in sorted(CHANGE_DIR.glob("changes/*/specs/*/spec.md")):
                cap = spec_file.parent.name
                text = spec_file.read_text(encoding="utf-8")
                if "## ADDED Requirements" not in text:
                    continue
                titles = [
                    line[len("### Requirement: "):].strip()
                    for line in text.splitlines()
                    if line.startswith("### Requirement: ")
                ]
                shared.setdefault(cap, []).append((str(spec_file), titles))
            inspected = 0
            for cap, entries in shared.items():
                if len(entries) < 2:
                    continue
                inspected += 1
                seen = {}
                for path, titles in entries:
                    for title in titles:
                        self.assertNotIn(
                            title,
                            seen,
                            "{} offers {!r} twice ({} and {})".format(
                                cap, title, seen.get(title), path
                            ),
                        )
                        seen[title] = path
            self.assertGreater(
                inspected,
                0,
                "no capability is fed by two ADDED deltas; the union check would be vacuous",
            )


class ConflictingTextTest(unittest.TestCase):
    def test_conflicting_requirement_text_is_refused(self):
        with tempfile.TemporaryDirectory() as root:
            probe = Probe(root)
            main = probe.write_main("probe-capability", "")
            change = probe.write_change(
                "probe-conflict",
                "probe-capability",
                """## MODIFIED Requirements

### Requirement: Baseline Clause

The system SHALL keep this requirement but rewritten by one side.

#### Scenario: baseline path
- **WHEN** the baseline path runs
- **THEN** it keeps the baseline answer

## ADDED Requirements

### Requirement: Baseline Clause

The system SHALL say something else under the same header.

#### Scenario: other text
- **WHEN** two texts claim one header
- **THEN** nothing is written
""",
            )
            before = main.read_bytes()
            code, out = probe.archive("probe-conflict")

            self.assertIn("Requirement present in both MODIFIED and ADDED", out, out)
            self.assertIn("Baseline Clause", out, out)
            self.assertNotIn("archived as", out, out)
            self.assertEqual(before, main.read_bytes(), "a refused merge must not write")
            self.assertTrue(change.is_dir(), "a refused merge must not archive the change")
            self.assertIsNone(probe.archived("probe-conflict"))


class MissingRenameSourceTest(unittest.TestCase):
    def test_missing_rename_source_is_refused(self):
        with tempfile.TemporaryDirectory() as root:
            probe = Probe(root)
            main = probe.write_main("probe-capability", "")
            change = probe.write_change(
                "probe-rename",
                "probe-capability",
                """## RENAMED Requirements

- FROM: `### Requirement: Clause Nobody Declared`
- TO: `### Requirement: Renamed Target`
""",
            )
            before = main.read_bytes()
            code, out = probe.archive("probe-rename")

            self.assertIn("RENAMED failed", out, out)
            self.assertIn("source not found", out, out)
            self.assertIn("Clause Nobody Declared", out, out)
            self.assertNotIn("archived as", out, out)
            self.assertEqual(before, main.read_bytes(), "a refused merge must not write")
            self.assertTrue(change.is_dir(), "a refused merge must not archive the change")
            self.assertIsNone(probe.archived("probe-rename"))


def setUpModule():
    """The sync tool is frozen: an unpinned CLI would move the merge semantics."""
    code, out = run_cli(["--version"], REPO_ROOT)
    if code != 0:
        raise AssertionError("the sync tool did not answer: {!r}".format(out))
    if PINNED_VERSION not in out:
        raise AssertionError(
            "the sync tool version is pinned at {}; got {!r}".format(
                PINNED_VERSION, out.strip()
            )
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
