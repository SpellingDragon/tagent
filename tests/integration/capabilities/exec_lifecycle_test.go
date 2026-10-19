package capabilities

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestExecutionResourceLifecycle drives the execution resource lifecycle through the formal admission chain and checks the facts it settles.
// - one admitted run starts, settles and frees its resource at the settlement backflow beside the exact chain order
// - a cancelled caller context neither stops nor frees the resource before a confirmed stop
// - an unconfirmed stop keeps the resource held beside its open obligations and refuses a second execution under its key
// - a run of undecidable probes escalates diagnosis without deciding death
// - an expired lease goes through explicit stop handling and a renewal extends the lease
// - the public entry reuses one resource and releases it after its run settled
// - the monitor rhythm rides its call and a missing capability degrades observably
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func TestExecutionResourceLifecycle(t *testing.T) {
	t.Run("one admitted run starts, settles and frees its resource at the settlement backflow", func(t *testing.T) {
		ctx := context.Background()
		journal := &elJournal{}
		executor := newELExecutor(journal)
		executor.gate = make(chan struct{})
		manager := tagentexec.NewManager(executor)
		decl := elDecl()
		inner := &elTool{name: "exec_probe", manager: manager, decl: decl, journal: journal}
		records := make(chan task.Result, 4)
		settled := make(chan elSettle, 4)
		registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
		taskManager := task.NewMemoryManager(registry, func(_ context.Context, _ task.Notice) (task.DeliveryAcceptance, error) {
			return task.DeliveryAccepted, nil
		})
		wrapper := task.Wrap(taskManager, inner,
			task.WithResultRecorder(func(_ context.Context, res task.Result) (event.EventID, error) {
				journal.add("record")
				records <- res
				return event.EventID("fact-" + res.CallID), nil
			}),
			task.WithMailbox(func(ctx context.Context, notice task.Notice) error {
				journal.add("settle")
				evidence, err := manager.Release(context.WithoutCancel(ctx), decl.Resource)
				journal.add("release")
				settled <- elSettle{notice: notice, evidence: evidence}
				return err
			}),
		)

		receipt, err := wrapper.Call(task.WithCallFrame(ctx, elFrame("call-run")), nil)
		if err != nil {
			t.Fatalf("admit the wrapped call: %v", err)
		}
		journal.add("receipt")
		close(executor.gate)

		res := elAwaitRecord(t, records)
		settle := elAwaitSettle(t, settled)

		admitted, ok := receipt.(task.AdmissionReceipt)
		if !ok || !admitted.Accepted {
			t.Fatalf("admission receipt = (%+v, %v), want the accepted receipt of the admitted call", receipt, ok)
		}
		if res.TaskID != admitted.TaskID || settle.notice.TaskID != admitted.TaskID {
			t.Errorf("task identities = (receipt %s, result %s, notice %s), want one task across the receipt, the settle and the backflow", admitted.TaskID, res.TaskID, settle.notice.TaskID)
		}
		want := []string{"receipt", "start probe/r1", "end", "record", "settle", "stop probe/r1", "release"}
		if got := journal.entries(); !slices.Equal(got, want) {
			t.Errorf("chain journal = %v, want %v", got, want)
		}
		if !settle.evidence.Released || !settle.evidence.Stop.Confirmed {
			t.Errorf("release evidence = %+v, want the resource freed beside a confirmed stop", settle.evidence)
		}
		if manager.Released() != 1 || manager.Held() != 0 {
			t.Errorf("release accounting = (released %d, held %d), want exactly one confirmed release and no residue", manager.Released(), manager.Held())
		}
		if executor.stops != 1 || len(executor.live) != 0 {
			t.Errorf("executor accounting = (stops %d, live %v), want one stop and no held session left behind", executor.stops, executor.live)
		}
		if out := fmt.Sprint(res.Output); !strings.Contains(out, "executed probe/r1: frame:r1") {
			t.Errorf("settled output = %q, want the captured frame of the run", out)
		}
	})

	t.Run("a cancelled caller context neither stops nor frees the resource before a confirmed stop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		executor := newELExecutor(&elJournal{})
		manager := tagentexec.NewManager(executor)
		decl := elDecl()
		key, err := manager.Acquire(ctx, decl)
		if err != nil {
			t.Fatalf("acquire the execution resource: %v", err)
		}
		cancel()

		if executor.stops != 0 || !executor.live[key] {
			t.Errorf("after the cancel = (stops %d, live %v), want the execution untouched by a cancelled context", executor.stops, executor.live)
		}
		if manager.Held() != 1 {
			t.Errorf("held after the cancel = %d, want the lease kept until a real stop", manager.Held())
		}
		evidence, err := manager.Release(ctx, key)
		if err != nil || !evidence.Released {
			t.Fatalf("release under the cancelled context = (%+v, %v), want the confirmed stop observed under its own context", evidence, err)
		}
		if executor.stopSawCancel {
			t.Errorf("the stop ran under a cancelled context, want the stop observed on its own context")
		}
		if manager.Released() != 1 || manager.Held() != 0 || len(executor.live) != 0 {
			t.Errorf("release accounting = (released %d, held %d, live %v), want the confirmed stop freeing the resource exactly once", manager.Released(), manager.Held(), executor.live)
		}
	})

	t.Run("an unconfirmed stop keeps the resource held beside its open obligations", func(t *testing.T) {
		ctx := context.Background()
		executor := newELExecutor(&elJournal{})
		executor.confirmed = false
		manager := tagentexec.NewManager(executor)
		decl := elDecl()
		key, err := manager.Acquire(ctx, decl)
		if err != nil {
			t.Fatalf("acquire the execution resource: %v", err)
		}

		evidence, err := manager.Release(ctx, key)
		if err != nil {
			t.Fatalf("release the resource: %v", err)
		}
		if evidence.Released || evidence.Stop.Confirmed || evidence.Reason != "the execution reported no exit" {
			t.Errorf("release evidence = %+v, want the retention beside the unconfirmed stop", evidence)
		}
		if manager.Held() != 1 || manager.Released() != 0 {
			t.Errorf("held accounting = (held %d, released %d), want the resource kept under its key", manager.Held(), manager.Released())
		}
		explanation, err := manager.Explain(key)
		if err != nil || !explanation.Held || explanation.LastStop == nil || explanation.LastStop.Confirmed || len(explanation.Obligations) == 0 {
			t.Errorf("explanation = (%+v, %v), want the retained resource beside its open obligations and the unconfirmed stop it was last seen under", explanation, err)
		}
		if _, err := manager.Acquire(ctx, decl); !errors.Is(err, tagentexec.ErrResourceHeld) {
			t.Errorf("second acquisition = %v, want the held key refusing a second execution", err)
		}
		out, err := tagentexec.NewReleaseTool(manager).Call(ctx, []byte(`{"kind":"probe","resource":"r1"}`))
		if err != nil || !strings.HasPrefix(fmt.Sprint(out), "retained probe/r1: stop unconfirmed") {
			t.Errorf("release tool answer = (%v, %v), want the retained resource beside the reason and no claimed ending", out, err)
		}

		executor.confirmed = true
		evidence, err = manager.Release(ctx, key)
		if err != nil || !evidence.Released {
			t.Fatalf("release beside the confirmed stop = (%+v, %v), want the resource freed once the stop is observed", evidence, err)
		}
		if manager.Held() != 0 || manager.Released() != 1 {
			t.Errorf("release accounting = (held %d, released %d), want the confirmation freeing the resource exactly once", manager.Held(), manager.Released())
		}
	})

	t.Run("a run of undecidable probes escalates diagnosis without deciding death", func(t *testing.T) {
		ctx := context.Background()
		executor := newELExecutor(&elJournal{})
		executor.probes = []tagentexec.ProbeState{
			tagentexec.ProbeUnknown, tagentexec.ProbeUnknown, tagentexec.ProbeUnknown, tagentexec.ProbeAlive,
		}
		manager := tagentexec.NewManager(executor)
		key, err := manager.Acquire(ctx, elDecl())
		if err != nil {
			t.Fatalf("acquire the execution resource: %v", err)
		}

		for i := 0; i < 3; i++ {
			observed, err := manager.Observe(ctx, key)
			if err != nil || observed.State != tagentexec.ProbeUnknown {
				t.Fatalf("undecidable probe %d = (%+v, %v), want the unknown state reported as a state", i, observed, err)
			}
		}
		if manager.Held() != 1 || manager.Released() != 0 || manager.Escalated() != 3 {
			t.Errorf("after three unknowns = (held %d, released %d, escalated %d), want the lease kept beside three diagnosis escalations", manager.Held(), manager.Released(), manager.Escalated())
		}
		explanation, err := manager.Explain(key)
		if err != nil || explanation.ConsecutiveUnknowns != 3 || explanation.Escalations != 3 || len(explanation.Obligations) == 0 {
			t.Errorf("explanation = (%+v, %v), want the unknown run and the escalations beside the open obligations", explanation, err)
		}
		if executor.stops != 0 {
			t.Errorf("stops after the unknown run = %d, want no stop driven by undecidable probes", executor.stops)
		}
		if reused, err := manager.Acquire(ctx, elDecl()); err != nil || reused != key || manager.Reused() != 1 {
			t.Errorf("acquisition during the unknown run = (%v, %v, reused %d), want the held resource reused rather than decided dead", reused, err, manager.Reused())
		}

		observed, err := manager.Observe(ctx, key)
		if err != nil || observed.State != tagentexec.ProbeAlive {
			t.Fatalf("deciding probe = (%+v, %v), want the alive observation", observed, err)
		}
		explanation, err = manager.Explain(key)
		if err != nil || explanation.ConsecutiveUnknowns != 0 {
			t.Errorf("explanation after the deciding probe = (%+v, %v), want the unknown run reset", explanation, err)
		}
	})

	t.Run("an expired lease goes through explicit stop handling and a renewal extends it", func(t *testing.T) {
		ctx := context.Background()
		executor := newELExecutor(&elJournal{})
		clock := time.Now()
		manager := tagentexec.NewManager(executor, tagentexec.WithClock(func() time.Time { return clock }))
		key, err := manager.Acquire(ctx, elDecl(), tagentexec.WithTTL(10*time.Minute))
		if err != nil {
			t.Fatalf("acquire the execution resource: %v", err)
		}

		clock = clock.Add(6 * time.Minute)
		if err := manager.Renew(ctx, key, 10*time.Minute); err != nil {
			t.Fatalf("renew the lease: %v", err)
		}
		explanation, err := manager.Explain(key)
		if err != nil || explanation.Renewed != 1 || !explanation.Deadline.After(clock) {
			t.Errorf("explanation after the renewal = (%+v, %v), want the extended lease counted once", explanation, err)
		}
		clock = clock.Add(20 * time.Minute)
		reports := manager.Sweep(ctx)
		if len(reports) != 1 || !reports[0].Expired || !reports[0].Evidence.Released {
			t.Errorf("sweep reports = %+v, want the expired lease driven through the explicit stop to a confirmed release", reports)
		}
		if manager.Held() != 0 || manager.Released() != 1 || executor.stops != 1 {
			t.Errorf("accounting after the sweep = (held %d, released %d, stops %d), want the expiry freeing nothing until the confirmed stop", manager.Held(), manager.Released(), executor.stops)
		}

		executor.confirmed = false
		retained, err := manager.Acquire(ctx, elDecl(), tagentexec.WithTTL(time.Minute))
		if err != nil {
			t.Fatalf("acquire a second execution resource: %v", err)
		}
		clock = clock.Add(2 * time.Minute)
		reports = manager.Sweep(ctx)
		if len(reports) != 1 || reports[0].Key != retained || reports[0].Evidence.Released {
			t.Errorf("sweep reports = %+v, want the expired lease retained beside its unconfirmed stop", reports)
		}
		if manager.Held() != 1 {
			t.Errorf("held after the unconfirmed sweep = %d, want the resource kept under its key", manager.Held())
		}
	})

	t.Run("a reattached resource binds a lease without starting a second execution", func(t *testing.T) {
		ctx := context.Background()
		executor := newELExecutor(&elJournal{})
		manager := tagentexec.NewManager(executor)
		key := elDecl().Resource

		executor.probes = []tagentexec.ProbeState{tagentexec.ProbeAlive}
		if err := manager.Adopt(ctx, key); err != nil {
			t.Fatalf("adopt the running resource: %v", err)
		}
		if manager.Adopted() != 1 || executor.starts != 0 || manager.Held() != 1 {
			t.Errorf("adoption accounting = (adopted %d, starts %d, held %d), want the lease bound to the running execution beside no fresh start", manager.Adopted(), executor.starts, manager.Held())
		}
		if err := manager.Adopt(ctx, key); !errors.Is(err, tagentexec.ErrResourceHeld) {
			t.Errorf("second adoption = %v, want the held key refusing a second lease", err)
		}

		manager2 := tagentexec.NewManager(executor)
		executor.probes = []tagentexec.ProbeState{tagentexec.ProbeUnknown}
		if err := manager2.Adopt(ctx, key); !errors.Is(err, tagentexec.ErrAdoptUnconfirmed) {
			t.Errorf("adoption over an undecidable probe = %v, want the unconfirmed adoption refused while unknown counts as no absence", err)
		}
		if manager2.Held() != 0 || executor.starts != 0 {
			t.Errorf("refused adoption = (held %d, starts %d), want no lease and no execution behind the refusal", manager2.Held(), executor.starts)
		}
	})

	t.Run("the public entry reuses one resource and releases it after its run settled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		journal := &elJournal{}
		executor := newELExecutor(journal)
		manager := tagentexec.NewManager(executor)

		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			switch round {
			case 1:
				return ntToolTurn(ntCall("call-run-1", "exec_run", `{"command":"probe","args":["one"],"kind":"probe","resource":"r1","monitor":{"interval":"10s"},"compress":{"max_tool_result_chars":1}}`))
			case 2:
				if len(req.Messages) != 6 {
					t.Errorf("second request messages = %d, want the prompt, the skill body, the input, the calls and the settled run", len(req.Messages))
				} else {
					checkReceipt(t, req.Messages[4], "call-run-1", "exec_run", "executed probe/r1")
					checkObservation(t, req.Messages[5], "call-run-1", "exec_run", "executed probe/r1: frame:r1")
				}
				if explanation, err := manager.Explain(tagentexec.ResourceKey{Kind: "probe", Key: "r1"}); err != nil || explanation.Rhythm.Interval != 10*time.Second {
					t.Errorf("generation rhythm = (%+v, %v), want the monitor rhythm of the call bound to its generation", explanation, err)
				}
				return ntToolTurn(ntCall("call-run-2", "exec_run", `{"command":"probe","args":["one"],"kind":"probe","resource":"r1"}`))
			case 3:
				if len(req.Messages) != 9 {
					t.Errorf("third request messages = %d, want the reused run settled beside its own receipt and observation", len(req.Messages))
				} else {
					checkReceipt(t, req.Messages[7], "call-run-2", "exec_run", "executed probe/r1")
					checkObservation(t, req.Messages[8], "call-run-2", "exec_run", "executed probe/r1: frame:r1")
				}
				return ntToolTurn(ntCall("call-release", "exec_release", `{"kind":"probe","resource":"r1"}`))
			default:
				if len(req.Messages) != 12 {
					t.Errorf("closing request messages = %d, want the release settled after both runs settled", len(req.Messages))
				} else {
					checkObservation(t, req.Messages[11], "call-release", "exec_release", "released probe/r1 after confirmed stop: exit=0")
				}
				return ntFinalTurn("settled")
			}
		}}

		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tagentexec.NewRunTool(manager), tagentexec.NewReleaseTool(manager)}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-exec-lifecycle",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-exec-lifecycle"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the execution lifecycle: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 4 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside the run, reuse, release and closing decisions", turn.Terminal, len(turn.Decisions))
		}
		if manager.Acquired() != 1 || manager.Reused() != 1 || manager.Released() != 1 || manager.Held() != 0 {
			t.Errorf("manager accounting = (acquired %d, reused %d, released %d, held %d), want one started resource reused once and freed once", manager.Acquired(), manager.Reused(), manager.Released(), manager.Held())
		}
		if executor.starts != 1 || executor.stops != 1 || len(executor.live) != 0 {
			t.Errorf("executor accounting = (starts %d, stops %d, live %v), want one execution reused and one confirmed stop leaving nothing behind", executor.starts, executor.stops, executor.live)
		}
		if got := journal.entries(); !slices.Equal(got, []string{"start probe/r1", "stop probe/r1"}) {
			t.Errorf("executor journal = %v, want one start and one stop around the whole chain", got)
		}
	})

	t.Run("the monitor rhythm rides its call and a missing capability degrades observably", func(t *testing.T) {
		ctx := context.Background()
		rhythm, err := tagentexec.MonitorConfigFrom(map[string]any{
			"interval":                    "10s",
			"stable_duration":             "45s",
			"interactive_stable_duration": "2m",
			"fake_dead_duration":          "3m",
			"compress":                    map[string]any{"max_tool_result_chars": 1},
			"unknown_section":             "ignored",
		})
		if err != nil {
			t.Fatalf("read the monitor rhythm: %v", err)
		}
		want := tagentexec.MonitorConfig{
			Interval:                  10 * time.Second,
			StableDuration:            45 * time.Second,
			InteractiveStableDuration: 2 * time.Minute,
			FakeDeadDuration:          3 * time.Minute,
		}
		if rhythm != want {
			t.Errorf("monitor rhythm = %+v, want the four supported keys consumed and the retired keys ignored", rhythm)
		}
		if _, err := tagentexec.MonitorConfigFrom(map[string]any{"interval": "bogus"}); err == nil {
			t.Errorf("malformed monitor key accepted silently, want the malformed duration reported by name")
		}

		_, lookErr := exec.LookPath("tmux")
		if tagentexec.IsTmuxAvailable() != (lookErr == nil) {
			t.Errorf("IsTmuxAvailable = %v beside PATH lookup %v, want the real PATH probe of the tmux binary", tagentexec.IsTmuxAvailable(), lookErr)
		}

		executor := newELExecutor(&elJournal{})
		degrade := tagentexec.NewManager(executor, tagentexec.WithAvailability(func(tagentexec.ResourceKey) bool { return false }))
		out, err := tagentexec.NewRunTool(degrade).Call(ctx, []byte(`{"command":"probe","kind":"tmux","resource":"r1","monitor":{"interval":"10s"}}`))
		var refused *tagentexec.DegradeError
		if out != nil || !errors.As(err, &refused) || !errors.Is(err, tagentexec.ErrTmuxUnavailable) {
			t.Fatalf("degrade refusal = (%v, %v), want the named capability refusal of the tmux family", out, err)
		}
		if refused.Rhythm.Interval != 10*time.Second {
			t.Errorf("refused rhythm = %+v, want the monitor rhythm of the call carried on the degrade path", refused.Rhythm)
		}
		if degrade.Held() != 0 || executor.starts != 0 {
			t.Errorf("degrade accounting = (held %d, starts %d), want the refusal starting and holding nothing", degrade.Held(), executor.starts)
		}
	})
}

