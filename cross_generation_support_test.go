// cross_generation_support 是跨代发布族的共享 stub 模型与 fixture：零 Test 声明，不参与职责同位计数。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// entryGeneration returns the entry cm's ACTIVE (non-retired) generation id, or -1.
func entryGeneration(t *testing.T, entry *agent.TagentAgent) int64 {
	t.Helper()
	for _, g := range entry.ContextManager().ExecutorRefs().Generations {
		if !g.Retired {
			return g.Generation
		}
	}
	return -1
}

// hasGeneration reports whether a generation row with this id is still on the books
// (a retired generation disappears once its own reference count reaches zero — the
// reclaim gate reads exactly this accounting).
func hasGeneration(entry *agent.TagentAgent, id int64) bool {
	for _, g := range entry.ContextManager().ExecutorRefs().Generations {
		if g.Generation == id {
			return true
		}
	}
	return false
}

func crossWrite(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// noToolModel answers every request with a final text message and NEVER issues a tool
// call, regardless of what tools it is offered. It records HOW MANY times each caller
// (by system label) reached the model — which is how the negative control observes that
// a delegation target never actually ran (a sub-agent only reaches the model when its
// delegation is invoked).
type noToolModel struct {
	mu      sync.Mutex
	calls   map[string]int
	offered map[string][]string
}

func (m *noToolModel) record(label string, tools []string) {
	m.mu.Lock()
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	if m.offered == nil {
		m.offered = map[string][]string{}
	}
	m.calls[label]++
	if _, ok := m.offered[label]; !ok {
		m.offered[label] = tools
	}
	m.mu.Unlock()
}

func (m *noToolModel) count(label string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[label]
}

func (m *noToolModel) offeredTools(label string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offered[label]
}

func (m *noToolModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	m.record(label, delegToolNames(req))
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "final answer, no tool"}}}}
	close(ch)
	return ch, nil
}

func (m *noToolModel) Info() model.Info { return model.Info{Name: "no-tool-model"} }

// ackSettleModel is the witness that the hardest cross-generation row needs and
// the shared mock cannot provide: a detached run's result comes back as a
// USER-role `[task settled] … 结果: …` notification (measured — not as a tool
// result, so `delegServed.ToolResults` is blind to it). This model records, per
// ENTRY call, which tools that call was offered and whether its request carried
// the settle notification, so the row can be attributed by causality: the turn
// that holds the background answer is the settle-driven turn, and the tool set
// offered to IT is the generation that turn runs on.
type ackSettleModel struct {
	mu      sync.Mutex
	calls   []ackEntryCall
	subRuns int
	gate    chan struct{}
}

type ackEntryCall struct {
	tools   []string
	settled bool
}

func (m *ackSettleModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)
	joined := strings.Builder{}
	for _, mm := range req.Messages {
		joined.WriteString(string(mm.Role))
		joined.WriteByte(' ')
		joined.WriteString(mm.Content)
		joined.WriteByte('\n')
	}
	text := joined.String()

	m.mu.Lock()
	if label == "SUB-B-PROMPT" {
		m.subRuns++
	}
	settled := strings.Contains(text, "[task settled]") && strings.Contains(text, "served:SUB-B-PROMPT")
	offer := ""
	if label == "ENTRY-A-PROMPT" {
		if len(m.calls)%2 == 0 && len(tools) > 0 {
			offer = tools[0]
		}
		m.calls = append(m.calls, ackEntryCall{tools: tools, settled: settled})
	}
	g := m.gate
	m.mu.Unlock()

	if label == "SUB-B-PROMPT" && g != nil {
		select {
		case <-g:
		case <-time.After(30 * time.Second):
		}
	}

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "served:" + label}}}}
	}
	close(ch)
	return ch, nil
}

func (m *ackSettleModel) Info() model.Info { return model.Info{Name: "ack-settle-model"} }

func (m *ackSettleModel) snapshot() ([]ackEntryCall, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ackEntryCall(nil), m.calls...), m.subRuns
}

