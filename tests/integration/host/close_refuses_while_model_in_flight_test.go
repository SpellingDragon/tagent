package host

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCloseRefusesWhileModelInFlight drives one assembled run to its close beside the producers of its formal root and checks the drain they force:
// - a model in flight holds no task at all and is still named beside its unconfirmed stop
// - a cancelled request is never read as a stopped producer while the tool still runs
// - a producer answering false keeps the drain open and only its real stop lets the close report one
// - the close refuses new input before the drain empties and keeps refusing it afterwards
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestCloseRefusesWhileModelInFlight(t *testing.T) {
	t.Run("a model in flight holds no task at all and is still named beside its unconfirmed stop", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, provider, tool := newModelFlightRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-model-flight"}
		driving := make(chan error, 1)
		go func() {
			_, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-model-flight",
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			driving <- err
		}()
		provider.awaitRequest(t)
		report, err := run.Close(ctx, agent.CloseRequest{Address: addr, Deadline: time.Now().Add(300 * time.Millisecond)})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close beside the model in flight = (%+v, %v), want the named unsettled state instead of a clean drain", report, err)
		}
		wantOne := agent.UnsettledObligation{TaskID: "inv-model-flight", Reason: "the producer has not confirmed its stop"}
		if len(report.Unsettled) != 2 {
			t.Fatalf("unsettled = %+v, want the drive and the model production each named beside its unconfirmed stop", report.Unsettled)
		}
		for _, entry := range report.Unsettled {
			if !reflect.DeepEqual(entry, wantOne) {
				t.Fatalf("unsettled entry = %+v, want the named production %v", entry, wantOne)
			}
		}
		provider.unblock()
		if err := <-driving; err != nil {
			t.Fatalf("invoke beside the released model: %v", err)
		}
		if got := provider.requestCount(); got != 1 {
			t.Fatalf("model requests = %d, want the single drive the close stood beside", got)
		}
		if got := tool.calls(); got != 0 {
			t.Fatalf("tool executions = %d, want no task admitted while the model was in flight", got)
		}
		if tasks := runTasks(t, run, context.Background(), addr); len(tasks) != 0 {
			t.Fatalf("tasks beside the model in flight = %v, want no task admitted at all", tasks)
		}
	})

	t.Run("a cancelled request is never read as a stopped producer while the tool still runs", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		run, _, tool := newSlowToolRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-cancel"}
		driving := make(chan error, 1)
		go func() {
			_, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-cancel",
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			driving <- err
		}()
		tool.awaitCall(t)
		cancel()
		if err := <-driving; !errors.Is(err, context.Canceled) {
			t.Fatalf("invoke beside the cancelled request = %v, want the cancellation the drive ended on", err)
		}
		if tool.finished() {
			t.Fatal("the tool reported its end beside the cancellation, want the production still running")
		}
		report, err := run.Close(context.Background(), agent.CloseRequest{Address: addr, Deadline: time.Now().Add(300 * time.Millisecond)})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close beside the running tool = (%+v, %v), want the drain kept open by the production that never stopped", report, err)
		}
		tasks := runTasks(t, run, context.Background(), addr)
		if len(tasks) != 1 {
			t.Fatalf("tasks beside the cancelled drive = %v, want the one admitted call", tasks)
		}
		want := agent.UnsettledObligation{TaskID: tasks[0], Reason: "the producer has not confirmed its stop"}
		found := false
		for _, entry := range report.Unsettled {
			if entry == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("unsettled = %+v, want the tool production %v named beside its unconfirmed stop", report.Unsettled, want)
		}
		if got := tool.callCount(); got != 1 {
			t.Fatalf("tool executions = %d, want the single production the close stood beside", got)
		}
		tool.unblock()
	})

	t.Run("a producer answering false keeps the drain open and only its real stop lets the close report one", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _, tool := newSlowToolRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-real-stop"}
		driving := make(chan error, 1)
		go func() {
			_, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-real-stop",
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			driving <- err
		}()
		tool.awaitCall(t)
		type closeOutcome struct {
			report agent.DrainReport
			err    error
		}
		closed := make(chan closeOutcome, 1)
		go func() {
			report, err := run.Close(ctx, agent.CloseRequest{Address: addr, Deadline: time.Now().Add(20 * time.Second)})
			closed <- closeOutcome{report: report, err: err}
		}()
		select {
		case got := <-closed:
			t.Fatalf("close returned beside the running tool = (%+v, %v), want no drain before the real stop", got.report, got.err)
		case <-time.After(200 * time.Millisecond):
		}
		tool.unblock()
		got := <-closed
		if got.err != nil || !got.report.Drained || len(got.report.Unsettled) != 0 {
			t.Fatalf("close beside the stopped tool = (%+v, %v), want the drain reached beside the real stop", got.report, got.err)
		}
		if !tool.finished() {
			t.Fatal("the close reported its drain before the tool confirmed its stop")
		}
		if err := <-driving; err != nil {
			t.Fatalf("invoke beside the released tool: %v", err)
		}
		if got := tool.callCount(); got != 1 {
			t.Fatalf("tool executions = %d, want the single production the close waited out", got)
		}
	})

	t.Run("the close refuses new input before the drain empties and keeps refusing it afterwards", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, provider, tool := newSlowToolRun(t, ctx)
		addr := event.Address{AgentID: "agent-a", SessionID: "session-refuse"}
		driving := make(chan error, 1)
		go func() {
			_, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-first",
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			driving <- err
		}()
		tool.awaitCall(t)
		type closeOutcome struct {
			report agent.DrainReport
			err    error
		}
		closed := make(chan closeOutcome, 1)
		go func() {
			report, err := run.Close(ctx, agent.CloseRequest{Address: addr, Deadline: time.Now().Add(20 * time.Second)})
			closed <- closeOutcome{report: report, err: err}
		}()
		refused := false
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			if _, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-late",
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: "late"}},
			}); errors.Is(err, agent.ErrShutdownRefused) {
				refused = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !refused {
			t.Fatal("the close ran without refusing the new input, want the gate closed before the drain empties")
		}
		if tool.finished() {
			t.Fatal("the drain had already emptied beside the refusal, want new input refused while the work was still in flight")
		}
		tool.unblock()
		got := <-closed
		if got.err != nil || !got.report.Drained {
			t.Fatalf("close = (%+v, %v), want the drain reached after the real stop", got.report, got.err)
		}
		if err := <-driving; err != nil {
			t.Fatalf("invoke beside the released tool: %v", err)
		}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-after",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "after"}},
		}); !errors.Is(err, agent.ErrShutdownRefused) {
			t.Fatalf("invoke after the close = %v, want the named refusal of the closed gate", err)
		}
		if got := len(provider.recorded()); got < 2 {
			t.Fatalf("model requests = %d, want every decision of the first drive really driven", got)
		}
		if got := tool.callCount(); got != 1 {
			t.Fatalf("tool executions = %d, want the single production the drain settled", got)
		}
	})
}

