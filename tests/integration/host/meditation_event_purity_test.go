package host

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/ledger"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestMeditationDeliversNothingWithoutModelCall fires a reflection pass whose model never calls deliver_summary and finds the target chain unchanged:
// - the fired pass reaches a terminal with zero deliver_summary calls
// - the target session keeps exactly the facts its user turn committed
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationDeliversNothingWithoutModelCall(t *testing.T) {
	rig := newMeditationRig(t, 0, func(*model.Request) *model.Response {
		return entryFinalTurn("noted")
	})
	invokeUserTurn(t, rig.run, "inv-user-quiet", rig.userAddr, "hello")
	before := rig.run.SessionFacts(rig.userAddr)

	rig.awaitFire(t)

	after := rig.run.SessionFacts(rig.userAddr)
	if len(after) != len(before) {
		t.Fatalf("target chain length = %d, want the %d facts the user turn committed and nothing beside them", len(after), len(before))
	}
	if delivered := meditationStamped(after); len(delivered) != 0 {
		t.Fatalf("meditation-stamped facts = %d, want no cross-session write without a model call: %+v", len(delivered), delivered)
	}
	requests := rig.medModel.recorded()
	if len(requests) != 1 {
		t.Fatalf("meditation requests = %d, want exactly one fired reflection turn", len(requests))
	}
	if last := lastMessage(requests[0]); !strings.HasPrefix(last.Content, "[reflection]") {
		t.Fatalf("reflection input = %q, want the fired reflection turn to have run", last.Content)
	}
}

// TestDeliverSummaryLandsOnTargetChain has the model deliver one summary and finds it committed to the target chain, visible to the next assembly:
// - the target chain gains exactly one external input stamped with the meditation lineage
// - the target session's next request carries the delivered content
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestDeliverSummaryLandsOnTargetChain(t *testing.T) {
	rig := newMeditationRig(t, time.Hour, func(req *model.Request) *model.Response {
		if strings.HasPrefix(lastMessage(*req).Content, "[reflection]") {
			return entryToolTurn(entryCall("call-summary", meditation.DeliverSummaryToolName,
				`{"content":"cross-session summary"}`))
		}
		return entryFinalTurn("summary delivered")
	})
	invokeUserTurn(t, rig.run, "inv-user-visible", rig.userAddr, "hello")
	before := len(rig.run.SessionFacts(rig.userAddr))

	outcome := rig.awaitFire(t)
	if outcome.Injection == nil {
		t.Fatal("fired reflection carries no injection")
	}

	facts := rig.run.SessionFacts(rig.userAddr)
	delivered := meditationStamped(facts)
	if len(delivered) != 1 {
		t.Fatalf("meditation-stamped facts = %d, want the one summary the model delivered: %+v", len(delivered), delivered)
	}
	if len(facts) != before+1 {
		t.Fatalf("target chain length = %d, want exactly one fact beside the %d the user turn committed", len(facts), before)
	}
	summary := delivered[0]
	if summary.Type != event.TypeExternalInput {
		t.Fatalf("summary type = %q, want the external input the cross-session write commits", summary.Type)
	}
	if summary.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
		t.Fatalf("summary stamp = %q, want the meditation lineage", summary.Meta[event.MetaKeyTriggerSource])
	}
	if summary.Message.Content != "cross-session summary" {
		t.Fatalf("summary body = %q, want the content the model delivered", summary.Message.Content)
	}
	if summary.Owner != rig.userAddr || summary.Target != rig.userAddr {
		t.Fatalf("summary addresses = (owner %+v, target %+v), want the target session %+v", summary.Owner, summary.Target, rig.userAddr)
	}

	invokeUserTurn(t, rig.run, "inv-user-next", rig.userAddr, "second round")
	requests := rig.userModel.recorded()
	if len(requests) < 2 {
		t.Fatalf("user requests = %d, want the next round's assembly", len(requests))
	}
	next := requests[len(requests)-1]
	visible := false
	for _, message := range next.Messages {
		if message.Content == "cross-session summary" {
			visible = true
			break
		}
	}
	if !visible {
		t.Fatalf("next request messages = %+v, want the delivered summary assembled into the target session", next.Messages)
	}
}

// TestSummaryStampNoNovelty pins the loop-prevention rule: a summary the meditation lineage delivered never reads as novel material.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestSummaryStampNoNovelty(t *testing.T) {
	var policy meditation.NoveltyPolicy
	summary := event.Event{
		Type: event.TypeExternalInput,
		Meta: map[string]string{event.MetaKeyTriggerSource: meditation.LineageReflection},
	}
	if policy.Novel(summary) {
		t.Fatal("the meditation stamp reads as new material, want a delivered summary to never re-open the freshness gate")
	}
	user := event.Event{
		Type: event.TypeExternalInput,
		Meta: map[string]string{event.MetaKeyTriggerSource: "user"},
	}
	if !policy.Novel(user) {
		t.Fatal("a user stamp reads as self-managed, want the same policy to keep host-facing traffic novel")
	}
}

// TestDeliverSummaryRefusals pins the refusal texts a reflection turn continues past:
// - empty content is refused as result text, never as an error
// - a target with no bound session is refused by name, never skipped in silence
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestDeliverSummaryRefusals(t *testing.T) {
	tool := meditation.NewDeliverSummaryTool([]string{"target"}, &meditationSink{}, noSessions{})
	cases := []struct {
		args string
		want string
	}{
		{`{"content":""}`, "[deliver_summary_denied] empty content"},
		{`{"content":"summary"}`, "[deliver_summary_denied] no bound target sessions"},
	}
	for _, tc := range cases {
		got, err := tool.Call(context.Background(), []byte(tc.args))
		if err != nil {
			t.Fatalf("deliver_summary(%s) error = %v, want a refusal as result text", tc.args, err)
		}
		if got != tc.want {
			t.Fatalf("deliver_summary(%s) = %v, want %q", tc.args, got, tc.want)
		}
	}
}