// plainTextPullModel answers every request with plain text: the tests below hold a call
// open with a real LEASE, so nothing depends on model latency.
type plainTextPullModel struct{}

func (m *plainTextPullModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("ok")}}}
	close(ch)
	return ch, nil
}

func (m *plainTextPullModel) Info() model.Info { return model.Info{Name: "d52-pull"} }

// aliasSpellingYAML renders entry "main" routing sub1 (and optionally sub2 with its own
// numbers) using the requested tool-ref spelling: "explicit" writes `kind: agent`,
// "omitted" leaves the kind out — the same orchestration, spelled differently.
func aliasSpellingYAML(spelling string, keepMain int, withSub2 bool, keepSub2, maxSub2 int) string {
	ref := "      - kind: agent\n        agent: sub1\n        description: \"delegate-sub1\"\n"
	if spelling == "omitted" {
		ref = "      - agent: sub1\n        description: \"delegate-sub1\"\n"
	}
	sub2Ref := ""
	if withSub2 {
		sub2Ref = "      - kind: agent\n        agent: sub2\n        description: \"delegate-sub2\"\n"
	}
	sub2Def := ""
	if withSub2 {
		sub2Def = fmt.Sprintf("  sub2:\n    system_prompt:\n      inline: \"SUB2-D52\"\n    keep_recent_tasks: %d\n    max_tokens: %d\n    compress_threshold: 0.5\n    memory:\n      type: memory\n", keepSub2, maxSub2)
	}
	return "entry: main\nagents:\n  main:\n" +
		"    system_prompt:\n      inline: \"MAIN-D52\"\n" +
		fmt.Sprintf("    keep_recent_tasks: %d\n", keepMain) +
		"    memory:\n      type: memory\n" +
		"    tools:\n" + ref + sub2Ref +
		"  sub1:\n    system_prompt:\n      inline: \"SUB1-D52\"\n    memory:\n      type: memory\n" +
		sub2Def
}

// remoteSpellingYAML is a remote-only reference — legal with no local definition
// at all — written with or without the explicit kind.
func remoteSpellingYAML(spelling, endpoint string) string {
	ref := "      - kind: agent\n        agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	if spelling == "omitted" {
		ref = "      - agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n    tools:\n" + ref +
		fmt.Sprintf("        remote:\n          url: %q\n", endpoint)
}

func writeAliasConfig(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// addTwoYAML renders entry delegating to `base` plus two new agents. The second one
// ("zzbadagent") has an un-creatable localfile store path, so its RESIDENT build fails
// mid-candidate — after the first add has been built and merged.
func addTwoYAML(t testing.TB, base []string, okName, badName string) string {
	t.Helper()
	y := addOneYAML(t, base, okName)
	return strings.Replace(y,
		"description: \""+okName+"\"",
		"description: \""+okName+"\"\n      - kind: agent\n        agent: "+badName+"\n        description: \""+badName+"\"", 1) +
		badAgentDef(badName)
}

// addOneYAML renders the same org with just one added agent (memory path per agent:
// two agents sharing an empty path would collide on one store owner and mask the
// failure this scenario is about).
func addOneYAML(t testing.TB, base []string, name string) string {
	t.Helper()
	var toolLines, defs strings.Builder
	for _, n := range append(append([]string{}, base...), name) {
		toolLines.WriteString("      - kind: agent\n        agent: " + n + "\n        description: \"" + n + "\"\n")
	}
	defs.WriteString(orgHead())
	defs.WriteString(toolLines.String())
	defs.WriteString(subDef(t, base[0]))
	defs.WriteString(agentDef(t, name, "inline: \"ok agent\""))
	return defs.String()
}

func orgHead() string {
	return "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
}

// subDef renders the startup sub-agent exactly as ownerYAML defines it, so its memory
// section stays byte-identical across candidates (otherwise the memory rule refuses
// the candidate for the wrong reason).
func subDef(t testing.TB, name string) string {
	t.Helper()
	return agentDef(t, name, "inline: \""+name+"\"")
}

func agentDef(t testing.TB, name, prompt string) string {
	t.Helper()
	return "  " + name + ":\n    system_prompt:\n      " + prompt +
		fmt.Sprintf("\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "own-"+name))
}

// badAgentDef is a schema-valid agent whose store cannot be created: config parse and
// Validate pass, so the failure lands in buildAgent ("agent %q: create memory store")
// — late enough that an earlier add in the same candidate is already merged.
//
// 曾试过用“提示词文件缺失”作为构造失败点，但 loader 是 load-if-present（缺失仅
// INFO 跳过），不会失败；一个不存在的存储路径才是确定性的构造期失败。
func badAgentDef(name string) string {
	return "  " + name + ":\n    system_prompt:\n      inline: \"bad\"\n    memory:\n      type: localfile\n      path: \"/dev/null/" + name + "-store\"\n"
}

// chainDelegModel delegates to whichever offered tool names one of the agents in
// `prefer` (priority order). The framework does not promise tool ordering, so a
// mock that grabs tools[0] would assert an accident of slice order rather than the
// orchestration; naming the target makes each level's delegation deterministic at
// every depth.
type chainDelegModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
}

func (m *chainDelegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, want := range m.prefer {
			for _, t := range tools {
				if t == want {
					offer = t
					break
				}
			}
			if offer != "" {
				break
			}
		}
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "served:" + label}}}}
	close(ch)
	return ch, nil
}

