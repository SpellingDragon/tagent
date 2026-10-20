// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
//
// The strict mapping face of the declaration layer: it maps a decoded source
// tree onto the declaration vocabulary in config.go, rejecting every key the
// surface does not name, every repeated key and every value of the wrong shape,
// and it names the offending source, line and field path in the error. The field
// readers below are the only place the source node shapes are inspected, so the
// declaration types never carry syntax knowledge.
package config

import (
	"fmt"
	"math"
)

// decodeDeclaration maps the decoded source tree onto the declaration types,
// rejecting every key the declaration surface does not name. An empty or null
// document maps onto the zero declaration.
func decodeDeclaration(source string, root *sourceNode) (*Declaration, error) {
	d := &Declaration{}
	if root == nil || (root.kind == sourceScalar && root.scalar == nil) {
		return d, nil
	}
	if root.kind != sourceMap {
		return nil, &decodeError{source: source, line: root.line, reason: "expected a mapping"}
	}
	d.Raw = rawDocument(root)
	fields, err := mapFields(source, "", root, "entry", "agents", "refs", "baseline")
	if err != nil {
		return nil, err
	}
	if d.Entry, err = stringField(source, "entry", fields["entry"]); err != nil {
		return nil, err
	}
	if d.Agents, err = agentsField(source, fields["agents"]); err != nil {
		return nil, err
	}
	if d.Refs, err = toolRefsField(source, "refs", fields["refs"]); err != nil {
		return nil, err
	}
	if d.Baseline, err = stringField(source, "baseline", fields["baseline"]); err != nil {
		return nil, err
	}
	return d, nil
}

// mapFields indexes the entries of one fixed-shape mapping by key, rejecting
// keys outside the declared field set and repeated keys by name.
func mapFields(source, path string, n *sourceNode, allowed ...string) (map[string]*sourceEntry, error) {
	known := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		known[name] = true
	}
	fields := make(map[string]*sourceEntry, len(n.entries))
	for i := range n.entries {
		e := &n.entries[i]
		if !known[e.key] {
			return nil, &decodeError{source: source, line: e.line, field: joinField(path, e.key), reason: "unknown field"}
		}
		if _, dup := fields[e.key]; dup {
			return nil, &decodeError{source: source, line: e.line, field: joinField(path, e.key), reason: "duplicate field"}
		}
		fields[e.key] = e
	}
	return fields, nil
}

// mapEntryFields indexes the entries of one free-form mapping by key, rejecting
// repeated keys by name.
func mapEntryFields(source, path string, n *sourceNode) (map[string]*sourceEntry, error) {
	fields := make(map[string]*sourceEntry, len(n.entries))
	for i := range n.entries {
		e := &n.entries[i]
		if _, dup := fields[e.key]; dup {
			return nil, &decodeError{source: source, line: e.line, field: joinField(path, e.key), reason: "duplicate field"}
		}
		fields[e.key] = e
	}
	return fields, nil
}

// agentsField maps one agents sequence onto agent specs in declaration order.
func agentsField(source string, e *sourceEntry) ([]AgentSpec, error) {
	items, err := seqField(source, "agents", e)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, nil
	}
	agents := make([]AgentSpec, 0, len(items))
	for i, item := range items {
		agent, err := decodeAgent(source, indexPath("agents", i), item)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	return agents, nil
}

