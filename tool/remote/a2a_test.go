package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// TestEndpointDeclarationRefusesEveryUnresolvableTarget pins that a caller dials only what its declaration carries.
// - a target missing its destination or its agent name is refused before a declaration exists
// - one agent name declared with two destinations is refused by name
// - the same target declared twice is one declaration
// - a lookup of an undeclared name answers ErrTargetUndeclared
// - a lookup over no declaration answers ErrTargetUndeclared
// - a resolve answers the declared target when the name and the destination both match
// - a resolve over a destination that differs from the declaration answers ErrTargetUndeclared
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestEndpointDeclarationRefusesEveryUnresolvableTarget(t *testing.T) {
	alpha := Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:1/a2a"}
	beta := Target{AgentID: "beta", AgentURL: "http://127.0.0.1:1/a2a"}

	if _, err := NewEndpoints(Target{AgentID: "alpha"}); !errors.Is(err, ErrTargetMissing) {
		t.Fatalf("declaration without a destination = %v, want ErrTargetMissing", err)
	}
	if _, err := NewEndpoints(Target{AgentURL: "http://127.0.0.1:1/a2a"}); !errors.Is(err, ErrTargetMissing) {
		t.Fatalf("declaration without an agent name = %v, want ErrTargetMissing", err)
	}
	if _, err := NewEndpoints(alpha, Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:2/a2a"}); err == nil || !strings.Contains(err.Error(), `"alpha"`) {
		t.Fatalf("one agent declared with two destinations = %v, want it refused by name", err)
	}

	endpoints, err := NewEndpoints(alpha, beta, alpha)
	if err != nil {
		t.Fatalf("declaration = %v, want the repeated target folded", err)
	}
	if _, err := endpoints.Lookup("gamma"); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("lookup of an undeclared name = %v, want ErrTargetUndeclared", err)
	}
	var none *Endpoints
	if _, err := none.Lookup("alpha"); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("lookup over no declaration = %v, want ErrTargetUndeclared", err)
	}
	if resolved, err := endpoints.Resolve(alpha); err != nil || resolved != alpha {
		t.Fatalf("resolve of a declared target = (%+v, %v), want %+v", resolved, err, alpha)
	}
	drifted := Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:2/a2a"}
	if _, err := endpoints.Resolve(drifted); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("resolve of a drifted destination = %v, want ErrTargetUndeclared", err)
	}
}

// TestDispatcherRefusesEveryCallItCannotAttribute pins that a call is refused before the connector is asked to dial.
// - a dispatcher constructed without a connector is refused by name
// - a target missing its destination or its agent name is refused by name
// - a call missing any part of its identity is refused by name
// - a target outside the declaration is refused by name and nothing is dialled
// - a call over no declaration is refused by name and nothing is dialled
// - every refused call leaves no attribution record behind
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestDispatcherRefusesEveryCallItCannotAttribute(t *testing.T) {
	ctx := context.Background()
	alpha := Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:1/a2a"}
	beta := Target{AgentID: "beta", AgentURL: "http://127.0.0.1:1/a2a"}
	endpoints, err := NewEndpoints(alpha)
	if err != nil {
		t.Fatalf("declaration = %v", err)
	}

	if _, err := NewA2ADispatcher(nil); !errors.Is(err, ErrConnectorMissing) {
		t.Fatalf("dispatcher without a connector = %v, want ErrConnectorMissing", err)
	}

	connector := &countingConnector{err: errors.New("the call reached the network")}
	dispatcher, err := NewA2ADispatcher(connector, WithEndpoints(endpoints))
	if err != nil {
		t.Fatalf("dispatcher = %v", err)
	}
	identity := CallIdentity{InvocationID: "inv-1", DecisionID: "dec-1", CallID: "call-1"}

	if _, err := dispatcher.Send(ctx, Target{AgentID: "alpha"}, identity, testCallParams()); !errors.Is(err, ErrTargetMissing) {
		t.Fatalf("target without a destination = %v, want ErrTargetMissing", err)
	}
	for _, incomplete := range []CallIdentity{
		{DecisionID: "dec-1", CallID: "call-1"},
		{InvocationID: "inv-1", CallID: "call-1"},
		{InvocationID: "inv-1", DecisionID: "dec-1"},
	} {
		if _, err := dispatcher.Send(ctx, alpha, incomplete, testCallParams()); !errors.Is(err, ErrCallIdentityMissing) {
			t.Fatalf("identity %+v = %v, want ErrCallIdentityMissing", incomplete, err)
		}
	}
	if _, err := dispatcher.Send(ctx, beta, identity, testCallParams()); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("undeclared target = %v, want ErrTargetUndeclared", err)
	}
	undeclaredDispatcher, err := NewA2ADispatcher(connector)
	if err != nil {
		t.Fatalf("dispatcher over no declaration = %v", err)
	}
	if _, err := undeclaredDispatcher.Send(ctx, alpha, identity, testCallParams()); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("call over no declaration = %v, want ErrTargetUndeclared", err)
	}

	if got := connector.connectCount(); got != 0 {
		t.Fatalf("refused calls dialled the connector %d times, want 0", got)
	}
	if _, ok := dispatcher.Call(identity); ok {
		t.Fatalf("a refused call left an attribution record")
	}
}

