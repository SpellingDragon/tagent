// Package learning exercises the training records the assembled runtime
// produces over its formal entry: the attempt line of every real model
// decision, beside the explicit account of the writes a record pipeline
// refused.
package learning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// formalPolicy is the deterministic governance stand-in the formal assemblies
// of these checks are configured with: the fixed passing dispositions are
// mechanism inputs carrying no quality conclusion.
type formalPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (formalPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}

// failingRecorder is the record pipeline that refuses every write: it stands
// in for a pipeline outage at the production points, so the explicit
// recording-loss account of the run can be read back.
type failingRecorder struct{}

// RecordEpisode refuses the episode write by name.
func (failingRecorder) RecordEpisode(context.Context, rl.EpisodeRecord) error {
	return errors.New("the record pipeline refuses the episode write")
}

// RecordDecision refuses the decision write by name.
func (failingRecorder) RecordDecision(context.Context, rl.DecisionRecord) error {
	return errors.New("the record pipeline refuses the decision write")
}

// RecordAttempt refuses the attempt write by name.
func (failingRecorder) RecordAttempt(context.Context, rl.AttemptRecord) error {
	return errors.New("the record pipeline refuses the attempt write")
}

// formalRun assembles one runtime through the sole assembly entry beside the
// record pipeline it names: a scripted native model over one native echo tool,
// driven over a declaration naming the single agent.
func formalRun(t *testing.T, ctx context.Context, records rl.Recorder, answer func(req *model.Request) *model.Response) *tagent.Run {
	t.Helper()
	provider := &learnModel{name: "model-a", answer: answer}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
	}, tagent.WithGovernance(formalPolicy{}), tagent.WithRecorder(records))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// formalInvoke drives one invocation through the formal runtime entry beside
// the episode boundary it names.
func formalInvoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, episode event.EpisodeID) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: "learn me"}},
	})
	if err != nil {
		t.Fatalf("invoke the formal runtime: %v", err)
	}
	return turn
}

// TestFormalInvokeProducesTrainingRecords drives one formal Invoke and reads the training records its production points write.
//   - the formal invoke admits one attempt record per model decision, so every decision's outcome is replayable
//   - the intermediate decision's outcome survives in its own attempt response, which no committed output restates
//   - the episode identity a drive was handed rides the attempt and is never written as a record of its own
//   - an invocation whose input names no episode admits no episode onto its attempt and never guesses one
//   - a record write the pipeline refused is counted in the explicit recording-loss account of the run
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func TestFormalInvokeProducesTrainingRecords(t *testing.T) {
	t.Run("the formal invoke produces one replayable attempt per decision", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		records := &collector{}
		run := formalRun(t, ctx, records, func(req *model.Request) *model.Response {
			if last := req.Messages[len(req.Messages)-1]; last.Content == "learn me" {
				return learnToolTurn(learnCall("call-1", "echo-a", `{"x":1}`))
			}
			return learnFinalTurn("settled")
		})
		turn := formalInvoke(t, ctx, run, "inv-formal-1", "session-formal-1", "ep-formal-1")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		snap := records.snapshot()
		if len(snap.episodes) != 0 || len(snap.decisions) != 0 {
			t.Fatalf("records = (%d episodes, %d decisions), want the attempt line alone", len(snap.episodes), len(snap.decisions))
		}
		if len(snap.attempts) != 2 {
			t.Fatalf("attempt records = %d, want one per observable model attempt", len(snap.attempts))
		}
		for i, attempt := range snap.attempts {
			if attempt.Schema != rl.SchemaCurrent {
				t.Fatalf("attempt %d schema = %q, want %q", i, attempt.Schema, rl.SchemaCurrent)
			}
			if attempt.ID == "" || attempt.DecisionID != turn.Decisions[i].DecisionID {
				t.Fatalf("attempt %d identity = (%q, %q), want one attempt bound to decision %q", i, attempt.ID, attempt.DecisionID, turn.Decisions[i].DecisionID)
			}
			if attempt.EpisodeID != "ep-formal-1" {
				t.Fatalf("attempt %d episode = %q, want the episode boundary the invocation named", i, attempt.EpisodeID)
			}
			if attempt.Terminal != turn.Decisions[i].Terminal || attempt.Terminal != model.KindCompleted {
				t.Fatalf("attempt %d terminal = %v, want the shared terminal %v", i, attempt.Terminal, turn.Decisions[i].Terminal)
			}
			if attempt.Timestamp.IsZero() {
				t.Fatalf("attempt %d timestamp = %v, want the time the record was written", i, attempt.Timestamp)
			}
		}
		first := snap.attempts[0].Response
		if len(first.Choices) != 1 || len(first.Choices[0].Message.ToolCalls) != 1 ||
			first.Choices[0].Message.ToolCalls[0].ID != "call-1" ||
			first.Choices[0].Message.ToolCalls[0].Function.Name != "echo-a" {
			t.Fatalf("first attempt response = %+v, want the intermediate tool call kept byte-identical", first)
		}
		second := snap.attempts[1].Response
		if len(second.Choices) != 1 || second.Choices[0].Message.Content != "settled" {
			t.Fatalf("second attempt response = %+v, want the native final content kept as it stood", second)
		}
		if second := turn.Decisions[1].Request.Messages; !carriesToolOutput(second, "out-a") {
			t.Fatalf("second decision request = %+v, want the settled tool observation of the first round", second)
		}
	})

	t.Run("an invocation that names no episode admits no episode onto its attempt", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		records := &collector{}
		run := formalRun(t, ctx, records, func(*model.Request) *model.Response {
			return learnFinalTurn("settled")
		})
		turn := formalInvoke(t, ctx, run, "inv-formal-2", "session-formal-2", "")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 1 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside one decision", turn.Terminal, len(turn.Decisions))
		}
		snap := records.snapshot()
		if len(snap.episodes) != 0 || len(snap.decisions) != 0 {
			t.Fatalf("records = (%d episodes, %d decisions), want the attempt line alone", len(snap.episodes), len(snap.decisions))
		}
		if len(snap.attempts) != 1 {
			t.Fatalf("attempt records = %d, want the one attempt the drive admitted", len(snap.attempts))
		}
		if snap.attempts[0].EpisodeID != "" {
			t.Fatalf("attempt episode = %q, want empty beside an input that names no episode", snap.attempts[0].EpisodeID)
		}
	})

	t.Run("a refused record write is counted as an explicit recording loss", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run := formalRun(t, ctx, failingRecorder{}, func(req *model.Request) *model.Response {
			if last := req.Messages[len(req.Messages)-1]; last.Content == "learn me" {
				return learnToolTurn(learnCall("call-1", "echo-a", `{"x":1}`))
			}
			return learnFinalTurn("settled")
		})
		turn := formalInvoke(t, ctx, run, "inv-formal-3", "session-formal-3", "ep-formal-3")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn unaffected by the refused record writes", turn.Terminal, len(turn.Decisions))
		}
		counts := map[string]int{}
		for _, loss := range run.RecordingLosses() {
			if loss.ID == "" || loss.Err == nil {
				t.Fatalf("recording loss = %+v, want the record identity beside its named failure", loss)
			}
			counts[loss.Kind]++
		}
		if counts["capture-attempt"] != 2 || len(counts) != 1 {
			t.Fatalf("recording loss counts = %v, want the two attempt refusals and nothing else", counts)
		}
	})
}
