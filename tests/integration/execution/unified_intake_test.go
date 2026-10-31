package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// intakeEntryAgent names the material this anchor is written in, so every message is
// material the endpoint served and every invocation reaches an address the document
// really declares.
//
// - intakeSlowBody is the body the in-flight call delivers, and the outbound face parks on it
// - intakeSlowCallID is the call identity of that call, so a decision is read from the tool message that pairs it
const (
	intakeEntryAgent       = "user"
	intakeEntryModel       = "model-entry"
	intakeSession          = "session-intake"
	intakeFirstInvocation  = "inv-intake-first"
	intakeSecondInvocation = "inv-intake-second"
	intakeSlowCallID       = "call-intake-slow"
	intakeSlowBody         = "intake: the slow call is running"
	intakeFirstBody        = "intake: the message that opens the turn"
	intakeSecondBody       = "intake: the message that arrives in flight"
	intakeRepeatBody       = "intake: the same text sent twice"
	intakeFinalBody        = "intake: the slow call finished"
	// intakeInFlightBody is the message that lands in the chain of the turn in flight,
	// committed through the run's own write face while the slow call is parked.
	intakeInFlightBody = "intake: the message that lands in the chain in flight"
	// intakeEnvironmentSection is the named section the dynamic environment block
	// opens with: the system channel carries it the way the fixed prompt carries
	// its own material, so no marker of our own stands beside the adapter framing.
	intakeEnvironmentSection = "## Environment"
	// intakeEnvironmentPrompt is the prompt body the environment document declares,
	// so the block names a fixed content version instead of the empty face.
	intakeEnvironmentPrompt = "intake: the prompt body the environment block names"
	// intakeEnvironmentTool is the tool face the environment declaration carries,
	// so the block names a real tool instead of the empty face.
	intakeEnvironmentTool = "env-probe"
)

// intakeAcceptWindow bounds how long an in-flight invocation is read for an answer
// before the parked call is released: the answer of such an invocation belongs to the
// segment that reads the release, so one that answers earlier drove a turn of its own.
// intakeBootWindow bounds the wait for the call to park, intakeSettleWindow guards a
// harness that broke, and intakeParkCeiling bounds one parked delivery so a call
// nothing releases fails by name instead of hanging the package.
const (
	intakeAcceptWindow = 2 * time.Second
	intakeBootWindow   = 20 * time.Second
	intakeSettleWindow = 20 * time.Second
	intakeParkCeiling  = 30 * time.Second
)