// elDecl returns the execution declaration these sub-cases run under.
func elDecl() tagentexec.ExecutionDecl {
	return tagentexec.ExecutionDecl{
		Command:  "probe",
		Args:     []string{"one"},
		Resource: tagentexec.ResourceKey{Kind: "probe", Key: "r1"},
	}
}

// elFrame returns the admission identity one call of these sub-cases is
// admitted under.
func elFrame(callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-exec",
			InvocationID: "invocation-exec",
			DecisionID:   "decision-exec",
			CallID:       callID,
		},
		Address:   event.Address{AgentID: "agent-exec", SessionID: "session-exec"},
		Execution: "serial",
	}
}

// elJournal is the ordered fact log of one sub-case: every lifecycle transition
// appends the event it observed, so a sub-case reads the chain as one order.
type elJournal struct {
	mu     sync.Mutex
	events []string
}

// add records one observed event.
func (j *elJournal) add(event string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, event)
}

// entries reports the recorded events in the order they were observed.
func (j *elJournal) entries() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.events...)
}

// elExecutor is the deterministic fake execution face of these sub-cases: it
// owns the sessions it starts and reports exactly what it observed, so a
// lifecycle assertion rests on real state transitions rather than on fabricated
// evidence.
type elExecutor struct {
	mu            sync.Mutex
	journal       *elJournal
	gate          chan struct{}
	probes        []tagentexec.ProbeState
	confirmed     bool
	live          map[tagentexec.ResourceKey]bool
	starts        int
	stops         int
	stopSawCancel bool
}