// decodeAgent maps one agent mapping onto an agent spec.
func decodeAgent(source, path string, n *sourceNode) (*AgentSpec, error) {
	if n == nil || n.kind != sourceMap {
		return nil, &decodeError{source: source, line: nodeLine(n), field: path, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, path, n, "id", "parent_id", "model", "tools", "plugins", "task_policy", "files", "params", "final_round")
	if err != nil {
		return nil, err
	}
	a := &AgentSpec{}
	if a.ID, err = requiredStringField(source, path, n, "id", fields["id"]); err != nil {
		return nil, err
	}
	if a.ParentID, err = stringField(source, joinField(path, "parent_id"), fields["parent_id"]); err != nil {
		return nil, err
	}
	if a.Model, err = stringField(source, joinField(path, "model"), fields["model"]); err != nil {
		return nil, err
	}
	if a.Tools, err = toolRefsField(source, joinField(path, "tools"), fields["tools"]); err != nil {
		return nil, err
	}
	if a.Plugins, err = stringSeqField(source, joinField(path, "plugins"), fields["plugins"]); err != nil {
		return nil, err
	}
	if a.TaskPolicy, err = stringField(source, joinField(path, "task_policy"), fields["task_policy"]); err != nil {
		return nil, err
	}
	if a.Files, err = filesField(source, joinField(path, "files"), fields["files"]); err != nil {
		return nil, err
	}
	if a.Params, err = floatMapField(source, joinField(path, "params"), fields["params"]); err != nil {
		return nil, err
	}
	if a.FinalRound, err = finalRoundField(source, joinField(path, "final_round"), fields["final_round"]); err != nil {
		return nil, err
	}
	return a, nil
}

// finalRoundField maps one watchdog budget mapping onto its declaration; an
// absent or null field declares no bound at all.
func finalRoundField(source, field string, e *sourceEntry) (FinalRoundSpec, error) {
	spec := FinalRoundSpec{}
	if isAbsent(e) {
		return spec, nil
	}
	if e.value.kind != sourceMap {
		return spec, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, field, e.value, "max_rounds", "budget_seconds", "timeout_seconds", "prompt")
	if err != nil {
		return spec, err
	}
	if spec.MaxRounds, err = roundField(source, joinField(field, "max_rounds"), fields["max_rounds"]); err != nil {
		return spec, err
	}
	if spec.BudgetSeconds, err = secondsField(source, joinField(field, "budget_seconds"), fields["budget_seconds"]); err != nil {
		return spec, err
	}
	if spec.TimeoutSeconds, err = secondsField(source, joinField(field, "timeout_seconds"), fields["timeout_seconds"]); err != nil {
		return spec, err
	}
	if spec.Prompt, err = stringField(source, joinField(field, "prompt"), fields["prompt"]); err != nil {
		return spec, err
	}
	return spec, nil
}

// roundField reads one optional round count; an absent field reads as zero, and
// a negative or fractional count is refused by name.
func roundField(source, field string, e *sourceEntry) (int, error) {
	if isAbsent(e) {
		return 0, nil
	}
	value, err := numberNode(source, field, e.value)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a non-negative count of rounds"}
	}
	if value != math.Trunc(value) {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a whole count of rounds"}
	}
	return int(value), nil
}

// secondsField reads one optional duration written as a count of seconds; an
// absent field reads as zero, and a negative count is refused by name.
func secondsField(source, field string, e *sourceEntry) (float64, error) {
	if isAbsent(e) {
		return 0, nil
	}
	value, err := numberNode(source, field, e.value)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a non-negative count of seconds"}
	}
	return value, nil
}

// toolRefsField maps one references sequence onto tool refs.
func toolRefsField(source, field string, e *sourceEntry) ([]ToolRef, error) {
	items, err := seqField(source, field, e)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, nil
	}
	refs := make([]ToolRef, 0, len(items))
	for i, item := range items {
		ref, err := decodeToolRef(source, indexPath(field, i), item)
		if err != nil {
			return nil, err
		}
		refs = append(refs, *ref)
	}
	return refs, nil
}

// decodeToolRef maps one reference mapping onto a tool ref.
func decodeToolRef(source, path string, n *sourceNode) (*ToolRef, error) {
	if n == nil || n.kind != sourceMap {
		return nil, &decodeError{source: source, line: nodeLine(n), field: path, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, path, n, "name", "params", "endpoint")
	if err != nil {
		return nil, err
	}
	r := &ToolRef{}
	if r.Name, err = requiredStringField(source, path, n, "name", fields["name"]); err != nil {
		return nil, err
	}
	if r.Params, err = stringMapField(source, joinField(path, "params"), fields["params"]); err != nil {
		return nil, err
	}
	if r.Endpoint, err = stringField(source, joinField(path, "endpoint"), fields["endpoint"]); err != nil {
		return nil, err
	}
	return r, nil
}

// filesField maps one files sequence onto file refs.
func filesField(source, field string, e *sourceEntry) ([]FileRef, error) {
	items, err := seqField(source, field, e)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, nil
	}
	files := make([]FileRef, 0, len(items))
	for i, item := range items {
		file, err := decodeFileRef(source, indexPath(field, i), item)
		if err != nil {
			return nil, err
		}
		files = append(files, *file)
	}
	return files, nil
}

// decodeFileRef maps one file mapping onto a file ref. The kind is optional and
// closed over the file kind set; defaults fill it in later.
func decodeFileRef(source, path string, n *sourceNode) (*FileRef, error) {
	if n == nil || n.kind != sourceMap {
		return nil, &decodeError{source: source, line: nodeLine(n), field: path, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, path, n, "path", "kind")
	if err != nil {
		return nil, err
	}
	f := &FileRef{}
	if f.Path, err = requiredStringField(source, path, n, "path", fields["path"]); err != nil {
		return nil, err
	}
	kind := ""
	if kind, err = stringField(source, joinField(path, "kind"), fields["kind"]); err != nil {
		return nil, err
	}
	switch FileKind(kind) {
	case "", FileKindPrompt, FileKindSkill:
		f.Kind = FileKind(kind)
	default:
		return nil, &decodeError{source: source, line: entryLine(fields["kind"]), field: joinField(path, "kind"), reason: fmt.Sprintf("unknown kind %q, want prompt or skill", kind)}
	}
	return f, nil
}

// stringField reads one optional string field; an absent or null field reads as
// the empty string.
func stringField(source, field string, e *sourceEntry) (string, error) {
	if isAbsent(e) {
		return "", nil
	}
	return stringNode(source, field, e.value)
}

// requiredStringField reads one mandatory string field; an absent, null or empty
// field is rejected as missing.
func requiredStringField(source, path string, n *sourceNode, key string, e *sourceEntry) (string, error) {
	field := joinField(path, key)
	if isAbsent(e) {
		return "", &decodeError{source: source, line: nodeLine(n), field: field, reason: "required field missing"}
	}
	s, err := stringNode(source, field, e.value)
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", &decodeError{source: source, line: entryLine(e), field: field, reason: "required field missing"}
	}
	return s, nil
}

