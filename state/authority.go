package state

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/event"
)

// Subject is the trusted identity one read is checked against. A read is
// answered for a subject only beside a grant recorded for that subject and
// target.
type Subject string

// ErrReadNotAllowed reports a read of a target no grant covers for the subject
// that asked. An authority that holds no grants refuses every read.
var ErrReadNotAllowed = errors.New("state: read is not allowed for this subject and target")

// grantKey is one subject and one target address: the unit one read grant
// covers.
type grantKey struct {
	subject Subject
	target  event.Address
}

// 契约: docs/wiki/agent/event-flow.md#multi-user-isolation
//
// Authority holds the read grants recorded for subjects: which target each
// subject may read. A check only reads the recorded grants and never records
// one, so asking about a target grants nothing.
type Authority struct {
	mu    sync.RWMutex
	reads map[grantKey]struct{}
}

// NewAuthority returns an authority that holds no grants, so every read is
// refused until a grant is recorded.
func NewAuthority() *Authority {
	return &Authority{reads: map[grantKey]struct{}{}}
}

// GrantRead records the read grant of one subject over one target. The subject
// and the target are named explicitly: a grant covers that pair alone and
// spreads to no neighbouring session.
func (a *Authority) GrantRead(ctx context.Context, subject Subject, target event.Address) error {
	if a == nil {
		return ErrReadNotAllowed
	}
	if subject == "" {
		return ErrReadNotAllowed
	}
	if !target.IsComplete() {
		return ErrIncompleteAddress
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads[grantKey{subject: subject, target: target}] = struct{}{}
	return nil
}

// CheckRead reports whether one subject may read one target. A missing
// authority, an unnamed subject and a pair no grant covers all refuse the read,
// and the check records nothing.
func (a *Authority) CheckRead(ctx context.Context, subject Subject, target event.Address) error {
	if a == nil {
		return ErrReadNotAllowed
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if _, ok := a.reads[grantKey{subject: subject, target: target}]; !ok {
		return ErrReadNotAllowed
	}
	return nil
}
