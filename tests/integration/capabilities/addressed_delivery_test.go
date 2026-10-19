package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replica"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/remote"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestAddressedDelivery checks the explicit target, kept attribution and named refusal of a delivery.
// - an internal send enters the mailbox of its explicit target only
// - a delivery naming no target is refused beside no mailbox entry
// - a repeated delivery records one entry beside the attribution it kept
// - a notification started before an address change stays at its old site
// - an unauthorized, unknown or stopped target is refused by name beside its ledger entry
// - a remote call keeps its explicit target and attribution across its retries
// - the formal chain hands the model the preview of an oversized remote result
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestAddressedDelivery(t *testing.T) {
	const namedAgent event.AgentID = "agent-recv"

	t.Run("an internal send enters the mailbox of its explicit target only", func(t *testing.T) {
		ctx := context.Background()
		chain := newDeliveryChain(t, namedAgent)
		named := event.Address{AgentID: namedAgent, SessionID: "session-named"}
		beside := event.Address{AgentID: namedAgent, SessionID: "session-beside"}
		const namedInstance event.InstanceID = "instance-named"
		const besideInstance event.InstanceID = "instance-beside"
		chain.open(t, named, namedInstance)
		chain.open(t, beside, besideInstance)

		taskID := chain.register(t, "explicit", named, namedInstance)
		sent := chain.issue(t, taskID, "evt-explicit-1")
		if err := chain.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the notification: %v", err)
		}

		got := chain.mailbox(t, named, namedInstance)
		if len(got.Mailbox) != 1 {
			t.Fatalf("mailbox of the named target = %+v, want the one delivered notification", got.Mailbox)
		}
		entry := got.Mailbox[0]
		if entry.Notice != sent.Notice || entry.EventID != sent.Notice.Event {
			t.Fatalf("entry = %+v, want the identity and the fact of %+v", entry, sent.Notice)
		}
		if entry.InvocationID != "inv-explicit" {
			t.Fatalf("entry attribution = %q, want the invocation the producing call belongs to", entry.InvocationID)
		}
		if other := chain.mailbox(t, beside, besideInstance); len(other.Mailbox) != 0 {
			t.Fatalf("mailbox of the beside target = %+v, want no delivery at a site the send never named", other.Mailbox)
		}
	})

	t.Run("a delivery naming no target is refused beside no mailbox entry", func(t *testing.T) {
		ctx := context.Background()
		chain := newDeliveryChain(t, namedAgent)
		named := event.Address{AgentID: namedAgent, SessionID: "session-named"}
		const namedInstance event.InstanceID = "instance-named"
		chain.open(t, named, namedInstance)

		taskID := chain.register(t, "untargeted", named, namedInstance)
		unsent := chain.issue(t, taskID, "evt-untargeted-1")
		for _, target := range []event.Address{
			{},
			{SessionID: "session-named"},
			{AgentID: namedAgent},
		} {
			_, err := chain.face.Send(ctx, state.Delivery{Target: target, TargetInstance: namedInstance, Notice: unsent.Notice})
			if !errors.Is(err, state.ErrSendTargetMissing) {
				t.Fatalf("send to %+v = %v, want %v for a delivery no explicit target names", target, err, state.ErrSendTargetMissing)
			}
		}

		if got := chain.mailbox(t, named, namedInstance); len(got.Mailbox) != 0 {
			t.Fatalf("mailbox = %+v, want no entry for a delivery that named no target", got.Mailbox)
		}
		if refusals := chain.face.Refusals(); len(refusals) != 0 {
			t.Fatalf("refusals = %+v, want no ledger entry naming a destination the send never named", refusals)
		}
	})

	t.Run("a repeated delivery records one entry beside the attribution it kept", func(t *testing.T) {
		ctx := context.Background()
		chain := newDeliveryChain(t, namedAgent)
		named := event.Address{AgentID: namedAgent, SessionID: "session-named"}
		const namedInstance event.InstanceID = "instance-named"
		chain.open(t, named, namedInstance)

		taskID := chain.register(t, "duplicate", named, namedInstance)
		repeated := chain.issue(t, taskID, "evt-duplicate-1")
		first := chain.send(t, task.WithCallFrame(ctx, chainFrame("inv-duplicate")), state.Delivery{
			Target:         named,
			TargetInstance: namedInstance,
			Notice:         repeated.Notice,
		})
		if first.Duplicate || first.AcceptSeq == 0 {
			t.Fatalf("first receipt = %+v, want the acceptance sequence of the one entry it took", first)
		}
		retry := chain.send(t, task.WithCallFrame(ctx, chainFrame("inv-other")), state.Delivery{
			Target:         named,
			TargetInstance: namedInstance,
			Notice:         repeated.Notice,
		})
		if !retry.Duplicate || retry.AcceptSeq != first.AcceptSeq {
			t.Fatalf("retry receipt = %+v, want the entry %d the first acceptance took beside no second fact", retry, first.AcceptSeq)
		}
		if err := chain.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the same notification again: %v", err)
		}

		got := chain.mailbox(t, named, namedInstance)
		if len(got.Mailbox) != 1 {
			t.Fatalf("mailbox = %+v, want one entry beside the one notification identity", got.Mailbox)
		}
		if got.Mailbox[0].InvocationID != "inv-duplicate" {
			t.Fatalf("entry attribution = %q, want the attribution the first acceptance recorded", got.Mailbox[0].InvocationID)
		}
		conflict := repeated.Notice
		conflict.Event = "evt-duplicate-other"
		if _, err := chain.face.Send(ctx, state.Delivery{Target: named, TargetInstance: namedInstance, Notice: conflict}); !errors.Is(err, state.ErrNoticeConflict) {
			t.Fatalf("send of one identity with other content = %v, want %v", err, state.ErrNoticeConflict)
		}
	})

	t.Run("a notification started before an address change stays at its old site", func(t *testing.T) {
		ctx := context.Background()
		chain := newDeliveryChain(t, namedAgent)
		oldAddress := event.Address{AgentID: namedAgent, SessionID: "session-old"}
		newAddress := event.Address{AgentID: namedAgent, SessionID: "session-new"}
		const oldInstance event.InstanceID = "instance-old"
		const newInstance event.InstanceID = "instance-new"
		chain.open(t, oldAddress, oldInstance)
		chain.open(t, newAddress, newInstance)

		taskID := chain.register(t, "moved", oldAddress, oldInstance)
		before := chain.issue(t, taskID, "evt-moved-1")
		if err := chain.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the started notification: %v", err)
		}
		if err := chain.manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newAddress, TargetInstance: newInstance}); err != nil {
			t.Fatalf("handoff the task: %v", err)
		}
		after := chain.issue(t, taskID, "evt-moved-2")
		if err := chain.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush after the handoff: %v", err)
		}
		if err := chain.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the retried notifications: %v", err)
		}

		old := chain.mailbox(t, oldAddress, oldInstance)
		if len(old.Mailbox) != 1 || old.Mailbox[0].Notice != before.Notice {
			t.Fatalf("old mailbox = %+v, want the started notification kept there beside no second entry", old.Mailbox)
		}
		fresh := chain.mailbox(t, newAddress, newInstance)
		if len(fresh.Mailbox) != 1 || fresh.Mailbox[0].Notice != after.Notice {
			t.Fatalf("new mailbox = %+v, want the future notification delivered there", fresh.Mailbox)
		}
		if old.Mailbox[0].AcceptSeq != 1 || old.Mailbox[0].InvocationID != "inv-moved" {
			t.Fatalf("started entry = %+v, want the acceptance sequence and attribution it froze", old.Mailbox[0])
		}
	})

	t.Run("an unauthorized, unknown or stopped target is refused by name beside its ledger entry", func(t *testing.T) {
		ctx := context.Background()
		named := event.Address{AgentID: namedAgent, SessionID: "session-named"}
		const namedInstance event.InstanceID = "instance-named"
		notice := task.Notice{TaskID: "task-refused", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "evt-refused-1"}

		closed := newDeliveryChain(t)
		closed.open(t, named, namedInstance)
		if _, err := closed.face.Send(ctx, state.Delivery{Target: named, TargetInstance: namedInstance, Notice: notice}); !errors.Is(err, state.ErrDeliveryNotAllowed) {
			t.Fatalf("send beside an empty whitelist = %v, want %v", err, state.ErrDeliveryNotAllowed)
		}
		if err := checkRefusal(closed.face.Refusals(), notice, named, state.RefusalNotAuthorized); err != nil {
			t.Fatalf("refused send beside an empty whitelist: %v", err)
		}

		open := newDeliveryChain(t, namedAgent)
		if _, err := open.face.Send(ctx, state.Delivery{Target: named, TargetInstance: namedInstance, Notice: notice}); !errors.Is(err, state.ErrUnknownTarget) {
			t.Fatalf("send to an unserved target = %v, want %v", err, state.ErrUnknownTarget)
		}
		if err := checkRefusal(open.face.Refusals(), notice, named, state.RefusalUnknownTarget); err != nil {
			t.Fatalf("refused send to an unserved target: %v", err)
		}

		open.open(t, named, namedInstance)
		open.running[namedInstance] = false
		if _, err := open.face.Send(ctx, state.Delivery{Target: named, TargetInstance: namedInstance, Notice: notice}); !errors.Is(err, state.ErrDeliveryTargetNotRunning) {
			t.Fatalf("send to a stopped target = %v, want %v", err, state.ErrDeliveryTargetNotRunning)
		}
		if err := checkRefusal(open.face.Refusals()[1:], notice, named, state.RefusalTargetNotRunning); err != nil {
			t.Fatalf("refused send to a stopped target: %v", err)
		}
		if got := open.mailbox(t, named, namedInstance); len(got.Mailbox) != 0 {
			t.Fatalf("mailbox = %+v, want no entry for a refused delivery", got.Mailbox)
		}
	})

	t.Run("a remote call keeps its explicit target and attribution across its retries", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		flaky := newARemoteAgent("remote answer")
		flaky.failFirst = 1
		flakyServer := httptest.NewServer(flaky)
		defer flakyServer.Close()
		other := newARemoteAgent("other answer")
		otherServer := httptest.NewServer(other)
		defer otherServer.Close()

		endpoints := newEndpoints(t,
			remote.Target{AgentID: "remote-a", AgentURL: flakyServer.URL},
			remote.Target{AgentID: "remote-b", AgentURL: otherServer.URL},
		)
		dispatcher := newDispatcher(t, endpoints, 2)
		target := lookup(t, endpoints, "remote-a")
		identity := remote.CallIdentity{InvocationID: "inv-remote", DecisionID: "dec-remote", CallID: "call-remote"}
		params := remoteParams("hello remote")

		result, err := dispatcher.Send(ctx, target, identity, params)
		if err != nil {
			t.Fatalf("send the addressed call: %v", err)
		}
		if got := remote.ResultText(result); got != "remote answer" {
			t.Fatalf("result text = %q, want the text the addressed agent answered with", got)
		}
		if hits := flaky.hits(); hits != 2 {
			t.Fatalf("wire attempts = %d, want one refused attempt beside the one that answered", hits)
		}
		if body := flaky.lastBody(); !strings.Contains(body, "hello remote") {
			t.Fatalf("wire request = %q, want the message handed to the explicit target", body)
		}
		record, ok := dispatcher.Call(identity)
		if !ok || !record.Settled || record.Attempts != 2 || record.Target != target || record.Identity != identity {
			t.Fatalf("call record = (%+v, %v), want two attempts under the one identity beside the explicit target", record, ok)
		}

		again, err := dispatcher.Send(ctx, target, identity, params)
		if err != nil || remote.ResultText(again) != "remote answer" {
			t.Fatalf("repeated send = (%q, %v), want the outcome the call already left", remote.ResultText(again), err)
		}
		if hits := flaky.hits(); hits != 2 {
			t.Fatalf("wire attempts after the repeat = %d, want no second call for one identity", hits)
		}
		if _, err := dispatcher.Send(ctx, lookup(t, endpoints, "remote-b"), identity, params); !errors.Is(err, remote.ErrCallTargetConflict) {
			t.Fatalf("send of one identity to another target = %v, want %v", err, remote.ErrCallTargetConflict)
		}
		if hits := other.hits(); hits != 0 {
			t.Fatalf("attempts on the other target = %d, want a refused call sent nowhere", hits)
		}
		if _, err := dispatcher.Send(ctx, remote.Target{}, identity, params); !errors.Is(err, remote.ErrTargetMissing) {
			t.Fatalf("send beside no target = %v, want %v", err, remote.ErrTargetMissing)
		}
		if _, err := dispatcher.Send(ctx, remote.Target{AgentID: "remote-x", AgentURL: flakyServer.URL}, identity, params); !errors.Is(err, remote.ErrTargetUndeclared) {
			t.Fatalf("send to an undeclared target = %v, want %v", err, remote.ErrTargetUndeclared)
		}
		if _, err := dispatcher.Send(ctx, target, remote.CallIdentity{InvocationID: "inv-remote", DecisionID: "dec-remote"}, params); !errors.Is(err, remote.ErrCallIdentityMissing) {
			t.Fatalf("send beside no attribution = %v, want %v", err, remote.ErrCallIdentityMissing)
		}
		if hits := flaky.hits(); hits != 2 {
			t.Fatalf("wire attempts after the refused sends = %d, want a refusal to reach no destination", hits)
		}
	})

	t.Run("the formal chain hands the model the preview of an oversized remote result", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		big := strings.Repeat("remote-大-", 8000)
		answer := newARemoteAgent(big)
		answerServer := httptest.NewServer(answer)
		defer answerServer.Close()

		endpoints := newEndpoints(t, remote.Target{AgentID: "remote-a", AgentURL: answerServer.URL})
		dispatcher := newDispatcher(t, endpoints, 1)
		var mu sync.Mutex
		var resolved []remote.CallIdentity
		identity := func(ctx context.Context) (remote.CallIdentity, bool) {
			frame, ok := task.CallFrameFrom(ctx)
			if !ok {
				return remote.CallIdentity{}, false
			}
			call := remote.CallIdentity{
				InvocationID: string(frame.Identity.InvocationID),
				DecisionID:   string(frame.Identity.DecisionID),
				CallID:       frame.Identity.CallID,
			}
			mu.Lock()
			resolved = append(resolved, call)
			mu.Unlock()
			return call, true
		}
		callTool, err := remote.NewTool(dispatcher, endpoints, identity)
		if err != nil {
			t.Fatalf("build the remote call tool: %v", err)
		}

		owner := event.Address{AgentID: "agent-a", SessionID: "session-addressed"}
		scope := replica.ResultScope{Owner: owner, Format: replica.FormatToolResult, RetentionPolicy: "keep"}
		originals := ledger.NewMemoryOriginalStore()
		externalizer, err := replica.NewResultExternalizer(originals, replica.WithMaxTokens(16000))
		if err != nil {
			t.Fatalf("build the externalizer: %v", err)
		}
		bounded, err := replica.NewBoundedTool(callTool, externalizer, scope)
		if err != nil {
			t.Fatalf("bound the remote call tool: %v", err)
		}

		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		round := 0
		provider := &ntModel{name: "model-a", answer: func(*model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(ntCall("call-remote", "remote_call", `{"agent":"remote-a","message":"ask the remote agent"}`))
			}
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{bounded}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-addressed-delivery",
			Address:      owner,
			Input:        []model.Message{{Role: model.RoleUser, Content: "ask the remote agent"}},
		})
		if err != nil {
			t.Fatalf("invoke the assembled runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if body := answer.lastBody(); !strings.Contains(body, "ask the remote agent") {
			t.Fatalf("wire request = %q, want the message the model addressed to the declared agent", body)
		}

		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per decision", len(requests))
		}
		body, err := json.Marshal(big)
		if err != nil {
			t.Fatalf("serialize the remote result: %v", err)
		}
		ref := replica.OriginalRefFor(scope, body)
		var observation *model.Message
		for i := range requests[1].Messages {
			message := &requests[1].Messages[i]
			if message.ToolID == "call-remote" {
				observation = message
			}
			if len(message.Content) >= len(body) {
				t.Fatalf("the second request carries %d characters of a %d character result, want the bounded preview", len(message.Content), len(body))
			}
		}
		if observation == nil {
			t.Fatalf("the second request carries no settled observation of call-remote: %+v", requests[1].Messages)
		}
		if !strings.Contains(observation.Content, string(ref.Key)) || !strings.Contains(observation.Content, "externalized result") {
			t.Fatalf("settled observation = %q, want the ticket of the kept original %s", observation.Content, ref.Key)
		}
		if recalled := originals.Get(ctx, ref.Key); recalled.Outcome != ledger.RecallFound {
			t.Fatalf("original %s = outcome %v, want the remote result kept whole by the formal chain", ref.Key, recalled.Outcome)
		}

		mu.Lock()
		attribution := append([]remote.CallIdentity(nil), resolved...)
		mu.Unlock()
		if len(attribution) != 1 {
			t.Fatalf("resolved attributions = %+v, want the one call the model issued", attribution)
		}
		call := attribution[0]
		if call.InvocationID != "inv-addressed-delivery" || call.CallID != "call-remote" || call.DecisionID == "" {
			t.Fatalf("attribution = %+v, want the invocation, decision and native call of the formal chain", call)
		}
		record, ok := dispatcher.Call(call)
		if !ok || !record.Settled || record.Target != lookup(t, endpoints, "remote-a") {
			t.Fatalf("call record = (%+v, %v), want the addressed call settled at its declared target", record, ok)
		}
	})
}

