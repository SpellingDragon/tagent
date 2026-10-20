// wechat.go 是微信宿主唯一的适配文件：SDK 调用、收发、文件、路由、去重、send、审批与通知
// 都是微信协议语义的一部分。main.go 只做 wiring；核心的声明、装配、模型与工具面一律走
// api/document、api/config 与 tagent.Assemble 现有入口。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"github.com/SpellingDragon/tagent/telemetry"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/spec"
	"github.com/SpellingDragon/tagent/tool/subagent"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// tagentexecPresetName is the declaration id that grants the execution face;
// the core assembles the tool, the host accepts the reference.
const tagentexecPresetName = tagentexec.PresetName

// FileSender 是投递层依赖的窄接口；*wechat.Bot 天然满足，测试以替身顶上。
type FileSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
	SendImageFromPath(ctx context.Context, toUserID, imagePath string) error
	SendVoiceFromPath(ctx context.Context, toUserID, voicePath string, duration int) error
	SendVideoFromPath(ctx context.Context, toUserID, videoPath string) error
	SendFileFromPath(ctx context.Context, toUserID, filePath string) error
}

// MediaDownloader 是入站接收层的窄接口：SDK 边下边解密写入 Writer，MaxSize 由 SDK 在传输中强制。
type MediaDownloader interface {
	DownloadImageFromItemTo(ctx context.Context, cdnBaseURL string, img *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVoiceTo(ctx context.Context, voice *wechat.VoiceItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadFileFromItemTo(ctx context.Context, file *wechat.FileItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVideoFromItemTo(ctx context.Context, video *wechat.VideoItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
}

// wechatOutbound 是宿主发送通路对通道实例的窄依赖面：文本、媒体、typing 与 context-token。
type wechatOutbound interface {
	FileSender
	StopTyping(ctx context.Context, toUserID string) error
	GetContextToken(userID string) (string, error)
}

// wechatChannel 是挂点注册与驱动对通道实例的窄依赖面：出网面与入站媒体下载面之外，
// 再加"入站钩子、typing 入口与 CDN 根"。*wechat.Bot 天然满足，测试以替身顶上。
type wechatChannel interface {
	wechatOutbound
	MediaDownloader
	OnMessage(handler wechat.MessageHandler)
	SendTyping(ctx context.Context, toUserID string) error
	CDNBaseURL() string
}

var (
	_ FileSender      = (*wechat.Bot)(nil)
	_ MediaDownloader = (*wechat.Bot)(nil)
	_ wechatOutbound  = (*wechat.Bot)(nil)
	_ wechatChannel   = (*wechat.Bot)(nil)
)

// longTextFunc 是超长正文（>2000 字节）的分片投递动作，真机经 context-token 走微信长文接口。
type longTextFunc func(ctx context.Context, chatID, content, token string) error

// longTextViaContextToken 是真机一侧的长文投递实现。
func longTextViaContextToken(bot *wechat.Bot) longTextFunc {
	return func(ctx context.Context, chatID, content, token string) error {
		_, err := wechat.SendLongText(ctx, bot.Client(), bot.Media(), chatID, content, token)
		return err
	}
}

// WechatAppConfig is the WeChat-specific application section of the document.
type WechatAppConfig struct {
	ConfigDir       string   `yaml:"config_dir" json:"config_dir"`
	TokenFile       string   `yaml:"token_file" json:"token_file"`
	ContextTokenDir string   `yaml:"context_token_dir" json:"context_token_dir"`
	WorkspaceDir    string   `yaml:"workspace_dir" json:"workspace_dir"`
	Approvers       []string `yaml:"approvers" json:"approvers,omitempty"`
}

// EnsureDirs creates the directories the channel and the workspace need.
func (c *WechatAppConfig) EnsureDirs() error {
	dirs := []string{c.ConfigDir, c.ContextTokenDir}
	if c.WorkspaceDir != "" {
		dirs = append(dirs, filepath.Join(c.WorkspaceDir, uploadsSubdir))
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

// applyWechatDefaults fills the deployment defaults beside the environment override.
func applyWechatDefaults(c *WechatAppConfig, workspaceOverride string) {
	if c.ConfigDir == "" {
		c.ConfigDir = ".wechat-config"
	}
	if c.TokenFile == "" {
		c.TokenFile = "token.json"
	}
	if c.ContextTokenDir == "" {
		c.ContextTokenDir = ".wechat-context-tokens"
	}
	if workspaceOverride != "" {
		c.WorkspaceDir = workspaceOverride
	}
}

// hostDocument is the host-side view of one configuration source.
type hostDocument struct {
	Entry          string                          `yaml:"entry"`
	Provider       string                          `yaml:"provider"`
	Model          string                          `yaml:"model"`
	PromptDir      string                          `yaml:"prompt_dir"`
	Providers      map[string]providerEntry        `yaml:"providers"`
	Agents         map[string]agentEntry           `yaml:"agents"`
	Storage        storageEntry                    `yaml:"storage"`
	TimeoutSeconds int                             `yaml:"request_timeout_seconds"`
	Governance     governanceEntry                 `yaml:"governance"`
	MCPServers     map[string]config.MCPServerSpec `yaml:"mcp_servers"`
	App            struct {
		Wechat WechatAppConfig `yaml:"wechat"`
	} `yaml:"app"`

	once        sync.Once
	mapped      document.Document
	mappedError error

	// specBackend lets a test install the plan backend; nil uses the process default.
	specBackend spec.Backend
}

type storageEntry struct {
	Type string `yaml:"type"`
	Root string `yaml:"root"`
}

type providerEntry struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

type agentEntry struct {
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
	SystemPrompt struct {
		Files []string `yaml:"files"`
	} `yaml:"system_prompt"`
	Tools      []hostToolRef         `yaml:"tools"`
	SkillDirs  []string              `yaml:"skill_dirs"`
	ReadRoot   string                `yaml:"read_root"`
	FinalRound *finalRoundEntry      `yaml:"final_round"`
	Memory     config.MemorySpec     `yaml:"memory"`
	Meditation config.MeditationSpec `yaml:"meditation"`
	File       config.FileSpec       `yaml:"file"`
	Exec       config.ExecSpec       `yaml:"exec"`
}

type finalRoundEntry struct {
	MaxRounds      int     `yaml:"max_rounds"`
	BudgetSeconds  float64 `yaml:"budget_seconds"`
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	Prompt         string  `yaml:"prompt"`
}

type hostToolRef struct {
	Kind            string   `yaml:"kind"`
	ID              string   `yaml:"id"`
	Agent           string   `yaml:"agent"`
	DescriptionFile string   `yaml:"description_file"`
	EventParams     []string `yaml:"event_params"`
}

type governanceEntry struct {
	Enabled            bool   `yaml:"enabled"`
	Enforcement        string `yaml:"enforcement"`
	Dir                string `yaml:"dir"`
	ApprovalTTLMinutes int    `yaml:"approval_ttl_minutes"`
}

// loadHostDocument reads the configuration source the host is started with; an
// unknown key stops the startup instead of being read past.
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

// document maps the host sections onto the declaration-shaped section the core shares.
func (d *hostDocument) document() (document.Document, error) {
	d.once.Do(func() { d.mapped, d.mappedError = d.mapDocument() })
	return d.mapped, d.mappedError
}

// Declaration returns the organization declaration the host assembles.
func (d *hostDocument) Declaration() (*config.Declaration, error) {
	mapped, err := d.document()
	if err != nil {
		return nil, err
	}
	return mapped.Declaration()
}

func (d *hostDocument) mapDocument() (document.Document, error) {
	agents := make(map[string]config.AgentSpec, len(d.Agents))
	for id, entry := range d.Agents {
		spec := config.AgentSpec{
			ID:         id,
			Model:      d.modelName(id),
			FinalRound: entry.finalRound(),
			Memory:     entry.Memory,
			Meditation: entry.Meditation,
			File:       entry.File,
			Exec:       entry.Exec,
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

var (
	// ErrUnknownAgentReference reports an agent reference that names no declared agent.
	ErrUnknownAgentReference = errors.New("wechat-bot: the agent reference names no declared agent")
	// ErrUnknownHostTool reports a declared tool this host does not contribute.
	ErrUnknownHostTool = errors.New("wechat-bot: the declared tool is not contributed by this host")
)

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
			return nil, fmt.Errorf("wechat-bot: agent %q declares tool kind %q, which this host does not wire", id, ref.Kind)
		}
	}
	return refs, nil
}

// hostToolNames lists the tool ids this host contributes an instance for.
var hostToolNames = append(append([]string{
	sendToolID,
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
	knowledge.SkillSearchToolName,
	knowledge.SkillLoadToolName,
	spec.ToolName,
}, tagentfile.Names()...),
	tagentexecPresetName,
	mcpgw.ToolNameMCPCall,
	mcpgw.ToolNameMCPDiscover,
)

// hostContributes reports whether this host contributes an instance for one id.
func hostContributes(id string) bool {
	for _, name := range hostToolNames {
		if name == id {
			return true
		}
	}
	return false
}

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

// promptFiles reads the prompt files one declared agent is assembled from; an
// absent declared file is skipped with one info line.
func (d *hostDocument) promptFiles(id string) ([]config.FileRef, error) {
	files, absent, err := document.PromptFiles(d.PromptDir, d.Agents[id].SystemPrompt.Files)
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: %w", err)
	}
	for _, path := range absent {
		log.Infof("[host] declared prompt file absent, skipped: %s", path)
	}
	return files, nil
}

func (d *hostDocument) modelName(id string) string {
	if name := strings.TrimSpace(d.Agents[id].Model); name != "" {
		return name
	}
	return strings.TrimSpace(d.Model)
}

func (d *hostDocument) declaresSend(id string) bool { return d.declaresTool(id, sendToolID) }

func (d *hostDocument) declaresTool(id, name string) bool {
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind == "tool" && ref.ID == name {
			return true
		}
	}
	return false
}

// nativeAgents constructs the live pieces every declared agent is assembled with.
func (d *hostDocument) nativeAgents(ctx context.Context, send *hostSendSeam, recallPort *tagent.RecallPort) (map[event.AgentID]tagent.NativeAgent, error) {
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
		tools, err := d.hostTools(ctx, spec.ID, send, recallPort)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = tagent.NativeAgent{Model: native, Tools: tools}
	}
	return out, nil
}

// hostTools constructs the tool instances this host contributes; the core owns
// the file, skill, execution, MCP and delegation families of the declaration.
//
// A delegated turn runs on the target's native tools, so the host contributes
// the file surface its declaration grants; the core drops its own copy of a
// name the host already answers. The plan agent's write root is the plan
// backend's workspace; every other agent keeps its declared base directory.
func (d *hostDocument) hostTools(ctx context.Context, id string, send *hostSendSeam, recallPort *tagent.RecallPort) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolNames))
	if d.declaresSend(id) {
		available[sendToolID] = newSendTool(send)
	}
	if d.declaresRecall(id) {
		for _, candidate := range recall.New(recallPort.Reads(), memory.PartitionsOf(id, d.Agents[id].Memory.ReadNamespaces)) {
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
			return nil, fmt.Errorf("wechat-bot: create the plan workspace %s: %w", planRoot, err)
		}
	}
	if d.declaresFileTools(id) {
		surface, err := d.planFileTools(id, planRoot)
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

func (d *hostDocument) planBackend() spec.Backend {
	if d.specBackend != nil {
		return d.specBackend
	}
	return spec.NewOpenSpecBackend()
}

// planFileTools builds the file surface one declared agent's declaration grants.
// The write root is the plan workspace when the agent declares the plan tool —
// the plan backend reports it, so the deployment names no storage directory of
// its own — and the declared base directory otherwise.
func (d *hostDocument) planFileTools(id, writeRoot string) ([]trpctool.CallableTool, error) {
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
	readRoots, err := deploymentReadRoots(d.Agents[id].File.ReadRoots)
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: resolve the read roots of agent %q: %w", id, err)
	}
	surface, err := tagentfile.New(tagentfile.Options{BaseDir: base, ReadRoots: readRoots, Tools: granted})
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: construct the file surface of agent %q: %w", id, err)
	}
	tools := make([]trpctool.CallableTool, 0, len(granted))
	for _, candidate := range surface.Tools(context.Background()) {
		callable, ok := candidate.(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("wechat-bot: a file tool of agent %q carries no native call surface", id)
		}
		tools = append(tools, callable)
	}
	return tools, nil
}

