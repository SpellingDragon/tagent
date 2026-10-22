//go:build smoke

// smoke_harness_test.go 是本地长程冒烟的台架：进程内装配真实的微信宿主
// （hostRuntime / SessionRouter / SeenStore / internalInbox / approvalGateway /
// hostSender / registerHandlers 全是生产实例），模型换成本地 scripted HTTP，
// 微信出站换成记录全部外发的替身通道，宿主只经本地 HTTP 面驱动。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	// smokeApprovalMarker 是获批 send 调用投递的正文标记。
	smokeApprovalMarker = "SMOKE-APPROVED-BODY"
	// smokeApproverChat 是审批人会话标识（app.wechat.approvers 白名单成员）。
	smokeApproverChat = "smoke-approver"
	// smokeSessionTag 是入口 agent 与内存命名空间。
	smokeSessionTag = "smoke"
	// smokeEntryModel 是首版文档声明的入口模型。
	smokeEntryModel = "smoke-entry"
	// smokeReloadModel 是热换后文档声明的入口模型。
	smokeReloadModel = "smoke-entry-b"
	// smokeWorkerModel 是委派子 agent 的模型。
	smokeWorkerModel = "smoke-worker"
	// smokeReflectModel 是冥想 agent 的模型。
	smokeReflectModel = "smoke-reflect"

	// smokeEarlyPrefix 是每会话首轮用户文本的早期标记前缀。
	smokeEarlyPrefix = "EARLY-MARKER"
	// smokeReflectionMarker 是冥想摘要正文的标记。
	smokeReflectionMarker = "SMOKE-REFLECTION"
	// smokeInboxMarker 是内部通知正文的标记。
	smokeInboxMarker = "SMOKE-INBOX-NOTICE"
	// smokeFileMarker 是路径回复轮投递的文件名。
	smokeFileMarker = "smoke-report.txt"

	// smokeLongBytes 是长文轮脚本正文的字节量（>2000 触发截断降级）。
	smokeLongBytes = 3200
)

var smokeChats = []string{"smoke-chat-a", "smoke-chat-b", "smoke-chat-c"}

// smokeMarkerRE 从用户文本里取出本轮的轮次标记。
var smokeMarkerRE = regexp.MustCompile(`SMOKE-R[0-9]+`)

// smokeMarkerOf 报告文本里第一个轮次标记；没有标记的文本（内部通知、结算
// 通知）沿用它携带的那一个标记，仍然取不到时回报零值标记。
func smokeMarkerOf(text string) string {
	if marker := smokeMarkerRE.FindString(text); marker != "" {
		return marker
	}
	return "SMOKE-R0"
}

// smokeConfig 是一次冒烟的全部临时路径与文档形状。
type smokeConfig struct {
	Root         string
	WorkspaceDir string
	ConfigDir    string
	ContextDir   string
	ApprovalDir  string
	PromptDir    string
	StorageRoot  string
	ConfigPath   string
	Endpoint     string
	Model        string
	// Doc 覆盖默认文档渲染；第二波用它声明 MCP、技能根与记忆引擎。
	Doc func(*smokeConfig) string
}

// newSmokeConfig 建临时根、写冥想提示词与工作区文件，并写出首版宿主文档。
func newSmokeConfig(t *testing.T, endpoint string) *smokeConfig {
	t.Helper()
	root := t.TempDir()
	cfg := &smokeConfig{
		Root:         root,
		WorkspaceDir: filepath.Join(root, "workspace"),
		ConfigDir:    filepath.Join(root, "wechat-config"),
		ContextDir:   filepath.Join(root, "wechat-context"),
		ApprovalDir:  filepath.Join(root, "approvals"),
		PromptDir:    filepath.Join(root, "prompts"),
		StorageRoot:  filepath.Join(root, "storage"),
		ConfigPath:   filepath.Join(root, "tagent.yaml"),
		Endpoint:     endpoint,
		Model:        smokeEntryModel,
	}
	for _, dir := range []string{
		cfg.WorkspaceDir, cfg.ConfigDir, cfg.ContextDir,
		cfg.ApprovalDir, cfg.PromptDir, cfg.StorageRoot,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create the smoke directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.PromptDir, "smoke-meditation.md"),
		[]byte("Reflect over the observed smoke session and deliver one summary.\n"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.WorkspaceDir, smokeFileMarker),
		[]byte("smoke report body\n"), 0o644); err != nil {
		t.Fatalf("write the deliverable file: %v", err)
	}
	cfg.write(t)
	return cfg
}

