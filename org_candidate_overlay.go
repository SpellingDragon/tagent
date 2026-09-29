package tagent

import (
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// candidateOverlay is the private construction domain of ONE candidate — the
// shape S-B that reload established and rollback is required to share
// instead of keeping its own rebuild branch:
//
//   - owners a candidate needs but the online table lacks are built into the
//     overlay's cache, NEVER into the resident table, so no concurrent reader
//     can see a not-yet-published owner (「不提前公开新增 owner」);
//   - each build/registration enters the ordered responsibility table
//     immediately (recorded before the error is checked, so a failed parent is
//     unwindable too);
//   - commit() merges at the caller's single commit point, and abandon() unwinds
//     everything in reverse acquisition order when the candidate never publishes
//     — including a failure in the LAST stage (face build／activation), which is
//     exactly what the rollback branch used to leave half-applied.
type candidateOverlay struct {
	rc         *runtimeConfig
	cache      map[string]*agent.TagentAgent
	added      map[string]*agent.TagentAgent
	addedNames []string
	txn        *candidateTxn
	committed  bool
	discarded  bool
}

// buildCandidateOwners constructs every owner `reach` needs that is not resident
// yet (hot-add for reload, re-acquisition of a retired owner for rollback — the
// same operation on both entries). Fail-closed: on ok=false nothing was
// published and everything acquired has already been unwound; the caller still
// defers abandon() to cover its own later failures.
func buildCandidateOwners(
	rc *runtimeConfig,
	loader *prompt.Loader,
	next *Config,
	reach map[string]bool,
	fail func(site string, err error),
) (*candidateOverlay, bool) {
	ov := &candidateOverlay{
		rc:    rc,
		cache: rc.resident.Snapshot(),
		added: map[string]*agent.TagentAgent{},
		txn:   newCandidateTxn(rc),
	}
	var newNames []string
	for aname := range reach {
		if rc.resident.Get(aname) == nil {
			newNames = append(newNames, aname)
		}
	}
	sort.Strings(newNames)

	for _, aname := range newNames {
		if remoteDeclarationOnly(next, aname) {
			continue
		}
		acfg, defined := next.Agents[aname]
		if !defined {
			ov.abandon()
			fail(fmt.Sprintf("NEW agent %q is referenced but not defined", aname), nil)
			return nil, false
		}
		_, berr := buildAgent(aname, acfg, *next, rc, loader, ov.cache, buildModeResident)
		for n, a := range ov.cache {
			if a == nil || rc.resident.Get(n) != nil {
				continue
			}
			if _, dup := ov.added[n]; dup {
				continue
			}
			ov.added[n] = a
			ov.addedNames = append(ov.addedNames, n)
			ov.txn.acquire(n, a)
			mc := next.Agents[n]
			rc.residentMemFP[n] = agentMemoryFingerprint(&mc)
		}
		if berr != nil {
			ov.txn.acquire(aname, nil)
			ov.abandon()
			fail(fmt.Sprintf("build for %q FAILED", aname), berr)
			return nil, false
		}
	}
	return ov, true
}

// resolve is this candidate's face-build domain: resident owners ∪ its own new
// owners. Wrappers resolve through it, which is what keeps hot-adds publishable
// and unchanged owners zero-construction.
func (o *candidateOverlay) resolve() map[string]*agent.TagentAgent {
	if o == nil {
		return nil
	}
	return o.cache
}

// pendingNames reports the owners awaiting the commit point (logging only).
func (o *candidateOverlay) pendingNames() []string {
	if o == nil {
		return nil
	}
	return o.addedNames
}

// commit merges the overlay into the resident table — the single point where a
// new owner becomes visible to concurrent readers, which the caller performs
// inside its own commit critical section.
func (o *candidateOverlay) commit() {
	if o == nil || o.committed {
		return
	}
	o.committed = true
	if len(o.added) > 0 {
		o.rc.resident.Add(o.added)
	}
}

// abandon unwinds every responsibility the overlay took on, in reverse
// acquisition order, and is idempotent. After commit() it is a no-op: the
// published candidate owns those resources now, and tearing them down here
// would pull them out from under live work.
func (o *candidateOverlay) abandon() {
	if o == nil || o.committed || o.discarded {
		return
	}
	o.discarded = true
	o.rc.resident.Unpublish(o.addedNames)
	order := o.txn.discard()
	if len(order) > 0 {
		log.Warnf("[org-hotreload] refused candidate: unwound %d responsibility(ies) in reverse acquisition order %v; online topology untouched", len(order), order)
	}
	o.added = map[string]*agent.TagentAgent{}
	o.addedNames = nil
}
