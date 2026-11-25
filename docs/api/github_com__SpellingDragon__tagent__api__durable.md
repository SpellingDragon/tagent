package durable // import "github.com/SpellingDragon/tagent/api/durable"

Package durable holds the durable fact surface every fact ledger is written
through and replayed from: one key stages one fact, a prefix scan replays the
facts a log already holds, and the sync barrier is where a caller may report
durable success. The surface is a write-and-read obligation, not a fact family
and not an engine: it names no fact it stores, offers no delete and no key
order, and so a ledger of task facts, of governance events and of approval
attention facts all speak it alike. Its signatures carry built-in types only,
so any backend satisfies it structurally and no package edge is required in
either direction — the memory tier's key-value port among them.

A log that also answers one key by name states that read through its own method,
and a caller naming its own keys asks for it through the narrow capability its
consumer declares, so this surface stays the obligation every backend satisfies
while a point read costs the one key where the log states it. A point read
that crosses a hot log and an archive answers in one order — the hot index, the
archive manifest's key domain, the segment index, the segment bytes — and names
its failures apart: ErrOriginalMissing for a key neither statement carries,
ErrArchiveMissing for a segment or record the archive states and does not hold,
and ErrArchiveCorrupt for bytes that do not match their recorded digest or
checksum. A storage that cannot be read is never answered as a fact that does
not exist.

TYPES

type FactLog interface {
	// Put stages one key and its value.
	Put(ctx context.Context, key string, value []byte) error
	// Scan returns every stored pair under the key prefix as key and value.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
	// Sync completes the write barrier over everything staged so far, so a
	// caller reports durable success only behind a barrier that returned.
	Sync(ctx context.Context) error
}
    FactLog is the durable fact surface a ledger stores facts through: put,
    prefix scan and the sync barrier a commit reports its success behind.

    The redline list (4 rules): MUST stage one key and its value without
    claiming it durable. MUST complete the write barrier over everything staged
    before anyone reports durable success. MUST NOT report staged bytes as
    stored. MUST NOT answer a prefix scan with a subset of the pairs under that
    prefix.
