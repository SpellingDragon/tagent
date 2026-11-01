// The quiesce checks of the close face: the intake of a closing host closes
// while the work the host already owes keeps its derived turns, and a close
// that spends its budget beside an unreachable model still answers with what
// stands on the chain instead of an elapsed wait.
package host

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestQuiesceDerivedNotSelfLocked pins the shutter grading of one close over the formal host assembly:
// - the intake closes first, so an external turn is refused by name
// - the derived turn the host already owes is admitted inside that close and reaches its provider
// - the close reaches rest instead of spending its budget beside its own refused derivative
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceDerivedNotSelfLocked(t *testing.T) {
	t.Run("intake_refused_derived_admitted", runQuiesceDerivedNotSelfLocked)
}

// runQuiesceDerivedNotSelfLocked drives one delegated notification to its
// follow-up turn after the close of the host began: the derived turn is the
// work the closing host owes, so it is admitted, answers, and lets the close
// reach its fixed point.
func runQuiesceDerivedNotSelfLocked(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 42"
		entryAnswer    = "the entry accepted the delegation"
		followUpAnswer = "the derived turn settled inside the close"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	derivedArrived := make(chan struct{})
	var derivedOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if settleDriveReadsMaterial(request, workerBody) {
			derivedOnce.Do(func() { close(derivedArrived) })
			return scriptedAnswerOf(followUpAnswer), nil
		}
		mu.Lock()
		defer mu.Unlock()
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the delegating host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-quiesce-derived"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		invokeCancel()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked

	begun := make(chan struct{})
	closed := make(chan error, 1)
	go func() {
		close(begun)
		closed <- host.Close(ctx)
	}()
	<-begun
	time.Sleep(300 * time.Millisecond)

	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "one more request"}},
	}); !errors.Is(err, assembly.ErrHostClosed) {
		t.Fatalf("invoke after the close began = %v, want the intake refusal ErrHostClosed", err)
	}

	close(releaseWorker)
	select {
	case <-derivedArrived:
	case <-time.After(30 * time.Second):
		t.Fatalf("the derived turn the close owes never reached its provider, want the owed work admitted inside the close")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close beside the derived work = %v, want the close to reach its fixed point", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the close beside the derived work returned nothing")
	}
	report := host.CloseReport()
	if report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("close report = (%+v), want the derived work to let the close reach rest", report)
	}
	if got := deliveredCopies(sender.bodies(), followUpAnswer); got != 1 {
		t.Fatalf("delivered bodies = %q, want the one derived answer %q", sender.bodies(), followUpAnswer)
	}
}

// TestQuiesceOfflineCutReport pins the three-state exit of one close over the formal host assembly:
// - a close that names no timeout is refused by name instead of waiting
// - a close that names two disagreeing timeouts is refused by name
// - a close beside an unreachable model still terminates, answers Cut as a normal return, and names every address left on the chain
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceOfflineCutReport(t *testing.T) {
	t.Run("zero_timeout_refused", runQuiesceZeroTimeoutRefused)
	t.Run("offline_cut_names_each_address", runQuiesceOfflineCutReport)
}

// quiesceHostUnder boots the two-agent host the quiesce checks close: the entry
// delegates to one worker, and the caller's endpoint scripts both turns.
func quiesceHostUnder(t *testing.T, provider *scriptedProvider, sender *deliveryRecorder) *assembly.Host {
	t.Helper()
	wakeEnv(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the delegating host: %v", err)
	}
	return host
}

// runQuiesceZeroTimeoutRefused refuses a close that names no budget: an
// unbounded close waits forever beside work that never converges, so the
// request is refused by name before any intake closes.
func runQuiesceZeroTimeoutRefused(t *testing.T) {
	provider := newScriptedProvider(t)
	host := quiesceHostUnder(t, provider, &deliveryRecorder{})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	report, err := host.Quiesce(ctx, agent.CloseRequest{})
	if !errors.Is(err, agent.ErrCloseTimeoutMissing) {
		t.Fatalf("quiesce without a timeout = (%+v, %v), want the refusal ErrCloseTimeoutMissing", report, err)
	}
	conflicting := agent.CloseRequest{
		Timeout:  time.Now().Add(time.Second),
		Deadline: time.Now().Add(time.Minute),
	}
	if _, err := host.Quiesce(ctx, conflicting); !errors.Is(err, agent.ErrCloseTimeoutConflict) {
		t.Fatalf("quiesce naming two disagreeing timeouts = %v, want the refusal ErrCloseTimeoutConflict", err)
	}
	if report, err := host.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil || report.Outcome != agent.CloseQuiesced {
		t.Fatalf("quiesce of an idle host = (%+v, %v), want the idle host at rest", report, err)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close after a bare quiesce = %v, want the facilities that quiesce left running to stop", err)
	}
}

