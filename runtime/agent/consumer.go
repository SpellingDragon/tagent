// 契约: docs/wiki/agent/event-flow.md#runner-flow
//
// The resident consumption loop of one (agent, session) address: pull the
// events that arrived → dispatch them by what they are → call the model once
// when they drive → return to the pull. The loop never waits for a dispatched
// tool to settle: each settled result arrives as the event it is and drives the
// next decision of the segment it belongs to.
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// admittedCall is one tool call a decision admitted as a background task: the
// call identity the next decision pairs the settled result under, the tool name
// it was admitted as, and the task identity its settlement carries.
type admittedCall struct {
	callID string
	tool   string
	taskID event.TaskID
}

// segmentResult is the answer one closed segment leaves for the entries that
// wait on the inputs it consumed: the keys of those inputs, the aggregated
// turn, and the named cause of a segment that closed beside a failure.
type segmentResult struct {
	consumed []event.EventID
	turn     Turn
	err      error
	// trigger is the trigger-source stamp of the material that opened the
	// segment, so the fresh bodies leave under the lineage the entry named.
	trigger string
}

// segment is one in-flight consumption segment of an address: the range
// identity the segment mints, the transcript it accumulated past the session
// history it opened on, the decisions it ran, and the calls it admitted whose
// results have not paired yet. A segment opens on driving events, continues on
// each settled result and each input that arrives while it flies, and closes
// when the model answers without a tool call — the agent_output fact that
// landing closes is the boundary the projection folds by.
//
// The segment is owned by the one consumption goroutine of its address: the
// fields below the mutex of the consumer guard only what a waiting entry reads
// (the closed results and the notify channel), never the segment itself.
type segment struct {
	inv        Invocation
	policy     FinalRoundPolicy
	history    []model.Message
	transcript []model.Message
	decisions  []Decision
	consumed   []event.EventID
	pending    map[event.TaskID]admittedCall
	pipeline   *plugin.Pipeline
	started    time.Time
	rounds     int
	dirty      bool
	// trigger is the trigger-source stamp of the material the segment opened
	// on: the lineage its closing answer leaves under.
	trigger string
}

// Consumer is the resident consumption loop of one address. It is the only
// driver of the model decisions of its address: every entry commits its facts
// and wakes the loop, the loop pulls what arrived, and the answer a waiting
// entry reads is the closed segment that consumed its input.
//
// The redline list (3 rules):
// MUST return to the pull after each model call, never exit beside a dispatched call.
// MUST NOT wait for every dispatched call: the first arrived result drives the next decision.
// MUST answer a waiting entry only from the segment that consumed its input, never from a neighbour's.
type Consumer struct {
	addr    event.Address
	loop    *Loop
	deliver SegmentDelivery
	// turnContext decorates the context of each decision with the lineage the
	// segment opened on; nil leaves the decision context as it is.
	turnContext func(ctx context.Context, trigger string) context.Context
	wake        chan struct{}
	done        chan struct{}
	cancel      context.CancelFunc

	mu       sync.Mutex
	cursor   event.EventID
	read     map[event.EventID]bool
	flying   *segment
	results  []segmentResult
	segCount int
	notify   chan struct{}
}

// segmentWindow bounds how many closed results stay readable for the entries
// still waiting on them: an input is consumed by the segment that pulled it,
// and a late reader finds that segment unless segmentWindow segments closed
// beside the same input first.
const segmentWindow = 16

// newConsumer assembles the consumption loop of one address over the loop that
// drives its decisions.
func newConsumer(addr event.Address, loop *Loop, deliver SegmentDelivery) *Consumer {
	return &Consumer{
		addr:    addr,
		loop:    loop,
		deliver: deliver,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		read:    map[event.EventID]bool{},
		notify:  make(chan struct{}),
	}
}

// run is the resident loop: wait for a wake, consume everything that arrived,
// wait again. A cancelled context ends the loop; the segments it left flying
// are recovered by the restart from the chain, never by a second driver beside
// this one.
func (c *Consumer) run(ctx context.Context) {
	defer close(c.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		}
		c.consume(ctx)
	}
}

// Wake signals the loop that events may have arrived: the signal merges, so a
// wake that lands while the loop consumes keeps it turning once more.
func (c *Consumer) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// consume drives the address until no arrived event drives it further: the
// flying segment continues on each harvested result and each fresh input, a
// closed segment hands the address to the next pull, and a pull that reports
// no driving event returns the loop to its wait.
func (c *Consumer) consume(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if c.flyingSegment() == nil {
			pull := c.pullDriving()
			if !pull.drove() {
				return
			}
			c.openSegment(ctx, pull.driving, pull.material)
		}
		if !c.driveSegment(ctx, c.flyingSegment()) {
			return
		}
	}
}