// stringNode reads one string scalar node.
func stringNode(source, field string, n *sourceNode) (string, error) {
	if n == nil || n.kind != sourceScalar {
		return "", &decodeError{source: source, line: nodeLine(n), field: field, reason: "expected a string"}
	}
	s, ok := n.scalar.(string)
	if !ok {
		return "", &decodeError{source: source, line: nodeLine(n), field: field, reason: "expected a string"}
	}
	return s, nil
}

// seqField reads one optional sequence field; an absent or null field reads as
// no sequence at all.
func seqField(source, field string, e *sourceEntry) ([]*sourceNode, error) {
	if isAbsent(e) {
		return nil, nil
	}
	if e.value.kind != sourceSeq {
		return nil, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a sequence"}
	}
	return e.value.seq, nil
}

// stringSeqField reads one optional sequence of strings.
func stringSeqField(source, field string, e *sourceEntry) ([]string, error) {
	items, err := seqField(source, field, e)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, nil
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, err := stringNode(source, indexPath(field, i), item)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// floatMapField reads one optional free-form map of numbers.
func floatMapField(source, field string, e *sourceEntry) (map[string]float64, error) {
	if isAbsent(e) {
		return nil, nil
	}
	if e.value.kind != sourceMap {
		return nil, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	entries, err := mapEntryFields(source, field, e.value)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(entries))
	for key, ent := range entries {
		v, err := numberNode(source, joinField(field, key), ent.value)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

// stringMapField reads one optional free-form map of strings.
func stringMapField(source, field string, e *sourceEntry) (map[string]string, error) {
	if isAbsent(e) {
		return nil, nil
	}
	if e.value.kind != sourceMap {
		return nil, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	entries, err := mapEntryFields(source, field, e.value)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for key, ent := range entries {
		s, err := stringNode(source, joinField(field, key), ent.value)
		if err != nil {
			return nil, err
		}
		out[key] = s
	}
	return out, nil
}

// numberNode reads one numeric scalar node.
func numberNode(source, field string, n *sourceNode) (float64, error) {
	if n != nil && n.kind == sourceScalar {
		switch v := n.scalar.(type) {
		case int64:
			return float64(v), nil
		case float64:
			return v, nil
		}
	}
	return 0, &decodeError{source: source, line: nodeLine(n), field: field, reason: "expected a number"}
}

// rawDocument copies the decoded document into an independent plain-value map.
func rawDocument(n *sourceNode) map[string]any {
	if n == nil {
		return nil
	}
	m, _ := rawValue(n).(map[string]any)
	return m
}

// rawValue copies one decoded node into an independent plain value.
func rawValue(n *sourceNode) any {
	if n == nil {
		return nil
	}
	switch n.kind {
	case sourceScalar:
		return n.scalar
	case sourceSeq:
		out := make([]any, 0, len(n.seq))
		for _, item := range n.seq {
			out = append(out, rawValue(item))
		}
		return out
	default:
		out := make(map[string]any, len(n.entries))
		for _, e := range n.entries {
			out[e.key] = rawValue(e.value)
		}
		return out
	}
}

// isAbsent reports whether an entry holds no value: no entry, an empty node or
// an explicit null.
func isAbsent(e *sourceEntry) bool {
	return e == nil || e.value == nil || (e.value.kind == sourceScalar && e.value.scalar == nil)
}

// entryLine returns the line the entry key sits on.
func entryLine(e *sourceEntry) int {
	if e == nil {
		return 0
	}
	return e.line
}

// nodeLine returns the line a node starts on, or zero for an absent node.
func nodeLine(n *sourceNode) int {
	if n == nil {
		return 0
	}
	return n.line
}

// joinField renders one field path segment onto its parent path.
func joinField(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// indexPath renders one sequence index onto its parent path.
func indexPath(path string, i int) string {
	return fmt.Sprintf("%s[%d]", path, i)
}
