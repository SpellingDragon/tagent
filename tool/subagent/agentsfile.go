// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
//
// agentsfile reads the markdown form one sub-agent declaration is written in: a
// fenced YAML frontmatter block over a prompt body.
package subagent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// agentsFileFence fences the frontmatter block of an agents file.
const agentsFileFence = "---"

// agentsFileFields is the closed key set an agents file frontmatter may declare.
var agentsFileFields = map[string]bool{
	"name":        true,
	"description": true,
	"model":       true,
	"prompt":      true,
	"skills":      true,
	"tools":       true,
	"mcp":         true,
}

// loadAgentsFile reads one agents file and returns the definition it declares.
func loadAgentsFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Source: path, Kind: ErrFileUnreadable, Reason: err.Error()}
	}
	return parseAgentsFile(path, data)
}

// parseAgentsFile maps one agents file onto the definition its content declares.
func parseAgentsFile(source string, data []byte) (*Definition, error) {
	front, body, offset, err := splitFrontmatter(source, data)
	if err != nil {
		return nil, err
	}
	d := &Definition{}
	if len(bytes.TrimSpace(front)) > 0 {
		root, err := decodeFrontmatter(source, front, offset)
		if err != nil {
			return nil, err
		}
		if err := readFrontmatter(source, root, offset, d); err != nil {
			return nil, err
		}
	}
	if d.Prompt != "" {
		if err := resolveDeclaredPrompt(source, filepath.Dir(source), d); err != nil {
			return nil, err
		}
	}
	if d.Prompt == "" {
		d.Prompt = strings.TrimSpace(body)
	}
	d.promptText = true
	if err := requireName(source, d); err != nil {
		return nil, err
	}
	return d, nil
}

// resolveDeclaredPrompt reads a file-form prompt into text and marks it resolved.
func resolveDeclaredPrompt(source, baseDir string, d *Definition) error {
	text, err := readPrompt(source, baseDir, d.Prompt)
	if err != nil {
		return err
	}
	d.Prompt = text
	d.promptText = true
	return nil
}

// readPrompt returns the text a declared prompt holds: a value opening with ./,
// ../ or / names a file read relative to baseDir, and every other value is text.
func readPrompt(source, baseDir, value string) (string, error) {
	if !isPromptPath(value) {
		return value, nil
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", &Error{Source: source, Field: "prompt", Kind: ErrFileUnreadable, Reason: fmt.Sprintf("%s: %v", path, err)}
	}
	return strings.TrimSpace(string(data)), nil
}

// isPromptPath reports whether a declared prompt names a file rather than text.
func isPromptPath(value string) bool {
	return strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/")
}

// splitFrontmatter cuts an agents file into its frontmatter block, its body and the
// file line offset of the block's first YAML line.
func splitFrontmatter(source string, data []byte) (front []byte, body string, offset int, err error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != agentsFileFence {
		return nil, "", 0, &Error{Source: source, Line: 1, Kind: ErrFileShape, Reason: "the file does not open with a --- frontmatter fence"}
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != agentsFileFence {
			continue
		}
		return []byte(strings.Join(lines[1:i], "\n")), strings.Join(lines[i+1:], "\n"), 2, nil
	}
	return nil, "", 0, &Error{Source: source, Line: 1, Kind: ErrFileShape, Reason: "the frontmatter fence is not closed"}
}

// decodeFrontmatter decodes the one YAML mapping the frontmatter block holds.
func decodeFrontmatter(source string, front []byte, offset int) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(front))
	var head yaml.Node
	if err := dec.Decode(&head); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &Error{Source: source, Line: offset, Kind: ErrFileShape, Reason: "the frontmatter is empty"}
		}
		return nil, &Error{Source: source, Line: offset, Kind: ErrFileShape, Reason: "yaml: " + err.Error()}
	}
	var tail yaml.Node
	if err := dec.Decode(&tail); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, &Error{Source: source, Line: offset, Kind: ErrFileShape, Reason: "yaml: " + err.Error()}
		}
		return nil, &Error{Source: source, Line: tail.Line + offset - 1, Kind: ErrFileShape, Reason: "trailing document after the frontmatter"}
	}
	root := &head
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, &Error{Source: source, Line: offset, Kind: ErrFileShape, Reason: "the frontmatter is empty"}
		}
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, &Error{Source: source, Line: root.Line + offset - 1, Kind: ErrFileShape, Reason: "the frontmatter is not a mapping"}
	}
	return root, nil
}

// readFrontmatter maps the frontmatter mapping onto d, refusing keys the form does
// not name and keys declared twice.
func readFrontmatter(source string, root *yaml.Node, offset int, d *Definition) error {
	fields := make(map[string]*yaml.Node, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return &Error{Source: source, Line: key.Line + offset - 1, Kind: ErrFileShape, Reason: "a frontmatter key is not a string"}
		}
		if !agentsFileFields[key.Value] {
			return &Error{Source: source, Line: key.Line + offset - 1, Field: key.Value, Kind: ErrUnknownField, Reason: "unknown field"}
		}
		if _, dup := fields[key.Value]; dup {
			return &Error{Source: source, Line: key.Line + offset - 1, Field: key.Value, Kind: ErrFileShape, Reason: "duplicate field"}
		}
		fields[key.Value] = value
	}
	var err error
	if d.Name, err = stringField(source, "name", fields["name"], offset); err != nil {
		return err
	}
	if d.Description, err = stringField(source, "description", fields["description"], offset); err != nil {
		return err
	}
	if d.Model, err = stringField(source, "model", fields["model"], offset); err != nil {
		return err
	}
	if d.Prompt, err = stringField(source, "prompt", fields["prompt"], offset); err != nil {
		return err
	}
	if d.Skills, err = nameField(source, "skills", fields["skills"], offset); err != nil {
		return err
	}
	if d.Tools, err = nameField(source, "tools", fields["tools"], offset); err != nil {
		return err
	}
	if d.MCP, err = nameField(source, "mcp", fields["mcp"], offset); err != nil {
		return err
	}
	return nil
}

// stringField reads one optional string field; a null field reads as absent.
func stringField(source, field string, n *yaml.Node, offset int) (string, error) {
	if isNullNode(n) {
		return "", nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", &Error{Source: source, Line: n.Line + offset - 1, Field: field, Kind: ErrFieldType, Reason: "expected a string"}
	}
	return n.Value, nil
}

// nameField reads one optional name list: a sequence of strings or a
// comma-separated string, which is the form an agents file writes tools in.
func nameField(source, field string, n *yaml.Node, offset int) ([]string, error) {
	if isNullNode(n) {
		return nil, nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag != "!!str" {
			return nil, &Error{Source: source, Line: n.Line + offset - 1, Field: field, Kind: ErrFieldType, Reason: "expected a sequence of names or a comma-separated string"}
		}
		return splitNames(n.Value), nil
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return nil, &Error{Source: source, Line: item.Line + offset - 1, Field: field, Kind: ErrFieldType, Reason: "expected a string name"}
			}
			name := strings.TrimSpace(item.Value)
			if name == "" {
				return nil, &Error{Source: source, Line: item.Line + offset - 1, Field: field, Kind: ErrFieldType, Reason: "expected a non-empty name"}
			}
			out = append(out, name)
		}
		return out, nil
	default:
		return nil, &Error{Source: source, Line: n.Line + offset - 1, Field: field, Kind: ErrFieldType, Reason: "expected a sequence of names or a comma-separated string"}
	}
}

// splitNames reads a comma-separated name list into its non-empty names.
func splitNames(value string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(value, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// isNullNode reports whether a node holds no value.
func isNullNode(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}
