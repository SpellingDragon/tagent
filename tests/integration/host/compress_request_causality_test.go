// The causality anchor of the compression request: a judgment whose fact the
// commit entry refuses is refused whole. The fold quotes every judgment it
// files through the one commit entry every fact of the run lands under, and a
// judgment that lands in no ledger may not reach the consumer's memory: no
// product may answer it, no synthetic entry may present it, and the
// observation that filed it must answer the refusal by name — while the
// business facts that triggered the fold stand exactly as they committed.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// causalityTurns is the task material the anchor session commits: enough turns
// that the fold judges over the 100-token window several times, before and
// after the identity lane is armed.
const causalityTurns = 4

// TestCompressionRequestCommitFailureKeepsFactChainConsistent is the causality anchor of compression's request half.
//   - positive: the identity a judgment is quoted by already names another fact, so the fold's filing meets the named refusal
//   - the observation answers that refusal as a visible gap
//   - negative: no product answers the refused judgment and no synthetic entry presents one
//   - consumer: the scene runs over the public commit entry of a formally assembled run, from armed identities to published pipeline
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionRequestCommitFailureKeepsFactChainConsistent(t *testing.T) {
	reproductionEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the causality anchor drives no decision of its own")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 100}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	window := assembly.FoldWindow{
		MaxTokens:         100,
		CompressThreshold: config.DefaultCompressThreshold,
		KeepRecentTasks:   config.DefaultKeepRecentTasks,
	}
	plan := replay.AgeFoldPlan{KeepRecent: window.KeepRecentTasks, VolumeThreshold: window.VolumeThresholdBytes()}
	armed := map[event.EventID]bool{}
	replica := replay.NewReplayProjection()
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for turn := 0; turn < causalityTurns; turn++ {
		for position, leg := range compressTurnLegs(turn) {
			replica.Append(ctx, replay.ProjectionEntry{
				Key:     event.EventID(fmt.Sprintf("%02d-%s", turn, leg.position)),
				Type:    leg.typ,
				Summary: leg.summary,
				Content: compressBody(turn, leg.position),
				Time:    base.Add(time.Duration(turn*10+position) * time.Minute),
			})
			report, err := replica.PlanByAge(ctx, plan)
			if err != nil {
				t.Fatalf("plan the prefix after turn %d leg %s: %v", turn, leg.position, err)
			}
			for _, product := range report.Products {
				armed[product.Request.ID] = true
			}
		}
	}
	if len(armed) == 0 {
		t.Fatalf("the planned material minted no judgment identity to refuse, want at least one over %d turn(s)", causalityTurns)
	}
	identities := make([]event.EventID, 0, len(armed))
	for id := range armed {
		identities = append(identities, id)
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i] < identities[j] })

	lane := event.Address{AgentID: "agent-a", SessionID: "session-causality-lane"}
	for at, id := range identities {
		if _, err := run.CommitToSession(ctx, lane, event.Event{
			Key:       id,
			Type:      event.TypeExternalInput,
			Timestamp: base.Add(time.Duration(at) * time.Second),
			Message:   model.Message{Role: model.RoleUser, Content: "the identity lane holds this fact"},
		}); err != nil {
			t.Fatalf("arm the identity %s through the public commit entry: %v", id, err)
		}
	}

	addr := event.Address{AgentID: "agent-a", SessionID: "session-causality"}
	for turn := 0; turn < causalityTurns; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}

	refused := 0
	for _, presented := range run.Presentations() {
		for _, gap := range presented.Gaps {
			if gap.Plugin != assembly.AgeFoldPluginName {
				continue
			}
			if !errors.Is(gap.Err, memory.ErrFactKeyConflict) {
				continue
			}
			held, err := run.CommittedEvent(ctx, gap.Fact)
			if err == nil && held.Owner.SessionID == addr.SessionID {
				refused++
			}
		}
	}
	if refused == 0 {
		t.Fatalf("the refused judgment commit reached no observation gap: the fold answered a commit the entry refused as a filed judgment of session %s", addr.SessionID)
	}

	for _, evt := range compressFactsOf(run, addr.SessionID, event.CompressRequestType) {
		if armed[evt.Key] {
			t.Fatalf("the judgment %s the commit entry refused stands on the chain of %s", evt.Key, addr.SessionID)
		}
	}

	committed := compressRequestIDsOf(run, addr.SessionID)
	products, _ := compressProductsOf(t, run, addr.SessionID)
	for _, product := range products {
		if armed[product.Request] {
			t.Fatalf("the product %s answers the judgment %s the chain never committed: the consumer executed memory no fact backs", product.Key, product.Request)
		}
		if !committed[product.Request] {
			t.Fatalf("the product %s answers the judgment %s no committed request of the chain names", product.Key, product.Request)
		}
	}

	if synthetic := compressSyntheticKeysOf(t, run, addr.SessionID); len(synthetic) != 0 {
		t.Fatalf("the projection of %s carries %d synthetic entr(ies) %v beside refused judgments, want none: the consumer folded material no fact judged", addr.SessionID, len(synthetic), synthetic)
	}

	for turn := 0; turn < causalityTurns; turn++ {
		for _, leg := range compressTurnLegs(turn) {
			key := event.EventID(fmt.Sprintf("%02d-%s", turn, leg.position))
			held, err := run.CommittedEvent(ctx, key)
			if err != nil {
				t.Fatalf("the fact %s of the refused fold reads back: %v", key, err)
			}
			if held.Type != leg.typ || held.Message.Content != compressBody(turn, leg.position) {
				t.Fatalf("the fact %s of the refused fold reads back as type %s content %q, want the %s leg exactly as committed", key, held.Type, held.Message.Content, leg.typ)
			}
		}
	}

	for _, id := range identities {
		held, err := run.CommittedEvent(ctx, id)
		if err != nil || held.Type != event.TypeExternalInput {
			t.Fatalf("the armed identity %s no longer names the lane fact the run committed: err=%v type=%s", id, err, held.Type)
		}
	}
	t.Logf("[compress-causality] chain: %d armed judgment identit(ies), %d refused filing(s) answered as gaps, %d product(s), 0 synthetic entr(ies); the %d business legs stand as committed",
		len(identities), refused, len(products), causalityTurns*len(compressTurnLegs(0)))
}
