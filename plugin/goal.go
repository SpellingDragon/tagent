package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// GoalStatus is the lifecycle state of one goal. The four words are the ones
// the governance semantics fix and they are kept verbatim: active is the goal
// in flight — it occupies governance budget and takes part in the gate ruling;
// achieved is reached, so it holds no budget and takes no part in the ruling;
// abandoned is given up on deliberately; expired is the goal that outlived its
// validity unreached, kept apart from abandoned so statistics and budget
// attribution can tell the two apart.
//
// Only two of the four are ever written: a goal is explicitly resolved to
// achieved or abandoned, and expired is derived from ExpiresMs — a goal past
// its validity simply stops counting as active and its Status is never flipped
// by the registry. The resolution face takes the two writable words and the
// tool layer's two-value enumeration guards the rest; the registry itself
// guards no terminal state.
//
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
type GoalStatus string

const (
	// GoalActive is the goal in flight: it occupies governance budget and
	// takes part in the gate ruling.
	GoalActive GoalStatus = "active"
	// GoalAchieved is the reached goal: it holds no budget and takes no part in
	// the gate ruling.
	GoalAchieved GoalStatus = "achieved"
	// GoalAbandoned is the goal given up on deliberately, for example because
	// a better path replaced it or an operator ended it.
	GoalAbandoned GoalStatus = "abandoned"
	// GoalExpired is the goal that outlived its validity unreached. It is
	// derived from ExpiresMs and never written, kept apart from abandoned for
	// statistics and budget attribution.
	GoalExpired GoalStatus = "expired"
)

// SubtypeGoalDeclared and SubtypeGoalResolved are the goal operation words a
// goal governance event carries. The state words stay the old ones; these two
// name the operations and are the event vocabulary entry the goal contract
// fixes.
const (
	// SubtypeGoalDeclared is the governance event subtype of a declared goal.
	SubtypeGoalDeclared = "goal_declared"
	// SubtypeGoalResolved is the governance event subtype of a resolved goal.
	SubtypeGoalResolved = "goal_resolved"
)

// goalOpDeclared and goalOpResolved are the payload forms the goal events carry:
// they name the same two operations the subtypes above name, in the payload form
// the goal events have always spoken.
const (
	goalOpDeclared = "declared"
	goalOpResolved = "resolved"
)

// Goal is one autonomous goal declaration.
type Goal struct {
	// ID is the goal identity, minted in declaration order.
	ID string `json:"id"`
	// Statement is what the goal wants done.
	Statement string `json:"statement"`
	// CreatedBy names who declared the goal.
	CreatedBy string `json:"created_by"`
	// Status is the lifecycle state the goal is in.
	Status GoalStatus `json:"status"`
	// CreatedMs is the declaration time, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the goal stops counting as active, in Unix
	// milliseconds. Zero means the goal never expires.
	ExpiresMs int64 `json:"expires_ms,omitempty"`
}

