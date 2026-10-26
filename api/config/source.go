// The source-syntax face of the declaration layer: it turns the bytes of a
// .yaml, .yml or .json source into one sourceNode tree and rejects every other
// extension by name. The node types are syntax only — they name no declaration
// word — so decode.go maps this tree onto the vocabulary in config.go and this
// file never decides what a field means.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
//
// decodeSource decodes data as the declaration document named by the extension of
// source: .yaml and .yml decode as YAML, .json decodes as JSON, and every other
// extension is rejected by name. An empty document decodes to a nil tree, which the
// declaration mapping reads as the zero declaration.
func decodeSource(source string, data []byte) (*sourceNode, error) {
	switch ext := strings.ToLower(filepath.Ext(source)); ext {
	case ".yaml", ".yml":
		return decodeYAML(source, data)
	case ".json":
		return decodeJSON(source, data)
	default:
		return nil, &decodeError{source: source, reason: fmt.Sprintf("unsupported source format %q", ext)}
	}
}

// decodeYAML decodes the first YAML document of data and rejects anything after it.
func decodeYAML(source string, data []byte) (*sourceNode, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var head yaml.Node
	if err := dec.Decode(&head); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, &decodeError{source: source, reason: "yaml: " + err.Error()}
	}
	var tail yaml.Node
	if err := dec.Decode(&tail); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, &decodeError{source: source, reason: "yaml: " + err.Error()}
		}
		return nil, &decodeError{source: source, line: tail.Line, reason: "trailing document after the first"}
	}
	return yamlNode(source, &head)
}

// yamlNode maps one YAML node onto the generic source tree. Alias nodes expand onto
// their anchor content, and mapping keys must be strings.
func yamlNode(source string, n *yaml.Node) (*sourceNode, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return yamlNode(source, n.Content[0])
	case yaml.AliasNode:
		return yamlNode(source, n.Alias)
	case yaml.MappingNode:
		out := &sourceNode{kind: sourceMap, line: n.Line}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return nil, &decodeError{source: source, line: key.Line, reason: "expected a string key"}
			}
			decoded, err := yamlNode(source, val)
			if err != nil {
				return nil, err
			}
			out.entries = append(out.entries, sourceEntry{key: key.Value, line: key.Line, value: decoded})
		}
		return out, nil
	case yaml.SequenceNode:
		out := &sourceNode{kind: sourceSeq, line: n.Line}
		for _, item := range n.Content {
			decoded, err := yamlNode(source, item)
			if err != nil {
				return nil, err
			}
			out.seq = append(out.seq, decoded)
		}
		return out, nil
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, &decodeError{source: source, line: n.Line, reason: "yaml: " + err.Error()}
		}
		scalar, err := normalizeScalar(v)
		if err != nil {
			return nil, &decodeError{source: source, line: n.Line, reason: err.Error()}
		}
		return &sourceNode{kind: sourceScalar, line: n.Line, scalar: scalar}, nil
	default:
		return nil, &decodeError{source: source, line: n.Line, reason: "unsupported yaml node"}
	}
}

// decodeJSON decodes the first JSON value of data and rejects anything after it.
func decodeJSON(source string, data []byte) (*sourceNode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	head, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
	}
	root, err := jsonNode(source, data, dec, head)
	if err != nil {
		return nil, err
	}
	if tail, err := dec.Token(); err == nil {
		_ = tail
		return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "trailing content after the first value"}
	} else if !errors.Is(err, io.EOF) {
		return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
	}
	return root, nil
}

