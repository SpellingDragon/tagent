// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/telemetry"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/subagent"
	"github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ErrNoEntryAgent reports a host document that names no entry agent or names an
// agent it does not declare.
var ErrNoEntryAgent = errors.New("wechat-bot: the host document names no declared entry agent")

// ErrDuplicateInbound reports an inbound message whose identity was already
// admitted: one channel delivery drives one invocation.
var ErrDuplicateInbound = errors.New("wechat-bot: the inbound message was already admitted")

// ErrUnknownProvider reports an agent whose provider name resolves to no
// provider entry of the document.
var ErrUnknownProvider = errors.New("wechat-bot: the host document declares no such provider")

// hostToolNames lists the tools this host contributes for the declared tool
// ids of every agent, in the order one declaration receives them: the outbound
// tool, the recall tools, then the knowledge tools.
var hostToolNames = []string{
	sendToolID,
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
	knowledge.SkillSearchToolName,
	knowledge.SkillLoadToolName,
	knowledge.ReadFileToolName,
}

// hostDocument is the host-side view of one configuration source: the entry
// agent, the provider registry, the declared agents, the storage tier the recall
// read faces are wired over, and the governance and WeChat sections the host
// wires itself.
type hostDocument struct {
	Entry          string                   `yaml:"entry"`
	Provider       string                   `yaml:"provider"`
	Model          string                   `yaml:"model"`
	PromptDir      string                   `yaml:"prompt_dir"`
	Providers      map[string]providerEntry `yaml:"providers"`
	Agents         map[string]agentEntry    `yaml:"agents"`
	Storage        storageEntry             `yaml:"storage"`
	TimeoutSeconds int                      `yaml:"request_timeout_seconds"`
	Governance     governanceEntry          `yaml:"governance"`
	App            struct {
		Wechat WechatAppConfig `yaml:"wechat"`
	} `yaml:"app"`
}

// storageEntry is the storage tier the host opens for the runtime: the recall
// read faces are wired over the events this tier commits, so a document that
// declares no storage wires no table and no original store.
type storageEntry struct {
	Type string `yaml:"type"`
	Root string `yaml:"root"`
}

// providerEntry is one connection registry entry: the protocol, the endpoint
// the calls go to and the environment variable holding the key.
type providerEntry struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

// agentEntry is one declared agent as the host reads it: its model reference,
// the prompt files it is assembled from, the tools it declares, the skill roots
// and read root the knowledge tools are built over, and the watchdog budget its
// invocations run under.
type agentEntry struct {
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
	SystemPrompt struct {
		Files []string `yaml:"files"`
	} `yaml:"system_prompt"`
	Tools      []hostToolRef    `yaml:"tools"`
	SkillDirs  []string         `yaml:"skill_dirs"`
	ReadRoot   string           `yaml:"read_root"`
	FinalRound *finalRoundEntry `yaml:"final_round"`
	// Memory declares the shared storage tier and semantic engine this agent
	// reads, and the namespaces it is authorized to read across partitions.
	Memory config.MemorySpec `yaml:"memory"`
	// Meditation declares the scheduled reflection this agent runs: the
	// cadence, the observed namespaces and the agents its result flows back to.
	Meditation config.MeditationSpec `yaml:"meditation"`
}

// finalRoundEntry is the watchdog budget one declared agent opts into: the tool
// rounds one invocation may run, the time it may spend before its next decision
// becomes the closing round, the timeout that round runs under and the
// instruction it carries. Every entry is optional, so an agent declaring no
// budget keeps every invocation unbounded.
type finalRoundEntry struct {
	MaxRounds      int     `yaml:"max_rounds"`
	BudgetSeconds  float64 `yaml:"budget_seconds"`
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	Prompt         string  `yaml:"prompt"`
}

// hostToolRef is one tool reference of a declared agent: a host tool the host
// contributes under its id, or a declared agent the reference authorizes. An
// agent reference carries the file its description is read from and the call
// arguments that travel as the handoff's refs.
type hostToolRef struct {
	Kind            string   `yaml:"kind"`
	ID              string   `yaml:"id"`
	Agent           string   `yaml:"agent"`
	DescriptionFile string   `yaml:"description_file"`
	EventParams     []string `yaml:"event_params"`
}

