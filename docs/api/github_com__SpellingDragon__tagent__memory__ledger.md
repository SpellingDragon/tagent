package ledger // import "github.com/SpellingDragon/tagent/memory/ledger"

The one atomic storage seam a tier opens: engines implement it, the segment and
original stores read and commit through it, and a caller injects failures by
wrapping it. No caller names an engine's file layout, so switching engines needs
no adaptation and the same contract test runs over every engine.

Package ledger is the runtime's durable original-text layer: it names one stored
original and the reference metadata that scopes it, commits those bytes and
returns them byte for byte, and carries the storage tiers built beside them.

The package speaks the fact vocabulary of the event package and nothing above
it, so the durable side cannot be reversed into the intelligent or governing
side. Its files sit under the ledger unit by the mechanism-subpackage rule of
CONTRIBUTING.md: the fact vocabulary (OriginalRef, Integrity), the read face
(the stores, RecallResult) and the per-tier strategies (localfile, rustviking).

The journal is the localfile tier's persistent unit: one append-only file of
frames, each carrying the length of its payload and a CRC32C of that payload.
A frame that is fully present and matches its checksum is a committed record.
A frame the writer left half-written is a torn tail, and only that tail is cut
off at open. A complete frame whose checksum does not match is corruption, and
it refuses the open: the records before it are never served as the whole log,
because a silently accepted damaged frame is a fact that disappears.

journalKV is the key-value seam of the localfile tier over its append-only
journal. A commit appends one record, completes the log's barrier over it and
only then publishes its values to the resident set, so a successful return means
the record is durable and a commit whose write or barrier failed leaves the view
answering exactly the records the file holds.

The original store every durable engine keeps: the reference and independent
body model of the in-memory tier, committed through the atomic store seam so it
runs over any engine. Nothing here names a file layout.

The resident entry set every durable engine keeps beside its persisted record:
an LRU over key-value pairs with a byte budget, so what the process holds is
bounded whatever the store persists.

The one place that reads the retired sharded-snapshot layout: an operator
migrating a deployment root reads that layout as the read-only source and writes
the journal the build opens now. Every other caller reaches storage through the
atomic store seam and names no layout at all.

The original-text model: the named refusals of commit and recall, and the blob
and reference records one event key holds.

Original references and the read scope that authorizes them: one reference names
one stored original together with the metadata that scopes it, and the scoped
read face serves only the references registered inside the scope the read runs
under. See doc.go for the package's place in the memory axis.

Skeleton compile-time assertions for the ledger shapes: every declared field
name and type, every declared method signature, and every constant that must
stay a compile-time constant. A rename or a widened signature stops this file
from compiling, so the storage face cannot drift silently.

TierFacts is the durable fact surface of one opened tier: put, prefix scan, the
point read of one key and the sync barrier a commit reports its success behind.
It commits through the same atomic store engine the tier's segment store uses,
so the replay and the durable task commit share one database with the events
already stored there. Its fact-log signatures carry built-in types only,
so it matches the shared durable fact-log port structurally and no package edge
is required in either direction — the port is declared once for every ledger
that fills it (see api/durable) and this tier names the one surface it offers.

CONSTANTS

const DefaultSegmentMaxBytes int64 = 64 << 10
    DefaultSegmentMaxBytes is the finite bound served when a SegmentRequest
    carries a zero MaxBytes, so a request that states no limit stays bounded
    instead of reading an original whole.

VARIABLES

var ErrBatchNotAtomic = errors.New("memory: this engine lands one key per call and cannot commit a batch whole")
    ErrBatchNotAtomic reports a commit of a batch carrying several keys
    handed to an engine whose every call lands one key: the engine refuses
    the batch before any call runs, so a reader never meets the first keys
    landed beside a key that failed. A caller reads the same limit off
    StoreCapabilities.AtomicBatch instead of inferring it from this error.

var ErrCommitAfterClose = errors.New("memory: commit through a closed view")
    ErrCommitAfterClose reports a commit through a view whose Close ended it,
    so a caller that kept writing states the closed view instead of writing
    beside the process that took the root over.

var ErrCommitBarrier = errors.New("memory: commit barrier failed")
    ErrCommitBarrier reports a commit whose barrier did not complete, so nothing
    of it is published.

var ErrCommitUncertain = errors.New("memory: commit outcome uncertain")
    ErrCommitUncertain reports a commit whose durable outcome the engine cannot
    name: the write or its barrier did not return success and the engine
    could not confirm what the store holds, so the caller re-reads instead of
    reporting either outcome.

var ErrCommitWithoutWriteRights = errors.New("memory: commit through a view that holds no write rights")
    ErrCommitWithoutWriteRights reports a commit carrying keys through a view
    that declared no write rights at open: one root carries one writer, so a
    view holding none reads beside that writer and its commit is refused by name
    before any byte is appended, never admitted as a second writer of the root.

var ErrContentConflict = errors.New("memory: key already names other bytes")
    ErrContentConflict reports a commit whose key already names other bytes:
    one key names one original, so the second content is refused.

var ErrEventKeyRequired = errors.New("memory: segment record carries no event key")
    ErrEventKeyRequired reports a segment record that carries no event key,
    so no read could ever name the event again.

var ErrFactCommitUnsupported = errors.New("memory: the tier cannot commit a whole fact")
    ErrFactCommitUnsupported reports a tier this build cannot commit a whole
    fact through: the caller states the tier it was handed instead of committing
    one family beside a judgment it never made.

var ErrIntegrityMismatch = errors.New("memory: bytes do not match the recorded integrity")
    ErrIntegrityMismatch reports original bytes that do not match the
    completeness information recorded for them, so the read is refused instead
    of served.

var ErrJournalBarrier = errors.New("memory: journal barrier failed")
    ErrJournalBarrier reports a barrier that did not complete, so nothing staged
    in front of it is confirmed.

var ErrJournalCorrupt = errors.New("memory: journal frame is corrupt")
    ErrJournalCorrupt reports a frame that is fully present in the log and does
    not match the checksum written beside it.

