package rl // import "github.com/SpellingDragon/tagent/rl"

Package rl is the training observation surface of the runtime: it declares the
versioned records a model request is remembered by, the episode and dependency
grouping those records are split on, and the capture port that fixes a request
view before the request starts.

The package is data and ports only. Record identities come from the event
package, the observed response is the native model response, and no type here
derives an identity, a lineage or a redaction on its own.

  - record vocabulary: the versioned schemas, the line envelope and the legacy
    import (record, line, import)
  - capture: the pre-request snapshot port, its redaction policy and the sealed
    sink (capture, sink, writer)
  - production face: the recording port the execution loop attaches (produce)
  - feedback and reward: the two ledgers, the feedback binding and the delivery
    client (feedback, feedback_endpoint, feedback_resolver, http, reward,
    reward_ledger, replacement)
  - export: the exporter over a sealed capture and its provenance manifest
    (export, export_source, manifest)
  - sft: the cleaning, artifact and consumption faces (sft, sft_convert,
    sft_artifact, sft_consumer, sft_qualification)
  - record vocabulary versus event identity: a record's identity is the event
    package's EpisodeID, DecisionID or AttemptID, no second identity is declared
    here, and a record type is never read as an event type
  - shape pins: the compile-time assertions pin every declared shape and carry
    no logic of their own

CONSTANTS

const (
	// DefaultExportPageLimit is the per-partition page size a request that
	// names no limit is read with.
	DefaultExportPageLimit = 200
	// MaxExportPageLimit is the largest per-partition page size supported.
	MaxExportPageLimit = 1000
)
    DefaultExportPageLimit and MaxExportPageLimit bound the page size a request
    is read with: a request that names no page limit takes the default, and a
    limit outside the supported range is refused by name rather than clamped
    into it.

const (
	// FeedbackVersionCurrent is the feedback record version this face writes.
	FeedbackVersionCurrent = "feedback.v1"
	// FeedbackSourceAPI names the source of a conclusion that arrived over the RL
	// HTTP face.
	FeedbackSourceAPI = "api"
)
    FeedbackVersionCurrent and FeedbackSourceAPI pin the version every record of
    this face is written at and the source of a conclusion that arrived over the
    wire.

const (
	// DefaultMaxBodyBytes bounds a request body that names no bound.
	DefaultMaxBodyBytes int64 = 1 << 20
	// DefaultMaxResponseBytes bounds a response body that names no bound.
	DefaultMaxResponseBytes int64 = 4 << 20
)
    DefaultMaxBodyBytes and DefaultMaxResponseBytes bound a request or response
    body that names no bound: a zero bound is enforced under these defaults,
    so no face in front of the model runs beside an unbounded body.

const (
	// ImportReasonParseError is a line that is not valid JSON.
	ImportReasonParseError = "parse_error"
	// ImportReasonUnknownSchema is a line whose declared schema is not the
	// current record schema and is never read under it.
	ImportReasonUnknownSchema = "unknown_schema"
	// ImportReasonUnknownSchemaVersion is a legacy line whose schema_version is
	// not a number or lies beyond every known version; it is never guessed at.
	ImportReasonUnknownSchemaVersion = "unknown_schema_version"
	// ImportReasonUnclassified is a line matching no known legacy schema.
	ImportReasonUnclassified = "unclassified_row"
	// ImportReasonSchemaMisuse is a line declaring the current schema over a
	// legacy capture shape.
	ImportReasonSchemaMisuse = "schema_misuse"
	// ImportReasonCurrentLineRejected is a current-schema line the parse path
	// refused.
	ImportReasonCurrentLineRejected = "current_line_rejected"
)
    ImportReasonParseError and the other gap reasons: each names one way a line
    became a gap instead of a record.

const (
	// DefaultMaxRecordBytes is the per-record byte bound one sink takes when its
	// config names none: a marshaled record line above it is refused as a
	// counted loss.
	DefaultMaxRecordBytes int64 = 1 << 20
	// DefaultMaxInFlight is the in-flight count bound one sink takes when its
	// config names none: admitted records not yet accounted by the writer stay
	// below it, so the queue never grows past it.
	DefaultMaxInFlight int64 = 4096
	// DefaultMaxBytes is the byte budget one sink takes when its config names
	// none: admitted line bytes at or above it refuse every further record, and
	// the budget is never met by removing or rotating stored history.
	DefaultMaxBytes int64 = 512 << 20
)
    DefaultMaxRecordBytes and the other default admission bounds of one capture
    sink: a config that names no bound takes these, and a negative bound is
    refused rather than read as unlimited.

const DefaultMaxOpenFiles = MaxOpenFilesCap
    DefaultMaxOpenFiles is the open file bound one writer takes when its
    construction names none.

const DefaultSFTTrainRatio = 0.9
    DefaultSFTTrainRatio is the share of causal groups a cleaning run assigns to
    the training side when the request names no ratio.

const MaxOpenFilesCap = 16
    MaxOpenFilesCap is the hard cap on the files one writer holds open: a bound
    above the cap is clamped to the cap rather than obeyed.

const RLTokenEnv = "TAGENT_RL_AUTH_TOKEN"
    RLTokenEnv names the environment entry the RL request token is read from.

const RedactedValue = "[redacted]"
    RedactedValue is what every credential carrier is projected to: a header
    name, a query key or a field name stays readable while the credential itself
    is never persisted.

    - Safe snapshot face: one snapshot is fixed from the request before the
    request starts, projected so no header, cookie or URL credential reaches
    a persisted record. - A finished call's options never rebuild a request
    snapshot, so the snapshot records what the request boundary carried and
    replays nothing.

const SFTLabelIgnoreIndex = -100
    SFTLabelIgnoreIndex is the label a loss never reads: every position no loss
    trains on carries it.

const SFTLegacyConverterV2 = "scripts/convert_trajectories.py"
    SFTLegacyConverterV2 names the strict v2 converter that stays a read-only
    import: a sample written under the v2 record schema is routed to it and is
    never read as an SFT example of the current record schema.

const SFTScaffoldToken = -1
    SFTScaffoldToken attributes a token to no message: a token the chat template
    emitted around the messages.

const SealFileName = "seal.json"
    SealFileName is the file one writer persists its confirmed seal under.

    - The durable write point of one capture: the single writer the queue drains
    into, the seal that writer confirms and the capture state a restarted
    reader reads back. - The writer reports what reached storage and never
    what was merely handed to it: a file digest covers the bytes on disk,
    a sync result is the writer's own, and an existing file is never deleted or
    rotated to make room for new data. - A capture without a writer seal is read
    back partial with its reason, and the counts of such a capture are unknown
    rather than zero: missing evidence is never reported as an intact account.

const SinkRecordsFile = "attempts.jsonl"
    SinkRecordsFile is the file name the sink writes its record lines under.

    - The bounded admission queue of one capture: a record is admitted
    under three bounds (per-record size, in-flight count, byte budget),
    and a record a bound refuses is counted in the loss ledger under that bound
    instead of being dropped silently. - Admission never waits on the writer,
    and the ledger is read from counters rather than from the data queue,
    so a full queue never hides its own loss account. - Sending and closing
    share one protocol: the seal barrier enters the same queue every record
    line enters, so a seal accounts exactly what was admitted before it.
    - The terminal of an attempt record is the Kind the model reduction derived,
    shared through the single observation face and never re-derived here;
    a capture that ends without a writer seal is accounted partial with its

VARIABLES

var (
	// ErrRebuildRefused reports an attempt to rebuild a request snapshot by
	// replaying the options of a finished call. The snapshot is fixed before the
	// request starts and is never reconstructed afterwards.
	ErrRebuildRefused = errors.New("rl: a request snapshot is never rebuilt from the options of a finished call")
	// ErrSnapshotUnreadable reports a snapshot reference that cannot read its
	// content back: an empty reference is not a snapshot reference.
	ErrSnapshotUnreadable = errors.New("rl: the snapshot reference cannot read its content back")
	// ErrSnapshotMismatch reports snapshot content that does not recheck against
	// the digest of the reference it is read back under.
	ErrSnapshotMismatch = errors.New("rl: the snapshot content does not match its reference digest")
)
    ErrRebuildRefused and the other named errors of the snapshot production
    face: each names the way a safe snapshot or its reference was refused,
    so a caller can route it by name.

var (
	// ErrExportUnauthorized reports a request that authorizes no partition:
	// authorization runs before any read and an empty grant is a refusal,
	// never a whole-store scan.
	ErrExportUnauthorized = errors.New("rl: the export request authorizes no partition")
	// ErrExportPageLimit reports a page limit outside the supported range: a
	// limit is refused by name rather than clamped into the range.
	ErrExportPageLimit = errors.New("rl: the export page limit is outside the supported range")
	// ErrExportRepeatedPage reports a page that named no event the run had not
	// already seen: the read cannot advance and is stopped at that partition,
	// and a repeated page is never read as the end of the partition.
	ErrExportRepeatedPage = errors.New("rl: the export page repeated already-seen events and did not advance")
	// ErrExportReadFailed reports a source read that failed. What the run had
	// already read is returned as a partial result beside this error.
	ErrExportReadFailed = errors.New("rl: the export source read failed")
	// ErrExportConflict reports a snapshot seal that would overwrite an
	// existing snapshot: snapshots are created exclusively and a concurrent
	// run writes beside an earlier one, never over it.
	ErrExportConflict = errors.New("rl: the export snapshot already exists")
	// ErrExportWriteFailed reports a snapshot that could not be written.
	ErrExportWriteFailed = errors.New("rl: the export snapshot write failed")
)
    ErrExportUnauthorized and the export errors beside it name the way a run was
    refused or cut short, so a caller routes it by name and reads the partial
    result beside it.

var (
	// ErrExportCaptureUnsealed reports a source over a capture no writer
	// confirmed: without a seal there are no accounted bytes to hold an export
	// to, and the unsealed partial state is never read as a capture.
	ErrExportCaptureUnsealed = errors.New("rl: the capture holds no writer-confirmed seal to export from")
	// ErrExportCaptureMismatch reports capture bytes that do not recheck
	// against the seal that accounts them: bytes the seal never accounted and
	// accounted bytes no file holds are both this refusal.
	ErrExportCaptureMismatch = errors.New("rl: the capture bytes do not recheck against the seal that accounts them")
	// ErrExportCaptureUnreadable reports a stored record line the export source
	// cannot read back. The read stops at that line and reports it, so a line
	// outside the record schema is never guessed at nor silently dropped.
	ErrExportCaptureUnreadable = errors.New("rl: the capture record file holds a line the export source cannot read")
	// ErrExportPageOffset reports a page offset outside the partition: an
	// offset is refused by name rather than read from an invented position.
	ErrExportPageOffset = errors.New("rl: the export page offset is outside the partition")
)
    ErrExportCaptureUnsealed, ErrExportCaptureMismatch,
    ErrExportCaptureUnreadable and ErrExportPageOffset name the ways the
    capture-backed export source refused a read, so a caller routes each by
    name.

var (
	// ErrFeedbackIDMissing reports a feedback record that carries no identity:
	// a reward derived from it could never name its source again.
	ErrFeedbackIDMissing = errors.New("rl: the feedback record carries no feedback id")
	// ErrFeedbackKeyMissing reports a feedback record bound to no event key: a
	// binding is never inherited from the most recent parent call.
	ErrFeedbackKeyMissing = errors.New("rl: the feedback record carries no event key")
	// ErrFeedbackVersionMissing reports a feedback record that names no
	// version: a source is cited at the version it was read at, never without
	// one.
	ErrFeedbackVersionMissing = errors.New("rl: the feedback record carries no version")
	// ErrFeedbackConflict reports a second record under one feedback id that
	// does not match the first: a repeat of the same record is idempotent and
	// different content under one identity is a conflict, reported rather than
	// overwritten.
	ErrFeedbackConflict = errors.New("rl: the feedback id is already bound to a different record")
)
    ErrFeedbackIDMissing and the feedback errors beside it name the way a
    feedback record was refused, so a caller routes it by name.

var (
	// ErrFeedbackFaceClosed reports a request to a face beside no configured token:
	// the endpoint stays closed until a credential is configured for it.
	ErrFeedbackFaceClosed = errors.New("rl: the feedback endpoint is closed: no request token is configured")
	// ErrFeedbackNotAuthenticated reports a request whose subject did not
	// authenticate: the credential is missing, malformed or unmatched.
	ErrFeedbackNotAuthenticated = errors.New("rl: the feedback request is not authenticated")
	// ErrFeedbackMethod reports a request on a method other than POST.
	ErrFeedbackMethod = errors.New("rl: the feedback endpoint accepts POST only")
	// ErrFeedbackMalformed reports a body the endpoint cannot read as one payload.
	ErrFeedbackMalformed = errors.New("rl: the feedback body does not parse as one payload")
	// ErrFeedbackConclusionMissing reports a submission carrying no conclusion: a
	// conclusion is the evidence a reward is derived from and is never invented.
	ErrFeedbackConclusionMissing = errors.New("rl: the feedback submission carries no conclusion")
	// ErrFeedbackEventUnknown reports an event key the recorded evidence does not
	// hold: the submission is refused rather than bound to a guessed target.
	ErrFeedbackEventUnknown = errors.New("rl: no recorded event carries the feedback event key")
	// ErrFeedbackTargetAmbiguous reports evidence that resolves to conflicting
	// decisions or attempts, or a named target the evidence does not confirm.
	ErrFeedbackTargetAmbiguous = errors.New("rl: the feedback evidence resolves to conflicting decisions or attempts")
	// ErrFeedbackRecordless reports a face built beside no feedback or reward
	// account: a submission has nowhere to be written and nowhere to be reduced.
	ErrFeedbackRecordless = errors.New("rl: the feedback face carries no feedback or reward account")
)
    ErrFeedbackFaceClosed and the feedback endpoint errors beside it name the
    way a submission was refused, so a caller routes it by name rather than
    reading a status code alone.

var (
	// ErrRequestLimitsInvalid reports a bound that is negative: a negative bound
	// turns a limit off and is a configuration error, never a valid request.
	ErrRequestLimitsInvalid = errors.New("rl: a request bound is negative")
	// ErrBodyTooLarge reports a body read that passed the byte bound it was read
	// under. The read stops at the bound, so the exact size is not known and the
	// reason names the bound instead.
	ErrBodyTooLarge = errors.New("rl: the body passed the byte bound it was read under")
	// ErrEndpointURLInvalid reports a destination that does not parse as a URL.
	ErrEndpointURLInvalid = errors.New("rl: the endpoint URL does not parse")
	// ErrEndpointScheme reports a destination whose scheme is neither http nor
	// https, or is outside the declared scheme list.
	ErrEndpointScheme = errors.New("rl: the endpoint URL scheme is not allowed")
	// ErrEndpointUserInfo reports a destination carrying a credential in its URL.
	ErrEndpointUserInfo = errors.New("rl: the endpoint URL carries userinfo")
	// ErrEndpointFragment reports a destination carrying a fragment.
	ErrEndpointFragment = errors.New("rl: the endpoint URL carries a fragment")
	// ErrEndpointHost reports a destination whose host is not on the allowlist.
	ErrEndpointHost = errors.New("rl: the endpoint host is not on the allowlist")
	// ErrEndpointRedirect reports a redirect hop whose destination the allowlist
	// refuses: the hop is not followed and the destination receives no request.
	ErrEndpointRedirect = errors.New("rl: the endpoint redirect hop is not allowed")
	// ErrEndpointRedirectLimit reports a redirect chain longer than the declared
	// hop count.
	ErrEndpointRedirectLimit = errors.New("rl: the endpoint redirect chain exceeds the allowed hop count")
	// ErrEndpointStatus reports a destination that answered a status outside the
	// success range.
	ErrEndpointStatus = errors.New("rl: the endpoint answered a non-success status")
	// ErrInsecureListenAddr reports a listen address outside loopback beside no
	// request token: the face stays closed until a token, a loopback address or
	// the explicit unsafe switch is chosen.
	ErrInsecureListenAddr = errors.New("rl: refusing a non-loopback listen address without a request token (set " + RLTokenEnv + ", bind loopback, or pass the unsafe switch)")
)
    ErrRequestLimitsInvalid and the errors beside it name how a request bound,
    an endpoint allowlist or a listen address was refused, so a caller routes it
    by name.

