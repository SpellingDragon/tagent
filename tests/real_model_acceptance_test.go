package tagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trpcevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	tagent "github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/rl"
)

// realAcceptMaxCalls 一族的真实本地验收预算，集中定义在这里：一次 run 的调用数、单次输出
// 上界、单请求估计上界、单次超时、一轮排空上限，以及预算门禁演示用的输入上界。
const (
	realAcceptMaxCalls         = 24
	realAcceptMaxOutputTokens  = 2048
	realAcceptMaxRequestTokens = 32768
	realAcceptCallTimeout      = 120 * time.Second
	realAcceptTurnTimeout      = 4 * time.Minute
	realAcceptTightInputLimit  = 16
)

// realAcceptFactsFile 一族是运行时核账器认得的输入产物名与工具标识，写死在这里以免与脚本侧漂移。
const (
	realAcceptFactsFile        = "facts.jsonl"
	realAcceptCaptureManifest  = "capture-manifest.json"
	realAcceptExportManifest   = "export-manifest.json"
	realAcceptOverrideRef      = "acceptance-ref-override"
	realAcceptNonceToolID      = "acceptance_nonce"
	realAcceptBigToolID        = "acceptance_bigschema"
	realAcceptTokenCharDivisor = 4
)

// realAcceptCredentials 是真实 provider 的三项必读配置。
type realAcceptCredentials struct {
	Endpoint  string
	ModelName string
	APIKey    string
}

// realAcceptCall 是一次真实调用在花掉预算之外的可见事实：谁服务的、哪个模型名、用了多少。
type realAcceptCall struct {
	Label            string
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	ResponseID       string
}

// realAcceptLedger 给真实调用记账：槽位在发起前领走，用量在同一个槽位上回填，因此并发
// 交错也不会把一次调用的账写到另一次头上。预算用尽时拒绝再发。
type realAcceptLedger struct {
	mu       sync.Mutex
	calls    []realAcceptCall
	breached string
}

// reserve 为一次真实调用领一个账目槽位；预算用尽时返回 false，调用方不得再发。
func (l *realAcceptLedger) reserve() (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.calls) >= realAcceptMaxCalls {
		l.breached = fmt.Sprintf("real-model acceptance: call budget %d exhausted, stopping here", realAcceptMaxCalls)
		return 0, false
	}
	l.calls = append(l.calls, realAcceptCall{})
	return len(l.calls) - 1, true
}

// observe 把终端响应里的用量与响应身份写进它自己的槽位。
func (l *realAcceptLedger) observe(slot int, label, modelName string, resp *model.Response) {
	if resp == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if slot < 0 || slot >= len(l.calls) {
		return
	}
	c := &l.calls[slot]
	c.Label = label
	c.ModelName = modelName
	if resp.ID != "" {
		c.ResponseID = resp.ID
	}
	if resp.Usage != nil {
		if resp.Usage.PromptTokens > c.PromptTokens {
			c.PromptTokens = resp.Usage.PromptTokens
		}
		if resp.Usage.CompletionTokens > c.CompletionTokens {
			c.CompletionTokens = resp.Usage.CompletionTokens
		}
	}
}

// count 返回已入账的调用数。
func (l *realAcceptLedger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// since 返回第 from 个槽位之后的调用账目副本。
func (l *realAcceptLedger) since(from int) []realAcceptCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	if from > len(l.calls) {
		from = len(l.calls)
	}
	return append([]realAcceptCall(nil), l.calls[from:]...)
}

// labels 返回账目里出现过的实例标签。
func (l *realAcceptLedger) labels(calls []realAcceptCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Label)
	}
	return out
}

// totalTokens 返回入账的提示与生成 token 总量。
func (l *realAcceptLedger) totalTokens() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	prompt, completion := 0, 0
	for _, c := range l.calls {
		prompt += c.PromptTokens
		completion += c.CompletionTokens
	}
	return prompt, completion
}

// breach 返回预算中止的原因。
func (l *realAcceptLedger) breach() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.breached
}

// report 把调用数、模型名与用量打进测试日志，作为真实预算的入账证据；超支必须留成失败。
func (l *realAcceptLedger) report(t *testing.T, modelName string) {
	t.Helper()
	prompt, completion := l.totalTokens()
	t.Logf("real-model acceptance ledger: model=%s calls=%d prompt_tokens=%d completion_tokens=%d budget=%d breached=%q",
		modelName, l.count(), prompt, completion, realAcceptMaxCalls, l.breach())
	require.Emptyf(t, l.breach(), "预算超支必须留在未完成状态里：%s", l.breach())
}