// write 把当前模型名渲染成宿主文档并落盘（热换前的那一步）。
func (c *smokeConfig) write(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(c.ConfigPath, []byte(c.document()), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
}

// document 渲染宿主文档：入口 agent 小窗口高频折叠、多 agent、审批开启。
func (c *smokeConfig) document() string {
	if c.Doc != nil {
		return c.Doc(c)
	}
	return fmt.Sprintf(`entry: %s
provider: local
model: %s
prompt_dir: %s
request_timeout_seconds: 60
providers:
  local: { provider: openai, api_endpoint: "%s", api_key_env: SMOKE_API_KEY }
agents:
  %s:
    model: %s
    tools:
      - { kind: tool, id: send }
      - { kind: agent, agent: smoke-worker }
    max_tokens: 2000
    compress_threshold: 0.3
    keep_recent_tasks: 1
    max_tool_iterations: 3
  smoke-worker:
    model: smoke-worker
  smoke-reflect:
    model: smoke-reflect
    tools:
      - { kind: tool, id: deliver_summary }
    memory:
      read_namespaces: [%s]
    meditation:
      enabled: true
      interval: 5s
      min_gap: 10s
      prompt_file: smoke-meditation.md
      observed_namespaces: [%s]
      deliver_to: [%s]
storage: { type: localfile, root: %s }
governance:
  enabled: true
  enforcement: normal
  dir: %s
  approval_ttl_minutes: 30
app:
  wechat:
    config_dir: %s
    token_file: %s
    context_token_dir: %s
    workspace_dir: %s
    approvers: [%s]
`,
		smokeSessionTag, c.Model, c.PromptDir, c.Endpoint,
		smokeSessionTag, c.Model,
		smokeSessionTag, smokeSessionTag, smokeSessionTag,
		c.StorageRoot, c.ApprovalDir,
		c.ConfigDir, filepath.Join(c.ConfigDir, "token.json"), c.ContextDir,
		c.WorkspaceDir, smokeApproverChat,
	)
}

// smokeModelRequest 是脚本模型收到的一次请求：模型名与它读到的消息。
type smokeModelRequest struct {
	Model    string
	Messages []smokeModelMessage
}

// smokeModelMessage 是请求里的一条消息，解码到断言需要的字段。
type smokeModelMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// smokeProvider 是本地 scripted OpenAI 端点：按模型名与请求内容决定回答，
// 每个映射的会话与轮次因此都拿到确定的混合响应。
type smokeProvider struct {
	server *httptest.Server
	file   string

	// hook 让某一个测试脚本化固定日程之外的响应形状（第二波）：请求已记录后
	// 调用，返回 true 表示钩子已经写出响应。
	hook func(w http.ResponseWriter, index int, modelName string, messages []smokeModelMessage) bool

	mu       sync.Mutex
	requests []smokeModelRequest
}

// newSmokeProvider 起端点并随测试关闭。
func newSmokeProvider(t *testing.T, file string) *smokeProvider {
	t.Helper()
	provider := &smokeProvider{file: file}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// endpoint 报告文档里 provider 条目命中的基地址。
func (p *smokeProvider) endpoint() string { return p.server.URL + "/v1" }

// serve 回答一次 chat completion，并把请求原样记下。
func (p *smokeProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	var modelName string
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	var messages []smokeModelMessage
	if raw, ok := body["messages"]; ok {
		_ = json.Unmarshal(raw, &messages)
	}
	p.mu.Lock()
	index := len(p.requests)
	p.requests = append(p.requests, smokeModelRequest{Model: modelName, Messages: messages})
	hook := p.hook
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if hook != nil && hook(w, index, modelName, messages) {
		return
	}
	fmt.Fprint(w, p.answerFor(modelName, messages, index))
}

// answerFor 是本台架的响应脚本：委派子 agent 回结果正文、冥想 agent 投递摘要、
// 入口 agent 先落工具调用再按回执/观察/结算通知收尾。
// - an entry turn that ended with a tool call answers from the receipt or observation alone, never with a second call
func (p *smokeProvider) answerFor(modelName string, messages []smokeModelMessage, index int) string {
	last := smokeLastUserText(messages)
	switch modelName {
	case smokeWorkerModel:
		return smokeAnswerBody("worker result " + smokeMarkerOf(last))
	case smokeReflectModel:
		if smokeEndedWithTool(messages) {
			return smokeAnswerBody("reflection recorded")
		}
		return smokeToolCallBody(fmt.Sprintf("call-reflect-%d", index), "deliver_summary",
			`{"content":"`+smokeReflectionMarker+" "+strconv.Itoa(index)+`"}`)
	}
	marker := smokeMarkerIn(messages)
	if smokeEndedWithTool(messages) {
		end := smokeLastContent(messages)
		switch {
		case strings.Contains(end, "parked awaiting approval"):
			return smokeAnswerBody("hold-ack " + marker)
		case strings.Contains(end, "accepted task"):
			return smokeAnswerBody("delegate ack " + marker)
		case strings.Contains(end, "[send_ok]"):
			return smokeAnswerBody("approved-done " + marker)
		case strings.Contains(end, "call-worker"):
			return smokeAnswerBody("delegate ack " + marker)
		case strings.Contains(end, "call-hold"):
			return smokeAnswerBody("hold-ack " + marker)
		case strings.Contains(end, "call-retry"):
			return smokeAnswerBody("approved-done " + marker)
		case strings.Contains(end, "deliver_summary"):
			return smokeAnswerBody("reflection recorded")
		}
	}
	switch {
	case strings.Contains(last, "tagent: task") && strings.Contains(last, "settled"):
		return smokeAnswerBody("settle-ack " + marker)
	case strings.Contains(last, "smoke retry"):
		return smokeToolCallBody("call-retry-"+marker, sendToolID, smokeSendArgs(marker))
	case strings.Contains(last, "smoke approval"):
		return smokeToolCallBody("call-hold-"+marker, sendToolID, smokeSendArgs(marker))
	case strings.Contains(last, "smoke delegate"):
		return smokeToolCallBody("call-worker-"+marker, smokeWorkerModel,
			`{"input":"produce `+marker+`"}`)
	case strings.Contains(last, "smoke file"):
		return smokeAnswerBody("here is the report for " + marker + ": " + p.file)
	case strings.Contains(last, "smoke long"):
		return smokeAnswerBody(smokeLongBody(marker))
	}
	return smokeAnswerBody("answer " + marker)
}

// count 报告端点已经回答的请求数。
func (p *smokeProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// recorded 报告端点回答过的请求，按到达顺序。
func (p *smokeProvider) recorded() []smokeModelRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]smokeModelRequest(nil), p.requests...)
}

