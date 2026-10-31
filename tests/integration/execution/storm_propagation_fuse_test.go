// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
//
// The storm-propagation anchor: one injection, two shapes, the same verdict on
// the baseline that fell over. The victim's storm propagated because a single
// model response could issue six thousand calls and because every input (and
// every notice) opened a turn of its own; this anchor replays both shapes at a
// magnitude the anchor can hold and reads what the new baseline did with them.
//
//   - the held-response case: one response issues 500 calls under a cap of 64 and nothing runs until an approver answers; the whole response stands as one approval request, and the approval runs it exactly once
//   - the chain case: the approved storm leaves one result fact per admitted call and one decision fact, never a second fact per call for a degraded backflow
//   - the input-storm case: twenty inputs that arrive together ride one segment, so the model request count does not follow the number of inputs
//
// The comparison numbers of the old baseline (c111d7a2) are logged by the chain
// case; see stormFuseLogComparison below for what each column means.
package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

const (
	// stormFuseCalls is the single-response call storm this anchor replays: an
	// order of magnitude past the cap, small enough that the anchor reads every
	// receipt it carries, and the same shape the victim issued by the thousand.
	stormFuseCalls = 500
	// stormFuseCap is the per-response cap the storm is held under: the
	// documented production default.
	stormFuseCap = 64
	// stormFuseInputs is the input storm of the second shape: twenty messages
	// that arrive at one address together while its segment is in flight.
	stormFuseInputs = 20
	// stormFuseRequestCeiling is how many model requests that storm may cost:
	// far below one request per input, because the inputs ride the segment the
	// address already flies rather than opening a turn each.
	stormFuseRequestCeiling = 8
	// stormFuseWaitWindow is the short window the held response is observed
	// through before it is answered: past the approval poll interval, so a fuse
	// that admitted anything at all would have shown it by now.
	stormFuseWaitWindow = 750 * time.Millisecond
	// stormFuseInputBody is the material every input of the input storm carries,
	// so a request that reads two of them is read as the batch it is.
	stormFuseInputBody = "storm input"
	// stormFuseHoldCeiling bounds how long the input-storm case holds its first
	// decision inside the model, so a gate nobody opens fails the case instead of
	// hanging it.
	stormFuseHoldCeiling = 30 * time.Second
)

