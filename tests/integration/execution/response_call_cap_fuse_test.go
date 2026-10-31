package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// responseStormCalls is the single-response call storm these anchors replay:
// the same magnitude the victim issued before the assembly died, spread over
// the tool the victim's agent actually carried.
const responseStormCalls = 200

// batchApprovalToolName is the identity a parked batch is registered under: the
// approval subject names one tool per request, and a batch names the group it
// is rather than borrowing the name of a member.
const batchApprovalToolName = "tagent:tool-call-batch"

// TestResponseCallCapFuse 钉住单响应调用数熔断：一次响应超出上限时，整流按一项审批登记、等待裁决，裁定后整批执行或整批不执行。
// - approval_admits_the_whole_batch：K=64 时 200 个调用登记为恰 1 项审批（摘要头含 calls/cap/prompt_tokens、清单 200 项、人读侧留 call ID）；测试侧批准后 200 条受理回执 + 1 条批次回执、任务受理 200。
// - refusal_runs_nothing：拒绝时 200 条拒绝回执、零派发、零任务。
// - expiry_runs_nothing：短有效期（100ms）无裁决 → 200 条未执行回执、零派发。
// - no_book_refuses_the_batch_whole：未接线审批面时 fail-closed 拒绝全部（措辞含 no approval channel is wired），绝不回退截断。
// - uncapped_response_admits_every_call：上限为 0（关闭态）时同一响应全部受理且不登记任何审批。
// - call_ids_stay_out_of_the_digest：同样调用换一套 call ID 重发 → 同一个 request digest、不同 request 身份（人读清单各自保留）。
// - consumer_path_holds_the_batch_for_approval：消费路径同一道闸——批准后 200 条外发、200 条受理回执 + 批次回执。
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestResponseCallCapFuse(t *testing.T) {
	t.Run("approval_admits_the_whole_batch", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := responseStormRig(t, "session-cap")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, assembly.InvokeRequest{
			InvocationID: "inv-cap-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		})
		req := awaitBatchRequest(t, book, dir)
		assertBatchSubject(t, req, 64, "call-cap")
		filed := readRegisteredBatch(t, dir, req.ID)
		assertBatchFile(t, filed)
		decideBatch(t, book, req.ID, approval.ApprovalApproved)
		awaitAsync(t, errs)
		if got := tool.calls(); got != responseStormCalls {
			t.Fatalf("dispatched calls = %d, want the whole approved batch and nothing held back", got)
		}
		requests := provider.recorded()
		if len(requests) < 2 {
			t.Fatalf("recorded requests = %d, want the storm round beside the round that reads its receipts", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != responseStormCalls || admissions != responseStormCalls {
			t.Fatalf("tool receipts = %d (admissions %d, others %v), want one admission per call of the approved batch", receipts, admissions, others)
		}
		notice := batchNoticeIn(requests[1].Messages)
		if notice == "" {
			t.Fatalf("no batch receipt in the next request, want the one receipt naming the approval of the whole batch")
		}
		for _, want := range []string{"200", "approved", req.ID} {
			if !strings.Contains(notice, want) {
				t.Fatalf("batch receipt = %q, want it to name the batch size, the approval and the request identity", notice)
			}
		}
		if tasks := responseStormTasks(t, run, addr); tasks != responseStormCalls {
			t.Fatalf("tasks admitted by the approved batch = %d, want every call of it", tasks)
		}
		actions, results := responseStormChain(t, run, addr)
		if actions != 1 || results != responseStormCalls {
			t.Fatalf("chain increment = %d actions beside %d result facts, want one decision and one result per admitted call", actions, results)
		}
		if registered := registeredBatches(t, dir); len(registered) != 1 {
			t.Fatalf("registered approval requests = %d (%v), want exactly one for the whole response", len(registered), registered)
		}
		if settled := readRegisteredBatch(t, dir, req.ID); settled.Status != approval.ApprovalApproved || settled.DecidedBy != approvalTestApprover {
			t.Fatalf("stored request status = %q by %q, want the ruling that let the batch run", settled.Status, settled.DecidedBy)
		}
	})

	t.Run("refusal_runs_nothing", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := responseStormRig(t, "session-cap-refused")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, assembly.InvokeRequest{
			InvocationID: "inv-cap-refused-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		})
		req := awaitBatchRequest(t, book, dir)
		decideBatch(t, book, req.ID, approval.ApprovalDenied)
		awaitAsync(t, errs)
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls of a refused batch = %d, want none", got)
		}
		requests := provider.recorded()
		if len(requests) < 2 {
			t.Fatalf("recorded requests = %d, want the storm round beside the round that reads its refusal", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != responseStormCalls || admissions != 0 {
			t.Fatalf("tool receipts = %d (admissions %d), want one refusal per call and no admission", receipts, admissions)
		}
		for _, text := range others {
			if !strings.Contains(text, "refused this batch") {
				t.Fatalf("refusal receipt = %q, want every call of the refused batch answered with its ruling", text)
			}
		}
		if notice := batchNoticeIn(requests[1].Messages); notice != "" {
			t.Fatalf("batch receipt of a refused batch = %q, want the refusal answered per call only", notice)
		}
		if tasks := responseStormTasks(t, run, addr); tasks != 0 {
			t.Fatalf("tasks admitted by a refused batch = %d, want none", tasks)
		}
		actions, results := responseStormChain(t, run, addr)
		if actions != 1 || results != 0 {
			t.Fatalf("chain increment of a refused batch = %d actions beside %d result facts, want the decision alone", actions, results)
		}
		if registered := registeredBatches(t, dir); len(registered) != 1 {
			t.Fatalf("registered approval requests = %d (%v), want exactly one for the whole response", len(registered), registered)
		}
	})

	t.Run("expiry_runs_nothing", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		book, dir := approvalBook(t, 100*time.Millisecond)
		run, provider, tool, addr := responseStormRig(t, "session-cap-expired")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, assembly.InvokeRequest{
			InvocationID: "inv-cap-expired-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		})
		awaitBatchRequest(t, book, dir)
		awaitAsync(t, errs)
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls of an expired batch = %d, want none", got)
		}
		requests := provider.recorded()
		if len(requests) < 2 {
			t.Fatalf("recorded requests = %d, want the storm round beside the round that reads its expiry", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != responseStormCalls || admissions != 0 {
			t.Fatalf("tool receipts = %d (admissions %d), want one not-executed receipt per call", receipts, admissions)
		}
		for _, text := range others {
			if !strings.Contains(text, "expired without a ruling") {
				t.Fatalf("expiry receipt = %q, want every call of the expired batch told its request outlived its validity", text)
			}
		}
		if tasks := responseStormTasks(t, run, addr); tasks != 0 {
			t.Fatalf("tasks admitted by an expired batch = %d, want none", tasks)
		}
	})

	t.Run("no_book_refuses_the_batch_whole", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		previous := agent.SetDefaultApprovalBook(nil)
		t.Cleanup(func() { agent.SetDefaultApprovalBook(previous) })
		run, provider, tool, addr := responseStormRig(t, "session-cap-unwired")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-cap-unwired-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		}); err != nil {
			t.Fatalf("invoke the storm beside no approval channel: %v", err)
		}
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls beside no approval channel = %d, want the batch refused rather than truncated", got)
		}
		requests := provider.recorded()
		if len(requests) < 2 {
			t.Fatalf("recorded requests = %d, want the storm round beside the round that reads its refusal", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != responseStormCalls || admissions != 0 {
			t.Fatalf("tool receipts = %d (admissions %d), want one refusal per call", receipts, admissions)
		}
		for _, text := range others {
			if !strings.Contains(text, "no approval channel is wired") {
				t.Fatalf("refusal receipt = %q, want the named gap every gate beside no channel answers with", text)
			}
		}
		if tasks := responseStormTasks(t, run, addr); tasks != 0 {
			t.Fatalf("tasks admitted beside no approval channel = %d, want none", tasks)
		}
	})

	t.Run("uncapped_response_admits_every_call", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 0})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := responseStormRig(t, "session-uncapped")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-uncapped-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		}); err != nil {
			t.Fatalf("invoke the uncapped range: %v", err)
		}
		if got := tool.calls(); got != responseStormCalls {
			t.Fatalf("dispatched calls under no cap = %d, want every call of the response admitted", got)
		}
		requests := provider.recorded()
		if len(requests) < 2 {
			t.Fatalf("recorded requests = %d, want the storm round beside the round that reads its receipts", len(requests))
		}
		receipts, admissions, others := toolReceipts(requests[1].Messages)
		if receipts != responseStormCalls || admissions != responseStormCalls {
			t.Fatalf("receipts = %d (admissions %d, others %v), want every call answered under no cap", receipts, admissions, others)
		}
		if notice := batchNoticeIn(requests[1].Messages); notice != "" {
			t.Fatalf("batch receipt under no cap = %q, want no batch to be parked at all", notice)
		}
		if pending := book.Pending(); len(pending) != 0 {
			t.Fatalf("pending approvals under no cap = %d, want the fuse off to park nothing", len(pending))
		}
		if registered := registeredBatches(t, dir); len(registered) != 0 {
			t.Fatalf("registered approval requests under no cap = %d (%v), want none", len(registered), registered)
		}
		if tasks := responseStormTasks(t, run, addr); tasks != responseStormCalls {
			t.Fatalf("tasks admitted under no cap = %d, want every call of the response", tasks)
		}
	})

	t.Run("call_ids_stay_out_of_the_digest", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		book, dir := approvalBook(t, 0)
		storms := 0
		tool := &convTool{name: "send", output: "sent"}
		provider := &convModel{name: "model-digest", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				storms++
				return convToolTurn(responseStorm(fmt.Sprintf("storm-%d", storms))...)
			}
			return convFinalTurn("done")
		}}
		run := responseStormAssembly(t, provider, tool)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		first := responseStormRefused(t, ctx, run, book, dir, event.Address{AgentID: "agent-a", SessionID: "session-digest-a"}, "inv-digest-1")
		time.Sleep(5 * time.Millisecond)
		second := responseStormRefused(t, ctx, run, book, dir, event.Address{AgentID: "agent-a", SessionID: "session-digest-b"}, "inv-digest-2")
		if first.ArgsDigest != second.ArgsDigest {
			t.Fatalf("request digest of the same calls under other call ids = %q then %q, want the digest bound to the content", first.ArgsDigest, second.ArgsDigest)
		}
		if first.ID == second.ID {
			t.Fatalf("approval identities = %q twice, want one identity per parked batch", first.ID)
		}
		for at, req := range []*approval.ApprovalRequest{first, second} {
			want := fmt.Sprintf("storm-%d-199", at+1)
			if !strings.Contains(req.Reason, want) {
				t.Fatalf("reason of request %d = %q, want the call ids of that very batch kept on the read side", at+1, req.Reason)
			}
		}
	})

	t.Run("consumer_path_holds_the_batch_for_approval", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{CallCap: 64})
		book, dir := approvalBook(t, 0)
		provider := newIntakeProvider(t)
		provider.firstAnswer = consumerStormAnswer()
		sender := newIntakeSender()
		host := intakeBoot(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		addr := event.Address{AgentID: intakeEntryAgent, SessionID: "session-host-cap"}
		errs := invokeAsync(ctx, host, assembly.InvokeRequest{
			InvocationID: "inv-host-cap-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
		})
		req := awaitBatchRequest(t, book, dir)
		assertBatchSubject(t, req, 64, "call-host")
		decideBatch(t, book, req.ID, approval.ApprovalApproved)
		awaitAsync(t, errs)
		sends := 0
		for _, body := range sender.accepted() {
			if strings.HasPrefix(body, "rotation ") {
				sends++
			}
		}
		if sends != responseStormCalls {
			t.Fatalf("outbound sends of the approved batch = %d, want every call of it on the consumption path too", sends)
		}
		receipts, admissions, others := consumerStormReceipts(t, provider)
		if receipts != responseStormCalls || admissions != responseStormCalls {
			t.Fatalf("tool receipts on the consumption path = %d (admissions %d, others %v), want one admission per call", receipts, admissions, others)
		}
		if notice := consumerBatchNotice(t, provider); !strings.Contains(notice, "approved") {
			t.Fatalf("batch receipt on the consumption path = %q, want the approval of the whole batch named", notice)
		}
	})
}

