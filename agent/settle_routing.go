// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

type settleAcceptor func(context.Context, *AgentEvent) (PublishReceipt, error)

type settleWaitSnapshot struct {
	TaskID  string             `json:"task_id"`
	RoundID uint64             `json:"round_id"`
	WaitFor task.WaitCondition `json:"wait_for"`
}

type settleSequenceRange struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

type settleStreamSnapshot struct {
	TaskID        string                `json:"task_id"`
	RoundID       uint64                `json:"round_id"`
	Accepted      []settleSequenceRange `json:"accepted,omitempty"`
	Owner         string                `json:"owner,omitempty"`
	Lifetime      string                `json:"lifetime,omitempty"`
	WaitFor       task.WaitCondition    `json:"wait_for,omitempty"`
	WaitSatisfied bool                  `json:"wait_satisfied,omitempty"`
	Terminal      bool                  `json:"terminal,omitempty"`
}

type settleRoutingSnapshot struct {
	Waits   []settleWaitSnapshot   `json:"waits,omitempty"`
	Streams []settleStreamSnapshot `json:"streams,omitempty"`
}

type settleWaitKey struct {
	taskID  string
	roundID uint64
	waitFor task.WaitCondition
}

type settleProducerKey struct {
	taskID  string
	roundID uint64
}

type settleDeliveryKey struct {
	sourceID string
	taskID   string
	roundID  uint64
	seq      uint64
}

type settleBooking struct {
	inv    string
	key    settleWaitKey
	active bool
}

type settleStream struct {
	accepted      []settleSequenceRange
	owner         string
	lifetime      string
	tk            *task.Task
	waitFor       task.WaitCondition
	waitSatisfied bool
	terminal      bool
}

type settleAttempt struct {
	done     chan struct{}
	accepted bool
}

type settleInvocation struct {
	changed     chan struct{}
	idle        chan struct{}
	destination settleAcceptor
	sealed      bool
	pause       *int
	inFlight    int
	retired     bool
	bookings    map[*settleBooking]struct{}
	waits       map[settleWaitKey]*settleBooking
	streams     map[settleProducerKey]*settleStream
}

type settleSinkRegistry struct {
	mu       sync.Mutex
	byInv    map[string]*EventBus
	pending  map[string]int
	states   map[string]*settleInvocation
	inFlight map[settleDeliveryKey]*settleAttempt
}

func newSettleSinkRegistry() *settleSinkRegistry { return &settleSinkRegistry{} }

func (r *settleSinkRegistry) stateLocked(id string) *settleInvocation {
	if r.states == nil {
		r.states = make(map[string]*settleInvocation)
	}
	s := r.states[id]
	if s == nil {
		s = &settleInvocation{changed: make(chan struct{})}
		r.states[id] = s
	}
	return s
}

func (r *settleSinkRegistry) changedLocked(s *settleInvocation) {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (r *settleSinkRegistry) bind(id string, bus *EventBus) {
	if r == nil || id == "" || bus == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byInv == nil {
		r.byInv = make(map[string]*EventBus)
	}
	s := r.stateLocked(id)
	s.retired = false
	r.byInv[id] = bus
	r.changedLocked(s)
}

func (r *settleSinkRegistry) unbind(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byInv, id)
	if s := r.states[id]; s != nil {
		for b := range s.bookings {
			b.active = false
		}
		s.bookings = nil
		s.waits = nil
		delete(r.pending, id)
		r.changedLocked(s)
	}
	r.compactLocked()
}

func (r *settleSinkRegistry) book(id string, waitFor task.WaitCondition) *settleBooking {
	if r == nil || id == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[id]
	if s == nil || (r.byInv[id] == nil && s.destination == nil) || s.retired {
		return nil
	}
	b := &settleBooking{inv: id, key: settleWaitKey{waitFor: waitFor}, active: true}
	if s.bookings == nil {
		s.bookings = make(map[*settleBooking]struct{})
	}
	s.bookings[b] = struct{}{}
	if r.pending == nil {
		r.pending = make(map[string]int)
	}
	r.pending[id]++
	r.changedLocked(s)
	return b
}

