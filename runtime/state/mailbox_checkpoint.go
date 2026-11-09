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
// the next decision instead of being lost with the process.
func (s mailboxState) RestoreState(ctx context.Context, payload []byte) error {
	if s.store == nil {
		return nil
	}
	var wire mailboxSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	return s.store.foldOwed(ctx, wire.Owed)
}