// governanceEntry is the governance section the host wires: the approval
// directory, the enforcement grade and the approval validity.
type governanceEntry struct {
	Enabled            bool   `yaml:"enabled"`
	Enforcement        string `yaml:"enforcement"`
	Dir                string `yaml:"dir"`
	ApprovalTTLMinutes int    `yaml:"approval_ttl_minutes"`
}

// loadHostDocument reads the configuration source the host is started with: the
// document keeps the sections this host wires, and every key the schema below does
// not name stops the startup instead of being read past. The rejection names the
// source, the line and the field path, so a document and this schema cannot drift
// apart unnoticed.
func loadHostDocument(path string) (*hostDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: read %s: %w", path, err)
	}
	doc := &hostDocument{}
	if err := config.DecodeStrict(path, raw, doc); err != nil {
		return nil, fmt.Errorf("wechat-bot: %w", err)
	}
	if doc.PromptDir == "" {
		doc.PromptDir = "."
	}
	return doc, nil
}

// Declaration returns the organization declaration the host assembles: the entry
// agent, the agents the document declares with the prompt files they are
// assembled from, and the tool references this host contributes.
func (d *hostDocument) Declaration() (*config.Declaration, error) {
	entry := strings.TrimSpace(d.Entry)
	if entry == "" {
		return nil, ErrNoEntryAgent
	}
	if _, ok := d.Agents[entry]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoEntryAgent, entry)
	}
	decl := &config.Declaration{Entry: entry}
	for _, id := range d.agentOrder() {
		spec := config.AgentSpec{ID: id, Model: d.modelName(id)}
		files, err := d.promptFiles(id)
		if err != nil {
			return nil, err
		}
		spec.Files = files
		refs, err := d.toolRefs(id)
		if err != nil {
			return nil, err
		}
		spec.Tools = refs
		spec.FinalRound = d.Agents[id].finalRound()
		spec.Memory = d.Agents[id].Memory
		spec.Meditation = d.Agents[id].Meditation
		decl.Agents = append(decl.Agents, spec)
	}
	return decl, nil
}

// ErrUnknownAgentReference reports an agent reference whose target this document
// does not declare: the declaration is the authorization, so a reference to an
// undeclared agent stops the startup instead of reaching the assembly.
var ErrUnknownAgentReference = errors.New("wechat-bot: the agent reference names no declared agent")

// ErrUnknownHostTool reports a declared host tool this host contributes no
// instance for: the schema names the ids it reads, and a document that drifts
// from them stops the startup instead of exposing a name nothing answers.
var ErrUnknownHostTool = errors.New("wechat-bot: the declared tool is not contributed by this host")

// toolRefs resolves the tool references one declared agent carries, in
// declaration order: a host tool reference becomes the plain name this host
// contributes, and an agent reference becomes the target tool of the declared
// agent it authorizes, carrying the description file it names and the call
// arguments that travel as the handoff's refs. A reference writing only the
// bare `- agent: <id>` form names the same grant as the written-out agent
// reference, so the kind defaults instead of the declaration being refused.
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
			return nil, fmt.Errorf("wechat-bot: agent %q declares tool kind %q, which this host does not wire", id, ref.Kind)
		}
	}
	return refs, nil
}

// hostContributes reports whether this host contributes an instance for one
// declared tool id.
func hostContributes(id string) bool {
	for _, name := range hostToolNames {
		if name == id {
			return true
		}
	}
	return false
}

// finalRound reports the watchdog budget one declared agent opts into; an agent
// declaring none reports the zero budget, which leaves its invocations
// unbounded.
func (a agentEntry) finalRound() config.FinalRoundSpec {
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

// promptFiles reports the prompt files one declared agent is assembled from. A
// declared file the checkout does not carry is skipped with one info line, so the
// personal context a deployment keeps beside the checkout stays an input rather
// than a startup precondition; any other read failure stops the assembly.
func (d *hostDocument) promptFiles(id string) ([]config.FileRef, error) {
	files := make([]config.FileRef, 0, len(d.Agents[id].SystemPrompt.Files))
	for _, name := range d.Agents[id].SystemPrompt.Files {
		path := filepath.Join(d.PromptDir, name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				log.Infof("[host] declared prompt file absent, skipped: %s", path)
				continue
			}
			return nil, fmt.Errorf("wechat-bot: read the declared prompt file %s: %w", path, err)
		}
		files = append(files, config.FileRef{Path: path, Kind: config.FileKindPrompt})
	}
	return files, nil
}

