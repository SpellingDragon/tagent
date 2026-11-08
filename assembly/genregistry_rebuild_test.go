// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
// The generation-scoped registry the publication path drives: every candidate
// generation rebuilds the instances of the consumers mounted on it, in the order
// their wiring mounted them, and the resources a consumer acquires split three
// ways — the process-private ones die with their generation, the external durable
// ones survive the switch and are re-probed, and the shared natives are reused as
// they stand.
package assembly

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// genProbeDurableIterations is the declaration value the failing probe consumer
// refuses: a published candidate that carries it never reaches the publication
// entry, so the consumers built before it are released and their faces restored.
const genProbeDurableIterations = 7

// genProbeFace is one face a probe consumer answers beside its per-generation
// instance: the durable face is re-probed by every generation instead of rebuilt,
// and the shared face is the one the wiring handed over.
type genProbeFace struct {
	label string
}

// genProbeInstance is the instance one probe consumer built for one generation,
// beside the account of what happened to it.
type genProbeInstance struct {
	consumer string
	gen      state.Generation
	face     *genProbeFace
	restored bool
	released int
}

// genProbeAccount records what the probe consumers of one assembled run did, in
// build order, so a test reads the rebuild of a generation instead of inferring
// it.
type genProbeAccount struct {
	mu      sync.Mutex
	order   []string
	built   []*genProbeInstance
	durable *genProbeFace
	probes  int
	shared  *genProbeFace
}

// newGenProbeAccount returns the account of one test over the shared face the
// wiring hands every consumer.
func newGenProbeAccount() *genProbeAccount {
	return &genProbeAccount{shared: &genProbeFace{label: "shared"}}
}

// install builds the instance of one consumer for one generation and re-probes
// the external durable face the account keeps across generations.
func (a *genProbeAccount) install(consumer string, gen state.Generation) *genProbeInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.durable == nil {
		a.durable = &genProbeFace{label: "durable"}
	}
	a.probes++
	instance := &genProbeInstance{consumer: consumer, gen: gen, face: &genProbeFace{label: fmt.Sprintf("private-%s-%d", consumer, gen)}}
	a.order = append(a.order, fmt.Sprintf("%s@%d", consumer, gen))
	a.built = append(a.built, instance)
	return instance
}

// restore records that the face one candidate installed was put back.
func (a *genProbeAccount) restore(instance *genProbeInstance) {
	a.mu.Lock()
	defer a.mu.Unlock()
	instance.restored = true
}

// release records the retirement release of one generation's private instance
// and refuses a second release of the same generation.
func (a *genProbeAccount) release(instance *genProbeInstance) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	instance.released++
	if instance.released > 1 {
		return fmt.Errorf("probe: the %s instance of generation %d was released %d times", instance.consumer, instance.gen, instance.released)
	}
	return nil
}

// buildOrder reports the build order the consumers ran in.
func (a *genProbeAccount) buildOrder() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.order...)
}

// instancesOf reports the instances one consumer built, in build order.
func (a *genProbeAccount) instancesOf(consumer string) []*genProbeInstance {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*genProbeInstance, 0, len(a.built))
	for _, instance := range a.built {
		if instance.consumer == consumer {
			out = append(out, instance)
		}
	}
	return out
}

// durableProbes reports how often the consumers re-probed the durable face.
func (a *genProbeAccount) durableProbes() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.probes
}

// genProbeConsumer is one consumer mounted on the generation-scoped registry of
// a test assembly: it installs the instance of its generation as the face its
// construction reads, re-probes the durable face, reads the shared face, and
// registers its private instance as the exclusive resource of the generation
// when it is told to.
type genProbeConsumer struct {
	name      string
	account   *genProbeAccount
	exclusive bool
	fail      func(decl *config.Declaration) bool
}

// registration answers the generation-scoped registration of one probe consumer.
func (c genProbeConsumer) registration() state.GenRegistration {
	return state.GenRegistration{
		Name: c.name,
		BuildGen: func(_ context.Context, decl *config.Declaration, gen state.Generation) (state.GenResources, error) {
			if c.fail != nil && c.fail(decl) {
				return state.GenResources{}, fmt.Errorf("probe: the %s consumer refuses this declaration", c.name)
			}
			instance := c.account.install(c.name, gen)
			restore := func() { c.account.restore(instance) }
			if !c.exclusive {
				return state.GenResources{Restore: restore}, nil
			}
			resources := state.NewLeases()
			resources.Register("probe", fmt.Sprintf("gen:%d", gen), func(context.Context) error {
				return c.account.release(instance)
			})
			return state.GenResources{Resources: resources, Restore: restore}, nil
		},
	}
}