// approvalTestApprover is the name every ruling in these anchors is rendered
// under, so a stored request reads back the approver that answered it.
const approvalTestApprover = "test-approver"

// approvalBook wires a real approval manager over one directory the anchor owns
// and restores the book of the surrounding scene when the case ends. The
// manager is the production artifact and the request file it writes is the face
// an external approver answers, so the anchors approve through that very face
// instead of a stand-in book.
func approvalBook(t *testing.T, ttl time.Duration) (*approval.ApprovalManager, string) {
	t.Helper()
	dir := t.TempDir()
	book := approval.NewApprovalManager(dir, ttl)
	previous := agent.SetDefaultApprovalBook(book)
	t.Cleanup(func() { agent.SetDefaultApprovalBook(previous) })
	return book, filepath.Join(dir, "approvals")
}

// invoker is the entry an anchor drives: the run of the direct path and the
// host of the consumption path answer the one invoke face.
type invoker interface {
	Invoke(ctx context.Context, req assembly.InvokeRequest) (agent.Turn, error)
}

// invokeAsync runs one invocation off the test goroutine: a held response parks
// inside its own dispatch, so the anchor answers the ruling while the invocation
// waits, exactly as a host entry does at runtime.
func invokeAsync(ctx context.Context, run invoker, req assembly.InvokeRequest) <-chan error {
	errs := make(chan error, 1)
	go func() {
		_, err := run.Invoke(ctx, req)
		errs <- err
	}()
	return errs
}

