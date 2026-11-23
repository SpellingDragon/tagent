package history

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestProjectionReplayAndBudget proves the projection, its presentation decisions and the budget assembly on the frozen 03.4 face.
//
//   - 大结果仅票据不复发全文: a large result is presented as a ticket only
//   - 投影进入实际请求: the replayed projection is assembled into the actual request
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionReplayAndBudget(t *testing.T) {
	ctx := context.Background()
	session := replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("s1")}
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	at := func(step int) time.Time { return base.Add(time.Duration(step) * time.Minute) }

	factOf := func(key, typ, summary, content string, when time.Time) replay.ChainFact {
		return replay.ChainFact{
			Key:     event.EventID(key),
			Chain:   session,
			Type:    typ,
			Summary: summary,
			Content: content,
			Time:    when,
		}
	}
	entryOf := func(fact replay.ChainFact) replay.ProjectionEntry {
		return replay.ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time}
	}

	t.Run("G2.3 大结果仅票据不复发全文: a large result is presented as a ticket, never re-carrying the full text", func(t *testing.T) {
		full := strings.Repeat("大结果正文。", 300)
		large := replay.ChainFact{Key: "big-1", Chain: session, Type: event.TypeAgentOutput, Summary: "big result", Content: full, Time: at(1)}
		proj := replay.NewReplayProjection()
		proj.Append(ctx, entryOf(large))

		first, err := proj.AssembleBudget(ctx, 200)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		if len(first) != 1 {
			t.Fatalf("assembly returned %d entries, want 1", len(first))
		}
		view := first[0].Content
		if !strings.Contains(view, "[evt_big-1|") {
			t.Fatalf("ticket view = %q, want a ticket reaching the original by segment", view)
		}
		if strings.Contains(view, full) || len(view) > 512 {
			t.Fatalf("ticket view re-carry the full text (%d bytes), want a bounded preview only", len(view))
		}
		if original, kept := proj.Original("big-1"); !kept || original != full {
			t.Fatalf("original layer = %d bytes kept=%v, want the full text kept apart from the view", len(original), kept)
		}

		second, err := proj.AssembleBudget(ctx, 200)
		if err != nil {
			t.Fatalf("second AssembleBudget: %v", err)
		}
		if second[0].Content != view {
			t.Fatalf("second assembly view = %q, want the same budget to recompute the same view", second[0].Content)
		}
		if strings.Contains(second[0].Content, full) {
			t.Fatal("second assembly re-carried the full text although the budget only fits the ticket")
		}
	})

	t.Run("G2.6 投影进入实际请求: the replayed projection is assembled into the actual request", func(t *testing.T) {
		proj := replay.NewReplayProjection()
		for _, fact := range []replay.ChainFact{
			factOf("q-0001", event.TypeExternalInput, "in", "进入请求的输入原文。", at(1)),
			factOf("q-0002", event.TypeThinkingPlan, "plan", "进入请求的思考正文。", at(2)),
			factOf("q-0003", event.TypeActionCommand, "call", "进入请求的工具调用。", at(3)),
		} {
			proj.Append(ctx, entryOf(fact))
		}
		presented, err := proj.AssembleBudget(ctx, 100)
		if err != nil {
			t.Fatalf("AssembleBudget: %v", err)
		}
		provider := &replayModel{name: "model-a"}
		run, err := assembly.Assemble(ctx, &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider},
		}, assembly.WithGovernance(replayPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		if err := run.AttachReplay(agent.RequestChain{PartitionID: session.PartitionID, SessionID: session.SessionID}, assembly.FoldWindow{MaxTokens: 100}, proj); err != nil {
			t.Fatalf("AttachReplay: %v", err)
		}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-replay-1",
			Address:      event.Address{AgentID: "agent-a", SessionID: session.SessionID},
			Input:        []model.Message{{Role: model.RoleUser, Content: "请求带投影。"}},
		}); err != nil {
			t.Fatalf("invoke the formal request: %v", err)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
		}
		got := requests[0].Messages
		if len(got) != len(presented)+1 {
			t.Fatalf("request carries %d messages, want %d presented entries in front of the transcript", len(got), len(presented))
		}
		sawInput, sawPlan, sawTool := false, false, false
		for i, message := range got[:len(presented)] {
			want := presented[i].Content
			if strings.HasPrefix(string(presented[i].Key), "q-") {
				want = presentedTicket(presented[i].Key, presented[i].Type) + " " + want
			}
			if message.Content != want {
				t.Fatalf("message %d content = %q, want the presented view %q behind the ticket of the fact it was minted from", i, message.Content, want)
			}
			switch presented[i].Type {
			case event.TypeExternalInput:
				sawInput = message.Role == "user"
			case event.TypeThinkingPlan:
				sawPlan = message.Role == "assistant"
			case event.TypeActionCommand:
				sawTool = message.Role == "tool"
			}
		}
		if !sawInput || !sawPlan || !sawTool {
			t.Fatalf("role mapping = input:%v plan:%v tool:%v, want the frozen types mapped onto user/assistant/tool", sawInput, sawPlan, sawTool)
		}
		if err := run.AttachReplay(agent.RequestChain{PartitionID: session.PartitionID}, assembly.FoldWindow{MaxTokens: 100}, proj); !errors.Is(err, agent.ErrRequestChainSessionRequired) {
			t.Fatalf("sessionless registration error = %v, want ErrRequestChainSessionRequired", err)
		}
	})
}