func (m *chainDelegModel) Info() model.Info { return model.Info{Name: "chain-deleg-model"} }

func (m *chainDelegModel) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]delegServed, len(m.served))
	copy(out, m.served)
	return out
}

// reentryChainYAML builds a→b→c. b carries the PRODUCTION task tools, so the
// re-entry under test is the same object the LLM would call. c is delegated
// asynchronously (default), which is what puts a subagent task on **b's own**
// board rather than on the entry's.
func reentryChainYAML(cPrompt string, dropC bool) string {
	bTools := "      - kind: agent\n        agent: c\n        description: delegate-c\n      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n"
	if dropC {
		bTools = "      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n"
	}
	cDef := ""
	if !dropC {
		cDef = fmt.Sprintf("  c:\n    system_prompt:\n      inline: %q\n    memory:\n      type: memory\n", cPrompt)
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    memory:\n      type: memory\n    tools:\n      - kind: agent\n        agent: b\n        description: delegate-b\n" +
		"  b:\n    system_prompt:\n      inline: \"SUB-B\"\n    memory:\n      type: memory\n    tools:\n" + bTools + cDef
}

func writeReentryChain(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// startReentryChain runs one full a→b→c delegation and returns the org plus b's
// resident owner with the subagent task it left on b's OWN board.
func startReentryChain(t *testing.T, yamlPath string) (entry *agent.TagentAgent, b *agent.TagentAgent, taskID string, m *chainDelegModel) {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m = &chainDelegModel{prefer: []string{"b", "c"}}
	entry, err = New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)

	out, err := entry.StartLoop("u", "reentry-chain-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
	require.NoError(t, err)
	waitFor(t, "the nested delegation settled on c", func() bool { return countServed(m.snapshot(), "SUB-C") >= 1 })

	b = residentCacheForTest(entry)["b"]
	require.NotNil(t, b, "b is a resident owner")
	var found *task.Task
	for _, tk := range b.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			found = tk
			break
		}
	}
	require.NotNil(t, found, "the subagent task belongs to b's own manager, not the entry's")
	return entry, b, found.ID, m
}

// noDelegation 关掉本模型的一切委派，使断言只能被被测动作满足，不被无关轮次顺带达标。
func (m *chainDelegModel) noDelegation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefer = nil
}

const (
	walReentryPhaseEnv = "TAGENT_WAL42_PHASE"
	walReentryYamlEnv  = "TAGENT_WAL42_YAML"
	walReentryFilter   = "TestRelaunchAfterRestartResolvesOnTheCurrentFace$"
)

