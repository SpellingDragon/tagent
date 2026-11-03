// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
//
// The producing half of the final round judgment: the watchdog budget of one
// (agent, session) range is judged off the chain the range committed, and a
// spent budget is filed as the final_round.request fact of that range. The fact
// states which range, why and the inputs the judgment read; what the consumer
// runs from it — the closing round — is the reading half, and this planner
// never decides anything else.
package agent

import (
	"context"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// FinalRoundPlannerName is the stable name the final round planner answers.
const FinalRoundPlannerName = "final-round-planner"

// FinalRoundSegment is the material one final round judgment reads off the chain
// of one address: the range its next decision serves, the tool rounds that range
// already ran and the moment the segment's first fact landed.
type FinalRoundSegment struct {
	// Range is the invocation whose budget the judgment reads.
	Range event.InvocationID
	// Rounds is the number of tool rounds the range ran before the judgment.
	Rounds int
	// Started is the moment the segment's earliest fact landed: the clock the
	// time budget is spent against.
	Started time.Time
}

// FinalRoundChain is the chain face the planner reads one commit through: the
// address a committed fact belongs to, the segment that address flies and the
// filing of the judgment the segment's spent budget answered.
//
// The redline list (3 rules):
// MUST answer a fact no chain owns as unowned, and a chain beside no range in flight as no segment.
// MUST report the rounds and the start the chain records, never a count the planner keeps.
// MUST NOT commit a second request for a range that already carries one.
type FinalRoundChain interface {
	// AddressOf reports the (agent, session) address one committed fact belongs
	// to; a fact beside no agent or no session belongs to no chain.
	AddressOf(ctx context.Context, id event.EventID) (event.Address, bool)
	// SegmentOf reports the segment one address flies: the facts committed past
	// the last agent_output of that chain, read as one judgment's material.
	SegmentOf(ctx context.Context, addr event.Address) (FinalRoundSegment, bool)
	// FileFinalRoundRequest files one judgment on the chain of the address whose
	// budget it judged; a range already carrying a request answers as filed.
	FileFinalRoundRequest(ctx context.Context, addr event.Address, request event.FinalRoundRequest) error
}

// FinalRoundPlanner files the closing round of a range as the fact it is: it
// reads the segment one commit grew, judges the budget its own agent declares
// and files the final_round.request of the range whose budget is spent. The
// judgment is read off the chain, so a fresh process judges the same chain the
// same way, and the identity of the fact is content-addressed, so a re-evaluation
// of the same range files nothing twice.
//
// The planner is an instance of the material flow like any other: it reads the
// committed fact and answers a read-only observation, so the pipeline dispatches
// it beside the reflection driver and the age fold. Its own filing is a commit
// of the range and never a second observation, so the judgment it just filed
// never re-enters this pass.
type FinalRoundPlanner struct {
	chain  FinalRoundChain
	policy func(event.AgentID) FinalRoundPolicy
}

// The planner serves the fact→observation port and observes every commit.
var (
	_ plugin.Instance           = (*FinalRoundPlanner)(nil)
	_ plugin.PostCommitObserver = (*FinalRoundPlanner)(nil)
)

// NewFinalRoundPlanner assembles the planner over the chain face of one run and
// the budget table of the declaration it published last. A nil face or a nil
// table files nothing, so an assembly mounted beside neither judges no budget
// instead of failing on the first commit.
func NewFinalRoundPlanner(chain FinalRoundChain, policy func(event.AgentID) FinalRoundPolicy) *FinalRoundPlanner {
	return &FinalRoundPlanner{chain: chain, policy: policy}
}

// Name reports the stable name of this planner instance.
func (p *FinalRoundPlanner) Name() string { return FinalRoundPlannerName }

// Port reports the consumption the planner serves: one committed fact in, the
// judgment of one spent budget out.
func (p *FinalRoundPlanner) Port() plugin.Port { return plugin.PortFactObservation }

// ObserveCommit judges the budget of the segment one committed fact belongs to
// and files the closing round of the range whose budget is spent.
//
// The duty is read-only over the fact and over the chain: a fact beside no
// agent address, a chain beside no range in flight, a segment within both of its
// budgets and an agent that declares no budget each leave the commit as it
// stands. The filing is the one write, and a failure to file it is reported to
// the pipeline as the named gap beside a fact that stays committed.
func (p *FinalRoundPlanner) ObserveCommit(ctx context.Context, id event.EventID) error {
	if p == nil || p.chain == nil || p.policy == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	addr, held := p.chain.AddressOf(ctx, id)
	if !held {
		return nil
	}
	segment, carried := p.chain.SegmentOf(ctx, addr)
	if !carried {
		return nil
	}
	elapsed := time.Since(segment.Started)
	reason := FinalRoundDue(p.policy(addr.AgentID), segment.Rounds, elapsed)
	if reason == FinalRoundNone {
		return nil
	}
	return p.chain.FileFinalRoundRequest(ctx, addr, event.FinalRoundRequestFor(segment.Range, string(reason), segment.Rounds, elapsed))
}

// finalRoundRangeKey, finalRoundReasonKey, finalRoundRoundsKey and
// finalRoundElapsedKey are the metadata keys a final round request fact states its
// judgment under: the reading half quotes the keys the filing side lands, so both
// halves name one judgment.
const (
	finalRoundRangeKey   = "final_round_range"
	finalRoundReasonKey  = "final_round_reason"
	finalRoundRoundsKey  = "final_round_rounds"
	finalRoundElapsedKey = "final_round_elapsed"
)

// finalRoundRequestOf reads the final round judgment the chain of one session
// already filed for one range: the request fact the range is named by, or no
// request while the chain carries none.
//
// The redline list (3 rules):
// MUST read the range off the judgment's own range field, never off a position.
// MUST answer the first request the chain carries for the range and read no later one.
// MUST report a request beside an address or a chain that cannot be read as none.
func finalRoundRequestOf(facts []event.Event, rangeID event.InvocationID) (event.FinalRoundRequest, bool) {
	if rangeID == "" {
		return event.FinalRoundRequest{}, false
	}
	for _, evt := range facts {
		if evt.Type != event.FinalRoundRequestType || finalRoundRangeOf(evt) != rangeID {
			continue
		}
		return finalRoundRequestFrom(evt), true
	}
	return event.FinalRoundRequest{}, false
}

// finalRoundRangeOf reports the range one request fact judged: the field the
// filing side states it under, falling back to the range the fact belongs to.
func finalRoundRangeOf(evt event.Event) event.InvocationID {
	if stated := evt.Meta[finalRoundRangeKey]; stated != "" {
		return event.InvocationID(stated)
	}
	return evt.InvocationID
}

// finalRoundRequestFrom reads one request fact back as the judgment it states.
// A field the fact left unreadable answers as its zero value: the judgment is
// still the one the chain carries, and a reason it names is what the closing
// round runs from.
func finalRoundRequestFrom(evt event.Event) event.FinalRoundRequest {
	request := event.FinalRoundRequest{
		ID:     event.EventID(evt.Key),
		Range:  finalRoundRangeOf(evt),
		Reason: evt.Meta[finalRoundReasonKey],
	}
	if rounds, err := strconv.Atoi(evt.Meta[finalRoundRoundsKey]); err == nil {
		request.Rounds = rounds
	}
	if elapsed, err := time.ParseDuration(evt.Meta[finalRoundElapsedKey]); err == nil {
		request.Elapsed = elapsed
	}
	return request
}
