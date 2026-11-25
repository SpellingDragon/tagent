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
#   - A declared pattern that expands to no Go package is skipped with a printed
#     note, because the module root carries no Go files; any OTHER go list failure
#     stays red, and a set left with no Go package at all is refused rather than
#     passing vacuously.
#   - The canonical set is checked against `go list ./...`: every first-party
#     package of the module must be covered, `./assembly/...` must still be the
#     single ./assembly package the odd/even roster split assumes, and both the
#     shard and the aggregate path run that check. A package that escapes the
#     declaration turns the gate red instead of running untested.
#   - Upstream-race waivers apply ONLY when ALL of: the run failed; every
#     DATA RACE report's top offending frame is inside a registered upstream
#     module (trpc-agent-go, trpc-mcp-go); the frame matches the registered
#     waiver list below; and no failure hides behind the races.
#   - A DATA RACE report whose conflicting-access blocks are absent or
#     unparsable fails closed: with no top frame the owner of the race is
#     unknown, and an unknown owner is never an upstream waiver. Every
#     `WARNING: DATA RACE` block is judged on its own frames: each block must
#     yield one COMPLETE top frame (2-space symbol line plus its 6-space path
#     line), so a frameless report is not compensated by a sibling race whose
#     frames do match a waiver.
#   - "No failure hides behind the races" is decided per (package, test) from
#     `go test -json`: a failing test is race-attributed only when its own
#     output stream — or a failing descendant's, since a failed subtest fails its
#     parent — carries DATA RACE or the parallel-sibling marker Go prints for
#     every test that was executing when the race fired (testing.checkRaces: the
#     stack dump lands in one sibling's log, each other sibling gets only
#     "race detected during execution of test"). A failing test without either
#     marker, a package that fails with no failing test, a build failure and a
#     panic all refuse the waiver; the marker alone is not a race report, so the
#     precheck still demands at least one real WARNING. Counting `--- FAIL:`
#     lines against race reports is deliberately gone from the real path: equal
#     counts cannot distinguish two races plus a plain failure from one race plus
#     a parent/child double FAIL.
#
# Registered upstream waivers — LEDGER 红色耦合台账:
#   U2  runner steer-queue close vs event consume (invocation/steer frames),
#       trpc-agent-go v1.10.0
#   U3  inmemory session service hook chain (session.go:122 stack family),
#       trpc-agent-go v1.10.0
#   U5  streamable HTTP client transport fields — concurrent send vs the GET
#       SSE goroutine (streamable_client.go family), trpc-mcp-go v0.0.10;
#       both the symbol line and the file path line need a signature, since
#       the transport symbol never carries its file name.
#   C3  timing flakiness is NOT waived here — the upstream race signatures
#       waived below are classified by name, not by a build-tag skip.
# Removal condition: upgrade past the upstream fix (trpc-agent-go, or
# trpc-mcp-go for U5), delete the signature AND the per-test waivers, rerun
# `go test ./runtime/agent/... -race` (and the MCP face test for U5).
#
# Usage:
#   ./scripts/gates/race_check.sh --shard <1..3>
#   ./scripts/gates/race_check.sh --shard-union-check [artifact-bundle]
#   ./scripts/gates/race_check.sh [packages...]
# No package arguments runs the canonical full set. With an artifact bundle,
# --shard-union-check compares the three uploaded package manifests with that set;
# without one it validates the static shard definitions and prints the sorted set.
# RACE_CHECK_PACKAGE_FILE names an optional output manifest for the selected set.
# The ./assembly/... entry is split across shard 1/2 by the `go test -list` roster
# (sorted positions alternate odd/even); those shards also publish
# assembly-selector.txt and assembly-run.txt beside the manifest, and
# --shard-union-check re-derives the roster to prove the split is exact.
# Test hooks: RACE_CHECK_CLASSIFY_ONLY=1 feeds a plain-text stream and
# RACE_CHECK_CLASSIFY_JSON_ONLY=1 feeds a `go test -json` event stream to their
# classifiers without running go test (both used by scripts/gates/
# test_race_check.sh); RACE_CHECK_FAKE_EXIT selects the simulated go test exit
# status. Coverage verification still runs before either hook.
set -uo pipefail

# Bash ERE fragments matched against each race's TOP frame (symbol AND file
# path lines both feed the match, since symbols join packages with dots).
RACE_WAIVER_SIGNATURES=(
	'trpc-agent-go.*steer'
	'trpc-agent-go.*invocation'
	'trpc-agent-go/session.*inmemory'
	'trpc-agent-go.*session/session\.go'
	# U5 — trpc-mcp-go streamable client transport setSessionID/send race.
	# v0.0.18 tried and rolled back 2026-10-28: same race plus a functional
	# regression (connection refused in mcp_face); stays on v0.0.10 until fixed.
	'trpc-mcp-go.*streamableHTTPClientTransport'
	'trpc-mcp-go.*streamable_client\.go'
)