// TestStormPropagationFuse 钉住风暴传播两形态：单响应海量调用（B）与并发输入风暴（A）在同一注入下都不放大。
//   - a_response_over_the_cap_waits_as_one_batch：K=64 下 500 调用整批挂审批，短窗零执行、审批簿恰 1 项、请求文件 1 份；批准后 500 次派发恰一次。
//   - an_approved_storm_leaves_a_bounded_chain_increment：批准路径链增量 = 1 决策 + 500 结果（受理数），无 500×2 降级回灌。
//   - a_twenty_input_storm_rides_one_segment：一段在飞请求期间 20 条输入一起落地 → 模型请求 ≤8，且至少一条请求读到两条风暴输入。
//
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestStormPropagationFuse(t *testing.T) {
	t.Run("a_response_over_the_cap_waits_as_one_batch", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: stormFuseCap})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := stormFuseRig(t, "session-storm-held")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, stormFuseRequest(addr, "inv-storm-held"))
		req := awaitBatchRequest(t, book, dir)
		assertStormBatchHead(t, req)

		time.Sleep(stormFuseWaitWindow)
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls without a ruling = %d, want the whole response held", got)
		}
		if recorded := provider.recorded(); len(recorded) != 1 {
			t.Fatalf("model requests without a ruling = %d, want the one round that issued the storm", len(recorded))
		}
		if pending := book.Pending(); len(pending) != 1 {
			t.Fatalf("pending approvals of one held response = %d, want exactly one batch", len(pending))
		}
		if registered := registeredBatches(t, dir); len(registered) != 1 {
			t.Fatalf("registered approval requests of one held response = %d (%v), want exactly one", len(registered), registered)
		}

		decideBatch(t, book, req.ID, approval.ApprovalApproved)
		awaitAsync(t, errs)
		if got := tool.calls(); got != stormFuseCalls {
			t.Fatalf("dispatched calls of the approved storm = %d, want every call of it ran exactly once", got)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests of the approved storm = %d, want the storm round beside the one round that reads its receipts", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != stormFuseCalls || admissions != stormFuseCalls {
			t.Fatalf("tool receipts = %d (admissions %d, others %v), want one admission per call of the approved batch", receipts, admissions, others)
		}
		notice := batchNoticeIn(requests[1].Messages)
		for _, want := range []string{fmt.Sprint(stormFuseCalls), "approved", req.ID} {
			if !strings.Contains(notice, want) {
				t.Fatalf("batch receipt = %q, want the batch size, the approval and the request identity named", notice)
			}
		}
		if tasks := responseStormTasks(t, run, addr); tasks != stormFuseCalls {
			t.Fatalf("tasks admitted by the approved storm = %d, want every call of it", tasks)
		}
		if settled := readRegisteredBatch(t, dir, req.ID); settled.Status != approval.ApprovalApproved || settled.DecidedBy != approvalTestApprover {
			t.Fatalf("stored request status = %q by %q, want the ruling that let the batch run", settled.Status, settled.DecidedBy)
		}
		if got := tool.calls(); got != stormFuseCalls {
			t.Fatalf("dispatched calls after the round that read the receipts = %d, want the batch to run once and never twice", got)
		}
	})

	t.Run("an_approved_storm_leaves_a_bounded_chain_increment", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: stormFuseCap})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := stormFuseRig(t, "session-storm-chain")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, stormFuseRequest(addr, "inv-storm-chain"))
		req := awaitBatchRequest(t, book, dir)
		decideBatch(t, book, req.ID, approval.ApprovalApproved)
		awaitAsync(t, errs)
		if got := tool.calls(); got != stormFuseCalls {
			t.Fatalf("dispatched calls of the approved storm = %d, want every call of it", got)
		}

		actions, results := responseStormChain(t, run, addr)
		if actions != 1 || results != stormFuseCalls {
			t.Fatalf("chain increment = %d actions beside %d result facts, want one decision and one result per admitted call", actions, results)
		}
		if results >= 2*stormFuseCalls {
			t.Fatalf("result facts of the approved storm = %d, want no degraded fallback beside the admitted calls", results)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests of the approved storm = %d, want the storm round beside the round that reads its receipts", len(requests))
		}
		strays, composition := stormFuseSecondRequest(requests[1].Messages, req.ID)
		if len(strays) != 0 {
			t.Fatalf("second request carries %d unpaired user message(s) (%v), want no degraded fallback re-entering the range", len(strays), strays)
		}
		stormFuseLogComparison(t, stormFuseMeasure{
			requests:    len(requests),
			messages:    len(requests[1].Messages),
			composition: composition,
			actions:     actions,
			results:     results,
			tasks:       responseStormTasks(t, run, addr),
			dispatch:    tool.calls(),
			approvals:   len(registeredBatches(t, dir)),
		})
	})

	t.Run("a_twenty_input_storm_rides_one_segment", func(t *testing.T) {
		endpoint := newStormInputEndpoint(t)
		sender := newIntakeSender()
		host := stormInputBoot(t, endpoint, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		addr := event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession + "-storm-inputs"}

		opened := invokeAsync(ctx, host, assembly.InvokeRequest{
			InvocationID: "inv-storm-open",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "storm open"}},
		})
		endpoint.awaitFlight(t)
		failures := stormInvokeAll(ctx, host, addr)
		stormAwaitInputFacts(t, host, addr, stormFuseInputs+1)
		endpoint.release()
		awaitAsync(t, opened)
		for _, failure := range failures.await(t) {
			t.Fatalf("invoke the input storm: %v", failure)
		}

		requests := endpoint.requests()
		if len(requests) > stormFuseRequestCeiling {
			t.Fatalf("model requests of a %d input storm = %d, want at most %d: the inputs ride the segment rather than opening a turn each",
				stormFuseInputs, len(requests), stormFuseRequestCeiling)
		}
		batched := stormFuseBatchedRequest(requests)
		if batched < 2 {
			t.Fatalf("the widest request of the storm reads %d storm input(s), want one request that reads at least two of them", batched)
		}
		if delivered := stormFuseDeliveredBodies(sender.accepted()); delivered < 1 || delivered > len(requests) {
			t.Fatalf("delivered answers = %d, want one body per closed segment and never one per entry (%d segments)", delivered, len(requests))
		} else {
			t.Logf("input storm: %d inputs, %d model requests, widest request reads %d inputs, %d answer bodies delivered for %d entries",
				stormFuseInputs, len(requests), batched, delivered, stormFuseInputs+1)
		}
	})
}