// runQuiesceOfflineCutReport drives one notification onto the chain and closes
// the host beside a model that answers nothing: the close spends its budget,
// returns Cut as a normal result instead of an elapsed wait, and names the
// unconsumed notification beside the address it stands at.
func runQuiesceOfflineCutReport(t *testing.T) {
	const (
		workerBody  = "the delegated worker reports the total is 42"
		entryAnswer = "the entry accepted the delegation"
	)
	provider := newScriptedProvider(t)
	var offline atomic.Bool
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if offline.Load() {
			return "", errors.New("the model endpoint is unreachable")
		}
		mu.Lock()
		defer mu.Unlock()
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}

	sender := &deliveryRecorder{}
	host := quiesceHostUnder(t, provider, sender)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-quiesce-offline"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		invokeCancel()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked

	offline.Store(true)
	close(releaseWorker)
	waitForSettle(t, "the delegated task to land its notification", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" {
				return true
			}
		}
		return false
	})

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close beside an unreachable model = %v, want the cut to be a normal return", err)
	}
	report := host.CloseReport()
	if report.Outcome != agent.CloseCut {
		t.Fatalf("close report = (%+v), want Cut beside the unconsumed notification", report)
	}
	if report.Drained {
		t.Fatalf("close report = (%+v), want no rest claimed beside work left on the chain", report)
	}
	if len(report.Unsettled) == 0 {
		t.Fatal("the cut report names nothing, want every address the close left on the chain")
	}
	namedNotice := false
	for _, entry := range report.Unsettled {
		if entry.Address.SessionID == "" {
			t.Fatalf("unsettled entry names no address: %+v", entry)
		}
		if entry.Reason == "" {
			t.Fatalf("unsettled entry names no reason: %+v", entry)
		}
		if entry.FactKey != "" {
			namedNotice = true
		}
	}
	if !namedNotice {
		t.Fatalf("unsettled = %+v, want the unconsumed notification named beside its fact", report.Unsettled)
	}
}

// TestQuiesceRejectedCutAllowed pins the refusal of one notification beside a closed session:
//   - the close of a run beside a model still in flight answers Cut as a normal return
//   - the notification the released worker owes lands as a refusal the account keeps, not as an owed delivery
//   - a restart over the same log reads that refusal back and counts the notification as answered
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceRejectedCutAllowed(t *testing.T) {
	t.Run("rejected_notice_cuts_and_lands", runQuiesceRejectedCutAllowed)
	t.Run("rejection_visible_after_restart", runQuiesceRejectionVisibleAfterRestart)
}

// rejectedScene is one delegation whose terminal notification is owed to the
// session the close of the run already closed.
type rejectedScene struct {
	host       *assembly.Host
	configPath string
	addr       event.Address
	sender     *deliveryRecorder
	release    func()
}

// rejectedSceneUnder drives one delegated turn to a parked worker and closes the
// run's own side for the entry session while the worker is still in flight, so
// the terminal notification the worker lands afterwards is aimed at a session
// whose side is already closed.
func rejectedSceneUnder(t *testing.T, ctx context.Context) *rejectedScene {
	t.Helper()
	wakeEnv(t)

	const (
		workerBody  = "the delegated worker reports the total is 42"
		entryAnswer = "the entry accepted the delegation"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce, releaseOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		mu.Lock()
		defer mu.Unlock()
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}
	release := func() { releaseOnce.Do(func() { close(releaseWorker) }) }

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		release()
		t.Fatalf("boot the delegating host: %v", err)
	}
	t.Cleanup(func() { release() })
	addr := event.Address{AgentID: "user", SessionID: "session-quiesce-rejected"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		invokeCancel()
		release()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked

	return &rejectedScene{host: host, configPath: configPath, addr: addr, sender: sender, release: release}
}

