// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
//
// The scheduled reflection of the host: the meditation section names one
// observing agent, the cadence its ticks run at, the namespaces it reflects on
// and the declared agents its result flows back to. The core driver owns the
// tick loop; this file resolves the declaration into its plan, supplies the
// observation and injection faces over the assembled run, and gives the
// delivery port the one outbound path. A document declaring no enabled
// meditation starts no driver and changes nothing.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ErrMeditationDeclaration reports a meditation section this host cannot drive:
// two agents enabling one reflection loop, a cadence or prompt that cannot be
// read, an observation face outside the agent's authorized read scope, a
// delivery target the document does not declare, or an enabled section beside
// no shared storage tier for its anchor record.
var ErrMeditationDeclaration = errors.New("wechat-bot: the meditation declaration is inconsistent")

// meditationPlan is the one scheduled reflection a document declares, resolved
// at assembly: the observing agent and its own loop address, the cadence, the
// prompt the input carries, the resolved observation face, the declared
// delivery targets and the anchor record its watermark persists in.
type meditationPlan struct {
	agent              event.AgentID
	address            event.Address
	interval           time.Duration
	minGap             time.Duration
	prompt             string
	observedNamespaces []string
	readNamespaces     []string
	deliverTo          []event.AgentID
	anchorsPath        string
}

// driverPlan reports the core driver plan this declaration names.
func (p meditationPlan) driverPlan() meditation.DriverPlan {
	return meditation.DriverPlan{
		ReflectionConfig: meditation.ReflectionConfig{
			Loop: meditation.ReflectionLoop{Agent: string(p.agent), Session: string(p.address.SessionID)},
			Face: meditation.ReflectionFace{
				ObservedNamespaces: p.observedNamespaces,
				ReadNamespaces:     p.readNamespaces,
			},
			Cadence: meditation.ReflectionCadence{Interval: p.interval, MinGap: p.minGap},
			Prompt:  p.prompt,
		},
		Targets: append([]event.AgentID(nil), p.deliverTo...),
	}
}

