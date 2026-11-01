// The instance symptom this change exists for, read from the one anchor it owes: an
// event that arrives at a non-entry agent's session is driven through one round by the
// resident instance of that address.
//
// - the arrival is issued beside a caller that never reads its answer, so the round is read back from the chain of the address rather than from a returned turn
// - the round a resident instance drives lands under the segment identity that instance minted for its address, never under the identity the arriving call names
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
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
)

// instanceEntryAgent, instanceEntryModel, instanceWorkerAgent, instanceWorkerModel,
// instanceSession and instanceInvocation name the material the instance anchor is written
// in: one entry agent and one agent that is no entry at all. instanceArrival is the body
// the arriving event carries, and instanceAnswer the body the scripted model answers with.
const (
	instanceEntryAgent  = "user"
	instanceEntryModel  = "model-instance-entry"
	instanceWorkerAgent = "worker"
	instanceWorkerModel = "model-instance-worker"
	instanceSession     = "session-instances"
	instanceInvocation  = "inv-instances-arrival"

	instanceArrival = "instances: the event that arrives at the non-entry session"
	instanceAnswer  = "instances: the non-entry session answered"
)

// instanceWindow bounds every wait of the instance anchor, so a round nothing drives
// answers the named gap instead of holding the run.
const instanceWindow = 20 * time.Second

// TestInstancesSymptomEveryAgentResident drives one round of an event arriving at a non-entry agent's session through the resident instance of its address.
// - positive: the address runs a round beside an arriving event, and the round answers with the scripted body
// - positive: the round lands under the segment identity the resident instance of the address minted, so the instance is the driver of its own address
// - negative: the round does not land under the identity the arriving call names, because a caller's own drive is no drive of the address
// - consumer: the arrival is one Host.Invoke over a host booted from a real document, and the round is read back from the committed chain of the address
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestInstancesSymptomEveryAgentResident(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newInstanceProvider(t)
	host := instanceBoot(t, provider)
	worker := event.Address{AgentID: instanceWorkerAgent, SessionID: instanceSession}

	go func() {
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: instanceInvocation,
			Address:      worker,
			Input:        []model.Message{{Role: model.RoleUser, Content: instanceArrival}},
		})
	}()

	output := instanceAwaitOutput(t, host, worker, instanceWindow)
	if !strings.Contains(output.Message.Content, instanceAnswer) {
		t.Fatalf("the round of %s answered %q, want the body the scripted model answered %q with:\n%s",
			worker.AgentID, output.Message.Content, instanceAnswer, instanceDescribe(provider.requests()))
	}
	if !instanceSegmentOf(worker, output.InvocationID) {
		t.Fatalf("the round of the non-entry session landed under the invocation %q, want the segment identity its resident instance mints (%q#c1): "+
			"the round was run by the caller's own synchronous direct drive, so the address holds no instance of its own:\n%s",
			output.InvocationID, worker.SessionID, instanceDescribe(provider.requests()))
	}
	if output.InvocationID == instanceInvocation {
		t.Fatalf("the round of the non-entry session landed under the identity the arriving call named (%q), want the identity of the instance that drove it: "+
			"a call that names an invocation is no instance of the address it names", output.InvocationID)
	}
}

// instanceSegmentOf reports whether one invocation identity is a segment identity of the
// named address: the identity the resident instance of that address mints for the round it
// drives, and never the identity a caller names.
func instanceSegmentOf(addr event.Address, invocation event.InvocationID) bool {
	return strings.HasPrefix(string(invocation), string(addr.SessionID)+"#c")
}

// instanceAwaitOutput waits for the output fact of one address inside the anchor window,
// so a session no instance drives answers the named gap instead of a timeout alone.
func instanceAwaitOutput(t *testing.T, host *assembly.Host, addr event.Address, window time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(window)
	for {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeAgentOutput {
				return fact
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("waited %s for the round of the non-entry session %s, want it driven beside the event that arrived: "+
				"the address holds no resident consumption instance, so an arriving event is driven by nobody",
				window, addr.SessionID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// instanceProvider is the OpenAI-compatible endpoint the instance anchor boots against: it
// records every request and answers the scripted body, so a round is read from the request
// the model served and from the output fact the address committed.
type instanceProvider struct {
	server *httptest.Server

	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
}

// newInstanceProvider starts the endpoint and closes it with the test.
func newInstanceProvider(t *testing.T) *instanceProvider {
	t.Helper()
	provider := &instanceProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// endpoint reports the base URL the host document's provider entry names.
func (p *instanceProvider) endpoint() string { return p.server.URL + "/v1" }

// serve answers one chat completion with the scripted body and records the request it
// carried.
func (p *instanceProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	p.mu.Lock()
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, intakeFinalAnswer(instanceAnswer))
}

// requests reports the requests the endpoint served, in request order.
func (p *instanceProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// instanceDescribe renders the requests the anchor reads its evidence from.
func instanceDescribe(requests []intakeRequest) string { return intakeDescribe(requests) }

// instanceHostDocument renders the document the instance anchor boots: one entry agent
// beside one agent that is no entry, both on their own model.
func instanceHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + instanceEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + instanceEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + instanceEntryAgent + ":\n")
	b.WriteString("    model: " + instanceEntryModel + "\n")
	b.WriteString("  " + instanceWorkerAgent + ":\n")
	b.WriteString("    model: " + instanceWorkerModel + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// instanceBoot boots the formal host the anchor drives, so the arrival below reaches the
// runtime through the public entry the deployment uses.
func instanceBoot(t *testing.T, provider *instanceProvider) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(instanceHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(&intakeSender{}))
	if err != nil {
		t.Fatalf("boot the instance host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the instance host ended its close beside: %v", err)
		}
	})
	return host
}
