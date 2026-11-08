// The generation-scoped consumers of the host: the native face a candidate
// generation rebuilds from its own document, the dialect of that document a
// per-generation consumer reads, and the unconditional self-registration that
// keeps one consumer set for a boot whose declaration grants none of them.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/runtime/state"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// newGenerationTestProvider answers one completed assistant message for every chat
// completion it serves: these tests read the assembled generation face, so the
// model output carries no assertion of its own.
func newGenerationTestProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"generation","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// generationDocument renders one host document over the scripted endpoint: the
// entry agent carries the given block, and the extra sections are appended as
// top-level keys.
func generationDocument(root, endpoint, agentBlock string, extra ...string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-a\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	if strings.TrimSpace(agentBlock) == "" {
		b.WriteString("  user: {}\n")
	} else {
		b.WriteString("  user:\n")
		b.WriteString(agentBlock)
	}
	b.WriteString("storage: { type: localfile, root: " + filepath.Join(root, "storage") + " }\n")
	for _, section := range extra {
		b.WriteString(section)
	}
	return b.String()
}

// bootGenerationHost writes one document and boots the host over it.
func bootGenerationHost(t *testing.T, configPath, document string) *Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	host, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close the host: %v", err)
		}
	})
	return host
}

// rewriteGenerationDocument writes one document over the configuration source a
// booted host reads.
func rewriteGenerationDocument(t *testing.T, configPath, document string) {
	t.Helper()
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("rewrite the configuration source: %v", err)
	}
}

// servedToolNames reports the tool names the served generation of one agent
// carries at one session.
func servedToolNames(t *testing.T, run *Run) []string {
	t.Helper()
	names, err := run.SessionToolNames(event.AgentID("user"), event.SessionID("session-generation"))
	if err != nil {
		t.Fatalf("read the served tool face: %v", err)
	}
	return names
}

// carriesTool reports whether one served tool face carries the named tool.
func carriesTool(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestHotReloadHostToolsNoRestart moves the tool declarations, the skill roots and the file face of a running host through the public reload entry.
//   - the reload publishes a new generation instead of refusing the move
//   - the served tool face of the new generation carries the tool the new document declares and carries none of the ones it dropped
//   - the boot generation's face is the one the boot document declared
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestHotReloadHostToolsNoRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	bootBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + root + "\"\n"
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", bootBlock))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bootFace := servedToolNames(t, host.Run())
	if !carriesTool(bootFace, tagentfile.ReadFileToolName) {
		t.Fatalf("boot tool face = %v, want the declared %q", bootFace, tagentfile.ReadFileToolName)
	}
	if carriesTool(bootFace, tagentfile.SaveFileToolName) {
		t.Fatalf("boot tool face = %v, want no %q the document never declared", bootFace, tagentfile.SaveFileToolName)
	}

	skills := filepath.Join(root, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatalf("create the skill root: %v", err)
	}
	movedBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.SaveFileToolName + " }\n" +
		"    skill_dirs: [\"" + skills + "\"]\n" +
		"    read_root: \"" + root + "\"\n" +
		"    file:\n      base_dir: \"" + filepath.Join(root, "moved") + "\"\n"
	if err := os.MkdirAll(filepath.Join(root, "moved"), 0o755); err != nil {
		t.Fatalf("create the moved base directory: %v", err)
	}
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", movedBlock))

	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved host faces answered %v, want the change published without a restart", err)
	}
	if !report.Publication.Published {
		t.Fatalf("publication = %+v, want the moved host faces published as a new generation", report.Publication)
	}
	movedFace := servedToolNames(t, host.Run())
	if !carriesTool(movedFace, tagentfile.SaveFileToolName) {
		t.Fatalf("served tool face = %v, want the %q the published document declares", movedFace, tagentfile.SaveFileToolName)
	}
	if carriesTool(movedFace, tagentfile.ReadFileToolName) {
		t.Fatalf("served tool face = %v, want no %q the published document dropped", movedFace, tagentfile.ReadFileToolName)
	}
}

