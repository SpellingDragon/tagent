#!/usr/bin/env bash
# check_dependency_debt.sh — prints every pinned module in go.mod together with
# the condition and the steps that retire it.
#
# usage: scripts/gates/check_dependency_debt.sh
#
# Exit status is always 0 (or 2 only when go.mod itself is unreadable). This is a
# register, not a gate. A pin here is a deliberate, recorded decision — the
# alternative, an unpublished fix nobody can build, is worse — so its presence
# must stay visible at any time without a reminder turning into a build failure.
# The gate that does bite is `go mod verify`: it fails when the pinned tag's
# content is not what go.sum records, which is the failure a pin can really cause.
#
# The register body lives beside the replace directive in go.mod (so the reason
# travels with the pin, through every clone and every review of that line). This
# script only surfaces it: a reminder that depends on one maintainer's memory is
# not a register.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f go.mod ]; then
  echo "check_dependency_debt.sh: no go.mod here ($(pwd)) — run from the repository root" >&2
  exit 2
fi

# Single-line replace directives are what this repository uses. A block form
# would silently escape a line-anchored match, so it is named instead of missed.
pins="$(grep -nE '^replace[[:space:]]' go.mod || true)"
blocks="$(grep -nE '^replace[[:space:]]*\($' go.mod || true)"
if [ -n "$blocks" ]; then
  echo "dependency-debt: go.mod uses a replace block at:"
  printf '%s\n' "$blocks" | sed 's/^/  go.mod:/'
  echo "  this script reads single-line directives only; check that block by hand."
fi

if [ -z "$pins" ]; then
  echo "dependency-debt: no pinned module — every require resolves to an official release"
  exit 0
fi

echo "dependency-debt: $(printf '%s\n' "$pins" | wc -l | tr -d ' ') pinned module directive(s) in go.mod"

while IFS= read -r hit; do
  lineno="${hit%%:*}"
  directive="${hit#*:}"
  echo
  echo "  go.mod:${lineno}"
  echo "    $(printf '%s' "$directive" | sed 's/[[:space:]]\+/ /g')"
  case "$directive" in
    *trpc.group/trpc-go/trpc-agent-go*)
      cat <<'REGISTER'
    pinned: the fork tag v1.11.2-tagent.1 = upstream v1.11.2 + the 4
      stream-close-contract commits (close event streams only after the producer
      is done). The tag is published, so a fresh clone builds from the proxy.
    why: those commits reach no official tag yet; upstream PR #2637 carries them.
    retire when: #2637 is merged and an official tag carries the same commit set.
    then: go mod edit -dropreplace=trpc.group/trpc-go/trpc-agent-go
          go get trpc.group/trpc-go/trpc-agent-go@<official tag>
          go mod tidy && go mod verify
          re-run short tests, the race gate, lint, openspec spec hygiene and the
          bot module build, then note the removal in CHANGELOG.md (Unreleased).
REGISTER
      ;;
    *)
      echo "    pinned: (no register entry)"
      echo "    retire when: not recorded — add the condition beside this directive in"
      echo "      go.mod, so the next maintainer inherits a reason and not a guess."
      ;;
  esac
done <<EOF
$pins
EOF

echo
echo "dependency-debt: reminder only — exit 0 by design"
