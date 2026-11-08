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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	// batch marks a call admitted as part of one whole response the fuse held for
	// an approver and the approval released: the follow-up decision of the
	// segment waits for every call of that batch and reads their results as one.
	batch bool
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
	// whole counts the calls of one approved batch whose settled results this
	// segment has not paired yet: the follow-up decision fires once, after the
	// last of them lands, so the decision reads every result as one batch.
	whole int
	// paired names the tasks whose settled result this segment took: the
	// consumption of each is written beside the delivery of this segment, never
	// beside the pairing alone.
	paired   []event.TaskID
	pipeline *plugin.Pipeline
	started  time.Time
	rounds   int
	dirty    bool
	// trigger is the trigger-source stamp of the material the segment opened
	// on: the lineage its closing answer leaves under.
	trigger string
	// explicit marks the segment an explicit drive submitted: the synchronous
	// face that submitted it owns the answer, so no delivery face is called and
	// the capture is sealed once when that face returns.
	explicit bool
	// answered reports whether the closing decision's assistant message already
	// entered the transcript: a tool-call round appends it before the segment
	// continues, so the closing answer of that round is not committed twice.
	answered bool
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
	seeded bool
	flying *segment
	// busy reports that this loop stands inside a consumption pass: a pull is
	// being read, or a segment is being opened. A close that reads the loop as
	// resting reads a writer stopped, and a loop that stands between a pull and
	// the segment it opens still writes — the pull marks its facts read and the
	// segment commits the answer — so the window between them is a writer at
	// work, never a loop at rest.
	busy bool
	// pendingPass counts the consumption passes this loop queued but has not
	// entered yet: a wake the channel accepted and the boot pass a mount owes.
	// A loop that took its wake off the channel and has not entered the pass it
	// asked for is still a writer at work, so the count is what covers the
	// window between the wake and the pass, never the channel length alone. The
	// count lives in this process only: no restart reads it.
	pendingPass int
	// segCount counts the segments this consumer opened, so each minted
	// invocation identity names the segment it belongs to.
	segCount int
	// answers indexes the closed answers by the key of every input the closing
	// segment consumed, so an entry that starts waiting after its segment closed
	// reads that answer by key instead of scanning a window of recent segments.
	// order names the retained answers by the age they closed at, so the window
	// evicts the oldest answer whole.
	answers map[event.EventID]*closedAnswer
	order   []*closedAnswer
	// set is the consumption set this instance was mounted by: the instance is
	// forgotten from that set and from the loop's registry when its loop ends.
	set    *Consumers
	notify chan struct{}
	// explicit marks the consumer an explicit drive assembled for one invocation
	// and never mounted: it flies that segment synchronously on the caller's
	// goroutine, so no second driver may wake it.
	explicit bool
	// quiet marks a consumer mounted for a delegated batch: the first read is
	// suppressed, because the batch is committed after the mount and woken on
	// once it is whole, so a read that ran in between could take one fact of it
	// and drive half a round. Every later input of the address reaches the loop
	// through the wake its producer sends after the commit.
	quiet bool
	// closed is the answer of the last segment this consumer settled, kept so the
	// explicit drive that flew it answers the very turn the segment closed on.
	closed *SegmentAnswer
}

// answerWindow bounds how many closed answers stay readable by their input
// keys: an input is consumed by the segment that pulled it, and a late reader
// finds that answer unless answerWindow further segments closed on the same
// address first. The count is of answers rather than of inputs, so a segment
// that consumed a batch evicts one entry and not one per input.
const answerWindow = 256

// answerTTL bounds how long one closed answer stays readable by its input keys:
// an entry that was admitted long before it waits reads the answer of a segment
// that closed inside this window, and a wait that starts past it is answered by
// the segment that consumes the input next rather than by a stale answer.
const answerTTL = 5 * time.Minute

// closedAnswer is one closed segment's answer as the entries waiting on its
// inputs read it, beside the moment it closed.
type closedAnswer struct {
	answer SegmentAnswer
	at     time.Time
}

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
		answers: map[event.EventID]*closedAnswer{},
		notify:  make(chan struct{}),
	}
}

// driveInvocation flies one explicit invocation on this consumer: the ready
// facts of the invocation open the segment it names, the consumer runs the same
// decision, harvest and seal path it runs for a resident address, and the caller
// reads the turn the segment closed on. It is the whole body Loop.Drive keeps, so
// the loop holds no second decision, harvest or seal path beside the Consumer.
//
// - The consumer is never mounted, so no wake can start a second segment beside the one this call flies.
// - The capture is sealed once, when this face returns, exactly as the direct drive sealed its turn.
// - The closed answer is read from the segment this consumer settled, never re-derived from the chain.
// - A context that ends before the segment closes answers the named cancellation of the abandoned segment.
func (c *Consumer) driveInvocation(ctx context.Context, inv Invocation) (turn Turn, err error) {
	turn = Turn{InvocationID: inv.InvocationID, Terminal: model.KindUnknown}
	started := time.Now()
	consumerDebug("tagent: consumer drive start agent=%s session=%s invocation=%s ready=%d",
		inv.Address.AgentID, inv.Address.SessionID, inv.InvocationID, len(inv.Ready))
	defer func() {
		consumerDebug("tagent: consumer drive end invocation=%s decisions=%d terminal=%s elapsed=%s failed=%t",
			inv.InvocationID, turn.DecisionCount, turn.Terminal, time.Since(started).Round(time.Microsecond), err != nil)
	}()
	production := c.loop.beginWriter("drive:" + string(inv.InvocationID))
	defer production()
	defer c.loop.sealCapture(ctx)
	c.openSegmentFor(ctx, inv.InvocationID, inv.Ready, inv.Ready, true)
	c.consume(ctx)
	answer, ok := c.closedAnswer()
	if !ok {
		c.abandonSegment()
		answer, _ = c.closedAnswer()
	}
	turn = answer.Turn
	if turn.InvocationID == "" {
		turn.InvocationID = inv.InvocationID
	}
	return turn, answer.Err
}

// closedAnswer reports the answer of the last segment this consumer settled, or
// none while no segment closed yet.
func (c *Consumer) closedAnswer() (SegmentAnswer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed == nil {
		return SegmentAnswer{}, false
	}
	return *c.closed, true
}

