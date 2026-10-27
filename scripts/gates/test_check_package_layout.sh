#!/usr/bin/env bash
# test_check_package_layout.sh — the package-layout gate's own negative cases.
#
# Runs the REAL command over fixture trees, so the witness is the process exit
# code: a gate that prints the right rule but exits 0 would fail here, as would a
# rule that became unreachable. The clean fixture is the positive control: a gate
# that always fails cannot pass it.
#
# usage: scripts/gates/test_check_package_layout.sh
set -euo pipefail

# The gate builds and runs the real codetools command, so the fixtures are laid out
# relative to the repository root that owns it.
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/codetools" ./scripts/codetools

fixtures="$tmp/fixtures"
mkdir -p "$fixtures"

# clean: one production file, its exact mirror, one frozen-suffix mirror and the
# one legally named skeleton file.
mkdir -p "$fixtures/clean/agent"
cat > "$fixtures/clean/agent/loop.go" <<'EOF'
package agent
EOF
cat > "$fixtures/clean/agent/loop_test.go" <<'EOF'
package agent
EOF
cat > "$fixtures/clean/agent/loop_real_test.go" <<'EOF'
package agent
EOF
cat > "$fixtures/clean/agent/skeleton_assert.go" <<'EOF'
package agent
EOF

# variant: the shape pin under a second spelling.
mkdir -p "$fixtures/variant/agent"
cat > "$fixtures/variant/agent/reflection_skeleton_assert.go" <<'EOF'
package agent
EOF

# collision: a second skeleton file beside the legal one.
mkdir -p "$fixtures/collision/agent"
cat > "$fixtures/collision/agent/skeleton_assert.go" <<'EOF'
package agent
EOF
cat > "$fixtures/collision/agent/reflection_skeleton_assert.go" <<'EOF'
package agent
EOF

# uppercase: a file name no case-insensitive checkout can tell from its neighbours.
mkdir -p "$fixtures/uppercase/agent"
cat > "$fixtures/uppercase/agent/Loop.go" <<'EOF'
package agent
EOF

# orphan: a test file naming no production file it guards.
mkdir -p "$fixtures/orphan/agent"
cat > "$fixtures/orphan/agent/loop.go" <<'EOF'
package agent
EOF
cat > "$fixtures/orphan/agent/judge_test.go" <<'EOF'
package agent
EOF

fail=0

# expect_case <name> <want-exit> <want-text|-> <dir>
expect_case() {
  local name="$1" want="$2" want_text="$3" dir="$4"
  local out got
  set +e
  out="$("$tmp/codetools" package-layout "$dir" 2>&1)"
  got=$?
  set -e
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: exit $got, want $want; output: $out" >&2
    fail=1
    return
  fi
  if [ "$want_text" != "-" ] && ! printf '%s' "$out" | grep -q "$want_text"; then
    echo "FAIL $name: output does not contain $want_text; output: $out" >&2
    fail=1
    return
  fi
  echo "ok   $name (exit $got)"
}

expect_case clean 0 "names, skeleton pins and test mirrors hold" "$fixtures/clean"
expect_case variant 1 "skeleton-variant" "$fixtures/variant"
expect_case collision 1 "skeleton-collision" "$fixtures/collision"
expect_case uppercase 1 "file-name-not-lower-snake" "$fixtures/uppercase"
expect_case orphan 1 "test-mirror-missing" "$fixtures/orphan"

# A missing root is an unusable input, never a pass.
set +e
"$tmp/codetools" package-layout "$tmp/absent" >/dev/null 2>&1
got=$?
set -e
if [ "$got" -ne 2 ]; then
  echo "FAIL absent-root: exit $got, want 2" >&2
  fail=1
else
  echo "ok   absent-root (exit 2)"
fi

if [ "$fail" -ne 0 ]; then
  echo "test_check_package_layout: failed" >&2
  exit 1
fi
echo "test_check_package_layout: OK (5 fixtures, negative cases exit non-zero)"