// jsonNode maps one JSON value, opened by tok, onto the generic source tree.
func jsonNode(source string, data []byte, dec *json.Decoder, tok json.Token) (*sourceNode, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := &sourceNode{kind: sourceMap, line: lineAt(data, dec.InputOffset())}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "expected a string key"}
				}
				keyLine := lineAt(data, dec.InputOffset())
				valTok, err := dec.Token()
				if err != nil {
					return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
				}
				val, err := jsonNode(source, data, dec, valTok)
				if err != nil {
					return nil, err
				}
				out.entries = append(out.entries, sourceEntry{key: key, line: keyLine, value: val})
			}
			if _, err := dec.Token(); err != nil {
				return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
			}
			return out, nil
		case '[':
			out := &sourceNode{kind: sourceSeq, line: lineAt(data, dec.InputOffset())}
			for dec.More() {
				itemTok, err := dec.Token()
				if err != nil {
					return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
				}
				item, err := jsonNode(source, data, dec, itemTok)
				if err != nil {
					return nil, err
				}
				out.seq = append(out.seq, item)
			}
			if _, err := dec.Token(); err != nil {
				return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: " + err.Error()}
			}
			return out, nil
		default:
			return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "unexpected json delimiter"}
		}
	case json.Number:
		text := t.String()
		if !strings.ContainsAny(text, ".eE") {
			if i, err := strconv.ParseInt(text, 10, 64); err == nil {
				return &sourceNode{kind: sourceScalar, line: lineAt(data, dec.InputOffset()), scalar: i}, nil
			}
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: "json: invalid number " + text}
		}
		return &sourceNode{kind: sourceScalar, line: lineAt(data, dec.InputOffset()), scalar: f}, nil
	case string:
		return &sourceNode{kind: sourceScalar, line: lineAt(data, dec.InputOffset()), scalar: t}, nil
	case bool:
		return &sourceNode{kind: sourceScalar, line: lineAt(data, dec.InputOffset()), scalar: t}, nil
	case nil:
		return &sourceNode{kind: sourceScalar, line: lineAt(data, dec.InputOffset()), scalar: nil}, nil
	default:
		return nil, &decodeError{source: source, line: lineAt(data, dec.InputOffset()), reason: fmt.Sprintf("unsupported json value %T", t)}
	}
}

// lineAt returns the one-based line the byte offset sits on.
func lineAt(data []byte, offset int64) int {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return 1 + bytes.Count(data[:offset], []byte("\n"))
}

// normalizeScalar narrows one decoded scalar to the declaration scalar set: nil,
// bool, int64, float64 or string.
func normalizeScalar(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string, int64, float64:
		return t, nil
	case int:
		return int64(t), nil
	case int32:
		return int64(t), nil
	case uint64:
		if t > math.MaxInt64 {
			return nil, errors.New("integer out of range")
		}
		return int64(t), nil
	case uint:
		if uint64(t) > math.MaxInt64 {
			return nil, errors.New("integer out of range")
		}
		return int64(t), nil
	default:
		return nil, fmt.Errorf("unsupported scalar %T", v)
	}
}

// sourceKind classifies one decoded source node.
type sourceKind int

const (
	sourceScalar sourceKind = iota
	sourceSeq
	sourceMap
)

// sourceNode is one decoded document node together with the source line it starts
// on. Scalars carry nil, bool, int64, float64 or string values.
type sourceNode struct {
	kind    sourceKind
	line    int
	scalar  any
	seq     []*sourceNode
	entries []sourceEntry
}

// sourceEntry is one mapping entry: its key, the line the key sits on and its value.
type sourceEntry struct {
	key   string
	line  int
	value *sourceNode
}

// decodeError names one rejected configuration input: the source it came from, the
// line it sits on, the field path it was read at, the reason it was rejected and
// the sentinel that classifies the rejection.
type decodeError struct {
	source string
	line   int
	field  string
	reason string
	cause  error
}

// Unwrap reports the sentinel that classifies the rejection, so a caller can
// test errors.Is against it.
func (e *decodeError) Unwrap() error { return e.cause }

// Error renders the rejection as source, line, field path and reason.
func (e *decodeError) Error() string {
	where := e.source
	if e.line > 0 {
		where = fmt.Sprintf("%s:%d", e.source, e.line)
	}
	if e.field == "" {
		return fmt.Sprintf("config: %s: %s", where, e.reason)
	}
	return fmt.Sprintf("config: %s: %s: %s", where, e.field, e.reason)
}
