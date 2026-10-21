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
	"time"
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
	fields, err := mapFields(source, "", root, "entry", "agents", "refs", "baseline", "mcp_servers")
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
	if d.Servers, err = mcpServersField(source, "mcp_servers", fields["mcp_servers"]); err != nil {
		return nil, err
	}
	return d, nil
}

// mcpServersField maps one free-form mapping of server names onto the server
// declarations it binds; an absent or null field binds no server.
func mcpServersField(source, field string, e *sourceEntry) (map[string]MCPServerSpec, error) {
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
	out := make(map[string]MCPServerSpec, len(entries))
	for name, ent := range entries {
		server, err := mcpServerField(source, joinField(field, name), ent)
		if err != nil {
			return nil, err
		}
		out[name] = server
	}
	return out, nil
}

// mcpServerField maps one MCP server declaration onto its spec.
func mcpServerField(source, field string, e *sourceEntry) (MCPServerSpec, error) {
	spec := MCPServerSpec{}
	if isAbsent(e) {
		return spec, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	if e.value.kind != sourceMap {
		return spec, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, field, e.value, "transport", "url", "api_key_env")
	if err != nil {
		return spec, err
	}
	if spec.Transport, err = stringField(source, joinField(field, "transport"), fields["transport"]); err != nil {
		return spec, err
	}
	if spec.URL, err = requiredStringField(source, field, e.value, "url", fields["url"]); err != nil {
		return spec, err
	}
	if spec.APIKeyEnv, err = stringField(source, joinField(field, "api_key_env"), fields["api_key_env"]); err != nil {
		return spec, err
	}
	return spec, nil
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

// agentKeys are the agent keys the declaration surface names: the core fields
// this package reads itself and the four domain sections, which are plain
// fields of AgentSpec.
var agentKeys = []string{
	"id", "parent_id", "model", "tools", "plugins", "task_policy", "files", "params",
	"final_round", "max_tokens", "compress_threshold", "keep_recent_tasks", "environment_block",
	"memory", "meditation", "file", "exec",
}

// decodeAgent maps one agent mapping onto an agent spec: the core keys first,
// then the four domain sections in canonical slot order.
func decodeAgent(source, path string, n *sourceNode) (*AgentSpec, error) {
	if n == nil || n.kind != sourceMap {
		return nil, &decodeError{source: source, line: nodeLine(n), field: path, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, path, n, agentKeys...)
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
	if a.MaxTokens, err = countField(source, joinField(path, "max_tokens"), fields["max_tokens"]); err != nil {
		return nil, err
	}
	if a.CompressThreshold, err = ratioField(source, joinField(path, "compress_threshold"), fields["compress_threshold"]); err != nil {
		return nil, err
	}
	if a.KeepRecentTasks, err = countField(source, joinField(path, "keep_recent_tasks"), fields["keep_recent_tasks"]); err != nil {
		return nil, err
	}
	if a.EnvironmentBlock, err = boolField(source, joinField(path, "environment_block"), fields["environment_block"]); err != nil {
		return nil, err
	}
	if a.Memory, err = memoryField(source, joinField(path, "memory"), fields["memory"]); err != nil {
		return nil, err
	}
	if a.Meditation, err = meditationField(source, joinField(path, "meditation"), fields["meditation"]); err != nil {
		return nil, err
	}
	if a.File, err = fileField(source, joinField(path, "file"), fields["file"]); err != nil {
		return nil, err
	}
	if a.Exec, err = execField(source, joinField(path, "exec"), fields["exec"]); err != nil {
		return nil, err
	}
	return a, nil
}

// sectionFields reads one optional section mapping: an absent or null section
// declares nothing, and a value that is not a mapping is refused by field path
// so every decoder below only ever reads a mapping. The keys the section names
// are declared here, so a key outside them is refused before the decoder runs.
func sectionFields(source, field string, e *sourceEntry, keys ...string) (map[string]*sourceEntry, bool, error) {
	if isAbsent(e) {
		return nil, false, nil
	}
	if e.value.kind != sourceMap {
		return nil, false, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a mapping"}
	}
	fields, err := mapFields(source, field, e.value, keys...)
	if err != nil {
		return nil, false, err
	}
	return fields, true, nil
}

// memoryField maps one memory section mapping onto its declaration.
func memoryField(source, field string, e *sourceEntry) (MemorySpec, error) {
	spec := MemorySpec{}
	fields, declared, err := sectionFields(source, field, e, "type", "path", "read_namespaces", "engine")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Type, err = stringField(source, joinField(field, "type"), fields["type"]); err != nil {
		return MemorySpec{}, err
	}
	if spec.Path, err = stringField(source, joinField(field, "path"), fields["path"]); err != nil {
		return MemorySpec{}, err
	}
	if spec.ReadNamespaces, err = stringSeqField(source, joinField(field, "read_namespaces"), fields["read_namespaces"]); err != nil {
		return MemorySpec{}, err
	}
	if spec.Engine, err = engineField(source, joinField(field, "engine"), fields["engine"]); err != nil {
		return MemorySpec{}, err
	}
	return spec, nil
}

// engineField maps one semantic engine section onto its declaration; an absent
// or null engine declares no engine, which is the keyword-only retrieval.
func engineField(source, field string, e *sourceEntry) (EngineSpec, error) {
	spec := EngineSpec{}
	fields, declared, err := sectionFields(source, field, e, "backend", "embedding", "vector_top_k", "keyword_top_k", "rrf_k")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Backend, err = stringField(source, joinField(field, "backend"), fields["backend"]); err != nil {
		return EngineSpec{}, err
	}
	if spec.Embedding, err = embeddingField(source, joinField(field, "embedding"), fields["embedding"]); err != nil {
		return EngineSpec{}, err
	}
	if spec.VectorTopK, err = countField(source, joinField(field, "vector_top_k"), fields["vector_top_k"]); err != nil {
		return EngineSpec{}, err
	}
	if spec.KeywordTopK, err = countField(source, joinField(field, "keyword_top_k"), fields["keyword_top_k"]); err != nil {
		return EngineSpec{}, err
	}
	if spec.RRFK, err = countField(source, joinField(field, "rrf_k"), fields["rrf_k"]); err != nil {
		return EngineSpec{}, err
	}
	return spec, nil
}

// embeddingField maps one embedding connection onto its declaration; an absent
// or null connection names no embedding.
func embeddingField(source, field string, e *sourceEntry) (EmbeddingSpec, error) {
	spec := EmbeddingSpec{}
	fields, declared, err := sectionFields(source, field, e, "provider", "model", "api_key_env", "dimensions", "api_endpoint")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Provider, err = stringField(source, joinField(field, "provider"), fields["provider"]); err != nil {
		return EmbeddingSpec{}, err
	}
	if spec.Model, err = stringField(source, joinField(field, "model"), fields["model"]); err != nil {
		return EmbeddingSpec{}, err
	}
	if spec.APIKeyEnv, err = stringField(source, joinField(field, "api_key_env"), fields["api_key_env"]); err != nil {
		return EmbeddingSpec{}, err
	}
	if spec.Dimensions, err = countField(source, joinField(field, "dimensions"), fields["dimensions"]); err != nil {
		return EmbeddingSpec{}, err
	}
	if spec.APIEndpoint, err = stringField(source, joinField(field, "api_endpoint"), fields["api_endpoint"]); err != nil {
		return EmbeddingSpec{}, err
	}
	return spec, nil
}

// meditationField maps one meditation section mapping onto its declaration.
func meditationField(source, field string, e *sourceEntry) (MeditationSpec, error) {
	spec := MeditationSpec{}
	fields, declared, err := sectionFields(source, field, e, "enabled", "interval", "min_gap", "prompt_file", "observed_namespaces", "deliver_to")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Enabled, err = boolField(source, joinField(field, "enabled"), fields["enabled"]); err != nil {
		return MeditationSpec{}, err
	}
	if spec.Interval, err = durationField(source, joinField(field, "interval"), fields["interval"]); err != nil {
		return MeditationSpec{}, err
	}
	if spec.MinGap, err = durationField(source, joinField(field, "min_gap"), fields["min_gap"]); err != nil {
		return MeditationSpec{}, err
	}
	if spec.PromptFile, err = stringField(source, joinField(field, "prompt_file"), fields["prompt_file"]); err != nil {
		return MeditationSpec{}, err
	}
	if spec.ObservedNamespaces, err = stringSeqField(source, joinField(field, "observed_namespaces"), fields["observed_namespaces"]); err != nil {
		return MeditationSpec{}, err
	}
	if spec.DeliverTo, err = stringSeqField(source, joinField(field, "deliver_to"), fields["deliver_to"]); err != nil {
		return MeditationSpec{}, err
	}
	return spec, nil
}

// fileField maps one file section mapping onto its declaration.
func fileField(source, field string, e *sourceEntry) (FileSpec, error) {
	spec := FileSpec{}
	fields, declared, err := sectionFields(source, field, e, "base_dir", "read_roots")
	if err != nil || !declared {
		return spec, err
	}
	if spec.BaseDir, err = stringField(source, joinField(field, "base_dir"), fields["base_dir"]); err != nil {
		return FileSpec{}, err
	}
	if spec.ReadRoots, err = stringSeqField(source, joinField(field, "read_roots"), fields["read_roots"]); err != nil {
		return FileSpec{}, err
	}
	return spec, nil
}

// execField maps one exec section mapping onto its declaration.
func execField(source, field string, e *sourceEntry) (ExecSpec, error) {
	spec := ExecSpec{}
	fields, declared, err := sectionFields(source, field, e, "monitor")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Monitor, err = monitorField(source, joinField(field, "monitor"), fields["monitor"]); err != nil {
		return ExecSpec{}, err
	}
	return spec, nil
}

// monitorField maps one monitor rhythm onto its declaration; an absent or null
// rhythm takes the runtime defaults.
func monitorField(source, field string, e *sourceEntry) (MonitorSpec, error) {
	spec := MonitorSpec{}
	fields, declared, err := sectionFields(source, field, e, "interval", "stable_duration", "interactive_stable_duration", "fake_dead_duration")
	if err != nil || !declared {
		return spec, err
	}
	if spec.Interval, err = durationField(source, joinField(field, "interval"), fields["interval"]); err != nil {
		return MonitorSpec{}, err
	}
	if spec.StableDuration, err = durationField(source, joinField(field, "stable_duration"), fields["stable_duration"]); err != nil {
		return MonitorSpec{}, err
	}
	if spec.InteractiveStableDuration, err = durationField(source, joinField(field, "interactive_stable_duration"), fields["interactive_stable_duration"]); err != nil {
		return MonitorSpec{}, err
	}
	if spec.FakeDeadDuration, err = durationField(source, joinField(field, "fake_dead_duration"), fields["fake_dead_duration"]); err != nil {
		return MonitorSpec{}, err
	}
	return spec, nil
}

// boolField reads one optional boolean field; an absent or null field reads as
// false.
func boolField(source, field string, e *sourceEntry) (bool, error) {
	if isAbsent(e) {
		return false, nil
	}
	if e.value.kind != sourceScalar {
		return false, &decodeError{source: source, line: nodeLine(e.value), field: field, reason: "expected a boolean"}
	}
	value, ok := e.value.scalar.(bool)
	if !ok {
		return false, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a boolean"}
	}
	return value, nil
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

// countField reads one optional non-negative whole number; an absent field
// reads as zero, and a negative or fractional count is refused by name.
func countField(source, field string, e *sourceEntry) (int, error) {
	if isAbsent(e) {
		return 0, nil
	}
	value, err := numberNode(source, field, e.value)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a non-negative whole number"}
	}
	if value != math.Trunc(value) {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a whole number"}
	}
	return int(value), nil
}

// ratioField reads one optional ratio in (0, 1]; an absent field reads as zero,
// which the declaration layer reads as the default ratio, and a value outside
// the interval is refused by name instead of degrading to a bound the
// declaration can never mean.
func ratioField(source, field string, e *sourceEntry) (float64, error) {
	if isAbsent(e) {
		return 0, nil
	}
	value, err := numberNode(source, field, e.value)
	if err != nil {
		return 0, err
	}
	if value <= 0 || value > 1 {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: "expected a ratio greater than zero and at most one"}
	}
	return value, nil
}

// durationField reads one optional duration written as a Go duration string;
// an absent field reads as zero.
func durationField(source, field string, e *sourceEntry) (time.Duration, error) {
	if isAbsent(e) {
		return 0, nil
	}
	text, err := stringNode(source, field, e.value)
	if err != nil {
		return 0, err
	}
	value, err := time.ParseDuration(text)
	if err != nil {
		return 0, &decodeError{source: source, line: entryLine(e), field: field, reason: fmt.Sprintf("expected a duration such as 30m: %v", err)}
	}
	return value, nil
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