// genRegistryDeclaration renders the one-agent declaration a probe run publishes,
// carrying the tool iteration count that moves between two publications.
func genRegistryDeclaration(iterations int) *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxToolIterations: iterations}},
	}
}

// genRegistryNatives composes the native contribution of the one declared agent.
func genRegistryNatives() map[event.AgentID]NativeAgent {
	return map[event.AgentID]NativeAgent{
		"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
				Done:    true,
			}
		}}},
	}
}

// genRegistryRun assembles a run carrying the given probe consumers and answers
// it beside the natives and the declaration its publications run over.
func genRegistryRun(t *testing.T, ctx context.Context, options ...Option) (*Run, map[event.AgentID]NativeAgent) {
	t.Helper()
	natives := genRegistryNatives()
	attached := append([]Option{WithGovernance(snapshotTestGovernance{})}, options...)
	run, err := assemble(ctx, genRegistryDeclaration(0), natives, true, attached...)
	if err != nil {
		t.Fatalf("assemble the probe run: %v", err)
	}
	return run, natives
}

// registryMatches reports whether the introspection of one generation answers
// exactly the named consumers, in the given order, each with the exclusive
// registration the wanted flag states.
func registryMatches(got []GenerationConsumer, gen state.Generation, names []string, registered bool) error {
	if len(got) != len(names) {
		return fmt.Errorf("the registry reports %d consumer(s), want %d", len(got), len(names))
	}
	for i, name := range names {
		want := state.GenLeaseKey(gen, name)
		if got[i].Name != name {
			return fmt.Errorf("consumer %d is %q, want %q in build order", i, got[i].Name, name)
		}
		if got[i].Exclusive != want {
			return fmt.Errorf("consumer %q registers %q, want the frozen key %q", name, got[i].Exclusive, want)
		}
		if got[i].Registered != registered {
			return fmt.Errorf("consumer %q reports registered=%t, want %t", name, got[i].Registered, registered)
		}
	}
	return nil
}

// TestGenScopedRegistryRebuild is the positive leaf of the generation-scoped registry.
// - every published generation rebuilds the mounted consumers in the order their wiring mounted them
// - each generation's exclusive registration lands on the published plan under the frozen key
// - a candidate whose consumer fails puts back the faces the earlier consumers installed and releases what they acquired
func TestGenScopedRegistryRebuild(t *testing.T) {
	ctx := context.Background()
	account := newGenProbeAccount()
	refusing := genProbeConsumer{
		name:    "gamma",
		account: account,
		fail: func(decl *config.Declaration) bool {
			return decl != nil && len(decl.Agents) == 1 && decl.Agents[0].MaxToolIterations == genProbeDurableIterations
		},
	}
	run, natives := genRegistryRun(t, ctx,
		WithGenerationConsumer(genProbeConsumer{name: "alpha", account: account, exclusive: true}.registration()),
		WithGenerationConsumer(genProbeConsumer{name: "beta", account: account, exclusive: true}.registration()),
		WithGenerationConsumer(refusing.registration()),
	)
	if got := account.buildOrder(); len(got) != 3 || got[0] != "alpha@1" || got[1] != "beta@1" || got[2] != "gamma@1" {
		t.Fatalf("the first generation built %v, want alpha, beta then gamma of generation 1", got)
	}
	first := run.GenerationConsumers(state.Generation(1))
	if len(first) != 3 || first[0].Name != "alpha" || first[1].Name != "beta" || first[2].Name != "gamma" {
		t.Fatalf("the registry reports %+v, want alpha, beta then gamma in build order", first)
	}
	if !first[0].Registered || !first[1].Registered || first[2].Registered {
		t.Fatalf("the served generation registers %+v, want alpha and beta registered beside gamma", first)
	}

	held, err := run.publisher.Borrow(ctx, event.AgentID("agent-a"))
	if err != nil {
		t.Fatalf("borrow the first generation: %v", err)
	}
	outcome, err := run.Publish(ctx, genRegistryDeclaration(2), natives)
	if err != nil {
		t.Fatalf("publish the moved declaration: %v", err)
	}
	if outcome.Result != state.PublishApplied || outcome.Publication.Generation != 2 {
		t.Fatalf("publication = %+v, want the second generation applied", outcome.Publication)
	}
	order := account.buildOrder()
	if len(order) != 6 || order[3] != "alpha@2" || order[4] != "beta@2" || order[5] != "gamma@2" {
		t.Fatalf("the second generation built %v, want the same consumers in the same order", order)
	}
	second := run.GenerationConsumers(state.Generation(2))
	if err := registryMatches(second[:2], state.Generation(2), []string{"alpha", "beta"}, true); err != nil {
		t.Fatalf("the second generation reports %v: %v", second, err)
	}
	alphaFirst := account.instancesOf("alpha")[0]
	if alphaFirst.released != 0 || alphaFirst.restored {
		t.Fatalf("the held generation released its instance before the drain: released=%d restored=%t", alphaFirst.released, alphaFirst.restored)
	}
	if err := held.Release(ctx); err != nil {
		t.Fatalf("release the held generation: %v", err)
	}
	if alphaFirst.released != 1 {
		t.Fatalf("the retired generation released its instance %d time(s), want exactly one", alphaFirst.released)
	}

	serving := run.publisher.Current()
	if _, err := run.Publish(ctx, genRegistryDeclaration(genProbeDurableIterations), natives); err == nil {
		t.Fatal("the publication of a candidate one consumer refuses answered success, want the refusal")
	}
	if got := run.publisher.Current(); got.Generation != serving.Generation || got.Fingerprint != serving.Fingerprint {
		t.Fatalf("publication after the refused candidate = %+v, want the generation %+v kept", got, serving)
	}
	refused := account.instancesOf("alpha")
	last := refused[len(refused)-1]
	if !last.restored {
		t.Fatal("the refused candidate left the face its consumer installed in place, want the restore")
	}
	if last.released != 1 {
		t.Fatalf("the refused candidate released its instance %d time(s), want exactly one", last.released)
	}
}