// deliveryChain is one assembled internal delivery chain: the task manager
// offers each notification beside the routing its envelope froze, and the send
// seam delivers it into the explicit target that envelope names.
type deliveryChain struct {
	store    *state.MemoryStore
	face     *state.SendFace
	manager  *task.MemoryManager
	registry *task.MemoryRegistry
	running  map[event.InstanceID]bool
}

// newDeliveryChain builds the chain of one case beside the sender's deliver_to
// whitelist.
func newDeliveryChain(t *testing.T, deliverTo ...event.AgentID) *deliveryChain {
	t.Helper()
	chain := &deliveryChain{
		store:    state.NewMemoryStore(state.NewAuthority()),
		registry: task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission()),
		running:  map[event.InstanceID]bool{},
	}
	chain.face = state.NewSendFace(chain.store,
		state.WithDeliverTo(deliverTo...),
		state.WithRunningCheck(func(_ event.Address, instance event.InstanceID) bool {
			return chain.running[instance]
		}),
	)
	chain.manager = task.NewMemoryManager(chain.registry, nil, task.WithEnvelopeDeliverer(
		func(ctx context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
			_, err := chain.face.Send(ctx, state.Delivery{
				Target:         envelope.Target,
				TargetInstance: envelope.TargetInstance,
				Notice:         envelope.Notice,
			})
			if err != nil {
				return task.DeliveryRejected, err
			}
			return task.DeliveryAccepted, nil
		}))
	return chain
}

