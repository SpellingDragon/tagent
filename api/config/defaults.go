package config

// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// applyDefaults fills in the fields the declaration surface gives a default
// semantics to. It runs after decoding and before fingerprinting, so equivalent
// documents that spell a default out and documents that omit it normalize onto
// the same declaration. A file whose kind is omitted plays the prompt role; an
// agent that declares a replay window but names neither fold field takes the
// declaration defaults, so a declaration naming only max_tokens folds under the
// default share of its window and keeps the default recent turns. Values
// written in the source are never overwritten and no other field is given a
// value the source did not name.
func applyDefaults(d *Declaration) {
	for i := range d.Agents {
		a := &d.Agents[i]
		for j := range a.Files {
			if a.Files[j].Kind == "" {
				a.Files[j].Kind = defaultFileKind
			}
		}
		if a.MaxTokens > 0 {
			if a.CompressThreshold <= 0 {
				a.CompressThreshold = DefaultCompressThreshold
			}
			if a.KeepRecentTasks <= 0 {
				a.KeepRecentTasks = DefaultKeepRecentTasks
			}
		}
	}
}

// defaultFileKind is the file kind a file ref takes when its kind is omitted.
const defaultFileKind = FileKindPrompt

const (
	// DefaultCompressThreshold is the share of the model window the replayed
	// projection may fill before the age fold runs, taken when a declaration
	// names no threshold.
	DefaultCompressThreshold = 0.8
	// DefaultKeepRecentTasks is the number of recent task turns the age fold
	// keeps whole, taken when a declaration names no count.
	DefaultKeepRecentTasks = 2
)
