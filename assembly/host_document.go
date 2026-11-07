// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The host document dialect one Boot call decodes and the assembly it spells: the
// declared agents, their prompts, tools and providers, and the model each agent
// samples through. The document is the host's own reading of the shared declaration
// shape, so a section the core shares is read here exactly as the declaration reads
// it, and only the application section stays opaque.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/state"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/spec"
	"github.com/SpellingDragon/tagent/tool/subagent"

	"github.com/openai/openai-go/option"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// hostDocument is the host document dialect one Boot call decodes: the
// declaration-shaped sections the core shares beside the provider registry, the
// governance section and the application section a deployment owns. The
// governance and application sections are carried opaquely, so the core reads no
// application concept out of them.
type hostDocument struct {
	Entry          string                          `yaml:"entry"`
	Provider       string                          `yaml:"provider"`
	Model          string                          `yaml:"model"`
	PromptDir      string                          `yaml:"prompt_dir"`
	Providers      map[string]hostProvider         `yaml:"providers"`
	Agents         map[string]hostAgent            `yaml:"agents"`
	Storage        hostStorage                     `yaml:"storage"`
	TimeoutSeconds int                             `yaml:"request_timeout_seconds"`
	MCPServers     map[string]config.MCPServerSpec `yaml:"mcp_servers"`
	Governance     map[string]any                  `yaml:"governance"`
	App            map[string]any                  `yaml:"app"`
	Telemetry      config.TelemetrySpec            `yaml:"telemetry"`

	once      sync.Once
	mapped    document.Document
	mappedErr error
}

// hostStorage is the top-level storage section of a host document.
type hostStorage struct {
	Type string `yaml:"type"`
	Root string `yaml:"root"`
}

// hostProvider is one entry of the provider registry: the protocol the
// connection speaks, the endpoint it reaches and the environment variable
// holding its key.
type hostProvider struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

// hostAgent is one agent section of a host document.
type hostAgent struct {
	Provider     string                `yaml:"provider"`
	Model        string                `yaml:"model"`
	SystemPrompt hostPrompt            `yaml:"system_prompt"`
	Tools        []hostToolRef         `yaml:"tools"`
	SkillDirs    []string              `yaml:"skill_dirs"`
	ReadRoot     string                `yaml:"read_root"`
	FinalRound   *hostFinalRound       `yaml:"final_round"`
	Memory       config.MemorySpec     `yaml:"memory"`
	Meditation   config.MeditationSpec `yaml:"meditation"`
	File         config.FileSpec       `yaml:"file"`
	Exec         config.ExecSpec       `yaml:"exec"`

	Temperature           *float64 `yaml:"temperature"`
	ThinkingEnabled       *bool    `yaml:"thinking_enabled"`
	ReasoningEffort       string   `yaml:"reasoning_effort"`
	MaxToolIterations     int      `yaml:"max_tool_iterations"`
	MaxTokens             int      `yaml:"max_tokens"`
	ProviderContextWindow *int     `yaml:"provider_context_window"`
	OutputReserve         *int     `yaml:"output_reserve"`
	CompressThreshold     float64  `yaml:"compress_threshold"`
	KeepRecentTasks       int      `yaml:"keep_recent_tasks"`
}

// hostPrompt is the prompt section of one agent: the declared files resolved
// beside the document's prompt directory.
type hostPrompt struct {
	Files []string `yaml:"files"`
}

// hostFinalRound is the watchdog budget one agent declares.
type hostFinalRound struct {
	MaxRounds      int     `yaml:"max_rounds"`
	BudgetSeconds  float64 `yaml:"budget_seconds"`
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	Prompt         string  `yaml:"prompt"`
}

// hostToolRef is one tool reference of an agent declaration: a tool instance
// the host contributes, or a delegation to another declared agent.
type hostToolRef struct {
	Kind            string   `yaml:"kind"`
	ID              string   `yaml:"id"`
	Agent           string   `yaml:"agent"`
	DescriptionFile string   `yaml:"description_file"`
	EventParams     []string `yaml:"event_params"`
}

