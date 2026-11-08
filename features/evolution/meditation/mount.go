package meditation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// ErrToolMisplaced reports a meditation declaration whose reflection would
// deliver through a host tool the observing agent does not carry: the
// reflection turn writes its delivery through that tool, so an enabled
// reflection with declared targets beside no such tool could never deliver.
var ErrToolMisplaced = errors.New("tagent: a meditation tool is granted to the wrong agent")

// DefaultInterval is the tick cadence of a declaration that names none.
const DefaultInterval = 30 * time.Minute

// 契约: docs/adr/0004-feature-mounting-is-a-plugin-registry.md
//
// Plan is the one reflection section a document enables: the observing agent,
// the loop session, the cadence and floor, the static prompt, the observed and
// read namespaces, the declared delivery targets and the anchor record the
// watermark persists through.
//
// The section is resolved, judged and assembled here rather than in the
// composition root, so that root performs the generic steps of a mount and
// reads no meditation parameter itself.
type Plan struct {
	// Agent is the declared agent that runs the reflection loop.
	Agent string
	// Session is the dedicated reflection session that agent reflects in.
	Session string
	// SelfReflection reports a reflection the HOST ITSELF runs: the agent that
	// enables meditation is the entry agent, so its dedicated reflection
	// session shares one tool binding with the entry session. In this form the
	// cross-session send tool is granted to the reflection session alone by
	// this plugin (session-scoped), and the entry tool face stays free of it.
	// The retired independent curation agent keeps the older declarative grant
	// instead.
	SelfReflection bool
	// Interval is the tick cadence the loop fires at.
	Interval time.Duration
	// MinGap is the floor between two consumed reflections.
	MinGap time.Duration
	// Prompt is the static prompt one reflection pass carries.
	Prompt string
	// Observed are the namespaces the reflection observes.
	Observed []string
	// Reads are the namespaces the observing agent is authorized to read.
	Reads []string
	// Targets are the declared agents the reflection result flows back to.
	Targets []string
	// Anchors is the file the watermark anchor record persists through.
	Anchors string
}