// walReentryYAML renders a→b→c with EVERY agent on its own localfile store, so the
// fact chain physically survives the process that wrote it. b carries the
// PRODUCTION task tools (relaunch/resume) and delegates to c asynchronously
// (default), which is what puts the subagent task on b's own board.
func walReentryYAML(root string, dropC bool) string {
	mem := func(name string) string {
		return fmt.Sprintf("    memory:\n      type: localfile\n      path: %q\n", filepath.Join(root, "store-"+name))
	}
	cRef := "      - kind: agent\n        agent: c\n        description: delegate-c\n"
	cDef := "  c:\n    system_prompt:\n      inline: \"SUB-C\"\n" + mem("c")
	if dropC {
		cRef, cDef = "", ""
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n" + mem("a") +
		"    tools:\n      - kind: agent\n        agent: b\n        description: delegate-b\n" +
		"  b:\n    system_prompt:\n      inline: \"SUB-B\"\n" + mem("b") +
		"    tools:\n" + cRef +
		"      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n" + cDef
}

// walReentryModel delegates to a named target at every level (never tools[0], whose order
// the framework does not promise) and can park c's FIRST call so the task stays un-settled.
type walReentryModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
	gate    chan struct{}
	gated   bool
}

// GenerateContent is the WAL-reentry mock. It holds the producer open while a gate
// is armed: the delegated task must NOT reach a terminal settle during that window, so
// the board the next boot folds really still carries it.
func (m *walReentryModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, want := range m.prefer {
			for _, t := range tools {
				if t == want {
					offer = t
					break
				}
			}
			if offer != "" {
				break
			}
		}
	}
	park := m.gate
	if label == "SUB-C" && park != nil && !m.gated {
		m.gated = true
	} else {
		park = nil
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	if park != nil {
		select {
		case <-park:
		case <-time.After(60 * time.Second):
		}
	}

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "served:" + label}}}}
	}
	close(ch)
	return ch, nil
}

func (m *walReentryModel) Info() model.Info { return model.Info{Name: "walReentry-model"} }

func (m *walReentryModel) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]delegServed(nil), m.served...)
}

// firstSubagentTask reads the subagent task off the owner's OWN board.
func firstSubagentTask(b *agent.TagentAgent) *task.Task {
	for _, tk := range b.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	return nil
}

func walReentryBoot(t *testing.T, yamlPath string, m *walReentryModel) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// walReentryChild runs one boot of the WAL-reentry scenario as its own process.
//
// The spawn phase gives the record time to reach the durable path, then exits WITHOUT
// Close. The task must be left un-settled — that is the whole state under test, and it
// is the physical shape of a crash rather than a hand-written record.
func walReentryChild(t *testing.T, phase string) {
	yamlPath := os.Getenv(walReentryYamlEnv)
	require.NotEmpty(t, yamlPath, "the parent must hand the shared config path through the env")
	dir := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(walReentryYAML(dir, false)), 0o644))
		gate := make(chan struct{})
		m := &walReentryModel{prefer: []string{"b", "c"}, gate: gate}
		entry := walReentryBoot(t, yamlPath, m)
		out, err := entry.StartLoop("u", "walReentry-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
		require.NoError(t, err)
		waitFor(t, "the nested delegation entered and is still running", func() bool {
			return countServed(m.snapshot(), "SUB-C") >= 1
		})
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b, "b is a resident owner")
		require.NotNil(t, firstSubagentTask(b),
			"precondition: the unfinished subagent task sits on b's OWN board")
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "restart":
		m := &walReentryModel{prefer: []string{"b", "c"}}
		entry := walReentryBoot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)

		tk := firstSubagentTask(b)
		require.NotNilf(t, tk,
			"§4.2 WAL 重建入口：重启后 b 自己的 board 必须从事实链折回那个未终结的子 agent 任务（实得 board=%v）", len(b.TaskManager().List()))
		before := countServed(m.snapshot(), "SUB-C")

		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		require.NoError(t, err)
		require.NotContains(t, res.(string), "失败",
			"重建出的合法任务在重启入口必须能重放：%v", res)
		waitFor(t, "the relaunched depth-2 delegation really ran on the rebuilt face", func() bool {
			return countServed(m.snapshot(), "SUB-C") > before
		})

	case "restart_drop_c":
		m := &walReentryModel{prefer: []string{"b", "c"}}
		entry := walReentryBoot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)
		tk := firstSubagentTask(b)
		require.NotNil(t, tk, "precondition: the rebuilt board still carries the durable task")
		require.Nil(t, b.ContextManager().SubagentWrapper("c"),
			"precondition: the restarted face no longer routes c")

		before := countServed(m.snapshot(), "SUB-C")
		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		if err == nil {
			require.Contains(t, res.(string), "失败",
				"§4.2：重启后目标已被摘路由，存量任务必须**按名拒绝**而不是静默复活旧代绑定：%v", res)
		}
		require.Zero(t, countServed(m.snapshot(), "SUB-C")-before,
			"a refused re-entry must not execute the removed target")

	default:
		t.Fatalf("unknown phase %q", phase)
	}
	os.Exit(0)
}