// TestCallAttemptsKeepOneOutcomePerIdentity pins that retries are counted on one call and a repeated identity replays its outcome.
// - a call with a policy of three attempts sends three times and reports one settled record with three attempts
// - a call whose first attempt answers sends once and reports one attempt
// - a repeat of a settled identity answers the outcome it left without a second send
// - concurrent sends under one identity send once and answer the same outcome
// - an identity repeated against another declared target is refused by name without dialling it
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestCallAttemptsKeepOneOutcomePerIdentity(t *testing.T) {
	ctx := context.Background()

	t.Run("policy bounds the attempts", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "upstream is not ready")
		}))
		defer server.Close()
		dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(3))
		identity := CallIdentity{InvocationID: "inv-retry", DecisionID: "dec-retry", CallID: "call-retry"}

		result, err := dispatcher.Send(ctx, testTarget(server.URL), identity, testCallParams())
		if err == nil || result != nil {
			t.Fatalf("send over a failing server = (%v, %v), want a named failure", result, err)
		}
		if !strings.Contains(err.Error(), "failed after 3 attempts") || !strings.Contains(err.Error(), "alpha") {
			t.Fatalf("failure = %q, want the attempt count and the target", err)
		}
		if got := requests.Load(); got != 3 {
			t.Fatalf("server saw %d requests, want 3", got)
		}
		record, ok := dispatcher.Call(identity)
		if !ok || record.Attempts != 3 || !record.Settled || record.Identity != identity {
			t.Fatalf("record = %+v (present %v), want three settled attempts under the identity", record, ok)
		}
		if record.Target != testTarget(server.URL) {
			t.Fatalf("record target = %+v, want the resolved target", record.Target)
		}
	})

	t.Run("first attempt settles the call", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(messageServer(&requests, "ready"))
		defer server.Close()
		dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(3))
		identity := CallIdentity{InvocationID: "inv-ok", DecisionID: "dec-ok", CallID: "call-ok"}

		result, err := dispatcher.Send(ctx, testTarget(server.URL), identity, testCallParams())
		if err != nil || result == nil {
			t.Fatalf("send = (%v, %v), want a message result", result, err)
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("server saw %d requests, want 1", got)
		}
		record, ok := dispatcher.Call(identity)
		if !ok || record.Attempts != 1 || !record.Settled {
			t.Fatalf("record = %+v (present %v), want one settled attempt", record, ok)
		}

		again, err := dispatcher.Send(ctx, testTarget(server.URL), identity, testCallParams())
		if err != nil || again != result {
			t.Fatalf("repeat of a settled identity = (%v, %v), want the first result", again, err)
		}
		if got := requests.Load(); got != 1 {
			t.Fatalf("a repeat of the identity sent again: %d requests", got)
		}
	})

	t.Run("concurrent sends share one outcome", func(t *testing.T) {
		var requests atomic.Int32
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			<-release
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":"","result":{"kind":"message","messageId":"m-1","role":"agent","parts":[{"kind":"text","text":"shared"}]}}`)
		}))
		defer server.Close()
		dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(2))
		identity := CallIdentity{InvocationID: "inv-shared", DecisionID: "dec-shared", CallID: "call-shared"}

		const callers = 8
		results := make([]*a2aprotocol.MessageResult, callers)
		errs := make([]error, callers)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				results[index], errs[index] = dispatcher.Send(ctx, testTarget(server.URL), identity, testCallParams())
			}(i)
		}
		time.Sleep(20 * time.Millisecond)
		close(release)
		wg.Wait()

		if got := requests.Load(); got != 1 {
			t.Fatalf("concurrent sends made %d requests, want 1", got)
		}
		for i := range results {
			if errs[i] != nil || results[i] == nil {
				t.Fatalf("caller %d = (%v, %v), want the shared result", i, results[i], errs[i])
			}
			if results[i] != results[0] {
				t.Fatalf("caller %d answered a different result", i)
			}
		}
	})

	t.Run("identity bound to one target", func(t *testing.T) {
		alphaRequests := atomic.Int32{}
		betaRequests := atomic.Int32{}
		alphaServer := httptest.NewServer(messageServer(&alphaRequests, "alpha answered"))
		defer alphaServer.Close()
		betaServer := httptest.NewServer(messageServer(&betaRequests, "beta answered"))
		defer betaServer.Close()
		alpha := testTarget(alphaServer.URL)
		beta := Target{AgentID: "beta", AgentURL: betaServer.URL}
		endpoints, err := NewEndpoints(alpha, beta)
		if err != nil {
			t.Fatalf("declaration = %v", err)
		}
		dispatcher, err := NewA2ADispatcher(NativeConnector{}, WithEndpoints(endpoints), WithRetryPolicy(NewRetryPolicy(1)))
		if err != nil {
			t.Fatalf("dispatcher = %v", err)
		}
		identity := CallIdentity{InvocationID: "inv-conflict", DecisionID: "dec-conflict", CallID: "call-conflict"}
		if _, err := dispatcher.Send(ctx, alpha, identity, testCallParams()); err != nil {
			t.Fatalf("first send = %v", err)
		}
		if _, err := dispatcher.Send(ctx, beta, identity, testCallParams()); !errors.Is(err, ErrCallTargetConflict) {
			t.Fatalf("same identity on another target = %v, want ErrCallTargetConflict", err)
		}
		if got := betaRequests.Load(); got != 0 {
			t.Fatalf("the conflicting target was dialled %d times, want 0", got)
		}
	})
}

