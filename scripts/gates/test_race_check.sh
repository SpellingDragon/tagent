#!/usr/bin/env bash
# test_race_check.sh — negative + positive tests for race_check.sh v2
# (resident-readiness-plan 1.2/1.3).
#
# Cases 1–15 exercise the plain-text classifier via RACE_CHECK_CLASSIFY_ONLY
# (fast, deterministic, no real go test); cases 16–19 are real end-to-end runs
# (legal package → OK; illegal package → non-zero — the old script's
# false-green scenario; the no-Go-files root entry → skipped beside a real
# package, and a set left with no Go package → refused). Cases 20–29 exercise
# shard selection, exact static/artifact unions, missing/duplicate/blank refusal,
# and invalid CLI forms. Cases 30–33 pin the assembly roster split against a
# fake `go test -list` roster: exact once-only coverage of every listed test and
# fail-closed handling of missing, blank, repeated, dropped, extra and
# unlabelled roster artifacts. JSON-1..JSON-17 pin the real path's (package,
# test) attribution from `go test -json`: two races beside a plain failure must
# stay red, a raced child with a failed parent must stay waived, an
# unattributable package/build/panic/line must refuse the waiver, a parallel
# sibling that carries only the race marker is race-attributed while a plain
# sibling stays red, and a race whose conflicting-access frames cannot be parsed
# is refused on both the plain-text (15b/15c) and the real (JSON-16/JSON-17)
# path. 15c and JSON-17 pin the per-block rule: a frameless WARNING beside a
# fully waived race may not borrow that race's frames. C1–C2
# pin the authoritative package-coverage check (a first-party package dropped
# from RACE_PACKAGES, and assembly subpackages that invalidate the roster
# split).
# Run: bash scripts/gates/test_race_check.sh  (all pass → 0)
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$HERE/race_check.sh"
REAL_GO=$(command -v go)
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

