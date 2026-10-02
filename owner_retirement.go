// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
package tagent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
)

// retirementLedger is the assembly's list of owners whose name left the routable
// set (design D7, R02). Removal itself is atomic and already done by
// the publish: the new generation simply does not route the name. What the
// publish must NOT do is close the owner — its executions, background work and
// accepted inputs may still depend on it — so the owner goes on this list and is
// retired only once every obligation it still carries has disappeared.
//
// The ledger is bounded by REAL pending obligations, not by history: an entry
// leaves as soon as the owner is retired, and re-enters nothing when a name is
// re-routed before its drain finished (that owner is simply reused, which is also
// why a re-add can never produce a second writer for a live store).
type retirementLedger struct {
	mu sync.Mutex
	// pending maps a retired name to the owner still awaiting quiescence.
	pending map[string]*agent.TagentAgent
	// held maps a name to the close error that keeps it listed.
	held map[string]error
	// usageOf reports how many live execution generations still hold a USAGE RIGHT
	// on a name: a version that routed B may legitimately call B until
	// its own references drain, even if it never called B and even if the current
	// generation removed the route. The number is DERIVED by the agent layer from
	// the bindings' own published faces (single routing truth) — the ledger
	// only reads it and never registers anything. Nil means the axis is absent.
	usageOf func(name string) int
}

func newRetirementLedger(usageOf func(name string) int) *retirementLedger {
	return &retirementLedger{pending: map[string]*agent.TagentAgent{}, held: map[string]error{}, usageOf: usageOf}
}

// usageHolders answers the usage axis for one name, tolerating its absence.
func (l *retirementLedger) usageHolders(name string) int {
	if l.usageOf == nil {
		return 0
	}
	return l.usageOf(name)
}

// track puts a removed name on the list. Idempotent: re-tracking a name that is
// already pending (e.g. two publishes in a row without a drain) keeps the FIRST
// instance, which is the one still holding the store.
func (l *retirementLedger) track(name string, owner *agent.TagentAgent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, seen := l.pending[name]; !seen {
		l.pending[name] = owner
	}
}

// release drops a name that became routable again before it drained. The owner
// instance stays resident and serving; nothing was closed, so there is nothing to
// restore.
func (l *retirementLedger) release(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pending, name)
	delete(l.held, name)
}

// drop removes a name whose retirement completed.
func (l *retirementLedger) drop(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pending, name)
	delete(l.held, name)
}

// noteKeep records WHY the owner stays listed after a close attempt (an execution
// that never confirmed a stop is held, not force-closed — a guarantee
// inherited here rather than flattened).
func (l *retirementLedger) noteKeep(name string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held[name] = err
}

// snapshot copies the pending set so a sweep can iterate without holding the
// ledger's lock across each owner's close sequence.
func (l *retirementLedger) snapshot() map[string]*agent.TagentAgent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]*agent.TagentAgent, len(l.pending))
	for n, a := range l.pending {
		out[n] = a
	}
	return out
}

// hasPending reports whether any owner is still draining. The turn-boundary check
// uses it to decide whether a background sweep is worth scheduling at all when the
// config file itself did not change — so an owner whose work just settled is
// retired at the next activity without a dedicated timer goroutine.
func (l *retirementLedger) hasPending() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pending) > 0
}

