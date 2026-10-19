// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// The assembly face of the declaration layer: the one entry that turns a source
// file into a Declaration. Decoding the source syntax belongs to source.go,
// mapping the decoded tree onto the declaration types belongs to decode.go and
// the default pass belongs to defaults.go; this file only sequences them, so a
// caller reads one door instead of three.
package config

import (
	"context"
	"fmt"
	"os"
)

// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// Load reads the declaration document at path, applies defaults to it and
// returns the normalized Declaration. Sources ending in .yaml or .yml decode as
// YAML, sources ending in .json decode as JSON, and every other extension is
// rejected by name. Failures are reported as errors that name the offending
// source, the line within it, the field path and the reason.
func Load(ctx context.Context, path string) (*Declaration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	root, err := decodeSource(path, data)
	if err != nil {
		return nil, err
	}
	d, err := decodeDeclaration(path, root)
	if err != nil {
		return nil, err
	}
	applyDefaults(d)
	return d, nil
}