// open establishes one site and reports its loop running.
func (c *deliveryChain) open(t *testing.T, addr event.Address, instance event.InstanceID) {
	t.Helper()
	if _, err := c.store.Establish(context.Background(), addr, instance); err != nil {
		t.Fatalf("establish %+v on %s: %v", addr, instance, err)
	}
	c.running[instance] = true
}

// mailbox reports the bookkeeping of one established site.
func (c *deliveryChain) mailbox(t *testing.T, addr event.Address, instance event.InstanceID) *state.Session {
	t.Helper()
	session, err := c.store.Establish(context.Background(), addr, instance)
	if err != nil {
		t.Fatalf("read the site %+v on %s: %v", addr, instance, err)
	}
	return session
}

// register admits one task targeting the named site.
func (c *deliveryChain) register(t *testing.T, name string, target event.Address, instance event.InstanceID) event.TaskID {
	t.Helper()
	ctx := context.Background()
	receipt, err := c.manager.Admit(ctx, task.Spec{
		CallID:       "call-" + name,
		DedupKey:     task.DedupKey("dedup-" + name),
		InvocationID: event.InvocationID("inv-" + name),
		InstanceID:   instance,
		Description:  "deliver the report of " + name,
		Source:       event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Target:       target,
		Execution:    "serial",
	})
	if err != nil || !receipt.Accepted {
		t.Fatalf("admit %s = (%+v, %v), want one accepted admission", name, receipt, err)
	}
	if result, err := c.registry.Advance(ctx, receipt.TaskID, task.StatusRunning, task.MutationID("run-"+name), nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance %s = (%v, %v), want the task running", name, result, err)
	}
	return receipt.TaskID
}