func deploymentReadRoots(declared []string) ([]string, error) {
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

// declaresFileTools reports whether one declared agent names any file tool.
func (d *hostDocument) declaresFileTools(id string) bool {
	for _, name := range tagentfile.Names() {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

func (d *hostDocument) declaresRecall(id string) bool {
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

func (d *hostDocument) declaresKnowledge(id string) bool {
	for _, name := range []string{knowledge.SkillSearchToolName, knowledge.SkillLoadToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// knowledgeTools constructs the skill surface of one declared agent: its skill
// roots and the read root its file reads are confined to.
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

func (d *hostDocument) skillRoots(id string) []string {
	roots := make([]string, 0, len(d.Agents[id].SkillDirs))
	for _, dir := range d.Agents[id].SkillDirs {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	return roots
}

func (d *hostDocument) readRoot(id string) string {
	if root := strings.TrimSpace(d.Agents[id].ReadRoot); root != "" {
		return root
	}
	return "."
}

func (d *hostDocument) requestTimeout() time.Duration {
	if d.TimeoutSeconds > 0 {
		return time.Duration(d.TimeoutSeconds) * time.Second
	}
	return 10 * time.Minute
}

// modelFor constructs the native model of one declared agent from its provider
// entry. Chat telemetry stays on the entry agent's model only: a delegated turn
// runs on the framework flow which reports its own span already.
func (d *hostDocument) modelFor(id string, client *http.Client) (trpcmodel.Model, error) {
	entry := d.Agents[id]
	providerName := strings.TrimSpace(entry.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(d.Provider)
	}
	provider, ok := d.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("wechat-bot: the host document declares no provider %q", providerName)
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

// hostRuntime is the assembled host of one process.
type hostRuntime struct {
	run    *tagent.Run
	router *SessionRouter
	inbox  *internalInbox
	sender *hostSender
	seen   *SeenStore
	gates  sync.Map

	meditationPlan *meditationPlan
	meditation     *meditationDriver

	chatMu   sync.Mutex
	lastChat string
}

// inboundMessage is one channel delivery the host admits.
type inboundMessage struct {
	Key      string
	ChatID   string
	UserName string
	Text     string
}

// relations reports the causal relation ledger the recall trace walks.
func (d *hostDocument) relations(plan *tagent.MemoryPlan) (*replay.MemoryRelations, error) {
	cfg := plan.Storage()
	if cfg == nil {
		return replay.NewMemoryRelations(), nil
	}
	root := filepath.Join(cfg.Root, "relations")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("wechat-bot: create the relation journal %s: %w", root, err)
	}
	relations, err := replay.OpenMemoryRelations(root)
	if err != nil {
		return nil, fmt.Errorf("wechat-bot: open the relation journal %s: %w", root, err)
	}
	return relations, nil
}

// assembleHost builds the runtime of one document and returns the host that drives it.
func assembleHost(ctx context.Context, doc *hostDocument, gateway *approvalGateway, seen *SeenStore) (*hostRuntime, error) {
	send := &hostSendSeam{}
	decl, err := doc.Declaration()
	if err != nil {
		return nil, err
	}
	plan, err := tagent.MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		return nil, err
	}
	relations, err := doc.relations(plan)
	if err != nil {
		return nil, err
	}
	embedder, err := plan.Embedder()
	if err != nil {
		return nil, err
	}
	recallPort := tagent.NewRecallPort(embedder, relations)
	natives, err := doc.nativeAgents(ctx, send, recallPort)
	if err != nil {
		return nil, err
	}
	host, err := assembleHostFrom(ctx, doc, gateway, seen, send, natives, plan)
	if err != nil {
		return nil, err
	}
	recallPort.Bind(host.run)
	return host, nil
}

// assembleHostFrom builds the host around the live pieces every declared agent is
// assembled with, so a caller that builds its own models and tools drives the
// very same assembly and outbound path this process runs.
func assembleHostFrom(ctx context.Context, doc *hostDocument, gateway *approvalGateway, seen *SeenStore, seam *hostSendSeam, natives map[event.AgentID]tagent.NativeAgent, memoryPlan *tagent.MemoryPlan) (*hostRuntime, error) {
	decl, err := doc.Declaration()
	if err != nil {
		return nil, err
	}
	router, err := NewSessionRouter(event.AgentID(decl.Entry), sessionPrefix(doc))
	if err != nil {
		return nil, err
	}
	if memoryPlan == nil {
		memoryPlan, err = tagent.MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
		if err != nil {
			return nil, err
		}
	}
	opts := []tagent.Option{
		tagent.WithGovernance(evolution.NewPolicy(evolution.NewStructuralGate())),
		tagent.WithTelemetry(telemetry.NewLogObserver(log.Default)),
	}
	if memoryPlan != nil {
		opts = append(opts, tagent.WithMemoryPlan(memoryPlan))
	}
	var storageRoot string
	if memoryPlan != nil && memoryPlan.Storage() != nil {
		storageRoot = memoryPlan.Storage().Root
	}
	mapped, err := doc.document()
	if err != nil {
		return nil, err
	}
	meditationPlan, err := meditationPlanOf(mapped, doc.PromptDir, storageRoot)
	if err != nil {
		return nil, err
	}
	if meditationPlan != nil {
		opts = append(opts, tagent.WithReflectionAnchors(meditationPlan.anchors))
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
		meditationPlan: meditationPlan,
	}
	seam.bind(func(ctx context.Context, addr event.Address, content string) (string, error) {
		return host.deliver(ctx, addr, sendToolLineage, content)
	})
	return host, nil
}

// sessionPrefix reports the session identity prefix the document's addresses are
// spelled with.
func sessionPrefix(doc *hostDocument) string { return sessionPrefixOf(doc.Entry) }

func sessionPrefixOf(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "wechat-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
}

// Close drains the runtime and reports what stayed open.
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
	return h.run.CloseMCPServers()
}

// Admit claims the dedup identity of one channel delivery.
func (h *hostRuntime) Admit(key string) error {
	if h.seen == nil || key == "" {
		return nil
	}
	if !h.seen.CheckAndMark(key) {
		return fmt.Errorf("wechat-bot: the inbound message was already admitted: %s", key)
	}
	return nil
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
			addr: addr, userName: userName, triggerSource: "user", content: content,
		}); err != nil {
			log.Warnf("[host] delivery refused for %s: %v", addr.SessionID, err)
			continue
		}
		delivered = true
	}
	if !delivered && turn.Terminal == tagentmodel.KindFailed {
		_, _ = h.sender.sendToUser(ctx, sendRequest{
			addr: addr, userName: userName, triggerSource: "error",
			content: "执行出错：本轮模型回合失败，未产出交付内容。",
		})
	}
}

func (h *hostRuntime) rememberChat(chatID string) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return
	}
	h.chatMu.Lock()
	h.lastChat = trimmed
	h.chatMu.Unlock()
}

// mostRecentChat reports the chat that last delivered, and no chat beside none.
func (h *hostRuntime) mostRecentChat() string {
	h.chatMu.Lock()
	defer h.chatMu.Unlock()
	return h.lastChat
}

// deliver is the one outbound entry both channels reach.
func (h *hostRuntime) deliver(ctx context.Context, addr event.Address, source, content string) (string, error) {
	return h.sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: source, content: content})
}

