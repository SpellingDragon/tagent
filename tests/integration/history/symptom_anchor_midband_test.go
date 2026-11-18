package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/tool/recall"
)

// symptomAnchorSession is the session the mid-band recall anchor commits its
// turns on.
const symptomAnchorSession = event.SessionID("session-midband-recall")

// symptomAnchorWindowTokens is the replay window the anchor declares: wide
// enough that the folds run over the turns that have already left the recent
// window and stop before the oldest band is summarised, so the sequence the
// request presents is the middle band the fold rewrote and not the history
// summary that would have consumed it.
const symptomAnchorWindowTokens = 1600

// chainProcessDetail reports the process detail one session chain committed: the
// reasoning and the tool commands of its turns, in commit order. The fold
// consumes exactly this material, so the keys the presented projection does not
// hold are the material it took.
func chainProcessDetail(t *testing.T, ctx context.Context, run *assembly.Run, session event.SessionID) []event.EventID {
	t.Helper()
	keys := make([]event.EventID, 0, 8)
	if err := run.EachSessionFact(ctx, event.Address{SessionID: session}, func(evt event.Event) error {
		if evt.Type != event.TypeThinkingPlan && evt.Type != event.TypeActionCommand {
			return nil
		}
		if evt.Message.Content == "" {
			return nil
		}
		keys = append(keys, evt.Key)
		return nil
	}); err != nil {
		t.Fatalf("stream the process detail of the session chain: %v", err)
	}
	return keys
}

// foldedProcessKeys reports the process-detail keys one fold consumed: the chain
// keys the presented projection does not carry. A request that carries no ticket
// is then a claim about material the fold really took.
func foldedProcessKeys(committed []event.EventID, presented []replay.ProjectionEntry) []event.EventID {
	held := make(map[event.EventID]struct{}, len(presented))
	for _, entry := range presented {
		held[entry.Key] = struct{}{}
	}
	folded := make([]event.EventID, 0, len(committed))
	for _, key := range committed {
		if _, kept := held[key]; kept {
			continue
		}
		folded = append(folded, key)
	}
	return folded
}

// segmentFoldProducts reports the segment-fold products one presented sequence
// carries, beside whether the sequence was already summarised into the oldest
// band: the middle band is the state a fold product still stands in.
func segmentFoldProducts(entries []replay.ProjectionEntry) (int, bool) {
	products := 0
	summarised := false
	for _, entry := range entries {
		switch entry.Type {
		case replay.ProjectionTypeSegmentFold:
			products++
		case replay.ProjectionTypeHistorySummary:
			summarised = true
		}
	}
	return products, summarised
}

// recallGetAnswer is the answer one exact read states: the event it read,
// whether text came back, and the segment of the original it served.
type recallGetAnswer struct {
	Key    string `json:"key"`
	Found  bool   `json:"found"`
	Reason string `json:"reason"`
	Body   string `json:"body"`
}

// recallGet calls the exact read tool of the recall surface with one key
// argument, exactly as a model's call reaches it: the ticket it hands back is
// the argument the tool parses, so the anchor reads the same path a turn takes.
func recallGet(t *testing.T, ctx context.Context, reads recall.Reads, partitions []string, key string) recallGetAnswer {
	t.Helper()
	tools := recall.New(reads, partitions)
	for _, tool := range tools {
		if tool.Declaration().Name != recall.GetToolName {
			continue
		}
		answer, err := tool.Call(ctx, []byte(fmt.Sprintf(`{"key":%q}`, key)))
		if err != nil {
			t.Fatalf("%s(%q): %v", recall.GetToolName, key, err)
		}
		encoded, held := answer.(string)
		if !held {
			t.Fatalf("%s(%q) answered %T, want the encoded answer", recall.GetToolName, key, answer)
		}
		var decoded recallGetAnswer
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			t.Fatalf("%s(%q) answered %q, want the encoded answer: %v", recall.GetToolName, key, encoded, err)
		}
		return decoded
	}
	t.Fatalf("the recall surface wires no %s", recall.GetToolName)
	return recallGetAnswer{}
}

// ticketOf renders the ticket one event reaches the model as: the event key
// behind the recall prefix, beside the frozen type of the event.
func ticketOf(key event.EventID, evtType string) string {
	return "[evt_" + string(key) + "|" + evtType + "]"
}

