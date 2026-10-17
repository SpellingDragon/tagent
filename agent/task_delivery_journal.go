// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// taskSignalRecord preserves a producer's exact result before transport acceptance.
type taskSignalRecord struct {
	TaskID              string             `json:"task_id"`
	Kind                task.SettleKind    `json:"kind"`
	Output              string             `json:"output,omitempty"`
	Error               string             `json:"error,omitempty"`
	ErrorKind           string             `json:"error_kind,omitempty"`
	RoundID             uint64             `json:"task_round_id"`
	RoundOwner          string             `json:"round_owner,omitempty"`
	WaitFor             task.WaitCondition `json:"wait_for,omitempty"`
	InputBatchID        string             `json:"input_batch_id,omitempty"`
	Seq                 uint64             `json:"seq"`
	ObservedAt          time.Time          `json:"observed_at"`
	Final               bool               `json:"final"`
	Ready               bool               `json:"ready"`
	WaitSatisfied       bool               `json:"wait_satisfied"`
	Observation         string             `json:"observation,omitempty"`
	Lineage             string             `json:"lineage,omitempty"`
	ExitCode            int                `json:"exit_code,omitempty"`
	ProducerRound       uint64             `json:"producer_round,omitempty"`
	ProducerStatus      task.TaskStatus    `json:"producer_status,omitempty"`
	ProducerOwner       string             `json:"producer_owner,omitempty"`
	ProducerLastSeq     uint64             `json:"producer_last_seq,omitempty"`
	LifetimeAnchorMilli int64              `json:"lifetime_anchor_milli,omitempty"`
	RoundInput          string             `json:"round_input,omitempty"`
	Inline              bool               `json:"inline,omitempty"`
	InlineTurnToken     string             `json:"inline_turn_token,omitempty"`
	InlineReceiptKey    int64              `json:"inline_receipt_key,omitempty"`
	ReceiptKey          int64              `json:"receipt_key"`
	Event               *AgentEvent        `json:"event"`
	ChildInvocationID   string             `json:"child_invocation_id,omitempty"`
	ChildAgentName      string             `json:"child_agent_name,omitempty"`
	ChildContinuation   bool               `json:"child_continuation,omitempty"`
	Declarative         *task.Declarative  `json:"declarative,omitempty"`
}

func (r taskSignalRecord) signal() task.SettleSignal {
	sig := task.SettleSignal{Kind: r.Kind, Output: r.Output, RoundID: r.RoundID, ProducerRound: r.ProducerRound, RoundOwner: r.RoundOwner, WaitFor: r.WaitFor, InputBatchID: r.InputBatchID, Declarative: r.Declarative, Seq: r.Seq, ObservedAt: r.ObservedAt, Final: r.Final, Ready: r.Ready, WaitSatisfied: r.WaitSatisfied, Observation: r.Observation, Lineage: r.Lineage, ExitCode: r.ExitCode}
	if r.Error != "" {
		sig.Err = errors.New(r.Error)
	}
	if r.ErrorKind == "continuation_interrupted" {
		sig.Observation = "continuation_interrupted"
	}
	return sig
}

type taskDeliveryJournal struct {
	mu       sync.Mutex
	pending  map[string]*frozenTaskDelivery
	phases   map[string]memory.FullEvent
	leases   map[int64]int64
	accepted map[int64]bool
	consumed map[int64]bool
}

type frozenTaskDelivery struct {
	event         *AgentEvent
	fact, receipt memory.FullEvent
	received      bool
}

func (s *taskRecordSink) retainSignalKeys(rawKey, receiptKey int64) {
	if s == nil || s.cm == nil || rawKey <= 0 || receiptKey <= 0 {
		return
	}
	guard, _ := s.cm.memStore.(memory.RetentionGuard)
	j := &s.delivery
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.leases == nil {
		j.leases = make(map[int64]int64)
	}
	if _, held := j.leases[rawKey]; !held {
		if guard != nil {
			guard.ProtectKey(rawKey)
			guard.ProtectKey(receiptKey)
		}
		j.leases[rawKey] = receiptKey
	}
}

func (s *taskRecordSink) releaseSignalKeys(rawKey, receiptKey int64) bool {
	if s == nil || s.cm == nil {
		return false
	}
	j := &s.delivery
	j.mu.Lock()
	defer j.mu.Unlock()
	return s.releaseSignalKeysLocked(rawKey, receiptKey)
}