// TestRemoteCallErrorsStayClassified pins that a transport failure, a remote refusal and a timeout stay distinguishable.
// - a refused TCP connection keeps syscall.ECONNREFUSED in its error chain
// - a context deadline keeps context.DeadlineExceeded in its error chain and stops after one attempt
// - a remote refusal keeps the remote message text and matches none of the transport classes
// - a connect failure is not retried and leaves its attempt count at zero
// - a connector naming an incomplete target is refused by name before any URL is parsed
// - a connector handed an unusable URL reports it instead of opening a client
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestRemoteCallErrorsStayClassified(t *testing.T) {
	identity := CallIdentity{InvocationID: "inv-class", DecisionID: "dec-class", CallID: "call-class"}

	t.Run("refused connection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := server.URL
		server.Close()
		dispatcher := testDispatcher(t, url, NewRetryPolicy(2))
		_, err := dispatcher.Send(context.Background(), testTarget(url), identity, testCallParams())
		if !errors.Is(err, syscall.ECONNREFUSED) {
			t.Fatalf("refused connection = %v, want syscall.ECONNREFUSED in the chain", err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
		}))
		defer server.Close()
		dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(3))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := dispatcher.Send(ctx, testTarget(server.URL), identity, testCallParams())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline = %v, want context.DeadlineExceeded in the chain", err)
		}
		record, ok := dispatcher.Call(identity)
		if !ok || record.Attempts != 1 || !record.Settled {
			t.Fatalf("record = %+v (present %v), want one settled attempt", record, ok)
		}
	})

	t.Run("remote refusal", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":"","error":{"code":-32001,"message":"quota exhausted"}}`)
		}))
		defer server.Close()
		dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(2))
		_, err := dispatcher.Send(context.Background(), testTarget(server.URL), identity, testCallParams())
		if err == nil || !strings.Contains(err.Error(), "quota exhausted") || !strings.Contains(err.Error(), "alpha") {
			t.Fatalf("remote refusal = %v, want the remote text beside the target", err)
		}
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrTargetMissing) || errors.Is(err, ErrTargetUndeclared) ||
			errors.Is(err, ErrCallIdentityMissing) || errors.Is(err, ErrCallTargetConflict) {
			t.Fatalf("remote refusal matched a local or transport class: %v", err)
		}
		if got := requests.Load(); got != 2 {
			t.Fatalf("remote refusal saw %d attempts, want the declared 2", got)
		}
	})

	t.Run("connect failure is not retried", func(t *testing.T) {
		dialErr := errors.New("the destination is unreachable")
		connector := &countingConnector{err: dialErr}
		dispatcher, err := NewA2ADispatcher(connector, WithEndpoints(mustEndpoints(t, testTarget("http://127.0.0.1:1/a2a"))), WithRetryPolicy(NewRetryPolicy(3)))
		if err != nil {
			t.Fatalf("dispatcher = %v", err)
		}
		_, sendErr := dispatcher.Send(context.Background(), testTarget("http://127.0.0.1:1/a2a"), identity, testCallParams())
		if !errors.Is(sendErr, dialErr) {
			t.Fatalf("connect failure = %v, want the connector cause", sendErr)
		}
		if got := connector.connectCount(); got != 1 {
			t.Fatalf("connector dialled %d times, want 1", got)
		}
		record, ok := dispatcher.Call(identity)
		if !ok || record.Attempts != 0 || !record.Settled {
			t.Fatalf("record = %+v (present %v), want a settled call with no attempt", record, ok)
		}
	})

	if _, err := (NativeConnector{}).Connect(context.Background(), Target{AgentID: "alpha"}); !errors.Is(err, ErrTargetMissing) {
		t.Fatalf("connector over an incomplete target = %v, want ErrTargetMissing", err)
	}
	if _, err := (NativeConnector{}).Connect(context.Background(), Target{AgentID: "alpha", AgentURL: "://bad"}); err == nil ||
		!strings.Contains(err.Error(), "invalid agent URL") {
		t.Fatalf("connector over an unusable URL = %v, want the URL refusal", err)
	}
}