// loadHostDocument reads and strictly decodes the configuration source one Boot
// call is started with; an unknown key stops the startup instead of being read
// past.
func loadHostDocument(path string) (*hostDocument, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrHostConfigMissing
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tagent: read %s: %w", path, err)
	}
	doc := &hostDocument{}
	if err := config.DecodeStrict(path, raw, doc); err != nil {
		return nil, fmt.Errorf("tagent: %w", err)
	}
	if strings.TrimSpace(doc.PromptDir) == "" {
		doc.PromptDir = "."
	}
	return doc, nil
}

// document maps the host sections onto the declaration-shaped section the core
// shares, once per document.
func (d *hostDocument) document() (document.Document, error) {
	d.once.Do(func() { d.mapped, d.mappedErr = d.mapDocument() })
	return d.mapped, d.mappedErr
}

// Declaration returns the organization declaration the host assembles. The
// telemetry section stays the host's own dialect: the core reads no observation
// concept out of the declaration, so the section never enters it.
func (d *hostDocument) Declaration() (*config.Declaration, error) {
	mapped, err := d.document()
	if err != nil {
		return nil, err
	}
	decl, err := mapped.Declaration()
	if err != nil {
		return nil, err
	}
	return decl, nil
}

// mapDocument builds the declaration-shaped document of the host.
func (d *hostDocument) mapDocument() (document.Document, error) {
	agents := make(map[string]config.AgentSpec, len(d.Agents))
	for id, entry := range d.Agents {
		finalRound := entry.finalRound()
		if finalRound.MaxRounds == 0 && entry.MaxToolIterations > 0 {
			finalRound.MaxRounds = entry.MaxToolIterations
		}
		spec := config.AgentSpec{
			ID:                    id,
			Model:                 d.modelName(id),
			FinalRound:            finalRound,
			Temperature:           entry.Temperature,
			ThinkingEnabled:       entry.ThinkingEnabled,
			ReasoningEffort:       entry.ReasoningEffort,
			MaxToolIterations:     entry.MaxToolIterations,
			MaxTokens:             entry.MaxTokens,
			ProviderContextWindow: derefInt(entry.ProviderContextWindow),
			OutputReserve:         derefInt(entry.OutputReserve),
			CompressThreshold:     entry.CompressThreshold,
			KeepRecentTasks:       entry.KeepRecentTasks,
			Memory:                entry.Memory,
			Meditation:            entry.Meditation,
			File:                  entry.File,
			Exec:                  entry.Exec,
		}
		files, err := d.promptFiles(id)
		if err != nil {
			return document.Document{}, err
		}
		spec.Files = files
		refs, err := d.toolRefs(id)
		if err != nil {
			return document.Document{}, err
		}
		spec.Tools = refs
		agents[id] = spec
	}
	return document.Document{Entry: d.Entry, Agents: agents, Servers: d.MCPServers}, nil
}

// ErrUnknownAgentReference reports an agent reference that names no declared
// agent.
var ErrUnknownAgentReference = errors.New("tagent: the agent reference names no declared agent")

// ErrUnknownHostTool reports a declared tool this host does not contribute.
var ErrUnknownHostTool = errors.New("tagent: the declared tool is not contributed by this host")

// toolRefs resolves the tool references one declared agent carries.
func (d *hostDocument) toolRefs(id string) ([]config.ToolRef, error) {
	refs := make([]config.ToolRef, 0, len(d.Agents[id].Tools))
	for _, ref := range d.Agents[id].Tools {
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" && strings.TrimSpace(ref.Agent) != "" {
			kind = "agent"
		}
		switch kind {
		case "tool":
			if !hostContributes(ref.ID) {
				return nil, fmt.Errorf("%w: %q declared by %q", ErrUnknownHostTool, ref.ID, id)
			}
			refs = append(refs, config.ToolRef{Name: ref.ID})
		case "agent":
			target := strings.TrimSpace(ref.Agent)
			if _, ok := d.Agents[target]; target == "" || !ok {
				return nil, fmt.Errorf("%w: %q referenced by %q", ErrUnknownAgentReference, target, id)
			}
			params := map[string]string{subagent.ParamAgent: target}
			if file := strings.TrimSpace(ref.DescriptionFile); file != "" {
				params[subagent.ParamDescriptionFile] = filepath.Join(d.PromptDir, file)
			}
			if len(ref.EventParams) > 0 {
				params[subagent.ParamEventParams] = strings.Join(ref.EventParams, ",")
			}
			name := target
			if declared := strings.TrimSpace(ref.ID); declared != "" {
				name = declared
			}
			refs = append(refs, config.ToolRef{Name: name, Params: params})
		default:
			return nil, fmt.Errorf("tagent: agent %q declares tool kind %q, which this host does not wire", id, ref.Kind)
		}
	}
	return refs, nil
}