// flyingSegment reports the segment this address flies, or none.
func (c *Consumer) flyingSegment() *segment {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flying
}

// pulledEvents splits one pull of the session chain into the facts that drive
// the loop and the material those facts carry: an input an entry committed and
// a notification an unattended follow-up lands are driving facts wherever they
// stand, while the material they carry belongs to the segment only past the
// last agent_output the chain holds — everything that closed segment already
// presents as the prefix of a request, so a driving fact behind that boundary
// drives without carrying: presenting it twice would read one fact as two.
// The cursor is the identity of the last fact the chain held at the read, so a
// fold rewrite that hides it re-reads the chain head — the read set keeps each
// fact once by its own identity.
type pulledEvents struct {
	// driving lists every driving fact the pull took, carrying or not.
	driving []event.Event
	// material lists the driving facts whose material the segment carries.
	material []event.Event
	// chain is the session chain the pull read, as the chain face answered it:
	// the very facts a judgment of this boundary is read from, so the drive reads
	// the chain once per pull instead of a second time for its round.
	chain []event.Event
}

func (p pulledEvents) drove() bool { return len(p.driving) > 0 }

func (c *Consumer) pullDriving() pulledEvents {
	facts := c.loop.sessionFacts(c.addr)
	if len(facts) == 0 {
		return pulledEvents{}
	}
	lastOutput := -1
	for at, evt := range facts {
		if evt.Type == event.TypeAgentOutput {
			lastOutput = at
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	from := 0
	if c.cursor != "" {
		for at, evt := range facts {
			if evt.Key == c.cursor {
				from = at + 1
				break
			}
		}
	}
	pull := pulledEvents{chain: facts}
	for at := from; at < len(facts); at++ {
		evt := facts[at]
		if evt.Key == "" || !startsRange(evt) {
			continue
		}
		if c.read[evt.Key] {
			continue
		}
		c.read[evt.Key] = true
		pull.driving = append(pull.driving, evt)
		if at > lastOutput {
			pull.material = append(pull.material, evt)
		}
	}
	c.cursor = facts[len(facts)-1].Key
	return pull
}

// openSegment mints the range identity of one segment over the driving facts
// it opens on and the material those facts carry,
// it folds past, and observes each driving fact once. The address now flies
// this segment until it closes.
func (c *Consumer) openSegment(ctx context.Context, driving, material []event.Event) *segment {
	c.mu.Lock()
	c.segCount++
	count := c.segCount
	c.mu.Unlock()
	id := event.InvocationID(fmt.Sprintf("%s#c%d", c.addr.SessionID, count))
	inv := Invocation{
		InvocationID: id,
		Address:      c.addr,
		Instance:     c.loop.instance,
		Ready:        material,
	}
	c.loop.intakeFor(id).setAddress(c.addr)
	seg := &segment{
		inv:     inv,
		policy:  c.loop.finalRoundFor(c.addr.AgentID),
		history: c.loop.sessionHistory(c.addr),
		pending: map[event.TaskID]admittedCall{},
		started: time.Now(),
		dirty:   true,
		trigger: drivingTrigger(driving),
	}
	if ready, err := aggregateReady(material); err != nil {
		consumerWarn("tagent: the opening events of %s did not aggregate: %v", id, err)
	} else {
		seg.transcript = append(seg.transcript, ready...)
	}
	for _, evt := range driving {
		if evt.Key != "" {
			seg.consumed = append(seg.consumed, evt.Key)
		}
		c.loop.observeFact(ctx, inv, evt)
	}
	c.mu.Lock()
	c.flying = seg
	c.mu.Unlock()
	consumerDebug("tagent: consumer segment open agent=%s session=%s invocation=%s ready=%d",
		c.addr.AgentID, c.addr.SessionID, id, len(driving))
	return seg
}

// drivingTrigger reports the trigger-source stamp of one batch of driving
// facts: the stamp of the first fact that carries one, so the answer a segment
// closes on leaves under the lineage of the entry that opened it. A batch beside
// no stamp at all answers the empty lineage, which the host reads as its own
// default.
func drivingTrigger(events []event.Event) string {
	for _, evt := range events {
		if stamp := evt.Meta[event.MetaKeyTriggerSource]; stamp != "" {
			return stamp
		}
	}
	return ""
}

// driveSegment advances the flying segment by one pull: the results that
// arrived harvest into observations, the inputs that landed append as material
// the next decision reads, and a segment that stands beside no new material
// returns to the wait — its calls still run, and each settlement wakes the
// loop again. A decision that answers a tool call leaves its receipts as the
// material of the next round; a decision that answers none closes the segment.
//
// - The decision runs under the lineage the material that opened the segment named, so a body a call of it sends names the line its turn names instead of the send tool's own default.
// - The receipts alone drive the next round only when no call is running: a decision whose every call was refused owes the model the refusal it reads, so it decides again at once, while a decision with calls in flight parks — each settlement and each input that lands wakes the loop, and the next decision reads the result beside whatever else arrived.
// - The closing round admits none of the calls it issued: the receipts it leaves are the refusals the model reads, and no further round may read them, so the segment closes on the closing round exactly as the direct drive ends on the one it ran.
func (c *Consumer) driveSegment(ctx context.Context, seg *segment) bool {
	harvested := seg.harvest(ctx, c.loop)
	if len(harvested) > 0 {
		seg.transcript = append(seg.transcript, harvested...)
		seg.dirty = true
	}
	pull := c.pullDriving()
	if pull.drove() {
		seg.dirty = true
	}
	for _, evt := range pull.driving {
		c.loop.observeFact(ctx, seg.inv, evt)
	}
	if len(pull.material) > 0 {
		if msgs, err := aggregateReady(pull.material); err != nil {
			consumerWarn("tagent: the fresh input of %s did not aggregate: %v", seg.inv.InvocationID, err)
		} else {
			seg.transcript = append(seg.transcript, msgs...)
		}
		for _, evt := range pull.material {
			if evt.Key != "" {
				seg.consumed = append(seg.consumed, evt.Key)
			}
		}
	}
	if !seg.dirty {
		return false
	}
	round := c.closingRoundOf(seg, pull.chain)
	transcript := append(append([]model.Message{}, seg.history...), seg.transcript...)
	if c.turnContext != nil {
		ctx = c.turnContext(ctx, seg.trigger)
	}
	decision, outcome, receipts, running, err := c.loop.decide(ctx, seg.inv, transcript, round)
	if err != nil {
		c.closeSegment(ctx, seg, decision, outcome, round, err)
		return true
	}
	seg.dirty = false
	seg.decisions = append(seg.decisions, decision)
	if flow, flowErr := plugin.NewFlow(decision.Binding.Plugins); flowErr == nil {
		seg.pipeline = &flow
	}
	for _, adm := range running {
		seg.pending[adm.taskID] = adm
	}
	if outcome.Kind == model.KindCompleted && outcome.Body != nil && outcome.Body.IsToolCallResponse() {
		seg.transcript = append(seg.transcript, assistantOf(outcome))
		seg.transcript = append(seg.transcript, receipts...)
		seg.rounds++
		if round.final {
			c.closeSegment(ctx, seg, decision, outcome, round, nil)
			return true
		}
		if len(running) == 0 {
			seg.dirty = true
		}
		return true
	}
	c.closeSegment(ctx, seg, decision, outcome, round, nil)
	return true
}

// closingRoundOf reports the round the next decision of one segment runs: the
// closing round the final_round.request fact of the segment's range names once
// it stands on the chain, and the closing round the segment's own spent budget
// owes while no request stands there yet. The fact is the judgment, so a reader
// that finds one never re-judges the budget the producer already spent; the
// range identity keeps a request of a closed range from closing the next one,
// and a segment closes on its closing round, so one range reads one judgment.
// The instruction and the timeout the round runs under stay the policy's: the
// fact states why the budget is spent, never how the closing round speaks.
// The chain is the one the pull of this boundary read, so the judgment and the
// material that drove the round are read off one snapshot of the same chain.
func (c *Consumer) closingRoundOf(seg *segment, chain []event.Event) decisionRound {
	if request, held := finalRoundRequestOf(chain, seg.inv.InvocationID); held {
		return decisionRound{
			final:   true,
			reason:  FinalRoundReason(request.Reason),
			prompt:  seg.policy.closingPrompt(),
			timeout: seg.policy.closingTimeout(),
		}
	}
	return closingRound(seg.policy, seg.rounds, time.Since(seg.started))
}

// closeSegment lands the closing answer of one segment: the transcript it
// accumulated joins the session history, the agent_output fact commits under
// the range identity the segment minted, the fresh bodies of the answer leave
// through the delivery face of the address, and the closed result — the keys of
// the inputs it consumed beside the aggregated turn — wakes the entries that
// wait on them. The delivery stands before the waiters are answered for the
// reason the drive it replaces delivered before it returned: an answer reaches
// the outbound face once per segment, whether an entry waited on that segment
// or a notification drove it into being. A segment that closed beside a named
// failure reports that cause to its waiters and commits no output fact.
func (c *Consumer) closeSegment(ctx context.Context, seg *segment, decision Decision, outcome model.Outcome, round decisionRound, cause error) {
	turn := Turn{
		InvocationID: seg.inv.InvocationID,
		Decisions:    seg.decisions,
		Terminal:     outcome.Kind,
	}
	if round.final {
		turn.FinalRound = round.reason
	}
	if outcome.Kind == model.KindFailed {
		if err := outcome.Cause(); err != nil {
			turn.Failure = strings.TrimSpace(err.Error())
		}
	}
	c.loop.commitTranscript(c.addr, seg.transcript)
	if cause == nil {
		if err := c.loop.commitOutput(ctx, seg.inv, decision, outcome); err != nil {
			cause = fmt.Errorf("tagent: the output fact of the segment did not commit: %w", err)
		}
	}
	if cause != nil {
		turn.Terminal = model.KindFailed
		turn.Failure = strings.TrimSpace(cause.Error())
	}
	answer := SegmentAnswer{Turn: turn, Err: cause, Trigger: seg.trigger, Consumed: append([]event.EventID(nil), seg.consumed...)}
	if c.deliver != nil {
		c.deliver(ctx, c.addr, answer)
	}
	c.mu.Lock()
	c.flying = nil
	c.results = append(c.results, segmentResult{consumed: seg.consumed, turn: turn, err: cause, trigger: seg.trigger})
	if len(c.results) > segmentWindow {
		c.results = c.results[len(c.results)-segmentWindow:]
	}
	notify := c.notify
	c.notify = make(chan struct{})
	c.mu.Unlock()
	close(notify)
	consumerDebug("tagent: consumer segment close invocation=%s decisions=%d terminal=%s consumed=%d",
		seg.inv.InvocationID, len(turn.Decisions), turn.Terminal, len(seg.consumed))
}

// harvest pairs the results that arrived for the calls this segment admitted:
// each settled delivery the intake parked becomes the observation of its call,
// observed through the pipeline of the decision that admitted it. The harvest
// never waits: a call whose result has not arrived stays pending, and the loop
// returns to the pull beside it.
func (s *segment) harvest(ctx context.Context, l *Loop) []model.Message {
	if len(s.pending) == 0 {
		return nil
	}
	in := l.intakeFor(s.inv.InvocationID)
	type pairing struct {
		call admittedCall
		item delivered
	}
	var paired []pairing
	in.mu.Lock()
	for taskID, call := range s.pending {
		items := in.settled[taskID]
		if len(items) == 0 {
			continue
		}
		item := items[0]
		if len(items) > 1 {
			in.settled[taskID] = items[1:]
		} else {
			delete(in.settled, taskID)
		}
		delete(s.pending, taskID)
		paired = append(paired, pairing{call: call, item: item})
	}
	in.mu.Unlock()
	var out []model.Message
	for _, pair := range paired {
		out = append(out, observationOf(pair.item.msg, pair.call.callID, pair.call.tool))
		if pair.item.fact != "" && s.pipeline != nil {
			l.publish(s.pipeline.After(ctx, pair.item.fact))
		}
	}
	return out
}

// SegmentDelivery is the face the host hands one closed segment's fresh answer
// to: the address whose chain the segment committed its output to, and the
// answer the entries waiting on that segment read. A consumer beside no face
// delivers nothing and leaves every answer on the chain, which is the shape a
// loop driven by tests keeps.
type SegmentDelivery func(ctx context.Context, addr event.Address, answer SegmentAnswer)

// SegmentAnswer is the closed-segment answer one waiting entry reads: the
// aggregated turn, the named cause of a failed segment, the trigger-source
// stamp the segment opened on, which names the lineage its answer leaves
// under, and the keys of the inputs the segment consumed, so a face that
// delivers the answer knows which entries that segment answered.
type SegmentAnswer struct {
	Turn     Turn
	Err      error
	Trigger  string
	Consumed []event.EventID
}

// WaitAnswer waits for the segment that consumed any of the named input keys
// and answers its aggregated turn. An input the flying segment already took
// answers when that segment closes; an input no segment took yet answers when
// the next pull drives one.
func (c *Consumer) WaitAnswer(ctx context.Context, keys []event.EventID) (SegmentAnswer, error) {
	for {
		c.mu.Lock()
		for _, res := range c.results {
			for _, key := range keys {
				for _, consumed := range res.consumed {
					if consumed == key {
						answer := SegmentAnswer{Turn: res.turn, Err: res.err, Trigger: res.trigger, Consumed: append([]event.EventID(nil), res.consumed...)}
						c.mu.Unlock()
						return answer, res.err
					}
				}
			}
		}
		notify := c.notify
		c.mu.Unlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return SegmentAnswer{}, ctx.Err()
		}
	}
}

// Done reports the channel that closes when the consumption loop ended.
func (c *Consumer) Done() <-chan struct{} { return c.done }

// Consumers owns the resident consumption loops of a run: one per address an
// entry wakes, each the only driver of the model decisions of its address. The
// loops live as long as the context they were started under; a loop that ended
// is never restarted in place, and the chain carries the position a restart
// resumes from. Every closed segment hands its fresh answer to the one delivery
// face of the set, so the answer of a segment no entry waits on leaves through
// the same path the answer of a waited segment does.
type Consumers struct {
	loop    *Loop
	base    context.Context
	deliver SegmentDelivery
	// turnContext decorates the decision context of every segment these loops
	// fly; nil leaves it as it is.
	turnContext func(ctx context.Context, trigger string) context.Context
	mu          sync.Mutex
	live        map[event.Address]*Consumer
}

// SetTurnContext attaches the decorator every decision of every segment these
// loops fly runs under: the host stamps the lineage of the material that opened
// the segment, so a body a call of that decision sends leaves under the line of
// its own turn. The setter is separate from the constructor so the declared
// construction shape of the set stays as it is.
func (cs *Consumers) SetTurnContext(decorate func(ctx context.Context, trigger string) context.Context) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.turnContext = decorate
	for _, consumer := range cs.live {
		consumer.turnContext = decorate
	}
}