# race_scan_frames <output> — classifies the TOP offending frame of every
# conflicting access: BOTH the symbol line (2-space indent) and its file path
# line (6-space indent) feed signature matching — upstream symbols join packages
# with dots, paths with slashes. A tagent-owned frame there means a race WE
# introduced. Each `WARNING: DATA RACE` block is scanned on its own and must
# yield at least one COMPLETE top frame (symbol line plus the path line below
# it): a block with none sets RACE_UNFRAMED, because a symbol that never got its
# path proves no more ownership than no frame at all. Sets RACE_TOPFRAMES (the
# frame lines deduplicated across all blocks), RACE_BLOCKS, RACE_UNFRAMED,
# RACE_TAGENT_OWNED and RACE_UNMATCHED.
race_scan_frames() {
	local out="$1" f sig matched kind block_index payload
	local blocks topframes
	local -a frames=()
	blocks=$(awk '
		function close_block() {
			if (block > 0) {
				printf "BLOCK\t%d\t%d\n", block, framed
			}
		}
		/WARNING: DATA RACE/ { close_block(); block++; framed=0; grab=0; sym=""; next }
		block == 0 { next }
		/^(Read|Write|Previous read|Previous write) at / { grab=1; sym=""; next }
		grab == 1 && /^  [^ ]/ { sym=$1; grab=2; next }
		grab == 2 && /^      / { printf "FRAME\t%d\t%s\n", block, sym; printf "FRAME\t%d\t%s\n", block, $0; framed++; grab=0; next }
		END { close_block() }
	' <<<"$out")
	RACE_BLOCKS=0
	RACE_UNFRAMED=0
	while IFS=$'\t' read -r kind block_index payload; do
		case "$kind" in
			BLOCK)
				RACE_BLOCKS=$((RACE_BLOCKS + 1))
				if [ "$payload" -eq 0 ]; then
					RACE_UNFRAMED=$((RACE_UNFRAMED + 1))
				fi
				;;
			FRAME)
				frames+=("$payload")
				;;
		esac
	done <<<"$blocks"
	topframes=''
	if [ "${#frames[@]}" -gt 0 ]; then
		topframes=$(printf '%s\n' "${frames[@]}" | sed '/^$/d' | sort -u)
	fi
	RACE_TOPFRAMES="$topframes"
	RACE_TAGENT_OWNED=0
	RACE_UNMATCHED=0
	while IFS= read -r f; do
		[ -z "$f" ] && continue
		if [[ "$f" == *"SpellingDragon/tagent"* ]]; then
			RACE_TAGENT_OWNED=$((RACE_TAGENT_OWNED + 1))
			continue
		fi
		matched=0
		for sig in "${RACE_WAIVER_SIGNATURES[@]}"; do
			if [[ "$f" =~ $sig ]]; then
				matched=1
				break
			fi
		done
		[ "$matched" -eq 0 ] && RACE_UNMATCHED=$((RACE_UNMATCHED + 1))
	done <<<"$topframes"
}

# race_check_precheck <go-test-exit-code> <output> — prints the full evidence
# and runs every check that does not depend on failure attribution. Returns
# 0 = the run passed, 1 = a red verdict is already printed, 2 = every DATA RACE
# frame matches a registered upstream waiver, so the caller must still prove
# that no plain failure hides behind the races. A block whose top frame cannot be
# parsed returns 1: an unknown owner cannot be waived, and every WARNING block
# must carry its own complete frame.
race_check_precheck() {
	local exit_code="$1" out="$2"
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
	# A build-failed package has no --- FAIL line, so a mixed run can have the
	# same FAIL/race counts as a pure waived race. Panics are independent of
	# race attribution too; neither may be hidden by an upstream waiver.
	if grep -Eq '\[build failed\]|^[[:space:]]*panic:' <<<"$out"; then
		echo "race_check: FAIL — non-race build failure or panic detected alongside DATA RACE reports; upstream waivers do not apply."
		return 1
	fi
	race_scan_frames "$out"
	# Every block is judged on its own frames: one frameless report (a WARNING
	# truncated before its access frames, or an access whose path line never
	# arrives) leaves that race's owner unknown even when a sibling block parsed
	# cleanly and matches a waiver. Both ownership counters are 0 in that state,
	# which used to fall through to the waiver on the sibling's frames; a missing
	# frame is never compensated by another block.
	if [ "$RACE_UNFRAMED" -gt 0 ]; then
		echo "race_check: FAIL — $RACE_UNFRAMED of $RACE_BLOCKS WARNING: DATA RACE block(s) carry no conflicting-access top frame: an unframed report is a missing frame (every block needs its 2-space symbol line plus the 6-space path line below it), and another block's frames cannot stand in for it, so the owner of that race is unknown and no waiver applies. Full output above."
		return 1
	fi
	if [ "$RACE_TAGENT_OWNED" -gt 0 ]; then
		echo "race_check: FAIL — tagent-owned racy access detected ($RACE_TAGENT_OWNED frame(s)):"
		grep -B 2 -A 30 "WARNING: DATA RACE" <<<"$out" || true
		return 1
	fi
	if [ "$RACE_UNMATCHED" -gt 0 ]; then
		echo "race_check: FAIL — upstream race(s) NOT covered by the registered waiver list. Register the signature in LEDGER 红色耦合台账 + RACE_WAIVER_SIGNATURES (or fix locally). Unmatched top frames:"
		printf '%s\n' "$RACE_TOPFRAMES" | sed 's/^/            /'
		return 1
	fi
	return 2
}