var (
	// ErrSchemaMissing reports a line that carries no schema version at all.
	ErrSchemaMissing = errors.New("rl: the record line carries no schema version")
	// ErrSchemaUnknown reports a line whose schema version this package does not
	// recognise; the line is never read under a known version.
	ErrSchemaUnknown = errors.New("rl: the record line carries an unknown schema version")
	// ErrLegacySchemaLine reports a legacy line offered to the current parse
	// path; the legacy import is its only read path.
	ErrLegacySchemaLine = errors.New("rl: the record line carries a legacy schema")
	// ErrSchemaMisuse reports a line that declares the current schema while
	// carrying legacy capture fields.
	ErrSchemaMisuse = errors.New("rl: the record line declares the current schema over a legacy capture shape")
	// ErrLineShape reports a line whose kind and payload do not agree.
	ErrLineShape = errors.New("rl: the record line kind and payload do not agree")
)
    ErrSchemaMissing and the other named errors of the line parse path: each
    names the way a line was refused, so a caller can route the line rather than
    guess at it.

var (
	// ErrLeaseModelMissing reports a lease built beside no model: a version in
	// effect is never invented.
	ErrLeaseModelMissing = errors.New("rl: the model lease was built beside no model")
	// ErrLeaseVersionMissing reports a replacement that names no version: an
	// unidentified model is never installed as the version in effect.
	ErrLeaseVersionMissing = errors.New("rl: the model replacement names no version")
	// ErrLeaseVersionHeld reports a replacement naming the version already in
	// effect: nothing is rebuilt and nothing is published.
	ErrLeaseVersionHeld = errors.New("rl: the model version is already the version in effect")
	// ErrLeaseRebuildMissing reports a replacement carrying no rebuild step: no
	// model is invented for it.
	ErrLeaseRebuildMissing = errors.New("rl: the model replacement carries no rebuild")
	// ErrLeaseRebuildFailed reports a rebuild that failed or yielded no model.
	// No publication runs and the version in effect is untouched.
	ErrLeaseRebuildFailed = errors.New("rl: the model rebuild failed and no binding was published")
	// ErrLeasePublishMissing reports a replacement carrying no publication
	// entry: a rebuilt model is never installed inside the lease alone.
	ErrLeasePublishMissing = errors.New("rl: the model replacement carries no publication entry")
	// ErrLeasePublishFailed reports a publication that refused the rebuilt
	// model. Nothing took effect and the version in effect is untouched.
	ErrLeasePublishFailed = errors.New("rl: the rebuilt model was not published and no binding took effect")
	// ErrLeaseVersionClosed reports a request that reached a version already
	// retired and closed. The refusal is named rather than served from a
	// released model.
	ErrLeaseVersionClosed = errors.New("rl: the model version was retired and closed")
	// ErrLeaseClosed reports a replacement offered to a closed lease: a closed
	// lease installs no further version.
	ErrLeaseClosed = errors.New("rl: the model lease is closed")
	// ErrLeaseHistoryInvalid reports a replacement record that cannot stand on
	// its own: no version to name the attempt by.
	ErrLeaseHistoryInvalid = errors.New("rl: the replacement record cannot stand on its own")
	// ErrLeaseStreamMissing reports a version that handed over no stream at all:
	// an absent stream is named rather than read as a completed one.
	ErrLeaseStreamMissing = errors.New("rl: the model version handed over no stream")
	// ErrLeaseRestoreMismatch reports a restored replacement history whose last
	// applied version is not the version the lease holds: a restarted lease is
	// never read across versions.
	ErrLeaseRestoreMismatch = errors.New("rl: the restored replacement history names a version the lease does not hold")
)
    ErrLeaseModelMissing and the lease errors beside it name the way a lease
    or a replacement was refused, so a caller routes each by name instead of
    matching on message text.

var (
	// ErrRewardNoFeedback reports a derivation that was handed no feedback
	// evidence. The record it accompanies is recorded as unrated and carries
	// no value: a reward is never invented from a task terminal, so the
	// refusal and the unrated mark are both observable.
	ErrRewardNoFeedback = errors.New("rl: the reward derivation carries no feedback evidence")
	// ErrRewardRuleMissing reports a derivation whose rule names no version or
	// carries no value function: a value computed without a rule could never
	// be re-read under the version it claims.
	ErrRewardRuleMissing = errors.New("rl: the reward rule names no version or carries no value function")
	// ErrRewardConflict reports a restore of a record under an instance ID the
	// account already holds with different content: a repeat of the same
	// record is idempotent and different content under one identity is a
	// conflict, reported rather than overwritten.
	ErrRewardConflict = errors.New("rl: the reward instance is already recorded with different content")
	// ErrRewardRecordInvalid reports a restore of a record that cannot stand
	// on its own: no decision to derive for, or no schema to read it under.
	ErrRewardRecordInvalid = errors.New("rl: the reward record cannot stand on its own")
	// ErrRewardVersionConflict reports evidence naming two different model
	// versions for one decision and attempt. A value derived across versions is
	// attributed to neither, so the derivation is refused by name rather than
	// stamped with one of them.
	ErrRewardVersionConflict = errors.New("rl: the reward evidence crosses two model versions")
)
    ErrRewardNoFeedback and the reward errors beside it name the way a
    derivation or a restore was refused, so a caller routes it by name.

var (
	// ErrNegativeBound reports a negative bound offered to a sink or a writer: a
	// negative bound is refused rather than read as unlimited.
	ErrNegativeBound = errors.New("rl: the capture bound is negative")
	// ErrCaptureIDMissing reports a sink built without a capture id: the
	// manifest it seals accounts the capture it belongs to.
	ErrCaptureIDMissing = errors.New("rl: the capture sink was built without a capture id")
	// ErrWriterMissing reports a sink built without a writer: the queue drains
	// into one durable write point and never accumulates without one.
	ErrWriterMissing = errors.New("rl: the capture sink was built without a writer")
)
    ErrNegativeBound and the other named errors of the bounded capture queue:
    each names the way a sink was refused at construction, so a caller routes it
    by name.

var (
	// ErrWriterSealed reports an append offered to a writer that already sealed.
	ErrWriterSealed = errors.New("rl: the capture writer already sealed")
	// ErrWriterUnsealed reports a seal commit offered to a writer that has not
	// sealed yet: the persisted seal states what the writer confirmed, and there
	// is nothing confirmed to persist before the seal.
	ErrWriterUnsealed = errors.New("rl: the capture writer was offered a seal commit before it sealed")
	// ErrWriterFileName reports a file name that is not a plain file name: the
	// writer persists inside its own directory and refuses any name that would
	// reach out of it.
	ErrWriterFileName = errors.New("rl: the capture writer file name is not a plain file name")
	// ErrWriterOpenBound reports a file the writer refuses to open because its
	// open file bound is held. The refusal is a write loss and is never met by
	// closing another file's history.
	ErrWriterOpenBound = errors.New("rl: the capture writer open file bound is reached")
)
    ErrWriterSealed and the other named errors of the durable write point: each
    names the way a write or a seal was refused, so a caller routes it by name.

var ErrCaptureRefused = errors.New("rl: the capture refused the record at admission")
    ErrCaptureRefused reports a record the capture refused at admission on the
    recorder face: the refusal is counted in the loss ledger under the bound
    that refused it and reported by name here, so a write the capture could not
    take reaches the caller's loss account instead of silence.

var ErrLogprobsSourceMissing = errors.New("rl: the logprobs unsupported state carries no declaration source")
    ErrLogprobsSourceMissing reports an attempt carrying a LogprobsUnsupported
    state with no declaration named: an unsupported state rests on a provider
    declaration and is never written or read without naming it.

FUNCTIONS

func AttemptError(outcome model.Outcome) string
    AttemptError reports the error text one observed attempt carries: the error
    of the retained body, or the first error the observation trace carries when
    the body is absent or carries none. It is empty for an attempt that observed
    no error, and it reports what was observed without judging a terminal.

func BuildSFTArtifact(example SFTExample, tokenizer SFTSourceTokenizer) (SFTArtifact, error)
    BuildSFTArtifact tokenizes one cleaned sample and stores its token planes:
    the mask side of every token follows the message it came from, the loss
    trains on every action token and the labels carry the ids a loss reads.

func CollateSFT(artifacts []SFTArtifact, paddingID, maxLength int) (SFTBatch, error)
    CollateSFT pads the token planes of one batch to the longest sample and
    keeps the unpadded length of every sample: a pad position carries the
    padding id, no attention, no loss and SFTLabelIgnoreIndex.

func ImportFile(path string) (ImportResult, error)
    ImportFile reads one JSONL source read-only and classifies each of its
    lines. The file is opened for reading only and is never modified, rewritten
    or replaced.

func ImportLines(source io.Reader) (ImportResult, error)
    ImportLines reads one JSONL source read-only and classifies each of its
    lines into the three chains of ImportResult. Blank lines are skipped,
    every other line lands in exactly one chain, and a line the importer could
    not read is an explicit gap rather than a silent drop. The source is never
    written to.

func MarshalLine(line Line) ([]byte, error)
    MarshalLine encodes one versioned record line. A line carrying no schema,
    an unknown schema or a kind and payload that do not agree is refused rather
    than written without its version.

    - An attempt line is written only in a shape that can be read back:
    LogprobsUnsupported without its declaration source is refused here, never
    persisted to be refused at the read side.

func MarshalSnapshot(snapshot RequestSnapshot) ([]byte, error)
    MarshalSnapshot encodes one safe snapshot for persistence.

func NewCaptureExportSource(dir string) (*CaptureExportSource, error)
    NewCaptureExportSource returns the export source over one capture directory.
    A directory whose writer never confirmed a seal is refused with
    ErrExportCaptureUnsealed: bytes beside an unsealed capture are never read as
    an export source.

func NewCaptureSink(id string, cfg SinkConfig, writer CaptureWriter) (*CaptureSink, error)
    NewCaptureSink returns the bounded admission queue of one capture, draining
    into writer. A missing capture id or writer is refused, and a negative bound
    is refused rather than read as unlimited: a zero bound takes its default.

func NewEndpointClient(allowlist EndpointAllowlist, limits RequestLimits) (*EndpointClient, error)
    NewEndpointClient returns the outbound client of one allowlisted endpoint.
    An allowlist naming no host admits no destination, and a negative bound is
    refused by name.

func NewFeedbackEndpoint(resolver FeedbackTargetResolver, feedback *FeedbackLedger, rewards *RewardLedger, rule RewardRule, opts ...FeedbackEndpointOption) (*FeedbackEndpoint, error)
    NewFeedbackEndpoint returns the feedback face over the given evidence and
    the two accounts, computing every reward under the given rule. The resolver,
    the two accounts and the rule are positional because the face cannot be
    built without them; the request credential and the request bounds are
    named by the option that adds them. An argument beside no resolver, no
    account or no derivation rule is refused by name before the face is built,
    and a negative request bound is refused as well.

func NewFileCaptureWriter(dir string, maxOpenFiles int) (*FileCaptureWriter, error)
    NewFileCaptureWriter returns the real writer rooted at dir. The open file
    bound is clamped to MaxOpenFilesCap: a bound above the cap is clamped rather
    than obeyed, a negative bound is refused rather than read as unlimited,
    and no bound opens a second write point beside this one.

func NewModelLease(initial model.Model, version string) (*ModelLease, error)
    NewModelLease returns the lease of one model reference, holding initial as
    the version named by version. An empty version is the unversioned model of a
    deployment that names no generation, and it is recorded unversioned rather
    than assumed to be a current version.

func ParseLine(data []byte) (Line, error)
    ParseLine decodes one versioned record line: it reads the declared schema
    first and selects the parse path from that value alone. A legacy line is
    refused with its migration route (the legacy import reads it), a line that
    declares the current schema over legacy capture fields is refused as a
    masquerade, and an unknown version is refused as unknown rather than read
    under a known one.

    - On the read side an attempt line carrying a declaration-free unsupported
    logprobs state is refused as well: the wire is never trusted to hold a shape
    the write side would have refused.

func ParseSnapshot(content []byte) (RequestSnapshot, error)
    ParseSnapshot decodes one safe snapshot from its persisted bytes.

func RLTokenFromEnv() string
    RLTokenFromEnv reads the RL request token the environment names, trimmed of
    the surrounding space a shell leaves. An absent or empty entry reads as no
    token.

func ReadCaptureSeal(dir string) (CaptureManifest, error)
    ReadCaptureSeal reads the state of one capture back from the writer's
    directory: the persisted writer seal when there is one, and the explicit
    partial state of a capture whose writer never sealed when there is not.
    A capture read back without a seal is partial with its reason and its counts
    reported unknown, never complete and never zero-loss.

func ReadSFTArtifacts(source io.Reader) ([]SFTArtifact, error)
    ReadSFTArtifacts reads stored artifacts, one JSON line per sample, and
    verifies every artifact as it is read.

func RedactExtraFields(fields map[string]any) map[string]any
    RedactExtraFields is the default safe projection of the provider extra
    fields: a credential-named key keeps its name and loses its value to
    RedactedValue, a string value carrying URL credentials is projected through
    SanitizeURL, and everything else keeps its value as it stood.

func SFTTokenSides(messageMask []SFTMask, messageOf []int) ([]SFTMask, error)
    SFTTokenSides maps every token to the mask side of the message it came from:
    a token attributed to a message carries that message's side and a token the
    template emitted around the messages carries the observation side.

func SanitizeURL(raw string) string
    SanitizeURL projects one URL carrier to its safe form: a URL that carries
    no credential keeps its bytes exactly as they stand, while a URL carrying a
    userinfo or a credential query value loses that credential to RedactedValue
    and keeps its readable structure. A URL that cannot be parsed is projected
    to empty rather than persisted as it stands.

func SnapshotFromOptions(messages []model.Message, opts ...func(*model.Request)) (RequestSnapshot, error)
    SnapshotFromOptions is the refused rebuild path: a request snapshot is fixed
    before the request starts and is never rebuilt by replaying the options of
    a finished call. The call is refused before any option runs and before any
    message is read, so the options of a finished call are never re-run here.

func SnapshotRequest(view ModelViewRef, req model.Request, policy SnapshotPolicy) (RequestSnapshot, error)
    SnapshotRequest fixes the safe snapshot of req as it stands before the
    request starts, under the projection policy. The request is observed and
    never edited: the projection runs over deep copies, so the caller's request
    keeps every original value including its credentials.

func SplitOf(groupID string, seed int64, trainRatio float64) string
    SplitOf assigns one causal group to a side deterministically: the group and
    the seed decide the side, so the same group lands on the same side on every
    run and no group crosses the train and test sides.

func ValidateAttempt(record AttemptRecord) error
    ValidateAttempt reports whether one attempt record may be written and read
    back. It holds exactly one rule: a LogprobsUnsupported state is valid only
    beside the declaration it rests on. A present state rests on the values the
    response returned and needs no source, and an unknown state is never forced
    into either of the other two.

func ValidateConversation(messages []model.Message) error

    ValidateConversation reports the first legality violation of one
    conversation: a known role, system messages only ahead of the conversation,
    a tool message only as the answer of a pending assistant tool call,
    an assistant message carrying content or a call, and an assistant action as
    the final message. A tool call opened by the final action is answered by the
    next request of the episode, so an outstanding call is legal only there.

func ValidateListenAddr(addr, token string, allowUnsafe bool) error
    ValidateListenAddr judges one listen address beside the token the face
    is configured with. A token admits every address; beside no token only
    loopback is admitted, unless the caller passes the explicit unsafe switch.
    The refusal names the three ways out.

