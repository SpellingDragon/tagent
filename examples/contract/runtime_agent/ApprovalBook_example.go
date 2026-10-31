// Package agentexample shows the approval book the response fuse parks a whole
// oversized batch on and reads the ruling back through.
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agentexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

type demoBook struct{ manager *approval.ApprovalManager }

// Request files one batch for a human ruling.
//
// MUST NOT execute the calls it is handed: filing a batch only records it.
// MUST NOT invent a ruling: the answer a caller reads back is the human one.
func (b demoBook) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*approval.ApprovalRequest, error) {
	return b.manager.Request(ctx, id, tool, jsonArgs, risk, rule, reason)
}

// Lookup reads the effective ruling of one filed batch back.
//
// MUST NOT answer a batch the book never filed: an unknown digest answers none.
func (b demoBook) Lookup(tool, argsDigest string) *approval.ApprovalRequest {
	return b.manager.Lookup(tool, argsDigest)
}

// ExampleApprovalBook wires the loop's batch fuse onto a real approval manager:
// the fuse hands its whole oversized response over as one request and waits for
// the human ruling the same manager serves every other approval through.
// - MUST NOT park a batch beside no book: an unwired fuse refuses the whole response rather than executing any of it unruled.
func ExampleApprovalBook() {
	book := demoBook{manager: approval.NewApprovalManager(".", 0)}
	loop := loopOfCompositionRoot(book)
	loop.SetApprovalBook(book)
	fmt.Println("the batch fuse now parks on the one approval book")
}

// loopOfCompositionRoot stands for the assembled loop a composition root hands
// the setter to: agent.NewLoop builds one, and the sketch keeps the example
// free of a nil receiver a copied snippet could panic on.
func loopOfCompositionRoot(book demoBook) interface{ SetApprovalBook(agent.ApprovalBook) } {
	return assembledLoop{book: book}
}

type assembledLoop struct{ book demoBook }

func (assembledLoop) SetApprovalBook(agent.ApprovalBook) {}