var ErrJournalFormat = errors.New("memory: journal format version is not supported")
    ErrJournalFormat reports a log written under a format version this build
    does not read.

var ErrJournalFrameTooLarge = errors.New("memory: journal record is over the frame bound")
    ErrJournalFrameTooLarge reports one record whose encoded payload is over
    the frame bound: the refusal is decided before any byte of the record is
    written, so no part of it is staged and a caller states the refusal instead
    of an uncertain outcome.

var ErrJournalLegacyLayout = errors.New("memory: data directory holds the retired shard layout, open a new directory")
    ErrJournalLegacyLayout reports a data directory that holds the retired
    sharded-snapshot layout instead of a journal, so the caller is told to open
    a new directory rather than being handed a half-read store.

var ErrKeyRequired = errors.New("memory: original reference carries no key")
    ErrKeyRequired reports a commit that carries no key, so no read could ever
    name the original again.

var ErrMigrateMismatch = errors.New("memory: migrated root does not read back the source")
    ErrMigrateMismatch reports a copied root that does not read back exactly
    what the source held. The copy does not report success for a root it could
    not verify.

var ErrMigrateSourceUnchanged = errors.New("memory: migration source changed under the copy")
    ErrMigrateSourceUnchanged reports a source that does not read the same way
    twice: the verifying pass over the retired layout names a different key set
    than the copy landed, so the result is refused instead of reporting a copy
    of a root that moved under it.

var ErrMigrateTargetNotEmpty = errors.New("memory: migration target already holds data")
    ErrMigrateTargetNotEmpty reports a migration target that already holds keys:
    the copy rewrites nothing, so an operator points it at the new, still empty
    directory instead of a live one.

var ErrOriginalExpired = errors.New("memory: every reference is outside its retention window")
    ErrOriginalExpired reports a segmented read of a key whose references are
    all outside their retention windows.

var ErrOriginalMissing = errors.New("memory: no reference names the key")
    ErrOriginalMissing reports a segmented read of a key no reference names.

var ErrReadAfterClose = errors.New("memory: read through a closed view")
    ErrReadAfterClose reports a read through a view whose Close ended it:
    the view states the closed view by name instead of answering the absence its
    released handle states, so a caller tells a view it ended from a body the
    log cannot answer.

var ErrRootRequired = errors.New("memory: persistent tiers require a root directory")
    ErrRootRequired reports a persistent tier opened without the directory it
    persists under.

var ErrRootWriteLocked = errors.New("memory: another process holds write rights on this storage root")
    ErrRootWriteLocked reports a write-rights open of a storage root another
    process already holds write rights on: one root carries one writer,
    so the second open is refused by name instead of committing over the slots
    the first one's index points at.

var ErrRustViking = errors.New("memory: rustviking CLI call failed")
    ErrRustViking reports a rustviking CLI call that did not succeed, carrying
    the report the CLI made of it.

var ErrRustVikingMissing = errors.New("memory: rustviking CLI is not executable")
    ErrRustVikingMissing reports a file tier whose rustviking CLI is not
    executable on this machine.

var ErrScopeRequired = errors.New("memory: read scope names no owner, format or retention policy")
    ErrScopeRequired reports a read scope that names no owner, format or
    retention policy: it aligns with no reference, so binding it would authorize
    nothing and granting it would authorize everything, and it is refused by
    name instead.

var ErrSegmentBarrier = errors.New("memory: segment commit barrier failed")
    ErrSegmentBarrier reports a commit whose barrier did not complete, so the
    record reaches no reader and no count.

var ErrSegmentBodyMissing = errors.New("memory: committed event body is missing")
    ErrSegmentBodyMissing reports an event the index names but whose persisted
    body cannot be read: the read names the failure instead of publishing the
    event as an absent one.

var ErrSegmentLimit = errors.New("memory: segment request carries a negative byte limit")
    ErrSegmentLimit reports a segment request carrying a negative byte limit,
    so no bounded segment could answer it.

var ErrSegmentOffset = errors.New("memory: segment request carries a negative byte offset")
    ErrSegmentOffset reports a segment request carrying a negative byte offset,
    so no position could name where the segment starts.

var ErrSegmentPastEnd = errors.New("memory: segment starts past the last line")
    ErrSegmentPastEnd reports a segment request whose start line lies past the
    last stored line.

var ErrSegmentPosition = errors.New("memory: segment record carries a negative window or sequence")
    ErrSegmentPosition reports a segment record whose window or sequence slot is
    negative, so its key could not name a position.

var ErrSegmentRange = errors.New("memory: segment must start at line 1 or later and read at least one line")
    ErrSegmentRange reports a segment request that starts before the first line
    or asks for no lines at all.

var ErrSnapshotCorrupt = errors.New("memory: stored record is corrupt")
    ErrSnapshotCorrupt reports stored content that cannot be read back as the
    key-value state it claims to hold.

var ErrUnknownTier = errors.New("memory: unknown storage tier")
    ErrUnknownTier reports a storage config whose tier name matches no known
    tier.

TYPES

