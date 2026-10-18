package state

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/task"

	"trpc.group/trpc-go/trpc-agent-go/skill"
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

	_ func(Borrow, context.Context, BindingRef) (*BindingHandle, error) = Borrow.BorrowDerived
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

// The binding reference keeps its documented fields, and the borrowed binding
// handle keeps its documented fields and its release method shape.
var (
	_ = BindingRef{
		AgentID:    "",
		InstanceID: "",
		Generation: 0,
	}

	_ = BindingHandle{
		Binding: AgentBinding{},
		Ref:     BindingRef{},
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
		Notice:       task.Notice{},
		EventID:      "",
		InvocationID: "",
		AcceptSeq:    0,
		Claim:        ClaimPending,
		Receipt:      ConsumeReceipt{},
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

// BorrowDerived implements Borrow.BorrowDerived as a phase 2 stub.
func (skeletonBorrow) BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error) {
	return nil, errors.New("phase 2: not implemented")
}

// The publication entry keeps its documented shapes: the identity one
// publication assigns, the dispositions the publish precondition contract
// records, and the self-contained record the publication recorder commits.
var (
	_ func(*Bindings, PublishContract, Governance, PublicationRecorder) *Publisher = NewPublisher

	_ func(*Publisher, context.Context, Candidate) (PublishOutcome, error) = (*Publisher).Publish

	_ func(*Publisher, context.Context, Candidate) (PublishOutcome, error) = (*Publisher).Rollback

	_ func(*Publisher) Publication = (*Publisher).Current

	_ func(*Publisher) string = (*Publisher).Baseline

	_ func(*Publisher) (OrgPlan, bool) = (*Publisher).RollbackConfig

	_ func(Governance, context.Context, StatePlan) (map[CheckName]CheckAnswer, error) = Governance.Check

	_ func(PublicationRecorder, context.Context, PublicationRecord) error = PublicationRecorder.CommitPublication
)

// The publication vocabulary and record shapes keep every documented field.
var (
	_ Generation = Generation(0)
	_ CheckName  = CheckName("")

	_ = CheckAnswer{
		Name:     "",
		Verdict:  CheckUnanswered,
		Reason:   "",
		Scope:    "",
		Evidence: []string{},
	}

	_ = PublishContract{
		Required: []CheckName{},
	}

	_ = PublicationRecord{
		Generation:     0,
		Fingerprint:    "",
		Result:         PublishUnchanged,
		RollbackSource: 0,
		Checks:         []CheckAnswer{},
	}

	_ = Candidate{
		Plan:      StatePlan{},
		Resources: (*Leases)(nil),
	}

	_ = PublishOutcome{
		Publication: Publication{},
		Result:      PublishUnchanged,
	}
)

// The verdict and result vocabularies stay compile-time constants of their
// declared types, and the publication faces are satisfied by the entry.
const (
	_ CheckVerdict = CheckUnanswered
	_ CheckVerdict = CheckPass
	_ CheckVerdict = CheckReject
	_ CheckVerdict = CheckExempt

	_ PublishResult = PublishUnchanged
	_ PublishResult = PublishApplied
	_ PublishResult = PublishRolledBack
)

var (
	_ Governance          = (Governance)(nil)
	_ PublicationRecorder = (PublicationRecorder)(nil)
	_ Borrow              = (*Publisher)(nil)
)

// The port interfaces are satisfied by their skeleton placeholders.
var (
	_ Store  = (*skeletonStore)(nil)
	_ Borrow = (*skeletonBorrow)(nil)
)

// The durable commit face keeps its documented shapes: the byte-level fact
// log, the durable carrier behind the task commit port and the owner
// resolution over the current orchestration binding.
var (
	_ FactLog = (FactLog)(nil)

	_ func(FactLog, task.UncertainProbe) (*DurableCommit, error) = NewDurableCommit

	_ func(*DurableCommit, context.Context, task.CommitRecord) (task.CommitResult, error) = (*DurableCommit).Commit
	_ func(*DurableCommit, context.Context, event.TaskID, task.Spec) error                = (*DurableCommit).Declare
	_ func(*DurableCommit, context.Context, task.Notice) (task.DeliveryAcceptance, error) = (*DurableCommit).Accept
	_ func(*DurableCommit, context.Context, event.TaskID) ([]task.TaskRecord, error)      = (*DurableCommit).Records

	_ task.OwnerBinding              = (*BindingOwners)(nil)
	_ func(*Bindings) *BindingOwners = NewBindingOwners

	_ error = ErrNoticeNotOwed
	_ error = ErrSpecConflict
)

// The heat matrix, the file source and the failure cause chain keep their
// documented shapes: the routing face beside its channel and dimension
// vocabularies, the request-boundary file fixation, and the cause link beside
// its chain walker.
var (
	_ func(Change) (Channel, error) = Route

	_ func(*Router, context.Context, PromptSource) (FileSnapshot, error)        = (*Router).Read
	_ func(*Router, context.Context, Change, Candidate) (PublishOutcome, error) = (*Router).Commit
	_ func(*Router, context.Context, Change, Candidate) (PublishOutcome, error) = (*Router).Rollback
	_ func(*FileSource, *Publisher) *Router                                     = NewRouter

	_ func(string, skill.Repository) *FileSource                             = NewFileSource
	_ func(*FileSource, context.Context, PromptSource) (FileSnapshot, error) = (*FileSource).Fixate

	_ func(string, int, string, error) *Cause = NewCause
	_ func(error) []*Cause                    = CauseChain
)

// The channel and dimension vocabularies stay compile-time constants of their
// declared types, and the matrix rows keep every documented field.
const (
	_ Channel = ChannelFP
	_ Channel = ChannelSRC
	_ Channel = ChannelFile
	_ Channel = ChannelRestart

	_ Dimension = DimensionModel
	_ Dimension = DimensionTools
	_ Dimension = DimensionPlugins
	_ Dimension = DimensionTaskPolicy
	_ Dimension = DimensionResourceRefs
	_ Dimension = DimensionMemory
	_ Dimension = DimensionPromptBody
	_ Dimension = DimensionSkillBody
	_ Dimension = DimensionRoots
	_ Dimension = DimensionEndpoints
)

var (
	_ func(Channel) string = Channel.String

	_ = ChannelRow{
		Dimension: "",
		Channel:   ChannelFP,
		Evidence:  "",
	}

	_ = Change{
		Dimension: "",
		Via:       ChannelFP,
		Source:    "",
		Line:      0,
	}

	_ = FixedFile{
		Ref:     config.FileRef{},
		Content: "",
	}

	_ = FileSnapshot{
		Files:   []FixedFile{},
		Version: "",
	}

	// The tool resources keep the registration record fields and the skill
	// load state fields of the binding's tool-set slot.
	_ = ToolResource{
		Name:   "",
		Kind:   ToolKindFile,
		Source: "",
	}

	_ = ToolKindSkill
	_ = ToolKindFunction

	_ = SkillLoad{
		Skill:   "",
		Agent:   "",
		Loaded:  false,
		Version: "",
		Body:    "",
		Reason:  "",
	}

	_ trpctool.ToolSet = (*ToolResources)(nil)

	_ func(*ToolResources, context.Context) []trpctool.Tool = (*ToolResources).Tools

	_ func(*ToolResources) error = (*ToolResources).Close

	_ func(*ToolResources, string, string, string, func(string) (SkillLoad, error)) (SkillLoad, error) = (*ToolResources).FixateSkillLoad

	_ error = ErrSkillReadMissing

	_ error = ErrToolSetClosed

	_ = Cause{
		Source: "",
		Line:   0,
		Reason: "",
		Err:    (error)(nil),
	}

	_ func(*Cause) error = (*Cause).Unwrap
)

// The routing and file-face refusals stay named errors of the chain they are
// matched through.
var (
	_ error = ErrDimensionUnknown
	_ error = ErrChannelMismatch
	_ error = ErrStructuralViaFile
	_ error = ErrRestartRefused
	_ error = ErrRouterFileSourceMissing
	_ error = ErrRouterGateMissing
	_ error = ErrFileRootMissing
	_ error = ErrSkillRepositoryMissing
	_ error = ErrFileKindUnknown
)

// The archive migration face keeps its documented shapes: the import report
// beside its named gaps, the rollback accounting, the structural ports the two
// entry points run over, and the entry points themselves.
var (
	_ = MigrationReport{SourceDigest: "", Tasks: []MigratedTask{}, Originals: []MigratedOriginal{}, Gaps: []MigrationGap{}}
	_ = MigratedTask{TaskID: "", Source: "", Status: task.StatusUnknown, MutationID: ""}
	_ = MigratedOriginal{Key: "", Source: "", Digest: "", Length: 0}
	_ = MigrationGap{Kind: GapMissingExternalFile, Name: "", Detail: ""}
	_ = RollbackReport{SourceDigest: "", Items: []RollbackItem{}}
	_ = RollbackItem{Name: "", Present: false, Verified: false}

	_ GapKind = GapMissingExternalFile
	_ GapKind = GapUnrecoverableTask

	_ OriginalMigrator  = (OriginalMigrator)(nil)
	_ OriginalReader    = (OriginalReader)(nil)
	_ ArchiveTaskWriter = (ArchiveTaskWriter)(nil)
	_ ArchiveTaskReader = (ArchiveTaskReader)(nil)

	_ func(context.Context, string, ArchiveTaskWriter, OriginalMigrator) (MigrationReport, error) = MigrateArchive
	_ func(context.Context, string, ArchiveTaskReader, OriginalReader) (RollbackReport, error)    = RollbackArchive
	_ func(string) (string, error)                                                                = ArchiveDigest

	_ error = ErrArchiveUnreadable
)