// lockAddress serializes the drives of one session.
func (h *hostRuntime) lockAddress(addr event.Address) func() {
	value, _ := h.gates.LoadOrStore(addr, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// internalInbox holds the internal notices a session has to read on its next turn.
type internalInbox struct {
	mu      sync.Mutex
	byAddr  map[event.Address][]tagentmodel.Message
	pending []tagentmodel.Message
}

func newInternalInbox() *internalInbox {
	return &internalInbox{byAddr: map[event.Address][]tagentmodel.Message{}}
}

// InjectInternal queues one internal notice for the named address; an incomplete
// address queues it for the first turn of any session.
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

// meditationDefaultInterval is the tick cadence of a declaration that names
// none; the declaration face keeps the same default the engine used.
const meditationDefaultInterval = 30 * time.Minute

// meditationPlan is the one scheduled reflection a document declares, resolved
// at assembly: the observing agent, its loop session, the cadence, the prompt
// the input carries, the resolved observation face, the declared delivery
// targets and the anchor record its watermark persists in.
type meditationPlan struct {
	agent    string
	session  string
	interval time.Duration
	minGap   time.Duration
	prompt   string
	observed []string
	reads    []string
	targets  []string
	anchors  string
}

// meditationPlanOf resolves the one meditation section doc enables: the entry
// agent is resolved first and the remaining agents in declared order, so one
// document resolves to one plan across restarts. A document enabling none
// reports a nil plan. Every inconsistent section is refused by name at assembly
// rather than driven into a reflection the declaration did not state.
func meditationPlanOf(doc document.Document, promptDir, storageRoot string) (*meditationPlan, error) {
	var plan *meditationPlan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec, ok := doc.Agents[id]
		if !ok || !spec.Meditation.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("wechat-bot: agents %q and %q both enable meditation, and one host drives one reflection loop",
				declaredBy, id)
		}
		entry := spec.Meditation
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("wechat-bot: agent %q declares a negative meditation interval %s", id, interval)
		}
		if interval == 0 {
			interval = meditationDefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("wechat-bot: agent %q declares a negative meditation min_gap %s", id, entry.MinGap)
		}
		prompt, err := meditationPromptOf(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := meditation.ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("wechat-bot: %w", err)
		}
		targets, err := declaredMeditationTargets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("wechat-bot: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist", id)
		}
		plan = &meditationPlan{
			agent:    id,
			session:  sessionPrefixOf(doc.Entry) + "meditation",
			interval: interval,
			minGap:   entry.MinGap,
			prompt:   prompt,
			observed: scope.Names,
			reads:    reads,
			targets:  targets,
			anchors:  filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// meditationPromptOf reads the static prompt one enabled meditation carries. A
// relative path is resolved beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly.
func meditationPromptOf(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("wechat-bot: agent %q enables meditation but names no prompt_file", id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("wechat-bot: agent %q cannot read the meditation prompt %s: %w", id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("wechat-bot: agent %q declares an empty meditation prompt %s", id, path)
	}
	return string(body), nil
}

// declaredMeditationTargets resolves the delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and a section that names no target is refused by name.
func declaredMeditationTargets(doc document.Document, id string, declared []string) ([]string, error) {
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("wechat-bot: agent %q delivers its meditation to the undeclared agent %q", id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, trimmed)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("wechat-bot: agent %q enables meditation but names no deliver_to target", id)
	}
	return targets, nil
}

// meditationDriver drives one declared reflection loop: each tick reads the
// observed face through the reflection chain, injects the reflection input into
// the observing agent's own loop session, and hands a fired reflection's output
// to the delivery port.
type meditationDriver struct {
	agent      string
	interval   time.Duration
	targets    []event.AgentID
	source     event.Address
	reflection *meditation.Reflection
	deliver    func(context.Context, event.Address, []event.AgentID)

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (h *hostRuntime) newMeditationDriver() (*meditationDriver, error) {
	gate := h.run.ReflectionGate()
	if gate == nil {
		return nil, fmt.Errorf("wechat-bot: the assembly carries no reflection anchor store")
	}
	plan := h.meditationPlan
	reflection, err := meditation.NewReflection(meditation.ReflectionConfig{
		Loop:    meditation.ReflectionLoop{Agent: plan.agent, Session: plan.session},
		Face:    meditation.ReflectionFace{ObservedNamespaces: plan.observed, ReadNamespaces: plan.reads},
		Cadence: meditation.ReflectionCadence{Interval: plan.interval, MinGap: plan.minGap},
		Prompt:  plan.prompt,
	}, gate, h.run.ObservationFacts(), h.run.ReflectionInput())
	if err != nil {
		return nil, err
	}
	return &meditationDriver{
		agent:      plan.agent,
		interval:   plan.interval,
		targets:    meditationTargets(plan.targets),
		source:     event.Address{AgentID: event.AgentID(plan.agent), SessionID: event.SessionID(plan.session)},
		reflection: reflection,
		deliver:    h.deliverReflection,
	}, nil
}

// startMeditation starts the scheduled reflection beside the host.
func (h *hostRuntime) startMeditation(ctx context.Context) error {
	if h == nil || h.meditationPlan == nil || h.meditation != nil {
		return nil
	}
	driver, err := h.newMeditationDriver()
	if err != nil {
		return err
	}
	driver.start(ctx)
	h.meditation = driver
	return nil
}

// start begins the tick loop: the first tick runs one interval after the start,
// so a restart never fires a reflection the previous process just consumed.
func (d *meditationDriver) start(ctx context.Context) {
	d.mu.Lock()
	if d.cancel != nil {
		d.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	d.cancel, d.done = cancel, done
	d.mu.Unlock()
	go d.run(loopCtx, done)
}

func (d *meditationDriver) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tick(ctx)
		}
	}
}

// tick runs one scheduled reflection and, when it fired, hands what the
// reflection turn produced to the delivery port.
func (d *meditationDriver) tick(ctx context.Context) {
	outcome := d.reflection.Tick(ctx, time.Now())
	if outcome.Decision.Disposition != meditation.TickFired {
		log.Debugf("wechat-bot: meditation driver %s tick %s (%s)", d.agent, outcome.Decision.Disposition, outcome.Decision.Reason)
		return
	}
	delivery := meditation.DeliveryPolicy{}
	if outcome.Injection == nil || !delivery.Deliverable(outcome.Injection.Event) {
		log.Debugf("wechat-bot: meditation driver %s withheld reflection: the fired input carries no host-visible lineage", d.agent)
		return
	}
	log.Infof("wechat-bot: meditation driver %s fired reflection %s", d.agent, outcome.Injection.Key)
	d.deliver(ctx, d.source, d.targets)
}

// Stop ends the tick loop and waits for the tick in flight to return.
func (d *meditationDriver) Stop() {
	d.mu.Lock()
	cancel, done := d.cancel, d.done
	d.cancel, d.done = nil, nil
	d.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func meditationTargets(names []string) []event.AgentID {
	ids := make([]event.AgentID, 0, len(names))
	for _, name := range names {
		ids = append(ids, event.AgentID(name))
	}
	return ids
}

// deliverReflection hands the fresh assistant output of the reflection turn to
// every chat the declared target agent has bound.
func (h *hostRuntime) deliverReflection(ctx context.Context, source event.Address, targets []event.AgentID) {
	if h.sender == nil {
		log.Warnf("[meditation] %s fired beside no outbound path, the result is withheld", source.AgentID)
		return
	}
	contents := assistantContents(h.router.TakeUndelivered(source, h.run.SessionHistory(source)))
	if len(contents) == 0 {
		log.Debugf("[meditation] %s produced no deliverable assistant output", source.AgentID)
		return
	}
	for _, target := range targets {
		addresses := h.router.BoundAddresses(target)
		if len(addresses) == 0 {
			log.Warnf("[meditation] target %q holds no bound chat, %d message(s) withheld", target, len(contents))
			continue
		}
		for _, addr := range addresses {
			for _, content := range contents {
				if _, err := h.sender.sendToUser(ctx, sendRequest{
					addr: addr, triggerSource: meditation.LineageReflection, content: content,
				}); err != nil {
					log.Warnf("[meditation] delivery to %s refused: %v", addr.SessionID, err)
				}
			}
		}
	}
}

func assistantContents(messages []tagentmodel.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != tagentmodel.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return contents
}

// sendToolLineage 是主动通道回执的血统标注：与投递终态回执同族，落在门内——只留痕，不自达。
const sendToolLineage = "send-tool"

// errNoSendTarget 点名"该会址没有绑定的送达目标"：发送缝据此回模型具名拒绝，扣留而非猜测。
var errNoSendTarget = errors.New("无可解析的送达目标（本会址未绑定会话）")

// sendRequest 描述一次送达意图：正文、该次送达的会址与血统标注。
//   - addr 是本次调用的显式会址；目标只经路由表按精确会址解析，不经任何全局最近会话回退；
//   - triggerSource 是日志与回执的血统标注；userName 只参与目标标签渲染。
type sendRequest struct {
	addr          event.Address
	userName      string
	triggerSource string
	content       string
}

// hostSender 承载宿主唯一的出网发送通路：被动通道（回合结束的交付）与主动通道
// （send 工具）汇合于 sendToUser，typing 清理、长文分片、附件投递、失败回执一条不漏。
type hostSender struct {
	bot          wechatOutbound
	longText     longTextFunc
	typingActive *sync.Map
	router       *SessionRouter
	inj          receiptInjector
	workspaceDir string
	sends        atomic.Uint64
}

// sendToUser 把一次送达意图落成实际投递，返回实际送达目标的标签。
//   - 目标按精确会址解析：会址须完整且已绑定会话，否则具名拒绝（不广播、不猜测、不回退他会话）；
//   - 文本发送失败记 send-failed 回执并把原因随 error 回调用方，附件投递照常发生。
func (h *hostSender) sendToUser(ctx context.Context, req sendRequest) (string, error) {
	h.sends.Add(1)

	target := ""
	if req.addr.IsComplete() && h.router != nil {
		target, _ = h.router.Target(req.addr)
	}
	if target == "" {
		log.Warnf("[Agent][%s] 会址未绑定会话，无可回退目标，扣留: %s", req.triggerSource, truncateLog(req.content))
		emitReceipt(h.inj, req.addr, "no-target", "WARN", req.triggerSource, "", req.content)
		return "", errNoSendTarget
	}
	if startTime, ok := h.typingActive.Load(target); ok {
		if t, ok := startTime.(time.Time); ok && time.Since(t) < 60*time.Second {
			_ = h.bot.StopTyping(ctx, target)
		}
		h.typingActive.Delete(target)
	}

	userLabel := target
	if req.userName != "" {
		userLabel = fmt.Sprintf("%s(%s)", req.userName, target)
	}
	log.Infof("[Agent][%s->%s] %s", req.triggerSource, userLabel, truncateLog(req.content))

	originalContent := req.content
	content := req.content
	textSent := false
	if len(content) > 2000 {
		token, _ := h.bot.GetContextToken(target)
		if token != "" && h.longText != nil {
			if err := h.longText(ctx, target, content, token); err == nil {
				textSent = true
			} else {
				content = content[:2000] + "\n\n[Message truncated]"
			}
		} else {
			content = content[:2000] + "\n\n[Message truncated]"
		}
	}
	var sendErr error
	if !textSent {
		if err := h.bot.SendTextToUser(ctx, target, content); err != nil {
			log.Errorf("SendTextToUser failed for %s: %v", target, err)
			emitReceipt(h.inj, req.addr, "send-failed", "ERROR", req.triggerSource, target, content)
			sendErr = err
		}
	}

	h.deliverFiles(ctx, req, target, originalContent)
	return userLabel, sendErr
}

// deliverFiles 把一条回复命名的文件交给通道：每一笔拒绝与失败都以具名回执回流该会话，
// 已投递的正文不受影响，一个文件失败也不阻断其余文件。
func (h *hostSender) deliverFiles(ctx context.Context, req sendRequest, target, content string) fileDeliveryReport {
	return fileDelivery{
		sender:        h.bot,
		chatID:        target,
		content:       content,
		workspaceDir:  h.workspaceDir,
		addr:          req.addr,
		triggerSource: req.triggerSource,
		receipts:      h.inj,
	}.deliver(ctx)
}

// hostSendFunc is the late-bound send seam the tool instances carry: one
// explicit address beside one body.
type hostSendFunc func(ctx context.Context, addr event.Address, content string) (string, error)

// hostSendSeam 是主动通道的晚绑定发送缝：工具实例先于 bot 产出，绑定完成后调用直达 hostSender。
type hostSendSeam struct {
	mu   sync.RWMutex
	send hostSendFunc
}

// bind registers the host-side delivery function of this process.
func (s *hostSendSeam) bind(send hostSendFunc) {
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
}

// Send delivers one body at the explicit address the call carries. An unbound
// seam refuses by name instead of panicking.
func (s *hostSendSeam) Send(ctx context.Context, addr event.Address, content string) (string, error) {
	s.mu.RLock()
	send := s.send
	s.mu.RUnlock()
	if send == nil {
		return "", errors.New("宿主发送通路尚未就绪（bot 未完成装配），本次发送无法执行")
	}
	return send(ctx, addr, content)
}

// receiptLineage labels the internal delivery receipts fed back to the agent. It
// stays outside every deliverable lineage: a receipt turn is internal, its own
// sending never receipts, and a withheld body travels with the receipt of the
// address that produced it.
const receiptLineage = "delivery_receipt"

// receiptInjector is the narrow host-side view of the inbox a receipt is queued
// into; *internalInbox satisfies it.
type receiptInjector interface {
	InjectInternal(addr event.Address, source string, msg tagentmodel.Message)
}

// emitReceipt renders one receipt, logs it at its grade and queues it for the
// address whose delivery ended without reaching the user. A receipt of the
// receipt lineage itself is dropped: the no-recursion red line.
func emitReceipt(inj receiptInjector, addr event.Address, reason, level, triggerSource, target, content string) {
	if inj == nil || triggerSource == receiptLineage {
		return
	}
	line := fmt.Sprintf("[delivery-receipt] 投递终态=%s 血统=%s 目标=%s：%s",
		reason, triggerSource, orNoTarget(target), truncateLog(content))
	if level == "ERROR" {
		log.Errorf("%s", line)
	} else {
		log.Warnf("%s", line)
	}
	inj.InjectInternal(addr, receiptLineage, tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: line + "\n该内容未达用户；若属用户等待的交付，请在用户回合投递。",
	})
}