// TestHotReloadMemoryNoRestart moves the memory section of a declared agent through the public reload entry.
//   - the read namespaces a generation recalls through are rebuilt by the face of that generation, so the reload publishes instead of refusing
//   - the memory tier the process opened once is still refused by name, because a moved tier would answer success for a storage no consumer reopens
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestHotReloadMemoryNoRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	namespaces := "    memory:\n      read_namespaces: [alice, bob]\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", namespaces))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved memory read namespaces answered %v, want the change published without a restart", err)
	}
	if !report.Publication.Published {
		t.Fatalf("publication = %+v, want the moved read namespaces published as a new generation", report.Publication)
	}

	movedTier := "    memory:\n      type: localfile\n      path: \"" + filepath.Join(root, "store") + "\"\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", movedTier))
	_, err = host.Reload(ctx)
	if !errors.Is(err, ErrHostReloadRestartRequired) {
		t.Fatalf("reload of a moved memory tier = %v, want the restart refusal", err)
	}
	if !strings.Contains(err.Error(), "agents.user.memory") {
		t.Fatalf("reload error = %v, want the memory path named", err)
	}
}

// TestHotReloadTimeoutNoRestart moves request_timeout_seconds of a running host.
//   - the reload publishes the new value instead of refusing it
//   - the dialect in effect answers the timeout the published document names, so the request path reads the value of the generation in effect
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestHotReloadTimeoutNoRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", "", "request_timeout_seconds: 30\n"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if got := host.specs.timeout(); got != 30*time.Second {
		t.Fatalf("timeout in effect = %s, want the declared 30s", got)
	}

	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", "", "request_timeout_seconds: 5\n"))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved request timeout answered %v, want the change taken without a restart", err)
	}
	if report.Publication.Generation != 1 {
		t.Fatalf("publication = %+v, want the generation the host booted at carried beside the moved dialect", report.Publication)
	}
	if got := host.specs.timeout(); got != 5*time.Second {
		t.Fatalf("timeout in effect = %s, want the 5s the published document names", got)
	}
}

// TestStorageRootStaysRestartTier keeps the root state named.
//   - the storage tier one process opened is never reopened by a generation
//   - a document that moves it is refused by the path an operator searches for
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestStorageRootStaysRestartTier(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	moved := generationDocument(root, provider.URL+"/v1", "")
	moved = strings.Replace(moved, "root: "+filepath.Join(root, "storage"), "root: "+filepath.Join(root, "elsewhere"), 1)
	rewriteGenerationDocument(t, configPath, moved)
	_, err := host.Reload(ctx)
	if !errors.Is(err, ErrHostReloadRestartRequired) {
		t.Fatalf("reload of a moved storage root = %v, want the restart refusal", err)
	}
	if !strings.Contains(err.Error(), "storage.root") {
		t.Fatalf("reload error = %v, want the storage root named", err)
	}
}

// TestReloadNonRootNoRestartLeft pins the restart-only list of a reload to the root state.
//   - every non-root section a generation rebuilds answers no name here
//   - the storage tier one process opened stays named
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestReloadNonRootNoRestartLeft(t *testing.T) {
	root := t.TempDir()
	write := func(name, agentBlock string, extra ...string) *hostDocument {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(generationDocument(root, "http://127.0.0.1:1/v1", agentBlock, extra...)), 0o644); err != nil {
			t.Fatalf("write the document %s: %v", name, err)
		}
		doc, err := loadHostDocument(path)
		if err != nil {
			t.Fatalf("load the document %s: %v", name, err)
		}
		return doc
	}
	skills := filepath.Join(root, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatalf("create the skill root: %v", err)
	}
	before := write("before.yaml", "")
	after := write("after.yaml",
		"    tools:\n      - { kind: tool, id: "+tagentfile.ReadFileToolName+" }\n"+
			"    skill_dirs: [\""+skills+"\"]\n"+
			"    read_root: \""+root+"\"\n"+
			"    file:\n      base_dir: \""+root+"\"\n"+
			"    memory:\n      read_namespaces: [alice]\n"+
			"    exec:\n      monitor:\n        interval: 3s\n",
		"request_timeout_seconds: 42\n")
	if changed := before.restartRequiredChanges(after); len(changed) != 0 {
		t.Fatalf("restart-required changes = %v, want the non-root sections rebuilt by their generation", changed)
	}
	if changed := before.restartRequiredChanges(write("dialect.yaml", "",
		"governance:\n  risk_rules: []\n",
		"telemetry:\n  otel_enabled: true\n")); len(changed) != 0 {
		t.Fatalf("restart-required changes = %v, want the host dialect sections rebuilt by their generation", changed)
	}
	meditation := write("meditation.yaml", "    meditation:\n      enabled: true\n      deliver_to: [user]\n      prompt_file: reflect.md\n")
	if changed := before.restartRequiredChanges(meditation); len(changed) != 0 {
		t.Fatalf("restart-required changes = %v, want the meditation loop rebuilt by its generation", changed)
	}
	tier := write("tier.yaml", "    memory:\n      type: localfile\n      path: \""+filepath.Join(root, "moved-store")+"\"\n")
	if changed := before.restartRequiredChanges(tier); len(changed) != 0 {
		t.Fatalf("restart-required changes = %v, want the memory declaration judged by its own consumer", changed)
	}
	storageRoot := write("storage.yaml", "")
	storageRoot.Storage.Root = filepath.Join(root, "elsewhere")
	if changed := before.restartRequiredChanges(storageRoot); len(changed) != 1 || changed[0] != "storage.root" {
		t.Fatalf("restart-required changes = %v, want the storage root alone named", changed)
	}
}