type AtomicStore interface {
	// Read returns the pairs the fetch names.
	Read(ctx context.Context, fetch Fetch) ([]Pair, error)
	// Keys returns the keys the prefix names, in key order, without reading
	// their values.
	Keys(ctx context.Context, prefix string) ([]string, error)
	// Namespaces returns the key namespaces the store holds, in name order,
	// without reading any value.
	Namespaces(ctx context.Context) ([]string, error)
	// Commit applies one batch behind one barrier.
	Commit(ctx context.Context, batch Batch) error
}
    AtomicStore is the storage engine every tier opens: one atomic commit unit
    and one read face that answers only bytes a completed commit landed.
      - Commit is all or nothing: a commit that returns an error publishes no
        part of its batch, and no later commit carries its bytes into a read
      - A commit takes exactly one barrier: a successful return means every
        value of the batch is durable and readable by a later process the engine
        persists across
      - An empty batch writes nothing and only completes that barrier
      - Read answers landed bytes only: an engine never serves a value whose
        commit did not return success
      - A commit whose durable outcome the engine cannot name returns
        ErrCommitUncertain, and the caller re-reads to decide instead of
        reporting success or failure
      - An engine that can decide before writing that it cannot carry the
        batch refuses it by name instead, so a refusal that landed no key never
        answers ErrCommitUncertain
      - That re-read is an open that took no part in the commit, because the
        view that committed publishes nothing from a commit it did not see
        return success: its own read answers absence even beside a batch that
        landed whole
      - ErrCommitUncertain never means that nothing landed: a caller that reads
        a refusal as zero writes re-commits a fact the store already holds
      - An engine whose every call lands one key keeps this contract by refusing
        a batch that carries several keys: it names ErrBatchNotAtomic and no
        key lands, and the tier's StoreCapabilities.AtomicBatch states the same
        limit
      - Failure injection belongs at this seam: a caller wraps an AtomicStore
        and fails, delays or drops a commit or a read, so the same injection
        runs over any engine and never names a file layout

func OpenAtomicStore(ctx context.Context, cfg StorageConfig) (AtomicStore, error)
    OpenAtomicStore opens the engine the config names, without opening a tier
    over it. A caller that wants to observe or fail commits opens the engine
    here, wraps it, and hands the wrapper back through StorageConfig.Engine:
    the wrapper is the same interface, so every engine takes the same injection.

type Batch struct {
	// Puts are the entries the commit writes.
	Puts []Pair
	// Deletes are the keys the commit removes.
	Deletes []string
}
    Batch is one write set committed as a unit: the keys it puts and the keys it
    deletes. A key stands on one side or the other, so the order the two sides
    are applied in names no different state.

type CommitOrder interface {
	EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error
}
    CommitOrder is the capability of stating the order an engine's commits
    landed in: the keys it holds beside the value each key's newest statement
    carries, named from the newest committed record to the oldest, stopping
    when visit answers false. An append-log engine keeps that order on disk,
    so naming it costs the records walked and the values those records state;
    a key-ordered key-value engine keeps no such order and does not answer this
    capability at all, so a caller reads the answer through the type assertion
    instead of a method every engine carries.
      - EachCommittedNewestFirst names a key once, at its newest commit,
        and skips a key the newest commit that touched it deleted
      - The value is the one that newest statement left under the key, so a
        caller identifies an entry without a second read
      - visit answers false to end the walk where it stands, so a caller reads
        the prefix its order bounds instead of the whole history

    The redline list (4 rules): MUST answer every key once, from the newest
    committed record to the oldest. MUST end the walk where visit answers
    false and return the error visit reports. MUST answer the value the newest
    statement left under the key, so a caller never reads it twice. MUST NOT
    name a key at an older commit or name a key the newest commit that touched
    it deleted.

type FactCommit struct {
	// Record is the segment record the fact commits as.
	Record SegmentRecord
	// Ref names the original the fact commits.
	Ref OriginalRef
	// Body is the original bytes the reference names.
	Body []byte
}
    FactCommit is one fact's whole key set: the record that places it and the
    original bytes the recall face reads back. The two name one fact, so the
    record's event key and the reference's key carry one identity.

type FactCommitOutcome int
    FactCommitOutcome names what one whole-fact commit left in the store.

const (
	// FactCommitted reports a fact that landed whole and took one sequence slot.
	FactCommitted FactCommitOutcome = iota
	// FactAlreadyPresent reports a fact the store already held whole: no key moved and no slot was taken.
	FactAlreadyPresent
	// FactConflict reports an identity the store holds under another content: no key moved.
	FactConflict
	// FactUncertain reports a commit whose durable outcome the store cannot name: only an open that took no part in it can decide what landed.
	FactUncertain
	// FactRefused reports a fact that landed no key, with the error naming the cause.
	FactRefused
)
func CommitFact(ctx context.Context, tier *Tier, fact FactCommit) (FactCommitOutcome, error)
    CommitFact commits one fact of one tier as one unit: the original family
    and the event family travel in one batch, so a reader meets the fact whole
    or not at all and neither family ever stands committed beside the other
    missing.
      - The duplicate judgment of both families and the commit under it run in
        one critical section, and the storage root's exclusive write rights keep
        every other writer out of that section, so no two writers interleave the
        judgment and the landing of one fact
      - A fact the store already holds stays the fact it is: no key moves,
        no sequence slot is taken and the window meta is not refreshed
      - A refusal the engine decides before writing is reported as the refusal
        it is, never as an uncertain outcome the caller would re-read: the seam
        states that a commit returning an error published no part of its batch,
        so only ErrCommitUncertain leaves the caller a read to make, and every
        other failure names a fact that landed no key
      - An uncertain outcome publishes nothing to either store's in-process
        state, because that view answers absence beside a batch that landed
        whole

type Fetch struct {
	// Key names the one entry to read.
	Key string
	// Prefix names the entries to read when Key is empty.
	Prefix string
}
    Fetch names one read: the single key it names, or every key carrying the
    prefix it names when no key is named. An empty fetch reads the whole store,
    ordered by key.

type FileSegmentStore struct {
	// Has unexported fields.
}
    FileSegmentStore commits events into time-window segments over one atomic
    store engine; it names no engine and no file layout, so any engine backs it.
    One commit stages the event body, its index entry, the window meta the
    window needs, the store's seat and the partition set as one batch,
    and publishes the event to the read cache and the event count only when that
    commit returns success. A commit that fails names the barrier as its step,
    carries its cause, unstages what it wrote, and publishes no part of the
    record.

    The resident state is bounded by the chain's shape and never by its history:
    the seat states the count and the commit sequence, the partition set states
    the partitions holding at least one event, and the read cache keeps bodies
    under a byte budget. Load reads the seat and the partition set and both
    are single keys, so opening a store reads no index entry whatever the
    history behind it. A point read probes the index entry of the identity in
    each stated partition and reads the value that entry spans, and a point
    read that names the partition probes that one entry and consults no set,
    so the set is an accelerator over the index and never the bound a caller
    that placed the event pays; a newest-first walk reads the commit order the
    engine states and stops where a caller stops; an enumeration reads the index
    of the partitions it names and therefore costs their entries. No window meta
    is held: the {pid}:meta:{window} key a window is filed under is the record,
    and the one commit that reads it reads that key.

