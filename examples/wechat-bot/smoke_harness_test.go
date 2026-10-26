//go:build smoke

// smoke_harness_test.go 是本地长程冒烟的台架：进程内装配真实的微信宿主
// （hostRuntime / SessionRouter / SeenStore / internalInbox / approvalGateway /
// hostSender / registerHandlers 全是生产实例），模型换成本地 scripted HTTP，
// 微信出站换成记录全部外发的替身通道，宿主只经本地 HTTP 面驱动。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
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
	// MeditationInterval 覆盖冥想节奏下限的渲染值；空值保持文档默认的 5s。
	// 第三波用它把反思固定成首次提交触发的一次（1h），或放开成毫秒级以判定
	// 自身产物是否自激（1ms）。
	MeditationInterval string
	// MeditationMinGap 覆盖上次反思被消费后的最小间隔；空值保持默认的 10s。
	MeditationMinGap string
	// MaxTokens 覆盖入口 agent 的回放窗口；0 保持既有 2000，校准与长程用它
	// 参数化 256/32768 两档（语义是回放 MaxTokens，不是响应长度）。
	MaxTokens int
	// DeliveryRuler 把投递审核注入宿主；nil 保持默认不拦截（D5）。症状 G-B
	// 与收尾轮迁移用它在投递侧挂起/放行。
	DeliveryRuler plugin.DeliveryRuler
}

// meditationInterval 报告文档渲染的冥想节奏下限。
func (c *smokeConfig) meditationInterval() string {
	if c.MeditationInterval != "" {
		return c.MeditationInterval
	}
	return "5s"
}

// meditationMinGap 报告文档渲染的反思最小间隔。
func (c *smokeConfig) meditationMinGap() string {
	if c.MeditationMinGap != "" {
		return c.MeditationMinGap
	}
	return "10s"
}

// maxTokens 报告文档渲染的回放窗口；0 保持既有 smoke 的 2000。
func (c *smokeConfig) maxTokens() int {
	if c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return 2000
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

// document 渲染宿主文档：入口 agent 小窗口高频折叠、多 agent、治理开启。
// 入口不声明 send：正文只经自动投递出口落地，工具面无 send（D4）。
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
      - { kind: agent, agent: smoke-worker }
    max_tokens: %d
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
      interval: %s
      min_gap: %s
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
		smokeSessionTag, c.Model, c.maxTokens(),
		smokeSessionTag, c.meditationInterval(), c.meditationMinGap(),
		smokeSessionTag, smokeSessionTag,
		c.StorageRoot, c.ApprovalDir,
		c.ConfigDir, filepath.Join(c.ConfigDir, "token.json"), c.ContextDir,
		c.WorkspaceDir, smokeApproverChat,
	)
}

// smokeApprovalDocument 在默认文档上重新声明 send 与它的 critical 风险规则：
// 审批通道（tool+digest/TTL/重试执行）仍要一个 critical 工具来练，而生产入口
// 不声明 send（D4）。只有审批类场景用这份夹具，其余场景走无 send 的默认文档。
func smokeApprovalDocument(c *smokeConfig) string {
	base := *c
	base.Doc = nil
	document := base.document()
	tools := "    tools:\n      - { kind: agent, agent: smoke-worker }\n"
	patched := strings.Replace(document, tools,
		"    tools:\n      - { kind: tool, id: send }\n      - { kind: agent, agent: smoke-worker }\n", 1)
	if patched == document {
		panic("smoke: the rendered document carries no entry tools block to declare send in")
	}
	ttl := "  approval_ttl_minutes: 30\n"
	patched = strings.Replace(patched, ttl,
		ttl+"  risk_rules:\n    - { tool: send, level: critical }\n", 1)
	if !strings.Contains(patched, "risk_rules") {
		panic("smoke: the rendered document carries no governance block to declare the risk rule in")
	}
	return patched
}

// smokeModelRequest 是脚本模型收到的一次请求：模型名、它读到的消息与它被授予的
// 工具面（工具名）。
type smokeModelRequest struct {
	Model    string
	Messages []smokeModelMessage
	Tools    []string
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
	var tools []string
	if raw, ok := body["tools"]; ok {
		var declared []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &declared); err == nil {
			for _, tool := range declared {
				tools = append(tools, tool.Function.Name)
			}
		}
	}
	p.mu.Lock()
	index := len(p.requests)
	p.requests = append(p.requests, smokeModelRequest{Model: modelName, Messages: messages, Tools: tools})
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
// - 受理回执按结构取键（tool 角色 + 本次调用的 id）：回执是裸 "ok"，正文里没有可识别的字样，按正文取键会让脚本答不上来而重复发起委派。
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
		case smokePairedResult(messages, "call-worker-"+marker):
			return smokeAnswerBody("delegate ack " + marker)
		case smokePairedResult(messages, "call-retry-"+marker):
			return smokeAnswerBody("approved-done " + marker)
		case smokeCredentialFrame(end):
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
	case strings.Contains(last, "smoke review"):
		return smokeAnswerBody("hold-ack " + marker)
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

// smokeCalibrationMustExecutable 报告当前测试二进制：宿主子进程与基线场景都
// 由它自执行。
func smokeCalibrationMustExecutable(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	return binary
}

// smokeCalibrationRunDir 报告本次校准的产物目录：TMPDIR 下按窗口/seed/时刻
// 分目录，不覆盖历史原始数据；基线子进程用环境变量指定自己的子目录。
func smokeCalibrationRunDir(t *testing.T) string {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv(smokeCalibrationRunEnv))
	if dir == "" {
		base := os.Getenv("TMPDIR")
		if base == "" {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "event-consumption-integrity", "03.10",
			fmt.Sprintf("run-w%d-s%d-%s", *smokeCalibrationWindowFlag, *smokeCalibrationSeedFlag, time.Now().Format("20060102-150405")))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the calibration run directory %s: %v", dir, err)
	}
	return dir
}

// smokeCalibrationBuildPlan 把契约里的父侧路径与缩样时间线落成一份计划。
func smokeCalibrationBuildPlan(cfg *smokeConfig, runDir string, window, seed int) smokeCalibrationPlan {
	return smokeCalibrationPlan{
		Leaf: smokeCalibrationLeaf, Window: window, Seed: seed,
		Root: cfg.Root, WorkspaceDir: cfg.WorkspaceDir, ConfigDir: cfg.ConfigDir,
		ContextDir: cfg.ContextDir, ApprovalDir: cfg.ApprovalDir, PromptDir: cfg.PromptDir,
		StorageRoot: cfg.StorageRoot, ConfigPath: cfg.ConfigPath, Endpoint: cfg.Endpoint,
		Sessions: smokeCalibrationChats, InputsPerSession: smokeCalibrationInputsPerSession,
		RestartsPerSession: smokeCalibrationRestartsPerSession, RestartEvery: smokeCalibrationRestartEvery,
		Timeline:  smokeCalibrationTimeline(smokeCalibrationInputsPerSession, smokeCalibrationRestartsPerSession, smokeCalibrationRestartEvery, smokeCalibrationChats),
		ReportDir: filepath.Join(runDir, "reports"),
	}
}

// smokeCalibrationColdBuild 测冷构建：隔离 GOCACHE 从零编译同一个测试包。
// 外层已用 date 计时时通过 TAGENT_CALIBRATION_COLD_BUILD_MS 传入，台架跳过重编。
func smokeCalibrationColdBuild(t *testing.T, runDir string, race bool) (float64, string) {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv(smokeCalibrationColdEnv)); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil && value > 0 {
			return value, "external"
		}
		t.Fatalf("TAGENT_CALIBRATION_COLD_BUILD_MS = %q is not a positive millisecond count", raw)
	}
	out := filepath.Join(runDir, "cold-build.test")
	args := []string{"test", "-c", "-tags", "smoke"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-o", out, ".")
	ctx, cancel := context.WithTimeout(context.Background(), smokeCalibrationColdBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = smokeCalibrationEnv("GOCACHE", filepath.Join(runDir, "cold-gocache"))
	started := time.Now()
	payload, err := cmd.CombinedOutput()
	elapsed := smokeCalibrationMS(time.Since(started))
	if err != nil {
		t.Fatalf("cold build the smoke package (isolated GOCACHE): %v\n%s", err, smokeCalibrationLogTailText(string(payload), 40))
	}
	return elapsed, "self"
}

// smokeCalibrationLogTailText 截取命令输出的末尾若干行。
func smokeCalibrationLogTailText(text string, lines int) string {
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// smokeCalibrationBaselineBuild 用同一个包编一份非 race 测试二进制，并在独立
// 目录里跑同一份计划：两份 wall 的差就是 -race 开销。
func smokeCalibrationBaselineBuild(t *testing.T, runDir string, window, seed int) (smokeCalibrationBaseline, string) {
	t.Helper()
	plain := filepath.Join(runDir, "wechat-bot-plain.test")
	ctx, cancel := context.WithTimeout(context.Background(), smokeCalibrationColdBuildTimeout)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "test", "-c", "-tags", "smoke", "-o", plain, ".")
	build.Env = os.Environ()
	started := time.Now()
	payload, err := build.CombinedOutput()
	buildMS := smokeCalibrationMS(time.Since(started))
	if err != nil {
		t.Fatalf("build the non-race baseline binary: %v\n%s", err, smokeCalibrationLogTailText(string(payload), 40))
	}

	baseDir := filepath.Join(runDir, "baseline")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("create the baseline directory: %v", err)
	}
	logPath := filepath.Join(baseDir, "scenario.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create the baseline log: %v", err)
	}
	defer logFile.Close()
	scenario := exec.Command(plain, "-test.run=^TestEventIntegrityLongRunCalibration$",
		"-test.timeout="+smokeCalibrationChildTimeout.String(),
		fmt.Sprintf("-window=%d", window), fmt.Sprintf("-seed=%d", seed))
	scenario.Env = smokeCalibrationEnv(
		smokeCalibrationModeEnv, smokeCalibrationModeScenario,
		smokeCalibrationBaselineEnv, "1",
		smokeCalibrationRunEnv, baseDir,
	)
	scenario.Stdout, scenario.Stderr = logFile, logFile
	if err := scenario.Run(); err != nil {
		t.Fatalf("run the non-race baseline scenario: %v\n%s", err, smokeCalibrationLogTail(logPath, 40))
	}
	var baseline smokeCalibrationBaseline
	if err := smokeCalibrationReadJSON(filepath.Join(baseDir, "baseline.json"), &baseline); err != nil {
		t.Fatalf("read the baseline report: %v", err)
	}
	baseline.BaselineBuildMS = buildMS
	return baseline, baseDir
}

// smokeCalibrationBaselineScenario 在非 race 二进制里跑同一份缩样场景，只交回
// wall 与逐输入均值；它跳过基线构建（否则递归），也不判预算。
func smokeCalibrationBaselineScenario(t *testing.T) {
	t.Helper()
	runDir := smokeCalibrationRunDir(t)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.MaxTokens = *smokeCalibrationWindowFlag
	cfg.write(t)
	plan := smokeCalibrationBuildPlan(cfg, runDir, *smokeCalibrationWindowFlag, *smokeCalibrationSeedFlag)
	planPath := filepath.Join(runDir, "plan.json")
	if err := smokeCalibrationWriteJSON(planPath, plan); err != nil {
		t.Fatalf("write the baseline plan: %v", err)
	}
	run := smokeCalibrationRunGenerations(t, smokeCalibrationMustExecutable(t), planPath, runDir, plan)
	samples := smokeCalibrationInputSamples(run)
	inputMS := make([]float64, 0, len(samples))
	for _, sample := range samples {
		inputMS = append(inputMS, sample.MS)
	}
	baseline := smokeCalibrationBaseline{
		Window: plan.Window, Seed: plan.Seed, WallMS: run.WallMS,
		PerInputMS: smokeCalibrationMean(inputMS), InputSamples: len(samples),
		InputsTotal: plan.inputs(), RaceEnabled: smokeCalibrationRaceEnabled(),
	}
	if err := smokeCalibrationWriteJSON(filepath.Join(runDir, "baseline.json"), baseline); err != nil {
		t.Fatalf("write the baseline report: %v", err)
	}
}

// smokeCalibrationInputSamples 摊平各代的逐输入样本。
func smokeCalibrationInputSamples(run *smokeCalibrationRun) []smokeCalibrationInputSample {
	samples := make([]smokeCalibrationInputSample, 0, smokeCalibrationInputsPerSession*smokeCalibrationSessions)
	for _, generation := range run.Generations {
		samples = append(samples, generation.Inputs...)
	}
	return samples
}

// smokeCalibrationRebindSamples 摊平各代的重绑样本。
func smokeCalibrationRebindSamples(run *smokeCalibrationRun) []smokeCalibrationRebindSample {
	samples := make([]smokeCalibrationRebindSample, 0, (smokeCalibrationRestartsPerSession*smokeCalibrationSessions+1)*smokeCalibrationSessions)
	for _, generation := range run.Generations {
		samples = append(samples, generation.Rebinds...)
	}
	return samples
}

// smokeCalibrationReconcile 跨重启对账：每个输入标记恰好一次入口请求，每个
// 重绑标记恰好一次，并统计重启后首轮携带上下文消息的输入数。
func smokeCalibrationReconcile(provider *smokeProvider, plan smokeCalibrationPlan) smokeCalibrationReconciliation {
	recon := smokeCalibrationReconciliation{ExpectedInputs: plan.inputs()}
	counts := map[string]int{}
	messages := map[string][]int{}
	for _, req := range provider.recorded() {
		recon.ProviderRequests++
		if req.Model == smokeReflectModel {
			recon.ReflectRequests++
			continue
		}
		if req.Model != smokeEntryModel {
			continue
		}
		last := strings.TrimSpace(smokeLastUserText(req.Messages))
		counts[last]++
		messages[last] = append(messages[last], len(req.Messages))
	}
	for _, step := range plan.Timeline {
		if step.Kind != "input" {
			continue
		}
		switch counts[step.Text] {
		case 1:
			recon.DrivenInputs++
		case 0:
			recon.MissingInputs = append(recon.MissingInputs, step.Marker)
		default:
			recon.DuplicateInputs = append(recon.DuplicateInputs, step.Marker)
		}
	}
	for gen := 0; gen <= plan.restarts(); gen++ {
		for index, chat := range plan.Sessions {
			recon.ExpectedRebinds++
			marker := fmt.Sprintf("SMOKE-R86%02d%d", gen, index)
			text := marker + " rebind " + chat
			if counts[text] == 1 {
				recon.RebindRequests++
			}
			if counts[text] == 0 {
				recon.MissingRebinds = append(recon.MissingRebinds, marker)
			}
		}
	}
	for gen := 1; gen <= plan.restarts(); gen++ {
		for _, chat := range plan.Sessions {
			step, ok := smokeCalibrationFirstInput(plan, gen, chat)
			if !ok {
				continue
			}
			recon.RestoredContextChecks++
			if sizes := messages[step.Text]; len(sizes) == 1 && sizes[0] >= 3 {
				recon.RestoredContextInputs++
			}
		}
	}
	return recon
}

// smokeCalibrationFirstInput 报告某代里某会话的第一个输入步。
func smokeCalibrationFirstInput(plan smokeCalibrationPlan, gen int, chat string) (smokeCalibrationStep, bool) {
	var found smokeCalibrationStep
	ok := false
	for _, step := range plan.Timeline {
		if step.Generation != gen || step.Kind != "input" || step.Session != chat {
			continue
		}
		if !ok || step.Index < found.Index {
			found, ok = step, true
		}
	}
	return found, ok
}

