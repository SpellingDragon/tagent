package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionFactory_Properties(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		wantErr    bool
	}{
		{
			name:       "empty properties",
			properties: nil,
			wantErr:    false,
		},
		{
			name: "work_dir only",
			properties: map[string]any{
				"work_dir": "/tmp/tagent-workspace",
			},
			wantErr: false,
		},
		{
			name: "run_as_user only",
			properties: map[string]any{
				"run_as_user": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "run_as_group only",
			properties: map[string]any{
				"run_as_group": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "all properties",
			properties: map[string]any{
				"work_dir":     "/tmp/tagent-workspace",
				"run_as_user":  "tagent-runner",
				"run_as_group": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "non-string property values ignored",
			properties: map[string]any{
				"work_dir": 123,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callable, err := actionFactory(agent.PlainToolFactoryConfig{
				ID:         "exec",
				Properties: tt.properties,
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, callable)

			// Verify it is an ActionTool.
			at, ok := callable.(*action.ActionTool)
			require.True(t, ok, "factory should return *action.ActionTool")
			assert.NotNil(t, at)
		})
	}
}

func TestActionFactory_ReturnsCallableTool(t *testing.T) {
	callable, err := actionFactory(agent.PlainToolFactoryConfig{ID: "exec"})
	require.NoError(t, err)
	require.NotNil(t, callable)

	// Declaration should be non-nil.
	decl := callable.Declaration()
	require.NotNil(t, decl)
	assert.Equal(t, "action", decl.Name)
}
