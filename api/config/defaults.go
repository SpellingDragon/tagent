package config

// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// applyDefaults fills in the fields the declaration surface gives a default
// semantics to. It runs after decoding and before fingerprinting, so equivalent
// documents that spell a default out and documents that omit it normalize onto
// the same declaration. Only file kind carries a default here: a file whose kind
// is omitted plays the prompt role. Values written in the source are never
// overwritten and no other field is given a value the source did not name.
func applyDefaults(d *Declaration) {
	for i := range d.Agents {
		files := d.Agents[i].Files
		for j := range files {
			if files[j].Kind == "" {
				files[j].Kind = defaultFileKind
			}
		}
	}
}

// defaultFileKind is the file kind a file ref takes when its kind is omitted.
const defaultFileKind = FileKindPrompt