func (s *taskRecordSink) releaseSignalKeysLocked(rawKey, receiptKey int64) bool {
	j := &s.delivery
	if j.leases[rawKey] != receiptKey || receiptKey <= 0 {
		return false
	}
	delete(j.leases, rawKey)
	delete(j.accepted, rawKey)
	delete(j.consumed, rawKey)
	if len(j.leases) == 0 {
		j.leases = nil
		j.accepted, j.consumed = nil, nil
	}
	if guard, ok := s.cm.memStore.(memory.RetentionGuard); ok {
		guard.ReleaseKey(rawKey)
		guard.ReleaseKey(receiptKey)
	}
	return true
}

func (s *taskRecordSink) markSignalAccepted(rawKey, receiptKey int64) {
	s.markSignalProgress(rawKey, receiptKey, true)
}

func (s *taskRecordSink) markSignalConsumed(rawKey, receiptKey int64) {
	s.markSignalProgress(rawKey, receiptKey, false)
}

func (s *taskRecordSink) markSignalProgress(rawKey, receiptKey int64, accepted bool) {
	if s == nil || s.cm == nil {
		return
	}
	j := &s.delivery
	j.mu.Lock()
	defer j.mu.Unlock()
	if receiptKey <= 0 || j.leases[rawKey] != receiptKey {
		return
	}
	if accepted {
		if j.accepted == nil {
			j.accepted = make(map[int64]bool)
		}
		j.accepted[rawKey] = true
	} else {
		if j.consumed == nil {
			j.consumed = make(map[int64]bool)
		}
		j.consumed[rawKey] = true
	}
	if j.accepted[rawKey] && j.consumed[rawKey] {
		s.releaseSignalKeysLocked(rawKey, receiptKey)
	}
}

func (s *taskRecordSink) commitFrozen(f memory.FullEvent) bool {
	if s == nil || s.cm == nil {
		return false
	}
	if s.cm.memStore == nil {
		return true
	}
	var err error
	if _, ok := s.cm.memStore.(memory.EventReplayer); ok {
		err = s.cm.commitReceiptFact(f)
	} else {
		err = s.cm.memStore.StoreEvent(f.EventKey, f)
	}
	if err != nil {
		log.Errorf("[task outbox] commit key=%d type=%s: %v", f.EventKey, f.EventType, err)
	}
	return err == nil
}

func (s *taskRecordSink) recordPhase(tk *task.Task, phase string) bool {
	if s == nil || s.cm == nil || tk == nil {
		return false
	}
	spec := tk.SpecSnapshot()
	if spec.Declarative == nil {
		return true
	}
	decl := *spec.Declarative
	decl.Mode, decl.WaitFor = spec.Mode, task.WaitForOf(spec)
	if decl.StartedAtMilli == 0 {
		decl.StartedAtMilli = tk.StartedAt.UnixMilli()
	}
	if len(decl.Origin) == 0 {
		decl.Origin = spec.Origin
	}
	if decl.Lifetime == "" {
		decl.Lifetime = task.LifetimeOf(spec)
	}
	data, err := json.Marshal(decl)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(data)
	id := fmt.Sprintf("%s/%d/%s/%x", tk.ID, decl.RoundID, phase, digest[:])
	j := &s.delivery
	j.mu.Lock()
	if j.phases == nil {
		j.phases = make(map[string]memory.FullEvent)
	}
	fact, exists := j.phases[id]
	if !exists {
		fact = memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(s.cm.partitionID, 0), PartitionID: s.cm.partitionID, EventType: tagentevent.TypeTaskSpawned, Timestamp: tk.StartedAt.UnixMilli(), Content: string(data), EventSummary: "任务绑定: " + spec.Desc, Metadata: map[string]string{tagentevent.MetaKeyAgentName: s.cm.name, "task_id": tk.ID, tagentevent.MetaKeyTaskRoundID: fmt.Sprint(decl.RoundID), tagentevent.MetaKeyTaskRoundOwner: decl.RoundOwner, "admission_phase": phase}}
		j.phases[id] = fact
	}
	j.mu.Unlock()
	ok := s.commitFrozen(fact)
	j.mu.Lock()
	if ok || phase != "round_aborted" {
		delete(j.phases, id)
	}
	j.mu.Unlock()
	return ok
}

