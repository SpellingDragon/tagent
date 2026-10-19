package rl

import (
	"context"
	"encoding/json"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
//
// Skeleton compile-time assertions for the phase 1 record, grouping and capture
// declarations of this package.
//
// The phase 2 implementation type that satisfies Capture does not exist yet, so
// there is no `var _ Capture = (*capture)(nil)` to write. The assertions below pin
// what the skeleton proves on its own: every declared method keeps its exact
// shape, every documented record field keeps its name and type, and the states and
// bounds stay compile-time constants.
var (
	_ Capture = (Capture)(nil)

	_ func(Capture, context.Context, model.Request) (ModelViewRef, error)     = Capture.Snapshot
	_ func(Capture, context.Context, AttemptRecord) (CaptureAdmission, error) = Capture.Record
	_ func(Capture, context.Context) (CaptureManifest, error)                 = Capture.Seal
)

// Record, grouping and capture types, naming every documented field so a renamed
// field or a changed field type stops the package from compiling.
var (
	_ = EpisodeRecord{
		Schema:    "",
		ID:        "",
		ParentID:  "",
		RootID:    "",
		Ancestors: []event.EpisodeID{},
		Sessions:  []event.SessionID{},
		DependsOn: []EpisodeDependency{},
		Lineage:   LineageUnknown,
		Timestamp: time.Time{},
	}

	_ = DecisionRecord{
		Schema:       "",
		ID:           "",
		InvocationID: "",
		EpisodeID:    "",
		InputEvents:  []event.EventID{},
		ModelView:    ModelViewRef{},
		Timestamp:    time.Time{},
	}

	_ = AttemptRecord{
		Schema:         "",
		ID:             "",
		DecisionID:     "",
		EpisodeID:      "",
		ModelVersion:   "",
		Response:       model.Response{},
		Terminal:       model.KindUnknown,
		Usage:          model.Usage{},
		Error:          "",
		Logprobs:       LogprobsUnknown,
		LogprobsSource: "",
		Timestamp:      time.Time{},
	}

	_ = ModelViewRef{
		Model:            "",
		ModelVersion:     "",
		ParametersDigest: "",
		PromptVersion:    "",
		VisibleTools:     []VersionedName{},
		VisibleSkills:    []VersionedName{},
		ContextDigest:    "",
		Snapshot:         SnapshotRef{},
	}

	_ = SnapshotRef{
		SnapshotID: "",
		Digest:     "",
		ReadEntry:  "",
	}

	_ = VersionedName{
		Name:    "",
		Version: "",
	}

	_ = EpisodeDependency{
		EpisodeID: "",
		DependsOn: "",
		Kind:      DependencyChildCall,
		Lineage:   LineageUnknown,
	}

	_ = EpisodeGroup{
		GroupID: "",
		RootID:  "",
		Members: []event.EpisodeID{},
		Basis:   []EpisodeDependency{},
		Lineage: LineageUnknown,
	}

	_ = CaptureAdmission{
		Admitted: false,
		Bound:    BoundNone,
		Reason:   "",
	}

	_ = CaptureManifest{
		CaptureID: "",
		Sealed:    false,
		Partial:   false,
		Records:   0,
		Dropped:   0,
		InFlight:  0,
		Files:     []CaptureFileDigest{},
		Sync:      WriterSyncResult{},
		Reasons:   []string{},
	}

	_ = CaptureFileDigest{
		Name:    "",
		SHA256:  "",
		Bytes:   0,
		Records: 0,
	}

	_ = WriterSyncResult{
		Synced: false,
		Reason: "",
		At:     time.Time{},
	}
)

// Record versions, lineages, logprobs states, dependency kinds and admission
// bounds must stay compile-time constants: a variable or a computed value stops
// this block from compiling.
const (
	_ = LineageUnknown
	_ = LineageKnown

	_ = LogprobsUnknown
	_ = LogprobsPresent
	_ = LogprobsUnsupported

	_ = DependencyChildCall
	_ = DependencyHandoff
	_ = DependencySharedHistory

	_ = BoundNone
	_ = BoundRecordSize
	_ = BoundInFlight
	_ = BoundBytes
	_ = BoundClosed
)

// The record production face and the versioned line keep their declared shapes:
// the three recorder methods, the line envelope fields and the three import
// chains each stay pinned by name and type.
var (
	_ Recorder = (Recorder)(nil)

	_ func(Recorder, context.Context, EpisodeRecord) error  = Recorder.RecordEpisode
	_ func(Recorder, context.Context, DecisionRecord) error = Recorder.RecordDecision
	_ func(Recorder, context.Context, AttemptRecord) error  = Recorder.RecordAttempt

	_ = Line{
		Schema:   "",
		Kind:     "",
		Episode:  nil,
		Decision: nil,
		Attempt:  nil,
	}

	_ = LegacyRecord{
		Schema:        "",
		Line:          0,
		Fields:        map[string]any{},
		UnknownFields: []string{},
		Lineage:       LineageUnknown,
		Raw:           "",
	}

	_ = ImportGap{
		Line:   0,
		Reason: "",
		Detail: "",
		Raw:    "",
		Err:    nil,
	}

	_ = ImportResult{
		Records: []Line{},
		Legacy:  []LegacyRecord{},
		Unknown: []ImportGap{},
	}
)

// The adapter of the execution loop's narrow recording face keeps its declared
// shape: the constructor over this package's record pipeline and the three
// face entries it satisfies, each crossing built-in, event and model types only.
var (
	_ func(Recorder) RecorderAdapter = NewRecorderAdapter

	_ func(RecorderAdapter, context.Context, event.EpisodeID, event.SessionID, time.Time) error = RecorderAdapter.RecordEpisode

	_ func(RecorderAdapter, context.Context, event.DecisionID, event.InvocationID, event.EpisodeID, []event.EventID, string, string, []string, time.Time) error = RecorderAdapter.RecordDecision

	_ func(RecorderAdapter, context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) error = RecorderAdapter.RecordAttempt
)

// The schema versions and record kinds are compile-time constants, so the
// versioned records can never lose their version to a computed value.
const (
	_ = SchemaCurrent
	_ = SchemaLegacyV2
	_ = SchemaLegacyV1
	_ = SchemaUnknown

	_ = RecordKindEpisode
	_ = RecordKindDecision
	_ = RecordKindAttempt
)

// The named line errors stay errors, so a reader can route a refused line by
// name instead of matching on message text.
var (
	_ error = ErrSchemaMissing
	_ error = ErrSchemaUnknown
	_ error = ErrLegacySchemaLine
	_ error = ErrSchemaMisuse
	_ error = ErrLineShape
)

// The safe request snapshot production face keeps its declared shape: the
// snapshot fields, the projection policy, the projection functions and the
// named errors each stay pinned by name and type.
var (
	_ = RequestSnapshot{
		Model:            "",
		ModelVersion:     "",
		PromptVersion:    "",
		VisibleTools:     []VersionedName{},
		VisibleSkills:    []VersionedName{},
		Parameters:       json.RawMessage{},
		StructuredOutput: json.RawMessage{},
		Messages:         []model.Message{},
		ToolSchemas:      []json.RawMessage{},
		ExtraFields:      map[string]any{},
		Headers:          map[string]string{},
	}

	_ = SnapshotPolicy{
		ProjectExtraFields: nil,
		ProjectURL:         nil,
	}

	_ func(ModelViewRef, model.Request, SnapshotPolicy) (RequestSnapshot, error) = SnapshotRequest
	_ func([]model.Message, ...func(*model.Request)) (RequestSnapshot, error)    = SnapshotFromOptions
	_ func(string) string                                                        = SanitizeURL
	_ func(map[string]any) map[string]any                                        = RedactExtraFields
	_ func(RequestSnapshot) ([]byte, error)                                      = MarshalSnapshot
	_ func([]byte) (RequestSnapshot, error)                                      = ParseSnapshot
	_ func(RequestSnapshot, string, string) (SnapshotRef, error)                 = RequestSnapshot.Ref
	_ func(SnapshotRef, []byte) error                                            = VerifySnapshot

	_ func(AttemptRecord) error                                                             = ValidateAttempt
	_ func(event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) AttemptRecord = AttemptOf
)

// The redaction value is a compile-time constant and the named snapshot errors
// stay errors, so a projection never swaps in a computed placeholder and a
// refused snapshot is routed by name.
const _ = RedactedValue

var (
	_ error = ErrRebuildRefused
	_ error = ErrSnapshotUnreadable
	_ error = ErrSnapshotMismatch
	_ error = ErrLogprobsSourceMissing
)

// The bounded capture queue and its durable write point keep their declared
// shapes: the sink config, the loss ledger, the writer face and the queue and
// writer entries each stay pinned by name and type.
var (
	_ = SinkConfig{
		MaxRecordBytes: 0,
		MaxInFlight:    0,
		MaxBytes:       0,
	}

	_ = CaptureLedger{
		Admitted:          0,
		DroppedRecordSize: 0,
		DroppedInFlight:   0,
		DroppedBytes:      0,
		DroppedClosed:     0,
		WriteErrors:       0,
		Written:           0,
		InFlight:          0,
	}

	_ func(CaptureLedger) int64 = CaptureLedger.Dropped

	_ CaptureWriter = (CaptureWriter)(nil)
	_ CaptureWriter = (*FileCaptureWriter)(nil)

	_ func(CaptureWriter, string, []byte) error                   = CaptureWriter.Append
	_ func(CaptureWriter) ([]CaptureFileDigest, WriterSyncResult) = CaptureWriter.Seal
	_ func(CaptureWriter, CaptureManifest) error                  = CaptureWriter.Commit

	_ func(string, SinkConfig, CaptureWriter) (*CaptureSink, error)                                                                    = NewCaptureSink
	_ func(*CaptureSink, context.Context, AttemptRecord) (CaptureAdmission, error)                                                     = (*CaptureSink).Record
	_ func(*CaptureSink, context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) (CaptureAdmission, error) = (*CaptureSink).RecordOutcome
	_ func(*CaptureSink, context.Context, EpisodeRecord) error                                                                         = (*CaptureSink).RecordEpisode
	_ func(*CaptureSink, context.Context, DecisionRecord) error                                                                        = (*CaptureSink).RecordDecision
	_ func(*CaptureSink, context.Context, AttemptRecord) error                                                                         = (*CaptureSink).RecordAttempt
	_ func(*CaptureSink) CaptureLedger                                                                                                 = (*CaptureSink).Ledger
	_ func(*CaptureSink, context.Context) (CaptureManifest, error)                                                                     = (*CaptureSink).Seal

	_ Recorder = (*CaptureSink)(nil)

	_ func(string, int) (*FileCaptureWriter, error) = NewFileCaptureWriter
	_ func(string) (CaptureManifest, error)         = ReadCaptureSeal
)

// The default capture bounds stay compile-time constants, so a queue is never
// built over a computed or swapped-in bound.
const (
	_ = DefaultMaxRecordBytes
	_ = DefaultMaxInFlight
	_ = DefaultMaxBytes
	_ = SinkRecordsFile
	_ = SealFileName
	_ = MaxOpenFilesCap
	_ = DefaultMaxOpenFiles
)

// The named queue and writer errors stay errors, so a refused record, writer
// write or seal is routed by name instead of matched on message text.
var (
	_ error = ErrNegativeBound
	_ error = ErrCaptureIDMissing
	_ error = ErrWriterMissing
	_ error = ErrWriterSealed
	_ error = ErrWriterUnsealed
	_ error = ErrWriterFileName
	_ error = ErrWriterOpenBound
	_ error = ErrCaptureRefused
)

// The feedback and reward accounts keep their declared shapes: the raw
// feedback record, its ledger face, the derivation rule and the derived reward
// ledger each stay pinned by name and type, so the two accounts never merge
// into one.
var (
	_ = FeedbackRecord{
		Schema:       "",
		FeedbackID:   "",
		EventKey:     "",
		DecisionID:   "",
		AttemptID:    "",
		ModelVersion: "",
		Conclusion:   "",
		Source:       "",
		Version:      "",
		RecordedAt:   time.Time{},
	}

	_ func(*FeedbackLedger, FeedbackRecord) error    = (*FeedbackLedger).Add
	_ func(*FeedbackLedger, string) []FeedbackRecord = (*FeedbackLedger).BoundTo
	_ func(*FeedbackLedger) []FeedbackRecord         = (*FeedbackLedger).All

	_ = RewardRule{
		Version: "",
		Value:   nil,
	}

	_ func(*RewardLedger, event.DecisionID, event.AttemptID, RewardRule, []FeedbackRecord) (RewardRecord, error) = (*RewardLedger).Derive
	_ func(*RewardLedger, event.DecisionID, event.AttemptID) (RewardRecord, bool)                                = (*RewardLedger).Latest
	_ func(*RewardLedger, event.DecisionID, event.AttemptID) []RewardRecord                                      = (*RewardLedger).Records
	_ func(*RewardLedger, event.DecisionID, event.AttemptID) RewardState                                         = (*RewardLedger).StateOf
	_ func(*RewardLedger, RewardRecord) error                                                                    = (*RewardLedger).Restore
)

// The feedback face keeps the zero-selection construction: the evidence, the two
// accounts and the derivation rule are positional and the credential and request
// bounds are named by the option that adds them, so the face carries no
// configuration value to choose from.
var (
	_ func(FeedbackTargetResolver, *FeedbackLedger, *RewardLedger, RewardRule, ...FeedbackEndpointOption) (*FeedbackEndpoint, error) = NewFeedbackEndpoint

	_ func(string) FeedbackEndpointOption = WithFeedbackToken

	_ func(RequestLimits) FeedbackEndpointOption = WithFeedbackLimits

	_ = []FeedbackEndpointOption{WithFeedbackToken(""), WithFeedbackLimits(RequestLimits{})}
)

// The named feedback and reward errors stay errors, so a refused record,
// derivation or restore is routed by name instead of matched on message text.
var (
	_ error = ErrFeedbackIDMissing
	_ error = ErrFeedbackKeyMissing
	_ error = ErrFeedbackVersionMissing
	_ error = ErrFeedbackConflict
	_ error = ErrRewardNoFeedback
	_ error = ErrRewardRuleMissing
	_ error = ErrRewardConflict
	_ error = ErrRewardRecordInvalid
)

// The authorized export keeps its declared shapes: the paged read seam, the
// fact and sample line it produces and the exporter that runs it each stay
// pinned by name and type, so a widened grant or a renamed field stops the
// package from compiling.
var (
	_ = ExportedFact{
		EventKey:   "",
		Partition:  "",
		CallID:     "",
		CaptureID:  "",
		EpisodeID:  "",
		DecisionID: "",
		AttemptID:  "",
		Schema:     "",
		ParentKey:  "",
		Timestamp:  0,
		Body:       nil,
	}

	_ ExportSource                                                                  = (ExportSource)(nil)
	_ func(ExportSource, context.Context, string, int, int) ([]ExportedFact, error) = ExportSource.Page
	_ func(ExportSource, context.Context) ([]EpisodeRecord, error)                  = ExportSource.Episodes

	_ = SampleLine{
		Schema:     "",
		Provenance: SampleProvenance{},
		Fact:       ExportedFact{},
		Feedback:   []FeedbackRecord{},
		Reward:     RewardRecord{},
	}

	_ Exporter = (*TrainingExporter)(nil)

	_ func(ExportSource, *FeedbackLedger, *RewardLedger, RewardRule) *TrainingExporter = NewTrainingExporter
	_ func(*TrainingExporter, string, []SampleLine) error                              = (*TrainingExporter).SealSnapshot
	_ func(*TrainingExporter, string) ([]SampleLine, bool)                             = (*TrainingExporter).Snapshot
	_ func(*TrainingExporter) []string                                                 = (*TrainingExporter).SnapshotIDs
)

// The named export errors stay errors and the page limits stay constants, so a
// run refused or cut short is routed by name and a page limit is never clamped
// into a silently different size.
var (
	_ error = ErrExportUnauthorized
	_ error = ErrExportPageLimit
	_ error = ErrExportRepeatedPage
	_ error = ErrExportReadFailed
	_ error = ErrExportConflict
	_ error = ErrExportWriteFailed
)

const (
	_ = DefaultExportPageLimit
	_ = MaxExportPageLimit
)

// The replacement lease keeps its declared shapes: the identity an attempt is
// attributed to, the replacement record, the state pair and the lease and
// handle faces each stay pinned by name and type, so a renamed field or a
// handle that stops being a model stops the package from compiling.
var (
	_ = ModelIdentity{
		Model:   "",
		Version: "",
	}

	_ func(ModelIdentity) string                     = ModelIdentity.Name
	_ func(string) ModelIdentity                     = ParseModelIdentity
	_ func(model.Model, string) (*ModelLease, error) = NewModelLease

	_ = ReplacementRecord{
		Schema: "",
		From:   "",
		To:     "",
		State:  ReplacementRefused,
		Reason: "",
		At:     time.Time{},
	}

	_ model.Model     = (*VersionedModel)(nil)
	_ model.IterModel = (*VersionedModel)(nil)

	_ func(*ModelLease) *VersionedModel                                                                                                          = (*ModelLease).Current
	_ func(*ModelLease) string                                                                                                                   = (*ModelLease).CurrentVersion
	_ func(*ModelLease) int64                                                                                                                    = (*ModelLease).InFlight
	_ func(*ModelLease) []ReplacementRecord                                                                                                      = (*ModelLease).Replacements
	_ func(*ModelLease) error                                                                                                                    = (*ModelLease).Close
	_ func(*ModelLease, []ReplacementRecord) error                                                                                               = (*ModelLease).Restore
	_ func(*ModelLease, context.Context, string, func() (model.Model, error), func(ModelIdentity, model.Model) error) (ReplacementRecord, error) = (*ModelLease).Replace
)

// The replacement states stay compile-time constants and the named lease errors
// stay errors, so a refused replacement is never read as an applied one and a
// refusal is routed by name rather than matched on message text.
const (
	_ = ReplacementRefused
	_ = ReplacementApplied
)

var (
	_ error = ErrLeaseModelMissing
	_ error = ErrLeaseVersionMissing
	_ error = ErrLeaseVersionHeld
	_ error = ErrLeaseRebuildMissing
	_ error = ErrLeaseRebuildFailed
	_ error = ErrLeasePublishMissing
	_ error = ErrLeasePublishFailed
	_ error = ErrLeaseVersionClosed
	_ error = ErrLeaseClosed
	_ error = ErrLeaseHistoryInvalid
	_ error = ErrLeaseStreamMissing
	_ error = ErrLeaseRestoreMismatch
	_ error = ErrRewardVersionConflict
)