// stormFuseRig assembles the storm runtime of the direct path: the agent answers
// its opening request with the whole storm and every later request with the
// final answer.
func stormFuseRig(t *testing.T, session string) (*assembly.Run, *convModel, *convTool, event.Address) {
	t.Helper()
	tool := &convTool{name: "send", output: "sent"}
	provider := &convModel{name: "model-storm", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(stormFuseCallSet("storm")...)
		}
		return convFinalTurn("done")
	}}
	return responseStormAssembly(t, provider, tool), provider, tool, event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)}
}

// stormFuseCallSet builds the calls of one storm response: every call names the
// one tool the agent carries and carries its own index, so a receipt is readable
// by the identity it was answered under.
func stormFuseCallSet(prefix string) []model.ToolCall {
	calls := make([]model.ToolCall, 0, stormFuseCalls)
	for at := 0; at < stormFuseCalls; at++ {
		calls = append(calls, convCall(fmt.Sprintf("%s-%03d", prefix, at), "send", fmt.Sprintf(`{"n":%d}`, at)))
	}
	return calls
}

// stormFuseRequest builds the invocation that opens one storm range.
func stormFuseRequest(addr event.Address, invocation string) assembly.InvokeRequest {
	return assembly.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
	}
}

// assertStormBatchHead pins the request one storm is parked as: the group it
// names, the cap that held it, and the summary head an approver reads first —
// the issued count, the cap it broke and the tool the batch carries.
func assertStormBatchHead(t *testing.T, req *approval.ApprovalRequest) {
	t.Helper()
	if req.ToolName != batchApprovalToolName {
		t.Fatalf("approval subject tool = %q, want the batch group %q", req.ToolName, batchApprovalToolName)
	}
	if req.RuleID != "response-call-cap" {
		t.Fatalf("approval rule = %q, want the cap that held the response", req.RuleID)
	}
	if req.Status != approval.ApprovalPending {
		t.Fatalf("approval status = %q, want the request parked until it is answered", req.Status)
	}
	for _, want := range []string{
		fmt.Sprintf(`"calls":%d`, stormFuseCalls),
		fmt.Sprintf(`"cap":%d`, stormFuseCap),
		`"prompt_tokens"`,
		`"items":[{"tool":"send"`,
	} {
		if !strings.Contains(req.ArgsPreview, want) {
			t.Fatalf("approval preview = %q, want the summary head %s an approver reads", req.ArgsPreview, want)
		}
	}
	for _, want := range []string{"storm-000", fmt.Sprintf("storm-%03d", stormFuseCalls-1)} {
		if !strings.Contains(req.Reason, want) {
			t.Fatalf("approval reason = %q, want the call ids of the batch kept on the read side (%s)", req.Reason, want)
		}
	}
}

// stormFuseSecondRequest reads the request that follows the approved storm: the
// messages no call pairs, and how the request is composed. A message carrying a
// call identifier pairs a call — the admission receipt or the observation it
// settled into — and the decision's own assistant turn stands beside them. What
// is left is the opening input and the one batch receipt; any further user
// message is the degraded fallback of the old shape, which replaced every call
// it could not run with input material of its own.
func stormFuseSecondRequest(messages []model.Message, batchID string) ([]string, stormFuseComposition) {
	strays := make([]string, 0, 4)
	composition := stormFuseComposition{}
	for _, msg := range messages {
		switch {
		case msg.ToolID != "" && msg.Role == model.RoleTool:
			composition.admissions++
		case msg.ToolID != "":
			composition.observations++
		case msg.Role == model.RoleAssistant:
			composition.decision++
		case msg.Role == model.RoleUser && composition.inputs == 0:
			composition.inputs++
		case msg.Role == model.RoleUser && strings.Contains(msg.Content, batchID):
			composition.notices++
		default:
			strays = append(strays, string(msg.Role)+": "+msg.Content)
		}
	}
	return strays, composition
}

// stormFuseComposition is how the request after an approved storm is put
// together, so the comparison table states the measured shape rather than a
// remembered one.
type stormFuseComposition struct {
	inputs       int
	decision     int
	admissions   int
	observations int
	notices      int
}

// stormFuseMeasure is what one replay of the injection cost the new baseline, as
// the comparison table reads it.
type stormFuseMeasure struct {
	requests    int
	messages    int
	composition stormFuseComposition
	actions     int
	results     int
	tasks       int
	dispatch    int
	approvals   int
}

