// 契约: docs/wiki/plugin/plugin-architecture.md
//
// The run-level plugin set one assembly publishes: the sequence of factories a
// caller contributes beside the host's own, what a factory that fails leaves
// behind, and the set a reload keeps.
package assembly

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// pluginAssemblyObserver is the run-level face these checks contribute: it
// records every committed fact the fact->observation pass dispatches it.
type pluginAssemblyObserver struct {
	name string
	mu   sync.Mutex
	seen []event.EventID
}

// Name returns the stable name of this instance.
func (o *pluginAssemblyObserver) Name() string { return o.name }

// Port reports the port this instance serves.
func (o *pluginAssemblyObserver) Port() plugin.Port { return plugin.PortFactObservation }

// ObserveCommit records one committed fact the observation pass dispatched.
func (o *pluginAssemblyObserver) ObserveCommit(_ context.Context, id event.EventID) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, id)
	return nil
}

// observed reports the facts the pass dispatched, in dispatch order.
func (o *pluginAssemblyObserver) observed() []event.EventID {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.EventID(nil), o.seen...)
}

// TestFailedPluginFactoryReleasesCompletedFactories pins what a factory that fails leaves behind.
//   - the sequence stops there: the factories declared after it never run and no factory runs twice
//   - the failure names the position the factory holds beside the cause it answered, so the cause is not swallowed
//   - the run carries no instance of a completed factory while the sequence continues, and the boot mounts nothing it could not build
//   - consumer: the public Boot entry, which refuses the boot rather than answer a host beside the contribution it dropped
func TestFailedPluginFactoryReleasesCompletedFactories(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	document := generationDocument(root, provider.URL+"/v1", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	completed := &pluginAssemblyObserver{name: "plugin-assembly-completed"}
	cause := errors.New("the second factory refuses to build")
	builds := make([]int, 3)
	mounted := -1
	host, err := Boot(configPath,
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			builds[0]++
			return []plugin.Instance{completed}, nil
		}),
		WithPlugins(func(run *Run) ([]plugin.Instance, error) {
			builds[1]++
			mounted = len(run.plugins)
			return nil, cause
		}),
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			builds[2]++
			return nil, nil
		}),
	)
	if host != nil {
		t.Fatalf("boot answered a host beside the factory that failed: %v", host)
	}
	if err == nil {
		t.Fatal("boot succeeded although the second contributed factory failed")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("boot failure %v does not carry the cause the factory answered", err)
	}
	if !strings.Contains(err.Error(), "factory 2 of ") {
		t.Fatalf("boot failure %q names no position for the factory that failed", err)
	}
	if builds[0] != 1 || builds[1] != 1 {
		t.Fatalf("the factories up to the failure ran %v times, want one run of each", builds)
	}
	if builds[2] != 0 {
		t.Fatalf("the factory after the failure ran %d times, want the sequence stopped there", builds[2])
	}
	if mounted != 0 {
		t.Fatalf("the run carried %d instance(s) of the completed factory while the next factory ran, want none mounted before the sequence completes", mounted)
	}
}

