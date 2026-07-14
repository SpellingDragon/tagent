package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

func TestMeditationManager_UpdateLastEventTime(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{}, inj)

	now := time.Now()
	mgr.UpdateLastEventTime(now)

	stored := time.UnixMilli(mgr.lastEventTime.Load())
	assert.WithinDuration(t, now, stored, time.Second)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenNoEvents(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Second}, inj)

	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_SkipsWhenGapTooSmall(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{MinGap: time.Hour}, inj)

	mgr.UpdateLastEventTime(time.Now())
	mgr.checkAndMeditate()

	assert.Empty(t, inj.messages)
}

func TestMeditationManager_checkAndMeditate_FiresWhenGapMet(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		MinGap:     time.Millisecond,
		PromptText: "meditation prompt text",
	}
	mgr := NewMeditationManager(cfg, inj)

	// Set last event time in the past so gap >= MinGap.
	mgr.UpdateLastEventTime(time.Now().Add(-time.Second))
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

	mgr.UpdateLastEventTime(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	assert.Greater(t, mgr.lastMeditation.Load(), int64(0))
}

func TestMeditationManager_StartStop(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{
		Interval:   10 * time.Millisecond,
		MinGap:     time.Millisecond,
		PromptText: "meditation",
	}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastEventTime(time.Now().Add(-time.Second))
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
	msg := mgr.buildMeditationMessage(now)

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
	msg := mgr.buildMeditationMessage(now)

	assert.Contains(t, msg.Content, lastMed.Format("2006-01-02 15:04:05"))
	assert.NotContains(t, msg.Content, "首次冥想")
}

func TestMeditationManager_MessageContainsRequiredMarkers(t *testing.T) {
	inj := &mockMessageInjector{}
	cfg := MeditationConfig{PromptText: "meditation prompt"}
	mgr := NewMeditationManager(cfg, inj)

	mgr.UpdateLastEventTime(time.Now().Add(-time.Second))
	mgr.checkAndMeditate()

	require.Len(t, inj.messages, 1)
	content := inj.messages[0].Content

	assert.True(t, strings.HasPrefix(content, "[meditation]"))
	assert.Contains(t, content, "上次有效冥想时间")
	assert.Contains(t, content, "当前时间")
}
