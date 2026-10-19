package stateexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// originalMeta is the reference metadata one import marks an archived original by.
type originalMeta struct {
	owner     event.Address
	format    string
	retention string
	digest    string
	length    int64
}

// archiveMigration is the canonical write face of the migrated originals: the bytes one
// archive dump lands under, beside the reference metadata the import names.
type archiveMigration struct {
	archiveOriginals
	meta map[event.EventID]originalMeta
}

// PutOriginal stores body under key beside the reference metadata the import names.
func (m *archiveMigration) PutOriginal(_ context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error {
	if m.bodies == nil {
		m.bodies = map[event.EventID][]byte{}
	}
	if m.meta == nil {
		m.meta = map[event.EventID]originalMeta{}
	}
	m.bodies[key] = append([]byte(nil), body...)
	if _, ok := m.meta[key]; !ok {
		m.meta[key] = originalMeta{owner: owner, format: format, retention: retention, digest: digest, length: length}
	}
	return nil
}

var _ state.OriginalMigrator = (*archiveMigration)(nil)

// ExampleOriginalMigrator lands one archived original beside its reference metadata, reads
// it back byte for byte and reports that a later Put keeps the first metadata effective.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func ExampleOriginalMigrator() {
	ctx := context.Background()
	migrator := &archiveMigration{}
	owner := event.Address{AgentID: "assistant", SessionID: "session-1"}

	if err := migrator.PutOriginal(ctx, "event-1", owner, "text", "durable", "digest-1", 13, []byte("original text")); err != nil {
		return
	}
	if err := migrator.PutOriginal(ctx, "event-1", owner, "text", "durable", "digest-2", 4, []byte("late")); err != nil {
		return
	}
	body, held, err := migrator.GetOriginal(ctx, "event-1")
	if err != nil || !held || string(body) != "late" {
		return
	}
	if migrator.meta["event-1"].digest != "digest-1" {
		return
	}
}
