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
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
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
	// bound is the binding this address decides on when its material names one
	// explicitly — the binding of a delegated execution site, which is the
	// assembled sub-agent's own rather than one a published generation holds for
	// a declared agent. A nil bound leaves every decision on the generation the
	// borrowing face serves at its start.
	bound  *state.AgentBinding
	wake   chan struct{}
	done   chan struct{}
	cancel context.CancelFunc
	// progress reports that this loop moved toward its fixed point: a segment
	// closed, a result paired or a pull that drove nothing returned to the wait.
	// A close waiting on the criterion of the run reads this instead of polling
	// for the movement, so the wait is an event and the budget its only clock.
	progress func()

	mu sync.Mutex
	// awaited names the tasks the flying segment admitted and has not paired
	// yet; a settled result of one of them belongs to the segment.
	awaited sync.Map
	cursor  event.EventID
	read    map[event.EventID]bool
	// taken names the driving facts a segment of this process pulled, so a name
	// that arrives for one of them knows its material was already driven here.
	taken map[event.EventID]bool
	// named names the driving facts the next pull must take whatever the closed
	// segments of the chain present them as: the notification an entry still
	// owes is named by the face that fires the attempt, never guessed from a
	// position.
	named map[event.EventID]bool
	// seeded reports whether the first pull of this loop has already marked
	// the driving facts the boot found inside closed segments as read; a loop
	// that boots over no chain holds nothing to seed.
	seeded   bool
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
		taken:   map[event.EventID]bool{},
		named:   map[event.EventID]bool{},
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
		c.noteProgress()
	}
}

// SetProgress attaches the pulse this loop reports each movement through, or
// detaches it beside nil. The pulse is a report, never a judgment: a close reads
// it to re-read its criterion at the moment the account moved and keeps its own
// reading as the answer.
func (c *Consumer) SetProgress(pulse func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progress = pulse
}