func VerifySFTArtifact(artifact SFTArtifact) error
    VerifySFTArtifact reports the first violation of one stored artifact:
    its schema, its planes aligned to one length, one label per position,
    the loss positions exactly on the action side and a non-empty target.

func VerifySFTArtifactAgainst(artifact SFTArtifact, example SFTExample) error
    VerifySFTArtifactAgainst reports the first difference between one stored
    artifact and the cleaned sample it names: the bytes it was read off,
    the sample identity and the mask side of every message.

func VerifySnapshot(ref SnapshotRef, content []byte) error
    VerifySnapshot rechecks snapshot content against the reference it is read
    back under: an empty reference cannot read content back and is refused
    as unreadable, and content that does not match its digest is refused as a
    mismatch rather than read as the referenced snapshot.

TYPES

type AttemptRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// ID identifies the attempt.
	ID event.AttemptID `json:"attempt_id"`
	// DecisionID names the decision the attempt belongs to.
	DecisionID event.DecisionID `json:"decision_id"`
	// EpisodeID names the episode the attempt belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// ModelVersion is the version of the model that served the response, read
	// from the identity the serving version declares on it. It is empty when
	// the response names no version, which is recorded unversioned rather than
	// assumed to be the version in effect.
	ModelVersion string `json:"model_version,omitempty"`
	// Response is the native model response the attempt observed, kept as the
	// snapshot the attempt produced rather than a derived copy of it.
	Response model.Response `json:"response"`
	// Terminal is the single terminal the model reduction derived for the attempt;
	// the capture shares that terminal and derives none of its own.
	Terminal model.Kind `json:"terminal"`
	// Usage is the usage the reduction accumulated for the attempt. It is
	// recorded beside the response because the retained body may carry no usage
	// of its own while a usage tail does.
	Usage model.Usage `json:"usage"`
	// Error is the error text the attempt observed: the response error of the
	// observed body, or the error the trace carries when the body is absent. It
	// is empty for an attempt that observed no error.
	Error string `json:"error,omitempty"`
	// Logprobs states how the logprobs of the response are known.
	Logprobs LogprobsState `json:"logprobs"`
	// LogprobsSource names the provider declaration a LogprobsUnsupported state
	// rests on; it is required for that state and empty otherwise.
	LogprobsSource string `json:"logprobs_source,omitempty"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}
    AttemptRecord is one observable model attempt as data: the response it
    observed, the terminal it reached and the logprobs state of that response.

    A retry below the decision boundary is not a second attempt, and the
    response is the native response the attempt observed rather than a derived
    copy of it.

func AttemptOf(id event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) AttemptRecord
    AttemptOf is the single observation face of one observable model attempt:
    the record of what the reduction actually produced. The terminal is the Kind
    the model reduction derived — it is never re-derived here — the response
    is the native response body the attempt observed, and the logprobs state is
    read from that body through LogprobsStateOf so a value the response never
    returned is never reported as present.

    The model version is read from the identity the serving version declared on
    the response it handed over, so the version a record names is the one that
    actually served the attempt rather than the one in effect when the record
    was written. A response naming no identity is recorded unversioned.

func (r AttemptRecord) FactKey() string
    FactKey returns the attempt's identity as a fact key: the attempt ID the
    record is stored under. A retry hidden below the decision boundary is not a
    second attempt, so the key of one attempt stays the ID it was recorded with.

func (r AttemptRecord) FactType() string
    FactType returns the record kind an attempt line is read as; the record
    family keeps one frozen type per record kind.

type Capture interface {
	// Snapshot fixes the model, the generation parameters and the visible prompt,
	// skill and tool set of req before the request starts, and returns the
	// reference a decision records. A view is never reconstructed from the options
	// of a finished call.
	Snapshot(ctx context.Context, req model.Request) (ModelViewRef, error)

	// Record admits one observed attempt and reports the admission bound that
	// refused it when it is not admitted.
	Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error)

	// Seal closes the capture and returns the manifest a real writer confirmed. A
	// capture that still holds records in flight, or that no writer sealed, comes
	// back with Partial set.
	Seal(ctx context.Context) (CaptureManifest, error)
}

    Capture is the request-boundary observation port through which one model
    request is remembered: it fixes the view a request actually runs on,
    admits the attempts observed for it under a bound, and seals what it holds
    with a manifest.

    The redline list (4 rules): MUST fix the model, the generation parameters
    and the visible prompt, skill and tool set before the request starts.
    MUST keep a header, cookie or URL credential out of every persisted record.
    MUST NOT let admission wait on the caller or cross its bound. MUST NOT
    report a capture as complete while a record is in flight or no writer
    confirmed the seal.

type CaptureAdmission struct {
	// Admitted reports whether the record was admitted.
	Admitted bool
	// Bound names the bound that refused the record; it is BoundNone for an
	// admitted record.
	Bound CaptureBound
	// Reason states why the bound refused the record.
	Reason string
}
    CaptureAdmission states whether a capture admitted one record, and which
    bound refused it when it did not.

    An unadmitted record is a counted loss and not a silent one: the bound that
    refused it stays readable on the admission.

type CaptureBound string
    CaptureBound names the admission bound that refused a record.

const (
	// BoundNone marks an admitted record: no bound refused it.
	BoundNone CaptureBound = ""
	// BoundRecordSize marks a record that exceeded the per-record byte bound.
	BoundRecordSize CaptureBound = "record_size"
	// BoundInFlight marks a record refused because the in-flight count bound was
	// reached.
	BoundInFlight CaptureBound = "in_flight"
	// BoundBytes marks a record refused because the run byte budget was reached.
	BoundBytes CaptureBound = "bytes"
	// BoundClosed marks a record refused because the capture was already closed.
	BoundClosed CaptureBound = "closed"
)
type CaptureExportSource struct {
	// Has unexported fields.
}
    CaptureExportSource is the export source over one sealed capture directory:
    the record file the writer confirmed, read under the partition named by the
    capture identity. The source hands over exactly what the writer sealed and
    nothing beside it: the record file's bytes are rechecked against the seal
    that accounts them, and one stored line the read cannot classify stops the
    read by name rather than being skipped. The capture is one partition — the
    grant names the capture and every fact is rechecked under that name — and a
    partition the capture does not hold carries no facts rather than a widened
    read.

    Episode records are handed over as the episode account the split groups are
    drawn on, and every decision and attempt record becomes a fact whose body is
    the stored line itself byte for byte, so no stored line falls outside those
    two accounts and an exported sample traces back to the record file without a
    projection in between.

func (s *CaptureExportSource) CaptureID() string
    CaptureID reports the capture identity this source serves as its partition:
    the name a grant authorizes and every fact is rechecked against.

func (s *CaptureExportSource) Episodes(ctx context.Context) ([]EpisodeRecord, error)
    Episodes returns the episode records the capture holds: the account the
    leakage-free split groups are drawn on.

func (s *CaptureExportSource) Manifest() CaptureManifest
    Manifest reports the seal the source reads under, so a caller reconciles the
    export against the capture account that backs it.

func (s *CaptureExportSource) Page(ctx context.Context, partition string, offset, limit int) ([]ExportedFact, error)
    Page returns at most limit facts of one partition starting at the given
    offset, in stored order. A partition the capture does not hold carries no
    facts, and a page past the end is empty: the source never widens a named
    partition into a read of anything else.

type CaptureFeedbackResolver struct {
	// Has unexported fields.
}

    CaptureFeedbackResolver is the evidence seam of the feedback endpoint
    over one sealed capture's records: an event key is looked up in the stored
    decision and attempt records, and the identity the record carries is the
    binding the feedback is written under. A binding is never inherited from the
    most recent parent call.

func NewCaptureFeedbackResolver(source *CaptureExportSource) *CaptureFeedbackResolver
    NewCaptureFeedbackResolver returns the resolver over one capture source.
    The source reads under the seal the writer confirmed, so attribution rests
    on the records that reached storage and never on bytes beside them.

func (r *CaptureFeedbackResolver) ResolveFeedbackTarget(ctx context.Context, eventKey string) (FeedbackTarget, error)
    ResolveFeedbackTarget returns the decision, attempt and model version the
    record under one event key carries. A key the capture does not hold is
    ErrFeedbackEventUnknown, and a key whose records disagree on the identity is
    ErrFeedbackTargetAmbiguous.

type CaptureFileDigest struct {
	// Name identifies the file within the capture.
	Name string
	// SHA256 is the content digest of the bytes actually written to the file.
	SHA256 string
	// Bytes is the count of bytes written to the file.
	Bytes int64
	// Records counts the records the file holds.
	Records int64
}
    CaptureFileDigest is the seal entry of one file a capture wrote: the name it
    is accounted under, the digest of the bytes actually written and what those
    bytes hold.

    The entry is the unit a seal is rechecked at: a recheck that finds a
    differing file names that file rather than reporting a mismatch on the
    capture as a whole.

type CaptureLedger struct {
	// Admitted counts the records the sink took into its queue.
	Admitted int64
	// DroppedRecordSize counts the records the per-record byte bound refused.
	DroppedRecordSize int64
	// DroppedInFlight counts the records the in-flight count bound refused.
	DroppedInFlight int64
	// DroppedBytes counts the records the byte budget refused.
	DroppedBytes int64
	// DroppedClosed counts the records refused because the sink was closed.
	DroppedClosed int64
	// WriteErrors counts the admitted records that failed to reach storage.
	WriteErrors int64
	// Written counts the admitted records that reached storage.
	Written int64
	// InFlight counts the admitted records the writer has not accounted yet.
	InFlight int64
}
    CaptureLedger is the loss account of one capture sink: what it admitted and
    wrote, what each admission bound refused and what failed to reach storage.
    The account is a counter snapshot and never a read of the data queue,
    so it stays readable while the queue is full.

func (l CaptureLedger) Dropped() int64
    Dropped reports every record the capture refused under an admission bound.

type CaptureManifest struct {
	// CaptureID identifies the capture the manifest seals.
	CaptureID string
	// Sealed reports whether a real writer confirmed the seal after the records
	// were flushed.
	Sealed bool
	// Partial reports a capture that closed with records in flight, refused
	// records, write losses, without a writer seal or without a completed sync.
	Partial bool
	// Records counts the records the capture wrote.
	Records int64
	// Dropped counts the records the capture refused under an admission bound.
	Dropped int64
	// InFlight counts the records still unflushed when the capture closed.
	InFlight int64
	// Files lists the seal entry of every file the capture wrote, so the seal is
	// rechecked per file.
	Files []CaptureFileDigest
	// Sync is the sync result the writer itself reported at seal time.
	Sync WriterSyncResult
	// Reasons lists why the capture is partial.
	Reasons []string
}
    CaptureManifest is the seal of one capture: what it holds file by file,
    whether a real writer confirmed and synced it, and what it lost.

    A capture without a writer seal is partial. A partial capture is never
    reported as complete, and the reason says what was still in flight. The
    seal is accounted per file: an aggregate hash alone is not a seal account,
    because a recheck must name the file that differs. A manifest whose writer
    reported no sync is partial as well — the seal covers what reached storage,
    never what was only written to.

type CaptureSink struct {
	// Has unexported fields.
}
    CaptureSink is the bounded admission queue of one capture: records enter
    under the configured bounds, the single writer drains them, and the seal is
    what that writer confirmed. Sending and closing run through one queue and
    one lock, so a concurrent seal is serialized against admission instead of
    racing it.

func (s *CaptureSink) Ledger() CaptureLedger
    Ledger reports the loss account of the capture from counters, readable while
    the queue is full and never drained through it.

func (s *CaptureSink) Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error)
    Record admits one observed attempt record under the sink's bounds and
    reports the bound that refused it when it is not admitted. Admission never
    waits on the writer: a record the bounds refuse is a counted loss with
    its bound named, a record the queue holds no room for is refused under
    the in-flight bound, and a closed capture refuses every record under
    BoundClosed. The record line is marshaled through the versioned line,
    which refuses a record shape that cannot be read back.

func (s *CaptureSink) RecordAttempt(ctx context.Context, record AttemptRecord) error
    RecordAttempt admits the attempt record of one observable model attempt
    through the single capture entry, refused and accounted exactly as
    RecordEpisode is. The terminal it admits is the one the record carries and
    none is re-derived here.

func (s *CaptureSink) RecordDecision(ctx context.Context, record DecisionRecord) error
    RecordDecision admits the decision record of one model decision through the
    single capture entry, refused and accounted exactly as RecordEpisode is.

func (s *CaptureSink) RecordEpisode(ctx context.Context, record EpisodeRecord) error
    RecordEpisode admits the episode record of one business process through
    the single capture entry every record kind enters the queue through.
    A record the entry cannot take is reported as an error naming the bound that
    refused it rather than dropped silently, and the refusal is counted in the
    loss ledger under that bound either way.

func (s *CaptureSink) RecordOutcome(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) (CaptureAdmission, error)
    RecordOutcome admits the attempt record of one model outcome through the
    single observation face AttemptOf: the terminal it records is the Kind the
    model reduction derived and none is re-derived here, so the capture and the
    model share exactly one terminal account.

func (s *CaptureSink) Seal(ctx context.Context) (CaptureManifest, error)
    Seal closes the capture and returns the manifest a real writer confirmed.
    The seal barrier enters the same queue every record line enters, so the
    seal accounts exactly what was admitted before it and a concurrent send
    is judged against the same bounds as the close. A second seal returns the
    first manifest and never seals twice. A seal the writer did not confirm
    within ctx is reported as the explicit partial state of an abandoned drain,
    carrying its in-flight and loss counts, and never as complete.

type CaptureWriter interface {
	// Append persists one record line under the named file. A line the writer
	// cannot persist is reported as an error and counted as a write loss by the
	// caller, and a bound reached on the way is refused rather than satisfied by
	// removing or rotating what is already stored.
	Append(name string, line []byte) error
	// Seal closes and syncs every file the writer holds and reports the seal
	// evidence of its own: the per-file digest entries of the bytes actually
	// written, in file name order, and the sync result the writer observed.
	Seal() ([]CaptureFileDigest, WriterSyncResult)
	// Commit persists the confirmed manifest so a restarted reader reads the
	// capture's state back instead of guessing it.
	Commit(manifest CaptureManifest) error
}
    CaptureWriter is the single durable write point of one capture: the queue
    drains its record lines into it and the seal of the capture is what this
    writer confirms.

    The redline list (4 rules): MUST report the digest entry of every file it
    actually wrote, in file name order. MUST report its own sync result at seal
    time. MUST NOT infer a sync result from record counts or file digests.
    MUST NOT satisfy a bound by removing or rotating what is already stored.

type DecisionRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// ID identifies the decision boundary.
	ID event.DecisionID `json:"decision_id"`
	// InvocationID scopes the decision to one responsibility range.
	InvocationID event.InvocationID `json:"invocation_id"`
	// EpisodeID names the episode the decision belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// InputEvents lists the stored events that were reduced into this decision, in
	// the order they were read.
	InputEvents []event.EventID `json:"input_events,omitempty"`
	// ModelView references the view the request actually ran on.
	ModelView ModelViewRef `json:"model_view"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}
    DecisionRecord is one model decision as data: the input events it was
    reduced from, the model view it actually ran on and the invocation it
    belongs to.

type DependencyKind string

    DependencyKind classifies why one episode's records depend on another's:
    a sub-call inside a parent episode, a handoff to another receiver, or a
    stored history that two episodes both read.

