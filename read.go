package tagent

import (
	"context"
	"errors"
	"sort"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Query serves one bounded, immutable reading of the live scene through the
// read-only projection, filtered by the authorization scope the caller already
// holds.
//
// The reading grants no authorization of its own and derives every count from
// the detail entries it returns: an object outside the presented scope is
// excluded rather than rewritten, and a source that cannot be read is refused
// by name instead of being answered as empty detail.
func (r *Run) Query(ctx context.Context, filter query.ListFilter) (query.Snapshot, error) {
	snapshot, _, err := r.projection.Snapshot(ctx, filter)
	return snapshot, err
}

// Explain returns one authorized explanation of what keeps this run from
// advancing: every blocker named beside its readable reason and the party that
// advances it, every obligation still open, and every registered lease beside
// the retention facts of its resource, under the authorization scope the
// caller presents. The read grants no authorization of its own and reads every
// fact from the owners at read time.
func (r *Run) Explain(ctx context.Context, filter query.ListFilter) (query.BlockerReport, error) {
	return r.explainer.Explain(ctx, filter)
}

// Recall runs one retrieval over the recall face of this run and answers the
// reference set the query hit, ordered from the best hit down and cut to the
// page the query asks for. The retrieval reads through the observation table
// over the committed store and the searcher fusing its two legs, so a hit is
// answered as the recall reference its event was landed with and never as a
// copy of the text behind it: the original stays behind the scoped read path.
// The query carries its own authorized partition set — the recall grants no
// authorization of its own — and a run assembled beside no storage tier is
// refused by name with ErrRetrievalUnwired instead of answered as an empty set.
func (r *Run) Recall(ctx context.Context, q search.Query) (search.Result, error) {
	if r.retrieval == nil {
		return search.Result{}, ErrRetrievalUnwired
	}
	return r.retrieval.searcher.Search(ctx, q)
}

// retrievalSupply is the retrieval face one run consumes recall through: the
// observation table over the events its storage tier committed, the vector
// seam the embedding leg borrows its vectors from, and the searcher reading
// both legs through that table. It is wired once at the composition root over
// the production components the retrieval domain ships — a query consumes an
// event after the table observes it, and never at the moment its bytes happen
// to be committed — and it holds no second copy of any event.
type retrievalSupply struct {
	source   *memory.StoreQuerySource
	seam     *memory.VectorSeam
	searcher *search.StoreSearcher
}

// supplyRetrieval wires the retrieval face over one opened storage tier: the
// observation table lands every event the tier has published, each landed
// event submits its vector generation without waiting for it, and the searcher
// fuses the two legs of every query back into the one ordered reference set.
// A nil embedder names no vector generation at all: no seam is built, no
// embedding is ever called, and the searcher answers through the keyword leg
// alone — the zero-change behaviour the embedding contract states. The
// embedding text of one event is its stored content, falling back to its
// stored summary when the content is empty.
func supplyRetrieval(ctx context.Context, tier *ledger.Tier, embedder memory.Embedder) (*retrievalSupply, error) {
	source := memory.NewStoreQuerySource()
	if err := source.ObserveStore(ctx, tier.Segments); err != nil {
		return nil, err
	}
	supply := &retrievalSupply{source: source}
	if embedder == nil {
		supply.searcher = search.NewStoreSearcher(source, nil, nil)
		return supply, nil
	}
	events, err := tier.Segments.Events(ctx)
	if err != nil {
		return nil, err
	}
	seam := memory.NewVectorSeam(0)
	seam.Bind(embedder)
	for _, stored := range events {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			seam.Close()
			return nil, err
		}
		text := body.Content
		if text == "" {
			text = body.Summary
		}
		seam.Submit(stored.Key, text)
	}
	supply.seam = seam
	supply.searcher = search.NewStoreSearcher(source, seam, embedder)
	return supply, nil
}

// close releases the vector generation pipeline of the supply. A repeat close
// is a no-op, and the keyword-only supply beside no seam closes nothing.
func (s *retrievalSupply) close() {
	if s.seam != nil {
		s.seam.Close()
	}
}

// runReading is the read port of the diagnostic read: the binding generation the
// sole publication entry admitted, the session sites this run established and
// the tasks the task layer of this run holds. Every reading goes back to the
// owner that already holds the fact — the current binding account, the state
// owner's own authorized read and the task carriers — so the run keeps no
// second copy of the live scene and a reload that adds or removes an agent is
// visible in the next reading.
//
// A task the commit carrier lists is read beside the obligations its manager
// still holds and the notifications its record still owes; a task the owner
// cannot answer for refuses the reading by name instead of being dropped from
// it in silence.
type runReading struct{ run *Run }

// The read port answers every dimension of the inventory.
var _ query.Reading = runReading{}