# race_check_classify <go-test-exit-code> — plain-text classifier, reachable
# only through RACE_CHECK_CLASSIFY_ONLY (the real path runs go test -json).
# Prints the output plus a verdict; returns the gate exit code.
race_check_classify() {
	local exit_code="$1"
	local out
	out=$(cat)
	race_check_precheck "$exit_code" "$out"
	case "$?" in
		0) return 0 ;;
		1) return 1 ;;
	esac
	# A plain-text stream has no (package, test) streams, so this hook keeps the
	# historical count comparison. The REAL path must never rely on it: equal
	# counts cannot distinguish two races plus a plain failure from one race plus
	# a parent/child double FAIL, and it attributes nothing by package.
	local races fails package_fails
	races=$(grep -c "WARNING: DATA RACE" <<<"$out" || true)
	fails=$(grep -c -- "--- FAIL:" <<<"$out" || true)
	package_fails=$(grep -cE '^FAIL[[:space:]]+[^[:space:]]+' <<<"$out" || true)
	if [ "${fails:-0}" -gt "${races:-0}" ] || [ "${package_fails:-0}" -gt "${races:-0}" ]; then
		echo "race_check: FAIL — ${fails} failing test(s), ${package_fails} failed package(s) vs ${races} race report(s): at least one plain (non-race) failure; upstream waivers do not apply."
		return 1
	fi
	echo "race_check: WAIVED — ${races} race(s), all top frames match registered upstream waivers (LEDGER U2/U3/U5: trpc-agent-go v1.10.0, trpc-mcp-go v0.0.10). Track for upstream fix:"
	printf '%s\n' "$RACE_TOPFRAMES" | sed 's/^/            /'
	return 0
}

# race_decode_events <json-file> <attribution-file> <go-test-exit-code> —
# reconstructs the human go test output from the `-json` event stream on stdout
# and writes the (package, test) attribution to the second file. A (package,
# test) carries race evidence when its own stream holds a DATA RACE report or
# the parallel-sibling marker Go prints for every test caught by one race.
# A passing run
# keeps the package status lines only (the frames of passing tests are not
# evidence and go test -json would otherwise turn every green shard verbose); a
# failing run prints every event. An unreadable event or a non-JSON line (other
# than go's own download chatter) is recorded as a plain failure instead of
# being dropped: failure evidence may not shrink silently.
race_decode_events() {
	python3 - "$1" "$2" "$3" <<'PY'
import json
import sys

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

json_path, attribution_path, exit_code = sys.argv[1], sys.argv[2], int(sys.argv[3])
verbose = exit_code != 0

chunks = []
failing = set()
self_race = set()
sibling_race = set()
package_fail = set()
build_failures = set()
undecodable = []
race_count = 0
sibling_marker_count = 0
BENIGN_PREFIXES = ("go: downloading ",)
RACE_WARNING = "WARNING: DATA RACE"
# Go marks EVERY test that was executing when a race fired: the WARNING stack
# dump lands in one sibling's log and each other parallel sibling gets only this
# line (testing.checkRaces), then fails. Both are race evidence.
RACE_SIBLING_MARKER = "race detected during execution of test"

with open(json_path, "r", encoding="utf-8", errors="replace") as handle:
    for raw in handle:
        line = raw.rstrip("\n")
        if not line.startswith("{"):
            chunks.append(line + "\n")
            if not line.startswith(BENIGN_PREFIXES):
                undecodable.append(line)
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            chunks.append(line + "\n")
            undecodable.append(line)
            continue
        output = event.get("Output")
        action = event.get("Action") or ""
        package = event.get("Package") or ""
        test = event.get("Test") or ""
        if output and (verbose or not test):
            chunks.append(output)
        if action == "output" and output:
            if RACE_WARNING in output:
                self_race.add((package, test))
                race_count += output.count(RACE_WARNING)
            if RACE_SIBLING_MARKER in output:
                sibling_race.add((package, test))
                sibling_marker_count += output.count(RACE_SIBLING_MARKER)
        if action == "fail":
            if test:
                failing.add((package, test))
            elif package:
                package_fail.add(package)
        if action == "build-fail" or event.get("FailedBuild"):
            build_failures.add(event.get("ImportPath") or package or "<unknown>")

sys.stdout.write("".join(chunks))


def attributed(package, test):
    if (package, test) in self_race or (package, test) in sibling_race:
        return True
    prefix = test + "/"
    return any(
        raced_package == package and raced_test.startswith(prefix)
        for (raced_package, raced_test) in self_race | sibling_race
    )


details = []
for (package, test) in sorted(failing):
    if not attributed(package, test):
        details.append(f"failing test without DATA RACE in its output: {package} {test}")
for package in sorted(package_fail):
    if not any(failing_package == package for (failing_package, _) in failing):
        details.append(f"package failed without any failing test: {package}")
for path in sorted(build_failures):
    details.append(f"build failure: {path}")
for line in undecodable:
    details.append(f"undecodable go test -json line: {line}")

with open(attribution_path, "w", encoding="utf-8") as handle:
    handle.write("verdict=" + ("plain" if details else "ok") + "\n")
    handle.write(f"races={race_count}\n")
    handle.write(f"sibling_markers={sibling_marker_count}\n")
    handle.write(f"failing_tests={len(failing)}\n")
    handle.write(f"package_failures={len(package_fail)}\n")
    for detail in details:
        handle.write(f"detail={detail}\n")
PY
}

