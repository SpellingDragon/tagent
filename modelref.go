package tagent

import (
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// FoldModelRefAliases merges legacy flat knob declarations into their unified
// ModelRef holders. Explicit ModelRef fields win per-field; legacy values only
// fill fields the ModelRef leaves unset. (tagent-unify-model-call-config.)
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
		if changed {
			c.Agents[name] = ac
		}
	}
}