func (s *taskRecordSink) prepareSignal(tk *task.Task, sig task.SettleSignal, workspace string, inline bool) (*frozenTaskDelivery, bool) {
	if s == nil || s.cm == nil || tk == nil {
		return nil, false
	}
	if sig.SourceID != "" {
		s.delivery.mu.Lock()
		d := s.delivery.pending[sig.SourceID]
		s.delivery.mu.Unlock()
		if d != nil {
			return d, true
		}
	}
	evt := newTaskSettledEvent(tk, sig, settleInlineCapChars, workspace)
	if evt == nil {
		return nil, false
	}
	j := &s.delivery
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.pending == nil {
		j.pending = make(map[string]*frozenTaskDelivery)
	}
	if d := j.pending[evt.ID]; d != nil {
		return d, true
	}
	key := memory.NewSnowflakeEventKey(s.cm.partitionID, 0)
	receiptKey := memory.NewSnowflakeEventKey(s.cm.partitionID, 0)
	evt.Metadata["task_signal_key"] = tagentevent.FormatEventKey(key)
	evt.Metadata["task_signal_owner"] = s.cm.name
	if len(sig.Output) > settleInlineCapChars && evt.Message != nil {
		evt.Message.Content += " [原始结果 eventkey=" + tagentevent.FormatEventKey(key) + "]"
	}
	r := taskSignalRecord{TaskID: tk.ID, Kind: sig.Kind, Output: sig.Output, RoundID: sig.RoundID, ProducerRound: sig.ProducerRound, RoundOwner: sig.RoundOwner, WaitFor: sig.WaitFor, Seq: sig.Seq, ObservedAt: evt.Timestamp, Final: sig.Final, Ready: sig.Ready, WaitSatisfied: sig.WaitSatisfied, Observation: sig.Observation, Lineage: sig.Lineage, ExitCode: sig.ExitCode, ReceiptKey: receiptKey, Event: evt, ProducerStatus: tk.Status(), ProducerOwner: tk.ProducerOwner(), ProducerLastSeq: tk.SignalSeq(), LifetimeAnchorMilli: tk.TTLAnchorMilli(), Inline: inline}
	if inline {
		r.InlineTurnToken = sig.InputBatchID
		if r.InlineTurnToken == "" && sig.RoundID <= 1 {
			r.InlineTurnToken = tk.SpecSnapshot().Origin["task_inline_turn"]
		}
		r.InlineReceiptKey = memory.NewSnowflakeEventKey(s.cm.partitionID, 0)
	}
	r.InputBatchID = sig.InputBatchID
	r.Declarative = sig.Declarative
	if r.Declarative != nil {
		r.ProducerOwner = r.Declarative.ProducerOwner
		r.ProducerLastSeq = r.Declarative.SignalSeq
		r.LifetimeAnchorMilli = r.Declarative.TTLAnchorMilli
	}
	if sig.Err != nil {
		r.Error = sig.Err.Error()
	}
	if sig.Observation == "continuation_interrupted" || sig.Observation == "continuation_not_accepted" {
		r.ErrorKind = sig.Observation
	}
	decl := sig.Declarative
	if decl == nil {
		decl = tk.SpecSnapshot().Declarative
	}
	if decl != nil && decl.Kind == "subagent" && sig.RoundID == sig.ProducerRound {
		r.ChildInvocationID = decl.Params["child_invocation_id"]
		r.ChildAgentName = decl.AgentName
		r.ChildContinuation = decl.Params["local_continuation"] == "true"
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, false
	}
	md := map[string]string{tagentevent.MetaKeyAgentName: s.cm.name, "task_id": tk.ID, tagentevent.MetaKeyTaskRoundID: fmt.Sprint(sig.RoundID), tagentevent.MetaKeyTaskRoundOwner: sig.RoundOwner, tagentevent.MetaKeyTaskSignalSeq: fmt.Sprint(sig.Seq), tagentevent.MetaKeySourceEventID: evt.ID, "child_invocation_id": r.ChildInvocationID, "producer_status": string(r.ProducerStatus), "signal_observation": sig.Observation, "signal_lineage": sig.Lineage, "signal_kind": string(sig.Kind), "signal_final": fmt.Sprint(sig.Final), "raw_signal_key": tagentevent.FormatEventKey(key)}
	d := &frozenTaskDelivery{event: evt, fact: memory.FullEvent{EventKey: key, PartitionID: s.cm.partitionID, EventType: tagentevent.TypeTaskSignal, Timestamp: evt.Timestamp.UnixMilli(), EventSummary: "任务信号: " + evt.ID, Content: string(data), Metadata: md}, receipt: memory.FullEvent{EventKey: receiptKey, PartitionID: s.cm.partitionID, EventType: tagentevent.TypeTaskSignalReceipt, Timestamp: evt.Timestamp.UnixMilli(), EventSummary: "任务信号已受理: " + evt.ID, Content: tagentevent.FormatEventKey(key), Metadata: md}}
	j.pending[evt.ID] = d
	if j.leases == nil {
		j.leases = make(map[int64]int64)
	}
	if guard, ok := s.cm.memStore.(memory.RetentionGuard); ok {
		guard.ProtectKey(key)
		guard.ProtectKey(receiptKey)
	}
	j.leases[key] = receiptKey
	return d, true
}

