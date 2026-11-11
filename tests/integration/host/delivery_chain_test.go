// The delivery-chain checks: what the delivery account looks like on the fact
// chain — the owed declaration an agent_output fact carries and the settlement
// facts the host appends beside it. The read-only KV records stay on the tier as
// the audit of the migration; the assertions of this package read the live chain
// instead.
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package host

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// deliverySettlementType and the metadata keys below are the frozen wire
// spellings the assembly package commits (assembly/host_delivery.go); the check
// reads them as frozen and never re-derives them from the assembly.
const (
	deliverySettlementType   = "delivery.settlement"
	deliveryMetaIdentity     = event.MetaPrefix + "delivery_identity"
	deliveryMetaState        = event.MetaPrefix + "delivery_state"
	deliveryMetaBody         = event.MetaPrefix + "delivery_body"
	deliveryMetaTargetAgent  = event.MetaPrefix + "delivery_target_agent"
	deliveryMetaTargetSess   = event.MetaPrefix + "delivery_target_session"
	deliveryMetaOwed         = event.MetaPrefix + "delivery_owed"
	deliveryDeliveryFresh    = "delivery-fresh-"
	deliveryStateSent        = "sent"
	deliveryStateOwed        = "owed"
	deliveryStateRetiredHead = "headless-retired"
)

// deliveryChainFact is one fact of the delivery account as the durable chain
// carries it: a settlement fact states its identity and state, and an agent
// output states the owed declaration beside the body it owes.
type deliveryChainFact struct {
	Key       event.EventID
	Type      string
	Identity  string
	State     string
	Content   string
	Target    event.Address
	Declared  bool
	Timestamp int64
}

// deliveryFacts reads every delivery fact one storage root holds on its event
// chain, in commit order.
func deliveryFacts(t *testing.T, root string) []deliveryChainFact {
	t.Helper()
	tier, err := ledger.OpenTier(context.Background(), ledger.StorageConfig{
		Type: ledger.StorageLocalFile,
		Root: root,
	})
	if err != nil {
		t.Fatalf("open the storage tier of %s: %v", root, err)
	}
	stored, err := tier.Segments.Events(context.Background())
	if err != nil {
		t.Fatalf("read the event chain of %s: %v", root, err)
	}
	facts := make([]deliveryChainFact, 0, len(stored))
	for _, one := range stored {
		body, err := memory.DecodeEventBody(one.Body)
		if err != nil {
			t.Fatalf("decode the committed event %s of %s: %v", one.Key, root, err)
		}
		evt := body.Event()
		evt.Key = one.Key
		switch evt.Type {
		case deliverySettlementType:
			facts = append(facts, deliveryChainFact{
				Key:       evt.Key,
				Type:      evt.Type,
				Identity:  evt.Meta[deliveryMetaIdentity],
				State:     evt.Meta[deliveryMetaState],
				Content:   evt.Meta[deliveryMetaBody],
				Target:    event.Address{AgentID: event.AgentID(evt.Meta[deliveryMetaTargetAgent]), SessionID: event.SessionID(evt.Meta[deliveryMetaTargetSess])},
				Timestamp: evt.Timestamp.UnixNano(),
			})
		case event.TypeAgentOutput:
			if evt.Meta[deliveryMetaOwed] != "1" {
				continue
			}
			facts = append(facts, deliveryChainFact{
				Key:       evt.Key,
				Type:      evt.Type,
				Identity:  deliveryDeliveryFresh + string(evt.Key),
				Content:   evt.Message.Content,
				Target:    event.Address{AgentID: event.AgentID(evt.Meta[deliveryMetaTargetAgent]), SessionID: event.SessionID(evt.Meta[deliveryMetaTargetSess])},
				Declared:  true,
				Timestamp: evt.Timestamp.UnixNano(),
			})
		}
	}
	return facts
}

// deliveryDeclarations reports the owed declarations carrying one body.
func deliveryDeclarations(facts []deliveryChainFact, body string) []deliveryChainFact {
	out := make([]deliveryChainFact, 0, 1)
	for _, fact := range facts {
		if fact.Declared && fact.Content == body {
			out = append(out, fact)
		}
	}
	return out
}

// deliveryNewestState reports the settlement state one identity stands in: the
// last settlement fact the chain holds for it, and no state beside an identity
// the chain never settled.
func deliveryNewestState(facts []deliveryChainFact, identity string) (string, bool) {
	state := ""
	held := false
	for _, fact := range facts {
		if fact.Declared || fact.Identity != identity {
			continue
		}
		state = fact.State
		held = true
	}
	return state, held
}

// settlementFactsCarrying reports the settlement facts that keep one body: the
// retire and hold states record it for the audit trail, a discharged state does
// not.
func settlementFactsCarrying(facts []deliveryChainFact, body string) []deliveryChainFact {
	out := make([]deliveryChainFact, 0, 1)
	for _, fact := range facts {
		if !fact.Declared && fact.Content == body {
			out = append(out, fact)
		}
	}
	return out
}

// deliverySettlementFactsOf reports every settlement fact one identity holds, in
// commit order.
func deliverySettlementFactsOf(facts []deliveryChainFact, identity string) []deliveryChainFact {
	out := make([]deliveryChainFact, 0, 2)
	for _, fact := range facts {
		if !fact.Declared && fact.Identity == identity {
			out = append(out, fact)
		}
	}
	return out
}
