package host

import (
	"context"
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
)

// TestMeditationDeliversNothingWithoutModelCall fires a reflection pass whose model never calls the communication tool and finds the target chain unchanged:
// - the fired pass reaches a terminal beside no cross-session call
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

// meditationRig is one assembled runtime whose reflection driver observes the
// user agent's partition and injects into the meditator's own loop session: the
// two agents and the channel the fired passes report on.
type meditationRig struct {
	run       *assembly.Run
	userModel *entryModel
	medModel  *entryModel
	userAddr  event.Address
	fired     chan meditation.TickOutcome
}

// newMeditationRig assembles the rig over a local storage tier and an anchor
// record, so the reflection reads the same chain the run commits.
func newMeditationRig(t *testing.T, interval time.Duration, medAnswer func(*model.Request) *model.Response) *meditationRig {
	t.Helper()
	root := t.TempDir()
	userModel := &entryModel{name: "model-user", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("user output")
	}}
	medModel := &entryModel{name: "model-meditator", answer: medAnswer}
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
		"meditator":  {Model: medModel},
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
			driver := meditation.NewDriver(reflection, interval, 0, nil, "")
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
	return &meditationRig{
		run:       run,
		userModel: userModel,
		medModel:  medModel,
		userAddr:  event.Address{AgentID: "user-agent", SessionID: "session-user"},
		fired:     fired,
	}
}

// awaitFire waits for one fired reflection pass to settle, which is the moment
// its turn reached a terminal.
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
