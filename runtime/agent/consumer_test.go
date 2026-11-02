// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// residentInstanceEntry is the agent the declaration names as its entry:
	// every other agent of the same declaration owns addresses of its own.
	residentInstanceEntry = "agent-a"
	// residentInstanceOther is the declared agent that is not the entry.
	residentInstanceOther = "agent-b"
	// residentInstanceSession is the session the other agent's address runs in.
	residentInstanceSession = "session-resident"
	// residentInstanceInput is the material a producer lands on the address the
	// instance mounts on.
	residentInstanceInput = "the input a producer lands on a non-entry address"
	// residentInstanceAnswer is the body the model answers the material with.
	residentInstanceAnswer = "the answer the resident instance minted"
	// residentInstanceNotice is the notification material the face that fires an
	// owed attempt lands on the address its entry still owes.
	residentInstanceNotice = "the notice an entry still owes across the restart"
	// residentInstanceQuietWindow bounds how long a check watches an address to
	// report that no driver woke beside it.
	residentInstanceQuietWindow = 500 * time.Millisecond
)

// TestConsumersResidentEveryAddress pins the resident instance of one run.
// - an instance mounts for every agent address its material names, not for the entry agent alone
// - the mounted instance drives the decision of that address from the material that landed before it
// - the segment identity the answer carries is the one its own instance minted, and the answer reaches the delivery face of the address
// - one address carries exactly one instance, however often it is asked for: two instances would drive one input twice
// - the instance ends beside the run: a stopped set answers no instance and the addresses it mounted report their end
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumersResidentEveryAddress(t *testing.T) {
	t.Run("one instance mounts for every agent address its material names", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		if addr.AgentID == rig.entry {
			t.Fatalf("the probed address names the entry agent %q, want another declared agent's own address", rig.entry)
		}
		key := rig.landInput(addr, residentInstanceInput)
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the resident instance of %s/%s = nil, want one instance for every declared agent address", addr.AgentID, addr.SessionID)
		}
		consumer.Wake()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil {
			t.Fatalf("the segment of %s closed beside %v, want a completed turn", addr.SessionID, answer.Err)
		}
		if want := event.InvocationID(string(addr.SessionID) + "#c1"); answer.Turn.InvocationID != want {
			t.Fatalf("the segment of %s answered under %q, want the identity its own instance minted (%q)", addr.SessionID, answer.Turn.InvocationID, want)
		}
		if answer.Turn.Terminal != model.KindCompleted {
			t.Fatalf("the terminal of %s = %v, want %v", addr.SessionID, answer.Turn.Terminal, model.KindCompleted)
		}
		if len(answer.Consumed) != 1 || answer.Consumed[0] != key {
			t.Fatalf("the segment of %s consumed %v, want the material that landed at %q", addr.SessionID, answer.Consumed, key)
		}
		requests := rig.behaviour.requestMessages()
		if len(requests) != 1 {
			t.Fatalf("model requests of the address = %d, want the one decision its instance drove", len(requests))
		}
		if !invariantCarries(requests[0], residentInstanceInput) {
			t.Fatalf("the request of %s carries no material the instance pulled: %+v", addr.SessionID, requests[0])
		}
		outputs := rig.outputs(addr)
		if len(outputs) != 1 {
			t.Fatalf("agent_output facts of %s = %d, want the one the closing segment landed", addr.SessionID, len(outputs))
		}
		if outputs[0].InvocationID != answer.Turn.InvocationID || outputs[0].Message.Content != residentInstanceAnswer {
			t.Fatalf("the landed output of %s = (%q, %q), want the answer under the segment identity its instance minted (%q)",
				addr.SessionID, outputs[0].InvocationID, outputs[0].Message.Content, answer.Turn.InvocationID)
		}
	})

	t.Run("rejects_duplicate_instance_per_address", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		first := rig.consumers.Ensure(addr)
		second := rig.consumers.Ensure(addr)
		if first == nil || second == nil {
			t.Fatal("an address asked for an instance twice answered none, want the mounted instance both times")
		}
		if first != second {
			t.Fatal("the second request of one address answered another instance, want the one already mounted beside it")
		}
		if got := rig.instances(); got != 1 {
			t.Fatalf("instances mounted for one address = %d, want exactly one driver of its decisions", got)
		}
		rig.landInput(addr, residentInstanceInput)
		first.Wake()
		second.Wake()
		rig.awaitAnswer(t, addr)
		if got := len(rig.behaviour.requestMessages()); got != 1 {
			t.Fatalf("model decisions of one address = %d, want the one its single instance drove", got)
		}
		if got := len(rig.outputs(addr)); got != 1 {
			t.Fatalf("agent_output facts of one address = %d, want one closed segment", got)
		}
	})

	t.Run("the instance ends beside the run it belongs to", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the resident instance of %s/%s = nil, want one instance for every declared agent address", addr.AgentID, addr.SessionID)
		}
		rig.consumers.Stop()
		select {
		case <-consumer.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("the instance of %s did not end beside the run that stopped", addr.SessionID)
		}
		if got := rig.consumers.Ensure(addr); got != nil {
			t.Fatalf("a stopped run mounted an instance for %s again, want none beside a drained run", addr.SessionID)
		}
	})
}

