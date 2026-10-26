// Package rootexample shows how a host satisfies the SettleWake port.
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
package rootexample

import (
	"context"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// demoWake is the mailbox account stand-in: the one ledger an entry moves
// through from pending to claimed to consumed on.
type demoWake struct {
	entries []state.WorkSeat
}

func (w *demoWake) RestoreOwed(_ context.Context) error { return nil }

func (w *demoWake) Unconsumed() []state.WorkSeat { return w.entries }

func (w *demoWake) Claim(_ context.Context, _ event.Address, _ event.InstanceID, _ state.AcceptSeq) error {
	return nil
}

func (w *demoWake) Release(_ context.Context, _ event.Address, _ event.InstanceID, _ state.AcceptSeq) error {
	return nil
}

func (w *demoWake) Consume(_ context.Context, _ event.Address, _ event.InstanceID, _ state.AcceptSeq) error {
	return nil
}

// ExampleSettleWake demonstrates the account the settle pump drives its turns
// from: one entry is claimed for exactly one turn, and only its consumption
// receipt closes it — a turn that stopped before its delivery releases the
// entry back, so the work stays owed instead of dying with the turn.
func ExampleSettleWake() {
	addr := event.Address{AgentID: "agent-a", SessionID: "chat-a"}
	var account tagent.SettleWake = &demoWake{}
	_ = account.Unconsumed()
	_ = account.Claim(context.Background(), addr, "instance-1", 1)
	_ = account.Consume(context.Background(), addr, "instance-1", 1)
}
