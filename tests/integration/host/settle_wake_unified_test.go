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

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
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
func runWakeDuplicateFire(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 42"
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
	entryCalls := 0
	provider.answer = func(index int) string {
		models := provider.servedModels()
		if models[index] == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody)
		}
		mu.Lock()
		entryCalls++
		call := entryCalls
		mu.Unlock()
		if call == 1 {
			return wakeDelegate("call-worker", "compute the total")
		}
		if call == 2 {
			close(followUpStarted)
			<-holdFollowUp
		}
		return scriptedAnswerOf(followUpAnswer)
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-duplicate"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, tagent.InvokeRequest{
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
		return len(sender.bodies()) >= 1
	})
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	turns := entryCalls
	mu.Unlock()
	if turns != 2 {
		t.Fatalf("entry model turns = %d, want the delegating decision beside exactly one follow-up turn", turns)
	}
	bodies := sender.bodies()
	if len(bodies) != 1 || bodies[0] != followUpAnswer {
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
func runWakeRestartResume(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 7"
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
	entryCalls := 0
	provider.answer = func(index int) string {
		models := provider.servedModels()
		if models[index] == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody)
		}
		mu.Lock()
		entryCalls++
		call := entryCalls
		mu.Unlock()
		if call == 1 {
			return wakeDelegate("call-worker", "compute the total")
		}
		if call == 2 {
			close(followUpStarted)
			<-holdFollowUp
		}
		return scriptedAnswerOf(followUpAnswer)
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-restart"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, tagent.InvokeRequest{
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

	restored, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	waitForSettle(t, "the resumed follow-up to deliver its answer", func() bool {
		return len(sender.bodies()) >= 1
	})
	bodies := sender.bodies()
	if len(bodies) != 1 || bodies[0] != followUpAnswer {
		t.Fatalf("delivered bodies = %q, want the one resumed answer %q", bodies, followUpAnswer)
	}
	mu.Lock()
	turns := entryCalls
	mu.Unlock()
	if turns < 3 {
		t.Fatalf("entry model turns = %d, want the delegating decision, the stopped attempt and the resumed follow-up", turns)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}

// runWakeStopRetains stops a host beside one follow-up turn in flight and one
// settled notification no turn has started: both must be waiting in the
// mailbox account of the next boot, because a stop keeps unconsumed notices
// instead of clearing the queue behind it.
func runWakeStopRetains(t *testing.T) {
	wakeEnv(t)

	const (
		workerBodyA    = "the first worker reports the total is 1"
		workerBodyB    = "the second worker reports the total is 2"
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
	provider.answer = func(index int) string {
		models := provider.servedModels()
		if models[index] == "model-worker" {
			mu.Lock()
			workerCalls++
			worker := workerCalls
			mu.Unlock()
			switch worker {
			case 1:
				close(workerSeenA)
				<-releaseWorkerA
				return scriptedAnswerOf(workerBodyA)
			default:
				close(workerSeenB)
				<-releaseWorkerB
				return scriptedAnswerOf(workerBodyB)
			}
		}
		mu.Lock()
		entryCalls++
		call := entryCalls
		mu.Unlock()
		switch call {
		case 1:
			return wakeDelegate("call-a", "the first total")
		case 2:
			return wakeDelegate("call-b", "the second total")
		case 3:
			close(followUpStarted)
			<-holdFollowUp
		}
		return scriptedAnswerOf(followUpAnswer)
	}
	releaseHold := func() { releaseOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
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
		_, err := host.Invoke(invokeCtxA, tagent.InvokeRequest{
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
		_, err := host.Invoke(invokeCtxB, tagent.InvokeRequest{
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

	restored, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	waitForSettle(t, "both retained notices to resume", func() bool {
		return len(sender.bodies()) >= 2
	})
	bodies := sender.bodies()
	if len(bodies) < 2 {
		t.Fatalf("delivered bodies = %q, want one resumed answer per retained notice", bodies)
	}
	mu.Lock()
	turns := entryCalls
	mu.Unlock()
	if turns < 5 {
		t.Fatalf("entry model turns = %d, want two delegating decisions, the stopped attempt and the two resumed follow-ups", turns)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}

// runWakeConsumedNoReplay is the guard beside the resume check: a notice whose
// turn completed and consumed its entry never fires again across a further
// restart.
func runWakeConsumedNoReplay(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody     = "the delegated worker reports the total is 5"
		followUpAnswer = "the consumed settle never came back"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	var mu sync.Mutex
	entryCalls := 0
	provider.answer = func(index int) string {
		models := provider.servedModels()
		if models[index] == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody)
		}
		mu.Lock()
		entryCalls++
		call := entryCalls
		mu.Unlock()
		if call == 1 {
			return wakeDelegate("call-worker", "compute the total")
		}
		return scriptedAnswerOf(followUpAnswer)
	}

	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-wake-consumed"}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, tagent.InvokeRequest{
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
	waitForSettle(t, "the follow-up turn to deliver its answer", func() bool {
		return len(sender.bodies()) >= 1
	})
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}

	mu.Lock()
	turns := entryCalls
	mu.Unlock()
	bodies := len(sender.bodies())

	restored, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	after := entryCalls
	mu.Unlock()
	if after != turns {
		t.Fatalf("entry model turns after the restart = %d, want the %d the consumed notice never exceeds", after, turns)
	}
	if got := len(sender.bodies()); got != bodies {
		t.Fatalf("delivered bodies after the restart = %d, want the %d the consumed notice already delivered", got, bodies)
	}
	if err := restored.Close(ctx); err != nil {
		t.Fatalf("close the restored host: %v", err)
	}
}
