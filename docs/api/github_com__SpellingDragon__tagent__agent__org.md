package org // import "github.com/SpellingDragon/tagent/agent/org"

TYPES

type Ledger struct {
	// Has unexported fields.
}
    Ledger is the assembly's list of owners whose name left the routable set
    (design D7, R02). Removal itself is atomic and already done by the publish:
    the new generation simply does not route the name. What the publish must NOT
    do is close the owner — its executions, background work and accepted inputs
    may still depend on it — so the owner goes on this list and is retired only
    once every obligation it still carries has disappeared.

    The ledger is bounded by REAL pending obligations, not by history: an entry
    leaves as soon as the owner is retired, and re-enters nothing when a name is
    re-routed before its drain finished (that owner is simply reused, which is
    also why a re-add can never produce a second writer for a live store).

func NewLedger(usageOf func(name string) int) *Ledger
    NewLedger builds an empty retirement ledger. usageOf supplies how many
    live holders a name still has, which is the usage axis of the three-way
    retirement judgement.

func (l *Ledger) ClosingIn(reach map[string]bool) []string
    ClosingIn answers D7's refusal question BEFORE any candidate resource is
    built: a name the new generation wants is on the retirement list and its
    owner has already begun closing (or its close was refused by an unconverged
    execution), so reusing it is impossible and admitting a fresh one would be
    a second writer for the same storage identity. Returns the blocked names,
    sorted for deterministic refusal text.

func (l *Ledger) Diagnostics() []map[string]any
    Diagnostics reports what is still held and why — pending retirement is a
    real state the host must be able to see (an owner kept alive by its own
    unfinished work is legitimate; an owner kept alive invisibly is not).
    The usage axis is derived by walking live bindings, so it is read only after
    the ledger lock is released: a diagnostics path must never invert the ledger
    → agent lock order.

func (l *Ledger) Drop(name string)
    Drop removes a name whose retirement completed.

func (l *Ledger) HasPending() bool
    HasPending reports whether any owner is still draining. The turn-boundary
    check uses it to decide whether a background sweep is worth scheduling at
    all when the config file itself did not change — so an owner whose work just
    settled is retired at the next activity without a dedicated timer goroutine.

func (l *Ledger) NoteKeep(name string, err error)
    NoteKeep records WHY the owner stays listed after a close attempt (an
    execution that never confirmed a stop is held, not force-closed — a
    guarantee inherited here rather than flattened).

func (l *Ledger) Release(name string)
    Release drops a name that became routable again before it drained. The owner
    instance stays resident and serving; nothing was closed, so there is nothing
    to restore.

func (l *Ledger) RetireablePending(reach map[string]bool) bool
    RetireablePending reports whether any pending name is CURRENTLY unblocked
    — the same predicate a sweep acts on (unrouted, no usage right left, no
    obligation of its own), evaluated without changing anything. The drain tail
    asks it once after a pass because a release that landed DURING that pass was
    merged away by the assembly's single-flight gate: without this re-notice,
    a cascade (owner A exits → the name A was holding becomes free) would
    stall until new traffic arrived, which the retirement protocol forbids.
    Termination is structural: it can only answer true when a pass would
    actually retire something, and every retirement shrinks the pending set.

    Called with the reload's `mu` held (publishedReach is `mu`'s state).

func (l *Ledger) Snapshot() map[string]*agent.TagentAgent
    Snapshot copies the pending set so a sweep can iterate without holding the
    ledger's lock across each owner's close sequence.

func (l *Ledger) Sweep(reach map[string]bool) []RetireDecision
    Sweep evaluates every pending owner once: still routable (release),
    still obliged (keep), or idle (close, then retire). Called with the reload's
    `mu` held, so it can never race a publish's track/release for the same
    name. An owner is held either by its own unfinished work or by another
    generation’s still-valid usage right (deferred delegation); that second term
    is derived from live bindings, so a holder cannot slip through the way a
    hand-maintained registration would let one slip.

func (l *Ledger) Track(name string, owner *agent.TagentAgent)
    Track puts a removed name on the list. Idempotent: re-tracking a name that
    is already pending (e.g. two publishes in a row without a drain) keeps the
    FIRST instance, which is the one still holding the store.

func (l *Ledger) UsageHolders(name string) int
    UsageHolders answers the usage axis for one name, tolerating its absence.

type RetireDecision struct {
	Name    string
	Retired bool
	Held    bool
	Why     string
}
    RetireDecision is one sweep step's outcome for one name, returned for
    logging by the caller (which owns the reload's log prefix).