// closeBesideWorker spends the budget of the run's own close while the worker is
// still in flight: the close answers Cut as a normal return, because the work it
// leaves stands on the chain for a restart to resume.
func (s *rejectedScene) closeBesideWorker(t *testing.T, ctx context.Context) {
	t.Helper()
	report, err := s.host.Run().Close(ctx, agent.CloseRequest{
		Address: s.addr,
		Timeout: time.Now().Add(300 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("close of the run beside the model in flight = %v, want Cut as a normal return", err)
	}
	if report.Outcome != agent.CloseCut || report.Drained {
		t.Fatalf("close report = (%+v), want Cut beside the work the close left on the chain", report)
	}
}

// awaitRefusal waits for the account to answer the notification the close left
// aimed at its own closed session, and reports the refusal it read.
func (s *rejectedScene) awaitRefusal(t *testing.T, ctx context.Context) state.Rejection {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last []state.Rejection
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			t.Fatalf("the wait for the refusal of %s ended with %v", s.addr, err)
		}
		last = s.host.Rejections()
		for _, refusal := range last {
			if refusal.Target == s.addr {
				return refusal
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refusals of %s = (%+v) after 30s, want the refusal of the notification left aimed at the closed session", s.addr, last)
	return state.Rejection{}
}

// awaitRest waits for the rest criterion of one run to answer at rest, so the
// anchor states the criterion the account is judged by and not one read beside a
// turn that still flies.
func awaitRest(t *testing.T, ctx context.Context, run interface {
	Unsettled(context.Context) task.Unsettled
}) task.Unsettled {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var read task.Unsettled
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			t.Fatalf("the wait for rest ended with %v", err)
		}
		read = run.Unsettled(ctx)
		if read.Resting() {
			return read
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("rest criterion = (%+v) after 30s, want the refused notification counted as answered", read)
	return read
}

// runQuiesceRejectedCutAllowed drives one refusal onto the account and states
// that the refusal is a consumption result: the notification stops standing as
// an obligation the rest criterion waits on.
func runQuiesceRejectedCutAllowed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scene := rejectedSceneUnder(t, ctx)
	scene.closeBesideWorker(t, ctx)
	scene.release()

	refusal := scene.awaitRefusal(t, ctx)
	if refusal.Notice.TaskID == "" || refusal.Notice.FactKey() == "" || refusal.Reason == "" {
		t.Fatalf("refusal = (%+v), want the notification it answers beside the reason the target gave", refusal)
	}
	if !strings.Contains(refusal.Reason, "closed") {
		t.Fatalf("refusal reason = %q, want the named refusal of the closed session side", refusal.Reason)
	}
	if fact := refusal.Feedback(); fact.Type != event.TypeFeedback {
		t.Fatalf("refusal fact type = %q, want the feedback fact %q the refusal is committed as", fact.Type, event.TypeFeedback)
	}
	awaitRest(t, ctx, scene.host.Run())
	if err := scene.host.Close(ctx); err != nil {
		t.Fatalf("close of the host after the refusal = %v, want the facilities to stop", err)
	}
}

// runQuiesceRejectionVisibleAfterRestart restarts over the same log and states
// that the refusal is read back there and that the notification it answers is
// not counted as an unconsumed notification.
func runQuiesceRejectionVisibleAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scene := rejectedSceneUnder(t, ctx)
	scene.closeBesideWorker(t, ctx)
	scene.release()
	refusal := scene.awaitRefusal(t, ctx)
	awaitRest(t, ctx, scene.host.Run())
	if err := scene.host.Close(ctx); err != nil {
		t.Fatalf("close of the first host = %v, want the facilities to stop", err)
	}

	restarted, err := assembly.Boot(scene.configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		t.Fatalf("boot over the log the refusal landed on: %v", err)
	}
	defer func() {
		if err := restarted.Close(ctx); err != nil {
			t.Fatalf("close of the restarted host: %v", err)
		}
	}()
	awaitRest(t, ctx, restarted.Run())
	restored := false
	for _, again := range restarted.Rejections() {
		if again.Notice.FactKey() != refusal.Notice.FactKey() {
			continue
		}
		restored = true
		if again.Reason != refusal.Reason {
			t.Fatalf("refusal read back = (%+v), want the reason %q the first host recorded", again, refusal.Reason)
		}
	}
	if !restored {
		t.Fatalf("refusals after the restart = (%+v), want %q read back off the same log", restarted.Rejections(), refusal.Notice.FactKey())
	}
}

// TestQuiesceUnsettledSingleSource pins the one criterion the close, the report and a restart read:
//   - the per-address report of a cut close names exactly the unconsumed notifications the criterion reads
//   - every piece of unfinished work the criterion names stands in that report beside it
//   - a restart over the same log reads the same unconsumed notifications back
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceUnsettledSingleSource(t *testing.T) {
	t.Run("report_reads_the_one_criterion", runQuiesceReportReadsOneCriterion)
	t.Run("restart_reads_the_same_criterion", runQuiesceRestartReadsSameCriterion)
}

// unconsumedSceneUnder drives one delegated notification onto the chain beside a
// model that answers nothing once the delegation is out, so the notification the
// worker owes stays unconsumed.
func unconsumedSceneUnder(t *testing.T, ctx context.Context) (*assembly.Host, *scriptedProvider, event.Address, func(), string) {
	t.Helper()
	wakeEnv(t)

	const (
		workerBody  = "the delegated worker reports the total is 42"
		entryAnswer = "the entry accepted the delegation"
	)
	provider := newScriptedProvider(t)
	var offline atomic.Bool
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce, releaseOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if offline.Load() {
			return "", errors.New("the model endpoint is unreachable")
		}
		mu.Lock()
		defer mu.Unlock()
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}
	release := func() { releaseOnce.Do(func() { close(releaseWorker) }) }

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	host, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		release()
		t.Fatalf("boot the delegating host: %v", err)
	}
	t.Cleanup(release)
	addr := event.Address{AgentID: "user", SessionID: "session-quiesce-single-source"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		invokeCancel()
		release()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked

	offline.Store(true)
	release()
	waitForSettle(t, "the delegated task to land its notification", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" {
				return true
			}
		}
		return false
	})
	return host, provider, addr, release, configPath
}

