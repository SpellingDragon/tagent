package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// MeditationConfig is the runtime configuration for the meditation manager.
// Converted from config.MeditationConfig (string durations) by tagent.go.
type MeditationConfig struct {
	Enabled    bool
	Interval   time.Duration // Check interval (default: 30m)
	MinGap     time.Duration // Minimum idle gap for valid meditation (default: 2h)
	PromptText string        // Meditation prompt text (loaded from file)
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

// UpdateLastEventTime records the timestamp of the most recent event.
// Called by TagentAgent on InjectMessage and on event forwarding in loop().
func (m *MeditationManager) UpdateLastEventTime(t time.Time) {
	m.lastEventTime.Store(t.UnixMilli())
}

// LastEventTime returns the most recent event timestamp in Unix milliseconds.
// Shared with ProjectionOrganizer for idle detection.
func (m *MeditationManager) LastEventTime() int64 {
	return m.lastEventTime.Load()
}

// checkAndMeditate evaluates whether a meditation should fire.
// A meditation is valid only if the gap since the last event >= MinGap.
func (m *MeditationManager) checkAndMeditate() {
	now := time.Now()
	lastEventMs := m.lastEventTime.Load()

	if lastEventMs == 0 {
		// No events received yet — skip meditation.
		return
	}

	gap := now.Sub(time.UnixMilli(lastEventMs))
	if gap < m.cfg.MinGap {
		log.Debugf("[Meditation] skipping: gap=%s < min_gap=%s", gap, m.cfg.MinGap)
		return
	}

	// Conditions met — inject meditation message.
	msg := m.buildMeditationMessage(now)
	m.injector.InjectMessageWithSource("meditation", msg)
	m.lastMeditation.Store(now.UnixMilli())

	log.Infof("[Meditation] triggered: gap=%s since last event", gap)
}

// buildMeditationMessage constructs the meditation external_input message.
// The message includes a [meditation] marker, timestamps, and the prompt text.
func (m *MeditationManager) buildMeditationMessage(now time.Time) model.Message {
	var lastMed string
	if lm := m.lastMeditation.Load(); lm > 0 {
		lastMed = time.UnixMilli(lm).UTC().Format("2006-01-02 15:04:05")
	} else {
		lastMed = "首次冥想"
	}

	content := fmt.Sprintf("[meditation] 这是一次定时冥想事件。\n\n"+
		"上次有效冥想时间：%s\n当前时间：%s\n\n%s",
		lastMed, now.UTC().Format("2006-01-02 15:04:05"), m.cfg.PromptText)

	return model.Message{
		Role:    model.RoleUser,
		Content: content,
	}
}
