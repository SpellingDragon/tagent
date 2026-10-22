// The settle-turn checks of the host: a terminal task notification the
// delivery path commits drives one follow-up turn at the user address that owns
// the task, and no other fact ever does.
package host

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestSettledNoticeDrivesFollowUpTurn pins the inputless follow-up turn a delivered terminal settle fires at the user address.
//
// - the entry model delegates, and the user turn ends before the worker settles
// - the follow-up turn is stamped task and reads the settled notice as material
// - the notice material carries the worker result body the delegated turn produced
// - the sender receives the follow-up answer without a second user invocation
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestSettledNoticeDrivesFollowUpTurn(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	const (
		workerBody     = "the delegated worker reports the total is 42"
		followUpAnswer = "the settled task reached the user without a second request"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	var entryMu sync.Mutex
	entryCalls := 0
	provider.answer = func(index int) string {
		models := provider.servedModels()
		switch models[index] {
		case "model-worker":
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody)
		default:
			entryMu.Lock()
			entryCalls++
			call := entryCalls
			entryMu.Unlock()
			if call == 1 {
				return settleToolCallAnswer("worker", "call-worker", `{"input":"compute the total"}`)
			}
			return scriptedAnswerOf(followUpAnswer)
		}
	}

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry",
		"  user:\n"+
			"    model: model-entry\n"+
			"    max_tokens: 32000\n"+
			"    tools:\n"+
			"      - { kind: agent, agent: worker }\n"+
			"  worker:\n"+
			"    model: model-worker\n"))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-settle-turn"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, tagent.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(20 * time.Second):
		invokeCancel()
		t.Fatal("the delegated turn never reached its provider")
	}

	invokeCancel()
	select {
	case <-invoked:
	case <-time.After(20 * time.Second):
		t.Fatal("the cancelled user turn never returned")
	}
	close(releaseWorker)

	waitForSettle(t, "the follow-up turn to deliver its answer", func() bool {
		return len(sender.bodies()) >= 1
	})

	models := provider.servedModels()
	if len(models) != 3 {
		t.Fatalf("model requests = %v, want the delegated decision, the worker turn and the one follow-up turn", models)
	}
	entry, worker := 0, 0
	for _, m := range models {
		switch m {
		case "model-entry":
			entry++
		case "model-worker":
			worker++
		}
	}
	if entry != 2 || worker != 1 {
		t.Fatalf("model requests = %v, want two entry turns and one worker turn in any order", models)
	}
	bodies := sender.bodies()
	if len(bodies) != 1 || bodies[0] != followUpAnswer {
		t.Fatalf("delivered bodies = %q, want the one follow-up answer %q", bodies, followUpAnswer)
	}
	deliveries := sender.deliveries()
	if deliveries[0].target != addr {
		t.Fatalf("the follow-up answer was delivered to %+v, want the user address %+v", deliveries[0].target, addr)
	}

	followUp := settleRequestTexts(t, provider.recorded()[2])
	noticeSeen := false
	for _, message := range followUp {
		if strings.Contains(message.content, "tagent: task task-") && strings.Contains(message.content, "settled") {
			noticeSeen = true
		}
	}
	if !noticeSeen {
		t.Fatalf("the follow-up request carries no settled task notice: %+v", followUp)
	}
	bodySeen := false
	for _, message := range followUp {
		if strings.Contains(message.content, workerBody) {
			bodySeen = true
		}
	}
	if !bodySeen {
		t.Fatalf("the follow-up request carries no worker result body %q: %+v", workerBody, followUp)
	}

	facts := host.Run().SessionFacts(addr)
	settleFact, taskNotice := false, false
	for _, fact := range facts {
		kind, taskID := fact.Meta[event.MetaKeyNoticeKind], fact.Meta[event.MetaKeyTaskID]
		mechanical := kind == string(task.NoticeSettled) && taskID == string(fact.TaskID) &&
			fact.Meta[event.MetaKeyEventSummary] == kind
		switch {
		case fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] == string(task.NoticeSettled):
			settleFact = settleFact || mechanical
		case fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task":
			taskNotice = taskNotice || mechanical
		}
	}
	if !settleFact {
		t.Fatalf("the session carries no terminal settle fact with a settle lineage and its mechanical kind and task keys: %+v", facts)
	}
	if !taskNotice {
		t.Fatalf("the follow-up range carries no feedback fact stamped task with its mechanical kind and task keys: %+v", facts)
	}

	entryMu.Lock()
	entryTurns := entryCalls
	entryMu.Unlock()
	if entryTurns != 2 {
		t.Fatalf("entry model turns = %d, want the delegating decision beside the one follow-up turn", entryTurns)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestSettleDriverIgnoresNonTerminalFacts pins the closed trigger table: a fact that is not a delivered settle fires nothing.
//
// - an ordinary user fact fires no follow-up turn
// - a terminal settle with no settle lineage, the loop's own backflow shape, fires nothing
// - the provider and the sender see no second request and no second body
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestSettleDriverIgnoresNonTerminalFacts(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf("ok") }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-settle-negative"}
	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
	}
	requests := len(provider.servedModels())
	bodies := len(sender.bodies())

	if _, err := host.Run().CommitToSession(ctx, addr, event.Event{
		Type:    event.TypeExternalInput,
		Message: model.Message{Role: model.RoleUser, Content: "an ordinary follow-up question"},
	}); err != nil {
		t.Fatalf("commit the ordinary user fact: %v", err)
	}
	if _, err := host.Run().CommitToSession(ctx, addr, event.Event{
		Type:    event.TypeTaskSettled,
		TaskID:  "task-attended",
		Message: model.Message{Role: model.RoleUser, Content: "tagent: task task-attended settled"},
	}); err != nil {
		t.Fatalf("commit the attended terminal fact: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	if got := len(provider.servedModels()); got != requests {
		t.Fatalf("model requests after non-triggering facts = %d, want the %d the user turn made", got, requests)
	}
	if got := len(sender.bodies()); got != bodies {
		t.Fatalf("delivered bodies after non-triggering facts = %d, want the %d the user turn delivered", got, bodies)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// settleToolCallAnswer renders the answer of one decision that issues a tool
// call of the given name, so the scripted entry delegates to the declared
// worker exactly as the model would.
func settleToolCallAnswer(name, callID, arguments string) string {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic("host: encode the scripted tool arguments: " + err.Error())
	}
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"delegating","tool_calls":[{"id":"` +
		callID + `","type":"function","function":{"name":"` + name + `","arguments":` + string(encoded) + `}}]},"finish_reason":"tool_calls"}]}`
}

// settleRequestTexts reads the messages one recorded request carried, as the
// role beside the content each message spoke.
func settleRequestTexts(t *testing.T, body map[string]json.RawMessage) []struct{ role, content string } {
	t.Helper()
	raw, ok := body["messages"]
	if !ok {
		t.Fatalf("the recorded request carries no messages: %+v", body)
	}
	var decoded []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode the request messages: %v", err)
	}
	out := make([]struct{ role, content string }, 0, len(decoded))
	for _, message := range decoded {
		out = append(out, struct{ role, content string }{role: message.Role, content: message.Content})
	}
	return out
}

// waitForSettle waits for one settle-driven effect, bounded by the check's own
// deadline.
func waitForSettle(t *testing.T, what string, reached func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !reached() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