# race_check_classify_json <go-test-exit-code> <json-file> — the real path's
# classifier. The shared precheck owns the evidence, the no-race/panic/build
# scan and the TOP-frame waiver signatures; attribution for a run whose frames
# are all waived comes from the -json event streams only.
race_check_classify_json() {
	local exit_code="$1" json_file="$2"
	local attribution="$json_file.attribution"
	local decoded verdict races failing markers
	if ! decoded=$(race_decode_events "$json_file" "$attribution" "$exit_code"); then
		echo "race_check: FAIL — cannot decode the go test -json event stream: $json_file"
		return 1
	fi
	race_check_precheck "$exit_code" "$decoded"
	case "$?" in
		0) return 0 ;;
		1) return 1 ;;
	esac
	verdict=$(sed -n '1s/^verdict=//p' "$attribution" 2>/dev/null)
	races=$(sed -n 's/^races=//p' "$attribution" 2>/dev/null | head -1)
	failing=$(sed -n 's/^failing_tests=//p' "$attribution" 2>/dev/null | head -1)
	markers=$(sed -n 's/^sibling_markers=//p' "$attribution" 2>/dev/null | head -1)
	if [ "$verdict" != "ok" ]; then
		echo "race_check: FAIL — ${races:-0} race report(s) but at least one failure is not attributed to a DATA RACE; upstream waivers do not apply."
		sed -n 's/^detail=/            /p' "$attribution" 2>/dev/null
		return 1
	fi
	echo "race_check: WAIVED — ${races:-0} race(s) across ${failing:-0} failing test(s) (${markers:-0} parallel-sibling race marker(s)), every failure attributed to a DATA RACE whose top frames match registered upstream waivers (LEDGER U2/U3/U5: trpc-agent-go v1.10.0, trpc-mcp-go v0.0.10). Track for upstream fix:"
	printf '%s\n' "$RACE_TOPFRAMES" | sed 's/^/            /'
	return 0
}

# Canonical package set. The full set is declared only here; shards contain
# indexes into this array, so adding or moving a package cannot create a second
# list that silently drifts from the full set. race_verify_coverage compares the
# expansion against `go list ./...`, so a package directory that nobody added
# here turns the gate red instead of running outside race detection.
RACE_PACKAGES=(
	.
	./assembly/...
	./cli/...
	./api/durable/...
	./api/event/...
	./api/config/...
	./api/document/...
	./api/model/...
	./api/view/...
	./api/plugin/...
	./runtime/agent/...
	./runtime/task/...
	./runtime/state/...
	./memory/...
	./features/...
	./tool/...
	./bench/...
	./internal/...
	./tests/integration/execution/...
	./tests/integration/history/...
	./tests/integration/host/...
	./tests/integration/capabilities/...
	./tests/integration/evolution/...
	./tests/integration/learning/...
	./tests/integration
	./tests/integration/acceptance/...
	./tests/integration/config/...
	./tests/integration/hotupdate/...
	./tests/integration/inspection/...
	./tests/integration/journey/...
	./tests/integration/state/...
	./examples/contract/...
	./scripts/...
)
RACE_SHARDS=(
	'1 10 11 12 13 24 26 27 31 32'
	'1 15 18 19 25 28 29 30'
	'0 2 3 4 5 6 7 8 9 14 16 17 20 21 22 23'
)

race_usage() {
	echo "usage: race_check.sh --shard <1..3> | --shard-union-check [artifact-bundle] | [packages...]" >&2
}