// agentOrder returns the declared agent identities in declaration order: the
// entry first, then the remaining identities sorted, so one document assembles
// in one order across restarts.
func (d *hostDocument) agentOrder() []string {
	ids := make([]string, 0, len(d.Agents))
	for id := range d.Agents {
		if id != strings.TrimSpace(d.Entry) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return append([]string{strings.TrimSpace(d.Entry)}, ids...)
}

// modelName reports the model reference of one declared agent: its own
// reference when declared, the document default otherwise.
func (d *hostDocument) modelName(id string) string {
	if name := strings.TrimSpace(d.Agents[id].Model); name != "" {
		return name
	}
	return strings.TrimSpace(d.Model)
}

// declaresSend reports whether one declared agent declares the send tool: the
// declaration is the grant, so an agent outside it never sees the tool.
func (d *hostDocument) declaresSend(id string) bool {
	return d.declaresTool(id, sendToolID)
}

// declaresTool reports whether one declared agent declares the named host tool.
func (d *hostDocument) declaresTool(id, name string) bool {
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind == "tool" && ref.ID == name {
			return true
		}
	}
	return false
}

// nativeAgents constructs the live pieces every declared agent is assembled
// with: its own model and the contributed tools its declaration grants.
func (d *hostDocument) nativeAgents(ctx context.Context, send *hostSendSeam, recallSeam *hostRecallSeam) (map[event.AgentID]tagent.NativeAgent, error) {
	decl, err := d.Declaration()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: d.requestTimeout()}
	out := make(map[event.AgentID]tagent.NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		native, err := d.modelFor(spec.ID, client)
		if err != nil {
			return nil, err
		}
		tools, err := d.hostTools(ctx, spec.ID, send, recallSeam)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = tagent.NativeAgent{Model: native, Tools: tools}
	}
	return out, nil
}