// noSessions is the live-binding face beside no bound session.
type noSessions struct{}

// ActiveSessions reports no binding.
func (noSessions) ActiveSessions(event.AgentID) []event.Address { return nil }

// meditationRig is one assembled runtime whose reflection driver observes the
// user agent's partition and injects into the meditator's own loop session: the
// two agents, the late-bound cross-session sink the summary tool writes
// through, and the channel the fired passes report on.
type meditationRig struct {
	run       *assembly.Run
	userModel *entryModel
	medModel  *entryModel
	userAddr  event.Address
	fired     chan meditation.TickOutcome
}

// newMeditationRig assembles the rig over a local storage tier and an anchor
// record, so the reflection reads the same chain the run commits and the
// summary tool writes through the same cross-session entry the host wires.
func newMeditationRig(t *testing.T, interval time.Duration, medAnswer func(*model.Request) *model.Response) *meditationRig {
	t.Helper()
	root := t.TempDir()
	userModel := &entryModel{name: "model-user", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("user output")
	}}
	medModel := &entryModel{name: "model-meditator", answer: medAnswer}
	sink := &meditationSink{}
	summary := meditation.NewDeliverSummaryTool([]string{"user-agent"}, sink, sink)
	fired := make(chan meditation.TickOutcome, 8)

	decl := &config.Declaration{
		Entry: "user-agent",
		Agents: []config.AgentSpec{
			{ID: "user-agent", Model: "model-user", MaxTokens: 100},
			{ID: "meditator", Model: "model-meditator"},
		},
	}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"user-agent": {Model: userModel},
		"meditator":  {Model: medModel, Tools: []trpctool.CallableTool{summary}},
	},
		assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}),
		assembly.WithReflectionAnchors(filepath.Join(root, "anchors.json")),
		assembly.WithPlugins(func(assembled *assembly.Run) ([]plugin.Instance, error) {
			reflection, err := meditation.NewReflection(meditation.ReflectionConfig{
				Loop: meditation.ReflectionLoop{Agent: "meditator", Session: "meditator-loop"},
				Face: meditation.ReflectionFace{
					ObservedNamespaces: []string{"user-agent"},
					ReadNamespaces:     []string{"user-agent", "meditator"},
				},
				Cadence: meditation.ReflectionCadence{Interval: interval},
				Prompt:  "write a summary when the reflection found something worth delivering",
			}, assembled.ReflectionGate(), assembled.ObservationFacts(), assembled.ReflectionInput())
			if err != nil {
				return nil, err
			}
			driver := meditation.NewDriver(reflection, interval, 0)
			driver.SetObserver(func(outcome meditation.TickOutcome) {
				if outcome.Decision.Disposition != meditation.TickFired {
					return
				}
				select {
				case fired <- outcome:
				default:
				}
			})
			return []plugin.Instance{driver}, nil
		}),
	)
	if err != nil {
		t.Fatalf("assemble the meditation rig: %v", err)
	}
	sink.Bind(run)
	return &meditationRig{
		run:       run,
		userModel: userModel,
		medModel:  medModel,
		userAddr:  event.Address{AgentID: "user-agent", SessionID: "session-user"},
		fired:     fired,
	}
}

// awaitFire waits for one fired reflection pass to settle, which is the moment
// its turn reached a terminal and any deliver_summary it made has committed.
func (r *meditationRig) awaitFire(t *testing.T) meditation.TickOutcome {
	t.Helper()
	select {
	case outcome := <-r.fired:
		return outcome
	case <-time.After(20 * time.Second):
		t.Fatal("the reflection driver fired no pass")
		return meditation.TickOutcome{}
	}
}

// meditationSink is the late-bound cross-session face of the rig: the tool is
// built before the run exists and forwards through the run bound after the
// assembly, exactly as the host's port does.
type meditationSink struct {
	run *assembly.Run
}

// Bind attaches the assembled run.
func (s *meditationSink) Bind(run *assembly.Run) { s.run = run }

// CommitToSession commits one delivered event into the chain of its address.
func (s *meditationSink) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
	if s.run == nil {
		return "", errors.New("the meditation sink is bound to no run")
	}
	return s.run.CommitToSession(ctx, addr, evt)
}

// ActiveSessions reports the run's live bindings of one agent.
func (s *meditationSink) ActiveSessions(agentID event.AgentID) []event.Address {
	if s.run == nil {
		return nil
	}
	return s.run.ActiveSessions(agentID)
}

// meditationStamped reports the facts carrying the meditation lineage.
func meditationStamped(facts []event.Event) []event.Event {
	var out []event.Event
	for _, evt := range facts {
		if evt.Meta[event.MetaKeyTriggerSource] == meditation.LineageReflection {
			out = append(out, evt)
		}
	}
	return out
}

// lastMessage reports the last message of one recorded request, and the zero
// message beside none.
func lastMessage(req model.Request) model.Message {
	if len(req.Messages) == 0 {
		return model.Message{}
	}
	return req.Messages[len(req.Messages)-1]
}

// invokeUserTurn drives one invocation whose input facts carry the user stamp:
// the stamp the freshness policy reads as an interaction someone awaits.
func invokeUserTurn(t *testing.T, run *assembly.Run, id string, addr event.Address, input string) {
	t.Helper()
	if _, err := run.Invoke(context.Background(), assembly.InvokeRequest{
		InvocationID:  event.InvocationID(id),
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: input}},
	}); err != nil {
		t.Fatalf("invoke %s: %v", id, err)
	}
}