// text 把一次请求读到的全部正文拼起来，供"模型确实看到了什么"的断言。
func (r smokeModelRequest) text() string {
	parts := make([]string, 0, len(r.Messages))
	for _, msg := range r.Messages {
		if strings.TrimSpace(msg.Content) != "" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// smokeLastUserText 报告最后一条用户消息的正文。
func smokeLastUserText(messages []smokeModelMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == string(tagentmodel.RoleUser) {
			return messages[i].Content
		}
	}
	return ""
}

// smokeAfterLastUser 报告最后一条用户消息之后的消息：本次决策的回执与观察。
func smokeAfterLastUser(messages []smokeModelMessage) []smokeModelMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == string(tagentmodel.RoleUser) {
			return messages[i+1:]
		}
	}
	return nil
}

// smokeLastContent 报告请求里最后一条消息的正文。
func smokeLastContent(messages []smokeModelMessage) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Content
}

// smokeEndedWithTool 报告本次决策的最后一条消息是否已是工具交代：工具角色的
// 结果消息，或循环折成用户角色的两种帧——以调用 id 开头的回执帧
// （{call-... task-N true/false ...}）与工具结果帧（[send_ok] / [deliver_summary_ok]）。
// 判"本轮是否已有交代"必须看最后一条消息的角色：帧本身就是用户角色，只看最后
// 一条用户消息之后的尾巴会漏掉它。反思输入以 "[reflection]" 开头，所以帧前缀
// 只认具体的结果标记，不能按 "[" 一刀切。
func smokeEndedWithTool(messages []smokeModelMessage) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	if last.Role != string(tagentmodel.RoleUser) {
		return true
	}
	content := strings.TrimSpace(last.Content)
	return strings.HasPrefix(content, "{") && strings.Contains(content, "call-") ||
		strings.HasPrefix(content, "[send_") || strings.HasPrefix(content, "[deliver_summary_")
}

