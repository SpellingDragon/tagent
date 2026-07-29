package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// mockMessageInjector records injected messages for test verification.
type mockMessageInjector struct {
	messages []model.Message
}

func (m *mockMessageInjector) InjectMessageWithSource(source string, msg model.Message) {
	m.messages = append(m.messages, msg)
}

func TestNewMeditationManager(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Enabled:    true,
		Interval:   30 * time.Minute,
		MinGap:     2 * time.Hour,
		PromptText: "meditation prompt",
	}

	mgr := NewMeditationManager(cfg, inj)
	require.NotNil(t, mgr)
	assert.Equal(t, cfg, mgr.cfg)
	assert.Equal(t, inj, mgr.injector)
}

func TestMeditationManager_UpdateAnchors(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)

	now := time.Now()
	mgr.UpdateLastUserInput(now)
	mgr.UpdateLastTurnEnd(now)

	assert.WithinDuration(t, now, time.UnixMilli(mgr.lastUserInput.Load()), time.Second)
	assert.WithinDuration(t, now, time.UnixMilli(mgr.lastTurnEnd.Load()), time.Second)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenNoUserInput(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	// Even with a completed turn long ago, no user input ⇒ nothing to reflect on.
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenNoTurnCompleted(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	// User input arrived but no turn has completed yet (first turn in flight).
	mgr.UpdateLastUserInput(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenIdleTooSmall(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Hour}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-time.Second))
	mgr.UpdateLastTurnEnd(time.Now())
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_FiresWhenGatesMet(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		MinGap:     time.Millisecond,
		PromptText: "meditation prompt text",
	}
	mgr := NewMeditationManager(cfg, inj)

	// Both gates pass: user input exists (novelty) and last turn ended long ago (idle).
	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	msg := inj.messages[0]
	assert.Equal(t, model.RoleUser, msg.Role)
	assert.Contains(t, msg.Content, "[meditation]")
	assert.Contains(t, msg.Content, cfg.PromptText)
}

func TestMeditationManager_checkAndMeditate_UpdatesLastMeditation(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{MinGap: time.Millisecond}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Greater(t, mgr.lastMeditation.Load(), int64(0))
}

// Novelty gate: during sustained silence a second meditation must NOT fire —
// no new user input since the previous one.
func TestMeditationManager_checkAndMeditate_SkipsWithoutNewUserInput(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first meditation fires")

	// Silence — second check must skip.
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 1, "no second meditation without new user input")

	// New user input AFTER the last meditation → gate re-arms.
	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond)) // idle gate passes
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "meditation fires again after new user input")
}

// Perpetual-motion regression (meditation-gate-split): turns derived from a
// meditation (spawned tasks settling as Source="task" reclaim turns) move ONLY
// the idle anchor. Without new user input, meditation must never re-fire, no
// matter how many MinGap windows elapse.
func TestMeditationManager_MeditationDerivedTurnsDoNotRearm(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1, "first meditation fires")

	// Simulate the meditation turn and its spawned tasks' settle turns ending
	// repeatedly, each leaving the idle gate wide open again.
	for i := 0; i < 3; i++ {
		mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond))
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.messages, 1, "derived turn ends must not re-arm meditation")

	// Only real user input re-arms the novelty gate.
	time.Sleep(2 * time.Millisecond)
	mgr.UpdateLastUserInput(time.Now())
	mgr.UpdateLastTurnEnd(time.Now().Add(-10 * time.Millisecond))
	mgr.checkAndMeditate()
	assert.Len(t, inj.messages, 2, "meditation fires again after real user input")
}

// Self-lock: after firing, the novelty gate blocks all subsequent checks by
// itself (lastUserInput <= lastMeditation) — no fire-time anchor reset needed.
func TestMeditationManager_SelfLocksAfterFiring(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Millisecond}, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()
	require.Len(t, inj.messages, 1)

	for i := 0; i < 5; i++ {
		mgr.checkAndMeditate()
	}
	assert.Len(t, inj.messages, 1, "novelty gate self-locks without any reset")
}

