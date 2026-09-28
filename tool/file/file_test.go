package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SpellingDragon/tagent/agent"
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

	RegisterTools()

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

	content, err = os.ReadFile(filepath.Join(tempDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestResolveBaseDir(t *testing.T) {
	assert.Equal(t, ".", resolveBaseDir(nil, ""))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{}, ""))
	assert.Equal(t, "/tmp", resolveBaseDir(map[string]any{"base_dir": "/tmp"}, ""))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{"base_dir": ""}, ""))
	assert.Equal(t, ".", resolveBaseDir(map[string]any{"base_dir": 123}, ""))
	assert.Equal(t, "/codes", resolveBaseDir(nil, "/codes"))
	assert.Equal(t, "/codes", resolveBaseDir(map[string]any{}, "/codes"))
	assert.Equal(t, "/codes", resolveBaseDir(map[string]any{"base_dir": ""}, "/codes"))
	assert.Equal(t, "/tmp", resolveBaseDir(map[string]any{"base_dir": "/tmp"}, "/codes"))
}

// TestMakeFileToolFactory_WorkingDirAsBaseDir 是 C 方案(框架级 working_dir)端到端回归:
// cfg.WorkingDir(config.working_dir / TAGENT_WORKING_DIR 注入)在无显式 base_dir 时作为 file
// 工具的根目录 —— save_file 落盘到 WorkingDir 而非进程 cwd;显式 base_dir 仍优先。
func TestMakeFileToolFactory_WorkingDirAsBaseDir(t *testing.T) {
	RegisterTools()
	workingDir := t.TempDir()

	saveFactory, ok := agent.GetPlainToolFactory("save_file")
	require.True(t, ok)
	saveTool, err := saveFactory(agent.PlainToolFactoryConfig{ID: "save_file", WorkingDir: workingDir})
	require.NoError(t, err)

	args, _ := json.Marshal(map[string]any{"file_name": "wd.txt", "contents": "via WorkingDir", "overwrite": true})
	_, err = saveTool.Call(context.Background(), args)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(workingDir, "wd.txt"))
	require.NoError(t, err, "save_file 应以 cfg.WorkingDir 为 base_dir 落盘")
	assert.Equal(t, "via WorkingDir", string(content))

	explicit := t.TempDir()
	saveTool2, err := saveFactory(agent.PlainToolFactoryConfig{
		ID:         "save_file",
		WorkingDir: workingDir,
		Properties: map[string]any{"base_dir": explicit},
	})
	require.NoError(t, err)
	args2, _ := json.Marshal(map[string]any{"file_name": "explicit.txt", "contents": "x", "overwrite": true})
	_, err = saveTool2.Call(context.Background(), args2)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(explicit, "explicit.txt"))
	assert.NoError(t, err, "显式 base_dir 应优先于 WorkingDir")
	_, err = os.Stat(filepath.Join(workingDir, "explicit.txt"))
	assert.True(t, os.IsNotExist(err), "显式 base_dir 优先 → 不应落 WorkingDir")
}
