// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The anchor state hook is the registration seam for state the chain cannot
// derive: a session's owner registers a named capture and restore, and the
// fold moment carries the captured bytes on the same fact as the survivor
// checkpoint. A run with no registered owners mints the same fold as before.
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
)

// anchorStateOwner is one registered source of non-derivable session state.
type anchorStateOwner struct {
	capture func(ctx context.Context, session event.SessionID) ([]byte, error)
	restore func(ctx context.Context, session event.SessionID, payload []byte) error
}

// anchorStateSnapshot is one named state blob a fold checkpoint carries.
type anchorStateSnapshot struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// WithAnchorState registers one named owner before assembly opens recovery,
// so a restart restores through the same registration that a live fold captures
// through. A nil callback leaves that half unregistered; registering the same
// name again replaces the pair, so one owner holds one slot.
func WithAnchorState(name string, capture func(context.Context, event.SessionID) ([]byte, error), restore func(context.Context, event.SessionID, []byte) error) Option {
	return func(c *assemblyConfig) {
		if name == "" {
			return
		}
		if c.anchorStateOwners == nil {
			c.anchorStateOwners = map[string]anchorStateOwner{}
		}
		c.anchorStateOwners[name] = anchorStateOwner{capture: capture, restore: restore}
	}
}

// RegisterAnchorState registers one named owner of state the chain cannot
// derive on a running assembly. The fold moment calls capture for the session
// being folded; recovery calls restore with the very bytes capture returned.
// Use WithAnchorState when the owner must already exist during boot recovery.
func (r *Run) RegisterAnchorState(name string, capture func(context.Context, event.SessionID) ([]byte, error), restore func(context.Context, event.SessionID, []byte) error) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.anchorStateOwners == nil {
		r.anchorStateOwners = map[string]anchorStateOwner{}
	}
	r.anchorStateOwners[name] = anchorStateOwner{capture: capture, restore: restore}
}

// captureAnchorStates states the registered owners snapshots as one opaque
// payload, ordered by name so one fold mints one byte string. No owner with a
// capture callback yields no payload at all, which keeps an unregistered runs
// fold byte-identical to the fold it minted before this seam existed.
func (r *Run) captureAnchorStates(ctx context.Context, session event.SessionID) ([]byte, error) {
	r.mu.Lock()
	owners := make(map[string]func(context.Context, event.SessionID) ([]byte, error))
	for name, owner := range r.anchorStateOwners {
		if owner.capture != nil {
			owners[name] = owner.capture
		}
	}
	r.mu.Unlock()
	if len(owners) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)
	snapshots := make([]anchorStateSnapshot, 0, len(names))
	for _, name := range names {
		data, err := owners[name](ctx, session)
		if err != nil {
			return nil, fmt.Errorf("tagent: the state owner %s did not capture session %s: %w", name, session, err)
		}
		snapshots = append(snapshots, anchorStateSnapshot{Name: name, Data: data})
	}
	encoded, err := json.Marshal(snapshots)
	if err != nil {
		return nil, fmt.Errorf("tagent: state the anchor snapshots: %w", err)
	}
	return encoded, nil
}

// restoreAnchorStates hands each captured blob to the owner of the same name.
// A blob whose owner this run did not register is reported and left alone: the
// chain stated it, but only its owner may interpret it.
func (r *Run) restoreAnchorStates(ctx context.Context, session event.SessionID, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	var snapshots []anchorStateSnapshot
	if err := json.Unmarshal(payload, &snapshots); err != nil {
		return fmt.Errorf("tagent: read the anchor state payload: %w", err)
	}
	for _, snapshot := range snapshots {
		r.mu.Lock()
		owner, held := r.anchorStateOwners[snapshot.Name]
		r.mu.Unlock()
		if !held || owner.restore == nil {
			assemblyWarn("tagent: the anchor state owner %s of session %s has no restore registered", snapshot.Name, session)
			continue
		}
		if err := owner.restore(ctx, session, snapshot.Data); err != nil {
			return fmt.Errorf("tagent: the state owner %s did not restore session %s: %w", snapshot.Name, session, err)
		}
	}
	return nil
}
