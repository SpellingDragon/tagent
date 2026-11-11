// The chaos leaf of event-guided hot update, read from the public boot and reload
// entry: one running host moves every generation-scoped consumer at once while a
// turn is still under way, and the whole organization switches as one generation
// without a restart, without tearing the turn off the generation it borrowed and
// without leaking the generation the switch retired.
package hotupdate

import (
	"context"
	"errors"
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
	tagentfile "github.com/SpellingDragon/tagent/tool/file"
)

// generationRetireKey is the frozen key the assembly files one generation's
// retirement under, beside the metadata keys that state which generation it was.
// The check reads the spellings as frozen wire names and never re-derives them.
const (
	generationRetireKey       = "gen-retire:%d"
	generationRetireDimension = "generation-retire"
	metaConfigChangeDimension = "config_change_dimension"
	metaConfigChangeGen       = "config_change_generation"
)

// fullListStages is the consumer state one full-list document declares: every
// generation-scoped consumer of the host, in the form the two stages of the
// chaos differ by.
type fullListStages struct {
	// SkillRoot is the skill directory the generation reads.
	SkillRoot string
	// ReadRoot is the root the recall read face of the generation walks.
	ReadRoot string
	// FileBase is the base directory of the declared file face.
	FileBase string
	// Tools lists the declared tool ids of the entry agent.
	Tools []string
	// Timeout is the request timeout the generation serves under.
	Timeout int
}

// fullListProvider answers one completed assistant message for every chat
// completion it serves, and holds the first completion that arrives after the
// chaos arms it: the held completion is the turn under way when the organization
// switches.
type fullListProvider struct {
	server *httptest.Server

	mu      sync.Mutex
	armed   bool
	held    bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// newFullListProvider starts the scripted provider of one chaos run.
func newFullListProvider(t *testing.T) *fullListProvider {
	t.Helper()
	provider := &fullListProvider{entered: make(chan struct{}), release: make(chan struct{})}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		provider.mu.Lock()
		hold := provider.armed && !provider.held
		if hold {
			provider.held = true
		}
		provider.mu.Unlock()
		if hold {
			close(provider.entered)
			<-provider.release
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"hotupdate","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(provider.server.Close)
	t.Cleanup(provider.finish)
	return provider
}

// arm makes the provider hold the next completion it is asked for.
func (p *fullListProvider) arm() {
	p.mu.Lock()
	p.armed = true
	p.mu.Unlock()
}

// finish lets the held completion go, once.
func (p *fullListProvider) finish() {
	p.once.Do(func() { close(p.release) })
}

// endpoint reports the OpenAI-compatible endpoint the provider serves.
func (p *fullListProvider) endpoint() string { return p.server.URL + "/v1" }

// fullListDocument renders the host document of one stage: the entry agent
// declaring the tool face, the skill roots, the read root, the file face, the
// memory read namespaces, the reflection loop and the execution face, beside the
// organization-level observation, governance and request-timeout sections.
func fullListDocument(root, endpoint string, stage fullListStages) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-a\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString(fmt.Sprintf("request_timeout_seconds: %d\n", stage.Timeout))
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("governance:\n  risk_rules:\n    - { tool: " + stage.Tools[0] + ", level: high }\n")
	b.WriteString("telemetry:\n  otel_enabled: true\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    tools:\n")
	for _, tool := range stage.Tools {
		b.WriteString("      - { kind: tool, id: " + tool + " }\n")
	}
	b.WriteString("    exec:\n      monitor:\n        interval: 2s\n")
	b.WriteString("    memory:\n      read_namespaces: [" + strings.Join(stage.Tools, ", ") + "]\n")
	b.WriteString("    meditation:\n      enabled: true\n      deliver_to: [user]\n      prompt_file: reflect.md\n")
	b.WriteString("    skill_dirs: [\"" + stage.SkillRoot + "\"]\n")
	b.WriteString("    read_root: \"" + stage.ReadRoot + "\"\n")
	b.WriteString("    file:\n      base_dir: \"" + stage.FileBase + "\"\n")
	b.WriteString("storage: { type: localfile, root: " + filepath.Join(root, "storage") + " }\n")
	return b.String()
}

// writeDocument makes one document the source the host reads.
func writeDocument(t *testing.T, path, document string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
}

// invokeTurn drives one turn of the entry agent at its own session and answers
// the terminal it reached.
func invokeTurn(t *testing.T, ctx context.Context, host *assembly.Host, session string) agentTurn {
	t.Helper()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: "user", SessionID: event.SessionID(session)},
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the host at %s: %v", session, err)
	}
	return agentTurn{Terminal: turn.Terminal}
}

// agentTurn is the terminal one turn reached.
type agentTurn struct {
	// Terminal is the terminal the turn ended at.
	Terminal model.Kind
}

// retirementOf reads the retirement record of one generation from the chain the
// run holds.
func retirementOf(ctx context.Context, run *assembly.Run, generation uint64) (event.Event, error) {
	return run.CommittedEvent(ctx, event.EventID(fmt.Sprintf(generationRetireKey, generation)))
}