func (s *FileSegmentStore) Close() error
    Close ends the store with a final barrier over whatever the backend still
    holds staged, releases the write rights this store's open took on its
    storage root and the journal handle behind them, and repeats as a no-op.

func (s *FileSegmentStore) EachEvent(ctx context.Context, visit func(StoredEvent) error) error
    EachEvent streams every published event in index order: partition first,
    then the window it is grouped under, then the sequence slot it takes inside
    that window, with the event key as the last tie-break. The body of each
    event is read on demand and handed to visit alone, so an enumeration never
    holds the whole store in memory. A visit error ends the enumeration and is
    returned.

    It reads the index of every partition the chain states, so the enumeration
    costs the entries of the whole history: that is the cost of the question,
    and no narrower answer names every event.

func (s *FileSegmentStore) EachEventOfPartitions(ctx context.Context, partitions []int, visit func(StoredEvent) error) error
    EachEventOfPartitions streams every published event of the named partitions
    in the index order EachEvent states, reading the index of those partitions
    alone. A nil or empty set names every partition the chain states. It is the
    face a scoped query reads: a caller that names the partitions it may read
    pays their entries rather than the entries of the whole chain.

    A partition the chain states no entry in answers nothing rather than a
    failure: a partition no commit filed an event under holds no event to
    present.

func (s *FileSegmentStore) EachIndexNewestFirst(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error) error
    EachIndexNewestFirst streams the index of the whole history in commit order,
    newest commit first, handing each entry its key, position, frozen event type
    and session without reading any body. It is the walk of a caller that names
    every entry — a replay, an export, a listing — and it holds no stop of its
    own, so a caller that reads a bounded prefix asks EachIndexNewestFirstUntil
    instead.

      - A visit that answers an error ends the walk and answers it.
      - A visit reads the store: the walk hands the engine back before every
        visit, so a visit reads one event body beside the walk and never waits
        on it.

func (s *FileSegmentStore) EachIndexNewestFirstFrom(ctx context.Context, from event.EventID, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) (bool, error)) error
    EachIndexNewestFirstFrom streams the index from the entry of one key onwards
    in the order EachIndexNewestFirstUntil states.
      - a caller that already knows the key it is looking for pays its own
        commit and the ones behind it instead of every commit filed after it
      - the walk starts where the engine's order-line index states the commit
        that last stated the key ended, which costs one probe of the identity
        index and one read of that index
      - so the walk costs the history committed after the key nothing
      - the seek is answered only once the walk names the key it was read for,
        because one order line states every key of its commit: a walk that ends
        without naming that key discards the seek and walks the index from the
        newest entry, so a word stating some other byte costs the walk its seek
        and never its answer
      - an engine that states no such index, a key this store holds no published
        entry of, and an entry the engine names no commit for all walk the index
        from the newest entry
      - so a seek that cannot be placed costs the walk it always took and never
        an entry it skipped

func (s *FileSegmentStore) EachIndexNewestFirstUntil(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) (bool, error)) error
    EachIndexNewestFirstUntil streams the index in commit order, newest commit
    first, and stops where the visit answers false: a point query reads the
    prefix its own answer bounds instead of the history behind it.

    The order is the one the engine's records sit in, so it ranks every entry
    against every other — a commit of one partition that landed after a commit
    of another comes first — and a caller that wants commit order walks it
    backwards. The walk streams the order the engine states, so it holds no
    entry between two visits and costs the commits it walks.

      - false is the stop: the walk ends where the visit stands and answers nil,
        so a query that has its answer pays the commits down to it and not the
        ones behind it.
      - An error ends the walk and is answered as the failure it is, so a stop
        is never spelled as a failure a caller has to recognize.
      - A visit reads the store: the walk hands the engine back before every
        visit, so a visit reads one event body beside the walk and never waits
        on it.

func (s *FileSegmentStore) EventCount() int
    EventCount returns the number of events published by successful commits,
    so the count moves only where durable success was reported.

func (s *FileSegmentStore) Events(ctx context.Context) ([]StoredEvent, error)
    Events returns every published event in index order. It is the materialized
    form of EachEvent for callers that need the whole set; the assembly read
    faces stream instead, one event at a time.

func (s *FileSegmentStore) GetEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error)
    GetEvent returns the committed event under key together with the index
    position it was committed at, and reports whether any is published.
    The position is the index entry of that identity in one of the partitions
    the chain states, so the answer costs one small read per partition and no
    walk of the history behind them. A body the cache evicted is read back
    from its persisted entry; an index entry whose body cannot be read reports
    ErrSegmentBodyMissing instead of an absent event.

      - This read serves the callers an identity reaches without a partition:
        the seat is the only index such a caller has, because enumerating the
        index behind it is the walk the seat replaces, so the answer is exactly
        as wide as the partitions the seat states and the seat that under-states
        one answers absence beside a body the log holds.
      - A caller that holds the partition SHALL name it with
        GetEventInPartition, which addresses that partition's index entry and
        consults no seat.

func (s *FileSegmentStore) GetEventInPartition(ctx context.Context, key event.EventID, partition int) (StoredEvent, bool, error)
    GetEventInPartition returns the committed event under key that one named
    partition files, and reports whether that partition published one. The read
    addresses the index entry of the identity in the partition the caller names,
    so it pays one small read and probes no partition set. A caller that names
    the partition pays that read whatever the seat states. A body the cache
    evicted is read back from its persisted entry; an index entry whose body
    cannot be read reports ErrSegmentBodyMissing instead of an absent event.

