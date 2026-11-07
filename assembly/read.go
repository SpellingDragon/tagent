package assembly

import (
	"context"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/memory/replay"
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
// reference set the query hit. The retrieval reads the durable chain on demand
// and the searcher fuses its two legs, so a hit is answered as the original
// reference the chain states and never as a copy of the text behind it: the
// original stays behind the scoped read path.
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
// stateless durable query source, the vector seam the embedding leg borrows its
// vectors from, and the searcher reading both legs through that source. It is
// wired once at the composition root and holds no second copy of any event.
type retrievalSupply struct {
	source   *tierQuerySource
	seam     *memory.VectorSeam
	searcher *search.StoreSearcher
}

// tierQuerySource is the stateless durable read face: every query decodes the
// chain on demand, while no event or parent map is retained between calls.
type tierQuerySource struct {
	segments *ledger.FileSegmentStore
}

// NewTierQuerySource attaches the durable read face without landing anything.
// The returned face answers QueryEvents, Event, Reference and GetParent on
// demand and retains no event or parent map between calls.
func NewTierQuerySource(segments *ledger.FileSegmentStore) *tierQuerySource {
	return &tierQuerySource{segments: segments}
}

// recordOfTierEvent decodes one stored event into the existing query kernel.
func recordOfTierEvent(stored ledger.StoredEvent) (memory.EventRecord, ledger.OriginalRef, bool, error) {
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return memory.EventRecord{}, ledger.OriginalRef{}, false, err
	}
	if body.Key != stored.Key {
		return memory.EventRecord{}, ledger.OriginalRef{}, false, memory.ErrBodyKeyMismatch
	}
	if event.CompressionMechanism(body.Type) {
		return memory.EventRecord{}, ledger.OriginalRef{}, false, nil
	}
	return body.Record(stored.PartitionID, stored.WindowTS), body.Ref(), true, nil
}

// QueryEvents answers through the existing query kernel over a temporary read.
func (s *tierQuerySource) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	partitions := queryPartitionsOf(opts)
	if len(partitions) == 0 {
		return []memory.EventReference{}, nil
	}
	query := memory.NewMemoryEventQuery()
	if err := s.segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, _ int64, _ string, _ event.SessionID) error {
		if !partitions[partition] {
			return nil
		}
		stored, held, err := s.segments.GetEvent(ctx, key)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, key)
		}
		record, _, admitted, err := recordOfTierEvent(stored)
		if err != nil {
			return err
		}
		if !admitted {
			return nil
		}
		return query.Store(ctx, record)
	}); err != nil {
		return nil, err
	}
	return query.QueryEvents(ctx, opts)
}

// queryPartitionsOf resolves the authorized partition set before any body read.
func queryPartitionsOf(opts memory.QueryOptions) map[int]bool {
	partitions := map[int]bool{}
	if len(opts.PartitionIDs) > 0 {
		for _, partition := range opts.PartitionIDs {
			partitions[partition] = true
		}
		return partitions
	}
	if opts.PartitionID != 0 {
		partitions[opts.PartitionID] = true
	}
	return partitions
}

// Event returns one event reference directly from the chain.
func (s *tierQuerySource) Event(key event.EventID) (memory.EventReference, bool) {
	stored, held, err := s.segments.GetEvent(context.Background(), key)
	if err != nil || !held {
		return memory.EventReference{}, false
	}
	record, _, held, err := recordOfTierEvent(stored)
	if err != nil || !held {
		return memory.EventReference{}, false
	}
	return record.Reference(), true
}

// Reference returns one original reference directly from the chain.
func (s *tierQuerySource) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	stored, held, err := s.segments.GetEvent(context.Background(), key)
	if err != nil || !held {
		return ledger.OriginalRef{}, false
	}
	_, ref, held, err := recordOfTierEvent(stored)
	if err != nil || !held {
		return ledger.OriginalRef{}, false
	}
	return ref, true
}

// GetParent answers the preceding non-synthetic fact on the same chain. The
// index normally proves the edge without bodies; an index entry with no session
// falls back to the body walk so two sessions can never be joined accidentally.
func (s *tierQuerySource) GetParent(key event.EventID) (event.EventID, error) {
	if key == "" {
		return "", nil
	}
	found := false
	fallback := false
	partition := 0
	session := event.SessionID("")
	parent := event.EventID("")
	if err := s.segments.EachIndexNewestFirst(context.Background(), func(candidate event.EventID, part int, _ int64, _ string, sess event.SessionID) error {
		if !found {
			if candidate == key {
				found = true
				partition = part
				session = sess
				if session == "" {
					fallback = true
				}
			}
			return nil
		}
		if parent != "" || part != partition {
			return nil
		}
		if sess == "" {
			fallback = true
			return nil
		}
		if sess != session || replay.SyntheticKey(candidate) {
			return nil
		}
		parent = candidate
		return nil
	}); err != nil {
		return "", err
	}
	if fallback {
		return s.parentFromBodies(key, partition)
	}
	return parent, nil
}

// parentFromBodies resolves chains whose index carries no session. Only the
// key's partition is decoded; an empty session remains its own chain.
func (s *tierQuerySource) parentFromBodies(key event.EventID, partition int) (event.EventID, error) {
	var refs []anchorRef
	if err := s.segments.EachIndexNewestFirst(context.Background(), func(candidate event.EventID, part int, seq int64, typ string, session event.SessionID) error {
		if part == partition {
			refs = append(refs, anchorRef{key: candidate, partition: part, seq: seq, typ: typ, session: session})
		}
		return nil
	}); err != nil {
		return "", err
	}
	previous := map[replay.ChainKey]event.EventID{}
	parent := event.EventID("")
	for i := len(refs) - 1; i >= 0; i-- {
		stored, held, err := s.segments.GetEvent(context.Background(), refs[i].key)
		if err != nil {
			return "", err
		}
		if !held {
			return "", fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, refs[i].key)
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return "", err
		}
		evt := body.Event()
		evt.Key = stored.Key
		if replay.SyntheticKey(evt.Key) {
			continue
		}
		chain := replay.ChainKey{PartitionID: stored.PartitionID, SessionID: sessionOf(evt)}
		if evt.Key == key {
			parent = previous[chain]
		}
		previous[chain] = evt.Key
	}
	return parent, nil
}

// supplyRetrieval wires the retrieval face over one opened storage tier: the
// durable query source reads only when asked, each business event submits its
// vector generation without waiting for it, and the searcher fuses the two legs
// of every query back into the one ordered reference set.
// A nil embedder names no vector generation at all: no seam is built, no
// embedding is ever called, and the searcher answers through the keyword leg
// alone — the zero-change behaviour the embedding contract states. The
// embedding text of one event is its stored content, falling back to its
// stored summary when the content is empty.
func supplyRetrieval(ctx context.Context, tier *ledger.Tier, embedder memory.Embedder) (*retrievalSupply, error) {
	source := NewTierQuerySource(tier.Segments)
	supply := &retrievalSupply{source: source}
	if embedder == nil {
		supply.searcher = search.NewStoreSearcher(source, nil, nil)
		return supply, nil
	}
	seam := memory.NewVectorSeam(0)
	seam.Bind(embedder)
	if err := tier.Segments.EachEvent(ctx, func(stored ledger.StoredEvent) error {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return err
		}
		if event.CompressionMechanism(body.Type) {
			return nil
		}
		text := body.Message.Content
		if text == "" {
			text = body.Meta[event.MetaKeyEventSummary]
		}
		seam.Submit(stored.Key, text)
		return nil
	}); err != nil {
		seam.Close()
		return nil, err
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
