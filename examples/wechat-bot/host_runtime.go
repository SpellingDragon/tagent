// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
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

// hostDocument is the host-side view of one configuration source: the entry
// agent, the provider registry, the declared agents, and the governance and
// WeChat sections the host wires itself.
type hostDocument struct {
	Entry          string                   `yaml:"entry"`
	Provider       string                   `yaml:"provider"`
	Model          string                   `yaml:"model"`
	PromptDir      string                   `yaml:"prompt_dir"`
	Providers      map[string]providerEntry `yaml:"providers"`
	Agents         map[string]agentEntry    `yaml:"agents"`
	TimeoutSeconds int                      `yaml:"request_timeout_seconds"`
	Governance     governanceEntry          `yaml:"governance"`
	App            struct {
		Wechat WechatAppConfig `yaml:"wechat"`
	} `yaml:"app"`
}

// providerEntry is one connection registry entry: the protocol, the endpoint
// the calls go to and the environment variable holding the key.
type providerEntry struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

// agentEntry is one declared agent as the host reads it: its model reference,
// the prompt files it is assembled from and the tools it declares.
type agentEntry struct {
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
	SystemPrompt struct {
		Files []string `yaml:"files"`
	} `yaml:"system_prompt"`
	Tools []hostToolRef `yaml:"tools"`
}

// hostToolRef is one tool reference of a declared agent.
type hostToolRef struct {
	Kind  string `yaml:"kind"`
	ID    string `yaml:"id"`
	Agent string `yaml:"agent"`
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
		for _, name := range d.Agents[id].SystemPrompt.Files {
			spec.Files = append(spec.Files, config.FileRef{
				Path: filepath.Join(d.PromptDir, name),
				Kind: config.FileKindPrompt,
			})
		}
		if d.declaresSend(id) {
			spec.Tools = append(spec.Tools, config.ToolRef{Name: sendToolID})
		}
		decl.Agents = append(decl.Agents, spec)
	}
	return decl, nil
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
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind == "tool" && ref.ID == sendToolID {
			return true
		}
	}
	return false
}

// nativeAgents constructs the live pieces every declared agent is assembled
// with: its own model and the contributed tools its declaration grants.
func (d *hostDocument) nativeAgents(seam *hostSendSeam) (map[event.AgentID]tagent.NativeAgent, error) {
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
		tools := make([]trpctool.CallableTool, 0, 1)
		if d.declaresSend(spec.ID) {
			tools = append(tools, sendToolFactory(seam))
		}
		out[event.AgentID(spec.ID)] = tagent.NativeAgent{Model: native, Tools: tools}
	}
	return out, nil
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
	seam := &hostSendSeam{}
	natives, err := doc.nativeAgents(seam)
	if err != nil {
		return nil, err
	}
	return assembleHostFrom(ctx, doc, gateway, seen, seam, natives)
}

// assembleHostFrom builds the host around the live pieces every declared agent
// is assembled with. The pieces are handed in rather than constructed here, so
// a caller that builds its own models and tools drives the very same assembly
// and the very same outbound path this process runs. Governance is mandatory at
// publication; the approval policy is attached when a gateway is supplied.
func assembleHostFrom(ctx context.Context, doc *hostDocument, gateway *approvalGateway, seen *SeenStore, seam *hostSendSeam, natives map[event.AgentID]tagent.NativeAgent) (*hostRuntime, error) {
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
		run:    run,
		router: router,
		inbox:  newInternalInbox(),
		seen:   seen,
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

// Close drains the runtime and reports what stayed open.
func (h *hostRuntime) Close(ctx context.Context) error {
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
		InvocationID: h.router.NextInvocation(addr),
		Address:      addr,
		Input:        input,
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