func (s *FileSegmentStore) IndexEntriesRead() int
    IndexEntriesRead reports the index entries this store's load enumerated:
    none beside a chain that states its seat and its partition set,
    and the whole index of every partition beside a chain that states neither.
    It is the quantity a bounded recovery is measured by, and never EventCount,
    which counts the whole committed history: a load that enumerated every entry
    answers the same number twice, which is exactly the unbounded recovery the
    measure has to be able to tell apart from a bounded one.

func (s *FileSegmentStore) IndexWalkReads() (visits int, valueBytes int64, valueReads int)
    IndexWalkReads reports what the commit-order walks of this store cost:
    the index entries they named, the value bytes they read to name them and how
    many values those bytes were. It is the quantity a bounded boot is measured
    by after the entries stopped being materialized, because a walk that reads
    a value per held key and one that reads the index family alone name the
    same entries at different costs, and only the bytes tell the two apart.
    Both are counted by the walk, so a caller reads what the boot paid, never an
    estimate kept beside it.

func (s *FileSegmentStore) Load(ctx context.Context) error
    Load states the count, the commit sequence and the partitions the chain
    holds events in, without reading one index entry and without returning an
    event body: one read of the seat states all three, so the open costs one
    small read whatever the history behind it.
      - The seat states the count, the commit sequence and the partitions of the
        whole history with one read of one key, so neither the count nor the set
        of partitions a point read probes costs a reader an aggregate over the
        entries behind them
      - A seat the chain does not state, states unreadably, states a commit
        sequence below its count, or states a count beside no partition falls
        back to the count and the partitions the index states: one enumeration
        of the index, which is the derived answer a chain that states no seat
        costs
      - A store that writes again states the seat in the frame of its next
        commit, so a root whose seat could not be read is read without an
        aggregate from the next commit on

func (s *FileSegmentStore) NewestOfType(ctx context.Context, typ string) (event.EventID, int, bool, error)
    NewestOfType answers the identity of the newest index entry of one frozen
    event type beside the partition that entry landed in: the walk reads the
    store's commit order from the newest commit backwards and stops at the
    first entry of the type, so a lookup beside a checkpoint filed every cadence
    commits reads a bounded prefix of the order whatever the history behind it,
    sorts nothing, and reads no event body.

      - The answer spans partitions: an identity is newer than another because
        its commit landed later, never because its window, its sequence slot or
        its partition is higher, so a run whose owner moved between processes
        reads the checkpoint its newest process filed.
      - The partition travels beside the identity because the walk already
        holds the position it answered, so the caller reads that body back
        with GetEventInPartition and pays one read instead of a probe of the
        partitions the seat states.
      - A type the store holds no entry of answers found=false, partition 0 and
        no error.

func (s *FileSegmentStore) StoreEvent(ctx context.Context, rec SegmentRecord) error
    StoreEvent commits one event behind the segment commit barrier: the event
    body, its index entry and the window meta the window needs are one batch
    committed before the event reaches the read cache, the partition set,
    the event count or the caller.

      - A repeat of the committed record returns success without a second fact,
        other bytes under the same event key are refused, and a failed commit
        leaves nothing published.
      - A fact the store holds is a committed fact: committing it again answers
        as the duplicate it is, never as a second landing of one identity.

type Integrity struct {
	// Digest is the digest over the stored bytes, in the canonical text form of
	// the algorithm that produced it.
	Digest string
	// Length is the exact byte length the stored original must have.
	Length int64
}
    Integrity is the completeness information of one original blob: the digest
    computed over its stored bytes and the byte length those bytes must have.

    A read returns the digest and the length together with the bytes, so a
    truncated or substituted original is reported as a failed read instead of
    being served as if it were complete.

type MigrateReport struct {
	// Keys is the number of keys the target now holds.
	Keys int
	// Bytes is the payload the copy committed, summed over its keys.
	Bytes int64
	// Batches is the number of commits the copy took to land them.
	Batches int
}
    MigrateReport counts what one migration copied.

func MigrateShardedRoot(ctx context.Context, sourceRoot, targetRoot string) (report MigrateReport, err error)
    MigrateShardedRoot copies every key of a retired sharded-snapshot root into
    a journal root under targetRoot, in bounded batches, and reads the copy back
    through a freshly opened view before it reports success.

    The source is read-only: this function only enumerates and reads it,
    so the build that wrote that layout still reads it after the copy,
    and a migration interrupted anywhere leaves the source exactly as it was.
    The target is refused when it already holds keys, so no copy overwrites
    a root in use. A root whose copy does not read back byte for byte returns
    ErrMigrateMismatch naming the key that differs, and a copied root whose
    reference names no independent body returns ErrSnapshotCorrupt naming the
    orphaned key, the same refusal the original store's load reports at boot.
    The copy writes the target, so it takes that root's write rights and gives
    them up when it returns, and the verifying pass it takes in between opens
    the copy as a component view that reads beside the writer instead of being
    refused by it.

type OriginalRef struct {
	// Key is the EventID that owns this original. Different keys never share
	// a body, even when their bytes are identical.
	Key event.EventID
	// Owner is the address the reference belongs to. It is part of the
	// reference scope, so two owners never overwrite each other.
	Owner event.Address
	// Format is the original format the reference was registered in. It is part
	// of the reference scope.
	Format string
	// RetentionPolicy is the retention policy the reference is held under. It is
	// part of the reference scope.
	RetentionPolicy string
	// Integrity is the completeness information of the stored bytes.
	Integrity Integrity
}
    OriginalRef names one stored original blob together with the reference
    metadata that scopes it: the owner that registered the reference,
    the format it was registered in, the retention policy it is held under,
    and the completeness information of its bytes.

    Every event key owns an independent body. Reference metadata stays separate:
    references that differ in owner, format or retention policy MUST NOT
    overwrite one another, and a later commit MUST NOT invalidate an earlier
    reference.

func (r OriginalRef) FactKey() string
    FactKey returns the EventID owning the original. The reference scope (owner,
    format and retention policy) keeps references registered side by side; every
    EventID owns an independent body, even when another event has identical
    bytes.