// issue records one progress notification of the task beside its frozen routing.
func (c *deliveryChain) issue(t *testing.T, taskID event.TaskID, fact event.EventID) task.NoticeEnvelope {
	t.Helper()
	ctx := context.Background()
	envelope, err := c.registry.IssueNotice(ctx, taskID, task.NoticeProgress, fact)
	if err != nil {
		t.Fatalf("issue %s: %v", fact, err)
	}
	if result, err := c.registry.Advance(ctx, taskID, task.StatusProgress, task.MutationID("progress-"+string(fact)), []task.NoticeEnvelope{envelope}); result != task.CommitCommitted || err != nil {
		t.Fatalf("commit %s = (%v, %v), want the notification committed", fact, result, err)
	}
	return envelope
}

// send drives one delivery through the send seam.
func (c *deliveryChain) send(t *testing.T, ctx context.Context, delivery state.Delivery) state.SendReceipt {
	t.Helper()
	receipt, err := c.face.Send(ctx, delivery)
	if err != nil {
		t.Fatalf("send %+v: %v", delivery.Notice, err)
	}
	return receipt
}

// chainFrame is one call frame of a delivery driven beside an explicit
// attribution.
func chainFrame(invocation event.InvocationID) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InvocationID: invocation,
			DecisionID:   event.DecisionID("dec-" + string(invocation)),
			CallID:       "call-" + string(invocation),
		},
		Address:   event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Execution: "serial",
	}
}

