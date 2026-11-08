// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// The delivery consumer anchor, read from the public boot entry rather than an assembly helper: a body the outbound channel definitively refused stands owed on the chain, and a restart through assembly.Boot redelivers it exactly once through the new process's sender.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
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
)

// restartDeliveryBody is the one assistant answer the scripted provider serves.
const restartDeliveryBody = "the owed restart body"

// TestDeliveryRestartRedelivery pins the owed redelivery through the public boot.
//   - The first boot's sender definitively refuses the answer, so the body stands owed (held) instead of delivered: the refusal is the negative leg
//   - A second boot over the same chain restores the owed obligation and redelivers the body exactly once through its own sender, with no second invoke
func TestDeliveryRestartRedelivery(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	provider := newRestartDeliveryProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	document := "entry: user\n" +
		"provider: local\n" +
		"model: model-entry\n" +
		"providers:\n" +
		"  local: { provider: openai, api_endpoint: \"" + provider.endpoint() + "\", api_key_env: TAGENT_TEST_API_KEY }\n" +
		"agents:\n" +
		"  user: {}\n" +
		"storage: { type: localfile, root: " + root + " }\n"
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-restart-redelivery"}

	refused := &refusingDeliverySender{}
	first, err := assembly.Boot(configPath, assembly.WithSender(refused))
	if err != nil {
		t.Fatalf("boot the first host: %v", err)
	}
	turn, err := first.Invoke(ctx, assembly.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the first boot: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("the first turn terminal = %v, want the completed answer the channel then refused", turn.Terminal)
	}
	waitForRestartDelivery(t, "the refused answer to stand owed", func() bool {
		return len(first.HeldDeliveries()) > 0
	})
	if refused.attempts() == 0 {
		t.Fatal("the first boot never attempted the send, so the refusal leg did not run")
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close the first host beside the owed body: %v", err)
	}

	recorder := &recordingDeliverySender{}
	second, err := assembly.Boot(configPath, assembly.WithSender(recorder))
	if err != nil {
		t.Fatalf("boot over the owed chain: %v", err)
	}
	defer second.Close(ctx)
	waitForRestartDelivery(t, "the restart to redeliver the owed body", func() bool {
		return recorder.count(restartDeliveryBody) > 0
	})
	if got := recorder.count(restartDeliveryBody); got != 1 {
		t.Fatalf("the restart redelivered the owed body %d times, want exactly once", got)
	}
}

// waitForRestartDelivery polls one condition until it holds or the budget runs out.
func waitForRestartDelivery(t *testing.T, what string, reached func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if reached() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// restartDeliveryProvider is the OpenAI-compatible endpoint the boot anchor runs
// against: every chat completion answers the one terminal body.
type restartDeliveryProvider struct{ server *httptest.Server }

// newRestartDeliveryProvider starts the endpoint and closes it with the test.
func newRestartDeliveryProvider(t *testing.T) *restartDeliveryProvider {
	t.Helper()
	provider := &restartDeliveryProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		encoded, err := json.Marshal(restartDeliveryBody)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":`+string(encoded)+`},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

// endpoint reports the URL the host document names.
func (p *restartDeliveryProvider) endpoint() string { return p.server.URL }

// refusingDeliverySender definitively refuses every send, so the answer stands
// owed instead of delivered.
type refusingDeliverySender struct {
	mu       sync.Mutex
	attemptN int
}

// Send records the attempt and answers the channel's definite refusal.
func (s *refusingDeliverySender) Send(_ context.Context, _ event.Address, _ string) error {
	s.mu.Lock()
	s.attemptN++
	s.mu.Unlock()
	return restartDeliveryRefusal{}
}

// attempts reports how many sends the channel was asked for.
func (s *refusingDeliverySender) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attemptN
}

// restartDeliveryRefusal is a definite channel refusal: not a timeout, so the
// account records it as retrying and the bounded redrive owns it.
type restartDeliveryRefusal struct{}

// Error renders the refusal.
func (restartDeliveryRefusal) Error() string { return "the outbound channel refused the connection" }

// Timeout reports false: a refusal is not a timeout.
func (restartDeliveryRefusal) Timeout() bool { return false }

// Temporary reports the refusal as retryable.
func (restartDeliveryRefusal) Temporary() bool { return true }

// recordingDeliverySender accepts and records every body the restart delivers.
type recordingDeliverySender struct {
	mu     sync.Mutex
	bodies []string
}

// Send records one accepted body.
func (s *recordingDeliverySender) Send(_ context.Context, _ event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, content)
	return nil
}

// count reports how many times one body was delivered.
func (s *recordingDeliverySender) count(content string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, body := range s.bodies {
		if body == content {
			n++
		}
	}
	return n
}