// orNoTarget names the missing delivery target of one receipt.
func orNoTarget(target string) string {
	if target == "" {
		return "(无目标)"
	}
	return target
}

// truncateLog shortens a body for one log line.
func truncateLog(s string) string { return truncateLogN(s, 120) }

// truncateLogN shortens a body to at most n bytes for one log line.
func truncateLogN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// executableExts 为拒绝发送的可执行文件扩展名（与权限位检查互补）。硬性规则，不可配置。
var executableExts = map[string]bool{
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".sh": true,
	".bin": true, ".out": true, ".app": true, ".msi": true, ".com": true, ".run": true,
}

// pathCandidateRE 匹配以扩展名结尾的路径候选（绝对或相对）。字符类使用 \p{L}/\p{N}
// 以正确识别中文等非 ASCII 文件名；具体是否为真实文件由 os.Stat 二次校验。
var pathCandidateRE = regexp.MustCompile(`[\p{L}\p{N}_./-]+\.[\p{L}\p{N}-]{1,10}`)

// executableRefusal 是拒绝投递可执行文件的固定规则：可执行权限位或拒绝扩展名命中即拒绝，
// 没有任何配置面能放宽它。
const executableRefusal = "可执行文件不投递（权限位或拒绝扩展名命中，硬规则不可配置）"

// fileCandidate 是回复文本命名的一个本地文件，连同决定它能否投递的规则：
// refusal 为空表示可投递，非空表示宿主在发送前按具名规则拒绝。
type fileCandidate struct {
	Path    string
	Refusal string
}

// classifyFilePaths 按六条识别规则读出回复文本命名的本地文件（本地路径形态、排除 URL、
// 必须带扩展名、必须存在且为普通文件、拒绝可执行、去重保序），保持首次出现顺序。
func classifyFilePaths(text, workspaceDir string) []fileCandidate {
	seen := make(map[string]bool)
	var result []fileCandidate
	for _, cand := range pathCandidateRE.FindAllString(text, -1) {
		if strings.Contains(cand, "://") {
			continue
		}
		var abs string
		switch {
		case strings.HasPrefix(cand, "/"):
			abs = cand
		case strings.HasPrefix(cand, "./"), strings.HasPrefix(cand, "../"), strings.Contains(cand, "/"):
			if workspaceDir == "" {
				continue
			}
			abs = filepath.Join(workspaceDir, cand)
		default:
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() || seen[abs] {
			continue
		}
		seen[abs] = true
		candidate := fileCandidate{Path: abs}
		if isExecutable(info, abs) {
			candidate.Refusal = executableRefusal
		}
		result = append(result, candidate)
	}
	return result
}

// voiceMaxBytes is the 5MB cap for audio sent over the voice channel:
// audio larger than this falls back to a plain file message (voice CDN rejects long audio).
const voiceMaxBytes = 5 << 20

// isExecutable 判断文件是否为可执行文件：具有任意可执行权限位，或扩展名在拒绝列表中。
func isExecutable(info os.FileInfo, path string) bool {
	if info.Mode()&0o111 != 0 {
		return true
	}
	return executableExts[strings.ToLower(filepath.Ext(path))]
}

// fileSendFn 封装"按类型选择的具体发送动作"，便于统一调用与测试。
type fileSendFn func(sender FileSender, ctx context.Context, toUserID, path string) error

// selectSendFn 根据扩展名选择微信发送接口。voice 的 duration 传 0，由 SDK/微信侧处理；
// 超过 5MB 的音频回退为普通文件消息。
func selectSendFn(ext string) fileSendFn {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendImageFromPath(ctx, toUserID, path)
		}
	case ".mp3", ".wav", ".amr", ".m4a":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			if info, err := os.Stat(path); err == nil && info.Size() > voiceMaxBytes {
				return s.SendFileFromPath(ctx, toUserID, path)
			}
			return s.SendVoiceFromPath(ctx, toUserID, path, 0)
		}
	case ".mp4", ".mov", ".avi", ".webm", ".mkv":
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendVideoFromPath(ctx, toUserID, path)
		}
	default:
		return func(s FileSender, ctx context.Context, toUserID, path string) error {
			return s.SendFileFromPath(ctx, toUserID, path)
		}
	}
}

// fileRefusal 是宿主在发送前按具名规则拒绝投递的一个已识别文件：路径与拒绝理由。
type fileRefusal struct {
	Path   string
	Reason string
}

// fileFailure 是通道拒绝投递的一个文件，连同它给出的原因。
type fileFailure struct {
	Path string
	Err  error
}

// fileDeliveryReport 是一次回复的文件投递账目：已送达、宿主按具名规则拒绝、通道投递失败。
type fileDeliveryReport struct {
	Delivered []string
	Refused   []fileRefusal
	Failed    []fileFailure
}

// fileDelivery 是一次回复的出海文件工作：读路径的正文、投递到的会话、唯一的出网发送面。
// 文本投递由调用方负责，因此任一文件失败都不阻断文本；一个文件失败也不阻断其余文件。
type fileDelivery struct {
	sender        FileSender
	chatID        string
	content       string
	workspaceDir  string
	addr          event.Address
	triggerSource string
	receipts      receiptInjector
}

// deliver 投递回复命名的每个文件并记下每一笔拒绝与失败：拒绝在发送前判定，
// 失败连同原因入账且循环继续。
func (d fileDelivery) deliver(ctx context.Context) fileDeliveryReport {
	report := fileDeliveryReport{}
	for _, cand := range classifyFilePaths(d.content, d.workspaceDir) {
		if cand.Refusal != "" {
			report.Refused = append(report.Refused, fileRefusal{Path: cand.Path, Reason: cand.Refusal})
			emitReceipt(d.receipts, d.addr, "file-denied", "WARN", d.triggerSource, d.chatID, cand.Refusal+"："+cand.Path)
			continue
		}
		if err := d.sendFile(ctx, cand.Path); err != nil {
			log.Errorf("发送文件失败 %s: %v", cand.Path, err)
			report.Failed = append(report.Failed, fileFailure{Path: cand.Path, Err: err})
			emitReceipt(d.receipts, d.addr, "file-failed", "ERROR", d.triggerSource, d.chatID, err.Error()+"："+cand.Path)
			continue
		}
		report.Delivered = append(report.Delivered, cand.Path)
	}
	return report
}

// sendFile 把一个文件交给通道：按扩展名选择图片/语音/视频/文件接口。
func (d fileDelivery) sendFile(ctx context.Context, path string) error {
	return selectSendFn(filepath.Ext(path))(d.sender, ctx, d.chatID, path)
}

const (
	uploadsSubdir = "uploads"

	// maxInboundFileSize 为入站单文件上限。容器 workspace 为 256m tmpfs，
	// 且 SDK 下载为全量 []byte，上限过大会同时威胁磁盘与内存。
	maxInboundFileSize = 50 * 1024 * 1024

	// longTextThreshold 超过该字符数（rune 计）的入站文本转存为 .txt 文件。
	longTextThreshold = 1000

	// previewRunes 长文本转文件后注入的预览字符数。
	previewRunes = 200
)

// SavedFile 描述一个已落盘的入站文件。
type SavedFile struct {
	Path     string
	OrigName string
	Size     int
}

// IntakeOutcome 是一条媒体消息的接收结果。
type IntakeOutcome struct {
	Saved      []SavedFile
	Transcript string
	Rejects    []string
}

// InboundKind 是入站消息的处理分类。
type InboundKind int

const (
	// InboundIgnore 文本为空且非媒体，不进入会话。
	InboundIgnore InboundKind = iota
	// InboundShortText 其余文本：未超过长文本阈值，或工作区未配置时的降级归类。
	InboundShortText
	// InboundLongText 文本 rune 数超过 longTextThreshold，且 workspaceConfigured 为真。
	InboundLongText
	// InboundMedia 消息含图片/语音/文件/视频之一。
	InboundMedia
)

// ClassifyInbound 对入站消息分类。纯函数。workspaceConfigured 为 false 时长文本降级为
// ShortText（保持现状行为）。
func ClassifyInbound(msg *wechat.Message, workspaceConfigured bool) InboundKind {
	if msg.IsImage() || msg.IsVoice() || msg.IsFile() || msg.IsVideo() {
		return InboundMedia
	}
	text := msg.Text()
	if text == "" {
		return InboundIgnore
	}
	if workspaceConfigured && utf8.RuneCountInString(text) > longTextThreshold {
		return InboundLongText
	}
	return InboundShortText
}

// unsafeNameChars 匹配文件名中需要替换的字符（白名单之外的一切）。
var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeName 将不可信文件名/用户 ID 清洗为安全路径段：仅保留 [A-Za-z0-9._-]，
// 去除路径分隔符与 ".."，剥掉前导点（防隐藏文件与相对段）。
func sanitizeName(name string) string {
	name = unsafeNameChars.ReplaceAllString(name, "_")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "_" {
		name = "file"
	}
	return name
}

// buildSavePath 构造落盘路径 {workspaceDir}/uploads/{chatID}/{ts}_{name}，
// 规范化后强制校验仍位于 uploads 目录内（防穿越，双保险）。
func buildSavePath(workspaceDir, chatID, name string, now time.Time) (string, error) {
	base := filepath.Clean(filepath.Join(workspaceDir, uploadsSubdir))
	stamped := now.Format("20060102-150405") + "_" + sanitizeName(name)
	path := filepath.Join(base, sanitizeName(chatID), stamped)
	if !strings.HasPrefix(filepath.Clean(path), base+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path escapes uploads dir: %q", name)
	}
	return path, nil
}

// createExclusive 以 O_CREATE|O_EXCL 原子创建文件，同名冲突时追加序号重试。
func createExclusive(path string) (*os.File, string, error) {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 1; ; i++ {
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, candidate, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
		candidate = stem + "-" + strconv.Itoa(i) + ext
	}
}

