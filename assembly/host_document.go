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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
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

// nativeModels constructs the shared native model of every declared agent: the
// one piece of an agent's native material that survives every generation switch.
// Boot builds it for the first generation, and a reload builds the model of an
// agent its document adds; a model the document moves afterwards moves through
// the agent's lease, never through a second construction here.
func (d *hostDocument) nativeModels(client *http.Client) (map[event.AgentID]model.Model, error) {
	decl, err := d.Declaration()
	if err != nil {
		return nil, err
	}
	out := make(map[event.AgentID]model.Model, len(decl.Agents))
	for _, spec := range decl.Agents {
		native, err := d.modelFor(spec.ID, client)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = native
	}
	return out, nil
}

// face constructs the per-generation native face one declared agent is
// assembled with: the tools the host contributes for the agent this document
// declares beside the session-scoped reflection face its meditation section
// declares. The model is not part of the face — it is the shared native the
// generation samples through.
//
// Boot builds the face of the first generation through it, and a reload builds
// the face of every declared agent through the very same construction over the
// ports the boot bound to the run, so one document answers one face whichever
// generation reads it.
func (d *hostDocument) face(ctx context.Context, id string, recallPort *RecallPort, plan *meditation.Plan, meditationPort *hostMeditationPort, reentryPort hostSendReentry) (NativeAgent, error) {
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
	if plan.GrantsSend(id) && !d.declaresTool(id, hostSendToolID) {
		reflectionSession = event.SessionID(plan.Session)
		reflectionOnly = []string{hostSendToolID}
	}
	return NativeAgent{Tools: tools, reflectionSession: reflectionSession, reflectionOnly: reflectionOnly}, nil
}

// GenHostSpec is the host document dialect one candidate generation is
// published under: the values a host document states beside the shared
// declaration the core reads. The generation-scoped consumers of the host read
// the spec of the generation they build here, so one builder never answers for
// another document.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
type GenHostSpec struct {
	// Document is the host document the generation was read from: the tool
	// declarations, skill roots, read roots and file faces the generation's
	// native face is built from.
	Document *hostDocument
	// Timeout is the request timeout the models of the generation run under.
	Timeout time.Duration
	// RiskRules is the ordered risk table the governance face of the generation
	// opens over.
	RiskRules []governance.ToolRiskRule
	// Telemetry is the observation section the generation declares.
	Telemetry config.TelemetrySpec
}

// specOfDocument reports the host dialect one loaded document states.
func specOfDocument(doc *hostDocument) (GenHostSpec, error) {
	if doc == nil {
		return GenHostSpec{}, fmt.Errorf("tagent: the host dialect is read from no document")
	}
	rules, err := doc.riskRules()
	if err != nil {
		return GenHostSpec{}, err
	}
	return GenHostSpec{Document: doc, Timeout: doc.requestTimeout(), RiskRules: rules, Telemetry: doc.Telemetry}, nil
}

// hostDialect is the content one host dialect digest covers: the sections a
// generation rebuilds that the shared declaration does not carry.
type hostDialect struct {
	RiskRules []governance.ToolRiskRule   `json:"risk_rules"`
	Telemetry config.TelemetrySpec        `json:"telemetry"`
	Agents    map[string]hostDialectAgent `json:"agents"`
}

// hostDialectAgent is the per-agent part of a host dialect digest: the skill
// roots and the read root. The tool list and the file surface are left out
// because the shared declaration carries both and its own fingerprint answers
// them.
type hostDialectAgent struct {
	SkillDirs []string `json:"skill_dirs"`
	ReadRoot  string   `json:"read_root"`
}