func (r OriginalRef) FactType() string
    FactType returns the frozen type a reference ticket is read as.

func (r OriginalRef) ReadScope() ReadScope
    ReadScope returns the read scope this reference was written under:
    the owner, format and retention policy it registered with. A reader bound to
    exactly this scope is a reader this reference is visible to; a reader bound
    to any other scope never aggregates it.

type OriginalStore interface {
	// Put commits the independent body owned by ref.Key. Owner, format and
	// retention policy scope the reference metadata, so an accepted reference
	// is never invalidated by a later Put.
	Put(ctx context.Context, ref OriginalRef, body []byte) error

	// Get returns the exact recall result for key: the original bytes,
	// byte-identical to the bytes Put accepted, or a named
	// missing/expired/failed classification (see RecallResult), so a caller
	// tells the three outcomes apart instead of reading one bare error.
	//
	// A bare key is not a read authorization (see ReadScope): the visibility
	// decision here aggregates every reference of the key whatever scope it was
	// registered under. Authorized reads run through ScopedOriginalStore and
	// ScopedReader, where the trusted scope the read runs inside decides which
	// references take part.
	Get(ctx context.Context, key event.EventID) RecallResult

	// ReadSegment returns at most numLines lines of the original stored under
	// key starting at line startLine, so an oversized original is consumed by
	// segment rather than returned whole.
	//
	// The bound is a line count, not a byte count: one oversized line is
	// returned whole whatever numLines is. The byte-bounded face is
	// SegmentRequest and SegmentResult, served by ScopedReader.ReadSegment and
	// ScopedOriginalStore.ReadSegmentScoped, which cut at byte boundaries and
	// never at line boundaries.
	ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
}
    OriginalStore is the original-text port: it commits the bytes a reference
    names and returns them byte for byte, whole or as segments.

    The redline list (4 rules): MUST commit the bytes a reference names before
    any bounded preview or segment entry exists. MUST return the original byte
    for byte, whole or as segments. MUST NOT answer a read with a model view in
    place of the original. MUST NOT invalidate an accepted reference by a later
    Put.

type Pair struct {
	// Key is the entry's key.
	Key string
	// Value is the entry's bytes.
	Value []byte
}
    Pair is one key-value entry a read returns or a commit writes.

type ReadScope struct {
	// Owner is the address the reads are authorized for.
	Owner event.Address
	// Format is the original format the reads are authorized for.
	Format string
	// RetentionPolicy is the retention policy the reads are authorized under.
	RetentionPolicy string
}
    ReadScope is the trusted read scope one reader runs inside: the owner,
    format and retention policy its reads are authorized under, aligned with the
    metadata a reference was written under.

    - A read scope is not a caller's claim: it is bound once from trusted state
    when a scoped reader is constructed, and the values a caller merely types
    grant nothing. - Holding a bare key is not a read authorization: a read
    aggregates only the references registered inside its scope, so a key whose
    references all lie outside it reads as missing, its existence not leaked.
    - The triple is the one references are registered under (see refScope),
    read here as authorization rather than as deduplication identity: the two
    scopes decide different things and neither stands in for the other. - The
    tiers keep their baseline faces beside their storage logic; the visibility
    contract they share lives here, beside the read contract it serves.

func (s ReadScope) Validate() error
    Validate returns the named refusal that rejects this scope before any read
    runs under it, or nil when the scope may be bound. The only refusal today
    is the zero scope: it names no owner, no format and no retention policy,
    aligns with no reference, and is refused by name instead of being read as
    authorization for everything.

type RecallOutcome int
    RecallOutcome names the classification of one exact recall result.

const (
	// RecallUnclassified is the zero value: the outcome was not determined, so
	// it is never reported as a successful recall.
	RecallUnclassified RecallOutcome = iota
	// RecallFound means the original bytes were returned.
	RecallFound
	// RecallMissing means no original is stored under the key.
	RecallMissing
	// RecallExpired means the original is outside the retention window of its policy.
	RecallExpired
	// RecallFailed means the read itself failed and Err carries the cause.
	RecallFailed
)
type RecallResult struct {
	// Key is the identity that was recalled.
	Key event.EventID
	// Outcome is the named classification of this result.
	Outcome RecallOutcome
	// Body holds the returned original bytes, and is set only when Outcome is
	// RecallFound.
	Body []byte
	// Err carries the cause when Outcome is RecallFailed, and is nil otherwise.
	Err error
}
    RecallResult is the outcome of exact recall for one key: the original bytes
    when they were found, and otherwise a named classification of why they were
    not, so a miss is reported instead of silently omitted.

type ScopedOriginalStore interface {
	// GetScoped returns the exact recall of key as visible inside scope: the
	// committed bytes while the reference registered inside scope is inside its
	// retention window, a named expired result when that reference is past its
	// window whatever the other references of the key say, a named missing
	// result when no reference names the key inside scope, and a named failed
	// result carrying the cause when the read itself fails. A key whose
	// references all lie outside scope reads as missing, so its existence is
	// not leaked.
	GetScoped(ctx context.Context, key event.EventID, scope ReadScope) RecallResult

	// ReadSegmentScoped returns one byte-bounded segment of the original stored
	// under key as visible inside scope, on exactly the terms of SegmentRequest
	// and SegmentResult. A read it cannot serve inside scope is named by the
	// same errors GetScoped classifies with — ErrOriginalMissing for a key with
	// no reference inside scope, ErrOriginalExpired for one past its window —
	// so an out-of-scope key is refused as missing and leaks nothing.
	ReadSegmentScoped(ctx context.Context, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error)
}
    ScopedOriginalStore is the original-text read face that decides visibility
    inside one trusted read scope: it serves the reads that aggregate only
    the references registered inside the scope they name, and nothing else.
    The bare commit and recall face of OriginalStore stays on the store itself,
    so holding this port never hands a caller an unauthorized read.

    The redline list (3 rules): MUST aggregate only the references registered
    inside the scope the read names. MUST NOT hand a caller the bare commit or
    recall face of the store itself. MUST NOT leak the existence of a key whose
    references all lie outside the scope, nor be implemented by a store that
    cannot decide visibility inside one.