// TestMessageReachesTheNextDecisionOfAInFlightTurn checks that a message arriving inside a slow call reaches the next decision of the same turn.
//
// - positive: the invocation of that message is answered by the segment the turn in flight drives, the completed turn of its two decisions
// - positive: the message stands in the next decision of that segment once, and lands as one fact
// - negative: the decision already running reads nothing of it, and the turn in flight ends by its own terminal
// - negative: the message drives no segment of its own, so the run serves the two decisions of the one turn in flight
// - negative: a fact landing beside unfinished material only appends, so the projection prefix that stood stays an exact prefix
// - negative: a progress notice enters no decision input, in this window too
// - negative: two identical texts sent in one turn are read twice, since the consumed identity de-duplicates
// - no request may carry the text more times than the two facts hold it: a third copy in one request would be a fact read twice as fresh material
// - the identity each fact commits under keeps the two apart, so the text is never collapsed into one read
// - consumer: every invocation is one Host.Invoke over a host booted from a real document, read back from the requests served
//
// 契约: docs/wiki/agent/event-flow.md
func TestMessageReachesTheNextDecisionOfAInFlightTurn(t *testing.T) {
	t.Run("the message lands in the next decision of the in-flight turn", func(t *testing.T) {
		scene := intakeDrive(t, intakeSecondBody)

		t.Run("positive: the in-flight invocation is accepted and the message reaches the next decision once", func(t *testing.T) {
			accepted := scene.accepted[0]
			if accepted.err != nil {
				t.Fatalf("invoke beside the in-flight turn = %v, want the message answered beside no error", accepted.err)
			}
			if accepted.turn.Terminal != model.KindCompleted || len(accepted.turn.Decisions) != 2 {
				t.Fatalf("accepted turn = (%v, %d decisions), want the completed turn of the segment that consumed the message: "+
					"the call that was parked beside the decision that read its result (%s)",
					accepted.turn.Terminal, len(accepted.turn.Decisions), intakeDescribe(scene.requests))
			}
			if accepted.turn.InvocationID != scene.first.InvocationID {
				t.Fatalf("the message was answered by the turn %q, want the segment %q the turn already runs: "+
					"a message that lands in flight is read by the decision of that segment, not by a turn of its own",
					accepted.turn.InvocationID, scene.first.InvocationID)
			}
			at, ok := intakeDecisionAfterCall(scene.requests)
			if !ok {
				t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
			}
			if copies := intakeUserCopies(scene.requests[at], intakeSecondBody); copies != 1 {
				t.Fatalf("copies of the in-flight message in the next decision = %d, want exactly 1 (%s)",
					copies, intakeDescribe(scene.requests))
			}
			if landed := intakeFactsCarrying(scene.facts, intakeSecondBody); len(landed) != 1 {
				t.Fatalf("facts carrying the in-flight message = %d, want the one fact the message lands as: "+
					"an accepted message is committed once and no later turn commits it again", len(landed))
			}
		})

		t.Run("negative: the in-flight turn reads the message from no other lane", func(t *testing.T) {
			if scene.firstErr != nil || scene.first.Terminal != model.KindCompleted {
				t.Fatalf("the turn in flight = (%v, %v), want it to end by its own terminal beside the message that arrived in it",
					scene.first.Terminal, scene.firstErr)
			}
			if copies := intakeUserCopies(scene.requests[0], intakeSecondBody); copies != 0 {
				t.Fatalf("copies of the in-flight message in the decision already running = %d, want 0: "+
					"a running decision is never fed the message that arrives beside it (%s)",
					copies, intakeDescribe(scene.requests))
			}
			if len(scene.requests) != 2 {
				t.Fatalf("model requests = %d (%s), want the two decisions of the one in-flight turn: "+
					"an in-flight message drives no turn of its own", len(scene.requests), intakeDescribe(scene.requests))
			}
		})

		t.Run("negative: the projection prefix of the unfinished material is unchanged", func(t *testing.T) {
			if !scene.beforeCarried || !scene.afterCarried {
				t.Fatalf("projection carried = (%t, %t), want the session chain to register the mapped sequence the prefix is read from",
					scene.beforeCarried, scene.afterCarried)
			}
			if len(scene.after) < len(scene.before) {
				t.Fatalf("projection entries = %d after, %d before, want a landed fact to append and never to shorten the chain",
					len(scene.after), len(scene.before))
			}
			for at := range scene.before {
				if scene.after[at].Key != scene.before[at].Key ||
					scene.after[at].Type != scene.before[at].Type ||
					scene.after[at].Content != scene.before[at].Content {
					t.Fatalf("projection entry %d = %+v, want the entry %+v that stood when the message arrived: "+
						"material that is not finished is never rewritten by a fact landing beside it",
						at, scene.after[at], scene.before[at])
				}
			}
			// TODO(A2): pin the ended-material boundary itself beside this prefix, the position the next request presents
		})

		t.Run("negative: no decision input carries progress material", func(t *testing.T) {
			if len(scene.requests) == 0 {
				t.Fatalf("model requests = 0, want the decisions of the in-flight turn to be served")
			}
			for _, request := range scene.requests {
				for _, message := range request.Messages {
					if strings.Contains(message.Content, progressNoteFragment) {
						t.Fatalf("request messages = %+v, want no progress report in the material a decision reads: "+
							"the in-flight window is no second lane for notices", request.Messages)
					}
				}
			}
		})
	})

	t.Run("the same text sent twice in one turn stands in the input twice", func(t *testing.T) {
		scene := intakeDrive(t, intakeRepeatBody, intakeRepeatBody)
		landed := intakeFactsCarrying(scene.facts, intakeRepeatBody)
		if len(landed) != 2 {
			t.Fatalf("facts carrying the same text = %d, want the two facts the two entries committed (%s)",
				len(landed), intakeDescribe(scene.requests))
		}
		if landed[0].Key == landed[1].Key {
			t.Fatalf("fact keys of the two entries = %q, want two identities beside one text: "+
				"the chain de-duplicates by the identity a fact commits under, never by the content it reads as", landed[0].Key)
		}
		read := make([]int, 0, 2)
		copies := 0
		for _, request := range scene.requests {
			if got := intakeUserCopies(request, intakeRepeatBody); got > 0 {
				read = append(read, request.Index)
				copies += got
				if got > len(landed) {
					t.Fatalf("request %d carries the same text %d times, want at most the %d facts that hold it",
						request.Index, got, len(landed))
				}
			}
		}
		if copies < len(landed) {
			t.Fatalf("the same text served to the model %d times in requests %v, want each of the %d facts read: "+
				"the consumed identity de-duplicates and the content does not (%s)", copies, read, len(landed), intakeDescribe(scene.requests))
		}
	})
}