// hostTools constructs the tool instances one declared agent's declaration
// grants, in declaration order: the outbound tool, every recall tool the
// declaration names and every knowledge tool whose faces this host wires. A
// tool the declaration does not name is never constructed, so a declaration
// outside the set sees none of them.
func (d *hostDocument) hostTools(ctx context.Context, id string, send *hostSendSeam, recallSeam *hostRecallSeam) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolNames))
	if d.declaresTool(id, sendToolID) {
		available[sendToolID] = sendToolFactory(send)
	}
	if d.declaresRecall(id) {
		for _, candidate := range recall.New(recallSeam.readsFace(), d.readNamespaces(id)) {
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
	tools := make([]trpctool.CallableTool, 0, len(available))
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind != "tool" {
			continue
		}
		if tool, ok := available[ref.ID]; ok {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

// declaresRecall reports whether one declared agent names any recall tool.
func (d *hostDocument) declaresRecall(id string) bool {
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// declaresKnowledge reports whether one declared agent names any knowledge tool.
func (d *hostDocument) declaresKnowledge(id string) bool {
	for _, name := range []string{knowledge.SkillSearchToolName, knowledge.SkillLoadToolName, knowledge.ReadFileToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// knowledgeTools constructs the knowledge surface of one declared agent over
// the faces its declaration names: the skill repository of its declared skill
// roots and the read root its file read is confined to. The surface owns no
// repository and no file system of its own; both are the deployment's.
func (d *hostDocument) knowledgeTools(ctx context.Context, id string) ([]trpctool.CallableTool, error) {
	opts := knowledge.Options{ReadRoot: d.readRoot(id)}
	if roots := d.skillRoots(id); len(roots) > 0 {
		repository, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, fmt.Errorf("wechat-bot: open the skill repository of agent %q: %w", id, err)
		}
		opts.Skills = repository
	}
	return knowledge.New(ctx, opts)
}

// skillRoots reports the skill roots one declared agent names, in declaration
// order: a root the checkout does not carry stops the startup instead of leaving
// the surface short of the skills the document promised.
func (d *hostDocument) skillRoots(id string) []string {
	roots := make([]string, 0, len(d.Agents[id].SkillDirs))
	for _, dir := range d.Agents[id].SkillDirs {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	return roots
}

// readRoot reports the directory the knowledge file read of one declared agent
// is confined to: the root it names, or the working directory when it names
// none.
func (d *hostDocument) readRoot(id string) string {
	if root := strings.TrimSpace(d.Agents[id].ReadRoot); root != "" {
		return root
	}
	return "."
}

// hostRecallSeam is the recall read seam of this host: the tools are
// constructed before the runtime exists, so every face is bound to the assembled
// run once the assembly returns. A call before that binding answers the named
// refusal of the recall surface.
//
// The seam carries the read faces the tools consume and forwards each to the
// face the run wired: the retrieval port, the observation table over the
// committed store and the scoped original store of the storage tier. The
// relation ledger is this host's own, because the run wires none, so it is held
// here from the first call and never rebound.
type hostRecallSeam struct {
	mu       sync.RWMutex
	face     func(ctx context.Context, query search.Query) (search.Result, error)
	reads    recall.Reads
	embedder memory.Embedder
	trace    *replay.MemoryRelations
}

var _ search.Searcher = (*hostRecallSeam)(nil)

var _ recall.Table = (*hostRecallSeam)(nil)

var _ ledger.ScopedOriginalStore = (*hostRecallSeam)(nil)

// bind attaches the read faces of the assembled run.
func (s *hostRecallSeam) bind(face func(ctx context.Context, query search.Query) (search.Result, error), reads recall.Reads) {
	s.mu.Lock()
	s.face = face
	s.reads = reads
	s.mu.Unlock()
}

// Search answers one retrieval through the bound face.
func (s *hostRecallSeam) Search(ctx context.Context, query search.Query) (search.Result, error) {
	s.mu.RLock()
	face := s.face
	s.mu.RUnlock()
	if face == nil {
		return search.Result{}, recall.ErrRecallUnwired
	}
	return face(ctx, query)
}

// QueryEvents answers the event references one query matches through the bound
// observation table.
func (s *hostRecallSeam) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	s.mu.RLock()
	table := s.reads.Table
	s.mu.RUnlock()
	if table == nil {
		return nil, fmt.Errorf("recall_query: %w", recall.ErrRecallUnwired)
	}
	return table.QueryEvents(ctx, opts)
}

// Event answers the reference of one landed event through the bound table.
func (s *hostRecallSeam) Event(key event.EventID) (memory.EventReference, bool) {
	s.mu.RLock()
	table := s.reads.Table
	s.mu.RUnlock()
	if table == nil {
		return memory.EventReference{}, false
	}
	return table.Event(key)
}

// Reference answers the original reference one landed event carries through the
// bound table.
func (s *hostRecallSeam) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	s.mu.RLock()
	table := s.reads.Table
	s.mu.RUnlock()
	if table == nil {
		return ledger.OriginalRef{}, false
	}
	return table.Reference(key)
}

// GetScoped answers one original read through the bound scoped original store.
func (s *hostRecallSeam) GetScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope) ledger.RecallResult {
	s.mu.RLock()
	originals := s.reads.Originals
	s.mu.RUnlock()
	if originals == nil {
		return ledger.RecallResult{Key: key, Outcome: ledger.RecallFailed, Err: recall.ErrRecallUnwired}
	}
	return originals.GetScoped(ctx, key, scope)
}

// ReadSegmentScoped answers one segment read through the bound scoped original
// store.
func (s *hostRecallSeam) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope, req ledger.SegmentRequest) (ledger.SegmentResult, error) {
	s.mu.RLock()
	originals := s.reads.Originals
	s.mu.RUnlock()
	if originals == nil {
		return ledger.SegmentResult{}, fmt.Errorf("recall_get: %w", recall.ErrRecallUnwired)
	}
	return originals.ReadSegmentScoped(ctx, key, scope, req)
}

// readsFace reports the read face this host wires for the recall tools: the retrieval
// port, the embedding binding and the table and original store of the bound
// run, beside the relation ledger this host holds. A face the deployment wires
// nothing for stays absent, so the tool that needs it refuses by name instead
// of answering an empty result.
func (s *hostRecallSeam) readsFace() recall.Reads {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reads := s.reads
	reads.Searcher = s
	reads.Trace = s.trace
	if s.embedder != nil {
		reads.Embedder = s.embedder
	}
	if reads.Table != nil {
		reads.Table = s
	}
	if reads.Originals != nil {
		reads.Originals = s
	}
	return reads
}

// requestTimeout reports the model call timeout the document declares.
func (d *hostDocument) requestTimeout() time.Duration {
	if d.TimeoutSeconds > 0 {
		return time.Duration(d.TimeoutSeconds) * time.Second
	}
	return 10 * time.Minute
}

// modelFor constructs the native model of one declared agent from its provider
// entry: the endpoint and the key environment variable the entry names.
//
// Chat telemetry stays on the model the runtime's own loop drives — the entry
// agent's — because that loop reports no generation span of its own, while a
// delegated turn runs on the framework flow, which reports one already: a model
// carrying the flag there as well would land two.
func (d *hostDocument) modelFor(id string, client *http.Client) (trpcmodel.Model, error) {
	entry := d.Agents[id]
	providerName := strings.TrimSpace(entry.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(d.Provider)
	}
	provider, ok := d.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, providerName)
	}
	if protocol := strings.TrimSpace(provider.Provider); protocol != "" && protocol != "openai" {
		return nil, fmt.Errorf("wechat-bot: provider %q runs protocol %q, which this host does not wire", providerName, protocol)
	}
	modelName := d.modelName(id)
	if modelName == "" {
		return nil, fmt.Errorf("wechat-bot: agent %q declares no model", id)
	}
	keyEnv := strings.TrimSpace(provider.APIKeyEnv)
	if keyEnv == "" {
		return nil, fmt.Errorf("wechat-bot: provider %q names no api_key_env", providerName)
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return nil, fmt.Errorf("wechat-bot: provider %q requires %s in the environment", providerName, keyEnv)
	}
	endpoint := strings.TrimSpace(provider.APIEndpoint)
	if id == strings.TrimSpace(d.Entry) {
		if override := os.Getenv("TAGENT_API_ENDPOINT"); override != "" {
			endpoint = override
		}
	}
	opts := []openai.Option{openai.WithAPIKey(key)}
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

// hostRuntime is the assembled host of one process: the runtime driven per
// explicit session address, the router that maps chat identities to those
// addresses, the inbox the internal notices wait in, and the one outbound path
// both the passive and the active channel deliver through.
type hostRuntime struct {
	run    *tagent.Run
	router *SessionRouter
	inbox  *internalInbox
	sender *hostSender
	seen   *SeenStore
	gates  sync.Map

	meditationPlan *meditationPlan
	meditation     *meditation.Driver

	chatMu   sync.Mutex
	lastChat string
}

// inboundMessage is one channel delivery the host admits: the dedup identity of
// the delivery, the chat identity it names and the text its turn carries.
type inboundMessage struct {
	Key      string
	ChatID   string
	UserName string
	Text     string
}

// assembleHost builds the runtime of one document and returns the host that
// drives it: the live pieces of every declared agent are constructed here and
// the assembly they form is the one this process runs.
func assembleHost(ctx context.Context, doc *hostDocument, gateway *approvalGateway, seen *SeenStore) (*hostRuntime, error) {
	send := &hostSendSeam{}
	relations, err := doc.relations()
	if err != nil {
		return nil, err
	}
	embedder, err := doc.embedder()
	if err != nil {
		return nil, err
	}
	recallSeam := &hostRecallSeam{trace: relations, embedder: embedder}
	natives, err := doc.nativeAgents(ctx, send, recallSeam)
	if err != nil {
		return nil, err
	}
	host, err := assembleHostFrom(ctx, doc, gateway, seen, send, natives, embedder)
	if err != nil {
		return nil, err
	}
	recallSeam.bind(host.run.Recall, host.run.RecallReads())
	return host, nil
}

// assembleHostFrom builds the host around the live pieces every declared agent
// is assembled with. The pieces are handed in rather than constructed here, so
// a caller that builds its own models and tools drives the very same assembly
// and the very same outbound path this process runs. Governance is mandatory at
// publication; the approval policy is attached when a gateway is supplied.
func assembleHostFrom(ctx context.Context, doc *hostDocument, gateway *approvalGateway, seen *SeenStore, seam *hostSendSeam, natives map[event.AgentID]tagent.NativeAgent, embedder memory.Embedder) (*hostRuntime, error) {
	decl, err := doc.Declaration()
	if err != nil {
		return nil, err
	}
	router, err := NewSessionRouter(event.AgentID(decl.Entry), sessionPrefix(doc))
	if err != nil {
		return nil, err
	}
	opts := []tagent.Option{
		tagent.WithGovernance(evolution.NewPolicy(evolution.NewStructuralGate())),
		tagent.WithTelemetry(telemetry.NewLogObserver(log.Default)),
	}
	if cfg, err := doc.storageConfig(); err != nil {
		return nil, err
	} else if cfg != nil {
		opts = append(opts, tagent.WithStorage(*cfg))
		if embedder != nil {
			opts = append(opts, tagent.WithEmbedder(embedder))
		}
	}
	vectorTopK, keywordTopK, rrfK, err := doc.retrievalTuning()
	if err != nil {
		return nil, err
	}
	if vectorTopK > 0 || keywordTopK > 0 || rrfK > 0 {
		opts = append(opts, tagent.WithRetrievalTuning(vectorTopK, keywordTopK, rrfK))
	}
	plan, err := doc.meditationPlan()
	if err != nil {
		return nil, err
	}
	if plan != nil {
		opts = append(opts, tagent.WithReflectionAnchors(plan.anchorsPath))
	}
	if gateway != nil && gateway.Enabled() {
		policyOpts := []governance.PolicyOption{governance.WithApprovals(gateway.Manager())}
		if strings.EqualFold(strings.TrimSpace(doc.Governance.Enforcement), "strict") {
			policyOpts = append(policyOpts, governance.WithStrict())
		}
		opts = append(opts, tagent.WithPolicy(governance.Open(policyOpts...)))
	}
	run, err := tagent.Assemble(ctx, decl, natives, opts...)
	if err != nil {
		return nil, err
	}
	host := &hostRuntime{
		run:            run,
		router:         router,
		inbox:          newInternalInbox(),
		seen:           seen,
		meditationPlan: plan,
	}
	seam.bind(func(ctx context.Context, addr event.Address, content string) (string, error) {
		return host.deliver(ctx, addr, sendToolLineage, content)
	})
	return host, nil
}

// sessionPrefix reports the session identity prefix the document's addresses are
// spelled with: the entry agent beside the configured instance label.
func sessionPrefix(doc *hostDocument) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "wechat-session"
	}
	return label + ":" + strings.TrimSpace(doc.Entry) + ":"
}

