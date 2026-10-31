// The unified wake checks of the settle face: the mailbox answers whether work
// waits, and one entry there is the only account a follow-up turn is started
// from. A repeated fire of one notification starts no second turn, a notice no
// turn consumed survives a restart and resumes the turn it owes, and a stop
// keeps what no turn consumed instead of clearing it.
package host

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestSettleWakeUnified pins the one-work-account semantics of the settle driver over the formal host assembly.
//   - a repeated fire of one delivered settle starts exactly one follow-up turn
//   - an unconsumed notice is resumed by the next boot without a user input
//   - a stop keeps the unconsumed notices of the mailbox instead of clearing them
//   - a consumed notice never replays across a further restart (guard)
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestSettleWakeUnified(t *testing.T) {
	t.Run("a repeated fire starts one follow-up turn", runWakeDuplicateFire)
	t.Run("an unconsumed notice resumes across a restart", runWakeRestartResume)
	t.Run("a stop keeps what no turn consumed", runWakeStopRetains)
	t.Run("a consumed notice never replays", runWakeConsumedNoReplay)
}

// wakeEnv sets the host environment every wake check boots under.
func wakeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
}

// wakeAgentSections renders the two-agent surface the wake checks delegate
// under: the entry agent delegates to one worker beside its own model.
const wakeAgentSections = "  user:\n" +
	"    model: model-entry\n" +
	"    max_tokens: 32000\n" +
	"    tools:\n" +
	"      - { kind: agent, agent: worker }\n" +
	"  worker:\n" +
	"    model: model-worker\n"

// wakeDelegate renders the scripted decision that delegates one input to the
// worker, stamped with the call identity the answer table dispatches on.
func wakeDelegate(callID, input string) string {
	return settleToolCallAnswer("worker", callID, `{"input":"`+input+`"}`)
}

