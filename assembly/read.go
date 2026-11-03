package assembly

import (
	"context"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/memory/search"
	"github.com/SpellingDragon/tagent/runtime/state"
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

// ModelInFlight reports the model request one site is executing, read off the
// consumption loop of this run: the range the decision serves and the identity
// of the decision itself. A site in another instance, a run beside no loop and
// a site running no request each answer no flight.
func (r *Run) ModelInFlight(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool) {
	if !addr.IsComplete() || instance == "" || instance != r.instance {
		return "", "", false
	}
	r.mu.Lock()
	loop := r.loop
	r.mu.Unlock()
	if loop == nil {
		return "", "", false
	}
	return loop.InFlightModel(addr)
}

// recall runs one retrieval over the recall face of this run and answers the
// reference set the query hit. The retrieval reads through the observation table
// over the committed store and the searcher fusing its two legs, so a hit is
// answered as the recall reference its event was landed with and never as a
// copy of the text behind it: the original stays behind the scoped read path.
// The query carries its own authorized partition set — the recall grants no
// authorization of its own — and a run assembled beside no storage tier is
// refused by name with ErrRetrievalUnwired instead of answered as an empty set.
func (r *Run) recall(ctx context.Context, q search.Query) (search.Result, error) {
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
		text := body.Message.Content
		if text == "" {
			text = body.Meta[event.MetaKeyEventSummary]
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
// ungranted subject is refused by the owner rather than by the run. The slot a
// snapshot names is the model request the consumption loop of the run actually
// flies, never a copy the read keeps on the side.
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
		invocation, decision, held := s.run.ModelInFlight(addr, s.run.instance)
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
