// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
package assembly

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// carriesInstance reports whether one resolved pipeline carries a member the
// matcher admits.
func carriesInstance(instances []plugin.Instance, wanted func(plugin.Instance) bool) bool {
	for _, instance := range instances {
		if wanted(instance) {
			return true
		}
	}
	return false
}

// countInstances counts the members of one resolved pipeline a matcher admits.
func countInstances(instances []plugin.Instance, wanted func(plugin.Instance) bool) int {
	count := 0
	for _, instance := range instances {
		if wanted(instance) {
			count++
		}
	}
	return count
}

// isCompressionConsumer, isAgeFold and isFinalRound name the concrete plugin
// types the builtin catalog mounts, so a resolution can be read member by member.
func isCompressionConsumer(instance plugin.Instance) bool {
	_, ok := instance.(*CompressionConsumer)
	return ok
}

func isAgeFold(instance plugin.Instance) bool {
	_, ok := instance.(*AgeFoldPlugin)
	return ok
}

func isFinalRound(instance plugin.Instance) bool {
	_, ok := instance.(*agent.FinalRoundPlanner)
	return ok
}

// TestBuiltinCatalogMountsEveryFeature pins the default boot flow of the catalog.
//   - every builtin plugin is a unique catalog entry
//   - the run-level set carries the features the assembly root used to construct
//   - the run-level instances the catalog prepares are the ones the run serves
func TestBuiltinCatalogMountsEveryFeature(t *testing.T) {
	if err := checkPluginCatalog(builtinPluginCatalog); err != nil {
		t.Fatalf("the builtin catalog registers a duplicate: %v", err)
	}
	if got := len(builtinPluginCatalog); got != 7 {
		t.Fatalf("the builtin catalog holds %d entr(ies), want the 7 builtin plugins", got)
	}
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	if !carriesInstance(run.plugins, isCompressionConsumer) {
		t.Fatalf("the run-level set carries no compression consumer: %v", run.plugins)
	}
	if !carriesInstance(run.plugins, isFinalRound) {
		t.Fatalf("the run-level set carries no final round planner: %v", run.plugins)
	}
	if run.compress == nil || run.ageFold == nil || run.finalRound == nil {
		t.Fatalf("the prepared instances are compress=%v age fold=%v final round=%v, want all three mounted", run.compress, run.ageFold, run.finalRound)
	}
	if run.ageFold.executor != compressExecutor(run.compress) {
		t.Fatal("the age fold executes no judgment of the mounted compression consumer")
	}
}

// TestHostPluginsMountThroughTheCatalog pins the host-level catalog path.
//   - the host assembly forwards the collections the catalog resolves and keeps the faces they install
//   - the assembly root names no feature of its own
func TestHostPluginsMountThroughTheCatalog(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	seam := &hostRunPluginSeam{
		run:  run,
		host: func() *Host { return nil },
		port: &hostMeditationPort{},
	}
	instances, err := hostPlugins(seam)
	if err != nil {
		t.Fatalf("mount the host plugins: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("the host plugins mounted %d instance(s), want the reflection driver beside the settle driver", len(instances))
	}
	if seam.driver == nil || seam.settle == nil {
		t.Fatalf("the host faces installed by the catalog = driver %v settle %v, want both", seam.driver, seam.settle)
	}
}

// TestPluginDeclarationDisablesABuiltinPlugin pins the customization semantics.
//   - an absent declaration mounts every agent-level builtin
//   - the "-" prefix disables exactly one of them
func TestPluginDeclarationDisablesABuiltinPlugin(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	defaults, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60})
	if err != nil {
		t.Fatalf("resolve the absent declaration: %v", err)
	}
	if !carriesInstance(defaults, isAgeFold) {
		t.Fatalf("the default pipeline carries no age fold: %v", defaults)
	}
	disabled, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-age-fold"}})
	if err != nil {
		t.Fatalf("resolve the disabling declaration: %v", err)
	}
	if carriesInstance(disabled, isAgeFold) {
		t.Fatalf("the disabling declaration left the age fold mounted: %v", disabled)
	}
	if !carriesInstance(disabled, isCompressionConsumer) {
		t.Fatalf("the disabling declaration dropped the run-level plugins too: %v", disabled)
	}
}

// TestPluginDeclarationMountsANamedPluginOnce pins the idempotent entry.
//   - naming a builtin twice mounts one instance
func TestPluginDeclarationMountsANamedPluginOnce(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	instances, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"age-fold", "age-fold"}})
	if err != nil {
		t.Fatalf("resolve the naming declaration: %v", err)
	}
	if got := countInstances(instances, isAgeFold); got != 1 {
		t.Fatalf("the naming declaration mounted %d age fold(s), want one", got)
	}
}

// TestPluginDeclarationRefusesAnUnregisteredName pins the loud refusal.
//   - a name no plugin registers stops the assembly instead of mounting nothing
func TestPluginDeclarationRefusesAnUnregisteredName(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	_, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"no-such-plugin"}})
	if err == nil || !strings.Contains(err.Error(), "no-such-plugin") {
		t.Fatalf("the unregistered declaration = %v, want a refusal naming it", err)
	}
}

