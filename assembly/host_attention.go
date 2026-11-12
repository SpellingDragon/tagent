package assembly

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// hostReading is the run's read port beside the attention read face: the three
// readings of the diagnostic read together with the attention questions the
// owners still hold as questions, so the projection gathers its ObjectAttention
// dimension from the very port the six dimensions beside it come from. The face
// is derived at read time and records nothing of its own:
//
// - an approval question is the presence the approval book answers, judged against the validity the registration was filed with
// - a notification question is a notification nobody consumed, judged against the validity its landing fact plus the default validity name
// - a summary question is a reflection delivery the target session has not answered with an output of its own yet
// - the work end clears the derived presence once the close drained every obligation, and a close that left one open clears nothing
//
// 红线清单（4 条）:
// MUST 每次调用现读：本题面不缓存任何问题、不记录任何事实
// MUST 只对链上真实存在的问题读链；notice 与 summary 的在场是读时推导，不得补造 question 或 clearance 事实
// MUST 在排空未完成时保留全部在场：work-end 清除以排空后的状态为唯一判据
// MUST NOT 改写任何 owner 的事实、相位或开关：本题面只读
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
type hostReading struct {
	runReading
	approvals *approval.ApprovalManager
}

// The run's read port answers every dimension of the inventory beside the
// attention dimension the projection derives its ObjectAttention objects from.
var (
	_ query.Reading          = hostReading{}
	_ query.AttentionReading = hostReading{}
)

const (
	// attentionNoticeValidity is the validity a notification's attention reading
	// is judged against when the composition names none: a notification nobody
	// consumed stops being present once this much time passed since the fact it
	// points at landed, and the ObjectNotice dimension keeps reporting its phase
	// beside it unchanged.
	attentionNoticeValidity = 30 * time.Minute
	// attentionTargetAddressSep joins the two halves of the thread or person an
	// attention target names, so one target reads as "agent/session" wherever a
	// question is put to a session.
	attentionTargetAddressSep = "/"
)

// attentionNow is the clock the attention read face judges a notification's
// validity with. It reads the wall clock unless a test injects its own through
// SetAttentionClock, so a validity window is driven deterministically instead of
// slept through.
var attentionNow = time.Now

// SetAttentionClock replaces the clock the attention read face judges a
// notification's validity with and returns the restore of the clock in effect. A
// nil clock restores the wall clock, so a caller that names none reads one time
// axis with every other read of this package.
func SetAttentionClock(now func() time.Time) (restore func()) {
	previous := attentionNow
	if now == nil {
		now = time.Now
	}
	attentionNow = now
	return func() { attentionNow = previous }
}

// AttentionQuestions reads the questions the owners still hold as questions at
// read time: the approval presence of the book, the notifications nobody
// consumed within their validity and the reflection deliveries no output of the
// target session answered. A run whose close drained every obligation derives no
// presence at all, and a close that left an obligation open keeps every question
// it read before it, so the work-end clearing is judged by the post-drain state
// alone.
func (h hostReading) AttentionQuestions(ctx context.Context) ([]task.AttentionQuestion, error) {
	ended, drained, err := h.attentionWorkEnd(ctx)
	if err != nil {
		return nil, err
	}
	questions, err := h.approvalQuestions(ctx, ended && drained)
	if err != nil {
		return nil, err
	}
	if ended && drained {
		return questions, nil
	}
	notices, err := h.noticeQuestions(ctx)
	if err != nil {
		return nil, err
	}
	questions = append(questions, notices...)
	return append(questions, h.summaryQuestions()...), nil
}

// attentionWorkEnd reports whether the close of this run began and whether it
// drained every obligation it owed. The drain is judged the way the close itself
// judges it: every task the commit port lists answers no open obligation, so a
// process that stopped before its drain settled one clears nothing.
func (h hostReading) attentionWorkEnd(ctx context.Context) (ended, drained bool, err error) {
	run := h.run
	if run == nil {
		return false, false, nil
	}
	run.mu.Lock()
	closing := run.closing
	run.mu.Unlock()
	if !closing {
		return false, false, nil
	}
	for _, id := range run.commit.TaskIDs() {
		obligations, err := run.manager.Obligations(ctx, id)
		if err != nil {
			if errors.Is(err, task.ErrTaskNotFound) {
				continue
			}
			return true, false, err
		}
		if len(obligations.Open) > 0 {
			return true, false, nil
		}
	}
	return true, true, nil
}

// approvalQuestions reads the approval presence the book answers: the questions
// the attention chain still holds as questions, judged against the validity the
// registration was filed with so an expired request is no presence of this
// dimension. A cleared work end reports no approval question and leaves every
// fact the chain holds untouched.
func (h hostReading) approvalQuestions(ctx context.Context, cleared bool) ([]task.AttentionQuestion, error) {
	if cleared || h.approvals == nil {
		return nil, nil
	}
	present := h.approvals.AttentionPresence(ctx)
	questions := make([]task.AttentionQuestion, 0, len(present))
	for _, question := range present {
		if question.Expired {
			continue
		}
		questions = append(questions, question)
	}
	return questions, nil
}

