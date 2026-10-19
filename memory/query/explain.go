package query

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// LeaseRetention is the retained state of one execution resource beside the
// facts its ledger holds it on: the lease deadline, the renewals it received,
// the probe readings recorded against it and the obligations kept open beside
// it. The reading is a projection of what the ledger records and adds no fact
// of its own.
type LeaseRetention struct {
	// Ref names the lease registration of the resource.
	Ref state.LeaseRef
	// Held reports whether the ledger still holds the resource under its key.
	Held bool
	// Deadline is the lease deadline the resource is held to.
	Deadline time.Time
	// Renewed counts the lease renewals the resource received.
	Renewed int
	// Adopted reports whether the resource was reattached rather than started.
	Adopted bool
	// ConsecutiveUnknowns counts the undecidable probes since the last probe
	// that did decide. The count is a diagnosis reading: it escalates diagnosis
	// beside the resource and never decides a death of it.
	ConsecutiveUnknowns int
	// Escalations counts the diagnosis escalations raised against the resource.
	Escalations int
	// StopReason carries the reason the last stop of the resource stayed
	// unconfirmed; it is empty beside a stop the ledger observed confirmed.
	StopReason string
	// Obligations lists the obligations the ledger keeps open beside the lease.
	Obligations []string
}

// LeaseLedgerCounts is the counting face of one execution resource ledger: the
// counts the ledger keeps over the leases it acquired, renewed, swept and
// freed. Every number is read from the ledger beside the retention of its
// resources, so a caller reads one accounting rather than two.
type LeaseLedgerCounts struct {
	// Held counts the resources the ledger currently holds.
	Held int
	// Acquired counts the resources started under a lease.
	Acquired int
	// Adopted counts the resources reattached under a lease.
	Adopted int
	// Reused counts the acquisitions answered from a resource already held
	// instead of starting a second one.
	Reused int
	// Released counts the resources freed beside a confirmed stop.
	Released int
	// Escalated counts the diagnosis escalations undecidable probes raised.
	Escalated int
}

// LeaseExplainer reads one execution resource ledger: the retained state of one
// resource beside the counts the ledger keeps over its leases. It is a read
// face over facts a ledger owner already holds and records nothing of its own.
// 红线清单（3 条）：
// MUST 只读：回答账本持有者已记录的事实，自身不记录第二份
// MUST 让账本未持有的 ref 以 not held 回答，绝不臆造保留态
// MUST NOT 把逐类计数合成一个总量：分类计数就是审计面
type LeaseExplainer interface {
	// ExplainLease returns the retained state of the resource registered under
	// ref. A key the ledger holds nothing under answers as not held.
	ExplainLease(ref state.LeaseRef) (LeaseRetention, error)
	// LeaseCounts returns the counting face of the ledger.
	LeaseCounts() LeaseLedgerCounts
}

// AdvancerExecutor names the executor running a call as the party that moves
// its open execution obligation forward.
const AdvancerExecutor Advancer = "executor"

// AdvancerDiagnosis names the diagnosis escalation of a run of undecidable
// probes as the advancement action of that run: an undecidable observation
// raises diagnosis beside the resource and never decides a death of it.
const AdvancerDiagnosis Advancer = "diagnosis"

// AdvancerExplicitStop names the explicit stop handling of a retained execution
// resource as the party that frees it beside a confirmed stop.
const AdvancerExplicitStop Advancer = "explicit-stop"

// ErrLeaseExplainerMissing reports an explain read asked to explain the
// retention of a registered lease beside no execution resource ledger to read
// it from. The absence is named instead of answering a retention in silence.
var ErrLeaseExplainerMissing = errors.New("query: no execution resource ledger is attached to this read")

// Blocker is one named runtime blocker: an obligation that cannot proceed, or
// an execution resource retained under its key. Each blocker carries the
// identity it is recorded under, the readable reason beside it and the party
// that advances it, so one blocker is explained by name rather than reported as
// a nameless stall.
type Blocker struct {
	// Ref names the object the blocker sits on: the task whose obligation
	// cannot proceed, or the lease whose resource is retained.
	Ref ObjectRef
	// Notice names the blocked notification; its zero value marks a blocker
	// that sits on a retained resource rather than on a notification.
	Notice task.Notice
	// Reason is the readable reason the blocker cannot proceed, kept exactly as
	// its owner records it.
	Reason string
	// Advancer names the next party that advances this blocker: the executor,
	// notification transport, diagnosis escalation or explicit stop handling it
	// waits on.
	Advancer Advancer
	// State is how far the advance of this blocker has got.
	State AdvanceState
	// Retention carries the retention facts when the blocker sits on a retained
	// execution resource; it stays zero beside a blocked notification.
	Retention LeaseRetention
}

