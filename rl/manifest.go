package rl

import "github.com/SpellingDragon/tagent/event"

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// SampleProvenance links one exported sample to the records it was produced from:
// the capture, export, episode, decision and attempt it descends through, each with
// the version it was read at, and the concrete reward instance the sample carries.
//
// A sample whose lineage cannot be established is listed in the rejection ledger
// rather than written under a guessed link, and its split group names the episodes it
// shares data with so a leakage-free split stays drawn on real dependencies. The
// source is pinned to instance versions and never to a rule version alone: the
// reward the sample carries is named by instance ID and revision, and a sample
// whose reward instance cannot be pinned is refused rather than written under a
// derivation rule version that could stand for several records.
type SampleProvenance struct {
	// SampleID identifies the exported sample.
	SampleID string
	// CaptureID names the capture the sample was recorded under.
	CaptureID string
	// ExportID names the export run that wrote the sample.
	ExportID string
	// EpisodeID names the episode the sample belongs to.
	EpisodeID event.EpisodeID
	// DecisionID names the decision the sample was reduced from.
	DecisionID event.DecisionID
	// AttemptID names the attempt the sample observed.
	AttemptID event.AttemptID
	// Schema is the record schema version the sample was read at.
	Schema SchemaVersion
	// RewardInstanceID names the concrete reward instance the sample carries.
	RewardInstanceID string
	// RewardRevision is the revision of that reward instance, so a late feedback
	// derived value is a different citation than the one written earlier.
	RewardRevision int64
	// RewardVersion is the reward derivation rule version the sample carries; it
	// records the rule the value was computed with and is not an instance
	// identity.
	RewardVersion string
	// GroupID names the leakage-free split group the sample was assigned to.
	GroupID string
}

// ProvenanceGapKind classifies why an export run refused a sample.
type ProvenanceGapKind string

const (
	// ProvenanceGapMissing marks a sample with no provenance entry at all.
	ProvenanceGapMissing ProvenanceGapKind = "missing"
	// ProvenanceGapMalformed marks a provenance entry that does not parse.
	ProvenanceGapMalformed ProvenanceGapKind = "malformed"
	// ProvenanceGapUnknown marks a sample whose source quality could not be
	// established, so its lineage is reported as unknown rather than assumed.
	ProvenanceGapUnknown ProvenanceGapKind = "unknown"
)

// ProvenanceGap is one sample an export run refused to write, with the reason it was
// refused.
//
// A refusal is counted and named rather than dropped in silence, so the manifest
// shows what was left out and why.
type ProvenanceGap struct {
	// SampleID identifies the refused sample.
	SampleID string
	// Kind classifies the gap.
	Kind ProvenanceGapKind
	// Reason states why the sample was refused.
	Reason string
}

// ExportManifest is the provenance manifest of one export run: one entry per written
// sample, the leakage-free split groups those samples were assigned to, and the
// ledger of everything the run refused.
//
// A manifest is complete only when the run read every authorized page without a
// failure, the row recheck refused nothing in silence, and the digest covers the
// bytes actually written; anything less is marked partial with its reason readable.
type ExportManifest struct {
	// Schema is the manifest schema version the manifest was written with.
	Schema SchemaVersion
	// ExportID identifies the export run.
	ExportID string
	// Samples lists the provenance of every sample the run wrote.
	Samples []SampleProvenance
	// Groups lists the leakage-free split groups the samples were assigned to, so no
	// group crosses the train and test sides.
	Groups []EpisodeGroup
	// Rejected lists the samples the run refused, each with its reason.
	Rejected []ProvenanceGap
	// Forbidden counts the rows the per-row authorization recheck refused.
	Forbidden int64
	// Filtered counts the facts the caller's filters removed.
	Filtered int64
	// ParentKeyMissing counts the samples whose parent key was absent; an absent key
	// is reported as absent and never invented.
	ParentKeyMissing int64
	// SourceSHA256 is the digest of the bytes the run actually wrote.
	SourceSHA256 string
	// Cutoff records the time bound the run actually applied, in Unix milliseconds.
	Cutoff int64
	// Complete reports whether the run read every authorized page and wrote every
	// accepted sample without a failure.
	Complete bool
}

// Manifest contract assertions pin every declared field and gap kind: a renamed
// field or a changed field type stops the package from compiling.
var (
	_ = SampleProvenance{
		SampleID:         "",
		CaptureID:        "",
		ExportID:         "",
		EpisodeID:        "",
		DecisionID:       "",
		AttemptID:        "",
		Schema:           "",
		RewardInstanceID: "",
		RewardRevision:   0,
		RewardVersion:    "",
		GroupID:          "",
	}

	_ = ProvenanceGap{
		SampleID: "",
		Kind:     ProvenanceGapMissing,
		Reason:   "",
	}

	_ = ExportManifest{
		Schema:           "",
		ExportID:         "",
		Samples:          []SampleProvenance{},
		Groups:           []EpisodeGroup{},
		Rejected:         []ProvenanceGap{},
		Forbidden:        0,
		Filtered:         0,
		ParentKeyMissing: 0,
		SourceSHA256:     "",
		Cutoff:           0,
		Complete:         false,
	}
)

const (
	_ = ProvenanceGapMissing
	_ = ProvenanceGapMalformed
	_ = ProvenanceGapUnknown
)