// run is the resident loop: wait for a wake, consume everything that arrived,
// wait again. A cancelled context ends the loop, and the segment it still flies
// is closed by this loop before it returns (see abandonSegment), so a set that
// joins on this loop's done channel reads a segment that really ended.
//
// A set marked stopped ends this loop on its own, and only at rest: the mark
// cancels nothing, so the obligations the loop already holds are finished for
// real inside the budget of the close that marked the set, and a wake that
// already arrived outranks the mark because the pass it asks for is owed.
//
// - The turn of work one pass drives is registered as one writer of the run's writer ledger (see consume), so a close reads a loop that still consumes as a writer that has not stopped.
// - An abnormal exit settles the flying segment before done closes, so the address rests honestly beside ended work.
// - A fresh mount reads the chain once before it waits; a quiet mount or a chain that holds no fact gives the queued boot pass back.
func (c *Consumer) run(ctx context.Context) {
	defer close(c.done)
	defer c.abandonIfFlying()
	defer c.loop.forgetInstance(c)
	if !c.set.halted() && !c.quiet && c.chainHeldAtMount() {
		c.consume(ctx)
		if ctx.Err() != nil {
			c.abandonSegment()
			c.drainWake()
			return
		}
		c.noteProgress()
	} else {
		c.takePass()
	}
	for {
		select {
		case <-c.wake:
		default:
			select {
			case <-ctx.Done():
				c.abandonSegment()
				c.drainWake()
				return
			case <-c.set.haltSignal():
				if c.Resting() {
					return
				}
				select {
				case <-ctx.Done():
					c.abandonSegment()
					c.drainWake()
					return
				case <-c.wake:
				}
			case <-c.wake:
			}
		}
		c.consume(ctx)
		if ctx.Err() != nil {
			c.abandonSegment()
			c.drainWake()
			return
		}
		c.noteProgress()
		if c.set.halted() && c.Resting() {
			return
		}
	}
}

// chainHeldAtMount reports whether the chain of this address held any fact when
// the instance started: a chain beside no fact holds nothing a first read could
// consume, and the fact a producer commits beside the mount is woken on after
// its commit.
func (c *Consumer) chainHeldAtMount() bool {
	return len(c.loop.sessionFacts(c.addr)) > 0
}

