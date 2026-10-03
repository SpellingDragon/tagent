// 契约: docs/wiki/agent/agent-architecture.md#core-components
package tagent

import (
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// FoldModelRefAliases folds each agent's deprecated flat compress.summary_*
// knobs into its unified ModelRef holders: an explicit ModelRef field wins per
// field, and a flat knob only fills what the ModelRef leaves unset. Every fold
// logs a warning naming both configuration keys, so a mixed declaration stays
// visible in the startup log instead of being silently resolved.
func (c *Config) FoldModelRefAliases() {
	for name, ac := range c.Agents {
		changed := false
		if ac.Compress.SummaryModel != "" && ac.Compress.Summary.Model == "" {
			ac.Compress.Summary.Model = ac.Compress.SummaryModel
			ac.Compress.SummaryModel = ""
			changed = true
			log.Warnf("[tagent] agent %q: compress.summary_model is deprecated, folded into compress.summary.model=%q", name, ac.Compress.Summary.Model)
		}
		if ac.Compress.SummaryProvider != "" && ac.Compress.Summary.Provider == "" {
			ac.Compress.Summary.Provider = ac.Compress.SummaryProvider
			ac.Compress.SummaryProvider = ""
			changed = true
			log.Warnf("[tagent] agent %q: compress.summary_provider is deprecated, folded into compress.summary.provider=%q", name, ac.Compress.Summary.Provider)
		}
		if ac.Compress.SummaryEffort != "" && ac.Compress.Summary.ReasoningEffort == nil {
			effort := ac.Compress.SummaryEffort
			ac.Compress.Summary.ReasoningEffort = &effort
			ac.Compress.SummaryEffort = ""
			changed = true
			log.Warnf("[tagent] agent %q: compress.summary_effort is deprecated, folded into compress.summary.reasoning_effort=%q", name, effort)
		}
		if changed {
			c.Agents[name] = ac
		}
	}
}
