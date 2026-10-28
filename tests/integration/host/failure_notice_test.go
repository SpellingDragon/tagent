// The failure-notice checks of the host: a turn whose model request really
// failed, and that produced no deliverable assistant body, delivers the raw
// provider error through the same sender every other turn delivers through, and
// leaves one payload-free fact behind. The reverse boundaries are pinned beside
// it: a success delivers its answer and no failure text, a host/protocol error
// is never read as a model failure, and a cancelled turn stays silent.
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

const (
	// failureProviderCause is the raw text the failing endpoint answers with;
	// the "500" marker is what every face must carry unchanged.
	failureProviderCause = "the provider answered 500: the upstream is unavailable"
	// failureProviderAnswer is the assistant body the answering endpoint serves.
	failureProviderAnswer = "the answer stands"
	// failureNoticeSummaryPrefix is the summary prefix every failure fact carries.
	failureNoticeSummaryPrefix = "failure-notice"
	// failureNoticeTruncationMark is the mark a cut notice ends with.
	failureNoticeTruncationMark = "（已截断）"
	// failureNoticeMaxBytes is the bound a delivered cause keeps before the mark.
	failureNoticeMaxBytes = 2 << 10
)

// failureProviderMode names what the endpoint answers one request with.
type failureProviderMode string

const (
	// failureModeFails answers a real server failure.
	failureModeFails failureProviderMode = "fails"
	// failureModeAnswers answers the terminal assistant body.
	failureModeAnswers failureProviderMode = "answers"
	// failureModeHangs parks the request until its caller gives up.
	failureModeHangs failureProviderMode = "hangs"
)

// failureProvider is the OpenAI-compatible endpoint the failure-notice checks
// drive: it records every request body and answers it in the mode the check
// switches, so the sender's actions and the model's requests are both read from
// what actually happened rather than from the declaration.
type failureProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	mode   failureProviderMode
	cause  string
	bodies []string
}

// newFailureProvider starts the endpoint in the given mode and closes it with
// the test.
func newFailureProvider(t *testing.T, mode failureProviderMode) *failureProvider {
	t.Helper()
	provider := &failureProvider{mode: mode, cause: failureProviderCause}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve records one request and answers it in the current mode.
func (p *failureProvider) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	p.mu.Lock()
	p.bodies = append(p.bodies, string(raw))
	mode, cause := p.mode, p.cause
	p.mu.Unlock()
	switch mode {
	case failureModeFails:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":%q,"type":"server_error"}}`, cause)
	case failureModeAnswers:
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"failure-notice","object":"chat.completion","created":1699200000,`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"`+failureProviderAnswer+`"},"finish_reason":"stop"}]}`)
	case failureModeHangs:
		<-r.Context().Done()
	}
}

// setCause changes the raw text the failing mode answers with.
func (p *failureProvider) setCause(cause string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cause = cause
}

// switchMode changes the answer every later request reads.
func (p *failureProvider) switchMode(mode failureProviderMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// requests reports the raw bodies the endpoint answered, in request order.
func (p *failureProvider) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.bodies...)
}

// requestCount reports how many requests the endpoint answered.
func (p *failureProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.bodies)
}

// endpoint reports the base URL the document's provider entry names.
func (p *failureProvider) endpoint() string { return p.server.URL + "/v1" }

// bootFailureHost boots the entry document the failure-notice checks drive: the
// bare entry agent resolved at the given endpoint over a temp storage root.
func bootFailureHost(t *testing.T, provider *failureProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the failure-notice host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = host.Close(closeCtx)
	})
	return host
}

// failureFactsOf reports the payload-free failure facts of one session, in
// commit order.
func failureFactsOf(host *assembly.Host, addr event.Address) []event.Event {
	var matched []event.Event
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type != event.TypeFeedback {
			continue
		}
		summary := fact.Meta[event.MetaKeyEventSummary]
		if strings.HasPrefix(summary, failureNoticeSummaryPrefix) {
			matched = append(matched, fact)
		}
	}
	return matched
}

// failureFactOf reports the one failure fact of the session, failing beside the
// count when none or several were committed.
func failureFactOf(t *testing.T, host *assembly.Host, addr event.Address) event.Event {
	t.Helper()
	facts := failureFactsOf(host, addr)
	if len(facts) != 1 {
		t.Fatalf("failure facts = %d, want exactly 1", len(facts))
	}
	return facts[0]
}

// failureActionsOf reports the outbound actions whose body carries the marker.
func failureActionsOf(sender *deliverySender, marker string) []deliveryAction {
	var matched []deliveryAction
	for _, action := range sender.actions() {
		if strings.Contains(action.body, marker) {
			matched = append(matched, action)
		}
	}
	return matched
}

