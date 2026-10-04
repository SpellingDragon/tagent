// org_candidate_support 是候选事务族的共享 fixture 与门控桩：零 Test 声明，不参与职责同位计数。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
package tagent

import (
	"github.com/SpellingDragon/tagent/agent/resources"
	"github.com/SpellingDragon/tagent/config"

	"github.com/SpellingDragon/tagent/agent/org"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// populatedAgentConfig sets every AgentConfig field to a distinct non-zero
// value so the fingerprint subset can be audited field by field.
func populatedAgentConfig() AgentConfig {
	enable := true
	effort := "high"
	tokens := 4096
	return AgentConfig{
		Model: "m1", Provider: "p1", PromptDir: "pd",
		SystemPrompt:         PromptConfig{Inline: "sp"},
		Memory:               MemoryConfig{Type: "memory"},
		Tools:                []ToolRef{{Kind: ToolKindAgent, AgentID: "sub", Description: "d", EventParams: []string{"event_key"}, ExtraParams: []ExtraParam{{Name: "action", Type: "string"}}, Async: &enable}},
		MaxToolIterations:    7,
		MaxTokens:            8000,
		Temperature:          0.5,
		CompressThreshold:    0.7,
		KeepRecentTasks:      3,
		TaskTerminalTTL:      "2m",
		TaskDefaultTTL:       "10m",
		ResumeContextRounds:  2,
		Compress:             CompressConfig{CompactKeysListed: 1, RecentFullCount: 2, CardMaxChars: 300},
		ThinkingEnabled:      &enable,
		ThinkingTokens:       &tokens,
		ReasoningEffort:      &effort,
		ReasoningContentMode: "raw",
		Meditation:           MeditationConfig{Enabled: true},
		WorkspaceRoot:        "/ws",
		Description:          "desc",
	}
}

// fingerprintExcludedFields are the AgentConfig fields deliberately outside the
// org fingerprint, keyed by their **YAML/JSON name** (what the subset carries).
// Each entry states WHY; a new field is only acceptable here if it is
// hot-applicable through its own contract or needs a restart.
var fingerprintExcludedFields = map[string]string{
	"memory":             "storage paths/backends cannot migrate at runtime — restart (pinned by the reloader's config.ChangedMemoryAgents/residentMemFP pre-check)",
	"max_tokens":         "hot-applicable via ApplyOrgHotParams (compressor budget)",
	"compress_threshold": "hot-applicable via ApplyOrgHotParams (compressor threshold)",
	"keep_recent_tasks":  "hot-applicable via ApplyOrgHotParams",
	"task_terminal_ttl":  "hot-applicable via the TaskManager reaper setter",
	"task_default_ttl":   "hot-applicable via the TaskManager reaper setter",
}

func jsonFieldName(f reflect.StructField) string {
	tag := strings.Split(f.Tag.Get("json"), ",")[0]
	if tag == "" {
		return f.Name
	}
	return tag
}

func mustFP(t *testing.T, c *Config) string {
	t.Helper()
	fp, err := org.ComputeOrgFingerprint(c)
	require.NoError(t, err)
	return fp
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }

const candTxnStartupYAML = `entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: keep
        description: "keep"
  keep:
    system_prompt:
      inline: "keep"
`

// candTxnRefusedYAML 让 main 同时委派 keep 与 aaa_parent，aaa_parent 再委派合法的 zzz_dep：
// 依赖经递归先建成并登记属主，aaa_parent 随后因缺失冥想提示词文件、在它的依赖建成之后才失败，
// 以此确定时序触发「被拒候选必须完全退场」。
const candTxnRefusedYAML = `entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: keep
        description: "keep"
      - kind: agent
        agent: aaa_parent
        description: "p"
  keep:
    system_prompt:
      inline: "keep"
  aaa_parent:
    system_prompt:
      inline: "aaa_parent"
    tools:
      - kind: agent
        agent: zzz_dep
        description: "z"
    meditation:
      enabled: true
      prompt_file: "does-not-exist-r01-probe.md"
  zzz_dep:
    system_prompt:
      inline: "zzz_dep"
`

// txnYAML renders the S-B fixture: entry main → sub1; hot-add cases inject
// extra referenced agents. failZzz points zzz_probe's memory at /dev/null so
// its store creation fails DETERMINISTICALLY mid-candidate (after aaa_probe,
// which sorts first, has been built successfully) — the refused-candidate
// cleanup path then has TWO acquired responsibilities to unwind.
func txnYAML(t testing.TB, mainModel string, addProbes, failZzz bool) string {
	t.Helper()
	probes := ""
	if addProbes {
		zzzPath := fmt.Sprintf("%q", testStore(t, "hottest-zzz"))
		if failZzz {
			zzzPath = `"/dev/null/zzz-probe-cannot-create"`
		}
		probes = fmt.Sprintf(`      - kind: agent
        agent: aaa_probe
        description: "aaa"
      - kind: agent
        agent: zzz_probe
        description: "zzz"
  aaa_probe:
    system_prompt:
      inline: "aaa"
    memory:
      type: localfile
      path: %q
  zzz_probe:
    system_prompt:
      inline: "zzz"
    memory:
      type: localfile
      path: %s
`, testStore(t, "hottest-aaa"), zzzPath)
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    model: %s
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, mainModel, probes, testStore(t, "hottest-sub1"))
}

// orgLastDiscardOrder returns the most recent candidate-discard order (TEST
// introspection only; nil before the first discard).
func orgLastDiscardOrder() []string {
	if v, ok := org.LastDiscardOrder(); ok {
		return v
	}
	return nil
}

const (
	g24Tool   = "g24_flaky_tool"
	g24Leaf   = "g24_leaf"
	g24Mid    = "g24_mid"
	g24B      = "g24_b"
	g24P1     = "g24_p1"
	g24P2     = "g24_p2"
	g24Shared = "g24_shared"
)

// stageGate is the injected REAL failure: a plain-tool factory that cannot serve
// past a call budget. Nothing in the product path knows about it.
//
// The registration is process-global and a duplicate id panics, so the gate is a
// package-level singleton armed once and RESET per run — which keeps this file
// usable under a -count>1 repetition gate instead of needing an exemption.
type stageGate struct {
	limit atomic.Int64
	calls atomic.Int64
}

var (
	g24GateOnce sync.Once
	g24Flaky    = &stageGate{}
)

func armStageGate(t *testing.T) *stageGate {
	t.Helper()
	g24GateOnce.Do(func() {
		agent.RegisterPlainTool(g24Tool, g24Flaky.tool)
	})
	g24Flaky.calls.Store(0)
	g24Flaky.limit.Store(1 << 40)
	return g24Flaky
}

func (g *stageGate) tool(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
	if g.calls.Add(1) > g.limit.Load() {
		return nil, errors.New("injected: tool factory cannot serve")
	}
	return &mockCallableTool{name: g24Tool}, nil
}

func writeGateConfig(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// leafYAML: main → mid → leaf. leaf declares the flaky tool and owns its own
// localfile store, so a leaked owner is observable twice over: in the resident
// table, and as a still-held writer lock. withLeaf=false drops leaf from mid's
// routing (structural: the routing shape is fingerprinted).
func leafYAML(withLeaf bool, leafStore string) string {
	midTail := "    tools: []\n"
	leafDef := ""
	if withLeaf {
		midTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-leaf\n", g24Leaf)
		leafDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"LEAF\"\n    memory:\n      type: localfile\n      path: %q\n    tools:\n      - kind: tool\n        id: %s\n", g24Leaf, leafStore, g24Tool)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-mid
  %s:
    system_prompt:
      inline: "MID"
    memory:
      type: memory
%s%s`, g24Mid, g24Mid, midTail, leafDef)
}

// ownerBYAML routes main→b when withB, with b's OWN hot numerics; changing those
// numerics is numeric-only (fingerprint-excluded), changing the routing is not.
func ownerBYAML(withB bool, keep int, terminal string) string {
	mainTail := "    tools: []\n"
	bDef := ""
	if withB {
		mainTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-b\n        async: false\n", g24B)
		bDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"SUB-B\"\n    memory:\n      type: memory\n    keep_recent_tasks: %d\n    task_terminal_ttl: %q\n", g24B, keep, terminal)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
%s%s`, mainTail, bDef)
}

// countMainCompletedByB counts MAIN records whose ToolResults already carry
// B's answer — the host-visible completion edge of a MAIN→B delegation turn.
func countMainCompletedByB(snaps []delegServed) int {
	n := 0
	for _, s := range snaps {
		if s.System != "MAIN" {
			continue
		}
		for _, r := range s.ToolResults {
			if strings.Contains(r, "served:SUB-B") {
				n++
				break
			}
		}
	}
	return n
}

// diamondYAML routes main→{p1,p2} (or only p2), both to the SAME shared child
// which owns its own localfile store.
func diamondYAML(withP1 bool, sharedStore string) string {
	mainTools := fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p2\n", g24P2)
	if withP1 {
		mainTools = fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p1\n%s", g24P1, mainTools)
	}
	routes := func(parent string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"P-%s\"\n    memory:\n      type: memory\n    tools:\n      - kind: agent\n        agent: %s\n        description: shared-child\n", parent, parent, g24Shared)
	}
	p1 := ""
	if withP1 {
		p1 = routes(g24P1)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
%s%s%s  %s:
    system_prompt:
      inline: "SHARED"
    memory:
      type: localfile
      path: %q
`, mainTools, p1, routes(g24P2), g24Shared, sharedStore)
}

// l3YAML renders a single-entry org named main whose system_prompt participates in the
// org fingerprint, while keep/max/threshold/terminal are hot-applicable numerics outside it.
func l3YAML(prompt string, keep, max int, threshold float64, terminal string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: " + strconv.Quote(prompt) + "\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keep) + "\n" +
		"    max_tokens: " + strconv.Itoa(max) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(threshold, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(terminal) + "\n" +
		"    memory:\n      type: memory\n"
}

func diagInt64(t *testing.T, d map[string]any, k string) int64 {
	t.Helper()
	v, ok := d[k]
	require.Truef(t, ok, "diagnostics missing %q", k)
	n, ok := v.(int64)
	require.Truef(t, ok, "diagnostics %q is %T, want int64", k, v)
	return n
}

func diagTime(t *testing.T, d map[string]any, k string) (time.Time, bool) {
	t.Helper()
	v, ok := d[k]
	if !ok {
		return time.Time{}, false
	}
	tt, ok := v.(time.Time)
	require.Truef(t, ok, "diagnostics %q is %T, want time.Time", k, v)
	return tt, true
}

// sdCloseYAML routes `routed` from main, giving every routed agent its OWN
// localfile store so the final lock state is observable from outside the org.
func sdCloseYAML(t testing.TB, routed []string, storeOf func(name string) string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	defs := ""
	for _, r := range routed {
		defs += fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: localfile\n      path: %q\n", r, r, storeOf(r))
	}
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" + tools + defs
}

// assertStoreWriterFree proves that a store lease really was handed back: the
// single-writer lock on that path must be acquirable by this test alone. A lock that
// is still held means either a leak or a live writer, so the exit is checked here.
func assertStoreWriterFree(t *testing.T, path string) {
	t.Helper()
	probe, err := os.OpenFile(filepath.Join(resources.Canonicalize(path), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer probe.Close()
	require.NoError(t, resources.FlockExclusive(probe),
		"关闭后 %s 的写锁必须已归还（恰一次释放，不待下一个用户请求）", path)
}

// ownerYAML renders entry "main" delegating to `targets`（sub1/sub2 始终被定义，
// 所以移除只改变可达性，不改变配置里存在什么）。sub2 的 memory 段作为参数，便于
// 渲染“同名重入且存储不变”与“重入但存储变了”两种候选。
func ownerYAML(t testing.TB, targets []string, sub2Mem string) string {
	t.Helper()
	var toolLines string
	for _, t := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", t, t)
	}
	head := "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
	defs := fmt.Sprintf(`  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
      path: %q
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
`, testStore(t, "own-sub1"))
	return head + toolLines + defs + sub2Mem + "\n"
}

// sub2MemDefault renders sub2 with its own store path: each agent needs a distinct store
// to have observable identity. testStore moves the path out of the working tree and
// isolates it per case, because acquire makes the directory and takes its flock before
// dispatching on the store kind — a memory store still touches the filesystem.
func sub2MemDefault(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2"))
}

func sub2MemMoved(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2-moved"))
}

// sub2MemInMemory carries no path, so it needs no per-case store root.
const sub2MemInMemory = "      type: memory\n"

// ownerWriter bumps mtime deterministically: FS granularity can otherwise
// swallow a rapid rewrite and silently skip the reload.
func ownerWriter(t *testing.T, yamlPath string) func(string) {
	t.Helper()
	tick := time.Now()
	return func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
}

func buildOwnerAgent(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.Close() })
	return ta
}

func entryToolNames(ta *agent.TagentAgent) []string {
	face := ta.ContextManager().ExecutorConfig()
	var out []string
	for _, tl := range face.Tools {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// hotAddDataYAML renders entry "main" routing `targets`, with sub1/sub2 always
// DEFINED (so publishing sub2 is a hot ADD of a routed owner, not a new
// definition) and sub2 on its own localfile store. Deliberately NO model/providers
// section: resolveAgentModel's documented order 2 hands an agent that declares no
// model to the host-injected instance — that is what lets one mock serve main and
// sub2 and makes the delegation observable at the model boundary.
func hotAddDataYAML(t *testing.T, targets []string) string {
	t.Helper()
	var toolLines string
	for _, name := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n        async: false\n", name, name)
	}
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n      path: " + fmt.Sprintf("%q\n", testStore(t, "hotadd-data-main")) +
		"    tools:\n" + toolLines +
		fmt.Sprintf(`  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
      path: %q
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
`, testStore(t, "hotadd-data-sub1"), testStore(t, "hotadd-data-sub2"))
}

// hotAddDataModel drives a REAL delegation: the entry's first call asks for the
// hot-added sub-agent with a payload only that sub-agent can echo, and the
// sub-agent answers with a distinctive marker. Every request is captured so the
// host's view of the returned value is read off the wire, not off an internal
// field.
type hotAddDataModel struct {
	mu       sync.Mutex
	requests []*model.Request
}

func (m *hotAddDataModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *hotAddDataModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	m.mu.Lock()
	m.requests = append(m.requests, req)
	mainCalls := 0
	for _, r := range m.requests {
		if len(r.Messages) > 0 && strings.HasPrefix(r.Messages[0].Content, "main") {
			mainCalls++
		}
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	switch {
	case strings.HasPrefix(system, "sub2"):
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage(hotAddAns)}}}
	case strings.HasPrefix(system, "main") && mainCalls == 1:
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-sub2", Function: model.FunctionDefinitionParam{
				Name: "sub2", Arguments: []byte(`{"request":"` + hotAddReq + `"}`)}}},
		}}}}
	default:
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage(hotAddMark)}}}
	}
	close(ch)
	return ch, nil
}

func (m *hotAddDataModel) Info() model.Info { return model.Info{Name: "hotadd-data-model"} }

// storeFacts lists the events in `store` whose stored content matches `keyword`,
// as "type|content" read back off the record itself — attribution is judged on
// what the store actually holds, not on a summary the framework handed to the
// caller. Limit is explicit: QueryEvents returns nothing without one.
// The partition axis is the ownership axis: context_manager derives it from the
// agent NAME (`PartitionIDFromName(cfg.Name)`), and an unpartitioned query
// deliberately scans NOTHING (the isolation contract both store implementations
// share) — so reading back attribution must name the partition it belongs to.
func storeFacts(t *testing.T, store memory.MemoryStore, partitionName, keyword string) []string {
	t.Helper()
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName(partitionName)},
		Keyword:      keyword,
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	var out []string
	for _, ref := range refs {
		full, err := store.GetEvent(ref.EventKey)
		if err != nil {
			continue
		}
		out = append(out, ref.EventType+"|"+full.Content)
	}
	return out
}

func countFacts(facts []string, eventType, substr string) int {
	n := 0
	for _, f := range facts {
		parts := strings.SplitN(f, "|", 2)
		if parts[0] == eventType && strings.Contains(parts[1], substr) {
			n++
		}
	}
	return n
}

// dirContains reports whether any file under root carries the marker bytes.
func dirContains(t *testing.T, root, marker string) bool {
	t.Helper()
	var found bool
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(b), marker) {
			found = true
		}
		return nil
	})
	return found
}

// factoryTrunkYAML routes `leaf` from main with async:false (a sync delegation:
// the witness is the call itself, not a settle-driven extra turn) and makes the
// leaf's OWN max_tool_iterations the varied field — it is fingerprinted, so
// changing it publishes a new generation, and it also reaches the factory via
// ToolAgentFactoryConfig, which is how the declaration changes.
func factoryTrunkYAML(leaf string, iters int) string {
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-leaf
        async: false
  %s:
    system_prompt:
      inline: "DECLARED-IGNORED"
    memory:
      type: memory
    max_tool_iterations: %d
`, leaf, leaf, iters)
}

