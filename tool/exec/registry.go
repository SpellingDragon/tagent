// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
// The generation-scoped lifecycle of the execution consumer: the boot manager
// serves as the template every generation's instance is built from and as the
// stable face the host closes, while each generation's tools bind the instance
// of their own generation. The resources split three ways: the manager
// bookkeeping and its trackers are process-private and die with the generation
// (Detach), the external tmux sessions survive the generation switch and are
// re-probed by the next generation's calls instead of rebuilt, and the executor
// beside its session generation tokens is shared across generations.
package exec

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// GenRegistration returns the generation-scoped registration of the execution
// consumer over one boot manager: the manager is the template each generation's
// instance is built from — the same executor, observer and options — and the
// stable face the host's close runs. The assembly root mounts this registration
// through the registry it drives; the rebuild logic lives here, never in a
// feature path of the root.
func GenRegistration(template *Manager) state.GenRegistration {
	return state.GenRegistration{
		Name: PresetName,
		BuildGen: func(ctx context.Context, decl *config.Declaration, gen state.Generation) (state.GenResources, error) {
			if template == nil {
				return state.GenResources{}, ErrUnwired
			}
			if err := validateExecMonitor(decl); err != nil {
				return state.GenResources{}, err
			}
			next := template.newGenerationInstance()
			restore := template.installGeneration(next)
			resources := state.NewLeases()
			resources.Register("exec", fmt.Sprintf("gen:%d", gen), func(ctx context.Context) error {
				return next.Detach(ctx)
			})
			return state.GenResources{Resources: resources, Restore: restore}, nil
		},
	}
}

// validateExecMonitor refuses an execution face whose declared monitor rhythm
// carries a negative span: a negative cadence observes nothing, so the
// declaration is refused by name instead of silently running unobserved. Only
// the agents whose references grant the exec face are judged; a section on an
// agent that never reads it is not this consumer's declaration.
func validateExecMonitor(decl *config.Declaration) error {
	if decl == nil {
		return nil
	}
	for _, spec := range decl.Agents {
		if !grantsExecFace(spec) {
			continue
		}
		monitor := spec.Exec.Monitor
		if monitor.Interval < 0 || monitor.StableDuration < 0 || monitor.InteractiveStableDuration < 0 || monitor.FakeDeadDuration < 0 {
			return fmt.Errorf("exec: the monitor rhythm of agent %q declares a negative span", spec.ID)
		}
	}
	return nil
}

// grantsExecFace reports whether one agent's tool references grant the
// execution face.
func grantsExecFace(spec config.AgentSpec) bool {
	for _, ref := range spec.Tools {
		if ref.Name == PresetName {
			return true
		}
	}
	return false
}

// newGenerationInstance builds the manager of one generation over the executor
// and options this template carries: the bookkeeping starts empty, while the
// external executor, the observer and the acquisition options are shared, so a
// session the earlier generation started stays verifiable and observable.
func (m *Manager) newGenerationInstance() *Manager {
	return &Manager{
		backend:            m.backend,
		now:                m.now,
		available:          m.available,
		defaultTTL:         m.defaultTTL,
		finalCaptureWindow: m.finalCaptureWindow,
		resources:          map[ResourceKey]*heldResource{},
		final:              map[ResourceKey]FinalOutput{},
		observer:           m.observer,
	}
}

// installGeneration installs one constructed instance as the face the
// candidate construction reads, and returns the restore that puts the previous
// face back and drops the instance from the template's set.
func (m *Manager) installGeneration(next *Manager) (restore func()) {
	m.genMu.Lock()
	defer m.genMu.Unlock()
	previous := m.genCurrent
	m.genCurrent = next
	m.genInstances = append(m.genInstances, next)
	return func() {
		m.genMu.Lock()
		defer m.genMu.Unlock()
		if m.genCurrent == next {
			m.genCurrent = previous
		}
		for i, instance := range m.genInstances {
			if instance == next {
				m.genInstances = append(m.genInstances[:i], m.genInstances[i+1:]...)
				break
			}
		}
	}
}

// presetFace reports the manager the tools of the candidate construction bind:
// the installed generation instance, or the manager itself beside no installed
// generation, so a preset built over a standalone manager behaves exactly as
// it did before the generation lifecycle existed.
func (m *Manager) presetFace() *Manager {
	m.genMu.Lock()
	defer m.genMu.Unlock()
	if m.genCurrent != nil {
		return m.genCurrent
	}
	return m
}

// generationInstances reports the instances this template installed, in
// installation order, as a snapshot the caller owns.
func (m *Manager) generationInstances() []*Manager {
	m.genMu.Lock()
	defer m.genMu.Unlock()
	return append([]*Manager(nil), m.genInstances...)
}

// generationHolder reports the generation instance that holds one resource:
// the current generation first, then the installed instances newest first, so
// the diagnostic reads the ledger that actually holds the lease. A key no
// instance holds answers nil.
func (m *Manager) generationHolder(key ResourceKey) *Manager {
	m.genMu.Lock()
	ordered := make([]*Manager, 0, len(m.genInstances)+1)
	if m.genCurrent != nil {
		ordered = append(ordered, m.genCurrent)
	}
	for i := len(m.genInstances) - 1; i >= 0; i-- {
		if m.genInstances[i] != m.genCurrent {
			ordered = append(ordered, m.genInstances[i])
		}
	}
	m.genMu.Unlock()
	for _, instance := range ordered {
		if instance.held(key) {
			return instance
		}
	}
	return nil
}

// Detach ends the process-private life of one manager generation without
// stopping the external executions it holds: every tracker stops and is joined
// and the bookkeeping of the held resources is dropped, while the durable
// sessions stay running for the generation in effect to re-probe. It is the
// release the retirement of a superseded generation runs on this instance; a
// repeated detach reports nothing left to detach.
func (m *Manager) Detach(_ context.Context) error {
	m.mu.Lock()
	if m.detached {
		m.mu.Unlock()
		return nil
	}
	m.detached = true
	var waits []chan struct{}
	for _, res := range m.resources {
		if res.stop != nil {
			close(res.stop)
			res.stop = nil
			waits = append(waits, res.done)
		}
	}
	m.resources = map[ResourceKey]*heldResource{}
	m.mu.Unlock()
	for _, done := range waits {
		<-done
	}
	return nil
}