// smokeFrame 报告用户消息是不是循环折成的工具结果帧。
func smokeFrame(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, "{") && strings.Contains(content, "call-") ||
		strings.HasPrefix(content, "[send_") || strings.HasPrefix(content, "[deliver_summary_")
}

// smokeMarkerIn 报告请求里最近一条非帧用户消息携带的冒烟标记：工具结果帧
// （回执帧、[send_ok]）自身不带标记，标记在它之前那条用户消息里。
func smokeMarkerIn(messages []smokeModelMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != string(tagentmodel.RoleUser) {
			continue
		}
		if smokeFrame(msg.Content) {
			continue
		}
		if marker := smokeMarkerRE.FindString(msg.Content); marker != "" {
			return marker
		}
	}
	return "SMOKE-R0"
}

// smokeSendArgs 渲染审批轮 send 调用的参数（批准前后必须逐字节一致）。
func smokeSendArgs(marker string) string {
	return `{"content":"` + smokeApprovalMarker + " " + marker + `"}`
}

// smokeLongBody 渲染超阈值的长文正文，长文降级由 sender 的截断回退承载。
func smokeLongBody(marker string) string {
	pad := strings.Repeat("smoke-long-body ", smokeLongBytes/len("smoke-long-body "))
	return "LONG " + marker + " " + pad
}

// smokeAnswerBody 渲染一条无工具调用的终局回答。
func smokeAnswerBody(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("smoke: encode the scripted answer: " + err.Error())
	}
	return `{"id":"smoke","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` +
		string(encoded) + `},"finish_reason":"stop"}]}`
}

// smokeToolCallBody 渲染一次工具调用决策。
func smokeToolCallBody(id, name, args string) string {
	encode := func(s string) string {
		raw, err := json.Marshal(s)
		if err != nil {
			panic("smoke: encode the scripted tool call: " + err.Error())
		}
		return string(raw)
	}
	return `{"id":"smoke","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":` + encode(id) + `,"type":"function","function":{"name":` + encode(name) +
		`,"arguments":` + encode(args) + `}}]},"finish_reason":"tool_calls"}]}`
}

// smokeGovernor 是审批接缝：生产装配里没有任何风险面给 pass.Risk 定级，默认档
// 因而永不 Hold。台架只把 send 的等级抬到 critical，判定仍由同一份治理策略
// （governance.Open + WithApprovals）给出；Hold 时补上生产缺失的那一步——
// 把请求登记进审批管理器，审批面才有话可下发。
type smokeGovernor struct {
	inner   plugin.Governor
	manager *approval.ApprovalManager
}

// Judge 实现 plugin.Governor。
func (g smokeGovernor) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	escalated := false
	if pass.Final.Function.Name == sendToolID {
		pass.Risk = plugin.RiskCritical
		escalated = true
	}
	decision, err := g.inner.Judge(pass)
	if err != nil || !escalated {
		return decision, err
	}
	if decision.Disposition == plugin.DispositionHold {
		id := task.NewApprovalID()
		if _, reqErr := g.manager.Request(context.Background(), id, pass.Final.Function.Name,
			[]byte(pass.Final.Function.Arguments), plugin.RiskCritical, decision.Rule, decision.Reason); reqErr != nil {
			return decision, reqErr
		}
	}
	return decision, nil
}

