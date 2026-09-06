package tagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestConfig_WorkingDir 验证 C 方案(框架级 working_dir)配置层:yaml 解析 + 空默认(现状零变化)
// + TAGENT_WORKING_DIR env 覆盖(部署时免改 yaml 指定 clone 根)。
func TestConfig_WorkingDir(t *testing.T) {
	t.Run("yaml 解析 working_dir", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "") // 隔离 env(空=不触发覆盖)
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /home/user/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/home/user/codes", cfg.WorkingDir)
	})

	t.Run("空 working_dir 默认空(=继承进程 cwd,现状逐字节不变)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Empty(t, cfg.WorkingDir, "空=file base_dir '.'/exec 继承进程 cwd,现状不变")
	})

	t.Run("TAGENT_WORKING_DIR env 覆盖 yaml 值", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/codes")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /yaml/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/codes", cfg.WorkingDir, "env 应覆盖 yaml(部署时灵活指定 clone 根)")
	})

	t.Run("TAGENT_WORKING_DIR env 注入(yaml 未配)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/only")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/only", cfg.WorkingDir)
	})
}
