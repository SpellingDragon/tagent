package memory

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// ArchiveOriginals is the original-text face one archive migration writes and
// reads back through: it maps the byte identity and the reference metadata a
// migration carries onto the ledger.OriginalStore of the tier it stands over. Its
// method signatures carry built-in and event types only, so the migration's
// original port matches it structurally and no package edge is required in
// either direction.
type ArchiveOriginals struct {
	store ledger.OriginalStore
}

// NewArchiveOriginals returns the archive face over one original store.
func NewArchiveOriginals(store ledger.OriginalStore) *ArchiveOriginals {
	return &ArchiveOriginals{store: store}
}

// PutOriginal commits body under key beside the reference metadata the
// migration names, so a repeated import of the same dump lands the same bytes
// under the same reference.
func (a *ArchiveOriginals) PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error {
	return a.store.Put(ctx, ledger.OriginalRef{
		Key:             key,
		Owner:           owner,
		Format:          format,
		RetentionPolicy: retention,
		Integrity:       ledger.Integrity{Digest: digest, Length: length},
	}, body)
}

// GetOriginal returns the exact bytes the store holds under key and whether it
// holds any: a key nothing is registered under reads back as absent beside no
// error, and a failed read reports its cause.
func (a *ArchiveOriginals) GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error) {
	result := a.store.Get(ctx, key)
	switch result.Outcome {
	case ledger.RecallFound:
		return result.Body, true, nil
	case ledger.RecallMissing, ledger.RecallExpired:
		return nil, false, nil
	default:
		return nil, false, result.Err
	}
}