// TestBootPublishesContributedPluginsAheadOfItsOwn pins the order the boot applies.
//   - the caller's factories answer the head of the published set in the order the caller declared them
//   - the instances the host mounts itself follow them, so a contribution composes beside the host's own set instead of replacing it
//   - each contributed factory runs exactly once per boot, so neither a skipped nor a repeated construction can produce the same list
//   - consumer: the public Boot entry and the run-level set it publishes
func TestBootPublishesContributedPluginsAheadOfItsOwn(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	document := generationDocument(root, provider.URL+"/v1", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	first := &pluginAssemblyObserver{name: "plugin-assembly-first"}
	second := &pluginAssemblyObserver{name: "plugin-assembly-second"}
	builds := make([]int, 2)
	host, err := Boot(configPath,
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			builds[0]++
			return []plugin.Instance{first}, nil
		}),
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			builds[1]++
			return []plugin.Instance{second}, nil
		}),
	)
	if err != nil {
		t.Fatalf("boot the host beside the two contributed factories: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, host) })
	plugins := host.Run().plugins
	if len(plugins) < 3 {
		t.Fatalf("the published set holds %d instance(s), want the two contributions beside the host's own", len(plugins))
	}
	if plugins[0] != first || plugins[1] != second {
		t.Fatalf("the published set starts with %v and %v, want the contributions in the order the caller declared them", plugins[0], plugins[1])
	}
	if builds[0] != 1 || builds[1] != 1 {
		t.Fatalf("the contributed factories ran %v times, want one run of each", builds)
	}
}

// TestReloadKeepsContributedPluginsObservingCommits pins the generation scope of the contributed set.
//   - the contributed factory builds once, and the reload that publishes a new generation neither runs it again nor replaces the instance it answered
//   - that instance observes the commits of the turn driven after the reload, so the set the boot published is the one the new generation dispatches
//   - the reload publishes off a moved binding field, so the pass that keeps observing is one set across two generations
//   - consumer: the public Boot entry beside the public Reload entry and the Run.Invoke turn it publishes
func TestReloadKeepsContributedPluginsObservingCommits(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	document := generationDocument(root, provider.URL+"/v1", "")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	contributed := &pluginAssemblyObserver{name: "plugin-assembly-reload"}
	builds := 0
	host, err := Boot(configPath, WithPlugins(func(*Run) ([]plugin.Instance, error) {
		builds++
		return []plugin.Instance{contributed}, nil
	}))
	if err != nil {
		t.Fatalf("boot the host beside the contributed instance: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, host) })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-plugin-assembly-reload"}
	drive := func(invocation string) {
		t.Helper()
		turn, err := host.Run().Invoke(ctx, InvokeRequest{
			InvocationID:  event.InvocationID(invocation),
			Address:       addr,
			TriggerSource: "user",
			Input:         []model.Message{{Role: model.RoleUser, Content: "the turn " + invocation}},
		})
		if err != nil {
			t.Fatalf("drive the turn %s: %v", invocation, err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("terminal of the turn %s = %q, want the completed answer", invocation, turn.Terminal)
		}
	}
	drive("inv-plugin-assembly-reload-before")
	before := len(contributed.observed())
	if before == 0 {
		t.Fatal("the contributed instance observed no fact before the reload, so the check cannot tell a kept instance from one the reload replaced")
	}
	rewriteGenerationDocument(t, configPath, generationDocument(root, provider.URL+"/v1", "    replay_window: 40\n"))
	report, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the moved binding field: %v", err)
	}
	if !report.Publication.Published {
		t.Fatalf("publication = %+v, want the moved binding field published as a new generation", report.Publication)
	}
	drive("inv-plugin-assembly-reload-after")
	observed := contributed.observed()
	if len(observed) <= before {
		t.Fatalf("the contributed instance observed %d fact(s) in total, want more than the %d it observed before the reload", len(observed), before)
	}
	if builds != 1 {
		t.Fatalf("the contributed factory ran %d times, want the one build of the boot the reload leaves in place", builds)
	}
	for _, id := range observed[before:] {
		if _, err := host.Run().CommittedEvent(ctx, id); err != nil {
			t.Fatalf("read the fact %s the reloaded generation dispatched: %v", id, err)
		}
	}
}

// contributedRuler is the per-agent face these checks contribute in the native
// face: it records every call pass the published pipeline asks it to rule.
type contributedRuler struct {
	name  string
	mu    sync.Mutex
	asked []plugin.CallPass
}

// Name returns the stable name of this instance.
func (r *contributedRuler) Name() string { return r.name }

// Port reports the port this instance serves.
func (r *contributedRuler) Port() plugin.Port { return plugin.PortCallRuling }

// Judge records one pass and allows the call.
func (r *contributedRuler) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, pass)
	return plugin.Decision{Disposition: plugin.DispositionAllow}, nil
}

// ruled counts the passes the pipeline asked this instance to rule.
func (r *contributedRuler) ruled() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.asked)
}

// isContributedRuler names the contributed instance in one resolved pipeline.
func isContributedRuler(instance plugin.Instance) bool {
	_, ok := instance.(*contributedRuler)
	return ok
}