race_shard_definitions() {
	local expected actual actual_unique duplicates missing extra shard_text index candidate count
	local -a indexes=() union=()
	if [ "${#RACE_SHARDS[@]}" -ne 3 ]; then
		echo "race_check: FAIL — expected exactly 3 shard definitions, found ${#RACE_SHARDS[@]}" >&2
		return 1
	fi
	for shard_text in "${RACE_SHARDS[@]}"; do
		read -r -a indexes <<<"$shard_text"
		if [ "${#indexes[@]}" -eq 0 ]; then
			echo "race_check: FAIL — a shard definition is empty" >&2
			return 1
		fi
		for index in "${indexes[@]}"; do
			if [[ ! "$index" =~ ^(0|[1-9][0-9]*)$ ]] || [ "$index" -ge "${#RACE_PACKAGES[@]}" ]; then
				echo "race_check: FAIL — shard index $index is outside the canonical package set" >&2
				return 1
			fi
			union+=("${RACE_PACKAGES[$index]}")
		done
	done
	for ((index=0; index<${#RACE_PACKAGES[@]}; index++)); do
		count=0
		for shard_text in "${RACE_SHARDS[@]}"; do
			read -r -a indexes <<<"$shard_text"
			for candidate in "${indexes[@]}"; do
				if [ "$candidate" = "$index" ]; then
					count=$((count + 1))
				fi
			done
		done
		if [ "$count" -gt 1 ] && { [ "$index" != "1" ] || [ "$count" -ne 2 ]; }; then
			echo "race_check: FAIL — duplicate shard index $index; only assembly may be split once" >&2
			return 1
		fi
	done
	expected=$(printf '%s\n' "${RACE_PACKAGES[@]}" | LC_ALL=C sort)
	actual_unique=$(printf '%s\n' "${union[@]}" | LC_ALL=C sort -u)
	duplicates=$(uniq -d <<<"$(printf '%s\n' "${union[@]}" | LC_ALL=C sort)" | grep -v '^./assembly/\.\.\.$' || true)
	missing=$(LC_ALL=C comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique"))
	extra=$(LC_ALL=C comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique"))
	if [ -n "$duplicates" ] || [ -n "$missing" ] || [ -n "$extra" ] || ! diff -q <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique") >/dev/null; then
		echo "race_check: FAIL — shard union differs from the canonical package set" >&2
		[ -n "$duplicates" ] && printf 'duplicate package(s):\n%s\n' "$duplicates" >&2
		[ -n "$missing" ] && printf 'missing package(s):\n%s\n' "$missing" >&2
		[ -n "$extra" ] && printf 'unexpected package(s):\n%s\n' "$extra" >&2
		return 1
	fi
}

# RACE_COVERAGE_ERR is the shared stderr sink of race_coverage_go_list; the
# coverage verifier owns its lifetime.
RACE_COVERAGE_ERR=''

# race_coverage_go_list <description> [go list args...] — prints the package
# list on stdout; a go list failure is reported with the captured stderr.
race_coverage_go_list() {
	local description="$1"
	shift
	local output
	if ! output=$(go list "$@" 2>"$RACE_COVERAGE_ERR"); then
		echo "race_check: FAIL — go list $description: $(cat "$RACE_COVERAGE_ERR")" >&2
		return 1
	fi
	printf '%s\n' "$output"
}

# race_verify_coverage — the canonical declaration must equal `go list ./...`
# for this module, and the ./assembly/... entry must still expand to the single
# ./assembly package the odd/even roster split assumes. Both the shard path and
# the aggregate path call this, and the classifier-only hooks do not bypass it:
# a first-party package that escapes RACE_PACKAGES would otherwise run with no
# race evidence at all. `.` is allowed to match no Go package (the module root
# carries no Go files); every other dead or misspelt pattern is red.
race_verify_coverage() {
	local err_file rc=0 assembly_base assembly_expanded declared_raw root_package
	local authoritative declared missing extra
	local -a patterns=()
	local package
	for package in "${RACE_PACKAGES[@]}"; do
		[ "$package" = "." ] || patterns+=("$package")
	done
	if [ "${#patterns[@]}" -eq 0 ]; then
		echo "race_check: FAIL — the canonical package set declares no patterns" >&2
		return 1
	fi
	err_file=$(mktemp) || {
		echo "race_check: FAIL — cannot create a temporary file for go list" >&2
		return 1
	}
	RACE_COVERAGE_ERR="$err_file"
	if ! assembly_base=$(race_coverage_go_list "./assembly" ./assembly); then
		rc=1
	elif ! assembly_expanded=$(race_coverage_go_list "./assembly/..." ./assembly/...); then
		rc=1
	elif [ "$assembly_expanded" != "$assembly_base" ]; then
		echo "race_check: FAIL — the ./assembly/... entry no longer expands to the single $assembly_base package; the odd/even assembly roster split only supports that one package. Update the shard/roster split before landing the subpackages:" >&2
		printf '%s\n' "$assembly_expanded" | sed 's/^/            /' >&2
		rc=1
	elif ! declared_raw=$(race_coverage_go_list "the canonical patterns" "${patterns[@]}"); then
		rc=1
	elif grep -q 'matched no packages' "$err_file"; then
		echo "race_check: FAIL — a canonical package pattern matches no package: $(grep 'matched no packages' "$err_file")" >&2
		rc=1
	else
		if root_package=$(go list . 2>"$err_file"); then
			declared_raw=$(printf '%s\n%s\n' "$declared_raw" "$root_package")
		elif ! grep -q "no Go files in " "$err_file"; then
			echo "race_check: FAIL — go list .: $(cat "$err_file")" >&2
			rc=1
		fi
	fi
	if [ "$rc" -eq 0 ] && ! authoritative=$(race_coverage_go_list "./..." ./...); then
		rc=1
	fi
	if [ "$rc" -eq 0 ]; then
		declared=$(printf '%s\n' "$declared_raw" | sed '/^$/d' | LC_ALL=C sort -u)
		authoritative=$(printf '%s\n' "$authoritative" | sed '/^$/d' | LC_ALL=C sort -u)
		missing=$(LC_ALL=C comm -23 <(printf '%s\n' "$authoritative") <(printf '%s\n' "$declared"))
		extra=$(LC_ALL=C comm -13 <(printf '%s\n' "$authoritative") <(printf '%s\n' "$declared"))
		if [ -n "$missing" ]; then
			echo "race_check: FAIL — $(printf '%s\n' "$missing" | grep -c .) first-party package(s) from go list ./... are not covered by RACE_PACKAGES; register them in the canonical set and a shard so no package escapes race detection:" >&2
			printf '%s\n' "$missing" | sed 's/^/            /' >&2
			rc=1
		elif [ -n "$extra" ]; then
			echo "race_check: FAIL — RACE_PACKAGES expands to package(s) that go list ./... does not report; remove the stale entries:" >&2
			printf '%s\n' "$extra" | sed 's/^/            /' >&2
			rc=1
		fi
	fi
	rm -f "$err_file"
	RACE_COVERAGE_ERR=''
	if [ "$rc" -eq 0 ]; then
		echo "race_check: coverage OK (${#RACE_PACKAGES[@]} declared patterns cover $(printf '%s\n' "$authoritative" | grep -c .) first-party packages)" >&2
	fi
	return "$rc"
}

# The assembly package is the only set split across two shards, and the split is
# driven by the test roster rather than by name classes. A first-letter class
# (^Test[ACEGIKMOQSUWY]) has no bucket for a name whose character after "Test"
# is not a letter, so a Test_Underscore or Test1Digit would match neither half
# and never run. `go test -list` is authoritative for the roster: the sorted list
# alternates between shard 1 (odd positions) and shard 2 (even positions), and
# the selected half becomes the exact `-run` alternation, so the executed tests
# are exactly the listed ones. Shards publish the roster so the aggregate can
# re-derive it.
ASSEMBLY_LIST_PACKAGE='./assembly'
ASSEMBLY_ALL=()
ASSEMBLY_ODD=()
ASSEMBLY_EVEN=()

# race_assembly_plan fills ASSEMBLY_ALL/ODD/EVEN from `go test -list` and sorts
# them. Every failure mode fails closed: go itself failing, an answer with no
# tests, a roster too small to split, or a name carrying regex metacharacters
# (which would corrupt the -run alternation).
race_assembly_plan() {
	local listing names name position
	local -a all=() odd=() even=()
	ASSEMBLY_ALL=()
	ASSEMBLY_ODD=()
	ASSEMBLY_EVEN=()
	if ! listing=$(go test -list '^Test' "$ASSEMBLY_LIST_PACKAGE" 2>&1); then
		echo "race_check: FAIL — go test -list $ASSEMBLY_LIST_PACKAGE: $listing" >&2
		return 1
	fi
	if ! names=$(printf '%s\n' "$listing" | grep -E '^Test' | LC_ALL=C sort); then
		echo "race_check: FAIL — go test -list $ASSEMBLY_LIST_PACKAGE reported no tests" >&2
		return 1
	fi
	position=0
	while IFS= read -r name; do
		if [[ ! "$name" =~ ^Test[A-Za-z0-9_]+$ ]]; then
			echo "race_check: FAIL — go test -list emitted an unexpected test name (regex metacharacters would corrupt the -run alternation): $name" >&2
			return 1
		fi
		all+=("$name")
		position=$((position + 1))
		if [ $((position % 2)) -eq 1 ]; then
			odd+=("$name")
		else
			even+=("$name")
		fi
	done <<<"$names"
	if [ "${#odd[@]}" -eq 0 ] || [ "${#even[@]}" -eq 0 ]; then
		echo "race_check: FAIL — $ASSEMBLY_LIST_PACKAGE lists fewer than two tests; the two-shard split would leave one shard empty" >&2
		return 1
	fi
	ASSEMBLY_ALL=("${all[@]}")
	ASSEMBLY_ODD=("${odd[@]}")
	ASSEMBLY_EVEN=("${even[@]}")
}

# race_assembly_run_regex joins the selected half into one anchored alternation
# for `go test -run`. Names are bare Go identifiers (checked by the plan), so no
# escaping is needed.
race_assembly_run_regex() {
	local joined='' name
	for name in "$@"; do
		joined+="${joined:+|}${name}"
	done
	printf '^(?:%s)$' "$joined"
}

# race_artifact_assembly re-derives the roster with the same `go test -list`
# call the shards used, then proves the two published rosters are exactly the
# odd/even halves: nothing missing, nothing extra, nothing twice. Missing,
# blank and duplicated artifacts fail closed instead of passing vacuously.
race_artifact_assembly() {
	local bundle="$1" roster name duplicates
	local -a rosters=() actual=()
	rosters=("$bundle/race-shard-1/assembly-run.txt" "$bundle/race-shard-2/assembly-run.txt")
	for roster in "${rosters[@]}"; do
		if [ ! -s "$roster" ]; then
			echo "race_check: FAIL — missing or empty assembly roster: $roster" >&2
			return 1
		fi
		while IFS= read -r name || [ -n "$name" ]; do
			if [ -z "$name" ]; then
				echo "race_check: FAIL — blank line in assembly roster: $roster" >&2
				return 1
			fi
			actual+=("$name")
		done <"$roster"
	done
	duplicates=$(printf '%s\n' "${actual[@]}" | LC_ALL=C sort | uniq -d)
	if [ -n "$duplicates" ]; then
		echo "race_check: FAIL — duplicate test name(s) across the assembly rosters:" >&2
		printf '%s\n' "$duplicates" | sed 's/^/            /' >&2
		return 1
	fi
	race_assembly_plan || return 1
	local expected_odd expected_even expected_all union
	expected_odd=$(printf '%s\n' "${ASSEMBLY_ODD[@]}")
	expected_even=$(printf '%s\n' "${ASSEMBLY_EVEN[@]}")
	expected_all=$(printf '%s\n' "${ASSEMBLY_ALL[@]}")
	if ! diff -q <(printf '%s\n' "$expected_odd") <(LC_ALL=C sort "${rosters[0]}") >/dev/null; then
		echo "race_check: FAIL — shard 1 assembly roster differs from the odd half of go test -list:" >&2
		diff -u <(printf '%s\n' "$expected_odd") <(LC_ALL=C sort "${rosters[0]}") | sed 's/^/            /' >&2 || true
		return 1
	fi
	if ! diff -q <(printf '%s\n' "$expected_even") <(LC_ALL=C sort "${rosters[1]}") >/dev/null; then
		echo "race_check: FAIL — shard 2 assembly roster differs from the even half of go test -list:" >&2
		diff -u <(printf '%s\n' "$expected_even") <(LC_ALL=C sort "${rosters[1]}") | sed 's/^/            /' >&2 || true
		return 1
	fi
	union=$(printf '%s\n' "${actual[@]}" | LC_ALL=C sort -u)
	if ! diff -q <(printf '%s\n' "$expected_all") <(printf '%s\n' "$union") >/dev/null; then
		echo "race_check: FAIL — the two assembly rosters do not union to the full go test -list set:" >&2
		diff -u <(printf '%s\n' "$expected_all") <(printf '%s\n' "$union") | sed 's/^/            /' >&2 || true
		return 1
	fi
	echo "race_check: assembly roster OK (${#ASSEMBLY_ALL[@]} tests, odd/even exact and disjoint)"
}

race_artifact_union() {
	local bundle="$1" shard manifest package
	local -a manifests=() actual=()
	for shard in 1 2 3; do
		manifest="$bundle/race-shard-$shard/packages.txt"
		if [ ! -s "$manifest" ]; then
			echo "race_check: FAIL — missing or empty shard package manifest: $manifest" >&2
			return 1
		fi
		manifests+=("$manifest")
	done
	for manifest in "${manifests[@]}"; do
		local before=${#actual[@]}
		while IFS= read -r package || [ -n "$package" ]; do
			[ -n "$package" ] && actual+=("$package")
		done <"$manifest"
		if [ "${#actual[@]}" -eq "$before" ]; then
			echo "race_check: FAIL — shard package manifest contains no package entries: $manifest" >&2
			return 1
		fi
	done
	if [ "${#actual[@]}" -eq 0 ]; then
		echo "race_check: FAIL — uploaded shard manifests contain no package entries" >&2
		return 1
	fi
	local expected actual_sorted actual_unique duplicates missing extra selector_one selector_two
	expected=$(printf '%s\n' "${RACE_PACKAGES[@]}" | LC_ALL=C sort)
	actual_sorted=$(printf '%s\n' "${actual[@]}" | LC_ALL=C sort)
	actual_unique=$(printf '%s\n' "${actual[@]}" | LC_ALL=C sort -u)
	duplicates=$(uniq -d <<<"$actual_sorted" | grep -v '^./assembly/\.\.\.$' || true)
	selector_one=$(cat "$bundle/race-shard-1/assembly-selector.txt" 2>/dev/null || true)
	selector_two=$(cat "$bundle/race-shard-2/assembly-selector.txt" 2>/dev/null || true)
	if [ "$selector_one" != "odd" ] || [ "$selector_two" != "even" ]; then
		echo "race_check: FAIL — assembly split selectors must be odd/even in shard 1/2" >&2
		return 1
	fi
	missing=$(LC_ALL=C comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique"))
	extra=$(LC_ALL=C comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique"))
	if [ -n "$duplicates" ] || [ -n "$missing" ] || [ -n "$extra" ] || ! diff -q <(printf '%s\n' "$expected") <(printf '%s\n' "$actual_unique") >/dev/null; then
		echo "race_check: FAIL — uploaded shard union differs from the canonical package set" >&2
		[ -n "$duplicates" ] && printf 'duplicate package(s):\n%s\n' "$duplicates" >&2
		[ -n "$missing" ] && printf 'missing package(s):\n%s\n' "$missing" >&2
		[ -n "$extra" ] && printf 'unexpected package(s):\n%s\n' "$extra" >&2
		return 1
	fi
	race_artifact_assembly "$bundle" || return 1
	echo "race_check: shard artifact union OK (${#RACE_PACKAGES[@]} packages across 3 shards)"
}

shard=''
union_check=0
pkgs=()
while [ "$#" -gt 0 ]; do
	case "$1" in
		--shard)
			if [ "$#" -lt 2 ] || [[ ! "$2" =~ ^[1-3]$ ]] || [ -n "$shard" ] || [ "$union_check" -ne 0 ]; then
				echo "race_check: FAIL -- --shard requires exactly one value in 1..3" >&2
				race_usage
				exit 2
			fi
			shard="$2"
			shift 2
			;;
		--shard-union-check)
			if [ "$union_check" -ne 0 ] || [ -n "$shard" ]; then
				echo "race_check: FAIL -- --shard-union-check cannot be combined with --shard or repeated" >&2
				race_usage
				exit 2
			fi
			union_check=1
			shift
			;;
		--)
			shift
			pkgs+=("$@")
			break
			;;
		-*)
			echo "race_check: FAIL — unknown option: $1" >&2
			race_usage
			exit 2
			;;
		*)
			pkgs+=("$1")
			shift
			;;
	esac
done

if [ "$union_check" -ne 0 ]; then
	if [ "${#pkgs[@]}" -gt 1 ]; then
		race_usage
		exit 2
	fi
	race_shard_definitions || exit 1
	race_verify_coverage || exit 1
	if [ "${#pkgs[@]}" -eq 1 ]; then
		race_artifact_union "${pkgs[0]}"
	else
		printf '%s\n' "${RACE_PACKAGES[@]}" | LC_ALL=C sort
	fi
	exit $?
fi

if [ -n "$shard" ]; then
	if [ "${#pkgs[@]}" -ne 0 ]; then
		echo "race_check: FAIL -- --shard selects the canonical set and cannot take package arguments" >&2
		race_usage
		exit 2
	fi
	race_shard_definitions || exit 1
	read -r -a pkgs <<<"${RACE_SHARDS[$((shard - 1))]}"
	indexes=()
	for index in "${pkgs[@]}"; do
		indexes+=("${RACE_PACKAGES[$index]}")
	done
	pkgs=("${indexes[@]}")
elif [ "${#pkgs[@]}" -eq 0 ]; then
	pkgs=("${RACE_PACKAGES[@]}")
fi

# Coverage is verified after selection and before any hook or go test, so a
# shard, an aggregate and the classifier-only self-tests all prove the same
# first-party-package invariant.
race_verify_coverage || exit 1

if [ -n "${RACE_CHECK_PACKAGE_FILE:-}" ]; then
	if ! printf '%s\n' "${pkgs[@]}" >"$RACE_CHECK_PACKAGE_FILE"; then
		echo "race_check: FAIL — cannot write package manifest: $RACE_CHECK_PACKAGE_FILE" >&2
		exit 1
	fi
fi

# The assembly package is stripped from the package list and run separately with
# the roster alternation. The plan runs before the classifier-only hook below, so
# that hook still publishes the roster artifact the aggregate checks.
assembly_pattern='./assembly/...'
assembly_selector=''
assembly_run=''
if [ -n "$shard" ]; then
	kept=()
	assembly_found=0
	for pattern in "${pkgs[@]}"; do
		if [ "$pattern" = "$assembly_pattern" ]; then
			assembly_found=1
			continue
		fi
		kept+=("$pattern")
	done
	if [ "$assembly_found" -eq 1 ]; then
		case "$shard" in
			1) assembly_selector='odd' ;;
			2) assembly_selector='even' ;;
			*) echo "race_check: FAIL — assembly split is only valid in shard 1/2" >&2; exit 1 ;;
		esac
		race_assembly_plan || exit 1
		if [ "$assembly_selector" = 'odd' ]; then
			selected=("${ASSEMBLY_ODD[@]}")
		else
			selected=("${ASSEMBLY_EVEN[@]}")
		fi
		assembly_run=$(race_assembly_run_regex "${selected[@]}")
		if [ -n "${RACE_CHECK_PACKAGE_FILE:-}" ]; then
			artifact_dir=$(dirname "$RACE_CHECK_PACKAGE_FILE")
			if ! printf '%s\n' "$assembly_selector" >"$artifact_dir/assembly-selector.txt" ||
				! printf '%s\n' "${selected[@]}" >"$artifact_dir/assembly-run.txt"; then
				echo "race_check: FAIL — cannot write the assembly roster: $artifact_dir" >&2
				exit 1
			fi
		fi
	fi
	pkgs=("${kept[@]}")
