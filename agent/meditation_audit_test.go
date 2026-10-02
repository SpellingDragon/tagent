// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestAgentWiresAuditLineWhenMeditationEnabled 钉住启用冥想时 audit digest 行已接线。
// - 接线必须发生在 meditationMgr 构造之后，否则 SetAuditLine 静默落空。
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

// TestMeditationMessageCarriesAuditLine 钉住消费侧：audit 行已集则冥想消息内容携带它。
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
