// The quiesce checks of the close face: the intake of a closing host closes
// while the work the host already owes keeps its derived turns, and a close
// beside an unreachable model settles the failed answer, names it in the report,
// and lets the failure observation reach the real model request instead of
// leaving the notification owed or swallowing the cause.
package host

import (
	"context"
	"errors"
	"slices"
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
	if len(report.Failed) != 0 {
		t.Fatalf("close report names failed answers %v beside a clean close, want a clean close told from one over failures", report.Failed)
	}
	if got := deliveredCopies(sender.bodies(), followUpAnswer); got != 1 {
		t.Fatalf("delivered bodies = %q, want the one derived answer %q", sender.bodies(), followUpAnswer)
	}
}

// TestQuiesceOfflineCutReport pins the three-state exit of one close over the formal host assembly:
// - a close that names no timeout is refused by name instead of waiting
// - a close that names two disagreeing timeouts is refused by name
// - a close beside an unreachable model settles the failed follow-up with its receipt, names the failure in the report, and lets the failure observation reach a real model request
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
// the host beside a model that answers nothing: the follow-up answer fails, the
// close settles that failed answer with its consumption receipt instead of
// leaving the notification owed, names the settled failure in its report, and
// lands the failure as an observation the address reads in a real model request.
// The failure stays visible on all three faces — the report, the chain fact and
// the raw cause delivered to the user — and it never replays the notification.
func runQuiesceOfflineCutReport(t *testing.T) {
	const (
		workerBody   = "the delegated worker reports the total is 42"
		entryAnswer  = "the entry accepted the delegation"
		failureCause = "the model endpoint is unreachable"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	observationSeen := make(chan struct{})
	var workerOnce, observationOnce sync.Once
	var mu sync.Mutex
	entryCalls, followUps := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if scriptedRequestCarriesText(request, failureCause) {
			// The failure observation landed as material of a request: the model
			// really read the cause the close settled.
			observationOnce.Do(func() { close(observationSeen) })
			return "", errors.New(failureCause)
		}
		if settleDriveReadsMaterial(request, workerBody) {
			mu.Lock()
			followUps++
			mu.Unlock()
			return "", errors.New(failureCause)
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
		t.Fatalf("close beside an unreachable model = %v, want the settled failure to be a normal return", err)
	}
	report := host.CloseReport()
	if report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("close report = (%+v), want the failed answer settled and the close at rest", report)
	}
	if len(report.Unsettled) != 0 {
		t.Fatalf("close report = (%+v), want no work left owed beside the settled failure", report)
	}
	facts := host.Run().SessionFacts(addr)
	failed := settledFailureInvocationsOf(facts)
	if len(failed) == 0 {
		t.Fatalf("settled failures = %v, want the failed follow-up answer the receipt closed", failed)
	}
	if !slices.Equal(report.Failed, failed) {
		t.Fatalf("report names failed answers %v, want the settled failures %v the chain records", report.Failed, failed)
	}
	observations := failureObservationFactsOf(facts)
	if len(observations) != len(failed) {
		t.Fatalf("failure observations = %d, want one per settled failure %v", len(observations), failed)
	}
	for _, observation := range observations {
		if !strings.Contains(observation.Message.Content, failureCause) {
			t.Fatalf("failure observation = %q, want the raw cause %q the chain keeps", observation.Message.Content, failureCause)
		}
		if observation.Owner != addr || observation.Source != addr || observation.Target != addr {
			t.Fatalf("failure observation addresses = (owner %+v, source %+v, target %+v), want the driven address %+v", observation.Owner, observation.Source, observation.Target, addr)
		}
	}
	select {
	case <-observationSeen:
	default:
		t.Fatal("no real model request carried the settled failure, want the failure observation driven to its address")
	}
	mu.Lock()
	driven := followUps
	mu.Unlock()
	if driven != 1 {
		t.Fatalf("follow-up turns beside the failed answer = %d, want the one the notification drove: a failed result must not replay", driven)
	}
	if read := host.Run().Unsettled(ctx); !read.Resting() {
		t.Fatalf("rest criterion = (%+v), want the failed answer's notification counted as settled", read)
	}
	if bodies := strings.Join(sender.bodies(), "\n"); !strings.Contains(bodies, failureCause) {
		t.Fatalf("delivered bodies = %q, want the raw failure cause kept visible to the user", sender.bodies())
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
//   - the report of a close names exactly the settled failures the chain records, and the criterion owes no notification
//   - every failure the report names stands on the chain beside the observation of its cause
//   - a restart over the same log reads the same settled failure back and names it again
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestQuiesceUnsettledSingleSource(t *testing.T) {
	t.Run("report_reads_the_one_criterion", runQuiesceReportReadsOneCriterion)
	t.Run("restart_reads_the_same_criterion", runQuiesceRestartReadsSameCriterion)
}

// unconsumedScene is the one scene the close anchors read: a delegated worker
// whose notification reaches the entry chain after the caller cancels, beside a
// model that answers nothing once the delegation is out.
type unconsumedScene struct {
	host       *assembly.Host
	provider   *scriptedProvider
	addr       event.Address
	configPath string
	// releaseWorker lets the delegated worker answer.
	releaseWorker func()
	// followUpSeen closes when the notification's follow-up turn reaches the
	// provider, and releaseFollowUp lets that turn end. A close that must catch
	// the work still owed waits on the turn instead of racing the failure it
	// lands.
	followUpSeen    <-chan struct{}
	releaseFollowUp func()
}

// unconsumedSceneUnder drives one delegated notification onto the chain beside a
// model that answers nothing once the delegation is out, so the notification the
// worker owes stays unconsumed. With holdFollowUp the follow-up turn stops at
// its provider after announcing itself, so the caller reads the close while the
// notification still stands on the chain.
func unconsumedSceneUnder(t *testing.T, ctx context.Context, holdFollowUp bool) *unconsumedScene {
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
	followUpSeen := make(chan struct{})
	releaseFollowUp := make(chan struct{})
	var workerOnce, releaseWorkerOnce, followUpOnce, releaseFollowUpOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if offline.Load() {
			if holdFollowUp {
				followUpOnce.Do(func() { close(followUpSeen) })
				<-releaseFollowUp
			}
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
	releaseWorkerFn := func() { releaseWorkerOnce.Do(func() { close(releaseWorker) }) }
	releaseFollowUpFn := func() { releaseFollowUpOnce.Do(func() { close(releaseFollowUp) }) }

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	host, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		releaseWorkerFn()
		releaseFollowUpFn()
		t.Fatalf("boot the delegating host: %v", err)
	}
	t.Cleanup(releaseWorkerFn)
	t.Cleanup(releaseFollowUpFn)
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
		releaseWorkerFn()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked

	offline.Store(true)
	releaseWorkerFn()
	waitForSettle(t, "the delegated task to land its notification", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" {
				return true
			}
		}
		return false
	})
	return &unconsumedScene{
		host: host, provider: provider, addr: addr, configPath: configPath,
		releaseWorker: releaseWorkerFn, followUpSeen: followUpSeen, releaseFollowUp: releaseFollowUpFn,
	}
}