// hostDialectDigest reports the stable digest of the host dialect one loaded
// document states: the risk table, the observation section and the skill and
// read face of every declared agent. The shared declaration does not carry those
// sections, so its fingerprint answers the same value for a document that moved
// only them; the digest is what tells that move from a publication of the same
// configuration again. A document that declares none of them answers the empty
// digest, so its publication fingerprint stays the declaration fingerprint.
//
// The request timeout and the storage tier are not covered: both are read from
// the dialect in effect at the moment they are used, so a reload takes a moved
// value into service without publishing a generation for it.
func hostDialectDigest(spec GenHostSpec) (string, error) {
	if spec.Document == nil {
		return "", fmt.Errorf("tagent: the host dialect is read from no document")
	}
	agents := make(map[string]hostDialectAgent, len(spec.Document.Agents))
	declared := len(spec.RiskRules) > 0 || spec.Telemetry != (config.TelemetrySpec{})
	for id, agent := range spec.Document.Agents {
		if len(agent.SkillDirs) > 0 || agent.ReadRoot != "" {
			declared = true
		}
		agents[id] = hostDialectAgent{SkillDirs: agent.SkillDirs, ReadRoot: agent.ReadRoot}
	}
	if !declared {
		return "", nil
	}
	body, err := json.Marshal(hostDialect{
		RiskRules: spec.RiskRules,
		Telemetry: spec.Telemetry,
		Agents:    agents,
	})
	if err != nil {
		return "", fmt.Errorf("tagent: render the host dialect digest: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// genHostSpecs holds the host dialect of the candidate declarations in flight
// beside the dialect of the generation in effect. A candidate is claimed under
// the fingerprint of its own declaration, so a builder that is handed one
// declaration reads the document that declaration was loaded from and never the
// document another attempt carries.
type genHostSpecs struct {
	mu       sync.RWMutex
	inFlight map[string]GenHostSpec
	current  GenHostSpec
}

// newGenHostSpecs returns the empty claim table of one host.
func newGenHostSpecs() *genHostSpecs {
	return &genHostSpecs{inFlight: map[string]GenHostSpec{}}
}

// claim records the host dialect of one candidate declaration.
func (s *genHostSpecs) claim(decl *config.Declaration, spec GenHostSpec) {
	if s == nil || decl == nil {
		return
	}
	s.mu.Lock()
	s.inFlight[decl.FactKey()] = spec
	s.mu.Unlock()
}

// specOf reports the host dialect claimed for one candidate declaration.
func (s *genHostSpecs) specOf(decl *config.Declaration) (GenHostSpec, bool) {
	if s == nil || decl == nil {
		return GenHostSpec{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.inFlight[decl.FactKey()]
	return spec, ok
}

// drop ends the claim of one candidate: the generation it carried took effect
// (and became the dialect in effect) or never will.
func (s *genHostSpecs) drop(decl *config.Declaration) {
	if s == nil || decl == nil {
		return
	}
	s.mu.Lock()
	delete(s.inFlight, decl.FactKey())
	s.mu.Unlock()
}

// install records the host dialect that took effect.
func (s *genHostSpecs) install(spec GenHostSpec) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.current = spec
	s.mu.Unlock()
}

// timeout reports the request timeout in effect, and zero beside no document in
// effect at all.
func (s *genHostSpecs) timeout() time.Duration {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Timeout
}

// hostModels holds the shared native model of every declared agent: the one
// native piece a generation switch leaves standing. Boot installs the models of
// the first generation, a reload installs the model of an agent its document
// adds, and a model the document moves afterwards moves through the agent's
// lease instead.
type hostModels struct {
	mu     sync.RWMutex
	models map[event.AgentID]model.Model
}

// newHostModels returns the empty shared-model table of one host.
func newHostModels() *hostModels {
	return &hostModels{models: map[event.AgentID]model.Model{}}
}

// install records the shared model of one agent.
func (m *hostModels) install(id event.AgentID, native model.Model) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.models[id] = native
	m.mu.Unlock()
}

// modelOf reports the shared model of one agent.
func (m *hostModels) modelOf(id event.AgentID) (model.Model, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	native, ok := m.models[id]
	return native, ok
}

// hostFaceConsumerName is the registration name the host's native face consumer
// takes on the generation-scoped registry.
const hostFaceConsumerName = "host-face"

// hostFaces is the generation-scoped consumer of the host's own native face: the
// tools, the skill repository, the read root, the file surface and the
// session-scoped reflection face every declared agent carries in one generation.
// It rebuilds that face from the document the candidate generation was loaded
// from, composes it over the shared models, and answers the composed natives of
// the generation to the publication path.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
type hostFaces struct {
	specs      *genHostSpecs
	models     *hostModels
	recallPort *RecallPort
	meditation *hostMeditationPort
	reentry    hostSendReentry

	mu    sync.RWMutex
	faces map[state.Generation]map[event.AgentID]NativeAgent
}

// newHostFaces returns the face consumer of one host over the ports its boot
// bound to the run.
func newHostFaces(specs *genHostSpecs, models *hostModels, recallPort *RecallPort, meditation *hostMeditationPort, reentry hostSendReentry) *hostFaces {
	return &hostFaces{
		specs:      specs,
		models:     models,
		recallPort: recallPort,
		meditation: meditation,
		reentry:    reentry,
		faces:      map[state.Generation]map[event.AgentID]NativeAgent{},
	}
}

// registration reports the mount of the face consumer on the generation-scoped
// registry: the name its registration is keyed by beside the builder that
// reconstructs one generation's face.
func (f *hostFaces) registration() state.GenRegistration {
	return state.GenRegistration{Name: hostFaceConsumerName, BuildGen: f.build}
}

// build constructs the native face of one candidate generation and installs it
// as the face that generation's binding construction reads. The face holds no
// exclusive resource: every tool of it is a process-local instance over the
// document's own roots, so a generation retiring releases nothing here and only
// the installation itself is put back when the candidate leaves.
func (f *hostFaces) build(ctx context.Context, decl *config.Declaration, gen state.Generation) (state.GenResources, error) {
	if _, err := f.compose(ctx, decl, gen); err != nil {
		return state.GenResources{}, err
	}
	return state.GenResources{Restore: func() { f.drop(gen) }}, nil
}

// compose builds the composed natives of one declaration and installs them under
// the generation it publishes.
func (f *hostFaces) compose(ctx context.Context, decl *config.Declaration, gen state.Generation) (map[event.AgentID]NativeAgent, error) {
	spec, ok := f.specs.specOf(decl)
	if !ok {
		return nil, fmt.Errorf("tagent: no host document was claimed for the declaration %q", decl.FactKey())
	}
	out := make(map[event.AgentID]NativeAgent, len(decl.Agents))
	for _, spec2 := range decl.Agents {
		id := event.AgentID(spec2.ID)
		native, held := f.models.modelOf(id)
		if !held {
			return nil, fmt.Errorf("tagent: agent %q carries no shared model for generation %d", spec2.ID, gen)
		}
		face, err := spec.Document.face(ctx, spec2.ID, f.recallPort, f.meditation.planInEffect(), f.meditation, f.reentry)
		if err != nil {
			return nil, err
		}
		face.Model = native
		out[id] = face
	}
	f.install(gen, out)
	return out, nil
}

// install records the composed natives of one generation.
func (f *hostFaces) install(gen state.Generation, faces map[event.AgentID]NativeAgent) {
	f.mu.Lock()
	f.faces[gen] = faces
	f.mu.Unlock()
}

// drop removes the installation of one generation: the candidate it belonged to
// never took effect.
func (f *hostFaces) drop(gen state.Generation) {
	f.mu.Lock()
	delete(f.faces, gen)
	f.mu.Unlock()
}

// Face reports the composed natives of one generation, and false beside a
// generation whose face was never built.
func (f *hostFaces) Face(gen state.Generation) (map[event.AgentID]NativeAgent, bool) {
	if f == nil {
		return nil, false
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	faces, ok := f.faces[gen]
	return faces, ok
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
	plan := meditationPort.planInEffect()
	grantSend := plan.GrantsSend(id)
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
// - The host dialect of this process is the one its source file states: the sections the shared declaration does not carry — the skill roots, the read root, the tool declarations, the request timeout, the risk table and the observation face — are read from the file beside whichever declaration the chain takes into service, so a restart that adopts the chain still serves the deployment's own dialect.
// - An adopted generation rebuilds every generation-scoped consumer of its declaration: the adoption drives the same registry the publication path drives, so a restart beside an agreeing chain serves the consumers the document names — the tool face, the execution instance, the memory read namespaces, the reflection loop, the governance rules and the observation face — instead of the models alone, and the exclusive resources those consumers acquired retire with that generation.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func reconcileBootConfig(ctx context.Context, run *Run, configPath string, doc *hostDocument, decl *config.Declaration, agents map[event.AgentID]NativeAgent, specs *genHostSpecs, faces *hostFaces) error {
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
	dialect, err := specOfDocument(doc)
	if err != nil {
		return err
	}
	dialectKey, err := hostDialectDigest(dialect)
	if err != nil {
		return err
	}
	claim := func(target *config.Declaration) {
		if target != nil {
			specs.claim(target, dialect)
		}
	}
	defer func() {
		specs.drop(decl)
		specs.drop(chainDecl)
	}()
	adopt := func(target *config.Declaration, record state.PublicationRecord) error {
		claim(target)
		return adoptBootGeneration(ctx, run, target, agents, faces, record, dialectKey)
	}
	if !declared {
		claim(decl)
		return publishBootDeclaration(ctx, run, configPath, doc, decl, agents, 0, "", dialectKey)
	}
	if fingerprint == declFact.Fingerprint {
		if published && record.Fingerprint == publicationFingerprint(declFact.Fingerprint, dialectKey) {
			return adopt(chainDecl, record)
		}
		claim(chainDecl)
		return publishBootDeclaration(ctx, run, configPath, doc, chainDecl, agents, uint64(record.Generation), record.Fingerprint, dialectKey)
	}
	newer, err := fileNewerThan(configPath, declFact.At)
	if err != nil {
		return err
	}
	if newer {
		if published && record.Fingerprint == publicationFingerprint(declFact.Fingerprint, dialectKey) {
			if err := adopt(chainDecl, record); err != nil {
				return err
			}
		}
		claim(decl)
		return publishBootDeclaration(ctx, run, configPath, doc, decl, agents, uint64(record.Generation), record.Fingerprint, dialectKey)
	}
	if published && record.Fingerprint == publicationFingerprint(declFact.Fingerprint, dialectKey) {
		if err := adopt(chainDecl, record); err != nil {
			return err
		}
	} else {
		claim(chainDecl)
		if err := publishBootDeclaration(ctx, run, configPath, doc, chainDecl, agents, uint64(record.Generation), record.Fingerprint, dialectKey); err != nil {
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

// adoptBootGeneration takes one recorded generation into service at startup: the
// generation-scoped consumers rebuild their instances for the generation the
// record names, the composed native face of that generation is what the adopted
// binding construction reads, and the exclusive resources those consumers
// acquired are handed to the retirement ledger of the adopted generation, so the
// publication that supersedes it later frees them through the same drain.
//
// A consumer that refuses the declaration, a generation without a composed face
// and a refused adoption each leave the boot failed instead of serving a
// generation whose consumers were only partly built; the instances built so far
// put their faces back and release what they acquired.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func adoptBootGeneration(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent, faces *hostFaces, record state.PublicationRecord, dialectKey string) error {
	gen := state.Generation(record.Generation)
	built, err := run.genRegistry.Build(ctx, decl, gen)
	if err != nil {
		return err
	}
	composed, ok := faces.Face(gen)
	if !ok {
		built.Restore()
		return errors.Join(
			fmt.Errorf("tagent: the consumer side built no native face for the adopted generation %d", gen),
			built.Release(ctx),
		)
	}
	if err := run.adoptConfigPublication(ctx, decl, composed, record, dialectKey); err != nil {
		built.Restore()
		return errors.Join(err, built.Release(ctx))
	}
	return run.trackGenerationConsumers(ctx, gen, built)
}

// publishBootDeclaration commits the declaration fact of the declaration this
// boot publishes and runs the one publication entry over it. The generation the
// chain's last record assigned is the generation the candidate is meant to
// follow; the window records every model version the new declaration puts in
// effect.
func publishBootDeclaration(ctx context.Context, run *Run, configPath string, doc *hostDocument, decl *config.Declaration, agents map[event.AgentID]NativeAgent, previousGeneration uint64, previousFingerprint, dialectKey string) error {
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
		dialectKey:          dialectKey,
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

// telemetryFaceConsumerName and its siblings are the registration names the host's own consumers take on the generation-scoped registry: the observation face, the governance rules, the merged memory declaration and the reflection loop.
// - Each answers the builder of its per-generation instance and mounts through the one registry the assembly drives, so a reload that moves one of them takes effect as a new generation instead of waiting for a restart.
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
const (
	telemetryFaceConsumerName  = "telemetry"
	governanceFaceConsumerName = "governance"
	memoryFaceConsumerName     = "memory"
	meditationFaceConsumerName = "meditation"
)

// generationReplacement answers the resources one candidate generation acquired
// by replacing a face of the generation in effect: its registration frees the
// face it replaced, and the restore that runs when the candidate never takes
// effect puts the face in effect back and closes what the candidate built.
//
// - The replaced face is closed when the generation that replaced it retires, so a superseded face never serves a generation it was not built for.
// - A refused candidate closes only what it built: the face in effect is put back untouched, and the release of that same candidate finds the replacement already settled.
func generationReplacement(ctx context.Context, consumer string, gen state.Generation, previous, built func(context.Context) error, restore func()) state.GenResources {
	var mu sync.Mutex
	settled := false
	take := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if settled {
			return false
		}
		settled = true
		return true
	}
	stopCtx := context.WithoutCancel(ctx)
	var resources *state.Leases
	if previous != nil {
		resources = state.NewLeases()
		resources.Register(consumer, fmt.Sprintf("gen:%d", gen), func(context.Context) error {
			if !take() {
				return nil
			}
			return previous(stopCtx)
		})
	}
	return state.GenResources{Resources: resources, Restore: func() {
		if !take() {
			return
		}
		if restore != nil {
			restore()
		}
		if built != nil {
			_ = built(stopCtx)
		}
	}}
}

// telemetryFace is the observation face of one generation: the section it was
// built from beside the observer and the stop that ends the pipelines it
// started.
type telemetryFace struct {
	section  config.TelemetrySpec
	observer plugin.Observer
	stop     func(context.Context) error
}

// hostTelemetryFaces is the generation-scoped consumer of the host's observation
// face: the log observer a document declares, or the layered SDK observer its
// OTel switch starts. It rebuilds that face for every candidate generation from
// the observation section that generation is published under.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
//   - The observer beside the pipelines it started is process-private and dies with the generation: the face a candidate installed is closed when the generation that superseded it retires, and a refused candidate closes the face it built and puts the one in effect back.
//   - The section is compared before anything is built, so a generation that moves nothing reuses the observer in effect instead of opening a second pipeline beside it.
type hostTelemetryFaces struct {
	specs *genHostSpecs
	build func(ctx context.Context, section config.TelemetrySpec) (plugin.Observer, func(context.Context) error, error)
	run   *Run

	mu   sync.Mutex
	face telemetryFace
	held bool
}

// newHostTelemetryFaces returns the observation consumer of one host over the
// construction its document declares.
func newHostTelemetryFaces(specs *genHostSpecs, build func(ctx context.Context, section config.TelemetrySpec) (plugin.Observer, func(context.Context) error, error)) *hostTelemetryFaces {
	return &hostTelemetryFaces{specs: specs, build: build}
}

// bind attaches the run whose observation face every generation installs into.
func (f *hostTelemetryFaces) bind(run *Run) { f.run = run }

// boot installs the observation face the boot document declares and answers the
// observer of that face.
func (f *hostTelemetryFaces) boot(ctx context.Context, section config.TelemetrySpec) (plugin.Observer, error) {
	observer, stop, err := f.build(ctx, section)
	if err != nil {
		return nil, err
	}
	f.install(telemetryFace{section: section, observer: observer, stop: stop})
	return observer, nil
}

// install records one observation face as the face in effect and puts it on the
// run, answering the face it replaced beside whether a face was in effect.
func (f *hostTelemetryFaces) install(face telemetryFace) (telemetryFace, bool) {
	f.mu.Lock()
	previous, held := f.face, f.held
	f.face, f.held = face, true
	run := f.run
	f.mu.Unlock()
	if run != nil {
		run.installObserver(face.observer)
	}
	return previous, held
}

// restore puts one observation face back as the face in effect.
func (f *hostTelemetryFaces) restore(face telemetryFace) {
	f.mu.Lock()
	f.face = face
	run := f.run
	f.mu.Unlock()
	if run != nil {
		run.installObserver(face.observer)
	}
}

// close stops the observation pipeline of the face in effect, once.
func (f *hostTelemetryFaces) close(ctx context.Context) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	face, held := f.face, f.held
	f.face, f.held = telemetryFace{}, false
	f.mu.Unlock()
	if !held || face.stop == nil {
		return nil
	}
	return face.stop(ctx)
}

// registration reports the mount of the observation consumer on the
// generation-scoped registry.
func (f *hostTelemetryFaces) registration() state.GenRegistration {
	return state.GenRegistration{Name: telemetryFaceConsumerName, BuildGen: f.buildGeneration}
}

// buildGeneration rebuilds the observation face of one candidate generation: a
// section that did not move keeps the observer in effect, and a moved one starts
// its own pipelines beside the generation that declared it.
func (f *hostTelemetryFaces) buildGeneration(ctx context.Context, decl *config.Declaration, gen state.Generation) (state.GenResources, error) {
	spec, ok := f.specs.specOf(decl)
	if !ok {
		return state.GenResources{}, fmt.Errorf("tagent: no host document was claimed for the declaration %q", decl.FactKey())
	}
	f.mu.Lock()
	previous, held := f.face, f.held
	f.mu.Unlock()
	if held && reflect.DeepEqual(previous.section, spec.Telemetry) {
		return state.GenResources{Restore: func() {}}, nil
	}
	observer, stop, err := f.build(ctx, spec.Telemetry)
	if err != nil {
		return state.GenResources{}, err
	}
	replaced, _ := f.install(telemetryFace{section: spec.Telemetry, observer: observer, stop: stop})
	var previousStop func(context.Context) error
	if replaced.stop != nil {
		previousStop = func(ctx context.Context) error { return replaced.stop(ctx) }
	}
	var builtStop func(context.Context) error
	if stop != nil {
		builtStop = func(ctx context.Context) error { return stop(ctx) }
	}
	return generationReplacement(ctx, telemetryFaceConsumerName, gen, previousStop, builtStop,
		func() { f.restore(replaced) }), nil
}

// governanceFace is the governance rules of one generation: the risk table they
// were opened from beside the policy that rules under them. The approval book a
// critical call is parked on is the host's own and survives every generation, so
// the face owns no process resource beside it.
type governanceFace struct {
	rules  []governance.ToolRiskRule
	policy plugin.Governor
}

// hostGovernanceFaces is the generation-scoped consumer of the host's governance
// rules: the risk table a document declares, opened over the approval book of
// this host. It reopens that table for every candidate generation, so a reload
// that moves the rules takes effect without a restart.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
// - The risk table is compared before anything is opened, so a generation that moves nothing keeps the policy in effect instead of opening a second one beside it.
// - A candidate that moves the table opens the policy of its own generation before its bindings are prepared, so every call of the new generation is ruled by the rules that generation declared.
type hostGovernanceFaces struct {
	specs *genHostSpecs
	open  func(rules []governance.ToolRiskRule) plugin.Governor
	run   *Run

	mu   sync.Mutex
	face governanceFace
	held bool
}

// newHostGovernanceFaces returns the governance consumer of one host over the
// policy construction its boot bound, and beside a nil construction a host whose
// caller mounted its own policy: that policy is the caller's face for every
// generation, so the consumer opens none.
func newHostGovernanceFaces(specs *genHostSpecs, open func(rules []governance.ToolRiskRule) plugin.Governor) *hostGovernanceFaces {
	return &hostGovernanceFaces{specs: specs, open: open}
}

// bind attaches the run whose governance face every generation installs into.
func (f *hostGovernanceFaces) bind(run *Run) { f.run = run }

// boot installs the governance rules the boot document declares.
func (f *hostGovernanceFaces) boot(rules []governance.ToolRiskRule) {
	if f.open == nil {
		return
	}
	f.install(governanceFace{rules: rules, policy: f.open(rules)})
}

// install records one governance face as the face in effect and puts it on the
// run.
func (f *hostGovernanceFaces) install(face governanceFace) governanceFace {
	f.mu.Lock()
	previous := f.face
	f.face, f.held = face, true
	run := f.run
	f.mu.Unlock()
	if run != nil {
		run.installGovernor(face.policy)
	}
	return previous
}

// restore puts one governance face back as the face in effect.
func (f *hostGovernanceFaces) restore(face governanceFace) {
	f.mu.Lock()
	f.face = face
	run := f.run
	f.mu.Unlock()
	if run != nil {
		run.installGovernor(face.policy)
	}
}

// policyInEffect reports the governance policy of the face in effect.
func (f *hostGovernanceFaces) policyInEffect() plugin.Governor {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.face.policy
}

// registration reports the mount of the governance consumer on the
// generation-scoped registry.
func (f *hostGovernanceFaces) registration() state.GenRegistration {
	return state.GenRegistration{Name: governanceFaceConsumerName, BuildGen: f.buildGeneration}
}

// buildGeneration reopens the governance rules of one candidate generation.
func (f *hostGovernanceFaces) buildGeneration(_ context.Context, decl *config.Declaration, _ state.Generation) (state.GenResources, error) {
	spec, ok := f.specs.specOf(decl)
	if !ok {
		return state.GenResources{}, fmt.Errorf("tagent: no host document was claimed for the declaration %q", decl.FactKey())
	}
	if f.open == nil {
		return state.GenResources{Restore: func() {}}, nil
	}
	f.mu.Lock()
	previous, held := f.face, f.held
	f.mu.Unlock()
	if held && reflect.DeepEqual(previous.rules, spec.RiskRules) {
		return state.GenResources{Restore: func() {}}, nil
	}
	replaced := f.install(governanceFace{rules: spec.RiskRules, policy: f.open(spec.RiskRules)})
	return state.GenResources{Restore: func() { f.restore(replaced) }}, nil
}

// hostMemoryFaces is the generation-scoped consumer of the merged memory
// declaration: the storage tier every declaring agent shares and the semantic
// engine its recall vocabulary embeds under. It resolves that declaration again
// for every candidate generation, so the memory section a generation declares is
// judged against the declarations beside it instead of the ones the process
// booted at.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
//   - The store itself is the durable chain this process opened: a candidate naming another tier is refused by the field an operator searches for, because no generation reopens a chain under a second root (design Q3). The generations a boot takes into service are judged the same way, so a restart that adopts the recorded generation serves the store it opened.
//   - The engine binding is opened from the source document of this process and no generation rebuilds it, so a candidate offered once the process serves is refused by the field an operator searches for when it names another engine. The generations the boot itself takes into service run over that binding instead of being judged by it: the recorded generation a restart adopts carries the ledger's own declaration, and the source declaration published beside it is the one the binding was opened from.
//   - The read namespaces are the half a generation reads through its recall face, and they move with the generation that declares them; the merged declaration is resolved per generation so an inconsistent one is refused before anything is published.
type hostMemoryFaces struct {
	specs  *genHostSpecs
	opened *MemoryPlan
	// serving states that this process has taken a generation into service: the
	// memory candidates offered from then on are judged against the engine binding
	// the boot opened, while the generations the boot itself built were not.
	serving atomic.Bool
}

// newHostMemoryFaces returns the memory consumer of one host over the store its
// boot opened.
func newHostMemoryFaces(specs *genHostSpecs, opened *MemoryPlan) *hostMemoryFaces {
	return &hostMemoryFaces{specs: specs, opened: opened}
}

// arm marks the generations this process built as served: every candidate
// offered afterwards is judged against the engine binding the boot opened.
func (f *hostMemoryFaces) arm() {
	if f == nil {
		return
	}
	f.serving.Store(true)
}

// registration reports the mount of the memory consumer on the
// generation-scoped registry.
func (f *hostMemoryFaces) registration() state.GenRegistration {
	return state.GenRegistration{Name: memoryFaceConsumerName, BuildGen: f.buildGeneration}
}

// buildGeneration resolves the merged memory declaration of one candidate
// generation and refuses by name what this process cannot serve.
func (f *hostMemoryFaces) buildGeneration(_ context.Context, decl *config.Declaration, _ state.Generation) (state.GenResources, error) {
	spec, ok := f.specs.specOf(decl)
	if !ok {
		return state.GenResources{}, fmt.Errorf("tagent: no host document was claimed for the declaration %q", decl.FactKey())
	}
	plan, err := MemoryPlanOf(decl, spec.Document.Storage.Type, spec.Document.Storage.Root)
	if err != nil {
		return state.GenResources{}, memoryRefusalOf(decl, err)
	}
	if !sameMemoryTier(f.opened, plan, f.serving.Load()) {
		return state.GenResources{}, memoryRefusalOf(decl,
			fmt.Errorf("the generation names a memory tier beside the one this process opened"))
	}
	return state.GenResources{Restore: func() {}}, nil
}

// memoryRefusalOf attaches one memory refusal to the restart-required family: a
// memory tier this process cannot serve is a restart by name, and the field path
// it names is the one an operator searches the configuration source for.
func memoryRefusalOf(decl *config.Declaration, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrHostReloadRestartRequired, memoryFieldPathOf(decl), cause)
}

// memoryFieldPathOf reports the field path of the memory declaration a document
// states, in agent name order.
func memoryFieldPathOf(decl *config.Declaration) string {
	if decl == nil {
		return "storage.root"
	}
	ids := make([]string, 0, len(decl.Agents))
	for _, spec := range decl.Agents {
		entry := spec.Memory
		if strings.TrimSpace(entry.Type) != "" || strings.TrimSpace(entry.Path) != "" || declaresEngine(entry.Engine) {
			ids = append(ids, spec.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return "storage.root"
	}
	return "agents." + ids[0] + ".memory"
}

// sameMemoryTier reports whether one generation's merged declaration names the
// store this process opened, and — once the process serves — the embedding
// binding it opened with.
func sameMemoryTier(opened, next *MemoryPlan, judgeEngine bool) bool {
	left, right := opened.Storage(), next.Storage()
	if left == nil || right == nil {
		if left != nil || right != nil {
			return false
		}
		return !judgeEngine || reflect.DeepEqual(opened.Engine(), next.Engine())
	}
	if *left != *right {
		return false
	}
	return !judgeEngine || reflect.DeepEqual(opened.Engine(), next.Engine())
}

// hostMeditationFaces is the generation-scoped consumer of the host's reflection
// loop: it resolves the meditation section of every candidate generation and
// mounts the reflection beside its driver over the run faces the port bound.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
//   - The loop is process-private and dies with the generation: the driver a candidate installed is stopped when the generation that superseded it retires, and a refused candidate puts the loop in effect back and stops the driver it built.
//   - The rhythm is a floor and the watermark the loop consumed stands in the anchor record under the storage root, so a rebuilt loop starts from the watermark the chain holds and a generation switch never re-fires a reflection already consumed.
type hostMeditationFaces struct {
	specs       *genHostSpecs
	port        *hostMeditationPort
	storageRoot string
}

// newHostMeditationFaces returns the reflection consumer of one host over the
// port its boot bound.
func newHostMeditationFaces(specs *genHostSpecs, port *hostMeditationPort, storageRoot string) *hostMeditationFaces {
	return &hostMeditationFaces{specs: specs, port: port, storageRoot: storageRoot}
}

// registration reports the mount of the reflection consumer on the
// generation-scoped registry.
func (f *hostMeditationFaces) registration() state.GenRegistration {
	return state.GenRegistration{Name: meditationFaceConsumerName, BuildGen: f.buildGeneration}
}

// buildGeneration mounts the reflection loop of one candidate generation: a
// section that did not move keeps the loop in effect, and a moved one is
// resolved, judged and mounted before the face of its generation is composed.
func (f *hostMeditationFaces) buildGeneration(ctx context.Context, decl *config.Declaration, gen state.Generation) (state.GenResources, error) {
	spec, ok := f.specs.specOf(decl)
	if !ok {
		return state.GenResources{}, fmt.Errorf("tagent: no host document was claimed for the declaration %q", decl.FactKey())
	}
	mapped, err := spec.Document.document()
	if err != nil {
		return state.GenResources{}, err
	}
	plan, err := meditation.Resolve(mapped, spec.Document.PromptDir, f.storageRoot)
	if err != nil {
		return state.GenResources{}, err
	}
	if err := plan.ValidateDelivery(spec.Document.declaresTool, hostSendToolID); err != nil {
		return state.GenResources{}, err
	}
	previous := f.port.reflectionInEffect()
	if f.port.reflectionInstalled() && sameMeditationPlan(previous.plan, plan) {
		return state.GenResources{Restore: func() {}}, nil
	}
	driver, err := f.port.mountReflection(plan)
	if err != nil {
		return state.GenResources{}, err
	}
	var replaced, built func(context.Context) error
	if previous.driver != nil {
		replaced = func(context.Context) error {
			previous.driver.Stop()
			return nil
		}
	}
	if driver != nil {
		built = func(context.Context) error {
			driver.Stop()
			return nil
		}
	}
	return generationReplacement(ctx, meditationFaceConsumerName, gen, replaced, built,
		func() { f.port.restoreReflection(previous) }), nil
}

// sameMeditationPlan reports whether one generation declares the reflection loop
// the generation in effect already serves.
func sameMeditationPlan(inEffect, next *meditation.Plan) bool {
	if inEffect == nil || next == nil {
		return inEffect == nil && next == nil
	}
	return reflect.DeepEqual(*inEffect, *next)
}