// TestHotReloadGovernanceNoRestart moves the governance rules and the observation face of a running host through the public reload entry.
//   - both sections publish as a new generation instead of being refused
//   - the governance face the run rules calls under is the policy the published generation's risk table opened, so a call the table grades is graded by it
//   - a call the table does not name keeps the default grade
//   - the observation face the run observes commits through is the observer the published section names
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestHotReloadGovernanceNoRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if policy := host.Run().governorInEffect(); policy != nil {
		t.Fatalf("governance face of the boot generation = %v, want none beside no declared risk table", policy)
	}
	if _, ok := host.Run().observerInEffect().(*telemetry.LogObserver); !ok {
		t.Fatalf("observation face of the boot generation = %T, want the log observer an OTel switch off declares", host.Run().observerInEffect())
	}

	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", "",
		"governance:\n  risk_rules:\n    - { tool: exec, level: high }\n",
		"telemetry:\n  otel_enabled: true\n"))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload of the moved governance rules answered %v, want the change published without a restart", err)
	}
	if !report.Publication.Published {
		t.Fatalf("publication = %+v, want the moved governance rules published as a new generation", report.Publication)
	}

	policy := host.Run().governorInEffect()
	if policy == nil {
		t.Fatal("governance face after the reload = none, want the policy the published risk table opened")
	}
	graded, err := policy.Judge(plugin.CallPass{Final: trpcmodel.ToolCall{Function: trpcmodel.FunctionDefinitionParam{Name: "exec"}}})
	if err != nil {
		t.Fatalf("rule a call of the declared tool: %v", err)
	}
	if graded.Risk != plugin.RiskHigh {
		t.Fatalf("graded risk of the declared tool = %v, want the %v the published table names", graded.Risk, plugin.RiskHigh)
	}
	ungraded, err := policy.Judge(plugin.CallPass{Final: trpcmodel.ToolCall{Function: trpcmodel.FunctionDefinitionParam{Name: "other"}}})
	if err != nil {
		t.Fatalf("rule a call of the undeclared tool: %v", err)
	}
	if ungraded.Risk != plugin.RiskMedium {
		t.Fatalf("graded risk of the undeclared tool = %v, want the default %v", ungraded.Risk, plugin.RiskMedium)
	}
	if _, ok := host.Run().observerInEffect().(*telemetry.SDKObserver); !ok {
		t.Fatalf("observation face after the reload = %T, want the SDK observer the published section declares", host.Run().observerInEffect())
	}
}