// Close drains the runtime and reports what stayed open. The scheduled
// reflection stops first, so no tick runs beside the closing runtime.
func (h *hostRuntime) Close(ctx context.Context) error {
	if h.meditation != nil {
		h.meditation.Stop()
	}
	report, err := h.run.Close(ctx, agent.CloseRequest{})
	if err != nil {
		return err
	}
	if !report.Drained {
		log.Warnf("[host] close reported %d unsettled obligation(s)", len(report.Unsettled))
	}
	return nil
}

// Admit claims the dedup identity of one channel delivery: a duplicate delivery
// reports ErrDuplicateInbound and drives nothing.
func (h *hostRuntime) Admit(key string) error {
	if h.seen == nil || key == "" {
		return nil
	}
	if !h.seen.CheckAndMark(key) {
		return fmt.Errorf("%w: %s", ErrDuplicateInbound, key)
	}
	return nil
}

// HandleInbound admits one channel delivery and drives its session: the chat
// identity is mapped to its explicit address, the address is bound to that chat
// as its delivery identity, and the turn's fresh assistant messages are
// delivered to that same chat.
func (h *hostRuntime) HandleInbound(ctx context.Context, in inboundMessage) (event.Address, error) {
	if err := h.Admit(in.Key); err != nil {
		return event.Address{}, err
	}
	return h.Drive(ctx, in)
}