func (r *settleSinkRegistry) bindBooking(b *settleBooking, tk *task.Task) {
	if r == nil || b == nil || tk == nil {
		return
	}
	snapshot := tk.RoutingSnapshot()
	spec := snapshot.Spec
	key := settleWaitKey{taskID: tk.ID, roundID: snapshot.RoundID, waitFor: task.WaitForOf(spec)}
	if key.taskID == "" || key.roundID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[b.inv]
	if !b.active || s == nil {
		return
	}
	if b.key.taskID != "" {
		if b.key.taskID == key.taskID && b.key.roundID == key.roundID && b.key.waitFor != key.waitFor {
			delete(s.waits, b.key)
			b.key = key
			s.waits[key] = b
			stream := r.streamLocked(s, settleProducerKey{taskID: key.taskID, roundID: key.roundID})
			if stream.waitSatisfied && (stream.waitFor == "" || stream.waitFor == key.waitFor) {
				r.voidBookingLocked(b, s)
			}
			r.changedLocked(s)
		}
		return
	}
	b.key = key
	if s.waits == nil {
		s.waits = make(map[settleWaitKey]*settleBooking)
	}
	if prior := s.waits[key]; prior != nil && prior != b {
		r.voidBookingLocked(b, s)
		return
	}
	s.waits[key] = b
	stream := r.streamLocked(s, settleProducerKey{taskID: tk.ID, roundID: key.roundID})
	stream.tk, stream.owner, stream.lifetime = tk, b.inv, task.LifetimeOf(spec)
	stream.waitFor = key.waitFor
	r.changedLocked(s)
}

func (r *settleSinkRegistry) voidBookingLocked(b *settleBooking, s *settleInvocation) {
	if b == nil || !b.active || s == nil {
		return
	}
	b.active = false
	delete(s.bookings, b)
	if s.waits[b.key] == b {
		delete(s.waits, b.key)
	}
	if r.pending[b.inv] > 1 {
		r.pending[b.inv]--
	} else {
		delete(r.pending, b.inv)
	}
	r.changedLocked(s)
}

func (r *settleSinkRegistry) voidBooking(b *settleBooking) {
	if r == nil || b == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.voidBookingLocked(b, r.states[b.inv])
}

func (r *settleSinkRegistry) noteSpawn(id string) { r.book(id, task.WaitExit) }

func (r *settleSinkRegistry) voidSpawn(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.states[id]; s != nil {
		for b := range s.bookings {
			if b.key.taskID == "" {
				r.voidBookingLocked(b, s)
				return
			}
		}
	}
}

func (r *settleSinkRegistry) waitState(id string) (bool, <-chan struct{}) {
	if r == nil || id == "" {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[id]
	if s == nil {
		return false, nil
	}
	return len(s.bookings) > 0, s.changed
}

func (r *settleSinkRegistry) awaiting(id string) bool {
	awaiting, _ := r.waitState(id)
	return awaiting
}

func (r *settleSinkRegistry) setDestination(id string, dst settleAcceptor) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(id)
	s.destination = dst
	r.changedLocked(s)
}

func (r *settleSinkRegistry) pauseDestination(ctx context.Context, id string) (func(settleAcceptor), error) {
	if r == nil || id == "" {
		return func(settleAcceptor) {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var s *settleInvocation
	var token *int
	for {
		r.mu.Lock()
		s = r.stateLocked(id)
		if !s.sealed {
			token = new(int)
			s.sealed, s.pause = true, token
			r.changedLocked(s)
			r.mu.Unlock()
			break
		}
		changed := s.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var once sync.Once
	finish := func(dst settleAcceptor, replace bool) {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if current := r.states[id]; current == s && current.pause == token {
				if replace {
					current.destination = dst
				}
				current.sealed, current.pause = false, nil
				r.changedLocked(current)
			}
		})
	}
	release := func(dst settleAcceptor) { finish(dst, true) }
	for {
		r.mu.Lock()
		if s.inFlight == 0 {
			err := ctx.Err()
			r.mu.Unlock()
			if err != nil {
				finish(nil, false)
				return nil, err
			}
			return release, nil
		}
		idle := s.idle
		r.mu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			finish(nil, false)
			return nil, ctx.Err()
		}
	}
}

