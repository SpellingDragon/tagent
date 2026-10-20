package rl // import "github.com/SpellingDragon/tagent/rl"

Package rl is the training observation surface of the runtime: it declares the
versioned records a model request is remembered by, the episode and dependency
grouping those records are split on, and the capture port that fixes a request
view before the request starts.

The package is data and ports only. Record identities come from the event
package, the observed response is the native model response, and no type here
derives an identity, a lineage or a redaction on its own.

  - record vocabulary: the versioned schemas and the line envelope (record,
    line)
  - capture: the pre-request snapshot port, its redaction policy and the sealed
    sink (capture, sink, writer)
  - production face: the recording port the execution loop attaches (produce)
  - feedback attribution: the counters the host reads beside the training face
    (feedback)
  - replacement lease: the swappable model handle and its replacement history
    (replacement)
  - record vocabulary versus event identity: a record's identity is the event
    package's EpisodeID, DecisionID or AttemptID, no second identity is declared
    here, and a record type is never read as an event type
  - shape pins: the compile-time assertions pin every declared shape and carry
    no logic of their own

CONSTANTS

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

const MaxOpenFilesCap = 16
    MaxOpenFilesCap is the hard cap on the files one writer holds open: a bound
    above the cap is clamped to the cap rather than obeyed.

const RedactedValue = "[redacted]"
    RedactedValue is what every credential carrier is projected to: a header
    name, a query key or a field name stays readable while the credential itself
    is never persisted.

    - Safe snapshot face: one snapshot is fixed from the request before the
    request starts, projected so no header, cookie or URL credential reaches
    a persisted record. - A finished call's options never rebuild a request
    snapshot, so the snapshot records what the request boundary carried and
    replays nothing.

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

func MarshalLine(line Line) ([]byte, error)
    MarshalLine encodes one versioned record line. A line carrying no schema,
    an unknown schema or a kind and payload that do not agree is refused rather
    than written without its version.

    - An attempt line is written only in a shape that can be read back:
    LogprobsUnsupported without its declaration source is refused here, never
    persisted to be refused at the read side.

func MarshalSnapshot(snapshot RequestSnapshot) ([]byte, error)
    MarshalSnapshot encodes one safe snapshot for persistence.

func NewCaptureSink(id string, cfg SinkConfig, writer CaptureWriter) (*CaptureSink, error)
    NewCaptureSink returns the bounded admission queue of one capture, draining
    into writer. A missing capture id or writer is refused, and a negative bound
    is refused rather than read as unlimited: a zero bound takes its default.

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

func ReadCaptureSeal(dir string) (CaptureManifest, error)
    ReadCaptureSeal reads the state of one capture back from the writer's
    directory: the persisted writer seal when there is one, and the explicit
    partial state of a capture whose writer never sealed when there is not.
    A capture read back without a seal is partial with its reason and its counts
    reported unknown, never complete and never zero-loss.

func RedactExtraFields(fields map[string]any) map[string]any
    RedactExtraFields is the default safe projection of the provider extra
    fields: a credential-named key keeps its name and loses its value to
    RedactedValue, a string value carrying URL credentials is projected through
    SanitizeURL, and everything else keeps its value as it stood.

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

func ValidateAttempt(record AttemptRecord) error
    ValidateAttempt reports whether one attempt record may be written and read
    back. It holds exactly one rule: a LogprobsUnsupported state is valid only
    beside the declaration it rests on. A present state rests on the values the
    response returned and needs no source, and an unknown state is never forced
    into either of the other two.

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

type FeedbackAttribution struct {
	// Bound counts the submissions attributed to recorded evidence.
	Bound int64 `json:"bound"`
	// Unbound counts the submissions carrying no locatable reference.
	Unbound int64 `json:"unbound"`
	// Ambiguous counts the submissions whose evidence conflicted.
	Ambiguous int64 `json:"ambiguous"`
}

    FeedbackAttribution is the running account of how the submissions of the
    training observation face were attributed: the host reads it beside the
    training face so an operator can tell bound evidence from unbound traffic.

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