const (
	// DependencyChildCall marks a child call whose data cannot be split away from
	// the episode that made it.
	DependencyChildCall DependencyKind = "child_call"
	// DependencyHandoff marks a handoff, where the receiving episode continues the
	// work of the episode that handed it over.
	DependencyHandoff DependencyKind = "handoff"
	// DependencySharedHistory marks two episodes reading one stored history, so
	// their records stay on the same side of a split.
	DependencySharedHistory DependencyKind = "shared_history"
)
type EndpointAllowlist struct {
	// Hosts lists the exact hosts allowed; the port is free.
	Hosts []string
	// Schemes lists the allowed URL schemes, each of http or https; an empty list
	// admits both of them and nothing else.
	Schemes []string
	// MaxRedirects is the number of redirect hops that are each rechecked; zero
	// follows no redirect at all.
	MaxRedirects int
}
    EndpointAllowlist is the declared host allowlist of the dynamic model
    endpoint: an exact host with a free port, a scheme restricted to http or
    https, and no userinfo or fragment in the URL.

    The allowlist is rechecked on every redirect hop, so a first hop to an
    allowed host that redirects outside the list is refused and no prompt
    is sent to the destination of that hop. An empty host list admits no
    destination at all.

func (a EndpointAllowlist) Check(raw string) (*url.URL, error)
    Check judges one destination URL against the allowlist and returns the
    parsed form the request is sent to. A URL that does not parse, a scheme
    outside http/https or the declared list, userinfo, a fragment and a host
    outside the list are each refused by name.

type EndpointClient struct {
	// Has unexported fields.
}
    EndpointClient is the outbound face of one allowlisted dynamic endpoint:
    the request URL and every redirect hop are judged against the same allowlist
    before the hop is followed, and a response body is read under the declared
    byte bound.

func (c *EndpointClient) Get(ctx context.Context, rawURL string) ([]byte, error)
    Get fetches one allowlisted destination and returns the body it answered
    with. The destination URL is judged before the request is built, every
    redirect hop is judged before it is followed, and the body is read under
    the declared bound. A status outside the success range is refused without
    reading the body.

type EpisodeDependency struct {
	// EpisodeID names the episode that depends on another.
	EpisodeID event.EpisodeID `json:"episode_id"`
	// DependsOn names the episode it depends on.
	DependsOn event.EpisodeID `json:"depends_on"`
	// Kind classifies the dependency.
	Kind DependencyKind `json:"kind"`
	// Lineage states how certain the declared dependency is.
	Lineage Lineage `json:"lineage"`
}
    EpisodeDependency declares that one episode's records share data with
    another's and must therefore not be split away from it.

    A dependency that could not be established is marked unknown rather than
    dropped, because a missing dependency is what a leakage free split is
    decided on.

type EpisodeGroup struct {
	// GroupID identifies the group.
	GroupID string
	// RootID names the shared ancestor of the members; it is empty for a group
	// holding no common ancestor.
	RootID event.EpisodeID
	// Members lists the episode identifiers the group holds.
	Members []event.EpisodeID
	// Basis lists the declared dependencies that hold the members together.
	Basis []EpisodeDependency
	// Lineage states how certain the grouping is.
	Lineage Lineage
}
    EpisodeGroup is one leakage free split unit: an episode together with every
    episode it shares an ancestor, a candidate set or a history dependency with.

    A group is a split unit and not a session: an episode may span sessions and
    a session may hold several episodes, and neither equality is forced.

func CausalGroupOf(episodes []EpisodeRecord, episode event.EpisodeID) EpisodeGroup
    CausalGroupOf returns the leakage-free group one episode belongs to: every
    episode it shares a parent link or a declared dependency with. A session is
    never substituted for an episode, so two episodes of one session stay apart
    unless their records declare that their data cannot be split.

type EpisodeRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// ID identifies the episode.
	ID event.EpisodeID `json:"episode_id"`
	// ParentID names the episode this one was spawned from; it is empty for a
	// root episode and for an episode of unknown lineage.
	ParentID event.EpisodeID `json:"parent_id,omitempty"`
	// RootID names the first episode of the lineage.
	RootID event.EpisodeID `json:"root_id,omitempty"`
	// Ancestors lists the lineage from the root down to ParentID, in that order.
	Ancestors []event.EpisodeID `json:"ancestors,omitempty"`
	// Sessions lists the sessions the episode spans.
	Sessions []event.SessionID `json:"sessions,omitempty"`
	// DependsOn declares the episodes this episode shares data with, which is the
	// basis a leakage free split is drawn on.
	DependsOn []EpisodeDependency `json:"depends_on,omitempty"`
	// Lineage states how certain the recorded lineage is.
	Lineage Lineage `json:"lineage"`
	// Timestamp is the time the record was written.
	Timestamp time.Time `json:"timestamp"`
}
    EpisodeRecord is one business process as data: its lineage, its parent and
    the dependencies that decide which records may be split apart from it.

    An episode is a business process and not a session: a session may hold
    several episodes and an episode may span several sessions, and neither
    equality is forced. A record whose origin could not be read carries
    LineageUnknown with an empty ParentID instead of a guessed parent.

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
    ExportManifest is the provenance manifest of one export run: one entry per
    written sample, the leakage-free split groups those samples were assigned
    to, and the ledger of everything the run refused.

    A manifest is complete only when the run read every authorized page without
    a failure, the row recheck refused nothing in silence, and the digest covers
    the bytes actually written; anything less is marked partial with its reason
    readable.

type ExportRequest struct {
	// PartitionIDs names the partitions the caller is authorized to read. An empty
	// list is refused, and the port never widens it into a whole-store scan.
	PartitionIDs []string
	// CallIDs keeps only the facts whose call id is listed when it is non-empty.
	CallIDs []string
	// Since and Until bound the export in Unix milliseconds, the unit the stored
	// events use; 0 means open.
	Since int64
	Until int64
	// PageLimit is the per-partition page size; 0 takes the default and a value
	// outside the supported range is refused rather than clamped.
	PageLimit int
}
    ExportRequest is one authorized export run: the partitions the caller may
    read, the optional call-id filter and time bounds, and the page size to read
    them with.

    The request is the authorization input: the partition list is the caller's
    grant, and no field of the request may widen that grant.

type ExportResult struct {
	// Manifest is the provenance manifest of the run.
	Manifest ExportManifest
	// Written counts the sample lines the run wrote.
	Written int64
	// Forbidden counts the rows the per-row authorization recheck refused.
	Forbidden int64
	// Filtered counts the facts the caller's filters removed.
	Filtered int64
	// Partial reports a run that stopped before every authorized page was read.
	Partial bool
}
    ExportResult is the outcome of one export run: the provenance manifest of
    the snapshot together with the counts a caller reads without opening the
    snapshot.

    A partial run returns both the manifest and a non-nil error, so a truncated
    snapshot is never mistaken for a complete one.

type ExportSource interface {
	// Page returns at most limit facts of one partition starting at the given
	// offset, in a stable order. A short or empty page ends the partition, and
	// paging is by offset and never by an event-key cursor.
	Page(ctx context.Context, partition string, offset, limit int) ([]ExportedFact, error)
	// Episodes returns the episode records the split groups are drawn on.
	Episodes(ctx context.Context) ([]EpisodeRecord, error)
}
    ExportSource is the read seam an authorized export pages the store through:
    offset-paged reads of one partition and the episode records the leakage-free
    split groups are drawn on. The exporter reaches no further than this seam.

    The redline list (3 rules): MUST page one partition by offset in a stable
    order, ending it on a short or empty page. MUST return the episode records
    the split groups are drawn on. MUST NOT page by an event-key cursor or reach
    beyond this seam.

type ExportedFact struct {
	// EventKey is the canonical identity of the event.
	EventKey event.EventID `json:"event_key"`
	// Partition is the partition the event is filed under; the export
	// rechecks it against the grant before every written row.
	Partition string `json:"partition"`
	// CallID names the call the event belongs to; the caller's call filter is
	// applied to it.
	CallID string `json:"call_id,omitempty"`
	// CaptureID names the capture the event was recorded under.
	CaptureID string `json:"capture_id,omitempty"`
	// EpisodeID names the episode the event belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// DecisionID names the decision the event was reduced from.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the event observed.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// Schema is the record schema version the event was written with.
	Schema SchemaVersion `json:"schema"`
	// ParentKey names the parent the event was linked under; an absent key is
	// reported as absent and never invented.
	ParentKey event.EventID `json:"parent_key,omitempty"`
	// Timestamp is the event time in Unix milliseconds, the unit the time
	// bounds of the request are read in.
	Timestamp int64 `json:"timestamp"`
	// Body is the event payload, byte for byte.
	Body json.RawMessage `json:"body"`
}
    ExportedFact is one stored event as the export source hands it over:
    the canonical event key and the partition it is filed under, the capture,
    episode, decision and attempt it descends from, the schema it was
    written with, the parent key it was linked under, its event time in Unix
    milliseconds and the event payload committed byte for byte.

type Exporter interface {
	// Export runs one authorized export and returns the manifest of the snapshot it
	// wrote with the counts of the run. The context bounds the run, and a cancelled
	// run reports what it read as partial.
	Export(ctx context.Context, req ExportRequest) (ExportResult, error)
}

    Exporter is the read-only consumption port for authorized training export:
    it reads the events of the partitions a caller is authorized for, together
    with the feedback bound to them, and writes one snapshot whose provenance
    manifest says what the snapshot contains and how completely it was read.

    The redline list (4 rules): MUST run authorization before any read, and
    refuse an empty partition list. MUST recheck every row it writes and count
    a row that fails the recheck. MUST NOT write a row that fails its recheck.
    MUST NOT report a partial read as a complete snapshot.

type FeedbackAttribution struct {
	// Bound counts the submissions attributed to recorded evidence.
	Bound int64 `json:"bound"`
	// Unbound counts the submissions carrying no locatable reference.
	Unbound int64 `json:"unbound"`
	// Ambiguous counts the submissions whose evidence conflicted.
	Ambiguous int64 `json:"ambiguous"`
}
    FeedbackAttribution is the running account of how the submissions this face
    took were attributed.

type FeedbackBindingState string

    FeedbackBindingState names how one submission was attributed to the recorded
    evidence.

const (
	// FeedbackBound marks a submission attributed to a recorded decision or attempt.
	FeedbackBound FeedbackBindingState = "bound"
	// FeedbackUnbound marks a submission carrying no locatable event reference: the
	// conclusion is not bound and no reward is derived from it.
	FeedbackUnbound FeedbackBindingState = "unbound"
	// FeedbackAmbiguous marks a submission whose evidence conflicts: nothing is
	// written under a guessed binding.
	FeedbackAmbiguous FeedbackBindingState = "ambiguous"
)
type FeedbackEndpoint struct {
	// Has unexported fields.
}
    FeedbackEndpoint is the RL HTTP face of the delayed feedback entry:
    it authenticates at one point ahead of the payload read, attributes the
    submission to recorded evidence, writes the raw feedback and answers with
    the reward instance derived from it.

func (e *FeedbackEndpoint) Attribution() FeedbackAttribution
    Attribution returns the account of how the submissions taken so far were
    attributed.

func (e *FeedbackEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request)
    ServeHTTP serves one feedback submission: the request subject authenticates
    ahead of the payload read, the body is read under the declared byte bound,
    and the submission is attributed before anything is written. The reply
    carries the binding state and the reward instance, and a refusal carries its
    named error.

func (e *FeedbackEndpoint) Submit(ctx context.Context, payload FeedbackPayload) (FeedbackSubmission, error)
    Submit attributes one submission to the recorded evidence and writes it
    to the feedback account. A submission naming no event key is answered as
    unbound and writes nothing; a key the evidence does not hold is refused
    by name; conflicting evidence is answered as ambiguous beside its named
    refusal. A bound submission is written at its own identity and the reward
    account is brought to the evidence set of its decision, taking a new
    revision only when the evidence changed.

type FeedbackEndpointOption func(*FeedbackEndpoint)
    FeedbackEndpointOption names one optional part of a feedback face at
    construction: the request credential or the request bounds. The face carries
    no configuration value to choose from: the evidence resolver, the two
    accounts and the derivation rule are positional, and every optional part is
    added by the option that names it.

func WithFeedbackLimits(limits RequestLimits) FeedbackEndpointOption
    WithFeedbackLimits sets the request bounds the face enforces.
    A negative bound and the defaults a zero bound takes are decided by
    RequestLimits.Normalize, so the face reads one bound set for every request
    it serves.

func WithFeedbackToken(token string) FeedbackEndpointOption
    WithFeedbackToken sets the request credential the face authenticates with:
    the bearer token of every accepted submission. A face beside no credential
    stays closed, so no submission is served under an absent credential.

type FeedbackLedger struct {
	// Has unexported fields.
}
    FeedbackLedger is the feedback account: the raw feedback records in the
    order they were recorded, append-only. Nothing in this account is derived
    and nothing is rewritten — a late feedback is another record beside the
    earlier ones, so the reward derivations that read this account always name
    the exact evidence set they were computed from.

func NewFeedbackLedger() *FeedbackLedger
    NewFeedbackLedger returns an empty feedback account.

func (l *FeedbackLedger) Add(record FeedbackRecord) error
    Add records one feedback event under its own identity. A record without
    an identity, a binding or a version is refused by name, a repeat of the
    same record is idempotent, and different content under one feedback id is
    the named conflict ErrFeedbackConflict rather than a silent overwrite.
    Stamping SchemaCurrent happens here and nowhere else, so every record the
    account hands out carries the schema it was written with.

func (l *FeedbackLedger) All() []FeedbackRecord
    All returns every feedback record the account holds, in the order they were
    recorded.

func (l *FeedbackLedger) BoundTo(eventKey string) []FeedbackRecord
    BoundTo returns the feedback records bound to one event key, in the order
    they were recorded. A key with no feedback returns no record, which is the
    unrated case and never an error.

func (l *FeedbackLedger) Restore(record FeedbackRecord) error
    Restore records one feedback record read back from storage, keeping
    the schema, the identity and the recorded time it was written with,
    so a restarted reader attributes a late submission to the same decision
    and attempt as the writer did. A repeat of the same record is idempotent
    and different content under one feedback id is the named conflict
    ErrFeedbackConflict rather than an overwrite.

type FeedbackPayload struct {
	// EventKey is the hex key of the event the feedback is bound to.
	EventKey string `json:"event_key"`
	// DecisionID names the decision the feedback belongs to; it is empty when the
	// caller names only an event key.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback belongs to; it is empty when the
	// feedback is bound at the decision boundary.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// Conclusion is the external conclusion to bind.
	Conclusion string `json:"conclusion"`
	// Source names where the conclusion came from.
	Source string `json:"source,omitempty"`
}

    FeedbackPayload is the body of one feedback submission: the hex event key
    it targets, the decision or attempt it belongs to when the caller can name
    them, and the external conclusion to bind there.

    The type is a wire contract only and pulls in no HTTP dependency.
    Authentication runs at a single point before routing, so no endpoint,
    including a read-only one, has a bypass, and a caller credential stays out
    of any persisted record. A payload that cannot be resolved to an event is
    marked unbound, and a binding is never inherited from the most recent parent
    call.

type FeedbackRecord struct {
	// Schema is the record schema version the feedback was written with.
	Schema SchemaVersion `json:"schema"`
	// FeedbackID identifies the feedback event.
	FeedbackID string `json:"feedback_id"`
	// EventKey is the hex event key the feedback is bound to.
	EventKey string `json:"event_key"`
	// DecisionID names the decision the feedback belongs to; it is empty when
	// the feedback is bound to the event key alone.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback belongs to; it is empty when the
	// feedback is bound at the decision boundary.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// ModelVersion is the version of the model the bound evidence was served
	// under, read from the recorded attempt of the target. It is empty when the
	// target names no version, which is bound unversioned rather than assumed
	// to be the version in effect.
	ModelVersion string `json:"model_version,omitempty"`
	// Conclusion is the external conclusion that was bound.
	Conclusion string `json:"conclusion"`
	// Source names where the conclusion came from.
	Source string `json:"source"`
	// Version is the feedback version the record was written at, so a reader
	// names the version a derived reward was read from.
	Version string `json:"version"`
	// RecordedAt is the time the feedback was recorded.
	RecordedAt time.Time `json:"recorded_at"`
}

    FeedbackRecord is one feedback event as data: the external conclusion bound
    to one event key, kept exactly as it was recorded.

    The record is the raw feedback and not a reward: there is no value field
    because a conclusion is evidence and not a number, and no field here is ever
    derived from a task terminal. A reward derived from this record carries its
    identity in the reward's source list, so the feedback stays readable as
    the evidence it was after any number of derivations. The two accounts stay
    separate: a late feedback is read as new evidence and never as a rewrite of
    anything already derived.

