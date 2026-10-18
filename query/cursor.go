// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Cursor minting and binding: the opaque continuation token is minted beside
// the bounded read that produced it and checked against every read it is later
// presented with. The token carries its binding inside itself and exposes
// nothing, so no caller forges one and no read serves a page from another view.
//
// The check reports the first binding that diverged, in the order the refusals
// answer for: a cursor replayed at another revision is ErrCursorRevisionMismatch,
// one presented under another authorization scope is ErrCursorScopeChanged and
// one presented under another filter is ErrCursorFilterChanged. The bounded
// read window is checked by the read service before the binding, so a cursor
// whose window has expired answers ErrCursorWindowExpired rather than any of
// the three mismatches.
package query

import "github.com/SpellingDragon/tagent/event"

// mintCursor mints the opaque continuation token of one bounded read: the
// revision the read was anchored to, the authorization scope it ran under and
// the filter it ran under. The token is the only way a later page of the same
// read is continued, and carrying it never widens the scope it was minted
// under.
func mintCursor(at Revision, filter ListFilter) Cursor {
	return Cursor{bound: cursorBinding{Revision: at, Scope: filter.Scope, Filter: filter}}
}

// checkBinding checks a presented cursor against the read it is presented
// with and reports the first binding that diverged by name. A cursor is bound
// to the revision, the authorization scope and the filter of its minting read,
// and no path silently re-anchors one: every divergence is a named refusal so
// a caller can tell a replayed revision from a changed authorization and a
// changed filter.
func checkBinding(cursor Cursor, at Revision, filter ListFilter) error {
	if cursor.bound.Revision != at {
		return ErrCursorRevisionMismatch
	}
	if !scopeEqual(cursor.bound.Scope, filter.Scope) {
		return ErrCursorScopeChanged
	}
	if !filterEqual(cursor.bound.Filter, filter) {
		return ErrCursorFilterChanged
	}
	return nil
}

// scopeEqual reports whether two authorization scopes cover the same objects.
// The scope lists are unordered sets: a reordered scope is the same scope and
// never a refusal, while a narrowed or widened one always is.
func scopeEqual(a, b AuthorizationScope) bool {
	return agentSetEqual(a.Agents, b.Agents) && addressSetEqual(a.Sessions, b.Sessions)
}

// filterEqual reports whether two dimension filters narrow the projection the
// same way. Every dimension list is an unordered set, so a reordered filter is
// the same filter and never a refusal. The authorization scope is not part of
// this comparison: it is refused separately as ErrCursorScopeChanged so a
// changed authorization never hides behind a changed filter.
func filterEqual(a, b ListFilter) bool {
	return agentSetEqual(a.Agents, b.Agents) &&
		addressSetEqual(a.Sessions, b.Sessions) &&
		kindSetEqual(a.Kinds, b.Kinds) &&
		stateSetEqual(a.States, b.States)
}

// agentSetEqual reports whether two agent identity lists hold the same
// members, whatever order they list in.
func agentSetEqual(a, b []event.AgentID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		found := false
		for _, other := range b {
			if id == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// addressSetEqual reports whether two session address lists hold the same
// members, whatever order they list in.
func addressSetEqual(a, b []event.Address) bool {
	if len(a) != len(b) {
		return false
	}
	for _, addr := range a {
		found := false
		for _, other := range b {
			if addr == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// kindSetEqual reports whether two object-kind lists hold the same members,
// whatever order they list in.
func kindSetEqual(a, b []ObjectKind) bool {
	if len(a) != len(b) {
		return false
	}
	for _, kind := range a {
		found := false
		for _, other := range b {
			if kind == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// stateSetEqual reports whether two detail-state lists hold the same members,
// whatever order they list in.
func stateSetEqual(a, b []DetailState) bool {
	if len(a) != len(b) {
		return false
	}
	for _, st := range a {
		found := false
		for _, other := range b {
			if st == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