// checkRefusal checks one ledger entry against the delivery it names.
func checkRefusal(refusals []state.Refusal, notice task.Notice, target event.Address, category state.RefusalCategory) error {
	if len(refusals) != 1 {
		return fmt.Errorf("refusals = %+v, want the one entry of the refused delivery", refusals)
	}
	refusal := refusals[0]
	if refusal.Notice != notice || refusal.Target != target || refusal.Category != category {
		return fmt.Errorf("refusal = %+v, want the refused %+v to %+v under %q", refusal, notice, target, category)
	}
	if refusal.At.IsZero() {
		return fmt.Errorf("refusal = %+v, want the time the delivery was refused", refusal)
	}
	return nil
}

// aRemoteAgent is one stand-in A2A agent: it counts the requests it serves,
// refuses the first ones it is told to fail and answers with a message result.
type aRemoteAgent struct {
	mu        sync.Mutex
	text      string
	failFirst int
	served    int
	body      string
}

// newARemoteAgent returns a stand-in agent answering with the given text.
func newARemoteAgent(text string) *aRemoteAgent { return &aRemoteAgent{text: text} }

// ServeHTTP serves one JSON-RPC message/send request.
func (a *aRemoteAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.served++
	hit := a.served
	a.body = string(body)
	text, fail := a.text, hit <= a.failFirst
	a.mu.Unlock()
	if fail {
		http.Error(w, "the remote agent is not available", http.StatusServiceUnavailable)
		return
	}
	reply, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(requestID(body)),
		"result": map[string]any{
			"kind":      a2aprotocol.KindMessage,
			"messageId": "msg-1",
			"role":      string(a2aprotocol.MessageRoleAgent),
			"parts":     []map[string]any{{"kind": a2aprotocol.KindText, "text": text}},
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}

// hits reports the requests the stand-in agent served.
func (a *aRemoteAgent) hits() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.served
}

