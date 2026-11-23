#!/usr/bin/env bash
# archive-preflight.sh — the gate between a finished change and the archive.
#
# `openspec archive` moves a change out of the in-flight tree and asks nothing
# about why it is finished: no run products, no criteria, no spec check. That
# decision therefore lives in this step. Three judgements decide, and all three
# have to hold at once:
#
#   1 evidence binding    the run products are read against this tree, not
#                         against each other (check_runtime_evidence.py --bundle)
#   2 definition of done  the mutation verdicts come from run products; a
#                         hand-filled kill states no verdict
#                         (check_dod_coverage.py --mutation-evidence)
#   3 spec hygiene        the main specs stay strict-valid through the archive
#                         (check-openspec.sh)
#
# The evidence directory is the one the local chain writes and CI downloads, so
# this step reads one run's artifacts instead of the latest green result anyone
# remembers. Each judgement prints the command it ran and that command's own exit
# code, so a refusal names the gate that refused instead of collapsing three
# answers into one.
#
# A change can reach the archive without this step: `openspec archive -y` or a
# plain `mv` writes the same tree, and no local script can stop a command that
# never calls it. `--commit` closes that route from the candidate commit instead
# of from the caller's intentions: a commit that adds paths under the openspec
# archive tree must carry evidence bound to that same revision, or it is refused.
# Running this step before the archive is a discipline; the commit mode is what a
# hook or a required check calls, and until such a caller is configured nothing
# here is a hard block.
#
# usage: bash scripts/gates/archive-preflight.sh [--evidence DIR] [--expect-rev REV]
#        bash scripts/gates/archive-preflight.sh --commit REV [--base REV] [--evidence DIR]
#   --evidence DIR    run directory holding `mutations/evidence.json`
#                     (default: $TAGENT_RUNTIME_EVIDENCE_DIR)
#   --expect-rev REV  the evidence must have been produced at REV
#   --commit REV      judge the tree change REV brings (default base REV^)
#   --base REV        the base of the range --commit inspects
set -euo pipefail
cd "$(dirname "$0")/../.."

contract="golden/runtime/event_integrity_scenarios.json"
# Where a finished change lands. A commit that adds a path here performs the
# archive, whichever command produced the move.
archive_tree_re='^openspec/[^/]+/archive/'

evidence="${TAGENT_RUNTIME_EVIDENCE_DIR:-}"
expect_rev=""
commit_rev=""
base_rev=""
while [ $# -gt 0 ]; do
  case "$1" in
    --evidence)   evidence="${2:?--evidence needs a directory}"; shift 2 ;;
    --expect-rev) expect_rev="${2:?--expect-rev needs a revision}"; shift 2 ;;
    --commit)     commit_rev="${2:?--commit needs a revision}"; shift 2 ;;
    --base)       base_rev="${2:?--base needs a revision}"; shift 2 ;;
    -h|--help)    sed -n '2,45p' "$0"; exit 0 ;;
    *) echo "archive-preflight.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done

refuse() {
  printf '\narchive-preflight: REFUSED — %s\n' "$*" >&2
  exit 1
}

# Resolved once here so every message can name both sides in full.
resolve_rev() {
  git rev-parse --verify --quiet "$1^{commit}"
}

if [ -n "$commit_rev" ]; then
  rev="$(resolve_rev "$commit_rev")" || refuse "cannot resolve --commit $commit_rev"
  [ -n "$base_rev" ] || base_rev="$commit_rev^"
  base="$(resolve_rev "$base_rev")" || refuse "cannot resolve --base $base_rev"
  # --no-renames: a move must show up as an addition here, or a `git mv` into the
  # archive tree would arrive as R and slip past an additions-only filter.
  added="$(git diff --no-renames --name-only --diff-filter=A "$base" "$rev")"
  archived="$(printf '%s\n' "$added" | grep -E "$archive_tree_re" || true)"
  if [ -z "$archived" ]; then
    echo "archive-preflight: $rev adds no path under the archive tree; nothing to judge"
    exit 0
  fi
  echo "archive-preflight: $rev adds $(printf '%s\n' "$archived" | wc -l | tr -d ' ') archived path(s):"
  printf '  %s\n' $archived
  expect_rev="$rev"
fi

if [ -z "$evidence" ]; then
  refuse "no evidence directory: pass --evidence DIR or set TAGENT_RUNTIME_EVIDENCE_DIR (the local chain prints the directory it wrote)"
fi
if [ ! -f "$evidence/mutations/evidence.json" ]; then
  refuse "$evidence holds no mutations/evidence.json: run check_event_integrity_mutations.py --out '$evidence/mutations' (bash scripts/preflight.sh does this), or point --evidence at the directory CI downloads"
fi

evidence_rc=0
printf '\narchive-preflight: 1/3 evidence binding\n  $ python3 scripts/gates/check_runtime_evidence.py --contract %s --bundle %s --repo-root .\n' "$contract" "$evidence"
python3 scripts/gates/check_runtime_evidence.py \
  --contract "$contract" --bundle "$evidence" --repo-root . || evidence_rc=$?

dod_rc=0
printf '\narchive-preflight: 2/3 definition of done\n  $ python3 scripts/gates/check_dod_coverage.py --mutation-evidence %s/mutations\n' "$evidence"
python3 scripts/gates/check_dod_coverage.py --mutation-evidence "$evidence/mutations" || dod_rc=$?

spec_rc=0
printf '\narchive-preflight: 3/3 spec hygiene\n  $ bash scripts/gates/check-openspec.sh\n'
bash scripts/gates/check-openspec.sh || spec_rc=$?

rev_rc=0
if [ -n "$expect_rev" ]; then
  expected="$(resolve_rev "$expect_rev")" || refuse "cannot resolve --expect-rev $expect_rev"
  recorded="$(python3 -c 'import json,sys;print((json.load(open(sys.argv[1])).get("source") or {}).get("git_head") or "")' "$evidence/mutations/evidence.json")"
  printf '\narchive-preflight: commit identity\n  evidence git_head %s, candidate %s\n' "${recorded:-(none)}" "$expected"
  if [ "$recorded" != "$expected" ]; then
    rev_rc=1
    printf '  the run products describe revision %s, not %s: evidence from another commit cannot archive this one\n' "${recorded:-(none)}" "$expected" >&2
  fi
fi

failed=""
if [ "$evidence_rc" -ne 0 ]; then
  failed="$failed evidence-binding(exit $evidence_rc)"
fi
if [ "$dod_rc" -ne 0 ]; then
  failed="$failed definition-of-done(exit $dod_rc)"
fi
if [ "$spec_rc" -ne 0 ]; then
  failed="$failed spec-hygiene(exit $spec_rc)"
fi
if [ "$rev_rc" -ne 0 ]; then
  failed="$failed commit-identity"
fi
if [ -n "$failed" ]; then
  refuse "gates failed:$failed — the archive may not proceed"
fi

printf '\narchive-preflight: all gates green for %s; `openspec archive <change>` may proceed\n' "$evidence"