func (r *settleSinkRegistry) streamLocked(s *settleInvocation, key settleProducerKey) *settleStream {
	if s.streams == nil {
		s.streams = make(map[settleProducerKey]*settleStream)
	}
	stream := s.streams[key]
	if stream == nil {
		stream = &settleStream{}
		s.streams[key] = stream
	}
	return stream
}

func sequenceAccepted(ranges []settleSequenceRange, seq uint64) bool {
	for _, interval := range ranges {
		if seq < interval.First {
			return false
		}
		if seq <= interval.Last {
			return true
		}
	}
	return false
}

func acceptSequence(ranges []settleSequenceRange, seq uint64) []settleSequenceRange {
	if sequenceAccepted(ranges, seq) {
		return ranges
	}
	ranges = append(ranges, settleSequenceRange{First: seq, Last: seq})
	return mergeSequenceRanges(ranges)
}

func mergeSequenceRanges(ranges []settleSequenceRange) []settleSequenceRange {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].First < ranges[j].First })
	out := ranges[:0]
	for _, interval := range ranges {
		if interval.First == 0 || interval.Last < interval.First {
			continue
		}
		if len(out) == 0 || (out[len(out)-1].Last != ^uint64(0) && interval.First > out[len(out)-1].Last+1) {
			out = append(out, interval)
		} else if interval.Last > out[len(out)-1].Last {
			out[len(out)-1].Last = interval.Last
		}
	}
	return out
}

func settleUint(v any) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case uint:
		return uint64(n)
	case int:
		if n > 0 {
			return uint64(n)
		}
	case int64:
		if n > 0 {
			return uint64(n)
		}
	case float64:
		if n > 0 {
			return uint64(n)
		}
	case json.Number:
		v, _ := strconv.ParseUint(string(n), 10, 64)
		return v
	case string:
		v, _ := strconv.ParseUint(n, 10, 64)
		return v
	}
	return 0
}

func settleBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	s, _ := v.(string)
	return s == "true"
}

func deliveryKey(evt *AgentEvent) settleDeliveryKey {
	key := settleDeliveryKey{sourceID: evt.ID}
	key.taskID, _ = evt.Metadata["task_id"].(string)
	key.roundID = settleUint(evt.Metadata["task_round_id"])
	if producer := settleUint(evt.Metadata["task_producer_round_id"]); producer != 0 {
		key.roundID = producer
	}
	key.seq = settleUint(evt.Metadata["task_signal_seq"])
	return key
}

func settleEventOwner(evt *AgentEvent) (string, bool) {
	owner, present := evt.Metadata["task_round_owner"].(string)
	return owner, present && (owner != "" || settleUint(evt.Metadata["task_round_id"]) != 0)
}

func canonicalDelivery(key settleDeliveryKey) bool {
	return key.taskID != "" && key.roundID != 0 && key.seq != 0 && key.sourceID == task.SignalSourceID(key.taskID, key.roundID, key.seq)
}

func (r *settleSinkRegistry) acceptedLocked(s *settleInvocation, key settleDeliveryKey) bool {
	if s == nil || !canonicalDelivery(key) {
		return false
	}
	stream := s.streams[settleProducerKey{taskID: key.taskID, roundID: key.roundID}]
	return stream != nil && sequenceAccepted(stream.accepted, key.seq)
}