func writeFactoryTrunk(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// factoryLeafYAML routes `leafName` from main and gives it its OWN localfile store, so the
// writer lock on that path is an outside-observable witness of whether the lease came back.
func factoryLeafYAML(leafName, storePath string) string {
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-leaf
        async: false
  %s:
    system_prompt:
      inline: "LEAF"
    memory:
      type: localfile
      path: %q
`, leafName, leafName, storePath)
}

// noopStoreForTakeover embeds a nil MemoryStore: Acquire only needs a non-nil store
// value, which this zero-cost stub provides.
type noopStoreForTakeover struct{ memory.MemoryStore }

// takeOverStore asks a FRESH registry to open the path: success proves the
// previous holder handed the writer slot back exactly once.
func takeOverStore(t *testing.T, path string) error {
	t.Helper()
	fresh := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: path})
	_, _, rel, err := fresh.Acquire("localfile", path, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: noopStoreForTakeover{}}, nil
	})
	if err == nil {
		require.NoError(t, rel())
	}
	return err
}

// seedUnackedEnvelope writes one claimed+prepared but un-acked durable envelope
// into the inbox rooted at spillDir (spillDir/inbox-v2), whose prepared fact key
// is factKey and reserved receipt key is receiptKey. A later
// NewReliableEventBus(spillDir)+ArmRetentionFromInbox enumerates it and protects
// exactly those keys, the same way a restart-recovery owner does.
func seedUnackedEnvelope(t *testing.T, spillDir string, factKey, receiptKey int64) {
	t.Helper()
	in, err := reliability.NewInbox(spillDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "leak-probe", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e-leak","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + strconv.FormatInt(factKey, 10) + `}`)}))
	require.NoError(t, in.Close())
}