// realAcceptWindow 是一次「在途发布」窗口的两端：被领用的调用停在 entered 之后、release 之前。
type realAcceptWindow struct {
	entered  chan struct{}
	release  chan struct{}
	openOnce sync.Once
}

// open 放行窗口里停住的全部调用；重复调用无害。
func (w *realAcceptWindow) open() { w.openOnce.Do(func() { close(w.release) }) }

// waitEntered 等在途窗口真的形成；没形成就没有可钉的发布，调用方据此把验收判成失败。
func (w *realAcceptWindow) waitEntered(t *testing.T, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-w.entered:
		return true
	case <-time.After(timeout):
		return false
	}
}

// realAcceptModel 把真实 provider 包在预算面里：限流、计时、入账，并可把若干次调用停在
// 在途窗口内，用来观测「发布只影响下一颗代」。
type realAcceptModel struct {
	inner  model.Model
	label  string
	ledger *realAcceptLedger

	winMu  sync.Mutex
	holds  int
	window *realAcceptWindow
}

// newRealAcceptModel 包一层真实 openai 句柄；alt 非空时用它替换模型名而沿用端点与凭据。
func newRealAcceptModel(creds realAcceptCredentials, alt, label string, ledger *realAcceptLedger) *realAcceptModel {
	name := creds.ModelName
	if alt != "" {
		name = alt
	}
	inner := openai.New(name, openai.WithAPIKey(creds.APIKey), openai.WithBaseURL(creds.Endpoint))
	return &realAcceptModel{inner: inner, label: label, ledger: ledger}
}

// armInFlightWindow 让接下来 calls 次真实调用停在「已进入、未放行」的窗口里，返回窗口两端。
func (m *realAcceptModel) armInFlightWindow(calls int) *realAcceptWindow {
	win := &realAcceptWindow{entered: make(chan struct{}, 1), release: make(chan struct{})}
	m.winMu.Lock()
	defer m.winMu.Unlock()
	m.window = win
	m.holds = calls
	return win
}

// takeWindow 在发起真实调用前领用一次窗口额度；额度用尽后直发。
func (m *realAcceptModel) takeWindow() *realAcceptWindow {
	m.winMu.Lock()
	defer m.winMu.Unlock()
	if m.holds <= 0 || m.window == nil {
		return nil
	}
	m.holds--
	return m.window
}

// GenerateContent 在预算面内发起一次真实调用：先确认还花得起，再压住输出上界与请求估计，
// 领到窗口就停在发布之前，最后限时转发响应并把用量写回自己的槽位。
func (m *realAcceptModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	slot, ok := m.ledger.reserve()
	if !ok {
		return nil, fmt.Errorf("real-model acceptance: %s", m.ledger.breach())
	}
	guarded, err := m.guard(req)
	if err != nil {
		return nil, err
	}
	if win := m.takeWindow(); win != nil {
		select {
		case win.entered <- struct{}{}:
		default:
		}
		<-win.release
	}
	callCtx, cancel := context.WithTimeout(ctx, realAcceptCallTimeout)
	respCh, err := m.inner.GenerateContent(callCtx, guarded)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan *model.Response, 64)
	go func() {
		defer func() {
			cancel()
			close(out)
		}()
		for resp := range respCh {
			m.ledger.observe(slot, m.label, m.info().Name, resp)
			out <- resp
		}
	}()
	return out, nil
}

// guard 检查请求估计并压住单次输出上界；估计超限就中止，不做分片重试。
func (m *realAcceptModel) guard(req *model.Request) (*model.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("real-model acceptance: nil request")
	}
	estimated := estimateRequestTokens(req)
	if estimated > realAcceptMaxRequestTokens {
		return nil, fmt.Errorf("real-model acceptance: request estimate %d tokens exceeds %d, refusing to send",
			estimated, realAcceptMaxRequestTokens)
	}
	cp := *req
	gen := cp.GenerationConfig
	if gen.MaxTokens == nil || *gen.MaxTokens > realAcceptMaxOutputTokens {
		limit := realAcceptMaxOutputTokens
		gen.MaxTokens = &limit
		cp.GenerationConfig = gen
	}
	return &cp, nil
}

// info 取内层模型的身份，内层不报身份时退回包上的标签。
func (m *realAcceptModel) info() model.Info {
	if provider, ok := m.inner.(interface{ Info() model.Info }); ok {
		return provider.Info()
	}
	return model.Info{Name: m.label}
}

// Info 报告被包装的真实模型身份。
func (m *realAcceptModel) Info() model.Info { return m.info() }