// TestConsumersStopJoinsTheWriter pins the stop of one consumption set as a stop over stopped writers.
// - positive: a stop returns only once every loop it cancelled returned, so the segment a loop still unwinds is closed by that loop and not beside the stop
// - negative: a stop never reports itself beside a loop that still runs, because the write a cancelled decision still owes would land after the close returned
// - the write is behind the stop: the turn-end fact a cancelled decision still owes is landed by the loop before the set answers its stop, never after it
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumersStopJoinsTheWriter(t *testing.T) {
	t.Run("a stop returns only beside the loops it cancelled", func(t *testing.T) {
		behaviour := newParkingLoopModel()
		defer behaviour.release()
		rig := newResidentInstanceRigWithModel(t, behaviour)
		addr := rig.otherAddress()
		rig.landInput(addr, residentInstanceInput)
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the resident instance of %s/%s = nil, want one instance to hold a decision", addr.AgentID, addr.SessionID)
		}
		consumer.Wake()
		select {
		case <-behaviour.entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("no model decision of %s/%s started: the material landed on the address drove nothing", addr.AgentID, addr.SessionID)
		}

		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			rig.consumers.Stop()
		}()
		select {
		case <-stopped:
			t.Fatal("the set answered its stop while the decision it flew was still unwinding: the write that decision still owes lands after the stop returned")
		case <-time.After(residentInstanceQuietWindow):
		}

		behaviour.release()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatal("the stop of the set never returned beside the loop it cancelled")
		}
		select {
		case <-consumer.Done():
		default:
			t.Fatal("the set answered its stop beside a loop that still runs, want every loop returned before the stop does")
		}
		if landed := rig.outputs(addr); len(landed) != 1 {
			t.Fatalf("agent_output facts of %s beside the stop = %d, want the one turn-end fact of the cancelled decision landed before the stop answered", addr.SessionID, len(landed))
		}
	})
}

// parkingLoopModel parks every request until its caller gives up and then waits
// for the release the check holds: a loop cancelled mid-decision is observably
// still unwinding the segment it flew, so a stop that does not join the loop is
// read as the stop it is not.
type parkingLoopModel struct {
	// entered signals each request the model took, so a check stops the set only
	// beside a decision that really started.
	entered chan struct{}
	// parked holds every parked request until the check lets it go.
	parked chan struct{}
	once   sync.Once
}

// newParkingLoopModel returns a model that parks every request it is handed.
func newParkingLoopModel() *parkingLoopModel {
	return &parkingLoopModel{entered: make(chan struct{}, 4), parked: make(chan struct{})}
}