// failureObservationFactsOf reports the failure observations one session chain
// holds, in commit order: the facts the settle face lands beside the failed
// answers its receipts closed.
func failureObservationFactsOf(facts []event.Event) []event.Event {
	var observations []event.Event
	for _, fact := range facts {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyEventSummary] == "failure-observation" {
			observations = append(observations, fact)
		}
	}
	return observations
}

// settledFailureInvocationsOf reports the invocations of the failed answers the
// chain records an observation for, in commit order: each one is an answer the
// close settled on a failed terminal, named by the invocation that ended it.
func settledFailureInvocationsOf(facts []event.Event) []event.InvocationID {
	observations := failureObservationFactsOf(facts)
	failed := make([]event.InvocationID, 0, len(observations))
	for _, observation := range observations {
		invocation := event.InvocationID(observation.Meta[event.MetaPrefix+"invocation"])
		if invocation == "" {
			invocation = observation.InvocationID
		}
		if invocation != "" {
			failed = append(failed, invocation)
		}
	}
	return failed
}

// scriptedRequestCarriesText reports whether one model request carries the text
// as one of the messages it read.
func scriptedRequestCarriesText(request scriptedRequest, text string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

// runQuiesceReportReadsOneCriterion states that the report of one close names
// the same settled failures the chain records and the rest criterion reads:
// the failed follow-up is settled, the criterion owes no notification, and every
// failure the close settled is named in the report beside the observation the
// chain keeps of its cause.
func runQuiesceReportReadsOneCriterion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scene := unconsumedSceneUnder(t, ctx, false)
	defer scene.releaseWorker()
	host, addr := scene.host, scene.addr

	report, err := host.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)})
	if err != nil {
		t.Fatalf("quiesce beside the failed follow-up = %v, want the settled failure as a normal return", err)
	}
	if report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("quiesce report = (%+v), want the failed answer settled and the close at rest", report)
	}
	if len(report.Unsettled) != 0 {
		t.Fatalf("quiesce report = (%+v), want no work left owed beside the settled failure", report)
	}
	read := host.Run().Unsettled(ctx)
	if !read.Resting() {
		t.Fatalf("rest criterion = (%+v), want the failed answer's notification counted as settled", read)
	}
	facts := host.Run().SessionFacts(addr)
	failed := settledFailureInvocationsOf(facts)
	if len(failed) != 1 {
		t.Fatalf("settled failures = %v, want the one failed follow-up answer one notification drives: one notification starts one turn however often it is observed", failed)
	}
	if !slices.Equal(report.Failed, failed) {
		t.Fatalf("report names failed answers %v, want the settled failures %v the chain records", report.Failed, failed)
	}
	for _, observation := range failureObservationFactsOf(facts) {
		if !strings.Contains(observation.Message.Content, "the model endpoint is unreachable") {
			t.Fatalf("failure observation = %q, want the raw cause kept on the chain", observation.Message.Content)
		}
		if observation.Owner != addr || observation.Source != addr || observation.Target != addr {
			t.Fatalf("failure observation addresses = (owner %+v, source %+v, target %+v), want the driven address %+v", observation.Owner, observation.Source, observation.Target, addr)
		}
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close of the host = %v, want the facilities to stop", err)
	}
}