// smokeCalibrationExtrapolate 把缩样成本外推到 03.11 单组合（1200 输入/120
// 重启），并给出 05.1 的预算建议。所有外推值都带复核标注。
func smokeCalibrationExtrapolate(report *smokeCalibrationReport) {
	measurements := &report.Measurements
	inputFactor := float64(smokeCalibrationFullInputs) / float64(report.Scale.InputsTotal)
	restartFactor := float64(smokeCalibrationFullRestarts) / float64(report.Scale.RestartsTotal)
	linearWall := float64(smokeCalibrationFullInputs)*measurements.PerInputMS +
		float64(smokeCalibrationFullRestarts)*measurements.PerRestartMS + measurements.InitialBootMS

	slope := smokeCalibrationSlope(measurements.InputDetail)
	contextWall := linearWall
	if slope > 0 {
		meanIndex := float64(smokeCalibrationInputsPerSession-1) / 2
		fullMeanIndex := float64(300-1) / 2
		perInput := measurements.PerInputMS + slope*(fullMeanIndex-meanIndex)
		if perInput > 4*measurements.PerInputMS {
			perInput = 4 * measurements.PerInputMS
		}
		if perInput < measurements.PerInputMS {
			perInput = measurements.PerInputMS
		}
		contextWall = float64(smokeCalibrationFullInputs)*perInput +
			float64(smokeCalibrationFullRestarts)*measurements.PerRestartMS + measurements.InitialBootMS
	}

	required := math.Max(linearWall, contextWall)
	fullStorage := int64(float64(measurements.Storage.GrowthBytes) * inputFactor)
	fullRSSLinear := int64(float64(measurements.Peak.MaxChildRSSBytes) * inputFactor)
	fullRSSBounded := int64(float64(measurements.Peak.MaxChildRSSBytes) * 1.5)
	fullCPU := int64(float64(measurements.Peak.TotalChildCPUMillis) * inputFactor)

	report.Extrapolation = smokeCalibrationExtrapolation{
		Note:                      smokeCalibrationReviewNote,
		Formula:                   "full_wall_ms = 1200×per_input_ms + 120×per_restart_ms + initial_boot_ms；full_storage = growth × (1200/80)；full_rss 线性值 = max_child_rss × (1200/80)，有界值 = ×1.5；full_cpu = 子进程 CPU 总和 × (1200/80)",
		InputGrowthFactor:         inputFactor,
		RestartGrowthFactor:       restartFactor,
		FullWallMS:                linearWall,
		FullWallMinutes:           linearWall / 60000,
		ContextSlopeMS:            slope,
		ContextAdjustedFullWallMS: contextWall,
		FullStorageBytes:          fullStorage,
		FullRSSBytesLinear:        fullRSSLinear,
		FullRSSBytesBounded:       fullRSSBounded,
		RSSModel:                  "non_linear_bounded",
		FullCPUMillis:             fullCPU,
	}

	testSeconds := int(math.Ceil(required * smokeCalibrationBudgetMargin / 1000))
	if testSeconds < smokeCalibrationMinGoTestSeconds {
		testSeconds = smokeCalibrationMinGoTestSeconds
	}
	requiredJobMS := required*smokeCalibrationMatrixCombos + measurements.ColdBuildMS
	jobMinutes := int(math.Ceil((float64(testSeconds)*smokeCalibrationMatrixCombos+
		measurements.ColdBuildMS*smokeCalibrationBudgetMargin/1000)/60)) + smokeCalibrationJobSetupMinutes
	candidateMS := float64(smokeCalibrationCandidateLong) * 60 * 1000
	feasible := requiredJobMS <= candidateMS
	verdict := "feasible"
	reason := fmt.Sprintf("单组合外推（含历史增长修正）%.2f 分钟 × %d 组合 + 冷构建 %.2f 分钟 = %.2f 分钟；候选 %d 分钟足以覆盖，建议 %d 分钟（分片 go test %d 秒）",
		required/60000, smokeCalibrationMatrixCombos, measurements.ColdBuildMS/60000, requiredJobMS/60000,
		smokeCalibrationCandidateLong, jobMinutes, testSeconds)
	if !feasible {
		verdict = "infeasible"
		reason = fmt.Sprintf("单组合外推 %.2f 分钟 × %d 组合 + 冷构建 = %.2f 分钟超过候选 %d 分钟，建议冻结值上调到 %d 分钟",
			required/60000, smokeCalibrationMatrixCombos, requiredJobMS/60000,
			smokeCalibrationCandidateLong, jobMinutes)
	}
	shortReason := "本校准只覆盖长程内核；short 的成本由 04.2 自己的校准提供，本轮沿用候选值，不据此冻结"
	mutationsReason := "本校准只覆盖长程内核；mutations 的成本由 04.4 自己的校准提供，本轮沿用候选值，不据此冻结"
	report.Budget = smokeCalibrationBudget{
		Source: "measured",
		Margin: smokeCalibrationBudgetMargin,
		Candidates: smokeCalibrationCandidates{
			ShortMinutes: smokeCalibrationCandidateShort, LongMinutes: smokeCalibrationCandidateLong,
			MutationsMinutes: smokeCalibrationCandidateMutations,
		},
		Recommended: smokeCalibrationRecommended{
			LongJobTimeoutMinutes: jobMinutes, GoTestTimeoutSeconds: testSeconds,
			ShortJobTimeoutMinutes: smokeCalibrationCandidateShort, MutationsJobTimeoutMinutes: smokeCalibrationCandidateMutations,
		},
		Feasibility: []smokeCalibrationFeasibility{
			{Surface: "short", CandidateMinutes: smokeCalibrationCandidateShort, Measured: false,
				Verdict: "provisional_not_measured", RecommendedMinutes: smokeCalibrationCandidateShort, Reason: shortReason},
			{Surface: "long", CandidateMinutes: smokeCalibrationCandidateLong, Measured: true,
				Verdict: verdict, RequiredMinutes: requiredJobMS / 60000,
				RequiredPerShardMinutes: required / 60000,
				RecommendedMinutes:      jobMinutes, Reason: reason},
			{Surface: "mutations", CandidateMinutes: smokeCalibrationCandidateMutations, Measured: false,
				Verdict: "provisional_not_measured", RecommendedMinutes: smokeCalibrationCandidateMutations, Reason: mutationsReason},
		},
		OverBudget:         !feasible,
		OverBudgetReported: !feasible,
		P95: smokeCalibrationP95{
			Claimed: len(measurements.InputDetail) >= smokeCalibrationP95MinSamples,
			Samples: len(measurements.InputDetail), PerInputMS: measurements.PerInputP95MS,
			Note: fmt.Sprintf("基于 %d 个缩样输入的 p95，不是 03.11 的 1200 输入 p95", len(measurements.InputDetail)),
		},
		Notes: []string{
			smokeCalibrationReviewNote,
			"long 分片 go test 建议 = max(ceil(外推 wall×1.5), 60s)；long 作业建议 = ceil((6 分片 + 冷构建×1.5)/60)+5 分钟 runner 准备；候选 45m 的可行性按同一作业口径判断",
			shortReason,
			mutationsReason,
			"RSS 由窗口与并发决定，不是输入数的线性函数：线性值只作上限参考，建议值取有界模型",
		},
	}
}

// errCalibrationColdBuild, errCalibrationHotRun, errCalibrationRestarts, errCalibrationRace, errCalibrationStorage, errCalibrationPeak, errCalibrationFixedTimeout, errCalibrationP95, errCalibrationOverBudget, errCalibrationReviewNote, errCalibrationWindow and errCalibrationInputs are the named failures of the validator's negative anchors.
var (
	errCalibrationColdBuild    = errors.New("calibration: cold build cost missing")
	errCalibrationHotRun       = errors.New("calibration: hot run cost missing")
	errCalibrationRestarts     = errors.New("calibration: restart cost missing")
	errCalibrationRace         = errors.New("calibration: -race overhead missing")
	errCalibrationStorage      = errors.New("calibration: storage growth missing")
	errCalibrationPeak         = errors.New("calibration: peak resource missing")
	errCalibrationFixedTimeout = errors.New("calibration: budget copied from the candidate timeout")
	errCalibrationP95          = errors.New("calibration: p95 claimed from too few samples")
	errCalibrationOverBudget   = errors.New("calibration: over-budget extrapolation not reported")
	errCalibrationReviewNote   = errors.New("calibration: extrapolation missing the review note")
	errCalibrationWindow       = errors.New("calibration: window is not one of the two frozen levels")
	errCalibrationInputs       = errors.New("calibration: input samples do not cover the shrunk timeline")
)

// smokeCalibrationValidate 校验六项成本齐全、外推标注存在、预算来源是实测而
// 不是候选超时，以及超预算与 p95 的申报一致。
func smokeCalibrationValidate(report *smokeCalibrationReport) error {
	if report.Leaf != smokeCalibrationLeaf || report.Kind != "calibration" {
		return fmt.Errorf("calibration: report identity = %q/%q", report.Leaf, report.Kind)
	}
	if report.Window != 256 && report.Window != 32768 {
		return fmt.Errorf("%w: %d", errCalibrationWindow, report.Window)
	}
	if report.Scale.Sessions != smokeCalibrationSessions || report.Scale.InputsTotal != smokeCalibrationInputsPerSession*smokeCalibrationSessions ||
		report.Scale.RestartsTotal != smokeCalibrationRestartsPerSession*smokeCalibrationSessions {
		return fmt.Errorf("calibration: scale = %+v, want %d sessions × %d inputs × %d restarts",
			report.Scale, smokeCalibrationSessions, smokeCalibrationInputsPerSession, smokeCalibrationRestartsPerSession)
	}
	measurements := report.Measurements
	if measurements.ColdBuildMS <= 0 || measurements.ColdBuildSource == "" {
		return fmt.Errorf("%w: cold_build_ms=%v source=%q", errCalibrationColdBuild, measurements.ColdBuildMS, measurements.ColdBuildSource)
	}
	if measurements.HotRunWallMS <= 0 || measurements.PerInputMS <= 0 || measurements.PerInputP95MS <= 0 {
		return fmt.Errorf("%w: wall=%v per_input=%v p95=%v", errCalibrationHotRun,
			measurements.HotRunWallMS, measurements.PerInputMS, measurements.PerInputP95MS)
	}
	if measurements.InputSamples != report.Scale.InputsTotal {
		return fmt.Errorf("%w: samples=%d want=%d", errCalibrationInputs, measurements.InputSamples, report.Scale.InputsTotal)
	}
	if measurements.PerRestartMS <= 0 || len(measurements.RestartDetail) != report.Scale.RestartsTotal {
		return fmt.Errorf("%w: per_restart=%v samples=%d want=%d", errCalibrationRestarts,
			measurements.PerRestartMS, len(measurements.RestartDetail), report.Scale.RestartsTotal)
	}
	if !measurements.Race.Enabled || measurements.Race.BaselineWallMS <= 0 || measurements.Race.OverheadFactor <= 0 {
		return fmt.Errorf("%w: enabled=%t baseline_wall=%v factor=%v", errCalibrationRace,
			measurements.Race.Enabled, measurements.Race.BaselineWallMS, measurements.Race.OverheadFactor)
	}
	if measurements.Storage.GrowthBytes <= 0 || measurements.Storage.AfterBytes < measurements.Storage.BeforeBytes {
		return fmt.Errorf("%w: growth=%d before=%d after=%d", errCalibrationStorage,
			measurements.Storage.GrowthBytes, measurements.Storage.BeforeBytes, measurements.Storage.AfterBytes)
	}
	if measurements.Peak.MaxChildRSSBytes <= 0 || measurements.Peak.MaxChildCPUMillis <= 0 {
		return fmt.Errorf("%w: rss=%d cpu=%d", errCalibrationPeak,
			measurements.Peak.MaxChildRSSBytes, measurements.Peak.MaxChildCPUMillis)
	}
	if report.Extrapolation.Note != smokeCalibrationReviewNote || report.Extrapolation.Formula == "" || report.Extrapolation.FullWallMS <= 0 {
		return fmt.Errorf("%w: note=%q formula=%q wall=%v", errCalibrationReviewNote,
			report.Extrapolation.Note, report.Extrapolation.Formula, report.Extrapolation.FullWallMS)
	}
	if report.Budget.Source != "measured" {
		return fmt.Errorf("%w: source=%q", errCalibrationFixedTimeout, report.Budget.Source)
	}
	if report.Budget.Recommended.GoTestTimeoutSeconds <= 0 || report.Budget.Recommended.LongJobTimeoutMinutes <= 0 {
		return fmt.Errorf("%w: go_test=%d job=%d", errCalibrationFixedTimeout,
			report.Budget.Recommended.GoTestTimeoutSeconds, report.Budget.Recommended.LongJobTimeoutMinutes)
	}
	if report.Budget.Recommended.LongJobTimeoutMinutes*60 <= report.Budget.Recommended.GoTestTimeoutSeconds {
		return fmt.Errorf("calibration: the job timeout %dm does not exceed the go test timeout %ds",
			report.Budget.Recommended.LongJobTimeoutMinutes, report.Budget.Recommended.GoTestTimeoutSeconds)
	}
	if report.Budget.P95.Claimed && report.Budget.P95.Samples < smokeCalibrationP95MinSamples {
		return fmt.Errorf("%w: samples=%d", errCalibrationP95, report.Budget.P95.Samples)
	}
	required := math.Max(report.Extrapolation.FullWallMS, report.Extrapolation.ContextAdjustedFullWallMS)
	requiredJob := required*smokeCalibrationMatrixCombos + report.Measurements.ColdBuildMS
	exceeds := requiredJob > float64(smokeCalibrationCandidateLong)*60*1000
	if exceeds && (!report.Budget.OverBudget || !report.Budget.OverBudgetReported) {
		return fmt.Errorf("%w: extrapolated job %.1f min > candidate %d min, over_budget=%t reported=%t",
			errCalibrationOverBudget, requiredJob/60000, smokeCalibrationCandidateLong,
			report.Budget.OverBudget, report.Budget.OverBudgetReported)
	}
	return nil
}

// smokeCalibrationPrimary 跑一次完整校准：冷构建（外部或隔离 GOCACHE 自测）→
// 缩样场景（真实子进程宿主、8 次 SIGKILL 重启）→ 存储前后账 → 非 race 基线，
// 最后外推并落盘报告。
func smokeCalibrationPrimary(t *testing.T) (*smokeCalibrationReport, string) {
	t.Helper()
	runDir := smokeCalibrationRunDir(t)
	window, seed := *smokeCalibrationWindowFlag, *smokeCalibrationSeedFlag
	if window != 256 && window != 32768 {
		t.Fatalf("window %d is not one of the two calibration levels (256/32768)", window)
	}
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.MaxTokens = window
	cfg.write(t)

	plan := smokeCalibrationBuildPlan(cfg, runDir, window, seed)
	planPath := filepath.Join(runDir, "plan.json")
	if err := smokeCalibrationWriteJSON(planPath, plan); err != nil {
		t.Fatalf("write the calibration plan: %v", err)
	}
	beforeBytes, beforeFiles, problems := smokeCalibrationWalk(cfg.StorageRoot)

	raceEnabled := smokeCalibrationRaceEnabled()
	coldMS, coldSource := smokeCalibrationColdBuild(t, runDir, raceEnabled)
	run := smokeCalibrationRunGenerations(t, smokeCalibrationMustExecutable(t), planPath, runDir, plan)
	afterBytes, afterFiles, afterProblems := smokeCalibrationWalk(cfg.StorageRoot)
	problems = append(problems, afterProblems...)

	inputs := smokeCalibrationInputSamples(run)
	inputMS := make([]float64, 0, len(inputs))
	for _, sample := range inputs {
		inputMS = append(inputMS, sample.MS)
	}
	rebinds := smokeCalibrationRebindSamples(run)
	rebindMS := make([]float64, 0, len(rebinds))
	for _, sample := range rebinds {
		rebindMS = append(rebindMS, sample.MS)
	}
	restartMS := make([]float64, 0, len(run.Restarts))
	for _, sample := range run.Restarts {
		restartMS = append(restartMS, sample.KillToReadyMS)
	}

	report := &smokeCalibrationReport{
		Leaf: smokeCalibrationLeaf, Kind: "calibration", Window: window, Seed: seed,
		RaceEnabled: raceEnabled, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Machine: smokeCalibrationMachine{
			GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, NumCPU: runtime.NumCPU(),
			GOMAXPROCS: runtime.GOMAXPROCS(0), GoVersion: runtime.Version(),
		},
		Scale: smokeCalibrationScale{
			Sessions: len(smokeCalibrationChats), InputsPerSession: smokeCalibrationInputsPerSession,
			InputsTotal: plan.inputs(), RestartsPerSession: smokeCalibrationRestartsPerSession,
			RestartsTotal: plan.restarts(), Generations: plan.restarts() + 1,
			RestartEvery: smokeCalibrationRestartEvery, SharedHost: true,
			MeditationEnabled: true, FsyncDropped: false,
		},
		Measurements: smokeCalibrationMeasurements{
			ColdBuildMS: coldMS, ColdBuildSource: coldSource,
			InitialBootMS: run.InitialBootMS, HotRunWallMS: run.WallMS,
			HotRunSource: "measured（首个 spawn → 末代关闭，冷构建在外）",
			PerInputMS:   smokeCalibrationMean(inputMS), PerInputMedianMS: smokeCalibrationMedian(inputMS),
			PerInputP95MS: smokeCalibrationP95Of(inputMS), InputSamples: len(inputs), InputDetail: inputs,
			PerRestartMS: smokeCalibrationMean(restartMS), RestartDetail: run.Restarts,
			RebindSamples: len(rebinds), RebindMeanMS: smokeCalibrationMean(rebindMS),
			Storage: smokeCalibrationStorage{
				Root: cfg.StorageRoot, BeforeBytes: beforeBytes, AfterBytes: afterBytes,
				GrowthBytes: afterBytes - beforeBytes, BeforeFiles: beforeFiles, AfterFiles: afterFiles,
				FilesAdded: afterFiles - beforeFiles, Problems: problems,
			},
			Peak: run.Peak,
		},
		Raw: smokeCalibrationRaw{
			RunDir: runDir, PlanPath: planPath, ChildLogs: run.ChildLogs,
			ColdBuild: filepath.Join(runDir, "cold-build.test"),
		},
	}

	if raceEnabled {
		baseline, baseDir := smokeCalibrationBaselineBuild(t, runDir, window, seed)
		report.Raw.BaselineDir = baseDir
		factor := 0.0
		if baseline.WallMS > 0 {
			factor = run.WallMS / baseline.WallMS
		}
		perInputFactor := 0.0
		if baseline.PerInputMS > 0 {
			perInputFactor = report.Measurements.PerInputMS / baseline.PerInputMS
		}
		report.Measurements.Race = smokeCalibrationRace{
			Enabled: true, BaselineWallMS: baseline.WallMS, BaselinePerInputMS: baseline.PerInputMS,
			BaselineInputs: baseline.InputSamples, BaselineBuildMS: baseline.BaselineBuildMS,
			OverheadFactor: factor, PerInputFactor: perInputFactor, Source: "same scenario, non-race test binary",
		}
	} else {
		report.Measurements.Race = smokeCalibrationRace{Enabled: false, Source: "not_applicable: this binary was not built with -race"}
	}

	report.Reconciliation = smokeCalibrationReconcile(provider, plan)
	smokeCalibrationExtrapolate(report)
	reportPath := filepath.Join(runDir, fmt.Sprintf("report-window-%d-seed-%d.json", window, seed))
	report.Raw.ReportPath = reportPath
	if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
		t.Fatalf("write the calibration report %s: %v", reportPath, err)
	}
	if err := smokeCalibrationWriteJSON(filepath.Join(runDir, "report.json"), report); err != nil {
		t.Fatalf("write the calibration report alias: %v", err)
	}
	return report, reportPath
}

// smokeCalibrationBudgetFile 是 05.1 唯一读取的那一份字段：报告里的 budget。
type smokeCalibrationBudgetFile struct {
	Leaf   string                 `json:"leaf"`
	Window int                    `json:"window"`
	Budget smokeCalibrationBudget `json:"budget"`
}

// smokeCalibrationReadBudget 从磁盘报告里只读 budget 对象：消费面与 05.1 的
// 预算冻结 / timeout-minutes 读取同一份 JSON 字段，不另建第二张预算表。
func smokeCalibrationReadBudget(path string) (smokeCalibrationBudget, error) {
	var file smokeCalibrationBudgetFile
	if err := smokeCalibrationReadJSON(path, &file); err != nil {
		return smokeCalibrationBudget{}, err
	}
	if file.Leaf != smokeCalibrationLeaf {
		return smokeCalibrationBudget{}, fmt.Errorf("calibration: budget file leaf = %q", file.Leaf)
	}
	return file.Budget, nil
}

