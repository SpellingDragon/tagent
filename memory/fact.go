package memory

import "github.com/SpellingDragon/tagent/event"

// FactKey returns the reference's identity as a fact key: the canonical hex key
// of the original blob it names. The deduplication scope (owner, format and
// retention policy) keeps references registered side by side and never
// re-identifies the blob, so two tickets of byte-identical content answer the
// same key.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r OriginalRef) FactKey() string { return string(r.Key) }

// FactType returns the frozen type a reference ticket is read as.
func (r OriginalRef) FactType() string { return "memory.original" }

// The reference ticket family satisfies the fact category contract.
var _ event.Fact = OriginalRef{}

// FactKey returns the compress request's identity as a fact key: the
// content-addressed key one judgment is quoted by, unchanged by every re-filing
// of that judgment.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r CompressRequest) FactKey() string { return string(r.ID) }

// FactType returns the frozen type a compress request is read as.
func (r CompressRequest) FactType() string { return CompressRequestType }

// The compress request family satisfies the fact category contract.
var _ event.Fact = CompressRequest{}

// FactKey returns the condensed artifact's identity as a fact key: the
// content-addressed key of the card references it carries, so the same
// condensation answers the same key every time it is asked.
func (a CurationArtifact) FactKey() string { return string(a.Key) }

// FactType returns the frozen type a condensed artifact is read as.
func (a CurationArtifact) FactType() string { return CurationArtifactType }

// The condensed artifact family satisfies the fact category contract.
var _ event.Fact = CurationArtifact{}