// Drive routes one admitted delivery to its session and delivers the turn's
// fresh assistant messages to the chat the address is bound to.
func (h *hostRuntime) Drive(ctx context.Context, in inboundMessage) (event.Address, error) {
	addr, err := h.router.AddressFor(in.ChatID)
	if err != nil {
		return event.Address{}, err
	}
	if err := h.router.Bind(addr, in.ChatID); err != nil {
		return event.Address{}, err
	}
	h.rememberChat(in.ChatID)
	unlock := h.lockAddress(addr)
	defer unlock()

	input := h.inbox.Drain(addr)
	if text := strings.TrimSpace(in.Text); text != "" {
		input = append(input, tagentmodel.Message{Role: tagentmodel.RoleUser, Content: text})
	}
	if len(input) == 0 {
		return addr, nil
	}
	turn, err := h.run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID:  h.router.NextInvocation(addr),
		Address:       addr,
		TriggerSource: "user",
		Input:         input,
	})
	if err != nil {
		return addr, err
	}
	h.deliverTurn(ctx, addr, in.UserName, turn)
	return addr, nil
}

// deliverTurn delivers the assistant messages the session accumulated since the
// previous delivery, one chat only.
func (h *hostRuntime) deliverTurn(ctx context.Context, addr event.Address, userName string, turn agent.Turn) {
	fresh := h.router.TakeUndelivered(addr, h.run.SessionHistory(addr))
	delivered := false
	for _, msg := range fresh {
		if msg.Role != tagentmodel.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if _, err := h.sender.sendToUser(ctx, sendRequest{
			addr:          addr,
			userName:      userName,
			triggerSource: "user",
			content:       content,
		}); err != nil {
			log.Warnf("[host] delivery refused for %s: %v", addr.SessionID, err)
			continue
		}
		delivered = true
	}
	if !delivered && turn.Terminal == tagentmodel.KindFailed {
		_, _ = h.sender.sendToUser(ctx, sendRequest{
			addr:          addr,
			userName:      userName,
			triggerSource: "error",
			content:       "执行出错：本轮模型回合失败，未产出交付内容。",
		})
	}
}