func (r FeedbackRecord) FactKey() string
    FactKey returns the feedback's identity as a fact key: the feedback ID the
    record is stored under. A second record under one ID is either the same
    record (idempotent) or refused as a conflict on the write path, so one
    feedback ID never names two facts.

func (r FeedbackRecord) FactType() string
    FactType returns the record kind a feedback line is read as.

type FeedbackSubmission struct {
	// State names how the submission was attributed.
	State FeedbackBindingState `json:"state"`
	// FeedbackID identifies the feedback record the submission was written as.
	FeedbackID string `json:"feedback_id,omitempty"`
	// EventKey is the event key the submission named.
	EventKey string `json:"event_key,omitempty"`
	// DecisionID names the decision the feedback is bound to.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback is bound to.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// ModelVersion is the version of the model the bound evidence was served
	// under, so a submission binds to the version that ran rather than the one
	// in effect when it arrived.
	ModelVersion string `json:"model_version,omitempty"`
	// RewardInstanceID identifies the reward instance derived from the evidence.
	RewardInstanceID string `json:"reward_instance_id,omitempty"`
	// RewardRevision orders the derivation beside the earlier revisions.
	RewardRevision int64 `json:"reward_revision,omitempty"`
	// RewardVersion is the rule version the reward value was computed under.
	RewardVersion string `json:"reward_version,omitempty"`
}
    FeedbackSubmission is the outcome of one attributed submission: the binding
    state beside the feedback identity it was written under and the reward
    instance derived from the evidence.

type FeedbackTarget struct {
	// EventKey is the hex event key the evidence was read under.
	EventKey string
	// DecisionID names the decision the recorded event belongs to.
	DecisionID event.DecisionID
	// AttemptID names the attempt the recorded event observed.
	AttemptID event.AttemptID
	// ModelVersion is the version of the model the recorded event was served
	// under. It is empty when the record names no version, which is bound
	// unversioned rather than assumed to be the version in effect.
	ModelVersion string
}
    FeedbackTarget is the recorded identity one event key resolves to:
    the decision the record was reduced for, the attempt it observed and the
    model version that served it. The attempt is empty for a record filed at the
    decision boundary.

type FeedbackTargetResolver interface {
	// ResolveFeedbackTarget returns the recorded identity one event key names. A key
	// the evidence does not hold is ErrFeedbackEventUnknown, and evidence naming
	// conflicting identities is ErrFeedbackTargetAmbiguous.
	ResolveFeedbackTarget(ctx context.Context, eventKey string) (FeedbackTarget, error)
}
    FeedbackTargetResolver resolves the recorded identity behind one event key.
    The endpoint reaches no further than this seam, so attribution is decided by
    the evidence a resolver holds and never by the presence of a trace.

    The redline list (3 rules): MUST resolve the recorded identity from the
    evidence the resolver holds. MUST NOT decide attribution from the presence
    of a trace. MUST NOT answer a key the evidence does not hold as a resolved
    target.

type FileCaptureWriter struct {
	// Has unexported fields.
}
    FileCaptureWriter is the real durable writer of one capture: it appends
    record lines to files under its directory, closes and syncs those files at
    seal time and persists the confirmed seal there. Every digest it reports is
    the digest of the bytes it wrote to storage, and no path it opens escapes
    its own directory.

func (w *FileCaptureWriter) Append(name string, line []byte) error
    Append persists one record line under the named file inside the writer's
    directory. A sealed writer refuses the line, a name that would reach out of
    the directory is refused, and a file beyond the open file bound is refused
    as a write loss with ErrWriterOpenBound.

func (w *FileCaptureWriter) Commit(manifest CaptureManifest) error
    Commit persists the confirmed manifest under SealFileName in the writer's
    directory and syncs it to storage. A seal that is not persisted leaves a
    restarted reader with the partial state of a capture whose writer never
    sealed, so the commit is part of the seal rather than an afterthought.

func (w *FileCaptureWriter) Seal() ([]CaptureFileDigest, WriterSyncResult)
    Seal closes and syncs every file the writer holds and reports the seal
    evidence of its own: the per-file digest entries of the bytes actually
    written, in file name order so the same capture seals to the same account,
    and the sync result the writer observed. A sync that did not complete is
    reported with its reason and is never presented as a completed sync.

type ImportGap struct {
	// Line is the 1-based line number the gap was found on.
	Line int `json:"line"`
	// Reason names the gap: one of the ImportReason constants.
	Reason string `json:"reason"`
	// Detail carries the human-readable context of the gap.
	Detail string `json:"detail,omitempty"`
	// Raw keeps the raw bytes of the line as they stood.
	Raw string `json:"raw,omitempty"`
	// Err is the named parse-path error behind the gap, when one applies.
	Err error `json:"-"`
}
    ImportGap is one thing the legacy import could not read: an unparsable row,
    an unknown schema version or a row matching no known legacy schema.
    Every gap is recorded explicitly with its line number and raw bytes,
    so nothing the import could not read is dropped or guessed at.

    - The import is read-only over legacy JSONL and yields three separate
    chains: rows already written in the current record schema, legacy rows
    held read-only, and the explicit gaps of everything it could not classify.
    - A legacy row is mapped into its read-only projection with its raw bytes
    kept; no output of this package re-encodes a legacy row. - Classification
    mirrors scripts/convert_trajectories.py (classify_capture_row):
    schema_version 2 is capture v2, a numeric version below 2 is the legacy
    v1 dump era, an unknown or non-numeric version is never guessed at,
    a row with no schema_version is a v1 dump only beside llm_call without

type ImportResult struct {
	// Records holds the rows written in the current record schema, in line
	// order.
	Records []Line
	// Legacy holds the legacy rows read-only, in line order.
	Legacy []LegacyRecord
	// Unknown holds the explicit gaps, in line order.
	Unknown []ImportGap
}
    ImportResult is the three-chain result of one read-only import: the rows
    already written in the current record schema, the legacy rows held read-only
    and the explicit gaps. The chains are separate so a legacy row is never read
    as a current record and a current record is never projected into a legacy
    one.

type LegacyRecord struct {
	// Schema names the legacy schema the row was classified as.
	Schema SchemaVersion `json:"schema"`
	// Line is the 1-based line number the row was read from.
	Line int `json:"line"`
	// Fields keeps every field of the row as it stood, including the ones named
	// in UnknownFields.
	Fields map[string]any `json:"fields"`
	// UnknownFields names the fields outside this legacy schema's known set,
	// sorted; they are kept in Fields and never dropped.
	UnknownFields []string `json:"unknown_fields,omitempty"`
	// Lineage states how certain the lineage of the imported row is. A legacy
	// row is always LineageUnknown: the legacy format carries no episode
	// lineage, and a session or a trace is never substituted for it.
	Lineage Lineage `json:"lineage"`
	// Raw keeps the raw bytes of the row as they stood.
	Raw string `json:"raw"`
}
    LegacyRecord is one legacy row held read-only: the fields it carries kept
    as they stand, the fields outside that schema's known set named explicitly
    instead of dropped, and the lineage it can offer stated as unknown — a
    legacy row carries no episode lineage contract, so its lineage is never
    guessed.

    The record is a projection of a row the import only reads. Nothing writes it
    back in its legacy shape.

type Line struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion `json:"schema"`
	// Kind names the record kind the line carries.
	Kind RecordKind `json:"kind"`
	// Episode is the episode record of an episode line.
	Episode *EpisodeRecord `json:"episode,omitempty"`
	// Decision is the decision record of a decision line.
	Decision *DecisionRecord `json:"decision,omitempty"`
	// Attempt is the attempt record of an attempt line.
	Attempt *AttemptRecord `json:"attempt,omitempty"`
}
    Line is one versioned record line: the schema it was written with, the kind
    it carries and exactly one record payload.

func NewAttemptLine(record AttemptRecord) Line
    NewAttemptLine builds the versioned line of one attempt record, stamped with
    the schema version this package writes.

func NewDecisionLine(record DecisionRecord) Line
    NewDecisionLine builds the versioned line of one decision record, stamped
    with the schema version this package writes.

func NewEpisodeLine(record EpisodeRecord) Line
    NewEpisodeLine builds the versioned line of one episode record, stamped with
    the schema version this package writes.

type Lineage int
    Lineage states how certain a record's lineage is.

    LineageUnknown is the zero value: a record whose origin could not be read is
    marked unknown, and an unknown lineage is never reported as a known parent.

const (
	// LineageUnknown is the zero value: the record's origin could not be read.
	LineageUnknown Lineage = iota
	// LineageKnown is a lineage read from the application's own episode boundary.
	LineageKnown
)
func (l Lineage) MarshalJSON() ([]byte, error)
    MarshalJSON writes one lineage as its explicit wire name.

func (l *Lineage) UnmarshalJSON(data []byte) error
    UnmarshalJSON reads one lineage from its explicit wire name; a name this
    package does not know is refused rather than read as the zero value.

type LogprobsState int
    LogprobsState states how the logprobs of one attempt are known.

    LogprobsUnknown is the zero value: with neither a returned value nor a
    provider declaration observed, the state is unknown, and unknown is never
    reported as a plain absence of logprobs.

const (
	// LogprobsUnknown is the zero value: no returned logprobs and no provider
	// declaration were observed for the attempt.
	LogprobsUnknown LogprobsState = iota
	// LogprobsPresent marks logprobs the response actually returned.
	LogprobsPresent
	// LogprobsUnsupported marks a provider that declared the logprobs capability
	// unsupported; LogprobsSource names that declaration.
	LogprobsUnsupported
)
func LogprobsStateOf(resp *model.Response) (LogprobsState, string)
    LogprobsStateOf reports how the logprobs of one observed response are known:
    logprobs the response actually returned stand present, and a response
    that returned none stands unknown with no source, because a plain absence
    of logprobs is never reported as a provider declaration of unsupported.
    A LogprobsUnsupported state rests on a declaration the caller names and is
    never derived here.

func (s LogprobsState) MarshalJSON() ([]byte, error)
    MarshalJSON writes one logprobs state as its explicit wire name.

func (s *LogprobsState) UnmarshalJSON(data []byte) error
    UnmarshalJSON reads one logprobs state from its explicit wire name; a name
    this package does not know is refused rather than read as the zero value.

type ModelIdentity struct {
	// Model is the model name the request was declared against.
	Model string
	// Version is the version of that model. It is empty when the model ran
	// unversioned, which is recorded as unversioned rather than assumed to be
	// the version in effect.
	Version string
}

    ModelIdentity is one model beside the version of that model that served a
    request: the model name a declaration references and the generation of that
    name the request actually ran.

    The identity is what makes a model replacement readable in the records: two
    generations of one model name are two identities, so a record written before
    a replacement is never read as a record of the version installed after it.

func ParseModelIdentity(name string) ModelIdentity
    ParseModelIdentity reads one declared model name back into the name and
    the version it was rendered from. Exactly one separator with a non-empty
    name and version on its two sides is a rendered identity; every other name
    is read whole as an unversioned model name rather than split at a guessed
    position, so a provider name that happens to carry the separator is never
    split into a version it never declared.

func (i ModelIdentity) Name() string
    Name renders one identity as the single string a model declares on the
    responses it serves: the model name alone for an unversioned model, and the
    name, the separator and the version for a versioned one.

type ModelLease struct {
	// Has unexported fields.
}
    ModelLease is the replacement lease of one model reference: the version
    in effect, every version still serving a stream and the ordered account of
    every replacement attempt.

    A model version is never swapped under a request that already started:
    the lease covers the whole returned stream, and the version in effect is
    what a request that begins afterwards borrows. A replacement is a rebuild,
    a publication and only then an installation, so a failed replacement is
    never mistaken for a version that took effect; and the lease never edits the
    records written under an earlier version, so a decision, attempt or reward
    of the retired version stays readable as the record of the version that
    served it.

func (l *ModelLease) Close() error
    Close marks the lease closed and releases every retired version no stream
    still holds. The version in effect stays open: the binding that installed it
    still serves requests through it, and closing it here would pull the model
    from under those requests.

func (l *ModelLease) Current() *VersionedModel
    Current returns the model handle of the version in effect: the model a
    publication installs in a binding. The handle serves exactly one version,
    so the requests a generation borrows are the requests of that version.

func (l *ModelLease) CurrentVersion() string
    CurrentVersion returns the identity name of the version in effect: the name
    a declaration references for it.

func (l *ModelLease) InFlight() int64
    InFlight reports how many model streams the lease is still serving across
    every version it holds.

func (l *ModelLease) Replace(ctx context.Context, version string, rebuild func() (model.Model, error), publish func(ModelIdentity, model.Model) error) (ReplacementRecord, error)
    Replace rebuilds the model reference at one new version and installs it
    as the version in effect, in three steps that are refused as a whole: the
    rebuild produces the model, the publication entry accepts it, and only then
    does the lease make it the version a request that begins afterwards borrows.

    A rebuild that fails or yields no model records the attempt as refused
    beside ErrLeaseRebuildFailed and never reaches the publication entry,
    so a failed rebuild publishes no binding. A publication that refuses
    records the attempt as refused beside ErrLeasePublishFailed, releases the
    rebuilt model exactly once and leaves the version in effect untouched.
    An applied replacement retires the version it replaced: the retired version
    keeps serving the streams it already holds and is closed exactly once when
    the last of them ends.

func (l *ModelLease) Replacements() []ReplacementRecord
    Replacements returns the replacement attempts the lease recorded, in the
    order they were attempted.

func (l *ModelLease) Restore(history []ReplacementRecord) error
    Restore reads one persisted replacement history back into the lease and
    refuses a history whose last applied version is not the version the lease
    holds: a restarted lease attributes a request to the version persisted as
    the one in effect, and never strings a request across the version an earlier
    process served.

type ModelViewRef struct {
	// Model names the model the request was declared against.
	Model string `json:"model,omitempty"`
	// ModelVersion is the version of that model; it is empty when the model ran
	// unversioned.
	ModelVersion string `json:"model_version,omitempty"`
	// ParametersDigest is the content digest of the generation parameters the
	// request was declared with.
	ParametersDigest string `json:"parameters_digest,omitempty"`
	// PromptVersion is the version of the system prompt content the request was
	// assembled from.
	PromptVersion string `json:"prompt_version,omitempty"`
	// VisibleTools lists the tools visible to the request, each with the version
	// of its definition the request ran with.
	VisibleTools []VersionedName `json:"visible_tools,omitempty"`
	// VisibleSkills lists the skills visible to the request, each with the
	// version of its definition the request ran with.
	VisibleSkills []VersionedName `json:"visible_skills,omitempty"`
	// ContextDigest is the content digest of the assembled context.
	ContextDigest string `json:"context_digest,omitempty"`
	// Snapshot is the readable reference to the safe request snapshot the view
	// was fixed against. An empty reference is not a snapshot: a digest alone
	// confirms bytes it cannot return and is therefore not a snapshot reference.
	Snapshot SnapshotRef `json:"snapshot"`
}
    ModelViewRef references the model view a decision was actually made on:
    the safe request snapshot the request was fixed against, the model and
    generation parameters of the request, and the prompt, skill and tool set
    visible to it, each member with the version it was read at.

    The type names the view; it holds no second copy of the request. The view is
    fixed before the request starts and is never reconstructed from the options
    of a finished call: the digest fields verify what a reader reads back
    through Snapshot and never stand in for it, and a visible member is pinned
    by name and version together rather than by name alone.