// OpenObligation is one obligation a task manager still holds, explained beside
// the party that advances it. An obligation stays listed while its owner holds
// it open: a queue that holds nothing is not read as a settled obligation.
type OpenObligation struct {
	// Ref names the task the obligation belongs to.
	Ref ObjectRef
	// Obligation is the open obligation as its owner classifies it.
	Obligation task.Obligation
	// Advancer names the next party that advances this obligation.
	Advancer Advancer
	// State is how far the advance of this obligation has got.
	State AdvanceState
}

// BlockerCounts is the derived reading of one report: the numbers counted off
// the very entries the report lists beside them. The counts are taken after the
// authorization filter, so an object the caller may not read is excluded before
// anything is counted.
type BlockerCounts struct {
	// Blockers counts the blockers listed in the report.
	Blockers int
	// Open counts the obligations still open listed in the report.
	Open int
	// Leases counts the lease retentions listed in the report.
	Leases int
}

// BlockerReport is one authorized, read-only explanation of what keeps the
// runtime from advancing: every blocker named beside its reason and its next
// advancer, every obligation still open, and every registered lease explained
// beside the retention facts of its resource.
type BlockerReport struct {
	// Blockers lists every blocker of the filtered view in the stable identity
	// order of the objects they sit on.
	Blockers []Blocker
	// Open lists every obligation a task manager still holds for the filtered
	// view, each beside its own advancer.
	Open []OpenObligation
	// Leases explains every lease registration of the filtered view beside the
	// retention facts of its resource; a registration whose key the ledger
	// holds nothing under reads as Held false.
	Leases []LeaseRetention
	// Counts is derived from the three lists above after the authorization
	// filter.
	Counts BlockerCounts
	// Ledger is the counting face of the execution resource ledger, read beside
	// the retentions; it stays zero beside a read run with no ledger attached.
	Ledger LeaseLedgerCounts
}

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Explainer is the read-only blocker explanation service of the runtime: it
// reads the owners' own records at read time and answers with every blocker
// named beside its reason and the party that advances it, under the
// authorization scope the caller already holds.
//
// The service holds faces and nothing else: it records nothing, keeps no copy
// of a blocker between two reads and grants no authorization of its own. The
// authorization filter runs before any owner is read and before anything is
// counted, so an object outside the presented scope is never read and never
// counted.
type Explainer struct {
	inv    *Inventory
	leases LeaseExplainer
}

// NewExplainer returns the blocker explanation service over one runtime
// inventory and one execution resource ledger. The lease face may be absent for
// an assembly beside no execution resources: the reads then explain the task
// and notification blockers and refuse a registered lease by name instead of
// answering its retention in silence.
func NewExplainer(inv *Inventory, leases LeaseExplainer) *Explainer {
	return &Explainer{inv: inv, leases: leases}
}

// Explain returns one authorized explanation of what keeps the runtime from
// advancing: the blockers, the obligations still open and the lease retentions,
// each beside the party that advances it and beside the accounting of the
// execution resource ledger. A source that cannot be read refuses the read by
// name beside the dimension it affects instead of answering an empty report as
// a normal one.
func (e *Explainer) Explain(ctx context.Context, filter ListFilter) (BlockerReport, error) {
	view, err := e.inv.View(ctx)
	if err != nil {
		return BlockerReport{}, err
	}
	report := BlockerReport{}
	for _, entry := range view.Tasks() {
		ref := taskDetailOf(entry).Ref
		if !refAllowed(filter, ref) {
			continue
		}
		for _, open := range entry.Obligations.Open {
			report.Open = append(report.Open, OpenObligation{
				Ref:        ref,
				Obligation: open,
				Advancer:   advancerOf(open.Kind),
				State:      AdvanceWaiting,
			})
		}
		for _, record := range entry.Obligations.Blocked {
			report.Blockers = append(report.Blockers, Blocker{
				Ref:      ref,
				Notice:   record.Notice,
				Reason:   record.Reason,
				Advancer: AdvancerNoticeTransport,
				State:    AdvanceWaiting,
			})
		}
	}
	for _, entry := range view.Leases() {
		ref := ObjectRef{Kind: ObjectLease, Lease: entry.Lease}
		if !refAllowed(filter, ref) {
			continue
		}
		if e.leases == nil {
			return BlockerReport{}, &SourceError{Dimension: ObjectLease, Cause: ErrLeaseExplainerMissing}
		}
		holding, err := e.leases.ExplainLease(entry.Lease)
		if err != nil {
			return BlockerReport{}, &SourceError{Dimension: ObjectLease, Cause: err}
		}
		report.Leases = append(report.Leases, holding)
		report.Blockers = append(report.Blockers, leaseBlockersOf(holding)...)
	}
	sortBlockers(report.Blockers)
	sortOpenObligations(report.Open)
	sortRetentions(report.Leases)
	if e.leases != nil {
		report.Ledger = e.leases.LeaseCounts()
	}
	report.Counts = BlockerCounts{
		Blockers: len(report.Blockers),
		Open:     len(report.Open),
		Leases:   len(report.Leases),
	}
	return report, nil
}

