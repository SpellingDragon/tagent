// Package violations plants one violation per policy rule.
package violations

// Section5 does work.
//
// §5.44 requires the single predicate here.
func Section5() {}

// WhyRationale explains a decision.
//
// 之所以选 A 而排除 B，是因为 B 有竞态。
func WhyRationale() {}

// Rounds carries an iteration marker from round 42 (2026-09-27).
func Rounds() {}

// Steps narrates the mechanism: 先取快照，再在 mu 锁内换入。
func Steps() {}

// Points at a change artifact: openspec/changes/foo/design.md
func Artifact() {}

// Cites docs path without index form: see docs/wiki/agent/agent-architecture.md
func BareCitation() {}

func Undocumented() {}

type UndocumentedType struct{}

// missing directive-ish exemption test
func Comment() {
	x := 1 // trailing explanation
	_ = x
}