// TestHotReloadFullList is the chaos anchor of the full consumer list.
//   - a running host moves every non-root section at once, while one turn is still under way
//   - the moved sections are the tool face, the skill roots, the read root, the file face, the memory read namespaces and the request timeout
//   - the reflection loop, the execution face, the governance rules and the observation face move with them
//   - the reload publishes one generation instead of refusing the move
//   - the turn under way keeps the generation it borrowed and finishes on it
//   - the retired generation is not released while that turn holds it: no retirement record exists yet
//   - the drain releases it once the turn ends, recording exactly one retirement of exactly that generation
//   - the new generation serves the moved tool face, and the host closes clean
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestHotReloadFullList(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newFullListProvider(t)
	root := t.TempDir()
	boot := fullListStages{
		SkillRoot: filepath.Join(root, "skills-boot"),
		ReadRoot:  root,
		FileBase:  root,
		Tools:     []string{tagentfile.ReadFileToolName},
		Timeout:   60,
	}
	moved := fullListStages{
		SkillRoot: filepath.Join(root, "skills-moved"),
		ReadRoot:  filepath.Join(root, "read-moved"),
		FileBase:  filepath.Join(root, "file-moved"),
		Tools:     []string{tagentfile.SaveFileToolName, tagentfile.ListFileToolName},
		Timeout:   30,
	}
	for _, dir := range []string{boot.SkillRoot, moved.SkillRoot, moved.ReadRoot, moved.FileBase} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create the declared directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "reflect.md"), []byte("reflect on the turn"), 0o644); err != nil {
		t.Fatalf("write the reflection prompt: %v", err)
	}
	configPath := filepath.Join(root, "tagent.yaml")
	writeDocument(t, configPath, fullListDocument(root, provider.endpoint(), boot))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	host, err := assembly.Boot(configPath, assembly.WithSender(fullListSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close the host: %v", err)
		}
	}()
	run := host.Run()
	if turn := invokeTurn(t, ctx, host, "session-full-list"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the boot generation", turn.Terminal)
	}

	provider.arm()
	inFlight := make(chan agentTurn, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: event.Address{AgentID: "user", SessionID: "session-under-way"},
			Input:   []model.Message{{Role: model.RoleUser, Content: "hold this turn"}},
		})
		if err != nil {
			inFlight <- agentTurn{Terminal: model.KindFailed}
			return
		}
		inFlight <- agentTurn{Terminal: turn.Terminal}
	}()
	select {
	case <-provider.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the turn under way never reached the provider")
	}

	writeDocument(t, configPath, fullListDocument(root, provider.endpoint(), moved))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the full consumer list answered %v, want the change published without a restart", err)
	}
	if !report.Publication.Published || report.Publication.Generation == 0 {
		t.Fatalf("publication = %+v, want the moved consumer list published as a new generation", report.Publication)
	}
	serving := report.Publication.Generation
	retired := serving - 1
	if _, err := retirementOf(ctx, run, retired); !errors.Is(err, assembly.ErrCommittedFactMissing) {
		t.Fatalf("generation %d carries a retirement record while a turn holds it: %v", retired, err)
	}

	provider.finish()
	if turn := <-inFlight; turn.Terminal != model.KindCompleted {
		t.Fatalf("the turn under way ended %v, want the completed turn of the generation it borrowed", turn.Terminal)
	}
	record, err := retirementOf(ctx, run, retired)
	if err != nil {
		t.Fatalf("read the retirement record of generation %d: %v", retired, err)
	}
	if got := record.Meta[metaConfigChangeDimension]; got != generationRetireDimension {
		t.Fatalf("retirement record dimension = %q, want %q", got, generationRetireDimension)
	}
	if got := record.Meta[metaConfigChangeGen]; got != fmt.Sprintf("%d", retired) {
		t.Fatalf("retirement record generation = %q, want %d", got, retired)
	}

	face, err := run.SessionToolNames(event.AgentID("user"), event.SessionID("session-full-list"))
	if err != nil {
		t.Fatalf("read the served tool face: %v", err)
	}
	for _, want := range moved.Tools {
		if !carriesTool(face, want) {
			t.Fatalf("served tool face = %v, want the %q the published document declares", face, want)
		}
	}
	if carriesTool(face, tagentfile.ReadFileToolName) {
		t.Fatalf("served tool face = %v, want no %q the published document dropped", face, tagentfile.ReadFileToolName)
	}
	if turn := invokeTurn(t, ctx, host, "session-full-list"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn of the published generation terminal = %v, want completed", turn.Terminal)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	closed = true
}

// fullListSender accepts every outbound body the host hands it: the chaos reads
// the generation the switch serves, not the delivery it answered, so the sender
// files nothing of its own.
type fullListSender struct{}

// Send accepts one outbound body.
func (fullListSender) Send(context.Context, event.Address, string) error { return nil }

// carriesTool reports whether one served tool face carries the named tool.
func carriesTool(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
