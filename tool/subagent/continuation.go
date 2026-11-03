// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// ErrContinuationUnclaimed reports a round served for a task whose one slot no
// caller claimed: the input the round would read was never owned by this call, so
// serving it would hand a body to a round a second caller believes it owns.
var ErrContinuationUnclaimed = errors.New("subagent: the round of that task was not claimed before it was served")

// continuations is the round-typed continuation point of the delegated turns one
// tool serves: one entry per served task, holding the assembly, the address and
// the delegation faces its next round runs and settles through. The table is
// built inside this package and handed to a tool through an interface, so the
// mechanism never becomes a public type a caller can construct or extend.
//
// A delegated task is served round by round. The first round runs beside the
// admission, and a re-entry hands fresh input to the same task and serves its
// next round on the assembly the first round ran, so the sub-agent keeps its own
// declaration, prompt and tools across rounds instead of being assembled again.
// Every round settles the same task, so the terminal of a later round is a new
// notification of that task and never a task of its own. A task no caller
// re-enters keeps the one-round contract it had before this face existed: one
// round, one terminal, and nothing else changes.
type continuations struct {
	mu     sync.Mutex
	served map[event.TaskID]*continuation
	// faces is the run face every seeded entry is served through: the delegation
	// and borrowing faces a tool of this run hands the account, beside the account
	// itself. It is adopted by NewTool, so a table minted by the first delegated
	// tool of a run can rebuild a round after a restart through the very faces the
	// run holds now instead of the ones a stopped process left.
	faces delegationFaces
}

// continuation is the served task a re-entry may hand input to: the faces its
// next round settles through, the assembly and address it runs on, whether a
// round of it is running right now, and the input a round that ended beside still
// owes.
type continuation struct {
	faces    delegationFaces
	builder  Builder
	assembly *Assembly
	addr     event.Address
	running  bool
	// owed holds the input one earlier round could not settle, in the order it was
	// handed over. A body whose round ends beside stays owed rather than being
	// dropped, and the round the next re-entry serves carries it ahead of the body
	// that re-entry was handed, so no accepted body is lost.
	owed []string
}

// newContinuations returns an empty continuation account. The first tool of a
// run mints the one account its delegated rounds are parked in, and every later
// tool of the same run is built beside the account the first one answered with,
// so a task served by one agent's tool is re-entered through the same account
// after a generation change.
func newContinuations() *continuations {
	return &continuations{served: map[event.TaskID]*continuation{}}
}

// adopt keeps the run faces this account serves its delegated rounds through:
// the delegation face a round settles through, the borrowing face it is served
// on and the account itself. NewTool adopts them beside the table it mints or
// shares, so a table rebuilt after a restart still settles through the run that
// holds it now rather than through a process that ended.
func (c *continuations) adopt(faces delegationFaces) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if faces.continuations == nil {
		faces.continuations = c
	}
	c.faces = faces
}