// noteProgress reports one movement of this loop, or nothing while no pulse is
// attached.
func (c *Consumer) noteProgress() {
	c.mu.Lock()
	pulse := c.progress
	c.mu.Unlock()
	if pulse != nil {
		pulse()
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

// Drive names the facts that must drive the next pull of this address, whatever
// the closed segments of the chain present them as: the notification an entry
// still owes stands where the attempt that died left it, behind the answer that
// attempt closed on, and no position of the chain can tell it from material that
// closed segment already carried. The face that fires the attempt answers the
// judgment on itself and names the fact here.
//
// - A fact a segment of this process already pulled is never named: the material is driven here already, and a second segment over it would answer one notification twice.
// - The pull consumes the name it takes, so one name drives one segment and a segment that closed beside a failure leaves the next attempt free to name the fact again.
// - A name says the fact drives and never what it carries: the material of a fact behind the last closed answer stays the pull's judgment, so a fact the closed prefix already presents is never read as two.
func (c *Consumer) Drive(keys ...event.EventID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		if key == "" || c.taken[key] {
			continue
		}
		c.named[key] = true
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

// Resting reports whether this loop stands between segments: no segment flies and
// no wake waits to open one. The loop is the writer of its own address, so a loop
// that rests writes nothing more at that site, and a close that reads it resting
// reads a writer stopped rather than one merely paused.
func (c *Consumer) Resting() bool {
	if c == nil {
		return true
	}
	if c.flyingSegment() != nil {
		return false
	}
	return len(c.wake) == 0
}

// TaskState names what became of one settled result on this address: the
// flying segment still awaits it, a segment already paired it, or a closed
// segment left it unpaired. The notice-driven follow-up reads the state to know
// whether any consumer already took the result.
type TaskState string

const (
	// TaskAwaited: the flying segment admitted the task and has not paired it.
	TaskAwaited TaskState = "awaited"
	// TaskConsumed: a segment paired the result already.
	TaskConsumed TaskState = "consumed"
	// TaskOrphaned: the segment that admitted the task closed without pairing it.
	TaskOrphaned TaskState = "orphaned"
)

// AwaitTaskState waits until the named task stops being awaited by a flying
// segment and answers the settled state: the segment either pairs the result
// (consumed) or closes beside it (orphaned). A wait the context ends answers
// the state as it stands, so the caller decides over a named moment instead of
// guessing across the window between admission and the segment's close.
func (c *Consumer) AwaitTaskState(ctx context.Context, taskID event.TaskID) TaskState {
	for {
		switch state := c.TaskState(taskID); state {
		case TaskAwaited:
		default:
			return state
		}
		select {
		case <-ctx.Done():
			return c.TaskState(taskID)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TaskState reports what became of the named task on this address. A task this
// address never admitted answers empty, which the notice-driven follow-up reads
// as unattended and drives.
func (c *Consumer) TaskState(taskID event.TaskID) TaskState {
	value, held := c.awaited.Load(taskID)
	if !held {
		return ""
	}
	state, _ := value.(TaskState)
	return state
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
// fact once by its own identity. A fact the face that fires an owed attempt
// named drives wherever it stands, and the read of the whole chain starts from
// the head while a name waits, because the fact it names stands behind the
// cursor the previous pull left.
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
	if !c.seeded {
		c.seeded = true
		c.seedAnsweredLocked(facts, lastOutput)
	}
	from := 0
	if c.cursor != "" && len(c.named) == 0 {
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
		named := c.named[evt.Key]
		if c.read[evt.Key] && !named {
			continue
		}
		delete(c.named, evt.Key)
		c.read[evt.Key] = true
		c.taken[evt.Key] = true
		pull.driving = append(pull.driving, evt)
		if at > lastOutput {
			pull.material = append(pull.material, evt)
		}
	}
	c.cursor = facts[len(facts)-1].Key
	return pull
}

// seedAnsweredLocked marks the driving facts one boot finds inside a closed
// segment as read. The chain a fresh process opens on presents every closed
// segment as the history prefix of its next request, so the input behind that
// boundary was carried by the segment that closed there: reading it as fresh
// input again would drive a second answer over a question the history already
// holds, once per boot. Only the driving facts past the last agent_output —
// the material of a segment the process died beside — drive the first pull.
//
// - A chain beside no agent_output is one segment in flight from its head, so nothing is marked and every driving fact it holds drives.
// - The boundary is read from the chain the pull holds, never from the process clock: a fact the restart cannot yet see stays unanswered rather than being seeded away.
func (c *Consumer) seedAnsweredLocked(facts []event.Event, lastOutput int) {
	if lastOutput < 0 {
		return
	}
	for at := 0; at < lastOutput; at++ {
		evt := facts[at]
		if evt.Key == "" || !startsRange(evt) {
			continue
		}
		c.read[evt.Key] = true
	}
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
	harvested := seg.harvest(ctx, c.loop, &c.awaited)
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
	decision, outcome, receipts, running, err := c.loop.decideOn(ctx, seg.inv, transcript, round, c.binding())
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
		c.awaited.Store(adm.taskID, TaskAwaited)
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
	for taskID := range seg.pending {
		c.awaited.Store(taskID, TaskOrphaned)
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
	c.noteProgress()
	consumerDebug("tagent: consumer segment close invocation=%s decisions=%d terminal=%s consumed=%d",
		seg.inv.InvocationID, len(turn.Decisions), turn.Terminal, len(seg.consumed))
}

// harvest pairs the results that arrived for the calls this segment admitted:
// each settled delivery the intake parked becomes the observation of its call,
// observed through the pipeline of the decision that admitted it. The harvest
// never waits: a call whose result has not arrived stays pending, and the loop
// returns to the pull beside it.
func (s *segment) harvest(ctx context.Context, l *Loop, awaited *sync.Map) []model.Message {
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
		if awaited != nil {
			awaited.Store(taskID, TaskConsumed)
		}
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

// Consumers owns the resident consumption loops of a run: exactly one per
// complete agent address, each the only driver of the model decisions of its
// address. The set is address-blind — an instance mounts at the first activity
// of the address its material names, whether that address belongs to the entry
// agent or to any other agent the run declared, and no address is guessed from
// the entry the document names. The loops live as long as the context they were
// started under; a loop that ended is never restarted in place, the chain
// carries the position a restart resumes from, and a set stops mounting
// instances once the run it belongs to stopped. Every closed segment hands its
// fresh answer to the one delivery face of the set, so the answer of a segment
// no entry waits on leaves through the same path the answer of a waited segment
// does.
type Consumers struct {
	loop    *Loop
	base    context.Context
	deliver SegmentDelivery
	// turnContext decorates the decision context of every segment these loops
	// fly; nil leaves it as it is.
	turnContext func(ctx context.Context, trigger string) context.Context
	mu          sync.Mutex
	live        map[event.Address]*Consumer
	// stopped reports whether the run these loops belong to closed: a stopped
	// set mounts no instance, so the calls of a closing run never start a
	// driver beside the drain they are waiting on.
	stopped bool
	// progress is the pulse every loop of this set reports its movement
	// through; nil reports nothing.
	progress func()
}

// SetProgress attaches the pulse every loop of this set reports its movement
// through, the mounted ones included. The setter is separate from the
// constructor so the declared construction shape of the set stays as it is.
func (cs *Consumers) SetProgress(pulse func()) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.progress = pulse
	for _, consumer := range cs.live {
		consumer.SetProgress(pulse)
	}
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

// Ensure answers the resident instance of one address, assembling and starting
// it on first use. The address is the one the material itself names, so an
// instance mounts for every complete agent address the run drives, never for the
// entry agent alone.
//
// - Exactly one instance per address: the second Ensure of an address answers the instance already mounted, so one address never runs two drivers of its own decisions.
// - The lifecycle is the run's: an instance lives as long as the context the set was built under, and a set that stopped mounts none, so a call arriving beside a drained run answers no instance instead of starting a driver beside the drain.
// - The loop resumes behind the last agent_output the chain carries — the inputs past it are the material a restart still owes — and a chain beside no agent_output at all resumes from its head, so an unanswered input stays owed instead of vanishing behind a restart.
func (cs *Consumers) Ensure(addr event.Address) *Consumer {
	if cs == nil || !addr.IsComplete() {
		return nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.stopped {
		return nil
	}
	if consumer, ok := cs.live[addr]; ok {
		return consumer
	}
	consumer := newConsumer(addr, cs.loop, cs.deliver)
	consumer.turnContext = cs.turnContext
	consumer.progress = cs.progress
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

// binding reports the binding this address decides on, or nil while its material names
// none: the read is taken under the mutex the mounting writer holds.
func (c *Consumer) binding() *state.AgentBinding {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bound
}

// mount answers the resident instance of one address bound to the named binding: the
// second Ensure of the address answers the instance already mounted, and an instance
// mounted beside no binding of its own takes the binding this call names. The binding
// is the one the decisions of the address run on, so a delegated execution site decides
// on the assembled sub-agent's own material instead of a declared agent's generation.
func (cs *Consumers) mount(addr event.Address, bound *state.AgentBinding) *Consumer {
	consumer := cs.Ensure(addr)
	if consumer == nil || bound == nil {
		return consumer
	}
	consumer.mu.Lock()
	consumer.bound = bound
	consumer.mu.Unlock()
	return consumer
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

// Resting reports whether every loop this set mounted stands between segments,
// so the set writes nothing more at any of its addresses. A set this host mounts
// nothing on reports resting: nothing of it flies.
func (cs *Consumers) Resting() bool {
	if cs == nil {
		return true
	}
	cs.mu.Lock()
	live := make([]*Consumer, 0, len(cs.live))
	for _, consumer := range cs.live {
		live = append(live, consumer)
	}
	cs.mu.Unlock()
	for _, consumer := range live {
		if !consumer.Resting() {
			return false
		}
	}
	return true
}

// Stop ends every consumption loop this set owns; the loops answer their done
// channels, no further instance mounts for any address, and the segments the
// ended loops left flying stay on the chain for a restart.
func (cs *Consumers) Stop() {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.stopped = true
	for _, consumer := range cs.live {
		if consumer.cancel != nil {
			consumer.cancel()
		}
	}
}

// ErrShutdownRefused and ErrDrainUnsettled are the named refusals of the close:
// each one names the case it refuses instead of answering a silent default.
var (
	// ErrShutdownRefused reports new input offered to an agent whose close has
	// begun. The close refuses new input first and admits nothing beside the
	// drain.
	ErrShutdownRefused = errors.New("agent: the close of this agent refuses new input")
	// ErrDrainUnsettled reports a close that ended with obligations still open.
	// An honest close reports this named unsettled state beside the reasons of
	// what stayed open instead of a clean end.
	ErrDrainUnsettled = errors.New("agent: the close did not reach real termination")
)

// reasonWriterNotStopped is the named reason beside a writer that has not
// confirmed its stop when a close reports what stayed open.
const reasonWriterNotStopped = "this writer has not confirmed its stop"

// Reconciler is the delegation ledger a close lands the terminals of admitted
// calls through: a terminal whose obligations have cleared is landed and the
// booking beside it closes, while an obligation still open keeps the terminal
// withheld exactly as it does beside a live turn. The close reads it as one of
// its criteria, so a run is never reported at rest beside a terminal the ledger
// still holds.
//
// The redline list (3 rules):
// MUST land a pending terminal only once the obligations of that call are cleared.
// MUST report every booking it still holds, so a close never reports a rest beside an unlanded terminal.
// MUST NOT hold a terminal past the obligations it waits on, and MUST NOT report one before them.
type Reconciler interface {
	// Reconcile attempts the landing of one admitted call's terminal again and
	// reports the status the record stands at afterwards.
	Reconcile(ctx context.Context, taskID event.TaskID) (task.Status, error)
	// Pending reports how many fresh admissions the ledger still books; each
	// booking closes only when its terminal really lands.
	Pending() int
}

// writer is one production a run started: the name a close reports it under.
// The registration itself is the confirmation — a writer is live from the moment
// its work began and stopped only at the moment that work really ended.
type writer struct {
	name string
}

// Writers is the ledger of the writers one run started: the whole drive of one
// invocation, the model stream of each of its decisions, and the tool production
// of each admitted call. A close reads it as one of its criteria, so a run writes
// nothing more only once every writer confirmed its stop, and the ones that did
// not are named in the report beside the work they were running. A writer is not
// a task: the model stream in flight holds no task identity at all, so the ledger
// names the production itself rather than minting an identity the task account
// would have to carry, and the stop is confirmed by the production alone.
//
// The redline list (3 rules):
// MUST register a writer when its work really starts and confirm its stop only when that work really ended.
// MUST NOT read a cancelled request, an emptied queue or an elapsed budget as a stop.
// MUST NOT report a writer stopped for one whose confirmation still answers false.
type Writers struct {
	mu   sync.Mutex
	seq  int
	live map[int]writer
}

// NewWriters returns an empty writer ledger.
func NewWriters() *Writers {
	return &Writers{live: map[int]writer{}}
}

// Begin registers one writer that is starting now and answers the confirmation
// that closes its registration. The confirmation is idempotent and safe to call
// from a defer: a second call reports the same stop and drops no other writer.
// A ledger beside nil registers nothing and answers a confirmation that reports
// nothing, so a loop or a run without a ledger keeps running unregistered.
func (w *Writers) Begin(name string) func() {
	if w == nil {
		return func() {}
	}
	w.mu.Lock()
	w.seq++
	slot := w.seq
	w.live[slot] = writer{name: name}
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			delete(w.live, slot)
			w.mu.Unlock()
		})
	}
}

// Stopped reports whether every registered writer confirmed its stop, and names
// the ones that did not in registration order. A writer whose registration is
// still open has not stopped: the work it runs began and its end was never
// confirmed, whatever the state of the request that started it.
func (w *Writers) Stopped() (bool, []string) {
	if w == nil {
		return true, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.live) == 0 {
		return true, nil
	}
	slots := make([]int, 0, len(w.live))
	for slot := range w.live {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	names := make([]string, 0, len(slots))
	for _, slot := range slots {
		names = append(names, w.live[slot].name)
	}
	return false, names
}

// WriterObligations renders the writers that have not confirmed their stop as
// the report entries of one close: each one named by the production it runs
// instead of by a task identity it never held.
func WriterObligations(names []string) []UnsettledObligation {
	out := make([]UnsettledObligation, 0, len(names))
	for _, name := range names {
		out = append(out, UnsettledObligation{Reason: reasonWriterNotStopped + ": " + name})
	}
	return out
}
