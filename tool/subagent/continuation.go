// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// ErrContinuationMissing reports a re-entry that names a task this tool serves no
// round of: the task was never delegated here, or its continuation was released.
var ErrContinuationMissing = errors.New("subagent: no delegated turn of that task is served here")

// ErrContinuationBusy reports a re-entry that names a task whose round is still
// running. One round of one task runs at a time, so a re-entry arriving inside a
// round is refused by name rather than queued behind it.
var ErrContinuationBusy = errors.New("subagent: a round of that delegated task is in flight")

// Continuations is the round-typed continuation point of the delegated turns one
// tool serves: one entry per served task, holding the assembly, the address and
// the delegation faces its next round runs and settles through.
//
// A delegated task is served round by round. The first round runs beside the
// admission, and a re-entry hands fresh input to the same task and serves its
// next round on the assembly the first round ran, so the sub-agent keeps its own
// declaration, prompt and tools across rounds instead of being assembled again.
// Every round settles the same task, so the terminal of a later round is a new
// notification of that task and never a task of its own. A task no caller
// re-enters keeps the one-round contract it had before this face existed: one
// round, one terminal, and nothing else changes.
type Continuations struct {
	mu     sync.Mutex
	served map[event.TaskID]*continuation
}

// continuation is the served task a re-entry may hand input to: the faces its
// next round settles through, the assembly and address it runs on, and whether a
// round of it is running right now.
type continuation struct {
	faces    delegationFaces
	builder  Builder
	assembly *Assembly
	addr     event.Address
	running  bool
}

// NewContinuations returns an empty continuation table. A tool given none keeps
// the one-round contract and serves no second round.
func NewContinuations() *Continuations {
	return &Continuations{served: map[event.TaskID]*continuation{}}
}

// register opens the continuation of one task the tool starts to serve, so a
// re-entry may hand it fresh input once the round being served settles.
func (c *Continuations) register(taskID event.TaskID, faces delegationFaces, builder Builder, assembly *Assembly, addr event.Address) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.served[taskID] = &continuation{
		faces:    faces,
		builder:  builder,
		assembly: assembly,
		addr:     addr,
		running:  true,
	}
}

// idle reports that the running round of one task ended, so a re-entry may open
// the next one.
func (c *Continuations) idle(taskID event.TaskID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.served[taskID]; ok {
		entry.running = false
	}
}

// Served reports whether this tool still serves the named task a round of its own.
func (c *Continuations) Served(taskID event.TaskID) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.served[taskID]
	return ok
}

// Release ends the continuation of one task: a released task is refused by a later
// re-entry instead of being served a round nobody asked for.
func (c *Continuations) Release(taskID event.TaskID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.served, taskID)
}

// Reenter hands fresh input to the named task and serves its next round on the
// assembly its first round ran. The round settles through the same delegation face
// the first round settled through, so its terminal belongs to the same task and the
// re-entry never mints a task of its own. A task whose round is still in flight is
// refused by name beside no round served, so two re-entries of one task serve one
// round between them.
func (c *Continuations) Reenter(ctx context.Context, taskID event.TaskID, input string) error {
	if c == nil {
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	c.mu.Lock()
	entry, ok := c.served[taskID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	if entry.running {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrContinuationBusy, taskID)
	}
	entry.running = true
	c.mu.Unlock()

	status, body := entry.faces.runRound(ctx, entry.builder, entry.assembly, entry.addr, input, nil)
	err := entry.faces.completeDelegation(ctx, taskID, status, body)
	c.mu.Lock()
	entry.running = false
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("subagent: the next round of %s settled beside: %w", taskID, err)
	}
	return nil
}