// meditationPlan resolves the one meditation section this document enables. A
// document enabling none reports nil, which wires no reflection face and
// starts no driver. A second enabled section is refused by name: one host owns
// one reflection anchor record, so it drives one reflection loop.
func (d *hostDocument) meditationPlan() (*meditationPlan, error) {
	settings, err := d.memorySettings()
	if err != nil {
		return nil, err
	}
	var plan *meditationPlan
	declaredBy := ""
	for _, id := range d.agentOrder() {
		entry := d.Agents[id].Meditation
		if !entry.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("%w: agents %q and %q both enable meditation, and one host drives one reflection loop",
				ErrMeditationDeclaration, declaredBy, id)
		}
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("%w: agent %q declares a negative meditation interval %s", ErrMeditationDeclaration, id, interval)
		}
		if interval == 0 {
			interval = meditation.DefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("%w: agent %q declares a negative meditation min_gap %s", ErrMeditationDeclaration, id, entry.MinGap)
		}
		prompt, err := d.meditationPrompt(id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		scope, err := meditation.ResolveObservationScope(id, entry.ObservedNamespaces, d.readNamespaces(id))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMeditationDeclaration, err)
		}
		targets, err := d.meditationTargets(id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if settings == nil {
			return nil, fmt.Errorf("%w: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist",
				ErrMeditationDeclaration, id)
		}
		plan = &meditationPlan{
			agent:              event.AgentID(id),
			address:            event.Address{AgentID: event.AgentID(id), SessionID: event.SessionID(sessionPrefix(d) + "meditation")},
			interval:           interval,
			minGap:             entry.MinGap,
			prompt:             prompt,
			observedNamespaces: scope.Names,
			readNamespaces:     d.readNamespaces(id),
			deliverTo:          targets,
			anchorsPath:        filepath.Join(settings.storage.Root, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// meditationPrompt reads the static prompt one enabled meditation carries. The
// path is resolved beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly: a scheduled reflection without
// its prompt would run a different reflection than the one declared.
func (d *hostDocument) meditationPrompt(id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("%w: agent %q enables meditation but names no prompt_file", ErrMeditationDeclaration, id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(d.PromptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: agent %q cannot read the meditation prompt %s: %w", ErrMeditationDeclaration, id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("%w: agent %q declares an empty meditation prompt %s", ErrMeditationDeclaration, id, path)
	}
	return string(body), nil
}

// meditationTargets resolves the declared delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and an enabled section that names no target is refused by name rather
// than running a reflection whose result nobody receives.
func (d *hostDocument) meditationTargets(id string, declared []string) ([]event.AgentID, error) {
	targets := make([]event.AgentID, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := d.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("%w: agent %q delivers its meditation to the undeclared agent %q", ErrMeditationDeclaration, id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, event.AgentID(trimmed))
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: agent %q enables meditation but names no deliver_to target", ErrMeditationDeclaration, id)
	}
	return targets, nil
}

// newMeditationDriver builds the core driver of one plan over the assembled
// run: the gate is the anchor store the assembly built, the observation face is
// the run's own fact chain, the injection face drives the observing agent's own
// loop session, and the delivery port is the host's outbound path.
func newMeditationDriver(host *hostRuntime, plan meditationPlan) (*meditation.Driver, error) {
	gate := host.run.ReflectionGate()
	if gate == nil {
		return nil, fmt.Errorf("%w: the assembly carries no reflection anchor store", ErrMeditationDeclaration)
	}
	driver, err := meditation.NewDriver(plan.driverPlan(), gate, &hostObservedFacts{run: host.run}, &hostReflectionInput{run: host.run}, host.deliverReflection)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMeditationDeclaration, err)
	}
	return driver, nil
}

// startMeditation starts the scheduled reflection beside the host: one tick per
// declared interval until the context or Close stops it. A document enabling no
// meditation starts nothing and is exactly the host it was before the section
// existed.
func (h *hostRuntime) startMeditation(ctx context.Context) error {
	if h == nil || h.meditationPlan == nil {
		return nil
	}
	if h.meditation != nil {
		return nil
	}
	driver, err := newMeditationDriver(h, *h.meditationPlan)
	if err != nil {
		return err
	}
	driver.Start(ctx)
	h.meditation = driver
	return nil
}

// deliverReflection hands the assistant output the reflection turn produced to
// the declared targets: the fresh messages of the observing agent's own
// transcript, filtered to answers without tool calls, sent to every chat the
// target agent has bound. A target no chat ever reached receives nothing and the
// delivery is named in the log instead of being retargeted to another session.
func (h *hostRuntime) deliverReflection(ctx context.Context, source event.Address, targets []event.AgentID) {
	if h.sender == nil {
		log.Warnf("[meditation] %s fired beside no outbound path, the result is withheld", source.AgentID)
		return
	}
	history := h.run.SessionHistory(source)
	contents := assistantContents(h.router.TakeUndelivered(source, history))
	if len(contents) == 0 {
		log.Debugf("[meditation] %s produced no deliverable assistant output", source.AgentID)
		return
	}
	for _, target := range targets {
		addresses := h.router.BoundAddresses(target)
		if len(addresses) == 0 {
			log.Warnf("[meditation] target %q holds no bound chat, %d message(s) withheld", target, len(contents))
			continue
		}
		for _, addr := range addresses {
			for _, content := range contents {
				if _, err := h.sender.sendToUser(ctx, sendRequest{
					addr:          addr,
					triggerSource: meditation.LineageReflection,
					content:       content,
				}); err != nil {
					log.Warnf("[meditation] delivery to %s refused: %v", addr.SessionID, err)
				}
			}
		}
	}
}

// assistantContents filters one transcript slice down to the answers a delivery
// carries: assistant messages that call no tool and hold text.
func assistantContents(messages []tagentmodel.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != tagentmodel.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return contents
}

// hostObservedFacts is the observation face of one reflection over the assembled
// run: the committed fact chain the run reads, carrying the lineage each fact
// was committed under.
type hostObservedFacts struct {
	run *tagent.Run
}

// QueryEvents answers one page over the fact chain the run observes.
func (f *hostObservedFacts) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	return f.run.ObservedEvents(ctx, opts)
}

// CommittedFact reports the fact one reference names.
func (f *hostObservedFacts) CommittedFact(ctx context.Context, id event.EventID) (event.Event, error) {
	return f.run.CommittedEvent(ctx, id)
}

// hostReflectionInput is the injection face of one reflection over the assembled
// run: it commits the reflection input as a fact of the observing agent's own
// partition, drives the agent's own loop session with it and reports what became
// of that turn. The receipt is the only evidence the reflection turn ran, so an
// injection that errors reports no receipt and the watermark never moves.
type hostReflectionInput struct {
	run *tagent.Run

	mu  sync.Mutex
	seq uint64
}

var _ meditation.ReflectionInput = (*hostReflectionInput)(nil)

// InjectReflection commits one reflection input, drives its loop session and
// reports the outcome the turn reached: a completed terminal is a consumed
// reflection, and every other terminal is a deferral that leaves the watermark
// where it stands.
func (i *hostReflectionInput) InjectReflection(ctx context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	i.mu.Lock()
	i.seq++
	seq := i.seq
	i.mu.Unlock()
	invocation := event.InvocationID(fmt.Sprintf("%s#meditation-%d", evt.Owner.SessionID, seq))
	turn, key, err := i.run.InjectEvent(ctx, evt, invocation)
	if err != nil {
		return meditation.ReflectionReceipt{}, err
	}
	receipt := meditation.ReflectionReceipt{Key: key, At: evt.Timestamp}
	if turn.Terminal != tagentmodel.KindCompleted {
		receipt.Outcome = meditation.ReflectionDeferred
		receipt.Reason = fmt.Sprintf("the reflection turn reached terminal %q instead of a completed one", turn.Terminal)
		return receipt, nil
	}
	receipt.Outcome = meditation.ReflectionConsumed
	return receipt, nil
}