// TestSymptomAnchorMidbandProcessRecalledByTicket anchors the middle band: a folded entry reaches the model as an event-key ticket the exact read answers.
//   - fixture: one session over the declared replay window, four closed turns; the commits drive the folds that consume the process detail which left the window
//   - band presented: the sequence holds a segment-fold product and no history summary, so it is the middle band the fold rewrote
//   - denominator: the chain's process-detail keys the presented projection does not hold are the material the fold took, named key by key
//   - premise: recall_get answers the bare key with the committed text, so a recall that answers no text is a visibility failure, never a lost key
//   - material: the request carries the folded read and not the raw text it dropped, so a missing ticket is about the handle, not a lost fold
//   - negative: the fold line carries the provenance anchor and the one-line read alone and no ticket reaches the request, so the model cannot ask past the summary
//   - failure report: the bound names the key, the ticket it wanted, the folded set and the messages the request carried
//   - consumer: the request is the one Run.Invoke drives, and the read is recall_get over Run.RecallReads called with the ticket the request carried
//   - comparison: the recall body is compared to the committed text character for character
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSymptomAnchorMidbandProcessRecalledByTicket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	provider := &replayModel{name: "model-a"}
	run, addr := newProjectionRun(t, symptomAnchorWindowTokens, provider)
	for turn := 0; turn < 4; turn++ {
		commitProjectionTurn(t, ctx, run, addr, turn)
	}

	presented := projectionEntriesOf(t, run, addr.SessionID)
	products, summarised := segmentFoldProducts(presented)
	folded := foldedProcessKeys(chainProcessDetail(t, ctx, run, addr.SessionID), presented)
	t.Logf("the presented sequence holds %d entr(ies), %d segment-fold product(s), history summary=%t, and the fold took %v",
		len(presented), products, summarised, folded)
	if summarised {
		t.Fatalf("the presented sequence carries the history summary of the oldest band, want the middle band the fold rewrote: the fixture folded past the state this anchor measures")
	}
	if products == 0 || len(folded) == 0 {
		t.Fatalf("the presented sequence holds %d segment-fold product(s) and the fold took %d process entr(ies), want the process detail of the turns that left the recent window folded into products: the fixture proves nothing without it",
			products, len(folded))
	}

	target := folded[0]
	committed, err := run.CommittedEvent(ctx, target)
	if err != nil {
		t.Fatalf("read the folded fact %s the fold consumed: %v", target, err)
	}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-midband",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "中段过程折叠后的新请求。"}},
	}); err != nil {
		t.Fatalf("invoke the formal request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	messages := requests[0].Messages
	carried := strings.Join(contentsOf(messages), "\n")

	if !strings.Contains(carried, committed.Meta[event.MetaKeyEventSummary]) {
		t.Fatalf("the request carries none of the folded read %q of %s, want the fold product presented: the fixture proves nothing without it (messages=%q)",
			committed.Meta[event.MetaKeyEventSummary], target, carried)
	}
	if strings.Contains(carried, committed.Message.Content) {
		t.Fatalf("the request carries the raw text of the folded fact %s, want the folded process detail kept out of the request", target)
	}

	reads := run.RecallReads()
	partitions := []string{string(addr.AgentID)}
	directKey := "evt_" + string(target)
	direct := recallGet(t, ctx, reads, partitions, directKey)
	if !direct.Found || direct.Body != committed.Message.Content {
		t.Fatalf("recall_get(%q) = %+v, want the committed text of the folded fact served by segment: the read path holds the fact today, so the anchor below judges visibility alone",
			directKey, direct)
	}
	t.Logf("the premise holds: recall_get(%q) answered %d byte(s) of the committed text", directKey, len(direct.Body))

	for _, key := range folded {
		evt, err := run.CommittedEvent(ctx, key)
		if err != nil {
			t.Fatalf("read the folded fact %s: %v", key, err)
		}
		ticket := ticketOf(key, evt.Type)
		if !strings.Contains(carried, ticket) {
			t.Fatalf("the request carries no ticket %q for the folded entry %s (%s: %q): the model is handed the fold product's summary and no handle on the process text, so it cannot recall what the fold consumed (folded=%v, messages=%q)",
				ticket, key, evt.Type, evt.Meta[event.MetaKeyEventSummary], folded, carried)
		}
	}

	recalled := recallGet(t, ctx, reads, partitions, ticketOf(target, committed.Type))
	if !recalled.Found {
		t.Fatalf("recall_get answered the ticket %q with %+v, want the folded text", ticketOf(target, committed.Type), recalled)
	}
	if recalled.Body != committed.Message.Content {
		t.Fatalf("recall_get(%q) answered %q, want the committed text %q of the folded entry",
			ticketOf(target, committed.Type), recalled.Body, committed.Message.Content)
	}
}