// Published returns the generation currently in effect: the agents the
// declaration configures beside the resource registrations it holds, both read
// from the one account the publication entry admitted.
func (a runReading) Published(context.Context) (query.Generation, error) {
	a.run.mu.Lock()
	defer a.run.mu.Unlock()
	generation := query.Generation{
		Agents: make([]event.AgentID, 0, len(a.run.current.Agents)),
		Leases: append([]state.LeaseRef(nil), a.run.current.Leases...),
	}
	for id := range a.run.current.Agents {
		generation.Agents = append(generation.Agents, id)
	}
	sort.Slice(generation.Agents, func(i, j int) bool { return generation.Agents[i] < generation.Agents[j] })
	return generation, nil
}

// Sites returns one owner snapshot per site this run established, each beside
// the invocation and decision currently holding its execution slot. The site
// snapshots are read back through the state owner's own authorized read, so an
// ungranted subject is refused by the owner rather than by the run.
func (s runReading) Sites(ctx context.Context) ([]query.SiteReading, error) {
	s.run.mu.Lock()
	addrs := make([]event.Address, 0, len(s.run.sites))
	for addr := range s.run.sites {
		addrs = append(addrs, addr)
	}
	s.run.mu.Unlock()
	sort.Slice(addrs, func(i, j int) bool {
		if addrs[i].AgentID != addrs[j].AgentID {
			return addrs[i].AgentID < addrs[j].AgentID
		}
		return addrs[i].SessionID < addrs[j].SessionID
	})
	sites := make([]query.SiteReading, 0, len(addrs))
	for _, addr := range addrs {
		site, err := s.run.store.Site(ctx, s.run.inspector, addr, s.run.instance)
		if err != nil {
			return nil, err
		}
		invocation, decision, held := s.run.occupancy.Slot(addr, s.run.instance)
		sites = append(sites, query.SiteReading{
			Session:    *site,
			Invocation: invocation,
			Decision:   decision,
			Held:       held,
		})
	}
	return sites, nil
}

// Tasks returns every task this run holds, each beside the status it stands in,
// the obligations its manager still keeps open and the notifications its commit
// record still owes.
func (t runReading) Tasks(ctx context.Context) ([]query.TaskReading, error) {
	ids := t.run.commit.TaskIDs()
	readings := make([]query.TaskReading, 0, len(ids))
	for _, id := range ids {
		spec, status, err := t.run.registry.Lookup(ctx, id)
		if err != nil {
			return nil, err
		}
		obligations, err := t.run.manager.Obligations(ctx, id)
		if err != nil {
			return nil, err
		}
		readings = append(readings, query.TaskReading{
			TaskID:      id,
			Spec:        spec,
			Status:      status,
			Obligations: obligations,
			Outbox:      t.run.commit.Outbox(id),
		})
	}
	return readings, nil
}

// execLedger is the execution resource ledger face of the blocker explanation:
// the retained state of one registered lease and the accounting of the ledger,
// read from the manager that holds both. It reports what the ledger records and
// adds no fact of its own.
type execLedger struct{ manager *tagentexec.Manager }

// The ledger face answers the lease retention readings of the explanation.
var _ query.LeaseExplainer = execLedger{}

// ExplainLease returns the retained state of the resource the ledger holds
// under the registration. A key the ledger holds nothing under answers as not
// held, so a registration whose resource was freed stays explainable.
func (l execLedger) ExplainLease(ref state.LeaseRef) (query.LeaseRetention, error) {
	explanation, err := l.manager.Explain(tagentexec.ResourceKey{Kind: ref.Kind, Key: ref.Key})
	if errors.Is(err, tagentexec.ErrResourceUnknown) {
		return query.LeaseRetention{Ref: ref, Held: false}, nil
	}
	if err != nil {
		return query.LeaseRetention{}, err
	}
	retention := query.LeaseRetention{
		Ref:                 ref,
		Held:                explanation.Held,
		Deadline:            explanation.Deadline,
		Renewed:             explanation.Renewed,
		Adopted:             explanation.Adopted,
		ConsecutiveUnknowns: explanation.ConsecutiveUnknowns,
		Escalations:         explanation.Escalations,
		Obligations:         append([]string(nil), explanation.Obligations...),
	}
	if explanation.LastStop != nil && !explanation.LastStop.Confirmed {
		retention.StopReason = explanation.LastStop.Reason
	}
	return retention, nil
}

// LeaseCounts returns the accounting the ledger keeps over its leases.
func (l execLedger) LeaseCounts() query.LeaseLedgerCounts {
	return query.LeaseLedgerCounts{
		Held:      l.manager.Held(),
		Acquired:  l.manager.Acquired(),
		Adopted:   l.manager.Adopted(),
		Reused:    l.manager.Reused(),
		Released:  l.manager.Released(),
		Escalated: l.manager.Escalated(),
	}
}

// ExecLeaseExplainer returns the blocker explanation face over one execution
// resource ledger. The manager may be absent for an assembly beside no
// execution resources, in which case no ledger is attached to the read.
func ExecLeaseExplainer(manager *tagentexec.Manager) query.LeaseExplainer {
	if manager == nil {
		return nil
	}
	return execLedger{manager: manager}
}