// Seed rebuilds the continuation of one task from the declaration its delegated
// input fact carries: the assembly the round ran on, the execution site its
// address names, and the builder the round is served through. A re-entry after a
// restart reaches this method before it claims the round, so the task it names
// is served on the declaration the chain recorded instead of being refused as a
// round this process never saw. A task the table already serves is left exactly
// as it is: the live entry is the one authority of its round and its owed input.
//
// The site is rebuilt from the declaration's name and the caller session the
// delegated call was admitted from, never from a second persisted address: the
// agent is the assembled name and the session is the caller session beside the
// native call identity, so the address derives from facts the chain already
// holds.
func (c *continuations) Seed(taskID event.TaskID, declaration []byte, siteSession event.SessionID, builder Builder) error {
	if c == nil {
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	assembly, err := decodeDelegatedDeclaration(declaration)
	if err != nil {
		return err
	}
	site := event.Address{AgentID: event.AgentID(assembly.Name), SessionID: siteSession}
	if !site.IsComplete() {
		return fmt.Errorf("%w: %s carries no complete execution site", errContinuationUnbacked, taskID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.served[taskID]; held {
		return nil
	}
	faces := c.faces
	if faces.delegation == nil || faces.bindings == nil {
		return fmt.Errorf("%w: %s", errContinuationUnbacked, taskID)
	}
	if faces.continuations == nil {
		faces.continuations = c
	}
	c.served[taskID] = &continuation{
		faces:    faces,
		builder:  builder,
		assembly: assembly,
		addr:     site,
	}
	return nil
}

// DelegatedSiteSession reports the session of the execution site one delegated
// call runs in: the caller session the call was issued from beside the native
// call identity the site was minted for. The site's agent is the assembled
// name, so this session alone locates the site's chain before the declaration
// travelling on it names that agent. The identity is the frozen one the tool
// mints the site under, never a stamp of the round.
func DelegatedSiteSession(caller event.SessionID, callID string) event.SessionID {
	if caller == "" || callID == "" {
		return ""
	}
	return event.SessionID(string(caller) + "#sub:" + callID)
}

// delegatedDeclarationVersion is the one wire version of the declaration a
// delegated input fact carries. A body under another version is refused by
// name rather than read under fields it may not mean.
const delegatedDeclarationVersion = 1

// delegatedDeclaration is the wire form of one resolved assembly as a delegated
// input fact carries it: every field the builder resolves — the name and
// description the turn runs under, its instruction, model, skill, tool and MCP
// references and the sampling its model requests carry — and nothing only the
// process that admitted the task can know, such as a live model or tool value.
type delegatedDeclaration struct {
	Version         int      `json:"v"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Model           string   `json:"model"`
	Instruction     string   `json:"instruction,omitempty"`
	Skills          []string `json:"skills,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	MCP             []string `json:"mcp,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	ThinkingEnabled *bool    `json:"thinking_enabled,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
}

// encodeDelegatedDeclaration renders the declaration one delegated input fact
// carries, so the assembly a round runs on survives the process that admitted
// it without a second durable account beside the fact chain.
func encodeDelegatedDeclaration(assembly *Assembly) ([]byte, error) {
	if assembly == nil {
		return nil, ErrAssemblyMissing
	}
	return json.Marshal(delegatedDeclaration{
		Version:         delegatedDeclarationVersion,
		Name:            assembly.Name,
		Description:     assembly.Description,
		Model:           assembly.Model,
		Instruction:     assembly.Instruction,
		Skills:          assembly.Skills,
		Tools:           assembly.Tools,
		MCP:             assembly.MCP,
		Temperature:     assembly.Temperature,
		ThinkingEnabled: assembly.ThinkingEnabled,
		ReasoningEffort: assembly.ReasoningEffort,
	})
}

// decodeDelegatedDeclaration reads the declaration one delegated input fact
// carries back into the assembly a re-entered round is served on. A body this
// build does not know, or one that names no agent, is refused by name: a round
// nobody can name is not run on a guessed declaration.
func decodeDelegatedDeclaration(body []byte) (*Assembly, error) {
	if len(body) == 0 {
		return nil, errDeclarationMissing
	}
	var wire delegatedDeclaration
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("%w: %v", errDeclarationMissing, err)
	}
	if wire.Version != delegatedDeclarationVersion {
		return nil, fmt.Errorf("%w: version %d", errDeclarationVersion, wire.Version)
	}
	if strings.TrimSpace(wire.Name) == "" {
		return nil, fmt.Errorf("%w: the declaration names no agent", errDeclarationMissing)
	}
	return &Assembly{
		Definition: Definition{
			Name:            wire.Name,
			Description:     wire.Description,
			Model:           wire.Model,
			Prompt:          wire.Instruction,
			Skills:          wire.Skills,
			Tools:           wire.Tools,
			MCP:             wire.MCP,
			Temperature:     wire.Temperature,
			ThinkingEnabled: wire.ThinkingEnabled,
			ReasoningEffort: wire.ReasoningEffort,
		},
		Instruction: wire.Instruction,
	}, nil
}

// errDeclarationMissing reports a delegated input fact that carries no
// declaration, or one this build cannot read.
var errDeclarationMissing = errors.New("subagent: the delegated input carries no readable declaration")

// errDeclarationVersion reports a declaration written under a wire version this
// build does not know.
var errDeclarationVersion = errors.New("subagent: the delegated declaration carries an unknown version")

// errContinuationUnbacked reports a continuation account asked to seed a round
// without the run faces a round settles through, so no re-entry could serve it.
var errContinuationUnbacked = errors.New("subagent: the continuation account carries no run face to serve a rebuilt round")

// register opens the continuation of one task the tool starts to serve, so a
// re-entry may hand it fresh input once the round being served settles.
func (c *continuations) register(taskID event.TaskID, faces delegationFaces, builder Builder, assembly *Assembly, addr event.Address) {
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
func (c *continuations) idle(taskID event.TaskID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.served[taskID]; ok {
		entry.running = false
	}
}

// Served reports whether this tool still serves the named task a round of its own.
func (c *continuations) Served(taskID event.TaskID) bool {
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
func (c *continuations) Release(taskID event.TaskID) {
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
//
// The caller that must settle other preconditions before the round is served claims
// the slot first and serves it afterwards: the round a claim took is the round the
// body is owed to, so a caller that returns a delivery receipt for it knows the one
// round carrying the body exists.
func (c *continuations) Reenter(ctx context.Context, taskID event.TaskID, input string) error {
	if err := c.Claim(taskID); err != nil {
		return err
	}
	return c.Serve(ctx, taskID, input, nil)
}

// Claim takes the one round slot of the named task before any round is served for
// it, and reports the claim it could not make by name: a task this tool serves no
// round of, and a task whose round is already in flight, are each refused.
//
// The claim is the point at which the body handed to the re-entry is owned: from
// the moment it returns, the one round that will carry that body is this caller's,
// so a second caller is refused instead of being told a body was taken that no
// round holds. A claim a caller does not serve gives the slot back with Abandon.
func (c *continuations) Claim(taskID event.TaskID) error {
	if c == nil {
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.served[taskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	if entry.running {
		return fmt.Errorf("%w: %s", ErrContinuationBusy, taskID)
	}
	entry.running = true
	return nil
}

// Abandon gives back a claimed slot without serving a round for it, so a re-entry
// that claimed the one round of a task and then found a precondition gone leaves
// the task re-entrant instead of holding a slot no round serves.
func (c *continuations) Abandon(taskID event.TaskID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.served[taskID]; ok {
		entry.running = false
	}
}

// Serve serves the next round of a task this caller already claimed, reading the
// input one earlier round still owes ahead of the input handed here, and releases
// the slot when the round ends.
//
// The answer is the body's delivery and not its acceptance: Serve returns once the body is in
// the round that carries it, so a caller answering a delivery receipt answers it for a body
// that arrived. Only a round that never reaches its input returns a cause — the caller puts
// the task and the ledger back. A round that lands its input and then fails to record its
// terminal reports the cause to after instead: the failure belongs to the round, the accepted
// body stays owed to the next round, and never denies the input the receipt already answered.
func (c *continuations) Serve(ctx context.Context, taskID event.TaskID, input string, after func(error)) error {
	if c == nil {
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	c.mu.Lock()
	entry, ok := c.served[taskID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrContinuationMissing, taskID)
	}
	if !entry.running {
		c.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrContinuationUnclaimed, taskID)
	}
	owed := entry.owed
	entry.owed = nil
	c.mu.Unlock()

	started := make(chan error, 1)
	go c.fly(context.WithoutCancel(ctx), entry, taskID, owed, input, func(err error) { started <- err }, after)
	return <-started
}

// fly runs one round of a served task beside the call that handed the body over: the start is
// reported as soon as the round's own input landed, so the caller's receipt answers for the
// body instead of for a goroutine, and the terminal is recorded beside it.
//
// A round that never lands its input settles nothing: there is no round of the task to record a
// terminal for, so the body stays owed and the caller that reads the reported cause puts the
// task back. Only a round whose input landed is settled, beside a nil or a failed outcome — a
// round that landed the body owes the terminal of the task it belongs to. The round holds the
// one slot of its task for its whole life, and the defer below is what
// always gives it back: a round that ends beside an unrecorded terminal leaves the whole input
// owed instead of dropping it, and a round that panics does the same — the slot is idle and the
// body is owed again before the panic is recovered, so a task whose round died is re-entrant
// rather than holding a slot no turn will ever release. The recovered round is reported as a
// failure, never as a round that started and settled. A terminal the round
// could not record is reported to after as that round's own failure: the slot
// is given back and the input stays owed, while the committed input itself is
// never withdrawn by this table.
func (c *continuations) fly(ctx context.Context, entry *continuation, taskID event.TaskID, owed []string, input string, start, after func(error)) {
	landed := make(chan error, 1)
	var once sync.Once
	report := func(err error) {
		once.Do(func() {
			landed <- err
			start(err)
		})
	}
	settled := false
	defer func() {
		c.mu.Lock()
		entry.running = false
		if !settled {
			entry.owed = append(append(make([]string, 0, len(owed)+1), owed...), input)
		}
		c.mu.Unlock()
		failure := recover()
		if failure == nil {
			return
		}
		err := fmt.Errorf("subagent: the next round of %s ended beside a panic: %v", taskID, failure)
		report(err)
		announce(after, err)
	}()
	status, body := entry.faces.runRound(ctx, entry.builder, entry.assembly, entry.addr, joinOwed(owed, input), nil, report)
	if !continuationLanded(landed) {
		return
	}
	err := entry.faces.completeDelegation(ctx, taskID, status, body)
	if err == nil {
		settled = true
	}
	announce(after, err)
}

// continuationLanded reports whether the round of one continuation landed its own input: the
// report is read from the channel the round answers once, so a round that was never reported as
// started is told apart from a round that started and a round that started beside a cause.
func continuationLanded(report chan error) bool {
	select {
	case err := <-report:
		return err == nil
	default:
		return false
	}
}

// announce tells the caller that handed a body over how its round ended, if it asked: the
// cause of a round whose terminal was not recorded, and nil for one that settled.
func announce(after func(error), err error) {
	if after != nil {
		after(err)
	}
}

// joinOwed renders the input one round reads: the bodies an earlier round left owed,
// in the order they were handed over, and the body this round was handed last.
func joinOwed(owed []string, input string) string {
	if len(owed) == 0 {
		return input
	}
	bodies := make([]string, 0, len(owed)+1)
	bodies = append(bodies, owed...)
	bodies = append(bodies, input)
	return strings.Join(bodies, hostOwedSeparator)
}

// hostOwedSeparator is the blank line between the bodies one round reads: each body
// is a whole message of its own, so a round that carries a body left owed ahead of
// the one just handed over reads both as the separate deliveries they were.
const hostOwedSeparator = "\n\n"