func settleSatisfied(evt *AgentEvent) bool {
	if v, ok := evt.Metadata["settle_wait_satisfied"]; ok {
		return settleBool(v)
	}
	if settleBool(evt.Metadata[tagentevent.MetaKeySettleFinal]) {
		return true
	}
	status, _ := evt.Metadata["settle_status"].(string)
	return status == "completed" || status == "failed" || status == "cancelled" || status == "dead"
}

func settleTerminal(evt *AgentEvent) bool {
	status, _ := evt.Metadata["settle_status"].(string)
	return status == "completed" || status == "failed" || status == "cancelled" || status == "dead"
}

func (r *settleSinkRegistry) accountAcceptedLocked(id string, s *settleInvocation, evt *AgentEvent) {
	key := deliveryKey(evt)
	if canonicalDelivery(key) {
		producer := settleProducerKey{taskID: key.taskID, roundID: key.roundID}
		stream := r.streamLocked(s, producer)
		stream.accepted = acceptSequence(stream.accepted, key.seq)
		if settleSatisfied(evt) {
			stream.waitFor, _ = evt.Metadata["wait_for"].(task.WaitCondition)
			if stream.waitFor == "" {
				waitFor, _ := evt.Metadata["wait_for"].(string)
				stream.waitFor = task.WaitCondition(waitFor)
			}
			stream.waitSatisfied = true
		}
		if settleTerminal(evt) {
			stream.owner = ""
			stream.terminal = true
			if s.retired {
				delete(s.streams, producer)
				if len(s.streams) == 0 {
					s.destination = nil
				}
			}
		}
	}
	if !settleSatisfied(evt) {
		return
	}
	waitFor, _ := evt.Metadata["wait_for"].(string)
	for b := range s.bookings {
		if b.key.taskID == "" {
			if key.taskID == "" || len(s.waits) == 0 {
				r.voidBookingLocked(b, s)
				return
			}
			continue
		}
		if b.key.taskID == key.taskID && b.key.roundID == key.roundID && (waitFor == "" || b.key.waitFor == "" || string(b.key.waitFor) == waitFor) {
			r.voidBookingLocked(b, s)
		}
	}
}

func invokeSettleAcceptor(ctx context.Context, dst settleAcceptor, evt *AgentEvent) (receipt PublishReceipt, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("settle receiver panicked: %v", recovered)
		}
	}()
	return dst(ctx, evt)
}

func (r *settleSinkRegistry) accept(ctx context.Context, id string, evt *AgentEvent, fallback settleAcceptor, tasks ...*task.Task) bool {
	if r == nil || evt == nil {
		return false
	}
	if owner, ok := settleEventOwner(evt); ok && owner != id {
		return false
	}
	key := deliveryKey(evt)
	var tk *task.Task
	var lifetime string
	if len(tasks) > 0 && tasks[0] != nil {
		tk = tasks[0]
		lifetime = task.LifetimeOf(tk.SpecSnapshot())
	}
	for {
		r.mu.Lock()
		s := r.states[id]
		if r.acceptedLocked(s, key) {
			r.mu.Unlock()
			return true
		}
		if prior := r.inFlight[key]; prior != nil {
			done := prior.done
			r.mu.Unlock()
			select {
			case <-done:
				return prior.accepted
			case <-ctx.Done():
				return false
			}
		}
		if s != nil && s.sealed {
			changed := s.changed
			r.mu.Unlock()
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return false
			}
		}
		var dst settleAcceptor
		if s != nil {
			dst = s.destination
		}
		if dst == nil && r.byInv[id] != nil {
			dst = r.byInv[id].PublishContext
		}
		if dst == nil && id == "" {
			dst = fallback
		}
		if dst == nil {
			r.mu.Unlock()
			return false
		}
		if s == nil {
			s = r.stateLocked(id)
		}
		if canonicalDelivery(key) && tk != nil {
			stream := r.streamLocked(s, settleProducerKey{taskID: key.taskID, roundID: key.roundID})
			stream.tk, stream.lifetime = tk, lifetime
			if !stream.terminal {
				stream.owner = id
			}
		}
		if r.inFlight == nil {
			r.inFlight = make(map[settleDeliveryKey]*settleAttempt)
		}
		attempt := &settleAttempt{done: make(chan struct{})}
		r.inFlight[key] = attempt
		if s.inFlight == 0 {
			s.idle = make(chan struct{})
		}
		s.inFlight++
		r.mu.Unlock()

		_, err := invokeSettleAcceptor(ctx, dst, evt)

		r.mu.Lock()
		attempt.accepted = err == nil
		if attempt.accepted {
			r.accountAcceptedLocked(id, s, evt)
		}
		s.inFlight--
		if s.inFlight == 0 {
			close(s.idle)
			s.idle = nil
		}
		close(attempt.done)
		delete(r.inFlight, key)
		r.changedLocked(s)
		r.compactLocked()
		r.mu.Unlock()
		return attempt.accepted
	}
}

