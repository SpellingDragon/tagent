// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// The read-only diagnostic projection of the runtime: what the owners already
// hold, read through one port in three semantic units — what the binding
// generation names, the session sites the state owner holds, and the tasks the
// task owner holds — so an assembly supplies one reading instead of one face per
// dimension.
package query

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// Reading is the single read port the runtime inventory gathers through: one
// reading of what the binding generation in effect names, of the session sites
// the state owner holds and of the tasks the task owner holds. Every method is a
// read of an owner that already holds the facts, and the three methods are the
// three semantic units of that reading rather than one method per owner type.
//
//   - the read path one assembly wires, each element a type of this package:
//   - Reading is the owner read port the assembly supplies once
//   - NewInventory(reads) is the inventory that gathers six dimensions from that port at read time
//   - NewInventorySource(inventory) is the production ReadingSource over that inventory
//   - ReadingSource.Freeze answers one DetailSource beside the per-source watermarks of the same gathering
//   - DetailSource is the frozen view a reading serves from: Order and DetailOf answer one identity at a time
//   - NewProjection(inventory, window) and NewBoundedProjection(source, window, budget) are the Projection over that source
//   - Projection.Snapshot, List, Detail and Follow read only frozen readings and never re-read the owners
//   - a frozen reading is never re-read or rewritten: later owner changes never reach it
//
// 红线清单（4 条）：
// MUST 每次调用都从持有该事实的 owner 现读，逐项如实返回或具名失败
// MUST 让源不可读的维度以具名拒绝回答，绝不以空明细冒充可读
// MUST NOT 记录、缓存或合成任何事实：读口自身不是第二个真源
// MUST NOT 放宽授权：越权对象被排除而非被改写
type Reading interface {
	// Published reads the binding generation currently in effect: the agent
	// identities it configures and the resource registrations it holds. Both
	// readings come from the one generation, so an agent listing and a lease
	// listing of one reading are never paired across two generations.
	Published(ctx context.Context) (Generation, error)
	// Sites reads every session site the state owner holds, each beside the
	// call currently holding the site's execution slot. A site the reader is
	// not granted is refused by the owner that holds it.
	Sites(ctx context.Context) ([]SiteReading, error)
	// Tasks reads every task the task owner holds: its declaration, the status
	// it currently stands in, the obligations its manager keeps open and the
	// notifications its commit record still owes.
	Tasks(ctx context.Context) ([]TaskReading, error)
}

// AttentionReading is the optional read face an assembly may wire beside the
// three readings of the port: the attention questions the owners still hold as
// questions at read time, each in the typed form the attention chain answers it
// in — the kind it was asked under, the target it was put to, the lineage it came
// from and the validity it was filed under. It is optional on purpose: the six
// dimensions of the inventory keep answering from the three readings alone, and a
// port that wires no attention face refuses the attention dimension by name
// instead of answering an empty presence.
//
// 红线清单（4 条）：
// MUST 每次调用从持有该问题的 owner 现读，逐项如实返回或具名失败
// MUST 以提问时的 kind、target、lineage 作为身份原样返回，不合成第二个身份
// MUST NOT 在本题面补造 question 或 clearance 事实：notice 与 summary 的在场是读时推导
// MUST NOT 记录、缓存或自行过期任何问题：过期与清除由持有该事实的读面判定
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
type AttentionReading interface {
	// AttentionQuestions reads every attention question still present at read
	// time: the questions no answer and no clearance closed and whose validity
	// has not run out, in whatever order the owners answer them.
	AttentionQuestions(ctx context.Context) ([]task.AttentionQuestion, error)
}

// Generation is what one published binding generation names: the agents it
// configures and the resources it registers. The two readings are taken from
// the same generation, so a lease is never reported beside an agent set that a
// later reload already replaced.
type Generation struct {
	// Agents lists the agent identities the generation configures.
	Agents []event.AgentID
	// Leases lists the resource registrations of the generation, in the order
	// the generation acquired them.
	Leases []state.LeaseRef
}

// SiteReading is one session site as its owner holds it, beside the call
// currently holding the site's execution slot. The site snapshot and the slot
// reading belong to one reading of the site, so a site is never reported as
// resident here and busy under another reading.
type SiteReading struct {
	// Session is the owner snapshot of the site, its mailbox and its own
	// consumption position included.
	Session state.Session
	// Invocation names the invocation whose call holds the site's execution
	// slot; it is empty when nobody holds the slot.
	Invocation event.InvocationID
	// Decision names the decision holding the site's execution slot; it is
	// empty when nobody holds the slot.
	Decision event.DecisionID
	// Held reports whether anyone holds the site's execution slot at all.
	Held bool
}

// TaskReading is one task as its owner holds it: the admission declaration, the
// status it currently stands in, the obligations its manager keeps open and the
// notifications its commit record still owes. The four readings describe one
// task, so an obligation and a declaration of one reading always name the same
// task.
type TaskReading struct {
	// TaskID names the task the reading describes.
	TaskID event.TaskID
	// Spec is the admission declaration of the task.
	Spec task.Spec
	// Status is the status the task currently stands in.
	Status task.Status
	// Obligations is the classified obligation reading of the task's manager.
	Obligations task.Obligations
	// Outbox lists the notifications the commit record of the task still owes,
	// each beside the delivery-layer state it stands in.
	Outbox []task.OutboxNotice
}