// awaitAsync waits for the invocation the anchor answered to return.
func awaitAsync(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("invoke the held range: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatalf("the invocation never returned: the parked batch was not resolved")
	}
}

// awaitBatchRequest waits for the one approval request the held response
// registers and reports it once its request file stands, because that file is
// the face an approver answers: the file lands before the index names it, so
// an approver reading the file waits for it exactly as a host entry does; the
// wait keeps the helper honest against any future reordering.
func awaitBatchRequest(t *testing.T, book *approval.ApprovalManager, dir string) *approval.ApprovalRequest {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pending := book.Pending()
		if len(pending) > 1 {
			t.Fatalf("pending approval requests = %d, want the response parked as exactly one batch", len(pending))
		}
		if len(pending) == 1 {
			if _, err := os.Stat(filepath.Join(dir, pending[0].ID+".json")); err == nil {
				return pending[0]
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no approval request was registered and written for the held response")
	return nil
}

// decideBatch answers one parked batch through the approval face a host entry
// answers with.
func decideBatch(t *testing.T, book *approval.ApprovalManager, id string, status approval.ApprovalStatus) {
	t.Helper()
	if _, err := book.Decide(id, status, approvalTestApprover); err != nil {
		t.Fatalf("decide the parked batch %s: %v", id, err)
	}
}

// assertBatchSubject pins the request one storm is parked as: the group it
// names, the rule that held it and the summary head an approver reads first.
func assertBatchSubject(t *testing.T, req *approval.ApprovalRequest, cap int, prefix string) {
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
	for _, want := range []string{`"calls":200`, fmt.Sprintf(`"cap":%d`, cap), `"prompt_tokens"`, `"tool":"send"`} {
		if !strings.Contains(req.ArgsPreview, want) {
			t.Fatalf("approval preview = %q, want the summary head %s an approver reads", req.ArgsPreview, want)
		}
	}
	for _, want := range []string{prefix + "-000", prefix + "-199"} {
		if !strings.Contains(req.Reason, want) {
			t.Fatalf("approval reason = %q, want the call ids of the batch kept on the read side (%s)", req.Reason, want)
		}
	}
	if req.ExpiresMs <= req.CreatedMs {
		t.Fatalf("approval validity = %d..%d, want a request that outlives its registration", req.CreatedMs, req.ExpiresMs)
	}
}

// assertBatchFile pins the request file an external approver answers: the
// human-readable face is the production artifact, so the anchor reads it back
// from the directory the book wrote it into.
func assertBatchFile(t *testing.T, req approval.ApprovalRequest) {
	t.Helper()
	if req.ToolName != batchApprovalToolName || req.RuleID != "response-call-cap" {
		t.Fatalf("stored request tool/rule = %q/%q, want the batch group under the cap that held it", req.ToolName, req.RuleID)
	}
	if !strings.Contains(req.ArgsPreview, `"calls":200`) {
		t.Fatalf("stored preview = %q, want the issued count an approver judges the batch by", req.ArgsPreview)
	}
	if !strings.Contains(req.Reason, "call-cap-199") {
		t.Fatalf("stored reason = %q, want the call ids of the batch", req.Reason)
	}
}

// readRegisteredBatch reads the request file the book wrote for one identity.
func readRegisteredBatch(t *testing.T, dir, id string) approval.ApprovalRequest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("read the approval request file of %s: %v", id, err)
	}
	var req approval.ApprovalRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode the approval request file of %s: %v", id, err)
	}
	if req.ID != id {
		t.Fatalf("stored request identity = %q, want the one file it is kept under (%s)", req.ID, id)
	}
	return req
}