func (s *taskRecordSink) acceptSignal(tk *task.Task, sig task.SettleSignal, workspace string, accept func(*AgentEvent) bool) bool {
	d, ok := s.prepareSignal(tk, sig, workspace, accept == nil)
	if !ok || !s.commitFrozen(d.fact) {
		return false
	}
	j := &s.delivery
	j.mu.Lock()
	received := d.received
	j.mu.Unlock()
	if !received {
		if accept != nil && !accept(d.event) {
			return false
		}
		j.mu.Lock()
		d.received = true
		j.mu.Unlock()
	}
	if !s.commitFrozen(d.receipt) {
		return false
	}
	if !s.ackChildResult(d.fact, d.receipt) {
		return false
	}
	s.markSignalAccepted(d.fact.EventKey, d.receipt.EventKey)
	j.mu.Lock()
	delete(j.pending, d.event.ID)
	j.mu.Unlock()
	return true
}

func (s *taskRecordSink) acceptBatch(batch []task.BatchRetired, workspace string, bus *EventBus) bool {
	if len(batch) == 0 {
		return true
	}
	var ds []*frozenTaskDelivery
	var refs []taskBatchSignalRef
	var latest time.Time
	for _, r := range batch {
		d, ok := s.prepareSignal(r.Task, r.Sig, workspace, false)
		if !ok || !s.commitFrozen(d.fact) {
			return false
		}
		ds = append(ds, d)
		refs = append(refs, taskBatchSignalRef{RawKey: d.fact.EventKey, ReceiptKey: d.receipt.EventKey, SourceID: d.event.ID, TaskID: r.Task.ID, RoundID: r.Sig.RoundID, Seq: r.Sig.Seq, Owner: s.cm.name})
		if d.event.Timestamp.After(latest) {
			latest = d.event.Timestamp
		}
	}
	evt := newBatchRetiredSummaryEvent(batch)
	if evt == nil {
		return false
	}
	evt.ID = taskBatchID(refs)
	evt.Source = SourceTask
	evt.Timestamp = latest
	data, err := json.Marshal(refs)
	if err != nil {
		return false
	}
	evt.Metadata[taskBatchSignalsKey] = string(data)
	evt.Metadata[tagentevent.MetaKeySourceEventID] = evt.ID
	evt.Metadata[tagentevent.MetaKeySettleWake] = "decision"
	evt.Metadata[tagentevent.MetaKeySettleFinal] = "true"
	if _, err := bus.PublishContext(context.Background(), evt); err != nil {
		return false
	}
	j := &s.delivery
	j.mu.Lock()
	for _, d := range ds {
		d.received = true
	}
	j.mu.Unlock()
	accepted := true
	for _, r := range batch {
		if !s.acceptSignal(r.Task, r.Sig, workspace, nil) {
			accepted = false
		}
	}
	return accepted
}

func (s *taskRecordSink) ackChildResult(fact, proof memory.FullEvent) bool {
	if s.owner == nil || s.owner.memStore == nil {
		return true
	}
	var r taskSignalRecord
	if json.Unmarshal([]byte(fact.Content), &r) != nil {
		return false
	}
	if r.Kind != task.SettleCompleted && r.Kind != task.SettleFailed && r.Lineage != task.LineageRetired {
		return true
	}
	if r.ChildInvocationID == "" {
		return true
	}
	child := s.owner.resolveContinuationOwner(r.ChildAgentName)
	if child == nil {
		return !r.ChildContinuation
	}
	if r.ErrorKind == "continuation_not_accepted" {
		c, err := child.continuationByID(r.ChildInvocationID)
		if err != nil {
			return false
		}
		if c == nil {
			return true
		}
	}
	var err error
	if r.Lineage == task.LineageRetired {
		err = child.CancelContinuationByParent(context.Background(), r.ChildInvocationID, proof)
	} else if r.Kind == task.SettleCompleted || r.Kind == task.SettleFailed {
		err = child.AcknowledgeContinuationResultWithReceipt(context.Background(), r.ChildInvocationID, proof)
	}
	if err != nil {
		log.Errorf("[task outbox] child receipt: %v", err)
	}
	return err == nil
}

// RecordRecoveredTaskBinding commits an executor identity learned during recovery.
func (ta *TagentAgent) RecordRecoveredTaskBinding(tk *task.Task) bool {
	return ta != nil && ta.taskRecords != nil && ta.taskRecords.recordPhase(tk, "recovered")
}