// drainWake drops the wakes still buffered on a loop that is ending: the
// material they ask for stays owed on the chain for the restart, and a dead loop
// must read resting instead of naming a pass it will never run. The queued
// passes leave the book with the wakes: no read will enter one of them, and a
// count kept past the loop would leave the address reading not resting forever.
func (c *Consumer) drainWake() {
	for {
		select {
		case <-c.wake:
		default:
			c.mu.Lock()
			c.pendingPass = 0
			c.mu.Unlock()
			return
		}
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
//
// - Only a wake the channel really accepted queues a pass: a merged wake asks for the pass an earlier token already owes, so it repeats no count.
// - The pass is queued before the token lands, so the loop that takes the token and has not entered the pass yet is never read as resting.
func (c *Consumer) Wake() {
	c.queuePass()
	select {
	case c.wake <- struct{}{}:
	default:
		c.takePass()
	}
}

// queuePass records one consumption pass this loop queued and will enter.
func (c *Consumer) queuePass() {
	c.mu.Lock()
	c.pendingPass++
	c.mu.Unlock()
}

// takePass records that a queued pass was entered, or given back beside a mount
// whose read is skipped. A pass that no wake or mount queued takes nothing: the
// explicit drive runs its segment on the caller's goroutine and owes no pass of
// the loop. The count never goes negative, so a pass queued later still covers
// the wake it belongs to.
func (c *Consumer) takePass() {
	c.mu.Lock()
	if c.pendingPass > 0 {
		c.pendingPass--
	}
	c.mu.Unlock()
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
//
// The whole pass is registered as one writer of the run's writer ledger, named
// by the entry invocation the driving material of its first pull names — the
// drive of that invocation is this pass, its queueing waits included — or by
// the address alone beside material no invocation claims. The registration
// opens before the first segment of the pass does and closes when the pass
// returns, so a close that reads the ledger while the pass flies names the
// production that holds it open. A loop beside no writer ledger registers
// nothing.
func (c *Consumer) consume(ctx context.Context) {
	c.takePass()
	c.setBusy(true)
	defer c.setBusy(false)
	var confirm func()
	defer func() {
		if confirm != nil {
			confirm()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if c.explicit && c.flyingSegment() == nil {
			return
		}
		if c.flyingSegment() == nil {
			pull := c.pullDriving()
			if !pull.drove() {
				return
			}
			if confirm == nil {
				confirm = c.loop.beginWriter(passWriterName(pull.driving, c.addr))
			}
			c.openSegment(ctx, pull.driving, pull.material)
		}
		if !c.driveSegment(ctx, c.flyingSegment()) {
			if c.explicit {
				if seg := c.flyingSegment(); seg != nil && len(seg.pending) > 0 {
					c.waitSettled(ctx, seg)
					continue
				}
			}
			return
		}
	}
}

// waitSettled waits for the settled result of every call the explicit segment
// still admits: the synchronous face that submitted the segment runs its whole
// multi-round exchange under one call, so each result is paired before the next
// decision reads. The wait hands each arrival back to the intake once it lands,
// and the harvest of the next round is what pairs it, so the observation keeps
// the one pairing path the resident loop runs. A wait the context ended leaves
// the calls to the abandonment the caller's return drives.
func (c *Consumer) waitSettled(ctx context.Context, seg *segment) {
	in := c.loop.intakeFor(seg.inv.InvocationID)
	for taskID := range seg.pending {
		arrival, ok := in.await(ctx, taskID)
		if !ok {
			return
		}
		in.deliver(taskID, arrival)
	}
}

// passWriterName names the production one consumption pass runs: the drive of
// the entry invocation its driving material names — the whole wait of that
// invocation is the pass that consumes its input — or the consumption of the
// address alone beside material no invocation claims. The name is what a close
// reports the loop under when it has not confirmed its stop.
func passWriterName(driving []event.Event, addr event.Address) string {
	for _, evt := range driving {
		if evt.InvocationID != "" {
			return "drive:" + string(evt.InvocationID)
		}
	}
	return "consumer:" + string(addr.AgentID) + "/" + string(addr.SessionID)
}

// Resting reports whether this loop stands between segments: no segment flies, no
// consumption pass is running, no pass is owed, and no wake waits to open one.
// The loop is the writer of its own address, so a loop that rests writes nothing
// more at that site, and a close that reads it resting reads a writer stopped
// rather than one merely paused. A pass between its pull and the segment it
// opens is in that window, so the pass itself is what rests the loop, never the
// arrival of a segment to fly — and a wake the loop already took off its channel
// is still the pass it owes, never a loop between passes.
// - The first clause is the whole judgment of a dead loop: a loop whose run ended
// leaves the segment it flew closed (see abandonSegment), so an ended loop beside a
// segment still flying answers not resting here instead of reading its shutdown as
// rest.
func (c *Consumer) Resting() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	busy, flying, pending := c.busy, c.flying, c.pendingPass
	c.mu.Unlock()
	if busy || flying != nil || pending > 0 {
		return false
	}
	return len(c.wake) == 0
}

// setBusy records whether this loop stands inside a consumption pass, so a read
// of Resting beside it answers a writer at work rather than a loop between
// segments.
func (c *Consumer) setBusy(busy bool) {
	c.mu.Lock()
	c.busy = busy
	c.mu.Unlock()
}

// TaskState names what became of one settled result on this address: a flying
// segment still awaits it, a segment delivered the answer that paired it, or a
// closed segment left it unpaired. The notice-driven follow-up reads the state to
// know whether any consumer already took the result.
type TaskState string

const (
	// TaskAwaited: the flying segment admitted the task and its delivery that
	// pairs it has not landed yet.
	TaskAwaited TaskState = "awaited"
	// TaskConsumed: a segment paired the result and delivered the answer of it.
	// The pairing alone never answers this state, and the round leaves it once
	// the pull receives the terminal notice the delivery answered: the state
	// never outlives the round it reports, so a later round of the same task is
	// not read as one this book already took.
	TaskConsumed TaskState = "consumed"
	// TaskOrphaned: the segment that admitted the task closed without pairing it.
	TaskOrphaned TaskState = "orphaned"
)

// AwaitTaskState waits until the named task stops being awaited by a flying
// segment and answers the settled state: the segment either delivers the answer
// that pairs the result (consumed) or closes beside it (orphaned). The pairing a
// segment takes before it closes never answers consumed by itself, so the wait
// spans the whole window between admission and delivery. A wait the context ends
// answers the state as it stands, so the caller decides over a named moment
// instead of guessing across that window.
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
// stand, while the material they carry belongs to the segment only while the
// request prefix presents none of it — a fact the prefix carries is already
// read by every request of this range, so presenting it again would read one
// fact as two, and a fact the prefix leaves out (behind the ended boundary of
// its chain, or replaced there by a summary, a ticket or a fold product) carries
// wherever it stands: the segment that pulled before it landed commits its
// answer after, so a boundary read off the chain alone would leave the body of
// that input behind an answer no request ever read.
// A loop beside no presentation face assembles no prefix and keeps the chain's
// own boundary: the material of a pull is what stands past the last
// agent_output the chain holds.
// The cursor is the identity of the last fact the chain held at the read, so a
// fold rewrite that hides it re-reads the chain head — the read set keeps each
// fact once by its own identity. A fact the face that fires an owed attempt
// named drives wherever it stands, and the read of the whole chain starts from
// the head while a name waits, because the fact it names stands behind the
// cursor the previous pull left.
type pulledEvents struct {
	// driving lists every driving fact the pull took, carrying or not.
	driving []event.Event
	// material lists the driving facts whose body the segment carries: the ones
	// the request prefix presents nowhere.
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
	lastOutput := lastAnswerIndex(facts)
	shown, byPrefix := c.loop.sessionPresented(c.addr)
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
		named := c.named[evt.Key]
		if evt.Key == "" {
			continue
		}
		if named && evt.TaskID != "" {
			switch c.TaskState(evt.TaskID) {
			case TaskAwaited:
				if terminalTaskNotice(evt) {
					continue
				}
				delete(c.named, evt.Key)
				continue
			case TaskConsumed:
				if terminalTaskNotice(evt) {
					c.releaseClosedRoundLocked(evt)
				} else {
					delete(c.named, evt.Key)
				}
				continue
			}
		}
		if !startsRange(evt) && !(named && relatedToAddress(evt, c.addr)) {
			continue
		}
		if c.read[evt.Key] && !named {
			continue
		}
		delete(c.named, evt.Key)
		c.read[evt.Key] = true
		c.taken[evt.Key] = true
		if evt.TaskID != "" && terminalTaskNotice(evt) {
			c.awaited.Delete(evt.TaskID)
		}
		pull.driving = append(pull.driving, evt)
		carries := at > lastOutput
		if byPrefix {
			carries = !shown[evt.Key]
		}
		if !carries {
			continue
		}
		if evt.Type == event.TypeTaskSettled && observationless(evt.Message) {
			if result, resultAt, held := resultFactOf(facts, evt.TaskID); held && !presentedFact(result, resultAt, lastOutput, shown, byPrefix) {
				pull.material = append(pull.material, result)
			}
			continue
		}
		pull.material = append(pull.material, evt)
	}
	c.cursor = facts[len(facts)-1].Key
	return pull
}

// releaseClosedRoundLocked accounts the terminal notice of one closed round as
// read and releases the round's book entry: the notice was answered by the
// segment that paired its result, so it drives no segment of its own, and the
// next round of the same task must find the book free to drive again. The fact
// is named taken, so a retried wake that names the same notice never reads it as
// material. The caller holds the loop's book lock.
func (c *Consumer) releaseClosedRoundLocked(evt event.Event) {
	delete(c.named, evt.Key)
	c.read[evt.Key] = true
	c.taken[evt.Key] = true
	c.awaited.Delete(evt.TaskID)
}

// terminalTaskNotice reports whether one fact is the terminal notification of
// its task: the delivery fact a settled round commits, whose type is the task
// settle, and the notice material a follow-up turn reads, stamped with a
// terminal kind. A progress report is no terminal notice: the book it meets
// stays as it stands.
func terminalTaskNotice(evt event.Event) bool {
	if evt.TaskID == "" {
		return false
	}
	if evt.Type == event.TypeTaskSettled {
		return true
	}
	switch task.NoticeKind(evt.Meta[event.MetaKeyNoticeKind]) {
	case task.NoticeSettled, task.NoticeFailed, task.NoticeCancelled:
		return true
	}
	switch task.NoticeKind(evt.Meta[event.MetaKeySettleTriggerSource]) {
	case task.NoticeSettled, task.NoticeFailed, task.NoticeCancelled:
		return true
	}
	return false
}

// observationless reports whether one delivery fact carries no settled body:
// the notification names the result, and the result fact is where its text
// stands.
func observationless(msg model.Message) bool {
	return msg.Content == "" && msg.ToolID == "" && msg.ToolName == "" && len(msg.ToolCalls) == 0
}

// resultFactOf reports the result fact a delivery fact points at, beside its
// position in the chain read: a task settles under the one result fact that
// carries its body, so the delivery never needs a second copy to drive on.
func resultFactOf(facts []event.Event, taskID event.TaskID) (event.Event, int, bool) {
	if taskID == "" {
		return event.Event{}, 0, false
	}
	for at := len(facts) - 1; at >= 0; at-- {
		evt := facts[at]
		if evt.TaskID == taskID && evt.Type == event.TypeActionCommand {
			return evt, at, true
		}
	}
	return event.Event{}, 0, false
}

// presentedFact reports whether the request prefix of one session already
// presents one chain fact at its own text: the position past the last answer
// when the loop assembles no prefix, and the presented read of the request
// boundary otherwise. The segment that drives on the fact carries it only when
// the prefix carries it nowhere.
func presentedFact(evt event.Event, at, lastOutput int, shown map[event.EventID]bool, byPrefix bool) bool {
	if byPrefix {
		return shown[evt.Key]
	}
	return at <= lastOutput
}

// relatedToAddress reports whether one committed fact belongs to the address it
// stands on: the ownership block names that address as the owner, the source or
// the target. A fact a wake names is read as driving only while it is related
// this way, so a name can never make a neighbour's fact drive this address.
func relatedToAddress(evt event.Event, addr event.Address) bool {
	return evt.Owner == addr || evt.Source == addr || evt.Target == addr
}

// lastAnswerIndex reports the position of the last agent_output fact one session
// chain holds: the boundary "the round the chain last answered" is read from, and
// the one place that boundary is computed, so a pull, a boot and the rest
// criterion of a run all measure it the same way.
func lastAnswerIndex(facts []event.Event) int {
	last := -1
	for at, evt := range facts {
		if evt.Type == event.TypeAgentOutput {
			last = at
		}
	}
	return last
}

// PendingInputs reports the driving facts one session chain holds with no round
// answered for them: the facts that start a range and stand past the last
// agent_output the chain committed, in commit order. It is the consumption
// position of a chain read back from the chain itself, never from a ledger kept
// beside it: the answer a round closes on is the one boundary the chain records,
// so an input the chain committed and no answer closed on is still owed a drive.
//
// - A chain beside no agent_output is one range in flight from its head, so every driving fact it holds is pending.
// - A fact that starts no range belongs to the range that committed it and is pending never, since its range's answer is what closes it.
func PendingInputs(facts []event.Event) []event.Event {
	last := lastAnswerIndex(facts)
	pending := make([]event.Event, 0, len(facts))
	for at := last + 1; at < len(facts); at++ {
		evt := facts[at]
		if evt.Key == "" || !startsRange(evt) {
			continue
		}
		pending = append(pending, evt)
	}
	return pending
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
// it opens on and the material those facts carry, and observes each driving fact
// once. The address now flies this segment until it closes.
func (c *Consumer) openSegment(ctx context.Context, driving, material []event.Event) *segment {
	return c.openSegmentFor(ctx, "", driving, material, false)
}

// openSegmentFor opens one segment under the invocation identity the caller
// names, or one this consumer mints beside an empty name. An explicit drive
// names its invocation and marks the segment explicit: the synchronous face that
// submitted it owns the answer, the capture seals when it returns, and the
// segment logs the drive's start and end rather than the resident open and
// close. The ready facts of an explicit drive are marked read beside the open,
// so a chain that carries them does not hand them to the first pull a second
// time.
func (c *Consumer) openSegmentFor(ctx context.Context, invocationID event.InvocationID, driving, material []event.Event, explicit bool) *segment {
	c.mu.Lock()
	c.segCount++
	count := c.segCount
	c.mu.Unlock()
	id := invocationID
	if id == "" {
		id = event.InvocationID(fmt.Sprintf("%s#c%d", c.addr.SessionID, count))
	}
	inv := Invocation{
		InvocationID: id,
		Address:      c.addr,
		Instance:     c.loop.instance,
		Ready:        material,
	}
	c.loop.intakeFor(id).setAddress(c.addr)
	seg := &segment{
		inv:      inv,
		policy:   c.loop.finalRoundFor(c.addr.AgentID),
		history:  c.loop.sessionHistory(c.addr),
		pending:  map[event.TaskID]admittedCall{},
		started:  time.Now(),
		dirty:    true,
		trigger:  drivingTrigger(driving),
		explicit: explicit,
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
	if explicit {
		c.mu.Lock()
		for _, evt := range driving {
			if evt.Key != "" {
				c.read[evt.Key] = true
			}
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.flying = seg
	c.mu.Unlock()
	if !explicit {
		consumerDebug("tagent: consumer segment open agent=%s session=%s invocation=%s ready=%d",
			c.addr.AgentID, c.addr.SessionID, id, len(driving))
	}
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
// - The calls of an approved batch are the one exception to the park: the follow-up decision waits until every call of the batch has settled, so one decision reads the whole batch as the set the approver released instead of one turn per settled result.
// - The closing round admits none of the calls it issued: the receipts it leaves are the refusals the model reads, and no further round may read them, so the segment closes on the closing round exactly as the direct drive ends on the one it ran.
// - An explicit drive reads the address the same way a resident segment does:
// every related fact past the boundary may open or continue the segment, so the
// face that submits one invocation does not run a second boundary rule beside
// the consumer's.
func (c *Consumer) driveSegment(ctx context.Context, seg *segment) bool {
	harvested := seg.harvest(ctx, c.loop)
	if len(harvested) > 0 {
		seg.transcript = append(seg.transcript, harvested...)
		seg.dirty = true
	}
	pull := c.pullDriving()
	c.applyPull(ctx, seg, pull)
	if seg.explicit {
		if notes := c.loop.terminalNotes(seg.inv); len(notes) > 0 {
			seg.transcript = append(seg.transcript, notes...)
			seg.dirty = true
		}
	}
	if seg.whole > 0 {
		return false
	}
	if !seg.dirty {
		return false
	}
	round := closingRound(seg.policy, seg.rounds, time.Since(seg.started))
	if !seg.explicit {
		round = c.closingRoundOf(seg, pull.chain)
	}
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
	seg.answered = false
	if flow, flowErr := plugin.NewFlow(decision.Binding.Plugins); flowErr == nil {
		seg.pipeline = &flow
	}
	for _, adm := range running {
		seg.pending[adm.taskID] = adm
		if adm.batch {
			seg.whole++
		}
		c.awaited.Store(adm.taskID, TaskAwaited)
	}
	if outcome.Kind == model.KindCompleted && outcome.Body != nil && outcome.Body.IsToolCallResponse() {
		seg.transcript = append(seg.transcript, assistantOf(outcome))
		seg.transcript = append(seg.transcript, receipts...)
		seg.answered = true
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
	late := seg.harvest(ctx, c.loop)
	if len(late) > 0 {
		seg.transcript = append(seg.transcript, late...)
		seg.dirty = true
		return true
	}
	c.closeSegment(ctx, seg, decision, outcome, round, nil)
	return true
}

// applyPull lands one pull on a segment: every driving fact is observed and
// counted as consumed, and the material the segment must read joins its
// transcript. The pull's own report is what says whether anything drove.
func (c *Consumer) applyPull(ctx context.Context, seg *segment, pull pulledEvents) {
	if pull.drove() {
		seg.dirty = true
	}
	for _, evt := range pull.driving {
		c.loop.observeFact(ctx, seg.inv, evt)
		if evt.Key != "" {
			seg.consumed = append(seg.consumed, evt.Key)
		}
	}
	if len(pull.material) == 0 {
		return
	}
	if msgs, err := aggregateReady(pull.material); err != nil {
		consumerWarn("tagent: the fresh input of %s did not aggregate: %v", seg.inv.InvocationID, err)
	} else {
		seg.transcript = append(seg.transcript, msgs...)
	}
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
// failure reports that cause to its waiters and commits no output fact. The tasks
// whose settled result the segment paired turn consumed inside that same delivery:
// the consumption is written after the answer left and never before it, so a
// segment that never delivers leaves them awaited and the entry of a settlement is
// never closed on a turn nobody read.
func (c *Consumer) closeSegment(ctx context.Context, seg *segment, decision Decision, outcome model.Outcome, round decisionRound, cause error) {
	turn := Turn{
		InvocationID:  seg.inv.InvocationID,
		DecisionCount: len(seg.decisions),
		Terminal:      outcome.Kind,
	}
	if round.final {
		turn.FinalRound = round.reason
	}
	if outcome.Kind == model.KindFailed {
		if err := outcome.Cause(); err != nil {
			turn.Failure = strings.TrimSpace(err.Error())
		}
	}
	transcript := seg.transcript
	if seg.explicit && !seg.answered {
		transcript = append(transcript, assistantOf(outcome))
	}
	c.loop.commitTranscript(c.addr, transcript)
	if cause == nil {
		if err := c.loop.commitOutput(ctx, seg.inv, decision, outcome, seg.consumed); err != nil {
			cause = fmt.Errorf("tagent: the output fact of the segment did not commit: %w", err)
		}
	}
	if cause != nil {
		turn.Terminal = model.KindFailed
		turn.Failure = strings.TrimSpace(cause.Error())
	}
	answer := SegmentAnswer{
		Turn:     turn,
		Err:      cause,
		Trigger:  seg.trigger,
		Consumed: append([]event.EventID(nil), seg.consumed...),
		Body:     assistantOf(outcome).Content,
	}
	c.settleSegment(ctx, seg, answer, true)
	if !seg.explicit {
		consumerDebug("tagent: consumer segment close invocation=%s decisions=%d terminal=%s consumed=%d",
			seg.inv.InvocationID, turn.DecisionCount, turn.Terminal, len(seg.consumed))
	}
}

// abandonIfFlying settles the segment an abnormal exit left flying — a panic
// or a Goexit inside a model or plugin call — so Resting answers true beside
// ended work. The normal paths settle their own segments; this guard only
// answers the exits that skip them.
func (c *Consumer) abandonIfFlying() {
	if c.flyingSegment() != nil {
		c.abandonSegment()
	}
	c.drainWake()
}

// abandonSegment closes the segment this loop still flies when the loop itself is
// cancelled: the stop of the address is answered by closing what it left flying,
// so no segment outlives the loop that drove it and no entry waiting on one
// blocks on a driver that already returned.
//
// - The abandoned answer is a cancellation by name: the entries waiting on the segment read the named cause instead of the result that never arrived.
// - It writes nothing: the transcript the segment accumulated is not committed and no output fact lands, because the input that segment read stays owed on the chain for the restart, and a committed transcript beside an uncommitted input would read one fact as two.
// - It hands nothing to the delivery face: no turn was committed, so no entry's settle account closes behind an answer nobody produced. The results it paired are read as orphaned exactly as the ones still awaited, so the notice of each drives the follow-up that settles it.
func (c *Consumer) abandonSegment() {
	seg := c.flyingSegment()
	if seg == nil {
		return
	}
	answer := SegmentAnswer{
		Turn: Turn{
			InvocationID:  seg.inv.InvocationID,
			DecisionCount: len(seg.decisions),
			Terminal:      model.KindCancelled,
			Failure:       ErrSegmentCancelled.Error(),
		},
		Err:      ErrSegmentCancelled,
		Trigger:  seg.trigger,
		Consumed: append([]event.EventID(nil), seg.consumed...),
	}
	c.settleSegment(context.Background(), seg, answer, false)
	if !seg.explicit {
		consumerDebug("tagent: consumer segment abandoned invocation=%s decisions=%d consumed=%d",
			seg.inv.InvocationID, len(seg.decisions), len(seg.consumed))
	}
}

// settleSegment lands the one answer of a closed segment and hands the address back
// to the next pull: a committed answer leaves through the delivery face of the
// address, the closed answer is indexed by the inputs it consumed so the entries
// waiting on them read it, and the tasks the segment admitted but never paired are
// read as orphaned. A segment that committed nothing (committed false) answers its
// waiters and the book, and leaves the delivery face beside it.
//
// - A committed answer beside no delivery face leaves the paired results awaited: the pairing a segment takes before it closes never answers consumed by itself.
// - The capture is sealed before the waiters wake; an explicit drive seals when the face that submitted it returns.
func (c *Consumer) settleSegment(ctx context.Context, seg *segment, answer SegmentAnswer, committed bool) {
	if committed && c.deliver != nil {
		c.deliver(ctx, c.addr, answer)
		for _, taskID := range seg.paired {
			c.awaited.Store(taskID, TaskConsumed)
		}
	} else if !committed {
		for _, taskID := range seg.paired {
			c.awaited.Store(taskID, TaskOrphaned)
		}
	}
	for taskID := range seg.pending {
		c.awaited.Store(taskID, TaskOrphaned)
	}
	c.mu.Lock()
	c.flying = nil
	c.indexAnswerLocked(answer)
	if c.explicit {
		closed := answer
		c.closed = &closed
	}
	notify := c.notify
	c.notify = make(chan struct{})
	c.mu.Unlock()
	if !seg.explicit {
		c.loop.sealCapture(ctx)
	}
	close(notify)
	c.loop.retireIntake(seg.inv.InvocationID)
	c.noteProgress()
}

// indexAnswerLocked records one closed answer by the key of every input its segment
// consumed and evicts the answers that no wait may read any more: the window keeps
// the most recent answerWindow answers the chain can rebuild whole, and an answer
// older than answerTTL is dropped by the read that finds it. An answer no output
// fact records is the only copy of its result, so neither bound drops it: evicting
// it would leave its wait with no answer at all, and only an explicit cleanup
// outside these bounds may still forget it.
func (c *Consumer) indexAnswerLocked(answer SegmentAnswer) {
	closed := &closedAnswer{answer: answer, at: time.Now()}
	for _, key := range answer.Consumed {
		if key == "" {
			continue
		}
		c.answers[key] = closed
	}
	c.order = append(c.order, closed)
	for len(c.order) > answerWindow {
		at := -1
		for index, held := range c.order {
			if c.evictableAnswerLocked(held) {
				at = index
				break
			}
		}
		if at < 0 {
			break
		}
		c.forgetAnswerLocked(c.order[at])
	}
}

// evictableAnswerLocked reports whether the window or the ttl may forget one indexed
// answer: an answer an output fact on the chain records as consuming its inputs is
// rebuildable acceleration, while an answer no fact records is the only copy of its
// result and neither bound may drop it.
func (c *Consumer) evictableAnswerLocked(closed *closedAnswer) bool {
	_, _, rebuildable := c.outputFactOf(closed.answer.Consumed)
	return rebuildable
}

// forgetAnswerLocked drops one retained answer from the index and from the order the
// window evicts by. A key that a later segment consumed keeps the answer of that
// later segment: only the keys that still name this answer are removed.
func (c *Consumer) forgetAnswerLocked(closed *closedAnswer) {
	for _, key := range closed.answer.Consumed {
		if held, ok := c.answers[key]; ok && held == closed {
			delete(c.answers, key)
		}
	}
	for at, held := range c.order {
		if held == closed {
			c.order = append(c.order[:at], c.order[at+1:]...)
			break
		}
	}
}

// harvest pairs the results that arrived for the calls this segment admitted:
// each settled delivery the intake parked becomes the observation of its call,
// observed through the pipeline of the decision that admitted it. The harvest
// never waits: a call whose result has not arrived stays pending, and the loop
// returns to the pull beside it. The tasks it paired stay awaited in the book
// until this segment delivers, so a result that was paired but never delivered
// is never read as consumed.
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
		s.paired = append(s.paired, taskID)
		paired = append(paired, pairing{call: call, item: item})
	}
	in.mu.Unlock()
	var out []model.Message
	for _, pair := range paired {
		if pair.call.batch {
			s.whole--
		}
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
// under, the keys of the inputs the segment consumed, so a face that
// delivers the answer knows which entries that segment answered, and the body
// its last decision produced, so the entry reads the answer of the segment it
// waited on rather than whatever the chain ended at meanwhile.
type SegmentAnswer struct {
	Turn     Turn
	Err      error
	Trigger  string
	Consumed []event.EventID
	// Body is the assistant text the closing decision of this segment produced,
	// or none beside a segment that produced no text or committed no turn.
	Body string
}

// ErrSegmentCancelled reports a wait that the end of the consumption loop answers
// instead of a segment: the loop that would have consumed the named inputs stopped,
// so the inputs stay owed on the chain for a restart and the entry reads the named
// cancellation instead of blocking on a driver that already returned.
var ErrSegmentCancelled = errors.New("agent: the consumption loop ended before it answered the segment")

// WaitAnswer waits for the segment that consumed any of the named input keys
// and answers its aggregated turn. An input the flying segment already took
// answers when that segment closes; an input no segment took yet answers when
// the next pull drives one. An input whose segment closed before this wait
// began is found by its own key, so a late entry reads the answer of the
// segment that consumed it rather than the answer of a segment that closed
// beside it. A wait the end of the loop meets answers ErrSegmentCancelled.
//
// - The answer index and the notify channel are swapped under one lock, so an atomic miss waits on the channel the next settle closes.
func (c *Consumer) WaitAnswer(ctx context.Context, keys []event.EventID) (SegmentAnswer, error) {
	if c == nil {
		return SegmentAnswer{}, ErrSegmentCancelled
	}
	for {
		c.mu.Lock()
		cached, held := c.cachedAnswerLocked(keys)
		notify := c.notify
		c.mu.Unlock()
		if held {
			return cached, cached.Err
		}
		if answer, ok := c.answerOfChain(keys); ok {
			return answer, answer.Err
		}
		select {
		case <-notify:
		case <-c.done:
			if answer, ok := c.answerFor(keys); ok {
				return answer, answer.Err
			}
			return SegmentAnswer{}, ErrSegmentCancelled
		case <-ctx.Done():
			return SegmentAnswer{}, ctx.Err()
		}
	}
}

// answerFor reports the closed answer of the newest segment that consumed any of the
// named keys, and whether one stands readable at all: a rebuildable answer past
// answerTTL names a segment no entry may still be waiting on, and is dropped by the
// read that finds it rather than answered, while an answer the chain cannot rebuild
// stays readable. An answer the process does not index is read back from the
// output fact that closed its segment, so the cache only accelerates a wait the chain
// can satisfy and the only copy of an answer no fact records never leaves it.
func (c *Consumer) answerFor(keys []event.EventID) (SegmentAnswer, bool) {
	c.mu.Lock()
	cached, held := c.cachedAnswerLocked(keys)
	c.mu.Unlock()
	if held {
		return cached, true
	}
	return c.answerOfChain(keys)
}

// cachedAnswerLocked reports the newest retained answer of the named keys, dropping
// the rebuildable answers past answerTTL that the read meets: the window and the ttl
// bound the acceleration of the answers the chain can rebuild, and an answer they
// drop stays readable on the chain. An answer no fact records stays readable here,
// because there is no chain copy to fall back to.
func (c *Consumer) cachedAnswerLocked(keys []event.EventID) (SegmentAnswer, bool) {
	var newest *closedAnswer
	for _, key := range keys {
		closed, held := c.answers[key]
		if !held {
			continue
		}
		if time.Since(closed.at) > answerTTL && c.evictableAnswerLocked(closed) {
			c.forgetAnswerLocked(closed)
			continue
		}
		if newest == nil || closed.at.After(newest.at) {
			newest = closed
		}
	}
	if newest == nil {
		return SegmentAnswer{}, false
	}
	return newest.answer, true
}

// answerOfChain rebuilds the answer of the closed segment that consumed one of the
// named keys from the output fact that closed it. The output fact records the input
// keys its segment actually consumed beside the range identity, the body it
// committed and the stable fields of the turn it closed, so a wait that starts after
// the process dropped its answer still reads the answer of its own segment instead of
// waiting on a cache that does not hold it. The relation is read, never guessed: an
// output whose metadata is missing, empty or unparseable names no input and answers
// no key, and a turn field the event does not carry stays at its zero value — the
// terminal unknown and the count zero — rather than defaulting to a completed word or
// inventing decisions the fact never recorded. The read adds no fact, starts no
// segment and leaves the cache as it stands.
func (c *Consumer) answerOfChain(keys []event.EventID) (SegmentAnswer, bool) {
	matched, consumed, rebuildable := c.outputFactOf(keys)
	if !rebuildable {
		return SegmentAnswer{}, false
	}
	return SegmentAnswer{
		Turn:     turnOfOutput(matched),
		Trigger:  matched.Meta[outputMetaTrigger],
		Consumed: consumed,
		Body:     matched.Message.Content,
	}, true
}

// turnOfOutput rebuilds the stable turn one agent_output fact records: the terminal
// the closing decision reduced to, the cause a failed turn reported, the closing
// round it ran and the number of decisions it drove. An entry the fact does not carry
// leaves its field at the zero value — KindUnknown and 0 — so an output that carries
// no such entry reads as the unknown turn it recorded instead of a completed one it never named.
func turnOfOutput(evt event.Event) Turn {
	turn := Turn{
		InvocationID: evt.InvocationID,
		Failure:      evt.Meta[outputMetaFailure],
		FinalRound:   FinalRoundReason(evt.Meta[outputMetaFinalRound]),
	}
	switch raw := evt.Meta[outputMetaTerminal]; raw {
	case model.KindCompleted.String():
		turn.Terminal = model.KindCompleted
	case model.KindFailed.String():
		turn.Terminal = model.KindFailed
	case model.KindCancelled.String():
		turn.Terminal = model.KindCancelled
	case model.KindPartialOnly.String():
		turn.Terminal = model.KindPartialOnly
	}
	if raw, held := evt.Meta[outputMetaDecisionCount]; held {
		if decisions, err := strconv.Atoi(raw); err == nil {
			turn.DecisionCount = decisions
		}
	}
	return turn
}

// outputFactOf reports the newest output fact whose recorded inputs include any of
// the named keys, beside the ordered keys it records. The relation is read, never
// guessed: an output whose metadata is missing, empty or unparseable names no input,
// and an output beside no invocation names no turn, so neither is an answer a key
// can rebuild.
func (c *Consumer) outputFactOf(keys []event.EventID) (event.Event, []event.EventID, bool) {
	wanted := make(map[event.EventID]bool, len(keys))
	for _, key := range keys {
		if key != "" {
			wanted[key] = true
		}
	}
	if len(wanted) == 0 {
		return event.Event{}, nil, false
	}
	var matched event.Event
	var consumed []event.EventID
	for _, evt := range c.loop.sessionFacts(c.addr) {
		if evt.Type != event.TypeAgentOutput || evt.InvocationID == "" {
			continue
		}
		inputs, held := outputInputsOf(evt)
		if !held || !anyInputIn(inputs, wanted) {
			continue
		}
		matched = evt
		consumed = inputs
	}
	if matched.InvocationID == "" {
		return event.Event{}, nil, false
	}
	return matched, consumed, true
}

// outputInputsOf reports the input fact identities one agent_output records as the
// ones its segment consumed: the relation the commit wrote beside the output, read
// back as the ordered keys it stands for. A missing, empty or unparseable entry is
// not a relation at all, so it answers no key rather than every key.
func outputInputsOf(evt event.Event) ([]event.EventID, bool) {
	raw, held := evt.Meta[event.MetaKeyInputEvents]
	if !held {
		return nil, false
	}
	var inputs []event.EventID
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil, false
	}
	kept := make([]event.EventID, 0, len(inputs))
	seen := make(map[event.EventID]bool, len(inputs))
	for _, key := range inputs {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, key)
	}
	if len(kept) == 0 {
		return nil, false
	}
	return kept, true
}

// anyInputIn reports whether one output's consumed keys include any key a wait names.
func anyInputIn(consumed []event.EventID, wanted map[event.EventID]bool) bool {
	for _, key := range consumed {
		if wanted[key] {
			return true
		}
	}
	return false
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
	// stopping closes once, exactly when stopped is first set: it is the signal
	// every mounted loop parks beside, so a set marked from the outside ends
	// the loops it already holds without cancelling anything they fly.
	stopping chan struct{}
	// progress is the pulse every loop of this set reports its movement
	// through; nil reports nothing.
	progress func()
	// idleTTL is how long a resting address with no unsettled delivery keeps its
	// instance before the idle reaper reclaims it; zero uses DefaultIdleAddressTTL
	// and a negative value keeps every instance for the life of the set.
	idleTTL time.Duration
	// idleSince records, per address, the first sweep that found the instance
	// resting: the TTL is counted from there, so a single sweep never reclaims a
	// loop that only paused.
	idleSince map[event.Address]time.Time
	// deliveryPending reports an address whose delivery account still owes a
	// target. The assembly registers the predicate; a nil predicate names no
	// unsettled delivery anywhere. The reaper reads it outside the set lock, so a
	// predicate that reads the run's ledger never nests under this one.
	deliveryPending func(event.Address) bool
	// recycled counts the idle instances this set reclaimed, so a reader can
	// verify the reclamation without scanning the log.
	recycled uint64
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
	cs := &Consumers{
		loop:      loop,
		base:      base,
		deliver:   deliver,
		live:      map[event.Address]*Consumer{},
		idleSince: map[event.Address]time.Time{},
		idleTTL:   defaultIdleAddressTTL,
		stopping:  make(chan struct{}),
	}
	go cs.reapIdle()
	return cs
}

// Ensure answers the resident instance of one address, assembling and starting
// it on first use. The address is the one the material itself names, so an
// instance mounts for every complete agent address the run drives, never for the
// entry agent alone.
//
// - Exactly one instance per address: the instance belongs to the loop, not to the
// set. A second Ensure of an address — from this set or from any other set mounted
// over the same loop — answers the instance the loop already carries, so one address
// never runs two drivers of its own decisions, however many sets were mounted.
// - The lifecycle is the run's: an instance lives as long as the context the set was built under, and a set that stopped mounts none, so a call arriving beside a drained run answers no instance instead of starting a driver beside the drain.
// - The loop resumes behind the last agent_output the chain carries — the inputs past it are the material a restart still owes — and a chain beside no agent_output at all resumes from its head, so an unanswered input stays owed instead of vanishing behind a restart.
func (cs *Consumers) Ensure(addr event.Address) *Consumer {
	if cs == nil || !addr.IsComplete() {
		return nil
	}
	return cs.loop.instanceFor(addr, cs, false)
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

// mountLocked assembles and starts the instance one address of one loop runs on, and
// answers none beside a set that stopped. The caller holds the registry lock of the
// loop, so the instance it answers is the one the loop records for that address; the
// cursor is the chain position the caller read outside that lock. A quiet mount marks
// a fresh instance for a delegated batch, whose first read waits for the wake that
// follows the whole batch (see Consumer.run).
//
// - The boot pass is queued before the instance is visible, so a close in that window reads a writer that owes a read.
func (cs *Consumers) mountLocked(l *Loop, addr event.Address, cursor event.EventID, quiet bool) *Consumer {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.stopped {
		return nil
	}
	if consumer, held := cs.live[addr]; held {
		return consumer
	}
	consumer := newConsumer(addr, l, cs.deliver)
	consumer.set = cs
	consumer.quiet = quiet
	consumer.turnContext = cs.turnContext
	consumer.progress = cs.progress
	consumer.cursor = cursor
	consumer.queuePass()
	ctx, cancel := context.WithCancel(cs.base)
	consumer.cancel = cancel
	cs.live[addr] = consumer
	go consumer.run(ctx)
	return consumer
}

// forget drops one ended instance from this set's live view, so a set that stopped
// answers resting instead of naming a driver that already returned. A set beside nil
// drops nothing.
func (cs *Consumers) forget(consumer *Consumer) {
	if cs == nil || consumer == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if held, ok := cs.live[consumer.addr]; ok && held == consumer {
		delete(cs.live, consumer.addr)
	}
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
//
// A fresh instance mounts quiet: the caller commits the delegated input batch after the
// mount and wakes the site once the batch is whole, so the mounted loop reads the batch
// as one material instead of meeting its first fact in a read of its own.
func (cs *Consumers) mount(addr event.Address, bound *state.AgentBinding) *Consumer {
	if cs == nil || !addr.IsComplete() {
		return nil
	}
	consumer := cs.loop.instanceFor(addr, cs, true)
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

// wakeFact wakes the live consumer of one address and names the fact whose
// commit asks for the wake, so the pull of that address reads a fact no position
// of its chain would open a segment on. An address beside no live consumer wakes
// nothing: a settled result never starts a driver the run did not already hold.
// A result the flying segment still awaits, or one already consumed, is dropped
// by the pull that meets it as material — the harvest of that segment pairs the
// first, and the second drives nothing.
func (cs *Consumers) wakeFact(addr event.Address, key event.EventID) {
	if cs == nil || !addr.IsComplete() {
		return
	}
	cs.mu.Lock()
	consumer := cs.live[addr]
	cs.mu.Unlock()
	if consumer == nil {
		return
	}
	if key != "" {
		consumer.Drive(key)
	}
	consumer.Wake()
}

// Has reports whether one address carries a resident consumption loop.
func (cs *Consumers) Has(addr event.Address) bool {
	if cs == nil || cs.loop == nil {
		return false
	}
	return cs.loop.drivesAddress(addr)
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

// Stop ends every consumption loop this set owns and answers only once each of
// them returned: the loops answer their done channels, the segment a loop still
// flew is closed by that loop — the answer of it reached to the entries waiting on
// it and its calls read as orphaned — and no further instance mounts for any
// address. The segments the ended loops left flying stay on the chain for a restart,
// and the instances they mounted leave the registry of their loop once they really
// ended, so the address that mounts next is a new driver instead of the dead one this
// set stopped behind.
//
// The join is the stop itself, not a courtesy beside it: a loop is the writer of
// its own address, so a set that returned while a loop still unwound a segment
// would answer a stopped set over writes that have not landed yet, and a close
// that read that answer would leave those writes behind it. The wait is
// unbounded for the same reason the settle driver's stop is: the cancellation is
// the request, and only the loop's own return is the confirmation. This hard stop
// belongs to the teardown of a run whose close already judged it; the close
// itself marks the set and awaits it inside its own budget through
// StopResidentsWithin instead.
func (cs *Consumers) Stop() {
	if cs == nil {
		return
	}
	cs.markStopped()
	cs.mu.Lock()
	loops := make([]*Consumer, 0, len(cs.live))
	for _, consumer := range cs.live {
		loops = append(loops, consumer)
		if consumer.cancel != nil {
			consumer.cancel()
		}
	}
	cs.mu.Unlock()
	for _, consumer := range loops {
		<-consumer.done
	}
	cs.loop.forgetInstancesOf(cs)
}

// markStopped closes the terminal marker of this set once: no further instance
// mounts for any address, and every loop the set already holds ends its own life
// once the work it holds really ends. Marking cancels nothing — a segment in
// flight keeps its context, so the obligation it drives is finished for real or
// stays open on the chain, never answered by a cancellation the stop invented —
// which is why a close marks first and joins inside its own budget.
func (cs *Consumers) markStopped() {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.stopped {
		return
	}
	cs.stopped = true
	close(cs.stopping)
}

// halted reports whether the terminal marker of this set already closed.
func (cs *Consumers) halted() bool {
	if cs == nil {
		return false
	}
	select {
	case <-cs.stopping:
		return true
	default:
		return false
	}
}

// haltSignal answers the channel that closes once this set is marked stopped,
// so a loop parking between its passes observes the mark as an event instead of
// polling it. A set beside none answers a channel that never closes.
func (cs *Consumers) haltSignal() <-chan struct{} {
	if cs == nil {
		return make(chan struct{})
	}
	return cs.stopping
}

// awaitStopped waits until every loop this set mounted confirmed its own exit,
// or the named deadline arrived. When the deadline ends beside loops that
// still fly, each of them is judged by what it holds: a loop inside a model
// production is left exactly as it is — its resources stay owned, its
// production stays named by the writer ledger of the run, and a later drive
// finishes it for real — and its address is the name the answer reports; a
// loop that stands between turns, waiting on a settlement no close may answer
// for it, is released by cancelling its context, so its own loop closes the
// segment it left flying and answers the entries waiting on it with the named
// cancellation — never a fact nobody produced and never a waiter left on a
// driver that already returned. A zero deadline waits without one and cancels
// nothing.
func (cs *Consumers) awaitStopped(deadline time.Time) []string {
	if cs == nil {
		return nil
	}
	cs.mu.Lock()
	loops := make([]*Consumer, 0, len(cs.live))
	for _, consumer := range cs.live {
		loops = append(loops, consumer)
	}
	cs.mu.Unlock()
	if len(loops) == 0 {
		return nil
	}
	var budget <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		budget = timer.C
	}
	flying := make([]string, 0, len(loops))
	for at, consumer := range loops {
		if budget == nil {
			<-consumer.done
			continue
		}
		select {
		case <-consumer.done:
		case <-budget:
			for _, rest := range loops[at:] {
				if cs.loop != nil {
					if _, _, inModel := cs.loop.InFlightModel(rest.addr); inModel {
						flying = append(flying, "consumer:"+string(rest.addr.AgentID)+"/"+string(rest.addr.SessionID))
						continue
					}
				}
				cs.mu.Lock()
				cancel := rest.cancel
				cs.mu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
			return flying
		}
	}
	return flying
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

// writers is the ledger of the writers one run started: the whole drive of one
// invocation, the model stream of each of its decisions, and the tool production
// of each admitted call. The loop builds it for itself and the close reads it
// through the loop, so it never becomes a second public mechanism beside the
// productions it names: a run writes nothing more only once every writer
// confirmed its stop, and the ones that did not are named in the report beside
// the work they were running. A writer is not a task: the model stream in flight
// holds no task identity at all, so the ledger names the production itself rather
// than minting an identity the task account would have to carry, and the stop is
// confirmed by the production alone.
//
// The redline list (3 rules):
// MUST register a writer when its work really starts and confirm its stop only when that work really ended.
// MUST NOT read a cancelled request, an emptied queue or an elapsed budget as a stop.
// MUST NOT report a writer stopped for one whose confirmation still answers false.
type writers struct {
	mu   sync.Mutex
	seq  int
	live map[int]writer
}

// newWriters returns the empty writer ledger one loop builds for itself.
func newWriters() *writers {
	return &writers{live: map[int]writer{}}
}

// Begin registers one writer that is starting now and answers the confirmation
// that closes its registration. The confirmation is idempotent and safe to call
// from a defer: a second call reports the same stop and drops no other writer.
// A ledger beside nil registers nothing and answers a confirmation that reports
// nothing, so a loop or a run without a ledger keeps running unregistered.
func (w *writers) Begin(name string) func() {
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
func (w *writers) Stopped() (bool, []string) {
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