func (r *settleSinkRegistry) route(id string, evt *AgentEvent) bool {
	return r.accept(context.Background(), id, evt, nil)
}

func taskInvocationID(tk *task.Task) string {
	if tk == nil {
		return ""
	}
	if owner := tk.RoundOwner(); owner != "" {
		return owner
	}
	return tk.SpecSnapshot().Origin[metaKeyInvocationID]
}

func deliverTaskSettled(sinks *settleSinkRegistry, bus *EventBus, tk *task.Task, evt *AgentEvent) bool {
	if evt == nil {
		return false
	}
	id, explicit := settleEventOwner(evt)
	if !explicit {
		id = taskInvocationID(tk)
	}
	var fallback settleAcceptor
	if bus != nil {
		fallback = bus.PublishContext
	}
	if sinks != nil {
		return sinks.accept(context.Background(), id, evt, fallback, tk)
	}
	if id == "" && fallback != nil {
		_, err := fallback(context.Background(), evt)
		return err == nil
	}
	return false
}

func (r *settleSinkRegistry) snapshotWaits(id string) []settleWaitSnapshot {
	return r.snapshotRouting(id).Waits
}

func (r *settleSinkRegistry) snapshotRouting(id string) settleRoutingSnapshot {
	if r == nil {
		return settleRoutingSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[id]
	if s == nil {
		return settleRoutingSnapshot{}
	}
	var snapshot settleRoutingSnapshot
	for key := range s.waits {
		snapshot.Waits = append(snapshot.Waits, settleWaitSnapshot{TaskID: key.taskID, RoundID: key.roundID, WaitFor: key.waitFor})
	}
	for key, stream := range s.streams {
		snapshot.Streams = append(snapshot.Streams, settleStreamSnapshot{TaskID: key.taskID, RoundID: key.roundID, Accepted: append([]settleSequenceRange(nil), stream.accepted...), Owner: stream.owner, Lifetime: stream.lifetime, WaitFor: stream.waitFor, WaitSatisfied: stream.waitSatisfied, Terminal: stream.terminal})
	}
	sort.Slice(snapshot.Waits, func(i, j int) bool {
		a, b := snapshot.Waits[i], snapshot.Waits[j]
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		if a.RoundID != b.RoundID {
			return a.RoundID < b.RoundID
		}
		return a.WaitFor < b.WaitFor
	})
	sort.Slice(snapshot.Streams, func(i, j int) bool {
		a, b := snapshot.Streams[i], snapshot.Streams[j]
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		return a.RoundID < b.RoundID
	})
	return snapshot
}

func (r *settleSinkRegistry) restoreWaits(id string, waits []settleWaitSnapshot) {
	r.restoreRouting(id, settleRoutingSnapshot{Waits: waits})
}

func (r *settleSinkRegistry) restoreRouting(id string, snapshot settleRoutingSnapshot, controllers ...task.TaskController) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	s := r.stateLocked(id)
	for _, wait := range snapshot.Waits {
		key := settleWaitKey{taskID: wait.TaskID, roundID: wait.RoundID, waitFor: wait.WaitFor}
		if key.taskID == "" || key.roundID == 0 || s.waits[key] != nil {
			continue
		}
		b := &settleBooking{inv: id, key: key, active: true}
		if s.bookings == nil {
			s.bookings = make(map[*settleBooking]struct{})
		}
		if s.waits == nil {
			s.waits = make(map[settleWaitKey]*settleBooking)
		}
		s.bookings[b] = struct{}{}
		s.waits[key] = b
		if r.pending == nil {
			r.pending = make(map[string]int)
		}
		r.pending[id]++
	}
	for _, saved := range snapshot.Streams {
		if saved.TaskID == "" || saved.RoundID == 0 {
			continue
		}
		stream := r.streamLocked(s, settleProducerKey{taskID: saved.TaskID, roundID: saved.RoundID})
		stream.terminal = stream.terminal || saved.Terminal
		if !stream.terminal {
			stream.owner = saved.Owner
		}
		stream.lifetime = saved.Lifetime
		if !stream.waitSatisfied {
			stream.waitFor = saved.WaitFor
		}
		stream.waitSatisfied = stream.waitSatisfied || saved.WaitSatisfied
		for _, interval := range saved.Accepted {
			stream.accepted = append(stream.accepted, interval)
		}
		stream.accepted = mergeSequenceRanges(stream.accepted)
	}
	for key, b := range s.waits {
		stream := s.streams[settleProducerKey{taskID: key.taskID, roundID: key.roundID}]
		if stream != nil && stream.waitSatisfied && (stream.waitFor == "" || stream.waitFor == key.waitFor) {
			r.voidBookingLocked(b, s)
		}
	}
	r.changedLocked(s)
	r.mu.Unlock()
	if len(controllers) > 0 && controllers[0] != nil {
		r.reconcileTasks(id, controllers[0])
	}
}

