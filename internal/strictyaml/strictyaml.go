// Package strictyaml is the single strict-decode implementation shared by
// every config entry point (implementation-hardening 6.1): unknown fields
// make loading FAIL loudly instead of silently ignoring a typo'd key.
// One implementation, N call sites — adding a new config entry point MUST
// go through this package.
package strictyaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeYAML strictly parses YAML into out, rejecting unknown fields.
func DecodeYAML(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty document: nothing to decode
		}
		return fmt.Errorf("strict yaml: %w", err)
	}
	return nil
}

// DecodeJSON strictly parses JSON into out, rejecting unknown fields.
func DecodeJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("strict json: %w", err)
	}
	return nil
}

// DecodeByExt dispatches on file extension (".yaml"/".yml" → YAML, anything
// else → JSON), mirroring LoadConfig's format auto-detection.
func DecodeByExt(path string, data []byte, out any) error {
	if strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml") {
		return DecodeYAML(data, out)
	}
	return DecodeJSON(data, out)
}