// TestInflightUserMessageIsAcceptedNotQueued checks that a message arriving beside a running turn is accepted onto that turn's chain, not queued behind the gate.
//
// - positive: the message is answered by the segment the turn in flight drives, the completed turn of its two decisions
// - positive: the fact commits under the identity of the entry that submitted it, which is the source association the segment reads
// - negative: the in-flight turn reads the message once and drives no segment of its own for it, so nothing is queued beside the gate
// - consumer: Host.Invoke over a host booted from a real document, read back from the requests served and the run's own chain
//
// 契约: docs/wiki/agent/event-flow.md
func TestInflightUserMessageIsAcceptedNotQueued(t *testing.T) {
	scene := intakeDrive(t, intakeSecondBody)

	accepted := scene.accepted[0]
	if accepted.err != nil {
		t.Fatalf("invoke beside the in-flight turn = %v, want the message answered beside no error", accepted.err)
	}
	if accepted.turn.Terminal != model.KindCompleted || len(accepted.turn.Decisions) != 2 {
		t.Fatalf("accepted turn = (%v, %d decisions), want the completed turn of the segment that consumed the message (%s)",
			accepted.turn.Terminal, len(accepted.turn.Decisions), intakeDescribe(scene.requests))
	}
	if accepted.turn.InvocationID != scene.first.InvocationID {
		t.Fatalf("the message was answered by the turn %q, want the segment %q the turn already runs: "+
			"a message accepted beside a running turn is material of that segment, so it queues nothing beside the gate",
			accepted.turn.InvocationID, scene.first.InvocationID)
	}

	landed := intakeFactsCarrying(scene.facts, intakeSecondBody)
	if len(landed) != 1 {
		t.Fatalf("facts carrying the in-flight message = %d, want the one fact the acceptance commits: a queued message is committed later, and only once it drives its own turn", len(landed))
	}
	submitted := event.InvocationID(intakeSecondInvocation + "-0")
	if landed[0].InvocationID != submitted {
		t.Fatalf("the landed fact belongs to %q, want the entry identity %q: each source keeps its own association beside the segment that reads it",
			landed[0].InvocationID, submitted)
	}

	at, ok := intakeDecisionAfterCall(scene.requests)
	if !ok {
		t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
	}
	if copies := intakeUserCopies(scene.requests[at], intakeSecondBody); copies != 1 {
		t.Fatalf("copies of the accepted message in the next decision of the turn in flight = %d, want exactly 1 (%s)",
			copies, intakeDescribe(scene.requests))
	}
	if len(scene.requests) != 2 {
		t.Fatalf("model requests = %d (%s), want the two decisions of the one turn in flight: an accepted message queues no turn of its own behind the gate",
			len(scene.requests), intakeDescribe(scene.requests))
	}
}

