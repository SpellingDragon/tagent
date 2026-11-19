// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
//
// The compression consumer's own mount: the one place its concrete type is
// constructed. The consumer executes the judgments the age fold files, so its
// mount also installs it as that plugin's executor; the age fold finds it
// constructed whichever order the catalog lists the two in.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package assembly

import (
	"github.com/SpellingDragon/tagent/api/plugin"
)

// compressConsumerPlugin is the catalog entry of the compression consumer: a
// run-level plugin every agent generation carries, mounted by the default boot
// flow unless a deployment customizes the run-level set.
func compressConsumerPlugin() builtinPlugin {
	return builtinPlugin{
		name:  compressConsumerPluginName,
		scope: runPluginScope,
		run: func(r *Run) ([]plugin.Instance, error) {
			face := ageFoldSessions{run: r}
			r.compress = NewCompressionConsumer(face, face).withSummaryGist(bandSummaries{run: r})
			r.ageFold.setExecutor(r.compress)
			return []plugin.Instance{r.compress}, nil
		},
	}
}
