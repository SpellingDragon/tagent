package rl

import "github.com/SpellingDragon/tagent/event"

// 契约: docs/wiki/rl/rl-architecture.md#http-api
//
// FeedbackPayload is the body of one feedback submission: the hex event key it
// targets, the decision or attempt it belongs to when the caller can name them, and
// the external conclusion to bind there.
//
// The type is a wire contract only and pulls in no HTTP dependency. Authentication
// runs at a single point before routing, so no endpoint, including a read-only one,
// has a bypass, and a caller credential stays out of any persisted record. A payload
// that cannot be resolved to an event is marked unbound, and a binding is never
// inherited from the most recent parent call.
type FeedbackPayload struct {
	// EventKey is the hex key of the event the feedback is bound to.
	EventKey string
	// DecisionID names the decision the feedback belongs to; it is empty when the
	// caller names only an event key.
	DecisionID event.DecisionID
	// AttemptID names the attempt the feedback belongs to; it is empty when the
	// feedback is bound at the decision boundary.
	AttemptID event.AttemptID
	// Conclusion is the external conclusion to bind.
	Conclusion string
	// Source names where the conclusion came from.
	Source string
}

// EndpointAllowlist is the declared host allowlist of the dynamic model endpoint: an
// exact host with a free port, a scheme restricted to http or https, and no userinfo
// or fragment in the URL.
//
// The allowlist is rechecked on every redirect hop, so a first hop to an allowed host
// that redirects outside the list is refused and no prompt is sent to the destination
// of that hop.
type EndpointAllowlist struct {
	// Hosts lists the exact hosts allowed; the port is free.
	Hosts []string
	// Schemes lists the allowed URL schemes, each of http or https.
	Schemes []string
	// MaxRedirects is the number of redirect hops that are each rechecked.
	MaxRedirects int
}

// RequestLimits declares the single-point bounds every request is checked against,
// including a feedback submission.
//
// The limits are enforced in one place: a body over the byte bound is refused, the
// message count and the per-message byte bound are refused as well, and a value that
// would turn a bound off is a configuration error rather than a valid request.
type RequestLimits struct {
	// MaxBodyBytes bounds a request body.
	MaxBodyBytes int64
	// MaxMessages bounds the message count of a task submission.
	MaxMessages int
	// MaxMessageBytes bounds the content of a single message.
	MaxMessageBytes int64
	// MaxResponseBytes bounds a response body read back from the endpoint.
	MaxResponseBytes int64
	// Roles lists the roles a message may carry.
	Roles []string
}

// Feedback HTTP contract assertions pin every declared field: a renamed wire field
// or a changed field type stops the package from compiling.
var (
	_ = FeedbackPayload{
		EventKey:   "",
		DecisionID: "",
		AttemptID:  "",
		Conclusion: "",
		Source:     "",
	}

	_ = EndpointAllowlist{
		Hosts:        []string{},
		Schemes:      []string{},
		MaxRedirects: 0,
	}

	_ = RequestLimits{
		MaxBodyBytes:     0,
		MaxMessages:      0,
		MaxMessageBytes:  0,
		MaxResponseBytes: 0,
		Roles:            []string{},
	}
)