// Resolve resolves the one meditation section a document enables: the entry
// agent first and the remaining agents in declaration order, so one document
// resolves to one plan across restarts. A document enabling none reports a nil
// plan, and every inconsistent section is refused by name.
//
// A relative prompt_file resolves beside promptDir, and anchors is written under
// storageRoot.
func Resolve(doc document.Document, promptDir, storageRoot string) (*Plan, error) {
	var plan *Plan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec, ok := doc.Agents[id]
		if !ok || !spec.Meditation.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("tagent: agents %q and %q both enable meditation, and one host drives one reflection loop",
				declaredBy, id)
		}
		entry := spec.Meditation
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation interval %s", id, interval)
		}
		if interval == 0 {
			interval = DefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation min_gap %s", id, entry.MinGap)
		}
		prompt, err := prompt(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("tagent: %w", err)
		}
		targets, err := targets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("tagent: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist", id)
		}
		plan = &Plan{
			Agent:          id,
			Session:        reflectionSession(doc.Entry, id),
			SelfReflection: id == doc.Entry,
			Interval:       interval,
			MinGap:         entry.MinGap,
			Prompt:         prompt,
			Observed:       scope.Names,
			Reads:          reads,
			Targets:        targets,
			Anchors:        filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// ValidateDelivery refuses a plan whose reflection would deliver through a face
// the observing agent does not carry: the retired independent curation agent with
// a declared deliver_to white list must itself declare the cross-session
// communication tool, because the delivery is the reflection turn's own decision
// and no mechanism writes a body in its place. A reflection declared beside no
// white list delivers nothing and is not judged.
//
// Host self-reflection is not judged: the entry and reflection sessions share one
// binding, so this plugin grants the send tool to the reflection session alone,
// and the entry tool face keeps it out. declares answers whether one declared
// agent carries one host tool, and tool names the tool a delivery passes through.
func (p *Plan) ValidateDelivery(declares func(id, tool string) bool, tool string) error {
	if p == nil || len(p.Targets) == 0 {
		return nil
	}
	if p.SelfReflection {
		return nil
	}
	if !declares(p.Agent, tool) {
		return fmt.Errorf("%w: the meditation session %q delivers to %d declared target(s) beside no %q tool, so no reflection turn could ever write one",
			ErrToolMisplaced, p.Agent, len(p.Targets), tool)
	}
	return nil
}

// GrantsSend reports whether this plan makes the plugin grant the cross-session
// send tool to the dedicated reflection session of the named agent. Only host
// self-reflection does: the entry and reflection sessions share one binding, so
// send must be scoped to the reflection session alone. An independent curation
// agent keeps its own declarative tool face.
func (p *Plan) GrantsSend(agent string) bool {
	return p != nil && p.SelfReflection && p.Agent == agent && len(p.Targets) > 0
}

// Faces are the run faces one mounted reflection is assembled over: the anchor
// store it advances only on a consumed turn, the observation and injection
// faces it reads and writes through, the read of whether a model request of the
// observed session is executing, the instance that request runs in and the
// session query its delivery resolves against.
type Faces struct {
	// Gate is the anchor store this host wired as the watermark gate.
	Gate WatermarkGate
	// Observed is the fact-chain face the reflection scans.
	Observed ObservedFacts
	// Input is the injection face the reflection pass enters the loop through.
	Input ReflectionInput
	// ModelInFlight reports the model request one site of the observed session
	// is executing.
	ModelInFlight func(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool)
	// Instance is the runtime instance identity the passes are stamped with.
	Instance event.InstanceID
	// Sessions is the live-binding face a delivery target resolves against.
	Sessions SessionQuery
}

// Instances builds the reflection and its driver over one plan: the one plugin
// instance a mounted meditation serves its generation with. A plan built beside
// no anchor store refuses by name, because a watermark the assembly forgot to
// wire would leave the loop firing on facts it already consumed.
//
// observe reads every pass the driver runs; a nil observer drops the readings.
func (p *Plan) Instances(faces Faces, observe func(TickOutcome)) (*Driver, error) {
	if faces.Gate == nil {
		return nil, fmt.Errorf("tagent: agent %q enables meditation beside no reflection anchor store", p.Agent)
	}
	reflection, err := NewReflection(ReflectionConfig{
		Loop:     ReflectionLoop{Agent: p.Agent, Session: p.Session},
		Face:     ReflectionFace{ObservedNamespaces: p.Observed, ReadNamespaces: p.Reads},
		Cadence:  ReflectionCadence{Interval: p.Interval, MinGap: p.MinGap},
		Prompt:   p.Prompt,
		Delivery: ReflectionDelivery{Targets: p.Targets, Sessions: faces.Sessions},
	}, faces.Gate, faces.Observed, faces.Input)
	if err != nil {
		return nil, err
	}
	driver := NewDriver(reflection, p.Interval, p.MinGap, faces.ModelInFlight, faces.Instance)
	driver.SetObserver(observe)
	return driver, nil
}

// prompt reads the static prompt one enabled meditation carries. A relative
// path resolves beside the declared prompt directory, and a file the checkout
// does not carry refuses the mount.
func prompt(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("tagent: agent %q enables meditation but names no prompt_file", id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("tagent: agent %q cannot read the meditation prompt %s: %w", id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("tagent: agent %q declares an empty meditation prompt %s", id, path)
	}
	return string(body), nil
}

// targets resolves the delivery white list of one enabled meditation: every name
// must be a declared agent, deduplicated in declaration order, and a section that
// names no target is refused by name.
func targets(doc document.Document, id string, declared []string) ([]string, error) {
	out := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("tagent: agent %q delivers its meditation to the undeclared agent %q", id, trimmed)
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tagent: agent %q enables meditation but names no deliver_to target", id)
	}
	return out, nil
}

// reflectionSession reports the dedicated reflection session of the agent that
// declared meditation. The host itself reflects in the reserved reflection
// session of the entry agent; a separate curation agent keeps its own meditation
// session, so the two forms never collide and existing curation deployments read
// the session they always read.
func reflectionSession(entry, agent string) string {
	suffix := "meditation"
	if agent == entry {
		suffix = "reflection"
	}
	return sessionPrefix(entry) + suffix
}

// sessionPrefix reports the session identity prefix of one entry agent: the
// label the deployment names, and the host default beside none.
func sessionPrefix(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "tagent-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
}