// GenerateContent reports the request it took, waits for its caller to give up
// and then for the check's release, and answers the cancellation it ran under.
func (m *parkingLoopModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.entered <- struct{}{}
	<-ctx.Done()
	<-m.parked
	return nil, ctx.Err()
}

// Info reports the model descriptor.
func (m *parkingLoopModel) Info() model.Info { return model.Info{Name: "parking-model"} }

// release lets every parked request go; a second call releases nothing more.
func (m *parkingLoopModel) release() { m.once.Do(func() { close(m.parked) }) }

// TestConsumersResumeTheOwedNotice pins the resume of one notification the settle account still owes across a restart of the process.
// - negative: a boot that opens a chain where the notice stands behind the answer the dead attempt closed on drives no segment by itself
// - positive: the face that fires the attempt names the notification, and the next pull of the address drives a segment on it
// - one name drives one segment, however often the loop is woken beside it, so one notification never answers twice
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumersResumeTheOwedNotice(t *testing.T) {
	t.Run("the boot alone drives no notice a closed segment already carried", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		rig.landInput(addr, residentInstanceInput)
		notice := rig.landNotice(addr, residentInstanceNotice)
		rig.landAnswer(addr, "the answer the dead attempt closed on")
		consumers := rig.restart(t)
		consumer := consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the restarted set mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
		}
		consumer.Wake()
		rig.expectQuiet(t, addr, "the notice standing behind the closed answer drove a segment nobody named")
		if got := len(rig.outputs(addr)); got != 1 {
			t.Fatalf("agent_output facts of %s = %d, want the one the chain already held (notice %q)", addr.SessionID, got, notice)
		}
	})

	t.Run("a named notice drives the next pull of the address", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		rig.landInput(addr, residentInstanceInput)
		notice := rig.landNotice(addr, residentInstanceNotice)
		rig.landAnswer(addr, "the answer the dead attempt closed on")
		consumers := rig.restart(t)
		consumer := consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the restarted set mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
		}
		consumer.Drive(notice)
		consumer.Wake()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil {
			t.Fatalf("the resumed segment of %s closed beside %v, want a completed turn", addr.SessionID, answer.Err)
		}
		if want := event.InvocationID(string(addr.SessionID) + "#c1"); answer.Turn.InvocationID != want {
			t.Fatalf("the resumed attempt of %s answered under %q, want the segment identity %q", addr.SessionID, answer.Turn.InvocationID, want)
		}
		if len(answer.Consumed) != 1 || answer.Consumed[0] != notice {
			t.Fatalf("the resumed segment of %s consumed %v, want the named notice %q", addr.SessionID, answer.Consumed, notice)
		}
		if got := len(rig.behaviour.requestMessages()); got != 1 {
			t.Fatalf("model requests of the resumed attempt = %d, want the one decision its segment drove", got)
		}
		if got := len(rig.outputs(addr)); got != 2 {
			t.Fatalf("agent_output facts of %s = %d, want the closed answer beside the resumed one", addr.SessionID, got)
		}
	})

	t.Run("one name drives one segment", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		rig.landInput(addr, residentInstanceInput)
		notice := rig.landNotice(addr, residentInstanceNotice)
		rig.landAnswer(addr, "the answer the dead attempt closed on")
		consumers := rig.restart(t)
		consumer := consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the restarted set mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
		}
		consumer.Drive(notice)
		consumer.Wake()
		consumer.Wake()
		rig.awaitAnswer(t, addr)
		rig.expectQuiet(t, addr, "a wake beside one name drove a second segment over the same notification")
		if got := len(rig.behaviour.requestMessages()); got != 1 {
			t.Fatalf("model requests of one named notice = %d, want the one attempt the name drives", got)
		}
		if got := len(rig.outputs(addr)); got != 2 {
			t.Fatalf("agent_output facts of %s = %d, want the closed answer beside the resumed one", addr.SessionID, got)
		}
	})
}

