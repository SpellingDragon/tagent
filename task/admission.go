package task

import (
	"sync"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
//
// CallIdentity is the identity one native call admission is resolved against:
// the instance, the invocation, the decision and the native call identifier. A
// native CallID is not globally unique, so the same CallID in another instance
// is a different call.
type CallIdentity struct {
	// InstanceID is the runtime instance the call was admitted in.
	InstanceID event.InstanceID
	// InvocationID is the responsibility range the call belongs to.
	InvocationID event.InvocationID
	// DecisionID is the model decision the call came from.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier, kept byte-identical.
	CallID string
}

// DedupKey composes the dedup key of the identity, in the shape Spec.DedupKey
// carries it: instance, invocation, decision and native call identifier.
func (c CallIdentity) DedupKey() DedupKey {
	return DedupKey(string(c.InstanceID) + "\x00" + string(c.InvocationID) + "\x00" +
		string(c.DecisionID) + "\x00" + c.CallID)
}

// MemoryAdmission is the in-memory admission face: it returns the single receipt
// of one call admission and resolves a repeated admission against the dedup key
// of the call identity. The first admission of a key registers its task as a
// fresh admission; a repeated admission of that key reports the task the first
// one registered and adds no obligation.
type MemoryAdmission struct {
	mu    sync.Mutex
	tasks map[DedupKey]event.TaskID
}

// NewMemoryAdmission returns an empty in-memory admission face.
func NewMemoryAdmission() *MemoryAdmission {
	return &MemoryAdmission{tasks: map[DedupKey]event.TaskID{}}
}

// Admit returns the single receipt of one call admission. A fresh admission
// registers taskID and keeps its obligation open; a repeated admission of the
// same dedup key returns the task of the first admission as a dedup, which adds
// no obligation. Admission is not a result, so the receipt reports only that the
// call was admitted.
func (m *MemoryAdmission) Admit(identity CallIdentity, taskID event.TaskID) AdmissionReceipt {
	return m.AdmitKey(identity.DedupKey(), identity.CallID, taskID)
}

// AdmitKey returns the single receipt of one call admission resolved against a dedup
// key the caller composed. A fresh admission registers taskID and keeps its obligation
// open; a repeated admission of that key reports the task of the first admission as a
// dedup, which adds no obligation. An empty key disables deduplication, so every
// admission under it is fresh.
func (m *MemoryAdmission) AdmitKey(key DedupKey, callID string, taskID event.TaskID) AdmissionReceipt {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key != "" {
		if registered, ok := m.tasks[key]; ok {
			return AdmissionReceipt{
				CallID:   callID,
				TaskID:   registered,
				Accepted: true,
				Kind:     AdmissionDedup,
			}
		}
	}
	if key != "" {
		m.tasks[key] = taskID
	}
	return AdmissionReceipt{
		CallID:   callID,
		TaskID:   taskID,
		Accepted: true,
		Kind:     AdmissionFresh,
	}
}

// Lookup reports the task a repeated admission of this dedup key resolves to, and
// whether the key has been admitted at all.
func (m *MemoryAdmission) Lookup(key DedupKey) (event.TaskID, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	taskID, ok := m.tasks[key]
	return taskID, ok
}