// smokeCalibrationClone 深拷贝一份报告：负锚在副本上逐项变异，不污染实测值。
func smokeCalibrationClone(t *testing.T, report *smokeCalibrationReport) *smokeCalibrationReport {
	t.Helper()
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("encode the calibration report: %v", err)
	}
	var clone smokeCalibrationReport
	if err := json.Unmarshal(payload, &clone); err != nil {
		t.Fatalf("decode the calibration report: %v", err)
	}
	return &clone
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
// 结果消息（send 成功回执就是裸 ok 的工具结果），或循环折成用户角色的两种帧——
// send 成功回执凭据本身（裸 ok）与回执帧/工具结果帧（{call-... task-N true/false ...} /
// [deliver_summary_ok]）。
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
		smokeCredentialFrame(content) || strings.HasPrefix(content, "[deliver_summary_")
}

// smokePairedResult 报告请求的最后一条消息是不是给定调用的工具结果：按角色与
// 调用 id 取键，因为受理回执是裸 "ok"，正文里没有可识别的字样。
func smokePairedResult(messages []smokeModelMessage, callID string) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	return last.Role == string(tagentmodel.RoleTool) && last.ToolCallID == callID
}

// smokeFrame 报告用户消息是不是循环折成的工具结果帧。
func smokeFrame(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, "{") && strings.Contains(content, "call-") ||
		smokeCredentialFrame(content) || strings.HasPrefix(content, "[deliver_summary_")
}

// smokeCredentialFrame 报告用户消息是不是 send 成功回执折成的帧：回执是凭据
// 不是消息，正文里没有可识别的字样，判据只能是整条正文就是这份凭据本身。
func smokeCredentialFrame(content string) bool {
	return strings.TrimSpace(content) == smokeSendCredential
}

// smokeSendCredential 是 send 成功回执的正文：模型收到的只该是这份凭据，目标会话
// 与送达字样都不进材料。
const smokeSendCredential = "ok"

// smokeMarkerIn 报告请求里最近一条非帧用户消息携带的冒烟标记：工具结果帧
// （回执帧、send 凭据、[deliver_summary_ok]）自身不带标记，标记在它之前那条用户消息里。
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
	return bootSmokeRigWithPort(t, cfg, provider, gateway, nil, nil)
}

// bootSmokeRigWithPort 在 bootSmokeRigWith 之上允许调用方注入出站通道：03.11
// 的渠道失败场景要一个能按标记拒绝投递的包装器，记录仍落在同一个 fakeChannel
// 上。channel 为 nil 时新建一台；port 为 nil 时与 channel 完全一致。
func bootSmokeRigWithPort(t *testing.T, cfg *smokeConfig, provider *smokeProvider, gateway *approvalGateway, channel *fakeChannel, port wechatChannel) *smokeRig {
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
	if channel == nil {
		channel = newFakeChannel()
	}
	if port == nil {
		port = channel
	}
	options := append(app.options(), tagent.WithApprovals(gateway.Manager()))
	if cfg.DeliveryRuler != nil {
		options = append(options, tagent.WithDeliveryRuler(cfg.DeliveryRuler))
	}
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
	gateway.SetSender(port)
	app.typing = bindChannel(app.host, port, appCfg.App.Wechat)
	registerHandlers(app.host, port, gateway, appCfg.App.Wechat, app.typing)
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
	options := append(app.options(), tagent.WithApprovals(gateway.Manager()))
	if r.cfg.DeliveryRuler != nil {
		options = append(options, tagent.WithDeliveryRuler(r.cfg.DeliveryRuler))
	}
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

// smokeReviewRuler 是投递审核的台架实现：正文以任一 holdPrefix 开头时挂起，
// 其余放行；每条裁决都记下，供断言“投递确实经过了审核出口”。
type smokeReviewRuler struct {
	mu         sync.Mutex
	holdPrefix []string
	seen       []plugin.Delivery
}

// Rule 记下一条投递并给出脚本裁决。
func (r *smokeReviewRuler) Rule(_ context.Context, delivery plugin.Delivery) (plugin.Disposition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, delivery)
	for _, prefix := range r.holdPrefix {
		if strings.HasPrefix(delivery.Content, prefix) {
			return plugin.DispositionHold, nil
		}
	}
	return plugin.DispositionAllow, nil
}

// seenDeliveries 报告裁决过的投递，按裁决顺序。
func (r *smokeReviewRuler) seenDeliveries() []plugin.Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]plugin.Delivery(nil), r.seen...)
}

// sawContent 报告是否有一次裁决的正文以给定前缀开头。
func (r *smokeReviewRuler) sawContent(prefix string) bool {
	for _, delivery := range r.seenDeliveries() {
		if strings.HasPrefix(delivery.Content, prefix) {
			return true
		}
	}
	return false
}