// TestTheChainIsReadAgainAtTheDecisionBoundary checks that a message landing in the chain of a turn in flight reaches that turn's next decision.
//
// - positive: the message committed under the range in flight stands in the decision that reads the parked call's result, exactly once
// - negative: no second copy stands there, so the projection lane and the boundary read are one read of one chain and never two
// - consumer: the message commits through the run's own cross-session write face, over a host booted from a real document
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestTheChainIsReadAgainAtTheDecisionBoundary(t *testing.T) {
	scene := intakeLandingDrive(t, intakeInFlightBody)
	at, ok := intakeDecisionAfterCall(scene.requests)
	if !ok {
		t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
	}
	if scene.err != nil || scene.turn.Terminal != model.KindCompleted {
		t.Fatalf("turn in flight = (%v, %v), want it to end by its own terminal beside the message that landed in it",
			scene.turn.Terminal, scene.err)
	}
	if copies := intakeUserCopies(scene.requests[at], intakeInFlightBody); copies != 1 {
		t.Fatalf("copies of the message that landed in flight in the next decision = %d, want exactly 1: "+
			"the boundary reads the chain that already carries it, so a second copy means two lanes hold one input (%s)",
			copies, intakeDescribe(scene.requests))
	}
	if landed := intakeFactsCarrying(scene.facts, intakeInFlightBody); len(landed) != 1 {
		t.Fatalf("facts carrying the message that landed in flight = %d, want the one fact the message was committed as", len(landed))
	}
}

// TestTheChainKeepsTheSameTextTwice checks that two in-flight messages carrying one text reach the next decision as two inputs.
// - positive: both messages stand in the decision that reads the parked call's result, two copies of one text
// - negative: the boundary de-duplicates by the identity a fact committed under, never by the content it reads as
// - consumer: both messages commit through the run's own cross-session write face, over a host booted from a real document
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestTheChainKeepsTheSameTextTwice(t *testing.T) {
	scene := intakeLandingDrive(t, intakeRepeatBody, intakeRepeatBody)
	at, ok := intakeDecisionAfterCall(scene.requests)
	if !ok {
		t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
	}
	if scene.err != nil || scene.turn.Terminal != model.KindCompleted {
		t.Fatalf("turn in flight = (%v, %v), want it to end by its own terminal beside the messages that landed in it",
			scene.turn.Terminal, scene.err)
	}
	if copies := intakeUserCopies(scene.requests[at], intakeRepeatBody); copies != 2 {
		t.Fatalf("copies of one text landed as two facts = %d, want 2: the committed identity de-duplicates and the content does not (%s)",
			copies, intakeDescribe(scene.requests))
	}
}

// TestTheEnvironmentBlockClosesTheSystemMaterial checks where the environment block stands in the request the model reads.
//
// - positive: the last system message of every request is the environment section; every message after it is conversation
// - positive: the section names the tool face beside the declared prompt version
// - negative: no message carries a self-made marker, so the block never poses as conversation
// - positive: the tool face travels the native Tools field — the request carries no tool schema as text
// - negative: an agent declaring no environment block carries no environment section at all
// - consumer: assembly.Assemble + Run.Invoke, read back from the native requests the model entry was driven with
//
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func TestTheEnvironmentBlockClosesTheSystemMaterial(t *testing.T) {
	t.Run("the block closes the system material of every request", func(t *testing.T) {
		requests := intakeEnvironmentDrive(t, true)
		if len(requests) == 0 {
			t.Fatal("model requests = 0, want the turn served")
		}
		for index, request := range requests {
			if len(request.Messages) == 0 {
				t.Fatalf("request %d carries no message", index)
			}
			lastSystem := -1
			for at, message := range request.Messages {
				if message.Role == model.RoleSystem {
					lastSystem = at
				}
			}
			if lastSystem < 0 {
				t.Fatalf("request %d carries no system message (%s)", index, intakeEnvironmentShape(requests))
			}
			block := request.Messages[lastSystem]
			if !strings.HasPrefix(block.Content, intakeEnvironmentSection) {
				t.Fatalf("last system message of request %d = %q, want it to open with the environment section (%s)",
					index, block.Content, intakeEnvironmentShape(requests))
			}
			for at := lastSystem + 1; at < len(request.Messages); at++ {
				if request.Messages[at].Role == model.RoleSystem {
					t.Fatalf("message %d of request %d is system material behind the conversation: the block closes the system channel (%s)",
						at, index, intakeEnvironmentShape(requests))
				}
			}
			for _, want := range []string{"- agent: " + intakeEntryAgent, "- model: " + intakeEntryModel, "- skills: (none)", "- mcp: (none)", "- tasks: 0 open"} {
				if !strings.Contains(block.Content, want) {
					t.Fatalf("environment block = %q, want it to name %q", block.Content, want)
				}
			}
			if tools := intakeEnvironmentValue(block.Content, "- tools:"); !strings.Contains(tools, intakeEnvironmentTool) {
				t.Fatalf("environment block tool face = %q, want it to name %q (%q)", tools, intakeEnvironmentTool, block.Content)
			}
			if version := intakeEnvironmentValue(block.Content, "- prompt_version:"); version == "" || version == "(none)" {
				t.Fatalf("environment block prompt version = %q, want the content version the request boundary fixed (%q)", version, block.Content)
			}
			if request.Tools[intakeEnvironmentTool] == nil {
				t.Fatalf("native tools of request %d carry no %q: the tool face travels the Tools field, never the transcript (%s)",
					index, intakeEnvironmentTool, intakeEnvironmentShape(requests))
			}
			for at, message := range request.Messages {
				if strings.Contains(message.Content, `"parameters"`) || strings.Contains(message.Content, "<environment>") {
					t.Fatalf("message %d of request %d carries tool schema or a self-made marker: the parser layer owns both framings (%q)",
						at, index, message.Content)
				}
			}
		}
	})

	t.Run("negative: an undeclared block carries no environment section", func(t *testing.T) {
		requests := intakeEnvironmentDrive(t, false)
		if len(requests) == 0 {
			t.Fatal("model requests = 0, want the turn served")
		}
		for index, request := range requests {
			for at, message := range request.Messages {
				if strings.Contains(message.Content, intakeEnvironmentSection) {
					t.Fatalf("message %d of request %d = (%q, %q), want no environment section for an agent that declares none (%s)",
						at, index, message.Role, message.Content, intakeEnvironmentShape(requests))
				}
			}
		}
	})
}