// rememberChat records the chat this delivery came from as the chat an injection
// arriving without a chat of its own is admitted at.
func (h *hostRuntime) rememberChat(chatID string) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return
	}
	h.chatMu.Lock()
	h.lastChat = trimmed
	h.chatMu.Unlock()
}

// mostRecentChat reports the chat that last delivered, and no chat beside none: an
// injection is admitted there and never at a chat of another session.
func (h *hostRuntime) mostRecentChat() string {
	h.chatMu.Lock()
	defer h.chatMu.Unlock()
	return h.lastChat
}

// deliver is the one outbound entry both channels reach: the active send tool
// and the passive delivery of an ended turn.
func (h *hostRuntime) deliver(ctx context.Context, addr event.Address, source, content string) (string, error) {
	return h.sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: source, content: content})
}

// lockAddress serializes the drives of one session, so two deliveries of one
// chat never interleave their turns and two chats still run side by side.
func (h *hostRuntime) lockAddress(addr event.Address) func() {
	value, _ := h.gates.LoadOrStore(addr, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// internalInbox holds the internal notices a session has to read on its next
// turn: a notice bound to one address waits for that address, and a notice
// bound to no address waits for the first turn of any session.
type internalInbox struct {
	mu      sync.Mutex
	byAddr  map[event.Address][]tagentmodel.Message
	pending []tagentmodel.Message
}

// newInternalInbox returns an empty internal inbox.
func newInternalInbox() *internalInbox {
	return &internalInbox{byAddr: map[event.Address][]tagentmodel.Message{}}
}

// InjectInternal queues one internal notice for the named address; an
// incomplete address queues it for the first turn of any session.
func (in *internalInbox) InjectInternal(addr event.Address, source string, msg tagentmodel.Message) {
	if in == nil {
		return
	}
	if msg.Role == "" {
		msg.Role = tagentmodel.RoleUser
	}
	log.Debugf("[inbox] queue %s notice for %s", source, addr.SessionID)
	in.mu.Lock()
	defer in.mu.Unlock()
	if !addr.IsComplete() {
		in.pending = append(in.pending, msg)
		return
	}
	in.byAddr[addr] = append(in.byAddr[addr], msg)
}

// Drain returns the notices waiting for the named address beside the notices
// waiting for any session, and records them as consumed.
func (in *internalInbox) Drain(addr event.Address) []tagentmodel.Message {
	if in == nil {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	out := append([]tagentmodel.Message(nil), in.byAddr[addr]...)
	delete(in.byAddr, addr)
	out = append(out, in.pending...)
	in.pending = nil
	return out
}

// pendingCount reports how many notices wait for any session.
func (in *internalInbox) pendingCount() int {
	if in == nil {
		return 0
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return len(in.pending)
}