// registeredBatches reports the request files the book keeps, so an anchor
// counts the registrations a response really made.
func registeredBatches(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the approvals directory %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, entry.Name())
	}
	return out
}

// responseStormRefused runs one storm and refuses the batch it registers,
// reporting the request as the book stored it.
func responseStormRefused(t *testing.T, ctx context.Context, run invoker, book *approval.ApprovalManager, dir string, addr event.Address, invocation string) *approval.ApprovalRequest {
	t.Helper()
	errs := invokeAsync(ctx, run, assembly.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "run every rotation at once"}},
	})
	req := awaitBatchRequest(t, book, dir)
	decideBatch(t, book, req.ID, approval.ApprovalDenied)
	awaitAsync(t, errs)
	return req
}

// toolReceipts reports the tool receipts of one recorded request: how many
// answered a call, how many carried the admission confirmation and the texts
// that answered the rest.
func toolReceipts(messages []model.Message) (int, int, []string) {
	receipts, admissions := 0, 0
	others := make([]string, 0, 4)
	for _, msg := range messages {
		if msg.Role != model.RoleTool {
			continue
		}
		receipts++
		if msg.Content == task.AdmissionReceiptText {
			admissions++
			continue
		}
		if len(others) < 8 {
			others = append(others, msg.Content)
		} else {
			others = others[:7]
			others = append(others, msg.Content)
		}
	}
	return receipts, admissions, others
}