// residentInstanceRig is one run's consumption set over a two-agent declaration:
// the entry agent and a declared agent beside it, sharing one session fact chain
// and one model behaviour.
type residentInstanceRig struct {
	entry     event.AgentID
	loop      *Loop
	history   *invariantLoopHistory
	behaviour *invariantLoopModel
	consumers *Consumers
	// answers carries the closed segment of every address, so one check reads the
	// answer of the address it drove and never a neighbour's.
	answers chan SegmentAnswer
	// mu guards the fact keys and the answers taken off the delivery channel.
	mu  sync.Mutex
	seq int
	got []residentInstanceDelivery
}

// residentInstanceDelivery is one closed segment as the delivery face read it.
type residentInstanceDelivery struct {
	addr   event.Address
	answer SegmentAnswer
}

// newResidentInstanceRig assembles the consumption set of one run over two
// declared agents, with the entry agent named first as the document's entry.
func newResidentInstanceRig(t *testing.T) *residentInstanceRig {
	t.Helper()
	behaviour := newInvariantLoopModel(residentInstanceAnswer, nil)
	rig := newResidentInstanceRigWithModel(t, behaviour)
	rig.behaviour = behaviour
	return rig
}

// newResidentInstanceRigWithModel assembles the same run over the model a check
// hands it, so a check can hold one decision of the run in flight.
func newResidentInstanceRigWithModel(t *testing.T, behaviour model.Model) *residentInstanceRig {
	t.Helper()
	declaration := &config.Declaration{
		Entry:  residentInstanceEntry,
		Agents: []config.AgentSpec{{ID: residentInstanceEntry}, {ID: residentInstanceOther}},
	}
	plan, err := state.NewStatePlan(
		state.OrgPlan{Declaration: declaration, Fingerprint: "fp-resident"},
		map[event.AgentID]state.AgentBinding{
			residentInstanceEntry: {Model: behaviour, PromptSource: state.PromptSource{Version: "v1"}},
			residentInstanceOther: {Model: behaviour, PromptSource: state.PromptSource{Version: "v1"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("build the binding plan of the resident run: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the binding generation of the resident run: %v", err)
	}
	history := newInvariantLoopHistory()
	loop := NewLoop(bindings, state.NewOccupancy(), "instance-resident", nil)
	loop.SetHistory(history)
	rig := &residentInstanceRig{
		entry:   residentInstanceEntry,
		loop:    loop,
		history: history,
		answers: make(chan SegmentAnswer, 8),
	}
	loop.SetFactCommit(rig.commit)
	rig.consumers = NewConsumers(loop, context.Background(), rig.deliver)
	t.Cleanup(rig.consumers.Stop)
	return rig
}

// otherAddress reports the address of the declared agent that is not the entry.
func (r *residentInstanceRig) otherAddress() event.Address {
	return event.Address{AgentID: residentInstanceOther, SessionID: residentInstanceSession}
}

// commit lands one fact the loop produced in the chain of its own session and
// answers the identity it landed under.
func (r *residentInstanceRig) commit(evt event.Event) (event.EventID, error) {
	key := r.mint(evt.Owner)
	evt.Key = key
	r.history.land(evt)
	return key, nil
}

// landInput commits one external input on the named address the way a producer
// outside the instance lands material before the instance mounts.
func (r *residentInstanceRig) landInput(addr event.Address, content string) event.EventID {
	key := r.mint(addr)
	r.history.land(event.Event{
		Key:       key,
		Type:      event.TypeExternalInput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleUser, Content: content},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
	})
	return key
}

// landNotice commits one notification material on the named address, the way the
// face that fires an owed attempt lands it: the trigger stamp makes it a driving
// fact of the loop.
func (r *residentInstanceRig) landNotice(addr event.Address, content string) event.EventID {
	key := r.mint(addr)
	r.history.land(event.Event{
		Key:       key,
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleUser, Content: content},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
		Meta:      map[string]string{event.MetaKeyTriggerSource: "task"},
	})
	return key
}

// landAnswer commits one agent_output of the named address, the way a segment
// that closed leaves the boundary the next pull reads.
func (r *residentInstanceRig) landAnswer(addr event.Address, content string) event.EventID {
	key := r.mint(addr)
	r.history.land(event.Event{
		Key:          key,
		Type:         event.TypeAgentOutput,
		Timestamp:    time.Now(),
		Message:      model.Message{Role: model.RoleAssistant, Content: content},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: event.InvocationID(string(addr.SessionID) + "#c1"),
	})
	return key
}

// restart mounts a fresh consumption set over the same run, the way a process
// that opens the chain again does: nothing of the ended attempt's read state
// survives it.
func (r *residentInstanceRig) restart(t *testing.T) *Consumers {
	t.Helper()
	set := NewConsumers(r.loop, context.Background(), r.deliver)
	t.Cleanup(set.Stop)
	return set
}

// expectQuiet reports that one address drives nothing further: no segment closes
// beside it and no model request left it inside the window, so a driver that
// should have stayed idle answers the named gap instead of a silent pass.
func (r *residentInstanceRig) expectQuiet(t *testing.T, addr event.Address, gap string) {
	t.Helper()
	before := len(r.behaviour.requestMessages())
	select {
	case answer := <-r.answers:
		t.Fatalf("%s: the segment of %s closed beside %q", gap, addr.SessionID, answer.Turn.InvocationID)
	case <-time.After(residentInstanceQuietWindow):
	}
	if after := len(r.behaviour.requestMessages()); after != before {
		t.Fatalf("%s: model requests of %s = %d, want the %d that stood before the wake", gap, addr.SessionID, after, before)
	}
}

// mint answers one fresh fact identity of the named address.
func (r *residentInstanceRig) mint(addr event.Address) event.EventID {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return event.EventID(fmt.Sprintf("%s#fact-%d", addr.SessionID, r.seq))
}

// deliver records the closed segment of one address beside the answers a check
// reads off the channel.
func (r *residentInstanceRig) deliver(_ context.Context, addr event.Address, answer SegmentAnswer) {
	r.mu.Lock()
	r.got = append(r.got, residentInstanceDelivery{addr: addr, answer: answer})
	r.mu.Unlock()
	r.answers <- answer
}

// awaitAnswer waits for the closed segment of the named address.
func (r *residentInstanceRig) awaitAnswer(t *testing.T, addr event.Address) SegmentAnswer {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case answer := <-r.answers:
			return answer
		case <-deadline:
			t.Fatalf("the instance of %s/%s closed no segment: the material that landed on the address drove nothing",
				addr.AgentID, addr.SessionID)
			return SegmentAnswer{}
		}
	}
}