// hostToolIDs lists the tool ids the host assembly accepts in a declaration:
// the instances the host contributes beside the presets the core assembles.
var hostToolIDs = append(append([]string{
	hostSendToolID,
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
	knowledge.SkillSearchToolName,
	knowledge.SkillLoadToolName,
	spec.ToolName,
}, tagentfile.Names()...),
	tagentexec.PresetName,
	subagent.CallSubagentRef,
	mcpgw.ToolNameMCPCall,
	mcpgw.ToolNameMCPDiscover,
)

// hostContributes reports whether the host assembly accepts one tool id.
func hostContributes(id string) bool {
	for _, name := range hostToolIDs {
		if name == id {
			return true
		}
	}
	return false
}

// finalRound reports the watchdog budget of one agent section.
func (a hostAgent) finalRound() config.FinalRoundSpec {
	if a.FinalRound == nil {
		return config.FinalRoundSpec{}
	}
	return config.FinalRoundSpec{
		MaxRounds:      a.FinalRound.MaxRounds,
		BudgetSeconds:  a.FinalRound.BudgetSeconds,
		TimeoutSeconds: a.FinalRound.TimeoutSeconds,
		Prompt:         a.FinalRound.Prompt,
	}
}

// promptFiles reads the prompt files one declared agent is assembled from.
func (d *hostDocument) promptFiles(id string) ([]config.FileRef, error) {
	files, absent, err := document.PromptFiles(d.PromptDir, d.Agents[id].SystemPrompt.Files)
	if err != nil {
		return nil, fmt.Errorf("tagent: %w", err)
	}
	for _, path := range absent {
		assemblyInfo("tagent: the declared prompt file is absent and skipped: %s", path)
	}
	return files, nil
}

// modelName reports the model one agent runs against, and the document default
// beside no agent-level declaration.
func (d *hostDocument) modelName(id string) string {
	if name := strings.TrimSpace(d.Agents[id].Model); name != "" {
		return name
	}
	return strings.TrimSpace(d.Model)
}

// validateMeditationTools refuses a document whose reflection would deliver through a
// face it does not carry. For the retired independent curation agent, the
// enabled reflection with a declared deliver_to white list must itself declare
// the one cross-session communication tool, because the delivery is the
// reflection turn's own decision and no mechanism writes a body in its place.
//
// Host self-reflection is different: the entry and reflection sessions share one
// binding, so the meditation plugin grants the send tool to the reflection
// session alone (session-scoped), and the entry tool face stays free of it. A
// declarative grant is neither required nor permitted here, so there is no
// misplacement to refuse; an enabled reflection always reaches a send face.
// A reflection declared beside no white list delivers nothing and is not judged
// in either form.
func (d *hostDocument) validateMeditationTools(plan *hostMeditationPlan) error {
	if plan == nil || len(plan.targets) == 0 {
		return nil
	}
	if plan.selfReflection {
		return nil
	}
	if !d.declaresTool(plan.agent, hostSendToolID) {
		return fmt.Errorf("%w: the meditation session %q delivers to %d declared target(s) beside no %q tool, so no reflection turn could ever write one",
			ErrHostMeditationToolMisplaced, plan.agent, len(plan.targets), hostSendToolID)
	}
	return nil
}

