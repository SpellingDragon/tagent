// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
package assembly

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
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

// TestDeclaredPluginChoiceSurvivesRestart pins the two answers one plugins: declaration gets.
//   - a supported agent-level choice takes real effect: naming the age fold mounts it, and the "-" prefix leaves it out
//   - the run-level set stays mounted across either choice, so a disabling declaration narrows one agent and not the assembly
//   - a name the run or the host carries is refused by name beside its scope, so a deployment never believes it turned one off
//   - the second process over the root the first one left answers both the same way: the choice belongs to the declaration
//
// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
func TestDeclaredPluginChoiceSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := windowChainRun(t, ctx, storage)
	commitWindowTurns(t, ctx, first, event.Address{AgentID: "agent-a", SessionID: "session-declared-choice"}, 2)
	assertDeclaredPluginChoice(t, first)
	endRun(t, first)

	booted := windowChainRun(t, ctx, storage)
	defer endRun(t, booted)
	if entries, held := booted.ReplayEntries("session-declared-choice"); !held || len(entries) == 0 {
		t.Fatalf("the second process reads %d entr(ies) of the session the first one folded, want the chain it left: the restart proves nothing about the declaration without it", len(entries))
	}
	assertDeclaredPluginChoice(t, booted)
}

// assertDeclaredPluginChoice pins what one process answers for the supported and
// the unsupported halves of a plugins: declaration.
func assertDeclaredPluginChoice(t *testing.T, run *Run) {
	t.Helper()
	named, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"age-fold"}})
	if err != nil {
		t.Fatalf("resolve the supported naming declaration: %v", err)
	}
	if got := countInstances(named, isAgeFold); got != 1 {
		t.Fatalf("the supported declaration mounted %d age fold(s), want the one it names", got)
	}
	disabled, err := run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-age-fold"}})
	if err != nil {
		t.Fatalf("resolve the supported disabling declaration: %v", err)
	}
	if carriesInstance(disabled, isAgeFold) {
		t.Fatalf("the supported disabling declaration left the age fold mounted: %v", disabled)
	}
	if !carriesInstance(disabled, isCompressionConsumer) {
		t.Fatalf("the supported disabling declaration dropped the run-level set too: %v", disabled)
	}
	_, err = run.agentPlugins(nil, config.AgentSpec{ID: "agent-a", ReplayWindow: 60, Plugins: []string{"-settle-driver"}})
	if err == nil || !strings.Contains(err.Error(), "settle-driver") || !strings.Contains(err.Error(), "host-level") {
		t.Fatalf("the host-level declaration = %v, want a refusal naming the plugin and its scope", err)
	}
}

// TestSettleFactRepairRunsOnTheCatalogAssembly pins the commit face the catalog hands the settle pump.
//   - the catalog mounts the driver beside the run's own commit entry, so a boot supplies the fact a refused delivery write left owed
//   - that boot is the assembly the host builds: the check wires no commit face and drives no delivery seam of its own
//   - the turn stays owed, because the drive belongs to the host and a seam beside none records no receipt for an unrun turn
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSettleFactRepairRunsOnTheCatalogAssembly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	addr := deliveryTestAddress("session-catalog-settle-repair")
	refusal := errors.New("the commit face refused the delivery fact of the settled call")
	tool := &deliveryFactObligationTool{}
	run := deliveryFactObligationRun(t, ctx, root, tool)
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		if evt.Type == event.TypeTaskSettled {
			return "", refusal
		}
		return run.commitFactChecked(ctx, evt)
	})
	if _, err := run.Invoke(ctx, InvokeRequest{
		InvocationID:  "inv-catalog-settle-repair",
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "call the tool once"}},
	}); err != nil {
		t.Fatalf("invoke the run beside the refused delivery fact: %v", err)
	}
	waitScopeGate(t, "the refused notification to stand owed under the claim this process took", func() bool {
		return len(run.store.Unconsumed()) == 1
	})
	if _, _, err := run.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the process beside the owed notification: %v", err)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the process beside the owed notification: %v", err)
	}

	booted := deliveryFactObligationBoot(t, ctx, root, tool)
	defer func() {
		if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process after the restart: %v", err)
		}
	}()
	restored := booted.store.Unconsumed()
	if len(restored) != 1 {
		t.Fatalf("the booted mailbox owes %+v, want the one notification the refused delivery fact left", restored)
	}
	if facts := deliveryFactsOf(booted.SessionFacts(addr)); len(facts) != 0 {
		t.Fatalf("the booted chain already holds %d delivery fact(s), want the refused write to have left none", len(facts))
	}

	seam := &hostRunPluginSeam{
		run:  booted,
		host: func() *Host { return nil },
		port: &hostMeditationPort{},
	}
	if _, err := hostPlugins(seam); err != nil {
		t.Fatalf("mount the host plugins of the booted run: %v", err)
	}
	if seam.settle == nil {
		t.Fatal("the catalog mounted no settle driver: the assembly the host builds carries no recovery of the obligation a refused delivery fact leaves")
	}
	seam.settle.DeclareScope(string(restored[0].Address.AgentID), string(restored[0].Address.AgentID))
	seam.settle.Wake()
	waitScopeGate(t, "the catalog-assembled pump to supply the fact the refused write left owed", func() bool {
		return len(deliveryFactsOf(booted.SessionFacts(addr))) == 1
	})
	seam.settle.Stop()

	if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 0 {
		t.Fatalf("the root holds %d consumption receipt(s), want none: the turn belongs to the host and this seam has none", len(keys))
	}
	if seats := booted.store.Unconsumed(); len(seats) != 1 {
		t.Fatalf("the booted mailbox owes %+v after the repair, want the one notification whose turn is still owed", seats)
	}
}