// outputs reports the agent_output facts one address committed, in commit order.
func (r *residentInstanceRig) outputs(addr event.Address) []event.Event {
	var out []event.Event
	for _, evt := range r.history.SessionFacts(addr) {
		if evt.Type == event.TypeAgentOutput {
			out = append(out, evt)
		}
	}
	return out
}

// instances reports how many instances the set mounted across every address.
func (r *residentInstanceRig) instances() int {
	r.consumers.mu.Lock()
	defer r.consumers.mu.Unlock()
	return len(r.consumers.live)
}

const (
	// receiptEntryAgent is the agent the receipt checks drive.
	receiptEntryAgent = "agent-a"
	// receiptSession is the session its address runs in.
	receiptSession = "session-receipt"
	// receiptAdmitName is the native tool that admits one call as a task.
	receiptAdmitName = "admit"
	// receiptCallID is the native call that admits the task.
	receiptCallID = "call-admit"
	// receiptTask is the task the admission answers with, and therefore the
	// identity the settle notice names.
	receiptTask = event.TaskID("task-receipt")
	// receiptInput is the material the check lands on the address.
	receiptInput = "run the admitted call"
	// receiptAnswer is the body the segment answers the settled result with.
	receiptAnswer = "the answer the waiting entry is closed on"
)

