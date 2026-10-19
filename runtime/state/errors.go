// 契约: docs/wiki/platform/org-hot-reload.md#overview
//
// The failure cause chain of the two reload paths: a structural change that is
// refused or fails keeps the chain of the causes it rests on — the source file,
// the normalized line within it and the named reason beside it — so the
// failure is locatable and is never swallowed into a generic reload failure.
//
// Each link wraps the failure it reports, so the chain stays matchable by
// errors.Is and errors.As all the way down: the source and the line locate the
// change, the reason names what stopped it, and the wrapped failure keeps the
// named refusal underneath.
package state

import (
	"fmt"
)

// Cause is one link of a failure's cause chain: the source file the failure
// comes from, the normalized line within that source and the named reason it
// rests on, beside the failure the link wraps.
//
// The link is self-contained: a reader holding only the link knows which source
// and which line to look at and what named reason stopped the change. The
// wrapped failure is kept rather than flattened, so no link ever collapses a
// chain into one generic message.
type Cause struct {
	// Source is the source file the failure comes from.
	Source string
	// Line is the normalized line within the source; it stays zero when the
	// failure names no line.
	Line int
	// Reason is the named reason the failure rests on.
	Reason string
	// Err is the failure the link wraps, kept so the chain stays matchable.
	Err error
}

// NewCause returns one cause chain link over the given source, normalized line
// and named reason, wrapping the failure it reports. The failure is kept
// beside the link rather than flattened into it.
func NewCause(source string, line int, reason string, err error) *Cause {
	return &Cause{Source: source, Line: line, Reason: reason, Err: err}
}

// Error reports the link as the source and normalized line it locates beside
// the named reason it rests on and the failure it wraps, so the rendered
// message reads as the chain it belongs to.
func (c *Cause) Error() string {
	where := c.Source
	if c.Line > 0 {
		where = fmt.Sprintf("%s:%d", c.Source, c.Line)
	}
	msg := fmt.Sprintf("state: %s: %s", where, c.Reason)
	if c.Err != nil {
		msg += ": " + c.Err.Error()
	}
	return msg
}

// Unwrap reports the failure the link wraps, so a caller matching the named
// refusal underneath matches it through the chain.
func (c *Cause) Unwrap() error { return c.Err }

// CauseChain reports the cause links a failure carries, ordered from the
// outermost link to the innermost one. A failure that carries no link reports
// an empty chain, and a chain never loses the links sitting under a joined
// failure.
func CauseChain(err error) []*Cause {
	var out []*Cause
	var walk func(error)
	walk = func(err error) {
		if err == nil {
			return
		}
		if cause, ok := err.(*Cause); ok {
			out = append(out, cause)
		}
		switch inner := err.(type) {
		case interface{ Unwrap() []error }:
			for _, e := range inner.Unwrap() {
				walk(e)
			}
		case interface{ Unwrap() error }:
			walk(inner.Unwrap())
		}
	}
	walk(err)
	return out
}