// riskRules reads the declared risk table of the governance section: the
// ordered entries the boot opens its policy over. A section beside no table
// declares no rule, so a document that never named one keeps the behavior it
// had. Every entry names a tool (or a prefix) and one declared grade; a key, a
// value or a grade the table does not name is refused at boot instead of being
// read past.
func (d *hostDocument) riskRules() ([]governance.ToolRiskRule, error) {
	raw, held := d.Governance["risk_rules"]
	if !held || raw == nil {
		return nil, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("tagent: governance risk_rules is not a list")
	}
	rules := make([]governance.ToolRiskRule, 0, len(entries))
	for i, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] is not a mapping", i)
		}
		var rule governance.ToolRiskRule
		for key, value := range fields {
			switch key {
			case "tool":
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].tool is not a string", i)
				}
				rule.Tool = strings.TrimSpace(text)
			case "prefix":
				flag, ok := value.(bool)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].prefix is not a bool", i)
				}
				rule.Prefix = flag
			case "level":
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].level is not a string", i)
				}
				rule.Level = plugin.RiskLevel(strings.TrimSpace(text))
			default:
				return nil, fmt.Errorf("tagent: governance risk_rules[%d] names the unknown key %q", i, key)
			}
		}
		if rule.Tool == "" {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] names no tool", i)
		}
		if rule.Level.Rank() == 0 {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] (%s) carries the unknown risk grade %q", i, rule.Tool, rule.Level)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// declaresTool reports whether one agent section declares one host tool.
func (d *hostDocument) declaresTool(id, name string) bool {
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind == "tool" && ref.ID == name {
			return true
		}
	}
	return false
}

// nativeAgents constructs the live pieces every declared agent is assembled
// with.
func (d *hostDocument) nativeAgents(ctx context.Context, client *http.Client, recallPort *RecallPort, meditationPlan *hostMeditationPlan, meditationPort *hostMeditationPort, reentryPort hostSendReentry) (map[event.AgentID]NativeAgent, error) {
	decl, err := d.Declaration()
	if err != nil {
		return nil, err
	}
	out := make(map[event.AgentID]NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		native, err := d.nativeAgent(ctx, spec.ID, client, recallPort, meditationPlan, meditationPort, reentryPort)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = native
	}
	return out, nil
}

// nativeAgent constructs the live pieces one declared agent is assembled with:
// the model the provider registry of this document names beside the tools the
// declaration contributes. Boot builds every native of the first generation
// through it, and a reload builds an agent its document adds through the very
// same construction, over the ports the boot bound to the run.
func (d *hostDocument) nativeAgent(ctx context.Context, id string, client *http.Client, recallPort *RecallPort, plan *hostMeditationPlan, meditationPort *hostMeditationPort, reentryPort hostSendReentry) (NativeAgent, error) {
	native, err := d.modelFor(id, client)
	if err != nil {
		return NativeAgent{}, err
	}
	tools, err := d.toolsFor(ctx, id, recallPort, meditationPort, reentryPort)
	if err != nil {
		return NativeAgent{}, err
	}
	// Host self-reflection shares one binding between the entry and its
	// dedicated reflection session. Mark that session and the cross-session
	// send tool the plugin granted to it, so the runtime filters send out of
	// every other (entry/business) session's request face while the reflection
	// turn keeps the one write face its lesson delivery needs. A declarative
	// send stays a normal business-face tool: only the plugin-granted one
	// (the agent declared no send) is reflection-only, or a fixture naming its
	// own send would lose it from its business turns.
	var reflectionSession event.SessionID
	var reflectionOnly []string
	if plan != nil && plan.reflectionGrantsSend(id) && !d.declaresTool(id, hostSendToolID) {
		reflectionSession = event.SessionID(plan.session)
		reflectionOnly = []string{hostSendToolID}
	}
	return NativeAgent{Model: native, Tools: tools, reflectionSession: reflectionSession, reflectionOnly: reflectionOnly}, nil
}

