package rl

// 契约: docs/wiki/rl/rl-architecture.md#http-api
//
// FeedbackAttribution is the running account of how the submissions of the
// training observation face were attributed: the host reads it beside the
// training face so an operator can tell bound evidence from unbound traffic.
type FeedbackAttribution struct {
	// Bound counts the submissions attributed to recorded evidence.
	Bound int64 `json:"bound"`
	// Unbound counts the submissions carrying no locatable reference.
	Unbound int64 `json:"unbound"`
	// Ambiguous counts the submissions whose evidence conflicted.
	Ambiguous int64 `json:"ambiguous"`
}