// intakeEnvironmentDrive runs one turn over the assembled runtime and answers the
// native requests the model entry was driven with. The declared run boots the
// agent that declares the block; the negative run boots the same agent declaring
// none, so the two runs differ in the declaration alone.
func intakeEnvironmentDrive(t *testing.T, declared bool) []model.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	root := t.TempDir()
	promptPath := filepath.Join(root, "system.md")
	if err := os.WriteFile(promptPath, []byte(intakeEnvironmentPrompt), 0o644); err != nil {
		t.Fatalf("write the declared prompt file: %v", err)
	}
	provider := &convModel{name: intakeEntryModel, answer: func(*model.Request) *model.Response {
		return convFinalTurn(intakeFinalBody)
	}}
	decl := &config.Declaration{
		Entry: intakeEntryAgent,
		Agents: []config.AgentSpec{{
			ID:               intakeEntryAgent,
			Model:            intakeEntryModel,
			EnvironmentBlock: declared,
			Files:            []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}},
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		intakeEntryAgent: {Model: provider, Tools: []trpctool.CallableTool{&convTool{name: intakeEnvironmentTool, output: intakeFinalBody}}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	turn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: intakeFirstInvocation,
		Address:      event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession},
		Input:        []model.Message{{Role: model.RoleUser, Content: intakeFirstBody}},
	})
	if err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the environment turn = (%v, %v), want it to end by its own terminal", turn.Terminal, err)
	}
	return provider.recorded()
}

// intakeEnvironmentValue reports the value one line prefix carries in the block.
func intakeEnvironmentValue(block, linePrefix string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, linePrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, linePrefix))
		}
	}
	return ""
}

// intakeEnvironmentShape renders the roles and the trailing marker of every
// request, so a failure states where the block really stands.
func intakeEnvironmentShape(requests []model.Request) string {
	described := make([]string, 0, len(requests))
	for index, request := range requests {
		roles := make([]string, 0, len(request.Messages))
		section := ""
		for _, message := range request.Messages {
			roles = append(roles, string(message.Role))
			if strings.Contains(message.Content, intakeEnvironmentSection) {
				section = string(message.Role)
			}
		}
		described = append(described, fmt.Sprintf("#%d roles=%v section=%q", index, roles, section))
	}
	return "[" + strings.Join(described, "; ") + "]"
}