// The event callback must not touch meditation anchors at all — gating moved
// to the injection points (novelty) and runEventLoop turn ends (idle).
func TestOnEventCallback_DoesNotTouchMeditationAnchors(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Hour}, inj)
	ta := &TagentAgent{name: "t", meditationMgr: mgr}
	callback := ta.makeOnEventCallback()

	final := func(source string) *event.Event {
		evt := event.New("inv", "t")
		evt.Response = &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "out"}}}}
		evt.StateDelta = map[string][]byte{tagentevent.MetaKeyTriggerSource: []byte(source)}
		return evt
	}

	callback(final("meditation"))
	callback(final("user"))
	callback(final("task"))

	assert.Zero(t, mgr.lastUserInput.Load(), "callback must not arm the novelty gate")
	assert.Zero(t, mgr.lastTurnEnd.Load(), "callback must not move the idle anchor")
}

// Injection-side arming: only source=="user" updates the novelty anchor.
func TestArmMeditationNoveltyGate_UserSourceOnly(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)
	ta := &TagentAgent{name: "t", meditationMgr: mgr}

	ta.armMeditationNoveltyGate("meditation")
	ta.armMeditationNoveltyGate(SourceTask)
	assert.Zero(t, mgr.lastUserInput.Load(), "non-user sources must not arm the gate")

	ta.armMeditationNoveltyGate("user")
	assert.Greater(t, mgr.lastUserInput.Load(), int64(0), "user source arms the gate")
}

// Mixed-batch defense (D4): meditation yields whenever it shares a batch.
func TestDropMeditationFromMixedBatch(t *testing.T) {
	newMed := func() *AgentEvent {
		return NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "[meditation] reflect"})
	}
	newTask := func() *AgentEvent {
		return NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: "[task settled] done"})
	}
	newUser := func() *AgentEvent {
		return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hi"})
	}

	t.Run("task first — meditation dropped, turn keeps task lineage", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newTask(), newMed()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, SourceTask, extractTriggerSource(filtered))
	})

	t.Run("meditation first — meditation dropped, turn keeps user lineage", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newMed(), newUser()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, "user", extractTriggerSource(filtered))
	})

	t.Run("pure meditation batch passes through", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newMed()}, "t")
		require.Len(t, filtered, 1)
		assert.Equal(t, "meditation", extractTriggerSource(filtered))
	})

	t.Run("no meditation batch passes through", func(t *testing.T) {
		filtered := dropMeditationFromMixedBatch([]*AgentEvent{newUser(), newTask()}, "t")
		assert.Len(t, filtered, 2)
	})
}

func TestMeditationManager_StartStop(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Interval:   10 * time.Millisecond,
		MinGap:     time.Millisecond,
		PromptText: "meditation",
	}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.Start()

	// Wait long enough for at least one tick.
	time.Sleep(50 * time.Millisecond)

	mgr.Stop()

	// Should have injected at least one meditation message.
	assert.GreaterOrEqual(t, len(inj.messages), 1)
}

func TestMeditationManager_buildMeditationMessage(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "reflect and summarize"}
	mgr := NewMeditationManager(cfg, inj)

	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	msg := mgr.buildMeditationMessage(now, time.Hour)

	assert.Equal(t, model.RoleUser, msg.Role)
	assert.Contains(t, msg.Content, "[meditation]")
	assert.Contains(t, msg.Content, "reflect and summarize")
	assert.Contains(t, msg.Content, "2026-06-30 12:00:00")
	assert.Contains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_buildMeditationMessage_WithLastMeditation(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "reflect"}
	mgr := NewMeditationManager(cfg, inj)

	lastMed := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	mgr.lastMeditation.Store(lastMed.UnixMilli())

	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	msg := mgr.buildMeditationMessage(now, time.Hour)

	assert.Contains(t, msg.Content, lastMed.Format("2006-01-02 15:04:05"))
	assert.NotContains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_MessageContainsRequiredMarkers(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "meditation prompt"}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastUserInput(time.Now().Add(-2 * time.Second))
	mgr.UpdateLastTurnEnd(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	content := inj.messages[0].Content

	assert.True(t, strings.HasPrefix(content, "[meditation]"))
	assert.Contains(t, content, "上次有效冥想时间")
	assert.Contains(t, content, "当前时间")
}