run_json_case() {
	local name="$1" fake_exit="$2" input="$3" want_rc="$4" want_pat="$5"
	local rc out
	out=$(RACE_CHECK_CLASSIFY_JSON_ONLY=1 RACE_CHECK_FAKE_EXIT="$fake_exit" \
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
		echo "$out" | tail -8 | sed 's/^/    /'
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
# A top frame is a symbol line plus the 6-space path line under it; the precheck
# matches both lines, so an unregistered race needs the same complete pair a
# real `go test` dump carries.
OTHER_UP_PATH='      /Users/x/go/pkg/mod/trpc.group/trpc-go/trpc-agent-go@v1.9.0/model/stream.go:40 +0x1c'
# U5 — trpc-mcp-go v0.0.10 streamable client transport (v0.0.18 tried and
# rolled back: same race + functional regression; waiver stays).
# never carries its file name, so the symbol line and the path line each need
# their own signature (a single-path signature left the symbol line unmatched).
MCP_SERVER_FRAME='  trpc.group/trpc-go/trpc-mcp-go.(*streamableHTTPServer).handlePost(...)'  # server side, NOT waived
MCP_SERVER_PATH='      /Users/x/go/pkg/mod/trpc.group/trpc-go/trpc-mcp-go@v0.0.10/streamable_server.go:207 +0x1c'
MCP_FRAME='  trpc.group/trpc-go/trpc-mcp-go.(*streamableHTTPClientTransport).setSessionID()'
MCP_PATH='      /Users/x/go/pkg/mod/trpc.group/trpc-go/trpc-mcp-go@v0.0.10/streamable_client.go:607 +0x414'

# JSON fixtures mirror the `go test -json` events the real path consumes. The
# Output values carry a literal backslash-n (single quotes / double quotes do
# not interpret it), so each event is one JSON line.
JPKG='github.com/SpellingDragon/tagent/agent'
JOTHER='github.com/SpellingDragon/tagent/other'
j_out() { printf '{"Action":"output","Package":"%s","Test":"%s","Output":"%s"}\n' "$1" "$2" "$3"; }
j_run() { printf '{"Action":"run","Package":"%s","Test":"%s"}\n' "$1" "$2"; }
j_fail() { printf '{"Action":"fail","Package":"%s","Test":"%s"}\n' "$1" "$2"; }
j_pkgfail() { printf '{"Action":"fail","Package":"%s"}\n' "$1"; }
j_build_output() { printf '{"Action":"build-output","ImportPath":"%s","Output":"%s"}\n' "$1" "$2"; }
j_build_failed() { printf '{"Action":"build-fail","ImportPath":"%s"}\n' "$1"; }
j_race() { # pkg test top-frame symbol top-frame path
	j_out "$1" "$2" '==================\n'
	j_out "$1" "$2" 'WARNING: DATA RACE\n'
	j_out "$1" "$2" 'Read at 0x00 by goroutine 8:\n'
	j_out "$1" "$2" "$3\n"
	j_out "$1" "$2" "$4\n"
	j_out "$1" "$2" 'Previous write at 0x00 by goroutine 7:\n'
	j_out "$1" "$2" "$3\n"
	j_out "$1" "$2" "$4\n"
	j_out "$1" "$2" '==================\n'
}
j_waived_race() { # test — one registered upstream race failing that test
	j_run "$JPKG" "$1"
	j_race "$JPKG" "$1" "$UP_FRAME" "$UP_PATH"
	j_out "$JPKG" "$1" "--- FAIL: $1 (0.01s)\n"
	j_fail "$JPKG" "$1"
}

# Go marks EVERY test that was executing when a race fired: the WARNING stack
# lands in one sibling's log, and each other parallel sibling gets only this
# line (testing.go checkRaces) plus its own --- FAIL. Neither sibling is a plain
# failure, so attribution has to accept both markers.
j_sibling_marker() { # pkg test
	j_out "$1" "$2" '    testing.go:1865: race detected during execution of test\n'
}

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
run_case "unregistered upstream race" 1 "$(race_out "$OTHER_UP" "$OTHER_UP_PATH")" 1 "NOT covered by the registered waiver"

# 8. Registered upstream race alone → waived.
run_case "waived upstream race" 1 "$(race_out "$UP_FRAME" "$UP_PATH")" 0 "WAIVED"

# 9. Waived race PLUS a plain failure (2 FAILs vs 1 race) → must fail.
mixed="$(race_out "$UP_FRAME" "$UP_PATH")
--- FAIL: TestOtherAssertion (0.01s)"
run_case "waived race + plain failure" 1 "$mixed" 1 "plain (non-race) failure"

# 9b. Another package can report a bare FAIL line without a --- FAIL test line.
# Equal test-failure/race counts must not hide that package behind the waiver.
mixed_package="$(race_out "$UP_FRAME" "$UP_PATH")
FAIL	github.com/SpellingDragon/tagent/other	0.5s"
run_case "waived race + another package bare FAIL" 1 "$mixed_package" 1 "plain (non-race) failure"

# 10. Unknown-vs-waiver mix: tagent frame wins even with a waived frame too.
mix2="$(race_out "$UP_FRAME" "$UP_PATH")
WARNING: DATA RACE
Read at 0x01 by goroutine 9:
$TAG_FRAME
$TAG_PATH
--- FAIL: TestRacy (0.02s)
--- FAIL: TestRacy2 (0.02s)"
run_case "mixed tagent+waived frames" 1 "$mix2" 1 "tagent-owned"

# 11. U5 registered upstream race (trpc-mcp-go streamable client) → waived.
run_case "waived trpc-mcp-go streamable client race" 1 "$(race_out "$MCP_FRAME" "$MCP_PATH")" 0 "WAIVED"

# 12. U5 waived race PLUS a plain failure → waivers must not mask it.
mcp_mixed="$(race_out "$MCP_FRAME" "$MCP_PATH")
--- FAIL: TestPlainAssertion (0.01s)"
run_case "waived trpc-mcp-go race + plain failure" 1 "$mcp_mixed" 1 "plain (non-race) failure"

# 13. trpc-mcp-go frame OUTSIDE the transport/file waiver (server side) → red.
run_case "trpc-mcp-go server frame still unregistered" 1 "$(race_out "$MCP_SERVER_FRAME" "$MCP_SERVER_PATH")" 1 "NOT covered by the registered waiver"

# 14. Waived race PLUS a build-failed package: the build failure adds no
# --- FAIL line, so FAIL/race counting alone would still return WAIVED.
mixed_build="$(race_out "$UP_FRAME" "$UP_PATH")
FAIL	github.com/SpellingDragon/tagent/other [build failed]"
run_case "waived upstream race + build failed package" 1 "$mixed_build" 1 "non-race build failure or panic"

# 15. Waived race PLUS a panic: the panic is not attributable to the race.
mixed_panic="$(race_out "$UP_FRAME" "$UP_PATH")
panic: unrelated test panic"
run_case "waived upstream race + panic" 1 "$mixed_panic" 1 "non-race build failure or panic"

# 15b. Race text with no conflicting-access block at all. The precheck used to
# accept any "WARNING: DATA RACE" line and then hand the run to waiver
# attribution: with zero parsed top frames both the tagent-owned and the
# unmatched counters stayed 0, so a race of unknown ownership was waived.
race_no_frames=$'WARNING: DATA RACE\n--- FAIL: TestRacy (0.02s)\nFAIL\tgithub.com/SpellingDragon/tagent/agent\t1.2s'
run_case "race text without any parsable top frame" 1 "$race_no_frames" 1 "no conflicting-access top frame"

# 15c. A frameless WARNING beside a fully waived race. The scanner flattened
# every access block into one frame set, so the waived block's frames covered
# the frameless report too (counts: 2 races, 2 FAILs, 1 package FAIL → WAIVED)
# even though that second race has no identifiable owner. Each WARNING block
# must parse its own complete top frame; a missing frame is never compensated
# by another block.
mixed_unframed="$(race_out "$UP_FRAME" "$UP_PATH")
WARNING: DATA RACE
--- FAIL: TestUnframed (0.02s)"
run_case "waived race + frameless WARNING" 1 "$mixed_unframed" 1 "unframed"

# JSON-1..JSON-16 — the real path's (package, test) attribution. Counting FAIL
# lines against race reports cannot decide these: JSON-4 has equal counts but a
# plain failure, and JSON-5 has a parent/child double FAIL from one race.
run_json_case "JSON clean pass" 0 \
	"$(printf '{"Action":"output","Package":"%s","Output":"ok\\n"}\n' "$JPKG")" 0 "OK"

json_plain="$(j_run "$JPKG" TestPlain)
$(j_out "$JPKG" TestPlain '    race_test.go:9: assertion failed\n')
$(j_out "$JPKG" TestPlain '--- FAIL: TestPlain (0.01s)\n')
$(j_fail "$JPKG" TestPlain)
$(j_pkgfail "$JPKG")"
run_json_case "JSON plain assertion failure" 1 "$json_plain" 1 "no DATA RACE report"

run_json_case "JSON waived upstream race" 1 \
	"$(j_waived_race TestRacy)
$(j_pkgfail "$JPKG")" 0 "WAIVED"

# Exactly the count bug the review named: 2 race reports and 2 failing tests.
# The old text classifier compared those totals (2 > 2 is false) and waived the
# plain failure; per-test attribution must keep it red.
json_two_races="$(j_run "$JPKG" TestRacy)
$(j_race "$JPKG" TestRacy "$UP_FRAME" "$UP_PATH")
$(j_race "$JPKG" TestRacy "$UP_FRAME" "$UP_PATH")
$(j_out "$JPKG" TestRacy '--- FAIL: TestRacy (0.01s)\n')
$(j_fail "$JPKG" TestRacy)
$(j_run "$JPKG" TestPlain)
$(j_out "$JPKG" TestPlain '    race_test.go:20: assertion failed\n')
$(j_out "$JPKG" TestPlain '--- FAIL: TestPlain (0.01s)\n')
$(j_fail "$JPKG" TestPlain)
$(j_pkgfail "$JPKG")"
run_json_case "JSON two races in one package + a plain failure" 1 "$json_two_races" 1 \
	"not attributed to a DATA RACE"

json_child_race="$(j_waived_race TestParent/child)
$(j_out "$JPKG" TestParent '--- FAIL: TestParent (0.00s)\n')
$(j_fail "$JPKG" TestParent)
$(j_pkgfail "$JPKG")"
run_json_case "JSON raced child waives its failed parent" 1 "$json_child_race" 0 \
	"2 failing test(s)"

json_package_fail="$(j_waived_race TestRacy)
$(j_pkgfail "$JPKG")
$(j_out "$JOTHER" '' 'FAIL\tgithub.com/SpellingDragon/tagent/other\t0.2s\n')
$(j_pkgfail "$JOTHER")"
run_json_case "JSON package failure without a failing test" 1 "$json_package_fail" 1 \
	"package failed without any failing test"

json_build_fail="$(j_waived_race TestRacy)
$(j_pkgfail "$JPKG")
$(j_build_output 'github.com/SpellingDragon/tagent/other [github.com/SpellingDragon/tagent/other.test]' '# github.com/SpellingDragon/tagent/other\n')
$(j_build_failed 'github.com/SpellingDragon/tagent/other [github.com/SpellingDragon/tagent/other.test]')
$(j_out "$JOTHER" '' 'FAIL\tgithub.com/SpellingDragon/tagent/other [build failed]\n')
$(j_pkgfail "$JOTHER")"
run_json_case "JSON build failure beside a waived race" 1 "$json_build_fail" 1 \
	"non-race build failure or panic"

json_panic="$(j_waived_race TestRacy)
$(j_out "$JPKG" TestRacy 'panic: boom\n')
$(j_pkgfail "$JPKG")"
run_json_case "JSON panic beside a waived race" 1 "$json_panic" 1 \
	"non-race build failure or panic"

json_tagent="$(j_run "$JPKG" TestRacy)
$(j_race "$JPKG" TestRacy "$TAG_FRAME" "$TAG_PATH")
$(j_out "$JPKG" TestRacy '--- FAIL: TestRacy (0.01s)\n')
$(j_fail "$JPKG" TestRacy)
$(j_pkgfail "$JPKG")"
run_json_case "JSON tagent-owned race" 1 "$json_tagent" 1 "tagent-owned"

json_unregistered="$(j_run "$JPKG" TestRacy)
$(j_race "$JPKG" TestRacy "$OTHER_UP" "$OTHER_UP_PATH")
$(j_out "$JPKG" TestRacy '--- FAIL: TestRacy (0.01s)\n')
$(j_fail "$JPKG" TestRacy)
$(j_pkgfail "$JPKG")"
run_json_case "JSON unregistered upstream race" 1 "$json_unregistered" 1 \
	"NOT covered by the registered waiver"

json_undecodable="$(j_waived_race TestRacy)
$(j_pkgfail "$JPKG")
random plain line that is not a json event"
run_json_case "JSON undecodable line refuses the waiver" 1 "$json_undecodable" 1 \
	"undecodable go test -json line"

json_benign="go: downloading example.com/x v1.0.0
$(j_waived_race TestRacy)
$(j_pkgfail "$JPKG")"
run_json_case "JSON benign download line keeps the waiver" 1 "$json_benign" 0 "WAIVED"

# JSON-13. Parallel siblings of one race: the WARNING stack is printed into one
# test's log and the sibling carries only the race marker. Counting only
# "WARNING: DATA RACE" called the sibling a plain failure and refused a waiver
# the race itself earns; both failing tests are race-attributed.
json_parallel_siblings="$(j_waived_race TestRacyUpstream)
$(j_run "$JPKG" TestParallelSibling)
$(j_sibling_marker "$JPKG" TestParallelSibling)
$(j_out "$JPKG" TestParallelSibling '--- FAIL: TestParallelSibling (0.01s)\n')
$(j_fail "$JPKG" TestParallelSibling)
$(j_pkgfail "$JPKG")"
run_json_case "JSON parallel sibling marker joins the waived race" 1 "$json_parallel_siblings" 0 \
	"2 failing test(s)"

# JSON-14. The marker may not excuse a failure that carries no race evidence of
# its own: a sibling that fails on an assertion keeps the run red.
json_plain_sibling="$(j_waived_race TestRacy)
$(j_run "$JPKG" TestPlainSibling)
$(j_out "$JPKG" TestPlainSibling '    race_test.go:41: assertion failed\n')
$(j_out "$JPKG" TestPlainSibling '--- FAIL: TestPlainSibling (0.01s)\n')
$(j_fail "$JPKG" TestPlainSibling)
$(j_pkgfail "$JPKG")"
run_json_case "JSON plain sibling is not excused by the race marker" 1 "$json_plain_sibling" 1 \
	"not attributed to a DATA RACE"

# JSON-15. The marker is a sibling attestation, never a race report: a run whose
# only evidence is the marker still has no DATA RACE to waive.
json_marker_only="$(j_run "$JPKG" TestParallelSibling)
$(j_sibling_marker "$JPKG" TestParallelSibling)
$(j_out "$JPKG" TestParallelSibling '--- FAIL: TestParallelSibling (0.01s)\n')
$(j_fail "$JPKG" TestParallelSibling)
$(j_pkgfail "$JPKG")"
run_json_case "JSON race marker alone still demands a real DATA RACE" 1 "$json_marker_only" 1 \
	"no DATA RACE report"

# JSON-16. Same fail-closed rule as case 15b on the real path: a WARNING with no
# conflicting-access block leaves the owner unknowable.
json_no_frames="$(j_run "$JPKG" TestRacy)
$(j_out "$JPKG" TestRacy 'WARNING: DATA RACE\n')
$(j_out "$JPKG" TestRacy '--- FAIL: TestRacy (0.01s)\n')
$(j_fail "$JPKG" TestRacy)
$(j_pkgfail "$JPKG")"
run_json_case "JSON race without a parsable top frame refuses the waiver" 1 "$json_no_frames" 1 \
	"no conflicting-access top frame"

# JSON-17. Same rule as 15c on the real path: per-stream attribution sees the
# WARNING in TestUnframed's own output and would waive it, while the only parsed
# frames come from TestRacy's waived report. A block with a missing frame fails
# closed on its own.
json_unframed="$(j_waived_race TestRacy)
$(j_run "$JPKG" TestUnframed)
$(j_out "$JPKG" TestUnframed 'WARNING: DATA RACE\n')
$(j_out "$JPKG" TestUnframed '--- FAIL: TestUnframed (0.01s)\n')
$(j_fail "$JPKG" TestUnframed)
$(j_pkgfail "$JPKG")"
run_json_case "JSON waived race + frameless WARNING in another test" 1 "$json_unframed" 1 \
	"missing frame"

# 16. REAL end-to-end: legal package passes.
out=$(bash "$SCRIPT" ./api/event 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && grep -q "OK" <<<"$out"; then
	echo "PASS: real run legal package (./event)"
else
	echo "FAIL: real run legal package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 17. REAL end-to-end: illegal package must be non-zero (old false green).
out=$(bash "$SCRIPT" ./definitely-nonexistent-audit-package 2>&1)
rc=$?
if [ "$rc" -ne 0 ]; then
	echo "PASS: real run illegal package fails (old false-green fixed)"
else
	echo "FAIL: real run illegal package exited 0 — false green persists"
	FAILS=$((FAILS + 1))
fi

# Case 18 pins the no-Go-files skip: the module root (`.`) expands to no Go
# package, and before the skip existed `go test -race .` failed the whole job with
# `[setup failed]`. The declared entry stays in the set and is skipped by name;
# case 19 refuses a set that keeps no Go package at all, so the skip can never
# empty the gate; case 17 (illegal package) keeps proving a misspelt package is
# NOT skipped.
out=$(bash "$SCRIPT" . ./api/event 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && grep -q 'skip \. (' <<<"$out" && grep -q "OK" <<<"$out"; then
	echo "PASS: real run skips the root module beside a real package"
else
	echo "FAIL: real run with the root module — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 19. REAL end-to-end: a set that keeps no Go package must be refused, not green.
out=$(bash "$SCRIPT" . 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "no package in the declared set carries Go files" <<<"$out"; then
	echo "PASS: real run refuses a set with no Go package"
else
	echo "FAIL: real run with only the root module — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 20. Static union: exactly the canonical set, sorted and duplicate-free.
out=$(bash "$SCRIPT" --shard-union-check 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -n "$out" ] && [ "$(sort <<<"$out" | uniq -d | wc -l | tr -d ' ')" -eq 0 ]; then
	echo "PASS: static shard union is nonempty, sorted and duplicate-free"
else
	echo "FAIL: static shard union — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 21. Every shard emits its selected manifest before the classifier test hook.
TMP=$(mktemp -d)
cleanup() {
	rm -f "$TMP"/*.sh "$TMP"/*.packages "$TMP"/*.txt \
		"$TMP"/bundle/race-shard-*/* \
		"$TMP"/roster/race-shard-*/* "$TMP"/fakebin/go
	rmdir "$TMP"/bundle/race-shard-* "$TMP"/bundle \
		"$TMP"/roster/race-shard-* "$TMP"/roster "$TMP"/fakebin 2>/dev/null || true
	rmdir "$TMP" 2>/dev/null || true
}
trap cleanup EXIT
for shard in 1 2 3; do
	mkdir -p "$TMP/bundle/race-shard-$shard"
	manifest="$TMP/bundle/race-shard-$shard/packages.txt"
	out=$(RACE_CHECK_CLASSIFY_ONLY=1 RACE_CHECK_FAKE_EXIT=0 \
		RACE_CHECK_PACKAGE_FILE="$manifest" bash "$SCRIPT" --shard "$shard" <<<"ok" 2>&1)
	rc=$?
	if [ "$rc" -eq 0 ] && [ -s "$manifest" ] && grep -q "OK" <<<"$out"; then
		echo "PASS: shard $shard selects packages and writes its manifest"
	else
		echo "FAIL: shard $shard selection — exit=$rc"
		echo "$out" | tail -5 | sed 's/^/    /'
		FAILS=$((FAILS + 1))
	fi
done

# 22. Uploaded manifests union to the same exact canonical set. Compare both
# through the gate and independently through sort/diff.
bash "$SCRIPT" --shard-union-check >"$TMP/expected.packages"
sort -u "$TMP"/bundle/race-shard-*/packages.txt >"$TMP/actual.packages"
out=$(bash "$SCRIPT" --shard-union-check "$TMP/bundle" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && grep -q "shard artifact union OK" <<<"$out" && \
	diff -u "$TMP/expected.packages" "$TMP/actual.packages" >/dev/null; then
	echo "PASS: uploaded shard manifests have an exact union"
else
	echo "FAIL: uploaded shard union — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 23. A missing manifest is refused, never treated as an empty successful shard.
mv "$TMP/bundle/race-shard-2/packages.txt" "$TMP/missing-packages.txt"
out=$(bash "$SCRIPT" --shard-union-check "$TMP/bundle" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "missing or empty shard package manifest" <<<"$out"; then
	echo "PASS: missing shard manifest is refused"
else
	echo "FAIL: missing shard manifest — exit=$rc"
	FAILS=$((FAILS + 1))
fi
mv "$TMP/missing-packages.txt" "$TMP/bundle/race-shard-2/packages.txt"

# 23b. Blank-only files are nonempty on disk but carry no package evidence.
cp "$TMP/bundle/race-shard-2/packages.txt" "$TMP/shard-2.packages"
printf '\n' >"$TMP/bundle/race-shard-2/packages.txt"
out=$(bash "$SCRIPT" --shard-union-check "$TMP/bundle" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "contains no package entries" <<<"$out"; then
	echo "PASS: blank-only shard manifests are refused"
else
	echo "FAIL: blank-only shard manifest — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi
cp "$TMP/shard-2.packages" "$TMP/bundle/race-shard-2/packages.txt"

# 24. Removing one package from an uploaded manifest makes the exact diff red.
cp "$TMP/bundle/race-shard-3/packages.txt" "$TMP/shard-3.packages"
sed '1d' "$TMP/shard-3.packages" >"$TMP/bundle/race-shard-3/packages.txt"
out=$(bash "$SCRIPT" --shard-union-check "$TMP/bundle" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "missing package(s)" <<<"$out"; then
	echo "PASS: missing package in uploaded union is refused"
else
	echo "FAIL: missing uploaded package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi
cp "$TMP/shard-3.packages" "$TMP/bundle/race-shard-3/packages.txt"

# 25. Repeating a package across shards is refused even if the line count matches.
first_package=$(sed -n '1p' "$TMP/bundle/race-shard-3/packages.txt")
printf '%s\n' "$first_package" >>"$TMP/bundle/race-shard-2/packages.txt"
out=$(bash "$SCRIPT" --shard-union-check "$TMP/bundle" 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "duplicate package(s)" <<<"$out"; then
	echo "PASS: duplicate package across uploaded shards is refused"
else
	echo "FAIL: duplicate uploaded package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi
sed '$d' "$TMP/bundle/race-shard-2/packages.txt" >"$TMP/shard-2.packages"
mv "$TMP/shard-2.packages" "$TMP/bundle/race-shard-2/packages.txt"

# 26. Negative control: delete one index from a shard definition. The static
# union must name the missing package and fail before any go test can run.
sed "s/'0 2 3 4 5 6 7 8 9 14 16 17 20 21 22 23'/'0 2 3 4 5 6 7 8 9 14 16 17 21 22 23'/" "$SCRIPT" >"$TMP/race_check_missing.sh"
out=$(bash "$TMP/race_check_missing.sh" --shard-union-check 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "missing package(s)" <<<"$out" && grep -q "./tests/integration/host/..." <<<"$out"; then
	echo "PASS: deleting a package from a shard definition turns static union red"
else
	echo "FAIL: deleted shard package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 27. Negative control: duplicate one index in a shard definition.
sed "s/'0 2 3 4 5 6 7 8 9 14 16 17 20 21 22 23'/'0 2 3 4 5 6 7 8 9 14 16 17 20 20 21 22 23'/" "$SCRIPT" >"$TMP/race_check_duplicate.sh"
out=$(bash "$TMP/race_check_duplicate.sh" --shard-union-check 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "duplicate shard index.*only assembly" <<<"$out"; then
	echo "PASS: duplicating a package in a shard definition turns static union red"
else
	echo "FAIL: duplicated shard package — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 28. Invalid shard selectors and ambiguous modes fail before go test.
for args in '--shard' '--shard 0' '--shard 4' '--shard 1 ./api/event' \
	'--shard-union-check a b' '--shard-union-check --shard 1' '--bogus'; do
	out=$(bash "$SCRIPT" $args 2>&1)
	rc=$?
	if [ "$rc" -ne 0 ]; then
		echo "PASS: invalid CLI refused: $args"
	else
		echo "FAIL: invalid CLI accepted: $args"
		FAILS=$((FAILS + 1))
	fi
done

# 29. An out-of-range or noncanonical index is a definition error, not a test skip.
for replacement in '33' '01' 'x'; do
	sed "s/'0 2 3 4 5 6 7 8 9 14 16 17 20 21 22 23'/'0 2 3 4 5 6 7 8 9 14 16 17 $replacement 21 22 23'/" "$SCRIPT" >"$TMP/race_check_invalid.sh"
	out=$(bash "$TMP/race_check_invalid.sh" --shard-union-check 2>&1)
	rc=$?
	if [ "$rc" -ne 0 ] && grep -q "outside the canonical package set" <<<"$out"; then
		echo "PASS: invalid shard index refused: $replacement"
	else
		echo "FAIL: invalid shard index accepted: $replacement"
		FAILS=$((FAILS + 1))
	fi
done

# --- 30–33: assembly roster split. A first-letter class (^Test[ACEG...]) drops
# any test whose character after "Test" is not a letter (Test_Underscore,
# Test1Digit), so the split must come from the `go test -list` roster. A fake go
# lies about the roster only: package discovery and existence checks delegate to
# the real go, so the authoritative coverage verification runs for real under
# these fake-roster cases too.
FAKE_BIN="$TMP/fakebin"
mkdir -p "$FAKE_BIN"
cat >"$FAKE_BIN/go" <<'FAKE_GO'
#!/usr/bin/env bash
if [ "${FAKE_ASSEMBLY_MULTI:-}" = 1 ] && [ "$#" -eq 2 ] && [ "$1" = list ] && [ "$2" = ./assembly/... ]; then
	printf '%s\n' example.com/fake/assembly example.com/fake/assembly/sub
	exit 0
fi
if [ "$#" -eq 4 ] && [ "$1" = test ] && [ "$2" = -list ] && [ "$3" = '^Test' ] && [ "$4" = ./assembly ]; then
	printf '%s\n' TestAlpha TestBeta Test1Digit Test_Underscore TestGamma TestDelta
	exit 0
fi
exec "${FAKE_REAL_GO:?FAKE_REAL_GO must point at the real go binary}" "$@"
FAKE_GO
chmod +x "$FAKE_BIN/go"
expected_roster=$(printf '%s\n' TestAlpha TestBeta Test1Digit Test_Underscore TestGamma TestDelta | LC_ALL=C sort)
for shard in 1 2 3; do
	mkdir -p "$TMP/roster/race-shard-$shard"
	out=$(PATH="$FAKE_BIN:$PATH" FAKE_REAL_GO="$REAL_GO" RACE_CHECK_CLASSIFY_ONLY=1 RACE_CHECK_FAKE_EXIT=0 \
		RACE_CHECK_PACKAGE_FILE="$TMP/roster/race-shard-$shard/packages.txt" \
		bash "$SCRIPT" --shard "$shard" <<<"ok" 2>&1)
	rc=$?
	if [ "$rc" -eq 0 ] && grep -q "OK" <<<"$out"; then
		echo "PASS: fake-roster shard $shard runs"
	else
		echo "FAIL: fake-roster shard $shard — exit=$rc"
		echo "$out" | tail -5 | sed 's/^/    /'
		FAILS=$((FAILS + 1))
	fi
done

# 30. Both split shards emit a non-empty roster beside their selector.
for shard in 1 2; do
	roster_dir="$TMP/roster/race-shard-$shard"
	want_selector=odd
	[ "$shard" -eq 2 ] && want_selector=even
	if [ -s "$roster_dir/assembly-run.txt" ] && [ "$(cat "$roster_dir/assembly-selector.txt" 2>/dev/null)" = "$want_selector" ]; then
		echo "PASS: shard $shard emits a non-empty $want_selector roster"
	else
		echo "FAIL: shard $shard roster artifact missing, empty or mislabelled"
		echo "    selector=$(cat "$roster_dir/assembly-selector.txt" 2>/dev/null) want=$want_selector"
		FAILS=$((FAILS + 1))
	fi
done

# 31. Every listed test lands in exactly one shard, including the names a letter
# class drops; the union is exactly the roster, with no repeats.
cat "$TMP/roster/race-shard-1/assembly-run.txt" "$TMP/roster/race-shard-2/assembly-run.txt" >"$TMP/roster.union"
split_ok=1
for name in Test_Underscore Test1Digit TestAlpha TestBeta; do
	one=$(grep -cx "$name" "$TMP/roster/race-shard-1/assembly-run.txt" 2>/dev/null)
	two=$(grep -cx "$name" "$TMP/roster/race-shard-2/assembly-run.txt" 2>/dev/null)
	seen=$(( ${one:-0} + ${two:-0} ))
	if [ "$seen" -ne 1 ]; then
		split_ok=0
		echo "    $name appears $seen time(s) across the two rosters"
	fi
done
if [ "$split_ok" -eq 1 ] \
	&& diff -u <(printf '%s\n' "$expected_roster") <(LC_ALL=C sort "$TMP/roster.union") >/dev/null \
	&& [ -z "$(LC_ALL=C sort "$TMP/roster.union" | uniq -d)" ]; then
	echo "PASS: Test_Underscore/Test1Digit/TestAlpha/TestBeta each split once, union exact"
else
	echo "FAIL: assembly roster split does not cover every listed test exactly once"
	diff -u <(printf '%s\n' "$expected_roster") <(LC_ALL=C sort "$TMP/roster.union") 2>&1 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 32. The aggregate accepts the matching rosters.
out=$(PATH="$FAKE_BIN:$PATH" FAKE_REAL_GO="$REAL_GO" bash "$SCRIPT" --shard-union-check "$TMP/roster" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && grep -q "assembly roster OK" <<<"$out"; then
	echo "PASS: aggregate accepts exact assembly rosters"
else
	echo "FAIL: aggregate rejected matching assembly rosters — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# 33. Missing, blank, repeated, dropped, extra and unlabelled rosters all fail
# closed. Each mutation is restored so the next case starts from a valid bundle.
roster_rejected() { # $1=label $2=expected message pattern
	local out rc
	out=$(PATH="$FAKE_BIN:$PATH" FAKE_REAL_GO="$REAL_GO" bash "$SCRIPT" --shard-union-check "$TMP/roster" 2>&1)
	rc=$?
	if [ "$rc" -ne 0 ] && grep -q "$2" <<<"$out"; then
		echo "PASS: aggregate rejects $1"
	else
		echo "FAIL: aggregate accepted $1 — exit=$rc (want: $2)"
		echo "$out" | tail -5 | sed 's/^/    /'
		FAILS=$((FAILS + 1))
	fi
}
cp "$TMP/roster/race-shard-1/assembly-run.txt" "$TMP/shard-1-roster.txt"
cp "$TMP/roster/race-shard-2/assembly-run.txt" "$TMP/shard-2-roster.txt"
cp "$TMP/roster/race-shard-1/assembly-selector.txt" "$TMP/shard-1-selector.txt"

mv "$TMP/roster/race-shard-1/assembly-run.txt" "$TMP/hold-roster.txt"
roster_rejected "a missing shard roster" "missing or empty assembly roster"
mv "$TMP/hold-roster.txt" "$TMP/roster/race-shard-1/assembly-run.txt"

printf '\n' >"$TMP/roster/race-shard-2/assembly-run.txt"
roster_rejected "a blank-only shard roster" "blank line in assembly roster"
cp "$TMP/shard-2-roster.txt" "$TMP/roster/race-shard-2/assembly-run.txt"

sed -n '1p' "$TMP/shard-1-roster.txt" >>"$TMP/roster/race-shard-1/assembly-run.txt"
roster_rejected "a name repeated inside one shard" "duplicate test name"
cp "$TMP/shard-1-roster.txt" "$TMP/roster/race-shard-1/assembly-run.txt"

sed -n '1p' "$TMP/shard-1-roster.txt" >>"$TMP/roster/race-shard-2/assembly-run.txt"
roster_rejected "a name repeated across shards" "duplicate test name"
cp "$TMP/shard-2-roster.txt" "$TMP/roster/race-shard-2/assembly-run.txt"

sed '1d' "$TMP/shard-1-roster.txt" >"$TMP/roster/race-shard-1/assembly-run.txt"
roster_rejected "a dropped name" "differs from the odd half"
cp "$TMP/shard-1-roster.txt" "$TMP/roster/race-shard-1/assembly-run.txt"

printf 'TestNotInTheAssemblyRoster\n' >>"$TMP/roster/race-shard-1/assembly-run.txt"
roster_rejected "an extra name the roster never listed" "differs from the odd half"
cp "$TMP/shard-1-roster.txt" "$TMP/roster/race-shard-1/assembly-run.txt"

rm -f "$TMP/roster/race-shard-1/assembly-selector.txt"
roster_rejected "a missing split selector" "assembly split selectors must be odd/even"
cp "$TMP/shard-1-selector.txt" "$TMP/roster/race-shard-1/assembly-selector.txt"

# C1. Authoritative coverage, real drift: deleting a first-party pattern from
# RACE_PACKAGES must turn the gate red before go test runs, naming the package
# that would otherwise escape race detection. The rest of the declaration and
# the real `go list ./...` are untouched, so this is the exact failure a new
# package directory produces when nobody updates the canonical set.
sed '/^[[:space:]]*\.\/api\/event\/\.\.\.$/d' "$SCRIPT" >"$TMP/race_check_uncovered.sh"
out=$(bash "$TMP/race_check_uncovered.sh" ./api/event 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "not covered by RACE_PACKAGES" <<<"$out" && grep -q "api/event" <<<"$out"; then
	echo "PASS: a first-party package missing from RACE_PACKAGES turns the gate red"
else
	echo "FAIL: uncovered first-party package accepted — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

# C2. The assembly roster split assumes `go list ./assembly/...` is exactly the
# single ./assembly package. A subpackage must fail closed with the update hint
# instead of silently running an unlisted package. Only that one go list call is
# faked; coverage otherwise still runs against the real module.
out=$(PATH="$FAKE_BIN:$PATH" FAKE_REAL_GO="$REAL_GO" FAKE_ASSEMBLY_MULTI=1 \
	bash "$SCRIPT" --shard-union-check 2>&1)
rc=$?
if [ "$rc" -ne 0 ] && grep -q "no longer expands to the single" <<<"$out"; then
	echo "PASS: assembly subpackages refuse the odd/even roster split"
else
	echo "FAIL: assembly subpackages accepted — exit=$rc"
	echo "$out" | tail -5 | sed 's/^/    /'
	FAILS=$((FAILS + 1))
fi

if [ "$FAILS" -gt 0 ]; then
	echo
	echo "FAILED: $FAILS case(s)"
	exit 1
fi
echo
echo "ALL PASS"
