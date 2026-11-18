// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// factStore is one attention store in memory: the pairs written beside the reads
// it answered and the Put it refuses on demand.
type factStore struct {
	mu     sync.Mutex
	stored map[string][]byte
	scans  int
	refuse int
}

// newFactStore returns an empty attention store.
func newFactStore() *factStore {
	return &factStore{stored: map[string][]byte{}}
}

// Put stores one fact, or refuses the write while its counter still holds one.
func (s *factStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse > 0 {
		s.refuse--
		return fmt.Errorf("the store refused the attention write")
	}
	s.stored[key] = append([]byte(nil), value...)
	return nil
}

// Scan answers every stored pair under the prefix.
func (s *factStore) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scans++
	found := map[string][]byte{}
	for key, value := range s.stored {
		if strings.HasPrefix(key, prefix) {
			found[key] = append([]byte(nil), value...)
		}
	}
	return found, nil
}

// Sync completes the write barrier of one staged write.
func (s *factStore) Sync(context.Context) error { return nil }

// TestAttentionChainOutlivesTheProcess pins the chain as the durable record.
//   - positive: a question and the answer it was ruled with are stored, and a book opened beside the same store answers the same presence and the same ruling
//   - negative: an unwritable store reports the refusal to the caller that filed the fact instead of reporting it as durable
//   - consumer: BindAttention is the seam the composition hands the durable face through
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionChainOutlivesTheProcess(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	args := []byte(`{"cmd":"deploy"}`)
	first := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := first.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	held, err := first.Request(ctx, "durable-hold", "shell_exec", args,
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	if _, err := first.Decide("durable-hold", ApprovalDenied, testApprovalApprover); err != nil {
		t.Fatalf("rule the hold denied: %v", err)
	}
	store.mu.Lock()
	stored := 0
	for key := range store.stored {
		if strings.HasPrefix(key, attentionFactPrefix) {
			stored++
		}
	}
	store.mu.Unlock()
	if stored != 2 {
		t.Fatalf("stored attention facts = %d, want the question and the answer the registration and the ruling filed", stored)
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if got := restarted.Pending(); len(got) != 0 {
		t.Fatalf("the restarted waiting face = %+v, want the ruled question read back as answered", got)
	}
	question := questionKeyOf("shell_exec", ArgsDigest(args), held.ID)
	if !holdsFact(t, restarted, question) {
		t.Fatalf("the restarted chain holds no question %s for the hold the first process filed", question)
	}

	refusing := newFactStore()
	refusing.refuse = 1
	blocked := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := blocked.BindAttention(ctx, refusing); err != nil {
		t.Fatalf("bind the refusing durable face: %v", err)
	}
	_, err = blocked.Request(ctx, "refused-hold", "shell_exec", []byte(`{"cmd":"scale"}`),
		plugin.RiskCritical, "no-scale", "critical risk awaits approval")
	if err == nil {
		t.Fatal("a registration whose attention fact the store refused answered nil, want the refusal named to the caller")
	}
	if !strings.Contains(err.Error(), "refused the attention write") {
		t.Fatalf("registration error = %v, want the store's own refusal named", err)
	}
}

// TestAttentionChainRefusesAStoredBodyOfAnotherFact pins the fold against a mislabelled key.
//   - positive: the stored question is folded when its body states the fact its key names
//   - negative: a body naming another fact is refused by name instead of landing as that key
//   - consumer: foldAttentionFacts is the read BindAttention runs
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionChainRefusesAStoredBodyOfAnotherFact(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	manager := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := manager.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if _, err := manager.Request(ctx, "tampering-hold", "shell_exec", []byte(`{"cmd":"deploy"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval"); err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	store.mu.Lock()
	var key string
	for held := range store.stored {
		if strings.HasPrefix(held, attentionFactPrefix) {
			key = held
		}
	}
	raw, err := json.Marshal(event.Event{Key: "tagent:attention:other", Type: event.TypeAttentionQuestion})
	if key == "" {
		store.mu.Unlock()
		t.Fatal("the store holds no attention fact, so the tamper has nothing to overwrite")
	}
	store.stored[key] = raw
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("render the tampered body: %v", err)
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	err = restarted.BindAttention(ctx, store)
	if err == nil {
		t.Fatal("a stored body naming another fact was folded, want the refusal by name")
	}
	if !strings.Contains(err.Error(), "the attention fact key stands for other content") {
		t.Fatalf("fold error = %v, want the conflict named", err)
	}
}

// TestRequestRecordKeepsTheIdentityItsKeyNames pins the request-record fold.
//   - positive: the identity a registration answered with is seated from the store alone, beside no stated cut
//   - negative: a record storing another identity than its key names is refused by name instead of seated under that key
//   - consumer: BindAttention folds the records through the same durable face the facts use
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestRequestRecordKeepsTheIdentityItsKeyNames(t *testing.T) {
	ctx := context.Background()
	store := newFactStore()
	first := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := first.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	held, err := first.Request(ctx, "seated-hold", "shell_exec", []byte(`{"cmd":"deploy"}`),
		plugin.RiskCritical, "no-deploy", "critical risk awaits approval")
	if err != nil {
		t.Fatalf("register the hold: %v", err)
	}
	store.mu.Lock()
	record, stored := store.stored[approvalRequestKey("seated-hold")]
	store.mu.Unlock()
	if !stored {
		t.Fatalf("the store holds no request record under %s, so the identity has no durable record", approvalRequestKey("seated-hold"))
	}

	restarted := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	if err := restarted.BindAttention(ctx, store); err != nil {
		t.Fatalf("bind the durable face: %v", err)
	}
	if _, err := restarted.Decide(held.ID, ApprovalApproved, testApprovalApprover); err != nil {
		t.Fatalf("decide the identity the first book named: %v — the record %s seated no identity", err, approvalRequestKey("seated-hold"))
	}

	var tampered approvalRequestRecord
	if err := json.Unmarshal(record, &tampered); err != nil {
		t.Fatalf("read the stored request record: %v", err)
	}
	tampered.Asked = "another-hold"
	raw, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("render the tampered record: %v", err)
	}
	store.mu.Lock()
	store.stored[approvalRequestKey("seated-hold")] = raw
	store.mu.Unlock()

	blocked := NewApprovalManager("", time.Hour, WithApprovalRescanInterval(0))
	err = blocked.BindAttention(ctx, store)
	if err == nil {
		t.Fatal("a record storing another identity than its key names was seated, want the refusal by name")
	}
	if !strings.Contains(err.Error(), approvalRequestKey("seated-hold")) {
		t.Fatalf("fold error = %v, want the key %s named", err, approvalRequestKey("seated-hold"))
	}
}