func (r *settleSinkRegistry) reconcileTasks(id string, controller task.TaskController) {
	if r == nil || id == "" || controller == nil {
		return
	}
	tasks := controller.List()
	for _, tk := range tasks {
		if tk == nil {
			continue
		}
		snapshot := tk.RoutingSnapshot()
		spec := snapshot.Spec
		round, owner := snapshot.ProducerRound, snapshot.ProducerOwner
		if round == 0 {
			round, owner = snapshot.RoundID, snapshot.RoundOwner
		}
		if owner != id || round == 0 {
			continue
		}
		pending, pendingSatisfying := false, false
		for _, sig := range snapshot.PendingSignals {
			if sig.RoundID == round && sig.RoundOwner == id {
				pending = true
				pendingSatisfying = pendingSatisfying || sig.SatisfiesWait()
			}
		}
		status := snapshot.Status
		live, producerSatisfied := status.Live(), snapshot.WaitSatisfied
		r.mu.Lock()
		s := r.stateLocked(id)
		stream := r.streamLocked(s, settleProducerKey{taskID: tk.ID, roundID: round})
		stream.tk, stream.lifetime = tk, task.LifetimeOf(spec)
		if live && !stream.terminal {
			stream.owner = id
		}
		wait := task.WaitForOf(spec)
		if !stream.waitSatisfied {
			stream.waitFor = wait
		}
		key := settleWaitKey{taskID: tk.ID, roundID: round, waitFor: wait}
		satisfied := (stream.waitSatisfied && (stream.waitFor == "" || stream.waitFor == wait)) || (producerSatisfied && !pendingSatisfying)
		if !pending && status == task.TaskAliveDetached && wait == task.WaitReady {
			satisfied = true
		}
		if (live || pending) && !satisfied && s.waits[key] == nil && !s.retired {
			b := &settleBooking{inv: id, key: key, active: true}
			if s.bookings == nil {
				s.bookings = make(map[*settleBooking]struct{})
			}
			if s.waits == nil {
				s.waits = make(map[settleWaitKey]*settleBooking)
			}
			s.bookings[b], s.waits[key] = struct{}{}, b
			if r.pending == nil {
				r.pending = make(map[string]int)
			}
			r.pending[id]++
		}
		if satisfied {
			r.voidBookingLocked(s.waits[key], s)
		}
		r.changedLocked(s)
		r.mu.Unlock()
	}
}

