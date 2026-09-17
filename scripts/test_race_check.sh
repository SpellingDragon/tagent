#!/usr/bin/env bash
# test_race_check.sh — negative + positive tests for race_check.sh v2
# (resident-readiness-plan 1.2/1.3).
#
# Cases 1–10 exercise the classifier via RACE_CHECK_CLASSIFY_ONLY (fast,
# deterministic, no real go test); cases 11–12 are real end-to-end runs
# (legal package → OK; illegal package → non-zero — the old script's
# false-green scenario). Run: bash scripts/test_race_check.sh  (all pass → 0)
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$HERE/race_check.sh"
FAILS=0

run_case() {
	local name="$1" fake_exit="$2" input="$3" want_rc="$4" want_pat="$5"
	local rc out
	out=$(RACE_CHECK_CLASSIFY_ONLY=1 RACE_CHECK_FAKE_EXIT="$fake_exit" \
		bash "$SCRIPT" <<<"$input" 2>&1)
	rc=$?
	if [ "$rc" -ne "$want_rc" ]; then
		echo "FAIL: $name — exit=$rc want=$want_rc"
		echo "$out" | tail -5 | sed 's/^/    /'
		FAILS=$((FAILS + 1))
		return
	fi
	if [ -n "$want_pat" ] && ! grep -q "$want_pat" <<<"$out"; then
		echo "FAIL: $name — exit ok but output missing pattern: $want_pat"
		FAILS=$((FAILS + 1))
		return
	fi
	echo "PASS: $name"
}

UP_FRAME='  trpc.group/trpc-go/trpc-agent-go/session.(*inmemoryService).AppendEvent(...)'
UP_PATH='      /Users/x/go/pkg/mod/trpc.group/trpc-go/trpc-agent-go@v1.10.0/session/session.go:122 +0x1c'
TAG_FRAME='  github.com/SpellingDragon/tagent/agent.(*ContextManager).persistBusEvent(...)'
TAG_PATH='      /repo/agent/context_manager.go:802 +0x9c'
OTHER_UP='  trpc.group/trpc-go/trpc-agent-go@v1.9.0/model/stream.go:40 (...)' # upstream, NOT in waiver list

# 1. Clean pass.
run_case "clean pass" 0 "ok  \tgithub.com/SpellingDragon/tagent/event\t0.4s" 0 "OK"

# 2. Illegal package (old false-green scenario).
run_case "illegal package" 1 $'no Go files in /repo/definitely-nonexistent-audit-package' 1 "no DATA RACE report"

# 3. Compile failure.
run_case "compile failure" 1 $'FAIL\tgithub.com/SpellingDragon/tagent/agent [build failed]\nagent/event_bus.go:62:2: undefined: TypeToolUse' 1 "no DATA RACE report"

# 4. Plain assertion failure (no race text).
run_case "plain assertion failure" 1 $'--- FAIL: TestSomething (0.01s)\nFAIL\tgithub.com/SpellingDragon/tagent/agent\t0.5s' 1 "no DATA RACE report"

# 5. Test timeout (panic, no race).
run_case "test timeout" 1 $'panic: test timed out after 10m0s\n--- FAIL: TestSlow (0.00s)' 1 "no DATA RACE report"

# 6. Unknown race with tagent-owned top frame.
race_out() { # $1=top frame sym, $2=frame path
	printf 'WARNING: DATA RACE\nRead at 0x00 by goroutine 8:\n%s\n%s\nPrevious write at 0x00 by goroutine 7:\n%s\n%s\n--- FAIL: TestRacy (0.02s)\nFAIL\tgithub.com/SpellingDragon/tagent/agent\t1.2s\n' "$1" "$2" "$1" "$2"
}
run_case "tagent-owned race" 1 "$(race_out "$TAG_FRAME" "$TAG_PATH")" 1 "tagent-owned"

# 7. Upstream race NOT in the registered waiver list.
run_case "unregistered upstream race" 1 "$(race_out "$OTHER_UP" "$OTHER_UP")" 1 "NOT covered by the registered waiver"

# 8. Registered upstream race alone → waived.
run_case "waived upstream race" 1 "$(race_out "$UP_FRAME" "$UP_PATH")" 0 "WAIVED"

# 9. Waived race PLUS a plain failure (2 FAILs vs 1 race) → must fail.
mixed="$(race_out "$UP_FRAME" "$UP_PATH")
--- FAIL: TestOtherAssertion (0.01s)"
run_case "waived race + plain failure" 1 "$mixed" 1 "plain (non-race) failure"

# 10. Unknown-vs-waiver mix: tagent frame wins even with a waived frame too.
mix2="$(race_out "$UP_FRAME" "$UP_PATH")
WARNING: DATA RACE
Read at 0x01 by goroutine 9:
$TAG_FRAME
$TAG_PATH
--- FAIL: TestRacy (0.02s)
--- FAIL: TestRacy2 (0.02s)"
run_case "mixed tagent+waived frames" 1 "$mix2" 1 "tagent-owned"

# 11. REAL end-to-end: legal package passes.
out=$(bash "$SCRIPT" ./event 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && grep -q "OK" <<<"$out"; then
	echo "PASS: real run legal package (./event)"
else
	echo "FAIL: real run legal package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 12. REAL end-to-end: illegal package must be non-zero (old false green).
out=$(bash "$SCRIPT" ./definitely-nonexistent-audit-package 2>&1)
rc=$?
if [ "$rc" -ne 0 ]; then
	echo "PASS: real run illegal package fails (old false-green fixed)"
else
	echo "FAIL: real run illegal package exited 0 — false green persists"
	FAILS=$((FAILS + 1))
fi

if [ "$FAILS" -gt 0 ]; then
	echo
	echo "FAILED: $FAILS case(s)"
	exit 1
fi
echo
echo "ALL PASS"
