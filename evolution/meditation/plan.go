// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// The declaration face of one scheduled reflection: a document states which
// agent observes, at which cadence, over which namespaces, with which prompt and
// to which declared targets, and PlanOf resolves that section once
// into the plan a driver runs. The host maps its own document onto
// document.Document; the resolution rules — one loop per host, cadence bounds, the
// observation face inside the authorized read scope, the declared delivery
// targets and the anchor record path — live here, not in the host.
package meditation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/memory"
)

// ErrDeclaration reports a meditation section that cannot be resolved
// into one scheduled reflection: more than one agent enabling the loop, a
// negative cadence, an unreadable or empty prompt, an observation face outside
// the authorized read scope, an undeclared delivery target, or an enabled
// section beside no shared storage tier for its anchor record.
var ErrDeclaration = errors.New("tagent: the meditation declaration is inconsistent")

// Plan is the one scheduled reflection a document declares, resolved
// at assembly: the observing agent, the cadence, the prompt the input carries,
// the resolved observation face, the declared delivery targets and the anchor
// record its watermark persists in. A document enabling no meditation resolves
// to a nil plan, which starts no driver.
type Plan struct {
	agent    string
	interval time.Duration
	minGap   time.Duration
	prompt   string
	observed []string
	reads    []string
	targets  []string
	anchors  string
}

// PlanOf resolves the one meditation section doc enables: the entry
// agent is resolved first and the remaining agents in declared order, so one
// document resolves to one plan across restarts. A document enabling none
// reports a nil plan. Every inconsistent section is refused by name at assembly
// rather than driven into a reflection the declaration did not state.
func PlanOf(doc *document.Document, promptDir, storageRoot string) (*Plan, error) {
	if doc == nil {
		return nil, nil
	}
	var plan *Plan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec := doc.Agents[id]
		entry := spec.Meditation
		if !entry.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("%w: agents %q and %q both enable meditation, and one host drives one reflection loop",
				ErrDeclaration, declaredBy, id)
		}
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("%w: agent %q declares a negative meditation interval %s", ErrDeclaration, id, interval)
		}
		if interval == 0 {
			interval = DefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("%w: agent %q declares a negative meditation min_gap %s", ErrDeclaration, id, entry.MinGap)
		}
		prompt, err := meditationPrompt(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDeclaration, err)
		}
		targets, err := meditationTargets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("%w: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist",
				ErrDeclaration, id)
		}
		plan = &Plan{
			agent:    id,
			interval: interval,
			minGap:   entry.MinGap,
			prompt:   prompt,
			observed: scope.Names,
			reads:    reads,
			targets:  targets,
			anchors:  filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// meditationPrompt reads the static prompt one enabled meditation carries. A
// relative path is resolved beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly: a scheduled reflection without
// its prompt would run a different reflection than the one declared.
func meditationPrompt(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("%w: agent %q enables meditation but names no prompt_file", ErrDeclaration, id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: agent %q cannot read the meditation prompt %s: %w", ErrDeclaration, id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("%w: agent %q declares an empty meditation prompt %s", ErrDeclaration, id, path)
	}
	return string(body), nil
}

// meditationTargets resolves the declared delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and an enabled section that names no target is refused by name rather
// than running a reflection whose result nobody receives.
func meditationTargets(doc *document.Document, id string, declared []string) ([]string, error) {
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("%w: agent %q delivers its meditation to the undeclared agent %q", ErrDeclaration, id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, trimmed)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: agent %q enables meditation but names no deliver_to target", ErrDeclaration, id)
	}
	return targets, nil
}

// Agent reports the observing agent's identity.
func (p *Plan) Agent() string {
	if p == nil {
		return ""
	}
	return p.agent
}

// Interval reports the tick cadence the reflection loop runs at.
func (p *Plan) Interval() time.Duration {
	if p == nil {
		return 0
	}
	return p.interval
}

// MinGap reports the rhythm floor between two fired reflections; zero keeps the
// cadence as the only bound.
func (p *Plan) MinGap() time.Duration {
	if p == nil {
		return 0
	}
	return p.minGap
}

// Prompt reports the static prompt every reflection input carries.
func (p *Plan) Prompt() string {
	if p == nil {
		return ""
	}
	return p.prompt
}

// ObservedNamespaces reports the resolved observation face: the declared
// namespaces inside the authorized read scope, or the agent's own namespace
// beside no declaration.
func (p *Plan) ObservedNamespaces() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.observed...)
}

// ReadNamespaces reports the namespaces the observing agent may read across,
// its own first and every granted cross-namespace read after it.
func (p *Plan) ReadNamespaces() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.reads...)
}

// DeliverTo reports the declared delivery white list the fired reflection flows
// back through.
func (p *Plan) DeliverTo() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.targets...)
}

// AnchorsPath reports the anchor record the reflection watermark persists in:
// the store beside the shared storage tier the document names.
func (p *Plan) AnchorsPath() string {
	if p == nil {
		return ""
	}
	return p.anchors
}