// smokeRig 是一台已装配的微信宿主：真实应用侧组件 + 真实 Host + 替身通道。
type smokeRig struct {
	t        *testing.T
	cfg      *smokeConfig
	provider *smokeProvider
	channel  *fakeChannel
	gateway  *approvalGateway
	app      *wechatApp
	host     *tagent.Host
	lastChat string

	closed   bool
	closeErr error
}

// bootSmokeRig 装配一台宿主：newWechatApp 的组件、Boot 的真实运行时，
// 以及 bindWechat 的接线（去掉登录、轮询与启动通知）。
// - the invoke face is the production bindWechat wiring minus real login, Poller and the one-shot startup notice
func bootSmokeRig(t *testing.T, cfg *smokeConfig, provider *smokeProvider) *smokeRig {
	return bootSmokeRigWith(t, cfg, provider, nil)
}

// bootSmokeRigWith 装配一台宿主，并允许调用方注入自己的审批网关（第二波的
// TTL 场景要一个可控时钟）。传 nil 时与 bootSmokeRig 完全一致。
func bootSmokeRigWith(t *testing.T, cfg *smokeConfig, provider *smokeProvider, gateway *approvalGateway) *smokeRig {
	t.Helper()
	t.Setenv("SMOKE_API_KEY", "smoke-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Setenv("TAGENT_SESSION_ID", "smoke-run")
	t.Setenv("TAGENT_LOG_LEVEL", "debug")

	appCfg, err := loadWechatConfig(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("load the wechat section: %v", err)
	}
	if gateway == nil {
		gateway = newApprovalGateway(appCfg.Governance, appCfg.App.Wechat.Approvers)
	}
	app, err := newWechatApp(appCfg, gateway)
	if err != nil {
		t.Fatalf("assemble the wechat application: %v", err)
	}
	channel := newFakeChannel()
	policy := governance.Open(governance.WithApprovals(gateway.Manager()))
	options := append(app.options(),
		tagent.WithPolicy(smokeGovernor{inner: policy, manager: gateway.Manager()}))
	host, err := tagent.Boot(cfg.ConfigPath, options...)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	app.host.invoke = func(ctx context.Context, req tagent.InvokeRequest) (bool, error) {
		turn, err := host.Invoke(ctx, req)
		if err != nil {
			return false, err
		}
		return turn.Terminal == tagentmodel.KindFailed, nil
	}
	app.face = host.FaceAddr()
	gateway.SetSender(channel)
	app.typing = bindChannel(app.host, channel, appCfg.App.Wechat)
	registerHandlers(app.host, channel, gateway, appCfg.App.Wechat, app.typing)
	if app.face == "" {
		t.Fatal("the host face listens on no address")
	}
	rig := &smokeRig{t: t, cfg: cfg, provider: provider, channel: channel,
		gateway: gateway, app: app, host: host}
	t.Cleanup(rig.shutdown)
	return rig
}

// shutdown 关闭宿主一次；重复调用是安全的空操作。
func (r *smokeRig) shutdown() {
	if r.closed {
		return
	}
	r.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r.closeErr = r.host.Close(ctx)
}

// close 显式关闭宿主（重启/收尾），并留下关闭结果供缺陷记录。
func (r *smokeRig) close() error {
	r.shutdown()
	return r.closeErr
}

// reboot 关闭宿主后在同一进程里重建一台：应用侧组件与 Boot 运行时都重新装配，
// 通道替身沿用同一个，所以外发记录与断言上下文跨重启连续。
func (r *smokeRig) reboot() {
	r.t.Helper()
	appCfg, err := loadWechatConfig(r.cfg.ConfigPath)
	if err != nil {
		r.t.Fatalf("reload the wechat section on reboot: %v", err)
	}
	gateway := newApprovalGateway(appCfg.Governance, appCfg.App.Wechat.Approvers)
	app, err := newWechatApp(appCfg, gateway)
	if err != nil {
		r.t.Fatalf("assemble the wechat application on reboot: %v", err)
	}
	policy := governance.Open(governance.WithApprovals(gateway.Manager()))
	options := append(app.options(),
		tagent.WithPolicy(smokeGovernor{inner: policy, manager: gateway.Manager()}))
	host, err := tagent.Boot(r.cfg.ConfigPath, options...)
	if err != nil {
		r.t.Fatalf("boot the host on reboot: %v", err)
	}
	app.host.invoke = func(ctx context.Context, req tagent.InvokeRequest) (bool, error) {
		turn, err := host.Invoke(ctx, req)
		if err != nil {
			return false, err
		}
		return turn.Terminal == tagentmodel.KindFailed, nil
	}
	app.face = host.FaceAddr()
	gateway.SetSender(r.channel)
	app.typing = bindChannel(app.host, r.channel, appCfg.App.Wechat)
	registerHandlers(app.host, r.channel, gateway, appCfg.App.Wechat, app.typing)
	if app.face == "" {
		r.t.Fatal("the rebooted host face listens on no address")
	}
	r.app, r.gateway, r.host = app, gateway, host
	r.closed, r.closeErr, r.lastChat = false, nil, ""
}

// face 报告注入面的监听地址。
func (r *smokeRig) face() string { return r.app.face }

// smokeInjection 是 /task 注入面的线上形状：宿主的 hostInjection 不导出，这里
// 按它的 JSON 契约逐字段重建，触发器来源与消息形状保持一致。
type smokeInjection struct {
	Messages      []smokeInjectionMessage `json:"messages"`
	TriggerSource string                  `json:"trigger_source"`
}

// smokeInjectionMessage 是注入里的一条消息。
type smokeInjectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// postTask 经本地注入面驱动一轮：POST /task，面回 202 后由它自己跑完。
func (r *smokeRig) postTask(text string) {
	r.t.Helper()
	status, body := r.postTaskStatus(text)
	if status != http.StatusAccepted {
		r.t.Fatalf("task injection status = %d, want 202: %s", status, body)
	}
}

// postTaskStatus 发一次注入并把面的状态与正文交回调用方：负向锚据此断言
// 注入面会拒绝什么，而不是只靠主流程全绿反推。
func (r *smokeRig) postTaskStatus(text string) (int, string) {
	r.t.Helper()
	payload, err := json.Marshal(smokeInjection{
		Messages:      []smokeInjectionMessage{{Role: string(tagentmodel.RoleUser), Content: text}},
		TriggerSource: "user",
	})
	if err != nil {
		r.t.Fatalf("encode the injection: %v", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post("http://"+r.face()+"/task", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		r.t.Fatalf("post the task injection: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(body)
}

// drive 走生产入站路径（hostRuntime.Drive）：绑定路由、抽干内部通知、驱动一轮。
func (r *smokeRig) drive(chat, text string) {
	r.t.Helper()
	if _, err := r.app.host.Drive(context.Background(), inboundMessage{
		ChatID: chat, UserName: chat, Text: text,
	}); err != nil {
		r.t.Fatalf("drive the session %s: %v", chat, err)
	}
	r.lastChat = chat
}

// route 把注入面指到该会话：注入面没有地址字段，只驱动"最后准入的用户会话"，
// 所以换会话时先用生产 Drive 把目标会话置为最近会话，再用 POST /task 驱动测量轮。
func (r *smokeRig) route(chat, marker string) {
	r.t.Helper()
	if r.lastChat == chat {
		return
	}
	r.drive(chat, marker+" switch to "+chat)
}

// healthz 探活一次。
func (r *smokeRig) healthz() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + r.face() + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("healthz = %d %q", resp.StatusCode, body)
	}
	return nil
}

// smokeReloadAnswer 是热换端点读回的结果。
type smokeReloadAnswer struct {
	Reloaded     bool `json:"reloaded"`
	ModelChanges []struct {
		Agent string `json:"agent"`
		From  string `json:"from"`
		To    string `json:"to"`
		Fact  string `json:"fact"`
	} `json:"model_changes"`
}

// reload 改写文档里的模型名并 POST /reload。
func (r *smokeRig) reload(model string) smokeReloadAnswer {
	r.t.Helper()
	r.cfg.Model = model
	r.cfg.write(r.t)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post("http://"+r.face()+"/reload", "application/json", nil)
	if err != nil {
		r.t.Fatalf("post the reload: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("reload status = %d, want 200: %s", resp.StatusCode, body)
	}
	var answer smokeReloadAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		r.t.Fatalf("decode the reload answer %q: %v", body, err)
	}
	return answer
}

// recorded 报告替身通道记下的全部外发。
func (r *smokeRig) recorded() []outboundEvent { return r.channel.recorded() }

// waitText 等到替身通道向该会话投递出含给定子串的文本。
func (r *smokeRig) waitText(chat, want string, timeout time.Duration) outboundEvent {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, evt := range r.recorded() {
			if evt.Kind == "text" && evt.Target == chat && strings.Contains(evt.Body, want) {
				return evt
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no text to %s carrying %q after %s (recorded=%d)\nrecent outbound:\n%s\nrecent requests:\n%s",
				chat, want, timeout, len(r.recorded()), r.recentOutbound(8), r.recentRequests(8))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fileCount 报告已向该会话投递过多少次该文件。
func (r *smokeRig) fileCount(chat, path string) int {
	count := 0
	for _, evt := range r.recorded() {
		if evt.Kind == "file" && evt.Target == chat && evt.Body == path {
			count++
		}
	}
	return count
}

// waitFile 等到替身通道向该会话再投递一次该文件（超过 after 次）。
func (r *smokeRig) waitFile(chat, path string, after int, timeout time.Duration) outboundEvent {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		seen := 0
		for _, evt := range r.recorded() {
			if evt.Kind == "file" && evt.Target == chat && evt.Body == path {
				seen++
				if seen > after {
					return evt
				}
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no file to %s named %q after %s (already delivered %d times)\nrecent outbound:\n%s\nrecent requests:\n%s",
				chat, path, timeout, seen, r.recentOutbound(8), r.recentRequests(8))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recentOutbound 渲染最近的若干条外发，供失败诊断。
func (r *smokeRig) recentOutbound(limit int) string {
	events := r.recorded()
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	lines := make([]string, 0, len(events))
	for _, evt := range events {
		body := evt.Body
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		lines = append(lines, fmt.Sprintf("- %s -> %s: %s", evt.Kind, evt.Target, body))
	}
	return strings.Join(lines, "\n")
}

// recentRequests 渲染最近若干次模型请求的模型名、最后一条用户消息与尾部消息，
// 供失败诊断。
func (r *smokeRig) recentRequests(limit int) string {
	requests := r.provider.recorded()
	if len(requests) > limit {
		requests = requests[len(requests)-limit:]
	}
	lines := make([]string, 0, len(requests))
	for _, req := range requests {
		tail := smokeAfterLastUser(req.Messages)
		tailText := ""
		if len(tail) > 0 {
			last := tail[len(tail)-1]
			tailText = last.Role + ":" + last.Content
			if len(tailText) > 160 {
				tailText = tailText[:160] + "…"
			}
		}
		lines = append(lines, fmt.Sprintf("- %s last=%q tail=%q", req.Model, smokeLastUserText(req.Messages), tailText))
	}
	return strings.Join(lines, "\n")
}

// pendingApproval 等到审批管理器里出现一条待裁决请求。
func (r *smokeRig) pendingApproval(timeout time.Duration) *approval.ApprovalRequest {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if pending := r.gateway.Manager().Pending(); len(pending) > 0 {
			return pending[0]
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no pending approval after %s", timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// addressOf 报告某会话的会址。
func (r *smokeRig) addressOf(chat string) event.Address {
	r.t.Helper()
	addr, err := r.app.host.router.AddressFor(chat)
	if err != nil {
		r.t.Fatalf("address of %s: %v", chat, err)
	}
	return addr
}

// sessionFacts 报告某会话的实时事实链。
func (r *smokeRig) sessionFacts(chat string) []event.Event {
	return r.host.Run().SessionFacts(r.addressOf(chat))
}

// smokeSample 是一次资源采样。
type smokeSample struct {
	Round      int
	Goroutines int
	HeapInuse  uint64
}

// sampleResources 采一次样，用于 goroutine 与堆的稳定性断言。
func (r *smokeRig) sampleResources(round int) smokeSample {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return smokeSample{Round: round, Goroutines: runtime.NumGoroutine(), HeapInuse: stats.HeapInuse}
}

// smokeLogRecorder 是替换 log.Default 的记录器：只记录，不转发，保持测试输出干净。
type smokeLogRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (l *smokeLogRecorder) add(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

// Debug implements log.Logger.
func (l *smokeLogRecorder) Debug(args ...any) { l.add("%s", fmt.Sprint(args...)) }

// Debugf implements log.Logger.
func (l *smokeLogRecorder) Debugf(format string, args ...any) { l.add(format, args...) }

// Info implements log.Logger.
func (l *smokeLogRecorder) Info(args ...any) { l.add("%s", fmt.Sprint(args...)) }

// Infof implements log.Logger.
func (l *smokeLogRecorder) Infof(format string, args ...any) { l.add(format, args...) }

// Warn implements log.Logger.
func (l *smokeLogRecorder) Warn(args ...any) { l.add("%s", fmt.Sprint(args...)) }

// Warnf implements log.Logger.
func (l *smokeLogRecorder) Warnf(format string, args ...any) { l.add(format, args...) }

// Error implements log.Logger.
func (l *smokeLogRecorder) Error(args ...any) { l.add("%s", fmt.Sprint(args...)) }

// Errorf implements log.Logger.
func (l *smokeLogRecorder) Errorf(format string, args ...any) { l.add(format, args...) }

// Fatal implements log.Logger without ending the process: the smoke keeps running.
func (l *smokeLogRecorder) Fatal(args ...any) { l.add("%s", fmt.Sprint(args...)) }

// Fatalf implements log.Logger without ending the process.
func (l *smokeLogRecorder) Fatalf(format string, args ...any) { l.add(format, args...) }

// count 报告含给定子串的行数。
func (l *smokeLogRecorder) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// size 报告记录到的行数。
func (l *smokeLogRecorder) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

// sample 报告含给定子串的前若干行，用于报告里的证据引用。
func (l *smokeLogRecorder) sample(substr string, limit int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, limit)
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			out = append(out, line)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// installSmokeLog 把 trpc 日志面换成记录器，测试结束时恢复。
func installSmokeLog(t *testing.T) *smokeLogRecorder {
	t.Helper()
	previous, previousContext := log.Default, log.ContextDefault
	recorder := &smokeLogRecorder{}
	log.Default = recorder
	log.ContextDefault = recorder
	t.Cleanup(func() {
		log.Default = previous
		log.ContextDefault = previousContext
	})
	return recorder
}

// smokeTrajectory 是轨迹目录的两份产物。
type smokeTrajectory struct {
	Records int
	Seal    *rl.CaptureManifest
	// Heads 是每条 attempt 响应正文的首行（截断），供行数与模型调用对不上时对照。
	Heads []string
}

// readTrajectory 读回 attempts.jsonl 的行数与 seal.json 的清单。
func readTrajectory(root string) (smokeTrajectory, error) {
	dir := filepath.Join(root, "trajectories")
	out := smokeTrajectory{}
	raw, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		return out, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out.Records++
		var decoded rl.Line
		head := "(unparsed)"
		if err := json.Unmarshal([]byte(line), &decoded); err == nil && decoded.Attempt != nil &&
			len(decoded.Attempt.Response.Choices) > 0 {
			head = strings.SplitN(decoded.Attempt.Response.Choices[0].Message.Content, "\n", 2)[0]
		}
		if decoded.Kind != "" && decoded.Kind != rl.RecordKindAttempt {
			head = string(decoded.Kind) + " " + head
		}
		if len(head) > 48 {
			head = head[:48] + "…"
		}
		out.Heads = append(out.Heads, fmt.Sprintf("%d %s", out.Records, head))
	}
	sealRaw, err := os.ReadFile(filepath.Join(dir, rl.SealFileName))
	if err != nil {
		return out, err
	}
	var seal rl.CaptureManifest
	if err := json.Unmarshal(sealRaw, &seal); err != nil {
		return out, err
	}
	out.Seal = &seal
	return out, nil
}

// syntheticEntries 报告某会话投影里的折叠产物。
func syntheticEntries(entries []replay.ProjectionEntry) []replay.ProjectionEntry {
	out := make([]replay.ProjectionEntry, 0, len(entries))
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			out = append(out, entry)
		}
	}
	return out
}