// TestSendHandoffCarriesTheEnvelopeBesideTheRequest pins the envelope on the wire:
// - a send carrying a handoff keeps the text part of its request and adds one data part after it
// - the data part decodes back into the envelope the call carried, and that envelope validates
// - the caller's params keep the message it built
// - a send carrying no handoff puts no data part on the wire
// - an envelope naming no intent or no acceptance constraint is refused before the target is dialled
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestSendHandoffCarriesTheEnvelopeBesideTheRequest(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":"","result":{"kind":"message","messageId":"m-1","role":"agent","parts":[{"kind":"text","text":"accepted"}]}}`)
	}))
	defer server.Close()
	wireBody := func(index int) string {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if index >= len(bodies) {
			t.Fatalf("server saw %d requests, want request %d", len(bodies), index)
		}
		return bodies[index]
	}

	dispatcher := testDispatcher(t, server.URL, NewRetryPolicy(1))
	envelope := agent.HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file"},
		Grants:           []string{"tool:exec_run"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             []event.EventID{"ev-context-1"},
	}
	params := testCallParams()
	identity := CallIdentity{InvocationID: "inv-handoff", DecisionID: "dec-handoff", CallID: "call-handoff"}
	result, err := dispatcher.SendHandoff(ctx, testTarget(server.URL), identity, envelope, params)
	if err != nil || result == nil {
		t.Fatalf("send carrying a handoff = (%v, %v), want the remote answer", result, err)
	}
	if got := ResultText(result); got != "accepted" {
		t.Fatalf("result text = %q, want the text the remote agent answered with", got)
	}
	if len(params.Message.Parts) != 1 {
		t.Fatalf("caller params carry %d parts, want the message it built kept", len(params.Message.Parts))
	}

	parts := requestParts(t, wireBody(0))
	if len(parts) != 2 {
		t.Fatalf("wire parts = %d, want the request text beside one handoff data part", len(parts))
	}
	if text := messageText(&a2aprotocol.Message{Parts: parts[:1]}); parts[0].GetKind() != a2aprotocol.KindText || text != "hello" {
		t.Fatalf("first wire part = %#v, want the request text the call sent", parts[0])
	}
	if parts[1].GetKind() != a2aprotocol.KindData {
		t.Fatalf("second wire part kind = %q, want %q", parts[1].GetKind(), a2aprotocol.KindData)
	}
	carried, err := agent.FromDataPart(parts[1])
	if err != nil {
		t.Fatalf("read the handoff from the wire = %v", err)
	}
	if !reflect.DeepEqual(carried, envelope) {
		t.Fatalf("wire envelope = %+v, want %+v", carried, envelope)
	}
	if err := carried.Validate(); err != nil {
		t.Fatalf("validate the wire envelope = %v, want it accepted", err)
	}

	plain := CallIdentity{InvocationID: "inv-plain", DecisionID: "dec-plain", CallID: "call-plain"}
	if _, err := dispatcher.Send(ctx, testTarget(server.URL), plain, testCallParams()); err != nil {
		t.Fatalf("send carrying no handoff = %v", err)
	}
	if parts := requestParts(t, wireBody(1)); len(parts) != 1 || parts[0].GetKind() != a2aprotocol.KindText {
		t.Fatalf("wire parts of a call carrying no handoff = %#v, want the request text only", parts)
	}

	connector := &countingConnector{err: errors.New("the call reached the network")}
	refusing, err := NewA2ADispatcher(connector, WithEndpoints(mustEndpoints(t, testTarget("http://127.0.0.1:1/a2a"))))
	if err != nil {
		t.Fatalf("dispatcher = %v", err)
	}
	refused := CallIdentity{InvocationID: "inv-refused", DecisionID: "dec-refused", CallID: "call-refused"}
	if _, err := refusing.SendHandoff(ctx, testTarget("http://127.0.0.1:1/a2a"), refused, agent.HandoffEnvelope{Constraints: []string{"an explicit verdict"}}, testCallParams()); !errors.Is(err, agent.ErrHandoffIntentMissing) {
		t.Fatalf("handoff naming no intent = %v, want %v", err, agent.ErrHandoffIntentMissing)
	}
	if _, err := refusing.SendHandoff(ctx, testTarget("http://127.0.0.1:1/a2a"), refused, agent.HandoffEnvelope{Intent: "review the change"}, testCallParams()); !errors.Is(err, agent.ErrHandoffConstraintsMissing) {
		t.Fatalf("handoff naming no constraint = %v, want %v", err, agent.ErrHandoffConstraintsMissing)
	}
	if _, err := refusing.SendHandoff(ctx, testTarget("http://127.0.0.1:1/a2a"), refused, nil, testCallParams()); !errors.Is(err, ErrHandoffMissing) {
		t.Fatalf("send naming no handoff = %v, want %v", err, ErrHandoffMissing)
	}
	if got := connector.connectCount(); got != 0 {
		t.Fatalf("a refused handoff dialled the connector %d times, want 0", got)
	}
	if _, ok := refusing.Call(refused); ok {
		t.Fatalf("a refused handoff left an attribution record")
	}
}