// TestRegistryResourceTaxonomy is the negative leaf of the three-way resource split.
// - only the process-private instance of a generation is an exclusive resource, released by its retirement
// - the external durable face is re-probed by every generation instead of rebuilt
// - the shared face the wiring handed over is the one every generation reads
// - a consumer that acquired nothing exclusive registers no generation resource, so no retirement reaches it
func TestRegistryResourceTaxonomy(t *testing.T) {
	ctx := context.Background()
	account := newGenProbeAccount()
	run, natives := genRegistryRun(t, ctx,
		WithGenerationConsumer(genProbeConsumer{name: "probe", account: account, exclusive: true}.registration()),
		WithGenerationConsumer(genProbeConsumer{name: "plain", account: account}.registration()),
	)
	if got := account.durableProbes(); got != 2 {
		t.Fatalf("the first generation probed the durable face %d time(s), want one probe per consumer", got)
	}
	if account.durable == nil || account.shared == nil {
		t.Fatal("the account holds no durable or shared face after the first generation")
	}
	shared, durable := account.shared, account.durable

	held, err := run.publisher.Borrow(ctx, event.AgentID("agent-a"))
	if err != nil {
		t.Fatalf("borrow the first generation: %v", err)
	}
	if _, err := run.Publish(ctx, genRegistryDeclaration(3), natives); err != nil {
		t.Fatalf("publish the second generation: %v", err)
	}
	if got := account.durableProbes(); got != 4 {
		t.Fatalf("two generations probed the durable face %d time(s), want two probes per generation", got)
	}
	if account.durable != durable || account.shared != shared {
		t.Fatal("a generation rebuilt the durable or the shared face instead of re-probing it")
	}
	probes := account.instancesOf("probe")
	if len(probes) != 2 {
		t.Fatalf("the probe consumer built %d instance(s), want one per generation", len(probes))
	}
	if probes[0].face == probes[1].face {
		t.Fatal("the second generation reused the private instance of the first instead of building its own")
	}
	if err := held.Release(ctx); err != nil {
		t.Fatalf("release the held generation: %v", err)
	}
	if probes[0].released != 1 {
		t.Fatalf("the retirement released the private instance %d time(s), want exactly one", probes[0].released)
	}
	if probes[1].released != 0 {
		t.Fatalf("the retirement released the serving generation's instance %d time(s), want none", probes[1].released)
	}
	plain := account.instancesOf("plain")
	if len(plain) != 2 || plain[0].released != 0 {
		t.Fatalf("the plain consumer answered %d instance(s) beside %d release(s), want two instances never released", len(plain), plain[0].released)
	}
	report := run.GenerationConsumers(state.Generation(2))
	if err := registryMatches(report[:1], state.Generation(2), []string{"probe"}, true); err != nil {
		t.Fatalf("the served generation reports %+v: %v", report, err)
	}
	if len(report) != 2 || report[1].Name != "plain" || report[1].Registered {
		t.Fatalf("the served generation reports %+v, want the plain consumer beside no registration", report)
	}
}