// TestPluginDeclarationRefusesARunLevelPluginName pins the scope refusal.
//   - an agent declaration can neither disable nor restate a plugin the run or the host carries
//   - so a deployment never believes it turned one off
func TestPluginDeclarationRefusesARunLevelPluginName(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	_, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-compress-consumer"}})
	if err == nil || !strings.Contains(err.Error(), "compress-consumer") || !strings.Contains(err.Error(), "run-level") {
		t.Fatalf("the run-level declaration = %v, want a refusal naming the plugin and its scope", err)
	}
}

// TestDisabledBuiltinHandsOverItsObligations pins what a plugins: declaration owes when it disables a plugin that holds an obligation.
//   - the disabling declaration leaves the named agent-level builtin out of the pipeline while the run-level set stays mounted
//   - the slice carrying the obligation keeps its name, its payload and its refusal, so nothing is handed to a successor the declaration never named
//   - a declaration that dropped the run-level set, an unmount that deleted the holder, or a cut that stopped stating its payload fails one of the three checks
//   - consumer: Run.agentPlugins — the resolution Publish mounts — Run.RegisterStateSlice's disposer and Run.Checkpoint
//
// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
func TestDisabledBuiltinHandsOverItsObligations(t *testing.T) {
	ctx := context.Background()
	const sliceName = "assembly.disabled-consumer"
	const owedItem = "the-notice-of-the-disabled-consumer"
	holder := &owedSlice{name: sliceName}
	holder.open(owedItem)
	var unmount func() error
	run := windowChainRun(t, ctx, withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, agents map[event.AgentID]NativeAgent) error {
		dispose, err := run.RegisterStateSlice(holder)
		if err != nil {
			return err
		}
		unmount = dispose
		_, err = run.Publish(ctx, decl, agents)
		return err
	}))
	run.stopCheckpointFiler()
	commitSessionTurns(t, ctx, run, event.Address{AgentID: "agent-a", SessionID: "session-disabled"}, "disabled", 1)

	disabled, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-age-fold"}})
	if err != nil {
		t.Fatalf("resolve the disabling declaration: %v", err)
	}
	if carriesInstance(disabled, isAgeFold) {
		t.Fatalf("the disabling declaration left the age fold mounted: %v", disabled)
	}
	if !carriesInstance(disabled, isCompressionConsumer) {
		t.Fatalf("the disabling declaration dropped the run-level plugins too: %v", disabled)
	}
	if names := run.StateRegistry().Names(); !sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the disabling declaration, want the slice kept", names)
	}
	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut after the disabling declaration = (%t, %v), want a filed cut", filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, run, key)
	if payload := slicePayloadOf(t, cut, sliceName); !strings.Contains(payload, owedItem) {
		t.Fatalf("the cut states %q for %s, want the obligation %s the disabled plugin carried nowhere", payload, sliceName, owedItem)
	}
	refusal := unmount()
	if refusal == nil || !strings.Contains(refusal.Error(), "1 unfinished obligation(s)") || !strings.Contains(refusal.Error(), sliceName) {
		t.Fatalf("the unmount of the slice the declaration disabled = %v, want a refusal naming the slice and its 1 unfinished obligation(s)", refusal)
	}
	if names := run.StateRegistry().Names(); !sliceContainsName(names, sliceName) {
		t.Fatalf("the registry names %v after the refused unmount, want the slice kept with its obligation", names)
	}
}

// TestBuiltinCatalogRefusesADuplicateName pins the catalog check.
//   - one name twice would make a declaration mount an arbitrary one of the two entries
func TestBuiltinCatalogRefusesADuplicateName(t *testing.T) {
	err := checkPluginCatalog([]builtinPlugin{{name: "twice"}, {name: "twice"}})
	if err == nil || !strings.Contains(err.Error(), "registered twice") {
		t.Fatalf("the duplicated catalog = %v, want a refusal naming the duplication", err)
	}
}

// namedInstance is a contributed pipeline element of one name: the native face of
// a host may carry its own plugins, and a declaration names them by Name.
type namedInstance struct{ name string }

// Name reports the instance's stable name.
func (n namedInstance) Name() string { return n.name }

// Port reports the consumption the instance serves.
func (n namedInstance) Port() plugin.Port { return plugin.PortFactObservation }

// TestPluginDeclarationCarriesAContributedInstance pins the extension surface.
//   - a name the host contributed in the native face resolves
//   - naming it with the "-" prefix leaves the instance out of the pipeline
func TestPluginDeclarationCarriesAContributedInstance(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	contributed := []plugin.Instance{namedInstance{name: "transform-a"}}
	kept, err := run.agentPlugins(contributed, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"transform-a"}})
	if err != nil {
		t.Fatalf("declare a contributed instance: %v", err)
	}
	if !carriesInstance(kept, func(instance plugin.Instance) bool { return instance.Name() == "transform-a" }) {
		t.Fatalf("the pipeline dropped the contributed instance the declaration names: %v", kept)
	}
	dropped, err := run.agentPlugins(contributed, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-transform-a"}})
	if err != nil {
		t.Fatalf("disable a contributed instance: %v", err)
	}
	if carriesInstance(dropped, func(instance plugin.Instance) bool { return instance.Name() == "transform-a" }) {
		t.Fatalf("the disabling declaration left the contributed instance mounted: %v", dropped)
	}
}
