package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterTools_CreatesCallableTools(t *testing.T) {
	RegisterTools()

	for _, name := range fileToolNames {
		factory, ok := agent.GetPlainToolFactory(name)
		require.True(t, ok, "file tool %q should be registered", name)
		require.NotNil(t, factory, "factory for %q should not be nil", name)
	}
}

func TestMakeFileToolFactory_ReadFileAndSaveFile(t *testing.T) {
	tempDir := t.TempDir()

	// Register tools (idempotent)
	RegisterTools()

	// Get save_file factory and save a file.
	saveFactory, ok := agent.GetPlainToolFactory("save_file")
	require.True(t, ok)
	saveTool, err := saveFactory(agent.PlainToolFactoryConfig{
		ID:         "save_file",
		Properties: map[string]any{"base_dir": tempDir},
	})
	require.NoError(t, err)

	args, err := json.Marshal(map[string]any{
		"file_name": "hello.txt",
		"contents":  "hello world",
		"overwrite": true,
	})
	require.NoError(t, err)

	_, err = saveTool.Call(context.Background(), args)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tempDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	// Get read_file factory and read the file back.
	readFactory, ok := agent.GetPlainToolFactory("read_file")
	require.True(t, ok)
	readTool, err := readFactory(agent.PlainToolFactoryConfig{
		ID:         "read_file",
		Properties: map[string]any{"base_dir": tempDir},
	})
	require.NoError(t, err)

	args, err = json.Marshal(map[string]any{"file_name": "hello.txt"})
	require.NoError(t, err)

	raw, err := readTool.Call(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, raw)

	// Verify the file was written correctly via save_file and can be read back.
	content, err = os.ReadFile(filepath.Join(tempDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestResolveBaseDir(t *testing.T) {
	assert.Equal(t, ".", resolveBaseDir(nil))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{}))
	assert.Equal(t, "/tmp", resolveBaseDir(map[string]any{"base_dir": "/tmp"}))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{"base_dir": ""}))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{"base_dir": 123}))
}