// saveInboundFile 校验并落盘一个入站文件。拒绝可执行扩展名；复核实际字节数；
// 权限 0644（无执行位）；写入失败时删除残留的不完整文件。
func saveInboundFile(workspaceDir, chatID, origName string, data []byte, now time.Time) (SavedFile, error) {
	if executableExts[strings.ToLower(filepath.Ext(origName))] {
		return SavedFile{}, fmt.Errorf("可执行文件不允许接收: %s", origName)
	}
	if len(data) > maxInboundFileSize {
		return SavedFile{}, fmt.Errorf("文件超过大小上限 %s: %s (%s)",
			formatSize(maxInboundFileSize), origName, formatSize(len(data)))
	}
	path, err := buildSavePath(workspaceDir, chatID, origName, now)
	if err != nil {
		return SavedFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return SavedFile{}, fmt.Errorf("create upload dir: %w", err)
	}
	f, finalPath, err := createExclusive(path)
	if err != nil {
		return SavedFile{}, fmt.Errorf("create upload file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(finalPath)
		return SavedFile{}, fmt.Errorf("write upload file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(finalPath)
		return SavedFile{}, fmt.Errorf("close upload file: %w", err)
	}
	return SavedFile{Path: finalPath, OrigName: origName, Size: len(data)}, nil
}

// downloadSem 限制全局并发下载数（带宽保护/CDN 友好）。
var downloadSem = make(chan struct{}, 2)

// IntakeMedia 处理一条含媒体 item 的消息：逐 item 预判大小（超限免下载）→ O_EXCL 创建目标文件
// → SDK 流式下载直落盘（MaxSize 传输中强制）。单 item 失败/被拒不阻断其余 item；
// 原因归入 Rejects（面向用户，不注入 agent）；任何失败路径删除不完整残留文件。
func IntakeMedia(ctx context.Context, dl MediaDownloader, cdnBaseURL, workspaceDir, chatID string, msg *wechat.Message, now time.Time) IntakeOutcome {
	var out IntakeOutcome
	opts := wechat.DownloadOptions{MaxSize: maxInboundFileSize}
	for _, item := range msg.ItemList {
		var (
			origName string
			declared int64
			download func(w io.Writer) (int64, error)
		)
		switch {
		case item.Type == wechat.ItemTypeImage && item.ImageItem != nil:
			img := item.ImageItem
			origName = "image.jpg"
			declared = img.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadImageFromItemTo(ctx, cdnBaseURL, img, w, opts)
			}
		case item.Type == wechat.ItemTypeVoice && item.VoiceItem != nil:
			voice := item.VoiceItem
			out.Transcript = voice.Text
			origName = "voice.silk"
			declared = voice.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadVoiceTo(ctx, voice, cdnBaseURL, w, opts)
			}
		case item.Type == wechat.ItemTypeFile && item.FileItem != nil:
			file := item.FileItem
			origName = file.FileName
			if origName == "" {
				origName = "attachment.dat"
			}
			declared = file.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadFileFromItemTo(ctx, file, cdnBaseURL, w, opts)
			}
		case item.Type == wechat.ItemTypeVideo && item.VideoItem != nil:
			video := item.VideoItem
			origName = "video.mp4"
			declared = video.DeclaredSize()
			download = func(w io.Writer) (int64, error) {
				return dl.DownloadVideoFromItemTo(ctx, video, cdnBaseURL, w, opts)
			}
		default:
			continue
		}

		if executableExts[strings.ToLower(filepath.Ext(origName))] {
			out.Rejects = append(out.Rejects, fmt.Sprintf("已拒收可执行文件: %s", origName))
			continue
		}
		if declared > maxInboundFileSize {
			out.Rejects = append(out.Rejects, fmt.Sprintf("文件 %s 超过大小上限 %s（%s），未接收",
				origName, formatSize(maxInboundFileSize), formatSize(int(declared))))
			continue
		}

		path, err := buildSavePath(workspaceDir, chatID, origName, now)
		if err != nil {
			log.Errorf("[Intake] 构造路径失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Errorf("[Intake] 创建目录失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		f, finalPath, err := createExclusive(path)
		if err != nil {
			log.Errorf("[Intake] 创建文件失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}

		downloadSem <- struct{}{}
		written, err := download(f)
		<-downloadSem
		if err != nil {
			f.Close()
			os.Remove(finalPath)
			if errors.Is(err, wechat.ErrMaxSizeExceeded) {
				out.Rejects = append(out.Rejects, fmt.Sprintf("文件 %s 超过大小上限 %s，未接收",
					origName, formatSize(maxInboundFileSize)))
			} else {
				log.Errorf("[Intake] 下载失败 %s (chat=%s): %v", origName, chatID, err)
				out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 下载失败，请重新发送", origName))
			}
			continue
		}
		if err := f.Close(); err != nil {
			os.Remove(finalPath)
			log.Errorf("[Intake] 落盘失败 %s (chat=%s): %v", origName, chatID, err)
			out.Rejects = append(out.Rejects, fmt.Sprintf("附件 %s 保存失败: %v", origName, err))
			continue
		}
		out.Saved = append(out.Saved, SavedFile{Path: finalPath, OrigName: origName, Size: int(written)})
	}
	return out
}

// SaveLongText 将超长文本落盘为 .txt，返回保存结果与注入文本。
func SaveLongText(workspaceDir, chatID, text, nameHint string, now time.Time) (SavedFile, string, error) {
	if nameHint == "" {
		nameHint = "input"
	}
	saved, err := saveInboundFile(workspaceDir, chatID, nameHint+".txt", []byte(text), now)
	if err != nil {
		return SavedFile{}, "", err
	}
	runes := []rune(text)
	preview := string(runes[:min(len(runes), previewRunes)])
	inject := fmt.Sprintf("[用户输入较长，已存为文件]\n文件: %s (共 %d 字)\n预览: %s...",
		saved.Path, len(runes), preview)
	return saved, inject, nil
}

// ComposeMediaInject 拼装媒体消息的注入文本：附件路径列表 + 语音转写 + 用户附言。
// 转写超过阈值时同样转存文件（走 SaveLongText）。无可注入内容时返回空串。
func ComposeMediaInject(outcome IntakeOutcome, userText, workspaceDir, chatID string, now time.Time) string {
	var lines []string
	if len(outcome.Saved) > 0 {
		lines = append(lines, "[用户发来附件]")
		for _, f := range outcome.Saved {
			lines = append(lines, fmt.Sprintf("文件: %s (原名: %s, %s)", f.Path, f.OrigName, formatSize(f.Size)))
		}
	}
	if outcome.Transcript != "" {
		if utf8.RuneCountInString(outcome.Transcript) > longTextThreshold {
			if _, inject, err := SaveLongText(workspaceDir, chatID, outcome.Transcript, "voice_transcript", now); err == nil {
				lines = append(lines, "语音转写较长，已存为文件:", inject)
			} else {
				log.Errorf("[Intake] 转写落盘失败 (chat=%s): %v", chatID, err)
				lines = append(lines, "语音转写: "+outcome.Transcript)
			}
		} else {
			lines = append(lines, "语音转写: "+outcome.Transcript)
		}
	}
	if userText != "" {
		lines = append(lines, userText)
	}
	return strings.Join(lines, "\n")
}

// formatSize 人类可读的字节数。
func formatSize(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// SeenStore provides message-level idempotency across restarts.
//
//   - Dedup key layering: ClientID when non-empty, otherwise uid plus a text hash prefix.
//   - seen.json sits next to the bot config, loads at start, prunes by TTL and capacity, writes
//     atomically; a corrupted file degrades to an empty set so dedup never blocks the pipeline.
type SeenStore struct {
	path     string
	ttl      time.Duration
	capacity int
	mu       sync.Mutex
	seen     map[string]int64
	logger   *slog.Logger
}

// NewSeenStore loads (or initializes) the seen set from dir/seen.json.
func NewSeenStore(dir string, logger *slog.Logger) *SeenStore {
	s := &SeenStore{
		path:     filepath.Join(dir, "seen.json"),
		ttl:      24 * time.Hour,
		capacity: 10000,
		seen:     make(map[string]int64),
		logger:   logger,
	}
	s.load()
	return s
}

func (s *SeenStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("[Dedup] failed to read seen.json, starting empty", "error", err)
		}
		return
	}
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		s.logger.Warn("[Dedup] seen.json corrupted, starting empty", "error", err)
		_ = os.Rename(s.path, s.path+".corrupt")
		return
	}
	now := time.Now().Unix()
	for k, ts := range m {
		if now-ts <= int64(s.ttl.Seconds()) {
			s.seen[k] = ts
		}
	}
	s.logger.Info("[Dedup] seen.json restored", "entries", len(s.seen))
}

// CheckAndMark returns true if the key was new (message should be processed),
// false if it was already seen (duplicate — drop). Marking is persisted
// immediately (atomic write) so a crash right after cannot replay the message.
func (s *SeenStore) CheckAndMark(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[key]; ok {
		s.seen[key] = time.Now().Unix()
		return false
	}
	s.seen[key] = time.Now().Unix()
	s.persistLocked()
	return true
}

// persistLocked prunes (TTL, then capacity) and atomically writes the set.
// Caller must hold s.mu.
func (s *SeenStore) persistLocked() {
	now := time.Now().Unix()
	ttlSec := int64(s.ttl.Seconds())
	for k, ts := range s.seen {
		if now-ts > ttlSec {
			delete(s.seen, k)
		}
	}
	if len(s.seen) > s.capacity {
		type kv struct {
			k string
			v int64
		}
		list := make([]kv, 0, len(s.seen))
		for k, v := range s.seen {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v < list[j].v })
		for i := 0; i < len(list)-s.capacity; i++ {
			delete(s.seen, list[i].k)
		}
	}

	data, err := json.Marshal(s.seen)
	if err != nil {
		s.logger.Warn("[Dedup] marshal seen.json failed", "error", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.logger.Warn("[Dedup] ensure seen dir failed", "error", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".seen-*")
	if err != nil {
		s.logger.Warn("[Dedup] create seen tmp failed", "error", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] write seen tmp failed", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] close seen tmp failed", "error", err)
		return
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("[Dedup] rename seen.json failed", "error", err)
		return
	}
}

// DedupKey builds the layered dedup key for an inbound message.
func DedupKey(msg *wechat.Message) string {
	if msg.ClientID != "" {
		return "cid:" + msg.ClientID
	}
	sum := sha256.Sum256([]byte(msg.Text()))
	return fmt.Sprintf("uid:%s#%s", msg.FromUserID, hex.EncodeToString(sum[:8]))
}

// ErrNoChatAddress reports an inbound message that names no chat identity. The
// refusal is the whole answer: a message without an address is never routed to
// a session that belongs to somebody else.
var ErrNoChatAddress = errors.New("wechat-bot: the inbound message names no chat identity")

// ErrSessionPrefixMissing reports a router constructed without the explicit
// session prefix its address spelling starts with.
var ErrSessionPrefixMissing = errors.New("wechat-bot: the session router carries no session prefix")

// ErrTargetRebind reports a second chat identity claimed for an address that
// already delivered to another chat. The bound target is the delivery identity
// of that session and is never rewritten by a later claim.
var ErrTargetRebind = errors.New("wechat-bot: the address is already bound to another chat identity")

// SessionRouter maps one explicit chat identity to the session address the
// runtime is driven at, and holds the delivery identity of each address it
// admitted.
//
// The mapping is injective in its spelling: one chat identity has exactly one
// session identity and two chat identities never share one, so a restart
// re-derives the same addresses instead of inventing a fresh history.
type SessionRouter struct {
	agentID event.AgentID
	prefix  string

	mu      sync.Mutex
	seq     uint64
	targets map[event.Address]string
	sent    map[event.Address]int
}

// NewSessionRouter returns the router of one declared agent beside the explicit
// session prefix its addresses are spelled with. A missing agent or prefix is
// refused at construction, so no address is ever built from a default.
func NewSessionRouter(agentID event.AgentID, prefix string) (*SessionRouter, error) {
	if agentID == "" {
		return nil, ErrNoChatAddress
	}
	if prefix == "" {
		return nil, ErrSessionPrefixMissing
	}
	return &SessionRouter{
		agentID: agentID,
		prefix:  prefix,
		targets: map[event.Address]string{},
		sent:    map[event.Address]int{},
	}, nil
}

// AddressFor returns the explicit address of one chat identity: the declared
// agent beside the session identity that identity spells. The chat identity is
// percent-escaped, so the spelling is injective over every byte a channel may
// carry and two chats never collapse onto one session.
func (r *SessionRouter) AddressFor(chatID string) (event.Address, error) {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return event.Address{}, ErrNoChatAddress
	}
	escaped := url.PathEscape(trimmed)
	if escaped == "" {
		return event.Address{}, ErrNoChatAddress
	}
	return event.Address{AgentID: r.agentID, SessionID: event.SessionID(r.prefix + escaped)}, nil
}