// toolsFor constructs the tool instances the host contributes for one agent;
// the core owns the execution, MCP and delegation families of the declaration.
// The recall set is built before the run is assembled, so its durable query
// and original store are wired to the port itself (it forwards to the faces Bind
// attaches) and the parent reader comes from the port's read face, so
// recall_trace walks the parent chain already on the event chain.
// - One tool answers every task and peer-session destination, and every agent that declares it carries both dispatch faces — the meditation session included, because a reflection turn delivers its summary through this very tool (design D15 DEL-2).
// - The delivery face itself refuses a sender the document's white list does not authorize, so a grant is never an authorization of its own.
// - Host self-reflection contributes the cross-session send tool through the meditation plugin, not through the agent-level declaration; the runtime scopes the tool to the dedicated reflection session alone.
// - A plugin-granted send that no declaration named is appended to the contributed tools after the declared list, and a declared send is never contributed twice.
func (d *hostDocument) toolsFor(ctx context.Context, id string, recallPort *RecallPort, meditationPort *hostMeditationPort, reentryPort hostSendReentry) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolIDs))
	grantSend := meditationPort != nil && meditationPort.plan != nil && meditationPort.plan.reflectionGrantsSend(id)
	declaresSend := d.declaresTool(id, hostSendToolID)
	if declaresSend || grantSend {
		available[hostSendToolID] = &hostSendTool{reentry: reentryPort, session: meditationPort}
	}
	if d.declaresRecall(id) {
		reads := recallPort.Reads()
		reads.Table = recallPort
		reads.Originals = recallPort
		for _, candidate := range recall.New(reads, memory.PartitionsOf(id, d.Agents[id].Memory.ReadNamespaces)) {
			available[candidate.Declaration().Name] = candidate
		}
	}
	if d.declaresKnowledge(id) {
		surface, err := d.knowledgeTools(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, candidate := range surface {
			available[candidate.Declaration().Name] = candidate
		}
	}
	planRoot := ""
	if d.declaresTool(id, spec.ToolName) {
		backend := d.planBackend()
		available[spec.ToolName] = spec.NewTool(backend)
		planRoot = backend.WorkspaceRoot()
		if err := os.MkdirAll(planRoot, 0o755); err != nil {
			return nil, fmt.Errorf("tagent: create the plan workspace %s: %w", planRoot, err)
		}
	}
	if d.declaresFile(id) {
		surface, err := d.planFileTools(ctx, id, planRoot)
		if err != nil {
			return nil, err
		}
		for _, candidate := range surface {
			available[candidate.Declaration().Name] = candidate
		}
	}
	tools := make([]trpctool.CallableTool, 0, len(available))
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind != "tool" {
			continue
		}
		if tool, ok := available[ref.ID]; ok {
			tools = append(tools, tool)
		}
	}
	if grantSend && !declaresSend {
		tools = append(tools, available[hostSendToolID])
	}
	return tools, nil
}

// planBackend reports the plan backend the plan agent's work is written
// through.
func (d *hostDocument) planBackend() spec.Backend {
	return spec.NewOpenSpecBackend()
}

// planFileTools builds the file surface one agent's declaration grants. The
// write root is the plan workspace when the agent declares the plan tool, and
// the declared base directory otherwise.
func (d *hostDocument) planFileTools(ctx context.Context, id, writeRoot string) ([]trpctool.CallableTool, error) {
	granted := make([]string, 0, len(tagentfile.Names()))
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind != "tool" {
			continue
		}
		for _, name := range tagentfile.Names() {
			if ref.ID == name {
				granted = append(granted, name)
			}
		}
	}
	if len(granted) == 0 {
		return nil, nil
	}
	base := writeRoot
	if base == "" {
		base = d.Agents[id].File.BaseDir
	}
	readRoots, err := hostReadRoots(d.Agents[id].File.ReadRoots)
	if err != nil {
		return nil, fmt.Errorf("tagent: resolve the read roots of agent %q: %w", id, err)
	}
	surface, err := tagentfile.New(tagentfile.Options{BaseDir: base, ReadRoots: readRoots, Tools: granted})
	if err != nil {
		return nil, fmt.Errorf("tagent: construct the file surface of agent %q: %w", id, err)
	}
	tools := make([]trpctool.CallableTool, 0, len(granted))
	for _, candidate := range surface.Tools(ctx) {
		callable, ok := candidate.(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("tagent: a file tool of agent %q carries no native call surface", id)
		}
		tools = append(tools, callable)
	}
	return tools, nil
}