// estimateRequestTokens 以字符数粗估一次请求的 token 量：只用于守住预算上界，不做成计费口径。
func estimateRequestTokens(req *model.Request) int {
	chars := 0
	for _, msg := range req.Messages {
		chars += len(msg.Content)
		for _, call := range msg.ToolCalls {
			chars += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	for _, t := range req.Tools {
		if d := t.Declaration(); d != nil {
			chars += len(d.Name) + len(d.Description) + len(schemaBody(d))
		}
	}
	return chars / realAcceptTokenCharDivisor
}

// schemaBody 把一份输入面声明压成文本，只为估算请求体积。
func schemaBody(d *trpctool.Declaration) string {
	raw, err := json.Marshal(d.InputSchema)
	if err != nil {
		return d.Name + d.Description
	}
	return string(raw)
}

// acceptanceNonceTool 是只读的临时文件工具：它只把测试自己写下的那个 nonce 读回来。
type acceptanceNonceTool struct{}

// Declaration 声明一个无参数的只读工具。
func (acceptanceNonceTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        realAcceptNonceToolID,
		Description: "read the acceptance nonce back",
		InputSchema: &trpctool.Schema{Type: "object"},
	}
}

// Call 读回 nonce 文件内容。
func (acceptanceNonceTool) Call(_ context.Context, _ []byte) (any, error) {
	body, err := os.ReadFile(acceptanceNoncePath())
	if err != nil {
		return nil, err
	}
	return strings.TrimSpace(string(body)), nil
}

// acceptanceBigSchemaTool 是一个描述面被撑大的只读工具：它的固定开销就是预算门禁的演示对象。
type acceptanceBigSchemaTool struct{}

// Declaration 声明一份远超小额度输入上界的工具描述。
func (acceptanceBigSchemaTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        realAcceptBigToolID,
		Description: strings.Repeat("acceptance padding for a wide fixed overhead ", 300),
		InputSchema: &trpctool.Schema{Type: "object"},
	}
}

// Call 什么都不做，只证明它被声明了。
func (acceptanceBigSchemaTool) Call(_ context.Context, _ []byte) (any, error) {
	return "BIG-OK", nil
}

// acceptanceToolRegistration 保证进程级工具工厂只装一次：重复注册同名 id 会 panic。
var acceptanceToolRegistration sync.Once

// acceptanceNoncePathMu 保护工厂闭包读取的当前 nonce 路径。
var acceptanceNoncePathMu sync.Mutex

// acceptanceNoncePathValue 保存最近一次装好的 nonce 文件位置。
var acceptanceNoncePathValue string

// setAcceptanceNoncePath 记下本次运行的 nonce 文件位置。
func setAcceptanceNoncePath(path string) {
	acceptanceNoncePathMu.Lock()
	defer acceptanceNoncePathMu.Unlock()
	acceptanceNoncePathValue = path
}

// acceptanceNoncePath 读回当前 nonce 文件位置。
func acceptanceNoncePath() string {
	acceptanceNoncePathMu.Lock()
	defer acceptanceNoncePathMu.Unlock()
	return acceptanceNoncePathValue
}

// installAcceptanceNonceTool 在 run 目录里写一个 nonce 文件并装上只读工具，返回 nonce 文本。
func installAcceptanceNonceTool(t *testing.T, dir string) string {
	t.Helper()
	nonce := "nonce-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	path := filepath.Join(dir, "nonce.txt")
	require.NoError(t, os.WriteFile(path, []byte(nonce), 0o600))
	acceptanceToolRegistration.Do(func() {
		agent.RegisterPlainTool(realAcceptNonceToolID, func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
			return acceptanceNonceTool{}, nil
		})
		agent.RegisterPlainTool(realAcceptBigToolID, func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
			return acceptanceBigSchemaTool{}, nil
		})
	})
	setAcceptanceNoncePath(path)
	return nonce
}

