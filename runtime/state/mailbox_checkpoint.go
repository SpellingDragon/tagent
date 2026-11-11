// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The mailbox state slice: the notifications the mailbox still owes a delivery
// for, as the framework checkpoint carries them. The slice is the size of the
// work still owed, never of the work the process already delivered, so a boot
// restores the obligations a stopped run left open instead of replaying the
// history that produced them.
package state

import (
	"context"
	"encoding/json"

	"github.com/SpellingDragon/tagent/runtime/task"
)

// StateSliceMailbox is the name the mailbox slice is keyed by in a checkpoint.
const StateSliceMailbox = "mailbox"

// NewMailboxState returns the mailbox's recoverable state as the framework sees
// it: the slice one mailbox store states and restores.
func NewMailboxState(store *MemoryStore) Stateful { return mailboxState{store: store} }

// mailboxState is the mailbox's recoverable state as the framework sees it.
type mailboxState struct {
	// store is the mailbox this slice states and restores.
	store *MemoryStore
}

// mailboxSliceWire is the payload of the mailbox slice: every notification the
// mailbox still owes, with the routing it froze.
type mailboxSliceWire struct {
	Owed []task.OutboxNotice `json:"owed,omitempty"`
}

// StateName reports the name the mailbox slice is keyed by.
func (s mailboxState) StateName() string { return StateSliceMailbox }

// SnapshotState states every notification the mailbox still owes a delivery for.
func (s mailboxState) SnapshotState(ctx context.Context) ([]byte, error) {
	if s.store == nil {
		return []byte("null"), nil
	}
	wire := mailboxSliceWire{}
	if s.store.consumptions != nil {
		wire.Owed = s.store.consumptions.OwedNotices()
	}
	return json.Marshal(wire)
}

// RestoreState puts the owed notifications back: each is accepted into the site
// its frozen routing names, so the work a stopped run left unconsumed waits for
// the next decision instead of being lost with the process. The obligations the
// cut covered come back from the payload; the ones the account behind the
// mailbox still owes are read from that account, which holds every commitment
// the log folded — the ones past the cut included — so this restore states the
// whole owed set whenever the framework restores the slice. The fold is
// idempotent by the notification identity, so an obligation both sources name is
// seated once.
func (s mailboxState) RestoreState(ctx context.Context, payload []byte) error {
	if s.store == nil {
		return nil
	}
	var wire mailboxSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	return s.store.foldOwed(ctx, s.owedOfWork(wire.Owed))
}

// owedOfWork reports the obligations this restore folds: the ones the cut
// stated beside the ones the account behind the mailbox still owes, each named
// once by the notification identity acceptance answers to, so one obligation
// two sources state is seated once.
func (s mailboxState) owedOfWork(stated []task.OutboxNotice) []task.OutboxNotice {
	owed := make([]task.OutboxNotice, 0, len(stated))
	seen := make(map[noticeKey]bool, len(stated))
	for _, notice := range stated {
		key := noticeKeyOf(notice.Notice.Notice)
		if seen[key] {
			continue
		}
		seen[key] = true
		owed = append(owed, notice)
	}
	if s.store.consumptions == nil {
		return owed
	}
	for _, notice := range s.store.consumptions.OwedNotices() {
		key := noticeKeyOf(notice.Notice.Notice)
		if seen[key] {
			continue
		}
		seen[key] = true
		owed = append(owed, notice)
	}
	return owed
}