// intakeLandingScene is one run of the boundary anchor: the turn that was in flight and
// the material read back from the model endpoint and the run itself.
type intakeLandingScene struct {
	turn     agent.Turn
	err      error
	requests []intakeRequest
	facts    []event.Event
}

// intakeLandingDrive opens a turn whose first decision issues the slow call, commits each
// in-flight message into the chain under that turn's own invocation while the call is
// parked, releases the call, and reads back what the model and the run recorded. The
// commit is the run's own write face, so the material is in the very chain the drive
// commits to and no lane beside it.
//
// The invocation of the commit is the range in flight, which is the identity the host
// records beside an input admitted while a turn runs (see the host's acceptance face).
func intakeLandingDrive(t *testing.T, landed ...string) *intakeLandingScene {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession}

	firstDone := make(chan intakeLandingScene, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeFirstInvocation,
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: intakeFirstBody}},
		})
		firstDone <- intakeLandingScene{turn: turn, err: err}
	}()
	sender.awaitParked(t)
	for _, body := range landed {
		if _, err := host.Run().CommitToSession(ctx, addr, event.Event{
			Type:         event.TypeExternalInput,
			Message:      model.Message{Role: model.RoleUser, Content: body},
			InvocationID: intakeFirstInvocation,
		}); err != nil {
			t.Fatalf("commit the message %q into the chain of the turn in flight: %v", body, err)
		}
	}
	sender.release()

	scene := &intakeLandingScene{}
	select {
	case settled := <-firstDone:
		scene.turn, scene.err = settled.turn, settled.err
	case <-time.After(intakeSettleWindow):
		t.Fatalf("the turn in flight never ended after its call was released")
	}
	scene.requests = provider.requests()
	scene.facts = host.Run().SessionFacts(addr)
	return scene
}

// intakeScene is one run of the anchor: the invocation that opens the turn, the
// invocations that arrive beside it, and the material read back from the model
// endpoint and the run itself.
type intakeScene struct {
	addr     event.Address
	first    agent.Turn
	firstErr error
	// accepted carries one entry per message the run sent in flight, in issue
	// order; waited marks the entry whose invocation answered nothing inside
	// the acceptance window and was read after the parked call was released.
	accepted      []intakeAcceptance
	requests      []intakeRequest
	before        []replay.ProjectionEntry
	beforeCarried bool
	after         []replay.ProjectionEntry
	afterCarried  bool
	facts         []event.Event
}

// intakeAcceptance is the receipt one in-flight invocation answered with, or the
// record that it answered nothing before the acceptance window ended.
type intakeAcceptance struct {
	turn   agent.Turn
	err    error
	waited bool
}

// intakeDrive runs the anchor once: it opens a turn whose first decision issues
// the slow call, waits until that call is really parked inside the outbound
// face, reads the projection prefix that stands at that moment, sends the given
// messages in the same session, releases the call, and reads back what the
// model and the run recorded.
func intakeDrive(t *testing.T, inFlight ...string) *intakeScene {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	scene := &intakeScene{addr: event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession}}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		scene.first, scene.firstErr = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeFirstInvocation,
			Address:      scene.addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: intakeFirstBody}},
		})
	}()
	sender.awaitParked(t)
	scene.before, scene.beforeCarried = host.Run().ReplayEntries(scene.addr.SessionID)

	type arrival struct {
		turn agent.Turn
		err  error
	}
	arrivals := make([]chan arrival, len(inFlight))
	for at, body := range inFlight {
		arrivals[at] = make(chan arrival, 1)
		invocationID := event.InvocationID(fmt.Sprintf("%s-%d", intakeSecondInvocation, at))
		go func(ch chan arrival, id event.InvocationID, content string) {
			turn, err := host.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: id,
				Address:      scene.addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: content}},
			})
			ch <- arrival{turn: turn, err: err}
		}(arrivals[at], invocationID, body)
	}
	for at := range inFlight {
		select {
		case answer := <-arrivals[at]:
			scene.accepted = append(scene.accepted, intakeAcceptance{turn: answer.turn, err: answer.err})
		case <-time.After(intakeAcceptWindow):
			scene.accepted = append(scene.accepted, intakeAcceptance{waited: true})
		}
	}

	sender.release()
	select {
	case <-firstDone:
	case <-time.After(intakeSettleWindow):
		t.Fatalf("the turn in flight never ended after its call was released")
	}
	for at := range inFlight {
		if !scene.accepted[at].waited {
			continue
		}
		select {
		case answer := <-arrivals[at]:
			scene.accepted[at] = intakeAcceptance{turn: answer.turn, err: answer.err, waited: true}
		case <-time.After(intakeSettleWindow):
			t.Fatalf("the in-flight invocation %d never returned beside a released call", at)
		}
	}
	scene.requests = provider.requests()
	scene.after, scene.afterCarried = host.Run().ReplayEntries(scene.addr.SessionID)
	scene.facts = host.Run().SessionFacts(scene.addr)
	return scene
}