// advancerOf names the party that moves one open obligation forward: the
// executor running the call settles an execution obligation, and the
// notification transport delivers the notification a delivery obligation points
// at.
func advancerOf(kind task.ObligationKind) Advancer {
	switch kind {
	case task.ObligationAwaitingTool:
		return AdvancerExecutor
	default:
		return AdvancerNoticeTransport
	}
}

// leaseBlockersOf projects the retention of one resource into its named
// blockers: one per obligation the ledger keeps open beside the lease, one for a
// stop that stayed unconfirmed and one for a run of undecidable probes, each
// beside the advancement action its own cause waits on.
func leaseBlockersOf(holding LeaseRetention) []Blocker {
	if !holding.Held {
		return nil
	}
	ref := ObjectRef{Kind: ObjectLease, Lease: holding.Ref}
	var out []Blocker
	for _, obligation := range holding.Obligations {
		out = append(out, Blocker{
			Ref:       ref,
			Reason:    obligation,
			Advancer:  AdvancerExplicitStop,
			State:     AdvanceWaiting,
			Retention: holding,
		})
	}
	if holding.StopReason != "" {
		out = append(out, Blocker{
			Ref:       ref,
			Reason:    holding.StopReason,
			Advancer:  AdvancerExplicitStop,
			State:     AdvanceWaiting,
			Retention: holding,
		})
	}
	if holding.ConsecutiveUnknowns > 0 {
		out = append(out, Blocker{
			Ref:       ref,
			Reason:    fmt.Sprintf("%d undecidable probe(s) raised diagnosis beside the retained resource", holding.ConsecutiveUnknowns),
			Advancer:  AdvancerDiagnosis,
			State:     AdvanceWaiting,
			Retention: holding,
		})
	}
	return out
}

// sortBlockers orders the blockers of one report into the stable identity order
// of the objects they sit on, the notification identities and the reasons
// beside them.
func sortBlockers(list []Blocker) {
	sort.SliceStable(list, func(i, j int) bool {
		if refLess(list[i].Ref, list[j].Ref) {
			return true
		}
		if refLess(list[j].Ref, list[i].Ref) {
			return false
		}
		if noticeLess(list[i].Notice, list[j].Notice) {
			return true
		}
		if noticeLess(list[j].Notice, list[i].Notice) {
			return false
		}
		return list[i].Reason < list[j].Reason
	})
}

// sortOpenObligations orders the open obligations of one report into the stable
// identity order of the tasks they belong to, the notification identities and
// the classes beside them.
func sortOpenObligations(list []OpenObligation) {
	sort.SliceStable(list, func(i, j int) bool {
		if refLess(list[i].Ref, list[j].Ref) {
			return true
		}
		if refLess(list[j].Ref, list[i].Ref) {
			return false
		}
		if noticeLess(list[i].Obligation.Notice, list[j].Obligation.Notice) {
			return true
		}
		if noticeLess(list[j].Obligation.Notice, list[i].Obligation.Notice) {
			return false
		}
		return list[i].Obligation.Kind < list[j].Obligation.Kind
	})
}

// sortRetentions orders the lease retentions of one report into the stable
// identity order of their registrations.
func sortRetentions(list []LeaseRetention) {
	sort.SliceStable(list, func(i, j int) bool {
		return refLess(
			ObjectRef{Kind: ObjectLease, Lease: list[i].Ref},
			ObjectRef{Kind: ObjectLease, Lease: list[j].Ref},
		)
	})
}
