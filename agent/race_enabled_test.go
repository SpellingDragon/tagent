//go:build race

package agent

// raceEnabled is true under `-race` (implementation-hardening 8.1/8.2).
// Tests that trip KNOWN UPSTREAM trpc-agent-go internal races (LEDGER
// 红色耦合台账 U2/U3 — invocation/steer/session-service concurrency, not
// tagent code) skip themselves here so the agent package can join the race
// gate without masking regressions in tagent-owned code.
const raceEnabled = true
