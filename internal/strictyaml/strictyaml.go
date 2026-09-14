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

// DecodeYAML strictly parses YAML into out, rejecting unknown fields AND any
// trailing documents after the first (a silent second document could carry
// config the user believes is live — review P2-4).
func DecodeYAML(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty document: nothing to decode
		}
		return fmt.Errorf("strict yaml: %w", err)
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		return nil // exactly one document
	case err != nil:
		return fmt.Errorf("strict yaml: trailing content: %w", err)
	default:
		return fmt.Errorf("strict yaml: unexpected second document after the first (multi-document streams are not config)")
	}
}

// DecodeJSON strictly parses JSON into out, rejecting unknown fields AND any
// trailing content after the first value.
func DecodeJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("strict json: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("strict json: trailing content: %w", err)
		}
		return fmt.Errorf("strict json: unexpected content after the first value")
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