// batchNoticeIn reports the batch receipt one recorded request carries: the
// notice of the approval stands as input material of the range, under no call
// identifier, because it answers the batch and not one of its calls.
func batchNoticeIn(messages []model.Message) string {
	for _, msg := range messages {
		if msg.Role == model.RoleUser && msg.ToolID == "" && strings.Contains(msg.Content, "was approved") {
			return msg.Content
		}
	}
	return ""
}

// consumerStormAnswer renders the scripted completion the host entry reads: one
// response carrying the whole storm, so the cap is judged on the path a
// deployment really runs rather than on the direct drive alone.
func consumerStormAnswer() string {
	calls := make([]string, 0, responseStormCalls)
	for at := 0; at < responseStormCalls; at++ {
		arguments := intakeQuoted(fmt.Sprintf(`{"content":"rotation %d"}`, at))
		calls = append(calls, fmt.Sprintf(`{"id":"call-host-%03d","type":"function","function":{"name":"send","arguments":%s}}`, at, arguments))
	}
	return `{"id":"scripted-storm","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"calling","tool_calls":[` + strings.Join(calls, ",") + `]},` +
		`"finish_reason":"tool_calls"}]}`
}

// consumerStormReceipts reports the tool receipts of the first request the host
// entry served that already carries one: how many answered a call, how many
// carried the admission confirmation, and the texts that answered the rest.
func consumerStormReceipts(t *testing.T, provider *intakeProvider) (int, int, []string) {
	t.Helper()
	for _, request := range provider.requests() {
		receipts, admissions := 0, 0
		others := make([]string, 0, 4)
		for _, msg := range request.Messages {
			if msg.Role != string(model.RoleTool) {
				continue
			}
			receipts++
			if msg.Content == task.AdmissionReceiptText {
				admissions++
				continue
			}
			if len(others) < 8 {
				others = append(others, msg.Content)
			} else {
				others = others[:7]
				others = append(others, msg.Content)
			}
		}
		if receipts > 0 {
			return receipts, admissions, others
		}
	}
	t.Fatalf("no request of the host entry carried a tool receipt, so the storm round never reached a second decision")
	return 0, 0, nil
}