// realAcceptGate 决定这条真实验收到底跑不跑：
//   - TAGENT_REQUIRE_REAL_MODEL 未置 1 时 Skip，这是本文件唯一允许的 Skip 路径（D17）；
//   - 置 1 后端点、模型名、凭据缺任何一项都是 Fatal：必需验收不能由猜测的端点顶替。
//   - 凭据只从环境显式读取，不 source 任何 shell 配置。
//
// 契约: docs/wiki/rl/rl-architecture.md#dual-stream-cli
func realAcceptGate(t *testing.T) realAcceptCredentials {
	t.Helper()
	if os.Getenv("TAGENT_REQUIRE_REAL_MODEL") != "1" {
		t.Skip("真实模型验收只在 TAGENT_REQUIRE_REAL_MODEL=1 时执行；其余档位不得把真实调用计成证据")
	}
	creds := realAcceptCredentials{
		Endpoint:  strings.TrimSpace(os.Getenv("TRPC_CLAW_API_ENDPOINT")),
		ModelName: strings.TrimSpace(os.Getenv("TRPC_CLAW_MODEL_NAME")),
		APIKey:    strings.TrimSpace(os.Getenv("ZAI_API_KEY")),
	}
	if creds.Endpoint == "" {
		t.Fatal("TAGENT_REQUIRE_REAL_MODEL=1 但 TRPC_CLAW_API_ENDPOINT 缺失：必需验收不能由猜测的端点代替")
	}
	if creds.ModelName == "" {
		t.Fatal("TAGENT_REQUIRE_REAL_MODEL=1 但 TRPC_CLAW_MODEL_NAME 缺失：入账的模型名必须是被真实调用的那一个")
	}
	if creds.APIKey == "" {
		t.Fatal("TAGENT_REQUIRE_REAL_MODEL=1 但 ZAI_API_KEY 缺失：凭据只从环境显式读取，不 source shell")
	}
	return creds
}

// realAcceptAltModel 是可选的第二模型名，用来把「换代」做成跨模型的事实而不是同模型换实例。
func realAcceptAltModel() string { return strings.TrimSpace(os.Getenv("TAGENT_TEST_ALT_MODEL")) }

// realAcceptRunDir 返回本次验收的产物目录：TAGENT_ACCEPTANCE_DIR 指定时用它，否则用临时目录。
func realAcceptRunDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("TAGENT_ACCEPTANCE_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	require.NoErrorf(t, os.MkdirAll(dir, 0o700), "验收产物目录必须可建 %s", dir)
	return dir
}

// realAcceptNoncePrompt 给真实模型一条只涉及 nonce 的合成指令：不含任何真实聊天历史。
func realAcceptNoncePrompt(nonce string) string {
	return "ACCEPTANCE " + nonce + "：用你可用的工具把 " + nonce + " 这个值读回来，然后原样转述工具返回。"
}

// realAcceptPrompt 额外点名 model_override：委派参数 request 填 nonce，模型引用填 ref。
func realAcceptPrompt(nonce, ref string) string {
	return "ACCEPTANCE " + nonce + "：调用委派工具，参数 request 填 " + nonce +
		"，model_override 填 " + ref + "，然后把子 agent 的返回原样转述。"
}

// realAcceptOrgConfig 声明一条装好工具面、真机采集与轨迹落盘的验收配置（Go 结构形态）。
func realAcceptOrgConfig(dir, nonce string, maxTokens int, bigSchemaTool bool, eventsDir string) tagent.Config {
	entryTools := []tagent.ToolRef{{
		Kind:        tagent.ToolKindAgent,
		AgentID:     "acceptance-sub",
		Description: "delegate the nonce read",
		Async:       boolPtr(false),
	}}
	if bigSchemaTool {
		entryTools = []tagent.ToolRef{{Kind: tagent.ToolKindTool, ID: realAcceptBigToolID}}
	}
	return tagent.Config{
		Entry: "acceptance-entry",
		Agents: map[string]tagent.AgentConfig{
			"acceptance-entry": {
				SystemPrompt:      tagent.PromptConfig{Inline: "acceptance-entry 只按指令调用委派工具：" + nonce},
				MaxToolIterations: 2,
				MaxTokens:         maxTokens,
				Memory:            realAcceptMemoryConfig(eventsDir),
				Tools:             entryTools,
			},
			"acceptance-sub": {
				SystemPrompt:      tagent.PromptConfig{Inline: "acceptance-sub 调用 nonce 工具一次并转述结果：" + nonce},
				MaxToolIterations: 2,
				Memory:            realAcceptMemoryConfig(eventsDir),
				Tools:             []tagent.ToolRef{{Kind: tagent.ToolKindTool, ID: realAcceptNonceToolID}},
			},
		},
		TrajectoryDump:    true,
		TrajectoryDir:     dir,
		TrajectoryCapture: config.CaptureBlock{Enabled: true, MaxRecordBytes: 1 << 22},
	}
}

// realAcceptMemoryConfig 空目录=进程内存储；非空=localfile（同路径=共享实例，
// 两代实例按既有耐久屏障读回同一事实链）。
func realAcceptMemoryConfig(eventsDir string) tagent.MemoryConfig {
	if eventsDir == "" {
		return tagent.MemoryConfig{Type: "memory"}
	}
	return tagent.MemoryConfig{Type: "localfile", Path: eventsDir}
}