// noticeQuestions reads the notifications the owners still hang on: a
// notification the commit record of its task still owes, and one accepted into a
// mailbox nobody consumed. A notification whose entry completed or was consumed
// is out of the presence, and one whose validity ran out since its landing fact
// landed is out of it too. The phase the ObjectNotice dimension reports is read
// from the same owner facts and is never changed here.
func (h hostReading) noticeQuestions(ctx context.Context) ([]task.AttentionQuestion, error) {
	tasks, err := h.runReading.Tasks(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := h.runReading.Sites(ctx)
	if err != nil {
		return nil, err
	}
	hanging := make(map[task.Notice]event.Address, len(tasks))
	for _, reading := range tasks {
		for _, owed := range reading.Outbox {
			hanging[owed.Notice.Notice] = owed.Notice.Target
		}
	}
	for _, site := range sites {
		for _, input := range site.Session.Mailbox {
			if input.Receipt.Completed || input.Claim == state.ClaimConsumed {
				delete(hanging, input.Notice)
				continue
			}
			if _, held := hanging[input.Notice]; !held {
				hanging[input.Notice] = site.Session.Address
			}
		}
	}
	byFact, byTask := h.attentionNoticeStamps()
	now := attentionNow().UnixMilli()
	questions := make([]task.AttentionQuestion, 0, len(hanging))
	for notice, target := range hanging {
		expires := int64(0)
		stamp, dated := byFact[notice.Event]
		if !dated {
			stamp, dated = byTask[notice.TaskID]
		}
		if dated {
			expires = stamp.Add(attentionNoticeValidity).UnixMilli()
		}
		if expires > 0 && now >= expires {
			continue
		}
		questions = append(questions, task.AttentionQuestion{
			Kind:      task.AttentionKindNotice,
			Target:    attentionTargetOf(target),
			Lineage:   noticeAttentionLineage(notice),
			ExpiresMs: expires,
		})
	}
	return questions, nil
}

// summaryQuestions reads the reflection deliveries of this run's live chains: a
// delivery the target session answered with an output of its own landed after it
// is read as consumed, and every other delivery stays present. The delivery path
// is untouched — the fact the reflection tool committed is the whole evidence —
// so this reading adds no fact and no phase to the chain it reads.
func (h hostReading) summaryQuestions() []task.AttentionQuestion {
	run := h.run
	if run == nil {
		return nil
	}
	run.mu.Lock()
	order := append([]event.EventID(nil), run.factOrder...)
	facts := make(map[event.EventID]event.Event, len(run.facts))
	for key, evt := range run.facts {
		facts[key] = evt
	}
	run.mu.Unlock()
	questions := make([]task.AttentionQuestion, 0, 2)
	for at, key := range order {
		evt, held := facts[key]
		if !held || !isReflectionDelivery(evt) {
			continue
		}
		if summaryAnswered(order[at+1:], facts, sessionOf(evt)) {
			continue
		}
		questions = append(questions, task.AttentionQuestion{
			Kind:    task.AttentionKindSummary,
			Target:  attentionTargetOf(evt.Owner),
			Lineage: "event=" + string(evt.Key),
		})
	}
	return questions
}

// attentionNoticeStamps answers the landing time of every notification by the
// two identities its owners stamp it with: the fact it points at, and the settle
// facts its task landed when the notification was delivered - the delivery fact
// the settle committed, or the request material a follow-up turn assembled off
// it. A notification no live fact dates keeps no validity: it stays present
// rather than being dropped on a clock this read started.
func (h hostReading) attentionNoticeStamps() (byFact map[event.EventID]time.Time, byTask map[event.TaskID]time.Time) {
	run := h.run
	if run == nil {
		return nil, nil
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	byFact = make(map[event.EventID]time.Time, len(run.facts))
	byTask = make(map[event.TaskID]time.Time, len(run.facts))
	for key, evt := range run.facts {
		byFact[key] = evt.Timestamp
		settled := evt.Type == event.TypeTaskSettled ||
			(evt.Type == event.TypeFeedback && evt.Meta[event.MetaKeyTriggerSource] == "task")
		if !settled {
			continue
		}
		taskID := noticeTaskIDOf(evt)
		if taskID == "" {
			continue
		}
		if held, stamped := byTask[taskID]; stamped && !evt.Timestamp.Before(held) {
			continue
		}
		byTask[taskID] = evt.Timestamp
	}
	return byFact, byTask
}

// summaryAnswered reports whether a session answered one reflection delivery
// with an output of its own landed after the delivery: the wide clearance point
// of the summary kind, which reads the delivery as read once the target session
// produced its next output.
func summaryAnswered(rest []event.EventID, facts map[event.EventID]event.Event, session event.SessionID) bool {
	if session == "" {
		return false
	}
	for _, key := range rest {
		evt, held := facts[key]
		if !held || evt.Type != event.TypeAgentOutput {
			continue
		}
		if sessionOf(evt) == session {
			return true
		}
	}
	return false
}

// isReflectionDelivery reports whether one fact is a reflection summary
// delivered into a session chain: the external input the delivery tool commits
// under the reflection lineage, into a complete target address.
func isReflectionDelivery(evt event.Event) bool {
	return evt.Type == event.TypeExternalInput &&
		evt.Meta[event.MetaKeyTriggerSource] == meditation.LineageReflection &&
		evt.Owner.IsComplete()
}

// attentionTargetOf renders the thread or person one question is put to as the
// one target form this dimension names: the agent and the session of the address
// it belongs to.
func attentionTargetOf(addr event.Address) string {
	return string(addr.AgentID) + attentionTargetAddressSep + string(addr.SessionID)
}

// noticeAttentionLineage answers the lineage one notification question came
// from: the stable notification identity the task, the round, the category and
// the sequence inside the category name, written the way the attention family
// states a lineage. The identity is the one the notification dimension already
// merges under, so the two dimensions never name one notification by two
// lineages.
func noticeAttentionLineage(notice task.Notice) string {
	return "task=" + string(notice.TaskID) +
		"\x00round=" + strconv.FormatInt(notice.Round, 10) +
		"\x00kind=" + string(notice.Kind) +
		"\x00seq=" + strconv.FormatInt(notice.Seq, 10)
}