// newModelFlightRun assembles one runtime whose model parks its stream until
// the test releases it, beside one echo tool the parked model never reaches.
func newModelFlightRun(t *testing.T, ctx context.Context) (*assembly.Run, *blockingModel, *entryTool) {
	t.Helper()
	provider := newBlockingModel("model-a", func(req *model.Request) *model.Response {
		return entryFinalTurn("settled")
	})
	tool := &entryTool{name: "echo", output: "out-a"}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, provider, tool
}

// newSlowToolRun assembles one runtime whose first decision reaches one slow
// tool and whose second decision ends the turn, over the scripted native model.
func newSlowToolRun(t *testing.T, ctx context.Context) (*assembly.Run, *entryModel, *slowTool) {
	t.Helper()
	provider := &entryModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if last := req.Messages[len(req.Messages)-1]; last.Content == "hello" {
			return entryToolTurn(entryCall("call-1", "slow", `{"x":1}`))
		}
		return entryFinalTurn("settled")
	}}
	tool := newSlowTool("slow")
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, provider, tool
}

// runTasks lists the task identities the assembled run holds for the named
// address through its diagnostic reading.
func runTasks(t *testing.T, run *assembly.Run, ctx context.Context, addr event.Address) []event.TaskID {
	t.Helper()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectTask},
	})
	if err != nil {
		t.Fatalf("query the task dimension: %v", err)
	}
	out := make([]event.TaskID, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		out = append(out, detail.Ref.TaskID)
	}
	return out
}

// blockingModel is the scripted native model stand-in that parks its stream
// until the test releases it: the request is recorded as it arrives, and the
// answer runs only after the release, so a close driven beside the park finds
// one model production in flight that has confirmed no stop.
type blockingModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
	arrived  chan struct{}
	release  chan struct{}
	released bool
}

// newBlockingModel returns the parked model stand-in beside its scripted
// answer.
func newBlockingModel(name string, answer func(req *model.Request) *model.Response) *blockingModel {
	return &blockingModel{
		name:    name,
		answer:  answer,
		arrived: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

// GenerateContent records the native request, parks the stream and answers
// with the scripted response once the test releases it.
func (m *blockingModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	select {
	case m.arrived <- struct{}{}:
	default:
	}
	<-m.release
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the parked model's descriptor.
func (m *blockingModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// requestCount reports how often the parked model was driven.
func (m *blockingModel) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// awaitRequest waits until the parked model records its first request.
func (m *blockingModel) awaitRequest(t *testing.T) {
	t.Helper()
	select {
	case <-m.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the model received no request, want the drive to reach its production")
	}
}

// unblock lets the parked stream produce its answer.
func (m *blockingModel) unblock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.released {
		m.released = true
		close(m.release)
	}
}

// slowTool is the native tool stand-in whose production runs until the test
// releases it: the call is recorded as it starts and the end is reported only
// after the production really returned, so a close driven beside the run reads
// the production as unconfirmed and never as stopped.
type slowTool struct {
	mu       sync.Mutex
	name     string
	calls    int
	done     bool
	arrived  chan struct{}
	release  chan struct{}
	released bool
}

// newSlowTool returns the slow tool stand-in beside its release channel.
func newSlowTool(name string) *slowTool {
	return &slowTool{
		name:    name,
		arrived: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

// Declaration returns the declaration naming the slow tool.
func (s *slowTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: s.name}
}

// Call runs the slow production and answers with its output once the test
// releases it.
func (s *slowTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case s.arrived <- struct{}{}:
	default:
	}
	<-s.release
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	return "out-slow", nil
}

// callCount reports how often the slow tool started a production.
func (s *slowTool) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// finished reports whether the slow production really returned.
func (s *slowTool) finished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// awaitCall waits until the slow tool starts its production.
func (s *slowTool) awaitCall(t *testing.T) {
	t.Helper()
	select {
	case <-s.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the tool received no call, want the drive to reach its production")
	}
}

// unblock lets the slow production return.
func (s *slowTool) unblock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.released {
		s.released = true
		close(s.release)
	}
}
