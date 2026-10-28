package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestHostBootWiresTheMeditationDriver boots a host whose meditation session carries deliver_summary, then closes it:
// - the boot accepts the document and the run serves an empty active-session set
// - Close stops the driver and returns no error
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestHostBootWiresTheMeditationDriver(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	root := t.TempDir()
	configPath := writeMeditationHostDocument(t, root, []string{"send"}, []string{"deliver_summary"})
	host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if host.FaceAddr() == "" {
		t.Fatal("the host face listens on no address")
	}
	if host.Run() == nil {
		t.Fatal("the host drives no run")
	}
	if sessions := host.Run().ActiveSessions("user"); len(sessions) != 0 {
		t.Fatalf("active user sessions = %+v, want none before the first turn", sessions)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestHostBootRefusesMisplacedMeditationTools checks that a tool granted to the wrong agent refuses the boot by name:
// - deliver_summary outside the meditation session is refused
// - send on the meditation session is refused
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestHostBootRefusesMisplacedMeditationTools(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	cases := []struct {
		name            string
		userTools       []string
		meditationTools []string
		want            string
	}{
		{"summary outside the meditation session", []string{"send", "deliver_summary"}, []string{"deliver_summary"}, "deliver_summary"},
		{"send on the meditation session", []string{"send"}, []string{"deliver_summary", "send"}, "send"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := writeMeditationHostDocument(t, root, tc.userTools, tc.meditationTools)
			host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
			if host != nil {
				t.Fatal("a refused boot returned a host")
			}
			if !errors.Is(err, assembly.ErrHostMeditationToolMisplaced) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("boot error = %v, want the misplaced %q refusal", err, tc.want)
			}
		})
	}
}

// writeMeditationHostDocument writes one host document over the temp root,
// granting the named tools to the entry agent and to the meditation session,
// and answers its path. The meditation prompt the plan reads is written beside
// it.
func writeMeditationHostDocument(t *testing.T, root string, userTools, meditationTools []string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: scripted\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"http://127.0.0.1:1/v1\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    tools:\n")
	for _, name := range userTools {
		b.WriteString("      - { kind: tool, id: " + name + " }\n")
	}
	b.WriteString("  meditator:\n")
	b.WriteString("    tools:\n")
	for _, name := range meditationTools {
		b.WriteString("      - { kind: tool, id: " + name + " }\n")
	}
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [user] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [user]\n")
	b.WriteString("      deliver_to: [user]\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the session"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	return configPath
}

// recordingSender is the outbound message stand-in of the boot checks: it
// accepts every body and records the address, the body and the result of each
// action it answered. The zero value accepts and records nothing, so the checks
// that only need a sender keep using it as a value; newRecordingSender carries
// the ledger a check reads back.
type recordingSender struct {
	ledger *senderLedger
}

// Send accepts one body and records the action beside its result.
func (s recordingSender) Send(ctx context.Context, target event.Address, body string) error {
	if s.ledger == nil {
		return nil
	}
	return s.ledger.record(ctx, target, body)
}

// actions reports the actions the sender answered in delivery order, as a copy
// the caller owns.
func (s recordingSender) actions() []senderAction {
	if s.ledger == nil {
		return nil
	}
	return s.ledger.actionsRecorded()
}

// bodies reports the recorded bodies in delivery order, as a copy the caller
// owns.
func (s recordingSender) bodies() []string {
	recorded := s.actions()
	out := make([]string, 0, len(recorded))
	for _, action := range recorded {
		out = append(out, action.body)
	}
	return out
}

// newRecordingSender returns the stand-in beside the ledger it records into.
func newRecordingSender() recordingSender {
	return recordingSender{ledger: &senderLedger{}}
}

// newGatedRecordingSender returns the stand-in beside the gate that parks every
// outbound action until the check releases it, so a check reads a tool on the
// outbound face before it returns to the model.
func newGatedRecordingSender() (recordingSender, *senderGate) {
	gate := newSenderGate()
	return recordingSender{ledger: &senderLedger{gate: gate}}, gate
}

// senderLedger records the actions one sender answered.
type senderLedger struct {
	mu      sync.Mutex
	actions []senderAction
	gate    *senderGate
}

// senderAction is one outbound action: the address it was addressed to, the
// body it carried and the result the sender answered. pending marks an action
// the sender is still holding, so its confirmation reads as unconfirmed.
type senderAction struct {
	target  event.Address
	body    string
	pending bool
	result  error
}

// confirmed reports whether the sender really answered this action: the action
// returned and the result carried no refusal.
func (a senderAction) confirmed() bool { return !a.pending && a.result == nil }

// record appends one action, parks it beside the gate when the check declared
// one, and answers with the result the action settled on.
func (l *senderLedger) record(ctx context.Context, target event.Address, body string) error {
	l.mu.Lock()
	index := len(l.actions)
	l.actions = append(l.actions, senderAction{target: target, body: body, pending: true})
	gate := l.gate
	l.mu.Unlock()
	if gate != nil {
		gate.arrive()
		select {
		case <-gate.release:
		case <-ctx.Done():
			l.resolve(index, ctx.Err())
			return ctx.Err()
		}
	}
	l.resolve(index, nil)
	return nil
}

// resolve records the result one action settled on.
func (l *senderLedger) resolve(index int, result error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.actions[index].pending = false
	l.actions[index].result = result
}

// actionsRecorded reports the recorded actions in delivery order, as a copy
// the caller owns.
func (l *senderLedger) actionsRecorded() []senderAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]senderAction(nil), l.actions...)
}

// senderGate parks every outbound action until the check releases it.
type senderGate struct {
	arrivals chan struct{}
	release  chan struct{}
	once     sync.Once
}

// newSenderGate returns the gate beside its arrival and release channels.
func newSenderGate() *senderGate {
	return &senderGate{arrivals: make(chan struct{}, 8), release: make(chan struct{})}
}

// arrive reports one action waiting at the gate.
func (g *senderGate) arrive() { g.arrivals <- struct{}{} }

// wait waits until one action reached the gate.
func (g *senderGate) wait(t *testing.T, what string) {
	t.Helper()
	select {
	case <-g.arrivals:
	case <-time.After(60 * time.Second):
		t.Fatalf("no outbound action reached the gate while waiting for %s", what)
	}
}

// releaseAll lets every parked action answer.
func (g *senderGate) releaseAll() { g.once.Do(func() { close(g.release) }) }
