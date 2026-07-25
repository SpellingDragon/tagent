package agent

import (
	"context"
	"fmt"
	"github.com/SpellingDragon/tagent/agent/task"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/prompt"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// MeditationConfig is the runtime configuration for the meditation manager.
// Converted from config.MeditationConfig (string durations) by tagent.go.
type MeditationConfig struct {
	Enabled      bool
	Interval     time.Duration  // Check interval (default: 30m)
	MinGap       time.Duration  // Minimum idle gap for valid meditation (default: 2h)
	PromptText   string         // Meditation prompt text (static, loaded once at init)
	PromptSource *prompt.Source // Hot-reloadable meditation prompt (optional, overrides PromptText)
}

// messageInjector is the interface for injecting messages into the event loop.
// *TagentAgent satisfies this interface.
type messageInjector interface {
	InjectMessageWithSource(source string, msg model.Message)
}

// MeditationManager periodically injects "meditation" external_input events
// into the event loop when the agent has been idle for at least MinGap.
//
// A meditation is valid only if no events (user input, agent output, tool
// calls, etc.) have been received for at least MinGap duration. This ensures
// meditation doesn't interrupt active conversations.
//
// The meditation event triggers the LLM to perform context cleanup, deep
// analysis of recent memories, and skill accumulation — all guided by the
// meditation prompt.
type MeditationManager struct {
	cfg      MeditationConfig
	injector messageInjector

	// taskController, when set, provides a read-only task-layer snapshot for the
	// self-state digest prepended to the meditation prompt. nil → digest omitted
	// (graceful degradation when no task layer is wired).
	taskController task.TaskController

	// lastEventTime tracks the most recent event timestamp (Unix milliseconds).
	// Updated by TagentAgent on every InjectMessage and event forwarding.
	lastEventTime atomic.Int64

	// lastMeditation tracks the most recent valid meditation timestamp.
	lastMeditation atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMeditationManager creates a MeditationManager.
// The injector is typically the *TagentAgent that owns this manager.
func NewMeditationManager(cfg MeditationConfig, injector messageInjector) *MeditationManager {
	return &MeditationManager{
		cfg:      cfg,
		injector: injector,
	}
}

// SetTaskController wires an optional read-only task controller used to render
// the self-state digest prepended to the meditation prompt. Safe to leave unset
// (digest is omitted — meditation behavior unchanged).
func (m *MeditationManager) SetTaskController(tc task.TaskController) {
	m.taskController = tc
}

// Start launches the meditation ticker goroutine.
// Must be called after the owner's persistent event loop is active.
func (m *MeditationManager) Start() {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.cfg.Interval)
		defer ticker.Stop()
		log.Infof("[Meditation] manager started: interval=%s min_gap=%s", m.cfg.Interval, m.cfg.MinGap)
		for {
			select {
			case <-ticker.C:
				m.checkAndMeditate()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the meditation goroutine to stop and waits for it.
func (m *MeditationManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	log.Info("[Meditation] manager stopped")
}

// UpdateLastEventTime records the timestamp of the most recent agent OUTPUT
// (final response). Idle-detection is anchored on agent output — not injected
// inputs — so meditation fires only after MinGap with no agent activity.
func (m *MeditationManager) UpdateLastEventTime(t time.Time) {
	m.lastEventTime.Store(t.UnixMilli())
}

// LastEventTime returns the most recent event timestamp in Unix milliseconds.
func (m *MeditationManager) LastEventTime() int64 {
	return m.lastEventTime.Load()
}

// checkAndMeditate evaluates whether a meditation should fire.
// Two gates must both pass:
//  1. idle gate: gap since the last REAL agent output >= MinGap;
//  2. novelty gate: there has been real activity SINCE the last meditation.
//     Without it, silence becomes a perpetual-motion machine: each meditation's
//     own output would eventually satisfy the idle gate again, producing an
//     endless chain of "nothing happened" summaries that pollute the projection
//     and burn LLM calls (meditation output does not update the idle anchor —
//     see makeOnEventCallback — so lastEventTime only moves on real activity).
func (m *MeditationManager) checkAndMeditate() {
	now := time.Now()
	lastEventMs := m.lastEventTime.Load()

	if lastEventMs == 0 {
		// No events received yet — skip meditation.
		return
	}

	// Novelty gate: no real agent output since the previous meditation ⇒
	// nothing new to reflect on.
	if lm := m.lastMeditation.Load(); lm > 0 && lastEventMs <= lm {
		log.Debugf("[Meditation] skipping: no new activity since last meditation")
		return
	}

	gap := now.Sub(time.UnixMilli(lastEventMs))
	if gap < m.cfg.MinGap {
		log.Debugf("[Meditation] skipping: gap=%s < min_gap=%s", gap, m.cfg.MinGap)
		return
	}

	// Conditions met — inject meditation message.
	msg := m.buildMeditationMessage(now, gap)
	m.injector.InjectMessageWithSource("meditation", msg)
	m.lastMeditation.Store(now.UnixMilli())
	// Reset the idle clock on firing so we don't re-meditate every check
	// interval before the resulting turn produces output (which also resets it).
	m.lastEventTime.Store(now.UnixMilli())

	log.Infof("[Meditation] triggered: gap=%s since last agent output", gap)
}

// buildMeditationMessage constructs the meditation external_input message.
// The message includes a [meditation] marker, timestamps, and the prompt text.
// If PromptSource is configured, the prompt is re-read from disk (hot-reload).
func (m *MeditationManager) buildMeditationMessage(now time.Time, idle time.Duration) model.Message {
	var lastMed string
	if lm := m.lastMeditation.Load(); lm > 0 {
		lastMed = time.UnixMilli(lm).UTC().Format("2006-01-02 15:04:05")
	} else {
		lastMed = "首次冥想"
	}

	// Hot-reload: prefer PromptSource over static PromptText
	promptText := m.cfg.PromptText
	if m.cfg.PromptSource != nil {
		if loaded, err := m.cfg.PromptSource.Get(); err == nil && loaded != "" {
			promptText = loaded
		}
	}

	// Self-state digest (task-layer health + idle) — prepended BEFORE the prompt
	// so the LLM reflects on real runtime state first. Omitted (empty) when no
	// task controller is wired, keeping behavior identical to before.
	var digest string
	if m.taskController != nil {
		digest = renderSelfStateDigest(m.taskController.List(), idle)
	}

	header := fmt.Sprintf("[meditation] 这是一次定时冥想事件。\n\n"+
		"上次有效冥想时间：%s\n当前时间：%s",
		lastMed, now.UTC().Format("2006-01-02 15:04:05"))
	parts := []string{header}
	if digest != "" {
		parts = append(parts, digest)
	}
	parts = append(parts, promptText)

	return model.Message{
		Role:    model.RoleUser,
		Content: strings.Join(parts, "\n\n"),
	}
}
