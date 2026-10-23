// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
//
// The strict face for caller-owned document schemas: decode.go names one schema,
// the declaration vocabulary, while a host composes its own document (connection
// registry, governance section, application section) and owes its reader the same
// rejection. DecodeStrict applies this package's policy — every key the schema
// does not name is refused by source, line and field path — to the schema the
// caller's struct type states in its yaml tags, so one rejection vocabulary
// covers both kinds of document and no host spells its own.
package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrUnknownConfigKey reports a document key the target schema does not name.
// Errors wrapping it also carry the source, the line and the field path, so a
// caller can tell a key outside the schema from a value of the wrong shape with
// errors.Is alone.
var ErrUnknownConfigKey = errors.New("document key outside the schema")

// DecodeStrict decodes the document in data into v and refuses every mapping key
// the type of v does not name. v must point at a struct whose yaml tags state the
// schema; nested structs, maps and sequences are walked, so a host document and
// its sections are checked to the same depth. The zero document decodes into the
// zero value of v, and a value of the wrong shape is reported by source and
// reason rather than by key.
func DecodeStrict(source string, data []byte, v any) error {
	root, err := decodeSource(source, data)
	if err != nil {
		return err
	}
	target, err := schemaType(v)
	if err != nil {
		return fmt.Errorf("config: %s: %w", source, err)
	}
	if err := strictKeys(source, "", root, target); err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return &decodeError{source: source, reason: "yaml: " + err.Error()}
	}
	return nil
}

// schemaType reports the struct type v points at, refusing a target that cannot
// state a document schema.
func schemaType(v any) (reflect.Type, error) {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("strict decode needs a pointer to a struct, got %T", v)
	}
	return t.Elem(), nil
}

// strictKeys walks one decoded node against the schema type t and reports the
// first key outside it. A mapping whose target names no field set — a map, an
// interface, a scalar holder — carries free-form keys and is walked for its
// values only.
func strictKeys(source, path string, n *sourceNode, t reflect.Type) error {
	if n == nil || t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch n.kind {
	case sourceMap:
		switch t.Kind() {
		case reflect.Map:
			for i := range n.entries {
				e := &n.entries[i]
				if err := strictKeys(source, joinField(path, e.key), e.value, t.Elem()); err != nil {
					return err
				}
			}
		case reflect.Struct:
			fields := yamlFields(t)
			for i := range n.entries {
				e := &n.entries[i]
				field, ok := fields[e.key]
				if !ok {
					return &decodeError{
						source: source,
						line:   e.line,
						field:  joinField(path, e.key),
						reason: "unknown field",
						cause:  ErrUnknownConfigKey,
					}
				}
				if err := strictKeys(source, joinField(path, e.key), e.value, field); err != nil {
					return err
				}
			}
		}
	case sourceSeq:
		if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
			return nil
		}
		for i, item := range n.seq {
			if err := strictKeys(source, indexPath(path, i), item, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// yamlFields indexes one struct type's document keys by the yaml tag each field
// declares; an untagged field takes the lowercased field name and a field tagged
// "-" is outside the document.
func yamlFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Type
	}
	return fields
}