// TestHotReloadMeditationNoRestart moves the meditation section of a running host through the public reload entry.
//   - the reload resolves, judges and mounts the reflection loop of the published generation instead of refusing the move
//   - the face of that generation grants the cross-session send tool to the dedicated reflection session the plan names
//   - the boot generation's face carries that grant nowhere
//   - a section naming an undeclared delivery target refuses the candidate and keeps the loop in effect
//
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestHotReloadMeditationNoRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(filepath.Join(root, "reflect.md"), []byte("reflect on the turn"), 0o644); err != nil {
		t.Fatalf("write the reflection prompt: %v", err)
	}
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	enabled := "    meditation:\n      enabled: true\n      deliver_to: [user]\n      prompt_file: reflect.md\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", enabled))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload of the enabled meditation answered %v, want the loop mounted without a restart", err)
	}
	if !report.Publication.Published {
		t.Fatalf("publication = %+v, want the enabled meditation published as a new generation", report.Publication)
	}
	run := host.Run()
	plan := host.meditation.port.planInEffect()
	if plan == nil {
		t.Fatal("meditation plan in effect = none, want the plan the published generation declares")
	}
	reflection := servedToolNamesAt(t, run, event.AgentID("user"), event.SessionID(plan.Session))
	if !carriesTool(reflection, hostSendToolID) {
		t.Fatalf("reflection session %q tool face = %v, want the %q the plan grants", plan.Session, reflection, hostSendToolID)
	}
	if business := servedToolNamesAt(t, run, event.AgentID("user"), event.SessionID("session-generation")); carriesTool(business, hostSendToolID) {
		t.Fatalf("business session tool face = %v, want no %q the plan scopes to the reflection session", business, hostSendToolID)
	}
	if host.meditation.port.driverInEffect() == nil {
		t.Fatal("reflection driver in effect = none, want the driver the published generation mounted")
	}

	broken := "    meditation:\n      enabled: true\n      deliver_to: [ghost]\n      prompt_file: reflect.md\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", broken))
	if _, err := host.Reload(ctx); err == nil {
		t.Fatal("the reload of a meditation section naming an undeclared target answered success, want the fail-closed refusal")
	}
	if plan := host.meditation.port.planInEffect(); plan == nil || plan.Agent != "user" {
		t.Fatalf("meditation plan in effect after the refused candidate = %+v, want the plan of the generation in service", plan)
	}
}

// TestHotReloadMeditationAnchorSurvivesRestart pins the anchor record a reopened host resumes from.
//   - the reload that enables meditation points the run's watermark at the record its plan names instead of the store the boot wired beside no meditation
//   - the watermark that loop consumed is written to that record
//   - a restart over the same document and root reads that watermark back, so the reflection already consumed is not replayed
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestHotReloadMeditationAnchorSurvivesRestart(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(filepath.Join(root, "reflect.md"), []byte("reflect on the turn"), 0o644); err != nil {
		t.Fatalf("write the reflection prompt: %v", err)
	}
	booted := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	enabled := "    meditation:\n      enabled: true\n      deliver_to: [user]\n      prompt_file: reflect.md\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", enabled))
	if _, err := booted.Reload(ctx); err != nil {
		t.Fatalf("reload of the enabled meditation answered %v, want the loop mounted without a restart", err)
	}
	plan := booted.meditation.port.planInEffect()
	if plan == nil {
		t.Fatal("meditation plan in effect = none, want the plan the published generation declares")
	}
	gate := booted.Run().ReflectionGate()
	store, ok := gate.(*meditation.AnchorStore)
	if !ok {
		t.Fatalf("reflection gate in effect = %T, want the anchor store the generation mounted", gate)
	}
	if store.Path() != plan.Anchors {
		t.Fatalf("anchor record the loaded generation reads = %s, want the record its plan names %s", store.Path(), plan.Anchors)
	}
	consumed := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	if err := gate.Advance(ctx, consumed); err != nil {
		t.Fatalf("advance the watermark of a consumed reflection: %v", err)
	}
	if err := booted.Close(ctx); err != nil {
		t.Fatalf("close the host before the restart: %v", err)
	}

	restarted := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", enabled))
	restartedGate := restarted.Run().ReflectionGate()
	restartedStore, ok := restartedGate.(*meditation.AnchorStore)
	if !ok || restartedStore.Path() != plan.Anchors {
		t.Fatalf("anchor record the restarted host reads = %v, want the record the plan names %s", restartedGate, plan.Anchors)
	}
	resumed, err := restartedGate.Current(ctx)
	if err != nil {
		t.Fatalf("read the watermark after the restart: %v", err)
	}
	if !resumed.Moment.Equal(consumed) {
		t.Fatalf("watermark after the restart = %v, want the %v the consumed reflection left, else the restart replays that reflection", resumed.Moment, consumed)
	}
}

// servedToolNamesAt reports the tool names one session of one agent carries in
// the generation in effect.
func servedToolNamesAt(t *testing.T, run *Run, agent event.AgentID, session event.SessionID) []string {
	t.Helper()
	names, err := run.SessionToolNames(agent, session)
	if err != nil {
		t.Fatalf("read the served tool face of %s/%s: %v", agent, session, err)
	}
	return names
}