// TestConsumerWritesConsumptionBesideDelivery pins when one settled result is read as consumed.
// - negative: a segment that paired the settled result of a task and has not delivered yet leaves the task awaited, so nothing may write the receipt of its entry
// - negative: the delivery face reads the task awaited at the moment it is handed the answer, so the receipt never precedes the delivery that closes it
// - positive: the consumption is written beside that delivery, and the entry the notice drove is closed on a turn that really delivered
// - the paired result is the observation of its call in the next decision, so the pairing the check holds is the one the segment really took
//
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func TestConsumerWritesConsumptionBesideDelivery(t *testing.T) {
	t.Run("notice_receipt_waits_for_segment_delivery", func(t *testing.T) {
		behaviour := newReceiptScript(
			receiptStep{calls: []model.ToolCall{receiptCall(receiptCallID, receiptAdmitName)}},
			receiptStep{park: true, answer: receiptAnswer},
		)
		admit := &receiptAdmit{taskID: receiptTask, admitted: make(chan struct{}, 1)}
		rig := newReceiptRig(t, behaviour, []trpctool.Tool{admit})
		addr := receiptAddress()
		rig.landInput(addr, receiptInput)
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the instance of %s/%s = nil, want one instance to drive the material", addr.AgentID, addr.SessionID)
		}
		rig.read(consumer)
		consumer.Wake()
		admit.awaitAdmission(t)

		if read := consumer.TaskState(receiptTask); read != TaskAwaited {
			t.Fatalf("the admitted task reads %q beside the flying segment, want %q: no settled result stands there yet",
				read, TaskAwaited)
		}

		rig.loop.Backflow(event.InvocationID(string(addr.SessionID)+"#c1"), task.Notice{
			TaskID: receiptTask,
			Kind:   task.NoticeSettled,
			Event:  event.EventID("fact-receipt"),
		})
		consumer.Wake()
		behaviour.awaitRound(t, 2, "the decision the paired result drove")

		paired := behaviour.requestRound(1)
		if !receiptObservation(paired, receiptCallID) {
			t.Fatalf("the decision after the settled result carries no observation of %q: %+v", receiptCallID, paired)
		}
		if read := consumer.TaskState(receiptTask); read != TaskAwaited {
			t.Fatalf("the settled result of %s reads %q beside the segment that paired it and has not delivered, want %q",
				receiptTask, read, TaskAwaited)
		}
		rig.expectNoAnswer(t, "the entry was closed beside a segment that had not delivered")

		behaviour.release()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil || answer.Turn.Terminal != model.KindCompleted {
			t.Fatalf("the segment of %s closed = (%+v, %v), want one completed turn", addr.SessionID, answer, answer.Err)
		}
		if at := rig.stateAtDelivery(); at != TaskAwaited {
			t.Fatalf("the task read %q beside the delivery face, want %q: a receipt written before the answer left closes an entry nobody read",
				at, TaskAwaited)
		}
		rig.awaitState(t, consumer, TaskConsumed)
	})
}

// receiptObservation reports whether one request carries the observation of the
// named native call, which is the result its pairing produced.
func receiptObservation(messages []model.Message, callID string) bool {
	for _, message := range messages {
		if message.Role == model.RoleTool && message.ToolID == callID {
			return true
		}
	}
	return false
}

// receiptStep is one scripted answer of the receipt model: the tool calls or the
// body it streams, and whether it parks until the check releases it.
type receiptStep struct {
	calls  []model.ToolCall
	answer string
	park   bool
}

