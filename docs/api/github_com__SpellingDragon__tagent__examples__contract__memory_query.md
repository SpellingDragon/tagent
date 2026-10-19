package memoryqueryexample // import "github.com/SpellingDragon/tagent/examples/contract/memory_query"

Package memoryqueryexample holds the canonical example of every read face
the runtime inventory gathers through: one file per interface, each a minimal
implementation beside the Example function that drives it through its real
caller.

FUNCTIONS

func ExampleDetailSource()
    ExampleDetailSource materializes one page of a frozen reading and reports
    an identity outside that reading as not held instead of serving an empty
    detail; a reference carrying content fields beside its identity answers with
    the same entry.

func ExampleDetail_blockedObligations()
    ExampleDetail_blockedObligations reads the blocked list of one task detail
    through the real projection: each blocked obligation keeps its own reason
    and its own advancement entry, and the zero advance state is never read as
    settled.

    - It prints reason and state per blocked obligation: "the named executor
    has not confirmed its stop not-settled" and "the storage action is running
    not-settled".

func ExampleDetail_refusal()
    ExampleDetail_refusal drives both named refusals of a detail read through
    the real projection: an object the caller is not authorized to read and an
    object the bounded snapshot does not hold each refuse by name instead of
    answering an empty detail.

func ExampleLeaseExplainer()
    ExampleLeaseExplainer explains one held lease beside the counts of the
    ledger, and reports a key the ledger holds nothing under as not held.

func ExampleReading()
    ExampleReading gathers one reading of the three semantic units: the
    generation in effect, the session sites it serves and the tasks the task
    owner holds.

func ExampleReadingSource()
    ExampleReadingSource freezes one reading and reports that the pages of that
    revision describe the same content beside the position of the source they
    were read from.

func ExampleReading_assembly()
    ExampleReading_assembly reads through the assembled path: one snapshot
    beside its cursor, one page of that revision, one session detail and one
    agent detail asked for by a reference that carries content fields. The
    session detail reads the mailbox of the one state.Session the site holds,
    the agent asked for beside a session answers with the canonical reference of
    that agent identity, and the entries answer in the stable identity order of
    the reading.

