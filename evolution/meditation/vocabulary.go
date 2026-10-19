// 契约: docs/FACT-VOCABULARY.md#families
//
// The meditation vocabulary: the fate one card is filed under, the retention
// kind a held reference is registered as, and the condensed card a pass leaves
// behind as a fact of its own — the artifact is a new reference over the refs it
// keeps, never a rewrite of a fact it condenses. The card sequence and the
// artifact are identified by content, so one body of material and one
// condensation each answer one identity.
package meditation

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// MeditationFate names the one channel a card's meditation fate is filed under:
// exactly one of retain, evict or protect for every card of a pass. The fates
// are mutually exclusive and exhaustive — a card belongs to one channel and
// never to a fourth — so the eviction a pass declares is the whole eviction it
// performs, and it is performed on the tombstone channel alone.
type MeditationFate string

const (
	// MeditationRetain is the fate of a card the pass keeps: the fact stays as
	// it is and takes no part in any eviction.
	MeditationRetain MeditationFate = "retain"
	// MeditationEvict is the fate of a card the pass evicts: the fact is
	// tombstoned through the one eviction channel and stays readable until
	// compaction reclaims it. Eviction never rewrites or destroys bytes
	// directly.
	MeditationEvict MeditationFate = "evict"
	// MeditationProtect is the fate of a card whose original is protected from
	// eviction: the condensed artifact references it or another holder already
	// holds it, so the referenced original is never deleted while that
	// reference stands.
	MeditationProtect MeditationFate = "protect"
)

// RetentionMeditation is the kind of a retention registration protecting
// material a condensed artifact still references: the hold a meditation pass
// takes on each key its artifact cites, so the referenced original is never
// tombstoned or reclaimed while the artifact can still recall it by key.
const RetentionMeditation ledger.RetentionKind = "curation_reference"

// MeditationArtifactType is the frozen type label of the condensed card
// artifact one meditation pass leaves behind.
const MeditationArtifactType = "memory.condensed_card"

// MeditationCard is one card line of the sequence a meditation pass runs over: the
// [evt_key] ticket of the fact behind it, its frozen type, the small rendering
// text of the line, the time the fact was recorded, and the count of standing
// causal links — references and parent keys — that point at the fact.
type MeditationCard struct {
	// Key is the [evt_key] ticket of the fact the card line carries.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the small rendering text of the card line.
	Summary string
	// RecordedAt is the time the fact was recorded.
	RecordedAt time.Time
	// Links counts the standing causal links (references and parent keys)
	// pointing at the fact.
	Links int
}

// MeditationArtifact is the condensed card one pass leaves behind: the new
// reference the meditation produces, never a rewrite of any fact it condenses.
// Its refs are the [evt_key] tickets of the key cards it keeps, so the
// condensed result stays traceable and recall still refills by key. Sunk
// counts the card lines the pass collapsed into its count line when it
// degraded to the engineering fallback, and is zero while a condensation
// holds.
type MeditationArtifact struct {
	// Key is the content-addressed identity of the artifact: the same
	// condensation always mints the same identity.
	Key event.EventID
	// Type is the frozen type label of the artifact.
	Type string
	// Summary is the condensed text of the overflow run, or the count line
	// the degraded pass sank it into.
	Summary string
	// Refs are the key references the condensed result keeps, in value order.
	Refs []event.EventID
	// Sunk is how many card lines the degraded pass sank into the count line.
	Sunk int
	// CreatedAt is the time the pass created the artifact.
	CreatedAt time.Time
}

// meditationSequenceKey derives the identity of one card sequence from the keys,
// types and recorded times of its cards: the same body of material always answers
// the same identity, so the bound judgment filed over it is quoted by one key for
// as long as that material stands. The order the cards arrived in names nothing
// about the material, so the parts are sorted before they are hashed.
func meditationSequenceKey(cards []MeditationCard) event.EventID {
	parts := make([]string, 0, len(cards))
	for _, card := range cards {
		parts = append(parts, string(card.Key)+"|"+card.Type+"|"+card.RecordedAt.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return event.EventID("seq-" + hex.EncodeToString(sum[:8]))
}

// meditationArtifactKey derives the content-addressed identity of one condensed
// artifact from the key references it carries: the same condensation always
// mints the same identity, so a re-run of one pass never leaves two artifacts
// of the same refs behind.
func meditationArtifactKey(refs []event.EventID) event.EventID {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, string(ref))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return event.EventID("cur-" + hex.EncodeToString(sum[:8]))
}

// FactKey returns the condensed artifact's identity as a fact key: the
// content-addressed key of the card references it carries, so the same
// condensation answers the same key every time it is asked.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (a MeditationArtifact) FactKey() string { return string(a.Key) }

// FactType returns the frozen type a condensed artifact is read as.
func (a MeditationArtifact) FactType() string { return MeditationArtifactType }

// The condensed artifact family satisfies the fact category contract.
var _ event.Fact = MeditationArtifact{}
