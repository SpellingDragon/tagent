#!/usr/bin/env bash
# race_check.sh — race gate for tagent-OWNED code (v2).
#
# Exit-code fidelity rules (resident-readiness-plan 1.3):
#   - go test's exit status is authoritative. Compile errors, bad package
#     paths, timeouts and plain assertion failures ALWAYS exit non-zero —
#     a clean race log must never be reported as success when the test
#     command itself failed (the old script scanned for race text only and
#     printed "OK" for `race_check.sh ./no-such-package`).
#   - "no DATA RACE text" alone is NOT success.
#   - Upstream-race waivers apply ONLY when ALL of: the run failed; every
#     DATA RACE report's top offending frame is inside trpc-agent-go; the
#     frame matches the registered waiver list below; and the run contains
#     no plain (non-race) failures.
#
# Registered upstream waivers — LEDGER 红色耦合台账, trpc-agent-go v1.10.0:
#   U2  runner steer-queue close vs event consume (invocation/steer frames)
#   U3  inmemory session service hook chain (session.go:122 stack family)
#   C3  timing flakiness is NOT waived here — those tests skip via the
#       agent/race_enabled_test.go build-tag mechanism instead.
# Removal condition: upgrade trpc-agent-go past the upstream fix, delete the
# signature AND the per-test skips, rerun `go test ./agent/... -race`.
#
# Usage: ./scripts/race_check.sh [packages...]   (default: ./agent/... ./plugin/ ./event/)
# Test hook: RACE_CHECK_CLASSIFY_ONLY=1 RACE_CHECK_FAKE_EXIT=<n> feeds stdin
# to the classifier without running go test (used by scripts/test_race_check.sh).
set -uo pipefail

# Bash ERE fragments matched against each race's TOP frame (symbol AND file
# path lines both feed the match, since symbols join packages with dots).
RACE_WAIVER_SIGNATURES=(
	'trpc-agent-go.*steer'
	'trpc-agent-go.*invocation'
	'trpc-agent-go/session.*inmemory'
	'trpc-agent-go.*session/session\.go'
)

# classify <go-test-exit-code> — reads the FULL go-test output on stdin.
# Prints the output plus a verdict; returns the gate exit code.
race_check_classify() {
	local exit_code="$1"
	local out
	out=$(cat)
	# Keep full evidence in every path — never truncate the failure output.
	printf '%s\n' "$out"
	if [ "$exit_code" -eq 0 ]; then
		echo "race_check: OK (go test passed)"
		return 0
	fi
	if ! grep -q "WARNING: DATA RACE" <<<"$out"; then
		echo "race_check: FAIL — go test exited $exit_code with no DATA RACE report (compile error / bad package / timeout / assertion failure). Full output above."
		return 1
	fi
	# Top offending frame of each conflicting access: BOTH the symbol line
	# (2-space indent) and its file path line (6-space indent) feed signature
	# matching — upstream symbols join packages with dots, paths with slashes.
	# A tagent-owned frame there means a race WE introduced.
	local offenders topframes
	offenders=$(awk '
		/^(Read|Write|Previous read|Previous write) at /{grab=1; next}
		grab == 1 && /^  [^ ]/{print $1; grab=2; next}
		grab == 2 && /^      /{print; grab=0; next}
	' <<<"$out")
	topframes=$(printf '%s\n' "$offenders" | sed '/^$/d' | sort -u)
	local tagent_owned=0 unmatched=0 f sig matched
	while IFS= read -r f; do
		[ -z "$f" ] && continue
		if [[ "$f" == *"SpellingDragon/tagent"* ]]; then
			tagent_owned=$((tagent_owned + 1))
			continue
		fi
		matched=0
		for sig in "${RACE_WAIVER_SIGNATURES[@]}"; do
			if [[ "$f" =~ $sig ]]; then
				matched=1
				break
			fi
		done
		[ "$matched" -eq 0 ] && unmatched=$((unmatched + 1))
	done <<<"$topframes"
	if [ "$tagent_owned" -gt 0 ]; then
		echo "race_check: FAIL — tagent-owned racy access detected ($tagent_owned frame(s)):"
		grep -B 2 -A 30 "WARNING: DATA RACE" <<<"$out" || true
		return 1
	fi
	if [ "$unmatched" -gt 0 ]; then
		echo "race_check: FAIL — upstream race(s) NOT covered by the registered waiver list. Register the signature in LEDGER 红色耦合台账 + RACE_WAIVER_SIGNATURES (or fix locally). Unmatched top frames:"
		printf '%s\n' "$topframes" | sed 's/^/            /'
		return 1
	fi
	# Every race is a registered upstream waiver. A single DATA RACE report
	# fails at least one test, so more `--- FAIL:` lines than race reports
	# proves an additional plain failure — waivers must not mask it.
	local races fails
	races=$(grep -c "WARNING: DATA RACE" <<<"$out" || true)
	fails=$(grep -c -- "--- FAIL:" <<<"$out" || true)
	if [ "${fails:-0}" -gt "${races:-0}" ]; then
		echo "race_check: FAIL — ${fails} failing test(s) vs ${races} race report(s): at least one plain (non-race) failure; upstream waivers do not apply."
		return 1
	fi
	echo "race_check: WAIVED — ${races} race(s), all top frames match registered upstream waivers (LEDGER U2/U3, trpc-agent-go v1.10.0). Track for upstream fix:"
	printf '%s\n' "$topframes" | sed 's/^/            /'
	return 0
}

pkgs=("$@")
if [ ${#pkgs[@]} -eq 0 ]; then
	pkgs=(./agent/... ./plugin/ ./event/)
fi

if [ -n "${RACE_CHECK_CLASSIFY_ONLY:-}" ]; then
	race_check_classify "${RACE_CHECK_FAKE_EXIT:-1}"
	exit $?
fi

out=$(go test -race -count=1 "${pkgs[@]}" 2>&1)
rc=$?
printf '%s\n' "$out" | race_check_classify "$rc"
exit $?