// runWakeDuplicateFire drives one delegated settle and fires the delivered
// notification twice: once by its own delivery observation and once by a
// repeated commit of the very same settle fact, which is an idempotent hit that
// still runs its After pass. Exactly one follow-up turn may answer the
// provider; the mailbox claim is the account that keeps the second fire out.
//
// The script reads the request instead of counting decisions: a segment that is
// still flying when the notice lands reads the settle material in its own next
// decision, and a segment that already closed lets the notice open a range of
// its own. Both shapes read the settle material exactly once, so the decision
// that carries it — never the position of the call — is what the follow-up turn
// is, and the accept decision beside it answers as the plain entry answer.
func runWakeDuplicateFire(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 42"
		entryAnswer    = "the entry accepted the delegation"
		followUpAnswer = "the settle reached the user exactly once"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	holdFollowUp := make(chan struct{})
	followUpStarted := make(chan struct{})
	var releaseOnce sync.Once
	var mu sync.Mutex
	entryCalls, followUps := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, workerBody) {
			followUps++
			if followUps == 1 {
				close(followUpStarted)
				<-holdFollowUp
			}
			return scriptedAnswerOf(followUpAnswer), nil
		}
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-duplicate"}

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

	select {
	case <-followUpStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("the follow-up turn never reached its provider")
	}

	// Fire the very same delivered settle fact again while the first turn is
	// still in flight: the idempotent commit still runs its After pass, so the
	// observation face sees the notification a second time.
	var settleFact event.Event
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" {
			settleFact = fact
		}
	}
	if settleFact.Key == "" {
		t.Fatalf("the session carries no delivered settle fact to fire twice: %+v", host.Run().SessionFacts(addr))
	}
	if _, err := host.Run().CommitToSession(ctx, addr, settleFact); err != nil {
		t.Fatalf("repeat the commit of the delivered settle fact: %v", err)
	}

	releaseHold()
	waitForSettle(t, "the follow-up turn to deliver its answer", func() bool {
		return deliveredCopies(sender.bodies(), followUpAnswer) == 1
	})
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	turns := followUps
	mu.Unlock()
	if turns != 1 {
		t.Fatalf("entry follow-up turns = %d, want the one the delivered settle drove and no turn beside the repeated fire", turns)
	}
	bodies := sender.bodies()
	if got := deliveredCopies(bodies, followUpAnswer); got != 1 {
		t.Fatalf("delivered bodies = %q, want the one follow-up answer %q", bodies, followUpAnswer)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runWakeRestartResume stops a host beside a follow-up turn nobody consumed and
// boots the next process over the same storage: the unconsumed notice must
// resume its turn without any user input and deliver the answer the first
// attempt never reached.
//
// The script reads the request instead of counting decisions, so the shape that
// reads the settle material in the decision the parked call left in flight and
// the shape that lets the notice open a range of its own both hold the same
// attempt: what the check counts is the decisions that read the settlement, and
// the assertion is the one answer that reaches the user.
func runWakeRestartResume(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 7"
		entryAnswer    = "the entry accepted the delegation"
		followUpAnswer = "the resumed follow-up reached the user without a second request"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	holdFollowUp := make(chan struct{})
	followUpStarted := make(chan struct{})
	var releaseOnce sync.Once
	var mu sync.Mutex
	entryCalls, followUps := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, workerBody) {
			followUps++
			if followUps == 1 {
				close(followUpStarted)
				<-holdFollowUp
			}
			return scriptedAnswerOf(followUpAnswer), nil
		}
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-restart"}

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

	select {
	case <-followUpStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("the first follow-up attempt never reached its provider")
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host beside the unconsumed notice: %v", err)
	}
	releaseHold()

	restored, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	waitForSettle(t, "the resumed follow-up to deliver its answer", func() bool {
		return deliveredCopies(sender.bodies(), followUpAnswer) == 1
	})
	bodies := sender.bodies()
	if got := deliveredCopies(bodies, followUpAnswer); got != 1 {
		t.Fatalf("delivered bodies = %q, want the one resumed answer %q", bodies, followUpAnswer)
	}
	mu.Lock()
	followed := followUps
	mu.Unlock()
	if followed < 2 {
		t.Fatalf("decisions that read the settlement = %d, want the stopped attempt and the resumed follow-up", followed)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}

// runWakeStopRetains stops a host beside one follow-up turn in flight and one
// settled notification no turn has started: both must be waiting in the
// mailbox account of the next boot, because a stop keeps unconsumed notices
// instead of clearing the queue behind it.
//
// The script reads the request instead of counting decisions, so the two shapes
// a landed notice takes — read by the segment still flying beside its own
// delegation, or read by a range the notice opens after that segment closed —
// both hold one attempt per session: the check counts the decisions that read
// each session's settlement and the resumed answers that reach the user.
func runWakeStopRetains(t *testing.T) {
	wakeEnv(t)

	const (
		workerBodyA    = "the first worker reports the total is 1"
		workerBodyB    = "the second worker reports the total is 2"
		entryAnswer    = "the entry accepted the delegation"
		followUpAnswer = "a retained notice resumed after the stop"
	)
	provider := newScriptedProvider(t)
	workerSeenA := make(chan struct{})
	workerSeenB := make(chan struct{})
	releaseWorkerA := make(chan struct{})
	releaseWorkerB := make(chan struct{})
	holdFollowUp := make(chan struct{})
	followUpStarted := make(chan struct{})
	var releaseOnce sync.Once
	var mu sync.Mutex
	entryCalls, workerCalls := 0, 0
	followUpsA, followUpsB := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			mu.Lock()
			workerCalls++
			worker := workerCalls
			mu.Unlock()
			switch worker {
			case 1:
				close(workerSeenA)
				<-releaseWorkerA
				return scriptedAnswerOf(workerBodyA), nil
			default:
				close(workerSeenB)
				<-releaseWorkerB
				return scriptedAnswerOf(workerBodyB), nil
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, workerBodyA) {
			followUpsA++
			if followUpsA == 1 {
				close(followUpStarted)
				<-holdFollowUp
			}
			return scriptedAnswerOf(followUpAnswer), nil
		}
		if settleDriveReadsMaterial(request, workerBodyB) {
			followUpsB++
			return scriptedAnswerOf(followUpAnswer), nil
		}
		entryCalls++
		switch entryCalls {
		case 1:
			return wakeDelegate("call-a", "the first total"), nil
		case 2:
			return wakeDelegate("call-b", "the second total"), nil
		}
		return scriptedAnswerOf(entryAnswer), nil
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addrA := event.Address{AgentID: "user", SessionID: "session-wake-a"}
	addrB := event.Address{AgentID: "user", SessionID: "session-wake-b"}

	doneA := make(chan error, 1)
	invokeCtxA, invokeCancelA := context.WithCancel(ctx)
	go func() {
		_, err := host.Invoke(invokeCtxA, assembly.InvokeRequest{
			Address: addrA,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the first total"}},
		})
		doneA <- err
	}()
	select {
	case <-workerSeenA:
	case <-time.After(30 * time.Second):
		invokeCancelA()
		t.Fatal("the first delegated turn never reached its provider")
	}
	invokeCancelA()
	<-doneA

	doneB := make(chan error, 1)
	invokeCtxB, invokeCancelB := context.WithCancel(ctx)
	go func() {
		_, err := host.Invoke(invokeCtxB, assembly.InvokeRequest{
			Address: addrB,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the second total"}},
		})
		doneB <- err
	}()
	select {
	case <-workerSeenB:
	case <-time.After(30 * time.Second):
		invokeCancelB()
		t.Fatal("the second delegated turn never reached its provider")
	}
	invokeCancelB()
	<-doneB

	close(releaseWorkerA)
	select {
	case <-followUpStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("the first follow-up turn never reached its provider")
	}
	close(releaseWorkerB)
	waitForSettle(t, "the second settle fact to land", func() bool {
		for _, fact := range host.Run().SessionFacts(addrB) {
			if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" {
				return true
			}
		}
		return false
	})

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host beside the unconsumed notices: %v", err)
	}
	releaseHold()

	restored, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	waitForSettle(t, "both retained notices to resume", func() bool {
		return deliveredCopies(sender.bodies(), followUpAnswer) >= 2
	})
	bodies := sender.bodies()
	if got := deliveredCopies(bodies, followUpAnswer); got != 2 {
		t.Fatalf("delivered bodies = %q, want one resumed answer per retained notice", bodies)
	}
	mu.Lock()
	turns, first, second := entryCalls, followUpsA, followUpsB
	mu.Unlock()
	if first < 1 || second < 1 {
		t.Fatalf("decisions that read each session's settlement = (%d, %d), want one per retained notice", first, second)
	}
	if turns > 4 {
		t.Fatalf("entry model turns = %d, want at most the two delegating decisions and the accept decision beside each of them", turns)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}

