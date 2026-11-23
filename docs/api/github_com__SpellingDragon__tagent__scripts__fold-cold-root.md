Command fold-cold-root states the facts a root holds across its hot and cold
journals in a new root, in the commit order the index entries of those facts
carry. A build that reads one journal cannot serve a root whose cold journal
holds part of that history, so an operator folds such a root before this build
serves it: the hot journal is `kv/journal.log` under the root and the cold one
is `cold/kv/journal.log`.

The fold is a one-time move outside the framework: the source root is read-only,
the target root states no key, and a source the fold cannot read whole is
refused rather than read as complete.

  - scan: without --restore the run writes nothing, stating the keys the folded
    root would hold and naming the ones it leaves behind
  - restore: the run holds the write rights of -root, states at most
    foldedBatchKeys original keys per commit, and reads every identity back
    through a freshly opened engine
  - offline source: the run stamps every journal of the source before it reads
    any of them and states them again once it has read them all, so a journal
    that moved under the read refuses the run instead of being folded as one
    consistent read of two journals
  - refusals: ledger.ErrMigrateSourceUnchanged, ErrSourceUnstated,
    ErrSourceConflict, ErrSourceIncomplete, ledger.ErrRootWriteLocked,
    ledger.ErrBatchNotAtomic

VARIABLES

var ErrSourceConflict = errors.New("fold-cold-root: the source states one identity two ways")
    ErrSourceConflict reports a source root that states one identity two ways:
    one key the two journals hold with different bytes, one commit two events
    claim, or a value the key's own record cannot decode.

var ErrSourceIncomplete = errors.New("fold-cold-root: the source holds part of an event")
    ErrSourceIncomplete reports a source root that holds part of an event: an
    index entry with no body or no window meta, a body or a window meta no index
    entry names, or a cold journal beside no statement of the commits it holds.

var ErrSourceUnstated = errors.New("fold-cold-root: the source root holds no hot journal")
    ErrSourceUnstated reports a source root that holds no hot journal, the one
    directory every durable root of this build writes.