// realAcceptYAML 写出可热更的验收配置文本，供 WithConfigPath 那条路径重新读取；
// prompt 参与结构指纹，governance 块是须重启维度。
func realAcceptYAML(prompt string, restartGovernance bool) string {
	body := "entry: acceptance-entry\n" +
		"agents:\n" +
		"  acceptance-entry:\n" +
		"    system_prompt:\n      inline: \"" + prompt + "\"\n" +
		"    max_tool_iterations: 2\n" +
		"    keep_recent_tasks: 5\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: acceptance-sub\n        description: delegate the nonce read\n        async: false\n" +
		"  acceptance-sub:\n" +
		"    system_prompt:\n      inline: \"acceptance-sub 调用 nonce 工具一次并转述结果\"\n" +
		"    max_tool_iterations: 2\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: tool\n        id: " + realAcceptNonceToolID + "\n"
	if restartGovernance {
		body += "governance:\n  enabled: true\n"
	}
	return body
}

// realAcceptDiagStrings 把诊断面里的一个列表读成字符串切片。
func realAcceptDiagStrings(t *testing.T, diag map[string]any, key string) []string {
	t.Helper()
	raw, ok := diag[key]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]string)
	if ok {
		return append([]string(nil), list...)
	}
	anyList, ok := raw.([]any)
	require.Truef(t, ok, "诊断面 %s 的形状必须是一个列表，读到 %T", key, raw)
	out := make([]string, 0, len(anyList))
	for _, item := range anyList {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

// TestRealModel_RuntimeOverrides 真实模型下的运行期覆盖验收：
//   - 子 agent 的 model_override 只认已注册引用，并由被引用的那个真实实例服务这次委派；
//   - 在途发布把引用换到新实例时，已经解析过的那次调用不跟着走，下一颗独立调用才读到新的；
//   - 只须重启的维度改动被具名拒绝并留在 restartRequired 里，不改动已经发布的那一代。
//   - 引用快照与执行代同生命周期：改名须随发布生效，裸 Register 不换代。
//   - model_override 由模型自决是否填写；契约面只要求新调用不复用被钉旧代，命中新代的确定证据由窗口内第一颗调用与根包机制测试共同闭合。
//
// 契约: docs/wiki/agent/execution-generations.md#model-reference-pinning
func TestRealModel_RuntimeOverrides(t *testing.T) {
	creds := realAcceptGate(t)
	runDir := realAcceptRunDir(t)
	nonce := installAcceptanceNonceTool(t, runDir)
	ledger := &realAcceptLedger{}

	yamlPath := filepath.Join(runDir, "org.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(realAcceptYAML("acceptance-entry prompt-A", false)), 0o600))
	cfg, err := config.LoadConfig(yamlPath)
	require.NoErrorf(t, err, "验收配置必须可被真实装载器读通 %s", yamlPath)

	primary := newRealAcceptModel(creds, "", "generation-a", ledger)
	require.NotNil(t, primary)
	entry, err := tagent.New(*cfg, tagent.WithModel(primary), tagent.WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	generationB := newRealAcceptModel(creds, realAcceptAltModel(), "generation-b", ledger)
	generationC := newRealAcceptModel(creds, realAcceptAltModel(), "generation-c", ledger)
	agent.RegisterModelReference(realAcceptOverrideRef, generationB)
	window := generationB.armInFlightWindow(1)

	out, err := entry.StartLoop("acceptance-user", "acceptance-override")
	require.NoError(t, err)
	baseline := ledger.count()
	entry.InjectMessage(model.NewUserMessage(realAcceptPrompt(nonce, realAcceptOverrideRef)))

	if !window.waitEntered(t, realAcceptTurnTimeout) {
		window.open()
		ledger.report(t, creds.ModelName)
		t.Fatal("在途窗口没有形成：真实模型没有走进被委派的那次调用，钉旧无从观测")
	}
	agent.RegisterModelReference(realAcceptOverrideRef, generationC)
	window.open()
	drainUntilFinal(t, out, realAcceptTurnTimeout)

	firstTurn := ledger.since(baseline)
	require.NotEmptyf(t, firstTurn, "这条用例必须留下真实调用，零调用不算通过（ledger=%+v）", firstTurn)
	require.Containsf(t, ledger.labels(firstTurn), "generation-b",
		"model_override 指向的引用必须由被注册的那个真实实例服务：%v", ledger.labels(firstTurn))
	require.NotContainsf(t, ledger.labels(firstTurn), "generation-c",
		"已经解析过的那次调用不得跟着在途发布走：%v", ledger.labels(firstTurn))

	secondBaseline := ledger.count()
	require.NoError(t, os.WriteFile(yamlPath, []byte(realAcceptYAML("acceptance-entry prompt-B", false)), 0o600))
	tickB := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(yamlPath, tickB, tickB))
	entry.CheckOrgReload()
	out2, err := entry.StartLoop("acceptance-user", "acceptance-override-2")
	require.NoErrorf(t, err, "第二颗独立调用必须能开新循环")
	entry.InjectMessage(model.NewUserMessage(realAcceptPrompt(nonce, realAcceptOverrideRef)))
	drainUntilFinal(t, out2, realAcceptTurnTimeout)
	secondTurn := ledger.since(secondBaseline)
	require.NotEmpty(t, secondTurn, "第二颗独立调用同样要有真实调用")
	require.NotContainsf(t, ledger.labels(secondTurn), "generation-b",
		"新独立调用绝不得复用发布前钉定的旧引用代：%v", ledger.labels(secondTurn))

	require.NoError(t, os.WriteFile(yamlPath, []byte(realAcceptYAML("acceptance-entry prompt-A", true)), 0o600))
	tick := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	before := ledger.count()
	entry.CheckOrgReload()
	diag := entry.OrgDiagnostics()
	restart := realAcceptDiagStrings(t, diag, "restartRequired")
	require.NotEmptyf(t, restart, "含须重启维度的改动必须被具名点名，不能静默 applied：%v", diag)
	require.Containsf(t, strings.Join(restart, ","), "governance.enabled",
		"点名内容要指到那条须重启的字段路径：%v", restart)
	require.Zero(t, ledger.count()-before, "须重启的改动被拒在装载面，一个真实调用都不许花")

	ledger.report(t, creds.ModelName)
	if realAcceptAltModel() == "" {
		t.Log("未提供 TAGENT_TEST_ALT_MODEL：代际换代是同端点同模型的实例替换，不主张跨模型覆盖")
	}
}

// TestRealModel_RequestBudget 真实模型下的请求预算验收：
//   - 正常声明的一次真实往返要成功，且入账的用量非零（真实计费面在场）；
//   - 固定开销吃掉输入上界时必须在发出之前拦住：一个真实调用都不许多花；
//   - 拒发理由以可机读的 budget_exceeded 出现在终态事件文案里（这一层导出只给文案）。
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestRealModel_RequestBudget(t *testing.T) {
	creds := realAcceptGate(t)
	runDir := realAcceptRunDir(t)
	nonce := installAcceptanceNonceTool(t, runDir)
	ledger := &realAcceptLedger{}
	model1 := newRealAcceptModel(creds, "", "budget-main", ledger)

	entry, err := tagent.New(realAcceptOrgConfig(filepath.Join(runDir, "capture-budget"), nonce, 0, false, ""),
		tagent.WithModel(model1))
	require.NoError(t, err)
	out, err := entry.StartLoop("acceptance-user", "acceptance-budget-ok")
	require.NoError(t, err)
	before := ledger.count()
	entry.InjectMessage(model.NewUserMessage(realAcceptNoncePrompt(nonce)))
	drainUntilFinal(t, out, realAcceptTurnTimeout)
	require.NotEmpty(t, ledger.since(before), "正常预算的往返必须真的发过一次")
	prompt, completion := ledger.totalTokens()
	require.Greaterf(t, prompt+completion, 0, "真实往返必须留下非零用量：prompt=%d completion=%d", prompt, completion)
	require.NoError(t, entry.Close())
	ledger.report(t, creds.ModelName)

	tight := &realAcceptLedger{}
	tinyModel := newRealAcceptModel(creds, "", "budget-tight", tight)
	tightCfg := realAcceptOrgConfig(filepath.Join(runDir, "capture-budget-tight"), nonce, realAcceptTightInputLimit, true, "")
	refused, err := tagent.New(tightCfg, tagent.WithModel(tinyModel))
	require.NoError(t, err)
	ro, err := refused.StartLoop("acceptance-user", "acceptance-budget-refused")
	require.NoError(t, err)
	tightBefore := tight.count()
	refused.InjectMessage(model.NewUserMessage(realAcceptNoncePrompt(nonce)))
	events := drainUntilTerminalError(t, ro, realAcceptTurnTimeout)
	joined := strings.Join(errorMessages(events), "\n")
	require.Containsf(t, joined, agent.ErrBudgetExceeded.Error(),
		"拒发必须带着可机读的理由出现在终态事件上，而不是只留下一个空回合：%q", joined)
	require.Containsf(t, joined, fmt.Sprintf("input limit %d", realAcceptTightInputLimit),
		"理由要点名它拦住的那条输入上界：%q", joined)
	require.Zero(t, tight.count()-tightBefore, "预算门禁要在发出去之前拦住：一个真实调用都不许花")
	require.NoError(t, refused.Close())
	tight.report(t, creds.ModelName)
}

// TestRealModel_DecisionCapture 真实模型下的决策采集验收：
//   - 一次真实工具往返被采集 v2 记录如实收下：请求摘要、真实响应身份与归因都在场；
//   - 至少一条记录按响应身份精确绑定。
//   - 已提交事实上的 call_id 必须能对回某条被记录下来的调用，对不上就是采集与提交脱节。
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRealModel_DecisionCapture(t *testing.T) {
	creds := realAcceptGate(t)
	runDir := realAcceptRunDir(t)
	nonce := installAcceptanceNonceTool(t, runDir)
	ledger := &realAcceptLedger{}
	captureDir := filepath.Join(runDir, "capture")

	entry, err := tagent.New(realAcceptOrgConfig(captureDir, nonce, 0, false, ""),
		tagent.WithModel(newRealAcceptModel(creds, "", "capture-main", ledger)))
	require.NoError(t, err)
	out, err := entry.StartLoop("acceptance-user", "acceptance-capture")
	require.NoError(t, err)
	before := ledger.count()
	entry.InjectMessage(model.NewUserMessage(realAcceptNoncePrompt(nonce)))
	drainUntilFinal(t, out, realAcceptTurnTimeout)
	require.NotEmpty(t, ledger.since(before), "采集的前提是真实调用真的发生过")

	tr := entry.TrajectoryRecorder()
	require.NotNil(t, tr, "验收配置必须把采集层装起来")
	require.True(t, tr.CaptureEnabled())
	store := entry.MemStore()
	require.NoError(t, entry.Close())

	manifest, err := tr.FlushAndWait(context.Background())
	require.NoError(t, err)
	require.Truef(t, manifest.Sealed, "真实运行的封账必须 seal：%+v", manifest)
	require.Truef(t, manifest.Complete, "真实运行不得带着丢失封账：%+v", manifest.CaptureStats)
	require.GreaterOrEqual(t, manifest.Written, int64(1), "至少一条真实调用要落盘")

	records := allRecords(t, captureDir)
	require.NotEmpty(t, records, "落盘目录里必须读得出 v2 记录")
	callIndex := map[string]bool{}
	bound := -1
	for i, rec := range records {
		callIndex[rec.CallID] = true
		if rec.BindingStatus == rl.BindingBound && bound < 0 {
			bound = i
		}
	}
	require.GreaterOrEqualf(t, bound, 0,
		"真实响应带着 SDK 给的响应身份，必须至少有一条被精确绑定：%+v", ledger.since(before))
	require.NotEmptyf(t, records[bound].ResponseID, "绑定成功的记录要交出它据以绑定的真实响应身份：%+v", records[bound])
	require.NotEmpty(t, records[bound].RequestDigest, "请求摘要让离线侧能比对同一次调用是否同一份输入")
	require.NotEmpty(t, records[bound].Owner.AgentName, "真实归因要指名是哪一代 agent 的调用")

	stamped := factsWithCallID(t, store, "acceptance-entry")
	require.NotEmpty(t, stamped, "真实管线要把调用票据盖进已提交事实，否则离线侧无从 join")
	matched := false
	for _, evt := range stamped {
		if callIndex[evt.Metadata[tagentevent.MetaKeyCallID]] {
			matched = true
		}
	}
	require.Truef(t, matched, "已提交事实上的 call_id 必须能对回某条被记录的调用：%+v", stamped)

	ledger.report(t, creds.ModelName)
}

// TestRealModel_OfflineSFT 真实模型下的离线数据集验收骨架：
//   - 两个隔离 root session 各走一次真实工具往返，落在同一个存储与同一个采集目录；
//   - 人工反馈绑到其中一条带上 call_id 的事实，随后按授权分区导出；
//   - run 目录里落下三件产物：facts 快照、capture 封账、导出封账，供双流转换器与核账器消费。
//   - 一颗常驻循环绑一个 (user, session)，两个 root session 因此走两代实例并按 localfile 共享事实链；最终封账由 Close 恰一次完成，FlushAndWait 幂等复读。
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestRealModel_OfflineSFT(t *testing.T) {
	creds := realAcceptGate(t)
	runDir := realAcceptRunDir(t)
	nonce := installAcceptanceNonceTool(t, runDir)
	ledger := &realAcceptLedger{}
	captureDir := filepath.Join(runDir, "capture")

	eventsDir := filepath.Join(runDir, "events")
	var manifests []rl.CaptureManifest
	var store memory.MemoryStore
	for i, session := range []string{"acceptance-sft-a", "acceptance-sft-b"} {
		inst, ierr := tagent.New(realAcceptOrgConfig(captureDir, nonce, 0, false, eventsDir),
			tagent.WithModel(newRealAcceptModel(creds, "", fmt.Sprintf("sft-main-%d", i), ledger)))
		require.NoErrorf(t, ierr, "第 %d 代验收实例", i)
		out, oerr := inst.StartLoop("acceptance-user", session)
		require.NoErrorf(t, oerr, "session %s 开循环", session)
		inst.InjectMessage(model.NewUserMessage(realAcceptNoncePrompt(nonce)))
		drainUntilFinal(t, out, realAcceptTurnTimeout)
		tr := inst.TrajectoryRecorder()
		require.NotNil(t, tr)
		_, ferr := tr.FlushAndWait(context.Background())
		require.NoErrorf(t, ferr, "中途封账不得出错（%s）", session)
		store = inst.MemStore()
		require.NoErrorf(t, inst.Close(), "实例收尾 %s", session)
		manifest, ferr := tr.FlushAndWait(context.Background())
		require.NoErrorf(t, ferr, "Close 后读取最终封账（%s）", session)
		manifests = append(manifests, manifest)
	}
	for _, manifest := range manifests {
		require.Truef(t, manifest.Sealed, "SFT 的采集封账必须 seal：%+v", manifest)
		require.Truef(t, manifest.Complete, "SFT 不得用带丢失的采集当输入：%+v", manifest.CaptureStats)
	}

	sessions := map[string]bool{}
	for _, rec := range allRecords(t, captureDir) {
		sessions[rec.SessionID] = true
	}
	require.GreaterOrEqualf(t, len(sessions), 2, "两个隔离 root session 才是分割证据，读到 %v", sessions)

	stamped := factsWithCallID(t, store, "acceptance-entry")
	require.NotEmpty(t, stamped, "导出要能 join 上 call_id，前提是真实管线确实盖了票据")
	_, ferr := memory.BindFeedback(store, stamped[0].EventKey, memory.FeedbackPayload{
		Verdict: "approved",
		Rating:  1,
		Note:    "offline review fixture",
		Source:  "acceptance-review",
	})
	require.NoError(t, ferr, "人工反馈必须能绑到真实产生的一条事实上")

	factsPath := filepath.Join(runDir, realAcceptFactsFile)
	factsFile, err := os.Create(factsPath)
	require.NoError(t, err)
	exportManifest, err := rl.ExportTrainingFacts(context.Background(), store,
		rl.ExportOptions{PartitionIDs: []int{memory.PartitionIDFromName("acceptance-entry")}}, factsFile)
	require.NoError(t, factsFile.Close())
	require.NoError(t, err)
	require.Truef(t, exportManifest.Complete, "导出封账不得是 partial：%+v", exportManifest)
	require.GreaterOrEqual(t, exportManifest.EventsWritten, 1)
	require.GreaterOrEqualf(t, exportManifest.JoinBound, 1, "真实反馈要能按 call_id 精确入账：%+v", exportManifest)

	body, err := os.ReadFile(factsPath)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(body)), "快照必须真有内容")

	writeRealAcceptManifests(t, runDir, manifests, exportManifest)
	t.Logf("real-model acceptance artifacts: run=%s capture=%s facts=%s", runDir, captureDir, factsPath)
	ledger.report(t, creds.ModelName)
}