func (r *settleSinkRegistry) reconcileAccepted(id string, events []*AgentEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(id)
	for _, evt := range events {
		if evt == nil || evt.Source != SourceTask {
			continue
		}
		if owner, ok := settleEventOwner(evt); ok && owner != id {
			continue
		}
		r.accountAcceptedLocked(id, s, evt)
	}
	r.changedLocked(s)
}

func (r *settleSinkRegistry) abortRound(tk *task.Task, sig task.SettleSignal) {
	if r == nil || tk == nil || sig.RoundOwner == "" || sig.RoundID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[sig.RoundOwner]
	if s == nil {
		return
	}
	producer := settleProducerKey{taskID: tk.ID, roundID: sig.RoundID}
	stream := r.streamLocked(s, producer)
	stream.owner, stream.waitSatisfied, stream.terminal = "", true, true
	if sig.Seq > 0 && sig.SourceID == task.SignalSourceID(tk.ID, sig.RoundID, sig.Seq) {
		stream.accepted = acceptSequence(stream.accepted, sig.Seq)
	}
	for key, b := range s.waits {
		if key.taskID == tk.ID && key.roundID == sig.RoundID {
			stream.waitFor = key.waitFor
			r.voidBookingLocked(b, s)
		}
	}
	if s.retired {
		delete(s.streams, producer)
		if len(s.streams) == 0 {
			s.destination = nil
		}
	}
	r.changedLocked(s)
	r.compactLocked()
}

func (r *settleSinkRegistry) retireStateOnly(id string) {
	r.retire(id)
}

func (r *settleSinkRegistry) retire(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byInv, id)
	delete(r.pending, id)
	if s := r.states[id]; s != nil {
		for b := range s.bookings {
			b.active = false
		}
		s.bookings, s.waits = nil, nil
		s.retired = true
		for key, stream := range s.streams {
			live := stream.tk == nil
			if stream.tk != nil {
				snapshot := stream.tk.RoutingSnapshot()
				live = snapshot.Status.Live() && snapshot.ProducerRound == key.roundID
				for _, pending := range snapshot.PendingSignals {
					if pending.RoundID == key.roundID && pending.RoundOwner == stream.owner {
						live = true
						break
					}
				}
			}
			if s.destination == nil || stream.owner == "" || !live {
				delete(s.streams, key)
			}
		}
		if len(s.streams) == 0 {
			s.destination = nil
		}
		r.changedLocked(s)
	}
	r.compactLocked()
}

func (r *settleSinkRegistry) compactLocked() {
	for id, s := range r.states {
		if !s.sealed && s.inFlight == 0 && len(s.bookings) == 0 && r.byInv[id] == nil && s.destination == nil && (id != "" || len(s.streams) == 0) {
			r.changedLocked(s)
			delete(r.states, id)
		}
	}
	if len(r.states) == 0 {
		r.states = nil
	}
	if len(r.byInv) == 0 {
		r.byInv = nil
	}
	if len(r.pending) == 0 {
		r.pending = nil
	}
	if len(r.inFlight) == 0 {
		r.inFlight = nil
	}
}