// receiptScript is the model of the receipt checks: it hands the scripted steps
// in order, reports every request it took and parks the steps that say so.
type receiptScript struct {
	mu       sync.Mutex
	steps    []receiptStep
	requests [][]model.Message
	arrived  chan struct{}
	gate     chan struct{}
	once     sync.Once
}

// newReceiptScript returns a model answering with the scripted steps in order.
func newReceiptScript(steps ...receiptStep) *receiptScript {
	return &receiptScript{
		steps:   steps,
		arrived: make(chan struct{}, 8),
		gate:    make(chan struct{}),
	}
}

// GenerateContent records the request, parks a parked step until the check
// releases it and streams the answer of its step.
func (m *receiptScript) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, append([]model.Message(nil), req.Messages...))
	step := receiptStep{}
	if len(m.steps) > 0 {
		step = m.steps[0]
		m.steps = m.steps[1:]
	}
	m.mu.Unlock()
	select {
	case m.arrived <- struct{}{}:
	default:
	}
	if step.park {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	message := model.Message{Role: model.RoleAssistant, Content: step.answer}
	if len(step.calls) > 0 {
		message = model.Message{Role: model.RoleAssistant, ToolCalls: step.calls}
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: message}}, Done: true}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *receiptScript) Info() model.Info { return model.Info{Name: "receipt-model"} }

// awaitRound waits until the model took the named number of requests.
func (m *receiptScript) awaitRound(t *testing.T, want int, what string) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		m.mu.Lock()
		got := len(m.requests)
		m.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-m.arrived:
		case <-deadline:
			t.Fatalf("the model took %d requests, want the %d of %s", got, want, what)
		}
	}
}

// requestRound reports the messages of the request at the given zero-based
// position.
func (m *receiptScript) requestRound(index int) []model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= len(m.requests) {
		return nil
	}
	return m.requests[index]
}

// release lets every parked request go on.
func (m *receiptScript) release() { m.once.Do(func() { close(m.gate) }) }

// receiptAdmit admits one native call as the task it names, the way an
// asynchronous tool answers its admission receipt.
type receiptAdmit struct {
	mu       sync.Mutex
	taskID   event.TaskID
	calls    int
	admitted chan struct{}
}

// Declaration returns the declaration of the admitting tool.
func (a *receiptAdmit) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: receiptAdmitName}
}

// Call answers the admission receipt of the call it is handed.
func (a *receiptAdmit) Call(ctx context.Context, _ []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, task.ErrCallFrameMissing
	}
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	select {
	case a.admitted <- struct{}{}:
	default:
	}
	return task.AdmissionReceipt{CallID: frame.Identity.CallID, TaskID: a.taskID, Accepted: true, Kind: task.AdmissionFresh}, nil
}

// awaitAdmission waits until the tool admitted its call.
func (a *receiptAdmit) awaitAdmission(t *testing.T) {
	t.Helper()
	select {
	case <-a.admitted:
	case <-time.After(20 * time.Second):
		t.Fatal("the admitting tool received no call, want the decision to reach its admission")
	}
}

// receiptCall builds one native tool call of a scripted assistant message.
func receiptCall(id, name string) model.ToolCall {
	return model.ToolCall{
		ID:       id,
		Type:     "function",
		Function: model.FunctionDefinitionParam{Name: name, Arguments: []byte(`{}`)},
	}
}

// receiptRig is one run's consumption set over a single declared agent, with the
// task state read beside the delivery face of every closed segment.
type receiptRig struct {
	loop      *Loop
	history   *invariantLoopHistory
	consumers *Consumers
	answers   chan SegmentAnswer
	mu        sync.Mutex
	seq       int
	consumer  *Consumer
	atDeliver TaskState
}