// Bind records the chat identity a drive was admitted for as the delivery
// identity of its address: a later explicit send from that address reaches the
// chat its own drive came from. A rebound address keeps its first identity and
// reports the conflict instead of silently retargeting the session.
func (r *SessionRouter) Bind(addr event.Address, chatID string) error {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return ErrNoChatAddress
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if bound, ok := r.targets[addr]; ok && bound != trimmed {
		return fmt.Errorf("%w: %s", ErrTargetRebind, addr.SessionID)
	}
	r.targets[addr] = trimmed
	return nil
}

// Target reports the chat identity bound to exactly this address. An address no
// drive ever bound answers not-ok: the delivery face then refuses instead of
// resolving a most-recent chat of another session.
func (r *SessionRouter) Target(addr event.Address) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chatID, ok := r.targets[addr]
	return chatID, ok
}

// BoundAddresses reports the addresses of the named agent that a drive bound to
// a chat identity, in session order.
func (r *SessionRouter) BoundAddresses(agentID event.AgentID) []event.Address {
	if agentID == "" || agentID != r.agentID {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	addresses := make([]event.Address, 0, len(r.targets))
	for addr := range r.targets {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].SessionID < addresses[j].SessionID })
	return addresses
}

// NextInvocation mints the responsibility range identity of one drive of the
// address. The identity is unique per drive and derived from the address, so a
// range never collides with the range of another session.
func (r *SessionRouter) NextInvocation(addr event.Address) event.InvocationID {
	r.mu.Lock()
	r.seq++
	n := r.seq
	r.mu.Unlock()
	return event.InvocationID(string(addr.SessionID) + "#" + strconv.FormatUint(n, 10))
}

// TakeUndelivered returns the transcript messages the address accumulated since
// the previous take and records them as consumed. Delivery reads one session's
// own transcript only, so a reply never carries another session's turn.
func (r *SessionRouter) TakeUndelivered(addr event.Address, history []tagentmodel.Message) []tagentmodel.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	from := r.sent[addr]
	if from > len(history) {
		from = len(history)
	}
	fresh := append([]tagentmodel.Message(nil), history[from:]...)
	r.sent[addr] = len(history)
	return fresh
}

// contentMaxBytes 是单次送达正文的字节上界：一条微信消息不是语料库。
const contentMaxBytes = 8 * 1024

// sendToolID 是声明侧 ToolRef 与工具实例共用的工具名。
const sendToolID = "send"

// SendFunc 是组合根注入的发送缝：把一条正文按显式会址送进宿主唯一的发送通路，
// 返回实际送达目标的标签；error 携带具名拒绝原因，由工具层转写为结果文本。
type SendFunc func(ctx context.Context, addr event.Address, content string) (targetLabel string, err error)

// SendTool 是模型可见的主动送达面：send 是装配完成后绑定的发送缝；
// 未绑定时的调用得到具名拒绝。
type SendTool struct {
	send SendFunc
}

var _ trpctool.CallableTool = (*SendTool)(nil)

// NewSendTool 构造送达面：send 传 nil 表示延后绑定，装配尾段经 SetSend 补上。
func NewSendTool(send SendFunc) *SendTool { return &SendTool{send: send} }

// SetSend 绑定发送缝：通道与目标定位由装配闭包固定，不经参数出入。
func (t *SendTool) SetSend(send SendFunc) { t.send = send }

// sendArgs 是 LLM 可见的参数形态：正文一位，仅此一项。
type sendArgs struct {
	Content string `json:"content"`
}

// Declaration implements tool.CallableTool.
func (t *SendTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        sendToolID,
		Description: "把内容显式送达微信用户：content 是正文（上限 8KiB）。这是与回合触发源无关的主动通道——你在非用户回合想对用户说的话，不经此调用不会自动送达。发送目标取自本次调用的会话地址及其绑定会话，不是参数；失败以 [send_denied] 结果文本回给你，回合照常继续。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"content": {Type: "string", Description: "送达用户的正文，UTF-8，上限 8KiB"},
			},
			Required: []string{"content"},
		},
	}
}

// Call implements tool.CallableTool.
//
//   - 一切拒绝与发送失败都作为结果文本返回给模型（回合继续），目标规则随文案点名；
//   - err 只留给解不出参数形态的协议错，此时回合交由框架处置。
func (t *SendTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if unknown := unknownFields(probe); len(unknown) > 0 {
		return t.denied("参数含声明之外的字段 " + strings.Join(unknown, ", ") +
			"——send 只接受 content，发送目标与通道由宿主固定、不可指定或覆写"), nil
	}
	var args sendArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if args.Content == "" {
		return t.denied("content 不能为空：空正文没有可送达之物"), nil
	}
	if len(args.Content) > contentMaxBytes {
		return t.denied(fmt.Sprintf("content exceeds 8KiB 上界（实际 %d 字节）：凝练成一条消息再发", len(args.Content))), nil
	}
	if t.send == nil {
		return t.denied("宿主未接线发送缝，本次发送无法执行"), nil
	}
	addr, ok := callAddress(ctx)
	if !ok {
		return t.denied("本次调用不携带显式会话地址，无法确定送达目标"), nil
	}
	label, err := t.send(ctx, addr, args.Content)
	if err != nil {
		return t.denied(err.Error()), nil
	}
	log.Debugf("[send] delivered target=%s bytes=%d", label, len(args.Content))
	return "[send_ok] 已送达（目标 " + label + "）；通道=宿主唯一发送通路，长文拆分与附件投递由其一并完成", nil
}

// callAddress reports the explicit invocation address the call frame carries.
func callAddress(ctx context.Context) (event.Address, bool) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok || !frame.Address.IsComplete() {
		return event.Address{}, false
	}
	return frame.Address, true
}

// denied 把一次拒绝渲染为结果文本：具名原因加目标规则现状，run 级 debug 日志即观测面。
func (t *SendTool) denied(reason string) string {
	log.Debugf("[send] refused: %s", reason)
	return fmt.Sprintf("[send_denied] %s；目标规则：本会话地址绑定的会话（不经最近活跃回退）", reason)
}

// unknownFields 点名声明之外的参数字段，排序保证文案可复现。
func unknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for k := range probe {
		if k != "content" {
			out = append(out, "'"+k+"'")
		}
	}
	sort.Strings(out)
	return out
}

// newSendTool 产出声明侧的工具实例：工具只持发送缝，宿主通路经缝晚绑定。
// 只有声明了 {kind: tool, id: send} 的 agent 拿到它——授予即授权。
func newSendTool(seam *hostSendSeam) *SendTool { return NewSendTool(seam.Send) }

// ErrApprovalNotApprover reports a message-channel ruling from a user outside
// the approver whitelist: an empty whitelist refuses every message ruling.
var ErrApprovalNotApprover = errors.New("wechat-bot: the sender is not in the approval whitelist")

// ErrApprovalAmbiguous reports a ruling whose digest prefix names more than one
// pending request: an ambiguous ruling is refused instead of applied to a guess.
var ErrApprovalAmbiguous = errors.New("wechat-bot: the reply names more than one pending approval")

// minApprovalPrefix is the shortest digest prefix a ruling may name.
const minApprovalPrefix = 4

// approvalSender is the narrow outbound face the gateway announces requests
// over; *wechat.Bot satisfies it.
type approvalSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
}

// approvalGateway is the host-side approval face: it delivers one request to
// every configured approver chat and resolves a reply against the manager, so a
// ruling is bound to the arguments that were held.
type approvalGateway struct {
	manager   *approval.ApprovalManager
	enabled   bool
	approvers []string

	mu     sync.RWMutex
	sender approvalSender
}

// newApprovalGateway returns the gateway of one governance section: the request
// directory, the approval validity and the approver whitelist.
func newApprovalGateway(gov governanceEntry, approvers []string) *approvalGateway {
	dir := ""
	if gov.Dir != "" {
		dir = gov.Dir
	}
	ttl := time.Duration(gov.ApprovalTTLMinutes) * time.Minute
	g := &approvalGateway{
		manager: approval.NewApprovalManager(dir, ttl),
		enabled: gov.Enabled,
	}
	for _, id := range approvers {
		if id = strings.TrimSpace(id); id != "" {
			g.approvers = append(g.approvers, id)
		}
	}
	g.manager.AddChannel(g)
	return g
}

// Manager reports the approval manager the execution policy reads its rulings
// through.
func (g *approvalGateway) Manager() *approval.ApprovalManager { return g.manager }

// Enabled reports whether the host attaches the approval policy of this
// governance section to the assembly.
func (g *approvalGateway) Enabled() bool { return g != nil && g.enabled }

// SetSender binds the outbound channel the requests are announced over.
func (g *approvalGateway) SetSender(sender approvalSender) {
	g.mu.Lock()
	g.sender = sender
	g.mu.Unlock()
}

// IsApprover reports whether one user may rule over a request through the
// message channel.
func (g *approvalGateway) IsApprover(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, id := range g.approvers {
		if id == userID {
			return true
		}
	}
	return false
}

// Deliver offers one approval request to every configured approver chat. No
// approver and no sender are named failures: the manager records them as
// visible delivery gaps and the gate keeps waiting.
func (g *approvalGateway) Deliver(ctx context.Context, req approval.ApprovalRequest) error {
	g.mu.RLock()
	sender := g.sender
	g.mu.RUnlock()
	if len(g.approvers) == 0 {
		return errors.New("wechat-bot: no approval recipient is configured (app.wechat.approvers)")
	}
	if sender == nil {
		return errors.New("wechat-bot: the wechat sender is not ready")
	}
	digest := shortDigest(req.ArgsDigest)
	text := fmt.Sprintf("⚠ 审批请求（tool=%s）\ndigest: %s\n批准回复: approve %s ｜ 拒绝: reject %s",
		req.ToolName, digest, digest, digest)
	var failures []error
	for _, to := range g.approvers {
		if err := sender.SendTextToUser(ctx, to, text); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", to, err))
		}
	}
	return errors.Join(failures...)
}