// goalEventPayload is the structured JSON one goal governance event carries:
// the goal it is about, the operation it records and the fields that operation
// froze.
type goalEventPayload struct {
	GoalID    string `json:"goal_id"`
	Op        string `json:"op"`
	Statement string `json:"statement,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	Status    string `json:"status,omitempty"`
	ExpiresMs int64  `json:"expires_ms,omitempty"`
}

// GoalRegistry is the goal registry: every declared goal in memory beside the
// governance events the declarations and resolutions also write. A declaration
// activates the governance goal gate, and a refusal of a goal-required call
// permeates the goal context into its record.
//
// The registry is concurrent safe, and its durable face is late-bound and nil
// safe: without a store it degrades to memory state instead of failing, and
// binding one replays the stored goal events back into the registry, so a
// restart keeps the goals it already declared.
//
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
type GoalRegistry struct {
	mu    sync.RWMutex
	goals map[string]*Goal
	seq   int

	log    GovernanceEventLog
	evtSeq int64
}

// NewGoalRegistry builds one goal registry in memory state. BindStore hands it
// the durable face whenever the wiring has one.
func NewGoalRegistry() *GoalRegistry {
	return &GoalRegistry{goals: make(map[string]*Goal)}
}

// BindStore hands the registry its durable face and replays the goal events
// already stored there back into it — declarations and resolutions in the
// order the operations happened, so a resolved goal is never resurrected and a
// later declaration never reuses an identity. It is nil safe, and it runs
// single threaded at construction.
func (g *GoalRegistry) BindStore(log GovernanceEventLog) {
	if g == nil || log == nil {
		return
	}
	g.mu.Lock()
	g.log = log
	g.evtSeq = g.seedSeqLocked()
	g.mu.Unlock()
	g.rebuildFromStore()
}

// Declare registers one goal and returns its identity. The declaration writes
// its governance event beside the memory entry, so a restart replays it back.
func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	g.seq++
	id := fmt.Sprintf("g-%d", g.seq)
	now := time.Now().UnixMilli()
	goal := &Goal{
		ID: id, Statement: statement, CreatedBy: createdBy,
		Status: GoalActive, CreatedMs: now, ExpiresMs: expiresMs,
	}
	g.goals[id] = goal
	evtKey := g.eventKeyLocked()
	log := g.log
	g.mu.Unlock()
	g.writeGoalEvent(log, goalEventPayload{
		GoalID: id, Op: goalOpDeclared, Statement: statement,
		CreatedBy: createdBy, ExpiresMs: expiresMs,
	}, evtKey, now, SubtypeGoalDeclared)
	return id
}

// Resolve moves one goal to its resolution and reports whether the goal exists.
// The two writable resolutions are achieved and abandoned; expired is derived
// and never written here. The registry guards no terminal state: a repeated
// resolution is applied as it arrives, and the tool layer's two-value
// enumeration is what keeps an unwritten word out.
func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	goal, ok := g.goals[id]
	if ok {
		goal.Status = status
	}
	evtKey := g.eventKeyLocked()
	now := time.Now().UnixMilli()
	log := g.log
	g.mu.Unlock()
	if ok {
		g.writeGoalEvent(log, goalEventPayload{
			GoalID: id, Op: goalOpResolved, Status: string(status),
		}, evtKey, now, SubtypeGoalResolved)
	}
	return ok
}

// List returns a snapshot copy of every goal the registry holds, in no
// particular order. The copy keeps a reader from mutating registry state.
func (g *GoalRegistry) List() []*Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Goal, 0, len(g.goals))
	for _, goal := range g.goals {
		cp := *goal
		out = append(out, &cp)
	}
	return out
}

// HasActive reports whether an unexpired active goal exists — the judgment the
// governance goal gate rules on.
func (g *GoalRegistry) HasActive() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	for _, goal := range g.goals {
		if inFlight(goal, now) {
			return true
		}
	}
	return false
}

// Active returns every unexpired active goal, for diagnostics and tools.
func (g *GoalRegistry) Active() []*Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	out := make([]*Goal, 0)
	for _, goal := range g.goals {
		if inFlight(goal, now) {
			out = append(out, goal)
		}
	}
	return out
}

// Current returns the goal the registry reads as the current goal context: the
// newest goal still in flight, or nil when none is. A refusal permeates this
// goal into its record, so the audit reads which autonomous scope the ruled
// call was hanging on.
func (g *GoalRegistry) Current() *Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	var current *Goal
	for _, goal := range g.goals {
		if !inFlight(goal, now) {
			continue
		}
		if current == nil || newerGoal(goal, current) {
			current = goal
		}
	}
	if current == nil {
		return nil
	}
	cp := *current
	return &cp
}

// inFlight reports whether the goal counts as active: declared and not past
// its validity. A goal past its validity is the derived expired state — it
// stops counting here and its recorded Status is never flipped.
func inFlight(goal *Goal, nowMs int64) bool {
	return goal.Status == GoalActive && (goal.ExpiresMs == 0 || goal.ExpiresMs > nowMs)
}

// newerGoal reports whether one goal was declared after another, falling back
// to declaration order for a same-moment pair.
func newerGoal(a, b *Goal) bool {
	if a.CreatedMs != b.CreatedMs {
		return a.CreatedMs > b.CreatedMs
	}
	return goalSeqOf(a.ID) > goalSeqOf(b.ID)
}

// goalSeqOf reads the declaration order one goal identity carries.
func goalSeqOf(id string) int {
	var n int
	if _, err := fmt.Sscanf(id, "g-%d", &n); err != nil {
		return 0
	}
	return n
}

// eventKeyLocked allocates the ordered identity of the next goal event. The
// caller holds the lock, so the key order is the operation order and a replay
// reconstructs it exactly — a resolved goal is never resurrected by a rebuild.
func (g *GoalRegistry) eventKeyLocked() string {
	g.evtSeq++
	return fmt.Sprintf("%s%019d", goalEventPrefix, g.evtSeq)
}

// seedSeqLocked continues the event numbering where the stored stream ends, so
// a restarted registry never reuses a key. The caller holds the lock.
func (g *GoalRegistry) seedSeqLocked() int64 {
	return seedEventSeq(context.Background(), g.log, goalEventPrefix)
}

// writeGoalEvent writes one goal operation as the governance event its
// declaration or resolution requires. A write that fails is dropped beside the
// standing memory entry: the goal gate keeps working and the audit
// persistence stays best effort.
func (g *GoalRegistry) writeGoalEvent(log GovernanceEventLog, p goalEventPayload, key string, ts int64, subtype string) {
	if log == nil {
		return
	}
	content, err := json.Marshal(p)
	if err != nil {
		return
	}
	meta := map[string]string{
		MetaKeySubtype: subtype,
		"goal_op":      p.Op,
		"goal_id":      p.GoalID,
	}
	evt := event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeGovernance,
		Timestamp: time.UnixMilli(ts),
		Meta:      meta,
		Message:   contentMessage(string(content)),
	}
	putGovernanceEvent(context.Background(), log, key, evt)
}

// rebuildFromStore replays the stored goal events back into the registry: the
// declared goals and their resolutions in the order the operations happened,
// with the declaration order realigned so a later declaration never reuses an
// identity a stored goal already took. A malformed event is skipped rather
// than failing the rebuild.
func (g *GoalRegistry) rebuildFromStore() {
	g.mu.RLock()
	log := g.log
	g.mu.RUnlock()
	if log == nil {
		return
	}
	events := replayGovernanceEvents(context.Background(), log, goalEventPrefix)
	g.mu.Lock()
	defer g.mu.Unlock()
	maxSeq := g.seq
	for _, evt := range events {
		if evt.Type != event.TypeGovernance {
			continue
		}
		var p goalEventPayload
		if err := json.Unmarshal([]byte(evt.Message.Content), &p); err != nil || p.GoalID == "" {
			continue
		}
		switch p.Op {
		case goalOpDeclared:
			g.goals[p.GoalID] = &Goal{
				ID: p.GoalID, Statement: p.Statement, CreatedBy: p.CreatedBy,
				Status: GoalActive, CreatedMs: evt.Timestamp.UnixMilli(), ExpiresMs: p.ExpiresMs,
			}
		case goalOpResolved:
			if goal, ok := g.goals[p.GoalID]; ok {
				goal.Status = GoalStatus(p.Status)
			}
		}
		if n := goalSeqOf(p.GoalID); n > maxSeq {
			maxSeq = n
		}
	}
	g.seq = maxSeq
}
