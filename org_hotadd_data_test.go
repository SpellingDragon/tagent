package tagent

// §3.4「热增后真实数据归属——各给宿主返回与资源尾部证据」。
//
// 此前的热增证点全部停在**身份**上（`require.NotSame` 的两台 store），从未证明
// 「热增 agent 的真实读写确实落在它自己的存储里」——身份不同不等于数据落对地方：
// 一次把子 agent 回合写进宿主 store 的实现，在身份断言下照样全绿。本测把这条
// 数据流走到底：真实委派 → 宿主拿到真实返回 → 子回合记录只出现在**子自己的**
// store（宿主 store 里没有它）→ 真落盘字节可寻 → Close 后该数据的主人释放其
// store 注册（资源尾部）。
//
// 判据刻意不用「宿主 store 完全找不到该文本」：委派返回本就以 action_command
// 记录进宿主回合，那是正确行为而非泄漏。区分归属的是**记录种类**——子 agent
// 自己回合的 agent_output 只能落在子自己的分区里。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	hotAddReq  = "HOTADD-REQ-98"
	hotAddAns  = "HOTADD-ANS-98"
	hotAddMark = "main-final-98"
)

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

// TestHotAdd34_DataLandsInItsOwnStoreWithHostReturn drives the whole attribution
// chain for an owner that joined through the hot path.
func TestHotAdd34_DataLandsInItsOwnStoreWithHostReturn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	// Cold: sub2 is DEFINED but unrouted, so it has no owner yet.
	write(hotAddDataYAML(t, []string{"sub1"}))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &hotAddDataModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }() // idempotent; the explicit Close below is the one under test

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

	// Hot-add: route sub2 — its owner is built on the hot path with its own store.
	write(hotAddDataYAML(t, []string{"sub1", "sub2"}))
	entry.CheckOrgReload()
	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2, "precondition: sub2 became a resident owner through the hot path")
	require.NotSame(t, entry.MemStore(), sub2.MemStore(), "and it holds its own store")

	out, err := entry.StartLoop("u", "hotadd-data-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start the hot-added work"))
	require.NoError(t, err)

	// (1) 宿主返回：the host turn really received the sub-agent's payload.
	waitFor(t, "the hot-added owner served the delegation and the host got its answer", func() bool {
		for _, req := range m.snapshot() {
			for _, got := range toolResultsOf(req) {
				if strings.Contains(got, hotAddAns) {
					return true
				}
			}
		}
		return false
	})

	// (2) 真实落位：the sub-agent's OWN turn records live in the sub-agent's store.
	// Event write-back is asynchronous, so the read is bounded-wait, not immediate.
	var sub2Facts []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sub2Facts = storeFacts(t, sub2.MemStore(), "sub2", hotAddAns)
		if countFacts(sub2Facts, "agent_output", hotAddAns) >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	entryFacts := storeFacts(t, entry.MemStore(), "main", hotAddAns)
	require.Empty(t, storeFacts(t, sub2.MemStore(), "main", hotAddAns),
		"非空洞性自检：同一台 store 换一个主人的分区就什么都看不见——上面的命中确由归属轴给出，不是「查什么都有」")
	require.GreaterOrEqual(t, countFacts(sub2Facts, "agent_output", hotAddAns), 1,
		"§3.4：热增 owner 自己回合的产出必须落在它自己的 store 里（实测其记录集：%v）", sub2Facts)

	// (3) 不串宿主：the host's store must not carry the sub-agent's own turn record.
	// It legitimately carries the returned value as an action_command — that is the
	// delegation result, not leakage; what must NOT appear there is a second owner's
	// agent_output.
	require.Zero(t, countFacts(entryFacts, "agent_output", hotAddAns),
		"the hot-added owner's turn record must not be written into the host's store")

	// (3b) 跨 owner 不可见：the host's store must not even hold a record under the
	// sub-agent's partition — data does not migrate between owners' namespaces.
	require.Empty(t, storeFacts(t, entry.MemStore(), "sub2", hotAddAns),
		"the host store must carry no record under the hot-added owner's partition")

	// (4) 真落盘：localfile means bytes on disk, so the attribution is physical.
	storeDir := testStore(t, "hotadd-data-sub2")
	waitFor(t, "the owner's data really persisted to its own store directory", func() bool {
		return dirContains(t, storeDir, hotAddAns)
	})

	// (5) 资源尾部：the owner that held this data releases its store registration on
	// the organization's Close.
	require.Contains(t, entry.StoreOwnerSnapshot(), "sub2", "precondition: the hot-added owner registered its store")
	require.NoError(t, entry.Close())
	require.NotContains(t, entry.StoreOwnerSnapshot(), "sub2",
		"the owner holding this data must release its store registration when the org closes")
	require.True(t, sub2.CloseStarted(), "and its own close sequence really ran")
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
