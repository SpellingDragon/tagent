package plugin

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
//
// Delivery is one outbound body the sending plugin is about to hand to a
// channel: the body, the lineage of the turn that produced it and the explicit
// address it is addressed to. The review plugin rules this semantic before the
// send, so a delivery is reviewed whether or not a send tool exists.
type Delivery struct {
	// Content is the body to deliver.
	Content string

	// Lineage is the trigger-source stamp of the turn that produced the body,
	// read from the same stamp tool-call rulings carry.
	Lineage string

	// Target is the explicit address the body is delivered to.
	Target event.Address
}

// DeliveryRuler is the delivery->disposition port: it renders the one ruling
// for one outbound delivery before the sending plugin sends it. A hold parks
// the delivery as a wait: the channel receives nothing until the host approves
// it, and a refusal discards it with an audit fact.
//
// The redline list (3 rules):
// MUST render one ruling per delivery, over the delivery semantic alone.
// MUST NOT deliver the body itself, and MUST NOT bypass a hold it rendered.
// MUST NOT report a hold as a delivery.
type DeliveryRuler interface {
	// Rule returns the disposition for one outbound delivery. An allowed or
	// recorded delivery is sent; a held delivery waits for the host's decision.
	// Rule may be called concurrently for deliveries of different sessions.
	Rule(ctx context.Context, delivery Delivery) (Disposition, error)
}