fi

if [ -n "${RACE_CHECK_CLASSIFY_JSON_ONLY:-}" ]; then
	json_input=$(mktemp) || {
		echo "race_check: FAIL — cannot create a temporary file for the -json events" >&2
		exit 1
	}
	cat >"$json_input"
	race_check_classify_json "${RACE_CHECK_FAKE_EXIT:-1}" "$json_input"
	rc=$?
	rm -f "$json_input" "$json_input.attribution"
	exit "$rc"
fi

if [ -n "${RACE_CHECK_CLASSIFY_ONLY:-}" ]; then
	race_check_classify "${RACE_CHECK_FAKE_EXIT:-1}"
	exit $?
fi

# race_keep_packages drops the patterns that expand to no Go package and keeps the
# rest, in the declared order. The root-module entry of the canonical set is the case
# this exists for: the module root itself carries no Go files (the composition root
# is its own directory), and `go test -race .` answers `[setup failed]` beside
# "no Go files", which the classifier could only report as a plain failure. The
# skip is NARROWER than "go list failed": only the exact no-Go-files answer is
# skipped, so a misspelt or missing package still turns the gate red, and a set
# that keeps no package at all is refused instead of passing vacuously.
race_keep_packages() {
	local pattern out
	local kept=()
	for pattern in "$@"; do
		if out=$(go list "$pattern" 2>&1); then
			kept+=("$pattern")
			continue
		fi
		if [[ "$out" == *"no Go files in "* ]]; then
			echo "race_check: skip $pattern (the directory carries no Go files)" >&2
			continue
		fi
		echo "race_check: FAIL — go list $pattern: $out" >&2
		return 1
	done
	if [ ${#kept[@]} -eq 0 ]; then
		echo "race_check: FAIL — no package in the declared set carries Go files, so the gate would prove nothing" >&2
		return 1
	fi
	pkgs=("${kept[@]}")
}

race_keep_packages "${pkgs[@]}" || exit 1

# -p 1 keeps PACKAGE binaries from running at the same time: the tmux-backed
# suites share the machine's default tmux server, and their setup harvests orphan
# sessions, so concurrent packages destroy each other's live sessions. The failure
# shape is a vanished session (kill reports exit status 1, status "error", empty
# output) rather than a timeout, and CPU pressure alone can push a run over the
# edge. Package-level serialisation is the documented discipline for this family and
# cannot hide a data race: each package's own tests still run concurrently inside
# one binary, and packages never share a process.
# The real run consumes `go test -json` so the classifier can attribute every
# failure to its (package, test) output stream instead of comparing totals.
# race_check_classify_json reconstructs and prints the test output, so a failing
# run keeps the full evidence.
json_log=$(mktemp) || {
	echo "race_check: FAIL — cannot create the go test -json log" >&2
	exit 1
}
go test -race -count=1 -p 1 -json "${pkgs[@]}" >"$json_log" 2>&1
rc=$?
if [ -n "$assembly_selector" ]; then
	# The run names the same package the roster was listed from: a package the
	# roster never saw cannot slip in, and a listed test cannot be skipped.
	go test -race -count=1 -p 1 -json "$ASSEMBLY_LIST_PACKAGE" -run "$assembly_run" >>"$json_log" 2>&1
	assembly_rc=$?
	if [ "$rc" -eq 0 ]; then
		rc=$assembly_rc
	fi
fi
race_check_classify_json "$rc" "$json_log"
classify_rc=$?
rm -f "$json_log" "$json_log.attribution"
exit "$classify_rc"