type ProvenanceGap struct {
	// SampleID identifies the refused sample.
	SampleID string
	// Kind classifies the gap.
	Kind ProvenanceGapKind
	// Reason states why the sample was refused.
	Reason string
}
    ProvenanceGap is one sample an export run refused to write, with the reason
    it was refused.

    A refusal is counted and named rather than dropped in silence, so the
    manifest shows what was left out and why.

type ProvenanceGapKind string
    ProvenanceGapKind classifies why an export run refused a sample.

const (
	// ProvenanceGapMissing marks a sample with no provenance entry at all.
	ProvenanceGapMissing ProvenanceGapKind = "missing"
	// ProvenanceGapMalformed marks a provenance entry that does not parse.
	ProvenanceGapMalformed ProvenanceGapKind = "malformed"
	// ProvenanceGapUnknown marks a sample whose source quality could not be
	// established, so its lineage is reported as unknown rather than assumed.
	ProvenanceGapUnknown ProvenanceGapKind = "unknown"
)
type QualificationMatrix struct {
	// Rows holds one row per purpose.
	Rows []QualificationRow
}
    QualificationMatrix is the qualification contract of one cleaning run:
    one row per training purpose, in purpose order.

func (m QualificationMatrix) Row(purpose TrainingPurpose) (QualificationRow, bool)
    Row returns the row of one purpose and reports whether the matrix holds it.

type QualificationRow struct {
	// Purpose names the training use the row judges.
	Purpose TrainingPurpose
	// RequiredEvidence names the evidence the purpose is decided on.
	RequiredEvidence []string
	// Verdict is exactly one of the three qualification states.
	Verdict QualificationVerdict
	// MissingReasons names every condition the verdict rests on that the
	// observed evidence does not meet; it is empty only for a qualified row.
	MissingReasons []string
}
    QualificationRow is one row of the training-use qualification matrix: the
    purpose, the real evidence it requires, its single verdict and the reasons
    behind a verdict that is not qualified.

type QualificationVerdict string
    QualificationVerdict is exactly one of the three qualification states.

const (
	// QualificationUnknown marks a purpose whose evidence is genuinely missing:
	// nothing observed rules the purpose out and nothing confirms it.
	QualificationUnknown QualificationVerdict = "unknown"
	// QualificationIneligible marks a purpose known not to meet a necessary
	// condition on the observed data.
	QualificationIneligible QualificationVerdict = "ineligible"
	// QualificationQualified marks a purpose with a named consumer and every
	// required condition met.
	QualificationQualified QualificationVerdict = "qualified"
)
type RecordKind string
    RecordKind names which of the three record kinds one line carries.

    - The versioned record line: one JSONL line carries its schema version
    and exactly one record kind, and a reader selects its parse path from
    the version the line declares. - The wire shape is this schema's own:
    it evolves on its own version and is never bound to the shape of
    a legacy capture dump, so a line carrying legacy capture fields

const (
	// RecordKindEpisode names a line carrying one EpisodeRecord.
	RecordKindEpisode RecordKind = "episode"
	// RecordKindDecision names a line carrying one DecisionRecord.
	RecordKindDecision RecordKind = "decision"
	// RecordKindAttempt names a line carrying one AttemptRecord.
	RecordKindAttempt RecordKind = "attempt"
)
type Recorder interface {
	// RecordEpisode writes the episode record of one business process at the
	// point that process starts.
	RecordEpisode(ctx context.Context, record EpisodeRecord) error
	// RecordDecision writes the decision record of one model decision at the
	// point that decision produced its terminal.
	RecordDecision(ctx context.Context, record DecisionRecord) error
	// RecordAttempt writes the attempt record of one observable model attempt at
	// the point that attempt produced its response.
	RecordAttempt(ctx context.Context, record AttemptRecord) error
}
    Recorder is the face one production point writes its versioned records
    through. The three methods cover the three record kinds, one call per record
    written at the point that record actually originates.

    The redline list (4 rules): MUST write each record at the point that record
    actually originates. MUST report a record it cannot accept as an error.
    MUST NOT edit the fields of a record it persists or projects. MUST NOT drop
    a record it could not accept without reporting it.

    - The production face of the versioned records: one episode boundary,
    one model decision, one observable model attempt each write their record
    through this face, exactly as the production point observed them. - A
    production point records what it observed and derives nothing: the terminal
    comes from the single model reduction, the response is the native response
    the attempt saw, and no record here re-classifies a state of its own. - A
    recorder error never reaches the model decision: recording is observation,
    not execution. - The telemetry observation sink shares only the word:

type RecorderAdapter struct {
	// Has unexported fields.
}
    RecorderAdapter adapts the execution loop's narrow recording face onto this
    package's record pipeline: the scattered observations one production point
    writes are assembled here into the versioned episode, decision and attempt
    records and handed to the wrapped recorder exactly as the production point
    observed them. The adapter edits no field of the record it assembles and
    derives nothing: the attempt record is built through the single observation
    face AttemptOf, and a visible member that ran unversioned is recorded
    unversioned rather than assumed current.

    The methods below carry built-in, event and model types only — the same
    shapes the execution loop's recording face crosses — so this adapter
    satisfies that face with no package edge into the execution core.

func NewRecorderAdapter(inner Recorder) RecorderAdapter
    NewRecorderAdapter wraps one record pipeline in the recording face the
    execution loop attaches: every record assembled from one production point's
    observations is written through inner.

func (a RecorderAdapter) RecordAttempt(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error
    RecordAttempt builds the attempt record through the single observation face
    and writes it through the wrapped pipeline: the terminal it records is the
    one the model reduction derived, and none is re-derived here.

func (a RecorderAdapter) RecordDecision(ctx context.Context, decisionID event.DecisionID, invocationID event.InvocationID, episodeID event.EpisodeID, inputs []event.EventID, modelName, promptVersion string, visibleTools []string, at time.Time) error
    RecordDecision assembles the decision record of one model decision from
    the identities, stored input events and model view material the production
    point observed and writes it through the wrapped pipeline. A visible tool
    that ran unversioned is recorded unversioned rather than assumed current,
    and the snapshot reference stays empty at this production point: the safe
    request snapshot is the capture port's to produce, and a digest alone is
    never a snapshot reference.

func (a RecorderAdapter) RecordEpisode(ctx context.Context, episodeID event.EpisodeID, sessionID event.SessionID, at time.Time) error
    RecordEpisode assembles the episode record of one business process from the
    boundary the production point observed and writes it through the wrapped
    pipeline. The lineage is recorded as unknown with no parent invented:
    no origin is read at this production point, and an unknown lineage is never
    reported as a known parent.

type ReplacementRecord struct {
	// Schema is the record schema version the attempt was recorded under.
	Schema SchemaVersion `json:"schema"`
	// From is the identity in effect when the attempt began; it is empty for a
	// lease that held no version.
	From string `json:"from,omitempty"`
	// To is the identity the attempt asked for. A refused attempt names the
	// version asked for under the model name in effect, because no rebuilt
	// model stood to declare its own.
	To string `json:"to"`
	// State states whether a binding took effect.
	State ReplacementState `json:"state"`
	// Reason is the named refusal of an attempt that took no effect; it is
	// empty for an applied replacement.
	Reason string `json:"reason,omitempty"`
	// At is the time the attempt was recorded.
	At time.Time `json:"at"`
}
    ReplacementRecord is one replacement attempt as data: the version it started
    from, the version it asked for, whether a binding took effect and the named
    reason of a refusal.

    The record is an account and not a switch: a refused attempt leaves the
    version in effect as it was, so the history of a lease reads as the versions
    it actually served rather than the versions it was asked for.

type ReplacementState int
    ReplacementState states how one replacement attempt ended.

    ReplacementRefused is the zero value: an attempt that published no binding
    is never reported as a version that took effect.

const (
	// ReplacementRefused marks an attempt that published no binding: the
	// version in effect is the one it was before the attempt.
	ReplacementRefused ReplacementState = iota
	// ReplacementApplied marks an attempt whose rebuilt model the publication
	// entry accepted.
	ReplacementApplied
)
func (s ReplacementState) MarshalJSON() ([]byte, error)
    MarshalJSON writes one replacement state as its explicit wire name.

func (s *ReplacementState) UnmarshalJSON(data []byte) error
    UnmarshalJSON reads one replacement state from its explicit wire name;
    a name this package does not know is refused rather than read as the zero
    value.

type RequestLimits struct {
	// MaxBodyBytes bounds a request body.
	MaxBodyBytes int64
	// MaxMessages bounds the message count of a task submission.
	MaxMessages int
	// MaxMessageBytes bounds the content of a single message.
	MaxMessageBytes int64
	// MaxResponseBytes bounds a response body read back from the endpoint.
	MaxResponseBytes int64
	// Roles lists the roles a message may carry.
	Roles []string
}
    RequestLimits declares the single-point bounds every request is checked
    against, including a feedback submission.

    The limits are enforced in one place: a body over the byte bound is refused,
    the message count and the per-message byte bound are refused as well,
    and a value that would turn a bound off is a configuration error rather than
    a valid request.

func (l RequestLimits) Normalize() (RequestLimits, error)
    Normalize returns the bounds one request is enforced under: a zero bound
    takes its default and a negative bound is refused by name. Every check of
    the request reads the returned value, so a caller never sees two different
    bounds.

type RequestSnapshot struct {
	// Model names the model the request was declared against.
	Model string `json:"model,omitempty"`
	// ModelVersion is the version of that model; empty when it ran unversioned.
	ModelVersion string `json:"model_version,omitempty"`
	// PromptVersion is the version of the system prompt content the request was
	// assembled from.
	PromptVersion string `json:"prompt_version,omitempty"`
	// VisibleTools lists the tools visible to the request, each with the version
	// of its definition the request ran with.
	VisibleTools []VersionedName `json:"visible_tools,omitempty"`
	// VisibleSkills lists the skills visible to the request.
	VisibleSkills []VersionedName `json:"visible_skills,omitempty"`
	// Parameters is the generation configuration the request was declared with,
	// kept as the actual parameters rather than a digest of them.
	Parameters json.RawMessage `json:"parameters,omitempty"`
	// StructuredOutput is the structured output declaration of the request.
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	// Messages holds the conversation the request carried, projected at its URL
	// carriers and verbatim everywhere else.
	Messages []model.Message `json:"messages"`
	// ToolSchemas holds the declaration of every tool visible to the request,
	// in tool name order so the snapshot bytes are deterministic.
	ToolSchemas []json.RawMessage `json:"tool_schemas,omitempty"`
	// ExtraFields holds the projected provider extra fields of the request.
	ExtraFields map[string]any `json:"extra_fields,omitempty"`
	// Headers holds the header names the request carried with their values
	// projected to RedactedValue: a header value never reaches a persisted
	// record.
	Headers map[string]string `json:"headers,omitempty"`
}
    RequestSnapshot is the safe request boundary snapshot of one model request:
    the model view it was declared against and the request content it carried,
    projected so no header, cookie or URL credential is persisted.

    The snapshot is fixed before the request starts and holds the request as
    it stood then: the messages, tool schemas, multimodal parts, reasoning and
    actual parameters are kept verbatim while a credential carrier is projected,
    so the projection never rewrites business content. The headers are kept as
    their names with their values projected to RedactedValue, because a header
    value is a transport credential carrier rather than request content.

func (s RequestSnapshot) Ref(snapshotID, readEntry string) (SnapshotRef, error)
    Ref reports the readable reference to this snapshot under the named read
    entry: the digest covers the exact bytes MarshalSnapshot writes, and a
    reader rechecks it against what it reads back.

type RewardLedger struct {
	// Has unexported fields.
}
    RewardLedger is the reward account: the derived reward records per decision
    boundary and attempt, append-only. Every derivation takes the next revision
    and its own instance ID, so two values derived from different evidence
    sets remain distinguishable and an earlier record keeps the revision it was
    written under, never rewritten in place.

func NewRewardLedger() *RewardLedger
    NewRewardLedger returns an empty reward account.

func (l *RewardLedger) Derive(decision event.DecisionID, attempt event.AttemptID, rule RewardRule, evidence []FeedbackRecord) (RewardRecord, error)
    Derive derives one reward record from the feedback evidence and records
    it as a new instance at the next revision. Nothing already recorded is
    rewritten.

    A derivation handed no evidence is refused by name with ErrRewardNoFeedback
    and recorded as an unrated instance that carries no value: a reward is never
    invented from a task terminal, and both the refusal and the unrated mark
    reach the caller. A rule that names no version or carries no value function
    is refused with ErrRewardRuleMissing before anything is recorded.

func (l *RewardLedger) DeriveForEvidence(decision event.DecisionID, attempt event.AttemptID, rule RewardRule, evidence []FeedbackRecord) (RewardRecord, error)
    DeriveForEvidence returns the reward instance one decision boundary and
    attempt carries under the given evidence set. When the newest recorded
    derivation was computed from exactly that evidence under the same model
    version, it is returned as it stands; when the evidence changed — a late
    feedback arrived — or the version it was served under differs — a model
    replacement moved the attempt onto another generation — a new instance is
    derived at the next revision and the record earlier readers cite keeps the
    revision it was written under.

func (l *RewardLedger) Latest(decision event.DecisionID, attempt event.AttemptID) (RewardRecord, bool)
    Latest returns the newest reward record derived for one decision boundary
    and attempt, and reports whether one is held. The record is returned as
    it was written: a reader cites the instance ID and revision it carries,
    never a regenerated identity.

func (l *RewardLedger) Records(decision event.DecisionID, attempt event.AttemptID) []RewardRecord
    Records returns every reward record derived for one decision boundary and
    attempt, in the order they were derived. A late feedback derivation is
    another record beside the earlier ones, so the history stays readable.

func (l *RewardLedger) Restore(record RewardRecord) error
    Restore records one reward record read back from storage, keeping the
    identity and revision it was written with. A repeat of the same record is
    idempotent and different content under one instance identity is the named
    conflict ErrRewardConflict rather than a silent overwrite. A record without
    an instance identity is kept as it stands: it cannot be cited by an exported
    sample and stays readable for the rejection ledger to name.

func (l *RewardLedger) StateOf(decision event.DecisionID, attempt event.AttemptID) RewardState
    StateOf returns the reward state of the newest record derived for one
    decision boundary and attempt: RewardUnrated when nothing was derived or the
    newest derivation carried no evidence, RewardRated otherwise.

type RewardRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion
	// InstanceID identifies this derived record as one concrete reward instance:
	// every derivation for a decision takes its own instance ID, so two values
	// derived under one rule version remain distinguishable.
	InstanceID string
	// Revision orders the reward derivations recorded for one decision: each new
	// derivation takes the next revision and an earlier record keeps the revision
	// it was written under, never rewritten in place.
	Revision int64
	// DecisionID names the decision the reward is derived for.
	DecisionID event.DecisionID
	// AttemptID names the attempt the reward is derived for; it is empty when the
	// reward is derived at the decision boundary.
	AttemptID event.AttemptID
	// ModelVersion is the version of the model the evidence was served under, so
	// a reward derived before a model replacement is never read as the reward of
	// the version installed after it. It is empty for evidence that names no
	// version, which is recorded unversioned rather than assumed current.
	ModelVersion string
	// State states whether a reward was derived at all.
	State RewardState
	// Value is the derived reward value; it is meaningful only under RewardRated.
	Value float64
	// RuleVersion is the version of the derivation rule the value was computed
	// with, so a reader re-reads the value under that version rather than a
	// current one; it is not an instance identity.
	RuleVersion string
	// DerivedFrom lists the feedback sources the reward was derived from, in the
	// order they were read.
	DerivedFrom []RewardSource
	// DerivedAt is the time the reward was derived.
	DerivedAt time.Time
}
    RewardRecord is a reward derived from feedback for one decision: a derived
    record and not the raw feedback event, carrying the instance it identifies
    itself by, the derivation rule version it was computed with and the feedback
    sources it was derived from.

    The record is derived and versioned rather than overwritten in place:
    a later feedback produces a new instance at a new revision, and the recorded
    instance stays readable under the rule that computed it. A rule version
    alone never identifies a reward record — the same rule deriving a value from
    late feedback is a different record — so the instance ID and revision are
    what a consumer cites, and a record without an instance identity cannot be
    cited at all.