// closingIn answers D7's refusal question BEFORE any candidate resource is built:
// a name the new generation wants is on the retirement list and its owner has
// already begun closing (or its close was refused by an unconverged execution), so
// reusing it is impossible and admitting a fresh one would be a second writer for
// the same storage identity. Returns the blocked names, sorted for deterministic
// refusal text.
func (l *retirementLedger) closingIn(reach map[string]bool) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for name, owner := range l.pending {
		if !reach[name] || owner == nil {
			continue
		}
		if owner.CloseStarted() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// diagnostics reports what is still held and why — pending retirement is a real
// state the host must be able to see (an owner kept alive by its own unfinished
// work is legitimate; an owner kept alive invisibly is not). The usage axis is
// derived by walking live bindings, so it is read only after the ledger lock is
// released: a diagnostics path must never invert the ledger → agent lock order.
func (l *retirementLedger) diagnostics() []map[string]any {
	l.mu.Lock()
	if len(l.pending) == 0 {
		l.mu.Unlock()
		return nil
	}
	type rec struct {
		name      string
		ob        string
		heldSince string
	}
	names := make([]string, 0, len(l.pending))
	for n := range l.pending {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]rec, 0, len(names))
	for _, n := range names {
		r := rec{name: n, ob: l.pending[n].Obligations().String()}
		if err, held := l.held[n]; held {
			r.heldSince = err.Error()
		}
		recs = append(recs, r)
	}
	l.mu.Unlock()

	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		entry := map[string]any{"name": r.name, "obligations": r.ob}
		if r.heldSince != "" {
			entry["heldSince"] = r.heldSince
		}
		if holders := l.usageHolders(r.name); holders > 0 {
			entry["usageHeldBy"] = holders
		}
		out = append(out, entry)
	}
	return out
}

// retireablePending reports whether any pending name is CURRENTLY unblocked — the
// same predicate a sweep acts on (unrouted, no usage right left, no obligation of
// its own), evaluated without changing anything. The drain tail asks it once after
// a pass because a release that landed DURING that pass was merged away by the
// assembly's single-flight gate: without this re-notice, a cascade (owner A exits →
// the name A was holding becomes free) would stall until new traffic arrived, which
// the retirement protocol forbids. Termination is structural: it can only answer true when a pass would
// actually retire something, and every retirement shrinks the pending set.
//
// Called with the reload's `mu` held (publishedReach is `mu`'s state).
func (l *retirementLedger) retireablePending(reach map[string]bool) bool {
	for name, owner := range l.snapshot() {
		if owner == nil || reach[name] {
			continue
		}
		if l.usageHolders(name) > 0 {
			continue
		}
		if !owner.Obligations().Idle() {
			continue
		}
		return true
	}
	return false
}

// retireDecision is one sweep step's outcome for one name, returned for logging
// by the caller (which owns the reload's log prefix).
type retireDecision struct {
	Name    string
	Retired bool
	Held    bool
	Why     string
}

// sweep evaluates every pending owner once: still routable (release), still
// obliged (keep), or idle (close, then retire). Called with the reload's `mu`
// held, so it can never race a publish's track/release for the same name. An owner is held either by its own unfinished work or by another
// generation’s still-valid usage right (deferred delegation); that second term
// is derived from live bindings, so a holder cannot slip through the way a
// hand-maintained registration would let one slip.
func (l *retirementLedger) sweep(reach map[string]bool) []retireDecision {
	var decisions []retireDecision
	for name, owner := range l.snapshot() {
		if owner == nil {
			l.drop(name)
			continue
		}
		if reach[name] {
			l.release(name)
			decisions = append(decisions, retireDecision{Name: name, Why: "re-routed before drain finished — original owner reused"})
			continue
		}
		holders := l.usageHolders(name)
		if ob := owner.Obligations(); !ob.Idle() || holders > 0 {
			why := "obligations remain: " + ob.String()
			if holders > 0 {
				why += fmt.Sprintf("; usage rights held by %d live generation(s)", holders)
			}
			decisions = append(decisions, retireDecision{Name: name, Why: why})
			continue
		}
		if err := owner.Close(); err != nil {
			l.noteKeep(name, err)
			decisions = append(decisions, retireDecision{Name: name, Held: true, Why: "close left it unconverged: " + err.Error()})
			continue
		}
		l.drop(name)
		decisions = append(decisions, retireDecision{Name: name, Retired: true, Why: "obligations converged; exclusive components closed, lease released, registrations revoked"})
	}
	return decisions
}

var _ = fmt.Sprintf