// TestConsumersSelfRegister pins the generation-scoped consumer set of a host.
//   - the execution consumer and the host's native face consumer are mounted on every boot, whichever declaration the document carries
//   - a reload that adds an agent carrying either face therefore takes effect without a restart
//   - the registry reports them in mount order beside the exclusive registration the generation in effect carries
//   - no consumer is named by a feature path of the assembly root: the mount is the option the consumer's own package answers
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
func TestConsumersSelfRegister(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", ""))

	gen := state.Generation(host.Run().publisher.Current().Generation)
	consumers := host.Run().GenerationConsumers(gen)
	if len(consumers) == 0 {
		t.Fatal("generation consumers = none, want the execution and host face consumers mounted unconditionally")
	}
	seen := map[string]GenerationConsumer{}
	mounted := make([]string, 0, len(consumers))
	for _, consumer := range consumers {
		seen[consumer.Name] = consumer
		mounted = append(mounted, consumer.Name)
	}
	for _, name := range []string{
		telemetryFaceConsumerName,
		governanceFaceConsumerName,
		memoryFaceConsumerName,
		meditationFaceConsumerName,
		"exec",
		hostFaceConsumerName,
	} {
		consumer, held := seen[name]
		if !held {
			t.Fatalf("generation consumers = %v, want %q mounted on a boot whose declaration names no consumer", mounted, name)
		}
		if want := state.GenLeaseKey(gen, name); consumer.Exclusive != want {
			t.Fatalf("consumer %q exclusive key = %q, want the frozen %q", name, consumer.Exclusive, want)
		}
	}
	if !seen["exec"].Registered {
		t.Fatalf("exec consumer = %+v, want the process-private ledger of the generation registered for its retirement", seen["exec"])
	}
	for _, name := range []string{telemetryFaceConsumerName, governanceFaceConsumerName, memoryFaceConsumerName, meditationFaceConsumerName, hostFaceConsumerName} {
		if seen[name].Registered {
			t.Fatalf("consumer %q = %+v, want no exclusive registration: the generation in effect replaced no face of an earlier one", name, seen[name])
		}
	}
	if mounted[len(mounted)-1] != hostFaceConsumerName {
		t.Fatalf("consumer mount order = %v, want %q last", mounted, hostFaceConsumerName)
	}
}

// TestHotReloadHostFaceFailClosed is the negative anchor of the host face: a candidate whose face cannot be constructed leaves the generation in service.
//   - a file face naming a base directory the checkout does not carry refuses the candidate before anything is published
//   - the publication stays at the generation in service and its tool face keeps serving the tools the document in effect declares
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
func TestHotReloadHostFaceFailClosed(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	bootBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + root + "\"\n"
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", bootBlock))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	before := host.Run().publisher.Current()

	brokenBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + filepath.Join(root, "missing-base-dir") + "\"\n"
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", brokenBlock))
	if _, err := host.Reload(ctx); err == nil {
		t.Fatal("the reload of an unbuildable host face answered success, want the fail-closed refusal")
	}
	if after := host.Run().publisher.Current(); after.Generation != before.Generation || after.Fingerprint != before.Fingerprint {
		t.Fatalf("publication after the refused candidate = %+v, want the generation %+v kept", after, before)
	}
	face := servedToolNames(t, host.Run())
	if !carriesTool(face, tagentfile.ReadFileToolName) {
		t.Fatalf("served tool face = %v, want the %q of the generation in service", face, tagentfile.ReadFileToolName)
	}
}