// newELExecutor returns the fake executor beside the journal it reports to.
func newELExecutor(journal *elJournal) *elExecutor {
	return &elExecutor{journal: journal, confirmed: true, live: map[tagentexec.ResourceKey]bool{}}
}

// Start opens the session the declaration names and records the start.
func (e *elExecutor) Start(_ context.Context, decl tagentexec.ExecutionDecl) (tagentexec.ResourceKey, error) {
	if e.gate != nil {
		<-e.gate
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	e.live[decl.Resource] = true
	e.journal.add("start " + decl.Resource.String())
	return decl.Resource, nil
}

// Probe reports the scripted observation where one is queued and the session
// state it holds otherwise.
func (e *elExecutor) Probe(_ context.Context, key tagentexec.ResourceKey) (tagentexec.ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.probes) > 0 {
		state := e.probes[0]
		e.probes = e.probes[1:]
		return tagentexec.ProbeResult{State: state, Detail: "scripted observation"}, nil
	}
	if e.live[key] {
		return tagentexec.ProbeResult{State: tagentexec.ProbeAlive}, nil
	}
	return tagentexec.ProbeResult{State: tagentexec.ProbeDead, Detail: "no session behind this key"}, nil
}

// Capture returns the frame recorded for the session.
func (e *elExecutor) Capture(_ context.Context, key tagentexec.ResourceKey) ([]byte, error) {
	return []byte("frame:" + key.Key), nil
}

