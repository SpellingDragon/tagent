package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestAnchorStateCaptureFailureCommitsNoProducts pins batch atomicity.
// - the fold has pending judgments and a registered owner refuses capture
// - execution returns the refusal without filing any product of the batch
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchorStateCaptureFailureCommitsNoProducts(t *testing.T) {
	ctx := context.Background()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	run := windowChainRun(t, ctx, storage)
	consumer := run.compress
	sessions := consumer.sessions
	consumer.sessions = nil
	run.ageFold.setExecutor(nil)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-capture-refusal"}
	commitWindowTurns(t, ctx, run, addr, 12)
	consumer.sessions = sessions
	run.RegisterAnchorState("refusing-owner", func(context.Context, event.SessionID) ([]byte, error) {
		return nil, errors.New("capture refused")
	}, nil)
	pendingBefore := len(consumer.pending(addr.SessionID))
	if pendingBefore == 0 {
		t.Fatal("the fixture filed no judgment to execute")
	}
	err := consumer.Execute(ctx, addr.SessionID)
	if err == nil || !strings.Contains(err.Error(), "capture refused") {
		t.Fatalf("the capture refusal returned %v, want the named error", err)
	}
	if count := countStateAnchorProducts(t, ctx, run); count != 0 {
		t.Fatalf("the refused fold filed %d product(s), want an empty batch", count)
	}
	if pendingAfter := len(consumer.pending(addr.SessionID)); pendingAfter != pendingBefore {
		t.Fatalf("the refused fold answered %d judgment(s), want none", pendingBefore-pendingAfter)
	}
	run.RegisterAnchorState("refusing-owner", nil, nil)
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the refused run: %v", err)
	}
	restarted := windowChainRun(t, ctx, storage)
	if count := countStateAnchorProducts(t, ctx, restarted); count != 0 {
		t.Fatalf("the restart found %d partial product(s), want none", count)
	}
	if _, err := restarted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the restarted run: %v", err)
	}
}

// TestAnchorStateRecoveryParity pins one state-selection rule for both walks.
// - a checkpoint-less newest product makes both paths restore the prior anchor
// - malformed state bytes make both paths fail by name instead of diverging
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchorStateRecoveryParity(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	checkpoint := anchorCheckpointOf([]replay.ProjectionEntry{{Key: "survivor", Type: event.TypeExternalInput, Content: "kept", Time: at}})
	const session = event.SessionID("session-state-parity")
	const owner = "state-owner"

	t.Run("checkpoint-less newest", func(t *testing.T) {
		old := event.CompressProductFor("request-old", compressionProducer, "-hist-old", nil, "old").WithCheckpoint(checkpoint).WithStates(stateAnchorPayload(t, owner, []byte("OLD")))
		newest := event.CompressProductFor("request-new", compressionProducer, "-hist-new", nil, "new")
		restore := func(values *[]byte) func(context.Context, event.SessionID, []byte) error {
			return func(_ context.Context, _ event.SessionID, data []byte) error {
				*values = append([]byte(nil), data...)
				return nil
			}
		}
		full := windowChainRun(t, ctx)
		storeStateAnchorProduct(t, ctx, full, old, session, 1, at)
		storeStateAnchorProduct(t, ctx, full, newest, session, 2, at)
		var fullState []byte
		full.RegisterAnchorState(owner, nil, restore(&fullState))
		if _, _, _, err := full.seedFullReplay(ctx, full.tier); err != nil {
			t.Fatalf("restore the full walk: %v", err)
		}
		anchored := windowChainRun(t, ctx)
		storeStateAnchorProduct(t, ctx, anchored, old, session, 1, at)
		storeStateAnchorProduct(t, ctx, anchored, newest, session, 2, at)
		var anchoredState []byte
		anchored.RegisterAnchorState(owner, nil, restore(&anchoredState))
		_, report, err := anchored.seedAnchoredReplay(ctx, anchored.tier)
		if err != nil {
			t.Fatalf("restore the anchored fallback: %v", err)
		}
		if report.Fallbacks != 1 {
			t.Fatalf("the checkpoint-less newest product caused %d fallback(s), want one", report.Fallbacks)
		}
		if string(fullState) != "OLD" || string(anchoredState) != string(fullState) {
			t.Fatalf("full restored %q while anchored restored %q, want the same prior state", fullState, anchoredState)
		}
	})

	t.Run("malformed states", func(t *testing.T) {
		product := event.CompressProductFor("request-bad", compressionProducer, "-hist-bad", nil, "bad").WithCheckpoint(checkpoint).WithStates([]byte("{bad"))
		full := windowChainRun(t, ctx)
		storeStateAnchorProduct(t, ctx, full, product, session, 1, at)
		_, _, _, fullErr := full.seedFullReplay(ctx, full.tier)
		anchored := windowChainRun(t, ctx)
		storeStateAnchorProduct(t, ctx, anchored, product, session, 1, at)
		_, _, anchoredErr := anchored.seedAnchoredReplay(ctx, anchored.tier)
		if fullErr == nil || anchoredErr == nil {
			t.Fatalf("malformed states returned full=%v anchored=%v, want both refused", fullErr, anchoredErr)
		}
		if !strings.Contains(fullErr.Error(), "anchor state") || !strings.Contains(anchoredErr.Error(), "anchor state") {
			t.Fatalf("the corruption was not named: full=%v anchored=%v", fullErr, anchoredErr)
		}
	})
}

// stateAnchorPayload states one owner blob in the durable checkpoint shape.
func stateAnchorPayload(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	payload, err := json.Marshal([]anchorStateSnapshot{{Name: name, Data: data}})
	if err != nil {
		t.Fatalf("state the test payload: %v", err)
	}
	return payload
}

// storeStateAnchorProduct files one synthetic product on a fresh test chain.
func storeStateAnchorProduct(t *testing.T, ctx context.Context, run *Run, product event.CompressProduct, session event.SessionID, seq int64, at time.Time) {
	t.Helper()
	evt := compressProductEvent(session, "agent-a", product, at)
	body, err := memory.BodyFromEvent(evt, "test", "test")
	if err != nil {
		t.Fatalf("encode the product: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal the product: %v", err)
	}
	record := ledger.SegmentRecord{PartitionID: 1, WindowTS: at.UnixMilli(), Seq: seq, EventKey: evt.Key, EventTime: at.UnixMilli(), Body: encoded}.WithChain(evt.Type, session)
	if err := run.tier.Segments.StoreEvent(ctx, record); err != nil {
		t.Fatalf("file the product: %v", err)
	}
}

// countStateAnchorProducts counts the product facts a refused batch left.
func countStateAnchorProducts(t *testing.T, ctx context.Context, run *Run) int {
	t.Helper()
	count := 0
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(_ event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if typ == event.CompressProductType {
			count++
		}
		return nil
	}); err != nil {
		t.Fatalf("count the filed products: %v", err)
	}
	return count
}
