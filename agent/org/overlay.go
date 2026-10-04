// 本文件承载候选的私有构造域：新属主建在覆盖层而非在线表，单点提交、逆序放弃。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
package org

import (
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ShellBuilder constructs one resident owner on a candidate configuration. The
// composition root supplies it as a closure that already knows its runtime state
// and build mode, so the mechanism here never reaches into assembly internals.
type ShellBuilder func(name string, acfg config.AgentConfig, cfg config.Config, cache map[string]*agent.TagentAgent) (*agent.TagentAgent, error)

// Deps is the injection surface of a candidate build: everything the mechanism
// needs from the composition root, and nothing else.
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

// Overlay is the private construction domain of ONE candidate — the shape reload
// established and rollback is required to share.
//
// - Owners the online table lacks are built into the overlay cache, never into the resident table, so no concurrent reader sees a not-yet-published owner.
// - Each build or registration enters the ordered responsibility table immediately, recorded before the error is checked, so a failed parent is unwindable.
// - Commit merges at the caller's single commit point; Abandon unwinds in reverse acquisition order whenever the candidate never publishes.
type Overlay struct {
	deps       Deps
	cache      map[string]*agent.TagentAgent
	added      map[string]*agent.TagentAgent
	addedNames []string
	txn        *Txn
	committed  bool
	discarded  bool
}

// BuildOwners constructs every owner `reach` needs that is not resident yet
// (hot-add for reload, re-acquisition of a retired owner for rollback — the same
// operation on both entries). Fail-closed: on ok=false nothing was published and
// everything acquired has already been unwound; the caller still defers Abandon to
// cover its own later failures.
func BuildOwners(d Deps, next *config.Config, reach map[string]bool, fail func(site string, err error)) (*Overlay, bool) {
	ov := &Overlay{
		deps:  d,
		cache: d.Resident.Snapshot(),
		added: map[string]*agent.TagentAgent{},
		txn:   NewTxn(d.DropFP, d.UnregisterOwner),
	}
	var newNames []string
	for aname := range reach {
		if d.Resident.Get(aname) == nil {
			newNames = append(newNames, aname)
		}
	}
	sort.Strings(newNames)

	for _, aname := range newNames {
		if config.RemoteDeclarationOnly(next, aname) {
			continue
		}
		acfg, defined := next.Agents[aname]
		if !defined {
			ov.Abandon()
			fail(fmt.Sprintf("NEW agent %q is referenced but not defined", aname), nil)
			return nil, false
		}
		_, berr := d.Builder(aname, acfg, *next, ov.cache)
		for n, a := range ov.cache {
			if a == nil || d.Resident.Get(n) != nil {
				continue
			}
			if _, dup := ov.added[n]; dup {
				continue
			}
			ov.added[n] = a
			ov.addedNames = append(ov.addedNames, n)
			ov.txn.Acquire(n, a)
			mc := next.Agents[n]
			if d.SetFP != nil {
				d.SetFP(n, config.AgentMemoryFingerprint(&mc))
			}
		}
		if berr != nil {
			ov.txn.Acquire(aname, nil)
			ov.Abandon()
			fail(fmt.Sprintf("build for %q FAILED", aname), berr)
			return nil, false
		}
	}
	return ov, true
}

// Resolve is this candidate's face-build domain: resident owners ∪ its own new
// owners. Wrappers resolve through it, which is what keeps hot-adds publishable
// and unchanged owners zero-construction.
func (o *Overlay) Resolve() map[string]*agent.TagentAgent {
	if o == nil {
		return nil
	}
	return o.cache
}

// PendingNames reports the owners awaiting the commit point (logging only).
func (o *Overlay) PendingNames() []string {
	if o == nil {
		return nil
	}
	return o.addedNames
}

// Commit merges the overlay into the resident table — the single point where a
// new owner becomes visible to concurrent readers, which the caller performs
// inside its own commit critical section.
func (o *Overlay) Commit() {
	if o == nil || o.committed {
		return
	}
	o.committed = true
	if len(o.added) > 0 {
		o.deps.Resident.Add(o.added)
	}
}

// Abandon unwinds every responsibility the overlay took on, in reverse
// acquisition order, and is idempotent. After Commit it is a no-op: the published
// candidate owns those resources now, and tearing them down here would pull them
// out from under live work.
func (o *Overlay) Abandon() {
	if o == nil || o.committed || o.discarded {
		return
	}
	o.discarded = true
	o.deps.Resident.Unpublish(o.addedNames)
	order := o.txn.Discard()
	if len(order) > 0 {
		log.Warnf("[org-hotreload] refused candidate: unwound %d responsibility(ies) in reverse acquisition order %v; online topology untouched", len(order), order)
	}
	o.added = map[string]*agent.TagentAgent{}
	o.addedNames = nil
}