// Stop ends the session behind the key and reports the evidence observed for
// it, keeping the session where no exit was observed.
func (e *elExecutor) Stop(ctx context.Context, key tagentexec.ResourceKey) (tagentexec.StopEvidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.journal.add("stop " + key.String())
	if ctx.Err() != nil {
		e.stopSawCancel = true
	}
	e.stops++
	if !e.confirmed {
		return tagentexec.StopEvidence{Resource: key, Confirmed: false, Reason: "the execution reported no exit"}, nil
	}
	delete(e.live, key)
	return tagentexec.StopEvidence{Resource: key, Confirmed: true, ExitCode: 0}, nil
}

// elTool is the inner native tool of the formal chain: it acquires the
// execution resource its declaration names and reports what the execution
// recorded.
type elTool struct {
	name    string
	manager *tagentexec.Manager
	decl    tagentexec.ExecutionDecl
	journal *elJournal
}

// Declaration returns the declaration of the inner native tool.
func (t *elTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call acquires the execution resource and reports the captured frame of the
// execution it ran.
func (t *elTool) Call(ctx context.Context, _ []byte) (any, error) {
	key, err := t.manager.Acquire(ctx, t.decl)
	if err != nil {
		return nil, err
	}
	output, err := t.manager.Capture(ctx, key)
	if err != nil {
		return nil, err
	}
	t.journal.add("end")
	return fmt.Sprintf("executed %s: %s", key, output), nil
}

// elSettle pairs the settlement backflow of one task with the release evidence
// its resource was freed under.
type elSettle struct {
	notice   task.Notice
	evidence tagentexec.ReleaseEvidence
}

// elAwaitRecord reports the next recorded result, failing the sub-case when no
// result arrives.
func elAwaitRecord(t *testing.T, records chan task.Result) task.Result {
	t.Helper()
	select {
	case res := <-records:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("the recorded result did not arrive")
		return task.Result{}
	}
}

// elAwaitSettle reports the next settlement backflow, failing the sub-case when
// none arrives.
func elAwaitSettle(t *testing.T, settled chan elSettle) elSettle {
	t.Helper()
	select {
	case settle := <-settled:
		return settle
	case <-time.After(5 * time.Second):
		t.Fatal("the settlement backflow did not arrive")
		return elSettle{}
	}
}