const (
	sessionWatchPhaseEnv = "TAGENT_MON33_PHASE"
	sessionWatchYamlEnv  = "TAGENT_MON33_YAML"
	sessionWatchFilter   = "TestLiveSessionStaysWatchedAcrossToolGeneration$"
	sessionWatchCommand  = "sleep 150"
	sessionWatchSvcName  = "sessionWatchsvc"
)

// sessionWatchYAML renders an entry that owns the REAL exec tool (the ActionTool), on a
// localfile store so the task record outlives the process. No model/providers
// section: the host-injected mock then serves the agent (resolveAgentModel order 2).
func sessionWatchYAML(root string) string { return sessionWatchYAMLWithExtra(root, "") }

// sessionWatchYAMLAfterPublish adds a routed sub-agent: a declaration the current face
// does not have, so CheckOrgReload really publishes a NEW generation (and with it
// a freshly assembled ActionTool for the owner).
func sessionWatchYAMLAfterPublish(root string) string {
	return sessionWatchYAMLWithExtra(root, "      - kind: agent\n        agent: helper\n        description: delegate-helper\n"+"  helper:\n    system_prompt:\n      inline: \"SUB-HELPER\"\n    memory:\n      type: memory\n")
}

func sessionWatchYAMLWithExtra(root, extra string) string {
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: localfile\n      path: " +
		fmt.Sprintf("%q", filepath.Join(root, "store-a")) +
		"\n    tools:\n      - kind: tool\n        id: exec\n" + extra
}

// sessionWatchModel asks for the action tool once, with a command that keeps its tmux
// session alive well past the restart, then closes the turn on the ack. The
// registry id is "exec" while the tool's own DECLARATION name is "action"
// (action_tool.go:287) — the model only ever sees the declaration name, so
// targeting the id from YAML would silently offer nothing.
type sessionWatchModel struct {
	mu    sync.Mutex
	calls int
	// offered records which call numbers actually issued a tool call, so the
	// post-publish assertion cannot be satisfied by a turn that never delegated.
	offered  []int
	requests []*model.Request
}

// count reads the call total under the same mutex the model writes under — the
// polling predicates below must never touch the field directly (a plain read
// against the model's locked write is a data race, caught by -race here).
func (m *sessionWatchModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *sessionWatchModel) offeredCalls() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.offered...)
}

func (m *sessionWatchModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *sessionWatchModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.calls++
	n := m.calls
	offer := ""
	if n == 1 || n == 3 {
		for _, t := range tools {
			if t == "action" {
				offer = t
			}
		}
	}
	if offer != "" {
		m.offered = append(m.offered, n)
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		args, err := json.Marshal(map[string]any{
			"command": sessionWatchCommand, "ttl": 600, "mode": "resident", "name": sessionWatchSvcName,
		})
		if err != nil {
			return nil, err
		}
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-action", Function: model.FunctionDefinitionParam{
				Name: offer, Arguments: args}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("served:" + label)}}}
	}
	close(ch)
	return ch, nil
}