// runWakeConsumedNoReplay is the guard beside the resume check: a notice whose
// turn completed and consumed its entry never fires again across a further
// restart. The script answers the decision that read the settlement material,
// so the same answers serve the shape that reads the notice in the decision the
// parked call left in flight and the shape that reads it in a range of its own.
//
// - The guard is about a notice whose entry is consumed, so the check waits for the signal every shape of the landed notice holds: the follow-up answer the segment delivered reaching the sender exactly once.
// - The durable account is read only after the close and never beside a live host: opening the storage tier over a root a host still writes drops the temporary file of an in-flight barrier (memory/ledger readShardSnapshot drops it on every open), and the settle commit it cuts is the very receipt this guard reads — a poll loop over the tier therefore fails the drive it is waiting for.
// - A stop beside a body whose send never returned leaves an obligation the next process delivers, which the owed-delivery check pins instead of this guard.
func runWakeConsumedNoReplay(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 5"
		entryAnswer    = "the entry accepted the delegation"
		followUpAnswer = "the consumed settle never came back"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	var mu sync.Mutex
	entryCalls, settlements := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, workerBody) {
			settlements++
			return scriptedAnswerOf(followUpAnswer), nil
		}
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
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-consumed"}

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
	waitForSettle(t, "the follow-up answer to reach its sender", func() bool {
		return deliveredCopies(sender.bodies(), followUpAnswer) >= 1
	})
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	if got := deliveredCopies(sender.bodies(), followUpAnswer); got != 1 {
		t.Fatalf("delivered copies of %q = %d, want the one the consumed notice owns", followUpAnswer, got)
	}
	records := owedDeliveryRecords(t, root)
	if len(tierKeys(t, root, settleConsumePrefix)) == 0 {
		t.Fatalf("no consumption receipt stands beside the delivered notice, want the one the landed generation wrote")
	}
	if owed := recordsInState(records, owedState); len(owed) != 0 {
		t.Fatalf("delivery obligations standing owed after the close = %+v, want none beside a delivered answer", owed)
	}
	if sent := recordsCarrying(records, followUpAnswer); len(sent) != 1 || sent[0].Status != sentState {
		t.Fatalf("the record of %q after the close = %+v, want the one record settled as sent", followUpAnswer, sent)
	}

	mu.Lock()
	turns := entryCalls
	answers := settlements
	mu.Unlock()
	bodies := len(sender.bodies())

	restored, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	after := entryCalls
	answered := settlements
	mu.Unlock()
	if after != turns {
		t.Fatalf("entry model turns after the restart = %d, want the %d the consumed notice never exceeds", after, turns)
	}
	if answered != answers {
		t.Fatalf("settlement answers after the restart = %d, want the %d the consumed notice already closed", answered, answers)
	}
	if got := len(sender.bodies()); got != bodies {
		t.Fatalf("delivered bodies after the restart = %d, want the %d the consumed notice already delivered", got, bodies)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}

// deliveredCopies counts how many of the delivered bodies carry exactly the
// named answer, so a check of one answer reaching the user once reads the
// bodies it was actually delivered as instead of the position of a decision.
func deliveredCopies(bodies []string, answer string) int {
	copies := 0
	for _, body := range bodies {
		if body == answer {
			copies++
		}
	}
	return copies
}