func (r RewardRecord) FactKey() string
    FactKey returns the reward's identity as a fact key: the instance ID a
    derivation takes, because a rule version alone never identifies a reward
    record — the same rule deriving a value from late feedback is a different
    record, and a record without an instance identity could not be cited at all.

func (r RewardRecord) FactType() string
    FactType returns the record kind a reward line is read as.

type RewardRule struct {
	// Version is the rule version a derived value is stamped with.
	Version string
	// Value reduces the feedback evidence to the reward value. It runs only
	// over real evidence: a derivation with no evidence never reaches it.
	Value func(evidence []FeedbackRecord) (float64, error)
}

    RewardRule is one derivation rule: the version a derived value is computed
    under and the function that reduces the feedback evidence to that value.

    The rule version is what a reader re-reads the value under. It is never
    an instance identity: the same rule deriving a value from late feedback
    is a different reward record, and the instance ID and revision are what a
    consumer cites.

type RewardSource struct {
	// FeedbackID identifies the feedback event the reward was derived from.
	FeedbackID string
	// EventKey is the hex event key the feedback was bound to.
	EventKey string
	// Version is the feedback version the source was read at.
	Version string
	// RecordedAt is the time the feedback was recorded.
	RecordedAt time.Time
}
    RewardSource references one feedback a reward was derived from: the feedback
    event, the event key it was bound to, and the version it was read at.

type RewardState int

    RewardState states whether a reward was derived for a decision.

    RewardUnrated is the zero value: a decision with no linked feedback is
    recorded as unrated and is never given a value inferred from its task
    terminal.

const (
	// RewardUnrated is the zero value: no feedback was linked, so no reward exists.
	RewardUnrated RewardState = iota
	// RewardRated marks a reward derived from at least one feedback source.
	RewardRated
)
type SFTArtifact struct {
	// Schema is the artifact schema the sample was written with.
	Schema SchemaVersion `json:"schema"`
	// SampleID names the exported sample the artifact was built from.
	SampleID string `json:"sample_id"`
	// SourceDigest is the SHA-256 of the stored body the sample was read off.
	SourceDigest string `json:"source_digest"`
	// Encoder names the tokenizer behind the ids.
	Encoder SFTEncoderIdentity `json:"encoder"`
	// Sequence holds the token planes of the sample.
	Sequence SFTSequence `json:"sequence"`
}
    SFTArtifact is one token-level training sample as it is stored and read
    back: the schema it was written with, the sample it came from, the bytes it
    was read off, the encoder behind its ids and its token planes.

type SFTBatch struct {
	// Planes holds the padded planes of the batch.
	Planes SFTBatchPlanes
	// Lengths holds the unpadded token count of every sample, in row order.
	Lengths []int
	// PaddingID is the id a pad position carries.
	PaddingID int
}
    SFTBatch is one collated batch: the padded planes, the unpadded length of
    every sample and the id every pad position carries.

type SFTBatchPlanes struct {
	// InputIDs carries one padded id row per sample.
	InputIDs [][]int `json:"input_ids"`
	// AttentionMask carries one padded attention row per sample.
	AttentionMask [][]int `json:"attention_mask"`
	// LossMask carries one padded loss row per sample.
	LossMask [][]int `json:"loss_mask"`
	// Labels carries one padded label row per sample.
	Labels [][]int `json:"labels"`
}
    SFTBatchPlanes holds the padded planes of one batch, one row per sample.

type SFTCleanRecord struct {
	// SampleID names the sample the record belongs to.
	SampleID string
	// EventKey names the stored event the sample was reduced from.
	EventKey event.EventID
	// GroupID is the causal split group the sample's provenance names.
	GroupID string
	// Kept reports whether the sample became an example.
	Kept bool
	// Gap names the refusal that kept the sample out; it is empty when kept.
	Gap SFTGap
	// Actions and Observations are the mask counts of a kept sample.
	Actions      int
	Observations int
	// SourceDigest is the SHA-256 of the stored body the sample carries.
	SourceDigest string
}
    SFTCleanRecord is the cleaning record of one input sample: kept or refused,
    the single refusal that kept it out, the mask counts of a kept sample and
    the digest of the bytes it was read from. Every input sample carries exactly
    one record.

type SFTCleanRequest struct {
	// Samples are the exported samples to clean.
	Samples []SampleLine
	// Prompts are the fixed prompts, one per decision a sample was made under.
	Prompts []SFTPrompt
	// Episodes are the episode records the causal groups are drawn on; the
	// split never follows a session identity.
	Episodes []EpisodeRecord
	// Consumer declares the real consumer the cleaned data is qualified for.
	Consumer TrainingUseConsumer
	// Split is the deterministic split declaration of this cleaning run.
	Split SplitDeclaration
}
    SFTCleanRequest is one cleaning run: the exported samples, the fixed prompts
    they are paired with, the episode records their causal groups are drawn on,
    the consumer the data is qualified for and the split declaration.

type SFTCleanResult struct {
	// Records holds one cleaning record per input sample, in sample order.
	Records []SFTCleanRecord
	// Examples holds the samples that passed the cleaning contract.
	Examples []SFTExample
	// Ledger is the failure account, in gap-kind order.
	Ledger []SFTGapLedger
	// Splits holds one entry per causal group the examples were drawn from.
	Splits []SFTSplit
	// Kept and Rejected count the samples on each side of the contract.
	Kept     int
	Rejected int
	// Matrix is the qualification contract of the run.
	Matrix QualificationMatrix
}
    SFTCleanResult is the outcome of one cleaning run: one record per input
    sample, the examples that passed, the failure account by gap kind, the
    causal split groups and the qualification matrix of the run.

func CleanSFT(req SFTCleanRequest) SFTCleanResult
    CleanSFT runs one cleaning pass over exported samples: every sample is
    either converted into a masked example or refused with a named gap,
    the split is drawn on causal groups, and the qualification matrix is derived
    from the evidence the run observed. The samples and prompts are read and
    never edited.

type SFTConsumerRecord struct {
	// SampleID names the sample the record belongs to.
	SampleID string
	// GroupID is the causal split group of the sample.
	GroupID string
	// Split is the side the group was assigned to.
	Split string
	// Accepted reports whether the sample entered the batch.
	Accepted bool
	// Refusal names the refusal that kept the sample out; it is zero when
	// accepted.
	Refusal SFTRefusal
}
    SFTConsumerRecord is the consumer record of one cleaned sample: accepted
    into the batch or refused by name, beside the group and split the sample
    carries.

type SFTConsumerRequest struct {
	// Examples are the cleaned samples of one SFT run.
	Examples []SFTExample
	// Tokenizer produces and names the ids of every sample.
	Tokenizer SFTSourceTokenizer
	// MaxLength is the longest sequence the run accepts; a value at or below
	// zero accepts any length.
	MaxLength int
	// PaddingID is the id a pad position carries.
	PaddingID int
}
    SFTConsumerRequest is one consumer run: the cleaned samples to tokenize,
    the tokenizer they are read through, the longest sequence the run accepts
    and the id a pad position carries.

type SFTConsumerResult struct {
	// Records holds one consumer record per sample, in sample order.
	Records []SFTConsumerRecord
	// Artifacts holds the samples that passed, in sample order.
	Artifacts []SFTArtifact
	// Batches holds the collated batch; it is empty when no sample passed.
	Batches []SFTBatch
	// Face names what the run encoded through.
	Face SFTTokenizerFace
}
    SFTConsumerResult is the outcome of one consumer run: one record per sample,
    the artifacts that passed, the collated batch and the face the run stood on.

func ConsumeSFT(req SFTConsumerRequest) SFTConsumerResult
    ConsumeSFT runs one consumer pass over cleaned samples: every sample is
    tokenized, stored, verified and collated, or refused by name. A run that
    names no tokenizer accepts nothing, and a tokenizer that encodes distinct
    conversations into one sequence is refused as a stand-in.

func (r SFTConsumerResult) Accepted() int
    Accepted counts the samples that entered the batch.

func (r SFTConsumerResult) Ledger() []SFTRefusalLedger
    Ledger reports the refusal account by kind, in kind order.

func (r SFTConsumerResult) Refused() int
    Refused counts the samples kept out of the batch.

type SFTEncodeFunc func(SFTExample) (SFTTokenization, error)
    SFTEncodeFunc tokenizes one cleaned conversation.

type SFTEncoderIdentity struct {
	// Provenance is the kind of encoder the identity claims.
	Provenance SFTEncoderProvenance `json:"provenance"`
	// Tokenizer names the tokenizer; an empty name declares no tokenizer.
	Tokenizer string `json:"tokenizer"`
	// TemplateHash fingerprints the chat template the ids came from.
	TemplateHash string `json:"template_hash"`
}
    SFTEncoderIdentity names the tokenizer an encoder stands on: the kind it
    claims, its name and the fingerprint of the template its ids were rendered
    with.

type SFTEncoderProvenance string
    SFTEncoderProvenance names what produced the token ids of one sample.

const (
	// SFTEncoderReal marks ids produced by a real tokenizer through its chat
	// template.
	SFTEncoderReal SFTEncoderProvenance = "real"
	// SFTEncoderDeterministic marks ids produced by a deterministic encoder that
	// stands on no tokenizer.
	SFTEncoderDeterministic SFTEncoderProvenance = "deterministic"
)
type SFTExample struct {
	// SampleID names the exported sample the example was converted from.
	SampleID string
	// GroupID names the causal split group of the example.
	GroupID string
	// Split names the side the group was assigned to.
	Split string
	// DecisionID names the decision the attempt was made under.
	DecisionID event.DecisionID
	// AttemptID names the attempt the completion was observed on.
	AttemptID event.AttemptID
	// Messages holds the conversation exactly as read, each with its mask side.
	Messages []SFTMessage
	// Actions counts the messages a loss trains on.
	Actions int
	// Observations counts the context messages a loss never trains on.
	Observations int
	// SourceDigest is the SHA-256 of the stored body the example was read from.
	SourceDigest string
}
    SFTExample is one sample that passed the cleaning contract: the conversation
    it converts to, the mask side of every message, the split group it belongs
    to and the digest of the bytes it was read from.

type SFTGap struct {
	// Kind classifies the refusal.
	Kind SFTGapKind
	// Reason states what was observed.
	Reason string
}
    SFTGap is one named refusal: the kind it is classified under and the reason
    it was refused.

type SFTGapKind string
    SFTGapKind classifies why one sample was kept out of the dataset. Every
    refusal is named: a sample that is neither converted nor refused would be a
    silent drop.

const (
	// SFTGapLegacyReadOnly marks a sample written under the v2 record schema: it
	// is read through the v2 converter and never as an example here.
	SFTGapLegacyReadOnly SFTGapKind = "legacy_read_only"
	// SFTGapUnknownSchema marks a sample whose schema this package cannot read.
	SFTGapUnknownSchema SFTGapKind = "unknown_schema"
	// SFTGapNotAnAttempt marks a sample carrying no observed attempt: an SFT
	// example is the completion of one attempt, and a decision carries none.
	SFTGapNotAnAttempt SFTGapKind = "not_an_attempt"
	// SFTGapBodyUnreadable marks a sample whose stored body cannot be parsed.
	SFTGapBodyUnreadable SFTGapKind = "body_unreadable"
	// SFTGapIdentityMismatch marks a body whose identity differs from the fact
	// carrying it.
	SFTGapIdentityMismatch SFTGapKind = "identity_mismatch"
	// SFTGapTerminalNotCompleted marks an attempt that did not complete.
	SFTGapTerminalNotCompleted SFTGapKind = "terminal_not_completed"
	// SFTGapAttemptFailed marks an attempt that observed an error.
	SFTGapAttemptFailed SFTGapKind = "attempt_failed"
	// SFTGapCompletionMalformed marks a response that is not exactly one
	// assistant choice, or a choice of another role.
	SFTGapCompletionMalformed SFTGapKind = "completion_malformed"
	// SFTGapEmptyCompletion marks an assistant action with neither content nor
	// a tool call.
	SFTGapEmptyCompletion SFTGapKind = "empty_completion"
	// SFTGapPromptMissing marks a sample whose decision has no fixed prompt.
	SFTGapPromptMissing SFTGapKind = "prompt_missing"
	// SFTGapIllegalMessage marks a conversation whose message sequence or role
	// is not legal.
	SFTGapIllegalMessage SFTGapKind = "illegal_message"
	// SFTGapGroupMismatch marks a sample whose provenance group differs from
	// the group its episode's causal dependencies decide.
	SFTGapGroupMismatch SFTGapKind = "group_mismatch"
)
type SFTGapLedger struct {
	// Kind classifies the refusals this line accounts for.
	Kind SFTGapKind
	// Count is the number of samples the kind refused.
	Count int
	// SampleIDs names the refused samples.
	SampleIDs []string
}
    SFTGapLedger is one line of the failure account: a gap kind, how many
    samples it refused and which ones, in sample order.

type SFTMask string
    SFTMask names the loss side of one conversation message: an action is
    trained on and an observation is context only. The side follows the role of
    the message and is never read from its content.

const (
	// SFTMaskObservation marks context a loss never trains on.
	SFTMaskObservation SFTMask = "observation"
	// SFTMaskAction marks a model action a loss trains on.
	SFTMaskAction SFTMask = "action"
)
type SFTMessage struct {
	// Message is the conversation message exactly as the records carried it.
	Message model.Message
	// Mask is the loss side of that message.
	Mask SFTMask
}
    SFTMessage is one message of a cleaned conversation beside the mask side it
    falls on. The message is carried as it was read, never rewritten.

type SFTPrompt struct {
	// DecisionID names the decision the prompt was fixed for.
	DecisionID event.DecisionID
	// Messages holds the request messages as the boundary fixed them.
	Messages []model.Message
}
    SFTPrompt is the prompt fixed for one decision at the formal request
    boundary: the conversation the model actually saw before the attempt
    started.

type SFTRefusal struct {
	// Kind classifies the refusal.
	Kind SFTRefusalKind
	// Reason states what was observed.
	Reason string
}
    SFTRefusal is one named refusal of the consumer: the kind it is classified
    under and what was observed.

func SFTRefusalOf(err error) (SFTRefusal, bool)
    SFTRefusalOf reports the named refusal an error carries, so a caller
    classifies a refusal by its kind rather than by its prose.

type SFTRefusalKind string
    SFTRefusalKind classifies why one sample was kept out of a training batch.

