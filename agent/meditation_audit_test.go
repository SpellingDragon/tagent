package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// The audit digest wiring must run after the meditation manager exists.
// Before the fix, SetAuditLine was invoked while meditationMgr was still nil
// (assigned only later in the same constructor), so the reflection feedback
// line silently never reached any meditation message.
func TestAgentWiresAuditLineWhenMeditationEnabled(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:      "resp-1",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "test"}}},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "test",
		Meditation: MeditationConfig{
			Enabled:    true,
			Interval:   30 * time.Minute,
			MinGap:     time.Hour,
			PromptText: "reflect",
		},
	})
	require.NoError(t, err)
	defer ta.Close()

	require.NotNil(t, ta.meditationMgr)
	assert.NotNil(t, ta.meditationMgr.auditLine,
		"self-audit digest line must be wired when meditation is enabled")
}

// The consumption side: a set audit line must surface in the meditation
// message content.
func TestMeditationMessageCarriesAuditLine(t *testing.T) {
	inj := &mockMessageInjector{}
	mgr := NewMeditationManager(MeditationConfig{
		Enabled:    true,
		Interval:   30 * time.Minute,
		MinGap:     time.Hour,
		PromptText: "reflect",
	}, inj)
	mgr.SetAuditLine(func() string { return "[self-telemetry-audit] 空转审计级别 L1" })

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour)
	assert.Contains(t, msg.Content, "[self-telemetry-audit] 空转审计级别 L1")
}