func (m *sessionWatchModel) Info() model.Info { return model.Info{Name: "sessionWatch-model"} }

// commandTask finds the exec task and its backing tmux session name.
func commandTask(ta *agent.TagentAgent) (*task.Task, string) {
	for _, tk := range ta.TaskManager().List() {
		if tk.Spec.Kind == "command" && tk.Spec.Declarative != nil && tk.Spec.Declarative.TaskID != "" {
			return tk, tk.Spec.Declarative.TaskID
		}
	}
	return nil, ""
}

// tmuxSessionAlive asks the tmux server itself — the ground truth the monitor's
// answer has to agree with, so a promoted task cannot be an accident of a dead session.
func tmuxSessionAlive(session string) bool {
	if session == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// killOwnSession releases ONLY a session this test created (name guard), leaving
// anything else on the operator's tmux server untouched.
func killOwnSession(t *testing.T, session string) {
	t.Helper()
	if session != "n-"+sessionWatchSvcName {
		t.Logf("refusing to kill a session this test did not create: %q", session)
		return
	}
	if out, err := exec.Command("tmux", "kill-session", "-t", session).CombinedOutput(); err != nil {
		t.Logf("kill-session %s: %v %s", session, err, out)
	}
}

func sessionWatchBoot(t *testing.T, yamlPath string) (*agent.TagentAgent, *sessionWatchModel) {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &sessionWatchModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry, m
}

// sessionWatchChild runs one boot of the live-session anchor as its own process.
//
// - Spawn phase owns one fixed session name and kills any session of that name first: a named session survives a crashed earlier run while the tool refuses a duplicate name, so the anchor stays re-runnable.
// - It lets the record reach the durable path and leaves without Close, so nothing reaps the session and the task never settles.
// - Publish phase writes a new generation that re-assembles the owner ActionTool; the current generation must still own the live session, and the same logical name is refused as a duplicate.
func sessionWatchChild(t *testing.T, phase string) {
	yamlPath := os.Getenv(sessionWatchYamlEnv)
	require.NotEmpty(t, yamlPath)
	root := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
		killOwnSession(t, "n-"+sessionWatchSvcName)
		entry, _ := sessionWatchBoot(t, yamlPath)
		out, err := entry.StartLoop("u", "sessionWatch-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start a long job"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the exec task exists with a backing session", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: the tmux server must really be running session %s", session)
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "hotreload":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
		killOwnSession(t, "n-"+sessionWatchSvcName)
		entry, m := sessionWatchBoot(t, yamlPath)
		tick := time.Now().Add(2 * time.Second)
		out, err := entry.StartLoop("u", "sessionWatch-hot")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start the resident service"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the resident session is up under the first generation", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session), "precondition: %s must be live", session)

		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAMLAfterPublish(root)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
		before := m.count()
		entry.CheckOrgReload()
		require.Greater(t, diagInt64(t, entry.OrgDiagnostics(), "generation"), int64(0),
			"precondition: the publish really advanced the generation")

		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start it again"))
		require.NoError(t, err)
		waitFor(t, "the second attempt was answered", func() bool { return m.count() > before+1 })

		require.Contains(t, m.offeredCalls(), 3,
			"precondition: the current generation must really have been asked to spawn again")

		tasks := 0
		for _, tk := range entry.TaskManager().List() {
			if tk.Spec.Kind == "command" {
				tasks++
			}
		}
		require.Equalf(t, 1, tasks,
			"§3.3「已纳管任务不因工具换代失监视」：换代后的当前代仍须认得那个存活会话（同名第二次派生被当成新会话＝monitor 空、监视已失），实得 command 任务数=%d", tasks)
		require.Truef(t, tmuxSessionAlive(session), "the original session %s must be the one still alive", session)

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	case "restart":
		entry, _ := sessionWatchBoot(t, yamlPath)
		tk, session := commandTask(entry)
		require.NotNilf(t, tk,
			"§3.3 端到端：重启必须从事实链折回那个 exec 任务（board=%v）", len(entry.TaskManager().List()))
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: session %s must still be alive for the monitoring claim to mean anything", session)

		deadline := time.Now().Add(30 * time.Second)
		for tk.Status() != task.TaskRunning && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			tk, _ = commandTask(entry)
		}
		require.Equalf(t, task.TaskRunning, tk.Status(),
			"§3.3「已纳管任务不因工具换代失监视」：存活会话 %s 在换代后的新代里必须仍被监视并提升回 running（实得 status=%s）",
			session, string(tk.Status()))

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	default:
		t.Fatalf("unknown phase %q", phase)
	}
}