// stormFuseLogComparison logs the two baselines side by side, so the numbers the
// old one fell over with stand beside the ones this anchor measured instead of
// being remembered in a review thread.
//
// The old-baseline column is measured, not read: the same injection was replayed
// on a worktree of c111d7a2 (`git worktree add /tmp/tagent-c111d7a2 c111d7a2`)
// through two throwaway probes of the same shape this anchor drives — one with
// the calls the agent really carries, one with calls of a tool it does not, the
// shape the victim's degraded storm took. The probes were removed with the
// worktree, so this table and the report that carries it are the record.
// Measured there: dispatch=500, requests=2, second request=1002 messages for the
// carried-tool shape, and dispatch=0 with 502 messages (500 degraded user
// messages carrying their whole payload) for the invalid shape.
// - The victim column is the production run the change was opened for: 6,277 invalid calls in one response (design.md D3, proposal.md S3), logged as the pair of lines each degraded call wrote.
func stormFuseLogComparison(t *testing.T, measured stormFuseMeasure) {
	t.Helper()
	t.Logf("storm propagation, single response of %d calls under a cap of %d:", stormFuseCalls, stormFuseCap)
	t.Logf("| 面 | 旧基线 c111d7a2（worktree 实测） | 新基线（本锚实测） |")
	t.Logf("|---|---|---|")
	t.Logf("| 无裁决时受理数 | %d（无闸，立即全部派发） | 0（整批挂审批） |", stormFuseCalls)
	t.Logf("| 审批簿项数 | 0（无审批面） | %d（不随 N 膨胀） |", measured.approvals)
	t.Logf("| 裁决后受理数 | —（无裁决点） | %d（恰一次：受理回执 %d、任务 %d） |", measured.dispatch, measured.composition.admissions, measured.tasks)
	t.Logf("| 链上事实增量 | 0 决策 + %d 结果 | %d 决策 + %d 结果（=受理数，非 %d） |", stormFuseCalls, measured.actions, measured.results, 2*stormFuseCalls)
	t.Logf("| 次轮请求消息数 | %d（开场 1 + 决策 1 + 受理 %d + 落定 %d） | %d（开场 %d + 决策 %d + 受理 %d + 落定 %d + 批次通知 %d + 降级回链 0） |",
		2+2*stormFuseCalls, stormFuseCalls, stormFuseCalls,
		measured.messages, measured.composition.inputs, measured.composition.decision, measured.composition.admissions,
		measured.composition.observations, measured.composition.notices)
	t.Logf("| 同注入换成无效调用（victim 形态） | 0 派发 + %d 条降级 user 消息（实测 502 条消息，payload 无长度上限） | 0 派发（整批挂审批，降级面不经裁决不可触达；批准后 fork 限长 4KB） |", stormFuseCalls)
	t.Logf("| 模型请求次数 | 2 | %d |", measured.requests)
	t.Logf("victim（production, 6,277 无效调用/响应）：最后一轮无 model response（OOM）；日志 12,560 行、6,277 对 ERROR/WARN 降级记录——每个无效调用各写一对，与旧基线实测的「每调用 1 条降级消息」同形。")
	t.Logf("来源：调用数 6,277 见 design.md:25 与 proposal.md:8；日志行数/配对数为派发单台账口径（仓内 ORCHESTRATION.md 未登记该两数，此处按派发单引用）。")
}

// stormInputEndpoint is the endpoint the input-storm case boots against: it
// records every request, answers each with the final answer, and holds the first
// one until the anchor opens its gate, so the storm lands while one segment is
// in flight.
type stormInputEndpoint struct {
	server *httptest.Server

	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage

	flight chan struct{}
	gate   chan struct{}
	opened sync.Once
	once   sync.Once
}

// newStormInputEndpoint starts the endpoint and closes it with the test.
func newStormInputEndpoint(t *testing.T) *stormInputEndpoint {
	t.Helper()
	endpoint := &stormInputEndpoint{flight: make(chan struct{}), gate: make(chan struct{})}
	endpoint.server = httptest.NewServer(http.HandlerFunc(endpoint.serve))
	t.Cleanup(endpoint.server.Close)
	return endpoint
}