// boundPlugins reads the plugin pipeline of the binding generation in effect
// for one agent: the order the next request executes with.
func boundPlugins(t *testing.T, ctx context.Context, run *Run, agent event.AgentID) []plugin.Instance {
	t.Helper()
	handle, err := run.bindings.Borrow(ctx, agent)
	if err != nil {
		t.Fatalf("borrow the published binding of %s: %v", agent, err)
	}
	defer func() { _ = handle.Release(ctx) }()
	return append([]plugin.Instance(nil), handle.Binding.Plugins...)
}

// contributedRuledRun assembles one run whose agent-a carries the contributed
// ruler beside the ruled tool, over a provider that calls the tool on the first
// decision of every turn and settles on the second. It answers the run beside
// the native face it was assembled over, so a later publication keeps the very
// contribution the disable declaration names.
func contributedRuledRun(t *testing.T, ctx context.Context, ruler *contributedRuler, tool *pipelineRuledTool, holder *owedSlice, unmount *func() error) (*Run, map[event.AgentID]NativeAgent) {
	t.Helper()
	round := 0
	var mu sync.Mutex
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		mu.Lock()
		defer mu.Unlock()
		round++
		if round%2 == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-contributed",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      tool.name,
							Arguments: []byte(`{}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}}
	natives := map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}, Plugins: []plugin.Instance{ruler}},
	}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}, natives,
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
		withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
			dispose, err := run.RegisterStateSlice(holder)
			if err != nil {
				return err
			}
			*unmount = dispose
			_, err = run.Publish(ctx, decl, agents)
			return err
		}))
	if err != nil {
		t.Fatalf("assemble the run beside the contributed instance: %v", err)
	}
	return run, natives
}

// TestDisabledContributionKeepsItsObligations pins what a plugins: declaration owes when it disables a contributed instance.
//   - the contributed ruler rules the call the turn makes, so the published pipeline carries it
//   - a publication that moves another binding field keeps it, so the generation replacement re-resolves the declaration
//   - the disabling declaration leaves it out of the agent's pipeline while the run-level set stays mounted, without a restart
//   - the instance rules nothing after the disable, and the call it does not see still executes under the run-level policy
//   - the slice the contribution held keeps its name, its payload and its refusal, so nothing is handed to an unnamed successor
//   - negative: a kept instance, a deleted holder or a cut that stopped stating its payload fails one of the five checks
//   - consumer: the public Assemble entry, the Run.Invoke turn it serves, the public Run.Publish entry, RegisterStateSlice and Checkpoint
//
// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
func TestDisabledContributionKeepsItsObligations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const sliceName = "assembly.disabled-contribution"
	const owedItem = "the-notice-of-the-disabled-contribution"
	ruler := &contributedRuler{name: "contributed-ruler"}
	tool := &pipelineRuledTool{name: "contributed-ruled"}
	holder := &owedSlice{name: sliceName}
	holder.open(owedItem)
	var unmount func() error
	run, natives := contributedRuledRun(t, ctx, ruler, tool, holder, &unmount)
	defer run.stopCheckpointFiler()
	if !carriesInstance(boundPlugins(t, ctx, run, "agent-a"), isContributedRuler) {
		t.Fatalf("the published pipeline of agent-a = %v, want the instance the host contributed in the native face", boundPlugins(t, ctx, run, "agent-a"))
	}
	pipelineRuledInvoke(t, ctx, run, "inv-contribution-before-disable")
	if got := ruler.ruled(); got != 1 {
		t.Fatalf("the contributed ruler was asked %d time(s) before the disable, want the one ruling of the call", got)
	}
	if got := tool.executions(); got != 1 {
		t.Fatalf("the ruled call executed %d time(s) before the disable, want the one execution of the ruling that allowed it", got)
	}

	replaced, err := run.Publish(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 40}},
	}, natives)
	if err != nil {
		t.Fatalf("publish the generation that moves the replay window: %v", err)
	}
	if replaced.Result != state.PublishApplied {
		t.Fatalf("publication = %+v, want the moved binding field published as a new generation", replaced)
	}
	if carried := boundPlugins(t, ctx, run, "agent-a"); !carriesInstance(carried, isContributedRuler) {
		t.Fatalf("the published pipeline of agent-a = %v, want the contributed instance kept across the generation the declaration does not name it in", carried)
	}
	pipelineRuledInvoke(t, ctx, run, "inv-contribution-after-replacement")
	if got := ruler.ruled(); got != 2 {
		t.Fatalf("the contributed ruler was asked %d time(s) across two generations, want the second ruling of the kept instance", got)
	}

	outcome, err := run.Publish(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60, Plugins: []string{"-" + ruler.name}}},
	}, natives)
	if err != nil {
		t.Fatalf("publish the declaration that disables the contributed instance: %v", err)
	}
	if outcome.Result != state.PublishApplied {
		t.Fatalf("publication = %+v, want the disabling declaration published as a new generation", outcome)
	}
	if carried := boundPlugins(t, ctx, run, "agent-a"); carriesInstance(carried, isContributedRuler) {
		t.Fatalf("the published pipeline of agent-a = %v, want the disabled instance out of it", carried)
	}
	if !carriesInstance(run.plugins, isCompressionConsumer) {
		t.Fatalf("the run-level set = %v, want it mounted beside the disabling declaration", run.plugins)
	}

	pipelineRuledInvoke(t, ctx, run, "inv-contribution-after-disable")
	if got := ruler.ruled(); got != 2 {
		t.Fatalf("the contributed ruler was asked %d time(s) in total, want the 2 it was asked before the disable", got)
	}
	if got := tool.executions(); got != 3 {
		t.Fatalf("the ruled call executed %d time(s) in total, want the third execution beside the disabled instance", got)
	}

	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut after the disabling declaration = (%t, %v), want a filed cut", filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, run, key)
	if payload := slicePayloadOf(t, cut, sliceName); !strings.Contains(payload, owedItem) {
		t.Fatalf("the cut states %q for %s, want the obligation %s the disabled contribution carried nowhere", payload, sliceName, owedItem)
	}
	refusal := unmount()
	if refusal == nil || !strings.Contains(refusal.Error(), "1 unfinished obligation(s)") || !strings.Contains(refusal.Error(), sliceName) {
		t.Fatalf("the unmount of the slice the contribution disabled = %v, want a refusal naming the slice and its 1 unfinished obligation(s)", refusal)
	}
	if names := run.StateRegistry().Names(); !sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the refused unmount, want the slice kept with its obligation", names)
	}
}

// TestRunLevelContributionDisableIsRefusedByName pins the scope of a declaration entry naming a run-level contribution.
//   - the declaration is refused by name: the run-level set is process-scoped, so no agent declaration narrows it
//   - the publication in effect stays the one the attempt met, so the refused declaration turns nothing off
//   - the slice the contribution held keeps its name, its payload and its refusal, so no obligation leaves its holder
//   - negative: a declaration answered as a new generation, a cut without the obligation or a deleted holder fails
//   - consumer: the public Assemble entry, the public Run.Publish entry, RegisterStateSlice's disposer and Checkpoint
//
// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
func TestRunLevelContributionDisableIsRefusedByName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const sliceName = "assembly.run-level-contribution"
	const owedItem = "the-notice-of-the-run-level-contribution"
	const instanceName = "run-level-contribution"
	contributed := &pluginAssemblyObserver{name: instanceName}
	holder := &owedSlice{name: sliceName}
	holder.open(owedItem)
	var unmount func() error
	natives := map[event.AgentID]NativeAgent{"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}}, Done: true}
	}}}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}, natives,
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			return []plugin.Instance{contributed}, nil
		}),
		withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
			dispose, err := run.RegisterStateSlice(holder)
			if err != nil {
				return err
			}
			unmount = dispose
			_, err = run.Publish(ctx, decl, agents)
			return err
		}))
	if err != nil {
		t.Fatalf("assemble the run beside the run-level contribution: %v", err)
	}
	defer run.stopCheckpointFiler()
	if !carriesInstance(run.plugins, func(instance plugin.Instance) bool { return instance == plugin.Instance(contributed) }) {
		t.Fatalf("the run-level set = %v, want the instance the caller contributed", run.plugins)
	}
	commitSessionTurns(t, ctx, run, event.Address{AgentID: "agent-a", SessionID: "session-run-level-contribution"}, "run-level-contribution", 1)
	before := run.publisher.Current().Generation

	if _, err := run.Publish(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60, Plugins: []string{"-" + instanceName}}},
	}, natives); err == nil || !strings.Contains(err.Error(), instanceName) || !strings.Contains(err.Error(), "run-level") {
		t.Fatalf("the declaration disabling the run-level contribution = %v, want a refusal naming %s and its run-level scope", err, instanceName)
	}
	if after := run.publisher.Current().Generation; after != before {
		t.Fatalf("the publication in effect after the refused declaration = generation %d, want the %d the attempt met", after, before)
	}
	if !carriesInstance(run.plugins, func(instance plugin.Instance) bool { return instance == plugin.Instance(contributed) }) {
		t.Fatalf("the run-level set = %v after the refused declaration, want the contribution kept", run.plugins)
	}

	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut after the refused declaration = (%t, %v), want a filed cut", filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, run, key)
	if payload := slicePayloadOf(t, cut, sliceName); !strings.Contains(payload, owedItem) {
		t.Fatalf("the cut states %q for %s, want the obligation %s the refused disable left with its holder", payload, sliceName, owedItem)
	}
	refusal := unmount()
	if refusal == nil || !strings.Contains(refusal.Error(), "1 unfinished obligation(s)") || !strings.Contains(refusal.Error(), sliceName) {
		t.Fatalf("the unmount of the slice the refused declaration named = %v, want a refusal naming the slice and its 1 unfinished obligation(s)", refusal)
	}
	if names := run.StateRegistry().Names(); !sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the refused unmount, want the slice kept with its obligation", names)
	}
}

// TestRestartedBootCarriesNoContributionItWasNotGiven pins the scope of the run-level contribution across a restart.
//   - the first boot mounts the instance the caller contributes beside the host's own set, and the factory builds once
//   - the restarted boot adopts the recorded generation and carries no instance of that contribution
//   - the adoption is what the restart serves, so the missing instance is the scope, not a publication that dropped it
//   - consumer: the public Boot entry over one configuration source, twice
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
func TestRestartedBootCarriesNoContributionItWasNotGiven(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(generationDocument(root, provider.URL+"/v1", "")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	contributed := &pluginAssemblyObserver{name: "plugin-assembly-process"}
	isProcessContribution := func(instance plugin.Instance) bool { return instance == plugin.Instance(contributed) }
	builds := 0
	first, err := Boot(configPath, WithPlugins(func(*Run) ([]plugin.Instance, error) {
		builds++
		return []plugin.Instance{contributed}, nil
	}))
	if err != nil {
		t.Fatalf("boot the process beside the contributed instance: %v", err)
	}
	if !carriesInstance(first.Run().plugins, isProcessContribution) {
		t.Fatalf("the run-level set of the first boot = %v, want the contributed instance", first.Run().plugins)
	}
	if builds != 1 {
		t.Fatalf("the contributed factory ran %d time(s), want the one build of the boot", builds)
	}
	published := first.Run().publisher.Current().Generation
	closeLiveness(t, first)

	second, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the restarted process over the root of the first: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, second) })
	if adopted := second.Run().publisher.Current().Generation; adopted != published {
		t.Fatalf("the restarted boot serves generation %d, want the recorded %d it adopted", adopted, published)
	}
	if carriesInstance(second.Run().plugins, isProcessContribution) {
		t.Fatalf("the run-level set of the restarted boot = %v, want no instance of a contribution the process never received", second.Run().plugins)
	}
	if !carriesInstance(second.Run().plugins, isCompressionConsumer) {
		t.Fatalf("the run-level set of the restarted boot = %v, want the builtin instance every boot mounts", second.Run().plugins)
	}
	if builds != 1 {
		t.Fatalf("the contributed factory ran %d time(s) across both boots, want the one build of the boot that contributed it", builds)
	}
}