// armGate parks every FUTURE call of the agent whose prompt starts with `label`.
// Safe for concurrent use because GenerateContent reads the map under m.mu.
func (m *delegModel) armGate(label string, ch chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gates == nil {
		m.gates = map[string]chan struct{}{}
	}
	m.gates[label] = ch
}

// disarmGate frees a parked call and is idempotent, so a bail-out can never hang the
// package on a gate nobody will close.
func disarmGate(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// chainYAML is A→B→C with C's declaration text parameterized: changing C's prompt is a
// structural change, so applying it republishes C into a new generation.
func chainYAML(cPrompt string) string {
	return strings.Replace(nestedYAML(), `inline: "SUB-C-PROMPT"`, fmt.Sprintf("inline: %q", cPrompt), 1)
}

// allLevelsChangedYAML is the same chain with a new declaration at EVERY level.
func allLevelsChangedYAML() string {
	return strings.NewReplacer(
		`inline: "ENTRY-A-PROMPT"`, `inline: "ENTRY-A-PROMPT-G2"`,
		`inline: "SUB-B-PROMPT"`, `inline: "SUB-B-PROMPT-G2"`,
		`inline: "SUB-C-PROMPT"`, `inline: "SUB-C-PROMPT-G2"`,
	).Replace(nestedYAML())
}

// reentryYAML renders entry "a" delegating to `target` with relaunch_task and
// resume_task on the same face. `maxIters` is a fingerprint field, so changing it
// is what makes an edit publish a NEW generation while the routing shape holds.
func reentryYAML(target string, maxIters int) string {
	return fmt.Sprintf(`entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: %d
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-%s
      - kind: tool
        id: relaunch_task
      - kind: tool
        id: resume_task
  b:
    system_prompt:
      inline: "SUB-B-PROMPT"
    memory:
      type: memory
  c:
    system_prompt:
      inline: "SUB-C-PROMPT"
    memory:
      type: memory
`, maxIters, target, target)
}

func writeReentryYAML(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
}

// namedDelegModel is delegModel with the choice made BY NAME: the framework does
// not promise any ordering of the tools it hands the model, so a mock that calls
// "tools[0]" would be asserting an accident of slice order rather than the
// orchestration. This mock plays the honest LLM role: it delegates to the tool it
// was told to prefer, when that tool is really on the face it was offered.
type namedDelegModel struct {
	delegModel
	pick string
}

func (m *namedDelegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, t := range tools {
			if t == m.pick {
				offer = t
			}
		}
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "served:" + label}}}}
	close(ch)
	return ch, nil
}

// subagentTask finds the task the delegation left behind (kind/key are the
// production spawn's own identity, not a test fixture).
func subagentTask(t *testing.T, tm *task.TaskManager) *task.Task {
	t.Helper()
	for _, tk := range tm.List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	t.Fatalf("no subagent task on the board: %+v", tm.List())
	return nil
}

func relaunchArgs(t *testing.T, id string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"task_id": id})
	require.NoError(t, err)
	return b
}