// TestBootAdoptNewConsumers restarts one host over the chain its first process published.
//   - the restarted process adopts the recorded generation instead of publishing a second one
//   - the adopted generation serves the tool face its document declares, because the adoption composes the per-generation face as well
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestBootAdoptNewConsumers(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	agentBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + root + "\"\n"
	document := generationDocument(root, provider.URL+"/v1", agentBlock)

	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	first, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the first host: %v", err)
	}
	published := first.Run().publisher.Current().Generation
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close the first host: %v", err)
	}

	second, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the adopting host: %v", err)
	}
	defer func() {
		if err := second.Close(ctx); err != nil {
			t.Errorf("close the adopting host: %v", err)
		}
	}()
	if adopted := second.Run().publisher.Current().Generation; adopted != published {
		t.Fatalf("adopted generation = %d, want the recorded %d served beside no second publication", adopted, published)
	}
	face := servedToolNames(t, second.Run())
	if !carriesTool(face, tagentfile.ReadFileToolName) {
		t.Fatalf("adopted tool face = %v, want the %q the adopted declaration grants", face, tagentfile.ReadFileToolName)
	}

	run := second.Run()
	adopted := state.Generation(published)
	mounted := make(map[string]GenerationConsumer, len(run.GenerationConsumers(adopted)))
	for _, consumer := range run.GenerationConsumers(adopted) {
		mounted[consumer.Name] = consumer
	}
	for _, name := range []string{"exec", hostFaceConsumerName} {
		consumer, held := mounted[name]
		if !held {
			t.Fatalf("the adopted generation mounts %v, want the %q consumer beside the writing path", mounted, name)
		}
		if want := state.GenLeaseKey(adopted, name); consumer.Exclusive != want {
			t.Fatalf("consumer %q exclusive key = %q, want the frozen %q", name, consumer.Exclusive, want)
		}
	}
	if !mounted["exec"].Registered {
		t.Fatalf("the adopted generation carries no exclusive execution registration: %+v", mounted)
	}
	if mounted[hostFaceConsumerName].Registered {
		t.Fatalf("the adopted face consumer registers an exclusive resource it never acquired: %+v", mounted[hostFaceConsumerName])
	}
	if _, err := run.CommittedEvent(ctx, generationRetireKey(adopted)); !errors.Is(err, ErrCommittedFactMissing) {
		t.Fatalf("the generation in effect carries a retirement record: %v", err)
	}
}

// TestReloadRecordsGenerationRetirement pins the retirement record the drain writes.
//   - the reload that supersedes a generation releases it at once and files the retirement under the frozen key beside the generation stamp
//   - the generation in effect carries no retirement record
//   - a later reload records only the generation it retired instead of filing the earlier one again
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestReloadRecordsGenerationRetirement(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	blocks := []string{
		"    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n",
		"    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
			"      - { kind: tool, id: " + tagentfile.ListFileToolName + " }\n",
		"    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
			"      - { kind: tool, id: " + tagentfile.SearchFileToolName + " }\n",
	}
	blocks[0] += "    file:\n      base_dir: \"" + root + "\"\n"
	blocks[1] += "    file:\n      base_dir: \"" + root + "\"\n"
	blocks[2] += "    file:\n      base_dir: \"" + root + "\"\n"
	host := bootGenerationHost(t, configPath, generationDocument(root, provider.URL+"/v1", blocks[0]))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := host.Run()
	first := run.publisher.Current().Generation
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", blocks[1]))
	if _, err := host.Reload(ctx); err != nil {
		t.Fatalf("reload the moved tool face: %v", err)
	}
	record, err := run.CommittedEvent(ctx, generationRetireKey(state.Generation(first)))
	if err != nil {
		t.Fatalf("read the retirement record of generation %d: %v", first, err)
	}
	if record.Type != event.TypeConfigChange {
		t.Fatalf("retirement record type = %q, want %q", record.Type, event.TypeConfigChange)
	}
	if got := record.Meta[event.MetaKeyConfigChangeDimension]; got != generationRetireDimension {
		t.Fatalf("retirement record dimension = %q, want %q", got, generationRetireDimension)
	}
	if got := record.Meta[event.MetaKeyConfigChangeGeneration]; got != strconv.FormatUint(first, 10) {
		t.Fatalf("retirement record generation = %q, want %q", got, strconv.FormatUint(first, 10))
	}
	serving := run.publisher.Current().Generation
	if _, err := run.CommittedEvent(ctx, generationRetireKey(state.Generation(serving))); !errors.Is(err, ErrCommittedFactMissing) {
		t.Fatalf("the generation in effect carries a retirement record: %v", err)
	}

	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", blocks[2]))
	if _, err := host.Reload(ctx); err != nil {
		t.Fatalf("reload the second tool move: %v", err)
	}
	if _, err := run.CommittedEvent(ctx, generationRetireKey(state.Generation(serving))); err != nil {
		t.Fatalf("read the retirement record of generation %d: %v", serving, err)
	}
	again, err := run.CommittedEvent(ctx, generationRetireKey(state.Generation(first)))
	if err != nil {
		t.Fatalf("read the first retirement record again: %v", err)
	}
	if again.Timestamp.UnixMilli() != record.Timestamp.UnixMilli() {
		t.Fatalf("the later reload derived the retirement of generation %d again (%v), want the record %v", first, again.Timestamp, record.Timestamp)
	}
}