// requestParts reports the message parts of one JSON-RPC message/send body as the
// receiving side decodes them.
func requestParts(t *testing.T, body string) []a2aprotocol.Part {
	t.Helper()
	var request struct {
		Params struct {
			Message a2aprotocol.Message `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatalf("read the wire request = %v: %s", err, body)
	}
	return request.Params.Message.Parts
}

func testCallParams() a2aprotocol.SendMessageParams {
	return a2aprotocol.SendMessageParams{
		Message: a2aprotocol.NewMessage(a2aprotocol.MessageRoleUser, []a2aprotocol.Part{a2aprotocol.NewTextPart("hello")}),
	}
}

func testTarget(url string) Target {
	return Target{AgentID: "alpha", AgentURL: url}
}

func messageServer(requests *atomic.Int32, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":"","result":{"kind":"message","messageId":"m-1","role":"agent","parts":[{"kind":"text","text":%q}]}}`, text)
	}
}

func testDispatcher(t *testing.T, url string, policy RetryPolicy) *A2ADispatcher {
	t.Helper()
	dispatcher, err := NewA2ADispatcher(NativeConnector{}, WithEndpoints(mustEndpoints(t, testTarget(url))), WithRetryPolicy(policy))
	if err != nil {
		t.Fatalf("dispatcher = %v", err)
	}
	return dispatcher
}

func mustEndpoints(t *testing.T, targets ...Target) *Endpoints {
	t.Helper()
	endpoints, err := NewEndpoints(targets...)
	if err != nil {
		t.Fatalf("declaration = %v", err)
	}
	return endpoints
}

type countingConnector struct {
	connects atomic.Int32
	err      error
}

func (c *countingConnector) Connect(context.Context, Target, ...a2aclient.Option) (*a2aclient.A2AClient, error) {
	c.connects.Add(1)
	return nil, c.err
}

func (c *countingConnector) connectCount() int { return int(c.connects.Load()) }