type ScopedReader struct {
	// Has unexported fields.
}
    ScopedReader is an original-text reader bound to one trusted read scope:
    the scope is bound once at construction, and every read it serves runs
    inside that scope, so authorization travels with the reader and never with a
    bare key.

    Holding a key is not a read authorization (see ReadScope): a read through
    this reader aggregates only the references registered inside the bound
    scope, and a key whose references lie outside it reads as missing.

func NewScopedReader(store ScopedOriginalStore, scope ReadScope) (*ScopedReader, error)
    NewScopedReader derives a reader bound to scope from store. The scope comes
    from the trusted state of the caller, not from the per-read calls the
    reader then serves, and a scope that names nothing is refused by name with
    ErrScopeRequired before any read runs.

func (r *ScopedReader) Get(ctx context.Context, key event.EventID) RecallResult
    Get returns the exact recall of key inside the bound scope, on the terms of
    ScopedOriginalStore.GetScoped: references registered outside the scope take
    no part in the visibility decision, so a key the scope cannot see reads as
    missing and leaks nothing.

func (r *ScopedReader) ReadSegment(ctx context.Context, key event.EventID, req SegmentRequest) (SegmentResult, error)
    ReadSegment returns one byte-bounded segment of the original stored under
    key, inside the bound scope: the request names the byte position to resume
    from — carried forward from the Next of the previous result — and the
    byte bound one segment may return, and the result carries the segment,
    the position the next segment resumes from and whether the end of the
    original was reached. Segments are cut at byte boundaries whatever the
    content holds and concatenate back to the exact bytes; a request with a zero
    MaxBytes is served under DefaultSegmentMaxBytes and never without a bound
    (see SegmentRequest).

func (r *ScopedReader) Scope() ReadScope
    Scope returns the trusted read scope the reader is bound to, so a consumer
    can record which authority a result was read under.

type SegmentPosition struct {
	// Offset is the byte offset from the start of the stored original.
	Offset int64
}
    SegmentPosition names one byte position inside a stored original, so one
    segmented read resumes exactly where the segment before it ended.

type SegmentRecord struct {
	// PartitionID is the partition the event belongs to.
	PartitionID int
	// WindowTS is the time window the event is grouped under.
	WindowTS int64
	// Seq is the sequence slot the event takes inside that window.
	Seq int64
	// EventKey is the canonical identity of the event.
	EventKey event.EventID
	// EventTime is the actual event time the window meta accounts for.
	EventTime int64
	// Body is the event payload, committed byte for byte.
	Body []byte

	// Has unexported fields.
}
    SegmentRecord is one event on its way into a segment: the partition and
    window it belongs to, the sequence slot it takes, its canonical event key,
    the event time the window meta covers, and the event bytes committed
    verbatim.

func (r SegmentRecord) WithChain(typ string, session event.SessionID) SegmentRecord
    WithChain states the frozen event type and session this record carries in
    the same batch as its body. The index keeps them as a projection of the
    fact, so a restart can group by type and session without reading event
    bodies.

type SegmentRequest struct {
	// From is the byte position the segment starts at.
	From SegmentPosition
	// MaxBytes bounds the bytes one segment returns. Zero means
	// DefaultSegmentMaxBytes.
	MaxBytes int64
}
    SegmentRequest asks for one byte-bounded segment of a stored original: the
    byte position to resume from and the maximum number of bytes to hand back.

    The position is the continuation form: it is filled from the Next of the
    previous SegmentResult, so one original is consumed segment by segment
    and the segments of one original concatenate back to its exact bytes.
    The bound is in bytes and always finite: a zero MaxBytes is served as
    DefaultSegmentMaxBytes and never as an unbounded read, a negative MaxBytes
    is refused by ErrSegmentLimit, a negative offset by ErrSegmentOffset,
    and an offset past the end of the stored original by ErrSegmentPastEnd.

type SegmentResult struct {
	// Body holds the bytes of this segment.
	Body []byte
	// Next is the byte position the following segment resumes from.
	Next SegmentPosition
	// EOF reports that this segment reached the end of the stored original.
	EOF bool
}
    SegmentResult is one byte-bounded segment of a stored original: the bytes of
    this segment, the byte position the next segment resumes from, and whether
    the end of the original was reached. Segments are cut at byte boundaries
    whatever the content holds, so a multi-byte character or a line ending may
    span two segments and the segments of one original concatenate back to
    its exact bytes. Next is the zero position when EOF is set and carries no
    resumption point then.

type StorageConfig struct {
	// Type names the tier to open.
	Type StorageType
	// Root is the directory the localfile and file tiers persist under.
	Root string
	// RustVikingBinary names or locates the rustviking executable; empty means
	// "rustviking" looked up on PATH.
	RustVikingBinary string
	// RustVikingConfig is the config file passed to the rustviking CLI; empty
	// means the config.toml under Root.
	RustVikingConfig string
	// Engine, when set, is the engine the tier opens over instead of the one Type
	// names. It is the seam a caller wraps to observe, delay or fail commits: the
	// wrapper implements the same AtomicStore, so the injection works over any
	// engine and never names a file layout. A tier beside no wrapper opens the
	// engine Type names.
	Engine AtomicStore
	// WriteRights declares that this open writes the root it names: it holds that
	// root's exclusive write lock for as long as the view lives, so a second such
	// open, in this process or another, is refused by name. A caller that commits
	// to the root declares it, and the boot that stands for one runtime instance
	// over that root does too. An open that declares nothing is a component view:
	// it holds no write rights and reads beside the writer that does.
	WriteRights bool
	// RetentionWindows registers, per retention policy, the window an original
	// recall keeps the references of that policy readable for. A policy with no
	// registered window keeps its references readable indefinitely, and a window
	// of zero or less reports every read of that policy as expired.
	RetentionWindows map[string]time.Duration
}
    StorageConfig locates one storage tier: the tier name, the root directory
    the persistent tiers persist under, and the rustviking executable and config
    the file tier drives.