// TestBootConvergesGenerationRetirement pins the crash window between a generation switch and the drain that retires the generation it replaced.
//   - a request holds the first generation across the reload, so the superseded generation cannot retire and the process ends without its record
//   - the restart adopts the published generation and records the retirement of the generation it left behind, with the frozen key and generation stamp
//   - a further restart re-reads that record instead of filing a second one, and the generation in effect carries no retirement record at all
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestBootConvergesGenerationRetirement(t *testing.T) {
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	bootBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + root + "\"\n"
	movedBlock := "    tools:\n      - { kind: tool, id: " + tagentfile.ReadFileToolName + " }\n" +
		"      - { kind: tool, id: " + tagentfile.ListFileToolName + " }\n" +
		"    file:\n      base_dir: \"" + root + "\"\n"
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", bootBlock))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	first, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the first host: %v", err)
	}
	run := first.Run()
	published := run.publisher.Current().Generation
	held, err := run.publisher.Borrow(ctx, event.AgentID("user"))
	if err != nil {
		t.Fatalf("hold the first generation: %v", err)
	}
	if uint64(held.Ref.Generation) != published {
		t.Fatalf("the held request borrowed generation %d, want the first generation %d", held.Ref.Generation, published)
	}
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", movedBlock))
	report, err := first.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved tool face: %v", err)
	}
	if !report.Publication.Published || report.Publication.Generation != published+1 {
		t.Fatalf("reload publication = %+v, want generation %d published beside the held one", report.Publication, published+1)
	}
	if _, err := run.CommittedEvent(ctx, generationRetireKey(state.Generation(published))); !errors.Is(err, ErrCommittedFactMissing) {
		t.Fatalf("the held generation carries a retirement record before its drain: %v", err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close the first host: %v", err)
	}

	second, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the converging host: %v", err)
	}
	restarted := second.Run()
	converged, err := restarted.CommittedEvent(ctx, generationRetireKey(state.Generation(published)))
	if err != nil {
		t.Fatalf("read the converged retirement record of generation %d: %v", published, err)
	}
	if converged.Type != event.TypeConfigChange {
		t.Fatalf("retirement record type = %q, want %q", converged.Type, event.TypeConfigChange)
	}
	if got := converged.Meta[event.MetaKeyConfigChangeDimension]; got != generationRetireDimension {
		t.Fatalf("retirement record dimension = %q, want %q", got, generationRetireDimension)
	}
	if got := converged.Meta[event.MetaKeyConfigChangeGeneration]; got != strconv.FormatUint(published, 10) {
		t.Fatalf("retirement record generation = %q, want %q", got, strconv.FormatUint(published, 10))
	}
	serving := restarted.publisher.Current().Generation
	if _, err := restarted.CommittedEvent(ctx, generationRetireKey(state.Generation(serving))); !errors.Is(err, ErrCommittedFactMissing) {
		t.Fatalf("the adopted generation carries a retirement record: %v", err)
	}
	if err := second.Close(ctx); err != nil {
		t.Fatalf("close the converging host: %v", err)
	}

	third, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the third host: %v", err)
	}
	defer func() {
		if err := third.Close(ctx); err != nil {
			t.Errorf("close the third host: %v", err)
		}
	}()
	if got := third.Run().publisher.Current().Generation; got != serving {
		t.Fatalf("the third boot serves generation %d, want the adopted %d", got, serving)
	}
	again, err := third.Run().CommittedEvent(ctx, generationRetireKey(state.Generation(published)))
	if err != nil {
		t.Fatalf("read the retirement record again: %v", err)
	}
	if again.Timestamp.UnixMilli() != converged.Timestamp.UnixMilli() {
		t.Fatalf("the restart derived the retirement of generation %d again (%v), want the record %v the chain already holds", published, again.Timestamp, converged.Timestamp)
	}
}