// replayPolicy is the deterministic stand-in governance of this assembly: the
// three required checks each carry a passing disposition, which is a mechanism
// input with no quality conclusion.
type replayPolicy struct{}

// Check implements state.Governance beside the fixed passing dispositions.
func (replayPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}

// replayModel is the scripted provider stand-in: it records every native
// request the session's agent is driven with and answers with a fixed final. It
// never reaches a real endpoint. A band summary request is answered with the
// anchored reads it was handed — the gist a faithful summarizer returns — so the
// summary a fold files stands for the same material the mechanical form stood
// for, and a check reads the anchors and the bound rather than one summarizer's
// wording. A band summary is the fold asking that same model for a gist and not
// a turn the agent ran, so it is answered without being recorded: recorded
// answers the formal requests alone.
type replayModel struct {
	name     string
	requests []model.Request
}

// GenerateContent records the native request and streams the fixed final.
func (m *replayModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	answer := "settled"
	if material, summary := replayBandSummary(*req); summary {
		answer = material
	} else {
		m.requests = append(m.requests, *req)
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: answer}}},
		Done:    true,
	}
	close(ch)
	return ch, nil
}

// bandReadAnchor matches one rendered provenance anchor, the form every read a
// fold product carries opens with: [MM-DD HH:MM|gen:<generation>].
var bandReadAnchor = regexp.MustCompile(`\[\d\d-\d\d \d\d:\d\d\|gen:[^\]]*\]`)

// replayBandSummary reports the mechanical reads a band summary request carries,
// and whether the request is one at all: the fold asks the session's own model
// for the gist of its oldest band, so that ask reaches the same provider a
// decision does, carrying the band's anchored reads behind the instruction
// paragraph and no tool surface. A decision of an agent that declares no tools
// carries no tool surface either, so the rendered anchors of those reads are
// what tells the two apart.
func replayBandSummary(req model.Request) (string, bool) {
	if len(req.Tools) != 0 || len(req.Messages) != 1 || req.Messages[0].Role != model.RoleUser {
		return "", false
	}
	_, material, asked := strings.Cut(req.Messages[0].Content, "\n\n")
	if !asked || !bandReadAnchor.MatchString(material) {
		return "", false
	}
	return strings.TrimSpace(material), true
}

// Info reports the scripted provider's descriptor.
func (m *replayModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the formal native requests the fake provider was driven with,
// in the order the session's agent ran them: the band summaries the fold asked
// the same provider for are not among them.
func (m *replayModel) recorded() []model.Request {
	return append([]model.Request(nil), m.requests...)
}