// consumerBatchNotice reports the batch receipt the host entry's next request
// carries: a notice under no call identifier is the batch's, not a call's.
func consumerBatchNotice(t *testing.T, provider *intakeProvider) string {
	t.Helper()
	for _, request := range provider.requests() {
		for _, msg := range request.Messages {
			if msg.Role == string(model.RoleUser) && msg.ToolCallID == "" && strings.Contains(msg.Content, "was approved") {
				return msg.Content
			}
		}
	}
	t.Fatalf("no request of the host entry carried the batch receipt of the approved response")
	return ""
}

// fuseLimits declares the fuses the next assembled loop is constructed beside,
// and restores the declaration of the surrounding scene when the case ends: the
// host entry publishes no fuse seam of its own yet, so the declaration is the
// one the composition root makes for the process.
func fuseLimits(t *testing.T, limits agent.FuseLimits) {
	t.Helper()
	previous := agent.SetDefaultFuseLimits(limits)
	t.Cleanup(func() { agent.SetDefaultFuseLimits(previous) })
}

// responseStorm builds the calls of one storm response: every call names the
// one tool the agent carries and carries its own index, so a receipt of a
// refused call is readable by the identity it was answered under.
func responseStorm(prefix string) []model.ToolCall {
	calls := make([]model.ToolCall, 0, responseStormCalls)
	for at := 0; at < responseStormCalls; at++ {
		calls = append(calls, convCall(fmt.Sprintf("%s-%03d", prefix, at), "send", fmt.Sprintf(`{"n":%d}`, at)))
	}
	return calls
}

// responseStormRig assembles the storm runtime of one anchor: the agent that
// answers its opening request with the whole storm, under the deterministic
// stand-in governance.
func responseStormRig(t *testing.T, session string) (*assembly.Run, *convModel, *convTool, event.Address) {
	t.Helper()
	tool := &convTool{name: "send", output: "sent"}
	provider := &convModel{name: "model-cap", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(responseStorm("call-cap")...)
		}
		return convFinalTurn("done")
	}}
	return responseStormAssembly(t, provider, tool), provider, tool, event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)}
}

// responseStormAssembly assembles the fuse runtime through the public entry: one
// agent carrying the stormed tool, under the deterministic stand-in governance.
func responseStormAssembly(t *testing.T, provider *convModel, tool *convTool) *assembly.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: provider.name}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the fuse runtime: %v", err)
	}
	return run
}

// responseStormChain reports what the storm round added to the session chain:
// the action facts of the decision beside the result facts its admitted calls
// settled into. A call of a batch that did not run leaves neither, so the
// increment is the ruling's own bound rather than the size of the response.
func responseStormChain(t *testing.T, run *assembly.Run, addr event.Address) (int, int) {
	t.Helper()
	entries, _ := run.ReplayEntries(addr.SessionID)
	actions, results := 0, 0
	for _, entry := range entries {
		switch entry.Type {
		case event.TypeThinkingPlan:
			actions++
		case event.TypeActionCommand:
			results++
		}
	}
	return actions, results
}

// responseStormTasks reports how many tasks the storm round admitted, read off
// the task projection of the session: a call of a batch that did not run leaves
// no task behind, so the count is the admitted set.
func responseStormTasks(t *testing.T, run *assembly.Run, addr event.Address) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectTask},
	})
	if err != nil {
		t.Fatalf("read the tasks of %+v: %v", addr, err)
	}
	return len(snapshot.Details)
}