// Resolve reads one ruling reply. It reports handled=false for a message that is
// not a ruling, so the message continues as a chat turn. A ruling from outside
// the whitelist, an ambiguous prefix and an unknown prefix are named refusals;
// the manager decides the matched request under the digest it holds.
func (g *approvalGateway) Resolve(from, text string) (handled bool, reply string, err error) {
	status, prefix, ok := parseApprovalReply(text)
	if !ok {
		return false, "", nil
	}
	by := strings.TrimSpace(from)
	if by != "cli" && !g.IsApprover(by) {
		return true, "你没有审批权限（不在 app.wechat.approvers 白名单）。请由审批人经 CLI（wechat-bot approve <digest>）执行。", ErrApprovalNotApprover
	}
	req, err := g.match(prefix)
	if err != nil {
		log.Warnf("[approval] ruling from %s refused: %v", by, err)
		return true, "审批失败: " + err.Error(), err
	}
	decided, err := g.manager.Decide(req.ID, status, "wechat:"+by)
	if err != nil {
		log.Warnf("[approval] decide %s refused: %v", req.ID, err)
		return true, "审批失败: " + err.Error(), err
	}
	log.Infof("[approval] request %s decided %s by %s (tool=%s)", decided.ID, decided.Status, by, decided.ToolName)
	return true, fmt.Sprintf("审批已记录：%s %s（tool=%s, digest=%s）",
		decided.ID, decided.Status, decided.ToolName, shortDigest(decided.ArgsDigest)), nil
}

// match reports the one pending request whose digest starts with the prefix. An
// unknown prefix and an ambiguous prefix are refused by name.
func (g *approvalGateway) match(prefix string) (*approval.ApprovalRequest, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if len(prefix) < minApprovalPrefix {
		return nil, fmt.Errorf("%w: 前缀至少 %d 位十六进制字符", approval.ErrApprovalNotFound, minApprovalPrefix)
	}
	var found []*approval.ApprovalRequest
	for _, req := range g.manager.Pending() {
		if strings.HasPrefix(strings.ToLower(req.ArgsDigest), prefix) {
			found = append(found, req)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%w: 前缀 %s", approval.ErrApprovalNotFound, prefix)
	case 1:
		return found[0], nil
	default:
		return nil, fmt.Errorf("%w: 前缀 %s 命中 %d 条", ErrApprovalAmbiguous, prefix, len(found))
	}
}

// parseApprovalReply reads one ruling reply: the verb beside the digest prefix
// an approver answers with. A message that carries neither is not a ruling.
func parseApprovalReply(text string) (approval.ApprovalStatus, string, bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "", "", false
	}
	verb, prefix := strings.ToLower(fields[0]), strings.TrimSpace(fields[1])
	switch verb {
	case "approve", "批准":
		return approval.ApprovalApproved, prefix, true
	case "reject", "拒绝":
		return approval.ApprovalDenied, prefix, true
	default:
		return "", "", false
	}
}

// shortDigest renders the readable prefix of one argument digest.
func shortDigest(digest string) string {
	if len(digest) > 8 {
		return digest[:8]
	}
	return digest
}

const (
	// restartDoneFreshWindow 是新鲜度窗：标记文件新于此窗内的启动才算转世，否则按冷启动处理。
	restartDoneFreshWindow = 10 * time.Minute

	// walTailEventLimit 与 walTailSummaryMaxChars 是通报的令牌预算：尾现场块只取最新的有限条、
	// 每条摘要截断，绝不带事件全文——通报要进上下文，不是日志。
	walTailEventLimit      = 12
	walTailSummaryMaxChars = 160

	// noticeConsumedSuffix 是消费标记的重命名后缀。
	noticeConsumedSuffix = ".notified"

	// systemAlertConsumedSuffix 是系统告警消费标记的重命名后缀。
	systemAlertConsumedSuffix = ".consumed"
)

// inboxInjector 是两条启动通知共用到的最小接口面：把一条通报交给宿主的内部收件箱。
type inboxInjector interface {
	InjectInternal(addr event.Address, source string, msg tagentmodel.Message)
}

// detectReincarnation 判定本次启动是否为转世：只看换装标记是否存在且足够新鲜。标记由保险链在
// 派生新进程之前写好，故无竞态；冷启动从不产生标记，故不会假阳性。
func detectReincarnation(noticePath string, now time.Time) bool {
	st, err := os.Stat(noticePath)
	if err != nil {
		return false
	}
	return now.Sub(st.ModTime()) <= restartDoneFreshWindow
}

// readNoticeMetadata 解析标记的键值档案；缺失或读不到返回 nil，由通报正文显式标注降级（不静默）。
func readNoticeMetadata(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	meta := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, ":"); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			if k != "" {
				meta[k] = v
			}
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// tailQuerier 只声明取尾事件真正用到的能力：完整存储与测试替身都无需实现一堆无关方法即可满足。
type tailQuerier interface {
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
}

// fetchWALTail 按时间倒序取该 agent 最新的有限条事件；查询错误一律上抛以走显式降级。
func fetchWALTail(ctx context.Context, store tailQuerier, agentName string, limit int) ([]memory.EventReference, error) {
	if store == nil {
		return nil, fmt.Errorf("memstore unavailable")
	}
	pid := memory.PartitionIDFromName(agentName)
	return store.QueryEvents(ctx, memory.QueryOptions{
		PartitionIDs: []int{pid},
		Limit:        limit,
		OrderBy:      memory.QueryOrderByDesc,
	})
}

// hasOpenBreakpoint 判断上一世是否中断于回合中途：事件按新到旧取回，最新一条即最后动作。
func hasOpenBreakpoint(refs []memory.EventReference) bool {
	if len(refs) == 0 {
		return false
	}
	switch refs[0].Type {
	case "thinking_plan", "action_command":
		return true
	default:
		return false
	}
}

// buildNoticeText 组装通报三段：元数据、尾现场块、断点判定。降级阶梯每一级都必须在正文里
// 显式写出，不得用一份看起来完整的通报掩盖数据缺失。
func buildNoticeText(meta map[string]string, refs []memory.EventReference, walErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[转世通报] 检测到保险链自替换换装（D1 命中：REINCARNATION_NOTICE 新鲜；PID 详情见元数据段）\n\n")

	b.WriteString("== 元数据 ==\n")
	if meta == nil {
		b.WriteString("（NOTICE 档案缺失——降级：检测依据 NOTICE 自身 mtime，详情查 logs/restart.log）\n")
	} else {
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s: %s\n", k, meta[k])
		}
	}

	b.WriteString("\n== WAL 尾现场块（最近事件，时间倒序）==\n")
	switch {
	case walErr != nil:
		fmt.Fprintf(&b, "（现场块不可用：%v——降级，禁静默）\n", walErr)
	case len(refs) == 0:
		b.WriteString("（事件存储为空或查询无结果——降级，禁静默）\n")
	default:
		for _, r := range refs {
			summary := r.Summary
			if len(summary) > walTailSummaryMaxChars {
				summary = summary[:walTailSummaryMaxChars] + "…"
			}
			summary = strings.ReplaceAll(summary, "\n", " ")
			fmt.Fprintf(&b, "- [evt_%s|%s] %s (%s)\n",
				r.Key, r.Type, summary, time.UnixMilli(r.Timestamp).Format("15:04:05"))
		}
		if hasOpenBreakpoint(refs) {
			b.WriteString("\n断点判定：最后事件止于回合中途且无收尾 agent_output → 上一世最后一回合**中断于此**；\n续作前先核验该时刻的承诺是否已兑现（对照 WAL 与系统实态），勿凭通报默认成功。\n")
		} else {
			b.WriteString("\n断点判定：最后事件为收尾输出 → 上一世回合已闭环，无未兑现承诺迹象。\n")
		}
	}
	return b.String()
}

// noticeWaitMax 与 noticePollStep 是等待标记出现的预算：必须轮询而非固定 sleep 一次——
// 单次检查会静默错过慢写入者，错过一次等于整世失忆且无人报错。
const (
	noticeWaitMax  = 60 * time.Second
	noticePollStep = 500 * time.Millisecond
)

// waitNoticeAppearance 轮询标记直至出现或预算用尽，返回是否见到（新鲜度由调用方判）。
func waitNoticeAppearance(noticePath string, maxWait, poll time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for {
		if _, err := os.Stat(noticePath); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(poll)
	}
}

// maybeInjectReincarnationNotice 是一次性启动钩子：轮询标记、判新鲜、组装通报、注入、记消费。
// tail 为事件尾查询面；nil 表示本进程未接事件存储，通报正文显式写出该降级。
// 每一步都记日志，任何一步都不允许让机器人起不来。
func maybeInjectReincarnationNotice(inj inboxInjector, tail tailQuerier, agentName, runDir string, noticeWait time.Duration) {
	if !filepath.IsAbs(runDir) {
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	noticePath := filepath.Join(runDir, "REINCARNATION_NOTICE")

	if !waitNoticeAppearance(noticePath, noticeWait, noticePollStep) {
		return
	}
	if !detectReincarnation(noticePath, time.Now()) {
		return
	}
	log.Infof("[reincarnation] D1 hit: fresh REINCARNATION_NOTICE at %s", noticePath)

	meta := readNoticeMetadata(noticePath)
	if meta == nil {
		log.Warnf("[reincarnation] NOTICE archive missing at %s — degraded metadata", noticePath)
	}
	refs, walErr := fetchWALTail(context.Background(), tail, agentName, walTailEventLimit)
	if walErr != nil {
		log.Warnf("[reincarnation] WAL tail query failed: %v — degraded scene block", walErr)
	}

	text := buildNoticeText(meta, refs, walErr)
	inj.InjectInternal(event.Address{}, "reincarnation", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	})
	log.Infof("[reincarnation] notice injected via reincarnation source (%d WAL events, meta=%v)", len(refs), meta != nil)

	if err := os.Rename(noticePath, noticePath+noticeConsumedSuffix); err != nil {
		log.Warnf("[reincarnation] consume-marker rename failed (re-notice on next boot): %v", err)
	} else {
		log.Infof("[reincarnation] NOTICE renamed → consumed marker")
	}
}