type StorageType string
    StorageType names one storage tier of the original and segment stores.

const (
	// StorageMemory is the in-process tier: it holds only what the process holds
	// and declares no recovery across process exit.
	StorageMemory StorageType = "memory"
	// StorageLocalFile is the retired name of the durable local tier. The build
	// opens it as the journal, the one durable local engine, and refuses a root
	// that still holds the retired sharded-snapshot layout by name: a deployment
	// moves such a root with MigrateShardedRoot and a caller that names no layout
	// selects StorageJournal. The name is kept because deployments and checks
	// still carry it; renaming it is a separate cleanup, not a second engine.
	StorageLocalFile StorageType = "localfile"
	// StorageJournal is the durable local tier: key values are the records of one
	// append-only journal under a root directory. A commit is one frame, and the
	// log's sync is its only barrier.
	StorageJournal StorageType = "journal"
	// StorageFile is the tier whose key values live in a rustviking store reached
	// through the rustviking CLI.
	StorageFile StorageType = "file"
)
type StoreCapabilities struct {
	// Tier is the tier the capabilities describe.
	Tier StorageType
	// Durable reports whether a successful commit outlives the process that made
	// it.
	Durable bool
	// AtomicBatch reports whether a commit carrying several keys lands whole or
	// not at all. A tier that declares false lands one key per call and answers
	// ErrBatchNotAtomic for every larger batch, having landed none of it.
	AtomicBatch bool
}
    StoreCapabilities names what one opened tier guarantees, so a tier that
    holds nothing across process exit says so instead of appearing durable.

type StoredEvent struct {
	// Key is the canonical identity of the event.
	Key event.EventID
	// PartitionID is the partition the event was committed into.
	PartitionID int
	// WindowTS is the time window the event was grouped under.
	WindowTS int64
	// Seq is the sequence slot the event was committed into.
	Seq int64
	// Body is the committed event payload.
	Body []byte
}
    StoredEvent is one committed event as a read returns it: the bytes that were
    committed and the index position they were committed at.

type Tier struct {
	// Originals commits and recalls original bytes.
	Originals OriginalStore
	// Segments commits events behind the segment commit barrier.
	Segments *FileSegmentStore
	// Store is the engine this tier holds, as the interface every engine
	// implements: the stores above read and commit through it, and a caller never
	// names the engine behind it.
	Store AtomicStore
	// Capabilities names what the backend guarantees.
	Capabilities StoreCapabilities

	// Has unexported fields.
}
    Tier is one opened storage tier: the original store and the segment store of
    the same backend, together with the capabilities that backend guarantees and
    the engine both stores commit through.

func OpenTier(ctx context.Context, cfg StorageConfig) (*Tier, error)
    OpenTier opens one storage tier over its backend.
      - The memory tier holds only what the process holds; the durable local
        tier appends to the journal under Root; the file tier drives the
        rustviking CLI and reports ErrRustVikingMissing when no such executable
        is found
      - Each call opens its own store over the same backend, so a caller reads
        exactly what an earlier process committed
      - A config that carries an engine opens the tier over that engine instead,
        so a caller wraps the engine at the seam and the tier takes the wrapper
        unchanged
      - The journal tier's write path is one append-only file with one writer
        handle per opened view, and each commit is appended as one whole frame,
        so a view reading beside a writer cannot see half a frame
      - One directory carries one writer: an open that declares
        StorageConfig.WriteRights holds the root's write lock, and a second
        such open, in this process or another, is refused by name with
        ErrRootWriteLocked, so two writers never commit over one slot
      - This open hands the declaration through unchanged, so a caller that
        commits to the root declares the rights and every other caller holds
        none: a component view opens beside the live writer, a commit carrying
        keys through it is refused as ErrCommitWithoutWriteRights, and Close
        gives the root up to the next writer
      - The kernel holds the lock, so it drops when the holding process exits;
        no recovery pass clears a stale lock, and durability never depended on
        the lock, because the barrier is the log's own sync
      - The config's RetentionWindows register the original-recall window of
        each retention policy on the tier's original store, so a read of a
        reference outside its registered window is named expired; a policy the
        config leaves unregistered keeps its references readable indefinitely

func (t *Tier) Close() error
    Close ends this tier's view of its backend: the segment store completes its
    final barrier, the engine behind it closes, and a durable tier gives its
    storage root up to the next writer.
      - The root's write rights leave with this close, so the next writer — in
        this process or another — is admitted instead of being refused for a
        view that already ended
      - The release happens however the barrier ended, because the engine closes
        the journal handle and the write lock together, so a failed barrier
        never leaves the root locked
      - A repeated Close ends nothing twice and answers nil, so a caller that
        closes a run twice does not see an error the first close did not report
      - The engine closes once, through the segment store, because both stores
        of a tier read and commit through that one engine

type TierFacts struct {
	// Has unexported fields.
}
    TierFacts is the durable fact surface of one opened tier.

func TierFactLog(tier *Tier) *TierFacts
    TierFactLog returns the durable fact surface of one opened tier, so a
    durable commit and a replay run on the same engine the tier's events are
    committed through — no second database is introduced for either.

func (l TierFacts) Get(ctx context.Context, key string) ([]byte, bool, error)
    Get answers the value held under one key, and whether the log holds it: the
    point read a caller that names its own keys pays instead of a prefix scan,
    so the read costs the one value and the probe that locates it.

func (l TierFacts) Put(ctx context.Context, key string, value []byte) error
    Put stages one key and its value.

func (l TierFacts) Scan(ctx context.Context, prefix string) (map[string][]byte, error)
    Scan returns every stored pair under the key prefix as key and value.

func (l TierFacts) Sync(ctx context.Context) error
    Sync completes the write barrier over everything staged so far.