// hostReadRoots resolves the extra read roots one file declaration names.
func hostReadRoots(declared []string) ([]string, error) {
	roots := make([]string, 0, len(declared))
	for _, root := range declared {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", trimmed, err)
		}
		roots = append(roots, absolute)
	}
	return roots, nil
}

// declaresFile reports whether one agent section declares any file tool.
func (d *hostDocument) declaresFile(id string) bool {
	for _, name := range tagentfile.Names() {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// declaresRecall reports whether one agent section declares any recall tool.
func (d *hostDocument) declaresRecall(id string) bool {
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// declaresKnowledge reports whether one agent section declares any knowledge
// tool.
func (d *hostDocument) declaresKnowledge(id string) bool {
	for _, name := range []string{knowledge.SkillSearchToolName, knowledge.SkillLoadToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// knowledgeTools constructs the knowledge surface of one declared agent: its
// skill roots, the read root its file reads are confined to and the ledger the
// loaded bodies publish to, so the request boundary carries a loaded body into
// that agent's next request instead of answering a name alone.
func (d *hostDocument) knowledgeTools(ctx context.Context, id string) ([]trpctool.CallableTool, error) {
	opts := knowledge.Options{
		ReadRoot: d.readRoot(id),
		Sink:     NewSkillLoadSink(event.AgentID(id)),
	}
	if roots := d.skillRoots(id); len(roots) > 0 {
		repository, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, fmt.Errorf("tagent: open the skill repository of agent %q: %w", id, err)
		}
		opts.Skills = repository
	}
	return knowledge.New(ctx, opts)
}

// skillRoots reports the declared skill roots of one agent section.
func (d *hostDocument) skillRoots(id string) []string {
	roots := make([]string, 0, len(d.Agents[id].SkillDirs))
	for _, dir := range d.Agents[id].SkillDirs {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	return roots
}

// readRoot reports the directory the knowledge reads of one agent are confined
// to.
func (d *hostDocument) readRoot(id string) string {
	if root := strings.TrimSpace(d.Agents[id].ReadRoot); root != "" {
		return root
	}
	return "."
}

// requestTimeout reports the HTTP timeout the models of this document run
// under.
func (d *hostDocument) requestTimeout() time.Duration {
	if d.TimeoutSeconds > 0 {
		return time.Duration(d.TimeoutSeconds) * time.Second
	}
	return 10 * time.Minute
}

// modelRef reports the provider registry key and the model name one declared
// agent resolves in this document, before either half is built into a model.
func (d *hostDocument) modelRef(id string) (string, string) {
	providerName := strings.TrimSpace(d.Agents[id].Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(d.Provider)
	}
	return providerName, d.modelName(id)
}

// modelFor constructs the native model of one declared agent from its provider
// entry. Chat telemetry stays on the entry agent's model only.
func (d *hostDocument) modelFor(id string, client *http.Client) (trpcmodel.Model, error) {
	providerName, modelName := d.modelRef(id)
	return d.buildModel(id, providerName, modelName, client)
}

// buildModel constructs one native model from an explicit provider and model
// name: the reentrant step both Boot and a lease rebuild run, so a model
// replacement reads the very construction the first generation read. The
// adapter's cache reorder is off: it would lift the trailing dynamic system
// section the loop appends, and the layout already keeps the stable prefix
// first without it.
func (d *hostDocument) buildModel(id, providerName, modelName string, client *http.Client) (trpcmodel.Model, error) {
	providerName = strings.TrimSpace(providerName)
	provider, ok := d.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("tagent: the host document declares no provider %q", providerName)
	}
	if protocol := strings.TrimSpace(provider.Provider); protocol != "" && protocol != "openai" {
		return nil, fmt.Errorf("tagent: provider %q runs protocol %q, which this host does not wire", providerName, protocol)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, fmt.Errorf("tagent: agent %q declares no model", id)
	}
	keyEnv := strings.TrimSpace(provider.APIKeyEnv)
	if keyEnv == "" {
		return nil, fmt.Errorf("tagent: provider %q names no api_key_env", providerName)
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return nil, fmt.Errorf("tagent: provider %q requires %s in the environment", providerName, keyEnv)
	}
	endpoint := strings.TrimSpace(provider.APIEndpoint)
	if id == strings.TrimSpace(d.Entry) {
		if override := os.Getenv(hostAPIEndpointEnv); override != "" {
			endpoint = override
		}
	}
	opts := []openai.Option{
		openai.WithAPIKey(key),
		openai.WithOptimizeForCache(false),
	}
	if id == strings.TrimSpace(d.Entry) {
		opts = append(opts, openai.WithChatTelemetry(true))
	}
	if endpoint != "" {
		opts = append(opts, openai.WithBaseURL(endpoint))
	}
	if client != nil {
		opts = append(opts, openai.WithOpenAIOptions(option.WithHTTPClient(client)))
	}
	return openai.New(modelName, opts...), nil
}

// hostAPIEndpointEnv names the environment variable that overrides the endpoint
// of the entry agent's provider.
const hostAPIEndpointEnv = "TAGENT_API_ENDPOINT"

// reconcileBootConfig is the startup reconciliation of one host: it reads the
// configuration chain the storage tier already holds and decides which
// declaration this process serves before the first publication runs.
//
// - no declaration fact: the source file is published through the sole entry as the first generation;
// - the file names the newest declaration: it is adopted at the recorded generation, so a consistent restart publishes nothing;
// - the file is newer than the newest declaration: it is published through the same entry after the recorded generation;
// - the chain is newer than the file: the chain declaration is adopted and the lagging file is recorded by name.
//
// The reconciliation never writes a second publication path: every generation it takes into service is either the ledger's own record or the outcome of the one publication entry.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func reconcileBootConfig(ctx context.Context, run *Run, configPath string, doc *hostDocument, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
	if run == nil || decl == nil {
		return fmt.Errorf("tagent: the startup reconciliation carries no run or declaration")
	}
	fingerprint := decl.FactKey()
	if fingerprint == "" {
		return fmt.Errorf("tagent: the source declaration answers no canonical fingerprint")
	}
	declFact, chainDecl, declared, err := run.latestDeclaration(ctx)
	if err != nil {
		return err
	}
	record, published, err := run.latestPublishedRecord(ctx)
	if err != nil {
		return err
	}
	if !declared {
		return publishBootDeclaration(ctx, run, configPath, doc, decl, agents, 0, "")
	}
	if fingerprint == declFact.Fingerprint {
		if published && record.Fingerprint == declFact.Fingerprint {
			return run.adoptConfigPublication(ctx, chainDecl, agents, record)
		}
		return publishBootDeclaration(ctx, run, configPath, doc, chainDecl, agents, uint64(record.Generation), record.Fingerprint)
	}
	newer, err := fileNewerThan(configPath, declFact.At)
	if err != nil {
		return err
	}
	if newer {
		if published && record.Fingerprint == declFact.Fingerprint {
			if err := run.adoptConfigPublication(ctx, chainDecl, agents, record); err != nil {
				return err
			}
		}
		return publishBootDeclaration(ctx, run, configPath, doc, decl, agents, uint64(record.Generation), record.Fingerprint)
	}
	if published && record.Fingerprint == declFact.Fingerprint {
		if err := run.adoptConfigPublication(ctx, chainDecl, agents, record); err != nil {
			return err
		}
	} else {
		if err := publishBootDeclaration(ctx, run, configPath, doc, chainDecl, agents, uint64(record.Generation), record.Fingerprint); err != nil {
			return err
		}
	}
	_, err = run.recordPublicationAttempt(ctx, publicationAttempt{
		Result:              publicationResultUnchanged,
		Fingerprint:         declFact.Fingerprint,
		Generation:          uint64(run.publisher.Current().Generation),
		PreviousGeneration:  uint64(record.Generation),
		PreviousFingerprint: record.Fingerprint,
		Reason:              fmt.Sprintf("tagent: the source file %s is older than the chain declaration; the chain stays the truth", configPath),
		Models:              modelVersionsOf(doc, chainDecl),
		Dimensions:          skippedDimensions(chainDecl),
	})
	return err
}

// publishBootDeclaration commits the declaration fact of the declaration this
// boot publishes and runs the one publication entry over it. The generation the
// chain's last record assigned is the generation the candidate is meant to
// follow; the window records every model version the new declaration puts in
// effect.
func publishBootDeclaration(ctx context.Context, run *Run, configPath string, doc *hostDocument, decl *config.Declaration, agents map[event.AgentID]NativeAgent, previousGeneration uint64, previousFingerprint string) error {
	generation := previousGeneration + 1
	if _, err := run.recordDeclarationFact(ctx, decl, generation); err != nil {
		return err
	}
	_, chainDecl, held, err := run.latestDeclaration(ctx)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("tagent: the declaration fact of %s is not readable after its commit", configPath)
	}
	window := publicationWindow{
		previous:            state.Generation(previousGeneration),
		previousFingerprint: previousFingerprint,
		models:              modelVersionsOf(doc, chainDecl),
		dimensions:          appliedModelDimensions(chainDecl),
		at:                  time.Now().UTC(),
	}
	outcome, err := run.publishDeclared(ctx, chainDecl, agents, window)
	if err != nil {
		_, recordErr := run.recordPublicationAttempt(ctx, publicationAttempt{
			Result:              publicationResultGovernanceRejected,
			Fingerprint:         chainDecl.FactKey(),
			Generation:          previousGeneration,
			PreviousGeneration:  previousGeneration,
			PreviousFingerprint: previousFingerprint,
			Reason:              err.Error(),
			Models:              window.models,
			Dimensions:          window.dimensions,
		})
		return errors.Join(err, recordErr)
	}
	result := publicationResultPublished
	if outcome.Result == state.PublishUnchanged {
		result = publicationResultUnchanged
	}
	_, err = run.recordPublicationAttempt(ctx, publicationAttempt{
		Result:              result,
		Fingerprint:         chainDecl.FactKey(),
		Generation:          outcome.Publication.Generation,
		PreviousGeneration:  previousGeneration,
		PreviousFingerprint: previousFingerprint,
		Models:              window.models,
		Dimensions:          window.dimensions,
	})
	return err
}

// fileNewerThan reports whether the source file was modified after the named
// chain moment. The comparison is the file's own modification time against the
// declaration fact's recorded time: a restart whose file is not newer keeps the
// chain instead of overwriting it with a rolled-back edit surface.
func fileNewerThan(configPath string, chainAt time.Time) (bool, error) {
	info, err := os.Stat(configPath)
	if err != nil {
		return false, fmt.Errorf("tagent: stat the configuration source %s: %w", configPath, err)
	}
	return info.ModTime().After(chainAt), nil
}

// appliedModelDimensions names the model dimension of every agent as applied:
// the generation a publication takes into service installs the model version
// the declaration names beside it.
func appliedModelDimensions(decl *config.Declaration) map[string]string {
	out := make(map[string]string, len(decl.Agents))
	for _, spec := range decl.Agents {
		out[spec.ID+"|"+event.ConfigChangeDimensionModel] = "applied"
	}
	return out
}

// skippedDimensions names the model dimension of every agent as skipped: the
// declaration did not move it in the window this record describes.
func skippedDimensions(decl *config.Declaration) map[string]string {
	out := make(map[string]string, len(decl.Agents))
	for _, spec := range decl.Agents {
		out[spec.ID+"|"+event.ConfigChangeDimensionModel] = "skipped"
	}
	return out
}

// derefInt reads an optional YAML integer: an absent key keeps the zero value.
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
