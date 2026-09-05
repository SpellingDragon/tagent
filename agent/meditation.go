package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"

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

	// AnchorPath 是冥想门控锚点持久化路径（T-G AnchorStore）。非空则跨重启保留三锚点
	// （novelty/idle/last-meditation），重启后不立即误触发冥想；空 = 纯内存（现状，重启失忆）。
	AnchorPath string
}

// messageInjector is the interface for injecting messages into the event loop.
// *TagentAgent satisfies this interface.
type messageInjector interface {
	InjectMessageWithSource(source string, msg model.Message)
}

// MeditationManager periodically injects "meditation" external_input events
// into the event loop when the agent has been idle for at least MinGap AND
// there has been new user input since the last meditation.
//
// Gating is split across two independent anchors (meditation-gate-split):
// the idle gate is lineage-AGNOSTIC (any turn end counts as busy), while the
// novelty gate is INPUT-side anchored (only source=="user" injections arm it).
// This split makes output-side lineage tracking unnecessary: activity derived
// from a meditation turn (e.g. a spawned task settling as Source="task") can
// only DELAY the next meditation via the idle gate, never re-arm the novelty
// gate — which kills the self-feeding perpetual-motion loop.
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

	// lastUserInput is the novelty-gate anchor (Unix ms): the most recent
	// injection with source == "user". Updated only at the injection points
	// (inject.go) — input-side source is ground truth and cannot be laundered
	// by the task layer.
	lastUserInput atomic.Int64

	// lastTurnEnd is the idle-gate anchor (Unix ms): when the most recent turn
	// ended — any trigger source, including failed turns. Updated
	// unconditionally by runEventLoop after each RunFlow.
	lastTurnEnd atomic.Int64

	// lastMeditation tracks the most recent valid meditation timestamp.
	lastMeditation atomic.Int64

	// anchorStore 可选：持久化三锚点（T-G AnchorStore），跨重启保留冥想门控连续性。
	// nil = 纯内存（现状，重启失忆）。经 SetAnchorStore 注入。
	anchorStore *reliability.AnchorStore

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

// SetAnchorStore 注入锚点持久化存储（T-G AnchorStore），并 Load 恢复三锚点——跨重启保留冥想
// 门控连续性（重启后不立即误触发冥想、正确计算 novelty）。Load 失败保守用当前值（不阻断启动）。
func (m *MeditationManager) SetAnchorStore(s *reliability.AnchorStore) {
	m.anchorStore = s
	if s == nil {
		return
	}
	a, err := s.Load()
	if err != nil {
		log.Warnf("[Meditation] anchor load failed (%v), starting fresh", err)
		return
	}
	if a.LastUserInput > 0 {
		m.lastUserInput.Store(a.LastUserInput)
	}
	if a.LastTurnEnd > 0 {
		m.lastTurnEnd.Store(a.LastTurnEnd)
	}
	if a.LastMeditation > 0 {
		m.lastMeditation.Store(a.LastMeditation)
	}
	log.Infof("[Meditation] anchors restored across restart: lastUserInput=%d lastTurnEnd=%d lastMeditation=%d",
		a.LastUserInput, a.LastTurnEnd, a.LastMeditation)
}

// persistAnchors 保存当前三锚点到 anchorStore（若配置）。写失败仅告警（冥想门控降级为内存态，
// 不阻断主流程）。锚点更新（turn end / user input / meditation fire）时调用。
func (m *MeditationManager) persistAnchors() {
	if m.anchorStore == nil {
		return
	}
	a := reliability.MeditationAnchors{
		LastUserInput:  m.lastUserInput.Load(),
		LastTurnEnd:    m.lastTurnEnd.Load(),
		LastMeditation: m.lastMeditation.Load(),
	}
	if err := m.anchorStore.Save(a); err != nil {
		log.Warnf("[Meditation] anchor persist failed: %v", err)
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

// UpdateLastUserInput records a source=="user" injection timestamp — the
// novelty-gate anchor. Called from the injection points only (inject.go);
// non-user sources (meditation/task/tmux) must never arm this gate.
func (m *MeditationManager) UpdateLastUserInput(t time.Time) {
	m.lastUserInput.Store(t.UnixMilli())
	m.persistAnchors()
}

// UpdateLastTurnEnd records a turn-end timestamp — the idle-gate anchor.
// Called unconditionally by runEventLoop after every RunFlow, regardless of
// trigger source or success (lineage-agnostic by design).
func (m *MeditationManager) UpdateLastTurnEnd(t time.Time) {
	m.lastTurnEnd.Store(t.UnixMilli())
	m.persistAnchors()
}

// checkAndMeditate evaluates whether a meditation should fire.
// Two independent gates must both pass (meditation-gate-split):
//  1. novelty gate (input-side): there has been user input SINCE the last
//     meditation. Injection-point source is ground truth, so activity
//     laundered through the task layer (Source="task" settles of
//     meditation-spawned work) can never re-arm this gate — this alone kills
//     the perpetual-motion loop of "nothing happened" summaries.
//  2. idle gate (lineage-agnostic): gap since the last turn end >= MinGap.
//     ANY turn counts as busy — meditation-derived turns merely delay the
//     next meditation, which is harmless (and desirable while background
//     work is still churning).
//
// No fire-time anchor reset is needed: storing lastMeditation locks the
// novelty gate (lastUserInput <= lastMeditation) until real user input.
func (m *MeditationManager) checkAndMeditate() {
	now := time.Now()

	lastUserMs := m.lastUserInput.Load()
	if lastUserMs == 0 {
		// No user input received yet — nothing to reflect on.
		return
	}

	// Novelty gate: no user input since the previous meditation ⇒ skip.
	if lm := m.lastMeditation.Load(); lm > 0 && lastUserMs <= lm {
		log.Debugf("[Meditation] skipping: no new user input since last meditation")
		return
	}

	lastTurnMs := m.lastTurnEnd.Load()
	if lastTurnMs == 0 {
		// No turn has completed yet (first turn may still be in flight) — skip.
		return
	}
	idle := now.Sub(time.UnixMilli(lastTurnMs))
	if idle < m.cfg.MinGap {
		log.Debugf("[Meditation] skipping: idle=%s < min_gap=%s", idle, m.cfg.MinGap)
		return
	}

	// Conditions met — inject meditation message.
	msg := m.buildMeditationMessage(now, idle)
	m.injector.InjectMessageWithSource("meditation", msg)
	m.lastMeditation.Store(now.UnixMilli())
	m.persistAnchors()

	log.Infof("[Meditation] triggered: idle=%s since last turn end", idle)
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
