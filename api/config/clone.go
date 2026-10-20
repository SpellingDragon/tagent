package config

// 契约: docs/wiki/platform/org-hot-reload.md#config-clone
//
// Clone returns a deep copy of the declaration. Mutating the copy never changes
// the original, and the copy owns its own raw document.
func (d *Declaration) Clone() *Declaration {
	if d == nil {
		return nil
	}
	out := &Declaration{
		Entry:    d.Entry,
		Agents:   cloneAgents(d.Agents),
		Refs:     cloneToolRefs(d.Refs),
		Baseline: d.Baseline,
		Servers:  cloneServerSpecs(d.Servers),
		Raw:      cloneRawMap(d.Raw),
	}
	return out
}

// AgentSpecs returns the declared agents as a deep copy, so callers may mutate
// the result without touching the declaration.
func (d *Declaration) AgentSpecs() []AgentSpec {
	return cloneAgents(d.Agents)
}

// ToolRefs returns the organization level references as a deep copy, so callers
// may mutate the result without touching the declaration.
func (d *Declaration) ToolRefs() []ToolRef {
	return cloneToolRefs(d.Refs)
}

// RawDocument returns the decoded source document as a deep copy, so callers may
// mutate the result without touching the declaration.
func (d *Declaration) RawDocument() map[string]any {
	return cloneRawMap(d.Raw)
}

// cloneAgents deep copies a slice of agent specs.
func cloneAgents(in []AgentSpec) []AgentSpec {
	if in == nil {
		return nil
	}
	out := make([]AgentSpec, len(in))
	for i := range in {
		out[i] = cloneAgentSpec(in[i])
	}
	return out
}

// cloneAgentSpec deep copies one agent spec.
func cloneAgentSpec(in AgentSpec) AgentSpec {
	out := in
	out.Tools = cloneToolRefs(in.Tools)
	out.Plugins = cloneStrings(in.Plugins)
	out.Files = cloneFileRefs(in.Files)
	out.Params = cloneFloatMap(in.Params)
	out.Memory.ReadNamespaces = cloneStrings(in.Memory.ReadNamespaces)
	out.Meditation.ObservedNamespaces = cloneStrings(in.Meditation.ObservedNamespaces)
	out.Meditation.DeliverTo = cloneStrings(in.Meditation.DeliverTo)
	out.File.ReadRoots = cloneStrings(in.File.ReadRoots)
	return out
}

// cloneServerSpecs deep copies the declared MCP servers.
func cloneServerSpecs(in map[string]MCPServerSpec) map[string]MCPServerSpec {
	if in == nil {
		return nil
	}
	out := make(map[string]MCPServerSpec, len(in))
	for name, spec := range in {
		out[name] = spec
	}
	return out
}

// cloneToolRefs deep copies a slice of tool refs.
func cloneToolRefs(in []ToolRef) []ToolRef {
	if in == nil {
		return nil
	}
	out := make([]ToolRef, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Params = cloneStringMap(in[i].Params)
	}
	return out
}

// cloneFileRefs deep copies a slice of file refs.
func cloneFileRefs(in []FileRef) []FileRef {
	if in == nil {
		return nil
	}
	out := make([]FileRef, len(in))
	copy(out, in)
	return out
}

// cloneStrings deep copies a slice of strings.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// cloneStringMap deep copies a string map.
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneFloatMap deep copies a float map.
func cloneFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneRawMap deep copies the decoded source document.
func cloneRawMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneRawValue(v)
	}
	return out
}

// cloneRawValue deep copies one decoded plain value.
func cloneRawValue(in any) any {
	switch v := in.(type) {
	case map[string]any:
		return cloneRawMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneRawValue(v[i])
		}
		return out
	default:
		return in
	}
}