// lastBody reports the last request body the stand-in agent read.
func (a *aRemoteAgent) lastBody() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.body
}

// requestID reports the raw identifier of one JSON-RPC request body.
func requestID(body []byte) string {
	var request struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &request); err != nil || len(request.ID) == 0 {
		return `"1"`
	}
	return string(request.ID)
}

// newEndpoints declares the remote agents of one case.
func newEndpoints(t *testing.T, targets ...remote.Target) *remote.Endpoints {
	t.Helper()
	endpoints, err := remote.NewEndpoints(targets...)
	if err != nil {
		t.Fatalf("declare the remote endpoints: %v", err)
	}
	return endpoints
}

// lookup reports the target one agent name declares.
func lookup(t *testing.T, endpoints *remote.Endpoints, agent string) remote.Target {
	t.Helper()
	target, err := endpoints.Lookup(agent)
	if err != nil {
		t.Fatalf("look up %q: %v", agent, err)
	}
	return target
}

// newDispatcher builds the addressed dispatcher over the declared endpoints.
func newDispatcher(t *testing.T, endpoints *remote.Endpoints, attempts int) *remote.A2ADispatcher {
	t.Helper()
	dispatcher, err := remote.NewA2ADispatcher(remote.NativeConnector{},
		remote.WithEndpoints(endpoints),
		remote.WithRetryPolicy(remote.NewRetryPolicy(attempts)),
	)
	if err != nil {
		t.Fatalf("build the addressed dispatcher: %v", err)
	}
	return dispatcher
}

// remoteParams is one message/send parameter set carrying the given text.
func remoteParams(text string) a2aprotocol.SendMessageParams {
	return a2aprotocol.SendMessageParams{
		Message: a2aprotocol.NewMessage(a2aprotocol.MessageRoleUser, []a2aprotocol.Part{a2aprotocol.NewTextPart(text)}),
	}
}