// intakeProvider is the OpenAI-compatible endpoint the anchor boots against: the
// first request of the run issues the slow call, every later request ends the
// turn that reads it, and every request is recorded in the shape the anchor
// reads its evidence from.
type intakeProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	// firstAnswer overrides the answer of the first request, so an anchor
	// scripts the calls its opening decision issues; empty serves the one slow
	// send call the in-flight anchors park on.
	firstAnswer string
	// answers overrides the answer of the request at one index, so an anchor
	// scripts the calls of a round that is not the first one; an index beside no
	// entry serves the final answer, or the slow call for the first request.
	answers map[int]string
}

// newIntakeProvider starts the endpoint and closes it with the test.
func newIntakeProvider(t *testing.T) *intakeProvider {
	t.Helper()
	provider := &intakeProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the request it carried.
func (p *intakeProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	p.mu.Lock()
	index := len(p.bodies)
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()
	payload := intakeFinalAnswer(intakeFinalBody)
	if index == 0 {
		payload = intakeToolCallAnswer("send", intakeSlowCallID, `{"content":`+intakeQuoted(intakeSlowBody)+`}`)
		if p.firstAnswer != "" {
			payload = p.firstAnswer
		}
	}
	if scripted, held := p.answers[index]; held {
		payload = scripted
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
}

// requests reports the requests the endpoint served, in request order.
func (p *intakeProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// endpoint reports the base URL the host document's provider entry names.
func (p *intakeProvider) endpoint() string { return p.server.URL + "/v1" }

// intakeRequest is one chat completion request as the endpoint decoded it.
type intakeRequest struct {
	Index    int
	Model    string
	Messages []intakeMessage
	Tools    []string
}

// intakeMessage is one message a request carried.
type intakeMessage struct {
	Role       string
	Content    string
	ToolCallID string
}

// intakeDecodeRequest reads one recorded body into the shape the anchor reads.
func intakeDecodeRequest(index int, modelName string, body map[string]json.RawMessage) intakeRequest {
	request := intakeRequest{Index: index, Model: modelName}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				request.Messages = append(request.Messages, intakeMessage{
					Role:       message.Role,
					Content:    intakeContent(message.Content),
					ToolCallID: message.ToolCallID,
				})
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// intakeContent reads one message content the protocol may render as a plain
// string or as text parts.
func intakeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "")
}

// intakeQuoted renders one string as the JSON literal a scripted answer carries.
func intakeQuoted(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic("execution: encode the scripted material: " + err.Error())
	}
	return string(encoded)
}

// intakeToolCallAnswer renders the chat completion that issues one native call.
func intakeToolCallAnswer(name, callID, arguments string) string {
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"calling","tool_calls":[{"id":"` + callID +
		`","type":"function","function":{"name":"` + name + `","arguments":` + intakeQuoted(arguments) + `}}]},` +
		`"finish_reason":"tool_calls"}]}`
}

// intakeFinalAnswer renders the chat completion that ends the turn.
func intakeFinalAnswer(content string) string {
	return `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":` + intakeQuoted(content) + `},"finish_reason":"stop"}]}`
}

// intakeSender is the outbound face of the booted host: it records every body it
// accepted, and it parks the one body the slow call delivers until the anchor
// releases it, so the in-flight window is a real tool execution rather than an
// elapsed timeout.
type intakeSender struct {
	mu      sync.Mutex
	targets []event.Address
	bodies  []string
	parked  chan struct{}
	freed   chan struct{}
	once    sync.Once
	opened  sync.Once
}

// newIntakeSender returns the recording outbound face of one run.
func newIntakeSender() *intakeSender {
	return &intakeSender{parked: make(chan struct{}), freed: make(chan struct{})}
}

// Send records one accepted body and parks the slow call's body until release.
func (s *intakeSender) Send(ctx context.Context, target event.Address, content string) error {
	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.bodies = append(s.bodies, content)
	s.mu.Unlock()
	if content != intakeSlowBody {
		return nil
	}
	s.opened.Do(func() { close(s.parked) })
	select {
	case <-s.freed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(intakeParkCeiling):
		return fmt.Errorf("execution: the slow call was never released")
	}
}

// awaitParked waits until the slow call really reached the outbound face.
func (s *intakeSender) awaitParked(t *testing.T) {
	t.Helper()
	select {
	case <-s.parked:
	case <-time.After(intakeBootWindow):
		t.Fatalf("no decision reached the slow call within %s, so the in-flight window was never opened", intakeBootWindow)
	}
}

// release frees the parked call, exactly once.
func (s *intakeSender) release() {
	s.once.Do(func() { close(s.freed) })
}

// accepted reports the bodies the outbound face accepted so far, in send order.
func (s *intakeSender) accepted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// intakeHostDocument renders the one document the anchor boots: the entry agent
// keeps the declared send tool, whose outbound face the anchor supplies, so one
// real decision can be parked inside a real tool execution.
func intakeHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + intakeEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + intakeEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + intakeEntryAgent + ":\n")
	b.WriteString("    model: " + intakeEntryModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: send }\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// intakeBoot boots the formal host the anchor drives, so every invocation below
// reaches the runtime through the public entry the deployment uses.
func intakeBoot(t *testing.T, provider *intakeProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	return intakeBootDocument(t, provider, sender, intakeHostDocument)
}

// intakeBootDocument boots the formal host of the document one anchor renders,
// so a test that declares a budget reaches the runtime through the very
// document face a deployment declares it under.
func intakeBootDocument(t *testing.T, provider *intakeProvider, sender assembly.MessageSender, render func(root, endpoint string) string) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(render(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the intake host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the intake host ended its close beside: %v", err)
		}
	})
	return host
}

