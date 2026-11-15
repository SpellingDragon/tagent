// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
// The builtin plugin catalog: one entry per feature the framework mounts out of
// the box, beside the resolution the default boot flow, the host assembly and a
// plugins: declaration drive. The assembly root names no feature: it forwards the
// collections this file resolves, so adding a plugin is a catalog line plus, for
// an agent plugin, a declaration line.
//
// This file and assembly/compress_mount.go are the plugin registry
// implementation registered under mounting.registry_implementation in
// golden/runtime/architecture.json. That is why the R5a mount rule does not
// judge the concrete constructions they carry: they are the one legal place a
// plugin is built, and judging them would make the rule unsatisfiable.
package assembly

import (
	"context"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// compressConsumerPluginName and the names beside it are the whole address of one
// builtin plugin: a plugins: declaration names one, a refusal quotes it.
const (
	compressConsumerPluginName  = "compress-consumer"
	finalRoundPluginName        = "final-round"
	reflectionDriverPluginName  = "reflection-driver"
	settleDriverPluginName      = "settle-driver"
	leaseObserverPluginName     = "lease-observer"
	ageFoldPluginName           = "age-fold"
	telemetryObserverPluginName = "telemetry-observer"
)

// pluginScope names where one catalogued plugin is carried.
type pluginScope int

const (
	// runPluginScope is carried by the run: one instance every agent generation
	// of the run sees.
	runPluginScope pluginScope = iota
	// hostPluginScope is carried by the host assembly's run-level set, beside
	// the face the host keeps of it.
	hostPluginScope
	// agentPluginScope is carried by the agents whose declaration leaves it
	// mounted.
	agentPluginScope
)

// scopeName reports one scope as a declaration refusal names it.
func scopeName(scope pluginScope) string {
	switch scope {
	case runPluginScope:
		return "run"
	case hostPluginScope:
		return "host"
	case agentPluginScope:
		return "agent"
	}
	return "unknown"
}

// builtinPlugin is one entry of the builtin catalog: the name a plugins:
// declaration names it by, the scope that carries it, and the construction that
// mounts it. The function of its own scope is the one set; the others are nil.
type builtinPlugin struct {
	// name is the name a plugins: declaration names this plugin by.
	name string
	// scope is the set that carries the plugin.
	scope pluginScope
	// prepare constructs the run-level instance an agent-scope plugin carries,
	// once per boot and before any generation is published.
	prepare func(r *Run) error
	// run mounts the instances of a run-scope plugin on the run.
	run func(r *Run) ([]plugin.Instance, error)
	// agent answers the instances one agent's pipeline carries, and nil
	// instances beside an agent the plugin does not apply to.
	agent func(r *Run, spec config.AgentSpec) ([]plugin.Instance, error)
	// host mounts the instances a host plugin contributes to the run-level set,
	// beside the face it installs on the seam.
	host func(seam *hostRunPluginSeam) ([]plugin.Instance, error)
}

// hostRunPluginSeam is what the host-scope plugins build from: the run they mount
// on, the host they deliver through, the meditation port and plan they bind, the
// declaration whose settle scope they read, and the faces they install back. The
// host is an accessor because a settle face reaches it only once a settlement
// runs, which is after Boot published it.
type hostRunPluginSeam struct {
	run    *Run
	host   func() *Host
	port   *hostMeditationPort
	plan   *meditation.Plan
	decl   *config.Declaration
	driver *generationReflection
	settle *SettleDriver
}

// builtinPluginCatalog lists every builtin plugin in mount order: the run-level
// set keeps the order the pipeline had while these features were constructed in
// the assembly root, and the agent-level set keeps the order its agents carried.
var builtinPluginCatalog = []builtinPlugin{
	compressConsumerPlugin(),
	{
		name:  finalRoundPluginName,
		scope: runPluginScope,
		run: func(r *Run) ([]plugin.Instance, error) {
			r.finalRound = agent.NewFinalRoundPlanner(finalRoundChain{run: r}, r.finalRoundPolicyOf)
			return []plugin.Instance{r.finalRound}, nil
		},
	},
	{
		name:  reflectionDriverPluginName,
		scope: hostPluginScope,
		host: func(seam *hostRunPluginSeam) ([]plugin.Instance, error) {
			seam.port.bindFaces(meditation.Faces{
				Gate:          seam.run.ReflectionGate(),
				Observed:      seam.run.ObservationFacts(),
				Input:         seam.run.ReflectionInput(),
				ModelInFlight: seam.run.ModelInFlight,
				Instance:      seam.run.instance,
				Sessions:      seam.port,
			}, func(outcome meditation.TickOutcome) {
				plan := seam.port.planInEffect()
				if plan == nil {
					return
				}
				assemblyInfo("tagent: meditation driver of %s: %s (%s)", plan.Agent, outcome.Decision.Disposition, outcome.Decision.Reason)
			})
			if _, err := seam.port.mountReflection(seam.plan); err != nil {
				return nil, err
			}
			seam.driver = newGenerationReflection(seam.port)
			return []plugin.Instance{seam.driver}, nil
		},
	},
	{
		name:  settleDriverPluginName,
		scope: hostPluginScope,
		host: func(seam *hostRunPluginSeam) ([]plugin.Instance, error) {
			driver := NewSettleDriver(
				seam.run.CommittedEvent,
				func() SettleWake {
					if seam.run.store == nil {
						return nil
					}
					return seam.run.store
				},
				func(ctx context.Context, addr event.Address, fact event.Event) error {
					return seam.host().fireNoticeTurn(ctx, addr, fact)
				},
			)
			driver.setHandoff(func(ctx context.Context, addr event.Address, notice task.Notice) error {
				return seam.host().handoffSettledBodies(ctx, addr, notice)
			})
			if scope, declared := settleScopeOfDocument(seam.decl); declared {
				driver.DeclareScope(scope.declaring, scope.targets...)
			}
			seam.settle = driver
			return []plugin.Instance{driver}, nil
		},
	},
	{
		name:  leaseObserverPluginName,
		scope: agentPluginScope,
		agent: func(r *Run, spec config.AgentSpec) ([]plugin.Instance, error) {
			observer := r.leaseObserverOf(event.AgentID(spec.ID))
			if observer == nil {
				return nil, nil
			}
			return []plugin.Instance{observer}, nil
		},
	},
	{
		name:  ageFoldPluginName,
		scope: agentPluginScope,
		prepare: func(r *Run) error {
			r.ageFold = NewAgeFoldPlugin(ageFoldSessions{run: r})
			return nil
		},
		agent: func(r *Run, spec config.AgentSpec) ([]plugin.Instance, error) {
			if spec.ReplayWindow <= 0 || r.ageFold == nil {
				return nil, nil
			}
			return []plugin.Instance{r.ageFold}, nil
		},
	},
	{
		name:  telemetryObserverPluginName,
		scope: agentPluginScope,
		agent: func(r *Run, spec config.AgentSpec) ([]plugin.Instance, error) {
			if !r.observationWired() {
				return nil, nil
			}
			return []plugin.Instance{observePlugin{observer: telemetry.NewCommitObserver(runFacts{run: r}, generationObserver{run: r})}}, nil
		},
	},
}

// checkPluginCatalog refuses a catalog registering one name twice: a declaration
// naming it would otherwise mount an arbitrary one of the two.
func checkPluginCatalog(catalog []builtinPlugin) error {
	seen := make(map[string]bool, len(catalog))
	for _, mount := range catalog {
		if mount.name == "" {
			return fmt.Errorf("tagent: the plugin catalog registers an unnamed plugin")
		}
		if seen[mount.name] {
			return fmt.Errorf("tagent: the plugin %q is registered twice in the catalog", mount.name)
		}
		seen[mount.name] = true
	}
	return nil
}

// builtinPluginNamed reports the catalog entry one name registers. A name the
// catalog does not carry is refused by the declaration resolution, never
// mounted as an empty instance.
func builtinPluginNamed(name string) (builtinPlugin, bool) {
	for _, mount := range builtinPluginCatalog {
		if mount.name == name {
			return mount, true
		}
	}
	return builtinPlugin{}, false
}

// agentPluginNames lists the names an agent declaration may name, in catalog
// order, so a refusal can tell the deployment what it could have written.
func agentPluginNames() []string {
	names := make([]string, 0, len(builtinPluginCatalog))
	for _, mount := range builtinPluginCatalog {
		if mount.scope == agentPluginScope {
			names = append(names, mount.name)
		}
	}
	return names
}

// pluginDeclaration is one agent's resolved plugins: declaration. The declaration
// customizes what the agent carries — an entry names a builtin plugin the
// framework mounts by default or an instance the host contributed in the native
// face, and the "-" prefix disables it — so it never reads as a whitelist that
// silently drops the plugins a deployment did not restate.
type pluginDeclaration struct {
	// disabled names the agent-scope builtins the declaration leaves unmounted.
	disabled map[string]bool
	// unmounted names the contributed instances the declaration disables.
	unmounted map[string]bool
}

// mounts reports whether the declaration leaves one agent-scope plugin mounted.
func (d pluginDeclaration) mounts(name string) bool {
	return !d.disabled[name]
}

// keeps reports whether the declaration leaves one contributed instance mounted.
func (d pluginDeclaration) keeps(instance plugin.Instance) bool {
	return !d.unmounted[instance.Name()]
}

// declaredPluginsOf reads one agent's plugins: declaration against the catalog
// and the instances the host contributed. An empty name, and a name neither the
// catalog nor a contributed instance registers, are refused by name: a
// declaration that silently mounts nothing is a misconfiguration reported as a
// missing summary instead of a rejected boot.
func declaredPluginsOf(spec config.AgentSpec, contributed []plugin.Instance) (pluginDeclaration, error) {
	carried := make(map[string]bool, len(contributed))
	for _, instance := range contributed {
		carried[instance.Name()] = true
	}
	resolved := pluginDeclaration{disabled: map[string]bool{}, unmounted: map[string]bool{}}
	for _, entry := range spec.Plugins {
		name := strings.TrimSpace(entry)
		disabled := strings.HasPrefix(name, "-")
		name = strings.TrimSpace(strings.TrimPrefix(name, "-"))
		if name == "" {
			return pluginDeclaration{}, fmt.Errorf("the plugins declaration of agent %q carries an empty plugin name", spec.ID)
		}
		mount, known := builtinPluginNamed(name)
		if !known {
			if !carried[name] {
				return pluginDeclaration{}, fmt.Errorf(
					"agent %q declares the plugin %q, which no plugin registers and no contributed instance carries (agent plugins: %s)",
					spec.ID, name, strings.Join(agentPluginNames(), ", "))
			}
			if disabled {
				resolved.unmounted[name] = true
				continue
			}
			delete(resolved.unmounted, name)
			continue
		}
		if mount.scope != agentPluginScope {
			return pluginDeclaration{}, fmt.Errorf(
				"agent %q declares the %s-level plugin %q, which %s (agent plugins: %s)",
				spec.ID, scopeName(mount.scope), name, carriedBy(mount.scope), strings.Join(agentPluginNames(), ", "))
		}
		if disabled {
			resolved.disabled[name] = true
			continue
		}
		delete(resolved.disabled, name)
	}
	return resolved, nil
}

// carriedBy states where a plugin of one scope is mounted, for a refusal that
// names the scope instead of silently ignoring the entry.
func carriedBy(scope pluginScope) string {
	switch scope {
	case runPluginScope:
		return "the default boot flow mounts on the run instead"
	case hostPluginScope:
		return "the host assembly mounts on the run instead"
	}
	return "no scope carries"
}

// mountBuiltinPlugins mounts the builtin catalog on the run: every entry prepares
// its run-level instance first, so a plugin that executes another's judgments
// finds it constructed whichever order the catalog lists them in, and the
// run-scope entries then forward the instances they resolve into the run-level
// set every agent generation carries.
func (r *Run) mountBuiltinPlugins() error {
	if err := checkPluginCatalog(builtinPluginCatalog); err != nil {
		return err
	}
	for _, mount := range builtinPluginCatalog {
		if mount.prepare == nil {
			continue
		}
		if err := mount.prepare(r); err != nil {
			return fmt.Errorf("tagent: mount the %s plugin: %w", mount.name, err)
		}
	}
	for _, mount := range builtinPluginCatalog {
		if mount.run == nil {
			continue
		}
		instances, err := mount.run(r)
		if err != nil {
			return fmt.Errorf("tagent: mount the %s plugin: %w", mount.name, err)
		}
		r.plugins = append(r.plugins, instances...)
	}
	return nil
}

// hostPlugins mounts the host-scope builtin plugins of one assembly in catalog
// order: each installs its face on the seam and forwards the instances it
// contributes to the run-level set.
func hostPlugins(seam *hostRunPluginSeam) ([]plugin.Instance, error) {
	var instances []plugin.Instance
	for _, mount := range builtinPluginCatalog {
		if mount.host == nil {
			continue
		}
		built, err := mount.host(seam)
		if err != nil {
			return nil, fmt.Errorf("tagent: mount the %s plugin: %w", mount.name, err)
		}
		instances = append(instances, built...)
	}
	return instances, nil
}

// agentPlugins resolves the plugins one agent's pipeline carries: the instances
// the host contributed in the native face, the run-level instances every
// generation carries, and the agent-scope builtins the declaration leaves
// mounted, in catalog order.
func (r *Run) agentPlugins(contributed []plugin.Instance, spec config.AgentSpec) ([]plugin.Instance, error) {
	declared, err := declaredPluginsOf(spec, contributed)
	if err != nil {
		return nil, err
	}
	elements := make([]plugin.Instance, 0, len(contributed)+len(r.plugins))
	for _, instance := range contributed {
		if declared.keeps(instance) {
			elements = append(elements, instance)
		}
	}
	elements = append(elements, r.plugins...)
	for _, mount := range builtinPluginCatalog {
		if mount.agent == nil || !declared.mounts(mount.name) {
			continue
		}
		instances, err := mount.agent(r, spec)
		if err != nil {
			return nil, fmt.Errorf("tagent: mount the %s plugin of agent %q: %w", mount.name, spec.ID, err)
		}
		elements = append(elements, instances...)
	}
	return elements, nil
}