// noticeFactKeysOf reports the fact keys of one set of unconsumed notifications,
// sorted, so two reads of the same account compare as one set.
func noticeFactKeysOf(notices []task.UnsettledNotice) []string {
	keys := make([]string, 0, len(notices))
	for _, notice := range notices {
		keys = append(keys, notice.Notice.FactKey())
	}
	sort.Strings(keys)
	return keys
}

// runQuiesceReportReadsOneCriterion states that the per-address report of one
// cut close and the rest criterion name the same unconsumed notifications.
func runQuiesceReportReadsOneCriterion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	host, _, _, release, _ := unconsumedSceneUnder(t, ctx)
	defer release()

	report, err := host.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(400 * time.Millisecond)})
	if err != nil {
		t.Fatalf("quiesce beside the unconsumed notification = %v, want Cut as a normal return", err)
	}
	if report.Outcome != agent.CloseCut || report.Drained {
		t.Fatalf("quiesce report = (%+v), want Cut beside the notification left unconsumed", report)
	}
	read := host.Run().Unsettled(ctx)
	fromCriterion := noticeFactKeysOf(read.Notices)
	if len(fromCriterion) == 0 {
		t.Fatalf("rest criterion = (%+v), want the unconsumed notification named", read)
	}
	fromReport := make([]string, 0, len(report.Unsettled))
	for _, entry := range report.Unsettled {
		if entry.FactKey != "" {
			fromReport = append(fromReport, entry.FactKey)
		}
	}
	sort.Strings(fromReport)
	if !slices.Equal(fromCriterion, fromReport) {
		t.Fatalf("report names %q while the criterion reads %q, want one account behind the two faces", fromReport, fromCriterion)
	}
	named := make(map[event.TaskID]bool, len(report.Unsettled))
	for _, entry := range report.Unsettled {
		named[entry.TaskID] = true
	}
	for _, work := range read.Work {
		if !named[work.TaskID] {
			t.Fatalf("work %q stands unfinished in the criterion and nowhere in the report (%+v)", work.TaskID, report.Unsettled)
		}
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close of the host = %v, want the facilities to stop", err)
	}
}

// runQuiesceRestartReadsSameCriterion states that a restart over the same log
// reads back the very unconsumed notifications the first process judged.
func runQuiesceRestartReadsSameCriterion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	host, _, _, release, configPath := unconsumedSceneUnder(t, ctx)
	defer release()

	if _, err := host.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(400 * time.Millisecond)}); err != nil {
		t.Fatalf("quiesce beside the unconsumed notification = %v, want Cut as a normal return", err)
	}
	before := noticeFactKeysOf(host.Run().Unsettled(ctx).Notices)
	if len(before) == 0 {
		t.Fatal("rest criterion before the restart names no unconsumed notification, want the one the worker owes")
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close of the first host = %v, want the facilities to stop", err)
	}

	restarted, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		t.Fatalf("boot over the chain the notification stands on: %v", err)
	}
	defer func() {
		if err := restarted.Close(ctx); err != nil {
			t.Fatalf("close of the restarted host: %v", err)
		}
	}()
	after := noticeFactKeysOf(restarted.Run().Unsettled(ctx).Notices)
	if !slices.Equal(before, after) {
		t.Fatalf("criterion before the restart = %q and after = %q, want the same unconsumed notification", before, after)
	}
}