func (r *settleSinkRegistry) compact(controllers ...task.TaskController) {
	if r == nil {
		return
	}
	if len(controllers) > 0 && controllers[0] != nil {
		r.mu.Lock()
		ids := make(map[string]struct{})
		for _, s := range r.states {
			for key := range s.streams {
				ids[key.taskID] = struct{}{}
			}
		}
		r.mu.Unlock()
		missing := make(map[string]bool)
		for id := range ids {
			_, exists := controllers[0].Get(id)
			missing[id] = !exists
		}
		r.mu.Lock()
		for _, s := range r.states {
			for key, stream := range s.streams {
				if missing[key.taskID] && stream.owner == "" {
					delete(s.streams, key)
				}
			}
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.compactLocked()
}

func drainSettleBusTo(inv *EventBus, persistent *EventBus) {
	if inv == nil || persistent == nil {
		return
	}
	for _, ev := range inv.TryPull() {
		persistent.Publish(ev)
	}
}

func (ta *TagentAgent) bindSettleBus(id string, bus *EventBus) {
	if ta != nil {
		ta.settleSinks.bind(id, bus)
	}
}

func (ta *TagentAgent) unbindSettleBus(id string) {
	if ta != nil {
		ta.settleSinks.unbind(id)
	}
}

type countingSpawner struct {
	inner task.TaskSpawner
	sinks *settleSinkRegistry
	id    string
}

var _ task.TaskController = (*countingSpawner)(nil)

func (c *countingSpawner) Spawn(spec task.TaskSpec, detector task.SettleDetector) task.SpawnResult {
	b := c.sinks.book(c.id, task.WaitForOf(spec))
	spec.RoundOwner = c.id
	original := spec.OnRoundStart
	spec.OnRoundStart = func(tk *task.Task) {
		c.sinks.bindBooking(b, tk)
		if original != nil {
			original(tk)
		}
	}
	res := c.inner.Spawn(spec, detector)
	if res.Task != nil {
		c.sinks.bindBooking(b, res.Task)
	}
	if (res.Settled && res.Signal.SatisfiesWait()) || res.Deduped || res.Blocked != "" {
		c.sinks.voidBooking(b)
	}
	return res
}

func (c *countingSpawner) controller() task.TaskController {
	controller, _ := c.inner.(task.TaskController)
	return controller
}

func (c *countingSpawner) List() []*task.Task {
	if controller := c.controller(); controller != nil {
		return controller.List()
	}
	return nil
}

func (c *countingSpawner) Get(id string) (*task.Task, bool) {
	if controller := c.controller(); controller != nil {
		return controller.Get(id)
	}
	return nil, false
}

func (c *countingSpawner) Cancel(id string) bool {
	if controller := c.controller(); controller != nil {
		return controller.Cancel(id)
	}
	return false
}

func (c *countingSpawner) Relaunch(ctx context.Context, id string) (task.SpawnResult, error) {
	if controller := c.controller(); controller != nil {
		ctx = task.WithTaskSpawner(task.WithRoundOwner(ctx, c.id), c)
		return controller.Relaunch(ctx, id)
	}
	return task.SpawnResult{}, fmt.Errorf("task spawner does not support relaunch")
}

func (c *countingSpawner) Resume(ctx context.Context, id string, input string) (task.SpawnResult, error) {
	controller := c.controller()
	if controller == nil {
		return task.SpawnResult{}, fmt.Errorf("task spawner does not support resume")
	}
	var waitFor task.WaitCondition
	if tk, ok := controller.Get(id); ok {
		waitFor = task.WaitForOf(tk.SpecSnapshot())
	}
	b := c.sinks.book(c.id, waitFor)
	ctx = task.WithRoundOwner(ctx, c.id)
	ctx = task.WithRoundStart(ctx, func(tk *task.Task) { c.sinks.bindBooking(b, tk) })
	res, err := controller.Resume(ctx, id, input)
	if res.Task != nil {
		c.sinks.bindBooking(b, res.Task)
	}
	if err != nil || res.Deduped || res.Blocked != "" || (res.Settled && res.Signal.SatisfiesWait()) {
		c.sinks.voidBooking(b)
	}
	return res, err
}

func (c *countingSpawner) RenewTTLBySession(id string) bool {
	if controller := c.controller(); controller != nil {
		return controller.RenewTTLBySession(id)
	}
	return false
}

func (c *countingSpawner) DefaultTTL() time.Duration {
	if controller := c.controller(); controller != nil {
		return controller.DefaultTTL()
	}
	return 0
}