// TestFailureNotice pins who is told, what is told, what fact is left, and every boundary that stays silent.
// - the raw cause reaches the user exactly once, bounded, beside no fixed wording
// - the fact carries no model payload and drives no turn
// - a success, a cancelled turn and a host error all stay silent
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFailureNotice(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	t.Run("raw_error_delivered_once", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeFails)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider refuses")
		if err != nil {
			t.Fatalf("the failed turn ended by error: %v; the model failure is a terminal, not a host error", err)
		}
		if turn.Terminal != model.KindFailed {
			t.Fatalf("turn terminal = %s, want the failed terminal of the provider refusal", turn.Terminal)
		}
		if !strings.Contains(turn.Failure, "500") {
			t.Fatalf("turn failure = %q, want the raw provider text carrying 500", turn.Failure)
		}

		actions := sender.actions()
		if len(actions) != 1 {
			t.Fatalf("delivered actions = %d, want exactly 1; bodies = %q", len(actions), sender.bodies())
		}
		delivered := actions[0]
		deliveryRequireAddress(t, delivered, deliveryAddr)
		if strings.TrimSpace(delivered.body) != strings.TrimSpace(turn.Failure) {
			t.Fatalf("delivered body = %q, want the raw cause %q the turn carries", delivered.body, turn.Failure)
		}
		if !strings.Contains(delivered.body, "500") {
			t.Fatalf("delivered body = %q, want the raw 500 identity of the provider failure", delivered.body)
		}
		if strings.Contains(delivered.body, "执行出错") || strings.Contains(delivered.body, "未产出交付内容") {
			t.Fatalf("delivered body = %q, want no fixed fallback sentence beside the raw cause", delivered.body)
		}
	})

	t.Run("a_long_cause_is_truncated_and_marked", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeFails)
		provider.setCause("服务不可用：" + strings.Repeat("上游拒绝", 600))
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider refuses at length")
		if err != nil {
			t.Fatalf("the failed turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindFailed || !strings.Contains(turn.Failure, "500") {
			t.Fatalf("turn = (%s, %q), want the failed terminal beside the raw 500 cause", turn.Terminal, turn.Failure)
		}
		actions := sender.actions()
		if len(actions) != 1 {
			t.Fatalf("delivered actions = %d, want exactly 1", len(actions))
		}
		body := actions[0].body
		if !strings.Contains(body, "500") {
			t.Fatalf("delivered body = %q, want the raw 500 identity kept in the truncated text", body)
		}
		if !strings.HasSuffix(body, failureNoticeTruncationMark) {
			t.Fatalf("delivered body ends with %q, want the truncation mark", body[len(body)-16:])
		}
		if !utf8.ValidString(body) {
			t.Fatalf("delivered body = %q, want the cut on a rune boundary", body)
		}
		if len(body) > failureNoticeMaxBytes+len(failureNoticeTruncationMark) {
			t.Fatalf("delivered body = %d bytes, want the 2KiB bound beside the mark", len(body))
		}
		if strings.Contains(body, "执行出错") {
			t.Fatalf("delivered body = %q, want no fixed fallback sentence beside the raw cause", body)
		}
	})

	t.Run("failure_fact_left_without_model_payload", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeFails)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider refuses")
		if err != nil {
			t.Fatalf("the failed turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindFailed {
			t.Fatalf("turn terminal = %s, want the failed terminal of the provider refusal", turn.Terminal)
		}

		fact := failureFactOf(t, host, deliveryAddr)
		if summary := fact.Meta[event.MetaKeyEventSummary]; !strings.HasPrefix(summary, failureNoticeSummaryPrefix+": ") {
			t.Fatalf("failure fact summary = %q, want the %q prefix beside the cause's first line", summary, failureNoticeSummaryPrefix)
		}
		if fact.Message.Content != "" || fact.Message.Role != "" || len(fact.Message.ToolCalls) > 0 {
			t.Fatalf("failure fact message = %+v, want no model payload", fact.Message)
		}
		if fact.InvocationID != turn.InvocationID {
			t.Fatalf("failure fact invocation = %q, want the failed invocation %q", fact.InvocationID, turn.InvocationID)
		}
		if got := fact.Meta[event.MetaPrefix+"terminal"]; got != model.KindFailed.String() {
			t.Fatalf("failure fact terminal = %q, want %q", got, model.KindFailed)
		}
		if got := fact.Meta[event.MetaPrefix+"failure_reason"]; got != turn.Failure {
			t.Fatalf("failure fact reason = %q, want the raw cause %q", got, turn.Failure)
		}
		if _, ok := fact.Meta[event.MetaKeyNoticeKind]; ok {
			t.Fatalf("failure fact carries %q, want no notice kind: a notice would drive a second turn", event.MetaKeyNoticeKind)
		}
		if _, ok := fact.Meta[event.MetaKeyTaskID]; ok {
			t.Fatalf("failure fact carries %q, want no task identity", event.MetaKeyTaskID)
		}

		for _, message := range host.Run().SessionHistory(deliveryAddr) {
			if strings.Contains(message.Content, "500") || strings.Contains(message.Content, "upstream is unavailable") {
				t.Fatalf("session history message = %+v, want the payload-free fact mapped onto no message", message)
			}
		}
		entries, _ := host.Run().ReplayEntries(deliveryAddr.SessionID)
		for _, entry := range entries {
			if strings.Contains(entry.Content, "500") || strings.Contains(entry.Content, "upstream is unavailable") {
				t.Fatalf("replay entry = %+v, want the payload-free fact projected onto no entry", entry)
			}
		}
	})

	t.Run("failure_fact_does_not_drive_a_turn", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeFails)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider refuses")
		if err != nil {
			t.Fatalf("the failed turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindFailed {
			t.Fatalf("turn terminal = %s, want the failed terminal of the provider refusal", turn.Terminal)
		}
		settled := provider.requestCount()
		delivered := len(sender.actions())

		time.Sleep(200 * time.Millisecond)
		if got := provider.requestCount(); got != settled {
			t.Fatalf("provider requests after the failure = %d, want the settled count %d: the fact drives no turn", got, settled)
		}
		if got := len(sender.actions()); got != delivered {
			t.Fatalf("delivered actions after the failure = %d, want the settled count %d", got, delivered)
		}

		provider.switchMode(failureModeAnswers)
		next, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks again after the provider recovers")
		if err != nil {
			t.Fatalf("the next turn ended by error: %v", err)
		}
		if next.Terminal != model.KindCompleted {
			t.Fatalf("next turn terminal = %s, want the completed terminal of the answer", next.Terminal)
		}
		if got := provider.requestCount(); got != settled+1 {
			t.Fatalf("provider requests = %d, want %d: the failure fact asks the model nothing", got, settled+1)
		}
		requests := provider.requests()
		if len(requests) != settled+1 {
			t.Fatalf("recorded requests = %d, want %d", len(requests), settled+1)
		}
		if strings.Contains(requests[len(requests)-1], "500") || strings.Contains(requests[len(requests)-1], "upstream is unavailable") {
			t.Fatalf("the next request carries the failure reason: %s", requests[len(requests)-1])
		}
		if actions := failureActionsOf(sender, failureProviderCause); len(actions) != 1 {
			t.Fatalf("failure bodies delivered = %d, want exactly the one of the failed turn", len(actions))
		}
	})

	t.Run("failed_turn_with_body_delivers_body_only", func(t *testing.T) {
		t.Skip("structurally unreachable at the host consumer: a decision runs one non-streaming request, so a failed outcome never retains a body; the body-first branch of deliverFresh is pinned by TestOutcomeCause/the reduction contract instead")
	})

	t.Run("success_turn_no_failure_notice", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeAnswers)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider answers")
		if err != nil {
			t.Fatalf("the successful turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %s, want the completed terminal of the answer", turn.Terminal)
		}
		if turn.Failure != "" {
			t.Fatalf("turn failure = %q, want no cause beside a completed terminal", turn.Failure)
		}
		actions := sender.actions()
		if len(actions) != 1 || strings.TrimSpace(actions[0].body) != failureProviderAnswer {
			t.Fatalf("delivered bodies = %q, want the answer %q alone", sender.bodies(), failureProviderAnswer)
		}
		if facts := failureFactsOf(host, deliveryAddr); len(facts) != 0 {
			t.Fatalf("failure facts = %d, want none beside a successful turn", len(facts))
		}
		if strings.Contains(actions[0].body, "500") || strings.Contains(actions[0].body, "执行出错") {
			t.Fatalf("delivered body = %q, want the answer with no failure text", actions[0].body)
		}
	})

	t.Run("host_error_not_a_model_failure", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeFails)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: event.Address{AgentID: "user"},
			Input:   []model.Message{{Role: model.RoleUser, Content: "an input the host refuses for its address"}},
		})
		if !errors.Is(err, assembly.ErrHostAddressMissing) {
			t.Fatalf("invoke error = %v, want the named host address error", err)
		}
		if turn.Terminal == model.KindFailed || turn.Failure != "" {
			t.Fatalf("turn = (%s, failure %q), want no model-failure terminal for a host error", turn.Terminal, turn.Failure)
		}
		if actions := sender.actions(); len(actions) != 0 {
			t.Fatalf("delivered bodies = %q, want no failure notice for a host error", sender.bodies())
		}
		if got := provider.requestCount(); got != 0 {
			t.Fatalf("provider requests = %d, want none: the host refused the invocation before any model request", got)
		}
	})

	t.Run("cancelled_turn_stays_silent", func(t *testing.T) {
		provider := newFailureProvider(t, failureModeHangs)
		sender := newDeliverySender(nil)
		host := bootFailureHost(t, provider, sender)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, "the user asks something the provider never answers")
		if err != nil {
			t.Fatalf("the cancelled turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindCancelled {
			t.Fatalf("turn terminal = %s, want the cancelled terminal of the abandoned request", turn.Terminal)
		}
		if turn.Failure != "" {
			t.Fatalf("turn failure = %q, want no cause beside a cancelled terminal", turn.Failure)
		}
		if actions := sender.actions(); len(actions) != 0 {
			t.Fatalf("delivered bodies = %q, want no notice for a cancelled turn", sender.bodies())
		}
		if facts := failureFactsOf(host, deliveryAddr); len(facts) != 0 {
			t.Fatalf("failure facts = %d, want none beside a cancelled turn", len(facts))
		}
	})
}
