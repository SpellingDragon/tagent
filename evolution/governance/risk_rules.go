package governance

import (
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// ToolRiskRule is one entry of the declared risk table: a tool name or a
// tool-name prefix beside the grade the default risk assessment renders for it.
// The entry is plain configuration — a name, the prefix marker and a level — and
// carries no match function of its own.
//
// A rule whose Prefix is set matches every tool whose name starts with the rule's
// Tool prefix, and a trailing "*" in Tool marks that prefix explicitly; a rule
// without Prefix matches the tool name exactly.
type ToolRiskRule struct {
	// Tool is the tool name, or the tool-name prefix when Prefix is set.
	Tool string
	// Prefix reports whether Tool names a prefix instead of an exact name.
	Prefix bool
	// Level is the grade the assessment renders for a matched tool.
	Level plugin.RiskLevel
}

// RiskRules is the declared risk table: the ordered entries one policy grades
// tool calls with. The table is the default risk assessment — it renders a grade
// only when the one Before pass carries none — and the selection prefers an
// exact tool name over the longest matching prefix, then the policy's default
// grade.
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
type RiskRules []ToolRiskRule

// WithRiskRules attaches the declared risk table the policy grades with when the
// one Before pass carries no grade. The entries are validated at construction:
// an empty tool name or an unknown grade refuses the policy by panicking, so a
// table that could grade nothing never starts as a gate.
func WithRiskRules(rules ...ToolRiskRule) PolicyOption {
	table := append(RiskRules(nil), rules...)
	return func(p *Policy) {
		if err := table.validate(); err != nil {
			panic(err)
		}
		p.riskRules = append(RiskRules(nil), table...)
	}
}

// grade reports the grade the table renders for one tool name: the level of the
// exact rule naming the tool, else the level of the longest prefix rule the tool
// starts with, else no grade. An exact rule wins over every prefix rule, and the
// first declared rule wins a tie between prefixes of one length.
func (rules RiskRules) grade(tool string) (plugin.RiskLevel, bool) {
	var longest *ToolRiskRule
	longestLen := 0
	for i := range rules {
		rule := &rules[i]
		if !rule.Prefix {
			if rule.Tool == tool {
				return rule.Level, true
			}
			continue
		}
		prefix := prefixName(rule.Tool)
		if !strings.HasPrefix(tool, prefix) {
			continue
		}
		if longest == nil || len(prefix) > longestLen {
			longest, longestLen = rule, len(prefix)
		}
	}
	if longest != nil {
		return longest.Level, true
	}
	return "", false
}

// validate reports the first entry that cannot grade a call: an entry with no
// tool name and an entry whose level is not a declared grade are both refused,
// so a broken table is reported where it is stated instead of grading nothing.
func (rules RiskRules) validate() error {
	for i, rule := range rules {
		if strings.TrimSpace(rule.Tool) == "" {
			return fmt.Errorf("governance: risk rule %d names no tool", i)
		}
		if rule.Level.Rank() == 0 {
			return fmt.Errorf("governance: risk rule %d (%s) carries the unknown risk grade %q", i, rule.Tool, rule.Level)
		}
	}
	return nil
}

// prefixName reports the tool-name prefix one prefix rule declares: a trailing
// "*" marks the prefix explicitly and is not part of the matched name.
func prefixName(tool string) string {
	return strings.TrimSuffix(tool, "*")
}