const (
	// SFTRefusalNoTokenizer marks a run that named no tokenizer: nothing is
	// tokenized and no sample is accepted.
	SFTRefusalNoTokenizer SFTRefusalKind = "no_tokenizer"
	// SFTRefusalEncodeFailed marks a tokenizer that failed on one conversation.
	SFTRefusalEncodeFailed SFTRefusalKind = "encode_failed"
	// SFTRefusalEncoderConstant marks distinct conversations a tokenizer encoded
	// into one sequence: a stand-in carries no tokenization of its input.
	SFTRefusalEncoderConstant SFTRefusalKind = "encoder_constant"
	// SFTRefusalEncoderIdentity marks an encoder identity that breaks its
	// contract, including a real tokenizer claim without a template fingerprint.
	SFTRefusalEncoderIdentity SFTRefusalKind = "encoder_identity"
	// SFTRefusalUnknownSchema marks an artifact written under a schema this
	// package does not read.
	SFTRefusalUnknownSchema SFTRefusalKind = "unknown_schema"
	// SFTRefusalUnreadableArtifact marks a stored line that does not decode.
	SFTRefusalUnreadableArtifact SFTRefusalKind = "unreadable_artifact"
	// SFTRefusalEmptySample marks a sample carrying no token.
	SFTRefusalEmptySample SFTRefusalKind = "empty_sample"
	// SFTRefusalLengthMismatch marks planes of different lengths, so a position
	// has no id, mask, label or message.
	SFTRefusalLengthMismatch SFTRefusalKind = "length_mismatch"
	// SFTRefusalSequenceInvalid marks a token id, an attention position or a
	// loss position outside its contract.
	SFTRefusalSequenceInvalid SFTRefusalKind = "sequence_invalid"
	// SFTRefusalLabelMismatch marks a label that differs from the id a trained
	// token carries, or from SFTLabelIgnoreIndex on an untrained one.
	SFTRefusalLabelMismatch SFTRefusalKind = "label_mismatch"
	// SFTRefusalAttributionMissing marks a sample naming no message behind its
	// tokens, so no token side can be read.
	SFTRefusalAttributionMissing SFTRefusalKind = "attribution_missing"
	// SFTRefusalAttributionInvalid marks an attribution naming a message the
	// conversation does not carry, or an unknown mask side.
	SFTRefusalAttributionInvalid SFTRefusalKind = "attribution_invalid"
	// SFTRefusalTargetEmpty marks a sample whose last message carries no token.
	SFTRefusalTargetEmpty SFTRefusalKind = "target_empty"
	// SFTRefusalObservationTrained marks a loss position on an observation side.
	SFTRefusalObservationTrained SFTRefusalKind = "observation_trained"
	// SFTRefusalActionUntrained marks an action token no loss trains on.
	SFTRefusalActionUntrained SFTRefusalKind = "action_untrained"
	// SFTRefusalSourceMismatch marks an artifact whose identity, source bytes or
	// message masks differ from the cleaned sample it names.
	SFTRefusalSourceMismatch SFTRefusalKind = "source_mismatch"
	// SFTRefusalTooLong marks a sample longer than the longest sequence the run
	// accepts.
	SFTRefusalTooLong SFTRefusalKind = "too_long"
)
type SFTRefusalLedger struct {
	// Kind classifies the refusals this line accounts for.
	Kind SFTRefusalKind
	// Count is the number of samples the kind refused.
	Count int
	// SampleIDs names the refused samples.
	SampleIDs []string
}
    SFTRefusalLedger is one line of the refusal account: a kind, how many
    samples it refused and which ones, in sample order.

type SFTSequence struct {
	// InputIDs are the token ids in conversation order.
	InputIDs []int `json:"input_ids"`
	// AttentionMask carries one attention position per token.
	AttentionMask []int `json:"attention_mask"`
	// LossMask carries one loss position per token: 1 on a trained token.
	LossMask []int `json:"loss_mask"`
	// Labels carry the id a loss reads on a trained token and
	// SFTLabelIgnoreIndex everywhere else.
	Labels []int `json:"labels"`
	// Attribution names the message behind every token.
	Attribution SFTTokenAttribution `json:"attribution"`
}
    SFTSequence is the token planes of one sample: the ids, their attention, the
    loss positions, the labels a loss reads and the attribution of every token.

type SFTSourceTokenizer struct {
	// Identity names the tokenizer behind the ids.
	Identity SFTEncoderIdentity
	// Encode produces the ids of one conversation; a nil function tokenizes
	// nothing.
	Encode SFTEncodeFunc
}
    SFTSourceTokenizer is the tokenizer one consumer run reads through:
    the identity its ids are stamped with and the function that produces them.

type SFTSplit struct {
	// GroupID names the group.
	GroupID string
	// Split names the side the group was assigned to.
	Split string
	// Examples counts the examples the group holds.
	Examples int
}
    SFTSplit is one causal split group and the side it was assigned to, with the
    number of examples it holds.

type SFTTokenAttribution struct {
	// MessageOf names, per token, the message it came from.
	MessageOf []int `json:"message_of"`
	// MessageMask is the mask side of every message of the conversation.
	MessageMask []SFTMask `json:"message_mask"`
}
    SFTTokenAttribution records what every token of one sample was produced
    from: the message it came from and the mask side of every message.

type SFTTokenization struct {
	// InputIDs are the token ids in conversation order.
	InputIDs []int
	// MessageOf names, per token, the index of the message it came from;
	// SFTScaffoldToken marks a token the template emitted around the messages.
	MessageOf []int
}
    SFTTokenization is one conversation tokenized as a whole: the ids the
    encoder produced and the message every token came from.

type SFTTokenizerFace string
    SFTTokenizerFace names what stood behind one consumer run.

const (
	// SFTTokenizerFaceReal marks a run encoded by a real tokenizer.
	SFTTokenizerFaceReal SFTTokenizerFace = "real"
	// SFTTokenizerFaceDeterministic marks a run encoded by a deterministic
	// encoder; it never counts as a tokenizer face.
	SFTTokenizerFaceDeterministic SFTTokenizerFace = "deterministic"
	// SFTTokenizerFaceAbsent marks a run that encoded nothing.
	SFTTokenizerFaceAbsent SFTTokenizerFace = "absent"
)
type SampleLine struct {
	// Schema is the line schema version the sample was written with.
	Schema SchemaVersion `json:"schema"`
	// Provenance is the manifest entry this sample carries.
	Provenance SampleProvenance `json:"provenance"`
	// Fact is the stored event the sample was reduced from.
	Fact ExportedFact `json:"fact"`
	// Feedback lists the raw feedback records the reward was derived from.
	Feedback []FeedbackRecord `json:"feedback,omitempty"`
	// Reward is the reward instance the sample carries, cited by instance ID
	// and revision.
	Reward RewardRecord `json:"reward"`
}
    SampleLine is one exported training sample: the provenance entry it carries
    beside the fact it was reduced from, the feedback evidence bound to it and
    the reward instance derived from that evidence. Feedback and reward stay two
    separate accounts on the line — the feedback is raw evidence and the reward
    is the derived record citing it by identity — and are never merged into one.

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

    SampleProvenance links one exported sample to the records it was produced
    from: the capture, export, episode, decision and attempt it descends
    through, each with the version it was read at, and the concrete reward
    instance the sample carries.

    A sample whose lineage cannot be established is listed in the rejection
    ledger rather than written under a guessed link, and its split group names
    the episodes it shares data with so a leakage-free split stays drawn on real
    dependencies. The source is pinned to instance versions and never to a rule
    version alone: the reward the sample carries is named by instance ID and
    revision, and a sample whose reward instance cannot be pinned is refused
    rather than written under a derivation rule version that could stand for
    several records.

type SchemaVersion string
    SchemaVersion names the record schema a line was written with. A reader
    selects its parse path from this value, and a version it does not recognise
    is reported as an unknown version rather than read under a known one.

    The current record schema is versioned on its own: its wire shape evolves
    independently and is never bound to the shape of a legacy capture dump.

const (
	// SchemaCurrent is the record schema version this package writes: the
	// versioned episode, decision and attempt records. A line written under it
	// carries exactly one record kind and no legacy capture field, and a reader
	// that does not know this value refuses the line instead of reading it as
	// another version.
	SchemaCurrent SchemaVersion = "tagent.record.v1"
	// SchemaLegacyV2 names the legacy capture v2 record schema (its own
	// schema_version 2): it is read through the legacy import only and is never
	// written back.
	SchemaLegacyV2 SchemaVersion = "capture.v2"
	// SchemaLegacyV1 names the legacy v1 trajectory dump schema: it is read
	// through the legacy import only and is never written back.
	SchemaLegacyV1 SchemaVersion = "trajectory.v1"
	// SchemaUnknown names a line whose schema version is not recognised. It is
	// recorded explicitly on such a line and is never reported as a known one.
	SchemaUnknown SchemaVersion = "unknown"
)
type SinkConfig struct {
	// MaxRecordBytes bounds the marshaled line of one record.
	MaxRecordBytes int64
	// MaxInFlight bounds the records admitted and not yet accounted by the
	// writer; it is also the bound on the queue backlog.
	MaxInFlight int64
	// MaxBytes bounds the total marshaled line bytes the capture admits.
	MaxBytes int64
}
    SinkConfig is the admission bounds of one capture sink. A zero bound takes
    its default, a negative bound is refused at construction, and each bound
    refuses records rather than removing stored history to make room.

type SnapshotPolicy struct {
	// ProjectExtraFields projects the provider extra fields before they are
	// persisted. It receives the JSON-normalized deep copy of the request's
	// extra fields and returns the projected map.
	ProjectExtraFields func(map[string]any) map[string]any
	// ProjectURL projects one URL carrier of a message content part before it is
	// persisted.
	ProjectURL func(string) string
}
    SnapshotPolicy is the configurable safe projection one snapshot is taken
    under. The zero policy is the default projection: credential-named extra
    field keys and URL credentials are redacted and everything else keeps its
    bytes. A projection replaces values; it never edits the request itself.

type SnapshotRef struct {
	// SnapshotID identifies the safe snapshot persisted at the request boundary,
	// before the request started.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Digest is the content digest of the persisted snapshot bytes, which a
	// reader rechecks against what it reads back.
	Digest string `json:"digest,omitempty"`
	// ReadEntry names the trusted authorization entry the snapshot is read back
	// through; the reference carries no raw credential and no unscoped path.
	ReadEntry string `json:"read_entry,omitempty"`
}
    SnapshotRef is the readable reference to one safe request snapshot:
    the request as it stood before the request started, projected so no header,
    cookie or URL credential is persisted, readable back in full through the
    named read entry and rechecked against its digest.

    The reference reads content back and not merely verifies it: the messages,
    tool schemas, multimodal parts, reasoning and actual parameters of the
    request are read back through the reference, because a digest alone
    confirms bytes it cannot return and is therefore not a snapshot reference.
    A reference to a snapshot that still carries a credential is not a safe
    reference, and a view whose snapshot cannot be read back is recorded as
    unreadable rather than re-derived from the options of a finished call.

type SplitDeclaration struct {
	// SplitSeed seeds the deterministic group-to-side assignment.
	SplitSeed int64
	// TrainRatio is the share of groups assigned to the training side; a value
	// outside (0, 1) takes DefaultSFTTrainRatio.
	TrainRatio float64
}
    SplitDeclaration is the split configuration of one cleaning run: the seed
    and ratio together are the deterministic group-to-side assignment contract,
    kept apart from the data-bearing request fields so the request structure
    stays a record of data, not of choices.

type TrainingExporter struct {
	// Has unexported fields.
}
    TrainingExporter runs authorized training exports over an export source
    and the two ledgers. Every run takes its own export ID and seals its own
    snapshot, so concurrent runs over one partition write beside each other and
    never overwrite.

func NewTrainingExporter(source ExportSource, feedback *FeedbackLedger, rewards *RewardLedger, rule RewardRule) *TrainingExporter
    NewTrainingExporter returns an exporter reading the given source and
    deriving every reward under the given rule from the feedback account.

func (e *TrainingExporter) Export(ctx context.Context, req ExportRequest) (ExportResult, error)
    Export runs one authorized export. Authorization runs before any read: a
    request that grants no partition is refused by name and the source is never
    touched. Every row is rechecked against the grant before it is written, and
    a row failing the recheck is counted as forbidden and written not at all.
    A run cut short by a read or write failure returns its manifest as partial
    beside a named error, never as a complete snapshot.

func (e *TrainingExporter) SealSnapshot(exportID string, lines []SampleLine) error
    SealSnapshot stores one export snapshot under its export ID. The seal is
    exclusive: an export ID already sealed is refused with ErrExportConflict,
    so concurrent runs write beside each other and a sealed snapshot is never
    overwritten.

func (e *TrainingExporter) Snapshot(exportID string) ([]SampleLine, bool)
    Snapshot returns the sample lines sealed under one export ID, and reports
    whether the snapshot is held.

func (e *TrainingExporter) SnapshotIDs() []string
    SnapshotIDs returns the export IDs sealed so far.

type TrainingPurpose string
    TrainingPurpose names one training use the qualification matrix judges.
    Each purpose is qualified on its own evidence; a verdict for one purpose
    never stands for another.

const (
	// TrainingPurposeSFT names supervised fine tuning on one observed completion.
	TrainingPurposeSFT TrainingPurpose = "sft"
	// TrainingPurposeDPO names direct preference optimisation on same-input
	// candidate pairs.
	TrainingPurposeDPO TrainingPurpose = "dpo"
	// TrainingPurposeSampledRL names off-policy learning from sampled rollouts
	// under one behaviour policy.
	TrainingPurposeSampledRL TrainingPurpose = "sampled_rl"
)
type TrainingUseConsumer struct {
	// Purpose names the training use the consumer reads the data for.
	Purpose TrainingPurpose
	// ConsumerID identifies the consumer; an empty identity declares none.
	ConsumerID string
	// Algorithm names the actual algorithm the consumer runs.
	Algorithm string
	// BehaviourPolicy identifies the policy the samples were drawn under.
	BehaviourPolicy string
	// RequiresLogprobs reports whether the algorithm needs logprobs of the
	// sampled tokens; a false value never makes their absence a failure.
	RequiresLogprobs bool
	// SamplingTemperature is the sampling temperature the consumer draws at; a
	// value at or below zero declares no sampling evidence.
	SamplingTemperature float64
}
    TrainingUseConsumer declares one real consumer of the cleaned data:
    the purpose it consumes for, its identity, and the algorithm and sampling
    declaration it runs under. A zero consumer declares no consumer at all.

type VersionedModel struct {
	// Has unexported fields.
}
    VersionedModel is the model handle of one version: the model a publication
    installs in a binding, serving exactly the version it was built for.

    The handle leases every call for the whole returned stream, so a version
    retired while the stream is open keeps serving it and is not closed until
    the stream ends. The handle declares the identity it serves on every
    response it hands over, which is what makes the served version readable in
    the attempt record without a second source of truth.

func (m *VersionedModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error)
    GenerateContent serves one request from the pinned version under a stream
    lease that covers the whole returned stream. A stream that fails to
    establish releases the lease at once; a stream whose caller stops reading
    keeps the lease, because a stream this handle cannot account as ended is
    never released under a possibly live producer.

func (m *VersionedModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error)
    GenerateContentIter serves one request from the pinned version in the
    caller goroutine. The sequence is lazy: building it takes no lease and
    touches no model, and the lease is taken when the caller drives it.
    An inner model that iterates itself is delegated to rather than bridged,
    so the iterator capability of the pinned instance is kept instead of hidden
    behind a channel. An entry refusal reaches the caller as the stream item the
    terminal reduction reads, so a refusal is never an empty sequence read as a
    completed one.

func (m *VersionedModel) Info() model.Info
    Info reports the descriptor of the version this handle serves: the model
    name it was built for beside the context window of the pinned instance.

type VersionedName struct {
	// Name identifies the member: a tool name or a skill name.
	Name string `json:"name"`
	// Version is the version of that member's definition the request ran with.
	// It is empty when the member ran unversioned, which is recorded as
	// unversioned rather than assumed to be the current one.
	Version string `json:"version,omitempty"`
}
    VersionedName names one member of a request's visible set together with the
    version of the definition the request actually ran with.

    A name without a version never pins what the request saw: two definitions
    under one tool name are different members of a view, and a member recorded
    without a version is reported as unversioned rather than assumed to be the
    current one.

type WriterSyncResult struct {
	// Synced reports whether the writer confirmed the written bytes reached
	// durable storage.
	Synced bool
	// Reason states why the sync did not complete; it is empty for a completed
	// sync.
	Reason string
	// At is the time the writer reported the result.
	At time.Time
}
    WriterSyncResult is the writer's own report at seal time: whether the
    written bytes were synced to durable storage, and why not when they were
    not.

    The result is evidence of what reached storage rather than what was handed
    to the writer: a manifest whose writer reported no sync is partial, and a
    sync result is never inferred from record counts or file digests.