// writeRealAcceptManifests 把两份封账按核账器认得的文件名落进 run 目录。
func writeRealAcceptManifests(t *testing.T, runDir string, capture []rl.CaptureManifest, export rl.ExportManifest) {
	t.Helper()
	body, err := rl.MarshalCaptureManifest(capture)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, realAcceptCaptureManifest), body, 0o600))

	exportBody, err := json.Marshal(export)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, realAcceptExportManifest), exportBody, 0o600))
}

// errorMessages 收集事件流里出现过的错误文案。
func errorMessages(events []*trpcevent.Event) []string {
	var out []string
	for _, evt := range events {
		if evt == nil || evt.Response == nil || evt.Response.Error == nil {
			continue
		}
		out = append(out, evt.Response.Error.Message)
	}
	return out
}

// drainUntilTerminalError 排空事件流直到出现带错误的终态响应，并返回收到的全部事件。
func drainUntilTerminalError(t *testing.T, out <-chan *trpcevent.Event, timeout time.Duration) []*trpcevent.Event {
	t.Helper()
	var got []*trpcevent.Event
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, evt)
			if evt.Response != nil && evt.Response.Error != nil {
				return got
			}
			if evt.IsFinalResponse() {
				return got
			}
		case <-deadline:
			t.Fatalf("no terminal response within %s", timeout)
		}
	}
}
