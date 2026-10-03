package org // import "github.com/SpellingDragon/tagent/agent/org"

FUNCTIONS

func LastDiscardOrder() ([]string, bool)
    LastDiscardOrder returns the order a previous Discard unwound, or false when
    nothing has been discarded yet. It exists because the cleanup contract is
    otherwise unobservable; no production path reads it.

TYPES

type Deps struct {
	// Resident is the published owner table the candidate reads and must not
	// touch until its single commit point.
	Resident *agent.ResidentTopology
	// Builder constructs owners (assembly mode fixed by the root's closure).
	Builder ShellBuilder
	// SetFP records a name's memory-section fingerprint; DropFP resets one the
	// candidate never published.
	SetFP  func(name, fingerprint string)
	DropFP func(name string)
	// UnregisterOwner revokes a store-owner registration taken during a build.
	UnregisterOwner func(name string)
}
    Deps is the injection surface of a candidate build: everything the mechanism
    needs from the composition root, and nothing else.

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

type Overlay struct {
	// Has unexported fields.
}
    Overlay is the private construction domain of ONE candidate — the shape
    reload established and rollback is required to share.

    - Owners the online table lacks are built into the overlay cache, never into
    the resident table, so no concurrent reader sees a not-yet-published owner.
    - Each build or registration enters the ordered responsibility table
    immediately, recorded before the error is checked, so a failed parent is
    unwindable. - Commit merges at the caller's single commit point; Abandon
    unwinds in reverse acquisition order whenever the candidate never publishes.

func BuildOwners(d Deps, next *config.Config, reach map[string]bool, fail func(site string, err error)) (*Overlay, bool)
    BuildOwners constructs every owner `reach` needs that is not resident yet
    (hot-add for reload, re-acquisition of a retired owner for rollback — the
    same operation on both entries). Fail-closed: on ok=false nothing was
    published and everything acquired has already been unwound; the caller still
    defers Abandon to cover its own later failures.

func (o *Overlay) Abandon()
    Abandon unwinds every responsibility the overlay took on, in reverse
    acquisition order, and is idempotent. After Commit it is a no-op:
    the published candidate owns those resources now, and tearing them down here
    would pull them out from under live work.

func (o *Overlay) Commit()
    Commit merges the overlay into the resident table — the single point where a
    new owner becomes visible to concurrent readers, which the caller performs
    inside its own commit critical section.

func (o *Overlay) PendingNames() []string
    PendingNames reports the owners awaiting the commit point (logging only).

func (o *Overlay) Resolve() map[string]*agent.TagentAgent
    Resolve is this candidate's face-build domain: resident owners ∪ its own
    new owners. Wrappers resolve through it, which is what keeps hot-adds
    publishable and unchanged owners zero-construction.

type RetireDecision struct {
	Name    string
	Retired bool
	Held    bool
	Why     string
}
    RetireDecision is one sweep step's outcome for one name, returned for
    logging by the caller (which owns the reload's log prefix).

type ShellBuilder func(name string, acfg config.AgentConfig, cfg config.Config, cache map[string]*agent.TagentAgent) (*agent.TagentAgent, error)
    ShellBuilder constructs one resident owner on a candidate configuration.
    The composition root supplies it as a closure that already knows its runtime
    state and build mode, so the mechanism here never reaches into assembly
    internals.

type Txn struct {
	// Has unexported fields.
}
    Txn is the ordered responsibility table of ONE candidate (S-B/2.3「事务」):
    every resource acquisition, owner registration and agent construction is
    recorded IMMEDIATELY — before the error is checked, so a failed parent
    is still unwindable — and earlier than the next fallible action. Discard
    unwinds in REVERSE acquisition order (partial registration revoked → built
    agent Closed → fingerprint reset), and that order comes from the recorded
    evidence rather than any difference over owned names or map iteration order.
    Reload and rollback share this structure.

func NewTxn(dropFP func(name string), unregisterOwner func(name string)) *Txn
    NewTxn builds an empty responsibility table. dropFP resets a name's memory
    fingerprint and unregisterOwner revokes its store-owner registration;
    both are supplied by the composition root, which owns the books they touch.

func (tx *Txn) Acquire(name string, a *agent.TagentAgent)
    Acquire records a responsibility the candidate has taken on: a fully built
    agent (a != nil) or a partial owner registration whose build failed midway
    (a == nil — its store lease was already released by the builder's own
    cleanup, so discard only revokes the registration). Idempotent per name:
    a dependency acquired by an earlier top is not re-recorded.

func (tx *Txn) Discard() []string
    Discard unwinds every responsibility in REVERSE acquisition order and
    returns that order (also recorded into the discard-order witness). Per item:
    the store-owner registration is revoked, the memory fingerprint is reset,
    and a built agent is Closed (its own Close releases the store lease;
    a partial registration has no lease left to release). Each entry revokes
    its registration whether or not an agent was built — a leftover entry
    could later refuse an unrelated agent that recycled the same heap address,
    reported as a partition collision. The published online face is never
    touched: discard only ever runs before the single commit point.