// serve answers one chat completion and records the request it carried: the
// first request is held until the gate opens, every other one ends at once.
func (e *stormInputEndpoint) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	var modelName string
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	e.mu.Lock()
	index := len(e.bodies)
	e.models = append(e.models, modelName)
	e.bodies = append(e.bodies, body)
	e.mu.Unlock()
	if index == 0 {
		e.opened.Do(func() { close(e.flight) })
		select {
		case <-e.gate:
		case <-time.After(stormFuseHoldCeiling):
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, intakeFinalAnswer(intakeFinalBody))
}

// awaitFlight waits until the opening decision reached the model and is held
// there, so every input injected after this point lands beside a segment in
// flight rather than opening one of its own.
func (e *stormInputEndpoint) awaitFlight(t *testing.T) {
	t.Helper()
	select {
	case <-e.flight:
	case <-time.After(stormFuseHoldCeiling):
		t.Fatalf("the opening decision never reached the model within %s", stormFuseHoldCeiling)
	}
}

// release frees the held decision, exactly once.
func (e *stormInputEndpoint) release() {
	e.once.Do(func() { close(e.gate) })
}

// requests reports the requests the endpoint served, in request order.
func (e *stormInputEndpoint) requests() []intakeRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]intakeRequest, 0, len(e.bodies))
	for index, body := range e.bodies {
		out = append(out, intakeDecodeRequest(index, e.models[index], body))
	}
	return out
}

// endpoint reports the base URL the host document's provider entry names.
func (e *stormInputEndpoint) endpoint() string { return e.server.URL + "/v1" }

// stormInputBoot boots the formal host the input storm is driven against, over
// the document the intake anchors use and the endpoint this case holds.
func stormInputBoot(t *testing.T, endpoint *stormInputEndpoint, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(intakeHostDocument(root, endpoint.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the storm host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the storm host ended its close beside: %v", err)
		}
	})
	return host
}

// stormInvokeAll lands the whole input storm at one address at once, each input
// under its own identity, and reports how the invocations ended.
func stormInvokeAll(ctx context.Context, host *assembly.Host, addr event.Address) *stormFailures {
	failures := &stormFailures{done: make(chan struct{})}
	var wg sync.WaitGroup
	wg.Add(stormFuseInputs)
	for at := 0; at < stormFuseInputs; at++ {
		go func(at int) {
			defer wg.Done()
			_, err := host.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: event.InvocationID(fmt.Sprintf("inv-storm-input-%02d", at)),
				Address:      addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: fmt.Sprintf("%s %02d", stormFuseInputBody, at)}},
			})
			if err != nil {
				failures.mu.Lock()
				failures.errs = append(failures.errs, err)
				failures.mu.Unlock()
			}
		}(at)
	}
	go func() {
		wg.Wait()
		close(failures.done)
	}()
	return failures
}

// stormFailures reports how a storm of invocations ended.
type stormFailures struct {
	mu   sync.Mutex
	errs []error
	done chan struct{}
}

// await waits for every invocation of the storm to return and reports the ones
// that ended by error.
func (f *stormFailures) await(t *testing.T) []error {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(90 * time.Second):
		t.Fatalf("the input storm never settled: an entry was left waiting on a segment that never closed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]error(nil), f.errs...)
}

// stormAwaitInputFacts waits until the named number of inputs stand committed on
// the session chain, so the storm is known to have landed before the held
// decision is released.
func stormAwaitInputFacts(t *testing.T, host *assembly.Host, addr event.Address, want int) {
	t.Helper()
	deadline := time.Now().Add(stormFuseHoldCeiling)
	for time.Now().Before(deadline) {
		inputs := 0
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeExternalInput {
				inputs++
			}
		}
		if inputs >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the chain carried fewer than %d committed inputs when the window closed, so the storm never landed together", want)
}

// stormFuseBatchedRequest reports the most storm inputs one model request read:
// a request that carries two of them is the batch the anchor is after.
func stormFuseBatchedRequest(requests []intakeRequest) int {
	widest := 0
	for _, request := range requests {
		carried := 0
		for _, msg := range request.Messages {
			if msg.Role == string(model.RoleUser) && strings.Contains(msg.Content, stormFuseInputBody) {
				carried++
			}
		}
		if carried > widest {
			widest = carried
		}
	}
	return widest
}

// stormFuseDeliveredBodies counts the bodies the outbound face accepted, so the
// storm is read as answered rather than merely consumed.
func stormFuseDeliveredBodies(bodies []string) int {
	total := 0
	for _, body := range bodies {
		if strings.Contains(body, intakeFinalBody) {
			total++
		}
	}
	return total
}