// runQuiesceRestartReadsSameCriterion states that a restart over the same log
// reads back the very criterion the first process judged: the failed answer is
// settled, the rest criterion owes no notification, and the failure the report
// named before the restart is the same one the restarted process names.
func runQuiesceRestartReadsSameCriterion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scene := unconsumedSceneUnder(t, ctx, false)
	defer scene.releaseWorker()
	host, addr, configPath := scene.host, scene.addr, scene.configPath

	if _, err := host.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("quiesce beside the failed follow-up = %v, want the settled failure as a normal return", err)
	}
	before := settledFailureInvocationsOf(host.Run().SessionFacts(addr))
	if len(before) == 0 {
		t.Fatal("the chain before the restart names no settled failure, want the failed follow-up answer")
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close of the first host = %v, want the facilities to stop", err)
	}

	restarted, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		t.Fatalf("boot over the chain the failure stands on: %v", err)
	}
	defer func() {
		if err := restarted.Close(ctx); err != nil {
			t.Fatalf("close of the restarted host: %v", err)
		}
	}()
	report, err := restarted.Quiesce(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)})
	if err != nil {
		t.Fatalf("quiesce of the restarted host = %v, want the settled failure read back as a normal return", err)
	}
	if report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("restarted quiesce report = (%+v), want the failed answer settled and the close at rest", report)
	}
	after := settledFailureInvocationsOf(restarted.Run().SessionFacts(addr))
	if !slices.Equal(before, after) {
		t.Fatalf("settled failures before the restart = %v and after = %v, want the same failed answer", before, after)
	}
	if !slices.Equal(report.Failed, after) {
		t.Fatalf("restarted report names failed answers %v, want the settled failures %v the chain records", report.Failed, after)
	}
	if read := restarted.Run().Unsettled(ctx); !read.Resting() {
		t.Fatalf("restarted rest criterion = (%+v), want the failed answer's notification counted as settled", read)
	}
}