// newReceiptRig assembles the consumption set of one run over the model and the
// native tools a check hands it.
func newReceiptRig(t *testing.T, behaviour model.Model, tools []trpctool.Tool) *receiptRig {
	t.Helper()
	declaration := &config.Declaration{
		Entry:  receiptEntryAgent,
		Agents: []config.AgentSpec{{ID: receiptEntryAgent}},
	}
	plan, err := state.NewStatePlan(
		state.OrgPlan{Declaration: declaration, Fingerprint: "fp-receipt"},
		map[event.AgentID]state.AgentBinding{
			receiptEntryAgent: {Model: behaviour, Tools: tools, PromptSource: state.PromptSource{Version: "v1"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("build the binding plan of the receipt run: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the binding generation of the receipt run: %v", err)
	}
	history := newInvariantLoopHistory()
	loop := NewLoop(bindings, state.NewOccupancy(), "instance-receipt", nil)
	loop.SetHistory(history)
	rig := &receiptRig{loop: loop, history: history, answers: make(chan SegmentAnswer, 8)}
	loop.SetFactCommit(rig.commit)
	rig.consumers = NewConsumers(loop, context.Background(), rig.deliver)
	t.Cleanup(rig.consumers.Stop)
	return rig
}

// read records the instance the delivery face reads the task state of.
func (r *receiptRig) read(consumer *Consumer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consumer = consumer
}

// commit lands one fact the loop produced in the chain of its own session and
// answers the identity it landed under.
func (r *receiptRig) commit(evt event.Event) (event.EventID, error) {
	key := event.EventID(r.mint(evt.Owner))
	evt.Key = key
	r.history.land(evt)
	return key, nil
}

// landInput commits one external input on the named address, the way a producer
// outside the instance lands material before the instance mounts.
func (r *receiptRig) landInput(addr event.Address, content string) event.EventID {
	key := event.EventID(r.mint(addr))
	r.history.land(event.Event{
		Key:       key,
		Type:      event.TypeExternalInput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleUser, Content: content},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
	})
	return key
}

// mint answers one fresh fact identity of the named address.
func (r *receiptRig) mint(addr event.Address) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return fmt.Sprintf("%s#receipt-fact-%d", addr.SessionID, r.seq)
}

// deliver reads the task state beside the delivery face and records the answer.
func (r *receiptRig) deliver(_ context.Context, _ event.Address, answer SegmentAnswer) {
	r.mu.Lock()
	if r.consumer != nil {
		r.atDeliver = r.consumer.TaskState(receiptTask)
	}
	r.mu.Unlock()
	r.answers <- answer
}

// stateAtDelivery reports the task state the delivery face read.
func (r *receiptRig) stateAtDelivery() TaskState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.atDeliver
}

// expectNoAnswer reports that no segment closed inside the quiet window.
func (r *receiptRig) expectNoAnswer(t *testing.T, gap string) {
	t.Helper()
	select {
	case answer := <-r.answers:
		t.Fatalf("%s: the segment of %s closed beside %q", gap, answer.Turn.InvocationID, answer.Turn.Terminal)
	case <-time.After(250 * time.Millisecond):
	}
}

// awaitAnswer waits for the closed segment of the named address.
func (r *receiptRig) awaitAnswer(t *testing.T, addr event.Address) SegmentAnswer {
	t.Helper()
	select {
	case answer := <-r.answers:
		return answer
	case <-time.After(20 * time.Second):
		t.Fatalf("the instance of %s/%s closed no segment", addr.AgentID, addr.SessionID)
		return SegmentAnswer{}
	}
}

// awaitState waits until the named task reads the state the check expects.
func (r *receiptRig) awaitState(t *testing.T, consumer *Consumer, want TaskState) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if got := consumer.TaskState(receiptTask); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the settled result of %s reads %q, want %q beside the delivered answer",
				receiptTask, consumer.TaskState(receiptTask), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// receiptAddress reports the address the receipt run drives.
func receiptAddress() event.Address {
	return event.Address{AgentID: receiptEntryAgent, SessionID: receiptSession}
}