// maybeConsumeSystemAlert is the one-shot startup hook: wait for the runtime to
// settle, queue any pending restart failure for the first user turn, mark
// consumed. Every step is logged; nothing here may crash the bot.
func maybeConsumeSystemAlert(inj inboxInjector, runDir string, delay time.Duration) {
	if !filepath.IsAbs(runDir) {
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	alertPath := filepath.Join(runDir, "SYSTEM_ALERT")

	time.Sleep(delay)

	data, err := os.ReadFile(alertPath)
	if err != nil {
		return
	}

	text := fmt.Sprintf("[System Alert] Detected an unprocessed failure record for the previous generation's restart script (run/SYSTEM_ALERT):\n%s",
		string(data))
	inj.InjectInternal(event.Address{}, "system_alert", tagentmodel.Message{
		Role:    tagentmodel.RoleUser,
		Content: text,
	})
	log.Infof("[system-alert] staged failure queued via system_alert source (%d bytes)", len(data))

	if err := os.Rename(alertPath, alertPath+systemAlertConsumedSuffix); err != nil {
		log.Warnf("[system-alert] consume-marker rename failed: %v", err)
	} else {
		log.Infof("[system-alert] SYSTEM_ALERT renamed -> consumed marker")
	}
}

const (
	facePortEnv     = "TAGENT_HTTP_PORT"
	faceDefaultPort = "8089"
	faceTokenEnv    = "TAGENT_RL_AUTH_TOKEN"
)

const (
	faceBodyLimitByte = 1 << 20
	faceShutdownGrace = 5 * time.Second
)

// ErrFaceUnauthenticated reports an injection request whose subject did not
// authenticate: no credential is configured for the face and the peer is not the
// loopback address, or a configured credential was missing or unmatched.
var ErrFaceUnauthenticated = errors.New("wechat-bot: the injection request is not authenticated")

// ErrFaceNoActiveChat reports an injection request arriving beside no chat
// delivery: an injection carries no chat identity of its own, so it is admitted at
// the chat that last delivered.
var ErrFaceNoActiveChat = errors.New("wechat-bot: no chat delivered yet, so the injection has no session to drive")

// ErrFaceEmptyInjection reports an injection request whose messages carry no user
// content: a turn reads user input only, so the request is refused by name.
var ErrFaceEmptyInjection = errors.New("wechat-bot: the injection carries no user message")

// faceDrive is the narrow host face the endpoints drive: one delivery admitted at
// the chat that last delivered, and that chat's identity.
type faceDrive interface {
	Drive(ctx context.Context, in inboundMessage) (event.Address, error)
	mostRecentChat() string
}

// taskInjection is the body an inbound channel posts to the injection endpoint:
// the messages the turn reads and the lineage the channel declares for it.
type taskInjection struct {
	Messages      []injectionMessage `json:"messages"`
	TriggerSource string             `json:"trigger_source"`
}

// injectionMessage is one message of an injection: the role it speaks for and its content.
type injectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// userText reports the user content the injection carries: the last non-empty
// user message is the one a turn reads, and an injection beside one answers no text.
func (in taskInjection) userText() string {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		msg := in.Messages[i]
		if msg.Role != string(tagentmodel.RoleUser) {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return msg.Content
		}
	}
	return ""
}

// taskFace serves the host's two HTTP endpoints: the liveness probe a supervisor
// polls and the injection endpoint an inbound channel posts a task to. The face
// drives the run through the narrow face it holds and adds no outbound path of
// its own.
type taskFace struct {
	ctx   context.Context
	drive faceDrive
	token string
}

// newTaskFace returns the host face over one drive face beside the request
// credential it accepts; an empty credential leaves the loopback peer as the only
// admitted subject.
func newTaskFace(ctx context.Context, drive faceDrive, token string) *taskFace {
	return &taskFace{ctx: ctx, drive: drive, token: strings.TrimSpace(token)}
}

// Handler returns the endpoints of the face: GET /healthz answers the liveness
// probe every supervisor polls, and POST /task admits one injection and answers
// 202 before the turn runs.
func (f *taskFace) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /task", f.inject)
	return mux
}

// inject admits one injection and answers before its turn runs: the body's user
// content is driven at the chat that last delivered, in the process context so a
// shutdown still reaches the turn. A refused subject answers 401, a body beside no
// user content 400, and a face beside no delivered chat 503.
func (f *taskFace) inject(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, faceBodyLimitByte))
	if err != nil {
		http.Error(w, fmt.Sprintf("read the injection body: %v", err), http.StatusBadRequest)
		return
	}
	var injection taskInjection
	if err := json.Unmarshal(body, &injection); err != nil {
		http.Error(w, fmt.Sprintf("decode the injection body: %v", err), http.StatusBadRequest)
		return
	}
	text := injection.userText()
	if text == "" {
		http.Error(w, ErrFaceEmptyInjection.Error(), http.StatusBadRequest)
		return
	}
	chat := f.drive.mostRecentChat()
	if chat == "" {
		http.Error(w, ErrFaceNoActiveChat.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		if _, err := f.drive.Drive(f.ctx, inboundMessage{ChatID: chat, UserName: chat, Text: text}); err != nil {
			log.Warnf("[host] injected task failed (chat=%s): %v", chat, err)
		}
	}()
}

// authorize judges one injection request: a configured credential must arrive as a
// bearer token and match in constant time, and a face beside no credential admits
// the loopback peer only.
func (f *taskFace) authorize(r *http.Request) error {
	if f.token != "" {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return ErrFaceUnauthenticated
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(f.token)) != 1 {
			return ErrFaceUnauthenticated
		}
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ErrFaceUnauthenticated
	}
	if !net.ParseIP(host).IsLoopback() {
		return ErrFaceUnauthenticated
	}
	return nil
}

// Listen opens the endpoint address of the face: the loopback interface on the
// port the host environment names, and the interface default beside no port.
func (f *taskFace) Listen() (net.Listener, error) {
	return net.Listen("tcp", faceAddr(os.Getenv(facePortEnv)))
}

// Serve answers the endpoints on the listener until the process context ends, then
// stops accepting and waits for the in-flight handlers.
func (f *taskFace) Serve(listener net.Listener) error {
	server := &http.Server{Handler: f.Handler()}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-f.ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), faceShutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	<-served
	return nil
}

// faceAddr reports the listen address of the face: the loopback interface on the
// named port, and on the interface default when the value names no port number.
func faceAddr(port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		port = faceDefaultPort
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// newWechatBot builds the channel instance of this process: the file cursor and
// token stores live beside the configured config directory.
func newWechatBot(cfg WechatAppConfig) *wechat.Bot {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cursorStore := wechat.NewFileCursorStore(filepath.Join(cfg.ConfigDir, "cursor.txt"))
	return wechat.NewBot(
		wechat.WithLogger(logger),
		wechat.WithCursorStore(cursorStore),
		wechat.WithTokenFile(cfg.TokenFile),
	)
}

// login drives the login flow and reports the QR code the user has to scan.
func login(ctx context.Context, bot *wechat.Bot) error {
	fmt.Println("Logging in to WeChat...")
	if err := bot.Login(ctx, func(qrCode string) {
		fmt.Println("\nPlease scan the QR code with WeChat:")
		fmt.Println("----------------------------------------")
		if strings.HasPrefix(qrCode, "http") {
			fmt.Println(qrCode)
		} else {
			fmt.Println("[QR code image content]")
		}
		fmt.Println("----------------------------------------")
	}); err != nil {
		return err
	}
	fmt.Println("Login successful!")
	return nil
}

// bindChannel binds the one outbound path of this process: passive turn
// delivery, the send tool, files and long text all leave through it.
func bindChannel(host *hostRuntime, bot wechatChannel, cfg WechatAppConfig) *sync.Map {
	typingActive := &sync.Map{}
	// The context-token long-text path is a property of the real channel; a
	// stand-in channel records the truncation fallback instead.
	var longText longTextFunc
	if concrete, ok := bot.(*wechat.Bot); ok {
		longText = longTextViaContextToken(concrete)
	}
	host.sender = &hostSender{
		bot:          bot,
		longText:     longText,
		typingActive: typingActive,
		router:       host.router,
		inj:          host.inbox,
		workspaceDir: cfg.WorkspaceDir,
	}
	return typingActive
}

// registerHandlers registers the inbound hook of the channel: approval rulings
// first, then dedup, classification, media intake and the drive.
func registerHandlers(host *hostRuntime, bot wechatChannel, gateway *approvalGateway, cfg WechatAppConfig, typingActive *sync.Map) {
	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		if handled, reply, _ := gateway.Resolve(msg.FromUserID, msg.Text()); handled {
			if reply != "" {
				_ = bot.SendTextToUser(ctx, msg.FromUserID, reply)
			}
			return nil
		}
		if err := host.Admit(DedupKey(msg)); err != nil {
			log.Warnf("[Dedup] duplicate message dropped (chat=%s)", msg.FromUserID)
			return nil
		}

		kind := ClassifyInbound(msg, cfg.WorkspaceDir != "")
		if kind == InboundIgnore {
			return nil
		}

		go func() {
			_ = bot.SendTyping(ctx, msg.FromUserID)
			typingActive.Store(msg.FromUserID, time.Now())

			injectText := msg.Text()
			now := time.Now()

			switch kind {
			case InboundMedia:
				if cfg.WorkspaceDir == "" {
					_ = bot.SendTextToUser(ctx, msg.FromUserID, "暂不支持附件接收（未配置 workspace）")
				} else {
					outcome := IntakeMedia(ctx, bot, bot.CDNBaseURL(), cfg.WorkspaceDir, msg.FromUserID, msg, now)
					for _, reason := range outcome.Rejects {
						_ = bot.SendTextToUser(ctx, msg.FromUserID, reason)
					}
					injectText = ComposeMediaInject(outcome, msg.Text(), cfg.WorkspaceDir, msg.FromUserID, now)
				}
			case InboundLongText:
				if _, inject, err := SaveLongText(cfg.WorkspaceDir, msg.FromUserID, injectText, "input", now); err == nil {
					injectText = inject
				} else {
					log.Errorf("[Intake] 长文本落盘失败 (chat=%s): %v", msg.FromUserID, err)
				}
			}

			if strings.TrimSpace(injectText) == "" {
				_ = bot.StopTyping(ctx, msg.FromUserID)
				typingActive.Delete(msg.FromUserID)
				return
			}

			if _, err := host.Drive(ctx, inboundMessage{
				ChatID:   msg.FromUserID,
				UserName: msg.FromUserID,
				Text:     injectText,
			}); err != nil {
				log.Errorf("[host] drive failed (chat=%s): %v", msg.FromUserID, err)
			}
			_ = bot.StopTyping(ctx, msg.FromUserID)
			typingActive.Delete(msg.FromUserID)
		}()

		return nil
	})
}

// serveFace opens the HTTP face beside the running bot; a port that cannot be
// bound leaves the probes unavailable without stopping the channel.
func serveFace(ctx context.Context, host *hostRuntime) {
	face := newTaskFace(ctx, host, os.Getenv(faceTokenEnv))
	listener, err := face.Listen()
	if err != nil {
		log.Errorf("[host] HTTP 面未装配，探针与注入端点均不可用: %v", err)
		return
	}
	fmt.Printf("  HTTP:        http://%s (/healthz, /task)\n", listener.Addr())
	go func() {
		if err := face.Serve(listener); err != nil && !errors.Is(err, context.Canceled) {
			log.Warnf("[host] HTTP 面结束: %v", err)
		}
	}()
}

// runApprovalCLI resolves one ruling typed on the command line, before the bot
// logs in. The second return value is the process exit code.
func runApprovalCLI(gateway *approvalGateway, args []string) (bool, int) {
	if len(args) < 2 || (args[0] != "approve" && args[0] != "reject") {
		return false, 0
	}
	_, reply, err := gateway.Resolve("cli", args[0]+" "+args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "审批失败: %v\n", err)
		return true, 1
	}
	fmt.Println(reply)
	return true, 0
}