// waitHeldDelivery 等到该会话挂起一条正文以 want 开头的投递，把它交回；超时即
// 失败并列出当前挂起表。
func (r *smokeRig) waitHeldDelivery(chat, want string, timeout time.Duration) tagent.HeldDelivery {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	target := r.addressOf(chat)
	for {
		for _, held := range r.host.HeldDeliveries() {
			if held.Delivery.Target == target && strings.HasPrefix(held.Delivery.Content, want) {
				return held
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("no held delivery to %s starts with %q within %s; held = %+v", chat, want, timeout, r.host.HeldDeliveries())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// approveDelivery 批准一条挂起投递，并要求它在超时内到达渠道。
func (r *smokeRig) approveDelivery(ctx context.Context, held tagent.HeldDelivery) {
	r.t.Helper()
	if err := r.host.ApproveDelivery(ctx, held.ID); err != nil {
		r.t.Fatalf("approve the held delivery %s: %v", held.ID, err)
	}
}

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
	return r.postTaskSession(text, "")
}

// postTaskSession 发一次可选携带独立会话标签的注入：空标签维持
// “驱动最近 admit 会话”的旧语义，非空标签驱动 injected 会话。
func (r *smokeRig) postTaskSession(text, session string) (int, string) {
	r.t.Helper()
	payload, err := json.Marshal(map[string]any{
		"messages":       []map[string]string{{"role": "user", "content": text}},
		"trigger_source": "user",
		"session":        session,
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
	if err := r.driveResult(chat, text); err != nil {
		r.t.Fatalf("drive the session %s: %v", chat, err)
	}
	r.lastChat = chat
}

// driveResult 是 drive 的可错误返回版本：故障注入与并发批次要拿到错误而不
// 结束测试进程，并且不能在并发时写 lastChat（那是 route 的单线程状态）。
func (r *smokeRig) driveResult(chat, text string) error {
	_, err := r.app.host.Drive(context.Background(), inboundMessage{
		ChatID: chat, UserName: chat, Text: text,
	})
	return err
}

// awaitText 是 waitText 的可错误返回版本：并发批次要用它把每一路的超时交回
// 调用方，而不是在 goroutine 里 t.Fatalf。
func (r *smokeRig) awaitText(chat, want string, timeout time.Duration) (outboundEvent, error) {
	deadline := time.Now().Add(timeout)
	for {
		for _, evt := range r.recorded() {
			if evt.Kind == "text" && evt.Target == chat && strings.Contains(evt.Body, want) {
				return evt, nil
			}
		}
		if time.Now().After(deadline) {
			return outboundEvent{}, fmt.Errorf("no text to %s carrying %q after %s (recorded=%d)", chat, want, timeout, len(r.recorded()))
		}
		time.Sleep(2 * time.Millisecond)
	}
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
	line := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, line)
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

// mark 报告当前的日志下标：调用方用它把一次故障注入前后的日志切开，避免把
// 无关的旧日志算成该次故障的具名证据。
func (l *smokeLogRecorder) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

// countAnySince 报告 mark 之后含任一子串（大小写不敏感）的行数。一行只计一次，
// 避免同一条错误因多个关键词被重复计入。
func (l *smokeLogRecorder) countAnySince(mark int, substrings ...string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if mark < 0 {
		mark = 0
	}
	if mark > len(l.lines) {
		mark = len(l.lines)
	}
	n := 0
	for _, line := range l.lines[mark:] {
		lower := strings.ToLower(line)
		for _, substr := range substrings {
			if strings.Contains(lower, strings.ToLower(substr)) {
				n++
				break
			}
		}
	}
	return n
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

// smokeLogSwitch 是进程级的日志分发器：产品日志面只在进程内安装一次，测试通过切换当前记录器取日志。拆成父子测试后每个子测试都有自己的 cleanup；若 cleanup 直接改写 log.Default，就会与上一段宿主仍在收尾的提交 goroutine 竞争（产品在 commitFact 里读 log.Default）。分发器内部的换手由互斥锁保护，全局只写一次。
type smokeLogSwitch struct {
	mu  sync.Mutex
	cur *smokeLogRecorder
}

func (s *smokeLogSwitch) set(recorder *smokeLogRecorder) {
	s.mu.Lock()
	s.cur = recorder
	s.mu.Unlock()
}

func (s *smokeLogSwitch) clear(recorder *smokeLogRecorder) {
	s.mu.Lock()
	if s.cur == recorder {
		s.cur = nil
	}
	s.mu.Unlock()
}

func (s *smokeLogSwitch) add(format string, args ...any) {
	s.mu.Lock()
	recorder := s.cur
	s.mu.Unlock()
	if recorder != nil {
		recorder.add(format, args...)
	}
}

// Debug implements log.Logger.
func (s *smokeLogSwitch) Debug(args ...any) { s.add("%s", fmt.Sprint(args...)) }

// Debugf implements log.Logger.
func (s *smokeLogSwitch) Debugf(format string, args ...any) { s.add(format, args...) }

// Info implements log.Logger.
func (s *smokeLogSwitch) Info(args ...any) { s.add("%s", fmt.Sprint(args...)) }

// Infof implements log.Logger.
func (s *smokeLogSwitch) Infof(format string, args ...any) { s.add(format, args...) }

// Warn implements log.Logger.
func (s *smokeLogSwitch) Warn(args ...any) { s.add("%s", fmt.Sprint(args...)) }

// Warnf implements log.Logger.
func (s *smokeLogSwitch) Warnf(format string, args ...any) { s.add(format, args...) }

// Error implements log.Logger.
func (s *smokeLogSwitch) Error(args ...any) { s.add("%s", fmt.Sprint(args...)) }

// Errorf implements log.Logger.
func (s *smokeLogSwitch) Errorf(format string, args ...any) { s.add(format, args...) }

// Fatal implements log.Logger without ending the process: the smoke keeps running.
func (s *smokeLogSwitch) Fatal(args ...any) { s.add("%s", fmt.Sprint(args...)) }

// Fatalf implements log.Logger without ending the process.
func (s *smokeLogSwitch) Fatalf(format string, args ...any) { s.add(format, args...) }

var (
	smokeLogs    smokeLogSwitch
	smokeLogOnce sync.Once
)

// installSmokeLog 把 trpc 日志面换成进程级分发器，并把当前记录器切到本次测试；测试结束时只摘掉自己的记录器，不改写全局日志面。
func installSmokeLog(t *testing.T) *smokeLogRecorder {
	t.Helper()
	recorder := &smokeLogRecorder{}
	smokeLogOnce.Do(func() {
		log.Default = &smokeLogs
		log.ContextDefault = &smokeLogs
	})
	smokeLogs.set(recorder)
	t.Cleanup(func() { smokeLogs.clear(recorder) })
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

// TestEventIntegrityHostAdapters drives one inbound channel message through the formal assembly and one entryless declaration:
// - formal_channel_turn runs loadWechatConfig -> newWechatApp -> tagent.Boot -> bindChannel -> registerHandlers
// - it checks the reply, the target session and the request identity
// - missing_entry_fails_closed checks an empty declared identity is refused by name, drives no model and binds no default address
func TestEventIntegrityHostAdapters(t *testing.T) {
	t.Run("formal_channel_turn", smokeIntegrityFormalChannelTurn)
	t.Run("missing_entry_fails_closed", smokeIntegrityMissingEntryFailsClosed)
}

const (
	// smokeIntegrityHostTurn 是本叶渠道回合的轮次标记：它进用户正文，也决定脚本答复。
	smokeIntegrityHostTurn = "SMOKE-R9001"
	// smokeIntegrityHostEmptyTurn 是缺身份负锚的正文：它绝不该被任何会话驱动。
	smokeIntegrityHostEmptyTurn = "SMOKE-R9002"
)

// smokeIntegrityFormalChannelTurn 只经渠道入站钩子驱动一轮：registerHandlers 注册的
// 钩子把消息交给 hostRuntime.Drive，Host.Invoke 产出模型请求，最终答复经 hostSender
// 回到同一会话。测试不手工 Bind、不手工 CommitToSession，装配全部来自产品入口。
func smokeIntegrityFormalChannelTurn(t *testing.T) {
	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, provider := host.rig, host.provider
	chat := smokeChats[0]
	inbound := smokeIntegrityHostTurn + " plain question"

	if err := rig.channel.emit(context.Background(), textMessage(chat, inbound)); err != nil {
		t.Fatalf("emit the inbound channel message: %v", err)
	}
	reply := rig.waitText(chat, "answer "+smokeIntegrityHostTurn, 120*time.Second)

	if got := smokeErrorsTextCount(rig, chat, "answer "+smokeIntegrityHostTurn); got != 1 {
		t.Fatalf("the channel received %d answer(s) for %s, want exactly one\nrecent outbound:\n%s",
			got, smokeIntegrityHostTurn, rig.recentOutbound(8))
	}
	if reply.Target != chat {
		t.Fatalf("the answer target = %q, want %q", reply.Target, chat)
	}
	for _, evt := range rig.recorded() {
		if evt.Kind == "text" && evt.Target != chat && strings.Contains(evt.Body, smokeIntegrityHostTurn) {
			t.Fatalf("the answer leaked to %q: %q", evt.Target, evt.Body)
		}
	}

	addr := rig.addressOf(chat)
	if target, ok := rig.app.host.router.Target(addr); !ok || target != chat {
		t.Fatalf("router target of %+v = (%q, %t), want %q", addr, target, ok, chat)
	}
	if bound := rig.app.host.router.BoundAddresses(event.AgentID(smokeSessionTag)); len(bound) != 1 || bound[0] != addr {
		t.Fatalf("bound addresses = %+v, want only %+v", bound, addr)
	}

	requests := smokeIntegrityRequestsForLastUser(provider, smokeEntryModel, smokeIntegrityHostTurn)
	if len(requests) != 1 {
		t.Fatalf("the turn drove %d entry request(s) carrying %s, want exactly one\nrecent requests:\n%s",
			len(requests), smokeIntegrityHostTurn, rig.recentRequests(8))
	}
	if got := strings.TrimSpace(smokeLastUserText(requests[0].Messages)); got != inbound {
		t.Fatalf("the request's last user text = %q, want the inbound channel body %q", got, inbound)
	}
	if got := strings.Count(requests[0].text(), smokeIntegrityHostTurn); got != 1 {
		t.Fatalf("the request carried %s %d time(s), want exactly one input", smokeIntegrityHostTurn, got)
	}

	inputs := 0
	for _, evt := range rig.sessionFacts(chat) {
		if evt.Type == event.TypeExternalInput && strings.Contains(evt.Message.Content, smokeIntegrityHostTurn) {
			inputs++
		}
	}
	if inputs != 1 {
		t.Fatalf("the address %+v carries %d external_input fact(s) for %s, want exactly one",
			addr, inputs, smokeIntegrityHostTurn)
	}
	t.Logf("[integrity-host-adapters] channel(%s) -> address(%s) -> one entry request -> one reply: %q",
		chat, addr.SessionID, reply.Body)
}

// smokeIntegrityMissingEntryFailsClosed 判定缺身份的声明在装配期具名拒绝：不写 entry
// 的文档经 loadWechatConfig 读成空身份，newWechatApp 必须返回 ErrNoChatAddress 而不是
// 凭空造一个默认会址；活宿主上空会话身份的 Drive 同样具名拒绝，不驱动模型、不产生
// 外发、不落默认绑定。
func smokeIntegrityMissingEntryFailsClosed(t *testing.T) {
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	if err := os.WriteFile(cfg.ConfigPath, []byte(smokeIntegrityEntrylessDoc(cfg)), 0o644); err != nil {
		t.Fatalf("write the entryless document: %v", err)
	}
	loaded, err := loadWechatConfig(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("load the entryless document: %v", err)
	}
	if strings.TrimSpace(loaded.Entry) != "" {
		t.Fatalf("the entryless document loaded entry %q, want none", loaded.Entry)
	}
	gateway := newApprovalGateway(loaded.Governance, loaded.App.Wechat.Approvers)
	app, err := newWechatApp(loaded, gateway)
	if app != nil {
		t.Fatalf("newWechatApp assembled an application from an entryless declaration: %+v", app)
	}
	if !errors.Is(err, ErrNoChatAddress) {
		t.Fatalf("newWechatApp over an entryless declaration = %v, want ErrNoChatAddress", err)
	}
	if got := provider.count(); got != 0 {
		t.Fatalf("the refused assembly drove %d model request(s), want none", got)
	}

	host := bootSmokeIntegrityHost(t, "1h", "")
	defer host.close(t)
	rig, liveProvider := host.rig, host.provider
	beforeRequests, beforeOutbound := liveProvider.count(), len(rig.recorded())
	if _, err := rig.app.host.Drive(context.Background(), inboundMessage{
		ChatID: "   ", UserName: "   ", Text: smokeIntegrityHostEmptyTurn,
	}); !errors.Is(err, ErrNoChatAddress) {
		t.Fatalf("a drive with no chat identity = %v, want ErrNoChatAddress", err)
	}
	if got := liveProvider.count(); got != beforeRequests {
		t.Fatalf("the refused identity drove %d model request(s), want none", got-beforeRequests)
	}
	if got := len(rig.recorded()); got != beforeOutbound {
		t.Fatalf("the refused identity produced %d outbound event(s), want none", got-beforeOutbound)
	}
	if bound := rig.app.host.router.BoundAddresses(event.AgentID(smokeSessionTag)); len(bound) != 0 {
		t.Fatalf("the refused identity bound %+v, want no default address", bound)
	}
	t.Logf("[integrity-host-adapters] entryless declaration -> ErrNoChatAddress; empty chat identity -> ErrNoChatAddress with no model request, no outbound and no binding")
}

// smokeIntegrityEntrylessDoc 渲染一份不声明 entry 的宿主文档：身份缺失只能被装配
// 拒绝，不能被某个默认会话兜底。
func smokeIntegrityEntrylessDoc(c *smokeConfig) string {
	return fmt.Sprintf(`app:
  wechat:
    config_dir: %s
    token_file: %s
    context_token_dir: %s
    workspace_dir: %s
`,
		c.ConfigDir, filepath.Join(c.ConfigDir, "token.json"), c.ContextDir, c.WorkspaceDir)
}

// smokeCalibrationModeEnv, smokeCalibrationPlanEnv, smokeCalibrationGenEnv, smokeCalibrationReportEnv, smokeCalibrationRunEnv, smokeCalibrationBaselineEnv and smokeCalibrationColdEnv name the calibration environment; smokeCalibrationModeHost and smokeCalibrationModeScenario split the binary into host child and baseline scenario.
//
// The calibration parent orchestrates and measures real out-of-process hosts (the same test binary, TAGENT_HTTP_PORT=0); a restart is SIGKILL and spawn, and the model endpoint stays in the parent.
const (
	smokeCalibrationModeEnv     = "TAGENT_CALIBRATION_MODE"
	smokeCalibrationPlanEnv     = "TAGENT_CALIBRATION_PLAN"
	smokeCalibrationGenEnv      = "TAGENT_CALIBRATION_GENERATION"
	smokeCalibrationReportEnv   = "TAGENT_CALIBRATION_REPORT"
	smokeCalibrationRunEnv      = "TAGENT_CALIBRATION_RUN_DIR"
	smokeCalibrationBaselineEnv = "TAGENT_CALIBRATION_BASELINE"
	smokeCalibrationColdEnv     = "TAGENT_CALIBRATION_COLD_BUILD_MS"

	smokeCalibrationModeHost     = "host"
	smokeCalibrationModeScenario = "scenario"

	// smokeCalibrationLeaf 是报告里的叶标识：03.10 校准。
	smokeCalibrationLeaf = "03.10"

	// smokeCalibrationSessions, smokeCalibrationInputsPerSession, smokeCalibrationRestartsPerSession and smokeCalibrationRestartEvery shrink the 03.11 shape to four sessions, twenty inputs and two restarts.
	smokeCalibrationSessions           = 4
	smokeCalibrationInputsPerSession   = 20
	smokeCalibrationRestartsPerSession = 2
	smokeCalibrationRestartEvery       = 10

	// smokeCalibrationFullInputs and smokeCalibrationFullRestarts are the full single-combination scale: 1200 inputs and 120 restarts.
	smokeCalibrationFullInputs   = 1200
	smokeCalibrationFullRestarts = 120

	// smokeCalibrationBudgetMargin and smokeCalibrationP95MinSamples are the budget margin and the minimum p95 sample count.
	smokeCalibrationBudgetMargin  = 1.5
	smokeCalibrationP95MinSamples = 30

	// smokeCalibrationMatrixCombos, smokeCalibrationMinGoTestSeconds and smokeCalibrationJobSetupMinutes size the long job over the six combinations (3 seeds x 2 windows).
	smokeCalibrationMatrixCombos     = 6
	smokeCalibrationMinGoTestSeconds = 60
	smokeCalibrationJobSetupMinutes  = 5

	// smokeCalibrationCandidateShort, smokeCalibrationCandidateLong and smokeCalibrationCandidateMutations are the 05.1 candidate timeouts in minutes; this calibration judges feasibility only.
	smokeCalibrationCandidateShort     = 20
	smokeCalibrationCandidateLong      = 45
	smokeCalibrationCandidateMutations = 30

	// smokeCalibrationReviewNote 必须进报告：外推值在 03.11 全量首轮后复核。
	smokeCalibrationReviewNote = "外推值，03.11 全量首轮后复核"

	smokeCalibrationChildTimeout      = 15 * time.Minute
	smokeCalibrationReadyTimeout      = 5 * time.Minute
	smokeCalibrationGenerationTimeout = 10 * time.Minute
	smokeCalibrationColdBuildTimeout  = 20 * time.Minute
)

const (
	// smokeLongLeaf, smokeLongSessions, smokeLongInputsPerSession, smokeLongRestartsPerSession and smokeLongRestartEvery are the frozen 03.11 scale: four sessions, 300 inputs and 30 restarts each; six combinations total 7200 inputs and 720 restarts.
	smokeLongLeaf               = "03.11"
	smokeLongSessions           = 4
	smokeLongInputsPerSession   = 300
	smokeLongRestartsPerSession = 30
	smokeLongRestartEvery       = 10

	// smokeLongDelegateEvery 让每会话每 25 个输入里有一个委派输入：它派生一次
	// worker 调用与一次结算通知回合，通知身份因此有真实材料可对账。
	smokeLongDelegateEvery = 25

	// smokeLongStorageFaultRound, smokeLongModelFaultRound and smokeLongChannelFaultRound mount the three faults on fixed rounds of one generation.
	smokeLongStorageFaultRound = 24
	smokeLongModelFaultRound   = 55
	smokeLongChannelFaultRound = 85

	// smokeLongReloadGens 是替换与压缩交错的三个替换点：小窗口持续折叠，替换
	// 夹在折叠之间，验证两件事互不吞掉输入。
	smokeLongReloadGens  = 3
	smokeLongReloadEvery = 30

	// smokeLongConcurrentRoundFirst and smokeLongConcurrentRoundStep place the rounds all four sessions complete together.
	smokeLongConcurrentRoundFirst = 150
	smokeLongConcurrentRoundStep  = 100

	// smokeLongFaultStorage, smokeLongFaultModel and smokeLongFaultChannel name the three fault kinds of 03.11.
	smokeLongFaultStorage = "storage_deny"
	smokeLongFaultModel   = "model_fail"
	smokeLongFaultChannel = "channel_fail"

	// smokeLongChildTest is the child-process test name of 03.11.
	smokeLongChildTest = "TestEventIntegrityLongRun"

	// smokeLongRebindPrefix 是每代四会话重绑输入的标记前缀：与 03.10 的
	// SMOKE-R86 分开，父进程按前缀单独核账，不混进 1200 个业务输入。
	smokeLongRebindPrefix = "SMOKE-R87"
)

// smokeCalibrationChats 是 4 个共享同一台 Host 的会话。
var smokeCalibrationChats = []string{"smoke-chat-a", "smoke-chat-b", "smoke-chat-c", "smoke-chat-d"}

// smokeCalibrationStep 是时间线上的一步：一次输入，或一次重启触发点。
// 重启步骤归它结束的那一代，Generation 是即将被杀死的那一代。
type smokeCalibrationStep struct {
	Kind       string `json:"kind"`
	Generation int    `json:"generation"`
	Session    string `json:"session"`
	Index      int    `json:"index,omitempty"`
	Marker     string `json:"marker,omitempty"`
	Text       string `json:"text,omitempty"`
	// Shape 是输入的业务形状：空值等价 plain（03.10 的缩样时间线）；03.11 用
	// plain/delegate 两档，delegate 会派生一次 worker 调用与一次结算通知回合。
	Shape string `json:"shape,omitempty"`
}

// smokeCalibrationPlan 是父测试写给宿主子进程的完整计划：路径、窗口、时间线、
// 报告落点。子进程只读它，规模由父测试决定。
type smokeCalibrationPlan struct {
	Leaf               string                 `json:"leaf"`
	Window             int                    `json:"window"`
	Seed               int                    `json:"seed"`
	Root               string                 `json:"root"`
	WorkspaceDir       string                 `json:"workspace_dir"`
	ConfigDir          string                 `json:"config_dir"`
	ContextDir         string                 `json:"context_dir"`
	ApprovalDir        string                 `json:"approval_dir"`
	PromptDir          string                 `json:"prompt_dir"`
	StorageRoot        string                 `json:"storage_root"`
	ConfigPath         string                 `json:"config_path"`
	Endpoint           string                 `json:"endpoint"`
	Sessions           []string               `json:"sessions"`
	InputsPerSession   int                    `json:"inputs_per_session"`
	RestartsPerSession int                    `json:"restarts_per_session"`
	RestartEvery       int                    `json:"restart_every"`
	Timeline           []smokeCalibrationStep `json:"timeline"`
	ReportDir          string                 `json:"report_dir"`

	// ChildTest 是子进程要跑的父测试名（不含 ^$）；空值沿用 03.10 校准。
	ChildTest string `json:"child_test,omitempty"`
	// ConsumerSession 的每代首个输入经正式 /task 面驱动（03.11 消费者锚）。
	ConsumerSession string `json:"consumer_session,omitempty"`
	// Faults 是按输入步挂载的故障注入（03.11 负锚），子进程驱动该步时执行。
	Faults []smokeLongFault `json:"faults,omitempty"`
	// Reloads 是按代挂载的正式 /reload 替换点（03.11 替换与压缩交错）。
	Reloads []smokeLongReload `json:"reloads,omitempty"`
	// ConcurrentRounds 是四会话同时完成的轮次（03.11 负锚）。
	ConcurrentRounds []int `json:"concurrent_rounds,omitempty"`
}

// inputs 报告计划里的输入步数。
func (p smokeCalibrationPlan) inputs() int {
	n := 0
	for _, step := range p.Timeline {
		if step.Kind == "input" {
			n++
		}
	}
	return n
}

// restarts 报告计划里的重启触发次数（= 子进程代数 - 1）。
func (p smokeCalibrationPlan) restarts() int {
	n := 0
	for _, step := range p.Timeline {
		if step.Kind == "restart" {
			n++
		}
	}
	return n
}

// smokeCalibrationTimeline 造缩样时间线：4 个会话按轮次交错，每会话每
// restartEvery 个输入之后轮流触发一次重启（每会话 restartsPerSession 次）。
// 重启步骤紧跟触发会话的那次输入，下一代宿主从下一步继续。
func smokeCalibrationTimeline(inputsPerSession, restartsPerSession, restartEvery int, sessions []string) []smokeCalibrationStep {
	steps := make([]smokeCalibrationStep, 0, inputsPerSession*len(sessions)+restartsPerSession*len(sessions))
	triggered := make(map[string]int, len(sessions))
	gen, seq := 0, 0
	for round := 0; round < inputsPerSession; round++ {
		for _, chat := range sessions {
			marker := fmt.Sprintf("SMOKE-R85%03d", seq)
			steps = append(steps, smokeCalibrationStep{
				Kind: "input", Generation: gen, Session: chat, Index: round,
				Marker: marker, Text: marker + " plain question",
			})
			seq++
			if restartEvery <= 0 || (round+1)%restartEvery != 0 {
				continue
			}
			if triggered[chat] >= restartsPerSession {
				continue
			}
			triggered[chat]++
			steps = append(steps, smokeCalibrationStep{Kind: "restart", Generation: gen, Session: chat})
			gen++
		}
	}
	return steps
}

// smokeCalibrationInputSample 是一次输入的实测：Drive 同步跑到回合终局，
// 再加外发到达的等待。
type smokeCalibrationInputSample struct {
	Generation int     `json:"generation"`
	Session    string  `json:"session"`
	Index      int     `json:"index"`
	Marker     string  `json:"marker"`
	MS         float64 `json:"ms"`
}

// smokeCalibrationRebindSample 是一代宿主里一次会话重新绑定的实测：重启后
// 四个会话都要经生产 Drive 重新准入，才能继续驱动时间线。
type smokeCalibrationRebindSample struct {
	Generation int     `json:"generation"`
	Session    string  `json:"session"`
	Marker     string  `json:"marker"`
	MS         float64 `json:"ms"`
}

// smokeCalibrationSelfUsage 是子进程自己报告的资源峰值（rusage + 运行指标）。
type smokeCalibrationSelfUsage struct {
	RSSBytes       int64  `json:"rss_bytes"`
	CPUMillis      int64  `json:"cpu_millis"`
	OpenFDs        int    `json:"open_fds"`
	Goroutines     int    `json:"goroutines"`
	HeapInuseBytes uint64 `json:"heap_inuse_bytes"`
}

// smokeCalibrationGenerationReport 是一代宿主子进程交回的报告：它自己测的
// 启机/重绑/逐输入成本，以及恢复任务的数目。
type smokeCalibrationGenerationReport struct {
	Leaf       string `json:"leaf"`
	Generation int    `json:"generation"`
	Window     int    `json:"window"`
	// Phase is one of restart_point or done.
	Phase          string                         `json:"phase"`
	TriggerSession string                         `json:"trigger_session,omitempty"`
	BootMS         float64                        `json:"boot_ms"`
	RecoveredTasks int                            `json:"recovered_tasks"`
	RestoredFacts  int                            `json:"restored_facts"`
	Rebinds        []smokeCalibrationRebindSample `json:"rebinds"`
	Inputs         []smokeCalibrationInputSample  `json:"inputs"`
	CloseErr       string                         `json:"close_error,omitempty"`
	Failure        string                         `json:"failure,omitempty"`
	Self           smokeCalibrationSelfUsage      `json:"self"`
	WrittenAt      string                         `json:"written_at"`

	// Outbound, Faults, Reloads, Consumer, Folds, Concurrent, Drain, Logs and SettleAck are the additional 03.11 observations; the 03.10 shrink leaves them empty.
	Outbound   []smokeLongOutbound        `json:"outbound,omitempty"`
	Faults     []smokeLongFaultOutcome    `json:"faults,omitempty"`
	Reloads    []smokeLongReloadOutcome   `json:"reloads,omitempty"`
	Consumer   []smokeLongConsumerTurn    `json:"consumer_turns,omitempty"`
	Folds      []smokeLongFoldSample      `json:"folds,omitempty"`
	Concurrent []smokeLongConcurrentBatch `json:"concurrent,omitempty"`
	Drain      *smokeLongDrainReport      `json:"drain,omitempty"`
	Logs       smokeLongLogCounts         `json:"logs,omitempty"`
	SettleAck  int                        `json:"settle_acks,omitempty"`
}

// smokeCalibrationRestartSample 是一次重启的父侧实测：kill 到下一代 ready
// 的墙上时间，配合子进程自报的启机/重绑拆分。
type smokeCalibrationRestartSample struct {
	Generation     int     `json:"generation"`
	Trigger        string  `json:"trigger_session"`
	KillToReadyMS  float64 `json:"kill_to_ready_ms"`
	SpawnToReadyMS float64 `json:"spawn_to_ready_ms"`
	BootMS         float64 `json:"boot_ms"`
	RebindMS       float64 `json:"rebind_ms"`
	RecoveredTasks int     `json:"recovered_tasks"`
	Rebound        int     `json:"rebound_sessions"`
	RestoredInputs int     `json:"restored_context_inputs"`
}

// smokeCalibrationRace 记录同场景 race 与非 race 的对比。
type smokeCalibrationRace struct {
	Enabled            bool    `json:"enabled"`
	BaselineWallMS     float64 `json:"baseline_wall_ms"`
	BaselinePerInputMS float64 `json:"baseline_per_input_ms"`
	BaselineInputs     int     `json:"baseline_input_samples"`
	BaselineBuildMS    float64 `json:"baseline_build_ms"`
	OverheadFactor     float64 `json:"overhead_factor"`
	PerInputFactor     float64 `json:"per_input_overhead_factor"`
	Source             string  `json:"source"`
}

// smokeCalibrationStorage 是存储增长的前后账。
type smokeCalibrationStorage struct {
	Root        string   `json:"root"`
	BeforeBytes int64    `json:"before_bytes"`
	AfterBytes  int64    `json:"after_bytes"`
	GrowthBytes int64    `json:"growth_bytes"`
	BeforeFiles int      `json:"before_files"`
	AfterFiles  int      `json:"after_files"`
	FilesAdded  int      `json:"files_added"`
	Problems    []string `json:"problems,omitempty"`
}

// smokeCalibrationPeak 是峰值资源：子进程 rusage 的最大值，加上父进程自己。
type smokeCalibrationPeak struct {
	Children            int    `json:"children"`
	MaxChildRSSBytes    int64  `json:"max_child_rss_bytes"`
	MaxChildCPUMillis   int64  `json:"max_child_cpu_millis"`
	TotalChildCPUMillis int64  `json:"total_child_cpu_millis"`
	MaxChildOpenFDs     int    `json:"max_child_open_fds"`
	MaxChildGoroutines  int    `json:"max_child_goroutines"`
	MaxChildHeapBytes   uint64 `json:"max_child_heap_inuse_bytes"`
	ParentRSSBytes      int64  `json:"parent_rss_bytes"`
	ParentCPUMillis     int64  `json:"parent_cpu_millis"`
	ParentGoroutines    int    `json:"parent_goroutines"`
}

// smokeCalibrationMeasurements 是六项成本的实测值。
type smokeCalibrationMeasurements struct {
	ColdBuildMS      float64                         `json:"cold_build_ms"`
	ColdBuildSource  string                          `json:"cold_build_source"`
	InitialBootMS    float64                         `json:"initial_boot_ms"`
	HotRunWallMS     float64                         `json:"hot_run_wall_ms"`
	HotRunSource     string                          `json:"hot_run_source"`
	PerInputMS       float64                         `json:"per_input_ms"`
	PerInputMedianMS float64                         `json:"per_input_median_ms"`
	PerInputP95MS    float64                         `json:"per_input_p95_ms"`
	InputSamples     int                             `json:"input_samples"`
	InputDetail      []smokeCalibrationInputSample   `json:"input_samples_detail"`
	PerRestartMS     float64                         `json:"per_restart_ms"`
	RestartDetail    []smokeCalibrationRestartSample `json:"restart_samples_detail"`
	RebindSamples    int                             `json:"rebind_samples"`
	RebindMeanMS     float64                         `json:"rebind_mean_ms"`
	Race             smokeCalibrationRace            `json:"race"`
	Storage          smokeCalibrationStorage         `json:"storage"`
	Peak             smokeCalibrationPeak            `json:"peak"`
}

// smokeCalibrationExtrapolation 是缩样到 03.11 单组合（1200/120）的外推。
type smokeCalibrationExtrapolation struct {
	Note                      string  `json:"note"`
	Formula                   string  `json:"formula"`
	InputGrowthFactor         float64 `json:"input_growth_factor"`
	RestartGrowthFactor       float64 `json:"restart_growth_factor"`
	FullWallMS                float64 `json:"full_wall_ms"`
	FullWallMinutes           float64 `json:"full_wall_minutes"`
	ContextSlopeMS            float64 `json:"context_slope_ms_per_turn"`
	ContextAdjustedFullWallMS float64 `json:"context_adjusted_full_wall_ms"`
	FullStorageBytes          int64   `json:"full_storage_bytes"`
	FullRSSBytesLinear        int64   `json:"full_rss_bytes_linear"`
	FullRSSBytesBounded       int64   `json:"full_rss_bytes_bounded"`
	RSSModel                  string  `json:"rss_model"`
	FullCPUMillis             int64   `json:"full_cpu_millis"`
}

// smokeCalibrationCandidates 是 05.1 的候选超时（分钟），只记录不冻结。
type smokeCalibrationCandidates struct {
	ShortMinutes     int `json:"short_minutes"`
	LongMinutes      int `json:"long_minutes"`
	MutationsMinutes int `json:"mutations_minutes"`
}

// smokeCalibrationRecommended 是校准给出的建议值：long 用实测外推，short/
// mutations 标为本内核未测（沿用候选，待各自校准）。
type smokeCalibrationRecommended struct {
	LongJobTimeoutMinutes      int `json:"long_job_timeout_minutes"`
	GoTestTimeoutSeconds       int `json:"go_test_timeout_seconds_per_long_shard"`
	ShortJobTimeoutMinutes     int `json:"short_job_timeout_minutes"`
	MutationsJobTimeoutMinutes int `json:"mutations_job_timeout_minutes"`
}

// smokeCalibrationFeasibility 是一项候选超时的可行性判断。
type smokeCalibrationFeasibility struct {
	Surface          string `json:"surface"`
	CandidateMinutes int    `json:"candidate_minutes"`
	Measured         bool   `json:"measured"`
	// Verdict is one of feasible, infeasible or provisional_not_measured.
	Verdict         string  `json:"verdict"`
	RequiredMinutes float64 `json:"required_minutes"`
	// RequiredPerShardMinutes 是单组合（1200 输入/120 重启）的实测外推，
	// RequiredMinutes 是 03.11 long 作业的 6 组合口径；可行性按后者判断。
	RequiredPerShardMinutes float64 `json:"required_per_shard_minutes"`
	RecommendedMinutes      int     `json:"recommended_minutes"`
	Reason                  string  `json:"reason"`
}

// smokeCalibrationP95 声明本次 p95 的样本基础：少于 30 个样本不得宣称 p95。
type smokeCalibrationP95 struct {
	Claimed    bool    `json:"claimed"`
	Samples    int     `json:"samples"`
	PerInputMS float64 `json:"per_input_ms"`
	Note       string  `json:"note"`
}

// smokeCalibrationBudget 是报告被 05.1 消费的预算字段：同一份 JSON，不另建
// 第二份预算表。
type smokeCalibrationBudget struct {
	Source             string                        `json:"source"`
	Margin             float64                       `json:"margin"`
	Candidates         smokeCalibrationCandidates    `json:"candidates"`
	Recommended        smokeCalibrationRecommended   `json:"recommended"`
	Feasibility        []smokeCalibrationFeasibility `json:"feasibility"`
	OverBudget         bool                          `json:"over_budget"`
	OverBudgetReported bool                          `json:"over_budget_reported"`
	P95                smokeCalibrationP95           `json:"p95"`
	Notes              []string                      `json:"notes"`
}

// smokeCalibrationReconciliation 是跨重启的请求对账：每个输入标记只有一次
// 入口请求，每个重绑标记也只有一次。
type smokeCalibrationReconciliation struct {
	ExpectedInputs        int      `json:"expected_inputs"`
	DrivenInputs          int      `json:"driven_inputs"`
	MissingInputs         []string `json:"missing_input_requests"`
	DuplicateInputs       []string `json:"duplicate_input_requests"`
	ExpectedRebinds       int      `json:"expected_rebinds"`
	RebindRequests        int      `json:"rebind_requests"`
	MissingRebinds        []string `json:"missing_rebind_requests"`
	ProviderRequests      int      `json:"provider_requests"`
	ReflectRequests       int      `json:"reflect_requests"`
	RestoredContextInputs int      `json:"restored_context_inputs"`
	RestoredContextChecks int      `json:"restored_context_checks"`
}

// smokeCalibrationRaw 指向原始日志与产物：证据先从 TMPDIR 读，再汇总进 evidence。
type smokeCalibrationRaw struct {
	RunDir      string   `json:"run_dir"`
	PlanPath    string   `json:"plan_path"`
	ReportPath  string   `json:"report_path"`
	ChildLogs   []string `json:"child_logs"`
	ColdBuild   string   `json:"cold_build_binary"`
	BaselineDir string   `json:"baseline_dir"`
}

// smokeCalibrationReport 是一次校准的完整报告。
type smokeCalibrationReport struct {
	Leaf           string                         `json:"leaf"`
	Kind           string                         `json:"kind"`
	Window         int                            `json:"window"`
	Seed           int                            `json:"seed"`
	RaceEnabled    bool                           `json:"race_enabled"`
	GeneratedAt    string                         `json:"generated_at"`
	Machine        smokeCalibrationMachine        `json:"machine"`
	Scale          smokeCalibrationScale          `json:"scale"`
	Measurements   smokeCalibrationMeasurements   `json:"measurements"`
	Extrapolation  smokeCalibrationExtrapolation  `json:"extrapolation"`
	Budget         smokeCalibrationBudget         `json:"budget"`
	Reconciliation smokeCalibrationReconciliation `json:"reconciliation"`
	Raw            smokeCalibrationRaw            `json:"raw"`
}

// smokeCalibrationMachine 记录机器与工具链，供跨机复核比对。
type smokeCalibrationMachine struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	NumCPU     int    `json:"num_cpu"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	GoVersion  string `json:"go_version"`
}

// smokeCalibrationScale 是本次缩样的口径与不变式。
type smokeCalibrationScale struct {
	Sessions           int  `json:"sessions"`
	InputsPerSession   int  `json:"inputs_per_session"`
	InputsTotal        int  `json:"inputs_total"`
	RestartsPerSession int  `json:"restarts_per_session"`
	RestartsTotal      int  `json:"restarts_total"`
	Generations        int  `json:"generations"`
	RestartEvery       int  `json:"restart_every_inputs"`
	SharedHost         bool `json:"shared_host"`
	MeditationEnabled  bool `json:"meditation_enabled"`
	FsyncDropped       bool `json:"fsync_dropped"`
}

// smokeCalibrationBaseline 是非 race 基线子进程交回的对比值。
type smokeCalibrationBaseline struct {
	Window          int     `json:"window"`
	Seed            int     `json:"seed"`
	WallMS          float64 `json:"wall_ms"`
	PerInputMS      float64 `json:"per_input_ms"`
	InputSamples    int     `json:"input_samples"`
	InputsTotal     int     `json:"inputs_total"`
	RaceEnabled     bool    `json:"race_enabled"`
	BaselineBuildMS float64 `json:"baseline_build_ms"`
}

// smokeCalibrationWriteJSON 原子落盘：先写临时文件再 rename，读侧不会读到半份。
func smokeCalibrationWriteJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// smokeCalibrationReadJSON 读一份原子报告。
func smokeCalibrationReadJSON(path string, value any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}

// smokeCalibrationAwaitFile 轮询到文件出现，回报观测到的时刻（父侧时钟）。
func smokeCalibrationAwaitFile(path string, timeout time.Duration) (time.Time, error) {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return time.Now(), nil
		}
		if time.Now().After(deadline) {
			return time.Time{}, fmt.Errorf("smoke calibration: %s did not appear within %s", path, timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// smokeCalibrationMS 把时长换成毫秒浮点。
func smokeCalibrationMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// smokeCalibrationEnv 返回一份去掉了重复键的环境：追加项覆盖旧值，保证子进程
// 看到的 GOCACHE 等变量只有一个。
func smokeCalibrationEnv(pairs ...string) []string {
	env := os.Environ()
	for i := 0; i < len(pairs); i += 2 {
		key := pairs[i]
		filtered := env[:0]
		for _, entry := range env {
			if strings.HasPrefix(entry, key+"=") {
				continue
			}
			filtered = append(filtered, entry)
		}
		env = append(filtered, key+"="+pairs[i+1])
	}
	return env
}

// smokeCalibrationWalk 统计一个目录的字节数与文件数（存储增长的前后账）。
func smokeCalibrationWalk(root string) (int64, int, []string) {
	var bytes int64
	files := 0
	problems := make([]string, 0, 2)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, err.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			problems = append(problems, infoErr.Error())
			return nil
		}
		bytes += info.Size()
		files++
		return nil
	})
	if err != nil {
		problems = append(problems, err.Error())
	}
	return bytes, files, problems
}

// smokeCalibrationOpenFDs 报告本进程当前打开的文件描述符数。
func smokeCalibrationOpenFDs() int {
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// smokeCalibrationReadSelfUsage 读自己的峰值 RSS/CPU 与即时运行指标。
func smokeCalibrationReadSelfUsage() smokeCalibrationSelfUsage {
	usage := smokeCalibrationSelfUsage{OpenFDs: smokeCalibrationOpenFDs(), Goroutines: runtime.NumGoroutine()}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	usage.HeapInuseBytes = stats.HeapInuse
	var raw syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &raw); err == nil {
		usage.RSSBytes = smokeCalibrationRSSBytes(int64(raw.Maxrss))
		usage.CPUMillis = smokeCalibrationCPUMillis(raw)
	}
	return usage
}

// smokeCalibrationRSSBytes 把 rusage 的 Maxrss 归一成字节：darwin 已经是字节，
// linux 是 KiB。
func smokeCalibrationRSSBytes(maxrss int64) int64 {
	if runtime.GOOS == "linux" {
		return maxrss << 10
	}
	return maxrss
}

// smokeCalibrationCPUMillis 把用户态 + 内核态 CPU 时间换成毫秒。
func smokeCalibrationCPUMillis(raw syscall.Rusage) int64 {
	user := int64(raw.Utime.Sec)*1000 + int64(raw.Utime.Usec)/1000
	system := int64(raw.Stime.Sec)*1000 + int64(raw.Stime.Usec)/1000
	return user + system
}

// smokeCalibrationProcessUsage 读已结束子进程的 rusage。
func smokeCalibrationProcessUsage(state *os.ProcessState) (rss int64, cpu int64) {
	if state == nil {
		return 0, 0
	}
	raw, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || raw == nil {
		return 0, 0
	}
	return smokeCalibrationRSSBytes(int64(raw.Maxrss)), smokeCalibrationCPUMillis(*raw)
}

// smokeCalibrationMean 报告均值；空样本得到 0。
func smokeCalibrationMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

// smokeCalibrationMedian 报告中位数。
func smokeCalibrationMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// smokeCalibrationP95Of 报告 p95（最近秩）；样本少于 30 时调用方不得宣称 p95。
func smokeCalibrationP95Of(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// smokeCalibrationSlope 用最小二乘拟合“每轮输入成本随会话内序号的斜率”。
// 1200 输入的 prompt 随历史增长，缩样的 20 轮斜率是预算模型的保守修正项。
func smokeCalibrationSlope(samples []smokeCalibrationInputSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for _, sample := range samples {
		x := float64(sample.Index)
		sumX += x
		sumY += sample.MS
		sumXY += x * sample.MS
		sumXX += x * x
	}
	n := float64(len(samples))
	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denominator
}

// smokeCalibrationRaceEnabled 报告当前测试二进制是否用 -race 构建。
func smokeCalibrationRaceEnabled() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "-race" {
			return setting.Value == "true"
		}
	}
	return false
}

// smokeCalibrationLogTail 取子进程日志末尾若干行，供失败时定位（子进程被
// SIGKILL 时 t.Fatalf 的输出可能没来得及刷进日志）。
func smokeCalibrationLogTail(path string, lines int) string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("(no log at %s: %v)", path, err)
	}
	all := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// smokeCalibrationHostChild 是宿主子进程的一代：真实 Boot、恢复、四个会话
// 重新绑定，再驱动本代时间线；遇到重启步骤就落盘报告并等父进程 SIGKILL。
func smokeCalibrationHostChild(t *testing.T) {
	t.Helper()
	planPath := strings.TrimSpace(os.Getenv(smokeCalibrationPlanEnv))
	if planPath == "" {
		t.Fatal("the calibration host child carries no plan path")
	}
	var plan smokeCalibrationPlan
	if err := smokeCalibrationReadJSON(planPath, &plan); err != nil {
		t.Fatalf("read the calibration plan %s: %v", planPath, err)
	}
	gen, err := strconv.Atoi(strings.TrimSpace(os.Getenv(smokeCalibrationGenEnv)))
	if err != nil {
		t.Fatalf("the calibration host child carries no generation: %v", err)
	}
	reportPath := strings.TrimSpace(os.Getenv(smokeCalibrationReportEnv))
	if reportPath == "" {
		t.Fatal("the calibration host child carries no report path")
	}
	cfg := &smokeConfig{
		Root: plan.Root, WorkspaceDir: plan.WorkspaceDir, ConfigDir: plan.ConfigDir,
		ContextDir: plan.ContextDir, ApprovalDir: plan.ApprovalDir, PromptDir: plan.PromptDir,
		StorageRoot: plan.StorageRoot, ConfigPath: plan.ConfigPath, Endpoint: plan.Endpoint,
		Model: smokeEntryModel, MaxTokens: plan.Window,
	}

	started := time.Now()
	rig := bootSmokeRig(t, cfg, nil)
	report := &smokeCalibrationGenerationReport{
		Leaf: smokeCalibrationLeaf, Generation: gen, Window: plan.Window,
		BootMS:         smokeCalibrationMS(time.Since(started)),
		RecoveredTasks: len(rig.host.Run().Recovery().Tasks),
		RestoredFacts:  rig.host.Run().CommittedFacts(),
	}

	for index, chat := range plan.Sessions {
		marker := fmt.Sprintf("SMOKE-R86%02d%d", gen, index)
		start := time.Now()
		rig.drive(chat, marker+" rebind "+chat)
		rig.waitText(chat, "answer "+marker, 60*time.Second)
		report.Rebinds = append(report.Rebinds, smokeCalibrationRebindSample{
			Generation: gen, Session: chat, Marker: marker,
			MS: smokeCalibrationMS(time.Since(start)),
		})
	}
	report.Self = smokeCalibrationReadSelfUsage()
	report.WrittenAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := smokeCalibrationWriteJSON(reportPath+".ready", report); err != nil {
		t.Fatalf("write the calibration ready report %s: %v", reportPath, err)
	}

	for _, step := range plan.Timeline {
		if step.Generation != gen {
			continue
		}
		if step.Kind == "restart" {
			report.TriggerSession = step.Session
			break
		}
		start := time.Now()
		rig.drive(step.Session, step.Text)
		rig.waitText(step.Session, "answer "+step.Marker, 60*time.Second)
		report.Inputs = append(report.Inputs, smokeCalibrationInputSample{
			Generation: gen, Session: step.Session, Index: step.Index,
			Marker: step.Marker, MS: smokeCalibrationMS(time.Since(start)),
		})
	}

	report.Self = smokeCalibrationReadSelfUsage()
	report.WrittenAt = time.Now().UTC().Format(time.RFC3339Nano)
	if report.TriggerSession != "" {
		report.Phase = "restart_point"
		if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
			t.Fatalf("write the restart point %s: %v", reportPath, err)
		}
		time.Sleep(10 * time.Minute)
		return
	}
	report.Phase = "done"
	if err := rig.close(); err != nil {
		report.CloseErr = err.Error()
	}
	if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
		t.Fatalf("write the final generation report %s: %v", reportPath, err)
	}
}

// smokeCalibrationSpawn 启动下一代宿主子进程，返回命令、日志句柄与 spawn 时刻。
// childTest 是子进程入口测试名：03.10 用校准入口，03.11 用长程入口，同一个
// 自执行二进制与同一套启机/握手结构。
func smokeCalibrationSpawn(t *testing.T, binary, planPath, runDir string, gen int, childTest string) (*exec.Cmd, *os.File, time.Time) {
	t.Helper()
	if childTest == "" {
		childTest = "TestEventIntegrityLongRunCalibration"
	}
	reportPath := filepath.Join(runDir, "reports", fmt.Sprintf("gen%02d.json", gen))
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatalf("create the calibration report directory: %v", err)
	}
	logPath := filepath.Join(runDir, "logs", fmt.Sprintf("gen%02d.log", gen))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("create the calibration log directory: %v", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create the child log %s: %v", logPath, err)
	}
	cmd := exec.Command(binary, "-test.run=^"+childTest+"$", "-test.v=true",
		"-test.timeout="+smokeCalibrationChildTimeout.String())
	cmd.Env = smokeCalibrationEnv(
		smokeCalibrationModeEnv, smokeCalibrationModeHost,
		smokeCalibrationPlanEnv, planPath,
		smokeCalibrationGenEnv, strconv.Itoa(gen),
		smokeCalibrationReportEnv, reportPath,
	)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("spawn the calibration host generation %d: %v", gen, err)
	}
	return cmd, logFile, time.Now()
}

// smokeCalibrationRun 是一轮场景（primary 或非 race 基线）的父侧观测。
type smokeCalibrationRun struct {
	WallMS        float64
	InitialBootMS float64
	Generations   []smokeCalibrationGenerationReport
	Restarts      []smokeCalibrationRestartSample
	Peak          smokeCalibrationPeak
	ChildLogs     []string
	ReportPaths   []string
}

// absorb 把一代子进程的 rusage 与自报指标并进峰值。
func (p *smokeCalibrationPeak) absorb(rss, cpu int64, self smokeCalibrationSelfUsage) {
	p.Children++
	if rss > p.MaxChildRSSBytes {
		p.MaxChildRSSBytes = rss
	}
	if self.RSSBytes > p.MaxChildRSSBytes {
		p.MaxChildRSSBytes = self.RSSBytes
	}
	if cpu > p.MaxChildCPUMillis {
		p.MaxChildCPUMillis = cpu
	}
	p.TotalChildCPUMillis += cpu
	if self.CPUMillis > p.MaxChildCPUMillis {
		p.MaxChildCPUMillis = self.CPUMillis
	}
	if self.OpenFDs > p.MaxChildOpenFDs {
		p.MaxChildOpenFDs = self.OpenFDs
	}
	if self.Goroutines > p.MaxChildGoroutines {
		p.MaxChildGoroutines = self.Goroutines
	}
	if self.HeapInuseBytes > p.MaxChildHeapBytes {
		p.MaxChildHeapBytes = self.HeapInuseBytes
	}
}

// smokeCalibrationRunGenerations 跑完一代接一代的宿主：等 ready、收本代报告、
// 到重启点 SIGKILL 并紧接着 spawn 下一代，kill→ready 就是一次重启的成本。
func smokeCalibrationRunGenerations(t *testing.T, binary, planPath, runDir string, plan smokeCalibrationPlan) *smokeCalibrationRun {
	t.Helper()
	run := &smokeCalibrationRun{}
	started := time.Now()
	var lastKill time.Time
	for gen := 0; gen <= plan.restarts(); gen++ {
		cmd, logFile, spawned := smokeCalibrationSpawn(t, binary, planPath, runDir, gen, plan.ChildTest)
		reportPath := filepath.Join(runDir, "reports", fmt.Sprintf("gen%02d.json", gen))
		run.ChildLogs = append(run.ChildLogs, logFile.Name())
		readyAt, err := smokeCalibrationAwaitFile(reportPath+".ready", smokeCalibrationReadyTimeout)
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = logFile.Close()
			t.Fatalf("calibration generation %d never became ready: %v\n%s", gen, err, smokeCalibrationLogTail(logFile.Name(), 40))
		}
		var ready smokeCalibrationGenerationReport
		if err := smokeCalibrationReadJSON(reportPath+".ready", &ready); err != nil {
			t.Fatalf("read the ready report of generation %d: %v", gen, err)
		}
		if gen == 0 {
			run.InitialBootMS = smokeCalibrationMS(readyAt.Sub(spawned))
		} else {
			rebindMS := 0.0
			for _, rebind := range ready.Rebinds {
				rebindMS += rebind.MS
			}
			run.Restarts = append(run.Restarts, smokeCalibrationRestartSample{
				Generation: gen, Trigger: plan.TimelineTrigger(gen - 1), KillToReadyMS: smokeCalibrationMS(readyAt.Sub(lastKill)),
				SpawnToReadyMS: smokeCalibrationMS(readyAt.Sub(spawned)), BootMS: ready.BootMS,
				RebindMS: rebindMS, RecoveredTasks: ready.RecoveredTasks, Rebound: len(ready.Rebinds),
			})
		}

		if _, err := smokeCalibrationAwaitFile(reportPath, smokeCalibrationGenerationTimeout); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = logFile.Close()
			t.Fatalf("calibration generation %d never reported: %v\n%s", gen, err, smokeCalibrationLogTail(logFile.Name(), 40))
		}
		var report smokeCalibrationGenerationReport
		if err := smokeCalibrationReadJSON(reportPath, &report); err != nil {
			t.Fatalf("read the report of generation %d: %v", gen, err)
		}
		if report.Phase == "failed" {
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case <-exited:
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
			_ = logFile.Close()
			t.Fatalf("generation %d failed: %s\n%s", gen, report.Failure, smokeCalibrationLogTail(logFile.Name(), 40))
		}
		run.Generations = append(run.Generations, report)
		run.ReportPaths = append(run.ReportPaths, reportPath)

		if report.Phase == "done" {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("the final generation exited with error: %v\n%s", err, smokeCalibrationLogTail(logFile.Name(), 40))
			}
			rss, cpu := smokeCalibrationProcessUsage(cmd.ProcessState)
			run.Peak.absorb(rss, cpu, report.Self)
			_ = logFile.Close()
			break
		}
		killAt := time.Now()
		killErr := cmd.Process.Kill()
		waitErr := cmd.Wait()
		rss, cpu := smokeCalibrationProcessUsage(cmd.ProcessState)
		run.Peak.absorb(rss, cpu, report.Self)
		_ = logFile.Close()
		if killErr != nil {
			t.Fatalf("kill generation %d at its restart point: %v", gen, killErr)
		}
		if waitErr == nil {
			t.Fatalf("generation %d did not die at its restart point", gen)
		}
		lastKill = killAt
	}
	run.WallMS = smokeCalibrationMS(time.Since(started))
	parent := smokeCalibrationReadSelfUsage()
	run.Peak.ParentRSSBytes = parent.RSSBytes
	run.Peak.ParentCPUMillis = parent.CPUMillis
	run.Peak.ParentGoroutines = parent.Goroutines
	return run
}

// TimelineTrigger 报告结束第 gen 代的那次重启由哪个会话触发。
func (p smokeCalibrationPlan) TimelineTrigger(gen int) string {
	for _, step := range p.Timeline {
		if step.Kind == "restart" && step.Generation == gen {
			return step.Session
		}
	}
	return ""
}

// smokeLongFault 是一次注入的故障点：挂在某个业务输入步上，子进程驱动该步时
// 执行，并把结果作为负锚的观测交回父进程。Round 是该输入在会话内的轮次。
type smokeLongFault struct {
	// Kind is one of storage_deny, model_fail or channel_fail.
	Kind       string `json:"kind"`
	Generation int    `json:"generation"`
	Session    string `json:"session"`
	Round      int    `json:"round"`
	Marker     string `json:"marker"`
}

// smokeLongReload 是一次正式 /reload 替换点：在某一代的四会话重绑之后执行。
type smokeLongReload struct {
	Generation int    `json:"generation"`
	Model      string `json:"model"`
}

// smokeLongOutbound 是子进程记下的一笔通道外发（动作身份对账的材料）。
type smokeLongOutbound struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Body   string `json:"body"`
}

// smokeLongFaultOutcome 是一个故障点的真实观测：驱动错误、是否应答、事实前后
// 计数、被拒的渠道发送与具名日志数。Outcome 只允许 answered / failed_named /
// dropped 三值，dropped 即静默丢弃（负锚失败）。
type smokeLongFaultOutcome struct {
	Kind         string `json:"kind"`
	Generation   int    `json:"generation"`
	Session      string `json:"session"`
	Round        int    `json:"round"`
	Marker       string `json:"marker"`
	Entry        string `json:"entry"`
	Answered     bool   `json:"answered"`
	AnswerBody   string `json:"answer_body,omitempty"`
	DriveError   string `json:"drive_error,omitempty"`
	FactsBefore  int    `json:"facts_before"`
	FactsAfter   int    `json:"facts_after"`
	RefusedSends int    `json:"refused_sends"`
	NamedLogs    int    `json:"named_logs"`
	// StorageProbe 是拒写期间测试自己对存储根做的一次写探针：非空即证明
	// 该次注入真的落在了一个不可写的存储根上。
	StorageProbe string `json:"storage_probe,omitempty"`
	Outcome      string `json:"outcome"`
}

// smokeLongReloadOutcome 是一次替换点的观测：安装是否成功、模型变更条数。
type smokeLongReloadOutcome struct {
	Generation int    `json:"generation"`
	Model      string `json:"model"`
	Reloaded   bool   `json:"reloaded"`
	Changes    int    `json:"changes"`
	Error      string `json:"error,omitempty"`
}

// smokeLongConsumerTurn 是一次经正式 /task 面驱动的业务输入。
type smokeLongConsumerTurn struct {
	Generation int    `json:"generation"`
	Marker     string `json:"marker"`
	Status     int    `json:"status"`
	Answered   bool   `json:"answered"`
	Body       string `json:"body,omitempty"`
	Error      string `json:"error,omitempty"`
}

// smokeLongFoldSample 是一代里一个会话的折叠产物计数：压缩确实发生过，替换与
// 压缩交错才有材料。
type smokeLongFoldSample struct {
	Session          string `json:"session"`
	Entries          int    `json:"entries"`
	Synthetics       int    `json:"synthetics"`
	HistorySummaries int    `json:"history_summaries"`
}

// smokeLongDrainReport 是末代关闭的终态排空结论。
type smokeLongDrainReport struct {
	Drained   bool     `json:"drained"`
	Unsettled []string `json:"unsettled,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// smokeLongLogCounts 是子进程里有名字的日志计数：渠道失败必须留下具名日志，
// 静默吞掉即负锚失败。
type smokeLongLogCounts struct {
	SendFailed int `json:"send_failed"`
}

// smokeLongConcurrentBatch 是一次「四会话同时完成」的观测：四路驱动的错误与
// 应答、以及是否四路都拿到本轮答案。
type smokeLongConcurrentBatch struct {
	Round    int      `json:"round"`
	Sessions int      `json:"sessions"`
	Answered int      `json:"answered"`
	Errors   []string `json:"errors,omitempty"`
}

// smokeLongPhrase 渲染一条与 seed 有关的业务正文：正文带唯一标记，措辞按 seed
// 与轮次轮换，但不含脚本模型的特殊关键字（send/approval/delegate/long/file）。
func smokeLongPhrase(seed, round int, chat string) string {
	phrases := []string{
		"plain question",
		"business note",
		"customer asks about the order",
		"status update for the team",
	}
	return phrases[(seed+round+len(chat))%len(phrases)]
}

// smokeLongTimeline 造 03.11 的冻结时间线：4 个会话按轮次交错，每会话每
// restartEvery 个输入之后各触发一次重启（四会话在同一轮各一次，共 restarts
// 次/会话）；每 smokeLongDelegateEvery 个输入里有一个委派输入。
func smokeLongTimeline(seed int, sessions []string) []smokeCalibrationStep {
	steps := make([]smokeCalibrationStep, 0, smokeLongInputsPerSession*len(sessions)+smokeLongRestartsPerSession*len(sessions))
	triggered := make(map[string]int, len(sessions))
	gen, seq := 0, 0
	for round := 0; round < smokeLongInputsPerSession; round++ {
		for _, chat := range sessions {
			marker := fmt.Sprintf("SMOKE-R%d%04d", seed, seq)
			shape := "plain"
			text := marker + " " + smokeLongPhrase(seed, round, chat)
			if (round+1)%smokeLongDelegateEvery == 0 {
				shape = "delegate"
				text = marker + " smoke delegate artifact"
			}
			steps = append(steps, smokeCalibrationStep{
				Kind: "input", Generation: gen, Session: chat, Index: round,
				Marker: marker, Text: text, Shape: shape,
			})
			seq++
			if (round+1)%smokeLongRestartEvery != 0 {
				continue
			}
			if triggered[chat] >= smokeLongRestartsPerSession {
				continue
			}
			triggered[chat]++
			steps = append(steps, smokeCalibrationStep{Kind: "restart", Generation: gen, Session: chat})
			gen++
		}
	}
	return steps
}

// smokeLongBuildPlan 把 03.11 的冻结口径落成一份计划：全量时间线 + 故障点 +
// 替换点 + 并发完成轮 + /task 消费会话。
func smokeLongBuildPlan(cfg *smokeConfig, runDir string, window, seed int) smokeCalibrationPlan {
	chats := append([]string(nil), smokeCalibrationChats...)
	consumer := chats[len(chats)-1]
	plan := smokeCalibrationPlan{
		Leaf: smokeLongLeaf, Window: window, Seed: seed,
		Root: cfg.Root, WorkspaceDir: cfg.WorkspaceDir, ConfigDir: cfg.ConfigDir,
		ContextDir: cfg.ContextDir, ApprovalDir: cfg.ApprovalDir, PromptDir: cfg.PromptDir,
		StorageRoot: cfg.StorageRoot, ConfigPath: cfg.ConfigPath, Endpoint: cfg.Endpoint,
		Sessions: chats, InputsPerSession: smokeLongInputsPerSession,
		RestartsPerSession: smokeLongRestartsPerSession, RestartEvery: smokeLongRestartEvery,
		Timeline:        smokeLongTimeline(seed, chats),
		ReportDir:       filepath.Join(runDir, "reports"),
		ChildTest:       smokeLongChildTest,
		ConsumerSession: consumer,
	}
	faults := []struct {
		kind  string
		round int
		chat  string
	}{
		{smokeLongFaultStorage, smokeLongStorageFaultRound, chats[0]},
		{smokeLongFaultModel, smokeLongModelFaultRound, chats[1]},
		{smokeLongFaultChannel, smokeLongChannelFaultRound, chats[2]},
	}
	for _, want := range faults {
		fault, ok := smokeLongFaultAt(plan.Timeline, want.kind, want.round, want.chat)
		if !ok {
			panic(fmt.Sprintf("smoke long: no timeline step for the %s fault at round %d of %s", want.kind, want.round, want.chat))
		}
		plan.Faults = append(plan.Faults, fault)
	}
	for i := 0; i < smokeLongReloadGens; i++ {
		model := smokeReloadModel
		if i%2 == 1 {
			model = smokeEntryModel
		}
		plan.Reloads = append(plan.Reloads, smokeLongReload{
			Generation: smokeLongReloadEvery * (i + 1), Model: model,
		})
	}
	for round := smokeLongConcurrentRoundFirst; round < smokeLongInputsPerSession; round += smokeLongConcurrentRoundStep {
		plan.ConcurrentRounds = append(plan.ConcurrentRounds, round)
	}
	return plan
}

// smokeLongFaultAt 在时间线上定位一个故障步：返回它真实所属的代与标记，而不是
// 用轮次和代数之间的算术关系去猜（重启让这种关系不是线性的）。
func smokeLongFaultAt(timeline []smokeCalibrationStep, kind string, round int, chat string) (smokeLongFault, bool) {
	for _, step := range timeline {
		if step.Kind != "input" || step.Session != chat || step.Index != round {
			continue
		}
		return smokeLongFault{
			Kind: kind, Generation: step.Generation, Session: chat,
			Round: round, Marker: step.Marker,
		}, true
	}
	return smokeLongFault{}, false
}

// smokeLongFindFault 报告某代/会话/轮次上的故障点。
func smokeLongFindFault(plan smokeCalibrationPlan, gen int, chat string, round int) (smokeLongFault, bool) {
	for _, fault := range plan.Faults {
		if fault.Generation == gen && fault.Session == chat && fault.Round == round {
			return fault, true
		}
	}
	return smokeLongFault{}, false
}

// smokeLongFindReload 报告某代挂载的替换点。
func smokeLongFindReload(plan smokeCalibrationPlan, gen int) (smokeLongReload, bool) {
	for _, reload := range plan.Reloads {
		if reload.Generation == gen {
			return reload, true
		}
	}
	return smokeLongReload{}, false
}

// smokeLongConcurrent 报告某轮是否要求四会话同时完成。
func smokeLongConcurrent(plan smokeCalibrationPlan, round int) bool {
	for _, want := range plan.ConcurrentRounds {
		if want == round {
			return true
		}
	}
	return false
}

// smokeLongWant 报告一个输入步应等到的外发正文前缀；委派输入两步。
func (s smokeCalibrationStep) smokeLongWant() []string {
	if s.Shape == "delegate" {
		return []string{"delegate ack " + s.Marker, "settle-ack " + s.Marker}
	}
	return []string{"answer " + s.Marker}
}

// smokeLongChannel 是能在指定标记上拒绝投递的通道包装器：渠道失败的负锚要
// 一个可复现的替身失败，且拒绝本身必须被记录（不能悄悄换一个替身）。
type smokeLongChannel struct {
	*fakeChannel

	mu      sync.Mutex
	fail    map[string]bool
	refused map[string]int
}

// newSmokeLongChannel 建一台带拒绝名单的通道；名单来自计划里的渠道故障标记。
func newSmokeLongChannel(plan smokeCalibrationPlan) *smokeLongChannel {
	channel := &smokeLongChannel{
		fakeChannel: newFakeChannel(),
		fail:        map[string]bool{},
		refused:     map[string]int{},
	}
	for _, fault := range plan.Faults {
		if fault.Kind == smokeLongFaultChannel {
			channel.fail[fault.Marker] = true
		}
	}
	return channel
}

// SendTextToUser 对命中拒绝名单的正文返回具名错误；其它正文照常记入底层替身。
func (c *smokeLongChannel) SendTextToUser(ctx context.Context, to, text string) error {
	c.mu.Lock()
	hit := ""
	for marker := range c.fail {
		if strings.Contains(text, marker) {
			hit = marker
			break
		}
	}
	if hit != "" {
		c.refused[hit]++
		c.mu.Unlock()
		return fmt.Errorf("smoke long: the channel refused the delivery of %s", hit)
	}
	c.mu.Unlock()
	return c.fakeChannel.SendTextToUser(ctx, to, text)
}

// refusedCount 报告某标记被拒的次数。
func (c *smokeLongChannel) refusedCount(marker string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refused[marker]
}

// smokeLongDenyWrites 把存储根下所有目录改成不可写，并写一次探针证明拒写真的
// 生效；返回探针错误与恢复函数。恢复函数幂等，调用方用 defer 保证恢复。
func smokeLongDenyWrites(t *testing.T, root string) (string, func() error) {
	t.Helper()
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the storage root %s before denying writes: %v", root, err)
	}
	for _, dir := range dirs {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("deny writes on %s: %v", dir, err)
		}
	}
	restored := false
	restore := func() error {
		if restored {
			return nil
		}
		restored = true
		var first error
		for i := len(dirs) - 1; i >= 0; i-- {
			if err := os.Chmod(dirs[i], 0o755); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	probePath := filepath.Join(root, fmt.Sprintf(".smoke-long-probe-%d", time.Now().UnixNano()))
	probeErr := os.WriteFile(probePath, []byte("probe"), 0o644)
	if probeErr == nil {
		_ = os.Remove(probePath)
		_ = restore()
		t.Fatalf("the storage root %s stayed writable under the deny injection", root)
	}
	return probeErr.Error(), restore
}

// smokeLongRebindOrder 报告一代重绑的会话顺序：消费会话放在最后，正式 /task
// 面因此指向它（注入面只驱动最后准入的用户会话）。
func smokeLongRebindOrder(plan smokeCalibrationPlan) []string {
	order := make([]string, 0, len(plan.Sessions))
	for _, chat := range plan.Sessions {
		if chat != plan.ConsumerSession {
			order = append(order, chat)
		}
	}
	if plan.ConsumerSession != "" {
		order = append(order, plan.ConsumerSession)
	}
	return order
}

// smokeLongRebindMarker 是一代里某会话的重绑标记：代三位 + 会话两位保证跨代
// 唯一，不复用 03.10 的 R86 前缀。
func smokeLongRebindMarker(plan smokeCalibrationPlan, gen int, chat string) string {
	index := 0
	for i, want := range plan.Sessions {
		if want == chat {
			index = i
			break
		}
	}
	return fmt.Sprintf("%s%03d%02d", smokeLongRebindPrefix, gen, index)
}

// smokeLongGenerationSteps 把一代的输入步按（轮次、会话顺序）排好，并把重启
// 步骤留在末尾：重启步骤归它结束的那一代。
func smokeLongGenerationSteps(plan smokeCalibrationPlan, gen int) ([]smokeCalibrationStep, string) {
	order := make(map[string]int, len(plan.Sessions))
	for i, chat := range plan.Sessions {
		order[chat] = i
	}
	inputs := make([]smokeCalibrationStep, 0, 64)
	trigger := ""
	for _, step := range plan.Timeline {
		if step.Generation != gen {
			continue
		}
		if step.Kind == "restart" {
			trigger = step.Session
			continue
		}
		inputs = append(inputs, step)
	}
	sort.SliceStable(inputs, func(i, j int) bool {
		if inputs[i].Index != inputs[j].Index {
			return inputs[i].Index < inputs[j].Index
		}
		return order[inputs[i].Session] < order[inputs[j].Session]
	})
	return inputs, trigger
}

// smokeLongConsumerMarker 报告本代第一个经正式 /task 面驱动的业务输入标记。
// 该代没有消费会话的输入、或第一个输入落在并发完成轮（四路同时）时为空。
func smokeLongConsumerMarker(plan smokeCalibrationPlan, gen int, inputs []smokeCalibrationStep) string {
	if plan.ConsumerSession == "" {
		return ""
	}
	markers := make([]string, 0, 16)
	for _, step := range inputs {
		if step.Session == plan.ConsumerSession {
			markers = append(markers, step.Marker)
		}
	}
	if len(markers) == 0 {
		return ""
	}
	index := 0
	if gen == 0 {
		index = 1
	}
	if index >= len(markers) {
		return ""
	}
	for _, step := range inputs {
		if step.Marker == markers[index] && smokeLongConcurrent(plan, step.Index) {
			return ""
		}
	}
	return markers[index]
}

// smokeLongAwaitLimit 是一次外发等待的上限：正式矩阵用它判定一条身份在这
// 一代里确实没有到达，而不是把缓慢当成丢失。
const smokeLongAwaitLimit = 60 * time.Second

// smokeLongAwait 是这一次等待的上限。
func smokeLongAwait() time.Duration { return smokeLongAwaitLimit }

// smokeLongAwaitAny 等到该会话任一候选正文出现，返回命中的正文；超时返回错误。
func (r *smokeRig) smokeLongAwaitAny(chat string, wants []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		for _, evt := range r.recorded() {
			if evt.Kind != "text" || evt.Target != chat {
				continue
			}
			for _, want := range wants {
				if strings.Contains(evt.Body, want) {
					return evt.Body, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no text to %s carrying any of %v after %s", chat, wants, timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// smokeLongHostChild 是 03.11 宿主子进程的一代：真实 Boot、四会话按消费会话
// 最后的顺序重绑、本代替换点、按轮次驱动本代输入（消费会话的首个输入经正式
// /task 面）、执行故障注入与并发完成批，最后把观测写进报告。遇到重启步骤就
// 落盘报告并等父进程 SIGKILL。
func smokeLongHostChild(t *testing.T) {
	t.Helper()
	planPath := strings.TrimSpace(os.Getenv(smokeCalibrationPlanEnv))
	if planPath == "" {
		t.Fatal("the long-run host child carries no plan path")
	}
	var plan smokeCalibrationPlan
	if err := smokeCalibrationReadJSON(planPath, &plan); err != nil {
		t.Fatalf("read the long-run plan %s: %v", planPath, err)
	}
	gen, err := strconv.Atoi(strings.TrimSpace(os.Getenv(smokeCalibrationGenEnv)))
	if err != nil {
		t.Fatalf("the long-run host child carries no generation: %v", err)
	}
	reportPath := strings.TrimSpace(os.Getenv(smokeCalibrationReportEnv))
	if reportPath == "" {
		t.Fatal("the long-run host child carries no report path")
	}
	cfg := &smokeConfig{
		Root: plan.Root, WorkspaceDir: plan.WorkspaceDir, ConfigDir: plan.ConfigDir,
		ContextDir: plan.ContextDir, ApprovalDir: plan.ApprovalDir, PromptDir: plan.PromptDir,
		StorageRoot: plan.StorageRoot, ConfigPath: plan.ConfigPath, Endpoint: plan.Endpoint,
		Model: smokeEntryModel, MaxTokens: plan.Window,
	}

	recorder := installSmokeLog(t)
	channel := newSmokeLongChannel(plan)
	started := time.Now()
	rig := bootSmokeRigWithPort(t, cfg, nil, nil, channel.fakeChannel, channel)
	report := &smokeCalibrationGenerationReport{
		Leaf: smokeLongLeaf, Generation: gen, Window: plan.Window,
		BootMS:         smokeCalibrationMS(time.Since(started)),
		RecoveredTasks: len(rig.host.Run().Recovery().Tasks),
		RestoredFacts:  rig.host.Run().CommittedFacts(),
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		report.Phase = "failed"
		report.Failure = "the generation ended on a failed assertion; the child log names it"
		report.WrittenAt = time.Now().UTC().Format(time.RFC3339Nano)
		_ = smokeCalibrationWriteJSON(reportPath, report)
	})

	for _, chat := range smokeLongRebindOrder(plan) {
		marker := smokeLongRebindMarker(plan, gen, chat)
		start := time.Now()
		if err := rig.driveResult(chat, marker+" rebind "+chat); err != nil {
			t.Fatalf("rebind %s in generation %d: %v", chat, gen, err)
		}
		if _, err := rig.awaitText(chat, "answer "+marker, 60*time.Second); err != nil {
			t.Fatalf("rebind %s in generation %d: %v", chat, gen, err)
		}
		report.Rebinds = append(report.Rebinds, smokeCalibrationRebindSample{
			Generation: gen, Session: chat, Marker: marker,
			MS: smokeCalibrationMS(time.Since(start)),
		})
	}

	if reload, ok := smokeLongFindReload(plan, gen); ok {
		result := rig.smokeIntegrityReload(reload.Model)
		outcome := smokeLongReloadOutcome{Generation: gen, Model: reload.Model}
		if result.err != nil {
			outcome.Error = result.err.Error()
		} else {
			outcome.Reloaded = result.answer.Reloaded
			outcome.Changes = len(result.answer.ModelChanges)
		}
		report.Reloads = append(report.Reloads, outcome)
		if result.err != nil {
			t.Fatalf("reload generation %d: %v", gen, result.err)
		}
	}

	report.Self = smokeCalibrationReadSelfUsage()
	report.WrittenAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := smokeCalibrationWriteJSON(reportPath+".ready", report); err != nil {
		t.Fatalf("write the long-run ready report %s: %v", reportPath, err)
	}

	inputs, trigger := smokeLongGenerationSteps(plan, gen)
	consumerTurn := smokeLongConsumerMarker(plan, gen, inputs)
	if consumerTurn != "" {
		for i, step := range inputs {
			if step.Marker != consumerTurn {
				continue
			}
			reordered := make([]smokeCalibrationStep, 0, len(inputs))
			reordered = append(reordered, step)
			reordered = append(reordered, inputs[:i]...)
			reordered = append(reordered, inputs[i+1:]...)
			inputs = reordered
			break
		}
	}

	for i := 0; i < len(inputs); {
		step := inputs[i]
		if smokeLongConcurrent(plan, step.Index) {
			end := i
			for end < len(inputs) && inputs[end].Index == step.Index {
				end++
			}
			batch := inputs[i:end]
			report.Concurrent = append(report.Concurrent, rig.smokeLongConcurrentBatch(batch))
			for _, item := range batch {
				report.Inputs = append(report.Inputs, smokeCalibrationInputSample{
					Generation: gen, Session: item.Session, Index: item.Index, Marker: item.Marker,
				})
			}
			i = end
			continue
		}
		start := time.Now()
		if consumerTurn != "" && step.Marker == consumerTurn {
			report.Consumer = append(report.Consumer, rig.smokeLongConsumer(gen, step))
		} else if fault, ok := smokeLongFindFault(plan, gen, step.Session, step.Index); ok {
			report.Faults = append(report.Faults, rig.smokeLongFault(recorder, channel, cfg, step, fault))
		} else {
			if err := rig.driveResult(step.Session, step.Text); err != nil {
				t.Fatalf("drive generation %d session %s round %d: %v", gen, step.Session, step.Index, err)
			}
			for _, want := range step.smokeLongWant() {
				if _, err := rig.awaitText(step.Session, want, smokeLongAwait()); err != nil {
					t.Fatalf("answer generation %d session %s round %d: %v", gen, step.Session, step.Index, err)
				}
			}
		}
		report.Inputs = append(report.Inputs, smokeCalibrationInputSample{
			Generation: gen, Session: step.Session, Index: step.Index, Marker: step.Marker,
			MS: smokeCalibrationMS(time.Since(start)),
		})
		i++
	}

	for _, chat := range plan.Sessions {
		entries, carried := rig.host.Run().ReplayEntries(rig.addressOf(chat).SessionID)
		sample := smokeLongFoldSample{Session: chat}
		if carried {
			sample.Entries = len(entries)
			synthetics := syntheticEntries(entries)
			sample.Synthetics = len(synthetics)
			for _, entry := range synthetics {
				if entry.Type == replay.ProjectionTypeHistorySummary {
					sample.HistorySummaries++
				}
			}
		}
		report.Folds = append(report.Folds, sample)
	}
	for _, evt := range channel.recorded() {
		report.Outbound = append(report.Outbound, smokeLongOutbound{Kind: evt.Kind, Target: evt.Target, Body: evt.Body})
	}
	report.Logs = smokeLongLogCounts{SendFailed: recorder.count("SendTextToUser failed")}
	report.Self = smokeCalibrationReadSelfUsage()
	report.WrittenAt = time.Now().UTC().Format(time.RFC3339Nano)

	if trigger != "" {
		report.TriggerSession = trigger
		report.Phase = "restart_point"
		if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
			t.Fatalf("write the long-run restart point %s: %v", reportPath, err)
		}
		time.Sleep(10 * time.Minute)
		return
	}

	report.Phase = "done"
	if err := rig.close(); err != nil {
		report.CloseErr = err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drain, drainErr := rig.host.Run().Close(ctx, agent.CloseRequest{})
	report.Drain = &smokeLongDrainReport{Drained: drain.Drained}
	for _, item := range drain.Unsettled {
		report.Drain.Unsettled = append(report.Drain.Unsettled, fmt.Sprintf("task=%s reason=%s", item.TaskID, item.Reason))
	}
	if drainErr != nil {
		report.Drain.Error = drainErr.Error()
	}
	if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
		t.Fatalf("write the final long-run generation report %s: %v", reportPath, err)
	}
}

// smokeLongFault 执行一个故障注入点并把它归类为 answered / failed_named /
// dropped。dropped 就是静默丢弃，直接让负锚失败。
func (r *smokeRig) smokeLongFault(recorder *smokeLogRecorder, channel *smokeLongChannel, cfg *smokeConfig, step smokeCalibrationStep, fault smokeLongFault) smokeLongFaultOutcome {
	outcome := smokeLongFaultOutcome{
		Kind: fault.Kind, Generation: fault.Generation, Session: fault.Session, Round: fault.Round,
		Marker: fault.Marker, Entry: smokeLongEntry(step.Text),
		FactsBefore: r.host.Run().CommittedFacts(),
	}
	mark := recorder.mark()
	wants := step.smokeLongWant()
	switch fault.Kind {
	case smokeLongFaultStorage:
		probe, restore := smokeLongDenyWrites(r.t, cfg.StorageRoot)
		outcome.StorageProbe = probe
		if err := r.driveResult(step.Session, step.Text); err != nil {
			outcome.DriveError = err.Error()
		}
		storageWants := append(wants, "answer "+fault.Marker)
		if body, err := r.smokeLongAwaitAny(step.Session, storageWants, 20*time.Second); err == nil {
			outcome.Answered = true
			outcome.AnswerBody = body
		}
		if err := restore(); err != nil {
			r.t.Fatalf("restore the storage root after the deny injection: %v", err)
		}
		outcome.NamedLogs = recorder.countAnySince(mark, "durable", "storage", "commit barrier", "uncommitted", "undeclared", "uncertain")
	case smokeLongFaultModel:
		if err := r.driveResult(step.Session, step.Text); err != nil {
			outcome.DriveError = err.Error()
		}
		if body, err := r.smokeLongAwaitAny(step.Session, append(wants, "500"), 20*time.Second); err == nil {
			outcome.AnswerBody = body
			outcome.Answered = strings.Contains(body, wants[0])
		}
		outcome.NamedLogs = recorder.countAnySince(mark, "model", "request failed", "failed")
	case smokeLongFaultChannel:
		if err := r.driveResult(step.Session, step.Text); err != nil {
			outcome.DriveError = err.Error()
		}
		deadline := time.Now().Add(10 * time.Second)
		for channel.refusedCount(fault.Marker) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		outcome.RefusedSends = channel.refusedCount(fault.Marker)
		outcome.NamedLogs = recorder.countAnySince(mark, "SendTextToUser failed")
	}
	outcome.FactsAfter = r.host.Run().CommittedFacts()
	switch {
	case outcome.Answered:
		outcome.Outcome = "answered"
	case outcome.AnswerBody != "" || outcome.DriveError != "" || outcome.NamedLogs > 0 || outcome.RefusedSends > 0:
		outcome.Outcome = "failed_named"
	default:
		outcome.Outcome = "dropped"
	}
	return outcome
}

// smokeLongEntry 截取一步输入的首行，供报告里引用受影响身份。
func smokeLongEntry(text string) string {
	line := strings.SplitN(text, "\n", 2)[0]
	if len(line) > 96 {
		line = line[:96]
	}
	return line
}

// smokeLongConcurrentBatch drives one round of four concurrent sessions and returns the four answers with their drive results.
func (r *smokeRig) smokeLongConcurrentBatch(batch []smokeCalibrationStep) smokeLongConcurrentBatch {
	type result struct {
		session  string
		err      error
		answered bool
	}
	results := make([]result, len(batch))
	var wg sync.WaitGroup
	for i, step := range batch {
		wg.Add(1)
		go func(i int, step smokeCalibrationStep) {
			defer wg.Done()
			results[i].session = step.Session
			if err := r.driveResult(step.Session, step.Text); err != nil {
				results[i].err = err
				return
			}
			if _, err := r.awaitText(step.Session, "answer "+step.Marker, 60*time.Second); err != nil {
				results[i].err = err
				return
			}
			results[i].answered = true
		}(i, step)
	}
	wg.Wait()
	out := smokeLongConcurrentBatch{Round: batch[0].Index, Sessions: len(batch)}
	for _, item := range results {
		if item.answered {
			out.Answered++
		}
		if item.err != nil {
			out.Errors = append(out.Errors, item.session+": "+item.err.Error())
		}
	}
	return out
}

// smokeLongConsumer 经正式 /task 面驱动消费会话的一个业务输入。
func (r *smokeRig) smokeLongConsumer(gen int, step smokeCalibrationStep) smokeLongConsumerTurn {
	turn := smokeLongConsumerTurn{Generation: gen, Marker: step.Marker}
	status, body := r.postTaskStatus(step.Text)
	turn.Status = status
	if status != 202 {
		turn.Error = fmt.Sprintf("task injection status = %d: %s", status, body)
		return turn
	}
	for _, want := range step.smokeLongWant() {
		if _, err := r.awaitText(step.Session, want, 60*time.Second); err != nil {
			turn.Error = err.Error()
			return turn
		}
	}
	turn.Answered = true
	return turn
}

// smokeLongScale 是一次 03.11 组合的冻结口径与不变式。
type smokeLongScale struct {
	Sessions           int    `json:"sessions"`
	InputsPerSession   int    `json:"inputs_per_session"`
	InputsTotal        int    `json:"inputs_total"`
	RestartsPerSession int    `json:"restarts_per_session"`
	RestartsTotal      int    `json:"restarts_total"`
	Generations        int    `json:"generations"`
	RestartEvery       int    `json:"restart_every_inputs"`
	DelegateInputs     int    `json:"delegate_inputs"`
	Faults             int    `json:"faults"`
	ConsumerSession    string `json:"consumer_session"`
	SharedHost         bool   `json:"shared_host"`
	MeditationEnabled  bool   `json:"meditation_enabled"`
	FsyncDropped       bool   `json:"fsync_dropped"`
}

// smokeLongReconciliation 是一次组合的逐身份对账：输入/通知/动作/重启/折叠/
// 故障/消费者链各自的实际计数与缺失清单。
type smokeLongReconciliation struct {
	ExpectedInputs         int      `json:"expected_inputs"`
	InputSamples           int      `json:"input_samples"`
	InputRequests          int      `json:"input_requests"`
	FaultInputAttempts     int      `json:"fault_input_attempts"`
	FaultInputRetries      int      `json:"fault_input_retries"`
	MissingInputRequests   []string `json:"missing_input_requests"`
	DuplicateInputRequests []string `json:"duplicate_input_requests"`
	ExpectedRebinds        int      `json:"expected_rebinds"`
	RebindRequests         int      `json:"rebind_requests"`
	MissingRebinds         []string `json:"missing_rebind_requests"`
	DuplicateRebinds       []string `json:"duplicate_rebind_requests,omitempty"`
	ExpectedNotifications  int      `json:"expected_notifications"`
	NotificationRequests   int      `json:"notification_requests"`
	MissingNotifications   []string `json:"missing_notifications"`
	DuplicateNotifications []string `json:"duplicate_notifications,omitempty"`
	ExpectedActions        int      `json:"expected_actions"`
	ActionEvents           int      `json:"action_events"`
	MissingActions         []string `json:"missing_actions"`
	DuplicateActions       []string `json:"duplicate_actions,omitempty"`
	Faults                 int      `json:"faults"`
	FaultsAnswered         int      `json:"faults_answered"`
	FaultsNamed            int      `json:"faults_named"`
	FaultsDropped          []string `json:"faults_dropped"`
	ProviderRequests       int      `json:"provider_requests"`
	ReflectRequests        int      `json:"reflect_requests"`
	DelegateInputs         int      `json:"delegate_inputs"`
	ExpectedConsumerTurns  int      `json:"expected_consumer_turns"`
	ConsumerTurns          int      `json:"consumer_turns"`
	ConsumerAnswered       int      `json:"consumer_answered"`
	ConcurrentBatches      int      `json:"concurrent_batches"`
	ConcurrentAnswered     int      `json:"concurrent_answered"`
	Reloads                int      `json:"reloads"`
	ReloadsInstalled       int      `json:"reloads_installed"`
	FoldSamples            int      `json:"fold_samples"`
	FoldSynthetics         int      `json:"fold_synthetics"`
}

// smokeLongReport 是一次 03.11 组合的完整报告：六个组合各一份，汇总时才得
// 7200 输入/720 重启。
type smokeLongReport struct {
	Leaf        string                          `json:"leaf"`
	Kind        string                          `json:"kind"`
	Window      int                             `json:"window"`
	Seed        int                             `json:"seed"`
	RaceEnabled bool                            `json:"race_enabled"`
	GeneratedAt string                          `json:"generated_at"`
	RunDir      string                          `json:"run_dir"`
	PlanPath    string                          `json:"plan_path"`
	ReportPath  string                          `json:"report_path"`
	WallMS      float64                         `json:"wall_ms"`
	InitialBoot float64                         `json:"initial_boot_ms"`
	Inputs      []smokeCalibrationInputSample   `json:"inputs"`
	Scale       smokeLongScale                  `json:"scale"`
	Recon       smokeLongReconciliation         `json:"reconciliation"`
	Restarts    []smokeCalibrationRestartSample `json:"restarts"`
	Faults      []smokeLongFaultOutcome         `json:"faults"`
	Reloads     []smokeLongReloadOutcome        `json:"reloads"`
	Consumer    []smokeLongConsumerTurn         `json:"consumer_turns"`
	Concurrent  []smokeLongConcurrentBatch      `json:"concurrent"`
	Folds       []smokeLongFoldSample           `json:"folds"`
	Drain       *smokeLongDrainReport           `json:"drain"`
	Outbound    []smokeLongOutbound             `json:"outbound"`
	Peak        smokeCalibrationPeak            `json:"peak"`
	Storage     smokeCalibrationStorage         `json:"storage"`
	ChildLogs   []string                        `json:"child_logs"`
}

// smokeLongRunDir 报告 03.11 组合的产物目录：默认按 seed/window 分目录并带
// 时间戳，不覆盖历史原始数据；父进程可用 TAGENT_CALIBRATION_RUN_DIR 固定它，
// 六组合与稳定性档因此各有独立的 plan/报告/子进程日志。
func smokeLongRunDir(t *testing.T) string {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv(smokeCalibrationRunEnv))
	if dir == "" {
		base := os.Getenv("TMPDIR")
		if base == "" {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "event-consumption-integrity", "03.11",
			fmt.Sprintf("run-s%d-w%d-%s", *smokeCalibrationSeedFlag, *smokeCalibrationWindowFlag, time.Now().Format("20060102-150405")))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the long-run directory %s: %v", dir, err)
	}
	return dir
}

// smokeLongModelFaultHook 让脚本模型拒绝模型故障步：请求已按输入身份记入
// provider.requests，再回 500；只有最后一条用户消息正是该标记的回合命中，
// 历史里带着旧标记的后续回合不受影响。
func smokeLongModelFaultHook(plan smokeCalibrationPlan) func(http.ResponseWriter, int, string, []smokeModelMessage) bool {
	marker := ""
	for _, fault := range plan.Faults {
		if fault.Kind == smokeLongFaultModel {
			marker = fault.Marker
			break
		}
	}
	if marker == "" {
		return nil
	}
	return func(w http.ResponseWriter, _ int, _ string, messages []smokeModelMessage) bool {
		if !strings.HasPrefix(smokeLastUserText(messages), marker+" ") {
			return false
		}
		http.Error(w, "smoke long: the scripted model refuses the "+marker+" turn", http.StatusInternalServerError)
		return true
	}
}

// smokeLongPrimary 跑一次完整的 03.11 组合：同一 Host 上 1200 输入/120 重启，
// 夹杂三个故障点、三个替换点、两个并发完成轮与 119 个 /task 消费回合；跑完
// 把跨代观测汇总成一份报告并落盘。
func smokeLongPrimary(t *testing.T) (*smokeLongReport, string) {
	t.Helper()
	runDir := smokeLongRunDir(t)
	window, seed := *smokeCalibrationWindowFlag, *smokeCalibrationSeedFlag
	if window != 256 && window != 32768 {
		t.Fatalf("window %d is not one of the two long-run levels (256/32768)", window)
	}
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.MaxTokens = window
	cfg.write(t)

	plan := smokeLongBuildPlan(cfg, runDir, window, seed)
	provider.hook = smokeLongModelFaultHook(plan)
	planPath := filepath.Join(runDir, "plan.json")
	if err := smokeCalibrationWriteJSON(planPath, plan); err != nil {
		t.Fatalf("write the long-run plan: %v", err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		smokeLongWriteProviderDump(t, provider, filepath.Join(runDir, "provider-requests-failed.json"))
	})
	beforeBytes, beforeFiles, problems := smokeCalibrationWalk(cfg.StorageRoot)
	run := smokeCalibrationRunGenerations(t, smokeCalibrationMustExecutable(t), planPath, runDir, plan)
	afterBytes, afterFiles, afterProblems := smokeCalibrationWalk(cfg.StorageRoot)
	problems = append(problems, afterProblems...)

	report := smokeLongAssemble(t, plan, run, provider, cfg, runDir, planPath, window, seed,
		beforeBytes, beforeFiles, afterBytes, afterFiles, problems)
	reportPath := filepath.Join(runDir, fmt.Sprintf("report-window-%d-seed-%d.json", window, seed))
	report.ReportPath = reportPath
	if err := smokeCalibrationWriteJSON(reportPath, report); err != nil {
		t.Fatalf("write the long-run report %s: %v", reportPath, err)
	}
	if err := smokeCalibrationWriteJSON(filepath.Join(runDir, "report.json"), report); err != nil {
		t.Fatalf("write the long-run report alias: %v", err)
	}
	return report, reportPath
}

// smokeLongProviderDump 是失败时落盘的“模型看到了什么”：每条请求的模型名、
// 消息数、最后一条用户正文（截断）与末条消息，以及最后五条请求的完整消息
// 角色序列。它只写测试已经观测到的内容，不改断言。
type smokeLongProviderDump struct {
	Requests []smokeLongProviderRequest `json:"requests"`
	Tail     []smokeLongProviderRequest `json:"tail"`
}

type smokeLongProviderRequest struct {
	Index  int      `json:"index"`
	Model  string   `json:"model"`
	Count  int      `json:"messages"`
	Last   string   `json:"last_user_text"`
	Roles  []string `json:"roles,omitempty"`
	Bodies []string `json:"bodies,omitempty"`
}

// smokeLongClip 把一段正文截到 n 个字符，供失败观测不无限增大。
func smokeLongClip(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}

// smokeLongWriteProviderDump 把父侧记下的请求写成一份原始观测；同一输入身份
// 重复出现时连角色与正文一起落盘，只有索引无法归因。
func smokeLongWriteProviderDump(t *testing.T, provider *smokeProvider, path string) {
	t.Helper()
	recorded := provider.recorded()
	dump := smokeLongProviderDump{}
	seen := map[string]int{}
	for _, req := range recorded {
		seen[smokeLastUserText(req.Messages)]++
	}
	for i, req := range recorded {
		last := smokeLastUserText(req.Messages)
		item := smokeLongProviderRequest{Index: i, Model: req.Model, Count: len(req.Messages), Last: smokeLongClip(last, 200)}
		if seen[last] > 1 {
			for _, msg := range req.Messages {
				item.Roles = append(item.Roles, msg.Role)
				item.Bodies = append(item.Bodies, smokeLongClip(msg.Content, 300))
			}
		}
		dump.Requests = append(dump.Requests, item)
	}
	start := len(recorded) - 5
	if start < 0 {
		start = 0
	}
	for i := start; i < len(recorded); i++ {
		item := smokeLongProviderRequest{Index: i, Model: recorded[i].Model, Count: len(recorded[i].Messages)}
		for _, msg := range recorded[i].Messages {
			item.Roles = append(item.Roles, msg.Role)
			item.Bodies = append(item.Bodies, smokeLongClip(msg.Content, 300))
		}
		dump.Tail = append(dump.Tail, item)
	}
	_ = smokeCalibrationWriteJSON(path, dump)
}

// smokeLongIsFaultStep 报告某输入步是否挂着故障注入。
func smokeLongIsFaultStep(plan smokeCalibrationPlan, step smokeCalibrationStep) bool {
	for _, fault := range plan.Faults {
		if fault.Generation == step.Generation && fault.Session == step.Session && fault.Round == step.Index {
			return true
		}
	}
	return false
}

// smokeLongDelegateCount 报告计划里委派输入的数目（每个都派生一次结算通知）。
func smokeLongDelegateCount(plan smokeCalibrationPlan) int {
	n := 0
	for _, step := range plan.Timeline {
		if step.Kind == "input" && step.Shape == "delegate" {
			n++
		}
	}
	return n
}

// smokeLongAssemble 把各代报告汇总成一份组合报告：输入、故障、替换、/task
// 回合、并发批、折叠、外发与终态排空都按到达顺序拼接，再按计划核账。
func smokeLongAssemble(t *testing.T, plan smokeCalibrationPlan, run *smokeCalibrationRun, provider *smokeProvider, cfg *smokeConfig, runDir, planPath string, window, seed int, beforeBytes int64, beforeFiles int, afterBytes int64, afterFiles int, problems []string) *smokeLongReport {
	t.Helper()
	report := &smokeLongReport{
		Leaf: smokeLongLeaf, Kind: "long_run", Window: window, Seed: seed,
		RaceEnabled: smokeCalibrationRaceEnabled(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		RunDir:      runDir, PlanPath: planPath,
		WallMS: run.WallMS, InitialBoot: run.InitialBootMS,
		Restarts: run.Restarts, Peak: run.Peak, ChildLogs: run.ChildLogs,
		Scale: smokeLongScale{
			Sessions: len(plan.Sessions), InputsPerSession: plan.InputsPerSession,
			InputsTotal: plan.inputs(), RestartsPerSession: plan.RestartsPerSession,
			RestartsTotal: plan.restarts(), Generations: plan.restarts() + 1,
			RestartEvery: plan.RestartEvery, DelegateInputs: smokeLongDelegateCount(plan),
			Faults: len(plan.Faults), ConsumerSession: plan.ConsumerSession,
			SharedHost: true, MeditationEnabled: true, FsyncDropped: false,
		},
		Storage: smokeCalibrationStorage{
			Root: cfg.StorageRoot, BeforeBytes: beforeBytes, AfterBytes: afterBytes,
			GrowthBytes: afterBytes - beforeBytes, BeforeFiles: beforeFiles, AfterFiles: afterFiles,
			FilesAdded: afterFiles - beforeFiles, Problems: problems,
		},
	}
	for _, gen := range run.Generations {
		report.Inputs = append(report.Inputs, gen.Inputs...)
		report.Faults = append(report.Faults, gen.Faults...)
		report.Reloads = append(report.Reloads, gen.Reloads...)
		report.Consumer = append(report.Consumer, gen.Consumer...)
		report.Concurrent = append(report.Concurrent, gen.Concurrent...)
		report.Folds = append(report.Folds, gen.Folds...)
		report.Outbound = append(report.Outbound, gen.Outbound...)
		if gen.Drain != nil {
			report.Drain = gen.Drain
		}
	}
	report.Recon = smokeLongReconcile(provider, plan, report)
	return report
}

// smokeLongReconcile 按计划逐身份核账：每个输入/重绑标记恰有一次入口请求，
// 每个非故障委派恰有一次结算通知，每个非故障输入与重绑恰有一次动作外发；
// 故障点只由负锚分类，丢弃即静默丢失；故障步可能一次都到不了模型，首尝试
// 单列、不并进重试数。
func smokeLongReconcile(provider *smokeProvider, plan smokeCalibrationPlan, report *smokeLongReport) smokeLongReconciliation {
	requests := provider.recorded()
	lasts := make([]string, len(requests))
	for i, req := range requests {
		lasts[i] = strings.TrimSpace(smokeLastUserText(req.Messages))
	}
	exact := func(marker string) int {
		n := 0
		for _, last := range lasts {
			if last == marker || strings.HasPrefix(last, marker+" ") {
				n++
			}
		}
		return n
	}
	recon := smokeLongReconciliation{
		ExpectedInputs: plan.inputs(), InputSamples: len(report.Inputs),
		ExpectedRebinds: (plan.restarts() + 1) * len(plan.Sessions),
		Faults:          len(plan.Faults), ProviderRequests: len(requests),
		DelegateInputs: smokeLongDelegateCount(plan),
	}
	for _, step := range plan.Timeline {
		if step.Kind != "input" {
			continue
		}
		got := exact(step.Marker)
		recon.InputRequests += got
		if smokeLongIsFaultStep(plan, step) {
			if got > 0 {
				recon.FaultInputAttempts++
			}
			if got > 1 {
				recon.FaultInputRetries += got - 1
			}
			continue
		}
		switch {
		case got == 0:
			recon.MissingInputRequests = append(recon.MissingInputRequests, step.Marker)
		case got > 1:
			recon.DuplicateInputRequests = append(recon.DuplicateInputRequests, fmt.Sprintf("%s x%d", step.Marker, got))
		}
	}
	for gen := 0; gen <= plan.restarts(); gen++ {
		inputs, _ := smokeLongGenerationSteps(plan, gen)
		if smokeLongConsumerMarker(plan, gen, inputs) != "" {
			recon.ExpectedConsumerTurns++
		}
	}
	for gen := 0; gen <= plan.restarts(); gen++ {
		for _, chat := range plan.Sessions {
			marker := smokeLongRebindMarker(plan, gen, chat)
			got := exact(marker)
			recon.RebindRequests += got
			switch {
			case got == 0:
				recon.MissingRebinds = append(recon.MissingRebinds, marker)
			case got > 1:
				recon.DuplicateRebinds = append(recon.DuplicateRebinds, fmt.Sprintf("%s x%d", marker, got))
			}
		}
	}
	for _, step := range plan.Timeline {
		if step.Kind != "input" || step.Shape != "delegate" || smokeLongIsFaultStep(plan, step) {
			continue
		}
		recon.ExpectedNotifications++
		got := 0
		for _, last := range lasts {
			if strings.HasPrefix(last, "tagent: task ") && strings.Contains(last, "settled") && strings.Contains(last, step.Marker) {
				got++
			}
		}
		recon.NotificationRequests += got
		switch {
		case got == 0:
			recon.MissingNotifications = append(recon.MissingNotifications, step.Marker)
		case got > 1:
			recon.DuplicateNotifications = append(recon.DuplicateNotifications, fmt.Sprintf("%s x%d", step.Marker, got))
		}
	}

	observed := map[string]int{}
	for _, evt := range report.Outbound {
		if evt.Kind != "text" {
			continue
		}
		for _, prefix := range []string{"answer ", "delegate ack ", "settle-ack "} {
			if !strings.HasPrefix(evt.Body, prefix) {
				continue
			}
			rest := strings.TrimSpace(evt.Body[len(prefix):])
			if rest == "" {
				continue
			}
			observed[prefix+strings.SplitN(rest, " ", 2)[0]]++
			break
		}
	}
	expect := func(want, marker string) {
		recon.ExpectedActions++
		key := want + " " + marker
		got := observed[key]
		recon.ActionEvents += got
		switch {
		case got == 0:
			recon.MissingActions = append(recon.MissingActions, key)
		case got > 1:
			recon.DuplicateActions = append(recon.DuplicateActions, fmt.Sprintf("%s x%d", key, got))
		}
	}
	for _, step := range plan.Timeline {
		if step.Kind != "input" || smokeLongIsFaultStep(plan, step) {
			continue
		}
		if step.Shape == "delegate" {
			expect("delegate ack", step.Marker)
			expect("settle-ack", step.Marker)
			continue
		}
		expect("answer", step.Marker)
	}
	for gen := 0; gen <= plan.restarts(); gen++ {
		for _, chat := range plan.Sessions {
			expect("answer", smokeLongRebindMarker(plan, gen, chat))
		}
	}

	for _, fault := range report.Faults {
		switch fault.Outcome {
		case "answered":
			recon.FaultsAnswered++
		case "failed_named":
			recon.FaultsNamed++
		default:
			recon.FaultsDropped = append(recon.FaultsDropped,
				fmt.Sprintf("%s/%s/round%d: %s", fault.Kind, fault.Session, fault.Round, fault.Outcome))
		}
	}
	recon.ConsumerTurns = len(report.Consumer)
	for _, turn := range report.Consumer {
		if turn.Answered {
			recon.ConsumerAnswered++
		}
	}
	recon.ConcurrentBatches = len(report.Concurrent)
	for _, batch := range report.Concurrent {
		recon.ConcurrentAnswered += batch.Answered
	}
	recon.Reloads = len(report.Reloads)
	for _, reload := range report.Reloads {
		if reload.Reloaded {
			recon.ReloadsInstalled++
		}
	}
	recon.FoldSamples = len(report.Folds)
	for _, sample := range report.Folds {
		recon.FoldSynthetics += sample.Synthetics
	}
	for _, req := range requests {
		if req.Model == smokeReflectModel {
			recon.ReflectRequests++
		}
	}
	return recon
}
