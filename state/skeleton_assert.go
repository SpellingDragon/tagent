package state

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/platform/org-hot-reload.md#overview
//
// Skeleton compile-time assertions for the phase 1 state declarations.
//
// The phase 2 types that will satisfy Store and Borrow do not exist yet, so the
// placeholders below are the only implementation assertion the skeleton can
// write: the assertions pin every declared method's exact shape, every declared
// field's name and type, and every declared constant's type.
var (
	_ func(Store, context.Context, event.Address, event.InstanceID) (*Session, error) = Store.Establish

	_ func(Store, context.Context, StatePlan) (Publication, error) = Store.Publish

	_ func(Store, context.Context, event.Address) error = Store.CloseSession

	_ func(Store, context.Context, task.Notice) error = Store.AcceptNotice

	_ func(Borrow, context.Context, event.AgentID) (*BindingHandle, error) = Borrow.Borrow
)

// Every declared port name is an interface type.
var (
	_ Store  = (Store)(nil)
	_ Borrow = (Borrow)(nil)
)

// The binding's live fields keep their native framework types.
var (
	_ model.Model      = AgentBinding{}.Model
	_ []trpctool.Tool  = AgentBinding{}.Tools
	_ []plugin.Plugin  = AgentBinding{}.Plugins
	_ trpctool.ToolSet = AgentBinding{}.Set
)

// The borrowed binding handle keeps its documented field and its release method
// shape.
var (
	_ = BindingHandle{
		Binding: AgentBinding{},
	}

	_ func(*BindingHandle, context.Context) error = (*BindingHandle).Release
)

// The declared plan, binding, site and publication types keep every documented
// field.
var (
	_ = OrgPlan{
		Declaration: (*config.Declaration)(nil),
		Baseline:    "",
		Fingerprint: "",
		Changes:     []PlanChange{},
		NextPrompt:  "",
	}

	_ = PlanChange{
		Kind:    PlanChangeAdded,
		AgentID: "",
		Summary: "",
	}

	_ = StatePlan{
		Plan:   OrgPlan{},
		Agents: map[event.AgentID]AgentBinding{},
		Leases: []LeaseRef{},
	}

	_ = LeaseRef{
		Kind: "",
		Key:  "",
		Ref:  "",
	}

	_ = AgentBinding{
		Model:        nil,
		Tools:        []trpctool.Tool{},
		Plugins:      []plugin.Plugin{},
		PromptSource: PromptSource{},
		Set:          nil,
	}

	_ = PromptSource{
		Files:   []config.FileRef{},
		Version: "",
	}

	_ = Publication{
		Generation:  0,
		Fingerprint: "",
		Baseline:    "",
	}

	_ = Session{
		Address:     event.Address{},
		Instance:    "",
		Mailbox:     []MailboxEntry{},
		Consumed:    0,
		Invocations: map[event.InvocationID]InvocationProjection{},
	}

	_ = MailboxEntry{
		AcceptSeq: 0,
		Claim:     ClaimPending,
		Receipt:   ConsumeReceipt{},
	}

	_ = ConsumeReceipt{
		Completed: false,
	}

	_ = InvocationProjection{
		InvocationID: "",
		Pending:      []task.Notice{},
	}
)

// The claim-state and change-kind vocabularies stay compile-time constants of
// their declared types.
const (
	_ ClaimState = ClaimPending
	_ ClaimState = ClaimClaimed
	_ ClaimState = ClaimConsumed

	_ PlanChangeKind = PlanChangeAdded
	_ PlanChangeKind = PlanChangeUpdated
	_ PlanChangeKind = PlanChangeRemoved
)

// skeletonStore is a compile-time placeholder for the shared state face. It
// carries the stub methods that satisfy Store and holds no logic.
type skeletonStore struct{}

// Establish implements Store.Establish as a phase 2 stub.
func (skeletonStore) Establish(ctx context.Context, addr event.Address, instance event.InstanceID) (*Session, error) {
	return nil, errors.New("phase 2: not implemented")
}

// Publish implements Store.Publish as a phase 2 stub.
func (skeletonStore) Publish(ctx context.Context, plan StatePlan) (Publication, error) {
	return Publication{}, errors.New("phase 2: not implemented")
}

// CloseSession implements Store.CloseSession as a phase 2 stub.
func (skeletonStore) CloseSession(ctx context.Context, addr event.Address) error {
	return errors.New("phase 2: not implemented")
}

// AcceptNotice implements Store.AcceptNotice as a phase 2 stub.
func (skeletonStore) AcceptNotice(ctx context.Context, notice task.Notice) error {
	return errors.New("phase 2: not implemented")
}

// skeletonBorrow is a compile-time placeholder for the borrowing face. It
// carries the stub method that satisfies Borrow and holds no logic.
type skeletonBorrow struct{}

// Borrow implements Borrow.Borrow as a phase 2 stub.
func (skeletonBorrow) Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error) {
	return nil, errors.New("phase 2: not implemented")
}

// Release implements BindingHandle.Release as a phase 2 stub.
func (*BindingHandle) Release(ctx context.Context) error {
	return errors.New("phase 2: not implemented")
}

// The port interfaces are satisfied by their skeleton placeholders.
var (
	_ Store  = (*skeletonStore)(nil)
	_ Borrow = (*skeletonBorrow)(nil)
)