// NewConsumers assembles the consumption loops of one run over the loop that
// drives its decisions, living as long as the named context does. The delivery
// face receives the fresh answer of every closed segment and may be nil, in
// which case the answers stay on the chain.
func NewConsumers(loop *Loop, base context.Context, deliver SegmentDelivery) *Consumers {
	return &Consumers{loop: loop, base: base, deliver: deliver, live: map[event.Address]*Consumer{}}
}

// Ensure answers the consumption loop of one address, assembling and starting
// it on first use: the loop resumes behind the last agent_output the chain
// carries — the inputs past it are the material a restart still owes — and a
// chain beside no agent_output at all resumes from its head, so an unanswered
// input stays owed instead of vanishing behind a restart.
func (cs *Consumers) Ensure(addr event.Address) *Consumer {
	if cs == nil || !addr.IsComplete() {
		return nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if consumer, ok := cs.live[addr]; ok {
		return consumer
	}
	consumer := newConsumer(addr, cs.loop, cs.deliver)
	consumer.turnContext = cs.turnContext
	consumer.cursor = lastOutputOf(cs.loop.sessionFacts(addr))
	ctx, cancel := context.WithCancel(cs.base)
	consumer.cancel = cancel
	cs.live[addr] = consumer
	go consumer.run(ctx)
	return consumer
}

// lastOutputOf reports the identity of the last agent_output fact one chain
// carries, or none beside a chain that closed no segment yet.
func lastOutputOf(facts []event.Event) event.EventID {
	for at := len(facts) - 1; at >= 0; at-- {
		if facts[at].Type == event.TypeAgentOutput {
			return facts[at].Key
		}
	}
	return ""
}

// Wake signals the consumption loop of one address; an address beside no loop
// wakes nothing, and the entry that owns its facts ensures the loop itself.
func (cs *Consumers) Wake(addr event.Address) {
	if cs == nil || !addr.IsComplete() {
		return
	}
	cs.mu.Lock()
	consumer := cs.live[addr]
	cs.mu.Unlock()
	if consumer != nil {
		consumer.Wake()
	}
}

// Has reports whether one address carries a resident consumption loop.
func (cs *Consumers) Has(addr event.Address) bool {
	if cs == nil {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	_, ok := cs.live[addr]
	return ok
}

// Stop ends every consumption loop this set owns; the loops answer their done
// channels and the segments they left flying stay on the chain for a restart.
func (cs *Consumers) Stop() {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, consumer := range cs.live {
		if consumer.cancel != nil {
			consumer.cancel()
		}
	}
}