// intakeDecisionAfterCall reports the first request that reads the result of the
// scripted call: the decision boundary the in-flight message has to reach.
func intakeDecisionAfterCall(requests []intakeRequest) (int, bool) {
	for at, request := range requests {
		for _, message := range request.Messages {
			if message.Role == string(model.RoleTool) && message.ToolCallID == intakeSlowCallID {
				return at, true
			}
		}
	}
	return 0, false
}

// intakeUserCopies counts the user messages of one request that carry the named
// material.
func intakeUserCopies(request intakeRequest, material string) int {
	count := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.Contains(message.Content, material) {
			count++
		}
	}
	return count
}

// intakeFactsCarrying reports the committed facts of the session whose message
// carries the named material, so a check reads the chain the run really holds
// rather than the shape it meant to write.
func intakeFactsCarrying(facts []event.Event, material string) []event.Event {
	out := make([]event.Event, 0, len(facts))
	for _, fact := range facts {
		if strings.Contains(fact.Message.Content, material) && fact.Type == event.TypeExternalInput {
			out = append(out, fact)
		}
	}
	return out
}

// intakeDescribe renders the requests the anchor reads its evidence from: the
// identity each one named, and the role and call pairing of every message, so a
// failure states what the model really saw.
func intakeDescribe(requests []intakeRequest) string {
	described := make([]string, 0, len(requests))
	for _, request := range requests {
		roles := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			role := message.Role
			if message.ToolCallID != "" {
				role += "(" + message.ToolCallID + ")"
			}
			roles = append(roles, role)
		}
		described = append(described, fmt.Sprintf("#%d model=%q messages=%v tools=%v", request.Index, request.Model, roles, request.Tools))
	}
	return "[" + strings.Join(described, "; ") + "]"
}
